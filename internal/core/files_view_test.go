package core

import (
	"context"
	"testing"
	"time"

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
	a := New(slack.New(fs.Creds()), store.New(""))
	a.SetExperimentGroup("custom_file_sections", "") // unassigned, as fakeslack has it (core's defaults hold the account's group); tests turn it on
	return a, fs
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

// Deleted: files.recentlyDeleted lists deleted canvases and lists (the
// latest first, by file_type, nothing older than a day), and
// files.undelete {file_id} brings one back.
func TestFilesViewRestore(t *testing.T) {
	a, fs := filesViewFake(t)
	ctx := context.Background()
	for _, id := range []string{fakeslack.CanvasShared, fakeslack.ListKitchen, fakeslack.CanvasChannel} {
		if err := a.DeleteFile(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	fs.TrashFile(fakeslack.CanvasShared, time.Now().Add(-time.Hour))
	fs.TrashFile(fakeslack.CanvasChannel, time.Now().Add(-25*time.Hour)) // past the day
	got, err := a.RecentlyDeleted(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != fakeslack.ListKitchen || got[0].FileType != "list" || got[1].ID != fakeslack.CanvasShared ||
		got[1].FileType != "quip" || got[1].Title != "Weekly sync notes" || got[1].DateDelete == 0 {
		t.Fatalf("recently deleted %+v", got)
	}
	if only, _ := a.RecentlyDeleted(ctx, "list"); len(only) != 1 || only[0].ID != fakeslack.ListKitchen {
		t.Errorf("lists only: %+v", only)
	}
	if err := a.UndeleteFile(ctx, fakeslack.CanvasShared); err != nil {
		t.Fatal(err)
	}
	calls := fs.FilesViewCalls()
	if last := calls[len(calls)-1]; last.Method != "files.undelete" || last.Args.Get("file_id") != fakeslack.CanvasShared {
		t.Errorf("last call %+v", last)
	}
	p, err := a.Files(ctx, FilesOpts{Kind: "canvases"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range p.Files {
		found = found || f.ID == fakeslack.CanvasShared
	}
	if !found {
		t.Error("the restored canvas isn't listed")
	}
	if left, _ := a.RecentlyDeleted(ctx, ""); len(left) != 1 || left[0].ID != fakeslack.ListKitchen {
		t.Errorf("after restore %+v", left)
	}
	if err := a.UndeleteFile(ctx, fakeslack.CanvasChannel); err == nil {
		t.Error("restored a canvas deleted more than a day ago")
	}
}
