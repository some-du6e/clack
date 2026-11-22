package taut

import (
	"os"
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
