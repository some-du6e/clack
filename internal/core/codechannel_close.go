package core

// Closing stale code channels, and reopening closed ones, ported from
// Slack's web client (CDN build 4d0c78085232, 2026-11-10):
//
//   - checkForOldCodeChannelsToClose (boot-render zuQH et8), behind
//     code_channels_auto_close: two minutes after the client connects
//     (its onConnected side effect, et7) Slack closes up to 10 stale code
//     channels, then every 15 minutes while it stays connected (an idle
//     callback with a 60s timeout) up to 5; a disconnect stops the timers
//     (et9). A code channel is stale when it is a member channel, open
//     (is_open isn't false), not archived, not the primary view, read, has
//     no draft, wasn't opened this session, sits only in Code channels
//     sections (listed there, or there by default), isn't starred, and the
//     newer of its latest message and when it was last reopened is over
//     604800 s (7 days) ago. The most idle close first. Each loses its
//     reopen time and goes through closeChannelApi (boot-shared 6wqB):
//     is_open false at once, then submitClose (client-boot 6ndL,
//     conversations.close {channel}), open again if that fails.
//   - the sidebar (lJBI) lists a record channel only while it is open
//     (Opi6.nT: !!is_open), unless a section lists it, it's starred or
//     temporary, or it is the selected conversation (app.go sections).
//   - record_channel_close (channelClosed) closes it and, behind the
//     experiment, drops its reopen time. record_channel_open
//     (recordChannelOpened) and viewing a closed one (the channel tab
//     view's effect, reason "viewed-record-channel") reopen it with
//     openRecordChannel (boot-render +Kg2): behind the experiment the
//     reopen time (setCodeChannelReopenedAt; the codeChannelReopen store,
//     Uge0, which Slack persists), is_open true, recordChannels.open
//     {channel, reason}, and the channel it answers stored; closed again
//     if the call fails.
//
// Clack learns a code channel's is_open only from those events and its own
// closing and reopening: users.conversations carries no is_open for
// channels. Slack's client reads it from client.channels at boot
// (client-boot dRMY dispatchChannelsDataInner, build 2c0d02ca130f: the
// channel objects' is_open, and under version_all_channels a closable
// channel the answer leaves out counts as closed), which Clack doesn't
// call. So Clack keeps the code channels it knows are closed on disk
// (code_channel_closed.json, next to the reopen times) and reads them
// before the first sidebar: before, every launch listed the closed ones
// again and the closer closed the same channels two minutes later.
//
//   - Only a close the server confirmed is saved: record_channel_close,
//     or conversations.close answering ok. The closer's optimistic close
//     lives in memory until then, so quitting mid-close keeps nothing.
//   - Each entry is a watermark: when Clack last knew the channel closed
//     (the close, then the newest message it heard on the socket, saved
//     on quit). With the socket up, a reopen would have come with that
//     message (record_channel_open). A newer message seen any other way,
//     at boot or in the counts after a socket gap or a poll, is activity
//     Clack wasn't hearing, where it may have missed the reopen, so the
//     entry goes: at worst the channel shows until it is stale again and
//     the closer closes it. Boot checks first; quitting saves the
//     watermarks only after that check ran.
//   - A join is a new membership (Slack's channelJoined upserts the
//     event's channel, is_open with it); leaving, archiving or deleting
//     the channel drops its entry, and so does boot for a channel that
//     isn't the viewer's code channel any more. Sign-out deletes the file
//     (store.DropDisk).
//
// A reopen made elsewhere while Clack wasn't running and without later
// activity shows once record_channel_open comes or the channel is viewed.
// Slack sets "opened this session" only from im_open / mpim_open and for
// IMs it created (setConversationOpen, channelCreated), never for a code
// channel, so that rule never holds one back here either.

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Slack's timings and limits for closing stale code channels (et6, et7).
var (
	codeCloseFirstDelay = 2 * time.Minute  // setTimeout(…, 12e4)
	codeCloseEvery      = 15 * time.Minute // setInterval(…, 9e5)
)

const (
	codeCloseMax       = 5      // et6
	codeCloseFirstMax  = 2 * 5  // the first run: 2 * et6
	codeCloseStaleSecs = 604800 // a week since the latest message or the reopen
	codeReopenFile     = "code_channel_reopen.json"
	codeClosedFile     = "code_channel_closed.json"
)

// codeOpenState is what core knows of code channels' is_open, plus Slack's
// codeChannelReopen store.
type codeOpenState struct {
	mu      sync.Mutex
	closed  map[string]int64 // code channel → is_open false since (the watermark, unix s)
	pending map[string]bool  // closed here, the server's answer not in yet: not saved
	// wrote is each code channel's last close or reopen in this session,
	// at the store journal's position (store/journal.go): Boot's check of
	// the saved set against its answers keeps it (reconcile)
	wrote        map[string]uint64
	moved        bool             // a watermark moved since the last save (noteCodeChannelMessage)
	checked      bool             // Boot's check of the saved set ran (reconcile): quitting may save the watermarks
	closedLoaded bool             // closed read from disk
	reopen       map[string]int64 // codeChannelReopen: code channel → reopenedAt (unix s)
	loaded       bool             // reopen read from disk
	conn         chan bool        // connection changes for the closer (true: connected)
}

var codeOpenStates = newPerApp() // *App → *codeOpenState

func (a *App) codeOpen() *codeOpenState {
	if v, ok := codeOpenStates.Load(a); ok {
		return v.(*codeOpenState)
	}
	v, _ := codeOpenStates.LoadOrStore(a, &codeOpenState{closed: map[string]int64{}, pending: map[string]bool{}, wrote: map[string]uint64{},
		reopen: map[string]int64{}, conn: make(chan bool, 4)})
	return v.(*codeOpenState)
}

// codeChannelsAutoCloseOn is code_channels_auto_close.
func (a *App) codeChannelsAutoCloseOn() bool { return a.experiment("code_channels_auto_close") == "on" }

// codeChannelClosed reports whether core knows the code channel ch is
// closed (is_open false).
func (a *App) codeChannelClosed(ch string) bool {
	st := a.codeOpen()
	st.mu.Lock()
	defer st.mu.Unlock()
	a.loadCodeClosedLocked(st)
	_, ok := st.closed[ch]
	return ok
}

// setCodeChannelOpen records ch's is_open and reports whether it changed.
// confirmed is the server's word (an event, or a call that answered ok):
// only that is saved, at once (off the UI goroutine: closing and reopening
// run on core's); an optimistic change stays in memory until confirmed.
func (a *App) setCodeChannelOpen(ch string, open, confirmed bool) bool {
	st := a.codeOpen()
	st.mu.Lock()
	defer st.mu.Unlock()
	a.loadCodeClosedLocked(st)
	_, closed := st.closed[ch]
	changed := closed == open
	switch {
	case open:
		delete(st.closed, ch)
		delete(st.pending, ch)
	case !closed:
		st.closed[ch] = time.Now().Unix()
	}
	if !open {
		if confirmed {
			delete(st.pending, ch)
		} else if changed {
			st.pending[ch] = true
		}
	}
	if changed { // the sidebar lists it, or doesn't: newer than an answer fetched before
		st.wrote[ch] = a.S.NextSeq()
	}
	if confirmed {
		a.saveCodeClosedLocked(st)
	}
	return changed
}

// saveCodeClosedLocked writes the confirmed closed set. st.mu is held.
func (a *App) saveCodeClosedLocked(st *codeOpenState) {
	m := make(map[string]int64, len(st.closed))
	for id, at := range st.closed {
		if !st.pending[id] {
			m[id] = at
		}
	}
	a.saveCodeStateLocked(codeClosedFile, m)
}

// codeChannelSavedClosed reports whether ch's close is the server's word
// (saved), not one still waiting for its answer.
func (a *App) codeChannelSavedClosed(ch string) bool {
	st := a.codeOpen()
	st.mu.Lock()
	defer st.mu.Unlock()
	a.loadCodeClosedLocked(st)
	_, closed := st.closed[ch]
	return closed && !st.pending[ch]
}

// forgetCodeChannelClosed drops what Clack knows of ch's is_open (a new
// membership, or none: joined, left, archived, deleted) and reports
// whether it was closed.
func (a *App) forgetCodeChannelClosed(ch string) bool {
	st := a.codeOpen()
	st.mu.Lock()
	defer st.mu.Unlock()
	a.loadCodeClosedLocked(st)
	if _, ok := st.closed[ch]; !ok {
		return false
	}
	delete(st.closed, ch)
	delete(st.pending, ch)
	st.wrote[ch] = a.S.NextSeq()
	a.saveCodeClosedLocked(st)
	return true
}

// latestSecs is ch's newest message (channelLatestSecs over the counts).
func (a *App) latestSecs(ch string) int64 {
	e, _ := a.S.Count(ch)
	c, _ := a.S.Conv(ch)
	return channelLatestSecs(e.Latest, c)
}

// noteCodeChannelMessage moves a closed code channel's watermark up to a
// new message (onMessage) when it was heard on the socket (live): a reopen
// would have come over the same socket. Activity Clack learns of any other
// way doesn't move it (see the top of this file).
func (a *App) noteCodeChannelMessage(ch, ts string, live bool) {
	if !live {
		return
	}
	secs := channelLatestSecs(ts, nil)
	if secs <= 0 {
		return
	}
	st := a.codeOpen()
	st.mu.Lock()
	defer st.mu.Unlock()
	a.loadCodeClosedLocked(st)
	if at, ok := st.closed[ch]; ok && secs > at {
		st.closed[ch] = secs
		st.moved = true
	}
}

// checkpointClosedCodeChannels saves the closed set's watermarks (quit:
// Close), once Boot's check of the saved set ran this session: before
// it, nothing this session saw may stand for what the check reads.
func (a *App) checkpointClosedCodeChannels() {
	st := a.codeOpen()
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.checked || !st.moved {
		return
	}
	st.moved = false
	a.saveCodeClosedLocked(st)
}

// codeChannelsChecked reports whether Boot's check of the saved closed set
// ran (reconcileClosedCodeChannels).
func (a *App) codeChannelsChecked() bool {
	st := a.codeOpen()
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.checked
}

// refreshCountsAfterGap is refreshCounts after a stretch Clack wasn't
// hearing the socket (a reconnect, a poll with realtime off): the saved
// closed code channels are checked against the fresh counts again, as at
// boot, since a reopen made meanwhile (record_channel_open) went unheard.
func (a *App) refreshCountsAfterGap(ctx context.Context) {
	since := a.S.Seq()
	a.refreshCounts(ctx)
	if ctx.Err() == nil && a.codeChannelsChecked() && a.reconcileClosedCodeChannels(since) {
		a.emit(Event{Type: "sidebar_changed"})
	}
}

// reconcileClosedCodeChannels is Boot's check of the saved closed set
// against the conversations and counts it fetched (at the store journal's
// position since), run again after a socket gap (refreshCountsAfterGap):
// entries for channels that aren't the viewer's unarchived code channels
// go, and so do those with a message newer than their watermark (see the
// top of this file). A channel closed or reopened here while the calls
// were out (a warm start's window is usable meanwhile) keeps that: it is
// newer than what they fetched. It reports whether an entry went.
func (a *App) reconcileClosedCodeChannels(since uint64) bool {
	st := a.codeOpen()
	st.mu.Lock()
	a.loadCodeClosedLocked(st)
	st.checked = true
	saved := make(map[string]int64, len(st.closed))
	for id, at := range st.closed {
		if !st.pending[id] && st.wrote[id] <= since {
			saved[id] = at
		}
	}
	st.mu.Unlock()
	var gone []string
	for id, at := range saved {
		c, ok := a.S.Conv(id)
		if !ok || c.IsArchived || codeChannelOf(c) == nil || a.latestSecs(id) > at {
			gone = append(gone, id)
		}
	}
	if len(gone) == 0 {
		return false
	}
	st.mu.Lock()
	for _, id := range gone {
		if st.wrote[id] > since { // closed or reopened here since the check read it
			continue
		}
		delete(st.closed, id)
		delete(st.pending, id)
	}
	a.saveCodeClosedLocked(st)
	st.mu.Unlock()
	sort.Strings(gone)
	log.Printf("code channels: %d saved as closed reopened or gone since (%v)", len(gone), gone)
	return true
}

// loadCodeChannelState reads the closed code channels and their reopen
// times from disk (Boot, before the first sidebar).
func (a *App) loadCodeChannelState() {
	st := a.codeOpen()
	st.mu.Lock()
	defer st.mu.Unlock()
	a.loadCodeClosedLocked(st)
	a.loadCodeReopenLocked(st)
}

// loadCodeClosedLocked reads the closed set once. st.mu is held.
func (a *App) loadCodeClosedLocked(st *codeOpenState) {
	if st.closedLoaded {
		return
	}
	st.closedLoaded = true
	for k, v := range a.readCodeState(codeClosedFile) {
		if _, ok := st.closed[k]; !ok { // what this session saw already wins
			st.closed[k] = v // saved: the server's word
		}
	}
}

// ---- codeChannelReopen (Uge0), persisted next to the account's cache ----

func (a *App) codeReopenPath() string { return a.codeStatePath(codeReopenFile) }

// codeStatePath is name in the account's cache directory ("" without one).
func (a *App) codeStatePath(name string) string {
	dir := a.S.CacheDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, name)
}

// readCodeState reads one of the code channel maps (channel → unix s).
func (a *App) readCodeState(name string) map[string]int64 {
	p := a.codeStatePath(name)
	if p == "" || a.S.Dropped() {
		return nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var m map[string]int64
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}

// saveCodeStateLocked writes one of the code channel maps. The codeOpen
// mutex is held; it runs on core's goroutines (never the UI's).
func (a *App) saveCodeStateLocked(name string, m map[string]int64) {
	p := a.codeStatePath(name)
	if p == "" || a.S.Dropped() {
		return
	}
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return
	}
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, p)
	}
}

// loadCodeReopenLocked reads the reopen times once. st.mu is held.
func (a *App) loadCodeReopenLocked(st *codeOpenState) {
	if st.loaded {
		return
	}
	st.loaded = true
	for k, v := range a.readCodeState(codeReopenFile) {
		st.reopen[k] = v
	}
}

// saveCodeReopenLocked writes the reopen times. st.mu is held; it runs on
// core's goroutines (never the UI's).
func (a *App) saveCodeReopenLocked(st *codeOpenState) {
	a.saveCodeStateLocked(codeReopenFile, st.reopen)
}

// codeChannelReopenedAt is codeChannelReopen[ch] (0 when never reopened).
func (a *App) codeChannelReopenedAt(ch string) int64 {
	st := a.codeOpen()
	st.mu.Lock()
	defer st.mu.Unlock()
	a.loadCodeReopenLocked(st)
	return st.reopen[ch]
}

// setCodeChannelReopenedAt is setCodeChannelReopenedAt.
func (a *App) setCodeChannelReopenedAt(ch string, at int64) {
	st := a.codeOpen()
	st.mu.Lock()
	defer st.mu.Unlock()
	a.loadCodeReopenLocked(st)
	st.reopen[ch] = at
	a.saveCodeReopenLocked(st)
}

// clearCodeChannelReopenedAt is clearCodeChannelReopenedAt.
func (a *App) clearCodeChannelReopenedAt(ch string) {
	st := a.codeOpen()
	st.mu.Lock()
	defer st.mu.Unlock()
	a.loadCodeReopenLocked(st)
	if _, ok := st.reopen[ch]; !ok {
		return
	}
	delete(st.reopen, ch)
	a.saveCodeReopenLocked(st)
}

// ---- the closer (et7 / et8 / et9) ----

// codeChannelsConnection tells the closer the realtime socket came up or
// went down (Slack's onConnected / onDisconnected side effects).
func (a *App) codeChannelsConnection(up bool) {
	select {
	case a.codeOpen().conn <- up:
	default: // the closer is behind; the latest state arrives with the next change
	}
}

// codeChannelCloserLoop runs checkForOldCodeChannelsToClose on Slack's
// schedule: the first check codeCloseFirstDelay after Clack is connected
// (Boot: the socket or, with realtime off, polling), then every
// codeCloseEvery; a disconnect stops it and a reconnect starts it over.
func (a *App) codeChannelCloserLoop(ctx context.Context) {
	st := a.codeOpen()
	first := time.NewTimer(codeCloseFirstDelay)
	defer first.Stop()
	var tick *time.Ticker
	var tickC <-chan time.Time
	stopTick := func() {
		if tick != nil {
			tick.Stop()
			tick, tickC = nil, nil
		}
	}
	defer stopTick()
	for {
		select {
		case <-ctx.Done():
			return
		case up := <-st.conn:
			stopTick()
			first.Stop()
			if up {
				first.Reset(codeCloseFirstDelay)
			}
		case <-first.C:
			a.closeStaleCodeChannels(ctx, codeCloseFirstMax)
			stopTick()
			tick = time.NewTicker(codeCloseEvery)
			tickC = tick.C
		case <-tickC:
			a.closeStaleCodeChannels(ctx, codeCloseMax)
		}
	}
}

// staleCodeChannels is checkForOldCodeChannelsToClose's pick: the code
// channels to close now, most idle first, at most limit.
func (a *App) staleCodeChannels(limit int, now time.Time) []string {
	if !a.codeChannelsAutoCloseOn() {
		return nil
	}
	a.S.RLock()
	hydrated := len(a.S.Sections) > 0 // "Skipping auto-close: channel sections not hydrated"
	sections := append([]slack.Section(nil), a.S.Sections...)
	a.S.RUnlock()
	if !hydrated {
		return nil
	}
	listed := map[string][]string{} // channel → the types of the sections listing it
	for _, s := range sections {
		for _, id := range s.ChannelIDsPage.ChannelIDs {
			listed[id] = append(listed[id], s.Type)
		}
	}
	a.mu.Lock()
	starred := map[string]bool{}
	for id := range a.starred {
		starred[id] = true
	}
	a.mu.Unlock()
	sel := a.selectedID()
	// a Code channels section to file it in: a persisted one, or the one
	// the client makes while session_channels is on (local_coding_channels)
	codeSection := a.sessionChannelsOn() || hasSectionType(sections, "coding_channels")
	type cand struct {
		id   string
		idle float64
	}
	var out []cand
	for _, id := range a.S.ConvIDs() {
		c, ok := a.S.Conv(id) // the viewer's conversations: member channels
		if !ok || c.IsArchived || codeChannelOf(c) == nil || a.codeChannelClosed(id) {
			continue
		}
		if id == sel { // "Is primary view"
			continue
		}
		e, _ := a.S.Count(id)
		if e.HasUnreads { // "Is unread"
			continue
		}
		if _, ok := a.DraftFor(id, ""); ok { // "Has draft"
			continue
		}
		// "Not in Code Channels section": the sections listing it, else the
		// section of the type getChannelSectionType (uSoP) gives it,
		// Starred for a starred one and Code channels for any other code
		// channel, when the sidebar has such a section (/yt0 nv)
		if types := listed[id]; len(types) > 0 {
			only := true
			for _, t := range types {
				only = only && t == "coding_channels"
			}
			if !only {
				continue
			}
		} else if starred[id] || !codeSection {
			continue
		}
		if starred[id] { // "Starred"
			continue
		}
		latest := channelLatestSecs(e.Latest, c)
		last := max(latest, a.codeChannelReopenedAt(id))
		if last <= 0 || now.Unix()-last <= codeCloseStaleSecs { // "Not stale"
			continue
		}
		idle := 0.0
		if latest > 0 {
			idle = float64(now.Unix() - latest)
		}
		out = append(out, cand{id, idle})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].idle > out[j].idle })
	if len(out) > limit {
		out = out[:limit]
	}
	ids := make([]string, len(out))
	for i, c := range out {
		ids[i] = c.id
	}
	return ids
}

// channelLatestSecs is channelLatestMessageTs (client-boot f95g.Nf): the
// latest message's ts in seconds, else the channel's updated time (ms or
// s; Clack's conversations don't keep it), else its creation time.
func channelLatestSecs(latest string, c *slack.Conversation) int64 {
	if i := strings.IndexByte(latest, '.'); i >= 0 {
		latest = latest[:i]
	}
	if n, err := strconv.ParseInt(latest, 10, 64); err == nil && n > 0 {
		return n
	}
	if c == nil {
		return 0
	}
	return c.Created
}

// closeStaleCodeChannels runs checkForOldCodeChannelsToClose once.
func (a *App) closeStaleCodeChannels(ctx context.Context, limit int) {
	ids := a.staleCodeChannels(limit, time.Now())
	if len(ids) == 0 {
		return
	}
	var errs []string
	for _, id := range ids {
		a.clearCodeChannelReopenedAt(id)
		if err := a.CloseCodeChannel(ctx, id); err != nil {
			errs = append(errs, id+": "+err.Error())
		}
	}
	log.Printf("code channels: closed %v (errs %v)", ids, errs)
}

// CloseCodeChannel is closeChannelApi for a code channel: closed in the
// sidebar at once, then conversations.close {channel}; open again when the
// call fails. The close is saved only once the server answered ok: a quit
// while it is in flight (the call never sent) leaves nothing on disk.
func (a *App) CloseCodeChannel(ctx context.Context, ch string) error {
	if a.setCodeChannelOpen(ch, false, false) {
		a.emit(Event{Type: "sidebar_changed"})
	}
	err := a.C.Call(ctx, "conversations.close", url.Values{"channel": {ch}}, nil)
	switch {
	case err == nil:
		a.setCodeChannelOpen(ch, false, true)
	case ctx.Err() == nil:
		if a.setCodeChannelOpen(ch, true, false) {
			a.emit(Event{Type: "sidebar_changed"})
		}
	}
	return err
}

// onRecordChannelClose is record_channel_close (channelClosed): the code
// channel is closed and, behind code_channels_auto_close, its reopen time
// goes.
func (a *App) onRecordChannelClose(ch string) {
	c, ok := a.S.Conv(ch)
	if !ok {
		return // "Unknown channel"
	}
	changed := a.setCodeChannelOpen(ch, false, true)
	if codeChannelOf(c) != nil && a.codeChannelsAutoCloseOn() {
		a.clearCodeChannelReopenedAt(ch)
	}
	if changed {
		a.emit(Event{Type: "sidebar_changed"})
	}
}

// onRecordChannelOpen is record_channel_open (recordChannelOpened): a
// closed code channel reopens.
func (a *App) onRecordChannelOpen(ctx context.Context, ch string) {
	if a.codeChannelClosed(ch) {
		_ = a.OpenRecordChannel(ctx, ch, "record-channel-opened-rtm")
	}
}

// maybeReopenViewedCodeChannel is the channel tab view's effect: viewing a
// closed code channel the viewer is in reopens it.
func (a *App) maybeReopenViewedCodeChannel(ch string) {
	c, ok := a.S.Conv(ch)
	if !ok || codeChannelOf(c) == nil || !a.codeChannelClosed(ch) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.bg, 30*time.Second)
		defer cancel()
		_ = a.OpenRecordChannel(ctx, ch, "viewed-record-channel")
	}()
}

// OpenRecordChannel is openRecordChannel for a code channel: behind
// code_channels_auto_close its reopen time is now; it is open at once;
// recordChannels.open {channel, reason} answers the channel, which is
// stored; when the call fails it is closed again.
func (a *App) OpenRecordChannel(ctx context.Context, ch, reason string) error {
	c, ok := a.S.Conv(ch)
	if ok && codeChannelOf(c) != nil && a.codeChannelsAutoCloseOn() {
		a.setCodeChannelReopenedAt(ch, time.Now().Unix())
	}
	saved := a.codeChannelSavedClosed(ch)
	if a.setCodeChannelOpen(ch, true, false) {
		a.emit(Event{Type: "sidebar_changed"})
	}
	var res struct {
		Channel json.RawMessage `json:"channel"`
	}
	if err := a.C.Call(ctx, "recordChannels.open", url.Values{"channel": {ch}, "reason": {reason}}, &res); err != nil {
		if a.setCodeChannelOpen(ch, false, saved) { // as it was
			a.emit(Event{Type: "sidebar_changed"})
		}
		return err
	}
	a.setCodeChannelOpen(ch, true, true)
	if len(res.Channel) > 0 && string(res.Channel) != "null" {
		if a.mergeChannel(ch, res.Channel) {
			if v, ok := a.Conv(ch); ok {
				a.emit(Event{Type: "conv", Channel: ch, Conv: &v})
			}
		}
	}
	return nil
}

// mergeChannel merges a channel object the server sent into the stored
// one (Slack's upsertChannel), keeping what the answer leaves out.
func (a *App) mergeChannel(ch string, raw json.RawMessage) bool {
	var up map[string]any
	if json.Unmarshal(raw, &up) != nil || len(up) == 0 {
		return false
	}
	delete(up, "is_open") // tracked by codeOpen
	cur, ok := a.S.Conv(ch)
	if !ok {
		return false
	}
	b, err := json.Marshal(cur)
	if err != nil {
		return false
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return false
	}
	mergeJSON(m, up)
	b, err = json.Marshal(m)
	if err != nil {
		return false
	}
	var next slack.Conversation
	if json.Unmarshal(b, &next) != nil {
		return false
	}
	next.ID = ch
	a.S.PutConvLocal(next)
	return true
}
