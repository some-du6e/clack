package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// readRecords reads every record under dir (an artifact per job).
func readRecords(dir string) ([]Record, error) {
	var recs []Record
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && p == dir {
				return fs.SkipAll
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".json") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var r Record
		if err := json.Unmarshal(b, &r); err != nil {
			return fmt.Errorf("%s: %v", p, err)
		}
		recs = append(recs, r)
		return nil
	})
	recs = latestAttempts(recs)
	sort.SliceStable(recs, func(i, j int) bool {
		if recs[i].Job != recs[j].Job {
			return recs[i].Job < recs[j].Job
		}
		return recs[i].Package < recs[j].Package
	})
	return recs, err
}

// latestAttempts keeps each leg's records of its latest attempt: a rerun
// of a proof run's failed jobs reruns those legs only, and the failed
// attempt's artifacts stay in the run beside the new ones.
func latestAttempts(recs []Record) []Record {
	latest := map[string]int{}
	for _, r := range recs {
		if r.Leg != "" && r.Attempt > latest[r.Leg] {
			latest[r.Leg] = r.Attempt
		}
	}
	out := recs[:0]
	for _, r := range recs {
		if r.Leg == "" || r.Attempt == latest[r.Leg] {
			out = append(out, r)
		}
	}
	return out
}

// seen is one listed test across records.
type seen struct {
	fails, passes, skips int
	where                []string
	head                 []string
}

func tally(l Listed, recs []Record) map[string]*seen {
	m := map[string]*seen{}
	for _, name := range l {
		m[name] = &seen{}
	}
	for _, r := range recs {
		for _, t := range r.Listed {
			s := m[t.Name]
			if s == nil {
				continue
			}
			s.fails += t.Fails + t.Open
			s.passes += t.Passes
			s.skips += t.Skips
			s.where = append(s.where, r.Job+": "+r.Package)
			if s.head == nil && t.failed() {
				s.head = t.Head
			}
		}
	}
	return m
}

// report writes the job's summary: each listed test it ran, with the
// first lines of its failure, and what failed that the list doesn't name.
func report(dir, summary string) int {
	recs, err := readRecords(filepath.Join(dir, "results"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "expect_fail: %v\n", err)
	}
	l, _ := parseListed(os.Getenv("EXPECT_FAIL"))
	var b strings.Builder
	fmt.Fprintf(&b, "### expect_fail: %s\n\n", jobName())
	writeTable(&b, l, recs, false)
	fmt.Print(b.String())
	appendFile(summary, b.String())
	return 0
}

// check is the run's verdict over every job's records: each listed test
// failed somewhere and passed nowhere, nothing else failed, and every job
// that ran succeeded. needs is toJSON(needs). It writes the run's summary
// and exits 1 on any miss.
func check(dir, list, needs, summary string, out io.Writer) int {
	l, err := parseListed(list)
	if err != nil {
		fmt.Fprintf(out, "::error title=expect_fail::%v\n", err)
		return 1
	}
	recs, err := readRecords(dir)
	var errs []string
	if err != nil {
		errs = append(errs, err.Error())
	}
	if len(recs) == 0 {
		errs = append(errs, "no test binary ran under expect_fail: no job ran (check only=), or the jobs run their tests some other way (go test -c and the binary)")
	}
	for _, r := range recs {
		for _, u := range r.Unexpected {
			errs = append(errs, fmt.Sprintf("%s: %s: %s, and expect_fail doesn't list it", r.Job, r.Package, u))
		}
		for _, v := range r.Violations {
			errs = append(errs, fmt.Sprintf("%s: %s: %s", r.Job, r.Package, v))
		}
	}
	m := tally(l, recs)
	for _, name := range l {
		s := m[name]
		if s.fails == 0 && s.passes == 0 && s.skips == 0 {
			errs = append(errs, name+": no job ran it (a name as go test -json reports it, run by a job this run started)")
		} else if s.fails == 0 {
			errs = append(errs, name+": it ran and never failed")
		}
	}
	if needs != "" {
		var jobs map[string]struct {
			Result string `json:"result"`
		}
		if err := json.Unmarshal([]byte(needs), &jobs); err != nil {
			errs = append(errs, "the jobs' results: "+err.Error())
		}
		ids := make([]string, 0, len(jobs))
		for id := range jobs {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			if r := jobs[id].Result; r != "success" && r != "skipped" {
				errs = append(errs, fmt.Sprintf("job %s: %s", id, r))
			}
		}
	}
	var b strings.Builder
	b.WriteString("## expect_fail\n\n")
	if len(errs) == 0 {
		fmt.Fprintf(&b, "Every listed test failed, and nothing else did: the proof holds.\n\n")
	} else {
		b.WriteString("**The proof doesn't hold:**\n\n")
		for _, e := range errs {
			fmt.Fprintf(&b, "- %s\n", e)
		}
		b.WriteString("\n")
	}
	writeTable(&b, l, recs, true)
	fmt.Fprint(out, b.String())
	appendFile(summary, b.String())
	for _, e := range errs {
		fmt.Fprintf(out, "::error title=expect_fail::%s\n", e)
	}
	if len(errs) > 0 {
		return 1
	}
	return 0
}

// writeTable lists each listed test: where it ran, how, and the first
// lines of its failure. all: tests no record has too (the run's check).
func writeTable(b *strings.Builder, l Listed, recs []Record, all bool) {
	m := tally(l, recs)
	for _, name := range l {
		s := m[name]
		if !all && len(s.where) == 0 {
			continue
		}
		res := "not run"
		switch {
		case s.passes > 0:
			res = fmt.Sprintf("passed %d, failed %d", s.passes, s.fails)
		case s.skips > 0:
			res = fmt.Sprintf("skipped %d", s.skips)
		case s.fails > 0:
			res = fmt.Sprintf("failed (%d)", s.fails)
		}
		fmt.Fprintf(b, "**%s**: %s", name, res)
		if len(s.where) > 0 {
			fmt.Fprintf(b, " in %s", strings.Join(dedupe(s.where), "; "))
		}
		b.WriteString("\n\n")
		if len(s.head) > 0 {
			b.WriteString("```\n")
			for _, h := range s.head {
				b.WriteString(strings.ReplaceAll(h, "```", "'''") + "\n")
			}
			b.WriteString("```\n\n")
		}
	}
	for _, r := range recs {
		for _, u := range r.Unexpected {
			fmt.Fprintf(b, "- unexpected in %s: %s: %s\n", r.Job, r.Package, u)
		}
		if len(r.Unexpected) > 0 && len(r.Crash) > 0 {
			b.WriteString("```\n" + strings.Join(r.Crash, "\n") + "\n```\n")
		}
	}
}

func dedupe(xs []string) []string {
	var out []string
	had := map[string]bool{}
	for _, x := range xs {
		if !had[x] {
			had[x] = true
			out = append(out, x)
		}
	}
	return out
}

func appendFile(path, s string) {
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "expect_fail: %v\n", err)
		return
	}
	defer f.Close()
	_, _ = f.WriteString(s)
}
