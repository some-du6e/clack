package popups

// Drawing helpers exported for internal/ui/slackc/kit, the api.elements
// look-alikes (kit imports popups; popups never imports kit). They draw
// with popups' measured Slack look, at the origin, and return their size.
// Menus (NewMenu, NewMenuOpts with MenuOpts.Raw), Popover (NewPopover with
// PopoverOpts.Anchor), Modal, Button and Tooltip were already exported.

import (
	"image"
	"image/color"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// ColorsOf returns popups' palette (Slack 2026 tokens) for th.
func ColorsOf(th *theme.Theme) *Colors { return colorsOf(th) }

// ModalClass is Slack's modal element (ReactModal__Content c-sk-modal,
// role dialog), which a theme's [role=dialog] / .c-sk-modal rules style.
var ModalClass = modalClass

// StyledCard lays out a card at pos (sz its content size, radius px) with
// its native look (fill, 1px ring, shadow around a rect) under the CSS box
// st, its Slack element, gets from a theme, and body at the content
// origin (see styledCard). With no rule on st it is the native card.
func StyledCard(gtx layout.Context, st slackc.Styled, fill, ring color.NRGBA, shadow func(gtx layout.Context, r image.Rectangle, radius int), pos, sz image.Point, radius int, body func(gtx layout.Context)) layout.Dimensions {
	return styledCardLook(gtx, st, cardLook{fill: fill, ring: ring, shadow: shadow}, pos, sz, radius, body)
}

// Field draws Slack's single-line input (.c-input_text: radius 8,
// InputBorder, 4dp focus ring) around ed, full constraint width, h dp tall,
// size sp text, with an optional leading icon and placeholder.
func Field(gtx layout.Context, th *theme.Theme, ed *widget.Editor, placeholder, leadIcon string, h, size float32) layout.Dimensions {
	return field(gtx, th, ed, placeholder, leadIcon, h, size)
}

var (
	checkboxClass = slackc.Class("c-input_checkbox", "type", "checkbox").Tag("input")
	toggleClass   = slackc.Class("c-toggle_switch", "role", "switch").Tag("button").Mods("c-toggle_switch--on")
	pillClass     = slackc.Class("c-filter-pill").Tag("button").Mods("c-filter-pill--active")
	pillInnerCls  = slackc.Class("c-filter-pill__content").Tag("span")
	tagClass      = slackc.Class("c-tag").Tag("span").Mods("c-tag--informative", "c-tag--micro")
)

// Checkbox draws Slack's 16dp checkbox (.c-input_checkbox): a filled
// Highlight box with a check when on, a dash when indeterminate, dimmed
// when disabled. Clicks are the caller's.
func Checkbox(gtx layout.Context, th *theme.Theme, on, indeterminate, disabled bool) layout.Dimensions {
	var state css.State
	if on {
		state |= css.StateChecked
	}
	if disabled {
		state |= css.StateDisabled
	}
	st := checkboxClass.Enter(gtx, state, 0)
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{}
	}
	return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
		cl := colorsOf(th)
		sz := dp(gtx, 16)
		r := image.Rect(0, 0, sz, sz)
		rad := dp(gtx, 4)
		fg := cl.HighText
		switch {
		case on || indeterminate:
			bg := cl.Highlight
			if disabled {
				bg = cl.Subtle
			}
			rrect(gtx, r, rad, st.Background(bg))
			if indeterminate && !on {
				h := maxInt(2, dp(gtx, 2))
				fill(gtx, image.Rect(dp(gtx, 4), (sz-h)/2, sz-dp(gtx, 4), (sz-h)/2+h), fg)
			} else {
				is := dp(gtx, 12)
				at(gtx, image.Pt((sz-is)/2, (sz-is)/2), func(gtx layout.Context) layout.Dimensions {
					return icon(gtx, "check", 12, fg)
				})
			}
		default:
			border := theme.WithAlpha(cl.Fg, 0x80)
			if disabled {
				border = cl.Subtle
			}
			rrect(gtx, r, rad, border)
			bg := cl.Bg
			if disabled {
				bg = cl.Disabled
			}
			rrect(gtx, r.Inset(border1(gtx)), rad-border1(gtx), st.Background(bg))
		}
		return layout.Dimensions{Size: r.Max}
	})
}

// Toggle draws Slack's small switch (28×16dp track, Presence green when
// on, white knob).
func Toggle(gtx layout.Context, th *theme.Theme, on, disabled bool) layout.Dimensions {
	var state css.State
	var mods uint64
	if on {
		state, mods = css.StateChecked, 1
	}
	if disabled {
		state |= css.StateDisabled
	}
	st := toggleClass.Enter(gtx, state, mods)
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{}
	}
	return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
		cl := colorsOf(th)
		w, h := dp(gtx, 28), dp(gtx, 16)
		track := theme.WithAlpha(cl.Fg, 0x40)
		if on {
			track = cl.Presence
		}
		if disabled {
			track = theme.WithAlpha(track, 0x60)
		}
		rrect(gtx, image.Rect(0, 0, w, h), h/2, st.Background(track))
		k := h - dp(gtx, 4)
		x := dp(gtx, 2)
		if on {
			x = w - k - dp(gtx, 2)
		}
		theme.Circle(gtx, image.Pt(x+k/2, h/2), k, rgb(0xffffff))
		return layout.Dimensions{Size: image.Pt(w, h)}
	})
}

// Pill draws Slack's 28dp filter pill (.c-filter-pill, search filters):
// optional 16dp icon, 13/700 label, optional chevron; active pills are
// link-blue. c may be nil (drawn without input).
func Pill(gtx layout.Context, th *theme.Theme, c *widget.Clickable, iconName, label string, chevron, active bool) layout.Dimensions {
	cl := colorsOf(th)
	h := dp(gtx, 28)
	tw := textW(gtx, th, 13, theme.Bold, label)
	w := dp(gtx, 10) + tw
	if iconName != "" {
		w += dp(gtx, 16) + dp(gtx, 6)
	}
	if chevron {
		w += dp(gtx, 6) + dp(gtx, 12) + dp(gtx, 8)
	} else {
		w += dp(gtx, 10)
	}
	hovered := c != nil && c.Hovered()
	var state css.State
	var mods uint64
	if hovered {
		state = css.StateHover
	}
	if active {
		mods = 1
	}
	st := pillClass.Enter(gtx, state, mods)
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{}
	}
	draw := func(gtx layout.Context) layout.Dimensions {
		hovered := c != nil && c.Hovered()
		r := image.Rect(0, 0, w, h)
		rad := dp(gtx, 8)
		fg, border, bg := cl.Fg, cl.Divider, cl.Bg
		if active {
			fg, border, bg = cl.Link, cl.Link, theme.WithAlpha(cl.Link, 0x1a)
			if hovered {
				bg = theme.WithAlpha(cl.Link, 0x2a)
			}
		} else if hovered {
			bg = cl.BgHover
		}
		fg = st.Color(fg)
		rrect(gtx, r, rad, border)
		rrect(gtx, r.Inset(border1(gtx)), rad-border1(gtx), cl.Bg)
		if bg = st.Background(bg); bg != cl.Bg {
			rrect(gtx, r.Inset(border1(gtx)), rad-border1(gtx), bg)
		}
		in := pillInnerCls.Enter(gtx, 0, 0)
		in.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
			lx := dp(gtx, 10)
			if iconName == srSlackMark { // the search source pill's logo image (appIconSmall: 15px)
				at(gtx, image.Pt(lx, (h-dp(gtx, 15))/2), func(gtx layout.Context) layout.Dimensions {
					return icons.Art(gtx, srSlackMark, dp(gtx, 15))
				})
				lx += dp(gtx, 16) + dp(gtx, 6)
			} else if iconName != "" {
				at(gtx, image.Pt(lx, (h-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
					return icon(gtx, iconName, 16, fg)
				})
				lx += dp(gtx, 16) + dp(gtx, 6)
			}
			at(gtx, image.Pt(lx, 0), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(tw+dp(gtx, 2), h)}
				return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Bold, fg, label)
				})
			})
			if chevron {
				at(gtx, image.Pt(lx+tw+dp(gtx, 6), (h-dp(gtx, 12))/2), func(gtx layout.Context) layout.Dimensions {
					return icon(gtx, "chevron-down", 12, fg)
				})
			}
			return layout.Dimensions{Size: r.Max}
		})
		in.Leave()
		if c != nil {
			pointerCursor(gtx, r.Max)
		}
		return layout.Dimensions{Size: r.Max}
	}
	return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
		if c == nil {
			return draw(gtx)
		}
		gtx.Constraints = layout.Exact(image.Pt(w, h))
		return c.Layout(gtx, draw)
	})
}

// Tag draws Slack's small label tag (.c-tag): 20dp (16dp micro) rounded
// tile, 12/700 text (11 micro); informative tags are link-blue, others
// grey.
func Tag(gtx layout.Context, th *theme.Theme, label string, informative, micro bool) layout.Dimensions {
	var mods uint64
	if informative {
		mods |= 1
	}
	if micro {
		mods |= 2
	}
	st := tagClass.Enter(gtx, 0, mods)
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{}
	}
	return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
		cl := colorsOf(th)
		h, fs, px := float32(20), float32(12), float32(6)
		if micro {
			h, fs, px = 16, 11, 4
		}
		fg, bg := cl.Fg2, cl.Selected
		if informative {
			fg, bg = cl.Link, theme.WithAlpha(cl.Link, 0x1a)
		}
		fg, bg = st.Color(fg), st.Background(bg)
		hh := dp(gtx, h)
		tw := textW(gtx, th, fs, theme.Bold, label)
		w := tw + 2*dp(gtx, px)
		rrect(gtx, image.Rect(0, 0, w, hh), dp(gtx, 4), bg)
		at(gtx, image.Pt(dp(gtx, px), 0), func(gtx layout.Context) layout.Dimensions {
			return vcenter(exact(gtx, image.Pt(tw, hh)), hh, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min = image.Point{}
				return text(gtx, th, fs, theme.Bold, fg, label)
			})
		})
		return layout.Dimensions{Size: image.Pt(w, hh)}
	})
}

// NewDatePicker opens Slack's calendar dropdown (.c-date_picker__dropdown:
// month grid, « ‹ › » navigation, keyboard) right-aligned under anchor
// (window px). Days before earliest (zero: none) are disabled; onPick gets the
// chosen day at local midnight. Show it with h.ShowOverlay.
func NewDatePicker(h Host, anchor image.Rectangle, selected, earliest time.Time, onPick func(day time.Time)) Overlay {
	if selected.IsZero() {
		selected = time.Now()
	}
	return dtNewCalendar(h, anchor, selected, earliest, onPick)
}

// NewTimePicker opens Slack's time list (TimePicker's dropdown) under
// anchor: times every step minutes (5/10/15/30/60) from from minutes
// after midnight, value (minutes) selected and scrolled into view; onPick
// gets the chosen minutes. Show it with h.ShowOverlay.
func NewTimePicker(h Host, anchor image.Rectangle, value, from, step int, onPick func(mins int)) Overlay {
	return dtNewTimeListStep(h, anchor, value, from, step, onPick)
}
