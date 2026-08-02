//go:build integration && linux && resourcebaseline

package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"awg3routerosgateway/internal/config"
	"awg3routerosgateway/internal/supervisor"
)

type resourceBaselineReport struct {
	Platform string `json:"platform"`
	Go       struct {
		Version string `json:"version"`
		OS      string `json:"os"`
		Arch    string `json:"arch"`
	} `json:"go"`
	Baseline struct {
		RSSKB          int   `json:"rss_kb"`
		PeakRSSKB      int   `json:"peak_rss_kb"`
		Goroutines     int   `json:"goroutines"`
		Threads        int   `json:"threads"`
		FDs            int   `json:"fds"`
		ChildProcesses int   `json:"child_processes"`
		UserMS         int64 `json:"user_ms"`
		SystemMS       int64 `json:"system_ms"`
	} `json:"baseline"`
	ApplyRollback struct {
		Cycles            int `json:"cycles"`
		RSSDeltaKB        int `json:"rss_delta_kb"`
		SteadyRSSKB       int `json:"steady_rss_kb"`
		GoroutineDelta    int `json:"goroutine_delta"`
		ThreadDelta       int `json:"thread_delta"`
		FDDelta           int `json:"fd_delta"`
		ChildProcessDelta int `json:"child_process_delta"`
	} `json:"apply_rollback"`
	UI struct {
		Cycles            int `json:"cycles"`
		GoroutineDelta    int `json:"goroutine_delta"`
		ThreadDelta       int `json:"thread_delta"`
		FDDelta           int `json:"fd_delta"`
		ChildProcessDelta int `json:"child_process_delta"`
	} `json:"ui"`
	RestartLoop struct {
		Observed        bool  `json:"observed"`
		RestartCount    int   `json:"restart_count"`
		ElapsedMS       int64 `json:"elapsed_ms"`
		BackoffBudgetMS int64 `json:"backoff_budget_ms"`
	} `json:"restart_loop"`
}

func TestResourceBaselineValidation(t *testing.T) {
	baseline := sampleSelfMetrics(t)
	beforeChildren := childProcessCount(t)

	basePair := pairForRuntime("gen-baseline")
	candidate := pairForRuntime("gen-candidate")
	candidate.Config.Generation = "gen-candidate"
	candidate.Secrets.Generation = "gen-candidate"

	ctrl := newResourceBaselineController(t, func(pair config.Pair) bool {
		return pair.Config.Generation == "gen-candidate"
	})
	if err := ctrl.Start(context.Background(), basePair); err != nil {
		t.Fatalf("baseline start failed: %v", err)
	}
	startMetrics := sampleSelfMetrics(t)

	for i := 0; i < 100; i++ {
		if err := ctrl.Apply(context.Background(), candidate); err == nil {
			t.Fatalf("expected candidate apply to fail and rollback on cycle %d", i+1)
		}
		status := ctrl.Status()
		if !status.Running || !status.Ready || status.Generation != basePair.Config.Generation || status.Degraded {
			t.Fatalf("unexpected controller status after rollback on cycle %d: %#v", i+1, status)
		}
	}
	applyRollbackMetrics := sampleSelfMetrics(t)
	childrenAfterCycles := childProcessCount(t)

	if err := ctrl.Stop(context.Background()); err != nil {
		t.Fatalf("controller stop after cycles failed: %v", err)
	}
	if err := ctrl.Shutdown(context.Background()); err != nil {
		t.Fatalf("controller shutdown after cycles failed: %v", err)
	}

	sup := newResourceBaselineSupervisor(t)
	uiBaseline := sampleSelfMetrics(t)
	for i := 0; i < 100; i++ {
		if err := sup.OpenUI(context.Background()); err != nil {
			t.Fatalf("open ui failed on cycle %d: %v", i+1, err)
		}
		if err := sup.CloseUI(context.Background()); err != nil {
			t.Fatalf("close ui failed on cycle %d: %v", i+1, err)
		}
	}
	uiMetrics := sampleSelfMetrics(t)
	uiChildren := childProcessCount(t)

	restartElapsed, restartCount := observeRestartLoop(t)
	final := sampleSelfMetrics(t)
	endChildren := childProcessCount(t)

	report := resourceBaselineReport{
		Platform: runtime.GOOS + "/" + runtime.GOARCH,
	}
	report.Go.Version = runtime.Version()
	report.Go.OS = runtime.GOOS
	report.Go.Arch = runtime.GOARCH
	report.Baseline = struct {
		RSSKB          int   `json:"rss_kb"`
		PeakRSSKB      int   `json:"peak_rss_kb"`
		Goroutines     int   `json:"goroutines"`
		Threads        int   `json:"threads"`
		FDs            int   `json:"fds"`
		ChildProcesses int   `json:"child_processes"`
		UserMS         int64 `json:"user_ms"`
		SystemMS       int64 `json:"system_ms"`
	}{
		RSSKB:          baseline.RSSKB,
		PeakRSSKB:      maxInt(baseline.PeakRSSKB, final.PeakRSSKB),
		Goroutines:     baseline.Goroutines,
		Threads:        baseline.Threads,
		FDs:            baseline.FDs,
		ChildProcesses: beforeChildren,
		UserMS:         final.UserMS - baseline.UserMS,
		SystemMS:       final.SystemMS - baseline.SystemMS,
	}
	report.ApplyRollback = struct {
		Cycles            int `json:"cycles"`
		RSSDeltaKB        int `json:"rss_delta_kb"`
		SteadyRSSKB       int `json:"steady_rss_kb"`
		GoroutineDelta    int `json:"goroutine_delta"`
		ThreadDelta       int `json:"thread_delta"`
		FDDelta           int `json:"fd_delta"`
		ChildProcessDelta int `json:"child_process_delta"`
	}{
		Cycles:            100,
		RSSDeltaKB:        absInt(applyRollbackMetrics.RSSKB - startMetrics.RSSKB),
		SteadyRSSKB:       applyRollbackMetrics.RSSKB,
		GoroutineDelta:    absInt(applyRollbackMetrics.Goroutines - startMetrics.Goroutines),
		ThreadDelta:       absInt(applyRollbackMetrics.Threads - startMetrics.Threads),
		FDDelta:           absInt(applyRollbackMetrics.FDs - startMetrics.FDs),
		ChildProcessDelta: childrenAfterCycles - beforeChildren,
	}
	report.UI = struct {
		Cycles            int `json:"cycles"`
		GoroutineDelta    int `json:"goroutine_delta"`
		ThreadDelta       int `json:"thread_delta"`
		FDDelta           int `json:"fd_delta"`
		ChildProcessDelta int `json:"child_process_delta"`
	}{
		Cycles:            100,
		GoroutineDelta:    uiMetrics.Goroutines - uiBaseline.Goroutines,
		ThreadDelta:       uiMetrics.Threads - uiBaseline.Threads,
		FDDelta:           uiMetrics.FDs - uiBaseline.FDs,
		ChildProcessDelta: uiChildren - beforeChildren,
	}
	report.RestartLoop.Observed = restartCount > 0
	report.RestartLoop.RestartCount = restartCount
	report.RestartLoop.ElapsedMS = restartElapsed.Milliseconds()
	report.RestartLoop.BackoffBudgetMS = 25

	if out := os.Getenv("AWG3_RESOURCE_BASELINE_OUT"); out != "" {
		if err := writeResourceBaselineReport(out, report); err != nil {
			t.Fatalf("write baseline report failed: %v", err)
		}
	}

	if report.ApplyRollback.RSSDeltaKB > 4096 {
		t.Fatalf("apply/rollback RSS delta too high: %+v", report.ApplyRollback)
	}
	if absInt(report.UI.GoroutineDelta) > 2 {
		t.Fatalf("ui goroutine delta too high: %+v", report.UI)
	}
	if report.RestartLoop.RestartCount == 0 || report.RestartLoop.ElapsedMS < report.RestartLoop.BackoffBudgetMS {
		t.Fatalf("restart loop was not observably backoff-bounded: %+v", report.RestartLoop)
	}

	_ = final
}

func newResourceBaselineController(t *testing.T, candidateShouldFail func(config.Pair) bool) *Controller {
	t.Helper()
	start := func(ctx context.Context, pair config.Pair) (Process, error) {
		cmd := helperCommand(ctx, "hold", "")
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return newExecProcess(cmd), nil
	}
	network := &baselineNetwork{candidateShouldFail: candidateShouldFail}
	ctrl, err := New(start, nil, network, func(ctx context.Context, proc Process) error {
		if sig, ok := proc.(interface{ Signal(os.Signal) error }); ok {
			if err := sig.Signal(syscall.SIGTERM); err == nil {
				return nil
			}
		}
		return proc.Kill()
	})
	if err != nil {
		t.Fatalf("new controller failed: %v", err)
	}
	ctrl.recovery.ApplyTimeout = 250 * time.Millisecond
	ctrl.recovery.ReadinessTimeout = 250 * time.Millisecond
	ctrl.recovery.RollbackTimeout = 250 * time.Millisecond
	ctrl.recovery.InitialBackoff = 25 * time.Millisecond
	ctrl.recovery.MaxBackoff = 50 * time.Millisecond
	ctrl.recovery.MaxAttempts = 3
	return ctrl
}

type baselineNetwork struct {
	candidateShouldFail func(config.Pair) bool
}

func (n *baselineNetwork) Apply(_ context.Context, pair config.Pair) error {
	if n.candidateShouldFail != nil && n.candidateShouldFail(pair) {
		return errors.New("candidate apply failed")
	}
	return nil
}

func (n *baselineNetwork) Ready(context.Context, config.Pair, ProcessHandle) error {
	return nil
}

func (n *baselineNetwork) Restore(context.Context, config.Pair) error {
	return nil
}

func newResourceBaselineSupervisor(t *testing.T) *supervisor.Supervisor {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "awg3.json")
	secretsPath := filepath.Join(dir, "secrets.json")
	pair := pairForRuntime("gen-ui")
	if err := config.SavePair(configPath, secretsPath, pair); err != nil {
		t.Fatalf("seed save failed: %v", err)
	}
	sup, err := supervisor.New(supervisor.Options{
		ConfigPath:     configPath,
		SecretsPath:    secretsPath,
		StatusAddr:     "127.0.0.1:0",
		ConfigAddr:     "127.0.0.1:0",
		Mode:           "run",
		SessionTTL:     20 * time.Millisecond,
		SessionIdleTTL: 20 * time.Millisecond,
		SessionMaxTTL:  200 * time.Millisecond,
		Clock:          time.Now,
		ConfigLoader:   config.LoadPair,
		ConfigSaver:    config.SavePair,
	})
	if err != nil {
		t.Fatalf("new supervisor failed: %v", err)
	}
	return sup
}

type selfMetrics struct {
	RSSKB      int
	PeakRSSKB  int
	Goroutines int
	Threads    int
	FDs        int
	UserMS     int64
	SystemMS   int64
}

func sampleSelfMetrics(t *testing.T) selfMetrics {
	t.Helper()
	rssKB, peakRSSKB, threads := readProcStatusMetrics(t)
	userMS, systemMS := readSelfCPUTime(t)
	return selfMetrics{
		RSSKB:      rssKB,
		PeakRSSKB:  peakRSSKB,
		Goroutines: runtime.NumGoroutine(),
		Threads:    threads,
		FDs:        fdCount(t),
		UserMS:     userMS,
		SystemMS:   systemMS,
	}
}

func readProcStatusMetrics(t *testing.T) (rssKB, peakRSSKB, threads int) {
	t.Helper()
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatalf("read status failed: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "VmRSS:"):
			rssKB = parseStatusKB(line)
		case strings.HasPrefix(line, "VmHWM:"):
			peakRSSKB = parseStatusKB(line)
		case strings.HasPrefix(line, "Threads:"):
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				threads, _ = strconv.Atoi(fields[1])
			}
		}
	}
	return rssKB, peakRSSKB, threads
}

func parseStatusKB(line string) int {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	value, _ := strconv.Atoi(fields[1])
	return value
}

func readSelfCPUTime(t *testing.T) (userMS, systemMS int64) {
	t.Helper()
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		t.Fatalf("getrusage failed: %v", err)
	}
	userMS = usage.Utime.Sec*1000 + int64(usage.Utime.Usec)/1000
	systemMS = usage.Stime.Sec*1000 + int64(usage.Stime.Usec)/1000
	return userMS, systemMS
}

func fdCount(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatalf("read fd dir failed: %v", err)
	}
	return len(entries)
}

func childProcessCount(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("ps", "-o", "pid=", "--ppid", strconv.Itoa(os.Getpid()))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("child process count failed: %v", err)
	}
	count := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func observeRestartLoop(t *testing.T) (time.Duration, int) {
	t.Helper()
	var startCount int
	ctrl, err := New(func(ctx context.Context, _ config.Pair) (Process, error) {
		startCount++
		mode := "hold"
		if startCount == 1 {
			mode = "exit0"
		}
		cmd := helperCommand(ctx, mode, "")
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return newExecProcess(cmd), nil
	}, nil, nil, func(context.Context, Process) error { return nil })
	if err != nil {
		t.Fatalf("new restart controller failed: %v", err)
	}
	ctrl.recovery.InitialBackoff = 25 * time.Millisecond
	ctrl.recovery.MaxBackoff = 50 * time.Millisecond
	ctrl.recovery.MaxAttempts = 2
	started := time.Now()
	if err := ctrl.Start(context.Background(), pairForRuntime("gen-restart")); err != nil {
		t.Fatalf("restart controller start failed: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status := ctrl.Status()
		if status.RestartCount > 0 && status.Running && status.Ready && !status.Degraded {
			if err := ctrl.Stop(context.Background()); err != nil {
				t.Fatalf("restart controller stop failed: %v", err)
			}
			_ = ctrl.Shutdown(context.Background())
			return time.Since(started), status.RestartCount
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = ctrl.Stop(context.Background())
	_ = ctrl.Shutdown(context.Background())
	t.Fatalf("restart loop did not recover in time: %#v", ctrl.Status())
	return 0, 0
}

func writeResourceBaselineReport(path string, report resourceBaselineReport) error {
	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o644)
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
