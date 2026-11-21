package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/some-du6e/clack/internal/applog"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// App wires the Slack client, the store and the realtime connection.
// All methods are safe for concurrent use.
type App struct {
	C   *slack.Client
	S   *store.Store
	RTM *slack.RTM

	usersAsked usersAsked // ids users/info was asked for (lazy_users.go)
	usersOut   usersOut   // the members users/info has out (members.go)

	fetchedHook atomic.Pointer[func(ch string, tss []string)] // told of the messages fetches by id brought (read_changes.go)

	prio      prioState  // the VIP unreads section's open conversation (prefs_a.go)
	topMem    topMembers // the channels' first members (membershipOrdered, code_card.go)
	acs       acState    // the composer autocomplete's frecency, model and history (autocomplete.go)
	ready     chan struct{}
	warm      warmState     // the warm boot: boot data, DataReady, the header cache (warmboot.go)
	recent    recentSnap    // RecentSnapshot's (snapshot_recent.go)
	sbPeople  sidebarPeople // the sidebar's DM people at the last snapshot (members.go)
	bdPeople  sidebarPeople // the unread IMs' partners the DM badges read (members.go readBadgePeople)
	bootErr   error
	mu        sync.Mutex
	connected bool
	online    onlineState // isClientOnline: the machine has a network (online.go)
	inflight  map[string]*call
	lookups   convLookups // LookupConv (channel_lookup.go)

	subMu sync.Mutex
	subs  map[*sub]struct{}
	evSeq uint64 // the last event's EmitSeq (subMu)

	docs docState // what message Docs read, as the last events left it (msg_refs.go)

	thrUnread threadsMarkedUnread // ThreadsMarkedUnread (message_menu.go)

	msgLog msgChanges // each message's latest change, for the views that keep copies of them (msg_changes.go)
	// refetchQ is RefetchMessages' rounds (read_changes.go)
	refetchQ refetchQueue

	// prefsMu orders every change to the raw prefs: a change copies them,
	// sets its keys and installs the copy, and two at once lost one of
	// them (mergePrefs, installPrefs).
	prefsMu sync.Mutex

	presSeen     map[string]time.Time
	presWaiters  []chan struct{}
	presSubs     map[string]int // Slack's presence manager's counts (presence_subs.go)
	presSubTimer *time.Timer
	countsTimer  *time.Timer
	countsReread bool           // refreshCounts' next read is the one after a merged answer (it doesn't ask again)
	actBadges    map[string]int // client.counts activity_v2 (activity_badges.go)
	actBadgesAt  uint64         // the store journal's position at their last change (setActivityBadgesSince)
	classic      classicBadges  // channel_badges and the alerts' counts (classic_badges.go)
	savedOverdue int            // saved.list counts.uncompleted_overdue_count (app_badge.go)
	cmdCache     []slack.Command
	cmdAt        time.Time
	cmdFailed    failedCommands // executeCommand's recently failed (command.go)

	files *fileCache

	streams openStreams // chat streams seen in progress (events.go emitStreamDone)

	saves sync.WaitGroup // background disk saves (loadEmoji's meta.json)
	side  sideReads      // History's side reads (history_side.go)

	// bg scopes all background work (realtime, prefetch, resolvers); it is
	// cancelled when the session ends.
	bg     context.Context
	stopBG context.CancelFunc
	// the activity events' work, off the event pump (activity_later.go)
	actLaterMu sync.Mutex
	actLaterQ  []func(context.Context)
	actLaterOn bool

	pending   map[string]*pendingSend // client_msg_id -> optimistic send
	fileEvs   *fileEvents             // file_created seen / awaited (upload_send.go)
	retired   map[string]string       // confirmed sends' local ts -> real ts (send.go LocalTS)
	retiredQ  []string                // retired's insertion order (bounded)
	saved     map[string]bool         // "channel/ts" of saved / to-do messages
	savedMeta map[string]SavedMeta    // their saved metadata (saved_meta.go)
	starred   map[string]bool         // starred conversation ids (stars.list)
	starsOK   bool                    // starred was loaded
	// starEdits/savedEdits, non-nil while loadStars/loadSaved is in
	// flight, record the stars and saves made meanwhile (Star, star_added,
	// Save), so the list read before them doesn't undo them.
	starEdits  map[string]bool
	savedEdits map[string]bool
	memberN    map[string]int       // conversation -> people count (edge users/counts)
	memberG    map[string]int       // conversation -> guests among them (edge users/counts)
	memberA    map[string]int       // conversation -> apps among its members (edge users/counts)
	dms        dmState              // which DMs Slack's sidebar shows (dmsidebar.go)
	exp        *sidebarExp          // experiments the sidebar rules read (nil: defaults)
	expGroups  map[string]string    // the groups every check reads (experiments.go)
	icon       teamIconState        // the rail's workspace icon (team_icon.go)
	rxn        reactState           // the viewer's reactions in flight (reactions.go)
	frec       frecencyState        // the frecency pref's save throttle and Frequently Used cache (frecency.go)
	rec        recentsState         // recent searches and places: the recents pref, visits (search_recents.go)
	secCol     sectionCollapseState // the channel_sections collapse write's debounce (section_collapse.go)
	drafts     draftCache           // the viewer's server drafts (drafts.go), memory only
	dnd        dndState             // the viewer's Do Not Disturb (dnd.go)
	mdnd       memberDNDState       // other members' Do Not Disturb (dnd_members.go)
	mega       megaphoneState       // megaphone spaces, global and per channel (megaphone.go)
	ssugg      searchSuggState      // the window's last search's filter suggestions (search_suggest.go)
	selected   string               // the open conversation (SetSelected, preview.go)
	temp       map[string]int64     // temporary memberships: channel -> expiration_ts (preview.go)

	// huddleChans is the huddle UI's huddleChannelIds (SetHuddleChannels,
	// sidebar_rules.go); nil until the UI sets it
	huddleChans func() map[string]bool
	// slackbotView: Clack shows Slack AI's Slackbot tab or sidebar
	// link (it has neither: false; tests set it), which hides Slackbot's
	// DM from the sidebar (slackbotDMHidden)
	slackbotView bool

	// experiments.go: experiments.getByUser's records (nil until it
	// answers) and the forced groups ("" reads as not assigned)
	expServer map[string]slack.ExperimentAssignment
	expForced map[string]string
	expLive   bool // experiments.getByUser answered (the disk cache's copy no longer applies)

	// Realtime (Slack's websocket gateway) is opt-in; see SetRealtime.
	rtMu      sync.Mutex
	rtWant    bool               // the user turned live updates on
	rtBooted  bool               // Boot finished; the transport may run
	rtStop    context.CancelFunc // stops the running RTM.Run
	rtDone    chan struct{}      // closed when that Run returned
	pollEvery time.Duration      // PollInterval, or tests' override
	pollSeen  map[string]string  // conversation -> counts latest|updated at the last poll
	watchCh   string             // conversation last opened (History), refreshed every poll
	watchTh   []watchedThread    // threads last opened (Replies), newest first
	// accessed: when each conversation ("ch/thread_ts" for a thread) was
	// last read, and loaded when its newest page was last fetched, for
	// Slack's message cache budget (evict.go)
	accessed, loaded map[string]time.Time

	// tempMuteTimer is kickOffTempMuteTimer's timer (temp_mute.go).
	tempMuteTimer *time.Timer

	// memberMC is each conversation's last users/counts answer whole;
	// memberStale the counts a join or a leave made stale and countThr
	// their refetch throttles (membership_counts.go)
	memberMC    map[string]MemberCounts
	memberStale map[string]bool
	countThr    map[string]*countThrottle
}

type watchedThread struct {
	ch, ts string
	at     time.Time
}

type call struct {
	done chan struct{}
	err  error
}

// New builds the service. Call Boot before using it.
func New(c *slack.Client, s *store.Store) *App {
	a := &App{C: c, S: s, ready: make(chan struct{}), inflight: map[string]*call{}, subs: map[*sub]struct{}{},
		presSeen: map[string]time.Time{}, presSubs: map[string]int{}, files: newFileCache(fileCacheBytes), pending: map[string]*pendingSend{},
		saved: map[string]bool{}, starred: map[string]bool{}, memberN: map[string]int{}, memberG: map[string]int{}, memberA: map[string]int{}}
	if s.SelfID() == "" {
		s.Lock()
		s.Self.ID = c.Creds.UserID
		s.Unlock()
	}
	a.warm.dataReady, a.warm.done = make(chan struct{}), make(chan struct{})
	a.bg, a.stopBG = context.WithCancel(context.Background())
	c.OnAuthFailed(a.onAuthFailed)
	a.RTM = c.NewRTM()
	a.RTM.OnState = func(up bool) {
		a.mu.Lock()
		a.connected = up
		a.mu.Unlock()
		// Merging msg-latency: its OnState block (socketConnected(fast)
		// and firstUp before the emit, socketDown on a drop) replaces this
		// emit; keep its block and give its emit Fast: up &&
		// a.RTM.LastConnectFast(). Without Fast every reconnect reloads
		// the Threads view (TestThreadsFastReconnectLikeSlack); without
		// its block slow connects miss its catch-up.
		if up && !a.RTM.LastConnectFast() {
			// Slack's onConnected "Resetting reachedEnd": every window's
			// end goes stale before the lists hear of the connection, and
			// the open one is read forward from it (reached_end.go)
			a.resetReachedEnd()
		}
		a.emit(Event{Type: "connection", Connected: up, Fast: up && a.RTM.LastConnectFast()})
		a.codeChannelsConnection(up) // the stale code channel closer's timers (codechannel_close.go)
		if up {
			a.socketConnected(a.RTM.LastConnectFast()) // the refetchers' slow reconnects (channel_pins.go)
			go a.refreshCountsAfterGap(a.bg)           // what the socket missed (codechannel_close.go)
		}
	}
	a.RTM.OnHello = a.onHello
	s.SetMembersOnline(a.membersOnline)
	appsByStore.Store(s, a)
	store.ExperimentGroup = experimentForStore
	a.followEmojiSet() // the defaults' set until the account's groups are in
	// what of the prefs conversions read as they start; every install
	// after notes what it moved, and the next prefs event converts again
	// what reads it (msg_refs.go)
	a.docs.prefs, a.docs.docPrefs = a.namePrefsKey(), a.docPrefsKey()
	return a
}

// appsByStore maps a store to its App, for store.ExperimentGroup (the
// huddle message sentences in richtext follow the viewer's groups).
var appsByStore sync.Map

func experimentForStore(s *store.Store, name string) string {
	if v, ok := appsByStore.Load(s); ok {
		return v.(*App).Experiment(name)
	}
	return ""
}

// PollInterval is how often core refreshes unread counts and the open
// conversations over the Web API while the realtime socket is off or down.
const PollInterval = 20 * time.Second

// SetRealtime turns Slack's realtime websocket on or off. It is off until
// the user opts in (the UI asks on first run): on some Enterprise Grid
// workspaces a connection Slack finds unusual can end the session
// everywhere, so Clack never opens it unasked. With it off, core polls
// client.counts and refreshes the open conversations every PollInterval,
// so reading and sending work, just without live push (typing, presence,
// huddle events, modals from slash commands). Safe to call before Boot.
func (a *App) SetRealtime(on bool) {
	a.rtMu.Lock()
	a.rtWant = on
	a.rtMu.Unlock()
	if on {
		a.startRealtime()
	} else {
		a.stopRealtime()
	}
	a.emit(Event{Type: "realtime", Connected: on})
}

// Realtime reports whether live updates are turned on (not whether the
// socket is up; see Snapshot.Connected).
func (a *App) Realtime() bool {
	a.rtMu.Lock()
	defer a.rtMu.Unlock()
	return a.rtWant
}

func (a *App) startRealtime() {
	a.ClientOnline() // starts the watcher that reconnects on wake and network changes (online.go)
	a.rtMu.Lock()
	defer a.rtMu.Unlock()
	if !a.rtWant || !a.rtBooted || a.rtStop != nil || a.bg.Err() != nil {
		return
	}
	ctx, cancel := context.WithCancel(a.bg)
	done := make(chan struct{})
	a.rtStop, a.rtDone = cancel, done
	go func() {
		defer close(done)
		a.RTM.Run(ctx)
		// Run doesn't report the drop when it was stopped (ctx cancelled)
		a.mu.Lock()
		was := a.connected
		a.connected = false
		a.mu.Unlock()
		if was {
			a.emit(Event{Type: "connection", Connected: false})
		}
		a.rtMu.Lock()
		if a.rtDone == done { // the session ended: allow a restart
			a.rtStop, a.rtDone = nil, nil
		}
		a.rtMu.Unlock()
		cancel()
	}()
}

func (a *App) stopRealtime() {
	a.rtMu.Lock()
	stop, done := a.rtStop, a.rtDone
	a.rtStop, a.rtDone = nil, nil
	a.rtMu.Unlock()
	if stop == nil {
		return
	}
	stop()
	<-done
}

// pollLoop stands in for the realtime socket while it is off or down:
// unread counts plus a "poll" event that makes the UI refetch the
// conversations and threads it has open. A handful of ordinary Web API
// reads every PollInterval, far less than Slack's own client sends.
func (a *App) pollLoop(ctx context.Context) {
	every := a.pollEvery
	if every <= 0 {
		every = PollInterval
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if a.C.SignedOut() {
			return
		}
		if a.RTM.Connected() {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		a.pollOnce(cctx)
		if a.teamIconDue() { // no team_icon_change without the socket
			a.refreshTeamIcon(cctx)
		}
		cancel()
	}
}

// maxPollFetches caps the history refetches of one poll.
const maxPollFetches = 6

// pollOnce refreshes counts, then refetches the newest page of the open
// conversation and of cached ones whose counts changed, and the threads
// opened in the last 15 minutes; then tells the UI ("poll").
func (a *App) pollOnce(ctx context.Context) {
	a.refreshCountsAfterGap(ctx) // no socket: nothing was heard (codechannel_close.go)
	if ctx.Err() != nil || a.C.SignedOut() {
		return
	}
	a.mu.Lock()
	if a.pollSeen == nil {
		a.pollSeen = map[string]string{}
	}
	watch := a.watchCh
	var threads []watchedThread
	for _, t := range a.watchTh {
		if time.Since(t.at) < 15*time.Minute {
			threads = append(threads, t)
		}
	}
	a.mu.Unlock()
	var fetch []string
	if watch != "" {
		fetch = append(fetch, watch)
	}
	ids := a.S.TimelineIDs()
	sort.Strings(ids)
	for _, id := range ids {
		e, _ := a.S.Count(id)
		key := e.Latest + "|" + e.Updated
		a.mu.Lock()
		old, seen := a.pollSeen[id]
		a.pollSeen[id] = key
		a.mu.Unlock()
		if seen && old != key && id != watch && len(fetch) < maxPollFetches {
			fetch = append(fetch, id)
		}
	}
	for _, ch := range fetch {
		if err := a.loadLatest(ctx, ch); err != nil {
			log.Printf("poll: history %s: %v", ch, err)
		}
	}
	for _, t := range threads {
		if err := a.fetchThread(ctx, t.ch, t.ts); err != nil {
			log.Printf("poll: thread %s: %v", t.ch, err)
		}
	}
	a.emit(Event{Type: "poll"})
}

// watching reports whether ch (or its thread threadTS) is what the UI
// opened last (watch): the conversation on screen.
func (a *App) watching(ch, threadTS string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if threadTS == "" {
		return ch != "" && ch == a.watchCh
	}
	for _, t := range a.watchTh {
		if t.ch == ch && t.ts == threadTS {
			return true
		}
	}
	return false
}

// watch remembers what the UI opened, for pollOnce.
func (a *App) watch(ch, threadTS string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if threadTS == "" {
		a.watchCh = ch
		return
	}
	th := []watchedThread{{ch: ch, ts: threadTS, at: time.Now()}}
	for _, t := range a.watchTh {
		if (t.ch != ch || t.ts != threadTS) && len(th) < 2 {
			th = append(th, t)
		}
	}
	a.watchTh = th
}

// onAuthFailed runs once when any call learns the session ended: stop all
// background work and tell the UI. Every later call fails fast with
// slack.ErrSignedOut without touching the network.
func (a *App) onAuthFailed(code string) {
	log.Printf("core: session ended (%s); stopping all Slack traffic", code)
	a.stopBG()
	// the warm boot's files only serve this session's next start (and
	// SaveWarm writes nothing once the session ended)
	a.S.RemoveWarmFiles()
	a.mu.Lock()
	if a.countsTimer != nil {
		a.countsTimer.Stop()
		a.countsTimer = nil
	}
	a.mu.Unlock()
	a.emit(Event{Type: "auth_failed", Name: code})
}

// Shutdown stops core's background work (realtime socket, prefetch,
// timers). The App must not be used afterwards. For tests and benchmarks
// that start many sessions in one process; the UI simply exits.
func (a *App) Shutdown() {
	defer a.side.wait() // History's side reads, which the stop below cancels
	a.StopTraffic()
	a.saves.Wait()   // a metadata save still writing (loadEmoji)
	a.warm.wg.Wait() // the warm boot's files, if they were writing
}

// StopTraffic is Shutdown without waiting for background disk saves (the
// UI goroutine calls it; Close, off it, waits).
func (a *App) StopTraffic() {
	a.stopBG()
	a.mu.Lock()
	if a.countsTimer != nil {
		a.countsTimer.Stop()
		a.countsTimer = nil
	}
	a.mu.Unlock()
}

// SignedOut reports whether the session ended (see Event "auth_failed").
func (a *App) SignedOut() bool { return a.C.SignedOut() }

// Ready is closed when Boot finished (successfully or not).
func (a *App) Ready() <-chan struct{} { return a.ready }

// ---- events ----

type sub struct {
	ch     chan Event
	lagged bool
}

// SubscriberBuffer is the per-subscriber event buffer.
const SubscriberBuffer = 1024

// Subscribe returns a buffered stream of events and a function that ends
// the subscription. If a subscriber falls behind, events are dropped and a
// single Event{Type:"resync"} is delivered once there is room again: the UI
// should then re-read what it shows (Snapshot, current History).
func (a *App) Subscribe() (<-chan Event, func()) {
	s := &sub{ch: make(chan Event, SubscriberBuffer)}
	a.subMu.Lock()
	a.subs[s] = struct{}{}
	a.subMu.Unlock()
	var once sync.Once
	return s.ch, func() {
		once.Do(func() {
			a.subMu.Lock()
			delete(a.subs, s)
			a.subMu.Unlock()
			close(s.ch)
		})
	}
}

// EmitSeq is the EmitSeq of the last event emitted so far: the last one
// that went out (emitIf numbers an event once its check has passed, so a
// refused one takes no number). A load's need is this, read when the load
// returns: the mirror always reaches it, as every number up to it went
// out (to a subscriber that fell behind as a resync, which claims what was
// emitted before it).
func (a *App) EmitSeq() uint64 {
	a.subMu.Lock()
	defer a.subMu.Unlock()
	return a.evSeq
}

// emit sends ev to every subscriber and returns it as they get it (with
// its EmitSeq, and a message change's Seq).
func (a *App) emit(ev Event) Event {
	ev, _ = a.emitIf(ev, nil)
	return ev
}

// emitIf is emit when ok, asked under the subscribers' lock (nil: always),
// so no event emitted after the check goes out before ev; it reports
// whether ev went out (read_changes.go announceHeld), and returns it as
// the subscribers got it. A refused event takes no EmitSeq: numbered
// before its check, it left App.EmitSeq past every event sent, a need no
// event the mirror handles could meet.
func (a *App) emitIf(ev Event, ok func() bool) (Event, bool) {
	a.noteDocChange(&ev) // what ev changed of what message Docs read (msg_refs.go)
	if !snapshotQuietEvents[ev.Type] {
		a.InvalidateSnapshot() // snapshot_recent.go
	}
	a.pinsTabSee(ev) // the Pins tab's message copies (channel_pins.go)
	a.subMu.Lock()
	defer a.subMu.Unlock()
	if ok != nil && !ok() {
		return ev, false
	}
	a.noteMsgChange(&ev) // stamps ev.Seq, in the subscribers' order (msg_changes.go)
	// numbered under the lock that orders the sends, once it goes out:
	// every subscriber gets the events in EmitSeq order
	a.evSeq++
	ev.EmitSeq = a.evSeq
	for s := range a.subs {
		if s.lagged {
			select {
			case s.ch <- Event{Type: "resync"}:
				s.lagged = false
			default:
				continue
			}
		}
		select {
		case s.ch <- ev:
		default:
			s.lagged = true
		}
	}
	return ev, true
}

// once deduplicates concurrent identical fetches (e.g. prefetch + open).
func (a *App) once(key string, f func() error) error {
	a.mu.Lock()
	if c, ok := a.inflight[key]; ok {
		a.mu.Unlock()
		<-c.done
		return c.err
	}
	c := &call{done: make(chan struct{})}
	a.inflight[key] = c
	a.mu.Unlock()
	c.err = f()
	a.mu.Lock()
	delete(a.inflight, key)
	a.mu.Unlock()
	close(c.done)
	return c.err
}

// ---- boot ----

// Boot loads everything the sidebar needs in parallel, then starts the
// realtime connection and background prefetch. It logs timings only.
func (a *App) Boot(ctx context.Context) error {
	defer close(a.ready)
	defer a.markDataReady() // a cold start's data is in (or Boot failed: what core holds is all there is)
	t0 := time.Now()
	// meta.json (the user directory, 65k emoji, usergroups) decodes
	// alongside the calls instead of before them; the steps that depend
	// on it wait for cacheDone.
	cacheDone := make(chan struct{})
	var tCache time.Duration
	go func() {
		s := time.Now()
		a.S.LoadCache()
		a.notePrefsInstalled()       // the cached prefs (msg_refs.go)
		a.installCachedExperiments() // Slack's persisted experiments store (experiments.go)
		a.loadCodeChannelState()     // the closed code channels, before the first sidebar (codechannel_close.go)
		tCache = time.Since(s)
		close(cacheDone)
		// The unknown member fetcher and the channel and bot name resolver
		// start once the persisted store is in, as Slack's first lookup
		// comes only after it loaded (renderApp waits for dataSetupStore).
		// Started before, an early bot answer was replaced by meta.json's
		// bots (and never asked for again: noteMissing records a bot once
		// a session), and members meta.json holds were asked for. What
		// lookups missed until now waits in the store's queues. From here
		// they run from the first lookup, as Slack's UnknownFetchManagers
		// do (client-boot 0mwJ, made in DOS4 H for members: isEnabled is on
		// everywhere but Slack's own test runners), not once every call
		// below has answered: a warm start draws the restored conversation
		// from the boot data while they're out, and a DM's partner the
		// metadata cache doesn't hold stayed BaseMissingMember's bar until
		// boot's last call (a slow users.conversations, a rate-limited
		// step), or for good when boot failed.
		go a.resolveLoop(a.bg) // unresolved channel/bot names
		go a.memberLoop(a.bg)  // members lookups missed (members.go)
	}()
	// The warm boot: boot_data.json and headers.json load alongside, and
	// once meta.json is in too the cached sidebar is installed and
	// DataReady closes, long before the calls below answer (warmboot.go).
	go a.loadWarm(cacheDone)
	var (
		wg       sync.WaitGroup
		tmu      sync.Mutex
		timings  []string
		convs    []slack.Conversation
		sections []slack.Section
		errConvs error
		raced    struct {
			sync.Mutex
			counts, sections bool
		}
	)
	// Each sidebar answer is installed when it comes, as Slack dispatches
	// every boot API's answer into its store on arrival, after the boot
	// data (a warm start's disk reads are far quicker than the calls), and
	// merged with what this session changed after the calls went out. The
	// window is usable from the boot data meanwhile, so a mark, a leave, a
	// DM opened or closed or a section move made then is newer than the
	// answer that lands after it, and it stays (store/journal.go). On a
	// warm start each install is announced, so the UI follows it the way
	// Slack's components re-render on each dispatch.
	since := a.S.Seq()
	installed := func(ev string) {
		if a.Warm() {
			a.emit(Event{Type: ev})
		}
	}
	countsIn := make(chan struct{}) // client.counts installed (or failed): sidebar.dms' reopen marks read it
	step := func(name string, f func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := time.Now()
			err := f()
			d := time.Since(s).Round(time.Millisecond).String()
			tmu.Lock()
			if err != nil {
				d += "(err)"
				log.Printf("boot: %s: %v", name, err)
			}
			timings = append(timings, name+"="+d)
			tmu.Unlock()
		}()
	}
	var tDMUsers time.Duration
	dmsAnswered := make(chan struct{}) // sidebar.dms answered (or failed), before its install waits for the counts
	var dmsOpen map[string]bool        // its open flags (nil: it failed)
	step("conversations", func() error {
		convs, errConvs = a.C.Conversations(ctx)
		if errConvs != nil {
			return errConvs
		}
		<-a.warm.done
		a.installConvs(convs, since)
		installed("sidebar_changed")
		// the DM rows' people, straight away (not after every other step):
		// the sidebar's DMs, not every DM users.conversations lists (closed
		// ones aren't rows; their partners are read when something draws
		// them: members.go). A warm start knows which are open from the
		// boot data at once; a cold start waits for sidebar.dms' answer,
		// not for client.counts its install waits on (bootDMPeople).
		open := map[string]bool(nil) // core's own (dmOpen)
		if !a.SidebarDMsKnown() {
			<-dmsAnswered
			open = dmsOpen // nil when it failed: every DM is a row then
		}
		ids := a.bootDMPeople(convs, open)
		<-cacheDone
		tu := time.Now()
		a.EnsureUsers(ctx, ids)
		tDMUsers = time.Since(tu)
		return nil
	})
	step("counts", func() error {
		defer close(countsIn)
		c, err := a.C.Counts(ctx)
		if err != nil {
			return err
		}
		<-a.warm.done
		if a.installCounts(c, since) {
			raced.Lock()
			raced.counts = true
			raced.Unlock()
		}
		installed("counts")
		return nil
	})
	step("sections", func() error {
		secs, err := a.C.SectionsAll(ctx)
		if err != nil {
			return err // a failed call keeps the boot data's sections
		}
		sections = secs
		<-a.warm.done
		if a.S.SetSectionsSince(secs, since) {
			installed("sidebar_changed")
		} else { // a section write since is newer
			raced.Lock()
			raced.sections = true
			raced.Unlock()
		}
		return nil
	})
	step("experiments", func() error { // every experiment check (experiments.go)
		g, err := a.C.ExperimentAssignments(ctx)
		if err == nil {
			a.setExperimentAssignments(g)
		}
		return err
	})
	step("sidebar.dms", func() error { // which DMs are open (users.conversations has no is_open)
		dms, err := a.C.SidebarDMs(ctx)
		if err == nil {
			dmsOpen = make(map[string]bool, len(dms))
			for _, d := range dms {
				if d.IsOpen {
					dmsOpen[d.ID] = true
				}
			}
		}
		close(dmsAnswered) // the DM rows' people don't wait for the counts below
		<-countsIn         // closed DMs reopen on messages newer than now (reopenMark reads the counts)
		if err != nil {
			return err // a failed call keeps the boot data's
		}
		<-a.warm.done
		a.setSidebarDMsSince(dms, since)
		installed("sidebar_changed")
		return nil
	})
	loadPrefs := func() error {
		raw, err := a.C.Prefs(ctx)
		if err == nil {
			a.installPrefs(raw)
			// set_tz_automatically keeps tz in step (timezone.go)
			go func() { _, _ = a.MaybeUpdateTimezone(a.bg) }()
			// the first frame drew the cached copy; or, without one, what
			// was converted before this install read the defaults
			if a.S.HasCachedPrefs() || a.prefsMoved() {
				p := a.Prefs()
				a.emit(Event{Type: "prefs", Prefs: &p})
			}
			// loadFrecencyPrefs (fetchDeferredData), over the full map: its
			// frecency is the plain pref, "" on Enterprise Grid (keepFrecency)
			if err := a.ensureFrecencyLoaded(ctx); err != nil {
				log.Printf("frecency: users.prefs.get emoji_use,frecency: %v", err)
			}
		}
		return err
	}
	step("prefs", loadPrefs) // the theme and the sidebar rules: in the batch
	// What the sidebar shows besides sections: starred conversations
	// (stars.list), and the saved set; in the batch, so the first sidebar
	// is complete instead of the Starred section filling in afterwards.
	step("stars", func() error { return a.loadStars(ctx) })
	step("saved", func() error { return a.loadSaved(ctx) })
	step("team.prefs", func() error { return a.LoadTeamPrefs(ctx) }) // teamprefs.go
	step("team", func() error {
		var r struct {
			Team struct {
				ID           string          `json:"id"`
				Name         string          `json:"name"`
				Domain       string          `json:"domain"`
				Icon         json.RawMessage `json:"icon"`
				Plan         string          `json:"plan"`
				EnterpriseID string          `json:"enterprise_id"`
			} `json:"team"`
		}
		err := a.C.Call(ctx, "team.info", nil, &r)
		if err == nil {
			a.S.Lock()
			a.S.TeamID, a.S.TeamName, a.S.TeamDomain = r.Team.ID, r.Team.Name, r.Team.Domain
			a.S.Unlock()
			a.setTeamPlan(r.Team.Plan, r.Team.EnterpriseID) // files_sections.go
			a.setTeamIcon(r.Team.ID, r.Team.Icon)
			a.setTeamName(r.Team.ID, r.Team.Name)
		}
		return err
	})
	// self, then the rail's workspace icon (which on Grid depends on the
	// viewer's workspaces): in the batch, so the icon doesn't change later
	step("self", func() error {
		<-cacheDone
		a.EnsureUsers(ctx, []string{a.C.Creds.UserID})
		a.refreshTeamIcon(ctx)
		return nil
	})
	// Custom emoji (65k on Hack Club, ~7 MB of JSON): from meta.json when
	// it has them (emoji_changed keeps a running session current, and a
	// stale map refreshes after startup), else fetched in the batch:
	// messages draw them as images from their first frame.
	step("emoji", func() error {
		<-cacheDone
		if a.S.CustomEmoji.Len() == 0 {
			return a.loadEmoji(ctx)
		}
		if a.S.EmojiAge() > EmojiMaxAge {
			a.later(10*time.Second, func() { _ = a.loadEmoji(a.bg) })
		}
		return nil
	})
	a.later(5*time.Second, func() { _ = a.loadUsergroups(a.bg) }) // cosmetic; cached on disk after the first run
	// client.extras: the message menu's Report to Slack (message_menu.go)
	a.later(5*time.Second, func() { _ = a.LoadClientExtras(a.bg) })
	wg.Wait()
	<-a.warm.done
	defer a.warmDone() // Boot's answers (or a failed Boot's store) replaced the boot data's sidebar
	if errConvs != nil {
		a.bootErr = errConvs
		return errConvs
	}
	a.reconcileClosedCodeChannels(since) // the saved closed code channels against what boot fetched (codechannel_close.go)
	if u, ok := a.S.User(a.C.Creds.UserID); ok {
		a.S.Lock()
		a.S.Self = *u
		a.S.Unlock()
	}
	sort.Strings(timings)
	log.Printf("boot: ready in %s (cache %s, %s, dm-users=%s); %d conversations, %d sections, %d emoji",
		time.Since(t0).Round(time.Millisecond), tCache.Round(time.Millisecond), strings.Join(timings, " "),
		tDMUsers.Round(time.Millisecond), len(convs), len(sections), a.S.CustomEmoji.Len())
	applog.Mark("core-ready", fmt.Sprintf("%d convs, %d sections", len(convs), len(sections)))
	a.setBootOK() // what core holds is worth keeping for the next warm boot
	// An answer that kept a change made here while it was out is older
	// than Slack's state now: read that list once more, merged the same way.
	raced.Lock()
	if raced.counts {
		a.refreshCountsSoon()
	}
	if raced.sections {
		go a.refreshSections(a.bg)
	}
	raced.Unlock()
	a.rtMu.Lock()
	a.rtBooted = true
	a.rtMu.Unlock()
	a.startRealtime()
	go a.pollLoop(a.bg)
	go a.evictLoop(a.bg)             // Slack's message cache budget (evict.go)
	go a.codeChannelCloserLoop(a.bg) // code_channels_auto_close (codechannel_close.go)
	go a.pump()
	go a.prefetch(a.bg)
	go a.membersLoop(a.bg) // the members cache's pass (members.go)
	if a.C.Creds.EnterpriseID != "" {
		go func() { _ = a.LoadEnterprisePrefs(a.bg) }() // enterprise_prefs.go
	}
	// everything boot fetched is in: plugins' state mirror re-reads it
	a.emit(Event{Type: "booted"})
	// recent searches and places (maybeLoadRecentsPrefs, search_recents.go)
	go a.MaybeLoadRecents(a.bg)
	// fetchedDeferredMessagingClientData's maybeFetchCollections (boot-
	// render): the file collections, under custom_file_sections, so the
	// canvas and list headers' section buttons know them
	go func() { _ = a.FetchFileCollections(a.bg) }()
	// The warm boot's files from this session's fresh state, once
	// startup's work is done, then with the metadata cache every 5
	// minutes, and at Close.
	a.warm.wg.Add(1) // Shutdown waits for it; Close doesn't (it saves itself)
	go func() {
		defer a.warm.wg.Done()
		t := time.NewTimer(20 * time.Second)
		defer t.Stop()
		select {
		case <-a.bg.Done():
			return
		case <-t.C:
		}
		a.SaveWarm()
	}()
	go func() {
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-a.bg.Done():
				return
			case <-t.C:
			}
			a.SaveWarm()
			_ = a.S.SaveCache()
		}
	}()
	return nil
}

// Close saves the warm boot's files and the metadata cache (after any
// save still writing).
func (a *App) Close() error {
	a.side.wait() // History's side reads (StopTraffic, before it, cancelled them)
	a.saves.Wait()
	a.checkpointClosedCodeChannels() // the closed code channels' watermarks (codechannel_close.go)
	a.SaveWarm()
	return a.S.SaveCache()
}

func (a *App) loadEmoji(ctx context.Context) error {
	t0 := time.Now()
	p, n, err := a.C.EmojiListPacked(ctx)
	if err != nil {
		log.Printf("emoji.list: %v", err)
		return err
	}
	tf := time.Since(t0)
	a.S.SetEmojiPacked(p)
	applog.Debugf("emoji.list: %d emoji, fetched+scanned in %s, installed in %s", n,
		tf.Round(time.Millisecond), (time.Since(t0) - tf).Round(time.Millisecond))
	a.saves.Add(1) // Shutdown waits for it (a test's temp dir, sign-out)
	go func() {    // 7 MB to disk: not on boot's path
		defer a.saves.Done()
		_ = a.S.SaveCache()
	}()
	return nil
}

// fileCacheBytes bounds Fetch's memory-only cache of authenticated
// downloads (files.slack.com thumbnails and files, compressed bytes; the
// image cache decodes from it). Was 256 MB.
const fileCacheBytes = 48 << 20

// EmojiMaxAge is how old the cached emoji map may be before Boot refetches
// emoji.list in the background.
var EmojiMaxAge = 6 * time.Hour

// later runs f after d on a background goroutine, unless the app stops
// first. f must use a.bg for its requests: the auth-failed stop cancels it.
// The returned channel closes once f ran or was skipped.
func (a *App) later(d time.Duration, f func()) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-t.C:
		case <-a.bg.Done():
			return
		}
		// re-check right before sending: the session may have ended while
		// the timer ran (the client also refuses after sign-out)
		if a.bg.Err() != nil || a.C.SignedOut() {
			return
		}
		f()
	}()
	return done
}

func (a *App) loadUsergroups(ctx context.Context) error {
	gs, err := a.C.Usergroups(ctx)
	if err != nil {
		return err
	}
	a.S.SetUsergroups(gs)
	// updateUserGroups: the viewer's groups (usergroups.membership), what
	// a group mention's highlight follows (UserGroup fg3x isUserInGroup)
	if mine, err := a.C.UsergroupMembership(ctx); err == nil && a.S.SetMyUsergroups(mine) {
		// the visible messages re-read their group mentions. Only when the
		// viewer's groups moved: a names{} converts every loaded message
		// again, and with no groups every Usergroups call (the composer's
		// first "@", a directory search) loaded them again
		a.emit(Event{Type: "names"})
	}
	return nil
}

// EnsureUsers fetches unknown users (edge cache, parallel batches of 100)
// and returns once they're in. One another request has out (the unknown
// member fetcher's, another EnsureUsers') isn't asked for again: this
// waits for that answer (usersOut).
func (a *App) EnsureUsers(ctx context.Context, ids []string) {
	miss := a.S.MissingUsers(ids)
	if len(miss) == 0 {
		return
	}
	ask, wait, done := a.usersOut.claim(miss)
	func() {
		defer a.usersOut.release(ask, done) // before the wait: two requests never wait on each other
		if len(ask) == 0 {
			return
		}
		a.usersAsked.Add(int64(len(ask)))
		us, err := a.C.UsersInfoParallel(ctx, ask)
		if err != nil {
			log.Printf("users/info: %v", err)
		}
		a.S.PutUsers(us)
	}()
	waitUsers(ctx, wait)
}

// prefetch warms the history of unread conversations so opening them is
// instant. Bounded: at most 24 conversations, 3 at a time.
func (a *App) prefetch(ctx context.Context) {
	t0 := time.Now()
	type cand struct {
		id, latest string
		score      int
	}
	var cs []cand
	a.S.RLock()
	for id, e := range a.S.Counts {
		if !e.HasUnreads && e.MentionCount == 0 {
			continue
		}
		if c, ok := a.S.Convs[id]; !ok || a.S.Muted[id] || IsFileChannel(c) {
			continue
		}
		cs = append(cs, cand{id, e.Latest, e.MentionCount})
	}
	a.S.RUnlock()
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].score != cs[j].score {
			return cs[i].score > cs[j].score
		}
		return store.TSLess(cs[j].latest, cs[i].latest)
	})
	if len(cs) > 24 {
		cs = cs[:24]
	}
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	n := 0
	for _, c := range cs {
		if a.S.Timeline(c.id) != nil {
			continue
		}
		n++
		wg.Add(1)
		sem <- struct{}{}
		go func(id string) {
			defer wg.Done()
			defer func() { <-sem }()
			_ = a.loadLatestIfMissing(ctx, id)
		}(c.id)
	}
	wg.Wait()
	if n > 0 {
		log.Printf("prefetch: %d unread conversations in %s", n, time.Since(t0).Round(time.Millisecond))
	}
}

// loadLatest fetches the newest page of ch, replacing the cached one.
func (a *App) loadLatest(ctx context.Context, ch string) error {
	return a.once("h:"+ch, func() error { return a.fetchLatest(ctx, ch) })
}

// loadLatestIfMissing is loadLatest for a timeline nothing loaded yet. The
// check runs inside the once: a prefetch queued behind the others must not
// refetch a channel History loaded meanwhile, since its page (read from
// Slack before a send's echo, say) would replace the newer cached one.
func (a *App) loadLatestIfMissing(ctx context.Context, ch string) error {
	// onceCtx: a caller joining another's read waits only while its own
	// ctx lasts (core's background readers end with the session)
	return a.onceCtx(ctx, "h:"+ch, func() error {
		if a.S.Timeline(ch) != nil {
			return nil
		}
		return a.fetchLatest(ctx, ch)
	})
}

func (a *App) fetchLatest(ctx context.Context, ch string) error {
	// a history just loaded (a prefetch, a refresh) counts as read for the
	// message cache budget: Slack resets a quiet conversation's history
	// only an hour after it was last used (evict.go)
	a.noteLoaded(ch)
	pins, subs, live, gen := a.S.PinSeq(), a.S.SubSeq(), a.S.LiveSeq(), a.S.EndGen()
	p, err := a.C.History(ctx, ch, slack.HistoryOpts{Limit: 50})
	if err != nil {
		return err
	}
	// its people are asked for as rows draw them (lazy_users.go); the
	// reactions the socket brought while the request was out stay, and a
	// revision the store held another of reaches every copy and the views
	// (store/read_revisions.go, read_changes.go)
	// the viewer's sends the window holds and the page doesn't stay (one
	// confirmed after the page was read, a pending or failed copy), and a
	// slow connect while the page was out leaves the end stale
	// (store/reached_end.go SetLatestReadAt)
	a.S.SetLatestReadAt(ch, p.Messages, p.HasMore, live, gen, a.sendsIn(ch))
	a.S.SyncPins(ch, pins, p.Messages) // a pin event newer than the answer (store/pins.go)
	a.S.SyncSubs(ch, subs, p.Messages) // the roots' thread subscriptions (store/thread_subs.go)
	a.announceReads()                  // after the pins and subscriptions: the copies as they are now
	for i := range p.Messages {
		if needsFileInfo(&p.Messages[i]) {
			go a.fillFiles(ch, p.Messages[i].TS)
		}
	}
	return nil
}

// ---- counts ----

func (a *App) refreshCounts(ctx context.Context) {
	since := a.S.Seq()
	if c, err := a.C.Counts(ctx); err == nil {
		// a mark or message while it was out stays (store/journal.go); an
		// answer merged with one is partly older than Slack is now, so the
		// counts are read once more (not again after that read: a busy
		// channel doesn't keep it asking)
		merged := a.installCounts(c, since)
		a.readBadgePeople() // an IM the answer made unread: its member (members.go)
		a.mu.Lock()
		reread := merged && !a.countsReread
		a.countsReread = reread
		a.mu.Unlock()
		if reread {
			a.refreshCountsSoon()
		}
		a.emit(Event{Type: "counts"})
		if a.reopenActive() { // a closed DM got a new message
			a.emit(Event{Type: "sidebar_changed"})
		}
	}
}

// refreshCountsSoon coalesces bursts of events into one client.counts call.
func (a *App) refreshCountsSoon() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.countsTimer != nil {
		return
	}
	a.countsTimer = time.AfterFunc(1500*time.Millisecond, func() {
		a.mu.Lock()
		a.countsTimer = nil
		a.mu.Unlock()
		ctx, cancel := context.WithTimeout(a.bg, 20*time.Second)
		defer cancel()
		a.refreshCounts(ctx)
	})
}

func countOf(e slack.CountEntry, muted bool) Count {
	return Count{LastRead: e.LastRead, Latest: e.Latest, Unread: e.HasUnreads, Mentions: e.MentionCount, Muted: muted}
}

// Count returns a conversation's unread state.
func (a *App) Count(ch string) Count {
	e, _ := a.S.Count(ch)
	a.S.RLock()
	m := a.S.Muted[ch]
	a.S.RUnlock()
	return countOf(e, m)
}

// Counts returns every conversation's unread state.
func (a *App) Counts() map[string]Count {
	a.S.RLock()
	defer a.S.RUnlock()
	out := make(map[string]Count, len(a.S.Counts))
	for id, e := range a.S.Counts {
		out[id] = countOf(*e, a.S.Muted[id])
	}
	return out
}

// ThreadsState returns the Threads view's unread state.
func (a *App) ThreadsState() ThreadsState {
	a.S.RLock()
	defer a.S.RUnlock()
	return ThreadsState{Unread: a.S.ThreadsUnread, Mentions: a.S.ThreadMentions}
}

// ---- snapshot ----

// Snapshot returns the sidebar state. It blocks until Boot finished, so
// it is for goroutines: the UI goroutine reads the model its backend
// applied, RecentSnapshot or SnapshotNow, which never wait. A wait longer
// than bootWaitLog is logged with its caller (warmboot.go), so a path that
// still waits on the UI goroutine names itself.
func (a *App) Snapshot() Snapshot {
	a.waitBoot("Snapshot")
	return a.snapshotNow()
}

// SnapshotNow is Snapshot without waiting for Boot: what core holds now
// (empty sections and conversations early in a cold start). For callers
// that must never block, like the plugins' state mirror on the JS thread;
// core emits "booted" when Boot's data is in.
func (a *App) SnapshotNow() Snapshot { return a.snapshotNow() }

// snapshotNow is Snapshot from what the store holds now.
func (a *App) snapshotNow() Snapshot {
	a.recent.builds.Add(1)
	seq := a.recent.seq.Add(1)
	a.S.RLock()
	cs := make([]*slack.Conversation, 0, len(a.S.Convs))
	for _, c := range a.S.Convs {
		cs = append(cs, c)
	}
	team := Team{ID: a.S.TeamID, Name: a.S.TeamName, Icon: a.S.TeamIcon, Domain: a.S.TeamDomain}
	selfID := a.S.Self.ID
	prefs := a.S.Prefs
	a.S.RUnlock()
	if c, ok := a.selectedExt(); ok { // a previewed channel's row (preview.go)
		cs = append(cs, c)
	}
	// every DM's partner as the store holds it (getRawMemberById): no hit,
	// nothing asked for. Only the DMs the sections list read theirs
	// through getMemberById, below.
	convs := make([]Conv, 0, len(cs))
	for _, c := range cs {
		convs = append(convs, a.convOf(c, false))
	}
	sort.Slice(convs, func(i, j int) bool { return convs[i].Name < convs[j].Name })
	a.mu.Lock()
	connected := a.connected
	a.mu.Unlock()
	self, _ := a.User(selfID)
	secs, counts := a.sections(convs), a.Counts()
	convs = sidebarFileConvs(convs, secs)
	a.readBadgePeople() // getMemberById for the unread IMs the DM badges read (members.go)
	users := a.sidebarUsers(convs, secs, selfID)
	return Snapshot{
		Self: self, Team: team, Convs: convs, Sections: secs, Counts: counts,
		Threads: a.ThreadsState(), Users: users, Connected: connected, Prefs: prefs,
		EmojiCount: a.S.CustomEmoji.Len(), EnterpriseID: a.C.Creds.EnterpriseID, Rail: a.Rail(),
		FilterNames: a.filterNames(convs, secs, counts), Seq: seq,
	}
}

// sidebarFileConvs drops the file channels (sidebar_docs.go) no section
// shows: Slack's candidates leave them out, so they aren't channels to
// count, switch to or pick.
func sidebarFileConvs(convs []Conv, secs []Section) []Conv {
	var shown map[string]bool
	var out []Conv
	for i, c := range convs {
		if !c.File {
			if out != nil {
				out = append(out, c)
			}
			continue
		}
		if shown == nil {
			shown = map[string]bool{}
			for _, s := range secs {
				for _, l := range [][]string{s.Channels, s.All, s.CollapsedChannels, s.HiddenRead, s.HiddenMuted, s.HiddenDormant, s.HiddenUnread} {
					for _, id := range l {
						shown[id] = true
					}
				}
			}
		}
		if out == nil {
			out = append(make([]Conv, 0, len(convs)), convs[:i]...)
		}
		if shown[c.ID] {
			out = append(out, c)
		}
	}
	if out == nil {
		return convs
	}
	return out
}

// BootErr is the error Boot returned, if any. It waits for Boot, like
// Snapshot.
func (a *App) BootErr() error {
	a.waitBoot("BootErr")
	return a.bootErr
}

// ---- conversions ----

// avatarURL is store.AvatarURL (message unfurl cards draw the same
// picture, richtext.MemberNamer).
func avatarURL(u *slack.User, size int, viewerTeam string) string {
	return store.AvatarURL(u, size, viewerTeam)
}

func (a *App) userOf(u *slack.User) User {
	viewer := firstNonEmpty(a.C.Creds.Team, a.S.TeamID)
	v := User{
		ID: u.ID, Name: store.DisplayName(u), RealName: strings.TrimSpace(firstNonEmpty(u.Profile.RealName, u.RealName)), Handle: u.Name,
		DisplaySet: strings.TrimSpace(u.Profile.DisplayName) != "",
		Avatar:     avatarURL(u, 72, viewer), Avatar192: avatarURL(u, 192, viewer), Avatar512: avatarURL(u, 512, viewer),
		Title: u.Profile.Title, Pronouns: u.Profile.Pronouns, Email: u.Profile.Email, StatusText: u.Profile.StatusText,
		StatusEmoji: u.Profile.StatusEmoji, StatusCanonical: u.Profile.StatusTextCanonical, StatusExp: u.Profile.StatusExpiration, TZ: u.TZ, TZOffset: u.TZOffset,
		TZLabel: u.TZLabel, IsBot: u.IsBot || u.ID == "USLACKBOT", Deleted: u.Deleted, IsAdmin: u.IsAdmin,
		IsOwner: u.IsOwner, Guest: u.IsRestricted || u.IsUltraRestricted, Frontline: u.RestrictedUserType == "frontline_worker", Color: u.Color,
		Huddle: u.Profile.HuddleState == "in_a_huddle", Agent: u.AgentStatus == "consistently_an_agent",
		HuddleChannel: u.Profile.HuddleChannel, HuddleExpires: u.Profile.HuddleExpires,
		Stranger: u.IsStranger, BotID: u.Profile.BotID, AppID: u.Profile.APIAppID,
		Workflow: u.IsWorkflowBot, Agentforce: u.IsAgentforceBot, AppUser: u.IsAppUser,
		DisplayName: strings.TrimSpace(u.Profile.DisplayName), FirstName: u.Profile.FirstName, LastName: u.Profile.LastName,
	}
	if eu := u.EnterpriseUser; eu != nil {
		v.EnterpriseID, v.Teams, v.EnterpriseAdmin = eu.EnterpriseID, eu.Teams, eu.IsAdmin
	}
	v.TeamID, v.External = a.locality(u)
	if v.RealName == "​" {
		v.RealName = ""
	}
	// the status emoji through its status_emoji_display_info entry (by
	// emoji_name, not the list's first: status_emoji.go)
	v.StatusEmojiInfo = parseStatusEmojiInfo(u.Profile.StatusEmojiDisplay)
	if v.StatusEmoji != "" {
		// StatusEmoji stays the profile's own code; the picture is the
		// display form's (a display_alias, a custom display_url)
		code, pic := EmojiDisplayInfo(v.StatusEmoji, v.StatusEmojiInfo)
		if pic == "" {
			pic, _, _ = a.S.Emoji(strings.Trim(code, ":"))
		}
		v.StatusEmojiURL = pic
	}
	v.DND = a.MemberInDND(u.ID, time.Now())
	a.S.RLock()
	v.Presence = a.S.Presence[u.ID]
	a.S.RUnlock()
	if v.Presence == "" && (u.Presence == "active" || u.Presence == "away") {
		v.Presence = u.Presence // the user object's own (users.info presence), until presence_change
	}
	return v
}

// locality is Slack's computeLocality (client-boot ppn0): the member's
// team (the org's id on Enterprise Grid when the user has an
// enterprise_user, else team_id, else the viewer's team) and whether the
// member is external: on neither the viewer's team (team_id or
// enterprise_user.teams) nor its org.
func (a *App) locality(u *slack.User) (team string, external bool) {
	cur := firstNonEmpty(a.C.Creds.Team, a.S.TeamID)
	org := a.C.Creds.EnterpriseID
	eu := u.EnterpriseUser
	switch {
	case org != "" && eu != nil && eu.EnterpriseID != "":
		team = eu.EnterpriseID
	case u.TeamID != "":
		team = u.TeamID
	case eu != nil && u.ID == eu.ID && eu.EnterpriseID != "":
		team = eu.EnterpriseID
	default:
		team = cur
	}
	if cur == "" {
		return team, false
	}
	member := team == cur
	if eu != nil && slices.Contains(eu.Teams, cur) {
		member = true
	}
	inOrg := org != "" && (team == org || eu != nil && eu.EnterpriseID == org)
	return team, !member && !inOrg
}

// User returns a known user (from memory; see EnsureUsers).
func (a *App) User(id string) (User, bool) {
	u, ok := a.S.User(id)
	if !ok {
		return User{}, false
	}
	return a.userOf(u), true
}

// RawUser is User as getRawMemberById reads it: the member if the store
// holds it, with no hit recorded and nothing asked for. For logic over
// people nothing draws (a closed DM's partner in a list of every DM).
func (a *App) RawUser(id string) (User, bool) {
	u, ok := a.S.RawUser(id)
	if !ok {
		return User{}, false
	}
	return a.userOf(u), true
}

// MemberAvatar is getProfileImageForMember: member id's picture for a
// size px slot ("" while the member isn't known: the lookup asks for it,
// and "names" follows).
func (a *App) MemberAvatar(id string, size int) string {
	if id == "" {
		return ""
	}
	u, ok := a.S.User(id)
	if !ok {
		return ""
	}
	return avatarURL(u, size, firstNonEmpty(a.C.Creds.Team, a.S.TeamID))
}

// Self returns the signed-in user.
func (a *App) Self() User {
	u, _ := a.User(a.S.SelfID())
	return u
}

func (a *App) users(ids []string) map[string]User {
	out := map[string]User{}
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := out[id]; ok {
			continue
		}
		if u, ok := a.User(id); ok {
			out[id] = u
		}
	}
	return out
}

// Users returns the known users among ids.
func (a *App) Users(ids []string) map[string]User { return a.users(ids) }

func (a *App) conv(c *slack.Conversation) Conv { return a.convOf(c, true) }

// convOf is conv. With lookup false an IM's partner is read as the store
// holds it (getRawMemberById: no hit, a miss asks for nothing), as the
// sidebar's snapshot reads the DMs it doesn't list.
func (a *App) convOf(c *slack.Conversation, lookup bool) Conv {
	v := Conv{ID: c.ID, Name: c.Name, User: c.User, NumMembers: c.NumMembers, IsArchived: c.IsArchived,
		IsGeneral: c.IsGeneral, IsShared: c.IsExtShared, Created: c.Created, Creator: c.Creator, Priority: c.Priority}
	switch {
	case c.IsIM:
		v.Kind = "im"
		if lookup {
			v.Name = a.S.UserName(c.User)
		} else {
			v.Name = a.rawUserName(c.User)
		}
		u, _ := a.S.RawUser(c.User)
		v.NoDMsBadge, v.NoDMsCount = dmBadgeSkips(c.User, u) // rail.go
		v.BotDM = isBotDM(c.User, u)
	case c.IsMPIM:
		v.Kind = "mpim"
		v.Members = c.Members
	case c.IsPrivate || c.IsGroup:
		v.Kind = "private"
	default:
		v.Kind = "channel"
	}
	if c.Topic != nil && c.Topic.Value != "" {
		v.Topic = c.Topic.Value
		v.TopicDoc = richtext.FromMrkdwn(a.S, v.Topic)
	}
	if c.Purpose != nil && c.Purpose.Value != "" {
		v.Purpose = c.Purpose.Value
		v.PurposeDoc = richtext.FromMrkdwn(a.S, v.Purpose)
	}
	a.S.RLock()
	v.Muted = a.S.Muted[c.ID]
	a.S.RUnlock()
	peopleOnly := a.agentMemberChannelCountOn()
	a.mu.Lock()
	if n, ok := a.memberN[c.ID]; ok {
		v.NumMembers = channelMemberCount(n, a.memberA[c.ID], peopleOnly) // agent.go
	}
	v.Starred = a.starred[c.ID]
	v.TempUntil = a.temp[c.ID]
	a.mu.Unlock()
	v.Preview = a.isPreviewConv(c)
	v.Member = a.isMemberConv(c)
	v.ConnectedTeams = a.connectedTeams(c)
	if cc := codeChannelOf(c); cc != nil {
		v.Code, v.CodeStatus, v.AgentName = cc, a.codeStatusOf(c, cc), a.AgentName(cc)
	}
	a.fileChannelConv(c, &v) // a canvas's or list's conversation (sidebar_docs.go)
	return v
}

// Conv returns a known conversation: the sidebar's, or one resolved
// earlier with ResolveConv (closed DMs etc.). See Snapshot for the sidebar.
func (a *App) Conv(id string) (Conv, bool) {
	c, ok := a.S.Conv(id)
	if !ok {
		if c, ok = a.S.ExtConv(id); !ok {
			return Conv{}, false
		}
	}
	return a.conv(c), true
}

func (a *App) kindOf(ch string) string {
	if c, ok := a.Conv(ch); ok {
		return c.Kind
	}
	return ""
}

// ConvRef describes any conversation id, member or not.
func (a *App) ConvRef(id string) ConvRef {
	if c, ok := a.Conv(id); ok {
		return ConvRef{ID: id, Name: c.Name, Kind: c.Kind, User: c.User}
	}
	kind := "channel"
	switch {
	case strings.HasPrefix(id, "D"):
		kind = "im"
	case strings.HasPrefix(id, "G"):
		kind = "private"
	}
	return ConvRef{ID: id, Name: a.S.ChannelName(id), Kind: kind}
}

func (a *App) convRefs(ids map[string]bool) map[string]ConvRef {
	out := make(map[string]ConvRef, len(ids))
	for id := range ids {
		if id != "" {
			out[id] = a.ConvRef(id)
		}
	}
	return out
}

var systemSubtypes = map[string]bool{
	"channel_join": true, "channel_leave": true, "channel_topic": true, "channel_purpose": true,
	"channel_name": true, "channel_archive": true, "channel_unarchive": true, "group_join": true,
	"group_leave": true, "group_topic": true, "group_purpose": true, "group_name": true,
	"pinned_item": true, "unpinned_item": true, "channel_canvas_updated": true, "channel_posting_permissions": true,
	"channel_convert_to_private": true, "channel_convert_to_public": true, "bookmark_added": true,
}

// message converts a Slack message for the UI.
func (a *App) message(ch string, m *slack.Message) Message { return a.messageWith(ch, m, false) }

// messageWith is message; with peek its member reads don't count as a
// view's (StoredMessage: a message converted again because a name in it
// changed, not because anything draws it).
func (a *App) messageWith(ch string, m *slack.Message, peek bool) Message {
	v := Message{
		TS: m.TS, Channel: firstNonEmpty(ch, m.Channel), User: m.User, BotID: m.BotID, Subtype: m.Subtype, Text: m.Text,
		Edited: m.Edited != nil, ThreadTS: m.ThreadTS, ReplyCount: m.ReplyCount, ReplyUsers: m.ReplyUsers,
		LatestReply: m.LatestReply, Pinned: len(m.PinnedTo) > 0, Huddle: m.Room,
		Broadcast: m.Subtype == "thread_broadcast" || m.ReplyBroadcast, System: systemSubtypes[m.Subtype],
		Tombstone: m.Subtype == "tombstone",
	}
	v.NewerReplies = a.newerReplies(ch, m) // broadcast_footer.go
	if m.Edited != nil {
		v.EditedTS, v.EditedBy = m.Edited.TS, m.Edited.User
	}
	v.Inviter, v.Topic, v.Purpose, v.OldName, v.NewName = m.Inviter, m.Topic, m.Purpose, m.OldName, m.NewName
	v.ReplyUserIcons = m.ReplyUserIcons
	v.ClientMsgID = m.ClientMsgID
	v.Blocks, v.Attachments, v.Metadata = m.Blocks, m.RawAttachments, m.Metadata
	v.StreamingState = m.StreamingState
	v.WorkflowID, v.TriggerID = m.WorkflowID, m.TriggerID
	v.AgentSession, v.AssistantThread = m.AgentSession, m.AssistantAppThread
	if c := m.AIContext; c != nil && c.ResultStatus == "stopped_by_user" { // vshb getMessageLabelValues
		v.Stopped, v.StoppedBy = true, c.StoppedByUserID
	}
	v.FunctionExecutionID = m.FunctionExecutionID
	v.Locked, v.LockedByTeam = m.IsLocked, m.LockedByTeamID
	if dc := m.DocumentComment; dc != nil {
		c := *dc
		c.Authors = append([]string(nil), dc.Authors...)
		v.DocumentComment = &c
	}
	if dm := m.DocumentMention; dm != nil {
		c := *dm
		c.MentioningUserIDs = append([]string(nil), dm.MentioningUserIDs...)
		v.DocumentMention = &c
		if dm.FileID != "" {
			// maybePrefetchCanvasFromNotification (boot-render): the
			// mention's canvas is asked for (files.info), so a jump to the
			// mention opens it and Activity names it (canvas_channel.go)
			a.S.CanvasRef(dm.FileID)
		}
	}
	if v.Attachments == nil && len(m.Attachments) > 0 { // built in memory (not decoded from Slack)
		v.Attachments, _ = json.Marshal(m.Attachments)
	}
	if bp := m.BotProfile; bp != nil {
		v.BotProfile = &BotProfile{ID: bp.ID, Name: bp.Name, AppID: bp.AppID, UserID: bp.UserID, TeamID: bp.TeamID,
			Icon: firstNonEmpty(bp.Icons.Image72, bp.Icons.Image48, bp.Icons.Image36), Deleted: bp.Deleted}
	}
	v.Pending, v.Failed = a.pendingState(m)
	v.Ephemeral = m.IsEphemeral
	v.NoDisplay, v.NoNotifications = m.NoDisplay, m.NoNotifications
	if m.PinnedInfo != nil {
		// getMessagePinnedInfo (client-boot): the label's pinnedBy is pinned_info.pinned_by
		v.PinnedBy = m.PinnedInfo.PinnedBy
		v.Pinned = true
	}
	// the thread's subscription, whichever copy of the root this is
	// (store/thread_subs.go: Slack's threadSub before the root's own)
	sub := a.S.ThreadSubOf(v.Channel, m)
	v.FollowingThread, v.ThreadLastRead, v.ReplyUsersN = sub.Subscribed, sub.LastRead, m.ReplyUsersN
	a.mu.Lock()
	v.Saved = a.saved[v.Channel+"/"+m.TS]
	a.mu.Unlock()
	a.senderOf(&v, m) // Slack ignores display_as_bot (sender.go)
	self := a.S.SelfID()
	for _, re := range m.Reactions {
		rv := Reaction{Name: re.Name, Count: re.Count, Users: re.Users, URL: re.URL}
		if rv.URL == "" {
			rv.URL, _, _ = a.S.Emoji(re.Name)
		}
		for _, u := range re.Users {
			if u == self {
				rv.Self = true
			}
		}
		v.Reactions = append(v.Reactions, rv)
	}
	v.Reactions = a.overlayReactions(ch, m.TS, v.Reactions)
	// mentions ask as they draw (lazy_users.go); in ch, a slug to one of
	// ch's tabs is the tab alone; Refs: what it read (msg_refs.go)
	rn := refNames{msgNames: msgNames{a.S}, app: a, peek: peek}
	v.Doc = richtext.FromMessageIn(&rn, m, ch)
	v.Refs = rn.refs
	// what senderOf and the reactions read besides: the sending bot's
	// name, the reactions' emoji
	if m.User == "" && m.BotID != "" {
		addRef(&v.Refs.Bots, m.BotID)
	}
	for _, re := range m.Reactions {
		addRef(&v.Refs.Emoji, re.Name)
	}
	return v
}

func (a *App) messagePtr(ch string, m *slack.Message) *Message {
	if m == nil || m.TS == "" {
		return nil
	}
	v := a.message(ch, m)
	return &v
}

// timeline converts top-level messages (and thread broadcasts).
func (a *App) timeline(ch string, msgs []slack.Message) []Message {
	out := make([]Message, 0, len(msgs))
	for i := range msgs {
		m := &msgs[i]
		if m.Subtype == "thread_broadcast" || m.ThreadTS == "" || m.ThreadTS == m.TS || m.ReplyBroadcast {
			out = append(out, a.message(ch, m))
		}
	}
	return out
}

func (a *App) messages(ch string, msgs []slack.Message) []Message {
	out := make([]Message, 0, len(msgs))
	for i := range msgs {
		out = append(out, a.message(firstNonEmpty(ch, msgs[i].Channel), &msgs[i]))
	}
	return out
}

// Mrkdwn renders arbitrary mrkdwn (previews, topics, command responses).
func (a *App) Mrkdwn(text string) []richtext.Block { return richtext.FromMrkdwn(a.S, text) }

// Emoji resolves an emoji name (custom or standard) to an image URL.
func (a *App) Emoji(name string) (url, text string, ok bool) { return a.S.Emoji(name) }

// EmojiVersion changes whenever the custom emoji map does (emoji.list,
// emoji_changed): a surface that converted emoji earlier converts again.
func (a *App) EmojiVersion() uint64 { return a.S.CustomEmoji.Version() }

// CustomEmoji returns the workspace's custom emoji: name -> URL or
// "alias:other". The map must not be modified.
func (a *App) CustomEmoji() map[string]string { return a.S.CustomEmoji.Snapshot() }

// ---- sidebar sections ----

// orderSections is Slack's updatedOrderedChannelSectionList (modern.vendor
// @14759265): follow next_channel_section_id from every section nothing
// points to (or from the first, if all are pointed to), longest chain first,
// and keep each section's first position. Sections outside every chain get
// position 0, so they sort with the first one (lodash sortBy quirk).
func orderSections(secs []slack.Section) []slack.Section {
	if len(secs) == 0 {
		return nil
	}
	next := map[string]string{}
	byID := map[string]slack.Section{}
	heads := map[string]bool{}
	var headOrder []string
	for _, s := range secs {
		if _, ok := byID[s.ID]; !ok {
			next[s.ID] = s.Next
			byID[s.ID] = s
		}
		if !heads[s.ID] {
			heads[s.ID] = true
			headOrder = append(headOrder, s.ID)
		}
	}
	for _, s := range secs {
		if s.Next != "" && heads[s.Next] {
			delete(heads, s.Next)
		}
	}
	if len(heads) == 0 {
		heads[secs[0].ID] = true
	}
	var chains [][]string
	for _, h := range headOrder {
		if !heads[h] {
			continue
		}
		seen := map[string]bool{}
		var chain []string
		for cur := h; cur != "" && !seen[cur]; cur = next[cur] {
			if _, ok := byID[cur]; ok {
				chain = append(chain, cur)
			}
			seen[cur] = true
		}
		chains = append(chains, chain)
	}
	sort.SliceStable(chains, func(i, j int) bool { return len(chains[i]) > len(chains[j]) })
	index := map[string]int{}
	for _, ch := range chains {
		for _, id := range ch {
			if _, ok := index[id]; !ok {
				index[id] = len(index)
			}
		}
	}
	// sortBy(sections, s => index[s.id] || 0): sections outside every chain
	// tie with the first one and keep their API order (stable sort).
	var out []slack.Section
	seen := map[string]bool{}
	for _, s := range secs {
		if !seen[s.ID] {
			seen[s.ID] = true
			out = append(out, byID[s.ID])
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return index[out[i].ID] < index[out[j].ID] })
	return out
}

// sections orders sections by their linked list and assigns every
// conversation to exactly one section (custom > default buckets).
func (a *App) sections(convs []Conv) []Section {
	a.S.RLock()
	secs := append([]slack.Section(nil), a.S.Sections...)
	a.S.RUnlock()
	ordered := orderSections(secs)
	// orderedChannelSectionList (client-boot Q): with
	// agents_in_channels_improvements on, Agents sections leave the list
	// (and lJBI's isAgent map is empty, so nothing is filed there); what
	// one listed goes to its default section.
	if a.AgentsInChannelsImprovements() {
		kept := ordered[:0:0]
		for _, s := range ordered {
			if s.Type != "agents" {
				kept = append(kept, s)
			}
		}
		ordered = kept
	}
	// Slack's local sections (client-boot z/Q): with session_channels on, a
	// missing Code channels section is made on the client and spliced in
	// at the front of the ordered list.
	if a.sessionChannelsOn() && !hasSectionType(ordered, "coding_channels") {
		ordered = append([]slack.Section{{ID: LocalCodingChannelsID, Name: "Code channels", Type: "coding_channels"}}, ordered...)
	}
	assigned := map[string]bool{}
	var out []Section
	chIdx, dmIdx, appIdx, codeIdx := -1, -1, -1, -1
	a.mu.Lock()
	var starred []string
	for id := range a.starred {
		starred = append(starred, id)
	}
	a.mu.Unlock()
	names := map[string]string{}
	for _, c := range convs {
		names[c.ID] = a.sortName(c)
	}
	temp := a.tempChannels(time.Now()) // temporary memberships (preview.go)
	secOf := assignSections(ordered, starred, temp)
	sel := a.selectedID()
	// Slack's candidates: closed DMs leave every section (sidebar_rules.go)
	// unless they have a draft or a huddle on, except the open one
	// (currentlySelectedId, preview.go)
	keep := a.sidebarKeeps()
	isDMKind := func(id string) bool {
		c, ok := a.S.Conv(id)
		return ok && (c.IsIM || c.IsMPIM)
	}
	visibleDM := func(id string) bool { return id == sel || !isDMKind(id) || a.dmCandidate(id, keep) }
	// a canvas's or list's conversation (sidebar_docs.go, lJBI): only with
	// GH on and where a section, a star or a temporary join puts it, or
	// while it is the open conversation
	fileRows := a.fileChannelsInSidebarOn()
	// External Connections (uSoP getChannelSectionType): with
	// should_show_connect_section on and a slack_connect section, every
	// externally shared conversation outside a custom section goes there;
	// off, the section isn't shown and they stay in Channels / DMs.
	showConnect := a.connectSectionOn(ordered)
	connectIdx := -1
	// VIP unreads: Slack's client-local section, first, taking its
	// conversations from every other section (prefs_a.go)
	prio := a.priorityChannels(convs)
	if len(prio) > 0 && !hasSectionType(ordered, "priority") {
		ordered = append([]slack.Section{{ID: PrioritySectionID, Name: "VIP unreads", Type: "priority"}}, ordered...)
	}
	for id := range prio {
		assigned[id] = true // nothing else lists them
	}
	for _, s := range ordered {
		switch s.Type {
		case "slack_connect":
			if !showConnect {
				continue
			}
		case "salesforce_records", "agents":
			if len(s.ChannelIDsPage.ChannelIDs) == 0 {
				continue
			}
		case "temporary_channels": // shown while the viewer has a temporary channel (lJBI)
			if len(s.ChannelIDsPage.ChannelIDs) == 0 && len(temp) == 0 {
				continue
			}
		}
		sv := Section{ID: s.ID, Name: s.Name, Type: s.Type, Emoji: s.Emoji, Channels: []string{}, Collapsed: a.sectionCollapsed(s)}
		if s.Type == "priority" { // VIP unreads: hidden while empty
			if len(prio) == 0 {
				continue
			}
			for _, c := range convs {
				if prio[c.ID] {
					sv.Channels = append(sv.Channels, c.ID)
				}
			}
			if sv.Name == "" {
				sv.Name = "VIP unreads"
			}
			out = append(out, sv)
			continue
		}
		if sv.Emoji != "" {
			sv.EmojiURL, _, _ = a.S.Emoji(strings.Trim(sv.Emoji, ":"))
		}
		ids := s.ChannelIDsPage.ChannelIDs
		switch s.Type {
		case "stars": // starred membership comes from stars.list
			ids = append(append([]string(nil), ids...), starred...)
		case "temporary_channels": // membership comes from the temporary channel store
			ids = append([]string(nil), ids...)
			for id := range temp {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			if sv.Name == "" {
				sv.Name = "Temporary channels"
			}
		}
		for _, id := range ids {
			if s.Type == "direct_messages" && a.isAppDM(id) {
				continue // Slack lists bot/app DMs under Apps
			}
			if !visibleDM(id) {
				continue
			}
			if secOf[id] != s.ID {
				continue // Slack files it in another section (starred first)
			}
			if c, ok := a.sidebarConv(id); ok && !assigned[id] {
				if IsFileChannel(c) {
					if !fileRows && id != sel {
						continue
					}
					a.ensureSidebarFile(FileOfChannel(id))
				}
				sv.Channels = append(sv.Channels, id)
				assigned[id] = true
			}
		}
		switch s.Type {
		case "channels":
			chIdx = len(out)
			if sv.Name == "" {
				sv.Name = "Channels"
			}
		case "direct_messages":
			dmIdx = len(out)
		case "stars":
			sv.Name = "Starred"
		case "slack_connect":
			if connectIdx < 0 {
				connectIdx = len(out)
			}
			sv.Name = "External connections" // a static name (p4Ef getChannelSectionNameIfStatic)
		case "recent_apps":
			// Apps sits where the user's section list puts it, not last.
			appIdx = len(out)
			sv.Name = "Apps"
		case "coding_channels":
			if codeIdx < 0 {
				codeIdx = len(out)
			}
			sv.Name = a.CodeChannelsSectionName(sv.Name) // a static name (p4Ef), code_channels_naming
		}
		out = append(out, sv)
	}
	if chIdx < 0 {
		out = append(out, Section{ID: "channels", Name: "Channels", Type: "channels"})
		chIdx = len(out) - 1
	}
	if dmIdx < 0 {
		out = append(out, Section{ID: "dms", Name: "Direct messages", Type: "direct_messages"})
		dmIdx = len(out) - 1
	}
	out[dmIdx].Name = "Direct messages"
	var apps []string
	if appIdx >= 0 {
		apps = out[appIdx].Channels
	}
	for _, c := range convs {
		if assigned[c.ID] {
			continue
		}
		if c.Kind == "im" && c.User == slackbotID && a.slackbotDMHidden() {
			continue // lJBI: no default section for Slackbot's DM while its tab or link shows, open or not
		}
		if c.File { // lJBI: no default section for a file channel, unless it is open
			if c.ID != sel {
				continue
			}
			a.ensureSidebarFile(c.FileID)
		}
		if c.Code != nil && c.ID != sel && a.codeChannelClosed(c.ID) {
			continue // lJBI keeps a closed record channel only when a section lists it, or starred or temporary (codechannel_close.go)
		}
		if c.Code != nil && codeIdx >= 0 { // getDefaultSectionType: code channels before any other bucket
			out[codeIdx].Channels = append(out[codeIdx].Channels, c.ID)
			continue
		}
		if connectIdx >= 0 && a.extShared(c.ID) && visibleDM(c.ID) { // then Slack Connect, before agents, apps and DMs
			out[connectIdx].Channels = append(out[connectIdx].Channels, c.ID)
			continue
		}
		switch c.Kind {
		case "im", "mpim":
			if !visibleDM(c.ID) {
				continue // closed (or inactive): Slack keeps it out of the sidebar
			}
			if a.isAppDM(c.ID) && !a.AgentInDMSidebar(c.ID) {
				if !a.emptyDM(c.ID) { // Slack hides app DMs that never had a message (e.g. Slackbot)
					apps = append(apps, c.ID)
				}
			} else {
				out[dmIdx].Channels = append(out[dmIdx].Channels, c.ID)
			}
		default:
			out[chIdx].Channels = append(out[chIdx].Channels, c.ID)
		}
	}
	// External connections shows while any candidate is externally shared
	// (lJBI's visibleChannelSections: showConnectSection &&
	// hasSharedChannels), even when a custom section or Starred holds all
	// of them and it has no rows of its own
	if connectIdx >= 0 && !anySectionConv(out, a.extShared) {
		out = append(out[:connectIdx], out[connectIdx+1:]...)
		if appIdx > connectIdx {
			appIdx--
		}
	}
	switch {
	case appIdx >= 0 && len(apps) > 0:
		out[appIdx].Channels = apps
	case appIdx >= 0: // no app DMs: Slack shows no Apps section (shouldSeparateApps)
		out = append(out[:appIdx], out[appIdx+1:]...)
	case len(apps) > 0:
		out = append(out, Section{ID: "apps", Name: "Apps", Type: "apps", Channels: apps})
	}
	// Starred: with nothing starred among the candidates Slack shows it
	// (empty) only where emptyStarredShown says so, and gives it m1KW's
	// banner (starredBanner)
	isStar := starredSet(ordered, starred)
	hasStar := anySectionConv(out, func(id string) bool { return isStar[id] })
	for i := 0; i < len(out); i++ {
		if out[i].Type != "stars" {
			continue
		}
		if !hasStar && !a.emptyStarredShown(ordered) {
			out = append(out[:i], out[i+1:]...)
			i--
			continue
		}
		out[i].StarredBanner = a.starredBanner(ordered, isStar)
	}
	// users.channelSections stores membership only; Slack filters and sorts
	// each section from prefs and experiments (sidebar_rules.go).
	a.applySectionRules(out, names, keep, time.Now())
	return out
}

// LocalCodingChannelsID is the id Slack gives its client-made Code
// channels section (WHwP: local_coding_channels).
const LocalCodingChannelsID = "local_coding_channels"

func hasSectionType(secs []slack.Section, typ string) bool {
	for _, s := range secs {
		if s.Type == typ {
			return true
		}
	}
	return false
}

// isAppDM reports whether ch is a DM with Slackbot, the "Slack" system
// bot or an app (these sort under Apps, as in Slack).
// AgentInDMSidebar is uXg5 oK (isAgentInDmSidebar, getDefaultSectionType
// files the DM under Direct messages instead of Apps): agents_in_dm_sidebar
// on, the agent not hidden there (shouldHideAgentsInDmSidebar:
// agents_in_dm_sidebar_pref on and Navigation > "Show Agents in direct
// messages" off, users.prefs hide_agents_in_dm_list), and an eligible agent
// DM: with support_lots_of_agents off, a bot member Slack badges AGENT
// (/ly8 hh) that isn't an Agentforce bot; with it on, the member's
// agent_status.
func (a *App) AgentInDMSidebar(ch string) bool {
	c, ok := a.S.Conv(ch)
	if !ok || !c.IsIM {
		return false
	}
	return a.agentInDMSidebarUser(c.User)
}

// agentInDMSidebarUser is uXg5 oK by the IM's member (the DMs tab asks it
// for DMs core may not hold, closed ones from client.dms).
func (a *App) agentInDMSidebarUser(uid string) bool {
	if a.experiment("agents_in_dm_sidebar") != "on" || a.HideAgentsInDMs() {
		return false
	}
	u, ok := a.User(uid)
	if !ok || u.Agentforce {
		return false
	}
	return a.memberIsAgent(u)
}

// HideAgentsInDMs is shouldHideAgentsInDmSidebar.
func (a *App) HideAgentsInDMs() bool {
	return a.experiment("agents_in_dm_sidebar_pref") == "on" && a.PrefBool("hide_agents_in_dm_list", false)
}

func (a *App) isAppDM(ch string) bool {
	c, ok := a.S.Conv(ch)
	if !ok || !c.IsIM {
		return false
	}
	switch c.User {
	case "USLACKBOT", "USLACK":
		return true
	}
	// raw: lJBI decides from imAndMpimMembers, which the snapshot reads
	// through getMemberById for the DMs it lists (sidebarUsers)
	u, ok := a.S.RawUser(c.User)
	return ok && (u.IsBot || u.IsAppUser || u.Profile.BotID != "" || u.Profile.APIAppID != "")
}

// emptyDM reports whether a DM is known to have no messages at all.
func (a *App) emptyDM(ch string) bool {
	e, ok := a.S.Count(ch)
	if !ok || e.HasUnreads {
		return false
	}
	return e.Latest == "" || strings.Trim(e.Latest, "0.") == ""
}

// ---- presence ----

func (a *App) onHello() {
	ids := a.presenceIDs(nil)
	if err := a.RTM.SubscribePresence(ids); err != nil {
		log.Printf("rtm: presence_sub: %v", err)
	}
	if err := a.RTM.SubscribeUsers(ids); err != nil {
		log.Printf("rtm: user subscribe: %v", err)
	}
}

// presenceIDs merges add into the subscribed set (self + DM partners +
// recently shown users, newest first, capped) and returns the full set.
func (a *App) presenceIDs(add []string) []string {
	base := map[string]bool{a.C.Creds.UserID: true}
	a.S.RLock()
	for _, c := range a.S.Convs {
		if c.IsIM && c.User != "" {
			base[c.User] = true
		}
	}
	a.S.RUnlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	for id := range a.presSubs { // members someone subscribed to (presence_subs.go)
		base[id] = true
	}
	now := time.Now()
	for _, id := range add {
		if strings.HasPrefix(id, "U") || strings.HasPrefix(id, "W") {
			a.presSeen[id] = now
		}
	}
	type kv struct {
		id string
		t  time.Time
	}
	var recent []kv
	for id, t := range a.presSeen {
		if !base[id] {
			recent = append(recent, kv{id, t})
		}
	}
	sort.Slice(recent, func(i, j int) bool { return recent[i].t.After(recent[j].t) })
	const maxSubs = 500
	out := make([]string, 0, len(base)+len(recent))
	for id := range base {
		out = append(out, id)
	}
	sort.Strings(out)
	for _, r := range recent {
		if len(out) >= maxSubs {
			delete(a.presSeen, r.id)
			continue
		}
		out = append(out, r.id)
	}
	return out
}

// SubscribePresence adds users the UI is showing to the realtime presence
// (and profile change) subscription. Updates arrive as "presence"/"user" events.
func (a *App) SubscribePresence(ids []string) {
	if len(ids) == 0 {
		return
	}
	a.mu.Lock()
	fresh := false
	for _, id := range ids {
		if _, ok := a.presSeen[id]; !ok && id != "" {
			fresh = true
			break
		}
	}
	a.mu.Unlock()
	all := a.presenceIDs(ids)
	if fresh && a.RTM.Connected() {
		_ = a.RTM.SubscribePresence(all)
		_ = a.RTM.SubscribeUsers(all)
	}
}

func (a *App) presenceArrived() {
	a.mu.Lock()
	ws := a.presWaiters
	a.presWaiters = nil
	a.mu.Unlock()
	for _, c := range ws {
		close(c)
	}
}

func (a *App) presenceMap(ids []string) (map[string]string, []string) {
	out := map[string]string{}
	var missing []string
	a.S.RLock()
	for _, id := range ids {
		if p, ok := a.S.Presence[id]; ok {
			out[id] = p
		} else if id != "" {
			missing = append(missing, id)
		}
	}
	a.S.RUnlock()
	return out, missing
}

// Presence returns the presence of ids (active | away). Unknown users are
// subscribed and awaited for up to 1.5 s (the gateway answers a
// subscription with presence_change events).
func (a *App) Presence(ctx context.Context, ids []string) map[string]string {
	out, missing := a.presenceMap(ids)
	if len(missing) == 0 {
		return out
	}
	a.SubscribePresence(missing)
	if !a.RTM.Connected() {
		return out // presence only arrives over the realtime socket
	}
	deadline := time.After(1500 * time.Millisecond)
	for len(missing) > 0 {
		c := make(chan struct{})
		a.mu.Lock()
		a.presWaiters = append(a.presWaiters, c)
		a.mu.Unlock()
		select {
		case <-c:
		case <-deadline:
			return out
		case <-ctx.Done():
			return out
		}
		out, missing = a.presenceMap(ids)
	}
	return out
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

func chanID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s)
		return s
	}
	var c struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &c)
	return c.ID
}
