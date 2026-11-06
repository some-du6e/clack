package popups

import (
	"context"
	"image"
	"sync"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
)

// ---- outside-click scrim and click blocking ----

// scrim is a full-window input catcher drawn under a popup card. It reports
// presses (outside clicks) and swallows wheel scrolling so the shell below
// does not move while a popup is open.
type scrim struct{ tag int }

// Layout registers the scrim over the whole window and returns true when it
// was pressed since the last frame.
func (s *scrim) Layout(gtx layout.Context) (pressed bool) {
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &s.tag, Kinds: pointer.Press | pointer.Scroll,
			ScrollY: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20}, ScrollX: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20}})
		if !ok {
			break
		}
		if pe, ok := e.(pointer.Event); ok && pe.Kind == pointer.Press {
			pressed = true
		}
	}
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &s.tag)
	return pressed
}

// blocker makes a card area opaque to input so clicks on its empty parts do
// not reach the scrim below.
type blocker struct{ tag int }

func (b *blocker) Layout(gtx layout.Context, r image.Rectangle) {
	for {
		_, ok := gtx.Event(pointer.Filter{Target: &b.tag, Kinds: pointer.Press | pointer.Release | pointer.Scroll,
			ScrollY: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20}})
		if !ok {
			break
		}
	}
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &b.tag)
}

// keyTarget makes tag focusable (so key.Filter{Focus: tag} works) and
// registers it over r.
func keyTarget(gtx layout.Context, tag event.Tag, r image.Rectangle) {
	for {
		if _, ok := gtx.Event(key.FocusFilter{Target: tag}); !ok {
			break
		}
	}
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, tag)
}

// escape drains Escape presses for tag (and any focus) and reports them.
func escape(gtx layout.Context, tag event.Tag) bool {
	hit := false
	for {
		e, ok := gtx.Event(key.Filter{Focus: tag, Name: key.NameEscape})
		if !ok {
			break
		}
		if ke, ok := e.(key.Event); ok && ke.State == key.Press {
			hit = true
		}
	}
	return hit
}

// ---- placement ----

// Side says where a popup prefers to open relative to its anchor.
type Side uint8

const (
	Below Side = iota
	Above
	Right
	Left
)

// place positions a box of size sz next to anchor inside a window of size
// win (keeping margin m from the edges). It prefers side and flips to the
// opposite side when there is no room, then clamps.
func place(anchor image.Rectangle, sz, win image.Point, m int, side Side) image.Point {
	var p image.Point
	switch side {
	case Below, Above:
		p.X = anchor.Min.X
		below := anchor.Max.Y
		above := anchor.Min.Y - sz.Y
		if side == Below {
			p.Y = below
			if below+sz.Y > win.Y-m && above >= m {
				p.Y = above
			}
		} else {
			p.Y = above
			if above < m && below+sz.Y <= win.Y-m {
				p.Y = below
			}
		}
	case Right, Left:
		p.Y = anchor.Min.Y
		right := anchor.Max.X
		left := anchor.Min.X - sz.X
		if side == Right {
			p.X = right
			if right+sz.X > win.X-m && left >= m {
				p.X = left
			}
		} else {
			p.X = left
			if left < m && right+sz.X <= win.X-m {
				p.X = right
			}
		}
	}
	return clampPt(p, sz, win, m)
}

func clampPt(p, sz, win image.Point, m int) image.Point {
	if p.X+sz.X > win.X-m {
		p.X = win.X - m - sz.X
	}
	if p.Y+sz.Y > win.Y-m {
		p.Y = win.Y - m - sz.Y
	}
	if p.X < m {
		p.X = m
	}
	if p.Y < m {
		p.Y = m
	}
	return p
}

// pointRect is a zero-size rectangle at p.
func pointRect(p image.Point) image.Rectangle { return image.Rectangle{Min: p, Max: p} }

// ---- async work ----
//
// Core calls run on goroutines. Their results are applied on the UI
// goroutine: done callbacks are queued and drained by every popups Layout,
// and while work is in flight a tiny invisible "pump" overlay is kept on the
// host's stack so results still apply after the popup that started them
// has closed (e.g. a menu action that toasts when it finishes).

var (
	asyncMu    sync.Mutex
	asyncQueue []func()
	inflight   int
	pumpLive   = map[Host]*pump{}
)

// post queues f to run on the UI goroutine.
func post(h Host, f func()) {
	asyncMu.Lock()
	asyncQueue = append(asyncQueue, f)
	asyncMu.Unlock()
	if h != nil {
		h.Invalidate()
	}
}

// drain runs queued UI callbacks. Called from every Layout in this package.
func drain() {
	asyncMu.Lock()
	q := asyncQueue
	asyncQueue = nil
	asyncMu.Unlock()
	for _, f := range q {
		f()
	}
}

// Pump runs queued popups callbacks; the ui root may call it once per frame
// (optional: popups also keeps a pump overlay alive while work is pending).
func Pump() { drain() }

// run executes work on a goroutine (15 s timeout) and then done on the UI
// goroutine. Must be called on the UI goroutine.
func run[T any](h Host, work func(ctx context.Context) (T, error), done func(T, error)) {
	asyncMu.Lock()
	inflight++
	p := pumpLive[h]
	needPump := p == nil
	if needPump {
		p = &pump{h: h}
		pumpLive[h] = p
	}
	asyncMu.Unlock()
	if needPump && h != nil {
		h.ShowOverlay(p)
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		v, err := work(ctx)
		cancel()
		post(h, func() {
			asyncMu.Lock()
			inflight--
			asyncMu.Unlock()
			if done != nil {
				done(v, err)
			}
		})
	}()
}

// do runs a core write and toasts errors (and okMsg on success, if set).
func do(h Host, okMsg string, work func(ctx context.Context) error) {
	run(h, func(ctx context.Context) (struct{}, error) { return struct{}{}, work(ctx) }, func(_ struct{}, err error) {
		if err != nil {
			h.Toast("Couldn't do that: " + errText(err))
			return
		}
		if okMsg != "" {
			h.Toast(okMsg)
		}
	})
}

func errText(err error) string {
	s := err.Error()
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return s
}

// pump is an invisible overlay that drains the async queue.
type pump struct {
	h    Host
	done bool
}

func (p *pump) Layout(gtx layout.Context) layout.Dimensions {
	drain()
	asyncMu.Lock()
	idle := inflight == 0 && len(asyncQueue) == 0
	if idle {
		p.done = true
		if pumpLive[p.h] == p {
			delete(pumpLive, p.h)
		}
	}
	asyncMu.Unlock()
	return layout.Dimensions{}
}

func (p *pump) Dismissed() bool { return p.done }

// Dismiss is what the root calls on Escape when the (invisible) pump is the
// topmost overlay. Escape is meant for the popup the user sees, so forward
// it to the topmost visible popups overlay, then re-arm a fresh pump if work
// is still pending (the root drops this one from its stack either way).
func (p *pump) Dismiss() {
	if p.done {
		return
	}
	p.done = true
	for i := len(stackPrev) - 1; i >= 0; i-- {
		o, ok := stackPrev[i].(interface {
			Dismissed() bool
			Dismiss()
		})
		if ok && !o.Dismissed() {
			o.Dismiss()
			break
		}
	}
	asyncMu.Lock()
	if pumpLive[p.h] == p {
		delete(pumpLive, p.h)
	}
	pending := inflight > 0 || len(asyncQueue) > 0
	var np *pump
	if pending && p.h != nil {
		np = &pump{h: p.h}
		pumpLive[p.h] = np
	}
	asyncMu.Unlock()
	if np != nil {
		p.h.ShowOverlay(np)
	}
}

// ---- stacking order ----
//
// Several overlays may be open (an emoji picker over a modal). Only the
// topmost one should react to Escape. Overlays call stacked(gtx, o) at the
// start of Layout; the last one laid out in the previous frame is on top.

var (
	stackFrame time.Time
	stackCur   []any
	stackPrev  []any
)

func stacked(gtx layout.Context, o any) {
	if !gtx.Now.Equal(stackFrame) {
		stackFrame = gtx.Now
		stackPrev, stackCur = stackCur, stackPrev[:0]
	}
	stackCur = append(stackCur, o)
}

// isTop reports whether o was the topmost popups overlay last frame (true
// when nothing was recorded yet).
func isTop(o any) bool {
	if len(stackPrev) == 0 {
		return true
	}
	return stackPrev[len(stackPrev)-1] == o
}
