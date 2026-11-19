package views

// The unread threads pills as Chrome draws Slack's rules for them
// (client-boot-styles' .p-threads_view .p-new_threads_banner and its
// c-icons in the Slack v2 icon font, Lato 13px bold on a 28px line).
// Chrome 157 laid the "↑ 2 unread threads" pill out at 1x with its white
// ink, from the pill's top left: the arrow's 8px glyph in rows 10-17,
// columns 14-20; the label's glyphs in rows 9-18 (the baseline at 19)
// from column 35; the close's times glyph in rows 12-17, its last column
// 16px in from the pill's right edge (a 38px button). The pill is 178.72px
// wide (8 + 20 + 4 + the label's 100.72 + 8 + the close's 38), the same
// with either digit (Lato's are tabular).

import (
	"image"
	"image/color"
	"testing"

	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// thrInkRun is a run of inked columns x0-x1 and the rows y0-y1 it inks.
type thrInkRun struct{ x0, x1, y0, y1 int }

// thrPillInk is the first pill (#1264a3) drawn in rows from..to of img,
// and its white ink's column runs (a gap over 4px starts a new run), all
// relative to the pill's top left. The pill's rounded ends (14px) are
// left out.
func thrPillInk(t *testing.T, img *image.RGBA, from, to int) (pill image.Rectangle, runs []thrInkRun) {
	t.Helper()
	near := func(c color.RGBA, r, g, b int) bool {
		return iabs(int(c.R)-r) <= 24 && iabs(int(c.G)-g) <= 24 && iabs(int(c.B)-b) <= 24
	}
	blue := func(x, y int) bool { return near(img.RGBAAt(x, y), 0x12, 0x64, 0xa3) }
	w := img.Bounds().Dx()
	y0, y1 := -1, -1
	for y := from; y < to; y++ {
		n := 0
		for x := 0; x < w; x++ {
			if blue(x, y) {
				n++
			}
		}
		if n > 40 {
			if y0 < 0 {
				y0 = y
			}
			y1 = y
		} else if y0 >= 0 {
			break
		}
	}
	if y0 < 0 {
		t.Fatalf("no pill drawn in rows %d-%d", from, to)
	}
	mid, x0, x1 := (y0+y1)/2, -1, -1
	for x := 0; x < w; x++ {
		if blue(x, mid) {
			if x0 < 0 {
				x0 = x
			}
			x1 = x
		}
	}
	pill = image.Rect(x0, y0, x1+1, y1+1)
	ink := func(x, y int) bool {
		c := img.RGBAAt(x, y)
		return min(c.R, c.G, c.B) > 150
	}
	var cur *thrInkRun
	for x := x0 + 14; x <= x1-14; x++ {
		top, bot := -1, -1
		for y := y0; y <= y1; y++ {
			if ink(x, y) {
				if top < 0 {
					top = y - y0
				}
				bot = y - y0
			}
		}
		if top < 0 {
			continue
		}
		if cur == nil || x-x0-cur.x1 > 4 {
			runs = append(runs, thrInkRun{x - x0, x - x0, top, bot})
			cur = &runs[len(runs)-1]
			continue
		}
		cur.x1, cur.y0, cur.y1 = x-x0, min(cur.y0, top), max(cur.y1, bot)
	}
	return pill, runs
}

func TestThreadsBannerPillDrawnLikeSlack(t *testing.T) {
	_, v, e, _ := thrBannerEnv(t, false, nil)
	e.frame()
	if len(v.ban.above) == 0 || len(v.ban.below) == 0 {
		t.Fatalf("banners above %v, below %v: want both", v.ban.above, v.ban.below)
	}
	img := pixeltest.Render(t, e.size.X, e.size.Y, &e.ops)
	near := func(got, want, tol int) bool { return iabs(got-want) <= tol }
	for _, tc := range []struct {
		name     string
		from, to int
	}{
		{"the top pill", 49, 120},
		{"the bottom pill", 180, e.size.Y},
	} {
		pill, runs := thrPillInk(t, img, tc.from, tc.to)
		t.Logf("%s: pill %v (%dx%d), ink runs %+v", tc.name, pill, pill.Dx(), pill.Dy(), runs)
		if pill.Dy() != 28 {
			t.Errorf("%s is %dpx high, want 28", tc.name, pill.Dy())
		}
		if !near(pill.Dx(), 179, 1) {
			t.Errorf("%s is %dpx wide; Chrome lays it out 178.72px wide (the arrow's 20px box, the close's 38px)", tc.name, pill.Dx())
		}
		if len(runs) < 3 {
			t.Errorf("%s draws %d ink runs (%+v), want the arrow, the label and the close", tc.name, len(runs), runs)
			continue
		}
		arrow, closeX, label := runs[0], runs[len(runs)-1], runs[1:len(runs)-1]
		if !near(arrow.y0, 10, 1) || !near(arrow.y1, 17, 1) || !near(arrow.x0, 14, 2) || !near(arrow.x1, 20, 2) {
			t.Errorf("%s's arrow inks rows %d-%d, columns %d-%d; Chrome draws the icon font's glyph at rows 10-17, columns 14-20", tc.name, arrow.y0, arrow.y1, arrow.x0, arrow.x1)
		}
		ly0, ly1 := label[0].y0, label[0].y1
		for _, r := range label {
			ly0, ly1 = min(ly0, r.y0), max(ly1, r.y1)
		}
		if !near(ly0, 9, 1) || !near(ly1, 18, 1) || !near(label[0].x0, 35, 2) {
			t.Errorf("%s's label inks rows %d-%d from column %d; Chrome draws it at rows 9-18 (its baseline at 19) from column 35", tc.name, ly0, ly1, label[0].x0)
		}
		if gap := pill.Dx() - 1 - closeX.x1; !near(closeX.y0, 12, 1) || !near(closeX.y1, 17, 1) || !near(gap, 16, 1) {
			t.Errorf("%s's close inks rows %d-%d, %dpx in from the right; Chrome draws it at rows 12-17, 16px in", tc.name, closeX.y0, closeX.y1, gap)
		}
	}
}
