package messages

// A list keeps its model when the hub lets it go (40 newer
// conversations), and the model takes none of core's events while it's
// out: a thread root edited meanwhile, with a second reply, is older in
// it than in core. Shown again, the list takes the model back and
// reloads core's copy. A reaction to the root landing during the reload
// was applied to the model's old copy, and the load put that copy back
// whole over its answer (keepLive): the pre-edit text and reply count
// until the next refresh. A reaction change takes only the reactions over
// the answer's copy, as Slack's reactions reducer touches nothing else.
// Synthetic data on internal/fakeslack.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// dropOut draws a list of each of 40 other conversations, so the hub lets
// v's model go (the one drawn longest ago). It returns the last of them
// and its message, whose model the hub keeps.
func dropOut(t *testing.T, h Host, app *core.App, v *ChannelView, prefix string) (lastCh, lastTS string) {
	t.Helper()
	base := time.Now().Add(-2 * time.Hour).Unix()
	for i := 0; i < maxModels; i++ {
		oc := fmt.Sprintf("%s%02d", prefix, i)
		ts := fmt.Sprintf("%d.000100", base+int64(i))
		app.S.PutConv(slack.Conversation{ID: oc, Name: fmt.Sprintf("other-%d", i), IsChannel: true, IsMember: true, NumMembers: 5})
		app.S.SetLatest(oc, []slack.Message{{Type: "message", TS: ts, User: "UANA00001", Text: "hello " + oc}}, false)
		ov := NewChannelView(h, oc)
		until(t, 5*time.Second, oc+" drawn", frameFn(ov), func() bool { return itemHeight(ov.s, ts) > 0 })
		lastCh, lastTS = oc, ts
	}
	hb := v.s.hb
	hb.mu.Lock()
	held := hb.chans[v.ch] == v.s.m
	hb.mu.Unlock()
	if held {
		t.Fatal("the hub still keeps the list's model after 40 other conversations")
	}
	return lastCh, lastTS
}

// hubPast waits for the hub to have handled every event core emitted so
// far: it applies them in order on its own goroutine, so once an edit of a
// conversation it holds (ch, ts) is in that model, the events before it
// are applied too.
func hubPast(t *testing.T, app *core.App, hb *hub, ch, ts string) {
	t.Helper()
	text := fmt.Sprintf("hello at %d", time.Now().UnixNano())
	editMsg(app, ch, ts, "UANA00001", text)
	until(t, 5*time.Second, "the hub past what core emitted", func() {}, func() bool {
		hb.mu.Lock()
		m := hb.chans[ch]
		hb.mu.Unlock()
		if m == nil {
			return false
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		i := m.find(ts)
		return i >= 0 && m.msgs[i].m.Text == text
	})
}

func TestDroppedModelKeepsItsEditUnderAReaction(t *testing.T) {
	app, _, _ := shotsApp(t)
	h := coreHost{app: app, th: theme.New(false)}
	const ch = "C0DROPRCT1"
	app.S.PutConv(slack.Conversation{ID: ch, Name: "lunch", IsChannel: true, IsMember: true, NumMembers: 12})
	root := lunchMsg()
	r1 := fmt.Sprintf("%d.000200", time.Now().Add(-50*time.Minute).Unix())
	root.ThreadTS, root.ReplyCount, root.ReplyUsers, root.LatestReply = root.TS, 1, []string{"UBEN00001"}, r1
	app.S.SetLatest(ch, []slack.Message{root}, false)
	v := NewChannelView(h, ch)
	frame := frameFn(v)
	row := func() *row { return v.s.rows[root.TS] }
	until(t, 5*time.Second, "the root drawn", frame, func() bool { r := row(); return r != nil && r.m.ReplyCount == 1 })

	lastCh, lastTS := dropOut(t, h, app, v, "C0DROPRO")

	// while the model is out: the root edited, with a second reply
	r2 := fmt.Sprintf("%d.000300", time.Now().Add(-40*time.Minute).Unix())
	evTS := fmt.Sprintf("%d.%06d", time.Now().Unix(), time.Now().Nanosecond()/1000)
	injectEvent(app, ch, "message", "message_changed", map[string]any{"hidden": true, "ts": evTS, "message": map[string]any{
		"type": "message", "user": "UANA00001", "ts": root.TS, "text": "lunch at one?", "thread_ts": root.TS,
		"reply_count": 2, "reply_users_count": 2, "reply_users": []string{"UBEN00001", "UCARA0001"}, "latest_reply": r2,
		"edited": map[string]any{"user": "UANA00001", "ts": evTS},
		"blocks": []any{map[string]any{"type": "rich_text", "block_id": "e", "elements": []any{map[string]any{"type": "rich_text_section",
			"elements": []any{map[string]any{"type": "text", "text": "lunch at one?"}}}}}}}})
	if m, ok := app.S.Find(ch, root.TS); !ok || m.Text != "lunch at one?" || m.ReplyCount != 2 {
		t.Fatalf("core's copy of the root: %q, %d replies", m.Text, m.ReplyCount)
	}
	// the hub handled the edit while it held no model of ch: taken back
	// after it, the model holds the copy from before
	hubPast(t, app, v.s.hb, lastCh, lastTS)

	// shown again: the list takes the model back and reloads, and a
	// reaction to the root lands while the reload's answer waits
	reached, landing := holdFirstLoad(t, ch)
	frame()
	waitClosed(t, reached, 5*time.Second, "the reload's answer")
	injectEvent(app, ch, "reaction_added", "", map[string]any{"user": "UBEN00001", "reaction": "eyes",
		"item": map[string]any{"type": "message", "channel": ch, "ts": root.TS}})
	until(t, 5*time.Second, "the reaction in the model", func() {}, func() bool {
		return modelHas(v, root.TS, func(m *core.Message) bool { return len(m.Reactions) == 1 })
	})
	if modelHas(v, root.TS, func(m *core.Message) bool { return m.Text == "lunch at one?" }) {
		t.Fatal("the re-adopted model took the edit: nothing stale for its reload to put back")
	}
	landing()
	draws := func() bool {
		r := row()
		return r != nil && strings.Contains(rowText(r), "lunch at one?") && r.m.ReplyCount == 2 && len(r.m.Reactions) == 1
	}
	until(t, 5*time.Second, "the edited root, its two replies and the reaction", frame, func() bool {
		m := v.s.m
		m.mu.Lock()
		loading := m.loading
		m.mu.Unlock()
		return !loading && draws()
	})
	for i := 0; i < 20; i++ {
		frame()
		if !draws() {
			r := row()
			t.Fatalf("frame %d after the reload drew %q with %d replies and %d reactions: the copy from before the model was dropped", i, rowText(r), r.m.ReplyCount, len(r.m.Reactions))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
