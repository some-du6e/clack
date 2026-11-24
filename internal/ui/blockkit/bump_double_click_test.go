package blockkit

import (
	"encoding/json"
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// TestBumpDoubleClickKeepsSubmit: the speed bump's continue button sits
// exactly on the footer's Submit (the sheet covers the footer, as Slack's
// does), so a double click on OK answered the confirm with its first click
// and submitted the modal with its second. The second click of that double
// click doesn't reach Submit; a click on Submit later does. Submit is seen
// by the required field it flags (validate), so nothing goes out.
func TestBumpDoubleClickKeepsSubmit(t *testing.T) {
	h := &confirmHost{th: theme.New(false)}
	blocks, _ := json.Marshal([]any{map[string]any{"type": "input", "block_id": "n", "label": map[string]any{"type": "plain_text", "text": "Name"},
		"element": map[string]any{"type": "plain_text_input", "action_id": "name"}}})
	v := &core.View{ID: "V1", RootViewID: "V1", Type: "modal", Blocks: blocks,
		Title:  mustJSON(Text{Type: "plain_text", Text: "Builds"}),
		Submit: mustJSON(Text{Type: "plain_text", Text: "Done"})}
	s := newStack(h, v)
	var r input.Router
	var ops op.Ops
	now := time.Unix(1_700_000_000, 0)
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Source: r.Source(), Now: now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1200, 800))}
		s.Layout(gtx)
		r.Frame(&ops)
	}
	frames := func() {
		for range 3 {
			frame()
		}
	}
	click := func(p f32.Point, after time.Duration) {
		now = now.Add(after)
		at := time.Duration(now.UnixNano())
		r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p, Time: at},
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: p, Time: at},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p, Time: at + 60*time.Millisecond})
		frames()
	}
	frames()
	sf := s.top()
	oks := 0
	mcConfirmIn(sf, &Confirm{Title: &Text{Type: "plain_text", Text: "Sure?"}}, func() { oks++ }, nil)
	frames()
	if _, up := mcBump(s); !up {
		t.Fatal("no speed bump")
	}
	// OK and Submit: both end 24px in from the card's right and sit 20px
	// above its bottom, 36px tall
	c := s.Rect()
	p := f32.Pt(float32(c.Max.X-24-20), float32(c.Max.Y-20-18))
	click(p, 0)
	if _, up := mcBump(s); up || oks != 1 {
		t.Fatalf("the click on OK: bump up %v, acted %d times; want it answered once (is %v on OK?)", up, oks, p)
	}
	click(p, 150*time.Millisecond) // the double click's second
	if len(sf.errs) != 0 {
		t.Errorf("the second click of a double click on OK reached Submit (errors %v)", sf.errs)
	}
	click(p, 2*time.Second)
	if len(sf.errs) == 0 {
		t.Errorf("a click on Submit 2s later didn't submit (is %v on Submit?)", p)
	}
}
