package popups

import (
	"context"
	"errors"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Slack's download toast (DownloadToast, DownloadControls) over core's
// streaming download, for a file over the old 64 MB cap: the inverted
// pillow's "{downloaded} of {total} " meta, Pause and Cancel while it
// downloads; Cancel leaves "Canceled" with Retry and no partial file;
// Retry downloads it whole; complete, the single item offers its folder,
// and 10 s later the toast is gone.
func TestDownloadToastFlow(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := core.New(slack.New(fs.Creds()), store.New(""))
	t.Cleanup(func() { a.Downloads().CancelAll() })
	dir := t.TempDir()
	DownloadDir = func() string { return dir }
	t.Cleanup(func() { DownloadDir = nil })
	th := theme.New(false)
	sh := theme.NewShaper()
	th.SetShapers(sh, sh)
	h := &tzHost{a: a, th: th, im: imgcache.New(noFetch, "", nil)}

	const size = 80 << 20
	sf := fs.AddBigFile("F0TOASTEXE1", fakeslack.BigFile{Name: "game_scarlett_master.exe", Size: size, HoldAt: 10 << 20})
	f := richtext.File{ID: sf.ID, Name: sf.Name, Title: sf.Title, Type: "exe", PrettyType: "Windows Application", Size: size, Mode: "hosted",
		URL: sf.URLPrivate, Download: sf.URLPrivateDownload, Permalink: sf.Permalink}
	if !StartDownload(h, &f) {
		t.Fatal("StartDownload refused a hosted file")
	}
	ds := a.Downloads()
	if got := ds.Active(); len(got) != 1 || got[0] != sf.ID {
		t.Fatalf("the toast's list is %v", got)
	}
	if d, _ := ds.Get(sf.ID); !strings.Contains(d.URL, "/download/") || !strings.Contains(d.URL, "origin_team="+fakeslack.TeamID) {
		t.Errorf("downloading %s, want url_private_download with origin_team (getFileDownloadUrl)", d.URL)
	}
	wait := func(what string, ok func() bool) {
		t.Helper()
		end := time.Now().Add(30 * time.Second)
		for !ok() {
			if time.Now().After(end) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	wait("the held body", func() bool { d, _ := ds.Get(sf.ID); return d.Received == 10<<20 })

	var toast DownloadToast
	var r input.Router
	now := time.Now()
	frame := func() {
		gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1200, 800)), Now: now, Source: r.Source()}
		toast.Layout(gtx, h)
		r.Frame(gtx.Ops)
	}
	frame()
	if rc := toast.Rect(); rc.Empty() || rc.Max != image.Pt(1180, 780) || rc.Dx() != 350 {
		t.Errorf("the toast is %v, want 350 wide at the bottom right, 20px in", toast.Rect())
	}
	it := toast.items[sf.ID]
	d, _ := ds.Get(sf.ID)
	s := dlShown{state: d.State, received: d.Received, total: d.Total, progress: d.Progress()}
	if got := dlMeta(&f, s, false); got != "10 MB of 84 MB Windows Application" {
		t.Errorf("the pillow's meta reads %q", got)
	}
	if got := linkLabels(toast.actionButtons(it, s)); got != "Pause,Cancel" {
		t.Errorf("controls while downloading: %s", got)
	}

	// Cancel: the stream stops, the partial file goes, and the item says
	// Canceled with Retry
	it.cancel.Click()
	frame()
	wait("the cancel", func() bool { _, ok := ds.Get(sf.ID); return !ok })
	frame()
	if names := dirList(t, dir); len(names) != 0 {
		t.Errorf("after Cancel the folder holds %v", names)
	}
	if !it.canceled {
		t.Fatal("the cancelled item isn't Canceled")
	}
	if got := linkLabels(toast.actionButtons(it, dlShown{state: core.DownloadCancelled})); got != "Canceled,Retry" {
		t.Errorf("controls after Cancel: %s", got)
	}

	// Retry: the whole file, then the item completes
	fs.ReleaseBigFile(sf.ID)
	it.retry.Click()
	frame()
	wait("the retried download", func() bool { d, _ := ds.Get(sf.ID); return d.State == core.DownloadCompleted })
	frame()
	d, _ = ds.Get(sf.ID)
	if st, err := os.Stat(filepath.Join(dir, "game_scarlett_master.exe")); err != nil || st.Size() != size || d.Path != filepath.Join(dir, "game_scarlett_master.exe") {
		t.Fatalf("the retried download: %v (%v), at %s", st, err, d.Path)
	}
	if it.canceled || it.removeAt.IsZero() {
		t.Errorf("complete: canceled %v, removal timer %v", it.canceled, it.removeAt)
	}
	if got := dlMeta(&f, dlShown{state: core.DownloadCompleted}, true); got != "Open file" {
		t.Errorf("a hovered complete pillow reads %q, want getActionText's Open file", got)
	}

	// 10 s on (activeDownloadRemoveTimeout), the item and the toast go
	now = now.Add(11 * time.Second)
	frame()
	if got := ds.Active(); len(got) != 0 {
		t.Errorf("after the removal timeout the toast's list is %v", got)
	}
	frame()
	if !toast.Rect().Empty() {
		t.Errorf("the toast still draws at %v with nothing in it", toast.Rect())
	}
	// the Downloads list still has it (View all downloads)
	if l := ds.List(); len(l) != 1 || l[0].ID != sf.ID || l[0].State != core.DownloadCompleted {
		t.Errorf("the downloads list is %+v", l)
	}
}

// A download that never starts (here a file the server doesn't have)
// leaves the store, and the toast says Canceled with Retry, as
// DownloadToastItem's componentDidUpdate does for a not_started download
// that's removed: never an error text.
func TestDownloadToastFailedStart(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := core.New(slack.New(fs.Creds()), store.New(""))
	t.Cleanup(func() { a.Downloads().CancelAll() })
	dir := t.TempDir()
	DownloadDir = func() string { return dir }
	t.Cleanup(func() { DownloadDir = nil })
	th := theme.New(false)
	h := &tzHost{a: a, th: th, im: imgcache.New(noFetch, "", nil)}
	f := richtext.File{ID: "F0GONE0001", Name: "gone.exe", Type: "exe", Mode: "hosted",
		Download: fs.URL + "files-pri/" + fakeslack.TeamID + "-F0GONE0001/download/gone.exe"}
	var toast DownloadToast
	var r input.Router
	frame := func() {
		gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1200, 800)), Now: time.Now(), Source: r.Source()}
		toast.Layout(gtx, h)
		r.Frame(gtx.Ops)
	}
	// the folder waits until the toast has seen the download not_started
	gate := make(chan struct{})
	ChooseDownloadDir = func() (string, bool, error) { <-gate; return dir, true, nil }
	t.Cleanup(func() { ChooseDownloadDir = nil })
	StartDownload(h, &f)
	frame() // the item seen not_started
	if it := toast.items[f.ID]; it == nil || it.prev.State != core.DownloadNotStarted {
		t.Fatalf("the toast's first look: %+v", toast.items[f.ID])
	}
	close(gate)
	end := time.Now().Add(10 * time.Second)
	for {
		if _, ok := a.Downloads().Get(f.ID); !ok {
			break
		}
		if time.Now().After(end) {
			t.Fatal("the failed download is still listed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	frame()
	it := toast.items[f.ID]
	if it == nil || !it.canceled {
		t.Fatalf("a download that never started: item %+v, want Canceled", it)
	}
	if toast.Rect().Empty() {
		t.Error("the toast hid the failed download at once")
	}
	if len(h.toastList()) != 0 {
		t.Errorf("a failed download toasted %q", h.toastList())
	}
}

func TestConvertFilesize(t *testing.T) {
	for n, want := range map[int64]string{0: "0 bytes", 999: "999 B", 1000: "1 kB", 1499: "1 kB", 1500: "2 kB", 10 << 20: "10 MB", 80 << 20: "84 MB", 5e9: "5 GB"} {
		if got := ConvertFilesize(n); got != want {
			t.Errorf("ConvertFilesize(%d) = %q, want %q", n, got, want)
		}
	}
}

func noFetch(context.Context, string) ([]byte, error) { return nil, errors.New("no network in tests") }

func linkLabels(ls []dlLink) string {
	var out []string
	for _, l := range ls {
		out = append(out, l.label)
	}
	return strings.Join(out, ",")
}

func dirList(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}
