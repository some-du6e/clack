package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uiguard"
)

// The macOS freeze (Karim, alpha.7/8): clicking around Preferences
// beachballed Clack, and it froze again at every launch, also with ui.json
// moved away. These tests start the whole client headlessly against
// fakeslack in every state a confused user could leave behind, on this
// device (ui.json) and in the account (users.prefs: theme, gradients,
// sidebar and read prefs), cold and warm (from the metadata cache), and
// assert that no frame and no posted UI work blocks: nothing marked with
// uiguard.OffUI runs on the UI goroutine, the frame watchdog writes no
// freeze report, and no frame takes freezeBudget. The macOS CI job runs
// them too.

// freezeBudget is the slowest frame allowed (the watchdog reports at 2 s).
const freezeBudget = 1500 * time.Millisecond

type freezeState struct {
	name  string
	ui    any            // ui.json (a map, or raw string for junk); nil: none
	acct  map[string]any // users.prefs on top of acctDefaults
	prefs bool           // open every Preferences section and click through Appearance
	warm  bool           // restart from the caches the first session wrote
	os    int            // OS scheme to report: 0 none, 1 light, 2 dark
}

// acctDefaults resets the prefs every state may change.
var acctDefaults = map[string]any{
	"ia_theme": "", "window_background_gradients": true, "sidebar_behavior": "", "channel_sort": "default",
	"mark_msgs_read_immediately": false, "start_scroll_at_oldest": false, "display_real_names_override": 0,
	"plain_text_mode": false, "emoji_style": "apple",
}

func freezeStates(chans []string) []freezeState {
	ch := ""
	if len(chans) > 0 {
		ch = chans[0]
	}
	st := []freezeState{
		{name: "fresh", prefs: true, warm: true},
		{name: "system-light-saved", ui: map[string]any{"theme_sync": true, "dark": false}, os: 2, prefs: true},
		{name: "system-dark-saved", ui: map[string]any{"theme_sync": true, "dark": true}, os: 1, prefs: true},
		{name: "system-no-os", ui: map[string]any{"theme_sync": true}},
		{name: "dark", ui: map[string]any{"theme_sync": false, "dark": true}, os: 1, prefs: true, warm: true},
		{name: "light", ui: map[string]any{"theme_sync": false, "dark": false}, os: 2},
		{name: "everything-toggled", ui: map[string]any{
			"theme_sync": true, "dark": true, "notifications": false, "emoji_style": "google", "sidebar_hidden": true,
			"no_update_check": true, "update_dismissed": "v99.0.0", "realtime": true, "plain_text_mode": true,
			"collapsed": []string{"channels", "dms", "starred", "nope"}, "sidebar_w": 9999, "secondary_w": 99999,
			"route": "huddles", "last_channel": "CNOPE"}, os: 2, prefs: true, warm: true},
		{name: "junk-ui-json", ui: "{\"dark\": tru"},
		{name: "odd-ui-values", ui: map[string]any{"sidebar_w": -5, "secondary_w": 0, "dark": nil, "theme_sync": nil, "route": "%%%", "collapsed": []string{""}}},
	}
	for _, r := range []string{"activity", "later", "files", "dms", "threads", "drafts", "directories", "huddles", "search?q=hello",
		"c/" + ch, "c/" + ch + "/t/1700000000.000100", "c/" + ch + "/m/1700000000.000100", "c/CNOPE", "nope/nope"} {
		st = append(st, freezeState{name: "route-" + strings.NewReplacer("/", "_", "?", "_").Replace(r), ui: map[string]any{"route": r}})
	}
	for _, pr := range theme.ThemePresets {
		st = append(st, freezeState{name: "theme-" + pr.ID, acct: map[string]any{"ia_theme": pr.Theme.JSON()}})
	}
	custom := func(b int, inv bool, hexes ...string) string {
		th := theme.DefaultIATheme
		th.UseCustomHex, th.Brightness, th.SidebarInverted = true, b, inv
		rs := []*theme.IARole{&th.Primary, &th.Highlight1, &th.Highlight2, &th.Important}
		for i, r := range rs {
			r.Custom = hexes[i%len(hexes)]
		}
		return th.JSON()
	}
	for _, c := range []struct {
		name string
		v    string
	}{
		{"custom-black", custom(6, true, "#000000")},
		{"custom-white", custom(1, false, "#FFFFFF")},
		{"custom-grey", custom(3, true, "#808080", "#7F7F80")},
		{"custom-mixed", custom(4, true, "#FF0000", "#00FF00", "#0000FF", "#FFFF00")},
		{"custom-brightness0", custom(0, true, "#123456")},
		{"custom-brightness9", custom(9, true, "#abcdef")},
		{"custom-junk-hex", custom(5, true, "#zzzzzz", "", "#12", "fff")},
		{"ia-theme-junk", "{"},
		{"ia-theme-array", "[]"},
		{"ia-theme-wrong-types", `{"brightness":"6","sidebarInverted":"yes","primary":"ocean"}`},
	} {
		st = append(st, freezeState{name: c.name, acct: map[string]any{"ia_theme": c.v},
			warm:  strings.HasPrefix(c.name, "custom-b") || c.name == "ia-theme-junk",
			prefs: c.name == "custom-mixed"}) // Appearance with a custom theme
	}
	st = append(st,
		freezeState{name: "no-gradients", acct: map[string]any{"window_background_gradients": false}, prefs: true},
		freezeState{name: "dark-custom-no-gradients", ui: map[string]any{"theme_sync": false, "dark": true},
			acct: map[string]any{"ia_theme": custom(2, true, "#1a1d21"), "window_background_gradients": false}, warm: true},
	)
	for _, v := range []string{"hide_read_channels", "hide_read_channels_unless_starred", "show_all", "zzz"} {
		st = append(st, freezeState{name: "sidebar_behavior-" + v, acct: map[string]any{"sidebar_behavior": v}})
	}
	for _, v := range []string{"alphabetical", "recent", "priority", "zzz"} {
		st = append(st, freezeState{name: "channel_sort-" + v, acct: map[string]any{"channel_sort": v}})
	}
	st = append(st, freezeState{name: "read-and-names-prefs", acct: map[string]any{
		"mark_msgs_read_immediately": true, "start_scroll_at_oldest": true, "display_real_names_override": -1,
		"plain_text_mode": true, "emoji_style": "google"}, prefs: true, warm: true})
	return st
}

func TestStartupStatesNeverBlock(t *testing.T) {
	if testing.Short() {
		t.Skip("starts the client ~80 times")
	}
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	type slowOne struct {
		d    time.Duration
		what string
	}
	var slow []slowOne
	for _, st := range freezeStates(fs.ChannelIDs()) {
		st := st
		t.Run(st.name, func(t *testing.T) {
			acct := map[string]any{}
			for k, v := range acctDefaults {
				acct[k] = v
			}
			for k, v := range st.acct {
				acct[k] = v
			}
			fs.SetPrefs(acct)
			t.Cleanup(composer.ResetPlainTextMode) // ui.json's plain_text_mode is process-wide
			home := freezeHome(t)
			switch v := st.ui.(type) {
			case nil:
			case string:
				writeFile(t, filepath.Join(home, "ui.json"), []byte(v))
			default:
				b, _ := json.Marshal(v)
				writeFile(t, filepath.Join(home, "ui.json"), b)
			}
			switch st.os {
			case 1:
				setOSScheme(false)
			case 2:
				setOSScheme(true)
			}
			r := runFreezeSession(t, fs, home, st, false)
			if st.warm {
				r2 := runFreezeSession(t, fs, home, st, true)
				if r2.worst > r.worst {
					r = r2
				}
			}
			slow = append(slow, slowOne{r.worst, fmt.Sprintf("%v %s (%s)", r.worst.Round(time.Millisecond), st.name, r.worstAt)})
		})
	}
	sort.Slice(slow, func(i, j int) bool { return slow[i].d > slow[j].d })
	var top []string
	for i := 0; i < len(slow) && i < 8; i++ {
		top = append(top, slow[i].what)
	}
	t.Logf("slowest frames: %s", strings.Join(top, "; "))
}

type freezeResult struct {
	worst   time.Duration
	worstAt string
}

func runFreezeSession(t *testing.T, fs *fakeslack.Server, home string, st freezeState, warm bool) freezeResult {
	t.Helper()
	t.Setenv("CLACK_HOME", home) // Taut's and Slick's dirs, per state
	uiguard.Reset()
	uiguard.MarkUI()
	defer uiguard.Unmark()
	e := startEnv(t, fs, home)
	a := e.a
	a.watchOSTheme()
	sh := <-a.shaperCh // what Run's first frames do without waiting
	a.th.SetShapers(sh[0], sh[1])

	wd := newWatchdog(home, time.Now())
	wd.after = freezeBudget
	var mu sync.Mutex
	var reports []string
	wd.written = func(p string) { mu.Lock(); reports = append(reports, p); mu.Unlock() }
	go wd.run()
	defer wd.stop()

	var res freezeResult
	frame := func(label string) {
		wd.begin(&stageLayout)
		t0 := time.Now()
		e.frame()
		d := time.Since(t0)
		wd.end()
		if d > res.worst {
			res.worst, res.worstAt = d, label
		}
		if d > freezeBudget {
			t.Errorf("warm=%v: frame %q took %v", warm, label, d)
		}
	}
	for i := 0; i < 3; i++ {
		frame("first frames")
	}
	dl := time.Now().Add(30 * time.Second)
	for a.data.booted.IsZero() {
		if time.Now().After(dl) {
			t.Fatalf("warm=%v: core not ready after 30s", warm)
		}
		frame("boot")
		time.Sleep(2 * time.Millisecond)
	}
	for end := time.Now().Add(300 * time.Millisecond); time.Now().Before(end); {
		frame("after boot")
		time.Sleep(2 * time.Millisecond)
	}
	if st.prefs && !warm {
		for _, sec := range prefsNav {
			a.showPrefs(sec.id)
			for i := 0; i < 3; i++ {
				frame("Preferences > " + sec.id)
			}
			e.key(key.NameEscape, 0)
			frame("close Preferences")
			a.overlays = nil
		}
		clickAppearance(t, e, frame)
	}
	frame("last")
	a.flushPrefs()
	if v := uiguard.Violations(); len(v) > 0 {
		t.Errorf("warm=%v: blocking calls on the UI goroutine: %v", warm, v)
	}
	mu.Lock()
	for _, p := range reports {
		b, _ := os.ReadFile(p)
		t.Errorf("warm=%v: the watchdog wrote a freeze report:\n%.4000s", warm, b)
	}
	mu.Unlock()
	<-a.data.stop() // writes the caches a warm start reads
	a.data = nil
	return res
}

// clickAppearance opens Appearance and clicks every control on it, the way
// Karim clicked around: modes, System, every preset, every palette of
// every role, brightness, darker sidebars, gradients, emoji sets.
func clickAppearance(t *testing.T, e *perfEnv, frame func(string)) {
	t.Helper()
	a := e.a
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}, section: "appearance"}
	mk := mkPrefsModal
	if mk == nil {
		mk = mkModal
	}
	if mk == nil {
		t.Skip("no modal hook")
	}
	a.ShowOverlay(mk(a, "Preferences", func(gtx layout.Context) layout.Dimensions { return p.layout(gtx) }, p.close))
	frame("Appearance")
	frame("Appearance")
	keys := make([]string, 0, len(p.clicks))
	for k := range p.clicks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) < 20 {
		t.Fatalf("Appearance has only %d controls: %v", len(keys), keys)
	}
	for i, k := range keys {
		if c := p.clicks[k]; c != nil {
			c.Click()
		}
		frame("click " + k)
		if i%8 == 0 {
			time.Sleep(20 * time.Millisecond) // let some account writes land
			frame("after click " + k)
		}
	}
	for end := time.Now().Add(300 * time.Millisecond); time.Now().Before(end); {
		frame("after clicking Appearance")
		time.Sleep(5 * time.Millisecond)
	}
	a.overlays = nil
}

// freezeHome is a config dir for one state. Not t.TempDir: the session's
// background writers (sidebar and metadata caches, ui.json) may still land
// a file just after the test ends, so removal retries for a moment.
func freezeHome(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "clack-freeze-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for i := 0; i < 20; i++ {
			if os.RemoveAll(dir) == nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	})
	return dir
}

func writeFile(t *testing.T, p string, b []byte) {
	t.Helper()
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// The OS scheme reaches the UI through the atomic: following it never
// calls the OS.
func TestOSSchemeFollow(t *testing.T) {
	a, _, _ := fakeApp(t, nil)
	on := true
	a.prefs.ThemeSync = &on
	uiguard.Reset()
	uiguard.MarkUI()
	defer uiguard.Unmark()
	for _, dark := range []bool{true, false, true} {
		setOSScheme(dark)
		a.followOSTheme()
		a.dispatchWait()
		if a.th.Dark != dark {
			t.Errorf("OS dark=%v: app dark=%v", dark, a.th.Dark)
		}
		if got, ok := osSchemeDark(); !ok || got != dark {
			t.Errorf("osSchemeDark = %v, %v", got, ok)
		}
	}
	a.flushPrefs()
	if v := uiguard.Violations(); len(v) > 0 {
		t.Errorf("blocking calls on the UI goroutine: %v", v)
	}
}

// The watchdog writes one report for a stuck task, redacted, with the
// stacks, and notes when the task finished.
func TestWatchdogReport(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "ui.json"), []byte(`{"route":"c/C1","note":"xoxc-123456-abcdef d=xoxd-SECRET"}`))
	wd := newWatchdog(home, time.Now())
	wd.after = 300 * time.Millisecond
	got := make(chan string, 4)
	wd.written = func(p string) { got <- p }
	go wd.run()
	defer wd.stop()
	wd.notePage("c")
	wd.begin(&stageLayout)
	var path string
	select {
	case path = <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("no report for a task stuck past the limit")
	}
	time.Sleep(300 * time.Millisecond)
	select {
	case p := <-got:
		t.Fatalf("a second report for the same task: %s", p)
	default:
	}
	wd.end()
	time.Sleep(600 * time.Millisecond)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{"Clack freeze report", "frame layout", "page: c", "goroutine ", "TestWatchdogReport", "responsive again"} {
		if !strings.Contains(s, want) {
			t.Errorf("report lacks %q:\n%.2000s", want, s)
		}
	}
	for _, secret := range []string{"123456-abcdef", "xoxd-SECRET"} {
		if strings.Contains(s, secret) {
			t.Errorf("report leaks %q", secret)
		}
	}
	if !strings.HasPrefix(filepath.Base(path), "freeze-") || filepath.Dir(path) != filepath.Join(home, "freeze-reports") {
		t.Errorf("report at %s", path)
	}
	// a quick task writes nothing
	wd.begin(&stageLayout)
	wd.end()
	time.Sleep(500 * time.Millisecond)
	select {
	case p := <-got:
		t.Errorf("report for a quick task: %s", p)
	default:
	}
}

// Run's first frames don't wait for the shaper.
func TestTakeShaperDoesNotWait(t *testing.T) {
	a := New(Options{ConfigDir: t.TempDir()}, nil)
	sh := <-a.shaperCh // hold it back
	t0 := time.Now()
	if a.takeShaper() {
		t.Fatal("took a shaper that wasn't there")
	}
	if d := time.Since(t0); d > 50*time.Millisecond {
		t.Errorf("takeShaper waited %v", d)
	}
	a.shaperCh <- sh
	if !a.takeShaper() || a.th.Shaper == nil {
		t.Fatal("didn't install the ready shaper")
	}
}

// Startup with every external tool Clack might run replaced by one that
// hangs and leaves a child holding its stdout (the shape of the pre-fix
// freeze: `defaults` exec'd on the UI goroutine at launch). The first
// frames, boot and Preferences must not wait on any of them, and
// `defaults` is never run at all.
func TestStartupWithHangingTools(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts")
	}
	bin := t.TempDir()
	marker := filepath.Join(t.TempDir(), "ran")
	script := "#!/bin/sh\necho \"$0 $*\" >> " + marker + "\n(sleep 60) &\nsleep 60\n"
	for _, tool := range []string{"defaults", "osascript", "pngpaste", "open", "xdg-open", "wl-paste", "xclip", "zenity", "kdialog", "dpkg-query", "rpm", "stty", "xdg-mime"} {
		writeFile(t, filepath.Join(bin, tool), []byte(script))
		if err := os.Chmod(filepath.Join(bin, tool), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	// the OS scheme read itself (the watcher's) answers without exec
	done := make(chan struct{})
	go func() { osPrefersDark(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("osPrefersDark is stuck")
	}

	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	fs.SetPrefs(acctDefaults)
	t.Cleanup(composer.ResetPlainTextMode)
	home := freezeHome(t) // fresh: nothing saved, so Sync with OS is on
	t0 := time.Now()
	r := runFreezeSession(t, fs, home, freezeState{name: "hanging-tools", prefs: true}, false)
	t.Logf("startup and Preferences with hanging tools: %v, slowest frame %v (%s)", time.Since(t0).Round(time.Millisecond), r.worst.Round(time.Millisecond), r.worstAt)
	if b, err := os.ReadFile(marker); err == nil {
		for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if strings.Contains(l, "/defaults") {
				t.Errorf("ran %s", l)
			} else {
				t.Logf("ran (off the UI goroutine) %s", l)
			}
		}
	}
}
