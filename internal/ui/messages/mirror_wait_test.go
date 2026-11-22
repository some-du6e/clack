package messages

// A row's view is ready only once the mirror has handled the event that
// wrote the row's revision, or the one a load read past
// (MessageViewState's need: entry.seq, core's emit order). Until then
// JS's copy of the message is of the revision before, and a row that
// took it drew the update, then took it back: a first reaction showed,
// went and came back; a vote went N, N-1, N. activity-live-msg brings a
// core.Event.Seq of its own, a per-message change counter: fed to the wait
// in place of the emit order, it is far below what the mirror has handled,
// and the wait is off. A change only a load brings (core's poll while the
// socket is down writes the newest page with no event) had no mark of its
// own for the message, and a lagged mirror's resync claimed the events
// queued behind it. Synthetic data on internal/fakeslack.

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// mirrorWaitEnv is a channel of one message no plugin changes (maskJS
// masks a word it doesn't have), its row drawn and its view read.
func mirrorWaitEnv(t *testing.T, ch string, msg slack.Message) (app *core.App, v *ChannelView, frame func(), rowOf func() *row) {
	t.Helper()
	app, _, _ = shotsApp(t)
	app.S.PutConv(slack.Conversation{ID: ch, Name: "lunch", IsChannel: true, IsMember: true, NumMembers: 12})
	app.S.PutConv(slack.Conversation{ID: mirrorWaitOther, Name: "elsewhere", IsChannel: true, IsMember: true, NumMembers: 12})
	app.S.SetLatest(ch, []slack.Message{msg}, false)
	patchedEnv(t, app, maskJS(ch, false))
	until(t, 5*time.Second, "the messages slice patched", func() {}, func() bool { return tautOf().SlicePatched("messages") })
	v = NewChannelView(coreHost{app: app, th: theme.New(false)}, ch)
	frame = frameFn(v)
	rowOf = func() *row { return v.s.rows[msg.TS] }
	until(t, 10*time.Second, "the row's view read", frame, func() bool { r := rowOf(); return r != nil && r.viewReady && !r.viewed })
	for i := 0; i < 20; i++ {
		frame()
		time.Sleep(5 * time.Millisecond)
	}
	return app, v, frame, rowOf
}

const mirrorWaitOther = "C0MIRWOTH1"

func lunchMsg() slack.Message {
	a := fmt.Sprintf("%d.000100", time.Now().Add(-time.Hour).Unix())
	return slack.Message{Type: "message", TS: a, User: "UANA00001", Text: "lunch at noon?", Blocks: shotRich(shotSection("lunch at noon?"))}
}

// modelHas reports whether the list's model holds ts with f true of it.
func modelHas(v *ChannelView, ts string, f func(*core.Message) bool) bool {
	m := v.s.m
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.find(ts)
	return i >= 0 && f(&m.msgs[i].m)
}

// drawsReaction fails t unless every frame for d draws the reaction on
// ts's row (taller than h0): its source's, or JS's copy of the revision
// that has it.
func drawsReaction(t *testing.T, v *ChannelView, frame func(), ts string, h0 int, d time.Duration, why string) {
	t.Helper()
	for dl := time.Now().Add(d); time.Now().Before(dl); {
		frame()
		r := v.s.rows[ts]
		if r == nil || len(r.m.Reactions) != 1 || itemHeight(v.s, ts) <= h0 {
			n := -1 // no row
			if r != nil {
				n = len(r.m.Reactions)
			}
			t.Fatalf("%s: a frame drew the row with %d reactions, %dpx tall (%dpx before the reaction): JS's copy of the revision before", why, n, itemHeight(v.s, ts), h0)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// drawsText fails t unless every frame for d draws text on ts's row.
func drawsText(t *testing.T, v *ChannelView, frame func(), ts, text string, d time.Duration, why string) {
	t.Helper()
	for dl := time.Now().Add(d); time.Now().Before(dl); {
		frame()
		if r := v.s.rows[ts]; r == nil || !strings.Contains(rowText(r), text) {
			got := "(no row)"
			if r != nil {
				got = rowText(r)
			}
			t.Fatalf("%s: a frame drew %q, want %q: JS's copy of the revision before", why, got, text)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// holdFirstLoad holds the loads of ch's channel model once their answer
// is in (loadLanded): reached closes as the first one gets there.
func holdFirstLoad(t *testing.T, ch string) (reached <-chan struct{}, release func()) {
	r, gate := make(chan struct{}), make(chan struct{})
	var once sync.Once
	f := func(m *model) {
		if m.ch == ch && m.threadTS == "" && !m.win {
			once.Do(func() { close(r) })
			<-gate
		}
	}
	loadLanded.Store(&f)
	release = sync.OnceFunc(func() {
		loadLanded.CompareAndSwap(&f, nil)
		close(gate)
	})
	t.Cleanup(release)
	return r, release
}

// waitClosed fails t unless c closes within d.
func waitClosed(t *testing.T, c <-chan struct{}, d time.Duration, what string) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(d):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// placeEnv is mirrorWaitEnv before any list of ch: the message is in
// core's store and JS has read the channel (its copy is the message as it
// is now), and no list has a model of it yet.
func placeEnv(t *testing.T, ch string, msg slack.Message) *core.App {
	t.Helper()
	app, _, _ := shotsApp(t)
	app.S.PutConv(slack.Conversation{ID: ch, Name: "lunch", IsChannel: true, IsMember: true, NumMembers: 12})
	app.S.PutConv(slack.Conversation{ID: mirrorWaitOther, Name: "elsewhere", IsChannel: true, IsMember: true, NumMembers: 12})
	app.S.SetLatest(ch, []slack.Message{msg}, false)
	patchedEnv(t, app, maskJS(ch, false))
	until(t, 5*time.Second, "the messages slice patched", func() {}, func() bool { return tautOf().SlicePatched("messages") })
	until(t, 5*time.Second, "JS's copy of the message", func() {}, func() bool {
		_, _, ready, ok := tautOf().MessageViewState(ch, msg.TS, false, 0)
		return ok && ready
	})
	aheadOfChanges(app)
	return app
}

// aheadOfChanges is the traffic of a session (typing: events that change
// no message), so core's emit order runs far ahead of its message change
// count, as it does once anyone types: fed the change table's place
// (activity-live-msg's Event.Seq) for a row's need, the mirror is past it
// already. In a fresh app nearly every event is a message change, and the
// two counts stay close.
func aheadOfChanges(app *core.App) {
	for i := 0; i < 60; i++ {
		injectEvent(app, mirrorWaitOther, "user_typing", "", map[string]any{"user": "UCARA0001"})
	}
	time.Sleep(300 * time.Millisecond) // the mirror handles them
}

// The row's copy an event wrote waits for the mirror to have handled the
// event, by every path it reaches the list: a reaction (applyReactions),
// an edit of a message the list holds (apply's upsert), and an edit of a
// message the list's first load hasn't brought in yet, which the load's
// answer takes over its own copy (notePlace, then keepLive). Each takes
// the event's EmitSeq: fed activity-live-msg's Event.Seq (the change
// table's place) or 0, the wait is off.
func TestRowWaitsForTheMirror(t *testing.T) {
	t.Run("reaction", func(t *testing.T) {
		const ch = "C0MIRWAIT1"
		msg := lunchMsg()
		app, v, frame, row := mirrorWaitEnv(t, ch, msg)
		h0 := itemHeight(v.s, msg.TS)
		// the mirror is behind: an event no view reads waits in it
		release := tautOf().HoldMirror()
		defer release()
		injectEvent(app, mirrorWaitOther, "user_typing", "", map[string]any{"user": "UBEN00001"})
		injectEvent(app, ch, "reaction_added", "", map[string]any{"user": "UBEN00001", "reaction": "eyes",
			"item": map[string]any{"type": "message", "channel": ch, "ts": msg.TS}})
		until(t, 5*time.Second, "the reaction in the list's model", frame, func() bool {
			return modelHas(v, msg.TS, func(m *core.Message) bool { return len(m.Reactions) == 1 })
		})
		// JS answers at once with its copy, from before the reaction for as
		// long as the mirror is held
		drawsReaction(t, v, frame, msg.TS, h0, 700*time.Millisecond, "the mirror behind the reaction")
		release()
		until(t, 5*time.Second, "JS's copy with the reaction", frame, func() bool {
			r := row()
			return r != nil && r.viewReady && len(r.m.Reactions) == 1
		})
		drawsReaction(t, v, frame, msg.TS, h0, 300*time.Millisecond, "JS's copy of the reaction")
	})
	t.Run("edit", func(t *testing.T) {
		const ch = "C0MIRWEDT1"
		msg := lunchMsg()
		app, v, frame, row := mirrorWaitEnv(t, ch, msg)
		aheadOfChanges(app)
		release := tautOf().HoldMirror()
		defer release()
		injectEvent(app, mirrorWaitOther, "user_typing", "", map[string]any{"user": "UBEN00001"})
		editMsg(app, ch, msg.TS, "UANA00001", "lunch at one?")
		until(t, 5*time.Second, "the edit in the list's model", frame, func() bool {
			return modelHas(v, msg.TS, func(m *core.Message) bool { return m.Text == "lunch at one?" })
		})
		drawsText(t, v, frame, msg.TS, "lunch at one?", 700*time.Millisecond, "the mirror behind the edit")
		release()
		until(t, 5*time.Second, "JS's copy of the edit", frame, func() bool {
			r := row()
			return r != nil && r.viewReady && strings.Contains(rowText(r), "lunch at one?")
		})
	})
	t.Run("place", func(t *testing.T) {
		const ch = "C0MIRWPLC1"
		msg := lunchMsg()
		app := placeEnv(t, ch, msg)
		reached, landing := holdFirstLoad(t, ch)
		v := NewChannelView(coreHost{app: app, th: theme.New(false)}, ch)
		frame := frameFn(v)
		frame() // the first load: its answer in, before the edit
		waitClosed(t, reached, 5*time.Second, "the first load's answer")
		release := tautOf().HoldMirror()
		defer release()
		injectEvent(app, mirrorWaitOther, "user_typing", "", map[string]any{"user": "UBEN00001"})
		editMsg(app, ch, msg.TS, "UANA00001", "lunch at one?")
		until(t, 5*time.Second, "the edit noted for the load", func() {}, func() bool {
			m := v.s.m
			m.mu.Lock()
			defer m.mu.Unlock()
			_, ok := m.livePlace[msg.TS]
			return ok
		})
		landing()
		until(t, 5*time.Second, "the row", frame, func() bool { return v.s.rows[msg.TS] != nil })
		drawsText(t, v, frame, msg.TS, "lunch at one?", 700*time.Millisecond, "the mirror behind an edit the first load took in")
		release()
		until(t, 5*time.Second, "JS's copy of the edit", frame, func() bool {
			r := v.s.rows[msg.TS]
			return r != nil && r.viewReady && strings.Contains(rowText(r), "lunch at one?")
		})
	})
}

// A reaction while a load is out: the load's answer, read before it,
// takes the reactions over its own copy (keepLive), with the reaction's
// EmitSeq, so the row waits for the mirror to have handled it. held: a
// reload of a message the list holds, whose answer also differs from the
// list's copy, so its views are marked for JS again (markLoadChanged) and
// JS reads core's copy: guarded twice. place: the list's first load, of a
// message it doesn't hold yet; the answer marks nothing, and only the
// EmitSeq keeps JS's copy from before the reaction from being ready (0 or
// the load's own: the reaction is after it).
func TestKeepLiveTakesTheEventsEmitSeq(t *testing.T) {
	react := func(app *core.App, ch, ts string) {
		injectEvent(app, ch, "reaction_added", "", map[string]any{"user": "UBEN00001", "reaction": "eyes",
			"item": map[string]any{"type": "message", "channel": ch, "ts": ts}})
	}
	noted := func(v *ChannelView, ts string) func() bool {
		return func() bool {
			m := v.s.m
			m.mu.Lock()
			defer m.mu.Unlock()
			_, ok := m.liveReact[ts]
			return ok
		}
	}
	t.Run("held", func(t *testing.T) {
		const ch = "C0KEEPHLD1"
		msg := lunchMsg()
		app, v, frame, _ := mirrorWaitEnv(t, ch, msg)
		h0 := itemHeight(v.s, msg.TS)
		reached, landing := holdFirstLoad(t, ch)
		v.Refresh()
		waitClosed(t, reached, 5*time.Second, "the reload's answer")
		release := tautOf().HoldMirror()
		defer release()
		injectEvent(app, mirrorWaitOther, "user_typing", "", map[string]any{"user": "UBEN00001"})
		react(app, ch, msg.TS)
		until(t, 5*time.Second, "the reaction noted for the load", func() {}, noted(v, msg.TS))
		landing()
		until(t, 5*time.Second, "the reload in", frame, func() bool {
			m := v.s.m
			m.mu.Lock()
			defer m.mu.Unlock()
			i := m.find(msg.TS)
			return !m.loading && i >= 0 && len(m.msgs[i].m.Reactions) == 1
		})
		drawsReaction(t, v, frame, msg.TS, h0, 700*time.Millisecond, "a reaction during the reload")
		release()
	})
	t.Run("place", func(t *testing.T) {
		const ch = "C0KEEPPLC1"
		msg := lunchMsg()
		app := placeEnv(t, ch, msg)
		reached, landing := holdFirstLoad(t, ch)
		v := NewChannelView(coreHost{app: app, th: theme.New(false)}, ch)
		frame := frameFn(v)
		frame()
		waitClosed(t, reached, 5*time.Second, "the first load's answer")
		release := tautOf().HoldMirror()
		defer release()
		injectEvent(app, mirrorWaitOther, "user_typing", "", map[string]any{"user": "UBEN00001"})
		react(app, ch, msg.TS)
		until(t, 5*time.Second, "the reaction noted for the load", func() {}, noted(v, msg.TS))
		landing()
		until(t, 5*time.Second, "the row", frame, func() bool { return v.s.rows[msg.TS] != nil })
		drawsReaction(t, v, frame, msg.TS, 0, 700*time.Millisecond, "a reaction during the first load")
		release()
		until(t, 5*time.Second, "JS's copy with the reaction", frame, func() bool {
			r := v.s.rows[msg.TS]
			return r != nil && r.viewReady && len(r.m.Reactions) == 1
		})
	})
}

// A change only a load brings: core's newest page with a thread's new
// reply, written with no event. The row draws the new count from the
// frame its model has it on, never JS's copy from before the load: at
// once with the mirror idle, and once the mirror catches up when it's
// behind the load.
func TestLoadOnlyChangeNeverDrawsTheOldCount(t *testing.T) {
	const ch = "C0LOADCHG1"
	msg := lunchMsg()
	r1 := fmt.Sprintf("%d.000200", time.Now().Add(-50*time.Minute).Unix())
	msg.ThreadTS, msg.ReplyCount, msg.ReplyUsers, msg.LatestReply = msg.TS, 1, []string{"UBEN00001"}, r1
	app, v, frame, row := mirrorWaitEnv(t, ch, msg)
	if r := row(); r.m.ReplyCount != 1 {
		t.Fatalf("the root draws %d replies, want 1", r.m.ReplyCount)
	}
	load := func(n int, held bool) {
		t.Helper()
		next := msg
		next.ReplyCount = n
		next.ReplyUsers = []string{"UBEN00001", "UCARA0001"}
		next.LatestReply = fmt.Sprintf("%d.000%d00", time.Now().Add(-40*time.Minute).Unix(), n+2)
		release := func() {}
		if held {
			release = tautOf().HoldMirror()
			defer release()
			injectEvent(app, mirrorWaitOther, "user_typing", "", map[string]any{"user": "UBEN00001"})
		}
		app.S.SetLatest(ch, []slack.Message{next}, false) // the poll's page: no event
		v.Refresh()
		until(t, 5*time.Second, fmt.Sprintf("the load's %d replies in the list's model", n), func() {}, func() bool {
			return modelHas(v, msg.TS, func(m *core.Message) bool { return m.ReplyCount == n })
		})
		check := func(d time.Duration) {
			t.Helper()
			for dl := time.Now().Add(d); time.Now().Before(dl); {
				frame()
				if r := row(); r == nil || r.m.ReplyCount != n {
					got := -1
					if r != nil {
						got = r.m.ReplyCount
					}
					t.Fatalf("held %v: the row's model has %d replies and a frame drew %d (JS's copy from before the load)", held, n, got)
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
		check(300 * time.Millisecond)
		release()
		check(1500 * time.Millisecond) // past the mirror's sweep (750 ms), which tells JS
	}
	load(2, false)
	load(3, true)
}

// The mirror fell behind (a reconnect's flood filled its buffer): core
// drops what it can't take and sends one resync, which the mirror handles
// by invalidating everything. An event emitted while it does that is
// queued behind it, its marks not made: the resync claims what came
// before it, not that one.
func TestResyncClaimsOnlyWhatCameBefore(t *testing.T) {
	const ch = "C0RESYNC01"
	msg := lunchMsg()
	app, v, frame, row := mirrorWaitEnv(t, ch, msg)
	h0 := itemHeight(v.s, msg.TS)
	rt, err := tautOf().JS()
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	reacted := make(chan func(), 1) // the hold the hook set, once the reaction is in
	restore := tautOf().AfterMirrorEvent(func(ev core.Event) {
		if ev.Type != "resync" || ev.EmitSeq != 0 {
			return
		}
		once.Do(func() {
			// JS has what the resync sent it (posted 8 ms on), and has read
			// the channel again since: its copy of the message is the one
			// from before the reaction
			time.Sleep(40 * time.Millisecond)
			_ = rt.Do(func(*jsrt.Ctx) error { return nil })
			for dl := time.Now().Add(3 * time.Second); time.Now().Before(dl); time.Sleep(2 * time.Millisecond) {
				if _, _, ready, ok := tautOf().MessageViewState(ch, msg.TS, false, 0); ok && ready {
					break
				}
			}
			// a reaction lands as the resync's handling ends; the mirror
			// handles it only after the event queued before it
			hold := tautOf().HoldMirror()
			injectEvent(app, ch, "reaction_added", "", map[string]any{"user": "UBEN00001", "reaction": "eyes",
				"item": map[string]any{"type": "message", "channel": ch, "ts": msg.TS}})
			for dl := time.Now().Add(2 * time.Second); time.Now().Before(dl); time.Sleep(time.Millisecond) {
				if modelHas(v, msg.TS, func(m *core.Message) bool { return len(m.Reactions) == 1 }) {
					break
				}
			}
			reacted <- hold
		})
	})
	defer restore()
	var holdAfter func()
	defer func() {
		if holdAfter != nil {
			holdAfter()
		}
	}()
	// the flood: the mirror's buffer full while it's held, the rest dropped
	flood := tautOf().HoldMirror()
	for i := 0; i <= core.SubscriberBuffer+64; i++ {
		injectEvent(app, mirrorWaitOther, "user_typing", "", map[string]any{"user": "UBEN00001"})
	}
	flood()
	// the next event finds room: the resync goes before it
	for dl := time.Now().Add(10 * time.Second); ; {
		injectEvent(app, mirrorWaitOther, "user_typing", "", map[string]any{"user": "UCARA0001"})
		select {
		case holdAfter = <-reacted:
		case <-time.After(200 * time.Millisecond):
			if time.Now().After(dl) {
				t.Fatal("the mirror never handled a resync")
			}
			continue
		}
		break
	}
	until(t, 5*time.Second, "the reaction in the row", frame, func() bool {
		r := row()
		return r != nil && len(r.src.Reactions) == 1
	})
	drawsReaction(t, v, frame, msg.TS, h0, 700*time.Millisecond, "the reaction queued behind the resync")
	holdAfter()
	until(t, 5*time.Second, "JS's copy with the reaction", frame, func() bool {
		r := row()
		return r != nil && r.viewReady && len(r.m.Reactions) == 1
	})
}
