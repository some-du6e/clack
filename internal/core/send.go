package core

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
)

// Optimistic sends.
//
// Send inserts a pending copy of the message into the cached timeline (or
// thread) under a local ts and emits Event{Type:"message"} with
// Msg.Pending=true at once. When Slack confirms (the realtime echo with
// the same client_msg_id, or the chat.postMessage reply, whichever comes
// first) the pending copy is replaced and Event{Type:"sent", ReplacesTS:
// <local ts>, TS: <real ts>, Msg} is emitted; the echo does not produce a
// second "message" event. On failure the copy stays with Failed=true
// (Event{Type:"update"}) until Resend or DiscardPending.

type pendingSend struct {
	opts      SendOpts
	ts        string // local ts of the pending copy
	realTS    string // set once confirmed
	failed    bool
	confirmed bool
}

// pendingTS returns a local ts that sorts after everything cached in ch.
func (a *App) pendingTS(ch, threadTS string) string {
	now := time.Now()
	ts := fmt.Sprintf("%d.%06d", now.Unix(), now.Nanosecond()/1000)
	var last string
	if threadTS != "" {
		if th, ok := a.S.Thread(ch, threadTS); ok && len(th) > 0 {
			last = th[len(th)-1].TS
		}
	} else if tl := a.S.Timeline(ch); tl != nil && len(tl.Msgs) > 0 {
		last = tl.Msgs[len(tl.Msgs)-1].TS
	}
	a.mu.Lock()
	for _, p := range a.pending {
		if !p.confirmed && store.TSLess(last, p.ts) {
			last = p.ts
		}
	}
	a.mu.Unlock()
	if last != "" && !store.TSLess(last, ts) {
		ts = tsAfter(last)
	}
	return ts
}

// tsAfter returns ts plus one microsecond.
func tsAfter(ts string) string {
	sec, frac, ok := strings.Cut(ts, ".")
	s, err1 := strconv.ParseInt(sec, 10, 64)
	f, err2 := strconv.ParseInt(frac, 10, 64)
	if !ok || err1 != nil || err2 != nil || len(frac) != 6 {
		return ts + "1"
	}
	f++
	if f > 999999 {
		s, f = s+1, 0
	}
	return fmt.Sprintf("%d.%06d", s, f)
}

func isTopLevel(m *slack.Message) bool {
	return m.ThreadTS == "" || m.ThreadTS == m.TS || m.Subtype == "thread_broadcast" || m.ReplyBroadcast
}

// putLocal inserts or replaces a message in the cached timeline / thread.
func (a *App) putLocal(ch string, m slack.Message) {
	if isTopLevel(&m) {
		a.S.Upsert(ch, m)
	}
	if m.ThreadTS != "" && m.ThreadTS != m.TS {
		a.S.AddReply(ch, m)
	}
}

// Send posts a message optimistically (see above). It blocks until Slack
// answers and returns the confirmed message.
func (a *App) Send(ctx context.Context, o SendOpts) (Message, error) {
	if o.Channel == "" {
		return Message{}, errors.New("core: Send needs a channel")
	}
	return a.send(ctx, o, clientMsgID())
}

func (a *App) send(ctx context.Context, o SendOpts, cid string) (Message, error) {
	p := &pendingSend{opts: o, ts: a.pendingTS(o.Channel, o.ThreadTS)}
	local := slack.Message{Type: "message", TS: p.ts, User: a.S.SelfID(), Text: o.Text, ThreadTS: o.ThreadTS,
		ClientMsgID: cid, Blocks: o.Blocks, ReplyBroadcast: o.Broadcast && o.ThreadTS != ""}
	a.mu.Lock()
	a.pending[cid] = p
	a.mu.Unlock()
	a.putLocal(o.Channel, local)
	v := a.message(o.Channel, &local)
	a.emit(Event{Type: "message", Channel: o.Channel, TS: p.ts, ThreadTS: o.ThreadTS, UserID: local.User, Msg: &v,
		ClientMsgID: cid, Reply: !isTopLevel(&local), ConvKind: a.kindOf(o.Channel)})

	m, err := a.C.Post(ctx, slack.PostOpts{Channel: o.Channel, Text: o.Text, ThreadTS: o.ThreadTS,
		Broadcast: o.Broadcast, Blocks: o.Blocks, ClientMsgID: cid})
	if err != nil {
		a.mu.Lock()
		confirmed := p.confirmed
		if !confirmed {
			p.failed = true
		}
		a.mu.Unlock()
		if confirmed { // the echo beat the (failed) reply: it was sent after all
			if got, ok := a.S.Find(o.Channel, p.realTS); ok {
				return a.message(o.Channel, &got), nil
			}
		}
		if fm, ok := a.S.Find(o.Channel, p.ts); ok {
			fv := a.message(o.Channel, &fm)
			a.emit(Event{Type: "update", Channel: o.Channel, TS: p.ts, ThreadTS: o.ThreadTS, Msg: &fv, ClientMsgID: cid})
		}
		return Message{}, err
	}
	if m.ClientMsgID == "" {
		m.ClientMsgID = cid
	}
	if m.ThreadTS == "" {
		m.ThreadTS = o.ThreadTS
	}
	if m.User == "" {
		m.User = local.User
	}
	if !a.confirmPending(o.Channel, m) {
		if got, ok := a.S.Find(o.Channel, m.TS); ok { // the echo already replaced it
			m = &got
		}
	}
	return a.message(o.Channel, m), nil
}

// confirmPending swaps a pending copy for the confirmed message. It reports
// false when there was nothing pending for its client_msg_id (already
// confirmed, or not ours).
func (a *App) confirmPending(ch string, m *slack.Message) bool {
	if m.ClientMsgID == "" {
		return false
	}
	a.mu.Lock()
	p, ok := a.pending[m.ClientMsgID]
	if !ok || p.confirmed {
		a.mu.Unlock()
		return false
	}
	p.confirmed, p.failed, p.realTS = true, false, m.TS
	cid := m.ClientMsgID
	a.mu.Unlock()
	time.AfterFunc(2*time.Minute, func() {
		a.mu.Lock()
		delete(a.pending, cid)
		a.mu.Unlock()
	})
	a.S.Remove(ch, p.ts)
	a.putLocal(ch, *m)
	if !isTopLevel(m) {
		a.bumpParent(ch, m)
	}
	v := a.message(ch, m)
	a.emit(Event{Type: "sent", Channel: ch, TS: m.TS, ThreadTS: m.ThreadTS, ReplacesTS: p.ts, ClientMsgID: cid,
		UserID: m.User, Msg: &v, Reply: !isTopLevel(m), ConvKind: a.kindOf(ch)})
	return true
}

// Resend retries a failed send (same client_msg_id, so Slack de-duplicates).
func (a *App) Resend(ctx context.Context, ch, clientMsgID string) (Message, error) {
	a.mu.Lock()
	p, ok := a.pending[clientMsgID]
	a.mu.Unlock()
	if !ok || !p.failed {
		return Message{}, errors.New("core: no failed message to resend")
	}
	a.DiscardPending(ch, clientMsgID)
	return a.send(ctx, p.opts, clientMsgID)
}

// DiscardPending drops a pending or failed local message (emits "delete").
func (a *App) DiscardPending(ch, clientMsgID string) {
	a.mu.Lock()
	p, ok := a.pending[clientMsgID]
	if ok && !p.confirmed {
		delete(a.pending, clientMsgID)
	}
	a.mu.Unlock()
	if !ok || p.confirmed {
		return
	}
	a.S.Remove(ch, p.ts)
	a.emit(Event{Type: "delete", Channel: ch, TS: p.ts, ThreadTS: p.opts.ThreadTS, ClientMsgID: clientMsgID})
}

// pendingState reports whether a cached message is a local pending/failed copy.
func (a *App) pendingState(m *slack.Message) (pending, failed bool) {
	if m.ClientMsgID == "" {
		return false, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if p, ok := a.pending[m.ClientMsgID]; ok && p.ts == m.TS && !p.confirmed {
		return !p.failed, p.failed
	}
	return false, false
}

// isLocalTS reports whether ts belongs to an unconfirmed local message.
func (a *App) isLocalTS(ts string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, p := range a.pending {
		if p.ts == ts && !p.confirmed {
			return true
		}
	}
	return false
}

// bumpParent updates a thread parent's reply metadata for a new reply.
func (a *App) bumpParent(ch string, m *slack.Message) {
	if upd, ok := a.S.Mutate(ch, m.ThreadTS, func(x *slack.Message) {
		x.ReplyCount++
		x.LatestReply = m.TS
		for _, u := range x.ReplyUsers {
			if u == m.User {
				return
			}
		}
		if m.User != "" {
			x.ReplyUsers = append(x.ReplyUsers, m.User)
		}
	}); ok {
		a.emitUpdate(ch, &upd)
	}
}

// wasConfirmed reports whether m is the (late) echo of an already confirmed send.
func (a *App) wasConfirmed(m *slack.Message) bool {
	if m.ClientMsgID == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.pending[m.ClientMsgID]
	return ok && p.confirmed && p.realTS == m.TS
}
