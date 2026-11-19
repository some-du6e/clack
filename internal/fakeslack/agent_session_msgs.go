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

// addAgentSessionMessages puts the request and the link post in
// #agent-requests (the reply is AgentSessionReply's, for thread tests).
func (s *Server) addAgentSessionMessages() {
	var msgs []slack.Message
	for _, raw := range []map[string]any{AgentSessionLinkPost(), AgentSessionRequest()} { // history is newest first
		b, _ := json.Marshal(raw)
		var m slack.Message
		if json.Unmarshal(b, &m) == nil {
			msgs = append(msgs, m)
		}
	}
	s.convs = append(s.convs, slack.Conversation{ID: AgentSessChannelID, Name: "agent-requests", IsChannel: true, IsMember: true,
		Created: 1763000000, Creator: SelfID, NumMembers: 2, ContextTeamID: TeamID})
	s.hist[AgentSessChannelID] = msgs
}
