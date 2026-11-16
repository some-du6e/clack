package messages

// The message pane's UnreadBanner ("↑ N new messages ✕") drawn through
// Gio's GPU renderer (Mesa llvmpipe in the messages-audit-shots
// workflow), light and dark, with the "New" line above the view
// (arrow-up) and in it (arrow-down), on exp_messages_test.go's synthetic
// channel. Nothing talks to Slack.
//
// Slack's IA4 CSS puts the 13px bold text on a 28px line (line-height
// 28) and both svg.is-inline icons (13px, vertical-align -.125em) on that
// line, so the text's figures and the arrow and ✕ share a centre about
// 14.5px down the 28px pill, and nothing reaches its edges. Before this
// test the text sat at the line's top (no CSS line box: Gio put all the
// leading under it), 6px above the icons and against the pill's edge.
//
// unread-banner-{up,down}-{light,dark}.png: the pill and 12px round it.

import (
	"image"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func TestUnreadBannerShots(t *testing.T) {
	dir := os.Getenv("MESSAGES_AUDIT_SHOTS_DIR")
	for _, dark := range []bool{false, true} {
		for _, tc := range []struct {
			name string
			h    int
			icon string
		}{
			{"up", 130, "arrow-up"},
			{"down", 700, "arrow-down"},
		} {
			mode := map[bool]string{false: "light", true: "dark"}[dark]
			tag := tc.name + "-" + mode
			app, _ := expApp(t, nil)
			app.S.SetCounts(&slack.Counts{Channels: []slack.CountEntry{{ID: tExp, LastRead: expTS["file"], Latest: expTS["bcast"], HasUnreads: true}}})
			th := theme.New(dark)
			v := NewChannelView(coreHost{app: app, th: th}, tExp)
			v.Preview = true
			const w = 900
			ops := new(op.Ops)
			f := func() { shotFrame(v, th, w, tc.h, ops) }
			until(t, 5*time.Second, tag+" banner", f, func() bool { return v.s.ub.text != "" })
			for i := 0; i < 3; i++ {
				f()
			}
			ub := &v.s.ub
			if ub.icon != tc.icon {
				t.Fatalf("%s: banner icon %s, want %s", tag, ub.icon, tc.icon)
			}
			img := pixeltest.Render(t, w, tc.h, ops)
			o := v.s.origin
			msgR, closeR := ub.msgR.Add(o), ub.closeR.Add(o)
			pill := msgR.Union(closeR)
			checkUnreadBannerPixels(t, tag, img, pill, msgR, closeR)
			if dir != "" {
				savePNG(t, filepath.Join(dir, "unread-banner-"+tag+".png"), img.SubImage(pill.Inset(-12).Intersect(img.Bounds())))
			}
		}
	}
}

// checkUnreadBannerPixels holds the pill to Slack's CSS: its top edge is
// drawn (not clipped), the text's first glyph and both icons are centred
// together on the 28px line, and no ink touches the pill's edges.
func checkUnreadBannerPixels(t *testing.T, tag string, img *image.RGBA, pill, msgR, closeR image.Rectangle) {
	t.Helper()
	blue := bannerBlue
	near := func(x, y int, r, g, b uint8, tol int) bool {
		c := img.RGBAAt(x, y)
		d := func(a, b uint8) int { return max(int(a)-int(b), int(b)-int(a)) }
		return d(c.R, r) <= tol && d(c.G, g) <= tol && d(c.B, b) <= tol
	}
	// the top and bottom rows at the centre are the pill's blue: nothing
	// over it or a parent clip cuts it
	cx := (pill.Min.X + pill.Max.X) / 2
	for _, y := range []int{pill.Min.Y, pill.Max.Y - 1} {
		if !near(cx, y, blue.R, blue.G, blue.B, 6) {
			t.Errorf("%s: pill row %d at x %d is %v, want #1264a3 (clipped or covered)", tag, y, cx, img.RGBAAt(cx, y))
		}
	}
	// inside: the pixel lies 1px or more inside the pill's 14px-radius
	// shape (past its round ends a light page shows through)
	rad := float64(pill.Dy()) / 2
	inside := func(x, y int) bool {
		px, py := float64(x)+0.5, float64(y)+0.5
		cy := float64(pill.Min.Y) + rad
		ccx := min(max(px, float64(pill.Min.X)+rad), float64(pill.Max.X)-rad)
		dx, dy := px-ccx, py-cy
		return dx*dx+dy*dy <= (rad-1)*(rad-1)
	}
	// ink: the white text and glyphs over the blue (green well above
	// #1264a3's 0x64)
	ink := func(x0, x1 int) (top, bot int) {
		top, bot = -1, -1
		for y := pill.Min.Y; y < pill.Max.Y; y++ {
			for x := x0; x < x1; x++ {
				if inside(x, y) && img.RGBAAt(x, y).G >= 0xb4 {
					if top < 0 {
						top = y
					}
					bot = y
					break
				}
			}
		}
		return top, bot
	}
	gs := 13
	textX := msgR.Min.X + 12 + gs + 4
	parts := []struct {
		name   string
		x0, x1 int
	}{
		{"arrow", msgR.Min.X + 12, msgR.Min.X + 12 + gs},
		{"text's first glyph", textX, textX + 6},
		{"close", closeR.Min.X + 4, closeR.Min.X + 4 + gs},
	}
	var centres []float64
	for _, p := range parts {
		top, bot := ink(p.x0, p.x1)
		if top < 0 {
			t.Errorf("%s: no %s ink in columns %d..%d", tag, p.name, p.x0, p.x1)
			centres = append(centres, 0)
			continue
		}
		t.Logf("%s: %s ink rows %d..%d of the pill's %d..%d", tag, p.name, top-pill.Min.Y, bot-pill.Min.Y, 0, pill.Dy()-1)
		if top-pill.Min.Y < 6 || pill.Max.Y-1-bot < 6 {
			t.Errorf("%s: %s ink rows %d..%d run within 6px of the 28px pill's edges (Slack's 28px line keeps them 9px in)", tag, p.name, top-pill.Min.Y, bot-pill.Min.Y)
		}
		centres = append(centres, float64(top+bot)/2-float64(pill.Min.Y))
	}
	lo, hi := centres[0], centres[0]
	for _, c := range centres[1:] {
		lo, hi = min(lo, c), max(hi, c)
	}
	if hi-lo > 1.5 {
		t.Errorf("%s: ink centres arrow %.1f, text %.1f, close %.1f: not centred together (Slack's line puts all three at ~14.5)", tag, centres[0], centres[1], centres[2])
	}
}
