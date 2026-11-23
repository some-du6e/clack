package slick

import (
	"bytes"
	"debug/pe"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	xdraw "golang.org/x/image/draw"
)

// Electron's nativeImage for main halves (30_main.js nativeImage): an image
// crosses to JS as PNG (base64) with its size, and every operation on it
// (decoding a file or a data URL, resizing, its bitmap) is done here, as
// Electron's are in Chromium's Skia. Formats are Electron's: PNG and JPEG
// from a path or a data URL; a thumbnail of an icon file (an .icns, an
// .exe's icon resources) as createThumbnailFromPath reads one.

// nativeImg is an image as main halves hold it.
type nativeImg struct {
	PNG string `json:"png"` // base64
	W   int    `json:"w"`
	H   int    `json:"h"`
}

// maxNativeImage bounds what an image may be (Electron's are a renderer's
// bitmaps): a side past it, or a file past maxImageFile, is an empty image.
const (
	maxNativeImage = 8192
	maxImageFile   = 64 << 20
)

var errNoImage = errors.New("slick: not an image Electron reads")

func toNRGBA(m image.Image) *image.NRGBA {
	if n, ok := m.(*image.NRGBA); ok && n.Rect.Min == (image.Point{}) {
		return n
	}
	b := m.Bounds()
	n := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	xdraw.Draw(n, n.Bounds(), m, b.Min, xdraw.Src)
	return n
}

// encodeNative is m as a main half's image.
func encodeNative(m image.Image) (*nativeImg, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, m); err != nil {
		return nil, err
	}
	b := m.Bounds()
	return &nativeImg{PNG: base64.StdEncoding.EncodeToString(buf.Bytes()), W: b.Dx(), H: b.Dy()}, nil
}

// decodeNative is a main half's image (its PNG, base64) decoded.
func decodeNative(b64 string) (*image.NRGBA, error) {
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	m, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	return toNRGBA(m), nil
}

// decodeImageBytes is PNG or JPEG bytes as nativeImage reads them.
func decodeImageBytes(b []byte) (image.Image, error) {
	var m image.Image
	var err error
	switch {
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		m, err = png.Decode(bytes.NewReader(b))
	case bytes.HasPrefix(b, []byte{0xff, 0xd8, 0xff}):
		m, err = jpeg.Decode(bytes.NewReader(b))
	default:
		return nil, errNoImage
	}
	if err != nil {
		return nil, err
	}
	if s := m.Bounds().Size(); s.X <= 0 || s.Y <= 0 || s.X > maxNativeImage || s.Y > maxNativeImage {
		return nil, errNoImage
	}
	return m, nil
}

// nativeFromDataURL is nativeImage.createFromDataURL: a data: URL of a
// PNG or JPEG, base64 or not. Anything else is the empty image (nil).
func nativeFromDataURL(u string) *nativeImg {
	head, data, ok := strings.Cut(u, ",")
	if !ok || !strings.HasPrefix(head, "data:") {
		return nil
	}
	var b []byte
	if strings.HasSuffix(head, ";base64") {
		var err error
		if b, err = base64.StdEncoding.DecodeString(data); err != nil {
			return nil
		}
	} else {
		b = []byte(data)
	}
	m, err := decodeImageBytes(b)
	if err != nil {
		return nil
	}
	n, err := encodeNative(m)
	if err != nil {
		return nil
	}
	return n
}

func readImageFile(path string) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > maxImageFile {
		return nil, errNoImage
	}
	return os.ReadFile(path)
}

// nativeFromPath is nativeImage.createFromPath: a PNG or JPEG file. A path
// that doesn't exist, can't be read or isn't such an image is the empty
// image (nil).
func nativeFromPath(path string) *nativeImg {
	b, err := readImageFile(path)
	if err != nil {
		return nil
	}
	m, err := decodeImageBytes(b)
	if err != nil {
		return nil
	}
	n, err := encodeNative(m)
	if err != nil {
		return nil
	}
	return n
}

// nativeThumbnail is nativeImage.createThumbnailFromPath (macOS and
// Windows): the file's image at most w × h, its aspect kept. An icon file
// (an .icns, an .exe or .dll's icon resources) gives its largest icon,
// an image file itself. A folder (an .app bundle, whose thumbnail is the
// system's own rendering of its icon) has none here: the error, as
// Electron's when the system gives no thumbnail.
func nativeThumbnail(path string, w, h int) (*nativeImg, error) {
	if w <= 0 || h <= 0 {
		return nil, errors.New("slick: a thumbnail needs a size")
	}
	b, err := readImageFile(path)
	if err != nil {
		return nil, errors.New("Failed to get thumbnail from local thumbnail cache")
	}
	var m image.Image
	switch ext := strings.ToLower(filepath.Ext(path)); {
	case bytes.HasPrefix(b, []byte("icns")):
		m, err = largestICNS(b)
	case bytes.HasPrefix(b, []byte("MZ")) && (ext == ".exe" || ext == ".dll"):
		m, err = largestPEIcon(b)
	default:
		m, err = decodeImageBytes(b)
	}
	if err != nil || m == nil {
		return nil, errors.New("Failed to get thumbnail from local thumbnail cache")
	}
	return encodeNative(fitWithin(m, w, h))
}

// fitWithin is m scaled down, its aspect kept, to fit w × h.
func fitWithin(m image.Image, w, h int) image.Image {
	s := m.Bounds().Size()
	if s.X <= w && s.Y <= h {
		return m
	}
	rw, rh := w, s.Y*w/s.X
	if rh > h {
		rw, rh = s.X*h/s.Y, h
	}
	return scaled(m, max(rw, 1), max(rh, 1), "best")
}

// scaled is m at w × h: Catmull-Rom for 'best' and 'better', bilinear for
// 'good' (Electron's resize qualities).
func scaled(m image.Image, w, h int, quality string) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	var k xdraw.Interpolator = xdraw.CatmullRom
	if quality == "good" {
		k = xdraw.ApproxBiLinear
	}
	k.Scale(dst, dst.Bounds(), m, m.Bounds(), xdraw.Src, nil)
	return dst
}

// nativeResize is image.resize({width, height, quality}): a side left out
// (0) keeps the aspect; with neither, the image as it is.
func nativeResize(img *nativeImg, w, h int, quality string) (*nativeImg, error) {
	m, err := decodeNative(img.PNG)
	if err != nil {
		return nil, err
	}
	s := m.Bounds().Size()
	switch {
	case w <= 0 && h <= 0:
		return img, nil
	case w <= 0:
		w = max(1, s.X*h/max(s.Y, 1))
	case h <= 0:
		h = max(1, s.Y*w/max(s.X, 1))
	}
	if w > maxNativeImage || h > maxNativeImage {
		return nil, errNoImage
	}
	return encodeNative(scaled(m, w, h, quality))
}

// nativeBitmap is image.toBitmap(): its pixels as Skia's N32 holds them on
// little-endian machines, B G R A premultiplied, row after row.
func nativeBitmap(img *nativeImg) ([]byte, error) {
	m, err := decodeNative(img.PNG)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(m.Pix))
	for i := 0; i+3 < len(m.Pix); i += 4 {
		r, g, b, a := uint32(m.Pix[i]), uint32(m.Pix[i+1]), uint32(m.Pix[i+2]), uint32(m.Pix[i+3])
		out[i] = byte((b*a + 127) / 255)
		out[i+1] = byte((g*a + 127) / 255)
		out[i+2] = byte((r*a + 127) / 255)
		out[i+3] = byte(a)
	}
	return out, nil
}

// nativeFromBitmap is nativeImage.createFromBitmap(buffer, {width,
// height}): toBitmap's format back. A buffer that isn't width × height × 4
// bytes is an error, as Electron's.
func nativeFromBitmap(bgra []byte, w, h int) (*nativeImg, error) {
	if w <= 0 || h <= 0 || w > maxNativeImage || h > maxNativeImage || len(bgra) != w*h*4 {
		return nil, errors.New("invalid buffer size")
	}
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i+3 < len(bgra); i += 4 {
		a := uint32(bgra[i+3])
		un := func(c byte) byte {
			if a == 0 {
				return 0
			}
			return byte(min(255, (uint32(c)*255+a/2)/a))
		}
		m.Pix[i], m.Pix[i+1], m.Pix[i+2], m.Pix[i+3] = un(bgra[i+2]), un(bgra[i+1]), un(bgra[i]), byte(a)
	}
	return encodeNative(m)
}

// ---- icon files ----

// largestICNS is an .icns file's largest PNG icon (the ic07 to ic14 and
// icp4 to icp6 entries macOS writes as PNG since 10.7).
func largestICNS(b []byte) (image.Image, error) {
	if len(b) < 8 || string(b[:4]) != "icns" {
		return nil, errNoImage
	}
	end := min(int(binary.BigEndian.Uint32(b[4:8])), len(b))
	var best image.Image
	for off := 8; off+8 <= end; {
		n := int(binary.BigEndian.Uint32(b[off+4 : off+8]))
		if n < 8 || off+n > end {
			break
		}
		if data := b[off+8 : off+n]; bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
			if m, err := decodeImageBytes(data); err == nil && (best == nil || m.Bounds().Dx() > best.Bounds().Dx()) {
				best = m
			}
		}
		off += n
	}
	if best == nil {
		return nil, errNoImage
	}
	return best, nil
}

// PE resource types: RT_ICON, RT_GROUP_ICON.
const (
	rtIcon      = 3
	rtGroupIcon = 14
)

// largestPEIcon is an .exe's (or .dll's) first icon group's largest icon,
// as the shell's thumbnail of the file shows it: a PNG entry (Vista's 256px
// icons) or a 32-bit DIB with its alpha.
func largestPEIcon(b []byte) (image.Image, error) {
	f, err := pe.NewFile(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sec := f.Section(".rsrc")
	if sec == nil {
		return nil, errNoImage
	}
	rsrc, err := sec.Data()
	if err != nil {
		return nil, err
	}
	return iconFromResources(rsrc, sec.VirtualAddress)
}

// iconFromResources is largestPEIcon's work on the .rsrc section's bytes
// (va: the section's RVA).
func iconFromResources(rsrc []byte, va uint32) (image.Image, error) {
	r := peResources{data: rsrc, va: va}
	groups := r.entries(0, rtGroupIcon)
	if len(groups) == 0 {
		return nil, errNoImage
	}
	dir, ok := r.firstData(groups[0])
	if !ok || len(dir) < 6 || binary.LittleEndian.Uint16(dir[2:4]) != 1 {
		return nil, errNoImage
	}
	type cand struct {
		w  int
		id uint16
	}
	var cands []cand
	for i, n := 0, int(binary.LittleEndian.Uint16(dir[4:6])); i < n && 6+14*(i+1) <= len(dir); i++ {
		e := dir[6+14*i:]
		w := int(e[0])
		if w == 0 {
			w = 256
		}
		cands = append(cands, cand{w, binary.LittleEndian.Uint16(e[12:14])})
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].w > cands[j].w })
	icons := map[uint16]uint32{}
	for _, e := range r.entries(0, rtIcon) {
		icons[uint16(e.id)] = e.off
	}
	for _, c := range cands {
		off, ok := icons[c.id]
		if !ok {
			continue
		}
		data, ok := r.firstData(peEntry{off: off})
		if !ok {
			continue
		}
		if bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
			if m, err := decodeImageBytes(data); err == nil {
				return m, nil
			}
			continue
		}
		if m, ok := decodeIconDIB(data); ok {
			return m, nil
		}
	}
	return nil, errNoImage
}

// peResources walks an .rsrc section's directory tree.
type peResources struct {
	data []byte
	va   uint32 // the section's RVA: data entries point at RVAs
}

type peEntry struct {
	id  uint32 // the entry's integer id (named entries are left out)
	off uint32 // what it points at, the subdirectory flag kept
}

// entries is the entries under a directory at off, or under its entry
// with id when id > 0 (a level down).
func (r peResources) entries(off uint32, id uint32) []peEntry {
	dir := r.dir(off)
	if id == 0 {
		return dir
	}
	for _, e := range dir {
		if e.id == id && e.off&0x80000000 != 0 {
			return r.dir(e.off &^ 0x80000000)
		}
	}
	return nil
}

func (r peResources) dir(off uint32) []peEntry {
	d := r.data
	if int(off)+16 > len(d) {
		return nil
	}
	named := int(binary.LittleEndian.Uint16(d[off+12:]))
	ids := int(binary.LittleEndian.Uint16(d[off+14:]))
	var out []peEntry
	for i := 0; i < named+ids; i++ {
		e := int(off) + 16 + 8*i
		if e+8 > len(d) {
			break
		}
		name := binary.LittleEndian.Uint32(d[e:])
		if name&0x80000000 != 0 {
			continue // a named entry
		}
		out = append(out, peEntry{id: name, off: binary.LittleEndian.Uint32(d[e+4:])})
	}
	return out
}

// firstData follows an entry's first subdirectories down to its data (an
// icon's or a group's first language).
func (r peResources) firstData(e peEntry) ([]byte, bool) {
	off := e.off
	for depth := 0; off&0x80000000 != 0; depth++ {
		sub := r.dir(off &^ 0x80000000)
		if len(sub) == 0 || depth > 4 {
			return nil, false
		}
		off = sub[0].off
	}
	d := r.data
	if int(off)+16 > len(d) {
		return nil, false
	}
	rva, size := binary.LittleEndian.Uint32(d[off:]), binary.LittleEndian.Uint32(d[off+4:])
	if rva < r.va {
		return nil, false
	}
	start := uint64(rva - r.va)
	if start+uint64(size) > uint64(len(d)) {
		return nil, false
	}
	return d[start : start+uint64(size)], true
}

// decodeIconDIB is an icon resource's 32-bit DIB: a BITMAPINFOHEADER whose
// height counts the AND mask too, then the pixels bottom up, BGRA.
func decodeIconDIB(b []byte) (image.Image, bool) {
	if len(b) < 40 || binary.LittleEndian.Uint32(b) < 40 {
		return nil, false
	}
	hdr := int(binary.LittleEndian.Uint32(b))
	w := int(int32(binary.LittleEndian.Uint32(b[4:])))
	h := int(int32(binary.LittleEndian.Uint32(b[8:]))) / 2
	if bpp := binary.LittleEndian.Uint16(b[14:]); bpp != 32 || w <= 0 || h <= 0 || w > 1024 || h > 1024 {
		return nil, false
	}
	px := b[hdr:]
	if len(px) < w*h*4 {
		return nil, false
	}
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		row := px[(h-1-y)*w*4:]
		for x := 0; x < w; x++ {
			p := row[x*4:]
			m.SetNRGBA(x, y, color.NRGBA{R: p[2], G: p[1], B: p[0], A: p[3]})
		}
	}
	return m, true
}

// slackResources is process.resourcesPath for main halves: the installed
// Slack's resources folder, where Slick's main halves read Slack's own
// files (CustomAppIcon's Slack icon, so Slick never ships Slack's logo).
// "" without a Slack install Clack knows of.
func slackResources() string {
	var try []string
	switch runtime.GOOS {
	case "darwin":
		try = append(try, "/Applications/Slack.app/Contents/Resources")
		if home, err := os.UserHomeDir(); err == nil {
			try = append(try, filepath.Join(home, "Applications", "Slack.app", "Contents", "Resources"))
		}
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			// Squirrel's per-user install: app-<version> folders, the newest last
			if dirs, _ := filepath.Glob(filepath.Join(local, "slack", "app-*")); len(dirs) > 0 {
				sort.Strings(dirs)
				try = append(try, filepath.Join(dirs[len(dirs)-1], "resources"))
			}
		}
		for _, pf := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
			if pf != "" {
				try = append(try, filepath.Join(pf, "Slack", "resources"))
			}
		}
	default:
		try = append(try, "/usr/lib/slack/resources", "/opt/Slack/resources")
	}
	for _, p := range try {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
	}
	return ""
}
