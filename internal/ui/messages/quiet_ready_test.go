package messages

// A row waiting for the plugins' view of its revision (taut's
// MessageViewState not ready: the mirror behind the event or load that
// wrote it, or a mark JS hasn't answered after) is read again only when
// taut's views change (ViewEpoch) or a frame comes 250 ms on. An idle
// window lays itself out only when something asks (Invalidate): no caret
// blinks in an unfocused window, and an animation's frames lay nothing
// out. When readiness turned true with nothing else changing (the mirror
// catching up, or JS's answer bringing back the bytes it had already
// sent), nothing asked, and the row stayed on what it drew: a word
// Censorship masks drawn in clear until a mouse move, a viewed row kept on
// the plugins' view after they stopped changing it, so its next update
// waited for JS too. These tests draw a frame only when the hub or taut
// asks for one. Synthetic data on internal/fakeslack.

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// invHost is coreHost whose Invalidate asks inv for a frame.
type invHost struct {
	coreHost
	inv func()
}

func (h invHost) Invalidate() { h.inv() }

// invFrames draws frames only when asked, as an idle window does: the
// hub's Invalidate (through an invHost) and taut's (tautInvalidate).
type invFrames struct{ ch chan struct{} }

func newInvFrames(t *testing.T) *invFrames {
	f := &invFrames{ch: make(chan struct{}, 1)}
	inv := f.invalidate
	tautInvalidate.Store(&inv)
	t.Cleanup(func() { tautInvalidate.Store(nil) })
	return f
}

func (f *invFrames) invalidate() {
	select {
	case f.ch <- struct{}{}:
	default:
	}
}

// run draws one frame, then one each time a frame is asked for, until
// cond holds after one (true) or d passes (false).
func (f *invFrames) run(frame func(), d time.Duration, cond func() bool) bool {
	frame()
	if cond() {
		return true
	}
	return f.wait(frame, d, cond)
}

// wait is run without the first frame: only those asked for.
func (f *invFrames) wait(frame func(), d time.Duration, cond func() bool) bool {
	timeout := time.After(d)
	for {
		select {
		case <-f.ch:
			frame()
			if cond() {
				return true
			}
		case <-timeout:
			return false
		}
	}
}

// The mirror is behind (an event no view reads queued in it) when the
// channel opens: its load reads core's emit order past that event, so its
// rows wait for the mirror. JS answers with its masked copy meanwhile, and
// the row keeps its source. The mirror then catches up and marks nothing
// in this channel: the view is ready, and that alone has to ask for the
// frame that draws it.
func TestReadyViewDrawsWithOnlyInvalidateFrames(t *testing.T) {
	app, _, _ := shotsApp(t)
	const ch, other = "C0QUIETRD1", "C0QUIETOT1"
	app.S.PutConv(slack.Conversation{ID: ch, Name: "fair", IsChannel: true, IsMember: true, NumMembers: 12})
	app.S.PutConv(slack.Conversation{ID: other, Name: "elsewhere", IsChannel: true, IsMember: true, NumMembers: 12})
	a := fmt.Sprintf("%d.000100", time.Now().Add(-time.Hour).Unix())
	app.S.SetLatest(ch, []slack.Message{{Type: "message", TS: a, User: "UANA00001", Text: "the job fair is today",
		Blocks: shotRich(shotSection("the job fair is today"))}}, false)
	f := newInvFrames(t)
	patchedEnv(t, app, maskJS(ch, false))
	until(t, 5*time.Second, "the messages slice patched", func() {}, func() bool { return tautOf().SlicePatched("messages") })
	release := tautOf().HoldMirror()
	defer release()
	injectEvent(app, other, "user_typing", "", map[string]any{"user": "UBEN00001"}) // waits in the mirror
	v := NewChannelView(invHost{coreHost{app: app, th: theme.New(false)}, f.invalidate}, ch)
	frame := frameFn(v)
	row := func() *row { return v.s.rows[a] }
	if !f.run(frame, 5*time.Second, func() bool {
		_, ok := tautOf().MessageView(ch, a)
		return ok && row() != nil
	}) {
		t.Fatal("the row and JS's view of its message never came")
	}
	f.run(frame, 300*time.Millisecond, func() bool { return false }) // what the views' landing asks for
	if strings.Contains(rowText(row()), "***") {
		t.Fatalf("the row drew the plugins' view before the mirror handled what its load read: %q", rowText(row()))
	}
	release()
	if !f.run(frame, 2*time.Second, func() bool { return strings.Contains(rowText(row()), "the *** fair is today") }) {
		t.Errorf("the mirror caught up and the row's view is ready, but no frame came to draw it: %q", rowText(row()))
	}
}

// A viewed row (its word masked) whose edit takes the word out goes back
// to its source, and one whose edit brings it in takes the masked view,
// with no frame but those the hub and taut ask for. JS is busy as each
// edit comes, and an invalidate task of an earlier change waits in its
// queue: the edit's own invalidate joins it, so the view fetched just
// after the edit runs after the change and brings back JS's new copy
// before the edit's last mark. Not ready then, it's asked again, and the
// answer brings back the same bytes: only the readiness changed.
func TestViewedRowGoesBackWithOnlyInvalidateFrames(t *testing.T) {
	app, _, _ := shotsApp(t)
	const ch, other = "C0QUIETBK1", "C0QUIETOT2"
	app.S.PutConv(slack.Conversation{ID: ch, Name: "fair", IsChannel: true, IsMember: true, NumMembers: 12})
	app.S.PutConv(slack.Conversation{ID: other, Name: "elsewhere", IsChannel: true, IsMember: true, NumMembers: 12})
	a := fmt.Sprintf("%d.000100", time.Now().Add(-time.Hour).Unix())
	app.S.SetLatest(ch, []slack.Message{{Type: "message", TS: a, User: "UANA00001", Text: "the job fair is today",
		Blocks: shotRich(shotSection("the job fair is today"))}}, false)
	f := newInvFrames(t)
	patchedEnv(t, app, maskJS(ch, false))
	until(t, 5*time.Second, "the messages slice patched", func() {}, func() bool { return tautOf().SlicePatched("messages") })
	v := NewChannelView(invHost{coreHost{app: app, th: theme.New(false)}, f.invalidate}, ch)
	frame := frameFn(v)
	row := func() *row { return v.s.rows[a] }
	draws := func(text string, viewed bool) func() bool {
		return func() bool {
			r := row()
			return r != nil && r.viewed == viewed && strings.Contains(rowText(r), text)
		}
	}
	if !f.run(frame, 10*time.Second, draws("the *** fair is today", true)) {
		t.Fatalf("the masked view never drew: %q", rowText(row()))
	}
	rt, err := tautOf().JS()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		masked := i%2 == 1
		text, want := fmt.Sprintf("the fair %d is today", i), fmt.Sprintf("the fair %d is today", i)
		if masked {
			text, want = fmt.Sprintf("the job fair %d is today", i), fmt.Sprintf("the *** fair %d is today", i)
		}
		hold := make(chan struct{})
		rt.Post(func(*jsrt.Ctx) { <-hold })
		// an earlier change's invalidate task, queued behind the busy JS
		injectEvent(app, other, "user_typing", "", map[string]any{"user": "UBEN00001"})
		time.Sleep(25 * time.Millisecond)
		ep := tautOf().ViewEpoch()
		editMsg(app, ch, a, "UANA00001", text)
		// the frame that reads the row as the mirror marks the edit: its
		// fetch goes in behind that task, which the edit's invalidate joins
		for dl := time.Now().Add(time.Second); time.Now().Before(dl) && tautOf().ViewEpoch() == ep; {
			runtime.Gosched()
		}
		frame()
		time.Sleep(15 * time.Millisecond)
		close(hold)
		if !f.run(frame, 3*time.Second, draws(want, masked)) {
			r := row()
			t.Errorf("edit %d (%q): the row draws %q (viewed %v) 3 s on, with only the frames asked for; want %q (viewed %v)",
				i, text, rowText(r), r != nil && r.viewed, want, masked)
			return
		}
	}
}
