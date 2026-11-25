package slack_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"testing"
	"testing/iotest"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// readers feed a body to readStreamed in the shapes a connection does:
// whole, a byte at a time, half reads, and odd fixed sizes, so elements
// and escapes straddle chunk boundaries.
func readers(body []byte) map[string]func() (io.Reader, int64) {
	return map[string]func() (io.Reader, int64){
		"whole":       func() (io.Reader, int64) { return bytes.NewReader(body), int64(len(body)) },
		"no length":   func() (io.Reader, int64) { return bytes.NewReader(body), -1 },
		"byte a read": func() (io.Reader, int64) { return iotest.OneByteReader(bytes.NewReader(body)), -1 },
		"half reads":  func() (io.Reader, int64) { return iotest.HalfReader(bytes.NewReader(body)), -1 },
		"7 bytes":     func() (io.Reader, int64) { return &sized{bytes.NewReader(body), 7}, -1 },
		"4093 bytes":  func() (io.Reader, int64) { return &sized{bytes.NewReader(body), 4093}, -1 },
	}
}

type sized struct {
	r io.Reader
	n int
}

func (s *sized) Read(p []byte) (int, error) { return s.r.Read(p[:min(len(p), s.n)]) }

// sameAsUnmarshal checks readStreamed against json.Unmarshal of the whole
// body: done, it holds what json.Unmarshal makes (and json.Unmarshal
// succeeds); not done, it gives the whole body back for the old decode.
func sameAsUnmarshal(t *testing.T, name string, body []byte) (streamed bool) {
	t.Helper()
	var want slack.HistoryPage
	wantErr := json.Unmarshal(body, &want)
	for how, rd := range readers(body) {
		r, hint := rd()
		var got slack.HistoryPage
		done, back, err := slack.ReadHistoryStreamed(&got, r, hint)
		switch {
		case err != nil:
			t.Errorf("%s (%s): a read error %v", name, how, err)
		case done && wantErr != nil:
			t.Errorf("%s (%s): decoded a body json.Unmarshal refuses (%v)", name, how, wantErr)
		case done && !reflect.DeepEqual(got, want):
			t.Errorf("%s (%s): decoded as\n%+v\nwant json.Unmarshal's\n%+v", name, how, got, want)
		case !done && !bytes.Equal(back, body):
			t.Errorf("%s (%s): not decoded, and gave back %d bytes of the %d", name, how, len(back), len(body))
		case !done && !reflect.DeepEqual(got, slack.HistoryPage{}):
			t.Errorf("%s (%s): not decoded, but the page was written", name, how)
		}
		streamed = done
	}
	return streamed
}

// The streamed decode of conversations.history makes exactly the page
// json.Unmarshal makes, or hands the whole body back for it: on hand-made
// answers (escapes, keys around the array, the shapes it must refuse) and
// on the fake's (every channel's newest page, #giant-wall's giant ones).
func TestHistoryStreamMatchesUnmarshal(t *testing.T) {
	// a raw string: JSON's own escapes (a quote, a newline, a backslash
	// before a quote's mate, \u escapes and a surrogate pair), and
	// brackets inside a string
	msg := `{"type":"message","ts":"1700000000.000100","user":"U1","text":"a \"quoted\" line\nand \\ a \u00e9 and \ud83d\ude00 \\\"","blocks":[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"x]}{\"y"}]}]}],"reactions":[{"name":"+1","count":2,"users":["U1","U2"]}]}`
	bs := "\\" // a backslash, for the escaped keys (written apart: tools turn its escape into the letter)
	cases := []struct {
		name   string
		body   string
		stream bool // the split vouches for it
	}{
		{"one message", `{"ok":true,"messages":[` + msg + `],"has_more":true,"pin_count":2}`, true},
		{"keys before and after", `{"ok":true,"latest":"x","messages":[` + msg + `,` + msg + `],"has_more":false,"response_metadata":{"next_cursor":"bmV4dF90czox"}}`, true},
		{"no messages", `{"ok":true,"messages":[],"has_more":false}`, true},
		{"no messages key", `{"ok":true,"has_more":false}`, true},
		{"whitespace", " \n{ \"ok\" : true , \"messages\" : [ \n" + msg + " ,\n\t" + msg + " ] , \"has_more\" : true } \n", true},
		{"null elements", `{"ok":true,"messages":[null]}`, false},
		{"a string element", `{"ok":true,"messages":["x"]}`, false},
		{"null messages", `{"ok":true,"messages":null}`, false},
		{"folded key", `{"ok":true,"Messages":[` + msg + `]}`, false},
		{"second key", `{"ok":true,"messages":[` + msg + `],"messages":[]}`, false},
		{"second key folded", `{"ok":true,"messages":[` + msg + `],"MESSAGES":[]}`, false},
		{"escaped key", `{"ok":true,"m` + bs + `u0065ssages":[` + msg + `]}`, false},
		{"escaped key after", `{"ok":true,"messages":[],"m` + bs + `u0065ssages":[` + msg + `]}`, false},
		{"trailing comma", `{"ok":true,"messages":[` + msg + `,]}`, false},
		{"no comma", `{"ok":true,"messages":[` + msg + msg + `]}`, false},
		{"leading comma", `{"ok":true,"messages":[,` + msg + `]}`, false},
		{"trailing bytes", `{"ok":true,"messages":[` + msg + `]} x`, false},
		{"cut short", `{"ok":true,"messages":[` + msg, false},
		{"bad element", `{"ok":true,"messages":[{"ts":1700000000}]}`, false},
		{"bad json in an element", `{"ok":true,"messages":[{"ts":"1",}]}`, false},
		{"not an object", `["ok"]`, false},
		{"empty", ``, false},
		{"error answer", `{"ok":false,"error":"channel_not_found"}`, false},
		{"error answer with messages", `{"ok":false,"error":"x","messages":[]}`, false},
	}
	for _, c := range cases {
		if c.stream {
			var p slack.HistoryPage
			if err := json.Unmarshal([]byte(c.body), &p); err != nil {
				t.Fatalf("%s: the case isn't an answer json.Unmarshal takes: %v", c.name, err)
			}
		}
		if got := sameAsUnmarshal(t, c.name, []byte(c.body)); got != c.stream {
			t.Errorf("%s: streamed %v, want %v", c.name, got, c.stream)
		}
	}

	// the fake's pages, #giant-wall's (three giant messages) among them
	sz := fakeslack.Small
	sz.Giant = 3
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	c := slack.New(fs.Creds())
	chans := append([]string{fakeslack.GiantWallID}, fs.ChannelIDs()[:6]...)
	for _, ch := range chans {
		for _, limit := range []string{"28", "3"} {
			var raw json.RawMessage
			if err := c.Call(context.Background(), "conversations.history", map[string][]string{"channel": {ch}, "limit": {limit}, "inclusive": {"true"}}, &raw); err != nil {
				t.Fatal(err)
			}
			if !sameAsUnmarshal(t, fmt.Sprintf("%s's newest %s", ch, limit), raw) {
				t.Errorf("%s's newest %s (%d bytes): not streamed", ch, limit, len(raw))
			}
		}
	}
}

// FuzzHistoryStream: whatever the bytes, the streamed decode makes what
// json.Unmarshal makes or gives every byte back.
func FuzzHistoryStream(f *testing.F) {
	msg := `{"type":"message","ts":"1.1","text":"a\"b\\c\u0041\\\"","blocks":[{"type":"rich_text","elements":[]}],"x":{"y":[1,{"z":"]"}]}}`
	for _, s := range []string{
		`{"ok":true,"messages":[` + msg + `],"has_more":true}`,
		`{"ok":true,"messages":[` + msg + `,` + msg + `],"response_metadata":{"next_cursor":"c"}}`,
		`{"ok":true,"messages":[]}`, `{"messages":[null]}`, `{"Messages":[]}`, `{"ok":true}`,
		`{"ok":true,"m` + "\\" + `u0065ssages":[` + msg + `]}`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		sameAsUnmarshal(t, "fuzzed", body)
	})
}
