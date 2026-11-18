package slackc

import (
	"encoding/json"
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/ui/slackc/css"
)

// A frame's mousemove reaches JS ahead of the press or release that follows
// it in the frame, coalesced to the newest position before each, as a
// browser sends its pending coalesced moves before the next mousedown or
// mouseup. Sent at the frame's end, the move came after the press and the
// release: a drag's mousedown handler saw the pointer where the previous
// frame's move had left it.
func TestFrameMoveBeforePress(t *testing.T) {
	withStyles(t, func(n *Node, s *css.Style) {
		if n.ID == "pad" {
			s.Display = css.DisplayBlock
			s.Width, s.Height = px(200), px(100)
		}
	})
	var got []string
	testDispatch = func(k int64, b []byte) {
		var m struct {
			Type    string  `json:"type"`
			ClientX float64 `json:"clientX"`
		}
		if err := json.Unmarshal(b, &m); err != nil {
			t.Errorf("bad event json %s: %v", b, err)
		}
		switch m.Type {
		case "mousemove", "mousedown", "mouseup":
			got = append(got, fmt.Sprintf("%s@%.0f", m.Type, m.ClientX))
		}
	}
	defer func() { testDispatch = nil }()
	windowSize = image.Pt(1000, 800)
	nodes := mustTree(t, `{"t":"div","k":1,"p":{"id":"pad"},"on":{"mousemove":3,"mousedown":4,"mouseup":5}}`)
	v := NewView(1)
	var r input.Router
	frame := func() {
		gtx := testGtx(400, 400)
		gtx.Source = r.Source()
		v.Layout(gtx, nodes, "", nil, nil)
		r.Frame(gtx.Ops)
	}
	frame()
	frame()
	ms := func(n int) time.Duration { return time.Duration(n) * time.Millisecond }
	r.Queue(
		pointer.Event{Kind: pointer.Move, Position: f32.Pt(10, 20), Source: pointer.Mouse, Time: ms(10)},
		pointer.Event{Kind: pointer.Move, Position: f32.Pt(20, 20), Source: pointer.Mouse, Time: ms(12)},
		pointer.Event{Kind: pointer.Press, Position: f32.Pt(20, 20), Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Time: ms(14)},
		// moves with the button held: the router delivers them as drags
		pointer.Event{Kind: pointer.Move, Position: f32.Pt(30, 20), Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Time: ms(16)},
		pointer.Event{Kind: pointer.Move, Position: f32.Pt(40, 20), Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Time: ms(18)},
		pointer.Event{Kind: pointer.Release, Position: f32.Pt(40, 20), Source: pointer.Mouse, Time: ms(20)},
		pointer.Event{Kind: pointer.Move, Position: f32.Pt(50, 20), Source: pointer.Mouse, Time: ms(22)},
	)
	frame()
	frame() // what Gio held back after a focus change
	if got, want := strings.Join(got, ", "), "mousemove@20, mousedown@20, mousemove@40, mouseup@40, mousemove@50"; got != want {
		t.Errorf("dispatched %s,\nwant %s", got, want)
	}
}
