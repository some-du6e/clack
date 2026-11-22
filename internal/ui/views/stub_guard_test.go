package views

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoUnsupportedToast: no control under internal/ui answers a click
// with "Not supported yet" (the old cdUnsupported). Slack opens the real
// thing there, or doesn't draw the control (Karim, 2026-11-22: the User
// Groups tab's Create user group).
func TestNoUnsupportedToast(t *testing.T) {
	root := filepath.Join("..")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if strings.Contains(line, `"Not supported yet"`) || strings.Contains(line, "cdUnsupported") {
				t.Errorf("%s:%d: a stub toast: %s", p, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
