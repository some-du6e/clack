package core

// The fourth wave of the account's experiments (branch exp-wave-4, CDN
// build fa938a086569, 2026-11-13; docs/EXPERIMENTS.md "wave 4"), each read
// the way Slack's client reads it.

import (
	"encoding/json"

	"github.com/some-du6e/clack/internal/slack"
)

// AgentChannelThreadNaming is client-boot HLQ9 GL: "on" ===
// getGroupForUser("agent_channel_thread_naming").
func (a *App) AgentChannelThreadNaming() bool {
	return a.experiment("agent_channel_thread_naming") == "on"
}

// AgentDMThreadNaming is HLQ9 AM: "on" ===
// getGroupForUser("agent_dm_thread_naming").
func (a *App) AgentDMThreadNaming() bool {
	return a.experiment("agent_dm_thread_naming") == "on"
}

// AgentAppChannel is Qek0 F1 (the thread pane's isAgentAppChannel) as it
// reads while support_lots_of_agents isn't assigned (the account's case;
// then getMemberAgentIdentity answers nothing): ch is a DM whose person's
// app profile is an active agent app (SM2u isActiveAgentAppProfile:
// legacy_app.is_agent_app, is_shown_as_agent true or missing). False
// until the profile is cached (Slack's selector reads the store too);
// LoadAgentAppChannel asks for it.
func (a *App) AgentAppChannel(ch string) bool {
	c, ok := a.S.Conv(ch)
	if !ok || !c.IsIM || c.User == "" {
		return false
	}
	card, ok := a.CachedAppCard(c.User, "")
	return ok && card.ActiveAgentApp
}

// AgentAppChannelPending reports a DM with a bot whose app profile isn't
// cached yet: AgentAppChannel can still change once it is.
func (a *App) AgentAppChannelPending(ch string) (user string, pending bool) {
	c, ok := a.S.Conv(ch)
	if !ok || !c.IsIM || c.User == "" {
		return "", false
	}
	u, ok := a.S.User(c.User)
	if !ok || (!u.IsBot && u.Profile.BotID == "") {
		return "", false
	}
	if _, ok := a.CachedAppCard(c.User, ""); ok {
		return "", false
	}
	return c.User, true
}

// ThreadTitle is the thread pane header's title (srGL renderThreadTitle).
type ThreadTitle struct {
	// Text is the title; "" draws "Thread" (EditableThreadTitle's and
	// AssistantThreadTitleLabel's fallback).
	Text string
	// Editable is EditableThreadTitle (bCNd): the title row with the "Edit
	// thread name" button that opens EditAgentThreadNameModal.
	Editable bool
}

// ThreadHeaderTitle is renderThreadTitle for the thread ch/ts with its
// root (nil before it loads):
//
//   - agent_channel_thread_naming, not an agent app DM, not a DM, and the
//     root has an agent_session: EditableThreadTitle with the session's
//     title;
//   - an agent app DM under agent_dm_thread_naming: EditableThreadTitle
//     with agent_session?.title ?? assistant_app_thread?.title;
//   - a DM whose root is an assistant thread: AgentThreadTitle (5RBz) with
//     its title, not editable;
//   - else "Thread".
func (a *App) ThreadHeaderTitle(ch, ts string, root *Message) ThreadTitle {
	if ch == "" || ts == "" {
		return ThreadTitle{}
	}
	c, ok := a.S.Conv(ch)
	isDM := ok && c.IsIM
	agentApp := a.AgentAppChannel(ch)
	switch {
	case a.AgentChannelThreadNaming() && !agentApp && !isDM && root != nil && root.AgentSession != nil:
		return ThreadTitle{Text: root.AgentSession.SessionTitle(), Editable: true}
	case agentApp && a.AgentDMThreadNaming():
		t := ThreadTitle{Editable: true}
		if root != nil {
			if s, ok := root.AgentSession.TitleSet(); ok {
				t.Text = s
			} else if root.AssistantThread != nil {
				t.Text = root.AssistantThread.Title
			}
		}
		return t
	case isDM && root != nil && root.AssistantThread != nil:
		return ThreadTitle{Text: root.AssistantThread.Title}
	}
	return ThreadTitle{}
}

// AgentThreadNameOf is EditAgentThreadNameModal's starting name:
// agent_session?.title ?? assistant_app_thread?.title (zoAg m keeps the
// text as it is for the plain field).
func AgentThreadNameOf(root *Message) string {
	if root == nil {
		return ""
	}
	if s, ok := root.AgentSession.TitleSet(); ok {
		return s
	}
	if root.AssistantThread != nil {
		return root.AssistantThread.Title
	}
	return ""
}

// SetAgentSessionTitle is useRenameAgentThread's optimistic upsert: the
// stored root's agent_session.title becomes title (and back to old when
// the write fails). It reports whether the root had a session to change.
func (a *App) SetAgentSessionTitle(ch, ts, title string) bool {
	changed := false
	upd, ok := a.S.Mutate(ch, ts, func(m *slack.Message) {
		if m.AgentSession == nil {
			return
		}
		as := *m.AgentSession
		as.RawTitle, _ = json.Marshal(title)
		m.AgentSession = &as
		changed = true
	})
	if ok && changed {
		a.emitUpdate(ch, &upd)
	}
	return ok && changed
}

// wave4Reads join experimentReads (TestExperimentReadsListed); their
// defaults are the account's groups (TestDefaultExperimentsMatchAccount).
var wave4Reads = map[string][]string{
	"agent_dm_thread_naming": on,
}

// (agent_channel_thread_naming's read and default were already in
// experiments.go and sidebar_rules.go, for the rail peek's Rename session.)
var wave4Defaults = map[string]string{
	"agent_dm_thread_naming": "on",
}

func init() {
	for name, groups := range wave4Reads {
		experimentReads[name] = groups
	}
	for name, g := range wave4Defaults {
		defaultExperimentGroups[name] = g
	}
}
