// Package richclip puts a copy on the system clipboard the way Slack's
// desktop app (Electron, so Chromium) does, and reads it back: plain
// text, HTML, and the custom MIME types Slack's copy handlers set with
// clipboardData.setData ("slack/html", "slack/texty", "text/markdown"),
// which Chromium keeps in its web custom data format beside the standard
// ones. Slack's composer reads those on paste, so a mention copied in
// Slack (or Clack) pastes as a mention in Slack (or Clack).
//
// Where Chromium keeps the custom types (ui/base/clipboard):
//
//   - Windows: the registered format "Chromium Web Custom MIME Data
//     Format"; HTML is "HTML Format" (CF_HTML), text CF_UNICODETEXT.
//   - macOS: the pasteboard type "org.chromium.web-custom-data"; HTML is
//     public.html, text public.utf8-plain-text.
//   - Linux: "chromium/x-web-custom-data" (not written here: Clack's
//     Linux clipboard goes through Gio, text only).
//
// The custom data is a base::Pickle (custom_data_helper.cc
// WriteCustomDataToPickle): a uint32 payload size, then a uint32 count
// and count pairs of String16 (an int32 length in UTF-16 units, the
// units, padded to 4 bytes), all little-endian.
package richclip

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
)

// ErrUnsupported: the platform has no rich clipboard here (the caller
// writes plain text instead).
var ErrUnsupported = errors.New("richclip: not supported on this platform")

// Copy is what one copy puts on the clipboard.
type Copy struct {
	Text string // text/plain
	HTML string // text/html ("" for none)
	// Custom are the other MIME types, as clipboardData.setData sets them
	// ("slack/html", "slack/texty", "text/markdown").
	Custom map[string]string
}

// Write puts c on the system clipboard, replacing what's there.
func Write(ctx context.Context, c Copy) error { return write(ctx, c) }

// ReadCustom reads the clipboard's web custom data (nil when it has
// none).
func ReadCustom(ctx context.Context) (map[string]string, error) { return readCustom(ctx) }

// Supported reports whether Write and ReadCustom work here.
func Supported() bool { return supported() }

// EncodeCustom is WriteCustomDataToPickle: m's pairs in sorted key order.
func EncodeCustom(m map[string]string) []byte {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	var p []byte
	p = binary.LittleEndian.AppendUint32(p, uint32(len(keys)))
	str := func(s string) {
		u := utf16.Encode([]rune(s))
		p = binary.LittleEndian.AppendUint32(p, uint32(len(u)))
		for _, c := range u {
			p = binary.LittleEndian.AppendUint16(p, c)
		}
		for len(p)%4 != 0 {
			p = append(p, 0)
		}
	}
	for _, k := range keys {
		str(k)
		str(m[k])
	}
	out := binary.LittleEndian.AppendUint32(nil, uint32(len(p)))
	return append(out, p...)
}

// maxCustom caps what DecodeCustom reads (clipboard data is untrusted).
const maxCustom = 8 << 20

// DecodeCustom is ReadCustomDataIntoMap.
func DecodeCustom(b []byte) (map[string]string, error) {
	if len(b) < 8 || len(b) > maxCustom {
		return nil, fmt.Errorf("richclip: custom data of %d bytes", len(b))
	}
	size := int(binary.LittleEndian.Uint32(b))
	p := b[4:]
	if size > len(p) {
		return nil, errors.New("richclip: truncated custom data")
	}
	p = p[:size]
	off := 0
	u32 := func() (uint32, bool) {
		if off+4 > len(p) {
			return 0, false
		}
		v := binary.LittleEndian.Uint32(p[off:])
		off += 4
		return v, true
	}
	str := func() (string, bool) {
		n, ok := u32()
		if !ok || int(n) > (len(p)-off)/2 {
			return "", false
		}
		u := make([]uint16, n)
		for i := range u {
			u[i] = binary.LittleEndian.Uint16(p[off+2*i:])
		}
		off += 2 * int(n)
		off = (off + 3) &^ 3
		return string(utf16.Decode(u)), true
	}
	n, ok := u32()
	if !ok || int(n) > len(p)/8 {
		return nil, errors.New("richclip: bad custom data count")
	}
	m := make(map[string]string, n)
	for i := 0; i < int(n); i++ {
		k, ok1 := str()
		v, ok2 := str()
		if !ok1 || !ok2 {
			return nil, errors.New("richclip: truncated custom data pair")
		}
		m[k] = v
	}
	return m, nil
}

// CFHTML wraps an HTML fragment in Windows' CF_HTML header (UTF-8, byte
// offsets), as Chromium's clipboard writes "HTML Format".
func CFHTML(fragment string) []byte {
	const hdr = "Version:0.9\r\nStartHTML:%010d\r\nEndHTML:%010d\r\nStartFragment:%010d\r\nEndFragment:%010d\r\n"
	pre := "<html>\r\n<body>\r\n<!--StartFragment-->"
	post := "<!--EndFragment-->\r\n</body>\r\n</html>"
	hl := len(fmt.Sprintf(hdr, 0, 0, 0, 0))
	startHTML := hl
	startFrag := hl + len(pre)
	endFrag := startFrag + len(fragment)
	endHTML := endFrag + len(post)
	var b strings.Builder
	fmt.Fprintf(&b, hdr, startHTML, endHTML, startFrag, endFrag)
	b.WriteString(pre)
	b.WriteString(fragment)
	b.WriteString(post)
	return []byte(b.String())
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
