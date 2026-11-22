package blockkit

// A container's toggle in a modal or App Home survives the view changing.
// ViewBlocks and AppHomeBlocks (WSwB) render one SUIRenderer whose items
// keep their React key (Q5CE: block_id, else type-index), and
// ContainerBlock's state is a useState reset only by its useEffect on
// is_collapsible / default_collapsed (yWQF): a views.update or an App
// Home publish leaves a container the viewer collapsed collapsed, as a
// message's update does (richview TestCollapseSurvivesUpdates).

import (
	"encoding/json"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/core"
)

func TestSurfaceCollapseSurvivesUpdate(t *testing.T) {
	h, _ := visibleHost()
	blocks := func(title string) json.RawMessage {
		raw, _ := json.Marshal([]any{
			map[string]any{"type": "container", "block_id": "c1", "title": map[string]any{"type": "plain_text", "text": title},
				"is_collapsible": true, "child_blocks": []any{
					map[string]any{"type": "section", "block_id": "s1", "text": map[string]any{"type": "mrkdwn", "text": "Child line one"}},
					map[string]any{"type": "section", "block_id": "s2", "text": map[string]any{"type": "mrkdwn", "text": "Child line two"}}}},
			map[string]any{"type": "section", "block_id": "after", "text": map[string]any{"type": "mrkdwn", "text": "After the container"}},
		})
		return raw
	}
	for _, typ := range []string{"modal", "home"} {
		s := newSurface(h, &core.View{Type: typ, Blocks: blocks("Deploy")})
		var r input.Router
		frame := func() int {
			gtx := visGtx(480)
			gtx.Source = r.Source()
			d := s.Layout(gtx)
			r.Frame(gtx.Ops)
			return d.Size.Y
		}
		open := frame()
		frame()
		// the header: the first block, 4px down, under the container's
		// 16px top margin; its whole row toggles
		pt := f32.Pt(40, float32(4+16+12))
		r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt},
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt})
		frame()
		closed := frame()
		if closed >= open {
			t.Fatalf("%s: the header click didn't collapse the container (height %d, open %d)", typ, closed, open)
		}
		// the app updates the view: the same blocks, a new title
		s.set(&core.View{Type: typ, Blocks: blocks("Deploy (2 of 3)")}, true)
		frame()
		if got := frame(); got != closed {
			t.Errorf("%s: after the update the surface is %dpx, want %d (still collapsed; %d open)", typ, got, closed, open)
		}
		// a new view (views.push / a different view) starts open again
		s.set(&core.View{Type: typ, Blocks: blocks("Deploy")}, false)
		frame()
		if got := frame(); got != open {
			t.Errorf("%s: a new view is %dpx, want %d (open)", typ, got, open)
		}
	}
}

// TestSurfaceNestedActionError: in a modal and in App Home, a failed
// action from a container's actions child draws the Actions renderable's
// error line under its buttons (/jJF renderBlock: isError and
// actionError from the inherited interactive state), as a top-level
// actions block does: 4px under the buttons, 20px tall.
func TestSurfaceNestedActionError(t *testing.T) {
	h, _ := visibleHost()
	raw, _ := json.Marshal([]any{
		map[string]any{"type": "container", "block_id": "c1", "title": map[string]any{"type": "plain_text", "text": "Deploy"},
			"child_blocks": []any{map[string]any{"type": "actions", "block_id": "a1", "elements": []any{visBtn("go")}}}},
		map[string]any{"type": "section", "block_id": "after", "text": map[string]any{"type": "mrkdwn", "text": "After"}},
	})
	for _, typ := range []string{"modal", "home"} {
		s := newSurface(h, &core.View{Type: typ, Blocks: raw})
		height := func() int {
			s.Layout(visGtx(480))
			return s.Layout(visGtx(480)).Size.Y
		}
		ok := height()
		s.blkErr["a1"] = "Something went wrong"
		s.gen++ // as fire's answer does
		if got := height(); got != ok+24 {
			t.Errorf("%s: with the nested action's error the surface is %dpx, want %d (the error line under the buttons)", typ, got, ok+24)
		}
	}
}
