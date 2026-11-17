package store

import (
	"slices"

	"github.com/some-du6e/clack/internal/slack"
)

// Realtime reactions that a fetch must not undo.
//
// Slack's reactions reducer changes the stored message's reactions as
// reaction_added and reaction_removed arrive. A message fetched while one
// was out comes back with its reactions as Slack had them when it
// answered: put back as it was, it lost a reaction the socket added
// meanwhile (or got back one it removed), and the next reaction's update
// carried a count one off (Activity's rows bounced between counts during
// a burst). So each reaction is noted here, in the order of the other
// realtime changes (LiveSeq), and a fetch replays the ones that came after
// its request began on the copy it keeps (PutFetched, SetThreadSince).
// Each counts a person once, as the reducer does (ApplyReaction), so a
// reaction the answer already has changes nothing.

type reactOp struct {
	seq        uint64
	name, user string
	add        bool
}

const (
	maxReactKeys = 1024 // the messages whose reactions are noted
	maxReactOps  = 32   // the latest reactions noted per message
)

// NoteReaction records user's reaction name added to (add) or taken off
// ch/ts, whether or not the store holds the message.
func (s *Store) NoteReaction(ch, ts, name, user string, add bool) {
	if ch == "" || ts == "" || name == "" || user == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.liveSeq++
	k := ch + "/" + ts
	if s.reacts == nil {
		s.reacts = map[string][]reactOp{}
	}
	ops, ok := s.reacts[k]
	if !ok {
		s.reactQ = append(s.reactQ, k)
		for len(s.reactQ) > maxReactKeys {
			delete(s.reacts, s.reactQ[0])
			s.reactQ = s.reactQ[1:]
		}
	}
	if len(ops) >= maxReactOps {
		ops = slices.Delete(slices.Clone(ops), 0, len(ops)-maxReactOps+1)
	}
	s.reacts[k] = append(ops, reactOp{seq: s.liveSeq, name: name, user: user, add: add})
}

// replayReactsLocked applies the reactions noted for ch/m.TS after since
// to m, in the order they came.
func (s *Store) replayReactsLocked(ch string, m *slack.Message, since uint64) {
	for _, op := range s.reacts[ch+"/"+m.TS] {
		if op.seq > since {
			ApplyReaction(m, op.name, op.user, op.add)
		}
	}
}

// replayAllReactsLocked is replayReactsLocked for each of msgs. Replayed
// in order, a person's latest reaction decides their state, whatever the
// copy had.
func (s *Store) replayAllReactsLocked(ch string, msgs []slack.Message, since uint64) {
	if len(s.reacts) == 0 {
		return
	}
	for i := range msgs {
		s.replayReactsLocked(ch, &msgs[i], since)
	}
}

// ApplyReaction adds or removes user's name reaction on m, counting each
// user once (Slack's reactions reducer). It never writes to the old
// slices (copies already handed out share them) and reports whether m
// changed.
func ApplyReaction(m *slack.Message, name, user string, add bool) bool {
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
