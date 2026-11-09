package ui

import (
	"image"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
)

func menuLabels(items []MenuItem) string {
	var out []string
	for _, it := range items {
		out = append(out, it.Label)
	}
	return strings.Join(out, " | ")
}

func topOverlay[T any](a *App) (T, bool) {
	for i := len(a.overlays) - 1; i >= 0; i-- {
		if v, ok := a.overlays[i].(T); ok && !a.overlays[i].Dismissed() {
			return v, true
		}
	}
	var zero T
	return zero, false
}

// shotTo saves the window (CREATE_CHANNEL_SHOTS_DIR, the create-channel
// workflow) as name.png.
func (e *perfEnv) shotTo(t *testing.T, dir, name string) {
	t.Helper()
	if dir == "" || e.gpu == nil {
		return
	}
	img := image.NewRGBA(image.Rectangle{Max: e.size})
	if err := e.gpu.Screenshot(img); err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// Every Slack entry point to the create-channel modal opens it here.
func TestCreateChannelEntryPoints(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	a := e.a

	var got []MenuItem
	oldMenu, oldAt := mkMenu, mkMenuAt
	t.Cleanup(func() { mkMenu, mkMenuAt = oldMenu, oldAt })
	mkMenu = func(a *App, anchor image.Point, items []MenuItem) Overlay { got = items; return nil }
	mkMenuAt = func(a *App, anchor image.Rectangle, side int, width float32, items []MenuItem) Overlay {
		if width != 360 || side != int(popups.Right) {
			t.Errorf("Create menu: width %v side %d, want Slack's 360 to the right", width, side)
		}
		got = items
		return nil
	}
	opens := func(what string, do func()) {
		t.Helper()
		for _, o := range a.overlays {
			if d, ok := o.(dismisser); ok {
				d.Dismiss()
			}
		}
		a.overlays = nil
		do()
		if _, ok := topOverlay[*popups.CreateChannelModal](a); !ok {
			t.Errorf("%s: no create-channel modal", what)
		}
	}
	pick := func(label string) func() {
		return func() {
			for _, it := range got {
				if it.Label == label && it.Do != nil {
					it.Do()
					return
				}
			}
			t.Errorf("no %q in %s", label, menuLabels(got))
		}
	}

	// the Channels footer's "Add channels"
	a.sidebarAction("add-channels")
	if s := menuLabels(got); s != "Create a new channel | Browse channels" {
		t.Errorf("Add channels menu: %s", s)
	}
	opens("Add channels → Create a new channel", pick("Create a new channel"))

	// the rail's + (Create menu)
	a.openCreateMenu(image.Rect(0, 0, 36, 36))
	if s := menuLabels(got); !strings.HasPrefix(s, "Create | Message | Channel | Huddle | Canvas") {
		t.Errorf("Create menu: %s", s)
	}
	opens("rail + → Channel", pick("Channel"))

	// section headings (the whole menus: section_menu_test.go)
	byType := map[string]*sbSection{}
	for i := range a.model().Sections {
		s := &a.model().Sections[i]
		if byType[s.Type] == nil {
			byType[s.Type] = s
		}
	}
	for typ, want := range map[string]bool{"channels": true, "standard": true, "stars": true, "direct_messages": false} {
		sec := byType[typ]
		if sec == nil {
			t.Errorf("no %s section", typ)
			continue
		}
		got = nil
		a.openSectionMenu(sec.ID, image.Pt(10, 10))
		has := strings.Contains(menuLabels(got), "| Create a channel |")
		if has != want {
			t.Errorf("%s heading menu %q: Create a channel %v, want %v", typ, menuLabels(got), has, want)
		}
		if want {
			opens(typ+" heading → Create a channel", pick("Create a channel"))
		}
		if typ == "channels" && !strings.Contains(menuLabels(got), "Browse channels") {
			t.Errorf("Channels heading menu has no Browse channels: %s", menuLabels(got))
		}
	}

	// the channel browser's button
	var h any = a
	if _, ok := h.(interface{ OpenCreateChannel() }); !ok {
		t.Error("*App has no OpenCreateChannel for Directories")
	}
	opens("Directories → Create Channel", a.OpenCreateChannel)
}

func byTypeName(m map[string]*sbSection, typ string) string {
	if s := m[typ]; s != nil {
		return s.Name
	}
	return ""
}

// The whole flow against the fake: type a name (normalized, checked), see
// a taken name's error, Next, pick Private, Create; the channel opens, sits
// in the sidebar, and "Add people" invites someone. With
// CREATE_CHANNEL_SHOTS_DIR set (the create-channel workflow, under xvfb)
// each step is saved as a screenshot.
func TestCreateChannelFlow(t *testing.T) {
	shots := os.Getenv("CREATE_CHANNEL_SHOTS_DIR")
	if shots != "" {
		t.Setenv("CLACK_BENCH_GPU", "1")
	}
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	var creates []url.Values
	fs.Hook = func(method string, f url.Values) (any, bool) {
		if method == "conversations.create" {
			creates = append(creates, f)
		}
		return nil, false
	}
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	e.settle(300 * time.Millisecond)
	a := e.a
	takenID := fs.ChannelIDs()[0]
	taken := a.model().conv(takenID).Name

	a.OpenCreateChannel()
	m, ok := topOverlay[*popups.CreateChannelModal](a)
	if !ok {
		t.Fatal("no modal")
	}
	e.frame()
	e.frame() // focus settles on the name input
	e.shotTo(t, shots, "1-name-empty")

	// typed text is normalized like Slack's input
	e.typeText("Launch Plan")
	e.until("name normalized", 5*time.Second, func() bool { return m.Name() == "launch-plan" })
	e.until("name checked", 5*time.Second, func() bool {
		return m.CanNext() && m.NameError() == "" && fs.CallCount("conversations.validateName") > 0
	})
	e.settle(1200 * time.Millisecond) // past the 1 s error reveal
	e.shotTo(t, shots, "2-name-typed")

	// a taken name: Slack's message under the input, Next disabled
	m.SetName(taken)
	e.until("taken error", 5*time.Second, func() bool { return strings.Contains(m.NameError(), "already taken") })
	e.shotTo(t, shots, "3-name-taken")
	m.Next()
	if m.Step() != "name" {
		t.Fatal("Next moved on with a taken name")
	}

	m.SetName("launch-plan")
	e.until("free name checked", 5*time.Second, func() bool { return m.CanNext() && m.Name() == "launch-plan" })
	e.settle(600 * time.Millisecond)
	e.key(key.NameReturn, 0) // Enter = Next
	e.until("visibility step", 5*time.Second, func() bool { return m.Step() == "visibility" })
	e.shotTo(t, shots, "4-visibility-public")
	m.SetPrivate(true)
	e.frame()
	e.frame()
	e.shotTo(t, shots, "5-visibility-private")

	m.Create()
	var add *popups.AddPeopleModal
	e.until("add people step", 10*time.Second, func() bool {
		add, ok = topOverlay[*popups.AddPeopleModal](a)
		return ok
	})
	ch := add.Channel()
	if len(creates) != 1 || creates[0].Get("name") != "launch-plan" || creates[0].Get("is_private") != "true" {
		t.Fatalf("conversations.create calls %v", creates)
	}
	if got := a.router.Current().Channel(); got != ch.ID {
		t.Errorf("route %q, want the new channel %s", a.router.Current(), ch.ID)
	}
	e.until("new channel in the sidebar", 10*time.Second, func() bool {
		for _, s := range a.model().Sections {
			if s.Type != "channels" {
				continue
			}
			for _, id := range s.Rows {
				if id == ch.ID {
					return true
				}
			}
		}
		return false
	})
	if b := strings.Join(add.Buttons(), ","); b != "Skip for now" {
		t.Errorf("add people buttons %q, want Skip for now", b)
	}
	e.settle(300 * time.Millisecond)
	e.shotTo(t, shots, "6-add-people")

	e.typeText("a")
	e.until("people results", 5*time.Second, func() bool { e.frame(); return fs.CallCount("edge/users/search") > 0 })
	e.settle(300 * time.Millisecond)
	e.shotTo(t, shots, "7-add-people-search")
	u := a.data.core.Users([]string{"U0FAKE00003"})["U0FAKE00003"]
	if u.ID == "" {
		u.ID, u.Name = "U0FAKE00003", "someone"
	}
	add.Pick(u)
	if b := strings.Join(add.Buttons(), ","); b != "Add" {
		t.Errorf("buttons with someone picked %q, want Add", b)
	}
	e.settle(200 * time.Millisecond)
	e.shotTo(t, shots, "8-add-people-picked")
	add.Add()
	e.until("invited", 10*time.Second, func() bool { return len(fs.Invited(ch.ID)) == 1 })
	e.until("add people closed", 5*time.Second, func() bool { return add.Dismissed() })

	// a restricted org: the error replaces the content, footer is "Okay"
	fs.Hook = func(method string, f url.Values) (any, bool) {
		if method == "conversations.create" {
			return map[string]any{"ok": false, "error": "restricted_action"}, true
		}
		return nil, false
	}
	a.OpenCreateChannel()
	m2, _ := topOverlay[*popups.CreateChannelModal](a)
	e.frame()
	m2.SetName("not-allowed")
	e.until("checked", 10*time.Second, func() bool { return m2.CanNext() && m2.Name() == "not-allowed" })
	m2.Next()
	if m2.Step() != "visibility" {
		t.Fatalf("Next with a checked free name stayed on %q", m2.Step())
	}
	m2.Create()
	e.until("create error", 10*time.Second, func() bool { return m2.CreateError() != "" })
	if !strings.Contains(m2.CreateError(), "may limit who can create channels") {
		t.Errorf("restricted message %q", m2.CreateError())
	}
	e.frame()
	e.shotTo(t, shots, "9-create-restricted")
}
