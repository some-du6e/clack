package store

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

func TestAccountDirAndDrop(t *testing.T) {
	root := t.TempDir()
	dir := AccountDir(root, "T0FAKE", "U0SELF")
	if want := filepath.Join(root, "v1", "T0FAKE-U0SELF"); dir != want {
		t.Fatalf("AccountDir = %s, want %s", dir, want)
	}
	if d := AccountDir(root, "../../etc", ""); filepath.Dir(filepath.Dir(d)) != root {
		t.Fatalf("AccountDir left the root: %s", d)
	}
	s := New(dir)
	s.PutUsers([]slack.User{{ID: "U0SELF", Name: "me"}})
	if err := s.SaveCache(); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(filepath.Join(dir, "meta.json"))
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("meta.json: %v mode %v, want 0600", err, fi)
		}
	}
	// alpha.28's boot snapshot goes
	for _, n := range []string{"boot.json", "history.json"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	RemoveBootFiles(dir)
	for _, n := range []string{"boot.json", "history.json"} {
		if _, err := os.Stat(filepath.Join(dir, n)); !os.IsNotExist(err) {
			t.Errorf("%s survived RemoveBootFiles: %v", n, err)
		}
	}
	// sign-out: everything goes and nothing comes back
	_ = os.MkdirAll(filepath.Join(dir, "img", "ab"), 0o700)
	_ = os.WriteFile(filepath.Join(dir, "img", "ab", "x"), []byte("png"), 0o600)
	s.DropDisk()
	s.PutUsers([]slack.User{{ID: "U2", Name: "later"}})
	_ = s.SaveCache()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		es, _ := os.ReadDir(dir)
		t.Fatalf("the account dir survived sign-out: %v %v", err, es)
	}
}

func TestPurgeOldVersions(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"v0/T-U", "v1/T-U", "vX", "other"} {
		_ = os.MkdirAll(filepath.Join(root, d), 0o700)
	}
	PurgeOldVersions(root)
	for d, want := range map[string]bool{"v0": false, "v1": true, "vX": true, "other": true} {
		_, err := os.Stat(filepath.Join(root, d))
		if (err == nil) != want {
			t.Errorf("%s exists=%v, want %v", d, err == nil, want)
		}
	}
}
