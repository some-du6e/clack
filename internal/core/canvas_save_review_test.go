package core

// The B2 review's blockers (wf_da55700d-6e0): saves that dropped an edit
// without a word, a tick and typing that replaced each other, a panic, a
// save on stale state and a lock that never lifted.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/quip"
)

// An edit made while the canvas's model can't be loaded waits for it (the
// load is part of the attempt, counted and retried like a failed save):
// the caller hears it's pending, and it saves once a load succeeds.
func TestCanvasSaveLoadFailureWaits(t *testing.T) {
	fastSaves(t)
	a, fs := canvasApp(t)
	ctx := context.Background()
	loadModel(t, a, fakeslack.CanvasShared) // the fake gives its sections their ids
	head := modelSection(t, a, fakeslack.CanvasShared, "Weekly sync")
	a.dropCanvasModel(fakeslack.CanvasShared) // as when the canvas's last view closes: core holds no model
	fs.SetFault("canvas/load-data", fakeslack.Fault{Status: http.StatusServiceUnavailable, Times: 1})
	err := a.SaveCanvasText(ctx, fakeslack.CanvasShared, *head.ID, "Weekly sync", "Weekly sync, after a failed load")
	if !errors.Is(err, ErrCanvasSavePending) {
		t.Errorf("an edit whose model load failed: %v", err)
	}
	if st := a.CanvasSaveState(fakeslack.CanvasShared); !st.Pending {
		t.Errorf("the edit isn't pending: %+v", st)
	}
	waitSaveState(t, a, fakeslack.CanvasShared, 5*time.Second, func(s CanvasSaveState) bool { return !s.Pending })
	fc, _ := fs.Canvas(fakeslack.CanvasShared)
	if b := fc.Doc.Find(*head.ID); b == nil || b.Text() != "Weekly sync, after a failed load" {
		t.Errorf("after the retry: %+v", b)
	}
}

// A conflict found on a retry (someone else saved the section after the
// first attempt failed) drops the edit with a notice for the view.
func TestCanvasSaveRetryConflictNotice(t *testing.T) {
	fastSaves(t)
	saveBackoffBase = 300 * time.Millisecond // the other save lands before the retry
	a, fs := canvasApp(t)
	ctx := context.Background()
	loadModel(t, a, fakeslack.CanvasShared)
	head := modelSection(t, a, fakeslack.CanvasShared, "Weekly sync")
	fs.SetFault("canvas/edit-document", fakeslack.Fault{Status: http.StatusServiceUnavailable, Times: 1})
	if err := a.SaveCanvasText(ctx, fakeslack.CanvasShared, *head.ID, "Weekly sync", "Weekly sync, A"); err == nil {
		t.Fatal("the first attempt went through")
	}
	if !fs.ReplaceCanvasBlockAsOther(fakeslack.CanvasShared, *head.ID, "# Weekly sync, B") {
		t.Fatal("no heading to replace")
	}
	waitSaveState(t, a, fakeslack.CanvasShared, 5*time.Second, func(s CanvasSaveState) bool { return !s.Pending })
	if ns := a.TakeCanvasSaveNotices(fakeslack.CanvasShared); len(ns) != 1 || !ns[0].Conflict {
		t.Errorf("notices %+v, want the conflict", ns)
	}
	fc, _ := fs.Canvas(fakeslack.CanvasShared)
	if b := fc.Doc.Find(*head.ID); b == nil || b.Text() != "Weekly sync, B" {
		t.Errorf("B's heading %+v", b)
	}
}

// A conflict found on an attempt that waited behind another save (its
// caller heard nil) is a notice too.
func TestCanvasSaveDeferredConflictNotice(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	loadModel(t, a, fakeslack.CanvasShared)
	head := modelSection(t, a, fakeslack.CanvasShared, "Weekly sync")
	item := modelSection(t, a, fakeslack.CanvasShared, "Write the changelog")
	before := fs.CallCount("canvas/edit-document")
	fs.SetFault("canvas/edit-document", fakeslack.Fault{ReplyDelay: 700 * time.Millisecond, Times: 1})
	first := make(chan error, 1)
	go func() {
		first <- a.SaveCanvasText(ctx, fakeslack.CanvasShared, *head.ID, "Weekly sync", "Weekly sync, one")
	}()
	deadline := time.Now().Add(5 * time.Second)
	for fs.CallCount("canvas/edit-document") == before {
		if time.Now().After(deadline) {
			t.Fatal("the first save never went out")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := a.SaveCanvasText(ctx, fakeslack.CanvasShared, *item.ID, "Write the changelog", "Write the changelog, A"); err != nil {
		t.Fatalf("an edit while a save is out: %v", err)
	}
	if !fs.ReplaceCanvasBlockAsOther(fakeslack.CanvasShared, *item.ID, "- [ ] Write the changelog, B") {
		t.Fatal("no item to replace")
	}
	if err := <-first; err != nil {
		t.Fatalf("the first save: %v", err)
	}
	waitSaveState(t, a, fakeslack.CanvasShared, 5*time.Second, func(s CanvasSaveState) bool { return !s.Pending && !s.Saving })
	if ns := a.TakeCanvasSaveNotices(fakeslack.CanvasShared); len(ns) != 1 || !ns[0].Conflict {
		t.Errorf("notices %+v, want the conflict", ns)
	}
}

// A tick and typing in the same item before a save are one edit, as
// Slack's dirty fields hold them: one bundle section with the text and
// the box, in either order.
func TestCanvasSaveTickAndText(t *testing.T) {
	fastSaves(t)
	saveBackoffBase = 300 * time.Millisecond // the second edit comes before the retry
	a, fs := canvasApp(t)
	ctx := context.Background()
	loadModel(t, a, fakeslack.CanvasShared)
	item := modelSection(t, a, fakeslack.CanvasShared, "Write the changelog")
	block := func() *canvas.Block { fc, _ := fs.Canvas(fakeslack.CanvasShared); return fc.Doc.Find(*item.ID) }

	// typing whose save failed once, then a tick
	fs.SetFault("canvas/edit-document", fakeslack.Fault{Status: http.StatusServiceUnavailable, Times: 1})
	if err := a.SaveCanvasText(ctx, fakeslack.CanvasShared, *item.ID, "Write the changelog", "Write the changelog today"); err == nil {
		t.Fatal("the typing's first attempt went through")
	}
	if err := a.SaveCanvas(ctx, fakeslack.CanvasShared, quip.Edit{Kind: quip.EditCheck, SectionID: *item.ID, Checked: true}); err != nil {
		t.Fatalf("the tick: %v", err)
	}
	waitSaveState(t, a, fakeslack.CanvasShared, 5*time.Second, func(s CanvasSaveState) bool { return !s.Pending })
	if b := block(); b == nil || b.Text() != "Write the changelog today" || !b.Checked {
		t.Errorf("after typing and a tick: %+v", b)
	}
	rs := saves(fs)
	if r := rs[len(rs)-1]; len(r.Save.Sections) != 1 || !strings.HasPrefix(quip.FieldsString(&r.Save.Sections[0]), "1,2,9,12,13,16,19,20,33") {
		t.Errorf("the save's sections: %d, fields %s", len(r.Save.Sections), quip.FieldsString(&r.Save.Sections[0]))
	}

	// an untick whose save failed once, then typing
	fs.SetFault("canvas/edit-document", fakeslack.Fault{Status: http.StatusServiceUnavailable, Times: 1})
	if err := a.SaveCanvas(ctx, fakeslack.CanvasShared, quip.Edit{Kind: quip.EditCheck, SectionID: *item.ID, Checked: false}); err == nil {
		t.Fatal("the untick's first attempt went through")
	}
	if err := a.SaveCanvasText(ctx, fakeslack.CanvasShared, *item.ID, "Write the changelog today", "Write the changelog tomorrow"); err != nil {
		t.Fatalf("the typing: %v", err)
	}
	waitSaveState(t, a, fakeslack.CanvasShared, 5*time.Second, func(s CanvasSaveState) bool { return !s.Pending })
	if b := block(); b == nil || b.Text() != "Write the changelog tomorrow" || b.Checked {
		t.Errorf("after an untick and typing: %+v", b)
	}
}

// CanvasTitleSection doesn't panic when the canvas's model state goes
// while it loads (the canvas's last view closed): it has no title section
// to give.
func TestCanvasSaveTitleSectionModelGone(t *testing.T) {
	a, fs := canvasApp(t)
	before := fs.CallCount("canvas/load-data")
	fs.SetFault("canvas/load-data", fakeslack.Fault{ReplyDelay: 400 * time.Millisecond, Times: 1})
	go func() {
		deadline := time.Now().Add(5 * time.Second)
		for fs.CallCount("canvas/load-data") == before && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		a.dropCanvasModel(fakeslack.CanvasShared)
	}()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("CanvasTitleSection panicked: %v", r)
		}
	}()
	if _, err := a.CanvasTitleSection(context.Background(), fakeslack.CanvasShared); err == nil {
		t.Error("a title section from a model that went")
	}
}

// A failed catch-up stops the save (never on stale state): the attempt
// fails, and the save goes after the backoff, caught up.
func TestCanvasSaveCatchUpFailureStops(t *testing.T) {
	fastSaves(t)
	a, fs := canvasApp(t)
	ctx := context.Background()
	loadModel(t, a, fakeslack.CanvasShared)
	head := modelSection(t, a, fakeslack.CanvasShared, "Weekly sync")
	fs.SetFault("canvas/fetch-latest", fakeslack.Fault{Status: http.StatusServiceUnavailable, Times: 1})
	if err := a.SaveCanvasText(ctx, fakeslack.CanvasShared, *head.ID, "Weekly sync", "Weekly sync, caught up"); err == nil {
		t.Error("the save went out after a failed catch-up")
	}
	if n := len(saves(fs)); n != 0 {
		t.Errorf("%d edit-document calls after a failed catch-up", n)
	}
	waitSaveState(t, a, fakeslack.CanvasShared, 5*time.Second, func(s CanvasSaveState) bool { return !s.Pending })
	if n := len(saves(fs)); n != 1 {
		t.Errorf("%d edit-document calls, want the retry's", n)
	}
	if after := modelSection(t, a, fakeslack.CanvasShared, "Weekly sync"); quip.SectionRTML(&after) != "Weekly sync, caught up" {
		t.Errorf("saved %q", quip.SectionRTML(&after))
	}
}

// Slack's error state ends once nothing is left to save: two failures lock
// the canvas, the retry finds the section saved by someone else and drops
// the edit, and with the queue empty the lock lifts.
func TestCanvasSaveLockLiftsWhenEmpty(t *testing.T) {
	fastSaves(t)
	saveBackoffBase = 400 * time.Millisecond // the other save lands before the third attempt
	a, fs := canvasApp(t)
	ctx := context.Background()
	loadModel(t, a, fakeslack.CanvasShared)
	head := modelSection(t, a, fakeslack.CanvasShared, "Weekly sync")
	fs.SetFault("canvas/edit-document", fakeslack.Fault{Status: http.StatusServiceUnavailable, Times: 2})
	if err := a.SaveCanvasText(ctx, fakeslack.CanvasShared, *head.ID, "Weekly sync", "Weekly sync, A"); err == nil {
		t.Fatal("the first attempt went through")
	}
	waitSaveState(t, a, fakeslack.CanvasShared, 5*time.Second, func(s CanvasSaveState) bool { return s.ReadOnly })
	if !fs.ReplaceCanvasBlockAsOther(fakeslack.CanvasShared, *head.ID, "# Weekly sync, B") {
		t.Fatal("no heading to replace")
	}
	waitSaveState(t, a, fakeslack.CanvasShared, 5*time.Second, func(s CanvasSaveState) bool { return !s.Pending && !s.ReadOnly })
}
