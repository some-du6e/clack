package fakeslack

import (
	"encoding/json"
	"net/url"

	"github.com/some-du6e/clack/internal/slack"
)

// messagesByID is messages.list for Size.MessagesByID: each message asked
// for that the fake has (a channel's history, a quiet thread's replies),
// by ts; one it doesn't have is left out of its channel's list, which
// Slack's answer means it doesn't exist.
func (s *Server) messagesByID(f url.Values) (any, bool) {
	var want []struct {
		Channel    string   `json:"channel"`
		Timestamps []string `json:"timestamps"`
	}
	if json.Unmarshal([]byte(f.Get("message_ids")), &want) != nil {
		return nil, false
	}
	out := map[string][]slack.Message{}
	for _, w := range want {
		for _, ts := range w.Timestamps {
			if m, ok := s.messageByTS(w.Channel, ts); ok {
				out[w.Channel] = append(out[w.Channel], m)
			}
		}
	}
	return ok("messages", out), true
}
