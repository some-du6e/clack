package richclip

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Karim's crash (2026-11-21, Windows): two of a paste's clipboard reads
// opened the clipboard with no window at the same time, which Windows
// allows, and one's close freed the copy the other was reading
// (0xc0000374). Every open of the clipboard in the process is a
// winclip.Session now (third_party/gioui.org/app/winclip), Gio's own
// included. TestClipboardOpensAreSessions fails on a Go file that opens it
// any other way: a new reader or writer goes through winclip.Session. And
// a format's data is read only by winclip.CopyData (its handle's checks:
// a null handle, a GDI one, an empty block, one that won't lock, its
// size), so no file calls GetClipboardData but that one.
func TestClipboardOpensAreSessions(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]string{
		"third_party/gioui.org/app/winclip/winclip_windows.go":  "the session itself",
		"third_party/gioui.org/app/winclip/winclip.go":          "the package's comment",
		"third_party/gioui.org/app/internal/windows/windows.go": "Gio's wrapper, called by nothing (os_windows.go uses winclip)",
	}
	readers := map[string]string{
		"third_party/gioui.org/app/winclip/winclip_windows.go":  "CopyData",
		"third_party/gioui.org/app/winclip/winclip.go":          "the package's comment",
		"third_party/gioui.org/app/internal/windows/windows.go": "Gio's wrapper, called by nothing (os_windows.go uses winclip.CopyData)",
	}
	found, readersFound := 0, 0
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if strings.Contains(string(b), "OpenClipboard") {
			if _, ok := allowed[rel]; ok {
				found++
			} else {
				t.Errorf("%s opens the clipboard itself: use winclip.Session (one session at a time in the process)", rel)
			}
		}
		if strings.Contains(string(b), "GetClipboardData") {
			if _, ok := readers[rel]; ok {
				readersFound++
			} else {
				t.Errorf("%s reads clipboard data itself: use winclip.Read or winclip.CopyData (the handle's checks)", rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if readersFound == 0 {
		t.Errorf("no file under %s mentions GetClipboardData: the scan looked in the wrong place", root)
	}
	if found == 0 {
		t.Errorf("no file under %s mentions OpenClipboard: the scan looked in the wrong place", root)
	}
}
