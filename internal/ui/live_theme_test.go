package ui

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// TestLiveThemeSwitchMatchesFresh: a live Light/Dark switch (Appearance,
// setUserPrefByApi dark_mode → Theme.SetDark) must draw what a client
// started in that mode draws. Each scene is set up in the other mode, the
// theme switched, and the window compared pixel by pixel with a fresh
// shell started in the target mode and set up the same way: the sidebar,
// the channel's messages, the composer with typed text, and an open emoji
// picker, menu and modal. A caret-thin difference (the blink) is allowed.
// With LIVE_THEME_SHOTS_DIR set the three images (live, fresh, diff) are
// written there. Needs a GPU: CI runs it under Mesa (composer-audit-shots).
func TestLiveThemeSwitchMatchesFresh(t *testing.T) {
	dir := os.Getenv("LIVE_THEME_SHOTS_DIR")
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	scenes := []struct {
		name  string
		setup func(e *actShellEnv)
	}{
		{"channel", func(*actShellEnv) {}},
		{"emoji-picker", func(e *actShellEnv) { e.a.OpenEmojiPicker(image.Pt(900, 760), func(string) {}) }},
		{"menu", func(e *actShellEnv) {
			e.a.menu(image.Pt(500, 300), []MenuItem{{Label: "Copy link", Icon: "link"}, {Label: "Mark unread", Icon: "mark-unread"}})
		}},
		{"modal", func(e *actShellEnv) {
			e.a.ShowOverlay(popups.NewModal(e.a, popups.ModalOpts{Title: "Theme check",
				Buttons: []*popups.ModalButton{{Label: "Cancel", Kind: popups.BtnOutline}, {Label: "Save", Kind: popups.BtnPrimary}}}))
		}},
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		for _, sc := range scenes {
			t.Run(sc.name+"-to-"+mode, func(t *testing.T) {
				sz := fakeslack.Small
				sz.Views = true
				fs, err := fakeslack.Start(sz) // one server: the same synthetic data in both
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = fs.Close() })
				shot := func(start bool) *image.RGBA {
					e := newActShellOn(t, fs)
					e.a.th.SetDark(start)
					e.frames(2)
					e.a.Navigate("c/" + fakeslack.ViewsChannel)
					e.until("the composer", func() bool {
						cv := e.a.convos[fakeslack.ViewsChannel+"/"]
						return cv != nil && cv.comp != nil && !cv.compRect.Empty()
					})
					// the draft outlives a shell (it's the process's): type into
					// an empty box only
					if c, ok := e.a.convos[fakeslack.ViewsChannel+"/"].comp.(interface {
						InsertText(string)
						DocText() string
					}); ok && c.DocText() == "" {
						c.InsertText("typed in the composer")
					}
					e.frames(10)
					sc.setup(e)
					e.frames(10)
					if start != dark {
						e.a.th.SetDark(dark) // what setDark's setUserPrefByApi native does
						e.a.Invalidate()
					}
					e.frames(10)
					return pixeltest.Render(t, e.sz.X, e.sz.Y, &e.ops)
				}
				live, fresh := shot(!dark), shot(dark)
				boxes, diff := themeDiff(live, fresh)
				if dir != "" {
					for name, img := range map[string]image.Image{"live": live, "fresh": fresh, "diff": diff} {
						writePNG(t, filepath.Join(dir, fmt.Sprintf("live-theme-%s-to-%s-%s.png", sc.name, mode, name)), img)
					}
				}
				if len(boxes) > 0 {
					t.Errorf("after a live switch to %s, %d region(s) differ from a fresh start: %v", mode, len(boxes), boxes)
				}
			})
		}
	}
}

// themeDiff returns the bounding boxes of the regions where a and b differ
// by more than a few levels in any channel, merged over an 8px grid, with
// caret-thin ones (≤ 2px wide) left out, and a diff image (red where they
// differ, b dimmed elsewhere).
func themeDiff(a, b *image.RGBA) ([]image.Rectangle, *image.RGBA) {
	r := a.Bounds().Intersect(b.Bounds())
	out := image.NewRGBA(r)
	const cell = 8
	cw, ch := r.Dx()/cell+1, r.Dy()/cell+1
	cells := make([]image.Rectangle, cw*ch) // the differing pixels' bounds per cell
	abs := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			pa, pb := a.RGBAAt(x, y), b.RGBAAt(x, y)
			if abs(pa.R, pb.R) > 6 || abs(pa.G, pb.G) > 6 || abs(pa.B, pb.B) > 6 {
				out.SetRGBA(x, y, color.RGBA{R: 255, A: 255})
				i := (y/cell)*cw + x/cell
				cells[i] = cells[i].Union(image.Rect(x, y, x+1, y+1))
				continue
			}
			out.SetRGBA(x, y, color.RGBA{R: pb.R / 3, G: pb.G / 3, B: pb.B / 3, A: 255})
		}
	}
	// merge neighbouring cells into regions
	seen := make([]bool, len(cells))
	var boxes []image.Rectangle
	for i := range cells {
		if seen[i] || cells[i].Empty() {
			continue
		}
		box := image.Rectangle{}
		stack := []int{i}
		seen[i] = true
		for len(stack) > 0 {
			j := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			box = box.Union(cells[j])
			cx, cy := j%cw, j/cw
			for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {-1, -1}, {1, -1}, {-1, 1}} {
				nx, ny := cx+d[0], cy+d[1]
				if nx < 0 || ny < 0 || nx >= cw || ny >= ch {
					continue
				}
				k := ny*cw + nx
				if !seen[k] && !cells[k].Empty() {
					seen[k] = true
					stack = append(stack, k)
				}
			}
		}
		if box.Dx() <= 2 { // the caret, mid-blink in one of them
			continue
		}
		boxes = append(boxes, box)
	}
	sort.Slice(boxes, func(i, j int) bool { return boxes[i].Dx()*boxes[i].Dy() > boxes[j].Dx()*boxes[j].Dy() })
	return boxes, out
}

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
