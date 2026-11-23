package slick

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func testImage(w, h int, alpha byte) *image.NRGBA {
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			m.SetNRGBA(x, y, color.NRGBA{R: byte(x * 7), G: byte(y * 5), B: 120, A: alpha})
		}
	}
	return m
}

func pngOf(t *testing.T, m image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, m); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestNativeImageLikeElectron: main halves' nativeImage does what
// Electron's does: PNG and JPEG data URLs (anything else is the empty
// image), resize keeping the aspect for a side left out, toBitmap as
// Skia's N32 (BGRA, premultiplied) and createFromBitmap back, a bitmap of
// the wrong size refused, thumbnails fitted within the size asked.
func TestNativeImageLikeElectron(t *testing.T) {
	p := pngOf(t, testImage(40, 20, 255))
	img := nativeFromDataURL("data:image/png;base64," + base64.StdEncoding.EncodeToString(p))
	if img == nil || img.W != 40 || img.H != 20 {
		t.Fatalf("a PNG data URL: %+v", img)
	}
	var jb bytes.Buffer
	if err := jpeg.Encode(&jb, testImage(16, 8, 255), nil); err != nil {
		t.Fatal(err)
	}
	if j := nativeFromDataURL("data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jb.Bytes())); j == nil || j.W != 16 || j.H != 8 {
		t.Errorf("a JPEG data URL: %+v", j)
	}
	for _, u := range []string{"data:image/svg+xml;base64,PHN2Zy8+", "data:text/plain,hello", "https://example.com/a.png", "data:image/png;base64,!!"} {
		if n := nativeFromDataURL(u); n != nil {
			t.Errorf("%q made an image: %+v", u, n)
		}
	}
	for _, c := range []struct{ w, h, ww, wh int }{{20, 0, 20, 10}, {0, 5, 10, 5}, {8, 8, 8, 8}, {0, 0, 40, 20}} {
		r, err := nativeResize(img, c.w, c.h, "best")
		if err != nil || r.W != c.ww || r.H != c.wh {
			t.Errorf("resize(%d, %d): %+v %v, want %d × %d", c.w, c.h, r, err, c.ww, c.wh)
		}
	}
	// toBitmap and back: opaque pixels exactly, translucent ones within the
	// premultiplication's rounding
	half := &nativeImg{}
	if n, err := encodeNative(testImage(6, 4, 128)); err == nil {
		half = n
	}
	for _, in := range []*nativeImg{img, half} {
		bgra, err := nativeBitmap(in)
		if err != nil || len(bgra) != in.W*in.H*4 {
			t.Fatalf("toBitmap: %d bytes, %v", len(bgra), err)
		}
		back, err := nativeFromBitmap(bgra, in.W, in.H)
		if err != nil {
			t.Fatal(err)
		}
		a, _ := decodeNative(in.PNG)
		b, _ := decodeNative(back.PNG)
		for i := range a.Pix {
			if d := int(a.Pix[i]) - int(b.Pix[i]); d > 2 || d < -2 {
				t.Fatalf("pixel byte %d: %d before toBitmap, %d after createFromBitmap", i, a.Pix[i], b.Pix[i])
			}
		}
	}
	if _, err := nativeFromBitmap(make([]byte, 10), 2, 2); err == nil {
		t.Error("createFromBitmap took a buffer of the wrong size")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "big.png")
	if err := os.WriteFile(file, pngOf(t, testImage(300, 150, 255)), 0o644); err != nil {
		t.Fatal(err)
	}
	if th, err := nativeThumbnail(file, 100, 100); err != nil || th.W != 100 || th.H != 50 {
		t.Errorf("a 300 × 150 PNG's thumbnail at 100: %+v %v, want 100 × 50", th, err)
	}
	if _, err := nativeThumbnail(dir, 64, 64); err == nil {
		t.Error("a folder (an .app bundle) had a thumbnail")
	}
}

// TestIconFilesLikeTheirThumbnails: an .icns file's thumbnail is its
// largest PNG icon; an .exe's is its first icon group's largest icon, a
// PNG entry (Vista's 256 px icons) or a 32-bit DIB.
func TestIconFilesLikeTheirThumbnails(t *testing.T) {
	small, big := pngOf(t, testImage(32, 32, 255)), pngOf(t, testImage(64, 64, 255))
	var icns bytes.Buffer
	entry := func(typ string, data []byte) {
		icns.WriteString(typ)
		_ = binary.Write(&icns, binary.BigEndian, uint32(8+len(data)))
		icns.Write(data)
	}
	icns.WriteString("icns")
	_ = binary.Write(&icns, binary.BigEndian, uint32(0)) // patched below
	entry("ic11", small)
	entry("info", []byte("not an image"))
	entry("ic12", big)
	b := icns.Bytes()
	binary.BigEndian.PutUint32(b[4:], uint32(len(b)))
	m, err := largestICNS(b)
	if err != nil || m.Bounds().Dx() != 64 {
		t.Errorf("the .icns: %v, %v; want its 64 px icon", m, err)
	}

	const va = 0x4000
	for _, c := range []struct {
		name string
		icon []byte
		w    int
	}{{"a PNG icon", big, 64}, {"a 32-bit DIB", iconDIB(16), 16}} {
		img, err := iconFromResources(resourceSection(va, c.icon), va)
		if err != nil || img.Bounds().Dx() != c.w {
			t.Errorf("%s: %v, %v; want %d px", c.name, img, err, c.w)
		}
	}
}

// iconDIB is a w × w 32-bit icon resource: its BITMAPINFOHEADER (height
// counting the AND mask), the pixels bottom up, then the mask.
func iconDIB(w int) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, struct {
		Size                 uint32
		W, H                 int32
		Planes, BitCount     uint16
		Compression, ImgSize uint32
		XPPM, YPPM           int32
		Used, Important      uint32
	}{40, int32(w), int32(2 * w), 1, 32, 0, 0, 0, 0, 0, 0})
	for i := 0; i < w*w; i++ {
		b.Write([]byte{200, 100, 50, 255})
	}
	b.Write(make([]byte, w*w/8))
	return b.Bytes()
}

// resourceSection is a PE .rsrc section (at RVA va) with one icon group
// of one icon (id 1), each under language 0x409.
func resourceSection(va uint32, icon []byte) []byte {
	group := []byte{0, 0, 1, 0, 1, 0} // GRPICONDIR: an icon group of 1
	g := make([]byte, 14)
	g[4], g[6] = 1, 32                                      // planes, bit count (width 0: 256)
	binary.LittleEndian.PutUint32(g[8:], uint32(len(icon))) // bytes in res
	binary.LittleEndian.PutUint16(g[12:], 1)                // the icon's id
	group = append(group, g...)

	dir := func(entries ...[2]uint32) []byte {
		out := make([]byte, 16, 16+8*len(entries))
		binary.LittleEndian.PutUint16(out[14:], uint16(len(entries)))
		for _, e := range entries {
			var x [8]byte
			binary.LittleEndian.PutUint32(x[:], e[0])
			binary.LittleEndian.PutUint32(x[4:], e[1])
			out = append(out, x[:]...)
		}
		return out
	}
	const sub = 0x80000000
	// layout: root, the two type dirs, the two name dirs (to languages),
	// the two data entries, then the data
	root := dir([2]uint32{rtIcon, sub | 40}, [2]uint32{rtGroupIcon, sub | 64})
	iconType := dir([2]uint32{1, sub | 88})
	groupType := dir([2]uint32{1, sub | 112})
	iconLang := dir([2]uint32{0x409, 136})
	groupLang := dir([2]uint32{0x409, 152})
	data := 168
	iconEntry := make([]byte, 16)
	binary.LittleEndian.PutUint32(iconEntry, va+uint32(data))
	binary.LittleEndian.PutUint32(iconEntry[4:], uint32(len(icon)))
	groupEntry := make([]byte, 16)
	binary.LittleEndian.PutUint32(groupEntry, va+uint32(data+len(icon)))
	binary.LittleEndian.PutUint32(groupEntry[4:], uint32(len(group)))
	var out []byte
	for _, part := range [][]byte{root, iconType, groupType, iconLang, groupLang, iconEntry, groupEntry, icon, group} {
		out = append(out, part...)
	}
	return out
}

// TestImageFilePolicy: a main half's nativeImage decodes a file the user
// picked for it (dialog.openFile), not one another plugin's pick, nor
// anything in Clack's own folders.
func TestImageFilePolicy(t *testing.T) {
	h := newHarness(t)
	pic := filepath.Join(t.TempDir(), "mine.png")
	if err := os.WriteFile(pic, pngOf(t, testImage(4, 4, 255)), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := h.s.imageFile("CustomAppIcon", pic); got != "" {
		t.Errorf("an unpicked file was readable: %q", got)
	}
	h.s.notePick("CustomAppIcon", pic) // its dialog.openFile answered with it
	if got := h.s.imageFile("CustomAppIcon", pic); got == "" {
		t.Error("the file picked for CustomAppIcon isn't its to decode")
	}
	if got := h.s.imageFile("OtherPlugin", pic); got != "" {
		t.Errorf("another plugin may decode CustomAppIcon's pick: %q", got)
	}
	inside := filepath.Join(h.s.dir, "plugins", "x.png")
	if err := os.WriteFile(inside, pngOf(t, testImage(4, 4, 255)), 0o644); err != nil {
		t.Fatal(err)
	}
	h.s.notePick("CustomAppIcon", inside)
	if got := h.s.imageFile("CustomAppIcon", inside); got != "" {
		t.Errorf("a file in Clack's own folders was readable: %q", got)
	}
}
