package popups

import (
	"context"
	"image"
	"strconv"
	"strings"
	"unicode/utf8"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Content review flags, as Slack's web client has them (build
// 4d0c78085232):
//
//   - The More actions row (2rcC ed, last group, before Report to Slack,
//     which it replaces): someone's message (not a tombstone, moderated,
//     automated subtype without a code channel note, or redesigned huddle
//     message, unless it is a channel name, topic or description
//     message), on an Enterprise team or workspace (jDlU
//     isEnterpriseOrEnterpriseWorkspace) whose org locks
//     content_review_enabled on (8wTz getLockedOrgPref). A message from
//     another org needs a channel team that allows_flag_content for it
//     (fedg), which Clack doesn't know, so none is offered. "Flag this
//     message…" with the warning icon, red when highlighted
//     (.p-message_actions_menu__flag); a name, topic or description
//     message that still matches the channel says "Flag channel name" /
//     "Flag conversation name" (flag_custom_mpdm_names and a custom group
//     DM name), "Flag channel topic" / "Flag conversation topic" (DMs),
//     "Flag channel description".
//   - FlagMessageDialog (Pult) over FlagDialogModal (5GOt): the title, the
//     info text, "View your organization’s policies." (the org's
//     flag_message_custom_link), for channel content the content itself
//     and "Why are you flagging this?", the reason (FormTextareaInput,
//     560 characters with a counter while focused), and in the footer
//     "Report to Slack" (with content reporting) and Submit, off until
//     the reason has something. Submit opens the speed bump (bold
//     description, Cancel / Submit); its Submit calls
//     moderation.flags.create, then the dialog says "Message has been
//     flagged" with "Other steps you can take:" (Mute channel unless
//     muted, Leave channel outside DMs, for a member), or for channel
//     content closes. A failure shows "Sorry, something went wrong. Try
//     again?".

// flagMaxChars is FlagDialogModal's maxCharacterLimit.
const flagMaxChars = 560

// Experiments the flag row and dialog read.
const (
	ExpFlagCustomMpdmNames       = "flag_custom_mpdm_names"
	ExpFlagXorgRefinement        = "flag_xorg_refinement"
	ExpFlagXorgExternalTeamCheck = "flag_xorg_external_team_check"
)

// flagContentType is FlagMessageDialog's u: the channel detail a name,
// topic or description message flags while it still matches the channel
// (TuDs doesMessageContentMatchChannel), "" for a message.
func flagContentType(h Host, m core.Message, conv core.Conv) string {
	if !flagMatchesChannel(h, m, conv) {
		return ""
	}
	custom := expOn(h, ExpFlagCustomMpdmNames) && mpdmCustomName(h, conv)
	switch m.Subtype {
	case "channel_name":
		if custom {
			return "mpdm_name"
		}
		return "channel_name"
	case "channel_topic":
		return "channel_topic"
	case "channel_purpose":
		return "channel_description"
	}
	return ""
}

// flagMatchesChannel is doesMessageContentMatchChannel: the message's new
// name, topic or purpose is still the channel's.
func flagMatchesChannel(h Host, m core.Message, conv core.Conv) bool {
	switch m.Subtype {
	case "channel_name":
		return m.NewName != "" && m.NewName == conv.Name
	case "channel_topic":
		return m.Topic == conv.Topic
	case "channel_purpose":
		return m.Purpose == conv.Purpose
	}
	return false
}

// mpdmCustomName is tpcx JM: a group DM with a custom name, under
// mpdm_naming (and sc_mpdm_naming when it's shared).
func mpdmCustomName(h Host, conv core.Conv) bool {
	a := h.Core()
	if a == nil || conv.Kind != "mpim" {
		return false
	}
	_, ok := a.MpdmCustomName(conv.ID)
	return ok
}

// flagMember is JUrK isMemberOfChannel: the conversation's is_member.
func flagMember(h Host, ch string) bool {
	a := h.Core()
	if a == nil || a.S == nil {
		return false
	}
	c, ok := a.S.Conv(ch)
	return ok && c.IsMember
}

// flagRow is ed, the "Flag this message…" row.
func flagRow(h Host, m core.Message, conv core.Conv) (MenuItem, bool) {
	a := h.Core()
	if a == nil || m.TS == "" || conv.ID == "" {
		return MenuItem{}, false
	}
	topic, purpose := m.Topic != "", m.Purpose != ""
	detail := m.Subtype == "channel_name" || m.Subtype == "channel_topic" && topic || m.Subtype == "channel_purpose" && purpose
	if !detail && (m.Tombstone || m.Subtype == "tombstone" || m.Subtype == "moderated" || IsAutomatedSubtype(m.Subtype) ||
		m.Subtype == "huddle_thread" && a.Experiment("system_messages_redesign_m1") == "on") {
		return MenuItem{}, false
	}
	if flagExternal(h, m) {
		// the channel's teams that allows_flag_content for the author's
		// team (fedg s3): unknown to Clack, so as if none did
		return MenuItem{}, false
	}
	if !a.EnterpriseTeam() || !a.ContentReviewEnabled() {
		return MenuItem{}, false
	}
	label := "Flag this message…"
	if detail && flagMatchesChannel(h, m, conv) {
		custom := expOn(h, ExpFlagCustomMpdmNames) && mpdmCustomName(h, conv)
		switch {
		case m.Subtype == "channel_name" && custom:
			label = "Flag conversation name"
		case m.Subtype == "channel_name":
			label = "Flag channel name"
		case m.Subtype == "channel_topic" && (conv.Kind == "im" || conv.Kind == "mpim"):
			label = "Flag conversation topic"
		case m.Subtype == "channel_topic":
			label = "Flag channel topic"
		case m.Subtype == "channel_purpose":
			label = "Flag channel description"
		}
	}
	return MenuItem{Key: "flag_message", Label: label, Icon: "warning", DangerHighlight: true, Do: func() { OpenFlagMessageDialog(h, m) }}, true
}

// flagExternal is ed's n: the message comes from another org. With
// flag_xorg_refinement treatment and flag_xorg_external_team_check on,
// the author's enterprise (or team) isn't the viewer's; otherwise its
// source team's enterprise isn't (Clack reads the author's team for the
// source team).
func flagExternal(h Host, m core.Message) bool {
	a := h.Core()
	if a == nil || m.User == "" {
		return false
	}
	u, ok := lookupUser(h, m.User)
	if !ok {
		return false
	}
	org := a.EnterpriseID()
	team := u.EnterpriseID
	if team == "" {
		team = u.TeamID
	}
	if a.Experiment(ExpFlagXorgRefinement) == "treatment" && a.Experiment(ExpFlagXorgExternalTeamCheck) == "on" {
		return org != "" && team != "" && org != team
	}
	if u.TeamID == "" || u.TeamID == org || u.TeamID == a.Self().TeamID {
		return false
	}
	return !(org != "" && u.EnterpriseID == org)
}

// flagDialog is FlagMessageDialog.
type flagDialog struct {
	h      Host
	m      core.Message
	conv   core.Conv
	kind   string // the content type ("" for a message)
	modal  *Modal
	submit *ModalButton

	reason     widget.Editor
	reasonArea widget.Clickable
	policy     widget.Clickable // View your organization's policies.
	report     widget.Clickable // Report to Slack
	mute       widget.Clickable
	leave      widget.Clickable
	veil       int // the speed bump's pointer shield

	bump             bool // the speed bump is up
	bumpCancel       *ModalButton
	bumpGo           *ModalButton
	sending          bool // D: the call is out
	failed           bool // B: it failed
	done             bool // H: it went through
	muting, leaving  bool
	focusIn, focused bool
}

// OpenFlagMessageDialog opens FlagMessageDialog for m (flagMessage), when
// the org has content review.
func OpenFlagMessageDialog(h Host, m core.Message) *Modal {
	a := h.Core()
	if a == nil || !a.ContentReviewEnabled() {
		return nil
	}
	conv, _ := a.Conv(m.Channel)
	if conv.ID == "" {
		conv.ID, conv.Kind = m.Channel, actKindOf(m.Channel)
	}
	f := &flagDialog{h: h, m: m, conv: conv, kind: flagContentType(h, m, conv), focusIn: true}
	f.bumpCancel = &ModalButton{Label: "Cancel", Kind: BtnOutline}
	f.bumpGo = &ModalButton{Label: "Submit", Kind: BtnPrimary}
	// Submit (oG6i): off without a reason or while the call is out; under
	// the speed bump the footer is inert (the veil takes the pointer)
	f.submit = &ModalButton{Label: "Submit", Kind: BtnPrimary, Enabled: func() bool { return f.valid() && !f.sending },
		Do: func() bool {
			if f.valid() {
				f.bump = true // showSpeedBump(true)
				f.focused = false
			} else {
				f.focusIn = true
			}
			return false
		}}
	f.modal = NewModal(h, ModalOpts{
		TitleFunc:  f.title,
		Body:       f.body,
		Buttons:    []*ModalButton{f.submit},
		FooterPad:  24,
		FooterLeft: f.reportLink,
		Over:       f.speedBump,
		FocusStops: f.stops,
		CloseGuard: func() bool {
			if f.bump { // the speed bump's Escape cancels it
				f.bump = false
				f.focusIn = true
				return false
			}
			return true
		},
	})
	h.ShowOverlay(f.modal)
	return f.modal
}

// valid is V: a reason, at most 560 characters, once trimmed.
func (f *flagDialog) valid() bool {
	n := utf8.RuneCountInString(strings.TrimSpace(f.reason.Text()))
	return n > 0 && n <= flagMaxChars
}

func (f *flagDialog) dm() bool { return f.conv.Kind == "im" || f.conv.Kind == "mpim" }

// customMpdm is FlagMessageDialog's l (flag_custom_mpdm_names).
func (f *flagDialog) customMpdm() bool { return expOn(f.h, ExpFlagCustomMpdmNames) }

func (f *flagDialog) title() string {
	if f.done {
		if f.kind != "" {
			return "Channel content has been flagged"
		}
		return "Message has been flagged"
	}
	switch f.kind {
	case "channel_name":
		return "Flag channel name"
	case "channel_topic":
		if f.dm() {
			return "Flag conversation topic"
		}
		return "Flag channel topic"
	case "channel_description":
		return "Flag channel description"
	case "mpdm_name":
		if f.customMpdm() {
			return "Flag conversation name"
		}
		return ""
	}
	return "Flag this message"
}

// info is the dialog's first paragraph (O).
func (f *flagDialog) info() string {
	switch f.kind {
	case "channel_name":
		return "Report the channel name to your organization’s Content Admins. Content Admins can see your name and your reason for flagging. Others won’t know you’ve flagged the name."
	case "channel_topic":
		if f.dm() {
			return "Report the conversation topic to your organization’s Content Admins. Content Admins can see your name and your reason for flagging. Others won’t know you’ve flagged the topic."
		}
		return "Report the channel topic to your organization’s Content Admins. Content Admins can see your name and your reason for flagging. Others won’t know you’ve flagged the topic."
	case "channel_description":
		return "Report the channel description to your organization’s Content Admins. Content Admins can see your name and your reason for flagging. Others won’t know you’ve flagged the description."
	case "mpdm_name":
		if f.customMpdm() {
			return "Report the conversation name to your organization’s Content Admins. Content Admins can see your name and your reason for flagging. Others won’t know you’ve flagged the name."
		}
		return ""
	}
	return "Report this message to your organization’s Content Admins. Content Admins can see your name and the details you add. Others won’t know you’ve flagged this message."
}

// bumpText is the speed bump's two paragraphs (E).
func (f *flagDialog) bumpText() []string {
	const review = "Your organization’s Content Admins will review your report and have the option to take action based on your organization’s policies."
	const topic = "Are you sure you want to flag this conversation topic as inappropriate?"
	if f.kind == "" {
		return []string{"Your organization’s Content Admins will review your report and have the option to hide this message based on your organization’s policies. ",
			"Are you sure you want to flag this message as inappropriate?"}
	}
	if !f.customMpdm() {
		return []string{review, topic}
	}
	q := ""
	switch f.kind {
	case "channel_name":
		q = "Are you sure you want to flag this channel name as inappropriate?"
	case "channel_topic":
		q = "Are you sure you want to flag this channel topic as inappropriate?"
		if mpdmCustomName(f.h, f.conv) { // c: a custom group DM name
			q = topic
		}
	case "channel_description":
		q = "Are you sure you want to flag this channel description as inappropriate?"
	case "mpdm_name":
		q = "Are you sure you want to flag this conversation name as inappropriate?"
	}
	return []string{review, q}
}

func (f *flagDialog) placeholder() string {
	if f.kind != "" {
		return "Please provide details"
	}
	return "I’d like you to review this inappropriate message because…"
}

// customLink is b: the org's flag_message_custom_link, for messages.
func (f *flagDialog) customLink() string {
	if f.kind != "" {
		return ""
	}
	if a := f.h.Core(); a != nil {
		return a.FlagCustomLink()
	}
	return ""
}

// stops is the body's tab order: the reason, or the speed bump's
// buttons while it is up (the footer is inert under it).
func (f *flagDialog) stops() []event.Tag {
	if f.bump {
		return []event.Tag{&f.bumpCancel.click, &f.bumpGo.click}
	}
	if f.done {
		return nil
	}
	return []event.Tag{&f.reason}
}

// send is the speed bump's Submit (Y): moderation.flags.create, then the
// submitted state, or the error alert.
func (f *flagDialog) send() {
	a := f.h.Core()
	if a == nil || f.sending {
		return
	}
	f.sending, f.failed, f.bump = true, false, false
	ch, ts, kind, reason := f.m.Channel, f.m.TS, f.kind, f.reason.Text()
	run(f.h, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, a.FlagContent(ctx, ch, ts, kind, reason)
	}, func(_ struct{}, err error) {
		f.sending = false
		if err != nil {
			f.failed, f.focusIn = true, true
			return
		}
		f.done = true
		f.modal.o.Buttons = nil // no footer once submitted (!H)
		f.modal.o.FooterLeft = nil
		if kind != "" {
			f.modal.Close() // H && channel_details: the dialog goes
		}
	})
}

func (f *flagDialog) body(gtx layout.Context) layout.Dimensions {
	if f.modal.done {
		return layout.Dimensions{}
	}
	th := f.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	if f.done {
		return f.submitted(gtx, th, cl)
	}
	if f.focusIn && !f.bump {
		gtx.Execute(key.FocusCmd{Tag: &f.reason})
		f.focusIn = false
	}
	for {
		if _, ok := f.reason.Update(gtx); !ok {
			break
		}
	}
	if f.reasonArea.Clicked(gtx) {
		gtx.Execute(key.FocusCmd{Tag: &f.reason})
	}
	if f.policy.Clicked(gtx) {
		if l := f.customLink(); l != "" {
			lbOpen(f.h, l)
		}
	}
	y := 0
	put := func(wd layout.Widget) int {
		call, d := record(loose(gtx, image.Pt(w, 1<<20)), wd)
		macroAt(gtx, call, image.Pt(0, y))
		return d.Size.Y
	}
	if s := f.info(); s != "" {
		y += put(func(gtx layout.Context) layout.Dimensions { return para(gtx, th, 15, 22, theme.Regular, cl.Fg, s, 0) })
	}
	if l := f.customLink(); l != "" {
		y += dp(gtx, 16) // p: margin-bottom 16px (sk.base)
		y += put(func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, &f.policy, nil, func(gtx layout.Context) layout.Dimensions {
				return linkWithNewWindow(gtx, th, cl, "View your organization’s policies.")
			})
		})
	}
	if f.kind != "" {
		// FlagMessageWithChannelSubtype (mVzX), margin_top_150
		if c := f.channelContent(); c != "" {
			y += dp(gtx, 24)
			y += put(func(gtx layout.Context) layout.Dimensions { return f.contentBox(gtx, th, cl, c) })
		}
		// the legend ("Why are you flagging this?"), margin_top_150
		y += dp(gtx, 24)
		y += put(func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 15, 22, theme.Bold, cl.Fg, "Why are you flagging this?", 0)
		})
		y += dp(gtx, 4) + dp(gtx, 8) // c-legend margin-bottom 4, then margin_top_50
	} else {
		y += dp(gtx, 24) // an empty legend, then margin_top_150
	}
	y += put(func(gtx layout.Context) layout.Dimensions { return f.textarea(gtx, th, cl) })
	y += dp(gtx, 20) // c-input_textarea__wrapper margin-bottom 20
	if f.failed {
		y += put(func(gtx layout.Context) layout.Dimensions {
			return srAlert(gtx, th, cl, "Sorry, something went wrong. Try again?", w)
		})
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// channelContent is what mVzX shows: the channel's name, topic or
// description.
func (f *flagDialog) channelContent() string {
	switch f.kind {
	case "channel_name", "mpdm_name":
		if f.conv.Kind == "im" { // getChannelName: none for DMs
			return ""
		}
		return f.conv.Name
	case "channel_topic":
		return f.conv.Topic
	case "channel_description":
		return f.conv.Purpose
	}
	return ""
}

// contentBox is .p-flag_dialog_channel_content_dialog__channel_*:
// base-sec, radius 8, padding 8 12; the name after a 20px channel icon
// and 12px.
func (f *flagDialog) contentBox(gtx layout.Context, th *theme.Theme, cl *Colors, s string) layout.Dimensions {
	w := gtx.Constraints.Max.X
	px, py := dp(gtx, 12), dp(gtx, 8)
	x := px
	if f.kind == "channel_name" {
		x += dp(gtx, 20) + dp(gtx, 12)
	}
	call, d := record(loose(gtx, image.Pt(maxInt(1, w-x-px), 1<<20)), func(gtx layout.Context) layout.Dimensions {
		return para(gtx, th, 15, 22, theme.Regular, cl.Fg, s, 0)
	})
	h := d.Size.Y + 2*py
	rrect(gtx, image.Rect(0, 0, w, h), dp(gtx, 8), cl.BgAlt)
	if f.kind == "channel_name" {
		at(gtx, image.Pt(px, py+(dp(gtx, 22)-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, "channel", 20, cl.Fg)
		})
	}
	macroAt(gtx, call, image.Pt(x, py))
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// textarea is FormTextareaInput (.c-input_textarea: 104px, radius 8,
// padding 8 12 4, 15px on 1.46667 lines) with its CharacterCount: while
// focused, the characters left at the top right (13px, content-sec,
// padding 8 12), the text making room for it.
func (f *flagDialog) textarea(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	w := gtx.Constraints.Max.X
	h := dp(gtx, 104)
	focused := gtx.Focused(&f.reason)
	box := image.Rect(0, 0, w, h)
	srInputBox(gtx, cl, box, focused)
	f.reasonArea.Layout(exact(gtx, box.Max), func(gtx layout.Context) layout.Dimensions {
		defer clip.Rect(box).Push(gtx.Ops).Pop()
		pointer.CursorText.Add(gtx.Ops)
		return layout.Dimensions{Size: box.Max}
	})
	padR := dp(gtx, 12)
	left := flagMaxChars - utf8.RuneCountInString(f.reason.Text())
	if focused {
		n := strconv.Itoa(left)
		cw := textW(gtx, th, 13, theme.Regular, n) + 2*dp(gtx, 12)
		padR = cw // padding-right: the counter's width
		col := cl.Fg2
		if left < 0 {
			col = cl.Danger // c-input_character_count--invalid: content-imp
		}
		at(gtx, image.Pt(w-cw+dp(gtx, 12), dp(gtx, 8)), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, dp(gtx, 18), func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, col, n)
			})
		})
	}
	tw := maxInt(1, w-dp(gtx, 12)-padR)
	at(gtx, image.Pt(dp(gtx, 12), dp(gtx, 8)), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Min: image.Pt(tw, 0), Max: image.Pt(tw, h-dp(gtx, 12))}
		if f.reason.Len() == 0 {
			call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min = image.Point{}
				return para(gtx, th, 15, 22, theme.Regular, cl.Placeholder, f.placeholder(), 1)
			})
			call.Add(gtx.Ops)
		}
		return th.Editor(gtx, &f.reason, unit.Sp(15), cl.Fg, theme.WithAlpha(cl.Focus, 0x55))
	})
	return layout.Dimensions{Size: box.Max}
}

// reportLink is the footer's "Report to Slack" (with the new-window
// icon) when content reporting is on: reportMessage (ceUl).
func (f *flagDialog) reportLink(gtx layout.Context) layout.Dimensions {
	if !actReporting(f.h) {
		return layout.Dimensions{}
	}
	if f.report.Clicked(gtx) {
		link := f.m.Permalink
		if link == "" {
			link = permalink(f.h, f.m.Channel, f.m.TS, f.m.ThreadTS)
		}
		reportMessage(f.h, link)
	}
	th := f.h.Theme()
	return clickBox(gtx, &f.report, nil, func(gtx layout.Context) layout.Dimensions {
		return linkWithNewWindow(gtx, th, colorsOf(th), "Report to Slack")
	})
}

// linkWithNewWindow is a c-link and its inline new-window icon, 4px after
// it (.p-flag_dialog__open_link_icon).
func linkWithNewWindow(gtx layout.Context, th *theme.Theme, cl *Colors, s string) layout.Dimensions {
	lh := dp(gtx, 22)
	tw := textW(gtx, th, 15, theme.Regular, s)
	at(gtx, image.Point{}, func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 15, theme.Regular, cl.Link, s) })
	})
	x := tw + dp(gtx, 4)
	at(gtx, image.Pt(x, (lh-dp(gtx, 15))/2), func(gtx layout.Context) layout.Dimensions {
		return icon(gtx, "new-window", 15, cl.Link)
	})
	sz := image.Pt(x+dp(gtx, 15), lh)
	pointerCursor(gtx, sz)
	return layout.Dimensions{Size: sz}
}

// submitted is FlagMessageDialogFormSubmitted over FlagDialogFormSubmitted:
// the thanks, "Other steps you can take:" (13px, the max foreground, 24px
// above) and, for a member of the conversation, Mute channel (unless
// muted) and Leave channel (outside DMs), each a link-blue 20px icon in a
// 36px column and a bold link-blue title over its description. Muting or
// leaving closes the dialog once it's done.
func (f *flagDialog) submitted(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	w := gtx.Constraints.Max.X
	muted := cxMuted(f.h, f.conv.ID)
	member := flagMember(f.h, f.conv.ID)
	if f.muting && muted || f.leaving && !member {
		f.modal.Close()
		return layout.Dimensions{}
	}
	if f.mute.Clicked(gtx) && !f.muting {
		f.muting = true
		ch := f.conv.ID
		actDo(f.h, "", func(ctx context.Context, a *core.App) error { return cxMute(ctx, f.h, ch, true) })
	}
	if f.leave.Clicked(gtx) && !f.leaving {
		f.leaving = true
		c := f.conv
		name := convTitle(f.h, c)
		if name == "" {
			name = c.ID
		}
		leaveChannel(f.h, c.ID, c.Kind, name, c.IsGeneral)
	}
	y := 0
	put := func(wd layout.Widget) int {
		call, d := record(loose(gtx, image.Pt(w, 1<<20)), wd)
		macroAt(gtx, call, image.Pt(0, y))
		return d.Size.Y
	}
	y += put(func(gtx layout.Context) layout.Dimensions {
		return para(gtx, th, 15, 22, theme.Regular, cl.Fg,
			"Thanks for reaching out to your organization. Someone may be in touch if they have any questions.", 0)
	})
	y += dp(gtx, 16) // p margin-bottom 16 (sk.base)
	type opt struct {
		c                 *widget.Clickable
		icon, title, desc string
		busy              bool
	}
	var opts []opt
	if member {
		if !muted {
			opts = append(opts, opt{&f.mute, "notifications-off", "Mute channel", "Take a break from getting notified about new messages", f.muting})
		}
		if !f.dm() {
			opts = append(opts, opt{&f.leave, "sign-out", "Leave channel", "", f.leaving})
		}
	}
	y += dp(gtx, 24) - dp(gtx, 16) // the subhead's margin-top 24 (collapsing with the p's 16)
	y += put(func(gtx layout.Context) layout.Dimensions {
		return para(gtx, th, 13, 18, theme.Bold, cl.Muted, "Other steps you can take:", 0)
	})
	for _, o := range opts {
		o := o
		y += dp(gtx, 16) // .p-flag_dialog__option margin-top 16
		y += put(func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, o.c, nil, func(gtx layout.Context) layout.Dimensions {
				ix := dp(gtx, 36)
				at(gtx, image.Pt(0, dp(gtx, 2)), func(gtx layout.Context) layout.Dimensions {
					return icon(gtx, o.icon, 20, cl.Link)
				})
				tw := maxInt(1, w-ix)
				tc, td := record(loose(gtx, image.Pt(tw, 1<<20)), func(gtx layout.Context) layout.Dimensions {
					return para(gtx, th, 15, 22, theme.Bold, cl.Link, o.title, 0)
				})
				macroAt(gtx, tc, image.Pt(ix, 0))
				hh := td.Size.Y
				if o.desc != "" {
					dc, dd := record(loose(gtx, image.Pt(tw, 1<<20)), func(gtx layout.Context) layout.Dimensions {
						return para(gtx, th, 15, 22, theme.Regular, cl.Muted, o.desc, 0)
					})
					macroAt(gtx, dc, image.Pt(ix, hh))
					hh += dd.Size.Y
				}
				sz := image.Pt(w, maxInt(hh, dp(gtx, 22)))
				pointerCursor(gtx, sz)
				return layout.Dimensions{Size: sz}
			})
		})
	}
	return layout.Dimensions{Size: image.Pt(w, y+dp(gtx, 48))} // margin_bottom_300
}

// speedBump is the c-modal_speed_bump over the dialog while it is up: the
// card veiled at 70% of the primary background, and the sheet at its
// bottom (base-pry, a 1px otl-ter top line, radius 0 0 8 8, padding 20
// 24) with the bold description (.p-flag_dialog__speedbump_description)
// 24px over Cancel and Submit (12px apart, at the right). Cancel has the
// focus (no autoFocusContinueButton).
func (f *flagDialog) speedBump(gtx layout.Context) {
	if !f.bump {
		return
	}
	th := f.h.Theme()
	cl := colorsOf(th)
	if f.bumpCancel.click.Clicked(gtx) {
		f.bump, f.focusIn = false, true
		return
	}
	if f.bumpGo.click.Clicked(gtx) {
		f.send()
		return
	}
	if !f.focused {
		f.focused = true
		f.modal.focusTo(gtx, &f.bumpCancel.click, true)
	}
	sz := gtx.Constraints.Max
	func() {
		rad := dp(gtx, 8)
		defer clip.RRect{Rect: image.Rectangle{Max: sz}, NE: rad, NW: rad, SE: rad, SW: rad}.Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &f.veil)
		for {
			if _, ok := gtx.Event(pointer.Filter{Target: &f.veil, Kinds: pointer.Press | pointer.Release | pointer.Scroll,
				ScrollY: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20}}); !ok {
				break
			}
		}
		fill(gtx, image.Rectangle{Max: sz}, theme.WithAlpha(cl.ModalBg, 179)) // .7
	}()
	px, py := dp(gtx, 24), dp(gtx, 20)
	iw := sz.X - 2*px
	var lines []layout.Widget
	for _, p := range f.bumpText() {
		p := p
		lines = append(lines, func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 15, 22, theme.Bold, cl.Fg, strings.TrimSpace(p), 0)
		})
	}
	textCall, td := record(loose(gtx, image.Pt(iw, 1<<20)), func(gtx layout.Context) layout.Dimensions {
		y := 0
		for i, l := range lines {
			if i > 0 {
				y += dp(gtx, 16) // p margin-bottom 16
			}
			c, d := record(loose(gtx, image.Pt(iw, 1<<20)), l)
			macroAt(gtx, c, image.Pt(0, y))
			y += d.Size.Y
		}
		return layout.Dimensions{Size: image.Pt(iw, y)}
	})
	bh := dp(gtx, 36)
	sheetH := py + td.Size.Y + dp(gtx, 24) + bh + py
	top := sz.Y - sheetH
	sheet := image.Rect(0, top, sz.X, sz.Y)
	rad := dp(gtx, 8)
	func() {
		defer clip.RRect{Rect: sheet, SE: rad, SW: rad}.Push(gtx.Ops).Pop()
		fill(gtx, sheet, cl.ModalBg)
	}()
	fill(gtx, image.Rect(0, top, sz.X, top+maxInt(1, dp(gtx, 1))), theme.OtlTer(th))
	macroAt(gtx, textCall, image.Pt(px, top+py))
	x := sz.X - px
	by := top + py + td.Size.Y + dp(gtx, 24)
	for _, b := range []*ModalButton{f.bumpGo, f.bumpCancel} {
		b := b
		ring := f.modal.ringOn(gtx, &b.click)
		call, d := record(gtx, func(gtx layout.Context) layout.Dimensions {
			return button(gtx, th, &b.click, b.Kind, BtnMedium, b.Label, "", 80, ring)
		})
		x -= d.Size.X
		macroAt(gtx, call, image.Pt(x, by))
		x -= dp(gtx, 12)
	}
}
