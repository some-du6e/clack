//go:build unix

package desktop

import (
	"fmt"
	"os"
	"syscall"
)

// privateDir makes sure dir is a directory only this user can enter:
// created 0700 if missing; an existing one must be a real directory (not a
// symlink) owned by us with no group or other permissions.
func privateDir(dir string) error {
	if err := os.Mkdir(dir, 0o700); err != nil && !os.IsExist(err) {
		return err
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	switch {
	case !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("%s is not a directory", dir)
	case !ok || int(st.Uid) != os.Getuid():
		return fmt.Errorf("%s belongs to another user", dir)
	case fi.Mode().Perm()&0o077 != 0:
		return fmt.Errorf("%s is open to other users (%v)", dir, fi.Mode().Perm())
	}
	return nil
}
