package richclip

import (
	"encoding/binary"
	"testing"
	"unicode/utf16"

	"gioui.org/app/winclip"
)

func utf16le(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 2*len(u))
	for i, v := range u {
		binary.LittleEndian.PutUint16(b[2*i:], v)
	}
	return b
}

// CF_UNICODETEXT's bytes as a clipboard hands them over: each gets a
// defined text, and nothing is read past the bytes (winclip.UTF16Text:
// Chromium's ReadText, GlobalSize / 2 code units up to the first NUL).
func TestClipMalformedText(t *testing.T) {
	for _, c := range []struct {
		name string
		b    []byte
		want string
	}{
		{"nothing", nil, ""},
		{"one byte", []byte{'a'}, ""},
		{"an odd length: the last byte dropped", []byte{'h', 0, 'i', 0, '!'}, "hi"},
		{"no terminator", []byte{'h', 0, 'i', 0}, "hi"},
		{"a terminator, then bytes after it", []byte{'h', 0, 0, 0, 'x', 0}, "h"},
		{"only a terminator", []byte{0, 0}, ""},
		{"an unpaired surrogate", []byte{0x00, 0xd8, 'a', 0}, "�a"},
		{"a pair", append(utf16le("😀"), 0, 0), "😀"},
		{"CRLF kept as it is", utf16le("a\r\nb"), "a\r\nb"},
	} {
		if got := winclip.UTF16Text(c.b); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// The web custom data (Chromium's Pickle) as another app might leave it:
// each malformed one is an error, never a read past the bytes.
func TestClipMalformedCustom(t *testing.T) {
	good := EncodeCustom(map[string]string{"slack/texty": "{}", "text/x": "y"})
	if m, err := DecodeCustom(good); err != nil || m["slack/texty"] != "{}" || m["text/x"] != "y" {
		t.Fatalf("the well-formed data: %v %v", m, err)
	}
	le := binary.LittleEndian
	mk := func(f func(b []byte) []byte) []byte { return f(append([]byte(nil), good...)) }
	for _, c := range []struct {
		name string
		b    []byte
	}{
		{"nothing", nil},
		{"under its header", []byte{1, 2, 3}},
		{"a payload size past the end", mk(func(b []byte) []byte { le.PutUint32(b, uint32(len(b))); return b })},
		{"a huge payload size", mk(func(b []byte) []byte { le.PutUint32(b, 0xffffffff); return b })},
		{"a count past what fits", mk(func(b []byte) []byte { le.PutUint32(b[4:], 1<<30); return b })},
		{"a string length past the end", mk(func(b []byte) []byte { le.PutUint32(b[8:], 1<<29); return b })},
		{"cut in its last string", mk(func(b []byte) []byte {
			b = b[:len(b)-6]
			le.PutUint32(b, uint32(len(b)-4))
			return b
		})},
		{"a pair's value missing", func() []byte {
			b := make([]byte, 4, 64)
			b = le.AppendUint32(b, 1) // one pair
			b = le.AppendUint32(b, 1) // a key of one unit
			b = append(b, 'k', 0, 0, 0)
			le.PutUint32(b, uint32(len(b)-4))
			return b
		}()},
		{"past the cap", make([]byte, maxCustom+1)},
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s: panicked: %v", c.name, r)
				}
			}()
			if m, err := DecodeCustom(c.b); err == nil {
				t.Errorf("%s: decoded %v, want an error", c.name, m)
			}
		}()
	}
}
