package woff2

import (
	"bytes"
	"encoding/binary"
	"os"
	"reflect"
	"testing"

	gtfont "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
)

// testdata/robotomono-cyr.woff2 is Slack's RobotoMono-Cyrillic-Regular
// (a.slack-edge.com/bv1-13-br/RobotoMono-Cyrillic-Regular-7e038d4.woff2:
// glyf/loca transformed, 41 composite glyphs, a wght axis).
// robotomono-cyr.ttf is the same font decoded by fontTools 4.46, and
// robotomono-cyr-hmtx.woff2 fontTools' re-encoding of it with the hmtx
// transform as well (Slack's files don't use it).

func load(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func parse(t *testing.T, b []byte) *gtfont.Font {
	t.Helper()
	ld, err := opentype.NewLoader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	f, err := gtfont.NewFont(ld)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func numGlyphs(t *testing.T, b []byte) int {
	t.Helper()
	ld, err := opentype.NewLoader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	maxp, err := ld.RawTable(opentype.MustNewTag("maxp"))
	if err != nil || len(maxp) < 6 {
		t.Fatalf("maxp: %v", err)
	}
	return int(binary.BigEndian.Uint16(maxp[4:]))
}

// sameGlyphs compares every glyph's outline and advance, at the default
// instance and at wght 700 (gvar deltas index the reconstructed points).
func sameGlyphs(t *testing.T, gotB, wantB []byte) {
	t.Helper()
	got, want := parse(t, gotB), parse(t, wantB)
	ng, nw := numGlyphs(t, gotB), numGlyphs(t, wantB)
	if ng != nw || ng == 0 {
		t.Fatalf("%d glyphs, want %d", ng, nw)
	}
	gi := got.Cmap.Iter()
	n := 0
	for gi.Next() {
		r, gid := gi.Char()
		wid, ok := want.Cmap.Lookup(r)
		if !ok || wid != gid {
			t.Fatalf("cmap U+%04X: got glyph %d, want %d (%v)", r, gid, wid, ok)
		}
		n++
	}
	if n == 0 {
		t.Fatal("empty cmap")
	}
	for _, vars := range [][]gtfont.Variation{nil, {{Tag: opentype.MustNewTag("wght"), Value: 700}}} {
		gf, wf := gtfont.NewFace(got), gtfont.NewFace(want)
		gf.SetVariations(vars)
		wf.SetVariations(vars)
		for gid := gtfont.GID(0); int(gid) < ng; gid++ {
			if a, b := gf.HorizontalAdvance(gid), wf.HorizontalAdvance(gid); a != b {
				t.Fatalf("glyph %d (%v): advance %v, want %v", gid, vars, a, b)
			}
			go1, ok1 := gf.GlyphData(gid).(gtfont.GlyphOutline)
			wo, ok2 := wf.GlyphData(gid).(gtfont.GlyphOutline)
			if ok1 != ok2 || !reflect.DeepEqual(go1.Segments, wo.Segments) {
				t.Fatalf("glyph %d (%v): outline differs:\n got %v\nwant %v", gid, vars, go1.Segments, wo.Segments)
			}
		}
	}
}

func TestDecodeSlackRobotoMono(t *testing.T) {
	src := load(t, "robotomono-cyr.woff2")
	if !IsWOFF2(src) {
		t.Fatal("not recognised as WOFF2")
	}
	out, err := Decode(src)
	if err != nil {
		t.Fatal(err)
	}
	if IsWOFF2(out) || binary.BigEndian.Uint32(out) != 0x00010000 {
		t.Fatalf("output flavour %x, want TrueType", out[:4])
	}
	sameGlyphs(t, out, load(t, "robotomono-cyr.ttf"))
	got := parse(t, out)
	// a composite glyph survived (Й = И + breve)
	if gid, ok := got.Cmap.Lookup('Й'); !ok || len(gtfont.NewFace(got).GlyphData(gid).(gtfont.GlyphOutline).Segments) == 0 {
		t.Fatal("Й (a composite) has no outline")
	}
	// the sfnt's head checksum adjustment balances the file
	if s := checksum(out); s != 0xB1B0AFBA {
		t.Errorf("file checksum %#x, want 0xB1B0AFBA", s)
	}
}

func TestDecodeHmtxTransform(t *testing.T) {
	out, err := Decode(load(t, "robotomono-cyr-hmtx.woff2"))
	if err != nil {
		t.Fatal(err)
	}
	sameGlyphs(t, out, load(t, "robotomono-cyr.ttf"))
}

func TestDecodeRejectsGarbage(t *testing.T) {
	src := load(t, "robotomono-cyr.woff2")
	for _, b := range [][]byte{nil, []byte("wOF2"), src[:60], src[:len(src)/2], load(t, "robotomono-cyr.ttf")} {
		if _, err := Decode(b); err == nil {
			t.Errorf("Decode(%d bytes) = nil error", len(b))
		}
	}
	// a flipped byte in the brotli stream must fail, not panic
	bad := append([]byte(nil), src...)
	bad[len(bad)-40] ^= 0xff
	_, _ = Decode(bad)
}

func TestBase128And255(t *testing.T) {
	for _, c := range []struct {
		in   []byte
		want uint32
		ok   bool
	}{
		{[]byte{0x3f}, 63, true},
		{[]byte{0x81, 0x00}, 128, true},
		{[]byte{0x8f, 0xff, 0xff, 0xff, 0x7f}, 0xffffffff, true},
		{[]byte{0x80, 0x01}, 0, false},                   // leading zero
		{[]byte{0x90, 0x80, 0x80, 0x80, 0x00}, 0, false}, // overflow
		{[]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0x7f}, 0, false},
	} {
		r := &reader{b: c.in}
		v := r.base128("t")
		if (r.err == nil) != c.ok || (c.ok && v != c.want) {
			t.Errorf("base128(%x) = %d, %v", c.in, v, r.err)
		}
	}
	for _, c := range []struct {
		in   []byte
		want uint16
	}{{[]byte{252}, 252}, {[]byte{255, 0}, 253}, {[]byte{254, 0}, 506}, {[]byte{253, 0x12, 0x34}, 0x1234}} {
		r := &reader{b: c.in}
		if v := r.u255("t"); v != c.want || r.err != nil {
			t.Errorf("255UInt16(%x) = %d, %v", c.in, v, r.err)
		}
	}
}
