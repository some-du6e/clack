package messages

// A reaction pill pressed while someone else's reaction changes the bar
// (the lost-click sweep, 2026-11-22): Slack's ReactionBar keys each
// Reaction by its emoji, so a pill pressed while another group comes or
// goes stays that emoji's element and its release is its click. Clack's
// pills were a slice by group index: a new group grew the slice (every
// pill's Clickable moved, and Gio routes a press by its handler's
// address, so the release went nowhere). (A group before the pressed
// pill going moves the pressed pill from under a still pointer, which
// loses the click in Slack too: the release is over another element.)
// Synthetic: the emoji fixture's "shipped it" message (grin: the viewer's
// and Ana's; +1: Cara's), with Gio's router routing a press and a release
// ~100ms apart, the pointer still, a new emoji landing between them, four
// times over (the slice grew past its capacity at 3 and at 5).

import (
	"image"
	"net/url"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestReactionPillClickSurvivesOtherReactions(t *testing.T) {
	app, fs, ts := esApp(t)
	target := ts["inline"]
	var mu sync.Mutex
	var calls []url.Values
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "reactions.add" || method == "reactions.remove" {
			mu.Lock()
			cp := url.Values{"method": {method}}
			for k, v := range f {
				cp[k] = v
			}
			calls = append(calls, cp)
			mu.Unlock()
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetHook(nil) })
	th := theme.New(false)
	host := identityHost{coreHost: coreHost{app: app, th: th}, imgs: imgcache.New((&rpFetch{}).fetch, "", nil)}
	v := NewChannelView(host, esChan)
	var rt input.Router
	ops := new(op.Ops)
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(700, 420)),
			Now: time.Now(), Source: rt.Source()}
		v.Layout(gtx)
		rt.Frame(ops)
	}
	grin := func() (image.Rectangle, bool) {
		for _, r := range v.ReactionRects() {
			if r.TS == target && r.Name == "grin" {
				return r.R, true
			}
		}
		return image.Rectangle{}, false
	}
	until(t, 8*time.Second, "the message's pills", frame, func() bool { _, ok := grin(); return ok })
	clk := time.Second
	for i, emoji := range []string{"eyes", "tada", "rocket", "fire"} {
		r, ok := grin()
		if !ok {
			t.Fatalf("click %d: no grin pill drawn", i+1)
		}
		p := f32.Pt(float32(r.Min.X+r.Dx()/2), float32(r.Min.Y+r.Dy()/2))
		clk += time.Second
		rt.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p, Time: clk})
		frame()
		frame()
		rt.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: p, Time: clk + 10*time.Millisecond})
		frame()
		injectEvent(app, esChan, "reaction_added", "", map[string]any{"user": "UCARA0001", "reaction": emoji,
			"item": map[string]any{"type": "message", "channel": esChan, "ts": target}})
		until(t, 5*time.Second, emoji+" drawn while grin is down", frame, func() bool {
			row := v.s.rows[target]
			return row != nil && len(row.groups) == 3+i
		})
		if r2, _ := grin(); r2 != r {
			t.Fatalf("click %d: grin moved from %v to %v under the still pointer", i+1, r, r2)
		}
		rt.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p, Time: clk + 110*time.Millisecond})
		frame()
		frame()
		want := []string{"reactions.remove", "reactions.add"}[i%2] // the viewer's grin goes, then comes back, …
		got := ""
		for dl := time.Now().Add(5 * time.Second); time.Now().Before(dl) && got == ""; time.Sleep(10 * time.Millisecond) {
			frame()
			mu.Lock()
			if len(calls) > i {
				got = calls[i].Get("method") + " name=" + calls[i].Get("name")
			}
			mu.Unlock()
		}
		if got != want+" name=grin" {
			t.Fatalf("click %d on grin, %s landing while it was down: got %q, want %s name=grin (Slack's pressed Reaction keeps its click)", i+1, emoji, got, want)
		}
		frame()
	}
}
