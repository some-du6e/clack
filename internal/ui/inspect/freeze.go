package inspect

import (
	"time"

	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
)

// The freeze (Karim, 2026-11-17: "the dev thing should leave focus of the
// pointer on the thing to take a screenshot for stuff like hovers"). A
// capture is shot from a frame drawn after the click, and the picker
// outlines what is under the pointer now, so whatever shows only while
// the pointer is on something (a message's hover toolbar, a tooltip, a
// hover card, a button's hover colour) was gone if the pointer had to
// leave it. FreezeKey holds the window on the frame on screen:
//
//   - That frame is the session's last laid-out one. End kept its ops as
//     one call (Gio patch 31, Ops.Whole) and the window's ops' end there
//     (patch 3, Ops.Checkpoint) before the tool drew over them, and Frame
//     runs first in every frame, before the window would reset them. Each
//     frame the window draws frozen is the window's own ops rolled back to
//     that end, with the tool's drawing after them: nothing is laid out,
//     so nothing the window shows changes, whatever lands meanwhile, and
//     the hover states, tooltips, the caret's phase and the animated
//     images (held: Host.Frozen) stay as they were drawn. The tool draws
//     into the window's ops, not ops of its own calling them: Gio numbers
//     the state a Defer saves (op.Defer, ops.Save) per op.Ops, so a
//     deferred tooltip in the frame and the tool's deferred drawing in
//     ops of its own both had state 1, and the tooltip was drawn, and hit,
//     at the tool's origin instead of its own (2026-11-18).
//   - The picker outlines from that frame's record. Its areas are in the
//     ops drawn again, with their tags, so Gio's own hit test still says
//     what the pointer is over (slackc InspectFrame.Hold).
//   - A click, a Shift+click and a drag capture from it: AfterFrame shoots
//     the frozen frame.
//   - The window takes no input: no widget is laid out to read any. The
//     focused editor keeps the focus (keepFocus); what is typed meanwhile
//     goes nowhere.
//   - FreezeKey again or Esc unfreezes (Esc again ends the session), and
//     so does the session's end or a new window size. The window lays out
//     again with every widget's pointer state started over where the
//     pointer is (Host.Frozen(false), Gio patch 49's ResetPointers): no
//     widget read its events while frozen, so one the pointer left would
//     have stayed hovered.
//
// A freeze holds the one frame's ops, in the window's own buffer, and the
// tool's drawing over it (show); both go when it ends. Without a session
// Frame returns at once.

// FreezeKey freezes the window in a session, and unfreezes it.
const FreezeKey key.Name = "F"

// noteFor is how long the banner shows a message while frozen (say).
const noteFor = 3 * time.Second

// freeze is a frozen frame.
type freeze struct {
	content op.CallOp     // the frame on screen's ops, without the tool's drawing (End)
	win     *op.Ops       // the window's ops: content, then each frozen frame the tool's drawing
	cp      op.Checkpoint // win's end after content (End)
	at      frameKey      // what the frame was laid out for
	regions []Region      // the shell's areas then
	// describe fills a report's window and page fields as they were
	// (Host.Describer): the page, the pane, the overlays and the theme
	describe func(r *Report, red *Redactor)
	note     string // a message for the banner (say), until noteEnd
	noteEnd  time.Time
}

// Frozen reports that the window is held on a frame (FreezeKey).
func (t *Tool) Frozen() bool { return t.fz != nil }

// Frame starts a frame of a session. Call it first in every window frame,
// before the window's ops are reset (they hold the frame on screen, which
// the freeze key holds), and after the frame's posted work; gtx is the
// frame's context (the one the window would lay out with), its Ops the
// window's ops as the last frame left them. It reads the frame's input,
// which Begin then finds read. Frozen, it draws the frozen frame into the
// window's ops and returns them to submit for this frame, with nothing
// laid out; call AfterFrame after submitting them. Otherwise it returns
// nil and the window lays out as usual, Begin to End.
func (t *Tool) Frame(gtx layout.Context) *op.Ops {
	t.flushTexts(gtx) // a frozen frame lays nothing out: Begin doesn't run
	if t.state == off {
		t.contentOK = false
		return nil
	}
	if fz := t.fz; fz != nil && (frameKey{gtx.Constraints.Max, gtx.Metric}) != fz.at {
		t.unfreeze() // the frozen frame no longer fits the window
		t.host.Toast("Inspect: unfrozen, the window changed size")
	}
	t.input(gtx)
	fz := t.fz
	if fz == nil {
		t.contentOK = false // the window lays this frame out: the ops End kept are reset
		return nil
	}
	// the window's ops back to the frame End finished, the tool's drawing
	// after them
	if gtx.Ops != fz.win || !fz.win.Rollback(fz.cp) {
		t.unfreeze() // the window's ops were reset: the frame is gone
		t.contentOK = false
		return nil
	}
	keepFocus(gtx.Source)
	t.overlay(gtx)
	return fz.win
}

// freeze holds the frame on screen, the session inspecting: from this
// frame on the window draws it again (Frame). It reports whether it
// froze: the frame on screen must be one End finished, of the size and
// scale the window has now.
func (t *Tool) freeze(gtx layout.Context) bool {
	at := frameKey{gtx.Constraints.Max, gtx.Metric}
	if t.fz != nil || t.state != inspecting || !t.contentOK || !t.rec || t.contentAt != at {
		return false
	}
	t.fz = &freeze{content: t.content, win: t.contentOps, cp: t.contentCP, at: at, regions: t.host.Regions(), describe: t.host.Describer()}
	t.host.Frozen(true)
	t.host.Invalidate()
	return true
}

// unfreeze lets the frozen frame go: the window lays out again, every
// widget's pointer state started over (Host.Frozen).
func (t *Tool) unfreeze() {
	if t.fz == nil {
		return
	}
	t.fz = nil
	t.host.Frozen(false)
	t.host.Invalidate()
}

// keepFocus asks for the focused widget's focus events as the widget
// would (a frozen frame lays nothing out): Gio keeps the focus only on a
// tag that asked in the frame (keyQueue.Frame), so the editor would lose
// its caret, and plugins would see a blur, when the freeze ends. An event
// it takes here meanwhile (the window's own blur, text typed) goes with
// the freeze.
func keepFocus(src input.Source) {
	if f := src.FocusTag(); f != nil {
		src.Event(key.FocusFilter{Target: f})
	}
}

// NotShown says the frame End just finished didn't go to the screen (the
// window showed its boot placeholder instead): no freeze holds it.
func (t *Tool) NotShown() { t.contentOK = false }

// describe fills a report's window and page fields: as they were when
// the freeze began while frozen, as they are now otherwise.
func (t *Tool) describe(r *Report, red *Redactor) {
	if t.fz != nil {
		t.fz.describe(r, red)
		return
	}
	t.host.Describer()(r, red)
}

// Say tells the user msg about the session: in a toast ("Inspect: " and
// msg), or while frozen in the banner (say).
func (t *Tool) Say(msg string) { t.say(msg) }

// regions are the shell's areas: the frozen frame's while frozen.
func (t *Tool) regions() []Region {
	if t.fz != nil {
		return t.fz.regions
	}
	return t.host.Regions()
}

// say tells the user msg: in a toast ("Inspect: " and msg), or while
// frozen, when the window draws no toast, in the banner for noteFor.
func (t *Tool) say(msg string) {
	fz := t.fz
	if fz == nil {
		t.host.Toast("Inspect: " + msg)
		return
	}
	at := t.inAt
	if at.IsZero() {
		at = time.Now()
	}
	fz.note, fz.noteEnd = msg, at.Add(noteFor)
	t.host.Invalidate()
}

// sessionHint is the banner while inspecting: HintInspect, or frozen
// HintFrozen, or say's message while it shows.
func (t *Tool) sessionHint(gtx layout.Context) string {
	fz := t.fz
	if fz == nil {
		return HintInspect
	}
	if fz.note != "" {
		if gtx.Now.Before(fz.noteEnd) {
			gtx.Execute(op.InvalidateCmd{At: fz.noteEnd})
			return "Frozen: " + fz.note + " · F or Esc unfreezes"
		}
		fz.note = ""
	}
	return HintFrozen
}

// FrozenOps are the ops the frozen frames are drawn in (the window's),
// nil while not frozen (tests).
func (t *Tool) FrozenOps() *op.Ops {
	if t.fz == nil {
		return nil
	}
	return t.fz.win
}
