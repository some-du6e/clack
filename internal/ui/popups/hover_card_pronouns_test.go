package popups

// The hover card's text column, drawn on the GPU, against Slack's
// MemberProfileBaseEntity (client-boot-shared Se3V / BaseEntity 0gdK,
// build 2c0d02ca130f) and client-boot-styles:
//
//	.c-base_entity__text-contents   flex column: name, title, pronouns
//	                                margin -4px 0, centred beside the avatar
//	title (c-base_entity__subtext)  15px/22px, content-sec, wraps
//	.p-member_profile_base_entity__pronouns
//	                                13px, line-height 1.46668, weight 400,
//	                                rgba(var(--sk_foreground_max), .7),
//	                                its own line below the title
//
// Each pronoun run is compared with a reference run drawn in the same
// frame at Slack's size, weight and colour (the colour computed here from
// the CSS, not from the card's palette): same ink box, same ink, same
// darkest pixel, at the column's x on the line after the title.

import (
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// Karim's profile (#clack, 2026-11-15): display name "karim", a real name
// of one zero-width space, pronouns "*", a long title, no status.
const (
	hpKarimID    = "U0PRONOUN1"
	hpShortID    = "U0PRONOUN2"
	hpKarimTitle = "under EN DEE AY 😱😱😱 — msgs expire in 72hrs unless they end with //"
)

// hpInkBox is the bounding box of the pixels in r that are at least half
// fg over bg, and how many there are.
func hpInkBox(img *image.RGBA, r image.Rectangle, fg, bg color.NRGBA) (image.Rectangle, int) {
	var box image.Rectangle
	n := 0
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if pixeltest.Ink(img, image.Rect(x, y, x+1, y+1), fg, bg) == 0 {
				continue
			}
			px := image.Rect(x, y, x+1, y+1)
			if n == 0 {
				box = px
			} else {
				box = box.Union(px)
			}
			n++
		}
	}
	return box, n
}

// hpDarkest is the pixel in r farthest from bg.
func hpDarkest(img *image.RGBA, r image.Rectangle, bg color.NRGBA) color.RGBA {
	var best color.RGBA
	bd := -1
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c := img.RGBAAt(x, y)
			d := absInt(int(c.R)-int(bg.R)) + absInt(int(c.G)-int(bg.G)) + absInt(int(c.B)-int(bg.B))
			if d > bd {
				best, bd = c, d
			}
		}
	}
	return best
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func TestHoverCardPronounsLikeSlack(t *testing.T) {
	h := hcEnv(t)
	h.a.S.PutUsers([]slack.User{
		{ID: hpKarimID, TeamID: fakeslack.TeamID, Name: "karim", RealName: "​",
			Profile: slack.Profile{DisplayName: "karim", RealName: "​", Pronouns: "*", Title: hpKarimTitle}},
		{ID: hpShortID, TeamID: fakeslack.TeamID, Name: "sam",
			Profile: slack.Profile{DisplayName: "sam", Pronouns: "they/them"}},
	})
	sh := theme.NewShaper()
	const W, H = 420, 360
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		th := theme.New(dark)
		th.SetShapers(sh, sh)
		h.th = th
		cl := colorsOf(th)
		// Slack's colour, from the CSS: rgba(var(--sk_foreground_max), .7)
		css := color.NRGBA{R: 29, G: 28, B: 29, A: 179}
		if dark {
			css = color.NRGBA{R: 232, G: 232, B: 232, A: 179}
		}
		want := theme.Over(css, cl.Bg)

		for _, c := range []struct {
			id, pronouns string
			title        bool
		}{
			{hpKarimID, "*", true},
			{hpShortID, "they/them", false},
		} {
			var drawn []string
			theme.DrawHook = func(s string) { drawn = append(drawn, s) }
			p := NewHoverProfileCard(h, image.Rect(20, 0, 80, 10), c.id)
			now := time.Date(2026, 11, 8, 15, 42, 0, 0, time.UTC)
			var ops op.Ops
			refAt := image.Pt(20, H-40)
			var titleH int
			for i := 0; i < 3; i++ {
				ops.Reset()
				drawn = drawn[:0]
				gtx := layout.Context{Ops: &ops, Now: now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(W, H))}
				paint.FillShape(&ops, cl.Bg, clip.Rect{Max: image.Pt(W, H)}.Op())
				p.Layout(gtx)
				// the reference run: Slack's 13px, weight 400, line-height 1.46668
				func() {
					defer op.Offset(refAt).Push(gtx.Ops).Pop()
					g := gtx
					g.Constraints = layout.Constraints{Max: image.Pt(300, 40)}
					para(g, th, 13, 13*1.46668, theme.Regular, want, c.pronouns, 1)
				}()
				if c.title {
					m := op.Record(gtx.Ops)
					g := gtx
					g.Constraints = layout.Constraints{Max: image.Pt(340-16-72-12-16, 1<<20)}
					titleH = para(g, th, 15, 22, theme.Regular, cl.Fg2, hpKarimTitle, 0).Size.Y
					m.Stop()
				}
			}
			theme.DrawHook = nil
			r := p.Rect()
			if r.Empty() {
				t.Fatalf("%s %s: no card", mode, c.id)
			}
			for _, s := range drawn {
				if strings.Trim(s, "​") == "" {
					t.Errorf("%s %s: drew a blank run %q (the zero-width real name)", mode, c.id, s)
				}
			}
			img := pixeltest.Render(t, W, H, &ops)

			colX := r.Min.X + 16 + 72 + 12
			colR := r.Max.X - 16
			var pronTop int
			if c.title {
				if titleH < 44 {
					t.Fatalf("%s: the title is %dpx, want it to wrap (≥ 2 lines)", mode, titleH)
				}
				// taller than the avatar: the column starts 4px above it
				pronTop = r.Min.Y + 16 - 4 + 22 + titleH
				// the title's lines sit between the name and the pronouns
				if _, n := hpInkBox(img, image.Rect(colX, r.Min.Y+12+22, colR, pronTop), cl.Fg2, cl.Bg); n < 100 {
					t.Errorf("%s: %d title pixels below the name, want the wrapped title there", mode, n)
				}
			} else {
				// name 22 + pronouns 19.07 - 8 (margins) = 33.07 < 72:
				// centred, (72-33.07)/2 = 19.47 below the avatar's top
				pronTop = r.Min.Y + 16 + 19 - 4 + 22
			}
			pronBand := image.Rect(colX, pronTop, colR, pronTop+19)
			got, gn := hpInkBox(img, pronBand, want, cl.Bg)
			ref, rn := hpInkBox(img, image.Rect(refAt.X, refAt.Y, refAt.X+300, refAt.Y+20), want, cl.Bg)
			if rn == 0 {
				t.Fatalf("%s %q: the reference run drew nothing", mode, c.pronouns)
			}
			if gn == 0 {
				t.Errorf("%s %q: no pronouns on the line after the %s (y %d..%d)", mode, c.pronouns,
					map[bool]string{true: "title", false: "name"}[c.title], pronBand.Min.Y, pronBand.Max.Y)
				continue
			}
			// size: the same ink box as Slack's 13px run
			if got.Size() != ref.Size() {
				t.Errorf("%s %q: pronoun ink %v, want Slack's 13px run %v", mode, c.pronouns, got.Size(), ref.Size())
			}
			// placement: at the column's left, on its own 19px line
			off, roff := got.Min.Sub(image.Pt(colX, pronTop)), ref.Min.Sub(refAt)
			if absInt(off.X-roff.X) > 1 || absInt(off.Y-roff.Y) > 1 {
				t.Errorf("%s %q: pronoun ink at %v from the line's corner, want %v", mode, c.pronouns, off, roff)
			}
			// weight: the same ink as the 400 run
			if d := absInt(gn - rn); d*10 > rn {
				t.Errorf("%s %q: %d ink pixels, Slack's weight-400 run has %d", mode, c.pronouns, gn, rn)
			}
			// colour: the darkest pixel matches the reference's
			if g, w := hpDarkest(img, got, cl.Bg), hpDarkest(img, ref, cl.Bg); !within(g, color.NRGBA{R: w.R, G: w.G, B: w.B, A: 0xff}, 4) {
				t.Errorf("%s %q: darkest pronoun pixel %v, want %v (rgba(var(--sk_foreground_max),.7) = %v)", mode, c.pronouns, g, w, want)
			}
		}
	}
}
