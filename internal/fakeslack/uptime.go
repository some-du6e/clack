package fakeslack

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// An uptime monitor's message (Karim's, 2026-11-18): the plans app posts a
// message of one plan block whose title is the outage and its duration
// ("10router blipped · 2m 1s") and edits it with chat.update as the
// duration ticks. Every edit keeps the block's id and changes the title.
// Size.AppPlans has the app and #plans (AppPlanChannelID).

// uptimeBlocks is the monitor's blocks for title.
func uptimeBlocks(title string) json.RawMessage {
	b, _ := json.Marshal([]any{map[string]any{"type": "plan", "block_id": "uptime_10router_0001", "title": title, "tasks": []any{
		agentTask("m22", "10router is down", "complete", map[string]any{"details": agentRich("Checked from 3 regions.")}),
	}}})
	return b
}

// PostUptimeQuiet adds the monitor's message titled title to #plans'
// history (newest) and returns its ts, unpushed.
func (s *Server) PostUptimeQuiet(title string) string {
	ts, _ := s.PostQuiet(AppPlanChannelID, AppPlanUserID, title)
	s.histMu.Lock()
	defer s.histMu.Unlock()
	if h := s.hist[AppPlanChannelID]; len(h) > 0 && h[0].TS == ts {
		h[0].ClientMsgID = ""
		h[0].BotID, h[0].AppID = AppPlanBotID, AppPlanAppID
		bp := appPlanBot()
		h[0].BotProfile = &bp
		h[0].Blocks = uptimeBlocks(title)
	}
	return ts
}

// EditUptime is the monitor's chat.update of ts: its plan's title (and
// text) becomes title in #plans' history, and the realtime
// message_changed goes out with the message whole, its thread's metadata
// too, as Slack sends a thread root's. It reports whether ts was found.
func (s *Server) EditUptime(ts, title string) bool {
	s.histMu.Lock()
	var m slack.Message
	found := false
	h := s.hist[AppPlanChannelID]
	for i := range h {
		if h[i].TS == ts {
			h[i].Text, h[i].Blocks = title, uptimeBlocks(title)
			h[i].Edited = &slack.Edited{User: AppPlanUserID, TS: uptimeEditTS(ts)}
			m, found = h[i], true
			break
		}
	}
	s.histMu.Unlock()
	if !found {
		return false
	}
	raw, _ := json.Marshal(m)
	var msg map[string]any
	_ = json.Unmarshal(raw, &msg)
	msg["channel"] = AppPlanChannelID
	ets := uptimeEditTS(ts)
	s.Push(map[string]any{"type": "message", "subtype": "message_changed", "channel": AppPlanChannelID, "hidden": true,
		"ts": ets, "event_ts": ets, "message": msg})
	return true
}

// uptimeEditTS is an edit's event ts: now, after the message's own.
func uptimeEditTS(ts string) string {
	sec, us := time.Now().Unix(), time.Now().Nanosecond()/1000
	if a, _, ok := strings.Cut(ts, "."); ok {
		if ms, _ := strconv.ParseInt(a, 10, 64); ms >= sec {
			sec = ms + 1
		}
	}
	return fmt.Sprintf("%d.%06d", sec, us)
}
