package richclip

import (
	"go/scanner"
	"go/token"
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
// included. TestClipboardOpensAreSessions fails on a Go file, a test's
// included, that opens it any other way: a new reader or writer goes
// through winclip.Session. And a format's data is read only by
// winclip.CopyData (its handle's checks: a null handle, a GDI one, an
// empty block, one that won't lock, its size), so no file calls
// GetClipboardData but that one.
//
// A call is the name in code (NewProc("OpenClipboard"), a function or
// method of that name); a comment naming it is no call.
func TestClipboardOpensAreSessions(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]string{
		"third_party/gioui.org/app/winclip/winclip_windows.go":  "the session itself",
		"third_party/gioui.org/app/internal/windows/windows.go": "Gio's wrapper, called by nothing (os_windows.go uses winclip)",
		"internal/ui/composer/paste_race_windows_test.go":       "clipOpenOverlap: Windows' own behaviour with two opens, for the readers test's log, after its readers stopped",
	}
	readers := map[string]string{
		"third_party/gioui.org/app/winclip/winclip_windows.go":  "CopyData",
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
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if rel == "internal/ui/richclip/session_guard_test.go" {
			return nil // the names in its cases and messages
		}
		opens, reads := clipCalls(b)
		if opens {
			if _, ok := allowed[rel]; ok {
				found++
			} else {
				t.Errorf("%s opens the clipboard itself: use winclip.Session (one session at a time in the process)", rel)
			}
		}
		if reads {
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
		t.Errorf("no file under %s calls GetClipboardData: the scan looked in the wrong place", root)
	}
	if found == 0 {
		t.Errorf("no file under %s calls OpenClipboard: the scan looked in the wrong place", root)
	}
}

// clipCalls reports whether Go source src names OpenClipboard or
// GetClipboardData in code: an identifier (a func, a method, a field) or
// a string literal holding the name (NewProc's), never a comment.
func clipCalls(src []byte) (opens, reads bool) {
	fset := token.NewFileSet()
	var s scanner.Scanner
	s.Init(fset.AddFile("", fset.Base(), len(src)), src, nil, 0) // comments skipped
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			return
		}
		if tok != token.IDENT && tok != token.STRING {
			continue
		}
		if tok == token.IDENT && (lit == "OpenClipboard" || lit == "_OpenClipboard") || tok == token.STRING && strings.Contains(lit, "OpenClipboard") {
			opens = true
		}
		if tok == token.IDENT && (lit == "GetClipboardData" || lit == "_GetClipboardData") || tok == token.STRING && strings.Contains(lit, "GetClipboardData") {
			reads = true
		}
	}
}

// The scan sees calls, not comments.
func TestClipCallsSkipsComments(t *testing.T) {
	for _, c := range []struct {
		src          string
		opens, reads bool
	}{
		{"package a\n// OpenClipboard(NULL) and GetClipboardData, as Windows has them\n", false, false},
		{"package a\n/* OpenClipboard */ var x = 1\n", false, false},
		{"package a\nvar p = user32.NewProc(\"OpenClipboard\")\n", true, false},
		{"package a\nfunc f() { windows.GetClipboardData(13) }\n", false, true},
		{"package a\nvar s = `GetClipboardData`\n", false, true},
		{"package a\nvar OpenClipboardish = 1\n", false, false},
	} {
		o, r := clipCalls([]byte(c.src))
		if o != c.opens || r != c.reads {
			t.Errorf("%q: opens %v, reads %v; want %v, %v", c.src, o, r, c.opens, c.reads)
		}
	}
}
