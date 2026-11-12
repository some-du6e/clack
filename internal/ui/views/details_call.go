package views

// Channel details' header "Start a Call ▾" (docs/GAPS-2026-11-11.md gap 14,
// W13): Slack's CallButton (use-slackbot-skills-tab chunk z2Iw, build
// d3de3a487b1c) in its useIaDetailsStyle, around WithCallMenu (boot-shared
// CXZJ) and CallMenu (GPML). The call apps and calls.request are
// core/chdetails_email.go.
//
//   - The button (getCallButtonUseIaDetailsStyle): "Start a Call" after the
//     phone glyph, or "Huddle" after headphones when the team has no call
//     apps (numCallApps 0); the chevron when it has more than one. Its
//     tooltip (on top, after 300ms) is getTipForStartCall and, unless the
//     button is disabled, getTipSubtitleForStartCall under it.
//   - isCallButtonVisibleForChannel (5R3h Xk) hides it; WithCallMenu
//     renders nothing without an app or a huddle to offer.
//   - One app and no huddle: a click starts that app's call. Otherwise it
//     opens CallMenu (305 wide, "Start a call"; bottom-left, offset -4,
//     -2): the huddle rows (HuddleButtonMenuItem and Copy huddle link,
//     from the ui root's huddles), "Video meetings" and the video apps,
//     and in a DM with a phone number a separator, "Phone {phone}" and the
//     audio apps.
//   - CallButton.onClick: the huddle row starts the huddle; anything but a
//     DM (or a conversation of 100+) asks first ("Start a call?",
//     getCallConfirmationStrings); then startCall (calls.request) and the
//     call's URL opens.
//
// Left out: the huddle discovery link ("New: Try a huddle…", an
// experiment shown only when the huddle row is hidden) and the frontline
// worker rule (frontline_users); Clack is always online, so the offline
// tooltip never shows.

import (
	"context"
	"image"
	"sync"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/popups"
)

var (
	nsChannelDetails = i18n.NS("channel_details") // CallButton, SendEmailsToChannelButton
	nsCalls          = i18n.NS("calls")           // CallMenu, 5R3h's tooltips and confirmation
)

// cdCall is the call button's state.
type cdCall struct {
	tip     popups.TipState
	size    image.Point     // the button as last drawn
	row     image.Rectangle // the header's button row, in the dialog
	dlg     image.Point     // the dialog's origin in the window
	menu    *popups.Menu
	confirm *popups.Modal
	mu      sync.Mutex
	opened  []string // the calls' URLs opened (tests)
}

// callHuddles is doesChannelSupportHuddles for the details' conversation,
// from the ui root's huddles (no huddles without one).
func (d *ChannelDetails) callHuddles() bool {
	if _, ok := d.h.(huddleCallMenu); !ok {
		return false
	}
	hs, ok := d.h.(interface{ SupportsHuddles(ch string) bool })
	return ok && hs.SupportsHuddles(d.id)
}

// huddleCallMenu is the ui root's CallMenu huddle rows for a channel.
type huddleCallMenu interface {
	HuddleCallMenu(ch string) []popups.MenuItem
}

// callMember is CallButton's member: the 1:1 DM's other person.
func (d *ChannelDetails) callMember() string {
	if cdGroup(d.kind) != "im" {
		return ""
	}
	if c, ok := d.h.Core().Conv(d.id); ok {
		return c.User
	}
	return ""
}

// callPhone is getMemberPhone(member).
func (d *ChannelDetails) callPhone() string {
	if m := d.callMember(); m != "" {
		return d.h.Core().MemberPhone(m)
	}
	return ""
}

// callApps is WithCallMenu's Q: getCallAppList(getCallsAppsFromTeamPref).
func (d *ChannelDetails) callApps() []core.CallApp {
	a := d.h.Core()
	return core.CallAppList(a.CallsAppsFor(d.id), d.callPhone(), false)
}

// callOffersHuddle is WithCallMenu's K with includeHuddle: a huddle with
// the member (canHuddleWithMember) or in the channel.
func (d *ChannelDetails) callOffersHuddle() bool {
	if !d.callHuddles() {
		return false
	}
	if m := d.callMember(); m != "" {
		u, ok := d.h.Core().User(m)
		return ok && !u.Deleted && m != d.h.Core().Self().ID
	}
	return true
}

// callVisible is isCallButtonVisibleForChannel (5R3h Xk, third party on)
// and WithCallMenu's "nothing to offer" null.
func (d *ChannelDetails) callVisible() bool {
	a := d.h.Core()
	c, ok := a.S.Conv(d.id)
	if !ok {
		return false
	}
	huddles := d.callHuddles()
	switch {
	case !a.CallsAllowed() && !huddles && !a.ThirdPartyCallEnabled():
		return false
	case c.IsFrozen:
		return false
	case c.IsIM && !a.CanCallMember(c.User, huddles):
		return false
	}
	// isUserInChannel and canUserPostInChannel
	inChannel := c.IsIM || c.IsMPIM || c.IsMember
	if !inChannel || c.IsArchived || !a.PostingFor(c).CanPost() {
		return false
	}
	return len(d.callApps()) > 0 || d.callOffersHuddle()
}

// callDisabled is isCallButtonDisabledForChannel (5R3h Rv): a preview or
// an archived conversation, unless it's a Slack Connect channel
// (shouldEnableForSponsoredConnections). Clack is always online.
func (d *ChannelDetails) callDisabled() bool {
	c, ok := d.h.Core().S.Conv(d.id)
	if !ok {
		return true
	}
	preview := !c.IsArchived && !c.IsIM && !c.IsMPIM && !c.IsMember
	sponsored := c.IsExtShared && !c.IsIM
	return (preview || c.IsArchived) && !sponsored
}

// callLabel is the button's glyph, label and chevron.
func (d *ChannelDetails) callLabel() (icon, label string, caret bool) {
	n := d.h.Core().NumCallApps()
	if n == 0 {
		return "headphones", nsChannelDetails.T("Huddle"), false
	}
	return "phone", nsChannelDetails.T("Start a Call"), n > 1
}

// callTip is getTipForStartCall and, when enabled, the subtitle.
func (d *ChannelDetails) callTip() []popups.TipLine {
	a := d.h.Core()
	c, ok := a.S.Conv(d.id)
	if !ok {
		return nil
	}
	r := !a.OnFreePlan() || a.ThirdPartyCallEnabled() // isPaidTeam || isThirdPartyCallEnabled
	preview := !c.IsArchived && !c.IsIM && !c.IsMPIM && !c.IsMember
	huddleOnly := d.callHuddles() && a.NumCallApps() == 0 // doesChannelSupportHuddles && !isAnyCallAppEnabled
	var tip string
	switch {
	case r && c.IsArchived:
		tip = nsCalls.T("Unarchive this channel to start a huddle")
	case r && preview:
		tip = nsCalls.T("Join this channel to start a huddle")
	case c.IsIM:
		name := d.userName(c.User)
		if huddleOnly {
			tip = nsCalls.T("Huddle with {name}", "name", name)
		} else {
			tip = nsCalls.T("Call {name}", "name", name)
		}
	case c.IsMPIM:
		cv, _ := a.Conv(d.id)
		_, names := d.mpdmOthers(cv.Members) // Opi6 d3, by name
		list := listify(names, false)
		if huddleOnly {
			tip = nsCalls.T("Huddle with {listOfNamesWithConjunction}", "listOfNamesWithConjunction", list)
		} else {
			tip = nsCalls.T("Call {listOfNamesWithConjunction}", "listOfNamesWithConjunction", list)
		}
	case huddleOnly:
		cv, _ := a.Conv(d.id)
		tip = nsCalls.T("Huddle in {channelName}", "channelName", "#"+cv.Name)
	default:
		tip = nsCalls.T("Start a call")
	}
	lines := []popups.TipLine{{Text: tip}}
	if !d.callDisabled() {
		if sub := d.callTipSubtitle(); sub != "" {
			lines = append(lines, popups.TipLine{Text: sub, Subtitle: true})
		}
	}
	return lines
}

// callTipSubtitle is getTipSubtitleForStartCall: "{n} calling options"
// counting the huddle, or "using {appName}" for a lone app.
func (d *ChannelDetails) callTipSubtitle() string {
	apps := d.callApps()
	n := len(apps)
	if d.callHuddles() {
		n++
	}
	switch {
	case n > 1:
		return nsCalls.T("{numCallApps} calling options", "numCallApps", n)
	case n == 1 && len(apps) == 1:
		return nsCalls.T("using {appName}", "appName", apps[0].Name)
	}
	return ""
}

// callButton draws the header's call button with its tooltip.
func (d *ChannelDetails) callButton(gtx C) D {
	icon, label, caret := d.callLabel()
	dims := cdBtn(&d.base, gtx, &d.call, icon, label, caret, 0)
	d.cl.size = dims.Size
	d.cl.tip.Delay = 300 * time.Millisecond
	popups.TooltipLines(gtx, d.th, &d.cl.tip, d.call.Hovered(), dims.Size, popups.Above, d.callTip())
	return dims
}

// callClicked is the button's click: WithCallMenu's menu, or the lone
// app's call.
func (d *ChannelDetails) callClicked() {
	if d.callDisabled() {
		return // CallButton.onClick returns while isDisabled
	}
	apps := d.callApps()
	if len(apps) == 1 && !d.callOffersHuddle() {
		d.callSelected(apps[0].ID, apps[0].Type)
		return
	}
	d.openCallMenu()
}

// callMenuItems is CallMenu's rows.
func (d *ChannelDetails) callMenuItems() []popups.MenuItem {
	a := d.h.Core()
	var items []popups.MenuItem
	if d.callOffersHuddle() { // addMenuItem for A01
		if hm, ok := d.h.(huddleCallMenu); ok {
			items = append(items, hm.HuddleCallMenu(d.id)...)
		}
	}
	apps := a.CallsAppsFor(d.id)
	phone := d.callPhone()
	isChannel := cdGroup(d.kind) == "channel"
	hideAudio := isChannel || !core.ValidPhone(phone)
	app := func(c core.CallApp, typ string) popups.MenuItem {
		img := core.CallAppImage(c)
		if img == "" {
			img = core.DefaultCallAppIcon
		}
		id := c.ID
		return popups.MenuItem{Key: typ + "-" + c.ID, Label: c.Name, IconURL: img, Do: func() { d.callSelected(id, typ) }}
	}
	if len(apps.Video) > 0 {
		items = append(items, popups.MenuItem{Header: nsCalls.T("Video meetings")})
		for _, c := range apps.Video {
			items = append(items, app(c, core.CallVideo))
		}
	}
	if !hideAudio && len(apps.Audio) > 0 {
		if len(apps.Video) > 0 {
			items = append(items, popups.Sep())
		}
		items = append(items, popups.MenuItem{Header: nsCalls.T("Phone {phone}", "phone", phone)})
		for _, c := range apps.Audio {
			items = append(items, app(c, core.CallAudio))
		}
	}
	return items
}

// openCallMenu opens CallMenu under the button (Popover bottom-left,
// offsetX -4, offsetY -2).
func (d *ChannelDetails) openCallMenu() {
	ph, ok := d.h.(popups.Host)
	if !ok {
		return
	}
	items := d.callMenuItems()
	if len(items) == 0 {
		return
	}
	at := d.callRect()
	d.cl.menu = popups.NewMenuOpts(ph, items, popups.MenuOpts{Width: 305, Anchor: at, Popover: "bottom-left",
		OffsetX: -4, OffsetY: -2})
	ph.ShowOverlay(d.cl.menu)
}

// callRect is the button in window coordinates as last drawn: the last
// of the header's button row.
func (d *ChannelDetails) callRect() image.Rectangle {
	at := d.cl.dlg.Add(image.Pt(d.cl.row.Max.X-d.cl.size.X, d.cl.row.Min.Y))
	return image.Rectangle{Min: at, Max: at.Add(d.cl.size)}
}

// CallRect is the call button in window coordinates (tests).
func (d *ChannelDetails) CallRect() image.Rectangle { return d.callRect() }

// callSelected is CallButton.onClick({appId, type, phoneNumber}).
func (d *ChannelDetails) callSelected(app, typ string) {
	phone := ""
	if cdGroup(d.kind) == "im" { // addMenuItem: phoneNumber only for a DM
		phone = d.callPhone()
	}
	a := d.h.Core()
	c, ok := a.S.Conv(d.id)
	if !ok {
		return
	}
	count := c.NumMembers
	if n, known := a.MemberCountCached(d.id); known {
		count = n
	}
	if count >= 100 || !c.IsIM {
		d.confirmCall(app, typ, phone, count)
		return
	}
	d.startCall(app, typ, phone)
}

// confirmCall is maybeRenderConfirmationModal: getCallConfirmationStrings
// in a centred modal, Cancel and Start Call.
func (d *ChannelDetails) confirmCall(app, typ, phone string, count int) {
	ph, ok := d.h.(popups.Host)
	if !ok {
		d.startCall(app, typ, phone)
		return
	}
	body := nsCalls.T("You’ll be starting a call that all {count} members of this channel can join.", "count", count)
	cancel := &popups.ModalButton{Label: nsCalls.T("Cancel"), Kind: popups.BtnOutline}
	start := &popups.ModalButton{Label: nsCalls.T("Start Call"), Kind: popups.BtnPrimary, Do: func() bool {
		d.startCall(app, typ, phone)
		return true
	}}
	d.cl.confirm = popups.NewModal(ph, popups.ModalOpts{Title: nsCalls.T("Start a call?"),
		Body:    func(gtx layout.Context) layout.Dimensions { return d.para(gtx, 15, 22, wReg, d.p.Text, 0, body) },
		Buttons: []*popups.ModalButton{cancel, start}})
	ph.ShowOverlay(d.cl.confirm)
}

// startCall is CallButton.startCall → startCall: calls.request, then the
// call's URL in the browser (or the app the OS hands it to). A failure is
// Slackbot's message in the conversation (core.StartCall).
func (d *ChannelDetails) startCall(app, typ, phone string) {
	a, ch, h := d.h.Core(), d.id, d.h
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		r, err := a.StartCall(ctx, ch, app, typ, phone)
		if err != nil {
			return
		}
		u := r.OpenURL(app)
		open := func() {
			d.cl.mu.Lock()
			d.cl.opened = append(d.cl.opened, u)
			d.cl.mu.Unlock()
			hostHandler{h}.OpenURL(u)
		}
		if p, ok := h.(interface{ Post(func()) }); ok {
			p.Post(open)
		} else {
			open()
		}
		h.Invalidate()
	}()
}

// ---- test hooks ----

// PressCall clicks the header's call button.
func (d *ChannelDetails) PressCall() { d.call.Click(); d.h.Invalidate() }

// CallButton reports the call button as drawn: visible, its label, the
// chevron, disabled.
func (d *ChannelDetails) CallButton() (visible bool, label string, caret, disabled bool) {
	if !d.callVisible() {
		return false, "", false, false
	}
	_, label, caret = d.callLabel()
	return true, label, caret, d.callDisabled()
}

// CallTip is the call button's tooltip lines.
func (d *ChannelDetails) CallTip() []string {
	var out []string
	for _, l := range d.callTip() {
		out = append(out, l.Text)
	}
	return out
}

// CallMenu is the open call menu (nil when none is open).
func (d *ChannelDetails) CallMenu() *popups.Menu {
	if d.cl.menu == nil || d.cl.menu.Dismissed() {
		return nil
	}
	return d.cl.menu
}

// CallConfirm is the open "Start a call?" modal.
func (d *ChannelDetails) CallConfirm() *popups.Modal {
	if d.cl.confirm == nil || d.cl.confirm.Dismissed() {
		return nil
	}
	return d.cl.confirm
}

// CallsOpened are the call URLs opened so far.
func (d *ChannelDetails) CallsOpened() []string {
	d.cl.mu.Lock()
	defer d.cl.mu.Unlock()
	return append([]string(nil), d.cl.opened...)
}
