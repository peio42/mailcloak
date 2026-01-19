package kcpolicy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
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

// Postfix socketmap framing: "<len>:<payload>,"
func handleSocketmapConn(conn net.Conn, cfg *Config, db *AliasDB) {
	defer conn.Close()
	r := bufio.NewReader(conn)

	for {
		payload, err := readSocketmapFrame(r)
		if err != nil {
			// normal close
			return
		}

		payload = strings.TrimSpace(payload)
		if payload == "" {
			_ = writeSocketmapFrame(conn, "NOTFOUND")
			continue
		}

		parts := strings.SplitN(payload, " ", 2)
		if len(parts) != 2 {
			_ = writeSocketmapFrame(conn, "TEMP")
			continue
		}

		mapName := parts[0]
		key := strings.ToLower(strings.TrimSpace(parts[1]))

		if mapName != "alias" {
			_ = writeSocketmapFrame(conn, "NOTFOUND")
			continue
		}

		// Only handle our domain
		domain := strings.ToLower(cfg.Policy.Domain)
		if !strings.HasSuffix(key, "@"+domain) {
			_ = writeSocketmapFrame(conn, "NOTFOUND")
			continue
		}

		username, ok, err := db.AliasOwner(key)
		if err != nil {
			_ = writeSocketmapFrame(conn, "TEMP")
			continue
		}
		if !ok {
			_ = writeSocketmapFrame(conn, "NOTFOUND")
			continue
		}

		// rewrite alias -> username@domain
		_ = writeSocketmapFrame(conn, fmt.Sprintf("OK %s@%s", username, domain))
	}
}

func readSocketmapFrame(r *bufio.Reader) (string, error) {
	// read decimal length until ':'
	var lenBuf strings.Builder
	for {
		b, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		if b == ':' {
			break
		}
		if b < '0' || b > '9' {
			return "", io.ErrUnexpectedEOF
		}
		lenBuf.WriteByte(b)
		if lenBuf.Len() > 10 {
			return "", io.ErrUnexpectedEOF
		}
	}

	n, err := strconv.Atoi(lenBuf.String())
	if err != nil || n < 0 || n > 1024*1024 {
		return "", io.ErrUnexpectedEOF
	}

	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}

	// expect trailing comma
	b, err := r.ReadByte()
	if err != nil {
		return "", err
	}
	if b != ',' {
		return "", io.ErrUnexpectedEOF
	}

	return string(buf), nil
}

func writeSocketmapFrame(w io.Writer, payload string) error {
	// "<len>:<payload>,"
	_, err := fmt.Fprintf(w, "%d:%s,", len(payload), payload)
	return err
}
