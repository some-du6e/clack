package ui

// Channel details' Slack parts against internal/fakeslack (synthetic; the
// ch-details-shots workflow saves each state with PREVIEW_SHOTS_DIR):
// Settings with AI use, Guest access and Member / Guest permissions, the
// last member's "Leave and archive", and a DM's Add people modal.

import (
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
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
