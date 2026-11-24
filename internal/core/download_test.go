package core

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// Downloads stream to disk (download.go): Karim's 64 MB+ installer failed
// with "file over 64 MB" because the save went through Fetch, which holds
// a file in memory and stops at MaxFetch.

func dlFake(t *testing.T) (*fakeslack.Server, *App) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := New(slack.New(fs.Creds()), store.New(""))
	t.Cleanup(func() { a.Downloads().CancelAll() })
	return fs, a
}

func dlStart(a *App, sf slack.File, dir string) {
	a.Downloads().Start(DownloadRequest{
		ID: sf.ID, URL: a.FileDownloadURL(sf.URLPrivateDownload),
		File: richtext.File{ID: sf.ID, Name: sf.Name, Title: sf.Title, Size: sf.Size, PrettyType: sf.PrettyType, Type: sf.Filetype},
		Dir:  func(context.Context) (string, error) { return dir, nil },
	})
}

// dlWait waits until file id's download is in one of states (or gone,
// when gone is set) and returns it.
func dlWait(t *testing.T, a *App, id string, d time.Duration, ok func(Download, bool) bool) Download {
	t.Helper()
	end := time.Now().Add(d)
	for {
		dl, found := a.Downloads().Get(id)
		if ok(dl, found) {
			return dl
		}
		if time.Now().After(end) {
			t.Fatalf("download %s: still %+v (found %v)", id, dl, found)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func inState(states ...DownloadState) func(Download, bool) bool {
	return func(d Download, found bool) bool {
		for _, s := range states {
			if found && d.State == s {
				return true
			}
		}
		return false
	}
}

// settled is a download that has ended, completed or interrupted, with its
// runner gone. A cut body is Interrupted for a moment before its
// auto-resume (as Electron's item is), and that moment isn't the end.
func settled(a *App, id string) func(Download, bool) bool {
	return func(d Download, found bool) bool {
		if !found || (d.State != DownloadCompleted && d.State != DownloadInterrupted) {
			return false
		}
		ds := a.Downloads()
		ds.mu.Lock()
		defer ds.mu.Unlock()
		it := ds.items[id]
		return it != nil && it.cancel == nil
	}
}

// sameAsBigFile reads p and checks it is big file id, size bytes, byte
// for byte (streamed: neither copy is held).
func sameAsBigFile(t *testing.T, p, id string, size int64) {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got := make([]byte, 1<<20)
	want := make([]byte, 1<<20)
	var off int64
	for {
		n, err := io.ReadFull(f, got)
		if n > 0 {
			fakeslack.BigFileFill(id, off, want[:n])
			if !bytes.Equal(got[:n], want[:n]) {
				i := 0
				for got[i] == want[i] {
					i++
				}
				t.Fatalf("%s differs from the file at byte %d", p, off+int64(i))
			}
			off += int64(n)
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if off != size {
		t.Fatalf("%s is %d bytes, the file %d", p, off, size)
	}
}

func dirNames(t *testing.T, dir string) []string {
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

// A file well over the old 64 MB cap downloads whole and byte-exact, into
// the folder under its own name, without the process allocating anything
// near its size, and without passing through Fetch's cache.
func TestDownloadStreamsFileOver64MB(t *testing.T) {
	fs, a := dlFake(t)
	const size = 96<<20 + 12345
	sf := fs.AddBigFile("F0BIGEXE01", fakeslack.BigFile{Name: "game_scarlett_master.exe", Size: size})
	dir := t.TempDir()

	runtime.GC()
	var m0 runtime.MemStats
	runtime.ReadMemStats(&m0)
	var peak atomic.Uint64
	stop := make(chan struct{})
	sampled := make(chan struct{})
	go func() { // the live heap while it downloads
		defer close(sampled)
		var m runtime.MemStats
		for {
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
			}
			runtime.ReadMemStats(&m)
			if m.HeapAlloc > peak.Load() {
				peak.Store(m.HeapAlloc)
			}
		}
	}()
	dlStart(a, sf, dir)
	d := dlWait(t, a, sf.ID, 2*time.Minute, settled(a, sf.ID))
	close(stop)
	<-sampled
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	if d.State != DownloadCompleted {
		t.Fatalf("download ended %s at %d of %d bytes", d.State, d.Received, d.Total)
	}
	want := filepath.Join(dir, "game_scarlett_master.exe")
	if d.Path != want {
		t.Errorf("saved at %s, want %s", d.Path, want)
	}
	if d.Received != size || d.Total != size || d.Progress() != 1 {
		t.Errorf("received %d of %d (progress %v), want %d", d.Received, d.Total, d.Progress(), size)
	}
	sameAsBigFile(t, want, sf.ID, size)
	if names := dirNames(t, dir); len(names) != 1 {
		t.Errorf("the folder holds %v, want just the file (no partial file left)", names)
	}
	// the whole process (fakeslack's side included) allocated far less
	// than the file: nothing of it was held
	alloc := m1.TotalAlloc - m0.TotalAlloc
	t.Logf("allocated %.1f MB for a %.1f MB download; live heap peaked %.1f MB over %.1f MB", float64(alloc)/(1<<20), float64(size)/(1<<20),
		float64(peak.Load())/(1<<20), float64(m0.HeapAlloc)/(1<<20))
	if alloc > 32<<20 {
		t.Errorf("allocated %d MB downloading a %d MB file: the download isn't bounded", alloc>>20, size>>20)
	}
	if p := peak.Load(); p > m0.HeapAlloc+32<<20 {
		t.Errorf("the live heap grew %d MB while downloading", (p-m0.HeapAlloc)>>20)
	}
	a.files.mu.Lock()
	n := a.files.size
	a.files.mu.Unlock()
	if n != 0 {
		t.Errorf("Fetch's cache holds %d bytes: the download went through it", n)
	}
}

// Cancel mid-body stops the stream, deletes the partial file and drops
// the entry (REMOVE_DOWNLOADS).
func TestDownloadCancelLeavesNoPartialFile(t *testing.T) {
	fs, a := dlFake(t)
	sf := fs.AddBigFile("F0BIGEXE02", fakeslack.BigFile{Name: "setup.exe", Size: 20 << 20, HoldAt: 8 << 20})
	defer fs.ReleaseBigFile(sf.ID)
	dir := t.TempDir()
	dlStart(a, sf, dir)
	d := dlWait(t, a, sf.ID, 30*time.Second, func(d Download, found bool) bool {
		return found && d.State == DownloadProgressing && d.Received == 8<<20
	})
	if names := dirNames(t, dir); len(names) != 1 || names[0] != "setup.exe"+partialSuffix {
		t.Fatalf("mid-download the folder holds %v, want only setup.exe%s", names, partialSuffix)
	}
	if _, err := os.Stat(d.Path); !os.IsNotExist(err) {
		t.Errorf("%s exists before the download completed", d.Path)
	}
	a.Downloads().Cancel(sf.ID)
	if _, ok := a.Downloads().Get(sf.ID); ok {
		t.Error("the cancelled download is still listed")
	}
	if names := dirNames(t, dir); len(names) != 0 {
		t.Errorf("after Cancel the folder holds %v, want nothing", names)
	}
}

// An answer cut off before its Content-Length is a failure, never a
// short complete file: the network cut is resumed from where it stopped
// (Chromium's auto-resume, 5 times), and when every attempt is cut the
// download is interrupted with no file at the save path.
func TestDownloadCutOffIsNeverComplete(t *testing.T) {
	defer func(d time.Duration) { downloadResumeDelay = d }(downloadResumeDelay)
	downloadResumeDelay = 0
	fs, a := dlFake(t)
	dir := t.TempDir()

	// cut every time at 1 MB: interrupted after the auto-resumes
	sf := fs.AddBigFile("F0BIGEXE03", fakeslack.BigFile{Name: "cut.exe", Size: 3 << 20, Cut: 100, CutAt: 1 << 20})
	dlStart(a, sf, dir)
	d := dlWait(t, a, sf.ID, 30*time.Second, settled(a, sf.ID))
	if d.State != DownloadInterrupted {
		t.Fatalf("a body cut off every time ended %s", d.State)
	}
	if _, err := os.Stat(filepath.Join(dir, "cut.exe")); !os.IsNotExist(err) {
		t.Errorf("a cut-off download left cut.exe (err %v)", err)
	}
	time.Sleep(50 * time.Millisecond)
	n, ranges := fs.BigFileRequests(sf.ID)
	if n != 1+downloadAutoResumes {
		t.Errorf("%d requests (%q), want 1 and %d resumes", n, ranges, downloadAutoResumes)
	}
	for i, r := range ranges {
		want := "bytes=0-"
		if i > 0 {
			want = "bytes=1048576-"
		}
		if r != want {
			t.Errorf("request %d asked for %q, want %q", i, r, want)
		}
	}

	// a server that answers no ranges can't be resumed: one cut ends it
	sf = fs.AddBigFile("F0BIGEXE04", fakeslack.BigFile{Name: "whole.exe", Size: 3 << 20, Cut: 1, CutAt: 1 << 20, NoRanges: true})
	dlStart(a, sf, dir)
	d = dlWait(t, a, sf.ID, 30*time.Second, settled(a, sf.ID))
	if d.State != DownloadInterrupted {
		t.Errorf("a cut-off whole answer ended %s", d.State)
	}
	if _, err := os.Stat(filepath.Join(dir, "whole.exe")); !os.IsNotExist(err) {
		t.Errorf("a cut-off download left whole.exe (err %v)", err)
	}

	// cut once: the resume brings the rest, and the file is whole
	sf = fs.AddBigFile("F0BIGEXE05", fakeslack.BigFile{Name: "once.exe", Size: 5<<20 + 7, Cut: 1, CutAt: 2 << 20})
	dlStart(a, sf, dir)
	d = dlWait(t, a, sf.ID, 30*time.Second, settled(a, sf.ID))
	if d.State != DownloadCompleted {
		t.Fatalf("a body cut once ended %s", d.State)
	}
	sameAsBigFile(t, d.Path, sf.ID, 5<<20+7)
	if _, ranges := fs.BigFileRequests(sf.ID); len(ranges) != 2 || ranges[1] != "bytes=2097152-" {
		t.Errorf("requests %q, want the start and a resume at 2097152", ranges)
	}
}

// Pause ends the stream and keeps the bytes; Resume asks for the rest and
// the file comes out whole.
func TestDownloadPauseResume(t *testing.T) {
	fs, a := dlFake(t)
	sf := fs.AddBigFile("F0BIGEXE06", fakeslack.BigFile{Name: "pause.exe", Size: 12<<20 + 3, HoldAt: 4 << 20})
	dir := t.TempDir()
	dlStart(a, sf, dir)
	dlWait(t, a, sf.ID, 30*time.Second, func(d Download, found bool) bool { return found && d.Received == 4<<20 })
	a.Downloads().Pause(sf.ID)
	d := dlWait(t, a, sf.ID, 10*time.Second, inState(DownloadPaused))
	if d.Received != 4<<20 || d.Total != 12<<20+3 {
		t.Errorf("paused at %d of %d", d.Received, d.Total)
	}
	fs.ReleaseBigFile(sf.ID)
	a.Downloads().Resume(sf.ID)
	d = dlWait(t, a, sf.ID, 30*time.Second, settled(a, sf.ID))
	if d.State != DownloadCompleted {
		t.Fatalf("the resumed download ended %s", d.State)
	}
	sameAsBigFile(t, d.Path, sf.ID, 12<<20+3)
	if _, ranges := fs.BigFileRequests(sf.ID); len(ranges) != 2 || ranges[1] != "bytes=4194304-" {
		t.Errorf("requests %q, want a resume at 4194304", ranges)
	}
}

// A download that fails before its body (here a 404) leaves the list, as
// REMOVE_DOWNLOADS does, and writes nothing.
func TestDownloadFailureBeforeBodyRemoves(t *testing.T) {
	fs, a := dlFake(t)
	dir := t.TempDir()
	a.Downloads().Start(DownloadRequest{ID: "F0MISSING1", URL: fs.URL + "files-pri/" + fakeslack.TeamID + "-F0MISSING1/download/x.exe",
		Dir: func(context.Context) (string, error) { return dir, nil }})
	dlWait(t, a, "F0MISSING1", 10*time.Second, func(_ Download, found bool) bool { return !found })
	if names := dirNames(t, dir); len(names) != 0 {
		t.Errorf("a failed download wrote %v", names)
	}
	// and a URL off Slack is never asked for
	a.Downloads().Start(DownloadRequest{ID: "F0EVIL0001", URL: "https://evil.example/x.exe", Dir: func(context.Context) (string, error) { return dir, nil }})
	dlWait(t, a, "F0EVIL0001", 10*time.Second, func(_ Download, found bool) bool { return !found })
}

// Two downloads of the same name land as "name" and "name (1)", and a
// second download of a file already downloaded gets the next name
// (setUniqueSavePath).
func TestDownloadUniqueNames(t *testing.T) {
	fs, a := dlFake(t)
	dir := t.TempDir()
	sf := fs.AddBigFile("F0BIGEXE07", fakeslack.BigFile{Name: "report.exe", Size: 1 << 20})
	for i := 0; i < 2; i++ {
		dlStart(a, sf, dir)
		dlWait(t, a, sf.ID, 30*time.Second, inState(DownloadCompleted))
	}
	got := strings.Join(dirNames(t, dir), ",")
	if got != "report (1).exe,report.exe" {
		t.Errorf("the folder holds %s", got)
	}
}

func TestUniqueSavePath(t *testing.T) {
	cases := []struct {
		name  string
		taken []string
		want  string
	}{
		{"a.exe", nil, "a.exe"},
		{"a.exe", []string{"a.exe"}, "a (1).exe"},
		{"a.exe", []string{"a.exe", "a (1).exe"}, "a (2).exe"},
		{"a (1).exe", []string{"a (1).exe"}, "a (2).exe"},
		{"x(3).txt", []string{"x(3).txt"}, "x(4).txt"},
		{".bashrc", []string{".bashrc"}, ".bashrc (1)"},
		{"archive.tar.gz", []string{"archive.tar.gz"}, "archive.tar (1).gz"},
		{"x().txt", []string{"x().txt"}, "x(NaN).txt"},
	}
	for _, c := range cases {
		taken := map[string]bool{}
		for _, n := range c.taken {
			taken[filepath.Join("d", n)] = true
		}
		got := uniqueSavePath("d", c.name, func(p string) bool { return taken[p] })
		if got != filepath.Join("d", c.want) {
			t.Errorf("uniqueSavePath(%q, taken %q) = %q, want %q", c.name, c.taken, got, c.want)
		}
	}
}
