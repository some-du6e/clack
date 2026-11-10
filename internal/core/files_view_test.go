package core

import (
	"context"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

func filesViewFake(t *testing.T) (*App, *fakeslack.Server) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	return New(slack.New(fs.Creds()), store.New("")), fs
}

// Starring goes out as Slack's fetchers send it (files.favorites.add /
// remove {file_id}), and files.favorites.list's file_ids come back newest
// first.
func TestFilesViewStar(t *testing.T) {
	a, fs := filesViewFake(t)
	ctx := context.Background()
	for _, id := range []string{fakeslack.CanvasShared, fakeslack.CanvasChannel} {
		if err := a.StarFile(ctx, id, true); err != nil {
			t.Fatalf("star %s: %v", id, err)
		}
	}
	if !a.FileStarred(fakeslack.CanvasShared) {
		t.Error("starred canvas not marked locally")
	}
	ids, err := a.StarredFiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != fakeslack.CanvasChannel || ids[1] != fakeslack.CanvasShared {
		t.Errorf("StarredFiles = %v, want newest first", ids)
	}
	if err := a.StarFile(ctx, fakeslack.CanvasShared, false); err != nil {
		t.Fatal(err)
	}
	if a.FileStarred(fakeslack.CanvasShared) {
		t.Error("unstarred canvas still marked")
	}
	if got := fs.StarredFiles(); len(got) != 1 || got[0] != fakeslack.CanvasChannel {
		t.Errorf("server starred = %v", got)
	}
	calls := fs.FilesViewCalls()
	if len(calls) != 3 || calls[0].Method != "files.favorites.add" || calls[2].Method != "files.favorites.remove" ||
		calls[2].Args.Get("file_id") != fakeslack.CanvasShared {
		t.Errorf("calls = %+v", calls)
	}
	// a failed call puts the local state back (the server never had it)
	a.SetFileStarred(fakeslack.CanvasShared, true)
	if err := a.StarFile(ctx, fakeslack.CanvasShared, false); err == nil {
		t.Error("unstarring a file the server hasn't starred: want not_starred")
	}
	if !a.FileStarred(fakeslack.CanvasShared) {
		t.Error("failed unstar didn't put the star back")
	}
}

// Deleting a canvas from the Files view (DeleteFileDialog's deleteFile:
// files.delete {file}) takes it out of the browser.
func TestFilesViewDelete(t *testing.T) {
	a, fs := filesViewFake(t)
	ctx := context.Background()
	if err := a.DeleteFile(ctx, fakeslack.CanvasShared); err != nil {
		t.Fatal(err)
	}
	if !fs.FileDeleted(fakeslack.CanvasShared) {
		t.Error("files.delete didn't reach the server")
	}
	p, err := a.Files(ctx, FilesOpts{Kind: "canvases"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range p.Files {
		if f.ID == fakeslack.CanvasShared {
			t.Error("deleted canvas still listed")
		}
	}
}

// Edit file details' Done is editFile: files.edit {file, title,
// is_canvas_template: false}.
func TestFilesViewRename(t *testing.T) {
	a, fs := filesViewFake(t)
	if err := a.RenameFile(context.Background(), fakeslack.SearchFileID, "Renamed checklist"); err != nil {
		t.Fatal(err)
	}
	if got := fs.FileTitle(fakeslack.SearchFileID); got != "Renamed checklist" {
		t.Errorf("title = %q", got)
	}
	calls := fs.FilesViewCalls()
	if len(calls) != 1 || calls[0].Args.Get("is_canvas_template") != "false" {
		t.Errorf("calls = %+v", calls)
	}
}
