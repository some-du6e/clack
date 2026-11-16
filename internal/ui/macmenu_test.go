package ui

import (
	"context"
	"encoding/json"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/menubar"
	"github.com/some-du6e/clack/internal/ui/popups"
)

func testMenus() []menubar.Menu {
	a := &App{opts: Options{CheckUpdate: func(context.Context) (UpdateInfo, bool, error) { return UpdateInfo{}, false, nil }}}
	return menubar.Clean(a.macMenus())
}

// renderMenus is the tree as title|accelerator|role, "→" for Clack's own
// actions (with "fwd" when the key goes to Gio's key handling).
func renderMenus(menus []menubar.Menu) string {
	var b strings.Builder
	var walk func(items []menubar.Item, depth int)
	walk = func(items []menubar.Item, depth int) {
		for _, it := range items {
			b.WriteString(strings.Repeat("  ", depth))
			switch {
			case it.Sep && it.Recent:
				b.WriteString("--- (recent views)\n")
				continue
			case it.Sep:
				b.WriteString("---\n")
				continue
			}
			act := it.Role
			if act == "" && it.Sub == nil {
				act = "→"
				if it.Forward {
					act = "→ fwd"
				}
			}
			b.WriteString(it.Title + "|" + it.Accel() + "|" + act + "\n")
			walk(it.Sub, depth+1)
		}
	}
	for _, m := range menus {
		b.WriteString(m.Title)
		if m.Kind != "" {
			b.WriteString(" [" + m.Kind + "]")
		}
		b.WriteString("\n")
		walk(m.Items, 1)
	}
	return b.String()
}

// Slack desktop 4.53.276's macOS menus (see macmenu.go for where each
// comes from), less the items Clack has no feature for.
const wantMenus = `Clack [app]
  About Clack||orderFrontStandardAboutPanel:
  Check for Updates…||→
  ---
  Preferences…|⌘,|→ fwd
  ---
  Services||services
  ---
  Hide Clack|⌘H|hide:
  Hide Others|⌥⌘H|hideOtherApplications:
  Show All||unhideAllApplications:
  ---
  Sign Out||
    Sign Out||→
  ---
  Quit Clack|⌘Q|→
File
  New Message|⌘N|→ fwd
  New Channel||→
  New Canvas|⇧⌘N|→ fwd
  ---
  Invite People||→
  ---
  Close|⌘W|clackClose:
Edit
  Undo|⌘Z|undo:
  Redo|⇧⌘Z|redo:
  ---
  Cut|⌘X|cut:
  Copy|⌘C|copy:
  Paste|⌘V|paste:
  Paste and Match Style|⇧⌘V|pasteAsPlainText:
  Delete||delete:
  Select All|⌘A|selectAll:
  ---
  Search|⌘G|→ fwd
  ---
  Find…|⌘F|→ fwd
View
  Toggle Full Screen|⌃⌘F|toggleFullScreen:
  ---
  Hide Sidebar|⇧⌘D|→ fwd
  ---
  Actual Size|⌘0|→ fwd
  Zoom In|⌘+|→ fwd
  Zoom Out|⌘-|→ fwd
Go
  Switch to Channel|⌘K|→ fwd
  ---
  All Unreads|⇧⌘A|→ fwd
  Threads|⇧⌘T|→ fwd
  All DMs|⇧⌘K|→ fwd
  Drafts & sent||→
  Activity|⇧⌘M|→ fwd
  Later||→
  ---
  Channel Browser|⇧⌘L|→ fwd
  File Browser||→
  People & User Groups|⇧⌘E|→ fwd
  Workflows & Apps||→
  ---
  Profile||→
History
  Back|⌘[|→ fwd
  Forward|⌘]|→ fwd
  --- (recent views)
  ---
  Show full history|⌘Y|→
Window [window]
  Minimize|⌘M|performMiniaturize:
  Zoom||performZoom:
  ---
  Bring All to Front||arrangeInFront:
Help [help]
  Keyboard Shortcuts||→
  Open Help Center||→
  Troubleshooting||
    Show Logs in File Manager||→
    Show Freeze Reports||→
    Copy Diagnostics||→
  ---
  Release Notes||→
  About Clack||orderFrontStandardAboutPanel:
`

func TestMacMenus(t *testing.T) {
	want := wantMenus
	if runtime.GOOS == "windows" { // the updater workflow runs this on Windows too (troubleshootingMenu)
		want = strings.Replace(want, "Show Logs in File Manager", "Show Logs in Explorer", 1)
	}
	if got := renderMenus(testMenus()); got != want {
		t.Errorf("menus:\n%s\nwant:\n%s", got, want)
	}
}

func eachItem(menus []menubar.Menu, f func(menubar.Item)) {
	var walk func([]menubar.Item)
	walk = func(items []menubar.Item) {
		for _, it := range items {
			f(it)
			walk(it.Sub)
		}
	}
	for _, m := range menus {
		walk(m.Items)
	}
}

// Every item does something, and a Forward item's key is one Clack's
// window binds on macOS (keys.go), or its key press would do nothing.
func TestMacMenuActions(t *testing.T) {
	a := &App{}
	bound := map[[2]string]bool{}
	for _, sc := range a.defaultShortcuts().list {
		bound[[2]string{string(sc.name), sc.mods.String()}] = true
	}
	for _, b := range navBindings("darwin") { // the registry has this OS's
		bound[[2]string{string(b.name), darwinMods(b.mods)}] = true
	}
	seen := map[string]string{}
	eachItem(testMenus(), func(it menubar.Item) {
		if it.Sep || it.Sub != nil {
			return
		}
		if it.Role == "" && it.Do == nil {
			t.Errorf("%s does nothing", it.Title)
		}
		if it.Role != "" && it.Do != nil {
			t.Errorf("%s has a role and an action", it.Title)
		}
		if it.ID == "" {
			t.Errorf("%s has no id", it.Title)
		}
		if acc := it.Accel(); acc != "" {
			if other, dup := seen[acc]; dup {
				t.Errorf("%s and %s both use %s", other, it.Title, acc)
			}
			seen[acc] = it.Title
		}
		if !it.Forward {
			return
		}
		var m key.Modifiers
		if it.Mods&menubar.Cmd != 0 {
			m |= key.ModCommand // key.ModShortcut on macOS
		}
		if it.Mods&menubar.Shift != 0 {
			m |= key.ModShift
		}
		if it.Mods&menubar.Option != 0 {
			m |= key.ModAlt
		}
		if it.Mods&menubar.Ctrl != 0 {
			m |= key.ModCtrl
		}
		name := strings.ToUpper(it.Key)
		if !bound[[2]string{name, darwinMods(m)}] {
			t.Errorf("%s (%s) is forwarded to Gio, but no shortcut binds it", it.Title, it.Accel())
		}
	})
}

// darwinMods is m as keys.go's ModShortcut entries read on macOS, where
// ModShortcut is ModCommand; the registry is built for the test's OS.
func darwinMods(m key.Modifiers) string {
	if m.Contain(key.ModCommand) {
		m = m&^key.ModCommand | key.ModShortcut
	}
	return m.String()
}

func TestMenuStateSignedOut(t *testing.T) {
	a := &App{}
	s := a.menuState(menuKey{})
	for id := range menuWorkspaceOnly {
		if !s.Disabled[id] {
			t.Errorf("%s enabled on the sign-in screen", id)
		}
	}
	ids := map[string]bool{}
	eachItem(testMenus(), func(it menubar.Item) { ids[it.ID] = true })
	for id := range menuWorkspaceOnly {
		if !ids[id] {
			t.Errorf("menuWorkspaceOnly names %s, which isn't in the menus", id)
		}
	}
}

// History's recent places are the web client's (a6xy gJ): only with
// desktop_history_menu, newest first, one per place (the current one too),
// places with no label left out, at most 15, then Show full history when
// there are more.
func TestMenuStateRecent(t *testing.T) {
	a := &App{}
	for _, r := range []Route{"activity", "c/C1", "threads", "c/C1", "dms", "later", "files", "drafts", "unreads", "huddles"} {
		a.router.Push(r)
	}
	k := menuKey{signedIn: true, back: a.router.CanBack(), sidebar: true, team: "Hack Club", cur: a.router.Current()}
	s := a.menuState(k)
	if len(s.Recent) != 0 || !s.Hidden[menuHistoryShowAll] || !s.Hidden[menuHistoryMoreSep] {
		t.Errorf("without desktop_history_menu: recent %v, hidden %v", s.Recent, s.Hidden)
	}
	if s.Titles["menuitem-toggle-sidebar"] != "Show Sidebar" || s.Titles[menuSignOutTeam] != "Hack Club" {
		t.Errorf("titles %v", s.Titles)
	}
	if s.Disabled["menuitem-navigate-back"] || !s.Disabled["menuitem-navigate-forward"] {
		t.Errorf("disabled %v", s.Disabled)
	}

	ca := core.New(slack.New(slack.Creds{Token: "xoxc-test"}), store.New(""))
	ca.SetExperimentGroup("desktop_history_menu", "on")
	a.data = &backend{core: ca, app: a}
	k.historyMenu = true
	s = a.menuState(k)
	var got []string
	for _, r := range s.Recent {
		got = append(got, r.Route+"="+r.Title)
	}
	// c/C1 isn't a conversation Clack knows: no label, no entry
	want := "huddles=Huddles unreads=All unreads drafts=Drafts & sent files=Files later=To-dos dms=DMs threads=All Threads activity=Activity"
	if j := strings.Join(got, " "); j != want {
		t.Errorf("recent = %q, want %q", j, want)
	}
	if !s.Hidden[menuHistoryShowAll] {
		t.Error("Show full history shown with 8 places")
	}
	for i := range 20 {
		a.router.Push(Route("search?q=q" + strconv.Itoa(i)))
	}
	s = a.menuState(k)
	if len(s.Recent) != historyOSMax || s.Recent[0].Title != "q19" || s.Hidden[menuHistoryShowAll] || s.Hidden[menuHistoryMoreSep] {
		t.Errorf("past 15 places: %d recent (first %+v), hidden %v", len(s.Recent), s.Recent[0], s.Hidden)
	}
}

func TestZoomLevels(t *testing.T) {
	a := &App{}
	if f := a.zoomFactor(); f != 1 {
		t.Fatalf("default zoom %v", f)
	}
	for range 10 {
		a.zoom(1)
	}
	if a.prefs.Zoom != zoomMax || a.zoomFactor() != 2 {
		t.Errorf("max zoom %d %v", a.prefs.Zoom, a.zoomFactor())
	}
	for range 20 {
		a.zoom(-1)
	}
	if a.prefs.Zoom != zoomMin || a.zoomFactor() != 0.7 {
		t.Errorf("min zoom %d %v", a.prefs.Zoom, a.zoomFactor())
	}
	a.zoom(0)
	if a.prefs.Zoom != 0 {
		t.Errorf("reset %d", a.prefs.Zoom)
	}
}

// File's web-client items are getFileMenuItems' (boot-render, build
// 2c0d02ca130f): New Message ⌘N, New Channel, New Canvas ⇧⌘N, then Invite
// People after the Workspace submenu's separator. Choosing each runs the
// real action on a fake Slack: the create-channel modal, a new canvas
// opened in full view, the invite modal with the click handler's source.
// Slack leaves New Canvas out for frontline members and free teams, and
// Invite People out when canUserSeeInviteModal says no.
func TestMacMenuFileItems(t *testing.T) {
	a, ca, _ := fakeApp(t, nil)
	file := map[string]menubar.Item{}
	var order []string
	for _, m := range menubar.Clean(a.macMenus()) {
		if m.Title != "File" {
			continue
		}
		for _, it := range m.Items {
			if it.Sep {
				order = append(order, "---")
				continue
			}
			order = append(order, it.Title)
			file[it.ID] = it
		}
	}
	if got, want := strings.Join(order, " | "), "New Message | New Channel | New Canvas | --- | Invite People | --- | Close"; got != want {
		t.Errorf("File = %q, want %q", got, want)
	}
	for _, w := range []struct{ id, title, accel string }{
		{menuNewChannel, "New Channel", ""},
		{menuNewCanvas, "New Canvas", "⇧⌘N"},
		{menuInvitePeople, "Invite People", ""},
	} {
		it, ok := file[w.id]
		if !ok {
			t.Fatalf("no %s in File", w.id)
		}
		if it.Title != w.title || it.Accel() != w.accel || it.Do == nil || it.Role != "" {
			t.Errorf("%s: %q %q do=%v role=%q, want %q %q", w.id, it.Title, it.Accel(), it.Do != nil, it.Role, w.title, w.accel)
		}
		if !menuWorkspaceOnly[w.id] {
			t.Errorf("%s isn't greyed on the sign-in screen", w.id)
		}
	}

	k := a.menuKeyNow()
	if !k.signedIn {
		t.Fatal("the fake app isn't signed in")
	}
	st := a.menuState(k)
	for _, id := range []string{menuNewChannel, menuNewCanvas, menuInvitePeople, menuInviteSep} {
		if st.Hidden[id] || st.Disabled[id] {
			t.Errorf("a member of the fake team: %s hidden %v, disabled %v", id, st.Hidden[id], st.Disabled[id])
		}
	}

	// New Channel: the create-channel modal (createChannel, app_menu)
	file[menuNewChannel].Do()
	waitTheme(t, a, "the create-channel modal", func() bool {
		_, ok := topOverlay[*popups.CreateChannelModal](a)
		return ok
	})
	// Invite People: the invite modal, source universal_click_handler
	file[menuInvitePeople].Do()
	waitTheme(t, a, "the invite modal", func() bool { return lastInviteModal(a) != nil })
	if src := lastInviteModal(a).Source(); src != "universal_click_handler" {
		t.Errorf("invite source %q, want universal_click_handler", src)
	}
	// New Canvas: canvases.create, then the canvas in full view
	file[menuNewCanvas].Do()
	waitTheme(t, a, "the new canvas", func() bool { return strings.HasPrefix(string(a.router.Current()), "docs/") })

	// the gates: a frontline member gets neither (KBR4 q; c1OI Lz)
	self := slack.User{ID: fakeslack.SelfID, Name: "self", RestrictedUserType: "frontline_worker"}
	ca.S.Lock()
	ca.S.Self = self
	ca.S.Unlock()
	ca.S.PutUsers([]slack.User{self})
	st = a.menuState(a.menuKeyNow())
	for _, id := range []string{menuNewCanvas, menuInvitePeople, menuInviteSep} {
		if !st.Hidden[id] {
			t.Errorf("frontline: %s shown", id)
		}
	}
	if st.Hidden[menuNewChannel] {
		t.Error("frontline: New Channel hidden")
	}
	// a full member on a team where only admins invite and nobody may
	// request invites: New Canvas, no Invite People
	self.RestrictedUserType = ""
	ca.S.Lock()
	ca.S.Self = self
	ca.S.Unlock()
	ca.S.PutUsers([]slack.User{self})
	ca.SetTeamPrefs(map[string]json.RawMessage{"invites_only_admins": json.RawMessage("true")})
	st = a.menuState(a.menuKeyNow())
	if st.Hidden[menuNewCanvas] || !st.Hidden[menuInvitePeople] || !st.Hidden[menuInviteSep] {
		t.Errorf("invites_only_admins: hidden %v", st.Hidden)
	}
}
