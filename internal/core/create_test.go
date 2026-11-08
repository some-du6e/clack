package core

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
)

// Slack's channel name input (boot-shared 45Rv + PvOQ replaceSpaces):
// lowercase, leading "#"s dropped, whitespace to "-". Everything else is
// left for conversations.validateName to reject.
func TestNormalizeChannelName(t *testing.T) {
	for in, want := range map[string]string{
		"":                  "",
		"Plan Budget":       "plan-budget",
		"##general":         "general",
		"#Team#Ops":         "team#ops",
		"a\tb\nc":           "a-b-c",
		"  lead":            "--lead",
		"ÜBER cool":         "über-cool",
		"q4.launch":         "q4.launch",
		"already-fine_name": "already-fine_name",
	} {
		if got := NormalizeChannelName(in); got != want {
			t.Errorf("NormalizeChannelName(%q) = %q, want %q", in, got, want)
		}
	}
}

// The modal's messages for each error (PvOQ getErrorStringFromChannelNameErrors
// after qJpn's mapping).
func TestChannelNameErrorText(t *testing.T) {
	for _, c := range []struct {
		err  error
		ws   string
		want string
	}{
		{nil, "", ""},
		{&slack.Error{Code: "name_taken"}, "Hack Club", "That name is already taken by a channel, username, or user group in Hack Club."},
		{&slack.Error{Code: "name_taken"}, "", "That name is already taken by a channel, username, or user group in this workspace."},
		{&slack.Error{Code: "invalid_name_specials"}, "", "Channel names can’t contain spaces or punctuation. Use dashes to separate words."},
		{&slack.Error{Code: "invalid_name_punctuation"}, "", "Channel names can’t contain spaces or punctuation. Use dashes to separate words."},
		{&slack.Error{Code: "invalid_name_maxlength"}, "", "Channel names can’t be longer than 80 characters."},
		{&slack.Error{Code: "invalid_name_required"}, "", "Give your channel a name to continue. You can always change the name later."},
		{&slack.Error{Code: "restricted_action"}, "", "Your organization’s settings have changed and may limit who can create channels. Contact an Admin or channel Owner to add people."},
		{errors.New("restricted_action"), "", "Your organization’s settings have changed and may limit who can create channels. Contact an Admin or channel Owner to add people."},
		{&slack.Error{Code: "ratelimited"}, "", "For some weird reason, that didn’t work. Please try again to continue."},
		{context.DeadlineExceeded, "", "For some weird reason, that didn’t work. Please try again to continue."},
	} {
		if got := ChannelNameErrorText(ChannelNameCode(c.err), c.ws); got != c.want {
			t.Errorf("%v: %q, want %q", c.err, got, c.want)
		}
	}
}

func bootFake(t *testing.T) (*fakeslack.Server, *App) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	return fs, a
}

// conversations.validateName against the fake: the server's codes come
// back as *slack.Error, the free name as Slack spells it.
func TestValidateChannelName(t *testing.T) {
	fs, a := bootFake(t)
	ctx := context.Background()
	first, ok := a.Conv(fs.ChannelIDs()[0])
	if !ok || first.Name == "" {
		t.Fatal("no first channel")
	}
	taken := first.Name
	for name, want := range map[string]string{
		"brand-new-room":        "",
		"":                      "invalid_name_required",
		strings.Repeat("x", 81): "invalid_name_maxlength",
		"---":                   "invalid_name_punctuation",
		"Capital":               "invalid_name_specials",
		"a.b":                   "invalid_name_specials",
		"two words":             "invalid_name_specials",
		taken:                   "name_taken",
		"über-straße_2":         "",
		strings.Repeat("y", 80): "",
	} {
		got, err := a.ValidateChannelName(ctx, name)
		if code := ChannelNameCode(err); code != want && !(want == "" && err == nil) {
			t.Errorf("validate %q: %v, want %q", name, err, want)
		}
		if want == "" && got != name {
			t.Errorf("validate %q answered name %q", name, got)
		}
	}
	// the two client-side short cuts never reach Slack
	if n := fs.CallCount("conversations.validateName"); n != 8 {
		t.Errorf("validateName calls = %d, want 8 (empty and too long are answered locally)", n)
	}
}

// End to end against the fake: create lands the channel in the sidebar's
// Channels section; from a custom section it goes into that section
// (bulkUpdate), from Starred it is starred; private sets is_private.
func TestCreateChannel(t *testing.T) {
	fs, a := bootFake(t)
	ctx := context.Background()
	var forms []url.Values
	fs.Hook = func(method string, f url.Values) (any, bool) {
		if method == "conversations.create" {
			forms = append(forms, f)
		}
		return nil, false
	}

	c, err := a.CreateChannel(ctx, CreateChannelOpts{Name: "launch-plan"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Kind != "channel" || c.Name != "launch-plan" || c.ID == "" {
		t.Fatalf("created %+v", c)
	}
	f := forms[0]
	if f.Get("name") != "launch-plan" || f.Get("validate_name") != "true" || f.Get("team_id") != fakeslack.TeamID || f.Has("is_private") {
		t.Errorf("conversations.create form %v", f)
	}
	if sec := sectionOf(a.Snapshot(), c.ID); sec != "channels" {
		t.Errorf("new channel is in section type %q, want channels", sec)
	}

	p, err := a.CreateChannel(ctx, CreateChannelOpts{Name: "secret-plans", Private: true, Section: "L0FAKE01"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != "private" || forms[1].Get("is_private") != "true" {
		t.Errorf("private: kind %q form %v", p.Kind, forms[1])
	}
	if got := fs.SectionInserts("L0FAKE01"); len(got) != 1 || got[0] != p.ID {
		t.Errorf("bulkUpdate inserts %v, want [%s]", got, p.ID)
	}
	if sec := sectionOfID(a.Snapshot(), p.ID); sec != "L0FAKE01" {
		t.Errorf("channel from a custom section is in %q, want L0FAKE01", sec)
	}

	s, err := a.CreateChannel(ctx, CreateChannelOpts{Name: "star-me", Section: "L0FAKE00"})
	if err != nil {
		t.Fatal(err)
	}
	if !fs.Starred(s.ID) || sectionOf(a.Snapshot(), s.ID) != "stars" {
		t.Errorf("channel from Starred: starred=%v section %q", fs.Starred(s.ID), sectionOf(a.Snapshot(), s.ID))
	}

	// taken names and server restrictions come back as Slack's codes
	if _, err := a.CreateChannel(ctx, CreateChannelOpts{Name: "launch-plan"}); ChannelNameCode(err) != NameTaken {
		t.Errorf("duplicate: %v", err)
	}
	fs.Hook = func(method string, f url.Values) (any, bool) {
		if method == "conversations.create" {
			return map[string]any{"ok": false, "error": "restricted_action"}, true
		}
		return nil, false
	}
	if _, err := a.CreateChannel(ctx, CreateChannelOpts{Name: "nope"}); ChannelNameCode(err) != NameRestricted {
		t.Errorf("restricted: %v", err)
	}
	if len(fs.Created()) != 3 {
		t.Errorf("fake made %d channels, want 3", len(fs.Created()))
	}

	// Add people: conversations.invite on the new channel
	if err := a.Invite(ctx, c.ID, "U0FAKE00003", "U0FAKE00004"); err != nil {
		t.Fatal(err)
	}
	if got := fs.Invited(c.ID); strings.Join(got, ",") != "U0FAKE00003,U0FAKE00004" {
		t.Errorf("invited %v", got)
	}
}

func sectionOf(s Snapshot, id string) string {
	for _, sec := range s.Sections {
		for _, ch := range sec.Channels {
			if ch == id {
				return sec.Type
			}
		}
	}
	return ""
}

func sectionOfID(s Snapshot, id string) string {
	for _, sec := range s.Sections {
		for _, ch := range sec.Channels {
			if ch == id {
				return sec.ID
			}
		}
	}
	return ""
}
