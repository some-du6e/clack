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
// channels (Slack's client reads client.userBoot's), so a code channel
// counts as open until Clack sees it closed. Slack sets "opened this
// session" only from im_open / mpim_open and for IMs it created
// (setConversationOpen, channelCreated), never for a code channel, so that
// rule never holds one back here either.

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
)

// codeOpenState is what core knows of code channels' is_open, plus Slack's
// codeChannelReopen store.
type codeOpenState struct {
	mu     sync.Mutex
	closed map[string]bool  // code channel → known closed (is_open false)
	reopen map[string]int64 // codeChannelReopen: code channel → reopenedAt (unix s)
	loaded bool             // reopen read from disk
	conn   chan bool        // connection changes for the closer (true: connected)
}

var codeOpenStates sync.Map // *App → *codeOpenState

func (a *App) codeOpen() *codeOpenState {
	if v, ok := codeOpenStates.Load(a); ok {
		return v.(*codeOpenState)
	}
	v, _ := codeOpenStates.LoadOrStore(a, &codeOpenState{closed: map[string]bool{}, reopen: map[string]int64{}, conn: make(chan bool, 4)})
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
	return st.closed[ch]
}

// setCodeChannelOpen records ch's is_open and reports whether it changed.
func (a *App) setCodeChannelOpen(ch string, open bool) bool {
	st := a.codeOpen()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.closed[ch] == !open {
		return false
	}
	if open {
		delete(st.closed, ch)
	} else {
		st.closed[ch] = true
	}
	return true
}

// ---- codeChannelReopen (Uge0), persisted next to the account's cache ----

func (a *App) codeReopenPath() string {
	dir := a.S.CacheDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, codeReopenFile)
}

// loadCodeReopenLocked reads the reopen times once. st.mu is held.
func (a *App) loadCodeReopenLocked(st *codeOpenState) {
	if st.loaded {
		return
	}
	st.loaded = true
	p := a.codeReopenPath()
	if p == "" || a.S.Dropped() {
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var m map[string]int64
	if json.Unmarshal(b, &m) == nil {
		for k, v := range m {
			st.reopen[k] = v
		}
	}
}

// saveCodeReopenLocked writes the reopen times. st.mu is held; it runs on
// core's goroutines (never the UI's).
func (a *App) saveCodeReopenLocked(st *codeOpenState) {
	p := a.codeReopenPath()
	if p == "" || a.S.Dropped() {
		return
	}
	b, err := json.Marshal(st.reopen)
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
	if len(ids) > 0 {
		log.Printf("code channels: closing %d stale code channels", len(ids))
	}
	for _, id := range ids {
		a.clearCodeChannelReopenedAt(id)
		_ = a.CloseCodeChannel(ctx, id)
	}
}

// CloseCodeChannel is closeChannelApi for a code channel: closed in the
// sidebar at once, then conversations.close {channel}; open again when the
// call fails.
func (a *App) CloseCodeChannel(ctx context.Context, ch string) error {
	if a.setCodeChannelOpen(ch, false) {
		a.emit(Event{Type: "sidebar_changed"})
	}
	err := a.C.Call(ctx, "conversations.close", url.Values{"channel": {ch}}, nil)
	if err != nil && ctx.Err() == nil {
		if a.setCodeChannelOpen(ch, true) {
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
	changed := a.setCodeChannelOpen(ch, false)
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
	if a.setCodeChannelOpen(ch, true) {
		a.emit(Event{Type: "sidebar_changed"})
	}
	var res struct {
		Channel json.RawMessage `json:"channel"`
	}
	if err := a.C.Call(ctx, "recordChannels.open", url.Values{"channel": {ch}, "reason": {reason}}, &res); err != nil {
		if a.setCodeChannelOpen(ch, false) {
			a.emit(Event{Type: "sidebar_changed"})
		}
		return err
	}
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
	a.S.PutConv(next)
	return true
}
