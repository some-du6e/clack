package store

import (
	"encoding/json"
	"sync"

	"github.com/some-du6e/clack/internal/richtext"
)

// List slugs' records (richtext.ListRecordResolver): an item's primary
// field and whether it still exists, in memory only. A miss is queued for
// core's resolve loop (TakeMissingListRecords), which fills it and emits
// "names".

type listRecordName struct {
	raw   json.RawMessage
	state string // base | deleted | missing
}

type listRecordRefs struct {
	mu      sync.Mutex
	recs    map[string]listRecordName // file id + "_" + record id
	seen    map[string]bool
	pending [][2]string
}

var listRecordRefsOf sync.Map // *Store -> *listRecordRefs

func (s *Store) listRecordRefs() *listRecordRefs {
	if v, ok := listRecordRefsOf.Load(s); ok {
		return v.(*listRecordRefs)
	}
	v, _ := listRecordRefsOf.LoadOrStore(s, &listRecordRefs{recs: map[string]listRecordName{}, seen: map[string]bool{}})
	return v.(*listRecordRefs)
}

// ListRecordName is richtext.ListRecordResolver.
func (s *Store) ListRecordName(fileID, recordID string) (json.RawMessage, string, bool) {
	c := s.listRecordRefs()
	key := fileID + "_" + recordID
	c.mu.Lock()
	r, ok := c.recs[key]
	if !ok && !c.seen[key] {
		c.seen[key] = true
		c.pending = append(c.pending, [2]string{fileID, recordID})
	}
	c.mu.Unlock()
	if !ok {
		s.notifyMissing()
	}
	return r.raw, r.state, ok
}

// PutListRecordName records a record's primary field (rich_text) and its
// state ("base", "deleted", "missing").
func (s *Store) PutListRecordName(fileID, recordID string, raw json.RawMessage, state string) {
	c := s.listRecordRefs()
	c.mu.Lock()
	c.recs[fileID+"_"+recordID] = listRecordName{raw: raw, state: state}
	c.mu.Unlock()
}

// ForgetListRecord lets a record be asked for again (a failed lookup).
func (s *Store) ForgetListRecord(fileID, recordID string) {
	c := s.listRecordRefs()
	c.mu.Lock()
	delete(c.seen, fileID+"_"+recordID)
	c.mu.Unlock()
}

// TakeMissingListRecords returns and clears the record lookups that
// missed, as file id and record id pairs.
func (s *Store) TakeMissingListRecords() [][2]string {
	c := s.listRecordRefs()
	c.mu.Lock()
	out := c.pending
	c.pending = nil
	c.mu.Unlock()
	return out
}

func (s *Store) notifyMissing() {
	s.MissingNotify() // makes the channel
	s.missMu.Lock()
	select {
	case s.missNotify <- struct{}{}:
	default:
	}
	s.missMu.Unlock()
}

// FileChannel resolves a file channel that reads as its file
// (richtext.FileChannelResolver; core sets it); nil leaves every file
// channel a channel mention.
var FileChannel func(s *Store, channelID string) (richtext.FileChannel, bool)

// FileChannel is richtext.FileChannelResolver.
func (s *Store) FileChannel(channelID string) (richtext.FileChannel, bool) {
	if FileChannel == nil {
		return richtext.FileChannel{}, false
	}
	return FileChannel(s, channelID)
}
