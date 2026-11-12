package ui

import (
	"image"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// organizeShotsDir is where these tests save their screenshots: the
// channels-audit workflow's PREVIEW_SHOTS_DIR or section-menus'
// SECTION_MENU_SHOTS_DIR.
func organizeShotsDir() string {
	if d := os.Getenv("PREVIEW_SHOTS_DIR"); d != "" {
		return d
	}
	return os.Getenv("SECTION_MENU_SHOTS_DIR")
}

// organizeEnv boots Clack over a small fake (setup runs on it first), with
// a GPU window when screenshots are wanted.
func organizeEnv(t *testing.T, setup func(fs *fakeslack.Server)) (*perfEnv, *fakeslack.Server) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	if setup != nil {
		setup(fs)
	}
	e := startEnv(t, fs, t.TempDir())
	if organizeShotsDir() != "" && e.gpu == nil {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	e.settle(300 * time.Millisecond)
	return e, fs
}

func (e *perfEnv) organizeShot(t *testing.T, name string) {
	t.Helper()
	e.settle(150 * time.Millisecond)
	e.shotTo(t, organizeShotsDir(), name)
}

// sbRowPoint is the window point in the middle of the sidebar row with
// item key k, scrolled into view first.
func (e *perfEnv) sbRowPoint(t *testing.T, k string) image.Point {
	t.Helper()
	s := &e.a.sidebar
	p := s.sbPos(k)
	if p < 0 {
		t.Fatalf("no sidebar row %q", k)
	}
	if !s.inView(p, 0) {
		s.scrollToPos(p, 0)
		e.frame()
	}
	h := s.kb.rowOff[p+1] - s.kb.rowOff[p]
	if r := s.rowAtPos(p); e.a.sbBBMBox(r) {
		h = e.a.sbRowH(e.context())
	}
	return s.origin.Add(image.Pt(90, s.listTop+s.kb.rowOff[p]-s.scrollTop()+h/2))
}

// clickWith clicks at p holding mods.
func (e *perfEnv) clickWith(p image.Point, mods key.Modifiers) {
	pt := f32.Pt(float32(p.X), float32(p.Y))
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt, Modifiers: mods},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt, Modifiers: mods},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt, Modifiers: mods},
	)
	e.frame()
	e.frame()
}

// sbConvs are the conversation rows of section sec as drawn.
func sbConvs(a *App, sec string) []string {
	var out []string
	for _, r := range a.sidebar.rows {
		if r.kind == sbConv && r.section == sec {
			out = append(out, r.key)
		}
	}
	return out
}

func inFakeSection(fs *fakeslack.Server, sec, ch string) bool {
	for _, s := range fs.Sections() {
		if s.ID == sec {
			return slices.Contains(s.Channels, ch)
		}
	}
	return false
}

// rowMenuMove opens ch's sidebar row menu and returns its Move ▸ rows.
func rowMenuMove(t *testing.T, a *App, ch string) []popups.MenuItem {
	t.Helper()
	var got []popups.MenuItem
	sbMenuTap = func(id string, items []popups.MenuItem) {
		if id == ch {
			got = items
		}
	}
	defer func() { sbMenuTap = nil }()
	a.openSidebarMenu(ch, image.Pt(120, 200), slackc.JSFunc{})
	for _, it := range got {
		if it.Key == "move" {
			return it.Sub
		}
	}
	t.Fatalf("no Move row in %s", menuLabelsP(got))
	return nil
}

// Organize mode, as Slack's sidebar has it (sidebar_organize.go): a row's
// Move channel ▸ ends with "Edit all sections"; in the mode the page
// links are disabled, every section is open with all its conversations and
// no footers, rows start with Slack's checkbox; a click picks a row,
// Shift+click a range; the bar counts the picks and offers Clear
// selection, Move to… (the multi-conversation Move to… menu: the picks'
// section checked, a move unpicks them, Undo picks them again), Leave
// (ConfirmLeaveChannelsModal for what can't be left, conversations.bulkLeave
// for the rest) and Done. Without bulk_leave_channels_from_sidebar there's
// no Leave, Move to… is primary and Done an outline "Done".
func TestSidebarOrganizeMode(t *testing.T) {
	e, fs := organizeEnv(t, nil)
	a := e.a
	c := a.Core()
	const from, to = "L0FAKE01", "L0FAKE03" // Hack Club, Events
	src := a.sectionByID(from)
	if src == nil || len(src.All) < 3 || a.sectionByID(to) == nil {
		t.Fatalf("fake sections: %+v", a.model().Sections)
	}
	if !a.bulkLeaveOn() {
		t.Fatal("bulk_leave_channels_from_sidebar isn't on for the fake account")
	}
	ch := src.All[0]

	// the row menu's way in
	move := rowMenuMove(t, a, ch)
	if n := len(move); n < 2 || move[n-1].Label != "Edit all sections" || !move[n-2].Separator {
		t.Fatalf("Move channel ▸ doesn't end with a separator and Edit all sections: %s", menuLabelsP(move))
	}
	e.organizeShot(t, "organize-row-menu")
	a.dismissTop()
	e.frame()
	move[len(move)-1].Do()
	e.frame()
	e.frame()
	if !a.sbEditMode() {
		t.Fatal("Edit all sections didn't enter organize mode")
	}
	if k := a.sidebar.kb.key; k != "sectionHeading-"+from {
		t.Errorf("the focus went to %q, want the row's section heading", k)
	}
	for _, r := range a.sidebar.rows {
		switch {
		case r.kind == sbLink && !r.disabled:
			t.Errorf("page link %s isn't disabled in organize mode", r.key)
		case r.kind == sbAction || r.kind == sbNote:
			t.Errorf("organize mode kept the %q row", r.key+r.label)
		}
	}
	m := a.model()
	for i := range m.Sections {
		sec := &m.Sections[i]
		if got, want := sbConvs(a, sec.ID), sbEditRows(m, sec, a.exp("improve_mute_pref") == "on"); !slices.Equal(got, want) {
			t.Errorf("section %s in organize mode:\n got %v\nwant %v", sec.Name, got, want)
		}
	}
	e.organizeShot(t, "organize-mode")

	// a click picks, Shift+click a range from it
	rows := sbConvs(a, from)
	e.clickWith(e.sbRowPoint(t, rows[0]), 0)
	e.clickWith(e.sbRowPoint(t, rows[2]), key.ModShift)
	if got := a.sbPicked(); len(got) != 3 || !slices.Contains(got, rows[1]) {
		t.Fatalf("picks after click + Shift+click: %v, want %v", got, rows[:3])
	}
	if r := string(a.router.Current()); strings.HasPrefix(r, "c/") && slices.Contains(rows[:3], r[2:]) {
		t.Error("a click in organize mode opened the conversation")
	}
	e.organizeShot(t, "organize-picked")

	// Move to…: the picks' section is checked, Events moves them there
	items := a.moveToMenuItems(a.sbPicked(), moveMenuOpts{})
	var hackClub, events MenuItem
	for _, it := range items {
		switch it.Label {
		case "Hack Club":
			hackClub = it
		case "Events":
			events = it
		}
	}
	if !hackClub.Checked || hackClub.Indeterminate || events.Checked {
		t.Errorf("Move to… radios: Hack Club %+v, Events %+v", hackClub, events)
	}
	a.openOrganizeMove()
	e.organizeShot(t, "organize-move-menu")
	a.dismissTop()
	e.frame()
	a.toast = toastState{}
	picked := a.sbPicked()
	events.Do()
	if len(a.sbPicked()) != 0 {
		t.Errorf("a move left the moved picks picked: %v", a.sbPicked())
	}
	e.until("moved into Events", 10*time.Second, func() bool {
		for _, id := range picked {
			if !inFakeSection(fs, to, id) || inFakeSection(fs, from, id) {
				return false
			}
		}
		return true
	})
	e.until("the move's toast", 5*time.Second, func() bool { return a.toast.undo != nil })
	if got, want := a.toast.text, "Updated conversations in Events."; got != want {
		t.Errorf("toast %q, want %q", got, want)
	}
	a.undoToast()
	if got := a.sbPicked(); len(got) != len(picked) {
		t.Errorf("Undo picked %v again, want %v", got, picked)
	}
	e.until("undone", 10*time.Second, func() bool { return inFakeSection(fs, from, picked[0]) && !inFakeSection(fs, to, picked[0]) })

	// Leave: a DM can't be left (the modal names it), the channel goes in
	// one conversations.bulkLeave
	a.sbClearSelection()
	e.settle(300 * time.Millisecond)
	var dm string
	for _, r := range a.sidebar.rows {
		if cv := a.model().conv(r.key); r.kind == sbConv && cv != nil && cv.Kind == "im" && !cv.Self {
			dm = r.key
			break
		}
	}
	var leaveCh string
	for _, id := range sbConvs(a, from) {
		if cv := a.model().conv(id); cv != nil && cv.Kind == "channel" && cv.Name != "general" && !cv.Archived {
			leaveCh = id
		}
	}
	if dm == "" || leaveCh == "" {
		t.Fatalf("no DM (%q) or channel (%q) to pick", dm, leaveCh)
	}
	a.sbEditClick(leaveCh, false)
	a.sbEditClick(dm, false)
	e.frame()
	if cannot := c.ChannelsThatCannotBeLeft(a.sbPicked()); !slices.Equal(cannot, []string{dm}) {
		t.Errorf("can't be left: %v, want the DM", cannot)
	}
	a.organizeLeave()
	e.frame()
	if a.sbEditMode() {
		t.Error("Leave didn't end organize mode")
	}
	modal, ok := topOverlay[*popups.Modal](a)
	if !ok {
		t.Fatal("Leave with a DM picked showed no ConfirmLeaveChannelsModal")
	}
	e.organizeShot(t, "organize-leave-confirm")
	if n := len(fs.BulkLeaveCalls()); n != 0 {
		t.Fatalf("left before Continue (%d calls)", n)
	}
	modal.PressDefault() // Continue
	e.until("conversations.bulkLeave", 10*time.Second, func() bool { return len(fs.BulkLeaveCalls()) == 1 })
	if got := fs.BulkLeaveCalls()[0]; !slices.Equal(got, []string{leaveCh}) {
		t.Errorf("bulkLeave channels %v, want only %s", got, leaveCh)
	}
	if !strings.HasPrefix(a.toast.text, "We’re tidying up …") {
		t.Errorf("toast %q, want LeaveChannelsInProgressToast's", a.toast.text)
	}
	e.until("the channel leaves the sidebar", 10*time.Second, func() bool { return a.model().conv(leaveCh) == nil || !e.sidebarHas(leaveCh) })

	// Done puts the list back
	a.sbEnterEditMode(from)
	e.frame()
	e.frame()
	e.organizeShot(t, "organize-mode-done-before")
	a.sbExitEditMode()
	e.frame()
	if a.sbEditMode() {
		t.Error("Done didn't end organize mode")
	}
	for _, r := range a.sidebar.rows {
		if r.disabled {
			t.Errorf("row %s still disabled after Done", r.key)
		}
	}

	// without bulk_leave_channels_from_sidebar
	c.SetExperimentGroup("bulk_leave_channels_from_sidebar", "off")
	e.settle(200 * time.Millisecond)
	a.sbEnterEditMode(from)
	e.frame()
	a.sbEditClick(sbConvs(a, from)[0], false)
	e.organizeShot(t, "organize-mode-leave-off")
	if a.bulkLeaveOn() {
		t.Error("the experiment flip didn't take")
	}
	a.sbExitEditMode()
	c.ClearExperimentGroup("bulk_leave_channels_from_sidebar")
}

// The sidebar keyboard (sidebar_keys.go): the filter's ArrowDown focuses
// the first row with the ring; Down / Up move over the treeitems only
// (spacers and the divider skipped); Enter on a heading collapses it,
// Right opens it again; Left from a conversation goes to its heading;
// Shift+Tab with sidebar_text_filter goes back to the input; a press
// takes the focus without the ring.
func TestSidebarKeyboard(t *testing.T) {
	e, _ := organizeEnv(t, nil)
	a := e.a
	s := &a.sidebar
	e.click(filterPoint())
	e.until("filter focused", 5*time.Second, func() bool { return s.filter.focused })
	e.key(key.NameDownArrow, 0)
	e.frame()
	e.frame()
	e.frame()
	first := ""
	for p := range s.kb.order {
		if r := s.rowAtPos(p); sbFocusable(r) {
			first = r.itemKey
			break
		}
	}
	if !s.kb.focused || s.kb.key != first || !s.kb.ring {
		t.Fatalf("ArrowDown in the filter: focused %v, key %q (want %q), ring %v", s.kb.focused, s.kb.key, first, s.kb.ring)
	}
	e.organizeShot(t, "sidebar-keys-first-row")

	// Down walks the treeitems in order
	var want []string
	for p := range s.kb.order {
		if r := s.rowAtPos(p); sbFocusable(r) {
			want = append(want, r.itemKey)
		}
	}
	for i := 1; i < 8 && i < len(want); i++ {
		e.key(key.NameDownArrow, 0)
		e.frame()
		if s.kb.key != want[i] {
			t.Fatalf("Down #%d went to %q, want %q", i, s.kb.key, want[i])
		}
	}
	e.key(key.NameUpArrow, 0)
	e.frame()
	if s.kb.key != want[6] {
		t.Errorf("Up went to %q, want %q", s.kb.key, want[6])
	}

	// a conversation: Left goes to its heading
	var conv *sbRow
	for i := range s.rows {
		if s.rows[i].kind == sbConv {
			conv = &s.rows[i]
			break
		}
	}
	s.setActive(s.sbPos(conv.itemKey))
	e.key(key.NameLeftArrow, 0)
	e.frame()
	head := "sectionHeading-" + conv.section
	if s.kb.key != head {
		t.Errorf("Left from a conversation went to %q, want %q", s.kb.key, head)
	}
	// Enter on the heading collapses it, Right opens it again
	e.key(key.NameReturn, 0)
	e.frame()
	if !s.collapsed[conv.section] {
		t.Error("Enter didn't collapse the heading")
	}
	e.organizeShot(t, "sidebar-keys-heading")
	e.key(key.NameRightArrow, 0)
	e.frame()
	if s.collapsed[conv.section] {
		t.Error("Right didn't open the heading")
	}
	// Enter on a conversation opens it
	s.setActive(s.sbPos(conv.itemKey))
	e.key(key.NameReturn, 0)
	e.frame()
	if r := string(a.router.Current()); r != "c/"+conv.key {
		t.Errorf("Enter on a conversation opened %q", r)
	}

	// Shift+Tab goes back to the filter input
	e.click(e.sbRowPoint(t, head))
	e.frame()
	if s.kb.ring {
		t.Error("a press left the focus ring on")
	}
	s.setActive(s.sbPos(head))
	e.key(key.NameTab, key.ModShift)
	e.frame()
	e.frame()
	if !s.filter.focused {
		t.Error("Shift+Tab didn't go back to the filter input")
	}
}

// performBulkChannelMove's errors (enqueueChannelSectionErrorToast): a
// section over its 500 conversations says so; too_many_operations (and
// anything else) is the generic "Something went wrong." with its link;
// both in the error Toast with the warning icon.
func TestSectionMoveErrorToasts(t *testing.T) {
	e, fs := organizeEnv(t, nil)
	a := e.a
	var mu sync.Mutex
	code := "too_many_channels"
	fs.SetHook(func(method string, _ url.Values) (any, bool) {
		if method != "users.channelSections.channels.bulkUpdate" {
			return nil, false
		}
		mu.Lock()
		defer mu.Unlock()
		return map[string]any{"ok": false, "error": code}, true
	})
	src := a.sectionByID("L0FAKE01")
	if src == nil || len(src.All) == 0 {
		t.Fatal("no fake sections")
	}
	ch := src.All[0]
	// a different move each time: slack.Client answers a request that just
	// failed the same way from its backoff, without asking again
	for _, tc := range []struct{ code, to, want string }{
		{"too_many_channels", "L0FAKE03", "Unfortunately, you can have up to 500 conversations in a section \u2014\u00a0but no more."},
		{"too_many_operations", "L0FAKE02", "Something went wrong. Reloading Slack might clear things up. If it doesn’t, drop us a line."},
	} {
		mu.Lock()
		code = tc.code
		mu.Unlock()
		dst := a.sectionByID(tc.to)
		if dst == nil {
			t.Fatalf("no section %s", tc.to)
		}
		a.toast = toastState{}
		a.moveChannelTo(ch, dst, a.channelSections(ch))
		e.until(tc.code+" toast", 10*time.Second, func() bool { return a.toast.text != "" })
		if a.toast.text != tc.want {
			t.Errorf("%s: toast %q, want %q", tc.code, a.toast.text, tc.want)
		}
		if a.toast.icon != "warning-filled" || !a.toast.rich {
			t.Errorf("%s: not the error Toast (icon %q)", tc.code, a.toast.icon)
		}
		e.organizeShot(t, "section-move-error-"+tc.code)
	}
}

// temp_mute_new_badge: the megaphone's generic_new_badge space (the boot
// load's second chunk) tags the bell's More options "New"; opening the
// submenu clears it.
func TestTempMuteNewBadge(t *testing.T) {
	e, _ := organizeEnv(t, func(fs *fakeslack.Server) { fs.SetNewBadges(popups.TempMuteNewBadge) })
	a := e.a
	c := a.Core()
	// the boot load goes out once a conversation is on screen (joinbanner.go)
	ch := fakeslackMemberChannel(e)
	e.openChannel(ch)
	e.until("the badge", 10*time.Second, func() bool { return c.MegaphoneNewBadge(popups.TempMuteNewBadge) })
	e.settle(200 * time.Millisecond)
	more := notifyPick(t, popups.ChannelNotifyItems(a, ch), "More options")
	if more.Tag == nil || more.Tag.Label != "New" || more.Tag.Style != "inverse-education" || more.Tag.HighStyle != "education" {
		t.Fatalf("More options' tag %+v, want EducationTag's New", more.Tag)
	}
	a.openNotifPrefs(ch)
	e.organizeShot(t, "bell-temp-mute-new")
	a.dismissTop()
	e.frame()
	if more.OnSubOpen == nil {
		t.Fatal("no onOpen on More options")
	}
	more.OnSubOpen()
	if c.MegaphoneNewBadge(popups.TempMuteNewBadge) {
		t.Error("opening More options didn't clear the badge")
	}
	if more := notifyPick(t, popups.ChannelNotifyItems(a, ch), "More options"); more.Tag != nil {
		t.Errorf("the tag is still there: %+v", more.Tag)
	}
	// without temp_mute there's no More options to tag
	c.SetExperimentGroup(core.ExpTempMute, "off")
	defer c.ClearExperimentGroup(core.ExpTempMute)
	for _, it := range popups.ChannelNotifyItems(a, ch) {
		if it.Tag != nil && it.Tag.Label == "New" {
			t.Error("a New tag without temp_mute")
		}
	}
}
