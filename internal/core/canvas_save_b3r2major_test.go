package core

// The B3 parts 2-3 review's MAJORs (wf_63029827-35f): the queue's deletes,
// splits and creates around other edits.

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/fakeslack"
)

// A delete built while a sibling's delete waits counts it: the list's last
// item goes with its list, so the two deletes save together.
func TestCanvasSaveSiblingDeletes(t *testing.T) {
	fastSaves(t)
	saveBackoffBase = time.Hour
	const id, one, two = "F0QUIPR2D01", "temp:C:RTD00002", "temp:C:RTD00003"
	a, fs := r2canvas(t, id, []canvas.Block{
		{ID: "temp:C:RTD00001", Kind: canvas.Paragraph, Spans: r2txt("before")},
		{ID: one, Kind: canvas.Bullet, Spans: r2txt("one")},
		{ID: two, Kind: canvas.Bullet, Spans: r2txt("two")},
		{ID: "temp:C:RTD00004", Kind: canvas.Paragraph, Spans: r2txt("after")},
	})
	ctx := context.Background()
	fs.SetFault("canvas/edit-document", fakeslack.Fault{Status: http.StatusServiceUnavailable, Times: 1})
	if err := a.SaveCanvasDelete(ctx, id, one, r2txt("one")); err == nil {
		t.Fatal("the first delete's attempt went through")
	}
	if err := a.SaveCanvasDelete(ctx, id, two, r2txt("two")); err != nil {
		t.Fatalf("the list's other item emptied while the first delete waits: %v", err)
	}
	if b1, b2 := fakeBlock(fs, id, "one"), fakeBlock(fs, id, "two"); b1 != nil || b2 != nil {
		t.Errorf("after both deletes: %+v %+v", b1, b2)
	}
	if ns := a.TakeCanvasSaveNotices(id); len(ns) != 0 {
		t.Errorf("notices %+v", ns)
	}
}

// A new block deleted before its first save was never made: the new block
// typed after it follows the one it followed, and saves.
func TestCanvasSaveDeletedNewBlockKeepsNext(t *testing.T) {
	fastSaves(t)
	saveBackoffBase = time.Hour
	const id, p = "F0QUIPR2E01", "temp:C:RTE00001"
	a, fs := r2canvas(t, id, []canvas.Block{
		{ID: p, Kind: canvas.Paragraph, Spans: r2txt("first")},
		{ID: "temp:C:RTE00002", Kind: canvas.Paragraph, Spans: r2txt("last")},
	})
	ctx := context.Background()
	x, n := a.NewCanvasSectionID(id), a.NewCanvasSectionID(id)
	fs.SetFault("canvas/edit-document", fakeslack.Fault{Status: http.StatusServiceUnavailable, Times: 1})
	err := a.SaveCanvasNewBlocks(ctx, id, p, []CanvasNewBlock{
		{ID: x, Kind: canvas.Paragraph, Spans: r2txt("deleted before its save")},
		{ID: n, Kind: canvas.Paragraph, Spans: r2txt("typed after it")},
	})
	if err == nil {
		t.Fatal("the first attempt went through")
	}
	if err := a.SaveCanvasDelete(ctx, id, x, r2txt("deleted before its save")); err != nil {
		t.Fatalf("the first new block emptied before its save: %v", err)
	}
	if st := a.CanvasSaveState(id); st.Pending {
		retryNow(t, a, id)
	}
	if b := fakeBlock(fs, id, "typed after it"); b == nil {
		t.Error("the new block after the deleted one was dropped")
	}
	if b := fakeBlock(fs, id, "deleted before its save"); b != nil {
		t.Errorf("the deleted new block was made: %+v", b)
	}
	if ns := a.TakeCanvasSaveNotices(id); len(ns) != 0 {
		t.Errorf("notices %+v", ns)
	}
}

// An Enter whose first part someone else saved over goes whole or not at
// all: the new block isn't made without its section's own edit, so the
// moved text isn't in both.
func TestCanvasSaveSplitConflictDropsNew(t *testing.T) {
	fastSaves(t)
	saveBackoffBase = time.Hour
	a, fs := canvasApp(t)
	ctx := context.Background()
	loadModel(t, a, fakeslack.CanvasShared)
	head := modelSection(t, a, fakeslack.CanvasShared, "Weekly sync")
	fs.SetFault("canvas/edit-document", fakeslack.Fault{Status: http.StatusServiceUnavailable, Times: 1})
	err := a.SaveCanvasSplit(ctx, fakeslack.CanvasShared, *head.ID, r2txt("Weekly sync"), r2txt("Weekly"),
		[]CanvasNewBlock{{ID: a.NewCanvasSectionID(fakeslack.CanvasShared), Kind: canvas.Paragraph, Spans: r2txt("sync, moved")}}, 0)
	if err == nil {
		t.Fatal("the first attempt went through")
	}
	if !fs.ReplaceCanvasBlockAsOther(fakeslack.CanvasShared, *head.ID, "# Weekly sync, theirs") {
		t.Fatal("no heading to replace")
	}
	retryNow(t, a, fakeslack.CanvasShared)
	if b := fakeBlock(fs, fakeslack.CanvasShared, "sync, moved"); b != nil {
		t.Errorf("half a split was saved: %+v", b)
	}
	if ns := a.TakeCanvasSaveNotices(fakeslack.CanvasShared); len(ns) != 1 || !ns[0].Conflict {
		t.Errorf("notices %+v, want the conflict", ns)
	}
}

// A new block whose create waited while another client added a block
// after the same one saves with the key Slack's editor made at Enter, as
// Slack sends it (the validator doesn't hold it to the model's neighbours).
func TestCanvasSaveNewBlockBesideTheirs(t *testing.T) {
	fastSaves(t)
	saveBackoffBase = time.Hour
	const id, last = "F0QUIPR2F01", "temp:C:RTF00002"
	a, fs := r2canvas(t, id, []canvas.Block{
		{ID: "temp:C:RTF00001", Kind: canvas.Paragraph, Spans: r2txt("first")},
		{ID: last, Kind: canvas.Paragraph, Spans: r2txt("last")},
	})
	ctx := context.Background()
	fs.SetFault("canvas/edit-document", fakeslack.Fault{Status: http.StatusServiceUnavailable, Times: 1})
	err := a.SaveCanvasNewBlocks(ctx, id, last, []CanvasNewBlock{{ID: a.NewCanvasSectionID(id), Kind: canvas.Paragraph, Spans: r2txt("ours")}})
	if err == nil {
		t.Fatal("the first attempt went through")
	}
	fs.EditCanvasAsOther(id, "theirs")
	retryNow(t, a, id)
	if b := fakeBlock(fs, id, "ours"); b == nil {
		t.Error("the new block wasn't saved beside the other client's")
	}
	if b := fakeBlock(fs, id, "theirs"); b == nil {
		t.Error("the other client's block went")
	}
	if ns := a.TakeCanvasSaveNotices(id); len(ns) != 0 {
		t.Errorf("notices %+v", ns)
	}
}
