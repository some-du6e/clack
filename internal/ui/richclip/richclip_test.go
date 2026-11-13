package richclip

import (
	"bytes"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/richtext"
)

// The pickle WriteCustomDataToPickle makes, byte for byte: payload size,
// count, then each String16 as its UTF-16 length and units padded to 4.
func TestEncodeCustom(t *testing.T) {
	got := EncodeCustom(map[string]string{"a": "xy"})
	want, _ := hex.DecodeString("14000000" + "01000000" + "01000000" + "61000000" + "02000000" + "78007900")
	if !bytes.Equal(got, want) {
		t.Fatalf("got %x\nwant %x", got, want)
	}
	m := map[string]string{"slack/texty": `{"ops":[{"insert":"@karim"}]}`, "slack/html": "<meta charset=\"utf-8\">héllo ✓", "text/markdown": ""}
	back, err := DecodeCustom(EncodeCustom(m))
	if err != nil || !reflect.DeepEqual(back, m) {
		t.Fatalf("round trip: %v %q", err, back)
	}
	for _, bad := range [][]byte{nil, {1, 0, 0, 0}, {0xff, 0xff, 0, 0, 1, 0, 0, 0}, want[:len(want)-2]} {
		if _, err := DecodeCustom(bad); err == nil {
			t.Errorf("%x decoded", bad)
		}
	}
}

// CF_HTML's offsets point at the HTML and the fragment.
func TestCFHTML(t *testing.T) {
	frag := `<a href="/team/U1">@karim</a> hi`
	b := string(CFHTML(frag))
	num := func(k string) int {
		i := strings.Index(b, k+":")
		n := 0
		for _, c := range b[i+len(k)+1 : i+len(k)+11] {
			n = n*10 + int(c-'0')
		}
		return n
	}
	if got := b[num("StartFragment"):num("EndFragment")]; got != frag {
		t.Errorf("fragment %q", got)
	}
	if !strings.HasPrefix(b[num("StartHTML"):], "<html>") || num("EndHTML") != len(b) {
		t.Errorf("html range: %q", b)
	}
}

// Copy message's slack/texty: a mention is a slackmention on its label.
func TestTextyDelta(t *testing.T) {
	d := richtext.Doc{Blocks: []richtext.Block{
		{Kind: richtext.Paragraph, Spans: []richtext.Span{{Text: "hi "}, {Kind: richtext.User, ID: "U1", Text: "@karim"},
			{Text: " in "}, {Kind: richtext.Channel, ID: "C1", Text: "#clack"}, {Text: " ok", Style: richtext.Bold}}},
		{Kind: richtext.Quote, Spans: []richtext.Span{{Kind: richtext.Broadcast, ID: "here", Text: "@here"}}},
	}}
	want := `{"ops":[{"insert":"hi "},{"insert":"@karim","attributes":{"slackmention":{"id":"U1","label":"@karim","mention":false,"unverified":false}}},` +
		`{"insert":" in "},{"insert":"#clack","attributes":{"slackmention":{"id":"C1","label":"#clack","mention":false,"unverified":false}}},` +
		`{"insert":" ok","attributes":{"bold":true}},{"insert":"\n"},` +
		`{"insert":"@here","attributes":{"slackmention":{"id":"BKhere","label":"@here","mention":false,"unverified":false}}},{"insert":"\n","attributes":{"blockquote":true}}]}`
	if got := TextyDelta(d); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
