package core

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// reactEnv boots core against a fake workspace whose reactions.add and
// reactions.remove wait for the test to answer them.
type reactEnv struct {
	a      *App
	fs     *fakeslack.Server
	ch, ts string
	calls  chan url.Values     // a reactions call arrived (its form)
	answer chan map[string]any // the test's reply to it
	events <-chan Event
}

func newReactEnv(t *testing.T) *reactEnv {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := &reactEnv{fs: fs, calls: make(chan url.Values, 4), answer: make(chan map[string]any)}
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		if method != "reactions.add" && method != "reactions.remove" {
			return nil, false
		}
		e.calls <- form
		return <-e.answer, true
	})
	e.a = New(slack.New(fs.Creds()), store.New(""))
	t.Cleanup(func() { shutdown(e.a) })
	ctx := context.Background()
	if err := e.a.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	e.ch = fs.ChannelIDs()[0]
	p, err := e.a.History(ctx, e.ch, "")
	if err != nil || len(p.Messages) == 0 {
		t.Fatalf("history: %v (%d messages)", err, len(p.Messages))
	}
	for _, m := range p.Messages {
		if len(m.Reactions) == 0 {
			e.ts = m.TS
		}
	}
	if e.ts == "" {
		t.Fatal("no message without reactions in the fake history")
	}
	evs, unsub := e.a.Subscribe()
	t.Cleanup(unsub)
	e.events = evs
	return e
}

// react runs App.React in the background; the result comes on the channel.
func (e *reactEnv) react(name string, add bool) <-chan error {
	done := make(chan error, 1)
	go func() { done <- e.a.React(context.Background(), e.ch, e.ts, name, add) }()
	return done
}

// update waits for the next "update" of the test message.
func (e *reactEnv) update(t *testing.T) Message {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-e.events:
			if ev.Type == "update" && ev.TS == e.ts && ev.Msg != nil {
				return *ev.Msg
			}
		case <-deadline:
			t.Fatal("no update event for the message")
		}
	}
}

// noUpdate fails if the message gets an update within d.
func (e *reactEnv) noUpdate(t *testing.T, d time.Duration) {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case ev := <-e.events:
			if ev.Type == "update" && ev.TS == e.ts {
				t.Fatalf("unexpected update: %+v", ev.Msg.Reactions)
			}
		case <-deadline:
			return
		}
	}
}

// drain drops the events so far (the refresh's own updates).
func (e *reactEnv) drain() {
	for {
		select {
		case <-e.events:
		case <-time.After(100 * time.Millisecond):
			return
		}
	}
}

func (e *reactEnv) call(t *testing.T) url.Values {
	t.Helper()
	select {
	case f := <-e.calls:
		return f
	case <-time.After(3 * time.Second):
		t.Fatal("reactions call never reached the fake")
		return nil
	}
}

func reaction(m Message, name string) (Reaction, bool) {
	for _, r := range m.Reactions {
		if r.Name == name {
			return r, true
		}
	}
	return Reaction{}, false
}

func (e *reactEnv) stored(t *testing.T) Message {
	t.Helper()
	tl := e.a.S.Timeline(e.ch)
	for i := range tl.Msgs {
		if tl.Msgs[i].TS == e.ts {
			return e.a.message(e.ch, &tl.Msgs[i])
		}
	}
	t.Fatal("message gone from the store")
	return Message{}
}

func TestReactOptimisticBeforeAPI(t *testing.T) {
	e := newReactEnv(t)
	done := e.react(":tada:", true)
	f := e.call(t) // reactions.add is in flight, unanswered
	if f.Get("name") != "tada" || f.Get("timestamp") != e.ts || f.Get("channel") != e.ch {
		t.Fatalf("reactions.add form %v", f)
	}
	m := e.update(t) // ...and the reaction is already there
	r, ok := reaction(m, "tada")
	if !ok || !r.Self || r.Count != 1 || len(r.Users) != 1 || r.Users[0] != fakeslack.SelfID {
		t.Fatalf("optimistic reaction %+v (ok %v)", r, ok)
	}
	select {
	case err := <-done:
		t.Fatalf("React returned (%v) before the API answered", err)
	default:
	}

	// a poll/refresh that raced the call returns the message without the
	// reaction (the fake never stores reactions): the overlay keeps it
	p, err := e.a.Refresh(context.Background(), e.ch)
	if err != nil {
		t.Fatal(err)
	}
	for _, pm := range p.Messages {
		if pm.TS == e.ts {
			if r, ok := reaction(pm, "tada"); !ok || !r.Self || r.Count != 1 {
				t.Fatalf("refresh dropped the pending reaction: %+v", pm.Reactions)
			}
		}
	}

	e.answer <- map[string]any{"ok": true}
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	// the realtime echo of our own reaction never counts twice. (The stale
	// refresh above replaced the stored copy, so this first echo puts the
	// reaction back in the store: one update, count 1.)
	echo := rtm("reaction_added", map[string]any{"user": fakeslack.SelfID, "reaction": "tada",
		"item": map[string]any{"type": "message", "channel": e.ch, "ts": e.ts}})
	e.a.handle(echo)
	if r, _ := reaction(e.stored(t), "tada"); r.Count != 1 || len(r.Users) != 1 || !r.Self {
		t.Fatalf("after the echo: %+v", r)
	}
	e.drain()
	e.a.handle(echo) // with the store up to date: a no-op, no event
	e.noUpdate(t, 300*time.Millisecond)
	if r, _ := reaction(e.stored(t), "tada"); r.Count != 1 || len(r.Users) != 1 {
		t.Fatalf("after a second echo: %+v", r)
	}

	// someone else reacting the same still counts
	e.a.handle(rtm("reaction_added", map[string]any{"user": "U0OTHER001", "reaction": "tada",
		"item": map[string]any{"type": "message", "channel": e.ch, "ts": e.ts}}))
	if r, _ := reaction(e.update(t), "tada"); r.Count != 2 || !r.Self {
		t.Fatalf("other user's reaction: %+v", r)
	}
}

func TestReactRollbackOnError(t *testing.T) {
	e := newReactEnv(t)
	done := e.react("tada", true)
	e.call(t)
	if _, ok := reaction(e.update(t), "tada"); !ok {
		t.Fatal("no optimistic reaction")
	}
	e.answer <- map[string]any{"ok": false, "error": "invalid_name"}
	if err := <-done; !isCode(err, "invalid_name") {
		t.Fatalf("React error %v, want invalid_name", err)
	}
	if m := e.update(t); len(m.Reactions) != 0 {
		t.Fatalf("not rolled back: %+v", m.Reactions)
	}
	if m := e.stored(t); len(m.Reactions) != 0 {
		t.Fatalf("store not rolled back: %+v", m.Reactions)
	}
}

func TestReactRemoveOptimisticAndRollback(t *testing.T) {
	e := newReactEnv(t)
	// a reaction from the viewer and one other person
	e.a.S.Mutate(e.ch, e.ts, func(m *slack.Message) {
		m.Reactions = []slack.Reaction{{Name: "eyes", Count: 2, Users: []string{"U0OTHER001", fakeslack.SelfID}},
			{Name: "wave", Count: 2, Users: []string{fakeslack.SelfID, "U0OTHER001"}}}
	})
	before := e.stored(t)

	done := e.react("eyes", false)
	e.call(t)
	r, ok := reaction(e.update(t), "eyes")
	if !ok || r.Self || r.Count != 1 {
		t.Fatalf("optimistic remove: %+v", r)
	}
	// the copy handed out before wasn't changed underneath its holder
	if br, _ := reaction(before, "eyes"); br.Count != 2 || len(br.Users) != 2 || br.Users[1] != fakeslack.SelfID {
		t.Fatalf("an earlier copy changed: %+v", br)
	}
	e.answer <- map[string]any{"ok": false, "error": "invalid_name"}
	if err := <-done; err == nil {
		t.Fatal("no error")
	}
	if r, _ := reaction(e.update(t), "eyes"); !r.Self || r.Count != 2 {
		t.Fatalf("remove not rolled back: %+v", r)
	}

	// and a successful remove, then its echo (another reaction: the client
	// backs off repeating a request that just failed)
	done = e.react("wave", false)
	e.call(t)
	e.update(t)
	e.answer <- map[string]any{"ok": true}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	e.a.handle(rtm("reaction_removed", map[string]any{"user": fakeslack.SelfID, "reaction": "wave",
		"item": map[string]any{"type": "message", "channel": e.ch, "ts": e.ts}}))
	e.noUpdate(t, 300*time.Millisecond)
	if r, _ := reaction(e.stored(t), "wave"); r.Count != 1 || r.Self || len(r.Users) != 1 {
		t.Fatalf("after remove + echo: %+v", r)
	}
}

// already_reacted means the server agrees: keep it, no rollback.
func TestReactAlreadyReacted(t *testing.T) {
	e := newReactEnv(t)
	done := e.react("tada", true)
	e.call(t)
	e.update(t)
	e.answer <- map[string]any{"ok": false, "error": "already_reacted"}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	e.noUpdate(t, 200*time.Millisecond)
	if r, _ := reaction(e.stored(t), "tada"); !r.Self || r.Count != 1 {
		t.Fatalf("%+v", r)
	}
}

// TestReactBeginEchoBeforeReply: ReactBegin is the optimistic half alone
// (popups.React shows its update in the frame of the pick): a custom
// emoji on a thread reply changes the stored reply at once, with the
// emoji's URL; the realtime echo of the viewer's own reaction landing
// before reactions.add answers counts once, and so does the reply after it.
func TestReactBeginEchoBeforeReply(t *testing.T) {
	e := newReactEnv(t)
	const pic = "https://emoji.slack-edge.com/T0FAKE/verity/0a1b2c.png"
	e.a.S.CustomEmoji.Set(map[string]string{"verity": pic})
	root, reply := e.ts, "9999999998.000002"
	e.a.S.SetThread(e.ch, root, []slack.Message{
		{Type: "message", TS: root, ThreadTS: root, User: "U0OTHER001", Text: "root", ReplyCount: 1},
		{Type: "message", TS: reply, ThreadTS: root, User: "U0OTHER001", Text: "a reply"},
	})
	storedReply := func() slack.Message {
		th, _ := e.a.S.Thread(e.ch, root)
		for _, m := range th {
			if m.TS == reply {
				return m
			}
		}
		t.Fatal("the reply left the store")
		return slack.Message{}
	}
	count := func() (int, bool) {
		for _, r := range storedReply().Reactions {
			if r.Name == "verity" {
				return r.Count, len(r.Users) == 1 && r.Users[0] == fakeslack.SelfID
			}
		}
		return 0, false
	}
	ev, call := e.a.ReactBegin(e.ch, reply, ":verity:", true)
	if ev == nil || ev.Msg == nil || ev.TS != reply || ev.ThreadTS != root {
		t.Fatalf("ReactBegin's update %+v", ev)
	}
	if r, ok := reaction(*ev.Msg, "verity"); !ok || !r.Self || r.Count != 1 || r.URL != pic {
		t.Fatalf("the update's pill %+v (ok %v), want the viewer's with %s", r, ok, pic)
	}
	done := make(chan error, 1)
	go func() { done <- call(context.Background()) }()
	if f := e.call(t); f.Get("name") != "verity" || f.Get("timestamp") != reply {
		t.Fatalf("reactions.add form %v", f)
	}
	// the echo first, while the call is out
	e.a.handle(rtm("reaction_added", map[string]any{"user": fakeslack.SelfID, "reaction": "verity",
		"item": map[string]any{"type": "message", "channel": e.ch, "ts": reply}}))
	if n, self := count(); n != 1 || !self {
		t.Fatalf("after the early echo: count %d, the viewer's %v", n, self)
	}
	e.answer <- map[string]any{"ok": true}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if n, self := count(); n != 1 || !self {
		t.Fatalf("after the reply: count %d, the viewer's %v", n, self)
	}
	// a second pick of the same: nothing changes, nothing to show
	if ev, call := e.a.ReactBegin(e.ch, reply, "verity", true); ev != nil {
		t.Errorf("already reacted, yet an update %+v", ev)
	} else {
		go func() { _ = call(context.Background()) }()
		e.call(t)
		e.answer <- map[string]any{"ok": false, "error": "already_reacted"}
	}
}
