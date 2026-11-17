package messages

// A re-conversion (a member renamed, an emoji added, a load's afterLoad)
// reads each message from the store, then writes what it made into the
// model. When hub.run applied an update to that message in between (a
// vote's chat.update), the write put the revision before's Doc over the
// update's: the count went back until the next update. Synthetic data.

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestReconvertKeepsAnUpdate(t *testing.T) {
	app, _, _ := shotsApp(t)
	const ch = "C0RECONV01"
	app.S.PutConv(slack.Conversation{ID: ch, Name: "ops", IsChannel: true, IsMember: true, NumMembers: 8})
	ts := fmt.Sprintf("%d.000100", time.Now().Add(-time.Hour).Unix())
	mention := json.RawMessage(`[{"type":"rich_text","block_id":"m","elements":[{"type":"rich_text_section","elements":[` +
		`{"type":"user","user_id":"UANA00001"},{"type":"text","text":" ready? Upvotes: 1"}]}]}]`)
	app.S.SetLatest(ch, []slack.Message{{Type: "message", TS: ts, User: "UBEN00001", Text: "<@UANA00001> ready? Upvotes: 1", Blocks: mention}}, false)
	v := NewChannelView(coreHost{app: app, th: theme.New(false)}, ch)
	frame := frameFn(v)
	until(t, 5*time.Second, "the message loaded", frame, func() bool { return v.s.rows[ts] != nil })
	m := v.s.m
	c := core.RefChange{Members: []string{"UANA00001"}}
	m.mu.Lock()
	reads := m.find(ts) >= 0 && m.msgs[m.find(ts)].m.Refs.Reads(c)
	m.mu.Unlock()
	if !reads {
		t.Fatal("the message doesn't read the member it mentions")
	}
	// between the conversion's store read and its write, hub.run applies
	// the next revision
	updated := richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: []richtext.Span{{Kind: richtext.Text, Text: "Upvotes: 2"}}}}}
	hook := func(mm *model, at string) {
		mm.mu.Lock()
		defer mm.mu.Unlock()
		if i := mm.find(at); i >= 0 {
			upd := mm.msgs[i].m
			upd.Text, upd.Doc = "<@UANA00001> ready? Upvotes: 2", updated
			mm.msgs[i] = entry{m: upd, rev: mm.bump()}
		}
	}
	reconvRead.Store(&hook)
	t.Cleanup(func() { reconvRead.Store(nil) })
	v.s.hb.reconvertIn([]*model{m}, c)
	m.mu.Lock()
	got := m.msgs[m.find(ts)].m
	m.mu.Unlock()
	if !reflect.DeepEqual(got.Doc, updated) {
		t.Errorf("the re-conversion wrote the revision before's Doc over the update: %q", richtext.PlainText(got.Doc))
	}
}
