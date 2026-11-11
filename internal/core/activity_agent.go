package core

// The Activity experiments Slack's client reads (client-boot 5Ns/, build
// 4d0c78085232; docs/ACTIVITY.md "Experiments"), and the agent status an
// Activity row shows (gantry boot-render aEhj useAgentActivityStatus).

import (
	"encoding/json"
	"strconv"
	"time"
)

// ActivityInbox is 5Ns/ Lh isActivityInboxEnabled: every getter below is
// behind it.
func (a *App) ActivityInbox() bool { return a.experiment("activity_inbox") == "on" }

// ActivityLayoutRevisions is 5Ns/ xi (activity_entry_layout_revisions_2):
// the multi-line and single-line rows, the status column, the tabs' dot
// badge, the stacked engagement tooltips.
func (a *App) ActivityLayoutRevisions() bool {
	return a.ActivityInbox() && a.experiment("activity_entry_layout_revisions_2") == "on"
}

// ActivityRemovePersistentDetailPanel is 5Ns/ Bb: the inbox is the full
// width (Primary) and an opened item is a secondary view beside it, not a
// list in the sidebar column with a persistent detail panel. It needs
// nested secondary views (kl02 W7 isNestedSecondaryViewEnabled: off under
// agentic_ia; Slack also turns it off in the BBM mode).
func (a *App) ActivityRemovePersistentDetailPanel() bool {
	return a.ActivityInbox() && a.nestedSecondaryView() && a.experiment("activity_inbox_remove_persistent_detail_panel") == "on"
}

func (a *App) nestedSecondaryView() bool {
	return a.experiment("agentic_ia") != "on" && a.experiment("nested_secondary_view") == "on"
}

// ActivityBotItemTypeReads is 5Ns/ zK (activity_bot_item_type_reads): the
// filter menu's "Exclude automations" row, and live entries from bots
// leave a view that excludes them (sD/F activityUpdatedV2).
func (a *App) ActivityBotItemTypeReads() bool {
	return a.ActivityInbox() && a.experiment("activity_bot_item_type_reads") == "on"
}

// ActivityAutomationsOnly is 5Ns/ LS (activity_automations_only): the
// "Automations only" row, and live entries from people leave a view
// limited to automations.
func (a *App) ActivityAutomationsOnly() bool {
	return a.ActivityInbox() && a.experiment("activity_automations_only") == "on"
}

// ActivityExcludeAutomationsFilter is 5Ns/ UK (exclude_automations_filter):
// the two automation rows become the "Notifications from" select (People,
// apps, and agents / People only / Apps and agents only) and Unreads joins
// the additional filters (vQqF w, lCwf ActivityFilterByMenu).
func (a *App) ActivityExcludeAutomationsFilter() bool {
	return a.ActivityInbox() && a.experiment("exclude_automations_filter") == "on"
}

// AgentStatusInActivity is M/qt ji (agent_status_in_activity). It isn't
// behind activity_inbox.
func (a *App) AgentStatusInActivity() bool { return a.experiment("agent_status_in_activity") == "on" }

// ActivityAgentStatus is useAgentActivityStatus (aEhj Ol) for a row: the
// session's status when it is Processing ("Working": the streaming
// spinner) or Suspended ("Needs attention"), else "". It is only asked
// for channel posts, threads, DMs and app DMs (Gjwd oI), and only when the
// conversation carries an agent_session or code_channel, or the row is a
// thread (j9ba useAgentStatusForSession's enabled). A thread's status is
// its root message's agent_session (Opxd getSessionStatus → j9If
// getAgentSessionStatus), a conversation's its properties' (j9If
// getAgentChannelStatus). Processing lapses at
// date_status_processing_expire (qYb/ useProcessingExpiry), which is the
// returned time (zero when none). Slack's live busy signals (an assistant
// status of typing or banner, a message still streaming) aren't in Clack;
// nor is the Slackbot DM's thread session (agentic IA only).
func (a *App) ActivityAgentStatus(it *ActivityItem, now time.Time) (string, time.Time) {
	if it == nil || !a.AgentStatusInActivity() {
		return "", time.Time{}
	}
	switch it.Type {
	case "channel", "thread_v2", "dm", "bot_dm_bundle":
	default:
		return "", time.Time{}
	}
	thread := it.ThreadTS
	if !a.conversationHasAgentSession(it.Channel) && thread == "" {
		return "", time.Time{}
	}
	status, expire := AgentActive, int64(0)
	if thread != "" {
		var s *agentSessionRaw
		if it.Root != nil && it.Root.TS == thread {
			if as := it.Root.AgentSession; as != nil {
				s = &agentSessionRaw{Status: as.Status, Expire: as.ProcessingExpire}
			}
		} else if m, ok := a.S.Find(it.Channel, thread); ok && m.AgentSession != nil {
			s = &agentSessionRaw{Status: m.AgentSession.Status, Expire: m.AgentSession.ProcessingExpire}
		} else if it.Msg != nil && it.Msg.TS == thread && it.Msg.AgentSession != nil {
			s = &agentSessionRaw{Status: it.Msg.AgentSession.Status, Expire: it.Msg.AgentSession.ProcessingExpire}
		}
		if s != nil && agentStatusKnown(s.Status) {
			status = s.Status
			if status == AgentProcessing {
				expire = s.expire()
			}
		}
	} else if s := a.conversationAgentSession(it.Channel); s != nil {
		switch s.Status {
		case AgentClosed, AgentSuspended, AgentProcessing:
			status = s.Status
		}
		if status == AgentProcessing {
			expire = s.expire()
		}
	}
	// the stale threshold only matters for Active, which shows nothing here
	if status == AgentProcessing && expire > 0 {
		if now.Unix() >= expire {
			return "", time.Time{}
		}
		return status, time.Unix(expire, 0)
	}
	if status == AgentProcessing || status == AgentSuspended {
		return status, time.Time{}
	}
	return "", time.Time{}
}

type agentSessionRaw struct {
	Status string      `json:"status"`
	Expire json.Number `json:"date_status_processing_expire"`
}

// expire is readProcessingExpireSeconds: a positive number, else none.
func (s *agentSessionRaw) expire() int64 {
	if s == nil || s.Expire == "" {
		return 0
	}
	f, err := strconv.ParseFloat(string(s.Expire), 64)
	if err != nil || f <= 0 {
		return 0
	}
	return int64(f)
}

func agentStatusKnown(s string) bool {
	switch s {
	case AgentActive, AgentProcessing, AgentSuspended, AgentClosed, AgentStale:
		return true
	}
	return false
}

// conversationProps is a conversation's properties (V1YJ j), member or
// not.
func (a *App) conversationProps(ch string) json.RawMessage {
	if ch == "" {
		return nil
	}
	c, ok := a.S.Conv(ch)
	if !ok {
		if c, ok = a.S.ExtConv(ch); !ok {
			return nil
		}
	}
	return c.Properties
}

// conversationHasAgentSession is aEhj's `!!(props?.agent_session ??
// props?.code_channel)`.
func (a *App) conversationHasAgentSession(ch string) bool {
	props := a.conversationProps(ch)
	if len(props) == 0 {
		return false
	}
	var p map[string]json.RawMessage
	if json.Unmarshal(props, &p) != nil {
		return false
	}
	return actTruthy(p["agent_session"]) || actTruthy(p["code_channel"])
}

func (a *App) conversationAgentSession(ch string) *agentSessionRaw {
	props := a.conversationProps(ch)
	if len(props) == 0 {
		return nil
	}
	var p struct {
		AgentSession *agentSessionRaw `json:"agent_session"`
	}
	if json.Unmarshal(props, &p) != nil {
		return nil
	}
	return p.AgentSession
}
