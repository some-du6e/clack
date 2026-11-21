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
// reply box) marks it read, and so does a new reply that lands on screen
// while the viewer's attention is on the view (markNewReplies).
// Measurements are from /tmp/ui-views-spec/threads.md.

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"slices"
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
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/chanent"
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/ctxclick"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const (
	thrLimit             = 8           // loadThreads' refreshThreads {limit: 8}
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
	// seq is the message change (core.MessageChangeSeq) the thread's
	// copies are current as of (threads_live.go)
	seq uint64
	// window is the first reply of the latest getView window: the
	// replies before it a refresh carried over (thrRefreshed)
	window string
}

type thrData struct {
	items   []thrItem
	next    string
	hasMore bool
	convs   map[string]core.ConvRef
	// unreads is the answer's threads_state.has_unreads (Slack's
	// userHasUnreadThreads)
	unreads bool
	// load counts the loads that sorted the list afresh (Slack's
	// refreshId): a realtime refresh or the next page keeps it
	load int
	// seq and mode stamp the answer with the fetch that brought it, so
	// settle takes each answer once and in its own mode, whenever it
	// lands (Threads.settled)
	seq  int
	mode actLoad
	// gaps are the cards a refresh's window starts after: replies may have
	// come between the card's newest and the window's first that neither
	// has (thrRefreshed); settle fetches them (fillGap)
	gaps []thrGap
}

// thrGap is a span a card may be missing: the replies of thread key after
// its card's newest (after) and before the refresh window's first (before).
type thrGap struct{ key, after, before string }

// thrLineDrawn is a New line as a frame drew it (Threads.lines).
type thrLineDrawn struct {
	col   color.NRGBA
	ghost bool
}

// thrCard is the per-thread widget state and cached header text.
type thrCard struct {
	title, more, reply     widget.Clickable
	msgs                   map[string]*thrMsg // per message ts
	itemKey                string             // data-item-key: <channel>-<root ts>
	name, people, moreText string
	also                   string // "Also send to #general"
	built                  bool
	gen                    uint64 // i18n.Gen the strings were made in
	// waits are the members (and bots) name and people left out for
	// want of a name (missing_member.go): "names" listing one makes the
	// card again (membersIn)
	waits []string
	// icon is getChannelIconType's name for a channel's header
	// (InlineChannelEntity), "" for a DM or group DM; spans the name a
	// file channel's entity draws (ChannelName: a canvas's title, a
	// name's emoji), nil for text
	icon  string
	spans []richtext.Span

	comp  *composer.Composer // the real reply composer, made on first click
	compY int                // composer top within the list element (px)
	// pane is the composer's slackc pane ("threads:" + the thread's key):
	// its plugin roots are its own, not the thread pane's composer's
	pane string

	// "Show N more replies" (threads_older.go): the older replies loaded
	// into the card, oldest first; a fetch running; the thread's start
	// reached (has_more false); the fetch's answer for the UI goroutine
	older      []core.Message
	olderBusy  bool
	olderStart bool
	olderRes   atomic.Pointer[thrOlder]
	// folds is the view's fold stamp when the card was made or last went
	// back to its default first reply (fold): a stamp no other card and
	// no remount repeats (Threads.folds), so a fetch asked for before it
	// lands nowhere
	folds uint64
	// mid is the replies fillGap brought between the card's replies and a
	// later refresh's window, oldest first, current as of message change
	// midSeq: they take what changed after it as the older ones do
	// (threads_live.go). One gap fetch at a time (gapOut): a refresh's gap
	// while one is out widens gapNext, fetched when it lands; a failure
	// cuts the card to its latest window, with floor the window's first
	// reply for the answers asked for before the cut (fetch seq up to
	// floorSeq), which may still carry older ones over, and gapBack the
	// time the card's gaps cut it so at once instead of fetching
	mid      []core.Message
	midSeq   uint64
	gapOut   bool
	gapNext  thrGap
	gapBack  time.Time
	floor    string
	floorSeq int

	// olderSeq is the message change older is current as of
	// (threads_live.go); shown is thrShown's older, filled and latest
	// replies, kept while none of them changes (shownOf: the latest
	// replies it was made with; whatever changes older or mid drops it)
	olderSeq       uint64
	shown, shownOf []core.Message
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
	missTip     missTip // the sender's BaseMissingMember tooltip
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
	// readKey is firstReadThreadKey for the load that sorted the list
	// (readLoad, the data's load): the first shown thread with nothing
	// unread, picked once, in that load's first frame or, while every
	// thread is unread, when one is read
	readKey  string
	readLoad int
	loads    int  // the loads started that sort the list afresh (thrData.load)
	left     bool // the route moved off the view (PageLeft): it mounts again
	// initial is initialLastReadByThread for the list's load (initLoad):
	// the last read each thread had when it came into the list unread, ""
	// for one read then or never read (threads_order.go noteEntries)
	initial  map[string]string
	initLoad int
	// mounting is isDoingGlobalRefresh: a new visit's load is out (the
	// floating loader, no divider); loadErr is the last failed load that
	// sorts the list (Slack's errored: the error view over the threads)
	mounting bool
	loadErr  error
	// named are the threads replies and thread_subscribed named since the
	// last load that sorts the list, each with the order it was named in
	// (guarded by mu, as namedSeq): a refresh puts on top only those
	// (thrRefresh), as maybeUpdateAllThreadsOnNewMessage does
	named    map[string]uint64
	namedSeq uint64
	// reconn is maybeRefreshAllThreads' reload waiting for the frame: a
	// slow socket connect came while the view showed (guarded by mu)
	reconn bool
	// maxSeen is each thread's newest reply and last read as the last
	// load left them, the props before Slack's componentDidUpdate; grew is
	// the threads the load that just landed moved on (their maxSeen
	// before it), waiting for the frame that lays them out
	// (markNewReplies)
	maxSeen, grew map[string]thrSeen
	// settled is the answer settle took last (thrData.seq); seq counts the
	// fetches started
	settled, seq int
	// markVisible's runs (maybeMarkVisibleThreadAsRead): those due (a
	// load's 350 ms on, a scroll's), the list's last position, when it
	// last moved and whether the scroll's trailing call is to come, and
	// the viewer's attention in the last frame the view drew (attSeen: it
	// has drawn one since it mounted) and whether it came back since
	visAt      []time.Time
	scrollPos  layout.Position
	scrollAt   time.Time
	scrollTail bool
	attWas     bool
	attSeen    bool
	attBack    bool
	// attLost is the host's count of the times the viewer's attention
	// left (AttentionLost) as of the last frame: one a hidden window
	// never drew still makes the next attentive frame an edge
	attLost uint64
	// folds stamps the cards: each one made or folded takes the next
	// (thrCard.folds), so no fetch outlives its card's state, across
	// remounts too
	folds uint64
	// lines is the New lines the last frame drew, by thread: their line's
	// colour and whether it was the ghost (what tests check as drawn)
	lines map[string]thrLineDrawn
	shown shownTracker
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

	// lv is the cards' messages as core's changes left them
	// (threads_live.go)
	lv thrLive
}

// MessageFocuser is optionally implemented by the Host: transitionFocusTo
// MESSAGE_PANE_MESSAGE_LIST with uiState.ts, the channel's message list
// taking the keyboard focus at message ts once it has it (after the
// Navigate that jumps there).
type MessageFocuser interface {
	FocusMessage(channel, ts string)
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
	v.mediaPane = "threads"
	return v
}

// match runs on the event goroutine. Slack's RTM-THREADS handlers: a new
// reply (messageReplied → "If all threads is open, maybe update it after a
// new message arrives") and thread_subscribed (threadSubscribed → the same
// thunk) fetch the thread into the list, which Clack does with a refresh;
// thread_marked patches the thread's last read in place; thread_unsubscribed
// only flips the root's subscribed flag and update_thread_state only the
// badges, so neither reloads. A slow socket connect reloads the list
// (maybeRefreshAllThreads). Replies and connects matter only while the view
// shows: Slack's ThreadsView is unmounted on another page, and the next
// visit loads afresh.
func (v *Threads) match(ev core.Event) bool {
	shown := v.shown.shown(v.h)
	if shown {
		v.noteNamed(ev) // the thread it names, for the refresh (threads_order.go)
	}
	switch ev.Type {
	case "connection":
		// k0ZD maybeRefreshAllThreads, on socket connected: nothing after a
		// fast reconnect (its payload), else the allThreads view open gets
		// 181D incrementAllThreadsRefreshCounter, which ThreadsView answers
		// with reloadThreads (reconnectDue); a slow connect missed the gap
		if ev.Connected && !ev.Fast && shown {
			v.mu.Lock()
			v.reconn = true
			v.mu.Unlock()
			v.h.Invalidate()
		}
		return false
	case "names":
		// the member fetcher (or bots.info) named people: the cards that
		// left one out make their header again (membersIn), as Slack's
		// ThreadParticipantList re-renders when the fetch upserts them
		if ids := ev.Users; len(ids) > 0 {
			v.notes.mu.Lock()
			v.notes.fns = append(v.notes.fns, func() { v.membersIn(ids) })
			v.notes.mu.Unlock()
			v.h.Invalidate()
		}
		return false
	case "message":
		return ev.Reply && shown
	case "update", "delete":
		// an edit, an app's chat.update, a reaction or a deletion of a
		// message a card shows (its root, its replies, the older ones it
		// loaded and the gaps it filled: noteLiveKeys): the next frame
		// reads it from core (threads_live.go), no reload; off screen the
		// next draw does
		if v.lv.wakes(ev) && v.lv.shown.shown(v.h) {
			v.h.Invalidate()
		}
		return false
	case "thread_subscription":
		return ev.Name == "follow" && shown
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

// membersIn makes the cards whose header left out a member (or bot)
// "names" lists (thrCard.waits) again: the store has them now, or knows
// they don't exist.
func (v *Threads) membersIn(ids []string) {
	for _, c := range v.cards {
		if c.built && namesAny(c.waits, ids) {
			c.built = false
		}
	}
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
	v.lv.shown.mark(v.h)
	v.frame()
	v.shown.mark(v.h)
	v.notes.drain(v.h)
	v.w.watch(v.h, v.match)
	v.applyLive() // threads_live.go
	v.applyMarks()
	v.settle() // a load that finished, before another can start and take its mode
	if v.left {
		v.left = false
		if v.data.started() {
			v.remount()
		}
	}
	// componentDidUpdate's attention edge, against the frame before: none
	// in the first frame of a mount (its props have no earlier attention).
	// Slack renders on the attention's every change; a window minimized
	// or hidden draws no frame without it, so the host's count of the
	// times it left tells one that went and came back between frames
	att, lost := v.attention(), v.attentionLost()
	v.attBack = v.attSeen && att && (!v.attWas || lost != v.attLost)
	v.attWas, v.attLost, v.attSeen = att, lost, true
	clear(v.lines)
	if !v.data.started() {
		v.load(actFresh)
	}
	if v.retry.Clicked(gtx) {
		v.tryAgain()
	}
	if v.retryMore.Clicked(gtx) {
		v.failedCursor = ""
	}
	if v.reconnectDue() {
		v.reloadThreads()
	}
	// not while a load that sorts the list is out: its staleness waits for
	// the next frame after it (due clears it)
	if !(v.data.busy() && v.mode == actFresh) && v.w.due(gtx, 5*time.Second) {
		v.load(actRefresh)
	}

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
		return layout.W.Layout(gtx, func(gtx C) D { return v.para(gtx, 18, 24, wBlack, v.p.Text, 1, nsThreads.T("Threads")) })
	})
	if border == thrBorderInset {
		hl := max(1, gtx.Dp(1))
		fill(gtx, image.Rect(0, h-hl, w, h), v.p.Border)
	}
	return D{Size: image.Pt(w, h)}
}

// settle takes in what the loads brought, at the top of each frame, of
// body and of every fetch: each answer once, in the mode of the fetch it
// came from (its stamp), whenever it landed, so one that lands between a
// frame's checks or just before the next fetch is still settled; then the
// end of the load that was out, a failure's errored and "Load more".
func (v *Threads) settle() {
	page, _, err := v.data.get()
	if page.seq != 0 && page.seq != v.settled {
		v.settled = page.seq
		v.took(page)
	}
	if !v.wasBusy || v.data.busy() {
		return
	}
	v.wasBusy = false
	if v.paging {
		v.paging = false
		if err != nil {
			v.failedCursor = v.pageCursor
		}
	}
	if err != nil && v.mode == actFresh {
		v.loadErr = err // loadThreads' errored, over whatever threads the view holds
	}
}

// took is an answer landing (settle). A load that sorts the list ends the
// global refresh and errored, its new refreshId folds every card, and
// 350 ms on loadThreads marks the threads whose unread row shows read
// (setTimeout(maybeMarkVisibleThreadAsRead, 350): markVisible). Another
// keeps the cards cut to their window since it went out (keepFloors),
// and a refresh's gaps are fetched (fillGap).
func (v *Threads) took(page thrData) {
	for _, c := range v.cards {
		c.built = false
	}
	if page.mode == actFresh {
		v.loadErr, v.mounting = nil, false
		for _, c := range v.cards {
			v.fold(c)
		}
		v.visAt = append(v.visAt, time.Now().Add(thrMarkDelay))
	} else {
		v.keepFloors(page) // threads_order.go
		page, _, _ = v.data.get()
	}
	for _, g := range page.gaps {
		v.fillGap(page, g) // threads_order.go
	}
	v.landed(page) // threads_order.go
}

// load fetches the threads: actFresh page 1 sorted afresh (the view's
// first load, Try Again), actRefresh page 1 again for realtime news,
// merged into the list in place (thrRefresh), actMore the next page.
func (v *Threads) load(mode actLoad) { v.fetch(mode, false) }

// reload is ThreadsView's loadThreads on a mount, or reloadThreads after
// its sidebar row is clicked again: page 1 sorted afresh, a new refreshId
// (a new "You’re up to date" thread). Slack's store keeps the threads
// loaded before, so they stay on screen until the answer replaces them,
// and the list keeps its scroll (nextScrollTop).
func (v *Threads) reload() { v.fetch(actFresh, true) }

func (v *Threads) fetch(mode actLoad, keep bool) {
	v.settle() // one that landed since the last frame: this load takes its mode
	c := v.h.Core()
	v.lv.log.start(c) // the messages change from here on (threads_live.go)
	old, _, _ := v.data.get()
	cursor := ""
	if mode == actMore {
		cursor = old.next
		v.pageCursor = cursor
	}
	load := old.load
	if mode == actFresh {
		v.loads++
		load = v.loads
		if !keep {
			for _, c := range v.cards {
				if c.comp != nil {
					c.comp.Close()
				}
			}
			v.cards = map[string]*thrCard{}
			v.list.Position = layout.Position{}
			v.scrollPos, v.scrollAt, v.scrollTail = layout.Position{}, time.Time{}, false // a new list, not a scroll
		}
		v.failedCursor = ""
		v.sortingLoad() // threads_order.go: its answer covers what came before it
	}
	named := v.namedNow(mode) // threads_order.go
	v.mode, v.paging, v.wasBusy = mode, mode == actMore, true
	v.seq++
	seq := v.seq
	v.data.load(v.h, mode != actFresh || keep, func(ctx context.Context) (thrData, error) {
		p, err := c.Threads(thrLoadCtx(ctx, mode), cursor, thrLimit)
		if err != nil {
			return thrData{}, err
		}
		fresh := thrFrom(p)
		switch mode {
		case actMore:
			fresh = thrJoin(old, fresh)
		case actRefresh:
			fresh = thrRefresh(old, fresh, named, thrCarryLive(c))
		default:
			fresh.load = load
		}
		fresh.seq, fresh.mode = seq, mode
		return fresh, nil
	})
}

// PageLeft is the route moving off the view (the ui root's afterRoute):
// Slack unmounts ThreadsView, and the next visit mounts it again. Its
// componentWillUnmount clears the threads marked unread
// (clearThreadsFromManuallyMarkedUnreadArray).
func (v *Threads) PageLeft() {
	v.left = true
	if app := v.h.Core(); app != nil {
		app.ForgetThreadMarkedUnread("", "")
	}
}

// remount is the view mounting again after another page (Slack's
// componentDidMount): a new list at its top with no active row, the
// store's threads sorted afresh with a new refreshId (resort) until
// loadThreads' answer replaces them, the floating loader and no divider
// meanwhile (isDoingGlobalRefresh). A new instance hasn't errored: a
// failure from the last visit shows no more.
func (v *Threads) remount() {
	for _, c := range v.cards {
		if c.comp != nil {
			c.comp.Close() // saves the draft, as the unmounted reply box did
		}
	}
	v.cards = map[string]*thrCard{}
	v.list.Position = layout.Position{}
	// a new list at its top: no scroll (componentDidMount's list has
	// scrolled nowhere), and no check before loadThreads' 350 ms
	v.scrollPos, v.scrollAt, v.scrollTail = layout.Position{}, time.Time{}, false
	v.kb.active, v.kb.initial, v.kb.ring = "", "", false
	v.mounting, v.loadErr = true, nil
	v.grew, v.visAt = nil, nil // a new instance compares only with its own props
	v.attSeen = false
	v.resort() // threads_order.go
	v.reload()
}

// tryAgain is ThreadsView's tryAgain, the error view's button: errored
// goes, and loadThreads runs again over the threads the view holds (a
// failed mount keeps its floating loader).
func (v *Threads) tryAgain() {
	v.loadErr = nil
	v.reload()
}

// Reselect is the sidebar's Threads row clicked while the view shows:
// AllThreadsRowElement's onClick dispatches
// incrementAllThreadsRefreshCounter when its row is selected
// (reloadThreads).
func (v *Threads) Reselect() { v.reloadThreads() }

// reloadThreads is incrementAllThreadsRefreshCounter landing (the
// sidebar row clicked again, a slow reconnect): ThreadsView's
// componentDidUpdate calls reloadThreads unless it is loading already
// (this.loading: its own load or the next page's). reloadThreads drops
// currentRefreshId first, so the threads are sorted afresh at once with a
// new divider thread (resort) and every card goes back to its default
// first reply (the older replies it loaded fold away, a fetch still out
// with them), then the list goes to its top and loadThreads runs.
func (v *Threads) reloadThreads() {
	if v.data.busy() && v.mode != actRefresh {
		return
	}
	v.resort()
	for _, c := range v.cards {
		v.fold(c)
	}
	v.list.Position = layout.Position{}
	v.reload()
	v.h.Invalidate()
}

// fold puts a card back to its default first reply: the older replies it
// loaded and the gaps it filled go, and a fetch for either still out lands
// nowhere (a new stamp). A failed gap's backoff stays (gapBack).
func (v *Threads) fold(c *thrCard) {
	c.older, c.olderStart, c.mid, c.shown, c.built = nil, false, nil, nil, false
	c.gapOut, c.gapNext, c.floor = false, thrGap{}, ""
	c.folds = v.nextFold()
}

// nextFold is a new card stamp (Threads.folds).
func (v *Threads) nextFold() uint64 {
	v.folds++
	return v.folds
}

// cardOf is thread t's card, made on first use.
func (v *Threads) cardOf(t *thrItem) *thrCard {
	c := v.cards[t.key]
	if c == nil {
		c = &thrCard{msgs: map[string]*thrMsg{}, itemKey: t.Channel + "-" + t.Root.TS, folds: v.nextFold()}
		v.cards[t.key] = c
	}
	return c
}

// thrFrom is an answer's threads in processThreads' order (thrSort).
func thrFrom(p core.ThreadsPage) thrData {
	d := thrData{next: p.Next, hasMore: p.HasMore, convs: p.Convs, unreads: p.State.Unread}
	d.items = make([]thrItem, 0, len(p.Threads))
	for _, t := range p.Threads {
		it := thrItem{key: t.Channel + "/" + t.Root.TS, Thread: t, seq: p.MsgSeq}
		if len(t.Replies) > 0 {
			it.window = t.Replies[0].TS
		}
		d.items = append(d.items, it)
	}
	thrSort(d.items)
	return d
}

// thrJoin is the next page b after the threads loaded, a: b's threads that
// aren't in a after a's, in b's own order, with b's cursor. Slack's
// appendMoreThreads appends the page's keys (uniq([...ordered, ...page]),
// the page sorted within itself by processThreads); nothing loaded before
// moves.
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
	return thrData{items: items, next: b.next, hasMore: b.hasMore, convs: actRefs(a.convs, b.convs), unreads: b.unreads, load: a.load}
}

// thrRefresh is a realtime refresh's page 1, fresh, over the threads
// listed, old. Slack sorts the list only on a load (a new refreshId) and
// takes realtime replies one at a time: a reply in a listed thread leaves
// it where it is, its card gaining the reply (maybeUpdateAllThreadsOnNewMessage
// returns early), and a followed thread the list doesn't have that a new
// reply or thread_subscribed names is fetched and put first
// (prependThread), its key going to the top of the order while the viewer
// has unread threads (ThreadsView's getDerivedStateFromProps). So listed
// threads keep their places with the page's copy of them (thrRefreshed),
// the page's threads in named go on top newest first, and threads the
// page doesn't have stay (a left channel's are drawn no more: shownItems).
// Its other threads stay out: one that only moved into page 1 because a
// listed one left it (unfollowed, its root deleted, its channel left) has
// no event, and comes with the next page, after the list, as
// appendMoreThreads brings it. An empty list takes the page as it is,
// sorted (an empty order is sorted again).
func thrRefresh(old, fresh thrData, named map[string]bool, carry thrCarry) thrData {
	if len(old.items) == 0 {
		fresh.load = old.load
		return fresh
	}
	at := make(map[string]int, len(fresh.items))
	for i, t := range fresh.items {
		at[t.key] = i
	}
	listed := make(map[string]bool, len(old.items))
	for _, t := range old.items {
		listed[t.key] = true
	}
	items := make([]thrItem, 0, len(old.items)+len(fresh.items))
	if fresh.unreads {
		for _, t := range fresh.items {
			if !listed[t.key] && named[t.key] {
				items = append(items, t)
			}
		}
		sort.SliceStable(items, func(i, j int) bool { return actTSLess(items[j].sortTS(), items[i].sortTS()) })
	}
	var gaps []thrGap
	for _, t := range old.items {
		if i, ok := at[t.key]; ok {
			var g thrGap
			var gap bool
			if t, g, gap = thrRefreshed(t, fresh.items[i], carry); gap { // threads_order.go
				gaps = append(gaps, g)
			}
		}
		items = append(items, t)
	}
	return thrData{items: items, next: old.next, hasMore: old.hasMore, convs: actRefs(old.convs, fresh.convs), unreads: fresh.unreads, load: old.load, gaps: gaps}
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

// markRead marks an unread thread read up to its latest reply (sendRead).
func (v *Threads) markRead(t *thrItem) {
	if v.unread(t) > 0 {
		v.sendRead(t)
	}
}

// sendRead is setThreadLastRead {lastRead: maxMarkableTs, read: true}:
// the thread read up to its latest reply, locally at once, then with
// subscriptions.thread.mark. A thread the viewer had marked unread leaves
// ThreadsMarkedUnread (removeThreadFromManuallyMarkedUnreadArray).
func (v *Threads) sendRead(t *thrItem) {
	ts := t.latest()
	v.read[t.key] = ts
	c, ch, root := v.h.Core(), t.Channel, t.Root.TS
	if c != nil {
		c.ForgetThreadMarkedUnread(ch, root)
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := c.MarkThread(ctx, ch, root, ts); err != nil {
			v.notes.add(v.h, "Couldn’t mark the thread as read: "+friendlyErr(err))
		}
	}()
}

// sortTS is the thread's newest activity, processThreads' sortTs: the
// newest reply the answer has (unread_replies, latest_replies; the root's
// latest_reply names it too), else the root.
func (t *thrItem) sortTS() string {
	ts := t.Root.TS
	if lr := t.Root.LatestReply; lr != "" && actTSLess(ts, lr) {
		ts = lr
	}
	if n := len(t.Replies); n > 0 && actTSLess(ts, t.Replies[n-1].TS) {
		ts = t.Replies[n-1].TS
	}
	return ts
}

// thrSort is the order Slack's client gives threads on every load, the
// server's whatever it was: processThreads (boot-shared zoZ+) sorts by
// `${hasUnreads ? 1 : 0}-${sortTs}` descending and ThreadsView's
// sortThreads (8orQ) by unread then maxMarkableTs the same way, so threads
// with unread replies come first, then each group newest activity first.
func thrSort(items []thrItem) {
	sort.SliceStable(items, func(i, j int) bool {
		if ui, uj := items[i].UnreadCount > 0, items[j].UnreadCount > 0; ui != uj {
			return ui
		}
		return actTSLess(items[j].sortTS(), items[i].sortTS())
	})
}

func (v *Threads) body(gtx C) D {
	v.settle() // an answer that landed since the frame began: drawn settled
	page, loaded, err := v.data.get()
	page.items = v.shownItems(page.items) // threads_order.go
	n := len(page.items)
	reloading := v.data.busy() && v.mode == actFresh
	if v.loadErr != nil || n == 0 {
		// no list: nothing to judge replies against (Slack's early return
		// without a listRef, the errored view); maxSeen keeps up, and a
		// mark check due finds no list and is spent (threads_order.go)
		v.grew = nil
		v.spendChecks(gtx)
	}
	switch {
	case v.loadErr != nil: // errored: the view's error state, whatever threads it holds
		return v.emptyState(gtx, "", nsThreads.T(thrErrText), friendlyErr(v.loadErr), &v.retry, nsThreads.T("Try Again"))
	case n == 0 && (!loaded || reloading && (v.mounting || err != nil)):
		// no threads to show while the first load, a mount's or Try
		// Again's is out: the loading view, not a stale error's
		return layout.Inset{Top: 16}.Layout(gtx, func(gtx C) D { return v.skeleton(gtx, 5, 36) })
	case err != nil && n == 0:
		return v.emptyState(gtx, "", nsThreads.T(thrErrText), friendlyErr(err), &v.retry, nsThreads.T("Try Again")) // MessageContainerError
	case n == 0:
		return v.empty(gtx)
	}
	failed := v.failedCursor != "" && v.failedCursor == page.next
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	clear(v.sizes)
	if page.load != v.readLoad { // sorted afresh: a new refreshId, a new divider thread
		v.readKey, v.readLoad = "", page.load
	}
	v.noteEntries(page) // threads_order.go
	div := v.firstRead(page)
	v.divAt = div
	v.kb.setItems(v.kbItems(page.items))
	v.kb.update(gtx, gtx.Constraints.Max.Y, vkOpts{Click: v.kbClick, Key: func(e key.Event, active string) bool { return v.kbKey(gtx, e, active) },
		Text: func(text, active string) { v.kbText(gtx, text, active) }, Extra: v.typeFilters()})
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
	if p := v.list.Position; v.mounting && p.First == 0 {
		// the "global-loading" row (threads_order.go), in the list
		st := clip.Rect(image.Rectangle{Max: d.Size}).Push(gtx.Ops)
		v.floatingLoader(gtx, d.Size.X, -p.Offset)
		st.Pop()
	}
	if v.markNewReplies(page) { // threads_order.go
		gtx.Execute(op.InvalidateCmd{})
	}
	if v.visibleDue(gtx) && v.markVisible(page) { // threads_order.go
		gtx.Execute(op.InvalidateCmd{})
	}
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
					return v.th.Layout(gtx, theme.TextStyle{Size: 18, LineHeight: 24, Weight: wBlack, Color: v.p.Text, Align: text.Middle}, nsThreads.T(thrEmptyTitle))
				})
			}),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D {
					return v.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: v.p.Sec, Align: text.Middle}, nsThreads.T(thrEmptyBody))
				})
			}),
			layout.Rigid(func(gtx C) D { return v.linkButton(gtx, &v.learn, 15, nsThreads.T("Learn about threads")) }),
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
// the first thread that was already read when the view loaded (where and
// when: firstRead). It's a 1px #dddddd line with the 700 #616061 label on
// the page background in a rounded pill (padding 4px 12px); margin 32px
// above, 20px below.
func (v *Threads) upToDate(gtx C) D {
	w := gtx.Constraints.Max.X
	top, bottom := gtx.Dp(32), gtx.Dp(20)
	label := nsThreads.T(thrUpToDate)
	lw := v.th.Measure(gtx, 15, wBold, label)
	pw, ph := lw+gtx.Dp(24), gtx.Dp(22+8)
	y := top
	fill(gtx, image.Rect(gtx.Dp(16), y, w-gtx.Dp(16), y+max(1, gtx.Dp(1))), v.p.Border)
	x := (w - pw) / 2
	rrect(gtx, image.Rect(x, y-ph/2, x+pw, y+ph/2), ph/2, v.p.Page)
	func() {
		defer op.Offset(image.Pt(x+gtx.Dp(12), y-ph/2+gtx.Dp(4))).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(lw+1, gtx.Dp(22))}
		v.para(g, 15, 22, wBold, v.meta(), 1, label)
	}()
	return D{Size: image.Pt(w, top+bottom)}
}

// firstRead is the index of the thread Slack puts the "You’re up to
// date" divider before, or -1. That thread is firstReadThreadKey
// (getDerivedStateFromProps): the first one in the order with nothing
// unread among the threads mapStateToProps shows (page holds only those),
// picked in the first frame of the load that sorts the list and kept
// through realtime refreshes and pages, whatever they bring to it. While
// there is none, the first thread read since is picked (Slack looks again
// while it has none). get_rows_and_keys draws the divider unless
// the view's load is out (isDoingGlobalRefresh), the thread is the first
// one, or a thread from it on (dropWhile, NbLH: never one above it) has a
// reply from someone else newer than the last read it came into the list
// with (initialLastReadByThread: unread then, read before). Threads sort
// unread first, so after a load the divider shows at once, whatever is
// unread above it; a page bringing an unread thread in below hides it.
func (v *Threads) firstRead(page thrData) int {
	items := page.items
	key := v.readKey
	if key == "" {
		for i := range items {
			if v.unread(&items[i]) == 0 {
				v.readKey, key = items[i].key, items[i].key
				break
			}
		}
	}
	at := -1
	for i := range items {
		if items[i].key == key && key != "" {
			at = i
			break
		}
	}
	if at <= 0 || v.mounting {
		return -1
	}
	self := v.selfID()
	for j := at; j < len(items); j++ {
		lr := v.initial[items[j].key]
		if lr == "" {
			continue
		}
		for _, m := range thrShown(&items[j], v.cards[items[j].key]) { // the thread's messages
			if m.User != self && actTSLess(lr, m.TS) {
				return -1
			}
		}
	}
	return at
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
// Grace, and you", or "Ada, Grace, Linus, and 4 others". A participant
// without a name yet (getPrefCompliantMemberNameById's "" for the stub,
// a bot whose name isn't loaded) is left out and asked for, as
// getThreadRepliersNamesFromState (client-boot V4VG) leaves them out;
// "names" makes the card again.
func (v *Threads) people(root *core.Message, replies []core.Message) string {
	self := v.selfID()
	seen := map[string]bool{}
	var names []string
	all, hasSelf := 0, false
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		all++
		if id == self {
			hasSelf = true
			return
		}
		if !core.LooksLikeMemberID(id) {
			// a bot id (ReplyUsers can hold them): the name its messages
			// carry, else getBotNameById's; never the raw B… id
			if n := botName(id, root, replies); n != "" {
				names = append(names, n)
			} else if n := v.personName(id); n != "" {
				names = append(names, n)
			}
			return
		}
		if n := v.userName(id); n != "" {
			names = append(names, n)
		}
	}
	if root.User == "" && !slices.Contains(root.ReplyUsers, root.BotID) {
		// a bot's root without a user: its bot first (mapStateToProps
		// puts the root's bot_id before the reply users without it)
		if n, _ := v.authorOf(root); root.BotID != "" || n != "" {
			seen[root.BotID] = true
			all++
			if n != "" {
				names = append(names, n)
			}
		}
	}
	add(root.User)
	for _, id := range root.ReplyUsers {
		add(id)
	}
	for i := range replies {
		add(replies[i].User)
	}
	return thrParticipants(names, hasSelf, all)
}

// thrParticipants is Slack's ThreadParticipantList (client-boot-shared
// oYYP) over getThreadRepliersNamesFromState's names (names: the named
// participants other than the viewer; hasSelf: the viewer is one, named
// "you", last; all: how many participants there are, named or not).
// useSimpleNameList is all of them three or fewer: getSimpleNameList
// ("Just you", "{userName} and you", "{userName1}, {userName2}, and
// you", else listify's "Ada and Grace"); past three getOverflowNameList
// ("{userName1}, {userName2}, and # others"). Both count only the named
// (replyUsersCount becomes the names' count), so a thread with someone
// left out reads one person shorter until "names" brings them.
//
// One divergence: past three people with fewer than two of them named,
// Slack's overflow text has no second name ("undefined") and -1 others;
// Clack draws the simple list of the named ones until two are.
func thrParticipants(names []string, hasSelf bool, all int) string {
	j := slices.Clip(names)
	if hasSelf {
		j = append(j, nsThreads.T("you"))
	}
	n := len(j)
	switch {
	case n == 0:
		return ""
	case all > 3 && n >= 2:
		return i18n.StripTags(nsThreads.T("{userName1}, {userName2}, and <span>{numOthers,plural,=1{# other}other{# others}}</span>",
			"userName1", j[0], "userName2", j[1], "numOthers", n-2))
	case !hasSelf:
		return listify(names, false)
	case n == 1:
		return nsThreads.T("Just you")
	case n == 2:
		return nsThreads.T("{userName} and you", "userName", names[0])
	}
	return nsThreads.T("{userName1}, {userName2}, and you", "userName1", names[0], "userName2", names[1])
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
	defer func() {
		if l := i18n.Locale(); l != i18n.EnUS {
			s = msgTimeIn(l, t, now) // the same formatter's strings
		}
	}()
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
	c := v.cardOf(t)
	if c.built && (c.name == t.Channel || c.name == "#"+t.Channel || c.name == unknownChannelName() || c.name == "#"+unknownChannelName()) {
		// Built before core knew the conversation (early boot, or a
		// channel it's still looking up: convTitle's placeholder): retry.
		if n := v.convTitle(t.Channel, convs); n != c.name {
			c.built = false
		}
	}
	if c.built && c.icon == "file" && v.h.Core() != nil {
		// a file channel whose file files.info hadn't named: again once it
		// has (its "names" draws the page)
		if _, known := v.h.Core().S.KnownCanvasRef(core.FileOfChannel(t.Channel)); known {
			c.built = false
		}
	}
	if c.built && len(c.waits) > 0 && v.waitOver(c.waits) {
		// a member the card couldn't name is in the store now (or known
		// not to exist) before a "names" said so: make it again
		c.built = false
	}
	if !c.built || c.gen != i18n.Gen() || v.viewsPatched() {
		c.gen = i18n.Gen()
		c.waits = v.recording(func() {
			c.name = v.convTitle(t.Channel, convs)
			if strings.HasPrefix(t.Channel, "D") && (c.name == t.Channel || c.name == dmFallbackName()) {
				// a DM core doesn't know yet: name it after the other person
				c.name = v.dmPartner(&t.Root, t.Replies)
			}
			c.people = v.people(&t.Root, t.Replies)
		})
		c.moreText = thrMoreText(t, c)
		kind := ""
		if r, ok := convs[t.Channel]; ok {
			kind = r.Kind
		} else {
			kind = v.h.Core().ConvRef(t.Channel).Kind
		}
		// BroadcastControls: a DM "Also send as direct message", a
		// channel "Also send to <span>#name</span>"
		c.also = i18n.StripTags(tagged(nsThreads.T("Also send to <span></span>"), "span", c.name))
		if isDMKind(kind) {
			c.also = nsThreads.T("Also send as direct message")
		}
		c.icon, c.spans = channelIcon(v.h.Core(), t.Channel, kind), nil
		// a canvas's or a list's file channel is its InlineChannelEntity
		// (getChannelIconType's file channel icon, ChannelName's title or
		// emoji), never a private channel's lock and raw name
		if app := v.h.Core(); app != nil && app.IsFileChannelID(t.Channel) {
			if ic, ok := app.KnownChannel(t.Channel); ok {
				e := chanent.Of(t.Channel, ic, nil)
				c.icon, c.name, c.spans = e.Icon, e.Text, e.Spans
			}
		}
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
	if v.drawUnread(t) > 0 { // threads_order.go
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
							name := func(gtx C) D {
								if len(c.spans) > 0 {
									return v.canvasTitle(gtx, c.spans, 15, nameW, v.p.Text)
								}
								return v.para(gtx, 15, 22, nameW, v.p.Text, 1, c.name)
							}
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

// openReply swaps in the card's real composer and focuses it. Opening it
// marks nothing: the box's focus does what onFooterInputFocus does
// (footerFocus), and the threads the viewer passes on the way are marked
// by the check over what shows once the list is still (markVisible).
func (v *Threads) openReply(t *thrItem, c *thrCard) {
	ch, ok := v.h.(composer.Host)
	if !ok {
		v.h.OpenThread(t.Channel, t.Root.TS)
		return
	}
	if c.comp == nil {
		c.comp = composer.New(ch, t.Channel, t.Root.TS)
		c.pane = "threads:" + t.key
		c.comp.InThreadsView = true // .p-threads_view: no 12px over the box
		key := t.key
		// onUpArrowInEmptyInput: focusItemAboveInput
		c.comp.OnFocusList = func() { v.focusAboveFooter(key) }
		// onDownArrowInEmptyInput: focusItemBelowInput
		c.comp.OnDownWhenEmpty = func() { v.focusBelowFooter(key) }
		// the box's onFocus: onFooterInputFocus
		c.comp.OnFocus = func() { v.footerFocus(key) }
	}
	c.comp.Focus()
}

// footerFocus is onFooterInputFocus (threads-view.js 1521-1542), a card's
// reply box taking the focus: a thread the viewer marked unread by hand
// (from any container: threadsManuallyMarkedUnread) and follows is marked
// read at its newest reply, even with nothing unread, and the hand mark
// goes. Any other thread's box marks nothing, and a hand-marked thread
// nobody follows keeps its mark.
func (v *Threads) footerFocus(key string) {
	app := v.h.Core()
	t, _ := v.item(key)
	if app == nil || t == nil || app.ThreadMarkedUnread(t.Channel, t.Root.TS) == "" || !v.followed(t) {
		return
	}
	v.sendRead(t)
}

// card lays out the card's segments and returns its size and the top of
// the reply box within it.
func (v *Threads) card(gtx C, c *thrCard, t *thrItem, row func(key string, y, h int)) (D, int) {
	replies := thrShown(t, c)               // the loaded older replies, then the latest ones
	at, ghost := v.unreadRow(t, c, replies) // threads_order.go
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
			d := v.olderRow(gtx, c)
			if at == thrUnreadAtOlder { // the unread run starts among the hidden replies
				v.newLine(gtx, d.Size.X, t.key, ghost)
			}
			return rec("load_older-"+t.key, d)
		}),
		layout.Rigid(func(gtx C) D {
			return actStack(gtx, len(replies), func(gtx C, i int) D {
				m := &replies[i]
				d := v.msg(gtx, c.msg(m.TS), m, 8)
				if i == at {
					v.newLine(gtx, d.Size.X, t.key, ghost)
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
					return c.comp.LayoutIn(gtx, c.pane)
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

// newLine is Slack's UnreadDivider (VxYf) for thread key's unread row,
// drawn over the top edge of the row it goes before: a 1px #e01e5a line
// with a bold 13px "New" at its right end (padding 0 4px, 15px from the
// edge), as in the channel view. A ghost one ("unread_marked_read":
// c-message_list__unread_divider--ghost) keeps its label, and its line
// is at half opacity from its first frame (ghostLine).
func (v *Threads) newLine(gtx C, w int, key string, ghost bool) {
	red := v.th.C.NewLine
	line := red
	if ghost {
		line = v.ghostLine(red) // threads_order.go
	}
	if v.lines == nil {
		v.lines = map[string]thrLineDrawn{}
	}
	v.lines[key] = thrLineDrawn{line, ghost}
	newTxt := nsMessages.T("New") // UnreadDivider's
	tw := v.th.Measure(gtx, 13, wBold, newTxt)
	lx := w - gtx.Dp(15) - gtx.Dp(4) - tw
	fill(gtx, image.Rect(0, 0, lx-gtx.Dp(4), max(1, gtx.Dp(1))), line)
	defer op.Offset(image.Pt(lx, -gtx.Dp(9))).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints.Min = image.Point{}
	v.para(g, 13, 19.5, wBold, red, 1, newTxt)
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
			v.para(g, 15, 22, wReg, soft, 1, nsThreads.T("Reply…"))
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
	return dmFallbackName()
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
	sender := func(gtx C) D { return v.para(gtx, size, 22, wBlack, col, 1, name) }
	if name == "" && s.m != nil && core.LooksLikeMemberID(s.m.User) {
		// BaseMessageSender: BaseMissingMember for a sender the store
		// can't name, "unknown" while users/info runs, "non-existent"
		// (outlined) once it didn't return them (missing_member.go)
		if miss := v.memberState(s.m.User); miss != core.MemberKnown {
			sender = func(gtx C) D {
				return v.missingBar(gtx, miss == core.MemberNonExistent, &s.missTip, func(gtx C) D {
					return v.para(gtx, size, 22, wBlack, col, 1, "")
				})
			}
		}
	}
	return row(gtx, layout.Baseline,
		layout.Rigid(sender),
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

// focusBelowFooter is focusItemBelowInput (client-threads-view): the
// key after the thread's footer, when there is one.
func (v *Threads) focusBelowFooter(thread string) {
	for i, it := range v.kb.items {
		if it.key == "footer-"+thread && i+1 < len(v.kb.items) {
			v.kb.focusOn(v.kb.items[i+1].key)
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
				// jumpToMessage({channelId, ts: threadTs}).then(
				// transitionFocusTo MESSAGE_PANE_MESSAGE_LIST with
				// uiState.ts: the root): the channel's list takes the
				// keyboard focus at the root once the jump has it
				v.h.Navigate("c/" + t.Channel + "/m/" + t.Root.TS)
				if f, ok := v.h.(MessageFocuser); ok {
					f.FocusMessage(t.Channel, t.Root.TS)
				}
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
	if !thrPrintable(e) {
		return false
	}
	v.typeInto(gtx, active, "")
	return true
}

// kbText is the text the key typed (key.EditEvent, after the key.Event):
// the browser types it into the reply box onKeyDown just focused, so a
// shifted symbol or another layout's character arrives as typed. Text
// without a key Clack names (a dead key's letter, an IME's) runs
// onKeyDown's move as well: isPrintableKeyEvent saw a one-character key.
func (v *Threads) kbText(gtx C, text, active string) {
	v.typeInto(gtx, active, text)
}

// typeInto is onKeyDown's move for a printable key: the keyboard thread's
// footer (focusOnKey footer-…, which scrolls it into view) and its reply
// box take the focus, and the box takes text. The list doesn't take the
// focus back (focusOn would): the box has it, as focusInput gave it.
func (v *Threads) typeInto(gtx C, active, text string) {
	thread := v.kbRows[v.kb.initial].key
	if thread == "" {
		thread = v.kbRows[active].key
	}
	t, c := v.item(thread)
	if t == nil || c == nil {
		return
	}
	v.openReply(t, c)
	if c.comp != nil && text != "" {
		c.comp.InsertText(text)
	}
	footer := "footer-" + thread
	v.kb.setActive(footer, true)
	if !v.kb.inView(footer) {
		v.kb.scrollTo(footer)
	}
	gtx.Execute(op.InvalidateCmd{})
}

// thrPrintable is isPrintableKeyEvent (client-boot 3NzO: e.key is one
// character, no ⌘ or Ctrl) for the keys Gio names. The character itself
// comes after, as the key.EditEvent the OS sends (kbText).
func thrPrintable(e key.Event) bool {
	if e.Modifiers.Contain(key.ModCtrl) || e.Modifiers.Contain(key.ModCommand) || e.Modifiers.Contain(key.ModSuper) {
		return false
	}
	return e.Name == key.NameSpace || len(e.Name) == 1
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
