package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"golang.org/x/image/font/sfnt"

	"github.com/some-du6e/clack/assets/fonts"
	"github.com/some-du6e/clack/internal/ui/shortcut"
)

// latoHas reports whether every Lato face the UI draws keycaps and chips
// with (Regular, Bold, Black) has a glyph for r.
func latoHas(t *testing.T, r rune) bool {
	t.Helper()
	for _, b := range [][]byte{fonts.Regular, fonts.Bold, fonts.Black} {
		f, err := sfnt.Parse(b)
		if err != nil {
			t.Fatal(err)
		}
		var buf sfnt.Buffer
		if gi, err := f.GlyphIndex(&buf, r); err != nil || gi == 0 {
			return false
		}
	}
	return true
}

// emojiSlotHas reports whether the emoji slot face maps r: those are drawn
// as emoji images (theme/emojislot.go), not from a font.
func emojiSlotHas(t *testing.T, r rune) bool {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("theme", "emojislot.ttf"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := sfnt.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	var buf sfnt.Buffer
	gi, err := f.GlyphIndex(&buf, r)
	return err == nil && gi != 0
}

// symbolsOutsideLato are the symbol runes (U+2000–U+2FFF) that appear in
// internal/ui's string literals although Lato has no glyph for them, and
// why each is fine. Anything drawn from Lato with no glyph falls back to
// the system's fonts, and on Windows that drew a missing-glyph box (the
// intro's Sign in keycap, α.36).
var symbolsOutsideLato = map[rune]string{
	'↵': "icon", // keycapIcons draws it as the enter icon
	'⌘': "macOS only: shortcut.FormatFor/ChipsFor/ClickHintFor's darwin branches, and parse aliases",
	'⌃': "macOS only: shortcut's darwin branches, and parse aliases",
	'⌥': "macOS only: shortcut's darwin branches, and parse aliases",
	'⇧': "macOS only: shortcut's darwin branches, and parse aliases",
	'⌦': "macOS only: menubar's NSMenu key equivalents",
	'‣': "input only: composer's pasted-list bullets, never drawn",
	'⁃': "input only: composer's pasted-list bullets, never drawn",
	'■': "input only: composer's pasted-list bullets, never drawn",
	'⧸': "input only: link_warning normalises it to /",
	'➤': "Windows draws it: the Windows toast's reply button (notify_toast.go, Slack's shell's content)",
}

// Every visible symbol in internal/ui's strings is in Lato or drawn as an
// emoji image, or listed above with a reason; ↵ in particular is drawn as
// an icon by every keycap. Characters that draw nothing (bidi controls,
// the word joiner, spaces, line and paragraph separators) and regular
// expressions (link_warning's look-alike set,
// matched in input, never drawn) don't count.
func TestUISymbolsHaveGlyphs(t *testing.T) {
	seen := map[rune]string{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		patterns := map[*ast.BasicLit]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if sel, ok := c.Fun.(*ast.SelectorExpr); ok {
					if x, ok := sel.X.(*ast.Ident); ok && x.Name == "regexp" {
						for _, a := range c.Args {
							if l, ok := a.(*ast.BasicLit); ok {
								patterns[l] = true
							}
						}
					}
				}
			}
			return true
		})
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if ok && patterns[lit] {
				return true
			}
			if !ok || (lit.Kind != token.STRING && lit.Kind != token.CHAR) {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			for _, r := range s {
				if r >= 0x2000 && r < 0x3000 && unicode.IsGraphic(r) && !unicode.IsSpace(r) && !unicode.Is(unicode.Cf, r) {
					if _, ok := seen[r]; !ok {
						seen[r] = fset.Position(lit.Pos()).String()
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for r, at := range seen {
		if latoHas(t, r) || emojiSlotHas(t, r) {
			continue
		}
		why, ok := symbolsOutsideLato[r]
		if !ok {
			t.Errorf("%s: %q (U+%04X) has no glyph in Lato: on Windows it draws a missing-glyph box; draw it as an icon (keycapIcons) or list it in symbolsOutsideLato with a reason", at, r, r)
			continue
		}
		if why == "icon" {
			if _, ok := keycapIcons[string(r)]; !ok {
				t.Errorf("%q is listed as drawn by an icon, but keycapIcons has no icon for it", r)
			}
		}
	}
}

// Windows' and Linux's shortcut labels and tooltip chips use only glyphs
// Lato has (the macOS ones use the system's ⌘⌥⇧⌃).
func TestShortcutChipsHaveGlyphs(t *testing.T) {
	combos := []string{"Mod+K", "Mod+Shift+K", "Ctrl+Alt+Shift+Up", "Alt+Down", "Alt+Left", "Alt+Right",
		"Mod+Enter", "Shift+Enter", "Esc", "Mod+[", "Mod+]", "Mod+,", "Mod+.", "Mod+/", "Shift+Space", "Tab", "Mod+Backspace"}
	for _, goos := range []string{"windows", "linux"} {
		for _, c := range combos {
			for _, s := range append(shortcut.ChipsFor(goos, c), shortcut.FormatFor(goos, c), shortcut.ClickHintFor(goos, c)) {
				for _, r := range s {
					if r > 0x7f && !latoHas(t, r) {
						t.Errorf("%s %q: %q has %q (U+%04X), which Lato lacks", goos, c, s, r, r)
					}
				}
			}
		}
	}
}
