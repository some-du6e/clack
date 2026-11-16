package ui

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A task still stuck at the second mark gets a second snapshot in its
// report. Each snapshot has the Go runtime's stop-the-world figures, the
// JS runtimes and when the stacks were asked for and got.
func TestWatchdogSecondSnapshot(t *testing.T) {
	home := t.TempDir()
	wd := newWatchdog(home, time.Now())
	wd.after, wd.second = 300*time.Millisecond, 900*time.Millisecond
	got := make(chan string, 4)
	wd.written = func(p string) { got <- p }
	go wd.run()
	defer wd.stop()
	wd.begin(&stageLayout)
	var path string
	select {
	case path = <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("no report for a task stuck past the limit")
	}
	read := func() string {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	for deadline := time.Now().Add(5 * time.Second); !strings.Contains(read(), "a second snapshot"); {
		if time.Now().After(deadline) {
			t.Fatalf("no second snapshot for a task stuck past %v:\n%.3000s", wd.second, read())
		}
		time.Sleep(50 * time.Millisecond)
	}
	wd.next(&stageSubmit)
	time.Sleep(600 * time.Millisecond)
	wd.end()
	time.Sleep(600 * time.Millisecond)
	s := read()
	for _, want := range []string{
		"Go runtime: heap",
		"waiting for every goroutine to stop, for a GC: ",
		"waiting for every goroutine to stop, for anything else",
		"JS runtimes",
		"stacks: asked for at ",
		"Still blocked after ",
		"responsive again",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("report lacks %q:\n%.3000s", want, s)
		}
	}
	if n := strings.Count(s, "stacks: asked for at "); n != 2 {
		t.Errorf("%d snapshots in the report, want 2 (no third one)", n)
	}
	if n := strings.Count(s, "a second snapshot"); n != 1 {
		t.Errorf("%d second snapshots, want 1", n)
	}
}

// Reports are kept by age and by total size, not by count; the newest
// always stays, and files that aren't reports are left alone.
func TestWatchdogPrune(t *testing.T) {
	home := t.TempDir()
	wd := newWatchdog(home, time.Now())
	wd.keepMax = 10 << 10
	dir := filepath.Join(home, "freeze-reports")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	mk := func(name string, size int, age time.Duration) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, bytes.Repeat([]byte("x"), size), 0o600); err != nil {
			t.Fatal(err)
		}
		mt := time.Now().Add(-age)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	names := func() []string {
		ents, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, e := range ents {
			out = append(out, e.Name())
		}
		return out
	}
	// twelve small reports from the last hour all stay (the old limit was
	// ten); one from last week goes
	for i := 0; i < 12; i++ {
		mk(fmt.Sprintf("freeze-20261115-1000%02d.000.txt", i), 100, time.Hour)
	}
	mk("freeze-20261107-100000.000.txt", 100, 8*24*time.Hour)
	mk("notes.txt", 100, 30*24*time.Hour)
	wd.prune()
	got := strings.Join(names(), " ")
	if n := strings.Count(got, "freeze-20261115-"); n != 12 {
		t.Errorf("%d recent reports kept, want 12: %s", n, got)
	}
	if strings.Contains(got, "freeze-20261107-") {
		t.Errorf("a report older than %v kept: %s", wd.keepAge, got)
	}
	if !strings.Contains(got, "notes.txt") {
		t.Errorf("a file that isn't a report was deleted: %s", got)
	}
	// a 9 KiB report: with a 10 KiB budget, the ten newest small ones fit
	// beside it and the two oldest go
	mk("freeze-20261115-110000.000.txt", 9<<10, 0)
	wd.prune()
	got = strings.Join(names(), " ")
	if n := strings.Count(got, "freeze-20261115-1000"); n != 10 {
		t.Errorf("%d small reports beside the big one, want 10: %s", n, got)
	}
	for _, gone := range []string{"freeze-20261115-100000.000.txt", "freeze-20261115-100001.000.txt"} {
		if strings.Contains(got, gone) {
			t.Errorf("%s (among the oldest) kept past the size budget: %s", gone, got)
		}
	}
	// the newest stays even when it alone is over the budget
	mk("freeze-20261115-120000.000.txt", 11<<10, 0)
	wd.prune()
	got = strings.Join(names(), " ")
	if !strings.Contains(got, "freeze-20261115-120000.000.txt") || strings.Contains(got, "freeze-20261115-110000") {
		t.Errorf("after an over-budget report: %s", got)
	}
}
