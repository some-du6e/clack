package views

// Unreads: Slack's All Unreads page (Home sidebar "Unreads", Mod+Shift+A),
// the default experience of page-unreads-view (LlSi renders 2cdE's
// UnreadsView without the catch-up experiment). Model: unreads_model.go.
//
//   - Header (AllUnreadsHeader, boot-render): "Unreads", the section
//     filter (BaseAllUnreadsSectionFilter: "All of {org}" with the team
//     icon, or a section, caret-down; radios for "All of …" and every
//     sidebar section, written to all_unreads_section_filter) and the
//     sort menu (BaseAllUnreadsSortMenu: Sorted like your sidebar /
//     alphabetically / by recommended order / newest to oldest / oldest
//     to newest, written to all_unreads_sort_order), then at the right
//     RefreshUnreadMessagesButton ("N new messages" with the sync icon)
//     while messages arrive in conversations the list doesn't hold
//     (NewMessagesUpdater).
//   - Rows (UnreadsList.renderRow): Header (F9zO) 48px on
//     foreground_min_solid with the collapse caret, the conversation's
//     name (opens it), "N messages" / "N+ messages", "Press Esc to" on
//     the selected header and the small outline "Mark as Read"; marked,
//     a check, "N messages marked read" and Undo; collapsed or with only
//     newer messages, "New messages" / "N new messages". Messages are the
//     channel's rows; "Loading more messages…" pages a frame (fetchPost);
//     ShowNewerButton "N new messages"; the footer's outline "Mark All
//     Messages Read" (BaseFooter).
//   - Empty (JhsS EmptyState): a random en-US line pair with its emoji
//     (the octopus/oyster swaps on a click), "N New Messages" with
//     "Enter ↵ to show" a second after new unreads arrive, and "You
//     marked all your conversations as read. Undo".
//   - Keys (UnreadsList keyCommands, global): Esc marks the selected
//     conversation read and moves to the next header; with the list
//     focused R on a header, ←/→ collapse and expand (→ on a message
//     opens its thread), Shift+PageUp/PageDown move between headers and
//     Enter on the footer marks everything read.
//   - Writes: conversations.mark (Mark, the header and Esc; Undo marks
//     back to the initial cursor), unread.markRead markers (Mark All and
//     its Undo), unread.collapse / unread.expand, users.prefs.set for the
//     sort and the filter.

import (
	"context"
	"image"
	"image/color"
	"math/rand"
	"strings"
	"sync"
	"time"

	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/ctxclick"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// unrLoadAhead is how many rows before the list's end the next page loads.
const unrLoadAhead = 8

// Slack's Unreads copy (all_unreads namespace).
const (
	unrTitle       = "Unreads"
	unrLoadingText = "Loading unread messages…"
	unrLoadingMore = "Loading more messages…"
	unrErrText     = "Something’s fishy! Slack can’t seem to load your unread messages."
	unrMarkAll     = "Mark All Messages Read"
	unrMarkedAll   = "You marked all your conversations as read."
)

// unrSortLabels are BaseAllUnreadsSortMenu's rows outside the Activity
// inbox, in Slack's order.
var unrSortLabels = []struct{ key, label string }{
	{"sidebar", "Sorted like your sidebar"},
	{"alphabetical", "Sorted alphabetically"},
	{"priority", "Sorted by recommended order"},
	{"newest", "Sorted newest to oldest"},
	{"oldest", "Sorted oldest to newest"},
}

// unrMsg is a message row's hover and right-click state.
type unrMsg struct {
	hv  gesture.Hover
	ctx struct{ _ byte }
}

type unrFetched struct {
	f    *unrFrame
	page core.UnreadPage
	err  error
}

// Unreads is the Unreads view.
type Unreads struct {
	base

	frames []*unrFrame
	built  bool
	order  string // the sort and filter the frames were built with
	filter string
	rows   []unrRow
	list   widget.List

	sortBtn, filterBtn, refreshBtn widget.Clickable
	markAll, emptyNew, undoAll     widget.Clickable
	showAll, jump, emojiBtn        widget.Clickable

	marked   []string       // markedChannels (Undo)
	newCount map[string]int // NewMessagesUpdater's unread messages outside the frames
	empty    unrEmpty
	emptyAt  time.Time // when the empty state appeared (the New Messages button waits 1 s)
	wasEmpty bool
	msgs     map[string]*unrMsg

	mu      sync.Mutex
	evq     []core.Event
	fetched []unrFetched
	sub     bool

	focused bool
	active  int       // the keyboard's row (index into rows), -1: none
	kbMode  bool      // the last input was a key: the active row shows the ring
	listH   int       // the list's height last frame
	page    int       // a page scroll waiting for the layout: vkPageFirst / vkPageLast
	top     *unrFrame // the frame whose header is topmost on screen
	notes   actNotes
	rng     *rand.Rand

	emptyLoc string // the locale empty was picked from
}

// NewUnreads builds the Unreads view.
func NewUnreads(h Host) *Unreads {
	v := &Unreads{base: newBase(h), newCount: map[string]int{}, msgs: map[string]*unrMsg{}, active: -1,
		rng: rand.New(rand.NewSource(time.Now().UnixNano()))}
	v.list.Axis = layout.Vertical
	v.pickEmpty()
	return v
}

// pickEmpty is getRandomMessageForLocale: a random entry of the active
// locale's list (JhsS C, unreads_empty_locales.go).
func (v *Unreads) pickEmpty() {
	t := unrEmptyByLocale[i18n.Locale()]
	if len(t) == 0 {
		t = unrEmptyMsgs
	}
	v.empty, v.emptyLoc = t[v.rng.Intn(len(t))], i18n.Locale()
}

// ---- state ----

// sortOrder is getAllUnreadsSortOrder.
func (v *Unreads) sortOrder() string {
	c := v.h.Core()
	p := c.PrefString("all_unreads_sort_order")
	for _, o := range unrSortOrders {
		if p == o {
			return p
		}
	}
	if c.PaidTeam() {
		return "priority"
	}
	return "sidebar"
}

func (v *Unreads) sectionFilter() string {
	f := v.h.Core().PrefString("all_unreads_section_filter")
	if f == unrAllSections {
		return ""
	}
	return f
}

func (v *Unreads) sections() []core.Section {
	return v.h.Core().Snapshot().Sections
}

func (v *Unreads) section(id string) *core.Section {
	for _, s := range v.sections() {
		if s.ID == id {
			s := s
			return &s
		}
	}
	return nil
}

// counted is shouldCountUnreadsInModel: an unread conversation that isn't
// muted, the viewer is in and isn't archived.
func (v *Unreads) counted(id string, cnt core.Count) (core.Conv, bool) {
	cv, ok := v.h.Core().Conv(id)
	if !ok || cnt.Muted || cv.Muted || cv.IsArchived || cv.Preview {
		return cv, false
	}
	return cv, cnt.Unread || cnt.Mentions > 0
}

// build is computeInitialState (and refreshAllUnreads).
func (v *Unreads) build() {
	c := v.h.Core()
	collapsed := map[string]bool{}
	for _, id := range c.UnreadCollapsed() {
		collapsed[id] = true
	}
	var cands []unrCand
	for id, cnt := range c.Counts() {
		cv, ok := v.counted(id, cnt)
		if !ok || cnt.LastRead == "" {
			continue
		}
		latest := cnt.Latest
		if latest == "" || latest == "0000000000.000000" {
			latest = actTS(time.Now().Unix())
		}
		// displayName without the hash (publicChannelIncludeHashSign: false);
		// the header draws the channel icon
		name := v.convTitle(id, nil)
		if cv.Kind == "channel" || cv.Kind == "private" {
			name = strings.TrimPrefix(name, "#")
		}
		cands = append(cands, unrCand{id: id, name: name, kind: cv.Kind, lastRead: cnt.LastRead, latest: latest,
			starred: cv.Starred, priority: cv.Priority, mentions: cnt.Mentions, created: cv.Created})
	}
	// a stable base order before Slack's sorts (Counts is a map)
	sortCands(cands)
	v.order, v.filter = v.sortOrder(), v.sectionFilter()
	secs := v.sections()
	cands = unrSort(cands, v.order, secs)
	if v.filter != "" { // filter_unread_channels
		if s := v.section(v.filter); s != nil {
			in := map[string]bool{}
			for _, id := range append(append([]string(nil), s.All...), s.Channels...) {
				in[id] = true
			}
			var keep []unrCand
			for _, cd := range cands {
				if in[cd.id] {
					keep = append(keep, cd)
				}
			}
			cands = keep
		}
	}
	old := map[string]*unrFrame{}
	for _, f := range v.frames {
		old[f.ch] = f
	}
	v.frames = v.frames[:0]
	for _, cd := range cands {
		f := &unrFrame{ch: cd.id, kind: cd.kind, name: cd.name, start: cd.lastRead, end: cd.latest, initial: cd.lastRead,
			collapsed: collapsed[cd.id], created: cd.created, priority: cd.priority, starred: cd.starred, mentions: cd.mentions}
		if o := old[cd.id]; o != nil {
			f.toggle, f.nameLink, f.markBtn, f.undo, f.newMsgs, f.newer = o.toggle, o.nameLink, o.markBtn, o.undo, o.newMsgs, o.newer
		}
		v.frames = append(v.frames, f)
	}
	v.marked = nil
	v.newCount = map[string]int{}
	v.list.Position = layout.Position{}
	v.active = -1
	v.built = true
}

func sortCands(cs []unrCand) {
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0 && cs[j].id < cs[j-1].id; j-- {
			cs[j], cs[j-1] = cs[j-1], cs[j]
		}
	}
}

func (v *Unreads) frameOf(ch string) *unrFrame {
	for _, f := range v.frames {
		if f.ch == ch && !f.gone {
			return f
		}
	}
	return nil
}

// newUnread is NewMessagesUpdater's count: unread messages that arrived
// outside the frames (in the filtered section only, with a filter).
func (v *Unreads) newUnread() int {
	var in map[string]bool
	if v.filter != "" {
		if s := v.section(v.filter); s != nil {
			in = map[string]bool{}
			for _, id := range append(append([]string(nil), s.All...), s.Channels...) {
				in[id] = true
			}
		}
	}
	n := 0
	for ch, k := range v.newCount {
		if in == nil || in[ch] {
			n += k
		}
	}
	return n
}

// subscribe queues core events for the UI goroutine.
func (v *Unreads) subscribe() {
	if v.sub {
		return
	}
	v.sub = true
	ch, _ := v.h.Core().Subscribe()
	go func() {
		for ev := range ch {
			switch ev.Type {
			case "message", "update", "delete", "prefs", "marked":
				v.mu.Lock()
				v.evq = append(v.evq, ev)
				v.mu.Unlock()
				v.h.Invalidate()
			}
		}
	}()
}

// drain applies queued events and fetch results.
func (v *Unreads) drain() {
	v.mu.Lock()
	evs, got := v.evq, v.fetched
	v.evq, v.fetched = nil, nil
	v.mu.Unlock()
	for _, r := range got {
		f := r.f
		f.loading = false
		if r.err != nil {
			v.notes.add(v.h, nsUnreads.T(unrErrText))
			f.reachedEnd = true // keep what loaded; Slack's fetchPost retries on scroll
			continue
		}
		f.add(r.page.Messages)
		for i := range r.page.Messages {
			if actTSLess(f.sliceEnd, r.page.Messages[i].TS) {
				f.sliceEnd = r.page.Messages[i].TS
			}
		}
		f.reachedEnd = !r.page.HasMore || len(r.page.Messages) == 0
		if f.reachedEnd { // getFramesToRemove's emptyChannels
			newest := ""
			for i := range f.msgs {
				newest = f.msgs[i].TS
			}
			if newest == "" || !actTSLess(f.initial, newest) {
				f.gone = true
			}
		}
	}
	self := v.selfID()
	for _, ev := range evs {
		switch ev.Type {
		case "prefs":
			if v.built && (v.sortOrder() != v.order || v.sectionFilter() != v.filter) {
				v.build() // componentDidUpdate: sort or filter changed
			}
		case "message":
			if ev.Reply || ev.Msg == nil {
				continue
			}
			if f := v.frameOf(ev.Channel); f != nil {
				if f.reachedEnd {
					f.add([]core.Message{*ev.Msg})
				}
				continue
			}
			if ev.Msg.User == self {
				continue
			}
			if _, ok := v.counted(ev.Channel, v.h.Core().Count(ev.Channel)); ok {
				v.newCount[ev.Channel]++
			}
		case "update":
			if f := v.frameOf(ev.Channel); f != nil && ev.Msg != nil {
				for i := range f.msgs {
					if f.msgs[i].TS == ev.Msg.TS {
						f.msgs[i] = *ev.Msg
					}
				}
			}
		case "delete":
			if f := v.frameOf(ev.Channel); f != nil {
				for i := range f.msgs {
					if f.msgs[i].TS == ev.TS {
						f.msgs = append(f.msgs[:i], f.msgs[i+1:]...)
						break
					}
				}
			}
		case "marked":
			if ev.Count != nil && !ev.Count.Unread {
				delete(v.newCount, ev.Channel)
			}
		}
	}
}

// fetch is fetchPost for f: the messages after its slice end.
func (v *Unreads) fetch(f *unrFrame) {
	if f.loading {
		return
	}
	f.loading = true
	c, ch, oldest := v.h.Core(), f.ch, f.sliceEnd
	if oldest == "" {
		oldest = f.start
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		p, err := c.UnreadHistory(ctx, ch, oldest)
		v.mu.Lock()
		v.fetched = append(v.fetched, unrFetched{f: f, page: p, err: err})
		v.mu.Unlock()
		v.h.Invalidate()
	}()
}

func (v *Unreads) run(fail string, f func(ctx context.Context, c *core.App) error) {
	c := v.h.Core()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := f(ctx, c); err != nil {
			v.notes.add(v.h, fail+": "+friendlyErr(err))
		}
	}()
}

// ---- actions (UnreadsView / UnreadsList) ----

// markFrame is markChannelLastRead to the frame's end.
func (v *Unreads) markFrame(f *unrFrame, count int, moveFocus bool) {
	ch, ts := f.ch, f.end
	v.run("Couldn’t mark the conversation as read", func(ctx context.Context, c *core.App) error {
		_, err := c.Mark(ctx, ch, ts)
		return err
	})
	f.start, f.marked = f.end, count
	v.marked = []string{f.ch}
	if moveFocus || v.focused {
		if count > 0 {
			v.focusNextHeader(f)
		} else {
			v.focusHeader(f)
		}
	}
}

// undoFrame is Header.undoMarkRead: back to the initial cursor.
func (v *Unreads) undoFrame(f *unrFrame) {
	ch, ts := f.ch, f.initial
	v.run("Couldn’t mark the conversation as unread", func(ctx context.Context, c *core.App) error {
		_, err := c.Mark(ctx, ch, ts)
		return err
	})
	f.start, f.marked = f.initial, 0
	v.marked = []string{f.ch}
}

// markAllRead is UnreadsList.markAllRead + UnreadsView.markAllRead.
func (v *Unreads) markAllRead() {
	markers := map[string]string{}
	var keys []string
	for _, r := range v.rows {
		if r.kind != unrHeader {
			continue
		}
		if (r.f.reachedEnd && r.count == 0) || r.f.end == "" {
			continue
		}
		markers[r.f.ch] = r.f.end
		keys = append(keys, r.f.ch)
	}
	v.run("Couldn’t mark everything as read", func(ctx context.Context, c *core.App) error { return c.MarkMarkers(ctx, markers) })
	for _, f := range v.frames {
		f.start = f.end
	}
	v.marked = keys
}

// undoMarkAll is UnreadsView.undoMarkAllRead.
func (v *Unreads) undoMarkAll() {
	markers := map[string]string{}
	in := map[string]bool{}
	for _, ch := range v.marked {
		in[ch] = true
	}
	for _, f := range v.frames {
		if !in[f.ch] {
			continue
		}
		f.start = f.initial
		if actTSLess(f.initial, f.end) {
			markers[f.ch] = f.initial
		}
	}
	v.run("Couldn’t undo", func(ctx context.Context, c *core.App) error { return c.MarkMarkers(ctx, markers) })
	v.marked = nil
}

// toggleCollapse is UnreadsList.toggleCollapse.
func (v *Unreads) toggleCollapse(f *unrFrame) {
	f.collapsed = !f.collapsed
	if !f.collapsed {
		f.end = f.latest() // expandChannel + showNewerMessages(e, latest)
	}
	ch, col := f.ch, f.collapsed
	v.run("Couldn’t change the conversation", func(ctx context.Context, c *core.App) error {
		return c.SetUnreadCollapsed(ctx, ch, col)
	})
}

// showNew is Header.showNew: everything up to the latest message, from the
// conversation's current read cursor.
func (v *Unreads) showNew(f *unrFrame) {
	if lr := v.h.Core().Count(f.ch).LastRead; lr != "" {
		f.initial, f.start = lr, lr
	}
	f.end = f.latest()
}

// refresh is refreshAllUnreads.
func (v *Unreads) refresh() { v.build() }

// MarkActiveRead is the view's Esc (onMarkReadKeyCommand): it marks the
// keyboard's or the topmost conversation read and reports whether it
// did.
func (v *Unreads) MarkActiveRead() bool {
	f := v.activeFrame()
	if f == nil {
		return false
	}
	n := 0
	for _, r := range v.rows {
		if r.kind == unrMsgRow && r.f == f {
			n++
		}
	}
	if n == 0 {
		return false // "No messages to mark"
	}
	v.markFrame(f, n, true)
	v.h.Invalidate()
	return true
}

// activeFrame is getActiveRowForKeyboardMarking's frame.
func (v *Unreads) activeFrame() *unrFrame {
	if v.focused && v.active >= 0 && v.active < len(v.rows) && v.rows[v.active].f != nil {
		return v.rows[v.active].f
	}
	if v.top != nil {
		return v.top
	}
	for _, r := range v.rows {
		if r.kind == unrHeader {
			return r.f
		}
	}
	return nil
}

func (v *Unreads) headerRow(f *unrFrame) int {
	for i, r := range v.rows {
		if r.kind == unrHeader && r.f == f {
			return i
		}
	}
	return -1
}

func (v *Unreads) focusHeader(f *unrFrame) {
	if i := v.headerRow(f); i >= 0 {
		v.active = i
		v.scrollTo(i)
	}
}

// focusNextHeader moves to the next frame's header (or stays).
func (v *Unreads) focusNextHeader(f *unrFrame) {
	next := f
	for i, g := range v.frames {
		if g == f {
			for _, h := range v.frames[i+1:] {
				if !h.gone {
					next = h
					break
				}
			}
		}
	}
	v.rows = unrRows(v.frames, time.Now(), v.lastRead)
	v.focusHeader(next)
}

func (v *Unreads) scrollTo(i int) {
	pos := v.list.Position
	if i < pos.First || i >= pos.First+max(1, pos.Count-1) {
		v.list.ScrollTo(i)
	}
}

func (v *Unreads) lastRead(ch string) string { return v.h.Core().Count(ch).LastRead }

// ---- Layout ----

// Layout draws the view.
func (v *Unreads) Layout(gtx C) D {
	v.frame()
	v.notes.drain(v.h)
	v.subscribe()
	if !v.built && v.known() {
		v.build()
	}
	v.drain()
	v.keys(gtx)
	if v.refreshBtn.Clicked(gtx) {
		v.refresh()
	}
	size := gtx.Constraints.Max
	fill(gtx, image.Rectangle{Max: size}, v.p.Bg)
	layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(v.head),
		layout.Flexed(1, v.body),
	)
	return D{Size: size}
}

// known is unreadChannelsAreKnown: core has applied client.counts.
func (v *Unreads) known() bool { return len(v.h.Core().Counts()) > 0 }

func (v *Unreads) dark() bool { return v.th.Dark }

// headBg is .p-unreads_view__header--new's foreground_min_solid.
func (v *Unreads) headBg() color.NRGBA { return v.th.C.BgInset }

// line is foreground_low_solid (#dddddd; dark 13% #e8e8e8).
func (v *Unreads) line() color.NRGBA { return v.lt(rgb(0xdddddd), argb(0x21e8e8e8)) }

// head is the view header: "Unreads", the filter and sort menus, the
// refresh button.
func (v *Unreads) head(gtx C) D {
	w, h := gtx.Constraints.Max.X, gtx.Dp(49)
	fill(gtx, image.Rect(0, 0, w, h), v.p.Bg)
	if v.filterBtn.Clicked(gtx) {
		v.openFilterMenu()
	}
	if v.sortBtn.Clicked(gtx) {
		v.openSortMenu()
	}
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, h))
	layout.Inset{Left: 18, Right: 12}.Layout(g, func(gtx C) D {
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D { return v.para(gtx, 18, 24, wBlack, v.p.Text, 1, nsUnreads.T(unrTitle)) }),
			hgap(8),
			layout.Rigid(func(gtx C) D { return v.control(gtx, &v.filterBtn, v.filterLabel(), true) }),
			hgap(4),
			layout.Rigid(func(gtx C) D { return v.control(gtx, &v.sortBtn, v.sortLabel(), false) }),
			layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Min.X, 0)} }),
			layout.Rigid(v.refreshButton),
		)
	})
	hl := max(1, gtx.Dp(1))
	fill(gtx, image.Rect(0, h-hl, w, h), v.p.Border)
	return D{Size: image.Pt(w, h)}
}

func (v *Unreads) sortLabel() string {
	o := v.sortOrder()
	for _, s := range unrSortLabels {
		if s.key == o {
			return nsUnreads.T(s.label)
		}
	}
	return ""
}

// filterLabel is renderFilterMenuTrigger's text.
func (v *Unreads) filterLabel() string {
	if f := v.sectionFilter(); f != "" {
		if s := v.section(f); s != nil {
			return s.Name
		}
	}
	return v.allLabel()
}

// allLabel: "All of {org}" on an Enterprise org (isEnterprise), else
// "All of {workspace}" / "All conversations".
func (v *Unreads) allLabel() string {
	snap := v.h.Core().Snapshot()
	if n := snap.Team.Name; n != "" {
		return nsUnreads.T("All of {workspace}", "workspace", n)
	}
	return nsUnreads.T("All conversations")
}

// control is .p-unreads_view__list_control_button: 13px content-sec text
// and a 16px caret-down; the filter leads with the 16px team icon.
func (v *Unreads) control(gtx C, c *widget.Clickable, label string, team bool) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		col := v.p.Sec
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D {
				if !team || v.sectionFilter() != "" {
					return D{}
				}
				icon := v.h.Core().Snapshot().Team.Icon
				return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D { return v.avatar(gtx, icon, 16) })
			}),
			layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wReg, col, label) }),
			hgap(4),
			layout.Rigid(func(gtx C) D { return v.icon(gtx, "caret-down", 16, col) }),
		)
	})
}

// refreshButton is RefreshUnreadMessagesButton (ViewHeaderIconButton):
// 13px bold content-sec, the sync icon, the label above 1070px.
func (v *Unreads) refreshButton(gtx C) D {
	n := v.newUnread()
	if n < 1 {
		return D{}
	}
	label := nsUnreads.T("{newUnreadMsgCount, plural, =1 {# new message} other {# new messages}}", "newUnreadMsgCount", n)
	wide := gtx.Constraints.Max.X > gtx.Dp(1070)
	return v.refreshBtn.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		m := op.Record(gtx.Ops)
		d := layout.UniformInset(8).Layout(gtx, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D { return v.icon(gtx, "sync", 18, v.p.Sec) }),
				layout.Rigid(func(gtx C) D {
					if !wide {
						return D{}
					}
					return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return v.label(gtx, 13, wBold, v.p.Sec, label) })
				}),
			)
		})
		call := m.Stop()
		if v.refreshBtn.Hovered() {
			rrect(gtx, image.Rectangle{Max: d.Size}, gtx.Dp(4), v.p.Hover)
		}
		call.Add(gtx.Ops)
		return d
	})
}

func (v *Unreads) menuAt() image.Rectangle {
	var at image.Point
	if pp, ok := v.h.(interface{ PointerPos() image.Point }); ok {
		at = pp.PointerPos()
	}
	return image.Rectangle{Min: at, Max: at.Add(image.Pt(1, 1))}
}

// openFilterMenu is the section filter's menu: "All of …", a
// separator, then the sidebar's sections as radios.
func (v *Unreads) openFilterMenu() {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	cur := v.sectionFilter()
	set := func(id string) func() {
		return func() {
			v.run("Couldn’t change the filter", func(ctx context.Context, c *core.App) error {
				return c.SetPref(ctx, "all_unreads_section_filter", id)
			})
		}
	}
	items := []popups.MenuItem{{Key: unrAllSections, Label: v.allLabel(), Radio: true, Checked: cur == "", Do: set(unrAllSections)}, popups.Sep()}
	for _, s := range v.sections() {
		switch s.Type {
		case "salesforce_records", "canvases_and_files", "agent_sessions":
			continue
		case "stars":
			if len(s.All) == 0 && len(s.Channels) == 0 {
				continue
			}
		}
		items = append(items, popups.MenuItem{Key: s.ID, Label: s.Name, Radio: true, Checked: cur == s.ID, Do: set(s.ID)})
	}
	ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: v.menuAt(), Width: 260}))
}

// openSortMenu is BaseAllUnreadsSortMenu: checkbox rows.
func (v *Unreads) openSortMenu() {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	cur := v.sortOrder()
	var items []popups.MenuItem
	for _, s := range unrSortLabels {
		key := s.key
		items = append(items, popups.MenuItem{Key: key, Label: nsUnreads.T(s.label), Checked: cur == key, Do: func() {
			v.run("Couldn’t change the sort", func(ctx context.Context, c *core.App) error {
				return c.SetPref(ctx, "all_unreads_sort_order", key)
			})
		}})
	}
	ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: v.menuAt(), Width: 260}))
}

// body is renderChannelContent.
func (v *Unreads) body(gtx C) D {
	if !v.built {
		return layout.Center.Layout(gtx, func(gtx C) D { return v.label(gtx, 15, wReg, v.p.Muted, nsUnreads.T(unrLoadingText)) })
	}
	v.rows = unrRows(v.frames, gtx.Now, v.lastRead)
	if !unrAnyUnread(v.frames) {
		if !v.wasEmpty {
			v.wasEmpty, v.emptyAt = true, gtx.Now
			v.pickEmpty()
		}
		if v.emptyLoc != i18n.Locale() {
			v.pickEmpty() // the new locale's list
		}
		return v.emptyState(gtx)
	}
	v.wasEmpty = false
	if v.markAll.Clicked(gtx) {
		v.markAllRead()
	}
	// a press anywhere on the list focuses it (UnreadsList's keyboard);
	// its area goes over the rows (below), letting presses through
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: v, Kinds: pointer.Press})
		if !ok {
			break
		}
		if _, ok := ev.(pointer.Event); ok {
			v.kbMode = false
			gtx.Execute(key.FocusCmd{Tag: v})
		}
	}
	d := v.scrollList(gtx, &v.list, len(v.rows), func(gtx C, i int) D { return v.rowAt(gtx, i) })
	v.listH = d.Size.Y
	func() {
		defer clip.Rect{Max: d.Size}.Push(gtx.Ops).Pop()
		defer pointer.PassOp{}.Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, v)
	}()
	v.afterPage()
	// loadPost: the virtual list asks for more once its end is near
	if n := len(v.rows); n > 0 && v.rows[n-1].kind == unrLoading {
		if p := v.list.Position; p.First+p.Count >= n-unrLoadAhead {
			v.fetch(v.rows[n-1].f)
		}
	}
	v.top = nil
	if p := v.list.Position; p.First < len(v.rows) {
		r := v.rows[p.First]
		v.top = r.f
		if r.f != nil && r.kind != unrHeader && (r.kind != unrFooter) {
			// the frame's header sticks to the top (stickyItems)
			v.header(gtx, r.f, v.headerCount(r.f))
		}
	}
	return d
}

func (v *Unreads) headerCount(f *unrFrame) int {
	for _, r := range v.rows {
		if r.kind == unrHeader && r.f == f {
			return r.count
		}
	}
	return 0
}

func (v *Unreads) rowAt(gtx C, i int) D {
	r := v.rows[i]
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	w := gtx.Constraints.Max.X
	d := v.drawRow(gtx, r, i)
	if v.focused && v.kbMode && i == v.active && r.kind != unrHeader {
		// .c-virtual_list__item--focus:focus:after: inset 4px, radius 8
		in := gtx.Dp(4)
		v.vkRing(gtx, image.Rect(in, in, d.Size.X-in, d.Size.Y-in), gtx.Dp(8))
	}
	_ = w
	return d
}

func (v *Unreads) drawRow(gtx C, r unrRow, i int) D {
	w := gtx.Constraints.Max.X
	switch r.kind {
	case unrHeader:
		return v.header(gtx, r.f, r.count)
	case unrSpacerTop:
		return D{Size: image.Pt(w, gtx.Dp(20))}
	case unrSpacerBottom:
		h := gtx.Dp(20)
		hl := max(1, gtx.Dp(1))
		fill(gtx, image.Rect(0, h, w, h+hl), v.line())
		return D{Size: image.Pt(w, h+hl)}
	case unrDay:
		return v.dayDivider(gtx, r.day, r.unread)
	case unrUnreadLine:
		h := gtx.Dp(20)
		defer op.Offset(image.Pt(0, h/2)).Push(gtx.Ops).Pop()
		v.newLine(gtx, w)
		return D{Size: image.Pt(w, h/2)}
	case unrMsgRow:
		return v.message(gtx, r)
	case unrLoading:
		v.fetch(r.f)
		h := gtx.Dp(80)
		g := gtx
		g.Constraints = layout.Exact(image.Pt(w, h))
		layout.Center.Layout(g, func(gtx C) D { return v.label(gtx, 15, wReg, v.p.Muted, nsUnreads.T(unrLoadingMore)) })
		return D{Size: image.Pt(w, h)}
	case unrShowNewer:
		f := r.f
		if f.newer.Clicked(gtx) {
			f.end = f.latest()
		}
		return f.newer.Layout(gtx, func(gtx C) D {
			pointerCursor(gtx)
			d := layout.Inset{Top: 8, Bottom: 8, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return v.para(gtx, 15, 22, wReg, v.p.Link, 1, nsUnreads.T("{count, plural, =1 {# new message} other {# new messages}}", "count", r.count)) // ShowNewerButton
			})
			hl := max(1, gtx.Dp(1))
			fill(gtx, image.Rect(0, d.Size.Y, w, d.Size.Y+hl), v.line())
			return D{Size: image.Pt(w, d.Size.Y+hl)}
		})
	case unrFooter:
		return layout.Inset{Top: 32, Bottom: 32}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					gtx.Constraints.Min.X = 0
					return v.button(gtx, &v.markAll, btnOutline, false, nsUnreads.T(unrMarkAll))
				}),
				layout.Rigid(func(gtx C) D { return v.jumpButton(gtx, 12) }),
			)
		})
	}
	return D{}
}

// header is UnreadsList's Header row.
func (v *Unreads) header(gtx C, f *unrFrame, count int) D {
	after := f.after()
	st := f.headerState(count, after)
	if f.toggle.Clicked(gtx) {
		v.toggleCollapse(f)
	}
	if f.nameLink.Clicked(gtx) {
		v.h.Navigate("c/" + f.ch)
	}
	if f.markBtn.Clicked(gtx) {
		v.markFrame(f, count, false)
	}
	if f.undo.Clicked(gtx) {
		v.undoFrame(f)
	}
	if f.newMsgs.Clicked(gtx) {
		if st == 1 {
			v.toggleCollapse(f)
		} else {
			v.showNew(f)
		}
	}
	w, h := gtx.Constraints.Max.X, gtx.Dp(48)
	marked := st == 2 || st == 3
	bg, fg := v.headBg(), v.p.Text
	if marked {
		bg, fg = v.p.Bg, v.p.Muted
	}
	fill(gtx, image.Rect(0, 0, w, h), bg)
	if marked || st == 1 {
		fill(gtx, image.Rect(0, h-max(1, gtx.Dp(1)), w, h), v.line())
	}
	selected := v.activeFrame() == f
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, h))
	layout.Inset{Left: 16, Right: 32}.Layout(g, func(gtx C) D {
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D {
				box := image.Pt(gtx.Dp(20), gtx.Dp(28))
				if marked {
					return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D {
						return fixed(gtx, box, func(gtx C) D {
							return layout.Center.Layout(gtx, func(gtx C) D { return v.icon(gtx, "check-small", 16, fg) })
						})
					})
				}
				icon := "caret-down-filled"
				if f.collapsed {
					icon = "caret-right-filled"
				}
				return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D {
					return f.toggle.Layout(gtx, func(gtx C) D {
						pointerCursor(gtx)
						return fixed(gtx, box, func(gtx C) D {
							return layout.Center.Layout(gtx, func(gtx C) D { return v.icon(gtx, icon, 16, fg) })
						})
					})
				})
			}),
			layout.Rigid(func(gtx C) D {
				gtx.Constraints.Min.X = 0
				gtx.Constraints.Max.X = max(0, gtx.Constraints.Max.X-gtx.Dp(260)) // leave the controls room
				return f.nameLink.Layout(gtx, func(gtx C) D {
					pointerCursor(gtx)
					d := row(gtx, layout.Middle,
						layout.Rigid(func(gtx C) D {
							ic := v.convIcon(f.kind)
							if ic == "" {
								return D{}
							}
							return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D { return v.icon(gtx, ic, 15, fg) })
						}),
						layout.Rigid(func(gtx C) D { return v.label(gtx, 15, wBlack, fg, f.name) }),
					)
					if f.nameLink.Hovered() {
						fill(gtx, image.Rect(0, d.Size.Y-gtx.Dp(3), d.Size.X, d.Size.Y-gtx.Dp(2)), fg)
					}
					return d
				})
			}),
			// the count's margin-left: auto pushes the rest to the right
			layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Max.X, 0)} }),
			layout.Rigid(func(gtx C) D {
				switch st {
				case 2:
					return row(gtx, layout.Middle,
						layout.Rigid(func(gtx C) D {
							return layout.Inset{Left: 4, Right: 8}.Layout(gtx, func(gtx C) D {
								return v.label(gtx, 13, wReg, fg, nsUnreads.T("{messageCount, plural, =1 {# message} other {# messages}} marked read", "messageCount", f.marked))
							})
						}),
						layout.Rigid(func(gtx C) D { return v.linkButton(gtx, &f.undo, 13, nsUnreads.T("Undo")) }),
					)
				case 1, 3:
					return v.newMsgsButton(gtx, f, count+after)
				}
				// HeaderDefault: "<span …>Press <key>Esc</key> to </span>
				// <button>Mark as Read</button>", its parts where the
				// locale puts them (the span only while selected)
				ch := []layout.FlexChild{layout.Rigid(func(gtx C) D {
					return layout.Inset{Left: 4, Right: 8}.Layout(gtx, func(gtx C) D {
						return v.label(gtx, 13, wReg, fg, unrMsgCount(count, f.reachedEnd))
					})
				})}
				for _, sg := range tagSegs(nsUnreads.T(`<span className="p-unreads_view__header__kb_instructions">Press <key>Esc</key> to </span><button>Mark as Read</button>`)) {
					sg := sg
					switch sg.tag {
					case "button":
						ch = append(ch, layout.Rigid(func(gtx C) D {
							return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D {
								return v.button(gtx, &f.markBtn, btnOutline, true, sg.text)
							})
						}))
					case "key":
						if selected {
							ch = append(ch, layout.Rigid(func(gtx C) D { return v.keycap(gtx, sg.text) }))
						}
					default:
						if selected && sg.text != "" {
							ch = append(ch, layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wReg, fg, sg.text) }))
						}
					}
				}
				return row(gtx, layout.Middle, ch...)
			}),
		)
	})
	if v.focused && v.active >= 0 && v.active < len(v.rows) && v.rows[v.active].kind == unrHeader && v.rows[v.active].f == f {
		border(gtx, image.Rect(0, 0, w, h), gtx.Dp(4), max(1, gtx.Dp(2)), v.p.Focus)
	}
	return D{Size: image.Pt(w, h)}
}

// newMsgsButton is renderNewMsgs: the label and a circle-fill dot.
func (v *Unreads) newMsgsButton(gtx C, f *unrFrame, n int) D {
	exact := f.reachedEnd
	return f.newMsgs.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wReg, v.p.Link, unrNewText(n, exact)+" ") }),
				layout.Rigid(func(gtx C) D { return v.icon(gtx, "circle-fill", 13, v.p.Link) }),
			)
		})
	})
}

// keycap is the slim KeyboardKey ("Esc").
func (v *Unreads) keycap(gtx C, s string) D {
	m := op.Record(gtx.Ops)
	d := layout.Inset{Left: 4, Right: 4}.Layout(gtx, func(gtx C) D { return v.label(gtx, 12, wReg, v.p.Text, s) })
	call := m.Stop()
	r := image.Rectangle{Max: d.Size}
	rrect(gtx, r, gtx.Dp(4), v.p.Bg)
	border(gtx, r, gtx.Dp(4), max(1, gtx.Dp(1)), v.p.BorderStr)
	call.Add(gtx.Ops)
	return d
}

func (v *Unreads) convIcon(kind string) string {
	switch kind {
	case "private":
		return "lock"
	case "im", "mpim", "app":
		return ""
	}
	return "hash"
}

// dayDivider is the channel list's day divider; on the first unread
// message the line turns red with "New".
func (v *Unreads) dayDivider(gtx C, day time.Time, unread bool) D {
	w, h := gtx.Constraints.Max.X, gtx.Dp(40)
	ly := gtx.Dp(19)
	if unread {
		func() {
			defer op.Offset(image.Pt(0, ly)).Push(gtx.Ops).Pop()
			v.newLine(gtx, w)
		}()
	} else {
		fill(gtx, image.Rect(0, ly, w, ly+max(1, gtx.Dp(1))), v.line())
	}
	label := unrDayLabel(day, gtx.Now)
	tw := v.th.Measure(gtx, 13, wBold, label)
	ph := gtx.Dp(28)
	pw := gtx.Dp(16) + tw + gtx.Dp(4) + gtx.Dp(13) + gtx.Dp(8)
	r := image.Rect(w/2-pw/2, (h-ph)/2, w/2-pw/2+pw, (h-ph)/2+ph)
	rrect(gtx, r, ph/2, v.p.Bg)
	border(gtx, r, ph/2, max(1, gtx.Dp(1)), v.p.Border)
	func() {
		defer op.Offset(image.Pt(r.Min.X+gtx.Dp(16), r.Min.Y+(ph-gtx.Dp(18))/2)).Push(gtx.Ops).Pop()
		v.para(gtx, 13, 18, wBold, v.p.Text, 1, label)
	}()
	func() {
		defer op.Offset(image.Pt(r.Min.X+gtx.Dp(16)+tw+gtx.Dp(4), r.Min.Y+(ph-gtx.Dp(13))/2)).Push(gtx.Ops).Pop()
		v.icon(gtx, "chevron-down", 13, v.p.Text)
	}()
	return D{Size: image.Pt(w, h)}
}

// unrDayLabel is the day divider's text: Today, Yesterday, "Friday,
// November 6th", with the year when it isn't this year.
func unrDayLabel(t, now time.Time) string {
	if l := i18n.Locale(); l != i18n.EnUS {
		return i18n.DayDivider(l, t, now, false) // formatDateDayDivider
	}
	if sameDay(t, now) {
		return "Today"
	}
	if sameDay(t, now.AddDate(0, 0, -1)) {
		return "Yesterday"
	}
	s := t.Format("Monday, January 2") + ordinal(t.Day())
	if t.Year() != now.Year() {
		s += t.Format(", 2006")
	}
	return s
}

// newLine is the message list's unread divider: a 1px raspberry line with
// a bold 13px "New" at its right end.
func (v *Unreads) newLine(gtx C, w int) {
	red := v.th.C.NewLine
	newTxt := nsMessages.T("New") // UnreadDivider's
	tw := v.th.Measure(gtx, 13, wBold, newTxt)
	lx := w - gtx.Dp(15) - gtx.Dp(4) - tw
	fill(gtx, image.Rect(0, 0, lx-gtx.Dp(4), max(1, gtx.Dp(1))), red)
	defer op.Offset(image.Pt(lx, -gtx.Dp(9))).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints.Min = image.Point{}
	v.para(g, 13, 19.5, wBold, red, 1, newTxt)
}

// message is a channel message row: a sender row (36px avatar at 20,8,
// 15px 900 name, 12px time 8px after it, the body; at least 52 tall) or
// an adjacent row (the body at x 64, the time in the gutter on hover).
func (v *Unreads) message(gtx C, r unrRow) D {
	m := r.msg
	key := m.Channel + "/" + m.TS
	s := v.msgs[key]
	if s == nil {
		s = new(unrMsg)
		v.msgs[key] = s
	}
	hovered := s.hv.Update(gtx.Source)
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &s.ctx, Kinds: pointer.Press})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok && ctxclick.Press(e) {
			v.messageMenu(*m)
		}
	}
	w := gtx.Constraints.Max.X
	name, av := v.authorOf(m)
	stamp := clock(tsTime(m.TS))
	rec := op.Record(gtx.Ops)
	cx := gtx.Dp(64)
	g := gtx
	g.Constraints.Min.X = 0
	g.Constraints.Max.X = max(0, w-cx-gtx.Dp(20))
	colRec := op.Record(gtx.Ops)
	cd := col(g,
		layout.Rigid(func(gtx C) D {
			if !r.header {
				return D{}
			}
			return row(gtx, layout.Baseline,
				layout.Rigid(func(gtx C) D { return v.para(gtx, 15, 22, wBlack, v.p.Text, 1, name) }),
				hgap(8),
				layout.Rigid(func(gtx C) D { return v.label(gtx, 12, wReg, v.p.Meta, stamp) }),
			)
		}),
		layout.Rigid(func(gtx C) D { return v.msgBody(gtx, m, false, 0) }),
		layout.Rigid(func(gtx C) D {
			if len(m.Reactions) == 0 {
				return D{}
			}
			return layout.Inset{Top: 4}.Layout(gtx, func(gtx C) D { return v.actReactions(gtx, m) })
		}),
	)
	colCall := colRec.Stop()
	hgt := gtx.Dp(4) + cd.Size.Y + gtx.Dp(4)
	if r.header {
		hgt = max(hgt, gtx.Dp(52))
	}
	func() {
		defer op.Offset(image.Pt(cx, gtx.Dp(4))).Push(gtx.Ops).Pop()
		colCall.Add(gtx.Ops)
	}()
	if r.header {
		func() {
			defer op.Offset(image.Pt(gtx.Dp(20), gtx.Dp(8))).Push(gtx.Ops).Pop()
			v.avatar(gtx, av, 36)
		}()
	} else if hovered {
		// the compact time, right-aligned in the 36px gutter
		tw := v.th.Measure(gtx, 12, wReg, stamp)
		func() {
			defer op.Offset(image.Pt(gtx.Dp(20)+gtx.Dp(36)-tw, gtx.Dp(4)+gtx.Dp(4))).Push(gtx.Ops).Pop()
			v.label(gtx, 12, wReg, v.p.Meta, stamp)
		}()
	}
	call := rec.Stop()
	rr := image.Rect(0, 0, w, hgt)
	if hovered {
		bg := v.p.ReadFill
		if v.dark() {
			bg = v.actHover(v.p.Bg)
		}
		fill(gtx, rr, bg)
	}
	call.Add(gtx.Ops)
	st := clip.Rect(rr).Push(gtx.Ops)
	s.hv.Add(gtx.Ops)
	pass := pointer.PassOp{}.Push(gtx.Ops)
	event.Op(gtx.Ops, &s.ctx)
	pass.Pop()
	st.Pop()
	return D{Size: rr.Size()}
}

// messageMenu opens the message's context menu at the pointer.
func (v *Unreads) messageMenu(m core.Message) {
	ph, ok := v.h.(popups.Host)
	if !ok {
		v.h.OpenMessageMenu(image.Point{}, m)
		return
	}
	at := v.menuAt().Min
	ph.ShowOverlay(popups.NewMessageContextMenu(ph, at, m, popups.InChannel, popups.MessageMenuOpts{}))
}

// jumpButton is JumpToSectionButton under a section filter: "Jump to the
// {section} section", the next section in sidebar order.
func (v *Unreads) jumpButton(gtx C, top unit.Dp) D {
	cur := v.sectionFilter()
	if cur == "" {
		return D{}
	}
	secs := v.sections()
	if len(secs) == 0 {
		return D{}
	}
	idx := -1
	for i, s := range secs {
		if s.ID == cur {
			idx = i
		}
	}
	next := secs[(idx+1)%len(secs)]
	if v.jump.Clicked(gtx) {
		id := next.ID
		v.run("Couldn’t change the filter", func(ctx context.Context, c *core.App) error {
			return c.SetPref(ctx, "all_unreads_section_filter", id)
		})
	}
	return layout.Inset{Top: top}.Layout(gtx, func(gtx C) D {
		return v.linkButton(gtx, &v.jump, 14, nsUnreads.T("Jump to the {section} section", "section", next.Name))
	})
}

// emptyState is JhsS EmptyState.
func (v *Unreads) emptyState(gtx C) D {
	if v.emptyNew.Clicked(gtx) {
		v.refresh()
	}
	if v.undoAll.Clicked(gtx) {
		v.undoMarkAll()
	}
	if v.emojiBtn.Clicked(gtx) {
		if e, ok := unrSwap(v.empty); ok {
			v.empty = e
		}
	}
	if v.showAll.Clicked(gtx) {
		v.run("Couldn’t change the filter", func(ctx context.Context, c *core.App) error {
			return c.SetPref(ctx, "all_unreads_section_filter", unrAllSections)
		})
	}
	n := v.newUnread()
	allow := !v.emptyAt.IsZero() && gtx.Now.Sub(v.emptyAt) >= time.Second
	if !allow && n > 0 {
		gtx.Execute(op.InvalidateCmd{At: v.emptyAt.Add(time.Second)})
	}
	showNew := allow && n > 0
	size := gtx.Constraints.Max
	filtered := v.sectionFilter() != ""
	layout.Center.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min = image.Point{}
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Bottom: 12}.Layout(gtx, func(gtx C) D { return v.emptyEmoji(gtx) })
			}),
			layout.Rigid(func(gtx C) D {
				col := v.p.Text
				if showNew {
					col.A = col.A / 2 // .p-unreads_view__empty--show_new .._message { opacity: .5 }
				}
				ts := theme.TextStyle{Size: 22, LineHeight: 30, Weight: wBlack, Color: col, Align: text.Middle}
				if filtered {
					name := v.filterLabel()
					return v.th.Layout(gtx, ts, "You're caught up on "+name+"!")
				}
				lines := v.empty.one
				if v.empty.two != "" {
					lines += "\n" + v.empty.two
				}
				return v.th.Layout(gtx, ts, lines)
			}),
			layout.Rigid(func(gtx C) D {
				if !filtered {
					return D{}
				}
				return layout.Inset{Top: 16}.Layout(gtx, func(gtx C) D {
					return v.button(gtx, &v.showAll, btnOutline, false, nsUnreads.T("Show All Unreads"))
				})
			}),
			layout.Rigid(func(gtx C) D {
				if !filtered {
					return D{}
				}
				return v.jumpButton(gtx, 8)
			}),
			layout.Rigid(func(gtx C) D {
				if !showNew {
					return D{}
				}
				label := strings.TrimSpace(nsUnreads.T("{totalUnreads, plural, =1 {# New Message} other {# New Messages} }", "totalUnreads", n))
				return layout.Inset{Top: 20}.Layout(gtx, func(gtx C) D { return v.button(gtx, &v.emptyNew, btnPrimary, false, label) })
			}),
			layout.Rigid(func(gtx C) D {
				if !showNew {
					return D{}
				}
				src := "<b>Enter</b> <enter-icon></enter-icon> to show"
				if shortcut.GOOS == "darwin" {
					src = "<b>Return</b> <enter-icon></enter-icon> to show"
				}
				caption := rgb(0x868686)
				var ch []layout.FlexChild
				for _, sg := range tagSegs(nsUnreads.T(src)) {
					sg := sg
					switch {
					case sg.tag == "enter-icon":
						ch = append(ch, layout.Rigid(func(gtx C) D {
							return layout.Inset{Left: 4, Right: 4}.Layout(gtx, func(gtx C) D { return v.icon(gtx, "enter", 13, caption) })
						}))
					case sg.tag == "b":
						ch = append(ch, layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wBold, caption, sg.text) }))
					case strings.TrimSpace(sg.text) != "":
						ch = append(ch, layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wReg, caption, strings.TrimSpace(sg.text)) }))
					}
				}
				return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D { return row(gtx, layout.Middle, ch...) })
			}),
		)
	})
	if len(v.marked) > 0 { // p-unreads_view__empty__undo at the bottom
		m := op.Record(gtx.Ops)
		g := gtx
		g.Constraints.Min = image.Point{}
		d := layout.UniformInset(10).Layout(g, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D { return v.label(gtx, 15, wReg, v.p.Muted, nsUnreads.T(unrMarkedAll)) }),
				hgap(8),
				layout.Rigid(func(gtx C) D { return v.underlined(gtx, &v.undoAll, nsUnreads.T("Undo")) }),
			)
		})
		call := m.Stop()
		func() {
			defer op.Offset(image.Pt((size.X-d.Size.X)/2, size.Y-d.Size.Y)).Push(gtx.Ops).Pop()
			call.Add(gtx.Ops)
		}()
	}
	return D{Size: size}
}

// underlined is .p-unreads_view__empty__undo_button: link blue, always
// underlined.
func (v *Unreads) underlined(gtx C, c *widget.Clickable, s string) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		d := v.label(gtx, 15, wReg, v.p.Link, s)
		fill(gtx, image.Rect(0, d.Size.Y-gtx.Dp(2), d.Size.X, d.Size.Y-gtx.Dp(1)), v.p.Link)
		return d
	})
}

// emptyEmoji draws the message's emoji (with the viewer's skin tone);
// the octopus and the oyster are a button that swaps them.
func (v *Unreads) emptyEmoji(gtx C) D {
	c := v.h.Core()
	name := v.empty.emoji
	if v.sectionFilter() != "" {
		if s := v.section(v.sectionFilter()); s != nil && s.EmojiURL != "" {
			return v.emojiImg(gtx, s.EmojiURL, 32)
		}
	}
	draw := func(gtx C) D {
		if unrSkinToned[name] || i18n.Locale() != i18n.EnUS { // the locale lists' own emoji: the tone a variant exists for
			if t := c.PrefString("preferred_skin_tone"); t != "" && t != "1" {
				if u, _, ok := c.Emoji(name + "::skin-tone-" + t); ok && u != "" {
					return v.emojiImg(gtx, u, 32)
				}
			}
		}
		if u, _, ok := c.Emoji(name); ok && u != "" {
			return v.emojiImg(gtx, u, 32)
		}
		return D{Size: image.Pt(gtx.Dp(32), gtx.Dp(32))}
	}
	if _, ok := unrSwap(v.empty); ok {
		return v.emojiBtn.Layout(gtx, func(gtx C) D {
			pointerCursor(gtx)
			return draw(gtx)
		})
	}
	return draw(gtx)
}

// ---- keyboard (UnreadsList keyCommands) ----

func (v *Unreads) keys(gtx C) {
	for {
		ev, ok := gtx.Event(
			key.FocusFilter{Target: v},
			key.Filter{Focus: v, Name: key.NameUpArrow},
			key.Filter{Focus: v, Name: key.NameDownArrow},
			key.Filter{Focus: v, Name: key.NameLeftArrow},
			key.Filter{Focus: v, Name: key.NameRightArrow},
			key.Filter{Focus: v, Name: key.NamePageUp, Optional: key.ModShift},
			key.Filter{Focus: v, Name: key.NamePageDown, Optional: key.ModShift},
			key.Filter{Focus: v, Name: key.NameHome},
			key.Filter{Focus: v, Name: key.NameEnd},
			key.Filter{Focus: v, Name: "R"},
			key.Filter{Focus: v, Name: key.NameReturn},
			key.Filter{Focus: v, Name: key.NameEnter},
		)
		if !ok {
			return
		}
		switch e := ev.(type) {
		case key.FocusEvent:
			v.focused = e.Focus
			if e.Focus && v.active < 0 {
				for i, r := range v.rows {
					if r.kind == unrHeader {
						v.active = i
						break
					}
				}
			}
		case key.Event:
			if e.State != key.Press && !(e.State == key.Release && (e.Name == key.NameReturn || e.Name == key.NameEnter)) {
				continue
			}
			v.kbMode = true
			v.key(e)
		}
	}
}

// focusable is UnreadsList.isFocusableItem.
func unrFocusable(k unrRowKind) bool {
	switch k {
	case unrHeader, unrMsgRow, unrShowNewer, unrFooter, unrDay, unrUnreadLine:
		return true
	}
	return false
}

func (v *Unreads) key(e key.Event) {
	if len(v.rows) == 0 {
		return
	}
	if v.active >= len(v.rows) {
		v.active = len(v.rows) - 1
	}
	cur := unrRow{}
	if v.active >= 0 {
		cur = v.rows[v.active]
	}
	step := func(d int) {
		for i := v.active + d; i >= 0 && i < len(v.rows); i += d {
			if unrFocusable(v.rows[i].kind) {
				v.active = i
				v.scrollTo(i)
				return
			}
		}
	}
	switch e.Name {
	case key.NameUpArrow:
		if e.State == key.Press {
			step(-1)
		}
	case key.NameDownArrow:
		if e.State == key.Press {
			step(1)
		}
	case "R": // onMarkReadKeyCommandWithR: on a header only
		if e.State == key.Press && cur.kind == unrHeader {
			v.MarkActiveRead()
		}
	case key.NameLeftArrow: // onLeftKeyCommand
		if e.State == key.Press && cur.kind == unrHeader && !cur.f.collapsed {
			v.toggleCollapse(cur.f)
			v.rows = unrRows(v.frames, time.Now(), v.lastRead)
			v.focusHeader(cur.f)
		}
	case key.NameRightArrow: // onRightKeyCommand
		if e.State != key.Press {
			return
		}
		switch {
		case cur.kind == unrHeader && cur.f.collapsed:
			v.toggleCollapse(cur.f)
			v.rows = unrRows(v.frames, time.Now(), v.lastRead)
			v.focusHeader(cur.f)
		case cur.kind == unrMsgRow:
			root := cur.msg.ThreadTS
			if root == "" {
				root = cur.msg.TS
			}
			v.h.OpenThread(cur.msg.Channel, root)
		}
	case key.NamePageUp, key.NamePageDown:
		if e.State != key.Press {
			return
		}
		if !e.Modifiers.Contain(key.ModShift) {
			v.pageKey(e.Name == key.NamePageDown) // the virtual list's own
			return
		}
		if cur.f == nil { // shift: previous / next channel
			return
		}
		at := -1
		var live []*unrFrame
		for _, f := range v.frames {
			if !f.gone {
				live = append(live, f)
			}
		}
		for i, f := range live {
			if f == cur.f {
				at = i
			}
		}
		target := cur.f
		if e.Name == key.NamePageUp {
			if cur.kind == unrHeader {
				if at <= 0 {
					return
				}
				target = live[at-1]
			}
		} else if at+1 < len(live) {
			target = live[at+1]
		} else {
			return
		}
		v.focusHeader(target)
	case key.NameHome, key.NameEnd: // the virtual list's: the first / last row
		if e.State != key.Press {
			return
		}
		for j := range v.rows {
			i := j
			if e.Name == key.NameEnd {
				i = len(v.rows) - 1 - j
			}
			if unrFocusable(v.rows[i].kind) {
				v.active = i
				v.list.ScrollTo(i)
				break
			}
		}
	case key.NameReturn, key.NameEnter: // onEnterKeyCommand, on keyup
		if e.State == key.Release && cur.kind == unrFooter {
			v.markAllRead()
		}
	}
	v.h.Invalidate()
}

// pageKey is BaseKeyboardNavigableList's PageUp/PageDown: scroll a page
// (the height − 21) and activate the first (up, scrolled to the top) or
// last focusable row of the new view once the list has moved; at either
// end, the first or last row in view.
func (v *Unreads) pageKey(down bool) {
	p := v.list.Position
	atTop := p.First == 0 && p.Offset <= 1
	atBottom := p.First+p.Count >= len(v.rows) && p.OffsetLast >= -1
	switch {
	case !down && atTop:
		v.page = vkPageFirst
	case down && atBottom:
		v.page = vkPageLast
	case down:
		v.list.Position.Offset += max(0, v.listH-21)
		v.page = vkPageLast
	default:
		v.list.Position.Offset -= max(0, v.listH-21)
		v.page = -vkPageFirst // scroll the row to the top too
	}
	v.list.Position.BeforeEnd = true
}

// afterPage picks the page key's row from the laid-out list: the rows
// that are wholly in view (getFocusableItemsInView).
func (v *Unreads) afterPage() {
	if v.page == vkNone {
		return
	}
	pg := v.page
	v.page = vkNone
	p := v.list.Position
	first, last := p.First, p.First+p.Count-1
	if p.Offset > 1 {
		first++
	}
	if p.OffsetLast < -1 {
		last--
	}
	pick := -1
	if pg == vkPageLast {
		for i := min(last, len(v.rows)-1); i >= first; i-- {
			if unrFocusable(v.rows[i].kind) {
				pick = i
				break
			}
		}
	} else {
		for i := max(first, 0); i <= last && i < len(v.rows); i++ {
			if unrFocusable(v.rows[i].kind) {
				pick = i
				break
			}
		}
	}
	if pick < 0 {
		return
	}
	v.active = pick
	if pg == -vkPageFirst {
		v.list.ScrollTo(pick)
	}
	v.h.Invalidate()
}

// EmptyEnter is the empty state's Enter: with new unread messages (and the
// button shown), refresh.
func (v *Unreads) EmptyEnter() bool {
	if v.built && !unrAnyUnread(v.frames) && v.newUnread() > 0 && !v.emptyAt.IsZero() && time.Since(v.emptyAt) >= time.Second {
		v.refresh()
		v.h.Invalidate()
		return true
	}
	return false
}

// Frames is for tests: the conversations in order (ids).
func (v *Unreads) Frames() []string {
	var out []string
	for _, f := range v.frames {
		if !f.gone {
			out = append(out, f.ch)
		}
	}
	return out
}
