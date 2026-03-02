package mailcloak

import (
	"context"
	"errors"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"testing"
	"time"
)

type acceptStep struct {
	conn net.Conn
	err  error
}

type stubListener struct {
	steps []acceptStep
}

func (l *stubListener) Accept() (net.Conn, error) {
	if len(l.steps) == 0 {
		return nil, net.ErrClosed
	}
	step := l.steps[0]
	l.steps = l.steps[1:]
	return step.conn, step.err
}

func (l *stubListener) Close() error { return nil }

func (l *stubListener) Addr() net.Addr { return stubAddr("stub") }

type stubAddr string

func (a stubAddr) Network() string { return string(a) }

func (a stubAddr) String() string { return string(a) }

type temporaryAcceptErr struct{ msg string }

func (e temporaryAcceptErr) Error() string   { return e.msg }
func (e temporaryAcceptErr) Timeout() bool   { return false }
func (e temporaryAcceptErr) Temporary() bool { return true }

func testSocketOwner(t *testing.T) (string, string) {
	t.Helper()
	u, err := user.Current()
	if err != nil {
		t.Fatalf("current user: %v", err)
	}
	g, err := user.LookupGroupId(u.Gid)
	if err != nil {
		t.Fatalf("lookup group: %v", err)
	}
	return u.Username, g.Name
}

func testConfig(t *testing.T, dir string) *Config {
	t.Helper()
	userName, groupName := testSocketOwner(t)
	return &Config{
		Daemon: struct {
			User string `yaml:"user"`
		}{
			User: "",
		},
		IDP: IDPConfig{
			Provider: "keycloak",
			Keycloak: KeycloakConfig{
				BaseURL:         "http://example.com",
				Realm:           "realm",
				ClientID:        "client",
				ClientSecret:    "secret",
				CacheTTLSeconds: 1,
			},
		},
		SQLite: struct {
			Path string `yaml:"path"`
		}{
			Path: filepath.Join(dir, "state.db"),
		},
		Policy: struct {
			IDPFailureMode      string `yaml:"idp_failure_mode"`
			KeycloakFailureMode string `yaml:"keycloak_failure_mode"`
		}{
			IDPFailureMode: "tempfail",
		},
		Sockets: struct {
			PolicySocket     string `yaml:"policy_socket"`
			SocketmapSocket  string `yaml:"socketmap_socket"`
			SocketOwnerUser  string `yaml:"socket_owner_user"`
			SocketOwnerGroup string `yaml:"socket_owner_group"`
			SocketMode       string `yaml:"socket_mode"`
		}{
			PolicySocket:     filepath.Join(dir, "policy.sock"),
			SocketmapSocket:  filepath.Join(dir, "socketmap.sock"),
			SocketOwnerUser:  userName,
			SocketOwnerGroup: groupName,
			SocketMode:       "0600",
		},
	}
}

func TestStartShutdown(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(t, dir)
	if err := os.WriteFile(cfg.SQLite.Path, []byte{}, 0o600); err != nil {
		t.Fatalf("write db: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc, err := Start(ctx, cfg)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Close()

	cancel()
	select {
	case <-svc.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for shutdown")
	}
	if err := svc.Err(); err != nil {
		t.Fatalf("unexpected service error: %v", err)
	}
}

func TestServiceCloseDoesNotCloseSQLite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatalf("write db: %v", err)
	}

	db, err := OpenMailcloakDB(path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	svc := &Service{
		db:   db,
		done: make(chan struct{}),
	}
	defer func() {
		if err := svc.closeDB(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	if err := svc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := svc.db.DB.Ping(); err != nil {
		t.Fatalf("expected sqlite db to remain open after Close, got %v", err)
	}
}

func TestServiceCloseDBClosesSQLite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatalf("write db: %v", err)
	}

	db, err := OpenMailcloakDB(path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	svc := &Service{
		db:   db,
		done: make(chan struct{}),
	}

	if err := svc.closeDB(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	if err := svc.db.DB.Ping(); err == nil {
		t.Fatal("expected sqlite db to be closed after closeDB")
	}
}

func TestStartPolicyListenerError(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(t, dir)
	cfg.Sockets.PolicySocket = filepath.Join(dir, "missing", "policy.sock")
	if err := os.WriteFile(cfg.SQLite.Path, []byte{}, 0o600); err != nil {
		t.Fatalf("write db: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := Start(ctx, cfg); err == nil {
		t.Fatal("expected error")
	}
}

func TestStartSocketmapListenerErrorClosesPolicy(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(t, dir)
	cfg.Sockets.SocketmapSocket = filepath.Join(dir, "missing", "socketmap.sock")
	if err := os.WriteFile(cfg.SQLite.Path, []byte{}, 0o600); err != nil {
		t.Fatalf("write db: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := Start(ctx, cfg); err == nil {
		t.Fatal("expected error")
	}

	conn, err := net.DialTimeout("unix", cfg.Sockets.PolicySocket, 200*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected policy listener to be closed")
	}
}

func TestServiceSetErrKeepsFirst(t *testing.T) {
	s := &Service{done: make(chan struct{})}
	err1 := errors.New("first")
	err2 := errors.New("second")

	s.setErr(err1)
	s.setErr(err2)

	if got := s.Err(); !errors.Is(got, err1) {
		t.Fatalf("expected first error to be kept, got %v", got)
	}
}

func TestIsExpectedServeErr(t *testing.T) {
	if !isExpectedServeErr(context.Background(), nil) {
		t.Fatal("nil error should be expected")
	}
	if !isExpectedServeErr(context.Background(), net.ErrClosed) {
		t.Fatal("net.ErrClosed should be expected")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !isExpectedServeErr(ctx, errors.New("boom")) {
		t.Fatal("error should be expected once context is canceled")
	}

	if isExpectedServeErr(context.Background(), errors.New("boom")) {
		t.Fatal("unexpected runtime error should not be expected")
	}
}

func TestServeListenerRetriesTemporaryAcceptErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	l := &stubListener{
		steps: []acceptStep{
			{err: temporaryAcceptErr{msg: "temporary accept failure"}},
			{conn: serverConn},
		},
	}

	handled := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- serveListener(ctx, "test-listener", l, func(conn net.Conn) {
			defer conn.Close()
			close(handled)
			cancel()
		})
	}()

	select {
	case <-handled:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("listener did not recover from temporary accept error")
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("serveListener returned unexpected error: %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("serveListener did not exit after cancellation")
	}
}

func TestServeListenerReturnsPermanentAcceptError(t *testing.T) {
	wantErr := errors.New("permanent accept failure")
	l := &stubListener{
		steps: []acceptStep{
			{err: wantErr},
		},
	}

	err := serveListener(context.Background(), "test-listener", l, func(conn net.Conn) {
		t.Fatal("handler should not be called")
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestHandleServeFailureClosesService(t *testing.T) {
	policyListener, err := net.Listen("unix", filepath.Join(t.TempDir(), "policy.sock"))
	if err != nil {
		t.Fatalf("policy listen: %v", err)
	}
	socketmapListener, err := net.Listen("unix", filepath.Join(t.TempDir(), "socketmap.sock"))
	if err != nil {
		t.Fatalf("socketmap listen: %v", err)
	}

	svc := &Service{
		policyListener:    policyListener,
		socketmapListener: socketmapListener,
		done:              make(chan struct{}),
	}

	failErr := errors.New("boom")
	svc.handleServeFailure("policy", failErr)

	if got := svc.Err(); !errors.Is(got, failErr) {
		t.Fatalf("expected recorded error %v, got %v", failErr, got)
	}

	if _, err := policyListener.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("expected policy listener to be closed, got %v", err)
	}
	if _, err := socketmapListener.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("expected socketmap listener to be closed, got %v", err)
	}
}

func TestShutdownDoesNotWaitForActiveSocketmapConnections(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(t, dir)
	if err := os.WriteFile(cfg.SQLite.Path, []byte{}, 0o600); err != nil {
		t.Fatalf("write db: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc, err := Start(ctx, cfg)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	conn, err := net.Dial("unix", cfg.Sockets.SocketmapSocket)
	if err != nil {
		t.Fatalf("dial socketmap: %v", err)
	}
	defer conn.Close()

	cancel()

	select {
	case <-svc.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for shutdown with active socketmap connection")
	}
}
