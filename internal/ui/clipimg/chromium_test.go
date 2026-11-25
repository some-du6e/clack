package clipimg

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
)

// The GIF picker message's image (#emojibot, Karim 2026-11-24) as Slack's
// AttachmentImage shows it with animations off: the proxy's still, with
// the image block's alt_text. Its markup and bookmark are Chromium's for
// that <img> (chromium.go).
const (
	gifSrc = "https://slack-imgs.com/?c=1&o1=ro.gu&url=https%3A%2F%2Fmedia3.giphy.com%2Fmedia%2Fv1.Y2lkPTc5MGI3NjExYWJj%2Fgiphy.webp%3Fcid%3D790b%26ct%3Dg"
	gifAlt = "A test GIF"
)

// URLToImageMarkup: the src and alt base::EscapeForHTML'd (& in the proxy
// URL's query too), alt left out when empty, "/>" closing the tag; no
// markup without a valid absolute URL.
func TestImageMarkup(t *testing.T) {
	want := `<img src="https://slack-imgs.com/?c=1&amp;o1=ro.gu&amp;url=https%3A%2F%2Fmedia3.giphy.com%2Fmedia%2Fv1.Y2lkPTc5MGI3NjExYWJj%2Fgiphy.webp%3Fcid%3D790b%26ct%3Dg" alt="A test GIF"/>`
	if got := imageMarkup(&Tag{URL: gifSrc, Title: gifAlt}); got != want {
		t.Errorf("markup\n %s\nwant\n %s", got, want)
	}
	if got := imageMarkup(&Tag{URL: "https://example.com/a.png"}); got != `<img src="https://example.com/a.png"/>` {
		t.Errorf("no alt: %s", got)
	}
	if got := imageMarkup(&Tag{URL: "https://example.com/a.png", Title: `"Tom" & <Jerry>'s`}); got != `<img src="https://example.com/a.png" alt="&quot;Tom&quot; &amp; &lt;Jerry&gt;&#39;s"/>` {
		t.Errorf("escaped alt: %s", got)
	}
	for _, u := range []string{"", "a.png", "/img/a.png", "https:", "%zz"} {
		if got := imageMarkup(&Tag{URL: u, Title: "x"}); got != "" {
			t.Errorf("%q has markup %s", u, got)
		}
	}
	if imageMarkup(nil) != "" || winURL(nil) != nil || mozURL(nil) != nil {
		t.Error("a nil tag has markup or a bookmark")
	}
}

// HtmlToCFHtml(html, ""): the header's four ten-digit offsets point at
// <html> (105), the fragment's start (141) and end, and the document's
// end; the fragment is the markup itself.
func TestCFHTML(t *testing.T) {
	html := imageMarkup(&Tag{URL: gifSrc, Title: gifAlt})
	cf := string(cfHTML(html))
	off := func(name string) int {
		i := strings.Index(cf, name+":")
		if i < 0 {
			t.Fatalf("no %s in %q", name, cf)
		}
		n, err := strconv.Atoi(cf[i+len(name)+1 : i+len(name)+11])
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return n
	}
	if !strings.HasPrefix(cf, "Version:0.9\r\nStartHTML:0000000105\r\nEndHTML:") {
		t.Fatalf("header %q", cf[:60])
	}
	sh, eh, sf, ef := off("StartHTML"), off("EndHTML"), off("StartFragment"), off("EndFragment")
	if sh != 105 || sf != 141 || ef != sf+len(html) || eh != len(cf) {
		t.Fatalf("offsets %d %d %d %d (len %d, html %d)", sh, eh, sf, ef, len(cf), len(html))
	}
	if cf[sh:sf] != "<html>\r\n<body>\r\n<!--StartFragment-->" || cf[sf:ef] != html || cf[ef:] != "<!--EndFragment-->\r\n</body>\r\n</html>" {
		t.Errorf("document %q", cf[sh:])
	}
}

// The bookmark: Windows' CFSTR_INETURLW is the URL alone in UTF-16 with a
// NUL; Linux's text/x-moz-url is UTF-16 "URL\nTITLE" with none. No
// bookmark for a file: URL (ClipboardHostImpl::WriteBookmark), though it
// still gets markup.
func TestBookmarks(t *testing.T) {
	u16 := func(b []byte) string {
		if len(b)%2 != 0 {
			t.Fatalf("odd UTF-16 length %d", len(b))
		}
		units := make([]uint16, len(b)/2)
		for i := range units {
			units[i] = binary.LittleEndian.Uint16(b[2*i:])
		}
		return string(utf16.Decode(units))
	}
	tag := &Tag{URL: gifSrc, Title: "Ünïcode ✓"}
	if got := u16(winURL(tag)); got != gifSrc+"\x00" {
		t.Errorf("UniformResourceLocatorW %q", got)
	}
	if got := u16(mozURL(tag)); got != gifSrc+"\nÜnïcode ✓" {
		t.Errorf("text/x-moz-url %q", got)
	}
	if got := u16(mozURL(&Tag{URL: gifSrc})); got != gifSrc+"\n" {
		t.Errorf("text/x-moz-url without a title %q", got)
	}
	file := &Tag{URL: "file:///C:/Users/x/a.png", Title: "a"}
	if winURL(file) != nil || mozURL(file) != nil {
		t.Error("a file: URL got a bookmark")
	}
	if imageMarkup(file) == "" {
		t.Error("a file: URL got no markup")
	}
}

// The DIBV5 is CreateDIBV5ImageDataFromN32SkBitmap's: a 124-byte
// BITMAPV5HEADER for a bottom-up 32-bit BI_RGB bitmap with an alpha mask
// in the Windows colour space, then premultiplied BGRA rows.
func TestDIBV5(t *testing.T) {
	h := dibV5Header(220, 260)
	le := binary.LittleEndian
	for _, c := range []struct {
		name     string
		off, got uint32
		want     uint32
	}{
		{"bV5Size", 0, le.Uint32(h[0:]), 124},
		{"bV5Width", 4, le.Uint32(h[4:]), 220},
		{"bV5Height", 8, le.Uint32(h[8:]), 260},
		{"bV5Planes", 12, uint32(le.Uint16(h[12:])), 1},
		{"bV5BitCount", 14, uint32(le.Uint16(h[14:])), 32},
		{"bV5Compression", 16, le.Uint32(h[16:]), 0},
		{"bV5SizeImage", 20, le.Uint32(h[20:]), 0},
		{"bV5RedMask", 40, le.Uint32(h[40:]), 0},
		{"bV5AlphaMask", 52, le.Uint32(h[52:]), 0xff000000},
		{"bV5CSType", 56, le.Uint32(h[56:]), 0x57696e20},
		{"bV5Intent", 108, le.Uint32(h[108:]), 4},
		{"bV5ProfileData", 112, le.Uint32(h[112:]), 0},
	} {
		if c.got != c.want {
			t.Errorf("%s (offset %d) %#x, want %#x", c.name, c.off, c.got, c.want)
		}
	}
	if len(h) != 124 {
		t.Fatalf("header is %d bytes", len(h))
	}
	img := image.NewNRGBA(image.Rect(0, 0, 3, 1))
	img.SetNRGBA(0, 0, color.NRGBA{255, 0, 0, 255})
	img.SetNRGBA(1, 0, color.NRGBA{0, 200, 100, 128}) // premultiplied: 0, 100, 50
	img.SetNRGBA(2, 0, color.NRGBA{10, 20, 30, 0})
	row := make([]byte, 12)
	premulBGRARow(row, img, 0)
	want := []byte{0, 0, 255, 255, 50, 100, 0, 128, 0, 0, 0, 0}
	for i := range want {
		d := int(row[i]) - int(want[i])
		if d < -1 || d > 1 {
			t.Fatalf("row %v, want %v (±1)", row, want)
		}
	}
}

// cfHTML's markup is what reads back: a CF_HTML reader (the composer's
// paste) finds the fragment.
func TestCFHTMLFragment(t *testing.T) {
	html := imageMarkup(&Tag{URL: "https://example.com/a.gif", Title: "a"})
	cf := cfHTML(html)
	i := bytes.Index(cf, []byte("<!--StartFragment-->"))
	j := bytes.Index(cf, []byte("<!--EndFragment-->"))
	if i < 0 || j < 0 || string(cf[i+len("<!--StartFragment-->"):j]) != html {
		t.Errorf("fragment of %q", cf)
	}
}
