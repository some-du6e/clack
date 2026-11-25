package pluginimport

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
)

// Before Taut 3.0 its plugin list was config.jsonc (commented JSON): read
// when there's no config.json.
func TestReadTautConfigJSONC(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "config.jsonc"), `{
  // Taut 2's list
  "plugins": {"Oneko": {"enabled": true, "speed": 12,},},
}`)
	in, ok := ReadTaut(dir)
	if !ok || in.Err != nil || len(in.Plugins) != 1 || in.Plugins[0].ID != "Oneko" || in.Plugins[0].Enabled == nil || !*in.Plugins[0].Enabled {
		t.Fatalf("ReadTaut(config.jsonc) = %+v, %v", in, ok)
	}
}

// The list names a user plugin by its file, which is read only when it's
// brought over (ReadCode); at most maxUserPlugins of them; a file that
// isn't a regular file isn't listed.
func TestUserPluginsReadOnImport(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < maxUserPlugins+5; i++ {
		write(t, filepath.Join(dir, "user-plugins", fmt.Sprintf("P%03d.js", i)), fmt.Sprintf("(() => class P%03d {})()", i))
	}
	write(t, filepath.Join(dir, "user-plugins", "Dir.js", "x"), "a folder named like a plugin")
	in, ok := ReadTaut(dir)
	if !ok {
		t.Fatal("no install")
	}
	if len(in.Plugins) != maxUserPlugins {
		t.Errorf("%d user plugins listed, want at most %d", len(in.Plugins), maxUserPlugins)
	}
	for _, f := range in.Plugins {
		if f.ID == "Dir" {
			t.Error("a folder was listed as a user plugin")
		}
		if f.Code != nil || f.CodePath == "" || !f.User() {
			t.Fatalf("%s: read while listing (code %d bytes, path %q)", f.ID, len(f.Code), f.CodePath)
		}
	}
	code, err := in.Plugins[0].ReadCode()
	if err != nil || string(code) != "(() => class P000 {})()" {
		t.Errorf("ReadCode: %q, %v", code, err)
	}
}

// With no home folder (and no APPDATA or XDG_CONFIG_HOME) there's no
// folder to look in, not one relative to wherever Clack runs.
func TestNoHomeNoFolder(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("macOS's home folder comes from the system, not $HOME")
	}
	for _, k := range []string{"HOME", "USERPROFILE", "APPDATA", "XDG_CONFIG_HOME", "SLICK_HANDOFF_PROFILE", "TAUT_CONFIG_DIR"} {
		t.Setenv(k, "")
	}
	if d := SlickDir(); d != "" {
		t.Errorf("SlickDir() = %q with no home", d)
	}
	if d := TautDir(); d != "" {
		t.Errorf("TautDir() = %q with no home", d)
	}
}
