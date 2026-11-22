package ui

import (
	"context"
	"fmt"
	"log"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/some-du6e/clack/internal/applog"
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/logtag"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slick"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/blockkit"
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/uiguard"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// backend adapts the in-process core service to the shell's model. Model
// mutations happen on the UI goroutine only (via App.Post).
type backend struct {
	core  *core.App
	taut  *taut.Taut   // Taut plugins (nil until boot)
	slick *slick.Slick // Slick plugins, on Taut's runtime (nil until boot)
	// tautStarting: startTautAsync is building the runtimes
	tautStarting bool
	// tautDeferJS: the real window's plugins (the JS engine) start after
	// the launch's first real frame (startDeferredTaut); the runtimes
	// themselves, with the theme and the user CSS, are made at once
	tautDeferJS bool
	// pluginStyled: this session's plugins added CSS of their own
	// (ui.json's plugin_styled for the next start)
	pluginStyled atomic.Bool

	m       model
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	unsub   func()
	// people: the viewer and the partners and members of the model's
	// DMs, app DMs and group DMs (apply), the people handle's presence
	// and profile changes can show; the event goroutine reads it (wants)
	people atomic.Pointer[map[string]bool]

	selfID string
	domain string
	teamID string
	notes  noteOrder            // notifications waiting for their lookups, in order (notify_conv.go)
	ext    map[string]*convInfo // ResolveConv results for convs outside the sidebar
	// extGen counts forgetExt calls per conversation: a resolve that
	// started before one is stale and must not land (resolved).
	extGen map[string]int
	// extStale marks ext entries refreshExt wants looked up again (their
	// copy shows meanwhile).
	extStale map[string]bool
	// extFailed marks conversations ResolveConv couldn't look up (offline,
	// channel_not_found): their membership stays unknown, and footerFor
	// stops holding the composer back for them.
	extFailed map[string]bool

	// unsent drafts (drafts.list, refreshed on "draft" events), UI goroutine
	// only: Slack's "✎ N" on Drafts & sent and the pencil on channel rows.
	draftN     int // unsent drafts (Drafts & sent's ✎ badge)
	schedN     int // scheduled messages (its schedule-send badge)
	draftChans map[string]bool
	draftSeq   int
	// chanDrafts are the drafts each row counts (the pencil's tooltip and
	// its Jump to draft… menu: sidebar_draft.go)
	chanDrafts map[string][]core.Draft

	tabCache map[string][]chTab
	tabSrc   map[string]tabRead // what each tab bar was built from (tabs)
	tabsDone map[string]bool    // the header shows what there is: opened() finished, or the header cache filled it (reveal.go)
	// headerReads is what this session's header reads of each
	// conversation brought (opened): whether one answered every part (the
	// header cache can't be newer then), the last tabs read and the
	// membership it saw. A read that failed or came back partial before
	// the header cache loaded leaves the cache to fill the gaps when it
	// does (headerFromCache). UI goroutine only.
	headerReads map[string]headerRead
	// pluginsReady closes when the plugins enabled at start have started
	// (any goroutine may wait on it)
	pluginsReady chan struct{}
	pluginsOnce  sync.Once
	bookmarks    map[string][]core.Bookmark
	fetched      map[string]time.Time
	// headerGen numbers each channel's header reads (opened): only the
	// newest one's answer lands. Each read posts once its three calls are
	// back, so an older one could post last: a read that started before a
	// Hide tab, its members call slow, put the hidden tab back on the bar
	// after the read the hide asked for (files-filters-3's push ci, run
	// 44980164338: reads of 1 tab, then 0 and 0, and the bar kept 1).
	headerGen map[string]int
	// headerStarted and headerAnswered count the header reads opened
	// started and the answers that came back to the UI goroutine (landed
	// or dropped): tests wait for every read out to have answered
	headerStarted, headerAnswered int
	// side is what opened() read for a conversation's header (facepile,
	// member count, topic), kept here so it lands whenever the
	// conversation's row exists: the restored channel's read usually
	// finishes before boot's first snapshot, and a channel outside the
	// sidebar lives in ext. UI goroutine only.
	side map[string]*chSide
	// drawn is when layoutChannel last drew each conversation (shown):
	// a pane that embeds one keeps its failed header reads retried.
	// UI goroutine only.
	drawn map[string]time.Time

	snapMu      sync.Mutex
	snapPending bool
	snapSeq     uint64 // the core.Snapshot.Seq applied last (apply skips older)

	booted time.Time // Boot's answers are applied
	// warmAt is when the boot data's sidebar was applied (a warm start:
	// core.WarmSnapshot), before Boot answered; the boot placeholder lifts
	// on it as on booted (reveal.go)
	warmAt    time.Time
	onSignOut func()
	app       *App

	// noteIgnoreDND are the messages ("channel/ts") a dnd_override showed
	// with Do Not Disturb ignored, Slack's _ignore_dnd; noteDNDHeld (and
	// its order) the message events DND held back lately, which a
	// dnd_override can show; noteShown and noteShownOrder the
	// notifications shown lately (notify_dm.go). UI goroutine.
	noteIgnoreDND      map[string]bool
	noteIgnoreDNDOrder []string
	noteDNDHeld        map[string]core.Event
	noteDNDHeldOrder   []string
	noteShown          map[string]bool
	noteShownOrder     []string
}

// A core call that waits for Boot on the UI goroutine freezes the window
// until Slack answers (a launch restored to Activity painted nothing for
// 2.3 s, 2026-11-15): uiguard logs it and records it for the tests
// (TestStartupStatesNeverBlock, TestUINeverWaitsForBoot). Core logs every
// wait over 50 ms with its caller too (warmboot.go).
func init() {
	core.SetBootWaitHook(func(method, caller string) {
		uiguard.OffUI("core." + method + " waiting for boot (" + caller + ")")
	})
}

// NewBackend wraps a core service. cacheDir is unused: the account's cache
// directory is the store's.
func newBackend(c *core.App, cacheDir string) *backend {
	b := &backend{core: c, pluginsReady: make(chan struct{}), tabCache: map[string][]chTab{}, bookmarks: map[string][]core.Bookmark{},
		fetched: map[string]time.Time{}, side: map[string]*chSide{}}
	b.m.Convs = map[string]*convInfo{}
	return b
}

// Attach connects the app to core. Call before Run, or after login.
func (a *App) Attach(c *core.App, cacheDir string) {
	a.data = newBackend(c, cacheDir)
	a.data.onSignOut = a.opts.SignOut
	if a.win != nil && a.login == nil {
		a.data.start(a)
	}
}

// Core returns the core service (Host contract).
func (a *App) Core() *core.App {
	if a.data == nil {
		return nil
	}
	return a.data.core
}

func (b *backend) start(a *App) {
	if b.started {
		return
	}
	b.started = true
	b.app = a
	b.ctx, b.cancel = context.WithCancel(context.Background())
	installMemberRead(b.core) // mentions ask for their members as they draw (lazy_users.go)
	// the sidebar rules keep a closed DM with a huddle on: they read the
	// huddle UI's rooms (huddle_wire.go sbHuddleSync rebuilds on a change)
	b.core.SetHuddleChannels(a.huddles().HuddleChannels)
	ch, unsub := b.core.Subscribe()
	b.unsub = unsub
	go func() {
		for ev := range ch {
			if !b.wants(ev) {
				continue
			}
			ev := ev
			a.Post(func() { b.handle(a, ev) })
		}
	}()
	// Live updates only when the user opted in (realtime.go); core polls
	// otherwise. Set before Boot so the socket is never opened unasked.
	b.core.SetRealtime(a.realtimeOn())
	a.attachOpenFiles(b.core)      // the Files sidebar's Recently viewed (files_open.go)
	if b.core.S.HasCachedPrefs() { // the cached account theme paints the first frame
		a.applyAccountPrefs(b.core.Prefs())
	}
	// Everything the first channel frame shows starts now, in parallel
	// with core's boot: the plugin runtimes (JS engine, bundles from the
	// bytecode cache, plugin start; their state mirror re-reads at
	// "booted"), Block Kit, the restored channel's header (tabs, bookmarks,
	// member count and facepile) and the drafts' sidebar markers. The
	// window stays covered until they're in (reveal.go).
	switch {
	case a.inert:
		// the introduction's preview renderer: no plugin system at all (no
		// Taut or Slick runtime, plugin config, storage or slots)
		b.pluginsOnce.Do(func() { close(b.pluginsReady) })
	default:
		if a.rv.inRun {
			// The real window's launch. The runtimes are made at once:
			// taut.New and slick.New install the user CSS and the Slick
			// theme (Go only, ms), which the reveal waits for, so the
			// client's first frame is in the theme. What last session's
			// plugins showed decides the rest (reveal.go): the components
			// they patched are watched for in the first frames, and when
			// they showed nothing (no patch, no CSS of their own) the
			// plugins (the JS engine) start after the first real frame,
			// so they don't take the CPU it needs; otherwise at once, as
			// the reveal may wait for them. A cold start (no boot data)
			// starts them at once too (below), and a window that draws
			// nothing (started hidden) after tautDeferMax.
			if !a.rv.done { // the launch's reveal, not a sign-in's later (it clears them)
				slackc.SetExpected(a.prefs.PluginPatched)
				// meta.json's custom emoji and user groups (6.6 MB and
				// 0.4 MB on Hack Club) load after the first real frame:
				// it doesn't draw them (the boot data has the few it
				// does), and decoding them took the CPU it needs
				b.core.DeferCacheRest()
			}
			if len(a.prefs.PluginPatched) == 0 && !a.prefs.PluginStyled {
				b.tautDeferJS = true
				time.AfterFunc(tautDeferMax, func() { a.Post(func() { b.startDeferredTaut(a) }) })
			}
		}
		b.startTautAsync(a)
	}
	b.startBlockkit(a)
	if !a.inert {
		go a.settlePluginSlots(b) // release reserved plugin slots, save the learned ones
	}
	if ch := a.router.Current().Channel(); ch != "" {
		b.opened(ch)
	}
	b.loadDrafts(a)
	// A warm start renders from the boot data, as Slack renders from its
	// persisted store, while Boot's calls are still out (core
	// warmboot.go): the sidebar, and the restored channel's header from
	// the header cache. Boot's answers replace them below.
	go func() {
		snap, warm := b.core.WarmSnapshot() // the disk reads only
		a.Post(func() {
			if a.data != b || b.ctx.Err() != nil {
				return // signed out or switched meanwhile
			}
			if warm && b.booted.IsZero() {
				b.apply(a, snap)
				b.warmAt = time.Now()
				applog.Mark("sidebar-cached", fmt.Sprintf("%d convs", len(snap.Convs)))
				if a.router.Current() == "" {
					a.openDefault()
				}
			}
			if b.warmAt.IsZero() {
				b.startDeferredTaut(a) // not a warm start: its placeholder waits for the plugins
			}
			if ch := a.router.Current().Channel(); ch != "" {
				b.headerFromCache(ch)
			}
		})
	}()
	go func() {
		t0 := time.Now()
		if err := b.core.Boot(b.ctx); err != nil {
			log.Printf("ui: boot: %v", err)
			a.Post(func() { a.Toast("Couldn't connect to Slack: " + err.Error()) })
		}
		snap := b.core.Snapshot()
		log.Printf("ui: core ready %v after boot start (%d convs)", time.Since(t0).Round(time.Millisecond), len(snap.Convs))
		go func() { // fetchUserDndInfo for the current member: the rail's status tab (rail_status.go)
			if b.core.LoadDND(b.ctx) == nil {
				a.Invalidate()
			}
		}()
		a.Post(func() {
			b.apply(a, snap)
			applog.Mark("sidebar-live", fmt.Sprintf("%d convs", len(snap.Convs)))
			b.booted = time.Now()
			if a.router.Current() == "" {
				a.openDefault()
			} else if ch := a.router.Current().Channel(); ch != "" {
				b.opened(ch) // started at once; a no-op unless that failed early
			}
			if b.slick != nil { // the team id is certain now
				a.slickRoute(a.router.Current())
			}
			b.maybePresencePrompt(a)
			b.a11yNewUserDefaults(a) // a11y_defaults.go
		})
	}()
}

// tautDeferMax is how long the real window's plugin runtimes wait at most
// for its first real frame (a window started hidden draws none).
const tautDeferMax = 2 * time.Second

// startDeferredTaut starts the plugins the launch left for later, once
// (UI goroutine): now when the runtimes are in, else as installTaut puts
// them in (startTautAsync).
func (b *backend) startDeferredTaut(a *App) {
	if !b.tautDeferJS || a.data != b || b.ctx.Err() != nil {
		return
	}
	b.tautDeferJS = false
	startRuntimes(b.taut, b.slick)
}

// startRuntimes starts Taut's and Slick's plugins (made with DeferStart;
// a no-op for runtimes already started).
func startRuntimes(t *taut.Taut, s *slick.Slick) {
	if t != nil {
		t.Start()
	}
	if s != nil {
		s.Start()
	}
}

// maybePresencePrompt is maybeOpenPresencePromptDialog: once connected,
// with confirm_user_marked_away on (Advanced > "Ask if I want to toggle my
// away status when I log in after having set myself away") and the viewer
// manually away, Slack asks "Set yourself to active?".
func (b *backend) maybePresencePrompt(a *App) {
	if !b.core.PrefBool("confirm_user_marked_away", false) { // !0!==pref: only when Slack sent true
		return
	}
	parent := b.ctx
	if parent == nil {
		parent = context.Background()
	}
	go func() {
		ctx, cancel := context.WithTimeout(parent, 15*time.Second)
		defer cancel()
		away, err := b.core.ManuallyAway(ctx)
		if err != nil || !away {
			return
		}
		a.Post(func() {
			popups.ConfirmAway(a, func() {
				if !b.m.SelfActive {
					b.togglePresence()
				} else {
					go func() { _ = b.core.SetPresence(parent, "auto") }()
				}
			})
		})
	}()
}

// loadDrafts refetches the unsent drafts for the sidebar's draft markers.
// UI goroutine; the call runs in the background.
func (b *backend) loadDrafts(a *App) {
	b.draftSeq++
	seq := b.draftSeq
	go func() {
		ctx, cancel := context.WithTimeout(b.ctx, 15*time.Second)
		defer cancel()
		ds, err := b.core.Drafts(ctx)
		if err != nil {
			return
		}
		// the drafts a sidebar row counts (Ex/u cF): its own unsent
		// drafts; with draft_management_in_composer also the composer's
		// and a DM's drafts addressed by user, and with
		// sidebar_draft_indicator_menu_and_thread_inclusion its threads'
		inComposer := b.core.Experiment("draft_management_in_composer") == "treatment"
		threads := inComposer && b.core.Experiment("sidebar_draft_indicator_menu_and_thread_inclusion") == "on"
		n, sched, chans := 0, 0, map[string]bool{}
		byChan := map[string][]core.Draft{}
		var dmUsers []string
		var dmDrafts []core.Draft
		for _, d := range ds {
			if d.DateScheduled != 0 {
				sched++  // Slack's getScheduledDraftsCount: date_scheduled set
				continue // scheduled messages live in the Scheduled tab
			}
			n++
			switch {
			case d.Channel == "":
				if inComposer && len(d.Users) == 1 {
					dmUsers = append(dmUsers, d.Users[0])
					dmDrafts = append(dmDrafts, d)
				}
			case d.ThreadTS != "" && !threads, d.FromComposer && !inComposer:
			default:
				chans[d.Channel] = true
				byChan[d.Channel] = append(byChan[d.Channel], d)
			}
		}
		a.Post(func() {
			if seq != b.draftSeq {
				return
			}
			for i, u := range dmUsers {
				for id, c := range b.m.Convs {
					if (c.Kind == "im" || c.Kind == "app") && c.User == u {
						chans[id] = true
						byChan[id] = append(byChan[id], dmDrafts[i])
					}
				}
			}
			b.draftN, b.schedN, b.draftChans, b.chanDrafts = n, sched, chans, byChan
			a.Invalidate()
		})
	}()
}

// stop drops this backend (quit, sign out, account switch, session
// ended). UI goroutine: it detaches the UI and stops core's traffic at
// once, and closes the rest in the background, because closing Taut runs
// every plugin's stop hook (seconds with a slow plugin) and core.Close
// writes the metadata cache. The channel closes when all of it is done.
func (b *backend) stop() <-chan struct{} {
	s, t := b.slick, b.taut
	b.detachTaut()
	b.stopBlockkit()
	uninstallMemberRead(b.core)
	if b.cancel != nil {
		b.cancel()
	}
	if b.unsub != nil {
		b.unsub()
	}
	// Every caller drops this backend: stop core's socket and loops too,
	// or the old session keeps a second realtime connection open next to
	// the new one.
	b.core.StopTraffic() // Close, below and off this goroutine, waits for disk saves
	done := make(chan struct{})
	go func() {
		defer close(done)
		// the next warm start's sidebar and headers first: the window's
		// close waits only so long, and plugins' stop hooks can be slow
		b.core.SaveWarm()
		// a section collapse still in its 3 s debounce goes out now
		// (flushPendingChannelSectionCollapsedSync on teardown)
		b.core.FlushSectionCollapsed()
		closeTaut(s, t)
		_ = b.core.Close() // and its Release: core's maps keyed by the App
		// what the UI's packages keep of the session beside it: its
		// message hub, its app DMs, its plugin credit names (each kept the
		// session in memory for good after a sign-out or a switch;
		// ci-speed-3)
		messages.ForgetCore(b.core)
		blockkit.ForgetCore(b.core)
		composer.ForgetCore(b.core)
		forgetCreditNames(b.core.S)
	}()
	return done
}

// resnapshot rebuilds the model off the UI goroutine (sidebar_changed,
// resync), coalescing bursts.
func (b *backend) resnapshot(a *App) {
	b.core.InvalidateSnapshot() // RecentSnapshot's readers (the huddle channels, a draft) see it too
	b.snapMu.Lock()
	if b.snapPending {
		b.snapMu.Unlock()
		return
	}
	b.snapPending = true
	b.snapMu.Unlock()
	go func() {
		time.Sleep(150 * time.Millisecond)
		b.snapMu.Lock()
		b.snapPending = false
		b.snapMu.Unlock()
		// before the sidebar's data is in there is nothing to show (a cold
		// start's Boot applies its own snapshot when it ends); from then on
		// (a warm start's boot data) what the store holds now, so a change
		// made before Boot finished shows at once
		<-b.core.DataReady()
		snap := b.core.SnapshotNow()
		a.Post(func() { b.apply(a, snap) })
	}()
}

// apply replaces the model from a snapshot, unless it is older than the
// one applied last (core.Snapshot.Seq: snapshots built on several
// goroutines, the boot's, a resnapshot's, a warm start's, can land out of
// order).
func (b *backend) apply(a *App, s core.Snapshot) {
	if s.Seq != 0 && s.Seq < b.snapSeq {
		return
	}
	b.snapSeq = max(b.snapSeq, s.Seq)
	m := &b.m
	a.applyAccountPrefs(s.Prefs)
	uipref.Set(b.core.Prefs())
	b.selfID = s.Self.ID
	b.domain = s.Team.Domain
	b.teamID = s.Team.ID
	m.TeamName = s.Team.Name
	m.TeamIcon = s.Team.Icon
	m.Rail = s.Rail
	m.SelfID = s.Self.ID
	m.SelfName = s.Self.Name
	m.SelfAvatar = s.Self.Avatar
	m.SelfActive = s.Self.Presence != "away"
	m.Connected = s.Connected
	convs := make(map[string]*convInfo, len(s.Convs))
	open := a.router.Current().Channel()
	for i := range s.Convs {
		c := &s.Convs[i]
		if c.IsArchived && c.ID != open { // the open one keeps its row (core SetSelected)
			continue
		}
		ci := &convInfo{ID: c.ID, Name: c.Name, Kind: c.Kind, User: c.User, Topic: c.Topic,
			NoDMsBadge: c.NoDMsBadge, NoDMsCount: c.NoDMsCount, BotDM: c.BotDM,
			NumMembers: c.NumMembers, Members: c.Members, Muted: c.Muted, TopicSpans: firstSpans(c.TopicDoc),
			Archived: c.IsArchived, Preview: c.Preview, Purpose: c.Purpose,
			Code: c.Code, CodeStatus: c.CodeStatus, AgentName: c.AgentName,
			File: c.File, FileID: c.FileID, Doc: c.Doc, NameSpans: c.NameSpans,
			Shared: c.IsShared, Teams: c.ConnectedTeams}
		if old := m.Convs[c.ID]; old != nil {
			// keep what opened() fetched (count, facepile) across snapshots
			if ci.NumMembers == 0 {
				ci.NumMembers = old.NumMembers
			}
			if len(ci.Members) == 0 {
				ci.Members = old.Members
			}
		}
		// and what it read before this conversation had a row (the
		// restored channel's header reads finish before boot's first
		// snapshot)
		b.applySide(ci, true)
		if c.Kind == "im" {
			if u, ok := s.Users[c.User]; ok {
				ci.Avatar = u.Avatar
				ci.StatusEmoji = u.StatusEmojiURL
				ci.Active = u.Presence == "active"
				ci.Bot = u.IsBot
				if u.Name != "" {
					ci.Name = u.Name
				}
				if u.External {
					ci.PeerTeam = u.TeamID
				}
			}
			ci.Self = c.User == s.Self.ID
			if ci.Bot {
				ci.Kind = "app"
			}
		}
		if c.Kind == "mpim" {
			// raw (no hit, nothing asked for): every group DM is here,
			// closed ones too; core's snapshot read the members of those
			// the sidebar lists through getMemberById (sidebarUsers)
			ci.Name = b.mpimNameBy(c, s.Self.ID, b.core.RawUser)
		}
		if cnt, ok := s.Counts[c.ID]; ok {
			ci.Unread, ci.Mentions, ci.LastRead, ci.Latest = cnt.Unread, cnt.Mentions, cnt.LastRead, cnt.Latest
		}
		convs[c.ID] = ci
	}
	m.Convs = convs
	b.notePeople()
	m.FilterNames = s.FilterNames
	m.Sections = m.Sections[:0]
	for _, sec := range s.Sections {
		ss := sbSection{ID: sec.ID, Name: sec.Name, Type: sec.Type, Emoji: sec.Emoji, EmojiURL: sec.EmojiURL, Collapsed: sec.Collapsed,
			Filter: sec.Filter, Sort: sec.Sort, StarredBanner: sec.StarredBanner}
		known := func(ids []string) []string {
			var out []string
			for _, id := range ids {
				if convs[id] != nil {
					out = append(out, id)
				}
			}
			return out
		}
		ss.All, ss.HiddenRead, ss.HiddenMuted = known(sec.All), known(sec.HiddenRead), known(sec.HiddenMuted)
		ss.HiddenDormant, ss.HiddenUnread = known(sec.HiddenDormant), known(sec.HiddenUnread)
		for _, id := range sec.CollapsedChannels {
			if convs[id] != nil {
				ss.CollapsedRows = append(ss.CollapsedRows, id)
			}
		}
		for _, id := range sec.Channels {
			if c := convs[id]; c != nil {
				ss.Rows = append(ss.Rows, id)
				if sec.Type == "stars" {
					c.Starred = true
				}
			}
		}
		if ss.Name == "" {
			ss.Name, ss.DefaultName = defaultSectionName(sec.Type), true
		} else if ss.Name == defaultSectionName(sec.Type) {
			ss.DefaultName = true // Slack's standard sections can't be renamed
		}
		m.Sections = append(m.Sections, ss)
	}
	m.threadsUnread = s.Threads.Unread
	b.recount()
	m.version++
	a.Invalidate()
}

// firstSpans returns the inline spans of a parsed text's first block.
func firstSpans(bs []richtext.Block) []richtext.Span {
	for _, b := range bs {
		if len(b.Spans) > 0 {
			return b.Spans
		}
	}
	return nil
}

func defaultSectionName(t string) string {
	switch t {
	case "stars":
		return "Starred"
	case "channels":
		return "Channels"
	case "direct_messages":
		return "Direct messages"
	case "apps", "recent_apps":
		return "Apps"
	case "slack_connect":
		return "External connections" // p4Ef's static name
	}
	return "Section"
}

// mpimNameBy is a group DM's name for the sidebar: its custom name, else
// its members' names but self's, read with user, else its mpdm slug.
func (b *backend) mpimNameBy(c *core.Conv, self string, user func(string) (core.User, bool)) string {
	if n, ok := b.core.MpdmCustomName(c.ID); ok { // mpdm_naming (core/mpdm_name.go)
		return n
	}
	var names []string
	for _, id := range c.Members {
		if id == self {
			continue
		}
		if u, ok := user(id); ok && u.Name != "" {
			names = append(names, u.Name)
		}
	}
	if len(names) == 0 {
		return strings.TrimPrefix(c.Name, "mpdm-")
	}
	return strings.Join(names, ", ")
}

// activityBadgeMoved reports whether a badge core keeps moved without any
// conversation's count changing (recount then picks it up): the Activity
// badge and its dot (activity_v2 under the inbox, channel_badges and the
// alerts without it), Home's activity_v2 dm count and the channel_badges'
// part of the app badge.
func (b *backend) activityBadgeMoved() bool {
	if b.core == nil {
		return false
	}
	m := &b.m
	c, _ := b.core.ActivityTabCounts()
	return b.core.ActivityBadgeCount() != m.ActivityCount || b.core.ActivityDot() != m.ActivityDot ||
		b.core.ChannelBadgeHighlights() != m.ChannelHighlights || c[1] != m.ActivityDMs
}

// recount derives the rail badges from conv counts and core's badges.
//
// The DMs tab's are Slack's (core rail.go DMsBadge): muted conversations
// count nowhere (siMV), an unread DM is its mention count or 1 (ZdmU), and
// the IMs the DMs page leaves out stay out of the tab's highlights (rHEx
// dmsBadgeCount) and, a bot's, out of its unread conversations (6Z4k Sm).
// Clack summed every unread DM into the tab, so the Slack system user's
// DM (USLACK, "Slack") and Slackbot's drew a count the DMs page had no
// row for (Karim, 2026-11-16). They still make something unread
// (DMUnread). The app badge's own highlights (HomeCount) leave the bot
// DMs out too (ZVw+ getTotalUnreadHighlights skips DUy0 G's; an app's DM
// comes back only through the server's channel_badges app_dms, which core
// adds without the inbox: AppBadgeCount).
//
// The Activity tab's count is core's (ActivityBadgeCount): activity_v2
// under the inbox, channel_badges and the alerts without it (3x6A
// getActivityBadgeCount), not the channels' mentions.
func (b *backend) recount() {
	m := &b.m
	m.DMUnread, m.DMHighlights, m.DMUnreadConvs, m.HomeCount, m.HomeUnread = false, 0, 0, 0, false
	m.ActivityCount, m.ActivityDot, m.ActivityDMs, m.ChannelHighlights = 0, false, 0, 0
	for _, c := range m.Convs {
		if c.Muted {
			continue
		}
		switch c.Kind {
		case "im", "mpim", "app":
			if c.Unread || c.Mentions > 0 {
				n := max(c.Mentions, 1)
				m.DMUnread = true
				if !c.BotDM {
					m.HomeCount += n
				}
				if !c.NoDMsBadge {
					m.DMHighlights += n
				}
				if !c.NoDMsCount {
					m.DMUnreadConvs++
				}
			}
		default:
			if c.Unread {
				m.HomeUnread = true
			}
		}
	}
	if b.core != nil {
		m.ActivityCount = b.core.ActivityBadgeCount()
		m.ActivityDot = b.core.ActivityDot()
		m.ChannelHighlights = b.core.ChannelBadgeHighlights()
		c, _ := b.core.ActivityTabCounts()
		m.ActivityDMs = c[1]
	}
	if b.app != nil {
		b.app.updateDock()
	}
}

// wantsNotify is a channel's level, getCalculatedDesktopNotifySetting
// (users.prefs all_notifications_prefs: the channel's desktop override,
// else global_desktop): everything, or mentions_dms (mentions and
// keywords: mention). "nothing" reads as mentions_dms, the override's
// and the global's alike (getCalculatedNotifySetting,
// getGlobalChannelNotifySetting; notify_dm.go): the desktop push switch,
// not the level, turns notifications off. A DM has no level
// (shouldShowNotificationForDirectMessage).
func (b *backend) wantsNotify(ch string, mention bool) bool {
	p := b.core.Prefs()
	level := p.GlobalDesktop
	if o := p.ChannelNotify[ch].Desktop; o != "" && o != "default" {
		level = o
	}
	return level == "everything" || mention
}

// handle applies one realtime event to the model.
func (b *backend) handle(a *App, ev core.Event) {
	if ev.Type == "huddle" {
		a.huddles().Handle(ev)
	}
	m := &b.m
	changed := false
	switch ev.Type {
	case "experiments": // a group changed (experiments.getByUser, a plugin's override): the sidebar rules and names read them
		b.resnapshot(a)
		b.loadDrafts(a) // which drafts a row counts follows the draft experiments
	case "prefs": // users.prefs changed (Preferences, pref_change)
		if ev.Prefs != nil {
			a.applyAccountPrefs(*ev.Prefs)
			uipref.Set(*ev.Prefs)
			if a.hud != nil {
				go a.hud.PrefsChanged() // the global mute shortcut, the lounge music
			}
			a.updateDock() // mac_ssb_bullet
		}
		// core's sidebar rules read sidebar_behavior, channel_sort,
		// boost_mentions…: rebuild the sections from a fresh snapshot
		b.resnapshot(a)
	case "draft":
		b.loadDrafts(a)
	case "typing":
		if os.Getenv("CLACK_TYPINGLOG") != "" { // ids only
			log.Printf("ui: typing ch=%s user=%s self=%v", ev.Channel, ev.UserID, ev.UserID == b.selfID)
		}
	case "message":
		if ev.Count != nil {
			changed = b.setCount(ev.Channel, *ev.Count)
		}
		b.maybeNotify(a, ev)
	case "update":
		// messageChanged: under defer_notification_3p_streaming the message
		// whose chat stream just ended gets the notification it was held
		// back from (maybeShowNotification for the settled message)
		if ev.StreamDone && b.core.Experiment("defer_notification_3p_streaming") == "on" {
			b.maybeNotify(a, ev)
		}
	case "desktop_notification": // a followed thread's reply (notify_threads.go)
		b.desktopNotification(a, ev)
	case "dnd_override": // a sender's notify anyway (notify_dm.go)
		b.noteDNDOverride(a, ev)
	case "marked", "counts":
		if ev.Count != nil && ev.Channel != "" {
			changed = b.setCount(ev.Channel, *ev.Count)
		} else {
			cs := b.core.Counts()
			for id, cnt := range cs {
				if b.setCount(id, cnt) {
					changed = true
				}
			}
		}
		// client.counts' activity_v2 can move with no conversation's
		// count changing (a thread reply): the rail's badge reads it
		changed = changed || b.activityBadgeMoved()
	case "activity_badges": // badge_counts_updated or a mark read (core activity_badges.go)
		changed = b.activityBadgeMoved()
	case "threads_state": // update_thread_state taken at once: the reply bars' Unread mark (core thread_unreads.go)
		m.threadsUnread = b.core.ThreadsState().Unread
		changed = true
	case "presence":
		for _, c := range m.Convs {
			if c.Kind == "im" && (c.User == ev.UserID || contains(ev.Users, c.User)) {
				c.Active = ev.Presence == "active"
				changed = true
			}
		}
		if ev.UserID == b.selfID || contains(ev.Users, b.selfID) {
			m.SelfActive = ev.Presence == "active"
			changed = true
		}
	case "user":
		if ev.User != nil {
			if ev.User.ID == b.selfID {
				m.SelfName, m.SelfAvatar = ev.User.Name, ev.User.Avatar
				changed = true
			}
			for _, c := range m.Convs {
				switch {
				case (c.Kind == "im" || c.Kind == "app") && c.User == ev.User.ID:
					c.Avatar, c.Name, c.StatusEmoji = ev.User.Avatar, ev.User.Name, ev.User.StatusEmojiURL
					changed = true
				case c.Kind == "mpim" && contains(c.Members, ev.User.ID):
					changed = true // its name lists its members
				}
			}
		}
	case "conv", "conv_removed", "sidebar_changed", "resync", "team": // "prefs" is handled above (it resnapshots too)
		if ev.Type == "conv_removed" && ev.Channel != "" {
			b.forgetExt(ev.Channel) // left: a preview again, not the member it resolved as
		}
		if ev.Type == "conv" && ev.Channel != "" {
			b.refreshExt(ev.Channel) // a non-member channel looked up again (renamed, unarchived): its header and footer
		}
		b.resnapshot(a)
	case "member_count_stale":
		// someone joined or left: the open channel's header draws the
		// count, and its draw refetches a stale one (getMembershipCountsByChannelId's
		// maybeRefetch, core/membership_counts.go); the answer comes back
		// as "member_count"
		if b.core != nil && ev.Channel != "" && a.router.Current().Channel() == ev.Channel {
			b.core.RefreshMemberCount(ev.Channel)
		}
	case "member_count":
		// users/counts answered after the page showed (core History's side
		// read): the header draws the count from the row, so set it there
		// (no snapshot, and no sidebar rebuild, for one number)
		if ev.Conv != nil && ev.Conv.NumMembers > 0 {
			for _, c := range []*convInfo{m.Convs[ev.Channel], b.ext[ev.Channel]} {
				if c != nil && c.NumMembers != ev.Conv.NumMembers {
					c.NumMembers = ev.Conv.NumMembers
					a.Invalidate()
				}
			}
		}
	case "auth_failed":
		// core stopped all traffic on a dead session: back to sign-in
		a.sessionEnded()
	case "members_evicted":
		// the members cache let members go (core/members.go): the sidebar
		// and the views read theirs again, as Slack's components re-render
		// on the removal; one still shown is asked for ("names" follows)
		b.resnapshot(a)
		a.Invalidate()
	case "names":
		// core resolved channel/user/bot names in the background: re-read
		// the visible conversations so mentions and headers re-render.
		b.resnapshot(a)
		a.refreshVisible()
		a.Invalidate()
		a.canvasJumpNames(ev.Users) // a jump that waited for its canvas's files.info (canvas_open.go)
		for id := range b.ext {
			// re-resolve closed-DM headers, keeping what they show until
			// the new answer lands: forgetting them blanked the header and
			// the join pane of an open non-member channel for the lookup's
			// round trip (TestChannelLinkTargets' "header conv <nil>")
			b.refreshExt(id)
		}
	case "tabs":
		delete(b.fetched, ev.Channel)
		if a.router.Current().Channel() == ev.Channel {
			b.opened(ev.Channel)
		}
	case "connection":
		m.Connected = ev.Connected
		changed = true
	case "client_online": // isClientOnline changed: the switcher's offline notice reads it (core/online.go)
		a.Invalidate()
	case "permissions": // permissions/info answered (core.ChannelGrant): the topic's Edit reads it
		a.Invalidate()
	case "megaphone": // a megaphone space changed: the join banner's gate reads them (joinbanner.go)
		a.Invalidate()
	case "work_objects": // the work object store moved (cards' refresh and comments, the flexpane)
		a.Invalidate()
	case "pins_tab", "bookmarks": // the Pins tab's rows, the folders' bookmarks (core/channel_pins.go, bookmark_folders.go)
		a.Invalidate()
	case "list_prefs": // a conversation's list prefs: the composer's + reads can_upload_files (core/upload_gates.go)
		a.Invalidate()
	}
	if changed {
		b.recount()
		m.version++
		a.Invalidate()
	}
}

// wants reports whether handle does anything with ev (the event
// goroutine). Each Post draws a frame, and a busy workspace's socket
// brings typing, reactions, new emoji, presence and profile changes about
// conversations and people the window doesn't show, many a second (Karim
// 2026-11-14: Hack Club's, while Clack sat idle): handle only logs typing,
// leaves updates other than a settled stream to the message lists, never
// reads new emoji, and changes the model for presence and profiles of the
// viewer and DM people only. The lists' hub, the composer and the views
// follow the events they draw on their own.
func (b *backend) wants(ev core.Event) bool {
	switch ev.Type {
	case "typing":
		return os.Getenv("CLACK_TYPINGLOG") != ""
	case "update":
		return ev.StreamDone
	case "emoji_changed", "hydrated": // hydrated: start's WarmSnapshot goroutine applies the boot data
		return false
	case "presence":
		return b.shows(ev.UserID) || b.shows(ev.Users...)
	case "user":
		id := ev.UserID
		if ev.User != nil {
			id = ev.User.ID
		}
		return b.shows(id)
	}
	return true
}

// shows reports whether any of ids is in people (any goroutine); before
// the first model, everyone is.
func (b *backend) shows(ids ...string) bool {
	p := b.people.Load()
	if p == nil {
		return true
	}
	for _, id := range ids {
		if id != "" && (*p)[id] {
			return true
		}
	}
	return false
}

// notePeople rebuilds people from the model (UI goroutine).
func (b *backend) notePeople() {
	p := map[string]bool{}
	if b.selfID != "" {
		p[b.selfID] = true
	}
	for _, c := range b.m.Convs {
		switch c.Kind {
		case "im", "app":
			if c.User != "" {
				p[c.User] = true
			}
		case "mpim":
			for _, u := range c.Members {
				p[u] = true
			}
		}
	}
	b.people.Store(&p)
}

// maybeNotify is maybeShowNotification for a message (or a settled
// stream's update): ephemerals (Slackbot's "you can't @channel here")
// never notify, and under defer_notification_3p_streaming neither does a
// message still streaming ("Skip notification for in-progress streaming
// message"). A conversation whose desktop push is off never does, a DM's
// message follows shouldShowNotificationForDirectMessage and any other's
// shouldShowNotificationForChannelMessage (notify_dm.go). The
// conversation comes from core, not the sidebar's rows, and what the
// notification names is looked up first when the store can't name it
// yet (notify_conv.go).
func (b *backend) maybeNotify(a *App, ev core.Event) { b.maybeNotifyAfter(a, ev, false) }

// maybeNotifyAfter is maybeNotify; resolved: the lookups it needed ran
// (noteResolve), so it shows what it has.
func (b *backend) maybeNotifyAfter(a *App, ev core.Event, resolved bool) {
	if ev.Msg == nil || ev.Msg.System {
		return // no message of Slack's to decide on
	}
	// every decision below writes its line (notify_decisions.go); a
	// notification that waits for lookups writes it once they answer
	skip := func(why noteSkip) { noteDecided(ev.Channel, ev.TS, why) }
	if b.core.Interactive().Ephemeral(ev.Channel, ev.TS) {
		skip(noteSkipEphemeral)
		return
	}
	key := noteShownKey(ev.Channel, ev.TS)
	if !resolved && b.noteWasShown(key) {
		skip(noteSkipShown) // shown already (notify_dm.go)
		return
	}
	if !b.notePushOn(ev.Channel) {
		skip(noteSkipPushOff) // "Skip pref-disabled push notification"
		return
	}
	if ev.Msg.Streaming() && b.core.Experiment("defer_notification_3p_streaming") == "on" {
		skip(noteSkipStreaming)
		return
	}
	if b.core.UserHidden(ev.Msg.User) {
		skip(noteSkipHidden) // a member the viewer hid (isUserHidden): no notification, no lookups for one
		return
	}
	if c, ok := b.core.MessageChange(ev.Channel, ev.TS); ok && c.Deleted {
		skip(noteSkipDeleted) // deleted while the lookups it waited for ran: nothing to show, or open
		return
	}
	c, named := b.noteConv(ev.Channel)
	if c != nil && c.Preview {
		skip(noteSkipNotMember) // a channel the viewer isn't in (isMemberOfChannel: reason 2)
		return
	}
	ignoreDND := b.noteDNDIgnored(ev.Channel, ev.TS)
	var why noteSkip
	if noteIsDM(ev, c) {
		why = b.noteDMReason(a, ev, c, ignoreDND)
	} else {
		why = b.noteChannelReason(a, ev, c, ignoreDND)
	}
	if why != noteShow {
		skip(why)
		return
	}
	// Slack records it as the showNotification thunk goes out, before the
	// models it waits for (ensureModelsArePresent: noteResolve here) land
	b.noteMarkShown(key)
	if !resolved {
		// a conversation core doesn't know was looked up for this very
		// message (core onMessage's refreshConv); one it knows without a
		// name, and the channels the text mentions that nobody named
		// yet, are looked up now. Not a group DM: its name is its members'
		// (the people below), and Slack looks nothing else up for it
		var ids []string
		if c != nil && !named && c.Kind != "im" && c.Kind != "app" && c.Kind != "mpim" {
			ids = append(ids, ev.Channel)
		}
		ids = append(ids, b.noteMentions(ev.Msg.Doc)...)
		// and the people it names core doesn't hold (core announced the
		// message without them; notify_conv.go noteMembers)
		people := b.noteMembers(ev.Msg)
		// and the messages its slugs link to (noteSlugs)
		msgs := b.noteSlugs(ev.Msg.Doc)
		if len(ids) > 0 || len(people) > 0 || len(msgs) > 0 {
			b.noteResolve(a, ev.Channel, ids, people, msgs, func() {
				if len(people) > 0 {
					ev = b.noteFresh(ev)
				}
				b.maybeNotifyAfter(a, ev, true)
			})
			return
		}
		// nothing to look up: now, unless an earlier notification of the
		// conversation still waits for its lookups or is queued (noteOrder)
		if b.notes.later(a, ev.Channel, func() { b.maybeNotifyAfter(a, ev, true) }) {
			return
		}
	}
	// through Taut's showNotification thunk: plugins may change or
	// drop it (native: the desktop notification)
	title, body := b.notifyTitle(ev, c), b.notifyBody(ev, c)
	msg := notificationMessage(a.Taut(), ev)
	note := a.messageNote(ev.Channel, ev.TS, ev.Msg.ThreadTS) // the toast's payload (notify.go)
	// the conversation's desktop push and the message preview pref were
	// applied here (notePushOn, notifyBody): notifyShow leaves them be
	note.pushChecked, note.textChecked = true, true
	note.soundThroughDND, note.prioritySound = b.noteVIPOverride(ev, c), b.notePrioritySound(ev, c)
	// with message previews off Slack writes the body from the message the
	// plugins pass and then hides it (shouldHideNotificationText): a
	// plugin's rewrite of the text never shows either
	hidden := b.notifyHiddenBody(ev, c)
	// in order with the other notifications (showNotification: taut.DispatchSeq)
	noteDecided(ev.Channel, ev.TS, noteShow)
	a.showNotification(map[string]any{"title": title, "message": msg, "body": body, "channelId": ev.Channel, "ts": ev.TS},
		func(ctx context.Context, args map[string]any) (any, error) {
			if t, text, ok := notificationText(args, msg, body); ok {
				if b.core.Prefs().NoTextInNotifications {
					text = hidden
				}
				a.notifyMessage(t, text, note)
			}
			return nil, nil
		})
}

// showNotification dispatches Slack's showNotification thunk for one
// notification (UI goroutine): plugins may change or drop it, and native
// shows it. Each goes out as it comes, without waiting for the ones
// before, as Slack's messageReceived dispatches it for every message: its
// JS call is queued now, in order with the socket's events and the store
// updates before it (a dispatch's in-order lane, ahead of the queued
// renders, as Slack shows the toast in the socket event's task; not the
// user's lane, which is for input), and the natives run in the order the
// plugins' patches call them, or the order of the calls with none
// (taut.DispatchSeq). A goroutine each (App.dispatch) could show two in
// reverse. However late its JS runs, its native still shows it.
func (a *App) showNotification(args map[string]any, native func(context.Context, map[string]any) (any, error)) {
	taut.DispatchSeq(context.Background(), a.Taut(), "showNotification", args, native, func(_ any, err error) {
		if err != nil {
			log.Printf("ui: showNotification: %v", err)
		}
	})
}

func (b *backend) setCount(id string, cnt core.Count) bool {
	c := b.m.conv(id)
	if c == nil {
		return false
	}
	if c.Unread == cnt.Unread && c.Mentions == cnt.Mentions && c.LastRead == cnt.LastRead && c.Latest == cnt.Latest {
		return false
	}
	c.Unread, c.Mentions, c.LastRead, c.Latest = cnt.Unread, cnt.Mentions, cnt.LastRead, cnt.Latest
	return true
}

// notifyTitle is Slack's getNotificationTitle (Eley) for one signed-in
// account: "New message from {name}" in a DM, named as
// getDisplayNameForChannel names it (the DM's user, never the message's
// username: noteConv; an app posting into the Slack system user's DM
// included), or "{name} is trying to reach you" when the message ignores
// Do Not Disturb (dnd_override); else "New message in {channel}" (growl;
// notify_threads.go). A conversation core doesn't know keeps the
// sender's form: its name isn't there to show.
func (b *backend) notifyTitle(ev core.Event, c *convInfo) string {
	switch {
	case c == nil:
		return nsGrowl.T("New message from {name}", "name", b.notifyFrom(ev))
	case c.Kind == "im" || c.Kind == "app":
		name := c.Name
		if !noteNamed(c) {
			name = b.notifyFrom(ev)
		}
		if b.noteDNDIgnored(ev.Channel, ev.TS) {
			return nsGrowl.T("{name} is trying to reach you", "name", name)
		}
		return nsGrowl.T("New message from {name}", "name", name)
	}
	return nsGrowl.T("New message in {channel}", "channel", notifyChannelName(c))
}

// notifyBody is getContentForNotification: the message's text
// (notifyText: extractTextFromMessage, richtext.NotificationMessageText, named by
// noteNames), after "{sender}: " outside DMs, with its mentions written as
// Slack's userToText and channelToText write them (notify_conv.go: never
// the raw id). With no_text_in_notifications (shouldHideNotificationText)
// a DM's body is empty and any other's is "a message from {sender}".
func (b *backend) notifyBody(ev core.Event, c *convInfo) string {
	if b.core == nil || b.core.S == nil {
		return "" // no session to read the message with
	}
	if b.core.Prefs().NoTextInNotifications {
		return b.notifyHiddenBody(ev, c)
	}
	text := notifyText(noteNames{b.core.S, b}, ev.Msg, b.noteSpanText)
	if c == nil || c.Kind == "im" || c.Kind == "app" {
		return text
	}
	if name := b.notifySender(ev); name != "" {
		return name + ": " + text
	}
	return text
}

// notifyHiddenBody is getContentForNotification under "Include a preview
// of the message" off (shouldHideNotificationText): nothing in a DM, "a
// message from {sender}" elsewhere (nothing for a message with no user).
func (b *backend) notifyHiddenBody(ev core.Event, c *convInfo) string {
	if c == nil || c.Kind == "im" || c.Kind == "app" {
		return ""
	}
	if name := b.notifySender(ev); name != "" {
		return nsGrowl.T("a message from {name}", "name", name)
	}
	return ""
}

// notifySender is a body's "{sender}: " (getContentForNotification's
// getPrefCompliantMemberNameById, R6Qy, over the message's user): the
// member's name under the viewer's display_real_names, as the store holds
// them (the toast waited for them: nothing asked again). "" for a message
// with no user, a webhook's or a legacy bot's post: getMemberById has no
// member for it, so Slack writes no prefix (Clack wrote the username).
func (b *backend) notifySender(ev core.Event) string {
	if ev.Msg.User == "" || b.core == nil {
		return ""
	}
	n, _ := b.core.S.PrefNameHeld(ev.Msg.User)
	return n
}

// notifyFrom names the sender for a title whose conversation can't name
// it: notifySender, else a user-less bot's own name (its username or the
// bot's: core's Message.Name).
func (b *backend) notifyFrom(ev core.Event) string {
	if n := b.notifySender(ev); n != "" {
		return n
	}
	return ev.Msg.Name
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// ---- per-channel side data (tabs, facepile) ----

// opened is called when a conversation becomes current: its header's
// reads (the tab bar with the folder tabs' bookmarks, the member facepile
// and count) go out in the background. A header this session hasn't read
// shows the header cache's copy at once (core header_cache.go, kept on
// disk across starts) while the reads revalidate it, and reads that bring
// the same header change nothing on screen.
func (b *backend) opened(ch string) {
	if t, ok := b.fetched[ch]; ok && time.Since(t) < 2*time.Minute {
		return
	}
	b.fetched[ch] = time.Now()
	b.headerFromCache(ch)
	a := b.app
	if b.headerGen == nil {
		b.headerGen = map[string]int{}
	}
	b.headerGen[ch]++
	gen := b.headerGen[ch]
	b.headerStarted++
	go func() {
		ctx, cancel := context.WithTimeout(b.ctx, 20*time.Second)
		defer cancel()
		// The header's three reads at once (they used to go one after
		// another: three round trips before the header was complete).
		// Members needs the kind: the sidebar's, else the id's.
		kind := ""
		if c, ok := b.core.Conv(ch); ok {
			kind = c.Kind
		} else if strings.HasPrefix(ch, "C") {
			kind = "channel"
		} else if strings.HasPrefix(ch, "G") {
			kind = "private"
		}
		var (
			wg    sync.WaitGroup
			ts    []core.Tab
			err   error
			info  core.ChannelDetails
			ierr  error
			us    []core.User
			merr  error
			tried bool
		)
		wg.Add(2)
		go func() { defer wg.Done(); ts, err = b.core.Tabs(ctx, ch) }()
		go func() { defer wg.Done(); info, ierr = b.core.ChannelInfo(ctx, ch) }()
		if kind == "channel" || kind == "private" {
			tried = true
			wg.Add(1)
			go func() { defer wg.Done(); us, _, merr = b.core.Members(ctx, ch, "", true) }()
		}
		wg.Wait()
		if err != nil {
			log.Printf("ui: tabs %s: %v", logtag.ID(ch), err)
		}
		// Slack's tab bar (core.Tabs: canvases, lists, bookmarks, files…).
		// A tab whose file or bookmarks read failed keeps what the last
		// good read had (the header cache: this session's or the disk's).
		if prev, ok := b.core.CachedHeader(ch); ok && err == nil {
			ts = mergeTabReads(ts, prev.Tabs)
		}
		complete := err == nil && tabsComplete(ts)
		notMember := ierr == nil && !info.IsMember
		tabs, bms := b.tabsOf(ts, notMember)
		var members []string
		if tried {
			log.Printf("ui: members %s: %d err=%v", logtag.ID(ch), len(us), merr)
			if merr == nil {
				for _, u := range us {
					if len(members) == 3 {
						break
					}
					if u.Deleted || u.IsBot {
						continue
					}
					members = append(members, u.ID) // avatar() draws them from the members cache
				}
			}
		}
		if a == nil {
			return
		}
		a.Post(func() {
			b.headerAnswered++
			if b.headerGen[ch] != gen {
				return // a newer read of this header is out: its answer wins
			}
			if b.tabsDone == nil {
				b.tabsDone = map[string]bool{}
			}
			if b.headerReads == nil {
				b.headerReads = map[string]headerRead{}
			}
			hr := b.headerReads[ch]
			if err == nil {
				hr.tabs = ts
			}
			if ierr == nil {
				hr.notMember, hr.memberKnown = notMember, true
			}
			hr.complete = hr.complete || complete && ierr == nil
			b.headerReads[ch] = hr
			// a failed tab read keeps the bar shown (this session's or the
			// cache's, the cache's also when it loads later); one that
			// brings the same bar changes nothing
			changed := !b.tabsDone[ch]
			switch {
			case err == nil:
				b.noteTabRead(ch, ts, notMember)
				if !reflect.DeepEqual(b.tabCache[ch], tabs) {
					a.keepTabSelection(ch, func() { b.tabCache[ch] = tabs })
					changed = true
				}
				if !reflect.DeepEqual(b.bookmarks[ch], bms) {
					b.bookmarks[ch] = bms
					changed = true
				}
			case !b.tabsDone[ch] && !b.headerFromCache(ch):
				a.keepTabSelection(ch, func() { b.tabCache[ch] = nil })
				b.bookmarks[ch] = nil
			}
			b.tabsDone[ch] = true
			// the facepile, count and topic reach the screen through the
			// conversation's row (applySide, below): a change there is
			// what redraws
			sd := b.sideOf(ch)
			if ierr == nil && info.Conv.NumMembers > 0 {
				sd.num = info.Conv.NumMembers
			}
			if ierr == nil && info.Conv.Topic != "" && sd.topic != info.Conv.Topic {
				sd.topic, sd.spans = info.Conv.Topic, firstSpans(info.Conv.TopicDoc)
			}
			if len(members) > 0 {
				sd.members = members
			}
			if tried && merr != nil {
				// the facepile stays the loading tiles until a read works:
				// try again while the channel is open (2 s doubling to 1 min)
				sd.fails++
				delete(b.fetched, ch)
				b.retryOpened(ch, sd.fails)
			} else {
				sd.fails = 0
			}
			if complete && ierr == nil { // the next start's header (and this session's next open); never a partial read
				b.core.SetCachedHeader(ch, core.ChannelHeader{Tabs: ts, NotMember: notMember, Members: sd.members, Num: sd.num})
			}
			// the read wins over a snapshot's count and topic (applySide),
			// as before; only a change draws
			for _, c := range []*convInfo{b.m.conv(ch), b.ext[ch]} {
				if c == nil {
					continue
				}
				n, ms, tp := c.NumMembers, c.Members, c.Topic
				b.applySide(c, false)
				if c.NumMembers != n || !slices.Equal(c.Members, ms) || c.Topic != tp {
					changed = true
				}
			}
			if changed {
				a.Invalidate()
			}
		})
	}()
}

// tabsOf is the tab bar a channel's core.Tabs read makes, and the folder
// tabs' bookmarks. notMember: Slack's tab bar marks no canvas unread in a
// channel you aren't in (C08E083LZ9A's "Untitled", 2026-11-08). Any
// goroutine.
func (b *backend) tabsOf(ts []core.Tab, notMember bool) ([]chTab, []core.Bookmark) {
	var tabs []chTab
	var bms []core.Bookmark
	viewNames := b.core.Experiment("list_channel_tab_show_view_name_fe") == "on"
	for _, t := range ts {
		if t.Type == "messages" {
			continue
		}
		label := t.Title
		if label == "" {
			label = t.Label
		}
		icon := tabIcon(t.Type)
		if t.Deleted {
			icon = "trash"
		}
		if label == "" {
			continue
		}
		doc, emo, label, spans := docTab(t, label, tabSpans(t, label, b.core.Emoji), viewNames, b.core.Emoji) // tab_emoji_icon.go
		tabs = append(tabs, chTab{key: "tab:" + t.ID, label: label, icon: icon, typ: t.Type, fileID: t.FileID, unread: t.Unread && !notMember,
			folder: t.FolderBookmarkID, appID: t.AppID, doc: doc, emojiIcon: emo,
			spans: spans, defaultView: t.DefaultView, viewName: t.ViewName, labeled: t.Label != "", rawLabel: t.Label})
		bms = append(bms, t.Bookmarks...)
	}
	return tabs, bms
}

// mergeTabReads fills the parts of a tab read that failed (a file's title
// and state, a folder's bookmarks: core.Tab's TitleUnknown and
// BookmarksUnknown) from an earlier read of the same tab, where that read
// had them.
func mergeTabReads(ts, prev []core.Tab) []core.Tab {
	var out []core.Tab
	for i, t := range ts {
		if !t.TitleUnknown && !t.BookmarksUnknown {
			continue
		}
		for _, p := range prev {
			if p.ID != t.ID {
				continue
			}
			if out == nil {
				out = append([]core.Tab(nil), ts...)
			}
			m := &out[i]
			if t.TitleUnknown && !p.TitleUnknown && p.FileID == t.FileID {
				m.Title, m.TitleSpans, m.FileTitle, m.Deleted, m.Unread, m.ViewName = p.Title, p.TitleSpans, p.FileTitle, p.Deleted, p.Unread, p.ViewName
				m.TitleUnknown = false
			}
			if t.BookmarksUnknown && !p.BookmarksUnknown {
				m.Bookmarks, m.BookmarksUnknown = p.Bookmarks, false
			}
			break
		}
	}
	if out == nil {
		return ts
	}
	return out
}

// tabsComplete reports whether every part of a tab read answered.
func tabsComplete(ts []core.Tab) bool {
	for _, t := range ts {
		if t.TitleUnknown || t.BookmarksUnknown {
			return false
		}
	}
	return true
}

// headerFromCache shows ch's header as its last read left it (the header
// cache: an earlier session's read on a warm start, or this session's)
// until opened's reads revalidate it: the tab bar, the folder tabs'
// bookmarks, the facepile and the member count. Not over a read of this
// session that answered every part; over one that failed, or filling the
// parts a partial one couldn't read (a read that ran before the header
// cache loaded: an offline warm start). It reports whether it filled the
// header. UI goroutine.
func (b *backend) headerFromCache(ch string) bool {
	hr := b.headerReads[ch]
	if hr.complete || b.core == nil {
		return false
	}
	h, ok := b.core.CachedHeader(ch)
	if !ok {
		return false
	}
	ts, notMember := h.Tabs, h.NotMember
	if hr.tabs != nil { // this session's partial read: its known parts are newer
		ts = mergeTabReads(hr.tabs, h.Tabs)
	}
	if hr.memberKnown {
		notMember = hr.notMember
	}
	tabs, bms := b.tabsOf(ts, notMember)
	b.noteTabRead(ch, ts, notMember)
	if !reflect.DeepEqual(b.tabCache[ch], tabs) {
		if b.app != nil {
			b.app.keepTabSelection(ch, func() { b.tabCache[ch] = tabs })
		} else {
			b.tabCache[ch] = tabs
		}
	}
	b.bookmarks[ch] = bms
	if b.tabsDone == nil {
		b.tabsDone = map[string]bool{}
	}
	b.tabsDone[ch] = true
	sd := b.sideOf(ch)
	if sd.num == 0 {
		sd.num = h.Num
	}
	if len(sd.members) == 0 {
		sd.members = h.Members
	}
	b.applySide(b.m.conv(ch), false)
	b.applySide(b.ext[ch], false)
	if b.app != nil {
		b.app.Invalidate()
	}
	return true
}

// headerRead is what this session's header reads of a conversation
// brought (backend.headerReads).
type headerRead struct {
	complete    bool       // a read answered every part
	tabs        []core.Tab // the last tabs read that answered (nil: none did)
	notMember   bool       // conversations.info: the viewer isn't in it (memberKnown)
	memberKnown bool
}

// chSide is what opened() read for a conversation's header.
type chSide struct {
	members []string        // the facepile's first three people
	num     int             // conversations.info num_members (0: not read)
	topic   string          // conversations.info topic ("": not read)
	spans   []richtext.Span // its first line
	fails   int             // members reads that failed in a row
}

func (b *backend) sideOf(ch string) *chSide {
	if b.side == nil {
		b.side = map[string]*chSide{}
	}
	sd := b.side[ch]
	if sd == nil {
		sd = &chSide{}
		b.side[ch] = sd
	}
	return sd
}

// applySide puts what opened() read into a conversation's row (nil: no row
// yet; apply and resolved call it again when there is one). With
// onlyMissing, a snapshot's own count and topic stay (they follow
// member_joined and topic events) and only empty fields are filled.
func (b *backend) applySide(c *convInfo, onlyMissing bool) {
	if c == nil {
		return
	}
	sd := b.side[c.ID]
	if sd == nil {
		return
	}
	if sd.num > 0 && (!onlyMissing || c.NumMembers == 0) {
		c.NumMembers = sd.num
	}
	if len(sd.members) > 0 && (!onlyMissing || len(c.Members) == 0) {
		c.Members = sd.members
	}
	if sd.topic != "" && (!onlyMissing || c.Topic == "") {
		c.Topic, c.TopicSpans = sd.topic, sd.spans
	}
}

// shown is opened for a conversation a pane draws without navigating
// to it: layoutChannel calls it every frame for the Activity page's
// opened item (Panes.ChannelPane, the persistent detail panel) and the
// secondary column's conversation (OpenChannelSecondary), whose headers
// otherwise never read their member facepile, topic or tabs: the
// facepile stayed the loading tiles (polish-1113c). It reads once; after
// a failed members read retryOpened's backoff owns the next read. UI
// goroutine only.
func (b *backend) shown(ch string) {
	if ch == "" {
		return
	}
	if b.drawn == nil {
		b.drawn = map[string]time.Time{}
	}
	b.drawn[ch] = time.Now()
	if _, read := b.fetched[ch]; read {
		return // read or reading (opened's own guard rereads on the next navigation)
	}
	if sd := b.side[ch]; sd != nil && sd.fails > 0 {
		return
	}
	b.opened(ch)
}

// stillShown reports whether ch is the open conversation, or a pane drew
// it in the last second.
func (b *backend) stillShown(ch string) bool {
	if b.app != nil && b.app.router.Current().Channel() == ch {
		return true
	}
	t, ok := b.drawn[ch]
	return ok && time.Since(t) < time.Second
}

// retryOpened reads the channel's header again after a failed members
// read, if it's still the open conversation by then, or still drawn in
// a pane (shown).
func (b *backend) retryOpened(ch string, fails int) {
	a := b.app
	if a == nil || b.ctx == nil {
		return
	}
	d := 2 * time.Second
	for i := 1; i < fails && d < time.Minute; i++ {
		d *= 2
	}
	d = min(d, time.Minute)
	ctx := b.ctx
	time.AfterFunc(d, func() {
		if ctx.Err() != nil {
			return
		}
		a.Post(func() {
			if b.stillShown(ch) {
				b.opened(ch)
			}
		})
	})
}

// tabs is ch's tab bar. A bar built before the emoji map had a title's
// emoji (the map still loading on a warm start, a custom emoji newer than
// the cached map) is built again from the same reads once the map
// changes, so its emoji draw as images and a leading one becomes the
// tab's icon, as Slack's tab renders them from the emoji store each time.
// UI goroutine.
func (b *backend) tabs(ch string) []chTab {
	if src, ok := b.tabSrc[ch]; ok && b.core != nil {
		if v := b.core.EmojiVersion(); v != src.emoji {
			src.emoji = v
			b.tabSrc[ch] = src
			if tabs, _ := b.tabsOf(src.tabs, src.notMember); !reflect.DeepEqual(b.tabCache[ch], tabs) {
				if b.app != nil {
					b.app.keepTabSelection(ch, func() { b.tabCache[ch] = tabs })
				} else {
					b.tabCache[ch] = tabs
				}
			}
		}
	}
	return b.tabCache[ch]
}

// tabRead is what a channel's tab bar was built from (tabs): the tab
// reads, the membership, and the emoji map's version then.
type tabRead struct {
	tabs      []core.Tab
	notMember bool
	emoji     uint64
}

// noteTabRead records what ch's tab bar is being built from (tabs).
func (b *backend) noteTabRead(ch string, ts []core.Tab, notMember bool) {
	if b.tabSrc == nil {
		b.tabSrc = map[string]tabRead{}
	}
	var v uint64
	if b.core != nil {
		v = b.core.EmojiVersion()
	}
	b.tabSrc[ch] = tabRead{tabs: ts, notMember: notMember, emoji: v}
}

// resolved returns header info for a conversation that isn't in the sidebar
// (a closed DM, a channel opened from search) via core.ResolveConv, fetched
// once in the background. UI goroutine only.
func (b *backend) resolved(ch string) *convInfo {
	ci, ok := b.ext[ch]
	if ok && !b.extStale[ch] {
		return ci
	}
	delete(b.extStale, ch)
	if b.ext == nil {
		b.ext = map[string]*convInfo{}
	}
	b.ext[ch] = ci // in flight (nil), or the stale copy until the answer
	gen := b.extGen[ch]
	a := b.app
	go func() {
		ctx, cancel := context.WithTimeout(b.ctx, 15*time.Second)
		defer cancel()
		c, ok := b.core.ResolveConv(ctx, ch)
		if a == nil {
			return
		}
		if !ok {
			a.Post(func() {
				if b.extGen[ch] != gen {
					return
				}
				if b.extFailed == nil {
					b.extFailed = map[string]bool{}
				}
				b.extFailed[ch] = true // footerFor stops waiting on it (joinpane.go)
				a.Invalidate()
			})
			return
		}
		ci := &convInfo{ID: c.ID, Name: c.Name, Kind: c.Kind, User: c.User, Topic: c.Topic, NumMembers: c.NumMembers,
			TopicSpans: firstSpans(c.TopicDoc), Archived: c.IsArchived, Preview: c.Preview, Purpose: c.Purpose,
			Code: c.Code, CodeStatus: c.CodeStatus, AgentName: c.AgentName,
			File: c.File, FileID: c.FileID, Doc: c.Doc, NameSpans: c.NameSpans}
		if c.Kind == "im" {
			if u, ok := b.core.User(c.User); ok {
				ci.Name, ci.Avatar, ci.Bot, ci.Active = u.Name, u.Avatar, u.IsBot, u.Presence == "active"
				ci.StatusEmoji = u.StatusEmojiURL
			}
			ci.Self = c.User == b.selfID
		}
		a.Post(func() {
			if b.extGen[ch] != gen { // forgotten meanwhile (joined, left): its answer is stale
				return
			}
			b.applySide(ci, true) // opened()'s facepile, if it came first
			b.ext[ch] = ci
			delete(b.extFailed, ch)
			a.Invalidate()
		})
	}()
	return ci
}

// refreshExt has ch's resolved header info looked up again on its next
// resolved call, still returning the current copy until the answer
// lands (a resolve in flight is dropped: it predates the change).
// UI goroutine only.
func (b *backend) refreshExt(ch string) {
	if _, ok := b.ext[ch]; !ok {
		return
	}
	if b.extGen == nil {
		b.extGen = map[string]int{}
	}
	b.extGen[ch]++
	if b.extStale == nil {
		b.extStale = map[string]bool{}
	}
	b.extStale[ch] = true
}

// forgetExt drops ch's resolved header info (and any resolve in flight),
// so the next resolved call asks core again. Membership changes need it:
// the copy resolved while the viewer was in the channel says it isn't a
// preview, and once they leave it is the only conversation info left.
// UI goroutine only.
func (b *backend) forgetExt(ch string) {
	delete(b.ext, ch)
	delete(b.extStale, ch)
	delete(b.extFailed, ch)
	if b.extGen == nil {
		b.extGen = map[string]int{}
	}
	b.extGen[ch]++
}

func tabIcon(typ string) string {
	switch typ {
	case "canvas", "channel_canvas":
		return "canvas"
	case "list":
		return "list-tab" // Slack's list tab: a card with checklist rows
	case "bookmarks": // the Pins tab (Slack 2026: pin icon)
		return "pin"
	case "folder":
		return "bookmark"
	case "files":
		// useTabIconProps with resources_channel_tab off: canvas-browser
		// (canvas-browser-filled when selected)
		return "canvas-browser"
	case "workflows":
		return "workflow"
	}
	return "canvas"
}

// openTab opens a channel tab that has no view of its own in the body:
// a canvas or list tab Clack can't draw opens in the browser. (Pins and
// folder tabs are body tabs: pins_tab.go.)
func (b *backend) openTab(ch, key string) {
	for _, t := range b.tabCache[ch] {
		if t.key != key {
			continue
		}
		if t.fileID != "" && b.domain != "" {
			openURL("https://" + b.domain + ".slack.com/docs/" + b.teamID + "/" + t.fileID)
			return
		}
	}
	if b.app != nil {
		b.app.Toast("This tab opens in Slack")
	}
}

func (b *backend) avatar(id string) string {
	// getProfileImageForMember reads the member through getMemberById:
	// drawing the avatar keeps its member in the members cache
	// (store/members.go), and one it doesn't hold (never fetched, or
	// evicted) is asked for, the default picture until "names" redraws it
	return b.core.MemberAvatar(id, 72)
}

// ---- writes ----

func (b *backend) togglePresence() {
	p := "away"
	if !b.m.SelfActive {
		p = "auto"
	}
	b.m.SelfActive = !b.m.SelfActive
	b.m.version++
	go func() { _ = b.core.SetPresence(b.ctx, p) }()
}

// setMuted is Slack's muteChannelById / unmuteChannelById (boot-shared
// IDOy): the row goes muted (or not) at once, the "muted" channel pref is
// written (users.prefs.setNotifications, core.Mute), and a failed write
// quietly puts the row back. Muting a muted channel (or unmuting an
// unmuted one) does nothing, and there is no toast.
func (b *backend) setMuted(ch string, muted bool) {
	c := b.m.conv(ch)
	if c == nil || c.Muted == muted || b.core == nil {
		return
	}
	c.Muted = muted
	b.m.version++
	ctx, a := b.ctx, b.app
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		if err := b.core.Mute(ctx, ch, muted); err != nil && a != nil {
			a.Post(func() {
				if c := b.m.conv(ch); c != nil && c.Muted == muted {
					c.Muted = !muted
					b.m.version++
				}
			})
		}
	}()
}

func (b *backend) leave(ch string) {
	go func() {
		if err := b.core.Leave(b.ctx, ch); err != nil && b.app != nil {
			b.app.Post(func() { b.app.Toast("Couldn't leave: " + err.Error()) })
		}
	}()
}

func (b *backend) openBookmark(ch, id string) {
	for _, bm := range b.bookmarks[ch] {
		if bm.ID == id && b.app != nil && b.app.tripBookmark(ch, bm) { // workflows_wire.go
			return
		}
		if bm.ID == id && bm.Link != "" {
			openURL(bm.Link)
			return
		}
	}
}

func (b *backend) markRead(ch string) {
	c := b.m.conv(ch)
	if c == nil || (!c.Unread && c.Mentions == 0) || c.Latest == "" {
		return
	}
	ts := c.Latest
	c.Unread, c.Mentions, c.LastRead = false, 0, ts
	b.recount()
	b.m.version++
	go func() { _, _ = b.core.Mark(b.ctx, ch, ts) }()
}

func (b *backend) markAllRead() {
	for id, c := range b.m.Convs {
		if c.Unread && c.Kind != "im" && c.Kind != "mpim" && c.Kind != "app" {
			b.markRead(id)
		}
	}
}

func (b *backend) permalink(ch, ts string) string {
	host := "app.slack.com"
	if b.domain != "" {
		host = b.domain + ".slack.com"
	}
	u := "https://" + host + "/archives/" + ch
	if ts != "" {
		u += "/p" + strings.ReplaceAll(ts, ".", "")
	}
	return u
}

func (b *backend) signOut() {
	// Sign-out drops the account's disk cache (boot snapshot, messages,
	// images) before stop's background save could write it again.
	b.core.DropCache()
	_ = b.stop()
	if b.onSignOut != nil {
		b.onSignOut()
	}
}

// openURL opens a link in the system browser (http, https and mailto only;
// see openurl.Check). A var: tests catch what reaches the browser.
var openURL = func(u string) { _ = openurl.Open(u) }

// notificationMessage is showNotification's `message`: the message as
// Slack's store keeps it, which plugins drop (ShutUpSlackbot, StreamerMode
// set it to undefined) or rewrite (Censorship masks its text).
func notificationMessage(t *taut.Taut, ev core.Event) map[string]any {
	if m := t.SlackMessage(ev.Channel, ev.TS); m != nil {
		return m
	}
	m := map[string]any{"type": "message", "channel": ev.Channel, "ts": ev.TS, "text": ev.Msg.Text}
	if ev.Msg.User != "" {
		m["user"] = ev.Msg.User
	}
	if ev.Msg.BotID != "" {
		m["bot_id"] = ev.Msg.BotID
	}
	if ev.Msg.ThreadTS != "" {
		m["thread_ts"] = ev.Msg.ThreadTS
	}
	if ev.Msg.Subtype != "" {
		m["subtype"] = ev.Msg.Subtype
	}
	return m
}

// notificationText is what the desktop notification shows after the
// showNotification thunk: nothing when a plugin removed the message, the
// plugin's text when it changed it, else Clack's plain rendering.
func notificationText(args map[string]any, orig map[string]any, body string) (title, text string, ok bool) {
	m, has := args["message"]
	if !has || m == nil {
		return "", "", false
	}
	title = argStr(args, "title")
	switch v := m.(type) {
	case string:
		return title, v, true
	case map[string]any:
		if s, _ := v["text"].(string); s != "" {
			if o, _ := orig["text"].(string); s != o {
				return title, s, true
			}
		}
	}
	return title, body, true
}
