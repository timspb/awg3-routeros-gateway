package runtime

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"awg3routerosgateway/internal/config"
)

func TestControllerApplyRollsBackOnProbeFailure(t *testing.T) {
	launcher := &FakeLauncher{}
	network := &countingNetwork{}
	stopCount := 0
	ctrl, err := New(launcher.Start, func(_ context.Context, pair config.Pair, _ Process) error {
		if pair.Config.Generation == "gen-2" {
			return errors.New("not ready")
		}
		return nil
	}, network, func(context.Context, Process) error {
		stopCount++
		return nil
	})
	if err != nil {
		t.Fatalf("new controller failed: %v", err)
	}

	gen1 := pairForRuntime("gen-1")
	gen2 := pairForRuntime("gen-2")

	if err := ctrl.Start(context.Background(), gen1); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	baselineApply := network.applyCount
	baselineRestore := network.restoreCount
	baselineReady := network.readyCount
	if got := ctrl.Status(); got.Generation != "gen-1" || !got.Running || !got.Ready {
		t.Fatalf("unexpected status after start: %#v", got)
	}
	if err := ctrl.Apply(context.Background(), gen2); err == nil {
		t.Fatalf("expected apply to fail")
	}
	if stopCount != 0 {
		t.Fatalf("expected previous runtime to remain untouched on probe failure, stopCount=%d", stopCount)
	}
	if network.applyCount != baselineApply || network.restoreCount != baselineRestore || network.readyCount != baselineReady {
		t.Fatalf("expected no network activity on probe failure, got %#v", network)
	}
	got := ctrl.Status()
	if got.Generation != "gen-1" {
		t.Fatalf("expected rollback to gen-1, got %#v", got)
	}
	if !got.Running || !got.Ready {
		t.Fatalf("expected controller to stay running after rollback, got %#v", got)
	}
	if len(launcher.Started) != 1 || launcher.Started[0] != "gen-1" {
		t.Fatalf("unexpected start sequence: %#v", launcher.Started)
	}
}

func TestControllerApplyMarksDegradedWhenPreviousStopFails(t *testing.T) {
	stopErr := errors.New("stop previous failed")
	launcher := &FakeLauncher{}
	ctrl, err := New(launcher.Start, nil, nil, func(_ context.Context, proc Process) error {
		if proc.PID() == 1 {
			return stopErr
		}
		return proc.Kill()
	})
	if err != nil {
		t.Fatalf("new controller failed: %v", err)
	}

	if err := ctrl.Start(context.Background(), pairForRuntime("gen-1")); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if err := ctrl.Apply(context.Background(), pairForRuntime("gen-2")); err == nil {
		t.Fatalf("expected apply to fail")
	}
	got := ctrl.Status()
	if !got.Degraded || got.DegradedReason != "previous_stop_failed" {
		t.Fatalf("expected degraded previous_stop_failed, got %#v", got)
	}
	if got.Ready {
		t.Fatalf("expected ready=false after stop failure, got %#v", got)
	}
	if got.Generation != "gen-1" {
		t.Fatalf("expected current generation to stay gen-1, got %#v", got)
	}
	if err := ctrl.Apply(context.Background(), pairForRuntime("gen-3")); err == nil {
		t.Fatalf("expected second apply to be blocked while degraded")
	}
}

func TestControllerApplyMarksDegradedWhenCandidateStopFails(t *testing.T) {
	stopErr := errors.New("stop candidate failed")
	launcher := &FakeLauncher{}
	network := &failingNetwork{applyErr: errors.New("apply failed"), failOnApplyCount: 2}
	ctrl, err := New(launcher.Start, func(_ context.Context, pair config.Pair, _ Process) error {
		return nil
	}, network, func(_ context.Context, proc Process) error {
		if proc.PID() == 2 {
			return stopErr
		}
		return proc.Kill()
	})
	if err != nil {
		t.Fatalf("new controller failed: %v", err)
	}

	if err := ctrl.Start(context.Background(), pairForRuntime("gen-1")); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if err := ctrl.Apply(context.Background(), pairForRuntime("gen-2")); err == nil {
		t.Fatalf("expected apply to fail")
	}
	got := ctrl.Status()
	if !got.Degraded || got.DegradedReason != "candidate_stop_failed" {
		t.Fatalf("expected degraded candidate_stop_failed, got %#v", got)
	}
	if got.Ready {
		t.Fatalf("expected ready=false after candidate stop failure, got %#v", got)
	}
}

func TestControllerStopClearsStatus(t *testing.T) {
	launcher := &FakeLauncher{}
	ctrl, err := New(launcher.Start, nil, nil, IgnoreStop)
	if err != nil {
		t.Fatalf("new controller failed: %v", err)
	}
	if err := ctrl.Start(context.Background(), pairForRuntime("gen-stop")); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if err := ctrl.Stop(context.Background()); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
	got := ctrl.Status()
	if got.Running || got.Ready {
		t.Fatalf("expected controller to be stopped, got %#v", got)
	}
}

func TestControllerMarksDegradedOnUnexpectedExit(t *testing.T) {
	exitCh := make(chan struct{})
	ctrl, err := New(func(context.Context, config.Pair) (Process, error) {
		return &exitProcess{
			exitCh:  exitCh,
			waitErr: errors.New("exit 42"),
			info:    ProcessExitInfo{Category: "exit", Code: 42},
		}, nil
	}, nil, nil, IgnoreStop)
	if err != nil {
		t.Fatalf("new controller failed: %v", err)
	}
	if err := ctrl.Start(context.Background(), pairForRuntime("gen-monitor")); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	close(exitCh)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := ctrl.Status(); got.Degraded {
			if got.DegradedReason != "unexpected_nonzero_exit" {
				t.Fatalf("unexpected degraded reason: %#v", got)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("controller never transitioned to degraded on unexpected exit: %#v", ctrl.Status())
}

func TestControllerMarksDegradedOnUnexpectedCleanExit(t *testing.T) {
	exitCh := make(chan struct{})
	ctrl, err := New(func(context.Context, config.Pair) (Process, error) {
		return &exitProcess{
			exitCh: exitCh,
			info:   ProcessExitInfo{Category: "clean_exit", Code: 0},
		}, nil
	}, nil, nil, IgnoreStop)
	if err != nil {
		t.Fatalf("new controller failed: %v", err)
	}
	if err := ctrl.Start(context.Background(), pairForRuntime("gen-clean-exit")); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	close(exitCh)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := ctrl.Status()
		if got.Degraded {
			if got.DegradedReason != "unexpected_clean_exit" {
				t.Fatalf("unexpected degraded reason: %#v", got)
			}
			if got.LastExitCategory != "clean_exit" || got.LastExitCode != 0 {
				t.Fatalf("unexpected exit info: %#v", got)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("controller never transitioned to degraded on clean exit: %#v", ctrl.Status())
}

func TestControllerMarksDegradedOnUnexpectedSignalExit(t *testing.T) {
	exitCh := make(chan struct{})
	ctrl, err := New(func(context.Context, config.Pair) (Process, error) {
		return &exitProcess{
			exitCh:  exitCh,
			waitErr: errors.New("signal killed"),
			info:    ProcessExitInfo{Category: "signal", Code: 137, Signal: "SIGKILL"},
		}, nil
	}, nil, nil, IgnoreStop)
	if err != nil {
		t.Fatalf("new controller failed: %v", err)
	}
	if err := ctrl.Start(context.Background(), pairForRuntime("gen-signal-exit")); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	close(exitCh)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := ctrl.Status()
		if got.Degraded {
			if got.DegradedReason != "unexpected_signal_exit" {
				t.Fatalf("unexpected degraded reason: %#v", got)
			}
			if got.LastExitCategory != "signal" || got.LastExitCode != 137 || got.LastExitSignal != "SIGKILL" {
				t.Fatalf("unexpected exit info: %#v", got)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("controller never transitioned to degraded on signal exit: %#v", ctrl.Status())
}

func TestControllerRollbackIgnoresRequestCancellation(t *testing.T) {
	reqCtx, cancelReq := context.WithCancel(context.Background())
	defer cancelReq()
	previousHold := make(chan struct{})
	rollbackReady := make(chan struct{})
	startCount := 0
	ctrl, err := New(func(context.Context, config.Pair) (Process, error) {
		startCount++
		switch startCount {
		case 1:
			return &holdProcess{done: previousHold, pid: 4101}, nil
		case 2:
			return &holdProcess{done: make(chan struct{}), pid: 4102}, nil
		case 3:
			return &holdProcess{done: make(chan struct{}), pid: 4103}, nil
		default:
			return &holdProcess{done: make(chan struct{}), pid: 4104}, nil
		}
	}, nil, &rollbackCancelNetwork{
		applyErr:         errors.New("candidate apply failed"),
		cancelOnApplyErr: cancelReq,
		restoreReady:     rollbackReady,
	}, IgnoreStop)
	if err != nil {
		t.Fatalf("new controller failed: %v", err)
	}
	if err := ctrl.Start(context.Background(), pairForRuntime("gen-1")); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- ctrl.Apply(reqCtx, pairForRuntime("gen-2"))
	}()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatalf("expected apply to fail")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("apply did not return")
	}
	select {
	case <-rollbackReady:
	case <-time.After(2 * time.Second):
		t.Fatalf("rollback did not reach restore path")
	}
	got := ctrl.Status()
	if got.Generation != "gen-1" || !got.Running || !got.Ready {
		t.Fatalf("expected rollback to restore gen-1 ready state, got %#v", got)
	}
}

func TestControllerRollbackStopsOnShutdownAndMarksDegraded(t *testing.T) {
	restoreBlocked := make(chan struct{})
	restoreStarted := make(chan struct{})
	startCount := 0
	ctrl, err := New(func(context.Context, config.Pair) (Process, error) {
		startCount++
		switch startCount {
		case 1:
			return &holdProcess{done: make(chan struct{}), pid: 5101}, nil
		case 2:
			return &holdProcess{done: make(chan struct{}), pid: 5102}, nil
		default:
			return &holdProcess{done: make(chan struct{}), pid: 5103}, nil
		}
	}, nil, &blockingRollbackNetwork{
		applyErr:       errors.New("candidate apply failed"),
		restoreStarted: restoreStarted,
		restoreBlocked: restoreBlocked,
	}, IgnoreStop)
	if err != nil {
		t.Fatalf("new controller failed: %v", err)
	}
	if err := ctrl.Start(context.Background(), pairForRuntime("gen-1")); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- ctrl.Apply(context.Background(), pairForRuntime("gen-2"))
	}()
	select {
	case <-restoreStarted:
	case <-time.After(2 * time.Second):
		t.Fatalf("rollback did not start")
	}
	if err := ctrl.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatalf("expected apply to fail during rollback")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("apply did not complete after shutdown")
	}
	got := ctrl.Status()
	if !got.Degraded || got.Ready {
		t.Fatalf("expected degraded and not ready after shutdown during rollback, got %#v", got)
	}
	if got.DegradedReason != "rollback_cancelled" && got.DegradedReason != "rollback_timeout" {
		t.Fatalf("unexpected degraded reason after rollback shutdown: %#v", got)
	}
	close(restoreBlocked)
}

func TestControllerRecoveryRestartsUnexpectedExit(t *testing.T) {
	exitCh := make(chan struct{})
	holdCh := make(chan struct{})
	startCount := 0
	ctrl, err := New(func(context.Context, config.Pair) (Process, error) {
		startCount++
		if startCount == 1 {
			return &exitProcess{exitCh: exitCh, waitErr: errors.New("exit 42"), info: ProcessExitInfo{Category: "exit", Code: 42}}, nil
		}
		return &holdProcess{done: holdCh, pid: 5252}, nil
	}, nil, nil, IgnoreStop)
	if err != nil {
		t.Fatalf("new controller failed: %v", err)
	}
	ctrl.recovery.MaxAttempts = 2
	ctrl.recovery.InitialBackoff = 5 * time.Millisecond
	ctrl.recovery.MaxBackoff = 10 * time.Millisecond
	if err := ctrl.Start(context.Background(), pairForRuntime("gen-recover")); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	close(exitCh)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := ctrl.Status()
		if got.RestartCount > 0 && got.Running && got.Ready && !got.Degraded {
			close(holdCh)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(holdCh)
	t.Fatalf("controller never recovered from unexpected exit: %#v", ctrl.Status())
}

func TestControllerShutdownStopsRunningRuntime(t *testing.T) {
	done := make(chan struct{})
	ctrl, err := New(func(context.Context, config.Pair) (Process, error) {
		return &shutdownProcess{done: done, pid: 5353}, nil
	}, nil, nil, IgnoreStop)
	if err != nil {
		t.Fatalf("new controller failed: %v", err)
	}
	if err := ctrl.Start(context.Background(), pairForRuntime("gen-shutdown")); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if err := ctrl.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
	select {
	case <-done:
	default:
		t.Fatalf("expected runtime to be stopped during shutdown")
	}
	got := ctrl.Status()
	if got.Running || got.Ready {
		t.Fatalf("expected runtime to be stopped, got %#v", got)
	}
}

func TestControllerShutdownDoesNotCleanupNetworkState(t *testing.T) {
	done := make(chan struct{})
	network := &shutdownCleanupNetwork{}
	ctrl, err := New(func(context.Context, config.Pair) (Process, error) {
		return &shutdownProcess{done: done, pid: 5354}, nil
	}, nil, network, IgnoreStop)
	if err != nil {
		t.Fatalf("new controller failed: %v", err)
	}
	if err := ctrl.Start(context.Background(), pairForRuntime("gen-shutdown-cleanup")); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if err := ctrl.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
	if network.cleanupCount != 0 {
		t.Fatalf("expected shutdown to skip network cleanup, cleanupCount=%d", network.cleanupCount)
	}
	select {
	case <-done:
	default:
		t.Fatalf("expected runtime to be stopped during shutdown")
	}
}

func TestExecProcessWaitBroadcastsResult(t *testing.T) {
	cmd := helperCommand(context.Background(), "exit0", "")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start command failed: %v", err)
	}
	proc := newExecProcess(cmd)
	var wg sync.WaitGroup
	wg.Add(2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			errs <- proc.Wait(context.Background())
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("wait returned error: %v", err)
		}
	}
	if err := proc.Wait(context.Background()); err != nil {
		t.Fatalf("subsequent wait returned error: %v", err)
	}
}

func pairForRuntime(gen string) config.Pair {
	return config.Pair{
		Config: config.Config{
			Version:                   "1",
			Generation:                gen,
			InterfaceName:             "awg3",
			ListenPort:                51820,
			MTU:                       1380,
			TunnelAddress:             "10.99.99.2/30",
			Gateway:                   "10.99.99.1",
			VethAddress:               "172.31.255.2/30",
			AllowedIPs:                []string{"10.99.99.0/24"},
			Endpoint:                  "213.176.116.165:443",
			Jc:                        4,
			Jmin:                      50,
			Jmax:                      250,
			S1:                        84,
			S2:                        40,
			S3:                        46,
			S4:                        20,
			H1:                        50,
			H2:                        250,
			H3:                        50,
			H4:                        250,
			I1:                        "template-1",
			I2:                        "template-2",
			I3:                        "template-3",
			I4:                        "template-4",
			I5:                        "template-5",
			ContentPaddingAdditionMin: 0,
			ContentPaddingAdditionMax: 32,
			RekeyAfterTimeMin:         110,
			RekeyAfterTimeMax:         130,
			RekeyTimeoutMin:           4,
			RekeyTimeoutMax:           6,
			RejectAfterTimeMin:        175,
			RejectAfterTimeMax:        190,
			KeepaliveTimeoutMin:       9,
			KeepaliveTimeoutMax:       11,
			MaxHandshakeAttemptsMin:   16,
			MaxHandshakeAttemptsMax:   20,
			PersistentKeepaliveMin:    23,
			PersistentKeepaliveMax:    27,
			HealthAddress:             "127.0.0.1:8080",
			UIMode:                    "on_demand",
			OuterPath: &config.OuterPath{
				EndpointExclusion: &config.EndpointExclusionConfig{
					Owner:                    "container",
					RoutingTable:             "main",
					OuterGateway:             "10.99.99.1",
					OuterEgressInterface:     "eth0",
					SourceAddress:            "10.99.99.2",
					EndpointResolutionPolicy: "literal",
					DynamicRenewal:           true,
					RefreshOwner:             "container",
				},
			},
		},
		Secrets: config.Secrets{
			Version:             "1",
			Generation:          gen,
			PrivateKey:          "priv",
			PeerPublicKey:       "peer",
			PresharedKey:        "psk",
			HeaderProtectionKey: "shared-key",
			ControlToken:        "token",
		},
	}
}

type exitProcess struct {
	exitCh  <-chan struct{}
	waitErr error
	dead    bool
	info    ProcessExitInfo
}

func (p *exitProcess) PID() int { return 4242 }

func (p *exitProcess) Kill() error {
	p.dead = true
	return nil
}

func (p *exitProcess) Wait(context.Context) error {
	<-p.exitCh
	return p.waitErr
}

func (p *exitProcess) ExitInfo() ProcessExitInfo { return p.info }

type holdProcess struct {
	done chan struct{}
	pid  int
	dead bool
}

func (p *holdProcess) PID() int { return p.pid }

func (p *holdProcess) Kill() error {
	p.dead = true
	if p.done != nil {
		select {
		case <-p.done:
		default:
			close(p.done)
		}
	}
	return nil
}

func (p *holdProcess) Signal(os.Signal) error { return nil }

func (p *holdProcess) Wait(context.Context) error {
	<-p.done
	return nil
}

type shutdownProcess struct {
	done chan struct{}
	pid  int
}

func (p *shutdownProcess) PID() int { return p.pid }

func (p *shutdownProcess) Kill() error {
	if p.done != nil {
		select {
		case <-p.done:
		default:
			close(p.done)
		}
	}
	return nil
}

func (p *shutdownProcess) Signal(os.Signal) error {
	return p.Kill()
}

func (p *shutdownProcess) Wait(context.Context) error {
	<-p.done
	return nil
}

type shutdownCleanupNetwork struct {
	cleanupCount int
}

func (n *shutdownCleanupNetwork) Apply(context.Context, config.Pair) error { return nil }

func (n *shutdownCleanupNetwork) Ready(context.Context, config.Pair, ProcessHandle) error { return nil }

func (n *shutdownCleanupNetwork) Restore(context.Context, config.Pair) error { return nil }

func (n *shutdownCleanupNetwork) Cleanup(context.Context, config.Pair) error {
	n.cleanupCount++
	return nil
}

type failingNetwork struct {
	applyErr         error
	readyErr         error
	restoreErr       error
	applyCount       int
	failOnApplyCount int
}

func (n *failingNetwork) Apply(context.Context, config.Pair) error {
	n.applyCount++
	if n.applyErr != nil && n.applyCount == n.failOnApplyCount {
		return n.applyErr
	}
	return nil
}

func (n *failingNetwork) Ready(context.Context, config.Pair, ProcessHandle) error { return n.readyErr }

func (n *failingNetwork) Restore(context.Context, config.Pair) error { return n.restoreErr }

type rollbackCancelNetwork struct {
	applyErr          error
	cancelOnApplyErr  context.CancelFunc
	restoreReady      chan struct{}
	restoreCalled     bool
	restoreGeneration string
}

func (n *rollbackCancelNetwork) Apply(ctx context.Context, pair config.Pair) error {
	if n.applyErr != nil && pair.Config.Generation == "gen-2" {
		if n.cancelOnApplyErr != nil {
			n.cancelOnApplyErr()
		}
		return n.applyErr
	}
	return nil
}

func (n *rollbackCancelNetwork) Ready(context.Context, config.Pair, ProcessHandle) error { return nil }

func (n *rollbackCancelNetwork) Restore(ctx context.Context, pair config.Pair) error {
	n.restoreCalled = true
	n.restoreGeneration = pair.Config.Generation
	if n.restoreReady != nil {
		close(n.restoreReady)
	}
	return nil
}

type blockingRollbackNetwork struct {
	applyErr       error
	restoreStarted chan struct{}
	restoreBlocked chan struct{}
}

func (n *blockingRollbackNetwork) Apply(ctx context.Context, pair config.Pair) error {
	if n.applyErr != nil && pair.Config.Generation == "gen-2" {
		return n.applyErr
	}
	return nil
}

func (n *blockingRollbackNetwork) Ready(context.Context, config.Pair, ProcessHandle) error {
	return nil
}

func (n *blockingRollbackNetwork) Restore(ctx context.Context, pair config.Pair) error {
	if n.restoreStarted != nil {
		close(n.restoreStarted)
	}
	select {
	case <-n.restoreBlocked:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type countingNetwork struct {
	applyCount   int
	restoreCount int
	readyCount   int
}

func (n *countingNetwork) Apply(context.Context, config.Pair) error {
	n.applyCount++
	return nil
}

func (n *countingNetwork) Ready(context.Context, config.Pair, ProcessHandle) error {
	n.readyCount++
	return nil
}

func (n *countingNetwork) Restore(context.Context, config.Pair) error {
	n.restoreCount++
	return nil
}
