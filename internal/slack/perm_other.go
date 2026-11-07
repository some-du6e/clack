//go:build !windows

package slack

import (
	"os"
	"path/filepath"
	"syscall"
)

// RestrictFile makes path readable and writable by its owner only (0600).
func RestrictFile(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() || fi.Mode().Perm() == 0o600 {
		return nil
	}
	return os.Chmod(path, 0o600)
}

// RestrictDir removes group and other access from dir (0700), the config
// directory holding creds.json. It leaves shared directories alone: one
// that isn't ours, a sticky one like /tmp, the home directory and /.
func RestrictDir(dir string) error {
	fi, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !fi.IsDir() || fi.Mode().Perm()&0o077 == 0 || fi.Mode()&os.ModeSticky != 0 {
		return nil
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != os.Getuid() {
		return nil
	}
	abs, _ := filepath.Abs(dir)
	if home, _ := os.UserHomeDir(); abs == "/" || abs == filepath.Clean(home) {
		return nil
	}
	return os.Chmod(dir, fi.Mode().Perm()&0o700)
}
