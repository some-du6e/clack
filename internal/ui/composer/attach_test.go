package composer

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
)

// attFakeHost implements only what attachments calls off the UI goroutine.
type attFakeHost struct {
	popups.Host // nil: any other call panics
	mu          sync.Mutex
	toasts      []string
	inval       atomic.Int32
}

func (h *attFakeHost) Invalidate() { h.inval.Add(1) }
func (h *attFakeHost) Toast(s string) {
	h.mu.Lock()
	h.toasts = append(h.toasts, s)
	h.mu.Unlock()
}

func (h *attFakeHost) takeToasts() []string {
	runRestores()
	h.mu.Lock()
	defer h.mu.Unlock()
	t := h.toasts
	h.toasts = nil
	return t
}

func TestAttKindOf(t *testing.T) {
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		data  []byte
		label string
		color uint32
		image bool
	}{
		{"report.pdf", nil, "PDF", attRed, false},
		{"archive.zip", nil, "Zip", attGray, false},
		{"backup.tar", nil, "TAR", attGray, false},
		{"Photo.PNG", nil, "PNG", attGreen, true},
		{"cat.jpeg", nil, "JPEG", attGreen, true},
		{"essay.docx", nil, "Word Document", attBlue, false},
		{"budget.xlsx", nil, "Excel Spreadsheet", attGreen, false},
		{"main.go", nil, "Go", attTeal, false},
		{"data.xyz", nil, "XYZ", attBlue, false},
		{"README", nil, "File", attBlue, false},
		{"screenshot", pngData.Bytes(), "PNG", attGreen, true}, // sniffed
		{"weird.bin", pngData.Bytes(), "BIN", attGreen, true},  // sniffed, keeps ext label
		{"photo.heic", nil, "HEIC", attGreen, false},           // not decodable: card
	}
	for _, c := range cases {
		k := attKindOf(c.name, c.data)
		if k.Label != c.label || k.Color != c.color || k.Image != c.image {
			t.Errorf("attKindOf(%q) = %+v, want label %q color %06x image %v", c.name, k, c.label, c.color, c.image)
		}
		if k.Icon == "" {
			t.Errorf("attKindOf(%q): no icon", c.name)
		}
	}
}

func TestAttCheckAdd(t *testing.T) {
	cases := []struct {
		count       int
		total, size int64
		want        error
	}{
		{0, 0, 10, nil},
		{0, 0, attMaxFileSize, nil},
		{0, 0, attMaxFileSize + 1, errAttTooBig},
		{attMaxFiles - 1, 0, 1, nil},
		{attMaxFiles, 0, 1, errAttTooMany},
		{1, attMaxTotalSize - 5, 5, nil},
		{1, attMaxTotalSize - 5, 6, errAttTotal},
	}
	for _, c := range cases {
		if got := attCheckAdd(c.count, c.total, c.size); !errors.Is(got, c.want) && got != c.want {
			t.Errorf("attCheckAdd(%d, %d, %d) = %v, want %v", c.count, c.total, c.size, got, c.want)
		}
	}
}

func TestAttAddRejects(t *testing.T) {
	h := &attFakeHost{}
	a := newAttachments(h)
	for i := 0; i < attMaxFiles; i++ {
		if !a.add("f.txt", []byte("x")) {
			t.Fatalf("file %d rejected", i)
		}
	}
	if a.add("one-more.txt", []byte("x")) {
		t.Fatal("11th file accepted")
	}
	if ts := h.takeToasts(); len(ts) != 1 || !strings.Contains(ts[0], "10 files") {
		t.Fatalf("toasts = %q", ts)
	}
	// Size is checked before reading a file into memory.
	if a.precheck("huge.iso", attMaxFileSize+1) {
		t.Fatal("precheck accepted a file over 1 GB")
	}
	if ts := h.takeToasts(); len(ts) != 1 || !strings.Contains(ts[0], "huge.iso is too big") {
		t.Fatalf("toasts = %q", ts)
	}
	if a.Len() != attMaxFiles {
		t.Fatalf("Len = %d", a.Len())
	}
}

func TestAttConcurrentAddFilesClear(t *testing.T) {
	h := &attFakeHost{}
	a := newAttachments(h)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			a.Add("a.txt", []byte("hello"))
		}()
		go func() {
			defer wg.Done()
			_ = a.Files()
			_ = a.Len()
		}()
	}
	wg.Wait()
	if a.Len() != 8 || len(a.Files()) != 8 {
		t.Fatalf("Len = %d, Files = %d", a.Len(), len(a.Files()))
	}
	if f := a.Files()[0]; f.Name != "a.txt" || string(f.Data) != "hello" {
		t.Fatalf("Files()[0] = %+v", f)
	}

	// An upload takes the pending files; files added meanwhile survive the
	// Clear that follows its success.
	a.SetUploading(true)
	if a.Len() != 0 || len(a.Files()) != 0 {
		t.Fatalf("uploading: Len = %d", a.Len())
	}
	a.Add("late.png", []byte("not really a png"))
	a.SetUploading(false)
	a.Clear()
	if fs := a.Files(); len(fs) != 1 || fs[0].Name != "late.png" {
		t.Fatalf("after upload: %+v", fs)
	}
	// A failed upload puts its files back.
	a.SetUploading(true)
	a.SetUploading(false)
	if a.Len() != 1 {
		t.Fatalf("after failed upload: Len = %d", a.Len())
	}
	a.Clear() // the retried batch finished: its file goes
	if a.Len() != 0 || a.total != 0 {
		t.Fatalf("after Clear: Len = %d total = %d", a.Len(), a.total)
	}
	if h.inval.Load() == 0 {
		t.Fatal("mutations didn't invalidate")
	}
}

func TestAttPickImageType(t *testing.T) {
	firefoxImage := "TIMESTAMP\nTARGETS\nMULTIPLE\nSAVE_TARGETS\ntext/html\ntext/_moz_htmlinfo\ntext/_moz_htmlcontext\nimage/png\nimage/jpeg\nimage/bmp\nimage/x-icon\n"
	screenshot := "TIMESTAMP\nTARGETS\nMULTIPLE\nimage/png\n"
	jpegOnly := "TARGETS\nimage/jpeg\n"
	plainText := "TIMESTAMP\nTARGETS\nMULTIPLE\nUTF8_STRING\nCOMPOUND_TEXT\nTEXT\nSTRING\ntext/plain;charset=utf-8\ntext/plain\n"
	libreOffice := "TARGETS\nSAVE_TARGETS\nTIMESTAMP\napplication/x-openoffice-embed-source-xml;windows_formatname=\"Star Embed Source (XML)\"\ntext/rtf\ntext/richtext\ntext/html\nimage/png\nimage/bmp\nUTF8_STRING\n"
	wlPaste := "image/png\nimage/bmp\n"
	empty := ""
	cases := []struct {
		out      string
		explicit bool
		want     string
	}{
		{firefoxImage, false, "image/png"},
		{screenshot, false, "image/png"},
		{jpegOnly, false, "image/jpeg"},
		{plainText, false, ""},
		{plainText, true, ""},
		{libreOffice, false, ""},
		{libreOffice, true, "image/png"},
		{wlPaste, false, "image/png"},
		{empty, true, ""},
		{"  IMAGE/PNG \r\n", false, "IMAGE/PNG"}, // keeps the exact target name
	}
	for i, c := range cases {
		if got := attPickImageType(attParseTargets(c.out), c.explicit); got != c.want {
			t.Errorf("case %d: got %q, want %q", i, got, c.want)
		}
	}
	if got := attParseTargets("a\n\n  b \r\n"); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("attParseTargets = %q", got)
	}
}

func TestAttPastedName(t *testing.T) {
	for mime, want := range map[string]string{
		"image/png": "image.png", "image/jpeg": "image.jpg", "image/jpg": "image.jpg",
		"image/gif": "image.gif", "image/webp": "image.webp", "image/bmp": "image.bmp", "": "image.png",
	} {
		if got := attPastedName(mime); got != want {
			t.Errorf("attPastedName(%q) = %q, want %q", mime, got, want)
		}
	}
}

func TestAttMacClipboard(t *testing.T) {
	info := "«class PNGf», 1234, «class 8BPS», 9000, GIF picture, 800, «class jp2 », 700, JPEG picture, 600, TIFF picture, 50000"
	if !attMacClipHasImage(info, false) {
		t.Error("image clipboard not detected")
	}
	if attMacClipHasImage("«class utf8», 12, «class ut16», 26, string, 12, Unicode text, 24", true) {
		t.Error("text clipboard detected as image")
	}
	word := "«class RTF », 4000, «class utf8», 12, «class PNGf», 9000"
	if attMacClipHasImage(word, false) || !attMacClipHasImage(word, true) {
		t.Error("rich text copy handling")
	}
	b, err := attParseAppleData("«data PNGf89504E470D0A1A0A»\n")
	if err != nil || !bytes.Equal(b, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		t.Errorf("attParseAppleData = %x, %v", b, err)
	}
	for _, bad := range []string{"", "missing value", "«data PNGf»", "«data PNGfZZ»", "«data PNGf89504"} {
		if _, err := attParseAppleData(bad); err == nil {
			t.Errorf("attParseAppleData(%q) accepted", bad)
		}
	}
}

func TestAttPickerOutput(t *testing.T) {
	paths, ok, err := attPickResult([]byte("/home/me/My Files/a b.txt\n/home/me/c.pdf\n\n"), nil)
	if !ok || err != nil || len(paths) != 2 || paths[0] != "/home/me/My Files/a b.txt" || paths[1] != "/home/me/c.pdf" {
		t.Errorf("attPickResult = %q %v %v", paths, ok, err)
	}
	if got := attParseLines("C:\\x.txt\r\n"); len(got) != 1 || got[0] != "C:\\x.txt" {
		t.Errorf("attParseLines = %q", got)
	}
}

func TestAttThumbnail(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 120, 60))
	for y := 0; y < 60; y++ {
		for x := 0; x < 120; x++ {
			c := color.NRGBA{R: 255, A: 255}
			if x >= 30 && x < 90 {
				c = color.NRGBA{B: 255, A: 255} // the center square
			}
			src.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	th, err := attThumbnail(buf.Bytes(), 62)
	if err != nil {
		t.Fatal(err)
	}
	if th.Bounds() != image.Rect(0, 0, 62, 62) {
		t.Fatalf("bounds %v", th.Bounds())
	}
	// Center-cropped: the thumbnail is the blue middle square.
	if c := th.RGBAAt(31, 31); c.B < 200 || c.R > 50 {
		t.Errorf("center pixel %v, want blue", c)
	}
	if _, err := attThumbnail([]byte("not an image"), 62); err == nil {
		t.Error("garbage decoded")
	}
}

func TestAttHumanSize(t *testing.T) {
	for n, want := range map[int64]string{
		0: "0 B", 512: "512 B", 2048: "2 KB", 1536 * 1024: "1.5 MB", 3 << 20: "3 MB", 5 << 30: "5 GB",
	} {
		if got := attHumanSize(n); got != want {
			t.Errorf("attHumanSize(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestAttCleanName(t *testing.T) {
	for in, want := range map[string]string{
		"a.txt": "a.txt", "/tmp/x/b.pdf": "b.pdf", `C:\Users\me\c.png`: "c.png", "": "file", "/": "file",
	} {
		if got := attCleanName(in); got != want {
			t.Errorf("attCleanName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAttReadDir(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"b.txt", "A.md", ".hidden"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "zdir"), 0o700); err != nil {
		t.Fatal(err)
	}
	ents, err := attReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.name)
	}
	if got := strings.Join(names, ","); got != "..,zdir,A.md,b.txt" {
		t.Fatalf("entries = %s", got)
	}
	if !ents[0].up || !ents[1].dir || ents[2].dir || ents[2].size != 1 {
		t.Fatalf("entry flags: %+v", ents)
	}
}
