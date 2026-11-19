package views

// The Threads view's unread threads banners, from Slack's ThreadsView
// (client-threads-view 9Ra5). renderTopBanners and renderBottomBanners
// (2374-2441) are drawn around the list in every branch of render:
// ThreadBannerContainerGeneric (Oo3p: div.p-threads_view__top_banners, 5px
// down, and div.p-threads_view__bottom_banners, 8px up, both centred over
// the list) around ThreadUnreadBannerGeneric (ntqA) on
// ThreadBannerButtonsGeneric (JwHt), the pill the thread pane draws for
// its new replies. "↑ N unread threads" counts the unread threads whose
// "unread" row is above the visible range, "↓ N unread threads" those
// below it (getCurrentUnreadThreads 1714-1741), as
// determineUnreadThreadBannerVisibility last saw them: after a load, once
// a scroll has stopped, and when the threads change (componentDidUpdate's
// getScanInputs). The label scrolls the nearest such thread's divider
// 100px down the list and marks the thread read
// (scrollToNextUnreadAndMark 2171-2192). The close marks every thread
// read (markAllThreadsAsread 2193-2203: subscriptions.thread.clearAll and
// the hand marks cleared) and hides both banners for 5 s.
//
// Not ported: ntqA's dispatch of odUS Yu on a click or a close (its
// module isn't in the builds cached here), the scroll's animation, and
// the analytics events.

import (
	"context"
	"image"
	"log"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// thrBanners is the banners' share of ThreadsView's state:
// unreadsAboveTopBanner, unreadsBelowTopBanner, unreadsNotBelowCount,
// hideUnreadBanners with its 5 s timer, markedReadKeys and pendingReads.
type thrBanners struct {
	above, below []string // thread keys, in the list's order
	notBelow     int
	hide         bool
	unhideAt     time.Time
	// due: determine once this frame's list is laid out (a load landed,
	// a scroll stopped); listed: the frame drew the list (getCurrentUnreadThreads
	// needs its listRef); sig: the threads' scan inputs at the last update
	due, listed bool
	sig         string
	up, down    thrBannerButtons
	// marked is markedReadKeys, the threads the view marked read until a
	// frame shows them read; pending is pendingReads (reconcilePendingReads),
	// prev the unread threads the last update saw and count
	// userUnreadThreadsCount then
	marked  map[string]bool
	pending []thrPendingRead
	prev    map[string]bool
	count   int
}

// thrBannerButtons is ThreadBannerButtonsGeneric's two buttons: the
// label's and the close.
type thrBannerButtons struct{ jump, close widget.Clickable }

// thrPendingRead is one of pendingReads: a thread the view marked read
// that turned read, with its last read then and when.
type thrPendingRead struct {
	key, lastRead string
	at            time.Time
}

// threadUnread is isThreadUnread (wmZA): the thread's last read short of
// its newest reply, maxMarkableTs.
func (v *Threads) threadUnread(t *thrItem) bool { return actTSLess(v.lastRead(t), t.latest()) }

// unreadRowKey is the keyboard row the thread's "unread" row (not the
// ghost) goes before: its first unread reply, or load_older when the
// unread run starts among the hidden replies; ok false without one.
func (v *Threads) unreadRowKey(t *thrItem) (string, bool) {
	c := v.cards[t.key]
	replies := thrShown(t, c)
	at, ghost := v.unreadRow(t, c, replies)
	switch {
	case at == thrNoUnreadRow || ghost:
		return "", false
	case at == thrUnreadAtOlder:
		return "load_older-" + t.key, true
	}
	return t.key + "-" + replies[at].TS, true
}

// currentUnreads is getCurrentUnreadThreads over this frame's layout: the
// unread threads whose "unread" row is above getVisibleRange(void 0, true)
// (its point at or above the list's top) and below it (past its bottom),
// each in the list's order, and unreadsNotBelowCount, the followed unread
// threads with a row before the range's end whose unread row isn't below
// it. ok is false when no list was drawn.
func (v *Threads) currentUnreads(page thrData) (above, below []string, notBelow int, ok bool) {
	if !v.ban.listed {
		return nil, nil, 0, false
	}
	rowBelow := map[string]bool{}
	for i := range page.items {
		t := &page.items[i]
		key, has := v.unreadRowKey(t)
		if !has {
			continue
		}
		switch side, known := v.kb.side(key); {
		case !known:
		case side < 0:
			if v.threadUnread(t) {
				above = append(above, t.key)
			}
		case side > 0:
			rowBelow[t.key] = true
			if v.threadUnread(t) {
				below = append(below, t.key)
			}
		}
	}
	for i := range page.items {
		t := &page.items[i]
		if !v.followed(t) || !v.threadUnread(t) || rowBelow[t.key] {
			continue
		}
		if side, known := v.kb.side("heading-" + t.key); known && side <= 0 {
			notBelow++
		}
	}
	return above, below, notBelow, true
}

// determineBanners is determineUnreadThreadBannerVisibility: the threads
// above and below the visible range, none while the banners are hidden
// (the close's 5 s), which ends early once either side has none.
func (v *Threads) determineBanners(page thrData) {
	above, below, notBelow, ok := v.currentUnreads(page)
	if !ok {
		return
	}
	b := &v.ban
	hidden := b.hide
	b.hide = hidden && len(above) > 0 && len(below) > 0
	if hidden {
		above, below = nil, nil
	}
	b.above, b.below, b.notBelow = above, below, notBelow
}

// scanInputs is componentDidUpdate's getScanInputs over the threads:
// each one's last read, unread state and subscription, in order.
func (v *Threads) scanInputs(page thrData) string {
	var sb strings.Builder
	for i := range page.items {
		t := &page.items[i]
		sb.WriteString(t.key)
		sb.WriteByte('|')
		sb.WriteString(v.lastRead(t))
		if v.threadUnread(t) {
			sb.WriteString("|u")
		}
		if v.followed(t) {
			sb.WriteString("|f")
		}
		sb.WriteByte(';')
	}
	return sb.String()
}

// bannersLaidOut runs once body's list is laid out: componentDidUpdate's
// updatePendingReads when the threads or the unread threads count
// changed, and determineUnreadThreadBannerVisibility when a load landed,
// a scroll stopped (due) or the threads' scan inputs changed.
func (v *Threads) bannersLaidOut(gtx C, page thrData) {
	b := &v.ban
	b.listed = true
	sig, count := v.scanInputs(page), 0
	if app := v.h.Core(); app != nil {
		count = app.UnreadThreadsCount()
	}
	if sig != b.sig || count != b.count {
		v.updatePendingReads(gtx.Now, page, count != b.count)
	}
	if b.due || sig != b.sig {
		b.due = false
		v.determineBanners(page)
	}
	b.sig, b.count = sig, count
}

// updatePendingReads is updatePendingReads with reconcilePendingReads
// (796-830): a thread the view marked read that turned read joins
// pendingReads; one unread again leaves; when the count changed, only
// those marked less than 10 s ago stay (Clack keeps no counts timestamp to
// cut them at). markedReadKeys lets go of every thread now read.
func (v *Threads) updatePendingReads(now time.Time, page thrData, countUpdated bool) {
	b := &v.ban
	unread := make(map[string]bool, len(page.items))
	for i := range page.items {
		if v.threadUnread(&page.items[i]) {
			unread[page.items[i].key] = true
		}
	}
	var fresh []thrPendingRead
	for i := range page.items {
		t := &page.items[i]
		if b.marked[t.key] && b.prev[t.key] && !unread[t.key] {
			fresh = append(fresh, thrPendingRead{key: t.key, lastRead: v.lastRead(t), at: now})
		}
		if !unread[t.key] {
			delete(b.marked, t.key)
		}
	}
	keep := b.pending[:0:0]
	for _, p := range b.pending {
		if !unread[p.key] {
			keep = append(keep, p)
		}
	}
	keep = append(keep, fresh...)
	if countUpdated {
		cut := keep[:0:0]
		for _, p := range keep {
			if now.Sub(p.at) < 10*time.Second {
				cut = append(cut, p)
			}
		}
		keep = cut
	}
	b.pending, b.prev = keep, unread
}

// unreadsBelowCount is getUnreadsBelowCount: the threads below, or with
// more pages to load and an unread threads count, at least that count less
// the pending reads and the unread threads not below.
func (v *Threads) unreadsBelowCount(page thrData) int {
	b := &v.ban
	n := len(b.below)
	if n == 0 || !page.hasMore || b.count == 0 {
		return n
	}
	return max(n, b.count-len(b.pending)-b.notBelow)
}

// bannerJump is scrollToNextUnreadAndMark: the nearest thread past the
// banner's edge (the last above, the first below) is scrolled to its
// divider 100px down the list and marked read, its hand mark gone
// whatever its origin, when the list has a divider keyed with the thread's
// first timestamp past its last read (divider-<thread>-<n>, n over every
// timestamp the store holds, hidden replies too); else nothing happens.
func (v *Threads) bannerJump(gtx C, up bool) {
	page, _, _ := v.data.get()
	page.items = v.shownItems(page.items)
	above, below, _, ok := v.currentUnreads(page)
	if !ok {
		return
	}
	var key string
	switch {
	case up && len(above) > 0:
		key = above[len(above)-1]
	case !up && len(below) > 0:
		key = below[0]
	default:
		return
	}
	t := thrByKey(page.items, key)
	if t == nil {
		return
	}
	// n: the first timestamp the store holds for the thread past its last
	// read, not the root's; Slack's timestamps are every reply held, the
	// hidden ones too
	lr, first, held := v.lastRead(t), "", ""
	for _, m := range thrKnown(t, v.cards[key]) {
		if m.TS == t.Root.TS {
			continue
		}
		if held == "" {
			held = m.TS
		}
		if actTSLess(lr, m.TS) {
			first = m.TS
			break
		}
	}
	row, has := v.unreadRowKey(t)
	if first == "" || !has {
		return
	}
	// get_rows_and_keys keys a reply's divider with that reply and
	// load_older's with the first timestamp held: with no divider keyed
	// with n (its reply hidden behind "Show N more replies"), Slack's
	// click does nothing
	if row != key+"-"+first && (row != "load_older-"+key || first != held) {
		return
	}
	v.kb.scrollTo(row)
	v.list.Position.Offset -= gtx.Dp(100)
	v.sendRead(t) // and the hand mark goes, whatever its origin
}

// bannerClose is markAllThreadsAsread: both banners hidden for 5 s, and
// every thread read on the server (core.ClearAllThreads, which lets go of
// the hand marks first).
func (v *Threads) bannerClose(now time.Time) {
	v.ban.hide, v.ban.unhideAt = true, now.Add(5*time.Second)
	app := v.h.Core()
	if app == nil {
		return
	}
	app.ForgetThreadMarkedUnread("", "")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := app.ClearAllThreads(ctx); err != nil {
			log.Printf("threads: subscriptions.thread.clearAll: %v", err)
		}
	}()
}

// banners takes the banners' clicks and draws them over the view's body
// (area), after it: ntqA draws nothing unless its count is above 0 and
// showBanner.
func (v *Threads) banners(gtx C, area image.Point) {
	b := &v.ban
	if b.up.jump.Clicked(gtx) {
		v.bannerJump(gtx, true)
		gtx.Execute(op.InvalidateCmd{})
	}
	if b.down.jump.Clicked(gtx) {
		v.bannerJump(gtx, false)
		gtx.Execute(op.InvalidateCmd{})
	}
	if b.up.close.Clicked(gtx) || b.down.close.Clicked(gtx) {
		v.bannerClose(gtx.Now)
		gtx.Execute(op.InvalidateCmd{})
	}
	if !b.unhideAt.IsZero() {
		if gtx.Now.Before(b.unhideAt) {
			gtx.Execute(op.InvalidateCmd{At: b.unhideAt})
		} else {
			b.hide, b.unhideAt = false, time.Time{}
		}
	}
	if n := len(b.above); n > 0 {
		v.bannerPill(gtx, &b.up, true, thrUnreadThreadsLabel(n), area)
	}
	if len(b.below) > 0 {
		page, _, _ := v.data.get()
		if n := v.unreadsBelowCount(page); n > 0 {
			v.bannerPill(gtx, &b.down, false, thrUnreadThreadsLabel(n), area)
		}
	}
}

// thrUnreadThreadsLabel is ThreadUnreadBannerGeneric's threads string.
func thrUnreadThreadsLabel(n int) string {
	return nsThreads.T("{unreads,plural,=1{{unreads} unread thread}other{{unreads} unread threads}}", "unreads", n)
}

// bannerPill is ThreadBannerButtonsGeneric under .p-threads_view: a 28px
// #1264a3 pill, radius 14, 13px bold white on a 28px line, its baseline
// where Chrome puts it (thrLineBaseline). The label button has 8px sides:
// the arrow-up or arrow-down c-icon, the icon font's glyph in a 20px box
// whose baseline (the font's 1700/2000 ascent) sits on the line's, moved
// 2px down (top:2px), then 4px, a space and the text. The close is the
// times-small c-icon: 36px wide plus its 2px right padding (content-box),
// the glyph in the middle of the 36 and of the 28 under its 1px top
// padding. A hovered button is #0b4c8c. The top banner sits 5px down the
// area, the bottom one 8px up plus the pill's 4px margin-bottom. Chrome
// lays Slack's rules out to these boxes: the arrow box at (8, 4), the
// text from 32px with its baseline at 19, the close 38px wide.
func (v *Threads) bannerPill(gtx C, bb *thrBannerButtons, up bool, txt string, area image.Point) {
	white := rgb(0xffffff)
	bh, is := gtx.Dp(28), gtx.Dp(20)
	tw, space := v.th.Measure(gtx, 13, wBold, txt), v.th.Measure(gtx, 13, wBold, " ")
	jw := gtx.Dp(8) + is + gtx.Dp(4) + space + tw + gtx.Dp(8)
	cw := gtx.Dp(36) + gtx.Dp(2)
	w := jw + cw
	y, icon := gtx.Dp(5), "arrow-up-font"
	if !up {
		y, icon = area.Y-gtx.Dp(8)-gtx.Dp(4)-bh, "arrow-down-font"
	}
	bl := thrLineBaseline(gtx, v.th, 13, 28)
	r := bh / 2
	defer op.Offset(image.Pt((area.X-w)/2, y)).Push(gtx.Ops).Pop()
	rrect(gtx, image.Rectangle{Max: image.Pt(w, bh)}, r, rgb(0x1264a3))
	btn := func(c *widget.Clickable, x0, bw int, left bool, draw func(gtx C)) {
		st := op.Offset(image.Pt(x0, 0)).Push(gtx.Ops)
		defer st.Pop()
		g := gtx
		g.Constraints = layout.Exact(image.Pt(bw, bh))
		c.Layout(g, func(gtx C) D {
			if c.Hovered() {
				rr := clip.RRect{Rect: image.Rectangle{Max: image.Pt(bw, bh)}}
				if left {
					rr.NW, rr.SW = r, r
				} else {
					rr.NE, rr.SE = r, r
				}
				paint.FillShape(gtx.Ops, rgb(0x0b4c8c), rr.Op(gtx.Ops))
			}
			draw(gtx)
			defer clip.Rect{Max: image.Pt(bw, bh)}.Push(gtx.Ops).Pop()
			pointerCursor(gtx)
			return D{Size: image.Pt(bw, bh)}
		})
	}
	btn(&bb.jump, 0, jw, true, func(gtx C) {
		st := op.Offset(image.Pt(gtx.Dp(8), bl-gtx.Dp(17)+gtx.Dp(2))).Push(gtx.Ops)
		v.icon(gtx, icon, 20, white)
		st.Pop()
		// the label's baseline on the line's (Gio puts a line's at its
		// ascent, with no half-leading above)
		m := op.Record(gtx.Ops)
		g := gtx
		g.Constraints.Min = image.Point{}
		d := v.label(g, 13, wBold, white, txt)
		call := m.Stop()
		st = op.Offset(image.Pt(gtx.Dp(8)+is+gtx.Dp(4)+space, bl-(d.Size.Y-d.Baseline))).Push(gtx.Ops)
		call.Add(gtx.Ops)
		st.Pop()
	})
	btn(&bb.close, jw, cw, false, func(gtx C) {
		cs := gtx.Dp(20)
		st := op.Offset(image.Pt((cw-gtx.Dp(2)-cs)/2, gtx.Dp(1)+(bh-cs)/2)).Push(gtx.Ops)
		v.icon(gtx, "times-small", 20, white)
		st.Pop()
	})
}

// thrLineBaseline is where Chrome puts a size/lineH line's baseline from
// its line box's top: half the leading above the UI font's ascent and
// descent, then the ascent (messages' cssBaseline).
func thrLineBaseline(gtx C, th *theme.Theme, size, lineH float32) int {
	sp := gtx.Metric.PxPerSp
	e := theme.UIExtents(th.Shaper)
	return int((lineH*sp-(e.Ascent+e.Descent)*size*sp)/2 + e.Ascent*size*sp + 0.5)
}
