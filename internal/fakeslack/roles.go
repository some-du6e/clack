package fakeslack

import (
	"encoding/json"

	"github.com/some-du6e/clack/internal/slack"
)

// Members of every role Slack's cards distinguish, served by users/info
// (variants). Their names carry no role words, so a test reading what a
// card draws finds a role only where a label put it.
const (
	OwnerID           = "U0FAKEOWNR" // is_owner (and is_admin, as Slack sends an owner)
	PrimaryOwnerID    = "U0FAKEPOWN" // is_primary_owner, is_owner, is_admin
	OrgAdminID        = "U0FAKEOADM" // enterprise_user.is_admin only
	OrgOwnerID        = "U0FAKEOOWN" // enterprise_user.is_owner and is_admin only
	OrgPrimaryOwnerID = "U0FAKEOPOW" // enterprise_user.is_primary_owner, is_owner, is_admin
	GuestID           = "U0FAKEGUES" // is_restricted: a Multi-Channel Guest
	SingleGuestID     = "U0FAKESCGU" // is_restricted + is_ultra_restricted: a Single-Channel Guest
	AppPersonID       = "U0FAKEAPPP" // is_app_user: a person who authorized an app (not a bot)
	MemberID          = "U0FAKEMEMB" // no role at all

	// RoleCardID is Karim's 2026-11-21 card (U09Q8MLTE58) in synthetic
	// form: a workspace owner and admin who is also an org owner and
	// admin, display name "ezs" and a different real name, a title that
	// wraps, in Do Not Disturb (SetMemberDND), with status_emoji
	// :speech_balloon: whose status_emoji_display_info lists the custom
	// :ping-9+: in the text first.
	RoleCardID       = "U0FAKEROLE"
	RoleCardRealName = "Ezra Sample"
	RoleCardTitle    = "Electro-Philosophical System © 1972 Digital Equipment Corporation"
	RoleCardStatus   = ":ping-9+: Absolutely DM me if you need me, but only after lunch"
)

// RoleCardPingURL is the custom :ping-9+:'s picture on s.
func (s *Server) RoleCardPingURL() string { return s.URL + "img/64x64/ping9.png" }

// roleUsers are the role fixtures, with prof the variants' profile maker.
func (s *Server) roleUsers(prof func(display, real, title, n string) slack.Profile) []slack.User {
	org := func(admin, owner, primary bool) *slack.EnterpriseUser {
		return &slack.EnterpriseUser{EnterpriseID: EnterpriseID, EnterpriseName: "Fake Org", Teams: []string{TeamID},
			IsAdmin: admin, IsOwner: owner, IsPrimaryOwner: primary}
	}
	u := func(id, handle, real string) slack.User {
		v := slack.User{ID: id, TeamID: TeamID, Name: handle, RealName: real, TZ: "Europe/London", TZOffset: 0,
			Profile: prof(handle, real, "", handle)}
		v.EnterpriseUser = org(false, false, false)
		v.EnterpriseUser.ID = id
		return v
	}
	owner := u(OwnerID, "olive.k", "Olive Kestrel")
	owner.IsOwner, owner.IsAdmin = true, true
	primary := u(PrimaryOwnerID, "petra.l", "Petra Lindqvist")
	primary.IsPrimaryOwner, primary.IsOwner, primary.IsAdmin = true, true, true
	oadm := u(OrgAdminID, "omar.d", "Omar Dunmore")
	oadm.EnterpriseUser = org(true, false, false)
	oown := u(OrgOwnerID, "zara.r", "Zara Lennox")
	oown.EnterpriseUser = org(true, true, false)
	opow := u(OrgPrimaryOwnerID, "pip.o", "Pip Okafor")
	opow.EnterpriseUser = org(true, true, true)
	guest := u(GuestID, "gwen.t", "Gwen Tarrant")
	guest.IsRestricted = true
	single := u(SingleGuestID, "sol.v", "Sol Varga")
	single.IsRestricted, single.IsUltraRestricted = true, true
	appP := u(AppPersonID, "april.n", "April Novak")
	appP.IsAppUser = true
	plain := u(MemberID, "milo.f", "Milo Fairweather")

	card := u(RoleCardID, "ezs", RoleCardRealName)
	card.IsOwner, card.IsAdmin = true, true
	card.EnterpriseUser = org(true, true, false)
	card.EnterpriseUser.ID = RoleCardID
	card.Presence = "away"
	card.TZ, card.TZOffset = "America/New_York", -18000
	card.Profile.Title = RoleCardTitle
	card.Profile.StatusEmoji = ":speech_balloon:"
	card.Profile.StatusText = RoleCardStatus
	di, _ := json.Marshal([]map[string]any{
		{"emoji_name": "ping-9+", "display_url": s.RoleCardPingURL()},
		{"emoji_name": "speech_balloon", "unicode": "1f4ac"},
	})
	card.Profile.StatusEmojiDisplay = di
	for _, p := range []*slack.User{&oadm, &oown, &opow} {
		p.EnterpriseUser.ID = p.ID
	}
	return []slack.User{owner, primary, oadm, oown, opow, guest, single, appP, plain, card}
}
