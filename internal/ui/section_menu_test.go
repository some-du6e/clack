package ui

import (
	"encoding/json"
	"image"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
)

// menuShape writes a menu as one line: "[Header]", "—" for separators,
// "Label ▸ value" for submenus, "Label ✓" for checked radios.
func menuShape(items []MenuItem) string {
	var out []string
	for _, it := range items {
		switch {
		case it.Divider:
			out = append(out, "—")
		case it.Header:
			out = append(out, "["+it.Label+"]")
		default:
			s := it.Label
			if it.Sub != nil {
				s += " ▸"
				if it.Right != "" {
					s += " " + it.Right
				}
			}
			if it.Checked {
				s += " ✓"
			}
			if it.Disabled {
				s += " (disabled)"
			}
			out = append(out, s)
		}
	}
	return strings.Join(out, " | ")
}

// menuFind follows labels through submenus.
func menuFind(t *testing.T, items []MenuItem, path ...string) MenuItem {
	t.Helper()
	for i, p := range path {
		found := false
		for _, it := range items {
			if it.Label == p && !it.Header {
				if i == len(path)-1 {
					return it
				}
				items, found = it.Sub, true
				break
			}
		}
		if !found {
			t.Fatalf("no %q (path %v) in %s", p, path, menuShape(items))
		}
	}
	t.Fatalf("empty path")
	return MenuItem{}
}

func sectionEnv(t *testing.T) (*perfEnv, *fakeslack.Server) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	e.settle(300 * time.Millisecond)
	return e, fs
}

func sectionOfType(a *App, typ string) *sbSection {
	for i := range a.model().Sections {
		if s := &a.model().Sections[i]; s.Type == typ {
			return s
		}
	}
	return nil
}

// Every section type's ⋯ and right-click menus, item for item, as Slack's
// templates build them for an account with filter_dormant_channels,
// sidebar_section_search, add_channels_channel_sections_slackathon,
// mute_channel_edu and dm_recency_sort_rollout (the test account's groups).
func TestSectionMenuTemplates(t *testing.T) {
	e, _ := sectionEnv(t)
	a := e.a
	mark := func(sec *sbSection) string {
		if len(a.sectionUnread(sec)) > 0 {
			return "Mark all as read | "
		}
		return ""
	}
	for _, c := range []struct {
		typ, ellipsis, context, manage string
	}{
		{"channels",
			"[Channels] | %mark%Create a channel | Browse channels | — | [Manage section] | Create a new section | — | [Section settings] | Filter ▸ All | Sort ▸ A-Z",
			"[Channels] | %mark%Create a channel | Browse channels | Manage section ▸",
			"Create a new section | — | [Section settings] | Filter ▸ All | Sort ▸ A-Z"},
		{"direct_messages",
			"[Direct messages] | %mark%Start direct message | Recent direct messages | — | [Manage section] | Create a new section | — | [Section settings] | Filter ▸ All | Sort ▸ Recency",
			"[Direct messages] | %mark%Start direct message | Manage section ▸",
			"Create a new section | — | [Section settings] | Manage VIPs | Filter ▸ All | Sort ▸ Recency"},
		{"standard",
			"[%name%] | Search in this section | %mark%Create a channel | Share ▸ | — | [Manage section] | Rename | Create a new section | Manage conversations | — | [Section settings] | Filter ▸ All | Sort ▸ A-Z | — | [Notifications] | Mute all conversations | — | Delete section",
			"[%name%] | %mark%Create a channel | Share ▸ | Manage section ▸",
			"Rename | Create a new section | Manage conversations | — | [Section settings] | Filter ▸ All | Sort ▸ A-Z | — | [Notifications] | Mute all conversations | — | Delete section"},
		{"stars",
			"[Starred] | Search in this section | %mark%Create a channel | — | [Manage section] | Create a new section | Manage conversations | — | [Section settings] | Filter ▸ All | Sort ▸ A-Z | — | Delete section",
			"[Starred] | %mark%Create a channel | Manage section ▸",
			"Create a new section | Manage conversations | — | [Section settings] | Filter ▸ All | Sort ▸ A-Z | — | Delete section"},
	} {
		sec := sectionOfType(a, c.typ)
		if sec == nil {
			t.Errorf("no %s section", c.typ)
			continue
		}
		fill := func(s string) string {
			return strings.NewReplacer("%mark%", mark(sec), "%name%", sec.Name).Replace(s)
		}
		if got := menuShape(a.sectionMenuItems(sec)); got != fill(c.ellipsis) {
			t.Errorf("%s ⋯ menu:\n got %s\nwant %s", c.typ, got, fill(c.ellipsis))
		}
		ctx := a.sectionContextItems(sec)
		top := ctx
		if i := slices.IndexFunc(ctx, func(it MenuItem) bool { return it.Label == "Manage section" }); i >= 0 {
			top = ctx[:i+1]
		}
		if got := menuShape(top); got != fill(c.context) {
			t.Errorf("%s right-click:\n got %s\nwant %s", c.typ, got, fill(c.context))
		}
		if got := menuShape(menuFind(t, ctx, "Manage section").Sub); got != c.manage {
			t.Errorf("%s Manage section ▸:\n got %s\nwant %s", c.typ, got, c.manage)
		}
	}

	custom := sectionOfType(a, "standard")
	if got := menuShape(menuFind(t, a.sectionMenuItems(custom), "Filter").Sub); got != "Active only | Unreads | Mentions | All ✓" {
		t.Errorf("Filter ▸ %s", got)
	}
	if it := menuFind(t, a.sectionMenuItems(custom), "Filter", "Active only"); it.Subtitle != "New activity within the last 30 days" {
		t.Errorf("Active only description %q", it.Subtitle)
	}
	if got := menuShape(menuFind(t, a.sectionMenuItems(custom), "Sort").Sub); got != "A–Z ✓ | Recency | Priority" {
		t.Errorf("Sort ▸ %s", got)
	}
	if got := menuShape(menuFind(t, a.sectionMenuItems(custom), "Share").Sub); got != "Share directly | Copy section link" {
		t.Errorf("Share ▸ %s", got)
	}
	dms := sectionOfType(a, "direct_messages")
	if got := menuShape(menuFind(t, a.sectionMenuItems(dms), "Filter").Sub); got != "Active only | Unreads | All ✓" {
		t.Errorf("DM Filter ▸ %s (no Mentions for DMs)", got)
	}
	if got := menuShape(menuFind(t, a.sectionMenuItems(dms), "Sort").Sub); got != "A–Z | Recency ✓ | Priority" {
		t.Errorf("DM Sort ▸ %s", got)
	}

	// without filter_dormant_channels: Slack's older labels
	a.Core().SetExperimentGroup("filter_dormant_channels", "off")
	if got := menuShape(menuFind(t, a.sectionMenuItems(custom), "Filter").Sub); got != "All ✓ | Unreads only | Mentions only" {
		t.Errorf("Filter ▸ (no dormant) %s", got)
	}
	if got := menuShape(menuFind(t, a.sectionMenuItems(dms), "Sort").Sub); got != "Alphabetically | By most recent (Default) ✓ | Priority" {
		t.Errorf("DM Sort ▸ (no dormant) %s", got)
	}
	a.Core().SetExperimentGroup("filter_dormant_channels", "on")

	// the rail's Create menu
	var got []MenuItem
	oldAt := mkMenuAt
	t.Cleanup(func() { mkMenuAt = oldAt })
	mkMenuAt = func(a *App, anchor image.Rectangle, side int, width float32, items []MenuItem) Overlay {
		got = items
		return nil
	}
	a.openCreateMenu(image.Rect(0, 0, 36, 36))
	if s := menuShape(got); s != "[Create] | Message | Channel | Huddle | Canvas | List | Workflow | — | Invite people" {
		t.Errorf("Create menu: %s", s)
	}
	for _, it := range got {
		if !it.Header && !it.Divider && it.Do == nil {
			t.Errorf("Create menu row %q does nothing", it.Label)
		}
	}
	if c := menuFind(t, got, "Canvas"); c.Hint != "Ctrl+Shift+N" || c.Subtitle != "Create and share content" {
		t.Errorf("Canvas row %+v", c)
	}
	a.Core().SetExperimentGroup("meeting_assistant", "on")
	a.openCreateMenu(image.Rect(0, 0, 36, 36))
	if s := menuShape(got); !strings.Contains(s, "Workflow | Notes | —") {
		t.Errorf("Create menu with meeting_assistant: %s", s)
	}
}

func channelSectionsPrefUI(t *testing.T, fs *fakeslack.Server) map[string]map[string]any {
	t.Helper()
	v, _ := fs.Pref("channel_sections")
	s, _ := v.(string)
	out := map[string]map[string]any{}
	if s != "" {
		_ = json.Unmarshal([]byte(s), &out)
	}
	return out
}

// Every item does what Slack's does, against the fake.
func TestSectionMenuActions(t *testing.T) {
	e, fs := sectionEnv(t)
	a := e.a
	custom := func() *sbSection { return a.sectionByID("L0FAKE02") }
	do := func(sec *sbSection, ctxMenu bool, path ...string) {
		t.Helper()
		items := a.sectionMenuItems(sec)
		if ctxMenu {
			items = a.sectionContextItems(sec)
		}
		it := menuFind(t, items, path...)
		if it.Do == nil || it.Disabled {
			t.Fatalf("%v: not clickable", path)
		}
		it.Do()
		e.frame()
	}

	// Sort ▸ Recency: channel_sections {sort: recent}; the ⋯ now reads Recency
	do(custom(), false, "Sort", "Recency")
	e.until("sort saved", 10*time.Second, func() bool {
		return channelSectionsPrefUI(t, fs)["L0FAKE02"]["sort"] == "recent" && custom().Sort == "recent"
	})
	if it := menuFind(t, a.sectionMenuItems(custom()), "Sort"); it.Right != "Recency" {
		t.Errorf("Sort value %q", it.Right)
	}

	// Filter ▸ Unreads: Custom by section, read rows leave the sidebar and
	// the right-click menu lists them
	do(custom(), true, "Manage section", "Filter", "Unreads")
	e.until("filter saved", 10*time.Second, func() bool {
		v, _ := fs.Pref("sidebar_behavior")
		return v == "hide_read_channels_unless_starred" && custom().Filter == "hid"
	})
	open := a.router.Current().Channel() // the open conversation always keeps its row
	for _, id := range custom().Rows {
		if c := a.model().conv(id); id != open && !c.Unread && c.Mentions == 0 {
			t.Errorf("read %s still in the sidebar", id)
		}
	}
	if len(custom().HiddenRead) > 0 && !strings.Contains(menuShape(a.sectionContextItems(custom())), "[Read conversations]") {
		t.Errorf("no Read conversations in %s", menuShape(a.sectionContextItems(custom())))
	}
	do(custom(), false, "Filter", "All")
	e.until("filter All", 10*time.Second, func() bool { return custom().Filter == "all" })

	// Mute all → Muted N channels. (Undo) → Unmute all
	ids := append([]string(nil), custom().All...)
	do(custom(), false, "Mute all conversations")
	e.until("muted", 10*time.Second, func() bool {
		for _, id := range ids {
			if c := a.model().conv(id); c == nil || !c.Muted {
				return false
			}
		}
		return true
	})
	if want := "Muted " + plural(len(ids), "channel", "channels") + "."; a.toast.text != want || a.toast.undo == nil {
		t.Errorf("toast %q (undo %v), want %q", a.toast.text, a.toast.undo != nil, want)
	}
	if !strings.Contains(menuShape(a.sectionMenuItems(custom())), "Unmute all conversations") {
		t.Error("no Unmute all after muting")
	}
	a.undoToast()
	e.until("unmuted by Undo", 10*time.Second, func() bool {
		c := a.model().conv(ids[0])
		return c != nil && !c.Muted
	})

	// Mark all as read, then Undo
	var withUnread *sbSection
	for i := range a.model().Sections {
		if s := &a.model().Sections[i]; len(a.sectionUnread(s)) > 0 && s.Type != "direct_messages" {
			withUnread = s
			break
		}
	}
	if withUnread == nil {
		t.Fatal("no section with unread channels")
	}
	secID, unread := withUnread.ID, a.sectionUnread(withUnread)
	do(withUnread, false, "Mark all as read")
	e.until("marked", 10*time.Second, func() bool { return len(a.sectionUnread(a.sectionByID(secID))) == 0 })
	if !strings.HasPrefix(a.toast.text, "Marked ") || !strings.HasSuffix(a.toast.text, " as read.") {
		t.Errorf("toast %q", a.toast.text)
	}
	for _, id := range unread {
		if fs.Marked(id) == "" {
			t.Errorf("no conversations.mark for %s", id)
		}
	}
	a.undoToast()
	e.until("unread again", 10*time.Second, func() bool { return len(a.sectionUnread(a.sectionByID(secID))) == len(unread) })

	// Rename (validation, then Save) with Undo
	do(custom(), false, "Rename")
	rn, ok := topOverlay[*popups.RenameSectionModal](a)
	if !ok {
		t.Fatal("no rename modal")
	}
	rn.SetName("starred")
	if rn.Error() != "You already have a section with this name." || rn.CanSave() {
		t.Errorf("taken name: %q, can save %v", rn.Error(), rn.CanSave())
	}
	rn.SetName("")
	if rn.CanSave() || rn.Error() != "" {
		t.Error("empty name can save")
	}
	rn.SetName("Liftoff")
	rn.SetEmoji("rocket")
	rn.Save()
	e.until("renamed", 10*time.Second, func() bool { return custom().Name == "Liftoff" })
	if a.toast.text != "Renamed to Liftoff." {
		t.Errorf("toast %q", a.toast.text)
	}
	if s := fs.Sections(); s[slices.IndexFunc(s, func(x fakeslack.SectionCall) bool { return x.ID == "L0FAKE02" })].Emoji != ":rocket:" {
		t.Error("emoji not saved")
	}
	a.undoToast()
	e.until("rename undone", 10*time.Second, func() bool { return custom().Name == "Projects" })

	// Create a new section (before Projects), sort Priority, add a channel
	// that sits in another section (speed bump), then manage and delete it
	do(custom(), false, "Create a new section")
	cm, ok := topOverlay[*popups.CreateSectionModal](a)
	if !ok || cm.Step() != "name" || cm.Title() != "Create a section" {
		t.Fatal("no create-section modal on its name step")
	}
	cm.SetName("Projects")
	if cm.CanCreate() {
		t.Error("a taken name can be created")
	}
	cm.SetName("Launch")
	cm.SetSort("priority")
	cm.Create()
	e.until("section created", 10*time.Second, func() bool { return cm.Step() == "conversations" })
	if cm.Title() != "Add conversations" {
		t.Errorf("step 2 title %q", cm.Title())
	}
	var launch string
	e.until("Launch in the sidebar", 10*time.Second, func() bool {
		for i, s := range a.model().Sections {
			if s.Name == "Launch" {
				launch = s.ID
				if next := a.model().Sections[i+1].ID; next != "L0FAKE02" {
					t.Errorf("Launch sits before %s, want Projects", next)
				}
			}
		}
		return launch != ""
	})
	if channelSectionsPrefUI(t, fs)[launch]["sort"] != "priority" {
		t.Errorf("Launch sort pref %v", channelSectionsPrefUI(t, fs)[launch])
	}
	moved := a.sectionByID("L0FAKE03").All[0]
	cm.Toggle(moved)
	cm.Submit()
	if !cm.Bumped() {
		t.Fatal("no speed bump for a conversation from another section")
	}
	cm.Submit() // Move Conversations
	e.until("moved", 10*time.Second, func() bool {
		s := a.sectionByID(launch)
		return s != nil && slices.Contains(s.All, moved) && !slices.Contains(a.sectionByID("L0FAKE03").All, moved)
	})

	do(a.sectionByID(launch), false, "Manage conversations")
	mc, ok := topOverlay[*popups.CreateSectionModal](a)
	if !ok || mc.Title() != "Manage conversations" || mc.Step() != "conversations" {
		t.Fatal("no Manage conversations modal")
	}
	mc.Toggle(moved)
	mc.Submit()
	e.until("removed", 10*time.Second, func() bool { return len(a.sectionByID(launch).All) == 0 })

	// Delete (empty: no confirmation) with Undo
	do(a.sectionByID(launch), false, "Delete section")
	e.until("deleted", 10*time.Second, func() bool { return a.sectionByID(launch) == nil })
	if a.toast.text != "Deleted Launch." || a.toast.undo == nil {
		t.Errorf("toast %q", a.toast.text)
	}
	a.undoToast()
	e.until("delete undone", 10*time.Second, func() bool {
		for _, s := range a.model().Sections {
			if s.Name == "Launch" {
				return true
			}
		}
		return false
	})

	// Deleting a section with conversations asks first
	do(a.sectionByID("L0FAKE04"), false, "Delete section")
	if _, ok := topOverlay[*popups.Modal](a); !ok {
		t.Fatal("no Delete this section? modal")
	}

	// Search in this section → search with the section's channels
	var q string
	oldSearch := mkSearch
	t.Cleanup(func() { mkSearch = oldSearch })
	mkSearch = func(a *App, s string) Overlay { q = s; return nil }
	do(custom(), false, "Search in this section")
	if !strings.HasPrefix(q, "in:<#"+custom().All[0]+">") {
		t.Errorf("search query %q", q)
	}

	// Share ▸ Copy section link
	do(custom(), false, "Share", "Copy section link")
	var copied string
	e.until("link copied", 10*time.Second, func() bool {
		if _, b, ok := e.r.WriteClipboard(); ok {
			copied = string(b)
		}
		return a.toast.text == "Section link copied" && copied != ""
	})
	if !strings.Contains(copied, "/section/") || fs.SectionLinks()[0].Get("section_name") != "Projects" {
		t.Errorf("clipboard %q", copied)
	}

	// Create menu: Canvas makes a standalone canvas; Huddle picks a conversation
	var created []MenuItem
	oldAt := mkMenuAt
	t.Cleanup(func() { mkMenuAt = oldAt })
	mkMenuAt = func(a *App, anchor image.Rectangle, side int, width float32, items []MenuItem) Overlay {
		created = items
		return nil
	}
	a.openCreateMenu(image.Rect(0, 0, 36, 36))
	n := fs.CallCount("canvases.create")
	menuFind(t, created, "Canvas").Do()
	e.until("canvas created", 10*time.Second, func() bool { return fs.CallCount("canvases.create") > n })
	menuFind(t, created, "Huddle").Do()
	hm, ok := topOverlay[*popups.HuddleStartModal](a)
	if !ok || len(hm.Matches()) == 0 {
		t.Fatal("no Start a Huddle picker")
	}
	menuFind(t, created, "List").Do()
	if a.toast.text != "Lists aren’t available in "+AppName+" yet" {
		t.Errorf("List toast %q", a.toast.text)
	}
}

// With SECTION_MENU_SHOTS_DIR set (the section-menus workflow, under xvfb)
// the menus and modals are rendered and saved.
func TestSectionMenuShots(t *testing.T) {
	dir := os.Getenv("SECTION_MENU_SHOTS_DIR")
	if dir == "" {
		t.Skip("SECTION_MENU_SHOTS_DIR not set")
	}
	t.Setenv("CLACK_BENCH_GPU", "1")
	e, _ := sectionEnv(t)
	a := e.a
	closeAll := func() {
		for _, o := range a.overlays {
			if d, ok := o.(dismisser); ok {
				d.Dismiss()
			}
		}
		a.overlays = nil
		e.frame()
	}
	shot := func(name string) {
		e.frame()
		e.frame()
		e.shotTo(t, dir, name)
	}
	menu := func(name string, open func(), subs ...string) {
		closeAll()
		open()
		e.frame()
		if m, ok := topOverlay[*popups.Menu](a); ok {
			for _, s := range subs {
				if !m.OpenSubmenu(s) {
					t.Errorf("%s: no submenu %q", name, s)
				}
				e.frame()
			}
		} else {
			t.Errorf("%s: no menu", name)
		}
		shot(name)
	}
	at := image.Pt(230, 300)
	menu("1-custom-ellipsis", func() { a.openSectionMenu("L0FAKE02", at) })
	menu("2-custom-sort", func() { a.openSectionMenu("L0FAKE02", at) }, "Sort")
	menu("3-custom-filter", func() { a.openSectionMenu("L0FAKE02", at) }, "Filter")
	menu("4-custom-share", func() { a.openSectionMenu("L0FAKE02", at) }, "Share")
	menu("5-dms-ellipsis", func() { a.openSectionMenu(sectionOfType(a, "direct_messages").ID, at) })
	menu("6-channels-ellipsis", func() { a.openSectionMenu(sectionOfType(a, "channels").ID, at) })
	menu("7-starred-ellipsis", func() { a.openSectionMenu("L0FAKE00", at) })
	menu("8-dms-context-manage", func() { a.openSectionContextMenu(sectionOfType(a, "direct_messages").ID, at) }, "Manage section")
	menu("9-custom-context-manage-sort", func() { a.openSectionContextMenu("L0FAKE02", at) }, "Manage section", "Sort")
	menu("10-create-menu", func() { a.openCreateMenu(image.Rect(8, 120, 44, 156)) })

	closeAll()
	a.openRenameSection("L0FAKE02", "Projects", "")
	shot("11-rename")
	if m, ok := topOverlay[*popups.RenameSectionModal](a); ok {
		m.SetName("Starred")
	}
	shot("12-rename-taken")
	closeAll()
	a.openCreateSection("L0FAKE02", "")
	if m, ok := topOverlay[*popups.CreateSectionModal](a); ok {
		m.SetName("Launch")
		shot("13-create-section-name")
		m.Create()
		e.until("step 2", 10*time.Second, func() bool { return m.Step() == "conversations" })
		shot("14-create-section-conversations")
	}
	closeAll()
	a.ShowOverlay(popups.NewDeleteSectionModal(a, "Projects", func() {}))
	shot("15-delete-confirm")
	closeAll()
	a.openHuddleStart()
	shot("16-start-huddle")
	closeAll()
	a.ToastUndo("Muted 4 channels.", func() {})
	shot("17-toast-undo")
}
