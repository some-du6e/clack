package popups

// Search feedback, as Slack's web client has it (CDN build 407aff01646b,
// fetched 2026-11-10; docs/SEARCH-AUDIT.md):
//
//   - CombinedSearchFeedbackModal (boot-shared q9NO), opened by the search
//     popover's footer (feedbackType "omniswitcher", with the query and the
//     suggestions on screen), the results page's "Give feedback"
//     (CombinedViewFeedbackLink: the active tab's module) and the
//     no-results state's "Not the results you expected? Give feedback."
//     (GenericSearchEmptyState: module plus feedbackLabelOverride, e.g.
//     "inChannel"). Title "Share your feedback on Search"; a select "How
//     would you describe the search results you’re seeing?" (eight
//     categories, "Select an option"); "Tell us details about your
//     feedback" (a 1,000-character textarea, "What would you like us to
//     know?"); the note about what gets attached, with "contact our Support
//     team" (/help/requests/new on the workspace) and feedback@slack.com;
//     "I’m okay with being contacted by Slack about my feedback"
//     (unchecked); the footer's "Privacy Policy" link, Cancel and Submit
//     (disabled until a category and some text are in). Submit is
//     combinedSearchFeedbackSubmit → core.SubmitSearchFeedback
//     (feedback.submitEnterpriseSearchFeedback). A failure keeps the
//     dialog with "Something went wrong submitting your feedback. Please
//     try again."; success replaces it with
//     CombinedSearchFeedbackConfirmationModal.
//   - search in channel's footer: searchFeedbackDialog → core's
//     SearchFeedbackDialog (search.feedback with a client_token); Slack
//     opens its own dialog for that token.

import (
	"context"
	"image"
	"strings"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// srFeedbackCategories are the modal's select options, in Slack's order.
var srFeedbackCategories = [...]struct{ value, label string }{
	{"helpful", "The results are helpful"},
	{"missing_results", "Something is missing"},
	{"irrelevant_results", "I’m seeing confusing or surprising results"},
	{"poor_ranking", "The results are out of order"},
	{"slow", "It took too long to see results"},
	{"missing_functionality", "Search is missing a feature"},
	{"bug", "Search is broken"},
	{"other", "Something else"},
}

const (
	srFeedbackTitle     = "Share your feedback on Search"
	srFeedbackAsk       = "How would you describe the search results you’re seeing?"
	srFeedbackPick      = "Select an option"
	srFeedbackDetails   = "Tell us details about your feedback"
	srFeedbackHint      = "What would you like us to know?"
	srFeedbackContact   = "I’m okay with being contacted by Slack about my feedback"
	srFeedbackFailed    = "Something went wrong submitting your feedback. Please try again."
	srFeedbackThanks    = "Thank you! You’re helping us make Slack better."
	srFeedbackThanksTxt = "Slack is a tool built by people. We’re actively listening to all of the feedback you’re sharing. If applicable, a member of our Slack team will reach out to you shortly."
	srFeedbackMax       = 1000

	// srFeedbackHero is the confirmation's illustration ("three people
	// celebrating", 231×199, the db+D asset on Slack's public CDN).
	srFeedbackHero    = "https://a.slack-edge.com/bv1-13-br/celebration-5ae8885.png"
	srPrivacyPolicy   = "https://slack.com/trust/privacy/privacy-policy"
	srFeedbackAddress = "feedback@slack.com"
)

// srFeedbackOpts are CombinedSearchFeedbackModal's props.
type srFeedbackOpts struct {
	feedbackType string     // "omniswitcher" (the popover) or "search"
	query        string     // the query the feedback is about
	payload      func() any // the JSON payload, built at Submit
}

// SearchFeedbackModal is CombinedSearchFeedbackModal.
type SearchFeedbackModal struct {
	h     Host
	o     srFeedbackOpts
	modal *Modal

	cat     int // srFeedbackCategories index, -1 none
	selBtn  widget.Clickable
	selR    image.Rectangle // the select's window rectangle, last frame
	ed      widget.Editor
	edArea  widget.Clickable
	contact bool
	box     widget.Clickable
	support widget.Clickable // "contact our Support team"
	mail    widget.Clickable // feedback@slack.com
	privacy widget.Clickable // footer "Privacy Policy"
	submit  *ModalButton
	busy    bool
	err     string
}

// openSearchFeedback shows CombinedSearchFeedbackModal.
func openSearchFeedback(h Host, o srFeedbackOpts) *SearchFeedbackModal {
	m := &SearchFeedbackModal{h: h, o: o, cat: -1}
	m.submit = &ModalButton{Label: nsClassicNav.T("Submit"), Kind: BtnPrimary, Enabled: func() bool { return m.valid() && !m.busy },
		Do: func() bool { m.Submit(); return false }}
	m.modal = NewModal(h, ModalOpts{Title: nsClassicNav.T(srFeedbackTitle), Body: m.layout, Class: "p-feedback_modal",
		FocusStops: func() []event.Tag { return []event.Tag{&m.selBtn, &m.ed, &m.box} },
		Buttons:    []*ModalButton{{Label: nsClassicNav.T("Cancel"), Kind: BtnOutline}, m.submit}})
	h.ShowOverlay(m)
	return m
}

func (m *SearchFeedbackModal) Layout(gtx layout.Context) layout.Dimensions {
	d := m.modal.Layout(gtx)
	if !m.modal.Dismissed() {
		m.privacyLink(gtx)
	}
	return d
}
func (m *SearchFeedbackModal) Dismissed() bool { return m.modal.Dismissed() }
func (m *SearchFeedbackModal) Dismiss()        { m.modal.Dismiss() }

// valid is the Submit button's condition: a category, and 1–1,000
// characters that aren't only spaces.
func (m *SearchFeedbackModal) valid() bool {
	t := m.ed.Text()
	return m.cat >= 0 && t != "" && len([]rune(t)) <= srFeedbackMax && strings.TrimSpace(t) != ""
}

// Pick chooses a category (the select; tests).
func (m *SearchFeedbackModal) Pick(i int) {
	if i >= 0 && i < len(srFeedbackCategories) {
		m.cat = i
	}
}

// SetText types the details (tests).
func (m *SearchFeedbackModal) SetText(s string) { m.ed.SetText(s) }

// Submit is the Submit button: combinedSearchFeedbackSubmit. The dialog
// stays until Slack answers.
func (m *SearchFeedbackModal) Submit() {
	c := m.h.Core()
	if !m.valid() || m.busy || c == nil {
		return
	}
	var payload any
	if m.o.payload != nil {
		payload = m.o.payload()
	}
	f := core.SearchFeedback{FeedbackType: m.o.feedbackType, Category: srFeedbackCategories[m.cat].value,
		Text: m.ed.Text(), Contact: m.contact, Query: m.o.query, Payload: payload}
	m.busy, m.err = true, ""
	h := m.h
	run(h, func(ctx context.Context) (struct{}, error) { return struct{}{}, c.SubmitSearchFeedback(ctx, f) },
		func(_ struct{}, err error) {
			m.busy = false
			if err != nil {
				m.err = nsClassicNav.T(srFeedbackFailed)
				return
			}
			m.modal.Dismiss()
			h.ShowOverlay(newSearchFeedbackThanks(h))
		})
}

func (m *SearchFeedbackModal) layout(gtx layout.Context) layout.Dimensions {
	th := m.h.Theme()
	cl := colorsOf(th)
	// nothing is focused on open: Slack's Modal focuses the dialog itself
	// (Tab reaches the select first)
	if m.selBtn.Clicked(gtx) {
		labels := make([]string, len(srFeedbackCategories))
		for i, c := range srFeedbackCategories {
			labels[i] = nsClassicNav.T(c.label)
		}
		// BasicSelect (zDm3) with placeholderText "Select an option", empty
		// at first: the placeholder row under basic_select_placeholder_option
		// clears the choice (onPick -1)
		m.h.ShowOverlay(NewSelectList(m.h, m.selR, labels, m.cat, SelectListOpts{Placeholder: nsClassicNav.T(srFeedbackPick)}, func(i int) { m.cat = i }))
	}
	if m.edArea.Clicked(gtx) {
		gtx.Execute(key.FocusCmd{Tag: &m.ed})
	}
	if m.box.Clicked(gtx) {
		m.contact = !m.contact
	}
	if m.support.Clicked(gtx) {
		if c := m.h.Core(); c != nil && c.TeamURL() != "" {
			srOpenLink(m.h, c.TeamURL()+"help/requests/new")
		}
	}
	if m.mail.Clicked(gtx) {
		srOpenLink(m.h, "mailto:"+srFeedbackAddress)
	}
	for {
		if _, ok := m.ed.Update(gtx); !ok {
			break
		}
	}
	w := gtx.Constraints.Max.X
	y := 0
	if m.err != "" { // the error Alert, margin_bottom_75
		d := at(gtx, image.Point{}, func(gtx layout.Context) layout.Dimensions { return srAlert(gtx, th, cl, m.err, w) })
		y += d.Size.Y + dp(gtx, 12)
	}
	label := func(s string) {
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return para(gtx, th, 15, 22, theme.Bold, cl.Fg, s, 0)
		})
		y += d.Size.Y + dp(gtx, 8) // .c-label__text padding-bottom 8
	}
	label(nsClassicNav.T(srFeedbackAsk))
	m.selectBox(gtx, th, cl, y, w)
	y += dp(gtx, 36) + dp(gtx, 20) // margin_top_125
	label(nsClassicNav.T(srFeedbackDetails))
	m.textarea(gtx, th, cl, y, w)
	y += dp(gtx, 104) + dp(gtx, 20)
	// 1z6E's "… <contact>contact our Support team</contact> or write in to
	// <email>feedback@{rootDomain}</email>." (classic_nav): the tags are
	// the links
	var note []srLinkSeg
	for _, p := range nsClassicNav.RT("Note: some extra information will be included, including your search query and the results it brought up. If you’re looking to send feedback unrelated to search, <contact>contact our Support team</contact> or write in to <email>feedback@{rootDomain}</email>.", "rootDomain", "slack.com") {
		sg := srLinkSeg{text: p.Text}
		switch p.Tag {
		case "contact":
			sg.link = &m.support
		case "email":
			sg.link = &m.mail
		}
		if n := len(note); n > 0 && sg.link != nil && note[n-1].link == sg.link {
			note[n-1].text += sg.text // a placeholder inside a link joins it
			continue
		}
		note = append(note, sg)
	}
	d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions { return srLinkPara(gtx, th, cl, note, w) })
	y += d.Size.Y + dp(gtx, 16)
	d = at(gtx, image.Pt(0, y), prefCheckRow(m.h, &m.box, nsClassicNav.T(srFeedbackContact), func() bool { return m.contact }))
	y += d.Size.Y
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// selectBox is Slack's Select (.c-select_button, 36px, radius 8): the
// chosen category or "Select an option", a chevron at the right.
func (m *SearchFeedbackModal) selectBox(gtx layout.Context, th *theme.Theme, cl *Colors, y, w int) {
	bh := dp(gtx, 36)
	r := image.Rect(0, y, w, y+bh)
	at(gtx, r.Min, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(r.Size())
		return m.selBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			srInputBox(gtx, cl, image.Rectangle{Max: r.Size()}, gtx.Focused(&m.selBtn))
			// renderContent: the placeholder in .c-select_button__placeholder,
			// or under basic_select_placeholder_option "--Select an option--"
			// in the button's own colour
			s, fg := nsClassicNav.T(srFeedbackPick), cl.Placeholder
			if placeholderOption(m.h) {
				s, fg = formatPlaceholderText(nsClassicNav.T(srFeedbackPick)), cl.Fg
			}
			if m.cat >= 0 {
				s, fg = nsClassicNav.T(srFeedbackCategories[m.cat].label), cl.Fg
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

// srInputBox is an input's box (.c-select_button, .c-input_textarea):
// the 1px outline, or with the focus the border turns clear and the
// focus shadow rings it.
func srInputBox(gtx layout.Context, cl *Colors, box image.Rectangle, focused bool) {
	rad := dp(gtx, 8)
	if focused {
		// box-shadow 0 0 0 1px focus, 0 0 0 5px focus at 30%, as the
		// text field draws it (fieldNative)
		ring, one := dp(gtx, 5), maxInt(1, dp(gtx, 1))
		rrect(gtx, box.Inset(-ring), rad+ring, cl.FocusRing)
		rrect(gtx, box.Inset(-one), rad+one, cl.Focus)
		rrect(gtx, box, rad, cl.ModalBg)
		return
	}
	rrect(gtx, box, rad, cl.InputBorder)
	rrect(gtx, box.Inset(1), rad-1, cl.ModalBg)
}

// textarea is .c-input_textarea: 104px tall, radius 8, padding 8 12 4,
// 15px text on 22px lines, the placeholder in the tertiary colour.
func (m *SearchFeedbackModal) textarea(gtx layout.Context, th *theme.Theme, cl *Colors, y, w int) {
	h := dp(gtx, 104)
	r := image.Rect(0, y, w, y+h)
	focused := gtx.Focused(&m.ed)
	at(gtx, r.Min, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(r.Size())
		srInputBox(gtx, cl, image.Rectangle{Max: r.Size()}, focused)
		m.edArea.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: r.Size()} })
		tw := maxInt(1, w-dp(gtx, 24))
		at(gtx, image.Pt(dp(gtx, 12), dp(gtx, 8)), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Min: image.Pt(tw, 0), Max: image.Pt(tw, h-dp(gtx, 12))}
			if m.ed.Len() == 0 {
				call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min = image.Point{}
					return para(gtx, th, 15, 22, theme.Regular, cl.Placeholder, nsClassicNav.T(srFeedbackHint), 1)
				})
				call.Add(gtx.Ops)
			}
			return th.Editor(gtx, &m.ed, unit.Sp(15), cl.Fg, theme.WithAlpha(cl.Focus, 0x55))
		})
		return layout.Dimensions{Size: r.Size()}
	})
}

// privacyLink is the footer's "Privacy Policy" (left of Cancel / Submit,
// level with them).
func (m *SearchFeedbackModal) privacyLink(gtx layout.Context) {
	if m.privacy.Clicked(gtx) {
		srOpenLink(m.h, srPrivacyPolicy)
	}
	r := m.modal.rect
	if r.Empty() {
		return
	}
	th := m.h.Theme()
	cl := colorsOf(th)
	bh := dp(gtx, 36)
	y := r.Max.Y - dp(gtx, 20) - bh
	at(gtx, image.Pt(r.Min.X+dp(gtx, 28), y), func(gtx layout.Context) layout.Dimensions {
		return clickBox(gtx, &m.privacy, nil, func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, cl.Link, nsClassicNav.T("Privacy Policy"))
			})
		})
	})
}

// newSearchFeedbackThanks is CombinedSearchFeedbackConfirmationModal: the
// celebration on the theme colour (padding 12), the title, the note and
// OK; no close button, 525 wide. The title is two lines at this width:
// Slack's title bar (padding 20 28) wraps its h1, so the title is drawn in
// the body under a 20px header rather than as the one-line modal title.
func newSearchFeedbackThanks(h Host) *Modal {
	th := h.Theme()
	return NewModal(h, ModalOpts{Width: 525, NoClose: true, EnterSubmits: true, HeadH: 20,
		HeroH: 199 + 24,
		Hero: func(gtx layout.Context) layout.Dimensions {
			cl := colorsOf(th)
			sz := gtx.Constraints.Max
			fill(gtx, image.Rectangle{Max: sz}, cl.Aubergine)
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
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return para(gtx, th, 22, 30, theme.Black, cl.Fg, nsClassicNav.T(srFeedbackThanks), 0)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: 20}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return para(gtx, th, 15, 22, theme.Regular, cl.Fg, nsClassicNav.T(srFeedbackThanksTxt), 0)
					})
				}),
			)
		},
		Buttons: []*ModalButton{{Label: "OK", Kind: BtnPrimary}}})
}

// srAlert is Slack's Alert at level error (.c-alert--level_error,
// inline_boxed): the warning icon and the message on the important
// surface, radius 8, padding 12 16.
func srAlert(gtx layout.Context, th *theme.Theme, cl *Colors, msg string, w int) layout.Dimensions {
	bg := rgba(0xff81aa, float32(0x21)/255)
	if th != nil && th.Dark {
		bg = rgba(0xb41541, float32(0x40)/255)
	}
	call, d := record(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-dp(gtx, 32)-dp(gtx, 28)), 1<<20)}
		return para(gtx, th, 15, 22, theme.Regular, cl.Fg, msg, 0)
	})
	h := d.Size.Y + dp(gtx, 24)
	rrect(gtx, image.Rect(0, 0, w, h), dp(gtx, 8), bg)
	at(gtx, image.Pt(dp(gtx, 16), dp(gtx, 12)+(dp(gtx, 22)-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
		return icon(gtx, "warning", 20, cl.Danger)
	})
	at(gtx, image.Pt(dp(gtx, 16)+dp(gtx, 28), dp(gtx, 12)), func(gtx layout.Context) layout.Dimensions {
		call.Add(gtx.Ops)
		return d
	})
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// srLinkSeg is a run of paragraph text, a link when link is set.
type srLinkSeg struct {
	text string
	link *widget.Clickable
}

// srLinkPara wraps segs (15px on 22px lines) in w, link runs in the link
// colour and clickable; it returns the paragraph's size.
func srLinkPara(gtx layout.Context, th *theme.Theme, cl *Colors, segs []srLinkSeg, w int) layout.Dimensions {
	lh := dp(gtx, 22)
	x, line := 0, 0
	var words []string
	for _, sg := range segs {
		words = srSplitWords(sg.text, words[:0])
		for _, wd := range words {
			if wd == "\n" {
				x, line = 0, line+1
				continue
			}
			space := wd[0] == ' '
			if space && x == 0 {
				continue
			}
			ww := textW(gtx, th, 15, theme.Regular, wd)
			if x+ww > w && x > 0 {
				if space {
					continue
				}
				x, line = 0, line+1
			}
			fg := cl.Fg
			if sg.link != nil && !space {
				fg = cl.Link
			}
			word, c := wd, sg.link
			at(gtx, image.Pt(x, line*lh), func(gtx layout.Context) layout.Dimensions {
				draw := func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 15, theme.Regular, fg, word)
					})
				}
				if c == nil || space {
					return draw(gtx)
				}
				return clickBox(gtx, c, nil, draw)
			})
			x += ww
		}
	}
	return layout.Dimensions{Size: image.Pt(w, (line+1)*lh)}
}

// srOpenLink opens a link in the browser (or the mail client).
func srOpenLink(h Host, u string) {
	if err := openurl.Open(u); err != nil {
		h.Toast("Couldn't open the link: " + errText(err))
	}
}

// srFeedbackDialog is search in channel's "Give feedback":
// searchFeedbackDialog's search.feedback with the in-channel payload.
// Slack opens its dialog for the request's client_token.
func srFeedbackDialog(h Host, payload any) {
	c := h.Core()
	if c == nil {
		return
	}
	// Slack's onClick drops a failure (the fetcher only logs it), so no
	// toast here either.
	run(h, func(ctx context.Context) (struct{}, error) { return struct{}{}, c.SearchFeedbackDialog(ctx, payload) },
		func(struct{}, error) {})
}
