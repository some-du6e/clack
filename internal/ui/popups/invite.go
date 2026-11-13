package popups

// Slack's invite modal, "Invite people to {workspace}": the team menu's
// row, the Create menu's "Invite people" and the Huddles page's "Add
// coworkers" open it (openInviteModal, gantry-v2-async-client-boot-shared
// module CJ8h, build 4d0c78085232; the rules and the API are
// core/invite.go, the email field's logic invite_tokens.go;
// docs/INVITES.md).
//
// The form is InviteForm as Enterprise Grid draws it (Hack Club is on
// Grid; off Grid Slack draws the simplified form, wfeE m), top down:
//
//   - the title: "Invite people to {workspaceName}", or "Request
//     invitations to {workspaceName}" when the send goes to an admin
//     (isRequestingInvites);
//   - "To:" and TokenizerEmailInput: typed or pasted addresses become
//     tokens on a separator, a space, Enter or blur; a bad token is
//     dashed red with its reason on hover ("Invalid email address",
//     "Duplicate email", "Already in this workspace", "You can’t invite
//     yourself"), and under the field each kind of error gets a line with
//     a "Remove …" link (MultiSelectErrorText, hSb/); the placeholder is
//     name@{the viewer's email domain};
//   - "Invite as:" Member / Guest (InviteTypeSelect, paid teams; Guest only
//     for who may invite guests or request it);
//   - "Add to team channels" (ChannelSelect: optional for members,
//     required for guests, whose one or several channels make them
//     single- or multi-channel guests), with the "Suggested:" row above
//     the field (GXKR: conversations.recommend on mount, the first three
//     not picked yet as informative Tags; three shimmer tags while it
//     loads, nothing when it has none);
//   - a guest's "Set an expiration date" (GuestExpirationSelect): No
//     limit, 7, 30 or 60 days, or Custom, which adds the outline
//     calendar button ("Deactivate on {date}" once a day is picked) over
//     the date picker (tomorrow up to ten years out);
//   - "Add a message" (CustomizeButton), which opens "Custom message:"
//     (CustomMessage, at most 560 characters);
//   - for a request, "Reason for request (optional)" and the note "Your
//     request will be sent to your admins, and you’ll be notified when
//     it’s approved or denied." (ReasonForRequest);
//   - a failed send's error banner (InviteFormError, invite-errors'
//     getInviteErrorText);
//   - the footer: "Copy invite link" (SharedInviteLink) when the team has
//     links (not on Grid, not a request, not a guest invite), "Learn more
//     about guests" for a guest invite, and Send / Send Request.
//
// After the send, ConfirmationScreen (boot-shared /oFV) and its
// InvitesSummaryList (kWXe): "Sent" / "Unable to send all" / "Unable to
// send", each address with its outcome, Invite More People and Done.
//
// Not drawn: the Google Workspace directory (it needs Google sign-in), the
// Slack Connect choice under "Invite as", the org's max_guest_duration
// limit on the expiration, the email preview and the invite link's
// settings form.

import (
	"context"
	"errors"
	"image"
	"image/color"
	"sort"
	"strings"
	"time"

	"gioui.org/gesture"
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

// The modal's I18n instances: CJ8h's "invite-to-workspace" (the form) and
// "invite" (TokenizerEmailInput), kWXe's "add-to-slack" (the summary),
// kqqh's "invite-errors" and hSb/'s "form_inputs".
var (
	nsInviteWS     = i18n.NS("invite-to-workspace")
	nsInvite       = i18n.NS("invite")
	nsAddToSlack   = i18n.NS("add-to-slack")
	nsInviteErrors = i18n.NS("invite-errors")
)

// Invite modal entry points (Smwf K3), sent as the invite's campaign.
const (
	InviteFromTeamMenu   = "team_menu"
	InviteFromCreateMenu = "sidebar_create_new_invite_people"
	InviteFromHuddles    = "huddles_page"
	// Directories > People: InvitePeopleButton and the education banner's
	// "Invite people" (client-browse-people-view, build fa938a086569)
	InviteFromPeopleBrowser     = "people_browser"
	InviteFromPeopleBrowserHero = "people_browser_hero"
)

// CanInvite is canUserSeeInviteModal: whether the entry points show.
func CanInvite(h Host) bool {
	a := h.Core()
	return a != nil && a.CanSeeInviteModal()
}

// invChip is a token's remove button, hover and tooltip.
type invChip struct {
	rm  widget.Clickable
	hv  gesture.Hover
	tip TipState
}

// InviteModal is the dialog.
type InviteModal struct {
	h      Host
	modal  *Modal
	source string
	team   string // the workspace's name
	grid   bool   // Enterprise Grid
	paid   bool   // !isOnFreePlan
	self   string // the viewer's email (creatorEmail)

	// the email field
	emails  []string
	to      widget.Editor
	toBox   widget.Clickable
	toFocus bool // focus the field next frame
	wasIn   bool // the field had the focus last frame (blur commits)
	chips   []*invChip
	fix     [5]widget.Clickable // the error lines' Remove links: invalid, existing, self, duplicate, over limit

	// Invite as
	typ    core.InviteType
	typBtn widget.Clickable
	typOv  Overlay

	// channels
	chans   []core.Conv
	chQ     widget.Editor
	chBox   widget.Clickable
	chMatch []core.Conv
	chQuery string
	chHL    int
	chSugg  [6]widget.Clickable
	chRm    []*widget.Clickable
	forceMC bool // the single channel guest's "Allow them to be added to more channels"
	mcBox   widget.Clickable

	// a guest's expiration (days; 0: no limit; invExpCustom: Custom, on
	// expDate, local midnight, zero until a day is picked)
	expDays int
	expDate time.Time
	expBtn  widget.Clickable
	expOv   Overlay
	expCal  widget.Clickable // Custom's calendar button
	calOv   Overlay

	// the recommended channels (GXKR)
	recLoading bool
	rec        []core.Conv
	recBtn     [3]widget.Clickable

	// Add a message / Custom message
	custom    widget.Clickable
	showMsg   bool
	msg       widget.Editor
	msgBox    widget.Clickable
	reason    widget.Editor
	reasonBox widget.Clickable

	// the invite link
	link       core.SharedInviteLink
	haveLink   bool
	linkBusy   bool // listing or minting
	linkCopied bool
	linkBtn    widget.Clickable
	linkTip    TipState
	linkTipUp  bool
	copyNext   string // copied on the next frame (the clipboard needs a gtx)

	learn widget.Clickable

	formErr string
	sending bool
	send    *ModalButton
}

// OpenInviteModal opens the dialog (openInviteModal with source).
func OpenInviteModal(h Host, source string) *InviteModal {
	return openInvite(h, source, nil)
}

func openInvite(h Host, source string, chans []core.Conv) *InviteModal {
	m := &InviteModal{h: h, source: source, typ: core.InviteMember, toFocus: true, chans: chans}
	m.to.Submit = true
	m.chQ.SingleLine, m.chQ.Submit = true, true
	m.msg.MaxLen = 560
	m.reason.SingleLine, m.reason.MaxLen = true, 560
	if a := h.Core(); a != nil {
		m.grid = a.IsEnterprise()
		m.paid = !a.OnFreePlan()
		m.self = strings.ToLower(strings.TrimSpace(a.Self().Email))
	}
	if snap, ok := actSnapshot(h); ok {
		m.team = strings.TrimSpace(snap.Team.Name)
	}
	m.send = &ModalButton{Kind: BtnPrimary, Enabled: m.CanSend, Do: func() bool { m.Send(); return false }}
	m.modal = NewModal(h, ModalOpts{TitleFunc: m.Title, Width: 650, Body: m.layout, Buttons: []*ModalButton{m.send},
		FooterLeft: m.footerLeft,
		FocusStops: func() []event.Tag { return []event.Tag{&m.to, &m.chQ, &m.msg, &m.reason} }})
	h.ShowOverlay(m)
	if m.linkShown() {
		m.fetchLink()
	}
	m.fetchRecommended()
	return m
}

// fetchRecommended is useRecommendedChannels' fetch on mount
// (fetchRecommendedChannels, limit 10).
func (m *InviteModal) fetchRecommended() {
	if m.h.Core() == nil {
		return
	}
	m.recLoading = true
	run(m.h, func(ctx context.Context) ([]core.Conv, error) {
		a, err := cxApp(m.h)
		if err != nil {
			return nil, err
		}
		return a.RecommendedChannels(ctx, 10)
	}, func(cs []core.Conv, err error) {
		m.recLoading = false
		if err == nil {
			m.rec = cs
		}
		m.h.Invalidate()
	})
}

// Suggested is the row's channels: the first three recommended ones not
// picked yet (recommendedChannels' filter and slice(0, 3)).
func (m *InviteModal) Suggested() []core.Conv {
	picked := map[string]bool{}
	for _, c := range m.chans {
		picked[c.ID] = true
	}
	var out []core.Conv
	for _, c := range m.rec {
		if len(out) == 3 {
			break
		}
		if !picked[c.ID] {
			out = append(out, c)
		}
	}
	return out
}

// AddSuggested is a suggested tag's onChannelClick:
// setSelectedChannelOptions([...selected, channel]). The search text
// stays; ChannelSelect's effect re-derives a guest's type.
func (m *InviteModal) AddSuggested(c core.Conv) {
	for _, x := range m.chans {
		if x.ID == c.ID {
			return
		}
	}
	m.chans = append(m.chans, c)
	m.chQuery = "\x00"
	m.applyGuestChannels()
}

// RecommendedLoading reports the row's shimmer.
func (m *InviteModal) RecommendedLoading() bool { return m.recLoading }

func (m *InviteModal) Layout(gtx layout.Context) layout.Dimensions { return m.modal.Layout(gtx) }
func (m *InviteModal) Dismissed() bool                             { return m.modal.Dismissed() }
func (m *InviteModal) Dismiss()                                    { m.modal.Dismiss() }

// ---- state ----

// Requesting is isRequestingInvites for the picked type.
func (m *InviteModal) Requesting() bool {
	a := m.h.Core()
	return a == nil || a.IsRequestingInvites(m.typ)
}

// Source is the modal's openInviteModal source (the invites' campaign).
func (m *InviteModal) Source() string { return m.source }

// Guest reports a guest invite.
func (m *InviteModal) Guest() bool { return m.typ.Guest() }

// Type is the picked invite type.
func (m *InviteModal) Type() core.InviteType { return m.typ }

// teamName is the workspace's name, read again until it is known (the
// modal can open before the team loads).
func (m *InviteModal) teamName() string {
	if m.team != "" {
		return m.team
	}
	if a := m.h.Core(); a != nil {
		a.S.RLock()
		m.team = strings.TrimSpace(a.S.TeamName)
		a.S.RUnlock()
	}
	return m.team
}

// Title is InviteForm's title (tS).
func (m *InviteModal) Title() string {
	m.teamName()
	if m.Requesting() {
		return nsInviteWS.T("Request invitations to {workspaceName}", "workspaceName", m.team)
	}
	return nsInviteWS.T("Invite people to {workspaceName}", "workspaceName", m.team)
}

// Emails are the field's tokens.
func (m *InviteModal) Emails() []string { return append([]string(nil), m.emails...) }

// Pending is the field's text not yet a token.
func (m *InviteModal) Pending() string { return m.to.Text() }

// TypeText types s at the end of the field (tokenizeFilter runs on the
// result, as on each keystroke).
func (m *InviteModal) TypeText(s string) { m.setText(m.to.Text() + s) }

// SetText replaces the field's text (as a paste would) and tokenizes it.
func (m *InviteModal) SetText(s string) { m.setText(s) }

func (m *InviteModal) setText(s string) {
	if add, ok := inviteTokenize(s); ok {
		m.addEmails(add)
		m.to.SetText("")
		return
	}
	if m.to.Text() != s {
		m.to.SetText(s)
	}
}

// Commit makes the field's text tokens (Enter, blur).
func (m *InviteModal) Commit() {
	if add := inviteSplit(m.to.Text()); len(add) > 0 {
		m.addEmails(add)
	}
	if strings.TrimSpace(m.to.Text()) != "" || m.to.Len() > 0 {
		m.to.SetText("")
	}
}

func (m *InviteModal) addEmails(add []string) {
	m.emails = append(m.emails, add...)
	m.syncChips()
}

// RemoveEmail drops the token at i (its ×).
func (m *InviteModal) RemoveEmail(i int) {
	if i < 0 || i >= len(m.emails) {
		return
	}
	m.emails = append(m.emails[:i:i], m.emails[i+1:]...)
	m.syncChips()
}

func (m *InviteModal) syncChips() {
	for len(m.chips) < len(m.emails) {
		m.chips = append(m.chips, &invChip{})
	}
	m.chips = m.chips[:len(m.emails)]
	m.formErr = ""
}

// RemoveAll is an error line's link: drop every token with error e
// (invalid_email, already_in_workspace, self_invite, duplicate), or with
// e "" the addresses past the 200th.
func (m *InviteModal) RemoveAll(e string) {
	st := m.Tokens()
	if e == "" {
		m.emails = st.withinLimit()
	} else {
		m.emails = st.withoutError(e)
	}
	m.syncChips()
}

// Tokens is the field's token state.
func (m *InviteModal) Tokens() invTokenState {
	var member func(string) bool
	if a := m.h.Core(); a != nil {
		member = func(e string) bool { return a.MemberIDByEmail(e) != "" }
	}
	return deriveInviteTokens(m.emails, m.self, member)
}

// SetType picks Member or a guest type.
func (m *InviteModal) SetType(t core.InviteType) {
	if t == m.typ || t.Guest() && m.typ.Guest() {
		return
	}
	m.typ = t
	m.forceMC, m.expDays, m.expDate = false, 0, time.Time{}
	if t.Guest() {
		m.applyGuestChannels()
	}
	if !m.linkShown() {
		return
	}
	if !m.haveLink && !m.linkBusy {
		m.fetchLink()
	}
}

// guestOption is whether "Invite as" offers Guest (shouldHideGuestOption).
func (m *InviteModal) guestOption() bool {
	a := m.h.Core()
	return a != nil && a.CanSeeAccountTypeOptions()
}

// Channels are the picked channels.
func (m *InviteModal) Channels() []core.Conv { return append([]core.Conv(nil), m.chans...) }

// AddChannel picks a channel.
func (m *InviteModal) AddChannel(c core.Conv) {
	for _, x := range m.chans {
		if x.ID == c.ID {
			return
		}
	}
	m.chans = append(m.chans, c)
	m.chQ.SetText("")
	m.chQuery = "\x00"
	m.applyGuestChannels()
}

// RemoveChannel drops the picked channel at i.
func (m *InviteModal) RemoveChannel(i int) {
	if i < 0 || i >= len(m.chans) {
		return
	}
	m.chans = append(m.chans[:i:i], m.chans[i+1:]...)
	m.chQuery = "\x00"
	m.applyGuestChannels()
}

// applyGuestChannels is ChannelSelect's O: one channel makes a single-
// channel guest unless "Allow them to be added to more channels" is
// ticked, any other count a multi-channel guest.
func (m *InviteModal) applyGuestChannels() {
	if !m.typ.Guest() {
		return
	}
	if len(m.chans) == 1 && !m.forceMC {
		m.typ = core.InviteSingleChannelGuest
	} else {
		m.typ = core.InviteMultiChannelGuest
	}
}

// ToggleMoreChannels is the single-channel guest's checkbox (onForceMcg).
func (m *InviteModal) ToggleMoreChannels() {
	m.forceMC = !m.forceMC
	m.applyGuestChannels()
}

// SetMessage sets the custom message or, for a request, the reason.
func (m *InviteModal) SetMessage(s string) {
	if m.Requesting() {
		m.reason.SetText(s)
		return
	}
	m.showMsg = true
	m.msg.SetText(s)
}

// SetExpiration picks a guest's expiration (days; 0: no limit;
// InviteExpiryCustom: Custom).
func (m *InviteModal) SetExpiration(days int) { m.expDays = days }

// SetCustomExpiration picks Custom and the day.
func (m *InviteModal) SetCustomExpiration(day time.Time) {
	m.expDays = InviteExpiryCustom
	y, mo, d := day.Date()
	m.expDate = time.Date(y, mo, d, 0, 0, 0, 0, day.Location())
}

// InviteExpiryCustom is GuestExpirationSelect's Custom option (value -1).
const InviteExpiryCustom = -1

// viewerLoc is the viewer's time zone (getTimezone of the current member).
func (m *InviteModal) viewerLoc() *time.Location {
	if a := m.h.Core(); a != nil {
		if tz := a.Self().TZ; tz != "" {
			if l, err := time.LoadLocation(tz); err == nil {
				return l
			}
		}
	}
	return time.Local
}

// expiresTs is getExpirationTs (6FNz): 0 for No limit; for Custom the
// picked day, or today when none is (getDateInTimeZone(tz)); otherwise
// that many days from today; 11:59:59 PM that day in the viewer's zone
// (dPI7 getEndOfDayTs).
func (m *InviteModal) expiresTs(now time.Time) int64 {
	loc := m.viewerLoc()
	switch {
	case m.expDays == 0:
		return 0
	case m.expDays == InviteExpiryCustom:
		day := now.In(loc)
		if !m.expDate.IsZero() {
			day = m.expDate
		}
		y, mo, d := day.Date()
		return time.Date(y, mo, d, 23, 59, 59, 0, loc).Unix()
	}
	return core.GuestExpiry(now, loc, m.expDays)
}

// ExpiresTs is the expiration_ts Send would send now (0: none).
func (m *InviteModal) ExpiresTs(now time.Time) int64 {
	if !m.Guest() {
		return 0
	}
	return m.expiresTs(now)
}

// FormError is the banner's text ("" when none).
func (m *InviteModal) FormError() string { return m.formErr }

// customShown is shouldShowCustomMessage (0g8g JQ): paid, not a request,
// and a guest invite or "Add a message" clicked.
func (m *InviteModal) customShown() bool {
	return m.paid && !m.Requesting() && (m.Guest() || m.showMsg)
}

// customizeShown is getShouldShowCustomizeButton: paid, not a request or a
// guest invite, not clicked yet.
func (m *InviteModal) customizeShown() bool {
	return m.paid && !m.Requesting() && !m.Guest() && !m.showMsg
}

// channelSelectShown: ChannelSelect is drawn for guests and members alike
// (after the reason when requesting a member).
func (m *InviteModal) message() string {
	if m.Requesting() {
		return strings.TrimSpace(m.reason.Text())
	}
	if m.customShown() {
		return strings.TrimSpace(m.msg.Text())
	}
	return ""
}

// CanSend is the Send button's state: no token errors, at least one
// address (isBlocked), not sending, and dubb Od's rules (a guest's
// channels).
func (m *InviteModal) CanSend() bool {
	if m.sending || m.Tokens().blocked() || strings.TrimSpace(m.to.Text()) != "" && len(m.emails) == 0 {
		return false
	}
	if m.msg.Len() > 560 || m.formErr == "no_urls_allowed" {
		return false
	}
	switch m.typ {
	case core.InviteMultiChannelGuest:
		return len(m.chans) >= 1
	case core.InviteSingleChannelGuest:
		return len(m.chans) == 1
	}
	return true
}

// Send is sendInvites.
func (m *InviteModal) Send() {
	if !m.CanSend() {
		return
	}
	st := m.Tokens()
	var ids []string
	for _, c := range m.chans {
		ids = append(ids, c.ID)
	}
	o := core.InviteOpts{Emails: st.invitable(), Type: m.typ, Channels: ids, Message: m.message(), Source: m.source}
	if m.Guest() && m.expDays != 0 {
		o.Expires = m.expiresTs(time.Now())
	}
	m.sending, m.formErr = true, ""
	requesting := m.Requesting()
	run(m.h, func(ctx context.Context) ([]core.InviteResult, error) {
		a, err := cxApp(m.h)
		if err != nil {
			return nil, err
		}
		return a.SendInvites(ctx, o)
	}, func(res []core.InviteResult, err error) {
		m.sending = false
		var ie *core.InviteError
		switch {
		case err == nil:
			m.showSummary(res, requesting)
		case errors.As(err, &ie):
			if txt := inviteErrorText(ie.Code, 1, m.team); txt != "" || len(ie.Results) == 0 {
				m.formErr = ie.Code
			} else {
				m.showSummary(ie.Results, requesting)
			}
		default:
			m.formErr = "invite_failed"
		}
		m.h.Invalidate()
	})
}

// ---- the invite link ----

// linkShown is shouldShowInviteLinkComponents: not on Grid, not a
// request, not a guest invite.
func (m *InviteModal) linkShown() bool { return !m.grid && !m.Requesting() && !m.Guest() }

// fetchLink is getCurrentSharedInviteLink.
func (m *InviteModal) fetchLink() {
	m.linkBusy = true
	run(m.h, func(ctx context.Context) (core.SharedInviteLink, error) {
		a, err := cxApp(m.h)
		if err != nil {
			return core.SharedInviteLink{}, err
		}
		l, ok, err := a.CurrentInviteLink(ctx)
		if err == nil && !ok {
			err = errNoLink
		}
		return l, err
	}, func(l core.SharedInviteLink, err error) {
		m.linkBusy = false
		if err == nil {
			m.link, m.haveLink = l, true
		} else if !errors.Is(err, errNoLink) && !m.haveLink {
			m.formErr = "list_shared_invite_links_failed"
		}
		m.h.Invalidate()
	})
}

var errNoLink = errors.New("no invite link")

// CopyLink is SharedInviteLink's copy: the current link, else a new one.
func (m *InviteModal) CopyLink() {
	if m.linkBusy {
		return
	}
	if m.haveLink {
		m.copyNext, m.linkCopied = m.link.ShareURL(), true
		m.h.Invalidate()
		return
	}
	m.linkBusy = true
	run(m.h, func(ctx context.Context) (core.SharedInviteLink, error) {
		a, err := cxApp(m.h)
		if err != nil {
			return core.SharedInviteLink{}, err
		}
		return a.CreateInviteLink(ctx)
	}, func(l core.SharedInviteLink, err error) {
		m.linkBusy = false
		if err != nil {
			m.formErr = "create_shared_invite_link_failed"
		} else {
			m.link, m.haveLink = l, true
			m.copyNext, m.linkCopied = l.ShareURL(), true
		}
		m.h.Invalidate()
	})
}

// LinkCopied reports the confirmation tooltip's state.
func (m *InviteModal) LinkCopied() bool { return m.linkCopied }

// linkTipText is the copy button's tooltip: "Invite your coworkers with
// this link", or after a copy getSharedInviteLinkCopyConfirmationText.
func (m *InviteModal) linkTipText() string {
	if !m.linkCopied || !m.haveLink {
		return nsInviteWS.T("Invite your coworkers with this link")
	}
	d := m.link.DaysLeft(time.Now())
	var parts []i18n.Part
	switch {
	case d > 365:
		parts = nsInviteWS.RT("<strong>Link copied</strong> - never expires")
	case d > 30:
		parts = nsInviteWS.RT("<strong>Link copied</strong> - expires in over 30 days")
	case d != 0:
		parts = nsInviteWS.RT("<strong>Link copied</strong> - expires in {daysRemainingOnLink, plural, =1 {# day} other {# days}}", "daysRemainingOnLink", d)
	default:
		parts = nsInviteWS.RT("<strong>Link copied</strong> - expires today")
	}
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

// ---- channels ----

// inviteChannelOptions are ChannelSelect's options: the channels Clack
// knows that aren't archived, DMs or Slack Connect channels.
func (m *InviteModal) inviteChannelOptions() []core.Conv {
	a := m.h.Core()
	if a == nil {
		return nil
	}
	var out []core.Conv
	for _, cv := range a.SnapshotNow().Convs {
		if (cv.Kind == "channel" || cv.Kind == "private") && !cv.IsArchived && !cv.IsShared && cv.Code == nil {
			out = append(out, cv)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func (m *InviteModal) refilterChannels() {
	q := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(m.chQ.Text(), "#")))
	if q == m.chQuery {
		return
	}
	m.chQuery, m.chHL = q, 0
	m.chMatch = m.chMatch[:0]
	if q == "" { // allowEmptyQuery: false
		return
	}
	picked := map[string]bool{}
	for _, c := range m.chans {
		picked[c.ID] = true
	}
	var pre, mid []core.Conv
	for _, c := range m.inviteChannelOptions() {
		n := strings.ToLower(c.Name)
		switch {
		case picked[c.ID]:
		case strings.HasPrefix(n, q):
			pre = append(pre, c)
		case strings.Contains(n, q):
			mid = append(mid, c)
		}
	}
	for _, c := range append(pre, mid...) {
		if len(m.chMatch) == len(m.chSugg) {
			break
		}
		m.chMatch = append(m.chMatch, c)
	}
}

// ---- the summary ----

func (m *InviteModal) showSummary(res []core.InviteResult, requesting bool) {
	m.modal.Close()
	openInviteSummary(m.h, m.source, m.teamName(), res, requesting, m.chans)
}

// ---- events ----

func (m *InviteModal) events(gtx layout.Context) {
	if m.copyNext != "" {
		copyText(gtx, m.copyNext)
		m.copyNext = ""
	}
	focused := gtx.Focused(&m.to)
	if m.wasIn && !focused && m.to.Len() > 0 {
		m.Commit() // onBlurCommit
	}
	m.wasIn = focused
	var filters []event.Filter
	if m.to.Len() == 0 && len(m.emails) > 0 {
		filters = append(filters, key.Filter{Focus: &m.to, Name: key.NameDeleteBackward})
	}
	if gtx.Focused(&m.chQ) && len(m.chMatch) > 0 {
		filters = append(filters, key.Filter{Focus: &m.chQ, Name: key.NameUpArrow}, key.Filter{Focus: &m.chQ, Name: key.NameDownArrow})
	}
	if m.chQ.Len() == 0 && len(m.chans) > 0 {
		filters = append(filters, key.Filter{Focus: &m.chQ, Name: key.NameDeleteBackward})
	}
	for len(filters) > 0 {
		e, ok := gtx.Event(filters...)
		if !ok {
			break
		}
		ke, isKey := e.(key.Event)
		if !isKey || ke.State != key.Press {
			continue
		}
		switch {
		case ke.Name == key.NameDeleteBackward && gtx.Focused(&m.to):
			m.RemoveEmail(len(m.emails) - 1)
		case ke.Name == key.NameDeleteBackward:
			m.RemoveChannel(len(m.chans) - 1)
		case ke.Name == key.NameUpArrow && len(m.chMatch) > 0:
			m.chHL = (m.chHL - 1 + len(m.chMatch)) % len(m.chMatch)
		case ke.Name == key.NameDownArrow && len(m.chMatch) > 0:
			m.chHL = (m.chHL + 1) % len(m.chMatch)
		}
	}
	for {
		e, ok := m.to.Update(gtx)
		if !ok {
			break
		}
		switch e.(type) {
		case widget.SubmitEvent:
			m.Commit()
		case widget.ChangeEvent:
			m.setText(m.to.Text())
		}
	}
	for {
		e, ok := m.chQ.Update(gtx)
		if !ok {
			break
		}
		if _, sub := e.(widget.SubmitEvent); sub && len(m.chMatch) > 0 {
			m.AddChannel(m.chMatch[m.chHL])
		}
	}
	for {
		if _, ok := m.msg.Update(gtx); !ok {
			break
		}
	}
	for {
		if _, ok := m.reason.Update(gtx); !ok {
			break
		}
	}
	m.refilterChannels()
	for i := range m.chMatch {
		if m.chSugg[i].Clicked(gtx) {
			m.AddChannel(m.chMatch[i])
			gtx.Execute(key.FocusCmd{Tag: &m.chQ})
			break
		}
	}
	for i := 0; i < len(m.chips); i++ {
		if m.chips[i].rm.Clicked(gtx) {
			m.RemoveEmail(i)
			m.toFocus = true
			break
		}
	}
	for i := 0; i < len(m.chRm) && i < len(m.chans); i++ {
		if m.chRm[i].Clicked(gtx) {
			m.RemoveChannel(i)
			break
		}
	}
	for i, e := range []string{invErrInvalid, invErrAlready, invErrSelf, invErrDuplicate, ""} {
		if m.fix[i].Clicked(gtx) {
			m.RemoveAll(e)
		}
	}
	if m.toBox.Clicked(gtx) {
		m.toFocus = true
	}
	if m.chBox.Clicked(gtx) {
		gtx.Execute(key.FocusCmd{Tag: &m.chQ})
	}
	if m.msgBox.Clicked(gtx) {
		gtx.Execute(key.FocusCmd{Tag: &m.msg})
	}
	if m.reasonBox.Clicked(gtx) {
		gtx.Execute(key.FocusCmd{Tag: &m.reason})
	}
	if m.custom.Clicked(gtx) {
		m.showMsg = true
		gtx.Execute(key.FocusCmd{Tag: &m.msg})
	}
	if m.mcBox.Clicked(gtx) {
		m.ToggleMoreChannels()
	}
	if m.linkBtn.Clicked(gtx) {
		m.CopyLink()
	}
	if m.learn.Clicked(gtx) {
		srOpenLink(m.h, "https://slack.com/help/articles/202518103")
	}
}

// ---- layout ----

func (m *InviteModal) layout(gtx layout.Context) layout.Dimensions {
	if m.modal.done {
		return layout.Dimensions{}
	}
	m.events(gtx)
	if m.modal.done {
		return layout.Dimensions{}
	}
	if m.toFocus {
		gtx.Execute(key.FocusCmd{Tag: &m.to})
		m.toFocus = false
	}
	th := m.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	req, guest := m.Requesting(), m.Guest()
	if req {
		m.send.Label = nsInviteWS.T("Send Request")
	} else {
		m.send.Label = nsInviteWS.T("Send")
	}
	org := ModalBodyOrigin(gtx, m.modal)
	y := 0
	put := func(wd layout.Widget) {
		call, d := record(loose(gtx, image.Pt(w, 1<<20)), wd)
		macroAt(gtx, call, image.Pt(0, y))
		y += d.Size.Y
	}
	// "To:" (c-label: 15/700, padding-bottom 8)
	m.label(gtx, th, cl, &y, w, []i18n.Part{{Text: nsInviteWS.T("To:"), Tag: "strong"}}, "")
	put(func(gtx layout.Context) layout.Dimensions { return m.emailField(gtx, th, cl) })
	put(func(gtx layout.Context) layout.Dimensions { return m.errorLines(gtx, th, cl) })
	y += dp(gtx, 8) // p-email_input_container padding-bottom
	// Invite as: (paid teams)
	if m.paid {
		y += dp(gtx, 8) // margin_top_50
		m.label(gtx, th, cl, &y, w, []i18n.Part{{Text: nsInviteWS.T("Invite as:"), Tag: "strong"}}, "")
		selY := y
		put(func(gtx layout.Context) layout.Dimensions {
			return SelectField(gtx, th, &m.typBtn, w, "", m.typeLabel(), invOverlayOpen(m.typOv), false)
		})
		if m.typBtn.Clicked(gtx) && !invOverlayOpen(m.typOv) {
			m.openTypeList(image.Rectangle{Min: org.Add(image.Pt(0, selY)), Max: org.Add(image.Pt(w, y))})
		}
	}
	chanBottom := -1
	channels := func() {
		y += dp(gtx, 16) // p-channel_select--label margin-top
		var lbl []i18n.Part
		if guest {
			lbl = nsInviteWS.RT("<strong>Add to team channels</strong> (required)")
		} else {
			lbl = nsInviteWS.RT("<strong>Add to team channels</strong> (optional)")
		}
		m.label(gtx, th, cl, &y, w, lbl, nsInviteWS.T("Make sure your teammates are in the right conversations from the get go."))
		put(func(gtx layout.Context) layout.Dimensions { return m.recommendedRow(gtx, th, cl, w) })
		put(func(gtx layout.Context) layout.Dimensions { return m.channelField(gtx, th, cl) })
		chanBottom = y
		if guest {
			put(func(gtx layout.Context) layout.Dimensions { return m.guestWarning(gtx, th, cl, w) })
		}
		y += dp(gtx, 16) // margin-bottom
	}
	if guest || !req {
		channels()
	}
	if m.paid && guest {
		y += dp(gtx, 24) // margin_top_150
		m.label(gtx, th, cl, &y, w, nsInviteWS.RT("<strong>Set an expiration date</strong>"), "")
		selY := y
		put(func(gtx layout.Context) layout.Dimensions {
			return SelectField(gtx, th, &m.expBtn, w, "", m.expLabel(), invOverlayOpen(m.expOv), false)
		})
		if m.expBtn.Clicked(gtx) && !invOverlayOpen(m.expOv) {
			m.openExpList(image.Rectangle{Min: org.Add(image.Pt(0, selY)), Max: org.Add(image.Pt(w, y))})
		}
		if m.expDays == InviteExpiryCustom {
			// .p-guest_expiration_select--custom_expiration: margin-top 12
			y += dp(gtx, 12)
			bY := y
			put(func(gtx layout.Context) layout.Dimensions { return m.customExpButton(gtx, th, cl, w) })
			if m.expCal.Clicked(gtx) && !invOverlayOpen(m.calOv) {
				m.openCalendar(image.Rectangle{Min: org.Add(image.Pt(0, bY)), Max: org.Add(image.Pt(w, y))}, dp(gtx, 315))
			}
		}
		// .p-guest_expiration_select--description: 13/18, 70% foreground,
		// margin-top 8
		y += dp(gtx, 8)
		put(func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 13, 18, theme.Regular, cl.Muted, nsInviteWS.T("These guest accounts will expire at 11:59 PM on the date you select."), 0)
		})
	}
	if m.customShown() {
		y += dp(gtx, 8) // margin_top_50
		m.label(gtx, th, cl, &y, w, nsInviteWS.RT("<strong>Custom message:</strong>"), "")
		put(func(gtx layout.Context) layout.Dimensions {
			return m.textBox(gtx, th, cl, &m.msg, &m.msgBox, nsInviteWS.T("Add a personal note to your invitation"), 66)
		})
	}
	if req {
		y += dp(gtx, 20) // p-reason_for_request margin-top
		m.label(gtx, th, cl, &y, w, nsInviteWS.RT("<strong>Reason for request</strong> (optional)"), "")
		put(func(gtx layout.Context) layout.Dimensions {
			return m.textBox(gtx, th, cl, &m.reason, &m.reasonBox, nsInviteWS.T("Add a note for your admin"), 0)
		})
		y += dp(gtx, 8)
		put(func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 15, 22, theme.Regular, cl.Muted, nsInviteWS.T("Your request will be sent to your admins, and you’ll be notified when it’s approved or denied."), 0)
		})
		if !guest {
			channels()
		}
	}
	if m.customizeShown() {
		y += dp(gtx, 12)
		put(func(gtx layout.Context) layout.Dimensions { return m.customizeButton(gtx, th, cl) })
	}
	if txt := m.formErrorText(); txt != "" {
		y += dp(gtx, 16)
		put(func(gtx layout.Context) layout.Dimensions { return inviteBanner(gtx, th, cl, w, txt) })
	}
	if chanBottom >= 0 && gtx.Focused(&m.chQ) && len(m.chMatch) > 0 {
		call, _ := record(loose(gtx, image.Pt(w, 1<<20)), func(gtx layout.Context) layout.Dimensions { return m.channelSuggestions(gtx, th, cl) })
		macroAt(gtx, call, image.Pt(0, chanBottom+dp(gtx, 4)))
	}
	return layout.Dimensions{Size: image.Pt(w, y+dp(gtx, 4))}
}

// label is Slack's c-label: 15/700 text (a <strong> part bold, the rest
// regular), then the c-label__subtext line, padding-bottom 8.
func (m *InviteModal) label(gtx layout.Context, th *theme.Theme, cl *Colors, y *int, w int, parts []i18n.Part, sub string) {
	x := 0
	h := 0
	for _, p := range parts {
		t := p.Text
		if t == "" {
			continue
		}
		wt := theme.Bold
		if p.Tag == "" && len(parts) > 1 {
			wt = theme.Regular
		}
		d := at(gtx, image.Pt(x, *y), func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, wt, cl.Fg, t)
		})
		x += d.Size.X
		h = maxInt(h, d.Size.Y)
	}
	*y += maxInt(h, dp(gtx, 22))
	if sub != "" {
		d := at(gtx, image.Pt(0, *y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return para(gtx, th, 15, 22, theme.Regular, cl.Muted, sub, 0)
		})
		*y += d.Size.Y
	}
	*y += dp(gtx, 8)
}

// typeLabel is the select's shown option.
func (m *InviteModal) typeLabel() string {
	if m.Guest() {
		for _, p := range nsInviteWS.RT("Guest <span className=\"p-invite_type_select_option_label_description\">– limited to select channels, files and directory.</span>") {
			_ = p
		}
		var b strings.Builder
		for _, p := range nsInviteWS.RT("Guest <span className=\"p-invite_type_select_option_label_description\">– limited to select channels, files and directory.</span>") {
			b.WriteString(p.Text)
		}
		return b.String()
	}
	return nsInviteWS.T("Member")
}

func (m *InviteModal) openTypeList(r image.Rectangle) {
	labels := []string{nsInviteWS.T("Member")}
	types := []core.InviteType{core.InviteMember}
	if m.guestOption() {
		labels = append(labels, nsInviteWS.T("Guest"))
		types = append(types, core.InviteMultiChannelGuest)
	}
	sel := 0
	if m.Guest() && len(types) > 1 {
		sel = 1
	}
	m.typOv = NewOptionList(m.h, r, labels, sel, func(i int) {
		m.SetType(types[i])
		m.h.Invalidate()
	})
	m.h.ShowOverlay(m.typOv)
}

// invExpOptions are GuestExpirationSelect's options.
var invExpOptions = []int{0, 7, 30, 60, InviteExpiryCustom}

func invExpLabel(days int) string {
	switch days {
	case InviteExpiryCustom:
		return nsInviteWS.T("Custom")
	case 7:
		return nsInviteWS.T("7 days")
	case 30:
		return nsInviteWS.T("30 days")
	case 60:
		return nsInviteWS.T("60 days")
	}
	return nsInviteWS.T("No limit")
}

func (m *InviteModal) expLabel() string { return invExpLabel(m.expDays) }

func (m *InviteModal) openExpList(r image.Rectangle) {
	labels := make([]string, len(invExpOptions))
	sel := 0
	for i, d := range invExpOptions {
		labels[i] = invExpLabel(d)
		if d == m.expDays {
			sel = i
		}
	}
	m.expOv = NewOptionList(m.h, r, labels, sel, func(i int) {
		m.expDays = invExpOptions[i]
		m.h.Invalidate()
	})
	m.h.ShowOverlay(m.expOv)
}

// openCalendar is the Custom button's DatePicker: position "bottom" with
// offsetY -315, days before tomorrow (getDateInTimeZone(tz, 1)) and more
// than ten years out disabled.
func (m *InviteModal) openCalendar(r image.Rectangle, up int) {
	now := time.Now()
	t := now.In(m.viewerLoc())
	tomorrow := time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, time.Local)
	latest := time.Date(now.Year()+10, now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	sel := m.expDate
	if sel.IsZero() {
		sel = tomorrow
	}
	m.calOv = NewDatePickerRange(m.h, r.Add(image.Pt(0, -up)), sel, tomorrow, latest, func(day time.Time) {
		m.SetCustomExpiration(day)
		m.h.Invalidate()
	})
	m.h.ShowOverlay(m.calOv)
}

// CustomExpirationText is the Custom button's text: "Deactivate on
// {date}" (moment "LL") once a day is picked.
func (m *InviteModal) CustomExpirationText() string {
	if m.expDate.IsZero() {
		return ""
	}
	return nsInviteWS.T("Deactivate on {date}", "date", i18n.FormatDayjs(i18n.Locale(), m.expDate, "LL"))
}

// customExpButton is .p-guest_expiration_select--custom_expiration: an
// outline button 36px tall, full width, radius 8, a 1px otl-pry border,
// padding 4px 12px, the 20px calendar icon and, after 8px, the 15px text.
func (m *InviteModal) customExpButton(gtx layout.Context, th *theme.Theme, cl *Colors, w int) layout.Dimensions {
	hh := dp(gtx, 36)
	open := invOverlayOpen(m.calOv)
	return clickBox(gtx, &m.expCal, nil, func(gtx layout.Context) layout.Dimensions {
		r := image.Rect(0, 0, w, hh)
		rad := dp(gtx, 8)
		b := dtBorder(th)
		if m.expCal.Hovered() {
			b = cl.Fg2
		}
		if open {
			FocusRingColors(gtx, cl.Focus, cl.FocusRing, r, rad)
		}
		rrect(gtx, r, rad, b)
		bw := border1(gtx)
		rrect(gtx, r.Inset(bw), rad-bw, cl.Bg)
		x := dp(gtx, 12)
		is := dp(gtx, 20)
		at(gtx, image.Pt(x, (hh-is)/2+dp(gtx, 1)), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, "calendar", 20, cl.Fg)
		})
		if txt := m.CustomExpirationText(); txt != "" {
			x += is + dp(gtx, 8)
			at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-x-dp(gtx, 12)), hh)}
				return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 15, theme.Regular, cl.Fg, txt)
				})
			})
		}
		return layout.Dimensions{Size: r.Max}
	})
}

// recommendedRow is GXKR's row: "Suggested:" (13px, content-sec), then
// a Tag per channel (informative: 24px, padding-inline 4, gap 4, radius
// 4, 15px; plus-filled and the channel's inline entity) wrapping 2px
// apart, 4px above the field; three shimmer tags while it loads.
func (m *InviteModal) recommendedRow(gtx layout.Context, th *theme.Theme, cl *Colors, w int) layout.Dimensions {
	sugg := m.Suggested()
	if !m.recLoading && len(sugg) == 0 {
		return layout.Dimensions{}
	}
	for i := range sugg {
		if m.recBtn[i].Clicked(gtx) {
			m.AddSuggested(sugg[i])
			m.h.Invalidate()
		}
	}
	bg, fg := menuTagColors(th, "informative")
	tagH, pad, gap := dp(gtx, 24), dp(gtx, 4), dp(gtx, 2)
	var items []func(gtx layout.Context) layout.Dimensions
	items = append(items, func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, tagH, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 13, theme.Regular, cl.Fg2, nsInviteWS.T("Suggested:"))
		})
	})
	if m.recLoading {
		shim := theme.WithAlpha(cl.Fg, 0x21)
		for i := 0; i < 3; i++ {
			items = append(items, func(gtx layout.Context) layout.Dimensions {
				// margin_left_25, the channel icon and a 120px missing-text bar
				x := gap
				sz := image.Pt(gap+pad+dp(gtx, 20)+dp(gtx, 4)+dp(gtx, 120)+pad, tagH)
				rrect(gtx, image.Rect(x, 0, sz.X, tagH), dp(gtx, 4), bg)
				x += pad
				at(gtx, image.Pt(x, (tagH-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions { return icon(gtx, "channel", 20, fg) })
				x += dp(gtx, 20) + dp(gtx, 4)
				rrect(gtx, image.Rect(x, (tagH-dp(gtx, 8))/2, x+dp(gtx, 120), (tagH+dp(gtx, 8))/2), dp(gtx, 4), shim)
				return layout.Dimensions{Size: sz}
			})
		}
	} else {
		for i := range sugg {
			c, btn := sugg[i], &m.recBtn[i]
			items = append(items, func(gtx layout.Context) layout.Dimensions {
				return invRecTag(gtx, th, btn, c, bg, fg, w)
			})
		}
	}
	// display flex, flex-wrap, align-items center, gap 2
	x, y, lineH := 0, 0, tagH
	for _, it := range items {
		call, d := record(loose(gtx, image.Pt(w, tagH)), it)
		if x > 0 && x+d.Size.X > w {
			x, y = 0, y+lineH+gap
		}
		macroAt(gtx, call, image.Pt(x, y+(lineH-d.Size.Y)/2))
		x += d.Size.X + gap
	}
	return layout.Dimensions{Size: image.Pt(w, y+lineH+dp(gtx, 4))} // margin_bottom_50
}

// invRecTag is one recommended channel: a clickable informative Tag with
// plus-filled (20) and the channel (its inline icon, 1em in a 20px box,
// and the name, truncated to the row).
func invRecTag(gtx layout.Context, th *theme.Theme, btn *widget.Clickable, c core.Conv, bg, fg color.NRGBA, maxW int) layout.Dimensions {
	tagH, pad, gap := dp(gtx, 24), dp(gtx, 4), dp(gtx, 4)
	ic := "channel"
	if c.Kind == "private" {
		ic = "lock"
	}
	nameMax := maxInt(1, maxW-2*pad-2*dp(gtx, 20)-gap)
	call, nd := record(loose(gtx, image.Pt(nameMax, tagH)), func(gtx layout.Context) layout.Dimensions {
		return text(gtx, th, 15, theme.Regular, fg, c.Name)
	})
	sz := image.Pt(pad+dp(gtx, 20)+gap+dp(gtx, 20)+nd.Size.X+pad, tagH)
	return clickBox(gtx, btn, nil, func(gtx layout.Context) layout.Dimensions {
		rrect(gtx, image.Rectangle{Max: sz}, dp(gtx, 4), bg)
		x := pad
		at(gtx, image.Pt(x, (tagH-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions { return icon(gtx, "plus-filled", 20, fg) })
		x += dp(gtx, 20) + gap
		at(gtx, image.Pt(x+(dp(gtx, 20)-dp(gtx, 15))/2, (tagH-dp(gtx, 15))/2), func(gtx layout.Context) layout.Dimensions { return icon(gtx, ic, 15, fg) })
		x += dp(gtx, 20)
		macroAt(gtx, call, image.Pt(x, (tagH-nd.Size.Y)/2))
		return layout.Dimensions{Size: sz}
	})
}

// emailField is the multi-select: tokens (c-token--large: 32px, 18px
// text, radius 4, surf-hgl-1; an error token surf-imp with a dashed
// otl-imp border) wrapping, then the text. The box is at least 44px
// (min-height space.500 plus the border) and at most 200.
func (m *InviteModal) emailField(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	w := gtx.Constraints.Max.X
	padX, padY := dp(gtx, 8), dp(gtx, 4)
	lineH, gap := dp(gtx, 32), dp(gtx, 4)
	innerW := maxInt(1, w-2*padX)
	st := m.Tokens()
	hover := map[string]string{
		invErrInvalid:   nsInvite.T("Invalid email address"),
		invErrDuplicate: nsInvite.T("Duplicate email"),
		invErrAlready:   nsInvite.T("Already in this workspace"),
		invErrSelf:      nsInvite.T("You can’t invite yourself"),
	}
	content, cd := record(gtx, func(gtx layout.Context) layout.Dimensions {
		x, y := 0, 0
		for i, t := range st.Tokens {
			if i >= len(m.chips) {
				break
			}
			c := m.chips[i]
			tip := ""
			if len(t.Errors) > 0 {
				tip = hover[t.Errors[0]]
			}
			call, d := record(gtx, func(gtx layout.Context) layout.Dimensions {
				return inviteToken(gtx, th, cl, &c.rm, "", t.Email, len(t.Errors) > 0, innerW, lineH, 18)
			})
			if x > 0 && x+d.Size.X > innerW {
				x, y = 0, y+lineH+gap
			}
			p := image.Pt(x, y)
			macroAt(gtx, call, p)
			at(gtx, p, func(gtx layout.Context) layout.Dimensions {
				hov := c.hv.Update(gtx.Source)
				area := clip.Rect{Max: d.Size}.Push(gtx.Ops)
				c.hv.Add(gtx.Ops)
				area.Pop()
				if tip != "" {
					Tooltip(gtx, th, &c.tip, hov, d.Size, Above, tip, "")
				}
				return layout.Dimensions{}
			})
			x += d.Size.X + gap
		}
		ew := innerW - x
		if ew < dp(gtx, 160) && x > 0 {
			x, y, ew = 0, y+lineH+gap, innerW
		}
		at(gtx, image.Pt(x+dp(gtx, 4), y), func(gtx layout.Context) layout.Dimensions {
			ew := maxInt(1, ew-dp(gtx, 4))
			return vcenter(gtx, lineH, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Min: image.Pt(ew, 0), Max: image.Pt(ew, lineH)}
				if m.to.Len() == 0 && len(m.emails) == 0 {
					call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 18, theme.Regular, cl.Placeholder, m.placeholder())
					})
					call.Add(gtx.Ops)
				}
				return th.Editor(gtx, &m.to, unit.Sp(18), cl.Fg, theme.WithAlpha(cl.Focus, 0x55))
			})
		})
		return layout.Dimensions{Size: image.Pt(innerW, y+lineH)}
	})
	hgt := minInt(maxInt(cd.Size.Y+2*padY, dp(gtx, 44)), dp(gtx, 200))
	r := image.Rect(0, 0, w, hgt)
	actFocusBox(gtx, cl, r, gtx.Focused(&m.to))
	m.toBox.Layout(exact(gtx, r.Max), func(gtx layout.Context) layout.Dimensions {
		defer clip.Rect(r).Push(gtx.Ops).Pop()
		pointer.CursorText.Add(gtx.Ops)
		return layout.Dimensions{Size: r.Max}
	})
	func() {
		defer clip.Rect(r.Inset(border1(gtx))).Push(gtx.Ops).Pop()
		macroAt(gtx, content, image.Pt(padX, padY+(hgt-2*padY-cd.Size.Y)/2*boolInt(cd.Size.Y < hgt-2*padY)))
	}()
	return layout.Dimensions{Size: r.Max}
}

// invOverlayOpen reports a select's list still showing.
func invOverlayOpen(o Overlay) bool { return o != nil && !o.Dismissed() }

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// placeholder is "name@{domain}" with the viewer's email domain
// (example.com without one).
func (m *InviteModal) placeholder() string {
	dom := ""
	if _, d, ok := strings.Cut(m.self, "@"); ok {
		dom = d
	}
	if dom == "" {
		dom = nsInvite.T("example.com")
	}
	return nsInvite.T("name@{domain}", "domain", dom)
}

// inviteToken draws one c-token: iconName (a channel's # or lock) or
// none, the label, and the × (c-token's remove button).
func inviteToken(gtx layout.Context, th *theme.Theme, cl *Colors, rm *widget.Clickable, iconName, label string, bad bool, maxW, h int, size float32) layout.Dimensions {
	tw := textW(gtx, th, size, theme.Bold, label)
	pad := dp(gtx, 4)
	iconW := 0
	if iconName != "" {
		iconW = dp(gtx, 16) + dp(gtx, 2)
	}
	rmW := dp(gtx, 20)
	w := pad + iconW + tw + pad + rmW + dp(gtx, 4)
	if w > maxW {
		tw -= w - maxW
		w = maxW
	}
	r := image.Rect(0, 0, w, h)
	bg := woTok(th, "surf-hgl-1")
	if bad {
		bg = woTok(th, "surf-imp")
	}
	rrect(gtx, r, dp(gtx, 4), bg)
	if bad {
		inviteDashed(gtx, r, dp(gtx, 4), woTok(th, "otl-imp"))
	}
	x := pad
	if iconName != "" {
		at(gtx, image.Pt(x, (h-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions { return icon(gtx, iconName, 16, cl.Fg) })
		x += iconW
	}
	at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, tw), h)}
		return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, size, theme.Bold, woTok(th, "content-pry"), label)
		})
	})
	x += maxInt(1, tw) + pad
	at(gtx, image.Pt(x, (h-rmW)/2), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, rm, "close-small", 20, 14, cl.Fg2, theme.WithAlpha(cl.Link, 0x26), 4)
	})
	return layout.Dimensions{Size: r.Max}
}

// inviteDashed draws a 1px dashed border around r (border: 1px dashed).
func inviteDashed(gtx layout.Context, r image.Rectangle, rad int, c color.NRGBA) {
	dash, gapW, t := dp(gtx, 3), dp(gtx, 3), border1(gtx)
	for x := r.Min.X + rad; x < r.Max.X-rad; x += dash + gapW {
		e := minInt(x+dash, r.Max.X-rad)
		fill(gtx, image.Rect(x, r.Min.Y, e, r.Min.Y+t), c)
		fill(gtx, image.Rect(x, r.Max.Y-t, e, r.Max.Y), c)
	}
	for y := r.Min.Y + rad; y < r.Max.Y-rad; y += dash + gapW {
		e := minInt(y+dash, r.Max.Y-rad)
		fill(gtx, image.Rect(r.Min.X, y, r.Min.X+t, e), c)
		fill(gtx, image.Rect(r.Max.X-t, y, r.Max.X, e), c)
	}
	// the rounded corners, solid
	for _, p := range []image.Point{r.Min, {r.Max.X - rad, r.Min.Y}, {r.Min.X, r.Max.Y - rad}, {r.Max.X - rad, r.Max.Y - rad}} {
		fill(gtx, image.Rect(p.X+rad/2, p.Y, p.X+rad/2+1, p.Y+t).Intersect(r), c)
	}
}

// errorLines are TokenizerEmailInput's MultiSelectErrorText lines (margin
// top 12): the warning icon, the count, a Remove link.
func (m *InviteModal) errorLines(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	st := m.Tokens()
	type line struct {
		i         int
		copy, cta string
	}
	var ls []line
	if n := st.InvalidN; n > 0 {
		ls = append(ls, line{0, nsInvite.T("{count, plural, =1 {# error} other {# errors}}.", "count", n), nsInvite.T("Remove all items with errors")})
	}
	if n := st.MemberN; n > 0 {
		ls = append(ls, line{1, nsInvite.T("{count, plural, =1 {# existing member} other {# existing members}}.", "count", n),
			nsInvite.T("{count, plural, =1 {Remove existing member} other {Remove existing members}}", "count", n)})
	}
	if st.SelfN > 0 {
		ls = append(ls, line{2, nsInvite.T("You can’t invite yourself."), nsInvite.T("Remove your email address")})
	}
	if n := st.DupN; n > 0 {
		ls = append(ls, line{3, nsInvite.T("{count, plural, =1 {# duplicate email} other {# duplicate emails}}.", "count", n),
			nsInvite.T("{count, plural, =1 {Remove duplicate} other {Remove duplicates}}", "count", n)})
	}
	if st.OverLimit > 0 {
		ls = append(ls, line{4, nsInvite.T("You can only add {emailLimit} people at a time.", "emailLimit", invEmailLimit),
			nsInvite.T("Remove the last {excessEmails}", "excessEmails", st.OverLimit)})
	}
	if len(ls) == 0 {
		return layout.Dimensions{}
	}
	y := dp(gtx, 12)
	lh := dp(gtx, 22)
	for _, l := range ls {
		at(gtx, image.Pt(0, y+(lh-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions { return icon(gtx, "warning", 16, cl.Danger) })
		x := dp(gtx, 20)
		d := at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 15, theme.Regular, cl.Fg, l.copy) })
		})
		x += d.Size.X + dp(gtx, 4)
		at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, &m.fix[l.i], nil, func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 15, theme.Regular, cl.Link, l.cta)
				})
			})
		})
		y += lh + dp(gtx, 4)
	}
	return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, y)}
}

// channelField is ChannelSelect's multi-select (medium: 26px tokens, 15px
// text), placeholder "Search channels".
func (m *InviteModal) channelField(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	w := gtx.Constraints.Max.X
	padX, padY := dp(gtx, 8), dp(gtx, 4)
	lineH, gap := dp(gtx, 26), dp(gtx, 4)
	innerW := maxInt(1, w-2*padX)
	for len(m.chRm) < len(m.chans) {
		m.chRm = append(m.chRm, new(widget.Clickable))
	}
	content, cd := record(gtx, func(gtx layout.Context) layout.Dimensions {
		x, y := 0, 0
		for i, c := range m.chans {
			ic := "channel"
			if c.Kind == "private" {
				ic = "lock"
			}
			call, d := record(gtx, func(gtx layout.Context) layout.Dimensions {
				return inviteToken(gtx, th, cl, m.chRm[i], ic, c.Name, false, innerW, lineH, 15)
			})
			if x > 0 && x+d.Size.X > innerW {
				x, y = 0, y+lineH+gap
			}
			macroAt(gtx, call, image.Pt(x, y))
			x += d.Size.X + gap
		}
		ew := innerW - x
		if ew < dp(gtx, 140) && x > 0 {
			x, y, ew = 0, y+lineH+gap, innerW
		}
		at(gtx, image.Pt(x+dp(gtx, 4), y), func(gtx layout.Context) layout.Dimensions {
			ew := maxInt(1, ew-dp(gtx, 4))
			return vcenter(gtx, lineH, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Min: image.Pt(ew, 0), Max: image.Pt(ew, lineH)}
				if m.chQ.Len() == 0 && len(m.chans) == 0 {
					call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 15, theme.Regular, cl.Placeholder, nsInviteWS.T("Search channels"))
					})
					call.Add(gtx.Ops)
				}
				return th.Editor(gtx, &m.chQ, unit.Sp(15), cl.Fg, theme.WithAlpha(cl.Focus, 0x55))
			})
		})
		return layout.Dimensions{Size: image.Pt(innerW, y+lineH)}
	})
	hgt := maxInt(cd.Size.Y+2*padY, dp(gtx, 36))
	r := image.Rect(0, 0, w, hgt)
	actFocusBox(gtx, cl, r, gtx.Focused(&m.chQ))
	m.chBox.Layout(exact(gtx, r.Max), func(gtx layout.Context) layout.Dimensions {
		defer clip.Rect(r).Push(gtx.Ops).Pop()
		pointer.CursorText.Add(gtx.Ops)
		return layout.Dimensions{Size: r.Max}
	})
	macroAt(gtx, content, image.Pt(padX, (hgt-cd.Size.Y)/2))
	return layout.Dimensions{Size: r.Max}
}

// channelSuggestions are the channel searcher's options.
func (m *InviteModal) channelSuggestions(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	w := gtx.Constraints.Max.X
	rh, pad := dp(gtx, 36), dp(gtx, 6)
	r := image.Rect(0, 0, w, len(m.chMatch)*rh+2*pad)
	rrect(gtx, r, dp(gtx, 8), cl.Ring)
	rrect(gtx, r.Inset(border1(gtx)), dp(gtx, 8)-border1(gtx), cl.Bg)
	for i := range m.chMatch {
		if m.chSugg[i].Hovered() {
			m.chHL = i
		}
	}
	for i := range m.chMatch {
		c := m.chMatch[i]
		at(gtx, image.Pt(0, pad+i*rh), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(w, rh))
			return clickBox(gtx, &m.chSugg[i], nil, func(gtx layout.Context) layout.Dimensions {
				fg := cl.Fg
				if i == m.chHL {
					fg = cl.HighText
					fill(gtx, image.Rect(0, 0, w, rh), cl.Highlight)
				}
				ic := "channel"
				if c.Kind == "private" {
					ic = "lock"
				}
				at(gtx, image.Pt(dp(gtx, 16), (rh-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions { return icon(gtx, ic, 16, fg) })
				at(gtx, image.Pt(dp(gtx, 40), 0), func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-dp(gtx, 56)), rh)}
					return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 15, theme.Regular, fg, c.Name) })
				})
				return layout.Dimensions{Size: image.Pt(w, rh)}
			})
		})
	}
	return layout.Dimensions{Size: r.Max}
}

// guestWarning is ChannelSelect's maybeRenderGuestWarning.
func (m *InviteModal) guestWarning(gtx layout.Context, th *theme.Theme, cl *Colors, w int) layout.Dimensions {
	switch {
	case len(m.chans) > 1:
		y := dp(gtx, 8)
		at(gtx, image.Pt(0, y+dp(gtx, 3)), func(gtx layout.Context) layout.Dimensions { return icon(gtx, "info", 16, cl.Fg2) })
		d := at(gtx, image.Pt(dp(gtx, 24), y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w-dp(gtx, 24), 1<<20)}
			return richPara(gtx, th, cl.Fg, nsInviteWS.RT("<strong>Multi-channel guests</strong> will be billed as full members. Single channel guests are free but limited in quantity."))
		})
		return layout.Dimensions{Size: image.Pt(w, y+d.Size.Y)}
	case len(m.chans) == 1:
		y := dp(gtx, 8)
		box := dp(gtx, 16)
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, &m.mcBox, nil, func(gtx layout.Context) layout.Dimensions {
				r := image.Rect(0, dp(gtx, 3), box, dp(gtx, 3)+box)
				if m.typ == core.InviteMultiChannelGuest {
					rrect(gtx, r, dp(gtx, 4), cl.Link)
					at(gtx, r.Min, func(gtx layout.Context) layout.Dimensions { return icon(gtx, "check", 16, cl.HighText) })
				} else {
					rrect(gtx, r, dp(gtx, 4), cl.BtnBorder)
					rrect(gtx, r.Inset(border1(gtx)), dp(gtx, 4)-border1(gtx), cl.Bg)
				}
				d := at(gtx, image.Pt(box+dp(gtx, 8), 0), func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints = layout.Constraints{Max: image.Pt(w-box-dp(gtx, 8), 1<<20)}
					return richPara(gtx, th, cl.Fg, nsInviteWS.RT("Allow them to be added to more channels. Guests who can join multiple channels are <strong>billed as full members.</strong>"))
				})
				return layout.Dimensions{Size: image.Pt(w, d.Size.Y)}
			})
		})
		return layout.Dimensions{Size: image.Pt(w, y+d.Size.Y)}
	}
	return layout.Dimensions{}
}

// richPara draws parts as one 15/22 paragraph, <strong> runs bold (Clack
// lays out each run on its own; a run longer than the line wraps).
func richPara(gtx layout.Context, th *theme.Theme, c color.NRGBA, parts []i18n.Part) layout.Dimensions {
	w := gtx.Constraints.Max.X
	var words []struct {
		s    string
		bold bool
	}
	for _, p := range parts {
		for _, f := range strings.Fields(p.Text) {
			words = append(words, struct {
				s    string
				bold bool
			}{f, p.Tag == "strong" || p.Tag == "b"})
		}
	}
	x, y, lh := 0, 0, dp(gtx, 22)
	space := textW(gtx, th, 15, theme.Regular, " ")
	for _, wd := range words {
		wt := theme.Regular
		if wd.bold {
			wt = theme.Bold
		}
		ww := textW(gtx, th, 15, wt, wd.s)
		if x > 0 && x+ww > w {
			x, y = 0, y+lh
		}
		at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 15, wt, c, wd.s) })
		})
		x += ww + space
	}
	return layout.Dimensions{Size: image.Pt(w, y+lh)}
}

// textBox is a c-input_text (36px, radius 8, padding 4/12; minH > 0:
// a multi-line box).
func (m *InviteModal) textBox(gtx layout.Context, th *theme.Theme, cl *Colors, ed *widget.Editor, box *widget.Clickable, ph string, maxH float32) layout.Dimensions {
	w := gtx.Constraints.Max.X
	padX, padY := dp(gtx, 12), dp(gtx, 7)
	tw := maxInt(1, w-2*padX)
	mh := dp(gtx, 22)
	if maxH > 0 {
		mh = dp(gtx, maxH)
	}
	content, cd := record(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Min: image.Pt(tw, 0), Max: image.Pt(tw, mh)}
		if ed.Len() == 0 {
			call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, cl.Placeholder, ph)
			})
			call.Add(gtx.Ops)
		}
		return th.Editor(gtx, ed, unit.Sp(15), cl.Fg, theme.WithAlpha(cl.Focus, 0x55))
	})
	r := image.Rect(0, 0, w, maxInt(cd.Size.Y, dp(gtx, 22))+2*padY)
	actFocusBox(gtx, cl, r, gtx.Focused(ed))
	box.Layout(exact(gtx, r.Max), func(gtx layout.Context) layout.Dimensions {
		defer clip.Rect(r).Push(gtx.Ops).Pop()
		pointer.CursorText.Add(gtx.Ops)
		return layout.Dimensions{Size: r.Max}
	})
	macroAt(gtx, content, image.Pt(padX, padY))
	return layout.Dimensions{Size: r.Max}
}

// customizeButton is CustomizeButton: the magic-wand and "Add a message",
// a link button.
func (m *InviteModal) customizeButton(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	return clickBox(gtx, &m.custom, nil, func(gtx layout.Context) layout.Dimensions {
		lh := dp(gtx, 22)
		at(gtx, image.Pt(0, (lh-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions { return icon(gtx, "magic-wand", 20, cl.Link) })
		d := at(gtx, image.Pt(dp(gtx, 26), 0), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, cl.Link, nsInviteWS.T("Add a message"))
			})
		})
		return layout.Dimensions{Size: image.Pt(dp(gtx, 26)+d.Size.X, lh)}
	})
}

// formErrorText is InviteFormError's text (td).
func (m *InviteModal) formErrorText() string {
	switch m.formErr {
	case "":
		return ""
	case "create_shared_invite_link_failed", "list_shared_invite_links_failed":
		if m.haveLink {
			return ""
		}
		return nsInviteWS.T("Something went wrong. We couldn’t generate an invite link.")
	}
	return inviteErrorText(m.formErr, len(m.Tokens().invitable()), m.team)
}

// inviteBanner is a level "error" banner (VvO8): the warning icon and the
// text on the important surface.
func inviteBanner(gtx layout.Context, th *theme.Theme, cl *Colors, w int, txt string) layout.Dimensions {
	pad := dp(gtx, 12)
	call, d := record(loose(gtx, image.Pt(w-2*pad-dp(gtx, 28), 1<<20)), func(gtx layout.Context) layout.Dimensions {
		return para(gtx, th, 15, 22, theme.Regular, cl.Fg, txt, 0)
	})
	r := image.Rect(0, 0, w, d.Size.Y+2*pad)
	rrect(gtx, r, dp(gtx, 8), cl.Danger)
	rrect(gtx, r.Inset(border1(gtx)), dp(gtx, 8)-border1(gtx), woTok(th, "surf-imp"))
	at(gtx, image.Pt(pad, pad+(dp(gtx, 22)-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
		return icon(gtx, "warning", 20, cl.Danger)
	})
	macroAt(gtx, call, image.Pt(pad+dp(gtx, 28), pad))
	return layout.Dimensions{Size: r.Max}
}

// footerLeft is the footer's left: the invite link, or for a guest
// invite "Learn more about guests".
func (m *InviteModal) footerLeft(gtx layout.Context) layout.Dimensions {
	th := m.h.Theme()
	cl := colorsOf(th)
	lh := dp(gtx, 22)
	switch {
	case m.Guest():
		return clickBox(gtx, &m.learn, nil, func(gtx layout.Context) layout.Dimensions {
			fg := cl.Muted
			at(gtx, image.Pt(0, (lh-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions { return icon(gtx, "info", 20, fg) })
			d := at(gtx, image.Pt(dp(gtx, 28), 0), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 15, theme.Regular, fg, nsInviteWS.T("Learn more about guests"))
				})
			})
			return layout.Dimensions{Size: image.Pt(dp(gtx, 28)+d.Size.X, lh)}
		})
	case m.linkShown():
		fg := cl.Link // .p-shared_invite_link: sk_highlight
		if m.linkBusy {
			fg = cl.Subtle // --disabled
		}
		d := clickBox(gtx, &m.linkBtn, nil, func(gtx layout.Context) layout.Dimensions {
			at(gtx, image.Pt(0, (lh-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions { return icon(gtx, "link", 20, fg) })
			d := at(gtx, image.Pt(dp(gtx, 28), 0), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 15, theme.Regular, fg, nsInviteWS.T("Copy invite link"))
				})
			})
			return layout.Dimensions{Size: image.Pt(dp(gtx, 28)+d.Size.X, lh)}
		})
		Tooltip(gtx, th, &m.linkTip, m.linkBtn.Hovered(), d.Size, Above, m.linkTipText(), "")
		up := m.linkTip.Showing(gtx.Now)
		if m.linkTipUp && !up {
			m.linkCopied = false // the tooltip's onHide (q)
		}
		m.linkTipUp = up
		return d
	}
	return layout.Dimensions{}
}

// inviteErrorText is invite-errors' getInviteErrorText (kqqh) for the
// codes an invite to the workspace gets ("" when Slack has no text).
func inviteErrorText(code string, n int, team string) string {
	if n < 1 {
		n = 1
	}
	switch code {
	case "url_in_message":
		return nsInviteErrors.T("Sorry, but URLs are not allowed in the custom message. Please remove it and try again!")
	case "invalid_email":
		return nsInviteErrors.T("{inviteesCount, plural, =1 {That doesn’t} other {These don’t}} look like a valid email {inviteesCount, plural, =1 {address} other {addresses}}!", "inviteesCount", n)
	case "user_is_profile_only", "already_in_team", "already_in_team_invited_user":
		return nsInviteErrors.T("{inviteesCount, plural, =1 {This person is} other {These people are}} already in your workspace.", "inviteesCount", n)
	case "already_invited":
		return nsInviteErrors.T("{inviteesCount, plural, =1 {This person has} other {These people have}} already been invited to your workspace.", "inviteesCount", n)
	case "sent_recently":
		return nsInviteErrors.T("{inviteesCount, plural, =1 {This person was} other {These people were}} recently invited. No need to invite them again just yet.", "inviteesCount", n)
	case "inviter_email_not_verified":
		return nsInviteErrors.T("Your email address isn’t verified. Verify it, then try sending the invites again.")
	case "invite_failed":
		return nsInviteErrors.T("Something went wrong with {inviteesCount, plural, =1 {this invite} other {this invites}} :(", "inviteesCount", n)
	case "ura_limit_reached":
		return nsInviteErrors.T("You’ve reached your workspace limit for Single-Channel Guests. You must invite more paid members first.")
	case "user_limit_reached":
		return nsInviteErrors.T("You’ve reached the maximum number of users for this workspace.")
	case "not_allowed":
		return nsInviteErrors.T("You can’t invite this type of account based on your current SSO settings.")
	case "custom_message_not_allowed":
		return nsInviteErrors.T("Sorry, you can’t add a custom message to this invite. Please remove it and try again!")
	case "domain_mismatch":
		return nsInviteErrors.T("Your SSO settings prevent you from inviting people from this email domain.")
	case "invite_limit_reached":
		return nsInviteErrors.T("Your workspace has reached its invitation limit. Once people accept the existing invitations, you’ll be able to send out more.")
	case "requires_channel":
		return nsInviteErrors.T("Pick at least one channel before inviting Multi-Channel Guests.")
	case "requires_one_channel":
		return nsInviteErrors.T("Pick a channel before inviting Single-Channel Guests.")
	case "already_requested":
		return nsInviteErrors.T("You have already requested an invite for {inviteesCount, plural, =1 {this person} other {these people}}. We’ll update you when an admin approves or denies this request.", "inviteesCount", n)
	case "existing_request":
		return nsInviteErrors.T("Someone has already requested an invite for {inviteesCount, plural, =1 {this person} other {these people}}. We’ll update you when an admin approves or denies this request.", "inviteesCount", n)
	case "channel_not_found", "method_not_supported_for_channel_type":
		return nsInviteErrors.T("A valid channel name is required.")
	case "no_urls_allowed":
		return nsInviteErrors.T("Unfortunately, custom messages can’t contain URLs.")
	case "no_perms":
		return nsInviteErrors.T("You can’t invite {inviteesCount, plural, =1 {this person} other {these people}} based on your organization’s settings.", "inviteesCount", n)
	}
	_ = team
	return ""
}

// ---- ConfirmationScreen ----

// InviteSummary is the confirmation after a send.
type InviteSummary struct {
	h          Host
	modal      *Modal
	res        []core.InviteResult
	requesting bool
	tips       []TipState
	rows       []widget.Clickable
	past       widget.Clickable
	admin      bool
}

func openInviteSummary(h Host, source, team string, res []core.InviteResult, requesting bool, chans []core.Conv) *InviteSummary {
	s := &InviteSummary{h: h, res: res, requesting: requesting, tips: make([]TipState, len(res)), rows: make([]widget.Clickable, len(res))}
	if a := h.Core(); a != nil {
		s.admin = a.Self().IsAdmin || a.Self().IsOwner
	}
	more := &ModalButton{Label: nsAddToSlack.T("Invite More People"), Kind: BtnOutline, Do: func() bool {
		openInvite(h, source, chans) // resetModalState({isClosing: false}) keeps the initial channels
		return true
	}}
	done := &ModalButton{Label: nsAddToSlack.T("Done"), Kind: BtnPrimary, Do: func() bool { return true }}
	// .p-add_to_slack_confirmation__base_modal hides the header, the
	// close button with it (Escape and Done close it)
	s.modal = NewModal(h, ModalOpts{Width: 520, HeadH: 8, NoClose: true, Body: s.layout, Buttons: []*ModalButton{more, done}, AutoFocus: done,
		FooterLeft: s.footerLeft})
	h.ShowOverlay(s)
	return s
}

func (s *InviteSummary) Layout(gtx layout.Context) layout.Dimensions { return s.modal.Layout(gtx) }
func (s *InviteSummary) Dismissed() bool                             { return s.modal.Dismissed() }
func (s *InviteSummary) Dismiss()                                    { s.modal.Dismiss() }

// Results are the outcomes shown.
func (s *InviteSummary) Results() []core.InviteResult { return s.res }

// Title is getWorkspaceInviteTitle: "Sent", "Unable to send all",
// "Unable to send".
func (s *InviteSummary) Title() string {
	ok, bad := 0, 0
	for _, r := range s.res {
		if r.OK {
			ok++
		} else {
			bad++
		}
	}
	switch {
	case ok > 0 && bad == 0:
		return nsAddToSlack.T("Sent")
	case ok > 0:
		return nsAddToSlack.T("Unable to send all")
	}
	return nsAddToSlack.T("Unable to send")
}

// ordered is the list: the failures grouped by error, then the successes
// (InvitesSummaryList).
func (s *InviteSummary) ordered() []int {
	var bad, good []int
	for i, r := range s.res {
		if r.OK {
			good = append(good, i)
		} else {
			bad = append(bad, i)
		}
	}
	sort.SliceStable(bad, func(a, b int) bool { return s.res[bad[a]].Error < s.res[bad[b]].Error })
	return append(bad, good...)
}

// Status is a row's status text and tooltip (InviteListEntity J and its
// tooltipTextContent).
func (s *InviteSummary) Status(i int) (status, tip string) {
	r := s.res[i]
	switch {
	case !r.OK:
		status = nsAddToSlack.T("Couldn’t invite")
		if tip = inviteErrorText(r.Error, 1, ""); tip == "" {
			tip = nsAddToSlack.T("Hmm, something went wrong. Try again?")
		}
	case r.Type.Guest():
		status = nsAddToSlack.T("Invited as a guest")
		if s.requesting {
			tip = nsAddToSlack.T("Your admin has to review this first.")
		} else {
			mc := 0
			if r.Type == core.InviteMultiChannelGuest {
				mc = 1
			}
			tip = nsAddToSlack.T("They’ll be automatically added to the selected {isMultiChannelGuest, plural, =1 {channels} other {channel}} once they join.", "isMultiChannelGuest", mc)
		}
	default:
		status = nsAddToSlack.T("Invited as a coworker")
		if s.requesting {
			tip = nsAddToSlack.T("Your admin has to review this first.")
		} else {
			tip = nsAddToSlack.T("They’re not on Slack yet, but you can still message them (they’ll be able to see all direct messages sent to them once they’ve joined).")
		}
	}
	return status, tip
}

func (s *InviteSummary) layout(gtx layout.Context) layout.Dimensions {
	th := s.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	if s.past.Clicked(gtx) {
		if a := s.h.Core(); a != nil {
			srOpenLink(s.h, a.C.Creds.TeamURL+"admin/invites#pending")
		}
	}
	allOK := true
	for _, r := range s.res {
		allOK = allOK && r.OK
	}
	y := dp(gtx, 32) // title_icon margin-top
	// the 60px circle: success green, else the warning orange (26% tint)
	ic, col := "warning", rgb(0xe8912d)
	if allOK && len(s.res) > 0 {
		ic, col = "check", rgb(0x007a5a)
	}
	d60 := dp(gtx, 60)
	cr := image.Rect((w-d60)/2, y, (w+d60)/2, y+d60)
	rrect(gtx, cr, d60/2, theme.Over(theme.WithAlpha(col, 0x26), cl.ModalBg))
	at(gtx, image.Pt(cr.Min.X+(d60-dp(gtx, 30))/2, cr.Min.Y+(d60-dp(gtx, 30))/2), func(gtx layout.Context) layout.Dimensions {
		return icon(gtx, ic, 30, col)
	})
	y += d60 + dp(gtx, 12)
	call, d := record(loose(gtx, image.Pt(w, 1<<20)), func(gtx layout.Context) layout.Dimensions {
		return text(gtx, th, 22, theme.Black, cl.Fg, s.Title())
	})
	macroAt(gtx, call, image.Pt((w-d.Size.X)/2, y))
	y += d.Size.Y + dp(gtx, 16)
	for _, i := range s.ordered() {
		i := i
		dd := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions { return s.row(gtx, th, cl, w, i) })
		y += dd.Size.Y
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// row is InviteListEntity: the 34px icon tile (paper-plane; a failure's
// warning on raspberry), the address in bold, the status at the right
// (13px, 70% foreground; raspberry for a failure) with an info icon,
// 6px above and below; the tooltip on hover.
func (s *InviteSummary) row(gtx layout.Context, th *theme.Theme, cl *Colors, w, i int) layout.Dimensions {
	r := s.res[i]
	status, tip := s.Status(i)
	tile := dp(gtx, 34)
	h := tile + dp(gtx, 12)
	d := clickBox(gtx, &s.rows[i], nil, func(gtx layout.Context) layout.Dimensions {
		y := dp(gtx, 6)
		tr := image.Rect(0, y, tile, y+tile)
		if r.OK {
			rrect(gtx, tr, dp(gtx, 4), theme.Over(theme.WithAlpha(cl.Fg, 0x0a), cl.ModalBg))
			at(gtx, image.Pt((tile-dp(gtx, 18))/2, y+(tile-dp(gtx, 18))/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, "paper-plane", 18, cl.Fg)
			})
		} else {
			rrect(gtx, tr, dp(gtx, 4), rgb(0xe01e5a))
			at(gtx, image.Pt((tile-dp(gtx, 18))/2, y+(tile-dp(gtx, 18))/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, "warning", 18, rgb(0xffffff))
			})
		}
		sc := theme.Over(theme.WithAlpha(cl.Fg, 0xb3), cl.ModalBg)
		if !r.OK {
			sc = rgb(0xe01e5a)
		}
		sw := textW(gtx, th, 13, theme.Regular, status) + dp(gtx, 4) + dp(gtx, 13)
		at(gtx, image.Pt(w-sw, 0), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 13, theme.Regular, sc, status) })
		})
		at(gtx, image.Pt(w-dp(gtx, 13), (h-dp(gtx, 13))/2), func(gtx layout.Context) layout.Dimensions { return icon(gtx, "info", 13, sc) })
		at(gtx, image.Pt(tile+dp(gtx, 12), 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-sw-tile-dp(gtx, 24)), h)}
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 15, theme.Bold, cl.Fg, r.Email) })
		})
		return layout.Dimensions{Size: image.Pt(w, h)}
	})
	Tooltip(gtx, th, &s.tips[i], s.rows[i].Hovered(), d.Size, Above, tip, "")
	return d
}

// footerLeft is maybeRenderWorkspaceInviteManageInvitations: "See past
// invites" for who can view pending invites (admins).
func (s *InviteSummary) footerLeft(gtx layout.Context) layout.Dimensions {
	if !s.admin {
		return layout.Dimensions{}
	}
	th := s.h.Theme()
	cl := colorsOf(th)
	fg := theme.Over(theme.WithAlpha(cl.Fg, 0xb3), cl.ModalBg)
	lh := dp(gtx, 22)
	return clickBox(gtx, &s.past, nil, func(gtx layout.Context) layout.Dimensions {
		at(gtx, image.Pt(0, (lh-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions { return icon(gtx, "paper-plane", 16, fg) })
		d := at(gtx, image.Pt(dp(gtx, 20)+dp(gtx, 16), 0), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, fg, nsAddToSlack.T("See past invites"))
			})
		})
		return layout.Dimensions{Size: image.Pt(dp(gtx, 36)+d.Size.X, lh)}
	})
}
