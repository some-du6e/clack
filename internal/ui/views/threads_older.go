package views

import (
	"context"
	"image"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
)

// "Show N more replies" in the Threads view, from Slack's ThreadsView
// (client-threads-view auBp, build 4d0c78085232):
//
//   - LoadOlderMessage (uy2e): N is the root's reply_count less the
//     replies the view has (the loaded but hidden ones count again), at
//     most 100; no row at 0, and none once the thread's start is reached
//     with nothing hidden (get_rows_and_keys' load_older condition).
//     The row (.loadOlderMessages__AsWvB): 32px, 20px in, the text in
//     content-hgl-1 on base-pry with the card's side lines, the
//     base-pry-hover fill while hovered; "Loading more replies" spinner
//     after the text while a fetch runs.
//   - A click or Enter (onItemClick → onLoadOlderRepliesClick →
//     loadOlderReplies) asks conversations.replies for the 100 replies up
//     to the oldest one shown (requestReplies {latest, limit: 100};
//     core.OlderReplies) and shows them above the others, in the card. It
//     doesn't mark the thread read or open it. A failed fetch is only
//     logged in Slack; Clack does the same (nothing shows).
//
// Not ported: the keyboard focus moving to the oldest shown reply after
// the load (focusOnKey on the minimum visible ts).

const thrOlderLimit = 100

// thrOlder is a finished fetch.
type thrOlder struct {
	latest string // the request's latest: drop answers for an older state
	folds  uint64 // the card's fold stamp when it was asked for (a fold since drops it)
	msgs   []core.Message
	more   bool
	err    error
	from   uint64 // the message change it went out at (threads_live.go)
	hidden int    // the replies the card held but hid when it was asked for
}

// thrAllReplies is getDefaultMinimumVisibleTs' "0000000000.000000": every
// reply the card holds shows.
const thrAllReplies = "0000000000.000000"

// thrKnown is every reply the card holds, oldest first (Slack's timestamps
// in the messages store): the loaded older ones, then getView's latest
// ones (with those a refresh carried over) and the gaps filled among them.
func thrKnown(t *thrItem, c *thrCard) []core.Message {
	if c == nil || len(c.older) == 0 && len(c.mid) == 0 {
		return t.Replies
	}
	out := make([]core.Message, 0, len(c.older)+len(c.mid)+len(t.Replies))
	out = append(out, c.older...)
	return append(out, thrMerge(c.mid, t.Replies)...)
}

// thrMinVisible is getDefaultMinimumVisibleTs (iPpN) over the replies a
// card holds (known, oldest first) and the thread's last read: every reply
// for a thread of four or fewer (reply_count), and for one whose start the
// card holds (reachedStart) with at most two read; the two newest when
// every reply is read; else from the newest read reply; "" (every reply,
// picked again in the next frame) when none is read.
func thrMinVisible(replyCount int, known []core.Message, lastRead string, reachedStart bool) string {
	if replyCount <= 4 {
		return thrAllReplies
	}
	var read []string
	for i := range known {
		if !actTSLess(lastRead, known[i].TS) {
			read = append(read, known[i].TS)
		}
	}
	switch {
	case reachedStart && len(read) <= 2:
		return thrAllReplies
	case len(read) == len(known):
		if len(read) == 0 {
			return ""
		}
		return read[max(0, len(read)-2)]
	case len(read) == 0:
		return ""
	}
	return read[len(read)-1]
}

// minVisibles is getDerivedStateFromProps' minimumVisibleTsByThread: each
// card's first shown reply, picked once per refreshId (the data's load)
// and again while it is "" (none read yet), so a thread read since shows
// its two newest. A card not made yet gets its own when it is (cardOf).
func (v *Threads) minVisibles(page thrData) {
	for i := range page.items {
		if c := v.cards[page.items[i].key]; c != nil {
			v.minVisible(&page.items[i], c, page.load)
		}
	}
}

// minVisible picks card c's first shown reply for the data's load.
func (v *Threads) minVisible(t *thrItem, c *thrCard, load int) {
	if c.minVisLoad == load && c.minVis != "" {
		return
	}
	known := thrKnown(t, c)
	mv := thrMinVisible(t.Root.ReplyCount, known, v.lastRead(t), c.olderStart || len(known) >= t.Root.ReplyCount)
	if mv != c.minVis || c.minVisLoad != load {
		c.setMinVis(mv)
	}
	c.minVisLoad = load
}

// setMinVis moves the card's first shown reply.
func (c *thrCard) setMinVis(ts string) {
	c.minVis, c.shown, c.built = ts, nil, false
}

// thrHidden is the replies the card holds before its first shown one.
func thrHidden(t *thrItem, c *thrCard) []core.Message {
	if c == nil || c.minVis == "" || c.minVis == thrAllReplies {
		return nil
	}
	known := thrKnown(t, c)
	n := 0
	for n < len(known) && actTSLess(known[n].TS, c.minVis) {
		n++
	}
	return known[:n]
}

// thrShown is the replies the card shows, oldest first: those it holds
// (thrKnown) from its first shown one on (get_rows_and_keys draws every
// timestamp from minimumVisibleTsByThread's). The slice is the card's
// until any of them changes, so its copies are the same from frame to
// frame (what a row's layout and a plugin's view are kept by).
func thrShown(t *thrItem, c *thrCard) []core.Message {
	all := c == nil || c.minVis == "" || c.minVis == thrAllReplies
	if c == nil || all && len(c.older) == 0 && len(c.mid) == 0 {
		return t.Replies
	}
	if c.shown != nil && thrSameMsgs(c.shownOf, t.Replies) {
		return c.shown
	}
	out := thrKnown(t, c)
	if !all {
		i := 0
		for i < len(out) && actTSLess(out[i].TS, c.minVis) {
			i++
		}
		out = out[i:]
	}
	c.shown, c.shownOf = out, t.Replies
	return c.shown
}

// thrMerge is a and b in ts order, each ts once (b's copy).
func thrMerge(a, b []core.Message) []core.Message {
	if len(a) == 0 {
		return b
	}
	out := make([]core.Message, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case j == len(b) || i < len(a) && actTSLess(a[i].TS, b[j].TS):
			out = append(out, a[i])
			i++
		case i == len(a) || actTSLess(b[j].TS, a[i].TS):
			out = append(out, b[j])
			j++
		default:
			out = append(out, b[j])
			i++
			j++
		}
	}
	return out
}

// thrSameMsgs reports whether a and b are the same slice.
func thrSameMsgs(a, b []core.Message) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}

// thrMoreText is LoadOlderMessage's label ("" for no row).
func thrMoreText(t *thrItem, c *thrCard) string {
	if c != nil && c.olderStart && len(thrHidden(t, c)) == 0 {
		return "" // get_rows_and_keys: the start reached and nothing hidden
	}
	n := t.Root.ReplyCount - len(thrShown(t, c))
	n = min(max(n, 0), thrOlderLimit)
	if n == 0 {
		return ""
	}
	return nsThreads.T("Show {showPrevCount, plural, =1 {# more reply} other {# more replies}}", "showPrevCount", n)
}

// loadOlder is loadOlderReplies: the replies the card holds but hides show
// first, a hundred at a time, or all once its start is in hand
// (reachedStart); else the older ones are fetched before the oldest it
// holds, and up to a hundred show with the hidden ones (applyOlder).
func (v *Threads) loadOlder(t *thrItem, c *thrCard) {
	app := v.h.Core()
	if c.olderBusy || app == nil {
		return
	}
	hidden := thrHidden(t, c)
	if len(hidden) >= thrOlderLimit {
		c.setMinVis(hidden[len(hidden)-thrOlderLimit].TS)
		c.moreText = thrMoreText(t, c)
		return
	}
	if c.olderStart {
		c.setMinVis(thrAllReplies)
		c.moreText = thrMoreText(t, c)
		return
	}
	latest := ""
	if known := thrKnown(t, c); len(known) > 0 {
		latest = known[0].TS
	}
	c.olderBusy = true
	ch, root, h, folds, from := t.Channel, t.Root.TS, v.h, c.folds, app.MessageChangeSeq()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		msgs, more, err := app.OlderReplies(ctx, ch, root, latest, thrOlderLimit)
		c.olderRes.Store(&thrOlder{latest: latest, folds: folds, msgs: msgs, more: more, err: err, from: from, hidden: len(hidden)})
		h.Invalidate()
	}()
}

// applyOlder takes a finished fetch in (UI goroutine): the replies older
// than the oldest shown go above it, the newest 100 of them. One asked for
// before the card folded (a new refreshId, a refresh that couldn't join
// its replies) lands nowhere: it was for replies the card no longer shows.
func (v *Threads) applyOlder(t *thrItem, c *thrCard) {
	r := c.olderRes.Swap(nil)
	if r == nil {
		return
	}
	c.olderBusy = false
	if r.err != nil || r.folds != c.folds {
		return
	}
	have := map[string]bool{}
	for _, m := range thrKnown(t, c) {
		have[m.TS] = true
	}
	var add []core.Message
	for _, m := range r.msgs {
		if !have[m.TS] && (r.latest == "" || actTSLess(m.TS, r.latest)) {
			have[m.TS] = true
			add = append(add, m)
		}
	}
	if len(add) > thrOlderLimit {
		add = add[len(add)-thrOlderLimit:]
	}
	c.older, c.shown = append(add, c.older...), nil
	// what changed after the request went out reaches them in the next
	// frame (threads_live.go landLive)
	c.olderSeq = min(c.olderSeq, r.from)
	if len(c.older) == len(add) {
		c.olderSeq = r.from
	}
	v.lv.items = nil
	if !r.more || len(add) == 0 {
		c.olderStart = true // reachedStart
	}
	// the newest of the fetched ones, with the hidden ones a hundred in
	// all, show: the first shown is the oldest of them ("" with none,
	// picked again)
	mv := ""
	if keep := thrOlderLimit - r.hidden; keep > 0 && len(add) > 0 {
		mv = add[max(0, len(add)-keep)].TS
	}
	c.setMinVis(mv)
	c.moreText = thrMoreText(t, c)
	// onLoadOlderRepliesClick, once loadOlderReplies is done:
	// focusOnKey(the key holding the minimum visible ts it started from),
	// the reply that was the oldest shown. A click or Enter both get here;
	// the ring stays as the modality left it (no ring after a click).
	if r.latest != "" {
		ring := v.kb.ring
		if v.kb.focusOn(t.key + "-" + r.latest) {
			v.kb.ring = ring
		}
	}
}

// olderRow draws the load_older row.
func (v *Threads) olderRow(gtx C, c *thrCard) D {
	w := gtx.Constraints.Max.X
	h := gtx.Dp(32)
	gtx.Constraints = layout.Exact(image.Pt(w, h))
	return c.more.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		if c.more.Hovered() {
			fill(gtx, image.Rect(0, 0, w, h), v.p.Hover)
		}
		return layout.Inset{Left: 20}.Layout(gtx, func(gtx C) D {
			return layout.W.Layout(gtx, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D { return v.para(gtx, 15, 32, wReg, v.p.Link, 1, c.moreText) }),
					layout.Rigid(func(gtx C) D {
						if !c.olderBusy {
							return D{}
						}
						return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D { return actSpinner(gtx, gtx.Dp(20), v.p.Muted) })
					}),
				)
			})
		})
	})
}
