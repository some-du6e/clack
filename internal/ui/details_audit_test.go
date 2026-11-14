package ui

// Channel details' Slack parts against internal/fakeslack (synthetic; the
// ch-details-shots workflow saves each state with PREVIEW_SHOTS_DIR):
// Settings with AI use, Guest access and Member / Guest permissions, the
// last member's "Leave and archive", and a DM's Add people modal.

import (
	"encoding/json"
	"net/url"
	"sync"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/views"
)

// chDetailsHooks answers the Settings reads Slack's AdminChannelModal
// makes, and edge users/counts with people for private.
func chDetailsHooks(fs *fakeslack.Server, private string, people int) {
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		switch method {
		case "permissions.info":
			return map[string]any{"ok": true, "allowed": []string{"RENAME_CHANNEL"}}, true
		case "conversations.permissions.accountTypes.list":
			return json.RawMessage(`{"ok":true,"account_types":[{"account_type":"FULL_MEMBER","permissions":[` +
				`{"permission":"ARCHIVE_CHANNEL","is_configurable":false,"is_allowed":true,"localized_permission_display_name":"Archive the channel"},` +
				`{"permission":"INVITE_TO_CHANNEL","is_configurable":false,"is_allowed":true,"localized_permission_display_name":"Add people to the channel"}]},` +
				`{"account_type":"MULTI_CHANNEL_GUEST","permissions":[]}]}`), true
		case "conversations.canSetProperties":
			return json.RawMessage(`{"ok":true,"props":[{"prop":"restrict_guests","can_set":false,"reason":"not_allowed"}]}`), true
		}
		return nil, false
	})
	fs.SetEdgeHook(func(method string, body map[string]json.RawMessage) (any, bool) {
		if method != "edge/users/counts" || private == "" {
			return nil, false
		}
		var ch string
		_ = json.Unmarshal(body["channel"], &ch)
		if ch != private {
			return nil, false
		}
		return map[string]any{"ok": true, "counts": map[string]int{"people": people, "everyone": people}}, true
	})
}

func chDetailsFind(e *perfEnv, kind string) string {
	for _, s := range e.a.model().Sections {
		for _, id := range s.Rows {
			if c := e.a.model().conv(id); c != nil && c.Kind == kind && !c.Preview {
				return id
			}
		}
	}
	return ""
}

// Settings for a plain member: Channel name (RENAME_CHANNEL), AI use and
// Guest access read-only, Huddles, Member / Guest permissions, member
// suggestions and Copy members.
func TestChannelDetailsSettingsSlackShot(t *testing.T) {
	e, fs := previewEnv(t)
	for _, x := range []string{"ai_channel_exclusions", "enable_new_channel_settings", "move_channel_tabs_settings"} {
		e.a.Core().SetExperimentGroup(x, "on")
	}
	chDetailsHooks(fs, "", 0)
	ch := fakeslackMemberChannel(e)
	e.openChannel(ch)
	e.settle(200 * time.Millisecond)
	e.a.OpenDetailsTab(ch, "Settings")
	e.settle(800 * time.Millisecond)
	d := topDetails(t, e)
	if d.Tab() != "Settings" {
		t.Fatalf("opened on %q", d.Tab())
	}
	e.shot("ch-details-settings")
	d.ScrollSettingsTo("channel-permissions")
	e.settle(300 * time.Millisecond)
	e.shot("ch-details-settings-permissions")
}

// The last member of a private channel leaving: Slack's archive dialog.
func TestChannelDetailsLeaveArchiveShot(t *testing.T) {
	e, fs := previewEnv(t)
	ch := chDetailsFind(e, "private")
	if ch == "" {
		t.Skip("the fake workspace has no private channel")
	}
	chDetailsHooks(fs, ch, 1)
	e.openChannel(ch)
	e.settle(200 * time.Millisecond)
	e.a.OpenDetails(ch)
	e.settle(500 * time.Millisecond)
	d := topDetails(t, e)
	d.Press("leave")
	e.until("archive dialog", 10*time.Second, func() bool { return d.Dialog() == "leave-archive" })
	e.settle(200 * time.Millisecond)
	e.shot("ch-details-leave-archive")
}

// A DM's "Add people to this conversation": the people step, then
// "Include conversation history?".
func TestChannelDetailsDMAddPeopleShot(t *testing.T) {
	e, _ := previewEnv(t)
	dm := chDetailsFind(e, "im")
	if dm == "" {
		t.Skip("the fake workspace has no DM")
	}
	e.openChannel(dm)
	e.settle(200 * time.Millisecond)
	e.a.OpenDetails(dm)
	e.settle(500 * time.Millisecond)
	d := topDetails(t, e)
	e.shot("ch-details-dm-header")
	d.Press("add-people")
	e.until("add people", 10*time.Second, func() bool { return d.Dialog() == "add-people" })
	e.settle(200 * time.Millisecond)
	e.shot("ch-details-dm-add-people")
	var other core.User
	for _, s := range e.a.model().Sections {
		for _, id := range s.Rows {
			if c := e.a.model().conv(id); c != nil && c.Kind == "im" && id != dm {
				if u, ok := e.a.Core().User(c.User); ok {
					other = u
				}
			}
		}
	}
	if other.ID == "" {
		t.Skip("no second person")
	}
	d.AddPerson(other)
	e.settle(100 * time.Millisecond)
	d.Press("ok")
	e.until("history step", 10*time.Second, func() bool { return d.Dialog() == "add-people-history" })
	e.settle(200 * time.Millisecond)
	e.shot("ch-details-dm-add-history")
}

// A private channel's Settings under
// fe_private_to_public_slack_connect_and_visual_updates: managing-this-
// channel's "Change to a public channel", disabled with its reason for a
// member without CONVERT_PRIVATE_TO_PUBLIC, then (allowed) its modal;
// light and dark.
func TestChannelDetailsToPublicShot(t *testing.T) {
	e, fs := previewEnv(t)
	ch := chDetailsFind(e, "private")
	if ch == "" {
		t.Skip("the fake workspace has no private channel")
	}
	c := e.a.Core()
	c.SetExperimentGroup("fe_private_to_public_slack_connect_and_visual_updates", "on")
	var mu sync.Mutex
	allowed := []string{"RENAME_CHANNEL"}
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		switch method {
		case "permissions.info":
			mu.Lock()
			defer mu.Unlock()
			return map[string]any{"ok": true, "allowed": allowed}, true
		case "conversations.canSetProperties":
			return json.RawMessage(`{"ok":true,"props":[]}`), true
		}
		return nil, false
	})
	open := func() *views.ChannelDetails {
		e.openChannel(ch)
		e.settle(200 * time.Millisecond)
		e.a.OpenDetailsTab(ch, "Settings")
		e.settle(800 * time.Millisecond)
		d := topDetails(t, e)
		d.ScrollSettingsTo("managing-this-channel")
		e.settle(300 * time.Millisecond)
		return d
	}
	both := func(name string) {
		for _, dark := range []bool{false, true} {
			e.setDark(dark)
			e.settle(300 * time.Millisecond)
			e.shot(name + "-" + map[bool]string{false: "light", true: "dark"}[dark])
		}
		e.setDark(false)
		e.settle(200 * time.Millisecond)
	}
	d := open()
	both("ch-details-to-public-disabled")
	d.Press("to-public")
	e.settle(200 * time.Millisecond)
	if d.Dialog() != "" {
		t.Errorf("the disabled row opened %q", d.Dialog())
	}
	e.key(key.NameEscape, 0) // close the details
	e.settle(300 * time.Millisecond)
	mu.Lock()
	allowed = []string{"RENAME_CHANNEL", "CONVERT_PRIVATE_TO_PUBLIC"}
	mu.Unlock()
	if d2 := open(); d2 == d {
		t.Fatal("Escape left the first details open")
	} else {
		d = d2
	}
	e.until("the convert modal", 10*time.Second, func() bool {
		if d.Dialog() == "" {
			d.Press("to-public") // once the permission is read the row takes it
		}
		return d.Dialog() == "to-public"
	})
	e.settle(200 * time.Millisecond)
	both("ch-details-to-public-modal")
}

// HideUserConfirmationModal's "preferences" HelpLink: plain off, always
// underlined under a11y_underline_button_link with underline_links on;
// light and dark.
func TestChannelDetailsHideLinkShot(t *testing.T) {
	e, _ := previewEnv(t)
	dm := chDetailsFind(e, "im")
	if dm == "" {
		t.Skip("the fake workspace has no DM")
	}
	c := e.a.Core()
	e.openChannel(dm)
	e.settle(200 * time.Millisecond)
	e.a.OpenDetails(dm)
	e.settle(500 * time.Millisecond)
	d := topDetails(t, e)
	d.Press("hide")
	e.until("the hide modal", 10*time.Second, func() bool { return d.Dialog() == "hide" })
	for _, on := range []bool{false, true} {
		g := "off"
		if on {
			g = "on"
		}
		c.SetExperimentGroup("a11y_underline_button_link", g)
		c.S.Lock()
		c.S.Prefs.UnderlineLinks = true
		c.S.Unlock()
		if c.UnderlineButtonLinks() != on {
			t.Fatalf("%s: UnderlineButtonLinks %v", g, !on)
		}
		for _, dark := range []bool{false, true} {
			e.setDark(dark)
			e.settle(300 * time.Millisecond)
			e.shot("ch-details-hide-link-" + g + "-" + map[bool]string{false: "light", true: "dark"}[dark])
		}
	}
	e.setDark(false)
}

// Settings' managing-this-channel for a public channel whose member may
// convert, archive and delete it (views/details_managing.go): the rows,
// then ArchiveChannelModal, DeleteChannelModal with its checkbox ticked
// and ConvertChannelToPrivateModal; light and dark.
func TestChannelDetailsManagingShot(t *testing.T) {
	e, fs := previewEnv(t)
	ch := ""
	for _, s := range e.a.model().Sections { // a public channel that isn't #general
		for _, id := range s.Rows {
			if c, ok := e.a.Core().Conv(id); ch == "" && ok && c.Kind == "channel" && !c.Preview && !c.IsGeneral {
				ch = id
			}
		}
	}
	if ch == "" {
		t.Skip("the fake workspace has no public channel besides #general")
	}
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		switch method {
		case "permissions.info":
			return map[string]any{"ok": true, "allowed": []string{"RENAME_CHANNEL", "ARCHIVE_CHANNEL", "DELETE_CHANNEL", "CONVERT_TO_PRIVATE"}}, true
		case "conversations.canSetProperties":
			return json.RawMessage(`{"ok":true,"props":[]}`), true
		}
		return nil, false
	})
	e.openChannel(ch)
	e.settle(200 * time.Millisecond)
	e.a.OpenDetailsTab(ch, "Settings")
	e.settle(800 * time.Millisecond)
	d := topDetails(t, e)
	d.ScrollSettingsTo("managing-this-channel")
	e.settle(300 * time.Millisecond)
	both := func(name string) {
		for _, dark := range []bool{false, true} {
			e.setDark(dark)
			e.settle(300 * time.Millisecond)
			e.shot(name + "-" + map[bool]string{false: "light", true: "dark"}[dark])
		}
		e.setDark(false)
		e.settle(200 * time.Millisecond)
	}
	both("ch-details-managing")
	for _, m := range []struct{ row, dlg, shot string }{
		{"archive", "archive", "ch-details-archive-modal"},
		{"delete", "delete", "ch-details-delete-modal"},
		{"to-private", "to-private", "ch-details-to-private-modal"},
	} {
		e.until("the "+m.dlg+" modal", 10*time.Second, func() bool {
			if d.Dialog() == "" {
				d.Press(m.row)
			}
			return d.Dialog() == m.dlg
		})
		if m.dlg == "delete" {
			d.Press("delete-confirm")
		}
		e.settle(200 * time.Millisecond)
		both(m.shot)
		d.Press("cancel")
		e.until("the modal closes", 10*time.Second, func() bool { return d.Dialog() == "" })
	}
}
