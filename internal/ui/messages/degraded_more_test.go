package messages

// The DegradedBanner's mounts and the thread beside it (boot-shared 7JQN,
// build 889ac449a32d): Slack renders it only while troubleConnecting, so
// each trouble period, and each remount of a view re-entered, starts at
// "Last updated less than a minute ago…" (resetLastUpdatedTs); its click
// asks for the open thread's replies again with the channel's history
// (requestOfflineHistory for both views). Synthetic data on
// internal/fakeslack.

import (
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestDegradedBannerPeriodsLikeSlack(t *testing.T) {
	th := theme.New(false)
	var b DegradedBanner
	draw := func(now, since time.Time) string {
		var ops op.Ops
		gtx := layout.Context{Ops: &ops, Now: now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(900, 100))}
		layoutBanner(&b, gtx, th, since)
		return b.Text
	}
	const fresh = "Last updated less than a minute ago…"
	t0 := time.Now()
	if got := draw(t0, t0); got != fresh {
		t.Fatalf("control: the first mount reads %q", got)
	}
	if got := draw(t0.Add(2*time.Minute), t0); got == fresh {
		t.Fatalf("control: two minutes on the same mount reads %q", got)
	}
	// the view was off screen when that trouble ended (no frame without
	// it); trouble again five hours later is a new period
	p2 := t0.Add(5 * time.Hour)
	if got := draw(p2.Add(time.Second), p2); got != fresh {
		t.Errorf("a new trouble period reads %q, want a new mount's %q", got, fresh)
	}
	// the same period, the view left and entered again ten minutes on
	draw(p2.Add(10*time.Minute), p2)
	resetBanner(&b)
	if got := draw(p2.Add(11*time.Minute), p2); got != fresh {
		t.Errorf("a re-entered view's banner reads %q, want a new mount's %q", got, fresh)
	}
}

// panes lays a conversation's list and a thread pane out side by side in
// one frame, the list first, as the window does (ClientPrimaryView, then
// the secondary view).
type panes struct {
	list   layout.Widget
	thread layout.Widget
	split  int
}

func (p *panes) Layout(gtx layout.Context) layout.Dimensions {
	size := gtx.Constraints.Max
	g := gtx
	g.Constraints = layout.Exact(image.Pt(p.split, size.Y))
	p.list(g)
	off := op.Offset(image.Pt(p.split, 0)).Push(gtx.Ops)
	g.Constraints = layout.Exact(image.Pt(size.X-p.split, size.Y))
	p.thread(g)
	off.Pop()
	return layout.Dimensions{Size: size}
}

func TestDegradedBannerRereadsTheThreadLikeSlack(t *testing.T) {
	since := time.Now()
	t.Cleanup(setConnectionTrouble(func(Host) time.Time { return since }))
	const ch = "C0DEGRADE2"
	var root string
	e, fs := guardEnv(t, ch, nil, func(_ *core.App, fs *fakeslack.Server, tss []string) {
		root = tss[230]
		stem := strings.TrimSuffix(root, "000100")
		for i := 1; i <= 3; i++ {
			fs.ReplyAtQuiet(ch, root, fmt.Sprintf("%s%06d", stem, 100+i*100), "UBEN00001", fmt.Sprintf("synthetic reply %d", i))
		}
	})
	tv := NewThreadView(e.h, ch, root)
	rt := newRouted(&panes{list: e.v.Layout, thread: tv.Layout, split: 900}, 1400, 700)
	rt.now = e.rt.now // the window's clock: the hub's frames are guardEnv's router's
	b := paneBanner(e.v)
	until(t, 10*time.Second, "the thread pane and the banner", rt.frame, func() bool {
		return len(tv.s.items) > 3 && b.Text != "" && !b.LinkR.Empty()
	})
	rt.frame() // the thread pane drawn in the frame the click lands in
	replies := fs.CallCount("conversations.replies")
	rt.press(centre(b.LinkR), true)
	until(t, 10*time.Second, "the open thread's replies asked for again", rt.frame, func() bool {
		return fs.CallCount("conversations.replies") > replies
	})
}

// The thread pane's loading row (getThreadRowsAndKeys' loading-start row
// over a slice short of the thread's start, G9yo) offline is
// InlineOfflineIndicator, in place of "Loading replies…".
func TestThreadLoadingRowOfflineLikeSlack(t *testing.T) {
	app, fs := headerEnv(t)
	th := fs.AddHugeThread("offline-row-fixture", 2000, 5) // replies a page at a time, as Slack's server answers
	tv := NewThreadView(coreHost{app: app, th: theme.New(false)}, th.Channel, th.Root)
	tv.JumpTo(fs.HugeThreadReplyTS(1500), false) // a jump far into the thread
	rt := newRouted(tv, 500, 700)
	for dl := time.Now().Add(10 * time.Second); !(len(tv.s.items) > 0 && tv.s.items[0].kind == itLoading); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatalf("the slice around the reply never started short of the root (%d items)", len(tv.s.items))
		}
		rt.frame()
	}
	t.Cleanup(setClientOnline(func(Host) bool { return false }))
	tv.s.list.Position = layout.Position{BeforeEnd: true, First: 0} // the slice's top
	tv.s.stopScroll()
	ls := labelsIn(rt.frame)
	if !hasLabelTrim(ls, "No connection.") || !hasLabelTrim(ls, "Go online to load more messages.") {
		t.Errorf("offline, the thread's loading row draws %q; want InlineOfflineIndicator's", ls)
	}
}
