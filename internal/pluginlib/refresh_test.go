package pluginlib

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// Refresh, the Library's self-refresh (Preferences' Plugins pane, the
// window's focus, the hourly check), sends one request at a time, only
// for a copy past maxAge unless forced, and after a failure holds back
// unforced ones: a minute, doubling with each failure in a row up to an
// hour, cleared by a success. A copy on disk is as old as its file.
func TestRefreshOneAtATimeWithBackoff(t *testing.T) {
	files := bundles()
	index := testIndex(t, files, nil)
	var hits atomic.Int32
	var failing, holding atomic.Bool
	gate := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if holding.Load() {
			<-gate
		}
		if failing.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write(index)
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	l := New(Options{IndexURL: srv.URL + "/index.json", CacheDir: dir})
	answers := make(chan error, 16)
	done := func(_ *Index, err error) { answers <- err }
	answer := func() error {
		t.Helper()
		select {
		case err := <-answers:
			return err
		case <-time.After(30 * time.Second):
			t.Fatal("no answer")
			return nil
		}
	}

	// no copy: a request, and only one while it's out, forced or not
	holding.Store(true)
	if !l.Refresh(time.Hour, false, done) || !l.Refreshing() {
		t.Fatal("no copy, and no request")
	}
	if l.Refresh(time.Hour, false, done) || l.Refresh(time.Hour, true, done) {
		t.Fatal("a second request went out while one was out")
	}
	close(gate)
	if err := answer(); err != nil {
		t.Fatal(err)
	}
	holding.Store(false)
	if l.Refreshing() || hits.Load() != 1 || l.Cached() == nil {
		t.Fatalf("after the answer: refreshing %v, %d requests, index %v", l.Refreshing(), hits.Load(), l.Cached())
	}

	// fresh: nothing goes out unless forced; past maxAge it does
	if l.Refresh(time.Hour, false, done) || hits.Load() != 1 {
		t.Fatal("a fresh copy was fetched again")
	}
	if !l.Refresh(time.Nanosecond, false, done) || answer() != nil || hits.Load() != 2 {
		t.Fatal("a copy past maxAge wasn't fetched again")
	}

	// a failure keeps the copy and holds unforced requests back a minute
	failing.Store(true)
	kept := l.Cached()
	if !l.Refresh(time.Nanosecond, false, done) || answer() == nil {
		t.Fatal("a failing release answered")
	}
	if l.Cached() != kept || l.backoff != time.Minute || time.Until(l.retryAt) < 50*time.Second {
		t.Fatalf("after a failure: index kept %v, backoff %v, retry in %v", l.Cached() == kept, l.backoff, time.Until(l.retryAt))
	}
	if l.Refresh(time.Nanosecond, false, done) || hits.Load() != 3 {
		t.Fatal("an unforced request went out during the backoff")
	}
	// a forced one isn't held back; each failure in a row doubles the
	// wait, to an hour at most
	want := []time.Duration{2, 4, 8, 16, 32, 60, 60}
	for _, m := range want {
		if !l.Refresh(time.Nanosecond, true, done) || answer() == nil {
			t.Fatal("a forced request was held back, or a failing release answered")
		}
		if l.backoff != m*time.Minute {
			t.Fatalf("backoff %v, want %v", l.backoff, m*time.Minute)
		}
	}
	// a success clears it
	failing.Store(false)
	if !l.Refresh(time.Nanosecond, true, done) || answer() != nil || l.backoff != 0 || !l.retryAt.IsZero() {
		t.Fatalf("after a success: backoff %v, retry at %v", l.backoff, l.retryAt)
	}
	if !l.Refresh(time.Nanosecond, false, done) || answer() != nil {
		t.Fatal("an unforced request was still held back after a success")
	}

	// a copy on disk is as old as its file: a new Library (Clack's next
	// start) fetches it again only once that's past maxAge
	p := filepath.Join(dir, "index.json")
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	l2 := New(Options{IndexURL: srv.URL + "/index.json", CacheDir: dir})
	if got := l2.Fetched(); got.Sub(old).Abs() > time.Second {
		t.Errorf("the copy on disk was fetched %v, its file is from %v", got, old)
	}
	n := hits.Load()
	if !l2.Refresh(time.Hour, false, done) || answer() != nil || hits.Load() != n+1 {
		t.Fatal("a copy two hours old wasn't fetched again")
	}
	l3 := New(Options{IndexURL: srv.URL + "/index.json", CacheDir: dir})
	if l3.Refresh(time.Hour, false, done) || hits.Load() != n+1 {
		t.Fatal("a copy just written was fetched again")
	}
}
