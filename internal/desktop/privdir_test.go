//go:build unix

package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

// The /tmp fallback for the instance socket only uses a directory that is
// ours alone: not a symlink, not a file, not open to other users.
func TestRuntimeDirRefusesForeignOrOpen(t *testing.T) {
	base := t.TempDir()
	fresh := filepath.Join(base, "fresh")
	if err := privateDir(fresh); err != nil {
		t.Fatalf("new dir: %v", err)
	}
	if fi, _ := os.Stat(fresh); fi.Mode().Perm() != 0o700 {
		t.Errorf("new dir mode %v, want 0700", fi.Mode().Perm())
	}
	if err := privateDir(fresh); err != nil {
		t.Errorf("existing private dir: %v", err)
	}

	open := filepath.Join(base, "open")
	if err := os.Mkdir(open, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(open, 0o777); err != nil {
		t.Fatal(err)
	}
	if privateDir(open) == nil {
		t.Error("accepted a 0777 directory")
	}

	link := filepath.Join(base, "link")
	if err := os.Symlink(fresh, link); err != nil {
		t.Fatal(err)
	}
	if privateDir(link) == nil {
		t.Error("accepted a symlink")
	}

	file := filepath.Join(base, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if privateDir(file) == nil {
		t.Error("accepted a file")
	}
}

func TestEmptySocketPathDisablesInstance(t *testing.T) {
	if forward("", Request{}) {
		t.Error("forward to no socket succeeded")
	}
	if _, err := listen("", func(Request) {}); err == nil {
		t.Error("listen on no socket succeeded")
	}
}
