package ui

import (
	"image"

	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// prefsForm lays out one Preferences page top to bottom: Slack's section
// headings (700 18px/24), checkbox and radio rows (18px box, 15px label,
// 13px hint) and muted notes. Rows keep their own clickables on the modal
// (keyed), so a page is just a sequence of calls each frame.
type prefsForm struct {
	p   *prefsModal
	gtx layout.Context
	w   int
	y   int
}

func (p *prefsModal) form(gtx layout.Context) *prefsForm {
	return &prefsForm{p: p, gtx: gtx, w: gtx.Constraints.Max.X}
}

// dims is the page's size so far.
func (f *prefsForm) dims() layout.Dimensions {
	return layout.Dimensions{Size: image.Pt(f.w, f.y)}
}

// heading is a section title (Slack's h2: 700 18px/24, 30px stride).
func (f *prefsForm) heading(t string) {
	gtx, a := f.gtx, f.p.a
	at(gtx, image.Pt(0, f.y), image.Pt(f.w, gtx.Dp(24)), func(gtx layout.Context) layout.Dimensions {
		return a.th.Label(gtx, 18, theme.Bold, a.th.C.Fg, t)
	})
	f.y += gtx.Dp(30)
}

// gap is the space between sections.
func (f *prefsForm) gap() { f.y += f.gtx.Dp(14) }

// note is muted 13px text, wrapped to lines (indent: under a row's label).
func (f *prefsForm) note(text string, indent bool, lines int) {
	gtx, a := f.gtx, f.p.a
	x := 0
	if indent {
		x = gtx.Dp(28)
	}
	if lines < 1 {
		lines = 1
	}
	h := gtx.Dp(18) * lines
	at(gtx, image.Pt(x, f.y), image.Pt(f.w-x, h+gtx.Dp(2)), func(gtx layout.Context) layout.Dimensions {
		return a.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Color: a.th.C.FgMuted, MaxLines: lines}, text)
	})
	f.y += h + gtx.Dp(10)
}

// option is a checkbox (square) or radio (round) row; do runs on click.
func (f *prefsForm) option(key, label, hint string, on, radio bool, do func()) {
	f.optionAt(key, label, hint, on, radio, true, do)
}

// optionAt is option; enabled=false greys the row and ignores clicks
// (settings Clack shows the way Slack does but can't change yet).
func (f *prefsForm) optionAt(key, label, hint string, on, radio, enabled bool, do func()) {
	gtx, a := f.gtx, f.p.a
	c := &a.th.C
	b := f.p.btn(key)
	rowH := gtx.Dp(28)
	if hint != "" {
		rowH = gtx.Dp(44)
	}
	w := f.w
	at(gtx, image.Pt(0, f.y), image.Pt(w, rowH), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, b, nil, func(gtx layout.Context) layout.Dimensions {
			s := gtx.Dp(18)
			r := image.Rect(0, gtx.Dp(3), s, gtx.Dp(3)+s)
			rad := gtx.Dp(4)
			if radio {
				rad = s / 2
			}
			fill := c.Link
			fg := c.Fg
			if !enabled {
				fill = c.FgSubtle
				fg = c.FgSubtle
			}
			if on {
				theme.RRect(gtx, r, rad, fill)
				if radio {
					theme.Circle(gtx, r.Min.Add(image.Pt(s/2, s/2)), s*4/9, c.Bg)
				} else {
					at(gtx, r.Min, image.Pt(s, s), func(gtx layout.Context) layout.Dimensions {
						return icons.Get("check")(gtx, 18, c.SendText)
					})
				}
			} else {
				theme.RRect(gtx, r, rad, c.Bg)
				theme.Border(gtx, r, rad, gtx.Dp(1), c.BorderStrong)
			}
			at(gtx, image.Pt(gtx.Dp(28), 0), image.Pt(w-gtx.Dp(28), gtx.Dp(24)), func(gtx layout.Context) layout.Dimensions {
				return a.th.Label(gtx, 15, theme.Regular, fg, label)
			})
			if hint != "" {
				at(gtx, image.Pt(gtx.Dp(28), gtx.Dp(22)), image.Pt(w-gtx.Dp(28), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 13, theme.Regular, c.FgMuted, hint)
				})
			}
			return layout.Dimensions{Size: image.Pt(w, rowH)}
		})
	})
	if clicked(b) && enabled {
		do()
		a.Invalidate()
	}
	f.y += rowH + gtx.Dp(4)
}
