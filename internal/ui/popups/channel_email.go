package popups

// The channel email modal: Slack's ChannelEmailAddressMultiPaneModal
// (boot-shared fRXX, CDN build d3de3a487b1c, 2026-11-12), opened by
// channel details' "Send emails to this channel" (about-channel-modal
// Qjs2 SendEmailsToChannelButton: openChannelEmailAddressModal with
// isStackable). The reads and writes are core/chdetails_email.go.
//
// One dialog (p-channel_email_address_modal--updated-width: max-width
// 580) swapping panes, defaultPane "get":
//
//   - get (GetChannelEmailAddressModal): "Send emails to {channel}", the
//     info line with its "How to use this address" link, then a spinner
//     while conversations.emailaddresses.info answers, "Get Email Address"
//     when there's no address (it creates one; a new one in a channel gets
//     Slackbot's share prompt), or the read-only address (the local part
//     cut to 27 characters and "...@domain" until it has the focus) and
//     Copy ("{channel} (Slack)" <address>, "Email address copied to
//     clipboard."), then " Customize Email Appearance" and " Delete
//     Address". A failure is the boxed "Failed to get email address for
//     {channel}". No footer.
//   - delete (BaseDeleteChannelEmailAddressModal): a back arrow, "Delete
//     email address for {channel}?", the explanation and the danger
//     "Delete email address": it closes the dialog, then
//     conversations.emailaddresses.delete and "Email address revoked" (or
//     "Could not delete email address for {channel}").
//   - help (BaseHelpChannelEmailAddressModal): a back arrow, "How to use
//     this address", the text with its Gmail and Outlook links on the
//     help section's background (light: #fef4ea), Slack's 524×240
//     illustration (cea-info-light / -dark), and the footer's info icon
//     and "Learn more about sending emails to Slack".
//   - update (BaseCustomizeChannelEmailAddressModal): a back arrow,
//     "Customize email appearance", the explanation, "Email Icon" (a
//     select: Default icon, Image file — the file chooser, then the crop
//     pane — or Emoji — the emoji picker) beside "Email" (the sender name,
//     at most 50 characters), "Message Preview" (the icon, the name or
//     "Email", "Email", a message bar, fading out), and Save Changes:
//     conversations.emailaddresses.update, "Updated email icon and name
//     for {channel}", close.
//   - crop (EmailIconCropPane): avatars.upload with "Uploading your
//     photo…" and a spinner, then "Crop your photo" (the 460 cropper,
//     minSize 36) and Save: avatars.crop, back to update with the cropped
//     icon.
//
// Strings are Slack's I18n namespaces channel-email-addresses and
// edit_profile (the crop pane). Not drawn: the TrialProBadge after the
// title (free and Pro plans only) and the share prompt's Dismiss / Share
// Email Address buttons (Clack's local Slackbot messages are text).

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"strings"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/theme"
)

var nsChannelEmail = i18n.NS("channel-email-addresses")

// Channel email pane names (fRXX's w…x).
const (
	CEGet    = "get"
	CEDelete = "delete"
	CEUpdate = "update"
	CECrop   = "crop"
	CEHelp   = "help"
)

// Email icon choices (fRXX y, v, b).
const (
	ceIconImage   = "image"
	ceIconEmoji   = "emoji"
	ceIconDefault = "defaultIcon"
)

// Help-pane assets (boot-render 9AOJ, Vo6m) and links.
const (
	ceHelpDark    = "https://a.slack-edge.com/bv1-13/cea-info-dark-e3b0339.gif"
	ceHelpLight   = "https://a.slack-edge.com/bv1-13/cea-info-light-c9cee56.gif"
	ceGmailHelp   = "https://support.google.com/mail/answer/10957"
	ceOutlookHelp = "https://support.microsoft.com/en-us/office/turn-on-automatic-forwarding-in-outlook-on-the-web-7f2670a1-7fff-4475-8a3c-5822d63b0c8e"
	ceHelpArticle = "https://slack.com/help/articles/206819278" // HelpLink articleId 0xc53cfce
)

// ChannelEmailModal is the channel email dialog.
type ChannelEmailModal struct {
	h    Host
	ch   string
	name string // channelDisplayName
	mpim bool   // isMpimOrIm: no share prompt
	pane string
	m    *Modal
	done bool

	email   core.ChannelEmail // the modal's channelEmail (setChannelEmail)
	isErr   bool
	status  string // not_started, in_progress, done (k)
	getting bool   // Get Email Address loading (v)

	get  *ceGet
	del  *ceDelete
	help *ceHelp
	upd  *ceUpdate
	crop *ceCrop

	copyNow string // Copy's text, put on the clipboard next frame
}

// OpenChannelEmail opens the modal for ch, named name (Slack's
// getDisplayNameForChannel: "#name" for a public channel); mpim is a DM
// or group DM.
func OpenChannelEmail(h Host, ch, name string, mpim bool) *ChannelEmailModal {
	e := NewChannelEmailModal(h, ch, name, mpim)
	h.ShowOverlay(e)
	return e
}

// NewChannelEmailModal builds the modal on its get pane.
func NewChannelEmailModal(h Host, ch, name string, mpim bool) *ChannelEmailModal {
	e := &ChannelEmailModal{h: h, ch: ch, name: name, mpim: mpim, status: "not_started"}
	if a := h.Core(); a != nil {
		if kept, ok := a.ChannelEmailCached(ch); ok {
			e.email = kept
		}
	}
	e.email.ChannelID = ch
	e.switchPane(CEGet)
	return e
}

func (e *ChannelEmailModal) Layout(gtx layout.Context) layout.Dimensions {
	if e.done || e.m == nil {
		return layout.Dimensions{}
	}
	if e.copyNow != "" {
		copyText(gtx, e.copyNow)
		e.copyNow = ""
	}
	m := e.m
	d := m.Layout(gtx)
	if m == e.m && m.Dismissed() {
		e.done = true
	}
	return d
}

// Dismissed reports the modal closed.
func (e *ChannelEmailModal) Dismissed() bool { return e.done || (e.m != nil && e.m.Dismissed()) }

// Dismiss closes it (Escape).
func (e *ChannelEmailModal) Dismiss() {
	if e.m != nil {
		e.m.Dismiss()
	}
}

func (e *ChannelEmailModal) close() {
	if e.m != nil {
		e.m.Close()
	}
	e.done = true
}

// Pane is the pane on screen.
func (e *ChannelEmailModal) Pane() string { return e.pane }

// Email is the modal's channel email.
func (e *ChannelEmailModal) Email() core.ChannelEmail { return e.email }

// Failed reports the get pane's error alert.
func (e *ChannelEmailModal) Failed() bool { return e.isErr }

// Status is the get pane's load state: not_started, in_progress or done.
func (e *ChannelEmailModal) Status() string { return e.status }

// Modal is the pane's dialog (tests press its buttons).
func (e *ChannelEmailModal) Modal() *Modal { return e.m }

// SwitchPane is switchPane(name) (tests).
func (e *ChannelEmailModal) SwitchPane(p string) { e.switchPane(p) }

func (e *ChannelEmailModal) switchPane(p string) {
	e.pane = p
	switch p {
	case CEGet: // each pane mounts afresh (its useState starts over)
		e.status, e.get = "not_started", newCEGet(e)
		e.m = e.get.m
	case CEDelete:
		e.del = newCEDelete(e)
		e.m = e.del.m
	case CEHelp:
		e.help = newCEHelp(e)
		e.m = e.help.m
	case CEUpdate:
		e.showUpdate("", nil)
		return
	case CECrop:
		e.m = e.crop.m
	}
	e.h.Invalidate()
}

// showUpdate mounts the update pane with switchPane's props
// (nameInProgress, croppedImage).
func (e *ChannelEmailModal) showUpdate(nameInProgress string, cropped *core.EmailIcon) {
	e.pane = CEUpdate
	e.upd = newCEUpdate(e, nameInProgress, cropped)
	e.m = e.upd.m
	e.h.Invalidate()
}

// ceBackHeader is the panes' ModalHeader: the back arrow (c-multi_pane_
// modal__back) and the title bar.
func ceBackHeader(h Host, back *widget.Clickable, title string, onBack func()) func(gtx layout.Context) layout.Dimensions {
	return func(gtx layout.Context) layout.Dimensions {
		th := h.Theme()
		cl := colorsOf(th)
		sz := gtx.Constraints.Max
		if back.Clicked(gtx) {
			onBack()
		}
		b := dp(gtx, 36)
		at(gtx, image.Pt(dp(gtx, 20), (sz.Y-b)/2), func(gtx layout.Context) layout.Dimensions {
			return iconButton(gtx, back, "arrow-left", 36, 20, cl.Fg2, cl.BgHover, 8)
		})
		x := dp(gtx, 20) + b + dp(gtx, 8)
		at(gtx, image.Pt(x, (sz.Y-dp(gtx, 30))/2), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, sz.X-x-dp(gtx, 64)), dp(gtx, 30))}
			return para(gtx, th, 22, 30, theme.Black, cl.Fg, title, 1)
		})
		return layout.Dimensions{Size: sz}
	}
}

// ceLinkSegs cuts an rt() string into link and text segments; link picks
// each <a>'s clickable from its text.
func ceLinkSegs(parts []i18n.Part, link func(text string) *widget.Clickable) []srLinkSeg {
	var out []srLinkSeg
	for _, p := range parts {
		if p.Tag == "a" {
			out = append(out, srLinkSeg{text: p.Text, link: link(p.Text)})
			continue
		}
		out = append(out, srLinkSeg{text: p.Text})
	}
	return out
}

// ---- get ----

type ceGet struct {
	e         *ChannelEmailModal
	m         *Modal
	howTo     widget.Clickable
	getBtn    widget.Clickable
	copyBtn   widget.Clickable
	customize widget.Clickable
	del       widget.Clickable
	addr      widget.Editor
	focused   bool
	started   bool
}

func newCEGet(e *ChannelEmailModal) *ceGet {
	g := &ceGet{e: e}
	g.addr.SingleLine, g.addr.ReadOnly = true, true
	g.m = NewModal(e.h, ModalOpts{Title: nsChannelEmail.T("Send emails to {channelDisplayName}", "channelDisplayName", e.name),
		Width: 580, Body: g.layout, Class: "p-channel_email_address_modal", OnClose: func() { e.done = true }})
	return g
}

// load is the pane's mount effect: the kept address, else
// getChannelEmail({channelId}).
func (g *ceGet) load() {
	e := g.e
	if g.started {
		return
	}
	g.started = true
	if e.email.Address != "" {
		e.status = "done"
		return
	}
	if e.status != "not_started" {
		return
	}
	a := e.h.Core()
	if a == nil {
		e.status, e.isErr = "done", true
		return
	}
	e.status = "in_progress"
	ch := e.ch
	run(e.h, func(ctx context.Context) (core.ChannelEmail, error) {
		return a.GetChannelEmail(ctx, ch, false), nil
	}, func(r core.ChannelEmail, _ error) {
		e.status = "done"
		if r.Err {
			e.isErr = true
		} else {
			e.email = r
		}
		e.h.Invalidate()
	})
}

// GetAddress is "Get Email Address": getChannelEmail({createIfNotExists}).
func (e *ChannelEmailModal) GetAddress() {
	a := e.h.Core()
	if a == nil || e.getting {
		return
	}
	e.isErr, e.getting = false, true
	ch, name, mpim := e.ch, e.name, e.mpim
	run(e.h, func(ctx context.Context) (core.ChannelEmail, error) {
		r := a.GetChannelEmail(ctx, ch, true)
		if !r.Err && r.CreatedNew && !mpim {
			// sendEphemeralFeedback with GuCY Jw's section text
			a.LocalEphemeral(ch, "", nsChannelEmail.T("Now that you have set an email address for {channelDisplayName}, would you like me to share it with everyone else in this channel? They’ll see it along with some brief instructions for how to use it.", "channelDisplayName", name))
		}
		return r, nil
	}, func(r core.ChannelEmail, _ error) {
		e.getting = false
		if r.Err {
			e.isErr = true
		} else {
			e.email = r
		}
		e.h.Invalidate()
	})
}

// CopyAddress is Copy: `"{channel} (Slack)" <address>` and the toast.
func (e *ChannelEmailModal) CopyAddress() string {
	if e.email.Address == "" {
		return ""
	}
	s := `"` + e.name + ` (Slack)" <` + e.email.Address + `>`
	e.copyNow = s
	e.h.Toast(nsChannelEmail.T("Email address copied to clipboard."))
	e.h.Invalidate()
	return s
}

// CEShortAddress is the address field's value without the focus: a
// local part over 27 characters cut to 27 and "...".
func CEShortAddress(s string) string {
	if s == "" {
		return ""
	}
	local, domain, ok := strings.Cut(s, "@")
	if !ok || strings.Contains(domain, "@") {
		return s
	}
	if r := []rune(local); len(r) > 27 {
		return string(r[:27]) + "...@" + domain
	}
	return s
}

func (g *ceGet) layout(gtx layout.Context) layout.Dimensions {
	e := g.e
	g.load()
	th := e.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	if g.howTo.Clicked(gtx) {
		e.switchPane(CEHelp)
	}
	if g.getBtn.Clicked(gtx) {
		e.GetAddress()
	}
	if g.copyBtn.Clicked(gtx) {
		e.CopyAddress()
	}
	if g.customize.Clicked(gtx) {
		e.switchPane(CEUpdate)
	}
	if g.del.Clicked(gtx) {
		e.switchPane(CEDelete)
	}
	y := 0
	info := nsChannelEmail.RT("Emails sent to this email address will be posted in the {channelDisplayName} channel. <a>How to use this address</a>.",
		"channelDisplayName", e.name)
	d := srLinkPara(gtx, th, cl, ceLinkSegs(info, func(string) *widget.Clickable { return &g.howTo }), w)
	y += d.Size.Y + dp(gtx, 12) + dp(gtx, 6) // info_text margin-bottom 12, label margin-top 6
	switch {
	case e.status == "in_progress":
		s := dp(gtx, 32) // Spinner size large
		at(gtx, image.Pt(w-s, y), func(gtx layout.Context) layout.Dimensions {
			epSpinner(gtx, s, cl.Fg3)
			return layout.Dimensions{Size: image.Pt(s, s)}
		})
		y += s
	case e.status == "done" && e.email.Address == "":
		kind := BtnPrimary
		if e.getting {
			kind = BtnDisabled
		}
		call, bd := record(gtx, func(gtx layout.Context) layout.Dimensions {
			return Button(gtx, th, &g.getBtn, kind, BtnMedium, nsChannelEmail.T("Get Email Address"), "", 80)
		})
		macroAt(gtx, call, image.Pt(w-bd.Size.X, y))
		y += bd.Size.Y + dp(gtx, 16)
	case e.status == "done":
		call, bd := record(gtx, func(gtx layout.Context) layout.Dimensions {
			return Button(gtx, th, &g.copyBtn, BtnPrimary, BtnMedium, nsChannelEmail.T("Copy"), "", 80)
		})
		bw := bd.Size.X
		iw := maxInt(1, w-bw-dp(gtx, 12))
		focused := gtx.Focused(&g.addr)
		want := CEShortAddress(e.email.Address)
		if focused {
			want = e.email.Address
		}
		if g.addr.Text() != want {
			g.addr.SetText(want)
		}
		if focused && !g.focused { // forceSelectAll
			g.addr.SetCaret(g.addr.Len(), 0)
		}
		g.focused = focused
		ih := dp(gtx, 36)
		at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(iw, ih))
			return fieldOn(gtx, th, &g.addr, "", "", 36, 15, cl.ModalBg)
		})
		macroAt(gtx, call, image.Pt(w-bw, y))
		y += ih + dp(gtx, 8) // .c-input_text margin-bottom 8
		// sub_content: the two links, margin-top 16 / right 16, padding-bottom 16
		y += dp(gtx, 16)
		x := 0
		for _, l := range []struct {
			c     *widget.Clickable
			icon  string
			label string
		}{{&g.customize, "pencil", nsChannelEmail.T(" Customize Email Appearance")}, {&g.del, "trash", nsChannelEmail.T(" Delete Address")}} {
			ld := at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
				return clickBox(gtx, l.c, nil, func(gtx layout.Context) layout.Dimensions {
					lh := dp(gtx, 22)
					icon(gtx, l.icon, 15, cl.Link)
					tw := textW(gtx, th, 15, theme.Regular, l.label)
					at(gtx, image.Pt(dp(gtx, 15), 0), func(gtx layout.Context) layout.Dimensions {
						return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
							return text(gtx, th, 15, theme.Regular, cl.Link, l.label)
						})
					})
					return layout.Dimensions{Size: image.Pt(dp(gtx, 15)+tw, lh)}
				})
			})
			x += ld.Size.X + dp(gtx, 16)
		}
		y += dp(gtx, 22) + dp(gtx, 16)
	}
	if e.isErr {
		h := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			hh := cnAlert(gtx, th, cl, true, []cnRun{{s: nsChannelEmail.T("Failed to get email address for {channelDisplayName}", "channelDisplayName", e.name)}}, w)
			return layout.Dimensions{Size: image.Pt(w, hh)}
		})
		y += h.Size.Y + dp(gtx, 24)
	}
	return layout.Dimensions{Size: image.Pt(w, y+dp(gtx, 24))} // get_body padding-bottom 24
}

// ---- delete ----

type ceDelete struct {
	e    *ChannelEmailModal
	m    *Modal
	back widget.Clickable
}

func newCEDelete(e *ChannelEmailModal) *ceDelete {
	d := &ceDelete{e: e}
	title := nsChannelEmail.T("Delete email address for {channelDisplayName}?", "channelDisplayName", e.name)
	del := &ModalButton{Label: nsChannelEmail.T("Delete email address"), Kind: BtnDanger, Do: func() bool { e.DeleteAddress(); return false }}
	d.m = NewModal(e.h, ModalOpts{Header: ceBackHeader(e.h, &d.back, title, func() { e.switchPane(CEGet) }), Width: 580,
		Body: d.layout, Buttons: []*ModalButton{del}, OnClose: func() { e.done = true }})
	return d
}

func (d *ceDelete) layout(gtx layout.Context) layout.Dimensions {
	th := d.e.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	body := nsChannelEmail.T("Emails sent or forwarded to the email address for {channelDisplayName} will no longer be posted in the channel when you delete the email address.",
		"channelDisplayName", d.e.name)
	pd := at(gtx, image.Pt(0, dp(gtx, 16)), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		return para(gtx, th, 15, 22, theme.Regular, cl.Fg, body, 0)
	})
	return layout.Dimensions{Size: image.Pt(w, dp(gtx, 16)+pd.Size.Y+dp(gtx, 8))}
}

// DeleteAddress is the danger button: close, then
// deleteChannelEmailAddress and its toast.
func (e *ChannelEmailModal) DeleteAddress() {
	a := e.h.Core()
	addr, ch, name, h := e.email.Address, e.ch, e.name, e.h
	e.close()
	if a == nil || addr == "" {
		h.Toast(nsChannelEmail.T("Could not delete email address for {channelDisplayName}", "channelDisplayName", name))
		return
	}
	run(h, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, a.DeleteChannelEmail(ctx, ch, addr)
	}, func(_ struct{}, err error) {
		if err != nil {
			h.Toast(nsChannelEmail.T("Could not delete email address for {channelDisplayName}", "channelDisplayName", name))
			return
		}
		h.Toast(nsChannelEmail.T("Email address revoked"))
	})
}

// ---- help ----

type ceHelp struct {
	e                   *ChannelEmailModal
	m                   *Modal
	back                widget.Clickable
	gmail, outlook, art widget.Clickable
}

func newCEHelp(e *ChannelEmailModal) *ceHelp {
	p := &ceHelp{e: e}
	p.m = NewModal(e.h, ModalOpts{Header: ceBackHeader(e.h, &p.back, nsChannelEmail.T("How to use this address"), func() { e.switchPane(CEGet) }),
		Width: 580, Body: p.layout, Flush: true, FooterLeft: p.footer, OnClose: func() { e.done = true }})
	return p
}

func (p *ceHelp) layout(gtx layout.Context) layout.Dimensions {
	th := p.e.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	if p.gmail.Clicked(gtx) {
		srOpenLink(p.e.h, ceGmailHelp)
	}
	if p.outlook.Clicked(gtx) {
		srOpenLink(p.e.h, ceOutlookHelp)
	}
	padX := dp(gtx, 28)
	tw := maxInt(1, w-2*padX)
	parts := nsChannelEmail.RT("Keep all your work communication in one place. Use this address to send individual emails directly to {channelDisplayName} or set up automatic email forwarding from providers like <a> Gmail </a> and <a> Outlook </a> so incoming emails — like alerts or results from a contact form on your website — arrive straight to {channelDisplayName}.",
		"channelDisplayName", p.e.name)
	segs := ceLinkSegs(parts, func(t string) *widget.Clickable {
		if strings.Contains(t, "Outlook") {
			return &p.outlook
		}
		return &p.gmail
	})
	call, td := record(gtx, func(gtx layout.Context) layout.Dimensions { return srLinkPara(gtx, th, cl, segs, tw) })
	// help_content_section: padding 16 0, min-height 430; the body's
	// margin 16 0 8; the image container 240 high, 16 below.
	h := dp(gtx, 16) + dp(gtx, 16) + td.Size.Y + dp(gtx, 8) + dp(gtx, 16) + dp(gtx, 240) + dp(gtx, 16)
	h = maxInt(h, dp(gtx, 430))
	bg := cl.BgAlt // sk_foreground_min_solid
	if !th.Dark {
		bg = rgb(0xfef4ea) // --light-mode
	}
	fill(gtx, image.Rect(0, 0, w, h), bg)
	y := dp(gtx, 32)
	macroAt(gtx, call, image.Pt(padX, y))
	y += td.Size.Y + dp(gtx, 8) + dp(gtx, 16)
	src := ceHelpLight
	if th.Dark {
		src = ceHelpDark
	}
	iw, ih := dp(gtx, 524), dp(gtx, 240)
	iw = minInt(iw, w)
	at(gtx, image.Pt((w-iw)/2, y), func(gtx layout.Context) layout.Dimensions {
		if img, ok := p.e.h.Images().Get(src, iw); ok {
			return theme.Image(gtx, img, image.Pt(iw, ih), 0)
		}
		s := dp(gtx, 32) // renderPending: the spinner
		at(gtx, image.Pt((iw-s)/2, (ih-s)/2), func(gtx layout.Context) layout.Dimensions {
			epSpinner(gtx, s, cl.Fg3)
			return layout.Dimensions{Size: image.Pt(s, s)}
		})
		return layout.Dimensions{Size: image.Pt(iw, ih)}
	})
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// footer is the help footer (justify flex-start): the info-circle icon
// and the help link, both at 70% of the foreground.
func (p *ceHelp) footer(gtx layout.Context) layout.Dimensions {
	th := p.e.h.Theme()
	cl := colorsOf(th)
	if p.art.Clicked(gtx) {
		srOpenLink(p.e.h, ceHelpArticle)
	}
	c := theme.WithAlpha(cl.Fg, 0xb3)
	label := nsChannelEmail.T("Learn more about sending emails to Slack")
	return clickBox(gtx, &p.art, nil, func(gtx layout.Context) layout.Dimensions {
		lh := dp(gtx, 22)
		at(gtx, image.Pt(0, (lh-dp(gtx, 15))/2), func(gtx layout.Context) layout.Dimensions { return icon(gtx, "info-circle", 15, c) })
		x := dp(gtx, 15) + dp(gtx, 4)
		tw := textW(gtx, th, 15, theme.Regular, label)
		at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 15, theme.Regular, c, label) })
		})
		if p.art.Hovered() {
			fill(gtx, image.Rect(x, lh-dp(gtx, 3), x+tw, lh-dp(gtx, 2)), c)
		}
		return layout.Dimensions{Size: image.Pt(x+tw, lh)}
	})
}

// ---- update (customize) ----

type ceUpdate struct {
	e      *ChannelEmailModal
	m      *Modal
	back   widget.Clickable
	sel    widget.Clickable
	name   widget.Editor
	img    *core.EmailIcon // w: the picked or kept image
	emoji  string          // T: ":name:"
	saving bool
	failed bool
	selR   image.Rectangle
}

func newCEUpdate(e *ChannelEmailModal, nameInProgress string, cropped *core.EmailIcon) *ceUpdate {
	u := &ceUpdate{e: e}
	u.name.SingleLine, u.name.MaxLen = true, 50
	name := nameInProgress
	if name == "" {
		name = e.email.Name
	}
	u.name.SetText(name)
	if cropped != nil {
		u.img = cropped
	} else if src := e.email.Icons.Image(); src != "" {
		u.img = &core.EmailIcon{URL: src}
	}
	u.emoji = e.email.Icons.Emoji
	save := &ModalButton{Label: nsChannelEmail.T("Save Changes"), Kind: BtnPrimary,
		Enabled: func() bool {
			return !u.saving && !(u.name.Len() == 0 && u.img == nil) && len([]rune(u.name.Text())) <= 50
		},
		Do: func() bool { u.Save(); return false }}
	u.m = NewModal(e.h, ModalOpts{Header: ceBackHeader(e.h, &u.back, nsChannelEmail.T("Customize email appearance"), func() { e.switchPane(CEGet) }),
		Width: 580, Body: u.layout, Buttons: []*ModalButton{save}, AutoFocus: save, QuietFocus: true,
		FocusStops: func() []event.Tag { return []event.Tag{&u.sel, &u.name} }, OnClose: func() { e.done = true }})
	return u
}

// Choice is Q(): image when the picked image isn't the default plugin
// icon, else emoji when one is set, else the default icon.
func (u *ceUpdate) Choice() string {
	switch {
	case u.img != nil && !core.IsDefaultEmailIcon(u.img.URL):
		return ceIconImage
	case u.emoji != "":
		return ceIconEmoji
	}
	return ceIconDefault
}

func ceIconLabels() []string {
	return []string{nsChannelEmail.T("Default icon"), nsChannelEmail.T("Image file"), nsChannelEmail.T("Emoji")}
}

var ceIconValues = []string{ceIconDefault, ceIconImage, ceIconEmoji}

// Choose is the select's onSelectionChange (Y).
func (u *ceUpdate) Choose(v string) {
	switch v {
	case ceIconEmoji:
		at := u.selR.Min
		u.e.h.OpenEmojiPicker(image.Pt(at.X, u.selR.Max.Y), func(name string) {
			u.img, u.emoji = nil, ":"+strings.Trim(name, ":")+":"
			u.e.h.Invalidate()
		})
	case ceIconImage:
		u.pickImage()
	default:
		u.img, u.emoji = nil, ""
	}
	u.e.h.Invalidate()
}

// pickImage is the hidden file input: the chosen image goes to the crop
// pane with the name typed so far.
func (u *ceUpdate) pickImage() {
	e := u.e
	if PickFiles == nil {
		e.h.Toast("No file chooser on this platform")
		return
	}
	name := u.name.Text()
	h := e.h
	go func() {
		fs, err := PickFiles("image/*", false)
		if err != nil || len(fs) == 0 {
			return
		}
		f := fs[0]
		post := func() {
			u.emoji = ""
			e.crop = newCECrop(e, f, name)
			e.switchPane(CECrop)
		}
		if p, ok := h.(interface{ Post(func()) }); ok {
			p.Post(post)
		} else {
			post()
		}
	}()
}

// Save is Z: updateChannelEmailAddress, the toast, close.
func (u *ceUpdate) Save() {
	e := u.e
	a := e.h.Core()
	if a == nil || u.saving || e.email.Address == "" {
		return
	}
	up := core.EmailUpdate{Name: u.name.Text(), Address: e.email.Address}
	if up.Name == "" {
		up.Name = "Email"
	}
	switch u.Choice() {
	case ceIconImage:
		if u.img != nil {
			up.Icon = u.img.ID
		}
	case ceIconEmoji:
		up.Emoji = u.emoji
	default:
		up.UseDefaultIcon = true
	}
	u.saving, u.failed = true, false
	ch, name := e.ch, e.name
	run(e.h, func(ctx context.Context) (core.ChannelEmail, error) {
		return a.UpdateChannelEmail(ctx, ch, up)
	}, func(r core.ChannelEmail, err error) {
		u.saving = false
		if err != nil {
			u.failed = true
			e.h.Invalidate()
			return
		}
		e.email = r
		e.h.Toast(nsChannelEmail.T("Updated email icon and name for {channelDisplayName}", "channelDisplayName", name))
		e.close()
	})
}

func (u *ceUpdate) layout(gtx layout.Context) layout.Dimensions {
	e := u.e
	th := e.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	choice := u.Choice()
	if u.sel.Clicked(gtx) {
		sel := 0
		for i, v := range ceIconValues {
			if v == choice {
				sel = i
			}
		}
		e.h.ShowOverlay(NewSelectList(e.h, u.selR, ceIconLabels(), sel, SelectListOpts{}, func(i int) {
			if i >= 0 && i < len(ceIconValues) {
				u.Choose(ceIconValues[i])
			}
		}))
	}
	for {
		if _, ok := u.name.Update(gtx); !ok {
			break
		}
	}
	y := dp(gtx, 5)
	if u.failed {
		h := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			hh := cnAlert(gtx, th, cl, true, []cnRun{{s: nsChannelEmail.T("Sorry! Something went wrong. Please try again.")}}, w)
			return layout.Dimensions{Size: image.Pt(w, hh)}
		})
		y += h.Size.Y
	}
	y += dp(gtx, 16) // __body margin-top 16
	intro := nsChannelEmail.T("You can change the appearance of the email that gets posted in channel to make it more recognizable to channel members:")
	pd := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		return para(gtx, th, 15, 22, theme.Regular, cl.Fg, intro, 0)
	})
	y += pd.Size.Y + dp(gtx, 16) // body_customize margin-top 16
	half := (w - dp(gtx, 8)) / 2
	labelH := dp(gtx, 22)
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		return text(gtx, th, 15, theme.Bold, cl.Fg, nsChannelEmail.T("Email Icon"))
	})
	at(gtx, image.Pt(half+dp(gtx, 8), y), func(gtx layout.Context) layout.Dimensions {
		return text(gtx, th, 15, theme.Bold, cl.Fg, nsChannelEmail.T("Email"))
	})
	y += labelH + dp(gtx, 8) // margin_bottom_50 / the label's gap
	ih := dp(gtx, 36)
	org := dtModalOrigin(gtx, u.m)
	u.selR = image.Rectangle{Min: org.Add(image.Pt(0, y)), Max: org.Add(image.Pt(half, y+ih))}
	labels := ceIconLabels()
	cur := labels[0]
	for i, v := range ceIconValues {
		if v == choice {
			cur = labels[i]
		}
	}
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		return ceSelectBox(gtx, th, cl, &u.sel, cur, half, ih, u.m.ringOn(gtx, &u.sel))
	})
	at(gtx, image.Pt(half+dp(gtx, 8), y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(w-half-dp(gtx, 8), ih))
		return fieldOn(gtx, th, &u.name, nsChannelEmail.T("Email"), "", 36, 15, cl.ModalBg)
	})
	y += ih + dp(gtx, 16)
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		return text(gtx, th, 13, theme.Regular, theme.WithAlpha(cl.Fg, 0xb3), nsChannelEmail.T("Message Preview"))
	})
	y += dp(gtx, 18) + dp(gtx, 4)
	pv := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions { return u.preview(gtx, th, cl, w) })
	y += pv.Size.Y
	return layout.Dimensions{Size: image.Pt(w, y+dp(gtx, 8))}
}

// ceSelectBox is the medium Select's box: the label, a caret.
func ceSelectBox(gtx layout.Context, th *theme.Theme, cl *Colors, c *widget.Clickable, label string, w, h int, ring bool) layout.Dimensions {
	r := image.Rect(0, 0, w, h)
	rad := dp(gtx, 8)
	if ring {
		focusRing(gtx, cl, r, rad)
	}
	rrect(gtx, r, rad, dtBorder(th))
	rrect(gtx, r.Inset(border1(gtx)), rad-border1(gtx), cl.ModalBg)
	at(gtx, image.Pt(dp(gtx, 12), 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-dp(gtx, 40)), h)}
		return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 15, 22, theme.Regular, cl.Fg, label, 1)
		})
	})
	at(gtx, image.Pt(w-dp(gtx, 28), (h-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
		return icon(gtx, "caret-down", 16, cl.Fg2)
	})
	return clickBox(gtx, c, nil, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: r.Max} })
}

// preview is p-channel_email_address_modal__body_message_preview: the
// 40px icon tile, the bold name (or "Email"), "Email" in gray, and the
// message bar, the whole box masked from 30% down to transparent.
func (u *ceUpdate) preview(gtx layout.Context, th *theme.Theme, cl *Colors, w int) layout.Dimensions {
	pad := dp(gtx, 20)
	tile := dp(gtx, 40)
	nameH := dp(gtx, 22)
	innerH := nameH*2 + dp(gtx, 4) + dp(gtx, 36)
	h := maxInt(dp(gtx, 125), innerH+2*pad)
	top := dp(gtx, 12)
	box := image.Rect(0, 0, w, h)
	rrectTop(gtx, box, top, theme.WithAlpha(cl.Fg, 0x0a)) // foreground_min .04
	tbg := rgb(0xdddddd)                                  // foreground_low_solid
	if th.Dark {
		tbg = rgb(0x35373b)
	}
	rrect(gtx, image.Rect(pad, pad, pad+tile, pad+tile), dp(gtx, 8), tbg)
	at(gtx, image.Pt(pad, pad), func(gtx layout.Context) layout.Dimensions {
		defer clip.UniformRRect(image.Rect(0, 0, tile, tile), dp(gtx, 8)).Push(gtx.Ops).Pop()
		switch {
		case u.img != nil && u.img.URL != "":
			if img, ok := u.e.h.Images().Get(u.img.URL, tile); ok {
				return theme.Image(gtx, img, image.Pt(tile, tile), 0)
			}
		case u.emoji != "":
			es := dp(gtx, 24) // the emoji at MEDIUM
			return at(gtx, image.Pt((tile-es)/2, (tile-es)/2), func(gtx layout.Context) layout.Dimensions {
				return emojiImage(gtx, u.e.h, u.emoji, 24)
			})
		default:
			if img, ok := u.e.h.Images().Get(core.DefaultChannelEmailIcon, tile); ok {
				return theme.Image(gtx, img, image.Pt(tile, tile), 0)
			}
		}
		return layout.Dimensions{Size: image.Pt(tile, tile)}
	})
	x := pad + tile + dp(gtx, 8)
	nw := maxInt(1, w-x-pad)
	name := []rune(u.name.Text())
	if len(name) > 50 {
		name = name[:50]
	}
	label := string(name)
	if label == "" {
		label = nsChannelEmail.T("Email")
	}
	at(gtx, image.Pt(x, pad), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(nw, nameH)}
		return para(gtx, th, 15, 22, theme.Bold, cl.Fg, label, 1)
	})
	at(gtx, image.Pt(x, pad+nameH), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(nw, nameH)}
		return para(gtx, th, 15, 22, theme.Regular, cl.Subtle, nsChannelEmail.T("Email"), 1)
	})
	my := pad + 2*nameH + dp(gtx, 4)
	bar := image.Rect(x, my, x+nw, my+dp(gtx, 36))
	rrectTop(gtx, bar, top, tbg)
	rrectTop(gtx, bar.Inset(border1(gtx)), top-border1(gtx), cl.ModalBg)
	// mask-image: linear-gradient(#000 30%, #0000 100%)
	from := float32(h) * 0.3
	paint.LinearGradientOp{Stop1: f32.Pt(0, from), Stop2: f32.Pt(0, float32(h)),
		Color1: color.NRGBA{R: cl.ModalBg.R, G: cl.ModalBg.G, B: cl.ModalBg.B}, Color2: cl.ModalBg}.Add(gtx.Ops)
	st := clip.Rect(image.Rect(0, int(from), w, h)).Push(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	st.Pop()
	return layout.Dimensions{Size: box.Max}
}

// rrectTop fills r with only its top corners rounded.
func rrectTop(gtx layout.Context, r image.Rectangle, rad int, c color.NRGBA) {
	defer clip.RRect{Rect: r, NW: rad, NE: rad}.Push(gtx.Ops).Pop()
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// Update is the update pane (tests).
func (e *ChannelEmailModal) Update() *ceUpdate { return e.upd }

// SetName types the sender name (tests).
func (u *ceUpdate) SetName(s string) { u.name.SetText(s) }

// SetEmoji picks an emoji, as the picker's onEmojiSelected does.
func (u *ceUpdate) SetEmoji(name string) { u.img, u.emoji = nil, ":"+strings.Trim(name, ":")+":" }

// ---- crop ----

type ceCrop struct {
	cropBox
	e        *ChannelEmailModal
	m        *Modal
	back     widget.Clickable
	file     core.UploadFile
	name     string // nameInProgress
	up       *core.EmailIcon
	imgW     int
	imgH     int
	ready    bool
	saving   bool
	failed   bool
	uploaded bool
}

func newCECrop(e *ChannelEmailModal, f core.UploadFile, nameInProgress string) *ceCrop {
	c := &ceCrop{e: e, file: f, name: nameInProgress}
	c.h = e.h
	back := func() { e.showUpdate(c.name, nil) }
	save := &ModalButton{Label: nsEditProfile.T("Save"), Kind: BtnPrimary, Enabled: func() bool { return c.ready && !c.saving },
		Do: func() bool { c.Save(); return false }}
	c.m = NewModal(e.h, ModalOpts{Header: func(gtx layout.Context) layout.Dimensions {
		title := nsEditProfile.T("Uploading your photo…")
		if c.ready {
			title = nsEditProfile.T("Crop your photo")
		}
		return ceBackHeader(e.h, &c.back, title, back)(gtx)
	}, Width: 580, Body: c.layout, Buttons: []*ModalButton{save}, AutoFocus: save, OnClose: func() { e.done = true }})
	c.start()
	return c
}

// start is the pane's effect: avatars.upload {image}, then the cropper.
func (c *ceCrop) start() {
	a := c.e.h.Core()
	if a == nil {
		c.failed = true
		return
	}
	f := c.file
	type res struct {
		up  core.EmailIcon
		img image.Image
	}
	run(c.e.h, func(ctx context.Context) (res, error) {
		up, err := a.UploadEmailIcon(ctx, f.Name, f.Data)
		if err != nil {
			return res{}, err
		}
		img, _, err := image.Decode(bytes.NewReader(f.Data))
		if err != nil {
			return res{}, err
		}
		return res{up, img}, nil
	}, func(r res, err error) {
		if c.e.crop != c {
			return
		}
		if err != nil {
			c.failed = true
			c.e.h.Invalidate()
			return
		}
		b := r.img.Bounds()
		c.up, c.imgW, c.imgH = &r.up, b.Dx(), b.Dy()
		c.cropBox = newCropBox(c.e.h, r.img, core.EmailCropMinSize)
		c.ready = true
		c.e.h.Invalidate()
	})
}

// Ready reports the cropper is up (the upload answered).
func (c *ceCrop) Ready() bool { return c.ready }

// Save is avatars.crop with the square, then the update pane with the
// cropped icon.
func (c *ceCrop) Save() {
	a := c.e.h.Core()
	if a == nil || !c.ready || c.saving {
		return
	}
	c.saving, c.failed = true, false
	id := c.up.ID
	box := core.EmailCropBox(c.x, c.y, c.s, c.dw, c.dh, c.imgW, c.imgH)
	e := c.e
	run(e.h, func(ctx context.Context) (core.EmailIcon, error) {
		return a.CropEmailIcon(ctx, id, box)
	}, func(r core.EmailIcon, err error) {
		c.saving = false
		if err != nil {
			c.failed = true
			e.h.Invalidate()
			return
		}
		if r.ID == "" {
			r.ID = id
		}
		e.showUpdate(c.name, &r)
	})
}

// Crop is the crop pane (tests).
func (e *ChannelEmailModal) Crop() *ceCrop { return e.crop }

// CropBox is the cropbox Save sends.
func (c *ceCrop) CropBox() string {
	return core.EmailCropBox(c.x, c.y, c.s, c.dw, c.dh, c.imgW, c.imgH)
}

func (c *ceCrop) layout(gtx layout.Context) layout.Dimensions {
	th := c.e.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	y := dp(gtx, 5)
	if c.failed {
		h := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			hh := cnAlert(gtx, th, cl, true, []cnRun{{s: nsEditProfile.T("Sorry! Something went wrong. Please try again.")}}, w)
			return layout.Dimensions{Size: image.Pt(w, hh)}
		})
		y += h.Size.Y + dp(gtx, 16)
	}
	box := dp(gtx, 460)
	bx := maxInt(0, (w-box)/2)
	rrect(gtx, image.Rect(bx, y, bx+box, y+box), dp(gtx, 4), theme.WithAlpha(cl.Fg, 0x21))
	if !c.ready {
		s := dp(gtx, 32)
		at(gtx, image.Pt(bx+(box-s)/2, y+(box-s)/2), func(gtx layout.Context) layout.Dimensions {
			epSpinner(gtx, s, cl.Fg3)
			return layout.Dimensions{Size: image.Pt(s, s)}
		})
		return layout.Dimensions{Size: image.Pt(w, y+box)}
	}
	iw, ih := int(c.dw*float64(gtx.Metric.PxPerDp)+.5), int(c.dh*float64(gtx.Metric.PxPerDp)+.5)
	at(gtx, image.Pt(bx+(box-iw)/2, y+(box-ih)/2), func(gtx layout.Context) layout.Dimensions {
		c.drawCropper(gtx, iw, ih)
		return layout.Dimensions{Size: image.Pt(iw, ih)}
	})
	return layout.Dimensions{Size: image.Pt(w, y+box)}
}
