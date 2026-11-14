package messages

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"strconv"
	"strings"
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
	hb     *hub
	m      *model
	thread bool
	ch     string
	ts     string // thread ts
	// container is the view container the list is in ("" main, "split";
	// see stateIn)
	container string

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
	positioned   bool
	markedTS     string
	lastMark     time.Time

	pointer   f32.Point
	pointerIn bool
	ctx       ctxPress // this frame's context-menu press (context.go)
	hoverIdx  int

	tb           [tbCount]widget.Clickable
	pbtn         map[string]*widget.Clickable // persistent "Message actions" buttons (persist.go)
	tbRect       image.Rectangle
	tbFor        int
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

	lastTyping time.Time

	noMark      bool
	unreadSeen  bool         // the "New" line (or no line at all) has been on screen
	pane        slackc.Props // $pane for components in a thread pane
	inj         injState     // plugin-injected messages
	pend        pendingEdit  // an edit waiting for taut's actionable message
	viewsOn     bool         // rows may show plugin views
	openUnread  bool         // the server said "unread" when the view opened (e.g. marked unread)
	jumpPages   int
	origin      image.Point // view origin in window coordinates
	lastOlder   time.Time
	highlight   string
	highlightAt time.Time
	// find in thread (thread_find.go): the highlight words (the query of
	// the last results), the current result and the result flashing
	findWords, findTS string
	findFlash         string
	findFlashAt       time.Time
	sel               selection
	sb                scrollbar
	pendingJump       string
	evictSeen         uint64 // the model's evictGen this list released its rows for (releaseEvicted)

	// the list as the last frame left it (anchoring in layoutList)
	laidOut   bool
	laidPos   layout.Position
	laidBuilt uint64

	rc rowCtx // this frame's row inputs (rows keep a pointer to it)

	// hover toolbar state for its natives (MessageActions)
	tbRow   *row
	tbSize  image.Point
	tbHov   int             // hovered toolbar button, -1 none
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
	s := &listState{hb: hb, ch: ch, ts: ts, thread: ts != "", container: pane, rows: map[string]*row{}, hoverIdx: -1, tbFor: -1}
	if ts != "" {
		s.m = hb.threadModel(ch, ts)
	} else {
		s.m = hb.channel(ch)
	}
	s.list.Axis = layout.Vertical
	s.list.ScrollToEnd = true
	if len(listCache) > 2*maxModels {
		for k := range listCache {
			delete(listCache, k)
			break
		}
	}
	listCache[key] = s
	return s
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
	if changed {
		s.sysUsers, s.sysHidden = s.hb.usersRev(), hk
		s.sysMissing = s.adjustSystem(h, sysOn)
	}
	if changed && len(s.inj.msgs) > 0 {
		s.mergeInjected()
	}
	if s.snap.jump != "" {
		s.highlight, s.highlightAt = s.snap.jump, now
		s.pendingJump, s.snap.jump = s.snap.jump, ""
	}
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
	// Scroll anchoring: remember the first visible message and its y (not
	// while a jump is pending: it places the list itself)
	anchorTS, anchorY := "", 0
	if s.list.Position.BeforeEnd && s.positioned && s.pendingJump == "" {
		for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
			if s.items[i].kind == itMsg && i < len(s.pos) {
				// items still index the previous snapshot; key is the ts
				anchorTS, anchorY = s.items[i].key, s.pos[i]
				break
			}
		}
	}
	s.build(h, now)
	s.rebuildKeyIdx()
	s.built = s.snap.rev
	s.dayKey = dk
	s.dense = dense
	if i := s.anchorItem(anchorTS); i >= 0 {
		s.list.Position.First = i
		s.list.Position.Offset = -anchorY
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
	if !s.thread && s.snap.loaded { // reachedEnd: Clack always holds the newest page
		s.items = append(s.items, item{kind: itBottom, key: "bottomSpacer"})
	}
	if s.thread && !s.snap.loaded && len(msgs) > 0 && s.snap.err == nil {
		// getThreadRowsAndKeys' "loading-end" row: some messages are in
		// (the root, a live reply) before the replies are (thread.go)
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
	s.hb.ensureLoaded(s.m)
	s.sync(h, now)
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
	}
	// first positioning: at the unread line if there are many unread
	if !s.positioned && s.snap.loaded {
		s.positioned = true
		if s.thread {
			// getThreadStartPosition: the first unread reply, the reply
			// last read to, or the root (thread_unread.go)
			s.list.Position = s.threadStartPosition(gtx, "")
		} else if u := s.unreadIndex(); u >= 0 && len(s.items)-u > 12 {
			s.list.Position = layout.Position{BeforeEnd: true, First: max(0, u-1)}
		} else {
			s.list.Position = layout.Position{}
		}
	}

	// hover from last frame's positions
	s.hoverIdx = -1
	if s.pointerIn {
		py := int(s.pointer.Y)
		if s.tbFor >= 0 && image.Pt(int(s.pointer.X), py).In(s.tbRect) {
			s.hoverIdx = s.tbFor
		} else {
			for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
				if s.items[i].kind == itMsg && py >= s.pos[i] && py < s.pos[i]+s.heights[i] {
					s.hoverIdx = i
					break
				}
			}
		}
	}
	if k := s.kbIndex(); k >= 0 {
		s.hoverIdx = k // the keyboard-selected message draws as hovered
	}

	// the wheel over the message actions (toolbar_scroll.go)
	actD := s.actionsScroll(gtx)

	// The anchor: the first message wholly on screen last frame, while
	// nothing has moved the list since (no rebuild, no jump).
	akey, ay := "", 0
	if s.laidOut && s.built == s.laidBuilt && samePos(s.list.Position, s.laidPos) && s.list.Position.BeforeEnd {
		for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
			if s.items[i].kind == itMsg && s.pos[i] >= 0 {
				akey, ay = s.items[i].key, s.pos[i]
				break
			}
		}
	}
	for i := range s.heights {
		s.heights[i] = 0
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
	s.origin = origin
	s.tip = ""
	ls := listClass.Enter(gtx, 0, 0)
	defer ls.Leave()
	lay := func() op.CallOp {
		m := op.Record(gtx.Ops)
		s.list.Layout(gtx, len(s.items), func(gtx layout.Context, i int) layout.Dimensions {
			d := s.layoutItem(gtx, h, rc, i)
			s.heights[i] = d.Size.Y
			return d
		})
		return m.Stop()
	}
	before := s.list.Position
	call := lay()
	// Nothing scrolled, yet the anchor moved: a row above it changed height
	// (a channel name or a picture arriving, a plugin's tree). Keep what
	// the reader is looking at where it was, in this frame: lay out again
	// with the offset corrected and draw only that.
	if akey != "" && samePos(s.list.Position, before) {
		if y, ok := s.yOfKey(akey, size.Y); ok && y != ay {
			for i := range s.heights {
				s.heights[i] = 0
			}
			s.list.Position.Offset += y - ay
			call = lay()
		}
	}
	call.Add(gtx.Ops)
	s.laidOut, s.laidPos, s.laidBuilt = true, s.list.Position, s.built
	s.actionsScrollEnd(actD)

	// positions of visible items
	p := s.list.Position
	y := -p.Offset
	s.visible = [2]int{p.First, p.First}
	for i := p.First; i < len(s.items) && y < size.Y; i++ {
		s.pos[i] = y
		y += s.heights[i]
		s.visible[1] = i + 1
	}
	// contentH is the total item height when every item was laid out (the
	// thread pane top-aligns short threads with it), else 0.
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

	// jumping to a message older than what's loaded: page back until found
	// (bounded), then sync() positions on it.
	if s.pendingJump != "" && !s.thread && s.snap.loaded && !s.snap.older {
		if s.snap.hasMore && len(s.snap.msgs) > 0 && tsLess(s.pendingJump, s.snap.msgs[0].m.TS) && s.jumpPages < 20 && now.Sub(s.lastOlder) > 300*time.Millisecond {
			s.jumpPages++
			s.lastOlder = now
			s.hb.loadOlder(s.m)
		} else if !s.snap.hasMore || s.jumpPages >= 20 {
			s.pendingJump = "" // not in this conversation (or too far back)
		}
	}
	s.pendingDateJump(now) // a day pill's "Jump to…" (banners.go)
	// infinite scroll up
	if !s.thread && p.First <= 2 && s.snap.hasMore && !s.snap.older && s.snap.loaded && now.Sub(s.lastOlder) > time.Second {
		s.lastOlder = now
		s.hb.loadOlder(s.m)
	}
	// Mark as read: mark_msgs_read_immediately marks as soon as the
	// conversation shows; Slack's default leaves unseen messages unread,
	// so the newest must be on screen and the "New" line must have been.
	pf := uipref.Get()
	seen := pf.MarkReadImmediately || (!p.BeforeEnd && s.unreadSeen)
	if !s.thread && !s.noMark && s.snap.loaded && seen && len(s.snap.msgs) > 0 {
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
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		// Slack: the reaction button dispatches changeReactionFromUserAction,
		// whose native submits submitAddOrRemoveReaction (plugins may wrap
		// either; with none this is just core.React).
		_, err := taut.Dispatch(ctx, tautOf(), "changeReactionFromUserAction", rxnUserArgs{Key: key, Name: name, Add: &add},
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
			})
		if err != nil {
			h.Toast("Couldn't update reaction")
		}
	}()
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
		if el := rc.now.Sub(s.highlightAt); el < 4*time.Second {
			rc.highlight = 1
			if el > 2*time.Second {
				rc.highlight = float32(4*time.Second-el) / float32(2*time.Second)
			}
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
	d := r.layout(gtx, rc)
	// hover tooltip for reaction pills
	if rc.hovered {
		for k := range r.pills {
			if k < len(r.reactRects) && k < len(r.groups) && r.pills[k].btn.Hovered() {
				s.tipRect = r.reactRects[k] // row-local; positioned in overlays
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

	// hover toolbar (the simplified layout's persistent menu instead)
	s.tbFor = -1
	if rc.persist {
		s.persistentActions(gtx, h, rc, size)
	} else if s.hoverIdx >= 0 && s.hoverIdx < len(s.items) && s.items[s.hoverIdx].kind == itMsg {
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
	td := flowTip(gtx, th, []tipRun{{st: ts, txt: txt}}, dp(gtx, 200)-2*padX)
	call := m.Stop()
	bw, bh := td.X+2*padX, td.Y+2*padY
	pl := theme.PlaceTooltip(gtx, target, image.Pt(bw, bh), bounds, theme.BaseTooltip(gtx, theme.TipTop))
	r := pl.Box
	return tipBox{r: r, arrow: pl.Arrow, text: image.Pt(r.Min.X+(bw-td.X)/2, r.Min.Y+padY), call: call}
}

func (s *listState) toolbar(gtx layout.Context, h Host, rc *rowCtx, r *row, idx int, size image.Point) {
	c := rc.c
	th := h.Theme()
	// MessageActions' position: "above" (top -28) for a grouped row, and
	// for every channel row under Compact (Message: w||a ? "above" :
	// "default"); a thread reply's is above only when grouped
	// (ThreadReplyGeneric: isCompact||adjacent, isCompact being the
	// Threads view's).
	top := mt.toolbarTop
	if !s.items[idx].header || (rc.dense && !s.thread) {
		top = -28
	}
	y := s.pos[idx] + dp(gtx, top)
	if y < dp(gtx, 4) {
		y = dp(gtx, 4)
	}
	s.tbRow, s.tbSize, s.tbHov = r, size, -1
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
	s.tbRect, s.tbFor = image.Rectangle{Min: image.Pt(x, y), Max: image.Pt(x, y).Add(d.Size)}, idx
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
		"isEditing": false, "position": "default", "messageContainerType": "message", "viewContext": r.viewContext(), "windowId": "main"})
}

func (s *listState) reactionButtonProps() slackc.Props {
	return s.paneProps(slackc.Props{"$key": s.tbRow.ts, "aria-label": tbTip(tbAddReact, tbTips[tbAddReact]), "label": tbTip(tbAddReact, tbTips[tbAddReact]),
		"data-qa": tbQA[tbAddReact], "actionId": "add_reaction", "showLabel": false})
}

// layoutActions is MessageActions, the hover toolbar (the one OwlMode
// wraps; Slack's other MessageActions wraps the whole message).
func (s *listState) layoutActions(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	st := actionsClass.Enter(gtx, css.StateHover, 0)
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
			h.ShowOverlay(popups.NewReactionPicker(h, anchor, m.Channel, m.TS, m.ThreadTS, nil))
		case tbReply:
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
			// Slack's menu top sits 32px above the kebab's centre (16 above
			// the button), its right edge 3px left of the centre.
			anchor = anchor.Add(image.Pt(-dp(gtx, 2), -dp(gtx, 32)))
			where := popups.InChannel // messageContainerType: a thread's Mark unread marks the thread
			if s.thread {
				where = popups.InThread
			}
			h.ShowOverlay(popups.NewMessageMenuOpts(h, anchor, m, popups.MessageMenuOpts{
				Delete:  func(ctx context.Context, ch, ts string) error { return deleteMessage(ctx, app, ch, ts) },
				OnEdit:  func() { s.startEdit(h, mm.TS) },
				OnReply: func() { h.OpenThread(mm.Channel, threadRoot(&mm)) },
				Where:   where,
			}))
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
	if st.Active() {
		return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return s.toolbarIcon(gtx, btn, i, todo, st) })
	}
	return s.toolbarIcon(gtx, btn, i, todo, st)
}

// toolbarIcon draws button i's icon; todo is the to-do action's (Yoyn).
// The hover background, the icon's colour and its scale (and Add
// reaction…'s tilt) follow Slack's transitions (toolbar_tilt.go).
func (s *listState) toolbarIcon(gtx layout.Context, btn *widget.Clickable, i int, todo popups.TodoAction, st slackc.Styled) layout.Dimensions {
	c := s.rc.c
	bs := gtx.Constraints.Max.X
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		rr := image.Rectangle{Max: image.Pt(bs, bs)}
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
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
				s.list.Position = layout.Position{} // newest: stay anchored at the bottom
				return
			}
			if i >= s.visible[0] && i < s.visible[1]-1 {
				return
			}
			s.list.Position = layout.Position{BeforeEnd: true, First: max(0, i-2)}
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
	name := func(id string) string {
		if app := h.Core(); app != nil {
			if u, ok := app.User(id); ok && u.Name != "" {
				return u.Name
			}
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

// placeJump puts a pending jump's message near the top of the view
// (two rows above it) once the built items hold it, and flashes it from
// then.
func (s *listState) placeJump(now time.Time) {
	if s.pendingJump == "" {
		return
	}
	for i, it := range s.items {
		if it.kind == itMsg && it.idx < len(s.snap.msgs) && s.snap.msgs[it.idx].m.TS == s.pendingJump {
			s.list.Position = layout.Position{BeforeEnd: true, First: max(0, i-2)}
			s.positioned = true
			s.pendingJump = ""
			s.highlightAt = now
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
