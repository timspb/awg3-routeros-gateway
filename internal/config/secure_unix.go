//go:build !windows

package config

import (
	"errors"
	"os"
	"syscall"
)

func validateSecureFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("symlink not allowed")
	}
	if !info.Mode().IsRegular() {
		return errors.New("regular file required")
	}
	if info.Mode().Perm() != 0o600 {
		return errors.New("file mode must be 0600")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("file owner metadata unavailable")
	}
	if int(stat.Uid) != os.Geteuid() || int(stat.Gid) != os.Getegid() {
		return errors.New("file owner must match effective user and group")
	}
	return nil
}

