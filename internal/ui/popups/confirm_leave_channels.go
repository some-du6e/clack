package popups

import (
	"image"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// ConfirmLeaveChannelsModal (client-channel-list-view SYcU, build
// 4d0c78085232): the sidebar organize mode's Leave when some of the picked
// conversations can't be left. A c-sk-modal 520px square (modalBody: height
// 520), its header the title bar's "Leave channels" (22/900) and the
// message under it (15/22, ModalContentSection: 28px in; 16px below; the
// header's 1px otl-ter bottom line); the content and footer in
// .p-about_modal__contents' grey (rgb(var(--sk_foreground_min_solid))):
// 16px down, the names that can't be left, one per 22px line, 8px apart
// (channelList: gap 4 and each li's 4px margin), scrolling; the footer
// Cancel (outline) and Continue (primary), or OK alone when none can be
// left. Continue leaves the others (leaveChannelsInBulk) through leave.
func NewConfirmLeaveChannelsModal(h Host, toLeave int, cannot []string, leave func()) *Modal {
	ns := i18n.NS("confirm-leave-channels")
	none := toLeave == 0
	msg := ns.T("You can’t leave these channels.")
	if !none {
		msg = ns.T("You’re about to leave {totalChannels, plural, =1{# channel} other {# channels}}, but {channelIdsThatCannotBeLeftCount, plural, =1{# channel} other {these # channels}} can’t be removed just yet. Would you still like to continue with the others?",
			"totalChannels", toLeave+len(cannot), "channelIdsThatCannotBeLeftCount", len(cannot))
	}
	okLabel := ns.T("Continue")
	if none {
		okLabel = ns.T("OK")
	}
	ok := &ModalButton{Label: okLabel, Kind: BtnPrimary, Do: func() bool {
		if leave != nil {
			leave()
		}
		return true
	}}
	var buttons []*ModalButton
	if !none {
		buttons = append(buttons, &ModalButton{Label: ns.T("Cancel"), Kind: BtnOutline, Do: func() bool { return true }})
	}
	buttons = append(buttons, ok)
	names := append([]string(nil), cannot...)
	return NewModal(h, ModalOpts{
		Title:          ns.T("Leave channels"),
		Height:         520,
		HeaderLine:     true,
		HeaderBelowPad: 16,
		Contents:       true,
		HeaderBelow: func(gtx layout.Context) layout.Dimensions {
			th := h.Theme()
			return para(gtx, th, 15, 22, theme.Regular, colorsOf(th).Fg, msg, 0)
		},
		Body: func(gtx layout.Context) layout.Dimensions {
			th := h.Theme()
			cl := colorsOf(th)
			y := dp(gtx, 16)
			w := gtx.Constraints.Max.X
			for i, n := range names {
				n := n
				at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints = layout.Constraints{Max: image.Pt(w, dp(gtx, 22))}
					return para(gtx, th, 15, 22, theme.Regular, cl.Fg, n, 1)
				})
				y += dp(gtx, 22)
				if i < len(names)-1 {
					y += dp(gtx, 8)
				}
			}
			return layout.Dimensions{Size: image.Pt(w, y+dp(gtx, 4))}
		},
		Buttons:   buttons,
		FooterPad: 24, // c-sk-modal_footer: padding 24px 28px
	})
}
