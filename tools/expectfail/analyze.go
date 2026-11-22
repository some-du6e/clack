package main

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// event is one line of `go tool test2json` output.
type event struct {
	Action  string
	Package string
	Test    string
	Output  string
}

// headLines is how many lines of a failure the summary shows.
const headLines = 12

// testState is one test's outcomes in one test binary's run.
type testState struct {
	Name   string
	Fails  int
	Passes int
	Skips  int
	// Open counts runs that started and never ended: the binary crashed,
	// timed out or exited inside them.
	Open int
	// Head is the first lines of the test's first failure (its own
	// output, without the === and --- framing lines).
	Head []string
	// OwnOut: a failed run printed lines of its own (not its subtests'):
	// a parent that failed by itself, not only through a subtest
	OwnOut bool
	buf    []string
}

// binRun collects one test binary's events.
type binRun struct {
	tests map[string]*testState
	order []string
	// crash is the first lines of package-level output from a panic,
	// fatal error or timeout, for failures that printed nothing of
	// their own.
	crash []string
}

func newBinRun() *binRun { return &binRun{tests: map[string]*testState{}} }

func (r *binRun) test(name string) *testState {
	st := r.tests[name]
	if st == nil {
		st = &testState{Name: name}
		r.tests[name] = st
		r.order = append(r.order, name)
	}
	return st
}

var crashStart = regexp.MustCompile(`^(panic: |fatal error: |SIGQUIT|unexpected fault|Exception 0x|\*\*\* Test killed)`)

func (r *binRun) add(e event) {
	if e.Test == "" {
		line := strings.TrimRight(e.Output, "\r\n")
		if e.Action == "output" && (len(r.crash) > 0 || crashStart.MatchString(line)) && len(r.crash) < headLines {
			r.crash = append(r.crash, line)
		}
		return
	}
	st := r.test(e.Test)
	switch e.Action {
	case "run":
		st.Open++
		st.buf = st.buf[:0]
	case "output":
		line := strings.TrimRight(e.Output, "\r\n")
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "=== ") || strings.HasPrefix(t, "--- ") || t == "" {
			return
		}
		if len(st.buf) < headLines {
			st.buf = append(st.buf, line)
		}
		if crashStart.MatchString(t) && len(r.crash) == 0 {
			r.crash = append(r.crash, line)
		}
	case "pass", "fail", "skip":
		if st.Open > 0 {
			st.Open--
		}
		switch e.Action {
		case "pass":
			st.Passes++
		case "skip":
			st.Skips++
		case "fail":
			st.Fails++
			if len(st.buf) > 0 {
				st.OwnOut = true
			}
			if st.Head == nil {
				st.Head = append([]string{}, st.buf...)
			}
		}
	}
}

// Listed is a list of expected-to-fail test names: TestA, TestB/sub.
type Listed []string

var testName = regexp.MustCompile(`^(Test|Example|Fuzz)[^\s,|^$()*+?\[\]{}\\]*$`)

// parseListed reads expect_fail's value: test names separated by commas
// (spaces and newlines also separate). It refuses -run regexes, names that
// aren't tests and duplicates: the list is what the run must prove.
func parseListed(s string) (Listed, error) {
	var l Listed
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' || r == '\r' }) {
		if !testName.MatchString(f) {
			return nil, fmt.Errorf("%q is not a test name: list names as go test -json reports them (TestA,TestB/sub), not a -run regex", f)
		}
		if strings.HasSuffix(f, "/") || strings.Contains(f, "//") {
			return nil, fmt.Errorf("%q has an empty subtest name", f)
		}
		if slices.Contains(l, f) {
			return nil, fmt.Errorf("%q is listed twice", f)
		}
		l = append(l, f)
	}
	if len(l) == 0 {
		return nil, fmt.Errorf("no test names in %q", s)
	}
	return l, nil
}

// covers reports whether name is a listed test or a subtest of one.
func (l Listed) covers(name string) bool {
	for _, x := range l {
		if name == x || strings.HasPrefix(name, x+"/") {
			return true
		}
	}
	return false
}

// TestResult is a test's outcome in one binary run, as a record keeps it.
type TestResult struct {
	Name   string   `json:"name"`
	Fails  int      `json:"fails"`
	Passes int      `json:"passes"`
	Skips  int      `json:"skips"`
	Open   int      `json:"open,omitempty"`
	Head   []string `json:"head,omitempty"`
}

func (t TestResult) failed() bool { return t.Fails > 0 || t.Open > 0 }

// Record is one test binary's run under expect_fail, written to
// $XF_DIR/results for the job's report and the run's check.
type Record struct {
	Job string `json:"job"`
	// Leg is the job's matrix leg (job id and strategy.job-index) and
	// Attempt the run's attempt: a rerun of failed jobs leaves the failed
	// attempt's records in the run, and the check keeps each leg's latest
	Leg     string `json:"leg,omitempty"`
	Attempt int    `json:"attempt,omitempty"`
	Package string `json:"package"`
	Exit    int    `json:"exit"`
	// Masked: the binary failed, every failure was a listed test's, and
	// the wrapper exited 0.
	Masked bool `json:"masked"`
	// Listed holds the listed tests this binary ran.
	Listed []TestResult `json:"listed,omitempty"`
	// Unexpected holds what failed that the list doesn't explain.
	Unexpected []string `json:"unexpected,omitempty"`
	// Violations holds listed tests that passed or were skipped.
	Violations []string `json:"violations,omitempty"`
	Crash      []string `json:"crash,omitempty"`
}

// OK reports whether the binary's run is consistent with the list.
func (r Record) OK() bool { return len(r.Unexpected) == 0 && len(r.Violations) == 0 }

// verdict judges one binary run: exit is its exit status.
//
// A test failed if it has a fail event, or if the binary exited non-zero
// while it was still running (a crash, os.Exit or a timeout inside it).
// A failed test is explained if it's listed, a subtest of a listed test,
// or the parent of an explained failure (a subtest's failure fails its
// parents). Anything else that failed is unexpected; so is a non-zero exit
// with no failed test at all (TestMain, init). A listed test that passed or
// was skipped, in any of its runs (-count), is a violation.
func verdict(r *binRun, exit int, l Listed) Record {
	rec := Record{Exit: exit, Crash: r.crash}
	var failed []string
	for _, name := range r.order {
		st := r.tests[name]
		if st.Fails > 0 || (exit != 0 && st.Open > 0) {
			failed = append(failed, name)
		}
	}
	explained := map[string]bool{}
	for _, f := range failed {
		if l.covers(f) {
			explained[f] = true
		}
	}
	// a parent fails when a subtest does: a listed subtest's explains it,
	// unless the parent printed lines of its own (its own t.Error, or a
	// t.Log, which test2json can't tell apart): then list the parent
	ownOut := map[string]bool{}
	for _, f := range failed {
		if explained[f] {
			continue
		}
		for g := range explained {
			if strings.HasPrefix(g, f+"/") {
				if r.tests[f].OwnOut {
					ownOut[f] = true
				} else {
					explained[f] = true
				}
				break
			}
		}
	}
	for _, f := range failed {
		if !explained[f] {
			st := r.tests[f]
			why := "failed"
			switch {
			case ownOut[f]:
				why = "failed with output of its own, besides its listed subtests' (list " + f + " to cover it)"
			case st.Fails == 0:
				why = "was still running when the binary exited " + fmt.Sprint(exit)
			}
			rec.Unexpected = append(rec.Unexpected, f+" "+why)
		}
	}
	if exit != 0 && len(failed) == 0 {
		rec.Unexpected = append(rec.Unexpected, fmt.Sprintf("the test binary exited %d with no failed test (TestMain, init or a crash between tests)", exit))
	}
	for _, name := range r.order {
		st := r.tests[name]
		if !slices.Contains(l, name) {
			continue
		}
		open := 0
		if exit != 0 {
			open = st.Open
		}
		head := st.Head
		if len(head) == 0 && open > 0 {
			head = r.crash
		}
		rec.Listed = append(rec.Listed, TestResult{Name: name, Fails: st.Fails, Passes: st.Passes, Skips: st.Skips, Open: open, Head: head})
		if st.Passes > 0 {
			rec.Violations = append(rec.Violations, fmt.Sprintf("%s passed (%d of %d runs): expect_fail says it fails", name, st.Passes, st.Passes+st.Fails+st.Skips+open))
		}
		if st.Skips > 0 {
			rec.Violations = append(rec.Violations, fmt.Sprintf("%s was skipped: a skip proves nothing", name))
		}
	}
	sort.Strings(rec.Unexpected)
	rec.Masked = exit != 0 && rec.OK()
	return rec
}
