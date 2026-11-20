package core

import (
	"time"
)

// The reply bar's agent nugget (web client build 9685e6ce144b, r0Yq
// ThreadReplies): a message with no reply, thread draft or scheduled reply
// (or a broadcast) draws AppSessionReplyNugget in its reply bar's place
// while app_thread_nugget_fe is on and its thread has an agent at work:
// the root's agent_session.status is processing
// (getAgentSessionStatusInThread, wClv hY), or an app's status shows in
// the thread (HQze ZL), and it isn't a Slackbot agent thread (HQze zV).
// Under app_nugget_refinement the nugget is AppSessionOpenThreadReplyNugget:
//
//   - the processing agents (HQze it over the root's agent_statuses, or
//     its agent_bot_user_ids; n7Jc useEffectiveAgentStatuses: processing
//     past date_status_processing_expire reads as active): the first as a
//     button (AppAgentReplyNugget) with its icon, "{app} is working" or its
//     loading_message, and a caret, which opens the thread; two or more as
//     one collapsed button, "Multiple agents are working";
//   - with none processing, the app's status as the same button (its
//     loading messages in turn, else "{app} is working");
//   - else nothing.
//
// Without the refinement it is AppSessionStopReplyNugget: a row per
// processing agent, a Stop button for one that is_stoppable. That one
// isn't in Clack (the account has the refinement).

// SessionAgent is one agent of a thread root's agent_session as the nugget
// reads it (HQze it, resolveAgentIdentity).
type SessionAgent struct {
	BotUserID, AppID, Name, IconEmoji, IconURL, LoadingMessage string
	// Status is the effective one (getEffectiveAgentStatus): processing
	// past its expiry is active.
	Status    string
	Stoppable bool
}

// SessionNugget is what the reply bar's nugget draws.
type SessionNugget struct {
	// Agents are the processing agents, in agent_statuses' order.
	Agents []SessionAgent
	// Status is the app's status in the thread, drawn when no agent is
	// processing (HQze ZL).
	Status AppNugget
	// OpenThread is app_nugget_refinement (always, in what Clack builds).
	OpenThread bool
}

// SessionReplyNugget reports the nugget for message m's reply bar at now,
// or false where Slack draws neither a nugget nor a reply bar. The caller
// asks only for a root with no reply, draft or scheduled reply, or a
// broadcast.
func (a *App) SessionReplyNugget(m *Message, now time.Time) (SessionNugget, bool) {
	if m == nil || a.experiment("app_thread_nugget_fe") != "on" || a.experiment("app_nugget_refinement") != "on" {
		return SessionNugget{}, false
	}
	root := m
	if m.ThreadTS != "" && m.ThreadTS != m.TS { // a broadcast: its thread's root
		if r, ok := a.S.Find(m.Channel, m.ThreadTS); ok {
			root = a.messagePtr(m.Channel, &r)
		} else {
			root = &Message{Channel: m.Channel, TS: m.ThreadTS}
		}
	}
	thread := root.TS
	if st, ok := a.AssistantStatusFor(m.Channel, thread); ok && st.Status != "" && st.BotUserID == "USLACKBOT" {
		return SessionNugget{}, false // isSlackbotAgentThread: Slackbot's status
	}
	agents := a.sessionAgents(root, now)
	status, hasStatus := a.AppThreadNugget(m.Channel, thread) // ZL
	processing := root.AgentSession != nil && root.AgentSession.Status == AgentProcessing
	if !processing && !hasStatus || sessionIsSlackbot(root) {
		return SessionNugget{}, false
	}
	var out SessionNugget
	out.OpenThread = true
	for _, ag := range agents {
		if ag.Status == AgentProcessing {
			out.Agents = append(out.Agents, ag)
		}
	}
	if len(out.Agents) == 0 {
		if !hasStatus {
			return SessionNugget{}, false
		}
		out.Status = status
	}
	return out, true
}

// sessionAgents is HQze it with useEffectiveAgentStatuses' status: the
// root's agent_statuses, or one agent from agent_bot_user_ids with the
// session's status.
func (a *App) sessionAgents(root *Message, now time.Time) []SessionAgent {
	s := root.AgentSession
	if s == nil {
		return nil
	}
	sec := now.Unix()
	effective := func(status string, expire int64) string {
		if status == AgentProcessing && expire > 0 && sec >= expire {
			return AgentActive
		}
		return status
	}
	if sts := s.Statuses(); len(sts) > 0 {
		out := make([]SessionAgent, 0, len(sts))
		for _, st := range sts {
			ag := a.sessionAgentIdentity(st.AgentBotUserID, st.Username)
			ag.IconEmoji, ag.IconURL = st.IconEmoji, st.IconURL
			ag.Status = effective(st.Status, st.ProcessingExpireSeconds())
			ag.LoadingMessage, ag.Stoppable = st.LoadingMessage, st.IsStoppable
			out = append(out, ag)
		}
		return out
	}
	ids := s.BotUserIDs()
	id := ""
	if len(ids) > 0 {
		id = ids[0]
	}
	ag := a.sessionAgentIdentity(id, "")
	ag.Status = effective(s.Status, s.ProcessingExpireSeconds())
	if st, ok := a.AssistantStatusFor(root.Channel, root.TS); ok && len(st.LoadingMessages) > 0 {
		ag.LoadingMessage = st.LoadingMessages[len(st.LoadingMessages)-1]
	}
	return []SessionAgent{ag}
}

// sessionAgentIdentity is resolveAgentIdentity: the bot user's app, and
// the status's username, else the member's name.
func (a *App) sessionAgentIdentity(botUserID, username string) SessionAgent {
	ag := SessionAgent{BotUserID: botUserID, Name: username}
	if u, ok := a.S.User(botUserID); ok && u != nil {
		ag.AppID = u.Profile.APIAppID
		if ag.Name == "" {
			ag.Name = firstNonEmpty(u.Profile.DisplayName, u.Profile.RealName, u.RealName)
		}
	}
	return ag
}

// sessionIsSlackbot is isSlackbotAgentThread's session half: every agent
// of the root's agent_session is Slackbot (its app status half is
// AppThreadNugget's).
func sessionIsSlackbot(root *Message) bool {
	s := root.AgentSession
	if s == nil {
		return false
	}
	var ids []string
	if sts := s.Statuses(); len(sts) > 0 {
		for _, st := range sts {
			ids = append(ids, st.AgentBotUserID)
		}
	} else {
		ids = s.BotUserIDs()
	}
	if len(ids) == 0 {
		return false
	}
	for _, id := range ids {
		if id != "USLACKBOT" {
			return false
		}
	}
	return true
}
