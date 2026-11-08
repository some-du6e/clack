package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `clack --update` runs without Clack's config: up to date against a fake
// releases API, it exits 0 and never migrates (reads) the old config,
// creds included.
func TestUpdateCLIUpToDateTouchesNoConfig(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.1.0-alpha.10"})
	}))
	defer api.Close()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Setenv("CLACK_HOME", "")
	t.Setenv("YOLOSLACK_HOME", "")
	t.Setenv("CLACK_UPDATE_URL", api.URL)
	t.Setenv("CLACK_UPDATE_AS", "v0.1.0-alpha.10")
	cfg, err := os.UserConfigDir()
	if err != nil {
		t.Skip(err)
	}
	old := filepath.Join(cfg, "yoloslack")
	os.MkdirAll(old, 0o700)
	os.WriteFile(filepath.Join(old, "creds.json"), []byte(`{"token":"xoxc-synthetic"}`), 0o600)
	os.WriteFile(filepath.Join(old, "ui.json"), []byte(`{}`), 0o600)
	var out, errb bytes.Buffer
	if code := updateCLI([]string{"--update"}, strings.NewReader(""), &out, &errb); code != 0 {
		t.Fatalf("exit %d\n%s%s", code, out.String(), errb.String())
	}
	if !strings.Contains(out.String(), "Clack is up to date") {
		t.Errorf("output:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(cfg, "clack", "creds.json")); !os.IsNotExist(err) {
		t.Error("--update migrated the config (read creds.json)")
	}
	// a failure exits nonzero with a message
	t.Setenv("CLACK_UPDATE_URL", api.URL+"/missing")
	out.Reset()
	errb.Reset()
	if code := updateCLI([]string{"--update", "--check"}, strings.NewReader(""), &out, &errb); code == 0 || !strings.Contains(errb.String(), "clack: update failed: ") {
		t.Errorf("failure: exit %d, %q", code, errb.String())
	}
	if !wantsUpdateCLI([]string{"--update", "--yes"}) || wantsUpdateCLI([]string{"slack://open"}) {
		t.Error("wantsUpdateCLI")
	}
}
