package richtext

// A contact_card block: Slack's ContactCard (boot-shared zb3H, its body
// SimpleContactCard alkx; CDN build 9f450b8ad3db). The block names a
// member (contact_user_id) and, once the server made it, the card
// (contact_card_id) its actions accept; what the card shows and which
// actions it offers come from the member, which the resolver supplies
// (ContactResolver).

import "encoding/json"

// ContactCard is a contact_card block.
type ContactCard struct {
	UserID, CardID string
	Member         ContactMember
}

// ContactMember is what ContactCard reads about its member.
type ContactMember struct {
	// Name is the member's name as messages print it (MemberName), Avatar
	// their 36px picture.
	Name, Avatar string
	// Team is TeamName's name for the card's team: the viewer's own
	// (getCurrentTeamId) for a member who isn't external, else the
	// member's (getTeamIdForMember); TeamUnknown is a team the client
	// hasn't loaded, which BaseMissingTeam draws as its "Loading…" bar.
	Team        string
	TeamUnknown bool
	// CanInteract is canInteract (th9D zP): no member object says the
	// viewer can't (is_stranger, xKHe's setCanInteract from members).
	CanInteract bool
	// ProfileOnly is isProfileOnlyUser; Deleted is isDeleted.
	ProfileOnly, Deleted bool
	// CanShare is canUserShareContactById (N6PQ VX): the viewer may share
	// this member's contact.
	CanShare bool
}

// ContactResolver is an optional Resolver extension: a contact card's
// member. ok is false while the member isn't known (the resolver asks for
// them and the Doc is rebuilt when they arrive) and for one that doesn't
// exist (isNonExistentMember): ContactCard then draws Slack's "This
// content could not be displayed." (KfF5).
type ContactResolver interface {
	ContactMember(id string) (m ContactMember, ok bool)
}

// bkContactCard is a contact_card block's keys, raw (toCamelCaseProps:
// either spelling).
type bkContactCard struct {
	UserID    json.RawMessage `json:"contact_user_id"`
	UserCamel json.RawMessage `json:"contactUserId"`
	CardID    json.RawMessage `json:"contact_card_id"`
	CardCamel json.RawMessage `json:"contactCardId"`
	BlockID   json.RawMessage `json:"block_id"`
}

// contactCardBlock builds a contact_card block: the card, or Slack's
// error block while its member is unknown or doesn't exist.
func (c *builder) contactCardBlock(b *bk) Block {
	v := b.contact
	if v == nil {
		v = &bkContactCard{}
	}
	cc := &ContactCard{UserID: rawString(firstRaw(v.UserID, v.UserCamel)), CardID: rawString(firstRaw(v.CardID, v.CardCamel))}
	cr, ok := c.r.(ContactResolver)
	if !ok || cc.UserID == "" {
		return unsupported()
	}
	m, known := cr.ContactMember(cc.UserID)
	if !known {
		return unsupported()
	}
	cc.Member = m
	return Block{Kind: ContactCardBlock, BlockID: b.BlockID, Contact: cc}
}

// ContactActions are ContactCard's actions for card c, in order: Message
// unless the member is profile only, Share contact… when the viewer may
// share it, Save contact unless they can already interact (or the member
// is deleted or profile only); none for a card without its id.
func ContactActions(c *ContactCard) []string {
	if c == nil || c.CardID == "" {
		return nil
	}
	var out []string
	m := &c.Member
	if !m.ProfileOnly {
		out = append(out, "message")
	}
	if m.CanShare {
		out = append(out, "share_contact")
	}
	if !m.CanInteract && !m.Deleted && !m.ProfileOnly {
		out = append(out, "save_info")
	}
	return out
}

// ContactOpensProfile reports whether a click on the card opens the
// member's profile (the viewer can interact with them, or they're
// profile only).
func ContactOpensProfile(c *ContactCard) bool {
	return c != nil && (c.Member.CanInteract || c.Member.ProfileOnly)
}
