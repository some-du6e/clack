package core

// Saving a canvas the way Slack's canvas editor does (docs/CANVASES.md
// "edit-document", "Failures and what the user sees"): edits wait as
// pending (Slack's dirty sections) until a save of them succeeds; each
// attempt is a fresh bundle of the canvas's model (quip.Doc.Bundle), held
// to what Slack's client would send (quip.Doc.Validate), posted to
// edit-document with getSavePayloadAndOptions' form. A failure is sorted
// by i40t.getSaveErrorInfo_, counted toward the error state, and retried
// after getSaveBackoffDelay; the error state shows QuipEmbed's banner and
// turns the canvas read-only (cant_sync) until a save succeeds.

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/quip"
	"github.com/some-du6e/clack/internal/slack"
)

// CanvasSaveState is a canvas's saving as its view shows it.
type CanvasSaveState struct {
	Pending bool // edits not saved yet
	Saving  bool // a save is out
	// Reason is the error the editor shows (Slack's setError reasons):
	// "Server not reachable", "Connection offline", "Document too large",
	// "Thread closed", "Early exit due to previous failures"; "" for none.
	Reason string
	// Banner is QuipEmbed's message over the canvas: "quipOffline",
	// "quipDocumentTooLarge", "unresolved" (edits unsaved for over two
	// minutes), or "".
	Banner string
	// ReadOnly is the error state's lock (Slack's cant_sync: the
	// platform's disableEditingDuringErrorState): no editing until a save
	// succeeds.
	ReadOnly bool
}

// Save timings, Slack's: backgroundMaxDelay, the backoff's cap, a stuck
// save (150 deferrals of 200 ms), the dirty warning (two minutes, polled
// every minute: useEmbedDirtyWarning), and how long a 403's error shows.
var (
	saveBackoffBase  = 10 * time.Second
	saveBackoffMax   = 10 * time.Minute
	saveStuckAfter   = 30 * time.Second
	unsavedWarnAfter = 2 * time.Minute
	unsavedPoll      = time.Minute
	forbiddenShows   = 15 * time.Second
)

// saveError is getSaveErrorInfo_'s {type, threshold}.
type saveError struct{ typ, threshold int }

// Save error types (i40t's L).
const (
	saveErrUnknown         = 0
	saveErrForbidden       = 1
	saveErrNotPermitted    = 2
	saveErrUnavailable     = 3
	saveErrTooLarge        = 4
	saveErrOutOfOrder      = 5
	saveErrTooManyReqs     = 6
	saveIDDiffThreshold    = 25
	maxFormForKeepalive    = 64 << 10 // zRL+.Su: a save on unload goes out under 64 KiB
	saveRetryWhileInFlight = 200 * time.Millisecond
)

// saveErrorInfo is getSaveErrorInfo_: the error type and the failures in a
// row before the error state. Anything without an HTTP status (a network
// failure, an {"ok":false} answer) is UNKNOWN.
func saveErrorInfo(err error) saveError {
	var he *slack.HTTPError
	if !errors.As(err, &he) {
		return saveError{saveErrUnknown, 2}
	}
	switch he.Status {
	case http.StatusBadRequest:
		return saveError{saveErrNotPermitted, 2}
	case http.StatusForbidden:
		return saveError{saveErrForbidden, 2}
	case http.StatusConflict:
		return saveError{saveErrOutOfOrder, 9}
	case http.StatusRequestEntityTooLarge:
		return saveError{saveErrTooLarge, 2}
	case http.StatusTooManyRequests:
		return saveError{saveErrTooManyReqs, 5}
	case http.StatusServiceUnavailable:
		return saveError{saveErrUnavailable, 2}
	}
	return saveError{saveErrUnknown, 2}
}

// saveBackoff is getSaveBackoffDelay for n failures in a row.
func saveBackoff(n int) time.Duration {
	if n == 0 {
		return 0
	}
	if n <= 5 {
		return saveBackoffBase
	}
	d := float64(saveBackoffBase)
	for i := 5; i < n; i++ {
		d *= 1.5
		if d >= float64(saveBackoffMax) {
			return saveBackoffMax
		}
	}
	return time.Duration(d)
}

// canvasSaver is one canvas's saving.
type canvasSaver struct {
	mu      sync.Mutex
	pending []quip.Edit // by section, one edit each (Slack's dirty sections: mergeEdit)
	// waiting are edits made while the canvas's model couldn't be loaded:
	// each is built against the model once a load succeeds, in order
	waiting []saveBuild
	// flight is the attempt in flight's edits; after, by section, what was
	// edited of one of them meanwhile (the fields dirty since), which is
	// what's left of it once that save succeeds
	flight    []quip.Edit
	after     map[string]quip.Edit
	notices   []CanvasSaveNotice // for the view: edits no caller heard were dropped
	since     time.Time          // when the oldest pending edit came
	errors    []saveError        // the failures in a row (errorState_.errors_)
	idDiff    int                // saves attempted since the last success
	inFlight  bool
	again     bool
	started   time.Time // the attempt in flight's start
	forbidden time.Time // a 403's error, shown until forbiddenShows after
	reason    string
	retry     *time.Timer
	poll      *time.Timer
}

var saverStates = newPerApp() // *App -> *saverState

type saverState struct {
	mu       sync.Mutex
	byID     map[string]*canvasSaver
	counters *quip.Counters
}

func (a *App) savers() *saverState {
	if v, ok := saverStates.Load(a); ok {
		return v.(*saverState)
	}
	v, _ := saverStates.LoadOrStore(a, &saverState{byID: map[string]*canvasSaver{}, counters: quip.NewCounters()})
	return v.(*saverState)
}

func (a *App) saver(id string) *canvasSaver {
	st := a.savers()
	st.mu.Lock()
	defer st.mu.Unlock()
	s := st.byID[id]
	if s == nil {
		s = &canvasSaver{}
		st.byID[id] = s
	}
	return s
}

// CanvasSaveState is the canvas's saving now.
func (a *App) CanvasSaveState(id string) CanvasSaveState {
	st := a.savers()
	st.mu.Lock()
	s := st.byID[id]
	st.mu.Unlock()
	if s == nil {
		return CanvasSaveState{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stateLocked(time.Now())
}

func (s *canvasSaver) hasErrorLocked(now time.Time) bool {
	if s.idDiff > saveIDDiffThreshold || s.inFlight && now.Sub(s.started) >= saveStuckAfter {
		return true
	}
	if len(s.errors) == 0 {
		return false
	}
	min := s.errors[0].threshold
	for _, e := range s.errors {
		if e.threshold < min {
			min = e.threshold
		}
	}
	return len(s.errors) >= min
}

func (s *canvasSaver) stateLocked(now time.Time) CanvasSaveState {
	st := CanvasSaveState{Pending: len(s.pending) > 0 || len(s.waiting) > 0, Saving: s.inFlight} // waiting: edits made before the model loaded
	switch {
	case s.hasErrorLocked(now):
		st.Reason, st.ReadOnly = s.reason, true
		if st.Reason == "" {
			st.Reason = "Server not reachable"
		}
		if st.Reason == "Document too large" {
			st.Banner = "quipDocumentTooLarge"
		} else {
			st.Banner = "quipOffline"
		}
	case !s.forbidden.IsZero() && now.Sub(s.forbidden) < forbiddenShows:
		st.Reason, st.Banner = "Thread closed", "quipOffline"
	case st.Pending && now.Sub(s.since) >= unsavedWarnAfter:
		st.Banner = "unresolved"
	}
	return st
}

// ErrCanvasReadOnly is an edit made while the canvas's error state locks
// editing.
var ErrCanvasReadOnly = errors.New("core: the canvas can't take edits until a save succeeds")

// ErrCanvasConflict is an edit of a section someone else saved since the
// edit was made (or deleted): it isn't sent, since Clack can't merge the
// two the way Slack's editor does (mergeRtml), and it leaves the queue.
var ErrCanvasConflict = errors.New("core: someone else changed that part of the canvas since it was edited")

// ErrCanvasSavePending wraps an attempt's failure (the save, or the
// model's load before it) while its edits stay pending: they go again
// after Slack's backoff, and the banner shows Slack's error state once it
// comes.
var ErrCanvasSavePending = errors.New("core: the change waits to be saved")

// A CanvasSaveNotice is an edit a save dropped where no caller heard of it
// (an attempt after a failure, one that waited behind another save, an
// edit that waited for the model): someone else saved its section
// meanwhile (Conflict), or Clack can't save it the way Slack's client
// would (Reason).
type CanvasSaveNotice struct {
	Conflict bool
	Reason   string
}

// maxSaveNotices bounds the notices a canvas keeps for a view to take.
const maxSaveNotices = 16

// TakeCanvasSaveNotices hands over the canvas's notices, each once (the
// view says so and reloads).
func (a *App) TakeCanvasSaveNotices(id string) []CanvasSaveNotice {
	st := a.savers()
	st.mu.Lock()
	s := st.byID[id]
	st.mu.Unlock()
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.notices
	s.notices = nil
	return out
}

func (s *canvasSaver) noteLocked(n CanvasSaveNotice) {
	if len(s.notices) == maxSaveNotices {
		s.notices = s.notices[1:]
	}
	s.notices = append(s.notices, n)
}

// noticeOf is the notice for an edit err kept from being saved.
func noticeOf(err error) CanvasSaveNotice {
	if errors.Is(err, ErrCanvasConflict) {
		return CanvasSaveNotice{Conflict: true}
	}
	r := err.Error()
	if i := strings.LastIndex(r, ": "); i >= 0 {
		r = r[i+2:]
	}
	return CanvasSaveNotice{Reason: r}
}

// SaveCanvas saves edits to the canvas through edit-document, as Slack's
// editor saves its dirty sections: they wait as pending until a save of
// them succeeds (a later edit of the same section replaces an earlier
// one), and a failed save is retried after Slack's backoff. It returns
// this attempt's result: nil once saved; a quip.ErrUnsupported or
// quip.ErrRefused error for edits that can't be saved the way Slack's
// client would (they are dropped, nothing was sent); else the failure,
// with the edits still pending.
//
// An edit without a Base is made against the section as it stands: a
// pending edit of it, else the model's (an ErrCanvasConflict when the
// model has no such section).
func (a *App) SaveCanvas(ctx context.Context, id string, edits ...quip.Edit) error {
	return a.queueSave(ctx, id, func(base func(string) (sectionBase, bool)) ([]quip.Edit, error) {
		out := append([]quip.Edit(nil), edits...)
		for i := range out {
			if out[i].Base != 0 {
				continue
			}
			b, ok := base(out[i].SectionID)
			if !ok {
				return nil, fmt.Errorf("%w: section %s", ErrCanvasConflict, out[i].SectionID)
			}
			out[i].Base = b.seq
		}
		return out, nil
	})
}

// sectionBase is what an edit of a section is made against: its RTML and
// sequence as the model holds them, or as a pending edit of it leaves them.
type sectionBase struct {
	rtml string
	seq  int64
	typ  int32
	sty  int32
}

// saveBuild makes a save's edits against each section's base.
type saveBuild func(base func(string) (sectionBase, bool)) ([]quip.Edit, error)

// queueSave has build make the edits against each section's base (under
// the saver's lock, so a save finishing meanwhile can't move it), queues
// them and runs a save. The canvas's model is loaded first; an edit made
// while it can't be (offline, a failed answer) waits for it as Slack's
// waits for a save: the load is part of the attempt, counted and retried
// like a failed save, and the error is ErrCanvasSavePending.
func (a *App) queueSave(ctx context.Context, id string, build saveBuild) error {
	s := a.saver(id)
	s.mu.Lock()
	if s.hasErrorLocked(time.Now()) {
		s.mu.Unlock()
		return ErrCanvasReadOnly
	}
	s.mu.Unlock()
	lerr := a.LoadCanvasModel(slack.NoBackoff(ctx), id)
	s.mu.Lock() // the saver, then the model (never the other way)
	if lerr != nil || len(s.waiting) > 0 {
		// the edit waits behind the model (or behind edits that already
		// do, so the order holds)
		if len(s.pending) == 0 && len(s.waiting) == 0 {
			s.since = time.Now()
		}
		s.waiting = append(s.waiting, build)
		s.armPollLocked(a, id)
		if lerr != nil {
			s.failLocked(a, id, lerr)
			s.mu.Unlock()
			a.emit(Event{Type: "canvas_save", ID: id})
			return fmt.Errorf("%w: %w", ErrCanvasSavePending, lerr)
		}
		s.mu.Unlock()
		a.emit(Event{Type: "canvas_save", ID: id})
		return a.saveAttempt(ctx, id, s, true)
	}
	edits, err := build(a.saveBase(id, s))
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if len(s.pending) == 0 {
		s.since = time.Now()
	}
	s.queueLocked(edits)
	s.armPollLocked(a, id)
	s.mu.Unlock()
	a.emit(Event{Type: "canvas_save", ID: id})
	return a.saveAttempt(ctx, id, s, true)
}

// saveBase is the base edits of the canvas are made against, with the
// saver's lock held: each section as the model holds it, or as a pending
// edit of it leaves it.
func (a *App) saveBase(id string, s *canvasSaver) func(string) (sectionBase, bool) {
	return func(sectionID string) (sectionBase, bool) {
		var b sectionBase
		found := false
		a.CanvasModel(id, func(d *quip.Doc) {
			if d == nil {
				return
			}
			sec := d.Section(sectionID)
			if sec == nil || sec.Deleted != nil && *sec.Deleted {
				return
			}
			found = true
			b.rtml = quip.SectionRTML(sec)
			if sec.Sequence != nil {
				b.seq = *sec.Sequence
			}
			if sec.Type != nil {
				b.typ = *sec.Type
			}
			if sec.Style != nil {
				b.sty = *sec.Style
			}
		})
		for _, p := range s.pending {
			if found && p.SectionID == sectionID {
				b.seq = p.Base
				if p.Kind == quip.EditText {
					b.rtml = p.RTML
				}
			}
		}
		return b, found
	}
}

// buildWaitingLocked builds the edits that waited for the model, in the
// order they were made, as queueSave would have; one that can't be made
// any more (its section changed or went meanwhile) is a notice, as its
// caller was told it waits.
func (a *App) buildWaitingLocked(id string, s *canvasSaver) {
	ws := s.waiting
	s.waiting = nil
	base := a.saveBase(id, s)
	for _, b := range ws {
		edits, err := b(base)
		if err != nil {
			s.noteLocked(noticeOf(err))
			continue
		}
		s.queueLocked(edits)
	}
}

// queueLocked queues edits as Slack's dirty fields hold them: an edit of a
// section already pending joins that edit (mergeEdit), so one bundle
// section carries both; one of a section whose save is out joins it too,
// and is kept apart as what's dirty since (after), which is all that's
// left of the section once that save succeeds.
func (s *canvasSaver) queueLocked(edits []quip.Edit) {
	for _, e := range edits {
		i := slices.IndexFunc(s.pending, func(p quip.Edit) bool { return p.SectionID == e.SectionID })
		if i < 0 {
			s.pending = append(s.pending, e)
			continue
		}
		if s.inFlight && slices.ContainsFunc(s.flight, func(f quip.Edit) bool { return f.SectionID == e.SectionID }) {
			if s.after == nil {
				s.after = map[string]quip.Edit{}
			}
			if a, ok := s.after[e.SectionID]; ok {
				s.after[e.SectionID] = mergeEdit(a, e)
			} else {
				s.after[e.SectionID] = e
			}
		}
		s.pending[i] = mergeEdit(s.pending[i], e)
	}
}

// mergeEdit is edit e of a section made while p of it is pending, as
// Slack's dirty fields hold the two: a tick and typing are one edit with
// the text and the box (toUpdateBundle sends content and attributes in
// one section); a later box or text replaces the earlier one, keeping the
// other; a delete is the latest edit.
func mergeEdit(p, e quip.Edit) quip.Edit {
	switch {
	case e.Kind == quip.EditDelete || p.Kind == quip.EditDelete:
		return e
	case e.Kind == quip.EditCheck && p.Kind == quip.EditText:
		p.Recheck, p.Checked = true, e.Checked
		return p
	case e.Kind == quip.EditText && !e.Recheck && (p.Kind == quip.EditCheck || p.Recheck):
		e.Recheck, e.Checked = true, p.Checked
		return e
	}
	return e
}

// armPollLocked starts the dirty warning's poll while edits are pending.
func (s *canvasSaver) armPollLocked(a *App, id string) {
	if s.poll != nil {
		return
	}
	s.poll = time.AfterFunc(unsavedPoll, func() {
		s.mu.Lock()
		s.poll = nil
		pending := (len(s.pending) > 0 || len(s.waiting) > 0) && a.bg.Err() == nil // nothing after the app shut down
		if pending {
			s.armPollLocked(a, id)
		}
		s.mu.Unlock()
		if pending {
			a.emit(Event{Type: "canvas_save", ID: id})
		}
	})
}

// saveAttempt is i40t.save for the canvas's pending edits (after building
// those that waited for the model, which it loads first). direct is an
// attempt its caller hears of: its result is the caller's to show;
// another's edits it drops or refuses are notices (TakeCanvasSaveNotices),
// told with its canvas_save event.
func (a *App) saveAttempt(ctx context.Context, id string, s *canvasSaver, direct bool) error {
	s.mu.Lock()
	if s.inFlight {
		// shouldDeferSave_: one save at a time, the next one 200 ms later
		s.again = true
		s.mu.Unlock()
		return nil
	}
	if len(s.waiting) > 0 {
		s.mu.Unlock()
		err := a.LoadCanvasModel(slack.NoBackoff(ctx), id)
		s.mu.Lock()
		if s.inFlight { // another attempt began meanwhile: it builds them
			s.again = true
			s.mu.Unlock()
			return nil
		}
		if err != nil {
			s.failLocked(a, id, err)
			s.mu.Unlock()
			a.emit(Event{Type: "canvas_save", ID: id})
			return fmt.Errorf("%w: %w", ErrCanvasSavePending, err)
		}
		a.buildWaitingLocked(id, s)
	}
	if len(s.pending) == 0 {
		if len(s.waiting) == 0 {
			s.settleLocked()
		}
		s.mu.Unlock()
		a.emit(Event{Type: "canvas_save", ID: id})
		return nil
	}
	edits := append([]quip.Edit(nil), s.pending...)
	s.flight = edits
	s.inFlight, s.started = true, time.Now()
	s.idDiff++
	retries := len(s.errors)
	s.mu.Unlock()
	a.emit(Event{Type: "canvas_save", ID: id})

	saved, dropped, err := a.postSave(ctx, id, edits, retries)

	s.mu.Lock()
	s.inFlight = false
	again := s.again
	s.again = false
	refused := errors.Is(err, quip.ErrUnsupported) || errors.Is(err, quip.ErrRefused) || errors.Is(err, ErrCanvasConflict)
	after := s.after
	s.flight, s.after = nil, nil
	// done takes a section's edit out of the queue: what was edited of it
	// while the save was out stays when keep, on base (the saved sequence)
	done := func(id string, keep bool, base int64) {
		i := slices.IndexFunc(s.pending, func(p quip.Edit) bool { return p.SectionID == id })
		if i < 0 {
			return
		}
		if e, ok := after[id]; ok && keep {
			if base != 0 {
				e.Base = base
			}
			s.pending[i] = e
			return
		}
		s.pending = slices.Delete(s.pending, i, i+1)
	}
	// someone else saved these sections: never sent, nor what was edited
	// of them since (made on the same base)
	for _, e := range dropped {
		done(e.SectionID, false, 0)
	}
	switch {
	case err == nil:
		for _, e := range edits { // saved: what was edited of it since builds on the saved sequence
			done(e.SectionID, true, saved[e.SectionID])
		}
		s.errors, s.idDiff, s.reason, s.forbidden = nil, 0, "", time.Time{}
	case refused:
		// never sendable: out of the queue, but not what was edited since
		for _, e := range edits {
			done(e.SectionID, true, 0)
		}
	default:
		// failed: all of it stays pending, what was in flight with what
		// was edited since (merged into it), for the retry
		s.failLocked(a, id, err)
		again = false
	}
	if !direct {
		if len(dropped) > 0 || errors.Is(err, ErrCanvasConflict) {
			s.noteLocked(CanvasSaveNotice{Conflict: true})
		}
		if refused && !errors.Is(err, ErrCanvasConflict) {
			s.noteLocked(noticeOf(err))
		}
	}
	switch {
	case len(s.pending) == 0 && len(s.waiting) == 0:
		s.settleLocked() // nothing left: the failure run is over
	case err == nil || refused:
		again = true
	}
	s.mu.Unlock()
	switch {
	case err == nil && len(dropped) > 0:
		err = fmt.Errorf("%w: %d edits", ErrCanvasConflict, len(dropped))
	case err != nil && !refused:
		err = fmt.Errorf("%w: %w", ErrCanvasSavePending, err)
	}
	a.emit(Event{Type: "canvas_save", ID: id})
	if again {
		time.AfterFunc(saveRetryWhileInFlight, func() {
			if a.bg.Err() != nil {
				return
			}
			rctx, cancel := context.WithTimeout(a.bg, 60*time.Second)
			defer cancel()
			a.saveAttempt(rctx, id, s, false)
		})
	}
	return err
}

// failLocked counts a failed attempt (the save, or the model's load before
// it) as getSaveErrorInfo_ sorts it, and arms the retry after Slack's
// backoff.
func (s *canvasSaver) failLocked(a *App, id string, err error) {
	info := saveErrorInfo(err)
	log.Printf("core: canvas %s: save failed (%d in a row before it): %s", id, len(s.errors), slack.FailureDetail(err))
	if info.typ == saveErrForbidden {
		// setError(1, 7, 15000) at once, not counted, no retry
		s.forbidden = time.Now()
		return
	}
	s.errors = append(s.errors, info)
	switch {
	case info.typ == saveErrTooLarge:
		s.reason = "Document too large"
	case !a.ClientOnline(): // navigator.onLine, in Slack
		s.reason = "Connection offline"
	default:
		s.reason = "Server not reachable"
	}
	delay := saveBackoff(len(s.errors))
	if s.retry != nil {
		s.retry.Stop()
	}
	s.retry = time.AfterFunc(delay, func() {
		if a.bg.Err() != nil {
			return
		}
		rctx, cancel := context.WithTimeout(a.bg, 60*time.Second)
		defer cancel()
		a.saveAttempt(rctx, id, s, false)
	})
}

// settleLocked ends the failure run once nothing is left to save (the
// queue emptied, whatever the last attempt's result): the error state, and
// its lock, lift.
func (s *canvasSaver) settleLocked() {
	s.errors, s.idDiff, s.reason = nil, 0, ""
}

// postSave builds, checks and posts one save of edits: the model caught up
// first (fetch-latest), the edits of sections someone else saved since
// (or deleted) dropped, the bundle and title metadata from the model, the
// form as getSavePayloadAndOptions writes it, and the answer applied. It
// returns each saved section's new sequence and the dropped edits.
func (a *App) postSave(ctx context.Context, id string, edits []quip.Edit, retries int) (map[string]int64, []quip.Edit, error) {
	q := a.quipCanvasFor(id)
	q.net.Lock() // no load or fetch between the bundle and its answer
	defer q.net.Unlock()
	if err := a.loadModelLocked(ctx, id, q); err != nil {
		return nil, nil, err
	}
	if err := a.fetchModelLocked(slack.NoBackoff(ctx), id, q); err != nil {
		// never on stale state: how the server takes a stale section
		// sequence isn't known, so the attempt fails and goes again
		return nil, nil, fmt.Errorf("core: catching the canvas up before a save: %w", err)
	}
	q.mu.Lock()
	d, thread := q.doc, q.thread
	if d == nil {
		q.mu.Unlock()
		return nil, nil, errors.New("core: no canvas model")
	}
	var keep, dropped []quip.Edit
	landed := map[string]int64{} // edits the model holds already, by section: their sequence
	for _, e := range edits {
		sec := d.Section(e.SectionID)
		switch {
		case sec == nil || sec.Deleted != nil && *sec.Deleted, sec.Sequence == nil || *sec.Sequence != e.Base:
			if holdsEdit(sec, e) {
				// the section holds the edit: this client's own save, its
				// answer lost (a timeout, a cut connection), not someone
				// else's
				landed[e.SectionID] = *sec.Sequence
				continue
			}
			// Slack's editor would merge someone else's save into the
			// edit (mergeRtml); Clack can't, so it never writes over one
			dropped = append(dropped, e)
		default:
			keep = append(keep, e)
		}
	}
	if len(keep) == 0 {
		q.mu.Unlock()
		if len(landed) > 0 {
			return landed, dropped, nil
		}
		return nil, dropped, fmt.Errorf("%w: %d edits", ErrCanvasConflict, len(dropped))
	}
	edits = keep
	ids := make([]string, 0, len(edits))
	for _, e := range edits {
		ids = append(ids, e.SectionID)
	}
	sv := a.savers().counters.Begin(ids)
	data, meta, err := d.Bundle(edits, sv)
	if err == nil {
		err = d.Validate(data, edits, meta, sv)
	}
	docID := ""
	if d.Document.ID != nil {
		docID = *d.Document.ID
	}
	q.mu.Unlock()
	if err != nil {
		return nil, dropped, err
	}
	if docID == "" {
		return nil, dropped, fmt.Errorf("%w: the model has no document id", quip.ErrRefused)
	}
	form := []slack.CanvasField{
		{Name: "thread", Value: thread},
		{Name: "document", Value: docID},
		{Name: "retry_count", Value: strconv.Itoa(retries)},
		{Name: "session", Value: a.savers().counters.Session},
		{Name: "sequence", Value: strconv.FormatInt(sv.Sequence, 10)},
		{Name: "data_binary", Value: quip.MarshalBase64(data)},
		{Name: "title", Value: meta.Title},
	}
	b, err := a.C.CanvasCall(ctx, "edit-document", form)
	if err != nil {
		return nil, dropped, err
	}
	var ans quip.EditAnswer
	if err := quip.Unmarshal(b, &ans); err != nil {
		return nil, dropped, err
	}
	saved := landed
	q.mu.Lock()
	if ans.Changed != nil && *ans.Changed && q.doc == d { // the same model: nothing reloaded it (q.net)
		d.ApplySaved(&ans)
	}
	for _, e := range edits {
		if sec := q.doc.Section(e.SectionID); sec != nil && sec.Sequence != nil {
			saved[e.SectionID] = *sec.Sequence
		}
	}
	q.mu.Unlock()
	a.emit(Event{Type: "canvas_model", ID: id})
	if a.CanvasOpen(id) {
		a.canvasChanged(id) // open views refetch the export, as after canvases.edit
	}
	return saved, dropped, nil
}

// holdsEdit reports whether section sec holds edit e already (an answer
// lost after the server took the save): the same text, the same box, or
// deleted.
func holdsEdit(sec *quip.Section, e quip.Edit) bool {
	if sec == nil || sec.Sequence == nil {
		return false
	}
	deleted := sec.Deleted != nil && *sec.Deleted
	checked := func() bool {
		return sec.Attributes != nil && sec.Attributes.Checked != nil && *sec.Attributes.Checked == e.Checked
	}
	switch e.Kind {
	case quip.EditDelete:
		return deleted
	case quip.EditCheck:
		return !deleted && checked()
	case quip.EditText:
		return !deleted && quip.SectionRTML(sec) == e.RTML && (!e.Recheck || checked())
	}
	return false
}

// flushAllCanvasSaves makes the last attempt for every canvas with edits
// pending when Clack quits, as Slack saves once more on unload (and as a
// canvas's last view closing does).
func (a *App) flushAllCanvasSaves(ctx context.Context) {
	st := a.savers()
	st.mu.Lock()
	ids := make([]string, 0, len(st.byID))
	for id := range st.byID {
		ids = append(ids, id)
	}
	st.mu.Unlock()
	for _, id := range ids {
		a.flushCanvasSaves(ctx, id)
	}
}

// stopCanvasSavers stops the savers' timers (the retry, the dirty
// warning's poll), so a released App leaves none behind.
func (a *App) stopCanvasSavers() {
	v, ok := saverStates.Load(a)
	if !ok {
		return
	}
	st := v.(*saverState)
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, s := range st.byID {
		s.mu.Lock()
		if s.retry != nil {
			s.retry.Stop()
			s.retry = nil
		}
		if s.poll != nil {
			s.poll.Stop()
			s.poll = nil
		}
		s.mu.Unlock()
	}
}

// flushCanvasSaves makes one last attempt for a canvas whose last view
// closes, as Slack saves once more on unload.
func (a *App) flushCanvasSaves(ctx context.Context, id string) {
	st := a.savers()
	st.mu.Lock()
	s := st.byID[id]
	st.mu.Unlock()
	if s == nil {
		return
	}
	s.mu.Lock()
	pending := (len(s.pending) > 0 || len(s.waiting) > 0) && !s.inFlight
	if s.retry != nil {
		s.retry.Stop()
		s.retry = nil
	}
	s.mu.Unlock()
	if pending {
		a.saveAttempt(ctx, id, s, false)
	}
}

// The switch (docs/CANVAS-EDIT-PLAN.md "Rules for round 2"): canvas edits
// save through edit-document only when it's on. Off by default; on with
// CLACK_CANVAS_QUIP_EDIT=1 or ui.json's canvas_quip_edit (SetQuipEdit).
var quipEditEnv = os.Getenv("CLACK_CANVAS_QUIP_EDIT") == "1"

var quipEditOn = newPerApp() // *App -> bool

// QuipEditOn reports whether canvas edits save through edit-document.
func (a *App) QuipEditOn() bool {
	if v, ok := quipEditOn.Load(a); ok {
		return v.(bool)
	}
	return quipEditEnv
}

// SetQuipEdit turns saving through edit-document on or off (ui.json's
// canvas_quip_edit; the environment's switch still turns it on).
func (a *App) SetQuipEdit(on bool) { quipEditOn.Store(a, on || quipEditEnv) }

// SaveCanvasText saves a block's new plain text: the section's RTML as
// Slack's save writer makes it of typed text (quip.TextRTML). orig is the
// text the edit began from, which has to be the section's still (as the
// model holds it, or a pending edit leaves it): else someone else saved it
// meanwhile and the edit is an ErrCanvasConflict. It refuses
// (quip.ErrUnsupported) a section plain text can't carry back:
// formatting, links, mentions, emoji, dates, line breaks, comment anchors,
// code, or anything but a text section or the title.
func (a *App) SaveCanvasText(ctx context.Context, id, sectionID, orig, text string) error {
	rtml, err := quip.TextRTML(text)
	if err != nil {
		return err
	}
	return a.queueSave(ctx, id, func(base func(string) (sectionBase, bool)) ([]quip.Edit, error) {
		b, err := plainBase(base, sectionID, orig)
		if err != nil {
			return nil, err
		}
		return []quip.Edit{{Kind: quip.EditText, SectionID: sectionID, RTML: rtml, Base: b.seq}}, nil
	})
}

// SaveCanvasDelete deletes a block's section (the block emptied), checked
// against orig as SaveCanvasText checks it.
func (a *App) SaveCanvasDelete(ctx context.Context, id, sectionID, orig string) error {
	return a.queueSave(ctx, id, func(base func(string) (sectionBase, bool)) ([]quip.Edit, error) {
		b, err := plainBase(base, sectionID, orig)
		if err != nil {
			return nil, err
		}
		return []quip.Edit{{Kind: quip.EditDelete, SectionID: sectionID, Base: b.seq}}, nil
	})
}

// plainBase is the base of a plain-text edit of sectionID begun from orig.
func plainBase(base func(string) (sectionBase, bool), sectionID, orig string) (sectionBase, error) {
	b, ok := base(sectionID)
	switch {
	case !ok:
		return b, fmt.Errorf("%w: section %s isn't in the canvas", ErrCanvasConflict, sectionID)
	case b.typ != quip.TypeText && b.typ != quip.TypeTitle:
		return b, fmt.Errorf("%w: a section of type %d", quip.ErrUnsupported, b.typ)
	case b.typ == quip.TypeText && b.sty == quip.StyleCode:
		return b, fmt.Errorf("%w: a code block", quip.ErrUnsupported)
	}
	t, marks, ok := quip.ParseRTML(b.rtml)
	if !ok || len(marks) > 0 || strings.Contains(t, "<control") || strings.Contains(t, "<br>") {
		return b, fmt.Errorf("%w: the section has formatting, links, mentions or line breaks", quip.ErrUnsupported)
	}
	if plainCompare(html.UnescapeString(t)) != plainCompare(orig) {
		return b, fmt.Errorf("%w: section %s", ErrCanvasConflict, sectionID)
	}
	return b, nil
}

// plainCompare is text as two copies of a section are compared: a
// no-break space as a space (the export and RTML may write either), the
// ends trimmed.
func plainCompare(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\u00a0", " "))
}

// CanvasTitleSection is the id of the canvas's title section.
func (a *App) CanvasTitleSection(ctx context.Context, id string) (string, error) {
	if err := a.LoadCanvasModel(ctx, id); err != nil {
		return "", err
	}
	if t := a.canvasTitleID(id); t != "" {
		return t, nil
	}
	return "", fmt.Errorf("%w: the canvas's model went, or has no title section", quip.ErrUnsupported)
}

// canvasTitleID is the model's title section ("" when core holds no model,
// or it has none).
func (a *App) canvasTitleID(id string) string {
	out := ""
	a.CanvasModel(id, func(d *quip.Doc) {
		if d == nil { // the state went with the canvas's last view
			return
		}
		for _, s := range d.Sections() {
			if s.Type != nil && *s.Type == quip.TypeTitle {
				out = *s.ID
			}
		}
	})
	return out
}

// SaveCanvasTitle renames the canvas through its title section's text, as
// SaveCanvasText saves a block's (orig is the title the rename began
// from). The title section is the model's when the edit is built, so a
// rename made before the model loads waits for it as any edit does.
func (a *App) SaveCanvasTitle(ctx context.Context, id, orig, title string) error {
	rtml, err := quip.TextRTML(title)
	if err != nil {
		return err
	}
	return a.queueSave(ctx, id, func(base func(string) (sectionBase, bool)) ([]quip.Edit, error) {
		tid := a.canvasTitleID(id)
		if tid == "" {
			return nil, fmt.Errorf("%w: the canvas has no title section", quip.ErrUnsupported)
		}
		b, err := plainBase(base, tid, orig)
		if err != nil {
			return nil, err
		}
		return []quip.Edit{{Kind: quip.EditText, SectionID: tid, RTML: rtml, Base: b.seq}}, nil
	})
}

// flushCanvasesOnQuit is the canvases' last save when Clack quits: the
// pending edits of every canvas go once more, briefly, on a context of
// their own (the App's background is stopping).
func (a *App) flushCanvasesOnQuit() {
	ctx, cancel := context.WithTimeout(context.Background(), quitSaveWait)
	defer cancel()
	a.flushAllCanvasSaves(ctx)
}

// quitSaveWait bounds the last save at quit.
var quitSaveWait = 3 * time.Second
