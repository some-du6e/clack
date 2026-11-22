package messages

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"gioui.org/f32"
	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/ctxclick"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

type itemKind uint8

const (
	itMsg itemKind = iota
	itDay
	itUnread
	itIntro
	itLoading
	itReplies // thread: "N replies" divider
	itThreadHead
	// itBottom is BaseMessageList's bottomSpacer row (boot-render bU1n):
	// once the newest messages are loaded (reachedEnd) the channel list
	// ends with space.150 (24px) above the composer, space.100 (16px)
	// while the channel's context bar shows (getBottomSpacerHeight).
	itBottom
)

type item struct {
	kind   itemKind
	idx    int // index into snap.msgs (itMsg)
	day    time.Time
	header bool
	key    string
	group  []int // join/leave summary: indices into snap.msgs
	mark   uint8 // thread: the separator's type, the "New" divider's ghost (thread_unread.go)
}

// toolbar buttons, in the real 2026 order for this account.
const (
	tbAddReact = iota
	tbReply
	tbForward
	tbTodo
	tbList
	tbMore
	tbCount
)

var tbTips = [tbCount]string{"Add reaction…", "Reply in thread", "Forward message…", "Add to-do", "Add to list…", "More actions"} // aria-labels of the live 2026 client
var tbIcons = [tbCount]string{"add-reaction", "comment-lines", "forward-msg", "todo-add", "list-add", "kebab"}

// listState is the per-conversation UI state shared by channel and thread
// views. It is cached across view instances (UI goroutine only), so
// switching back to a conversation keeps its rows and scroll position.
type listState struct {
	hb *hub
	m  *model // what the list shows: chm, or its window
	// chm is a channel list's conversation model (the newest messages,
	// every list's); win is the window it shows instead, its own (hub.go
	// newWindow), nil when it shows chm
	chm, win *model
	// highlightReq is the message the list was last asked to highlight
	// (Highlight), whether or not it holds it
	highlightReq string
	thread       bool
	ch           string
	ts           string // thread ts
	// container is the view container the list is in ("" main, "split";
	// see stateIn)
	container string
	// paneID is the list's slackc pane (its listCache key): the plugin
	// roots it draws are its own, never another list's (slackc.Pane)
	paneID string

	snap   snap
	rows   map[string]*row
	items  []item
	built  uint64 // snap.rev items were built from
	dayKey int    // date the items were built on, yyyymmdd (Today/Yesterday rollover)
	dense  bool   // messages_theme the items were built under (thread grouping)

	list     layout.List
	contentH int // total item height when all items fit (see layoutList)
	heights  []int
	pos      []int // y of visible items (valid where heights > 0 this frame)
	visible  [2]int

	openLastRead string // the "New" line position, fixed while open
	opened       bool
	thr          threadRead // the thread pane's read state (thread_unread.go)
	// rootHandedOn: the thread's root turned out to be a reply and the
	// Host was told (ReplyRootRedirector); cleared each time the pane
	// opens again (ThreadView.Reopen) and when the list lets go of its
	// messages (releaseEvicted)
	rootHandedOn bool
	tl           threadList // the thread list's paging (thread_load.go)
	positioned   bool
	markedTS     string
	lastMark     time.Time

	pointer   f32.Point
	pointerIn bool
	ctx       ctxPress // this frame's context-menu press (context.go)
	hoverIdx  int
	// laid is the message rows the last layout put on screen, by key:
	// the hover is found in them (layoutList), so a rebuild that moved
	// items to other indices since (older messages paged in above, a day
	// divider, a joins summary forming) can't hand the hover, and its
	// toolbar, to whatever message took the old index
	laid []laidRow

	tb           [tbCount]widget.Clickable
	pbtn         map[string]*widget.Clickable // persistent "Message actions" buttons (persist.go)
	tbRect       image.Rectangle
	tbFor        int    // the item the toolbar was drawn for this frame, -1 none
	tbKey        string // its key, for the next frame's hover (laid)
	jump         widget.Clickable
	retryBtn     widget.Clickable
	helpLink     widget.Clickable // ThreadsError's "drop us a line." (thread.go)
	pendAt       time.Time        // when the thread pane started waiting for its replies (FlexpaneLoading's animation)
	offlineDrawn bool             // the thread pane drew LargeOfflineIndicator this frame (thread.go)
	closeBtn     widget.Clickable

	tip     string
	tipRect image.Rectangle
	tipItem int
	tipG    groupTip  // the hovered pill's ReactionTip (reaction_tip.go)
	tipKey  string    // which pill (item key + name), to time the delay
	tipAt   time.Time // when the pointer reached it

	// the hovered sender status's tooltip (status_tip.go): its content,
	// the icon's box in list coordinates, the row (item) and since when
	stOn   bool
	stTip  statusTip
	stRect image.Rectangle
	stItem int
	stKey  string
	stAt   time.Time

	lastTyping time.Time

	noMark      bool
	unreadSeen  bool         // the "New" line (or no line at all) has been on screen
	pane        slackc.Props // $pane for components in a thread pane
	inj         injState     // plugin-injected messages
	pend        pendingEdit  // an edit waiting for taut's actionable message
	viewsOn     bool         // rows may show plugin views
	openUnread  bool         // the server said "unread" when the view opened (e.g. marked unread)
	jumpPages   int          // pages a day pill's "Jump to…" loaded (banners.go)
	aroundFor   string       // the jump the window around was loaded for (loadAround)
	origin      image.Point  // view origin in window coordinates
	size        image.Point  // the list's size in the last frame
	lastOlder   time.Time
	lastNewer   time.Time
	highlight   string
	highlightAt time.Time
	// hlSeen: the highlighted row has been drawn (its unmount ends the
	// flash: MessageBackground's componentWillUnmount); hlRestart: the
	// placement starts the flash (not a repeat jump to a ts still flashing,
	// whose highlight prop never turns from false to true)
	hlSeen, hlRestart bool
	// find in thread (thread_find.go): the highlight words (the query of
	// the last results), the current result and the result flashing
	findWords, findTS string
	findFlash         string
	findFlashAt       time.Time
	sel               selection
	sb                scrollbar
	pendingJump       string
	// jumpLazy: the pending jump is highlightRoot's (highlightLazy) or a
	// re-highlight in an open thread pane: a message whose top is already
	// on screen isn't scrolled to
	jumpLazy bool
	// thrOpening: the thread pane just opened (ThreadView.Reopen), so a jump
	// starts it at getThreadStartPosition's message
	thrOpening bool
	// thrMark: the scrollmark the pane saved as it unmounted (CloseThread),
	// for its first positioning (threadStartPosition)
	thrMark threadMark
	// place is a jump's placement, made in the next layout: the item keyed
	// place.key place.off under the list's top (placeJump). Nothing holds
	// it there after: Slack's anchor drops at its own scroll's echo, and
	// AnchorLayout's own anchoring keeps the row on the anchor line.
	place jumpPlace
	// the anchor line under the list's top and STICKY_EPSILON, px, as the
	// last layout had them (lineOff)
	linePx, stickyPx int
	// endGap is how far the content ran on past the list's bottom in the
	// last layout: 0 at its end, math.MaxInt32 when its last item wasn't
	// laid out
	endGap int
	// handAsked: a window that met the newest messages asked the
	// conversation's model to read them again (handOver); handWant is the
	// first of its reads that began after the join
	handAsked bool
	handWant  uint64
	// sendCID is the client_msg_id of the viewer's message from the
	// composer beside the list, which the list jumps to once it's in
	// (ChannelView.JumpToOwnSend); sendAt is when it went out.
	sendCID   string
	sendAt    time.Time
	evictSeen uint64 // the model's evictGen this list released its rows for (releaseEvicted)

	// the list as the last frame left it (anchoring in layoutList)
	laidOut   bool
	laidPos   layout.Position
	laidBuilt uint64
	laidAt    time.Time      // the frame (gtx.Now) of that layout
	laidH     int            // its height (setContainerHeight)
	pageH     int            // a PageUp's or PageDown's scroll, px (pageKey)
	tailH     map[string]int // the last tailItems items' heights as last laid out, by key (gapToEnd)
	// preparedAt is the frame (gtx.Now) prepare synced the list for:
	// layoutList draws that copy and doesn't sync again in that frame
	preparedAt time.Time

	rc rowCtx // this frame's row inputs (rows keep a pointer to it)

	// hover toolbar state for its natives (MessageActions)
	tbRow   *row
	tbSize  image.Point
	tbAt    int             // tbRow's MessageActions position (actionsAt)
	tbHov   int             // hovered toolbar button, -1 none
	tbHold  toolbarHold     // the popover a toolbar button opened, holding its row's hover
	tbBtns  []int           // the buttons tbRow's toolbar has, in order (actions_rules.go)
	tbLabel [tbCount]string // their labels (aria-label and tooltip) for tbRow
	tbQAs   [tbCount]string // … and data-qa
	quick   quickBar        // one-click reactions before the buttons (quickreact.go)
	tilt    toolbarTilt     // the buttons' hover transitions (toolbar_tilt.go)
	tbCard  color.NRGBA     // the toolbar card's background, under the buttons' hover fade

	// actScroll is the list's scroll under the hover toolbar's and the
	// persistent menu's buttons (toolbar_scroll.go).
	actScroll gesture.Scroll

	kb kbNav // keyboard focus and the selected message (kbnav.go)

	// the floating banners and the day pills' menu (banners.go)
	ub          unreadBannerState
	pendingDate time.Time                    // jumpToTimestamp's time, paged back to
	dateFor     string                       // the date jump's ts a window was opened around (pendingDateJump)
	own         [2]string                    // the model's first and last message, before plugins' injected ones (ends)
	dayReq      *dayMenuReq                  // a day pill clicked this frame
	dayBtns     map[string]*widget.Clickable // the dividers' pills, by day
	stickyDay   widget.Clickable             // the sticky pill
	latestR     image.Rectangle              // the "Latest messages" pill drawn last frame (tests)
	unreadTh    unreadThreads                // the reply bars' "Unread" marks (unread_threads.go)

	// system messages under system_messages_redesign_m1 (system.go)
	sysOn, sysInit, sysMissing bool
	sysUsers                   uint64
	// sysHidden: the hidden-people state the system texts were written
	// under (hiddenKey, system.go)
	sysHidden string
	// activeTS is the root of the thread open in the side pane, this frame
	// (active_thread.go)
	activeTS string
}

var listCache = map[string]*listState{}

// Off-screen rows keep their message but drop their laid-out text (glyph
// paths are most of a row's memory) once they have not been drawn for
// rowIdle; scrolling back lays them out again (~0.1 ms a row).
const (
	rowIdle    = 30 * time.Second
	sweepEvery = 10 * time.Second
)

var lastSweep time.Time

// sweepRows releases idle rows' layouts in every cached list, not just the
// one on screen (the views for other channels stay cached).
func sweepRows(now time.Time) {
	if now.Sub(lastSweep) < sweepEvery {
		return
	}
	lastSweep = now
	for _, s := range listCache {
		if _, _, _, _, ok := s.ordered(); ok {
			continue // a selection may span rows off screen (copy reads their text)
		}
		if s.releaseEvicted() {
			continue
		}
		for _, r := range s.rows {
			if r.rv != nil && r.edit == nil && now.Sub(r.drawn) > rowIdle {
				// the trees' msgs it remembered and laid over its message
				// (slackc's sweep has let their roots go, and a remount
				// hands the natives none until JS draws them again): not
				// kept alive for the row's lifetime
				r.textMemo, r.blocksMemo, r.filesMemo, r.attsMemo = fpMemo{}, fpMemo{}, fpMemo{}, fpMemo{}
				r.ovEl = elMemo{}
				if r.textOv != nil || r.filesOv != nil || r.attsOv != nil || r.wrapMsg != nil || r.inMsg != nil {
					r.textOv, r.filesOv, r.attsOv = nil, nil, nil
					r.wrapOv, r.msgOv, r.wrapMsg, r.inMsg = 0, 0, nil, nil
					r.recompose()
				}
				r.rv.Release()
			}
		}
	}
}

// releaseEvicted lets a list that isn't on screen drop its rows, items and
// copy of the messages once core trimmed its conversation (Slack's message
// cache budget, core/evict.go): they'd only be rebuilt from the trimmed
// model, and it starts from the newest messages (or the unread line) the
// next time it shows, as Slack's reset histories do. Reports whether it
// did.
func (s *listState) releaseEvicted() bool {
	s.m.mu.Lock()
	gen := s.m.evictGen
	s.m.mu.Unlock()
	if gen == s.evictSeen || s.onScreen() {
		return false
	}
	s.evictSeen = gen
	s.rows = map[string]*row{}
	s.items = nil
	s.snap.msgs = nil
	s.snap.rev = 0
	s.list.Position = layout.Position{}
	s.positioned, s.laidOut = false, false
	s.place, s.pendingJump, s.laid = jumpPlace{}, "", s.laid[:0]
	s.rootHandedOn = false // a reply-rooted pane hands on again when it next loads
	return true
}

// onScreen reports whether s is the list last drawn in its view container.
func (s *listState) onScreen() bool {
	if s == lastChannel || s == lastThread {
		return true
	}
	for _, p := range paneLast {
		if p != nil && *p == s {
			return true
		}
	}
	return false
}

// forgottenCores counts ForgetCore's calls; the next list drawn lets go
// of the forgotten sessions' lists (dropForgottenLists).
var forgottenCores atomic.Uint64

// droppedAt is forgottenCores when the lists were last swept (UI goroutine).
var droppedAt uint64

// dropForgottenLists lets go of the lists, and the last-drawn slots, whose
// hub is no longer its core's: a session ForgetCore let go of. The lists
// are the UI goroutine's and ForgetCore runs on a session's stop, so the
// next list drawn does it. Each kept its hub, and through it the session
// and its App, reachable until another list took its key (ci-speed-3:
// listCache held more finished tests' Apps than any root but one).
func dropForgottenLists() {
	n := forgottenCores.Load()
	if n == droppedAt {
		return
	}
	droppedAt = n
	var hostCore *core.App
	if lastHost != nil {
		hostCore = lastHost.Core()
	}
	hubsMu.Lock()
	defer hubsMu.Unlock()
	stale := func(s *listState) bool { return s != nil && s.hb != nil && hubs[s.hb.app] != s.hb }
	for k, s := range listCache {
		if stale(s) {
			delete(listCache, k)
		}
	}
	if stale(lastChannel) {
		lastChannel = nil
	}
	if stale(lastThread) {
		lastThread = nil
	}
	for k, p := range paneLast {
		if p != nil && stale(*p) {
			delete(paneLast, k)
		}
	}
	if _, ok := hubs[hostCore]; lastHost != nil && !ok {
		lastHost = nil
	}
}

// lastHost is the Host of the last message list drawn. Host components
// (drawn inside plugin trees, possibly outside any list) reach core and
// the popups through it; nil until a list has been drawn.
var lastHost Host

func stateFor(h Host, ch, ts string) *listState { return stateIn(h, ch, ts, "") }

// stateIn is stateFor for a list in view container pane: "" is the main
// window's views, "split" Slack's split view. Each container keeps its own
// rows, scroll position and inline editors over the shared conversation
// model, so the same conversation can be open in both.
func stateIn(h Host, ch, ts, pane string) *listState {
	dropForgottenLists()
	key := ch
	if ts != "" {
		key = ch + "/" + ts
	}
	if pane != "" {
		key = pane + ":" + key
	}
	hb := hubFor(h)
	if s, ok := listCache[key]; ok && s.hb == hb {
		return s
	}
	// a new core (signed in again, another account): fresh state
	s := &listState{hb: hb, ch: ch, ts: ts, thread: ts != "", container: pane, paneID: key, rows: map[string]*row{}, hoverIdx: -1, tbFor: -1}
	if ts != "" {
		s.m = hb.threadModel(ch, ts)
	} else {
		s.m = hb.channel(ch)
		s.chm = s.m
	}
	s.list.Axis = layout.Vertical
	s.list.ScrollToEnd = true
	// Slack top-aligns a short thread (its reply box right under the last
	// reply): the thread list draws from its top what doesn't fill it, and
	// still keeps to its end once the thread outgrows it (Gio patch 45)
	s.list.StartAligned = ts != ""
	if len(listCache) > 2*maxModels {
		evictList()
	}
	listCache[key] = s
	return s
}

// evictList lets the least recently drawn list go from the cache: one
// that isn't on screen and drew in neither of the latest frames, and only
// its window goes (dropWindow: it takes no more live changes). Its view
// may still be mounted in the ui root's LRU; drawn again, the list takes
// its window back (adopt). Map order picked any list, the one on screen
// too, and its window stopped taking reactions, edits and deletes.
func evictList() {
	victim, at := "", time.Time{}
	for k, s := range listCache {
		if s.onScreen() || s.mounted() {
			continue
		}
		if victim == "" || s.laidAt.Before(at) {
			victim, at = k, s.laidAt
		}
	}
	if old := listCache[victim]; old != nil {
		old.hb.dropWindow(old.win)
		delete(listCache, victim)
	}
}

// ensure is the list's load. A channel list opened at a message the
// conversation hasn't loaded (its newest messages aren't loaded or
// loading, and core's don't reach it) loads only the window around it:
// Slack's pane mounted at startTs requests around it, two calls, never
// the newest page first. Else its model's load (ensureLoaded).
func (s *listState) ensure() {
	if !s.thread && s.chm != nil && s.win == nil && s.pendingJump != "" && s.aroundFor != s.pendingJump {
		s.chm.mu.Lock()
		cold := !s.chm.loaded && !s.chm.loading
		s.chm.mu.Unlock()
		if app := s.hb.app; cold && app != nil && !app.S.TimelineHolds(s.ch, s.pendingJump) {
			s.aroundFor = s.pendingJump
			s.openWindow(s.pendingJump)
			return
		}
	}
	s.hb.ensureLoaded(s.m)
}

// openWindow shows the window around message ts in the list: a window
// model of its own (the conversation's model, every list's, keeps the
// newest messages), loaded from the window core holds that has it, else
// Slack's two calls (hub.loadWindow). The list draws it as it lands.
func (s *listState) openWindow(ts string) {
	if s.chm == nil { // a channel list from stateIn only
		return
	}
	s.hb.dropWindow(s.win)
	w := s.hb.newWindow(s.ch)
	s.win = w
	s.useModel(w)
	s.place, s.handAsked = jumpPlace{}, false
	s.hb.loadWindow(w, ts)
}

// reloadAt is where the list's window reloads when the list takes it back
// (adopt): the message on the anchor line when the list last drew, so
// the reader's rows come back, else the window's first message.
func (s *listState) reloadAt() string {
	key, _ := s.lineRow(s.linePx)
	ts := strings.TrimPrefix(key, "g") // a group row: its first message
	w := s.win
	w.mu.Lock()
	defer w.mu.Unlock()
	if ts != "" && w.find(ts) >= 0 {
		return ts
	}
	if len(w.msgs) > 0 {
		return w.msgs[0].m.TS
	}
	return s.aroundFor
}

// leaveWindow shows the conversation's newest messages again (its model,
// which took every live message meanwhile).
func (s *listState) leaveWindow() {
	if s.win == nil {
		return
	}
	s.hb.dropWindow(s.win)
	s.win = nil
	s.useModel(s.chm)
	// the newest messages: the list keeps to their end from now on, so a
	// Position{} written for them (jumpToLatest, Entered) is their end in
	// the next sync too (its stick test reads the value from before it)
	s.list.ScrollToEnd = true
	s.handAsked = false
	s.hb.ensureLoaded(s.m)
}

// jumpToLatest is the pane's jumpToMostRecent (ArchivesBanner's "Latest
// messages", a send): jumpToTimestamp({ts: null}), which clears the
// channel's scrollmark, so the conversation's newest messages show, at
// their end.
func (s *listState) jumpToLatest() {
	if s.win == nil {
		return
	}
	s.leaveWindow()
	s.pendingJump, s.aroundFor, s.place = "", "", jumpPlace{}
	s.list.Position = layout.Position{BeforeEnd: false}
	s.stopScroll()
	s.positioned = true
}

// handOver: the window met the conversation's newest messages and core
// joined it to their timeline, so the list shows them, read again from
// core (Slack's union of the two slices, in the state the pane reads).
// The window stays on screen until the conversation's model has read
// them in a read begun after the join (one in flight then began before
// it, and a failed one holds nothing: the window stays until a later read
// lands), then the rebuild keeps the reader's row where it was, or a
// pending jump places its message. Reports whether it went.
func (s *listState) handOver() bool {
	m := s.chm
	m.mu.Lock()
	if !s.handAsked {
		s.handAsked = true
		s.handWant = m.readGen + 1 // the next read: it begins after the join
	}
	ok, idle := m.readOK >= s.handWant, !m.loading
	m.mu.Unlock()
	if ok {
		s.leaveWindow()
		return true
	}
	if idle {
		s.hb.refresh(m) // starts the read, unless it's backing off a failure
	}
	return false
}

// newestHold reports whether the conversation's newest messages hold ts
// (loaded from its first message on, or its whole history).
func (s *listState) newestHold(ts string) bool {
	m := s.chm
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loaded && len(m.msgs) > 0 && (!m.hasMore || !tsLess(ts, m.msgs[0].m.TS))
}

// prepare is layoutList's load and sync, earlier in the frame at frame
// (ThreadView.Prepare): what the ui root decides from the list before it
// lays the list out (the reply box) is then what the list draws.
func (s *listState) prepare(h Host, frame time.Time) {
	s.endUnmountedFlash()
	s.adopt(frame)
	s.ensure()
	s.sync(h, time.Now())
	s.preparedAt = frame
}

// endUnmountedFlash ends a jump's flash whose list missed a frame since
// its row was drawn: off screen, its rows unmounted, and Slack's
// MessageBackground clears the highlight on unmount. Before the frame
// marks the list drawn (adopt).
func (s *listState) endUnmountedFlash() {
	if latest, before := s.hb.latestFrames(); s.hlSeen && s.laidOut && !s.laidAt.Equal(latest) && !s.laidAt.Equal(before) {
		s.highlight, s.hlSeen = "", false
	}
}

// adopt marks the list's model drawn in the frame at (the hub's eviction
// keeps the conversations drawn last; a model it dropped while the list
// was off screen is taken back here) and, when another list made the hub
// a model for the conversation meanwhile, switches to that one: the one
// core's events change. It runs before the frame's load and sync, in
// prepare or in layoutList's own, so a prepared frame (ThreadView.Prepare)
// syncs from the right model and every frame marks its model drawn.
// Revisions count per model, and a model's first load numbers its
// messages as the dropped one's did, so a row whose message changed while
// the model was out kept its copy (the same number) and skipped the
// updates whose numbers the old model had used: every row takes the new
// model's entries again, with the caches keyed on those numbers (its view
// read, its huddle, its hidden-sender check), and evictions count from
// the new model's own.
//
// A list showing a jump's window draws the window (its own model) and
// keeps the conversation's model the hub's too (chm, which takes the live
// messages it goes back to). A list the cache let go (evictList) and a
// view still draws is cached again, and takes its window back.
func (s *listState) adopt(at time.Time) {
	if _, ok := listCache[s.paneID]; !ok && s.paneID != "" {
		listCache[s.paneID] = s // unless a new list took its key
	}
	if s.win != nil {
		if s.hb.wantsWindowBack(s.win) {
			s.hb.takeBackWindow(s.win, s.reloadAt())
		}
		s.hb.markDrawn(s.win, at)
		if s.chm != nil {
			s.chm = s.hb.markDrawn(s.chm, at)
		}
		return
	}
	m := s.hb.markDrawn(s.m, at)
	if s.chm == s.m && s.chm != nil {
		s.chm = m
	}
	if m != s.m {
		s.useModel(m)
	}
}

// useModel has the list draw model m: the one the hub made for the
// conversation meanwhile (adopt), a jump's window (openWindow), or the
// conversation's model again (leaveWindow). Revisions count per model, a
// window's from its own first load as much as a new model's, so a row
// could hold the number m gave its message's copy and skip that copy:
// every row takes m's entries again, with the caches keyed on those
// numbers (its view read, its huddle, its hidden-sender check). And
// evictions count from m's own: switched to a window with the
// conversation's count of evictions, a list that had let its rows go for
// one let them go again the first time the sweep found it off screen (the
// window, never evicted, counted none), and lost its place and its jump.
func (s *listState) useModel(m *model) {
	s.m, s.snap.rev = m, 0
	for _, r := range s.rows {
		r.rev, r.viewRev, r.viewConv = 0, 0, 0
		// a viewed row's first ready read compares JS's view (turned): one
		// that changed while the list was off the model (an edit the
		// dropped model missed, a nickname) otherwise stayed drawn
		r.viewReady = false
		r.hmOK, r.hidChecked = false, false
	}
	m.mu.Lock()
	s.evictSeen = m.evictGen
	m.mu.Unlock()
}

// sync copies the model and rebuilds items if anything changed.
func (s *listState) sync(h Host, now time.Time) {
	if len(s.snap.typing) > 0 && now.Sub(s.lastTyping) > time.Second {
		s.snap.rev = 0 // re-read to expire typing indicators
	}
	// plugins: patched message views and injected messages (one atomic
	// load when no plugin patches messages)
	t := tautOf()
	views := t != nil && t.SlicePatched("messages")
	if views {
		if s.refreshInjected(t, now) {
			s.snap.rev = 0
		}
	} else if s.viewsOn {
		s.clearViews()
		if s.inj.keys != "" {
			s.inj = injState{}
			s.snap.rev = 0
		}
	}
	s.viewsOn = views
	// system messages follow system_messages_redesign_m1 (system.go): a new
	// group re-reads the snapshot and rebuilds the rows; an actor's name
	// arriving rebuilds the system rows
	sysOn := systemRedesign(h)
	hk := hiddenKey(h)
	switch {
	case !s.sysInit || sysOn != s.sysOn:
		if s.sysInit {
			for k := range s.rows {
				delete(s.rows, k)
			}
		}
		s.sysOn, s.sysInit = sysOn, true
		s.snap.rev = 0
	case s.sysMissing && s.hb.usersRev() != s.sysUsers, hk != s.sysHidden:
		// a name arrived, or someone was hidden or unhidden (RollupName
		// reads isUserHidden): write the system texts again
		s.sysHidden = hk
		for i := range s.snap.msgs {
			if m := &s.snap.msgs[i].m; m.System || isAutomatedSubtype(m.Subtype) {
				delete(s.rows, m.TS)
			}
		}
		s.snap.rev = 0
	}
	changed := s.m.snapshot(&s.snap)
	handed := false
	if s.win != nil && s.m == s.win && s.snap.merged && s.handOver() {
		changed, handed = s.m.snapshot(&s.snap), true
	}
	if changed {
		s.own = [2]string{}
		if n := len(s.snap.msgs); n > 0 {
			s.own = [2]string{s.snap.msgs[0].m.TS, s.snap.msgs[n-1].m.TS}
		}
	}
	// stickToBottom is reachedEnd (MessageList.componentDidUpdate): a
	// window short of the newest messages never keeps to its end, so a
	// newer page lands under the reader rather than pulling the list on.
	// componentDidUpdate sets it after the children's setKeys (AEjl:1163),
	// so the keys' change in this sync reads the flag from before
	// (wasEnd): the page that meets the newest messages lands under the
	// reader too, and only later changes stick.
	wasEnd := s.list.ScrollToEnd
	// a thread's slice short of the thread's end never keeps to its end
	// either (thread_load.go: setStickToBottom(reachedEnd)), so a newer
	// page lands under the reader; kept there, the list read page after
	// page on its own
	s.list.ScrollToEnd = !s.snap.newestMissing && (!s.thread || s.snap.th.reachedEnd)
	if changed {
		s.sysUsers, s.sysHidden = s.hb.usersRev(), hk
		s.sysMissing = s.adjustSystem(h, sysOn)
	}
	if changed && len(s.inj.msgs) > 0 {
		s.mergeInjected()
	}
	s.takeJump(now)
	if m := s.pend.take(); m != nil {
		s.openEdit(h, *m)
	}
	if changed {
		s.lastTyping = now
		if s.snap.refresh {
			s.hb.ensureLoaded(s.m)
		}
	}
	if s.snap.loaded && !s.opened {
		s.opened = true
		s.openLastRead = s.snap.lastRead
		// Mark as read > "Start me where I left off" (start_scroll_at_oldest
		// with mark_msgs_read_immediately): open on the first unread.
		if pf := uipref.Get(); pf.StartAtOldest && pf.MarkReadImmediately && !s.thread && s.pendingJump == "" && s.openLastRead != "" {
			for i := range s.snap.msgs {
				if ts := s.snap.msgs[i].m.TS; tsLess(s.openLastRead, ts) {
					if i > 0 || s.snap.hasMore {
						s.pendingJump = ts
					}
					break
				}
			}
		}
	}
	y, mo, d := now.Date() // not Format: this runs every frame
	dk := y*10000 + int(mo)*100 + d
	dense := uipref.Get().Dense
	if !changed && s.built == s.snap.rev && dk == s.dayKey && s.dense == dense && s.items != nil {
		s.placeJump(now)
		return
	}
	// AnchorLayout.setKeys (client-boot dxC+): as items come and go the
	// row on the anchor line keeps its place (findAnchor: the first row
	// ending below the line that's still there), and a list within
	// STICKY_EPSILON of its end stays at its end while it shows the newest
	// messages (stickToBottom). A window short of the newest never sticks,
	// and one handed over keeps its row even at its end (the window's end
	// isn't the newest messages'). Not while a jump is pending or placed:
	// it places the list itself.
	var anchors []laidRow
	stick, keep := false, false
	// a place written since the last layout (the page keys, the
	// scrollbar, ScrollPx) is where the list is: the last layout's rows
	// on the line are where it was, and keeping one there took the list
	// back. Its first item keeps the place, by key ("" with the place at
	// the end: drawn back from the new last item).
	written, writtenKey := false, ""
	if s.laidOut && s.positioned && s.pendingJump == "" && s.place.key == "" {
		p := s.list.Position
		switch {
		case !handed && wasEnd && (!p.BeforeEnd || s.gapNow() <= s.stickyPx):
			stick = true
		case !samePos(p, s.laidPos):
			written, keep = true, true
			if p.First < len(s.items) {
				writtenKey = s.items[max(0, p.First)].key
			}
		default:
			anchors = s.lineRows(s.linePx) // items still index the previous snapshot; the key is the ts
			keep = true
		}
	}
	s.build(h, now)
	s.rebuildKeyIdx()
	s.built = s.snap.rev
	s.dayKey = dk
	s.dense = dense
	anchored := false
	if written {
		if writtenKey == "" {
			s.list.Position.First, anchored = len(s.items), true
		} else if i := s.itemOfKey(writtenKey); i >= 0 {
			s.list.Position.First, anchored = i, true
		}
		s.list.Position.BeforeEnd = true
	}
	if !stick {
		// findAnchor: the first of the rows from the line down that's
		// still there keeps its place (a row deleted off the line: the one
		// after it, where the rows above would have moved down)
		for _, a := range anchors {
			if i := s.anchorItem(a.key); i >= 0 {
				s.list.Position, anchored = layout.Position{BeforeEnd: true, First: i, Offset: -a.y}, true
				break
			}
		}
	}
	if stick {
		s.list.Position = layout.Position{} // bracketScrollTop(Infinity)
	} else if !anchored && keep {
		// no row on the line came through: the list keeps its place, where
		// Gio would take one at its end to the new end (ScrollToEnd)
		s.list.Position.BeforeEnd = true
	}
	s.placeJump(now)
	// Confirmed sends keep their row (and its layout) under the real ts.
	for _, rn := range s.snap.renames {
		if r, ok := s.rows[rn[0]]; ok {
			delete(s.rows, rn[0])
			if _, taken := s.rows[rn[1]]; !taken {
				r.ts = rn[1]
				s.rows[rn[1]] = r
			}
		}
	}
	s.snap.renames = s.snap.renames[:0]
	// Refresh rows whose message changed; drop rows of deleted messages.
	live := make(map[string]bool, len(s.snap.msgs))
	for i := range s.snap.msgs {
		e := &s.snap.msgs[i]
		live[e.m.TS] = true
		if r, ok := s.rows[e.m.TS]; ok && r.rev != e.rev {
			r.set(e)
		}
	}
	for _, it := range s.items {
		if len(it.group) > 0 {
			live[it.key] = true
		}
	}
	for ts := range s.rows {
		if !live[ts] {
			delete(s.rows, ts)
		}
	}
}

// anchorItem is the item now showing the message keyed key, -1 if none.
// Items are matched by key (a join summary's is "g"+its first ts, while
// its idx is its last message); a summary that grew older joins in front
// is found by the message it started with.
func (s *listState) anchorItem(key string) int {
	if key == "" {
		return -1
	}
	for i, it := range s.items {
		if it.kind == itMsg && it.key == key {
			return i
		}
	}
	if ts, ok := strings.CutPrefix(key, "g"); ok {
		for i, it := range s.items {
			for _, k := range it.group {
				if s.snap.msgs[k].m.TS == ts {
					return i
				}
			}
		}
	}
	return -1
}

// groupable is Slack's isAdjacentMessage (boot-shared Ag38): b continues
// a's group. Senders compare as core.Adjacent does (same user; a user-less
// bot by bot_id, username and msg.icons); a thread parent (reply_count)
// on either side starts a new group, and so does an automated subtype
// (KC8h: joins, topics, pins…), whatever system_messages_redesign_m1
// draws it as. Pinning doesn't: Slack's Message hides the sender of any
// adjacent message (shouldHideSenderAndTimestamp) and draws the pinned
// label above its body.
func groupable(a, b *core.Message, ta, tb time.Time) bool {
	if a.System || b.System || isAutomatedSubtype(a.Subtype) || isAutomatedSubtype(b.Subtype) || a.Tombstone || b.Tombstone ||
		isHuddleMsg(a) || isHuddleMsg(b) || !core.Adjacent(a, b) { // a huddle message never groups (huddle_msg.go)
		return false
	}
	if b.Broadcast || a.ReplyCount > 0 || b.ReplyCount > 0 {
		return false
	}
	return tb.Sub(ta) < 5*time.Minute && tb.Sub(ta) >= 0
}

func (s *listState) build(h Host, now time.Time) {
	s.items = s.items[:0]
	msgs := s.snap.msgs
	if !s.thread {
		if s.snap.older || (s.snap.hasMore && len(msgs) > 0) {
			s.items = append(s.items, item{kind: itLoading, key: "loading"})
		} else if s.snap.loaded {
			s.items = append(s.items, item{kind: itIntro, key: "intro"})
		}
	}
	self := ""
	if h.Core() != nil {
		self = h.Core().Self().ID
	}
	var prev *core.Message
	var prevT time.Time
	unreadPlaced := false
	if s.thread {
		s.decideThreadRead()
		// getThreadRowsAndKeys' "loading-start-<first reply>" row over a
		// slice short of the thread's start (thread_load.go)
		if len(msgs) > 0 && s.snap.loaded && !s.snap.th.reachedStart {
			s.items = append(s.items, item{kind: itLoading, key: "loading-start-" + msgs[0].m.TS})
		}
	}
	for i := 0; i < len(msgs); i++ {
		m := &msgs[i].m
		t := tsTime(m.TS)
		header := prev == nil
		if s.thread {
			if i == 0 && m.TS == s.ts {
				s.items = append(s.items, item{kind: itMsg, idx: i, header: true, key: m.TS})
				// Slack shows no "N replies" divider until there is a
				// reply; when every reply is unread it carries the marker.
				if n := len(msgs) - 1; n > 0 {
					sep := s.threadSeparatorMark(self)
					unreadPlaced = sep != thrMarkNone
					s.items = append(s.items, item{kind: itReplies, idx: n, key: "replies", mark: sep})
				}
				prev, prevT = nil, t
				continue
			}
			// the "New" divider before the first unread reply (a thread
			// reply keeps its group across it: ThreadReply's prevTs)
			if !unreadPlaced && s.takesThreadMarker(m, self) {
				unreadPlaced = true
				s.items = append(s.items, item{kind: itUnread, key: "unread", mark: s.threadMarkKind()})
			}
		} else if prev == nil || !sameDay(t, prevT) {
			s.items = append(s.items, item{kind: itDay, day: t, key: "d" + m.TS})
			header = true
		}
		if !unreadPlaced && s.openLastRead != "" && tsLess(s.openLastRead, m.TS) && !s.thread {
			unreadPlaced = true
			// Only show the line when there is something read above it and
			// not everything after it is the viewer's own sends.
			othersAfter := false
			for k := i; k < len(msgs) && !othersAfter; k++ {
				othersAfter = msgs[k].m.User != self
			}
			if (i > 0 || s.snap.hasMore) && (othersAfter || s.openUnread) {
				s.items = append(s.items, item{kind: itUnread, key: "unread"})
				header = true
			}
		}
		if !header && prev != nil && !groupable(prev, m, prevT, t) {
			header = true
		}
		// Compact never groups thread replies (ThreadReply passes
		// adjacent only under "light"): each draws its sender and time.
		if s.thread && uipref.Get().Dense {
			header = true
		}
		// Consecutive joins (or leaves) collapse into one summary row.
		if jk := joinKind(m); jk != 0 && !s.thread {
			k := i + 1
			for k < len(msgs) && joinKind(&msgs[k].m) == jk && sameDay(tsTime(msgs[k].m.TS), t) &&
				(s.openLastRead == "" || tsLess(s.openLastRead, m.TS) == tsLess(s.openLastRead, msgs[k].m.TS)) {
				k++
			}
			if k-i >= 2 {
				g := make([]int, 0, k-i)
				for q := i; q < k; q++ {
					g = append(g, q)
				}
				s.items = append(s.items, item{kind: itMsg, idx: k - 1, header: true, key: "g" + m.TS, group: g})
				i = k - 1
				prev, prevT = &msgs[i].m, tsTime(msgs[i].m.TS)
				continue
			}
		}
		s.items = append(s.items, item{kind: itMsg, idx: i, header: header, key: m.TS})
		prev, prevT = m, t
	}
	// getListProps: the bottom spacer only at reachedEnd (a window short of
	// the newest messages has none), and the loading spinner at the end
	// while its newer page is out (isLoading, !isLoadingOlder, !reachedEnd)
	if !s.thread && s.snap.loaded && !s.snap.newestMissing {
		s.items = append(s.items, item{kind: itBottom, key: "bottomSpacer"})
	}
	if !s.thread && s.snap.newer {
		s.items = append(s.items, item{kind: itLoading, key: "loading-newer"})
	}
	if s.thread && len(msgs) > 0 && (!s.snap.loaded && s.snap.err == nil ||
		s.snap.loaded && !s.snap.th.reachedEnd && !s.snap.th.prevReachedEnd && !s.snap.th.endGap) {
		// getThreadRowsAndKeys' "loading-end" row: some messages are in
		// (the root, a live reply) before the replies are (thread.go), or
		// the slice stops short of the thread's end (thread_load.go). A
		// pane that was at the end when a slow connect made it stale keeps
		// the reply box (endGap) until its read past the old end lands,
		// even once a reply the socket brought makes the store's slice for
		// it no longer the last (prevReachedEnd false): Slack's input is
		// one of the list's rows, scrolled away with it, but Clack's box
		// is drawn under the list and may hold what's being typed
		s.items = append(s.items, item{kind: itLoading, key: "loading-end"})
	}
	if cap(s.heights) < len(s.items) {
		s.heights = make([]int, len(s.items), len(s.items)+64)
		s.pos = make([]int, len(s.items), len(s.items)+64)
	} else {
		s.heights = s.heights[:len(s.items)]
		s.pos = s.pos[:len(s.items)]
	}
}

// ContextBarShower is optional: whether a conversation's composer shows a
// context bar (Slack's getContextBarTypeForChannel), which shortens the
// list's bottom spacer.
type ContextBarShower interface {
	ContextBarShowing(ch string) bool
}

// bottomSpacerH is getBottomSpacerHeight: space.100 (16) with a context
// bar, else space.150 (24).
func (s *listState) bottomSpacerH(h Host) float32 {
	if cb, ok := h.(ContextBarShower); ok && cb.ContextBarShowing(s.ch) {
		return 16
	}
	return 24
}

// newestBottom is the window y of the newest message row's bottom edge,
// false when it wasn't on screen in the last frame.
func (s *listState) newestBottom() (int, bool) {
	n := s.newestItem()
	if n < s.visible[0] || n >= s.visible[1] || n >= len(s.pos) || s.heights[n] == 0 {
		return 0, false
	}
	return s.origin.Y + s.pos[n] + s.heights[n], true
}

// newestItem is the item index of the newest message (the bottom spacer
// comes after it), or -1.
func (s *listState) newestItem() int {
	for i := len(s.items) - 1; i >= 0; i-- {
		if s.items[i].kind == itMsg {
			return i
		}
	}
	return -1
}

// unreadIndex returns the item index of the "New" line, or -1.
func (s *listState) unreadIndex() int {
	for i, it := range s.items {
		if it.kind == itUnread {
			return i
		}
	}
	return -1
}

// layoutList is the common list drawing for channels and threads.
func (s *listState) layoutList(gtx layout.Context, h Host, origin image.Point) layout.Dimensions {
	th := h.Theme()
	c := colorsFor(th)
	st := styleFor(h)
	now := time.Now()
	lastHost = h
	if gtx.Now.IsZero() || !gtx.Now.Equal(s.preparedAt) { // else prepare did, this frame
		s.endUnmountedFlash()
		s.adopt(gtx.Now)
		s.ensure()
		s.sync(h, now)
	}
	s.placeSendJump(gtx, now)
	sweepRows(now)
	size := gtx.Constraints.Max
	fill(gtx.Ops, image.Rectangle{Max: size}, c.Bg)

	// view-level pointer tracking (ancestor of every row)
	area := clip.Rect(image.Rectangle{Max: size}).Push(gtx.Ops)
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: s, Kinds: pointer.Move | pointer.Drag | pointer.Press | pointer.Release | pointer.Enter | pointer.Leave | pointer.Cancel})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok {
			switch e.Kind {
			case pointer.Leave, pointer.Cancel:
				s.pointerIn = false
			default:
				s.pointerIn = true
				s.pointer = e.Position
			}
			if ctxclick.Press(e) {
				s.ctx = ctxPress{pos: e.Position, ok: true}
			}
			s.selPointer(gtx, h, e, size)
		}
	}
	s.selKeys(gtx, h)
	s.kbKeys(gtx, h)
	event.Op(gtx.Ops, s)

	if root := s.replyRoot(); root != "" {
		// the thread was opened with a reply's ts: never drawn as its
		// root, the pane is handed on to the reply's thread and stays
		// loading meanwhile (ReplyRootRedirector)
		if !s.rootHandedOn {
			s.rootHandedOn = true
			if r, ok := h.(ReplyRootRedirector); ok {
				r.ReplyAsThreadRoot(s.ch, s.ts, root)
			}
		}
		s.flexpaneLoading(gtx, h, c, size)
		area.Pop()
		return layout.Dimensions{Size: size}
	}

	if !s.snap.loaded && len(s.snap.msgs) == 0 {
		if s.thread && s.threadPending(gtx, h, c, size) { // FlexpaneLoading, ThreadsError (thread.go)
			area.Pop()
			return layout.Dimensions{Size: size}
		}
		if s.snap.err != nil && !s.snap.loading {
			s.layoutLoadError(gtx, h, c, size)
		} else {
			y := size.Y
			for i := 0; y > 0; i++ {
				gg := gtx
				gg.Constraints = layout.Exact(image.Pt(size.X, dp(gtx, 60)))
				y -= dp(gtx, 60)
				at(gtx, 0, y, func(gtx layout.Context) layout.Dimensions { return skeleton(gg, c, i) })
			}
		}
		area.Pop()
		return layout.Dimensions{Size: size}
	}

	if s.thread {
		s.pendAt = time.Time{}
		s.checkThreadUnreads(gtx, h, now) // thread_unread.go
		s.placeThreadJump(now)            // after the thread's last read is known
	}
	// first positioning: a conversation opens at its newest messages,
	// however many are unread. determineTimestamps adds no startTs with
	// Mark as read's default prefs (client-boot eCxs, dYJr gives one only
	// with start_scroll_at_oldest and mark_msgs_read_immediately), so the
	// pane anchors at the end (dxC+ getOffsetForKey(null)); the unread
	// banner jumps up to the "New" line. A thread opens at
	// getThreadStartPosition's message.
	if !s.positioned && s.snap.loaded {
		s.positioned = true
		if s.thread {
			// getThreadStartPosition: the first unread reply, the reply
			// last read to, or the root (thread_unread.go)
			s.list.Position = s.threadStartPosition(gtx, "")
		} else {
			s.toEnd()
		}
	}

	// hover from last frame's rows: the message under the pointer, or the
	// one whose toolbar it is on (Slack's toolbar is inside its row, so
	// the row stays hovered over it), found by key in this frame's items
	s.hoverIdx = -1
	if s.pointerIn {
		p := image.Pt(int(s.pointer.X), int(s.pointer.Y))
		key := ""
		if s.tbKey != "" && p.In(s.tbRect) {
			key = s.tbKey
		} else {
			for _, r := range s.laid {
				if p.Y >= r.y && p.Y < r.y+r.h {
					key = r.key
					break
				}
			}
		}
		if i, ok := s.sel.keyIdx[key]; ok && key != "" {
			s.hoverIdx = i
		}
	}
	if i, ok := s.tbHold.row(s); ok {
		s.hoverIdx = i // a popover the row's toolbar opened holds its hover
	}
	if k := s.kbIndex(); k >= 0 {
		s.hoverIdx = k // the keyboard-selected message draws as hovered
	}

	// the wheel over the message actions (toolbar_scroll.go)
	actD := s.actionsScroll(gtx)
	line := s.lineOff(gtx)
	if s.place.key != "" {
		// a jump's placement: a programmatic scroll, which ends the
		// reader's running one (Chromium cancels a wheel animation or a
		// fling on a scrollTop write)
		if i := s.itemOfKey(s.place.key); i >= 0 {
			s.list.Position = layout.Position{BeforeEnd: true, First: i, Offset: -gtx.Dp(s.place.off)}
			s.list.StopScroll()
			s.actScroll.Stop()
		}
		s.place = jumpPlace{}
	}

	// setContainerHeight (dxC+): the list's height changed (the composer
	// grew a line, the window resized) while it was within STICKY_EPSILON
	// of the newest messages' end: it stays at the end; else it keeps its
	// top (Gio keeps First and Offset)
	if s.laidOut && size.Y != s.laidH && s.list.ScrollToEnd && s.list.Position.BeforeEnd && s.endGap <= s.stickyPx &&
		samePos(s.list.Position, s.laidPos) {
		s.list.Position = layout.Position{}
	}
	// a page for PageUp and PageDown (getPageHeight)
	s.pageH = size.Y - gtx.Dp(21)

	// AnchorLayout.setHeight (dxC+): a row above the anchor line that
	// grows or shrinks moves the list by as much, so the row on the line
	// stays where it was (the reader's own scrolling in this frame aside:
	// ScrollDelta); a list within 2px of its end (STICKY_EPSILON_SETHEIGHT)
	// that shows the newest messages stays at its end instead. Only while
	// nothing else moved the list since the last layout (a placement, the
	// keys, the scrollbar: they say where it is).
	akey, ay, nearEnd := "", 0, false
	if s.laidOut && s.built == s.laidBuilt && samePos(s.list.Position, s.laidPos) && (s.list.Position.BeforeEnd || !s.list.ScrollToEnd) {
		akey, ay = s.lineRow(line)
		nearEnd = s.list.ScrollToEnd && s.endGap <= gtx.Dp(2)
	}
	for i := range s.heights {
		s.heights[i] = 0
	}
	if len(s.tailH) > 4*tailItems || s.tailH == nil {
		// the last items' heights only: keys of items no longer last go
		keep := make(map[string]int, tailItems)
		for i := max(0, len(s.items)-tailItems); i < len(s.items); i++ {
			if h, ok := s.tailH[s.items[i].key]; ok {
				keep[s.items[i].key] = h
			}
		}
		s.tailH = keep
	}
	selfID := ""
	if h.Core() != nil {
		selfID = h.Core().Self().ID
	}
	urev := s.hb.usersRev()
	chanFrame = urev // channel names the rows may show this frame (channels.go)
	s.rc = rowCtx{h: h, th: th, st: st, c: c, now: now, thread: s.thread, dense: uipref.Get().Dense, persist: PersistActions(), list: s, selfID: selfID, usersRev: urev + 1,
		origin:       func(p image.Point) image.Point { return s.anchor(h, origin, p) },
		onReact:      func(r *row, name string, add bool) { s.react(h, r, name, add, selfID) },
		onReactGroup: func(r *row, base string) { s.reactGroup(h, r, base, selfID) },
		onRetry:      func(r *row) { s.retry(h, r) },
		onDiscard:    func(r *row) { s.discard(h, r) },
	}
	rc := &s.rc
	s.activeTS = activeThreadIn(h, s.ch, s.thread)
	s.origin, s.size = origin, size
	s.tip = ""
	s.stOn = false
	ls := listClass.Enter(gtx, 0, 0)
	defer ls.Leave()
	lay := func() op.CallOp {
		m := op.Record(gtx.Ops)
		s.list.Layout(gtx, len(s.items), func(gtx layout.Context, i int) layout.Dimensions {
			d := s.layoutItem(gtx, h, rc, i)
			s.heights[i] = d.Size.Y
			if i >= len(s.items)-tailItems {
				s.tailH[s.items[i].key] = d.Size.Y
			}
			return d
		})
		return m.Stop()
	}
	call := lay()
	// The row on the line moved by more than the reader scrolled: a row
	// above it changed height (a channel name or a picture arriving, a
	// plugin's tree). Keep it where it was, in this frame: lay out again
	// with the offset corrected and draw only that.
	if akey != "" {
		d := s.list.ScrollDelta()
		if y, ok := s.yOfKey(akey, size.Y); ok && y != ay-d {
			for i := range s.heights {
				s.heights[i] = 0
			}
			if nearEnd && d == 0 {
				s.list.Position = layout.Position{} // within 2px of the end: it stays at the end
			} else {
				s.list.Position.Offset += y - (ay - d)
			}
			call = lay()
		} else if p := s.list.Position; nearEnd && d == 0 && p.First+p.Count >= len(s.items) && -p.OffsetLast > gtx.Dp(2) {
			// setHeight sticks before any row's compensation
			// (shouldStickToBottom(n, 2)): a row below the line that grew
			// left the list short of its end
			for i := range s.heights {
				s.heights[i] = 0
			}
			s.list.Position = layout.Position{}
			call = lay()
		}
	}
	call.Add(gtx.Ops)
	s.endGap = s.gapToEnd(s.list.Position)
	s.laidOut, s.laidPos, s.laidBuilt, s.laidAt, s.laidH = true, s.list.Position, s.built, gtx.Now, size.Y
	if s.thread && s.snap.loaded {
		s.thrOpening = false // the pane shows its thread: a jump now is a re-highlight
	}
	defer s.pageSelect() // after the positions below (kbnav.go)
	s.actionsScrollEnd(actD)

	// positions of visible items
	p := s.list.Position
	y := -p.Offset
	s.visible = [2]int{p.First, p.First}
	s.laid = s.laid[:0]
	for i := p.First; i < len(s.items) && y < size.Y; i++ {
		s.pos[i] = y
		if s.items[i].kind == itMsg && s.heights[i] > 0 {
			s.laid = append(s.laid, laidRow{key: s.items[i].key, y: y, h: s.heights[i]})
		}
		y += s.heights[i]
		s.visible[1] = i + 1
	}
	// MessageBackground's componentWillUnmount: the highlighted row leaving
	// the drawn rows (scrolled out of the virtual list) ends its flash
	if s.highlight != "" {
		drawn := false
		for _, r := range s.laid {
			if r.key == s.highlight {
				drawn = true
				break
			}
		}
		switch {
		case drawn:
			s.hlSeen = true
		case s.hlSeen:
			s.highlight, s.hlSeen = "", false
		}
	}
	// contentH is the total item height when every item was laid out (the
	// ui root puts a short thread's reply box under it), else 0.
	s.contentH = 0
	if p.First == 0 && s.visible[1] == len(s.items) {
		s.contentH = y + p.Offset
	}
	if !s.unreadSeen && s.opened {
		ui := s.unreadIndex()
		s.unreadSeen = ui < 0 || (ui >= s.visible[0] && ui < s.visible[1])
	}

	s.updateUnreadThreads(h)
	s.contextMenu(h, origin)
	s.overlays(gtx, h, rc, size)
	s.layoutScrollbar(gtx, c, size, now)
	area.Pop()
	s.reportMedia() // the video tiles' share on screen (file_rects.go)

	// jumping to a message what the list shows doesn't reach: Slack's pane
	// at startTs shows the slice holding it (windows.go): older than its
	// start (or past a window's end), the window around it, once
	// (openWindow), the conversation's newest messages when they hold it;
	// sync() places it, or the first message after it, once they cover it
	// (placeJump). One whose window gave way has nowhere to land.
	if s.pendingJump != "" && !s.thread && s.snap.loaded && !s.snap.older && !s.snap.loading && s.snap.around == "" {
		first, last, ok := s.ends()
		older := ok && s.snap.hasMore && tsLess(s.pendingJump, first)
		newer := s.win != nil && ok && tsLess(last, s.pendingJump)
		switch {
		case newer && s.newestHold(s.pendingJump):
			s.leaveWindow() // the conversation's newest messages hold it
		case (older || newer) && s.aroundFor != s.pendingJump:
			s.aroundFor = s.pendingJump
			s.openWindow(s.pendingJump)
		case (older || newer) && s.win == nil && s.built == s.snap.rev:
			// its window gave way (it failed, or came back empty) and the
			// newest messages don't reach it: nothing to land on
			s.pendingJump = ""
		}
	}
	if s.pendingJump != "" && s.thread && s.snap.loaded && !s.snap.loading && s.built == s.snap.rev {
		// the slice the jump moved the pane to is in (ThreadView.JumpTo,
		// thread_load.go): sync() placed the jump if the slice holds it,
		// so it isn't coming
		s.pendingJump = ""
	}
	s.pendingDateJump(now) // a day pill's "Jump to…" (banners.go)
	if s.thread {
		s.threadPaging(gtx, h, size, now) // pages before and after the slice (thread_load.go)
	}
	// infinite scroll up
	if !s.thread && p.First <= 2 && s.snap.hasMore && !s.snap.older && s.snap.loaded && now.Sub(s.lastOlder) > time.Second {
		if s.hb.loadOlder(s.m) {
			s.lastOlder = now
		}
	}
	// a window short of the newest message: its end in view loads the
	// next page (requestNewer)
	if !s.thread && s.snap.newestMissing && !s.snap.newer && s.snap.around == "" && s.snap.loaded &&
		s.visible[1] >= len(s.items)-2 && now.Sub(s.lastNewer) > time.Second {
		s.lastNewer = now
		s.hb.loadNewer(s.m)
	}
	// Mark as read: mark_msgs_read_immediately marks as soon as the
	// conversation shows; Slack's default leaves unseen messages unread,
	// so the newest must be on screen and the "New" line must have been.
	pf := uipref.Get()
	seen := pf.MarkReadImmediately || (!p.BeforeEnd && s.unreadSeen)
	// a window short of the newest message never marks read: its last
	// message isn't the conversation's
	if !s.thread && !s.noMark && s.snap.loaded && !s.snap.newestMissing && seen && len(s.snap.msgs) > 0 {
		latest := s.snap.msgs[len(s.snap.msgs)-1].m.TS
		app := h.Core()
		if latest != s.markedTS && app != nil && tsLess(app.Count(s.ch).LastRead, latest) && now.Sub(s.lastMark) > 1500*time.Millisecond {
			s.markedTS, s.lastMark = latest, now
			{
				ch := s.ch
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
					defer cancel()
					_ = markRead(ctx, app, ch, latest) // taut markLastRead {id, ts}
				}()
			}
		}
	}
	return layout.Dimensions{Size: size}
}

// laidRow is a message row as a layout put it on screen: its item's key,
// its top and height in the list.
type laidRow struct {
	key  string
	y, h int
}

// samePos reports two list positions at the same place.
func samePos(a, b layout.Position) bool {
	return a.First == b.First && a.Offset == b.Offset && a.BeforeEnd == b.BeforeEnd
}

// yOfKey is item key's y in this frame's layout (from Position and the
// heights just measured), if it is on screen.
func (s *listState) yOfKey(key string, viewH int) (int, bool) {
	p := s.list.Position
	y := -p.Offset
	for i := p.First; i < len(s.items) && y < viewH; i++ {
		if s.items[i].key == key {
			return y, true
		}
		y += s.heights[i]
	}
	return 0, false
}

func (s *listState) anchor(h Host, origin, p image.Point) image.Point {
	if ph, ok := h.(PointerHost); ok {
		return ph.PointerPos()
	}
	return origin.Add(image.Pt(int(s.pointer.X), int(s.pointer.Y)))
}

func (s *listState) react(h Host, r *row, name string, add bool, self string) {
	app := h.Core()
	if app == nil {
		return
	}
	key := reactionKey(&r.src)
	if t := tautOf(); t == nil || (!t.ThunkPatched("changeReactionFromUserAction") && !t.ThunkPatched("submitAddOrRemoveReaction")) {
		s.reactNow(h, app, key, name, add, self)
		return
	}
	// Slack: the reaction button dispatches changeReactionFromUserAction,
	// whose native submits submitAddOrRemoveReaction (plugins may wrap
	// either; with none this is just core.React). The click's: its JS
	// call is queued now, on the UI goroutine, on the user's lane
	// (UserAction, DispatchThen), so a plugin click after it can't go
	// ahead (OwlMode reads its mode in the reaction's patch).
	ctx, cancel := context.WithTimeout(taut.UserAction(context.Background()), 20*time.Second)
	taut.DispatchThen(ctx, tautOf(), "changeReactionFromUserAction", rxnUserArgs{Key: key, Name: name, Add: &add},
		func(ctx context.Context, a rxnUserArgs) (any, error) {
			on := !s.hasOwnReaction(a.Key, a.Name)
			if a.Add != nil {
				on = *a.Add
			}
			if on && !a.SkipFrecencyRecord {
				// recordEmojiFrecencyEvent {emojiName, text: ""}
				app.RecordEmojiFrecency(a.Name, "")
			}
			return s.submitReaction(ctx, h, app, a.Key, a.Name, on, self)
		}, func(_ any, err error) {
			cancel()
			if err != nil {
				h.Toast("Couldn't update reaction")
			}
		})
}

// reactNow is a pill's click with neither reaction thunk wrapped:
// changeReactionFromUserAction's frecency record and
// submitAddOrRemoveReaction's optimistic update happen here, on the UI
// goroutine, so the pill changes in the next frame while
// reactions.add/remove is out (core.ReactBegin; the views showing the
// message get the update now, ApplyEvent). The call runs after; a failure
// is rolled back by core and toasted.
func (s *listState) reactNow(h Host, app *core.App, key, name string, add bool, self string) {
	ch, ts, ok := parseRxnKey(key)
	if !ok {
		return
	}
	on := add
	if on {
		app.RecordEmojiFrecency(name, "") // recordEmojiFrecencyEvent {emojiName, text: ""}
	}
	ev, call := app.ReactBegin(ch, ts, name, on)
	local := false
	if ev != nil {
		ApplyEvent(app, *ev)
	} else if s.hasOwnReaction(key, name) != on {
		// a message core doesn't hold (submitReaction's own update)
		s.mutateReaction(h, ts, name, on, self)
		local = true
	}
	h.Invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := call(ctx); err != nil {
			if local {
				s.mutateReaction(h, ts, name, !on, self)
			}
			h.Toast("Couldn't update reaction")
			h.Invalidate()
		}
	}()
}

// submitReaction is submitAddOrRemoveReaction {reactionKey, name, add}:
// an optimistic local update, then core.React (reverted if it fails).
func (s *listState) submitReaction(ctx context.Context, h Host, app *core.App, key, name string, add bool, self string) (any, error) {
	return taut.Dispatch(ctx, tautOf(), "submitAddOrRemoveReaction", rxnSubmitArgs{ReactionKey: key, Name: name, Add: add},
		func(ctx context.Context, a rxnSubmitArgs) (any, error) {
			ch, ts, ok := parseRxnKey(a.ReactionKey)
			if !ok {
				return nil, fmt.Errorf("messages: bad reaction key %q", a.ReactionKey)
			}
			s.mutateReaction(h, ts, a.Name, a.Add, self)
			h.Invalidate()
			if err := app.React(ctx, ch, ts, a.Name, a.Add); err != nil {
				s.mutateReaction(h, ts, a.Name, !a.Add, self)
				h.Invalidate()
				return nil, err
			}
			return nil, nil
		})
}

// hasOwnReaction reports whether the viewer reacted name on the message
// with Slack reaction key key (any goroutine).
func (s *listState) hasOwnReaction(key, name string) bool {
	_, ts, ok := parseRxnKey(key)
	if !ok {
		return false
	}
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	if i := s.m.find(ts); i >= 0 {
		for _, re := range s.m.msgs[i].m.Reactions {
			if re.Name == name {
				return re.Self
			}
		}
	}
	return false
}

// mutateReaction adds or removes the viewer's reaction locally (any goroutine).
func (s *listState) mutateReaction(h Host, ts, name string, add bool, self string) {
	s.m.mutate(ts, func(m *core.Message) {
		for i := range m.Reactions {
			re := &m.Reactions[i]
			if re.Name != name {
				continue
			}
			if add && !re.Self {
				re.Self, re.Count = true, re.Count+1
				re.Users = append(re.Users, self)
			} else if !add && re.Self {
				re.Self, re.Count = false, re.Count-1
				if re.Count <= 0 {
					m.Reactions = append(m.Reactions[:i], m.Reactions[i+1:]...)
				}
			}
			return
		}
		if add {
			url := ""
			if h.Core() != nil {
				url, _, _ = h.Core().Emoji(name)
			}
			m.Reactions = append(m.Reactions, core.Reaction{Name: name, Count: 1, Users: []string{self}, URL: url, Self: true})
		}
	})
}

// layoutItem draws item i as a .c-virtual_list__item keyed like Slack's
// data-item-key.
func (s *listState) layoutItem(gtx layout.Context, h Host, rc *rowCtx, i int) layout.Dimensions {
	st := itemClass.EnterAttr(gtx, 0, 0, "data-item-key", s.items[i].key)
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{}
	}
	return s.layoutItemContent(gtx, h, rc, i)
}

func (s *listState) layoutItemContent(gtx layout.Context, h Host, rc *rowCtx, i int) layout.Dimensions {
	it := s.items[i]
	c := rc.c
	w := gtx.Constraints.Max.X
	switch it.kind {
	case itDay:
		return s.dayDivider(gtx, h, c, i, it.day, rc.now, w)
	case itUnread:
		if s.thread {
			return s.threadDivider(gtx, h, c, it, w, rc.now)
		}
		// Slack's divider is 0 tall: a 1px line with "New" centered on it at
		// the right end (padding 0 4, margin-right 15).
		hh := dp(gtx, 1)
		s := textStyle{size: 13, weight: theme.Bold, col: c.NewLine, lineH: 19.5}
		newTxt := messagesNS.T("New") // UnreadDivider's (VxYf, the messages I18n)
		sz := measure(gtx, h.Theme(), s, newTxt)
		lx := w - dp(gtx, 15) - dp(gtx, 4) - sz.X
		fill(gtx.Ops, image.Rect(0, 0, lx-dp(gtx, 4), hh), c.NewLine)
		at(gtx, lx, -dp(gtx, 10), func(gtx layout.Context) layout.Dimensions { return label(gtx, h.Theme(), s, newTxt) })
		return layout.Dimensions{Size: image.Pt(w, hh)}
	case itLoading:
		if s.thread {
			return s.loadingRepliesRow(gtx, h, c, w) // thread.go
		}
		gg := gtx
		gg.Constraints = layout.Exact(image.Pt(w, dp(gtx, 60)))
		return skeleton(gg, c, 0)
	case itIntro:
		return s.intro(gtx, h, c, w)
	case itReplies:
		return s.threadSeparator(gtx, h, c, it, w, rc.now)
	case itBottom:
		return layout.Dimensions{Size: image.Pt(w, dp(gtx, s.bottomSpacerH(h)))}
	}
	e := &s.snap.msgs[it.idx]
	if len(it.group) > 0 {
		ge := s.groupEntry(h, it.group)
		e = &ge
	}
	r, ok := s.rows[it.key]
	if !ok {
		r = newListRow(e, s.thread)
		s.rows[it.key] = r
	} else if len(it.group) > 0 && r.rev != e.rev {
		r.set(e)
	}
	if s.viewsOn && len(it.group) == 0 {
		s.applyView(tautOf(), r, rc.now)
		if !r.heldTil.IsZero() {
			gtx.Execute(op.InvalidateCmd{At: r.heldTil}) // the hold's end draws core's copy if JS hasn't answered
		}
	}
	r.rv.External = true
	r.drawn = rc.now
	sa, sb := s.rowSelection(i, r)
	r.rv.SetSelection(sa, sb)
	rc.header = it.header
	rc.hovered = i == s.hoverIdx && r.edit == nil
	rc.winOrigin = s.origin.Add(image.Pt(0, s.pos[i]))
	rc.parent = s.thread && e.m.TS == s.ts
	rc.activeThread = s.activeTS != "" && e.m.TS == s.activeTS
	// MessageWrapper's isSenderHidden and the message's isUnhidden
	// (hidden_msg.go): system rows too, and a joins summary by its first
	// message's sender (the rollup is that message's row)
	rc.hiddenUser, rc.unhidden = hiddenSender(h, &e.m, s.ch)
	rc.highlight = 0
	if s.highlight == e.m.TS {
		if el := rc.now.Sub(s.highlightAt); el < highlightFor {
			rc.highlight = highlightAlpha(el)
			gtx.Execute(op.InvalidateCmd{})
		}
	}
	rc.findFlash = 0
	if s.findFlash == e.m.TS {
		if el := rc.now.Sub(s.findFlashAt); el < findFlashFor {
			rc.findFlash = findFlashAlpha(el)
			gtx.Execute(op.InvalidateCmd{})
		}
	}
	// the query in a message's rich text: Slack's Blocks pass it only to
	// messages with blocks (BaseRichText)
	fw, ff := "", false
	if s.findWords != "" {
		ff = e.m.TS == s.findTS
		if len(e.m.Blocks) > 0 {
			fw = s.findWords
		}
	}
	if r.find != fw {
		r.find, r.findFocused = fw, ff
		r.setDoc()
	} else if r.findFocused != ff {
		r.findFocused = ff
		r.rv.SetOptions(r.rvOpts())
	}
	gtx.Constraints.Min.X = w
	r.statusDrawn = false
	d := r.layout(gtx, rc)
	// the sender status's tooltip: the icon's box is where the pointer
	// is, back by its place in the icon
	if rc.hovered && r.statusDrawn && r.statusHov {
		p := image.Pt(int(s.pointer.X-r.statusPt.X), int(s.pointer.Y-r.statusPt.Y))
		s.stOn, s.stItem, s.stRect = true, i, image.Rectangle{Min: p, Max: p.Add(r.statusSize)}
		if s.items[i].key != s.stKey {
			s.stKey, s.stAt = s.items[i].key, rc.now
		}
		s.stTip = r.statusTipFor(rc.now)
	}
	// hover tooltip for reaction pills
	if rc.hovered {
		for k := range r.pills {
			if k < len(r.reactRects) && k < len(r.groups) && r.pills[k].btn.Hovered() {
				s.tipRect = r.reactRects[k] // row-local; positioned in overlays
				if s.tipRect.Empty() {
					// a plugin's bar child holding several pills: the
					// pill is where the pointer is, back by its place on it
					pl := r.pills[k]
					p := image.Pt(int(s.pointer.X-pl.pt.X), int(s.pointer.Y-pl.pt.Y)-s.pos[i])
					s.tipRect = image.Rectangle{Min: p, Max: p.Add(pl.ptSize)}
				}
				s.tipItem = i
				if key := s.items[i].key + "\x00" + r.groups[k].name; key != s.tipKey {
					s.tipKey, s.tipAt = key, rc.now
				}
				// ReactionTip mounts once the tooltip shows, and its
				// TooltipMemberName reads the reactors (getMemberById,
				// which asks for a missing one) only then: before the
				// delay the tip is pending, and nobody is looked up
				if rc.now.Before(s.tipAt.Add(reactionTipDelay)) {
					s.tipG, s.tip = groupTip{}, tipPending
				} else {
					s.tipG = r.groupTip(h, k, rc.selfID) // the pill's group (reaction_groups.go)
					s.tip = s.tipG.text()
				}
			}
		}
	}
	return d
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// dayPill draws a day divider's pill (DayDividerJumper's
// c-message_list__day_divider__label__pill, a menu trigger: "Jump to
// date") centred on cx at y; it returns the pill's rect and whether it
// was clicked.
func (s *listState) dayPill(gtx layout.Context, h Host, c *colors, btn *widget.Clickable, label string, cx, y int) (image.Rectangle, bool) {
	th := h.Theme()
	ts := textStyle{size: 13, weight: theme.Bold, col: c.Text, lineH: 18}
	sz := measure(gtx, th, ts, label)
	ph := dp(gtx, mt.dayPillH)
	pw := dp(gtx, 16) + sz.X + dp(gtx, 4) + dp(gtx, 13) + dp(gtx, 8)
	r := image.Rect(cx-pw/2, y, cx-pw/2+pw, y+ph)
	clicked := btn.Clicked(gtx)
	shadowCard(gtx.Ops, r, ph/2, dp(gtx, 1), c.Bg, c.PillBorder, c.Shadow)
	at(gtx, r.Min.X, r.Min.Y, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(r.Size())
		return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			at(gtx, dp(gtx, 16), (ph-dp(gtx, 18))/2, func(gtx layout.Context) layout.Dimensions {
				return labelText(gtx, th, ts, label)
			})
			at(gtx, dp(gtx, 16)+sz.X+dp(gtx, 4), (ph-dp(gtx, 13))/2, func(gtx layout.Context) layout.Dimensions {
				return icons.Get("chevron-down")(gtx, unit.Dp(13), c.Text)
			})
			return layout.Dimensions{Size: r.Size()}
		})
	})
	return r, clicked
}

// labelText avoids the shadowed name in dayPill.
func labelText(gtx layout.Context, th *theme.Theme, s textStyle, txt string) layout.Dimensions {
	return label(gtx, th, s, txt)
}

func (s *listState) dayDivider(gtx layout.Context, h Host, c *colors, i int, day, now time.Time, w int) layout.Dimensions {
	hh := dp(gtx, mt.dayH)
	ly := dp(gtx, 19)
	fill(gtx.Ops, image.Rect(0, ly, w, ly+dp(gtx, 1)), c.DayLine)
	if s.dayBtns == nil {
		s.dayBtns = map[string]*widget.Clickable{}
	}
	key := day.Format("2006-01-02")
	btn := s.dayBtns[key]
	if btn == nil {
		btn = &widget.Clickable{}
		s.dayBtns[key] = btn
	}
	if pr, clicked := s.dayPill(gtx, h, c, btn, dayLabel(day, now), w/2, (hh-dp(gtx, mt.dayPillH))/2); clicked {
		s.dayReq = &dayMenuReq{day: day, item: i, r: pr}
	}
	return layout.Dimensions{Size: image.Pt(w, hh)}
}

func (s *listState) intro(gtx layout.Context, h Host, c *colors, w int) layout.Dimensions {
	th := h.Theme()
	name := s.ch
	kind := ""
	agent := "" // a code channel's agent (CodeChannelForewordDescription)
	if app := h.Core(); app != nil {
		if cv, ok := app.Conv(s.ch); ok {
			name, kind = cv.Name, cv.Kind
			if cv.Code != nil {
				agent = cv.AgentName
			}
			if kind == "im" {
				if u, ok := app.User(cv.User); ok {
					name = u.Name
				}
			}
		}
	}
	title := "#" + name
	sub := "This is the very beginning of the #" + name + " channel."
	if kind == "im" || kind == "mpim" {
		title = name
		sub = "This conversation is just between you and " + name + "."
	}
	px := dp(gtx, mt.padX)
	y := dp(gtx, 24)
	d := at(gtx, px, y, func(gtx layout.Context) layout.Dimensions {
		return label(gtx, th, textStyle{size: 28, weight: theme.Black, col: c.Text, lineH: 36}, title)
	})
	y += d.Size.Y + dp(gtx, 8)
	if agent != "" {
		// a code channel: Slack's two lines instead of the channel intro and
		// its buttons; the disclaimer is 12px content-sec, 4px below.
		sub = "This is a code channel — @" + agent + " can see and respond to every message and file."
	}
	d = at(gtx, px, y, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Max.X = w - 2*px
		return label(gtx, th, textStyle{size: 15, col: c.Text2, lineH: 22}, sub)
	})
	y += d.Size.Y
	if agent != "" {
		y += dp(gtx, 4)
		d = at(gtx, px, y, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Max.X = w - 2*px
			return label(gtx, th, textStyle{size: 12, col: c.Text3, lineH: 18},
				"Content shared here is also processed by "+agent+" according to its terms. "+agent+" uses AI and can make mistakes.")
		})
		y += d.Size.Y
	}
	y += dp(gtx, 16)
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// overlays draws the hover toolbar, the sticky day pill, tooltips and the
// jump-to-recent button on top of the list.
func (s *listState) overlays(gtx layout.Context, h Host, rc *rowCtx, size image.Point) {
	c := rc.c
	th := h.Theme()
	defer clip.Rect(image.Rectangle{Max: size}).Push(gtx.Ops).Pop()

	// sticky day pill
	if !s.thread {
		top := s.visible[0]
		d := -1
		for i := top; i >= 0 && i < len(s.items); i-- {
			if s.items[i].kind == itDay {
				d = i
				break
			}
		}
		if d >= 0 {
			pillY := dp(gtx, 6) // Slack: 6px under the header/tabs (y=133 at 1440×900)
			ownY := s.pos[d] + (dp(gtx, mt.dayH)-dp(gtx, mt.dayPillH))/2
			if d < s.visible[0] || ownY < pillY {
				// pushed up by the next day's divider
				for i := d + 1; i < s.visible[1]; i++ {
					if s.items[i].kind == itDay {
						if ny := s.pos[i] + (dp(gtx, mt.dayH)-dp(gtx, mt.dayPillH))/2 - dp(gtx, mt.dayPillH) - dp(gtx, 6); ny < pillY {
							pillY = ny
						}
						break
					}
				}
				day := s.items[d].day
				if pr, clicked := s.dayPill(gtx, h, c, &s.stickyDay, dayLabel(day, rc.now), size.X/2, pillY); clicked {
					s.dayReq = &dayMenuReq{day: day, item: -1, r: pr}
				}
			}
		}
	}

	// hover toolbar (the simplified layout's persistent menu instead), for
	// a row this layout put on screen: one that left it took its toolbar
	// along (Slack's is in the row)
	s.tbFor, s.tbKey = -1, ""
	if rc.persist {
		s.persistentActions(gtx, h, rc, size)
	} else if s.hoverIdx >= s.visible[0] && s.hoverIdx < s.visible[1] && s.hoverIdx < len(s.items) && s.items[s.hoverIdx].kind == itMsg {
		if r := s.rows[s.items[s.hoverIdx].key]; r != nil && !s.sel.dragging && r.edit == nil && len(s.items[s.hoverIdx].group) == 0 && !r.m.Pending && !r.m.Failed && !r.m.Tombstone {
			s.toolbar(gtx, h, rc, r, s.hoverIdx, size)
		}
	}
	if s.tbFor < 0 { // no toolbar: the next one mounts at rest
		s.tilt = toolbarTilt{}
	}

	// reaction tooltip
	if s.tip == "" {
		s.tipKey = ""
	} else if s.tipItem >= s.visible[0] && s.tipItem < s.visible[1] {
		if wait := s.tipAt.Add(reactionTipDelay); rc.now.Before(wait) {
			gtx.Execute(op.InvalidateCmd{At: wait})
		} else {
			r := s.tipRect.Add(image.Pt(0, s.pos[s.tipItem]))
			// deferred: the tip may reach past the pane, over the sidebar
			m := op.Record(gtx.Ops)
			reactionGroupTooltip(gtx, h, th, c, &s.tipG, r, theme.WindowBounds(s.origin))
			op.Defer(gtx.Ops, m.Stop())
		}
	}

	// the sender status's tooltip (BaseTooltip's 150 ms)
	if !s.stOn {
		s.stKey = ""
	} else if s.stItem >= s.visible[0] && s.stItem < s.visible[1] {
		if wait := s.stAt.Add(statusTipDelay); rc.now.Before(wait) {
			gtx.Execute(op.InvalidateCmd{At: wait})
		} else {
			m := op.Record(gtx.Ops)
			statusTooltip(gtx, h, th, c, s.stTip, s.stRect, theme.WindowBounds(s.origin))
			op.Defer(gtx.Ops, m.Stop())
		}
	}

	// ArchivesBanner ("Latest messages") and UnreadBanner (banners.go): the
	// message pane's (a thread pane has neither; its own banners are
	// ThreadFloatingBanners, thread_banner.go)
	s.archivesBanner(gtx, h, size)
	s.paneUnreadBanner(gtx, h, rc, size)
	if req := s.dayReq; req != nil {
		s.dayReq = nil
		s.openDayMenu(h, *req, rc.now, gtx)
	}
}

// tooltip is a toolbar button's Tooltip (F2XN BaseTooltip, position
// "top", TooltipTip's .c-tooltip__tip--top) for the button target at
// opacity alpha, placed by theme.PlaceTooltip inside bounds (the window
// in the list's coordinates): 8px over the button, keepInBounds' 3px
// inside the window, under the button when there's no room above
// (tryOppositePosition), the arrow on the button's centre. The tip is
// padding 8px 12px, radius 8, 13px/1.25 bold, centred, at most 200px wide
// (wrapping), with the 6px arrow (a 9px square turned 45°).
func tooltip(gtx layout.Context, th *theme.Theme, c *colors, txt string, target, bounds image.Rectangle, alpha float32) {
	if alpha <= 0 {
		return
	}
	tip := slackc.BodyScope().Child(tooltipClass, 0)
	b := tooltipBox(gtx, th, c, tip, txt, target, bounds)
	if o := tip.Opacity(); o < 1 {
		alpha *= max(o, 0)
	}
	bg := tip.Background(c.Tooltip)
	if alpha < 1 {
		// the popover's opacity, blended as the browser does (sRGB): Gio's
		// layer opacity blends in linear light
		fade := bg
		fade.A = uint8(alpha*255 + .5)
		defer paint.PushOpacity(gtx.Ops, float32(theme.LinearAlpha(fade, c.Bg).A)/255).Pop()
	}
	theme.TooltipShape(gtx, b.r, dp(gtx, 8), b.arrow, bg, tipGlow(th)) // theme/tooltip.go
	t := op.Offset(b.text).Push(gtx.Ops)
	b.call.Add(gtx.Ops)
	t.Pop()
}

// tipBox is a laid-out toolbar tooltip: the box, its arrow, and the text
// (recorded) and where it goes.
type tipBox struct {
	r     image.Rectangle
	arrow theme.TooltipArrow
	text  image.Point
	call  op.CallOp
}

// tooltipBox lays tooltip's box out (tooltip has the geometry).
func tooltipBox(gtx layout.Context, th *theme.Theme, c *colors, tip slackc.Styled, txt string, target, bounds image.Rectangle) tipBox {
	ts := cssText(tip, textStyle{size: 13, weight: theme.Bold, col: c.TooltipText, lineH: 16.25})
	padX, padY := dp(gtx, 12), dp(gtx, 8)
	m := op.Record(gtx.Ops)
	td := flowTip(gtx, th, []tipRun{{st: ts, txt: txt}}, dp(gtx, 200)-2*padX, tipBars{})
	call := m.Stop()
	bw, bh := td.X+2*padX, td.Y+2*padY
	pl := theme.PlaceTooltip(gtx, target, image.Pt(bw, bh), bounds, theme.BaseTooltip(gtx, theme.TipTop))
	r := pl.Box
	return tipBox{r: r, arrow: pl.Arrow, text: image.Pt(r.Min.X+(bw-td.X)/2, r.Min.Y+padY), call: call}
}

func (s *listState) toolbar(gtx layout.Context, h Host, rc *rowCtx, r *row, idx int, size image.Point) {
	c := rc.c
	th := h.Theme()
	// MessageActions' position (actionsAt) puts the card at the row's top
	// plus the position's offset, and only there: Slack's card is
	// position: absolute in the row, inside the list's scroller
	// (.c-virtual_list, overflow-y scroll), so it moves with the row and the
	// list clips it. A row whose top is at the list's top edge has its card
	// cut off, one scrolled above it its card out of view; nothing moves it
	// into view (an older Clack kept it 4px under the list's top, so a tall
	// message scrolled up had its toolbar there, far above the pointer).
	at := s.actionsAt(idx, rc.dense)
	y := s.pos[idx] + dp(gtx, actionsTop[at])
	s.tbRow, s.tbSize, s.tbHov, s.tbAt = r, size, -1, at
	s.tilt.mount(r)
	s.quick.prepare(h, s.ch, r)
	s.prepareButtons(h.Core(), r)
	if !reactable(h.Core(), r, s.thread) { // canAddReaction; ThreadRootCanvas: no quick reactions
		s.quick.names = s.quick.names[:0]
	}
	// message_actions_truncate (L2dv calculateResponsiveActions): the
	// quick reactions go when the toolbar with them is wider than the
	// message from its left edge to the toolbar's right edge
	if app := h.Core(); app != nil && app.Experiment("message_actions_truncate") == "on" && len(s.quick.names) > 0 {
		bs, pad := dp(gtx, mt.toolbarBtn), dp(gtx, mt.toolbarPad)
		if pad*2+(len(s.quick.names)+len(s.tbBtns))*bs > size.X-dp(gtx, mt.toolbarRight) {
			s.quick.names = s.quick.names[:0]
		}
	}
	// Right-aligned: measure first (a plugin may add to the toolbar). The
	// card is absolutely positioned in Slack, so it shrink-wraps: unbounded
	// width makes a patched (block-level) root as wide as its content
	// instead of the whole list (OwlMode's patch drew it at the left edge).
	rec := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(1<<24, gtx.Constraints.Max.Y)}
	d := slackc.Component(g, "MessageActions", s, (*listState).actionsProps, (*listState).layoutActions)
	call := rec.Stop()
	x := size.X - dp(gtx, mt.toolbarRight) - d.Size.X
	t := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	s.tbRect, s.tbFor, s.tbKey = image.Rectangle{Min: image.Pt(x, y), Max: image.Pt(x, y).Add(d.Size)}, idx, s.items[idx].key
	// the hovered button's tooltip (the card's top is 4px over the button)
	bs, pad := dp(gtx, mt.toolbarBtn), dp(gtx, mt.toolbarPad)
	key, tip, cx := "", "", 0
	if i := s.quick.hov; i >= 0 {
		key, tip, cx = "q"+strconv.Itoa(i), s.quick.tip(i), x+pad+i*bs+bs/2
	}
	if i := s.tbHov; i >= 0 {
		slot := 0
		for k, b := range s.tbBtns {
			if b == i {
				slot = k
			}
		}
		key, tip, cx = "b"+strconv.Itoa(i), s.tbLabel[i], x+pad+(len(s.quick.names)+slot)*bs+bs/2
		s.tip = ""
	}
	if a := s.tilt.tipAlpha(gtx, key); a > 0 {
		btn := image.Rect(cx-bs/2, y+pad, cx-bs/2+bs, y+pad+bs)
		tooltip(gtx, th, c, tip, btn, theme.WindowBounds(s.origin), a)
	}
}

// MessageActions' positions (actions_MessageActions' position prop: its
// wrapper is c-message_kit__actions--{position}, the card's
// .c-message__actions top follows it).
const (
	actsDefault = iota // top -16px
	actsAbove          // .c-message_kit__actions--above: top -28px
	actsInside         // .c-message_kit__actions--inside: top 4px
)

// actionsTop is each position's top (dp) from the row's top, and
// actionsPos its name (the position prop).
var (
	actionsTop = [...]float32{actsDefault: mt.toolbarTop, actsAbove: -28, actsInside: 4}
	actionsPos = [...]string{actsDefault: "default", actsAbove: "above", actsInside: "inside"}
)

// actionsAt is item idx's MessageActions position:
//
//   - the thread pane's root: "inside" (ThreadRoot: actionPosition
//     "inside" outside the Threads view, which ThreadRootGeneric hands
//     MessageActions), so the card sits 4px inside the root's top;
//   - "above" for a grouped row and for every channel row under Compact
//     (Message: w||a ? "above" : "default"); a thread reply's is above
//     only when grouped (ThreadReplyGeneric: isCompact||adjacent,
//     isCompact being the Threads view's);
//   - "default" otherwise.
func (s *listState) actionsAt(idx int, dense bool) int {
	it := &s.items[idx]
	switch {
	case s.thread && it.idx < len(s.snap.msgs) && len(it.group) == 0 && s.snap.msgs[it.idx].m.TS == s.ts:
		return actsInside
	case !it.header || (dense && !s.thread):
		return actsAbove
	}
	return actsDefault
}

// prepareButtons picks r's toolbar buttons and their labels: the thread
// and share actions follow Slack's rules (actions_rules.go), the rest are
// always there.
func (s *listState) prepareButtons(app *core.App, r *row) {
	s.tbBtns = s.tbBtns[:0]
	s.tbLabel, s.tbQAs = tbTips, tbQA
	for i := 0; i < tbCount; i++ {
		switch i {
		case tbReply:
			l := replyLabel(app, &r.src, s.thread)
			if l == "" {
				continue
			}
			s.tbLabel[i] = l
		case tbForward:
			l, qa, ok := shareAction(app, &r.src, s.thread)
			if !ok {
				continue
			}
			s.tbLabel[i], s.tbQAs[i] = l, qa
		case tbTodo:
			s.tbLabel[i] = todoAction(app, &r.src).Label // Yoyn: "Complete to-do" on a to-do
		case tbAddReact:
			if !reactable(app, r, s.thread) {
				continue
			}
		}
		s.tbLabel[i] = tbTip(i, s.tbLabel[i])
		s.tbBtns = append(s.tbBtns, i)
	}
}

func (s *listState) actionsProps() slackc.Props {
	r := s.tbRow
	return s.paneProps(slackc.Props{"$key": r.ts, "msg": r.message(), "ts": r.src.TS, "channelId": r.src.Channel, "hover": true,
		"isEditing": false, "position": actionsPos[s.tbAt], "messageContainerType": "message", "viewContext": r.viewContext(), "windowId": "main"})
}

func (s *listState) reactionButtonProps() slackc.Props {
	return s.paneProps(slackc.Props{"$key": s.tbRow.ts, "aria-label": tbTip(tbAddReact, tbTips[tbAddReact]), "label": tbTip(tbAddReact, tbTips[tbAddReact]),
		"data-qa": tbQA[tbAddReact], "actionId": "add_reaction", "showLabel": false})
}

// layoutActions is MessageActions, the hover toolbar (the one OwlMode
// wraps; Slack's other MessageActions wraps the whole message).
func (s *listState) layoutActions(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	st := actionsClass.Enter(gtx, css.StateHover, 1<<s.tbAt) // c-message_kit__actions--{position}
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{}
	}
	if st.Active() {
		return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return s.actionsCard(gtx, st) })
	}
	return s.actionsCard(gtx, st)
}

func (s *listState) actionsCard(gtx layout.Context, st slackc.Styled) layout.Dimensions {
	c := s.rc.c
	bs := dp(gtx, mt.toolbarBtn)
	pad := dp(gtx, mt.toolbarPad)
	nq := len(s.quick.names)
	size := image.Pt(pad*2+(nq+len(s.tbBtns))*bs, bs+2*pad)
	s.tbCard = st.Background(c.Bg)
	shadowCard(gtx.Ops, image.Rectangle{Max: size}, dp(gtx, 12), dp(gtx, 1), s.tbCard, c.PillBorder, c.Shadow)
	// the list's scroll under the buttons (toolbar_scroll.go), after the
	// shadow, which the area would clip
	defer s.actionsScrollArea(gtx, image.Rectangle{Max: size}).Pop()
	s.quick.hov = -1
	for i := 0; i < nq; i++ {
		at(gtx, pad+i*bs, pad, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(bs, bs))
			return s.quickButton(gtx, i)
		})
	}
	for k, i := range s.tbBtns {
		at(gtx, pad+(nq+k)*bs, pad, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(bs, bs))
			if i == tbAddReact {
				return slackc.Component(gtx, "MessageActionButtonReaction", s, (*listState).reactionButtonProps, (*listState).layoutReactionButton)
			}
			return s.toolbarButton(gtx, i)
		})
	}
	s.tilt.ask(gtx)
	return layout.Dimensions{Size: size}
}

// layoutReactionButton is MessageActionButtonReaction ("Add reaction…").
func (s *listState) layoutReactionButton(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	return s.toolbarButton(gtx, tbAddReact)
}

// toolbarButton draws toolbar button i (.c-message_actions__button).
func (s *listState) toolbarButton(gtx layout.Context, i int) layout.Dimensions {
	h, rc, r := s.rc.h, &s.rc, s.tbRow
	app := h.Core()
	m := r.src
	btn := &s.tb[i]
	var todo popups.TodoAction
	if i == tbTodo {
		todo = todoAction(app, &m)
	}
	if btn.Clicked(gtx) {
		anchor := s.anchor(h, image.Point{}, image.Point{})
		switch i {
		case tbAddReact:
			_ = rc
			o := popups.NewReactionPicker(h, anchor, m.Channel, m.TS, m.ThreadTS, nil)
			holdHover(s, o)
			h.ShowOverlay(o)
		case tbReply:
			// RwAt.onComment: the comment thunk with threadTs =
			// msg.thread_ts, so the key is the thread's even for a
			// broadcast reply, and its box takes the focus as it mounts
			popups.SetThreadFocusInput(h, m.Channel, threadRoot(&m))
			h.OpenThread(m.Channel, threadRoot(&m))
		case tbForward:
			h.ShowOverlay(popups.NewForwardModal(h, m))
		case tbTodo:
			s.toggleTodo(h, m)
		case tbList:
			if la, ok := h.(ListAdder); ok {
				la.AddMessageToList(m)
			}
		case tbMore:
			mm := m
			// the kebab in window px (its targetBounds): the pointer less
			// where in the button it pressed
			var trigger image.Rectangle
			if ps := btn.History(); len(ps) > 0 {
				o := anchor.Sub(ps[len(ps)-1].Position)
				trigger = image.Rectangle{Min: o, Max: o.Add(image.Pt(gtx.Constraints.Max.X, gtx.Constraints.Max.X))}
			}
			// Slack's menu top sits 32px above the kebab's centre (16 above
			// the button), its right edge 3px left of the centre.
			anchor = anchor.Add(image.Pt(-dp(gtx, 2), -dp(gtx, 32)))
			where := popups.InChannel // messageContainerType: a thread's Mark unread marks the thread
			if s.thread {
				where = popups.InThread
			}
			o := popups.NewMessageMenuOpts(h, anchor, m, popups.MessageMenuOpts{
				Delete:  func(ctx context.Context, ch, ts string) error { return deleteMessage(ctx, app, ch, ts) },
				OnEdit:  func() { s.startEdit(h, mm.TS) },
				OnReply: func() { h.OpenThread(mm.Channel, threadRoot(&mm)) },
				Where:   where,
				Trigger: trigger,
			})
			holdHover(s, o) // onMoreActionsMenuOpen: onPopoverOpen
			h.ShowOverlay(o)
		}
	}
	hov := btn.Hovered()
	if hov {
		s.tbHov = i
	}
	st := actionBtnClass.EnterAttr(gtx, btnState(gtx, btn), 0, "data-qa", s.tbQAs[i])
	defer st.Leave()
	bs := gtx.Constraints.Max.X
	if st.Hidden() {
		return layout.Dimensions{Size: image.Pt(bs, bs)}
	}
	var d layout.Dimensions
	if st.Active() {
		d = st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return s.toolbarIcon(gtx, btn, i, todo, st) })
	} else {
		d = s.toolbarIcon(gtx, btn, i, todo, st)
	}
	slackc.InspectArea(gtx, image.Rectangle{Max: d.Size}) // the button, for the inspect tool's picker
	return d
}

// toolbarIcon draws button i's icon; todo is the to-do action's (Yoyn).
// The hover background, the icon's colour and its scale (and Add
// reaction…'s tilt) follow Slack's transitions (toolbar_tilt.go).
func (s *listState) toolbarIcon(gtx layout.Context, btn *widget.Clickable, i int, todo popups.TodoAction, st slackc.Styled) layout.Dimensions {
	c := s.rc.c
	bs := gtx.Constraints.Max.X
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		rr := image.Rectangle{Max: image.Pt(bs, bs)}
		pointerCursor(gtx, rr)
		hov := btn.Hovered()
		move, paint := s.tilt.step(gtx, &s.tilt.tb[i], hov || gtx.Focused(btn), hov, tiltDur)
		if paint > 0 { // from transparent: the card's colour
			rrect(gtx.Ops, rr, dp(gtx, 8), mixColor(s.tbCard, st.Background(c.ToolbarHoverBg), paint))
		}
		col := mixColor(c.ToolbarIcon, c.ToolbarHoverIcon, paint)
		isz := float32(18)
		ic := tbIcons[i]
		if i == tbTodo && todo.Complete {
			// .c-message_actions__button--complete_todo (and :hover):
			// content-hgl-2 at font-size 24px, check-circle-filled
			ic, col, isz = todo.HoverIcon, c.HuddleLive, 24
		}
		col = st.Color(col)
		is := dp(gtx, isz)
		o := (bs - is) / 2
		ctr := f32.Pt(float32(o)+float32(is)/2, float32(o)+float32(is)/2)
		tr := op.Affine(tiltAffine(ctr, move, i == tbAddReact)).Push(gtx.Ops) // svg[data-qa=add-reaction] tilts
		at(gtx, o, o, func(gtx layout.Context) layout.Dimensions {
			return icons.Get(ic)(gtx, unit.Dp(isz), col)
		})
		tr.Pop()
		return layout.Dimensions{Size: rr.Size()}
	})
}

// toggleTodo is the to-do action's click (Oyqq, core ToggleTodo): the
// row redraws from the saved metadata as the write starts.
func (s *listState) toggleTodo(h Host, m core.Message) {
	popups.RunTodoAction(h, m)
	h.Invalidate()
}

func threadRoot(m *core.Message) string {
	if m.ThreadTS != "" {
		return m.ThreadTS
	}
	return m.TS
}

// typingText is Slack's TypingNames line ("X is typing", no ellipsis).
func typingText(names []string) string {
	if !uipref.Get().ShowTyping { // show_typing off
		return ""
	}
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0] + " is typing"
	case 2:
		return names[0] + " and " + names[1] + " are typing"
	}
	return "Several people are typing"
}

// startEdit replaces the body of message ts with the inline edit composer.
func (s *listState) startEdit(h Host, ts string) {
	r, ok := s.rows[ts]
	if !ok {
		i := -1
		for k := range s.snap.msgs {
			if s.snap.msgs[k].m.TS == ts {
				i = k
			}
		}
		if i < 0 {
			return
		}
		r = newListRow(&s.snap.msgs[i], s.thread)
		s.rows[ts] = r
	}
	if r.edit != nil {
		r.edit.Focus()
		return
	}
	// Plugins may change what the editor sees (patchActionableMessage):
	// ask JS off the UI goroutine, open on a later frame.
	if t := tautOf(); t != nil && t.ActionablePatched() {
		src, app := r.src, h.Core()
		go func() {
			// the user's ↑: on their lane, ahead of the queued renders
			ctx, cancel := context.WithTimeout(taut.UserAction(context.Background()), 5*time.Second)
			defer cancel()
			m := src
			if sm, ok := t.ActionableMessage(ctx, src.Channel, src.TS); ok && app != nil {
				am := app.MessageFromSlack(src.Channel, &sm)
				am.TS, am.Channel = src.TS, src.Channel
				m = am
			}
			s.pend.put(m)
			h.Invalidate()
		}()
		return
	}
	s.openEdit(h, r.src)
}

// openEdit opens the inline editor on m (UI goroutine).
func (s *listState) openEdit(h Host, m core.Message) {
	r, ok := s.rows[m.TS]
	if !ok {
		return
	}
	for _, o := range s.rows {
		if o.edit != nil {
			o.edit = nil // one inline editor at a time, like Slack
		}
	}
	t := tautOf()
	if t != nil {
		t.SetEditing("main", m.Channel, m.TS, true) // patchMessageText leaves it alone
	}
	rr := r
	r.editFrames = 0
	r.edit = composer.NewEdit(h, m, func() {
		rr.edit = nil
		if t != nil {
			t.SetEditing("main", m.Channel, m.TS, false)
		}
		h.Invalidate()
	})
	app := h.Core()
	r.edit.SaveEditThen = func(ctx context.Context, ch, ts, text string, blocks json.RawMessage, fileIDs []string, done func(error)) {
		commitEditThen(ctx, app, ch, ts, text, blocks, fileIDs, done)
	}
	r.edit.SaveEdit = func(ctx context.Context, ch, ts, text string, blocks json.RawMessage, fileIDs []string) error {
		return commitEdit(ctx, app, ch, ts, text, blocks, fileIDs)
	}
	s.scrollTo(m.TS)
	h.Invalidate()
}

// editLast edits the viewer's most recent message in this list.
func (s *listState) editLast(h Host) bool {
	app := h.Core()
	if app == nil {
		return false
	}
	self := app.Self().ID
	for i := len(s.snap.msgs) - 1; i >= 0; i-- {
		m := &s.snap.msgs[i].m
		if m.User == self && !m.System && !m.Pending {
			if s.thread && m.TS == s.ts && len(s.snap.msgs) > 1 {
				continue // in a thread, Up edits your last reply first
			}
			s.startEdit(h, m.TS)
			return true
		}
	}
	return false
}

// scrollTo brings ts into view if it isn't visible.
func (s *listState) scrollTo(ts string) {
	for i, it := range s.items {
		if it.kind == itMsg && it.key == ts {
			if i == s.newestItem() {
				// newest: the end, which a list showing the newest messages
				// keeps to (a window keeps to none, and Position{} is its top)
				s.toEnd()
				return
			}
			if i >= s.visible[0] && i < s.visible[1]-1 {
				return
			}
			s.list.Position = layout.Position{BeforeEnd: true, First: max(0, i-2)}
			s.stopScroll()
			return
		}
	}
}

// scrollToFind is find in thread's scrollToTs with scrollOffset -200:
// ts's row off px below the list's top (as near as the list's ends let
// it). False when ts isn't loaded.
func (s *listState) scrollToFind(ts string, off int) bool {
	for i, it := range s.items {
		if it.kind == itMsg && it.key == ts {
			s.list.Position = layout.Position{BeforeEnd: true, First: i, Offset: -off}
			s.stopScroll()
			return true
		}
	}
	return false
}

func (s *listState) retry(h Host, r *row) {
	m := r.m
	app := h.Core()
	if app == nil || m.ClientMsgID == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := app.Resend(ctx, m.Channel, m.ClientMsgID); err != nil {
			h.Toast("Couldn't send the message")
		}
		h.Invalidate()
	}()
}

func (s *listState) discard(h Host, r *row) {
	if app := h.Core(); app != nil && r.m.ClientMsgID != "" {
		app.DiscardPending(r.m.Channel, r.m.ClientMsgID) // emits "delete"
	}
}

// joinKind classifies join/leave system messages for rollups (0 = other).
func joinKind(m *core.Message) int {
	switch m.Subtype {
	case "channel_join", "group_join":
		return 1
	case "channel_leave", "group_leave":
		return 2
	}
	return 0
}

// groupEntry builds the summary message for a run of joins or leaves:
// "A and B have joined the channel", "A, B and N others have joined the
// channel" (names are member links). The row is the run's first message,
// as Slack's rollups are (client-boot Nw4e keeps the first message of
// the run and folds the rest into its rollup): its sender, time and
// isSenderHidden. A member the viewer hid is RollupName's
// HiddenUserIconAndName (hiddenSpan, system.go).
func (s *listState) groupEntry(h Host, idx []int) entry {
	msgs := s.snap.msgs
	first, last := msgs[idx[0]], msgs[idx[len(idx)-1]]
	var rev uint32 = uint32(len(idx))
	seen := map[string]bool{}
	var users []string
	app := h.Core()
	hidden := func(id string) bool { return app != nil && app.UserHidden(id) }
	for _, i := range idx {
		rev = rev*31 + msgs[i].rev
		if u := msgs[i].m.User; u != "" && !seen[u] {
			seen[u] = true
			users = append(users, u)
		}
	}
	// RollupName (quVX): "" for a member core can't name yet (the unknown
	// stub; richview names the span once it's known), never the id
	name := func(id string) string {
		if app := h.Core(); app != nil {
			if u, ok := app.User(id); ok {
				return u.Name
			}
			return ""
		}
		return id
	}
	var spans []richtext.Span
	user := func(id string) {
		if hidden(id) {
			spans = append(spans, hiddenSpan(id))
			rev = rev*31 + 7 // a hidden name rebuilds the row
			return
		}
		spans = append(spans, richtext.Span{Kind: richtext.User, ID: id, Text: "@" + name(id)})
	}
	txt := func(t string) { spans = append(spans, richtext.Span{Kind: richtext.Text, Text: t}) }
	shown := users
	if len(users) > 3 {
		shown = users[:2]
	}
	for i, id := range shown {
		switch {
		case i == 0:
		case i == len(shown)-1 && len(shown) == len(users):
			txt(" and ")
		default:
			txt(", ")
		}
		user(id)
	}
	if rest := len(users) - len(shown); rest > 0 {
		txt(" and " + itoa(rest) + " others")
	}
	verb := " have joined the channel"
	if joinKind(&last.m) == 2 {
		verb = " have left the channel"
	}
	if len(users) == 1 {
		verb = strings.Replace(verb, "have", "has", 1)
	}
	txt(verb)
	m := first.m
	m.Reactions = nil
	m.Doc = richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: spans}}}
	return entry{m: m, rev: rev | 1}
}

// closeEdits drops any open inline editor (leaving the conversation).
// editComposer is the list's open inline edit, if any (Slack has one per
// window: isMessageEdited).
func (s *listState) editComposer() *composer.Composer {
	for _, r := range s.rows {
		if r.edit != nil && !r.edit.Done() {
			return r.edit
		}
	}
	return nil
}

// placeJump places a pending jump's message once the built items hold
// it, and flashes it from then. Slack's message list scrolls the startTs
// message to the anchor line, ANCHOR_OFFSET under its top, for a message
// it holds (scrollToKey: AnchorLayout.getOffsetForKey) as for one it
// loads (the layout's anchor), bracketed by the content's ends. The
// anchor drops at that scroll's echo (MessageList.onScroll), so nothing
// holds the message after: AnchorLayout's own anchoring keeps the row on
// the line through height and item changes (lineRow). highlightRoot's
// highlightLazy scrolls only to a message whose top isn't on screen
// (scrollToKey's lazy: isItemInView).
func (s *listState) placeJump(now time.Time) {
	if s.pendingJump == "" || s.thread { // a thread's: placeThreadJump
		return
	}
	for i, it := range s.items {
		if it.kind == itMsg && it.idx < len(s.snap.msgs) && s.snap.msgs[it.idx].m.TS == s.pendingJump {
			if !s.jumpLazy || !s.topOnScreen(it.key) {
				s.placeAt(i, anchorOffset)
			}
			s.jumpLazy = false
			s.positioned = true
			if s.highlight == s.pendingJump && s.hlRestart {
				s.highlightAt = now // the flash starts as its message is placed
			}
			s.pendingJump = ""
			return
		}
	}
	// No row has it (a message deleted since, a reply the history never
	// holds, a join folded into a group): once what the list shows covers
	// it, the jump is to the first message at or after it, Slack's
	// findTimestampClosestToStartTs (find(timestamps, t => t >= startTs)),
	// with nothing to flash (its highlightTs names no row); with nothing at
	// or after it, the end (getOffsetForKey(undefined)). Nothing stays
	// pending.
	if s.thread || !s.jumpCovered() {
		return
	}
	if i := s.firstItemFrom(s.pendingJump); i >= 0 {
		s.placeAt(i, anchorOffset)
	} else {
		s.toEnd() // a window's end too (Position{} is its top)
	}
	s.jumpLazy = false
	s.positioned = true
	s.pendingJump = ""
}

// placeThreadJump is a thread pane's jump (ThreadView.JumpTo) once the
// slice it asked for is in (jumpThread: the store's slice at the reply,
// read around it when the store has none; thread_load.go): a pane that
// just opened starts at getThreadStartPosition's message (threadStart:
// the reply 100px under the top, or at the top when it's an unread one at
// or before the last read), and one already showing the thread scrolls
// that message's top to the list's top unless its top is on screen
// (scrollToKey lazy, the thread list's anchor line at the top). A ts the
// slice doesn't hold starts it at the newest message at or before it,
// else at its top.
func (s *listState) placeThreadJump(now time.Time) {
	if s.pendingJump == "" || !s.snap.loaded || s.snap.loading || s.built != s.snap.rev {
		return
	}
	key, offset := s.threadStart(s.pendingJump)
	i := s.itemOfKey(key)
	switch {
	case i < 0:
		s.list.Position = layout.Position{BeforeEnd: true} // getOffsetForKey of a ts it doesn't hold: 0
		s.stopScroll()
	case s.jumpLazy && s.topOnScreen(key):
	case s.jumpLazy || !offset:
		s.placeAt(i, 0)
	default:
		s.placeAt(i, 100) // getThreadStartPosition's startOffset
	}
	if s.highlight == s.pendingJump && s.hlRestart {
		s.highlightAt = now // the flash starts as its message is placed
	}
	s.pendingJump, s.jumpLazy = "", false
}

// mounted reports whether the list was drawn in the latest frame lists
// drew (or the one before, when another list drew first this frame): its
// rows are mounted, so a repeat jump finds its flash running. A list off
// screen since mounts its rows anew, and their flash starts over.
func (s *listState) mounted() bool {
	latest, before := s.hb.latestFrames()
	return s.laidOut && (s.laidAt.Equal(latest) || s.laidAt.Equal(before))
}

// tailItems is how many of the last items' heights the list keeps
// (tailH): enough for the gap to its end past what a layout reached.
const tailItems = 8

// gapToEnd is how far the list sits above its end at p, px: past its
// last laid-out item's bottom, the items after it at the heights they
// last laid out at (the 24px bottom spacer under a list 35px from its
// end: the layout stops at the viewport). MaxInt32 when that's more than
// tailItems or one hasn't laid out yet. It counted only a list whose last
// item was in view, so STICKY_EPSILON reached no further than the spacer.
func (s *listState) gapToEnd(p layout.Position) int {
	last := p.First + p.Count
	if last >= len(s.items) {
		return max(0, -p.OffsetLast)
	}
	if len(s.items)-last > tailItems {
		return math.MaxInt32
	}
	gap := -p.OffsetLast
	for i := last; i < len(s.items); i++ {
		h, ok := s.tailH[s.items[i].key]
		if !ok {
			return math.MaxInt32
		}
		gap += h
	}
	return max(0, gap)
}

// gapNow is how far the list sits above its end where it is now: the last
// layout's gap (endGap), moved by as much as a place written since then
// moved it (the page keys, the scrollbar, ScrollPx), by the heights the
// last layout measured; MaxInt32 when they don't reach. setKeys's
// stickToBottom reads the scrollTop such a write just set (dxC+): a new
// message in the frame after PageUp from the end took the list back down.
func (s *listState) gapNow() int {
	p, q := s.list.Position, s.laidPos
	if samePos(p, q) || s.endGap == math.MaxInt32 {
		return s.endGap
	}
	if !p.BeforeEnd {
		return 0 // a ScrollToEnd list's end
	}
	up := q.Offset - p.Offset // how much higher p sits than the last layout
	for i := p.First; i < q.First; i++ {
		if i >= len(s.heights) || s.heights[i] <= 0 {
			return math.MaxInt32
		}
		up += s.heights[i]
	}
	for i := q.First; i < p.First; i++ {
		if i >= len(s.heights) || s.heights[i] <= 0 {
			return math.MaxInt32
		}
		up -= s.heights[i]
	}
	return max(0, s.endGap+up)
}

// stopScroll ends the reader's running scroll (a wheel curve, a fling)
// where code writes the list's place: Chromium cancels a scroll animation
// on a scrollTop write, so the rest of the curve doesn't move the list off
// it (an upward notch, then End inside its curve, left the list short of
// its end).
func (s *listState) stopScroll() {
	s.list.StopScroll()
	s.actScroll.Stop()
}

// toEnd puts the list at its end, whether or not it keeps to it.
func (s *listState) toEnd() {
	s.stopScroll()
	s.list.Position = layout.Position{} // a ScrollToEnd list: its end
	if !s.list.ScrollToEnd {
		s.list.Position = layout.Position{BeforeEnd: true, First: len(s.items)} // drawn back from the last item
	}
}

// placeAt places item i off under the list's top in the next layout.
func (s *listState) placeAt(i int, off unit.Dp) {
	s.place = jumpPlace{key: s.items[i].key, off: off}
	s.list.Position = layout.Position{BeforeEnd: true, First: i} // until the layout places it
}

// jumpCovered reports whether the messages the list shows cover the
// pending jump's ts: loaded, no load out for it, and the ts within them
// (from their first message on, or anywhere before it when nothing is
// older; up to their last, or past it when they end at the newest).
func (s *listState) jumpCovered() bool {
	first, last, ok := s.ends()
	if !s.snap.loaded || s.snap.loading || s.snap.around != "" || !ok || s.built != s.snap.rev {
		return false
	}
	ts := s.pendingJump
	return (!s.snap.hasMore || !tsLess(ts, first)) && (!s.snap.newestMissing || !tsLess(last, ts))
}

// takeJump takes a plugin's jumpToReply from the snapshot (the model's
// jump): the jump, and a new flash, as highlight and JumpTo start one. It
// kept an earlier flash's hlSeen, and the unmount rule ended this flash in
// the first layout that didn't draw its message yet.
func (s *listState) takeJump(now time.Time) {
	if s.snap.jump == "" {
		return
	}
	s.highlight, s.highlightAt, s.hlSeen, s.hlRestart = s.snap.jump, now, false, true
	s.pendingJump, s.snap.jump = s.snap.jump, ""
}

// ends is the first and last message of the list's model (ok: it has
// any): the range a jump or a date reads. Plugins' injected messages
// (mergeInjected) don't widen it: one from today under a window from
// months ago counted as the window's end, and a jump past the window
// landed on it.
func (s *listState) ends() (first, last string, ok bool) {
	return s.own[0], s.own[1], s.own[0] != ""
}

// firstItemFrom is the first message item holding a message at or after
// ts (a join group by its last), -1 for none.
func (s *listState) firstItemFrom(ts string) int {
	for i, it := range s.items {
		if it.kind == itMsg && it.idx < len(s.snap.msgs) && !tsLess(s.snap.msgs[it.idx].m.TS, ts) {
			return i
		}
	}
	return -1
}

// jumpPlace is a jump's placement (placeJump): the item keyed key, off
// under the list's top.
type jumpPlace struct {
	key string
	off unit.Dp
}

// lineOff is the list's anchor line under its top, px, and it notes it and
// STICKY_EPSILON for the next sync: AnchorLayout's ANCHOR_OFFSET and
// STICKY_EPSILON as getMessageListLayout makes them (IY25/5cnQ: 64, and
// half the default row height, 80 for the "light" messages theme and 60
// for "dense", so 40 Clean and 30 Compact) in a conversation, and the
// thread list's own AnchorLayout's (Twp/: new AnchorLayout(), the line at
// the top, 2) in a thread. The theme getMessageListLayout reads is
// MessagePane's (xp33: "dense" === messages_theme ? "dense" : "light"),
// not the color mode.
func (s *listState) lineOff(gtx layout.Context) int {
	line, eps := 0, unit.Dp(2)
	if !s.thread {
		line, eps = gtx.Dp(anchorOffset), 40
		if uipref.Get().Dense {
			eps = 30
		}
	}
	s.linePx, s.stickyPx = line, gtx.Dp(eps)
	return line
}

// lineRow is the row on the anchor line, line px under the list's top, in
// the last layout, and its top: findAnchor's row, the first message row
// ending below the line ("" for none).
func (s *listState) lineRow(line int) (string, int) {
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items) && i < len(s.pos) && i < len(s.heights); i++ {
		if s.items[i].kind == itMsg && s.pos[i]+s.heights[i] > line {
			return s.items[i].key, s.pos[i]
		}
	}
	return "", 0
}

// lineRows is the message rows from the one on the anchor line (lineRow)
// down to the last in view, with their tops as the last layout drew them:
// findAnchor's candidates when the keys change (setKeys walks the old
// keys to the first row ending below the line that's still there).
func (s *listState) lineRows(line int) []laidRow {
	var out []laidRow
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items) && i < len(s.pos) && i < len(s.heights); i++ {
		if s.items[i].kind == itMsg && (len(out) > 0 || s.pos[i]+s.heights[i] > line) {
			out = append(out, laidRow{key: s.items[i].key, y: s.pos[i], h: s.heights[i]})
		}
	}
	return out
}

// itemOfKey is the item keyed key, -1 for none.
func (s *listState) itemOfKey(key string) int {
	if i, ok := s.sel.keyIdx[key]; ok && i < len(s.items) && s.items[i].key == key {
		return i
	}
	for i := range s.items {
		if s.items[i].key == key {
			return i
		}
	}
	return -1
}

// topOnScreen reports whether the list draws message item key's top
// inside it (isItemInView, which reads the live scroll): from its last
// layout, only when that laid out these very items in the latest frame
// lists drew (or the one before, when another list already drew this
// frame). A list off screen since, or rebuilt since, doesn't know.
func (s *listState) topOnScreen(key string) bool {
	latest, before := s.hb.latestFrames()
	if !s.laidOut || s.laidBuilt != s.built || s.laidAt.IsZero() || !s.laidAt.Equal(latest) && !s.laidAt.Equal(before) {
		return false
	}
	for _, r := range s.laid {
		if r.key == key {
			return r.y >= 0 && r.y <= s.size.Y
		}
	}
	return false
}

// firstVisibleMsg is the first message the list showed last frame ("" for
// none).
func (s *listState) firstVisibleMsg() string {
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
		if s.items[i].kind == itMsg {
			return s.items[i].key
		}
	}
	return ""
}

// Sending from the composer under a channel (Slack's sendMessage thunk,
// client-boot n6uE, build 2c0d02ca130f): after addMsgToEnd puts the
// pending copy at the end of the history, a top-level message (no
// thread_ts) dispatches jumpToTimestamp({ts: J.ts || null}) with the
// copy's temp ts, wherever the pane was. In the main window that
// navigates the sending view to startTs = ts with scrollmark null and
// highlightTs = uniqueId() (5Q/C), so nothing flashes and the saved
// place is gone. BaseMessageList anchors the timestamp closest to startTs
// (getListProps' anchor) ANCHOR_OFFSET under the list's top
// (getMessageListLayout, IY25: 64, 120 for the degraded list), and
// AnchorLayout's bracketScrollTop (62rv) holds the scroll within the
// content, so a message that fits sits at the bottom of the list over its
// bottom spacer, and a taller one shows its top 64px down. The scroll
// event that follows drops the anchor (onScroll), so the list then sticks
// to the bottom as before (stickToBottom).
//
// No other message moves the list: someone else's message while you read
// history keeps your place (AnchorLayout keeps the first visible row)
// and ArchivesBanner's "Latest messages" only shows while the history
// stops short of the newest (reachedEnd false). A message with files goes
// through sendDraftFiles, whose appendPlaceholderMessage has no
// jumpToTimestamp, and a thread reply (thread_ts) skips it.
const (
	// anchorOffset is ANCHOR_OFFSET (getMessageListLayout, IY25: 64, 120
	// for the degraded list), dp: the anchor line under the list's top
	anchorOffset   = 64
	sendJumpOffset = anchorOffset     // dp
	sendJumpWait   = 10 * time.Second // the copy is in at once; this only drops a send that never reached core
)

// placeSendJump places the list at the viewer's message sent from the
// composer beside it (JumpToOwnSend) once the message, pending or
// already confirmed, is in the list.
func (s *listState) placeSendJump(gtx layout.Context, now time.Time) {
	if s.sendCID == "" {
		return
	}
	if now.Sub(s.sendAt) > sendJumpWait {
		s.sendCID = ""
		return
	}
	for i := len(s.items) - 1; i >= 0; i-- {
		it := s.items[i]
		if it.kind != itMsg || len(it.group) > 0 || it.idx >= len(s.snap.msgs) {
			continue
		}
		if s.snap.msgs[it.idx].m.ClientMsgID == s.sendCID {
			// Gio's List clamps an offset past the end the way
			// bracketScrollTop does: the end then sits at the bottom
			s.list.Position = layout.Position{BeforeEnd: true, First: i, Offset: -gtx.Dp(sendJumpOffset)}
			s.stopScroll()
			s.positioned = true
			s.sendCID = ""
			return
		}
	}
}

func (s *listState) closeEdits() {
	t := tautOf()
	for _, r := range s.rows {
		if r.edit != nil && t != nil {
			t.SetEditing("main", r.src.Channel, r.src.TS, false)
		}
		r.edit = nil
	}
}

// layoutLoadError is Slack's "Couldn't load messages · Retry", centered in
// the pane. Loads back off on their own (hub.backoff); Retry loads now.
func (s *listState) layoutLoadError(gtx layout.Context, h Host, c *colors, size image.Point) {
	th := h.Theme()
	// After auth_failed core sends nothing until the user signs in again,
	// so Retry becomes the sign-in prompt instead of a refetch.
	signedOut := h.Core() != nil && h.Core().SignedOut()
	if s.retryBtn.Clicked(gtx) {
		if signedOut {
			if sh, ok := h.(SignInHost); ok {
				sh.ShowSignIn()
			} else {
				h.Toast("Your Slack session ended. Sign in again to load messages.")
			}
		} else {
			s.hb.retry(s.m)
		}
	}
	st := textStyle{size: 15, col: c.Text2, lineH: 22}
	lk := textStyle{size: 15, weight: theme.Bold, col: c.Link, lineH: 22}
	msg, sep, link := "Couldn't load messages", " · ", "Retry"
	if signedOut {
		msg, link = "Your Slack session ended", "Sign in again"
	}
	w := measure(gtx, th, st, msg).X + measure(gtx, th, st, sep).X + measure(gtx, th, lk, link).X
	x, y := (size.X-w)/2, size.Y/2-dp(gtx, 11)
	d := at(gtx, x, y, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, st, msg) })
	x += d.Size.X
	d = at(gtx, x, y, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, st, sep) })
	x += d.Size.X
	at(gtx, x, y, func(gtx layout.Context) layout.Dimensions {
		return s.retryBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			dd := label(gtx, th, lk, link)
			if s.retryBtn.Hovered() {
				base := dd.Size.Y - dd.Baseline
				fill(gtx.Ops, image.Rect(0, base+dp(gtx, 1), dd.Size.X, base+dp(gtx, 2)), c.Link)
			}
			return dd
		})
	})
}
