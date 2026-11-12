package core

// Inviting people to the workspace: Slack's InviteToWorkspaceModal (gantry-
// v2-async-client-boot-shared module CJ8h, build 4d0c78085232; docs/
// INVITES.md). This file is the modal's rules and its API; the dialog is
// internal/ui/popups/invite.go.
//
//   - Who sees the modal (client-boot c1OI canUserSeeInviteModal): anyone
//     who may invite (canUserInviteMembersToWorkspace) or ask an admin to
//     (canUserRequestToInvite). Guests and frontline members never do.
//   - Whether a send is a request for an admin (c1OI isRequestingInvites):
//     a member invite from a non-admin while the team pref
//     invites_only_admins is on; a guest invite from anyone who can't
//     invite guests.
//   - Direct invites go to users.admin.inviteBulk (FIV8), requests to
//     users.inviteRequests.create (b389), both from boot-shared 6FNz
//     (sendInvites → submitInvites / submitInviteRequests). Slack's
//     createApiMethodFetcher (WH55) snake-cases the arguments; the
//     invites and requests are InviteBulkApiInvite records (Smwf tw:
//     email, mode "manual", type) as JSON.
//   - The invite link (CJ8h SharedInviteLink, G3qv, 3Lll): the viewer's
//     newest live link from users.sharedInvites.list (mode
//     last_active_for_user), else one minted with
//     users.admin.createSharedInvite (30 days, 400 sign-ups).
//
// Clack has no role permissions (Slack's QaSy role grants: INVITE_GUEST,
// ADD_FULL_USER_TO_WORKSPACE), so a workspace admin or owner stands for
// them, as Slack's own fallback canUserActAsWorkspaceAdmin (7RIn Me) does.

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// InviteType is an invite's account type (Slack's FTX5 BD).
type InviteType string

const (
	InviteMember             InviteType = "regular"
	InviteMultiChannelGuest  InviteType = "restricted"
	InviteSingleChannelGuest InviteType = "ultra_restricted"
)

// Guest reports a guest invite.
func (t InviteType) Guest() bool {
	return t == InviteMultiChannelGuest || t == InviteSingleChannelGuest
}

// InviteResult is one address's outcome (Slack's FTX5 InviteResult).
type InviteResult struct {
	Email string
	OK    bool
	Error string // Slack's error code ("already_in_team", …)
	Type  InviteType
	// Requested: sent to an admin for approval (users.inviteRequests.create).
	Requested bool
}

// InviteOpts is one send from the modal.
type InviteOpts struct {
	Emails   []string
	Type     InviteType
	Channels []string // channel ids
	// Message is the custom message (a direct invite's extra_message) or
	// the reason for the request (requester_message).
	Message string
	// Expires is a guest invite's expiration_ts (Unix seconds; 0: none):
	// 11:59 PM on the day the expiration select picked (6FNz
	// getExpirationTs).
	Expires int64
	// Source is the entry point (Smwf K3: team_menu,
	// sidebar_create_new_invite_people, huddles_page), sent as campaign.
	Source string
}

// InviteError is a send that failed as a whole: Code is Slack's error;
// Results, when Slack listed the requests (users.inviteRequests.create's
// requests), are each address's outcome.
type InviteError struct {
	Code    string
	Results []InviteResult
}

func (e *InviteError) Error() string { return "invite: " + e.Code }

// truthy is JavaScript truthiness of a JSON value (Slack reads team prefs
// with getTeamPref and tests them with !!).
func truthy(v json.RawMessage) bool {
	s := strings.TrimSpace(string(v))
	switch s {
	case "", "null", "false", "0", `""`, "-0":
		return false
	}
	return true
}

// teamPrefOn is !!getTeamPref(state, name).
func (a *App) teamPrefOn(name string) bool {
	v, ok := a.TeamPref(name)
	return ok && truthy(v)
}

// inviteSelf is the viewer's store record.
func (a *App) inviteSelf() slack.User {
	a.S.RLock()
	defer a.S.RUnlock()
	if u := a.S.Users[a.S.Self.ID]; u != nil {
		return *u
	}
	return a.S.Self
}

// selfIsWorkspaceAdmin is canUserActAsWorkspaceAdmin (7RIn Me): a
// workspace admin or owner.
func (a *App) selfIsWorkspaceAdmin() bool {
	u := a.inviteSelf()
	return u.IsAdmin || u.IsOwner
}

// selfIsGuestOrFrontline is wY11 P || u+N9 W: a guest (is_restricted)
// or a frontline member.
func (a *App) selfIsGuestOrFrontline() bool {
	u := a.inviteSelf()
	return u.IsRestricted || u.IsUltraRestricted || u.RestrictedUserType == "frontline_worker"
}

// CanInviteGuests is c1OI canUserInviteGuests: the INVITE_GUEST grant
// (an admin or owner here).
func (a *App) CanInviteGuests() bool { return a.selfIsWorkspaceAdmin() }

// CanInviteMembers is c1OI canUserInviteMembersToWorkspace: an admin, or
// a full member while invites_only_admins is off (or who may invite
// guests).
func (a *App) CanInviteMembers() bool {
	if a.selfIsWorkspaceAdmin() {
		return true
	}
	return !a.selfIsGuestOrFrontline() && (!a.teamPrefOn("invites_only_admins") || a.CanInviteGuests())
}

// CanRequestInvites is c1OI canUserRequestToInvite: a full member while
// invite_requests_enabled is on.
func (a *App) CanRequestInvites() bool {
	return !a.selfIsGuestOrFrontline() && a.teamPrefOn("invite_requests_enabled")
}

// CanSeeInviteModal is c1OI canUserSeeInviteModal: the team menu's
// "Invite people to …", the Create menu's "Invite people" and the
// Huddles page's "Add coworkers" open the modal only then.
func (a *App) CanSeeInviteModal() bool { return a.CanRequestInvites() || a.CanInviteMembers() }

// CanSeeAccountTypeOptions is c1OI canUserSeeAccountTypeOptions: the
// "Invite as" select offers Guest (not on a free plan).
func (a *App) CanSeeAccountTypeOptions() bool {
	return !a.OnFreePlan() && (a.CanRequestInvites() || a.CanInviteGuests())
}

// IsRequestingInvites is c1OI isRequestingInvitesForType: the send asks an
// admin (users.inviteRequests.create) instead of inviting.
func (a *App) IsRequestingInvites(t InviteType) bool {
	switch t {
	case InviteMember, "":
		if a.selfIsWorkspaceAdmin() {
			return false
		}
		return a.teamPrefOn("invites_only_admins")
	case InviteMultiChannelGuest, InviteSingleChannelGuest:
		return !a.CanInviteGuests()
	}
	return true
}

// IsEnterprise is PDkO k: the team is on Enterprise Grid (Slack's invite
// modal then has no invite link and allows URLs in the custom message).
func (a *App) IsEnterprise() bool {
	if a.C.Creds.EnterpriseID != "" {
		return true
	}
	t := a.teamPlan()
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.enterpriseID != ""
}

// MemberIDByEmail is the invite form's membership annotator
// (FShz useWorkspaceMembershipAnnotator) over the members Clack has
// loaded: the id of the active, non-bot member with that email ("" when
// none). Slack asks its member searcher; Clack knows only the profiles in
// its store (on Hack Club, emails other than the viewer's are hidden).
func (a *App) MemberIDByEmail(email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return ""
	}
	a.S.RLock()
	defer a.S.RUnlock()
	for id, u := range a.S.Users {
		if u == nil || u.Deleted || u.IsBot || u.IsRestricted && u.RestrictedUserType == "external_guest" {
			continue
		}
		if strings.ToLower(strings.TrimSpace(u.Profile.Email)) == email {
			return id
		}
	}
	return ""
}

// inviteRecord is InviteBulkApiInvite (Smwf tw) as JSON.stringify writes
// it: undefined fields (real_name, invite_source) left out.
type inviteRecord struct {
	Email string     `json:"email"`
	Mode  string     `json:"mode"`
	Type  InviteType `json:"type"`
}

func inviteRecords(o InviteOpts) string {
	recs := make([]inviteRecord, 0, len(o.Emails))
	for _, e := range o.Emails {
		recs = append(recs, inviteRecord{Email: e, Mode: "manual", Type: o.Type})
	}
	b, _ := json.Marshal(recs)
	return string(b)
}

func boolArg(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// inviteParams are the arguments both methods share (6FNz sendInvites:
// teamId, restricted, ultraRestricted, campaign, channels).
func (a *App) inviteParams(o InviteOpts) url.Values {
	p := url.Values{}
	if team := a.C.Creds.Team; team != "" {
		p.Set("team_id", team)
	}
	p.Set("restricted", boolArg(o.Type == InviteMultiChannelGuest))
	p.Set("ultra_restricted", boolArg(o.Type == InviteSingleChannelGuest))
	if o.Source != "" {
		p.Set("campaign", o.Source)
	}
	if len(o.Channels) > 0 {
		p.Set("channels", strings.Join(o.Channels, ","))
	}
	if o.Expires > 0 {
		p.Set("expiration_ts", strconv.FormatInt(o.Expires, 10))
	}
	return p
}

type inviteRow struct {
	Email string `json:"email"`
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

// SendInvites is 6FNz sendInvites: a request for an admin when
// IsRequestingInvites, else the invites. The results are in o.Emails'
// order; an *InviteError is a send that failed as a whole.
func (a *App) SendInvites(ctx context.Context, o InviteOpts) ([]InviteResult, error) {
	if o.Type == "" {
		o.Type = InviteMember
	}
	if len(o.Emails) == 0 {
		return nil, errors.New("invite: no addresses")
	}
	if a.IsRequestingInvites(o.Type) {
		return a.requestInvites(ctx, o)
	}
	p := a.inviteParams(o)
	p.Set("invites", inviteRecords(o))
	if o.Message != "" {
		p.Set("extra_message", o.Message)
	}
	var r struct {
		Invites []inviteRow `json:"invites"`
	}
	if err := a.C.Call(ctx, "users.admin.inviteBulk", p, &r); err != nil {
		var se *slack.Error
		if errors.As(err, &se) {
			return nil, &InviteError{Code: se.Code}
		}
		return nil, err
	}
	out := make([]InviteResult, 0, len(r.Invites))
	for _, v := range r.Invites {
		out = append(out, InviteResult{Email: v.Email, OK: v.OK, Error: v.Error, Type: o.Type})
	}
	return out, nil
}

// requestInvites is 6FNz submitInviteRequests: users.inviteRequests.create.
// Success means every request went to the admins; a failure may list
// each request's outcome.
func (a *App) requestInvites(ctx context.Context, o InviteOpts) ([]InviteResult, error) {
	p := a.inviteParams(o)
	p.Set("requests", inviteRecords(o))
	if o.Message != "" {
		p.Set("requester_message", o.Message)
	}
	if err := a.C.Call(ctx, "users.inviteRequests.create", p, nil); err != nil {
		var se *slack.Error
		if !errors.As(err, &se) {
			return nil, err
		}
		ie := &InviteError{Code: se.Code}
		var body struct {
			Requests []inviteRow `json:"requests"`
		}
		if json.Unmarshal(se.Body, &body) == nil {
			for _, v := range body.Requests {
				ie.Results = append(ie.Results, InviteResult{Email: v.Email, OK: v.OK, Error: v.Error, Type: o.Type, Requested: v.OK})
			}
		}
		return nil, ie
	}
	out := make([]InviteResult, 0, len(o.Emails))
	for _, e := range o.Emails {
		out = append(out, InviteResult{Email: e, OK: true, Type: o.Type, Requested: true})
	}
	return out, nil
}

// GuestExpiry is getExpirationTs for an option of days days from today
// in loc: 11:59:59 PM that day (0 days: no limit, 0).
func GuestExpiry(now time.Time, loc *time.Location, days int) int64 {
	if days <= 0 {
		return 0
	}
	if loc == nil {
		loc = time.Local
	}
	t := now.In(loc)
	y, m, d := t.Date()
	return time.Date(y, m, d+days, 23, 59, 59, 0, loc).Unix()
}

// SharedInviteLink is a workspace invite link (Smwf SharedInviteLink).
type SharedInviteLink struct {
	Code, URL, ShortURL string
	InviterID           string
	Expires             int64 // date_expire (Unix seconds)
	MaxSignups          int
}

// ShareURL is getShareableInviteUrl: the short link when there is one.
func (l SharedInviteLink) ShareURL() string {
	if l.ShortURL != "" {
		return l.ShortURL
	}
	return l.URL
}

// DaysLeft is getDaysRemainingOnLink (differenceInDays, BWLe): whole days
// from the start of today to the expiry. A link without an expiry counts
// as 366, which Slack's copy confirmation reads as "never expires".
func (l SharedInviteLink) DaysLeft(now time.Time) int {
	if l.Expires == 0 {
		return 366
	}
	y, m, d := now.Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	return int(time.Unix(l.Expires, 0).Sub(start) / (24 * time.Hour))
}

type sharedInviteJSON struct {
	Code       string `json:"code"`
	URL        string `json:"url"`
	ShortURL   string `json:"short_url"`
	InviterID  string `json:"inviter_id"`
	DateExpire int64  `json:"date_expire"`
	DateDelete int64  `json:"date_delete"`
	MaxSignups int    `json:"max_signups"`
	NumSignups int    `json:"num_signups"`
}

func (j sharedInviteJSON) link() SharedInviteLink {
	return SharedInviteLink{Code: j.Code, URL: j.URL, ShortURL: j.ShortURL, InviterID: j.InviterID, Expires: j.DateExpire, MaxSignups: j.MaxSignups}
}

// CurrentInviteLink is G3qv getCurrentSharedInviteLink: the viewer's
// newest link that isn't deleted or expired (ok false: none).
func (a *App) CurrentInviteLink(ctx context.Context) (SharedInviteLink, bool, error) {
	var r struct {
		Invites []sharedInviteJSON `json:"invites"`
	}
	if err := a.C.Call(ctx, "users.sharedInvites.list", url.Values{"mode": {"last_active_for_user"}}, &r); err != nil {
		return SharedInviteLink{}, false, err
	}
	self, now := a.S.SelfID(), time.Now().Unix()
	for _, v := range r.Invites {
		if v.InviterID == self && v.DateDelete == 0 && v.DateExpire > now {
			return v.link(), true, nil
		}
	}
	return SharedInviteLink{}, false, nil
}

// CreateInviteLink is CJ8h createSharedInviteLink's
// users.admin.createSharedInvite: a link for 30 days and 400 sign-ups.
func (a *App) CreateInviteLink(ctx context.Context) (SharedInviteLink, error) {
	var r sharedInviteJSON
	err := a.C.Call(ctx, "users.admin.createSharedInvite", url.Values{"expiration": {"30"}, "max_signups": {"400"}}, &r)
	if err != nil {
		return SharedInviteLink{}, err
	}
	return r.link(), nil
}
