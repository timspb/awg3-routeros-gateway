package runtime

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"awg3routerosgateway/internal/config"
)

const (
	helperEnv       = "AWG3_TEST_HELPER"
	helperModeEnv   = "AWG3_TEST_HELPER_MODE"
	helperOutputEnv = "AWG3_TEST_HELPER_OUTPUT"
)

func TestExecAdapterProbeRequiresPreflightAdapter(t *testing.T) {
	adapter, err := NewExecAdapter(ExecOptions{Binary: "ignored"})
	if err != nil {
		t.Fatalf("new exec adapter failed: %v", err)
	}
	err = adapter.Probe(context.Background(), pairForRuntime("gen-probe"), &stubProcessHandle{})
	if err == nil {
		t.Fatalf("expected probe to fail without preflight adapter")
	}
	if got := err.Error(); got != "preflight adapter is required" {
		t.Fatalf("unexpected probe error: %q", got)
	}
}

func TestExecAdapterRejectsMismatchedDebugInterfaceOverride(t *testing.T) {
	adapter, err := NewExecAdapter(ExecOptions{
		Binary:                 "ignored",
		DebugInterfaceOverride: "debug0",
		CommandFactory:         &captureCommandFactory{},
	})
	if err != nil {
		t.Fatalf("new exec adapter failed: %v", err)
	}
	_, err = adapter.Start(context.Background(), pairForRuntime("gen-override"))
	if err == nil {
		t.Fatalf("expected start to fail on override mismatch")
	}
}

func TestExecAdapterUsesCanonicalInterfaceName(t *testing.T) {
	factory := &captureCommandFactory{}
	adapter, err := NewExecAdapter(ExecOptions{
		Binary:         "ignored",
		CommandFactory: factory,
		StdoutLimit:    1024,
		Stdout:         os.Stdout,
		Stderr:         os.Stderr,
	})
	if err != nil {
		t.Fatalf("new exec adapter failed: %v", err)
	}
	proc, err := adapter.Start(context.Background(), pairForRuntime("gen-canonical"))
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if factory.name != "ignored" {
		t.Fatalf("expected binary name to be forwarded, got %q", factory.name)
	}
	if got := factory.args[len(factory.args)-1]; got != "awg3" {
		t.Fatalf("expected canonical interface name in argv, got %q", got)
	}
	if handle, ok := proc.(ProcessHandle); ok {
		if err := handle.Wait(context.Background()); err != nil {
			t.Fatalf("wait failed: %v", err)
		}
	}
}

func TestBoundedWriterRedactsSecretValues(t *testing.T) {
	var buf bytes.Buffer
	w := &boundedWriter{
		dst:          &buf,
		limit:        1024,
		redactValues: []string{"priv", "peer", "psk", "shared-key", "token"},
	}
	if _, err := w.Write([]byte("priv peer psk shared-key token")); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	got := buf.String()
	for _, secret := range []string{"priv", "peer", "psk", "shared-key", "token"} {
		if strings.Contains(got, secret) {
			t.Fatalf("secret %q leaked in output: %q", secret, got)
		}
	}
	if !strings.Contains(got, "[redacted]") {
		t.Fatalf("expected redacted marker in output: %q", got)
	}
}

func TestExecAdapterApplyReadyAndRestoreUseOfficialTooling(t *testing.T) {
	pair := pairForRuntime("gen-apply")
	factory := &scriptedCommandFactory{pair: pair}
	exclusion := NewRouteEndpointExclusionAdapter()
	exclusion.CommandFactory = factory
	adapter, err := NewExecAdapter(ExecOptions{
		Binary:            "ignored",
		ToolsBinary:       "awg",
		IPBinary:          "ip",
		SysctlBinary:      "sysctl",
		CommandFactory:    factory,
		Preflight:         NewCanonicalProfilePreflightAdapter(),
		EndpointExclusion: exclusion,
		StdoutLimit:       1024,
	})
	if err != nil {
		t.Fatalf("new exec adapter failed: %v", err)
	}
	proc, err := adapter.Start(context.Background(), pair)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	handle, ok := proc.(ProcessHandle)
	if !ok {
		t.Fatalf("expected process handle")
	}
	if err := adapter.Apply(context.Background(), pair); err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if err := adapter.Ready(context.Background(), pair, handle); err != nil {
		t.Fatalf("ready failed: %v", err)
	}
	if err := adapter.Restore(context.Background(), pair); err != nil {
		t.Fatalf("restore failed: %v", err)
	}
	gotCalls := strings.Join(factory.calls, "\n")
	for _, want := range []string{
		"awg setconf awg3",
		"awg showconf awg3",
		"awg show awg3 dump",
		"ip addr replace 10.99.99.2/30 dev awg3",
		"ip link set dev awg3 mtu 1380 up",
		"ip route replace 10.99.99.0/24 dev awg3",
		"ip route replace 213.176.116.165/32 via 10.99.99.1 dev eth0 table main src 10.99.99.2",
		"ip route get 213.176.116.165 table main from 10.99.99.2",
		"sysctl -w net.ipv4.ip_forward=1",
	} {
		if !strings.Contains(gotCalls, want) {
			t.Fatalf("expected call %q in:\n%s", want, gotCalls)
		}
	}
	if strings.Contains(gotCalls, "dev awg3 table main") {
		t.Fatalf("endpoint exclusion must not use awg interface:\n%s", gotCalls)
	}
}

func TestOfficialParserPreflightCheckCleansUpTempFileAndUses0600(t *testing.T) {
	factory := &validatorCaptureFactory{}
	adapter, err := NewOfficialParserPreflightAdapter(OfficialParserPreflightOptions{
		ValidatorBinary: "/tmp/awg3-parser-validate",
		Timeout:         time.Second,
		TempDir:         t.TempDir(),
		CommandFactory:  factory,
	})
	if err != nil {
		t.Fatalf("new official parser preflight adapter failed: %v", err)
	}
	pair := pairForRuntime("gen-preflight")
	if err := adapter.Check(context.Background(), pair); err != nil {
		t.Fatalf("check failed: %v", err)
	}
	if factory.tempPath == "" {
		t.Fatalf("validator did not receive a temp path")
	}
	if runtime.GOOS != "windows" && factory.mode != 0o600 {
		t.Fatalf("temp file permissions = %v, want 0600", factory.mode)
	}
	if _, statErr := os.Stat(factory.tempPath); !os.IsNotExist(statErr) {
		t.Fatalf("temporary parser payload was not cleaned up: %v", statErr)
	}
}

func TestOfficialParserPreflightCheckFailureCleansUpTempFileWithoutLeakingSecrets(t *testing.T) {
	factory := &validatorCaptureFactory{returnErr: true}
	adapter, err := NewOfficialParserPreflightAdapter(OfficialParserPreflightOptions{
		ValidatorBinary: "/tmp/awg3-parser-validate",
		Timeout:         time.Second,
		TempDir:         t.TempDir(),
		CommandFactory:  factory,
	})
	if err != nil {
		t.Fatalf("new official parser preflight adapter failed: %v", err)
	}
	pair := pairForRuntime("gen-preflight-fail")
	err = adapter.Check(context.Background(), pair)
	if err == nil {
		t.Fatalf("expected parser validation to fail")
	}
	for _, secret := range []string{
		pair.Secrets.PrivateKey,
		pair.Secrets.PresharedKey,
		pair.Secrets.HeaderProtectionKey,
		pair.Secrets.ControlToken,
	} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("secret %q leaked in error: %v", secret, err)
		}
	}
	if factory.tempPath == "" {
		t.Fatalf("validator did not receive a temp path")
	}
	if _, statErr := os.Stat(factory.tempPath); !os.IsNotExist(statErr) {
		t.Fatalf("temporary parser payload was not cleaned up after failure: %v", statErr)
	}
}

type captureCommandFactory struct {
	name string
	args []string
}

func (f *captureCommandFactory) CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	f.name = name
	f.args = append([]string{}, args...)
	return helperCommand(ctx, "exit0", "")
}

type scriptedCommandFactory struct {
	pair  config.Pair
	calls []string
}

type validatorCaptureFactory struct {
	tempPath  string
	mode      os.FileMode
	returnErr bool
}

func (f *scriptedCommandFactory) CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	switch {
	case name == "awg" && len(args) >= 2 && args[0] == "showconf":
		return helperCommand(ctx, "echo", buildShowconfText(f.pair))
	case name == "awg" && len(args) >= 3 && args[0] == "show" && args[2] == "dump":
		return helperCommand(ctx, "echo", buildDumpText(f.pair))
	case name == "ip" && len(args) >= 2 && args[0] == "addr" && args[1] == "show":
		return helperCommand(ctx, "echo", f.pair.Config.TunnelAddress)
	case name == "ip" && len(args) >= 2 && args[0] == "link" && args[1] == "show":
		return helperCommand(ctx, "echo", fmt.Sprintf("mtu %d", f.pair.Config.MTU))
	case name == "ip" && len(args) >= 2 && args[0] == "route" && args[1] == "show":
		return helperCommand(ctx, "echo", strings.Join(f.pair.Config.AllowedIPs, "\n"))
	case name == "ip" && len(args) >= 2 && args[0] == "route" && args[1] == "get":
		return helperCommand(ctx, "echo", fmt.Sprintf("%s via %s dev %s src %s table %s", "213.176.116.165", f.pair.Config.OuterPath.EndpointExclusion.OuterGateway, f.pair.Config.OuterPath.EndpointExclusion.OuterEgressInterface, f.pair.Config.OuterPath.EndpointExclusion.SourceAddress, f.pair.Config.OuterPath.EndpointExclusion.RoutingTable))
	case name == "ip" && len(args) >= 2 && args[0] == "route" && (args[1] == "replace" || args[1] == "del"):
		return helperCommand(ctx, "exit0", "")
	case name == "sysctl" && len(args) >= 2 && args[0] == "-n":
		return helperCommand(ctx, "echo", "1")
	case name == "sysctl" && len(args) >= 2 && args[0] == "-w":
		return helperCommand(ctx, "exit0", "")
	case name == "awg" && len(args) >= 1 && args[0] == "setconf":
		return helperCommand(ctx, "exit0", "")
	default:
		return helperCommand(ctx, "exit0", "")
	}
}

func helperCommand(ctx context.Context, mode, output string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperProcess", "--", mode)
	encoded := base64.StdEncoding.EncodeToString([]byte(output))
	cmd.Env = append(os.Environ(), helperEnv+"=1", helperModeEnv+"="+mode, helperOutputEnv+"="+encoded)
	return cmd
}

func (f *validatorCaptureFactory) CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	if len(args) != 1 {
		return helperCommand(ctx, "exit1", "")
	}
	f.tempPath = args[0]
	if st, err := os.Stat(f.tempPath); err == nil {
		f.mode = st.Mode().Perm()
	}
	if f.returnErr {
		return helperCommand(ctx, "exit1", "")
	}
	return helperCommand(ctx, "exit0", "")
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		return
	}
	switch os.Getenv(helperModeEnv) {
	case "exit0":
		os.Exit(0)
	case "exit1":
		os.Exit(1)
	case "echo":
		if output := os.Getenv(helperOutputEnv); output != "" {
			decoded, err := base64.StdEncoding.DecodeString(output)
			if err != nil {
				os.Exit(3)
			}
			_, _ = fmt.Fprint(os.Stdout, string(decoded))
		}
		os.Exit(0)
	default:
		os.Exit(2)
	}
}

func buildShowconfText(pair config.Pair) string {
	lines := []string{
		"# generation = " + pair.Config.Generation,
		"# version = " + pair.Config.Version,
		"# interface_name = " + pair.Config.InterfaceName,
		"# gateway = " + pair.Config.Gateway,
		"# veth_address = " + pair.Config.VethAddress,
		"# health_address = " + pair.Config.HealthAddress,
		"# ui_mode = " + pair.Config.UIMode,
		"[Interface]",
		"PrivateKey = " + pair.Secrets.PrivateKey,
		"Address = " + pair.Config.TunnelAddress,
		"ListenPort = " + strconv.Itoa(pair.Config.ListenPort),
		"MTU = " + strconv.Itoa(pair.Config.MTU),
		"Jc = " + strconv.Itoa(pair.Config.Jc),
		"Jmin = " + strconv.Itoa(pair.Config.Jmin),
		"Jmax = " + strconv.Itoa(pair.Config.Jmax),
		"S1 = " + strconv.Itoa(pair.Config.S1),
		"S2 = " + strconv.Itoa(pair.Config.S2),
		"S3 = " + strconv.Itoa(pair.Config.S3),
		"S4 = " + strconv.Itoa(pair.Config.S4),
		"H1 = " + strconv.Itoa(pair.Config.H1),
		"H2 = " + strconv.Itoa(pair.Config.H2),
		"H3 = " + strconv.Itoa(pair.Config.H3),
		"H4 = " + strconv.Itoa(pair.Config.H4),
		"I1 = " + pair.Config.I1,
		"I2 = " + pair.Config.I2,
		"I3 = " + pair.Config.I3,
		"I4 = " + pair.Config.I4,
		"I5 = " + pair.Config.I5,
		"HeaderProtectionKey = " + pair.Secrets.HeaderProtectionKey,
		"ContentPaddingAddition = " + runtimeFormatRange(pair.Config.ContentPaddingAdditionMin, pair.Config.ContentPaddingAdditionMax),
		"RekeyAfterTime = " + runtimeFormatRange(pair.Config.RekeyAfterTimeMin, pair.Config.RekeyAfterTimeMax),
		"RekeyTimeout = " + runtimeFormatRange(pair.Config.RekeyTimeoutMin, pair.Config.RekeyTimeoutMax),
		"RejectAfterTime = " + runtimeFormatRange(pair.Config.RejectAfterTimeMin, pair.Config.RejectAfterTimeMax),
		"KeepaliveTimeout = " + runtimeFormatRange(pair.Config.KeepaliveTimeoutMin, pair.Config.KeepaliveTimeoutMax),
		"MaxHandshakeAttempts = " + runtimeFormatRange(pair.Config.MaxHandshakeAttemptsMin, pair.Config.MaxHandshakeAttemptsMax),
		"[Peer]",
		"PublicKey = " + pair.Secrets.PeerPublicKey,
		"PresharedKey = " + pair.Secrets.PresharedKey,
		"AllowedIPs = " + strings.Join(pair.Config.AllowedIPs, ", "),
		"Endpoint = " + pair.Config.Endpoint,
		"PersistentKeepalive = " + runtimeFormatRange(pair.Config.PersistentKeepaliveMin, pair.Config.PersistentKeepaliveMax),
	}
	return strings.Join(lines, "\n")
}

func buildDumpText(pair config.Pair) string {
	fields := make([]string, 27)
	fields[0] = pair.Secrets.PrivateKey
	fields[1] = "(none)"
	fields[2] = strconv.Itoa(pair.Config.ListenPort)
	fields[3] = strconv.Itoa(pair.Config.Jc)
	fields[14] = pair.Config.I1
	fields[18] = pair.Config.I5
	fields[19] = pair.Secrets.HeaderProtectionKey
	for i := range fields {
		if fields[i] == "" {
			fields[i] = "x"
		}
	}
	peer := []string{
		pair.Secrets.PeerPublicKey,
		pair.Secrets.PresharedKey,
		pair.Config.Endpoint,
		strings.Join(pair.Config.AllowedIPs, ","),
	}
	return strings.Join(fields, "\t") + "\n" + strings.Join(peer, "\t")
}

type stubProcessHandle struct{}

func (stubProcessHandle) PID() int                   { return 1 }
func (stubProcessHandle) Kill() error                { return nil }
func (stubProcessHandle) Wait(context.Context) error { return nil }
func (stubProcessHandle) Signal(os.Signal) error     { return nil }

var _ ProcessHandle = stubProcessHandle{}
