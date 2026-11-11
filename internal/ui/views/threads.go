package views

// Threads: Slack's Threads view (Home sidebar "Threads"). A 49px header,
// then on a #f8f8f8 background one block per followed thread (unread
// first): a header line outside the card (channel link + participants)
// and a white card (radius 12, 1px #dddddd) holding the root message,
// "Show N more replies", the latest replies and an inline composer.
// Until it's clicked the composer is a cheap picture of Slack's (toolbar,
// "Reply…", "Also send to", footer); the click swaps in the
// real composer (internal/ui/composer) for that card. Threads with unread
// replies get a black-weight channel name and Slack's red "New" line
// above the first unread reply; opening a thread (its replies link or its
// reply box) marks it read. Measurements are from
// /tmp/ui-views-spec/threads.md.

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/ctxclick"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const (
	thrLimit             = 10
	thrMsgPad    unit.Dp = 20          // message row side padding (avatar at card x+21)
	thrMsgIndent unit.Dp = 20 + 36 + 8 // padding + avatar + gap
	thrComposerH unit.Dp = 144
	thrHeadH     unit.Dp = 49
	thrCompMax   unit.Dp = 360 // the real composer's height cap inside the list
)

// Composer toolbar and footer icons; "|" is a group separator.
var (
	// the composer's formatting bar (composer.crFmtOrder): Slack's
	// sticky formatting bar in the Threads view's reply box
	thrToolbar = []string{"bold", "italic", "underline", "strike", "paragraph", "|", "link", "list-ordered", "list-bullet", "|", "quote", "code", "code-block", "divider"}
	thrFooter  = []string{"text-format", "emoji", "at", "|", "video", "mic", "|", "slash"}
)

type thrItem struct {
	key string
	core.Thread
}

type thrData struct {
	items   []thrItem
	next    string
	hasMore bool
	convs   map[string]core.ConvRef
}

// thrCard is the per-thread widget state and cached header text.
type thrCard struct {
	title, more, reply     widget.Clickable
	msgs                   map[string]*thrMsg // per message ts
	itemKey                string             // data-item-key: <channel>-<root ts>
	name, people, moreText string
	also                   string // "Also send to #general"
	built                  bool
	// icon is getChannelIconType's name for a channel's header
	// (InlineChannelEntity), "" for a DM or group DM
	icon string

	comp  *composer.Composer // the real reply composer, made on first click
	compY int                // composer top within the list element (px)

	// "Show N more replies" (threads_older.go): the older replies loaded
	// into the card, oldest first; a fetch running; the thread's start
	// reached (has_more false); the fetch's answer for the UI goroutine
	older      []core.Message
	olderBusy  bool
	olderStart bool
	olderRes   atomic.Pointer[thrOlder]
}

func (c *thrCard) msg(ts string) *thrMsg {
	m := c.msgs[ts]
	if m == nil {
		m = new(thrMsg)
		c.msgs[ts] = m
	}
	return m
}

// thrMsg is one message row's hover state and this frame's context, read
// by its Slack boundaries (ThreadRootGeneric,
// ThreadSenderAndTimestampGeneric) through method expressions.
type thrMsg struct {
	hv          gesture.Hover
	ctx         struct{ _ byte } // right-click target: the message's context menu
	v           *Threads
	m           *core.Message
	top         unit.Dp
	name, stamp string
}

// Threads is the Threads view.
type Threads struct {
	base

	data  async[thrData]
	list  widget.List
	cards map[string]*thrCard
	w     watcher

	mode         actLoad
	wasBusy      bool
	paging       bool
	pageCursor   string
	failedCursor string
	retry        widget.Clickable
	retryMore    widget.Clickable
	learn        widget.Clickable // empty state's "Learn about threads"

	// read is the local read state: thread key -> latest reply ts marked
	// read (Slack's view lags behind subscriptions.thread.mark).
	read  map[string]string
	sizes map[int]int // list element heights this frame (composer origins)
	divAt int         // the item the "You’re up to date" divider sits above (-1: none)
	notes actNotes

	// kb is the list's keyboard (vlist_keys.go) over Slack's rows:
	// heading, root, load_older, replies and footer per thread
	// (get_rows_and_keys); kbRows maps a row key to its thread and ts.
	kb     *vkList
	kbRows map[string]thrKBRow
	keyFs  []key.Filter

	// marks are thread_marked last_reads waiting for the UI goroutine
	// (threadMarked patches the thread in place: no reload).
	mu    sync.Mutex
	marks map[string]string
}

// thrKBRow is one keyboard row of the Threads list.
type thrKBRow struct {
	kind   string // heading, root, load_older, reply, footer
	key    string // the thread (thrItem.key)
	ch, ts string
}

// NewThreads builds the Threads view; it loads on first Layout.
func NewThreads(h Host) *Threads {
	v := &Threads{base: newBase(h), cards: map[string]*thrCard{}, read: map[string]string{}, sizes: map[int]int{},
		kbRows: map[string]thrKBRow{}, marks: map[string]string{}}
	v.list.Axis = layout.Vertical
	v.kb = newVKList(&v.list)
	v.kb.onFocus = v.kbFocus
	return v
}

// match runs on the event goroutine. Slack's RTM-THREADS handlers: a new
// reply (messageReplied → "If all threads is open, maybe update it after a
// new message arrives") and thread_subscribed (threadSubscribed → the same
// thunk) fetch the thread into the list, which Clack does with a refresh;
// thread_marked patches the thread's last read in place; thread_unsubscribed
// only flips the root's subscribed flag and update_thread_state only the
// badges, so neither reloads.
func (v *Threads) match(ev core.Event) bool {
	switch ev.Type {
	case "message":
		return ev.Reply
	case "thread_subscription":
		return ev.Name == "follow"
	case "thread_marked":
		var e struct {
			Subscription struct {
				Channel  string `json:"channel"`
				ThreadTS string `json:"thread_ts"`
				LastRead string `json:"last_read"`
			} `json:"subscription"`
		}
		if json.Unmarshal(ev.Raw, &e) == nil && e.Subscription.Channel != "" && e.Subscription.ThreadTS != "" && e.Subscription.LastRead != "" {
			v.mu.Lock()
			v.marks[e.Subscription.Channel+"/"+e.Subscription.ThreadTS] = e.Subscription.LastRead
			v.mu.Unlock()
			v.h.Invalidate()
		}
	}
	return false
}

// applyMarks moves thread_marked last reads into the local read state.
func (v *Threads) applyMarks() {
	v.mu.Lock()
	defer v.mu.Unlock()
	for k, lr := range v.marks {
		v.read[k] = lr
		delete(v.marks, k)
	}
}

// Layout draws the view.
func (v *Threads) Layout(gtx C) D {
	v.frame()
	v.notes.drain(v.h)
	v.w.watch(v.h, v.match)
	v.applyMarks()
	if !v.data.started() {
		v.load(actFresh)
	}
	if v.retry.Clicked(gtx) {
		v.load(actFresh)
	}
	if v.retryMore.Clicked(gtx) {
		v.failedCursor = ""
	}
	if v.w.due(gtx, 5*time.Second) && !(v.data.busy() && v.mode == actFresh) {
		v.load(actRefresh)
	}
	v.settle()

	st := thrViewClass.Enter(gtx, 0, 0)
	defer st.Leave()
	size := gtx.Constraints.Max
	fill(gtx, image.Rectangle{Max: size}, st.Background(v.p.Page))
	border := v.headBorder(gtx) // from the list's last position
	layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(v.head),
		layout.Flexed(1, v.body),
	)
	if border == thrBorderBelow { // .p-view_header's box-shadow 0 1px 0 0, over the list (z-index 202)
		h, hl := gtx.Dp(thrHeadH), max(1, gtx.Dp(1))
		fill(gtx, image.Rect(0, h, size.X, h+hl), v.p.Border)
	}
	return D{Size: size}
}

// The Threads header's bottom line (thrBorder*).
const (
	thrBorderNone  = iota // .p-view_header--without-bottom-border
	thrBorderInset        // .p-view_header--inset-box-shadow: inside the 49px
	thrBorderBelow        // the base box-shadow 0 1px 0 0: under the header
)

// headBorder is ThreadsViewHeader's (client-threads-view auBp): with
// nested_secondary_view the header always has its line, an inset one
// (ViewHeader W58+ --inset-box-shadow). Without it, hideBorder is
// !isScrolled: no line until, with view_header_overflow_borders, the list
// has scrolled more than 15px under the header (maybeSetIsScrolled);
// without that experiment isScrolled never turns true. (hideBorder also
// holds while the VIP tabs show; Clack's Threads view has none.)
func (v *Threads) headBorder(gtx C) int {
	if nestedSecondary(v.h) {
		return thrBorderInset
	}
	c := v.h.Core()
	if c == nil || !c.ViewHeaderOverflowBorders() {
		return thrBorderNone
	}
	if p := v.list.Position; p.First > 0 || p.Offset > gtx.Dp(15) {
		return thrBorderBelow
	}
	return thrBorderNone
}

// head is p-view_header: h 49, padding 0 12px 0 18px, 18px/24px 900 title.
// With nested_secondary_view it has the primary background
// (--primary-background: allThreads under W7) and a 1px line inside the
// 49px; without it the secondary background (base-sec, the page's) and
// the line headBorder gives.
func (v *Threads) head(gtx C) D {
	w, h := gtx.Constraints.Max.X, gtx.Dp(thrHeadH)
	r := image.Rect(0, 0, w, h)
	border := v.headBorder(gtx)
	bg := v.p.Page
	if border == thrBorderInset {
		bg = v.p.Bg
	}
	fill(gtx, r, bg)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, h))
	layout.Inset{Left: 18, Right: 12}.Layout(g, func(gtx C) D {
		return layout.W.Layout(gtx, func(gtx C) D { return v.para(gtx, 18, 24, wBlack, v.p.Text, 1, "Threads") })
	})
	if border == thrBorderInset {
		hl := max(1, gtx.Dp(1))
		fill(gtx, image.Rect(0, h-hl, w, h), v.p.Border)
	}
	return D{Size: image.Pt(w, h)}
}

// settle runs once after each load finishes.
func (v *Threads) settle() {
	if !v.wasBusy || v.data.busy() {
		return
	}
	v.wasBusy = false
	for _, c := range v.cards {
		c.built = false
	}
	if v.paging {
		v.paging = false
		if _, _, err := v.data.get(); err != nil {
			v.failedCursor = v.pageCursor
		}
	}
}

func (v *Threads) load(mode actLoad) {
	c := v.h.Core()
	old, _, _ := v.data.get()
	cursor := ""
	if mode == actMore {
		cursor = old.next
		v.pageCursor = cursor
	}
	if mode == actFresh {
		for _, c := range v.cards {
			if c.comp != nil {
				c.comp.Close()
			}
		}
		v.cards = map[string]*thrCard{}
		v.list.Position = layout.Position{}
		v.failedCursor = ""
	}
	v.mode, v.paging, v.wasBusy = mode, mode == actMore, true
	v.data.load(v.h, mode != actFresh, func(ctx context.Context) (thrData, error) {
		p, err := c.Threads(ctx, cursor, thrLimit)
		if err != nil {
			return thrData{}, err
		}
		fresh := thrFrom(p)
		switch mode {
		case actMore:
			return thrJoin(old, fresh), nil
		case actRefresh:
			if len(old.items) <= len(fresh.items) || len(fresh.items) == 0 {
				return fresh, nil
			}
			return thrJoin(fresh, old), nil
		}
		return fresh, nil
	})
}

func thrFrom(p core.ThreadsPage) thrData {
	d := thrData{next: p.Next, hasMore: p.HasMore, convs: p.Convs}
	d.items = make([]thrItem, 0, len(p.Threads))
	for _, t := range p.Threads {
		d.items = append(d.items, thrItem{key: t.Channel + "/" + t.Root.TS, Thread: t})
	}
	thrSort(d.items)
	return d
}

// thrJoin returns a's threads then b's that aren't in a, with b's cursor
// (pagination: a = loaded, b = next page; refresh: a = fresh first page,
// b = everything loaded before).
func thrJoin(a, b thrData) thrData {
	seen := make(map[string]bool, len(a.items))
	items := make([]thrItem, 0, len(a.items)+len(b.items))
	for _, t := range a.items {
		seen[t.key] = true
		items = append(items, t)
	}
	for _, t := range b.items {
		if !seen[t.key] {
			items = append(items, t)
		}
	}
	thrSort(items)
	return thrData{items: items, next: b.next, hasMore: b.hasMore, convs: actRefs(a.convs, b.convs)}
}

// latest is the newest reply ts shown for a thread (or the root's).
func (t *thrItem) latest() string {
	if n := len(t.Replies); n > 0 {
		return t.Replies[n-1].TS
	}
	if t.Root.LatestReply != "" {
		return t.Root.LatestReply
	}
	return t.Root.TS
}

// unread is the number of unread replies, after local read marks and
// thread_marked last reads (a mark back before the latest reply makes the
// replies after it unread again).
func (v *Threads) unread(t *thrItem) int {
	r, ok := v.read[t.key]
	if !ok {
		return max(t.UnreadCount, 0)
	}
	if !actTSLess(r, t.latest()) {
		return 0
	}
	n := 0
	for i := range t.Replies {
		if actTSLess(r, t.Replies[i].TS) {
			n++
		}
	}
	return max(n, t.UnreadCount)
}

// markRead marks a thread read up to its latest reply: locally at once,
// then with subscriptions.thread.mark.
func (v *Threads) markRead(t *thrItem) {
	if v.unread(t) == 0 {
		return
	}
	ts := t.latest()
	v.read[t.key] = ts
	c, ch, root := v.h.Core(), t.Channel, t.Root.TS
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := c.MarkThread(ctx, ch, root, ts); err != nil {
			v.notes.add(v.h, "Couldn’t mark the thread as read: "+friendlyErr(err))
		}
	}()
}

// thrSort puts threads with unread replies first, keeping Slack's order
// otherwise.
func thrSort(items []thrItem) {
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].UnreadCount > 0 && items[j].UnreadCount == 0
	})
}

func (v *Threads) body(gtx C) D {
	page, loaded, err := v.data.get()
	n := len(page.items)
	switch {
	case !loaded && n == 0:
		return layout.Inset{Top: 16}.Layout(gtx, func(gtx C) D { return v.skeleton(gtx, 5, 36) })
	case err != nil && n == 0:
		return v.emptyState(gtx, "", thrErrText, friendlyErr(err), &v.retry, "Try again")
	case n == 0:
		return v.empty(gtx)
	}
	failed := v.failedCursor != "" && v.failedCursor == page.next
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	clear(v.sizes)
	div := v.firstRead(page.items)
	v.divAt = div
	v.kb.setItems(v.kbItems(page.items))
	v.kb.update(gtx, gtx.Constraints.Max.Y, vkOpts{Click: v.kbClick, Key: func(e key.Event, active string) bool { return v.kbKey(gtx, e, active) }, Extra: v.typeFilters()})
	v.kb.begin()
	d := v.scrollList(gtx, &v.list, n+1, func(gtx C, i int) D {
		if i >= n {
			return v.footer(gtx, failed)
		}
		var d D
		if i == div {
			y0 := 0
			d = layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D { d := v.upToDate(gtx); y0 = d.Size.Y; return d }),
				layout.Rigid(func(gtx C) D { return v.thread(gtx, &page.items[i], page.convs, y0) }),
			)
		} else {
			d = v.thread(gtx, &page.items[i], page.convs, 0)
		}
		v.sizes[i] = d.Size.Y
		return v.kb.elem(i, d)
	})
	v.kb.area(gtx, image.Rectangle{Max: d.Size})
	v.kb.after()
	v.placeComposers(gtx, page.items)
	if page.hasMore && page.next != "" && !failed && !v.data.busy() &&
		v.list.Position.First+v.list.Position.Count >= n-2 {
		v.load(actMore)
	}
	return d
}

// placeComposers sets each open composer's window Origin (its menus and
// the emoji picker anchor to it) from where the list put its card this
// frame. Gio has no global transform query, so the page's own origin comes
// from the Host when it offers one, else from the default chrome geometry
// (rail, top bar and the default sidebar width).
func (v *Threads) placeComposers(gtx C, items []thrItem) {
	var org image.Point
	if o, ok := v.h.(interface{ PrimaryOrigin() image.Point }); ok {
		org = o.PrimaryOrigin()
	} else {
		org = image.Pt(gtx.Dp(theme.RailW+theme.SidebarW), gtx.Dp(theme.TopNavH))
	}
	pos := v.list.Position
	y := gtx.Dp(thrHeadH) - pos.Offset
	for i := pos.First; i < pos.First+pos.Count && i < len(items); i++ {
		if c := v.cards[items[i].key]; c != nil && c.comp != nil {
			cy := y + c.compY
			if i == v.divAt {
				cy += gtx.Dp(32 + 20) // the "You’re up to date" divider above it
			}
			c.comp.Origin = org.Add(image.Pt(gtx.Dp(16+4), cy))
		}
		y += v.sizes[i]
	}
}

// empty is Slack's ThreadsEmptyState: an extended c-empty_state (column
// max 420 wide) with the large :seedling: emoji, "Tend to your threads"
// (900 18px/24px, 8px below), the description (15px/22px, 8px below) and
// the "Learn about threads" help link.
func (v *Threads) empty(gtx C) D {
	if v.learn.Clicked(gtx) {
		go func() { _ = openurl.Open(thrHelpURL) }()
	}
	return layout.Center.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(420))
		gtx.Constraints.Min.X = 0
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Bottom: 24}.Layout(gtx, func(gtx C) D {
					if u, _, ok := v.h.Core().Emoji("seedling"); ok && u != "" {
						return v.emojiImg(gtx, u, 64)
					}
					return v.icon(gtx, "threads", 64, v.p.Faint)
				})
			}),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D {
					return v.th.Layout(gtx, theme.TextStyle{Size: 18, LineHeight: 24, Weight: wBlack, Color: v.p.Text, Align: text.Middle}, thrEmptyTitle)
				})
			}),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D {
					return v.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: v.p.Sec, Align: text.Middle}, thrEmptyBody)
				})
			}),
			layout.Rigid(func(gtx C) D { return v.linkButton(gtx, &v.learn, 15, "Learn about threads") }),
		)
	})
}

// Slack's Threads view copy (client-threads-view chunk).
const (
	thrEmptyTitle = "Tend to your threads"
	thrEmptyBody  = "Threads you’re involved in will be collected right here."
	thrErrText    = "Something’s fishy! Slack can’t seem to load your threads."
	thrUpToDate   = "You’re up to date"
	// HelpLink articleId 0x1ac6947d87
	thrHelpURL = "https://slack.com/help/articles/115000769927"
)

// upToDate is Slack's threads-view Divider ("You’re up to date") before
// the first thread that was already read when the view loaded, once no
// thread above it has unread replies (get_rows_and_keys: the divider row
// goes before firstReadThreadKey unless it's the first thread or a thread
// above still has replies newer than its initial last read). It's a 1px
// #dddddd line with the 700 #616061 label on the page background in a
// rounded pill (padding 4px 12px); margin 32px above, 20px below.
func (v *Threads) upToDate(gtx C) D {
	w := gtx.Constraints.Max.X
	top, bottom := gtx.Dp(32), gtx.Dp(20)
	lw := v.th.Measure(gtx, 15, wBold, thrUpToDate)
	pw, ph := lw+gtx.Dp(24), gtx.Dp(22+8)
	y := top
	fill(gtx, image.Rect(gtx.Dp(16), y, w-gtx.Dp(16), y+max(1, gtx.Dp(1))), v.p.Border)
	x := (w - pw) / 2
	rrect(gtx, image.Rect(x, y-ph/2, x+pw, y+ph/2), ph/2, v.p.Page)
	func() {
		defer op.Offset(image.Pt(x+gtx.Dp(12), y-ph/2+gtx.Dp(4))).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(lw+1, gtx.Dp(22))}
		v.para(g, 15, 22, wBold, v.meta(), 1, thrUpToDate)
	}()
	return D{Size: image.Pt(w, top+bottom)}
}

// firstRead is the index of the thread Slack puts the "You’re up to
// date" divider before, or -1: the first thread that had no unread
// replies at load, when it isn't the first one and every thread above it
// has since been read.
func (v *Threads) firstRead(items []thrItem) int {
	for i := range items {
		if items[i].UnreadCount > 0 {
			continue
		}
		if i == 0 {
			return -1
		}
		for j := 0; j < i; j++ {
			if v.unread(&items[j]) > 0 {
				return -1
			}
		}
		return i
	}
	return -1
}

func (v *Threads) footer(gtx C, failed bool) D {
	switch {
	case v.paging:
		return v.skeleton(gtx, 2, 36)
	case failed:
		return layout.Inset{Top: 8, Bottom: 16}.Layout(gtx, func(gtx C) D {
			return layout.Center.Layout(gtx, func(gtx C) D {
				return v.button(gtx, &v.retryMore, btnOutline, true, "Load more")
			})
		})
	}
	return D{Size: image.Pt(gtx.Constraints.Min.X, gtx.Dp(16))}
}

// people is the participants line: "Just you", "Ada and you", "Ada,
// Grace, and you", or "Ada, Grace, Linus, and 4 others".
func (v *Threads) people(root *core.Message, replies []core.Message) string {
	self := v.selfID()
	seen := map[string]bool{}
	var names []string
	hasSelf := false
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		if id == self {
			hasSelf = true
			return
		}
		if _, ok := v.user(id); !ok && strings.HasPrefix(id, "B") {
			// a bot id (ReplyUsers can hold them): use the name its
			// messages carry, never the raw B… id
			if n := botName(id, root, replies); n != "" {
				names = append(names, n)
			}
			return
		}
		names = append(names, v.userName(id))
	}
	if root.User == "" {
		if n, _ := v.authorOf(root); n != "" {
			names = append(names, n)
		}
	}
	add(root.User)
	for _, id := range root.ReplyUsers {
		add(id)
	}
	for i := range replies {
		add(replies[i].User)
	}
	if hasSelf {
		if len(names) == 0 {
			return "Just you"
		}
		names = append(names, "you")
	}
	switch {
	case len(names) == 0:
		return ""
	case len(names) == 1:
		return names[0]
	case len(names) > 4:
		return strings.Join(names[:3], ", ") + ", and " + strconv.Itoa(len(names)-3) + " others"
	case len(names) == 2:
		return names[0] + " and " + names[1]
	}
	// Slack (en-US list format): "Shroud, Lachlan, and you".
	return strings.Join(names[:len(names)-1], ", ") + ", and " + names[len(names)-1]
}

// thrTime is the message timestamp, Slack's message formatter (client-boot
// QvGr, the Threads view's c-timestamp): "Just now" under a minute, then
// toTimeAgo ("1 minute ago", "N minutes ago", "1 hour ago" from 45
// minutes), "Today at 2:46 PM", "Yesterday at …", the weekday within a
// week ("Saturday at 2:46 PM"), else "Nov 4th at 2:46 PM" (with the year
// when it isn't this year's). next is when the text changes (0 = not
// soon).
func thrTime(t, now time.Time) (s string, next time.Duration) {
	if t.IsZero() {
		return "", 0
	}
	age := now.Sub(t)
	if age < 0 {
		age = -age
	}
	switch {
	case age < time.Minute:
		return "Just now", time.Minute - age
	case age < 90*time.Second:
		return "1 minute ago", 90*time.Second - age
	case age < 45*time.Minute:
		// toTimeAgo rounds: 2:29 is "2 minutes ago", 2:30 "3 minutes ago"
		m := int((age + 30*time.Second) / time.Minute)
		return strconv.Itoa(m) + " minutes ago", time.Duration(m)*time.Minute + 30*time.Second - age
	case age < time.Hour:
		return "1 hour ago", time.Hour - age
	case sameDay(t, now):
		return "Today at " + clock(t), 0
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "Yesterday at " + clock(t), 0
	case age < 7*24*time.Hour:
		return t.Format("Monday") + " at " + clock(t), 0
	}
	s = t.Format("Jan 2") + ordinal(t.Day())
	if t.Year() != now.Year() {
		s += ", " + strconv.Itoa(t.Year())
	}
	return s + " at " + clock(t), 0
}

// meta is Slack's #616061 for timestamps and participants.
func (v *Threads) meta() color.NRGBA { return v.p.Meta }

// edge is the card border (#dddddd per tokens; the card hairline over the
// card surface in dark mode).
func (v *Threads) edge() color.NRGBA {
	if v.th.Dark {
		return flatten(darkPal.CardLine, v.p.Surface)
	}
	return rgb(0xdddddd)
}

// msgHover is the message hover fill on a card: #f8f8f8 in light mode,
// the hover overlay over the card surface in dark mode.
func (v *Threads) msgHover() color.NRGBA {
	if v.th.Dark {
		return v.actHover(v.p.Surface)
	}
	return v.p.ReadFill
}

func (v *Threads) cardFor(t *thrItem, convs map[string]core.ConvRef) *thrCard {
	c := v.cards[t.key]
	if c == nil {
		c = &thrCard{msgs: map[string]*thrMsg{}, itemKey: t.Channel + "-" + t.Root.TS}
		v.cards[t.key] = c
	}
	if c.built && (c.name == t.Channel || c.name == "#"+t.Channel) {
		// Built before core knew the conversation (early boot): retry.
		if n := v.convTitle(t.Channel, convs); n != c.name {
			c.built = false
		}
	}
	if !c.built || v.viewsPatched() {
		c.name = v.convTitle(t.Channel, convs)
		if strings.HasPrefix(t.Channel, "D") && (c.name == t.Channel || c.name == "Direct message") {
			// a DM core doesn't know yet: name it after the other person
			c.name = v.dmPartner(&t.Root, t.Replies)
		}
		c.people = v.people(&t.Root, t.Replies)
		c.moreText = thrMoreText(t, c)
		kind := ""
		if r, ok := convs[t.Channel]; ok {
			kind = r.Kind
		} else {
			kind = v.h.Core().ConvRef(t.Channel).Kind
		}
		c.also = "Also send to " + c.name
		if isDMKind(kind) {
			c.also = "Also send as direct message"
		}
		c.icon = channelIcon(v.h.Core(), t.Channel, kind)
		c.built = true
	}
	return c
}

// thread is one block: the header line on the gray background, then the
// card.
func (v *Threads) thread(gtx C, t *thrItem, convs map[string]core.ConvRef, y0 int) D {
	c := v.cardFor(t, convs)
	if c.title.Clicked(gtx) {
		v.h.Navigate("c/" + t.Channel)
	}
	v.applyOlder(t, c)
	if c.more.Clicked(gtx) {
		gtx.Execute(op.InvalidateCmd{})
		v.loadOlder(t, c)
	}
	if c.reply.Clicked(gtx) {
		gtx.Execute(op.InvalidateCmd{})
		v.openReply(t, c)
	}
	nameW := wBold
	if v.unread(t) > 0 {
		nameW = wBlack
	}
	st := thrItemClass.EnterAttr(gtx, 0, 0, "data-item-key", c.itemKey)
	defer st.Leave()
	if st.Hidden() {
		return D{}
	}
	headH, cardTop := 0, 0
	var subs []thrSub
	d := layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Card header: margin 16px 28px 12px 24px, h 40.
		layout.Rigid(func(gtx C) D {
			d := layout.Inset{Top: 16, Right: 28, Bottom: 12, Left: 24}.Layout(gtx, func(gtx C) D {
				return col(gtx,
					layout.Rigid(func(gtx C) D {
						gtx.Constraints.Min.X = 0
						return c.title.Layout(gtx, func(gtx C) D {
							pointerCursor(gtx)
							// a channel's name is Slack's InlineChannelEntity
							// (p-threads_view_header__channel_name nudge_left_3),
							// whose icon and name plugins' rules see
							// (StreamerMode blurs a private channel's)
							var nst slackc.Styled
							if c.icon != "" {
								ent := thrHeaderEntity.Enter(gtx, hoverState(c.title.Hovered()), t.Channel, c.icon)
								defer ent.Leave()
								if ent.Hidden() {
									return D{}
								}
								nst = thrHeaderEntity.Name(ent)
							}
							name := func(gtx C) D { return v.para(gtx, 15, 22, nameW, v.p.Text, 1, c.name) }
							var d D
							if nst.Active() {
								d = nst.Wrap(gtx, name)
							} else {
								d = name(gtx)
							}
							if c.title.Hovered() {
								fill(gtx, image.Rect(0, d.Size.Y-gtx.Dp(3), d.Size.X, d.Size.Y-gtx.Dp(2)), v.p.Text)
							}
							return d
						})
					}),
					layout.Rigid(func(gtx C) D {
						if c.people == "" {
							return D{}
						}
						return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D {
							return v.para(gtx, 12, 17.6, wReg, v.meta(), 1, c.people)
						})
					}),
				)
			})
			headH = d.Size.Y
			subs = append(subs, thrSub{"heading-" + t.key, 0, d.Size.Y})
			return d
		}),
		// Card: margin 0 16px 8px, radius 12, 1px #dddddd.
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 16, Right: 16, Bottom: 8}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				m := op.Record(gtx.Ops)
				d, compY := v.card(gtx, c, t, func(key string, y, h int) {
					subs = append(subs, thrSub{key, headH + y, h})
				})
				cardTop = compY
				call := m.Stop()
				r := image.Rectangle{Max: d.Size}
				rad := gtx.Dp(12)
				rrect(gtx, r, rad, v.p.Surface)
				func() {
					defer clip.UniformRRect(r, rad).Push(gtx.Ops).Pop()
					call.Add(gtx.Ops)
				}()
				border(gtx, r, rad, max(1, gtx.Dp(1)), v.edge())
				return d
			})
		}),
	)
	c.compY = headH + cardTop
	// Slack's rows are list items of their own: record where each sits and
	// ring the keyboard's (.p-threads_view .c-virtual_list__item--focus:
	// focus:after, inset 4px 16px).
	for _, sb := range subs {
		v.kb.sub(sb.key, y0+sb.y, sb.h)
		if v.kb.ringOn(sb.key) {
			v.vkRing(gtx, image.Rect(gtx.Dp(16), sb.y+gtx.Dp(4), d.Size.X-gtx.Dp(16), sb.y+sb.h-gtx.Dp(4)), gtx.Dp(8))
		}
	}
	return d
}

// thrSub is a keyboard row's place inside its thread element.
type thrSub struct {
	key  string
	y, h int
}

// openReply swaps in the card's real composer and focuses it.
func (v *Threads) openReply(t *thrItem, c *thrCard) {
	v.markRead(t)
	ch, ok := v.h.(composer.Host)
	if !ok {
		v.h.OpenThread(t.Channel, t.Root.TS)
		return
	}
	if c.comp == nil {
		c.comp = composer.New(ch, t.Channel, t.Root.TS)
		c.comp.InThreadsView = true // .p-threads_view: no 12px over the box
		key := t.key
		// onUpArrowInEmptyInput: focusItemAboveInput
		c.comp.OnFocusList = func() { v.focusAboveFooter(key) }
	}
	c.comp.Focus()
}

// card lays out the card's segments and returns its size and the top of
// the reply box within it.
func (v *Threads) card(gtx C, c *thrCard, t *thrItem, row func(key string, y, h int)) (D, int) {
	unread := v.unread(t)
	replies := thrShown(t, c) // the loaded older replies, then the latest ones
	unreadFrom := len(replies) - unread
	compH, compBottom := 0, 20
	if c.comp != nil {
		compBottom = 0 // the real composer ends with its 24px hint bar
	}
	y := 0 // the Flex lays its rigid children in order
	rec := func(key string, d D) D {
		row(key, y, d.Size.Y)
		y += d.Size.Y
		return d
	}
	d := layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			s := c.msg(t.Root.TS)
			s.v, s.m, s.top = v, &t.Root, 20
			return rec("root-"+t.key, slackc.Component(gtx, "ThreadRootGeneric", s, (*thrMsg).rootProps, (*thrMsg).layoutRoot))
		}),
		layout.Rigid(func(gtx C) D {
			if c.moreText == "" {
				return D{}
			}
			return rec("load_older-"+t.key, v.olderRow(gtx, c))
		}),
		layout.Rigid(func(gtx C) D {
			return actStack(gtx, len(replies), func(gtx C, i int) D {
				m := &replies[i]
				d := v.msg(gtx, c.msg(m.TS), m, 8)
				if unread > 0 && i == max(unreadFrom, 0) {
					v.newLine(gtx, d.Size.X)
				}
				return rec(t.key+"-"+m.TS, d)
			})
		}),
		// Last segment: padding-top 8px, then the composer (margin 0 20px;
		// the real composer brings 16px of its own).
		layout.Rigid(func(gtx C) D {
			if c.comp != nil {
				d := layout.Inset{Top: 8, Left: 4, Right: 4}.Layout(gtx, func(gtx C) D {
					gtx.Constraints.Min = image.Point{}
					gtx.Constraints.Max.Y = gtx.Dp(thrCompMax)
					return c.comp.Layout(gtx)
				})
				compH = d.Size.Y - gtx.Dp(8)
				return rec("footer-"+t.key, d)
			}
			d := layout.Inset{Top: 8, Bottom: 20, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
				return v.replyPicture(gtx, c)
			})
			compH = d.Size.Y - gtx.Dp(8+20)
			return rec("footer-"+t.key, d)
		}),
	)
	return d, d.Size.Y - compBottom - compH
}

// newLine is Slack's unread divider drawn over the top edge of the first
// unread reply: a 1px #e01e5a line with a bold 13px "New" at its right
// end (padding 0 4px, 15px from the edge), as in the channel view.
func (v *Threads) newLine(gtx C, w int) {
	red := v.th.C.NewLine
	tw := v.th.Measure(gtx, 13, wBold, "New")
	lx := w - gtx.Dp(15) - gtx.Dp(4) - tw
	fill(gtx, image.Rect(0, 0, lx-gtx.Dp(4), max(1, gtx.Dp(1))), red)
	defer op.Offset(image.Pt(lx, -gtx.Dp(9))).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints.Min = image.Point{}
	v.para(g, 13, 19.5, wBold, red, 1, "New")
}

// msg is c-message_kit__thread_message: padding top/20px/8px/20px, avatar
// 36 radius 8, 8px gap, 15px/22px 900 name, 12px/17.6px #616061 time 7px
// after it, 15px/22px body. Hover paints #f8f8f8 (the hover overlay in
// dark mode).
func (v *Threads) msg(gtx C, s *thrMsg, m *core.Message, top unit.Dp) D {
	hv := &s.hv
	hovered := hv.Update(gtx.Source)
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &s.ctx, Kinds: pointer.Press})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok && ctxclick.Press(e) {
			v.messageMenu(*m)
		}
	}
	name, av := v.authorOf(m)
	stamp, next := thrTime(tsTime(m.TS), gtx.Now)
	if next > 0 {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(next)})
	}
	s.v, s.m, s.name, s.stamp = v, m, name, stamp
	var mods uint64
	if hovered {
		mods = 1
	}
	cs := thrMsgClass.Enter(gtx, hoverState(hovered), mods)
	defer cs.Leave()
	if cs.Hidden() {
		return D{}
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	rec := op.Record(gtx.Ops)
	d := layout.Inset{Top: top, Bottom: 8, Left: thrMsgPad, Right: thrMsgPad}.Layout(gtx, func(gtx C) D {
		return row(gtx, layout.Start,
			layout.Rigid(func(gtx C) D { return v.avatar(gtx, av, 36) }),
			hgap(8),
			layout.Flexed(1, func(gtx C) D {
				return col(gtx,
					layout.Rigid(func(gtx C) D {
						return slackc.Component(gtx, "ThreadSenderAndTimestampGeneric", s, (*thrMsg).senderProps, (*thrMsg).layoutSender)
					}),
					layout.Rigid(func(gtx C) D { return v.msgBody(gtx, m, false, 0) }),
					layout.Rigid(func(gtx C) D {
						if len(m.Reactions) == 0 {
							return D{}
						}
						return layout.Inset{Top: 4}.Layout(gtx, func(gtx C) D { return v.actReactions(gtx, m) })
					}),
				)
			}),
		)
	})
	call := rec.Stop()
	r := image.Rectangle{Max: d.Size}
	if hovered {
		fill(gtx, r, cs.Background(v.msgHover()))
	}
	call.Add(gtx.Ops)
	st := clip.Rect(r).Push(gtx.Ops)
	hv.Add(gtx.Ops)
	pass := pointer.PassOp{}.Push(gtx.Ops)
	event.Op(gtx.Ops, &s.ctx)
	pass.Pop()
	st.Pop()
	return d
}

// messageMenu opens m's message actions menu at the pointer, as Slack's
// Threads view list does on a right-click (MessageActionsContextMenu with
// messageContainerType threads_view: "View thread").
func (v *Threads) messageMenu(m core.Message) {
	ph, ok := v.h.(popups.Host)
	if !ok {
		v.h.OpenMessageMenu(image.Point{}, m)
		return
	}
	var at image.Point
	if pp, ok := v.h.(interface{ PointerPos() image.Point }); ok {
		at = pp.PointerPos()
	}
	ph.ShowOverlay(popups.NewMessageContextMenu(ph, at, m, popups.InThreadsView, popups.MessageMenuOpts{}))
}

// replyPicture draws Slack's unfocused inline thread composer (h 144,
// radius 8, 1px #1d1c1d@.13): formatting toolbar with icons at 30% alpha,
// "Reply…", the "Also send to" checkbox row and the footer with
// the send button disabled. It costs nothing per card; a click swaps in
// the real composer (or opens the thread pane when the Host can't host
// one).
func (v *Threads) replyPicture(gtx C, c *thrCard) D {
	return c.reply.Layout(gtx, func(gtx C) D {
		pointer.CursorText.Add(gtx.Ops)
		w, h := gtx.Constraints.Max.X, gtx.Dp(thrComposerH)
		r := image.Rect(0, 0, w, h)
		rad := gtx.Dp(8)
		edge, faint, soft := argb(0x211d1c1d), argb(0x4d1d1c1d), argb(0xb31d1c1d)
		if v.th.Dark {
			edge, faint, soft = darkPal.Ring, theme.WithAlpha(v.p.Text, 0x4d), v.p.Muted
		}
		edge, faint, soft = flatten(edge, v.p.Bg), flatten(faint, v.p.Bg), flatten(soft, v.p.Bg)
		if c.reply.Hovered() {
			edge = v.p.BorderStr
		}
		rrect(gtx, r, rad, v.p.Bg)
		border(gtx, r, rad, max(1, gtx.Dp(1)), edge)

		// Toolbar (h 38): 28px buttons 4px apart from x 5, 18px icons;
		// Paragraph is 44 wide (icon + caret); separators sit 6px clear.
		v.thrFmtIcons(gtx, gtx.Dp(5), gtx.Dp(38), faint)

		// Editor: padding 8px 12px, 15px/22px placeholder.
		func() {
			defer op.Offset(image.Pt(gtx.Dp(12), gtx.Dp(38+8))).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Constraints{Max: image.Pt(max(0, w-gtx.Dp(24)), gtx.Dp(22))}
			v.para(g, 15, 22, wReg, soft, 1, "Reply…")
		}()

		// "Also send to #channel": 13px checkbox (12px right margin), 12px/18px label.
		func() {
			y := gtx.Dp(38 + 38)
			cb := gtx.Dp(13)
			func() {
				defer op.Offset(image.Pt(gtx.Dp(12), y+(gtx.Dp(26)-cb)/2)).Push(gtx.Ops).Pop()
				v.actCheck(gtx, 13, false)
			}()
			defer op.Offset(image.Pt(gtx.Dp(12+13+12), y+gtx.Dp(4))).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Constraints{Max: image.Pt(max(0, w-gtx.Dp(49)), gtx.Dp(18))}
			v.para(g, 12, 18, wReg, soft, 1, c.also)
		}()

		// Footer (h 40, padding 0 4px 0 6px): "+" in a #f8f8f8 circle,
		// formatting/emoji/mention, media, shortcuts; send + caret on the right.
		fy := h - gtx.Dp(40)
		func() {
			defer op.Offset(image.Pt(gtx.Dp(6), fy)).Push(gtx.Ops).Pop()
			s := gtx.Dp(24)
			cy := gtx.Dp(40) / 2
			theme.Circle(gtx, image.Pt(gtx.Dp(2)+s/2, cy), s, v.p.ReadFill)
			func() {
				defer op.Offset(image.Pt(gtx.Dp(2)+(s-gtx.Dp(16))/2, cy-gtx.Dp(8))).Push(gtx.Ops).Pop()
				v.icon(gtx, "plus", 16, v.p.Sec)
			}()
			v.thrIcons(gtx, image.Pt(gtx.Dp(32), 0), gtx.Dp(40), thrFooter, v.p.Sec)
		}()
		func() {
			sw := gtx.Dp(28)
			x := w - gtx.Dp(4) - gtx.Dp(20) - gtx.Dp(9) - sw
			cy := fy + gtx.Dp(40)/2
			func() {
				defer op.Offset(image.Pt(x+(sw-gtx.Dp(18))/2, cy-gtx.Dp(9))).Push(gtx.Ops).Pop()
				v.icon(gtx, "send", 18, faint)
			}()
			fill(gtx, image.Rect(x+sw+gtx.Dp(4), cy-gtx.Dp(10), x+sw+gtx.Dp(4)+max(1, gtx.Dp(1)), cy+gtx.Dp(10)), v.p.Border)
			defer op.Offset(image.Pt(x+sw+gtx.Dp(9), cy-gtx.Dp(8))).Push(gtx.Ops).Pop()
			v.icon(gtx, "chevron-down", 16, faint)
		}()
		return D{Size: r.Max}
	})
}

// thrFmtIcons draws the formatting bar of Slack's sticky toolbar
// (p-texty_sticky_formatting_bar): 28x28 buttons every 32px, the 44px
// Paragraph button (icon + 12px caret) and 1px separators with 6px on
// either side.
func (v *Threads) thrFmtIcons(gtx C, x, bandH int, col color.NRGBA) {
	is := gtx.Dp(18)
	for _, n := range thrToolbar {
		if n == "|" {
			x += gtx.Dp(2) // 6px after the last button (which took its 4px gap)
			fill(gtx, image.Rect(x, bandH/2-gtx.Dp(10), x+max(1, gtx.Dp(1)), bandH/2+gtx.Dp(10)), v.p.Border)
			x += max(1, gtx.Dp(1)) + gtx.Dp(6)
			continue
		}
		bw := gtx.Dp(28)
		ix := x + (bw-is)/2
		if n == "paragraph" {
			bw = gtx.Dp(44)
			ix = x + gtx.Dp(5)
			st := op.Offset(image.Pt(ix+is+gtx.Dp(1), (bandH-gtx.Dp(12))/2)).Push(gtx.Ops)
			v.icon(gtx, "chevron-down", 12, col)
			st.Pop()
		}
		st := op.Offset(image.Pt(ix, (bandH-is)/2)).Push(gtx.Ops)
		v.icon(gtx, n, 18, col)
		st.Pop()
		x += bw + gtx.Dp(4)
	}
}

// thrIcons draws a row of 18px icons in 32px cells (≈14px apart) starting
// at at, vertically centered in a band of height bandH; "|" draws a 1px
// separator.
func (v *Threads) thrIcons(gtx C, at image.Point, bandH int, names []string, col color.NRGBA) {
	cell, is := gtx.Dp(32), gtx.Dp(18)
	x := at.X
	for _, n := range names {
		if n == "|" {
			x += gtx.Dp(4)
			fill(gtx, image.Rect(x, at.Y+bandH/2-gtx.Dp(10), x+max(1, gtx.Dp(1)), at.Y+bandH/2+gtx.Dp(10)), v.p.Border)
			x += gtx.Dp(5)
			continue
		}
		st := op.Offset(image.Pt(x+(cell-is)/2, at.Y+(bandH-is)/2)).Push(gtx.Ops)
		v.icon(gtx, n, 18, col)
		st.Pop()
		x += cell
	}
}

// dmPartner names a DM thread after its first non-viewer participant.
func (v *Threads) dmPartner(root *core.Message, replies []core.Message) string {
	self := v.selfID()
	if root.User != "" && root.User != self {
		return v.userName(root.User)
	}
	for _, id := range root.ReplyUsers {
		if id != self {
			return v.userName(id)
		}
	}
	for i := range replies {
		if replies[i].User != "" && replies[i].User != self {
			return v.userName(replies[i].User)
		}
	}
	return "Direct message"
}

// botName finds a bot's display name on one of the thread's messages.
func botName(id string, root *core.Message, replies []core.Message) string {
	if root.BotID == id && root.Name != "" {
		return root.Name
	}
	for i := range replies {
		if replies[i].BotID == id && replies[i].Name != "" {
			return replies[i].Name
		}
	}
	return ""
}

// --- Slack boundaries (docs/PLUGINS-components.md §1.1, §5) ---

// rootProps: ThreadRootGeneric {msg}.
func (s *thrMsg) rootProps() slackc.Props {
	return slackc.Props{"$key": "root-" + s.m.Channel + "-" + s.m.TS, "msg": msgProps(s.m)}
}

// layoutRoot draws the thread's root message (ThreadRootGeneric's native),
// from the plugin's msg when it overrides it (ShowRealUser).
func (s *thrMsg) layoutRoot(gtx C, p slackc.Props) D {
	m := s.m
	if mp := p.Map("msg"); mp != nil {
		om := msgOverride(m, mp)
		m = &om
	}
	return s.v.msg(gtx, s, m, s.top)
}

// senderProps: ThreadSenderAndTimestampGeneric {msg, adjacent, visible,
// omitTimestamp, omitLinebreak, timestampLabel}. Clack draws the body on
// its own line, so omitLinebreak is set.
func (s *thrMsg) senderProps() slackc.Props {
	return slackc.Props{"$key": "sender-" + s.m.Channel + "-" + s.m.TS, "msg": msgProps(s.m), "adjacent": false,
		"visible": true, "omitTimestamp": false, "omitLinebreak": true, "keepSenderAccessible": false,
		"timestampLabel": s.stamp}
}

// layoutSender is the name and time line. Under a patch it honours
// children, visible=false, omitTimestamp and a msg override (the name).
func (s *thrMsg) layoutSender(gtx C, p slackc.Props) D {
	st := thrSenderClass.Enter(gtx, 0, 0)
	defer st.Leave()
	if st.Hidden() {
		return D{}
	}
	if e, ok := slackc.PropChildren(p); ok {
		return slackc.LayoutElement(gtx, e)
	}
	name, stamp := s.name, s.stamp
	if p != nil {
		if p.Has("visible") && !p.Bool("visible") {
			return D{}
		}
		if p.Bool("omitTimestamp") {
			stamp = ""
		}
		if mp := p.Map("msg"); mp != nil && s.m != nil {
			om := msgOverride(s.m, mp)
			if n, _ := s.v.authorOf(&om); n != "" {
				name = n
			}
		}
	}
	if !st.Active() {
		return s.line(gtx, st, name, stamp)
	}
	return st.Wrap(gtx, func(gtx C) D { return s.line(gtx, st, name, stamp) })
}

func (s *thrMsg) line(gtx C, st slackc.Styled, name, stamp string) D {
	v := s.v
	col, size := st.Color(v.p.Text), st.FontSize(15)
	return row(gtx, layout.Baseline,
		layout.Rigid(func(gtx C) D { return v.para(gtx, size, 22, wBlack, col, 1, name) }),
		hgap(7),
		layout.Rigid(func(gtx C) D {
			if stamp == "" {
				return D{}
			}
			return v.para(gtx, 12, 17.6, wReg, v.meta(), 1, stamp)
		}),
	)
}

// ---- keyboard (ThreadsView over the virtual list) ----

// kbItems are get_rows_and_keys' rows: per thread heading-, root-, then
// load_older- when older replies are hidden, a row per reply and the
// footer- (the reply box, left out of the roving tab index). All of them
// are focusable (isFocusableItem); the list's own footer isn't a row.
func (v *Threads) kbItems(items []thrItem) []vkItem {
	clear(v.kbRows)
	out := make([]vkItem, 0, len(items)*5)
	add := func(key string, r thrKBRow, elem int) {
		v.kbRows[key] = r
		out = append(out, vkItem{key: key, elem: elem, focusable: true, noInitial: r.kind == "footer"})
	}
	for i := range items {
		t := &items[i]
		k := t.key
		add("heading-"+k, thrKBRow{kind: "heading", key: k, ch: t.Channel}, i)
		add("root-"+k, thrKBRow{kind: "root", key: k, ch: t.Channel, ts: t.Root.TS}, i)
		if c := v.cards[k]; c != nil && c.moreText != "" {
			add("load_older-"+k, thrKBRow{kind: "load_older", key: k, ch: t.Channel}, i)
		}
		for _, m := range thrShown(t, v.cards[k]) {
			add(k+"-"+m.TS, thrKBRow{kind: "reply", key: k, ch: t.Channel, ts: m.TS}, i)
		}
		add("footer-"+k, thrKBRow{kind: "footer", key: k, ch: t.Channel}, i)
	}
	return out
}

func (v *Threads) item(key string) (*thrItem, *thrCard) {
	p, _, _ := v.data.get()
	for i := range p.items {
		if p.items[i].key == key {
			return &p.items[i], v.cards[key]
		}
	}
	return nil, nil
}

// kbClick is onItemClick (Enter, after onItemKeyDown): a load_older row
// shows the older replies.
func (v *Threads) kbClick(key string) {
	if r := v.kbRows[key]; r.kind == "load_older" {
		if t, c := v.item(r.key); t != nil && c != nil {
			v.loadOlder(t, c)
		}
	}
}

// kbFocus is onItemFocusEnter: the footer row hands focus to its reply box.
func (v *Threads) kbFocus(key string, _ bool) {
	r := v.kbRows[key]
	if r.kind != "footer" {
		return
	}
	if t, c := v.item(r.key); t != nil && c != nil {
		v.openReply(t, c)
	}
}

// focusAboveFooter is focusItemAboveInput.
func (v *Threads) focusAboveFooter(thread string) {
	for i, it := range v.kb.items {
		if it.key == "footer-"+thread && i > 0 {
			v.kb.focusOn(v.kb.items[i-1].key)
			v.h.Invalidate()
			return
		}
	}
}

// kbKey handles onItemKeyDown (Enter on a root or reply jumps to the
// thread's root message in the channel), keyCommands (→ opens the active
// root's or reply's thread) and the view's onKeyDown: a printable key
// moves focus to the keyboard thread's reply box, which takes the
// character.
func (v *Threads) kbKey(gtx C, e key.Event, active string) bool {
	r := v.kbRows[active]
	switch e.Name {
	case key.NameReturn, key.NameEnter:
		if (r.kind == "root" || r.kind == "reply") && active != "" {
			if t, _ := v.item(r.key); t != nil {
				v.h.Navigate("c/" + t.Channel + "/m/" + t.Root.TS)
				return true
			}
		}
		return false
	case key.NameRightArrow:
		if (r.kind == "root" || r.kind == "reply") && r.ts != "" {
			if t, _ := v.item(r.key); t != nil {
				v.h.OpenThread(t.Channel, t.Root.TS)
			}
		}
		return true
	}
	ch, ok := thrPrintable(e)
	if !ok {
		return false
	}
	thread := v.kbRows[v.kb.initial].key
	if thread == "" {
		thread = r.key
	}
	t, c := v.item(thread)
	if t == nil || c == nil {
		return true
	}
	v.openReply(t, c)
	if c.comp != nil && ch != "" {
		c.comp.InsertText(ch)
	}
	v.kb.setActive("footer-"+thread, true)
	gtx.Execute(op.InvalidateCmd{})
	return true
}

// thrPrintable is isPrintableKeyEvent (a one-character key without ⌘ or
// Ctrl) for the keys Gio names; the text is what the key types when it's
// known (letters, digits, space, unshifted punctuation).
func thrPrintable(e key.Event) (string, bool) {
	if e.Modifiers.Contain(key.ModCtrl) || e.Modifiers.Contain(key.ModCommand) || e.Modifiers.Contain(key.ModSuper) {
		return "", false
	}
	if e.Name == key.NameSpace {
		return " ", true
	}
	n := string(e.Name)
	if len(n) != 1 {
		return "", false
	}
	c := n[0]
	shift := e.Modifiers.Contain(key.ModShift)
	switch {
	case c >= 'A' && c <= 'Z':
		if shift {
			return n, true
		}
		return strings.ToLower(n), true
	case shift:
		return "", true // the shifted symbol isn't known: focus only
	}
	return n, true
}

// typeFilters are the keys ThreadsView's onKeyDown and keyCommands read
// while the list is focused.
func (v *Threads) typeFilters() []key.Filter {
	if v.keyFs != nil {
		return v.keyFs
	}
	fs := []key.Filter{{Focus: v.kb, Name: key.NameRightArrow}, {Focus: v.kb, Name: key.NameSpace, Optional: key.ModShift}}
	for c := 'A'; c <= 'Z'; c++ {
		fs = append(fs, key.Filter{Focus: v.kb, Name: key.Name(string(c)), Optional: key.ModShift})
	}
	for _, s := range strings.Split("0 1 2 3 4 5 6 7 8 9 , . / ; ' [ ] \\ - = `", " ") {
		fs = append(fs, key.Filter{Focus: v.kb, Name: key.Name(s), Optional: key.ModShift})
	}
	v.keyFs = fs
	return fs
}
