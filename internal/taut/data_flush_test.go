package taut

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A flush that finds nothing to write, because the debounce timer's flush
// took the dirty data first, returns only once that flush is on disk:
// Close ends with a flush, and nothing of the Taut's may land in its
// directory after Close returns. Before, the second flush returned at
// once while the first was still writing, and a test's TempDir cleanup
// failed "unlinkat .../001: directory not empty" (train-138, run
// 44960329088: TestPatchedReactionPillLikeNative/dark/cls, whose skip
// reached the cleanup while a flush was writing storage/).
func TestDataFlushWaitsForOneWriting(t *testing.T) {
	d := newDataStore(t.TempDir())
	writing, release := make(chan struct{}), make(chan struct{})
	// the first flush to get here is held; any other passes (a sync.Once
	// would hold them all: its Do waits for the first call)
	var held atomic.Bool
	hold := func() {
		if held.CompareAndSwap(false, true) {
			close(writing)
			<-release
		}
	}
	dataFlushHook.Store(&hold)
	t.Cleanup(func() { dataFlushHook.Store(nil) })
	d.write("plugin:p:storage", "k", "v")
	first := make(chan error, 1)
	go func() { first <- d.flush() }() // as the debounce timer's, held in its writes
	<-writing
	second := make(chan error, 1)
	go func() { second <- d.flush() }() // as Close's
	select {
	case err := <-second:
		close(release)
		<-first
		t.Fatalf("a flush returned (%v) while another was still writing: Close would return before its files are on disk", err)
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(d.fileFor("plugin:p:storage")); err != nil {
		t.Fatalf("after both flushes: %v", err)
	}
}

// A write that fails stays to be written: the namespace stays dirty, the
// next flush writes it (Close's too), and the debounce timer tries again
// by itself. Before, the dirty set was cleared before writing, and one
// failed rename (a Windows sharing violation, a full disk) left the data
// off disk until something else in the namespace changed.
func TestDataFlushKeepsWhatFailed(t *testing.T) {
	d := newDataStore(t.TempDir())
	path := d.fileFor("indexedDB")
	// a directory where the file goes: the rename over it fails
	if err := os.MkdirAll(filepath.Join(path, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	d.write("indexedDB", `m["db"]`, "v1")
	if err := d.flush(); err == nil {
		t.Fatal("the flush over a directory succeeded")
	}
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	// the retry timer's flush writes it, with nothing new written meanwhile
	eventually(t, 5*time.Second, "the failed write tried again", func() bool {
		b, err := os.ReadFile(path)
		return err == nil && strings.Contains(string(b), "v1")
	})
	// after close nothing is scheduled: a failed write isn't tried again
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(path, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	d.write("indexedDB", `m["db"]`, "v2")
	if err := closeData(d); err == nil {
		t.Fatal("close's flush over a directory succeeded")
	}
	d.mu.Lock()
	timer := d.timer
	d.mu.Unlock()
	if timer != nil {
		t.Error("close left a flush scheduled")
	}
}

// A namespace file that can't be read is set aside as <file>.corrupt, not
// written over: before, it read as empty and the next flush replaced it,
// IndexedDB's taking every plugin's databases with it.
func TestDataUnreadableFileKept(t *testing.T) {
	dir := t.TempDir()
	d := newDataStore(dir)
	path := d.fileFor("indexedDB")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"m[\"db\"]": "half-writ`), 0o600); err != nil {
		t.Fatal(err)
	}
	if keys := d.list("indexedDB"); len(keys) != 0 {
		t.Fatalf("an unreadable file read as %q", keys)
	}
	d.write("indexedDB", "new", "v")
	if err := d.flush(); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(path + ".corrupt"); err != nil || !strings.Contains(string(b), "half-writ") {
		t.Errorf("the unreadable file wasn't kept aside: %q, %v", b, err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != `{"new":"v"}` {
		t.Errorf("the new file: %q, %v", b, err)
	}
}
