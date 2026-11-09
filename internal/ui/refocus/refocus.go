// Package refocus gives the key focus back, when a modal closes, to the
// widget that had it before the modal opened: Slack's useRestoreFocus
// (BaseModal, c-sk-modal) records document.activeElement when a modal
// opens and, when it closes, focuses that element again if the focus is
// then on the body or still where the modal left it.
//
// Gio has no "active element" to read, so widgets that can take the focus
// back (the composer's editor) Note themselves on every live draw; the
// focused one becomes Last. A modal takes Last when it opens and calls
// Restore when it closes; the widget asks Take at the start of its next
// draw and focuses itself if nothing else took the focus meanwhile.
//
// Everything here runs on the UI goroutine; the mutex only keeps tests
// that lay out from several goroutines honest.
package refocus

import (
	"sync"
	"time"

	"gioui.org/io/event"
	"gioui.org/layout"
)

// live is how long after its last draw a widget still counts as on screen.
const live = 2 * time.Second

var st struct {
	sync.Mutex
	last    event.Tag
	lastAt  time.Time // last's latest live draw
	pending map[event.Tag]restore
}

type restore struct {
	from []event.Tag // the closed modal's tags: focus still there counts as "nowhere"
	at   time.Time   // when it was asked (requests expire after live)
}

// Note records a live draw of tag. Call it from the widget's Layout; a
// measure pass (input disabled) is ignored.
func Note(gtx layout.Context, tag event.Tag) {
	if !gtx.Source.Enabled() {
		return
	}
	st.Lock()
	defer st.Unlock()
	if gtx.Focused(tag) {
		st.last, st.lastAt = tag, gtx.Now
	} else if tag == st.last {
		st.lastAt = gtx.Now
	}
}

// Last is the most recently focused widget that is still drawn, or nil.
func Last(now time.Time) event.Tag {
	st.Lock()
	defer st.Unlock()
	if st.last == nil || now.Sub(st.lastAt) > live || now.Before(st.lastAt) {
		return nil
	}
	return st.last
}

// Restore asks tag to take the focus back on its next draw, provided the
// focus is then nowhere or still on one of from.
func Restore(tag event.Tag, from ...event.Tag) {
	if tag == nil {
		return
	}
	st.Lock()
	defer st.Unlock()
	if st.pending == nil {
		st.pending = map[event.Tag]restore{}
	}
	st.pending[tag] = restore{from: from, at: time.Now()}
}

// Take reports whether tag should focus itself now (a Restore is pending
// and nothing else took the focus). Call it at the start of the widget's
// Layout, before it reads its events, and execute a key.FocusCmd when it
// says so.
func Take(gtx layout.Context, tag event.Tag) bool {
	if !gtx.Source.Enabled() {
		return false
	}
	st.Lock()
	r, ok := st.pending[tag]
	if ok {
		delete(st.pending, tag)
	}
	st.Unlock()
	if !ok || time.Since(r.at) > live {
		return false
	}
	if gtx.Focused(nil) || gtx.Focused(tag) {
		return !gtx.Focused(tag)
	}
	for _, f := range r.from {
		if gtx.Focused(f) {
			return true
		}
	}
	return false
}

// Pending reports whether tag has a Restore waiting (tests).
func Pending(tag event.Tag) bool {
	st.Lock()
	defer st.Unlock()
	_, ok := st.pending[tag]
	return ok
}

// Reset forgets everything (tests).
func Reset() {
	st.Lock()
	defer st.Unlock()
	st.last, st.lastAt, st.pending = nil, time.Time{}, nil
}
