package views

// Saved's and To-dos' messages as they are now.
//
// Slack's saved rows read their message from the messages store as they
// render (LaterItemRow: getMessageByTimestamp on the item's channel and
// ts), so an edit, an app's chat.update, a reaction or a deletion shows
// in them in the commit it lands in. Clack's pages read saved.list again
// two seconds after an update to a listed message. They take core's
// changes into their items now (core's MessageChangedSince and
// MessageChange, read before the frame draws), with no reload; a page
// that lands takes what changed after its request went out.

import "github.com/some-du6e/clack/internal/core"

// applyLive takes core's message changes into the saved messages
// (applyFeeds). Further behind than core names, the pages are read
// again.
func (s *savedBase) applyLive() {
	c := s.h.Core()
	if c != nil {
		defer func() {
			for ch, tss := range s.refetch {
				c.RefetchMessages(ch, tss)
			}
			clear(s.refetch)
		}()
	}
	keys, prev, ok := s.lv.read(c)
	if !ok {
		for _, f := range s.feeds {
			if f.first.started() {
				s.reload(f)
			}
		}
		return
	}
	for i, k := range keys {
		if i > 0 && k == keys[i-1] {
			continue
		}
		ch, ok := c.MessageChange(k.Channel, k.TS)
		if !ok {
			continue
		}
		for _, f := range s.feeds {
			s.takeChange(c, f, k, &ch, prev)
		}
	}
}

// takeChange applies change ch of k to f's items whose copy is current
// as of have: the change's copy (a reactions change merges into the
// item's), a deletion (or Slack's "gone") as Later's tombstone (Gone). A
// change no copy is left of (the store let the message go, and core's
// copy of the change with it) is fetched again by id, as Slack's row
// fetches a message its store doesn't hold: it comes back as a change.
func (s *savedBase) takeChange(c *core.App, f *savedFeed, k core.MsgKey, ch *core.MsgChange, have uint64) {
	for j := range f.items {
		it := &f.items[j]
		if it.ItemType != "message" || it.Channel != k.Channel || it.TS != k.TS {
			continue
		}
		if ch.Deleted {
			it.Msg, it.Gone = nil, true
		} else if m, ok := c.ChangedMessage(k.Channel, k.TS, ch, it.Msg, have); ok {
			it.Msg, it.Gone = &m, false
		} else {
			if s.refetch == nil {
				s.refetch = map[string][]string{}
			}
			s.refetch[k.Channel] = append(s.refetch[k.Channel], k.TS)
			continue
		}
		s.dirty = true
	}
}

// landed takes into a page that just landed what core changed of its
// messages after its request went out (at change from): its answer was
// read before them.
func (s *savedBase) landed(f *savedFeed, from uint64) {
	c := s.h.Core()
	if c == nil {
		return
	}
	for j := range f.items {
		it := &f.items[j]
		if it.ItemType != "message" || it.Channel == "" || it.TS == "" {
			continue
		}
		if ch, ok := c.MessageChange(it.Channel, it.TS); ok && ch.Seq > from {
			s.takeChange(c, f, core.MsgKey{Channel: it.Channel, TS: it.TS}, &ch, from)
		}
	}
}
