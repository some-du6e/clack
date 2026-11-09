package core

// Code channels (Slack Code): a channel an AI agent opens for a longer
// task. Ported from Slack's web client (build a84662d2d135, 2026-11-08);
// docs/CODE-CHANNELS.md has the evidence for every rule here.
//
// The data rides on conversations.info's channel.properties:
//
//	record_channel {record_id "AC:…", record_type "agent_channel", record_label, record_label_plural}
//	agent_session  {status, title, agent_bot_user_ids, encoded_agent_bot_user_ids, date_status_processing_expire, origin_link}
//	code_channel   {repo, branch, pr_status, ci_state, pr_title, context_bar_items, agent_session_views, agent_views, diff_file_id, …}
//
// and changes arrive as channel_updated {updates: {properties: …}}.

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// Agent statuses (Slack's jZ43 enum).
const (
	AgentActive     = "active"
	AgentProcessing = "processing"
	AgentSuspended  = "suspended"
	AgentClosed     = "closed"
	AgentStale      = "stale"
)

// codeChannelStaleAfter is getAgentChannelStatus's isChannelPastStaleThreshold:
// a code channel whose latest message is older than 3 days is Stale.
const codeChannelStaleAfter = 259200 * time.Second

// CodeChannel is a code channel's properties, as Slack's client reads them.
type CodeChannel struct {
	RecordID, RecordType      string
	RecordLabel, RecordPlural string // "Code Channel", "Code Channels"

	// agent_session
	SessionStatus     string   // the raw status ("" when absent)
	Title             string   // agent_session.title
	AgentBotIDs       []string // encoded_agent_bot_user_ids, else agent_bot_user_ids
	ProcessingExpires int64    // date_status_processing_expire (unix s), 0 = none

	// code_channel
	Empty          bool // code_channel has no keys (the "No branch info" bar)
	Repo, Branch   string
	PRStatus       string // open | draft | blocked | closed | merged
	CIState        string
	PRTitle        string
	ContextBar     []ContextBarItem
	Views          []CodeChannelView
	SummaryMessage string // summary_message.message_ts
}

// ContextBarItem is one of code_channel.context_bar_items: a link
// (item_type info, the default) or an action the agent handles
// (item_type "action", sent with client.codeChannels.action).
type ContextBarItem struct {
	Key       string `json:"key"`
	ItemType  string `json:"item_type"`
	Label     string `json:"label"`
	Icon      string `json:"icon"`
	URL       string `json:"url"`
	BotUserID string `json:"bot_user_id"`
}

// IsAction reports whether the item is an action button.
func (i ContextBarItem) IsAction() bool { return i.ItemType == "action" }

// Code channel view types (Slack's SS3I enum).
const (
	ViewDiff        = "diff"
	ViewHTML        = "html"
	ViewCanvas      = "canvas"
	ViewBlockKit    = "block_kit"
	ViewPullRequest = "pull_request"
)

// CodeChannelView is an artifact the agent published: one of
// code_channel.agent_session_views, plus what getCodeChannelViews (PntH)
// adds from agent_views (html), diff_file_id (the Code diff) and
// plan_canvases.
type CodeChannelView struct {
	ViewID         string `json:"view_id"`
	Type           string `json:"type"`
	Name           string `json:"name"`
	FileID         string `json:"file_id"`
	CanvasID       string `json:"canvas_id"`
	PRURL          string `json:"pr_url"`
	ContentVersion int    `json:"content_version"`
}

type codeProps struct {
	RecordChannel *struct {
		RecordID          string `json:"record_id"`
		RecordType        string `json:"record_type"`
		RecordLabel       string `json:"record_label"`
		RecordLabelPlural string `json:"record_label_plural"`
	} `json:"record_channel"`
	AgentSession *struct {
		Status         string            `json:"status"`
		Title          string            `json:"title"`
		AgentBotIDs    []json.RawMessage `json:"agent_bot_user_ids"`
		EncodedBotIDs  []string          `json:"encoded_agent_bot_user_ids"`
		ProcessingExpr json.Number       `json:"date_status_processing_expire"`
	} `json:"agent_session"`
	CodeChannel map[string]json.RawMessage `json:"code_channel"`
}

// ParseCodeChannel reads a conversation's properties; nil unless it is a
// code channel (Slack's aK5v.$n: a record channel whose record_type is
// "agent_channel" or whose record_id starts with "AC:").
func ParseCodeChannel(props json.RawMessage) *CodeChannel {
	if len(props) == 0 {
		return nil
	}
	var p codeProps
	if json.Unmarshal(props, &p) != nil || p.RecordChannel == nil || p.RecordChannel.RecordID == "" {
		return nil
	}
	rc := p.RecordChannel
	if rc.RecordType != "agent_channel" && !strings.HasPrefix(rc.RecordID, "AC:") {
		return nil
	}
	cc := &CodeChannel{RecordID: rc.RecordID, RecordType: rc.RecordType, RecordLabel: rc.RecordLabel, RecordPlural: rc.RecordLabelPlural}
	if s := p.AgentSession; s != nil {
		cc.SessionStatus, cc.Title = s.Status, s.Title
		cc.AgentBotIDs = append(cc.AgentBotIDs, s.EncodedBotIDs...)
		if len(cc.AgentBotIDs) == 0 {
			for _, r := range s.AgentBotIDs { // numbers or strings
				var str string
				if json.Unmarshal(r, &str) != nil {
					str = string(r)
				}
				if str != "" {
					cc.AgentBotIDs = append(cc.AgentBotIDs, str)
				}
			}
		}
		if n, err := s.ProcessingExpr.Int64(); err == nil && n > 0 {
			cc.ProcessingExpires = n
		}
	}
	c := p.CodeChannel
	cc.Empty = len(c) == 0
	str := func(k string) string {
		var s string
		_ = json.Unmarshal(c[k], &s)
		return s
	}
	cc.Repo, cc.Branch = str("repo"), str("branch")
	cc.PRStatus, cc.CIState, cc.PRTitle = str("pr_status"), str("ci_state"), str("pr_title")
	_ = json.Unmarshal(c["context_bar_items"], &cc.ContextBar)
	var sm struct {
		MessageTS string `json:"message_ts"`
	}
	if json.Unmarshal(c["summary_message"], &sm) == nil {
		cc.SummaryMessage = sm.MessageTS
	}
	cc.Views = codeChannelViews(c)
	return cc
}

// codeChannelViews is getCodeChannelViews (PntH.B) with session channels
// on and PR views off (session_channels_code_tab_pr is off for the test
// account): agent_session_views without pull requests, agent_views as
// html, diff_file_id as the Code diff, plan_canvases as canvases; the Code
// diff first.
func codeChannelViews(c map[string]json.RawMessage) []CodeChannelView {
	var out []CodeChannelView
	seen := map[string]bool{}
	add := func(v CodeChannelView) {
		if seen[v.ViewID] {
			return
		}
		seen[v.ViewID] = true
		out = append(out, v)
	}
	var views []CodeChannelView
	_ = json.Unmarshal(c["agent_session_views"], &views)
	for _, v := range views {
		if v.Type == ViewPullRequest {
			continue
		}
		seen[v.ViewID] = true
		out = append(out, v)
	}
	var html []struct {
		ViewID         string `json:"view_id"`
		FileID         string `json:"file_id"`
		ContentVersion int    `json:"content_version"`
	}
	_ = json.Unmarshal(c["agent_views"], &html)
	for _, h := range html {
		add(CodeChannelView{ViewID: h.ViewID, Type: ViewHTML, FileID: h.FileID, ContentVersion: max(h.ContentVersion, 1)})
	}
	var diff string
	_ = json.Unmarshal(c["diff_file_id"], &diff)
	if diff != "" {
		var ver int
		_ = json.Unmarshal(c["diff_file_version"], &ver)
		add(CodeChannelView{ViewID: "code", Type: ViewDiff, FileID: diff, ContentVersion: max(ver, 1)})
	}
	var plans []struct {
		CanvasID string `json:"canvas_id"`
	}
	_ = json.Unmarshal(c["plan_canvases"], &plans)
	for _, p := range plans {
		add(CodeChannelView{ViewID: p.CanvasID, Type: ViewCanvas, CanvasID: p.CanvasID})
	}
	for i, v := range out { // the Code diff sorts first
		if v.ViewID == "code" && i > 0 {
			copy(out[1:i+1], out[:i])
			out[0] = v
			break
		}
	}
	return out
}

// codeChannelStatus is getAgentChannelStatusAt (j9If.kB): closed and
// suspended win; processing holds until date_status_processing_expire
// passes; otherwise a channel quiet for 3 days is Stale (session_channels
// on), else Active. latest is the channel's latest message ts.
//
// Slack also counts a live busy signal (an assistant status of typing or
// banner, or a message still streaming) as Processing; Clack has neither
// signal yet.
func codeChannelStatus(cc *CodeChannel, latest string, sessionChannels bool, now time.Time) string {
	if cc == nil {
		return ""
	}
	switch cc.SessionStatus {
	case AgentClosed:
		return AgentClosed
	case AgentSuspended:
		return AgentSuspended
	case AgentProcessing:
		if cc.ProcessingExpires == 0 || now.Unix() <= cc.ProcessingExpires {
			return AgentProcessing
		}
		return AgentActive // resolveAgentChannelStatus: expired, no live signal
	}
	if sessionChannels {
		if ts := tsSeconds(latest); ts > 0 && now.Sub(time.Unix(ts, 0)) > codeChannelStaleAfter {
			return AgentStale
		}
	}
	return AgentActive
}

func tsSeconds(ts string) int64 {
	if i := strings.IndexByte(ts, '.'); i >= 0 {
		ts = ts[:i]
	}
	n, _ := strconv.ParseInt(ts, 10, 64)
	return n
}

// AgentStatusDisplay is getAgentStatusDisplay (WbzC) for a status and the
// PR status: the sidebar dot's icon ("" for the spinner while
// processing, or the default status-member dot) and its tooltip.
func AgentStatusDisplay(status, prStatus string) (icon, tooltip string) {
	switch status {
	case AgentClosed:
		return "check-circle", AgentStatusLabel(status)
	case AgentStale:
		return "minus", AgentStatusLabel(status)
	case AgentSuspended:
		return "needs-attention", AgentStatusLabel(status)
	case AgentProcessing:
		return "", AgentStatusLabel(status)
	}
	switch prStatus {
	case "open", "draft":
		return "pr-open", "PR open"
	case "blocked":
		return "needs-attention", "PR blocked"
	case "closed":
		return "pr-closed", "PR closed"
	case "merged":
		return "branch", "PR merged"
	}
	return "", "Idle"
}

// AgentStatusLabel is getAgentStatusLabel (UaSM).
func AgentStatusLabel(status string) string {
	switch status {
	case AgentClosed:
		return "Done"
	case AgentProcessing:
		return "Working"
	case AgentSuspended:
		return "Needs attention"
	}
	return "Inactive"
}

// codeChannelOf parses c's properties (nil for other channels).
func codeChannelOf(c *slack.Conversation) *CodeChannel {
	if c == nil || c.IsIM || c.IsMPIM {
		return nil
	}
	return ParseCodeChannel(c.Properties)
}

// sessionChannelsOn is the session_channels experiment, which turns on
// the Code channels section, the status dots and the agent context bar.
func (a *App) sessionChannelsOn() bool { return a.experiment("session_channels") == "on" }

// CodeStatus is a code channel's effective agent status now ("" for
// other channels).
func (a *App) CodeStatus(ch string) string {
	c, ok := a.S.Conv(ch)
	if !ok {
		if c, ok = a.S.ExtConv(ch); !ok {
			return ""
		}
	}
	return a.codeStatusOf(c, codeChannelOf(c))
}

func (a *App) codeStatusOf(c *slack.Conversation, cc *CodeChannel) string {
	if cc == nil {
		return ""
	}
	latest := ""
	if e, ok := a.S.Count(c.ID); ok {
		latest = e.Latest
	}
	return codeChannelStatus(cc, latest, a.sessionChannelsOn(), time.Now())
}

// AgentName is useAgentName (TDkr): the session's first agent's name, or
// "Agent".
func (a *App) AgentName(cc *CodeChannel) string {
	if cc != nil && len(cc.AgentBotIDs) > 0 {
		if n := a.S.UserName(cc.AgentBotIDs[0]); n != "" && n != cc.AgentBotIDs[0] {
			return n
		}
	}
	return "Agent"
}

// CodeChannelAction runs a context bar action (dispatchContextBarActionThunk):
// client.codeChannels.action {channel_id, action_key, client_token}.
func (a *App) CodeChannelAction(ctx context.Context, ch, key string) error {
	return a.C.Call(ctx, "client.codeChannels.action", url.Values{
		"channel_id": {ch}, "action_key": {key}, "client_token": {a.Interactive().ClientToken()},
	}, nil)
}

// StopAgent is the working chip's Stop (agent-working-nugget):
// agents.sessions.stop {channel, reason: "stopped-by-user"}.
func (a *App) StopAgent(ctx context.Context, ch string) error {
	return a.C.Call(ctx, "agents.sessions.stop", url.Values{"channel": {ch}, "reason": {"stopped-by-user"}}, nil)
}

// CodeChannelBlockKitView fetches a block_kit view (CodeChannelBlockKitView):
// client.codeChannels.getBlockKitView {channel_id, view_id}, answering
// {blocks, service_id, app_id}.
func (a *App) CodeChannelBlockKitView(ctx context.Context, ch, viewID string) (json.RawMessage, error) {
	raw, err := a.C.Raw(ctx, "client.codeChannels.getBlockKitView", url.Values{"channel_id": {ch}, "view_id": {viewID}})
	if err != nil {
		return nil, err
	}
	var r struct {
		Blocks json.RawMessage `json:"blocks"`
	}
	_ = json.Unmarshal(raw, &r)
	return r.Blocks, nil
}

// onChannelUpdated is Slack's channelUpdated RTM handler for the part a
// code channel needs: the event's updates (channel_updated {channel |
// channels, updates}) merge into the stored channel, so an agent's
// status, title, context bar and views change live. It reports the
// channels it changed.
func (a *App) onChannelUpdated(raw json.RawMessage) []string {
	var e struct {
		Channel  json.RawMessage `json:"channel"`
		Channels []string        `json:"channels"`
		Updates  map[string]any  `json:"updates"`
	}
	if json.Unmarshal(raw, &e) != nil || len(e.Updates) == 0 {
		return nil // "Received channel_updated with no updates"
	}
	ids := e.Channels
	if len(ids) == 0 {
		var id string
		if json.Unmarshal(e.Channel, &id) != nil {
			var c struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(e.Channel, &c)
			id = c.ID
		}
		if id != "" {
			ids = []string{id}
		}
	}
	var changed []string
	for _, id := range ids {
		cur, ok := a.S.Conv(id)
		ext := false
		if !ok {
			if cur, ok = a.S.ExtConv(id); !ok {
				continue // "Received channel_updated on missing channel"
			}
			ext = true
		}
		b, err := json.Marshal(cur)
		if err != nil {
			continue
		}
		var m map[string]any
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		mergeJSON(m, e.Updates)
		b, err = json.Marshal(m)
		if err != nil {
			continue
		}
		var next slack.Conversation
		if json.Unmarshal(b, &next) != nil {
			continue
		}
		next.ID = id
		if ext {
			a.S.PutExtConv(next)
		} else {
			a.S.PutConv(next)
		}
		changed = append(changed, id)
	}
	return changed
}

// mergeJSON deep-merges src into dst (objects merge key by key; anything
// else replaces), as Slack's channel upsert merges an update.
func mergeJSON(dst, src map[string]any) {
	for k, v := range src {
		if sv, ok := v.(map[string]any); ok {
			if dv, ok := dst[k].(map[string]any); ok {
				mergeJSON(dv, sv)
				continue
			}
		}
		dst[k] = v
	}
}
