package mailcloak

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
)

func OpenSocketmapListener(cfg *Config) (net.Listener, error) {
	sock := cfg.Sockets.SocketmapSocket
	_ = os.Remove(sock)

	l, err := net.Listen("unix", sock)
	if err != nil {
		return nil, err
	}

	if err := ChownChmodSocket(sock, cfg); err != nil {
		_ = l.Close()
		return nil, err
	}
	log.Printf("socketmap listener ready on %s", sock)

	return l, nil
}

func ServeSocketmap(ctx context.Context, cfg *Config, db *MailcloakDB, l net.Listener) error {
	defer l.Close()

	for {
		conn, err := l.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				log.Printf("socketmap accept error: %v", err)
				return err
			}
		}
		go handleSocketmapConn(conn, cfg, db)
	}
}

func RunSocketmap(ctx context.Context, cfg *Config, db *MailcloakDB) error {
	l, err := OpenSocketmapListener(cfg)
	if err != nil {
		return err
	}
	return ServeSocketmap(ctx, cfg, db, l)
}

// Postfix socketmap framing: "<len>:<payload>,"
func handleSocketmapConn(conn net.Conn, cfg *Config, db *MailcloakDB) {
	defer conn.Close()
	r := bufio.NewReader(conn)

	for {
		payload, err := readSocketmapFrame(r)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
				log.Printf("socketmap read error: %v", err)
			}
			// normal close
			return
		}

		payload = strings.TrimSpace(payload)
		if payload == "" {
			log.Printf("socketmap request: empty payload")
			_ = writeSocketmapFrame(conn, "NOTFOUND")
			continue
		}

		parts := strings.SplitN(payload, " ", 2)
		if len(parts) != 2 {
			log.Printf("socketmap request: malformed payload=%q", payload)
			_ = writeSocketmapFrame(conn, "TEMP")
			continue
		}

		mapName := parts[0]
		key := strings.ToLower(strings.TrimSpace(parts[1]))
		log.Printf("socketmap request: map=%s key=%s", mapName, key)

		if mapName != "alias" {
			log.Printf("socketmap decision: map=%s action=NOTFOUND", mapName)
			_ = writeSocketmapFrame(conn, "NOTFOUND")
			continue
		}

		// Only handle our domain
		domain := strings.ToLower(cfg.Policy.Domain)
		if !strings.HasSuffix(key, "@"+domain) {
			log.Printf("socketmap decision: map=alias key=%s action=NOTFOUND (other domain)", key)
			_ = writeSocketmapFrame(conn, "NOTFOUND")
			continue
		}

		username, ok, err := db.AliasOwner(key)
		if err != nil {
			log.Printf("socketmap db error: key=%s err=%v", key, err)
			_ = writeSocketmapFrame(conn, "TEMP")
			continue
		}
		if !ok {
			log.Printf("socketmap decision: map=alias key=%s action=NOTFOUND", key)
			_ = writeSocketmapFrame(conn, "NOTFOUND")
			continue
		}

		// rewrite alias -> username@domain
		reply := fmt.Sprintf("OK %s@%s", username, domain)
		log.Printf("socketmap decision: map=alias key=%s action=%s", key, reply)
		_ = writeSocketmapFrame(conn, reply)
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
