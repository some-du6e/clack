package views

// Saved's and To-dos' messages as they are now.
//
// Slack's saved rows read their message from the messages store as they
// render (LaterItemRow: getMessageByTimestamp on the item's channel and
// ts), so an edit, an app's chat.update, a reaction or a deletion shows
// in them in the commit it lands in. Clack's pages read saved.list again
// two seconds after an update to a listed message. They take core's
// changes into their items now (its log of message changes, read before
// the frame draws), with no reload; a page that lands after a change
// takes the changes made since its request went out.

import "github.com/some-du6e/clack/internal/core"

// applyLive takes core's message changes into the saved messages
// (applyFeeds). More than core's log keeps went by unread: the pages are
// read again.
func (s *savedBase) applyLive() {
	evs, ok := s.lv.read(s.h.Core())
	if !ok {
		for _, f := range s.feeds {
			if f.first.started() {
				s.reload(f)
			}
		}
		return
	}
	for _, f := range s.feeds {
		s.takeChanges(f, evs)
	}
}

// takeChanges applies message changes to f's items: an update's copy (a
// reactions update merges into the item's), a deletion as Later's
// tombstone (Gone).
func (s *savedBase) takeChanges(f *savedFeed, evs []core.Event) {
	for i := range evs {
		ev := &evs[i]
		for j := range f.items {
			it := &f.items[j]
			if it.ItemType != "message" || it.Channel != ev.Channel || it.TS != ev.TS {
				continue
			}
			if ev.Type == "delete" {
				it.Msg, it.Gone = nil, true
			} else {
				m := msgAfter(it.Msg, ev)
				it.Msg, it.Gone = &m, false
			}
			s.dirty = true
		}
	}
}

// landed takes into a page that just landed the changes core made since
// its request went out (at log position from): its answer was read
// before them.
func (s *savedBase) landed(f *savedFeed, from uint64) {
	c := s.h.Core()
	if c == nil || !s.lv.started || from >= s.lv.seen {
		return
	}
	if evs, _, ok := c.MessageChanges(from); ok {
		s.takeChanges(f, evs)
	}
}
