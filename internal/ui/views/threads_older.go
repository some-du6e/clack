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
	msgs   []core.Message
	more   bool
	err    error
}

// thrShown is the replies the card shows: the loaded older ones, then the
// latest ones subscriptions.thread.getView sent.
func thrShown(t *thrItem, c *thrCard) []core.Message {
	if c == nil || len(c.older) == 0 {
		return t.Replies
	}
	out := make([]core.Message, 0, len(c.older)+len(t.Replies))
	out = append(out, c.older...)
	return append(out, t.Replies...)
}

// thrMoreText is LoadOlderMessage's label ("" for no row).
func thrMoreText(t *thrItem, c *thrCard) string {
	if c != nil && c.olderStart {
		return ""
	}
	n := t.Root.ReplyCount - len(thrShown(t, c))
	n = min(max(n, 0), thrOlderLimit)
	if n == 0 {
		return ""
	}
	return nsThreads.T("Show {showPrevCount, plural, =1 {# more reply} other {# more replies}}", "showPrevCount", n)
}

// loadOlder is loadOlderReplies.
func (v *Threads) loadOlder(t *thrItem, c *thrCard) {
	app := v.h.Core()
	if c.olderBusy || app == nil {
		return
	}
	latest := ""
	if sh := thrShown(t, c); len(sh) > 0 {
		latest = sh[0].TS
	}
	c.olderBusy = true
	ch, root, h := t.Channel, t.Root.TS, v.h
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		msgs, more, err := app.OlderReplies(ctx, ch, root, latest, thrOlderLimit)
		c.olderRes.Store(&thrOlder{latest: latest, msgs: msgs, more: more, err: err})
		h.Invalidate()
	}()
}

// applyOlder takes a finished fetch in (UI goroutine): the replies older
// than the oldest shown go above it, the newest 100 of them.
func (v *Threads) applyOlder(t *thrItem, c *thrCard) {
	r := c.olderRes.Swap(nil)
	if r == nil {
		return
	}
	c.olderBusy = false
	if r.err != nil {
		return
	}
	have := map[string]bool{}
	for _, m := range thrShown(t, c) {
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
	c.older = append(add, c.older...)
	if !r.more || len(add) == 0 {
		c.olderStart = true // reachedStart
	}
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
