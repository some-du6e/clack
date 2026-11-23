package composer

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
)

// noPanic runs f, failing t if it panics.
func noPanic(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s: panicked: %v", name, r)
		}
	}()
	f()
}

// cfHeader is a CF_HTML header with the four offsets as given.
func cfHeader(sh, eh, sf, ef int) string {
	return fmt.Sprintf("Version:0.9\r\nStartHTML:%010d\r\nEndHTML:%010d\r\nStartFragment:%010d\r\nEndFragment:%010d\r\n", sh, eh, sf, ef)
}

// "HTML Format" as another app might leave it: offsets outside the data,
// backwards, negative, a header with nothing after it. Each has a defined
// result (the document, the fragment, what follows the header, or
// nothing), never a slice past the bytes.
func TestClipMalformedHTMLFormat(t *testing.T) {
	doc := "<html><body><!--StartFragment--><b>hi</b><!--EndFragment--></body></html>"
	h := cfHeader(0, 0, 0, 0)
	n := len(h)
	good := cfHeader(n, n+len(doc), n+strings.Index(doc, "<b>"), n+strings.Index(doc, "<!--EndF")) + doc
	if got := string(cfHTML([]byte(good))); got != doc {
		t.Fatalf("the well-formed data: %q", got)
	}
	for _, c := range []struct {
		name, data, want string
	}{
		{"nothing", "", ""},
		{"only NULs", "\x00\x00\x00", ""},
		{"a header and nothing after it", h, ""},
		{"offsets past the end", cfHeader(n, n+99999, n+5, n+99998) + doc, doc},
		{"a document backwards, a good fragment", cfHeader(n+50, n+10, n+strings.Index(doc, "<b>"), n+strings.Index(doc, "<!--EndF")) + doc, "<b>hi</b>"},
		{"negative offsets (-1: none)", strings.ReplaceAll(cfHeader(1, 1, 1, 1), "0000000001", "-000000001") + doc, doc},
		{"huge offsets", strings.ReplaceAll(cfHeader(1, 1, 1, 1), "0000000001", "9999999999") + doc, doc},
		{"no header at all", doc, doc},
		{"trailing NULs", good + "\x00\x00", doc},
	} {
		noPanic(t, c.name, func() {
			if got := string(cfHTML([]byte(c.data))); got != c.want {
				t.Errorf("%s: %q, want %q", c.name, got, c.want)
			}
		})
	}
}

// A CF_HDROP (DROPFILES, then the paths) as another app might leave it:
// short, its offset outside it, a last path with no terminator, an odd
// length. Each gives the paths it holds whole, never a read past it.
func TestClipMalformedHDROP(t *testing.T) {
	le := binary.LittleEndian
	drop := func(off uint32, wide bool, body []byte) []byte {
		b := make([]byte, 20)
		le.PutUint32(b, off)
		if wide {
			le.PutUint32(b[16:], 1)
		}
		return append(b, body...)
	}
	w := func(s string) []byte {
		var b []byte
		for _, r := range s {
			b = le.AppendUint16(b, uint16(r))
		}
		return b
	}
	two := append(append(append(w(`C:\a.txt`), 0, 0), append(w(`C:\b.txt`), 0, 0)...), 0, 0)
	for _, c := range []struct {
		name string
		b    []byte
		want []string
	}{
		{"well formed", drop(20, true, two), []string{`C:\a.txt`, `C:\b.txt`}},
		{"under its header", []byte{20, 0, 0}, nil},
		{"an offset past the end", drop(9999, true, two), nil},
		{"an offset inside the header", drop(4, true, two), nil},
		{"the last path unterminated", drop(20, true, w(`C:\a.txt`)), nil},
		{"an odd length", drop(20, true, append(append(w(`C:\a.txt`), 0, 0), 'x')), []string{`C:\a.txt`}},
		{"ANSI paths, the last unterminated", drop(20, false, []byte("C:\\a.txt\x00C:\\b")), []string{`C:\a.txt`}},
	} {
		noPanic(t, c.name, func() {
			got := attParseHDROP(c.b)
			if fmt.Sprint(got) != fmt.Sprint(c.want) {
				t.Errorf("%s: %q, want %q", c.name, got, c.want)
			}
		})
	}
}

// A packed DIB as another app might leave it: short, a header size past
// it, sizes negative or huge, a palette or pixels cut short, a BI_PNG
// whose PNG isn't there. Each is an error, never a read past it.
func TestClipMalformedDIB(t *testing.T) {
	le := binary.LittleEndian
	dib := func(hs uint32, w, h int32, bpp uint16, comp uint32, clrUsed uint32, extra int) []byte {
		b := make([]byte, 40+extra)
		le.PutUint32(b, hs)
		le.PutUint32(b[4:], uint32(w))
		le.PutUint32(b[8:], uint32(h))
		le.PutUint16(b[12:], 1)
		le.PutUint16(b[14:], bpp)
		le.PutUint32(b[16:], comp)
		le.PutUint32(b[32:], clrUsed)
		return b
	}
	if _, err := attDIBToPNG(dib(40, 2, 2, 32, attBIRGB, 0, 16)); err != nil {
		t.Fatalf("a well-formed 2x2: %v", err)
	}
	for _, c := range []struct {
		name string
		b    []byte
	}{
		{"nothing", nil},
		{"under a header", make([]byte, 39)},
		{"a header size past the end", dib(400, 2, 2, 32, attBIRGB, 0, 16)},
		{"a header size under 40", dib(12, 2, 2, 32, attBIRGB, 0, 16)},
		{"a width of 0", dib(40, 0, 2, 32, attBIRGB, 0, 16)},
		{"a negative width", dib(40, -2, 2, 32, attBIRGB, 0, 16)},
		{"huge sizes", dib(40, 1<<20, 1<<20, 32, attBIRGB, 0, 16)},
		{"its pixels cut short", dib(40, 2, 2, 32, attBIRGB, 0, 15)},
		{"a palette past the end", dib(40, 2, 2, 8, attBIRGB, 256, 8)},
		{"masks past the end", dib(40, 2, 2, 32, attBIBitfields, 0, 4)},
		{"24 bits with masks", dib(40, 2, 2, 24, attBIBitfields, 0, 64)},
		{"an odd bit depth", dib(40, 2, 2, 7, attBIRGB, 0, 64)},
		{"a BI_PNG without its PNG", dib(40, 2, 2, 0, attBIPNG, 0, 16)},
	} {
		noPanic(t, c.name, func() {
			if p, err := attDIBToPNG(c.b); err == nil {
				t.Errorf("%s: %d bytes of PNG, want an error", c.name, len(p))
			}
		})
	}
}
