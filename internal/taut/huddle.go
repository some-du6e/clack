package taut

// The huddle the user is in, for api.huddles (onActiveHuddle, onPeerMessage,
// getAttendeeUserId, sendReaction) and Slack's peers / huddleEvents slices.
// internal/ui/huddleui feeds it from the native huddle session.

import (
	"encoding/json"
	"sync"
	"time"
)

type huddleState struct {
	mu       sync.Mutex
	callID   string
	channel  string
	self     string            // own attendee id
	selfUser string            // own user id
	peers    map[string]string // attendee -> user (present)
	left     map[string]string // attendee -> user (left)
	logOn    bool
	react    func(emoji, kind, url string) bool

	// clientHuddles: the most recent huddle and when you joined it (ms)
	lastCall, lastChannel string
	startedAt             int64
	ended                 bool
}

var huddles sync.Map // *Taut -> *huddleState

func (t *Taut) huddle() *huddleState {
	if v, ok := huddles.Load(t); ok {
		return v.(*huddleState)
	}
	v, _ := huddles.LoadOrStore(t, &huddleState{peers: map[string]string{}, left: map[string]string{}})
	return v.(*huddleState)
}

// SetHuddleReaction installs what api.huddles.sendReaction (Slack's
// sendReactionMessage) does: send emoji (kind standard|effect|gif, url for
// a custom image) into the active huddle; false when there is none.
func (t *Taut) SetHuddleReaction(f func(emoji, kind, url string) bool) {
	h := t.huddle()
	h.mu.Lock()
	h.react = f
	h.mu.Unlock()
}

// SetActiveHuddle tells plugins which huddle you're in or joining
// (api.huddles.onActiveHuddle); callID "" means none.
func (t *Taut) SetActiveHuddle(callID, channelID, selfAttendee, selfUser string) {
	h := t.huddle()
	h.mu.Lock()
	if h.callID == callID && h.channel == channelID && h.self == selfAttendee {
		h.mu.Unlock()
		return
	}
	if callID != "" && callID != h.callID {
		h.lastCall, h.lastChannel, h.startedAt, h.ended = callID, channelID, time.Now().UnixMilli(), false
	} else if callID == "" && h.callID != "" {
		h.ended = true
	} else if callID != "" && channelID != "" {
		h.lastChannel = channelID
	}
	h.callID, h.channel, h.self, h.selfUser = callID, channelID, selfAttendee, selfUser
	if callID == "" {
		h.peers, h.left, h.logOn = map[string]string{}, map[string]string{}, false
	}
	h.mu.Unlock()
	t.mu.Lock()
	rt := t.rt
	t.mu.Unlock()
	if rt == nil {
		return
	}
	t.mirror.invalidate("peers")
	t.mirror.invalidate("huddleEvents")
	t.mirror.invalidate("clientHuddles")
	if callID == "" {
		rt.post("activeHuddle", nil)
		return
	}
	a := map[string]string{"callId": callID}
	if channelID != "" {
		a["channelId"] = channelID
	}
	rt.post("activeHuddle", a)
}

// SetHuddlePeers updates the attendee → user map of the active huddle
// (Slack's peers slice, behind api.huddles.getAttendeeUserId). Attendees
// missing from present move to leftById.
func (t *Taut) SetHuddlePeers(present map[string]string) {
	h := t.huddle()
	h.mu.Lock()
	changed := len(present) != len(h.peers)
	for att, u := range h.peers {
		if present[att] != u {
			changed = true
		}
		if _, ok := present[att]; !ok {
			h.left[att] = u
		}
	}
	h.peers = make(map[string]string, len(present))
	for att, u := range present {
		h.peers[att] = u
		delete(h.left, att)
	}
	h.mu.Unlock()
	if changed && t.Started() {
		t.mirror.invalidate("peers")
	}
}

// HuddlePeerMessage delivers a decoded peer message to
// api.huddles.onPeerMessage listeners. ev marshals to
// {message, kind, senderAttendeeId, selfAttendeeId} (huddle.PeerEvent).
func (t *Taut) HuddlePeerMessage(ev any) {
	t.mu.Lock()
	rt := t.rt
	t.mu.Unlock()
	if rt != nil {
		rt.post("peerMessage", ev)
	}
}

func (t *Taut) huddleReaction(a []json.RawMessage) (any, error) {
	var p struct {
		Emoji        string `json:"emoji"`
		ReactionType string `json:"reactionType"`
		EmojiURL     string `json:"emojiUrl"`
	}
	if len(a) > 0 {
		_ = json.Unmarshal(a[0], &p)
	}
	h := t.huddle()
	h.mu.Lock()
	f, active := h.react, h.callID != ""
	h.mu.Unlock()
	if f == nil || !active || p.Emoji == "" {
		return false, nil
	}
	return f(p.Emoji, p.ReactionType, p.EmojiURL), nil
}

func (t *Taut) huddleEventLog(a []json.RawMessage) (any, error) {
	var on bool
	if len(a) > 0 {
		_ = json.Unmarshal(a[0], &on)
	}
	h := t.huddle()
	h.mu.Lock()
	h.logOn = on && h.callID != ""
	h.mu.Unlock()
	if t.Started() {
		t.mirror.invalidate("huddleEvents")
	}
	return nil, nil
}

// peersSlice is Slack's peers state for the active huddle.
func (t *Taut) peersSlice() map[string]any {
	h := t.huddle()
	h.mu.Lock()
	defer h.mu.Unlock()
	by := map[string]any{}
	for att, u := range h.peers {
		if att != h.self {
			by[att] = map[string]any{"id": att, "userId": u}
		}
	}
	left := map[string]any{}
	for att, u := range h.left {
		left[att] = map[string]any{"id": att, "userId": u}
	}
	self := map[string]any{"id": "self"}
	if h.self != "" {
		self = map[string]any{"id": h.self, "userId": h.selfUser}
	}
	return map[string]any{"self": self, "invitedUsers": map[string]any{}, "byId": by, "leftById": left, "reacjis": []any{}}
}

func (t *Taut) huddleEventsSlice() map[string]any {
	h := t.huddle()
	h.mu.Lock()
	on := h.logOn
	h.mu.Unlock()
	return map[string]any{"enabled": on, "isLoading": false, "events": []any{}, "isSideBySideEnabled": false, "isClosedCaptionsEnabled": false}
}

// clientHuddlesSlice is Slack's clientHuddles state: the most recent huddle
// (huddleState STARTED while you're in it, ENDED after) and lastStartTs,
// when you joined it in ms (CallTimer's elapsed time).
func (t *Taut) clientHuddlesSlice() map[string]any {
	h := t.huddle()
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]any{"lastStartTs": h.startedAt, "mostRecentHuddle": nil}
	if h.lastCall == "" {
		return out
	}
	state := "STARTED"
	if h.ended {
		state = "ENDED"
	}
	recent := map[string]any{"callId": h.lastCall, "huddleState": state}
	if h.lastChannel != "" {
		recent["channelId"] = h.lastChannel
	}
	out["mostRecentHuddle"] = recent
	return out
}
