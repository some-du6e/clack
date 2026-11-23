package store

import (
	"encoding/json"
	"strings"
	"testing"
	"unsafe"

	"github.com/some-du6e/clack/internal/slack"
)

// TestShareContent: a decoded copy of a message the store holds takes the
// store's text and blocks when they're the same bytes, wherever the store
// holds it (timeline, window, thread, fetched), and keeps its own when
// they differ or the store has no such message.
func TestShareContent(t *testing.T) {
	s := New("")
	const ch = "C0SHARE001"
	long := strings.Repeat("synthetic line\n", 4000)
	blocks := json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"synthetic"}]}]}]`)
	msg := func(ts string) slack.Message {
		return slack.Message{Type: "message", TS: ts, Text: long + ts, Blocks: append(json.RawMessage(nil), blocks...)}
	}
	s.SetLatest(ch, []slack.Message{msg("300.000001")}, true)
	s.AddWindow(ch, []slack.Message{msg("100.000001")}, "100.000001", "100.000001", true, true, s.LiveSeq())
	s.SetThread(ch, "300.000001", []slack.Message{msg("300.000001"), msg("300.000002")})
	s.PutFetched(ch, []slack.Message{msg("200.000001")}, s.LiveSeq())

	held := map[string]*slack.Message{}
	s.VisitMessages(ch, func(string) {}, func(m *slack.Message) {
		if held[m.TS] == nil {
			held[m.TS] = m
		}
	})
	same := func(a, b string) bool { return unsafe.StringData(a) == unsafe.StringData(b) }
	for _, ts := range []string{"300.000001", "100.000001", "300.000002", "200.000001"} {
		h := held[ts]
		if h == nil {
			t.Fatalf("the store holds no %s", ts)
		}
		c := msg(ts) // a copy decoded afresh: equal, not the same bytes
		if same(c.Text, h.Text) {
			t.Fatalf("%s: the copy shares the store's text before ShareContent", ts)
		}
		s.ShareContent(ch, &c)
		if !same(c.Text, h.Text) || c.Text != long+ts {
			t.Errorf("%s: the copy's text isn't the store's", ts)
		}
		if len(c.Blocks) == 0 || &c.Blocks[0] != &h.Blocks[0] {
			t.Errorf("%s: the copy's blocks aren't the store's", ts)
		}
	}

	// a plugin's edit keeps its own text; blocks the same are still shared
	c := msg("300.000001")
	c.Text = "patched " + c.Text
	s.ShareContent(ch, &c)
	if !strings.HasPrefix(c.Text, "patched ") || same(c.Text, held["300.000001"].Text) {
		t.Errorf("an edited text was replaced")
	}
	if &c.Blocks[0] != &held["300.000001"].Blocks[0] {
		t.Errorf("unchanged blocks beside an edited text weren't shared")
	}
	c = msg("300.000001")
	c.Blocks = json.RawMessage(`[]`)
	s.ShareContent(ch, &c)
	if string(c.Blocks) != `[]` {
		t.Errorf("edited blocks were replaced: %s", c.Blocks)
	}
	// a message the store doesn't hold, another channel's, no ts
	for _, o := range []struct{ ch, ts string }{{ch, "999.000001"}, {"C0OTHER001", "300.000001"}, {ch, ""}} {
		c := msg(o.ts)
		c.TS = o.ts
		text := c.Text
		s.ShareContent(o.ch, &c)
		if !same(c.Text, text) {
			t.Errorf("%s/%q: took another message's text", o.ch, o.ts)
		}
	}
	s.ShareContent(ch, nil) // no panic
}
