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
	mu        sync.Mutex
	pending   []quip.Edit // by section, the latest edit each
	since     time.Time   // when the oldest pending edit came
	errors    []saveError // the failures in a row (errorState_.errors_)
	idDiff    int         // saves attempted since the last success
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
	st := CanvasSaveState{Pending: len(s.pending) > 0, Saving: s.inFlight}
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

// queueSave loads the canvas's model, has build make the edits against
// each section's base (under the saver's lock, so a save finishing
// meanwhile can't move it), queues them and runs a save.
func (a *App) queueSave(ctx context.Context, id string, build func(base func(string) (sectionBase, bool)) ([]quip.Edit, error)) error {
	if err := a.LoadCanvasModel(ctx, id); err != nil {
		return err
	}
	s := a.saver(id)
	s.mu.Lock() // the saver, then the model (never the other way)
	if s.hasErrorLocked(time.Now()) {
		s.mu.Unlock()
		return ErrCanvasReadOnly
	}
	base := func(sectionID string) (sectionBase, bool) {
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
	edits, err := build(base)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if len(s.pending) == 0 {
		s.since = time.Now()
	}
	for _, e := range edits {
		replaced := false
		for i := range s.pending {
			if s.pending[i].SectionID == e.SectionID {
				s.pending[i], replaced = e, true
			}
		}
		if !replaced {
			s.pending = append(s.pending, e)
		}
	}
	s.armPollLocked(a, id)
	s.mu.Unlock()
	a.emit(Event{Type: "canvas_save", ID: id})
	return a.saveAttempt(ctx, id, s)
}

// armPollLocked starts the dirty warning's poll while edits are pending.
func (s *canvasSaver) armPollLocked(a *App, id string) {
	if s.poll != nil {
		return
	}
	s.poll = time.AfterFunc(unsavedPoll, func() {
		s.mu.Lock()
		s.poll = nil
		pending := len(s.pending) > 0 && a.bg.Err() == nil // nothing after the app shut down
		if pending {
			s.armPollLocked(a, id)
		}
		s.mu.Unlock()
		if pending {
			a.emit(Event{Type: "canvas_save", ID: id})
		}
	})
}

// saveAttempt is i40t.save for the canvas's pending edits.
func (a *App) saveAttempt(ctx context.Context, id string, s *canvasSaver) error {
	s.mu.Lock()
	if s.inFlight {
		// shouldDeferSave_: one save at a time, the next one 200 ms later
		s.again = true
		s.mu.Unlock()
		return nil
	}
	if len(s.pending) == 0 {
		s.mu.Unlock()
		return nil
	}
	edits := append([]quip.Edit(nil), s.pending...)
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
	leave := func(gone []quip.Edit) { // these edits leave the queue (a later edit of the same section made meanwhile stays)
		left := s.pending[:0]
		for _, p := range s.pending {
			if !slices.Contains(gone, p) {
				left = append(left, p)
			}
		}
		s.pending = left
	}
	leave(dropped) // edits of sections someone else saved: never sent
	switch {
	case err == nil || refused:
		// saved, or never sendable
		leave(edits)
		if err == nil {
			s.errors, s.idDiff, s.reason, s.forbidden = nil, 0, "", time.Time{}
			// an edit made meanwhile of a section just saved builds on
			// this save: its base is the saved sequence
			for i := range s.pending {
				if q, ok := saved[s.pending[i].SectionID]; ok {
					s.pending[i].Base = q
				}
			}
		}
		if len(s.pending) > 0 {
			again = true
		}
	default:
		info := saveErrorInfo(err)
		if info.typ == saveErrForbidden {
			// setError(1, 7, 15000) at once, not counted
			s.forbidden = time.Now()
		} else {
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
				_ = a.saveAttempt(rctx, id, s)
			})
		}
		again = false
		log.Printf("core: canvas %s: save failed (%d in a row): %s", id, len(s.errors), slack.FailureDetail(err))
	}
	s.mu.Unlock()
	if err == nil && len(dropped) > 0 {
		err = fmt.Errorf("%w: %d edits", ErrCanvasConflict, len(dropped))
	}
	a.emit(Event{Type: "canvas_save", ID: id})
	if again {
		time.AfterFunc(saveRetryWhileInFlight, func() {
			if a.bg.Err() != nil {
				return
			}
			rctx, cancel := context.WithTimeout(a.bg, 60*time.Second)
			defer cancel()
			_ = a.saveAttempt(rctx, id, s)
		})
	}
	return err
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
	if err := a.fetchModelLocked(ctx, id, q); err != nil && ctx.Err() == nil {
		// Slack saves without asking first; a failed catch-up only means
		// the model is as it was
		log.Printf("core: canvas %s: fetch-latest before a save: %s", id, slack.FailureDetail(err))
	}
	q.mu.Lock()
	d, thread := q.doc, q.thread
	if d == nil {
		q.mu.Unlock()
		return nil, nil, errors.New("core: no canvas model")
	}
	var keep, dropped []quip.Edit
	for _, e := range edits {
		sec := d.Section(e.SectionID)
		switch {
		case sec == nil || sec.Deleted != nil && *sec.Deleted, sec.Sequence == nil || *sec.Sequence != e.Base:
			// Slack's editor would merge someone else's save into the
			// edit (mergeRtml); Clack can't, so it never writes over one
			dropped = append(dropped, e)
		default:
			keep = append(keep, e)
		}
	}
	if len(keep) == 0 {
		q.mu.Unlock()
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
	saved := map[string]int64{}
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
	pending := len(s.pending) > 0 && !s.inFlight
	if s.retry != nil {
		s.retry.Stop()
		s.retry = nil
	}
	s.mu.Unlock()
	if pending {
		_ = a.saveAttempt(ctx, id, s)
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
	out := ""
	a.CanvasModel(id, func(d *quip.Doc) {
		for _, s := range d.Sections() {
			if s.Type != nil && *s.Type == quip.TypeTitle {
				out = *s.ID
			}
		}
	})
	if out == "" {
		return "", fmt.Errorf("%w: the canvas has no title section", quip.ErrUnsupported)
	}
	return out, nil
}
