package ui

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// timePoint is the middle of message ts's time in the pane's list (router
// coordinates), waiting for the row to draw it.
func (e *perfEnv) timePoint(t *testing.T, pane, ch, list, ts string) image.Point {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		for _, r := range e.paneRows(pane, ch, list) {
			if r.TS != ts || r.TimeTag == nil {
				continue
			}
			if full, _, ok := e.r.AreaBounds(r.TimeTag); ok && !full.Empty() {
				return full.Min.Add(full.Size().Div(2))
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("message %s's time isn't drawn in %s/%s: %s", ts, ch, list, drawnState(e.paneRows(pane, ch, list)))
		}
		e.frame()
		time.Sleep(time.Millisecond)
	}
}

// timeClick clicks the time at p (mods held), at a distinct time.
func (e *perfEnv) timeClick(p image.Point, mods key.Modifiers, at time.Duration) {
	pt := f32.Pt(float32(p.X), float32(p.Y))
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt, Time: at},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt, Modifiers: mods, Time: at + time.Millisecond},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt, Modifiers: mods, Time: at + 2*time.Millisecond},
	)
	for k := 0; k < 4; k++ {
		e.frame()
	}
}

// A message's time is Slack's c-timestamp link (MessageTimestamp): a
// click opens its permalink, which highlights the message (the channel's,
// a reply's in its thread); Cmd on a reply opens its thread in the split
// view; a right-click is the link's menu, Copy Link and Open Link, for
// the message's permalink.
func TestMessageTimeIsAPermalink(t *testing.T) {
	e := threadHeaderEnv(t, false)
	pub, root := fakeslack.ThreadPublicID, fakeslack.ThreadRootTS
	e.openChannel(pub)
	e.settle(800 * time.Millisecond)

	// the root's time in the channel: its permalink, the root highlighted
	p := e.timePoint(t, "", pub, "", root)
	e.timeClick(p, 0, time.Second)
	e.waitDrawn(t, "a click on the root's time (its permalink)", 10*time.Second, "", pub, "",
		func(rows []messages.SenderView) bool { return lit(rows, root) })
	if r := e.a.router.Current(); r.Channel() != pub {
		t.Errorf("after the click the route is %q, want the channel %s", r, pub)
	}

	// a right-click: the shell's link menu for the permalink
	n := len(e.a.overlays)
	p = e.timePoint(t, "", pub, "", root)
	pt := f32.Pt(float32(p.X), float32(p.Y))
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt, Time: 3 * time.Second},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonSecondary, Position: pt, Time: 3*time.Second + time.Millisecond},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt, Time: 3*time.Second + 2*time.Millisecond})
	e.until("the time's menu", 10*time.Second, func() bool { e.frame(); return len(e.a.overlays) > n })
	m, ok := e.a.overlays[len(e.a.overlays)-1].(*popups.Menu)
	if !ok {
		t.Fatalf("the right-click opened %T", e.a.overlays[len(e.a.overlays)-1])
	}
	var labels []string
	for _, it := range m.Items() {
		labels = append(labels, it.Label)
	}
	if got := strings.Join(labels, "|"); !strings.HasPrefix(got, "Copy Link|Open Link") {
		t.Errorf("the time's right-click menu is %q, want the link menu (Copy Link, Open Link)", got)
	}
	m.Dismiss()
	e.frame()

	// a reply's time inside its thread: the reply highlighted there (a
	// reply that starts a group: its time is the header's, drawn at rest;
	// a grouped one's gutter time shows only under the pointer)
	e.a.OpenThread(pub, root)
	e.settle(800 * time.Millisecond)
	reply := ""
	tries := 0
	e.until("a reply's time", 10*time.Second, func() bool {
		e.frame()
		rows := e.paneRows("", pub, root)
		var rootRow *messages.SenderView
		for i := range rows {
			if rows[i].TS == root {
				rootRow = &rows[i]
			}
			if rows[i].TS != root && rows[i].TimeTag != nil {
				reply = rows[i].TS
				return true
			}
		}
		// every reply grouped: the pointer on the last one shows its
		// gutter time (placed from the root's time and the rows' tops)
		if tries++; tries > 20 && rootRow != nil && rootRow.TimeTag != nil && len(rows) > 1 {
			if full, _, ok := e.r.AreaBounds(rootRow.TimeTag); ok {
				last := rows[len(rows)-1]
				y := full.Min.Y + (last.Top - rootRow.Top) + 10
				e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(full.Min.X+40), float32(y)), Time: 4 * time.Second})
			}
		}
		return false
	})
	p = e.timePoint(t, "", pub, root, reply)
	e.timeClick(p, 0, 5*time.Second)
	// openPermalink's jumpToMessage: the thread at the reply; its
	// highlight is jumpToThreadReply's (client-boot 6IaZ): under the
	// nested view none when the navigation came from the thread's own
	// conversation, as it does here (openThreadAt's flash)
	e.until("the thread at the reply", 10*time.Second, func() bool {
		e.frame()
		s := e.a.secondary
		return s.kind == secThread && s.channel == pub && s.ts == root && s.at == reply
	})
	if r := e.a.router.Current(); r != Route("c/"+pub+"/t/"+root) {
		t.Errorf("a click on the reply's time: route %q, want the thread c/%s/t/%s", r, pub, root)
	}
	if lit(e.paneRows("", pub, root), reply) && e.a.nestedSecondary() {
		t.Errorf("a click on the reply's time in its own conversation's thread highlighted it; jumpToThreadReply doesn't under the nested view")
	}

	// Cmd on a reply's time: its thread in the split view (shouldOpenInTile)
	p = e.timePoint(t, "", pub, root, reply)
	e.timeClick(p, key.ModShortcut, 7*time.Second)
	if got, want := e.a.splitTop(), Route("c/"+pub+"/t/"+root); got != want {
		t.Errorf("a Cmd click on the reply's time: split view %q, want %q", got, want)
	}
}
