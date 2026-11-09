package composer

import (
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op/clip"
)

// Focus across a missed frame.
//
// Gio drops key focus, silently (no FocusEvent), at the end of any frame
// in which the focused tag had no focusable handler: its widget wasn't
// drawn, or was drawn only in a discarded measure pass (io/input key.go,
// keyQueue.Frame). Under a plugin that wraps the primary view (Slick's
// Chat Tabs: ClientPrimaryView > RenderedViewContents) the conversation is
// drawn from the plugin's tree, and one frame without it left the composer
// deselected until a channel switch focused it again: "I couldn't type
// cuz I was getting deselected".
//
// KeepFocus, run once at the end of every frame, keeps a focused editor
// that wasn't drawn this frame focusable for Gio (its focus filter and a
// zero-area input op), so the focus survives. It never asks for focus:
// any real move (a click on another field, Escape, a FocusCmd) goes
// through Gio as before, and the editor reads the FocusEvent it gets
// later. Editors not drawn for keepFor are let go (closed composers).

const keepFor = 2 * time.Second

var kept struct {
	sync.Mutex
	at map[*Editor]time.Time // last live draw (input enabled)
}

// noteLive records a draw that delivers input (not a measure pass).
func (e *Editor) noteLive(gtx layout.Context) {
	if !gtx.Source.Enabled() {
		return
	}
	kept.Lock()
	if kept.at == nil {
		kept.at = map[*Editor]time.Time{}
	}
	kept.at[e] = gtx.Now
	kept.Unlock()
}

// KeepFocus keeps the focus of editors that missed this frame (see above).
// Call it once per frame, after everything was laid out, with the
// window's context.
func KeepFocus(gtx layout.Context) {
	if !gtx.Source.Enabled() {
		return
	}
	kept.Lock()
	var keep []*Editor
	for e, at := range kept.at {
		switch {
		case at.Equal(gtx.Now):
			// drawn this frame
		case gtx.Now.Sub(at) > keepFor || gtx.Now.Before(at):
			delete(kept.at, e)
		default:
			keep = append(keep, e)
		}
	}
	kept.Unlock()
	for _, e := range keep {
		if !gtx.Focused(e) {
			continue
		}
		focusStats.held.Add(1)
		for { // its focus filter makes it focusable; keep what arrives for its next draw
			ev, ok := gtx.Event(key.FocusFilter{Target: e})
			if !ok {
				break
			}
			e.missed = append(e.missed, ev)
		}
		func() {
			defer clip.Rect{}.Push(gtx.Ops).Pop()
			event.Op(gtx.Ops, e)
		}()
	}
}

// focusStats counts, for the missed-frame log line (ui: focus_log.go),
// the frames in which KeepFocus held a focused editor's focus, and the
// drops Gio made anyway that restoreDroppedFocus saw, with how many it
// gave the focus back for.
var focusStats struct{ held, dropped, restored atomic.Int64 }

// TakeFocusStats returns the counts since the last call and zeroes them.
func TakeFocusStats() (held, dropped, restored int64) {
	return focusStats.held.Swap(0), focusStats.dropped.Swap(0), focusStats.restored.Swap(0)
}

// nextKeyEvent is the editor's next key event: first what KeepFocus read
// for it while it wasn't drawn, then the router's.
func (e *Editor) nextKeyEvent(gtx layout.Context) (event.Event, bool) {
	if len(e.missed) > 0 {
		ev := e.missed[0]
		e.missed = e.missed[1:]
		return ev, true
	}
	return gtx.Event(e.filters...)
}

// ---- fallback: a drop that happened anyway ----

// pressEpoch counts pointer presses anywhere in the window (the root's
// pointer tracker calls NotePress). A drop is restored only when no press
// came since the editor was last seen focused: a click elsewhere may have
// been meant to move the focus.
var pressEpoch struct {
	sync.Mutex
	n uint64
}

// NotePress records a pointer press anywhere in the window.
func NotePress() {
	pressEpoch.Lock()
	pressEpoch.n++
	pressEpoch.Unlock()
}

func pressCount() uint64 {
	pressEpoch.Lock()
	defer pressEpoch.Unlock()
	return pressEpoch.n
}
