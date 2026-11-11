package slick

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/ui/slackc"
)

// Slick's Snappy (src/plugins/Snappy), unmodified, does in Clack what it
// does in Slack:
//
//   - its transitions scan finds Slack's stylesheet in the page (the rules
//     Clack's natives animate by, slackc.NativeTransitionsCSS) and
//     collapses every transition in it; in Clack's CSS engine that makes
//     the natives' own transitions instant;
//   - its main half's spellcheck setting turns Clack's composer
//     spellcheck off, live, and back on when it's off or Snappy stops;
//   - its boot's Chromium switches are kept for the next start, follow its
//     settings, and go when it's turned off;
//   - Preferences says what each setting does in Clack.
func TestSnappyInClack(t *testing.T) {
	dir := pluginsDir(t)
	h := newHarness(t)
	h.importFile(filepath.Join(dir, "Snappy.js"))
	h.importFile(filepath.Join(dir, "Snappy.main.js"))
	if err := h.s.SetOption(h.ctx(), "Snappy", "disableSpellcheck", true); err != nil {
		t.Fatal(err)
	}
	if err := h.s.SetOption(h.ctx(), "Snappy", "ignoreGpuBlocklist", true); err != nil {
		t.Fatal(err)
	}
	if err := h.s.SetEnabled(h.ctx(), "Snappy", true); err != nil {
		t.Fatal(err)
	}
	if info := h.plugin("Snappy"); !info.Running || info.StartError != "" {
		t.Fatalf("didn't start: %+v", info)
	}

	// transitions: Snappy waits for .p-client_workspace, then reads
	// document.styleSheets in idle time
	var css string
	eventually(t, 15*time.Second, "Snappy's transition overrides", func() bool {
		css = h.ui.style("plugin:Snappy:transitions:")
		return css != ""
	})
	for _, sel := range []string{
		".p-thread_separator_row_generic.p-thread_separator_row_generic--ghost .p-threads_flexpane__separator_line__read",
		".c-message_list__unread_divider--ghost .c-message_list__unread_divider__separator",
		".channelJoinBannerOuterWrapper__TpBQA",
		".block__G68co",
	} {
		if !strings.Contains(css, sel) {
			t.Errorf("Snappy didn't collapse %s's transition:\n%s", sel, css)
		}
	}
	if !strings.Contains(css, "transition-duration: .01ms !important") || !strings.Contains(css, "transition-delay: 0s !important") {
		t.Errorf("not Snappy's override:\n%s", css)
	}
	// the natives' timing through Clack's CSS engine, as the UI installs
	// the sheet (UIHooks.SetStyle → slackc.SetStyle)
	block := slackc.Class("block__G68co")
	before, _ := slackc.CurrentScope().Child(block, 0).Transition("height", time.Second, 0)
	slackc.SetStyle("plugin:Snappy:transitions:0", css)
	after, delay := slackc.CurrentScope().Child(block, 0).Transition("height", time.Second, 0)
	slackc.RemoveStyle("plugin:Snappy:transitions:0")
	if before != time.Second || after > time.Millisecond || delay != 0 {
		t.Errorf("agent block height: %v without Snappy (want the native's own 1s with no CSS), %v %v with it (want .01ms, no delay)", before, after, delay)
	}

	// spellcheck: Electron's session spellchecker is Clack's composer spellcheck
	eventually(t, 5*time.Second, "the composer spellcheck off", func() bool { return !h.spellOn() })
	if err := h.s.SetOption(h.ctx(), "Snappy", "disableSpellcheck", false); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "the composer spellcheck back on (live setting)", h.spellOn)
	if err := h.s.SetOption(h.ctx(), "Snappy", "disableSpellcheck", true); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "the composer spellcheck off again", func() bool { return !h.spellOn() })

	// switches for the next start
	slickDir := filepath.Join(h.dir, "slick")
	want := []string{SwitchDisableCrashReporter, SwitchDisableCrashpad, SwitchIgnoreGPUBlocklist} // sorted
	if got := StartupSwitches(slickDir); !slices.Equal(got, want) {
		t.Errorf("startup switches %v, want %v", got, want)
	}
	if err := h.s.SetOption(h.ctx(), "Snappy", "disableCrashReporter", false); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "the crash reporter switches gone", func() bool {
		return slices.Equal(StartupSwitches(slickDir), []string{SwitchIgnoreGPUBlocklist})
	})

	// Preferences: Slick's own descriptions, with what each does in Clack
	info := h.plugin("Snappy")
	if !strings.Contains(info.Description, "In Clack:") {
		t.Errorf("plugin description %q", info.Description)
	}
	for _, o := range info.Options {
		if !strings.Contains(o.Description, "In Clack") {
			t.Errorf("%s: no Clack note in %q", o.Key, o.Description)
		}
	}

	// off: the spellchecker comes back, the switches go, the styles go
	if err := h.s.SetEnabled(h.ctx(), "Snappy", false); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "the composer spellcheck on after Snappy stopped", h.spellOn)
	if got := StartupSwitches(slickDir); len(got) != 0 {
		t.Errorf("switches after Snappy was turned off: %v", got)
	}
	if h.ui.style("plugin:Snappy:transitions:") != "" {
		t.Error("transition overrides left after stop")
	}
}

// A plugin setting a switch with Slick off in settings.json gets nothing
// at start.
func TestStartupSwitchesFollowSettings(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := writeFileAtomic(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(switchesFile, `{"Snappy": ["disable-crashpad", "ignore-gpu-blocklist"], "Other": ["ignore-gpu-blocklist"]}`)
	if got := StartupSwitches(dir); !slices.Equal(got, []string{"disable-crashpad", "ignore-gpu-blocklist"}) {
		t.Errorf("no settings.json: %v", got)
	}
	write("settings.json", `{"plugins": {"Snappy": {"enabled": false}}}`)
	if got := StartupSwitches(dir); !slices.Equal(got, []string{"ignore-gpu-blocklist"}) {
		t.Errorf("Snappy off: %v", got)
	}
	write("settings.json", `{"enabled": false, "plugins": {}}`)
	if got := StartupSwitches(dir); len(got) != 0 {
		t.Errorf("Slick off: %v", got)
	}
}
