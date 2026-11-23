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

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/quip"
	"github.com/some-du6e/clack/internal/richtext"
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
	return a.queueSave(ctx, id, func(c *saveCtx) ([]quip.Edit, error) {
		base := c.base
		out := append([]quip.Edit(nil), edits...)
		for i := range out {
			if out[i].Base != 0 || out[i].Kind == quip.EditNew {
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
	// where a new section after it goes, and what initSibling copies
	pos       string
	parents   []string
	listStyle int32 // the first parent's style (a list's), when it has one
	indent    int32 // its indentation attribute, -1 when unset
	isNew     bool  // a new section not saved yet (a pending EditNew)
}

// saveCtx is what queueSave's build makes edits against: each section's
// base, and the positions and ids of the new sections not saved yet.
type saveCtx struct {
	base  func(sectionID string) (sectionBase, bool)
	taken []string
	news  []string // the new sections pending (queued or in flight)
}

// saveBuild makes a save's edits against each section's base.
type saveBuild func(c *saveCtx) ([]quip.Edit, error)

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
	edits, err := build(a.saveCtxLocked(id, s))
	if err != nil {
		s.mu.Unlock()
		return err
	}
	was := len(s.pending)
	if err := s.queueLocked(edits); err != nil {
		s.mu.Unlock()
		return err
	}
	if was == 0 {
		s.since = time.Now()
	}
	s.armPollLocked(a, id)
	s.mu.Unlock()
	a.emit(Event{Type: "canvas_save", ID: id})
	return a.saveAttempt(ctx, id, s, true)
}

// saveCtxLocked is what edits of the canvas are made against, with the
// saver's lock held: each section as the model holds it, or as a pending
// edit of it leaves it (a new section not saved yet, as its edit makes
// it), and the new sections pending.
func (a *App) saveCtxLocked(id string, s *canvasSaver) *saveCtx {
	var base func(sectionID string) (sectionBase, bool)
	base = func(sectionID string) (sectionBase, bool) {
		b := sectionBase{indent: -1}
		for _, p := range s.pending { // a new section not saved yet
			if p.Kind == quip.EditNew && p.SectionID == sectionID {
				after, ok := base(p.After)
				if !ok {
					return b, false
				}
				b = sectionBase{rtml: p.RTML, typ: quip.TypeText, sty: p.Style, pos: p.Position, parents: after.parents,
					listStyle: after.listStyle, indent: after.indent, isNew: true}
				return b, true
			}
		}
		found := false
		a.CanvasModel(id, func(d *quip.Doc) {
			if d == nil {
				return
			}
			sec := d.Section(d.RealID(sectionID))
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
			if sec.Position != nil {
				b.pos = *sec.Position
			}
			if sec.Parents != nil && len(sec.Parents.IDs) > 0 {
				b.parents = append([]string(nil), sec.Parents.IDs...)
				if p := d.Section(sec.Parents.IDs[0]); p != nil && p.Style != nil {
					b.listStyle = *p.Style
				}
			}
			if sec.Attributes != nil && sec.Attributes.Indentation != nil {
				b.indent = *sec.Attributes.Indentation
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
	sc := &saveCtx{base: base}
	for _, p := range s.pending {
		if p.Kind == quip.EditNew {
			sc.taken = append(sc.taken, p.Position)
			sc.news = append(sc.news, p.SectionID)
		}
	}
	return sc
}

// buildWaitingLocked builds the edits that waited for the model, in the
// order they were made, as queueSave would have; one that can't be made
// any more (its section changed or went meanwhile) is a notice, as its
// caller was told it waits.
func (a *App) buildWaitingLocked(id string, s *canvasSaver) {
	ws := s.waiting
	s.waiting = nil
	for _, b := range ws {
		edits, err := b(a.saveCtxLocked(id, s))
		if err == nil {
			err = s.queueLocked(edits)
		}
		if err != nil {
			s.noteLocked(noticeOf(err))
		}
	}
}

// queueLocked queues edits as Slack's dirty fields hold them: an edit of a
// section already pending joins that edit (mergeEdit), so one bundle
// section carries both; one of a section whose save is out joins it too,
// and is kept apart as what's dirty since (after), which is all that's
// left of the section once that save succeeds. A new section deleted
// before its save went out was never made: nothing is sent for it. Edits
// that can't join (mergeEdit) queue none of them.
func (s *canvasSaver) queueLocked(edits []quip.Edit) error {
	pending := append([]quip.Edit(nil), s.pending...)
	after := map[string]quip.Edit{}
	for k, v := range s.after {
		after[k] = v
	}
	for _, e := range edits {
		i := slices.IndexFunc(pending, func(p quip.Edit) bool { return p.SectionID == e.SectionID })
		if i < 0 {
			pending = append(pending, e)
			continue
		}
		flying := s.inFlight && slices.ContainsFunc(s.flight, func(f quip.Edit) bool { return f.SectionID == e.SectionID })
		if pending[i].Kind == quip.EditNew && e.Kind == quip.EditDelete && !flying {
			pending = slices.Delete(pending, i, i+1)
			continue
		}
		m, err := mergeEdit(pending[i], e)
		if err != nil {
			return err
		}
		if flying {
			if a, ok := after[e.SectionID]; ok {
				if after[e.SectionID], err = mergeEdit(a, e); err != nil {
					return err
				}
			} else {
				after[e.SectionID] = e
			}
		}
		pending[i] = m
	}
	s.pending, s.after = pending, after
	return nil
}

// mergeEdit is edit e of a section made while p of it is pending, as
// Slack's dirty fields hold the two. A new section edited again before it
// saved is still that new section, with the latest text, style or box (a
// list item's level, which initSibling copies, isn't changed before the
// first save yet). Two edits of a section are one: its text, style, level
// and box, each the latest one set (a tick and typing: content and
// attributes in one bundle section). A delete is the latest edit.
func mergeEdit(p, e quip.Edit) (quip.Edit, error) {
	switch {
	case p.Kind == quip.EditNew && e.Kind != quip.EditDelete:
		if e.Reindent {
			return p, fmt.Errorf("%w: a new list item's level before its first save", quip.ErrUnsupported)
		}
		if e.Kind == quip.EditText {
			p.RTML = e.RTML
		}
		if e.Restyle {
			p.Style = e.Style
		}
		if e.Kind == quip.EditCheck || e.Recheck {
			p.Checked = e.Checked
		}
		return p, nil
	case e.Kind == quip.EditDelete, p.Kind == quip.EditDelete, e.Kind == quip.EditNew:
		return e, nil
	}
	r := e
	if e.Kind != quip.EditText && p.Kind == quip.EditText {
		r.Kind, r.RTML = quip.EditText, p.RTML
	}
	if !e.Restyle && p.Restyle {
		r.Restyle, r.Style = true, p.Style
	}
	if !e.Reindent && p.Reindent {
		r.Reindent, r.Indent = true, p.Indent
	}
	eBox, pBox := e.Kind == quip.EditCheck || e.Recheck, p.Kind == quip.EditCheck || p.Recheck
	if !eBox && pBox {
		r.Checked = p.Checked
	}
	switch box := eBox || pBox; {
	case r.Kind == quip.EditText:
		r.Recheck = box
	case r.Restyle || r.Reindent:
		r.Kind, r.Recheck = quip.EditBlock, box
	default: // the box alone
		r.Kind, r.Recheck = quip.EditCheck, false
	}
	r.Base = p.Base
	return r, nil
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
		// was edited since (merged into it), for the retry; a new section
		// deleted since was never made, so nothing goes for it
		for _, e := range edits {
			i := slices.IndexFunc(s.pending, func(p quip.Edit) bool { return p.SectionID == e.SectionID })
			if e.Kind == quip.EditNew && i >= 0 && s.pending[i].Kind == quip.EditDelete {
				s.pending = slices.Delete(s.pending, i, i+1)
			}
		}
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
	var sent []string // the kept edits' own ids (a new section's temp id), in order
	kept := map[string]bool{}
	landed := map[string]int64{} // edits the model holds already, by the ids they were queued under: their sequence
	for _, e := range edits {
		live := func(id string) *quip.Section {
			if sec := d.Section(d.RealID(id)); sec != nil && (sec.Deleted == nil || !*sec.Deleted) {
				return sec
			}
			return nil
		}
		switch sec := live(e.SectionID); {
		case e.Kind == quip.EditNew && (kept[e.After] || live(e.After) != nil) && sec == nil:
			// made after a section that's still there
			keep, sent = append(keep, e), append(sent, e.SectionID)
			kept[e.SectionID] = true
		case e.Kind == quip.EditNew, sec == nil, sec.Sequence == nil || *sec.Sequence != e.Base:
			if held := d.Section(d.RealID(e.SectionID)); holdsEdit(held, e) {
				// the section holds the edit: this client's own save, its
				// answer lost (a timeout, a cut connection), not someone
				// else's (a new section is there under its temp id, which
				// the server keeps)
				landed[e.SectionID] = *held.Sequence
				continue
			}
			// Slack's editor would merge someone else's save into the
			// edit (mergeRtml); Clack can't, so it never writes over one
			dropped = append(dropped, e)
		default:
			keep, sent = append(keep, e), append(sent, e.SectionID)
			kept[e.SectionID] = true
		}
	}
	if len(keep) == 0 {
		q.mu.Unlock()
		if len(landed) > 0 {
			return landed, dropped, nil
		}
		return nil, dropped, fmt.Errorf("%w: %d edits", ErrCanvasConflict, len(dropped))
	}
	for i := range keep { // the ids the model holds them under now
		if keep[i].Kind != quip.EditNew {
			keep[i].SectionID = d.RealID(keep[i].SectionID)
		} else if !kept[keep[i].After] || d.Section(d.RealID(keep[i].After)) != nil {
			keep[i].After = d.RealID(keep[i].After)
		}
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
	saved := landed // by the ids the edits were queued under, with those that landed before
	q.mu.Lock()
	if ans.Changed != nil && *ans.Changed && q.doc == d { // the same model: nothing reloaded it (q.net)
		d.ApplySaved(&ans)
	}
	for i := range edits {
		real := q.doc.RealID(edits[i].SectionID)
		if real != edits[i].SectionID {
			a.savers().counters.Rename(edits[i].SectionID, real)
		}
		if sec := q.doc.Section(real); sec != nil && sec.Sequence != nil {
			saved[sent[i]] = *sec.Sequence
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
// lost after the server took the save): the same text, style, level and
// box, or deleted.
func holdsEdit(sec *quip.Section, e quip.Edit) bool {
	if sec == nil || sec.Sequence == nil {
		return false
	}
	deleted := sec.Deleted != nil && *sec.Deleted
	if e.Kind == quip.EditDelete || deleted {
		return e.Kind == quip.EditDelete && deleted
	}
	at := sec.Attributes
	checked := at != nil && at.Checked != nil && *at.Checked == e.Checked
	style := sec.Style != nil && *sec.Style == e.Style
	switch e.Kind {
	case quip.EditCheck:
		if !checked {
			return false
		}
	case quip.EditText:
		if quip.SectionRTML(sec) != e.RTML {
			return false
		}
	case quip.EditNew:
		return quip.SectionRTML(sec) == e.RTML && style && (!e.Checked || checked)
	case quip.EditBlock:
	default:
		return false
	}
	indent := int32(0)
	if at != nil && at.Indentation != nil {
		indent = *at.Indentation
	}
	return (!e.Recheck || checked) && (!e.Restyle || style) && (!e.Reindent || indent == e.Indent)
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
	return a.queueSave(ctx, id, func(c *saveCtx) ([]quip.Edit, error) {
		base := c.base
		b, err := plainBase(base, sectionID, orig)
		if err != nil {
			return nil, err
		}
		return []quip.Edit{{Kind: quip.EditText, SectionID: sectionID, RTML: rtml, Base: b.seq}}, nil
	})
}

// SaveCanvasDelete deletes a block's section (the block emptied). orig is
// the block's spans when the edit began, which the section has to hold
// still (else ErrCanvasConflict). A list's last item takes the list with
// it, as Slack's notifyDescendantDeleted_ does. Not deleted yet
// (quip.ErrUnsupported): a section holding mentions, emoji or dates (what
// Slack's editor does with their control sections then isn't ported), a
// block whose neighbours Slack would merge, a block in a layout. The
// canvas's last block isn't deleted but emptied, as Slack's editor leaves
// a section whose text is deleted (deleteEntity would make a new section
// in its place).
func (a *App) SaveCanvasDelete(ctx context.Context, id, sectionID string, orig []richtext.Span) error {
	return a.queueSave(ctx, id, func(c *saveCtx) ([]quip.Edit, error) {
		base := c.base
		b, ok := base(sectionID)
		switch {
		case !ok:
			return nil, fmt.Errorf("%w: section %s isn't in the canvas", ErrCanvasConflict, sectionID)
		case b.typ == quip.TypeTitle:
			// the title emptied: its section stays, with no text (Slack
			// shows its placeholder, and calls the canvas "Untitled")
			if _, err := plainBase(base, sectionID, spansPlain(orig)); err != nil {
				return nil, err
			}
			return []quip.Edit{{Kind: quip.EditText, SectionID: sectionID, RTML: "", Base: b.seq}}, nil
		case b.typ != quip.TypeText:
			return nil, fmt.Errorf("%w: a section of type %d", quip.ErrUnsupported, b.typ)
		}
		plain, ok := quip.RTMLPlain(b.rtml)
		if !ok {
			return nil, fmt.Errorf("%w: RTML Slack can't parse", quip.ErrUnsupported)
		}
		if plainCompare(plain) != plainCompare(spansPlain(orig)) {
			return nil, fmt.Errorf("%w: section %s", ErrCanvasConflict, sectionID)
		}
		if strings.ContainsRune(plain, quip.ControlMark) {
			return nil, fmt.Errorf("%w: deleting a block with mentions, emoji or dates", quip.ErrUnsupported)
		}
		// the new blocks pending: a top-level one stays after the delete,
		// one in this block's list keeps the list
		newTop, newInList := false, false
		for _, nid := range c.news {
			if nb, ok := c.base(nid); ok && nid != sectionID {
				newTop = newTop || len(nb.parents) == 0
				newInList = newInList || len(nb.parents) > 0 && len(b.parents) > 0 && nb.parents[0] == b.parents[0]
			}
		}
		edits := []quip.Edit{{Kind: quip.EditDelete, SectionID: sectionID, Base: b.seq}}
		merges, inLayout, left := false, false, false
		a.CanvasModel(id, func(d *quip.Doc) {
			if d == nil {
				return
			}
			real := d.RealID(sectionID)
			merges = d.DeleteMerges(real)
			if sec := d.Section(real); sec != nil && sec.HasLayoutParent != nil && *sec.HasLayoutParent {
				inLayout = true
			}
			gone := map[string]bool{real: true}
			if len(b.parents) > 0 && !newInList { // a list item: the last one takes its list with it (notifyDescendantDeleted_)
				list, others := b.parents[0], 0
				for _, c := range d.ListChildren(list) {
					if c != real {
						others++
					}
				}
				if others == 0 {
					listSeq := int64(0)
					if sec := d.Section(list); sec != nil && sec.Sequence != nil {
						listSeq = *sec.Sequence
					}
					gone[list] = true
					edits = append(edits, quip.Edit{Kind: quip.EditDelete, SectionID: list, Base: listSeq})
				}
			}
			left = newTop || !d.LeavesNoBlock(func(id string) bool { return gone[id] })
		})
		switch {
		case merges: // deleteSection would merge the two lists (or code blocks) around it
			return nil, fmt.Errorf("%w: deleting a block between two lists or code blocks Slack's editor would merge", quip.ErrUnsupported)
		case inLayout: // the layout's onChildRemoved would change it too
			return nil, fmt.Errorf("%w: deleting a block in a layout", quip.ErrUnsupported)
		case !left:
			// the canvas's last block stays, emptied, as Slack's editor
			// leaves a section whose text is deleted (deleting it,
			// deleteEntity would make a new section in its place)
			return []quip.Edit{{Kind: quip.EditText, SectionID: sectionID, RTML: "", Base: b.seq}}, nil
		}
		return edits, nil
	})
}

// SaveCanvasSpans saves a text block's new content as the editor holds
// it (canvas spans: text with its formatting and links, line breaks, and
// the block's mentions and emoji), written onto the section's RTML
// (quip.ApplyRuns): each control stays where the span standing for it is,
// and comment anchors move with their characters. orig is the block's
// spans when the edit began, which the section has to hold still (else
// ErrCanvasConflict). It refuses (quip.ErrUnsupported) what Slack's editor
// would write and Clack can't yet: a new mention or emoji (a control
// section of its own), a date, an @-mention of a group, a canvas, file or
// message mention, highlighted text, a code block.
func (a *App) SaveCanvasSpans(ctx context.Context, id, sectionID string, orig, spans []richtext.Span) error {
	return a.queueSave(ctx, id, func(c *saveCtx) ([]quip.Edit, error) {
		e, err := spansEdit(c.base, sectionID, orig, spans, 0)
		if err != nil {
			return nil, err
		}
		return []quip.Edit{e}, nil
	})
}

// spansEdit is SaveCanvasSpans' edit of the section.
//
// origSeq is the section's sequence when the block opened (0: unknown):
// when the block as it began doesn't write the section back, an edit over
// it would lose what the section holds that the block doesn't show, and
// that's someone else's change (ErrCanvasConflict) when the section moved
// since, else something the editor can't hold (quip.ErrUnsupported).
func spansEdit(base func(string) (sectionBase, bool), sectionID string, orig, spans []richtext.Span, origSeq int64) (quip.Edit, error) {
	b, ok := base(sectionID)
	switch {
	case !ok:
		return quip.Edit{}, fmt.Errorf("%w: section %s isn't in the canvas", ErrCanvasConflict, sectionID)
	case b.typ != quip.TypeText:
		return quip.Edit{}, fmt.Errorf("%w: a section of type %d", quip.ErrUnsupported, b.typ)
	case b.sty == quip.StyleCode:
		return quip.Edit{}, fmt.Errorf("%w: a code block", quip.ErrUnsupported)
	}
	plain, ok := quip.RTMLPlain(b.rtml)
	if !ok {
		return quip.Edit{}, fmt.Errorf("%w: RTML Slack can't parse", quip.ErrUnsupported)
	}
	if plainCompare(plain) != plainCompare(spansPlain(orig)) {
		return quip.Edit{}, fmt.Errorf("%w: section %s", ErrCanvasConflict, sectionID)
	}
	for _, s := range orig {
		switch {
		case !controlSpan(s), s.Kind == richtext.User && s.ID != "", s.Kind == richtext.Channel && s.ID != "", s.Kind == richtext.Emoji:
		default:
			// the editor holds a person, a channel and an emoji as they
			// are; anything else it would turn into text or a link
			return quip.Edit{}, fmt.Errorf("%w: a date, @-mention of a group or canvas, file or message mention", quip.ErrUnsupported)
		}
	}
	tail, err := faithfulBlock(b.rtml, orig)
	if err != nil {
		if origSeq != 0 && b.seq != origSeq {
			return quip.Edit{}, fmt.Errorf("%w: section %s", ErrCanvasConflict, sectionID)
		}
		return quip.Edit{}, err
	}
	ctls, _ := quip.RTMLControls(b.rtml)
	runs, err := spanRuns(orig, spans, ctls)
	if err != nil {
		return quip.Edit{}, err
	}
	for k := 0; k < tail; k++ { // the section's trailing line breaks stay
		runs = append(runs, quip.Run{Break: true})
	}
	rtml, err := quip.ApplyRuns(b.rtml, runs)
	if err != nil {
		return quip.Edit{}, err
	}
	return quip.Edit{Kind: quip.EditText, SectionID: sectionID, RTML: rtml, Base: b.seq}, nil
}

// faithfulBlock checks that a block shown as orig is all of a section of
// RTML rtml: its runs (with the section's line breaks at its end, a blank
// last line in Slack, which Clack's export drops) write the section back
// as it is. Else an edit over the block would lose what the section holds
// that the block doesn't show: someone else's formatting or link since, a
// link Clack's export doesn't show (tel:, ftp:, an over-long URL), a mark
// the editor can't hold, an empty comment anchor. It reports how many
// trailing line breaks the block doesn't show, which an edit keeps.
func faithfulBlock(rtml string, orig []richtext.Span) (int, error) {
	ctls, _ := quip.RTMLControls(rtml)
	runs, err := spanRuns(orig, orig, ctls)
	if err != nil {
		return 0, err
	}
	tail := 0
	if plain, ok := quip.RTMLPlain(rtml); ok {
		tail = len(plain) - len(strings.TrimRight(plain, "\n"))
	}
	for k := len(runs) - 1; k >= 0 && runs[k].Break && tail > 0; k-- {
		tail-- // shown ones
	}
	for k := 0; k < tail; k++ {
		runs = append(runs, quip.Run{Break: true})
	}
	if was, err := quip.ApplyRuns(rtml, runs); err != nil || !quip.SameRTML(was, rtml) {
		return 0, fmt.Errorf("%w: the block holds formatting or links Clack's editor doesn't show", quip.ErrUnsupported)
	}
	return tail, nil
}

// CanvasSectionSeq is the section's sequence in core's model of the canvas
// (0 when core holds no model, or no such section): a block's edit begun
// now is made against it (CanvasBlockEdit.OrigSeq).
func (a *App) CanvasSectionSeq(id, sectionID string) int64 {
	var n int64
	a.CanvasModel(id, func(d *quip.Doc) {
		if d == nil {
			return
		}
		if sec := d.Section(d.RealID(sectionID)); sec != nil && sec.Sequence != nil {
			n = *sec.Sequence
		}
	})
	return n
}

// CanvasTitleSaveable reports whether the canvas's title is one a save can
// send the metadata of (quip.TitleMetadata, which every save sends), and
// why not: while it isn't, no edit of the canvas saves. It's true when core
// holds no model of the canvas yet.
func (a *App) CanvasTitleSaveable(id string) (bool, string) {
	ok, why := true, ""
	a.CanvasModel(id, func(d *quip.Doc) {
		if d == nil {
			return
		}
		for _, t := range d.Sections() {
			if t.Type != nil && *t.Type == quip.TypeTitle {
				if _, err := quip.TitleMetadata(quip.SectionRTML(t)); err != nil {
					ok, why = false, "the canvas's title has an emoji, a mention, a link or a line break, which Clack can't save yet"
				}
				return
			}
		}
	})
	return ok, why
}

// CanvasBlockEditable reports whether a block shown as spans can be edited
// through edit-document without losing what its section holds that the
// block doesn't show (faithfulBlock), and why not; the title's own block
// only while it's plain text (titleBlockEdit). It's true when core holds no
// model of the canvas yet, or the block's text is another's than the
// section's (the export is late: the save checks again).
func (a *App) CanvasBlockEditable(id, sectionID string, spans []richtext.Span) (bool, string) {
	ok, why := true, ""
	a.CanvasModel(id, func(d *quip.Doc) {
		if d == nil {
			return
		}
		sec := d.Section(d.RealID(sectionID))
		if sec != nil && sec.Type != nil && *sec.Type == quip.TypeTitle {
			if t, marks, pok := quip.ParseRTML(quip.SectionRTML(sec)); !pok || len(marks) > 0 || strings.Contains(t, "<control") || strings.Contains(t, "<br>") {
				ok, why = false, "formatting, a link, a mention or a line break in the title"
			}
			return
		}
		if sec == nil || sec.Type == nil || *sec.Type != quip.TypeText {
			return
		}
		rtml := quip.SectionRTML(sec)
		if plain, pok := quip.RTMLPlain(rtml); !pok || plainCompare(plain) != plainCompare(spansPlain(spans)) {
			return
		}
		if _, err := faithfulBlock(rtml, spans); err != nil {
			ok, why = false, "formatting or links Clack can't show"
		}
	})
	return ok, why
}

// CanvasBlockEdit is an edit of one block through edit-document: what the
// block was when the edit began (its spans, kind, level and box) and what
// it is now.
type CanvasBlockEdit struct {
	SectionID   string
	Orig        []richtext.Span
	OrigKind    canvas.Kind
	OrigLevel   int
	OrigChecked bool
	OrigSeq     int64 // the section's sequence when the block opened (CanvasSectionSeq; 0: unknown)
	Spans       []richtext.Span
	Kind        canvas.Kind
	Level       int
	Checked     bool
}

// SaveCanvasBlock saves a block's edit: its text as SaveCanvasSpans saves
// it, and a new style between a paragraph and the headings
// (setSectionStyle), a list item's new level or a checklist item's box
// (setSectionAttribute), each with the fields Slack's setters mark. A
// list's kind changed, or a block moved in or out of a list, isn't saved
// yet (quip.ErrUnsupported). An edit that changes nothing sends nothing.
func (a *App) SaveCanvasBlock(ctx context.Context, id string, be CanvasBlockEdit) error {
	return a.queueSave(ctx, id, func(c *saveCtx) ([]quip.Edit, error) {
		e, err := blockEdit(c.base, be)
		if err != nil || e.Kind == 0 {
			return nil, err
		}
		return []quip.Edit{e}, nil
	})
}

// blockEdit is SaveCanvasBlock's edit (Kind 0: nothing changed).
func blockEdit(base func(string) (sectionBase, bool), be CanvasBlockEdit) (quip.Edit, error) {
	if b, ok := base(be.SectionID); ok && b.typ == quip.TypeTitle {
		return titleBlockEdit(base, be)
	}
	e, err := spansEdit(base, be.SectionID, be.Orig, be.Spans, be.OrigSeq)
	if err != nil {
		return e, err
	}
	b, _ := base(be.SectionID)
	if be.Kind != be.OrigKind {
		ns, okNew := textStyle(be.Kind)
		os, okOld := textStyle(be.OrigKind)
		switch {
		case !okNew || !okOld || len(b.parents) > 0 || b.sty == quip.StyleBlockquote:
			return quip.Edit{}, fmt.Errorf("%w: a block made a %v from a %v", quip.ErrUnsupported, be.Kind, be.OrigKind)
		case b.sty != os: // restyled meanwhile
			return quip.Edit{}, fmt.Errorf("%w: section %s isn't a %v any more", ErrCanvasConflict, be.SectionID, be.OrigKind)
		}
		e.Restyle, e.Style = true, ns
	}
	if be.Level != be.OrigLevel {
		if !be.Kind.IsList() || be.Kind != be.OrigKind || len(b.parents) == 0 || be.Level < 0 || be.Level > quip.MaxListLevel {
			return quip.Edit{}, fmt.Errorf("%w: a block's level changed outside a list", quip.ErrUnsupported)
		}
		e.Reindent, e.Indent = true, int32(be.Level)
	}
	if be.Checked != be.OrigChecked {
		if be.Kind != canvas.Check || be.OrigKind != canvas.Check || len(b.parents) == 0 {
			return quip.Edit{}, fmt.Errorf("%w: a box outside a checklist", quip.ErrUnsupported)
		}
		e.Recheck, e.Checked = true, be.Checked
	}
	// the text as it was (the block as it began writes the section back,
	// faithfulBlock): the style or attributes alone, as Slack's setters
	// leave the content
	same := quip.SameRTML(e.RTML, b.rtml)
	if same && !b.isNew {
		switch {
		case e.Recheck && !e.Restyle && !e.Reindent:
			return quip.Edit{Kind: quip.EditCheck, SectionID: e.SectionID, Checked: e.Checked, Base: e.Base}, nil
		case e.Restyle || e.Reindent || e.Recheck:
			e.Kind, e.RTML = quip.EditBlock, ""
		default:
			return quip.Edit{}, nil
		}
	}
	return e, nil
}

// titleBlockEdit is an edit of the canvas's title where it's drawn (the
// document's opening heading, which is its title section): its text, as a
// rename saves it (SaveCanvasTitle). Formatting, a link, a mention or a
// line break in the title isn't saved yet (TitleMetadata holds the plain
// title only), nor the title made another kind of block.
func titleBlockEdit(base func(string) (sectionBase, bool), be CanvasBlockEdit) (quip.Edit, error) {
	if be.Kind != be.OrigKind || be.Level != be.OrigLevel || be.Checked != be.OrigChecked {
		return quip.Edit{}, fmt.Errorf("%w: the title made another kind of block", quip.ErrUnsupported)
	}
	var text strings.Builder
	for _, sp := range be.Spans {
		if sp.Kind != richtext.Text || sp.Style != 0 {
			return quip.Edit{}, fmt.Errorf("%w: formatting, a link, a mention or a line break in the title", quip.ErrUnsupported)
		}
		text.WriteString(sp.Text)
	}
	orig := spansPlain(be.Orig)
	b, err := plainBase(base, be.SectionID, orig)
	if err != nil {
		if errors.Is(err, ErrCanvasConflict) || be.OrigSeq == 0 || b.seq == be.OrigSeq {
			return quip.Edit{}, err
		}
		return quip.Edit{}, fmt.Errorf("%w: section %s", ErrCanvasConflict, be.SectionID) // changed since the block opened
	}
	if text.String() == orig {
		return quip.Edit{}, nil
	}
	rtml, err := quip.TextRTML(text.String())
	if err != nil {
		return quip.Edit{}, err
	}
	return quip.Edit{Kind: quip.EditText, SectionID: be.SectionID, RTML: rtml, Base: b.seq}, nil
}

// textStyle is a top-level text block's section style (a paragraph or a
// heading).
func textStyle(k canvas.Kind) (int32, bool) {
	switch k {
	case canvas.Paragraph:
		return quip.StylePlain, true
	case canvas.Heading1:
		return quip.StyleH1, true
	case canvas.Heading2:
		return quip.StyleH2, true
	case canvas.Heading3:
		return quip.StyleH3, true
	}
	return 0, false
}

// SaveCanvasCode saves a code block's new text: its lines written onto the
// section's RTML as Slack's save writer writes the code block's DOM (each
// character through escapeText, a kept one as the DOM holds it, a line
// break between lines: quip.ApplyRuns), its language as it was. orig is
// the code the edit began from, which the section has to hold still (else
// ErrCanvasConflict).
func (a *App) SaveCanvasCode(ctx context.Context, id, sectionID, orig, code string) error {
	return a.queueSave(ctx, id, func(c *saveCtx) ([]quip.Edit, error) {
		b, ok := c.base(sectionID)
		switch {
		case !ok:
			return nil, fmt.Errorf("%w: section %s isn't in the canvas", ErrCanvasConflict, sectionID)
		case b.typ != quip.TypeText || b.sty != quip.StyleCode:
			return nil, fmt.Errorf("%w: section %s isn't a code block", quip.ErrUnsupported, sectionID)
		}
		plain, ok := quip.RTMLPlain(b.rtml)
		switch {
		case !ok:
			return nil, fmt.Errorf("%w: RTML Slack can't parse", quip.ErrUnsupported)
		case strings.ContainsRune(plain, quip.ControlMark):
			return nil, fmt.Errorf("%w: a code block holding a mention", quip.ErrUnsupported)
		case plainCompare(plain) != plainCompare(orig):
			return nil, fmt.Errorf("%w: section %s", ErrCanvasConflict, sectionID)
		}
		var runs []quip.Run
		for i, line := range strings.Split(code, "\n") {
			if i > 0 {
				runs = append(runs, quip.Run{Break: true})
			}
			if line != "" {
				runs = append(runs, quip.Run{Text: line})
			}
		}
		rtml, err := quip.ApplyRuns(b.rtml, runs)
		if err != nil {
			return nil, err
		}
		if rtml == b.rtml {
			return nil, nil // as it was
		}
		return []quip.Edit{{Kind: quip.EditText, SectionID: sectionID, RTML: rtml, Base: b.seq}}, nil
	})
}

// CanvasNewBlock is a block an edit made (Enter in a block, or typing
// under the last one): its new section's id (NewCanvasSectionID, so a
// later edit of the block reaches it), kind, level, box and spans.
type CanvasNewBlock struct {
	ID      string
	Kind    canvas.Kind
	Level   int
	Checked bool
	Spans   []richtext.Span
}

// NewCanvasSectionID is an id for a new section of canvas id's document
// (ZRIn.generateEntityId with its shard); "" until core holds its model.
func (a *App) NewCanvasSectionID(id string) string {
	out := ""
	a.CanvasModel(id, func(d *quip.Doc) {
		if d != nil {
			out = d.NewSectionIDFor()
		}
	})
	return out
}

// CanvasSectionID is the id canvas id's model holds section sectionID
// under now: a new section's real id once the server gave it one.
func (a *App) CanvasSectionID(id, sectionID string) string {
	out := sectionID
	a.CanvasModel(id, func(d *quip.Doc) {
		if d != nil {
			out = d.RealID(sectionID)
		}
	})
	return out
}

// SaveCanvasSplit saves an edit of block sectionID that left more than one
// block (Enter in it): the block's own new content (first, checked
// against orig as SaveCanvasSpans checks it), and new sections after it
// (rest), as Slack's editor makes them, in one save.
func (a *App) SaveCanvasSplit(ctx context.Context, id, sectionID string, orig, first []richtext.Span, rest []CanvasNewBlock, origSeq int64) error {
	return a.queueSave(ctx, id, func(c *saveCtx) ([]quip.Edit, error) {
		e, err := spansEdit(c.base, sectionID, orig, first, origSeq)
		if err != nil {
			return nil, err
		}
		news, err := a.newBlockEdits(id, c, sectionID, rest)
		if err != nil {
			return nil, err
		}
		return append([]quip.Edit{e}, news...), nil
	})
}

// SaveCanvasNewBlocks saves blocks typed where there was none, after
// section after (Slack's editor starts one under the last section).
func (a *App) SaveCanvasNewBlocks(ctx context.Context, id, after string, blocks []CanvasNewBlock) error {
	return a.queueSave(ctx, id, func(c *saveCtx) ([]quip.Edit, error) {
		return a.newBlockEdits(id, c, after, blocks)
	})
}

// newBlockEdits are the new sections blocks make after section after, each
// after the one before (createNewSection with the section before as its
// base). What a new section can't be yet is refused (quip.ErrUnsupported):
// a list where there was none or the end of one (a new block's kind has to
// keep the list it follows), a list item at another level, a mention,
// emoji or date (a control section of its own).
func (a *App) newBlockEdits(id string, c *saveCtx, after string, blocks []CanvasNewBlock) ([]quip.Edit, error) {
	ab, ok := c.base(after)
	switch {
	case !ok:
		return nil, fmt.Errorf("%w: section %s isn't in the canvas", ErrCanvasConflict, after)
	case ab.typ != quip.TypeText || ab.sty == quip.StyleCode || ab.sty == quip.StyleTitle:
		return nil, fmt.Errorf("%w: a new block after a section of type %d, style %d", quip.ErrUnsupported, ab.typ, ab.sty)
	}
	inList := len(ab.parents) > 0
	taken := append([]string(nil), c.taken...)
	prev, prevPos := after, ab.pos
	var out []quip.Edit
	for _, b := range blocks {
		style, list := quip.StylePlain, int32(0)
		switch b.Kind {
		case canvas.Paragraph:
		case canvas.Heading1:
			style = quip.StyleH1
		case canvas.Heading2:
			style = quip.StyleH2
		case canvas.Heading3:
			style = quip.StyleH3
		case canvas.Bullet:
			list = quip.StyleBullet
		case canvas.Numbered:
			list = quip.StyleNumbered
		case canvas.Check:
			list = quip.StyleChecklist
		default:
			return nil, fmt.Errorf("%w: a new block of kind %d", quip.ErrUnsupported, b.Kind)
		}
		switch {
		case b.ID == "":
			return nil, fmt.Errorf("%w: a new block without an id", quip.ErrUnsupported)
		case inList != (list != 0) || inList && list != ab.listStyle:
			return nil, fmt.Errorf("%w: a new block that starts or ends a list", quip.ErrUnsupported)
		case inList && int32(b.Level) != max(ab.indent, 0), !inList && b.Level != 0:
			return nil, fmt.Errorf("%w: a new list item at another level", quip.ErrUnsupported)
		case b.Checked && b.Kind != canvas.Check:
			return nil, fmt.Errorf("%w: a ticked box outside a checklist", quip.ErrUnsupported)
		}
		runs, err := spanRuns(nil, b.Spans, nil)
		if err != nil {
			return nil, err
		}
		rtml, err := quip.ApplyRuns("", runs)
		if err != nil {
			return nil, err
		}
		pos := ""
		a.CanvasModel(id, func(d *quip.Doc) {
			if d != nil && prevPos != "" {
				pos = d.PositionAfter(prevPos, taken)
			}
		})
		if pos == "" {
			return nil, fmt.Errorf("%w: no position after section %s", quip.ErrUnsupported, prev)
		}
		taken = append(taken, pos)
		out = append(out, quip.Edit{Kind: quip.EditNew, SectionID: b.ID, RTML: rtml, After: prev, Style: style, Position: pos, Checked: b.Checked})
		prev, prevPos = b.ID, pos
	}
	return out, nil
}

// controlSpan reports whether a canvas span is one of Quip's controls.
func controlSpan(s richtext.Span) bool {
	switch s.Kind {
	case richtext.User, richtext.Channel, richtext.Usergroup, richtext.Broadcast, richtext.Emoji, richtext.Date:
		return true
	case richtext.Link:
		return s.Control
	}
	return false
}

// spansPlain is spans as quip.RTMLPlain writes a section: a control as
// quip.ControlMark, a line break as "\n".
func spansPlain(spans []richtext.Span) string {
	var b strings.Builder
	for _, s := range spans {
		switch {
		case controlSpan(s):
			b.WriteRune(quip.ControlMark)
		case s.Kind == richtext.Break:
			b.WriteByte('\n')
		default:
			b.WriteString(s.Text)
		}
	}
	return b.String()
}

// spanRuns turns an editor's spans into ApplyRuns' runs: a control span
// is the section's control it stood for when the edit began (orig's
// controls are ctls, in order), matched in order by what it shows, then
// anywhere (a cut and paste).
func spanRuns(orig, spans []richtext.Span, ctls []string) ([]quip.Run, error) {
	type ctl struct {
		s  richtext.Span
		id string
	}
	var have []ctl
	for _, s := range orig {
		if controlSpan(s) {
			if len(have) >= len(ctls) {
				return nil, fmt.Errorf("%w: the block shows more controls than the section holds", ErrCanvasConflict)
			}
			have = append(have, ctl{s, ctls[len(have)]})
		}
	}
	if len(have) != len(ctls) {
		return nil, fmt.Errorf("%w: the block shows fewer controls than the section holds", ErrCanvasConflict)
	}
	used := make([]bool, len(have))
	next := 0
	var runs []quip.Run
	for _, s := range spans {
		switch {
		case controlSpan(s):
			found := -1
			for k := next; k < len(have) && found < 0; k++ {
				if !used[k] && sameControl(have[k].s, s) {
					found = k
				}
			}
			for k := 0; k < len(have) && found < 0; k++ {
				if !used[k] && sameControl(have[k].s, s) {
					found = k
				}
			}
			if found < 0 {
				return nil, fmt.Errorf("%w: a new mention, emoji or date", quip.ErrUnsupported)
			}
			used[found], next = true, found+1
			runs = append(runs, quip.Run{Control: have[found].id})
		case s.Kind == richtext.Break:
			runs = append(runs, quip.Run{Break: true})
		case s.Kind == richtext.Text || s.Kind == richtext.Link:
			if s.Style&richtext.Highlight != 0 {
				return nil, fmt.Errorf("%w: highlighted text", quip.ErrUnsupported)
			}
			r := quip.Run{Text: s.Text, Bold: s.Style&richtext.Bold != 0, Italic: s.Style&richtext.Italic != 0,
				Underline: s.Style&richtext.Underline != 0, Strike: s.Style&richtext.Strike != 0, Code: s.Style&richtext.Code != 0}
			if s.Kind == richtext.Link {
				r.Href = s.URL
			}
			runs = append(runs, r)
		default:
			return nil, fmt.Errorf("%w: an inline object of kind %d", quip.ErrUnsupported, s.Kind)
		}
	}
	return runs, nil
}

// sameControl reports whether two spans show the same control (an
// editor's copy of a mention is a new span, equal in what it shows).
func sameControl(a, b richtext.Span) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case richtext.User, richtext.Channel, richtext.Emoji:
		return a.ID == b.ID && (a.ID != "" || a.Text == b.Text)
	}
	return false
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

// untitled reports whether a title section's RTML holds no title.
func untitled(rtml string) bool {
	t, marks, ok := quip.ParseRTML(rtml)
	return ok && len(marks) == 0 && plainCompare(html.UnescapeString(t)) == ""
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
	return a.queueSave(ctx, id, func(c *saveCtx) ([]quip.Edit, error) {
		tid := a.canvasTitleID(id)
		if tid == "" {
			return nil, fmt.Errorf("%w: the canvas has no title section", quip.ErrUnsupported)
		}
		if b, ok := c.base(tid); ok && b.typ == quip.TypeTitle && untitled(b.rtml) {
			// files.info calls an untitled canvas "Untitled", or by its
			// first words, which its empty title section doesn't hold: a
			// rename goes from whatever name it began with, as Slack's
			// renameTitle sets the title without looking at the old one
			return []quip.Edit{{Kind: quip.EditText, SectionID: tid, RTML: rtml, Base: b.seq}}, nil
		}
		b, err := plainBase(c.base, tid, orig)
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
