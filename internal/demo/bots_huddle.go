package demo

import (
	"fmt"
	"net/url"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// Huddles. Now and then Gordon or Priya rings the viewer from their DM:
// huddle_invite as Slack sends it (channel_id, call_id, sender_user_id,
// team_id; internal/ui/huddleui's inviteEvent), cancelled with
// huddle_invite_cancel after 30 s without an answer, after which they
// type what they wanted to say. The demo has no audio: rooms.join fails
// with a clear error and the caller says something about it; a decline
// (rooms.inviteResponse, answered by the fake) gets a reply too.

type ringState struct {
	channel, call, from string
	answer              chan string // "decline", "join", "joining_soon"
}

// RingHuddle has from (a handle or user id) ring the viewer now, unless
// a ring is already pending.
func (w *World) RingHuddle(from string) {
	p, ok := w.person(from)
	if !ok {
		return
	}
	dm := w.ensureDM(p.ID)
	w.mu.Lock()
	if w.huddle != nil {
		w.mu.Unlock()
		return
	}
	r := &ringState{channel: dm, from: p.ID, answer: make(chan string, 1),
		call: fmt.Sprintf("R0DEMO%08d", w.nextMicroLocked()%1e8)}
	w.huddle = r
	w.mu.Unlock()
	w.fs.Push(map[string]any{"type": "huddle_invite", "channel_id": dm, "call_id": r.call, "sender_user_id": p.ID,
		"team_id": TeamID, "user_id": SelfID, "event_ts": microTS(w.now().UnixMicro())})
	w.goBusy(func() { w.bots.awaitRing(r, p) })
}

func (e *engine) awaitRing(r *ringState, p Person) {
	w := e.w
	d := time.Duration(float64(30*time.Second) / w.opts.Speed)
	t := time.NewTimer(d)
	defer t.Stop()
	kind := "missed"
	select {
	case <-w.ctx.Done():
		return
	case a := <-r.answer:
		kind = map[string]string{"decline": "declined"}[a]
		if kind == "" {
			kind = "no_audio"
		}
	case <-t.C:
		w.fs.Push(map[string]any{"type": "huddle_invite_cancel", "channel_id": r.channel, "call_id": r.call,
			"sender_user_id": r.from, "team_id": TeamID})
	}
	if kind != "missed" {
		// the ring stops once you answer either way
		w.fs.Push(map[string]any{"type": "huddle_invite_cancel", "channel_id": r.channel, "call_id": r.call,
			"sender_user_id": r.from, "team_id": TeamID})
	}
	w.mu.Lock()
	if w.huddle == r {
		w.huddle = nil
	}
	w.mu.Unlock()
	if kind != "missed" {
		e.mu.Lock()
		e.answered, e.answeredAt = r.channel, time.Now()
		e.mu.Unlock()
	}
	e.huddleLine(r.channel, p, kind)
}

// huddleLine has p say their kind ("missed", "declined", "no_audio")
// line in ch.
func (e *engine) huddleLine(ch string, p Person, kind string) {
	w := e.w
	lines := e.s.Replies.Huddle[p.Handle][kind]
	if len(lines) == 0 {
		lines = e.s.Replies.Huddle["gordon"][kind]
	}
	if len(lines) == 0 {
		return
	}
	text := e.expand(e.line(lines), "")
	if e.typeThenWait(p.ID, ch, "", text, w.randDur(2*time.Second, 4*time.Second)) {
		w.post(ch, slack.Message{User: p.ID, Text: text}, false)
	}
}

// onInviteResponse sees rooms.inviteResponse (Decline / Be there soon /
// accept) for the pending ring.
func (e *engine) onInviteResponse(f url.Values) {
	e.w.mu.Lock()
	r := e.w.huddle
	e.w.mu.Unlock()
	if r == nil || (f.Get("room_id") != r.call && f.Get("channel_id") != r.channel) {
		return
	}
	select {
	case r.answer <- f.Get("response"):
	default:
	}
}

// onJoinAttempt is rooms.join: answering the ring, or starting a huddle
// yourself (in a DM the other person answers; in a channel a regular).
func (e *engine) onJoinAttempt(ch string) {
	w := e.w
	w.mu.Lock()
	r := w.huddle
	c, ok := w.convLocked(ch)
	var conv slack.Conversation
	if ok {
		conv = *c
	}
	w.mu.Unlock()
	if r != nil && r.channel == ch {
		select {
		case r.answer <- "join":
		default:
		}
		return
	}
	e.mu.Lock()
	// accepting a ring sends rooms.inviteResponse, then rooms.join: the
	// caller has answered already
	echo := e.answered == ch && time.Since(e.answeredAt) < 10*time.Second
	e.mu.Unlock()
	if !ok || echo {
		return
	}
	who := ""
	switch {
	case conv.IsIM:
		who = conv.User
	default:
		pool := e.s.Replies.ChannelOwners[e.channelKey(ch)]
		if len(pool) == 0 {
			pool = []string{"gordon"}
		}
		who = pool[w.pick(len(pool))]
	}
	p, found := w.person(who)
	if !found || p.Bot || p.Self {
		return
	}
	w.goBusy(func() { e.huddleLine(ch, p, "no_audio") })
}

// huddleLoop: the first ring after 2–4 minutes, then every 10–20.
func (e *engine) huddleLoop() {
	w := e.w
	if !w.sleep(w.randDur(2*time.Minute, 4*time.Minute)) {
		return
	}
	for {
		from := "gordon"
		if w.chance(0.5) {
			from = "priya"
		}
		w.RingHuddle(from)
		if !w.sleep(w.randDur(10*time.Minute, 20*time.Minute)) {
			return
		}
	}
}
