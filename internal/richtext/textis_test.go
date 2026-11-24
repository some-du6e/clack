package richtext

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestTextIsAsDecoded: textIs(s) is textString() == s, for text elements
// written every way JSON can (escapes, \u escapes, surrogate pairs and
// lone ones, raw UTF-8, invalid bytes) and strings near them.
func TestTextIsAsDecoded(t *testing.T) {
	raws := []string{
		`""`, `"a"`, `"synthetic\nline"`, `"tab\tq\"b\\s\/e"`, `"\b\f\r"`,
		`"\u0041\u00e9\u4e2d"`, `"\u00E9"`, `"é中"`, `"\ud83d\ude00"`, `"😀"`,
		`"\ud83d"`, `"\ude00x"`, `"\u0000"`, `"x\"`, `"\u12"`, `"\uzzzz"`,
		"\"\xff\"", "\"\xed\xa0\x80\"", `"\ufffd"`, "\"\xef\xbf\xbd\"", `{"type":"mrkdwn","text":"a"}`, `a`,
	}
	for _, s := range []string{"<&>", "synthetic\x00", "line\u2028end", "bad \xff byte", strings.Repeat("synthetic words\n", 300)} {
		b, _ := json.Marshal(s)
		raws = append(raws, string(b))
	}
	for _, raw := range raws {
		el := bk{Text: json.RawMessage(raw)}
		if !json.Valid([]byte(raw)) {
			_ = el.textIs("") // the blocks' decode lets none through: no panic is all
			continue
		}
		dec := el.textString()
		cands := []string{dec, dec + "x", "", "a", "é", "😀", "\ufffd", "\ufffd\ufffd", "\x00", "x" + dec}
		if len(dec) > 0 {
			cands = append(cands, dec[:len(dec)-1], dec[1:])
		}
		for _, s := range cands {
			if got, want := el.textIs(s), dec == s && strings.HasPrefix(raw, `"`); got != want {
				// textIs may say no where decoding would say yes only for
				// what it leaves to textString: lone surrogates, bad bytes
				if !got && want && (strings.Contains(raw, `\ud`) || strings.ContainsAny(raw, "\xff\xed")) {
					continue
				}
				t.Errorf("textIs(%q) for %q = %v, decoded %q", s, raw, got, dec)
			}
		}
	}
}
