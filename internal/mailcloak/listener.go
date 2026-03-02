package mailcloak

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"syscall"
	"time"
)

const (
	initialAcceptRetryDelay = 5 * time.Millisecond
	maxAcceptRetryDelay     = 1 * time.Second
)

type temporaryError interface {
	error
	Temporary() bool
}

func prepareUnixSocket(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat socket path %s: %w", path, err)
	}

	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("socket path exists and is not a unix socket: %s", path)
	}

	conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("unix socket already in use: %s", path)
	}
	if !isStaleUnixSocketDialErr(err) {
		return fmt.Errorf("check existing unix socket %s: %w", path, err)
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale unix socket %s: %w", path, err)
	}
	return nil
}

func isStaleUnixSocketDialErr(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENOENT)
}

func serveListener(ctx context.Context, serverName string, l net.Listener, handle func(net.Conn)) error {
	var retryDelay time.Duration

	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}

			var tempErr temporaryError
			if errors.As(err, &tempErr) && tempErr.Temporary() {
				retryDelay = nextAcceptRetryDelay(retryDelay)
				log.Printf("%s temporary accept error: %v; retrying in %s", serverName, err, retryDelay)

				timer := time.NewTimer(retryDelay)
				select {
				case <-ctx.Done():
					if !timer.Stop() {
						<-timer.C
					}
					return nil
				case <-timer.C:
					continue
				}
			}

			log.Printf("%s accept error: %v", serverName, err)
			return err
		}

		retryDelay = 0
		handle(conn)
	}
}

func nextAcceptRetryDelay(current time.Duration) time.Duration {
	if current <= 0 {
		return initialAcceptRetryDelay
	}
	current *= 2
	if current > maxAcceptRetryDelay {
		return maxAcceptRetryDelay
	}
	return current
}
