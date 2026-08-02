package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"awg3routerosgateway/internal/config"
)

func TestRouteEndpointExclusionRejectsAWGInterfaceDev(t *testing.T) {
	adapter := NewRouteEndpointExclusionAdapter()
	pair := endpointExclusionPair("gen-awg", "213.176.116.165:443", "awg3")
	pair.Config.OuterPath.EndpointExclusion.OuterEgressInterface = pair.Config.InterfaceName
	if err := adapter.Apply(context.Background(), pair); err == nil {
		t.Fatalf("expected apply to reject awg interface recursion")
	}
}

func TestRouteEndpointExclusionAcceptsMainWANRoute(t *testing.T) {
	factory := &routeCommandFactory{routeGetOutputs: map[string]string{
		"213.176.116.165": "213.176.116.165 via 10.99.99.1 dev eth0 src 10.99.99.2 table main",
	}}
	adapter := NewRouteEndpointExclusionAdapter()
	adapter.CommandFactory = factory
	pair := endpointExclusionPair("gen-main", "213.176.116.165:443", "awg3")
	if err := adapter.Apply(context.Background(), pair); err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if err := adapter.Ready(context.Background(), pair); err != nil {
		t.Fatalf("ready failed: %v", err)
	}
	if got := strings.Join(factory.calls, "\n"); !strings.Contains(got, "ip route replace 213.176.116.165/32 via 10.99.99.1 dev eth0 table main src 10.99.99.2") {
		t.Fatalf("missing outer route replacement:\n%s", got)
	}
}

func TestRouteEndpointExclusionRejectsWrongGateway(t *testing.T) {
	factory := &routeCommandFactory{routeGetOutputs: map[string]string{
		"213.176.116.165": "213.176.116.165 via 10.99.99.254 dev eth0 src 10.99.99.2 table main",
	}}
	adapter := NewRouteEndpointExclusionAdapter()
	adapter.CommandFactory = factory
	pair := endpointExclusionPair("gen-gw", "213.176.116.165:443", "awg3")
	if err := adapter.Ready(context.Background(), pair); err == nil {
		t.Fatalf("expected wrong gateway to be rejected")
	}
}

func TestRouteEndpointExclusionRejectsWrongDevice(t *testing.T) {
	factory := &routeCommandFactory{routeGetOutputs: map[string]string{
		"213.176.116.165": "213.176.116.165 via 10.99.99.1 dev awg3 src 10.99.99.2 table main",
	}}
	adapter := NewRouteEndpointExclusionAdapter()
	adapter.CommandFactory = factory
	pair := endpointExclusionPair("gen-dev", "213.176.116.165:443", "awg3")
	if err := adapter.Ready(context.Background(), pair); err == nil {
		t.Fatalf("expected awg device to be rejected")
	}
}

func TestRouteEndpointExclusionRejectsMissingRoute(t *testing.T) {
	factory := &routeCommandFactory{routeGetErr: fmt.Errorf("route missing")}
	adapter := NewRouteEndpointExclusionAdapter()
	adapter.CommandFactory = factory
	pair := endpointExclusionPair("gen-missing", "213.176.116.165:443", "awg3")
	if err := adapter.Ready(context.Background(), pair); err == nil {
		t.Fatalf("expected missing route to be rejected")
	}
}

func TestRouteEndpointExclusionRestoreReplacesCandidateAndRemovesCandidateRoute(t *testing.T) {
	factory := &routeCommandFactory{routeGetOutputs: map[string]string{
		"203.0.113.10":  "203.0.113.10 via 10.99.99.1 dev eth0 src 10.99.99.2 table main",
		"198.51.100.20": "198.51.100.20 via 10.99.99.1 dev eth0 src 10.99.99.2 table main",
	}}
	adapter := NewRouteEndpointExclusionAdapter()
	adapter.CommandFactory = factory

	prev := endpointExclusionPair("gen-prev", "203.0.113.10:443", "awg3")
	next := endpointExclusionPair("gen-next", "198.51.100.20:443", "awg3")

	if err := adapter.Apply(context.Background(), prev); err != nil {
		t.Fatalf("apply prev failed: %v", err)
	}
	if err := adapter.Ready(context.Background(), prev); err != nil {
		t.Fatalf("prev ready failed: %v", err)
	}
	if err := adapter.Apply(context.Background(), next); err != nil {
		t.Fatalf("apply candidate failed: %v", err)
	}
	if err := adapter.Ready(context.Background(), next); err != nil {
		t.Fatalf("candidate ready failed: %v", err)
	}
	if err := adapter.Restore(context.Background(), prev); err != nil {
		t.Fatalf("restore prev failed: %v", err)
	}
	if err := adapter.Ready(context.Background(), prev); err != nil {
		t.Fatalf("prev ready after restore failed: %v", err)
	}

	got := strings.Join(factory.calls, "\n")
	if !strings.Contains(got, "ip route del 198.51.100.20/32 table main") {
		t.Fatalf("candidate route was not removed during rollback:\n%s", got)
	}
	if !strings.Contains(got, "ip route replace 203.0.113.10/32 via 10.99.99.1 dev eth0 table main src 10.99.99.2") {
		t.Fatalf("previous route was not restored:\n%s", got)
	}
}

type routeCommandFactory struct {
	routeGetOutputs map[string]string
	routeGetErr     error
	calls           []string
}

func (f *routeCommandFactory) CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	switch {
	case name == "ip" && len(args) >= 2 && args[0] == "route" && args[1] == "get":
		if f.routeGetErr != nil {
			return helperCommand(ctx, "exit1", "")
		}
		endpoint := ""
		if len(args) >= 3 {
			endpoint = args[2]
		}
		if out, ok := f.routeGetOutputs[endpoint]; ok {
			return helperCommand(ctx, "echo", out)
		}
		return helperCommand(ctx, "exit1", "")
	case name == "ip" && len(args) >= 2 && args[0] == "route" && (args[1] == "replace" || args[1] == "del"):
		return helperCommand(ctx, "exit0", "")
	default:
		return helperCommand(ctx, "exit0", "")
	}
}

func endpointExclusionPair(gen, endpoint, interfaceName string) config.Pair {
	return config.Pair{
		Config: config.Config{
			Version:                   "1",
			Generation:                gen,
			InterfaceName:             interfaceName,
			ListenPort:                51820,
			MTU:                       1380,
			TunnelAddress:             "10.99.99.2/30",
			Gateway:                   "10.99.99.1",
			VethAddress:               "172.31.255.2/30",
			AllowedIPs:                []string{"10.99.99.0/24"},
			Endpoint:                  endpoint,
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
