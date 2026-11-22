package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ev builds a test2json stream: "run TestA", "out TestA msg", "fail TestA",
// "pass", "skip", "out - panic: x" (package output).
func evs(lines ...string) []event {
	var out []event
	for _, l := range lines {
		f := strings.SplitN(l, " ", 3)
		e := event{Package: "p"}
		switch f[0] {
		case "run", "pass", "fail", "skip":
			e.Action, e.Test = f[0], f[1]
		case "out":
			e.Action = "output"
			if f[1] != "-" {
				e.Test = f[1]
			}
			e.Output = f[2] + "\n"
		}
		out = append(out, e)
	}
	return out
}

func judge(t *testing.T, list string, exit int, lines ...string) Record {
	t.Helper()
	l, err := parseListed(list)
	if err != nil {
		t.Fatal(err)
	}
	r := newBinRun()
	for _, e := range evs(lines...) {
		r.add(e)
	}
	return verdict(r, exit, l)
}

func TestVerdict(t *testing.T) {
	for _, c := range []struct {
		name       string
		list       string
		exit       int
		lines      []string
		ok, masked bool
		want       string // in Unexpected or Violations
	}{
		{"the listed test fails, nothing else", "TestA", 1,
			[]string{"run TestA", "out TestA x_test.go:3: want 1, got 2", "fail TestA", "run TestB", "pass TestB"}, true, true, ""},
		{"another test fails too", "TestA", 1,
			[]string{"run TestA", "fail TestA", "run TestB", "fail TestB"}, false, false, "TestB failed"},
		{"the listed test passes", "TestA", 0,
			[]string{"run TestA", "pass TestA"}, false, false, "TestA passed"},
		{"the listed test passes in one of its runs (-count)", "TestA", 1,
			[]string{"run TestA", "fail TestA", "run TestA", "pass TestA"}, false, false, "TestA passed (1 of 2 runs)"},
		{"the listed test is skipped", "TestA", 0,
			[]string{"run TestA", "skip TestA"}, false, false, "TestA was skipped"},
		{"a listed subtest fails its parent", "TestA/sub", 1,
			[]string{"run TestA", "run TestA/sub", "fail TestA/sub", "run TestA/ok", "pass TestA/ok", "fail TestA"}, true, true, ""},
		{"an unlisted sibling fails as well", "TestA/sub", 1,
			[]string{"run TestA", "run TestA/sub", "fail TestA/sub", "run TestA/other", "fail TestA/other", "fail TestA"}, false, false, "TestA/other failed"},
		{"a parent with a failure of its own beside a listed subtest", "TestA/sub", 1,
			[]string{"run TestA", "run TestA/sub", "fail TestA/sub", "out TestA a_test.go:30: the parent's own check", "fail TestA"}, false, false, "TestA failed with output of its own"},
		{"a listed parent covers its subtests", "TestA", 1,
			[]string{"run TestA", "run TestA/x", "fail TestA/x", "fail TestA"}, true, true, ""},
		{"a panic in the listed test", "TestA", 2,
			[]string{"run TestA", "fail TestA", "out - panic: boom [recovered]"}, true, true, ""},
		{"a timeout inside the listed test", "TestA", 2,
			[]string{"run TestB", "pass TestB", "run TestA", "out - panic: test timed out after 2s"}, true, true, ""},
		{"a timeout inside another test", "TestA", 2,
			[]string{"run TestA", "fail TestA", "run TestB", "out - panic: test timed out after 2s"}, false, false, "TestB was still running"},
		{"a binary failing with no failed test (TestMain)", "TestA", 1,
			[]string{"out - setup failed"}, false, false, "no failed test"},
		{"a binary that never ran the listed test", "TestA", 0,
			[]string{"run TestB", "pass TestB"}, true, false, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := judge(t, c.list, c.exit, c.lines...)
			if rec.OK() != c.ok || rec.Masked != c.masked {
				t.Errorf("ok %v masked %v, want %v %v: %+v", rec.OK(), rec.Masked, c.ok, c.masked, rec)
			}
			if c.want != "" && !strings.Contains(strings.Join(append(rec.Unexpected, rec.Violations...), "\n"), c.want) {
				t.Errorf("want %q in %q %q", c.want, rec.Unexpected, rec.Violations)
			}
		})
	}
}

// The listed test's record keeps its failure's first lines, not the
// === and --- framing.
func TestVerdictHead(t *testing.T) {
	rec := judge(t, "TestA", 1, "run TestA", "out TestA === RUN   TestA", "out TestA     a_test.go:9: got 2, want 1", "out TestA --- FAIL: TestA (0.00s)", "fail TestA")
	if len(rec.Listed) != 1 || len(rec.Listed[0].Head) != 1 || !strings.Contains(rec.Listed[0].Head[0], "a_test.go:9: got 2, want 1") {
		t.Fatalf("head %+v", rec.Listed)
	}
}

func TestParseListed(t *testing.T) {
	if l, err := parseListed(" TestA, TestB/sub_case\nExampleC "); err != nil || strings.Join(l, ",") != "TestA,TestB/sub_case,ExampleC" {
		t.Errorf("%q %v", l, err)
	}
	for _, bad := range []string{"", " , ", "^TestA$", "TestA|TestB", "TestA,TestA", "testA", "TestA/", "Test(A)", "BenchmarkA"} {
		if _, err := parseListed(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func writeRec(t *testing.T, dir string, r Record) {
	t.Helper()
	b, _ := json.Marshal(r)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.CreateTemp(dir, "*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(b); err != nil {
		t.Fatal(err)
	}
}

// The run's check across jobs: each listed test failed in some job and
// passed in none; nothing else failed; every job succeeded or was skipped.
func TestCheck(t *testing.T) {
	failA := Record{Job: "j1", Package: "p", Exit: 1, Masked: true, Listed: []TestResult{{Name: "TestA", Fails: 1, Head: []string{"boom"}}}}
	failB := Record{Job: "j2", Package: "q", Exit: 1, Masked: true, Listed: []TestResult{{Name: "TestB", Fails: 1}}}
	okNeeds := `{"j1":{"result":"success"},"j2":{"result":"success"},"j3":{"result":"skipped"}}`
	for _, c := range []struct {
		name  string
		list  string
		recs  []Record
		needs string
		code  int
		want  string
	}{
		{"both fail in their jobs", "TestA,TestB", []Record{failA, failB}, okNeeds, 0, "the proof holds"},
		{"one never ran", "TestA,TestB", []Record{failA}, okNeeds, 1, "TestB: no job ran it"},
		{"a job failed", "TestA", []Record{failA}, `{"j1":{"result":"failure"}}`, 1, "job j1: failure"},
		{"a job was cancelled", "TestA", []Record{failA}, `{"j1":{"result":"cancelled"}}`, 1, "job j1: cancelled"},
		{"nothing ran", "TestA", nil, `{"j1":{"result":"skipped"}}`, 1, "no test binary ran"},
		{"one passed somewhere", "TestA", []Record{failA, {Job: "j2", Package: "q", Listed: []TestResult{{Name: "TestA", Passes: 1}}, Violations: []string{"TestA passed (1 of 1 runs): expect_fail says it fails"}}}, okNeeds, 1, "TestA passed"},
		{"something else failed", "TestA", []Record{failA, {Job: "j2", Package: "q", Exit: 1, Unexpected: []string{"TestZ failed"}}}, okNeeds, 1, "TestZ failed"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			for i, r := range c.recs {
				writeRec(t, filepath.Join(dir, "expect-fail-"+string(rune('a'+i))), r)
			}
			sum := filepath.Join(t.TempDir(), "summary.md")
			var out bytes.Buffer
			if code := check(dir, c.list, c.needs, sum, &out); code != c.code {
				t.Errorf("exit %d, want %d: %s", code, c.code, out.String())
			}
			if !strings.Contains(out.String(), c.want) {
				t.Errorf("want %q in %s", c.want, out.String())
			}
			if b, _ := os.ReadFile(sum); !strings.Contains(string(b), "## expect_fail") {
				t.Errorf("no summary: %q", b)
			}
		})
	}
	// a rerun of the failed jobs: the leg's attempt 1 records (an extra
	// failure) give way to its attempt 2's; another leg's attempt 1 stays
	t.Run("a rerun's attempt replaces the leg's", func(t *testing.T) {
		dir := t.TempDir()
		writeRec(t, filepath.Join(dir, "a1"), Record{Job: "j1", Leg: "j1-0", Attempt: 1, Package: "p", Exit: 1, Unexpected: []string{"TestZ failed"}})
		writeRec(t, filepath.Join(dir, "a2"), Record{Job: "j1", Leg: "j1-0", Attempt: 2, Package: "p", Exit: 1, Masked: true, Listed: []TestResult{{Name: "TestA", Fails: 1}}})
		writeRec(t, filepath.Join(dir, "b1"), Record{Job: "j1", Leg: "j1-1", Attempt: 1, Package: "q", Exit: 1, Masked: true, Listed: []TestResult{{Name: "TestB", Fails: 1}}})
		var out bytes.Buffer
		if code := check(dir, "TestA,TestB", `{"j1":{"result":"success"}}`, "", &out); code != 0 {
			t.Errorf("exit %d: %s", code, out.String())
		}
	})
	// the summary shows the failure's first lines
	dir := t.TempDir()
	writeRec(t, filepath.Join(dir, "a"), failA)
	var out bytes.Buffer
	check(dir, "TestA", okNeeds, "", &out)
	if !strings.Contains(out.String(), "```\nboom\n```") {
		t.Errorf("no head in %s", out.String())
	}
}
