package popups

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
)

// sfBooted is emBooted with the Files & links fixtures (files.getOverview,
// files.share for #files-gallery's files).
func sfBooted(t *testing.T) (*tzHost, *fakeslack.Server, *emCalls) {
	t.Helper()
	size := fakeslack.Small
	size.Files = true
	fs, err := fakeslack.Start(size)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	rec := &emCalls{calls: map[string][]url.Values{}}
	fs.SetHook(emHook(t, rec))
	h := hcEnvOn(t, fs)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- h.a.Boot(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("boot: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("boot timed out")
	}
	return h, fs, rec
}

// The file share dialog (openShareDialog {fileId}, zUYy): "Share this
// file", files.getOverview on open and the access sentence, Copy Link's
// file tooltip, one destination, and Share sending files.share then
// handing back the destination and ts.
func TestShareFileDialogLikeSlack(t *testing.T) {
	h, _, rec := sfBooted(t)
	var sharedCh, sharedTS string
	f := newShareFile(h, ShareFileOpts{ID: "F0FILESPDF01", Title: "Q3 report", Permalink: "https://fake.slack.com/files/F0FILESPDF01",
		Shared: func(ch, ts string) { sharedCh, sharedTS = ch, ts }})
	if got := f.title(); got != "Share this file" {
		t.Errorf("title %q", got)
	}
	if got := f.copyTip(); got != "You can also copy a link to this file. To share, paste it in any channel or conversation." {
		t.Errorf("copy tip %q", got)
	}
	rec.wait(t, "files.getOverview", map[string]string{"file_id": "F0FILESPDF01"}, 10*time.Second)
	maUntil(t, "the overview", func() bool { return f.overviewDone })
	if got := fileAccessText(f.overview); got != "6 users and 2 channels have access to this file." {
		t.Errorf("access %q", got)
	}
	if len(f.all) < 2 {
		t.Fatalf("%d destinations", len(f.all))
	}
	f.match = []int{0, 1}
	f.pick(0)
	f.match = []int{0, 1}
	f.pick(1)
	if len(f.chips) != 1 || f.chips[0].t != &f.all[1] {
		t.Fatalf("chips %d: a file goes to one conversation", len(f.chips))
	}
	if !f.canGo() {
		t.Fatal("Share disabled with a destination")
	}
	f.note.SetText("for the review")
	f.shareFile()
	got := rec.wait(t, "files.share", map[string]string{"files": "F0FILESPDF01", "channel": f.all[1].id}, 10*time.Second)
	for k, v := range map[string]string{"resharing_aware": "true", "skip_dlp_user_warning": "false", "from_share_modal": "false"} {
		if got.Get(k) != v {
			t.Errorf("%s = %q", k, got.Get(k))
		}
	}
	maUntil(t, "the navigation", func() bool { return sharedTS != "" })
	if sharedCh != f.all[1].id {
		t.Errorf("shared to %q", sharedCh)
	}
}

// renderFileAccessMetadata's sentence: users at least 1, counts with
// "{count}+" at the cap, has / have by the total.
func TestFileAccessText(t *testing.T) {
	for _, c := range []struct {
		ov   core.FileOverview
		want string
	}{
		{core.FileOverview{}, "1 user has access to this file."},
		{core.FileOverview{Users: 1, Channels: 1}, "1 user and 1 channel have access to this file."},
		{core.FileOverview{Users: 3, Channels: 2, Files: 1}, "3 users, 2 channels and 1 file have access to this file."},
		{core.FileOverview{Users: 500, MaxUsers: true}, "99+ users have access to this file."},
	} {
		if got := fileAccessText(c.ov); got != c.want {
			t.Errorf("%+v: %q, want %q", c.ov, got, c.want)
		}
	}
}

// The gallery opens at the clicked file and steps through the rest.
func TestFileGallery(t *testing.T) {
	h := &tzHost{}
	items := []GalleryItem{
		{Img: richtext.Image{URL: "https://files.example/a.png"}, File: &richtext.File{ID: "F1", User: "U1"}},
		{Img: richtext.Image{URL: "https://files.example/b.png"}, File: &richtext.File{ID: "F2", User: "U2"}},
		{Img: richtext.Image{URL: "https://files.example/c.png"}, File: &richtext.File{ID: "F3"}},
	}
	lb := NewFileGallery(h, items, 1)
	if lb == nil || len(lb.items) != 3 || lb.idx != 1 || lb.Image().URL != "https://files.example/b.png" {
		t.Fatalf("gallery %+v", lb)
	}
	lb.go2(2)
	if lb.Image().URL != "https://files.example/c.png" {
		t.Errorf("→ shows %q", lb.Image().URL)
	}
	if NewFileGallery(h, nil, 0) != nil {
		t.Error("an empty gallery opened")
	}
}
