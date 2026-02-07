package mailcloak

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
)

type Service struct {
	policyListener    net.Listener
	socketmapListener net.Listener
	db                *MailcloakDB

	wg   sync.WaitGroup
	done chan struct{}
	once sync.Once

	errMu sync.Mutex
	err   error
}

func Start(ctx context.Context, cfg *Config) (*Service, error) {
	s := &Service{
		done: make(chan struct{}),
	}

	// Open listeners
	log.Printf("opening policy listener at %s", cfg.Sockets.PolicySocket)
	pl, err := OpenPolicyListener(cfg)
	if err != nil {
		return nil, fmt.Errorf("policy listener: %w", err)
	}
	s.policyListener = pl

	log.Printf("opening socketmap listener at %s", cfg.Sockets.SocketmapSocket)
	sl, err := OpenSocketmapListener(cfg)
	if err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("socketmap listener: %w", err)
	}
	s.socketmapListener = sl

	// Drop privileges
	if cfg.Daemon.User != "" {
		log.Printf("dropping privileges to %s", cfg.Daemon.User)
		if err := DropPrivileges(cfg); err != nil {
			_ = s.Close()
			return nil, fmt.Errorf("drop privileges: %w", err)
		}
	}

	// Open database
	log.Printf("opening sqlite db at %s", cfg.SQLite.Path)
	db, err := OpenMailcloakDB(cfg.SQLite.Path)
	if err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("sqlite: %w", err)
	}
	s.db = db

	// Create identity provider client
	idp, err := NewIdentityResolver(cfg)
	if err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("idp: %w", err)
	}

	// Start socketmap server
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := ServeSocketmap(ctx, s.db, s.socketmapListener); err != nil {
			if !isExpectedServeErr(ctx, err) {
				s.setErr(fmt.Errorf("socketmap: %w", err))
				log.Printf("socketmap: %v", err)
			}
		}
	}()

	// Start policy server
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := ServePolicy(ctx, cfg, s.db, idp, s.policyListener); err != nil {
			if !isExpectedServeErr(ctx, err) {
				s.setErr(fmt.Errorf("policy: %w", err))
				log.Printf("policy: %v", err)
			}
		}
	}()

	// Shutdown watcher
	go func() {
		<-ctx.Done()
		_ = s.Close()
	}()

	// Done closer
	go func() {
		s.wg.Wait()
		close(s.done)
	}()

	log.Printf("mailcloak started")
	return s, nil
}

func (s *Service) Close() error {
	var err error
	s.once.Do(func() {
		// close listeners to break Accept()
		if s.policyListener != nil {
			if e := s.policyListener.Close(); e != nil && err == nil {
				err = e
			}
		}
		if s.socketmapListener != nil {
			if e := s.socketmapListener.Close(); e != nil && err == nil {
				err = e
			}
		}
	})
	return err
}

func (s *Service) Done() <-chan struct{} { return s.done }

func (s *Service) Err() error {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	return s.err
}

func (s *Service) setErr(e error) {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	if s.err == nil {
		s.err = e
	}
}

// returns true if the error from a Serve* function is expected during shutdown.
func isExpectedServeErr(ctx context.Context, err error) bool {
	if err == nil {
		return true
	}
	if ctx.Err() != nil {
		return true
	}
	if errors.Is(err, net.ErrClosed) {
		return true
	}

	return false
}
