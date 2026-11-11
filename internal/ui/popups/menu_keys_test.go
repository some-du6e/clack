package popups

import (
	"context"
	"encoding/json"
	"image"
	"slices"
	"strings"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// mkEnv drives one menu through Gio's router at 1000x800.
type mkEnv struct {
	t   *testing.T
	r   input.Router
	ops op.Ops
	m   *Menu
	now time.Time
	// clip is the last text the menu put on the clipboard.
	clip string
}

func newMkEnv(t *testing.T, m *Menu) *mkEnv {
	e := &mkEnv{t: t, m: m, now: time.Date(2026, 11, 8, 15, 0, 0, 0, time.UTC)}
	// two frames: the first one's FocusCmd holds that frame's events
	e.frame()
	e.frame()
	return e
}

func (e *mkEnv) frame() {
	e.ops.Reset()
	gtx := layout.Context{Ops: &e.ops, Source: e.r.Source(), Now: e.now,
		Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1000, 800))}
	e.m.Layout(gtx)
	e.r.Frame(&e.ops)
	if _, b, ok := e.r.WriteClipboard(); ok {
		e.clip = string(b)
	}
	e.now = e.now.Add(16 * time.Millisecond)
}

// press sends a key press and release and runs the frames that handle it.
func (e *mkEnv) press(n key.Name, mods key.Modifiers) {
	e.r.Queue(key.Event{Name: n, Modifiers: mods, State: key.Press})
	e.frame()
	e.r.Queue(key.Event{Name: n, Modifiers: mods, State: key.Release})
	e.frame()
	e.frame()
}

// mkHost is hcEnv with its core booted against the fake Slack, so the
// menus' actions (mark unread, save, pin) run.
func mkHost(t *testing.T) *tzHost {
	t.Helper()
	h := hcEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- h.a.Boot(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("boot: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("boot timed out")
	}
	return h
}

func setGOOS(t *testing.T, goos string) {
	prev := shortcut.GOOS
	shortcut.GOOS = goos
	t.Cleanup(func() { shortcut.GOOS = prev })
}

// mkMsg is a synthetic message; own makes it the signed-in user's.
func mkMsg(own bool) core.Message {
	m := core.Message{Channel: "C0FAKE00001", TS: "1700000000.000100", User: fakeslack.ProfileID, Text: "synthetic menu keys"}
	if own {
		m.User = fakeslack.SelfID
	}
	return m
}

func lastShown[T any](h *tzHost) (T, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var zero T
	if len(h.shown) == 0 {
		return zero, false
	}
	v, ok := h.shown[len(h.shown)-1].(T)
	return v, ok
}

// waitToast runs frames (async results come back through drain on the
// UI goroutine) until h has toasted.
func waitToast(h *tzHost, e *mkEnv, d time.Duration) []string {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		e.frame()
		if ts := h.toastList(); len(ts) > 0 {
			return ts
		}
		time.Sleep(10 * time.Millisecond)
	}
	return h.toastList()
}

// Right-click a message, press L: the permalink is on the clipboard and
// the menu has closed (Karim's report). Same on macOS and elsewhere.
func TestMenuKeyCopyLink(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows"} {
		t.Run(goos, func(t *testing.T) {
			setGOOS(t, goos)
			h := mkHost(t)
			m := mkMsg(false)
			e := newMkEnv(t, NewMessageContextMenu(h, image.Pt(200, 200), m, InChannel, MessageMenuOpts{}))
			e.press("L", 0)
			want := permalink(h, m.Channel, m.TS, "")
			if e.clip != want {
				t.Errorf("clipboard %q, want %q", e.clip, want)
			}
			if !e.m.Dismissed() {
				t.Error("menu still open after L")
			}
			// Slack's copyLink (5FS0) only writes the clipboard
			if ts := h.toastList(); slices.Contains(ts, "Link copied") {
				t.Errorf("toasts %q", ts)
			}
		})
	}
}

// L before the core has booted still copies (the workspace domain comes
// from the store, so the UI goroutine never waits for Boot).
func TestMenuKeyCopyLinkBeforeBoot(t *testing.T) {
	setGOOS(t, "linux")
	h := hcEnv(t) // never booted
	m := mkMsg(false)
	e := newMkEnv(t, NewMessageContextMenu(h, image.Pt(200, 200), m, InChannel, MessageMenuOpts{}))
	done := make(chan struct{})
	go func() { e.press("L", 0); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("L blocked the frame before boot")
	}
	if !strings.HasPrefix(e.clip, "https://") || !strings.Contains(e.clip, "/archives/"+m.Channel+"/p") || !e.m.Dismissed() {
		t.Errorf("clip %q closed %v", e.clip, e.m.Dismissed())
	}
}

// Every message action key Slack binds (module EtVr's P2) runs its item
// and closes the menu, in the context menu and the ⋯ More actions menu.
func TestMenuKeysMessageActions(t *testing.T) {
	type check func(t *testing.T, h *tzHost, e *mkEnv, hooks *mkHooks)
	shownIs := func(want string) check {
		return func(t *testing.T, h *tzHost, e *mkEnv, _ *mkHooks) {
			h.mu.Lock()
			defer h.mu.Unlock()
			if len(h.shown) == 0 {
				t.Fatalf("nothing shown, want %s", want)
			}
			got := strings.TrimPrefix(strings.TrimPrefix(typeName(h.shown[len(h.shown)-1]), "*popups."), "popups.")
			if got != want {
				t.Errorf("shown %s, want %s", got, want)
			}
		}
	}
	hook := func(name string) check {
		return func(t *testing.T, _ *tzHost, _ *mkEnv, hooks *mkHooks) {
			if !slices.Contains(hooks.ran, name) {
				t.Errorf("hooks ran %q, want %s", hooks.ran, name)
			}
		}
	}
	toasted := func(t *testing.T, h *tzHost, e *mkEnv, _ *mkHooks) {
		if ts := waitToast(h, e, 10*time.Second); len(ts) == 0 {
			t.Error("no toast: the action didn't run")
		}
	}
	// Mark unread toasts nothing in Slack (a screen-reader announcement):
	// the read cursor moves just before the message
	markedUnread := func(t *testing.T, h *tzHost, e *mkEnv, _ *mkHooks) {
		const want = "1700000000.000099"
		end := time.Now().Add(10 * time.Second)
		for time.Now().Before(end) {
			e.frame()
			if h.a.Count("C0FAKE00001").LastRead == want {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Errorf("read cursor %q, want %s: Mark unread didn't run", h.a.Count("C0FAKE00001").LastRead, want)
	}
	copied := func(t *testing.T, _ *tzHost, e *mkEnv, _ *mkHooks) {
		if e.clip != "synthetic menu keys" {
			t.Errorf("clipboard %q", e.clip)
		}
	}
	cases := []struct {
		goos  string
		menu  string // "context" or "more"
		own   bool
		name  key.Name
		mods  key.Modifiers
		check check
	}{
		{"linux", "context", false, "R", 0, shownIs("EmojiPicker")},
		{"linux", "context", false, "T", 0, hook("reply")},
		{"linux", "context", false, "F", 0, shownIs("Modal")},
		{"linux", "context", false, "S", 0, shownIs("Modal")}, // SHARE is s and f
		{"linux", "context", false, "A", 0, hook("todo")},
		{"linux", "context", true, "E", 0, hook("edit")},
		{"linux", "more", true, "E", 0, hook("edit")},
		{"linux", "context", false, "U", 0, markedUnread},
		{"linux", "more", false, "U", 0, markedUnread},
		{"linux", "context", false, "C", key.ModCtrl, copied},
		{"darwin", "context", false, "C", key.ModCommand, copied},
		{"linux", "context", false, "S", key.ModCtrl | key.ModShift, toasted},    // Add to saved
		{"darwin", "more", false, "S", key.ModCommand | key.ModShift, toasted},   // ⇧⌘S
		{"linux", "context", true, key.NameDeleteForward, 0, shownIs("Modal")},   // Delete message…
		{"darwin", "context", true, key.NameDeleteBackward, 0, shownIs("Modal")}, // the Mac's delete key
		{"darwin", "more", true, key.NameDeleteForward, 0, shownIs("Modal")},     // fn-delete
		{"linux", "more", false, "L", 0, func(t *testing.T, h *tzHost, e *mkEnv, _ *mkHooks) {
			if want := permalink(h, "C0FAKE00001", "1700000000.000100", ""); e.clip != want {
				t.Errorf("clipboard %q, want %q", e.clip, want)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.goos+"/"+c.menu+"/"+string(c.name)+"/"+c.mods.String(), func(t *testing.T) {
			setGOOS(t, c.goos)
			h := mkHost(t)
			e, hooks := mkOpen(t, h, c.menu, c.own)
			e.press(c.name, c.mods)
			c.check(t, h, e, hooks)
			if !e.m.Dismissed() {
				t.Error("menu still open")
			}
		})
	}
}

// mkHooks records MessageMenuOpts hooks.
type mkHooks struct{ ran []string }

func mkOpen(t *testing.T, h *tzHost, which string, own bool) (*mkEnv, *mkHooks) {
	hooks := &mkHooks{}
	o := MessageMenuOpts{
		OnEdit:  func() { hooks.ran = append(hooks.ran, "edit") },
		OnReply: func() { hooks.ran = append(hooks.ran, "reply") },
		OnTodo:  func() { hooks.ran = append(hooks.ran, "todo") },
	}
	var m *Menu
	if which == "more" {
		m = NewMessageMenuOpts(h, image.Pt(600, 100), mkMsg(own), o)
	} else {
		m = NewMessageContextMenu(h, image.Pt(200, 200), mkMsg(own), InChannel, o)
	}
	return newMkEnv(t, m), hooks
}

func typeName(v any) string {
	switch v.(type) {
	case *EmojiPicker:
		return "*popups.EmojiPicker"
	case *Modal:
		return "*popups.Modal"
	case *Menu:
		return "*popups.Menu"
	}
	return "other"
}

// Keys Slack doesn't bind, Shift+letter, and (on macOS) Control+key do
// nothing and leave the menu open; on Windows and Linux ⌘'s Super key is
// no Ctrl.
func TestMenuKeysIgnored(t *testing.T) {
	cases := []struct {
		goos string
		name key.Name
		mods key.Modifiers
	}{
		{"linux", "Z", 0},
		{"linux", "L", key.ModShift},
		{"darwin", "C", key.ModCtrl},
		{"linux", "C", key.ModSuper},
		{"linux", "P", 0},                    // Pin is in Organize ▸; the submenu has the key
		{"linux", key.NameDeleteForward, 0},  // not own: no Delete message…
		{"linux", key.NameDeleteBackward, 0}, // Backspace deletes on macOS only
		{"darwin", "E", 0},                   // not own: no Edit message
	}
	for _, c := range cases {
		t.Run(c.goos+"/"+string(c.name)+"/"+c.mods.String(), func(t *testing.T) {
			setGOOS(t, c.goos)
			h := mkHost(t)
			e, hooks := mkOpen(t, h, "context", false)
			e.press(c.name, c.mods)
			if e.m.Dismissed() {
				t.Error("menu closed")
			}
			if e.clip != "" || h.shownCount() != 0 || len(hooks.ran) != 0 {
				t.Errorf("something ran: clip %q, shown %d, hooks %q", e.clip, h.shownCount(), hooks.ran)
			}
		})
	}
}

// M opens Add a reminder ▸ with the keyboard (it's a submenu row); P
// pins once the Organize ▸ submenu has keyboard focus.
func TestMenuKeysSubmenus(t *testing.T) {
	setGOOS(t, "linux")
	h := mkHost(t)
	e, _ := mkOpen(t, h, "more", false)
	e.press("M", 0)
	if e.m.Dismissed() || e.m.sub == nil || !e.m.sub.kbd || e.m.items[e.m.subIdx].Label != "Add a reminder" {
		t.Fatalf("M: submenu %v", e.m.sub != nil)
	}
	e.press(key.NameEscape, 0)
	if e.m.sub != nil || e.m.Dismissed() {
		t.Fatal("Escape should close only the submenu")
	}
	org := slices.IndexFunc(e.m.items, func(it MenuItem) bool { return it.Label == "Organize" })
	e.m.sel = org
	e.press(key.NameRightArrow, 0)
	if e.m.sub == nil || !e.m.sub.kbd {
		t.Fatal("→ didn't open Organize")
	}
	e.press("P", 0)
	if !e.m.Dismissed() {
		t.Error("P in Organize ▸ left the menu open")
	}
	if ts := waitToast(h, e, 10*time.Second); len(ts) == 0 {
		t.Error("P didn't pin")
	}
}

// Arrows, Home/End, Enter and Escape navigate as before.
func TestMenuKeysNavigation(t *testing.T) {
	setGOOS(t, "linux")
	h := mkHost(t)
	e, hooks := mkOpen(t, h, "context", false)
	e.press(key.NameDownArrow, 0)
	if e.m.sel != 0 {
		t.Fatalf("↓ sel %d", e.m.sel)
	}
	e.press(key.NameDownArrow, 0)
	e.press(key.NameUpArrow, 0)
	if e.m.sel != 0 {
		t.Fatalf("↓↑ sel %d", e.m.sel)
	}
	e.press(key.NameEnd, 0)
	if e.m.sel != len(e.m.items)-1 {
		t.Fatalf("End sel %d of %d", e.m.sel, len(e.m.items))
	}
	e.press(key.NameHome, 0)
	e.press(key.NameDownArrow, 0) // Reply in thread
	e.press(key.NameReturn, 0)
	if !e.m.Dismissed() || !slices.Contains(hooks.ran, "reply") {
		t.Fatalf("Enter on Reply in thread: closed %v, hooks %q", e.m.Dismissed(), hooks.ran)
	}
	e2, _ := mkOpen(t, h, "context", false)
	e2.press(key.NameEscape, 0)
	if !e2.m.Dismissed() {
		t.Error("Escape left the menu open")
	}
}

// With Slack's sk_menu_typeahead toggle on, letters move the highlight
// (and off macOS run a unique match) instead of running item keys;
// shortcuts with modifiers still run.
func TestMenuTypeAhead(t *testing.T) {
	prev := MenuTypeAhead
	MenuTypeAhead = true
	t.Cleanup(func() { MenuTypeAhead = prev })
	label := func(e *mkEnv) string {
		if e.m.sel < 0 {
			return ""
		}
		return e.m.items[e.m.sel].Label
	}

	setGOOS(t, "linux")
	h := mkHost(t)
	// a team that allows private message forwarding: the share row is
	// "Forward message…" (6bWJ), the menu's only F
	h.a.SetTeamPrefs(map[string]json.RawMessage{"private_message_forwarding": json.RawMessage("true")})
	e, _ := mkOpen(t, h, "context", false)
	e.press("C", 0) // Copy link, Copy message, Connect to apps: cycle
	first := label(e)
	e.press("C", 0)
	if first == "" || label(e) == first || !strings.HasPrefix(strings.ToLower(label(e)), "c") || e.m.Dismissed() {
		t.Errorf("C, C highlighted %q then %q", first, label(e))
	}
	e.press("L", 0) // no label starts with l, and L isn't Copy link now
	if e.clip != "" || e.m.Dismissed() {
		t.Errorf("L ran with type-ahead on: clip %q", e.clip)
	}
	e.press("F", 0) // the only F: runs Forward message…
	if _, ok := lastShown[*Modal](h); !ok || !e.m.Dismissed() {
		t.Error("F (unique) didn't run Forward message…")
	}
	e, _ = mkOpen(t, h, "context", false)
	e.press("C", key.ModCtrl) // modified shortcuts still run
	if e.clip != "synthetic menu keys" || !e.m.Dismissed() {
		t.Errorf("Ctrl+C with type-ahead: clip %q", e.clip)
	}

	setGOOS(t, "darwin")
	e, _ = mkOpen(t, h, "context", false)
	for _, k := range []key.Name{"C", "O", "N"} { // "con…" → Connect to apps
		e.press(k, 0)
	}
	if label(e) != "Connect to apps" || e.m.Dismissed() {
		t.Errorf("macOS buffer con highlighted %q", label(e))
	}
	e.now = e.now.Add(2 * time.Second) // the buffer resets after 1s
	e.press("F", 0)
	if label(e) != "Forward message…" || e.m.Dismissed() {
		t.Errorf("macOS F highlighted %q (should not run)", label(e))
	}
}

// Hints use the platform's formatting: ⌘C and ⇧⌘S on macOS, Ctrl+C and
// Ctrl+Shift+S elsewhere; the letter keys show as Slack's labels.
func TestMenuHintsPerOS(t *testing.T) {
	want := map[string]map[string]string{
		"darwin": {"Copy message": "⌘C", "Add to saved": "⇧⌘S", "Copy link": "L", "Add reaction…": "R", "Mark unread": "U", "Add a reminder": "M", "Delete message…": "delete", "Edit message": "E"},
		"linux":  {"Copy message": "Ctrl+C", "Add to saved": "Ctrl+Shift+S", "Copy link": "L", "Add reaction…": "R", "Mark unread": "U", "Add a reminder": "M", "Delete message…": "delete", "Edit message": "E"},
	}
	for goos, w := range want {
		setGOOS(t, goos)
		h := mkHost(t)
		got := map[string]string{}
		for _, it := range NewMessageContextMenu(h, image.Pt(10, 10), mkMsg(true), InChannel, MessageMenuOpts{}).Items() {
			got[it.Label] = it.Hint
		}
		for l, hint := range w {
			if got[l] != hint {
				t.Errorf("%s: %s hint %q, want %q", goos, l, got[l], hint)
			}
		}
		if sel := NewTextSelectionMenu(h, image.Pt(10, 10), "x").Items()[0].Hint; sel != w["Copy message"] {
			t.Errorf("%s: Copy text hint %q", goos, sel)
		}
	}
}

// The text selection menu's Copy text runs on ⌘C / Ctrl+C.
func TestMenuKeysTextSelection(t *testing.T) {
	setGOOS(t, "darwin")
	h := mkHost(t)
	e := newMkEnv(t, NewTextSelectionMenu(h, image.Pt(100, 100), "selected words"))
	e.press("C", key.ModCommand)
	if e.clip != "selected words" || !e.m.Dismissed() {
		t.Errorf("⌘C: clip %q closed %v", e.clip, e.m.Dismissed())
	}
}

// Plugins see each row's shortcut in the MenuTemplateItem ({label, key}
// like Slack's) and their own rows' shortcuts bind too.
func TestMenuTemplateShortcuts(t *testing.T) {
	setGOOS(t, "linux")
	h := mkHost(t)
	m := NewMessageContextMenu(h, image.Pt(10, 10), mkMsg(false), InChannel, MessageMenuOpts{})
	tpl := m.menuTemplate(m.orig)
	var link slackc.Props
	for _, v := range tpl {
		if p := asProps(v); p != nil && p["label"] == "Copy link" {
			link = asProps(p["shortcut"])
		}
	}
	if link == nil || link["label"] != "L" || !slices.Equal(link.List("key"), []any{"l"}) {
		t.Fatalf("Copy link shortcut %v", link)
	}
	ran := false
	tpl = append(tpl, slackc.Props{"key": "plugin_row", "label": "Plugin thing",
		"shortcut": slackc.Props{"label": "K", "key": "k"}, "click": func() { ran = true }})
	items := menuFromTemplate(h.Theme(), tpl, m.orig)
	last := items[len(items)-1]
	if last.Hint != "K" || !slices.Equal(last.Keys, []string{"k"}) {
		t.Fatalf("plugin row %q %q", last.Hint, last.Keys)
	}
	if i := slices.IndexFunc(items, func(it MenuItem) bool { return it.Label == "Copy link" }); i < 0 || !slices.Equal(items[i].Keys, KeyCopyLink) {
		t.Fatal("Copy link lost its keys through the template")
	}
	pm := NewMenuOpts(h, items, MenuOpts{Anchor: pointRect(image.Pt(10, 10)), Raw: true})
	e := newMkEnv(t, pm)
	e.press("K", 0)
	if !ran || !pm.Dismissed() {
		t.Errorf("plugin K: ran %v closed %v", ran, pm.Dismissed())
	}
}
