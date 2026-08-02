package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"awg3routerosgateway/internal/config"
)

type Process interface {
	PID() int
	Kill() error
}

type Waiter interface {
	Wait(context.Context) error
}

type StartFunc func(context.Context, config.Pair) (Process, error)
type ProbeFunc func(context.Context, config.Pair, Process) error
type StopFunc func(context.Context, Process) error

type RecoveryPolicy struct {
	MaxAttempts      int
	Window           time.Duration
	InitialBackoff   time.Duration
	MaxBackoff       time.Duration
	ApplyTimeout     time.Duration
	ReadinessTimeout time.Duration
	RollbackTimeout  time.Duration
}

type Status struct {
	Running             bool      `json:"running"`
	Ready               bool      `json:"ready"`
	Degraded            bool      `json:"degraded"`
	DegradedReason      string    `json:"degraded_reason,omitempty"`
	ContendedPID        int       `json:"contended_pid,omitempty"`
	ContendedGeneration string    `json:"contended_generation,omitempty"`
	PID                 int       `json:"pid"`
	Generation          string    `json:"generation"`
	LastExitCode        int       `json:"last_exit_code,omitempty"`
	LastExitCategory    string    `json:"last_exit_category,omitempty"`
	LastExitSignal      string    `json:"last_exit_signal,omitempty"`
	RestartCount        int       `json:"restart_count,omitempty"`
	LastError           string    `json:"last_error,omitempty"`
	StartedAt           time.Time `json:"started_at,omitempty"`
	LastApplied         time.Time `json:"last_applied,omitempty"`
	StoppedAt           time.Time `json:"stopped_at,omitempty"`
}

type Controller struct {
	start    StartFunc
	probe    ProbeFunc
	network  NetworkAdapter
	stop     StopFunc
	recovery RecoveryPolicy

	mu                 sync.Mutex
	wg                 sync.WaitGroup
	lifecycleCtx       context.Context
	lifecycleCancel    context.CancelFunc
	current            *state
	status             Status
	restartWindowStart time.Time
}

type state struct {
	pair     config.Pair
	process  Process
	stopping bool
}

type networkPhaseError struct {
	phase string
	err   error
}

func (e *networkPhaseError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *networkPhaseError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func New(start StartFunc, probe ProbeFunc, network NetworkAdapter, stop StopFunc) (*Controller, error) {
	if start == nil {
		return nil, errors.New("start function is required")
	}
	lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())
	return &Controller{
		start:           start,
		probe:           probe,
		network:         network,
		stop:            stop,
		lifecycleCtx:    lifecycleCtx,
		lifecycleCancel: lifecycleCancel,
		recovery: RecoveryPolicy{
			MaxAttempts:      3,
			Window:           time.Minute,
			InitialBackoff:   100 * time.Millisecond,
			MaxBackoff:       time.Second,
			ApplyTimeout:     30 * time.Second,
			ReadinessTimeout: 15 * time.Second,
			RollbackTimeout:  30 * time.Second,
		},
	}, nil
}

func (c *Controller) Start(ctx context.Context, pair config.Pair) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current != nil {
		return nil
	}
	return c.startLocked(ctx, pair)
}

func (c *Controller) Apply(ctx context.Context, pair config.Pair) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.status.Degraded {
		return errors.New("runtime degraded")
	}
	if c.current == nil {
		return c.startLocked(ctx, pair)
	}
	if c.probe != nil {
		if err := c.probe(ctx, pair, nil); err != nil {
			c.status.LastError = err.Error()
			return err
		}
	}
	prev := c.current
	prev.stopping = true
	if err := c.stopProcess(ctx, prev.process); err != nil {
		c.status = Status{
			Running:             true,
			Ready:               false,
			Degraded:            true,
			DegradedReason:      "previous_stop_failed",
			ContendedPID:        prev.process.PID(),
			ContendedGeneration: prev.pair.Config.Generation,
			PID:                 prev.process.PID(),
			Generation:          prev.pair.Config.Generation,
			LastError:           err.Error(),
			StartedAt:           time.Now().UTC(),
		}
		return err
	}
	c.current = nil
	nextProc, err := c.start(ctx, pair)
	if err != nil {
		c.status = Status{
			Running:        false,
			Ready:          false,
			Degraded:       true,
			DegradedReason: "candidate_start_failed",
			LastError:      err.Error(),
		}
		rollbackCtx, cancel := c.rollbackContext()
		defer cancel()
		if restoreErr := c.restorePreviousLocked(rollbackCtx, prev); restoreErr != nil {
			c.status.DegradedReason = c.rollbackFailureReason(restoreErr, "previous_restart_failed")
			c.status.LastError = restoreErr.Error()
			return fmt.Errorf("candidate start failed: %w; rollback failed: %v", err, restoreErr)
		}
		return err
	}
	if err := c.applyNetwork(ctx, pair, nextProc, false); err != nil {
		rollbackCtx, cancel := c.rollbackContext()
		defer cancel()
		phase := candidateFailureReason(err, "candidate_network_apply_failed")
		c.status = Status{
			Running:             false,
			Ready:               false,
			Degraded:            true,
			DegradedReason:      phase,
			ContendedPID:        nextProc.PID(),
			ContendedGeneration: pair.Config.Generation,
			LastError:           err.Error(),
		}
		stopErr := c.stopProcess(rollbackCtx, nextProc)
		if stopErr != nil {
			c.status.DegradedReason = c.rollbackFailureReason(stopErr, "candidate_stop_failed")
			c.status.LastError = stopErr.Error()
			return fmt.Errorf("apply failed: %w; candidate stop failed: %v", err, stopErr)
		}
		if restoreErr := c.restorePreviousLocked(rollbackCtx, prev); restoreErr != nil {
			c.status.DegradedReason = c.rollbackFailureReason(restoreErr, "previous_restart_failed")
			c.status.LastError = restoreErr.Error()
			return fmt.Errorf("apply failed: %w; rollback failed: %v", err, restoreErr)
		}
		return err
	}
	c.current = &state{pair: pair, process: nextProc}
	c.status = Status{
		Running:     true,
		Ready:       true,
		Degraded:    false,
		PID:         nextProc.PID(),
		Generation:  pair.Config.Generation,
		StartedAt:   time.Now().UTC(),
		LastApplied: time.Now().UTC(),
	}
	c.launchMonitorLocked(nextProc)
	return nil
}

func (c *Controller) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current == nil {
		return nil
	}
	c.current.stopping = true
	err := c.stopProcess(ctx, c.current.process)
	if err != nil {
		c.status.Running = true
		c.status.Ready = false
		c.status.Degraded = true
		c.status.DegradedReason = "previous_stop_failed"
		c.status.ContendedPID = c.current.process.PID()
		c.status.ContendedGeneration = c.current.pair.Config.Generation
		c.status.LastError = err.Error()
		return err
	}
	if cleaner, ok := c.network.(interface {
		Cleanup(context.Context, config.Pair) error
	}); ok {
		if cleanupErr := cleaner.Cleanup(ctx, c.current.pair); cleanupErr != nil {
			c.status.Running = false
			c.status.Ready = false
			c.status.Degraded = true
			c.status.DegradedReason = "endpoint_cleanup_failed"
			c.status.ContendedPID = c.current.process.PID()
			c.status.ContendedGeneration = c.current.pair.Config.Generation
			c.status.LastError = cleanupErr.Error()
			c.current = nil
			return cleanupErr
		}
	}
	c.current = nil
	c.status.Running = false
	c.status.Ready = false
	c.status.Degraded = false
	c.status.DegradedReason = ""
	c.status.ContendedPID = 0
	c.status.ContendedGeneration = ""
	c.status.StoppedAt = time.Now().UTC()
	c.restartWindowStart = time.Time{}
	c.status.RestartCount = 0
	return err
}

func (c *Controller) Shutdown(ctx context.Context) error {
	c.mu.Lock()
	current := c.current
	degraded := c.status.Degraded
	if current != nil && !degraded {
		current.stopping = true
	}
	c.mu.Unlock()
	if current != nil && !degraded {
		_ = c.stopProcess(ctx, current.process)
		c.mu.Lock()
		if c.current != nil && c.current.process == current.process {
			c.current = nil
			c.status.Running = false
			c.status.Ready = false
			c.status.Degraded = false
			c.status.DegradedReason = ""
			c.status.ContendedPID = 0
			c.status.ContendedGeneration = ""
			c.status.PID = 0
			c.status.StoppedAt = time.Now().UTC()
			c.restartWindowStart = time.Time{}
			c.status.RestartCount = 0
		}
		c.mu.Unlock()
	}
	c.lifecycleCancel()
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Controller) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

func (c *Controller) startLocked(ctx context.Context, pair config.Pair) error {
	if c.probe != nil {
		if err := c.probe(ctx, pair, nil); err != nil {
			c.status.LastError = err.Error()
			return err
		}
	}
	proc, err := c.start(ctx, pair)
	if err != nil {
		c.status.LastError = err.Error()
		return err
	}
	if err := c.applyNetwork(ctx, pair, proc, false); err != nil {
		_ = c.stopProcess(ctx, proc)
		c.status.LastError = err.Error()
		return err
	}
	c.current = &state{pair: pair, process: proc}
	c.status = Status{
		Running:     true,
		Ready:       true,
		Degraded:    false,
		PID:         proc.PID(),
		Generation:  pair.Config.Generation,
		StartedAt:   time.Now().UTC(),
		LastApplied: time.Now().UTC(),
	}
	c.restartWindowStart = time.Now().UTC()
	c.status.RestartCount = 0
	c.launchMonitorLocked(proc)
	return nil
}

func (c *Controller) stopProcess(ctx context.Context, proc Process) error {
	if proc == nil {
		return nil
	}
	var err error
	if c.stop != nil {
		err = c.stop(ctx, proc)
	} else {
		err = proc.Kill()
	}
	if err != nil {
		return err
	}
	waiter, ok := proc.(Waiter)
	if !ok {
		return nil
	}
	return waiter.Wait(ctx)
}

func (c *Controller) restorePreviousLocked(ctx context.Context, prev *state) error {
	if prev == nil {
		return errors.New("previous runtime is missing")
	}
	if c.probe != nil {
		if err := c.probe(ctx, prev.pair, nil); err != nil {
			c.status = Status{
				Running:        false,
				Ready:          false,
				Degraded:       true,
				DegradedReason: "previous_readiness_failed",
				LastError:      err.Error(),
			}
			return err
		}
	}
	proc, err := c.start(ctx, prev.pair)
	if err != nil {
		c.status = Status{
			Running:        false,
			Ready:          false,
			Degraded:       true,
			DegradedReason: "previous_restart_failed",
			LastError:      err.Error(),
		}
		return err
	}
	if err := c.applyNetwork(ctx, prev.pair, proc, true); err != nil {
		_ = c.stopProcess(ctx, proc)
		phase := previousFailureReason(err, "previous_network_restore_failed")
		c.status = Status{
			Running:        false,
			Ready:          false,
			Degraded:       true,
			DegradedReason: phase,
			LastError:      err.Error(),
		}
		return err
	}
	c.current = &state{pair: prev.pair, process: proc}
	c.status = Status{
		Running:     true,
		Ready:       true,
		Degraded:    false,
		PID:         proc.PID(),
		Generation:  prev.pair.Config.Generation,
		StartedAt:   time.Now().UTC(),
		LastApplied: time.Now().UTC(),
	}
	c.restartWindowStart = time.Now().UTC()
	c.status.RestartCount = 0
	c.launchMonitorLocked(proc)
	return nil
}

func (c *Controller) rollbackContext() (context.Context, context.CancelFunc) {
	return c.runtimeContext(c.lifecycleCtx, c.recovery.RollbackTimeout)
}

func (c *Controller) rollbackFailureReason(err error, fallback string) string {
	if errors.Is(err, context.Canceled) {
		return "rollback_cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "rollback_timeout"
	}
	return fallback
}

func candidateFailureReason(err error, fallback string) string {
	if phaseErr, ok := err.(*networkPhaseError); ok && phaseErr != nil && phaseErr.phase != "" {
		return phaseErr.phase
	}
	return fallback
}

func previousFailureReason(err error, fallback string) string {
	if phaseErr, ok := err.(*networkPhaseError); ok && phaseErr != nil && phaseErr.phase != "" {
		return phaseErr.phase
	}
	return fallback
}

func unexpectedExitReason(info ProcessExitInfo, err error) string {
	switch info.Category {
	case "clean_exit":
		return "unexpected_clean_exit"
	case "signal":
		return "unexpected_signal_exit"
	case "exit":
		if info.Code == 0 && err == nil {
			return "unexpected_clean_exit"
		}
		return "unexpected_nonzero_exit"
	default:
		if err == nil {
			return "unexpected_clean_exit"
		}
		return "unexpected_nonzero_exit"
	}
}

func (c *Controller) launchMonitorLocked(proc Process) {
	waiter, ok := proc.(Waiter)
	if !ok {
		return
	}
	c.wg.Add(1)
	go c.monitorProcess(proc, waiter)
}

func (c *Controller) applyNetwork(ctx context.Context, pair config.Pair, proc Process, restore bool) error {
	if c.network == nil {
		return nil
	}
	handle, ok := proc.(ProcessHandle)
	if !ok {
		return errors.New("process handle is required")
	}
	timeout := c.recovery.ApplyTimeout
	if restore && c.recovery.RollbackTimeout > 0 {
		timeout = c.recovery.RollbackTimeout
	}
	applyCtx, cancel := c.runtimeContext(ctx, timeout)
	defer cancel()
	if restore {
		if err := c.network.Restore(applyCtx, pair); err != nil {
			return &networkPhaseError{phase: "previous_network_restore_failed", err: err}
		}
	} else {
		if err := c.network.Apply(applyCtx, pair); err != nil {
			return &networkPhaseError{phase: "candidate_network_apply_failed", err: err}
		}
	}
	readyCtx, cancelReady := c.runtimeContext(ctx, c.recovery.ReadinessTimeout)
	defer cancelReady()
	if err := c.network.Ready(readyCtx, pair, handle); err != nil {
		if restore {
			return &networkPhaseError{phase: "previous_readiness_failed", err: err}
		}
		return &networkPhaseError{phase: "candidate_readiness_failed", err: err}
	}
	return nil
}

func (c *Controller) runtimeContext(base context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if base == nil {
		base = c.lifecycleCtx
	}
	if base == nil {
		base = context.Background()
	}
	if timeout <= 0 {
		return context.WithCancel(base)
	}
	return context.WithTimeout(base, timeout)
}

func (c *Controller) monitorProcess(proc Process, waiter Waiter) {
	defer c.wg.Done()
	err := waiter.Wait(c.lifecycleCtx)
	exitInfo := ProcessExitInfo{Category: "unknown_exit"}
	if reporter, ok := proc.(ExitReporter); ok {
		exitInfo = reporter.ExitInfo()
	}
	if c.lifecycleCtx.Err() != nil {
		return
	}
	reason := unexpectedExitReason(exitInfo, err)
	if err == nil {
		err = errors.New(reason)
	}
	backoff := c.recovery.InitialBackoff
	if backoff <= 0 {
		backoff = 100 * time.Millisecond
	}
	maxBackoff := c.recovery.MaxBackoff
	if maxBackoff <= 0 {
		maxBackoff = time.Second
	}
	attempts := c.recovery.MaxAttempts
	if attempts <= 0 {
		attempts = 1
	}
	c.mu.Lock()
	current := c.current
	if current == nil || current.process != proc || current.stopping {
		c.mu.Unlock()
		return
	}
	pair := current.pair
	c.status.LastExitCode = exitInfo.Code
	c.status.LastExitCategory = exitInfo.Category
	c.status.LastExitSignal = exitInfo.Signal
	c.status.Running = false
	c.status.Ready = false
	c.status.Degraded = true
	c.status.DegradedReason = reason
	c.status.LastError = err.Error()
	c.status.StoppedAt = time.Now().UTC()
	if c.restartWindowStart.IsZero() || (c.recovery.Window > 0 && time.Since(c.restartWindowStart) > c.recovery.Window) {
		c.restartWindowStart = time.Now().UTC()
		c.status.RestartCount = 0
	}
	for attempt := 0; attempt < attempts; attempt++ {
		delay := backoff
		if delay > maxBackoff {
			delay = maxBackoff
		}
		c.mu.Unlock()

		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-c.lifecycleCtx.Done():
			timer.Stop()
			return
		}
		timer.Stop()

		c.mu.Lock()
		if c.current == nil || c.current.process != proc || c.current.stopping {
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()

		if c.probe != nil {
			if probeErr := c.probe(c.lifecycleCtx, pair, nil); probeErr != nil {
				err = probeErr
				backoff *= 2
				continue
			}
		}
		newProc, startErr := c.start(c.lifecycleCtx, pair)
		if startErr != nil {
			err = startErr
			backoff *= 2
			continue
		}
		if applyErr := c.applyNetwork(c.lifecycleCtx, pair, newProc, false); applyErr != nil {
			_ = c.stopProcess(c.lifecycleCtx, newProc)
			err = applyErr
			backoff *= 2
			continue
		}

		c.mu.Lock()
		if c.current == nil || c.current.process != proc || c.current.stopping {
			c.mu.Unlock()
			_ = c.stopProcess(c.lifecycleCtx, newProc)
			return
		}
		c.current = &state{pair: pair, process: newProc}
		c.status.Running = true
		c.status.Ready = true
		c.status.Degraded = false
		c.status.DegradedReason = ""
		c.status.ContendedPID = 0
		c.status.ContendedGeneration = ""
		c.status.PID = newProc.PID()
		c.status.Generation = pair.Config.Generation
		c.status.RestartCount++
		c.status.StartedAt = time.Now().UTC()
		c.status.LastApplied = time.Now().UTC()
		c.restartWindowStart = time.Now().UTC()
		c.launchMonitorLocked(newProc)
		c.mu.Unlock()
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current == nil || c.current.process != proc || c.current.stopping {
		return
	}
	c.status.DegradedReason = "restart_backoff_exhausted"
	c.status.LastError = err.Error()
	c.status.StoppedAt = time.Now().UTC()
}

type FakeProcess struct {
	PIDValue int
	Dead     bool
}

func (p *FakeProcess) PID() int { return p.PIDValue }

func (p *FakeProcess) Kill() error {
	p.Dead = true
	return nil
}

func (p *FakeProcess) Signal(os.Signal) error { return nil }

func (p *FakeProcess) Wait(context.Context) error { return nil }

type FakeLauncher struct {
	NextPID  int
	Failures map[string]error
	Started  []string
}

func (f *FakeLauncher) Start(_ context.Context, pair config.Pair) (Process, error) {
	if f.Failures != nil {
		if err, ok := f.Failures[pair.Config.Generation]; ok {
			return nil, err
		}
	}
	f.NextPID++
	f.Started = append(f.Started, pair.Config.Generation)
	return &FakeProcess{PIDValue: f.NextPID}, nil
}

func IgnoreStop(_ context.Context, proc Process) error {
	return proc.Kill()
}

func NewFromLauncher(launcher *FakeLauncher, probe ProbeFunc) (*Controller, error) {
	if launcher == nil {
		return nil, fmt.Errorf("launcher is required")
	}
	return New(launcher.Start, probe, nil, IgnoreStop)
}

var _ Process = (*FakeProcess)(nil)
var _ = os.ErrClosed
