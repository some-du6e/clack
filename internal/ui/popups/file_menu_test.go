package popups

// The file menu with a signed-in core (fakeslack, synthetic): Slack's
// media-gallery templates (buildFileContextMenuItems) with Save for later
// on every file and Delete file on the viewer's own image and video
// files; the writes go out as Slack's saveFile / removeFile
// (saved.add / saved.delete {item_type: file, item_id}) and
// DeleteFileDialog's files.delete {file}.

import (
	"context"
	"image"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestFileMenuLaterAndDelete(t *testing.T) {
	srv, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	fs := core.New(slack.New(srv.Creds()), store.New(""))
	ctx0, cancel0 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel0()
	fs.EnsureUsers(ctx0, []string{fakeslack.SelfID, fakeslack.ProfileID})
	if u, ok := fs.S.User(fakeslack.SelfID); ok {
		fs.S.Self = *u
	}
	h := &tzHost{a: fs, th: theme.New(false)}
	var mu sync.Mutex
	calls := map[string]url.Values{}
	srv.SetHook(func(method string, f url.Values) (any, bool) {
		switch method {
		case "saved.add", "saved.delete", "files.delete":
			mu.Lock()
			calls[method] = f
			mu.Unlock()
			return map[string]any{"ok": true}, true
		}
		return nil, false
	})
	at := image.Pt(100, 100)
	self := fakeslack.SelfID
	m := core.Message{Channel: "C1", TS: "1700000000.000100", User: self}
	img := &richtext.File{ID: "F1", Name: "a.png", IsImage: true, User: self, URL: "https://files.slack.com/a.png", Permalink: "https://x.slack.com/files/U/F1/a.png"}
	if got := ctxLabels(NewFileMenu(h, at, m, img)); got != "Download|-|Share file…|Copy link|Copy image|Save for later|-|Open in[Open in browser,Open in new tab]|-|Delete file" {
		t.Errorf("own image file menu %q", got)
	}
	vid := &richtext.File{ID: "F2", Name: "a.mp4", IsVideo: true, User: self, URL: "https://files.slack.com/a.mp4", Permalink: "https://x.slack.com/files/U/F2/a.mp4"}
	if got := ctxLabels(NewFileMenu(h, at, m, vid)); got != "Download|Share file…|Copy link|Save for later|-|Open in[Open in browser,Open in thread,Open in new window]|-|Delete file" {
		t.Errorf("own video file menu %q", got)
	}
	// Slack's "other" template has no Delete file, own or not
	pdf := &richtext.File{ID: "F3", Name: "a.pdf", Type: "pdf", User: self, URL: "https://files.slack.com/a.pdf", Permalink: "https://x.slack.com/files/U/F3/a.pdf"}
	if got := ctxLabels(NewFileMenu(h, at, m, pdf)); got != "Share file…|Copy link|Download|Save for later" {
		t.Errorf("pdf file menu %q", got)
	}
	// someone else's image: no Delete file
	other := *img
	other.User = fakeslack.ProfileID
	if got := ctxLabels(NewFileMenu(h, at, core.Message{Channel: "C1", TS: "1.2", User: fakeslack.ProfileID}, &other)); got != "Download|-|Share file…|Copy link|Copy image|Save for later|-|Open in[Open in browser,Open in new tab]" {
		t.Errorf("another person's image menu %q", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := fs.SaveFile(ctx, "F1"); err != nil {
		t.Fatal(err)
	}
	got := func(method string) url.Values {
		mu.Lock()
		defer mu.Unlock()
		return calls[method]
	}
	if p := got("saved.add"); p.Get("item_type") != "file" || p.Get("item_id") != "F1" || p.Has("ts") {
		t.Errorf("saved.add %v", p)
	}
	if got := ctxLabels(NewFileMenu(h, at, m, pdf)); got != "Share file…|Copy link|Download|Save for later" {
		t.Errorf("an unsaved file reads Save for later: %q", got)
	}
	f1 := *pdf
	f1.ID = "F1"
	if got := ctxLabels(NewFileMenu(h, at, m, &f1)); got != "Share file…|Copy link|Download|Remove from Later" {
		t.Errorf("saved file menu %q", got)
	}
	if err := fs.UnsaveFile(ctx, "F1"); err != nil {
		t.Fatal(err)
	}
	if p := got("saved.delete"); p.Get("item_type") != "file" || p.Get("item_id") != "F1" {
		t.Errorf("saved.delete %v", p)
	}
	if fs.FileSaved("F1") {
		t.Error("still saved after Remove from Later")
	}
	if err := fs.DeleteFile(ctx, "F2"); err != nil {
		t.Fatal(err)
	}
	if p := got("files.delete"); p.Get("file") != "F2" || len(p) != 2 { // file + token
		t.Errorf("files.delete %v", p)
	}
}
