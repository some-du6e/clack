package slack

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"time"
)

// AuthTest returns the identity of the token.
type AuthInfo struct {
	URL          string `json:"url"`
	Team         string `json:"team"`
	User         string `json:"user"`
	TeamID       string `json:"team_id"`
	UserID       string `json:"user_id"`
	EnterpriseID string `json:"enterprise_id"`
}

func (c *Client) AuthTest(ctx context.Context) (*AuthInfo, error) {
	var a AuthInfo
	return &a, c.Call(ctx, "auth.test", nil, &a)
}

// Conversations lists every conversation the user is a member of.
// Archived channels are dropped, but DMs are kept even when Slack flags them
// archived (exclude_archived=true hides some app DMs the real client shows,
// verified 2026-11).
func (c *Client) Conversations(ctx context.Context) ([]Conversation, error) {
	var all []Conversation
	cursor := ""
	for {
		p := url.Values{
			"types":            {"public_channel,private_channel,mpim,im"},
			"exclude_archived": {"false"},
			"limit":            {"1000"},
		}
		if cursor != "" {
			p.Set("cursor", cursor)
		}
		var r struct {
			Channels []Conversation `json:"channels"`
			Meta     struct {
				NextCursor string `json:"next_cursor"`
			} `json:"response_metadata"`
		}
		if err := c.Call(ctx, "users.conversations", p, &r); err != nil {
			return all, err
		}
		for _, ch := range r.Channels {
			if ch.IsArchived && !ch.IsIM && !ch.IsMPIM {
				continue
			}
			all = append(all, ch)
		}
		if r.Meta.NextCursor == "" {
			return all, nil
		}
		cursor = r.Meta.NextCursor
	}
}

func (c *Client) ConversationInfo(ctx context.Context, id string) (*Conversation, error) {
	var r struct {
		Channel Conversation `json:"channel"`
	}
	err := c.Call(ctx, "conversations.info", url.Values{"channel": {id}, "include_num_members": {"true"}}, &r)
	return &r.Channel, err
}

func (c *Client) Counts(ctx context.Context) (*Counts, error) {
	var r Counts
	err := c.Call(ctx, "client.counts", url.Values{
		"thread_counts_by_channel": {"true"}, "org_wide_aware": {"true"},
		"include_file_channels": {"true"}, "include_all_unreads": {"true"},
	}, &r)
	return &r, err
}

func (c *Client) Sections(ctx context.Context) ([]Section, error) {
	var r struct {
		Sections []Section `json:"channel_sections"`
	}
	err := c.Call(ctx, "users.channelSections.list", nil, &r)
	return r.Sections, err
}

// HistoryOpts controls a conversations.history page.
type HistoryOpts struct {
	Latest, Oldest string
	Limit          int
	Inclusive      bool
}

type HistoryPage struct {
	Messages []Message `json:"messages"`
	HasMore  bool      `json:"has_more"`
	PinCount int       `json:"pin_count"`
}

func (c *Client) History(ctx context.Context, ch string, o HistoryOpts) (*HistoryPage, error) {
	p := url.Values{"channel": {ch}, "include_pin_count": {"true"}}
	if o.Limit == 0 {
		o.Limit = 50
	}
	p.Set("limit", strconv.Itoa(o.Limit))
	if o.Latest != "" {
		p.Set("latest", o.Latest)
	}
	if o.Oldest != "" {
		p.Set("oldest", o.Oldest)
	}
	if o.Inclusive {
		p.Set("inclusive", "true")
	}
	var r HistoryPage
	err := c.Call(ctx, "conversations.history", p, &r)
	return &r, err
}

func (c *Client) Replies(ctx context.Context, ch, ts, cursor string) ([]Message, string, error) {
	p := url.Values{"channel": {ch}, "ts": {ts}, "limit": {"200"}}
	if cursor != "" {
		p.Set("cursor", cursor)
	}
	var r struct {
		Messages []Message `json:"messages"`
		Meta     struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	err := c.Call(ctx, "conversations.replies", p, &r)
	return r.Messages, r.Meta.NextCursor, err
}

// PostOpts are options for chat.postMessage.
type PostOpts struct {
	Channel, Text, ThreadTS string
	Broadcast               bool
	Blocks                  json.RawMessage
	ClientMsgID             string
}

func (c *Client) Post(ctx context.Context, o PostOpts) (*Message, error) {
	p := url.Values{"channel": {o.Channel}, "text": {o.Text}, "link_names": {"true"}, "unfurl_links": {"true"}}
	if o.ThreadTS != "" {
		p.Set("thread_ts", o.ThreadTS)
		if o.Broadcast {
			p.Set("reply_broadcast", "true")
		}
	}
	if len(o.Blocks) > 0 {
		p.Set("blocks", string(o.Blocks))
	}
	if o.ClientMsgID != "" {
		p.Set("client_msg_id", o.ClientMsgID)
	}
	var r struct {
		Message Message `json:"message"`
		TS      string  `json:"ts"`
	}
	err := c.Call(ctx, "chat.postMessage", p, &r)
	if r.Message.TS == "" {
		r.Message.TS = r.TS
	}
	return &r.Message, err
}

func (c *Client) Update(ctx context.Context, ch, ts, text string, blocks json.RawMessage) error {
	p := url.Values{"channel": {ch}, "ts": {ts}, "text": {text}, "link_names": {"true"}}
	if len(blocks) > 0 {
		p.Set("blocks", string(blocks))
	}
	return c.Call(ctx, "chat.update", p, nil)
}

func (c *Client) Delete(ctx context.Context, ch, ts string) error {
	return c.Call(ctx, "chat.delete", url.Values{"channel": {ch}, "ts": {ts}}, nil)
}

func (c *Client) React(ctx context.Context, ch, ts, name string, add bool) error {
	m := "reactions.remove"
	if add {
		m = "reactions.add"
	}
	return c.Call(ctx, m, url.Values{"channel": {ch}, "timestamp": {ts}, "name": {name}}, nil)
}

func (c *Client) Mark(ctx context.Context, ch, ts string) error {
	return c.Call(ctx, "conversations.mark", url.Values{"channel": {ch}, "ts": {ts}}, nil)
}

// UsersInfo resolves users through the edge cache (one round trip for many ids).
func (c *Client) UsersInfo(ctx context.Context, ids []string) ([]User, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	up := map[string]int{}
	for _, id := range ids {
		up[id] = 0
	}
	var r struct {
		Results []User `json:"results"`
	}
	err := c.Edge(ctx, "users/info", map[string]any{
		"check_interaction": true, "include_profile_only_users": true, "updated_ids": up,
	}, &r)
	return r.Results, err
}

// EmojiList returns custom emoji: name -> url or "alias:other".
func (c *Client) EmojiList(ctx context.Context) (map[string]string, error) {
	var r struct {
		Emoji map[string]string `json:"emoji"`
	}
	err := c.Call(ctx, "emoji.list", nil, &r)
	return r.Emoji, err
}

// WebSocketURL builds the realtime gateway URL the web client uses
// (verified against app.slack.com: org token, gateway_server from
// client.getWebSocketURL's routing_context, and a real agent_version; the
// gateway closes the socket without a hello when agent_version is bogus).
func (c *Client) WebSocketURL(ctx context.Context, fallback bool) string {
	base := "wss://wss-primary.slack.com/"
	if fallback {
		base = "wss://wss-backup.slack.com/"
	}
	var r struct {
		Primary  string `json:"primary_websocket_url"`
		Fallback string `json:"fallback_websocket_url"`
		Routing  string `json:"routing_context"`
	}
	err := c.Call(ctx, "client.getWebSocketURL", nil, &r)
	if err == nil && r.Primary != "" && !fallback {
		base = r.Primary
	} else if err == nil && r.Fallback != "" && fallback {
		base = r.Fallback
	}
	tok := c.Creds.Token
	gw := c.Creds.Team
	if c.Creds.EnterpriseToken != "" && c.Creds.EnterpriseID != "" {
		tok = c.Creds.EnterpriseToken
		gw = c.Creds.EnterpriseID
	}
	if len(gw) > 1 {
		gw = "T" + gw[1:] + "-1"
	}
	if r.Routing != "" {
		gw = r.Routing
	}
	q := url.Values{
		"token":        {tok},
		"sync_desync":  {"1"},
		"slack_client": {"desktop"},
		"start_args": {"?agent=client&org_wide_aware=true&agent_version=" + strconv.FormatInt(time.Now().Unix(), 10) +
			"&eac_cache_ts=true&cache_ts=0&name_tagging=true&only_self_subteams=true&connect_only=true&ms_latest=true"},
		"no_query_on_subscribe": {"1"},
		"flannel":               {"3"},
		"lazy_channels":         {"1"},
		"gateway_server":        {gw},
		"batch_presence_aware":  {"1"},
	}
	if c.Creds.EnterpriseID != "" {
		q.Set("enterprise_id", c.Creds.EnterpriseID)
	}
	return base + "?" + q.Encode()
}
