package fakeslack

// Directories › External and Invitations (Slack's Slack Connect hub):
// Flannel's externalTeams/list and externalTeams/counts, the hub's
// connectInvites.list (invite_types channel and im, include_invite_requests),
// the invite writes it sends (conversations.revokeSharedInvite,
// sharedInvites.revoke), sharedInvites.canGetLink and the channel creation
// permissions the hub asks; for the organization page,
// users.slackConnect.orgInfo, edge users/counts for its channels and the
// channels/search and users/search an external_team scopes. Nothing is set up by default: a test gives the
// fake its organizations and invitations with SetSlackConnectHub.

import (
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/some-du6e/clack/internal/slack"
)

// ExternalTeamFixture is an organization the viewer is connected to (or
// could be), with its channel counts.
type ExternalTeamFixture struct {
	ID, Name, Domain                                   string
	Verified                                           bool
	PublicMemberOf, PublicNotMemberOf, PrivateMemberOf int
}

// HubInvite is a connect invite as connectInvites.list answers it.
type HubInvite struct {
	ID, Sig        string
	Kind           string // "channel" | "im"
	Direction      string // "incoming" | "outgoing"
	Status         string // "sent", "revoked", "expired", "requested", ...
	RecipientEmail string // an email invite's; a link invite has a Link
	Link           string
	ChannelID      string
	ChannelName    string
	Message        string
	DateCreated    int64
	DateInvalid    int64
	// Inviter and InvitingTeam: who sent it (a received invite's sender is
	// outside; a sent one's is the viewer).
	InviterID, InviterName, InviterEmail string
	TeamID, TeamName                     string
	TeamVerified                         bool
	// Acceptances: who took it up and where it stands.
	Acceptances []HubAcceptance
}

// HubAcceptance is an invite acceptance.
type HubAcceptance struct {
	UserID, UserName, TeamID, TeamName string
	Status                             string // pending_approval, approved, rejected, expired
	DateAccepted                       int64
}

// SlackConnectHub is what the fake's hub answers.
type SlackConnectHub struct {
	Connected, Available []ExternalTeamFixture
	Invites              []HubInvite
	DMEligible           bool // sharedInvites.canGetLink
	NoCreateChannel      bool // permissions/info leaves out CREATE_PUBLIC_CHANNEL / CREATE_PRIVATE_CHANNEL
	// Orgs are the organization pages (users.slackConnect.orgInfo) by
	// team id; a team in Connected or Available without one has no
	// channels.
	Orgs map[string]HubOrg
}

// HubOrg is an organization page's reads: orgInfo's channel_ids, the
// channels' edge users/counts, and Fail for an orgInfo that errors.
type HubOrg struct {
	ChannelIDs []string
	Counts     map[string]HubCount
	Fail       bool
}

// HubCount is a channel's users/counts: people and by_team.
type HubCount struct {
	People int
	ByTeam map[string]int
}

type scHub struct {
	mu     sync.Mutex
	h      SlackConnectHub
	forms  map[string][]url.Values
	marker map[string]bool // externalTeams/list asked with a marker
}

var scHubIndex = newPerServer()

func (s *Server) scHub() *scHub {
	v, _ := scHubIndex.LoadOrStore(s, &scHub{forms: map[string][]url.Values{}})
	return v.(*scHub)
}

// SetSlackConnectHub gives the fake its organizations and invitations.
func (s *Server) SetSlackConnectHub(h SlackConnectHub) {
	x := s.scHub()
	x.mu.Lock()
	x.h = h
	x.mu.Unlock()
}

// SlackConnectForms are the forms (an edge call's body as values) the hub's
// calls sent, by method, in order.
func (s *Server) SlackConnectForms(method string) []url.Values {
	x := s.scHub()
	x.mu.Lock()
	defer x.mu.Unlock()
	return append([]url.Values(nil), x.forms[method]...)
}

// HubInviteNow is invite id as the fake has it now.
func (s *Server) HubInviteNow(id string) (HubInvite, bool) {
	x := s.scHub()
	x.mu.Lock()
	defer x.mu.Unlock()
	for _, inv := range x.h.Invites {
		if inv.ID == id {
			return inv, true
		}
	}
	return HubInvite{}, false
}

func edgeForm(edge map[string]json.RawMessage) url.Values {
	v := url.Values{}
	for k, raw := range edge {
		if k == "token" || k == "enterprise_token" {
			continue
		}
		var s string
		if json.Unmarshal(raw, &s) == nil {
			v.Set(k, s)
			continue
		}
		v.Set(k, string(raw))
	}
	return v
}

func (x *scHub) note(method string, f url.Values) {
	x.forms[method] = append(x.forms[method], f)
}

func (s *Server) handleSlackConnectHub(method string, f url.Values, edge map[string]json.RawMessage) (any, bool) {
	x := s.scHub()
	switch method {
	case "edge/externalTeams/list":
		x.mu.Lock()
		defer x.mu.Unlock()
		x.note(method, edgeForm(edge))
		var filter, marker string
		var count int
		_ = json.Unmarshal(edge["filter"], &filter)
		_ = json.Unmarshal(edge["marker"], &marker)
		_ = json.Unmarshal(edge["count"], &count)
		teams := x.h.Available
		if filter == "connected" {
			teams = x.h.Connected
		}
		from := 0
		if strings.HasPrefix(marker, "etm:") {
			from, _ = strconv.Atoi(strings.TrimPrefix(marker, "etm:"))
		}
		from = min(max(from, 0), len(teams))
		to := len(teams)
		if count > 0 {
			to = min(from+count, len(teams))
		}
		out := []map[string]any{}
		for _, t := range teams[from:to] {
			out = append(out, externalTeamJSON(t))
		}
		res := ok("results", out)
		if to < len(teams) {
			res["next_marker"] = "etm:" + strconv.Itoa(to)
		}
		return res, true
	case "edge/externalTeams/search":
		x.mu.Lock()
		defer x.mu.Unlock()
		x.note(method, edgeForm(edge))
		var q string
		_ = json.Unmarshal(edge["query"], &q)
		out := []map[string]any{}
		for _, t := range append(slices.Clone(x.h.Connected), x.h.Available...) {
			if strings.Contains(strings.ToLower(t.Name), strings.ToLower(q)) {
				out = append(out, externalTeamJSON(t))
			}
		}
		return ok("results", out), true
	case "edge/externalTeams/counts":
		x.mu.Lock()
		defer x.mu.Unlock()
		x.note(method, edgeForm(edge))
		var ids []string
		_ = json.Unmarshal(edge["team_ids"], &ids)
		counts := map[string]any{}
		for _, t := range append(slices.Clone(x.h.Connected), x.h.Available...) {
			if slices.Contains(ids, t.ID) {
				counts[t.ID] = map[string]any{"public_channels_member_of": t.PublicMemberOf, "public_channels_not_member_of": t.PublicNotMemberOf,
					"private_channels_member_of": t.PrivateMemberOf}
			}
		}
		return ok("counts", counts), true
	case "edge/permissions/info":
		var perms []string
		_ = json.Unmarshal(edge["permissions"], &perms)
		if !slices.Contains(perms, "CREATE_PUBLIC_CHANNEL") {
			return nil, false
		}
		x.mu.Lock()
		defer x.mu.Unlock()
		allowed := []string{}
		if !x.h.NoCreateChannel {
			allowed = []string{"CREATE_PUBLIC_CHANNEL", "CREATE_PRIVATE_CHANNEL"}
		}
		return ok("allowed", allowed, "allowed_per_team", map[string]any{TeamID: allowed}), true
	case "users.slackConnect.orgInfo":
		x.mu.Lock()
		defer x.mu.Unlock()
		x.note(method, f)
		id := f.Get("team_id")
		org := x.h.Orgs[id]
		if org.Fail {
			return map[string]any{"ok": false, "error": "fatal_error"}, true
		}
		for _, t := range append(slices.Clone(x.h.Connected), x.h.Available...) {
			if t.ID == id {
				return ok("team_summary", externalTeamJSON(t), "channel_ids", append([]string{}, org.ChannelIDs...)), true
			}
		}
		return map[string]any{"ok": false, "error": "team_not_found"}, true
	case "edge/users/counts":
		var ch string
		_ = json.Unmarshal(edge["channel"], &ch)
		x.mu.Lock()
		defer x.mu.Unlock()
		for _, org := range x.h.Orgs {
			if n, found := org.Counts[ch]; found {
				x.note(method, edgeForm(edge))
				return ok("counts", map[string]any{"people": n.People, "members": n.People, "by_team": n.ByTeam}), true
			}
		}
		return nil, false
	case "edge/channels/search", "edge/users/search":
		var team, q string
		_ = json.Unmarshal(edge["external_team"], &team)
		if team == "" {
			return nil, false
		}
		_ = json.Unmarshal(edge["query"], &q)
		q = strings.ToLower(q)
		x.mu.Lock()
		x.note(method, edgeForm(edge))
		x.mu.Unlock()
		out := []map[string]any{}
		if method == "edge/users/search" {
			for _, u := range append(slices.Clone(s.users), s.variants()...) { // the fixed people (Partner Co's) are variants
				if u.TeamID == team && !u.Deleted && strings.Contains(strings.ToLower(u.Name+" "+u.RealName), q) {
					out = append(out, map[string]any{"id": u.ID, "team_id": u.TeamID, "name": u.Name, "real_name": u.RealName,
						"is_bot": u.IsBot, "profile": map[string]any{"real_name": u.RealName, "display_name": u.Profile.DisplayName}})
				}
			}
			return ok("results", out), true
		}
		s.convMu.Lock()
		all := append(slices.Clone(s.convs), s.others...)
		s.convMu.Unlock()
		for _, c := range all {
			if slices.Contains(c.ConnectedTeamIDs, team) && !c.IsArchived && strings.Contains(strings.ToLower(c.Name), q) {
				out = append(out, map[string]any{"id": c.ID, "name": c.Name, "is_channel": c.IsChannel, "is_private": c.IsPrivate,
					"is_member": c.IsMember, "num_members": c.NumMembers})
			}
		}
		return ok("results", out), true
	case "sharedInvites.canAccept", "sharedInvites.accept", "sharedInvites.ignoreInvite", "conversations.ignoreInvite",
		"sharedInvites.ignoreUser", "conversations.ignoreUser":
		// a received hub invite's Accept and Ignore (by sig for canAccept,
		// else by invite id); Activity's own invites stay activity.go's
		x.mu.Lock()
		defer x.mu.Unlock()
		for i := range x.h.Invites {
			inv := &x.h.Invites[i]
			if inv.Direction != "incoming" || method == "sharedInvites.canAccept" && inv.Sig != f.Get("sig") ||
				method != "sharedInvites.canAccept" && inv.ID != f.Get("invite") {
				continue
			}
			x.note(method, f)
			switch method {
			case "sharedInvites.canAccept":
				return ok("is_eligible", true), true
			case "sharedInvites.accept":
				inv.Status = "accepted"
				return ok("channel", "D0PARTNER1"), true
			}
			inv.Status = "ignored"
			return ok(), true
		}
		return nil, false
	case "sharedInvites.canGetLink":
		x.mu.Lock()
		defer x.mu.Unlock()
		x.note(method, f)
		return ok("is_eligible", x.h.DMEligible), true
	case "connectInvites.list":
		// the hub's reads (PC2X, McFF, +Zmg): no invite_ids; Activity's
		// (an incoming direction alone, or invite_ids) stay activity.go's
		if f.Has("invite_ids") || f.Get("direction") == "incoming" && !f.Has("include_invite_requests") {
			return nil, false
		}
		x.mu.Lock()
		defer x.mu.Unlock()
		x.note(method, f)
		out := []map[string]any{}
		for _, inv := range x.h.Invites {
			if d := f.Get("direction"); d != "" && inv.Direction != d {
				continue
			}
			if f.Get("only_pending_invites") == "true" && inv.Status != "sent" {
				continue
			}
			out = append(out, inv.json(s.URL))
		}
		return ok("connect_invites", out), true
	case "conversations.revokeSharedInvite", "sharedInvites.revoke":
		x.mu.Lock()
		defer x.mu.Unlock()
		x.note(method, f)
		id := f.Get("invite_id")
		if method == "sharedInvites.revoke" {
			id = f.Get("id")
		}
		for i := range x.h.Invites {
			inv := &x.h.Invites[i]
			if inv.ID != id {
				continue
			}
			if inv.Direction != "outgoing" || inv.Status == "revoked" {
				return map[string]any{"ok": false, "error": "invalid_invite"}, true
			}
			inv.Status = "revoked"
			return ok(), true
		}
		return map[string]any{"ok": false, "error": "invite_not_found"}, true
	}
	return nil, false
}

func externalTeamJSON(t ExternalTeamFixture) map[string]any {
	return map[string]any{"id": t.ID, "name": t.Name, "domain": t.Domain, "is_verified": t.Verified, "date_created": 1700000000,
		"icon": map[string]any{"image_default": true, "image_44": "", "image_68": "", "image_88": ""}}
}

func (inv HubInvite) json(base string) map[string]any {
	m := map[string]any{"id": inv.ID, "sig": inv.Sig, "invite_type": inv.Kind, "direction": inv.Direction, "status": inv.Status,
		"date_created": inv.DateCreated, "date_last_updated": inv.DateCreated, "date_invalid": inv.DateInvalid,
		"inviting_user": map[string]any{"id": inv.InviterID, "name": inv.InviterName,
			"profile": map[string]any{"real_name": inv.InviterName, "display_name": inv.InviterName, "email": inv.InviterEmail}},
		"inviting_team": map[string]any{"id": inv.TeamID, "name": inv.TeamName, "is_verified": inv.TeamVerified, "date_created": 1600000000,
			"icon": map[string]any{"image_34": "", "image_44": "", "image_68": ""}},
		"invite_payload": map[string]any{"message": inv.Message}}
	if inv.RecipientEmail != "" {
		m["recipient_email"] = inv.RecipientEmail
	}
	if inv.Link != "" {
		m["link"] = inv.Link
	}
	if inv.ChannelName != "" {
		m["invite_payload"].(map[string]any)["channel"] = map[string]any{"id": inv.ChannelID, "name": inv.ChannelName}
	}
	acc := []map[string]any{}
	for _, a := range inv.Acceptances {
		acc = append(acc, map[string]any{"approval_status": a.Status, "date_accepted": a.DateAccepted,
			"accepting_user": map[string]any{"id": a.UserID, "name": a.UserName, "profile": map[string]any{"real_name": a.UserName}},
			"accepting_team": map[string]any{"id": a.TeamID, "name": a.TeamName}})
	}
	m["acceptances"] = acc
	return m
}

// SetSelfEnterpriseUser gives the viewer's user its enterprise_user (an
// org admin or owner for the hub's admin content). Set it before boot.
func (s *Server) SetSelfEnterpriseUser(eu slack.EnterpriseUser) {
	for i := range s.users {
		if s.users[i].ID == SelfID {
			x := eu
			s.users[i].EnterpriseUser = &x
		}
	}
}
