package messages

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/composer"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc/css"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
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
)

type item struct {
	kind   itemKind
	idx    int // index into snap.msgs (itMsg)
	day    time.Time
	header bool
	key    string
	group  []int // join/leave summary: indices into snap.msgs
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
var tbIcons = [tbCount]string{"add-reaction", "reply", "forward-msg", "check", "list-view", "kebab"}

// listState is the per-conversation UI state shared by channel and thread
// views. It is cached across view instances (UI goroutine only), so
// switching back to a conversation keeps its rows and scroll position.
type listState struct {
	hb     *hub
	m      *model
	thread bool
	ch     string
	ts     string // thread ts

	snap   snap
	rows   map[string]*row
	items  []item
	built  uint64 // snap.rev items were built from
	dayKey int    // date the items were built on, yyyymmdd (Today/Yesterday rollover)

	list    layout.List
	heights []int
	pos     []int // y of visible items (valid where heights > 0 this frame)
	visible [2]int

	openLastRead string // the "New" line position, fixed while open
	opened       bool
	positioned   bool
	markedTS     string
	lastMark     time.Time

	pointer   f32.Point
	pointerIn bool
	hoverIdx  int

	tb       [tbCount]widget.Clickable
	tbRect   image.Rectangle
	tbFor    int
	jump     widget.Clickable
	retryBtn widget.Clickable
	closeBtn widget.Clickable

	tip     string
	tipRect image.Rectangle
	tipItem int

	lastTyping time.Time

	noMark      bool
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
	sel         selection
	sb          scrollbar
	pendingJump string

	rc rowCtx // this frame's row inputs (rows keep a pointer to it)

	// hover toolbar state for its natives (MessageActions)
	tbRow  *row
	tbSize image.Point
	tbHov  int // hovered toolbar button, -1 none
}

var listCache = map[string]*listState{}

// lastHost is the Host of the last message list drawn. Host components
// (drawn inside plugin trees, possibly outside any list) reach core and
// the popups through it; nil until a list has been drawn.
var lastHost Host

func stateFor(h Host, ch, ts string) *listState {
	key := ch
	if ts != "" {
		key = ch + "/" + ts
	}
	hb := hubFor(h)
	if s, ok := listCache[key]; ok && s.hb == hb {
		return s
	}
	// a new core (signed in again, another account): fresh state
	s := &listState{hb: hb, ch: ch, ts: ts, thread: ts != "", rows: map[string]*row{}, hoverIdx: -1, tbFor: -1}
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
	changed := s.m.snapshot(&s.snap)
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
	}
	y, mo, d := now.Date() // not Format: this runs every frame
	dk := y*10000 + int(mo)*100 + d
	if !changed && s.built == s.snap.rev && dk == s.dayKey && s.items != nil {
		return
	}
	// Scroll anchoring: remember the first visible message and its y.
	anchorTS, anchorY := "", 0
	if s.list.Position.BeforeEnd && s.positioned {
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
	if anchorTS != "" {
		for i, it := range s.items {
			if it.kind == itMsg && s.snap.msgs[it.idx].m.TS == anchorTS {
				s.list.Position.First = i
				s.list.Position.Offset = -anchorY
				break
			}
		}
	}
	if s.pendingJump != "" {
		for i, it := range s.items {
			if it.kind == itMsg && s.snap.msgs[it.idx].m.TS == s.pendingJump {
				s.list.Position = layout.Position{BeforeEnd: true, First: max(0, i-2)}
				s.positioned = true
				s.pendingJump = ""
				s.highlightAt = now
				break
			}
		}
	}
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

func groupable(a, b *core.Message, ta, tb time.Time) bool {
	if a.System || b.System || a.Subtype != b.Subtype && (a.Subtype == "bot_message" || b.Subtype == "bot_message") {
		return false
	}
	if a.User != b.User || a.BotID != b.BotID || a.Name != b.Name || a.Avatar != b.Avatar {
		return false
	}
	if b.Broadcast || a.Pinned || b.Pinned {
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
	for i := 0; i < len(msgs); i++ {
		m := &msgs[i].m
		t := tsTime(m.TS)
		header := prev == nil
		if s.thread {
			if i == 0 && m.TS == s.ts {
				s.items = append(s.items, item{kind: itMsg, idx: i, header: true, key: m.TS})
				n := len(msgs) - 1
				s.items = append(s.items, item{kind: itReplies, idx: n, key: "replies"})
				prev, prevT = nil, t
				continue
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
	if cap(s.heights) < len(s.items) {
		s.heights = make([]int, len(s.items), len(s.items)+64)
		s.pos = make([]int, len(s.items), len(s.items)+64)
	} else {
		s.heights = s.heights[:len(s.items)]
		s.pos = s.pos[:len(s.items)]
	}
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
			s.selPointer(gtx, h, e, size)
		}
	}
	s.selKeys(gtx, h)
	event.Op(gtx.Ops, s)

	if !s.snap.loaded && len(s.snap.msgs) == 0 {
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

	// first positioning: at the unread line if there are many unread
	if !s.positioned && s.snap.loaded {
		s.positioned = true
		if u := s.unreadIndex(); u >= 0 && len(s.items)-u > 12 {
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

	for i := range s.heights {
		s.heights[i] = 0
	}
	selfID := ""
	if h.Core() != nil {
		selfID = h.Core().Self().ID
	}
	s.rc = rowCtx{h: h, th: th, st: st, c: c, now: now, thread: s.thread, list: s, selfID: selfID, usersRev: s.hb.usersRev() + 1,
		origin:    func(p image.Point) image.Point { return s.anchor(h, origin, p) },
		onReact:   func(r *row, name string, add bool) { s.react(h, r, name, add, selfID) },
		onRetry:   func(r *row) { s.retry(h, r) },
		onDiscard: func(r *row) { s.discard(h, r) },
	}
	rc := &s.rc
	s.origin = origin
	s.tip = ""
	ls := listClass.Enter(gtx, 0, 0)
	defer ls.Leave()
	s.list.Layout(gtx, len(s.items), func(gtx layout.Context, i int) layout.Dimensions {
		d := s.layoutItem(gtx, h, rc, i)
		s.heights[i] = d.Size.Y
		return d
	})

	// positions of visible items
	p := s.list.Position
	y := -p.Offset
	s.visible = [2]int{p.First, p.First}
	for i := p.First; i < len(s.items) && y < size.Y; i++ {
		s.pos[i] = y
		y += s.heights[i]
		s.visible[1] = i + 1
	}

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
	// infinite scroll up
	if !s.thread && p.First <= 2 && s.snap.hasMore && !s.snap.older && s.snap.loaded && now.Sub(s.lastOlder) > time.Second {
		s.lastOlder = now
		s.hb.loadOlder(s.m)
	}
	// mark read at the bottom
	if !s.thread && !s.noMark && s.snap.loaded && !p.BeforeEnd && len(s.snap.msgs) > 0 {
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
				return s.submitReaction(ctx, h, app, a.Key, a.Name, on, self)
			})
		if err != nil {
			h.Toast("Couldn't update reaction")
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
		return s.dayDivider(gtx, h, c, it.day, rc.now, w)
	case itUnread:
		// Slack's divider is 0 tall: a 1px line with "New" centered on it at
		// the right end (padding 0 4, margin-right 15).
		hh := dp(gtx, 1)
		s := textStyle{size: 13, weight: theme.Bold, col: c.NewLine, lineH: 19.5}
		sz := measure(gtx, h.Theme(), s, "New")
		lx := w - dp(gtx, 15) - dp(gtx, 4) - sz.X
		fill(gtx.Ops, image.Rect(0, 0, lx-dp(gtx, 4), hh), c.NewLine)
		at(gtx, lx, -dp(gtx, 10), func(gtx layout.Context) layout.Dimensions { return label(gtx, h.Theme(), s, "New") })
		return layout.Dimensions{Size: image.Pt(w, hh)}
	case itLoading:
		gg := gtx
		gg.Constraints = layout.Exact(image.Pt(w, dp(gtx, 60)))
		return skeleton(gg, c, 0)
	case itIntro:
		return s.intro(gtx, h, c, w)
	case itReplies:
		// 18 tall, margin 4 0, padding-left 16; count then a hairline.
		hh := dp(gtx, 26)
		n := it.idx
		txt := "1 reply"
		if n != 1 {
			txt = itoa(n) + " replies"
		}
		st := textStyle{size: 13, col: c.Muted70, lineH: 18}
		d := at(gtx, dp(gtx, 16), dp(gtx, 4), func(gtx layout.Context) layout.Dimensions { return label(gtx, h.Theme(), st, txt) })
		lx := dp(gtx, 16) + d.Size.X + dp(gtx, 12)
		fill(gtx.Ops, image.Rect(lx, dp(gtx, 13), w-dp(gtx, 16), dp(gtx, 14)), c.PillBorder)
		return layout.Dimensions{Size: image.Pt(w, hh)}
	}
	e := &s.snap.msgs[it.idx]
	if len(it.group) > 0 {
		ge := s.groupEntry(h, it.group)
		e = &ge
	}
	r, ok := s.rows[it.key]
	if !ok {
		r = newRow(e)
		s.rows[it.key] = r
	} else if len(it.group) > 0 && r.rev != e.rev {
		r.set(e)
	}
	if s.viewsOn && len(it.group) == 0 {
		s.applyView(tautOf(), r, rc.now)
	}
	r.rv.External = true
	sa, sb := s.rowSelection(i, r)
	r.rv.SetSelection(sa, sb)
	rc.header = it.header
	rc.hovered = i == s.hoverIdx && r.edit == nil
	rc.winOrigin = s.origin.Add(image.Pt(0, s.pos[i]))
	rc.parent = s.thread && e.m.TS == s.ts
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
	gtx.Constraints.Min.X = w
	d := r.layout(gtx, rc)
	// hover tooltip for reaction pills
	if rc.hovered {
		for k := range r.pills {
			if k < len(r.reactRects) && k < len(r.m.Reactions) && r.pills[k].btn.Hovered() {
				s.tip = reactionTip(h, &r.m.Reactions[k], rc.selfID)
				s.tipRect = r.reactRects[k] // row-local; positioned in overlays
				s.tipItem = i
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

func (s *listState) dayPill(gtx layout.Context, h Host, c *colors, label string, cx, y int) {
	th := h.Theme()
	ts := textStyle{size: 13, weight: theme.Bold, col: c.Text, lineH: 18}
	sz := measure(gtx, th, ts, label)
	ph := dp(gtx, mt.dayPillH)
	pw := dp(gtx, 16) + sz.X + dp(gtx, 4) + dp(gtx, 13) + dp(gtx, 8)
	r := image.Rect(cx-pw/2, y, cx-pw/2+pw, y+ph)
	shadowCard(gtx.Ops, r, ph/2, dp(gtx, 1), c.Bg, c.PillBorder, c.Shadow)
	at(gtx, r.Min.X+dp(gtx, 16), y+(ph-dp(gtx, 18))/2, func(gtx layout.Context) layout.Dimensions {
		return labelText(gtx, th, ts, label)
	})
	at(gtx, r.Min.X+dp(gtx, 16)+sz.X+dp(gtx, 4), y+(ph-dp(gtx, 13))/2, func(gtx layout.Context) layout.Dimensions {
		return icons.Get("chevron-down")(gtx, unit.Dp(13), c.Text)
	})
}

// labelText avoids the shadowed name in dayPill.
func labelText(gtx layout.Context, th *theme.Theme, s textStyle, txt string) layout.Dimensions {
	return label(gtx, th, s, txt)
}

func (s *listState) dayDivider(gtx layout.Context, h Host, c *colors, day, now time.Time, w int) layout.Dimensions {
	hh := dp(gtx, mt.dayH)
	ly := dp(gtx, 19)
	fill(gtx.Ops, image.Rect(0, ly, w, ly+dp(gtx, 1)), c.DayLine)
	s.dayPill(gtx, h, c, dayLabel(day, now), w/2, (hh-dp(gtx, mt.dayPillH))/2)
	return layout.Dimensions{Size: image.Pt(w, hh)}
}

func (s *listState) intro(gtx layout.Context, h Host, c *colors, w int) layout.Dimensions {
	th := h.Theme()
	name := s.ch
	kind := ""
	if app := h.Core(); app != nil {
		if cv, ok := app.Conv(s.ch); ok {
			name, kind = cv.Name, cv.Kind
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
	d = at(gtx, px, y, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Max.X = w - 2*px
		return label(gtx, th, textStyle{size: 15, col: c.Text2, lineH: 22}, sub)
	})
	y += d.Size.Y + dp(gtx, 16)
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
			pillY := dp(gtx, 14)
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
				s.dayPill(gtx, h, c, dayLabel(s.items[d].day, rc.now), size.X/2, pillY)
			}
		}
	}

	// hover toolbar
	s.tbFor = -1
	if s.hoverIdx >= 0 && s.hoverIdx < len(s.items) && s.items[s.hoverIdx].kind == itMsg {
		if r := s.rows[s.items[s.hoverIdx].key]; r != nil && !s.sel.dragging && r.edit == nil && len(s.items[s.hoverIdx].group) == 0 && !r.m.Pending && !r.m.Failed {
			s.toolbar(gtx, h, rc, r, s.hoverIdx, size)
		}
	}

	// reaction tooltip
	if s.tip != "" && s.tipItem >= s.visible[0] && s.tipItem < s.visible[1] {
		r := s.tipRect.Add(image.Pt(0, s.pos[s.tipItem]))
		tooltip(gtx, th, c, s.tip, image.Pt((r.Min.X+r.Max.X)/2, r.Min.Y-dp(gtx, 6)), size)
	}

	// jump to recent
	p := s.list.Position
	if p.BeforeEnd && s.visible[1] < len(s.items)-2 {
		ts := textStyle{size: 13, weight: theme.Bold, col: c.JumpText, lineH: 18}
		txt := "Jump to recent"
		sz := measure(gtx, th, ts, txt)
		bh := dp(gtx, 28)
		bw := dp(gtx, 8) + dp(gtx, 16) + dp(gtx, 4) + sz.X + dp(gtx, 16)
		r := image.Rect((size.X-bw)/2, size.Y-bh-dp(gtx, 16), (size.X-bw)/2+bw, size.Y-dp(gtx, 16))
		if s.jump.Clicked(gtx) {
			s.list.Position = layout.Position{}
			h.Invalidate()
		}
		at(gtx, r.Min.X, r.Min.Y, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(r.Size())
			return s.jump.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				rr := image.Rectangle{Max: r.Size()}
				bg := c.JumpBg
				if s.jump.Hovered() {
					bg.A = 230
				}
				rrect(gtx.Ops, rr, bh/2, bg)
				at(gtx, dp(gtx, 8), (bh-dp(gtx, 16))/2, func(gtx layout.Context) layout.Dimensions {
					return icons.Get("arrow-down")(gtx, unit.Dp(16), c.JumpText)
				})
				at(gtx, dp(gtx, 28), (bh-dp(gtx, 18))/2, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, ts, txt) })
				return layout.Dimensions{Size: rr.Size()}
			})
		})
	}
}

// tooltip draws a dark tooltip centered above anchor (bottom-center).
func tooltip(gtx layout.Context, th *theme.Theme, c *colors, txt string, anchor image.Point, bounds image.Point) {
	ts := textStyle{size: 13, weight: theme.Bold, col: c.TooltipText, lineH: 18}
	maxW := dp(gtx, 260)
	sz := measure(gtx, th, ts, txt)
	tw := sz.X
	if tw > maxW {
		tw = maxW
	}
	pad := dp(gtx, 8)
	bw, bh := tw+2*pad, dp(gtx, 18)+2*dp(gtx, 6)
	x := anchor.X - bw/2
	if x < dp(gtx, 4) {
		x = dp(gtx, 4)
	}
	if x+bw > bounds.X-dp(gtx, 4) {
		x = bounds.X - dp(gtx, 4) - bw
	}
	y := anchor.Y - bh
	if y < 0 {
		y = anchor.Y + dp(gtx, 40)
	}
	r := image.Rect(x, y, x+bw, y+bh)
	rrect(gtx.Ops, r, dp(gtx, 6), c.Tooltip)
	at(gtx, x+pad, y+dp(gtx, 6), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Max.X = tw
		return label(gtx, th, ts, txt)
	})
}

func (s *listState) toolbar(gtx layout.Context, h Host, rc *rowCtx, r *row, idx int, size image.Point) {
	c := rc.c
	th := h.Theme()
	top := mt.toolbarTop
	if !s.items[idx].header {
		top = -28
	}
	y := s.pos[idx] + dp(gtx, top)
	if y < dp(gtx, 4) {
		y = dp(gtx, 4)
	}
	s.tbRow, s.tbSize, s.tbHov = r, size, -1
	// Right-aligned: measure first (a plugin may add to the toolbar).
	rec := op.Record(gtx.Ops)
	d := slackc.Component(gtx, "MessageActions", s, (*listState).actionsProps, (*listState).layoutActions)
	call := rec.Stop()
	x := size.X - dp(gtx, mt.toolbarRight) - d.Size.X
	t := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	s.tbRect, s.tbFor = image.Rectangle{Min: image.Pt(x, y), Max: image.Pt(x, y).Add(d.Size)}, idx
	if i := s.tbHov; i >= 0 {
		bs := dp(gtx, mt.toolbarBtn)
		bx := x + dp(gtx, mt.toolbarPad) + i*bs
		tip := tbTips[i]
		if i == tbTodo && r.src.Saved {
			tip = "Remove from to-dos"
		}
		s.tip = ""
		tooltip(gtx, th, c, tip, image.Pt(bx+bs/2, y-dp(gtx, 4)), size)
	}
}

func (s *listState) actionsProps() slackc.Props {
	r := s.tbRow
	return s.paneProps(slackc.Props{"$key": r.ts, "msg": r.message(), "ts": r.src.TS, "channelId": r.src.Channel, "hover": true,
		"isEditing": false, "position": "default", "messageContainerType": "message", "viewContext": r.viewContext(), "windowId": "main"})
}

func (s *listState) reactionButtonProps() slackc.Props {
	return s.paneProps(slackc.Props{"$key": s.tbRow.ts, "aria-label": tbTips[tbAddReact], "label": tbTips[tbAddReact],
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
	size := image.Pt(pad*2+tbCount*bs, bs+2*pad)
	shadowCard(gtx.Ops, image.Rectangle{Max: size}, dp(gtx, 12), dp(gtx, 1), st.Background(c.Bg), c.PillBorder, c.Shadow)
	for i := 0; i < tbCount; i++ {
		at(gtx, pad+i*bs, pad, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(bs, bs))
			if i == tbAddReact {
				return slackc.Component(gtx, "MessageActionButtonReaction", s, (*listState).reactionButtonProps, (*listState).layoutReactionButton)
			}
			return s.toolbarButton(gtx, i)
		})
	}
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
			s.toggleSaved(h, app, m)
		case tbList:
			h.Toast("Lists aren't supported yet")
		case tbMore:
			mm := m
			h.ShowOverlay(popups.NewMessageMenuOpts(h, anchor, m, popups.MessageMenuOpts{
				Delete:  func(ctx context.Context, ch, ts string) error { return deleteMessage(ctx, app, ch, ts) },
				OnEdit:  func() { s.startEdit(h, mm.TS) },
				OnReply: func() { h.OpenThread(mm.Channel, threadRoot(&mm)) },
			}))
		}
	}
	hov := btn.Hovered()
	if hov {
		s.tbHov = i
	}
	st := actionBtnClass.EnterAttr(gtx, btnState(gtx, btn), 0, "data-qa", tbQA[i])
	defer st.Leave()
	bs := gtx.Constraints.Max.X
	if st.Hidden() {
		return layout.Dimensions{Size: image.Pt(bs, bs)}
	}
	if st.Active() {
		return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return s.toolbarIcon(gtx, btn, i, m.Saved, st) })
	}
	return s.toolbarIcon(gtx, btn, i, m.Saved, st)
}

func (s *listState) toolbarIcon(gtx layout.Context, btn *widget.Clickable, i int, saved bool, st slackc.Styled) layout.Dimensions {
	c := s.rc.c
	bs := gtx.Constraints.Max.X
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		rr := image.Rectangle{Max: image.Pt(bs, bs)}
		col := c.ToolbarIcon
		isz := float32(18)
		if btn.Hovered() {
			rrect(gtx.Ops, rr, dp(gtx, 8), st.Background(c.ToolbarHoverBg))
			col = c.Text
			isz = 21.6 // Slack scales the icon 1.2x on hover
		}
		ic := tbIcons[i]
		if i == tbTodo && saved {
			ic = "check-circle"
			col = c.Link
		}
		col = st.Color(col)
		is := dp(gtx, isz)
		at(gtx, (bs-is)/2, (bs-is)/2, func(gtx layout.Context) layout.Dimensions {
			return icons.Get(ic)(gtx, unit.Dp(isz), col)
		})
		return layout.Dimensions{Size: rr.Size()}
	})
}

func (s *listState) toggleSaved(h Host, app *core.App, m core.Message) {
	saved := m.Saved
	s.m.mutate(m.TS, func(x *core.Message) { x.Saved = !saved }) // optimistic
	if app != nil {
		ch, ts := m.Channel, m.TS
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			var err error
			if saved {
				err = app.Unsave(ctx, ch, ts)
			} else {
				err = app.AddTodo(ctx, ch, ts, 0)
			}
			if err != nil {
				s.m.mutate(ts, func(x *core.Message) { x.Saved = saved })
				h.Toast("Couldn't update your to-dos")
				h.Invalidate()
			}
		}()
	}
	h.Invalidate()
}

func threadRoot(m *core.Message) string {
	if m.ThreadTS != "" {
		return m.ThreadTS
	}
	return m.TS
}

// typingText is Slack's "X is typing…" line.
func typingText(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0] + " is typing…"
	case 2:
		return names[0] + " and " + names[1] + " are typing…"
	}
	return "Several people are typing…"
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
		r = newRow(&s.snap.msgs[i])
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
	r.edit.SaveEdit = func(ctx context.Context, ch, ts, text string, blocks json.RawMessage) error {
		return commitEdit(ctx, app, ch, ts, text, blocks)
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
			if i == len(s.items)-1 {
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
// channel" (names are member links).
func (s *listState) groupEntry(h Host, idx []int) entry {
	msgs := s.snap.msgs
	last := msgs[idx[len(idx)-1]]
	var rev uint32 = uint32(len(idx))
	seen := map[string]bool{}
	var users []string
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
	m := last.m
	m.Reactions = nil
	m.Doc = richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: spans}}}
	return entry{m: m, rev: rev | 1}
}

// closeEdits drops any open inline editor (leaving the conversation).
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
