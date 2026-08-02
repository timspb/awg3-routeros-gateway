package config

import "testing"

func TestPairValidateAcceptsProductionStyleConfig(t *testing.T) {
	pair := Pair{
		Config: Config{
			Version:                 "1",
			Generation:              "gen-a",
			InterfaceName:           "awg3",
			ListenPort:              51820,
			MTU:                     1380,
			TunnelAddress:           "10.99.99.2/30",
			Gateway:                 "10.99.99.1",
			VethAddress:             "172.31.255.2/30",
			AllowedIPs:              []string{"10.99.99.0/24"},
			Endpoint:                "213.176.116.165:443",
			Jc:                      4,
			Jmin:                    50,
			Jmax:                    250,
			S1:                      84,
			S2:                      40,
			S3:                      46,
			S4:                      20,
			H1:                      50,
			H2:                      250,
			H3:                      50,
			H4:                      250,
			I1:                      "template-1",
			I2:                      "template-2",
			I3:                      "template-3",
			I4:                      "template-4",
			I5:                      "template-5",
			ContentPaddingAdditionMin: 0,
			ContentPaddingAdditionMax: 32,
			RekeyAfterTimeMin:       110,
			RekeyAfterTimeMax:       130,
			RekeyTimeoutMin:         4,
			RekeyTimeoutMax:         6,
			RejectAfterTimeMin:      175,
			RejectAfterTimeMax:      190,
			KeepaliveTimeoutMin:     9,
			KeepaliveTimeoutMax:     11,
			MaxHandshakeAttemptsMin: 16,
			MaxHandshakeAttemptsMax: 20,
			PersistentKeepaliveMin:  23,
			PersistentKeepaliveMax:  27,
			HealthAddress:           "127.0.0.1:8080",
			UIMode:                  "on_demand",
		},
		Secrets: Secrets{
			Version:             "1",
			Generation:          "gen-a",
			PrivateKey:          "priv",
			PeerPublicKey:       "peer",
			PresharedKey:        "psk",
			HeaderProtectionKey: "shared-key",
			ControlToken:        "token",
		},
	}
	if err := pair.Validate(); err != nil {
		t.Fatalf("unexpected validation failure: %v", err)
	}
	effective := pair.Effective()
	if got := effective.SecretFingerprints["private_key"]; len(got) != 32 {
		t.Fatalf("expected 32-hex fingerprint, got %q", got)
	}
}

func TestPairValidateRejectsInvalidRanges(t *testing.T) {
	pair := Pair{
		Config: Config{
			Version:                 "1",
			Generation:              "gen-b",
			InterfaceName:           "awg3",
			ListenPort:              51820,
			MTU:                     1380,
			TunnelAddress:           "10.99.99.2/30",
			Gateway:                 "10.99.99.1",
			VethAddress:             "172.31.255.2/30",
			AllowedIPs:              []string{"10.99.99.0/24"},
			Endpoint:                "213.176.116.165:443",
			Jc:                      4,
			Jmin:                    50,
			Jmax:                    250,
			S1:                      84,
			S2:                      40,
			S3:                      46,
			S4:                      20,
			H1:                      50,
			H2:                      250,
			H3:                      50,
			H4:                      250,
			I1:                      "template-1",
			I2:                      "template-2",
			I3:                      "template-3",
			I4:                      "template-4",
			I5:                      "template-5",
			ContentPaddingAdditionMin: 8,
			ContentPaddingAdditionMax: 4,
			RekeyAfterTimeMin:       110,
			RekeyAfterTimeMax:       130,
			RekeyTimeoutMin:         4,
			RekeyTimeoutMax:         6,
			RejectAfterTimeMin:      175,
			RejectAfterTimeMax:      190,
			KeepaliveTimeoutMin:     9,
			KeepaliveTimeoutMax:     11,
			MaxHandshakeAttemptsMin: 16,
			MaxHandshakeAttemptsMax: 20,
			PersistentKeepaliveMin:  23,
			PersistentKeepaliveMax:  27,
			HealthAddress:           "127.0.0.1:8080",
			UIMode:                  "always",
		},
		Secrets: Secrets{
			Version:             "1",
			Generation:          "gen-b",
			PrivateKey:          "priv",
			PeerPublicKey:       "peer",
			PresharedKey:        "psk",
			HeaderProtectionKey: "shared-key",
			ControlToken:        "token",
		},
	}
	if err := pair.Validate(); err == nil {
		t.Fatalf("expected validation failure")
	}
}
