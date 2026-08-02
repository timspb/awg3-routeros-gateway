package supervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"awg3routerosgateway/internal/config"
	awgruntime "awg3routerosgateway/internal/runtime"
)

func TestSupervisorStatusAndUIStateMachine(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "awg3.json")
	secretsPath := filepath.Join(dir, "secrets.json")
	pair := testPair()

	if err := config.SavePair(configPath, secretsPath, pair); err != nil {
		t.Fatalf("seed save failed: %v", err)
	}
	sup, err := New(Options{
		ConfigPath:   configPath,
		SecretsPath:  secretsPath,
		StatusAddr:   "127.0.0.1:0",
		ConfigAddr:   "127.0.0.1:0",
		Mode:         "run",
		SessionTTL:   time.Minute,
		Clock:        func() time.Time { return time.Unix(1000, 0) },
		ConfigLoader: config.LoadPair,
		ConfigSaver:  config.SavePair,
	})
	if err != nil {
		t.Fatalf("new supervisor failed: %v", err)
	}
	if err := sup.Validate(context.Background()); err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/config", nil)
	req.Header.Set("X-AWG3-Control-Token", "token")
	rec := httptest.NewRecorder()
	sup.configRoutes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected config to be closed, got status %d", rec.Code)
	}
	if err := sup.OpenUI(context.Background()); err != nil {
		t.Fatalf("open ui failed: %v", err)
	}
	nonce := sup.sessionNonce()
	if nonce == "" {
		t.Fatalf("expected session nonce after open")
	}
	req = httptest.NewRequest(http.MethodGet, "/config", nil)
	req.Header.Set("X-AWG3-Control-Token", "token")
	req.Header.Set("X-AWG3-Session-Nonce", nonce)
	rec = httptest.NewRecorder()
	sup.configRoutes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected config to open, got status %d", rec.Code)
	}
	candidate := pair
	candidate.Config.Endpoint = "213.176.116.165:8443"
	candidate.Config.Generation = "gen-sup-next"
	candidate.Secrets.Generation = "gen-sup-next"
	stageBody, err := json.Marshal(candidate)
	if err != nil {
		t.Fatalf("marshal candidate failed: %v", err)
	}
	stageReq := httptest.NewRequest(http.MethodPost, "/control/candidate", bytes.NewReader(stageBody))
	stageReq.Header.Set("X-AWG3-Control-Token", "token")
	stageReq.Header.Set("X-AWG3-Session-Nonce", nonce)
	stageRec := httptest.NewRecorder()
	sup.statusRoutes().ServeHTTP(stageRec, stageReq)
	if stageRec.Code != http.StatusOK {
		t.Fatalf("expected candidate stage ok, got %d: %s", stageRec.Code, stageRec.Body.String())
	}
	applyReq := httptest.NewRequest(http.MethodPost, "/control/apply", nil)
	applyReq.Header.Set("X-AWG3-Control-Token", "token")
	applyReq.Header.Set("X-AWG3-Session-Nonce", nonce)
	applyRec := httptest.NewRecorder()
	sup.statusRoutes().ServeHTTP(applyRec, applyReq)
	if applyRec.Code != http.StatusOK {
		t.Fatalf("expected apply ok, got %d: %s", applyRec.Code, applyRec.Body.String())
	}
	status := sup.Status()
	if status.UI.Open {
		t.Fatalf("expected ui closed after apply")
	}
	statusJSON, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal status failed: %v", err)
	}
	if strings.Contains(string(statusJSON), nonce) {
		t.Fatalf("public status leaked nonce: %s", string(statusJSON))
	}
	if got := status.Effective.Endpoint; got != "213.176.116.165:8443" {
		t.Fatalf("unexpected endpoint after apply: %q", got)
	}
	var buf bytes.Buffer
	if err := sup.WriteStatus(context.Background(), &buf); err != nil {
		t.Fatalf("write status failed: %v", err)
	}
	if !strings.Contains(buf.String(), "\"mode\"") {
		t.Fatalf("status output missing mode field: %s", buf.String())
	}
	if strings.Contains(buf.String(), nonce) {
		t.Fatalf("WriteStatus leaked nonce: %s", buf.String())
	}
	if err := sup.CloseUI(context.Background()); err != nil {
		t.Fatalf("close ui failed: %v", err)
	}
	if sup.Status().UI.Open {
		t.Fatalf("expected ui closed")
	}
}

func TestControlUIOpenReturnsNonceAndInvalidatesOnReopen(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "awg3.json")
	secretsPath := filepath.Join(dir, "secrets.json")
	pair := testPair()

	if err := config.SavePair(configPath, secretsPath, pair); err != nil {
		t.Fatalf("seed save failed: %v", err)
	}
	sup, err := New(Options{
		ConfigPath:   configPath,
		SecretsPath:  secretsPath,
		StatusAddr:   "127.0.0.1:0",
		ConfigAddr:   "127.0.0.1:0",
		Mode:         "run",
		SessionTTL:   time.Minute,
		Clock:        func() time.Time { return time.Unix(1000, 0) },
		ConfigLoader: config.LoadPair,
		ConfigSaver:  config.SavePair,
	})
	if err != nil {
		t.Fatalf("new supervisor failed: %v", err)
	}
	if err := sup.OpenUI(context.Background()); err != nil {
		t.Fatalf("open ui failed: %v", err)
	}
	first := sup.sessionNonce()
	if first == "" {
		t.Fatalf("expected nonce after open")
	}
	if err := sup.CloseUI(context.Background()); err != nil {
		t.Fatalf("close ui failed: %v", err)
	}
	if sup.sessionNonce() != "" {
		t.Fatalf("expected nonce to be cleared after close")
	}
	openReq := httptest.NewRequest(http.MethodPost, "/control/ui/open", nil)
	openReq.Header.Set("X-AWG3-Control-Token", "token")
	openRec := httptest.NewRecorder()
	sup.statusRoutes().ServeHTTP(openRec, openReq)
	if openRec.Code != http.StatusOK {
		t.Fatalf("expected authenticated open to succeed, got %d: %s", openRec.Code, openRec.Body.String())
	}
	var openResp OpenResponse
	if err := json.Unmarshal(openRec.Body.Bytes(), &openResp); err != nil {
		t.Fatalf("decode open response failed: %v", err)
	}
	if openResp.SessionNonce == "" {
		t.Fatalf("expected open response nonce")
	}
	if err := sup.OpenUI(context.Background()); err != nil {
		t.Fatalf("reopen ui failed: %v", err)
	}
	second := sup.sessionNonce()
	if second == "" || second == first {
		t.Fatalf("expected fresh nonce on reopen, got %q then %q", first, second)
	}
}

func TestOpenUIReportsBindFailure(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "awg3.json")
	secretsPath := filepath.Join(dir, "secrets.json")
	pair := testPair()

	if err := config.SavePair(configPath, secretsPath, pair); err != nil {
		t.Fatalf("seed save failed: %v", err)
	}

	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer blocker.Close()

	sup, err := New(Options{
		ConfigPath:     configPath,
		SecretsPath:    secretsPath,
		StatusAddr:     "127.0.0.1:0",
		ConfigAddr:     blocker.Addr().String(),
		Mode:           "run",
		SessionTTL:     time.Minute,
		SessionIdleTTL: 50 * time.Millisecond,
		SessionMaxTTL:  2 * time.Minute,
		Clock:          func() time.Time { return time.Unix(1000, 0) },
		ConfigLoader:   config.LoadPair,
		ConfigSaver:    config.SavePair,
	})
	if err != nil {
		t.Fatalf("new supervisor failed: %v", err)
	}
	if err := sup.OpenUI(context.Background()); err == nil {
		t.Fatalf("expected open ui to fail when bind is already taken")
	}
}

func TestSupervisorSessionExpiresInBackground(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "awg3.json")
	secretsPath := filepath.Join(dir, "secrets.json")
	pair := testPair()

	if err := config.SavePair(configPath, secretsPath, pair); err != nil {
		t.Fatalf("seed save failed: %v", err)
	}
	sup, err := New(Options{
		ConfigPath:     configPath,
		SecretsPath:    secretsPath,
		StatusAddr:     "127.0.0.1:0",
		ConfigAddr:     "127.0.0.1:0",
		Mode:           "run",
		SessionTTL:     40 * time.Millisecond,
		SessionIdleTTL: 40 * time.Millisecond,
		SessionMaxTTL:  120 * time.Millisecond,
		Clock:          time.Now,
		ConfigLoader:   config.LoadPair,
		ConfigSaver:    config.SavePair,
	})
	if err != nil {
		t.Fatalf("new supervisor failed: %v", err)
	}
	if err := sup.OpenUI(context.Background()); err != nil {
		t.Fatalf("open ui failed: %v", err)
	}
	addr := sup.Status().ConfigAddr
	if addr == "" {
		t.Fatalf("expected config addr after open")
	}
	time.Sleep(250 * time.Millisecond)
	conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		t.Fatalf("expected config listener to be closed after idle timeout")
	}
}

func TestSupervisorApplyRestoresPreviousGenerationOnRuntimeFailure(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "awg3.json")
	secretsPath := filepath.Join(dir, "secrets.json")
	pair := testPair()
	pair.Config.Generation = "gen-1"
	pair.Secrets.Generation = "gen-1"

	if err := config.SavePair(configPath, secretsPath, pair); err != nil {
		t.Fatalf("seed save failed: %v", err)
	}
	loadedBefore, err := config.LoadPair(configPath, secretsPath)
	if err != nil {
		t.Fatalf("load after seed save failed: %v", err)
	}

	ctrl, err := awgruntime.New(
		func(context.Context, config.Pair) (awgruntime.Process, error) {
			return &awgruntime.FakeProcess{PIDValue: 4242}, nil
		},
		func(_ context.Context, p config.Pair, _ awgruntime.Process) error {
			if p.Config.Generation == "gen-2" {
				return context.DeadlineExceeded
			}
			return nil
		},
		nil,
		awgruntime.IgnoreStop,
	)
	if err != nil {
		t.Fatalf("new runtime controller failed: %v", err)
	}
	if err := ctrl.Start(context.Background(), loadedBefore); err != nil {
		t.Fatalf("runtime start failed: %v", err)
	}

	sup, err := New(Options{
		ConfigPath:   configPath,
		SecretsPath:  secretsPath,
		StatusAddr:   "127.0.0.1:0",
		ConfigAddr:   "127.0.0.1:0",
		Mode:         "run",
		SessionTTL:   time.Minute,
		Clock:        func() time.Time { return time.Unix(1000, 0) },
		Runtime:      ctrl,
		ConfigLoader: config.LoadPair,
		ConfigSaver:  config.SavePair,
	})
	if err != nil {
		t.Fatalf("new supervisor failed: %v", err)
	}
	if err := sup.Validate(context.Background()); err != nil {
		t.Fatalf("validate failed: %v", err)
	}

	candidate := pair
	candidate.Config.Endpoint = "213.176.116.165:8443"
	candidate.Config.Generation = "gen-2"
	candidate.Secrets.Generation = "gen-2"
	if err := sup.SetCandidate(candidate); err != nil {
		t.Fatalf("set candidate failed: %v", err)
	}
	if err := sup.Apply(context.Background()); err == nil {
		t.Fatalf("expected apply to fail")
	}

	loaded, err := config.LoadPair(configPath, secretsPath)
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	if loaded.Config.Generation != loadedBefore.Config.Generation {
		t.Fatalf("expected rollback to %q, got %q", loadedBefore.Config.Generation, loaded.Config.Generation)
	}
	if loaded.Config.Endpoint != loadedBefore.Config.Endpoint {
		t.Fatalf("expected endpoint rollback to %q, got %q", loadedBefore.Config.Endpoint, loaded.Config.Endpoint)
	}
	if got := sup.Status().Runtime.Generation; got != loadedBefore.Config.Generation {
		t.Fatalf("expected runtime generation %q, got %q", loadedBefore.Config.Generation, got)
	}
}

func testPair() config.Pair {
	return config.Pair{
		Config: config.Config{
			Version:                   "1",
			Generation:                "gen-sup",
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
		},
		Secrets: config.Secrets{
			Version:             "1",
			Generation:          "gen-sup",
			PrivateKey:          "priv",
			PeerPublicKey:       "peer",
			PresharedKey:        "psk",
			HeaderProtectionKey: "shared-key",
			ControlToken:        "token",
		},
	}
}
