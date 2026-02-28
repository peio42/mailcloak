package mailcloak

import (
	"context"
	"errors"
	"log"
	"net"
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
