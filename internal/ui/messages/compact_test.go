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
	"encoding/json"
	"image"
	"os"
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

// TestRowHeightsMatchSlack: every row of the synthetic #release channel
// and its thread (compact_shots_test.go), in Clean and Compact, against the
// height Chrome lays the same row out at in Slack's markup under Slack's
// shipping CSS (.github/scripts/compact_ref.py, getBoundingClientRect at
// 1100px and the 520px thread pane). The table is
// testdata/slack-row-heights.json; the compact-shots workflow measures it
// again on every run and fails when it no longer matches.
func TestRowHeightsMatchSlack(t *testing.T) {
	raw, err := os.ReadFile("testdata/slack-row-heights.json")
	if err != nil {
		t.Fatal(err)
	}
	var slack map[string]map[string]float32
	if err := json.Unmarshal(raw, &slack); err != nil {
		t.Fatal(err)
	}
	for _, dense := range []bool{false, true} {
		layoutName := map[bool]string{false: "clean", true: "compact"}[dense]
		got := shotsRowHeights(t, dense)
		for _, list := range []string{"channel", "thread"} {
			want := slack[list+"-"+layoutName]
			if len(want) == 0 {
				t.Fatalf("no %s-%s heights in the table", list, layoutName)
			}
			for k, w := range want {
				g, ok := got[list+"-"+layoutName][k]
				if !ok {
					t.Errorf("%s-%s %s: no row", list, layoutName, k)
					continue
				}
				if g-w > 1 || w-g > 1 {
					t.Errorf("%s-%s %s: %vpx, Slack %vpx", list, layoutName, k, g, w)
				}
			}
		}
	}
}

// shotsRowHeights lays the synthetic channel and thread out at 1x (1100px,
// the thread pane 520px) and returns each row's height by list and key.
func shotsRowHeights(t *testing.T, dense bool) map[string]map[string]float32 {
	t.Helper()
	setDense(t, dense)
	app, _, ts := shotsApp(t)
	host := coreHost{app: app, th: theme.New(false)}
	v := NewChannelView(host, tShots)
	v.Preview = true
	f := scaledFrame(v, 1, 1100, 1500)
	loadList(v, f)
	until(t, 5*time.Second, "the rows", f, func() bool { return v.s.rows[ts["gone"]] != nil && itemHeight(v.s, ts["gone"]) > 0 })
	f()
	tv := NewThreadView(host, tShots, ts["root"])
	ft := scaledFrame(tv, 1, 520, 1100)
	until(t, 5*time.Second, "the replies", ft, func() bool { return tv.s.rows[ts["r3"]] != nil && itemHeight(tv.s, ts["r3"]) > 0 })
	ft()
	return rowHeights(v.s, tv.s, ts, dense)
}

// rowHeights reads the rows' heights back, keyed like compact-ref-rows.json.
func rowHeights(ch, th *listState, ts map[string]string, dense bool) map[string]map[string]float32 {
	layoutName := map[bool]string{false: "clean", true: "compact"}[dense]
	out := map[string]map[string]float32{"channel-" + layoutName: {}, "thread-" + layoutName: {}}
	for _, k := range shotChannelKeys {
		if h := itemHeight(ch, ts[k]); h > 0 {
			out["channel-"+layoutName][k] = float32(h)
		}
	}
	for _, k := range shotThreadKeys {
		if h := itemHeight(th, ts[k]); h > 0 {
			out["thread-"+layoutName][k] = float32(h)
		}
	}
	return out
}
