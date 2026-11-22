package store

import "github.com/some-du6e/clack/internal/slack"

// The notifications store's maxTs, as Slack's client keeps it (client-boot
// Fpyc, build 35b42de8b870: {maxTs: "0"} under storeKey "notifications",
// persistenceEnabled): on every "Add messages" batch (wVuW Kc, dispatched
// by each path that puts messages in Slack's store: history pages,
// replies, messages fetched by id, the socket's), the newest ts the batch
// holds, if newer. shouldShowNotificationForDesktopNotification drops a
// followed thread's reply newer than it ("Message is greater than latest
// in store, treating as duplicate", 19): the server's desktop_notification
// for a message the client hasn't taken. It starts at "0" and is kept in
// meta.json (diskCache.NotificationsMaxTS).

const notificationsMaxTSZero = "0"

// noteAddedTSLocked takes ts into maxTs (s.mu held).
func (s *Store) noteAddedTSLocked(ts string) {
	if ts == "" {
		return
	}
	if s.notesMaxTS == "" {
		s.notesMaxTS = notificationsMaxTSZero
	}
	if TSLess(s.notesMaxTS, ts) {
		s.notesMaxTS = ts
	}
}

// noteAddedLocked is noteAddedTSLocked for a batch's newest ts (s.mu held).
func (s *Store) noteAddedLocked(msgs []slack.Message) {
	for i := range msgs {
		s.noteAddedTSLocked(msgs[i].TS)
	}
}

// NoteAdded takes message ts into maxTs: a socket message the store may
// keep nowhere (a reply in a thread no cache holds), which Slack's store
// keeps all the same.
func (s *Store) NoteAdded(ts string) {
	s.mu.Lock()
	s.noteAddedTSLocked(ts)
	s.mu.Unlock()
}

// NotificationsMaxTS is maxTs: the newest ts of every message batch the
// store took this session or a past one ("0" before any).
func (s *Store) NotificationsMaxTS() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.notesMaxTS == "" {
		return notificationsMaxTSZero
	}
	return s.notesMaxTS
}
