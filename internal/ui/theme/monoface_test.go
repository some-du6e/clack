package theme

import (
	"encoding/binary"
	"reflect"
	"runtime"
	"testing"

	"gioui.org/font"
	"gioui.org/op/clip"
	"gioui.org/text"
	"golang.org/x/image/math/fixed"
)

// sfntDir builds a bare table directory (no table data) with the given
// tags, at offset 0 of the returned bytes.
func sfntDir(version string, tags ...string) []byte {
	b := make([]byte, 12+16*len(tags))
	copy(b, version)
	binary.BigEndian.PutUint16(b[4:], uint16(len(tags)))
	for i, t := range tags {
		copy(b[12+16*i:], t)
		binary.BigEndian.PutUint32(b[12+16*i+8:], uint32(0x1000+i)) // offset
		binary.BigEndian.PutUint32(b[12+16*i+12:], 4)               // length
	}
	return b
}

func dirTags(b []byte, at int) []string {
	n := int(binary.BigEndian.Uint16(b[at+4:]))
	var out []string
	for i := 0; i < n; i++ {
		out = append(out, string(b[at+12+16*i:at+16+16*i]))
	}
	return out
}

func TestStripBitmapTables(t *testing.T) {
	in := sfntDir("true", "bdat", "bloc", "cmap", "glyf", "head", "EBDT", "EBLC", "sbix")
	out := StripBitmapTables(in)
	want := []string{"xdat", "xloc", "cmap", "glyf", "head", "xBDT", "xBLC", "xbix"}
	if got := dirTags(out, 0); !reflect.DeepEqual(got, want) {
		t.Fatalf("tags %q, want %q", got, want)
	}
	if got := dirTags(in, 0); got[0] != "bdat" {
		t.Fatalf("input modified: %q", got)
	}
	// offsets and lengths are untouched
	for i := 0; i < 8; i++ {
		if binary.BigEndian.Uint32(out[12+16*i+8:]) != uint32(0x1000+i) {
			t.Fatalf("record %d offset changed", i)
		}
	}

	// a collection: every face's directory
	a := sfntDir("\x00\x01\x00\x00", "glyf", "EBLC")
	c := sfntDir("\x00\x01\x00\x00", "CBDT", "CBLC", "loca")
	hdr := make([]byte, 20)
	copy(hdr, "ttcf")
	binary.BigEndian.PutUint32(hdr[8:], 2)
	binary.BigEndian.PutUint32(hdr[12:], 20)
	binary.BigEndian.PutUint32(hdr[16:], uint32(20+len(a)))
	ttc := append(append(hdr, a...), c...)
	out = StripBitmapTables(ttc)
	if got := dirTags(out, 20); !reflect.DeepEqual(got, []string{"glyf", "xBLC"}) {
		t.Fatalf("ttc face 0 %q", got)
	}
	if got := dirTags(out, 20+len(a)); !reflect.DeepEqual(got, []string{"xBDT", "xBLC", "loca"}) {
		t.Fatalf("ttc face 1 %q", got)
	}

	if got := StripBitmapTables([]byte("not a font")); string(got) != "not a font" {
		t.Fatalf("non-font changed: %q", got)
	}
}

// pathHasSegments reads clip.PathSpec's unexported hasSegments: Gio
// skips glyphs it has no outline for, so an all-bitmap run is an empty path.
func pathHasSegments(p clip.PathSpec) bool {
	return reflect.ValueOf(p).FieldByName("hasSegments").Bool()
}

func shapeRun(sh *text.Shaper, tf font.Typeface, s string) []text.Glyph {
	sh.LayoutString(text.Parameters{Font: font.Font{Typeface: tf}, PxPerEm: fixed.I(12), MaxWidth: 1 << 20}, s)
	var gs []text.Glyph
	for {
		g, ok := sh.NextGlyph()
		if !ok {
			break
		}
		gs = append(gs, g)
	}
	return gs
}

// TestCodeRunsHaveOutlines shapes inline code the way richview and the
// composer do and checks Gio gets an outline to fill. On macOS this is the
// regression test for the blank code boxes: the system Monaco's glyphs
// are bitmap strikes to go-text, MonoTypeface's are outlines.
func TestCodeRunsHaveOutlines(t *testing.T) {
	sh := NewShaper()
	for _, tf := range []font.Typeface{
		MonoTypeface + ", Monaco, Menlo, Consolas, Courier New, monospace",
		"Roboto Mono, " + MonoTypeface + ", Monaco, Menlo, Consolas, \"Courier New\", monospace",
		MonoTypeface + ", monospace",
	} {
		for _, s := range []string{"@Abcd", "sho"} {
			gs := shapeRun(sh, tf, s)
			if len(gs) == 0 {
				t.Fatalf("%q in %q: no glyphs", s, tf)
			}
			if !pathHasSegments(sh.Shape(gs)) {
				t.Errorf("%q in %q: empty outline (glyphs drawn as nothing)", s, tf)
			}
		}
	}
	if runtime.GOOS != "darwin" {
		return
	}
	if len(MonoFaces()) == 0 {
		t.Fatal("no Monaco found for MonoTypeface on macOS")
	}
	// the code run uses the stripped Monaco (collection face 8, after Lato)
	gs := shapeRun(sh, MonoTypeface+", Monaco, monospace", "@Abcd")
	for _, g := range gs {
		if _, fi, _ := splitGlyphID(g.ID); fi != 8 {
			t.Errorf("glyph from face %d, want MonoTypeface (8)", fi)
		}
	}
	// what it replaces, for the log
	raw := pathHasSegments(sh.Shape(shapeRun(sh, "Monaco", "@Abcd")))
	t.Logf("system Monaco outline through Gio: %v (false = the blank-box bug)", raw)
}
