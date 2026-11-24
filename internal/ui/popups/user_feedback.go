package popups

// /feedback's dialog, as Slack's web client has it (gantry-v2-async-
// slash-commands-local tCkD feedbackCommand under slash_feedback_in_modal,
// VhHZ openUserFeedbackModal, build 9f450b8ad3db; on for Hack Club):
//
//   - UserFeedbackModal: "Share your feedback" over a column of
//     ModalContentSections 20px apart (modalContentSections: flex, gap 20,
//     padding-bottom 8): the error Alert after a failed submit; "What
//     brings you here?" over a BasicSelect ("Select an option": Share
//     product feedback, Get help or support).
//   - Share product feedback: "What area of the product are you sharing
//     feedback about?" (FormTextInput, "e.g., huddles, canvas, etc.", 50
//     characters), "What is your feedback?" (FormTextareaInput, 1,000,
//     with slash_feedback_modal_warning's hint "Note: Please don’t include
//     sensitive or proprietary information.") and the inline checkbox "I
//     understand that this feedback goes to our product team only, and
//     that I won’t receive a response from Slack support.". Submit stays
//     disabled until both are filled in (not only spaces), within their
//     limits (JavaScript lengths), and the box is ticked. It sends
//     feedback.submitUserFeedback {product_area, product_feedback, source
//     slash_feedback} (core.SubmitUserFeedback) and, done, shows
//     UserFeedbackConfirmationModal; a failure keeps the dialog with
//     "Something went wrong submitting your feedback. Please try again.",
//     a rate limit (429, ratelimited) with "Sorry, you’ve hit the rate
//     limit. You’ll be able to try again soon.".
//   - Get help or support: "We’re here to help" (bold), a break, "Find
//     answers to product questions or open a support case on the Slack
//     Help Center." (the link https://{team domain}/help), and Visit Help
//     Center in place of Submit (a ModalCloseButton with the link: it opens
//     it and closes the dialog).
//   - The inputs (FormTextInput and FormTextareaInput defaults: showError,
//     the deprecated Alert): their CharacterCount shows the characters
//     left while focused (13px content-sec, padding 8 12, at the top right;
//     content-imp past the limit), and the text keeps clear of it
//     (padding-right: the count's digits in ch plus 24px). Past the limit
//     the Alert "You can’t enter more than {n} characters." sits right
//     under the input (margin_bottom_100), in the textarea's place of its
//     hint.
//   - UserFeedbackConfirmationModal (centered, no close button): the
//     celebration (231x199) on theme-surf-sec with padding 12, "Thank you
//     for helping us make Slack better", "Your feedback goes to our
//     product teams and helps shape what we build next" and Close.

import (
	"context"
	"image"
	"image/color"
	"strconv"
	"strings"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// nsUserFeedback is VhHZ's namespace.
var nsUserFeedback = i18n.NS("user-feedback")

const (
	ufTitle     = "Share your feedback"
	ufAsk       = "What brings you here?"
	ufPick      = "Select an option"
	ufShare     = "Share product feedback"
	ufHelp      = "Get help or support"
	ufArea      = "What area of the product are you sharing feedback about?"
	ufAreaHint  = "e.g., huddles, canvas, etc."
	ufText      = "What is your feedback?"
	ufTextHint  = "How does this impact your current use of Slack or how could it potentially improve your use of Slack in the future?"
	ufWarn      = "Note: Please don’t include sensitive or proprietary information."
	ufAck       = "I understand that this feedback goes to our product team only, and that I won’t receive a response from Slack support."
	ufFailed    = "Something went wrong submitting your feedback. Please try again."
	ufLimited   = "Sorry, you’ve hit the rate limit. You’ll be able to try again soon."
	ufHelpText  = "<bold>We’re here to help \u2764\ufe0f </bold><br></br>Find answers to product questions or open a support case on the <helpCenter>Slack Help Center</helpCenter>."
	ufVisit     = "Visit Help Center"
	ufThanks    = "Thank you for helping us make Slack better"
	ufThanksTxt = "Your feedback goes to our product teams and helps shape what we build next"
	ufTooLong   = "You can’t enter more than {maxCharacterLimit, plural, =1 {# character} other {# characters}}."
	ufAreaMax   = 50
	ufTextMax   = 1000
)

// ufKinds are the select's options (value, label), in Slack's order.
var ufKinds = [...]struct{ value, label string }{
	{"share_product_feedback", ufShare},
	{"get_help_or_support", ufHelp},
}

// UserFeedbackModal is /feedback's dialog.
type UserFeedbackModal struct {
	h     Host
	modal *Modal

	kind    int // ufKinds index, -1 none
	selBtn  widget.Clickable
	selR    image.Rectangle // the select's window rectangle, last frame
	area    widget.Editor
	areaHit widget.Clickable
	text    widget.Editor
	textHit widget.Clickable
	ack     bool
	ackBtn  widget.Clickable
	helpBtn widget.Clickable // the Slack Help Center link
	busy    bool
	err     string // the error Alert
}

// NewUserFeedbackModal is openUserFeedbackModal: the dialog, shown.
func NewUserFeedbackModal(h Host) *UserFeedbackModal {
	m := &UserFeedbackModal{h: h, kind: -1}
	m.area.SingleLine = true
	m.area.Submit = true // Enter adds nothing (the input is in no form)
	submit := &ModalButton{Label: nsUserFeedback.T("Submit"), Kind: BtnPrimary,
		Enabled: func() bool { return m.invalid() == "" },
		Loading: func() bool { return m.busy },
		Hidden:  func() bool { return m.kind == 1 },
		Do:      func() bool { m.Submit(); return false }}
	visit := &ModalButton{Label: nsUserFeedback.T(ufVisit), Kind: BtnPrimary,
		Hidden: func() bool { return m.kind != 1 },
		Do:     func() bool { srOpenLink(m.h, m.helpURL()); return true }}
	m.modal = NewModal(h, ModalOpts{Title: nsUserFeedback.T(ufTitle), Name: "UserFeedbackModal", Body: m.layout,
		FocusStops: func() []event.Tag {
			switch m.kind {
			case 0:
				return []event.Tag{&m.selBtn, &m.area, &m.text, &m.ackBtn}
			case 1:
				return []event.Tag{&m.selBtn, &m.helpBtn}
			}
			return []event.Tag{&m.selBtn}
		},
		Buttons: []*ModalButton{{Label: nsUserFeedback.T("Cancel"), Kind: BtnOutline}, submit, visit}})
	h.ShowOverlay(m)
	return m
}

func (m *UserFeedbackModal) Layout(gtx layout.Context) layout.Dimensions { return m.modal.Layout(gtx) }
func (m *UserFeedbackModal) Dismissed() bool                             { return m.modal.Dismissed() }
func (m *UserFeedbackModal) Dismiss()                                    { m.modal.Dismiss() }

// ModalName is Slack's name for the dialog.
func (m *UserFeedbackModal) ModalName() string { return m.modal.ModalName() }

// Pick chooses "What brings you here?" (0 share, 1 help; tests).
func (m *UserFeedbackModal) Pick(i int) {
	if i >= -1 && i < len(ufKinds) {
		m.kind = i
	}
}

// Fill types the area and the feedback and ticks the box or not (tests).
func (m *UserFeedbackModal) Fill(area, feedback string, ack bool) {
	m.area.SetText(area)
	m.text.SetText(feedback)
	m.ack = ack
}

// Err is the error Alert's text ("" none).
func (m *UserFeedbackModal) Err() string { return m.err }

// CanSubmit reports Submit enabled (K null).
func (m *UserFeedbackModal) CanSubmit() bool { return m.invalid() == "" }

// invalid is K, what keeps Submit disabled ("" when it may send): an area
// or a feedback of only spaces, one past its limit, or the box unticked.
func (m *UserFeedbackModal) invalid() string {
	a, t := m.area.Text(), m.text.Text()
	if strings.TrimSpace(a) == "" || strings.TrimSpace(t) == "" || jsLen(a) > ufAreaMax || jsLen(t) > ufTextMax || !m.ack {
		return nsUserFeedback.T(ufFailed)
	}
	return ""
}

// helpURL is https://{team domain}/help.
func (m *UserFeedbackModal) helpURL() string {
	if c := m.h.Core(); c != nil && c.TeamURL() != "" {
		return strings.TrimSuffix(c.TeamURL(), "/") + "/help"
	}
	return "https://slack.com/help"
}

// Submit is the Submit button: the feedback goes to Slack, and the
// dialog stays until Slack answers.
func (m *UserFeedbackModal) Submit() {
	if bad := m.invalid(); bad != "" {
		m.err = bad
		return
	}
	c := m.h.Core()
	if c == nil || m.busy {
		return
	}
	m.err, m.busy = "", true
	area, text, h := m.area.Text(), m.text.Text(), m.h
	run(h, func(ctx context.Context) (struct{}, error) { return struct{}{}, c.SubmitUserFeedback(ctx, area, text) },
		func(_ struct{}, err error) {
			m.busy = false
			if err != nil {
				m.err = nsUserFeedback.T(ufFailed)
				if core.RateLimited(err) {
					m.err = nsUserFeedback.T(ufLimited)
				}
				return
			}
			m.modal.Dismiss()
			h.ShowOverlay(newUserFeedbackThanks(h))
		})
}

func (m *UserFeedbackModal) layout(gtx layout.Context) layout.Dimensions {
	th := m.h.Theme()
	cl := colorsOf(th)
	if m.selBtn.Clicked(gtx) {
		labels := make([]string, len(ufKinds))
		for i, k := range ufKinds {
			labels[i] = nsUserFeedback.T(k.label)
		}
		m.h.ShowOverlay(NewSelectList(m.h, m.selR, labels, m.kind, SelectListOpts{Placeholder: nsUserFeedback.T(ufPick)}, func(i int) { m.kind = i }))
	}
	if m.areaHit.Clicked(gtx) {
		gtx.Execute(key.FocusCmd{Tag: &m.area})
	}
	if m.textHit.Clicked(gtx) {
		gtx.Execute(key.FocusCmd{Tag: &m.text})
	}
	if m.ackBtn.Clicked(gtx) {
		m.ack = !m.ack
	}
	if m.helpBtn.Clicked(gtx) {
		srOpenLink(m.h, m.helpURL())
	}
	for _, ed := range [...]*widget.Editor{&m.area, &m.text} {
		for {
			if _, ok := ed.Update(gtx); !ok {
				break
			}
		}
	}
	w := gtx.Constraints.Max.X
	gap := dp(gtx, 20) // modalContentSections: gap 20px
	y := 0
	label := func(s string) {
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return para(gtx, th, 15, 22, theme.Bold, cl.Fg, s, 0)
		})
		y += d.Size.Y + dp(gtx, 8) // .c-label__text padding-bottom 8
	}
	if m.err != "" {
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions { return srAlert(gtx, th, cl, m.err, w) })
		y += d.Size.Y + gap
	}
	label(nsUserFeedback.T(ufAsk))
	m.selectBox(gtx, th, cl, y, w)
	y += dp(gtx, 36)
	switch m.kind {
	case 0:
		y += gap
		label(nsUserFeedback.T(ufArea))
		y += m.areaInput(gtx, th, cl, y, w)
		label(nsUserFeedback.T(ufText))
		y += m.textarea(gtx, th, cl, y, w)
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions { return m.ackRow(gtx, th, cl, w) })
		y += d.Size.Y + dp(gtx, 4) // .c-label--inline margin-bottom 4
	case 1:
		y += gap
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions { return m.helpText(gtx, th, cl, w) })
		y += d.Size.Y
	}
	return layout.Dimensions{Size: image.Pt(w, y+dp(gtx, 8))} // the sections' padding-bottom 8
}

// ufTooLongText is the inputs' maxCharacterLimit alert (form_inputs zI).
func ufTooLongText(n int) string { return nsFormInputs.T(ufTooLong, "maxCharacterLimit", n) }

// selectBox is the BasicSelect (.c-select_button, 36px, radius 8): the
// option chosen or "Select an option", a chevron at the right.
func (m *UserFeedbackModal) selectBox(gtx layout.Context, th *theme.Theme, cl *Colors, y, w int) {
	bh := dp(gtx, 36)
	r := image.Rect(0, y, w, y+bh)
	at(gtx, r.Min, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(r.Size())
		return m.selBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			srInputBox(gtx, cl, image.Rectangle{Max: r.Size()}, gtx.Focused(&m.selBtn))
			// renderContent: the placeholder, or under
			// basic_select_placeholder_option "--Select an option--" in the
			// button's own colour
			s, fg := nsUserFeedback.T(ufPick), cl.Placeholder
			if placeholderOption(m.h) {
				s, fg = formatPlaceholderText(nsUserFeedback.T(ufPick)), cl.Fg
			}
			if m.kind >= 0 {
				s, fg = nsUserFeedback.T(ufKinds[m.kind].label), cl.Fg
			}
			at(gtx, image.Pt(dp(gtx, 12), 0), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-dp(gtx, 44)), bh)}
				return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 15, theme.Regular, fg, s)
				})
			})
			at(gtx, image.Pt(w-dp(gtx, 32), (bh-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, "chevron-down", 20, cl.Fg2)
			})
			pointerCursor(gtx, r.Size())
			return layout.Dimensions{Size: r.Size()}
		})
	})
	m.selR = r.Add(dtModalOrigin(gtx, m.modal)) // body → window coordinates
}

// ufCountPad is the input's padding-right with a CharacterCount
// (getCharacterCountElementSize): the count's digits in ch (the input's
// 15px "0") plus twice the 12px inline padding, focused or not.
func ufCountPad(gtx layout.Context, th *theme.Theme, max, n int) int {
	digits := len(strconv.Itoa(max - n))
	return digits*textW(gtx, th, 15, theme.Regular, "0") + 2*dp(gtx, 12)
}

// ufCount is CharacterCount's characters-remaining box while its input
// is focused: at the input's top right (a 1px clear border, no left one;
// padding 8 12), 13px on 18px lines in content-sec, content-imp past the
// limit (--invalid). Its base-pry background is the input's own.
func ufCount(gtx layout.Context, th *theme.Theme, cl *Colors, max, n, w int) {
	s := strconv.Itoa(max - n)
	col := cl.Fg2
	if n > max {
		col = cl.Danger
	}
	nw := textW(gtx, th, 13, theme.Regular, s)
	at(gtx, image.Pt(w-dp(gtx, 1)-dp(gtx, 12)-nw, dp(gtx, 1)+dp(gtx, 8)), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, dp(gtx, 18), func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 13, theme.Regular, col, s)
		})
	})
}

// areaInput is the FormTextInput (.c-input_text: 36px, padding 4 12) with
// its CharacterCount; it returns its height with what follows it: the
// input's 20px margin, or past the limit the Alert right under it and its
// 16px (margin_bottom_100).
func (m *UserFeedbackModal) areaInput(gtx layout.Context, th *theme.Theme, cl *Colors, y, w int) int {
	ih := dp(gtx, 36)
	n := jsLen(m.area.Text())
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		sz := image.Pt(w, ih)
		gtx.Constraints = layout.Exact(sz)
		focused := gtx.Focused(&m.area)
		srInputBox(gtx, cl, image.Rectangle{Max: sz}, focused)
		m.areaHit.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
			pointer.CursorText.Add(gtx.Ops)
			return layout.Dimensions{Size: sz}
		})
		tw := maxInt(1, w-dp(gtx, 12)-ufCountPad(gtx, th, ufAreaMax, n))
		at(gtx, image.Pt(dp(gtx, 12), 0), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, ih, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Min: image.Pt(tw, 0), Max: image.Pt(tw, ih)}
				if m.area.Len() == 0 {
					call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 15, theme.Regular, cl.Placeholder, nsUserFeedback.T(ufAreaHint))
					})
					call.Add(gtx.Ops)
				}
				return th.Editor(gtx, &m.area, unit.Sp(15), cl.Fg, theme.WithAlpha(cl.Focus, 0x55))
			})
		})
		if focused {
			ufCount(gtx, th, cl, ufAreaMax, n, w)
		}
		return layout.Dimensions{Size: sz}
	})
	if n > ufAreaMax {
		d := at(gtx, image.Pt(0, y+ih), func(gtx layout.Context) layout.Dimensions {
			return srAlert(gtx, th, cl, ufTooLongText(ufAreaMax), w)
		})
		return ih + d.Size.Y + dp(gtx, 16)
	}
	return ih + dp(gtx, 20) // .c-input_text margin-bottom 20
}

// textarea is the FormTextareaInput (.c-input_textarea: 104px, padding 8
// 12 4, 15px on 22px lines) with its CharacterCount; it returns its
// height with what follows it inside .c-input_textarea__wrapper (margin
// bottom 20): past the limit the Alert right under it, else the hint
// (.c-hint: 13px on 18px lines in content-ter, margin 4 0 20).
func (m *UserFeedbackModal) textarea(gtx layout.Context, th *theme.Theme, cl *Colors, y, w int) int {
	h := dp(gtx, 104)
	n := jsLen(m.text.Text())
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		sz := image.Pt(w, h)
		gtx.Constraints = layout.Exact(sz)
		focused := gtx.Focused(&m.text)
		srInputBox(gtx, cl, image.Rectangle{Max: sz}, focused)
		m.textHit.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
			pointer.CursorText.Add(gtx.Ops)
			return layout.Dimensions{Size: sz}
		})
		tw := maxInt(1, w-dp(gtx, 12)-ufCountPad(gtx, th, ufTextMax, n))
		at(gtx, image.Pt(dp(gtx, 12), dp(gtx, 8)), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Min: image.Pt(tw, 0), Max: image.Pt(tw, h-dp(gtx, 12))}
			if m.text.Len() == 0 {
				call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min = image.Point{}
					return para(gtx, th, 15, 22, theme.Regular, cl.Placeholder, nsUserFeedback.T(ufTextHint), 4)
				})
				call.Add(gtx.Ops)
			}
			return th.Editor(gtx, &m.text, unit.Sp(15), cl.Fg, theme.WithAlpha(cl.Focus, 0x55))
		})
		if focused {
			ufCount(gtx, th, cl, ufTextMax, n, w)
		}
		return layout.Dimensions{Size: sz}
	})
	yy := h
	switch {
	case n > ufTextMax:
		d := at(gtx, image.Pt(0, y+yy), func(gtx layout.Context) layout.Dimensions {
			return srAlert(gtx, th, cl, ufTooLongText(ufTextMax), w)
		})
		yy += d.Size.Y
	case expOn(m.h, ExpSlashFeedbackModalWarning):
		yy += dp(gtx, 4)
		d := at(gtx, image.Pt(0, y+yy), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return para(gtx, th, 13, 18, theme.Regular, cl.Fg3, nsUserFeedback.T(ufWarn), 0)
		})
		yy += d.Size.Y
	}
	// the wrapper's 20px (the Alert's 16 and the hint's 20 fold into it)
	return yy + dp(gtx, 20)
}

// ackRow is the inline Label (.c-label--inline: the Checkbox, then the
// text in regular weight 1px up, wrapping in what is left; baseline
// aligned, so the 16px box sits 2px under the text's line top).
func (m *UserFeedbackModal) ackRow(gtx layout.Context, th *theme.Theme, cl *Colors, w int) layout.Dimensions {
	return m.ackBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		var bx layout.Dimensions
		at(gtx, image.Pt(0, dp(gtx, 1)), func(gtx layout.Context) layout.Dimensions {
			bx = Checkbox(gtx, th, m.ack, false, false)
			return bx
		})
		x := bx.Size.X + dp(gtx, 8)
		ld := at(gtx, image.Pt(x, -dp(gtx, 1)), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-x), 1<<20)}
			return para(gtx, th, 15, 22, theme.Regular, cl.Fg, nsUserFeedback.T(ufAck), 0)
		})
		sz := image.Pt(w, max(bx.Size.Y+dp(gtx, 1), ld.Size.Y))
		pointerCursor(gtx, sz)
		return layout.Dimensions{Size: sz}
	})
}

// helpText is the help option's rich text: the bold line, the break, the
// sentence with the Help Center link.
func (m *UserFeedbackModal) helpText(gtx layout.Context, th *theme.Theme, cl *Colors, w int) layout.Dimensions {
	var bold string
	var segs []srLinkSeg
	for _, p := range nsUserFeedback.RT(ufHelpText) {
		switch p.Tag {
		case "bold":
			bold += p.Text
		case "br":
		case "helpCenter":
			segs = append(segs, srLinkSeg{text: p.Text, link: &m.helpBtn})
		default:
			segs = append(segs, srLinkSeg{text: p.Text})
		}
	}
	bgtx := gtx
	bgtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
	bd := para(bgtx, th, 15, 22, theme.Bold, cl.Fg, strings.TrimRight(bold, " "), 0)
	pd := at(gtx, image.Pt(0, bd.Size.Y), func(gtx layout.Context) layout.Dimensions { return srLinkPara(gtx, th, cl, segs, w) })
	return layout.Dimensions{Size: image.Pt(w, bd.Size.Y+pd.Size.Y)}
}

// ufHeroBg is --dt_color-theme-surf-sec: rgba(aubergine-80, .07) light,
// rgba(aubergine-30, .3) dark, a theme's own value over them.
func ufHeroBg(th *theme.Theme) color.NRGBA {
	c := rgba(0x611f69, 0.07)
	if th != nil && th.Dark {
		c = rgba(0x7d3986, 0.3)
	}
	if th != nil {
		theme.SetColor(th.Tokens, &c, "var(--dt_color-theme-surf-sec)")
	}
	return c
}

// newUserFeedbackThanks is UserFeedbackConfirmationModal: the celebration
// on theme-surf-sec (padding 12) above the title bar, whose title wraps,
// the note and Close; no close button.
func newUserFeedbackThanks(h Host) *Modal {
	th := h.Theme()
	return NewModal(h, ModalOpts{Title: nsUserFeedback.T(ufThanks), NoClose: true, EnterSubmits: true,
		HeroH: 199 + 24,
		Hero: func(gtx layout.Context) layout.Dimensions {
			sz := gtx.Constraints.Max
			fill(gtx, image.Rectangle{Max: sz}, ufHeroBg(th))
			iw, ih := dp(gtx, 231), dp(gtx, 199)
			if img, ok := h.Images().Get(srFeedbackHero, iw); ok {
				at(gtx, image.Pt((sz.X-iw)/2, dp(gtx, 12)), func(gtx layout.Context) layout.Dimensions {
					return theme.Image(gtx, img, image.Pt(iw, ih), 0)
				})
			}
			return layout.Dimensions{Size: sz}
		},
		Body: func(gtx layout.Context) layout.Dimensions {
			cl := colorsOf(th)
			return para(gtx, th, 15, 22, theme.Regular, cl.Fg, nsUserFeedback.T(ufThanksTxt), 0)
		},
		Buttons: []*ModalButton{{Label: nsUserFeedback.T("Close"), Kind: BtnPrimary}}})
}
