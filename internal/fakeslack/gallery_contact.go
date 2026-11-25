package fakeslack

// The contact_card block (Slack's ContactCard): a member's card with its
// hover actions. Synthetic: the fixed variant users.

func init() { RegisterGallery("18-contact", galleryContact) }

func galleryContact(e GalleryEnv) []GalleryMessage {
	card := func(user, id string) string {
		s := `{"type":"contact_card","block_id":"cc-` + user + `","contact_user_id":"` + user + `"`
		if id != "" {
			s += `,"contact_card_id":"` + id + `"`
		}
		return s + `}`
	}
	return []GalleryMessage{
		{Name: "contact/member", Text: "Robin's contact card", Blocks: `[` + card(ProfileID, "CC0FAKEPROF") + `]`},
		{Name: "contact/external", Text: "Sam's contact card (Partner Co, not loaded: its team draws the loading bar)", Blocks: `[{"type":"section","text":{"type":"mrkdwn","text":"An external member's card:"}},` + card(ExternalID, "CC0FAKEXTRN") + `]`},
		{Name: "contact/deactivated", Text: "A deactivated member's card", Blocks: `[` + card(DeactivatedID, "CC0FAKEGONE") + `]`},
		{Name: "contact/no card id", Text: "A card the server hasn't made yet (no actions)", Blocks: `[` + card(AdminID, "") + `]`},
		{Name: "contact/unknown member", Text: "A card for a member nobody has", Blocks: `[{"type":"section","text":{"type":"mrkdwn","text":"A card for a member who doesn't exist:"}},` + card("U0NOSUCH01", "CC0NOSUCH1") + `]`},
	}
}
