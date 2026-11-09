package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDemoArgs(t *testing.T) {
	for _, c := range []struct {
		args []string
		want bool
		rest []string
	}{
		{[]string{"demo"}, true, nil},
		{[]string{"demo", "--speed", "2"}, true, []string{"--speed", "2"}},
		{[]string{"--demo", "--reset"}, true, []string{"--reset"}},
		{[]string{"-dark", "-demo"}, true, []string{"-dark"}},
		{[]string{"-dark"}, false, nil},
		{[]string{"login", "demo"}, false, nil}, // "demo" as a workspace name
		{nil, false, nil},
	} {
		if got := wantsDemo(c.args); got != c.want {
			t.Errorf("wantsDemo(%q) = %v", c.args, got)
		}
		if c.want {
			if got := demoArgs(c.args); !reflect.DeepEqual(got, c.rest) {
				t.Errorf("demoArgs(%q) = %q, want %q", c.args, got, c.rest)
			}
		}
	}
}

// The demo's folder is never the real config, and --reset deletes only a
// folder the demo made.
func TestDemoDirSafety(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	t.Setenv("CLACK_HOME", real)
	t.Setenv("CLACK_DEMO_HOME", filepath.Join(root, "demo"))
	dir, err := demoDir()
	if err != nil || dir != filepath.Join(root, "demo") {
		t.Fatalf("demoDir %q %v", dir, err)
	}
	for _, bad := range []string{real, root, filepath.Dir(root)} {
		if err := checkDemoDir(bad); err == nil {
			t.Errorf("checkDemoDir(%s) allowed the real config (or a parent of it)", bad)
		}
	}
	// a folder holding credentials is refused, even with the marker
	creds := filepath.Join(root, "withcreds")
	os.MkdirAll(creds, 0o700)
	os.WriteFile(filepath.Join(creds, "creds.json"), []byte("{}"), 0o600)
	os.WriteFile(filepath.Join(creds, demoMarker), nil, 0o600)
	if err := resetDemoDir(creds); err == nil || !strings.Contains(err.Error(), "creds.json") {
		t.Errorf("reset of a folder with creds.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(creds, "creds.json")); err != nil {
		t.Fatal("reset deleted creds.json")
	}
	// no marker: not reset
	other := filepath.Join(root, "other")
	os.MkdirAll(other, 0o700)
	os.WriteFile(filepath.Join(other, "keep.txt"), []byte("x"), 0o600)
	if err := resetDemoDir(other); err == nil {
		t.Error("reset a folder without the demo's marker")
	}
	if _, err := os.Stat(filepath.Join(other, "keep.txt")); err != nil {
		t.Fatal("reset deleted an unmarked folder's files")
	}
	// the demo's own folder: made with a marker and a first ui.json, then reset
	if err := prepareDemoDir(dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "ui.json"))
	if err != nil || !strings.Contains(string(b), `"realtime":true`) {
		t.Fatalf("ui.json %q %v", b, err)
	}
	os.MkdirAll(filepath.Join(dir, "cache", "img"), 0o700)
	if err := prepareDemoDir(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cache")); err == nil {
		t.Error("a start kept the last run's cache")
	}
	if err := resetDemoDir(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("reset left %s: %v", dir, err)
	}
	if err := resetDemoDir(dir); err != nil { // nothing to reset
		t.Errorf("reset of a missing folder: %v", err)
	}
}
