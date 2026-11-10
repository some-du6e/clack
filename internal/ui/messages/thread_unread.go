package messages

import (
	"context"
	"image"
	"image/color"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The thread pane's read state, from Slack's ThreadsFlexpane
// (client-boot-shared puxw, build 407aff01646b):
//
//   - getDerivedStateFromProps: once the thread's timestamps arrive, a
//     followed thread whose newest real message (maxRealTs: not a temp
//     or ephemeral one) is past the subscription's lastRead gets an
//     initialLastRead (that lastRead); anything else gets none. It is
//     decided once per open and cleared by Esc's markThreadRead.
//   - getThreadRowsAndKeys: when the first reply is past initialLastRead
//     and not the viewer's, the "N replies" separator itself carries the
//     marker (separatorType "unread", the red line with "New"); otherwise
//     an UnreadDivider ("New" at the right end of a red line) goes before
//     the first reply past it that isn't a temp message or the viewer's.
//     Once the thread is marked read the marker stays as
//     "unread_marked_read" (the ghost: its line at half opacity after 3 s).
//   - checkUnreads: while the user's attention is on the thread (window
//     focused, no overlay) a followed thread with lastRead < maxMarkableTs
//     is marked read 350 ms later (setThreadLastRead).
//   - getThreadStartPosition: a thread opens at its first unread reply
//     (offset 100), at the reply it was last read to, or at the root.
//   - onEsc: Esc marks the thread read when there is something to mark;
//     otherwise the secondary view's Escape closes the pane.

// Marker kinds (item.mark): the separator's separatorType and the
// UnreadDivider's ghost.
const (
	thrMarkNone   uint8 = iota
	thrMarkUnread       // "unread": the red line and "New"
	thrMarkGhost        // "unread_marked_read" / separatorType "read": the ghost
)

// threadRead is one open of the pane (Slack's component state).
type threadRead struct {
	decided bool      // initialLastRead is known
	initial string    // initialLastRead, "" for none
	ghostAt time.Time // when the marker turned into the ghost
	armed   time.Time // checkUnreads' setThreadLastRead timer
	marked  string    // the last ts this pane marked
	started bool      // the open position is applied
	// open is the lastRead the pane opened with (withOriginalLastRead:
	// the subscription's lastRead, "" when not followed).
	open string
}

// AttentionHost is optional: Slack marks a thread read only while the
// user's attention is on it (isUserAttentionOnThread: the window has
// the focus and no overlay is open). Hosts without it count as
// attentive.
type AttentionHost interface {
	UserAttention() bool
}

// threadRoot is the loaded root message.
func (s *listState) threadRoot() (*core.Message, bool) {
	if len(s.snap.msgs) == 0 || s.snap.msgs[0].m.TS != s.ts {
		return nil, false
	}
	return &s.snap.msgs[0].m, true
}

// threadSub is use_thread_subscription: followed, and its lastRead.
func (s *listState) threadSub() (subscribed bool, lastRead string) {
	r, ok := s.threadRoot()
	if !ok {
		return false, ""
	}
	if !r.FollowingThread {
		return false, ""
	}
	return true, r.ThreadLastRead
}

// maxMarkable is maxRealTs over the thread's timestamps: the newest
// message that isn't a temp (pending) one.
func (s *listState) maxMarkable() string {
	for i := len(s.snap.msgs) - 1; i >= 0; i-- {
		if m := &s.snap.msgs[i].m; !m.Pending && !m.Failed {
			return m.TS
		}
	}
	return ""
}

// decideThreadRead is getDerivedStateFromProps' initialLastRead, run
// once per open when the thread has loaded.
func (s *listState) decideThreadRead() {
	if s.thr.decided || !s.snap.loaded {
		return
	}
	if _, ok := s.threadRoot(); !ok {
		return
	}
	s.thr.decided = true
	sub, lr := s.threadSub()
	s.thr.open = lr
	if !sub {
		return
	}
	if mx := s.maxMarkable(); mx != "" && tsLess(lr, mx) {
		s.thr.initial = lr // "" when the thread was never read: no marker
	}
}

// threadMarkKind is the UnreadDivider's type: "unread" while the
// subscription's lastRead is still the one the pane opened with,
// "unread_marked_read" after.
func (s *listState) threadMarkKind() uint8 {
	if _, lr := s.threadSub(); lr == s.thr.initial {
		return thrMarkUnread
	}
	return thrMarkGhost
}

// threadSeparatorMark is getThreadRowsAndKeys' F: the separator carries
// the marker when every reply is past initialLastRead and the first one
// isn't the viewer's.
func (s *listState) threadSeparatorMark(self string) uint8 {
	sub, _ := s.threadSub()
	if !sub || s.thr.initial == "" || len(s.snap.msgs) < 2 {
		return thrMarkNone
	}
	first := &s.snap.msgs[1].m
	if !tsLess(s.thr.initial, first.TS) || (self != "" && first.User == self) {
		return thrMarkNone
	}
	return s.threadMarkKind()
}

// takesThreadMarker is getThreadRowsAndKeys' takesMarker for a reply.
func (s *listState) takesThreadMarker(m *core.Message, self string) bool {
	sub, _ := s.threadSub()
	return sub && s.thr.initial != "" && tsLess(s.thr.initial, m.TS) && !m.Pending && (self == "" || m.User != self)
}

// shouldMarkThreadRead is shouldMarkThreadAsRead (Esc).
func (s *listState) shouldMarkThreadRead() bool {
	sub, lr := s.threadSub()
	if !sub {
		return false
	}
	return !((s.thr.initial == "" || s.thr.initial == lr) && lr == s.maxMarkable())
}

// markThreadRead is Esc's markThreadRead: no marker any more, and the
// thread marked read up to maxMarkableTs.
func (s *listState) markThreadRead(h Host) {
	s.thr.initial = ""
	s.snap.rev = 0 // rebuild without the marker
	if mx := s.maxMarkable(); mx != "" {
		s.setThreadLastRead(h, mx)
	}
}

// checkThreadUnreads is checkUnreads: mark a followed thread read 350 ms
// after it has something to mark while the user's attention is on it.
func (s *listState) checkThreadUnreads(gtx layout.Context, h Host, now time.Time) {
	sub, lr := s.threadSub()
	mx := s.maxMarkable()
	if !sub || !s.snap.loaded || s.snap.loading || mx == "" || !tsLess(lr, mx) || mx == s.thr.marked {
		s.thr.armed = time.Time{}
		return
	}
	if ah, ok := h.(AttentionHost); ok && !ah.UserAttention() {
		s.thr.armed = time.Time{} // Not marking …: the window isn't focused
		return
	}
	if s.thr.armed.IsZero() {
		s.thr.armed = now
	}
	if at := s.thr.armed.Add(350 * time.Millisecond); now.Before(at) {
		gtx.Execute(op.InvalidateCmd{At: at})
		return
	}
	s.thr.armed = time.Time{}
	s.setThreadLastRead(h, mx)
}

// setThreadLastRead marks the thread read up to ts: Slack's thread mark
// through markLastRead {id, ts, threadTs} (plugins may wrap it), then
// subscriptions.thread.mark. Off the UI goroutine.
func (s *listState) setThreadLastRead(h Host, ts string) {
	app := h.Core()
	if app == nil || ts == "" {
		return
	}
	s.thr.marked = ts
	ch, thread := s.ch, s.ts
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = markThread(ctx, app, ch, thread, ts)
		h.Invalidate()
	}()
}

// markThread marks thread read up to ts through markLastRead with its
// threadTs (the Threads view and Taut's thunk take the same path).
func markThread(ctx context.Context, app *core.App, ch, thread, ts string) error {
	_, err := taut.Dispatch(ctx, tautOf(), "markLastRead", markLastReadArgs{ID: ch, TS: ts, ThreadTS: thread},
		func(ctx context.Context, a markLastReadArgs) (any, error) {
			if a.ThreadTS == "" {
				_, err := app.Mark(ctx, a.ID, a.TS)
				return nil, err
			}
			return nil, app.MarkThread(ctx, a.ID, a.ThreadTS, a.TS)
		})
	return err
}

// threadStart is getThreadStartPosition for a pane that just opened:
// the item to start at and whether it sits 100px below the top (Slack's
// startOffset 100) rather than at the top. jump is a reply the route
// asked for ("" none).
func (s *listState) threadStart(jump string) (key string, offset bool) {
	root := s.ts
	start := jump
	if start == "" {
		start = s.thr.open // originalLastRead
	}
	if start == "" {
		return root, false // !startTs: the root, no offset
	}
	// firstUnreadTs: the first timestamp past originalLastRead (not the
	// root), else originalLastRead itself
	first := ""
	if o := s.thr.open; o != "" {
		first = o
		for i := range s.snap.msgs {
			if ts := s.snap.msgs[i].m.TS; ts != root && tsLess(o, ts) {
				first = ts
				break
			}
		}
	}
	switch {
	case first != "" && s.thr.open == start:
		return first, true
	case first == "":
		// the newest timestamp at or before startTs
		k := start
		for i := range s.snap.msgs {
			if ts := s.snap.msgs[i].m.TS; !tsLess(start, ts) {
				k = ts
			}
		}
		return k, true
	}
	return start, start != root && tsLess(s.thr.initial, start)
}

// threadStartPosition is threadStart as a list position: the item at y
// 100 (or 0), clamped by the list at either end like Slack's
// AnchorLayout brackets scrollTop.
func (s *listState) threadStartPosition(gtx layout.Context, jump string) layout.Position {
	key, off := s.threadStart(jump)
	for i, it := range s.items {
		if it.kind == itMsg && it.key == key {
			p := layout.Position{BeforeEnd: true, First: i}
			if off {
				p.Offset = -dp(gtx, 100)
			}
			return p
		}
	}
	return layout.Position{BeforeEnd: true}
}

// threadNewColor is the thread pane's unread red: the UnreadDivider's
// c-message_list__unread_divider--unread_threads and the separator's
// unread line are #de4e2b in the dark theme, raspberry red in light.
func threadNewColor(th *theme.Theme, c *colors) color.NRGBA {
	if th.Dark {
		return color.NRGBA{R: 0xde, G: 0x4e, B: 0x2b, A: 0xff}
	}
	return c.NewLine
}

// ghostLine is the marked-as-read line: opacity .5 after a 3 s delay
// (transition: opacity .1s 3s).
func (s *listState) ghostLine(gtx layout.Context, col color.NRGBA, now time.Time) color.NRGBA {
	if s.thr.ghostAt.IsZero() {
		s.thr.ghostAt = now
	}
	at := s.thr.ghostAt.Add(3 * time.Second)
	if now.Before(at) {
		gtx.Execute(op.InvalidateCmd{At: at})
		return col
	}
	f := float32(now.Sub(at)) / float32(100*time.Millisecond)
	if f > 1 {
		f = 1
	} else {
		gtx.Execute(op.InvalidateCmd{})
	}
	col.A = uint8(float32(col.A) * (1 - 0.5*f))
	return col
}

// threadSeparator is ThreadSeparatorRowGeneric (EWXh): the root's reply
// count (Math.max(1, reply_count), not the replies loaded) as a 13px/18px
// label at 70% ink (padding-left 16, margin-right 12) and a 1px rule to
// width - 22, vertically centred (y 13), margin 4px 0 and margin-bottom
// -2 on the row: 24 tall. With the marker (separatorType "unread" or
// "read") the rule is the thread red and ends at the "New" label: 12px
// bold red, line-height 8, padding 2px 4px 4px, a 1px clear border,
// margin-top 1, on the pane's background.
func (s *listState) threadSeparator(gtx layout.Context, h Host, c *colors, it item, w int, now time.Time) layout.Dimensions {
	th := h.Theme()
	hh := dp(gtx, 24)
	n := 0
	if r, ok := s.threadRoot(); ok {
		n = r.ReplyCount
	}
	txt := repliesLabel(max(n, 1))
	st := textStyle{size: 13, col: c.Muted70, lineH: 18}
	d := at(gtx, dp(gtx, 16), dp(gtx, 4), func(gtx layout.Context) layout.Dimensions {
		return lineBox(gtx, st, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, st, txt) })
	})
	lx := dp(gtx, 16) + d.Size.X + dp(gtx, 12)
	end := w - dp(gtx, 22)
	line := c.PillBorder
	if it.mark != thrMarkNone {
		red := threadNewColor(th, c)
		ls := textStyle{size: 12, weight: theme.Bold, col: red}
		m := op.Record(gtx.Ops)
		lg := gtx
		lg.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)} // its own width (the row's Min is the row's)
		ld := label(lg, th, ls, threadsNS.T("New"))
		call := m.Stop()
		b, padX, padT, padB, lh := dp(gtx, 1), dp(gtx, 4), dp(gtx, 2), dp(gtx, 4), dp(gtx, 8)
		bw, bh := 2*b+2*padX+ld.Size.X, 2*b+padT+lh+padB
		bx := end - bw
		// align-items: center over the 18px count line, after margin-top 1
		by := dp(gtx, 4) + (dp(gtx, 18)-(bh+b))/2 + b
		fill(gtx.Ops, image.Rect(bx, by, bx+bw, by+bh), c.Bg)
		bl := by + b + padT + cssBaseline(gtx, 12, 8)
		t := op.Offset(image.Pt(bx+b+padX, bl-(ld.Size.Y-ld.Baseline))).Push(gtx.Ops)
		call.Add(gtx.Ops)
		t.Pop()
		end = bx
		line = red
		if it.mark == thrMarkGhost {
			line = s.ghostLine(gtx, red, now)
		}
	}
	if end > lx {
		fill(gtx.Ops, image.Rect(lx, dp(gtx, 13), end, dp(gtx, 13)+max(dp(gtx, 1), 1)), line)
	}
	return layout.Dimensions{Size: image.Pt(w, hh)}
}

// threadDivider is the thread pane's UnreadDivider (VxYf, class
// p-threads_flexpane__unread_divider, margin 0): the channel divider's
// 1px line and "New" (13px bold, padding 0 4, margin -10px 15px 0 0) in
// the thread red; marked read (p-threads_flexpane__marked_as_read_divider,
// ghost) the line drops to half opacity 3 s later.
func (s *listState) threadDivider(gtx layout.Context, h Host, c *colors, it item, w int, now time.Time) layout.Dimensions {
	th := h.Theme()
	red := threadNewColor(th, c)
	ls := textStyle{size: 13, weight: theme.Bold, col: red, lineH: 19.5}
	newTxt := i18n.T("messages", "New") // UnreadDivider's (the messages I18n)
	tw := measure(gtx, th, ls, newTxt).X
	lx := w - dp(gtx, 15) - dp(gtx, 4) - tw
	line := red
	if it.mark == thrMarkGhost {
		line = s.ghostLine(gtx, red, now)
	}
	fill(gtx.Ops, image.Rect(0, 0, lx-dp(gtx, 4), max(dp(gtx, 1), 1)), line)
	at(gtx, lx, -dp(gtx, 10), func(gtx layout.Context) layout.Dimensions {
		return lineBox(gtx, ls, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, ls, newTxt) })
	})
	return layout.Dimensions{Size: image.Pt(w, max(dp(gtx, 1), 1))}
}
