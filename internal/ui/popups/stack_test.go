package popups

import (
	"runtime"
	"testing"
	"time"
	"weak"

	"gioui.org/layout"
)

type stackProbe struct {
	base
	name string
}

// A closed overlay leaves the stacking order once ForgetDismissed runs,
// while what the open ones read (isTop, the pump's walk) stays the same:
// the stack held the last frame's overlays until another was laid out, so
// a closed emoji picker kept its index while no popup was open.
func TestForgetDismissedReleasesClosedOverlays(t *testing.T) {
	defer func(f time.Time, c, p []any) { stackFrame, stackCur, stackPrev = f, c, p }(stackFrame, stackCur, stackPrev)
	stackFrame, stackCur, stackPrev = time.Time{}, nil, nil
	frame := func(n int64, os ...*stackProbe) {
		gtx := layout.Context{Now: time.Unix(n, 0)}
		for _, o := range os {
			stacked(gtx, o)
		}
	}
	under := &stackProbe{name: "under"}
	over := &stackProbe{name: "over"}
	frame(1, under, over)
	frame(2, under, over)
	if !isTop(over) || isTop(under) {
		t.Fatalf("isTop(over)=%v isTop(under)=%v, want true, false", isTop(over), isTop(under))
	}
	w := weak.Make(over)
	over.Dismiss()
	frame(3, under) // the closed one is no longer laid out
	over = nil
	runtime.GC()
	if w.Value() == nil {
		t.Fatal("the stack let go of the closed overlay before ForgetDismissed: the test no longer shows the leak")
	}
	topBefore := isTop(under)
	ForgetDismissed()
	if got := isTop(under); got != topBefore {
		t.Errorf("isTop(under) %v after ForgetDismissed, %v before", got, topBefore)
	}
	runtime.GC()
	if w.Value() != nil {
		t.Error("a closed overlay is still reachable from the stack after ForgetDismissed")
	}
	// the pump's walk: the topmost open overlay is the one under
	var found *stackProbe
	for i := len(stackPrev) - 1; i >= 0; i-- {
		if o, ok := stackPrev[i].(*stackProbe); ok && !o.Dismissed() {
			found = o
			break
		}
	}
	if found != under {
		t.Errorf("topmost open overlay %v, want under", found)
	}
	ForgetDismissed() // open overlays stay
	frame(4, under)
	if !isTop(under) {
		t.Error("the open overlay isn't on top the frame after the closed one went")
	}
}
