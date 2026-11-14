package views

// A canvas's comment stream: Slack's QuipCommentStreamView (the lazy chunk
// quip-popout_quip-comment-stream-view 0893e737, read from the public CDN
// without a cookie; CSS from client-boot-styles cf726c4f), opened from the
// canvas's More actions "Show all threads" into the secondary pane
// (toQuipCommentStream). docs/CANVASES.md "Comments".
//
//   - QuipPopoutFlexpane: .p-quip_popout__flexpane_header (50px, padding
//     4px 12px, 18px Lato Black, a 1px foreground .13 line under it) with
//     p-quip_popout__comment_flexpane_toolbar (gap 8): Back while the view
//     has a back stack (caret-left-full), the title ("All threads",
//     "Unread comments", "Resolved threads"), then on the right the
//     Filter menu's refine button ("Filter" tooltip, bottom-right) and
//     Close ("Close", bottom-right).
//   - The Filter menu (QuipCommentsStreamFilterMenu, at the bottom): the
//     "Filter by" header, radios Most recent / Unread / Resolved, a
//     separator, then Resolve all threads when canArchiveAllComments,
//     which asks first (QuipArchiveAllModal: "Resolve all threads and
//     reactions?", danger).
//   - QuipCommentStream (commentsStream__kkkEa: base-sec, the list 16px
//     in from each side): getRows groups the roots by the day of their
//     latest reply (else their ts) under a date row (13px bold, padding
//     12px 16px, {day_divider_pretty}); each thread is a card
//     (defaultBackground: base-pry, 1px otl-ter sides, the first row's top
//     and radius 12, the last row's bottom, radius 12 and 16px under it):
//     the root (ThreadRootCanvas → QuipCommentRoot: padding 12px 20px 8px
//     12px, the 4px base-ter attachment bar, the highlighted text 12px
//     after it), "Show N more replies" when there are more than one (32px,
//     padding-left 16, surf-pry on hover), the unread line when the root
//     isn't read (padding 8px 16px, a 1px otl-imp rule, "New" 12px bold
//     content-imp), and the latest reply (a thread reply row, padding-bottom
//     8). Every row opens the thread.
//   - Empty (CommentStreamEmptyView): the threads icon (32px) over
//     EmptyState's title (18px Black) and description, per filter.
//   - Loading the first page: "Loading threads". Twenty roots a page; the
//     next page loads near the end (loadPost).

import (
	"context"
	"image"
	"image/color"
	"time"

	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func themeText(size, lh unit.Sp, w font.Weight, c color.NRGBA) theme.TextStyle {
	return theme.TextStyle{Size: size, Weight: w, Color: c, LineHeight: lh}
}

func themeTextAlign(size, lh unit.Sp, w font.Weight, c color.NRGBA, a text.Alignment) theme.TextStyle {
	return theme.TextStyle{Size: size, Weight: w, Color: c, LineHeight: lh, Align: a}
}

// Slack's strings for the stream: slack-docs (the stream and the resolve
// all modal), quip (the flexpane header and the filter menu), flexpane.
var nsComments = i18n.NewChain("slack-docs", "quip", "slack_docs", "flexpane", "messages")

// CommentThreadOpener is optionally implemented by the Host: open the
// comment thread at root ts in file channel ch over the stream of canvas
// file (Slack's showQuipCommentThread from the stream: the thread view's
// Back returns to it). Without it the stream calls OpenThread.
type CommentThreadOpener interface {
	OpenCommentThread(file, ch, ts string)
}

// SecondaryBacker is optionally implemented by the Host: the secondary
// view has a back stack (FlexpaneBackButton) and GoSecondaryBack pops it.
type SecondaryBacker interface {
	SecondaryBack() bool
	GoSecondaryBack()
}

type ccData struct {
	info    core.CanvasInfo // files.info (canArchiveAllComments)
	roots   []core.Message
	replies map[string][]core.Message
	cursor  string
	hasMore bool
}

// ccRowKind are getRows' row types.
type ccRowKind uint8

const (
	ccDate ccRowKind = iota
	ccRoot
	ccMore
	ccUnread
	ccReply
)

type ccRow struct {
	kind  ccRowKind
	key   string
	label string        // a date row's day
	root  *core.Message // the thread's root (every row but a date)
	reply *core.Message // a reply row's message
	more  int           // "Show N more replies"
	last  bool          // the card's last row
}

// CanvasComments is the comment stream of one canvas.
type CanvasComments struct {
	base
	file   string
	filter string

	data   async[ccData]
	list   widget.List
	clicks map[string]*widget.Clickable
	w      watcher
	paging bool

	back, filterBtn, closeBtn widget.Clickable
	filterTip, closeTip       popups.TipState
	filterR                   image.Rectangle // the Filter button, window coordinates
	origin                    image.Point     // the view's window origin (last frame)
	notes                     actNotes
	// sizes are this frame's list element heights; rects the rows' window
	// boxes by key (tests: a row is clicked where it is drawn)
	sizes map[int]int
	rects map[string]image.Rectangle
}

// SecondaryOriginer is optionally implemented by the Host: the secondary
// pane's window origin, where the stream is drawn (menus anchor to it).
type SecondaryOriginer interface {
	SecondaryOrigin() image.Point
}

// NewCanvasComments is the comment stream of canvas file, on filter
// (core.CommentsLatest, CommentsUnread, CommentsResolved; "" = Most
// recent).
func NewCanvasComments(h Host, file, filter string) *CanvasComments {
	if filter == "" {
		filter = core.CommentsLatest
	}
	v := &CanvasComments{base: newBase(h), file: file, filter: filter, clicks: map[string]*widget.Clickable{},
		sizes: map[int]int{}, rects: map[string]image.Rectangle{}}
	v.list.Axis = layout.Vertical
	v.filterTip.Delay, v.closeTip.Delay = 150*time.Millisecond, 150*time.Millisecond
	return v
}

// File is the canvas the stream belongs to.
func (v *CanvasComments) File() string { return v.file }

// Filter is the stream's document_stream filter.
func (v *CanvasComments) Filter() string { return v.filter }

// SetFilter is the Filter menu's radio: the stream reloads on it.
func (v *CanvasComments) SetFilter(f string) {
	if f == v.filter {
		return
	}
	v.filter = f
	v.list.Position = layout.Position{}
	v.load(false)
	v.h.Invalidate()
}

// Title is the header's title for the filter.
func (v *CanvasComments) Title() string {
	switch v.filter {
	case core.CommentsUnread:
		return nsComments.T("Unread comments")
	case core.CommentsResolved:
		return nsComments.T("Resolved threads")
	}
	return nsComments.T("All threads")
}

// Roots are the loaded thread roots, in order (tests).
func (v *CanvasComments) Roots() []core.Message {
	d, _, _ := v.data.get()
	return d.roots
}

// Loaded reports whether the first page arrived (tests).
func (v *CanvasComments) Loaded() bool {
	_, ok, _ := v.data.get()
	return ok && !v.data.busy()
}

// FilterRect is the Filter button (window coordinates; tests).
func (v *CanvasComments) FilterRect() image.Rectangle { return v.filterR }

// RowRect is the row with key (a root's or reply's ts, "load-more-<root
// ts>") as last drawn, window coordinates; empty when it isn't in view.
func (v *CanvasComments) RowRect(key string) image.Rectangle { return v.rects[key] }

// match: a reply or an edit in this canvas's file channel, or a root
// changing (resolve, reactions), reloads the stream (Slack's
// maybeUpdateQuipCommentStreamFrom* thunks keep it current).
func (v *CanvasComments) match(ev core.Event) bool {
	ch := core.CanvasFileChannel(v.file)
	return ev.Channel == ch && (ev.Type == "message" || ev.Type == "update" || ev.Type == "delete" || ev.Type == "reaction")
}

func (v *CanvasComments) load(more bool) {
	c := v.h.Core()
	if c == nil {
		return
	}
	old, _, _ := v.data.get()
	cursor, file, filter := "", v.file, v.filter
	if more {
		cursor = old.cursor
	}
	v.paging = more
	v.data.load(v.h, more, func(ctx context.Context) (ccData, error) {
		p, err := c.CanvasComments(ctx, file, filter, cursor)
		if err != nil {
			return ccData{}, err
		}
		d := ccData{info: old.info, roots: p.Roots, replies: p.Replies, cursor: p.Cursor, hasMore: p.HasMore}
		if d.info.ID == "" {
			if info, err := c.CanvasInfo(ctx, file); err == nil {
				d.info = info
			}
		}
		if more { // the reducer concatenates pages
			seen := map[string]bool{}
			for _, r := range old.roots {
				seen[r.TS] = true
			}
			roots := append([]core.Message(nil), old.roots...)
			for _, r := range p.Roots {
				if !seen[r.TS] {
					roots = append(roots, r)
				}
			}
			reps := map[string][]core.Message{}
			for k, x := range old.replies {
				reps[k] = x
			}
			for k, x := range p.Replies {
				reps[k] = x
			}
			d.roots, d.replies = roots, reps
		}
		return d, nil
	})
}

// rows is getRows: per day of the latest reply, a date row, then each
// thread's root, "Show N more replies", the unread line and the latest
// reply.
func (v *CanvasComments) rows(d ccData) []ccRow {
	var out []ccRow
	day := ""
	now := time.Now()
	for i := range d.roots {
		r := &d.roots[i]
		at := r.LatestReply
		if at == "" {
			at = r.TS
		}
		t := tsTime(at)
		if k := t.Format("2006-01-02"); k != day {
			day = k
			out = append(out, ccRow{kind: ccDate, key: "date-label--" + k, label: i18n.DayDivider(i18n.Locale(), t, now, false)})
		}
		out = append(out, ccRow{kind: ccRoot, key: r.TS, root: r})
		reps := d.replies[r.TS]
		if r.ReplyCount == 0 || len(reps) == 0 {
			out[len(out)-1].last = true
			continue
		}
		if n := r.ReplyCount - 1; n > 0 {
			out = append(out, ccRow{kind: ccMore, key: "load-more-" + r.TS, root: r, more: n})
		}
		if read, ok := r.CommentRead(); ok && !read {
			out = append(out, ccRow{kind: ccUnread, key: "new-unread-label-" + r.TS, root: r})
		}
		last := &reps[len(reps)-1]
		out = append(out, ccRow{kind: ccReply, key: last.TS, root: r, reply: last, last: true})
	}
	return out
}

// Layout draws the stream.
func (v *CanvasComments) Layout(gtx C) D {
	v.frame()
	v.notes.drain(v.h)
	if v.h.Core() != nil {
		v.w.watch(v.h, v.match)
	}
	if !v.data.started() {
		v.load(false)
	}
	if v.w.due(gtx, 500*time.Millisecond) && !v.data.busy() {
		v.load(false)
	}
	if t, ok := v.h.(SecondaryOriginer); ok {
		v.origin = t.SecondaryOrigin()
	}
	size := gtx.Constraints.Max
	fill(gtx, image.Rectangle{Max: size}, v.p.Bg)
	hh := gtx.Dp(50)
	v.header(gtx, size.X, hh)
	body := gtx
	body.Constraints = layout.Exact(image.Pt(size.X, max(0, size.Y-hh)))
	st := op.Offset(image.Pt(0, hh)).Push(gtx.Ops)
	v.body(body)
	st.Pop()
	return D{Size: size}
}

func (v *CanvasComments) header(gtx C, w, hh int) {
	if v.back.Clicked(gtx) {
		if b, ok := v.h.(SecondaryBacker); ok {
			b.GoSecondaryBack()
		}
	}
	if v.closeBtn.Clicked(gtx) {
		v.h.CloseSecondary()
	}
	if v.filterBtn.Clicked(gtx) {
		v.openFilter()
	}
	line := max(1, gtx.Dp(1))
	fill(gtx, image.Rect(0, hh-line, w, hh), v.p.Border)
	bs := gtx.Dp(28)
	x := gtx.Dp(12)
	y := (hh - line - bs) / 2
	if b, ok := v.h.(SecondaryBacker); ok && b.SecondaryBack() {
		at := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
		v.iconButton(gtx, &v.back, "caret-left-full", 28, 20, v.p.Sec)
		at.Pop()
		x += bs + gtx.Dp(8)
	}
	cx := w - gtx.Dp(12) - bs
	fx := cx - gtx.Dp(8) - bs
	v.filterR = image.Rect(fx, y, fx+bs, y+bs).Add(v.origin)
	tw := max(0, fx-gtx.Dp(8)-x)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(tw, hh)}
	rec := op.Record(gtx.Ops)
	d := v.th.Layout(g, themeText(18, 24, wBlack, v.p.Text), v.Title())
	call := rec.Stop()
	t := op.Offset(image.Pt(x, (hh-line-d.Size.Y)/2)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	for _, b := range []struct {
		c    *widget.Clickable
		tip  *popups.TipState
		x    int
		icon string
		text string
	}{{&v.filterBtn, &v.filterTip, fx, "refine", nsComments.T("Filter")}, {&v.closeBtn, &v.closeTip, cx, "close", nsComments.T("Close")}} {
		at := op.Offset(image.Pt(b.x, y)).Push(gtx.Ops)
		d := v.iconButton(gtx, b.c, b.icon, 28, 20, v.p.Sec)
		popups.Tooltip(gtx, v.th, b.tip, b.c.Hovered(), d.Size, popups.BelowRight, b.text, "")
		at.Pop()
	}
}

// openFilter is QuipCommentsStreamFilterMenu.
func (v *CanvasComments) openFilter() {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	set := func(f string) func() { return func() { v.SetFilter(f) } }
	items := []popups.MenuItem{
		{Header: nsComments.T("Filter by")},
		{Key: "most-recent-filter", Label: nsComments.T("Most recent"), Radio: true, Checked: v.filter == core.CommentsLatest || v.filter == "", Do: set(core.CommentsLatest)},
		{Key: "unread-filter", Label: nsComments.T("Unread"), Radio: true, Checked: v.filter == core.CommentsUnread, Do: set(core.CommentsUnread)},
		{Key: "archived-filter", Label: nsComments.T("Resolved"), Radio: true, Checked: v.filter == core.CommentsResolved, Do: set(core.CommentsResolved)},
	}
	if d, _, _ := v.data.get(); d.info.ID != "" && core.CanResolveAllComments(d.info) {
		items = append(items, popups.MenuItem{Separator: true},
			popups.MenuItem{Key: "archive", Label: nsComments.T("Resolve all threads"), Do: v.confirmResolveAll})
	}
	ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: v.filterR, Popover: "bottom"}))
}

// confirmResolveAll is QuipArchiveAllModal.
func (v *CanvasComments) confirmResolveAll() {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	title := nsComments.T("Resolve all threads and reactions?")
	body := nsComments.T("Are you sure you want to resolve all the threads and reactions in this canvas? You’ll still be able to find the comments via search, and individually re-open them, if you like. You won’t be able to undo this for reactions.")
	ph.ShowOverlay(popups.Confirm(ph, title, body, title, true, nil, func() {
		c, file := v.h.Core(), v.file
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := c.ResolveAllComments(ctx, file); err != nil {
				v.notes.add(v.h, err.Error())
				return
			}
			v.notes.mu.Lock()
			v.notes.fns = append(v.notes.fns, func() { v.load(false) })
			v.notes.mu.Unlock()
			v.h.Invalidate()
		}()
	}))
}

func (v *CanvasComments) body(gtx C) D {
	size := gtx.Constraints.Max
	fill(gtx, image.Rectangle{Max: size}, v.p.Page)
	d, loaded, err := v.data.get()
	if !loaded && v.data.busy() && !v.paging {
		return v.centerText(gtx, nsComments.T("Loading threads"))
	}
	if err != nil && len(d.roots) == 0 {
		return v.centerText(gtx, err.Error())
	}
	rows := v.rows(d)
	if len(rows) == 0 {
		return v.empty(gtx)
	}
	if d.hasMore && !v.data.busy() && v.list.Position.First+v.list.Position.Count >= len(rows)-2 {
		v.load(true) // loadPost
	}
	clear(v.sizes)
	clear(v.rects)
	side := gtx.Dp(16)
	ld := layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return v.list.Layout(gtx, len(rows), func(gtx C, i int) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			r := rows[i]
			d := v.row(gtx, r)
			v.sizes[i] = d.Size.Y
			if r.last {
				v.sizes[i] -= gtx.Dp(16) // the card's margin isn't the row's
			}
			if i == len(rows)-1 { // lastRow__cNSsg
				d.Size.Y += gtx.Dp(12)
			}
			return d
		})
	})
	// the rows' window boxes, from the list's final position
	y := -v.list.Position.Offset
	top := v.origin.Add(image.Pt(side, gtx.Dp(50)))
	for i := v.list.Position.First; i < len(rows); i++ {
		h, ok := v.sizes[i]
		if !ok {
			break
		}
		v.rects[rows[i].key] = image.Rect(0, y, size.X-2*side, y+h).Add(top)
		y += h
		if rows[i].last {
			y += gtx.Dp(16)
		}
	}
	return ld
}

func (v *CanvasComments) click(key string) *widget.Clickable {
	c := v.clicks[key]
	if c == nil {
		c = new(widget.Clickable)
		v.clicks[key] = c
	}
	return c
}

// open is the rows' click: showQuipCommentThread for the root.
func (v *CanvasComments) open(root *core.Message) {
	ch := core.CanvasFileChannel(v.file)
	if o, ok := v.h.(CommentThreadOpener); ok {
		o.OpenCommentThread(v.file, ch, root.TS)
		return
	}
	v.h.OpenThread(ch, root.TS)
}

func (v *CanvasComments) tok(name string) color.NRGBA {
	c, _ := css.ParseColor(css.SlackVars(v.th.Dark)["--dt_color-"+name])
	return c
}

func (v *CanvasComments) row(gtx C, r ccRow) D {
	if r.kind == ccDate { // divider__AGssM
		return layout.Inset{Top: 12, Bottom: 12, Left: 16, Right: 16}.Layout(gtx, func(gtx C) D {
			return v.para(gtx, 13, 16.25, wBold, v.p.Text, 1, r.label)
		})
	}
	ck := v.click(r.key)
	if ck.Clicked(gtx) && r.root != nil {
		v.open(r.root)
	}
	rec := op.Record(gtx.Ops)
	g := gtx
	var d D
	switch r.kind {
	case ccRoot:
		d = ck.Layout(g, func(gtx C) D { return v.rootRow(gtx, r.root, ck.Hovered()) })
	case ccMore:
		d = ck.Layout(g, func(gtx C) D { return v.moreRow(gtx, r.more, ck.Hovered()) })
	case ccUnread:
		d = v.unreadRow(g)
	case ccReply:
		d = ck.Layout(g, func(gtx C) D { return v.replyRow(gtx, r.reply, ck.Hovered()) })
	}
	call := rec.Stop()
	w, h := d.Size.X, d.Size.Y
	line := max(1, gtx.Dp(1))
	rad := gtx.Dp(12)
	first := r.kind == ccRoot
	// defaultBackground: base-pry with otl-ter sides; first: the top
	// line and top radius; last: the bottom line, bottom radius, 16px
	// under it
	edge := v.p.CardLine
	card := image.Rect(0, 0, w, h)
	rr := clip.RRect{Rect: card}
	if first {
		rr.NW, rr.NE = rad, rad
	}
	if r.last {
		rr.SW, rr.SE = rad, rad
	}
	paintRRect(gtx, rr, edge)
	inner := clip.RRect{Rect: image.Rect(line, 0, w-line, h)}
	if first {
		inner.Rect.Min.Y = line
		inner.NW, inner.NE = rad-line, rad-line
	}
	if r.last {
		inner.Rect.Max.Y = h - line
		inner.SW, inner.SE = rad-line, rad-line
	}
	paintRRect(gtx, inner, v.p.Bg)
	if ck.Hovered() && r.kind != ccUnread {
		bg := v.p.Hover
		if r.kind == ccMore {
			bg = v.tok("surf-pry")
		}
		paintRRect(gtx, inner, bg)
	}
	call.Add(gtx.Ops)
	if r.last {
		h += gtx.Dp(16)
	}
	return D{Size: image.Pt(w, h)}
}

func paintRRect(gtx C, rr clip.RRect, c color.NRGBA) {
	if rr.Rect.Empty() {
		return
	}
	st := rr.Push(gtx.Ops)
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	st.Pop()
}

// rootRow is ThreadRootCanvas: QuipCommentRoot without its heading (the
// stream isn't a thread context): the attachment bar and the highlighted
// text ("This content was deleted." when Quip no longer shows it).
func (v *CanvasComments) rootRow(gtx C, m *core.Message, hovered bool) D {
	pointer.CursorPointer.Add(gtx.Ops)
	return layout.Inset{Top: 12, Right: 20, Bottom: 8, Left: 12}.Layout(gtx, func(gtx C) D {
		bar := gtx.Dp(4)
		rec := op.Record(gtx.Ops)
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(max(0, gtx.Constraints.Max.X-bar-gtx.Dp(24)), 1<<20)}
		var bd D
		if dc := m.DocumentComment; dc != nil && !dc.IsVisible {
			bd = v.para(g, 15, 22, wReg, v.p.Muted, 0, nsComments.T("This content was deleted."))
		} else {
			bd = v.msgBody(g, m, false, 0)
		}
		call := rec.Stop()
		h := max(bd.Size.Y-gtx.Dp(4), gtx.Dp(8))                       // .p-quip_comment_root__body margin-bottom -4px
		rrect(gtx, image.Rect(0, 0, bar, h), bar/2, v.tok("base-ter")) // radius 8, clamped to the 4px width
		t := op.Offset(image.Pt(bar+gtx.Dp(12), 0)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		t.Pop()
		return D{Size: image.Pt(gtx.Constraints.Max.X, h)}
	})
}

// moreRow is LoadRepliesMessage: "Show N more replies".
func (v *CanvasComments) moreRow(gtx C, n int, hovered bool) D {
	pointer.CursorPointer.Add(gtx.Ops)
	h := gtx.Dp(32)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(gtx.Constraints.Max.X, h)}
	rec := op.Record(gtx.Ops)
	d := v.para(g, 13, 18, wBold, v.p.Link, 1, nsComments.T("Show {replyCount, plural, =1 {# more reply} other {# more replies}}", "replyCount", n))
	call := rec.Stop()
	t := op.Offset(image.Pt(gtx.Dp(16), (h-d.Size.Y)/2)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	return D{Size: image.Pt(gtx.Constraints.Max.X, h)}
}

// unreadRow is the stream's UnreadDivider: padding 8px 16px, a 1px
// otl-imp rule, then "New" 8px after it.
func (v *CanvasComments) unreadRow(gtx C) D {
	w := gtx.Constraints.Max.X
	label := nsComments.T("New")
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
	rec := op.Record(gtx.Ops)
	d := v.para(g, 12, 15, wBold, v.tok("content-imp"), 1, label)
	call := rec.Stop()
	pad := gtx.Dp(16)
	lx := w - pad - d.Size.X
	h := gtx.Dp(8)*2 + d.Size.Y
	ry := h / 2
	fill(gtx, image.Rect(pad, ry, max(pad, lx-gtx.Dp(8)), ry+max(1, gtx.Dp(1))), v.tok("otl-imp"))
	t := op.Offset(image.Pt(lx, gtx.Dp(8))).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	return D{Size: image.Pt(w, h)}
}

// replyRow is the latest reply (ThreadReply in the stream): avatar 36,
// the sender and time, the body and its reactions; padding-bottom 8.
func (v *CanvasComments) replyRow(gtx C, m *core.Message, hovered bool) D {
	pointer.CursorPointer.Add(gtx.Ops)
	name, av := v.authorOf(m)
	stamp, next := thrTime(tsTime(m.TS), gtx.Now)
	if next > 0 {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(next)})
	}
	return layout.Inset{Top: 8, Bottom: 8 + 8, Left: thrMsgPad, Right: thrMsgPad}.Layout(gtx, func(gtx C) D {
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
						return layout.Inset{Top: 4}.Layout(gtx, func(gtx C) D { return v.actReactions(gtx, m) })
					}),
				)
			}),
		)
	})
}

func (v *CanvasComments) meta() color.NRGBA { return v.p.Meta }

// empty is CommentStreamEmptyView.
func (v *CanvasComments) empty(gtx C) D {
	title := nsComments.T("Nobody’s added any comments")
	desc := nsComments.T("When someone adds a comment in this canvas, it’ll show up here. Highlight text to create your own.")
	switch v.filter {
	case core.CommentsResolved:
		title = nsComments.T("There aren’t any resolved threads")
		desc = nsComments.T("Resolve threads that aren’t needed anymore. Once you do, they’ll end up right here.")
	case core.CommentsUnread:
		title = nsComments.T("No unread comments")
		desc = nsComments.T("You’re all caught up. Enjoy the rest of your day.")
	}
	size := gtx.Constraints.Max
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(min(size.X-gtx.Dp(40), gtx.Dp(380)), size.Y)}
	rec := op.Record(gtx.Ops)
	d := layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(g,
		layout.Rigid(func(gtx C) D { return v.icon(gtx, "threads", 32, v.p.Text) }),
		vgap(12),
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return v.th.Layout(gtx, themeTextAlign(18, 24, wBlack, v.p.Text, text.Middle), title)
		}),
		vgap(8),
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return v.th.Layout(gtx, themeTextAlign(15, 22, wReg, v.p.Text, text.Middle), desc)
		}),
	)
	call := rec.Stop()
	t := op.Offset(image.Pt((size.X-d.Size.X)/2, (size.Y-d.Size.Y)/2)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	return D{Size: size}
}

func (v *CanvasComments) centerText(gtx C, s string) D {
	size := gtx.Constraints.Max
	g := gtx
	g.Constraints = layout.Constraints{Max: size}
	rec := op.Record(gtx.Ops)
	d := v.para(g, 15, 22, wReg, v.p.Muted, 0, s)
	call := rec.Stop()
	t := op.Offset(image.Pt((size.X-d.Size.X)/2, (size.Y-d.Size.Y)/2)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	return D{Size: size}
}
