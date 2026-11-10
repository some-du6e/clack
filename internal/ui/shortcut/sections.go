package shortcut

import "strings"

// The Keyboard Shortcuts pane's list: Slack's shortcutsData (boot-shared
// vLSD qu, build 4d0c78085232), as the desktop app shows it. Keys are
// Slack's own strings: "key_<name>" is a KeyboardKey chip, anything else
// ("  or  ", "  and click message") is plain text between the chips.
//
// Kept as Slack builds it: the sections and rows in its order, the rows a
// flag turns on or off, the filter (shortcutFilter: isSsbOnly rows stay in
// the desktop app; isDisabled, browserBlockList against the OS and
// localeBlockList against the keyboard locale drop rows), then
// replaceModifierKeysForOS (keysWin on Windows, keysLinux on Linux, else
// Cmd → Ctrl and Option → Alt off macOS, Strg on a German keyboard), and
// sections left empty dropped (Basics stays).

// Section is one heading of the list.
type Section struct {
	Title     string
	ID        string
	Shortcuts []Shortcut
}

// Shortcut is one row.
type Shortcut struct {
	Title string
	Keys  []string
	// ID is the row's executeKeyboardShortcut id ("new_message",
	// "open_preferences", …) and Tab its rail tab: such rows' titles are
	// buttons that run the shortcut.
	ID, Tab string
	// Info is the ⓘ tooltip after the title (hasInfoButton).
	Info string

	keysWin, keysLinux []string
	ssbOnly, disabled  bool
	browserBlock       []string
	localeBlock        []string
}

// ListOpts are what shortcutsData reads from the app.
type ListOpts struct {
	GOOS           string // "darwin", "windows", "linux"
	KeyboardLocale string // "us" (Clack can't read the layout)
	// the first nine rail tabs with their labels (G1Mx tabRailList,
	// kFfw label), and the Activity tab's label
	TabRail       []string
	TabLabels     map[string]string
	ActivityLabel string
	ActivityTab   func(tab string) bool // wrZz isActivityTab

	Huddles        bool // isHuddlesEnabledOrHasActiveHuddle
	CanvasAccess   bool // canAccessCanvas
	CanvasCreate   bool // canCreateCanvas
	UndoSend       bool // canUndoSendMessages
	UpToEdit       bool // composerUpArrowShortcutBehavior 3: ↑ edits
	PrivateForward bool // the private_message_forwarding team pref
	ActivityInbox  bool // activity_inbox (5Ns/ Lh)
	SubmitForward  bool // submit_forward_keybinding (+o0o j)
	LaterTodos     bool // later_todos (6owI tj)
}

// Sections is shortcutsData for o.
func Sections(o ListOpts) []Section {
	mac := o.GOOS == "darwin"
	or := "  or  "
	x := true // isDesktopApp
	S := mac
	T := []string{"key_Ctrl", "key_Shift"}
	if S {
		T = []string{"key_Ctrl"}
	}
	I := x // isDesktopApp || not Chrome
	var j []string
	if I {
		j = []string{or, "key_Shift", "key_Cmd", "key_M"}
	}
	rail := o.TabRail
	if len(rail) > 9 {
		rail = rail[:9]
	}
	activityTab := func(t string) bool { return o.ActivityTab != nil && o.ActivityTab(t) }
	var E []Shortcut
	hasActivity := false
	for i, t := range rail {
		keys := append(append([]string{}, T...), "key_"+itoa1(i+1))
		if activityTab(t) {
			keys = append(keys, j...)
			hasActivity = true
		}
		E = append(E, Shortcut{Title: o.TabLabels[t], Keys: keys, Tab: t})
	}
	if I && !hasActivity {
		E = append(E, Shortcut{Title: o.ActivityLabel, Keys: []string{"key_Shift", "key_Cmd", "key_M"}, ID: "open_activity"})
	}
	newMessage := []string{"key_Shift", "key_Cmd", "key_K"}
	if x {
		newMessage = []string{"key_Cmd", "key_N", or, "key_Shift", "key_Cmd", "key_K"}
	}
	jump := []string{"key_Cmd", "key_K"}
	if x {
		jump = []string{"key_Cmd", "key_K", or, "key_T"}
	}
	edit := []string{"key_↑", or, "key_Cmd", "key_↑"}
	if o.UpToEdit {
		edit = []string{"key_Cmd", "key_↑"}
	}
	altLeft, altRight := []string{"key_Alt", "key_←"}, []string{"key_Alt", "key_→"}
	historyLocales1 := []string{"de", "fr", "it", "zh-pinyin", "zh-cangjie-zhuyin", "ru"}
	historyLocales2 := []string{"us", "gb", "es", "ja", "br", "br-ABNT", "ko"}
	nextSection := []string{"key_Cmd", "key_F6"}
	prevSection := []string{"key_Shift", "key_Cmd", "key_F6"}
	if x {
		nextSection, prevSection = []string{"key_F6"}, []string{"key_Shift", "key_F6"}
	}
	if S {
		nextSection = append(nextSection, or, "key_Ctrl", "key_Cmd", "key_→")
		prevSection = append(prevSection, or, "key_Ctrl", "key_Cmd", "key_←")
	}
	playback := []string{"key_Alt"}
	summary := []string{"key_Ctrl", "key_Alt", "key_S"}
	if S {
		playback = []string{"key_Cmd", "key_Option"}
		summary = []string{"key_Cmd", "key_Option", "key_S"}
	}
	playback = append(playback, ",  ", "key_0", "  -  ", "key_9")
	kb := func(c string) string { return "key_" + keyForLocale(c, o.KeyboardLocale) }

	M := []Section{
		{Title: "Basics", ID: "basics", Shortcuts: []Shortcut{
			{Title: "New message", Keys: newMessage, ID: "new_message"},
			{Title: "New canvas", Keys: []string{"key_Cmd", "key_Shift", "key_N"}, ID: "new_canvas", ssbOnly: true, disabled: !o.CanvasCreate},
			{Title: "Jump to…", Keys: jump, Info: "Jump to another conversation by just typing its name", ID: "quick_switcher"},
			{Title: "Search", Keys: []string{"key_Cmd", "key_G"}, ssbOnly: true},
			{Title: "Search current conversation", Keys: []string{"key_Cmd", "key_F"}, ssbOnly: true},
			{Title: "Edit last message (in empty text input)", Keys: edit},
			{Title: "Unsend last message (in empty text input)", Keys: []string{"key_Cmd", "key_Z"}, disabled: !o.UndoSend},
			{Title: "Back in history", Keys: []string{"key_Cmd", "key_["}, keysWin: altLeft, keysLinux: altLeft, localeBlock: historyLocales1},
			{Title: "Forward in history", Keys: []string{"key_Cmd", "key_]"}, keysWin: altRight, keysLinux: altRight, localeBlock: historyLocales1},
			{Title: "Back in history", Keys: []string{"key_Cmd", "key_←"}, keysWin: altLeft, keysLinux: altLeft, localeBlock: historyLocales2, browserBlock: []string{"windows", "linux"}},
			{Title: "Forward in history", Keys: []string{"key_Cmd", "key_→"}, keysWin: altRight, keysLinux: altRight, localeBlock: historyLocales2, browserBlock: []string{"windows", "linux"}},
			{Title: "Threads", Keys: []string{"key_Shift", "key_Cmd", "key_T"}, ID: "open_all_threads", ssbOnly: true},
			{Title: "Dismiss dialogs", Keys: []string{"key_Esc"}},
		}},
		{Title: "Sidebar", ID: "sidebar", Shortcuts: []Shortcut{
			{Title: "Show / hide", Keys: []string{"key_Shift", "key_Cmd", "key_D"}, ssbOnly: true},
			{Title: "Resize (with focus on the resizer)", Keys: []string{"key_←", or, "key_→"}},
		}},
		{Title: "Navigation", ID: "navigation", Shortcuts: append(E,
			Shortcut{Title: "Open More menu", Keys: append(append([]string{}, T...), "key_0")},
			Shortcut{Title: "All Unreads", Keys: []string{"key_Cmd", "key_Shift", "key_A"}, ID: "open_all_unreads"},
			Shortcut{Title: "Move focus through messages", Keys: []string{"key_↑", or, "key_↓"}},
			Shortcut{Title: "Move focus to the next section", Keys: nextSection},
			Shortcut{Title: "Move focus to the previous section", Keys: prevSection},
			Shortcut{Title: `Take action or "click" on selected element`, Keys: []string{"key_Enter"}},
			Shortcut{Title: "Increase text size", Keys: []string{"key_Cmd", "key_+"}},
			Shortcut{Title: "Decrease text size", Keys: []string{"key_Cmd", "key_-"}},
			Shortcut{Title: "Scroll up", Keys: []string{"key_↑", or, "key_Page Up"}},
			Shortcut{Title: "Scroll down", Keys: []string{"key_↓", or, "key_Page Down"}},
			Shortcut{Title: "Scroll to previous day", Keys: []string{"key_Shift", "key_Page Up"}},
			Shortcut{Title: "Scroll to next day", Keys: []string{"key_Shift", "key_Page Down"}},
			Shortcut{Title: "Preferences", Keys: []string{"key_Cmd", kb(",")}, ID: "open_preferences", ssbOnly: true, browserBlock: []string{"windows", "linux"}},
			Shortcut{Title: "Close or re-open right pane", Keys: []string{"key_Cmd", kb(".")}},
			Shortcut{Title: "Channel Info", Keys: []string{"key_Shift", "key_Cmd", "key_I"}, ssbOnly: true},
			Shortcut{Title: "Browse channels", Keys: []string{"key_Shift", "key_Cmd", "key_L"}, ID: "browse_channels"},
			Shortcut{Title: "Directory", Keys: []string{"key_Shift", "key_Cmd", "key_E"}, browserBlock: []string{"firefox"}},
			Shortcut{Title: "Toggle full screen", Keys: []string{"key_Ctrl", "key_Cmd", "key_F"}, keysWin: []string{"key_F11"}, keysLinux: []string{"key_F11"}},
		)},
		{Title: "Screen readers", ID: "screen_readers", Shortcuts: []Shortcut{
			{Title: "Play back a number of recent messages", Keys: playback},
			{Title: "Play summary of notifications across workspaces", Keys: summary},
		}},
	}
	messaging := []Shortcut{
		{Title: "Select text to beginning of current line", Keys: []string{"key_Shift", "key_↑"}},
		{Title: "Select text to end of current line", Keys: []string{"key_Shift", "key_↓"}},
		{Title: "Start a new line", Keys: []string{"key_Shift", "key_Enter"}},
	}
	if o.SubmitForward {
		messaging = append(messaging, Shortcut{Title: "Submit forward dialog", Keys: []string{"key_Cmd", "key_Enter"}})
	}
	messaging = append(messaging,
		Shortcut{Title: "React to last message (or message with focus)", Keys: []string{"key_Shift", "key_Cmd", kb(`\`)}},
		Shortcut{Title: "Autocomplete @username", Keys: []string{"key_@"}},
		Shortcut{Title: "Autocomplete channel", Keys: []string{"key_#"}},
		Shortcut{Title: "Autocomplete emoji", Keys: []string{"key_:"}},
	)
	M = append(M,
		Section{Title: "Messaging", ID: "messaging", Shortcuts: messaging},
		Section{Title: "Mark Messages Read and Unread", ID: "mark_messages_read_unread", Shortcuts: []Shortcut{
			{Title: "Mark all messages in current channel as read", Keys: []string{"key_Esc"}},
			{Title: "Mark all messages as read", Keys: []string{"key_Shift", "key_Esc"}},
			{Title: "Mark a message as unread", Keys: []string{"key_Option", "  and click message"},
				keysWin: []string{"key_Alt", "  and click message"}, keysLinux: []string{"key_Alt", "  and click message"}},
		}},
		Section{Title: "Channels & Direct Messages", Shortcuts: []Shortcut{
			{Title: "Previous channel or DM", Keys: []string{"key_Option", "key_↑"}},
			{Title: "Next channel or DM", Keys: []string{"key_Option", "key_↓"}},
			{Title: "Previous unread channel or DM", Keys: []string{"key_Option", "key_Shift", "key_↑"}},
			{Title: "Next unread channel or DM", Keys: []string{"key_Option", "key_Shift", "key_↓"}},
		}},
		Section{Title: "All Unreads", ID: "all_unreads", Shortcuts: []Shortcut{
			{Title: "Expand channel", Keys: []string{"key_→"}},
			{Title: "Collapse channel", Keys: []string{"key_←"}},
			{Title: "Mark channel as read", Keys: []string{"key_Esc"}},
			{Title: "Previous channel", Keys: []string{"key_Shift", "key_Page Up"}},
			{Title: "Next channel", Keys: []string{"key_Shift", "key_Page Down"}},
		}},
	)
	if o.ActivityInbox {
		alt := func(k ...string) []string { return append([]string{"key_Alt"}, k...) }
		act := []Shortcut{
			{Title: "Focus on next item", Keys: []string{"key_↓"}},
			{Title: "Focus on previous item", Keys: []string{"key_↑"}},
			{Title: "Select next item", Keys: []string{"key_Option", "key_↓"}, keysWin: alt("key_↓"), keysLinux: alt("key_↓")},
			{Title: "Select previous item", Keys: []string{"key_Option", "key_↑"}, keysWin: alt("key_↑"), keysLinux: alt("key_↑")},
			{Title: "Select next unread item", Keys: []string{"key_Shift", "key_Option", "key_↓"},
				keysWin: []string{"key_Shift", "key_Alt", "key_↓"}, keysLinux: []string{"key_Shift", "key_Alt", "key_↓"}},
			{Title: "Select previous unread item", Keys: []string{"key_Shift", "key_Option", "key_↑"},
				keysWin: []string{"key_Shift", "key_Alt", "key_↑"}, keysLinux: []string{"key_Shift", "key_Alt", "key_↑"}},
			{Title: "Open item details", Keys: []string{"key_→", or, "key_Enter"}},
			{Title: "Focus back on item in list", Keys: []string{"key_←"}},
			{Title: "Close item details", Keys: []string{"key_Cmd", "key_.", or, "key_Esc"},
				keysWin: []string{"key_Alt", "key_.", or, "key_Esc"}, keysLinux: []string{"key_Alt", "key_.", or, "key_Esc"}},
			{Title: "Mark item read", Keys: []string{"key_U"}},
			{Title: "Clear item", Keys: []string{"key_C"}},
			{Title: "Save item", Keys: []string{"key_A"}},
			{Title: "Add reminder", Keys: []string{"key_M"}},
			{Title: "Snooze reminder", Keys: []string{"key_S"}},
			{Title: "Forward message", Keys: []string{"key_F"}},
			{Title: "Select item for bulk updates", Keys: []string{"key_X"}},
			{Title: "Mark all selected as read", Keys: []string{"key_Option", "key_U"}, keysWin: alt("key_U"), keysLinux: alt("key_U"),
				Info: "Available in bulk action mode"},
			{Title: "Clear all selected", Keys: []string{"key_Option", "key_C"}, keysWin: alt("key_C"), keysLinux: alt("key_C"),
				Info: "Available in bulk action mode"},
		}
		M = append(M, Section{Title: "Activity", ID: "activity_v2", Shortcuts: act})
	}
	M = append(M,
		Section{Title: "Files and Snippets", Shortcuts: []Shortcut{
			{Title: "Add a file", Keys: []string{"key_Cmd", "key_O"}, browserBlock: []string{"firefox"}},
			{Title: "Create a snippet", Keys: []string{"key_Shift", "key_Cmd", "key_Enter"}},
			{Title: "View all downloads", Keys: []string{"key_Shift", "key_Cmd", "key_J"}},
		}},
		Section{Title: "Workspace Switching", ID: "workspaces", Shortcuts: []Shortcut{
			{Title: "Next workspace", Keys: []string{"key_Ctrl", "key_Tab"}},
			{Title: "Previous workspace", Keys: []string{"key_Ctrl", "key_Shift", "key_Tab"}},
			{Title: "A specific workspace", Keys: []string{"key_Cmd", "  and 1-9"}},
			{Title: "Open workspace switcher", Keys: []string{"key_Shift", "key_Cmd", "key_S"}, browserBlock: []string{"firefox"}},
		}},
		Section{Title: "Tab Switching", ID: "tabs", Shortcuts: []Shortcut{
			{Title: "Next tab", Keys: []string{"key_Ctrl", "key_PageDown"}},
			{Title: "Previous tab", Keys: []string{"key_Ctrl", "key_PageUp"}},
		}},
	)
	forward := Shortcut{Title: "Share", Keys: []string{"key_S"}}
	if o.PrivateForward {
		forward = Shortcut{Title: "Forward", Keys: []string{"key_F"}}
	}
	M = append(M, Section{Title: "When focused on a message", ID: "message_shortcuts", Shortcuts: []Shortcut{
		{Title: "Edit (your own message)", Keys: []string{"key_E"}},
		{Title: "Delete (your own message)", Keys: []string{"key_Delete"}},
		{Title: "Add an emoji reaction", Keys: []string{"key_R"}},
		{Title: "Open a thread", Keys: []string{"key_→", "or", "key_T"}},
		{Title: "Pin / unpin", Keys: []string{"key_P"}},
		forward,
		{Title: "Save / remove from Later", Keys: []string{"key_A"}, disabled: o.LaterTodos},
		{Title: "Add / complete a to-do", Keys: []string{"key_A"}, disabled: !o.LaterTodos},
		{Title: "Save / remove from Saved", Keys: []string{"key_Cmd", "key_Shift", "key_S"}, disabled: !o.LaterTodos, browserBlock: []string{"firefox"}},
		{Title: "Mark as unread from this message", Keys: []string{"key_U"}},
		{Title: "Remind me about this message", Keys: []string{"key_M"}},
		{Title: "Copy a link to this message", Keys: []string{"key_L"}},
		{Title: "Copy text from this message", Keys: []string{"key_Cmd", "key_C"}},
	}})
	M = append(M, Section{Title: "Channel Sections", ID: "channel_sections", Shortcuts: []Shortcut{
		{Title: "Collapse or expand all", Keys: []string{"key_Option", "  and click section"}, keysWin: []string{"key_Alt", "  and click section"}},
	}})
	if o.Huddles {
		h := Section{Title: "Huddles", ID: "huddles_shortcuts"}
		if x {
			h.Shortcuts = append(h.Shortcuts, Shortcut{Title: "Toggle huddles start / stop", Keys: []string{"key_Shift", "key_Cmd", "key_H"},
				keysWin: []string{"key_Ctrl", "key_Shift", "key_H"}})
		}
		h.Shortcuts = append(h.Shortcuts, Shortcut{Title: "Toggle huddles mute / unmute", Keys: []string{"key_Shift", "key_Cmd", "key_Space"},
			keysWin: []string{"key_Ctrl", "key_Shift", "key_Space"}})
		M = append(M, h) // after calls_shortcuts, which this build no longer has: at the end
	}
	// (Simplified layout mode's section only shows on Slack's dev and JS
	// paths: EjMV Cu, b3.)
	if nav := sectionIndex(M, "navigation"); nav >= 0 && x {
		M[nav].Shortcuts = append(M[nav].Shortcuts, Shortcut{Title: "Jump to unread messages", Keys: []string{"key_Cmd", "key_J"}})
		// isWindowingSupported's new-window rows: Clack has one window
	}
	if o.CanvasAccess {
		react := Shortcut{Title: "View reactions", Keys: []string{"key_Cmd", "key_Option", "key_E"}}
		readerView := []string{"key_Cmd", "key_Option", "key_R"}
		canvas := []Shortcut{
			{Title: "View comment thread", Keys: []string{"key_Cmd", "key_Option", "key_T"}},
			react,
			{Title: "Open context menu", Keys: []string{"key_Ctrl", "key_Enter"}, keysWin: []string{"key_Shift", "key_F10"}, keysLinux: []string{"key_Ctrl", "key_Shift", "key_/"}},
			{Title: "Show reader / edit view", Keys: readerView},
			{Title: "Find / Find next", Keys: []string{"key_Cmd", "key_F"}},
			{Title: "Find previous", Keys: []string{"key_Cmd", "key_Shift", "key_F"}},
			{Title: "Insert link", Keys: []string{"key_Cmd", "key_Shift", "key_U"}},
			{Title: "Copy anchor link", Keys: []string{"key_Ctrl", "key_Option", "key_Q"}},
			{Title: "Undo", Keys: []string{"key_Cmd", "key_Z"}},
			{Title: "Redo", Keys: []string{"key_Cmd", "key_Shift", "key_Z"}, keysWin: []string{"key_Ctrl", "key_Y"}},
			{Title: "Move list item up", Keys: []string{"key_Cmd", "key_Option", "key_↑"}},
			{Title: "Move list item down", Keys: []string{"key_Cmd", "key_Option", "key_↓"}},
			{Title: "Bold", Keys: []string{"key_Cmd", "key_B"}},
			{Title: "Italic", Keys: []string{"key_Cmd", "key_I"}},
			{Title: "Underline", Keys: []string{"key_Cmd", "key_U"}},
			{Title: "Strikethrough", Keys: []string{"key_Cmd", "key_Shift", "key_X"}},
			{Title: "Inline code", Keys: []string{"key_Cmd", "key_Shift", "key_C"}},
			{Title: "Paragraph (normal text) style", Keys: []string{"key_Cmd", "key_Option", "key_0"}},
			{Title: "Big heading style", Keys: []string{"key_Cmd", "key_Option", "key_1"}},
			{Title: "Medium heading style", Keys: []string{"key_Cmd", "key_Option", "key_2"}},
			{Title: "Small heading style", Keys: []string{"key_Cmd", "key_Option", "key_3"}},
			{Title: "Block quote style", Keys: []string{"key_Cmd", "key_Shift", kb(">")}},
			{Title: "Code block style", Keys: []string{"key_Cmd", "key_Option", "key_Shift", "key_C"}},
			{Title: "Checklist style", Keys: []string{"key_Cmd", "key_Shift", "key_9"}},
			{Title: "Bulleted list style", Keys: []string{"key_Cmd", "key_Shift", "key_8"}},
			{Title: "Numbered list style", Keys: []string{"key_Cmd", "key_Shift", "key_7"}},
			{Title: "Toggle heading and list styles", Keys: []string{"key_Ctrl", kb("`")}},
			{Title: "Insert file", Keys: []string{"key_Cmd", "key_O"}},
			{Title: "Interact with card view", Keys: []string{"key_Cmd", "key_Enter"}},
		}
		at := sectionIndex(M, "workspaces") // before Workspace Switching
		if at < 0 {
			at = len(M)
		}
		M = append(M[:at], append([]Section{{Title: "Canvas Editing", ID: "canvas_shortcuts", Shortcuts: canvas}}, M[at:]...)...)
	}
	if i := sectionIndex(M, "messaging"); i >= 0 {
		M[i].Shortcuts = append(M[i].Shortcuts,
			Shortcut{Title: "Bold text", Keys: []string{"key_Cmd", "key_B"}},
			Shortcut{Title: "Italic text", Keys: []string{"key_Cmd", "key_I"}},
			Shortcut{Title: "Underline text", Keys: []string{"key_Cmd", "key_U"}},
			Shortcut{Title: "Strikethrough text", Keys: []string{"key_Shift", "key_Cmd", "key_X"}},
			Shortcut{Title: "Format text as code", Keys: []string{"key_Shift", "key_Cmd", "key_C"}},
			Shortcut{Title: "Format selection as bullet list", Keys: []string{"key_Shift", "key_Cmd", "key_8"}},
			Shortcut{Title: "Format selection as numbered list", Keys: []string{"key_Shift", "key_Cmd", "key_7"}},
			Shortcut{Title: "Format selection as quote", Keys: []string{"key_Shift", "key_Cmd", "key_9"}},
			Shortcut{Title: "Format selection as code block", Keys: []string{"key_Option", "key_Shift", "key_Cmd", "key_c"},
				keysWin: []string{"key_Alt", "key_Shift", "key_Cmd", "key_c"}},
		)
	}
	if i := sectionIndex(M, "basics"); i >= 0 {
		M[i].Shortcuts = append(M[i].Shortcuts,
			Shortcut{Title: "Set a status", Keys: []string{"key_Shift", "key_Cmd", "key_Y"}, ID: "set_a_status"},
			Shortcut{Title: "Open Help", Keys: []string{"key_F1"}, ID: "open_help", ssbOnly: true},
		)
	}

	osName := map[string]string{"darwin": "mac", "windows": "windows"}[o.GOOS]
	if osName == "" {
		osName = "linux"
	}
	keep := func(s Shortcut) bool { // shortcutFilter, in the desktop app (no browser name)
		if s.ssbOnly && !x || s.disabled {
			return false
		}
		return !containsStr(s.browserBlock, osName) && !containsStr(s.localeBlock, o.KeyboardLocale)
	}
	out := make([]Section, 0, len(M))
	for _, sec := range M {
		var rows []Shortcut
		for _, s := range sec.Shortcuts {
			if keep(s) {
				s.Keys = keysForOS(s, o.GOOS, o.KeyboardLocale)
				rows = append(rows, s)
			}
		}
		if len(rows) > 0 || sec.ID == "basics" {
			sec.Shortcuts = rows
			out = append(out, sec)
		}
	}
	return out
}

// keysForOS is replaceModifierKeysForOS.
func keysForOS(s Shortcut, goos, locale string) []string {
	switch {
	case goos == "windows" && s.keysWin != nil:
		return s.keysWin
	case goos == "linux" && s.keysLinux != nil:
		return s.keysLinux
	}
	out := make([]string, len(s.Keys))
	for i, k := range s.Keys {
		name, ok := strings.CutPrefix(k, "key_")
		switch {
		case !ok:
			out[i] = k
		case name == "Cmd" || name == "Option" || name == "Ctrl" || name == "CtrlAlternative" || name == "ShiftAlternative":
			out[i] = "key_" + modifierForOS(name, goos, locale)
		default:
			out[i] = k
		}
	}
	return out
}

// modifierForOS is getModifierKeyForOS.
func modifierForOS(name, goos, locale string) string {
	other := goos != "darwin"
	switch {
	case (name == "Cmd" || name == "Ctrl") && other && locale == "de":
		return "Strg"
	case name == "Cmd" && other:
		return "Ctrl"
	case name == "Option" && other:
		return "Alt"
	case name == "CtrlAlternative":
		return "Ctrl"
	case name == "ShiftAlternative":
		return "Shift"
	}
	return name
}

// keyForLocale is the key a character sits on for the keyboard locale
// (Slack's kB): Clack only knows the US layout, where it is the
// character itself.
func keyForLocale(c, _ string) string { return c }

// FilterSections is the search box's filterSections: the rows whose title
// contains q (case-insensitive), in sections that keep at least one.
func FilterSections(secs []Section, q string) []Section {
	q = strings.ToLower(q)
	var out []Section
	for _, s := range secs {
		var rows []Shortcut
		for _, r := range s.Shortcuts {
			if r.Title != "" && strings.Contains(strings.ToLower(r.Title), q) {
				rows = append(rows, r)
			}
		}
		if len(rows) > 0 {
			s.Shortcuts = rows
			out = append(out, s)
		}
	}
	return out
}

// CountShortcuts is countShortcuts.
func CountShortcuts(secs []Section) int {
	n := 0
	for _, s := range secs {
		n += len(s.Shortcuts)
	}
	return n
}

func sectionIndex(secs []Section, id string) int {
	for i, s := range secs {
		if s.ID == id {
			return i
		}
	}
	return -1
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func itoa1(n int) string { return string(rune('0' + n)) }
