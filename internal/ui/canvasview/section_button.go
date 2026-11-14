package canvasview

// The canvas header's FileSectionDropdownButton (boot-shared ofyN, CDN
// build fa938a086569): with custom_file_sections on, CanvasViewHeaderDefault
// (ioWZ) draws it before the title (margin_right_50: 8px) in the canvas's
// header, not in a channel tab (useIsViewInAChannelTab: the floating
// actions instead). Its face follows the file's collection: the filled
// star on sunflower (p-view_header_star_action_button_filled) in the
// starred one, the collection's emoji, the "section" icon, or "star" in
// none; the tooltip is "Move file" (bottom-right) and a click opens
// FileSectionMoveToMenu (Actions.SectionMenu).

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Section button faces (views.SectionButtonLook's values).
const (
	SectionStar = iota
	SectionStarred
	SectionEmoji
	SectionIcon
)

// The starred face's colours: plt-sunflower-0 / -5 (hover) under
// plt-sunflower-70, in both modes.
var (
	sunflower0  = color.NRGBA{R: 255, G: 250, B: 224, A: 255}
	sunflower5  = color.NRGBA{R: 255, G: 244, B: 184, A: 255}
	sunflower70 = color.NRGBA{R: 170, G: 128, B: 0, A: 255}
)

// sectionButtonShown reports whether the header draws the button.
func (v *View) sectionButtonShown() bool {
	if v.mode == Tab || v.act.SectionButton == nil {
		return false
	}
	_, _, ok := v.act.SectionButton()
	return ok
}

// layoutSectionButton draws the 28px button at p (view coordinates).
func (v *View) layoutSectionButton(gtx layout.Context, p image.Point) {
	look, emojiURL, ok := v.act.SectionButton()
	if !ok {
		return
	}
	th := v.h.Theme()
	c := &th.C
	clk := v.btn("bar.section")
	if clk.Clicked(gtx) && v.act.SectionMenu != nil { // before Layout, which drains a Clickable's events
		v.act.SectionMenu(sectionAnchor(v.h, clk, gtx.Dp(28)))
	}
	sz := gtx.Dp(28)
	at(gtx, p, image.Pt(sz, sz), func(gtx layout.Context) layout.Dimensions {
		d := clk.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			r := image.Rect(0, 0, sz, sz)
			hov := clk.Hovered()
			switch look {
			case SectionStarred:
				// p-view_header_star_action_button: margin-top 1px
				bg := sunflower0
				if hov {
					bg = sunflower5
				}
				rr := r.Add(image.Pt(0, gtx.Dp(1)))
				theme.RRect(gtx, rr, gtx.Dp(8), bg)
				at(gtx, image.Pt((sz-gtx.Dp(20))/2, gtx.Dp(1)+(sz-gtx.Dp(20))/2), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get("star-filled")(gtx, 20, sunflower70)
				})
			default:
				// c-icon_button--default, size small: content-sec, on
				// hover content-pry over base-ter; radius 8
				fg := c.FgMuted
				if hov {
					theme.RRect(gtx, r, gtx.Dp(8), c.BgHover)
					fg = c.Fg
				}
				isz := gtx.Dp(16) // calc(.5 * 28px + 2px)
				ip := image.Pt((sz-isz)/2, (sz-isz)/2)
				switch look {
				case SectionEmoji:
					if img, ok := v.h.Images().Get(emojiURL, isz*2); ok && emojiURL != "" {
						at(gtx, ip, image.Pt(isz, isz), func(gtx layout.Context) layout.Dimensions {
							return theme.Image(gtx, img, image.Pt(isz, isz), 0)
						})
					}
				case SectionIcon:
					at(gtx, ip, image.Pt(isz, isz), func(gtx layout.Context) layout.Dimensions { return icons.Get("section")(gtx, 16, fg) })
				default:
					at(gtx, ip, image.Pt(isz, isz), func(gtx layout.Context) layout.Dimensions { return icons.Get("star")(gtx, 16, fg) })
				}
			}
			return layout.Dimensions{Size: r.Max}
		})
		popups.Tooltip(gtx, th, &v.sectionTip, clk.Hovered(), d.Size, popups.BelowRight, i18n.T("files", "Move file"), "")
		return d
	})
}

// sectionAnchor is the clicked button's rectangle in window coordinates:
// the pointer's window position less the press's position in the button.
func sectionAnchor(h Host, clk *widget.Clickable, sz int) image.Rectangle {
	pp, ok := h.(interface{ PointerPos() image.Point })
	if !ok {
		return image.Rectangle{}
	}
	p := pp.PointerPos()
	if hist := clk.History(); len(hist) > 0 {
		at := hist[len(hist)-1].Position
		min := p.Sub(at)
		return image.Rectangle{Min: min, Max: min.Add(image.Pt(sz, sz))}
	}
	return image.Rectangle{Min: p, Max: p}
}
