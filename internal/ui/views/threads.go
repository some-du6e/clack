package views

// Threads: Slack's Threads view (Home sidebar "Threads"). A 49px header,
// then on a #f8f8f8 background one block per followed thread (unread
// first): a header line outside the card (channel link + participants)
// and a white card (radius 12, 1px #dddddd) holding the root message,
// "Show N more replies", the latest replies and an inline composer.
// Until it's clicked the composer is a cheap picture of Slack's (toolbar,
// "Reply to thread…", "Also send to", footer); the click swaps in the
// real composer (internal/ui/composer) for that card. Threads with unread
// replies get a black-weight channel name and Slack's red "New" line
// above the first unread reply; opening a thread (its replies link or its
// reply box) marks it read. Measurements are from
// /tmp/ui-views-spec/threads.md.

import (
	"context"
	"image"
	"image/color"
	"sort"
	"strconv"
	"strings"
	"time"

	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/composer"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
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
	thrToolbar = []string{"bold", "italic", "strike", "|", "link", "list-ordered", "list-bullet", "|", "quote", "code", "code-block"}
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

	comp  *composer.Composer // the real reply composer, made on first click
	compY int                // composer top within the list element (px)
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

	// read is the local read state: thread key -> latest reply ts marked
	// read (Slack's view lags behind subscriptions.thread.mark).
	read  map[string]string
	sizes map[int]int // list element heights this frame (composer origins)
	notes actNotes
}

// NewThreads builds the Threads view; it loads on first Layout.
func NewThreads(h Host) *Threads {
	v := &Threads{base: newBase(h), cards: map[string]*thrCard{}, read: map[string]string{}, sizes: map[int]int{}}
	v.list.Axis = layout.Vertical
	return v
}

func thrMatch(ev core.Event) bool {
	return ev.Type == "message" && ev.Reply || ev.Type == "thread_marked" || ev.Type == "update_thread_state"
}

// Layout draws the view.
func (v *Threads) Layout(gtx C) D {
	v.frame()
	v.notes.drain(v.h)
	v.w.watch(v.h, thrMatch)
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
	layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(v.head),
		layout.Flexed(1, v.body),
	)
	return D{Size: size}
}

// head is p-view_header: h 49, padding 0 12px 0 18px, white, 18px/24px
// 900 #1d1c1d title, a 1px bottom hairline inside the 49px.
func (v *Threads) head(gtx C) D {
	w, h := gtx.Constraints.Max.X, gtx.Dp(49)
	r := image.Rect(0, 0, w, h)
	fill(gtx, r, v.p.Bg)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, h))
	layout.Inset{Left: 18, Right: 12}.Layout(g, func(gtx C) D {
		return layout.W.Layout(gtx, func(gtx C) D { return v.para(gtx, 18, 24, wBlack, v.p.Text, 1, "Threads") })
	})
	hl := max(1, gtx.Dp(1))
	fill(gtx, image.Rect(0, h-hl, w, h), v.p.Border)
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

// unread is the number of unread replies, after local read marks.
func (v *Threads) unread(t *thrItem) int {
	if t.UnreadCount <= 0 {
		return 0
	}
	if r, ok := v.read[t.key]; ok && !actTSLess(r, t.latest()) {
		return 0
	}
	return t.UnreadCount
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
		return v.errorState(gtx, err, &v.retry)
	case n == 0:
		return v.empty(gtx)
	}
	failed := v.failedCursor != "" && v.failedCursor == page.next
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	clear(v.sizes)
	d := v.scrollList(gtx, &v.list, n+1, func(gtx C, i int) D {
		if i >= n {
			return v.footer(gtx, failed)
		}
		d := v.thread(gtx, &page.items[i], page.convs)
		v.sizes[i] = d.Size.Y
		return d
	})
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
			c.comp.Origin = org.Add(image.Pt(gtx.Dp(16+4), y+c.compY))
		}
		y += v.sizes[i]
	}
}

// empty follows the shared Saved/To-dos empty state: illustration, 18px
// 900 title, 15px body, centered column w 420. (The spec account had a
// followed thread, so the copy is ours.)
func (v *Threads) empty(gtx C) D {
	return layout.Center.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(420))
		gtx.Constraints.Min.X = 0
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Bottom: 16}.Layout(gtx, func(gtx C) D { return v.icon(gtx, "threads", 64, v.p.Faint) })
			}),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D {
					return v.th.Layout(gtx, theme.TextStyle{Size: 18, LineHeight: 24, Weight: wBlack, Color: v.p.Text, Align: text.Middle}, "No threads yet")
				})
			}),
			layout.Rigid(func(gtx C) D {
				return v.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: v.p.Sec, Align: text.Middle},
					"Threads you’re involved in will be collected right here.")
			}),
		)
	})
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

// thrTime is the message timestamp: relative for the last few hours
// ("1 hour ago"), else "11:17 AM" today, else "Nov 4th at 11:17 AM".
// next is when the text changes (0 = not soon).
func thrTime(t, now time.Time) (s string, next time.Duration) {
	if t.IsZero() {
		return "", 0
	}
	age := now.Sub(t)
	switch {
	case age < 0:
		return clock(t), 0
	case age < time.Minute:
		return "just now", time.Minute - age
	case age < time.Hour:
		m := int(age / time.Minute)
		left := time.Minute - age%time.Minute
		if m == 1 {
			return "1 minute ago", left
		}
		return strconv.Itoa(m) + " minutes ago", left
	case age < 3*time.Hour:
		h := int(age / time.Hour)
		left := time.Hour - age%time.Hour
		if h == 1 {
			return "1 hour ago", left
		}
		return strconv.Itoa(h) + " hours ago", left
	case sameDay(t, now):
		return "Today at " + clock(t), 0 // Slack's Threads view (measured)
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "Yesterday at " + clock(t), 0
	}
	return actStamp(t), 0
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
		c.moreText = ""
		if hidden := t.Root.ReplyCount - len(t.Replies); hidden > 0 {
			w := "replies"
			if hidden == 1 {
				w = "reply"
			}
			c.moreText = "Show " + strconv.Itoa(hidden) + " more " + w
		}
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
		c.built = true
	}
	return c
}

// thread is one block: the header line on the gray background, then the
// card.
func (v *Threads) thread(gtx C, t *thrItem, convs map[string]core.ConvRef) D {
	c := v.cardFor(t, convs)
	if c.title.Clicked(gtx) {
		v.h.Navigate("c/" + t.Channel)
	}
	if c.more.Clicked(gtx) {
		gtx.Execute(op.InvalidateCmd{})
		v.markRead(t)
		v.h.OpenThread(t.Channel, t.Root.TS)
	}
	if c.reply.Clicked(gtx) {
		gtx.Execute(op.InvalidateCmd{})
		v.markRead(t)
		if ch, ok := v.h.(composer.Host); ok && c.comp == nil {
			c.comp = composer.New(ch, t.Channel, t.Root.TS)
			c.comp.Focus()
		} else if !ok {
			v.h.OpenThread(t.Channel, t.Root.TS)
		}
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
	d := layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Card header: margin 16px 28px 12px 24px, h 40.
		layout.Rigid(func(gtx C) D {
			d := layout.Inset{Top: 16, Right: 28, Bottom: 12, Left: 24}.Layout(gtx, func(gtx C) D {
				return col(gtx,
					layout.Rigid(func(gtx C) D {
						gtx.Constraints.Min.X = 0
						return c.title.Layout(gtx, func(gtx C) D {
							pointerCursor(gtx)
							d := v.para(gtx, 15, 22, nameW, v.p.Text, 1, c.name)
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
			return d
		}),
		// Card: margin 0 16px 8px, radius 12, 1px #dddddd.
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 16, Right: 16, Bottom: 8}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				m := op.Record(gtx.Ops)
				d, compY := v.card(gtx, c, t)
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
	return d
}

// card lays out the card's segments and returns its size and the top of
// the reply box within it.
func (v *Threads) card(gtx C, c *thrCard, t *thrItem) (D, int) {
	unread := v.unread(t)
	unreadFrom := len(t.Replies) - unread
	compH, compBottom := 0, 20
	if c.comp != nil {
		compBottom = 0 // the real composer ends with its 24px hint bar
	}
	d := layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			s := c.msg(t.Root.TS)
			s.v, s.m, s.top = v, &t.Root, 20
			return slackc.Component(gtx, "ThreadRootGeneric", s, (*thrMsg).rootProps, (*thrMsg).layoutRoot)
		}),
		layout.Rigid(func(gtx C) D {
			if c.moreText == "" {
				return D{}
			}
			return layout.Inset{Top: 4, Bottom: 4, Left: thrMsgIndent, Right: thrMsgPad}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = 0
				return c.more.Layout(gtx, func(gtx C) D {
					pointerCursor(gtx)
					d := v.para(gtx, 15, 22, wBold, v.p.Link, 1, c.moreText)
					if c.more.Hovered() {
						fill(gtx, image.Rect(0, d.Size.Y-gtx.Dp(3), d.Size.X, d.Size.Y-gtx.Dp(2)), v.p.Link)
					}
					return d
				})
			})
		}),
		layout.Rigid(func(gtx C) D {
			return actStack(gtx, len(t.Replies), func(gtx C, i int) D {
				m := &t.Replies[i]
				d := v.msg(gtx, c.msg(m.TS), m, 8)
				if unread > 0 && i == max(unreadFrom, 0) {
					v.newLine(gtx, d.Size.X)
				}
				return d
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
				return d
			}
			d := layout.Inset{Top: 8, Bottom: 20, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
				return v.replyPicture(gtx, c)
			})
			compH = d.Size.Y - gtx.Dp(8+20)
			return d
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
						return layout.Inset{Top: 4}.Layout(gtx, func(gtx C) D { return v.actReactions(gtx, m.Reactions) })
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
	st.Pop()
	return d
}

// replyPicture draws Slack's unfocused inline thread composer (h 144,
// radius 8, 1px #1d1c1d@.13): formatting toolbar with icons at 30% alpha,
// "Reply to thread…", the "Also send to" checkbox row and the footer with
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

		// Toolbar (h 38): 18px icons, ~14px apart, separators between groups.
		v.thrIcons(gtx, image.Pt(gtx.Dp(8), 0), gtx.Dp(38), thrToolbar, faint)

		// Editor: padding 8px 12px, 15px/22px placeholder.
		func() {
			defer op.Offset(image.Pt(gtx.Dp(12), gtx.Dp(38+8))).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Constraints{Max: image.Pt(max(0, w-gtx.Dp(24)), gtx.Dp(22))}
			v.para(g, 15, 22, wReg, soft, 1, "Reply to thread…")
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
