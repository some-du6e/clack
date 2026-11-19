package messages

// Under a plugin that patches the messages slice and leaves an ordinary
// message as it is (ShowRealUser, on by default), a row draws its source,
// not a view. When the message changes (a first reaction, a vote's
// chat.update, the viewer's own edit), the row draws the new source; JS's
// copy of the message is the revision before until JS answers for the new
// one. The row took that copy on its second read, so the update showed,
// went back, and came again: a first reaction appeared, vanished (the rows
// below jumping) and came back; a vote's count went N, N-1, N. Every flip
// rebuilt the Block Kit host too, the lost click. Synthetic data on
// internal/fakeslack.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// unchangedPatchJS patches the messages slice and changes nothing in it.
var unchangedPatchJS = pluginJS(`
class LeaveAlone extends TautPlugin {
  static id = 'LeaveAlone'
  static pluginName = 'Leave messages alone (test)'
  static description = 'test: patches the messages slice, changes no message'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    const { patchSlice } = this.api.redux
    patchSlice('messages', (ch, bucket) => bucket)
  }
}
return LeaveAlone`)

func TestUnviewedRowTakesUpdatesOnce(t *testing.T) {
	// JS busy past the updates (a collection, a backlog): its copy stays
	// the revision before for every frame until it answers
	t.Run("JS held", func(t *testing.T) { checkUnviewedUpdates(t, true) })
	// free-running: the mirror marks the view stale again once it told JS,
	// so the next read comes before JS's answer
	t.Run("free-running", func(t *testing.T) { checkUnviewedUpdates(t, false) })
}

func checkUnviewedUpdates(t *testing.T, held bool) {
	app, _, _ := shotsApp(t)
	const ch = "C0UNVIEW01"
	app.S.PutConv(slack.Conversation{ID: ch, Name: "lunch", IsChannel: true, IsMember: true, NumMembers: 12})
	base := time.Now().Add(-time.Hour).Unix()
	ts, ts2 := fmt.Sprintf("%d.000100", base), fmt.Sprintf("%d.000200", base+60)
	app.S.SetLatest(ch, []slack.Message{
		{Type: "message", TS: ts, User: "UANA00001", Text: "lunch at noon?", Blocks: shotRich(shotSection("lunch at noon?"))},
		{Type: "message", TS: ts2, User: "UBEN00001", Text: "sure", Blocks: shotRich(shotSection("sure"))},
	}, false)
	patchedEnv(t, app, unchangedPatchJS)
	until(t, 5*time.Second, "the messages slice patched", func() {}, func() bool { return tautOf().SlicePatched("messages") })
	v := NewChannelView(coreHost{app: app, th: theme.New(false)}, ch)
	v.Preview = true
	frame := frameFn(v)
	row := func() *row { return v.s.rows[ts] }
	until(t, 10*time.Second, "the row drawn", frame, func() bool { return row() != nil && itemHeight(v.s, ts) > 0 })
	for i := 0; i < 40; i++ { // the views fetched, and settled
		frame()
		time.Sleep(5 * time.Millisecond)
	}
	if r := row(); r.viewed {
		t.Fatal("the patch leaves the message as it is and the row draws a view")
	}
	h0 := itemHeight(v.s, ts)
	var release chan struct{}
	if held {
		rt, err := tautOf().JS()
		if err != nil {
			t.Fatal(err)
		}
		release = make(chan struct{})
		rt.Post(func(*jsrt.Ctx) { <-release })
		defer func() {
			if release != nil {
				close(release)
			}
		}()
	}
	var bad []string
	note := func(s string) {
		if len(bad) < 12 {
			bad = append(bad, s)
		}
	}
	// a first reaction: once its pill is drawn (the row taller by the
	// bar), no frame may draw the row without it
	injectEvent(app, ch, "reaction_added", "", map[string]any{"user": "UBEN00001", "reaction": "eyes",
		"item": map[string]any{"type": "message", "channel": ch, "ts": ts}})
	drawn := false
	for i := 0; i < 80; i++ {
		frame()
		if itemHeight(v.s, ts) > h0 {
			drawn = true
		} else if drawn {
			note(fmt.Sprintf("frame %d after the reaction: drawn without it again (%dpx, %dpx before it)", i, itemHeight(v.s, ts), h0))
		}
		time.Sleep(4 * time.Millisecond)
	}
	if !drawn {
		t.Errorf("the reaction never drawn (%dpx, %dpx before it)", itemHeight(v.s, ts), h0)
	}
	// an edit: once the new text is drawn, never the old one again
	evTS := fmt.Sprintf("%d.%06d", time.Now().Unix(), time.Now().Nanosecond()/1000)
	injectEvent(app, ch, "message", "message_changed", map[string]any{"hidden": true, "ts": evTS, "message": map[string]any{
		"type": "message", "user": "UANA00001", "ts": ts, "text": "lunch at one?", "edited": map[string]any{"user": "UANA00001", "ts": evTS},
		"blocks": []any{map[string]any{"type": "rich_text", "block_id": "e", "elements": []any{map[string]any{"type": "rich_text_section",
			"elements": []any{map[string]any{"type": "text", "text": "lunch at one?"}}}}}},
		"reactions": []any{map[string]any{"name": "eyes", "count": 1, "users": []string{"UBEN00001"}}}}})
	edited := false
	for i := 0; i < 80; i++ {
		frame()
		txt := rowText(row())
		switch {
		case strings.Contains(txt, "lunch at one?"):
			edited = true
		case edited && strings.Contains(txt, "lunch at noon?"):
			note(fmt.Sprintf("frame %d after the edit: drew the text before it again (%q)", i, txt))
		}
		time.Sleep(4 * time.Millisecond)
	}
	if !edited {
		t.Errorf("the edit never drawn: %q", rowText(row()))
	}
	if held {
		close(release)
		release = nil
	}
	until(t, 10*time.Second, "the edit and the reaction drawn once JS answered", frame, func() bool {
		return strings.Contains(rowText(row()), "lunch at one?") && itemHeight(v.s, ts) > h0
	})
	for _, b := range bad {
		t.Error(b)
	}
}
