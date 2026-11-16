package composer

import (
	"image"
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
// later. Editors not drawn for keepFor and keepFrames frames are let go
// (closed composers).
//
// The hold bridges glitch frames, not navigation: an editor taken off the
// screen on purpose (another channel or page, a closed or replaced thread
// pane) is let go at once with Release. Held for 2 s, as before, it took
// what was typed meanwhile (the focus filter matches EditEvent,
// SnippetEvent and SelectionEvent), replayed it into whichever box drew
// that editor next, and then left nothing focused (Karim's log: 3078 held
// frames, most after a switch to Activity or another thread). keepFrames
// keeps a glitch of a few slow frames bridged where 250 ms is fewer than
// that (a plugin's tree on a slow machine, CI's software renderer).

const (
	keepFor    = 250 * time.Millisecond
	keepFrames = 4
)

var kept struct {
	sync.Mutex
	at      map[*Editor]time.Time // last live draw (input enabled)
	missed  map[*Editor]int       // frames since then
	release map[*Editor]bool      // Release: let go at the end of the frame
}

// Release lets go of ed's focus for good: its composer left the screen on
// purpose. What KeepFocus read for ed is dropped, and at the end of the
// frame (the next one, outside a frame) KeepFocus stops holding it and
// the focus goes, with nothing focused in its place, if ed still has it
// and wasn't drawn in that frame after all. Slack moves the focus to the
// view that took its place; whatever did (focusAfterNavigate,
// CloseSecondary) asks for it on its own. UI goroutine only.
func Release(ed *Editor) {
	if ed == nil {
		return
	}
	ed.missed = nil
	kept.Lock()
	if kept.release == nil {
		kept.release = map[*Editor]bool{}
	}
	kept.release[ed] = true
	kept.Unlock()
}

// DrawnLive reports whether ed was drawn in this frame's live layout (a
// measure pass doesn't count). Call it after the frame's layout.
func DrawnLive(gtx layout.Context, ed *Editor) bool {
	kept.Lock()
	defer kept.Unlock()
	at, ok := kept.at[ed]
	return ok && at.Equal(gtx.Now)
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
	delete(kept.missed, e)
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
	rel := kept.release
	kept.release = nil
	var keep []*Editor
	if kept.missed == nil {
		kept.missed = map[*Editor]int{}
	}
	for e, at := range kept.at {
		if at.Equal(gtx.Now) {
			// drawn this frame; released, it is on screen after all (the
			// same conversation in another pane) and keeps the focus
			delete(rel, e)
			continue
		}
		kept.missed[e]++
		if rel[e] || gtx.Now.Sub(at) > keepFor && kept.missed[e] > keepFrames || gtx.Now.Before(at) {
			delete(kept.at, e)
			delete(kept.missed, e)
			// the hold ended without the editor back: what it read
			// meanwhile was typed into a box nobody saw, and goes with it
			// (replayed, it came out wherever that editor was drawn next)
			e.missed = nil
			continue
		}
		keep = append(keep, e)
	}
	kept.Unlock()
	for e := range rel {
		e.missed = nil
		if gtx.Focused(e) {
			gtx.Execute(key.FocusCmd{}) // no tag: nothing focused
		}
	}
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
	if e.CmdKCoachmark && e.anchor != e.caret {
		return gtx.Event(e.filtersSel...)
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
	n  uint64
	at image.Point // where the last one was (NotePointer's position)
}

// NotePress records a pointer press anywhere in the window.
func NotePress() {
	pressEpoch.Lock()
	pressEpoch.n++
	pressEpoch.at = winPointer
	pressEpoch.Unlock()
}

// lastPress is the last press's window position (the footer overflow's
// outside-mousedown test, overflow.go).
func lastPress() image.Point {
	pressEpoch.Lock()
	defer pressEpoch.Unlock()
	return pressEpoch.at
}

func pressCount() uint64 {
	pressEpoch.Lock()
	defer pressEpoch.Unlock()
	return pressEpoch.n
}
