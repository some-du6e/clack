package taut

// The huddle the user is in, for api.huddles (onActiveHuddle, onPeerMessage,
// getAttendeeUserId, sendReaction) and Slack's peers / huddleEvents / alert
// slices. internal/ui/huddleui feeds it from the native huddle session:
// the event log (SetHuddleLog) is the session's, so the enableEventLog and
// disableEventLog actions and the transcription fetchers reach the session
// through HuddleHooks.

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
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
	log      HuddleLog
	extra    []map[string]any // events plugins pushed (pushNonTranscriptionEvent from JS)
	pushed   int              // how many of log's non-transcription events the action stream has seen
	alerts   []map[string]any // Slack's alert.alerts
	hooks    HuddleHooks
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
		h.peers, h.left, h.log, h.extra, h.pushed, h.alerts = map[string]string{}, map[string]string{}, HuddleLog{}, nil, 0, nil
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
	t.mirror.invalidate("alert")
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

// HuddleLog is the active huddle's event log as Slack's huddleEvents
// slice keeps it (client-boot 4xZR): on while captions are (or a plugin
// keeps it open), its events oldest first: START_TRANSCRIPTION,
// TRANSCRIBE, and the non-transcription JOIN, LEAVE, SCREENSHARE_START,
// SCREENSHARE_STOP and SEND_REACTION, each {type, memberId, timestampMs,
// …} as Slack stores it.
type HuddleLog struct {
	Enabled, Loading           bool
	SideBySide, ClosedCaptions bool // the captions views you turned on
	Events                     []map[string]any
}

// HuddleHooks are the native huddle's side of Slack's event log actions
// and fetchers, set by internal/ui/huddleui while you're in a huddle.
type HuddleHooks struct {
	// EventLog is the enableEventLog (true) / disableEventLog action: the
	// log on or off, its events dropped, no transcription change.
	EventLog func(on bool)
	// Transcription is roomsStartTranscriptionFetcher (true) /
	// roomsStopTranscriptionFetcher for callID.
	Transcription func(ctx context.Context, callID string, on bool) error
}

// SetHuddleHooks installs the native huddle's event log hooks.
func (t *Taut) SetHuddleHooks(hk HuddleHooks) {
	h := t.huddle()
	h.mu.Lock()
	h.hooks = hk
	h.mu.Unlock()
}

// SetHuddleLog replaces the event log with the session's. Events that
// are new non-transcription pushes reach the store's action stream as
// Slack's pushNonTranscriptionEvent (in order); a log turned on or off
// starts the count again.
func (t *Taut) SetHuddleLog(l HuddleLog) {
	h := t.huddle()
	h.mu.Lock()
	if h.callID == "" {
		h.mu.Unlock()
		return
	}
	if l.Enabled != h.log.Enabled {
		h.pushed, h.extra = 0, nil
	}
	same := l.Enabled == h.log.Enabled && l.Loading == h.log.Loading && l.SideBySide == h.log.SideBySide &&
		l.ClosedCaptions == h.log.ClosedCaptions && reflect.DeepEqual(l.Events, h.log.Events)
	h.log = l
	var push []map[string]any
	n := 0
	for _, ev := range l.Events {
		if typ, _ := ev["type"].(string); typ == "TRANSCRIBE" || typ == "START_TRANSCRIPTION" {
			continue
		}
		if n >= h.pushed {
			push = append(push, ev)
		}
		n++
	}
	h.pushed = n
	h.mu.Unlock()
	if same {
		return
	}
	t.mu.Lock()
	rt := t.rt
	t.mu.Unlock()
	if rt == nil {
		return
	}
	t.mirror.invalidate("huddleEvents")
	for _, ev := range push {
		rt.post("huddleAction", map[string]any{"type": "PUSH_NON_TRANSCRIPTION_EVENT", "payload": ev})
	}
}

// SetHuddleAlerts is Slack's alert.alerts for the huddle (makeAlertObject's
// {priority, type, id, …}; the peerMuteRequest alert's id is the
// attendee who muted you).
func (t *Taut) SetHuddleAlerts(alerts []map[string]any) {
	h := t.huddle()
	h.mu.Lock()
	h.alerts = alerts
	h.mu.Unlock()
	if t.Started() {
		t.mirror.invalidate("alert")
	}
}

// huddleEventLog is the enableEventLog / disableEventLog actions: the
// reducer's state at once (so the dispatching plugin reads it back), and
// the session's log through the hook.
func (t *Taut) huddleEventLog(a []json.RawMessage) (any, error) {
	var on bool
	if len(a) > 0 {
		_ = json.Unmarshal(a[0], &on)
	}
	h := t.huddle()
	h.mu.Lock()
	active := h.callID != ""
	if active {
		// the reducer keeps nothing else: enableEventLog / disableEventLog
		// return the initial state with enabled set
		h.log, h.extra, h.pushed = HuddleLog{Enabled: on}, nil, 0
	}
	hook := h.hooks.EventLog
	h.mu.Unlock()
	if active && hook != nil {
		hook(on) // the session's log changes now (its OnChange runs later, off this call)
	}
	if t.Started() {
		t.mirror.invalidate("huddleEvents")
	}
	return nil, nil
}

// huddlePushEvent is pushNonTranscriptionEvent dispatched by a plugin: the
// event joins the log while it's on.
func (t *Taut) huddlePushEvent(a []json.RawMessage) (any, error) {
	var ev map[string]any
	if len(a) > 0 {
		_ = json.Unmarshal(a[0], &ev)
	}
	if ev == nil {
		return nil, nil
	}
	h := t.huddle()
	h.mu.Lock()
	ok := h.callID != "" && h.log.Enabled
	if ok {
		h.extra = append(h.extra, ev)
	}
	h.mu.Unlock()
	if ok && t.Started() {
		t.mirror.invalidate("huddleEvents")
	}
	return nil, nil
}

// huddleTranscription runs roomsStartTranscriptionFetcher (on) or
// roomsStopTranscriptionFetcher ({callId, reason}).
func (t *Taut) huddleTranscription(ctx context.Context, a []json.RawMessage, on bool) (any, error) {
	callID := argObj(a).str("callId")
	h := t.huddle()
	h.mu.Lock()
	hook := h.hooks.Transcription
	if callID == "" {
		callID = h.callID
	}
	active := h.callID != "" && callID == h.callID
	h.mu.Unlock()
	if !active || hook == nil {
		return nil, errors.New("no active huddle")
	}
	return nil, hook(ctx, callID, on)
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

// huddleEventsSlice is Slack's huddleEvents state for the active huddle.
func (t *Taut) huddleEventsSlice() map[string]any {
	h := t.huddle()
	h.mu.Lock()
	defer h.mu.Unlock()
	l := h.log
	events := make([]any, 0, len(l.Events)+len(h.extra))
	if l.Enabled {
		for _, ev := range l.Events {
			events = append(events, ev)
		}
		for _, ev := range h.extra {
			events = append(events, ev)
		}
		if len(h.extra) > 0 {
			sort.SliceStable(events, func(i, j int) bool { return eventMs(events[i]) < eventMs(events[j]) })
		}
	}
	return map[string]any{"enabled": l.Enabled, "isLoading": l.Loading, "events": events,
		"isSideBySideEnabled": l.SideBySide, "isClosedCaptionsEnabled": l.ClosedCaptions}
}

func eventMs(ev any) float64 {
	m, _ := ev.(map[string]any)
	switch v := m["timestampMs"].(type) {
	case float64:
		return v
	case int64:
		return float64(v)
	case int:
		return float64(v)
	}
	return 0
}

// alertSlice is Slack's alert state.
func (t *Taut) alertSlice() map[string]any {
	h := t.huddle()
	h.mu.Lock()
	defer h.mu.Unlock()
	alerts := make([]any, 0, len(h.alerts))
	for _, a := range h.alerts {
		alerts = append(alerts, a)
	}
	return map[string]any{"alerts": alerts}
}

// HuddleEventsView is the huddleEvents slice as plugins present it
// (HuddleActivity's patchState adds its lines and forces the side-by-side
// view on): the raw slice until a plugin patches the state, then the
// patched one (non-blocking; ok=false until it has been read).
type HuddleEventsView struct {
	Enabled                 bool              `json:"enabled"`
	IsLoading               bool              `json:"isLoading"`
	IsSideBySideEnabled     bool              `json:"isSideBySideEnabled"`
	IsClosedCaptionsEnabled bool              `json:"isClosedCaptionsEnabled"`
	Events                  []json.RawMessage `json:"events"`
}

// HuddleEvents returns HuddleEventsView.
func (t *Taut) HuddleEvents() (HuddleEventsView, bool) {
	var raw json.RawMessage
	if t.SlicePatched("huddleEvents") {
		r, ok := t.ui.views.get("huddleEvents", "", "")
		if !ok || len(r) == 0 || string(r) == "null" {
			return HuddleEventsView{}, false
		}
		raw = r
	} else {
		b, err := json.Marshal(t.huddleEventsSlice())
		if err != nil {
			return HuddleEventsView{}, false
		}
		raw = b
	}
	var v HuddleEventsView
	if json.Unmarshal(raw, &v) != nil {
		return HuddleEventsView{}, false
	}
	return v, true
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
