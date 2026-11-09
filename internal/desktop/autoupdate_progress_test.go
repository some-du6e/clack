package desktop

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// slowReleases serves SHA256SUMS and one large asset a chunk at a time,
// pausing between chunks: a release download that takes a while.
type slowReleases struct {
	srv   *httptest.Server
	name  string
	body  []byte
	chunk int
	pause time.Duration
	// noLength: no Content-Length (chunked), so the size is unknown
	noLength bool
	// hold, when set, stops the body after holdAt bytes until it closes
	hold   chan struct{}
	holdAt int

	mu     sync.Mutex
	ranges []string
}

func newSlowReleases(t *testing.T, name string, size, chunk int, pause time.Duration) *slowReleases {
	s := &slowReleases{name: name, body: payload(size), chunk: chunk, pause: pause}
	s.srv = httptest.NewTLSServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *slowReleases) serve(w http.ResponseWriter, r *http.Request) {
	switch strings.TrimPrefix(r.URL.Path, "/download/") {
	case "SHA256SUMS":
		fmt.Fprintf(w, "%x  %s\n", sha256.Sum256(s.body), s.name)
		return
	case s.name:
	default:
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	s.ranges = append(s.ranges, r.Header.Get("Range"))
	s.mu.Unlock()
	start := 0
	if rg := r.Header.Get("Range"); rg != "" {
		fmt.Sscanf(rg, "bytes=%d-", &start)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(s.body)-1, len(s.body)))
		if !s.noLength {
			w.Header().Set("Content-Length", fmt.Sprint(len(s.body)-start))
		}
		w.WriteHeader(http.StatusPartialContent)
	} else if !s.noLength {
		w.Header().Set("Content-Length", fmt.Sprint(len(s.body)))
	}
	fl := w.(http.Flusher)
	for off := start; off < len(s.body); off += s.chunk {
		if s.hold != nil && off >= s.holdAt {
			select {
			case <-s.hold:
			case <-r.Context().Done():
				return
			}
		}
		if _, err := w.Write(s.body[off:min(off+s.chunk, len(s.body))]); err != nil {
			return
		}
		fl.Flush()
		select {
		case <-time.After(s.pause):
		case <-r.Context().Done():
			return
		}
	}
}

func (s *slowReleases) release(tag string, size int64) Release {
	return Release{Tag: tag, Assets: []Asset{
		{Name: s.name, URL: s.srv.URL + "/download/" + s.name, Size: size},
		{Name: "SHA256SUMS", URL: s.srv.URL + "/download/SHA256SUMS"},
	}}
}

func (s *slowReleases) updater(t *testing.T) *Updater {
	return &Updater{Dir: t.TempDir(), UserAgent: "clack/v0.1.0", Transport: s.srv.Client().Transport,
		Hosts: []string{"127.0.0.1"}, Backoff: time.Millisecond}
}

type progressLog struct {
	mu  sync.Mutex
	evs []Progress
	at  []time.Time
}

func (l *progressLog) add(p Progress) {
	l.mu.Lock()
	l.evs, l.at = append(l.evs, p), append(l.at, time.Now())
	l.mu.Unlock()
}

func (l *progressLog) get() []Progress {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Progress(nil), l.evs...)
}

// stages lists the stages in the order they first appear.
func stages(evs []Progress) []Stage {
	var out []Stage
	for _, e := range evs {
		if len(out) == 0 || out[len(out)-1] != e.Stage {
			out = append(out, e.Stage)
		}
	}
	return out
}

// A large asset served slowly: progress comes through each stage in
// order, the byte counts only grow and end at the size, and they're
// throttled (about 10 a second), not one per chunk.
func TestDownloadProgressSlow(t *testing.T) {
	noInstall(t)
	const size = 3 << 20
	s := newSlowReleases(t, "Clack-0.2.0-x86_64.AppImage", size, 32<<10, 4*time.Millisecond) // 96 chunks, ~0.4s+
	u := s.updater(t)
	var log progressLog
	u.Progress = log.add
	r := s.release("v0.2.0", size)
	start := time.Now()
	pkg, err := u.Download(context.Background(), r, r.Assets[0])
	if err != nil {
		t.Fatal(err)
	}
	took := time.Since(start)
	if b, _ := os.ReadFile(pkg.Path); !bytes.Equal(b, s.body) {
		t.Fatal("download differs")
	}
	evs := log.get()
	if got, want := fmt.Sprint(stages(evs)), fmt.Sprint([]Stage{StageDownloading, StageVerifying, StageStaging, StageReady}); got != want {
		t.Fatalf("stages %s, want %s", got, want)
	}
	var dl, ver []Progress
	for _, e := range evs {
		switch e.Stage {
		case StageDownloading:
			dl = append(dl, e)
		case StageVerifying:
			ver = append(ver, e)
		}
	}
	for i, e := range dl {
		if e.Total != size {
			t.Errorf("event %d: total %d, want the declared %d", i, e.Total, size)
		}
		if i > 0 && e.Done < dl[i-1].Done {
			t.Errorf("event %d: done went back from %d to %d", i, dl[i-1].Done, e.Done)
		}
	}
	if last := dl[len(dl)-1]; last.Done != size {
		t.Errorf("last download event %+v, want all %d bytes", last, size)
	}
	if last := ver[len(ver)-1]; last.Done != size || last.Total != size {
		t.Errorf("last verify event %+v", last)
	}
	// throttled: 96 chunks, but at most one event per 100ms plus each
	// fetch's first and last
	limit := int(took/(100*time.Millisecond)) + 4
	if len(dl) > limit || len(dl) >= 96 {
		t.Errorf("%d download events in %v (want ≤ %d): not throttled", len(dl), took, limit)
	}
	if len(dl) < 3 {
		t.Errorf("only %d download events in %v: no progress in between", len(dl), took)
	}
	// already downloaded: verifying, then ready, no bytes fetched
	var again progressLog
	u.Progress = again.add
	s.mu.Lock()
	n := len(s.ranges)
	s.mu.Unlock()
	if _, err := u.Download(context.Background(), r, r.Assets[0]); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(stages(again.get())); got != fmt.Sprint([]Stage{StageDownloading, StageVerifying, StageReady}) {
		t.Errorf("cached download stages %s", got)
	}
	s.mu.Lock()
	if len(s.ranges) != n {
		t.Error("a verified download was fetched again")
	}
	s.mu.Unlock()
}

// No declared size and no Content-Length: Total stays 0 (the UI shows an
// indeterminate bar) while Done counts up; with only Content-Length, that
// is the total.
func TestDownloadProgressUnknownSize(t *testing.T) {
	noInstall(t)
	const size = 600 << 10
	s := newSlowReleases(t, "clack_0.2.0_amd64.deb", size, 16<<10, time.Millisecond)
	s.noLength = true
	u := s.updater(t)
	u.ProgressEvery = time.Millisecond
	var log progressLog
	u.Progress = log.add
	r := s.release("v0.2.0", 0)
	if _, err := u.Download(context.Background(), r, r.Assets[0]); err != nil {
		t.Fatal(err)
	}
	var last Progress
	for _, e := range log.get() {
		if e.Stage == StageDownloading {
			if e.Total != 0 {
				t.Fatalf("unknown size reported a total: %+v", e)
			}
			last = e
		}
	}
	if last.Done != size {
		t.Errorf("last download event %+v, want %d bytes", last, size)
	}

	s2 := newSlowReleases(t, "clack_0.2.0_amd64.deb", size, 64<<10, time.Millisecond)
	u2 := s2.updater(t)
	var log2 progressLog
	u2.Progress = log2.add
	r2 := s2.release("v0.2.0", 0)
	if _, err := u2.Download(context.Background(), r2, r2.Assets[0]); err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, e := range log2.get() {
		if e.Stage == StageDownloading && e.Done > 0 {
			seen = true
			if e.Total != size {
				t.Errorf("Content-Length %d, total %+v", size, e)
			}
		}
	}
	if !seen {
		t.Error("no download progress")
	}
}

// Cancelling the context stops a download mid-body right away, keeps the
// partial file (no package), and the next download resumes from it.
func TestDownloadProgressCancel(t *testing.T) {
	noInstall(t)
	const size = 2 << 20
	s := newSlowReleases(t, "Clack-0.2.0-windows-amd64.zip", size, 64<<10, time.Millisecond)
	s.hold, s.holdAt = make(chan struct{}), 512<<10
	u := s.updater(t)
	u.ProgressEvery = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reached := make(chan struct{})
	var once sync.Once
	u.Progress = func(p Progress) {
		// (the throttle may hold the exact count at the hold: half is enough)
		if p.Stage == StageDownloading && p.Done >= int64(s.holdAt/2) {
			once.Do(func() { close(reached) })
		}
	}
	r := s.release("v0.2.0", size)
	done := make(chan error, 1)
	go func() { _, err := u.Download(ctx, r, r.Assets[0]); done <- err }()
	select {
	case <-reached:
	case <-time.After(10 * time.Second):
		t.Fatal("the download never got to the hold")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a cancelled download succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel didn't stop the download")
	}
	if _, err := os.Stat(u.Path("v0.2.0", s.name)); !os.IsNotExist(err) {
		t.Error("a cancelled download left a package")
	}
	fi, err := os.Stat(u.Path("v0.2.0", s.name) + ".part")
	if err != nil || fi.Size() < int64(s.holdAt/2) || fi.Size() >= size {
		t.Fatalf("partial file after cancel: %v %v", fi, err)
	}
	close(s.hold)
	u.Progress = nil
	pkg, err := u.Download(context.Background(), r, r.Assets[0])
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(pkg.Path); !bytes.Equal(b, s.body) {
		t.Fatal("resumed download differs")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if last := s.ranges[len(s.ranges)-1]; last != fmt.Sprintf("bytes=%d-", fi.Size()) {
		t.Errorf("didn't resume from %d: %q", fi.Size(), last)
	}
}

// The throttle: inside the interval only stage changes and forced
// (final) counts get through.
func TestDownloadProgressReporter(t *testing.T) {
	now := time.Unix(1000, 0)
	var got []Progress
	r := &reporter{fn: func(p Progress) { got = append(got, p) }, every: 100 * time.Millisecond, now: func() time.Time { return now }}
	r.report(Progress{Stage: StageDownloading, Total: 10}, false) // first: sent
	for i := int64(1); i <= 5; i++ {
		now = now.Add(10 * time.Millisecond)
		r.report(Progress{Stage: StageDownloading, Done: i, Total: 10}, false) // held
	}
	now = now.Add(60 * time.Millisecond)
	r.report(Progress{Stage: StageDownloading, Done: 6, Total: 10}, false) // 110ms: sent
	r.report(Progress{Stage: StageDownloading, Done: 7, Total: 10}, false) // held
	r.report(Progress{Stage: StageDownloading, Done: 10, Total: 10}, true) // forced
	r.report(Progress{Stage: StageVerifying, Total: 10}, false)            // new stage
	if want := "[{downloading 0 10} {downloading 6 10} {downloading 10 10} {verifying 0 10}]"; fmt.Sprint(got) != want {
		t.Errorf("reported %v, want %s", got, want)
	}
	var nilRep *reporter
	nilRep.report(Progress{}, true) // no Progress set: nothing, no panic
}
