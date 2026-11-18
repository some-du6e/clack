package ui

import (
	"image"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
	"github.com/some-du6e/clack/internal/ui/views"
)

// The Members tab's row menu (views/details_agents_menu.go,
// core/channel_managers_edit.go, popups/channel_managers.go) on
// fakeslack's #agents-lab (public) and #no-agents (private here), whose
// people are the viewer, Robin, Avery Admin (the channels' manager), the
// guest Gale and a few others. Slack's IaDetailsPopoverMemberItemActionsMenu
// (build 846030e1ce97) shows a "…" on the hovered row with, as the
// viewer's grants allow, Remove from Channel Managers, Make Channel
// Manager and Remove from channel; the Channel Manager items go through
// admin.roles.addMembers / removeMembers, the last through
// conversations.kick.

// Channel grants (permissions/info) the tests hand out. CAN_AT_CHANNEL
// is always there, so its arrival says the answer came.
var (
	mrmManage       = []string{"CAN_AT_CHANNEL", "MANAGE_CHANNEL_TABS", core.PermAssignChannelManager, core.PermRemoveFromChannel}
	mrmKickOnly     = []string{"CAN_AT_CHANNEL", "MANAGE_CHANNEL_TABS", core.PermRemoveFromChannel}
	mrmAssignOnly   = []string{"CAN_AT_CHANNEL", "MANAGE_CHANNEL_TABS", core.PermAssignChannelManager}
	mrmNone         = []string{"CAN_AT_CHANNEL", "MANAGE_CHANNEL_TABS"}
	mrmPrivateKick  = []string{"CAN_AT_CHANNEL", "MANAGE_CHANNEL_TABS", core.PermAssignChannelManager, core.PermRemoveFromPrivateChannel}
	mrmTeamGrants   = []string{"CAN_AT_CHANNEL", "CAN_AT_EVERYONE", "UPDATE_NAME", "UPDATE_DISPLAY_NAME", "EDIT_PROFILE_PHOTO"}
	mrmPublicID     = fakeslack.ChannelAgentsID
	mrmPrivateID    = fakeslack.ChannelNoAgentsID
	mrmRobin        = fakeslack.ProfileID
	mrmAvery        = fakeslack.AdminID
	mrmGale         = fakeslack.ChannelGuestID
	mrmMakeLabel    = "Make Channel Manager"
	mrmUnmakeLabel  = "Remove from Channel Managers"
	mrmKickLabel    = "Remove from channel"
	mrmDialogWidth  = 580 // views' cdWidth at 1440×900
	mrmButtonRight  = 16  // …__members_list_item_action_menu: right 16px
	mrmButtonSize   = 28  // c-icon_button--size_small
	mrmSecondaryEnd = 24  // the secondary content's margin-right
)

// memberMenuEnv starts the client on the ChannelAgents fixture
// (#no-agents private) under the account's agents_in_channels_improvements
// and agent_channel_header, with Avery managing both channels and the
// viewer holding pub in #agents-lab and priv in #no-agents; shots gives
// it the headless window PREVIEW_SHOTS_DIR asks for (one per test).
func memberMenuEnv(t *testing.T, pub, priv []string, shots bool) (*perfEnv, *fakeslack.Server) {
	t.Helper()
	sz := fakeslack.Small
	sz.ChannelAgents, sz.ChannelAgentsPrivate = true, true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	fs.Permissions = map[string][]string{"": mrmTeamGrants, mrmPublicID: pub, mrmPrivateID: priv}
	fs.ChannelManagers = map[string][]string{mrmPublicID: {mrmAvery}, mrmPrivateID: {mrmAvery}}
	e := startEnv(t, fs, t.TempDir())
	if dir := os.Getenv("PREVIEW_SHOTS_DIR"); shots && dir != "" && e.gpu == nil {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	e.a.Core().SetExperimentGroup(core.ExpAgentsInChannelsImprovements, "on")
	e.a.Core().SetExperimentGroup(core.ExpAgentChannelHeader, "on")
	return e, fs
}

// memberMenuTab opens ch's details on Members and waits for the roster,
// the managers read and the channel's grants.
func memberMenuTab(t *testing.T, e *perfEnv, ch string) *views.ChannelDetails {
	t.Helper()
	d := openAgentsDetails(t, e, ch)
	d.ShowTab("Members")
	e.until("the members and their managers", 10*time.Second, func() bool {
		e.frame()
		return slices.Contains(d.MembersShown(), mrmRobin) && d.ManagersLoaded()
	})
	e.until("the channel's grants", 10*time.Second, func() bool {
		e.frame()
		return e.a.Core().ChannelGrant(ch, "CAN_AT_CHANNEL")
	})
	e.settle(300 * time.Millisecond)
	return d
}

// mrmDialog is the details dialog's x range in the window.
func mrmDialog(e *perfEnv) (x0, x1 int) {
	w := min(mrmDialogWidth, e.size.X-56)
	x0 = (e.size.X - w) / 2
	return x0, x0 + w
}

// hoverRow scrolls member id's row into view (the roster sorts by name:
// Robin is below the fold at 1440×900), moves the pointer onto it and
// returns a point on it (its middle is 28px below).
func hoverRow(t *testing.T, e *perfEnv, d *views.ChannelDetails, id string) image.Point {
	t.Helper()
	if !d.ScrollMembersTo(id) {
		t.Fatalf("the roster doesn't list %s: %v", id, d.MembersShown())
	}
	e.settle(150 * time.Millisecond)
	row, _ := d.MemberRowClickables(id)
	x0, x1 := mrmDialog(e)
	at, ok := e.scanHover(row, image.Pt((x0+x1)/2, 0), image.Pt((x0+x1)/2, e.size.Y-1))
	if !ok {
		t.Fatalf("%s's row never took the hover", id)
	}
	e.frame()
	return at
}

// openRowMenu hovers member id's row, clicks its "…" and returns the
// menu.
func openRowMenu(t *testing.T, e *perfEnv, d *views.ChannelDetails, id string) *popups.Menu {
	t.Helper()
	at := hoverRow(t, e, d, id)
	_, btn := d.MemberRowClickables(id)
	y := at.Y + 28
	bp, ok := e.scanHover(btn, image.Pt(at.X, y), image.Pt(e.size.X-1, y))
	if !ok {
		t.Fatalf("no \"…\" on %s's hovered row", id)
	}
	e.click(bp)
	e.frame()
	e.frame()
	m := e.topMenu()
	if m == nil {
		t.Fatalf("%s's \"…\" opened no menu", id)
	}
	return m
}

// pickRow clicks the menu's row label.
func pickRow(t *testing.T, e *perfEnv, m *popups.Menu, label string) {
	t.Helper()
	rr := m.RowRect(label)
	if rr.Empty() {
		t.Fatalf("no %q in the menu %q", label, m.Labels())
	}
	e.click(rr.Min.Add(rr.Size().Div(2)))
	e.frame()
	e.frame()
}

// clickModalButton clicks b where the modal drew it.
func clickModalButton(t *testing.T, e *perfEnv, b *popups.ModalButton) {
	t.Helper()
	e.until(b.Label+" drawn", 5*time.Second, func() bool { e.frame(); return !b.Rect().Empty() })
	r := b.Rect()
	e.click(r.Min.Add(r.Size().Div(2)))
	e.frame()
}

// The "…" isn't there until the row is hovered; then it sits 16px in from
// the row's right edge, and a click on it opens the menu below it
// (bottom-right) without opening the row's profile. A right-click on a
// row opens nothing: Slack's members list passes its list no
// onItemContextMenu.
func TestMemberRowMenuHover(t *testing.T) {
	e, _ := memberMenuEnv(t, mrmManage, mrmPrivateKick, true)
	d := memberMenuTab(t, e, mrmPublicID)
	if d.MemberRowMenuShown(mrmRobin) {
		t.Error("the \"…\" shows before the row is hovered")
	}
	at := hoverRow(t, e, d, mrmRobin)
	if !d.MemberRowMenuShown(mrmRobin) {
		t.Fatal("the hovered row has no \"…\"")
	}
	e.darkShot("member-row-menu-hover")

	_, btn := d.MemberRowClickables(mrmRobin)
	y := at.Y + 28
	bp, ok := e.scanHover(btn, image.Pt(at.X, y), image.Pt(e.size.X-1, y))
	if !ok {
		t.Fatal("no \"…\" under the pointer along the row")
	}
	e.click(bp)
	e.frame()
	e.frame()
	m := e.topMenu()
	if m == nil {
		t.Fatal("the \"…\" opened no menu")
	}
	if got := m.Labels(); !slices.Equal(got, []string{mrmMakeLabel, mrmKickLabel}) {
		t.Errorf("Robin's menu %q", got)
	}
	if e.a.secondary.kind == secProfile {
		t.Error("the button's click also opened the row's profile")
	}
	anchor := d.MemberRowMenuAnchor(mrmRobin)
	_, x1 := mrmDialog(e)
	want := image.Rect(x1-mrmButtonRight-mrmButtonSize, anchor.Min.Y, x1-mrmButtonRight, anchor.Min.Y+mrmButtonSize)
	if anchor != want || !bp.In(anchor) {
		t.Errorf("the \"…\" at %v (clicked %v), want %v", anchor, bp, want)
	}
	if r := m.Rect(); r.Min.Y != anchor.Max.Y || r.Max.X != anchor.Max.X {
		t.Errorf("menu at %v, want bottom-right of %v", r, anchor)
	}
	e.darkShot("member-row-menu-open")
	e.closeMenu()
	e.settle(200 * time.Millisecond)

	// a right-click on Gale's row: no menu, no profile
	gale := hoverRow(t, e, d, mrmGale)
	e.rightClick(image.Pt(gale.X, gale.Y+28))
	e.settle(200 * time.Millisecond)
	if m := e.topMenu(); m != nil {
		t.Errorf("a right-click opened a menu %q", m.Labels())
	}
	if e.a.secondary.kind == secProfile {
		t.Error("a right-click opened the row's profile")
	}
}

// Which items each row gets, per the viewer's grants and the channel's
// type: Remove from Channel Managers for the manager, Make Channel
// Manager for the others but the guest, and Remove from channel where the
// kick grant for the channel's kind (REMOVE_FROM_CHANNEL public,
// REMOVE_FROM_PRIVATE_CHANNEL private) allows, never on the viewer's own
// row. No item, no button.
func TestMemberRowMenuItemsByRole(t *testing.T) {
	self := fakeslack.SelfID
	for _, c := range []struct {
		name  string
		pub   []string
		priv  []string
		ch    string
		items map[string][]string
	}{
		{"public/manage", mrmManage, mrmNone, mrmPublicID, map[string][]string{
			mrmAvery: {mrmUnmakeLabel, mrmKickLabel},
			mrmRobin: {mrmMakeLabel, mrmKickLabel},
			mrmGale:  {mrmKickLabel},
			self:     {mrmMakeLabel},
		}},
		{"public/kick-only", mrmKickOnly, mrmNone, mrmPublicID, map[string][]string{
			mrmAvery: {mrmKickLabel},
			mrmRobin: {mrmKickLabel},
			mrmGale:  {mrmKickLabel},
			self:     nil,
		}},
		{"public/assign-only", mrmAssignOnly, mrmNone, mrmPublicID, map[string][]string{
			mrmAvery: {mrmUnmakeLabel},
			mrmRobin: {mrmMakeLabel},
			mrmGale:  nil,
			self:     {mrmMakeLabel},
		}},
		{"public/none", mrmNone, mrmNone, mrmPublicID, map[string][]string{
			mrmAvery: nil, mrmRobin: nil, mrmGale: nil, self: nil,
		}},
		// a private channel: REMOVE_FROM_CHANNEL doesn't kick from it
		{"private/public-kick", mrmNone, mrmManage, mrmPrivateID, map[string][]string{
			mrmAvery: {mrmUnmakeLabel},
			mrmRobin: {mrmMakeLabel},
			mrmGale:  nil,
		}},
		{"private/private-kick", mrmNone, mrmPrivateKick, mrmPrivateID, map[string][]string{
			mrmAvery: {mrmUnmakeLabel, mrmKickLabel},
			mrmRobin: {mrmMakeLabel, mrmKickLabel},
			mrmGale:  {mrmKickLabel},
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e, _ := memberMenuEnv(t, c.pub, c.priv, false)
			d := memberMenuTab(t, e, c.ch)
			for id, want := range c.items {
				if got := d.MemberRowMenuItems(id); !slices.Equal(got, want) {
					t.Errorf("%s: %q, want %q", id, got, want)
				}
			}
			// the manager's pill whatever the viewer may do
			hoverRow(t, e, d, mrmAvery)
			if pill, _ := d.MemberRowPill(mrmAvery); pill.Empty() {
				t.Error("Avery's row has no Channel Manager pill")
			}
			// what the row draws on hover follows the items
			hoverRow(t, e, d, mrmRobin)
			if shown, want := d.MemberRowMenuShown(mrmRobin), len(c.items[mrmRobin]) > 0; shown != want {
				t.Errorf("Robin's hovered row: \"…\" %v, want %v", shown, want)
			}
		})
	}
}

// Make Channel Manager: MakeChannelManagerModal (no close button), whose
// button sends admin.roles.addMembers {role_id Rl0A, role_scopes
// [channel], user_ids [member], reason add-channel-managers} through the
// fake, closes, and the row gets the Channel Manager pill (placed as
// Chrome lays out Slack's flex rules: the pill keeps its width past the
// secondary content, the "…" paints over its end) and the Remove item.
func TestMemberRowMenuMakeManager(t *testing.T) {
	e, fs := memberMenuEnv(t, mrmManage, mrmPrivateKick, true)
	d := memberMenuTab(t, e, mrmPublicID)
	if pill, _ := d.MemberRowPill(mrmRobin); !pill.Empty() {
		t.Fatal("Robin has a pill before being made a manager")
	}
	m := openRowMenu(t, e, d, mrmRobin)
	pickRow(t, e, m, mrmMakeLabel)
	dlg, ok := topOverlay[*popups.ChannelManagerModal](e.a)
	if !ok {
		t.Fatalf("no MakeChannelManagerModal; overlays %v", e.overlayTypes())
	}
	if dlg.Heading != "Make Robin Example a Channel Manager of #agents-lab?" ||
		dlg.Text != "This person will be able to manage all aspects of the channel." || dlg.Go.Label != mrmMakeLabel {
		t.Errorf("modal %q / %q / %q", dlg.Heading, dlg.Text, dlg.Go.Label)
	}
	e.settle(200 * time.Millisecond)
	e.darkShot("member-row-make-manager")
	clickModalButton(t, e, dlg.Go)
	e.until("admin.roles.addMembers", 10*time.Second, func() bool { e.frame(); return len(fs.ManagerWrites()) > 0 })
	w := fs.ManagerWrites()[0]
	if w.Method != "admin.roles.addMembers" || w.RoleID != "Rl0A" || !slices.Equal(w.Scopes, []string{mrmPublicID}) ||
		!slices.Equal(w.Users, []string{mrmRobin}) || w.Reason != "add-channel-managers" {
		t.Errorf("write %+v", w)
	}
	e.until("the modal closes", 10*time.Second, func() bool { e.frame(); return dlg.Dismissed() })
	e.until("Robin's pill", 10*time.Second, func() bool {
		e.frame()
		pill, _ := d.MemberRowPill(mrmRobin)
		return !pill.Empty()
	})
	if got := fs.Managers(mrmPublicID); !slices.Contains(got, mrmRobin) {
		t.Errorf("the fake's managers %v", got)
	}
	if got := d.MemberRowMenuItems(mrmRobin); !slices.Equal(got, []string{mrmUnmakeLabel, mrmKickLabel}) {
		t.Errorf("Robin's menu after %q", got)
	}
	// the pill's place: entity and secondary content shrink in proportion
	// to their basis; the pill (flex-shrink 0) overhangs the secondary
	pill, row := d.MemberRowPill(mrmRobin)
	wc := row.X - 32
	p := pill.Dx()
	wantX := 16 + wc - mrmSecondaryEnd - p*(wc-mrmSecondaryEnd)/(wc+p)
	if row.X != mrmDialogWidth || pill.Min.X != wantX || pill.Min.Y != (row.Y-pill.Dy())/2 {
		t.Errorf("pill %v in a %v row, want x %d, centred", pill, row, wantX)
	}
	if pill.Max.X <= row.X-mrmButtonRight-mrmButtonSize {
		t.Errorf("pill ends at %d, left of the \"…\" (%d): Slack's pill runs under it", pill.Max.X, row.X-mrmButtonRight-mrmButtonSize)
	}
	hoverRow(t, e, d, mrmRobin)
	e.darkShot("member-row-manager-pill")
}

// A refused write keeps MakeChannelManagerModal open with its alert above
// the line, and the row stays as it was.
func TestMemberRowMenuMakeManagerFails(t *testing.T) {
	e, fs := memberMenuEnv(t, mrmManage, mrmPrivateKick, true)
	fs.SetFault("admin.roles.addMembers", fakeslack.Fault{Error: "not_permitted"})
	d := memberMenuTab(t, e, mrmPublicID)
	m := openRowMenu(t, e, d, mrmRobin)
	pickRow(t, e, m, mrmMakeLabel)
	dlg, ok := topOverlay[*popups.ChannelManagerModal](e.a)
	if !ok {
		t.Fatal("no MakeChannelManagerModal")
	}
	clickModalButton(t, e, dlg.Go)
	want := "There was a problem making this person a Channel Manager. Give it another try in a few minutes."
	e.until("the alert", 10*time.Second, func() bool { e.frame(); return dlg.Error() != "" })
	if dlg.Error() != want || dlg.Dismissed() || dlg.Pending() {
		t.Errorf("alert %q (dismissed %v, pending %v), want %q", dlg.Error(), dlg.Dismissed(), dlg.Pending(), want)
	}
	e.settle(200 * time.Millisecond)
	e.darkShot("member-row-make-manager-error")
	if pill, _ := d.MemberRowPill(mrmRobin); !pill.Empty() {
		t.Error("a refused write gave Robin the pill")
	}
	if slices.Contains(fs.Managers(mrmPublicID), mrmRobin) || slices.Contains(e.a.Core().ChannelManagerIDs(mrmPublicID), mrmRobin) {
		t.Error("a refused write made Robin a manager")
	}
}

// Remove from Channel Managers: RemoveManagerModal, whose "Remove From
// Channel Managers" sends admin.roles.removeMembers and takes the pill
// away; Make Channel Manager comes back.
func TestMemberRowMenuRemoveManager(t *testing.T) {
	e, fs := memberMenuEnv(t, mrmManage, mrmPrivateKick, true)
	d := memberMenuTab(t, e, mrmPublicID)
	m := openRowMenu(t, e, d, mrmAvery)
	if got := m.Labels(); !slices.Equal(got, []string{mrmUnmakeLabel, mrmKickLabel}) {
		t.Errorf("Avery's menu %q", got)
	}
	pickRow(t, e, m, mrmUnmakeLabel)
	dlg, ok := topOverlay[*popups.ChannelManagerModal](e.a)
	if !ok {
		t.Fatalf("no RemoveManagerModal; overlays %v", e.overlayTypes())
	}
	if dlg.Heading != "Remove Avery Admin from Channel Managers of #agents-lab?" ||
		dlg.Text != "This person won’t be able to manage the channel anymore. " || dlg.Go.Label != "Remove From Channel Managers" {
		t.Errorf("modal %q / %q / %q", dlg.Heading, dlg.Text, dlg.Go.Label)
	}
	e.settle(200 * time.Millisecond)
	e.darkShot("member-row-remove-manager")
	clickModalButton(t, e, dlg.Go)
	e.until("admin.roles.removeMembers", 10*time.Second, func() bool { e.frame(); return len(fs.ManagerWrites()) > 0 })
	w := fs.ManagerWrites()[0]
	if w.Method != "admin.roles.removeMembers" || w.RoleID != "Rl0A" || !slices.Equal(w.Scopes, []string{mrmPublicID}) ||
		!slices.Equal(w.Users, []string{mrmAvery}) || w.Reason != "remove-channel-managers" {
		t.Errorf("write %+v", w)
	}
	e.until("the modal closes", 10*time.Second, func() bool { e.frame(); return dlg.Dismissed() })
	e.until("Avery's pill goes", 10*time.Second, func() bool {
		e.frame()
		pill, _ := d.MemberRowPill(mrmAvery)
		return pill.Empty()
	})
	if got := d.MemberRowMenuItems(mrmAvery); !slices.Equal(got, []string{mrmMakeLabel, mrmKickLabel}) {
		t.Errorf("Avery's menu after %q", got)
	}
	if got := fs.Managers(mrmPublicID); slices.Contains(got, mrmAvery) {
		t.Errorf("the fake's managers %v", got)
	}
}

// Remove from channel: RemoveMemberDialog, whose Remove kicks Robin out
// (conversations.kick) and takes the row away; in the private channel the
// dialog's line is the private one.
func TestMemberRowMenuRemoveFromChannel(t *testing.T) {
	e, fs := memberMenuEnv(t, mrmManage, mrmPrivateKick, true)
	d := memberMenuTab(t, e, mrmPublicID)
	m := openRowMenu(t, e, d, mrmRobin)
	pickRow(t, e, m, mrmKickLabel)
	dlg, ok := topOverlay[*popups.RemoveMemberDialog](e.a)
	if !ok {
		t.Fatalf("no RemoveMemberDialog; overlays %v", e.overlayTypes())
	}
	if c := dlg.Copy; !strings.HasSuffix(c.Title, " from #agents-lab?") ||
		c.Para != "They’ll still be able to rejoin, or be added to the conversation." || c.Go != "Remove" {
		t.Errorf("dialog %+v", c)
	}
	e.settle(200 * time.Millisecond)
	e.darkShot("member-row-remove-dialog")
	if !dlg.Press("Remove") {
		t.Fatal("no Remove button")
	}
	e.until("conversations.kick", 10*time.Second, func() bool {
		e.frame()
		return slices.Contains(fs.Kicked(mrmPublicID), mrmRobin)
	})
	e.until("the dialog closes", 5*time.Second, func() bool { e.frame(); return dlg.Dismissed() })
	e.until("Robin leaves the roster", 10*time.Second, func() bool {
		e.frame()
		return !slices.Contains(d.MembersShown(), mrmRobin)
	})
	if !slices.Contains(d.MembersShown(), mrmAvery) {
		t.Errorf("the roster lost the others: %v", d.MembersShown())
	}

	// the private channel: the private line
	d.Escape() // closes the details
	e.settle(200 * time.Millisecond)
	pd := memberMenuTab(t, e, mrmPrivateID)
	pm := openRowMenu(t, e, pd, mrmGale)
	pickRow(t, e, pm, mrmKickLabel)
	pdlg, ok := topOverlay[*popups.RemoveMemberDialog](e.a)
	if !ok {
		t.Fatal("no RemoveMemberDialog in the private channel")
	}
	if c := pdlg.Copy; !strings.HasSuffix(c.Title, " from no-agents?") ||
		c.Para != "They’ll no longer have access to the channel and can only rejoin by invitation." {
		t.Errorf("private dialog %+v", c)
	}
}
