package store

import (
	"slices"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// What a contact_card block reads about its member (richtext.ContactMember;
// Slack's ContactCard zb3H, CDN build 9f450b8ad3db).

// ContactMember is richtext.ContactResolver: the member if the store
// holds them (a miss is asked for).
func (s *Store) ContactMember(id string) (richtext.ContactMember, bool) {
	return s.contactMember(s.User, id)
}

// ContactMember is richtext.ContactResolver without counting a hit.
func (p Peek) ContactMember(id string) (richtext.ContactMember, bool) {
	return p.contactMember(p.peekUser, id)
}

var (
	_ richtext.ContactResolver = (*Store)(nil)
	_ richtext.ContactResolver = Peek{}
)

func (s *Store) contactMember(look userLookup, id string) (richtext.ContactMember, bool) {
	u, ok := look(id)
	if !ok || u == nil {
		return richtext.ContactMember{}, false
	}
	name, _ := s.memberName(look, id)
	m := richtext.ContactMember{Name: name, Avatar: s.memberAvatar(look, id),
		CanInteract: !u.IsStranger, ProfileOnly: u.IsProfileOnlyUser, Deleted: u.Deleted}
	s.mu.RLock()
	self := s.Self
	cur, teamName := s.TeamID, s.TeamName
	s.mu.RUnlock()
	org := ""
	if eu := self.EnterpriseUser; eu != nil {
		org = eu.EnterpriseID
	}
	external := memberExternal(u, cur, org)
	// SimpleContactCard's team: the viewer's own (getCurrentTeamId: the
	// org on Grid) unless the member is external, then theirs
	// (getTeamIdForMember), which the store doesn't name: BaseMissingTeam
	switch {
	case !external && org != "" && self.EnterpriseUser.EnterpriseName != "":
		m.Team = self.EnterpriseUser.EnterpriseName
	case !external && teamName != "":
		m.Team = teamName
	default:
		m.TeamUnknown = true
	}
	m.CanShare = canShareContact(&self, u, external)
	return m, true
}

// memberExternal is isExternal as core's locality decides it: a member
// neither on the viewer's team cur nor in its org.
func memberExternal(u *slack.User, cur, org string) bool {
	if cur == "" {
		return false
	}
	eu := u.EnterpriseUser
	var team string
	switch {
	case org != "" && eu != nil && eu.EnterpriseID != "":
		team = eu.EnterpriseID
	case u.TeamID != "":
		team = u.TeamID
	case eu != nil && u.ID == eu.ID && eu.EnterpriseID != "":
		team = eu.EnterpriseID
	default:
		team = cur
	}
	member := team == cur || eu != nil && slices.Contains(eu.Teams, cur)
	inOrg := org != "" && (team == org || eu != nil && eu.EnterpriseID == org)
	return !member && !inOrg
}

// canShareContact is canUserShareContactById (N6PQ VX): not the viewer,
// an active member (DIay isActiveMember), a viewer who may share contacts
// (46d6 canUserShareContacts: not a guest; Slack also refuses a free
// team, whose plan core knows, not the store) and the member's
// who_can_share_contact_card: never NO_ONE, and EVERYONE for an external
// member.
func canShareContact(self, u *slack.User, external bool) bool {
	if u.ID == self.ID || !activeMember(u) || self.IsRestricted {
		return false
	}
	pref := u.WhoCanShareContactCard
	if pref == "NO_ONE" {
		return false
	}
	return !external || pref == "EVERYONE"
}

// activeMember is isActiveMember: no bot (is_bot, Slackbot, USLACK), not
// the security bot, no guest, not deleted, invited or forgotten.
func activeMember(u *slack.User) bool {
	if u == nil {
		return false
	}
	slackbot := u.IsSlackbot || u.ID == "USLACKBOT"
	return !(u.IsBot || slackbot || u.ID == "USLACK") && !(u.IsSlackSecurityBot || u.ID == "USLACKSECURITY") &&
		!u.IsRestricted && !u.IsUltraRestricted && !u.Deleted && !u.IsInvitedUser && !u.IsForgotten
}

// PromptAgent is richtext.AgentResolver: a share_prompt's agent if the
// store holds it (a miss is asked for).
func (s *Store) PromptAgent(id string) (name, avatar string, agentforce, ok bool) {
	return s.promptAgent(s.User, id)
}

// PromptAgent is richtext.AgentResolver without counting a hit.
func (p Peek) PromptAgent(id string) (name, avatar string, agentforce, ok bool) {
	return p.promptAgent(p.peekUser, id)
}

var (
	_ richtext.AgentResolver = (*Store)(nil)
	_ richtext.AgentResolver = Peek{}
)

func (s *Store) promptAgent(look userLookup, id string) (name, avatar string, agentforce, ok bool) {
	u, known := look(id)
	if !known || u == nil {
		return "", "", false, false
	}
	// getMemberRealName: real_name, else the profile's
	name = u.RealName
	if name == "" {
		name = u.Profile.RealName
	}
	return name, s.memberAvatar(look, id), u.IsAgentforceBot, true
}
