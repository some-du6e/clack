package core

import (
	"context"
	"encoding/json"
	"math/rand/v2"
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

// SharedPromptLink is agent_message_source_type for a shared prompt
// (uTwC NI.SharedPromptLink, which isn't in the chunks Clack read; its
// sibling AtMention goes out as "at_mention").
const SharedPromptLink = "shared_prompt_link"

// SendSharedPrompt is startAgentOrAssistantThread (G2io J) for an
// Agentforce agent's prompt: the agent's DM (the open IM, else
// conversations.open, as startImByMemberId does), and the prompt posted
// there as a verbatim mrkdwn section (or prompt_rich_text's blocks) with
// agent_message_source_type shared_prompt_link. It returns the DM and the
// message's ts, whose thread the caller opens.
func (a *App) SendSharedPrompt(ctx context.Context, agentUserID, prompt string, rich json.RawMessage) (ch, ts string, err error) {
	ch = a.openIMWith(agentUserID)
	if ch == "" {
		cv, err := a.OpenDM(ctx, agentUserID)
		if err != nil {
			return "", "", err
		}
		ch = cv.ID
	}
	blocks := rich
	if len(blocks) == 0 {
		blocks, err = json.Marshal([]map[string]any{{"type": "section", "block_id": newBlockID(),
			"text": map[string]any{"type": "mrkdwn", "text": prompt, "verbatim": true}}})
		if err != nil {
			return ch, "", err
		}
	} else if len(blocks) > 0 && blocks[0] == '{' {
		blocks = json.RawMessage("[" + string(blocks) + "]")
	}
	m, err := a.Send(ctx, SendOpts{Channel: ch, Text: prompt, Blocks: blocks, AgentMessageSourceType: SharedPromptLink})
	if err != nil {
		return ch, "", err
	}
	return ch, m.TS, nil
}

// newBlockID is a block's random id (Rb/W): five letters and digits.
func newBlockID() string {
	const set = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 5)
	for i := range b {
		b[i] = set[rand.IntN(len(set))]
	}
	return string(b)
}
