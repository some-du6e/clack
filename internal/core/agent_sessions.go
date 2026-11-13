package core

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// The rail's Agents & tools peek reads two lists (web client build
// f132f0e78468, boot-render Zxh6 AutomationsPeekView):
//
//   - Agent conversations: fetchAgentSessions (boot-shared p9pF) calls
//     ai.alpha.agents.sessions.list {reason: "recent_sessions_sidebar"}
//     and keeps the sessions that aren't is_archived, in the order they
//     come: a "thread" session is its root message (thread, upserted as a
//     message: agent_session.title, agent_bot_user_ids, latest_reply…), a
//     "channel" session its channel (channel_id, the channel object,
//     last_activity_ts); is_pinned splits Pinned from Recent.
//   - Workflows: fetchAndUpsertRecentWorkflows (boot-render Hdvm, h9ME)
//     calls workflows.triggers.listRecentlyRunForUser {reason:
//     "fetch_and_upsert_recently_run_triggers"}: recently_run_triggers
//     are the triggers, newest run first (getRecentlyRunTriggerIds keeps
//     their order).

// AgentSession is one ai.alpha.agents.sessions.list session.
type AgentSession struct {
	Kind    string // "thread" | "channel"
	Channel string
	TS      string // the thread's root (thread sessions)
	Pinned  bool
	// LastActivity is a channel session's last_activity_ts.
	LastActivity string
	// Root is a thread session's root message (nil for channels).
	Root *Message
	// AssistantTitle is the root's assistant_app_thread.title.
	AssistantTitle string
	// RootLastRead is the root's last_read (the viewer's last read reply).
	RootLastRead string
	// BotUserID is the session's first agent: agent_session's
	// agent_bot_user_ids, or a channel's properties.agent_session.
	// encoded_agent_bot_user_ids.
	BotUserID string
	// IconURL / IconEmoji / Username are that agent's agent_statuses
	// identity override (thread sessions).
	IconURL, IconEmoji, Username string
	// ChannelName is a channel session's name (the channel object's).
	ChannelName string
}

// rawAgentSession is a sessions.list entry.
type rawAgentSession struct {
	SessionType    string          `json:"session_type"`
	Thread         json.RawMessage `json:"thread"`
	ChannelID      string          `json:"channel_id"`
	Channel        json.RawMessage `json:"channel"`
	IsPinned       bool            `json:"is_pinned"`
	IsArchived     bool            `json:"is_archived"`
	LastActivityTS json.Number     `json:"last_activity_ts"`
}

// AgentSessions is fetchAgentSessions' list: the sessions that aren't
// archived, threads with their root and channels with theirs.
func (a *App) AgentSessions(ctx context.Context) ([]AgentSession, error) {
	var res struct {
		Sessions []rawAgentSession `json:"sessions"`
	}
	err := a.C.Call(ctx, "ai.alpha.agents.sessions.list", url.Values{"reason": {"recent_sessions_sidebar"}}, &res)
	logCall("ai.alpha.agents.sessions.list", "n="+strconv.Itoa(len(res.Sessions)), err)
	if err != nil {
		return nil, err
	}
	var out []AgentSession
	for _, r := range res.Sessions {
		if r.IsArchived {
			continue
		}
		switch r.SessionType {
		case "thread":
			if s, ok := a.threadSession(r); ok {
				out = append(out, s)
			}
		case "channel":
			if r.ChannelID == "" {
				continue
			}
			s := AgentSession{Kind: "channel", Channel: r.ChannelID, Pinned: r.IsPinned, LastActivity: r.LastActivityTS.String()}
			var ch struct {
				slack.Conversation
				Properties struct {
					AgentSession struct {
						Bots []string `json:"encoded_agent_bot_user_ids"`
					} `json:"agent_session"`
				} `json:"properties"`
			}
			if len(r.Channel) > 0 && json.Unmarshal(r.Channel, &ch) == nil {
				s.ChannelName = ch.Name
				if len(ch.Properties.AgentSession.Bots) > 0 {
					s.BotUserID = ch.Properties.AgentSession.Bots[0]
				}
				var conv slack.Conversation
				if json.Unmarshal(r.Channel, &conv) == nil && conv.ID != "" {
					if _, known := a.S.Conv(conv.ID); !known { // upsertChannels: the store learns it
						a.S.PutConv(conv)
					}
				}
			}
			if s.ChannelName == "" {
				s.ChannelName = a.S.ChannelName(r.ChannelID)
			}
			out = append(out, s)
		}
	}
	return out, nil
}

// threadSession reads a "thread" session: its root message.
func (a *App) threadSession(r rawAgentSession) (AgentSession, bool) {
	if len(r.Thread) == 0 {
		return AgentSession{}, false
	}
	var m slack.Message
	if json.Unmarshal(r.Thread, &m) != nil || m.Channel == "" || m.TS == "" {
		return AgentSession{}, false
	}
	var extra struct {
		AssistantAppThread struct {
			Title string `json:"title"`
		} `json:"assistant_app_thread"`
	}
	_ = json.Unmarshal(r.Thread, &extra)
	root := a.message(m.Channel, &m)
	s := AgentSession{Kind: "thread", Channel: m.Channel, TS: m.TS, Pinned: r.IsPinned, Root: &root,
		AssistantTitle: extra.AssistantAppThread.Title, RootLastRead: m.LastRead}
	if ids := m.AgentSession.BotUserIDs(); len(ids) > 0 {
		s.BotUserID = ids[0]
		for _, st := range m.AgentSession.Statuses() {
			if st.AgentBotUserID == s.BotUserID {
				s.IconURL, s.IconEmoji, s.Username = st.IconURL, st.IconEmoji, st.Username
				break
			}
		}
	}
	return s, true
}

// Title is getAgentSessionTitle (boot-shared RkDi) for a thread session:
// agent_session.title, "Huddle in #name" for a huddle thread, the
// assistant thread's title, the root's text as plain text, else
// "Untitled session". A channel session is titled with its channel's
// name (W+Ke getChannelDisplayName without the #).
func (s AgentSession) Title(plain func(*Message) string, channelName func(string) string) string {
	if s.Kind == "channel" {
		if n := channelName(s.Channel); n != "" {
			return n
		}
		return s.ChannelName
	}
	m := s.Root
	if m == nil {
		return "Untitled session"
	}
	if t := m.AgentSession.SessionTitle(); t != "" {
		return t
	}
	if m.Subtype == "huddle_thread" || m.Subtype == "huddle_message" { // R+b2 isHuddleMessage
		if n := channelName(s.Channel); n != "" {
			return "Huddle in #" + n
		}
	}
	if s.AssistantTitle != "" {
		return s.AssistantTitle
	}
	if t := strings.TrimSpace(plain(m)); t != "" {
		return t
	}
	return "Untitled session"
}

// ActivityTS is the row's "Updated" time: a thread's latest_reply or its
// root's ts; a channel's last_activity_ts.
func (s AgentSession) ActivityTS() string {
	if s.Kind == "channel" {
		return s.LastActivity
	}
	if s.Root != nil && s.Root.LatestReply != "" {
		return s.Root.LatestReply
	}
	return s.TS
}

// RecentlyRunTriggers is fetchAndUpsertRecentlyRunTriggers (boot-render
// h9ME): workflows.triggers.listRecentlyRunForUser's recently_run_triggers,
// in their order. The triggers join the ones Trigger knows.
func (a *App) RecentlyRunTriggers(ctx context.Context) ([]WorkflowTrigger, error) {
	var res struct {
		Triggers []rawTrigger `json:"recently_run_triggers"`
	}
	err := a.C.Call(ctx, "workflows.triggers.listRecentlyRunForUser",
		url.Values{"reason": {"fetch_and_upsert_recently_run_triggers"}}, &res)
	logCall("workflows.triggers.listRecentlyRunForUser", "n="+strconv.Itoa(len(res.Triggers)), err)
	if err != nil {
		return nil, err
	}
	out := make([]WorkflowTrigger, 0, len(res.Triggers))
	for _, r := range res.Triggers {
		if t := r.trigger(); t.ID != "" {
			out = append(out, t)
		}
	}
	a.Interactive().putTriggers(out...)
	return out, nil
}

// The agent session menu's writes (boot-shared 3DaI useSessionActions,
// n5nG useRenameAgentThread; methods from boot-vendors LSN0, KDGJ, ek0Z,
// d+ht): {channel, thread_ts for a thread session, reason}.

func sessionForm(ch, threadTS, reason string) url.Values {
	f := url.Values{"channel": {ch}, "reason": {reason}}
	if threadTS != "" {
		f.Set("thread_ts", threadTS)
	}
	return f
}

// PinAgentSession is pinAgentSession: agents.threads.pin, or unpin when
// pinned is false (reason agent_session_sidebar).
func (a *App) PinAgentSession(ctx context.Context, ch, threadTS string, pinned bool) error {
	m := "agents.threads.unpin"
	if pinned {
		m = "agents.threads.pin"
	}
	err := a.C.Call(ctx, m, sessionForm(ch, threadTS, "agent_session_sidebar"), nil)
	logCall(m, ch+" "+threadTS, err)
	return err
}

// ArchiveAgentSession is archiveAgentSession (the menu's Close
// conversation): agents.threads.archive (reason agent_session_sidebar).
func (a *App) ArchiveAgentSession(ctx context.Context, ch, threadTS string) error {
	err := a.C.Call(ctx, "agents.threads.archive", sessionForm(ch, threadTS, "agent_session_sidebar"), nil)
	logCall("agents.threads.archive", ch+" "+threadTS, err)
	return err
}

// RenameAgentThread is useRenameAgentThread's renameThread:
// agents.threads.rename {channel, thread_ts, name, reason
// rename_agent_thread}. The error's code picks the modal's message.
func (a *App) RenameAgentThread(ctx context.Context, ch, threadTS, name string) error {
	f := sessionForm(ch, threadTS, "rename_agent_thread")
	f.Set("name", name)
	err := a.C.Call(ctx, "agents.threads.rename", f, nil)
	logCall("agents.threads.rename", ch+" "+threadTS, err)
	return err
}

// SessionWithAgentApp is +RDp's B (Qek0 dy) for a session's channel: a DM
// whose person is an agent app (its app profile's legacy_app.is_agent_app,
// once the profile is loaded); such sessions can't be renamed.
func (a *App) SessionWithAgentApp(ch string) bool {
	c, ok := a.S.Conv(ch)
	if !ok || c.User == "" {
		return false
	}
	card, ok := a.CachedAppCard(c.User, "")
	return ok && card.AgentApp
}
