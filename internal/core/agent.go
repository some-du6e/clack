package core

// Slack's AI-app / agent realtime surface (2025–2026), read from the web
// client (gantry-v2-async-client-boot-render, 2026-11-08):
//
//   - Streaming: an app's chat.startStream posts a message with
//     streaming_state "in_progress"; chat.appendStream and stopStream
//     arrive as ordinary message_changed events (more text, new plan /
//     task_card states) until streaming_state is "completed" or
//     "errored". Message.StreamingState carries it; Slack holds the
//     notification of an in-progress message until it settles
//     (defer_notification_3p_streaming).
//   - ai_assistant_status (assistant.threads.setStatus): {channel_id,
//     thread_ts, bot_user_id, status, status_type, loading_messages,
//     username, icon_emoji, icon_url}. A status shows as the thread's
//     typing line, "<b>name</b> status" (AiAppStatusIndicator); an empty
//     status clears it, as do the app's next message and two minutes
//     without one.
//   - ai_assistant_threads_suggested_prompts (setSuggestedPrompts):
//     {channel_id, thread_ts, title, prompts: [{title, message}]}.

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Streaming reports an open chat stream (Slack's isMessageStreaming).
func (m *Message) Streaming() bool { return m.StreamingState == "in_progress" }

// AssistantStatus is an app's status line in an assistant thread.
type AssistantStatus struct {
	Channel         string   `json:"channel_id"`
	ThreadTS        string   `json:"thread_ts"`
	BotUserID       string   `json:"bot_user_id"`
	Status          string   `json:"status"`      // "is thinking…"; "" clears
	StatusType      string   `json:"status_type"` // typing (the thread's typing line) | banner
	LoadingMessages []string `json:"loading_messages"`
	Username        string   `json:"username"`
	IconEmoji       string   `json:"icon_emoji"`
	IconURL         string   `json:"icon_url"`
	Name            string   `json:"-"` // username, else the bot user's name

	at time.Time // when it was set (assistantStatusTTL)
}

// assistantStatusTTL is how long an app status lasts without a message
// (assistant.threads.setStatus: two minutes; the composer's typing line
// keeps the same, ui/composer/typing.go).
const assistantStatusTTL = 2 * time.Minute

// SuggestedPrompt is one assistant suggested prompt.
type SuggestedPrompt struct {
	Title   string `json:"title"`
	Message string `json:"message"`
}

// SuggestedPrompts is an assistant thread's prompt list.
type SuggestedPrompts struct {
	Channel   string            `json:"channel_id"`
	ThreadTS  string            `json:"thread_ts"`
	BotUserID string            `json:"bot_user_id"`
	Title     string            `json:"title"`
	Prompts   []SuggestedPrompt `json:"prompts"`
}

type assistantState struct {
	mu      sync.Mutex
	status  map[string]AssistantStatus
	prompts map[string]SuggestedPrompts
}

var assistants sync.Map // *App → *assistantState

func (a *App) assistant() *assistantState {
	if v, ok := assistants.Load(a); ok {
		return v.(*assistantState)
	}
	v, _ := assistants.LoadOrStore(a, &assistantState{status: map[string]AssistantStatus{}, prompts: map[string]SuggestedPrompts{}})
	return v.(*assistantState)
}

func threadKey(ch, ts string) string { return ch + "-" + ts }

// onAssistant handles ai_assistant_status and
// ai_assistant_threads_suggested_prompts (events.go).
func (a *App) onAssistant(ctx context.Context, ev slack.Event) {
	st := a.assistant()
	switch ev.Type {
	case "ai_assistant_status":
		var s AssistantStatus
		if json.Unmarshal(ev.Raw, &s) != nil || s.BotUserID == "" {
			return
		}
		if s.Username == "" {
			a.EnsureUsers(ctx, []string{s.BotUserID})
		}
		s.Name = firstNonEmpty(s.Username, a.S.UserName(s.BotUserID))
		s.at = time.Now()
		k := threadKey(s.Channel, s.ThreadTS)
		st.mu.Lock()
		_, had := st.status[k]
		if s.Status == "" {
			delete(st.status, k)
		} else {
			st.status[k] = s
		}
		st.mu.Unlock()
		if s.Status == "" && !had {
			return // already ended (a message in the thread cleared it)
		}
		raw, _ := json.Marshal(s)
		a.emit(Event{Type: "assistant_status", Channel: s.Channel, ThreadTS: s.ThreadTS, UserID: s.BotUserID, Name: s.Name, Raw: raw})
	case "ai_assistant_threads_suggested_prompts":
		var p SuggestedPrompts
		if json.Unmarshal(ev.Raw, &p) != nil || p.Channel == "" {
			return
		}
		st.mu.Lock()
		st.prompts[threadKey(p.Channel, p.ThreadTS)] = p
		st.mu.Unlock()
		raw, _ := json.Marshal(p)
		a.emit(Event{Type: "suggested_prompts", Channel: p.Channel, ThreadTS: p.ThreadTS, UserID: p.BotUserID, Raw: raw})
	}
}

// AssistantStatusOf decodes an "assistant_status" event.
func AssistantStatusOf(ev Event) (AssistantStatus, bool) {
	var s AssistantStatus
	if ev.Type != "assistant_status" || json.Unmarshal(ev.Raw, &s) != nil {
		return s, false
	}
	s.Name = ev.Name
	return s, true
}

// AssistantStatusFor is the app status shown in a thread, if any (set
// less than two minutes ago and not ended by a message since).
func (a *App) AssistantStatusFor(ch, threadTS string) (AssistantStatus, bool) {
	st := a.assistant()
	st.mu.Lock()
	defer st.mu.Unlock()
	s, ok := st.status[threadKey(ch, threadTS)]
	if ok && !s.at.IsZero() && time.Since(s.at) > assistantStatusTTL {
		return AssistantStatus{}, false
	}
	return s, ok
}

// clearAssistantStatusOnMessage is maybeClearAssistantStatusOnNewMessage
// (boot-render zuQH): a new message in a thread ends its app's status.
// (Under agent_ui_loading_state Slack keeps an optimistic loading status
// the viewer's own pending message sets; Clack sets none.)
func (a *App) clearAssistantStatusOnMessage(ch, threadTS string) {
	st := a.assistant()
	k := threadKey(ch, threadTS)
	st.mu.Lock()
	s, ok := st.status[k]
	if ok {
		delete(st.status, k)
	}
	st.mu.Unlock()
	if !ok {
		return
	}
	s.Status = ""
	raw, _ := json.Marshal(s)
	a.emit(Event{Type: "assistant_status", Channel: ch, ThreadTS: threadTS, UserID: s.BotUserID, Name: s.Name, Raw: raw})
}

// AppNugget is what the thread footer's AppThreadNugget (boot-shared 352G)
// shows for an app's assistant status (G2VO.ZL): the app (username, else
// the bot's real name, else "Agent"), its icon and its loading messages.
// Chip is app_nugget_refinement: the chip reading "{app} is working";
// without it, the shimmering row reading "{app} is working…".
type AppNugget struct {
	BotUserID, Name, IconEmoji, IconURL string
	LoadingMessages                     []string
	Chip                                bool
}

// AppThreadNugget reports the nugget for ch/ts: app_thread_nugget_fe on,
// an app status in the thread, and not a Slackbot agent thread
// (isSlackbotAgentThread). Slack also lists processing agent sessions
// (agent_session.agent_statuses on the root, with Stop); Clack keeps no
// agent_session on messages, so it shows the status nugget only.
func (a *App) AppThreadNugget(ch, ts string) (AppNugget, bool) {
	if a.experiment("app_thread_nugget_fe") != "on" {
		return AppNugget{}, false
	}
	s, ok := a.AssistantStatusFor(ch, ts)
	if !ok || s.Status == "" || s.BotUserID == "USLACKBOT" {
		return AppNugget{}, false
	}
	name := s.Username
	if name == "" {
		if u, ok := a.S.User(s.BotUserID); ok {
			name = firstNonEmpty(u.Profile.RealName, u.RealName)
		}
	}
	if name == "" {
		name = "Agent" // useFallbackAgentName
	}
	return AppNugget{BotUserID: s.BotUserID, Name: name, IconEmoji: s.IconEmoji, IconURL: s.IconURL,
		LoadingMessages: s.LoadingMessages, Chip: a.experiment("app_nugget_refinement") == "on"}, true
}

// SuppressBotTyping is G2VO.Ss, the thread composer's suppressBotTyping:
// while the nugget shows an app's status, the typing line leaves that app
// out (no AiAppStatusIndicator).
func (a *App) SuppressBotTyping(ch, ts string) (botUserID string, ok bool) {
	n, ok := a.AppThreadNugget(ch, ts)
	if !ok {
		return "", false
	}
	return n.BotUserID, true
}

// SuggestedPromptsFor is the thread's (or, with threadTS "", the
// channel's latest) suggested prompts.
func (a *App) SuggestedPromptsFor(ch, threadTS string) (SuggestedPrompts, bool) {
	st := a.assistant()
	st.mu.Lock()
	defer st.mu.Unlock()
	p, ok := st.prompts[threadKey(ch, threadTS)]
	return p, ok && len(p.Prompts) > 0
}

// agentMemberChannelCountOn is agent_member_channel_count (client-boot
// M/qt.lx): Slack's channel header avatar stack, About modal and member
// filters count a channel's people only, its apps and agents counted on
// the Agents & apps tab instead.
func (a *App) agentMemberChannelCountOn() bool {
	return a.experiment("agent_member_channel_count") == "on"
}

// channelMemberCount is the member count Slack shows for a channel from
// its membership counts (fetchAndUpsertMembershipCountsForChannel maps
// edge users/counts: member_count = people, app_count = apps). With
// agent_member_channel_count on it is RW6b.vC, member_count; otherwise
// RW6b.iz, member_count + app_count (plus one for an active native
// channel expert, which needs enable_native_channel_expert and the
// channel's channel_agent_status; Clack keeps neither, so it never adds
// it).
func channelMemberCount(people, apps int, peopleOnly bool) int {
	if peopleOnly {
		return people
	}
	return people + apps
}
