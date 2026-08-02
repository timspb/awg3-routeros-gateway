package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSaveAndLoadPairUsesAtomicFiles(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "awg3.json")
	secretsPath := filepath.Join(dir, "secrets.json")

	pair := Pair{
		Config: Config{
			Version:                 "1",
			Generation:              "gen-save",
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
			Generation:          "gen-save",
			PrivateKey:          "priv",
			PeerPublicKey:       "peer",
			PresharedKey:        "psk",
			HeaderProtectionKey: "shared-key",
			ControlToken:        "token",
		},
	}

	if err := SavePair(configPath, secretsPath, pair); err != nil {
		t.Fatalf("save pair failed: %v", err)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(configPath); err != nil {
			t.Fatalf("config missing: %v", err)
		} else if info.Mode().Perm() != 0o600 {
			t.Fatalf("config permissions = %v, want 0600", info.Mode().Perm())
		}
		if info, err := os.Stat(secretsPath); err != nil {
			t.Fatalf("secrets missing: %v", err)
		} else if info.Mode().Perm() != 0o600 {
			t.Fatalf("secrets permissions = %v, want 0600", info.Mode().Perm())
		}
	}

	loaded, err := LoadPair(configPath, secretsPath)
	if err != nil {
		t.Fatalf("load pair failed: %v", err)
	}
	if loaded.Config.InterfaceName != "awg3" {
		t.Fatalf("loaded config mismatch: %#v", loaded.Config)
	}
	if loaded.Config.Generation != loaded.Secrets.Generation {
		t.Fatalf("generation mismatch after load: %#v %#v", loaded.Config.Generation, loaded.Secrets.Generation)
	}
}
