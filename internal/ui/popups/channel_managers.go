package popups

// The channel details roster's channel manager confirmations, opened from
// a member row's "…" (views/details_agents_menu.go) as Slack's web client
// build 846030e1ce97 has them (ia-details-popover-members chunk; the
// writes are core/channel_managers_edit.go's):
//
//   - MakeChannelManagerModal (VSHD, I18n make_channel_manager_modal), a
//     c-sk-modal without a close button (showCloseButton false): the title
//     "Make {memberName} a Channel Manager of {channelName}?" (the
//     member's real name, getMemberRealName hWCs wg; the channel as
//     InlineChannelEntity draws it, its icon then its name), the line
//     "This person will be able to manage all aspects of the channel.",
//     then Cancel and the primary LoadingButton "Make Channel Manager".
//     The button loads while submitAddChannelManagers runs; done, the
//     modal closes; failed, an error Alert ("There was a problem making
//     this person a Channel Manager. Give it another try in a few
//     minutes.", margin_bottom_100) goes above the line and stays.
//   - RemoveManagerModal (HVho, I18n channel_details), a c-sk-modal with
//     its close button: "Remove {memberName} from Channel Managers of
//     #{channelName}?" (the channel's plain name, hsKo pP), "This person
//     won’t be able to manage the channel anymore. ", Cancel and the
//     LoadingButton "Remove From Channel Managers" (primary: the
//     button's default type). A press clears the alert, loads while
//     submitRemoveChannelManagers runs and closes when it's done; failed,
//     the alert says "There was a problem removing this person from
//     Channel Managers. Give it another try in a few minutes."
//
// Neither focuses a button on open (BaseModal focuses the dialog). Their
// titles wrap (.c-sk-modal_title_bar__text has no nowrap); a wrapped
// MakeChannelManagerModal title shows the channel as text, without the
// private channel's lock.

import (
	"context"
	"image"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// nsMakeManager is MakeChannelManagerModal's I18n namespace (VSHD eM).
var nsMakeManager = i18n.NS("make_channel_manager_modal")

// ChannelManagerModal is either open confirmation.
type ChannelManagerModal struct {
	*Modal
	// Heading is the whole title as text (the channel entity as "#name",
	// a private channel's bare name); Text is the body's line and Alert
	// the failure's text (Error).
	Heading, Text, Alert string
	Go                   *ModalButton
	pending              bool
	failed               bool
}

// Error is the error Alert's text ("" while there is none).
func (d *ChannelManagerModal) Error() string {
	if !d.failed {
		return ""
	}
	return d.Alert
}

// Pending reports a write in flight (the LoadingButton loads).
func (d *ChannelManagerModal) Pending() bool { return d.pending }

// managerNames are the titles' member and channel: the member's real
// name, and the channel's entity (its icon and name) or plain name.
func managerNames(a *core.App, ch, member string) (memberName string, icon string, entName, plainName string) {
	memberName, entName, plainName, icon = member, ch, ch, "hash"
	if a == nil {
		return
	}
	if u, ok := a.S.User(member); ok {
		memberName = u.Profile.RealName // getMemberRealName: profile.real_name, else real_name
		if memberName == "" {
			memberName = u.RealName
		}
	}
	ic := a.InlineChannel(ch)
	if ic.State == core.ChannelKnown {
		entName, plainName = ic.Name, ic.Name
	}
	if ic.Private {
		icon = "lock"
	}
	return
}

// OpenMakeChannelManagerModal opens MakeChannelManagerModal for member in
// ch.
func OpenMakeChannelManagerModal(h Host, ch, member string) *ChannelManagerModal {
	a := h.Core()
	memberName, ic, name, _ := managerNames(a, ch, member)
	d := &ChannelManagerModal{
		Text:  nsMakeManager.T("This person will be able to manage all aspects of the channel."),
		Alert: nsMakeManager.T("There was a problem making this person a Channel Manager. Give it another try in a few minutes."),
	}
	// the title's text around the channel entity
	pre, post, seen := "", "", false
	for _, part := range nsMakeManager.RT("Make {memberName} a Channel Manager of {channelName}?", "memberName", memberName, "channelName", "#") {
		switch {
		case part.Arg == "channelName":
			seen = true
		case seen:
			post += part.Text
		default:
			pre += part.Text
		}
	}
	ent := name
	if ic == "hash" {
		ent = "#" + name
	}
	d.Heading = pre + ent + post
	d.Go = &ModalButton{Label: nsMakeManager.T("Make Channel Manager"), Kind: BtnPrimary,
		Loading: func() bool { return d.pending },
		Do: func() bool {
			if d.pending || a == nil {
				return false
			}
			d.pending = true
			run(h, func(ctx context.Context) (struct{}, error) {
				return struct{}{}, a.AddChannelManagers(ctx, ch, []string{member})
			}, func(_ struct{}, err error) {
				d.pending = false
				if err != nil {
					d.failed = true
					h.Invalidate()
					return
				}
				d.Modal.Dismiss()
			})
			return false
		}}
	th := h.Theme()
	d.Modal = NewModal(h, ModalOpts{
		Title: pre, TitleIcon: ic, TitleAfter: name + post,
		// a title too long for its line wraps (.c-sk-modal_title_bar__text),
		// the channel then as text
		TitleWrap: true, TitleWrapText: d.Heading,
		NoClose: true, // showCloseButton false
		Class:   "p-make_channel_managers_modal",
		Body:    func(gtx layout.Context) layout.Dimensions { return d.body(gtx, th) },
		Buttons: []*ModalButton{{Label: nsMakeManager.T("Cancel"), Kind: BtnOutline}, d.Go},
	})
	h.ShowOverlay(d)
	return d
}

// OpenRemoveManagerModal opens RemoveManagerModal for member in ch.
func OpenRemoveManagerModal(h Host, ch, member string) *ChannelManagerModal {
	a := h.Core()
	memberName, _, _, plain := managerNames(a, ch, member)
	d := &ChannelManagerModal{
		Text:  nsChDetails.T("This person won’t be able to manage the channel anymore. "),
		Alert: nsChDetails.T("There was a problem removing this person from Channel Managers. Give it another try in a few minutes."),
	}
	d.Heading = nsChDetails.T("Remove {memberName} from Channel Managers of #{channelName}?", "memberName", memberName, "channelName", plain)
	d.Go = &ModalButton{Label: nsChDetails.T("Remove From Channel Managers"), Kind: BtnPrimary,
		Loading: func() bool { return d.pending },
		Do: func() bool {
			if d.pending || a == nil {
				return false
			}
			d.failed, d.pending = false, true
			run(h, func(ctx context.Context) (struct{}, error) {
				return struct{}{}, a.RemoveChannelManagers(ctx, ch, []string{member})
			}, func(_ struct{}, err error) {
				d.pending = false
				if err != nil {
					d.failed = true
					h.Invalidate()
					return
				}
				d.Modal.Dismiss()
			})
			return false
		}}
	th := h.Theme()
	d.Modal = NewModal(h, ModalOpts{
		Title:     d.Heading,
		TitleWrap: true, // .c-sk-modal_title_bar__text wraps
		Class:     "p-mpdm_add_people_modal",
		Body:      func(gtx layout.Context) layout.Dimensions { return d.body(gtx, th) },
		Buttons:   []*ModalButton{{Label: nsChDetails.T("Cancel"), Kind: BtnOutline}, d.Go},
	})
	h.ShowOverlay(d)
	return d
}

// body is the ModalContentSection: the error Alert (16px below it), then
// the line, 15/22.
func (d *ChannelManagerModal) body(gtx layout.Context, th *theme.Theme) layout.Dimensions {
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	y := 0
	if d.failed {
		y += actAlert(gtx, th, cl, 0, 0, w, d.Alert) // margin_bottom_100
	}
	dims := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w), 1<<20)}
		return para(gtx, th, 15, 22, theme.Regular, cl.Fg, d.Text, 0)
	})
	return layout.Dimensions{Size: image.Pt(w, y+dims.Size.Y)}
}
