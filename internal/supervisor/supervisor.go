package supervisor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"awg3routerosgateway/internal/config"
	awgruntime "awg3routerosgateway/internal/runtime"
)

type Options struct {
	ConfigPath     string
	SecretsPath    string
	StatusAddr     string
	ConfigAddr     string
	Mode           string
	SessionTTL     time.Duration
	SessionIdleTTL time.Duration
	SessionMaxTTL  time.Duration
	Runtime        *awgruntime.Controller
	Clock          func() time.Time
	ConfigLoader   func(string, string) (config.Pair, error)
	ConfigSaver    func(string, string, config.Pair) error
}

type Supervisor struct {
	opts Options

	mu          sync.Mutex
	pair        config.Pair
	candidate   *config.Pair
	ui          session
	statusSrv   *http.Server
	configSrv   *http.Server
	sessionStop chan struct{}
}

type session struct {
	Open           bool      `json:"open"`
	OpenedAt       time.Time `json:"opened_at"`
	LastActivityAt time.Time `json:"last_activity_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	IdleExpiresAt  time.Time `json:"idle_expires_at"`
	Nonce          string    `json:"-"`
}

type PublicSession struct {
	Open           bool      `json:"open"`
	OpenedAt       time.Time `json:"opened_at"`
	LastActivityAt time.Time `json:"last_activity_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	IdleExpiresAt  time.Time `json:"idle_expires_at"`
}

type OpenResponse struct {
	Status       string `json:"status"`
	SessionNonce string `json:"session_nonce"`
}

type Status struct {
	Mode          string                 `json:"mode"`
	StatusAddr    string                 `json:"status_addr"`
	ConfigAddr    string                 `json:"config_addr"`
	UI            PublicSession          `json:"ui"`
	Effective     config.EffectiveConfig `json:"effective"`
	Runtime       awgruntime.Status      `json:"runtime"`
	ConfigLoaded  bool                   `json:"config_loaded"`
	SecretsLoaded bool                   `json:"secrets_loaded"`
}

func New(opts Options) (*Supervisor, error) {
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.ConfigLoader == nil || opts.ConfigSaver == nil {
		return nil, errors.New("config loader and saver are required")
	}
	if opts.StatusAddr == "" {
		opts.StatusAddr = "127.0.0.1:8080"
	}
	if opts.ConfigAddr == "" {
		opts.ConfigAddr = "127.0.0.1:8081"
	}
	if opts.SessionIdleTTL <= 0 {
		opts.SessionIdleTTL = opts.SessionTTL
	}
	if opts.SessionIdleTTL <= 0 {
		opts.SessionIdleTTL = 10 * time.Minute
	}
	if opts.SessionMaxTTL <= 0 {
		if opts.SessionTTL > 0 {
			opts.SessionMaxTTL = opts.SessionTTL * 4
		}
	}
	if opts.SessionMaxTTL <= 0 {
		opts.SessionMaxTTL = time.Hour
	}
	if opts.SessionMaxTTL < opts.SessionIdleTTL {
		opts.SessionMaxTTL = opts.SessionIdleTTL
	}
	return &Supervisor{opts: opts}, nil
}

func (s *Supervisor) Validate(ctx context.Context) error {
	_, err := s.load()
	return err
}

func (s *Supervisor) Apply(ctx context.Context) error {
	s.mu.Lock()
	candidate := s.candidate
	previous := s.pair
	s.mu.Unlock()
	if candidate == nil {
		return errors.New("no candidate staged")
	}
	if err := s.opts.ConfigSaver(s.opts.ConfigPath, s.opts.SecretsPath, *candidate); err != nil {
		return err
	}
	if s.opts.Runtime != nil {
		if err := s.opts.Runtime.Apply(ctx, *candidate); err != nil {
			_, restoreErr := config.RestoreGeneration(s.opts.ConfigPath, s.opts.SecretsPath, previous.Config.Generation)
			if restoreErr != nil {
				return fmt.Errorf("runtime apply failed: %v; rollback restore failed: %w", err, restoreErr)
			}
			s.mu.Lock()
			s.pair = previous
			s.mu.Unlock()
			return err
		}
	}
	srv, stop := s.clearSessionAfterApply(*candidate)
	if stop != nil {
		close(stop)
	}
	if srv != nil {
		go shutdownServer(srv)
	}
	return nil
}

func (s *Supervisor) Run(ctx context.Context) error {
	if _, err := s.load(); err != nil {
		return err
	}
	if s.opts.Runtime != nil {
		if err := s.opts.Runtime.Start(ctx, s.pair); err != nil {
			return err
		}
	}
	srv := &http.Server{Addr: s.opts.StatusAddr, Handler: s.statusRoutes()}
	s.mu.Lock()
	s.statusSrv = srv
	s.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if s.opts.Runtime != nil {
			type shutdowner interface {
				Shutdown(context.Context) error
			}
			if sh, ok := any(s.opts.Runtime).(shutdowner); ok {
				_ = sh.Shutdown(shutdownCtx)
			} else {
				_ = s.opts.Runtime.Stop(shutdownCtx)
			}
		}
		configSrv, stop := s.detachConfigListenerLocked()
		if stop != nil {
			close(stop)
		}
		if configSrv != nil {
			_ = configSrv.Shutdown(shutdownCtx)
		}
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func (s *Supervisor) WriteStatus(ctx context.Context, w io.Writer) error {
	return json.NewEncoder(w).Encode(s.Status())
}

func (s *Supervisor) OpenUI(ctx context.Context) error {
	_, err := s.openUI(ctx)
	return err
}

func (s *Supervisor) openUI(ctx context.Context) (string, error) {
	if _, err := s.load(); err != nil {
		return "", err
	}

	s.mu.Lock()
	s.sessionExpiredLocked()
	if s.ui.Open && s.configSrv != nil {
		s.touchSessionLocked()
		s.mu.Unlock()
		return s.ui.Nonce, nil
	}
	s.mu.Unlock()

	ln, err := net.Listen("tcp", s.opts.ConfigAddr)
	if err != nil {
		return "", err
	}

	srv := &http.Server{Handler: s.configRoutes()}

	s.mu.Lock()
	if s.ui.Open && s.configSrv != nil {
		s.touchSessionLocked()
		s.mu.Unlock()
		_ = ln.Close()
		return s.ui.Nonce, nil
	}
	now := s.opts.Clock()
	nonce, err := randomNonce()
	if err != nil {
		s.mu.Unlock()
		_ = ln.Close()
		return "", err
	}
	s.ui = session{
		Open:           true,
		OpenedAt:       now,
		LastActivityAt: now,
		ExpiresAt:      now.Add(s.opts.SessionMaxTTL),
		IdleExpiresAt:  now.Add(s.opts.SessionIdleTTL),
		Nonce:          nonce,
	}
	s.configSrv = srv
	s.startSessionMonitorLocked()
	s.opts.ConfigAddr = ln.Addr().String()
	s.mu.Unlock()

	go serveConfig(srv, ln)
	return nonce, nil
}

func (s *Supervisor) CloseUI(ctx context.Context) error {
	srv, stop := s.detachConfigListener()
	if stop != nil {
		close(stop)
	}
	if srv != nil {
		shutdownServer(srv)
	}
	return nil
}

func (s *Supervisor) SetCandidate(pair config.Pair) error {
	if err := pair.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := pair
	s.candidate = &cp
	return nil
}

func (s *Supervisor) statusRoutes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		_ = json.NewEncoder(w).Encode(s.currentStatus())
	})
	mux.HandleFunc("/control/ui/open", s.authenticated(func(w http.ResponseWriter, r *http.Request) {
		nonce, err := s.openUI(r.Context())
		if err != nil {
			http.Error(w, "ui open failed", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(OpenResponse{Status: "opened", SessionNonce: nonce})
	}))
	mux.HandleFunc("/control/ui/close", s.authenticated(func(w http.ResponseWriter, r *http.Request) {
		if err := s.CloseUI(r.Context()); err != nil {
			http.Error(w, "ui close failed", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(s.currentStatus())
	}))
	mux.HandleFunc("/control/candidate", s.authenticated(func(w http.ResponseWriter, r *http.Request) {
		if !s.requireOpenSession(r) {
			http.Error(w, "configuration ui closed", http.StatusForbidden)
			return
		}
		var pair config.Pair
		if err := decodeStrictJSON(w, r, &pair, 1<<20); err != nil {
			http.Error(w, "invalid candidate payload", http.StatusBadRequest)
			return
		}
		if err := s.SetCandidate(pair); err != nil {
			http.Error(w, "invalid candidate", http.StatusBadRequest)
			return
		}
		s.touchSession()
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "staged"})
	}))
	mux.HandleFunc("/control/apply", s.authenticated(func(w http.ResponseWriter, r *http.Request) {
		if !s.requireOpenSession(r) {
			http.Error(w, "configuration ui closed", http.StatusForbidden)
			return
		}
		s.touchSession()
		if err := s.Apply(r.Context()); err != nil {
			http.Error(w, "apply failed", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "applied"})
	}))
	mux.HandleFunc("/control/effective", s.authenticated(func(w http.ResponseWriter, r *http.Request) {
		if !s.requireOpenSession(r) {
			http.Error(w, "configuration ui closed", http.StatusForbidden)
			return
		}
		s.touchSession()
		_ = json.NewEncoder(w).Encode(s.currentStatus().Effective)
	}))
	return mux
}

func (s *Supervisor) configRoutes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/config", s.authenticated(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.requireOpenSession(r) {
			http.Error(w, "configuration ui closed", http.StatusForbidden)
			return
		}
		s.touchSession()
		effective := s.effective()
		_ = json.NewEncoder(w).Encode(effective)
	}))
	mux.HandleFunc("/config/validate", s.authenticated(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.requireOpenSession(r) {
			http.Error(w, "configuration ui closed", http.StatusForbidden)
			return
		}
		s.touchSession()
		var pair config.Pair
		if err := decodeStrictJSON(w, r, &pair, 1<<20); err != nil {
			http.Error(w, "invalid config payload", http.StatusBadRequest)
			return
		}
		if err := pair.Validate(); err != nil {
			http.Error(w, "invalid config", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "valid"})
	}))
	mux.HandleFunc("/config/apply", s.authenticated(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.requireOpenSession(r) {
			http.Error(w, "configuration ui closed", http.StatusForbidden)
			return
		}
		s.touchSession()
		if err := s.Apply(r.Context()); err != nil {
			http.Error(w, "apply failed", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "applied"})
	}))
	mux.HandleFunc("/config/cancel", s.authenticated(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.requireOpenSession(r) {
			http.Error(w, "configuration ui closed", http.StatusForbidden)
			return
		}
		s.touchSession()
		if err := s.CloseUI(r.Context()); err != nil {
			http.Error(w, "ui close failed", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
	}))
	return mux
}

func (s *Supervisor) currentStatus() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionExpiredLocked()
	effective := config.EffectiveConfig{}
	if s.pair.Config.Version != "" || s.pair.Secrets.Version != "" {
		effective = s.effectiveLocked()
	}
	return Status{
		Mode:          s.opts.Mode,
		StatusAddr:    s.opts.StatusAddr,
		ConfigAddr:    s.opts.ConfigAddr,
		UI:            s.ui.Public(),
		Effective:     effective,
		Runtime:       s.runtimeStatus(),
		ConfigLoaded:  s.pair.Config.Version != "",
		SecretsLoaded: s.pair.Secrets.Version != "",
	}
}

func (s *Supervisor) effective() config.EffectiveConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.candidate != nil {
		return s.candidate.Effective()
	}
	return s.pair.Effective()
}

func (s *Supervisor) effectiveLocked() config.EffectiveConfig {
	if s.candidate != nil {
		return s.candidate.Effective()
	}
	return s.pair.Effective()
}

func (s *Supervisor) load() (config.Pair, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *Supervisor) loadLocked() (config.Pair, error) {
	pair, err := s.opts.ConfigLoader(s.opts.ConfigPath, s.opts.SecretsPath)
	if err != nil {
		return config.Pair{}, err
	}
	s.pair = pair
	return pair, nil
}

func (s *Supervisor) authenticated(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.checkToken(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Supervisor) checkToken(r *http.Request) bool {
	token := r.Header.Get("X-AWG3-Control-Token")
	if token == "" {
		auth := r.Header.Get("Authorization")
		token = strings.TrimPrefix(auth, "Bearer ")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pair.Secrets.ControlToken == "" {
		return false
	}
	return token != "" && token == s.pair.Secrets.ControlToken
}

func (s *Supervisor) requireOpenSession(r *http.Request) bool {
	token := r.Header.Get("X-AWG3-Session-Nonce")
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ui.Open || s.ui.Nonce == "" {
		return false
	}
	if s.sessionExpiredLocked() {
		return false
	}
	if token == "" || token != s.ui.Nonce {
		return false
	}
	return true
}

func (s *Supervisor) touchSession() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.touchSessionLocked()
}

func (s *Supervisor) touchSessionLocked() {
	if !s.ui.Open {
		return
	}
	now := s.opts.Clock()
	if s.ui.OpenedAt.IsZero() {
		s.ui.OpenedAt = now
	}
	s.ui.LastActivityAt = now
	if s.opts.SessionIdleTTL > 0 {
		s.ui.IdleExpiresAt = now.Add(s.opts.SessionIdleTTL)
	}
}

func (s *Supervisor) sessionExpiredLocked() bool {
	if !s.ui.Open {
		return false
	}
	now := s.opts.Clock()
	if !s.ui.IdleExpiresAt.IsZero() && !now.Before(s.ui.IdleExpiresAt) {
		s.clearSessionLocked()
		return true
	}
	if !s.ui.ExpiresAt.IsZero() && !now.Before(s.ui.ExpiresAt) {
		s.clearSessionLocked()
		return true
	}
	return false
}

func (s session) Public() PublicSession {
	return PublicSession{
		Open:           s.Open,
		OpenedAt:       s.OpenedAt,
		LastActivityAt: s.LastActivityAt,
		ExpiresAt:      s.ExpiresAt,
		IdleExpiresAt:  s.IdleExpiresAt,
	}
}

func (s *Supervisor) clearSessionLocked() (*http.Server, chan struct{}) {
	srv := s.configSrv
	stop := s.sessionStop
	s.configSrv = nil
	s.sessionStop = nil
	s.candidate = nil
	s.ui = session{}
	s.opts.ConfigAddr = "127.0.0.1:0"
	return srv, stop
}

func (s *Supervisor) detachConfigListenerLocked() (*http.Server, chan struct{}) {
	srv := s.configSrv
	stop := s.sessionStop
	s.configSrv = nil
	s.sessionStop = nil
	return srv, stop
}

func (s *Supervisor) detachConfigListener() (*http.Server, chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clearSessionLocked()
}

func (s *Supervisor) clearSessionAfterApply(candidate config.Pair) (*http.Server, chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pair = candidate
	s.candidate = nil
	return s.clearSessionLocked()
}

func (s *Supervisor) startSessionMonitorLocked() {
	if s.sessionStop != nil {
		close(s.sessionStop)
	}
	stop := make(chan struct{})
	s.sessionStop = stop
	go s.monitorSession(stop)
}

func (s *Supervisor) monitorSession(stop <-chan struct{}) {
	ticker := time.NewTicker(s.sessionMonitorInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			srv, stopCh := s.expireSessionIfNeeded()
			if srv != nil {
				if stopCh != nil {
					close(stopCh)
				}
				go shutdownServer(srv)
				return
			}
		case <-stop:
			return
		}
	}
}

func (s *Supervisor) expireSessionIfNeeded() (*http.Server, chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ui.Open {
		return nil, nil
	}
	now := s.opts.Clock()
	if (!s.ui.IdleExpiresAt.IsZero() && !now.Before(s.ui.IdleExpiresAt)) || (!s.ui.ExpiresAt.IsZero() && !now.Before(s.ui.ExpiresAt)) {
		return s.clearSessionLocked()
	}
	return nil, nil
}

func (s *Supervisor) sessionMonitorInterval() time.Duration {
	d := s.opts.SessionIdleTTL
	if d <= 0 || (s.opts.SessionMaxTTL > 0 && s.opts.SessionMaxTTL < d) {
		d = s.opts.SessionMaxTTL
	}
	if d <= 0 {
		return time.Second
	}
	d /= 2
	if d < 25*time.Millisecond {
		d = 25 * time.Millisecond
	}
	if d > time.Second {
		d = time.Second
	}
	return d
}

func randomNonce() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func decodeStrictJSON(w http.ResponseWriter, r *http.Request, out any, maxBytes int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return errors.New("trailing json data not allowed")
	}
	return nil
}

func serveConfig(srv *http.Server, ln net.Listener) {
	err := srv.Serve(ln)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return
	}
}

func shutdownServer(srv *http.Server) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

func (s *Supervisor) String() string {
	st := s.currentStatus()
	return fmt.Sprintf("mode=%s status=%s config=%s ui_open=%t", st.Mode, st.StatusAddr, st.ConfigAddr, st.UI.Open)
}

func (s *Supervisor) Status() Status {
	return s.currentStatus()
}

func (s *Supervisor) runtimeStatus() awgruntime.Status {
	if s.opts.Runtime == nil {
		return awgruntime.Status{}
	}
	return s.opts.Runtime.Status()
}

func (s *Supervisor) sessionNonce() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ui.Nonce
}
