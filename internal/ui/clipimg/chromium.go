package clipimg

import (
	"encoding/binary"
	"fmt"
	"image"
	"net/url"
	"strings"
	"unicode/utf16"
)

// What Slack's desktop app puts on the clipboard for an image, as its
// Chromium writes it (Electron 45.3.2, Chromium 156.0.8078.100: Slack
// 4.53.292). Two writes reach it:
//
//   - Copy Image, the desktop shell's image menu (buildMenuForImage's
//     addImageItems: webContents.copyImageAt). Blink's
//     LocalFrame::CopyImageAtViewportPoint hit-tests the point,
//     Editor::CopyImage takes the <img> there (an <img>'s LayoutImage
//     only: a CSS background is no image), and
//     SystemClipboard::WriteImageWithTag writes its bitmap, then, when
//     the src attribute completes to a valid URL, a bookmark of it (not on
//     macOS: crbug.com/838808) and URLToImageMarkup's
//     `<img src="URL" alt="ALT"/>`. ALT is the <img>'s alt attribute
//     (HitTestResult::AltDisplayString), left out when empty. WriteTagged,
//     with the Tag.
//   - Slack's own file menu's "Copy image" (copyImageToClipboardWithToast:
//     navigator.clipboard.write with an image/png ClipboardItem): Blink's
//     ClipboardImageWriter decodes the PNG and writes the bitmap alone
//     (SystemClipboard::WriteImage). WriteCharged.
//
// The bitmap is the image's first frame however far an animation has
// played: PaintImageForCurrentFrame is the decoder's whole image and
// GetSwSkImage decodes it at kDefaultFrameIndex (cc/paint/paint_image.cc).
// No format holds the image file's own bytes, so an animated GIF or WebP
// keeps its animation only through the URL in the markup and the
// bookmark.
//
// ui/base/clipboard writes, in this order (objects in Clipboard::Data's
// variant order: bitmap, HTML, URL):
//
//   - Windows (clipboard_win.cc): the registered "PNG", then CF_DIBV5
//     (Windows makes CF_DIB and CF_BITMAP from it), "HTML Format" (CF_HTML,
//     HtmlToCFHtml with no source URL) and "UniformResourceLocatorW" (the
//     URL alone, UTF-16).
//   - macOS (clipboard_mac.mm): one pasteboard item with public.png (an
//     NSPasteboardItem given to writeObjects:, which the pasteboard also
//     offers as TIFF), then public.html: "<meta charset='utf-8'>" and the
//     markup.
//   - Linux (clipboard_ozone.cc): image/png, text/html (the markup) and
//     text/x-moz-url (UTF-16: the URL, "\n", the title).
//
// Chromium's browser process also adds two formats of its own to every
// write: the source frame's token (SourceRFHTokenType) and the main
// frame's URL (the data source endpoint: "Chromium internal source URL",
// org.chromium.source-url, chromium/x-source-url). Only Chromium reads
// them, to tell its own frames apart for paste policies; they name a
// RenderFrameHost and a page Clack doesn't have, so Clack writes neither.

// Tag is the <img> a Copy Image copies, as WriteImageWithTag writes it
// beside the bitmap.
type Tag struct {
	// URL is the <img>'s src, completed (what Copy Image URL copies).
	URL string
	// Title is its alt attribute ("" for none).
	Title string
}

// tagURL is t's URL when Chromium writes the markup and bookmark for it
// (Blink's url.IsValid() && !url.IsEmpty(): an absolute URL), else "".
func tagURL(t *Tag) string {
	if t == nil || t.URL == "" {
		return ""
	}
	u, err := url.Parse(t.URL)
	if err != nil || u.Scheme == "" || (u.Opaque == "" && u.Host == "" && u.Path == "") {
		return ""
	}
	return t.URL
}

// bookmarkURL is the URL ClipboardHostImpl::WriteBookmark keeps of t ("" for
// none: no markup URL, or a file: URL, which a renderer may not put on the
// clipboard as a bookmark).
func bookmarkURL(t *Tag) string {
	u := tagURL(t)
	if u == "" || strings.HasPrefix(strings.ToLower(u), "file:") {
		return ""
	}
	return u
}

// escapeHTML is base::EscapeForHTML: < > & " ' as entities.
func escapeHTML(s string) string {
	if !strings.ContainsAny(s, `<>&"'`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 16)
	for _, r := range s {
		switch r {
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '&':
			b.WriteString("&amp;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&#39;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// imageMarkup is Blink's URLToImageMarkup (clipboard_utilities.cc) for t:
// `<img src="URL"/>`, with ` alt="TITLE"` before the "/>" when there is
// a title; "" when Chromium writes no markup for t.
func imageMarkup(t *Tag) string {
	u := tagURL(t)
	if u == "" {
		return ""
	}
	s := `<img src="` + escapeHTML(u) + `"`
	if t.Title != "" {
		s += ` alt="` + escapeHTML(t.Title) + `"`
	}
	return s + "/>"
}

// macHTMLPrefix is what ClipboardMac::WriteHTML puts before the markup,
// marking it UTF-8 (crbug.com/40759159).
const macHTMLPrefix = "<meta charset='utf-8'>"

// cfHTML is ui::clipboard_util::HtmlToCFHtml(html, "") (clipboard_util_win.cc):
// the CF_HTML header with ten-digit offsets, then html between
// <!--StartFragment--> and <!--EndFragment--> in a <html><body>. The
// clipboard's copy ends with a NUL (CreateGlobalData), which isn't here.
func cfHTML(html string) []byte {
	const (
		header      = "Version:0.9\r\nStartHTML:%010d\r\nEndHTML:%010d\r\nStartFragment:%010d\r\nEndFragment:%010d\r\n"
		headerLen   = len("Version:0.9\r\nStartHTML:\r\nEndHTML:\r\nStartFragment:\r\nEndFragment:\r\n") + 4*10
		startMarkup = "<html>\r\n<body>\r\n"
		endMarkup   = "\r\n</body>\r\n</html>"
		startFrag   = "<!--StartFragment-->"
		endFrag     = "<!--EndFragment-->"
	)
	startHTML := headerLen
	startFragment := startHTML + len(startMarkup) + len(startFrag)
	endFragment := startFragment + len(html)
	endHTML := endFragment + len(endFrag) + len(endMarkup)
	s := fmt.Sprintf(header, startHTML, endHTML, startFragment, endFragment) +
		startMarkup + startFrag + html + endFrag + endMarkup
	return []byte(s)
}

// utf16LE is s as UTF-16 code units, little-endian (Windows' and the
// x86/arm Linux hosts' byte order), with a NUL unit after when z.
func utf16LE(s string, z bool) []byte {
	units := utf16.Encode([]rune(s))
	if z {
		units = append(units, 0)
	}
	b := make([]byte, 2*len(units))
	for i, u := range units {
		binary.LittleEndian.PutUint16(b[2*i:], u)
	}
	return b
}

// winURL is ClipboardWin::WriteURL's CFSTR_INETURLW
// ("UniformResourceLocatorW"): the URL alone (no title), UTF-16 with a
// NUL; nil when Chromium writes no bookmark for t.
func winURL(t *Tag) []byte {
	u := bookmarkURL(t)
	if u == "" {
		return nil
	}
	return utf16LE(u, true)
}

// mozURL is ClipboardOzone::WriteURL's text/x-moz-url: UTF-16, the URL, a
// newline and the title, no NUL; nil when Chromium writes no bookmark
// for t.
func mozURL(t *Tag) []byte {
	u := bookmarkURL(t)
	if u == "" {
		return nil
	}
	return utf16LE(u+"\n"+t.Title, false)
}

// dibV5HeaderSize is sizeof(BITMAPV5HEADER).
const dibV5HeaderSize = 124

// dibV5Header is skia::CreateDIBV5ImageDataFromN32SkBitmap's header
// (CreateBitmapV5HeaderForARGB8888, the rest zero: GHND): a w x h
// bottom-up 32-bit BI_RGB bitmap with an alpha mask, in the Windows
// colour space.
func dibV5Header(w, h int) []byte {
	const (
		lcsWindowsColorSpace = 0x57696e20 // 'Win '
		lcsGMImages          = 4
	)
	b := make([]byte, dibV5HeaderSize)
	le := binary.LittleEndian
	le.PutUint32(b[0:], dibV5HeaderSize) // bV5Size
	le.PutUint32(b[4:], uint32(w))       // bV5Width
	le.PutUint32(b[8:], uint32(h))       // bV5Height: positive, bottom-up
	le.PutUint16(b[12:], 1)              // bV5Planes
	le.PutUint16(b[14:], 32)             // bV5BitCount
	// bV5Compression BI_RGB (0); bV5SizeImage 0 (it isn't set); the
	// red, green and blue masks 0
	le.PutUint32(b[52:], 0xff000000) // bV5AlphaMask
	le.PutUint32(b[56:], lcsWindowsColorSpace)
	le.PutUint32(b[108:], lcsGMImages) // bV5Intent
	return b
}

// premulBGRARow is row y of img as the DIBV5's pixels: N32 (BGRA on
// Windows) premultiplied, what Skia decodes an image to and the DIBV5
// copies (bitmap.readPixels into the same alpha type).
func premulBGRARow(row []byte, img image.Image, y int) {
	b := img.Bounds()
	put := func(x int, r, g, bl, a uint32) {
		row[x*4+0] = byte(bl >> 8)
		row[x*4+1] = byte(g >> 8)
		row[x*4+2] = byte(r >> 8)
		row[x*4+3] = byte(a >> 8)
	}
	if m, ok := img.(image.RGBA64Image); ok {
		for x := 0; x < b.Dx(); x++ {
			c := m.RGBA64At(b.Min.X+x, y)
			put(x, uint32(c.R), uint32(c.G), uint32(c.B), uint32(c.A))
		}
		return
	}
	for x := 0; x < b.Dx(); x++ {
		r, g, bl, a := img.At(b.Min.X+x, y).RGBA() // premultiplied, 16-bit
		put(x, r, g, bl, a)
	}
}
