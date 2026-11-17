package core

import (
	"context"
	"time"
)

// Revisions core's reads bring, announced as changes.
//
// A read that brings a revision of a message the store held another of
// (a thread read for the first time after a socket gap, a window read
// after one, messages.list) gives it to every copy the store keeps
// (store/read_revisions.go). Core announces it here as an "update", with
// no notification (nothing new arrived), so the change table
// (msg_changes.go) and every view keeping copies take it, as every row of
// Slack's draws what its one store holds once a replies answer or a
// history page lands.

// announceReads emits the revisions the store's reads brought since it
// last looked.
func (a *App) announceReads() {
	for _, r := range a.S.TakeReadRevisions() {
		m := r.Msg
		v := a.messageWith(r.Channel, &m, true) // asks for nobody: the rows ask as they draw
		a.emit(Event{Type: "update", Channel: r.Channel, TS: m.TS, ThreadTS: m.ThreadTS, Msg: &v})
	}
}

// RefetchMessages asks Slack again for the messages tss of ch by id (as
// Slack's row fetches a message its store doesn't hold), off the caller's
// goroutine: what comes back is kept and announced as a change. A view
// calls it for a message core changed but no longer has a copy of (the
// store let it go). One request per message while one is out.
func (a *App) RefetchMessages(ch string, tss []string) {
	l := &a.msgLog
	l.mu.Lock()
	var want []string
	for _, ts := range tss {
		k := MsgKey{ch, ts}
		if ch == "" || ts == "" || l.refetching[k] {
			continue
		}
		if l.refetching == nil {
			l.refetching = map[MsgKey]bool{}
		}
		l.refetching[k] = true
		want = append(want, ts)
	}
	l.mu.Unlock()
	if len(want) == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.bg, 30*time.Second)
		defer cancel()
		got, _ := a.hydrateKeep(ctx, map[string][]string{ch: want}, true)
		for _, ts := range want {
			if m, ok := got[ch+"/"+ts]; ok {
				v := a.messageWith(ch, &m, true)
				a.emit(Event{Type: "update", Channel: ch, TS: ts, ThreadTS: m.ThreadTS, Msg: &v})
			}
		}
		l.mu.Lock()
		for _, ts := range want {
			delete(l.refetching, MsgKey{ch, ts})
		}
		l.mu.Unlock()
	}()
}
