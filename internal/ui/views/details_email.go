package views

// About's "Send emails to this channel" (docs/GAPS-2026-11-11.md gap 14,
// W13): Slack's SendEmailsToChannelButton (about-channel-modal Qjs2,
// build d3de3a487b1c). The modal it opens is popups/channel_email.go,
// the reads core/chdetails_email.go.
//
//   - On a free team that could have one (showChannelEmailAddressUpgradePrompt)
//     it's a field with "See Upgrade Options" (the plans page, entry point
//     channel_details_modal_integrations_tab_email_to_channel).
//   - Else, with showChannelEmailAddress and either a kept address or the
//     org allowing them (isChannelEmailAddressEnabledByOrg), the button
//     field: the email-up glyph in its tile, "Send emails to this channel"
//     ("conversation" in DMs and group DMs) and "Get an email address that
//     posts incoming emails in this channel."; a click opens the modal
//     (stacked over the details).
//   - Otherwise nothing.
//
// The TrialProBadge after the title (free and Pro teams) isn't drawn.

import (
	"net/url"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/popups"
)

// cdEmail is the emails field's state.
type cdEmail struct {
	upgrade widget.Clickable
	modal   *popups.ChannelEmailModal
}

// emailsMode is which SendEmailsToChannelButton renders: "upgrade",
// "button" or "".
func (d *ChannelDetails) emailsMode() string {
	a := d.h.Core()
	if a.ChannelEmailUpgradePrompt(d.id) {
		return "upgrade"
	}
	if a.ShowChannelEmail(d.id) {
		kept, _ := a.ChannelEmailCached(d.id)
		if kept.Address != "" || a.ChannelEmailEnabledByOrg() {
			return "button"
		}
	}
	return ""
}

// emailsItems are the About tab's emails card and the gap before it.
func (d *ChannelDetails) emailsItems() []W {
	switch d.emailsMode() {
	case "upgrade":
		return []W{cdGap(16), d.cdCard(d.emailsUpgradeField)}
	case "button":
		return []W{cdGap(16), d.cdCard(d.emailsField)}
	}
	return nil
}

// emailsTitle is the field's title and text: "channel", or
// "conversation" in DMs and group DMs (aK5v JM).
func (d *ChannelDetails) emailsTitle() (title, text string) {
	if cdGroup(d.kind) != "channel" {
		return nsChannelDetails.T("Send emails to this conversation"), nsChannelDetails.T("Get an email address that posts incoming emails in this conversation.")
	}
	return nsChannelDetails.T("Send emails to this channel"), nsChannelDetails.T("Get an email address that posts incoming emails in this channel.")
}

// emailsField is the ButtonField (a5gu x) with renderEmailIcon.
func (d *ChannelDetails) emailsField(gtx C) D {
	title, text := d.emailsTitle()
	return d.cdButtonField(gtx, &d.emails, func(gtx C) D {
		return layout.Inset{Top: 16, Bottom: 16, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
			return d.cdTileRow(gtx, "email-up", func(gtx C) D {
				return col(gtx,
					layout.Rigid(func(gtx C) D { return d.para(gtx, 15, 22, wBold, d.p.Text, 1, title) }),
					layout.Rigid(func(gtx C) D { return d.para(gtx, 15, 22, wReg, d.p.Text, 0, text) }),
				)
			})
		})
	})
}

// emailsUpgradeField is the upgrade prompt's field: the title and text,
// and "See Upgrade Options" at the right.
func (d *ChannelDetails) emailsUpgradeField(gtx C) D {
	title, text := d.emailsTitle()
	if d.em.upgrade.Clicked(gtx) {
		hostHandler{d.h}.OpenURL(d.plansURL())
	}
	return layout.Inset{Top: 16, Bottom: 16, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
		return row(gtx, layout.Middle,
			layout.Flexed(1, func(gtx C) D {
				return col(gtx,
					layout.Rigid(func(gtx C) D { return d.para(gtx, 15, 22, wBold, d.p.Text, 1, title) }),
					layout.Rigid(func(gtx C) D { return d.para(gtx, 15, 22, wReg, d.p.Text, 0, text) }),
				)
			}),
			hgap(12),
			layout.Rigid(func(gtx C) D {
				return cdBtn(&d.base, gtx, &d.em.upgrade, "", nsChannelDetails.T("See Upgrade Options"), false, 0)
			}),
		)
	})
}

// plansURL is getPlansPageUrl({entryPoint}) on the team's domain.
func (d *ChannelDetails) plansURL() string {
	host := "app.slack.com"
	if a := d.h.Core(); a != nil {
		select {
		case <-a.Ready():
			if dom := a.Snapshot().Team.Domain; dom != "" {
				host = dom + ".slack.com"
			}
		default:
		}
	}
	return "https://" + host + "/plans?" + url.Values{"entry_point": {"channel_details_modal_integrations_tab_email_to_channel"}}.Encode()
}

// emailsName is getDisplayNameForChannel: "#name" for a public channel,
// the name for a private one, the person in a DM, the names in a group DM.
func (d *ChannelDetails) emailsName() string {
	cv, _ := d.h.Core().Conv(d.id)
	switch cdGroup(d.kind) {
	case "im":
		return d.userName(cv.User)
	case "mpim":
		return d.convTitle(d.id, nil)
	}
	if d.kind == "private" {
		return cv.Name
	}
	return "#" + cv.Name
}

// openEmails is openChannelEmailAddressModal({channel, isStackable}).
func (d *ChannelDetails) openEmails() {
	ph, ok := d.h.(popups.Host)
	if !ok {
		return
	}
	d.em.modal = popups.OpenChannelEmail(ph, d.id, d.emailsName(), cdGroup(d.kind) != "channel")
}

// ---- test hooks ----

// EmailsMode is which emails field About shows: "upgrade", "button" or "".
func (d *ChannelDetails) EmailsMode() string { return d.emailsMode() }

// PressEmails clicks "Send emails to this channel".
func (d *ChannelDetails) PressEmails() { d.emails.Click(); d.h.Invalidate() }

// EmailModal is the channel email modal the field opened.
func (d *ChannelDetails) EmailModal() *popups.ChannelEmailModal { return d.em.modal }
