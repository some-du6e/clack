package composer

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// What an edit keeps, and when /s may edit, as Slack's saveMessageEdit
// (boot-shared IHC0) and getLastEditableMessage (qszY W4) decide:
//
//   - the saved blocks are the edit's rich text, then the message's other
//     blocks (shouldPreserveBlockForEdit, qIlP: type !== rich_text): a
//     header, divider, section, context or image block survives /s and
//     an edit in the inline editor;
//   - the edit is in the store before chat.update (editMessage, wVuW
//     u_), so a second /s right after the first sees it; a failed save
//     puts the message back;
//   - during a slow connect's catch-up the window doesn't reach the end
//     (dTxY tp is empty until reachedEnd): "Message not found", the
//     command goes back in the composer.

// ekOthers are a message's non-rich_text blocks, one of each kind.
const ekOthers = `{"type":"header","block_id":"h1","text":{"type":"plain_text","text":"Plan"}},` +
	`{"type":"divider","block_id":"d1"},` +
	`{"type":"section","block_id":"s1","text":{"type":"mrkdwn","text":"*notes*"}},` +
	`{"type":"context","block_id":"c1","elements":[{"type":"mrkdwn","text":"from CI"}]},` +
	`{"type":"image","block_id":"i1","image_url":"https://example.com/a.png","alt_text":"chart"}`

func ekBlocks(text string) json.RawMessage {
	b, _ := json.Marshal(text)
	return json.RawMessage(`[{"type":"header","block_id":"h0","text":{"type":"plain_text","text":"Plan"}},` +
		`{"type":"rich_text","block_id":"r1","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":` + string(b) + `}]}]},` +
		ekOthers + `]`)
}

// ekTypes is the block types (and the header's text) chat.update sent.
func ekTypes(t *testing.T, u url.Values) string {
	t.Helper()
	var bs []struct {
		Type string `json:"type"`
		Text *struct {
			Text string `json:"text"`
		} `json:"text"`
	}
	if err := json.Unmarshal([]byte(u.Get("blocks")), &bs); err != nil {
		t.Fatalf("chat.update blocks %s: %v", u.Get("blocks"), err)
	}
	var out []string
	for _, b := range bs {
		s := b.Type
		if b.Type == "header" && b.Text != nil {
			s += ":" + b.Text.Text
		}
		out = append(out, s)
	}
	return strings.Join(out, ",")
}

// the saved order: the new rich text, then every other block in the
// message's order
const ekWantTypes = "rich_text,header:Plan,header:Plan,divider,section,context,image"

type ekEnv struct {
	t   *testing.T
	app *core.App
	fs  *fakeSlack
	c   *Composer
}

func newEKEnv(t *testing.T, seed func(*store.Store), root string) *ekEnv {
	t.Helper()
	app, fs := syntheticApp(t, seed)
	// Karim's group (core/testdata/account_experiments.json): canEditMessage
	// takes a message with blocks other than rich_text and context
	app.SetExperimentGroup("portable_content_composer", "treatment")
	stubTyping(t)
	h := &slickHost{app: app, th: theme.New(false)}
	c := New(h, tChGen, root)
	t.Cleanup(c.Close)
	return &ekEnv{t: t, app: app, fs: fs, c: c}
}

func (e *ekEnv) submit(s string) {
	d := NewDoc()
	d.Insert(0, s, Attr{})
	e.c.ed.SetDoc(d)
	e.c.submit()
}

// updates waits up to wait for n chat.update calls and returns them all.
func (e *ekEnv) updates(n int, wait time.Duration) []url.Values {
	deadline := time.Now().Add(wait)
	for {
		runRestores()
		ups := e.fs.called("chat.update")
		if len(ups) >= n || time.Now().After(deadline) {
			return ups
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func ekSeed(s *store.Store) {
	s.SetLatest(tChGen, []slack.Message{elMsg(elOwnTS, tSelf, "Plan ship it of", ekBlocks("ship it of"))}, false)
}

// /s keeps the message's header, divider, section, context and image
// blocks after the edited rich text.
func TestEditLastKeepsOtherBlocks(t *testing.T) {
	e := newEKEnv(t, ekSeed, "")
	e.submit("/s/of/off")
	ups := e.updates(1, 5*time.Second)
	if len(ups) != 1 {
		t.Fatalf("%d chat.update calls, want 1", len(ups))
	}
	if got := ekTypes(t, ups[0]); got != ekWantTypes {
		t.Fatalf("chat.update blocks %s, want %s", got, ekWantTypes)
	}
	if !strings.Contains(ups[0].Get("blocks"), `"ship it off"`) {
		t.Errorf("chat.update blocks %s lack the edited text", ups[0].Get("blocks"))
	}
}

// The inline editor (Up, Edit message) saves the same way: its edit of
// the rich text, then the message's other blocks.
func TestEditKeepsOtherBlocks(t *testing.T) {
	app, fs := syntheticApp(t, ekSeed)
	stubTyping(t)
	h := &slickHost{app: app, th: theme.New(false)}
	m := core.Message{Channel: tChGen, TS: elOwnTS, User: tSelf, Text: "Plan ship it of", Blocks: ekBlocks("ship it of")}
	c := NewEdit(h, m, func() {})
	t.Cleanup(c.Close)
	d := c.ed.Doc()
	d.Insert(d.Len(), " today", Attr{})
	c.ed.Touch()
	c.submit()
	deadline := time.Now().Add(5 * time.Second)
	var ups []url.Values
	for time.Now().Before(deadline) {
		runRestores()
		if ups = fs.called("chat.update"); len(ups) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(ups) != 1 {
		t.Fatalf("%d chat.update calls, want 1", len(ups))
	}
	if got := ekTypes(t, ups[0]); got != ekWantTypes {
		t.Fatalf("chat.update blocks %s, want %s", got, ekWantTypes)
	}
	if !strings.Contains(ups[0].Get("blocks"), "ship it of today") {
		t.Errorf("chat.update blocks %s lack the edit", ups[0].Get("blocks"))
	}
}

// Two /s in a row: the second sees the first's edit (editMessage put it
// in the store), so it edits the edited text.
func TestEditLastTwiceInARow(t *testing.T) {
	e := newEKEnv(t, ekSeed, "")
	e.submit("/s/of/off")
	if ups := e.updates(1, 5*time.Second); len(ups) != 1 {
		t.Fatalf("first /s: %d chat.update calls", len(ups))
	}
	e.submit("/s/off/on")
	ups := e.updates(2, 5*time.Second)
	if len(ups) != 2 {
		t.Fatalf("second /s: %d chat.update calls in all, want 2 (it didn't see the first edit)", len(ups))
	}
	if !strings.Contains(ups[1].Get("blocks"), `"ship it on"`) {
		t.Errorf("second chat.update blocks %s, want the text ship it on", ups[1].Get("blocks"))
	}
}

// During a slow connect's catch-up the channel's window and the thread's
// newest slice don't reach the end: nothing is edited and the command
// goes back in the composer.
func TestEditLastWaitsForCatchUp(t *testing.T) {
	for _, root := range []string{"", elRootTS} {
		e := newEKEnv(t, func(s *store.Store) {
			ekSeed(s)
			r := elMsg(elRootTS, "UALICE", "a question", elBlocks(elText("a question")))
			r.ThreadTS, r.ReplyCount = elRootTS, 1
			reply := elMsg(elReplyTS, tSelf, "is it of", elBlocks(elText("is it of")))
			reply.ThreadTS = elRootTS
			s.SetThread(tChGen, elRootTS, []slack.Message{r, reply})
			s.ResetReachedEnd(nil)
		}, root)
		e.submit("/s/of/off")
		if ups := e.updates(1, 600*time.Millisecond); len(ups) != 0 {
			t.Errorf("root %q: %d chat.update calls during the catch-up", root, len(ups))
		}
		if got := e.c.ed.Doc().Text(); got != "/s/of/off" {
			t.Errorf("root %q: composer holds %q, want the command back", root, got)
		}
	}
}

// A save the server refuses puts the message back in the store as it was
// and the command back in the composer.
func TestEditLastFailedSaveRollsBack(t *testing.T) {
	e := newEKEnv(t, ekSeed, "")
	e.fs.mu.Lock()
	e.fs.reply = map[string]any{"chat.update": map[string]any{"ok": false, "error": "cant_update_message"}}
	e.fs.mu.Unlock()
	e.submit("/s/of/off")
	if ups := e.updates(1, 5*time.Second); len(ups) != 1 {
		t.Fatalf("%d chat.update calls, want 1", len(ups))
	}
	deadline := time.Now().Add(5 * time.Second)
	for e.c.ed.Doc().Text() != "/s/of/off" && time.Now().Before(deadline) {
		runRestores()
		time.Sleep(10 * time.Millisecond)
	}
	if got := e.c.ed.Doc().Text(); got != "/s/of/off" {
		t.Errorf("composer holds %q, want the command back", got)
	}
	m, ok := e.app.S.Find(tChGen, elOwnTS)
	if !ok || string(m.Blocks) != string(ekBlocks("ship it of")) {
		t.Errorf("the store's message after the failed save: %s", m.Blocks)
	}
}
