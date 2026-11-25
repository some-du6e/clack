//go:build windows

package popups

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"

	"gioui.org/app/winclip"
	"golang.org/x/sys/windows"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/clipimg"
	"github.com/some-du6e/clack/internal/ui/theme"
)

var (
	ctUser32                    = windows.NewLazySystemDLL("user32.dll")
	ctEmptyClipboard            = ctUser32.NewProc("EmptyClipboard")
	ctIsFormatAvailable         = ctUser32.NewProc("IsClipboardFormatAvailable")
	ctRegisterFormat            = ctUser32.NewProc("RegisterClipboardFormatW")
	ctCFDIB             uintptr = 8
)

// ctEmpty empties the clipboard, in a session.
func ctEmpty(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := winclip.Session(ctx, 0, func() error {
		ctEmptyClipboard.Call()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func ctAvailable(f uintptr) bool {
	r, _, _ := ctIsFormatAvailable.Call(f)
	return r != 0
}

// ctSnapshot reads every format on the clipboard, in a session.
func ctSnapshot(t *testing.T) *winclip.Snapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	snap, err := winclip.Read(ctx, func(winclip.Format) int64 { return 64 << 20 })
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

// ctFormats checks snap's formats: want (registered ones by name, the
// others as "#id") in that order, as their owner put them there, then
// only what Windows makes from a CF_DIBV5 (CF_BITMAP, CF_DIB).
func ctFormats(t *testing.T, snap *winclip.Snapshot, want string) {
	t.Helper()
	var all []string
	for _, f := range snap.Formats {
		if f.Name != "" {
			all = append(all, f.Name)
		} else {
			all = append(all, "#"+strconv.Itoa(int(f.ID)))
		}
	}
	n := len(strings.Split(want, "|"))
	if len(all) < n || strings.Join(all[:n], "|") != want {
		t.Fatalf("formats %s, want Chromium's %s first (#17 CF_DIBV5)", strings.Join(all, "|"), want)
	}
	for _, f := range all[n:] {
		if f != "#2" && f != "#8" {
			t.Errorf("format %s after Chromium's (all: %s): only Windows' CF_BITMAP and CF_DIB from the DIBV5", f, strings.Join(all, "|"))
		}
	}
}

// ctUTF16 is s as Windows' UTF-16 text with its NUL.
func ctUTF16(s string) []byte {
	units := append(utf16.Encode([]rune(s)), 0)
	b := make([]byte, 2*len(units))
	for i, u := range units {
		binary.LittleEndian.PutUint16(b[2*i:], u)
	}
	return b
}

// The real clipboard (CI's windows-applog job): Copy Image on the GIF
// picker's image (fakeslack's animated WebP, its <img> as Slack shows it
// with animations off: the proxy's still, the block's alt_text) puts
// there what Slack's desktop app's Chromium does (ClipboardWin, Chromium
// 156: clipimg/chromium.go), in its order: "PNG" and CF_DIBV5 of the
// first frame, "HTML Format" with the <img> markup, and
// "UniformResourceLocatorW" with its src alone; Windows adds CF_DIB and
// CF_BITMAP from the DIBV5. No format holds the WebP itself. A file
// menu's Copy image (Slack's navigator.clipboard.write) puts the bitmap
// alone.
func TestWinCopyImageRoundTrip(t *testing.T) {
	if os.Getenv("CLACK_WINDOWS_CLIPBOARD_TESTS") == "" {
		t.Skip("replaces the clipboard: set CLACK_WINDOWS_CLIPBOARD_TESTS=1")
	}
	ctEmpty(t)
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := core.New(slack.New(fs.Creds()), store.New(""))
	h := &tzHost{a: a, th: theme.New(false)}
	shown := fs.URL + "img/220x260/gp" + fakeslack.TeamID + ".webp"
	still := "https://slack-imgs.com/?c=1&o1=ro.gu&url=" + gpQuery

	NewImageMenuOf(h, image.Pt(10, 10), ImageMenu{Src: still, Alt: gpAlt, Shown: shown, Proxy: true}).Items()[0].Do()
	cfPNG := func() uintptr {
		name, _ := windows.UTF16PtrFromString("PNG")
		f, _, _ := ctRegisterFormat.Call(uintptr(unsafe.Pointer(name)))
		return f
	}()
	deadline := time.Now().Add(20 * time.Second)
	for !ctAvailable(cfPNG) { // one session sets every format
		Pump()
		if ts := h.toastList(); len(ts) > 0 {
			t.Fatalf("Copy Image toasted %q", ts)
		}
		if time.Now().After(deadline) {
			t.Fatal("Copy Image put nothing on the clipboard")
		}
		time.Sleep(20 * time.Millisecond)
	}
	snap := ctSnapshot(t)
	ctFormats(t, snap, "PNG|#17|HTML Format|UniformResourceLocatorW")
	if !ctAvailable(ctCFDIB) {
		t.Error("no CF_DIB (Windows makes it from the CF_DIBV5)")
	}
	bytesOf := func(name string) []byte {
		f, ok := snap.Named(name)
		if !ok {
			t.Fatalf("no %s", name)
		}
		b, _ := snap.Bytes(f.ID)
		return b
	}
	want := fakeslack.GIFPickerFirst

	// "PNG": the first frame
	got, err := png.Decode(bytes.NewReader(bytesOf("PNG")))
	if err != nil {
		t.Fatalf("the clipboard's PNG: %v", err)
	}
	firstFrame(t, got, `"PNG"`)

	// CF_DIBV5: skia's header, then premultiplied BGRA rows bottom-up
	dib, _ := snap.Bytes(17)
	if len(dib) < 124+220*260*4 {
		t.Fatalf("CF_DIBV5 is %d bytes", len(dib))
	}
	le := binary.LittleEndian
	for _, c := range []struct {
		name      string
		got, want uint32
	}{
		{"bV5Size", le.Uint32(dib[0:]), 124}, {"bV5Width", le.Uint32(dib[4:]), 220}, {"bV5Height", le.Uint32(dib[8:]), 260},
		{"bV5Planes", uint32(le.Uint16(dib[12:])), 1}, {"bV5BitCount", uint32(le.Uint16(dib[14:])), 32},
		{"bV5Compression", le.Uint32(dib[16:]), 0}, {"bV5SizeImage", le.Uint32(dib[20:]), 0},
		{"bV5RedMask", le.Uint32(dib[40:]), 0}, {"bV5AlphaMask", le.Uint32(dib[52:]), 0xff000000},
		{"bV5CSType", le.Uint32(dib[56:]), 0x57696e20}, {"bV5Intent", le.Uint32(dib[108:]), 4},
	} {
		if c.got != c.want {
			t.Errorf("CF_DIBV5 %s %#x, want %#x", c.name, c.got, c.want)
		}
	}
	top := 124 + 259*220*4 // the image's top row is the DIB's last
	if px := dib[top : top+4]; px[0] != want.B || px[1] != want.G || px[2] != want.R || px[3] != want.A {
		t.Errorf("CF_DIBV5 top-left BGRA %v, want the first frame's %d %d %d %d", px, want.B, want.G, want.R, want.A)
	}

	// "HTML Format": HtmlToCFHtml of the <img> markup, NUL-terminated
	markup := `<img src="https://slack-imgs.com/?c=1&amp;o1=ro.gu&amp;url=` + gpQuery + `" alt="` + gpAlt + `"/>`
	sf := 105 + len("<html>\r\n<body>\r\n<!--StartFragment-->")
	ef := sf + len(markup)
	eh := ef + len("<!--EndFragment-->\r\n</body>\r\n</html>")
	cf := fmt.Sprintf("Version:0.9\r\nStartHTML:%010d\r\nEndHTML:%010d\r\nStartFragment:%010d\r\nEndFragment:%010d\r\n", 105, eh, sf, ef) +
		"<html>\r\n<body>\r\n<!--StartFragment-->" + markup + "<!--EndFragment-->\r\n</body>\r\n</html>\x00"
	if b := bytesOf("HTML Format"); !bytes.HasPrefix(b, []byte(cf)) {
		t.Errorf("HTML Format\n %q\nwant\n %q", b, cf)
	}
	// "UniformResourceLocatorW": the URL alone, UTF-16 with its NUL
	if b, w := bytesOf("UniformResourceLocatorW"), ctUTF16(still); !bytes.HasPrefix(b, w) {
		t.Errorf("UniformResourceLocatorW %q, want %q", b, w)
	}

	// the bitmap alone (a file menu's Copy image, clipimg.WriteCharged)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := clipimg.WriteCharged(ctx, got, nil); err != nil {
		t.Fatal(err)
	}
	ctFormats(t, ctSnapshot(t), "PNG|#17") // the bitmap alone
}
