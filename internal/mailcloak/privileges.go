package mailcloak

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"syscall"
)

func DropPrivileges(cfg *Config) error {
	userName := cfg.Daemon.User
	if userName == "" {
		userName = "mailcloak"
	}

	u, err := user.Lookup(userName)
	if err != nil {
		return fmt.Errorf("lookup user %s: %w", userName, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return fmt.Errorf("bad uid for %s: %w", userName, err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return fmt.Errorf("bad gid for %s: %w", userName, err)
	}

	euid := os.Geteuid()
	if euid == uid {
		return nil
	}
	if euid != 0 {
		return fmt.Errorf("must run as root to switch user to %s", userName)
	}

	if groups, err := u.GroupIds(); err == nil {
		gids := make([]int, 0, len(groups))
		for _, g := range groups {
			if id, err := strconv.Atoi(g); err == nil {
				gids = append(gids, id)
			}
		}
		if len(gids) > 0 {
			if err := syscall.Setgroups(gids); err != nil {
				return fmt.Errorf("setgroups: %w", err)
			}
		}
	}

	if err := syscall.Setgid(gid); err != nil {
		return fmt.Errorf("setgid: %w", err)
	}
	if err := syscall.Setuid(uid); err != nil {
		return fmt.Errorf("setuid: %w", err)
	}

	return nil
}
