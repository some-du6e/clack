package core

// The channel Pins tab's data: Slack's pins store and its fetch, as the
// web client wires them (build 859cadc0e0ab, read from the public CDN
// without a cookie; docs/CHANNELS-AUDIT.md "Pins tab"):
//
//   - The store (client-boot vB6l, storeKey "pins") keeps per channel the
//     pinned items' identifiers (pinsByChannel: {type, channel, ts} for a
//     message, {type, id} for a file) and a loading state
//     (loadingStateByChannel). The rows read each message from the
//     messages store (iR00 Ld) and each file from the files store, so an
//     edit or a reaction shows in the row.
//   - usePinsV2 (boot-shared nihB) reads getPinnedItemsForChannel (zaZU
//     YG) with refetchIfNeeded: the pins refetcher (HZu1, LoZG
//     ConditionalFetchManager with no maxAge, so an id stays fresh until a
//     slow reconnect marks it stale) runs clearPinnedItems and then
//     fetchPinsList (fI9Z) the first time a channel's pins are read.
//   - fetchPinsList: setLoadingState LOADING, pins.list {channel, reason
//     "fetchPinsList"} (htKN xs → bm9n pinsListFetcher), the messages and
//     files into their stores, addPinnedItems in the answer's order (each
//     identifier once), LOADED; on failure NOT_LOADED.
//   - The refetcher's freshness (LoZG _maybeResetState): every connect
//     that isn't a fast reconnect bumps the socket store's
//     slowConnectionCount (client-boot QXYt connected(isFast), the socket
//     manager's FAST_RECONNECTING); when the count differs from the one the
//     refetcher last saw (1 at first), every channel's pins are stale
//     (markStaleAfterSlowReconnect defaults to yes). The next read, which
//     for the open tab is the next store update since useSelector reruns
//     on every dispatch, runs clearPinnedItems and fetchPinsList again.
//   - pin_added / pin_removed (boot-render pinAdded / pinRemoved): while
//     the channel's pins are LOADED, prependPinnedItem (the new pin first,
//     unless it's there) or removePinnedItem.
//   - usePinsV2 drops file_comment pins and makes each item a row:
//     PIN_FILE for a file, PIN_MESSAGE otherwise; isLoading is any state
//     but LOADED, isEmpty no items.
//   - unpinMessage (4mNC): removePinnedItem and the message's pin cleared
//     at once, then pins.remove {channel, timestamp, reason
//     "unpin-message"} (htKN Rs); a failure puts both back (addPinnedItems:
//     at the end). The menu's Remove pin on a file only runs
//     removePinnedItem (usePinsListMenu: vB6l us, no API call).

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// PinsTabItem is one of a channel's pinned items (a usePinsV2 row).
type PinsTabItem struct {
	Type      string // "message" (PIN_MESSAGE) or "file" (PIN_FILE)
	Channel   string // a message's channel
	TS        string // a message's ts
	FileID    string // a file's id
	Created   int64  // when it was pinned
	CreatedBy string // who pinned it
	Msg       *Message
	File      *File
}

// Key is the row's key (usePinsV2: id ?? ts ?? fileId).
func (it PinsTabItem) Key() string {
	if it.Type == "file" {
		return it.FileID
	}
	return it.TS
}

func (it PinsTabItem) same(o PinsTabItem) bool { // arePinsEqual
	if it.Type == "message" || o.Type == "message" {
		return it.Type == o.Type && it.TS == o.TS && it.Channel == o.Channel
	}
	return it.FileID == o.FileID
}

const (
	pinsNotLoaded = iota
	pinsLoading
	pinsLoaded
)

type pinsTabState struct {
	mu   sync.Mutex
	chs  map[string]*pinsTabChan
	seen map[string]bool // fetched once (the refetcher's fresh ids)
	// slow is the slowConnectionCount the refetcher last saw (0: none yet)
	slow int
}

type pinsTabChan struct {
	state int
	items []PinsTabItem
}

var pinsTabs sync.Map // *App -> *pinsTabState

func (a *App) pinsTab() *pinsTabState {
	v, _ := pinsTabs.LoadOrStore(a, &pinsTabState{chs: map[string]*pinsTabChan{}, seen: map[string]bool{}})
	return v.(*pinsTabState)
}

// PinsTab is usePinsV2 for ch: the rows (newest pin first, as pins.list
// and prependPinnedItem order them) and whether they're loaded. The first
// read of a channel fetches its pins (the pins refetcher); "pins_tab"
// events tell when they change.
func (a *App) PinsTab(ch string) (items []PinsTabItem, loaded bool) {
	st := a.pinsTab()
	st.mu.Lock()
	a.pinsResetAfterSlowReconnectLocked(st)
	if !st.seen[ch] {
		st.seen[ch] = true
		st.chs[ch] = &pinsTabChan{state: pinsLoading} // clearPinnedItems, setLoadingState LOADING
		st.mu.Unlock()
		go a.fetchPinsTab(ch)
		return nil, false
	}
	c := st.chs[ch]
	if c == nil {
		st.mu.Unlock()
		return nil, false
	}
	items = slices.Clone(c.items)
	loaded = c.state == pinsLoaded
	st.mu.Unlock()
	// the rows read the messages store: a newer copy wins
	for i := range items {
		if items[i].Type == "message" {
			if m, ok := a.liveMessage(items[i].Channel, items[i].TS); ok {
				items[i].Msg = m
			}
		}
	}
	return items, loaded
}

// liveMessage is the timeline or thread's copy of ch/ts, when cached.
func (a *App) liveMessage(ch, ts string) (*Message, bool) {
	m, ok := a.S.Find(ch, ts)
	if !ok {
		return nil, false
	}
	return a.messagePtr(ch, &m), true
}

// slowConnections is the socket store's slowConnectionCount.
var slowConnections sync.Map // *App → *atomic.Int64

func (a *App) slowConnectionCount() *atomic.Int64 {
	v, _ := slowConnections.LoadOrStore(a, new(atomic.Int64))
	return v.(*atomic.Int64)
}

// socketConnected is the socket store's connected(isFast): a slow
// connect bumps slowConnectionCount. The open Pins tabs read again (the
// store update reruns their selectors), and so refetch when the count
// moved.
func (a *App) socketConnected(fast bool) {
	if fast {
		return
	}
	a.slowConnectionCount().Add(1)
	v, ok := pinsTabs.Load(a)
	if !ok {
		return
	}
	st := v.(*pinsTabState)
	st.mu.Lock()
	chs := make([]string, 0, len(st.chs))
	for ch := range st.chs {
		chs = append(chs, ch)
	}
	st.mu.Unlock()
	for _, ch := range chs {
		a.emit(Event{Type: "pins_tab", Channel: ch})
	}
}

// pinsResetAfterSlowReconnectLocked is _maybeResetState: a
// slowConnectionCount other than the one last seen marks every channel's
// pins stale. st.mu is held.
func (a *App) pinsResetAfterSlowReconnectLocked(st *pinsTabState) {
	if st.slow == 0 {
		st.slow = 1
	}
	n := max(int(a.slowConnectionCount().Load()), 1)
	if n != st.slow {
		st.slow = n
		clear(st.seen)
	}
}

// RefetchPinsTab marks ch's pins stale, so the next read fetches them
// again (markObjectsAsStale).
func (a *App) RefetchPinsTab(ch string) {
	st := a.pinsTab()
	st.mu.Lock()
	delete(st.seen, ch)
	st.mu.Unlock()
}

type pinsListItem struct {
	Type      string          `json:"type"`
	Channel   string          `json:"channel"`
	Created   int64           `json:"created"`
	CreatedBy string          `json:"created_by"`
	Message   *slack.Message  `json:"message"`
	File      *browseFileRaw  `json:"file"`
	Comment   json.RawMessage `json:"comment"`
}

// pinsTabItemOf is an API item as a row; ok false for what usePinsV2
// drops (file_comment) or can't key.
func (a *App) pinsTabItemOf(ch string, it pinsListItem) (PinsTabItem, bool) {
	switch it.Type {
	case "message":
		if it.Message == nil || it.Message.TS == "" {
			return PinsTabItem{}, false
		}
		c := firstNonEmpty(it.Channel, ch)
		return PinsTabItem{Type: "message", Channel: c, TS: it.Message.TS, Created: it.Created, CreatedBy: it.CreatedBy,
			Msg: a.messagePtr(c, it.Message)}, true
	case "file":
		if it.File == nil || it.File.ID == "" {
			return PinsTabItem{}, false
		}
		f := it.File.file()
		return PinsTabItem{Type: "file", FileID: f.ID, Created: it.Created, CreatedBy: it.CreatedBy, File: &f}, true
	}
	return PinsTabItem{}, false
}

// fetchPinsTab is fetchPinsList.
func (a *App) fetchPinsTab(ch string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var res struct {
		Items []pinsListItem `json:"items"`
	}
	err := a.C.Call(ctx, "pins.list", url.Values{"channel": {ch}, "reason": {"fetchPinsList"}}, &res)
	st := a.pinsTab()
	st.mu.Lock()
	c := st.chs[ch]
	if c == nil {
		c = &pinsTabChan{}
		st.chs[ch] = c
	}
	if err != nil {
		log.Printf("pins tab %s: pins.list: %v", ch, err)
		c.state = pinsNotLoaded
		delete(st.seen, ch) // the next read tries again
		st.mu.Unlock()
		a.emit(Event{Type: "pins_tab", Channel: ch})
		return
	}
	for _, raw := range res.Items {
		it, ok := a.pinsTabItemOf(ch, raw)
		if !ok {
			continue
		}
		if !slices.ContainsFunc(c.items, it.same) { // addPinnedItems: each once
			c.items = append(c.items, it)
		}
	}
	c.state = pinsLoaded
	st.mu.Unlock()
	a.emit(Event{Type: "pins_tab", Channel: ch})
}

// pinsTabEvent is pinAdded / pinRemoved's store step: while ch's pins are
// loaded, the item goes first (prependPinnedItem) or out
// (removePinnedItem).
func (a *App) pinsTabEvent(ch string, added bool, raw pinsListItem) {
	it, ok := a.pinsTabItemOf(ch, raw)
	if !ok {
		return
	}
	st := a.pinsTab()
	st.mu.Lock()
	c := st.chs[ch]
	if c == nil || c.state != pinsLoaded {
		st.mu.Unlock()
		return
	}
	i := slices.IndexFunc(c.items, it.same)
	switch {
	case added && i < 0:
		c.items = append([]PinsTabItem{it}, c.items...)
	case !added && i >= 0:
		c.items = slices.Delete(slices.Clone(c.items), i, i+1)
	default:
		st.mu.Unlock()
		return
	}
	st.mu.Unlock()
	a.emit(Event{Type: "pins_tab", Channel: ch})
}

// pinsTabSee keeps the rows' message copies current from the events
// that carry one (update), and drops a deleted message's row: Slack's
// rows read the messages store, where an edit, a reaction or a delete
// lands.
func (a *App) pinsTabSee(ev Event) {
	if ev.Type != "update" && ev.Type != "delete" || ev.Channel == "" || ev.TS == "" {
		return
	}
	v, ok := pinsTabs.Load(a)
	if !ok {
		return
	}
	st := v.(*pinsTabState)
	st.mu.Lock()
	c := st.chs[ev.Channel]
	if c == nil {
		st.mu.Unlock()
		return
	}
	i := slices.IndexFunc(c.items, func(it PinsTabItem) bool { return it.Type == "message" && it.TS == ev.TS })
	if i < 0 {
		st.mu.Unlock()
		return
	}
	c.items = slices.Clone(c.items)
	if ev.Type == "delete" {
		c.items = slices.Delete(c.items, i, i+1)
	} else if ev.Msg != nil {
		m := *ev.Msg
		c.items[i].Msg = &m
	}
	st.mu.Unlock()
	if ev.Type == "delete" {
		go a.emit(Event{Type: "pins_tab", Channel: ev.Channel})
	}
}

// UnpinFromTab is the Pins tab menu's Remove pin. A message: unpinMessage
// (the row and the message's pin go at once, pins.remove {channel,
// timestamp, reason "unpin-message"}, both back on failure). A file:
// removePinnedItem alone.
func (a *App) UnpinFromTab(ctx context.Context, it PinsTabItem) error {
	ch := it.Channel
	st := a.pinsTab()
	st.mu.Lock()
	for k, c := range st.chs {
		if it.Type == "file" || k == ch {
			if i := slices.IndexFunc(c.items, it.same); i >= 0 {
				c.items = slices.Delete(slices.Clone(c.items), i, i+1)
				if ch == "" {
					ch = k
				}
			}
		}
	}
	st.mu.Unlock()
	a.emit(Event{Type: "pins_tab", Channel: ch})
	if it.Type != "message" {
		return nil
	}
	var prev *slack.PinnedInfo
	if m, ok := a.S.Find(ch, it.TS); ok {
		prev = m.PinnedInfo
		if mm, ok := a.S.SetPin(ch, it.TS, false, nil); ok {
			a.emitUpdate(ch, &mm)
		}
	}
	err := a.C.Call(ctx, "pins.remove", url.Values{"channel": {ch}, "timestamp": {it.TS}, "reason": {"unpin-message"}}, nil)
	if err == nil || isCode(err, "no_pin") {
		return nil
	}
	st.mu.Lock()
	if c := st.chs[ch]; c != nil && !slices.ContainsFunc(c.items, it.same) {
		c.items = append(slices.Clone(c.items), it) // addPinnedItems: at the end
	}
	st.mu.Unlock()
	if mm, ok := a.S.SetPin(ch, it.TS, true, prev); ok {
		a.emitUpdate(ch, &mm)
	}
	a.emit(Event{Type: "pins_tab", Channel: ch})
	return err
}

// CanEditTabs: channel_managers.go.

// CanEditBookmarks is the folder menus' gate: canUserPostInChannel and
// isMemberOfChannel (o/IU c, JUrK bl).
func (a *App) CanEditBookmarks(ch string) bool {
	c, ok := a.S.Conv(ch)
	if !ok {
		if c, ok = a.S.ExtConv(ch); !ok {
			return false
		}
	}
	return a.isMemberConv(c) && !c.IsArchived
}
