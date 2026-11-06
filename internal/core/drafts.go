package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// Drafts and scheduled messages.
//
// Slack keeps both as server-side drafts that sync across the user's
// clients (verified against the web client, 2026-11): drafts.create /
// drafts.update / drafts.delete, and a scheduled message is a draft with
// date_scheduled > 0. chat.scheduleMessage and chat.scheduledMessages.list
// answer not_allowed_token_type for session tokens.

type draftRaw struct {
	ID            string          `json:"id"`
	ClientMsgID   string          `json:"client_msg_id"`
	Blocks        json.RawMessage `json:"blocks"`
	Destinations  []draftDest     `json:"destinations"`
	DateCreated   int64           `json:"date_created"`
	DateScheduled int64           `json:"date_scheduled"`
	LastUpdatedTS string          `json:"last_updated_ts"`
	IsSent        bool            `json:"is_sent"`
	IsDeleted     bool            `json:"is_deleted"`
	FileIDs       []string        `json:"file_ids"`
}

type draftDest struct {
	ChannelID string   `json:"channel_id,omitempty"`
	UserIDs   []string `json:"user_ids,omitempty"`
	ThreadTS  string   `json:"thread_ts,omitempty"`
	Broadcast bool     `json:"broadcast,omitempty"`
}

func (a *App) draftOf(d *draftRaw) Draft {
	v := Draft{ID: d.ID, ClientMsgID: d.ClientMsgID, Blocks: d.Blocks, DateCreated: d.DateCreated, DateScheduled: d.DateScheduled,
		LastUpdated: d.LastUpdatedTS, Files: len(d.FileIDs), FileIDs: d.FileIDs}
	v.Doc = richtext.FromMessage(a.S, &slack.Message{Blocks: d.Blocks})
	v.Text = richtext.PlainText(v.Doc)
	if len(d.Destinations) > 0 {
		dst := d.Destinations[0]
		v.Channel, v.Users, v.ThreadTS, v.Broadcast = dst.ChannelID, dst.UserIDs, dst.ThreadTS, dst.Broadcast
	}
	return v
}

// Drafts lists unsent drafts, including scheduled ones (DateScheduled > 0).
func (a *App) Drafts(ctx context.Context) ([]Draft, error) {
	var res struct {
		Drafts []draftRaw `json:"drafts"`
	}
	if err := a.C.Call(ctx, "drafts.list", url.Values{"is_active": {"true"}, "limit": {"100"}}, &res); err != nil {
		return nil, err
	}
	var out []Draft
	ids := []string{}
	for i := range res.Drafts {
		d := &res.Drafts[i]
		if d.IsSent || d.IsDeleted {
			continue
		}
		v := a.draftOf(d)
		ids = append(ids, v.Users...)
		out = append(out, v)
	}
	a.EnsureUsers(ctx, ids)
	return out, nil
}

// textBlocks turns plain text into the rich_text block the composer sends.
func textBlocks(text string) json.RawMessage {
	b, _ := json.Marshal([]any{map[string]any{"type": "rich_text", "elements": []any{
		map[string]any{"type": "rich_text_section", "elements": []any{map[string]any{"type": "text", "text": text}}},
	}}})
	return b
}

func msNow() string { return strconv.FormatInt(time.Now().UnixMilli(), 10) }

// SaveDraft creates (ID == "") or updates a draft and returns it.
// Editing a scheduled draft requires its DateScheduled (Slack answers
// reattaching_not_allowed otherwise); the draft keeps its ID.
func (a *App) SaveDraft(ctx context.Context, o DraftOpts) (Draft, error) {
	return a.saveDraft(ctx, o, o.DateScheduled)
}

func (a *App) saveDraft(ctx context.Context, o DraftOpts, scheduleAt int64) (Draft, error) {
	if o.Channel == "" && len(o.Users) == 0 {
		return Draft{}, errors.New("core: draft needs a channel or users")
	}
	blocks := o.Blocks
	if len(blocks) == 0 {
		blocks = textBlocks(o.Text)
	}
	dest, _ := json.Marshal([]draftDest{{ChannelID: o.Channel, UserIDs: o.Users, ThreadTS: o.ThreadTS, Broadcast: o.Broadcast}})
	files, _ := json.Marshal(append([]string{}, o.FileIDs...))
	p := url.Values{"blocks": {string(blocks)}, "destinations": {string(dest)}, "file_ids": {string(files)},
		"attachments": {"[]"}, "is_from_composer": {"false"}}
	var res struct {
		Draft draftRaw `json:"draft"`
	}
	id := o.ID
	if id == "" {
		p.Set("client_msg_id", clientMsgID())
		if err := a.C.Call(ctx, "drafts.create", p, &res); err != nil {
			return Draft{}, err
		}
		id = res.Draft.ID
		if scheduleAt == 0 {
			return a.draftOf(&res.Draft), nil
		}
	}
	p.Del("client_msg_id")
	p.Set("draft_id", id)
	p.Set("client_last_updated_ts", msNow())
	if scheduleAt > 0 {
		p.Set("date_scheduled", strconv.FormatInt(scheduleAt, 10))
		p.Set("is_from_composer", "true")
	}
	if err := a.C.Call(ctx, "drafts.update", p, &res); err != nil {
		return Draft{}, err
	}
	if res.Draft.ID == "" {
		res.Draft.ID = id
	}
	return a.draftOf(&res.Draft), nil
}

// DeleteDraft deletes a draft (or cancels a scheduled message).
func (a *App) DeleteDraft(ctx context.Context, id string) error {
	return a.C.Call(ctx, "drafts.delete", url.Values{"draft_id": {id}, "client_last_updated_ts": {msNow()}, "skip_file_deletion": {"true"}}, nil)
}

// SendDraft posts a draft now (optimistically, like Send) and deletes it.
func (a *App) SendDraft(ctx context.Context, id string) (Message, error) {
	ds, err := a.Drafts(ctx)
	if err != nil {
		return Message{}, err
	}
	for _, d := range ds {
		if d.ID != id {
			continue
		}
		ch := d.Channel
		if ch == "" && len(d.Users) > 0 {
			c, err := a.OpenDM(ctx, d.Users...)
			if err != nil {
				return Message{}, err
			}
			ch = c.ID
		}
		m, err := a.Send(ctx, SendOpts{Channel: ch, Text: d.Text, ThreadTS: d.ThreadTS, Broadcast: d.Broadcast, Blocks: d.Blocks})
		if err != nil {
			return Message{}, err
		}
		_ = a.DeleteDraft(ctx, id)
		return m, nil
	}
	return Message{}, errors.New("core: draft not found")
}

// ScheduleMessage schedules a message for postAt (unix seconds). Without
// Blocks the text is sent as one plain rich_text section.
func (a *App) ScheduleMessage(ctx context.Context, o SendOpts, postAt int64) (ScheduledMessage, error) {
	if postAt <= time.Now().Unix() {
		return ScheduledMessage{}, errors.New("core: postAt must be in the future")
	}
	d, err := a.saveDraft(ctx, DraftOpts{Channel: o.Channel, ThreadTS: o.ThreadTS, Broadcast: o.Broadcast, Text: o.Text, Blocks: o.Blocks}, postAt)
	if err != nil {
		return ScheduledMessage{}, err
	}
	return scheduledOf(d), nil
}

// RescheduleMessage moves a scheduled message to postAt.
func (a *App) RescheduleMessage(ctx context.Context, id string, postAt int64) (ScheduledMessage, error) {
	ds, err := a.Drafts(ctx)
	if err != nil {
		return ScheduledMessage{}, err
	}
	for _, d := range ds {
		if d.ID == id {
			nd, err := a.saveDraft(ctx, DraftOpts{ID: id, Channel: d.Channel, Users: d.Users, ThreadTS: d.ThreadTS, Broadcast: d.Broadcast, Blocks: d.Blocks, FileIDs: d.FileIDs}, postAt)
			return scheduledOf(nd), err
		}
	}
	return ScheduledMessage{}, errors.New("core: scheduled message not found")
}

func scheduledOf(d Draft) ScheduledMessage {
	return ScheduledMessage{ID: d.ID, Channel: d.Channel, ThreadTS: d.ThreadTS, PostAt: d.DateScheduled, DateCreated: d.DateCreated,
		Text: d.Text, Doc: d.Doc, Blocks: d.Blocks}
}

// ScheduledMessages lists scheduled messages (ch "" = all), soonest first.
func (a *App) ScheduledMessages(ctx context.Context, ch string) ([]ScheduledMessage, error) {
	ds, err := a.Drafts(ctx)
	if err != nil {
		return nil, err
	}
	var out []ScheduledMessage
	for _, d := range ds {
		if d.DateScheduled > 0 && (ch == "" || d.Channel == ch) {
			out = append(out, scheduledOf(d))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PostAt < out[j].PostAt })
	return out, nil
}

// DeleteScheduled cancels a scheduled message.
func (a *App) DeleteScheduled(ctx context.Context, ch, id string) error {
	return a.DeleteDraft(ctx, id)
}

// onDraftEvent turns draft_create/update/delete/send into Event{Type:"draft"}.
func (a *App) onDraftEvent(typ string, raw json.RawMessage) {
	var e struct {
		DraftID string    `json:"draft_id"`
		Draft   *draftRaw `json:"draft"`
	}
	_ = json.Unmarshal(raw, &e)
	ev := Event{Type: "draft", Name: strings.TrimPrefix(typ, "draft_"), ID: e.DraftID, Raw: raw}
	if e.Draft != nil && e.Draft.ID != "" {
		d := a.draftOf(e.Draft)
		ev.Draft = &d
		ev.Channel, ev.ThreadTS = d.Channel, d.ThreadTS
		if ev.ID == "" {
			ev.ID = d.ID
		}
	}
	a.emit(ev)
}
