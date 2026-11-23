package core

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/quip"
	"github.com/some-du6e/clack/internal/richtext"
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
	saveBackoffBase = 400 * time.Millisecond // long enough to see the error state between attempts
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
	st := waitSaveState(t, a, fakeslack.CanvasShared, 5*time.Second, func(s CanvasSaveState) bool { return s.ReadOnly })
	if st.Banner != "quipOffline" || st.Reason != "Server not reachable" {
		t.Errorf("the error state %+v", st)
	}
	if err := a.SaveCanvas(ctx, fakeslack.CanvasShared, quip.Edit{Kind: quip.EditText, SectionID: *p.ID, RTML: "typed while locked"}); !errors.Is(err, ErrCanvasReadOnly) {
		t.Errorf("an edit in the error state: %v", err)
	}
	waitSaveState(t, a, fakeslack.CanvasShared, 5*time.Second, func(s CanvasSaveState) bool { return !s.Pending && !s.ReadOnly })
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
	saveBackoffBase = 400 * time.Millisecond
	a, fs := canvasApp(t)
	ctx := context.Background()
	loadModel(t, a, fakeslack.CanvasShared)
	p := modelSection(t, a, fakeslack.CanvasShared, "Weekly sync")
	fs.SetFault("canvas/edit-document", fakeslack.Fault{Status: http.StatusRequestEntityTooLarge, Times: 2})
	_ = a.SaveCanvas(ctx, fakeslack.CanvasShared, quip.Edit{Kind: quip.EditText, SectionID: *p.ID, RTML: "big"})
	st := waitSaveState(t, a, fakeslack.CanvasShared, 5*time.Second, func(s CanvasSaveState) bool { return s.ReadOnly })
	if st.Banner != "quipDocumentTooLarge" || st.Reason != "Document too large" {
		t.Errorf("413 twice: %+v", st)
	}
	waitSaveState(t, a, fakeslack.CanvasShared, 5*time.Second, func(s CanvasSaveState) bool { return !s.Pending && !s.ReadOnly })

	// 403: the error at once, not counted, no lock, no retry
	n := len(saves(fs))
	fs.SetFault("canvas/edit-document", fakeslack.Fault{Status: http.StatusForbidden})
	_ = a.SaveCanvas(ctx, fakeslack.CanvasShared, quip.Edit{Kind: quip.EditText, SectionID: *p.ID, RTML: "forbidden"})
	st = a.CanvasSaveState(fakeslack.CanvasShared)
	if st.Reason != "Thread closed" || st.Banner != "quipOffline" || st.ReadOnly || !st.Pending {
		t.Errorf("403: %+v", st)
	}
	time.Sleep(200 * time.Millisecond)
	if got := len(saves(fs)); got != n+1 {
		t.Errorf("a 403 was retried: %d calls after %d", got, n)
	}
	fs.SetFault("canvas/edit-document", fakeslack.Fault{})
}

// A save that Slack's client wouldn't make is refused before anything goes
// out: an empty annotation (getCompactRtml would lose the text after it),
// a list deleted with its items left, a raw tab, a control the section
// doesn't hold.
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
	if err := a.SaveCanvas(ctx, fakeslack.CanvasShared, quip.Edit{Kind: quip.EditDelete, SectionID: item.Parents.IDs[0]}); !errors.Is(err, quip.ErrRefused) {
		t.Errorf("a list deleted with its items left: %v", err)
	}
	if err := a.SaveCanvas(ctx, fakeslack.CanvasShared, quip.Edit{Kind: quip.EditText, SectionID: *p.ID, RTML: "a\tb"}); !errors.Is(err, quip.ErrRefused) {
		t.Errorf("a raw tab: %v", err)
	}
	// the Agenda paragraph's emoji (":tada:") named in the heading: a
	// control the heading doesn't hold, so a new mention, which needs a
	// control section of its own
	agenda := modelSection(t, a, fakeslack.CanvasShared, "Agenda")
	ctls := quip.ControlIDs(quip.SectionRTML(&agenda))
	if len(ctls) == 0 {
		t.Fatalf("the Agenda paragraph has no control: %q", quip.SectionRTML(&agenda))
	}
	err = a.SaveCanvas(ctx, fakeslack.CanvasShared, quip.Edit{Kind: quip.EditText, SectionID: *p.ID, RTML: `Weekly <control id="` + ctls[0] + `"></control>`})
	if !errors.Is(err, quip.ErrRefused) || !strings.Contains(err.Error(), "adds a mention") {
		t.Errorf("another section's emoji in the heading: %v", err)
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
	if err := a.SaveCanvasDelete(ctx, fakeslack.CanvasShared, *p.ID, []richtext.Span{{Kind: richtext.Text, Text: "Weekly sync, mine again"}}); !errors.Is(err, ErrCanvasConflict) {
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

// A block's new content as the editor holds it goes onto its section's
// RTML: the Agenda paragraph ("Agenda for this week, with :tada: and bold
// text.") keeps its emoji where the editor has it and takes new
// formatting; a new mention isn't saved yet; an emoji taken out leaves
// the text (its control section stays, as Slack's editor leaves it); an
// edit begun from other text is a conflict.
func TestCanvasSaveSpans(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	loadModel(t, a, fakeslack.CanvasShared)
	agenda := modelSection(t, a, fakeslack.CanvasShared, "Agenda")
	block := func() *canvas.Block {
		t.Helper()
		fc, _ := fs.Canvas(fakeslack.CanvasShared)
		b := fc.Doc.Find(*agenda.ID)
		if b == nil {
			t.Fatal("no Agenda block")
		}
		return b
	}
	orig := append([]richtext.Span(nil), block().Spans...)
	var emoji richtext.Span
	for _, s := range orig {
		if s.Kind == richtext.Emoji {
			emoji = s
		}
	}
	if emoji.ID == "" {
		t.Fatalf("the Agenda paragraph has no emoji: %+v", orig)
	}
	styled := []richtext.Span{
		{Kind: richtext.Text, Text: "Agenda for "},
		{Kind: richtext.Text, Text: "this week", Style: richtext.Italic},
		{Kind: richtext.Text, Text: ", with "},
		emoji,
		{Kind: richtext.Text, Text: " and "},
		{Kind: richtext.Text, Text: "bold", Style: richtext.Bold},
		{Kind: richtext.Text, Text: " text."},
	}
	if err := a.SaveCanvasSpans(ctx, fakeslack.CanvasShared, *agenda.ID, orig, styled); err != nil {
		t.Fatalf("italics added: %v", err)
	}
	after := modelSection(t, a, fakeslack.CanvasShared, "Agenda")
	if got := quip.SectionRTML(&after); !strings.Contains(got, "<i>this week</i>") || !strings.Contains(got, "<b>bold</b>") ||
		len(quip.ControlIDs(got)) != 1 || quip.ControlIDs(got)[0] != quip.ControlIDs(quip.SectionRTML(&agenda))[0] {
		t.Errorf("the saved RTML %q", got)
	}
	var kinds []richtext.SpanKind
	for _, s := range block().Spans {
		kinds = append(kinds, s.Kind)
	}
	if !slices.Contains(kinds, richtext.Emoji) {
		t.Errorf("the export lost the emoji: %+v", block().Spans)
	}
	// a new mention: its control section isn't made yet
	withUser := append(append([]richtext.Span(nil), styled...), richtext.Span{Kind: richtext.Text, Text: " cc "}, richtext.Span{Kind: richtext.User, ID: "U0FAKE00001"})
	if err := a.SaveCanvasSpans(ctx, fakeslack.CanvasShared, *agenda.ID, styled, withUser); !errors.Is(err, quip.ErrUnsupported) {
		t.Errorf("a new mention: %v", err)
	}
	// the emoji taken out
	bare := []richtext.Span{{Kind: richtext.Text, Text: "Agenda for "}, {Kind: richtext.Text, Text: "this week", Style: richtext.Italic},
		{Kind: richtext.Text, Text: ", with "}, {Kind: richtext.Text, Text: "bold", Style: richtext.Bold}, {Kind: richtext.Text, Text: " text."}}
	if err := a.SaveCanvasSpans(ctx, fakeslack.CanvasShared, *agenda.ID, styled, bare); err != nil {
		t.Fatalf("the emoji taken out: %v", err)
	}
	bareSec := modelSection(t, a, fakeslack.CanvasShared, "Agenda")
	if got := quip.SectionRTML(&bareSec); len(quip.ControlIDs(got)) != 0 {
		t.Errorf("the emoji is still in %q", got)
	}
	// begun from text the section no longer holds
	if err := a.SaveCanvasSpans(ctx, fakeslack.CanvasShared, *agenda.ID, styled, bare); !errors.Is(err, ErrCanvasConflict) {
		t.Errorf("an edit from stale spans: %v", err)
	}
	head := modelSection(t, a, fakeslack.CanvasShared, "Weekly sync")
	txt := func(s string) []richtext.Span { return []richtext.Span{{Kind: richtext.Text, Text: s}} }
	if err := a.SaveCanvasSpans(ctx, fakeslack.CanvasShared, *head.ID, txt("Weekly sync"), txt("Weekly sync, A")); err != nil {
		t.Fatalf("the heading: %v", err)
	}
	if err := a.SaveCanvasSpans(ctx, fakeslack.CanvasShared, *head.ID, txt("Weekly sync"), txt("B")); !errors.Is(err, ErrCanvasConflict) {
		t.Errorf("the heading from its old text: %v", err)
	}
	if after := modelSection(t, a, fakeslack.CanvasShared, "Weekly sync"); quip.SectionRTML(&after) != "Weekly sync, A" {
		t.Errorf("the heading after a stale edit: %q", quip.SectionRTML(&after))
	}
}

// New blocks save as the new sections Slack's editor makes: typed under a
// heading (two in one save, the second after the first), Enter in the
// middle of the heading (its own text cut, a paragraph after it), a new
// checklist item in the list it follows; each one Slack's new-section
// bundle, in the fake's export where it was typed, and a later edit of a
// new block reaches its section. What a new section can't be yet is
// refused before anything goes out: a paragraph after a list item (the
// list's end), a mention in a new block.
func TestCanvasSaveNewBlocks(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	loadModel(t, a, fakeslack.CanvasShared)
	txt := func(s string) []richtext.Span { return []richtext.Span{{Kind: richtext.Text, Text: s}} }
	id := func() string {
		t.Helper()
		n := a.NewCanvasSectionID(fakeslack.CanvasShared)
		if n == "" {
			t.Fatal("no new section id with the model loaded")
		}
		return n
	}
	blocks := func() []canvas.Block { fc, _ := fs.Canvas(fakeslack.CanvasShared); return fc.Doc.Blocks }
	at := func(sectionID string) int {
		for i, b := range blocks() {
			if b.ID == sectionID {
				return i
			}
		}
		return -1
	}
	head := modelSection(t, a, fakeslack.CanvasShared, "Weekly sync")
	p1, h2 := id(), id()
	if p1 == h2 {
		t.Fatal("two new section ids are the same")
	}
	if err := a.SaveCanvasNewBlocks(ctx, fakeslack.CanvasShared, *head.ID, []CanvasNewBlock{
		{ID: p1, Kind: canvas.Paragraph, Spans: txt("First new paragraph")},
		{ID: h2, Kind: canvas.Heading2, Spans: txt("A new heading")},
	}); err != nil {
		t.Fatalf("two new blocks: %v", err)
	}
	rs := saves(fs)
	r := rs[len(rs)-1]
	if len(r.Save.Sections) != 2 || *r.Save.Sections[0].ID != p1 || *r.Save.Sections[1].ID != h2 {
		t.Fatalf("the save's sections %+v", r.Save.Sections)
	}
	for _, sec := range r.Save.Sections {
		if got := quip.FieldsString(&sec); got != "1,2,3,8,9,10,11,12,13,16,19,20,21,33,35,39" {
			t.Errorf("new section %s: fields %s", *sec.ID, got)
		}
	}
	if i := at(*head.ID); i < 0 || at(p1) != i+1 || at(h2) != i+2 || blocks()[i+2].Kind != canvas.Heading2 {
		t.Errorf("the export after two new blocks: %+v", blocks())
	}
	// the new paragraph typed into again
	if err := a.SaveCanvasSpans(ctx, fakeslack.CanvasShared, p1, txt("First new paragraph"), txt("First new paragraph, again")); err != nil {
		t.Errorf("an edit of a new block: %v", err)
	}
	if i := at(p1); i < 0 || blocks()[i].Text() != "First new paragraph, again" {
		t.Errorf("the new paragraph after an edit: %+v", blocks())
	}
	// Enter in the middle of the heading
	tail := id()
	if err := a.SaveCanvasSplit(ctx, fakeslack.CanvasShared, *head.ID, txt("Weekly sync"), txt("Weekly"),
		[]CanvasNewBlock{{ID: tail, Kind: canvas.Paragraph, Spans: txt("sync")}}); err != nil {
		t.Fatalf("a split: %v", err)
	}
	if i := at(*head.ID); i < 0 || blocks()[i].Text() != "Weekly" || at(tail) != i+1 || blocks()[i+1].Text() != "sync" {
		t.Errorf("the export after a split: %+v", blocks())
	}
	// a new checklist item after "Write the changelog", in its list
	item := modelSection(t, a, fakeslack.CanvasShared, "Write the changelog")
	notes := id()
	if err := a.SaveCanvasNewBlocks(ctx, fakeslack.CanvasShared, *item.ID, []CanvasNewBlock{{ID: notes, Kind: canvas.Check, Spans: txt("Write the release notes")}}); err != nil {
		t.Fatalf("a new checklist item: %v", err)
	}
	if i := at(*item.ID); i < 0 || at(notes) != i+1 || blocks()[i+1].Kind != canvas.Check || blocks()[i+1].Checked {
		t.Errorf("the export after a new checklist item: %+v", blocks())
	}
	n := len(saves(fs))
	if err := a.SaveCanvasNewBlocks(ctx, fakeslack.CanvasShared, *item.ID, []CanvasNewBlock{{ID: id(), Kind: canvas.Paragraph, Spans: txt("x")}}); !errors.Is(err, quip.ErrUnsupported) {
		t.Errorf("a paragraph after a list item: %v", err)
	}
	if err := a.SaveCanvasNewBlocks(ctx, fakeslack.CanvasShared, *head.ID, []CanvasNewBlock{{ID: id(), Kind: canvas.Paragraph,
		Spans: []richtext.Span{{Kind: richtext.Text, Text: "cc "}, {Kind: richtext.User, ID: "U0FAKE00001"}}}}); !errors.Is(err, quip.ErrUnsupported) {
		t.Errorf("a mention in a new block: %v", err)
	}
	if got := len(saves(fs)); got != n {
		t.Errorf("%d edit-document calls for refused new blocks", got-n)
	}
}

// A block's style, level and box save as Slack's setters mark them: the
// heading made a heading 2 (setSectionStyle: STYLE alone, no content),
// then a paragraph with new text (STYLE and CONTENT), a nested checklist
// item outdented (setSectionAttribute: its attributes at the new level),
// an item ticked with its text unchanged (a check edit). A block moved
// into a list, or a list made another kind, isn't saved yet.
func TestCanvasSaveRestyle(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	loadModel(t, a, fakeslack.CanvasShared)
	txt := func(s string) []richtext.Span { return []richtext.Span{{Kind: richtext.Text, Text: s}} }
	block := func(id string) *canvas.Block { fc, _ := fs.Canvas(fakeslack.CanvasShared); return fc.Doc.Find(id) }
	last := func() *quip.Section { rs := saves(fs); return &rs[len(rs)-1].Save.Sections[0] }
	head := modelSection(t, a, fakeslack.CanvasShared, "Weekly sync")
	if err := a.SaveCanvasBlock(ctx, fakeslack.CanvasShared, CanvasBlockEdit{SectionID: *head.ID, Orig: txt("Weekly sync"), OrigKind: canvas.Heading1,
		Spans: txt("Weekly sync"), Kind: canvas.Heading2}); err != nil {
		t.Fatalf("H1 to H2: %v", err)
	}
	if got := quip.FieldsString(last()); got != "1,2,9,10,13,19,20,33" || *last().Style != quip.StyleH2 {
		t.Errorf("H1 to H2: fields %s", got)
	}
	if b := block(*head.ID); b == nil || b.Kind != canvas.Heading2 || b.Text() != "Weekly sync" {
		t.Errorf("the export after H1 to H2: %+v", b)
	}
	if err := a.SaveCanvasBlock(ctx, fakeslack.CanvasShared, CanvasBlockEdit{SectionID: *head.ID, Orig: txt("Weekly sync"), OrigKind: canvas.Heading2,
		Spans: txt("Weekly sync, now a paragraph"), Kind: canvas.Paragraph}); err != nil {
		t.Fatalf("H2 to a paragraph with new text: %v", err)
	}
	if got := quip.FieldsString(last()); got != "1,2,9,10,12,13,19,20,33" {
		t.Errorf("H2 to a paragraph with new text: fields %s", got)
	}
	if b := block(*head.ID); b == nil || b.Kind != canvas.Paragraph || b.Text() != "Weekly sync, now a paragraph" {
		t.Errorf("the export after a restyle with text: %+v", b)
	}
	shots := modelSection(t, a, fakeslack.CanvasShared, "Screenshots")
	if err := a.SaveCanvasBlock(ctx, fakeslack.CanvasShared, CanvasBlockEdit{SectionID: *shots.ID, Orig: txt("Screenshots"), OrigKind: canvas.Check, OrigLevel: 1,
		Spans: txt("Screenshots"), Kind: canvas.Check, Level: 0}); err != nil {
		t.Fatalf("an item outdented: %v", err)
	}
	if got := quip.FieldsString(last()); got != "1,2,9,13,16,19,20,33" || *last().Attributes.Indentation != 0 {
		t.Errorf("an item outdented: fields %s", got)
	}
	if b := block(*shots.ID); b == nil || b.Level != 0 {
		t.Errorf("the export after an outdent: %+v", b)
	}
	item := modelSection(t, a, fakeslack.CanvasShared, "Write the changelog")
	if err := a.SaveCanvasBlock(ctx, fakeslack.CanvasShared, CanvasBlockEdit{SectionID: *item.ID, Orig: txt("Write the changelog"), OrigKind: canvas.Check,
		Spans: txt("Write the changelog"), Kind: canvas.Check, Checked: true}); err != nil {
		t.Fatalf("an item ticked: %v", err)
	}
	if got := quip.FieldsString(last()); !strings.HasPrefix(got, "1,2,9,13,16,19,20,33") {
		t.Errorf("an item ticked: fields %s", got)
	}
	if b := block(*item.ID); b == nil || !b.Checked {
		t.Errorf("the export after a tick: %+v", b)
	}
	n := len(saves(fs))
	if err := a.SaveCanvasBlock(ctx, fakeslack.CanvasShared, CanvasBlockEdit{SectionID: *shots.ID, Orig: txt("Screenshots"), OrigKind: canvas.Check,
		Spans: txt("Screenshots"), Kind: canvas.Check, Level: quip.MaxListLevel + 1}); !errors.Is(err, quip.ErrUnsupported) {
		t.Errorf("an item past the canvas editor's deepest level: %v", err)
	}
	if err := a.SaveCanvas(ctx, fakeslack.CanvasShared, quip.Edit{Kind: quip.EditBlock, SectionID: *shots.ID, Reindent: true, Indent: quip.MaxListLevel + 1}); !errors.Is(err, quip.ErrRefused) {
		t.Errorf("the validator took list level %d: %v", quip.MaxListLevel+1, err)
	}
	if err := a.SaveCanvasBlock(ctx, fakeslack.CanvasShared, CanvasBlockEdit{SectionID: *head.ID, Orig: txt("Weekly sync, now a paragraph"), OrigKind: canvas.Paragraph,
		Spans: txt("Weekly sync, now a paragraph"), Kind: canvas.Bullet}); !errors.Is(err, quip.ErrUnsupported) {
		t.Errorf("a paragraph made a bullet: %v", err)
	}
	if err := a.SaveCanvasBlock(ctx, fakeslack.CanvasShared, CanvasBlockEdit{SectionID: *item.ID, Orig: txt("Write the changelog"), OrigKind: canvas.Check, OrigChecked: true,
		Spans: txt("Write the changelog"), Kind: canvas.Numbered}); !errors.Is(err, quip.ErrUnsupported) {
		t.Errorf("a checklist item made a numbered one: %v", err)
	}
	// nothing changed: nothing sent
	if err := a.SaveCanvasBlock(ctx, fakeslack.CanvasShared, CanvasBlockEdit{SectionID: *item.ID, Orig: txt("Write the changelog"), OrigKind: canvas.Check, OrigChecked: true,
		Spans: txt("Write the changelog"), Kind: canvas.Check, Checked: true}); err != nil {
		t.Errorf("an edit that changes nothing: %v", err)
	}
	if got := len(saves(fs)); got != n {
		t.Errorf("%d edit-document calls for refused or empty edits", got-n)
	}
}

// A paragraph between two lists of a style isn't deleted: Slack's editor
// would merge the lists it leaves side by side (deleteSection's
// maybeMergeAdjacentSections), which Clack doesn't save yet. One after a
// list, with no list after it, is.
func TestCanvasSaveDeleteMerges(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	const id = "F0MERGE0001"
	txt := func(s string) []richtext.Span { return []richtext.Span{{Kind: richtext.Text, Text: s}} }
	fs.AddCanvas(fakeslack.FakeCanvas{ID: id, Title: "Merges", Doc: canvas.Doc{Blocks: []canvas.Block{
		{ID: "temp:C:MRG00001", Kind: canvas.Bullet, Spans: txt("one")},
		{ID: "temp:C:MRG00002", Kind: canvas.Paragraph, Spans: txt("between")},
		{ID: "temp:C:MRG00003", Kind: canvas.Bullet, Spans: txt("two")},
		{ID: "temp:C:MRG00004", Kind: canvas.Paragraph, Spans: txt("tail")},
	}}})
	loadModel(t, a, id)
	if err := a.SaveCanvasDelete(ctx, id, "temp:C:MRG00002", txt("between")); !errors.Is(err, quip.ErrUnsupported) {
		t.Errorf("a paragraph between two bullet lists: %v", err)
	}
	if err := a.SaveCanvas(ctx, id, quip.Edit{Kind: quip.EditDelete, SectionID: "temp:C:MRG00002"}); !errors.Is(err, quip.ErrRefused) {
		t.Errorf("the validator took the delete between two lists: %v", err)
	}
	if n := len(saves(fs)); n != 0 {
		t.Errorf("%d edit-document calls for a refused delete", n)
	}
	if err := a.SaveCanvasDelete(ctx, id, "temp:C:MRG00004", txt("tail")); err != nil {
		t.Errorf("a paragraph after the last list: %v", err)
	}
	fc, _ := fs.Canvas(id)
	if fc.Doc.Find("temp:C:MRG00004") != nil || fc.Doc.Find("temp:C:MRG00002") == nil {
		t.Errorf("the export after the deletes: %+v", fc.Doc.Blocks)
	}
}

// A list item is deleted alone while its list keeps items; the last one
// takes its list with it in the same save (notifyDescendantDeleted_).
func TestCanvasSaveListDeletes(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	const id = "F0QUIPDEL01" // not F0LIST…: the fake keeps those for Slack lists
	txt := func(s string) []richtext.Span { return []richtext.Span{{Kind: richtext.Text, Text: s}} }
	fs.AddCanvas(fakeslack.FakeCanvas{ID: id, Title: "List deletes", Doc: canvas.Doc{Blocks: []canvas.Block{
		{ID: "temp:C:LDL00001", Kind: canvas.Paragraph, Spans: txt("before")},
		{ID: "temp:C:LDL00002", Kind: canvas.Bullet, Spans: txt("one")},
		{ID: "temp:C:LDL00003", Kind: canvas.Bullet, Spans: txt("two")},
		{ID: "temp:C:LDL00004", Kind: canvas.Paragraph, Spans: txt("after")},
	}}})
	loadModel(t, a, id)
	if err := a.SaveCanvasDelete(ctx, id, "temp:C:LDL00002", txt("one")); err != nil {
		t.Fatalf("an item of two: %v", err)
	}
	rs := saves(fs)
	if len(rs) != 1 || len(rs[0].Save.Sections) != 1 || quip.FieldsString(&rs[0].Save.Sections[0]) != "1,2,3,9,13,19,20,33" {
		t.Errorf("an item of two: %+v", rs)
	}
	if err := a.SaveCanvas(ctx, id, quip.Edit{Kind: quip.EditDelete, SectionID: "temp:C:LDL00003"}); !errors.Is(err, quip.ErrRefused) {
		t.Errorf("the validator took the last item deleted without its list: %v", err)
	}
	if err := a.SaveCanvasDelete(ctx, id, "temp:C:LDL00003", txt("two")); err != nil {
		t.Fatalf("the last item: %v", err)
	}
	rs = saves(fs)
	r := rs[len(rs)-1]
	if len(r.Save.Sections) != 2 {
		t.Fatalf("the last item's save: %d sections, want the item and its list", len(r.Save.Sections))
	}
	var types []int32
	for _, sec := range r.Save.Sections {
		types = append(types, *sec.Type)
		if sec.Deleted == nil || !*sec.Deleted {
			t.Errorf("section %s isn't deleted", *sec.ID)
		}
	}
	if !slices.Contains(types, quip.TypeList) || !slices.Contains(types, quip.TypeText) {
		t.Errorf("the last item's save holds types %v", types)
	}
	fc, _ := fs.Canvas(id)
	for _, b := range fc.Doc.Blocks {
		if b.Kind.IsList() {
			t.Errorf("a list is left: %+v", fc.Doc.Blocks)
		}
	}
}

// A code block's text saves as Slack's save writer writes the code DOM:
// the lines with line breaks between them, a tab as a space, two spaces as
// two (canvases are pre-wrap), the language kept; from other text it's a
// conflict.
func TestCanvasSaveCode(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	loadModel(t, a, fakeslack.CanvasShared)
	fs.EditCanvasAsOther(fakeslack.CanvasShared, "```\nmake test\n```")
	if err := a.fetchCanvasModel(ctx, fakeslack.CanvasShared, a.quipCanvas(fakeslack.CanvasShared)); err != nil {
		t.Fatal(err)
	}
	code := modelSection(t, a, fakeslack.CanvasShared, "make test")
	if err := a.SaveCanvasCode(ctx, fakeslack.CanvasShared, *code.ID, "make test", "make lint\n\tindented  twice"); err != nil {
		t.Fatalf("a code block's text: %v", err)
	}
	after := modelSection(t, a, fakeslack.CanvasShared, "make lint")
	if got := quip.SectionRTML(&after); got != "make lint<br> indented  twice" {
		t.Errorf("the code block's RTML %q", got)
	}
	fc, _ := fs.Canvas(fakeslack.CanvasShared)
	if b := fc.Doc.Find(*code.ID); b == nil || b.Kind != canvas.Code || b.Code != "make lint\n indented  twice" {
		t.Errorf("the export's code block %+v", b)
	}
	if err := a.SaveCanvasCode(ctx, fakeslack.CanvasShared, *code.ID, "make test", "x"); !errors.Is(err, ErrCanvasConflict) {
		t.Errorf("a code edit from old text: %v", err)
	}
}

// A quote's paragraph (a section of style 16) saves its own text like any
// paragraph, and stays in the quote.
func TestCanvasSaveQuoteParagraph(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	loadModel(t, a, fakeslack.CanvasShared)
	txt := func(s string) []richtext.Span { return []richtext.Span{{Kind: richtext.Text, Text: s}} }
	q := modelSection(t, a, fakeslack.CanvasShared, "Keep it short.")
	if q.Style == nil || *q.Style != quip.StyleBlockquote {
		t.Fatalf("the quote's paragraph has style %v", q.Style)
	}
	if err := a.SaveCanvasBlock(ctx, fakeslack.CanvasShared, CanvasBlockEdit{SectionID: *q.ID, Orig: txt("Keep it short."), OrigKind: canvas.Paragraph,
		Spans: txt("Keep it short, please."), Kind: canvas.Paragraph}); err != nil {
		t.Fatalf("a quote's paragraph: %v", err)
	}
	after := modelSection(t, a, fakeslack.CanvasShared, "Keep it short, please.")
	if after.Style == nil || *after.Style != quip.StyleBlockquote {
		t.Errorf("the paragraph left the quote: style %v", after.Style)
	}
	if rs := saves(fs); len(rs) != 1 || quip.FieldsString(&rs[0].Save.Sections[0]) != "1,2,9,12,13,19,20,33" {
		t.Errorf("the save %+v", rs)
	}
	if err := a.SaveCanvasBlock(ctx, fakeslack.CanvasShared, CanvasBlockEdit{SectionID: *q.ID, Orig: txt("Keep it short, please."), OrigKind: canvas.Paragraph,
		Spans: txt("Keep it short, please."), Kind: canvas.Heading1}); !errors.Is(err, quip.ErrUnsupported) {
		t.Errorf("a quote's paragraph made a heading: %v", err)
	}
	if err := a.SaveCanvas(ctx, fakeslack.CanvasShared, quip.Edit{Kind: quip.EditBlock, SectionID: *q.ID, Restyle: true, Style: quip.StyleH1}); !errors.Is(err, quip.ErrRefused) {
		t.Errorf("the validator took a quote's paragraph made a heading: %v", err)
	}
}

// The canvas's last block isn't deleted: Slack's deleteEntity would make
// a new section in its place (a canvas hides its title, so the title is
// no sibling). Emptied, it stays, its text deleted, as Slack's editor
// leaves a section whose text goes: a paragraph, or the last item of the
// only list. The validator refuses its delete on its own; with a new
// block after it, the paragraph goes.
func TestCanvasSaveLastBlock(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	txt := func(s string) []richtext.Span { return []richtext.Span{{Kind: richtext.Text, Text: s}} }
	const id, list = "F0QUIPLAST1", "F0QUIPLAST2"
	fs.AddCanvas(fakeslack.FakeCanvas{ID: id, Title: "Last block", Doc: canvas.Doc{Blocks: []canvas.Block{
		{ID: "temp:C:LST00001", Kind: canvas.Paragraph, Spans: txt("only")},
	}}})
	fs.AddCanvas(fakeslack.FakeCanvas{ID: list, Title: "Last list", Doc: canvas.Doc{Blocks: []canvas.Block{
		{ID: "temp:C:LST00002", Kind: canvas.Bullet, Spans: txt("one")},
	}}})
	loadModel(t, a, id)
	loadModel(t, a, list)
	if err := a.SaveCanvas(ctx, id, quip.Edit{Kind: quip.EditDelete, SectionID: "temp:C:LST00001"}); !errors.Is(err, quip.ErrRefused) {
		t.Errorf("the validator took the last block's delete: %v", err)
	}
	if n := len(saves(fs)); n != 0 {
		t.Errorf("%d edit-document calls for a refused delete", n)
	}
	emptied := func(canvasID, sectionID, was string, kind canvas.Kind) { // its text alone, with the activity row for text taken out
		t.Helper()
		if err := a.SaveCanvasDelete(ctx, canvasID, sectionID, txt(was)); err != nil {
			t.Fatalf("%s emptied: %v", was, err)
		}
		rs := saves(fs)
		r := rs[len(rs)-1]
		if len(r.Save.Sections) != 1 || quip.FieldsString(&r.Save.Sections[0]) != "1,2,9,12,13,19,20,33,39" || quip.SectionRTML(&r.Save.Sections[0]) != "" {
			t.Errorf("%s emptied: %+v", was, r.Save.Sections)
		}
		fc, _ := fs.Canvas(canvasID)
		if len(fc.Doc.Blocks) != 1 || fc.Doc.Blocks[0].Kind != kind || fc.Doc.Blocks[0].Text() != "" || fc.Doc.Blocks[0].ID != sectionID {
			t.Errorf("%s emptied: the export %+v", was, fc.Doc.Blocks)
		}
	}
	emptied(id, "temp:C:LST00001", "only", canvas.Paragraph)
	emptied(list, "temp:C:LST00002", "one", canvas.Bullet)
	nid := a.NewCanvasSectionID(id)
	if err := a.SaveCanvasNewBlocks(ctx, id, "temp:C:LST00001", []CanvasNewBlock{{ID: nid, Kind: canvas.Paragraph, Spans: txt("next")}}); err != nil {
		t.Fatalf("a new block: %v", err)
	}
	if err := a.SaveCanvasDelete(ctx, id, "temp:C:LST00001", nil); err != nil {
		t.Errorf("a paragraph with a block after it: %v", err)
	}
	fc, _ := fs.Canvas(id)
	if len(fc.Doc.Blocks) != 1 || fc.Doc.Blocks[0].Text() != "next" {
		t.Errorf("the export after the delete: %+v", fc.Doc.Blocks)
	}
}

// A list's last item emptied while a new item after it waits to be saved
// (another save is out): the list stays, as the new item keeps it, and the
// delete goes with the new item in one save the validator takes.
func TestCanvasSaveListItemReplaced(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	txt := func(s string) []richtext.Span { return []richtext.Span{{Kind: richtext.Text, Text: s}} }
	const id = "F0QUIPLAST3"
	fs.AddCanvas(fakeslack.FakeCanvas{ID: id, Title: "Replaced item", Doc: canvas.Doc{Blocks: []canvas.Block{
		{ID: "temp:C:RPL00001", Kind: canvas.Paragraph, Spans: txt("before")},
		{ID: "temp:C:RPL00002", Kind: canvas.Bullet, Spans: txt("one")},
	}}})
	loadModel(t, a, id)
	// never the list deleted in a save that adds an item to it (a canvas
	// of its own, so the rest holds whatever this one does)
	const other = "F0QUIPLAST4"
	fs.AddCanvas(fakeslack.FakeCanvas{ID: other, Title: "Deleted list", Doc: canvas.Doc{Blocks: []canvas.Block{
		{ID: "temp:C:RPL00003", Kind: canvas.Paragraph, Spans: txt("before")},
		{ID: "temp:C:RPL00004", Kind: canvas.Bullet, Spans: txt("one")},
	}}})
	loadModel(t, a, other)
	x := modelSection(t, a, other, "one")
	var pos string
	a.CanvasModel(other, func(d *quip.Doc) { pos = d.PositionAfter(*x.Position, nil) })
	err := a.SaveCanvas(ctx, other, quip.Edit{Kind: quip.EditDelete, SectionID: *x.ID}, quip.Edit{Kind: quip.EditDelete, SectionID: x.Parents.IDs[0]},
		quip.Edit{Kind: quip.EditNew, SectionID: a.NewCanvasSectionID(other), After: *x.ID, Style: quip.StylePlain, Position: pos, RTML: "two"})
	if !errors.Is(err, quip.ErrRefused) || !strings.Contains(err.Error(), "adds an item") {
		t.Errorf("a list deleted in the save that adds an item to it: %v", err)
	}
	before := fs.CallCount("canvas/edit-document")
	fs.SetFault("canvas/edit-document", fakeslack.Fault{ReplyDelay: 700 * time.Millisecond, Times: 1})
	first := make(chan error, 1)
	go func() {
		first <- a.SaveCanvasSpans(ctx, id, "temp:C:RPL00001", txt("before"), txt("before, typed"))
	}()
	deadline := time.Now().Add(5 * time.Second)
	for fs.CallCount("canvas/edit-document") == before {
		if time.Now().After(deadline) {
			t.Fatal("the first save never went out")
		}
		time.Sleep(5 * time.Millisecond)
	}
	nid := a.NewCanvasSectionID(id)
	if err := a.SaveCanvasNewBlocks(ctx, id, "temp:C:RPL00002", []CanvasNewBlock{{ID: nid, Kind: canvas.Bullet, Spans: txt("two")}}); err != nil {
		t.Fatalf("a new item while a save is out: %v", err)
	}
	if err := a.SaveCanvasDelete(ctx, id, "temp:C:RPL00002", txt("one")); err != nil {
		t.Fatalf("the old item emptied: %v", err)
	}
	if err := <-first; err != nil {
		t.Fatalf("the first save: %v", err)
	}
	waitSaveState(t, a, id, 5*time.Second, func(s CanvasSaveState) bool { return !s.Pending && !s.Saving })
	rs := saves(fs)
	if len(rs) != 2 || len(rs[1].Save.Sections) != 2 || rs[1].Status != http.StatusOK {
		t.Fatalf("%d saves, want the paragraph's, then the delete with the new item: %+v", len(rs), rs)
	}
	fc, _ := fs.Canvas(id)
	if len(fc.Doc.Blocks) != 2 || fc.Doc.Blocks[1].Kind != canvas.Bullet || fc.Doc.Blocks[1].Text() != "two" {
		t.Errorf("the export: %+v", fc.Doc.Blocks)
	}
}
