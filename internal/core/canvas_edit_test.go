package core

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/quip"
	"github.com/some-du6e/clack/internal/slack"
)

// fastSaves shortens Slack's save timings for a test.
func fastSaves(t *testing.T) {
	t.Helper()
	b, w, p := saveBackoffBase, unsavedWarnAfter, unsavedPoll
	saveBackoffBase, unsavedWarnAfter, unsavedPoll = 30*time.Millisecond, 400*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { saveBackoffBase, unsavedWarnAfter, unsavedPoll = b, w, p })
}

// modelSection is the model's live text section whose RTML starts with
// prefix.
func modelSection(t *testing.T, a *App, id, prefix string) quip.Section {
	t.Helper()
	var out *quip.Section
	a.CanvasModel(id, func(d *quip.Doc) {
		if d == nil {
			t.Fatal("no model")
		}
		for _, s := range d.Sections() {
			if s.Type != nil && *s.Type == quip.TypeText && strings.HasPrefix(quip.SectionRTML(s), prefix) {
				c := *s
				out = &c
			}
		}
	})
	if out == nil {
		t.Fatalf("no section starting %q", prefix)
	}
	return *out
}

func saves(fs *fakeslack.Server) []fakeslack.QuipRequest {
	var out []fakeslack.QuipRequest
	for _, r := range fs.QuipRequests() {
		if r.Path == "edit-document" {
			out = append(out, r)
		}
	}
	return out
}

func waitSaveState(t *testing.T, a *App, id string, d time.Duration, want func(CanvasSaveState) bool) CanvasSaveState {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		st := a.CanvasSaveState(id)
		if want(st) {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("save state %+v", st)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// retryNow runs the saver's armed retry at once, as its timer would. A
// test that holds the retries (saveBackoffBase an hour) runs each one once
// it has checked the state between attempts, so no timer ends that state
// before the test sees it.
func retryNow(t *testing.T, a *App, id string) {
	t.Helper()
	s := a.saver(id)
	s.mu.Lock()
	tm := s.retry
	s.retry = nil
	s.mu.Unlock()
	if tm == nil || !tm.Stop() {
		t.Fatal("no retry armed")
	}
	ctx, cancel := context.WithTimeout(a.bg, 60*time.Second)
	defer cancel()
	_ = a.saveAttempt(ctx, id, s, false)
}

// retryArmed is whether the canvas's saver has a retry waiting.
func retryArmed(a *App, id string) bool {
	s := a.saver(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.retry != nil
}

func loadModel(t *testing.T, a *App, id string) {
	t.Helper()
	if err := a.LoadCanvasModel(context.Background(), id); err != nil {
		t.Fatal(err)
	}
}

// One edit: one load-data, a catch-up, one edit-document with Slack's form,
// the model at the saved sequence and the export showing it.
func TestCanvasSaveRoundTrip(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	loadModel(t, a, fakeslack.CanvasShared)
	p := modelSection(t, a, fakeslack.CanvasShared, "Weekly sync")
	if err := a.SaveCanvas(ctx, fakeslack.CanvasShared, quip.Edit{Kind: quip.EditText, SectionID: *p.ID, RTML: "Weekly sync, rewritten &amp; saved"}); err != nil {
		t.Fatal(err)
	}
	rs := saves(fs)
	if len(rs) != 1 || rs[0].Status != http.StatusOK {
		t.Fatalf("edit-document calls %+v", rs)
	}
	r := rs[0]
	if strings.Join(r.Fields, ",") != "thread,document,retry_count,session,sequence,data_binary,title,token" ||
		r.Form.Get("retry_count") != "0" || r.Form.Get("session") != a.savers().counters.Session || r.Form.Get("title") != "Weekly sync notes" {
		t.Errorf("the form %v %v", r.Fields, r.Form)
	}
	if len(r.Save.Sections) != 1 || quip.FieldsString(&r.Save.Sections[0]) != "1,2,9,12,13,19,20,33" || *r.Save.Sections[0].LocalSequence != 1 ||
		*r.Save.Sections[0].Sequence != *p.Sequence || r.Save.TitleRichText == nil {
		t.Errorf("the bundle %+v", r.Save)
	}
	after := modelSection(t, a, fakeslack.CanvasShared, "Weekly sync")
	if quip.SectionRTML(&after) != "Weekly sync, rewritten &amp; saved" || *after.Sequence <= *p.Sequence {
		t.Errorf("the model after the save: %q at %d", quip.SectionRTML(&after), *after.Sequence)
	}
	fc, _ := fs.Canvas(fakeslack.CanvasShared)
	if b := fc.Doc.Find(*p.ID); b == nil || b.Text() != "Weekly sync, rewritten & saved" {
		t.Errorf("the export %+v", b)
	}
	if st := a.CanvasSaveState(fakeslack.CanvasShared); st != (CanvasSaveState{}) {
		t.Errorf("state after a save %+v", st)
	}
	// the next save of the same section: a new session sequence, the
	// section's local sequence 2, its saved sequence
	if err := a.SaveCanvas(ctx, fakeslack.CanvasShared, quip.Edit{Kind: quip.EditText, SectionID: *p.ID, RTML: "Weekly sync again"}); err != nil {
		t.Fatal(err)
	}
	rs = saves(fs)
	s2 := rs[len(rs)-1]
	if s2.Form.Get("sequence") != "2" || *s2.Save.Sections[0].LocalSequence != 2 || *s2.Save.Sections[0].Sequence != *after.Sequence {
		t.Errorf("second save %v %+v", s2.Form, s2.Save.Sections[0])
	}
	if fs.CallCount("canvases.edit") != 0 {
		t.Error("canvases.edit was called")
	}
}

func TestCanvasSaveCheckboxAndDelete(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	loadModel(t, a, fakeslack.CanvasShared)
	item := modelSection(t, a, fakeslack.CanvasShared, "Write the changelog")
	if err := a.SaveCanvas(ctx, fakeslack.CanvasShared, quip.Edit{Kind: quip.EditCheck, SectionID: *item.ID, Checked: true}); err != nil {
		t.Fatal(err)
	}
	fc, _ := fs.Canvas(fakeslack.CanvasShared)
	if b := fc.Doc.Find(*item.ID); b == nil || !b.Checked {
		t.Errorf("the item after ticking %+v", b)
	}
	head := modelSection(t, a, fakeslack.CanvasShared, "Weekly sync")
	if err := a.SaveCanvas(ctx, fakeslack.CanvasShared, quip.Edit{Kind: quip.EditDelete, SectionID: *head.ID}); err != nil {
		t.Fatal(err)
	}
	fc, _ = fs.Canvas(fakeslack.CanvasShared)
	if fc.Doc.Find(*head.ID) != nil {
		t.Error("the heading is still in the export")
	}
	rs := saves(fs)
	if len(rs) != 2 || quip.FieldsString(&rs[0].Save.Sections[0]) != "1,2,9,13,16,19,20,33,39" || quip.FieldsString(&rs[1].Save.Sections[0]) != "1,2,3,9,13,19,20,33" {
		t.Errorf("bundles %v / %v", quip.FieldsString(&rs[0].Save.Sections[0]), quip.FieldsString(&rs[1].Save.Sections[0]))
	}
}

// A failed save stays pending and goes again after Slack's backoff, with
// retry_count and new local sequence; two failures in a row are the error
// state (the banner and the lock), and a success clears it.
func TestCanvasSaveRetries(t *testing.T) {
	fastSaves(t)
	saveBackoffBase = time.Hour // each retry goes when the test runs it (retryNow)
	a, fs := canvasApp(t)
	ctx := context.Background()
	loadModel(t, a, fakeslack.CanvasShared)
	p := modelSection(t, a, fakeslack.CanvasShared, "Weekly sync")
	fs.SetFault("canvas/edit-document", fakeslack.Fault{Status: http.StatusServiceUnavailable, Times: 2})
	err := a.SaveCanvas(ctx, fakeslack.CanvasShared, quip.Edit{Kind: quip.EditText, SectionID: *p.ID, RTML: "Weekly sync retried"})
	var he *slack.HTTPError
	if !errors.As(err, &he) || he.Status != http.StatusServiceUnavailable {
		t.Fatalf("first attempt %v", err)
	}
	if st := a.CanvasSaveState(fakeslack.CanvasShared); !st.Pending || st.ReadOnly {
		t.Errorf("after one failure %+v", st)
	}
	retryNow(t, a, fakeslack.CanvasShared)
	if st := a.CanvasSaveState(fakeslack.CanvasShared); !st.ReadOnly || st.Banner != "quipOffline" || st.Reason != "Server not reachable" {
		t.Errorf("the error state %+v", st)
	}
	if err := a.SaveCanvas(ctx, fakeslack.CanvasShared, quip.Edit{Kind: quip.EditText, SectionID: *p.ID, RTML: "typed while locked"}); !errors.Is(err, ErrCanvasReadOnly) {
		t.Errorf("an edit in the error state: %v", err)
	}
	retryNow(t, a, fakeslack.CanvasShared)
	if st := a.CanvasSaveState(fakeslack.CanvasShared); st.Pending || st.ReadOnly {
		t.Errorf("after the third attempt %+v", st)
	}
	rs := saves(fs)
	if len(rs) != 3 {
		t.Fatalf("%d edit-document calls, want 3", len(rs))
	}
	for i, r := range rs {
		if r.Form.Get("retry_count") != []string{"0", "1", "2"}[i] || *r.Save.Sections[0].LocalSequence != int64(i+1) {
			t.Errorf("attempt %d: retry_count %s, local sequence %d", i, r.Form.Get("retry_count"), *r.Save.Sections[0].LocalSequence)
		}
	}
	if after := modelSection(t, a, fakeslack.CanvasShared, "Weekly sync"); quip.SectionRTML(&after) != "Weekly sync retried" {
		t.Errorf("saved %q", quip.SectionRTML(&after))
	}
}

func TestCanvasSaveTooLargeAndForbidden(t *testing.T) {
	fastSaves(t)
	saveBackoffBase = time.Hour // each retry goes when the test runs it (retryNow)
	a, fs := canvasApp(t)
	ctx := context.Background()
	loadModel(t, a, fakeslack.CanvasShared)
	p := modelSection(t, a, fakeslack.CanvasShared, "Weekly sync")
	fs.SetFault("canvas/edit-document", fakeslack.Fault{Status: http.StatusRequestEntityTooLarge, Times: 2})
	_ = a.SaveCanvas(ctx, fakeslack.CanvasShared, quip.Edit{Kind: quip.EditText, SectionID: *p.ID, RTML: "big"})
	retryNow(t, a, fakeslack.CanvasShared)
	if st := a.CanvasSaveState(fakeslack.CanvasShared); !st.ReadOnly || st.Banner != "quipDocumentTooLarge" || st.Reason != "Document too large" {
		t.Errorf("413 twice: %+v", st)
	}
	retryNow(t, a, fakeslack.CanvasShared)
	if st := a.CanvasSaveState(fakeslack.CanvasShared); st.Pending || st.ReadOnly {
		t.Errorf("after the third attempt %+v", st)
	}

	// 403: the error at once, not counted, no lock, no retry
	n := len(saves(fs))
	fs.SetFault("canvas/edit-document", fakeslack.Fault{Status: http.StatusForbidden})
	_ = a.SaveCanvas(ctx, fakeslack.CanvasShared, quip.Edit{Kind: quip.EditText, SectionID: *p.ID, RTML: "forbidden"})
	st := a.CanvasSaveState(fakeslack.CanvasShared)
	if st.Reason != "Thread closed" || st.Banner != "quipOffline" || st.ReadOnly || !st.Pending {
		t.Errorf("403: %+v", st)
	}
	if retryArmed(a, fakeslack.CanvasShared) {
		t.Error("a 403 armed a retry")
	}
	if got := len(saves(fs)); got != n+1 {
		t.Errorf("%d calls after %d, want the 403's", got, n)
	}
	fs.SetFault("canvas/edit-document", fakeslack.Fault{})
}

// A save that Slack's client wouldn't make is refused before anything goes
// out: an empty annotation (getCompactRtml would lose the text after it),
// a list item's delete, a raw tab, a mention or emoji taken out of the text
// with its control section left.
func TestCanvasSaveRefusals(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	loadModel(t, a, fakeslack.CanvasShared)
	p := modelSection(t, a, fakeslack.CanvasShared, "Weekly sync")
	err := a.SaveCanvas(ctx, fakeslack.CanvasShared, quip.Edit{Kind: quip.EditText, SectionID: *p.ID, RTML: `ab<annotation id="z"></annotation>cd`})
	if !errors.Is(err, quip.ErrRefused) || !strings.Contains(err.Error(), "getCompactRtml") {
		t.Errorf("an empty annotation: %v", err)
	}
	item := modelSection(t, a, fakeslack.CanvasShared, "Write the changelog")
	if err := a.SaveCanvas(ctx, fakeslack.CanvasShared, quip.Edit{Kind: quip.EditDelete, SectionID: *item.ID}); !errors.Is(err, quip.ErrRefused) {
		t.Errorf("a list item's delete: %v", err)
	}
	if err := a.SaveCanvas(ctx, fakeslack.CanvasShared, quip.Edit{Kind: quip.EditText, SectionID: *p.ID, RTML: "a\tb"}); !errors.Is(err, quip.ErrRefused) {
		t.Errorf("a raw tab: %v", err)
	}
	// "Agenda for this week, with :tada: and bold text": saved as plain
	// text, the emoji would be taken out, which no save makes yet
	agenda := modelSection(t, a, fakeslack.CanvasShared, "Agenda")
	if len(quip.ControlIDs(quip.SectionRTML(&agenda))) == 0 {
		t.Fatalf("the Agenda paragraph has no control: %q", quip.SectionRTML(&agenda))
	}
	err = a.SaveCanvas(ctx, fakeslack.CanvasShared, quip.Edit{Kind: quip.EditText, SectionID: *agenda.ID, RTML: "Agenda for this week"})
	if !errors.Is(err, quip.ErrRefused) || !strings.Contains(err.Error(), "takes out") {
		t.Errorf("a save without the paragraph's emoji: %v", err)
	}
	if n := len(saves(fs)); n != 0 {
		t.Errorf("%d refused saves went out", n)
	}
	if st := a.CanvasSaveState(fakeslack.CanvasShared); st.Pending {
		t.Errorf("refused edits stayed pending: %+v", st)
	}
}

// Another client's edit lands before the save: the catch-up gives the
// save the newest sequence, so the fake (strict about stale ones) takes
// it; a 409 retries after the backoff and the next catch-up.
func TestCanvasSaveCatchesUp(t *testing.T) {
	fastSaves(t)
	a, fs := canvasApp(t)
	ctx := context.Background()
	loadModel(t, a, fakeslack.CanvasShared)
	p := modelSection(t, a, fakeslack.CanvasShared, "Weekly sync")
	fs.EditCanvasAsOther(fakeslack.CanvasShared, "Someone else's paragraph")
	if err := a.SaveCanvas(ctx, fakeslack.CanvasShared, quip.Edit{Kind: quip.EditText, SectionID: *p.ID, RTML: "Weekly sync after theirs"}); err != nil {
		t.Fatalf("a save after another client's edit: %v", err)
	}
	fs.SetFault("canvas/edit-document", fakeslack.Fault{Status: http.StatusConflict, Times: 1})
	_ = a.SaveCanvas(ctx, fakeslack.CanvasShared, quip.Edit{Kind: quip.EditText, SectionID: *p.ID, RTML: "Weekly sync after a conflict"})
	waitSaveState(t, a, fakeslack.CanvasShared, 5*time.Second, func(s CanvasSaveState) bool { return !s.Pending })
	if after := modelSection(t, a, fakeslack.CanvasShared, "Weekly sync"); quip.SectionRTML(&after) != "Weekly sync after a conflict" {
		t.Errorf("saved %q", quip.SectionRTML(&after))
	}
}

// Edits unsaved for two minutes (here less) show Slack's unresolved banner.
func TestCanvasSaveUnsavedBanner(t *testing.T) {
	fastSaves(t)
	saveBackoffBase = time.Hour // the retry waits; the edit stays unsaved
	a, fs := canvasApp(t)
	ctx := context.Background()
	loadModel(t, a, fakeslack.CanvasShared)
	p := modelSection(t, a, fakeslack.CanvasShared, "Weekly sync")
	fs.SetFault("canvas/edit-document", fakeslack.Fault{Status: http.StatusBadGateway, Times: 1})
	_ = a.SaveCanvas(ctx, fakeslack.CanvasShared, quip.Edit{Kind: quip.EditText, SectionID: *p.ID, RTML: "unsaved"})
	st := waitSaveState(t, a, fakeslack.CanvasShared, 5*time.Second, func(s CanvasSaveState) bool { return s.Banner == "unresolved" })
	if !st.Pending || st.ReadOnly {
		t.Errorf("unsaved too long %+v", st)
	}
}

// The switch is off unless asked for (docs/CANVAS-EDIT-PLAN.md, "Rules for
// round 2"): a fresh App doesn't save through edit-document, ui.json's
// canvas_quip_edit turns it on and off, and the environment's
// CLACK_CANVAS_QUIP_EDIT=1 keeps it on.
func TestCanvasSaveSwitchOff(t *testing.T) {
	if quipEditEnv {
		t.Skip("CLACK_CANVAS_QUIP_EDIT=1 in this test's environment")
	}
	a, _ := canvasApp(t)
	if a.QuipEditOn() {
		t.Fatal("saves go through edit-document by default")
	}
	a.SetQuipEdit(true)
	if !a.QuipEditOn() {
		t.Error("canvas_quip_edit didn't turn it on")
	}
	a.SetQuipEdit(false)
	if a.QuipEditOn() {
		t.Error("canvas_quip_edit didn't turn it off")
	}
	quipEditEnv = true
	t.Cleanup(func() { quipEditEnv = false })
	a.SetQuipEdit(false)
	if !a.QuipEditOn() {
		t.Error("CLACK_CANVAS_QUIP_EDIT=1 didn't keep it on")
	}
}

// Someone else's save of a section Clack has an edit of: the edit isn't
// sent (Slack's editor would merge the two, mergeRtml; Clack can't), their
// text stays, and the save says why. An edit made on top of Clack's own
// save of the section builds on that save, and one from their text goes.
func TestCanvasSaveConflict(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	loadModel(t, a, fakeslack.CanvasShared)
	p := modelSection(t, a, fakeslack.CanvasShared, "Weekly sync")
	if err := a.SaveCanvasText(ctx, fakeslack.CanvasShared, *p.ID, "Weekly sync", "Weekly sync, mine"); err != nil {
		t.Fatalf("an edit from the section's text: %v", err)
	}
	if err := a.SaveCanvasText(ctx, fakeslack.CanvasShared, *p.ID, "Weekly sync, mine", "Weekly sync, mine again"); err != nil {
		t.Fatalf("an edit on top of Clack's own save: %v", err)
	}
	// an edit from text the section no longer holds is refused at once
	if err := a.SaveCanvasText(ctx, fakeslack.CanvasShared, *p.ID, "Weekly sync", "stale"); !errors.Is(err, ErrCanvasConflict) {
		t.Errorf("an edit from stale text: %v", err)
	}
	// someone else saves the heading; Clack's model hasn't heard yet, so
	// the edit is made, and the catch-up before the save finds theirs
	if !fs.ReplaceCanvasBlockAsOther(fakeslack.CanvasShared, *p.ID, "# Weekly sync, theirs") {
		t.Fatal("no heading to replace")
	}
	n := len(saves(fs))
	if err := a.SaveCanvasText(ctx, fakeslack.CanvasShared, *p.ID, "Weekly sync, mine again", "Weekly sync, over theirs"); !errors.Is(err, ErrCanvasConflict) {
		t.Errorf("an edit over someone else's save: %v", err)
	}
	if got := len(saves(fs)); got != n {
		t.Errorf("%d edit-document calls for an edit over someone else's save", got-n)
	}
	fc, _ := fs.Canvas(fakeslack.CanvasShared)
	if b := fc.Doc.Find(*p.ID); b == nil || b.Text() != "Weekly sync, theirs" {
		t.Errorf("their heading after the refused edit: %+v", b)
	}
	if err := a.SaveCanvasDelete(ctx, fakeslack.CanvasShared, *p.ID, "Weekly sync, mine again"); !errors.Is(err, ErrCanvasConflict) {
		t.Errorf("a delete from stale text: %v", err)
	}
	if st := a.CanvasSaveState(fakeslack.CanvasShared); st.Pending {
		t.Errorf("a refused edit stayed pending: %+v", st)
	}
	// from their text: saved
	if err := a.SaveCanvasText(ctx, fakeslack.CanvasShared, *p.ID, "Weekly sync, theirs", "Weekly sync, after theirs"); err != nil {
		t.Errorf("an edit from their text: %v", err)
	}
	fc, _ = fs.Canvas(fakeslack.CanvasShared)
	if b := fc.Doc.Find(*p.ID); b == nil || b.Text() != "Weekly sync, after theirs" {
		t.Errorf("the heading after an edit from their text: %+v", b)
	}
}

// A code block, and a section with formatting, links or mentions, aren't
// saved from plain text: their RTML isn't typed text's.
func TestCanvasSaveTextRefusesRichSections(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	loadModel(t, a, fakeslack.CanvasShared)
	agenda := modelSection(t, a, fakeslack.CanvasShared, "Agenda")
	if err := a.SaveCanvasText(ctx, fakeslack.CanvasShared, *agenda.ID, "Agenda for this week, with :tada: and bold text.", "Agenda"); !errors.Is(err, quip.ErrUnsupported) {
		t.Errorf("a paragraph with an emoji and bold: %v", err)
	}
	fs.EditCanvasAsOther(fakeslack.CanvasShared, "```\nmake test\n```")
	if err := a.fetchCanvasModel(ctx, fakeslack.CanvasShared, a.quipCanvas(fakeslack.CanvasShared)); err != nil {
		t.Fatal(err)
	}
	code := modelSection(t, a, fakeslack.CanvasShared, "make test")
	if err := a.SaveCanvasText(ctx, fakeslack.CanvasShared, *code.ID, "make test", "make lint"); !errors.Is(err, quip.ErrUnsupported) {
		t.Errorf("a one-line code block: %v", err)
	}
	if n := len(saves(fs)); n != 0 {
		t.Errorf("%d edit-document calls", n)
	}
}

// An edit made while a save of the same section is out builds on that
// save once it's answered (its base moves to the saved sequence), so it
// isn't taken for someone else's and goes in the next save.
func TestCanvasSaveTypingDuringSave(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	loadModel(t, a, fakeslack.CanvasShared)
	p := modelSection(t, a, fakeslack.CanvasShared, "Weekly sync")
	before := fs.CallCount("canvas/edit-document")
	fs.SetFault("canvas/edit-document", fakeslack.Fault{ReplyDelay: 700 * time.Millisecond, Times: 1})
	first := make(chan error, 1)
	go func() {
		first <- a.SaveCanvasText(ctx, fakeslack.CanvasShared, *p.ID, "Weekly sync", "Weekly sync, one")
	}()
	deadline := time.Now().Add(5 * time.Second)
	for fs.CallCount("canvas/edit-document") == before {
		if time.Now().After(deadline) {
			t.Fatal("the first save never went out")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := a.SaveCanvasText(ctx, fakeslack.CanvasShared, *p.ID, "Weekly sync, one", "Weekly sync, one two"); err != nil {
		t.Fatalf("an edit while the save is out: %v", err)
	}
	if err := <-first; err != nil {
		t.Fatalf("the first save: %v", err)
	}
	waitSaveState(t, a, fakeslack.CanvasShared, 5*time.Second, func(s CanvasSaveState) bool { return !s.Pending && !s.Saving })
	fc, _ := fs.Canvas(fakeslack.CanvasShared)
	if b := fc.Doc.Find(*p.ID); b == nil || b.Text() != "Weekly sync, one two" {
		t.Errorf("the heading after both edits: %+v", b)
	}
	if n := len(saves(fs)); n != 2 {
		t.Errorf("%d edit-document calls, want 2", n)
	}
}
