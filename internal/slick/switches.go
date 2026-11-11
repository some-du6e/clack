package slick

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/some-du6e/clack/internal/jsrt"
)

// Chromium switches from main halves (MainCtx.switches.append, boot only).
// Slick appends them to Electron's command line at process start, from the
// settings stored then (src/desktop/pluginHost.ts). Clack has no Chromium:
// the switches with a Clack counterpart (Switch*) work Clack's own
// process-wide knobs at the next start (cmd/clack reads StartupSwitches),
// the rest are kept and change nothing.
//
// The next start gets what each enabled main half's boot appends with the
// settings it has then, as in Slick. Clack's main halves boot once per JS
// runtime, after the window is up, so whenever such a plugin (one that
// declares the switches capability) starts or its settings change, Clack
// runs its boot again against a context where only settings, log and
// switches do anything (S.previewSwitches) and keeps the result in
// <Dir>/switches.json. Turning the plugin or Slick off clears its entry.

// The Chromium switches Clack has a counterpart for.
const (
	// SwitchDisableCrashpad and SwitchDisableCrashReporter (Snappy's
	// "Disable crash reporter"): no crash capture (applog.CaptureCrashes:
	// crash.log, copied into clack.log at the next start).
	SwitchDisableCrashpad      = "disable-crashpad"
	SwitchDisableCrashReporter = "disable-crash-reporter"
	// SwitchIgnoreGPUBlocklist (Snappy's "Ignore GPU blocklist"): Gio's
	// only GPU blocklist is X11's Vulkan (vulkanBuggy, NVIDIA drivers,
	// gioui.org/issue/347); with it an X11 window tries Vulkan before
	// OpenGL ES (app.IgnoreGPUBlocklist).
	SwitchIgnoreGPUBlocklist = "ignore-gpu-blocklist"
)

const switchesFile = "switches.json"

// hasSwitches reports a main half that may append switches.
func hasSwitches(p *plugin) bool {
	return p.meta.Main != nil && slices.Contains(p.meta.Main.Capabilities, "switches")
}

// refreshSwitches records what p's boot appends with settings.
func (s *Slick) refreshSwitches(ctx context.Context, rt *jsrt.Runtime, p *plugin, settings map[string]any) {
	s.mu.Lock()
	ok := hasSwitches(p)
	s.mu.Unlock()
	if !ok {
		return
	}
	var sw []string
	if err := s.call(ctx, rt, "previewSwitches", &sw, p.id, settings); err != nil {
		log.Printf("slick: %s: switches: %v", p.id, err)
		return
	}
	s.setSwitches(p.id, sw)
}

// dropSwitches forgets a plugin's switches (it's off, or removed).
func (s *Slick) dropSwitches(id string) { s.setSwitches(id, nil) }

func (s *Slick) setSwitches(id string, sw []string) {
	s.switchMu.Lock()
	defer s.switchMu.Unlock()
	path := s.path(switchesFile)
	all := map[string][]string{}
	readJSONFile(path, &all)
	sw = slices.Compact(slices.Sorted(slices.Values(sw)))
	old, had := all[id]
	if len(sw) == 0 {
		if !had {
			return
		}
		delete(all, id)
	} else {
		if had && slices.Equal(old, sw) {
			return
		}
		all[id] = sw
	}
	if len(all) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Printf("slick: %s: %v", switchesFile, err)
		}
		return
	}
	b, _ := json.MarshalIndent(all, "", "  ")
	if err := writeFileAtomic(path, append(b, '\n'), 0o600); err != nil {
		log.Printf("slick: %s: %v", switchesFile, err)
		return
	}
	log.Printf("slick: %s's switches for the next start: %v", id, sw)
}

// StartupSwitches are the switches enabled main halves appended for this
// start (see above), for cmd/clack before it opens the window. dir is
// Options.Dir ("" = the default). Nothing when Slick is off in
// settings.json, or a plugin with an entry is off there.
func StartupSwitches(dir string) []string {
	if dir == "" {
		dir = filepath.Join(clackHome(), "slick")
	}
	all := map[string][]string{}
	readJSONFile(filepath.Join(dir, switchesFile), &all)
	if len(all) == 0 {
		return nil
	}
	cfg, err := loadConfig(filepath.Join(dir, "settings.json"))
	if err != nil || !cfg.globallyEnabled() {
		return nil
	}
	var out []string
	for id, sw := range all {
		// an entry exists only while the plugin was on; settings.json
		// edited by hand since can still turn it off
		if on, ok := cfg.Plugins[id]["enabled"].(bool); ok && !on {
			continue
		}
		out = append(out, sw...)
	}
	sort.Strings(out)
	return slices.Compact(out)
}
