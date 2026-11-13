package popups

import (
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// ugRT drives the User Group pane and its overlays through a Gio
// router: the pane at the right of a 1000px window under the top bar, where
// the pane menus expect it, overlays on top in the full window. The
// fake's realtime events go straight into core (subteam_updated after
// each edit), and every call is recorded.
type ugRT struct {
	t     *testing.T
	h     *tzHost
	fs    *fakeslack.Server
	v     *UserGroupView
	r     input.Router
	ops   op.Ops
	calls pfCalls
	at    time.Duration
	win   image.Point
	mode  string
}

func newUGRT(t *testing.T, editor, dark bool, id string) *ugRT {
	t.Helper()
	h, fs := pfEnv(t)
	h.th.SetDark(dark)
	fs.UsergroupEditor = editor
	e := &ugRT{t: t, h: h, fs: fs, win: image.Pt(1000, 900), mode: map[bool]string{false: "light", true: "dark"}[dark]}
	fs.Emit = func(ev map[string]any) {
		b, _ := json.Marshal(ev)
		typ, _ := ev["type"].(string)
		h.a.Inject(slack.Event{Type: typ, Raw: b})
	}
	fs.SetHook(func(m string, f url.Values) (any, bool) {
		g := url.Values{}
		for k, v := range f {
			if k != "token" { // the client's own
				g[k] = v
			}
		}
		e.calls.add(m, g)
		return nil, false
	})
	gs, err := h.a.C.Usergroups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	h.a.S.SetUsergroups(gs)
	e.v = NewUserGroupView(h, id)
	return e
}

// open shows group id's pane anew (the same viewer).
func (e *ugRT) open(id string) { e.v = NewUserGroupView(e.h, id) }

func (e *ugRT) overlays() []Overlay {
	e.h.mu.Lock()
	defer e.h.mu.Unlock()
	var out []Overlay
	for _, o := range e.h.shown {
		if !o.Dismissed() {
			out = append(out, o)
		}
	}
	return out
}

// frame lays everything out once and returns the texts drawn.
func (e *ugRT) frame() []string {
	e.ops.Reset()
	gtx := layout.Context{Ops: &e.ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(e.win), Source: e.r.Source()}
	paint.Fill(&e.ops, colorsOf(e.h.th).Bg)
	stop := theme.StartTofuScan()
	at(gtx, image.Pt(e.paneX(), theme.TopNavH), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(pvW, e.win.Y-theme.TopNavH-theme.PanelInset))
		return e.v.Layout(gtx)
	})
	for _, o := range e.overlays() {
		o.Layout(gtx)
	}
	st := stop()
	if st.Notdef > 0 {
		e.t.Errorf("%s: %d .notdef glyphs in %q", e.mode, st.Notdef, st.Texts)
	}
	e.r.Frame(&e.ops)
	drain()
	return st.Texts
}

// until frames until cond holds.
func (e *ugRT) until(what string, cond func() bool) {
	e.t.Helper()
	for dl := time.Now().Add(10 * time.Second); ; {
		e.frame()
		if cond() {
			return
		}
		if time.Now().After(dl) {
			e.t.Fatalf("%s: timed out waiting for %s", e.mode, what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// texts frames until the async queue is quiet and returns the texts.
func (e *ugRT) texts() []string {
	e.frame()
	return e.frame()
}

func (e *ugRT) click(r image.Rectangle) {
	e.t.Helper()
	if r.Empty() {
		e.t.Fatalf("%s: clicking an empty rect", e.mode)
	}
	p := f32.Pt(float32(r.Min.X+r.Max.X)/2, float32(r.Min.Y+r.Max.Y)/2)
	e.at += time.Second
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p, Time: e.at},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: p, Time: e.at + time.Millisecond},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p, Time: e.at + 2*time.Millisecond},
	)
	e.frame()
	e.frame()
}

// pane is a pane widget's rect in window coordinates.
func (e *ugRT) pane(key string) image.Rectangle {
	return e.v.Hit(key).Add(image.Pt(e.paneX(), theme.TopNavH))
}

// paneX is the pane's left edge: at the window's right, PanelInset in.
func (e *ugRT) paneX() int { return e.win.X - theme.PanelInset - pvW }

func (e *ugRT) press(name key.Name) {
	e.r.Queue(key.Event{Name: name, State: key.Press}, key.Event{Name: name, State: key.Release})
	e.frame()
	e.frame()
}

// typeIn types s at the end of the focused editor ed.
func (e *ugRT) typeIn(ed interface{ Len() int }, s string) {
	n := ed.Len()
	e.r.Queue(key.EditEvent{Range: key.Range{Start: n, End: n}, Text: s}, key.SelectionEvent{Start: n + len([]rune(s)), End: n + len([]rune(s))})
	e.frame()
	e.frame()
}

// top is the newest open overlay.
func (e *ugRT) top() Overlay {
	os := e.overlays()
	for i := len(os) - 1; i >= 0; i-- {
		if _, ok := os[i].(*pump); !ok { // run()'s async pump
			return os[i]
		}
	}
	return nil
}

// menu is the open pane menu.
func (e *ugRT) menu() *pfPaneMenu {
	e.t.Helper()
	m, ok := e.top().(*pfPaneMenu)
	if !ok || m.menu == nil {
		e.t.Fatalf("%s: no pane menu open (%T)", e.mode, e.top())
	}
	return m
}

// pick clicks menu row label.
func (e *ugRT) pick(label string) {
	e.t.Helper()
	m := e.menu()
	i := slices.Index(m.menu.Labels(), label)
	if i < 0 || i >= len(m.menu.rows) {
		e.t.Fatalf("%s: no %q in %q", e.mode, label, m.menu.Labels())
	}
	e.click(m.menu.rows[i])
}

// modal is the open dialog's Modal.
func (e *ugRT) modal() *Modal {
	e.t.Helper()
	switch o := e.top().(type) {
	case *Modal:
		return o
	case *UserGroupMembersModal:
		return o.m
	case *UserGroupEditModal:
		return o.m
	}
	e.t.Fatalf("%s: no dialog open (%T)", e.mode, e.top())
	return nil
}

// button is the open dialog's footer button label.
func (e *ugRT) button(label string) image.Rectangle {
	e.t.Helper()
	for _, b := range e.modal().o.Buttons {
		if b.Label == label {
			return b.rect
		}
	}
	e.t.Fatalf("%s: no %q button", e.mode, label)
	return image.Rectangle{}
}

func (e *ugRT) toasted(s string) bool { return slices.Contains(e.h.toastList(), s) }

// shot saves the last frame as name-<mode>.png under PROFILE_SHOTS_DIR.
func (e *ugRT) shot(name string) {
	e.t.Helper()
	dir := os.Getenv("PROFILE_SHOTS_DIR")
	if dir == "" {
		return
	}
	e.frame()
	gpu, err := pixeltest.NewWindow(e.win.X, e.win.Y)
	if err != nil {
		e.t.Fatalf("headless window: %v", err)
	}
	defer gpu.Release()
	if err := gpu.Frame(&e.ops); err != nil {
		e.t.Fatalf("gpu frame: %v", err)
	}
	img := image.NewRGBA(image.Rectangle{Max: e.win})
	if err := gpu.Screenshot(img); err != nil {
		e.t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, name+"-"+e.mode+".png"))
	if err != nil {
		e.t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		e.t.Fatal(err)
	}
}

// ready waits for a workspace group's pane: the members and the rights.
func (e *ugRT) ready(members int) {
	e.t.Helper()
	e.until("the pane", func() bool {
		return e.v.loaded && e.v.started && !e.v.loading && e.v.rightsOK && len(e.v.members) == members
	})
	e.texts() // the frames that draw what the answers brought
}

var ugEditorTexts = []string{"Edit Members", "Edit Channels", "Edit Details", "Copy Group ID", "Enable group", "Add Members", "Add Default Channels"}

// An editor's pane has the action bar (Edit Members, Edit Channels, the
// ⋮ "More actions" with Edit group details..., Copy group ID and
// Deactivate group) and a ⋮ on every member row; a viewer without the
// rights sees today's pane: none of that.
func TestUserGroupPaneEditorBar(t *testing.T) {
	for _, dark := range []bool{false, true} {
		e := newUGRT(t, true, dark, fakeslack.WorkspaceGroupID)
		e.ready(6)
		x := e.texts()
		for _, want := range []string{"Snack crew", "@snackcrew", "Edit Members", "Edit Channels", "Members", "Channels"} {
			if !hasText(x, want) {
				t.Errorf("%s editor: %q not drawn", e.mode, want)
			}
		}
		if hasText(x, "Edit Details") || hasText(x, "Copy Group ID") {
			t.Errorf("%s editor: Edit Details / Copy Group ID drawn for a group it can edit", e.mode)
		}
		for _, u := range e.v.members {
			if e.v.Hit("more:" + u.ID).Empty() {
				t.Errorf("%s editor: no ⋮ on %s's row", e.mode, u.ID)
			}
		}
		e.shot("usergroup-editor-pane")
		e.click(e.pane("options"))
		if got := e.menu().menu.Labels(); !slices.Equal(got, []string{"Edit group details...", "Copy group ID", "Deactivate group"}) {
			t.Errorf("%s ⋮ menu %q", e.mode, got)
		}
		e.shot("usergroup-editor-menu")
		e.pick("Edit group details...")
		if _, ok := e.top().(*UserGroupEditModal); !ok {
			t.Errorf("%s Edit group details...: %T", e.mode, e.top())
		}

		// the same pane for a viewer who can't edit
		w := newUGRT(t, false, dark, fakeslack.WorkspaceGroupID)
		w.ready(6)
		x = w.texts()
		for _, bad := range ugEditorTexts {
			if hasText(x, bad) {
				t.Errorf("%s viewer: %q drawn", w.mode, bad)
			}
		}
		for k := range w.v.hits {
			if strings.HasPrefix(k, "more:") || k == "options" {
				t.Errorf("%s viewer: %s drawn", w.mode, k)
			}
		}
		if len(w.calls.get("usergroups.update"))+len(w.calls.get("usergroups.users.update")) != 0 {
			t.Errorf("%s viewer: an edit was sent", w.mode)
		}
	}
}

// Edit Members opens Edit members on the fetched members; typing finds
// people, Enter adds one, Backspace selects the last token and removes
// it, and Save sends usergroups.users.update {usergroup, users, team_id},
// toasts "Group has been updated." and the pane refetches the members
// (subteam_updated). Emptying the select disables Save and shows the
// error.
func TestUserGroupPaneEditMembers(t *testing.T) {
	for _, dark := range []bool{false, true} {
		e := newUGRT(t, true, dark, fakeslack.WorkspaceGroupID)
		e.ready(6)
		e.click(e.pane("edit-members"))
		d, ok := e.top().(*UserGroupMembersModal)
		if !ok {
			t.Fatalf("%s Edit Members opened %T", e.mode, e.top())
		}
		e.until("the members fetch", func() bool { return d.Loaded() && e.r.Source().Focused(&d.sel.ed) })
		if n := len(d.sel.IDs()); n != 6 {
			t.Fatalf("%s: %d members picked, want 6", e.mode, n)
		}
		if x := e.texts(); !hasText(x, "Edit members") || !hasText(x, "Save") {
			t.Errorf("%s: %q", e.mode, x)
		}
		e.shot("usergroup-edit-members")
		e.typeIn(&d.sel.ed, "a") // the fake matches usernames
		e.until("the people search", func() bool { return len(d.sel.Options()) > 0 })
		add := d.sel.Options()[0]
		e.press(key.NameReturn)
		if ids := d.sel.IDs(); len(ids) != 7 || ids[6] != add.ID {
			t.Fatalf("%s Enter: picked %v, want %s added", e.mode, ids, add.ID)
		}
		e.shot("usergroup-edit-members-picked")
		e.press(key.NameDeleteBackward) // selects the last token
		e.press(key.NameDeleteBackward) // removes it
		if ids := d.sel.IDs(); len(ids) != 6 || slices.Contains(ids, add.ID) || d.sel.sel != 5 {
			t.Fatalf("%s Backspace twice: %v, selected %d (want the one before it, 5)", e.mode, ids, d.sel.sel)
		}
		e.typeIn(&d.sel.ed, "a") // the fake matches usernames
		e.until("the people search again", func() bool { return len(d.sel.Options()) > 0 })
		add = d.sel.Options()[0]
		e.press(key.NameReturn)
		want := d.sel.IDs()
		e.click(e.button("Save"))
		e.until("the toast", func() bool { return e.toasted("Group has been updated.") })
		f := e.calls.get("usergroups.users.update")
		if len(f) != 1 || f[0].Get("usergroup") != fakeslack.WorkspaceGroupID || f[0].Get("users") != strings.Join(want, ",") || f[0].Get("team_id") != fakeslack.TeamID {
			t.Errorf("%s usergroups.users.update %v, want users %v", e.mode, f, want)
		}
		if !d.Dismissed() {
			t.Errorf("%s: Edit members still open after the save", e.mode)
		}
		e.until("the refetched members", func() bool { return len(e.v.members) == 7 && !e.v.loading })
		if n := len(e.calls.get("usergroups.users.list")); n < 3 { // the pane, the dialog, the refetch
			t.Errorf("%s: usergroups.users.list %d times", e.mode, n)
		}
		if e.v.g.UserCount != 7 {
			t.Errorf("%s: Members count %d after the save", e.mode, e.v.g.UserCount)
		}

		// emptied: Save off, the alert up, nothing sent
		e.click(e.pane("edit-members"))
		d = e.top().(*UserGroupMembersModal)
		e.until("the members fetch", func() bool { return d.Loaded() && e.r.Source().Focused(&d.sel.ed) })
		for range 7 + 1 { // onBackspace: select the last, then each press removes one and selects the one before
			e.press(key.NameDeleteBackward)
		}
		x := e.texts()
		if len(d.sel.IDs()) != 0 || d.CanSave() || !d.NoMembersError() || !hasText(x, "User groups need to have at least one member.") {
			t.Errorf("%s emptied: %v, can save %v, alert %v", e.mode, d.sel.IDs(), d.CanSave(), d.NoMembersError())
		}
		e.shot("usergroup-edit-members-empty")
		e.click(e.button("Save"))
		if n := len(e.calls.get("usergroups.users.update")); n != 1 {
			t.Errorf("%s: Save with nobody sent usergroups.users.update (%d)", e.mode, n)
		}
	}
}

// Edit Channels opens Edit User Group (the channel picker focused); the
// name validates 300 ms after typing (subteams.validateName {name,
// target_team}), a bad handle shows Slack's error and disables Save, and
// Save sends usergroups.update with Slack's arguments, toasts and closes.
// Cancel asks first (the speed bump): Keep Editing stays, Close closes.
func TestUserGroupPaneEditDetails(t *testing.T) {
	for _, dark := range []bool{false, true} {
		e := newUGRT(t, true, dark, fakeslack.WorkspaceGroupID)
		e.fs.SetUsergroupChannels(fakeslack.WorkspaceGroupID, []string{"C0FAKE00000"})
		e.ready(6)
		e.until("the default channels", func() bool { return len(e.v.g.DefaultChannels()) == 1 })
		e.click(e.pane("edit-channels"))
		m, ok := e.top().(*UserGroupEditModal)
		if !ok {
			t.Fatalf("%s Edit Channels opened %T", e.mode, e.top())
		}
		e.until("the channel picker's focus", func() bool { return e.r.Source().Focused(&m.chans.ed) })
		x := e.texts()
		for _, want := range []string{"Edit User Group", "Name", "Handle", "Purpose", "(optional)", "Channels", "Cancel", "Save",
			"User Group ID: " + fakeslack.WorkspaceGroupID} {
			if !hasText(x, want) {
				t.Errorf("%s Edit User Group: %q not drawn", e.mode, want)
			}
		}
		if ids := m.chans.IDs(); !slices.Equal(ids, []string{"C0FAKE00000"}) {
			t.Errorf("%s: picked channels %v", e.mode, ids)
		}
		e.shot("usergroup-edit-details")

		// a bad handle: Slack's error, Save off
		m.SetHandle("SnackCrew")
		e.until("the handle error", func() bool { _, he := m.Errors(); return he != "" })
		if _, he := m.Errors(); he != "Handles can only be in lowercase." || m.CanSave() {
			t.Errorf("%s: handle error %q, can save %v", e.mode, he, m.CanSave())
		}
		if x := e.texts(); !hasText(x, "Handles can only be in lowercase.") {
			t.Errorf("%s: the handle error isn't drawn", e.mode)
		}
		e.shot("usergroup-edit-details-error")
		m.SetHandle("snack squad") // the space goes
		e.until("the handle check", func() bool { _, he := m.Errors(); return he == "" && m.handle.Text() == "snacksquad" })

		// typing in the name (through the router) validates it
		e.focus(&m.name)
		e.typeIn(&m.name, " fans")
		e.until("subteams.validateName", func() bool { return len(e.calls.get("subteams.validateName")) > 0 })
		if f := e.calls.get("subteams.validateName"); f[len(f)-1].Get("name") != "Snack crew fans" || f[len(f)-1].Get("target_team") != fakeslack.TeamID {
			t.Errorf("%s subteams.validateName %v", e.mode, f)
		}
		e.until("Save on", func() bool { return m.CanSave() })
		e.click(e.button("Save"))
		e.until("the toast", func() bool { return e.toasted("Group has been updated.") })
		f := e.calls.get("usergroups.update")
		want := url.Values{"usergroup": {fakeslack.WorkspaceGroupID}, "name": {"Snack crew fans"}, "handle": {"snacksquad"},
			"channels": {"C0FAKE00000"}, "team_id": {fakeslack.TeamID}, "enable_section": {"false"}, "description": {"Synthetic workspace group"}}
		if len(f) != 1 || f[0].Encode() != want.Encode() {
			t.Errorf("%s usergroups.update\n got %v\nwant %v", e.mode, f, want)
		}
		e.until("the pane's new name", func() bool { return e.v.g.Name == "Snack crew fans" && e.v.g.Handle == "snacksquad" })
		if x := e.texts(); !hasText(x, "@snacksquad") {
			t.Errorf("%s: the pane doesn't show the new handle", e.mode)
		}

		// the speed bump
		e.click(e.pane("edit-channels"))
		m = e.top().(*UserGroupEditModal)
		e.frame()
		e.click(e.button("Cancel"))
		if !m.SpeedBump() || m.Dismissed() {
			t.Fatalf("%s Cancel: bump %v, dismissed %v", e.mode, m.SpeedBump(), m.Dismissed())
		}
		if x := e.texts(); !hasText(x, "Your work won’t be saved - are you sure you want to close?") || !hasText(x, "Keep Editing") {
			t.Errorf("%s speed bump: %q", e.mode, x)
		}
		e.shot("usergroup-edit-speedbump")
		e.click(m.keep.rect)
		if m.SpeedBump() || m.Dismissed() {
			t.Errorf("%s Keep Editing: bump %v, dismissed %v", e.mode, m.SpeedBump(), m.Dismissed())
		}
		e.click(e.button("Cancel"))
		e.click(m.cls.rect)
		if !m.Dismissed() {
			t.Errorf("%s Close: still open", e.mode)
		}
		if n := len(e.calls.get("usergroups.update")); n != 1 {
			t.Errorf("%s: closing sent usergroups.update (%d)", e.mode, n)
		}
	}
}

// focus gives tag the keyboard focus (a FocusCmd in a frame).
func (e *ugRT) focus(tag any) {
	e.ops.Reset()
	gtx := layout.Context{Ops: &e.ops, Now: time.Now(), Source: e.r.Source(), Constraints: layout.Exact(e.win)}
	gtx.Execute(key.FocusCmd{Tag: tag})
	e.frame()
	e.frame()
}

// A member row's ⋮: View profile and Remove from group…; the confirmation
// ("Remove from group?", Slack's sentence), then usergroups.users.update
// with the others (no team_id), "Member removed from group." and the
// refetched list.
func TestUserGroupPaneRemoveMember(t *testing.T) {
	for _, dark := range []bool{false, true} {
		e := newUGRT(t, true, dark, fakeslack.WorkspaceGroupID)
		e.ready(6)
		e.click(e.pane("more:" + fakeslack.ProfileID))
		if got := e.menu().menu.Labels(); !slices.Equal(got, []string{"View profile", "Remove from group…"}) {
			t.Fatalf("%s member ⋮ %q", e.mode, got)
		}
		e.shot("usergroup-member-menu")
		e.pick("Remove from group…")
		x := e.texts()
		for _, want := range []string{"Remove from group?", "Remove", "Cancel", "Snack", "crew"} {
			if !hasText(x, want) {
				t.Errorf("%s confirmation: %q not drawn (%q)", e.mode, want, x)
			}
		}
		e.shot("usergroup-remove-member")
		ids, _ := e.h.a.S.GroupUsers(fakeslack.WorkspaceGroupID)
		rest := slices.DeleteFunc(slices.Clone(ids), func(id string) bool { return id == fakeslack.ProfileID })
		e.click(e.button("Remove"))
		e.until("the toast", func() bool { return e.toasted("Member removed from group.") })
		f := e.calls.get("usergroups.users.update")
		if len(f) != 1 || f[0].Get("users") != strings.Join(rest, ",") || f[0].Has("team_id") {
			t.Errorf("%s removeUserGroupMember %v, want %v", e.mode, f, rest)
		}
		e.until("the list without them", func() bool {
			return len(e.v.members) == 5 && !slices.ContainsFunc(e.v.members, func(u core.User) bool { return u.ID == fakeslack.ProfileID })
		})
	}
}

// A default channel's ⋮: Open channel and Remove from group…; Remove
// sends usergroups.update {usergroup, channels, enable_section,
// description: ""}, toasts "Channel removed from group.", and the empty
// tab offers "Add Default Channels".
func TestUserGroupPaneRemoveChannel(t *testing.T) {
	e := newUGRT(t, true, false, fakeslack.WorkspaceGroupID)
	e.fs.SetUsergroupChannels(fakeslack.WorkspaceGroupID, []string{"C0FAKE00000", "C0FAKE00001"})
	e.ready(6)
	e.until("the default channels", func() bool { return len(e.v.g.DefaultChannels()) == 2 })
	e.click(e.pane("tab:1"))
	e.click(e.pane("chmore:C0FAKE00000"))
	if got := e.menu().menu.Labels(); !slices.Equal(got, []string{"Open channel", "Remove from group…"}) {
		t.Fatalf("channel ⋮ %q", got)
	}
	e.pick("Remove from group…")
	if x := e.texts(); !hasText(x, "Remove default channel?") {
		t.Errorf("confirmation %q", x)
	}
	e.click(e.button("Remove"))
	e.until("the toast", func() bool { return e.toasted("Channel removed from group.") })
	f := e.calls.get("usergroups.update")
	want := url.Values{"usergroup": {fakeslack.WorkspaceGroupID}, "channels": {"C0FAKE00001"}, "enable_section": {"false"}, "description": {""}}
	if len(f) != 1 || f[0].Encode() != want.Encode() {
		t.Errorf("removeUserGroupChannel\n got %v\nwant %v", f, want)
	}
	e.until("one channel left", func() bool { return len(e.v.g.DefaultChannels()) == 1 })
	e.click(e.pane("chmore:C0FAKE00001"))
	e.pick("Remove from group…")
	e.click(e.button("Remove"))
	e.until("no channels", func() bool { return len(e.v.g.DefaultChannels()) == 0 })
	if x := e.texts(); !hasText(x, "No default channels") || !hasText(x, "Add Default Channels") {
		t.Errorf("editor's empty channels %q", x)
	}
	e.click(e.pane("add-channels"))
	if _, ok := e.top().(*UserGroupEditModal); !ok {
		t.Errorf("Add Default Channels opened %T", e.top())
	}
}

// Deactivate group (the ⋮ menu, confirmed) sends usergroups.disable
// {usergroup, team_id}; the pane reads "(Deactivated)" with "Enable
// group", which sends usergroups.enable with the team.
func TestUserGroupPaneDeactivate(t *testing.T) {
	e := newUGRT(t, true, false, fakeslack.WorkspaceGroupID)
	e.ready(6)
	e.click(e.pane("options"))
	e.pick("Deactivate group")
	x := e.texts()
	if !hasText(x, "Deactivate Snack crew?") || !hasText(x, "Deactivate") {
		t.Errorf("confirmation %q", x)
	}
	e.shot("usergroup-deactivate")
	e.click(e.button("Deactivate"))
	e.until("the deactivated pane", func() bool { return e.v.g.Disabled })
	if f := e.calls.get("usergroups.disable"); len(f) != 1 || f[0].Get("usergroup") != fakeslack.WorkspaceGroupID || f[0].Get("team_id") != fakeslack.TeamID {
		t.Errorf("usergroups.disable %v", f)
	}
	x = e.texts()
	if !hasText(x, "Snack crew (Deactivated)") || !hasText(x, "Enable group") || hasText(x, "Edit Members") || hasText(x, "Members") {
		t.Errorf("deactivated pane %q", x)
	}
	e.shot("usergroup-deactivated")
	e.click(e.pane("enable"))
	e.until("the active pane", func() bool { return !e.v.g.Disabled })
	if f := e.calls.get("usergroups.enable"); len(f) != 1 || f[0].Get("team_id") != fakeslack.TeamID {
		t.Errorf("usergroups.enable %v", f)
	}
}

// The locked (auto-generated admins) group: the bar has Edit Details
// alone, a member's ⋮ has no Remove from group…, and Edit User Group
// says why the name and handle can't change.
func TestUserGroupPaneLocked(t *testing.T) {
	for _, dark := range []bool{false, true} {
		e := newUGRT(t, true, dark, fakeslack.AdminsGroupID)
		e.ready(2)
		x := e.texts()
		if !hasText(x, "Edit Details") || hasText(x, "Edit Members") || hasText(x, "Edit Channels") {
			t.Errorf("%s locked bar %q", e.mode, x)
		}
		e.shot("usergroup-locked-pane")
		e.click(e.pane("more:" + fakeslack.ProfileID))
		if got := e.menu().menu.Labels(); !slices.Equal(got, []string{"View profile"}) {
			t.Errorf("%s locked member ⋮ %q", e.mode, got)
		}
		e.menu().Dismiss()
		e.frame() // the menu and its scrim leave the hit areas
		e.click(e.pane("options"))
		if got := e.menu().menu.Labels(); !slices.Equal(got, []string{"Copy group ID", "Deactivate group"}) {
			t.Errorf("%s locked ⋮ %q", e.mode, got)
		}
		e.menu().Dismiss()
		e.frame() // the menu and its scrim leave the hit areas
		e.click(e.pane("edit-details"))
		m, ok := e.top().(*UserGroupEditModal)
		if !ok {
			t.Fatalf("%s Edit Details opened %T", e.mode, e.top())
		}
		x = e.texts()
		if !hasText(x, "This user group is automatically generated for Workspace Admins.") || !m.name.ReadOnly || !m.handle.ReadOnly {
			t.Errorf("%s locked dialog: read-only %v %v, %q", e.mode, m.name.ReadOnly, m.handle.ReadOnly, x)
		}
		e.shot("usergroup-locked-dialog")
	}
}

// An editor's empty group: "You can add a few people with the button
// below." and Add Members, which opens Edit members; a viewer's still
// says to contact an administrator.
func TestUserGroupPaneEmptyEditor(t *testing.T) {
	e := newUGRT(t, true, false, "S0FAKE00000")
	e.ready(0)
	x := e.texts()
	if !hasText(x, "Nobody’s in this group") || !hasText(x, "Add Members") || hasText(x, "workspace administrator") {
		t.Errorf("editor's empty group %q", x)
	}
	e.shot("usergroup-empty-editor")
	e.click(e.pane("add-members"))
	if _, ok := e.top().(*UserGroupMembersModal); !ok {
		t.Errorf("Add Members opened %T", e.top())
	}
	w := newUGRT(t, false, false, "S0FAKE00000")
	w.ready(0)
	if x := w.texts(); hasText(x, "Add Members") || !hasText(x, "workspace administrator") {
		t.Errorf("viewer's empty group %q", x)
	}
}

// An org-level group's header (the newer pane's UserGroupHeader) ends
// with ManageUserGroupLink: "This group is managed by administrators."
// for a viewer without the org dashboard's Groups page, "To manage this
// group, go to the Groups Dashboard." (a link with its new-window icon)
// for one with it, whose empty states point there too. Neither gets an
// editor action.
func TestUserGroupPaneOrgManagedAlert(t *testing.T) {
	for _, dark := range []bool{false, true} {
		e := newUGRT(t, false, dark, fakeslack.OrgGroupID)
		e.ready(4)
		x := e.texts()
		for _, want := range []string{"Org crew", "@orgcrew", "This", "group", "is", "managed", "by", "administrators.", "Find members"} {
			if !hasText(x, want) {
				t.Errorf("%s viewer: %q not drawn", e.mode, want)
			}
		}
		for _, bad := range append([]string{"Groups", "Dashboard"}, ugEditorTexts...) {
			if hasText(x, bad) {
				t.Errorf("%s viewer: %q drawn", e.mode, bad)
			}
		}
		e.shot("usergroup-org-managed")

		a := newUGRT(t, false, dark, fakeslack.OrgGroupID)
		a.fs.UsergroupOrgAdmin = true
		a.ready(4)
		x = a.texts()
		for _, want := range []string{"To", "manage", "go", "the", "Groups", "Dashboard", "."} {
			if !hasText(x, want) {
				t.Errorf("%s org admin: %q not drawn", a.mode, want)
			}
		}
		if hasText(x, "administrators.") {
			t.Errorf("%s org admin: the viewers' sentence drawn", a.mode)
		}
		for _, bad := range ugEditorTexts {
			if hasText(x, bad) {
				t.Errorf("%s org admin: %q drawn", a.mode, bad)
			}
		}
		if !a.v.rights.OrgDash || a.v.dashURL() != "https://app.slack.com/manage/"+fakeslack.EnterpriseID+"/people/groups/"+fakeslack.OrgGroupID+"/details" {
			t.Errorf("%s org admin: OrgDash %v, link %s", a.mode, a.v.rights.OrgDash, a.v.dashURL())
		}
		a.shot("usergroup-org-dashboard")
	}
}
