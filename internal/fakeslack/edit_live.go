package fakeslack

import (
	"encoding/json"

	"github.com/some-du6e/clack/internal/slack"
)

// EditLive is chat.update of ch/ts by its author, as the server tells the
// clients: the message's text becomes text in the fake's history, and the
// realtime message_changed goes out with the message whole and the
// previous_message, as Slack sends them (Activity's messageChanged reads
// both: an edit dropping the viewer's mention removes their entry). It
// reports whether the fake holds ts.
func (s *Server) EditLive(ch, ts, text string) bool {
	s.histMu.Lock()
	var prev, m slack.Message
	found := false
	h := s.hist[ch]
	for i := range h {
		if h[i].TS == ts {
			prev = h[i]
			h[i].Text = text
			h[i].Edited = &slack.Edited{User: h[i].User, TS: uptimeEditTS(ts)}
			m, found = h[i], true
			break
		}
	}
	s.histMu.Unlock()
	if !found {
		return false
	}
	asMap := func(x slack.Message) map[string]any {
		raw, _ := json.Marshal(x)
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		out["channel"] = ch
		return out
	}
	ets := uptimeEditTS(ts)
	s.Push(map[string]any{"type": "message", "subtype": "message_changed", "channel": ch, "hidden": true,
		"ts": ets, "event_ts": ets, "message": asMap(m), "previous_message": asMap(prev)})
	return true
}
