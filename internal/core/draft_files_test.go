package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// Drafts carry their files: an uploaded (unshared) file's id goes in
// drafts.create/update file_ids, drafts.list describes it (files), DraftFor
// finds the draft for its conversation, and SendDraft shares the files with
// the draft's text and deletes the draft without deleting them. Synthetic
// files, internal/fakeslack.
func TestDraftFiles(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	ctx := context.Background()
	a := New(slack.New(fs.Creds()), store.New(""))
	ch := fs.ChannelIDs()[3]

	p, err := a.PrepareUpload(ctx, UploadFile{Name: "synthetic-plot.png", Title: "synthetic-plot.png", Data: []byte("\x89PNG synthetic")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	gen := a.DraftsGen()
	d, err := a.SaveDraft(ctx, DraftOpts{Channel: ch, FileIDs: []string{p.ID}})
	if err != nil {
		t.Fatal(err)
	}
	live := fs.LiveDrafts()
	if len(live) != 1 || len(live[0].FileIDs) != 1 || live[0].FileIDs[0] != p.ID || string(live[0].Blocks) != "[]" {
		t.Fatalf("server draft %+v, want file_ids [%s] and blocks []", live, p.ID)
	}
	if a.DraftsGen() == gen {
		t.Error("DraftsGen didn't move after SaveDraft")
	}
	got, ok := a.DraftFor(ch, "")
	if !ok || got.ID != d.ID || len(got.FileInfo) != 1 || got.FileInfo[0].Name != "synthetic-plot.png" || got.FileInfo[0].Thumb == "" {
		t.Fatalf("DraftFor = %+v, %v", got, ok)
	}
	if _, ok := a.DraftFor(ch, "1700000000.000100"); ok {
		t.Error("a channel draft matched a thread")
	}

	// text added; a fresh session (restart) lists it with the file
	if _, err := a.SaveDraft(ctx, DraftOpts{ID: d.ID, Channel: ch, Text: "synthetic plot attached", FileIDs: []string{p.ID}}); err != nil {
		t.Fatal(err)
	}
	b := New(slack.New(fs.Creds()), store.New(""))
	ds, err := b.Drafts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 1 || ds[0].Text != "synthetic plot attached" || len(ds[0].FileInfo) != 1 || ds[0].FileInfo[0].ID != p.ID || ds[0].Files != 1 {
		t.Fatalf("drafts after restart: %+v", ds)
	}
	if got, ok := b.DraftFor(ch, ""); !ok || got.FileInfo[0].Name != "synthetic-plot.png" {
		t.Fatalf("restarted DraftFor = %+v, %v", got, ok)
	}

	// another device deletes it: the realtime event drops it
	raw, _ := json.Marshal(map[string]any{"type": "draft_delete", "draft_id": d.ID})
	b.Inject(slack.Event{Type: "draft_delete", Raw: raw})
	if _, ok := b.DraftFor(ch, ""); ok {
		t.Error("draft_delete didn't drop the draft")
	}

	// send from Drafts & sent: shared with the text, draft gone, file kept
	if _, err := a.SendDraft(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	sh := fs.Shared()
	if len(sh) != 1 || sh[0].Channel != ch || len(sh[0].IDs) != 1 || sh[0].IDs[0] != p.ID ||
		(sh[0].Comment != "synthetic plot attached" && !strings.Contains(string(sh[0].Blocks), "synthetic plot attached")) {
		t.Fatalf("shared %+v", sh)
	}
	if len(fs.LiveDrafts()) != 0 {
		t.Error("the sent draft is still live")
	}
	for _, dr := range fs.Drafts() {
		if dr.ID == d.ID && !dr.SkipFileDeletion {
			t.Error("drafts.delete without skip_file_deletion: the shared file would go")
		}
	}
	if f, _ := fs.File(p.ID); !f.Shared || f.Deleted {
		t.Errorf("file after send: %+v", f)
	}
	if _, ok := a.DraftFor(ch, ""); ok {
		t.Error("DraftFor still has the sent draft")
	}
}
