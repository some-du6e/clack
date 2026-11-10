package popups

// Slack's EmojiSkinTonePicker (gantry-v2-async-client-boot-shared Zu56,
// CDN build 4d0c78085232), the footer's skin tone control:
//
//   - The toggle (.p-emoji_picker_skintone__toggle_btn) shows the hand in
//     the active tone (getCurrentTone) and "Skin Tone" only while
//     preferred_skin_tone is unset (maybeRenderSkinToneLabel). Under
//     emoji_skin_tone_picker_improvement it has the tooltip "Select default
//     skin tone".
//   - Pressing it opens a Popover at "top-right" (L1PI: the card's bottom
//     on the toggle's top, right edges aligned) holding the radio group:
//     .p-emoji_picker_skintone__radiogroup (primary background, 1px
//     base-ter border, radius 10, padding 4 8; --improved padding 4).
//     Without the experiment it starts with the label "Choose your default
//     skin tone" (.68rem bold, 4px under it). The six hands
//     (getChoices: the order of eG, or with a tone set the other five and
//     then the active one last) are 22px emoji; --improved cells are 36×32
//     (radius 4, 1px apart), tinted on hover by data-color-index (the
//     tone − 1) with the list's colours at .7.
//   - Opening focuses the active tone's radio (focusRadioGroup) and, from
//     the mouse, hovers it. A press on a cell picks it
//     (onSkinToneChanged: setUserPrefByApi preferred_skin_tone) and closes;
//     Escape closes; Tab stays. The arrow keys move between cells (native
//     radios without the experiment; moveFromSelectedSkinToneOption with
//     it, wrapping, from the selected one), and Enter or Space picks the
//     focused cell, or with the experiment the selected one. Without the
//     experiment the focused cell's emoji has the focus ring; with it the
//     cell has it, from the keyboard only.

import (
	"image"
	"strings"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// epToneUI is the skin tone control's state (EmojiSkinTonePicker's).
type epToneUI struct {
	open    bool
	focused int  // focusedSkinToneOption (0: none)
	hovered int  // hoveredSkinToneOption
	sel     int  // selectedSkinToneOption
	kbd     bool // interactionMode "keyboard" (else mouse)
	tip     TipState
	toggle  image.Rectangle // card coordinates, last frame
	tag     int             // the radio group's key focus
	scrim   int             // outside presses close the popover
	cells   [6]int          // pointer tags, by position
}

// epActiveTone is activeSkinToneId: preferred_skin_tone (or the tone just
// chosen while its users.prefs.set is in flight), 0 while it is unset.
func epActiveTone(h Host) int {
	epToneMu.Lock()
	t := epToneSet
	epToneMu.Unlock()
	if t >= 1 && t <= 6 {
		return t
	}
	if h == nil || h.Core() == nil {
		return 0
	}
	s := h.Core().Prefs().SkinTone
	if strings.Trim(s, ": ") == "" {
		return 0
	}
	return epParseTone(s)
}

// epToneChoices is getChoices: the tones in order, or with one active the
// other five and then the active one.
func epToneChoices(active int) [6]int {
	out := [6]int{1, 2, 3, 4, 5, 6}
	if active < 1 || active > 6 {
		return out
	}
	i := 0
	for t := 1; t <= 6; t++ {
		if t != active {
			out[i] = t
			i++
		}
	}
	out[5] = active
	return out
}

// toneImproved reports emoji_skin_tone_picker_improvement "on".
func (p *EmojiPicker) toneImproved() bool {
	c := p.h.Core()
	return c != nil && c.Experiment("emoji_skin_tone_picker_improvement") == "on"
}

// openTone is onPopoverOpen after focusRadioGroup, from the mouse.
func (p *EmojiPicker) openTone() {
	act := epActiveTone(p.h)
	p.toneUI = epToneUI{open: true, focused: act, hovered: act, sel: act, toggle: p.toneUI.toggle}
}

// closeTone closes the popover (popoverOnClose / onCancel).
func (p *EmojiPicker) closeTone() {
	p.toneUI.open = false
	p.focused = false // the search input takes the focus back
}

// pickTone is onSkinToneChanged, then the popover closes.
func (p *EmojiPicker) pickTone(t int) {
	if t < 1 || t > 6 {
		return
	}
	p.tone = t
	p.prevID = -1
	epSetTone(p.h, t)
	p.closeTone()
}

// toneKeys handles the open radio group's keys.
func (p *EmojiPicker) toneKeys(gtx layout.Context) {
	u := &p.toneUI
	improved := p.toneImproved()
	choices := epToneChoices(epActiveTone(p.h))
	for u.open {
		e, ok := gtx.Event(
			key.Filter{Focus: &u.tag, Name: key.NameEscape},
			key.Filter{Focus: &u.tag, Name: key.NameTab, Optional: key.ModShift},
			key.Filter{Focus: &u.tag, Name: key.NameUpArrow},
			key.Filter{Focus: &u.tag, Name: key.NameDownArrow},
			key.Filter{Focus: &u.tag, Name: key.NameLeftArrow},
			key.Filter{Focus: &u.tag, Name: key.NameRightArrow},
			key.Filter{Focus: &u.tag, Name: key.NameReturn},
			key.Filter{Focus: &u.tag, Name: key.NameEnter},
			key.Filter{Focus: &u.tag, Name: key.NameSpace},
		)
		if !ok {
			return
		}
		ke, ok := e.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		switch ke.Name {
		case key.NameEscape:
			p.closeTone()
		case key.NameTab: // preventDefault
		case key.NameReturn, key.NameEnter, key.NameSpace:
			t := u.focused
			if improved {
				t = u.sel
			}
			if t != 0 {
				p.pickTone(t)
			} else {
				p.closeTone()
			}
		default:
			dir := 1
			if ke.Name == key.NameUpArrow || ke.Name == key.NameLeftArrow {
				dir = -1
			}
			from := u.focused
			if improved {
				from = u.sel
			}
			i := -1
			for k, t := range choices {
				if t == from {
					i = k
				}
			}
			if i < 0 {
				continue
			}
			t := choices[(i+dir+6)%6]
			u.focused, u.sel, u.kbd = t, t, true
		}
	}
}

// layoutTone draws the toggle at the footer's right (footer coordinates,
// w×h) and returns the width it takes; the popover is drawn by
// layoutTonePopover after the footer.
func (p *EmojiPicker) layoutTone(gtx layout.Context, th *theme.Theme, cl *Colors, w, h int) int {
	if p.toneBtn.Clicked(gtx) {
		if p.toneUI.open {
			p.closeTone()
		} else {
			p.openTone()
		}
	}
	hand, okHand := p.handID()
	act := epActiveTone(p.h)
	// .p-emoji_picker_skintone (padding 3px; bottom 14.5px, right 16px) >
	// toggle_btn (1px clear border, padding 2px 3px): the 22px hand, then
	// the label (14.4/700, margin 0 3px) while no tone is set
	const label = "Skin Tone"
	bh, ig, in, gap := dp(gtx, 28), dp(gtx, 22), dp(gtx, 4), dp(gtx, 3)
	bw := in + ig + in
	lw := 0
	if act == 0 {
		lw = textW(gtx, th, 14.4, theme.Bold, label)
		bw = in + ig + gap + lw + gap + in
	}
	x := w - dp(gtx, 19) - bw
	y := h - dp(gtx, 14.5) - bh
	at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		d := clickBox(gtx, &p.toneBtn, nil, func(gtx layout.Context) layout.Dimensions {
			if okHand {
				at(gtx, image.Pt(in, (bh-ig)/2), func(gtx layout.Context) layout.Dimensions {
					return p.drawEmoji(gtx, hand, p.tone, epImgSz)
				})
			}
			if act == 0 {
				at(gtx, image.Pt(in+ig+gap, 0), func(gtx layout.Context) layout.Dimensions {
					return vcenter(loose(gtx, image.Pt(lw+1, bh)), bh, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 14.4, theme.Bold, cl.Fg2, label)
					})
				})
			}
			return layout.Dimensions{Size: image.Pt(bw, bh)}
		})
		if p.toneImproved() {
			Tooltip(gtx, th, &p.toneUI.tip, p.toneBtn.Hovered() && !p.toneUI.open, d.Size, Above, "Select default skin tone", "")
		}
		return d
	})
	p.toneUI.toggle = image.Rect(x, y, x+bw, y+bh)
	return bw + dp(gtx, 19)
}

// layoutTonePopover draws the open radio group (card coordinates; foot is
// the footer's top) over the card and handles its input.
func (p *EmojiPicker) layoutTonePopover(gtx layout.Context, th *theme.Theme, cl *Colors, card image.Point, foot int) {
	u := &p.toneUI
	if !u.open {
		return
	}
	improved := p.toneImproved()
	act := epActiveTone(p.h)
	choices := epToneChoices(act)
	hand, okHand := p.handID()

	// outside presses close it (the popover's overlay)
	area := clip.Rect{Max: card}.Push(gtx.Ops)
	event.Op(gtx.Ops, &u.scrim)
	area.Pop()
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &u.scrim, Kinds: pointer.Press})
		if !ok {
			break
		}
		if pe, ok := e.(pointer.Event); ok && pe.Kind == pointer.Press {
			p.closeTone()
		}
	}
	if !u.open {
		return
	}

	// the card: top-right of the toggle
	pad := image.Pt(dp(gtx, 8), dp(gtx, 4))
	cw, ch, cgap := dp(gtx, 22), dp(gtx, 22), 0
	labelH := 0
	const lbl = "Choose your default skin tone"
	if improved {
		pad = image.Pt(dp(gtx, 4), dp(gtx, 4))
		cw, ch, cgap = dp(gtx, 36), dp(gtx, 32), dp(gtx, 1)
	} else {
		labelH = dp(gtx, 16) + dp(gtx, 4)
	}
	rowW := 6*cw + 6*cgap
	innerW := rowW
	if !improved {
		innerW = maxInt(rowW, textW(gtx, th, 10.88, theme.Bold, lbl))
	}
	b := maxInt(1, dp(gtx, 1))
	bw := innerW + 2*pad.X + 2*b
	bh := labelH + ch + 2*pad.Y + 2*b
	t := u.toggle.Add(image.Pt(0, foot))
	r := image.Rect(t.Max.X-bw, t.Min.Y-bh, t.Max.X, t.Min.Y)
	if r.Min.X < 0 {
		r = r.Add(image.Pt(-r.Min.X, 0))
	}
	rad := dp(gtx, 10)
	rrect(gtx, r, rad, th.C.Skeleton)
	rrect(gtx, r.Inset(b), rad-b, cl.EmojiBg)
	// the popover takes its own presses
	pa := clip.Rect(r).Push(gtx.Ops)
	event.Op(gtx.Ops, &u.tag)
	pa.Pop()
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &u.tag, Kinds: pointer.Press})
		if !ok {
			break
		}
		_ = e
	}
	if !gtx.Focused(&u.tag) {
		gtx.Execute(key.FocusCmd{Tag: &u.tag})
	}
	p.toneKeys(gtx)
	if !u.open {
		return
	}
	x0, y0 := r.Min.X+b+pad.X, r.Min.Y+b+pad.Y
	if !improved {
		at(gtx, image.Pt(x0, y0), func(gtx layout.Context) layout.Dimensions {
			return vcenter(loose(gtx, image.Pt(innerW+1, dp(gtx, 16))), dp(gtx, 16), func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 10.88, theme.Bold, cl.Fg, lbl)
			})
		})
		y0 += labelH
	}
	for i, tone := range choices {
		c := image.Rect(x0+i*(cw+cgap), y0, x0+i*(cw+cgap)+cw, y0+ch)
		tag := &u.cells[i]
		ca := clip.Rect(c).Push(gtx.Ops)
		event.Op(gtx.Ops, tag)
		pointer.CursorPointer.Add(gtx.Ops)
		ca.Pop()
		for {
			e, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Press | pointer.Enter | pointer.Move})
			if !ok {
				break
			}
			pe, ok := e.(pointer.Event)
			if !ok {
				continue
			}
			switch pe.Kind {
			case pointer.Press:
				if pe.Buttons == pointer.ButtonPrimary && pe.Modifiers&key.ModCtrl == 0 { // onMouseDown: left, no ctrl
					p.pickTone(tone)
					return
				}
			case pointer.Enter, pointer.Move:
				if improved && (u.hovered != tone || u.kbd) { // onMouseEnter
					u.hovered, u.sel, u.kbd = tone, tone, false
				}
			}
		}
		ring := u.focused == tone && (!improved || u.kbd)
		if improved {
			if u.hovered == tone && !u.kbd {
				rrect(gtx, c, dp(gtx, 4), epCellTint(tone-1))
			}
			if ring {
				epToneRing(gtx, cl, c)
			}
		}
		e := image.Rect(c.Min.X+(cw-dp(gtx, epImgSz))/2, c.Min.Y+(ch-dp(gtx, epImgSz))/2, 0, 0)
		e.Max = e.Min.Add(image.Pt(dp(gtx, epImgSz), dp(gtx, epImgSz)))
		if !improved && ring {
			epToneRing(gtx, cl, e)
		}
		if okHand {
			at(gtx, e.Min, func(gtx layout.Context) layout.Dimensions {
				return p.drawEmoji(gtx, hand, tone, epImgSz)
			})
		}
	}
}

// epToneRing is Slack's focus ring: 1px focus colour and a 5px halo at .3,
// radius 8 (sk_input_styles-radius).
func epToneRing(gtx layout.Context, cl *Colors, r image.Rectangle) {
	o := r.Inset(-dp(gtx, 5))
	rad := dp(gtx, 8)
	stroke(gtx, o, rad+dp(gtx, 5), dp(gtx, 4), cl.FocusRing)
	stroke(gtx, r.Inset(-maxInt(1, dp(gtx, 1))), rad+1, maxInt(1, dp(gtx, 1)), cl.Focus)
}
