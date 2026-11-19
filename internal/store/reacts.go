package store

import (
	"slices"
	"time"

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
	url        string // reaction_added's: an emoji the workspace hasn't got (a Slack Connect partner's)
	add        bool
	at         time.Time
}

const (
	maxReactKeys = 1024 // the messages whose reactions are noted
	maxReactOps  = 32   // the latest reactions noted per message, past reactKeep
	// reactKeep is how long a reaction stays noted whatever the count (a
	// fetch takes at most this: its 45 s timeout), up to maxReactBurst
	reactKeep     = 2 * time.Minute
	maxReactBurst = 1024
)

// NoteReaction records user's reaction name added to (add) or taken off
// ch/ts, whether or not the store holds the message.
func (s *Store) NoteReaction(ch, ts, name, user string, add bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.noteReactionLocked(ch, ts, name, user, "", add)
}

// ApplyReactionNoted is a reaction the socket brought (or the viewer's
// own): user's name added to (add) or taken off every copy of ch/ts, and
// noted for the fetches out now (NoteReaction), under one lock. Made apart,
// a fetch landing between the two put back an answer from before it.
// It returns the first copy (Mutate's), whether there was one, and
// whether it changed.
func (s *Store) ApplyReactionNoted(ch, ts, name, user string, add bool) (m slack.Message, found, changed bool) {
	return s.ApplyReactionNotedURL(ch, ts, name, user, "", add)
}

// ApplyReactionNotedURL is ApplyReactionNoted with the reaction's url, a
// reaction_added's: an emoji the workspace hasn't got (a Slack Connect
// partner's custom one), which the pill draws from it (cBAo addReaction:
// m&&(h.url=m)); a fetch's replay keeps it. A malformed event, with no
// message, no reaction or an add by nobody, changes nothing, as cBAo
// (key, name and userId) and yzJf (key and name) refuse it.
func (s *Store) ApplyReactionNotedURL(ch, ts, name, user, url string, add bool) (m slack.Message, found, changed bool) {
	if ch == "" || ts == "" || name == "" || add && user == "" {
		return slack.Message{}, false, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.noteReactionLocked(ch, ts, name, user, url, add)
	m, found = s.mutateLocked(ch, ts, func(x *slack.Message) { changed = ApplyReactionURL(x, name, user, url, add) || changed })
	return m, found, changed
}

func (s *Store) noteReactionLocked(ch, ts, name, user, url string, add bool) {
	if ch == "" || ts == "" || name == "" || user == "" {
		return
	}
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
	now := time.Now()
	if len(ops) >= maxReactOps {
		// the oldest go once a fetch can't be out from before them
		drop := 0
		for drop < len(ops)-maxReactOps+1 && (now.Sub(ops[drop].at) > reactKeep || len(ops)-drop >= maxReactBurst) {
			drop++
		}
		if drop > 0 {
			ops = slices.Delete(slices.Clone(ops), 0, drop)
		}
	}
	s.reacts[k] = append(ops, reactOp{seq: s.liveSeq, name: name, user: user, url: url, add: add, at: now})
}

// replayReactsLocked applies the reactions noted for ch/m.TS after since
// to m, in the order they came. A reaction whose answer lists fewer people
// than it counts (Slack lists some of a big reaction's) may count one made
// meanwhile already: the count stays the answer's for the people it
// doesn't list.
func (s *Store) replayReactsLocked(ch string, m *slack.Message, since uint64) {
	for _, op := range s.reacts[ch+"/"+m.TS] {
		if op.seq <= since {
			continue
		}
		if i := slices.IndexFunc(m.Reactions, func(r slack.Reaction) bool { return r.Name == op.name }); i >= 0 {
			if r := &m.Reactions[i]; r.Count > len(r.Users) && !slices.Contains(r.Users, op.user) {
				continue
			}
		}
		ApplyReactionURL(m, op.name, op.user, op.url, op.add)
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
// user once (Slack's reactions reducer, boot-shared cBAo addReaction and
// yzJf removeReaction: a person it doesn't list changes nothing, and a
// reaction goes once nobody it lists is left or its count is below 1). It never
// writes to the old slices (copies already handed out share them) and
// reports whether m changed.
func ApplyReaction(m *slack.Message, name, user string, add bool) bool {
	return ApplyReactionURL(m, name, user, "", add)
}

// ApplyReactionURL is ApplyReaction with an add's url, set on the
// reaction it makes or joins, as cBAo sets h.url before comparing: an
// add by someone listed changes only a url it brings anew.
func ApplyReactionURL(m *slack.Message, name, user, url string, add bool) bool {
	for i := range m.Reactions {
		r := m.Reactions[i]
		if r.Name != name {
			continue
		}
		has := slices.Contains(r.Users, user)
		if add && has {
			if url == "" || r.URL == url {
				return false
			}
			rs := slices.Clone(m.Reactions)
			rs[i].URL = url
			m.Reactions = rs
			return true
		}
		if add == has {
			return false
		}
		rs := slices.Clone(m.Reactions)
		if add {
			rs[i].Users = append(slices.Clip(r.Users), user)
			rs[i].Count++
			if url != "" {
				rs[i].URL = url
			}
		} else {
			rs[i].Users = slices.DeleteFunc(slices.Clone(r.Users), func(u string) bool { return u == user })
			rs[i].Count--
			if len(rs[i].Users) == 0 || rs[i].Count < 1 {
				rs = slices.Delete(rs, i, i+1)
			}
		}
		m.Reactions = rs
		return true
	}
	if !add {
		return false
	}
	m.Reactions = append(slices.Clip(m.Reactions), slack.Reaction{Name: name, Count: 1, Users: []string{user}, URL: url})
	return true
}
