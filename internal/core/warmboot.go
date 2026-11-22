package core

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/some-du6e/clack/internal/applog"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// Warm boot, the way Slack's client does it (client-boot, build
// 2c0d02ca130f):
//
//   - getContextualInfoForTeamId asks for the persisted store of the team
//     and user (maybeGetStoredState → getStoredStateIfValid: IndexedDB,
//     keyed by both; skipped when the team expired, the store is older
//     than the minimum data version, or it holds no bootData) and
//     isBootingWarm is whether there was one;
//   - runBootTeam then renders straight from it: the store is set up from
//     the persisted state (dataSetupStore, setInitialState) and renderApp
//     waits only for that, the translations and the render module. Every
//     registered boot API is "nonblocking" on a warm boot
//     (getBlockingStatus(e,t){return t?"nonblocking":e}), so no boot call
//     holds the first render; each one's answer is dispatched into the
//     store when it comes.
//
// Clack does the same with the part of that store the first frame shows
// (store/bootdata.go): Boot reads boot_data.json and headers.json while
// meta.json decodes and, once all three are in, installs the boot data
// (only what Boot hasn't answered yet), builds a snapshot from it and
// closes DataReady. The UI renders that snapshot (WarmSnapshot) and lifts
// the boot placeholder. Each of Boot's sidebar answers then replaces its
// part when it arrives, as Slack dispatches each one: the conversations
// (cached ones Slack no longer lists leave), counts, sections,
// sidebar.dms and stars. A call that fails keeps the cached part, as a
// failed refresh keeps Slack's. The window is usable meanwhile, so each
// answer is merged with what this session changed after its call went out
// (store/journal.go): a mark, a leave, a DM opened or closed, a section
// move made then stays.
//
// Not copied: Slack boots cold again once its last cold boot is a week
// old (hasBeenTooLongSinceLastColdBoot, cold_boot_interval_elapsed),
// because its warm boot fetches conversations incrementally
// (fetchClientChannelsForBoot's minChannelUpdated) and can drift. Clack's
// warm boot refetches every list whole, so nothing drifts.

type warmState struct {
	// dataReady closes once the sidebar's data is in: the boot data
	// installed (a warm start) or Boot finished (a cold one).
	dataReady chan struct{}
	// deferRest: meta.json's custom emoji and user groups load after the
	// window's first real frame (DeferCacheRest), restReady closes for it
	// (FirstFrameDone, or a cold start, which has no warm frame to keep
	// clear); restOnce closes it
	deferRest atomic.Bool
	restReady chan struct{}
	restOnce  sync.Once
	readyOnce sync.Once
	// done closes once the warm load resolved, with or without boot data.
	done chan struct{}

	mu    sync.Mutex
	warm  bool                           // this session started from boot data
	snap  *Snapshot                      // the sidebar from the boot data, until Boot's answers are in (nil: a cold start)
	added map[string]*slack.Conversation // the conversations it installed (store.DropBootConvs), until then too
	// bootOK: Boot answered with the conversation list in this session,
	// so what core holds is worth keeping (a failed start never replaces
	// good boot data)
	bootOK bool
	saved  [sha256.Size]byte // what boot_data.json last got (unchanged data isn't written again)
	wg     sync.WaitGroup    // Boot's delayed save (Shutdown waits for it)

	headers headerCache // header_cache.go
}

// noWarmBoot (CLACK_NO_WARM_BOOT=1) starts cold even with boot data on
// disk, the way Clack started before it read it (the perf workflow's
// instrumented "before").
var noWarmBoot = os.Getenv("CLACK_NO_WARM_BOOT") != ""

// DataReady is closed once the sidebar's data is in: on a warm start when
// the boot data is installed (a disk read, before any boot call answers),
// otherwise when Boot finished. Ready closes at the same time or later.
func (a *App) DataReady() <-chan struct{} { return a.warm.dataReady }

func (a *App) markDataReady() { a.warm.readyOnce.Do(func() { close(a.warm.dataReady) }) }

// WarmSnapshot is the sidebar from the boot data, once the warm load
// resolved: it waits for the disk reads (meta.json, boot_data.json), never
// for the network. ok is false on a cold start. Call it off the UI
// goroutine.
func (a *App) WarmSnapshot() (Snapshot, bool) {
	select {
	case <-a.warm.done:
	case <-a.ready: // ServeOffline, or a Boot that never loaded
	}
	a.warm.mu.Lock()
	defer a.warm.mu.Unlock()
	if a.warm.snap == nil {
		return Snapshot{}, false
	}
	return *a.warm.snap, true
}

// Warm reports whether this session started from boot data.
func (a *App) Warm() bool {
	a.warm.mu.Lock()
	defer a.warm.mu.Unlock()
	return a.warm.warm
}

// bootAccount is the account the boot data belongs to: the session's
// workspace (the org on Grid) and user, as the cache directory is named.
func (a *App) bootAccount() (team, user string) {
	return firstNonEmpty(a.C.Creds.Team, a.C.Creds.EnterpriseID), a.C.Creds.UserID
}

// cacheRestMax is how long meta.json's custom emoji and user groups wait
// at most for the window's first real frame (one started hidden draws
// none).
const cacheRestMax = 1500 * time.Millisecond

// DeferCacheRest makes Boot load meta.json's custom emoji and user groups
// (6.6 MB and 0.4 MB on Hack Club) after the window's first real frame
// (FirstFrameDone), not alongside the first part, which the warm
// snapshot reads: the first frame doesn't draw them, and decoding them
// took the CPU it needs. Call it before Boot (the UI's real window).
func (a *App) DeferCacheRest() { a.warm.deferRest.Store(true) }

// FirstFrameDone tells core the window's first real frame is up: what
// DeferCacheRest held back loads now. Any goroutine, any number of times.
func (a *App) FirstFrameDone() { a.warm.restOnce.Do(func() { close(a.warm.restReady) }) }

// waitCacheRest waits, when the UI deferred it, for the first real frame
// (FirstFrameDone), at most cacheRestMax.
func (a *App) waitCacheRest(ctx context.Context) {
	if !a.warm.deferRest.Load() {
		return
	}
	t := time.NewTimer(cacheRestMax)
	defer t.Stop()
	select {
	case <-a.warm.restReady:
	case <-t.C:
	case <-ctx.Done():
	}
}

// loadWarm is Boot's warm load: boot_data.json and headers.json are read
// alongside meta.json's first part (cacheBase), then the boot data is
// installed and its snapshot built. It closes a.warm.done.
func (a *App) loadWarm(cacheBase <-chan struct{}) {
	defer close(a.warm.done)
	t0 := time.Now()
	team, user := a.bootAccount()
	var d *store.BootData
	if !noWarmBoot {
		d = a.S.ReadBootData(team, user)
		a.loadHeaders(team, user)
	} else {
		a.loadHeaders("", "") // none: headers come from their reads, as before the warm boot
	}
	<-cacheBase // the users, prefs and experiments the snapshot reads
	switch {
	case noWarmBoot:
		a.FirstFrameDone() // a cold start: nothing to hold meta.json's rest for
		applog.Mark("cache-read", "cold: CLACK_NO_WARM_BOOT")
		return
	case d == nil:
		a.FirstFrameDone()
		applog.Mark("cache-read", "cold: no boot data")
		return
	case !a.S.HasCachedPrefs(): // meta.json didn't load: the rows' names and rules would differ
		a.FirstFrameDone()
		applog.Mark("cache-read", "cold: no metadata cache")
		return
	}
	// the custom emoji the first frame draws (the sections' icons, status
	// emoji, tab bars' bookmarks): meta.json's full map comes after it
	a.S.InstallBootEmoji(d.Emoji)
	added := a.S.InstallBootData(d)
	a.installWarmCore(d)
	a.InvalidateSnapshot()
	snap := a.snapshotNow() // here, off the UI goroutine
	a.warm.mu.Lock()
	a.warm.snap, a.warm.added, a.warm.warm = &snap, added, true
	a.warm.mu.Unlock()
	a.markDataReady()
	// the boot data is in: plugins' state mirror re-reads it, as at
	// "booted" (Slack's dataAfterInitialStoreHydration)
	a.emit(Event{Type: "hydrated"})
	applog.Mark("cache-read", fmt.Sprintf("warm: %d convs, %d sections in %s, saved %s ago", len(snap.Convs), len(snap.Sections),
		time.Since(t0).Round(time.Millisecond), time.Since(time.Unix(d.SavedAt, 0)).Round(time.Second)))
}

// installWarmCore installs boot data's core parts, each only while Boot
// hasn't answered it: sidebar.dms' open flags, the starred conversations
// and the Activity badge counts.
func (a *App) installWarmCore(d *store.BootData) {
	var closed []string
	a.mu.Lock()
	if d.DMsKnown && !a.dms.known {
		a.dms.known = true
		a.dms.open = make(map[string]bool, len(d.DMsOpen))
		a.dms.closedAt = map[string]string{}
		for id, open := range d.DMsOpen {
			a.dms.open[id] = open
			if !open {
				closed = append(closed, id)
			}
		}
	}
	if d.StarsKnown && !a.starsOK { // starsOK stays for stars.list itself
		for _, id := range d.Starred {
			a.starred[id] = true
		}
	}
	if a.actBadges == nil && d.ActivityBadges != nil {
		a.actBadges = make(map[string]int, len(d.ActivityBadges))
		for k, v := range d.ActivityBadges {
			a.actBadges[k] = v
		}
	}
	a.mu.Unlock()
	// closed DMs reopen on messages newer than their latest (setSidebarDMs)
	now := nowTS()
	for _, id := range closed {
		m := a.reopenMark(id, now)
		a.mu.Lock()
		if _, live := a.dms.closedAt[id]; !live && !a.dms.open[id] {
			a.dms.closedAt[id] = m
		}
		a.mu.Unlock()
	}
}

// installConvs installs a users.conversations answer fetched at the store
// journal's position since (Boot): every conversation, except the ones
// this session changed after since (store.PutConvsSince), then the boot
// data's conversations the answer no longer lists leave.
func (a *App) installConvs(convs []slack.Conversation, since uint64) {
	if n := a.S.PutConvsSince(convs, since); n > 0 {
		log.Printf("boot: %d conversations changed here while users.conversations was out keep that", n)
	}
	// cached conversations Slack no longer lists (left, archived elsewhere) go
	if n := a.S.DropBootConvs(a.warmAdded(), convs, since); n > 0 {
		log.Printf("boot: %d cached conversations no longer listed", n)
	}
}

// installCounts installs a client.counts answer fetched at the store
// journal's position since (Boot, refreshCounts): the unread state and
// the Activity badges, merged with what changed after since (a mark, a
// new message, items marked read: store.SetCountsSince). It reports
// whether it merged anything: the result is then partly older than
// Slack's state, worth reading once more.
func (a *App) installCounts(c *slack.Counts, since uint64) bool {
	merged := a.S.SetCountsSince(c, since)
	badges := c.ActivityV2 == nil || a.setActivityBadgesSince(c.ActivityV2, since)
	a.mergeChannelBadges(c.ChannelBadges) // h1EQ: updateChannelBadgesCounts, resetAllUserAlertsUnreadCounts (classic_badges.go)
	a.resetAlerts(c.Alerts)
	return merged > 0 || !badges
}

// warmAdded is the conversations the boot data installed (nil on a cold
// start).
func (a *App) warmAdded() map[string]*slack.Conversation {
	a.warm.mu.Lock()
	defer a.warm.mu.Unlock()
	return a.warm.added
}

// warmDone lets go of the warm boot's snapshot and cached conversations
// once Boot's answers replaced them (the UI applied the snapshot long
// before: it read it as soon as the disk reads were in).
func (a *App) warmDone() {
	a.warm.mu.Lock()
	a.warm.snap, a.warm.added = nil, nil
	a.warm.mu.Unlock()
}

// SaveWarm writes the warm boot's files when they changed: the boot data
// and the header cache (header_cache.go). Boot does it 20 s after it
// answered and then every 5 minutes, Close at the end, and the UI as the
// window closes. Disk I/O: call it off the UI goroutine.
func (a *App) SaveWarm() {
	a.saveBootData()
	a.saveHeaders()
}

// saveBootData writes boot_data.json from what core holds now: only after
// a Boot that answered (a failed start never replaces good data), never
// after sign-out, and not again while nothing in it changed.
func (a *App) saveBootData() {
	a.warm.mu.Lock()
	ok := a.warm.bootOK
	a.warm.mu.Unlock()
	if !ok || a.S.CacheDir() == "" || a.S.Dropped() || a.C.SignedOut() {
		return
	}
	t0 := time.Now()
	team, user := a.bootAccount()
	d := &store.BootData{Team: team, User: user}
	a.S.BootSidebar(d)
	d.Emoji = a.bootEmoji(d)
	sort.Slice(d.Convs, func(i, j int) bool { return d.Convs[i].ID < d.Convs[j].ID })
	sort.Slice(d.Counts, func(i, j int) bool { return d.Counts[i].ID < d.Counts[j].ID })
	a.mu.Lock()
	if a.dms.known {
		d.DMsKnown = true
		d.DMsOpen = make(map[string]bool, len(a.dms.open))
		for k, v := range a.dms.open {
			d.DMsOpen[k] = v
		}
	}
	if a.starsOK {
		d.StarsKnown = true
		for id := range a.starred {
			d.Starred = append(d.Starred, id)
		}
		sort.Strings(d.Starred)
	}
	if a.actBadges != nil {
		d.ActivityBadges = make(map[string]int, len(a.actBadges))
		for k, v := range a.actBadges {
			d.ActivityBadges[k] = v
		}
	}
	a.mu.Unlock()
	// SavedAt aside, the same data makes the same bytes (sorted lists,
	// maps marshal sorted): nothing to write
	probe := *d
	probe.SavedAt, probe.V = 0, store.CacheVersion
	b, err := json.Marshal(&probe)
	if err != nil {
		return
	}
	sum := sha256.Sum256(b)
	a.warm.mu.Lock()
	same := sum == a.warm.saved
	a.warm.mu.Unlock()
	if same {
		return
	}
	if err := a.S.WriteBootData(d); err != nil {
		log.Printf("cache: boot data: %v", err)
		return
	}
	a.warm.mu.Lock()
	a.warm.saved = sum
	a.warm.mu.Unlock()
	applog.Debugf("cache: boot data saved (%d convs, %d sections, %d counts) in %s", len(d.Convs), len(d.Sections), len(d.Counts),
		time.Since(t0).Round(time.Millisecond))
}

// bootEmoji is the custom emoji a launch's first frame draws
// (BootData.Emoji): the sections' icons, the sidebar DMs' people's and the
// viewer's status emoji, and the cached tab bars' bookmark icons and the
// emoji in their titles, as the full map has them (aliases with their
// targets).
func (a *App) bootEmoji(d *store.BootData) map[string]string {
	var names []string
	for _, s := range d.Sections {
		if s.Emoji != "" {
			names = append(names, s.Emoji)
		}
	}
	a.S.RLock()
	for _, c := range d.Convs {
		if u := a.S.Users[c.User]; c.IsIM && u != nil && u.Profile.StatusEmoji != "" {
			names = append(names, u.Profile.StatusEmoji)
		}
	}
	if e := a.S.Self.Profile.StatusEmoji; e != "" {
		names = append(names, e)
	}
	a.S.RUnlock()
	h := &a.warm.headers
	h.mu.Lock()
	for _, e := range h.m {
		for _, t := range e.Tabs {
			for _, b := range t.Bookmarks {
				if b.Emoji != "" {
					names = append(names, b.Emoji)
				}
			}
			for _, code := range statusEmojiCodeRE.FindAllString(t.Label+" "+t.Title, -1) {
				names = append(names, strings.SplitN(strings.Trim(code, ":"), "::", 2)[0])
			}
		}
	}
	h.mu.Unlock()
	return a.S.BootEmoji(names)
}

// setBootOK records that Boot answered with the conversation list.
func (a *App) setBootOK() {
	a.warm.mu.Lock()
	a.warm.bootOK = true
	a.warm.mu.Unlock()
}

// ---- waiting for Boot ----

// bootWaitLog is how long a call may wait for Boot before it is logged
// (Snapshot, BootErr): the UI goroutine must never wait, and a wait this
// long from it is a frozen window, so the log names the caller.
const bootWaitLog = 50 * time.Millisecond

var bootWaitHook atomic.Pointer[func(method, caller string)]

// SetBootWaitHook makes f hear about every call that found Boot
// unfinished and waits for it: the method and its caller ("file:line
// func"). nil removes it. For tests that check a path never waits.
func SetBootWaitHook(f func(method, caller string)) {
	if f == nil {
		bootWaitHook.Store(nil)
		return
	}
	bootWaitHook.Store(&f)
}

// waitBoot waits for Boot, for method's caller (two frames up).
func (a *App) waitBoot(method string) {
	select {
	case <-a.ready:
		return
	default:
	}
	caller := callerAt(3)
	if h := bootWaitHook.Load(); h != nil {
		(*h)(method, caller)
	}
	t0 := time.Now()
	<-a.ready
	if d := time.Since(t0); d > bootWaitLog {
		log.Printf("core: %s waited %dms for boot (%s)", method, d.Milliseconds(), caller)
	}
}

// callerAt is runtime.Caller(skip) as "file:line func", the file from the
// module root.
func callerAt(skip int) string {
	pc, file, line, ok := runtime.Caller(skip)
	if !ok {
		return "?"
	}
	if i := strings.Index(file, "/internal/"); i >= 0 {
		file = file[i+1:]
	} else if i := strings.Index(file, "/cmd/"); i >= 0 {
		file = file[i+1:]
	}
	fn := ""
	if f := runtime.FuncForPC(pc); f != nil {
		fn = f.Name()
		if i := strings.LastIndex(fn, "/"); i >= 0 {
			fn = fn[i+1:]
		}
	}
	return fmt.Sprintf("%s:%d %s", file, line, fn)
}
