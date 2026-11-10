package messages

import (
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// Simplified layout mode's persistent message actions (persist.go):
// every visible message gets its "Message actions" button, the body column
// gives the button its 36px, and row heights stay put (the idle, fling
// and page-arrival tests with the menu on).

func setPersist(t *testing.T, on bool) {
	t.Helper()
	old := PersistActions
	PersistActions = func() bool { return on }
	t.Cleanup(func() { PersistActions = old })
}

func TestPersistActionsButtons(t *testing.T) {
	setPersist(t, false)
	h := spacingHost(t, false)
	v := NewChannelView(h, tSpacing)
	v.Preview = true
	f := scaledFrame(v, 1, 900, 700)
	loadList(v, f)
	ts := spacingTS
	until(t, 5*time.Second, "the rows", f, func() bool { return v.s.rows[ts.m3] != nil && itemHeight(v.s, ts.m3) > 0 })
	f()
	if len(v.s.pbtn) != 0 {
		t.Fatalf("persistent buttons without the mode: %d", len(v.s.pbtn))
	}
	if p := v.s.rc.actionsPad(scaledCtx(1)); p != 0 {
		t.Fatalf("actionsPad off = %d", p)
	}
	setPersist(t, true)
	f()
	f()
	if !v.s.rc.persist {
		t.Fatal("rowCtx.persist not set")
	}
	if p := v.s.rc.actionsPad(scaledCtx(1)); p != 36 {
		t.Fatalf("channel actionsPad = %d, want 36 (padding-right 44 over 8)", p)
	}
	for _, m := range []string{ts.m1, ts.m3} {
		if v.s.pbtn[m] == nil {
			t.Errorf("no Message actions button for %s", m)
		}
	}
	if v.s.tbFor != -1 {
		t.Errorf("the hover toolbar drew with the persistent menu on (row %d)", v.s.tbFor)
	}
}

func TestIdleRowsStayPutBBM(t *testing.T) {
	setPersist(t, true)
	TestIdleRowsStayPut(t)
}

func TestFlingScrollNoGapsBBM(t *testing.T) {
	setPersist(t, true)
	TestFlingScrollNoGaps(t)
}

func TestFlingPageArrivalEveryFrameBBM(t *testing.T) {
	setPersist(t, true)
	TestFlingPageArrivalEveryFrame(t)
}

func TestIdleRowsStayPutBBMCompact(t *testing.T) {
	setPersist(t, true)
	setDense(t, true)
	TestIdleRowsStayPut(t)
}

func scaledCtx(scale float32) layout.Context {
	return layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale}}
}
