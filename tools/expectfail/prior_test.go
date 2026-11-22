package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeGH answers prior-full-test.sh's gh api calls from testdata/prior
// (jobs-<run>.json are recorded runs' jobs, trimmed to what the script
// reads) and the case's runs and commits; a missing file is a failed
// call. --jq applies as gh applies it (raw strings).
const fakeGH = `#!/usr/bin/env bash
path=$2
shift 2
[ "${1:-}" = --jq ] && expr=$2
case $path in
*ci.yml/runs*event=workflow_dispatch*) f=runs-dispatch.json ;;
*ci.yml/runs*branch=ui*) f=runs-ui.json ;;
*ci.yml/runs*branch=main*) f=runs-main.json ;;
*/git/commits/*) f=commit-${path##*/}.json ;;
*/actions/runs/*/jobs*) id=${path#*actions/runs/}; f=jobs-${id%%/*}.json ;;
*) exit 1 ;;
esac
[ -f "$FIX/$f" ] || { echo "gh: HTTP 502 ($path)" >&2; exit 1; }
jq -r "$expr" "$FIX/$f"
`

type priorRun struct {
	ID      int64  `json:"id"`
	Created string `json:"created_at"`
	Sha     string `json:"head_sha"`
	Branch  string `json:"head_branch"`
	Event   string `json:"event"`
}

// The release's test job skips the suite only on a clean full-test of
// its tree, never a proof run's (its expect-fail job ran: listed failures
// exit 0 there), and any API error runs the suite. Runs: 44955419010 a
// clean full-test from before quiet-ci (no expect-fail job), 44979462996
// a clean one with expect-fail skipped, 44995394154 a proof run
// (expect_fail=TestXFProofFails: full-test and expect-fail green).
func TestPriorFullTest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash and jq")
	}
	for _, tool := range []string{"bash", "jq", "git", "date"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("no %s", tool)
		}
	}
	script, err := filepath.Abs(filepath.Join("..", "..", ".github", "scripts", "prior-full-test.sh"))
	if err != nil {
		t.Fatal(err)
	}
	const tree, other = "1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222"
	recorded, err := filepath.Abs(filepath.Join("testdata", "prior"))
	if err != nil {
		t.Fatal(err)
	}
	run := func(id int64, created, branch, event string) priorRun {
		return priorRun{ID: id, Created: created, Sha: strings.Repeat(string(rune('a'+id%26)), 40), Branch: branch, Event: event}
	}
	pre := run(44955419010, "2026-11-21T21:00:00Z", "notify-file-meta", "workflow_dispatch")
	clean := run(44979462996, "2026-11-22T02:00:00Z", "quiet-ci", "workflow_dispatch")
	proof := run(44995394154, "2026-11-22T09:00:00Z", "quiet-ci-xf-red5", "workflow_dispatch")
	uiPush := run(44000000001, "2026-11-22T08:00:00Z", "ui", "push") // no jobs file: its tree is other's
	for _, c := range []struct {
		name     string
		dispatch []priorRun
		ui       []priorRun
		trees    map[int64]string
		noUI     bool // the ui query fails
		passed   bool
		want     string
	}{
		{"a proof run is refused, the older clean run counts", []priorRun{proof, clean}, []priorRun{uiPush},
			map[int64]string{proof.ID: tree, clean.ID: tree, uiPush.ID: other}, false, true, "in run 44979462996"},
		{"only a proof run: the suite runs", []priorRun{proof}, nil, map[int64]string{proof.ID: tree}, false, false, "is a proof run"},
		{"a clean run from before quiet-ci counts", []priorRun{pre}, nil, map[int64]string{pre.ID: tree}, false, true, "in run 44955419010"},
		{"another tree's runs don't", []priorRun{clean, pre}, nil, map[int64]string{clean.ID: other, pre.ID: other}, false, false, "no ci full-test passed on tree"},
		{"an API error runs the suite", []priorRun{clean}, nil, map[int64]string{clean.ID: tree}, true, false, "the runs list (branch=ui&event=push) failed"},
	} {
		t.Run(c.name, func(t *testing.T) {
			fix := t.TempDir()
			bin := t.TempDir()
			write := func(dir, name string, v any) {
				t.Helper()
				var b []byte
				if s, ok := v.(string); ok {
					b = []byte(s)
				} else {
					b, _ = json.Marshal(v)
				}
				if err := os.WriteFile(filepath.Join(dir, name), b, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			write(bin, "gh", fakeGH)
			// the API's lists are arrays, never null (a nil slice's JSON)
			list := func(rs []priorRun) map[string]any {
				return map[string]any{"workflow_runs": append([]priorRun{}, rs...)}
			}
			write(fix, "runs-dispatch.json", list(c.dispatch))
			if !c.noUI {
				write(fix, "runs-ui.json", list(c.ui))
			}
			write(fix, "runs-main.json", map[string]any{"workflow_runs": []priorRun{}})
			for _, r := range append(append([]priorRun{}, c.dispatch...), c.ui...) {
				write(fix, "commit-"+r.Sha+".json", map[string]any{"tree": map[string]string{"sha": c.trees[r.ID]}})
			}
			ents, err := os.ReadDir(recorded)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range ents {
				b, err := os.ReadFile(filepath.Join(recorded, e.Name()))
				if err != nil {
					t.Fatal(err)
				}
				write(fix, e.Name(), string(b))
			}
			out := filepath.Join(t.TempDir(), "out")
			sum := filepath.Join(t.TempDir(), "summary")
			cmd := exec.Command("bash", script)
			cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "FIX="+fix, "PRIOR_TREE="+tree,
				"GITHUB_REPOSITORY=some-du6e/clack", "GITHUB_OUTPUT="+out, "GITHUB_STEP_SUMMARY="+sum)
			log, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%v: %s", err, log)
			}
			o, _ := os.ReadFile(out)
			if got := strings.TrimSpace(string(o)) == "passed=true"; got != c.passed {
				t.Errorf("passed %v, want %v: %s", got, c.passed, log)
			}
			if !strings.Contains(string(log), c.want) {
				t.Errorf("want %q in %s", c.want, log)
			}
			if s, _ := os.ReadFile(sum); c.passed != strings.Contains(string(s), "full-test passed on this tree") {
				t.Errorf("summary %q", s)
			}
		})
	}
}
