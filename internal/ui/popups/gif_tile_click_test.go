package popups

// A GIF tile pressed while the grid changes (thread-header-hover-2, the
// lost-click sweep): Slack's GifPicker keys each GifListItem by its GIF,
// so a tile held while the next page lands keeps its click, and one held
// while a new search's results land is gone with its GIF (the release is
// over another element: no pick). Clack's tiles were a slice by index: a
// page grew it past its capacity and moved every tile's Clickable (Gio
// routes a press by its handler's address: the release went nowhere), and
// a new search left the held Clickable under another GIF, which the
// release then picked. Synthetic GIFs, no fetch.

import (
	"fmt"
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

func synthGifs(prefix string, n int) []Gif {
	out := make([]Gif, n)
	for i := range out {
		id := fmt.Sprintf("%s%02d", prefix, i)
		out[i] = Gif{ID: id, Title: "synthetic " + id, URL: "https://media.invalid/" + id + "/giphy.webp",
			Preview: "https://media.invalid/" + id + "/200w.webp", Still: "https://media.invalid/" + id + "/200w_s.gif", W: 200, H: 200}
	}
	return out
}

func TestGifTileClickSurvivesGridChanges(t *testing.T) {
	for _, tc := range []struct {
		name string
		land func(g *gifPanel)
		want string // the GIF the release picks ("" none)
	}{
		{"the next page lands", func(g *gifPanel) {
			g.items = append(g.items, synthGifs("pageTwo", 9)...)
			gifLanded(g)
		}, "pageOne00"},
		{"a new search's results land", func(g *gifPanel) {
			g.items = append(g.items[:0], synthGifs("search", 3)...)
			gifLanded(g)
		}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := hcEnv(t)
			var picked []string
			p := NewEmojiPickerAt(h, image.Pt(400, 700), nil)
			p.SetGifHandler(func(g Gif) { picked = append(picked, g.ID) })
			p.setGifMode(true)
			g := &p.gif
			g.started = true // synthetic results, no fetch
			g.items = synthGifs("pageOne", 3)
			gifLanded(g)
			var rt input.Router
			ops := new(op.Ops)
			frame := func() {
				ops.Reset()
				gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1000, 800)),
					Now: time.Now(), Source: rt.Source()}
				p.Layout(gtx)
				rt.Frame(ops)
			}
			for i := 0; i < 3; i++ {
				frame()
			}
			// the first tile: the point where its button reads hovered
			first := g.items[0]
			var at f32.Point
			found := false
			r := p.Rect()
			clk := time.Second
			for y := r.Min.Y; y < r.Max.Y && !found; y += 12 {
				for x := r.Min.X; x < r.Max.X && !found; x += 12 {
					clk += time.Millisecond
					q := f32.Pt(float32(x), float32(y))
					rt.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: q, Time: clk})
					frame()
					if gifHovered(g, 0) {
						at, found = q, true
					}
				}
			}
			if !found {
				t.Fatalf("no point over the first GIF's tile in the picker %v", r)
			}
			frame()
			rt.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: at, Time: clk + 10*time.Millisecond})
			frame()
			tc.land(g)
			frame()
			frame()
			rt.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: at, Time: clk + 110*time.Millisecond})
			frame()
			frame()
			got := ""
			if len(picked) > 0 {
				got = picked[0]
			}
			if got != tc.want {
				t.Errorf("%s while %s was down: the release picked %q, want %q (Slack keys each GifListItem by its GIF)", tc.name, first.ID, got, tc.want)
			}
		})
	}
}
