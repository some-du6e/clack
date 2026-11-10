package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/pluginlib"
)

// TestExperimentsPluginsDriveClack installs the Library's two Experiments
// plugins (the published Taut and Slick bundles, same sha256 users get)
// into the whole client over internal/fakeslack, the way the plugin bench
// does, with an override saved the way each plugin's tab saves it, and
// checks a real Clack behaviour follows: sidebar_section_search gates the
// section menu's "Search in this section" (fakeslack, like the account,
// has it on). Turning the plugin off gives the server's group back.
// The experiments workflow downloads the Library and runs this.
func TestExperimentsPluginsDriveClack(t *testing.T) {
	dir := os.Getenv("PLUGIN_BENCH_LIBRARY")
	if dir == "" {
		t.Skip("PLUGIN_BENCH_LIBRARY not set: .github/workflows/experiments.yml downloads the Library and runs this")
	}
	srv := httptest.NewServer(http.StripPrefix("/rel/", http.FileServer(http.Dir(dir))))
	t.Cleanup(srv.Close)
	t.Setenv("CLACK_PLUGIN_LIBRARY", srv.URL+"/rel/index.json")
	b, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ix pluginlib.Index
	if err := json.Unmarshal(b, &ix); err != nil {
		t.Fatal(err)
	}
	for _, sys := range []string{pluginlib.Taut, pluginlib.Slick} {
		var entry *pluginlib.Entry
		for i := range ix.Plugins {
			if ix.Plugins[i].System == sys && ix.Plugins[i].ID == "Experiments" {
				entry = &ix.Plugins[i]
			}
		}
		if entry == nil {
			t.Fatalf("the Library has no %s Experiments", sys)
		}
		t.Run(sys, func(t *testing.T) { experimentsPluginDrives(t, *entry) })
	}
}

// the namespace each system keeps a plugin's storage in (Taut 07_manager
// storageNs; Slick's storage API over Taut's data store)
func pluginStorageNS(system, id string) string {
	if system == pluginlib.Slick {
		return "slick:plugin:" + id
	}
	return "plugin:" + id + ":storage"
}

func experimentsPluginDrives(t *testing.T, entry pluginlib.Entry) {
	fs, err := fakeslack.Start(benchSize)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	e := benchStart(t, fs)
	e.waitReady()
	e.until("Taut and Slick", 60*time.Second, func() bool { return e.a.Taut() != nil && e.a.Slick() != nil })
	tt, sl := e.a.Taut(), e.a.Slick()
	c := e.a.Core()
	searchItem := func() bool { return e.a.searchInSectionItem(&sbSection{Type: "stars"}) != nil }
	e.until("experiments.getByUser", 30*time.Second, func() bool { return c.ExperimentAssignments() != nil })
	if g := c.Experiment("sidebar_section_search"); g != "on" || !searchItem() {
		t.Fatalf("before the plugin: group %q, menu item %v", g, searchItem())
	}

	// the override, saved where the plugin's tab saves it (its storage key "overrides")
	tt.DataWrite(pluginStorageNS(entry.System, entry.ID), "overrides", `{"sidebar_section_search":"off"}`)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	lib := e.a.pluginLib()
	if lib == nil {
		t.Fatal("no plugin library")
	}
	if _, err := lib.Index(ctx, true); err != nil {
		t.Fatal(err)
	}
	code, main, err := lib.Download(ctx, entry)
	if err != nil {
		t.Fatalf("download %s: %v", entry.File.File, err)
	}
	h, ok := newLibraryPage(e.a, nil).host(entry.System)
	if !ok {
		t.Fatalf("no %s host", entry.System)
	}
	if err := h.install(ctx, entry, code, main, false); err != nil {
		t.Fatalf("install: %v", err)
	}
	enable := func(on bool) {
		t.Helper()
		var err error
		if entry.System == pluginlib.Taut {
			err = tt.SetEnabled(ctx, entry.ID, on)
		} else {
			err = sl.SetEnabled(ctx, entry.ID, on)
		}
		if err != nil {
			t.Fatalf("enable %v: %v", on, err)
		}
	}
	enable(true)
	e.until("the plugin's override in core", 30*time.Second, func() bool { return c.Experiment("sidebar_section_search") == "off" })
	if searchItem() {
		t.Error(`sidebar_section_search forced off, but the section menu still has "Search in this section"`)
	}
	// Slack's store under the patch still holds the server's record
	if raw := c.ExperimentAssignments()["sidebar_section_search"]; string(raw) != `{"group":"on"}` {
		t.Errorf("the server's record changed: %s", raw)
	}

	enable(false)
	e.until("the server's group back", 30*time.Second, func() bool { return c.Experiment("sidebar_section_search") == "on" })
	if !searchItem() {
		t.Error(`the plugin is off, but "Search in this section" didn't come back`)
	}
}
