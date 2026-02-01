package mailcloak

import (
	"fmt"
	"log"
	"os"
	"os/user"
	"strconv"
)

func ChownChmodSocket(path string, cfg *Config) error {
	if cfg.Sockets.SocketOwnerUser != "" && cfg.Sockets.SocketOwnerGroup != "" {
		u, err := user.Lookup(cfg.Sockets.SocketOwnerUser)
		if err != nil {
			return err
		}
		g, err := user.LookupGroup(cfg.Sockets.SocketOwnerGroup)
		if err != nil {
			return err
		}
		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(g.Gid)

		if err := os.Chown(path, uid, gid); err != nil {
			return err
		}
	}

	if cfg.Sockets.SocketMode != "" {
		mode, err := strconv.ParseUint(cfg.Sockets.SocketMode, 8, 32)
		if err != nil {
			return fmt.Errorf("bad socket_mode: %w", err)
		}
		log.Printf("socket perms: chown %s:%s mode %s on %s", cfg.Sockets.SocketOwnerUser, cfg.Sockets.SocketOwnerGroup, cfg.Sockets.SocketMode, path)
		return os.Chmod(path, os.FileMode(mode))
	}

	return nil
}
