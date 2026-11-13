package core

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// accountExperiments reads testdata/account_experiments.json: the test
// account's experiments.getByUser groups (names and groups only), as
// recorded on the date in "captured". Refresh it with one read-only call
// (docs/EXPERIMENTS.md, "Method") and fix what the tests below print.
func accountExperiments(t *testing.T) (captured string, groups map[string]string) {
	t.Helper()
	b, err := os.ReadFile("testdata/account_experiments.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Captured    string            `json:"captured"`
		Assignments map[string]string `json:"assignments"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Assignments) == 0 {
		t.Fatal("testdata/account_experiments.json lists no assignments")
	}
	return f.Captured, f.Assignments
}

// driftTable formats rows of (experiment, Clack, account) with "-" for
// not assigned.
func driftTable(head string, rows [][3]string) string {
	sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	var b strings.Builder
	fmt.Fprintf(&b, "%-60s %-14s %s\n", "experiment", head, "account")
	for _, r := range rows {
		c, a := r[1], r[2]
		if c == "" {
			c = "-"
		}
		if a == "" {
			a = "-"
		}
		fmt.Fprintf(&b, "%-60s %-14s %s\n", r[0], c, a)
	}
	return b.String()
}

// TestDefaultExperimentsMatchAccount: the groups every check reads before
// experiments.getByUser answers (with no disk cache) are the account's,
// for every experiment Clack reads and every default it keeps. An
// experiment the account isn't in has no default (Slack's null).
func TestDefaultExperimentsMatchAccount(t *testing.T) {
	captured, acct := accountExperiments(t)
	names := map[string]bool{}
	for _, r := range ExperimentReads() {
		names[r.Name] = true
	}
	for n := range defaultExperimentGroups {
		names[n] = true
	}
	var drift [][3]string
	for n := range names {
		if d, a := defaultExperimentGroups[n], acct[n]; d != a {
			drift = append(drift, [3]string{n, d, a})
		}
	}
	if len(drift) > 0 {
		t.Errorf("core's defaultExperimentGroups differ from the account's assignments of %s:\n%s", captured, driftTable("default", drift))
	}
	want := sidebarExp{
		filterDormant: acct["filter_dormant_channels"] == "on",
		improveMute:   acct["improve_mute_pref"] == "on",
		dmRecency:     acct["dm_recency_sort_rollout"] == "without_edu" || acct["dm_recency_sort_rollout"] == "with_edu",
		dmHistory:     acct["programmable_sidebar_dm_history"] == "on",
	}
	if defaultSidebarExp != want {
		t.Errorf("defaultSidebarExp %+v, the account's groups give %+v", defaultSidebarExp, want)
	}
}

// TestFakeslackExperimentsMatchAccount: fakeslack's experiments.getByUser
// gives no experiment a group the account doesn't have. It may leave
// some out (tests force those), and it prints them.
func TestFakeslackExperimentsMatchAccount(t *testing.T) {
	captured, acct := accountExperiments(t)
	fake := fakeslack.AccountExperimentGroups()
	var drift, missing [][3]string
	for n, g := range fake {
		if acct[n] != g {
			drift = append(drift, [3]string{n, g, acct[n]})
		}
	}
	for _, r := range ExperimentReads() {
		if _, ok := fake[r.Name]; !ok && acct[r.Name] != "" {
			missing = append(missing, [3]string{r.Name, "", acct[r.Name]})
		}
	}
	if len(drift) > 0 {
		t.Errorf("fakeslack's experiments.getByUser differs from the account's assignments of %s:\n%s", captured, driftTable("fakeslack", drift))
	}
	if len(missing) > 0 {
		t.Logf("experiments Clack reads that fakeslack leaves unassigned (the account's of %s):\n%s", captured, driftTable("fakeslack", missing))
	}
}

// TestCachedExperimentsBeforeServer: the disk cache's groups (Slack's
// persisted experiments store) replace the defaults whole until
// experiments.getByUser answers; the answer replaces them and is kept for
// the next start, and a cache read that lands after it changes nothing.
func TestCachedExperimentsBeforeServer(t *testing.T) {
	a, f := testApp(t)
	a.S.SetExperimentGroups(map[string]string{"channel_header_with_agents": "off"})
	a.installCachedExperiments()
	if g := a.Experiment("channel_header_with_agents"); g != "off" {
		t.Errorf("cached group %q, want off", g)
	}
	if g := a.Experiment("filter_dormant_channels"); g != "" {
		t.Errorf("a default (%q) read through the cached assignments", g)
	}
	if a.sidebarExperiments().filterDormant {
		t.Error("the sidebar rules kept a default the cache doesn't have")
	}

	f.reply["experiments.getByUser"] = `{"ok":true,"assignments":{"filter_dormant_channels":{"group":"on"}}}`
	as, err := a.C.ExperimentAssignments(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if p := f.last("experiments.getByUser"); p.Get("ssb_platform") != slack.SSBPlatform(runtime.GOOS) || p.Get("ssb_release_channel") != "prod" {
		t.Errorf("experiments.getByUser asked with %v, want Slack desktop's ssb_platform and ssb_release_channel", p)
	}
	a.setExperimentAssignments(as)
	if g := a.Experiment("channel_header_with_agents"); g != "" {
		t.Errorf("the cached group outlived the server's answer: %q", g)
	}
	if got := a.S.CachedExperimentGroups(); len(got) != 1 || got["filter_dormant_channels"] != "on" {
		t.Errorf("groups kept for the next start: %v", got)
	}
	a.S.SetExperimentGroups(map[string]string{"channel_header_with_agents": "on"})
	a.installCachedExperiments()
	if g := a.Experiment("channel_header_with_agents"); g != "" {
		t.Errorf("a cache read after the answer overrode it: %q", g)
	}
}

func TestSSBPlatform(t *testing.T) {
	for goos, want := range map[string]string{"windows": "win32", "darwin": "darwin", "linux": "linux"} {
		if got := slack.SSBPlatform(goos); got != want {
			t.Errorf("SSBPlatform(%q) = %q, want %q", goos, got, want)
		}
	}
}
