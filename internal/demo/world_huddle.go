package demo

import (
	"encoding/json"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// A huddle that's running in #party-planning-crew when the demo starts:
// the committee picking a venue. Its huddle_thread message sits in the
// channel ("A huddle started", Join Huddle with the faces in it), edge
// huddles/list lists it (the sidebar's headphones, the channel header,
// the Huddles page), and now and then someone joins or leaves it
// (sh_room_join / sh_room_leave, as Slack's server sends them). The demo
// has no audio: joining it is the same "no audio" as a ring's.

const (
	LiveHuddleChannel = PartyID
	LiveHuddleID      = "R0DEMOLIVE1"
	liveHuddleName    = "Venue shortlist (bring snacks)"
)

// liveHuddle is the running huddle's state (mu held).
type liveHuddle struct {
	ts           string // its huddle_thread message
	start        int64
	creator      string
	participants []string
	history      []string
	// bench is who may drop in later
	bench []string
}

// seedHuddleLocked starts the huddle 18 minutes before the demo does and
// puts its message in the channel.
func (w *World) seedHuddleLocked() {
	ids := func(hs ...string) []string {
		var out []string
		for _, h := range hs {
			if p, ok := w.person(h); ok {
				out = append(out, p.ID)
			}
		}
		return out
	}
	start := w.now().Add(-18 * time.Minute)
	h := &liveHuddle{start: start.Unix(), creator: ids("marisol")[0], participants: ids("marisol", "bev", "ruth"),
		history: ids("marisol", "bev", "ruth", "kip"), bench: ids("kip", "gordon")}
	h.ts = microTS(start.UnixMicro())
	m := slack.Message{Type: "message", Subtype: "huddle_thread", User: h.creator, TS: h.ts, Text: "A huddle started",
		Room: w.roomJSONLocked(h)}
	w.hist[LiveHuddleChannel] = append([]slack.Message{m}, w.hist[LiveHuddleChannel]...)
	sortNewestFirst(w.hist[LiveHuddleChannel])
	w.live = h
}

func (w *World) roomLocked(h *liveHuddle) map[string]any {
	return map[string]any{"id": LiveHuddleID, "name": liveHuddleName, "media_server": "", "created_by": h.creator,
		"date_start": h.start, "date_end": 0, "has_ended": false, "participants": append([]string{}, h.participants...),
		"participant_history": append([]string{}, h.history...), "participants_count": len(h.participants),
		"channels": []string{LiveHuddleChannel}, "is_dm_call": false, "was_rejected": false, "was_missed": false,
		"was_accepted": false, "thread_root_ts": h.ts, "channel_type": "channel", "call_family": "huddle",
		"attached_file_ids": []string{}, "huddle_link": "https://app.slack.com/huddle/" + TeamID + "/" + LiveHuddleChannel}
}

func (w *World) roomJSONLocked(h *liveHuddle) json.RawMessage {
	b, _ := json.Marshal(w.roomLocked(h))
	return b
}

// activeHuddles is edge huddles/list.
func (w *World) activeHuddles() any {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.live == nil {
		return okMap("huddles", []any{})
	}
	return okMap("huddles", []any{w.roomLocked(w.live)})
}

// huddleShuffle has someone join or leave the running huddle: the
// realtime event, and the huddle message's room rewritten (message_changed
// is what Slack sends when a huddle_thread's room changes).
func (w *World) huddleShuffle() {
	w.mu.Lock()
	h := w.live
	if h == nil {
		w.mu.Unlock()
		return
	}
	var typ, user string
	switch {
	case len(h.bench) > 0 && (len(h.participants) <= 2 || w.rnd.Intn(2) == 0):
		i := w.rnd.Intn(len(h.bench))
		user = h.bench[i]
		h.bench = append(h.bench[:i:i], h.bench[i+1:]...)
		h.participants = append(h.participants, user)
		if !hasStr(h.history, user) {
			h.history = append(h.history, user)
		}
		typ = "sh_room_join"
	case len(h.participants) > 2:
		i := 1 + w.rnd.Intn(len(h.participants)-1) // the host stays
		user = h.participants[i]
		h.participants = append(h.participants[:i:i], h.participants[i+1:]...)
		h.bench = append(h.bench, user)
		typ = "sh_room_leave"
	default:
		w.mu.Unlock()
		return
	}
	room := w.roomLocked(h)
	var changed slack.Message
	if p := w.msgPtrLocked(LiveHuddleChannel, h.ts); p != nil {
		p.Room = w.roomJSONLocked(h)
		changed = copyMsg(*p)
	}
	w.mu.Unlock()
	now := microTS(w.now().UnixMicro())
	w.fs.Push(map[string]any{"type": typ, "user": user, "room": room, "huddle": room, "channel_id": LiveHuddleChannel, "event_ts": now})
	if changed.TS != "" {
		w.fs.Push(map[string]any{"type": "message", "subtype": "message_changed", "channel": LiveHuddleChannel, "hidden": true,
			"message": changed, "ts": now, "event_ts": now})
	}
	w.logf("demo: %s %s the huddle in #party-planning-crew", user, map[string]string{"sh_room_join": "joined", "sh_room_leave": "left"}[typ])
}

// liveHuddleLoop shuffles the running huddle every few minutes.
func (e *engine) liveHuddleLoop() {
	w := e.w
	for {
		if !w.sleep(w.randDur(2*time.Minute, 5*time.Minute)) {
			return
		}
		w.huddleShuffle()
	}
}

func sortNewestFirst(ms []slack.Message) {
	for i := 1; i < len(ms); i++ {
		for j := i; j > 0 && tsBefore(ms[j-1].TS, ms[j].TS); j-- {
			ms[j-1], ms[j] = ms[j], ms[j-1]
		}
	}
}

// huddleHistory is the Huddles page's history (huddles.history, newest
// first): the viewer's past week of huddles. The running one comes first.
func (w *World) huddleHistory() []fakeslack.HuddleFixture {
	id := func(h string) string {
		p, _ := w.person(h)
		return p.ID
	}
	w.mu.Lock()
	gordonDM, priyaDM := w.dmWithLocked(id("gordon")), w.dmWithLocked(id("priya"))
	var out []fakeslack.HuddleFixture
	if h := w.live; h != nil {
		out = append(out, fakeslack.HuddleFixture(w.roomLocked(h)))
	}
	w.mu.Unlock()
	now := w.now()
	past := func(n int, name, ch string, dm bool, ago, mins time.Duration, by string, who ...string) fakeslack.HuddleFixture {
		start := now.Add(-ago)
		var ids []string
		for _, h := range append([]string{by}, who...) {
			ids = append(ids, id(h))
		}
		typ := "channel"
		if dm {
			typ = "im"
		}
		return fakeslack.HuddleFixture{"id": "R0DEMOPAST" + string(rune('0'+n)), "name": name, "created_by": id(by),
			"date_start": start.Unix(), "date_end": start.Add(mins).Unix(), "has_ended": true, "participants": []string{},
			"participant_history": ids, "channels": []string{ch}, "is_dm_call": dm, "channel_type": typ, "call_family": "huddle"}
	}
	out = append(out,
		past(1, "", gordonDM, true, 22*time.Hour, 14*time.Minute, "gordon", "jordan"),
		past(2, "Sales stand-up", SalesID, false, 26*time.Hour, 11*time.Minute, "priya", "dale", "nadia", "kip", "jordan"),
		past(3, "Order portal bug bash", CodeID, false, 30*time.Hour, 32*time.Minute, "theo", "jordan", "nadia"),
		past(4, "", priyaDM, true, 50*time.Hour, 8*time.Minute, "priya", "jordan"),
		past(5, "Dock 2 walk-through", ConnectID, false, 74*time.Hour, 19*time.Minute, "wendell", "ines", "jordan", "dale"),
	)
	return out
}
