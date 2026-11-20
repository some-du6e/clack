package fakeslack

import (
	"encoding/json"

	"github.com/some-du6e/clack/internal/slack"
)

// An agent session request in a channel, in the shapes of the message
// Karim saw drawn wrong (2026-11-20; read as shapes only): the person's
// message to the agent (rich_text with the agent's mention, a context
// block of one mrkdwn element, agent_session with its agent_statuses, and
// one attachment that is only agent_channel_unfurl, the code channel the
// agent made: CodeWorkingID); the agent's streamed reply in its thread
// (rich_text with a channel mention, context, streaming_state completed,
// edited); and a post linking the request (a message_mention, and the
// is_msg_unfurl attachment of it). Size.AgentSessionRequests (with
// CodeChannels). Synthetic only.
const (
	AgentSessChannelID = "C0AGSESS001" // #agent-requests
	AgentSessRootTS    = "1763600000.000100"
	AgentSessReplyTS   = "1763600004.000200"
	AgentSessLinkTS    = "1763600100.000300"
	// a second request whose code channel is private and the viewer isn't
	// in it: its card is the joinable one
	AgentSessPrivTS    = "1763600200.000400"
	AgentSessPrivateID = "C0CODE00010" // #hotfix-payments, private, codeHidden until joined
	AgentSessPrivName  = "hotfix-payments"
)

// AgentSessionRequest is the person's message to the agent.
func AgentSessionRequest() map[string]any {
	return map[string]any{
		"type": "message", "ts": AgentSessRootTS, "user": SelfID, "team": TeamID, "client_msg_id": "c5e5a1f0-0000-4000-8000-00000000a001",
		"text": "<@" + CodeAgentID + "> fix the login redirect loop on the staging site",
		"blocks": []any{
			map[string]any{"type": "rich_text", "block_id": "rq1", "elements": []any{map[string]any{"type": "rich_text_section", "elements": []any{
				map[string]any{"type": "user", "user_id": CodeAgentID},
				map[string]any{"type": "text", "text": " fix the login redirect loop on the staging site"}}}}},
			map[string]any{"type": "context", "block_id": "agent_session_context_rq1", "elements": []any{
				map[string]any{"type": "mrkdwn", "text": "Started a code channel with the agent", "verbatim": false}}},
		},
		"agent_session": map[string]any{"status": "active", "agent_bot_user_ids": []any{CodeAgentID},
			"encoded_initiator_user_id": SelfID,
			"agent_statuses":            []any{map[string]any{"agent_bot_user_id": CodeAgentID, "status": "active", "is_stoppable": false}}},
		"attachments": []any{map[string]any{"id": 1, "fallback": "Code channel",
			"agent_channel_unfurl": map[string]any{"channel_id": CodeWorkingID, "channel_name": "fix-login-redirect", "is_private": false,
				"agent_bot_user_id": CodeAgentID, "joinable_from_channel_id": AgentSessChannelID}}},
		"is_locked": false, "thread_ts": AgentSessRootTS, "reply_count": 1, "reply_users": []any{CodeAgentID}, "reply_users_count": 1,
		"latest_reply": AgentSessReplyTS, "subscribed": false,
	}
}

// AgentSessionPrivateRequest is a request whose code channel is private
// and that the viewer isn't in: the same shapes, its agent_channel_unfurl
// is_private, joinable from #agent-requests.
func AgentSessionPrivateRequest() map[string]any {
	m := AgentSessionRequest()
	for _, k := range []string{"thread_ts", "reply_count", "reply_users", "reply_users_count", "latest_reply", "subscribed"} {
		delete(m, k)
	}
	m["ts"], m["client_msg_id"] = AgentSessPrivTS, "c5e5a1f0-0000-4000-8000-00000000a004"
	m["text"] = "<@" + CodeAgentID + "> the payments webhook retries forever, find out why"
	m["blocks"] = []any{
		map[string]any{"type": "rich_text", "block_id": "rq2", "elements": []any{map[string]any{"type": "rich_text_section", "elements": []any{
			map[string]any{"type": "user", "user_id": CodeAgentID},
			map[string]any{"type": "text", "text": " the payments webhook retries forever, find out why"}}}}},
		map[string]any{"type": "context", "block_id": "agent_session_context_rq2", "elements": []any{
			map[string]any{"type": "mrkdwn", "text": "Started a private code channel with the agent", "verbatim": false}}},
	}
	m["attachments"] = []any{map[string]any{"id": 1, "fallback": "Code channel",
		"agent_channel_unfurl": map[string]any{"channel_id": AgentSessPrivateID, "channel_name": AgentSessPrivName, "is_private": true,
			"agent_bot_user_id": CodeAgentID, "joinable_from_channel_id": AgentSessChannelID}}}
	return m
}

// agentSessPrivateChannel is the private code channel, as
// client.codeChannels.join answers it once the viewer is in.
func agentSessPrivateChannel() slack.Conversation {
	props, _ := json.Marshal(map[string]any{
		"record_channel": map[string]any{"record_id": codeChannelRecord + AgentSessPrivateID, "record_type": "agent_channel",
			"record_label": "Code Channel", "record_label_plural": "Code Channels"},
		"code_channel":  map[string]any{"repo": "example/payments", "branch": "webhook-retries"},
		"agent_session": map[string]any{"status": "active", "title": "Webhook retries", "agent_bot_user_ids": []any{}, "encoded_agent_bot_user_ids": []string{CodeAgentID}},
	})
	return slack.Conversation{ID: AgentSessPrivateID, Name: AgentSessPrivName, IsChannel: true, IsPrivate: true, IsMember: false,
		Created: 1763600190, Creator: "U0FAKE00002", NumMembers: 2, ContextTeamID: TeamID, Properties: props,
		Purpose: &slack.Purpose{Value: "Code channel"}}
}

// AgentSessionReply is the agent's reply in the request's thread.
func AgentSessionReply() map[string]any {
	return map[string]any{
		"type": "message", "ts": AgentSessReplyTS, "thread_ts": AgentSessRootTS, "parent_user_id": SelfID, "user": CodeAgentID,
		"bot_id": CodeAgentBotID, "app_id": "A0AGENT0001", "team": TeamID,
		"text": "On it in <#" + CodeWorkingID + ">: I'll trace the redirect and post the fix there.",
		"blocks": []any{
			map[string]any{"type": "rich_text", "block_id": "rp1", "elements": []any{map[string]any{"type": "rich_text_section", "elements": []any{
				map[string]any{"type": "text", "text": "On it in "},
				map[string]any{"type": "channel", "channel_id": CodeWorkingID},
				map[string]any{"type": "text", "text": ": I'll trace the redirect and post the fix there."}}}}},
			map[string]any{"type": "context", "block_id": "agent_reply_ctx", "elements": []any{
				map[string]any{"type": "mrkdwn", "text": "Codebot is working", "verbatim": false}}},
		},
		"bot_profile": map[string]any{"id": CodeAgentBotID, "app_id": "A0AGENT0001", "user_id": CodeAgentID, "name": "Codebot",
			"team_id": TeamID, "deleted": false},
		"streaming_state": "completed", "edited": map[string]any{"user": CodeAgentID, "ts": "1763600009.000000"},
	}
}

// AgentSessionLinkPost is a post linking the request: its message_mention
// and the request's is_msg_unfurl attachment (the blocks, no inner card).
func AgentSessionLinkPost() map[string]any {
	url := "https://fake.slack.com/archives/" + AgentSessChannelID + "/p" + "1763600000000100"
	req := AgentSessionRequest()
	return map[string]any{
		"type": "message", "ts": AgentSessLinkTS, "user": SelfID, "team": TeamID, "client_msg_id": "c5e5a1f0-0000-4000-8000-00000000a003",
		"text": "this is drawn oddly <" + url + ">",
		"blocks": []any{map[string]any{"type": "rich_text", "block_id": "lk1", "elements": []any{map[string]any{"type": "rich_text_section", "elements": []any{
			map[string]any{"type": "text", "text": "this is drawn oddly "},
			map[string]any{"type": "message_mention", "channel_id": AgentSessChannelID, "message_ts": AgentSessRootTS, "author_id": SelfID,
				"text": url, "url": url},
			map[string]any{"type": "text", "text": " in the request"}}}}}},
		"attachments": []any{map[string]any{"id": 1, "is_msg_unfurl": true, "is_thread_root_unfurl": true,
			"author_id": SelfID, "author_name": "you", "author_subname": "you", "author_link": "https://fake.slack.com/team/" + SelfID,
			"author_icon": "https://fake.slack.com/img/48x48/self.png", "channel_id": AgentSessChannelID, "channel_team": TeamID,
			"fallback": "[request] fix the login redirect loop", "footer": "Thread in #agent-requests", "from_url": url, "original_url": url,
			"text": req["text"], "ts": AgentSessRootTS, "mrkdwn_in": []any{"text"}, "blocks": req["blocks"]}},
	}
}

// addAgentSessionMessages puts the requests and the link post in
// #agent-requests (the reply is AgentSessionReply's, for thread tests),
// and the private request's channel where only client.codeChannels.join
// finds it.
func (s *Server) addAgentSessionMessages() {
	var msgs []slack.Message
	for _, raw := range []map[string]any{AgentSessionPrivateRequest(), AgentSessionLinkPost(), AgentSessionRequest()} { // history is newest first
		b, _ := json.Marshal(raw)
		var m slack.Message
		if json.Unmarshal(b, &m) == nil {
			msgs = append(msgs, m)
		}
	}
	s.convs = append(s.convs, slack.Conversation{ID: AgentSessChannelID, Name: "agent-requests", IsChannel: true, IsMember: true,
		Created: 1763000000, Creator: SelfID, NumMembers: 2, ContextTeamID: TeamID})
	s.hist[AgentSessChannelID] = msgs
	s.codeHidden = append(s.codeHidden, agentSessPrivateChannel())
	s.hist[AgentSessPrivateID] = []slack.Message{{Type: "message", TS: "1763600210.000100", User: CodeAgentID, BotID: CodeAgentBotID,
		Text: "Looking at the retry policy.", Blocks: json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"Looking at the retry policy."}]}]}]`)}}
}
