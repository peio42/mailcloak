package kcpolicy

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"strings"
)

func RunSocketmap(ctx context.Context, cfg *Config, db *AliasDB) error {
	sock := cfg.Sockets.SocketmapSocket
	_ = os.Remove(sock)

	l, err := net.Listen("unix", sock)
	if err != nil {
		return err
	}
	defer l.Close()

	if err := ChownChmodSocket(sock, cfg); err != nil {
		return err
	}

	for {
		conn, err := l.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return err
			}
		}
		go handleSocketmapConn(conn, cfg, db)
	}
}

// Socketmap protocol: "mapname key\n" -> "OK value\n" or "NOTFOUND\n" or "TEMP\n"
func handleSocketmapConn(conn net.Conn, cfg *Config, db *AliasDB) {
	defer conn.Close()
	r := bufio.NewReader(conn)

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			fmt.Fprint(conn, "TEMP\n")
			continue
		}
		mapName := parts[0]
		key := strings.ToLower(strings.TrimSpace(parts[1]))

		if mapName != "alias" {
			fmt.Fprint(conn, "NOTFOUND\n")
			continue
		}

		// Only handle our domain
		if !strings.HasSuffix(key, "@"+strings.ToLower(cfg.Policy.Domain)) {
			fmt.Fprint(conn, "NOTFOUND\n")
			continue
		}

		username, ok, err := db.AliasOwner(key)
		if err != nil {
			fmt.Fprint(conn, "TEMP\n")
			continue
		}
		if !ok {
			fmt.Fprint(conn, "NOTFOUND\n")
			continue
		}
		// rewrite alias -> primary rcpt (username@domain)
		fmt.Fprintf(conn, "OK %s@%s\n", username, strings.ToLower(cfg.Policy.Domain))
	}
}
