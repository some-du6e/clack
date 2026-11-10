package core

import (
	"context"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Reactions are optimistic, like Slack's: React changes the stored message
// and emits "update" before reactions.add/remove goes out, and puts it back
// (another "update") if the call fails. While the call is in flight, and
// for reactGrace after it succeeded, the change is also laid over every
// copy of the message core hands out, so a history refresh or poll that
// raced it doesn't take the reaction away again. The realtime echo
// (reaction_added/removed for the viewer) is a no-op: applyReaction counts
// a user once.

// reactGrace covers reads that started before reactions.add returned (a
// poll, a channel refresh) and land after it.
const reactGrace = 15 * time.Second

type rxnKey struct{ ch, ts, name string }

type rxnOp struct {
	add bool
	seq uint64
}

type reactState struct {
	n   atomic.Int32 // len(ops), for message()'s fast path
	mu  sync.Mutex
	ops map[rxnKey]rxnOp
	seq uint64
}

func (r *reactState) begin(k rxnKey, add bool) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ops == nil {
		r.ops = map[rxnKey]rxnOp{}
	}
	r.seq++
	r.ops[k] = rxnOp{add: add, seq: r.seq}
	r.n.Store(int32(len(r.ops)))
	return r.seq
}

// end drops op seq; false if a later React on the same reaction replaced it.
func (r *reactState) end(k rxnKey, seq uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if op, ok := r.ops[k]; !ok || op.seq != seq {
		return false
	}
	delete(r.ops, k)
	r.n.Store(int32(len(r.ops)))
	return true
}

// pendingFor returns the viewer's reaction changes not yet settled on ts.
func (r *reactState) pendingFor(ch, ts string) map[string]bool {
	if r.n.Load() == 0 {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var out map[string]bool
	for k, op := range r.ops {
		if k.ch == ch && k.ts == ts {
			if out == nil {
				out = map[string]bool{}
			}
			out[k.name] = op.add
		}
	}
	return out
}

// React adds or removes the viewer's reaction: at once in the store (an
// "update" event), then reactions.add/remove; a failed call is rolled back
// (another "update") and its error returned for the UI's toast.
// already_reacted / no_reaction count as success.
func (a *App) React(ctx context.Context, ch, ts, name string, add bool) error {
	name = strings.Trim(name, ":")
	self := a.S.SelfID()
	k := rxnKey{ch, ts, name}
	seq := a.rxn.begin(k, add)
	changed := a.applyOwnReaction(ch, ts, name, self, add)

	err := a.C.React(ctx, ch, ts, name, add)
	if isCode(err, "already_reacted", "no_reaction") {
		err = nil
	}
	if err != nil {
		if a.rxn.end(k, seq) && changed {
			a.applyOwnReaction(ch, ts, name, self, !add)
		}
		return err
	}
	time.AfterFunc(reactGrace, func() { a.rxn.end(k, seq) })
	return nil
}

// applyOwnReaction changes the stored message and tells subscribers;
// it reports whether anything changed.
func (a *App) applyOwnReaction(ch, ts, name, self string, add bool) bool {
	changed := false
	m, ok := a.S.Mutate(ch, ts, func(m *slack.Message) { changed = applyReaction(m, name, self, add) || changed })
	if ok && changed {
		a.emitUpdate(ch, &m)
	}
	return changed
}

// applyReaction adds or removes user's name reaction on m, counting each
// user once. It never writes to the old slices (copies already handed to
// subscribers share them) and reports whether m changed.
func applyReaction(m *slack.Message, name, user string, add bool) bool {
	for i := range m.Reactions {
		r := m.Reactions[i]
		if r.Name != name {
			continue
		}
		has := slices.Contains(r.Users, user)
		if add == has {
			return false
		}
		rs := slices.Clone(m.Reactions)
		if add {
			rs[i].Users = append(slices.Clip(r.Users), user)
			rs[i].Count++
		} else {
			rs[i].Users = slices.DeleteFunc(slices.Clone(r.Users), func(u string) bool { return u == user })
			rs[i].Count--
			if rs[i].Count <= 0 {
				rs = slices.Delete(rs, i, i+1)
			}
		}
		m.Reactions = rs
		return true
	}
	if !add {
		return false
	}
	m.Reactions = append(slices.Clip(m.Reactions), slack.Reaction{Name: name, Count: 1, Users: []string{user}})
	return true
}

// overlayReactions lays the viewer's unsettled reaction changes over rs
// (a fresh slice from message(); its Users slices are the store's).
func (a *App) overlayReactions(ch, ts string, rs []Reaction) []Reaction {
	ops := a.rxn.pendingFor(ch, ts)
	if len(ops) == 0 {
		return rs
	}
	self := a.S.SelfID()
	for name, add := range ops {
		i := slices.IndexFunc(rs, func(r Reaction) bool { return r.Name == name })
		switch {
		case i < 0 && add:
			url, _, _ := a.S.Emoji(name)
			rs = append(rs, Reaction{Name: name, Count: 1, Users: []string{self}, URL: url, Self: true})
		case i >= 0 && add && !rs[i].Self:
			rs[i].Self, rs[i].Count = true, rs[i].Count+1
			rs[i].Users = append(slices.Clip(rs[i].Users), self)
		case i >= 0 && !add && rs[i].Self:
			rs[i].Self, rs[i].Count = false, rs[i].Count-1
			rs[i].Users = slices.DeleteFunc(slices.Clone(rs[i].Users), func(u string) bool { return u == self })
			if rs[i].Count <= 0 {
				rs = slices.Delete(rs, i, i+1)
			}
		}
	}
	return rs
}
