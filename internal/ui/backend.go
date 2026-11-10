package ui

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/applog"
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slick"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/popups"
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

	m       model
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	unsub   func()

	selfID string
	domain string
	teamID string
	ext    map[string]*convInfo // ResolveConv results for convs outside the sidebar

	// unsent drafts (drafts.list, refreshed on "draft" events), UI goroutine
	// only: Slack's "✎ N" on Drafts & sent and the pencil on channel rows.
	draftN     int // unsent drafts (Drafts & sent's ✎ badge)
	schedN     int // scheduled messages (its schedule-send badge)
	draftChans map[string]bool
	draftSeq   int

	tabCache map[string][]chTab
	tabsDone map[string]bool // opened() finished for the channel (the header is complete; reveal.go)
	// pluginsReady closes when the plugins enabled at start have started
	// (any goroutine may wait on it)
	pluginsReady chan struct{}
	pluginsOnce  sync.Once
	bookmarks    map[string][]core.Bookmark
	fetched      map[string]time.Time

	avMu    sync.Mutex
	avatars map[string]string
	asked   map[string]bool

	snapMu      sync.Mutex
	snapPending bool

	booted    time.Time
	onSignOut func()
	app       *App
}

// NewBackend wraps a core service. cacheDir is unused: the account's cache
// directory is the store's.
func newBackend(c *core.App, cacheDir string) *backend {
	b := &backend{core: c, pluginsReady: make(chan struct{}), tabCache: map[string][]chTab{}, bookmarks: map[string][]core.Bookmark{},
		fetched: map[string]time.Time{}, avatars: map[string]string{}, asked: map[string]bool{}}
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
	ch, unsub := b.core.Subscribe()
	b.unsub = unsub
	go func() {
		for ev := range ch {
			ev := ev
			a.Post(func() { b.handle(a, ev) })
		}
	}()
	// Live updates only when the user opted in (realtime.go); core polls
	// otherwise. Set before Boot so the socket is never opened unasked.
	b.core.SetRealtime(a.realtimeOn())
	if b.core.S.HasCachedPrefs() { // the cached account theme paints the first frame
		a.applyAccountPrefs(b.core.Prefs())
	}
	// Everything the first channel frame shows starts now, in parallel
	// with core's boot: the plugin runtimes (JS engine, bundles from the
	// bytecode cache, plugin start; their state mirror re-reads at
	// "booted"), Block Kit, the restored channel's header (tabs, bookmarks,
	// member count and facepile) and the drafts' sidebar markers. The
	// window stays covered until they're in (reveal.go).
	b.startTautAsync(a)
	b.startBlockkit(a)
	go a.settlePluginSlots(b) // release reserved plugin slots, save the learned ones
	if ch := a.router.Current().Channel(); ch != "" {
		b.opened(ch)
	}
	b.loadDrafts(a)
	go func() {
		t0 := time.Now()
		if err := b.core.Boot(b.ctx); err != nil {
			log.Printf("ui: boot: %v", err)
			a.Post(func() { a.Toast("Couldn't connect to Slack: " + err.Error()) })
		}
		snap := b.core.Snapshot()
		log.Printf("ui: core ready %v after boot start (%d convs)", time.Since(t0).Round(time.Millisecond), len(snap.Convs))
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
		})
	}()
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
		n, sched, chans := 0, 0, map[string]bool{}
		for _, d := range ds {
			if d.DateScheduled != 0 {
				sched++  // Slack's getScheduledDraftsCount: date_scheduled set
				continue // scheduled messages live in the Scheduled tab
			}
			n++
			if d.Channel != "" && d.ThreadTS == "" {
				chans[d.Channel] = true
			}
		}
		a.Post(func() {
			if seq != b.draftSeq {
				return
			}
			b.draftN, b.schedN, b.draftChans = n, sched, chans
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
		closeTaut(s, t)
		_ = b.core.Close()
	}()
	return done
}

// resnapshot rebuilds the model off the UI goroutine (sidebar_changed,
// resync), coalescing bursts.
func (b *backend) resnapshot(a *App) {
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
		snap := b.core.Snapshot()
		a.Post(func() { b.apply(a, snap) })
	}()
}

// apply replaces the model from a snapshot.
func (b *backend) apply(a *App, s core.Snapshot) {
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
			NumMembers: c.NumMembers, Members: c.Members, Muted: c.Muted, TopicSpans: firstSpans(c.TopicDoc),
			Archived: c.IsArchived, Preview: c.Preview, Purpose: c.Purpose,
			Code: c.Code, CodeStatus: c.CodeStatus, AgentName: c.AgentName}
		if old := m.Convs[c.ID]; old != nil {
			// keep what opened() fetched (count, facepile) across snapshots
			if ci.NumMembers == 0 {
				ci.NumMembers = old.NumMembers
			}
			if len(ci.Members) == 0 {
				ci.Members = old.Members
			}
		}
		if c.Kind == "im" {
			if u, ok := s.Users[c.User]; ok {
				ci.Avatar = u.Avatar
				ci.StatusEmoji = u.StatusEmojiURL
				ci.Active = u.Presence == "active"
				ci.Bot = u.IsBot
				if u.Name != "" {
					ci.Name = u.Name
				}
			}
			ci.Self = c.User == s.Self.ID
			if ci.Bot {
				ci.Kind = "app"
			}
		}
		if c.Kind == "mpim" {
			ci.Name = b.mpimName(c, s.Self.ID)
		}
		if cnt, ok := s.Counts[c.ID]; ok {
			ci.Unread, ci.Mentions, ci.LastRead, ci.Latest = cnt.Unread, cnt.Mentions, cnt.LastRead, cnt.Latest
		}
		convs[c.ID] = ci
	}
	m.Convs = convs
	m.Sections = m.Sections[:0]
	for _, sec := range s.Sections {
		ss := sbSection{ID: sec.ID, Name: sec.Name, Type: sec.Type, Emoji: sec.Emoji, EmojiURL: sec.EmojiURL, Collapsed: sec.Collapsed,
			Filter: sec.Filter, Sort: sec.Sort}
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
	}
	return "Section"
}

func (b *backend) mpimName(c *core.Conv, self string) string {
	if n, ok := b.core.MpdmCustomName(c.ID); ok { // mpdm_naming (core/mpdm_name.go)
		return n
	}
	var names []string
	for _, id := range c.Members {
		if id == self {
			continue
		}
		if u, ok := b.core.User(id); ok && u.Name != "" {
			names = append(names, u.Name)
		}
	}
	if len(names) == 0 {
		return strings.TrimPrefix(c.Name, "mpdm-")
	}
	return strings.Join(names, ", ")
}

// recount derives the rail badges from conv counts.
func (b *backend) recount() {
	m := &b.m
	m.DMCount, m.DMUnread, m.HomeCount, m.HomeUnread, m.ActivityCount = 0, false, 0, false, 0
	for _, c := range m.Convs {
		if c.Muted {
			continue
		}
		switch c.Kind {
		case "im", "mpim", "app":
			if c.Unread || c.Mentions > 0 {
				m.DMUnread = true
				m.DMCount += max(c.Mentions, 1)
			}
		default:
			m.HomeCount += c.Mentions
			m.ActivityCount += c.Mentions
			if c.Unread {
				m.HomeUnread = true
			}
		}
	}
	m.HomeCount += m.DMCount
	if b.app != nil {
		b.app.updateDock()
	}
}

// wantsNotify applies Notifications > Desktop notifications (users.prefs
// all_notifications_prefs: the channel's desktop override, else
// global_desktop): everything, mentions_dms (DMs, mentions and keywords:
// mention) or nothing.
func (b *backend) wantsNotify(ch string, mention bool) bool {
	p := b.core.Prefs()
	level := p.GlobalDesktop
	if o := p.ChannelNotify[ch].Desktop; o != "" && o != "default" {
		level = o
	}
	switch level {
	case "everything":
		return true
	case "nothing":
		return false
	}
	return mention
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
		// (ephemerals, e.g. Slackbot's "you can't @channel here", never notify)
		if ev.Msg != nil && ev.Msg.User != b.selfID && !ev.Msg.System && !b.core.Interactive().Ephemeral(ev.Channel, ev.TS) {
			cur := a.router.Current().Channel()
			c := m.conv(ev.Channel)
			direct := ev.ConvKind == "im" || ev.ConvKind == "mpim"
			if b.wantsNotify(ev.Channel, ev.Mention || direct) && (c == nil || !c.Muted) && (cur != ev.Channel || !a.focused) {
				// through Taut's showNotification thunk: plugins may change or
				// drop it (native: the desktop notification)
				title, body := b.notifyTitle(ev, c), richtext.PlainText(ev.Msg.Doc)
				msg := notificationMessage(a.Taut(), ev)
				a.dispatch("showNotification", map[string]any{"title": title, "message": msg, "body": body, "channelId": ev.Channel, "ts": ev.TS},
					func(ctx context.Context, args map[string]any) (any, error) {
						if t, b, ok := notificationText(args, msg, body); ok {
							a.notifyMessage(t, b)
						}
						return nil, nil
					})
			}
		}
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
			b.avMu.Lock()
			b.avatars[ev.User.ID] = ev.User.Avatar
			b.avMu.Unlock()
			if ev.User.ID == b.selfID {
				m.SelfName, m.SelfAvatar = ev.User.Name, ev.User.Avatar
			}
			for _, c := range m.Convs {
				if c.Kind == "im" || c.Kind == "app" {
					if c.User == ev.User.ID {
						c.Avatar, c.Name, c.StatusEmoji = ev.User.Avatar, ev.User.Name, ev.User.StatusEmojiURL
					}
				}
			}
			changed = true
		}
	case "conv", "conv_removed", "sidebar_changed", "resync", "team": // "prefs" is handled above (it resnapshots too)
		b.resnapshot(a)
	case "auth_failed":
		// core stopped all traffic on a dead session: back to sign-in
		a.sessionEnded()
	case "names":
		// core resolved channel/user/bot names in the background: re-read
		// the visible conversations so mentions and headers re-render.
		b.resnapshot(a)
		a.refreshVisible()
		for id := range b.ext {
			delete(b.ext, id) // re-resolve closed-DM headers
		}
	case "tabs":
		delete(b.fetched, ev.Channel)
		if a.router.Current().Channel() == ev.Channel {
			b.opened(ev.Channel)
		}
	case "connection":
		m.Connected = ev.Connected
		changed = true
	}
	if changed {
		b.recount()
		m.version++
		a.Invalidate()
	}
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

func (b *backend) notifyTitle(ev core.Event, c *convInfo) string {
	name := ev.Msg.Name
	if name == "" {
		if u, ok := b.core.User(ev.Msg.User); ok {
			name = u.Name
		}
	}
	if c != nil && (c.Kind == "channel" || c.Kind == "private") {
		return name + " in #" + c.Name
	}
	return name
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

// opened is called when a conversation becomes current: fetch its
// bookmarks/pins (tabs) and member facepile in the background.
func (b *backend) opened(ch string) {
	if t, ok := b.fetched[ch]; ok && time.Since(t) < 2*time.Minute {
		return
	}
	b.fetched[ch] = time.Now()
	a := b.app
	go func() {
		ctx, cancel := context.WithTimeout(b.ctx, 20*time.Second)
		defer cancel()
		// Slack's tab bar (core.Tabs: canvases, lists, bookmarks, files…).
		var tabs []chTab
		var bms []core.Bookmark
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
			log.Printf("ui: tabs %s: %v", ch, err)
		}
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
			tabs = append(tabs, chTab{key: "tab:" + t.ID, label: label, icon: icon, fileID: t.FileID, unread: t.Unread,
				spans: tabLabelSpans(label, b.core.Emoji)})
			bms = append(bms, t.Bookmarks...)
		}
		var members []string
		if tried {
			log.Printf("ui: members %s: %d err=%v", ch, len(us), merr)
			if merr == nil {
				for _, u := range us {
					if len(members) == 3 {
						break
					}
					if u.Deleted || u.IsBot {
						continue
					}
					members = append(members, u.ID)
					if u.Avatar != "" {
						b.avMu.Lock()
						b.avatars[u.ID] = u.Avatar
						b.avMu.Unlock()
					} // else avatar() resolves it via core.User / EnsureUsers
				}
			}
		}
		if a == nil {
			return
		}
		if ierr == nil && !info.IsMember {
			// Slack's tab bar marks no canvas unread in a channel you
			// aren't in (C08E083LZ9A's "Untitled", 2026-11-08).
			for i := range tabs {
				tabs[i].unread = false
			}
		}
		a.Post(func() {
			b.tabCache[ch] = tabs
			b.bookmarks[ch] = bms
			if b.tabsDone == nil {
				b.tabsDone = map[string]bool{}
			}
			b.tabsDone[ch] = true
			if c := b.m.conv(ch); c != nil {
				if ierr == nil && info.Conv.NumMembers > 0 {
					c.NumMembers = info.Conv.NumMembers
				}
				if len(members) > 0 {
					c.Members = members
				}
				if ierr == nil && info.Conv.Topic != "" {
					c.Topic = info.Conv.Topic
					c.TopicSpans = firstSpans(info.Conv.TopicDoc)
				}
			}
			a.Invalidate()
		})
	}()
}

func (b *backend) tabs(ch string) []chTab { return b.tabCache[ch] }

// resolved returns header info for a conversation that isn't in the sidebar
// (a closed DM, a channel opened from search) via core.ResolveConv, fetched
// once in the background. UI goroutine only.
func (b *backend) resolved(ch string) *convInfo {
	if ci, ok := b.ext[ch]; ok {
		return ci
	}
	if b.ext == nil {
		b.ext = map[string]*convInfo{}
	}
	b.ext[ch] = nil // in flight
	a := b.app
	go func() {
		ctx, cancel := context.WithTimeout(b.ctx, 15*time.Second)
		defer cancel()
		c, ok := b.core.ResolveConv(ctx, ch)
		if !ok || a == nil {
			return
		}
		ci := &convInfo{ID: c.ID, Name: c.Name, Kind: c.Kind, User: c.User, Topic: c.Topic, NumMembers: c.NumMembers,
			TopicSpans: firstSpans(c.TopicDoc), Archived: c.IsArchived, Preview: c.Preview, Purpose: c.Purpose,
			Code: c.Code, CodeStatus: c.CodeStatus, AgentName: c.AgentName}
		if c.Kind == "im" {
			if u, ok := b.core.User(c.User); ok {
				ci.Name, ci.Avatar, ci.Bot, ci.Active = u.Name, u.Avatar, u.IsBot, u.Presence == "active"
				ci.StatusEmoji = u.StatusEmojiURL
			}
			ci.Self = c.User == b.selfID
		}
		a.Post(func() {
			b.ext[ch] = ci
			a.Invalidate()
		})
	}()
	return nil
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
		return "files"
	case "workflows":
		return "workflow"
	}
	return "canvas"
}

// openTab opens a channel tab. Canvases and lists open in the browser
// (no native canvas editor yet); the Bookmarks tab lists its links.
func (b *backend) openTab(ch, key string) {
	for _, t := range b.tabCache[ch] {
		if t.key != key {
			continue
		}
		if (t.icon == "bookmark" || t.icon == "pin") && len(b.bookmarks[ch]) > 0 && b.app != nil {
			var items []MenuItem
			for _, bm := range b.bookmarks[ch] {
				bm := bm
				items = append(items, MenuItem{Label: bm.Title, Icon: "link", Do: func() { openURL(bm.Link) }})
			}
			b.app.menu(b.app.PointerPos(), items)
			return
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
	b.avMu.Lock()
	url := b.avatars[id]
	b.avMu.Unlock()
	if url != "" {
		return url
	}
	if u, ok := b.core.User(id); ok && u.Avatar != "" {
		b.avMu.Lock()
		b.avatars[id] = u.Avatar
		b.avMu.Unlock()
		return u.Avatar
	}
	// unknown, or known only partially (edge-API members carry no avatar):
	// fetch the full profile once; the next frame picks it up from core.
	b.avMu.Lock()
	ask := !b.asked[id]
	b.asked[id] = true
	b.avMu.Unlock()
	if ask && b.ctx != nil {
		a := b.app
		go func() {
			b.core.EnsureUsers(b.ctx, []string{id})
			if a != nil {
				a.Invalidate()
			}
		}()
	}
	return ""
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
// see openurl.Check).
func openURL(u string) { _ = openurl.Open(u) }

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
