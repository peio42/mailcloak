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

	serveWG sync.WaitGroup
	connWG  sync.WaitGroup
	done    chan struct{}
	once    sync.Once
	dbOnce  sync.Once

	connsMu sync.Mutex
	conns   map[net.Conn]struct{}

	errMu sync.Mutex
	err   error
}

func Start(ctx context.Context, cfg *Config) (*Service, error) {
	s := &Service{
		done:  make(chan struct{}),
		conns: make(map[net.Conn]struct{}),
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
		_ = s.closeDB()
		_ = s.Close()
		return nil, fmt.Errorf("idp: %w", err)
	}

	// Start socketmap server
	s.serveWG.Add(1)
	go func() {
		defer s.serveWG.Done()
		if err := ServeSocketmap(ctx, s.db, s.socketmapListener, s.startConn); err != nil {
			if !isExpectedServeErr(ctx, err) {
				s.handleServeFailure("socketmap", err)
			}
		}
	}()

	// Start policy server
	s.serveWG.Add(1)
	go func() {
		defer s.serveWG.Done()
		if err := ServePolicy(ctx, cfg, s.db, idp, s.policyListener, s.startConn); err != nil {
			if !isExpectedServeErr(ctx, err) {
				s.handleServeFailure("policy", err)
			}
		}
	}()

	// Shutdown watcher
	go func() {
		<-ctx.Done()
		_ = s.Close()
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
		s.serveWG.Wait()
		if e := s.closeActiveConns(); e != nil && err == nil {
			err = e
		}
		s.connWG.Wait()
		if e := s.closeDB(); e != nil && err == nil {
			err = e
		}
		if s.done != nil {
			close(s.done)
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

func (s *Service) handleServeFailure(component string, err error) {
	s.setErr(fmt.Errorf("%s: %w", component, err))
	log.Printf("%s: %v", component, err)
	go func() {
		_ = s.Close()
	}()
}

func (s *Service) closeDB() error {
	var closeErr error
	s.dbOnce.Do(func() {
		if s.db == nil {
			return
		}
		if err := s.db.Close(); err != nil {
			closeErr = err
			s.setErr(fmt.Errorf("sqlite close: %w", err))
			log.Printf("sqlite close: %v", err)
		}
	})
	return closeErr
}

func (s *Service) startConn(conn net.Conn, handle func()) {
	s.trackConn(conn)
	s.connWG.Add(1)
	go func() {
		defer s.connWG.Done()
		defer s.untrackConn(conn)
		handle()
	}()
}

func (s *Service) trackConn(conn net.Conn) {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	if s.conns == nil {
		s.conns = make(map[net.Conn]struct{})
	}
	s.conns[conn] = struct{}{}
}

func (s *Service) untrackConn(conn net.Conn) {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	delete(s.conns, conn)
}

func (s *Service) closeActiveConns() error {
	s.connsMu.Lock()
	conns := make([]net.Conn, 0, len(s.conns))
	for conn := range s.conns {
		conns = append(conns, conn)
	}
	s.connsMu.Unlock()

	var err error
	for _, conn := range conns {
		if e := conn.Close(); e != nil && err == nil && !errors.Is(e, net.ErrClosed) {
			err = e
		}
	}
	return err
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
