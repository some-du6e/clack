package slack_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// readers feed a body to readStreamed in the shapes a connection does:
// whole, a byte at a time, half reads, and odd fixed sizes.
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

// chunkings are the chunk sizes a body of unknown length is read in for
// the hand-made answers: the default (8 KB doubling to 4 MB), one to a
// few thousand bytes, so chunk boundaries fall on every byte (a
// backslash ending a chunk, an element's brace starting the next), and
// one byte doubling.
var chunkings = [][2]int{{}, {1, 1}, {2, 2}, {3, 3}, {7, 7}, {64, 64}, {4093, 4093}, {1, 4096}}

// callDecode is Call's decode of a whole 200 answer into a page
// (client.go: okFast, else the head's ok and its error as a string, then
// json.Unmarshal): ok when Call returns the page.
func callDecode(body []byte) (slack.HistoryPage, bool) {
	if !bytes.HasPrefix(body, []byte(`{"ok":true`)) {
		var head struct {
			OK    *bool  `json:"ok"`
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &head) != nil || head.OK != nil && !*head.OK {
			return slack.HistoryPage{}, false
		}
	}
	var p slack.HistoryPage
	if json.Unmarshal(body, &p) != nil {
		return slack.HistoryPage{}, false
	}
	return p, true
}

// sameAsCall checks readStreamed against Call's decode of the whole body,
// in every reader and chunking: done, it holds the page Call makes (and
// Call makes one); not done, it gives the whole body back for Call's
// decode, the page untouched. streamed: every reading streamed (they must
// agree).
func sameAsCall(t *testing.T, name string, body []byte, chs [][2]int) (streamed bool) {
	t.Helper()
	want, wantOK := callDecode(body)
	seen := map[bool]string{}
	for _, ch := range chs {
		restore := func() {}
		if ch[0] > 0 {
			restore = slack.SetStreamChunks(ch[0], ch[1])
		}
		for how, rd := range readers(body) {
			how := fmt.Sprintf("%s, chunks %v", how, ch)
			r, hint := rd()
			var got slack.HistoryPage
			done, back, err := slack.ReadHistoryStreamed(&got, r, hint)
			switch {
			case err != nil:
				t.Errorf("%s (%s): a read error %v", name, how, err)
			case done && !wantOK:
				t.Errorf("%s (%s): decoded a body Call refuses", name, how)
			case done && !reflect.DeepEqual(got, want):
				t.Errorf("%s (%s): decoded as\n%+v\nwant Call's\n%+v", name, how, got, want)
			case !done && !bytes.Equal(back, body):
				t.Errorf("%s (%s): not decoded, and gave back %d bytes of the %d", name, how, len(back), len(body))
			case !done && !reflect.DeepEqual(got, slack.HistoryPage{}):
				t.Errorf("%s (%s): not decoded, but the page was written", name, how)
			}
			seen[done] = how
		}
		restore()
	}
	if len(seen) == 2 {
		t.Errorf("%s: streamed when read %s, not when read %s", name, seen[true], seen[false])
	}
	return seen[true] != "" && seen[false] == ""
}

// splitEverywhere reads body in two reads split at every offset, then cut
// off by a read error at every offset: split, it's what Call makes or
// the whole body back; cut, an error, nothing back, the page untouched.
func splitEverywhere(t *testing.T, name string, body []byte) {
	t.Helper()
	want, wantOK := callDecode(body)
	for k := 0; k <= len(body); k++ {
		var got slack.HistoryPage
		done, back, err := slack.ReadHistoryStreamed(&got, io.MultiReader(bytes.NewReader(body[:k]), bytes.NewReader(body[k:])), -1)
		switch {
		case err != nil:
			t.Errorf("%s (split at %d): a read error %v", name, k, err)
		case done && (!wantOK || !reflect.DeepEqual(got, want)):
			t.Errorf("%s (split at %d): decoded as %+v, want Call's %+v (ok %v)", name, k, got, want, wantOK)
		case !done && (!bytes.Equal(back, body) || !reflect.DeepEqual(got, slack.HistoryPage{})):
			t.Errorf("%s (split at %d): not decoded, gave back %d bytes of %d", name, k, len(back), len(body))
		}
		var cut slack.HistoryPage
		done, back, err = slack.ReadHistoryStreamed(&cut, io.MultiReader(bytes.NewReader(body[:k]), iotest.ErrReader(io.ErrUnexpectedEOF)), -1)
		if err == nil || done || back != nil || !reflect.DeepEqual(cut, slack.HistoryPage{}) {
			t.Errorf("%s (cut at %d): done %v, %d bytes back, error %v, page written %v", name, k, done, len(back), err, !reflect.DeepEqual(cut, slack.HistoryPage{}))
		}
	}
}

// splitterLeft reports the goroutines a streamed read left behind (its
// workers), waiting a moment for them to end.
func splitterLeft() string {
	for k := 0; k < 100; k++ {
		buf := make([]byte, 4<<20)
		buf = buf[:runtime.Stack(buf, true)]
		if !bytes.Contains(buf, []byte("slack.(*msgSplitter)")) {
			return ""
		}
		if k == 99 {
			return string(buf)
		}
		time.Sleep(20 * time.Millisecond)
	}
	return ""
}

// The streamed decode of conversations.history makes exactly the page
// Call's decode makes, or hands the whole body back for it: on hand-made
// answers (escapes, keys around the array, the shapes it must refuse)
// read in every chunking, split and cut at every offset, and on the
// fake's (every channel's newest page, #giant-wall's giant ones).
func TestHistoryStreamMatchesUnmarshal(t *testing.T) {
	// a raw string: JSON's own escapes (a quote, a newline, a backslash
	// before a quote's mate, \u escapes and a surrogate pair), and
	// brackets inside a string
	msg := `{"type":"message","ts":"1700000000.000100","user":"U1","text":"a \"quoted\" line\nand \\ a \u00e9 and \ud83d\ude00 \\\"","blocks":[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"x]}{\"y"}]}]}],"reactions":[{"name":"+1","count":2,"users":["U1","U2"]}]}`
	bs := "\\" // a backslash, for escapes written apart (tools turn some into letters)
	// strings ending in an escaped backslash (an even run before the
	// quote), Slack's escaped slashes
	ends := `{"type":"message","ts":"1700000000.000200","text":"C:` + bs + bs + `","path":"` + bs + bs + bs + bs + `","url":"https:` + bs + `/` + bs + `/example.com` + bs + `/a"}`
	// the fields a page's messages and answer carry
	rich := `{"type":"message","ts":"1700000000.000300","user":"U1","text":"x","attachments":[{"fallback":"f","text":"t","color":"36a64f"}],"subscribed":true,"reply_count":2,"reply_users":["U2"],"latest_reply":"1700000001.000100","thread_ts":"1700000000.000300","files":[{"id":"F1","name":"a.png"}]}`
	longS := string(rune(0x17f))   // folds onto 's' (encoding/json's key match is bytes.EqualFold)
	nested := func(d int) string { // an element nested d deep, its own braces counted
		return `{"type":"message","ts":"1700000000.000400","x":` + strings.Repeat("[", d-1) + strings.Repeat("]", d-1) + `}`
	}
	many := func(n int) string { return strings.TrimSuffix(strings.Repeat(`{},`, n), ",") }
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
		{"strings ending in a backslash", `{"ok":true,"messages":[` + ends + `,` + ends + `]}`, true},
		{"attachments, files, a thread", `{"ok":true,"messages":[` + rich + `],"pin_count":3,"has_more":true,"channel_actions_ts":null,"channel_actions_count":0}`, true},
		{"ok last", `{"messages":[` + msg + `],"has_more":true,"ok":true}`, true},
		{"a number error after ok", `{"ok":true,"messages":[` + msg + `],"error":5}`, true},
		{"null elements", `{"ok":true,"messages":[null]}`, false},
		{"a string element", `{"ok":true,"messages":["x"]}`, false},
		{"null messages", `{"ok":true,"messages":null}`, false},
		{"folded key", `{"ok":true,"Messages":[` + msg + `]}`, false},
		{"second key", `{"ok":true,"messages":[` + msg + `],"messages":[]}`, false},
		{"second key folded", `{"ok":true,"messages":[` + msg + `],"MESSAGES":[]}`, false},
		{"second key folded past ASCII", `{"ok":true,"messages":[],"me` + longS + longS + `ages":[` + msg + `]}`, false},
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
		{"a number error, ok later", `{"messages":[],"ok":true,"error":5}`, false},
		{"an object error, no ok", `{"messages":[],"error":{"code":1}}`, false},
	}
	for _, c := range cases {
		if c.stream {
			if _, ok := callDecode([]byte(c.body)); !ok {
				t.Fatalf("%s: the case isn't an answer Call takes", c.name)
			}
		}
		if got := sameAsCall(t, c.name, []byte(c.body), chunkings); got != c.stream {
			t.Errorf("%s: streamed %v, want %v", c.name, got, c.stream)
		}
		splitEverywhere(t, c.name, []byte(c.body))
	}
	// the limits: an element as deep as json.Unmarshal takes in the whole
	// body (9998, under the object and the array) and one deeper, which
	// json.Unmarshal of the element alone takes; as many elements as the
	// split takes (1000) and one more
	big := []struct {
		name   string
		body   string
		stream bool
	}{
		{"nested 9998", `{"ok":true,"messages":[` + nested(9998) + `]}`, true},
		{"nested 9999", `{"ok":true,"messages":[` + nested(9999) + `]}`, false},
		{"1000 messages", `{"ok":true,"messages":[` + many(1000) + `]}`, true},
		{"1001 messages", `{"ok":true,"messages":[` + many(1001) + `]}`, false},
	}
	for _, c := range big {
		if got := sameAsCall(t, c.name, []byte(c.body), [][2]int{{}, {4093, 4093}}); got != c.stream {
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
			if !sameAsCall(t, fmt.Sprintf("%s's newest %s", ch, limit), raw, [][2]int{{}, {1 << 20, 1 << 20}}) {
				t.Errorf("%s's newest %s (%d bytes): not streamed", ch, limit, len(raw))
			}
		}
	}
	if left := splitterLeft(); left != "" {
		t.Errorf("the streamed reads left goroutines behind:\n%s", left)
	}
}

// FuzzHistoryStream: whatever the bytes, however the reads split and
// whatever the chunk size, the streamed decode makes what Call's decode
// makes or gives every byte back; cut off by a read error it gives an
// error and nothing else. A failing input lands in testdata/fuzz (the
// history-stream job keeps it as an artifact): commit it as a seed.
func FuzzHistoryStream(f *testing.F) {
	msg := `{"type":"message","ts":"1.1","text":"a\"b\\c\u0041\\\"","blocks":[{"type":"rich_text","elements":[]}],"x":{"y":[1,{"z":"]"}]}}`
	bs := "\\"
	for _, s := range []string{
		`{"ok":true,"messages":[` + msg + `],"has_more":true}`,
		`{"ok":true,"messages":[` + msg + `,` + msg + `],"response_metadata":{"next_cursor":"c"}}`,
		`{"ok":true,"messages":[]}`, `{"messages":[null]}`, `{"Messages":[]}`, `{"ok":true}`,
		`{"ok":true,"m` + bs + `u0065ssages":[` + msg + `]}`,
		`{"ok":true,"messages":[{"type":"message","ts":"1.2","text":"C:` + bs + bs + `","attachments":[{"text":"a"}],"subscribed":true}],"pin_count":2}`,
		`{"messages":[],"ok":true,"error":5}`,
	} {
		f.Add([]byte(s), uint16(0), uint16(len(s)/2))
		f.Add([]byte(s), uint16(6), uint16(len(s)/3))
	}
	f.Fuzz(func(t *testing.T, body []byte, chunk, split uint16) {
		size := 1 + int(chunk)%4096
		restore := slack.SetStreamChunks(size, size)
		defer restore()
		want, wantOK := callDecode(body)
		k := int(split) % (len(body) + 1)
		var got slack.HistoryPage
		done, back, err := slack.ReadHistoryStreamed(&got, io.MultiReader(bytes.NewReader(body[:k]), bytes.NewReader(body[k:])), -1)
		switch {
		case err != nil:
			t.Fatalf("a read error %v", err)
		case done && (!wantOK || !reflect.DeepEqual(got, want)):
			t.Fatalf("decoded as %+v, want Call's %+v (ok %v)", got, want, wantOK)
		case !done && (!bytes.Equal(back, body) || !reflect.DeepEqual(got, slack.HistoryPage{})):
			t.Fatalf("not decoded, gave back %d bytes of %d", len(back), len(body))
		}
		var cut slack.HistoryPage
		done, back, err = slack.ReadHistoryStreamed(&cut, io.MultiReader(bytes.NewReader(body[:k]), iotest.ErrReader(io.ErrUnexpectedEOF)), -1)
		if err == nil || done || back != nil || !reflect.DeepEqual(cut, slack.HistoryPage{}) {
			t.Fatalf("cut at %d: done %v, %d bytes back, error %v", k, done, len(back), err)
		}
	})
}
