package ui

// Diagnostic for hover-toolbar-pos (temporary, CI only): where the hover
// toolbar is drawn against the hovered row in each route Karim's report
// points at, on fakeslack's thread header fixtures (synthetic; nothing
// talks to Slack).

import (
	"fmt"
	"image"
	"os"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/messages"
)

type toolbarLister interface {
	RowRects() []messages.RowRect
	HoverToolbar() (string, image.Rectangle, bool)
	ListRect() image.Rectangle
}

func TestHoverToolbarDiag(t *testing.T) {
	if os.Getenv("HOVER_TOOLBAR_DIAG") == "" {
		t.Skip("HOVER_TOOLBAR_DIAG not set")
	}
	e := threadHeaderEnv(t, false)
	e.size = image.Pt(1918, 1080)
	ch := fakeslack.ThreadPublicID
	var pub fakeslack.ThreadHeaderCase
	for _, c := range e.fs.ThreadHeaderCases() {
		if c.Channel == ch {
			pub = c
		}
	}
	other := e.fs.ChannelIDs()[0]
	users := e.fs.UserIDs()[1:4]
	for n := 0; n < 40; n++ {
		_, ev := e.fs.PostQuiet(ch, users[n%len(users)], fmt.Sprintf("a synthetic newer message %d", n))
		e.fs.Push(ev)
	}
	e.settle(300 * time.Millisecond)
	root := fakeslack.ThreadRootTS
	chList := func() toolbarLister {
		if l, ok := e.a.convoFor(ch, "").list.(channelList); ok {
			return l
		}
		return nil
	}
	rootVisible := func() bool {
		l := chList()
		if l == nil {
			return false
		}
		for _, r := range l.RowRects() {
			if r.TS == root {
				return true
			}
		}
		return false
	}
	bad := 0
	clock := 10 * time.Second
	sweep := func(route string, l toolbarLister) {
		if l == nil {
			t.Errorf("%s: no list", route)
			return
		}
		lr := l.ListRect()
		rows := l.RowRects()
		t.Logf("%s: list %v, route %s, secondary %v %s/%s, rows %d", route, lr, e.a.router.Current(), e.a.secondary.kind, e.a.secondary.channel, e.a.secondary.ts, len(rows))
		for _, r := range rows {
			t.Logf("%s:   row %s %v", route, r.TS, r.R)
		}
		for _, r := range rows {
			for _, fx := range []float32{0.5, 0.9} {
				for _, fy := range []float32{0.1, 0.5, 0.9} {
					p := f32.Pt(float32(r.R.Min.X)+fx*float32(r.R.Dx()), float32(r.R.Min.Y)+fy*float32(r.R.Dy()))
					if !image.Pt(int(p.X), int(p.Y)).In(lr) {
						continue
					}
					clock += 50 * time.Millisecond
					e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p, Time: clock})
					e.frame()
					e.frame()
					e.frame()
					ts, card, ok := l.HoverToolbar()
					// the row may have moved under the pointer: find the row now under p
					now := ""
					var nr image.Rectangle
					for _, q := range l.RowRects() {
						if image.Pt(int(p.X), int(p.Y)).In(q.R) {
							now, nr = q.TS, q.R
						}
					}
					dy := card.Min.Y - nr.Min.Y
					okPos := ok && ts == now && (dy == -16 || dy == -28 || card.Min.Y == lr.Min.Y+4)
					mark := ""
					if !okPos {
						mark = "  <-- MISPLACED"
						bad++
					}
					t.Logf("%s: hover %s at (%.0f,%.0f) row-now %s %v: toolbar for %s card %v ok %v dy %d%s", route, r.TS, p.X, p.Y, now, nr, ts, card, ok, dy, mark)
				}
			}
		}
	}

	// 1. Activity: the thread beside the inbox at a reply (OpenThreadAt,
	// what an Activity thread item opens), then the thread header's
	// channel link: the channel at the root (c/<id>/m/<root>), the
	// thread pane staying open
	e.a.Navigate("activity")
	e.settle(800 * time.Millisecond)
	e.a.OpenThreadAt(ch, root, fakeslack.ThreadReplyTS(1))
	hr := e.waitThreadHeader(pub)
	clock += time.Second
	e.clickAt(hr.Min.Add(hr.Size().Div(2)), clock)
	want := Route("c/" + ch + "/m/" + root)
	e.until("the click navigates", 5*time.Second, func() bool { return e.a.router.Current() == want })
	e.until("the root on screen", 10*time.Second, rootVisible)
	e.settle(600 * time.Millisecond)
	sweep("activity-thread-link", chList())

	// 2. the same with the thread pane closed
	e.a.CloseSecondary()
	e.settle(600 * time.Millisecond)
	sweep("activity-thread-link-closed", chList())

	// 3. a permalink from another channel
	e.openChannel(other)
	e.settle(400 * time.Millisecond)
	e.a.Navigate("c/" + ch + "/m/" + root)
	e.until("the root on screen (permalink)", 10*time.Second, rootVisible)
	e.settle(600 * time.Millisecond)
	sweep("permalink", chList())

	// 4. after the 4 s highlight
	e.settle(4500 * time.Millisecond)
	sweep("permalink-after-flash", chList())

	// 5. the channel at its newest
	e.openChannel(other)
	e.settle(300 * time.Millisecond)
	e.openChannel(ch)
	e.toLatest()
	sweep("newest", chList())

	// 6. the thread pane
	e.a.OpenThread(ch, root)
	e.until("the thread", 10*time.Second, func() bool {
		tv := e.threadView(ch, root)
		return tv != nil && len(tv.RowRects()) >= 3
	})
	e.settle(600 * time.Millisecond)
	if tv := e.threadView(ch, root); tv != nil {
		sweep("thread-pane", tv)
	}

	if bad > 0 {
		t.Errorf("%d hovers drew the toolbar away from the hovered row", bad)
	}
}
