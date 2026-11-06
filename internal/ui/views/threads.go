package views

// Threads: Slack's Threads view (Home sidebar "Threads"). A 49px header,
// then on a #f8f8f8 background one block per followed thread (unread
// first): a header line outside the card (channel link + participants)
// and a white card (radius 12, 1px #dddddd) holding the root message,
// "Show N more replies", the latest replies and an inline composer.
// The composer is drawn as Slack's (toolbar, "Reply to thread…", "Also
// send to", footer); clicking it opens the thread pane. Measurements are
// from /tmp/ui-views-spec/threads.md.

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
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

const (
	thrLimit             = 10
	thrMsgPad    unit.Dp = 20          // message row side padding (avatar at card x+21)
	thrMsgIndent unit.Dp = 20 + 36 + 8 // padding + avatar + gap
	thrComposerH unit.Dp = 144
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
	hovers                 map[string]*gesture.Hover // per message ts
	name, people, moreText string
	also                   string // "Also send to #general"
	built                  bool
}

func (c *thrCard) hover(ts string) *gesture.Hover {
	h := c.hovers[ts]
	if h == nil {
		h = new(gesture.Hover)
		c.hovers[ts] = h
	}
	return h
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
}

// NewThreads builds the Threads view; it loads on first Layout.
func NewThreads(h Host) *Threads {
	v := &Threads{base: newBase(h), cards: map[string]*thrCard{}}
	v.list.Axis = layout.Vertical
	return v
}

func thrMatch(ev core.Event) bool {
	return ev.Type == "message" && ev.Reply || ev.Type == "thread_marked" || ev.Type == "update_thread_state"
}

// Layout draws the view.
func (v *Threads) Layout(gtx C) D {
	v.frame()
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

	size := gtx.Constraints.Max
	fill(gtx, image.Rectangle{Max: size}, v.p.ReadFill)
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
	d := v.scrollList(gtx, &v.list, n+1, func(gtx C, i int) D {
		if i >= n {
			return v.footer(gtx, failed)
		}
		return v.thread(gtx, &page.items[i], page.convs)
	})
	if page.hasMore && page.next != "" && !failed && !v.data.busy() &&
		v.list.Position.First+v.list.Position.Count >= n-2 {
		v.load(actMore)
	}
	return d
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

// people is the participants line: "Just you", "Ada, Grace and you", or
// "Ada, Grace, Linus and 4 others".
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
		return strings.Join(names[:3], ", ") + " and " + strconv.Itoa(len(names)-3) + " others"
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
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
		return clock(t), 0
	}
	return actStamp(t), 0
}

// thrMeta is Slack's #616061 for timestamps and participants.
func (v *Threads) meta() color.NRGBA {
	if v.th.Dark {
		return v.p.Muted
	}
	return rgb(0x616061)
}

// thrEdge is the card border (#dddddd per tokens).
func (v *Threads) edge() color.NRGBA {
	if v.th.Dark {
		return v.p.Border
	}
	return rgb(0xdddddd)
}

func (v *Threads) cardFor(t *thrItem, convs map[string]core.ConvRef) *thrCard {
	c := v.cards[t.key]
	if c == nil {
		c = &thrCard{hovers: map[string]*gesture.Hover{}}
		v.cards[t.key] = c
	}
	if !c.built {
		c.name = v.convTitle(t.Channel, convs)
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
	if c.more.Clicked(gtx) || c.reply.Clicked(gtx) {
		v.h.OpenThread(t.Channel, t.Root.TS)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Card header: margin 16px 28px 12px 24px, h 40.
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 16, Right: 28, Bottom: 12, Left: 24}.Layout(gtx, func(gtx C) D {
				return col(gtx,
					layout.Rigid(func(gtx C) D {
						gtx.Constraints.Min.X = 0
						return c.title.Layout(gtx, func(gtx C) D {
							pointerCursor(gtx)
							d := v.para(gtx, 15, 22, wBold, v.p.Text, 1, c.name)
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
		}),
		// Card: margin 0 16px 8px, radius 12, 1px #dddddd.
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 16, Right: 16, Bottom: 8}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				m := op.Record(gtx.Ops)
				d := v.card(gtx, c, t)
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
}

func (v *Threads) card(gtx C, c *thrCard, t *thrItem) D {
	unreadFrom := len(t.Replies) - t.UnreadCount
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return v.msg(gtx, c.hover(t.Root.TS), &t.Root, 20, false) }),
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
				return v.msg(gtx, c.hover(m.TS), m, 8, i >= unreadFrom)
			})
		}),
		// Last segment: padding-top 8px, then the composer (margin 0 20px).
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 8, Bottom: 20, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
				return v.composer(gtx, c)
			})
		}),
	)
}

// msg is c-message_kit__thread_message: padding top/20px/8px/20px, avatar
// 36 radius 8, 8px gap, 15px/22px 900 name, 12px/17.6px #616061 time 7px
// after it, 15px/22px body. Hover paints #f8f8f8. Unread replies (the
// spec account had none) get the highlighted-message yellow.
func (v *Threads) msg(gtx C, hv *gesture.Hover, m *core.Message, top unit.Dp, unread bool) D {
	hovered := hv.Update(gtx.Source)
	name, av := v.authorOf(m)
	stamp, next := thrTime(tsTime(m.TS), gtx.Now)
	if next > 0 {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(next)})
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
						return row(gtx, layout.Baseline,
							layout.Rigid(func(gtx C) D { return v.para(gtx, 15, 22, wBlack, v.p.Text, 1, name) }),
							hgap(7),
							layout.Rigid(func(gtx C) D { return v.para(gtx, 12, 17.6, wReg, v.meta(), 1, stamp) }),
						)
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
	switch {
	case unread && hovered:
		fill(gtx, r, v.p.Yellow)
		fill(gtx, r, v.p.Hover)
	case unread:
		fill(gtx, r, v.p.Yellow)
	case hovered:
		fill(gtx, r, v.p.ReadFill)
	}
	call.Add(gtx.Ops)
	st := clip.Rect(r).Push(gtx.Ops)
	hv.Add(gtx.Ops)
	st.Pop()
	return d
}

// composer draws Slack's inline thread composer (h 144, radius 8, 1px
// #1d1c1d@.13): formatting toolbar with icons at 30% alpha, "Reply to
// thread…", the "Also send to" checkbox row and the footer with the send
// button disabled. It is a facade: clicking opens the thread pane, where
// the real composer lives.
func (v *Threads) composer(gtx C, c *thrCard) D {
	return c.reply.Layout(gtx, func(gtx C) D {
		pointer.CursorText.Add(gtx.Ops)
		w, h := gtx.Constraints.Max.X, gtx.Dp(thrComposerH)
		r := image.Rect(0, 0, w, h)
		rad := gtx.Dp(8)
		edge, faint, soft := argb(0x211d1c1d), argb(0x4d1d1c1d), argb(0xb31d1c1d)
		if v.th.Dark {
			edge, faint, soft = v.p.Border, theme.WithAlpha(v.p.Text, 0x4d), v.p.Muted
		}
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
