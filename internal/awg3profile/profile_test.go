package awg3profile

import (
	"reflect"
	"strings"
	"testing"

	"awg3routerosgateway/internal/config"
)

func TestRenderParseRoundTrip(t *testing.T) {
	pair := config.Pair{
		Config: config.Config{
			Version:                   "1",
			Generation:                "gen-0000000000000001",
			InterfaceName:             "awg3",
			ListenPort:                51820,
			MTU:                       1380,
			TunnelAddress:             "10.99.99.2/30",
			Gateway:                   "10.99.99.1",
			VethAddress:               "172.31.255.2/30",
			AllowedIPs:                []string{"10.99.99.0/24", "192.168.50.0/24"},
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
		},
		Secrets: config.Secrets{
			Generation:          "gen-0000000000000001",
			PrivateKey:          "priv",
			PeerPublicKey:       "peer",
			PresharedKey:        "psk",
			HeaderProtectionKey: "shared-key",
		},
	}

	rendered, err := Render(pair)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if !strings.Contains(rendered, "# generation = gen-0000000000000001") {
		t.Fatalf("rendered profile missing generation comment: %s", rendered)
	}
	if !strings.Contains(rendered, "# ui_mode = on_demand") {
		t.Fatalf("rendered profile missing ui mode comment: %s", rendered)
	}

	parsed, err := Parse(rendered)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if !reflect.DeepEqual(parsed, pair) {
		t.Fatalf("round-trip mismatch:\nwant: %#v\n got: %#v", pair, parsed)
	}
}

func TestRenderSetconfOmitsOrchestrationFields(t *testing.T) {
	pair := config.Pair{
		Config: config.Config{
			Version:                   "1",
			Generation:                "gen-0000000000000001",
			InterfaceName:             "awg3",
			ListenPort:                51820,
			MTU:                       1380,
			TunnelAddress:             "10.99.99.2/30",
			Gateway:                   "10.99.99.1",
			VethAddress:               "172.31.255.2/30",
			AllowedIPs:                []string{"10.99.99.0/24", "192.168.50.0/24"},
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
		},
		Secrets: config.Secrets{
			Generation:          "gen-0000000000000001",
			PrivateKey:          "priv",
			PeerPublicKey:       "peer",
			PresharedKey:        "psk",
			HeaderProtectionKey: "shared-key",
		},
	}
	rendered, err := RenderSetconf(pair)
	if err != nil {
		t.Fatalf("render setconf failed: %v", err)
	}
	for _, forbidden := range []string{
		"# generation =",
		"# interface_name =",
		"# gateway =",
		"# veth_address =",
		"# health_address =",
		"# ui_mode =",
		"Address = ",
		"MTU = ",
	} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("setconf payload leaked orchestration field %q:\n%s", forbidden, rendered)
		}
	}
	for _, required := range []string{
		"[Interface]",
		"PrivateKey = priv",
		"ListenPort = 51820",
		"AllowedIPs = 10.99.99.0/24, 192.168.50.0/24",
		"Endpoint = 213.176.116.165:443",
	} {
		if !strings.Contains(rendered, required) {
			t.Fatalf("setconf payload missing %q:\n%s", required, rendered)
		}
	}
}

func TestParseRejectsUnknownKey(t *testing.T) {
	_, err := Parse("[Interface]\nPrivateKey = priv\nAddress = 10.0.0.2/30\nListenPort = 51820\nMTU = 1380\nJc = 1\nJmin = 0\nJmax = 1\nS1 = 12\nS2 = 12\nS3 = 12\nS4 = 12\nH1 = 0\nH2 = 0\nH3 = 0\nH4 = 0\nI1 = a\nI2 = b\nI3 = c\nI4 = d\nI5 = e\nHeaderProtectionKey = k\nContentPaddingAddition = 0\nRekeyAfterTime = 10\nRekeyTimeout = 5\nRejectAfterTime = 20\nKeepaliveTimeout = 10\nMaxHandshakeAttempts = 2\nBogus = 1\n[Peer]\nPublicKey = peer\nPresharedKey = psk\nAllowedIPs = 0.0.0.0/0\nEndpoint = example.com:443\nPersistentKeepalive = 15\n")
	if err == nil {
		t.Fatalf("expected parse error for unknown key")
	}
}
