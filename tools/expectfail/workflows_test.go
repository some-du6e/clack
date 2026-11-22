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

// wireHint is what each wiring failure says to do.
const wireHint = "python3 -I .github/scripts/expect_fail_wire.py .github/workflows/<file>.yml adds what's missing"

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
			setup := ""
			if sg+1 < len(j.steps) {
				setup = strings.Join(j.steps[sg+1], "\n")
			}
			if sg+1 >= len(j.steps) || !uses(j.steps[sg+1], "./.github/actions/expect-fail") ||
				!strings.Contains(setup, "tests: ${{ inputs.expect_fail }}") || !strings.Contains(setup, "leg: ${{ github.job }}-${{ strategy.job-index }}") {
				t.Errorf("%s: job %s: the step after setup-go isn't ./.github/actions/expect-fail with its tests and leg: %s", name, j.id, wireHint)
			}
			last := j.steps[len(j.steps)-1]
			if !uses(last, "./.github/actions/expect-fail-report") || !strings.Contains(strings.Join(last, "\n"), "if: ${{ always() && env.XF_ID }}") {
				t.Errorf("%s: job %s: its last step isn't ./.github/actions/expect-fail-report with if: ${{ always() && env.XF_ID }}: %s", name, j.id, wireHint)
			}
		}
		if check == nil {
			t.Errorf("%s: no expect-fail job", name)
			continue
		}
		body := strings.Join(check.lines, "\n")
		for _, id := range ids {
			if !regexp.MustCompile(`(?m)^      - ` + regexp.QuoteMeta(id) + `\s*$`).MatchString(body) {
				t.Errorf("%s: the expect-fail job's needs lacks %s: %s", name, id, wireHint)
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
		if !strings.Contains(on, `tags: ["**"]`) {
			t.Errorf("%s: push lacks tags: [\"**\"]", name)
		}
		m := regexp.MustCompile(`branches-ignore: \[([^\]\n]*(?:\][^\]\n]*)*)\]\n`).FindStringSubmatch(on)
		if m == nil {
			t.Errorf("%s: push has no branches-ignore", name)
			continue
		}
		var globs []string
		for _, g := range regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(m[1], -1) {
			globs = append(globs, g[1])
		}
		// the globs skip exactly the branches setup.sh calls evidence
		for _, b := range evidenceSamples {
			skip := slices.ContainsFunc(globs, func(g string) bool { return globMatch(g, b) })
			if ev := evidenceRule.MatchString(b); skip != ev {
				t.Errorf("%s: branch %s: push skipped %v, evidence %v", name, b, skip, ev)
			}
		}
	}
	if !strings.Contains(readWorkflow(t, "ci"), "case $id in only-check | linux-coverage | expect-fail) continue ;; esac") {
		t.Error("ci's only-check counts expect-fail as a job only= can start")
	}
}

const evidenceRegex = `-(r[0-9]+)?(red|mut)([0-9]+[a-z]?)?$`

var (
	evidenceRule    = regexp.MustCompile(evidenceRegex)
	evidenceSamples = []string{"thread-load-speed-red4", "caret-align-mut1", "ci-speed-3-red", "ci-speed-3-red13c", "ci-speed-2-red9b",
		"blockkit-coverage-r3red5", "blockkit-coverage-r3mut1", "x-r12red", "x-r3mut", "x-mut", "x-red1b", "x-red12cd", "x-red1-b",
		"msg-latency-3-red-minors", "red-badge", "quiet-ci", "ci-speed-2-grn8", "redraw-fix", "red-team", "x-redx", "x-r-red1", "x-rxred1",
		"feature/x-red2", "x-mut4z", "x-mutt", "ui", "main"}
)

// globMatch is GitHub's branch filter glob, as far as the push filters
// use it: ** any run, * any run without /, [..] a class, + one or more of
// what came before.
func globMatch(glob, s string) bool {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(glob); i++ {
		switch c := glob[i]; {
		case strings.HasPrefix(glob[i:], "**"):
			b.WriteString(".*")
			i++
		case c == '*':
			b.WriteString("[^/]*")
		case c == '[':
			j := strings.IndexByte(glob[i:], ']')
			b.WriteString(glob[i : i+j+1])
			i += j
		case c == '+':
			b.WriteString("+")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String()).MatchString(s)
}

// The setup's evidence-branch rule matches the push filter's branches.
func TestEvidenceBranchPattern(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "actions", "expect-fail", "setup.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `=~ `+evidenceRegex+` ]]`) {
		t.Fatal("setup.sh's evidence-branch rule changed: keep it, evidenceRegex and the push filters alike")
	}
	re := evidenceRule
	for _, c := range []struct {
		ref string
		ev  bool
	}{{"thread-load-speed-red4", true}, {"caret-align-mut1", true}, {"ci-speed-3-red", true},
		{"ci-speed-3-red13c", true}, {"ci-speed-2-red9b", true}, {"blockkit-coverage-r3red5", true}, {"blockkit-coverage-r3mut1", true},
		{"msg-latency-3-red-minors", false}, {"red-badge", false}, {"quiet-ci", false}, {"ci-speed-2-grn8", false}, {"redraw-fix", false}, {"red-team", false}} {
		if re.MatchString(c.ref) != c.ev {
			t.Errorf("%s: evidence %v, want %v", c.ref, !c.ev, c.ev)
		}
	}
}
