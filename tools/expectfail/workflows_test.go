package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// proofWorkflows take expect_fail: the ones agents dispatch for red and
// mutant runs (2026-11-16..21's evidence branches), flake-check first.
var proofWorkflows = []string{"flake-check", "ci", "threads-audit", "files-view", "hover-card", "bk-gallery-shots",
	"ui-taut", "ch-details-shots", "ui-slick", "composer-taut", "bk-agent-shots"}

type wfJob struct {
	id    string
	lines []string
	steps [][]string
}

var (
	jobLine  = regexp.MustCompile(`^  ([A-Za-z0-9_-]+):\s*$`)
	stepLine = regexp.MustCompile(`^      - `)
)

// parseJobs splits a workflow's jobs and their steps by indentation (the
// repo's workflows are all laid out alike: jobs at 2, steps at 6).
func parseJobs(t *testing.T, text string) []wfJob {
	t.Helper()
	lines := strings.Split(text, "\n")
	i := slices.Index(lines, "jobs:")
	if i < 0 {
		t.Fatal("no jobs:")
	}
	var jobs []wfJob
	for _, l := range lines[i+1:] {
		if m := jobLine.FindStringSubmatch(l); m != nil {
			jobs = append(jobs, wfJob{id: m[1]})
			continue
		}
		if len(jobs) == 0 || strings.HasPrefix(l, "  #") || strings.TrimSpace(l) == "" {
			continue
		}
		j := &jobs[len(jobs)-1]
		j.lines = append(j.lines, l)
		switch {
		case stepLine.MatchString(l):
			j.steps = append(j.steps, []string{l})
		case len(j.steps) > 0 && strings.HasPrefix(l, "        "):
			j.steps[len(j.steps)-1] = append(j.steps[len(j.steps)-1], l)
		}
	}
	return jobs
}

func uses(step []string, action string) bool {
	for _, l := range step {
		if strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "- ")) == "uses: "+action {
			return true
		}
	}
	return false
}

func readWorkflow(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", name+".yml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Every workflow that takes expect_fail runs it in every job that sets up
// Go: the setup action right after setup-go (before any go test), the
// report last, and the expect-fail job needing every job, so its check
// sees all of them. A job added without them would run its tests unjudged
// (a proof run's listed failures red) or unchecked (missing from needs).
func TestWorkflowsWireExpectFail(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", ".github", "workflows", "*.yml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("workflows: %v %d", err, len(files))
	}
	var wired []string
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".yml")
		text := readWorkflow(t, name)
		takes := regexp.MustCompile(`(?m)^      expect_fail:\s*$`).MatchString(text)
		refs := strings.Contains(text, "./.github/actions/expect-fail")
		if !takes {
			if refs {
				t.Errorf("%s: uses the expect-fail actions without the expect_fail input", name)
			}
			continue
		}
		wired = append(wired, name)
		jobs := parseJobs(t, text)
		var ids []string
		var check *wfJob
		for k := range jobs {
			j := &jobs[k]
			if j.id == "expect-fail" {
				check = j
				continue
			}
			ids = append(ids, j.id)
			sg := slices.IndexFunc(j.steps, func(s []string) bool { return strings.Contains(strings.Join(s, "\n"), "actions/setup-go@") })
			if sg < 0 {
				continue
			}
			if sg+1 >= len(j.steps) || !uses(j.steps[sg+1], "./.github/actions/expect-fail") ||
				!strings.Contains(strings.Join(j.steps[sg+1], "\n"), "tests: ${{ inputs.expect_fail }}") {
				t.Errorf("%s: job %s: the step after setup-go isn't ./.github/actions/expect-fail with tests: ${{ inputs.expect_fail }}", name, j.id)
			}
			last := j.steps[len(j.steps)-1]
			if !uses(last, "./.github/actions/expect-fail-report") || !strings.Contains(strings.Join(last, "\n"), "if: ${{ always() && env.XF_ID }}") {
				t.Errorf("%s: job %s: its last step isn't ./.github/actions/expect-fail-report with if: ${{ always() && env.XF_ID }}", name, j.id)
			}
		}
		if check == nil {
			t.Errorf("%s: no expect-fail job", name)
			continue
		}
		body := strings.Join(check.lines, "\n")
		for _, id := range ids {
			if !regexp.MustCompile(`(?m)^      - ` + regexp.QuoteMeta(id) + `\s*$`).MatchString(body) {
				t.Errorf("%s: the expect-fail job's needs lacks %s", name, id)
			}
		}
		if !strings.Contains(body, "if: ${{ always() && inputs.expect_fail != '' }}") {
			t.Errorf("%s: the expect-fail job runs on another condition", name)
		}
	}
	for _, w := range proofWorkflows {
		if !slices.Contains(wired, w) {
			t.Errorf("%s: no expect_fail input (agents dispatch it for proofs)", w)
		}
	}
}

// ci's and supply-chain's push runs skip evidence branches: a red branch's
// push run is red by design, and each one pings Karim's phone.
func TestEvidenceBranchesSkipPushRuns(t *testing.T) {
	for _, name := range []string{"ci", "supply-chain"} {
		text := readWorkflow(t, name)
		on := text[strings.Index(text, "\non:\n"):]
		on = on[:strings.Index(on, "\n  pull_request:")]
		for _, want := range []string{`branches-ignore: ["**-red", "**-red[0-9]*", "**-mut", "**-mut[0-9]*"]`, `tags: ["**"]`} {
			if !strings.Contains(on, want) {
				t.Errorf("%s: push lacks %s", name, want)
			}
		}
	}
	if !strings.Contains(readWorkflow(t, "ci"), "case $id in only-check | linux-coverage | expect-fail) continue ;; esac") {
		t.Error("ci's only-check counts expect-fail as a job only= can start")
	}
}

// The setup's evidence-branch rule matches the push filter's branches.
func TestEvidenceBranchPattern(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "actions", "expect-fail", "setup.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `=~ -(red|mut)[0-9]*$ ]]`) {
		t.Fatal("setup.sh's evidence-branch rule changed: keep it and the push filters alike")
	}
	re := regexp.MustCompile(`-(red|mut)[0-9]*$`)
	for _, c := range []struct {
		ref string
		ev  bool
	}{{"thread-load-speed-red4", true}, {"caret-align-mut1", true}, {"ci-speed-3-red", true}, {"quiet-ci", false}, {"ci-speed-2-grn8", false}, {"redraw-fix", false}, {"red-team", false}} {
		if re.MatchString(c.ref) != c.ev {
			t.Errorf("%s: evidence %v, want %v", c.ref, !c.ev, c.ev)
		}
	}
}
