package shortcut

import (
	"strings"
	"testing"
)

func baseOpts(goos string) ListOpts {
	return ListOpts{GOOS: goos, KeyboardLocale: "us", TabRail: []string{"home", "dms", "activity-inbox", "later"},
		TabLabels:     map[string]string{"home": "Home", "dms": "DMs", "activity-inbox": "Activity", "later": "To-dos"},
		ActivityLabel: "Activity", ActivityTab: func(t string) bool { return t == "activity-inbox" },
		Huddles: true, CanvasAccess: true, CanvasCreate: true, UndoSend: true, PrivateForward: true,
		ActivityInbox: true, LaterTodos: true}
}

func titles(secs []Section) string {
	var t []string
	for _, s := range secs {
		t = append(t, s.Title)
	}
	return strings.Join(t, "|")
}

func find(secs []Section, title string) (Shortcut, bool) {
	for _, s := range secs {
		for _, r := range s.Shortcuts {
			if r.Title == title {
				return r, true
			}
		}
	}
	return Shortcut{}, false
}

// shortcutsData's sections and rows in the desktop app on macOS.
func TestSectionsMac(t *testing.T) {
	secs := Sections(baseOpts("darwin"))
	want := "Basics|Sidebar|Navigation|Screen readers|Messaging|Mark Messages Read and Unread|Channels & Direct Messages|" +
		"All Unreads|Activity|Files and Snippets|Canvas Editing|Workspace Switching|Tab Switching|When focused on a message|Channel Sections|Huddles"
	if got := titles(secs); got != want {
		t.Errorf("sections\n%s\nwant\n%s", got, want)
	}
	check := func(title, keys string) {
		t.Helper()
		r, ok := find(secs, title)
		if !ok {
			t.Errorf("no %q", title)
			return
		}
		if got := strings.Join(r.Keys, "|"); got != keys {
			t.Errorf("%s: %s, want %s", title, got, keys)
		}
	}
	check("New message", "key_Cmd|key_N|  or  |key_Shift|key_Cmd|key_K")
	check("Jump to…", "key_Cmd|key_K|  or  |key_T")
	check("Back in history", "key_Cmd|key_[") // the ⌘← row is for other keyboards
	check("Edit last message (in empty text input)", "key_↑|  or  |key_Cmd|key_↑")
	check("Move focus to the next section", "key_F6|  or  |key_Ctrl|key_Cmd|key_→")
	check("Preferences", "key_Cmd|key_,")
	check("Mark a message as unread", "key_Option|  and click message")
	check("Toggle huddles start / stop", "key_Shift|key_Cmd|key_H")
	check("Forward", "key_F")
	check("Add / complete a to-do", "key_A")
	// the rail's first nine tabs (Ctrl+N on a Mac), the Activity tab with ⇧⌘M too
	check("DMs", "key_Ctrl|key_2")
	check("Activity", "key_Ctrl|key_3|  or  |key_Shift|key_Cmd|key_M")
	if r, _ := find(secs, "DMs"); r.Tab != "dms" {
		t.Errorf("DMs row tab %q", r.Tab)
	}
	if _, ok := find(secs, "Save / remove from Later"); ok {
		t.Error("Save / remove from Later shown with later_todos")
	}
	if r, _ := find(secs, "Jump to…"); r.Info != "Jump to another conversation by just typing its name" || r.ID != "quick_switcher" {
		t.Errorf("Jump to… %+v", r)
	}
	if n := len(secs[0].Shortcuts); secs[0].Shortcuts[n-1].Title != "Open Help" {
		t.Errorf("Basics ends with %q", secs[0].Shortcuts[n-1].Title)
	}
}

// Off macOS: keysLinux / keysWin, Cmd → Ctrl, Option → Alt, and the rows
// blocked on Windows and Linux (Preferences, the ⌘← history keys) gone.
func TestSectionsLinuxWindows(t *testing.T) {
	for _, goos := range []string{"linux", "windows"} {
		secs := Sections(baseOpts(goos))
		r, _ := find(secs, "Back in history")
		if strings.Join(r.Keys, "|") != "key_Alt|key_←" {
			t.Errorf("%s: Back in history %v", goos, r.Keys)
		}
		if _, ok := find(secs, "Preferences"); ok {
			t.Errorf("%s: Preferences listed", goos)
		}
		r, _ = find(secs, "Jump to…")
		if strings.Join(r.Keys, "|") != "key_Ctrl|key_K|  or  |key_T" {
			t.Errorf("%s: Jump to… %v", goos, r.Keys)
		}
		r, _ = find(secs, "Activity")
		if strings.Join(r.Keys, "|") != "key_Ctrl|key_Shift|key_3|  or  |key_Shift|key_Ctrl|key_M" {
			t.Errorf("%s: the Activity tab %v", goos, r.Keys)
		}
		r, _ = find(secs, "Previous channel or DM")
		if strings.Join(r.Keys, "|") != "key_Alt|key_↑" {
			t.Errorf("%s: Previous channel or DM %v", goos, r.Keys)
		}
	}
	r, _ := find(Sections(baseOpts("windows")), "Toggle huddles start / stop")
	if strings.Join(r.Keys, "|") != "key_Ctrl|key_Shift|key_H" {
		t.Errorf("windows huddle keys %v", r.Keys)
	}
}

// The rows and sections the flags switch.
func TestSectionsFlags(t *testing.T) {
	o := baseOpts("darwin")
	o.ActivityInbox, o.Huddles, o.CanvasAccess, o.LaterTodos, o.PrivateForward, o.UndoSend, o.CanvasCreate = false, false, false, false, false, false, false
	o.SubmitForward, o.UpToEdit = true, true
	o.TabRail = []string{"home", "dms"}
	secs := Sections(o)
	for _, s := range []string{"Activity", "Huddles", "Canvas Editing"} {
		if strings.Contains("|"+titles(secs)+"|", "|"+s+"|") {
			t.Errorf("%s listed", s)
		}
	}
	for title, want := range map[string]bool{"Save / remove from Later": true, "Add / complete a to-do": false, "Share": true,
		"Forward": false, "Unsend last message (in empty text input)": false, "New canvas": false, "Submit forward dialog": true} {
		if _, ok := find(secs, title); ok != want {
			t.Errorf("%q listed %v, want %v", title, ok, want)
		}
	}
	r, _ := find(secs, "Edit last message (in empty text input)")
	if strings.Join(r.Keys, "|") != "key_Cmd|key_↑" {
		t.Errorf("↑ edits: %v", r.Keys)
	}
	// no Activity tab in the rail: ⇧⌘M's own row, which runs open_activity
	r, ok := find(secs, "Activity")
	if !ok || r.ID != "open_activity" || strings.Join(r.Keys, "|") != "key_Shift|key_Cmd|key_M" {
		t.Errorf("Activity row %+v", r)
	}
}

// filterSections and countShortcuts.
func TestFilterSections(t *testing.T) {
	secs := Sections(baseOpts("darwin"))
	f := FilterSections(secs, "THREAD")
	if CountShortcuts(f) == 0 {
		t.Fatal("no thread rows")
	}
	for _, s := range f {
		for _, r := range s.Shortcuts {
			if !strings.Contains(strings.ToLower(r.Title), "thread") {
				t.Errorf("%q matched thread", r.Title)
			}
		}
	}
	if len(FilterSections(secs, "zzzz")) != 0 {
		t.Error("nonsense matched")
	}
	if CountShortcuts(secs) < 100 {
		t.Errorf("%d rows", CountShortcuts(secs))
	}
}
