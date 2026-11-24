package pluginimport

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func write(t *testing.T, path, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

// snapshot is every file under dir: its size, mode, modification time
// and contents' hash.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		st, err := d.Info()
		if err != nil {
			return err
		}
		key := st.Mode().String() + " " + st.ModTime().String()
		if !d.IsDir() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			key += fmt.Sprintf(" %d %x", len(b), sha256.Sum256(b))
		}
		out[p] = key
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func on(b bool) *bool { return &b }

// Find reads a Slick and a Taut install's plugins: each plugin's enabled
// state as its app has it (nil where that's left to the plugin's
// default), its settings, and the code of the plugins installed there
// from a file or a URL (Slick's storage/user-plugins, Taut's
// user-plugins). Taut's commented config.jsonc of before 3.0 reads too.
// Sign-in and session data (profile/, Taut's secrets, Slick's other
// storage) is never read, and nothing in either folder is written.
func TestFindReadsPluginsOnly(t *testing.T) {
	slick, taut := t.TempDir(), t.TempDir()
	t.Setenv("SLICK_HANDOFF_PROFILE", slick)
	t.Setenv("TAUT_CONFIG_DIR", taut)
	write(t, filepath.Join(slick, "slick", "settings.json"), `{
		"plugins": {
			"WhoReacted": {"enabled": true, "compact": true},
			"NoTrack": {"enabled": false},
			"HideResolved": {"channels": "help, *-help"},
			"MyPlugin": {"enabled": true, "greeting": "hi"}
		}
	}`)
	write(t, filepath.Join(slick, "slick", "storage", "user-plugins", "MyPlugin.json"), "// slick-plugin {\"id\":\"MyPlugin\"}\n(globalThis.__slickPlugins ??= []).push(() => null)")
	write(t, filepath.Join(slick, "slick", "storage", "message-logger", "archive.json"), `{"not": "a plugin"}`)
	write(t, filepath.Join(slick, "profile", "Cookies"), "session data")
	write(t, filepath.Join(taut, "config.json"), `{
		// Taut before 3.0 wrote comments, and trailing commas,
		"plugins": {
			"ClearURLs": {"enabled": true, "extraRules": ["utm_*",], /* a note */},
			"Oneko": {"speed": 12, "url": "https://example.invalid/a//b"},
		},
	}`)
	write(t, filepath.Join(taut, "user-plugins", "MyTaut.js"), "(() => { class MyTaut {}; return MyTaut })()")
	write(t, filepath.Join(taut, "secrets.dat"), "secret")
	write(t, filepath.Join(taut, "profile", "Local State"), "session data")
	beforeSlick, beforeTaut := snapshot(t, slick), snapshot(t, taut)

	got := Find()
	if len(got) != 2 || got[0].System != Slick || got[1].System != Taut {
		t.Fatalf("Find: %+v, want Slick's install, then Taut's", got)
	}
	for _, in := range got {
		if in.Err != nil {
			t.Errorf("%s: %v", in.System, in.Err)
		}
	}
	type row struct {
		ID       string
		Enabled  *bool
		Settings map[string]any
		User     bool
	}
	rows := func(in Install) []row {
		var out []row
		for _, f := range in.Plugins {
			if f.System != in.System {
				t.Errorf("%s's %s says it's %s's", in.System, f.ID, f.System)
			}
			out = append(out, row{f.ID, f.Enabled, f.Settings, f.User()})
		}
		return out
	}
	wantSlick := []row{
		{"HideResolved", nil, map[string]any{"channels": "help, *-help"}, false},
		{"MyPlugin", on(true), map[string]any{"greeting": "hi"}, true},
		{"NoTrack", on(false), map[string]any{}, false},
		{"WhoReacted", on(true), map[string]any{"compact": true}, false},
	}
	if g := rows(got[0]); !reflect.DeepEqual(g, wantSlick) {
		gj, _ := json.Marshal(g)
		t.Errorf("Slick's plugins: %s", gj)
	}
	wantTaut := []row{
		{"ClearURLs", on(true), map[string]any{"extraRules": []any{"utm_*"}}, false},
		{"MyTaut", nil, map[string]any{}, true},
		{"Oneko", nil, map[string]any{"speed": float64(12), "url": "https://example.invalid/a//b"}, false},
	}
	if g := rows(got[1]); !reflect.DeepEqual(g, wantTaut) {
		gj, _ := json.Marshal(g)
		t.Errorf("Taut's plugins: %s", gj)
	}
	if after := snapshot(t, slick); !reflect.DeepEqual(after, beforeSlick) {
		t.Error("Find changed something in Slick's folder")
	}
	if after := snapshot(t, taut); !reflect.DeepEqual(after, beforeTaut) {
		t.Error("Find changed something in Taut's folder")
	}
}

// An app never run here (no config folder, or one with no plugin list) has
// nothing to bring over; one whose list can't be read is listed with why,
// and what could be read of it.
func TestFindWithoutInstalls(t *testing.T) {
	t.Setenv("SLICK_HANDOFF_PROFILE", filepath.Join(t.TempDir(), "absent"))
	empty := t.TempDir()
	write(t, filepath.Join(empty, "profile", "Local State"), "session data")
	t.Setenv("TAUT_CONFIG_DIR", empty)
	if got := Find(); len(got) != 0 {
		t.Fatalf("Find with no plugin lists: %+v", got)
	}
	broken := t.TempDir()
	t.Setenv("TAUT_CONFIG_DIR", broken)
	write(t, filepath.Join(broken, "config.json"), `{"plugins": `)
	write(t, filepath.Join(broken, "user-plugins", "Mine.js"), "(() => null)()")
	got := Find()
	if len(got) != 1 || got[0].Err == nil || len(got[0].Plugins) != 1 || got[0].Plugins[0].ID != "Mine" {
		t.Fatalf("Taut with a broken config.json: %+v", got)
	}
}

func TestStripJSONC(t *testing.T) {
	for in, want := range map[string]string{
		`{"a": 1, // one` + "\n" + `}`:                 `{"a": 1 ` + "\n" + `}`,
		`{"a": "// not a comment", "b": "/* nor */",}`: `{"a": "// not a comment", "b": "/* nor */"}`,
		`[1, 2, /* two */ ]`:                           `[1, 2   ]`,
		`{"q": "a \"quoted, \" string,", }`:            `{"q": "a \"quoted, \" string," }`,
	} {
		if got := string(stripJSONC([]byte(in))); got != want {
			t.Errorf("stripJSONC(%q) = %q, want %q", in, got, want)
		}
		var v any
		if err := json.Unmarshal(stripJSONC([]byte(in)), &v); err != nil {
			t.Errorf("stripJSONC(%q) isn't JSON: %v", in, err)
		}
	}
}
