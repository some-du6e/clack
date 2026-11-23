package core

import (
	"context"
	"encoding/json"
	"net/url"
)

// A contact_card's writes (Slack's ContactCard zb3H and ShareContactModal,
// CDN build 9f450b8ad3db).

// The contacts.add errors ContactActionErrorModal words for the member
// (A0wp Zb), and the Saved contact modal's interaction (dY9S).
const (
	ContactCannotAddCard = "cannot_add_contact_card"
	ContactUserIsBlocked = "user_is_blocked"
	ContactInvalidCombo  = "invalid_user_combination"
	ContactUserNotFound  = "user_not_found"
	SaveContactModalComp = "SLACK_CONNECT_SAVE_CONTACT_MODAL" // G7.SlackConnectSaveContactModal
	InteractionDismiss   = "DISMISS"                          // X8.Dismiss
)

// AddContact is contacts.add (contactsAddFetcher): the viewer accepts
// contact card cardID, which lets them message its member. reason is the
// card's action ("contact_card_message", "contact_card_save_info").
func (a *App) AddContact(ctx context.Context, cardID, reason string) error {
	return a.C.Call(ctx, "contacts.add", url.Values{"contact_card_id": {cardID}, "reason": {reason}}, nil)
}

// ShareContact posts user's contact card to ch with note: Share contact's
// message, whose attachment is getFormattedContactCardAttachments' (LffH:
// hide_color, one contact_card block).
func (a *App) ShareContact(ctx context.Context, ch, user, note string) (Message, error) {
	att, err := json.Marshal([]map[string]any{{"hide_color": true,
		"blocks": []map[string]any{{"type": "contact_card", "contact_user_id": user}}}})
	if err != nil {
		return Message{}, err
	}
	return a.Send(ctx, SendOpts{Channel: ch, Text: note, Attachments: att})
}
