package applog

import (
	"fmt"
	"os"
	"path/filepath"
)

// rotating is the log file: when a write would take it past max bytes it
// becomes path.1 (path.1 becomes path.2, …) and a new path is started;
// keep files stay in all. Only the Async goroutine uses it.
type rotating struct {
	path string
	max  int64
	keep int
	f    *os.File
	size int64
}

func openRotating(path string, max int64, keep int) (*rotating, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	r := &rotating{path: path, max: max, keep: keep}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *rotating) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	r.f, r.size = f, st.Size()
	return nil
}

func (r *rotating) Write(p []byte) (int, error) {
	if r.f == nil {
		if err := r.open(); err != nil { // a rotation that couldn't reopen
			return 0, err
		}
	}
	if r.size > 0 && r.size+int64(len(p)) > r.max {
		if err := r.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

// rotate shifts path.(i) to path.(i+1), dropping the oldest, and starts
// an empty path. Renames that fail (a file another program holds open on
// Windows) are skipped: the log goes on in the old file.
func (r *rotating) rotate() error {
	r.f.Close()
	r.f = nil
	name := func(i int) string {
		if i == 0 {
			return r.path
		}
		return fmt.Sprintf("%s.%d", r.path, i)
	}
	_ = os.Remove(name(r.keep - 1))
	for i := r.keep - 2; i >= 0; i-- {
		_ = os.Rename(name(i), name(i+1))
	}
	return r.open()
}

func (r *rotating) Close() error {
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}
