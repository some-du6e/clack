package chart

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

// Synthetic fixtures only.

// layWithin lays spec out, failing when it takes longer than 10s (a loop
// that doesn't end).
func layWithin(t *testing.T, name string, f func() *Layout) *Layout {
	t.Helper()
	done := make(chan *Layout, 1)
	go func() { done <- f() }()
	select {
	case l := <-done:
		return l
	case <-time.After(10 * time.Second):
		t.Fatalf("%s: no layout after 10s", name)
	}
	return nil
}

func finite(vs ...float64) bool {
	for _, v := range vs {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// Values the value axis' tick loop can't step through: tiny ones whose
// step rounds to 0 at 20 decimals, and near float64's maximum, where the
// niced extent overflows to +Inf. Interval.getTicks stops at a tick that
// doesn't move or isn't finite and gives none past 3000; the plot keeps
// finite geometry (scaleMapper's normalize is 0.5 for a zero span).
func TestValueAxisExtremes(t *testing.T) {
	var pal Palette
	m := func(s string, w Weight) float64 { return 7 * float64(len(s)) }
	for _, c := range []struct{ name, data string }{
		{"tiny", `[{"label":"a","value":1e-25},{"label":"b","value":2e-25}]`},
		{"small", `[{"label":"a","value":3e-21},{"label":"b","value":1e-20}]`},
		{"huge", `[{"label":"a","value":1.6e308}]`},
	} {
		d := ParseChart(json.RawMessage(`{"type":"bar","series":[{"name":"s","data":` + c.data + `}],"axis_config":{"categories":["a","b"]}}`))
		l := layWithin(t, c.name, func() *Layout { return Lay(Build(&d, &pal), 598, 360, m) })
		if len(l.Splits) > tickSafeLimit+2 {
			t.Errorf("%s: %d split lines, want at most %d", c.name, len(l.Splits), tickSafeLimit+2)
		}
		if !finite(l.Grid.X, l.Grid.Y, l.Grid.W, l.Grid.H) {
			t.Errorf("%s: grid %+v", c.name, l.Grid)
		}
		for _, s := range l.Splits {
			if !finite(s.X1, s.Y1, s.X2, s.Y2) {
				t.Errorf("%s: split line %+v", c.name, s)
				break
			}
		}
		for _, b := range l.Bars {
			if !finite(b.X, b.Y, b.W, b.H) {
				t.Errorf("%s: bar %+v", c.name, b)
				break
			}
		}
		for _, lb := range l.Labels {
			if !finite(lb.X, lb.Y) {
				t.Errorf("%s: label %q at %v,%v", c.name, lb.Text, lb.X, lb.Y)
				break
			}
		}
	}
}

// A pie whose values sum under float64's least normal: 2π over the sum is
// +Inf, so the first segment ends at +Inf and the second starts there.
// The hover still answers, on the held segment's grown rim too (where
// only the second segment is tested), and doesn't loop.
func TestPieHoverInfiniteAngles(t *testing.T) {
	var pal Palette
	d := ParseChart(json.RawMessage(`{"type":"pie","segments":[{"label":"a","value":1e-308},{"label":"b","value":1e-308}]}`))
	l := layWithin(t, "pie", func() *Layout { return Lay(Build(&d, &pal), 598, 360, nil) })
	var s *SectorItem
	for i := range l.Sectors {
		if l.Sectors[i].Index == 1 {
			s = &l.Sectors[i]
		}
	}
	if s == nil || !math.IsInf(s.Start, 1) {
		t.Fatalf("sectors %+v: want the second one starting at +Inf", l.Sectors)
	}
	x, y := s.CX+s.R+pieScaleSize/2, s.CY // the held segment's 5px rim
	got := make(chan int, 1)
	go func() { got <- l.HoverAtHeld(x, y, 1) }()
	select {
	case h := <-got:
		if h == 1 {
			t.Errorf("the rim of a segment with no angle: %d", h)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("HoverAtHeld didn't return in 10s")
	}
}
