package ui

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// The Downloading Update row's description and the menus' title at each
// stage and size.
func TestUpdateProgressText(t *testing.T) {
	const tag = "v0.1.0-alpha.26"
	const mib = 1 << 20
	for _, c := range []struct {
		p          UpdateProgress
		want, menu string
		frac       float32
	}{
		{UpdateProgress{}, "Clack v0.1.0-alpha.26 · Starting…", "Downloading Update", -1},
		{UpdateProgress{Total: 37_330_000}, "Clack v0.1.0-alpha.26 · 0.0 of 35.6 MB (0%)", "Downloading Update… 0%", 0},
		{UpdateProgress{Done: 12_700_000, Total: 37_330_000}, "Clack v0.1.0-alpha.26 · 12.1 of 35.6 MB (34%)", "Downloading Update… 34%", 0.3402},
		{UpdateProgress{Done: 37_329_999, Total: 37_330_000}, "Clack v0.1.0-alpha.26 · 35.6 of 35.6 MB (99%)", "Downloading Update… 99%", 1},
		{UpdateProgress{Done: 37_330_000, Total: 37_330_000}, "Clack v0.1.0-alpha.26 · 35.6 of 35.6 MB (100%)", "Downloading Update… 100%", 1},
		{UpdateProgress{Done: 300 << 10, Total: 900 << 10}, "Clack v0.1.0-alpha.26 · 300 of 900 KB (33%)", "Downloading Update… 33%", 0.3333},
		{UpdateProgress{Done: 40 * mib, Total: 30 * mib}, "Clack v0.1.0-alpha.26 · 30.0 of 30.0 MB (100%)", "Downloading Update… 100%", 1},
		// size unknown: what's in so far, an indeterminate bar
		{UpdateProgress{Done: 512 << 10}, "Clack v0.1.0-alpha.26 · 512 KB", "Downloading Update", -1},
		{UpdateProgress{Done: 12*mib + mib/10}, "Clack v0.1.0-alpha.26 · 12.1 MB", "Downloading Update", -1},
		{UpdateProgress{Stage: UpdateVerifying, Done: 9 * mib, Total: 36 * mib}, "Clack v0.1.0-alpha.26 · Checking it against SHA256SUMS… 25%", "Downloading Update", 0.25},
		{UpdateProgress{Stage: UpdateVerifying}, "Clack v0.1.0-alpha.26 · Checking it against SHA256SUMS…", "Downloading Update", -1},
		{UpdateProgress{Stage: UpdateStaging, Done: mib, Total: mib}, "Clack v0.1.0-alpha.26 · Preparing…", "Downloading Update", -1},
		{UpdateProgress{Stage: UpdateStaged, Done: mib, Total: mib}, "Clack v0.1.0-alpha.26 · Preparing…", "Downloading Update", -1},
	} {
		if got := updateProgressText(tag, c.p); got != c.want {
			t.Errorf("%+v: %q, want %q", c.p, got, c.want)
		}
		if got := updateMenuProgress(c.p); got != c.menu {
			t.Errorf("%+v menu: %q, want %q", c.p, got, c.menu)
		}
		if got := c.p.frac(); got < c.frac-0.001 || got > c.frac+0.001 {
			t.Errorf("%+v bar: %v, want %v", c.p, got, c.frac)
		}
	}
}

// However fast the downloader reports, the sink posts at most once per
// interval, and the last post carries the last report.
func TestUpdateProgressThrottle(t *testing.T) {
	var mu sync.Mutex
	var posts []UpdateProgress
	s := newProgressSink(50*time.Millisecond, func(p UpdateProgress) {
		mu.Lock()
		posts = append(posts, p)
		mu.Unlock()
	})
	start := time.Now()
	var n int64
	for time.Since(start) < 300*time.Millisecond {
		n++
		s.report(UpdateProgress{Done: n, Total: 1 << 40})
	}
	took := time.Since(start)
	time.Sleep(120 * time.Millisecond) // the trailing post
	mu.Lock()
	got := append([]UpdateProgress(nil), posts...)
	mu.Unlock()
	if limit := int(took/(50*time.Millisecond)) + 2; len(got) > limit {
		t.Errorf("%d posts for %d reports in %v (want ≤ %d)", len(got), n, took, limit)
	}
	if len(got) < 3 {
		t.Errorf("%d posts in %v: nothing in between", len(got), took)
	}
	if last := got[len(got)-1]; last.Done != n {
		t.Errorf("last post %+v, want the last report (%d)", last, n)
	}
	s.stop()
	s.report(UpdateProgress{Done: n + 1})
	time.Sleep(60 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(posts) != len(got) {
		t.Error("posted after stop")
	}
}

// progressFake is a downloader the test drives: it hands over its
// progress callback and waits to be told how to end (or cancelled).
type progressFake struct {
	starts   atomic.Int32
	cancels  atomic.Int32
	progress chan func(UpdateProgress)
	release  chan error
}

func progressApp(t *testing.T, f *progressFake) *App {
	t.Helper()
	a := New(Options{ConfigDir: t.TempDir(), NoRealtime: true,
		CheckUpdate: func(context.Context) (UpdateInfo, bool, error) { return UpdateInfo{}, false, nil },
		DownloadUpdate: func(ctx context.Context, u UpdateInfo, progress func(UpdateProgress)) error {
			f.starts.Add(1)
			f.progress <- progress
			select {
			case err := <-f.release:
				return err
			case <-ctx.Done():
				f.cancels.Add(1)
				return ctx.Err()
			}
		},
		InstallUpdate: func(UpdateInfo) error { return nil },
	}, nil)
	sh := theme.NewShaper()
	a.th.SetShapers(sh, sh)
	a.update.quit = func() {}
	return a
}

func pendingPosts(a *App) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.pending)
}

// The Preferences row and the menus follow a download's progress; a
// storm of reports is a handful of redraws; Cancel stops the download
// (the hourly check doesn't restart it, Download does); a failure offers
// Try Again.
func TestUpdateProgressRowCancelRetry(t *testing.T) {
	SetVersion("v0.1.0-alpha.25")
	t.Cleanup(func() { SetVersion("") })
	f := &progressFake{progress: make(chan func(UpdateProgress), 1), release: make(chan error, 1)}
	a := progressApp(t, f)
	a.updProgressEvery = 40 * time.Millisecond
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}, section: clackTabID}
	click := func(label string) updateStatus { t.Helper(); return rowClick(t, a, p, label) }
	info := UpdateInfo{Tag: "v0.1.0-alpha.26", URL: "https://github.com/x"}
	a.updateFound(info, false)
	if a.update.phase != updDownloading {
		t.Fatalf("phase %d after finding an update", a.update.phase)
	}
	report := <-f.progress

	// a burst: thousands of reports, a post or two
	a.drain()
	for i := int64(1); i <= 5000; i++ {
		report(UpdateProgress{Done: i * 2540, Total: 37_330_000})
	}
	if n := pendingPosts(a); n > 2 {
		t.Errorf("%d posts for a burst of 5000 reports", n)
	}
	waitUpd(t, a, "the burst's last count", func() bool { return a.update.prog.Done == 5000*2540 })
	report(UpdateProgress{Done: 12_700_000, Total: 37_330_000})
	waitUpd(t, a, "34%", func() bool { return a.update.prog.Done == 12_700_000 })
	st := prefsRow(a, p)
	if st.title != "Downloading Update" || st.desc != "Clack v0.1.0-alpha.26 · 12.1 of 35.6 MB (34%)" || !st.bar || st.frac < 0.33 || st.frac > 0.35 {
		t.Fatalf("mid-download row: %+v", st)
	}
	if st.button != "Cancel" || st.primary || !st.enabled {
		t.Errorf("mid-download button %q (primary %v, enabled %v)", st.button, st.primary, st.enabled)
	}
	if title, ok := a.updateMenuItem(); title != "Downloading Update… 34%" || ok {
		t.Errorf("menu item %q (enabled %v)", title, ok)
	}
	report(UpdateProgress{Stage: UpdateVerifying, Done: 1, Total: 4})
	waitUpd(t, a, "verifying", func() bool { return a.update.prog.Stage == UpdateVerifying })
	if st := prefsRow(a, p); st.desc != "Clack v0.1.0-alpha.26 · Checking it against SHA256SUMS… 25%" {
		t.Errorf("verifying row: %q", st.desc)
	}

	// Cancel: stopped, back to Download; late reports don't come back
	st = click("Cancel")
	if a.update.phase != updAvailable || st.title != "An Update Is Available" || st.button != "Download" || !st.primary || st.bar {
		t.Fatalf("after Cancel: phase %d, %+v", a.update.phase, st)
	}
	waitUpd(t, a, "the download to see the cancel", func() bool { return f.cancels.Load() == 1 })
	report(UpdateProgress{Done: 99, Total: 100})
	time.Sleep(60 * time.Millisecond)
	a.drain()
	if a.update.phase != updAvailable || a.update.dlErr != "" || a.update.prog != (UpdateProgress{}) {
		t.Errorf("after the cancelled download returned: phase %d, dlErr %q, prog %+v", a.update.phase, a.update.dlErr, a.update.prog)
	}
	// the next automatic check finds the same release: stays cancelled
	a.updateFound(info, false)
	if a.update.phase != updAvailable || f.starts.Load() != 1 {
		t.Fatalf("the hourly check restarted a cancelled download (phase %d, %d starts)", a.update.phase, f.starts.Load())
	}

	// Download again: it fails, Try Again, it finishes
	prefsRow(a, p) // lays out the Download button
	if st := click("Download"); st.title != "Downloading Update" {
		t.Fatalf("Download again: %+v", st)
	}
	<-f.progress
	f.release <- context.DeadlineExceeded
	waitUpd(t, a, "the failure", func() bool { return a.update.phase == updAvailable })
	prefsRow(a, p)
	st = prefsRow(a, p)
	if st.title != "Couldn’t update Clack" || !strings.Contains(st.desc, "deadline exceeded") || st.button != "Try Again" || !st.primary {
		t.Fatalf("failed: %+v", st)
	}
	click("Try Again")
	report = <-f.progress
	report(UpdateProgress{Stage: UpdateStaging, Done: 4, Total: 4})
	waitUpd(t, a, "staging", func() bool { return a.update.prog.Stage == UpdateStaging })
	if st := prefsRow(a, p); st.desc != "Clack v0.1.0-alpha.26 · Preparing…" || !st.bar || st.frac >= 0 {
		t.Errorf("staging row: %+v", st)
	}
	f.release <- nil
	waitUpd(t, a, "ready", func() bool { return a.update.phase == updReady })
	if st := prefsRow(a, p); st.title != "Restart to Apply Update" || st.button != "Restart Clack" || st.bar {
		t.Errorf("ready row: %+v", st)
	}
	if n := f.starts.Load(); n != 3 {
		t.Errorf("%d downloads, want 3", n)
	}
}
