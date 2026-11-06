package views

// Threads: Slack's Threads view. A header, then a scrolling column of
// thread cards (unread first): channel title + participants, the root
// message, "N more replies", the latest replies inline (unread ones
// highlighted) and a "Reply…" box that opens the thread pane.

import (
	"context"
	"image"
	"image/color"
	"sort"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
)

const (
	thrLimit             = 10
	thrPad       unit.Dp = 16 // card inner padding
	thrMaxW      unit.Dp = 1100
	thrMsgIndent unit.Dp = 16 + 36 + 8 // card padding + avatar + gap
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
	name, people, moreText string
	built                  bool
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
	fill(gtx, image.Rectangle{Max: size}, v.p.Bg)
	layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return v.header(gtx, "Threads") }),
		layout.Rigid(func(gtx C) D { return hline(gtx, v.p.Border) }),
		layout.Flexed(1, v.body),
	)
	return D{Size: size}
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
		return v.emptyState(gtx, "threads", "No threads yet",
			"Threads you’re involved in will be collected right here.", nil, "")
	}
	failed := v.failedCursor != "" && v.failedCursor == page.next
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	d := v.scrollList(gtx, &v.list, n+1, func(gtx C, i int) D {
		if i >= n {
			return v.footer(gtx, failed)
		}
		return v.card(gtx, &page.items[i], page.convs, i == 0)
	})
	if page.hasMore && page.next != "" && !failed && !v.data.busy() &&
		v.list.Position.First+v.list.Position.Count >= n-2 {
		v.load(actMore)
	}
	return d
}

func (v *Threads) footer(gtx C, failed bool) D {
	switch {
	case v.paging:
		return v.skeleton(gtx, 2, 36)
	case failed:
		return layout.Inset{Bottom: 16}.Layout(gtx, func(gtx C) D {
			return layout.Center.Layout(gtx, func(gtx C) D {
				return v.button(gtx, &v.retryMore, btnOutline, true, "Load more")
			})
		})
	}
	return D{Size: image.Pt(gtx.Constraints.Min.X, gtx.Dp(8))}
}

// people is the participants line: "Ada, Grace and you", or "Ada, Grace,
// Linus and 4 others".
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

func (v *Threads) card(gtx C, t *thrItem, convs map[string]core.ConvRef, first bool) D {
	c := v.cards[t.key]
	if c == nil {
		c = &thrCard{}
		v.cards[t.key] = c
	}
	if !c.built {
		c.name = v.convTitle(t.Channel, convs)
		c.people = v.people(&t.Root, t.Replies)
		c.moreText = ""
		if hidden := t.Root.ReplyCount - len(t.Replies); hidden > 0 {
			c.moreText = actReplies(hidden, "more")
		}
		c.built = true
	}
	if c.title.Clicked(gtx) {
		v.h.Navigate("c/" + t.Channel)
	}
	if c.more.Clicked(gtx) || c.reply.Clicked(gtx) {
		v.h.OpenThread(t.Channel, t.Root.TS)
	}
	top := unit.Dp(0)
	if first {
		top = 16
	}
	return layout.Inset{Top: top, Bottom: 16, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
		// Center a capped column on very wide windows.
		w := min(gtx.Constraints.Max.X, gtx.Dp(thrMaxW))
		off := (gtx.Constraints.Max.X - w) / 2
		defer op.Offset(image.Pt(off, 0)).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints.Min.X, g.Constraints.Max.X = w, w
		m := op.Record(gtx.Ops)
		d := v.cardBody(g, c, t)
		call := m.Stop()
		r := image.Rectangle{Max: d.Size}
		rad := gtx.Dp(8)
		rrect(gtx, r, rad, v.p.Surface)
		func() {
			defer clip.UniformRRect(r, rad).Push(gtx.Ops).Pop()
			call.Add(gtx.Ops)
		}()
		border(gtx, r, rad, max(1, gtx.Dp(1)), v.p.Border)
		return D{Size: image.Pt(gtx.Constraints.Max.X, d.Size.Y)}
	})
}

func (v *Threads) cardBody(gtx C, c *thrCard, t *thrItem) D {
	unreadFrom := len(t.Replies) - t.UnreadCount
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Header: channel title (15px 900, link to the channel) and the
		// participants (13px muted).
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 12, Bottom: 4, Left: thrPad, Right: thrPad}.Layout(gtx, func(gtx C) D {
				return col(gtx,
					layout.Rigid(func(gtx C) D {
						gtx.Constraints.Min.X = 0
						return c.title.Layout(gtx, func(gtx C) D {
							pointerCursor(gtx)
							d := v.label(gtx, 15, wBlack, v.p.Text, c.name)
							if c.title.Hovered() {
								fill(gtx, image.Rect(0, d.Size.Y-gtx.Dp(2), d.Size.X, d.Size.Y-gtx.Dp(1)), v.p.Text)
							}
							return d
						})
					}),
					layout.Rigid(func(gtx C) D {
						if c.people == "" {
							return D{}
						}
						return layout.Inset{Top: 2}.Layout(gtx, func(gtx C) D {
							return v.label(gtx, 13, wReg, v.p.Muted, c.people)
						})
					}),
				)
			})
		}),
		layout.Rigid(func(gtx C) D { return v.actMsg(gtx, &t.Root, color.NRGBA{}, thrPad) }),
		layout.Rigid(func(gtx C) D {
			if c.moreText == "" {
				return D{}
			}
			return layout.Inset{Left: thrMsgIndent, Right: thrPad, Bottom: 4}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = 0
				return c.more.Layout(gtx, func(gtx C) D {
					pointerCursor(gtx)
					d := v.label(gtx, 13, wBold, v.p.Link, c.moreText)
					if c.more.Hovered() {
						fill(gtx, image.Rect(0, d.Size.Y-gtx.Dp(2), d.Size.X, d.Size.Y-gtx.Dp(1)), v.p.Link)
					}
					return d
				})
			})
		}),
		layout.Rigid(func(gtx C) D {
			return actStack(gtx, len(t.Replies), func(gtx C, i int) D {
				var bg color.NRGBA
				if i >= unreadFrom {
					bg = v.p.Yellow // unread replies: Slack's highlighted-message fill
				}
				return v.actMsg(gtx, &t.Replies[i], bg, thrPad)
			})
		}),
		// "Reply…": a one-line input look that opens the thread pane.
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 4, Bottom: 12, Left: thrPad, Right: thrPad}.Layout(gtx, func(gtx C) D {
				return c.reply.Layout(gtx, func(gtx C) D {
					pointerCursor(gtx)
					w, h := gtx.Constraints.Max.X, gtx.Dp(38)
					r := image.Rect(0, 0, w, h)
					rad := gtx.Dp(8)
					rrect(gtx, r, rad, v.p.Bg)
					bc := v.p.BorderStr
					if c.reply.Hovered() {
						bc = v.p.Faint
					}
					border(gtx, r, rad, max(1, gtx.Dp(1)), bc)
					g := gtx
					g.Constraints = layout.Exact(image.Pt(w, h))
					layout.Inset{Left: 12, Right: 12}.Layout(g, func(gtx C) D {
						return layout.W.Layout(gtx, func(gtx C) D {
							return v.label(gtx, 15, wReg, v.p.Faint, "Reply…")
						})
					})
					return D{Size: r.Max}
				})
			})
		}),
	)
}
