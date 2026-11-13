package views

// The channel's Pins tab (Slack's "bookmarks" tab type, the pin icon) and
// its peek, from the web client (build 859cadc0e0ab, read from the public
// CDN without a cookie; docs/CHANNELS-AUDIT.md "Pins tab"):
//
//   - useTabView's Xy.Bookmarks view is oG (use-tab-view chunk 5Itp):
//     .pinsTabView__AOEfN (foreground_min_solid, padding-top 20; base-pry
//     when stacked on the tabs list) around usePinsV2's rows (core.PinsTab,
//     includeHeader false):
//   - loading: .skeletonListContainer__WliHD (968 wide at most, 100% −
//     32px, a column 8 apart) of five PinsSkeleton (jlmI: a compact Entity
//     in its loading state, withBorder).
//   - empty: .emptyContainer__IoXdH (a centred column 4 apart, 15vh above
//     and below): "Easily access all of your most important messages"
//     (.emptyTitle__iUqbz, 18px Black) over "Right click on a message to
//     pin it to the channel."
//   - else "Pinned messages" (PinsListHeading, .heading__QZP5y: 13px Bold
//     in foreground_max .7, at most 968 wide, padding 0 0 6 4, at least 24
//     tall) over the list (.listContainer__IZ6w2, 968 at most, 32 under):
//     a message is PinsListMessage (GWqQ: .container__xBoXD, pointer) in
//     .message__ExB0G.withMargin__geAvs (primary background, 1px
//     primary_foreground .1, radius 8, 4px above and below) holding
//     BasePinnedMessage (keLK): the message kit pillow (padding 12, no
//     border), the 36px avatar in the gutter (8 to its right), the sender,
//     two spaces and the full timestamp (getFullTimestamp without
//     seconds), the body, files, attachments and reactions, and
//     MessageActions (container channel-tab-pins-peek, no quick
//     reactions) at .p-pinned_message__actions (top 32, right −6 around
//     the toolbar's own −16 / 17). A file is PinsListFile (JIpU: a compact
//     Entity, the file's icon thumbnail, its name and "Shared by {name}
//     on {date}") in .entityContainer__UP6hi.
//   - onItemClick: a message runs openPinnedItem (Sezs: the message's
//     permalink, so the channel or the thread at the message); a file
//     opens (5Ya6). The list is the keyboard list (↑/↓, Home/End, Enter).
//   - Right-click: PinsListMessageContextMenu (nihB): Copy link while the
//     message has a permalink (5FS0 copyLink: the clipboard, no toast),
//     Remove pin (danger) when useCanEditTabs; nothing when neither.
//   - The peek (PinsTabPeekV2, boot-shared q5Cv) is the same rows in
//     preview: no heading, each row .listItem__DP0n0 (primary background,
//     a 1px primary_foreground .1 bottom line but on the last, padding-right
//     4, the pillow borderless), its click the row's own; contentHeight
//     315 loading (five skeletons, no border), 220 empty
//     (.emptyContainer__HG5fM: centred, 8 apart, 32 in; the title
//     .emptyHeading__p458z 18px Black), else the rows' total height.
//
// Not ported: privacy blur (privacyBlurredRow, the viewer's blur pref) and
// the toolbar's Add to list… button.

import (
	"context"
	"image"
	"io"
	"strings"
	"time"

	"gioui.org/gesture"
	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/ctxclick"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// PinOpener is optionally implemented by the Host: openPinnedItem's
// navigation to a pinned message (its thread for a reply, else the
// channel's Messages at the message).
type PinOpener interface {
	OpenPinnedMessage(ch, ts, threadTS string)
}

// cpRow is one pin row's widgets.
type cpRow struct {
	row     widget.Clickable
	ctx     int // the right-click tag
	hv      gesture.Hover
	tb      [cpTbCount]widget.Clickable
	tbTip   [cpTbCount]popups.TipState
	tbRects [cpTbCount]image.Rectangle
}

// The pinned message's MessageActions buttons (no quick reactions).
const (
	cpTbReact = iota
	cpTbReply
	cpTbForward
	cpTbTodo
	cpTbMore
	cpTbCount
)

// ChannelPins is a channel's Pins tab (or, with preview, its peek).
type ChannelPins struct {
	base
	fv      *Files        // file tiles
	cf      *ChannelFiles // the file rows' "Shared by" line
	ch      string
	preview bool

	list widget.List
	kb   *vkList
	rows map[string]*cpRow

	items  []core.PinsTabItem
	loaded bool
	// peekH is the rows' total height (PinsTabPeekV2's
	// onTotalHeightChange) from the last frame.
	peekH int
}

// NewChannelPins builds ch's Pins tab.
func NewChannelPins(h Host, ch string) *ChannelPins {
	v := &ChannelPins{base: newBase(h), fv: NewFiles(h), cf: NewChannelFiles(h, ch), ch: ch, rows: map[string]*cpRow{}}
	v.list.Axis = layout.Vertical
	v.kb = newVKList(&v.list)
	return v
}

// NewChannelPinsPeek is the tab's PinsTabPeekV2.
func NewChannelPinsPeek(h Host, ch string) *ChannelPins {
	v := NewChannelPins(h, ch)
	v.preview = true
	return v
}

// Channel is the tab's channel.
func (v *ChannelPins) Channel() string { return v.ch }

// Items are the rows as last drawn (tests).
func (v *ChannelPins) Items() []core.PinsTabItem { return v.items }

// Loaded reports whether the pins have loaded (usePinsV2's !isLoading).
func (v *ChannelPins) Loaded() bool { return v.loaded }

func (v *ChannelPins) rowOf(key string) *cpRow {
	r := v.rows[key]
	if r == nil {
		r = new(cpRow)
		v.rows[key] = r
	}
	return r
}

func (v *ChannelPins) refresh() {
	c := v.h.Core()
	if c == nil {
		return
	}
	v.items, v.loaded = c.PinsTab(v.ch)
}

// PeekHeight is PinsTabPeekV2's contentHeight: 315 loading, 220 empty,
// else the rows' total height (0 until they've been measured once).
func (v *ChannelPins) PeekHeight(gtx layout.Context) int {
	v.refresh()
	switch {
	case !v.loaded:
		return gtx.Dp(315)
	case len(v.items) == 0:
		return gtx.Dp(220)
	}
	if v.peekH == 0 {
		return gtx.Dp(315)
	}
	return v.peekH
}

// ---- actions ----

// open is onItemClick: openPinnedItem for a message, the file opener for
// a file.
func (v *ChannelPins) open(it core.PinsTabItem) {
	switch it.Type {
	case "message":
		thread := ""
		if it.Msg != nil {
			thread = it.Msg.ThreadTS
		}
		if po, ok := v.h.(PinOpener); ok {
			po.OpenPinnedMessage(it.Channel, it.TS, thread)
			return
		}
		if thread != "" && thread != it.TS {
			v.h.OpenThread(it.Channel, thread)
			return
		}
		v.h.Navigate("c/" + it.Channel)
	case "file":
		if it.File != nil {
			v.cf.open(fileItem{File: *it.File})
		}
	}
}

// menuItems is PinsListMessageContextMenu's: Copy link while the message
// has a permalink (a message row), Remove pin when the viewer may edit
// the channel's tabs.
func (v *ChannelPins) menuItems(it core.PinsTabItem) []popups.MenuItem {
	var items []popups.MenuItem
	c := v.h.Core()
	ph, _ := v.h.(popups.Host)
	if it.Type == "message" && it.Msg != nil && ph != nil {
		link := popups.Permalink(ph, it.Channel, it.TS, it.Msg.ThreadTS)
		items = append(items, popups.MenuItem{Key: "copy", Label: nsTabbed.T("Copy link"), DoCtx: func(gtx layout.Context) {
			gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(link))})
		}})
	}
	if c != nil && c.CanEditTabs(v.ch) {
		items = append(items, popups.MenuItem{Key: "unpin", Label: nsTabbed.T("Remove pin"), Danger: true, Do: func() {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				_ = c.UnpinFromTab(ctx, it)
				v.h.Invalidate()
			}()
		}})
	}
	return items
}

// RowButton is a row's clickable (tests: hover and click it).
func (v *ChannelPins) RowButton(key string) *widget.Clickable { return &v.rowOf(key).row }

// MenuLabels are a row's context menu labels.
func (v *ChannelPins) MenuLabels(it core.PinsTabItem) []string { return menuLabelsOf(v.menuItems(it)) }

// OpenMenu opens a row's context menu at p (tests: the right-click).
func (v *ChannelPins) OpenMenu(it core.PinsTabItem, p image.Point) { v.openMenu(it, p) }

// openMenu opens the context menu at the pointer (nothing without items).
func (v *ChannelPins) openMenu(it core.PinsTabItem, at image.Point) {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	items := v.menuItems(it)
	if len(items) == 0 {
		return
	}
	ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: image.Rectangle{Min: at, Max: at.Add(image.Pt(1, 1))}, Width: 240}))
}

func (v *ChannelPins) pointer() image.Point {
	if pp, ok := v.h.(interface{ PointerPos() image.Point }); ok {
		return pp.PointerPos()
	}
	return image.Point{}
}

func (v *ChannelPins) update(gtx C) {
	for _, it := range v.items {
		r := v.rows[it.Key()]
		if r == nil {
			continue
		}
		took := false
		for i := range r.tb {
			if r.tb[i].Clicked(gtx) {
				took = true
				v.toolbar(it, i, r.tbRects[i])
			}
		}
		if r.row.Clicked(gtx) && !took {
			v.open(it)
		}
		for {
			ev, ok := gtx.Event(pointer.Filter{Target: &r.ctx, Kinds: pointer.Press})
			if !ok {
				break
			}
			if e, ok := ev.(pointer.Event); ok && ctxclick.Press(e) && !v.preview {
				v.openMenu(it, v.pointer())
			}
		}
	}
}

// toolbar runs a MessageActions button.
func (v *ChannelPins) toolbar(it core.PinsTabItem, b int, btn image.Rectangle) {
	ph, ok := v.h.(popups.Host)
	if !ok || it.Msg == nil {
		return
	}
	m := *it.Msg
	at := v.pointer()
	switch b {
	case cpTbReact:
		ph.ShowOverlay(popups.NewReactionPicker(ph, at, m.Channel, m.TS, m.ThreadTS, nil))
	case cpTbReply:
		root := m.ThreadTS
		if root == "" {
			root = m.TS
		}
		v.h.OpenThread(m.Channel, root)
	case cpTbForward:
		ph.ShowOverlay(popups.NewForwardModal(ph, m))
	case cpTbTodo:
		popups.RunTodoAction(ph, m)
	case cpTbMore:
		v.h.OpenMessageMenu(at, m)
	}
}

func (v *ChannelPins) kbClick(key string) {
	for _, it := range v.items {
		if it.Key() == key {
			v.open(it)
			return
		}
	}
}

// ---- layout ----

// Layout draws the tab (or the peek).
func (v *ChannelPins) Layout(gtx C) D {
	v.frame()
	v.fv.frame()
	v.cf.frame()
	v.refresh()
	v.update(gtx)
	sz := gtx.Constraints.Max
	if v.preview {
		fill(gtx, image.Rectangle{Max: sz}, v.p.Surface)
		return v.peek(gtx)
	}
	fill(gtx, image.Rectangle{Max: sz}, v.p.Page) // .pinsTabView__AOEfN: foreground_min_solid
	gtx.Constraints = layout.Exact(sz)
	return layout.Inset{Top: 20}.Layout(gtx, func(gtx C) D {
		switch {
		case !v.loaded:
			return v.contained(gtx, func(gtx C) D { return v.skeletons(gtx, true) })
		case len(v.items) == 0:
			return v.empty(gtx, 4, false)
		}
		return col(gtx,
			layout.Rigid(func(gtx C) D { return v.contained(gtx, v.heading) }),
			layout.Flexed(1, v.results),
		)
	})
}

// contained is the 968px column (100% − 32px), centred.
func (v *ChannelPins) contained(gtx C, w W) D {
	full := gtx.Constraints.Max.X
	cw := min(gtx.Dp(968), max(0, full-gtx.Dp(32)))
	g := gtx
	g.Constraints.Min.X, g.Constraints.Max.X = cw, cw
	m := op.Record(gtx.Ops)
	d := w(g)
	call := m.Stop()
	func() {
		defer op.Offset(image.Pt((full-cw)/2, 0)).Push(gtx.Ops).Pop()
		call.Add(gtx.Ops)
	}()
	return D{Size: image.Pt(full, d.Size.Y)}
}

// heading is PinsListHeading: "Pinned messages".
func (v *ChannelPins) heading(gtx C) D {
	w := gtx.Constraints.Max.X
	d := dirAt(gtx, image.Pt(gtx.Dp(4), 0), func(gtx C) D {
		gtx.Constraints.Max.X = max(0, w-gtx.Dp(4))
		return v.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Weight: wBold, Color: v.p.Muted, MaxLines: 1},
			nsTabbed.T("Pinned messages"))
	})
	h := max(gtx.Dp(24), d.Size.Y+gtx.Dp(6))
	return D{Size: image.Pt(w, h)}
}

// empty is the empty state: the tab's (gap 4, 15vh down) or the peek's
// (gap 8, 32 in, centred in its 220).
func (v *ChannelPins) empty(gtx C, gap unit.Dp, peek bool) D {
	sz := gtx.Constraints.Max
	title := nsTabbed.T("Easily access all of your most important messages")
	desc := nsTabbed.T("Right click on a message to pin it to the channel.")
	pad := 0
	if peek {
		pad = gtx.Dp(32)
	}
	tw := max(0, sz.X-2*pad)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(tw, sz.Y)}
	m1 := op.Record(gtx.Ops)
	t1 := v.th.Layout(g, theme.TextStyle{Size: 18, LineHeight: 24, Weight: wBlack, Color: v.p.Text, Align: text.Middle}, title)
	c1 := m1.Stop()
	m2 := op.Record(gtx.Ops)
	t2 := v.th.Layout(g, theme.TextStyle{Size: 15, LineHeight: 22, Weight: wReg, Color: v.p.Text, Align: text.Middle}, desc)
	c2 := m2.Stop()
	h := t1.Size.Y + gtx.Dp(gap) + t2.Size.Y
	y := int(float32(gtx.Constraints.Max.Y) * 0.15)
	if peek {
		y = max(0, (sz.Y-h)/2)
	} else if wh, ok := v.h.(interface{ WindowHeight() int }); ok {
		y = int(float32(wh.WindowHeight()) * 0.15) // margin: 15vh
	}
	func() {
		defer op.Offset(image.Pt(pad+(tw-t1.Size.X)/2, y)).Push(gtx.Ops).Pop()
		c1.Add(gtx.Ops)
	}()
	func() {
		defer op.Offset(image.Pt(pad+(tw-t2.Size.X)/2, y+t1.Size.Y+gtx.Dp(gap))).Push(gtx.Ops).Pop()
		c2.Add(gtx.Ops)
	}()
	return D{Size: sz}
}

// skeletons are five PinsSkeleton: a compact Entity loading (padding 12,
// a 36px icon placeholder, 12 apart, two text bars), with its border in
// the tab (8 apart) or as peek rows (63px, a bottom line).
func (v *ChannelPins) skeletons(gtx C, bordered bool) D {
	w := gtx.Constraints.Max.X
	y := 0
	bw := max(1, gtx.Dp(1))
	for i := 0; i < 5; i++ {
		h := gtx.Dp(60) + 2*bw
		if !bordered {
			h = gtx.Dp(63)
		}
		r := image.Rect(0, y, w, y+h)
		if bordered {
			rrect(gtx, r, gtx.Dp(12), theme.Over(v.p.CardLine, v.p.Surface))
			rrect(gtx, r.Inset(bw), max(0, gtx.Dp(12)-bw), v.p.Surface)
		} else {
			fill(gtx, r, v.p.Surface)
			if i < 4 {
				fill(gtx, image.Rect(0, r.Max.Y-bw, w, r.Max.Y), theme.Over(v.p.CardLine, v.p.Surface))
			}
		}
		o := image.Pt(gtx.Dp(12)+bw, y+(h-gtx.Dp(36))/2)
		rrect(gtx, image.Rectangle{Min: o, Max: o.Add(image.Pt(gtx.Dp(36), gtx.Dp(36)))}, gtx.Dp(8), v.p.Skel)
		tx := o.X + gtx.Dp(48)
		tw := max(0, w-tx-gtx.Dp(24))
		rrect(gtx, image.Rect(tx, o.Y+gtx.Dp(4), tx+tw*55/100, o.Y+gtx.Dp(16)), gtx.Dp(4), v.p.Skel)
		rrect(gtx, image.Rect(tx, o.Y+gtx.Dp(22), tx+tw*35/100, o.Y+gtx.Dp(32)), gtx.Dp(4), v.p.Skel)
		y += h
		if bordered {
			y += gtx.Dp(8)
		}
	}
	return D{Size: image.Pt(w, y)}
}

func (v *ChannelPins) kbItems() []vkItem {
	items := make([]vkItem, 0, len(v.items))
	for i, it := range v.items {
		items = append(items, vkItem{key: it.Key(), elem: i, focusable: true})
	}
	return items
}

// results is the list (the tab's).
func (v *ChannelPins) results(gtx C) D {
	v.kb.setItems(v.kbItems())
	v.kb.update(gtx, gtx.Constraints.Max.Y, vkOpts{Click: v.kbClick})
	v.kb.begin()
	n := len(v.items)
	d := v.scrollList(gtx, &v.list, n, func(gtx C, i int) D {
		it := v.items[i]
		return v.kb.elem(i, v.contained(gtx, func(gtx C) D {
			d := layout.Inset{Top: 4, Bottom: 4}.Layout(gtx, func(gtx C) D { return v.item(gtx, it, i == n-1) })
			if i == n-1 {
				d.Size.Y += gtx.Dp(32) // .listContainer: 32 under
			}
			return d
		}))
	})
	v.kb.area(gtx, image.Rectangle{Max: d.Size})
	v.kb.after()
	return d
}

// peek is PinsTabPeekV2's body.
func (v *ChannelPins) peek(gtx C) D {
	sz := gtx.Constraints.Max
	switch {
	case !v.loaded:
		return v.skeletons(gtx, false)
	case len(v.items) == 0:
		return v.empty(gtx, 8, true)
	}
	y := 0
	for i, it := range v.items {
		d := dirAt(gtx, image.Pt(0, y), func(gtx C) D {
			gtx.Constraints = layout.Constraints{Max: image.Pt(sz.X, 1<<20)}
			gtx.Constraints.Min.X = sz.X
			return v.item(gtx, it, i == len(v.items)-1)
		})
		y += d.Size.Y
	}
	if y != v.peekH { // onTotalHeightChange: the card takes the new height next frame
		v.peekH = y
		gtx.Execute(op.InvalidateCmd{})
	}
	return D{Size: image.Pt(sz.X, min(y, sz.Y))}
}

// item draws a row: a message card or a file entity.
func (v *ChannelPins) item(gtx C, it core.PinsTabItem, last bool) D {
	if it.Type == "file" {
		if it.File == nil {
			return D{}
		}
		return v.fileItem(gtx, it, last)
	}
	if it.Msg == nil {
		return D{} // PinsListMessage: null without its message
	}
	return v.message(gtx, it, last)
}

// frame draws a row's box: the tab's card (radius r, 1px line) or the
// peek's flat row with its bottom line.
func (v *ChannelPins) box(gtx C, sz image.Point, radius int, last bool) {
	bw := max(1, gtx.Dp(1))
	line := theme.Over(v.p.CardLine, v.p.Surface)
	if v.preview {
		fill(gtx, image.Rectangle{Max: sz}, v.p.Surface)
		if !last {
			fill(gtx, image.Rect(0, sz.Y-bw, sz.X, sz.Y), line)
		}
		return
	}
	rrect(gtx, image.Rectangle{Max: sz}, radius, line)
	rrect(gtx, image.Rectangle{Max: sz}.Inset(bw), max(0, radius-bw), v.p.Surface)
}

// message is BasePinnedMessage in its row.
func (v *ChannelPins) message(gtx C, it core.PinsTabItem, last bool) D {
	r := v.rowOf(it.Key())
	m := v.viewMsg(it.Msg)
	hovered := r.hv.Update(gtx.Source)
	ring := v.kb.ringOn(it.Key())
	w := gtx.Constraints.Max.X
	bw := 0
	if !v.preview {
		bw = max(1, gtx.Dp(1))
	}
	pad := gtx.Dp(12)
	right := pad
	if v.preview {
		right += gtx.Dp(4) // .listItem__DP0n0: padding-right 4
	}
	inner := max(0, w-2*bw-pad-right)
	name, av := v.authorOf(m)
	stamp := richview.FullTimestamp(m.TS, gtx.Now)
	rec := op.Record(gtx.Ops)
	cd := dirAt(gtx, image.Pt(bw+pad, bw+pad), func(gtx C) D {
		gtx.Constraints = layout.Constraints{Max: image.Pt(inner, 1<<20)}
		gtx.Constraints.Min.X = inner
		return row(gtx, layout.Start,
			layout.Rigid(func(gtx C) D { return v.avatar(gtx, av, 36) }),
			hgap(8),
			layout.Flexed(1, func(gtx C) D {
				return col(gtx,
					layout.Rigid(func(gtx C) D {
						return row(gtx, layout.Baseline,
							layout.Rigid(func(gtx C) D { return v.para(gtx, 15, 22, wBlack, v.p.Text, 1, name) }),
							hgap(8), // "\xa0\xa0"
							layout.Rigid(func(gtx C) D { return v.para(gtx, 12, 17.6, wReg, v.p.Meta, 1, stamp) }),
						)
					}),
					layout.Rigid(func(gtx C) D { return v.actPaneBody(gtx, m) }),
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
	h := cd.Size.Y + 2*pad + 2*bw
	sz := image.Pt(w, h)
	return r.row.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		v.box(gtx, sz, gtx.Dp(8), last)
		call.Add(gtx.Ops)
		if (hovered || r.row.Hovered()) && !v.preview {
			v.actions(gtx, r, it, sz)
		}
		if ring {
			v.vkRing(gtx, image.Rectangle{Max: sz}, gtx.Dp(8))
		}
		st := clip.Rect{Max: sz}.Push(gtx.Ops)
		r.hv.Add(gtx.Ops)
		pass := pointer.PassOp{}.Push(gtx.Ops)
		event.Op(gtx.Ops, &r.ctx)
		pass.Pop()
		st.Pop()
		return D{Size: sz}
	})
}

// actions is the pinned message's MessageActions: the hover group (4px
// padded, radius 12, the .13 ring and shadow) 16 down and 11 in from the
// card's top right: Add reaction, Reply in thread, Forward, the to-do
// action, More actions.
func (v *ChannelPins) actions(gtx C, r *cpRow, it core.PinsTabItem, sz image.Point) {
	bs, pad := gtx.Dp(32), gtx.Dp(4)
	gw, gh := 2*pad+cpTbCount*bs, 2*pad+bs
	org := image.Pt(sz.X-gtx.Dp(11)-gw, gtx.Dp(16))
	box := image.Rectangle{Min: org, Max: org.Add(image.Pt(gw, gh))}
	theme.Shadow(gtx, box, gtx.Dp(12), gtx.Dp(3), argb(0x14000000))
	rrect(gtx, box, gtx.Dp(12), v.p.Surface)
	border(gtx, box, gtx.Dp(12), max(1, gtx.Dp(1)), v.p.Ring)
	th := v.h.Theme()
	todo := popups.TodoActionFor(v.h.Core(), it.Msg)
	icons := [cpTbCount]string{"add-reaction", "comment-lines", "forward-msg", todo.HoverIcon, "kebab"}
	tips := [cpTbCount]string{nsMessage.T("Add reaction…"), nsMessage.T("Reply in thread"), nsMessage.T("Forward message…"),
		todo.Label, nsMessage.T("More actions")}
	if sa, ok := popups.ShareAction(v.h.Core(), it.Msg, false); ok {
		tips[cpTbForward] = sa.Label
	}
	for i := 0; i < cpTbCount; i++ {
		p := image.Pt(org.X+pad+i*bs, org.Y+pad)
		r.tbRects[i] = image.Rectangle{Min: p, Max: p.Add(image.Pt(bs, bs))}
		dirAt(gtx, p, func(gtx C) D {
			d := v.cf.actionButton(gtx, &r.tb[i], icons[i])
			popups.Tooltip(gtx, th, &r.tbTip[i], r.tb[i].Hovered(), d.Size, popups.Above, tips[i], "")
			return d
		})
	}
}

// fileItem is PinsListFile: a compact Entity with the file's icon
// thumbnail (36), its name (15 Bold) and "Shared by {name} on {date}".
func (v *ChannelPins) fileItem(gtx C, it core.PinsTabItem, last bool) D {
	r := v.rowOf(it.Key())
	fi := fileItem{File: *it.File}
	ring := v.kb.ringOn(it.Key())
	return r.row.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		bw := 0
		if !v.preview {
			bw = max(1, gtx.Dp(1))
		}
		w := gtx.Constraints.Max.X
		h := 2*gtx.Dp(12) + gtx.Dp(36) + 2*bw
		sz := image.Pt(w, h)
		v.box(gtx, sz, gtx.Dp(12), last)
		dirAt(gtx, image.Pt(bw+gtx.Dp(12), bw+gtx.Dp(12)), func(gtx C) D { return v.fv.fileTile(gtx, &fi, 36) })
		tx := bw + gtx.Dp(12) + gtx.Dp(36) + gtx.Dp(12)
		tw := max(0, w-tx-gtx.Dp(12)-bw)
		dirAt(gtx, image.Pt(tx, bw+gtx.Dp(10)), func(gtx C) D {
			gtx.Constraints.Max.X = tw
			return col(gtx,
				layout.Rigid(func(gtx C) D {
					return v.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: wBold, Color: v.p.Text, MaxLines: 1}, fi.title())
				}),
				layout.Rigid(func(gtx C) D {
					return v.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 16, Weight: wReg, Color: v.p.Muted, MaxLines: 1}, v.cf.fileDetail(&fi))
				}),
			)
		})
		if ring {
			v.vkRing(gtx, image.Rectangle{Max: sz}, gtx.Dp(12))
		}
		st := clip.Rect{Max: sz}.Push(gtx.Ops)
		pass := pointer.PassOp{}.Push(gtx.Ops)
		event.Op(gtx.Ops, &r.ctx)
		pass.Pop()
		st.Pop()
		return D{Size: sz}
	})
}
