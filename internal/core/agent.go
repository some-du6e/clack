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

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
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
}

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
		k := threadKey(s.Channel, s.ThreadTS)
		st.mu.Lock()
		if s.Status == "" {
			delete(st.status, k)
		} else {
			st.status[k] = s
		}
		st.mu.Unlock()
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

// AssistantStatusFor is the app status shown in a thread, if any.
func (a *App) AssistantStatusFor(ch, threadTS string) (AssistantStatus, bool) {
	st := a.assistant()
	st.mu.Lock()
	defer st.mu.Unlock()
	s, ok := st.status[threadKey(ch, threadTS)]
	return s, ok
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
