package core

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

func strp(s string) *string { return &s }

func waitExperimentsEvent(t *testing.T, evs <-chan Event) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-evs:
			if ev.Type == "experiments" {
				return
			}
		case <-deadline:
			t.Fatal("no experiments event")
		}
	}
}

func TestForcedExperimentGroupsOverServer(t *testing.T) {
	// "Active only" hides dormant channels under filter_dormant_channels
	// (TestActiveOnlyHidesDormantChannels); forcing the group changes the
	// sidebar at once, and clearing it restores the server's.
	a := rulesApp(t, store.Prefs{SidebarBehavior: "hide_inactive_channels"})
	evs, stop := a.Subscribe()
	defer stop()
	rec := json.RawMessage(`{"experiment_id":"1","type":"user","group":"on","trigger":"launch_user","exposure_id":"2","log_exposures":false}`)
	a.setExperimentAssignments(map[string]slack.ExperimentAssignment{
		"filter_dormant_channels": {Group: "on", Raw: rec},
		"improve_mute_pref":       {Group: "on"},
	})
	waitExperimentsEvent(t, evs)
	if got := sectionsByType(a)["channels"]; got != "C1,C3,C5" {
		t.Fatalf("server groups: channels %s", got)
	}

	a.SetExperimentGroups(map[string]*string{"filter_dormant_channels": strp("off")})
	waitExperimentsEvent(t, evs)
	if got := a.Experiment("filter_dormant_channels"); got != "off" {
		t.Errorf("forced group %q", got)
	}
	if got := sectionsByType(a)["channels"]; got != "C1,C3,C5,C2" {
		t.Errorf("forced off: channels %s (the dormant channel should be back)", got)
	}
	// Slack's experiments store keeps the server's record; forcing is a layer over it
	if got := string(a.ExperimentAssignments()["filter_dormant_channels"]); got != string(rec) {
		t.Errorf("server record changed: %s", got)
	}
	if f := a.ForcedExperiments(); len(f) != 1 || f["filter_dormant_channels"] != "off" {
		t.Errorf("forced %v", f)
	}

	// "" reads as not assigned (Slack's null)
	a.SetExperimentGroup("improve_mute_pref", "")
	if got := a.Experiment("improve_mute_pref"); got != "" {
		t.Errorf("unassigned reads %q", got)
	}

	a.ClearExperimentGroup("filter_dormant_channels")
	a.ClearExperimentGroup("improve_mute_pref")
	if got := sectionsByType(a)["channels"]; got != "C1,C3,C5" {
		t.Errorf("cleared: channels %s", got)
	}
	if f := a.ForcedExperiments(); len(f) != 0 {
		t.Errorf("forced after clearing %v", f)
	}

	// forced groups outlive experiments.getByUser answering again
	a.SetExperimentGroup("clack_test_experiment", "treatment")
	a.setExperiments(map[string]string{"filter_dormant_channels": "on"})
	if got := a.Experiment("clack_test_experiment"); got != "treatment" {
		t.Errorf("forced group lost on reload: %q", got)
	}
}

func TestForcedExperimentBeforeServerKeepsDefaults(t *testing.T) {
	a := rulesApp(t, store.Prefs{})
	a.mu.Lock()
	a.expServer, a.expGroups, a.exp = nil, nil, nil // before experiments.getByUser answers
	a.mu.Unlock()
	a.SetExperimentGroup("reskin_custom_responses", "off")
	if a.Experiment("reskin_custom_responses") != "off" || a.Experiment("filter_dormant_channels") != "on" {
		t.Errorf("forcing before the server answered lost the defaults: %q %q", a.Experiment("reskin_custom_responses"), a.Experiment("filter_dormant_channels"))
	}
	if a.ExperimentAssignments() != nil {
		t.Error("no server records before experiments.getByUser")
	}
}

// TestExperimentReadsListed keeps ExperimentReads (what Taut shows the
// Experiments plugins) in step with the experiment checks in Clack's code.
func TestExperimentReadsListed(t *testing.T) {
	read := regexp.MustCompile(`(?:\bExperiment|\bexperiment|\.exp|\bf\.on|\bf\.exp)\("([a-z0-9_]+)"\)`)
	constRe := regexp.MustCompile(`\bExp[A-Z]\w*\s*=\s*"([a-z0-9_]+)"`)
	found := map[string]string{}
	root := filepath.Join("..")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, re := range []*regexp.Regexp{read, constRe} {
			for _, m := range re.FindAllStringSubmatch(string(b), -1) {
				found[m[1]] = p
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, r := range ExperimentReads() {
		listed[r.Name] = true
		if len(r.Groups) == 0 {
			t.Errorf("%s lists no groups", r.Name)
		}
	}
	var missing, stale []string
	for name, p := range found {
		if !listed[name] {
			missing = append(missing, name+" ("+p+")")
		}
	}
	for name := range listed {
		if _, ok := found[name]; !ok {
			stale = append(stale, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	if len(missing) > 0 {
		t.Errorf("checked in code but not in experimentReads: %v", missing)
	}
	if len(stale) > 0 {
		t.Errorf("in experimentReads but no code reads them: %v", stale)
	}
}

func TestMpdmCustomNameFollowsExperiments(t *testing.T) {
	a := rulesApp(t, store.Prefs{})
	custom := json.RawMessage(`{"has_custom_mpdm_name":true}`)
	a.S.PutConv(slack.Conversation{ID: "G1", Name: "Design crew", IsMPIM: true, Properties: custom})
	a.S.PutConv(slack.Conversation{ID: "G2", Name: "Partners", IsMPIM: true, IsExtShared: true, Properties: custom})
	a.S.PutConv(slack.Conversation{ID: "G3", Name: "mpdm-ann--bob--cat-1", IsMPIM: true})
	a.setExperiments(map[string]string{"mpdm_naming": "on"})
	if n, ok := a.MpdmCustomName("G1"); !ok || n != "Design crew" {
		t.Errorf("renamed group DM: %q %v", n, ok)
	}
	if _, ok := a.MpdmCustomName("G2"); ok {
		t.Error("a Slack Connect group DM needs sc_mpdm_naming too")
	}
	if _, ok := a.MpdmCustomName("G3"); ok {
		t.Error("a group DM nobody renamed keeps its member list")
	}
	a.SetExperimentGroup("sc_mpdm_naming", "on")
	if n, ok := a.MpdmCustomName("G2"); !ok || n != "Partners" {
		t.Errorf("Slack Connect with sc_mpdm_naming: %q %v", n, ok)
	}
	a.SetExperimentGroup("mpdm_naming", "off") // e.g. an Experiments plugin override
	if _, ok := a.MpdmCustomName("G1"); ok {
		t.Error("mpdm_naming off still names the group DM")
	}
}
