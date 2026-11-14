package core

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/richtext"
)

// A channel the store can't name, as Slack's web client handles one
// (CDN build of 2026-11-13, chunks read without a session):
//
//   - getChannelById (client-boot 6Z4k OL) answers a channel the store
//     doesn't have with the UnknownFetchManager's stub ({id, isUnknown})
//     and starts the fetch (Gx04 fetchObjectWithId: batched, 200 ms
//     debounce, three tries). Once the fetch answers, the channel is in
//     the store and every component reading it renders again; an id the
//     fetch didn't return becomes a non-existent object ({id,
//     isNonExistent}): a private channel the viewer isn't in, another
//     workspace's.
//   - InlineChannelEntity (client-boot X3qV), which Activity rows (VeYL's
//     destination, PcpC ChannelName), the huddle invite window
//     (HuddleInviteWindowText), the sidebar invite card
//     (useHuddleInviteProps), the huddle preview's title
//     (useGetTitlebarText) and the huddle button's tooltips draw: for a
//     known channel InlineChannelIcon (N4L8: getChannelIconType, the hash,
//     lock-filled for a private channel, archive or archive-filled for an
//     archived one) and ChannelName (g5tH: getDisplayNameForChannel with
//     publicChannelIncludeHashSign off, so the bare name, " (archived)"
//     after an archived one's). For the stub BaseMissingChannel (1x8o)
//     draws MissingText (sSUQ: an 8px base-ter pill, 80, 120 or 160px
//     wide, the tooltip "Loading…") after the hash; for a non-existent
//     channel the lock and "Private channel" (channel_token), no hash.
//   - getDisplayNameForChannel (W+Ke SB) names a channel in text: "#name"
//     for a public one, a private one's bare name; formatChannelName
//     (ZuZy) says "unknown-channel" when there's no name. The huddle
//     invite's notification (sharedHuddleNotificationOptions) names the
//     channel only when it exists (isExistent), so the stub's reads
//     "{memberName} is inviting you to a huddle".
//
// Clack's store can lack a channel the same way: a conversation core
// doesn't have (not the viewer's, or one it hasn't loaded yet), or a stub
// whose payload had no name (named by its id, which is what
// store.ChannelName answers for a channel it can't name). InlineChannel
// is the store's answer for one, and LookupConv the fetch.

// convLookupWait bounds one LookupConv lookup.
const convLookupWait = 30 * time.Second

// convLookupMax is how many LookupConv lookups run at once.
const convLookupMax = 4

// convLookupCoalesce gathers the lookups that end close together into one
// "names" (a feed page's unknown channels), as the resolver's loop does.
const convLookupCoalesce = 100 * time.Millisecond

// convLookups is LookupConv's state.
type convLookups struct {
	mu     sync.Mutex
	m      map[string]bool // id → running (false: done)
	sem    chan struct{}
	done   []string // ended since the last "names"
	queued bool     // a "names" for done is due
}

// LookupConv is getChannelById's fetch for a conversation core can't name
// (named: not in the store, or a stub without its name): ResolveConv
// (conversations.info) on a background goroutine, at most once per id per
// session, then "names" with the id (the lookups ending within
// convLookupCoalesce share one), so the views reading it draw it again.
// A conversation conversations.info doesn't know is marked as one the
// viewer can't see (ResolveConv). pending reports whether the lookup is
// still to finish: until then Slack draws the unknown stub. Safe from any
// goroutine; it never blocks.
func (a *App) LookupConv(id string) (pending bool) {
	if id == "" {
		return false
	}
	c, stored := a.Conv(id)
	if stored && named(c) {
		return false
	}
	if !stored && a.S.ChannelAccess(id) == richtext.ChannelHidden {
		return false // non-existent already (edge channels/info's failed_ids, a lookup)
	}
	l := &a.lookups
	l.mu.Lock()
	running, seen := l.m[id]
	if !seen {
		if l.m == nil {
			l.m, l.sem = map[string]bool{}, make(chan struct{}, convLookupMax)
		}
		l.m[id] = true
	}
	sem := l.sem
	l.mu.Unlock()
	if seen {
		return running
	}
	go func() {
		sem <- struct{}{}
		ctx, cancel := context.WithTimeout(a.bg, convLookupWait)
		a.ResolveConv(ctx, id)
		cancel()
		<-sem
		l.mu.Lock()
		l.m[id] = false
		l.done = append(l.done, id)
		if !l.queued {
			l.queued = true
			time.AfterFunc(convLookupCoalesce, func() {
				l.mu.Lock()
				ids := l.done
				l.done, l.queued = nil, false
				l.mu.Unlock()
				a.emit(Event{Type: "names", Users: ids})
			})
		}
		l.mu.Unlock()
	}()
	return true
}

// ChannelState is what the store has for a channel, as Slack's channel
// entities read it.
type ChannelState uint8

const (
	ChannelKnown       ChannelState = iota // in the store with its name
	ChannelUnknown                         // the unknown stub: its lookup is due or running
	ChannelNonExistent                     // the lookup didn't find it: the viewer can't see it
	ChannelNameless                        // looked up, still without a name (formatChannelName's unknown-channel)
)

// InlineChannel is a channel as InlineChannelEntity and
// getDisplayNameForChannel read it (the header above). Name is set when
// State is ChannelKnown; Private, Archived and Doc when the store knows
// (Doc: a canvas's or list's conversation, "canvas" | "list", whose icon
// getChannelIconType draws instead of the hash or lock).
type InlineChannel struct {
	Name              string
	Private, Archived bool
	Doc               string
	State             ChannelState
}

// InlineChannel is what the store has for channel id (not a DM or a group
// DM, which InlineChannelEntity doesn't draw): its name, or Slack's state
// for a channel it can't name, starting the lookup (LookupConv) for one
// it doesn't know yet. The raw id is never the name. Cheap: safe to call
// while drawing.
func (a *App) InlineChannel(id string) InlineChannel {
	if ic, ok := a.KnownChannel(id); ok {
		return ic
	}
	if a.LookupConv(id) {
		return InlineChannel{State: ChannelUnknown}
	}
	if ic, ok := a.KnownChannel(id); ok { // it landed meanwhile
		return ic
	}
	if c, stored := a.Conv(id); stored {
		return InlineChannel{Private: c.Kind == "private", Archived: c.IsArchived, State: ChannelNameless}
	}
	return InlineChannel{State: ChannelNonExistent} // the fetch didn't return it
}

// KnownChannel is InlineChannel without the lookup: ok false when the
// store can't name id (yet).
func (a *App) KnownChannel(id string) (InlineChannel, bool) {
	if id == "" {
		return InlineChannel{State: ChannelNonExistent}, true
	}
	c, stored := a.Conv(id)
	if stored {
		if !named(c) {
			return InlineChannel{}, false // a stub
		}
		ic := InlineChannel{Name: c.Name, Private: c.Kind == "private", Archived: c.IsArchived, State: ChannelKnown}
		if c.File {
			ic.Doc = c.Doc
		}
		return ic, true
	}
	acc := a.S.ChannelAccess(id)
	if acc == richtext.ChannelHidden {
		return InlineChannel{State: ChannelNonExistent}, true
	}
	// a name seen in search, browse or a mention's lookup (meta.json, edge
	// channels/info), which also asks core's resolver for one it lacks
	if n := a.S.ChannelName(id); n != "" && n != id {
		return InlineChannel{Name: n, Private: acc == richtext.ChannelPrivate || strings.HasPrefix(id, "G"), State: ChannelKnown}, true
	}
	return InlineChannel{}, false
}
