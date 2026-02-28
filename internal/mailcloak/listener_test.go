package mailcloak

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestPrepareUnixSocketMissingPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.sock")

	if err := prepareUnixSocket(path); err != nil {
		t.Fatalf("prepareUnixSocket error: %v", err)
	}
}

func TestPrepareUnixSocketRejectsRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-socket")
	if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	err := prepareUnixSocket(path)
	if err == nil {
		t.Fatal("expected regular file to be rejected")
	}
	if !strings.Contains(err.Error(), "is not a unix socket") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("expected regular file to remain in place, got stat error: %v", statErr)
	}
}

func TestPrepareUnixSocketRejectsActiveSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "active.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() {
		_ = l.Close()
		_ = os.Remove(path)
	}()

	err = prepareUnixSocket(path)
	if err == nil {
		t.Fatal("expected active socket to be rejected")
	}
	if !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("expected active socket path to remain, got stat error: %v", statErr)
	}
}

func TestPrepareUnixSocketRemovesStaleSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stale.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	if err := prepareUnixSocket(path); err != nil {
		t.Fatalf("prepareUnixSocket error: %v", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected stale socket to be removed, got stat error: %v", statErr)
	}
}

func TestIsStaleUnixSocketDialErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "conn refused", err: syscall.ECONNREFUSED, want: true},
		{name: "missing path", err: syscall.ENOENT, want: true},
		{name: "permission denied", err: syscall.EACCES, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isStaleUnixSocketDialErr(tc.err); got != tc.want {
				t.Fatalf("expected %v, got %v for %v", tc.want, got, tc.err)
			}
		})
	}
}
