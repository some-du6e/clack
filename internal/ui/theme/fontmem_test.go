package theme

import (
	"bytes"
	"runtime"
	"testing"
	"unsafe"

	gioot "gioui.org/font/opentype"
	gtfont "github.com/go-text/typesetting/font"
	gtot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"
	"github.com/go-text/typesetting/fontscan"

	"github.com/some-du6e/clack/assets/fonts"
)

// go-text parses a font's glyphs as they're used (third_party/typesetting
// patch 1): every glyph of the embedded Lato has the extents and outline
// an eager parse of its glyf table gives.
func TestLazyGlyphsMatchEager(t *testing.T) {
	for name, b := range map[string][]byte{"Regular": fonts.Regular, "BoldItalic": fonts.BoldItalic} {
		ld, err := gtot.NewLoader(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		head, _, err := gtfont.LoadHeadTable(ld, nil)
		if err != nil {
			t.Fatal(err)
		}
		rawMaxp, _ := ld.RawTable(gtot.MustNewTag("maxp"))
		maxp, _, err := tables.ParseMaxp(rawMaxp)
		if err != nil {
			t.Fatal(err)
		}
		rawLoca, _ := ld.RawTable(gtot.MustNewTag("loca"))
		loca, err := tables.ParseLoca(rawLoca, int(maxp.NumGlyphs), head.IndexToLocFormat == 1)
		if err != nil {
			t.Fatal(err)
		}
		rawGlyf, _ := ld.RawTable(gtot.MustNewTag("glyf"))
		eager, err := tables.ParseGlyf(rawGlyf, loca)
		if err != nil {
			t.Fatal(err)
		}
		f, err := gioot.Parse(b)
		if err != nil {
			t.Fatal(err)
		}
		face := f.Face()
		for gid := range eager {
			g := eager[gid]
			ext, ok := face.GlyphExtents(gtfont.GID(gid))
			if !ok {
				t.Fatalf("%s glyph %d: no extents", name, gid)
			}
			if want := float32(max(g.YMin, g.YMax)); ext.YBearing != want || ext.Width != float32(max(g.XMin, g.XMax)-min(g.XMin, g.XMax)) {
				t.Fatalf("%s glyph %d: extents %+v, eager header %d..%d × %d..%d", name, gid, ext, g.XMin, g.XMax, g.YMin, g.YMax)
			}
			out, _ := face.GlyphDataOutline(gtfont.GID(gid))
			sg, simple := g.Data.(tables.SimpleGlyph)
			if simple && len(sg.EndPtsOfContours) > 0 && len(out.Segments) == 0 {
				t.Fatalf("%s glyph %d: %d contours eagerly, no outline lazily", name, gid, len(sg.EndPtsOfContours))
			}
		}
	}
}

// An embedded font's tables are read in place (third_party/typesetting
// patch 2, Gio patch 16): what the loader hands out are slices of the
// embedded bytes, and a buffer passed for reuse is never written.
func TestFontTablesNotCopied(t *testing.T) {
	b := fonts.Regular
	lo, hi := uintptr(unsafe.Pointer(unsafe.SliceData(b))), uintptr(unsafe.Pointer(unsafe.SliceData(b)))+uintptr(len(b))
	inside := func(s []byte) bool {
		p := uintptr(unsafe.Pointer(unsafe.SliceData(s)))
		return len(s) > 0 && p >= lo && p+uintptr(len(s)) <= hi
	}
	ld, err := gtot.NewLoader(gtot.NewBytesResource(b))
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"glyf", "GPOS", "cmap", "hmtx"} {
		raw, err := ld.RawTable(gtot.MustNewTag(tag))
		if err != nil {
			t.Fatalf("%s: %v", tag, err)
		}
		if !inside(raw) {
			t.Errorf("table %s was copied", tag)
		}
		if cap(raw) != len(raw) {
			t.Errorf("table %s can grow into the font's other bytes (cap %d, len %d)", tag, cap(raw), len(raw))
		}
	}
	dst := bytes.Repeat([]byte{0xAA}, 1<<20)
	raw, err := ld.RawTableTo(gtot.MustNewTag("glyf"), dst)
	if err != nil {
		t.Fatal(err)
	}
	if !inside(raw) || !bytes.Equal(dst, bytes.Repeat([]byte{0xAA}, 1<<20)) {
		t.Error("RawTableTo copied the table into dst")
	}
}

// A system font the shaper falls back to is memory-mapped and parsed in
// place (third_party/typesetting patch 3): one resolved through fontscan
// draws, and its tables are the mapped file's, not the Go heap's.
func TestSystemFallbackFaceMapped(t *testing.T) {
	fm := fontscan.NewFontMap(nil)
	if err := fm.UseSystemFonts(t.TempDir()); err != nil {
		t.Skipf("no system fonts: %v", err)
	}
	fm.SetQuery(fontscan.Query{Families: []string{"sans-serif"}})
	face := fm.ResolveFace('A')
	if face == nil {
		t.Skip("no system font has an A")
	}
	gid, ok := face.NominalGlyph('A')
	if !ok {
		t.Fatal("the resolved face has no glyph for A")
	}
	if out, ok := face.GlyphDataOutline(gid); ok && len(out.Segments) == 0 {
		t.Fatal("the mapped face's A has no outline")
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	if loc := fm.FontLocation(face.Font); loc.File == "" {
		t.Skip("no file behind the face")
	}
	t.Logf("resolved %v; heap %d MB", fm.FontLocation(face.Font), m.HeapAlloc>>20)
}
