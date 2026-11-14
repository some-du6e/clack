package ui

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/app"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/desktop"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// shellTray records what the UI asks of the tray's shell side (images,
// tooltips, menus) besides the balloons.
type shellTray struct {
	fakeTray
	smu   sync.Mutex
	calls []string
	menu  []desktop.TrayMenuItem
}

func (s *shellTray) SetImage(st desktop.TrayState) { s.add("image " + string(st)) }
func (s *shellTray) SetTooltip(tip string)         { s.add("tip " + tip) }
func (s *shellTray) SetMenu(items []desktop.TrayMenuItem) {
	s.smu.Lock()
	s.menu = items
	s.smu.Unlock()
	s.add("menu")
}

func (s *shellTray) add(c string) {
	s.smu.Lock()
	s.calls = append(s.calls, c)
	s.smu.Unlock()
}

func (s *shellTray) take() []string {
	s.smu.Lock()
	defer s.smu.Unlock()
	c := s.calls
	s.calls = nil
	return c
}

func (s *shellTray) items() []desktop.TrayMenuItem {
	s.smu.Lock()
	defer s.smu.Unlock()
	return s.menu
}

// noUpdateCheck is a CheckUpdate that finds nothing (the menus then have
// the update item).
func noUpdateCheck(context.Context) (UpdateInfo, bool, error) { return UpdateInfo{}, false, nil }

// trayOn runs the tray logic on this OS, with a shellTray attached and
// the jump list's shell calls stubbed out.
func trayOn(t *testing.T, a *App) *shellTray {
	t.Helper()
	trayAnyOS = true
	set, removed := setJumpList, jumpListRemoved
	setJumpList = func(string, []desktop.JumpListCategory) error { return nil }
	jumpListRemoved = func(string) ([]desktop.JumpListTask, error) { return nil, nil }
	t.Cleanup(func() { trayAnyOS, setJumpList, jumpListRemoved = false, set, removed })
	tr := &shellTray{}
	a.SetTray(tr)
	a.drain()
	return tr
}

// menuText is a tray menu as text: "-" separators, "[x]"/"[ ]" checkboxes,
// "(o)"/"( )" radio items, "~" greyed, "+" an item with an action.
func menuText(items []desktop.TrayMenuItem, indent string) string {
	var b strings.Builder
	for _, it := range items {
		if it.Sep {
			b.WriteString(indent + "-\n")
			continue
		}
		mark := ""
		switch {
		case it.Check && it.Checked:
			mark = "[x] "
		case it.Check:
			mark = "[ ] "
		case it.Radio && it.Checked:
			mark = "(o) "
		case it.Radio:
			mark = "( ) "
		}
		grey := ""
		if it.Disabled {
			grey = "~"
		}
		accel := ""
		if it.Accel != "" {
			accel = " <" + it.Accel + ">"
		}
		if it.Sub != nil {
			fmt.Fprintf(&b, "%s%s%s%s >\n%s", indent, grey, mark, it.Label, menuText(it.Sub, indent+"  "))
			continue
		}
		act := ""
		if it.Do != nil {
			act = " +"
		}
		fmt.Fprintf(&b, "%s%s%s%s%s%s\n", indent, grey, mark, it.Label, accel, act)
	}
	return b.String()
}

// trayItem is the menu item with label (in submenus too).
func trayItem(items []desktop.TrayMenuItem, label string) *desktop.TrayMenuItem {
	for i := range items {
		if items[i].Label == label {
			return &items[i]
		}
		if it := trayItem(items[i].Sub, label); it != nil {
			return it
		}
	}
	return nil
}

// getTrayStateFromUnreadsInfo and getTooltipText: highlights win, then
// unreads, else rest, with Slack's three tooltips (the plural's =1).
func TestTrayStateLikeSlack(t *testing.T) {
	for _, c := range []struct {
		du   core.DesktopUnreads
		st   desktop.TrayState
		tip  string
		name string
	}{
		{core.DesktopUnreads{}, desktop.TrayRest, "No unread messages", "nothing"},
		{core.DesktopUnreads{Bullet: true}, desktop.TrayRest, "No unread messages", "nothing, bullet on"},
		{core.DesktopUnreads{HasUnreads: true}, desktop.TrayUnread, "You have unread messages", "unreads"},
		{core.DesktopUnreads{HasUnreads: true, Bullet: false}, desktop.TrayUnread, "You have unread messages", "unreads, no bullet"},
		{core.DesktopUnreads{Count: 1, HasUnreads: true}, desktop.TrayHighlight, "You have 1 notification", "one highlight"},
		{core.DesktopUnreads{Count: 12}, desktop.TrayHighlight, "You have 12 notifications", "highlights"},
	} {
		st, tip := trayState(c.du)
		if st != c.st || tip != c.tip {
			t.Errorf("%s: %s %q, want %s %q", c.name, st, tip, c.st, c.tip)
		}
	}
}

// update(): the tray's icon and tooltip follow the app badge's unreads,
// each set only when it changes; a new tray gets both.
func TestTrayIconFollowsUnreads(t *testing.T) {
	a, _, _ := fakePrefsApp(t)
	a.Core().SetExperimentGroup("activity_inbox", "off")
	m := a.model()
	m.HomeCount, m.HomeUnread, m.DMUnread = 0, false, false
	tr := trayOn(t, a)
	if got, want := tr.take(), []string{"image rest", "tip No unread messages", "menu"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("a new tray: %q, want %q", got, want)
	}
	step := func(what string, count int, unread bool, want ...string) {
		t.Helper()
		m.HomeCount, m.HomeUnread = count, unread
		a.updateDock()
		if got := tr.take(); !reflect.DeepEqual(got, want) && !(len(got) == 0 && len(want) == 0) {
			t.Fatalf("%s: %q, want %q", what, got, want)
		}
	}
	step("unchanged", 0, false)
	step("an unread channel", 0, true, "image unread", "tip You have unread messages")
	step("again", 0, true)
	step("a mention", 1, true, "image highlight", "tip You have 1 notification")
	step("more mentions", 4, true, "tip You have 4 notifications")
	step("read", 0, false, "image rest", "tip No unread messages")
}

// createTrayMenu: Slack's items in Slack's order, labels with their
// mnemonics, Launch Control's checkbox and radio items for each launch
// state, the update item, Preferences and Quit with their accelerators,
// Troubleshooting with Clack's items; signed out there is no workspace
// row and no Preferences.
func TestTrayMenuLikeSlack(t *testing.T) {
	a, _, _ := fakePrefsApp(t)
	launch, sets := false, 0
	a.opts.LaunchOnLogin = func() bool { return launch }
	a.opts.SetLaunchOnLogin = func(v bool) error { launch = v; sets++; return nil }
	a.opts.CheckUpdate = noUpdateCheck
	tr := trayOn(t, a)
	logs := "Show Logs in File Manager"
	if runtime.GOOS == "windows" {
		logs = "Show &Logs in Explorer"
	}
	team := a.teamName()
	want := team + ` +
-
&Launch Control >
  [ ] &Launch on Login +
  -
  ~Show Clack &Immediately +
  ~Keep in &Background +
&Check for Updates… +
&Preferences <Ctrl+,> +
-
Troubleshooting >
  ` + logs + ` +
  Show Freeze Reports +
  Copy Diagnostics +
-
&Quit <Ctrl+Q> +
`
	if got := menuText(tr.items(), ""); got != want {
		t.Fatalf("menu, launch off:\n%s\nwant\n%s", got, want)
	}

	// Launch on Login: the shell's entry, and the radio items come alive
	// with Keep in Background (hideOnStartup's default)
	trayItem(tr.items(), "&Launch on Login").Do()
	a.drain()
	a.syncTray()
	if !launch || sets != 1 {
		t.Fatalf("Launch on Login: launch %v after %d sets", launch, sets)
	}
	want = strings.Replace(want, "  [ ] &Launch on Login +\n  -\n  ~Show Clack &Immediately +\n  ~Keep in &Background +\n",
		"  [x] &Launch on Login +\n  -\n  ( ) Show Clack &Immediately +\n  (o) Keep in &Background +\n", 1)
	if got := menuText(tr.items(), ""); got != want {
		t.Fatalf("menu, launch on:\n%s\nwant\n%s", got, want)
	}
	trayItem(tr.items(), "Show Clack &Immediately").Do()
	a.drain()
	if !a.prefs.ShowOnStartup {
		t.Fatal("Show Clack Immediately didn't set hideOnStartup false")
	}
	if it := trayItem(tr.items(), "Show Clack &Immediately"); !it.Checked || trayItem(tr.items(), "Keep in &Background").Checked {
		t.Fatal("the radio items don't follow Show Clack Immediately")
	}
	trayItem(tr.items(), "Keep in &Background").Do()
	a.drain()
	if a.prefs.ShowOnStartup {
		t.Fatal("Keep in Background didn't set hideOnStartup")
	}

	// signed out: no workspace, no Preferences; no updates: no update item
	a.opts.CheckUpdate = nil
	a.data = nil
	a.syncTray()
	got := menuText(tr.items(), "")
	if strings.Contains(got, "\n"+team+" +\n") || strings.HasPrefix(got, team+" +\n") || strings.Contains(got, "&Preferences") || strings.Contains(got, "Check for Updates") {
		t.Fatalf("signed out, no updates:\n%s", got)
	}
	if !strings.HasPrefix(got, "&Launch Control >") || !strings.HasSuffix(got, "&Quit <Ctrl+Q> +\n") {
		t.Fatalf("signed out:\n%s", got)
	}
	// unchanged: no new menu
	tr.take()
	a.syncTray()
	if c := tr.take(); len(c) != 0 {
		t.Errorf("an unchanged menu was set again: %q", c)
	}
}

// Launch on Login from the tray makes an open Preferences dialog read the
// setting again; the dialog's own change doesn't.
func TestLaunchOnLoginTrayAndPrefs(t *testing.T) {
	a, _, _ := fakePrefsApp(t)
	launch, reads := false, 0
	a.opts.LaunchOnLogin = func() bool { reads++; return launch }
	a.opts.SetLaunchOnLogin = func(v bool) error { launch = v; return nil }
	if on, ok := a.launchOnLogin(); on || !ok || reads != 1 {
		t.Fatalf("first read: %v %v, %d reads", on, ok, reads)
	}
	a.launchOnLogin()
	if reads != 1 {
		t.Fatalf("%d reads: the cache didn't hold", reads)
	}
	gen := a.launch.gen
	a.setLaunchFromTray(true)
	if !launch || a.launch.gen == gen || !a.launch.on {
		t.Fatalf("the tray's change: launch %v, gen %d → %d", launch, gen, a.launch.gen)
	}
}

// createJumpList: Workspaces (the workspace, "slack://open?team=…", the
// exe's icon) less what the user removed, and Tasks' Move Clack to
// Current Display (screen.ico, reset-window); remade only when the
// workspace or the locale changes.
func TestJumpListLikeSlack(t *testing.T) {
	k := jumpKey{teamID: "T0FAKETEAM", team: "Fake Workspace", set: true}
	cats := jumpListCategories(k, `C:\Clack\Clack.exe`, nil)
	want := []desktop.JumpListCategory{
		{Name: "Workspaces", Items: []desktop.JumpListTask{{Title: "Fake Workspace", Description: "Fake Workspace",
			Program: `C:\Clack\Clack.exe`, Args: "slack://open?team=T0FAKETEAM", IconPath: `C:\Clack\Clack.exe`}}},
		{Name: "Tasks", Items: []desktop.JumpListTask{{Title: "Move Clack to Current Display", Description: "Move the Clack window to the current display",
			Program: `C:\Clack\Clack.exe`, Args: "slack://app/?action=reset-window", IconPath: `C:\Clack\Clack.exe`, IconIndex: -2}}},
	}
	if !reflect.DeepEqual(cats, want) {
		t.Fatalf("categories\n%+v\nwant\n%+v", cats, want)
	}
	removed := []desktop.JumpListTask{{Program: `C:\Clack\Clack.exe`, Args: "slack://open?team=T0FAKETEAM"}}
	if c := jumpListCategories(k, `C:\Clack\Clack.exe`, removed); len(c[0].Items) != 0 || len(c[1].Items) != 1 {
		t.Fatalf("a removed workspace: %+v", c)
	}
	if c := jumpListCategories(jumpKey{set: true}, `C:\Clack\Clack.exe`, nil); len(c[0].Items) != 0 || len(c[1].Items) != 1 {
		t.Fatalf("signed out: %+v", c)
	}

	a, ca, _ := fakePrefsApp(t)
	ca.S.TeamID, ca.S.TeamName = "T0FAKETEAM", "Fake Workspace"
	trayAnyOS = true
	t.Cleanup(func() { trayAnyOS = false })
	var mu sync.Mutex
	var sets [][]desktop.JumpListCategory
	oldSet, oldRemoved := setJumpList, jumpListRemoved
	setJumpList = func(id string, c []desktop.JumpListCategory) error {
		mu.Lock()
		defer mu.Unlock()
		if id != "" {
			t.Errorf("app id %q: Clack's list is the process's own", id)
		}
		sets = append(sets, c)
		return nil
	}
	jumpListRemoved = func(string) ([]desktop.JumpListTask, error) { return nil, nil }
	t.Cleanup(func() { setJumpList, jumpListRemoved = oldSet, oldRemoved })
	got := func() int { mu.Lock(); defer mu.Unlock(); return len(sets) }
	a.syncTray()
	waitFor(t, a, "the jump list", func() bool { return got() == 1 })
	a.syncTray()
	a.syncTray()
	time.Sleep(50 * time.Millisecond)
	if n := got(); n != 1 {
		t.Fatalf("the jump list was made %d times for one workspace", n)
	}
	mu.Lock()
	first := sets[0]
	mu.Unlock()
	if len(first[0].Items) != 1 || first[0].Items[0].Args != "slack://open?team=T0FAKETEAM" || first[0].Items[0].Title != "Fake Workspace" {
		t.Fatalf("signed in: %+v", first)
	}
	a.data = nil // signed out: remade without the workspace
	a.syncTray()
	waitFor(t, a, "the jump list again", func() bool { return got() == 2 })
}

// slack://app?action=…: quit and reset-window are Clack's actions; the
// others, and other hosts, aren't.
func TestAppActions(t *testing.T) {
	for in, want := range map[string]string{
		"slack://app/?action=reset-window": "reset-window",
		"slack://app?action=QUIT":          "quit",
		"SLACK://APP/?action=reveal-logs":  "reveal-logs",
		"slack://app/":                     "",
		"slack://channel?id=C0&action=x":   "",
		"https://app/?action=quit":         "",
	} {
		if got := appAction(in); got != want {
			t.Errorf("%s: %q, want %q", in, got, want)
		}
	}
	a, _, _ := fakePrefsApp(t)
	if a.openDeepLink("slack://app/?action=clear-cache-and-restart") || a.openDeepLink("slack://app/?action=reveal-logs") {
		t.Error("an app action Clack has nothing for counted as handled")
	}
}

// MOVE_WINDOW_TO_PRIMARY_SCREEN: the move on the window's thread, then the
// maximize; with no window yet it waits for one.
func TestResetWindow(t *testing.T) {
	a, _, _ := fakePrefsApp(t)
	trayAnyOS = true
	var hwnd uintptr
	var moved []uintptr
	old := moveToCurrentDisplay
	moveToCurrentDisplay = func(h uintptr) bool { moved = append(moved, h); return true }
	taskbarHWNDHook = func() uintptr { return hwnd }
	t.Cleanup(func() { trayAnyOS, taskbarHWNDHook, moveToCurrentDisplay = false, nil, old })
	a.win = new(app.Window) // no driver: Option is recorded
	t.Cleanup(func() { a.win = nil })
	set, removed := setJumpList, jumpListRemoved
	setJumpList = func(string, []desktop.JumpListCategory) error { return nil }
	jumpListRemoved = func(string) ([]desktop.JumpListTask, error) { return nil, nil }
	t.Cleanup(func() { setJumpList, jumpListRemoved = set, removed })
	if !a.openDeepLink(resetWindowLink) {
		t.Fatal("reset-window wasn't handled")
	}
	a.drain()
	if len(moved) != 0 || !a.resetPending {
		t.Fatalf("no window yet: moved %v, pending %v", moved, a.resetPending)
	}
	hwnd = 0x1234
	a.syncTray()
	if !reflect.DeepEqual(moved, []uintptr{0x1234}) || a.resetPending {
		t.Fatalf("the window came: moved %v, pending %v", moved, a.resetPending)
	}
}

// Keep in Background (hideOnStartup, Slack's default) leaves a window that
// Launch on Login started in the notification area; Show Clack
// Immediately, a normal start, or no tray icon shows it.
func TestStartHiddenLikeSlack(t *testing.T) {
	a, _, _ := fakePrefsApp(t)
	trayAnyOS = true
	t.Cleanup(func() { trayAnyOS = false })
	if a.startHidden() {
		t.Fatal("a normal start, no tray: hidden")
	}
	a.opts.InvokedOnStartup = true
	if a.startHidden() {
		t.Fatal("no tray icon to come back from: hidden")
	}
	a.SetTray(&shellTray{})
	if !a.startHidden() {
		t.Fatal("launched on login, Keep in Background: shown")
	}
	a.prefs.ShowOnStartup = true
	if a.startHidden() {
		t.Fatal("Show Clack Immediately: hidden")
	}
}

// SHOULD_PLAY_SOUND: the shell's "sound playback" check (Windows: toasts
// on, Focus Assist off) holds the message sound like DND does.
func TestSoundPlaybackHoldsTheSound(t *testing.T) {
	a, ca, _ := fakePrefsApp(t)
	t.Cleanup(uipref.Reset)
	uipref.Set(ca.Prefs())
	var snd soundLog
	snd.hook(t)
	a.notify = &recNotifier{}
	ca.SetDNDStatus(core.DNDStatus{})
	old := soundPlayback
	allow := false
	soundPlayback = func() bool { return allow }
	t.Cleanup(func() { soundPlayback = old })
	a.notifyMessage("t", "b", nil)
	time.Sleep(100 * time.Millisecond)
	if n := len(snd.got()); n != 0 {
		t.Fatalf("a sound played while the shell said no: %v", snd.got())
	}
	allow = true
	a.notifyMessage("t", "b", nil)
	waitFor(t, a, "the sound", func() bool { return len(snd.got()) == 1 })

	// the real check on this OS: toasts and Focus Assist as desktop says
	fa, te := focusAssistActive, toastsEnabled
	t.Cleanup(func() { focusAssistActive, toastsEnabled = fa, te })
	soundPlayback = old
	for _, c := range []struct{ toasts, focus, want bool }{{true, false, true}, {true, true, false}, {false, false, false}} {
		toastsEnabled = func() bool { return c.toasts }
		focusAssistActive = func() bool { return c.focus }
		if got := soundPlayback(); got != c.want {
			t.Errorf("toasts %v, Focus Assist %v: %v, want %v", c.toasts, c.focus, got, c.want)
		}
	}
}
