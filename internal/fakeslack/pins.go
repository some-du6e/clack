package fakeslack

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// pins.add / pins.remove / pins.list over the fake's histories. A pin sets
// the history copy's pinned_to / pinned_info (what conversations.history
// then answers) and pushes pin_added / pin_removed in the gateway's shape:
// channel_id, user, item {type, channel, created, created_by, message},
// pin_count, pinned_info, has_pins, event_ts. Slack's pinAdded and
// pinRemoved (boot-render) read channel_id, item, pin_count, pinned_info
// and event_ts.
//
// Pin and PostQuiet return their events unpushed, so a test can deliver
// them in any order with Push.

type fakePin struct {
	ts, by string
	at     int64
}

func (s *Server) handlePins(method string, f url.Values) (any, bool) {
	switch method {
	case "pins.add", "pins.remove":
		ev, code := s.Pin(SelfID, f.Get("channel"), f.Get("timestamp"), method == "pins.add")
		if code != "" {
			return map[string]any{"ok": false, "error": code}, true
		}
		s.Push(ev)
		return ok(), true
	case "pins.list":
		return s.handlePinsList(f.Get("channel"))
	}
	return nil, false
}

// handlePinsList is pins.list for ch: its pinned messages, newest pin
// first.
func (s *Server) handlePinsList(ch string) (map[string]any, bool) {
	s.histMu.Lock()
	defer s.histMu.Unlock()
	h := s.histLocked(ch)
	items := []map[string]any{}
	ps := s.pins[ch]
	for i := len(ps) - 1; i >= 0; i-- { // newest pin first
		for _, m := range h {
			if m.TS == ps[i].ts {
				items = append(items, map[string]any{"type": "message", "channel": ch, "created": ps[i].at,
					"created_by": ps[i].by, "message": m})
			}
		}
	}
	return ok("items", items), true
}

// Pin pins (on) or unpins message ts of ch as user, as pins.add /
// pins.remove would from any client, and returns the pin_added /
// pin_removed event without pushing it. code is Slack's error
// (message_not_found, already_pinned, no_pin), "" when it applied.
func (s *Server) Pin(user, ch, ts string, on bool) (ev map[string]any, code string) {
	s.histMu.Lock()
	defer s.histMu.Unlock()
	h := s.histLocked(ch)
	idx := -1
	for i := range h {
		if h[i].TS == ts {
			idx = i
		}
	}
	if idx < 0 {
		return nil, "message_not_found"
	}
	pi := -1
	for i, p := range s.pins[ch] {
		if p.ts == ts {
			pi = i
		}
	}
	if on == (pi >= 0) {
		if on {
			return nil, "already_pinned"
		}
		return nil, "no_pin"
	}
	if s.pins == nil {
		s.pins = map[string][]fakePin{}
	}
	now := time.Now()
	// a new slice: historyPage encodes the old one without the lock
	nh := append([]slack.Message(nil), h...)
	m := &nh[idx]
	var info *slack.PinnedInfo
	if on {
		s.pins[ch] = append(s.pins[ch], fakePin{ts: ts, by: user, at: now.Unix()})
		info = &slack.PinnedInfo{Channel: ch, PinnedBy: user, PinnedTS: now.Unix()}
		m.PinnedTo, m.PinnedInfo = []string{ch}, info
	} else {
		s.pins[ch] = append(s.pins[ch][:pi:pi], s.pins[ch][pi+1:]...)
		m.PinnedTo, m.PinnedInfo = nil, nil
	}
	s.hist[ch] = nh
	typ := "pin_removed"
	if on {
		typ = "pin_added"
	}
	ev = map[string]any{"type": typ, "user": user, "channel_id": ch, "has_pins": len(s.pins[ch]) > 0,
		"pin_count": len(s.pins[ch]), "event_ts": fmt.Sprintf("%d.%06d", now.Unix(), now.Nanosecond()/1000),
		"item": map[string]any{"type": "message", "channel": ch, "created": now.Unix(), "created_by": user, "message": *m}}
	if info != nil {
		ev["pinned_info"] = info
	}
	return ev, ""
}

// PostBlocksQuiet is PostQuiet for a message with Block Kit blocks (a
// rich_text message: channel-tab slugs and the like).
func (s *Server) PostBlocksQuiet(ch, user, text string, blocks json.RawMessage) string {
	ts, _ := s.PostQuiet(ch, user, text)
	s.histMu.Lock()
	defer s.histMu.Unlock()
	if h := s.hist[ch]; len(h) > 0 && h[0].TS == ts {
		h[0].Blocks = blocks
	}
	return ts
}

// PostQuiet adds a message from user to ch's history (newest) and returns
// its ts and its realtime message event, unpushed.
func (s *Server) PostQuiet(ch, user, text string) (ts string, ev map[string]any) {
	s.histMu.Lock()
	defer s.histMu.Unlock()
	h := s.histLocked(ch)
	now := time.Now()
	sec, us := now.Unix(), int64(now.Nanosecond()/1000)
	if len(h) > 0 { // after the newest, whatever the clock says
		if a, b, ok := strings.Cut(h[0].TS, "."); ok {
			hs, _ := strconv.ParseInt(a, 10, 64)
			hu, _ := strconv.ParseInt(b, 10, 64)
			if hs > sec || hs == sec && hu >= us {
				sec, us = hs+1, 0
			}
		}
	}
	ts = fmt.Sprintf("%d.%06d", sec, us)
	m := slack.Message{Type: "message", TS: ts, User: user, Text: text, ClientMsgID: "quiet-" + ts}
	s.hist[ch] = append([]slack.Message{m}, h...)
	return ts, map[string]any{"type": "message", "channel": ch, "user": user, "text": text, "ts": ts, "event_ts": ts,
		"client_msg_id": m.ClientMsgID}
}

// PostReplyQuiet adds a reply from user to the thread under root (a
// message of ch's history, which gets the reply metadata) and returns its
// ts. conversations.replies then serves the root and its replies.
func (s *Server) PostReplyQuiet(ch, root, user, text string) string {
	s.histMu.Lock()
	defer s.histMu.Unlock()
	h := s.histLocked(ch)
	k := ch + "/" + root
	sec := time.Now().Unix()
	if a, _, ok := strings.Cut(root, "."); ok {
		if rs, _ := strconv.ParseInt(a, 10, 64); rs >= sec {
			sec = rs + 1
		}
	}
	n := len(s.quietReplies[k])
	ts := fmt.Sprintf("%d.%06d", sec+int64(n), 100)
	m := slack.Message{Type: "message", TS: ts, User: user, Text: text, ThreadTS: root, ClientMsgID: "quiet-" + ts}
	if s.quietReplies == nil {
		s.quietReplies = map[string][]slack.Message{}
	}
	s.quietReplies[k] = append(s.quietReplies[k], m)
	for i := range h {
		if h[i].TS == root {
			h[i].ThreadTS = root
			h[i].ReplyCount = n + 1
			h[i].LatestReply = ts
			if !slices.Contains(h[i].ReplyUsers, user) {
				h[i].ReplyUsers = append(h[i].ReplyUsers, user)
			}
		}
	}
	return ts
}

// quietThread is conversations.replies for a thread PostReplyQuiet made.
func (s *Server) quietThread(ch, root string) ([]slack.Message, bool) {
	s.histMu.Lock()
	defer s.histMu.Unlock()
	rs, ok := s.quietReplies[ch+"/"+root]
	if !ok {
		return nil, false
	}
	for _, m := range s.hist[ch] {
		if m.TS == root {
			return append([]slack.Message{m}, rs...), true
		}
	}
	return rs, true
}
