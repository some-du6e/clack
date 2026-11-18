package views

// The Threads view and the Saved page draw a message's edit and deletion
// in the frame after core gets them, with no reload: Slack's Threads view
// draws its threads with the thread pane's components over the messages
// store, and its saved rows read the store as they render (LaterItemRow's
// getMessageByTimestamp). On internal/fakeslack's views fixtures (made-up
// people and messages); the edits reach the client as the socket's
// message_changed and message_deleted (core.Inject).

import (
	"encoding/json"
	"fmt"
	"image"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// lvDrawn is the text b's cached body of ch/ts laid out last (msgBody's
// richview, keyed by the message).
func lvDrawn(b *base, ch, ts string) string {
	if r := b.rich[ch+"/"+ts]; r != nil {
		return r.v.TextRange(0, r.v.PlainLen())
	}
	return ""
}

// lvInject is the socket bringing ev in ch (core.Inject).
func lvInject(app *core.App, ch string, ev map[string]any) {
	evTS := fmt.Sprintf("%d.%06d", time.Now().Unix(), time.Now().Nanosecond()/1000)
	ev["ts"], ev["event_ts"], ev["channel"] = evTS, evTS, ch
	raw, _ := json.Marshal(ev)
	sub, _ := ev["subtype"].(string)
	typ, _ := ev["type"].(string)
	cj, _ := json.Marshal(ch)
	app.Inject(slack.Event{Type: typ, Subtype: sub, Channel: cj, Raw: raw})
}

// A followed thread's reply, edited and then deleted elsewhere: the card
// draws the edit in the next frame and leaves the reply out in the one
// after the deletion (its keyboard row too), with no getView.
func TestThreadsLiveMessages(t *testing.T) {
	app, fs := vaFake(t)
	v := NewThreads(&slickHost{app: app, th: theme.New(false)})
	e := &vaEnv{size: image.Pt(1000, 1100)}
	e.draw = func(gtx layout.Context) { v.Layout(gtx) }
	const ch, reply = fakeslack.ViewsChannel, "1762600200.000100" // the unread thread's follow-up
	e.until(t, "the follow-up drawn", func() bool { return strings.Contains(lvDrawn(&v.base, ch, reply), "a synthetic follow-up") })
	e.frame()
	views := fs.CallCount("subscriptions.thread.getView")
	prev := slack.Message{Type: "message", TS: reply, ThreadTS: fakeslack.ViewsThreadUnread, User: "U0FAKE00002", Text: "a synthetic follow-up"}
	edited := prev
	edited.Text = "a synthetic follow-up, said better"
	edited.Edited = &slack.Edited{User: "U0FAKE00002", TS: fmt.Sprintf("%d.000001", time.Now().Unix())}
	lvInject(app, ch, map[string]any{"type": "message", "subtype": "message_changed", "hidden": true, "message": edited, "previous_message": prev})
	e.frame()
	if got := lvDrawn(&v.base, ch, reply); !strings.Contains(got, "said better") {
		t.Fatalf("the frame after the edit drew %q", got)
	}
	key := ch + "/" + fakeslack.ViewsThreadUnread + "-" + reply
	if _, ok := v.kbRows[key]; !ok {
		t.Fatalf("no keyboard row %s before the deletion (have %d)", key, len(v.kbRows))
	}
	lvInject(app, ch, map[string]any{"type": "message", "subtype": "message_deleted", "hidden": true, "deleted_ts": reply, "previous_message": edited})
	e.frame()
	e.frame() // the keyboard's rows are the last frame's
	if _, ok := v.kbRows[key]; ok {
		t.Error("the deleted reply is still a row of the card")
	}
	for i := 0; i < 10; i++ {
		e.frame()
		time.Sleep(10 * time.Millisecond)
	}
	if n := fs.CallCount("subscriptions.thread.getView") - views; n != 0 {
		t.Errorf("%d getView calls for an edit and a deletion; Slack's view reads the store", n)
	}
}

// A saved message edited, then deleted: the Saved page draws the edit in
// the next frame and the deletion as Later's "no longer available" in
// the one after, with no saved.list.
func TestSavedLiveMessages(t *testing.T) {
	const ch, ts = fakeslack.ViewsChannel, "1762800100.000100"
	var mu sync.Mutex
	text, gone := "a synthetic plan to look over", false
	app, fs := vkFake(t, func(method string, f url.Values) (any, bool) {
		mu.Lock()
		defer mu.Unlock()
		switch method {
		case "saved.list":
			return map[string]any{"ok": true, "saved_items": []map[string]any{{"item_id": ch, "item_type": "message", "ts": ts,
				"todo_state": "saved", "date_created": time.Now().Unix() - 100}}}, true
		case "messages.list":
			out := map[string][]map[string]any{}
			if !gone {
				out[ch] = []map[string]any{{"type": "message", "ts": ts, "user": "U0FAKE00001", "text": text}}
			}
			return map[string]any{"ok": true, "messages": out}, true
		}
		return nil, false
	})
	s := NewSaved(newKBHost(app, false))
	e := &vaEnv{size: image.Pt(1000, 900)}
	e.draw = func(gtx layout.Context) { s.Layout(gtx) }
	e.until(t, "the saved message drawn", func() bool { return strings.Contains(lvDrawn(&s.base, ch, ts), "a synthetic plan") })
	e.frame()
	lists := fs.CallCount("saved.list")
	prev := slack.Message{Type: "message", TS: ts, User: "U0FAKE00001", Text: text}
	edited := prev
	edited.Text = "a synthetic plan, revised"
	edited.Edited = &slack.Edited{User: "U0FAKE00001", TS: fmt.Sprintf("%d.000001", time.Now().Unix())}
	mu.Lock()
	text = edited.Text
	mu.Unlock()
	lvInject(app, ch, map[string]any{"type": "message", "subtype": "message_changed", "hidden": true, "message": edited, "previous_message": prev})
	e.frame()
	if got := lvDrawn(&s.base, ch, ts); !strings.Contains(got, "revised") {
		t.Fatalf("the frame after the edit drew %q", got)
	}
	mu.Lock()
	gone = true
	mu.Unlock()
	lvInject(app, ch, map[string]any{"type": "message", "subtype": "message_deleted", "hidden": true, "deleted_ts": ts, "previous_message": edited})
	e.frame()
	if f, i := s.find(ch + "/" + ts); f == nil || f.items[i].Msg != nil || !f.items[i].Gone {
		t.Fatal("the frame after the deletion still has the saved message")
	}
	for i := 0; i < 10; i++ {
		e.frame()
		time.Sleep(10 * time.Millisecond)
	}
	if n := fs.CallCount("saved.list") - lists; n != 0 {
		t.Errorf("%d saved.list calls for an edit and a deletion; Slack's rows read the store", n)
	}
}

// A followed thread whose latest reply is an app's vote: each chat.update
// (the same length, nothing marked edited) draws in the next frame, a
// reaction to the vote reaches its card (core keeps getView's messages
// where the socket's changes reach them), and a refresh after the
// server's vote moved on unheard draws the refresh's revision: a change
// the view took earlier doesn't outrank it.
func TestThreadsLiveVote(t *testing.T) {
	app, fs := vaFake(t)
	f := &lmFake{rev: 1, gone: map[string]bool{}}
	fs.SetHook(f.hook)
	app.S.PutConv(slack.Conversation{ID: lmChan, Name: "live-votes", IsChannel: true, IsMember: true, NumMembers: 3})
	app.S.PutBot(lmBot, store.BotInfo{Name: "Vote Bot"})
	v := NewThreads(&slickHost{app: app, th: theme.New(false)})
	e := &vaEnv{size: image.Pt(1000, 1100)}
	e.draw = func(gtx layout.Context) { v.Layout(gtx) }
	rev := func() int { return lmRev(lvDrawn(&v.base, lmChan, lmVote)) }
	e.until(t, "the vote drawn", func() bool { return rev() == 1 })
	e.frame()
	views := fs.CallCount("subscriptions.thread.getView")
	for n := 2; n <= 4; n++ {
		prev := lmVoteMsg(n - 1)
		f.mu.Lock()
		f.rev = n
		f.mu.Unlock()
		lvInject(app, lmChan, map[string]any{"type": "message", "subtype": "message_changed", "hidden": true, "message": lmVoteMsg(n), "previous_message": prev})
		e.frame()
		if got := rev(); got != n {
			t.Fatalf("revision %d: the frame after its chat.update drew revision %d (%q)", n, got, lvDrawn(&v.base, lmChan, lmVote))
		}
	}
	lvInject(app, lmChan, map[string]any{"type": "reaction_added", "user": "U0FAKE00001", "reaction": "tada",
		"item": map[string]any{"type": "message", "channel": lmChan, "ts": lmVote}})
	e.frame()
	tadas := func() int {
		d, _, _ := v.data.get()
		for _, th := range d.items {
			for _, r := range th.Replies {
				if r.TS == lmVote {
					for _, rx := range r.Reactions {
						if rx.Name == "tada" {
							return rx.Count
						}
					}
				}
			}
		}
		return 0
	}
	if n := tadas(); n != 1 {
		t.Fatalf("the frame after a reaction to the vote: %d tada on its card", n)
	}
	if got := rev(); got != 4 {
		t.Fatalf("the reaction took the vote to revision %d", got)
	}
	if n := fs.CallCount("subscriptions.thread.getView") - views; n != 0 {
		t.Errorf("%d getView calls for chat.updates and a reaction; Slack's view reads the store", n)
	}
	// the server's vote moves on with no event; the view reads its
	// threads again
	f.mu.Lock()
	f.rev = 6
	f.mu.Unlock()
	v.load(actRefresh)
	e.until(t, "the refresh's revision 6", func() bool {
		if got := rev(); got < 4 {
			t.Fatalf("revision %d drawn while the refresh was out", got)
		}
		return rev() == 6
	})
	for i := 0; i < 10; i++ {
		e.frame()
		if got := rev(); got != 6 {
			t.Fatalf("frame %d after the refresh drew revision %d", i, got)
		}
	}
}

// A saved app message's chat.update keeps its length and marks nothing
// edited: the Saved page draws it in the next frame all the same.
func TestSavedLiveBotUpdate(t *testing.T) {
	const ch = lmChan
	var mu sync.Mutex
	n := 1
	app, _ := vkFake(t, func(method string, f url.Values) (any, bool) {
		mu.Lock()
		defer mu.Unlock()
		switch method {
		case "saved.list":
			return map[string]any{"ok": true, "saved_items": []map[string]any{{"item_id": ch, "item_type": "message", "ts": lmVote,
				"todo_state": "saved", "date_created": time.Now().Unix() - 100}}}, true
		case "messages.list":
			return map[string]any{"ok": true, "messages": map[string][]slack.Message{ch: {lmVoteMsg(n)}}}, true
		}
		return nil, false
	})
	app.S.PutConv(slack.Conversation{ID: lmChan, Name: "live-votes", IsChannel: true, IsMember: true, NumMembers: 3})
	app.S.PutBot(lmBot, store.BotInfo{Name: "Vote Bot"})
	s := NewSaved(newKBHost(app, false))
	e := &vaEnv{size: image.Pt(1000, 900)}
	e.draw = func(gtx layout.Context) { s.Layout(gtx) }
	rev := func() int { return lmRev(lvDrawn(&s.base, ch, lmVote)) }
	e.until(t, "the saved vote drawn", func() bool { return rev() == 1 })
	e.frame()
	for r := 2; r <= 4; r++ {
		mu.Lock()
		prev := lmVoteMsg(n)
		n = r
		mu.Unlock()
		lvInject(app, ch, map[string]any{"type": "message", "subtype": "message_changed", "hidden": true, "message": lmVoteMsg(r), "previous_message": prev})
		e.frame()
		if got := rev(); got != r {
			t.Fatalf("revision %d: the frame after its chat.update drew revision %d (%q)", r, got, lvDrawn(&s.base, ch, lmVote))
		}
	}
}

// thrLvEnv is the Threads view on the live fixture's followed thread.
type thrLvEnv struct {
	app *core.App
	fs  *fakeslack.Server
	f   *lmFake
	v   *Threads
	e   *vaEnv
	now time.Time // the last frame's
}

func newThrLvEnv(t *testing.T, f *lmFake) *thrLvEnv {
	t.Helper()
	app, fs := vaFake(t)
	fs.SetHook(f.hook)
	app.S.PutConv(slack.Conversation{ID: lmChan, Name: "live-votes", IsChannel: true, IsMember: true, NumMembers: 3})
	app.S.PutBot(lmBot, store.BotInfo{Name: "Vote Bot"})
	env := &thrLvEnv{app: app, fs: fs, f: f, v: NewThreads(&slickHost{app: app, th: theme.New(false)}), e: &vaEnv{size: image.Pt(1000, 1100)}}
	env.e.draw = func(gtx layout.Context) {
		env.now = gtx.Now
		env.v.Layout(gtx)
	}
	return env
}

// drawn is the body the card laid out for ts in the last frame ("" when
// none).
func (e *thrLvEnv) drawn(ts string) string {
	if r := e.v.rich[lmChan+"/"+ts]; r != nil && r.at.Equal(e.now) {
		return r.v.TextRange(0, r.v.PlainLen())
	}
	return ""
}

// changed is the socket's message_changed of ts to now.
func (e *thrLvEnv) changed(prev, now slack.Message) {
	lvInject(e.app, lmChan, map[string]any{"type": "message", "subtype": "message_changed", "hidden": true, "message": now, "previous_message": prev})
}

func (e *thrLvEnv) vote(n int) {
	e.f.mu.Lock()
	prev, now := e.f.voteLocked(n-1), e.f.voteLocked(n)
	e.f.rev = n
	e.f.mu.Unlock()
	e.changed(prev, now)
}

func (e *thrLvEnv) edit(ts, text string) {
	e.f.mu.Lock()
	prev, _ := e.f.msgLocked(ts)
	switch ts {
	case lmRoot:
		e.f.rootText = text
	case lmLatest:
		e.f.latestText = text
	}
	now, _ := e.f.msgLocked(ts)
	e.f.mu.Unlock()
	now.Edited = &slack.Edited{User: now.User, TS: fmt.Sprintf("%d.000001", time.Now().Unix())}
	e.changed(prev, now)
}

func (e *thrLvEnv) del(ts string) {
	e.f.mu.Lock()
	prev, _ := e.f.msgLocked(ts)
	e.f.gone[ts] = true
	e.f.mu.Unlock()
	lvInject(e.app, lmChan, map[string]any{"type": "message", "subtype": "message_deleted", "hidden": true, "deleted_ts": ts, "previous_message": prev})
}

// The socket changes several messages of one thread before the view's
// next frame (a busy vote, or everything since the view was last drawn):
// the card draws every change, whichever message changed last.
func TestThreadsLiveBatch(t *testing.T) {
	ready := func(t *testing.T, e *thrLvEnv) {
		t.Helper()
		e.e.until(t, "the thread drawn", func() bool {
			return lmRev(e.drawn(lmVote)) == 1 && e.drawn(lmRoot) != "" && e.drawn(lmLatest) != ""
		})
		e.e.frame()
	}
	t.Run("edits", func(t *testing.T) {
		e := newThrLvEnv(t, &lmFake{rev: 1, gone: map[string]bool{}, latest: true})
		ready(t, e)
		e.vote(2)
		e.edit(lmRoot, "Proposal: move the synthetic standup to 11am")
		e.vote(3)
		e.edit(lmLatest, "a synthetic reply after the vote, said again")
		e.vote(4)
		e.e.frame()
		if got := lmRev(e.drawn(lmVote)); got != 4 {
			t.Errorf("the vote drawn at revision %d, want 4", got)
		}
		if got := e.drawn(lmRoot); !strings.Contains(got, "11am") {
			t.Errorf("the root drew %q, not its edit", got)
		}
		if got := e.drawn(lmLatest); !strings.Contains(got, "said again") {
			t.Errorf("the latest reply drew %q, not its edit", got)
		}
	})
	t.Run("a deleted reply", func(t *testing.T) {
		e := newThrLvEnv(t, &lmFake{rev: 1, gone: map[string]bool{}, latest: true})
		ready(t, e)
		e.vote(2)
		e.del(lmLatest)
		e.vote(3)
		e.e.frame()
		e.e.frame() // the keyboard's rows are the last frame's
		if got := e.drawn(lmLatest); got != "" {
			t.Errorf("the deleted reply is still drawn: %q", got)
		}
		if got := lmRev(e.drawn(lmVote)); got != 3 {
			t.Errorf("the vote drawn at revision %d, want 3", got)
		}
	})
	t.Run("older replies", func(t *testing.T) {
		// the vote is one of the card's older replies ("Show 1 more reply")
		e := newThrLvEnv(t, &lmFake{rev: 1, gone: map[string]bool{}, latest: true, fewLatest: true})
		e.e.until(t, "the latest reply drawn", func() bool { return e.drawn(lmLatest) != "" && e.drawn(lmRoot) != "" })
		d, _, _ := e.v.data.get()
		if len(d.items) != 1 || e.v.cards[d.items[0].key] == nil {
			t.Fatalf("%d threads", len(d.items))
		}
		e.v.loadOlder(&d.items[0], e.v.cards[d.items[0].key])
		e.e.until(t, "the older vote drawn", func() bool { return lmRev(e.drawn(lmVote)) == 1 })
		e.e.frame()
		e.vote(2)
		e.edit(lmLatest, "a synthetic reply after the vote, said again")
		e.vote(3)
		e.edit(lmRoot, "Proposal: move the synthetic standup to 11am")
		e.vote(4)
		e.e.frame()
		if got := lmRev(e.drawn(lmVote)); got != 4 {
			t.Errorf("the older vote drawn at revision %d, want 4", got)
		}
		if got := e.drawn(lmLatest); !strings.Contains(got, "said again") {
			t.Errorf("the latest reply drew %q, not its edit", got)
		}
		if got := e.drawn(lmRoot); !strings.Contains(got, "11am") {
			t.Errorf("the root drew %q, not its edit", got)
		}
	})
	t.Run("an unread reply deleted", func(t *testing.T) {
		e := newThrLvEnv(t, &lmFake{rev: 1, gone: map[string]bool{}, latest: true, unread: true})
		ready(t, e)
		d, _, _ := e.v.data.get()
		if len(d.items) != 1 || e.v.unread(&d.items[0]) != 1 {
			t.Fatalf("before: %d threads, %d unread", len(d.items), e.v.unread(&d.items[0]))
		}
		e.del(lmLatest) // the one unread reply
		e.e.frame()
		d, _, _ = e.v.data.get()
		if n := e.v.unread(&d.items[0]); n != 0 {
			t.Errorf("after its one unread reply was deleted the card counts %d unread: its New line sits on a read reply", n)
		}
	})
}

// lvFiller pushes every copy fetched by id out of the store (other
// views' messages, half a megabyte each), and returns a busy channel's
// message whose reactions make changes enough that core keeps no copy of
// the ones before them.
func lvFiller(t *testing.T, app *core.App) slack.Message {
	t.Helper()
	big := strings.Repeat("x", 512<<10)
	var filler []slack.Message
	for i := 0; i < 10; i++ {
		filler = append(filler, slack.Message{Type: "message", TS: fmt.Sprintf("1762791%03d.000100", i), User: "U0FAKE00001", Text: big})
	}
	app.S.PutFetched(lmBusy, filler, app.S.LiveSeq())
	busy := slack.Message{Type: "message", TS: "1762790500.000100", User: "U0FAKE00001", Text: "a synthetic busy message"}
	app.S.PutConv(slack.Conversation{ID: lmBusy, Name: "live-busy", IsChannel: true, IsMember: true, NumMembers: 3})
	app.S.SetLatest(lmBusy, []slack.Message{busy}, false)
	return busy
}

// lvChurn is n reactions in the busy channel: changes that leave core
// with no copy of the ones before them.
func lvChurn(app *core.App, busy slack.Message, n int) {
	for i := 0; i < n; i++ {
		typ := "reaction_removed"
		if i%2 == 0 {
			typ = "reaction_added"
		}
		lvInject(app, lmBusy, map[string]any{"type": typ, "user": "U0FAKE00001", "reaction": "eyes",
			"item": map[string]any{"type": "message", "channel": lmBusy, "ts": busy.TS}})
	}
}

// A followed thread's vote the store let go of (other views' messages
// pushed it out of the copies fetched by id), voted on before the view's
// next frame with so many changes after it that core keeps no copy of
// the vote's: the card fetches the vote again and draws it, as Slack's
// row fetches a message its store doesn't hold.
func TestThreadsLiveEvictedRefetch(t *testing.T) {
	e := newThrLvEnv(t, &lmFake{rev: 1, gone: map[string]bool{}})
	e.e.until(t, "the vote drawn", func() bool { return lmRev(e.drawn(lmVote)) == 1 })
	e.e.frame()
	busy := lvFiller(t, e.app)
	if _, ok := e.app.S.FindHeld(lmChan, lmVote); ok {
		t.Fatal("the store still holds the vote")
	}
	e.vote(2)
	lvChurn(e.app, busy, 200)
	e.e.until(t, "the vote fetched again at revision 2", func() bool { return lmRev(e.drawn(lmVote)) == 2 })
}

// The same on the Saved page.
func TestSavedLiveEvictedRefetch(t *testing.T) {
	const ch = lmChan
	var mu sync.Mutex
	n := 1
	app, _ := vkFake(t, func(method string, f url.Values) (any, bool) {
		mu.Lock()
		defer mu.Unlock()
		switch method {
		case "saved.list":
			return map[string]any{"ok": true, "saved_items": []map[string]any{{"item_id": ch, "item_type": "message", "ts": lmVote,
				"todo_state": "saved", "date_created": time.Now().Unix() - 100}}}, true
		case "messages.list":
			return map[string]any{"ok": true, "messages": map[string][]slack.Message{ch: {lmVoteMsg(n)}}}, true
		}
		return nil, false
	})
	app.S.PutConv(slack.Conversation{ID: lmChan, Name: "live-votes", IsChannel: true, IsMember: true, NumMembers: 3})
	app.S.PutBot(lmBot, store.BotInfo{Name: "Vote Bot"})
	s := NewSaved(newKBHost(app, false))
	e := &vaEnv{size: image.Pt(1000, 900)}
	e.draw = func(gtx layout.Context) { s.Layout(gtx) }
	rev := func() int { return lmRev(lvDrawn(&s.base, ch, lmVote)) }
	e.until(t, "the saved vote drawn", func() bool { return rev() == 1 })
	e.frame()
	busy := lvFiller(t, app)
	if _, ok := app.S.FindHeld(ch, lmVote); ok {
		t.Fatal("the store still holds the vote")
	}
	mu.Lock()
	prev := lmVoteMsg(n)
	n = 2
	mu.Unlock()
	lvInject(app, ch, map[string]any{"type": "message", "subtype": "message_changed", "hidden": true, "message": lmVoteMsg(2), "previous_message": prev})
	lvChurn(app, busy, 200)
	e.until(t, "the vote fetched again at revision 2", func() bool { return rev() == 2 })
}

// The Threads view further behind than core remembers (off screen
// through a busy while: core forgot the vote's change): each card takes
// its messages as the store holds them now, on every page it loaded, with
// no getView for it.
func TestThreadsLiveFloor(t *testing.T) {
	e := newThrLvEnv(t, &lmFake{rev: 1, gone: map[string]bool{}})
	e.e.until(t, "the vote drawn", func() bool { return lmRev(e.drawn(lmVote)) == 1 })
	e.e.frame()
	views := e.fs.CallCount("subscriptions.thread.getView")
	e.vote(2) // off screen from here
	var many []slack.Message
	for i := 0; i < 8400; i++ {
		many = append(many, slack.Message{Type: "message", TS: fmt.Sprintf("1762700%03d.%06d", i/1000, i%1000), User: "U0FAKE00001", Text: "a synthetic busy message"})
	}
	e.app.S.PutConv(slack.Conversation{ID: lmBusy, Name: "live-busy", IsChannel: true, IsMember: true, NumMembers: 3})
	e.app.S.SetLatest(lmBusy, many, false)
	for _, m := range many {
		lvInject(e.app, lmBusy, map[string]any{"type": "reaction_added", "user": "U0FAKE00001", "reaction": "eyes",
			"item": map[string]any{"type": "message", "channel": lmBusy, "ts": m.TS}})
	}
	if _, ok := e.app.MessageChange(lmChan, lmVote); ok || e.app.MessageChangeFloor() == 0 {
		t.Fatalf("core still remembers the vote's change (floor %d)", e.app.MessageChangeFloor())
	}
	e.e.until(t, "the vote at revision 2", func() bool { return lmRev(e.drawn(lmVote)) == 2 })
	for i := 0; i < 10; i++ {
		e.e.frame()
		time.Sleep(5 * time.Millisecond)
	}
	if n := e.fs.CallCount("subscriptions.thread.getView") - views; n != 0 {
		t.Errorf("%d getView calls; the store had every message of the view", n)
	}
}
