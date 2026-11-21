package store

import (
	"bytes"
	"encoding/json"
	"reflect"

	"github.com/some-du6e/clack/internal/slack"
)

// Revisions a read brings, for every copy of the message and for core.
//
// Slack keeps one copy of each message: a history page, a replies answer
// or messages.list writes it (processMessages, trustReactions), and every
// view of the message draws that one. Clack's store keeps a window's,
// the cached threads' and the copies fetched by id, and a read writes one
// of them. After a socket gap a thread read for the first time had its
// root's new revision while the window, read before the gap, kept the old
// one, and closing the thread pane put an Activity row back on the
// window's (the row took the store's copy). So a read that brings a
// revision of a message the store held another of (one the socket changed
// while the read was out keeps the socket's: the reads merge those first)
// gives it to every copy, and the revision waits for core to announce it
// (TakeReadRevisions: an "update", which core's change table and every
// view take).
//
// Every read that writes the store calls noteReadLocked for each message
// it keeps, after it merged its answer with what the socket changed while
// it was out (never over the raw answer: a page read before a
// message_changed would spread the older revision): SetLatestRead and
// PrependOlderSince through readWindowLocked, SetThreadSince, PutFetched.
// msg-latency's window reads take the same two calls over what they keep:
// SetLatestSince calls s.readWindowLocked(ch, out, since) once its merge
// has built out and before the window is replaced, and so does
// RefreshRange over its out (store TestRefreshRangeRead, core
// TestReadsAnnounce and TestWindowRead*); SetLatestRead goes once
// fetchLatest calls SetLatestSince.

// ReadRevision is a message a read brought a new revision of.
type ReadRevision struct {
	Channel string
	Msg     slack.Message
}

// maxReadRevs bounds the revisions waiting for core (it takes them after
// each read; a store no core reads from drops the oldest).
const maxReadRevs = 4096

// noteReadLocked: a read keeps m for ch, its own place in the store
// having held prev (nil: none there). When the store held another
// revision of m (prev, else another place's), every copy takes m's and m
// waits to be announced. s.mu held, before the read writes its place.
func (s *Store) noteReadLocked(ch string, prev, m *slack.Message) {
	if m.TS == "" {
		return
	}
	if prev == nil {
		if prev = s.heldLocked(ch, m.TS); prev == nil {
			prev = s.fetched[ch+"/"+m.TS]
		}
	}
	if prev == nil || sameRevision(prev, m) {
		return
	}
	s.spreadLocked(ch, m)
	s.readRevs = append(s.readRevs, ReadRevision{Channel: ch, Msg: *m})
	if n := len(s.readRevs); n > maxReadRevs {
		s.readRevs = append(s.readRevs[:0:0], s.readRevs[n-maxReadRevs:]...)
	}
}

// spreadLocked writes m over every other copy of it the store keeps,
// keeping what's each copy's own: a reply's thread, the thread's
// subscription, a pin, the files the window filled in.
func (s *Store) spreadLocked(ch string, m *slack.Message) {
	put := func(x *slack.Message) {
		if sameRevision(x, m) {
			return
		}
		old := *x
		*x = *m
		if x.ThreadTS == "" {
			x.ThreadTS = old.ThreadTS
		}
		if !m.SubscribedSet {
			x.Subscribed, x.LastRead, x.SubscribedSet = old.Subscribed, old.LastRead, old.SubscribedSet
		}
		keepFilledFiles(x, &old)
		s.keepPinLocked(ch, x, &old)
	}
	if t := s.timelines[ch]; t != nil {
		for i := range t.Msgs {
			if t.Msgs[i].TS == m.TS {
				put(&t.Msgs[i])
			}
		}
		for i := range t.Ahead { // past a stale end (reached_end.go)
			if t.Ahead[i].TS == m.TS {
				put(&t.Ahead[i])
			}
		}
	}
	for k, th := range s.threads {
		if len(k) <= len(ch) || k[:len(ch)+1] != ch+"/" {
			continue
		}
		for i := range th {
			if th[i].TS == m.TS {
				put(&th[i])
			}
		}
	}
	if f := s.fetched[ch+"/"+m.TS]; f != nil {
		put(f)
		s.measureFetchedLocked(ch + "/" + m.TS)
	}
}

// TakeReadRevisions hands core the revisions the reads brought since it
// last asked, oldest first.
func (s *Store) TakeReadRevisions() []ReadRevision {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.readRevs
	s.readRevs = nil
	return out
}

// sameRevision reports whether a and b are one revision of a message as
// its views draw it: its text, blocks, attachments, files, edit,
// reactions (their counts: a read's lists can be shorter than the
// socket's) and thread. What is a copy's own (a subscription, a pin, a
// file's filled-in details) doesn't count.
func sameRevision(a, b *slack.Message) bool {
	if a.Text != b.Text || a.Subtype != b.Subtype || a.ReplyCount != b.ReplyCount || a.LatestReply != b.LatestReply ||
		a.ReplyUsersN != b.ReplyUsersN || a.IsLocked != b.IsLocked || editedTS(a) != editedTS(b) ||
		len(a.Reactions) != len(b.Reactions) || len(a.Files) != len(b.Files) {
		return false
	}
	for i := range a.Reactions {
		if a.Reactions[i].Name != b.Reactions[i].Name || a.Reactions[i].Count != b.Reactions[i].Count {
			return false
		}
	}
	for i := range a.Files {
		if a.Files[i].ID != b.Files[i].ID {
			return false
		}
	}
	if len(a.RawAttachments) == 0 && len(b.RawAttachments) == 0 && len(a.Attachments) != len(b.Attachments) {
		return false
	}
	return jsonSame(a.Blocks, b.Blocks) && jsonSame(a.RawAttachments, b.RawAttachments)
}

func editedTS(m *slack.Message) string {
	if m.Edited == nil {
		return ""
	}
	return m.Edited.TS
}

// jsonSame reports whether a and b say the same, however they are spaced
// or their keys ordered.
func jsonSame(a, b json.RawMessage) bool {
	if bytes.Equal(a, b) {
		return true
	}
	if isNullJSON(a) && isNullJSON(b) {
		return true
	}
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

func isNullJSON(r json.RawMessage) bool {
	return len(bytes.TrimSpace(r)) == 0 || string(bytes.TrimSpace(r)) == "null"
}

// keepFilledFiles keeps old's filled-in files where m has the stubs of
// the same files (a read brings stubs core filled in since: files.info).
func keepFilledFiles(m, old *slack.Message) {
	if len(old.Files) == 0 || len(m.Files) == 0 {
		return
	}
	byID := make(map[string]*slack.File, len(old.Files))
	for i := range old.Files {
		if !FileStub(&old.Files[i]) {
			byID[old.Files[i].ID] = &old.Files[i]
		}
	}
	var fs []slack.File
	for i := range m.Files {
		f, ok := byID[m.Files[i].ID]
		if !ok || !FileStub(&m.Files[i]) {
			continue
		}
		if fs == nil {
			fs = append([]slack.File(nil), m.Files...)
		}
		fs[i] = *f
	}
	if fs != nil {
		m.Files = fs
	}
}

// FileStub reports a file a message carries only part of (an upload not
// processed yet, a check_file_info stub): files.info has the rest.
func FileStub(f *slack.File) bool {
	if f.ID == "" || f.Mode == "tombstone" || f.Mode == "hidden_by_limit" {
		return false
	}
	if f.FileAccess == "check_file_info" || f.URLPrivate == "" {
		return true
	}
	return !f.IsExternal && f.Mimetype == "" && f.Filetype == ""
}

// SetLatestRead is SetLatest for a newest-page read of ch whose request
// began at LiveSeq since: a message the socket changed meanwhile keeps
// the store's copy (the socket's), one it deleted stays out
// (liveKeepLocked), then the window read's hooks run over what it keeps
// (readWindowLocked). It is SetLatestReadAt for a read no reset can have
// crossed, with none of the viewer's sends to carry.
func (s *Store) SetLatestRead(ch string, msgs []slack.Message, hasMore bool, since uint64) {
	s.SetLatestReadAt(ch, msgs, hasMore, since, s.EndGen(), nil)
}

// liveKeepLocked is what a read of ch's messages begun at LiveSeq since
// keeps of them (liveReadLocked), in order, the ones the socket deleted
// meanwhile left out (s.mu held).
func (s *Store) liveKeepLocked(ch string, msgs []slack.Message, since uint64) []slack.Message {
	out := make([]slack.Message, 0, len(msgs))
	for i := range msgs {
		if m, _, gone := s.liveReadLocked(ch, &msgs[i], since); !gone {
			out = append(out, m)
		}
	}
	return out
}

// readWindowLocked is a window read's hooks over out, what it keeps of
// ch's messages (merged with what the socket changed while it was out),
// before it writes them into the window: the reactions the socket brought
// after since are replayed on them (reacts.go), and a revision the store
// held another of (the window's copy, else another place's) reaches
// every copy and core (noteReadLocked) (s.mu held).
func (s *Store) readWindowLocked(ch string, out []slack.Message, since uint64) {
	s.replayAllReactsLocked(ch, out, since)
	var old map[string]*slack.Message
	if t := s.timelines[ch]; t != nil {
		old = make(map[string]*slack.Message, len(t.Msgs))
		for i := range t.Msgs {
			old[t.Msgs[i].TS] = &t.Msgs[i]
		}
	}
	for i := range out {
		s.noteReadLocked(ch, old[out[i].TS], &out[i])
	}
}

// readThreadLocked is noteReadLocked for the replies answer msgs of the
// thread ch/ts, before it replaces the cached thread (SetThreadSince).
func (s *Store) readThreadLocked(ch, ts string, msgs []slack.Message) {
	held := s.threads[ch+"/"+ts]
	old := make(map[string]*slack.Message, len(held))
	for i := range held {
		old[held[i].TS] = &held[i]
	}
	for i := range msgs {
		s.noteReadLocked(ch, old[msgs[i].TS], &msgs[i])
	}
}
