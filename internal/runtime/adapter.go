package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"awg3routerosgateway/internal/awg3profile"
	"awg3routerosgateway/internal/config"
)

type CommandFactory interface {
	CommandContext(context.Context, string, ...string) *exec.Cmd
}

type ProcessHandle interface {
	Process
	Wait(context.Context) error
	Signal(os.Signal) error
}

type ProcessExitInfo struct {
	Category string
	Code     int
	Signal   string
}

type ExitReporter interface {
	ExitInfo() ProcessExitInfo
}

type RuntimeProbe interface {
	Ready(context.Context, config.Pair, ProcessHandle) error
}

type NetworkAdapter interface {
	Apply(context.Context, config.Pair) error
	Ready(context.Context, config.Pair, ProcessHandle) error
	Restore(context.Context, config.Pair) error
}

type ProfilePreflight interface {
	Check(context.Context, config.Pair) error
}

type EndpointExclusionAdapter interface {
	Apply(context.Context, config.Pair) error
	Ready(context.Context, config.Pair) error
	Restore(context.Context, config.Pair) error
}

type Clock interface {
	Now() time.Time
	NewTimer(time.Duration) Timer
}

type Timer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(time.Duration) bool
}

type ExecOptions struct {
	Binary                 string
	ToolsBinary            string
	IPBinary               string
	SysctlBinary           string
	Args                   []string
	DebugInterfaceOverride string
	Stdout                 io.Writer
	Stderr                 io.Writer
	StdoutLimit            int
	StartTimeout           time.Duration
	ParserTimeout          time.Duration
	SetconfTimeout         time.Duration
	InterfaceTimeout       time.Duration
	RouteTimeout           time.Duration
	ForwardingTimeout      time.Duration
	ApplyTimeout           time.Duration
	ReadinessTimeout       time.Duration
	RestoreTimeout         time.Duration
	StopTimeout            time.Duration
	KillTimeout            time.Duration
	CommandFactory         CommandFactory
	Preflight              ProfilePreflight
	EndpointExclusion      EndpointExclusionAdapter
	Clock                  Clock
}

type ExecAdapter struct {
	opts ExecOptions

	stateMu            sync.Mutex
	forwardingBaseline map[string]string
	forwardingChanged  map[string]bool
}

func NewExecAdapter(opts ExecOptions) (*ExecAdapter, error) {
	if opts.Binary == "" {
		return nil, errors.New("binary is required")
	}
	if opts.ToolsBinary == "" {
		opts.ToolsBinary = "awg"
	}
	if opts.IPBinary == "" {
		opts.IPBinary = "ip"
	}
	if opts.SysctlBinary == "" {
		opts.SysctlBinary = "sysctl"
	}
	if opts.CommandFactory == nil {
		opts.CommandFactory = osCommandFactory{}
	}
	if opts.StartTimeout <= 0 {
		opts.StartTimeout = 10 * time.Second
	}
	if opts.ParserTimeout <= 0 {
		opts.ParserTimeout = 5 * time.Second
	}
	if opts.SetconfTimeout <= 0 {
		opts.SetconfTimeout = 10 * time.Second
	}
	if opts.InterfaceTimeout <= 0 {
		opts.InterfaceTimeout = 10 * time.Second
	}
	if opts.RouteTimeout <= 0 {
		opts.RouteTimeout = 10 * time.Second
	}
	if opts.ForwardingTimeout <= 0 {
		opts.ForwardingTimeout = 5 * time.Second
	}
	if opts.ApplyTimeout <= 0 {
		opts.ApplyTimeout = 30 * time.Second
	}
	if opts.ReadinessTimeout <= 0 {
		opts.ReadinessTimeout = 15 * time.Second
	}
	if opts.RestoreTimeout <= 0 {
		opts.RestoreTimeout = 30 * time.Second
	}
	if opts.StopTimeout <= 0 {
		opts.StopTimeout = 5 * time.Second
	}
	if opts.KillTimeout <= 0 {
		opts.KillTimeout = 2 * time.Second
	}
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	return &ExecAdapter{opts: opts}, nil
}

func (a *ExecAdapter) Start(ctx context.Context, pair config.Pair) (Process, error) {
	if err := pair.Validate(); err != nil {
		return nil, err
	}
	interfaceName := pair.Config.InterfaceName
	if a.opts.DebugInterfaceOverride != "" && a.opts.DebugInterfaceOverride != interfaceName {
		return nil, fmt.Errorf("debug interface override %q does not match canonical interface_name %q", a.opts.DebugInterfaceOverride, interfaceName)
	}
	args := append([]string{}, a.opts.Args...)
	args = append(args, interfaceName)
	// The controller owns the child lifetime. Request or signal-context
	// cancellation must not let os/exec kill the child before controlled Stop
	// has completed network cleanup.
	cmd := a.opts.CommandFactory.CommandContext(context.WithoutCancel(ctx), a.opts.Binary, args...)
	secretValues := []string{
		pair.Secrets.PrivateKey,
		pair.Secrets.PeerPublicKey,
		pair.Secrets.PresharedKey,
		pair.Secrets.HeaderProtectionKey,
		pair.Secrets.ControlToken,
	}
	cmd.Stdout = &boundedWriter{dst: a.opts.Stdout, limit: a.opts.StdoutLimit, redactValues: secretValues}
	cmd.Stderr = &boundedWriter{dst: a.opts.Stderr, limit: a.opts.StdoutLimit, redactValues: secretValues}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return newExecProcess(cmd), nil
}

func (a *ExecAdapter) Probe(ctx context.Context, pair config.Pair, proc ProcessHandle) error {
	if a.opts.Preflight == nil {
		return errors.New("preflight adapter is required")
	}
	return a.opts.Preflight.Check(ctx, pair)
}

func (a *ExecAdapter) Stop(ctx context.Context, proc ProcessHandle) error {
	if proc == nil {
		return nil
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		_ = proc.Kill()
		return err
	}
	stopTimer := time.NewTimer(a.opts.StopTimeout)
	defer stopTimer.Stop()
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- proc.Wait(ctx)
	}()
	select {
	case err := <-waitCh:
		return err
	case <-ctx.Done():
		_ = proc.Kill()
		return ctx.Err()
	case <-stopTimer.C:
		_ = proc.Kill()
		killTimer := time.NewTimer(a.opts.KillTimeout)
		defer killTimer.Stop()
		select {
		case err := <-waitCh:
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-killTimer.C:
			_ = proc.Kill()
			return context.DeadlineExceeded
		}
	}
}

func (a *ExecAdapter) Apply(ctx context.Context, pair config.Pair) error {
	return a.configureRuntime(ctx, pair, false)
}

func (a *ExecAdapter) Ready(ctx context.Context, pair config.Pair, proc ProcessHandle) error {
	if proc == nil {
		return errors.New("process is required")
	}
	if proc.PID() <= 0 {
		return errors.New("process pid is invalid")
	}
	readyCtx, cancel := a.stepContext(ctx, a.opts.ReadinessTimeout)
	defer cancel()
	return a.verifyRuntimeState(readyCtx, pair)
}

func (a *ExecAdapter) Restore(ctx context.Context, pair config.Pair) error {
	return a.configureRuntime(ctx, pair, true)
}

func (a *ExecAdapter) Cleanup(ctx context.Context, pair config.Pair) error {
	var cleanupErr error
	if cleaner, ok := a.opts.EndpointExclusion.(interface {
		Cleanup(context.Context, config.Pair) error
	}); ok {
		if err := cleaner.Cleanup(ctx, pair); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}

	a.stateMu.Lock()
	baseline := make(map[string]string, len(a.forwardingBaseline))
	changed := make(map[string]bool, len(a.forwardingChanged))
	for key, value := range a.forwardingBaseline {
		baseline[key] = value
	}
	for key, value := range a.forwardingChanged {
		changed[key] = value
	}
	a.stateMu.Unlock()

	for _, key := range []string{"net.ipv4.ip_forward", "net.ipv6.conf.all.forwarding"} {
		if !changed[key] {
			continue
		}
		if err := a.runTool(ctx, a.opts.SysctlBinary, a.opts.ForwardingTimeout, "-w", key+"="+baseline[key]); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	if cleanupErr != nil {
		return cleanupErr
	}

	a.stateMu.Lock()
	a.forwardingBaseline = nil
	a.forwardingChanged = nil
	a.stateMu.Unlock()
	return nil
}

type osCommandFactory struct{}

func (osCommandFactory) CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

type execProcess struct {
	cmd      *exec.Cmd
	waitOnce sync.Once
	waitDone chan struct{}
	waitErr  error
	exitInfo ProcessExitInfo
}

func newExecProcess(cmd *exec.Cmd) *execProcess {
	return &execProcess{
		cmd:      cmd,
		waitDone: make(chan struct{}),
	}
}

func (p *execProcess) PID() int {
	if p.cmd != nil && p.cmd.Process != nil {
		return p.cmd.Process.Pid
	}
	return 0
}

func (p *execProcess) Kill() error {
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

func (p *execProcess) Signal(sig os.Signal) error {
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Signal(sig)
}

func (p *execProcess) Wait(ctx context.Context) error {
	p.waitOnce.Do(func() {
		go func() {
			p.waitErr = p.cmd.Wait()
			p.exitInfo = captureExitInfo(p.waitErr)
			close(p.waitDone)
		}()
	})
	select {
	case <-p.waitDone:
		return p.waitErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *execProcess) ExitInfo() ProcessExitInfo {
	return p.exitInfo
}

func captureExitInfo(err error) ProcessExitInfo {
	info := ProcessExitInfo{Category: "clean_exit", Code: 0}
	if err == nil {
		return info
	}
	info.Category = "unknown_exit"
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ProcessState != nil {
		if ws, ok := exitErr.ProcessState.Sys().(syscall.WaitStatus); ok {
			switch {
			case ws.Signaled():
				info.Category = "signal"
				info.Signal = ws.Signal().String()
				info.Code = 128 + int(ws.Signal())
			case ws.Exited():
				info.Category = "exit"
				info.Code = ws.ExitStatus()
			}
		}
	}
	return info
}

func (a *ExecAdapter) configureRuntime(ctx context.Context, pair config.Pair, restore bool) error {
	if err := pair.Validate(); err != nil {
		return err
	}
	if a.opts.Preflight != nil {
		preflightCtx, cancel := a.stepContext(ctx, a.opts.ParserTimeout)
		defer cancel()
		if err := a.opts.Preflight.Check(preflightCtx, pair); err != nil {
			return err
		}
	}
	rendered, err := a.renderConfig(pair)
	if err != nil {
		return err
	}
	tmpPath, cleanup, err := a.writeTempConfig(rendered)
	if err != nil {
		return err
	}
	defer cleanup()

	if err := a.waitForInterface(ctx, pair.Config.InterfaceName); err != nil {
		return err
	}
	if err := a.runTool(ctx, a.opts.ToolsBinary, a.opts.SetconfTimeout, "setconf", pair.Config.InterfaceName, tmpPath); err != nil {
		return err
	}
	if err := a.captureForwardingBaseline(ctx); err != nil {
		return err
	}
	if err := a.applyInterfaceConfig(ctx, pair); err != nil {
		return err
	}
	if restore {
		if a.opts.EndpointExclusion != nil {
			if err := a.opts.EndpointExclusion.Restore(ctx, pair); err != nil {
				return err
			}
		}
	} else if a.opts.EndpointExclusion != nil {
		if err := a.opts.EndpointExclusion.Apply(ctx, pair); err != nil {
			return err
		}
	}
	if err := a.verifyRuntimeState(ctx, pair); err != nil {
		return err
	}
	return nil
}

func (a *ExecAdapter) verifyRuntimeState(ctx context.Context, pair config.Pair) error {
	if err := a.verifyAddress(ctx, pair); err != nil {
		return err
	}
	if err := a.verifyMTU(ctx, pair); err != nil {
		return err
	}
	if err := a.verifyRoutes(ctx, pair); err != nil {
		return err
	}
	if err := a.verifyForwarding(ctx, pair); err != nil {
		return err
	}
	if err := a.verifyShowconf(ctx, pair); err != nil {
		return err
	}
	if err := a.verifyDump(ctx, pair); err != nil {
		return err
	}
	if a.opts.EndpointExclusion != nil {
		if err := a.opts.EndpointExclusion.Ready(ctx, pair); err != nil {
			return err
		}
	}
	return nil
}

func (a *ExecAdapter) renderConfig(pair config.Pair) (string, error) {
	return awg3profile.RenderSetconf(pair)
}

func (a *ExecAdapter) writeTempConfig(rendered string) (string, func(), error) {
	tmp, err := os.CreateTemp("", "awg3-*.conf")
	if err != nil {
		return "", func() {}, err
	}
	if err := tmp.Chmod(0o600); err != nil {
		name := tmp.Name()
		_ = tmp.Close()
		_ = os.Remove(name)
		return "", func() {}, err
	}
	if _, err := tmp.WriteString(rendered); err != nil {
		name := tmp.Name()
		_ = tmp.Close()
		_ = os.Remove(name)
		return "", func() {}, err
	}
	if err := tmp.Close(); err != nil {
		name := tmp.Name()
		_ = os.Remove(name)
		return "", func() {}, err
	}
	return tmp.Name(), func() { _ = os.Remove(tmp.Name()) }, nil
}

func (a *ExecAdapter) runTool(ctx context.Context, binary string, timeout time.Duration, args ...string) error {
	if binary == "" {
		return errors.New("binary is required")
	}
	stepCtx, cancel := a.stepContext(ctx, timeout)
	defer cancel()
	cmd := a.opts.CommandFactory.CommandContext(stepCtx, binary, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s failed: %w", binary, strings.Join(args, " "), err)
	}
	return nil
}

func (a *ExecAdapter) commandOutput(ctx context.Context, binary string, timeout time.Duration, args ...string) (string, error) {
	if binary == "" {
		return "", errors.New("binary is required")
	}
	stepCtx, cancel := a.stepContext(ctx, timeout)
	defer cancel()
	cmd := a.opts.CommandFactory.CommandContext(stepCtx, binary, args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s failed: %w", binary, strings.Join(args, " "), err)
	}
	return stdout.String(), nil
}

func (a *ExecAdapter) waitForInterface(ctx context.Context, interfaceName string) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(a.opts.InterfaceTimeout)
	}
	for {
		if err := a.runTool(ctx, a.opts.IPBinary, a.opts.InterfaceTimeout, "link", "show", "dev", interfaceName); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("interface %q did not appear before timeout", interfaceName)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (a *ExecAdapter) applyInterfaceConfig(ctx context.Context, pair config.Pair) error {
	if err := a.runTool(ctx, a.opts.IPBinary, a.opts.RouteTimeout, "addr", "replace", pair.Config.TunnelAddress, "dev", pair.Config.InterfaceName); err != nil {
		return err
	}
	if err := a.runTool(ctx, a.opts.IPBinary, a.opts.RouteTimeout, "link", "set", "dev", pair.Config.InterfaceName, "mtu", strconv.Itoa(pair.Config.MTU), "up"); err != nil {
		return err
	}
	for _, route := range pair.Config.AllowedIPs {
		if route == "" {
			continue
		}
		if err := a.runTool(ctx, a.opts.IPBinary, a.opts.RouteTimeout, "route", "replace", route, "dev", pair.Config.InterfaceName); err != nil {
			return err
		}
	}
	if err := a.runTool(ctx, a.opts.SysctlBinary, a.opts.ForwardingTimeout, "-w", "net.ipv4.ip_forward=1"); err != nil {
		return err
	}
	a.markForwardingChanged("net.ipv4.ip_forward")
	if err := a.runTool(ctx, a.opts.SysctlBinary, a.opts.ForwardingTimeout, "-w", "net.ipv6.conf.all.forwarding=1"); err != nil {
		return err
	}
	a.markForwardingChanged("net.ipv6.conf.all.forwarding")
	return nil
}

func (a *ExecAdapter) captureForwardingBaseline(ctx context.Context) error {
	a.stateMu.Lock()
	if a.forwardingBaseline != nil {
		a.stateMu.Unlock()
		return nil
	}
	a.stateMu.Unlock()

	baseline := make(map[string]string, 2)
	for _, key := range []string{"net.ipv4.ip_forward", "net.ipv6.conf.all.forwarding"} {
		out, err := a.commandOutput(ctx, a.opts.SysctlBinary, a.opts.ForwardingTimeout, "-n", key)
		if err != nil {
			return err
		}
		value := strings.TrimSpace(out)
		if value != "0" && value != "1" {
			return fmt.Errorf("invalid forwarding baseline for %s", key)
		}
		baseline[key] = value
	}

	a.stateMu.Lock()
	if a.forwardingBaseline == nil {
		a.forwardingBaseline = baseline
		a.forwardingChanged = make(map[string]bool, 2)
	}
	a.stateMu.Unlock()
	return nil
}

func (a *ExecAdapter) markForwardingChanged(key string) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if a.forwardingChanged == nil {
		a.forwardingChanged = make(map[string]bool, 2)
	}
	a.forwardingChanged[key] = true
}

func (a *ExecAdapter) verifyAddress(ctx context.Context, pair config.Pair) error {
	out, err := a.commandOutput(ctx, a.opts.IPBinary, a.opts.ReadinessTimeout, "addr", "show", "dev", pair.Config.InterfaceName)
	if err != nil {
		return err
	}
	if !strings.Contains(out, pair.Config.TunnelAddress) {
		return fmt.Errorf("tunnel address mismatch")
	}
	return nil
}

func (a *ExecAdapter) verifyMTU(ctx context.Context, pair config.Pair) error {
	out, err := a.commandOutput(ctx, a.opts.IPBinary, a.opts.ReadinessTimeout, "link", "show", "dev", pair.Config.InterfaceName)
	if err != nil {
		return err
	}
	want := fmt.Sprintf("mtu %d", pair.Config.MTU)
	if !strings.Contains(out, want) {
		return errors.New("mtu mismatch")
	}
	return nil
}

func (a *ExecAdapter) verifyRoutes(ctx context.Context, pair config.Pair) error {
	out, err := a.commandOutput(ctx, a.opts.IPBinary, a.opts.ReadinessTimeout, "route", "show", "dev", pair.Config.InterfaceName)
	if err != nil {
		return err
	}
	for _, route := range pair.Config.AllowedIPs {
		if route == "" {
			continue
		}
		if !strings.Contains(out, route) {
			return fmt.Errorf("route mismatch for %q", route)
		}
	}
	return nil
}

func (a *ExecAdapter) verifyForwarding(ctx context.Context, pair config.Pair) error {
	_ = pair
	out, err := a.commandOutput(ctx, a.opts.SysctlBinary, a.opts.ReadinessTimeout, "-n", "net.ipv4.ip_forward")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "1" {
		return errors.New("ipv4 forwarding mismatch")
	}
	return nil
}

func (a *ExecAdapter) verifyShowconf(ctx context.Context, pair config.Pair) error {
	out, err := a.commandOutput(ctx, a.opts.ToolsBinary, a.opts.ReadinessTimeout, "showconf", pair.Config.InterfaceName)
	if err != nil {
		return err
	}
	return compareRuntimeShowconf(pair, out)
}

func (a *ExecAdapter) verifyDump(ctx context.Context, pair config.Pair) error {
	out, err := a.commandOutput(ctx, a.opts.ToolsBinary, a.opts.ReadinessTimeout, "show", pair.Config.InterfaceName, "dump")
	if err != nil {
		return err
	}
	if err := compareRuntimeDump(pair, out); err != nil {
		return err
	}
	return nil
}

func compareRuntimeShowconf(pair config.Pair, out string) error {
	got := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[") {
			continue
		}
		key, value, ok := strings.Cut(line, " = ")
		if !ok {
			continue
		}
		got[key] = value
	}
	expect := map[string]string{
		"PrivateKey":             pair.Secrets.PrivateKey,
		"ListenPort":             strconv.Itoa(pair.Config.ListenPort),
		"Jc":                     strconv.Itoa(pair.Config.Jc),
		"Jmin":                   strconv.Itoa(pair.Config.Jmin),
		"Jmax":                   strconv.Itoa(pair.Config.Jmax),
		"S1":                     strconv.Itoa(pair.Config.S1),
		"S2":                     strconv.Itoa(pair.Config.S2),
		"S3":                     strconv.Itoa(pair.Config.S3),
		"S4":                     strconv.Itoa(pair.Config.S4),
		"H1":                     strconv.Itoa(pair.Config.H1),
		"H2":                     strconv.Itoa(pair.Config.H2),
		"H3":                     strconv.Itoa(pair.Config.H3),
		"H4":                     strconv.Itoa(pair.Config.H4),
		"I1":                     pair.Config.I1,
		"I2":                     pair.Config.I2,
		"I3":                     pair.Config.I3,
		"I4":                     pair.Config.I4,
		"I5":                     pair.Config.I5,
		"HeaderProtectionKey":    pair.Secrets.HeaderProtectionKey,
		"ContentPaddingAddition": runtimeFormatRange(pair.Config.ContentPaddingAdditionMin, pair.Config.ContentPaddingAdditionMax),
		"RekeyAfterTime":         runtimeFormatRange(pair.Config.RekeyAfterTimeMin, pair.Config.RekeyAfterTimeMax),
		"RekeyTimeout":           runtimeFormatRange(pair.Config.RekeyTimeoutMin, pair.Config.RekeyTimeoutMax),
		"RejectAfterTime":        runtimeFormatRange(pair.Config.RejectAfterTimeMin, pair.Config.RejectAfterTimeMax),
		"KeepaliveTimeout":       runtimeFormatRange(pair.Config.KeepaliveTimeoutMin, pair.Config.KeepaliveTimeoutMax),
		"MaxHandshakeAttempts":   runtimeFormatRange(pair.Config.MaxHandshakeAttemptsMin, pair.Config.MaxHandshakeAttemptsMax),
		"PublicKey":              pair.Secrets.PeerPublicKey,
		"PresharedKey":           pair.Secrets.PresharedKey,
		"AllowedIPs":             strings.Join(pair.Config.AllowedIPs, ", "),
		"Endpoint":               pair.Config.Endpoint,
		"PersistentKeepalive":    runtimeFormatRange(pair.Config.PersistentKeepaliveMin, pair.Config.PersistentKeepaliveMax),
	}
	for key, want := range expect {
		if got[key] != want {
			return fmt.Errorf("showconf mismatch for %s", key)
		}
	}
	return nil
}

func compareRuntimeDump(pair config.Pair, out string) error {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 {
		return errors.New("dump output empty")
	}
	fields := strings.Split(lines[0], "\t")
	if len(fields) < 27 {
		return errors.New("dump output missing fields")
	}
	if fields[0] != pair.Secrets.PrivateKey {
		return errors.New("dump private key mismatch")
	}
	if fields[1] != "(none)" {
		return errors.New("dump interface public key mismatch")
	}
	if fields[2] != strconv.Itoa(pair.Config.ListenPort) {
		return errors.New("dump listen port mismatch")
	}
	if fields[3] != strconv.Itoa(pair.Config.Jc) {
		return errors.New("dump jc mismatch")
	}
	if fields[14] != pair.Config.I1 {
		return errors.New("dump i1 mismatch")
	}
	if fields[18] != pair.Config.I5 {
		return errors.New("dump i5 mismatch")
	}
	if fields[19] != pair.Secrets.HeaderProtectionKey {
		return errors.New("dump header protection key mismatch")
	}
	if len(lines) < 2 {
		return errors.New("dump peer line missing")
	}
	peerFields := strings.Split(lines[1], "\t")
	if len(peerFields) < 4 {
		return errors.New("dump peer fields missing")
	}
	if peerFields[0] != pair.Secrets.PeerPublicKey {
		return errors.New("dump peer public key mismatch")
	}
	if peerFields[1] != pair.Secrets.PresharedKey {
		return errors.New("dump peer preshared key mismatch")
	}
	if peerFields[2] != pair.Config.Endpoint {
		return errors.New("dump endpoint mismatch")
	}
	if peerFields[3] != strings.Join(pair.Config.AllowedIPs, ",") {
		return errors.New("dump allowed ips mismatch")
	}
	return nil
}

func runtimeFormatRange(min, max int) string {
	if min == max {
		return strconv.Itoa(min)
	}
	return fmt.Sprintf("%d-%d", min, max)
}

func (a *ExecAdapter) stepContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

type boundedWriter struct {
	dst          io.Writer
	limit        int
	redactValues []string
	mu           sync.Mutex
	n            int
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	if w.dst == nil || w.limit <= 0 {
		return len(p), nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	remain := w.limit - w.n
	if remain <= 0 {
		return len(p), nil
	}
	if len(p) > remain {
		p = p[:remain]
	}
	n, err := w.dst.Write(redactOutput(p, w.redactValues))
	w.n += n
	return len(p), err
}

func redactOutput(p []byte, secretValues []string) []byte {
	s := string(p)
	for _, token := range secretValues {
		if token == "" {
			continue
		}
		s = strings.ReplaceAll(s, token, "[redacted]")
	}
	return []byte(s)
}

var _ ProcessHandle = (*execProcess)(nil)
