package messages

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// rpCtxHost is rpHost recording the overlays the list shows.
type rpCtxHost struct {
	rpHost
	overlays []popups.Overlay
}

func (h *rpCtxHost) ShowOverlay(o popups.Overlay) { h.overlays = append(h.overlays, o) }

func (h *rpCtxHost) menus() []*popups.Menu {
	var out []*popups.Menu
	for _, o := range h.overlays {
		if m, ok := o.(*popups.Menu); ok {
			out = append(out, m)
		}
	}
	return out
}

// Under a plugin that patches ReactionBar (WhoReacted, OwlMode: the pill's
// children come back through the plugin's tree), a right-click on a
// reaction's emoji is still the desktop shell's image menu for the emoji,
// and one on its count still opens nothing, as on the native pill.
func TestContextMenuOnPatchedReaction(t *testing.T) {
	tt, u, rh := rpEnv(t, false)
	h := &rpCtxHost{rpHost: rh}
	v := NewChannelView(h, rpChan)
	v.Preview = true
	var r input.Router
	ops := new(op.Ops)
	now := time.Now()
	var clock time.Duration
	frame := func() {
		ops.Reset()
		now = now.Add(20 * time.Millisecond)
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(rpW, rpH)),
			Now: now, Source: r.Source()}
		slackc.Frame(gtx)
		v.Layout(gtx)
		r.Frame(ops)
	}
	rightClick := func(p f32.Point) {
		clock += time.Second
		r.Queue(
			pointer.Event{Kind: pointer.Move, Position: p, Source: pointer.Mouse, Time: clock},
			pointer.Event{Kind: pointer.Press, Position: p, Source: pointer.Mouse, Buttons: pointer.ButtonSecondary, Time: clock + 10*time.Millisecond},
			pointer.Event{Kind: pointer.Release, Position: p, Source: pointer.Mouse, Time: clock + 60*time.Millisecond},
		)
		for k := 0; k < 3; k++ {
			frame()
		}
	}
	// where the native pill draws the first custom emoji and its count:
	// the patched pill draws them there too (TestPatchedReactionPillLikeNative)
	until(t, 10*time.Second, "the native pills", frame, func() bool {
		row := v.s.rows[rpCustom]
		return row != nil && len(row.reactRects) >= 3 && len(row.pills) >= 3 && len(row.pills[0].emojiAt) > 0 && !row.pills[0].countAt.Empty()
	})
	row := v.s.rows[rpCustom]
	pill, pl := row.reactRects[0], row.pills[0]
	mid := func(r image.Rectangle) image.Point { return r.Min.Add(r.Size().Div(2)) }
	emojiPt, countPt := pill.Min.Add(mid(pl.emojiAt[0].r)), pill.Min.Add(mid(pl.countAt))
	wantURL := pl.emojiAt[0].url

	install(t, tt, rpPass)
	until(t, 10*time.Second, "the ReactionBar patch", frame, func() bool { return slackc.Patched("ReactionBar") })
	until(t, 15*time.Second, "the bar's tree", frame, func() bool {
		return u.anyTree(`"reactionKey":"message-` + rpCustom + `-` + rpChan + `"`)
	})
	for k := 0; k < 10; k++ {
		frame()
	}
	at := func(p image.Point) f32.Point {
		top := v.s.pos[v.s.sel.keyIdx[rpCustom]]
		return f32.Pt(float32(p.X)+.5, float32(top+p.Y)+.5)
	}
	rightClick(at(emojiPt))
	ms := h.menus()
	if len(ms) != 1 {
		t.Fatalf("a right-click on the patched pill's emoji opened %d menus, want 1", len(ms))
	}
	var got []string
	for _, it := range ms[0].Items() {
		got = append(got, it.Label)
	}
	if strings.Join(got, "|") != "Copy Image|Copy Image URL" {
		t.Fatalf("patched pill emoji menu %q, want the image menu", got)
	}
	if c := clipOf(t, ms[0].Items()[1]); c != wantURL {
		t.Errorf("Copy Image URL copied %q, want the emoji's %q", c, wantURL)
	}
	rightClick(at(countPt))
	if n := len(h.menus()); n != 1 {
		t.Errorf("a right-click on the patched pill's count opened a menu (%d menus)", n)
	}
}
