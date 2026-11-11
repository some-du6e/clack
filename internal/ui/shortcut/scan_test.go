package shortcut

import (
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// hardHint matches a shortcut hint written out for one platform: "Ctrl+K",
// "Cmd+Shift+S", "Ctrl Shift 1", or a macOS glyph like "⌘C" or "⇧⌘K".
var hardHint = regexp.MustCompile(`\b(Ctrl|Cmd|Command)(\+| )(Shift|Alt|Option|[A-Z0-9,./\[\]=-])|[⌘⌥⇧⌃]`)

// TestNoHardCodedHints fails when a Go string literal outside this package
// spells a shortcut for one platform. Write the combo in this package's
// syntax and show it with Format or Chips ("Mod+Shift+K"), so macOS gets
// ⇧⌘K and Windows and Linux get Ctrl+Shift+K. Comments and tests may
// say anything.
func TestNoHardCodedHints(t *testing.T) {
	formatter := map[string]bool{"IDENTFormat": true, "IDENTFormatFor": true, "IDENTChips": true, "IDENTChipsFor": true, "IDENTParse": true, "IDENTFormatAsWritten": true}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	self, _ := filepath.Abs(".")
	var bad []string
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if p == self || d.Name() == "testdata" || d.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			fs := token.NewFileSet()
			f := fs.AddFile(p, -1, len(src))
			var s scanner.Scanner
			s.Init(f, src, nil, 0)
			var prev [4]string // the last tokens, newest last
			for {
				pos, tok, lit := s.Scan()
				if tok == token.EOF {
					break
				}
				last := prev
				copy(prev[:], prev[1:])
				prev[3] = tok.String() + lit
				if tok != token.STRING || !hardHint.MatchString(lit) {
					continue
				}
				// a combo handed straight to the formatter is fine:
				// shortcut.Format("Ctrl+1") is ⌃1 / Ctrl+1
				if last[0] == "IDENTshortcut" && last[1] == "." && formatter[last[2]] && last[3] == "(" {
					continue
				}
				rel, _ := filepath.Rel(root, p)
				at := fs.Position(pos)
				bad = append(bad, rel+":"+itoa(at.Line)+": "+lit)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(bad) > 0 {
		t.Errorf("hard-coded shortcut hints (use shortcut.Format/Chips with a \"Mod+…\" combo):\n%s", strings.Join(bad, "\n"))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// The scan's pattern catches the spellings it is for and leaves combos
// and prose alone.
func TestHardHintPattern(t *testing.T) {
	for _, s := range []string{"Ctrl+C", "Ctrl+Shift+S", "Cmd+K", "Ctrl Shift 1", "⌘C", "⇧⌘K", "Ctrl+,", "%d selected. Ctrl+Enter attaches them."} {
		if !hardHint.MatchString(s) {
			t.Errorf("%q not caught", s)
		}
	}
	for _, s := range []string{"Mod+C", "Mod+Shift+S", "Alt+Up", "Ctrl+click", "delete", "L", "Esc"} {
		if hardHint.MatchString(s) {
			t.Errorf("%q caught", s)
		}
	}
}
