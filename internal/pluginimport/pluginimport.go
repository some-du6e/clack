// Package pluginimport finds the plugins a Slick or Taut install on this
// machine keeps, and their settings, for Clack to bring over (the
// introduction's "Bring over your plugins", Preferences > Plugins). It
// reads each app's plugin list, its plugins' settings and the files of
// plugins installed there from a file or a URL, nothing else: never a
// sign-in, a session or account data (each app's profile/, its secrets,
// its plugins' storage). It never writes to the other app.
package pluginimport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// The systems, as pluginlib names them.
const (
	Taut  = "taut"
	Slick = "slick"
)

// Found is a plugin another app has.
type Found struct {
	System string // Taut or Slick
	ID     string
	// Enabled is whether it's turned on there; nil when its list leaves
	// that to the plugin's default (Slick's and Taut's "only what's set")
	Enabled *bool
	// Settings are its options as that app keeps them, "enabled" aside
	Settings map[string]any
	// Code is a user plugin's file (one installed there from a file or a
	// URL, not one of the app's own); nil for the app's own plugins
	Code []byte
}

// Install is an app's plugins on this machine.
type Install struct {
	System  string // Taut or Slick
	Dir     string // its config folder
	Plugins []Found
	// Off: plugins are turned off there as a whole (Slick's switch)
	Off bool
	// Err: its plugin list couldn't be read; the plugins found are those
	// that could be
	Err error
}

// maxFile bounds what is read of one file: a plugin list, or a user
// plugin (Slick's storage takes values up to 8 MiB).
const maxFile = 16 << 20

// SlickDir is Slick's config folder, as Slick's own configDir has it
// (src/desktop/paths.ts): SLICK_HANDOFF_PROFILE, else the OS's.
func SlickDir() string {
	if d := os.Getenv("SLICK_HANDOFF_PROFILE"); d != "" {
		if a, err := filepath.Abs(d); err == nil {
			return a
		}
		return d
	}
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Slick")
	case "windows":
		return filepath.Join(appData(home), "Slick")
	}
	return filepath.Join(xdgConfig(home), "slick")
}

// TautDir is Taut's config folder, as Taut's desktop app has it
// (desktop/src/paths.ts): TAUT_CONFIG_DIR, else Electron's appData's Taut.
func TautDir() string {
	if d := os.Getenv("TAUT_CONFIG_DIR"); d != "" {
		if a, err := filepath.Abs(d); err == nil {
			return a
		}
		return d
	}
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Taut")
	case "windows":
		return filepath.Join(appData(home), "Taut")
	}
	return filepath.Join(xdgConfig(home), "Taut")
}

func appData(home string) string {
	if d := os.Getenv("APPDATA"); d != "" {
		return d
	}
	return filepath.Join(home, "AppData", "Roaming")
}

func xdgConfig(home string) string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return d
	}
	return filepath.Join(home, ".config")
}

// Find is the Slick and Taut installs on this machine with a plugin list
// to bring over (Slick's settings.json, Taut's config.json, or user
// plugins), Slick's first. An app never run here has none.
func Find() []Install {
	var out []Install
	if in, ok := ReadSlick(SlickDir()); ok {
		out = append(out, in)
	}
	if in, ok := ReadTaut(TautDir()); ok {
		out = append(out, in)
	}
	return out
}

// readFile is a file of at most maxFile bytes; os.ErrNotExist for none.
func readFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s isn't a file", filepath.Base(path))
	}
	b, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxFile {
		return nil, fmt.Errorf("%s is larger than %d MiB", filepath.Base(path), maxFile>>20)
	}
	return b, nil
}

// pluginBlocks is a plugin list's {"plugins": {id: {...}}}: each plugin's
// enabled state and its other options.
func pluginBlocks(m map[string]any) map[string]Found {
	out := map[string]Found{}
	ps, _ := m["plugins"].(map[string]any)
	for id, v := range ps {
		block, ok := v.(map[string]any)
		if !ok || id == "" {
			continue
		}
		f := Found{ID: id, Settings: map[string]any{}}
		for k, val := range block {
			if k == "enabled" {
				if b, ok := val.(bool); ok {
					f.Enabled = &b
				}
				continue
			}
			f.Settings[k] = val
		}
		out[id] = f
	}
	return out
}

// ReadSlick is Slick's plugins in its config folder dir: settings.json's
// (slick/settings.json: {enabled, plugins: {id: {enabled, ...settings}}})
// and the user plugins Slick keeps in its storage
// (slick/storage/user-plugins/<id>.json, each the plugin's code). ok is
// false when it has neither.
func ReadSlick(dir string) (Install, bool) {
	in := Install{System: Slick, Dir: dir}
	settings := filepath.Join(dir, "slick")
	blocks := map[string]Found{}
	b, err := readFile(filepath.Join(settings, "settings.json"))
	found := err == nil
	switch {
	case err == nil:
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			in.Err = fmt.Errorf("Slick's settings.json isn't JSON: %w", err)
		} else {
			blocks = pluginBlocks(m)
			in.Off = m["enabled"] == false
		}
	case !errors.Is(err, os.ErrNotExist):
		in.Err = fmt.Errorf("couldn't read Slick's settings.json: %w", err)
		found = true
	}
	// user plugins: storage/user-plugins/<id>.json (":" is %3A on Windows,
	// as Slick's blob store writes it)
	users := filepath.Join(settings, "storage", "user-plugins")
	if ents, err := os.ReadDir(users); err == nil {
		for _, e := range ents {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".tmp") {
				continue
			}
			id := strings.ReplaceAll(strings.TrimSuffix(name, ".json"), "%3A", ":")
			code, err := readFile(filepath.Join(users, name))
			if err != nil || len(bytes.TrimSpace(code)) == 0 {
				continue
			}
			f := blocks[id]
			f.ID, f.Code = id, code
			blocks[id] = f
			found = true
		}
	}
	if len(blocks) > 0 {
		found = true
	}
	in.Plugins = sorted(Slick, blocks)
	return in, found
}

// ReadTaut is Taut's plugins in its config folder dir: config.json's
// ({plugins: {id: {enabled, ...options}}}; before Taut 3.0 commented JSON)
// and its user plugins (user-plugins/<id>.js). ok is false when it has
// neither.
func ReadTaut(dir string) (Install, bool) {
	in := Install{System: Taut, Dir: dir}
	blocks := map[string]Found{}
	b, err := readFile(filepath.Join(dir, "config.json"))
	found := err == nil
	switch {
	case err == nil:
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			m = nil
			if err2 := json.Unmarshal(stripJSONC(b), &m); err2 != nil {
				in.Err = fmt.Errorf("Taut's config.json isn't JSON: %w", err)
			}
		}
		if in.Err == nil {
			blocks = pluginBlocks(m)
		}
	case !errors.Is(err, os.ErrNotExist):
		in.Err = fmt.Errorf("couldn't read Taut's config.json: %w", err)
		found = true
	}
	users := filepath.Join(dir, "user-plugins")
	if ents, err := os.ReadDir(users); err == nil {
		for _, e := range ents {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".js") {
				continue
			}
			id := strings.TrimSuffix(name, ".js")
			code, err := readFile(filepath.Join(users, name))
			if err != nil || len(bytes.TrimSpace(code)) == 0 {
				continue
			}
			f := blocks[id]
			f.ID, f.Code = id, code
			blocks[id] = f
			found = true
		}
	}
	if len(blocks) > 0 {
		found = true
	}
	in.Plugins = sorted(Taut, blocks)
	return in, found
}

func sorted(system string, blocks map[string]Found) []Found {
	out := make([]Found, 0, len(blocks))
	for _, f := range blocks {
		f.System = system
		if f.Settings == nil {
			f.Settings = map[string]any{}
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// stripJSONC is commented JSON (Taut's config.jsonc before 3.0) as JSON:
// its // and /* */ comments left out, then its trailing commas, strings
// as they are.
func stripJSONC(b []byte) []byte {
	return dropTrailingCommas(dropComments(b))
}

// dropComments is b without its // and /* */ comments (outside strings).
func dropComments(b []byte) []byte {
	var out bytes.Buffer
	inStr, esc := false, false
	for i := 0; i < len(b); i++ {
		c := b[i]
		if inStr {
			out.WriteByte(c)
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch {
		case c == '"':
			inStr = true
			out.WriteByte(c)
		case c == '/' && i+1 < len(b) && b[i+1] == '/':
			for i < len(b) && b[i] != '\n' {
				i++
			}
			if i < len(b) {
				out.WriteByte('\n')
			}
		case c == '/' && i+1 < len(b) && b[i+1] == '*':
			i += 2
			for i+1 < len(b) && !(b[i] == '*' && b[i+1] == '/') {
				i++
			}
			i++ // past the closing '/'
			out.WriteByte(' ')
		default:
			out.WriteByte(c)
		}
	}
	return out.Bytes()
}

// dropTrailingCommas is b without the commas (outside strings) that only
// space parts from a closing } or ].
func dropTrailingCommas(b []byte) []byte {
	var out bytes.Buffer
	inStr, esc := false, false
	for i := 0; i < len(b); i++ {
		c := b[i]
		if inStr {
			out.WriteByte(c)
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
		}
		if c == ',' {
			j := i + 1
			for j < len(b) && (b[j] == ' ' || b[j] == '\t' || b[j] == '\n' || b[j] == '\r') {
				j++
			}
			if j < len(b) && (b[j] == '}' || b[j] == ']') {
				continue
			}
		}
		out.WriteByte(c)
	}
	return out.Bytes()
}
