package theme

import (
	"encoding/binary"
	"os"
	"runtime"
	"sync"

	"gioui.org/font"
	"gioui.org/font/opentype"
)

// Inline code and code blocks.
//
// Slack sets code in "Monaco, Menlo, Consolas, Courier New, monospace".
// On macOS that is Monaco, and Monaco carries embedded bitmap strikes
// (Apple 'bdat'/'bloc') next to its outlines. go-text's Face.GlyphData
// prefers a strike over the outline and Gio's Shaper.Shape draws only
// outlines, so every Monaco glyph came out empty: inline code was a
// blank grey box, in messages and in the composer alike (2026-11-08).
//
// MonoTypeface is Monaco with its bitmap tables renamed away, read from
// the system at startup (nothing is embedded: Monaco is Apple's) and put
// in the shaper's collection, so the code typeface lists name it before
// Monaco. Elsewhere it doesn't exist and the lists fall through to
// Menlo, Consolas, Courier New or the system monospace, which have no
// strikes.
const MonoTypeface = "Clack Mono"

// monoFontPaths are the candidates for MonoTypeface, per GOOS.
var monoFontPaths = map[string][]string{
	"darwin": {
		"/System/Library/Fonts/Monaco.ttf",
		"/System/Library/Fonts/Supplemental/Monaco.ttf",
		"/Library/Fonts/Monaco.ttf",
	},
}

var (
	monoOnce  sync.Once
	monoFaces []font.FontFace
)

// MonoFaces is MonoTypeface's face (none when the system has no Monaco).
func MonoFaces() []font.FontFace {
	monoOnce.Do(func() {
		for _, p := range monoFontPaths[runtime.GOOS] {
			b, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			face, err := opentype.Parse(StripBitmapTables(b))
			if err != nil {
				continue
			}
			monoFaces = []font.FontFace{{Font: font.Font{Typeface: MonoTypeface}, Face: face}}
			return
		}
	})
	return monoFaces
}

// bitmapTags are the embedded-bitmap tables (OpenType EBDT/EBLC/EBSC,
// Apple bdat/bloc, color CBDT/CBLC and sbix).
var bitmapTags = []string{"EBDT", "EBLC", "EBSC", "bdat", "bloc", "CBDT", "CBLC", "sbix"}

// StripBitmapTables returns a copy of an sfnt font (or collection) whose
// embedded-bitmap tables are renamed to unknown tags, so parsers use the
// outlines. Anything that isn't an sfnt is returned unchanged.
func StripBitmapTables(b []byte) []byte {
	out := append([]byte(nil), b...)
	if len(out) < 12 {
		return out
	}
	var dirs []int // table-directory offsets
	switch string(out[:4]) {
	case "ttcf":
		n := int(binary.BigEndian.Uint32(out[8:12]))
		for i := 0; i < n && 12+4*i+4 <= len(out); i++ {
			dirs = append(dirs, int(binary.BigEndian.Uint32(out[12+4*i:])))
		}
	case "\x00\x01\x00\x00", "true", "OTTO", "typ1":
		dirs = []int{0}
	default:
		return out
	}
	for _, d := range dirs {
		if d < 0 || d+12 > len(out) {
			continue
		}
		n := int(binary.BigEndian.Uint16(out[d+4:]))
		for i := 0; i < n; i++ {
			rec := d + 12 + 16*i
			if rec+16 > len(out) {
				break
			}
			tag := string(out[rec : rec+4])
			for _, t := range bitmapTags {
				if tag == t {
					out[rec] = 'x' // "EBDT" -> "xBDT": no parser knows it
					break
				}
			}
		}
	}
	return out
}
