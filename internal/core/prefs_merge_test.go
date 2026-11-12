package core

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
)

// Prefs written at once all stay in core: each write copies the raw prefs,
// sets its key and installs the copy, and without one lock over that two
// finishing together kept only the later copy (mergePrefs; ui's
// TestAccountThemeWriteBack read "core still has gradients on", CI
// 2026-11-12).
func TestSetPrefConcurrentKeepsEvery(t *testing.T) {
	a, _ := canvasApp(t)
	ctx := context.Background()
	const n = 32
	name := func(i int) string { return fmt.Sprintf("clack_test_pref_%02d", i) }
	var wg sync.WaitGroup
	errs := make(chan error, n)
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- a.SetPref(ctx, name(i), i)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var lost []string
	for i := range n {
		if v, ok := a.Pref(name(i)); !ok || string(v) != strconv.Itoa(i) {
			lost = append(lost, name(i))
		}
	}
	if len(lost) > 0 {
		t.Errorf("%d of %d prefs written at once lost in core: %v", len(lost), n, lost)
	}
}
