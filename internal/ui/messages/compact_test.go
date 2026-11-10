package messages

// Compact (Preferences > Messages & media > Theme, messages_theme
// "dense") against Slack's web client (bv1-13, build a84662d2d135):
//
//   - Message (boot-shared NnKv): getCompactGutter for every channel row,
//     the time in .p-message_pane_message__compact_timestamp (width 56px,
//     margin -4px 0, text-align right; --adjacent: opacity 0 until
//     hovered) with its meridiem; getCompactMessageBody puts the sender
//     (.p-message_pane_message__sender--compact, margin-left -8px) and a
//     no-break space inline before the text, all inside
//     .c-message_kit__indent (padding-left 8px). Gutter left 20+56, its
//     margin-right 8, the indent 8: the body at 92, the sender at 84.
//   - MessageKitGutter (R86e): "dense" draws the compact slot, or only
//     gutter__right when there is none; ThreadReply (oDUT) passes a
//     compact slot only in the Threads view and adjacent only under
//     "light", so a thread pane has no avatar column and no grouping.
//
// A one-line row is 4 + 22 + 4 = 30 whether or not it starts a group.

import (
	"image"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// setDense turns Compact on or off over the current prefs until the test
// ends.
func setDense(t *testing.T, on bool) {
	t.Helper()
	p := *uipref.Get()
	p.Dense = on
	uipref.Set(p)
	t.Cleanup(uipref.Reset)
}

func TestCompactGeometryChannel(t *testing.T) {
	setDense(t, true)
	for _, scale := range []float32{1, 2} {
		h := spacingHost(t, false)
		v := NewChannelView(h, tSpacing)
		v.Preview = true
		f := scaledFrame(v, scale, int(900*scale), int(700*scale))
		loadList(v, f)
		ts := spacingTS
		until(t, 5*time.Second, "the rows", f, func() bool { return v.s.rows[ts.m3] != nil && itemHeight(v.s, ts.m3) > 0 })
		f()
		// the root and m1 start groups: the sender on the text's line
		for _, m := range []string{ts.root, ts.m1} {
			g := geomOf(t, v.s, m)
			if m == ts.root {
				g.h = px(30, scale) // plus its reply bar
			}
			checkRow(t, "compact header "+m, g, 30, 92, 4, 20.8, 20.8, scale)
			r := v.s.rows[m]
			if r.nameX != -px(8, scale) {
				t.Errorf("%vx: sender at column x %d, Slack -8 (margin-left -8px)", scale, r.nameX)
			}
			if r.rv.FirstBaseline() < 0 || r.compactBL == 0 {
				t.Errorf("%vx: %s: no time in the gutter of a row that starts a group", scale, m)
			}
			if got, want := r.col.Y+r.compactBL, g.bodyBL; got != want {
				t.Errorf("%vx: gutter time baseline %d, body %d", scale, got, want)
			}
		}
		for _, m := range []string{ts.m2, ts.m3} {
			checkRow(t, "compact continuation "+m, geomOf(t, v.s, m), 30, 92, 4, 20.8, 0, scale)
		}
		if r := v.s.rows[ts.m1]; r.bodyRect.Min.X != px(92, scale) {
			t.Errorf("%vx: body at x %d, Slack 92", scale, r.bodyRect.Min.X)
		}
		// the hover time on a grouped row: the same baseline
		r := v.s.rows[ts.m2]
		rc := v.s.rc
		rc.header, rc.hovered = false, true
		gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale}, Constraints: layout.Exact(image.Pt(int(900*scale), int(200*scale))), Now: time.Now()}
		r.compactBL = 0
		r.layout(gtx, &rc)
		if got, want := r.col.Y+r.compactBL, geomOf(t, v.s, ts.m2).bodyBL; got != want || r.compactBL == 0 {
			t.Errorf("%vx: hover time baseline %d, body baseline %d", scale, got, want)
		}
		for i := 1; i < len(v.s.items); i++ {
			if v.s.items[i].kind == itMsg && v.s.items[i-1].kind == itMsg && v.s.pos[i] != v.s.pos[i-1]+v.s.heights[i-1] {
				t.Errorf("gap above item %d", i)
			}
		}
	}
}

func TestCompactGeometryThread(t *testing.T) {
	setDense(t, true)
	for _, scale := range []float32{1, 2} {
		h := spacingHost(t, false)
		tv := NewThreadView(h, tSpacing, spacingTS.root)
		f := scaledFrame(tv, scale, int(420*scale), int(800*scale))
		ts := spacingTS
		until(t, 5*time.Second, "the replies", f, func() bool { return tv.s.rows[ts.r3] != nil && itemHeight(tv.s, ts.r3) > 0 })
		f()
		// no avatar column: the column at the gutter's 16px padding
		checkRow(t, "compact thread root", geomOf(t, tv.s, ts.root), 64, 16, 16, 16+22+16.8, 16+16.8, scale)
		// no grouping: every reply has its sender and time line
		for _, m := range []string{ts.r1, ts.r2, ts.r3} {
			checkRow(t, "compact reply "+m, geomOf(t, tv.s, m), 52, 16, 4, 42.8, 20.8, scale)
		}
	}
}

// Switching the pref relays the list out in place: Compact rows, then
// Clean ones again, with no stale heights (the scroll-gap invariant).
func TestCompactToggleLive(t *testing.T) {
	setDense(t, false)
	h := spacingHost(t, false)
	v := NewChannelView(h, tSpacing)
	v.Preview = true
	f := scaledFrame(v, 1, 900, 700)
	loadList(v, f)
	ts := spacingTS
	until(t, 5*time.Second, "the rows", f, func() bool { return v.s.rows[ts.m3] != nil && itemHeight(v.s, ts.m3) > 0 })
	f()
	if g := geomOf(t, v.s, ts.m1); g.h != 52 || g.colX != 64 {
		t.Fatalf("Clean m1: %+v", g)
	}
	uipref.Set(store.Prefs{MorePrefs: store.MorePrefs{Dense: true}})
	f()
	f()
	if g := geomOf(t, v.s, ts.m1); g.h != 30 || g.colX != 92 {
		t.Errorf("after Compact m1: %+v, want 30 tall at 92", g)
	}
	uipref.Set(store.Prefs{})
	f()
	f()
	if g := geomOf(t, v.s, ts.m1); g.h != 52 || g.colX != 64 {
		t.Errorf("back to Clean m1: %+v", g)
	}
}

// Row heights stay put under Compact too: the idle, fling and scroll-gap
// tests with the pref on.

func TestIdleRowsStayPutCompact(t *testing.T) {
	setDense(t, true)
	TestIdleRowsStayPut(t)
}

func TestFlingScrollNoGapsCompact(t *testing.T) {
	setDense(t, true)
	TestFlingScrollNoGaps(t)
}

func TestFlingPageArrivalEveryFrameCompact(t *testing.T) {
	setDense(t, true)
	TestFlingPageArrivalEveryFrame(t)
}

func TestPrependKeepsJoinSummaryInPlaceCompact(t *testing.T) {
	setDense(t, true)
	TestPrependKeepsJoinSummaryInPlace(t)
}

func TestChannelNameArrivesBetweenFramesCompact(t *testing.T) {
	setDense(t, true)
	TestChannelNameArrivesBetweenFrames(t)
}

func TestSlickViewFlingKeepsRowHeightsCompact(t *testing.T) {
	setDense(t, true)
	TestSlickViewFlingKeepsRowHeights(t)
}

func TestSlickViewIdleRowsStayPutCompact(t *testing.T) {
	setDense(t, true)
	TestSlickViewIdleRowsStayPut(t)
}

// TestCompactRowHeightsMatchSlack: the synthetic #release channel's rows
// (compact_shots_test.go) against the heights Chrome lays out for the same
// rows in Slack's markup under Slack's CSS (.github/scripts/compact_ref.py,
// 1100px, measured with getBoundingClientRect). The long wrapped message
// is left out: Chrome and Gio break its lines at slightly different words.
// So are rows that differ the same way in both layouts, Clean's own
// differences (not Compact's): the Block Kit section (bot, 56 vs 52 in
// both), Clean's broadcast (74 vs 76) and tombstone (80 vs 86).
func TestCompactRowHeightsMatchSlack(t *testing.T) {
	slack := map[bool]map[string]float32{
		true:  {"a1": 30, "a2": 30, "b2": 52, "root": 64, "bc": 52, "join": 33.5, "pin": 49.1, "c1": 30, "gone": 90},
		false: {"a1": 52, "a2": 30, "b2": 52, "root": 86, "pin": 71.1, "c1": 52},
	}
	for _, dense := range []bool{true, false} {
		setDense(t, dense)
		app, _, ts := shotsApp(t)
		v := NewChannelView(coreHost{app: app, th: theme.New(false)}, tShots)
		v.Preview = true
		f := scaledFrame(v, 1, 1100, 1500)
		loadList(v, f)
		until(t, 5*time.Second, "the rows", f, func() bool { return v.s.rows[ts["gone"]] != nil && itemHeight(v.s, ts["gone"]) > 0 })
		f()
		for k, want := range slack[dense] {
			if got := float32(itemHeight(v.s, ts[k])); got-want > 1 || want-got > 1 {
				t.Errorf("dense=%v %s: %vpx, Slack %vpx", dense, k, got, want)
			}
		}
	}
}
