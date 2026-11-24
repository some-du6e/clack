package richtext

import (
	"strings"
	"testing"
)

// Synthetic fixtures only.

// contactRes is the test resolver with members for contact cards.
type contactRes struct {
	fakeRes
	members map[string]ContactMember
}

func (r contactRes) ContactMember(id string) (ContactMember, bool) {
	m, ok := r.members[id]
	return m, ok
}

// A contact_card's member and card ids in either spelling, the member as
// the resolver has them; Slack's error block while the member is unknown
// or the id isn't a string, and with no resolver; one in a container's
// children too.
func TestContactCardDecode(t *testing.T) {
	r := contactRes{members: map[string]ContactMember{"U1": {Name: "Robin", Team: "Fake Club", CanInteract: true, CanShare: true}}}
	build := func(blocks string) Doc {
		return FromMessage(r, msg(t, `{"ts":"1.0","text":"fallback text","blocks":`+blocks+`}`))
	}
	for _, b := range []string{
		`{"type":"contact_card","block_id":"cc","contact_user_id":"U1","contact_card_id":"CC1"}`,
		`{"type":"contact_card","block_id":"cc","contactUserId":"U1","contactCardId":"CC1"}`,
	} {
		d := build(`[` + b + `]`)
		if len(d.Blocks) != 1 || d.Blocks[0].Kind != ContactCardBlock || d.Blocks[0].Contact == nil {
			t.Fatalf("%s: %s", b, dump(d.Blocks))
		}
		c := d.Blocks[0].Contact
		if c.UserID != "U1" || c.CardID != "CC1" || c.Member.Name != "Robin" || c.Member.Team != "Fake Club" || d.Blocks[0].BlockID != "cc" {
			t.Errorf("%s: card %+v", b, c)
		}
	}
	for _, c := range []struct{ name, block string }{
		{"unknown member", `{"type":"contact_card","contact_user_id":"U9","contact_card_id":"CC9"}`},
		{"a number id", `{"type":"contact_card","contact_user_id":7}`},
		{"no id", `{"type":"contact_card"}`},
	} {
		d := build(`[` + c.block + `,{"type":"section","text":{"type":"mrkdwn","text":"after"}}]`)
		if len(d.Blocks) != 2 || d.Blocks[0].Kind != Unsupported || PlainText(Doc{Blocks: d.Blocks[1:]}) != "after" {
			t.Errorf("%s: %s", c.name, dump(d.Blocks))
		}
	}
	// no ContactResolver: the error block
	if d := blocksMsg(t, `[{"type":"contact_card","contact_user_id":"U1","contact_card_id":"CC1"}]`); len(d.Blocks) != 1 || d.Blocks[0].Kind != Unsupported {
		t.Errorf("no resolver: %s", dump(d.Blocks))
	}
	d := build(`[{"type":"container","child_blocks":[{"type":"contact_card","contact_user_id":"U1","contact_card_id":"CC1"}]}]`)
	if len(d.Blocks) != 1 || len(d.Blocks[0].Children) != 1 || d.Blocks[0].Children[0].Kind != ContactCardBlock {
		t.Errorf("in a container: %s", dump(d.Blocks))
	}
	// notifications: nothing of its own (convertContactCardBlockToPlainText
	// speaks only in message summaries)
	if got := notificationTextOf(t, `{"ts":"1.0","text":"fallback","blocks":[{"type":"contact_card","contact_user_id":"U1","contact_card_id":"CC1"}]}`); strings.Contains(got, "Robin") {
		t.Errorf("notification %q", got)
	}
}

// ContactCard's actions and click, by its member.
func TestContactActions(t *testing.T) {
	for _, c := range []struct {
		name    string
		card    ContactCard
		acts    string
		profile bool
	}{
		{"a member the viewer can talk to", ContactCard{UserID: "U1", CardID: "C", Member: ContactMember{CanInteract: true, CanShare: true}}, "message share_contact", true},
		{"a stranger", ContactCard{UserID: "U1", CardID: "C", Member: ContactMember{CanShare: true}}, "message share_contact save_info", false},
		{"a stranger nobody may share", ContactCard{UserID: "U1", CardID: "C"}, "message save_info", false},
		{"profile only", ContactCard{UserID: "U1", CardID: "C", Member: ContactMember{ProfileOnly: true}}, "", true},
		{"deleted stranger", ContactCard{UserID: "U1", CardID: "C", Member: ContactMember{Deleted: true}}, "message", false},
		{"no card id", ContactCard{UserID: "U1", Member: ContactMember{CanInteract: true, CanShare: true}}, "", true},
	} {
		if got := strings.Join(ContactActions(&c.card), " "); got != c.acts {
			t.Errorf("%s: actions %q, want %q", c.name, got, c.acts)
		}
		if got := ContactOpensProfile(&c.card); got != c.profile {
			t.Errorf("%s: opens the profile %v, want %v", c.name, got, c.profile)
		}
	}
}
