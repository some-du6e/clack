//go:build windows

package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// heldPrefs is a ui.json holding before in a folder of its own, opened by
// a reader as Go's os.Open opens it, without FILE_SHARE_DELETE (the
// bounds tests' poll, a scanner). It checks what the tests below depend
// on: while the reader has it open, Windows refuses a rename over it, with
// an error writePrefsFile tries again.
func heldPrefs(t *testing.T, before string) (p string, f *os.File) {
	t.Helper()
	dir := t.TempDir()
	p = filepath.Join(dir, "ui.json")
	if err := os.WriteFile(p, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	other := filepath.Join(dir, "other.json")
	if err := os.WriteFile(other, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	switch err := os.Rename(other, p); {
	case err == nil:
		t.Fatal("a rename over ui.json went in while a reader had it open: nothing holds a write back")
	case !renameRefused(p, err):
		t.Fatalf("a rename over ui.json while a reader had it open: %v, which writePrefsFile doesn't try again", err)
	default:
		t.Logf("a rename over ui.json while a reader has it open: %v", err)
	}
	return p, f
}

// untilRefusals waits up to d for n renames of ui.json.tmp refused since
// base.
func untilRefusals(t *testing.T, base, n int64, d time.Duration) {
	t.Helper()
	for deadline := time.Now().Add(d); prefsRenameRefusals.Load() < base+n; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("%d refused renames of ui.json.tmp within %v, want %d", prefsRenameRefusals.Load()-base, d, n)
		}
	}
}

// Windows refuses to replace ui.json while another handle has it open
// without FILE_SHARE_DELETE. The write's rename is refused, tried again,
// and goes in once the reader lets go, rather than being lost
// (writePrefsFile).
func TestPrefsWriteWhileOpen(t *testing.T) {
	p, f := heldPrefs(t, `{"before":true}`)
	base := prefsRenameRefusals.Load()
	done := make(chan struct{})
	go func() { writePrefsFile(p, []byte(`{"after":true}`), nil); close(done) }()
	untilRefusals(t, base, 1, 5*time.Second) // the reader lets go once the write was refused
	f.Close()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the write never returned")
	}
	if b, _ := os.ReadFile(p); string(b) != `{"after":true}` {
		t.Errorf("ui.json after a write while a reader had it open: %s, want the write's", b)
	}
}

// A write still trying a refused rename gives way at once when newer
// bytes come (prefsSaver.retryWait): they replace it, so the flush at
// quit, which has 3 s when Windows ends the session, waits out the
// newest write's tries alone. The first write's seventh refusal leaves
// it 640 ms to wait; the newer bytes' rename comes well before that.
func TestPrefsWriteGivesWayToNewer(t *testing.T) {
	p, f := heldPrefs(t, `{"before":true}`)
	s := &prefsSaver{path: p, pending: []byte(`{"first":true}`), busy: true, done: make(chan struct{})}
	base := prefsRenameRefusals.Load()
	go s.loop()
	untilRefusals(t, base, 7, 5*time.Second)
	s.save(p, []byte(`{"second":true}`))
	untilRefusals(t, base, 8, 300*time.Millisecond)
	f.Close()
	flushed := make(chan struct{})
	go func() { s.flush(); close(flushed) }()
	select {
	case <-flushed:
	case <-time.After(10 * time.Second):
		t.Fatal("the writer never went idle")
	}
	if b, _ := os.ReadFile(p); string(b) != `{"second":true}` {
		t.Errorf("ui.json: %s, want the newer write's", b)
	}
}

// A refusal that outlasts the tries (8, about 1.3 s) ends the write,
// logged, with ui.json as it was.
func TestPrefsWriteRefusalLasts(t *testing.T) {
	p, _ := heldPrefs(t, `{"before":true}`)
	base := prefsRenameRefusals.Load()
	start := time.Now()
	writePrefsFile(p, []byte(`{"after":true}`), nil)
	took := time.Since(start)
	if n := prefsRenameRefusals.Load() - base; n != 8 {
		t.Errorf("%d refused renames before the write gave up, want 8", n)
	}
	if took < time.Second || took > 10*time.Second {
		t.Errorf("the write gave up after %v, want its tries' 1.27 s", took)
	}
	if b, _ := os.ReadFile(p); string(b) != `{"before":true}` {
		t.Errorf("ui.json after a write that gave up: %s, want it as it was", b)
	}
}

// A read-only ui.json refuses the rename for good: the write fails at
// once, never tried again (it took every quit about 1.3 s).
func TestPrefsWriteReadOnlyFailsAtOnce(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ui.json")
	if err := os.WriteFile(p, []byte(`{"before":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o600) }) // before TempDir's removal
	base := prefsRenameRefusals.Load()
	start := time.Now()
	writePrefsFile(p, []byte(`{"after":true}`), nil)
	if n := prefsRenameRefusals.Load() - base; n != 0 {
		t.Errorf("the rename over a read-only ui.json was tried again %d times", n)
	}
	if took := time.Since(start); took > 300*time.Millisecond {
		t.Errorf("a write over a read-only ui.json took %v, want it to fail at once", took)
	}
	b, _ := os.ReadFile(p)
	t.Logf("a read-only ui.json after the write: %s", b)
}
