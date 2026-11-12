package core

// Hidden people's messages (chdetails.go), against Slack's CDN build
// d3de3a487b1c: isUserHidden (client-boot 0OuZ a), msg.isUnhidden (z+/K)
// and its reset in unhideUserApi (boot-shared djmN), and
// HiddenMessageBody's direct-mention test (checkForMentionInMessage,
// c+16, with aOPx's options). Synthetic users, a fake Slack only.

import (
	"context"
	"encoding/json"
	"testing"
)

func TestUserHiddenLikeSlack(t *testing.T) {
	a, f := testApp(t)
	f.reply["users.prefs.set"] = `{"ok":true}`
	a.installPrefs(map[string]json.RawMessage{"hidden_users": json.RawMessage(`"U1, U2"`)})
	for _, c := range []struct {
		user string
		want bool
	}{{"U1", true}, {"U2", true}, {"USELF", false}, {"", false}, {"U9", false}} {
		if got := a.UserHidden(c.user); got != c.want {
			t.Errorf("UserHidden(%q) = %v, want %v", c.user, got, c.want)
		}
	}
	// the parsed list follows the pref
	a.installPrefs(map[string]json.RawMessage{"hidden_users": json.RawMessage(`"U2"`)})
	if a.UserHidden("U1") || !a.UserHidden("U2") {
		t.Error("UserHidden kept the old list")
	}
	// an org that locked hide_person_opt_out on: nobody is hidden
	for _, v := range []string{`true`, `1`, `"on"`} {
		a.setEnterprisePrefs(map[string]json.RawMessage{"locked_prefs": json.RawMessage(`{"hide_person_opt_out":` + v + `}`)})
		if a.UserHidden("U2") {
			t.Errorf("hide_person_opt_out %s: U2 still hidden", v)
		}
	}
	for _, v := range []string{`false`, `0`, `""`, `null`} {
		a.setEnterprisePrefs(map[string]json.RawMessage{"locked_prefs": json.RawMessage(`{"hide_person_opt_out":` + v + `}`)})
		if !a.UserHidden("U2") {
			t.Errorf("hide_person_opt_out %s: U2 not hidden", v)
		}
	}
	a.setEnterprisePrefs(map[string]json.RawMessage{})

	// Show anyway / Hide message, per message
	m := &Message{Channel: "C1", TS: "1.000100", User: "U2"}
	if !a.MessageFromHiddenSender(m) {
		t.Fatal("U2's message isn't from a hidden sender")
	}
	a.SetMessageUnhidden("C1", "1.000100", "U2", true)
	a.SetMessageUnhidden("C1", "2.000100", "U2", true)
	a.SetMessageUnhidden("C1", "3.000100", "U1", true)
	if !a.MessageUnhidden("C1", "1.000100") || a.MessageFromHiddenSender(m) {
		t.Error("Show anyway didn't mark the message")
	}
	if a.MessageUnhidden("C2", "1.000100") {
		t.Error("isUnhidden leaked to another channel")
	}
	a.SetMessageUnhidden("C1", "1.000100", "U2", false)
	if a.MessageUnhidden("C1", "1.000100") || !a.MessageFromHiddenSender(m) {
		t.Error("Hide message didn't clear it")
	}
	// unhideUserApi: U2's messages shown anyway go back to plain, U1's stay
	a.installPrefs(map[string]json.RawMessage{"hidden_users": json.RawMessage(`"U1,U2"`)})
	if err := a.UnhideUser(context.Background(), "U2"); err != nil {
		t.Fatal(err)
	}
	if v := f.last("users.prefs.set"); v.Get("name") != "hidden_users" || v.Get("value") != "U1" {
		t.Errorf("users.prefs.set %v, want hidden_users=U1", v)
	}
	if a.MessageUnhidden("C1", "2.000100") {
		t.Error("UnhideUser kept U2's message shown anyway")
	}
	if !a.MessageUnhidden("C1", "3.000100") {
		t.Error("UnhideUser reset U1's message")
	}
	a.ResetUnhidden("U1")
	if a.MessageUnhidden("C1", "3.000100") {
		t.Error("ResetUnhidden kept U1's message")
	}
}

func TestMentionsViewerDirectly(t *testing.T) {
	a, _ := testApp(t)
	a.S.SetMyUsergroup("SMINE0001", true)
	rich := func(els string) json.RawMessage {
		return json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[` + els + `]}]}]`)
	}
	for _, c := range []struct {
		name string
		m    Message
		want bool
	}{
		{"plain text", Message{Text: "hello"}, false},
		{"text, me", Message{Text: "hi <@USELF>"}, true},
		{"text, me with a label", Message{Text: "hi <@USELF|me>"}, true},
		{"text, a longer id", Message{Text: "hi <@USELF2>"}, false},
		{"text, someone else", Message{Text: "hi <@U1>"}, false},
		{"text, @here", Message{Text: "<!here> look"}, false},
		{"text, @channel", Message{Text: "<!channel> look"}, false},
		{"text, @everyone", Message{Text: "<!everyone> look"}, false},
		{"text, my group", Message{Text: "<!subteam^SMINE0001|@devs> look"}, true},
		{"text, another group", Message{Text: "<!subteam^SOTHER001|@ops> look"}, false},
		{"pinned_item", Message{Subtype: "pinned_item", Text: "<@USELF> pinned"}, false},
		{"rich text, me", Message{Blocks: rich(`{"type":"text","text":"hi "},{"type":"user","user_id":"USELF"}`)}, true},
		{"rich text, someone else", Message{Blocks: rich(`{"type":"user","user_id":"U1"}`)}, false},
		{"rich text, my group", Message{Blocks: rich(`{"type":"usergroup","usergroup_id":"SMINE0001"}`)}, true},
		{"rich text, @here", Message{Blocks: rich(`{"type":"broadcast","range":"here"}`)}, false},
		// with blocks the text isn't read
		{"blocks win over text", Message{Text: "<@USELF>", Blocks: rich(`{"type":"text","text":"hi"}`)}, false},
		{"a section's mrkdwn", Message{Blocks: json.RawMessage(`[{"type":"section","text":{"type":"mrkdwn","text":"ping <@USELF>"}}]`)}, true},
		{"a section's plain_text", Message{Blocks: json.RawMessage(`[{"type":"section","text":{"type":"plain_text","text":"ping <@USELF>"}}]`)}, false},
		{"a section field", Message{Blocks: json.RawMessage(`[{"type":"section","fields":[{"type":"mrkdwn","text":"<@USELF>"}]}]`)}, true},
		{"a context element", Message{Blocks: json.RawMessage(`[{"type":"context","elements":[{"type":"mrkdwn","text":"<@USELF>"}]}]`)}, true},
		{"an attachment's text", Message{Text: "see", Attachments: json.RawMessage(`[{"text":"for <@USELF>"}]`)}, true},
		{"an attachment's footer", Message{Text: "see", Attachments: json.RawMessage(`[{"footer":"<@USELF>"}]`)}, true},
		{"an attachment field", Message{Text: "see", Attachments: json.RawMessage(`[{"fields":[{"title":"Who","value":"<@USELF>"}]}]`)}, true},
		{"a link preview", Message{Text: "see", Attachments: json.RawMessage(`[{"from_url":"https://example.com","text":"<@USELF>"}]`)}, false},
		{"a forward", Message{Text: "see", Attachments: json.RawMessage(`[{"is_forwarded":true,"text":"<@USELF>"}]`)}, false},
		{"an attachment's rich text", Message{Text: "see", Attachments: json.RawMessage(`[{"blocks":` + string(rich(`{"type":"user","user_id":"USELF"}`)) + `}]`)}, false},
		{"an attachment's section", Message{Text: "see", Attachments: json.RawMessage(`[{"blocks":[{"type":"section","text":{"type":"mrkdwn","text":"<@USELF>"}}]}]`)}, true},
	} {
		if got := a.MentionsViewerDirectly(&c.m); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// TestMentionsViewerInReply: checkForMentionInMessage as
// shouldShowNotificationForChannelMessage asks it of a reply (gMhK, only
// ignoreHighlightWords): the viewer, a group they're in and @here count;
// a reply's @channel and @everyone don't (isReply).
func TestMentionsViewerInReply(t *testing.T) {
	a, _ := testApp(t)
	a.S.SetMyUsergroup("SMINE0001", true)
	rich := func(els string) json.RawMessage {
		return json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[` + els + `]}]}]`)
	}
	for _, c := range []struct {
		name string
		m    Message
		want bool
	}{
		{"plain text", Message{Text: "hello"}, false},
		{"text, me", Message{Text: "hi <@USELF>"}, true},
		{"text, my group", Message{Text: "<!subteam^SMINE0001|@devs> look"}, true},
		{"text, another group", Message{Text: "<!subteam^SOTHER001|@ops> look"}, false},
		{"text, @here", Message{Text: "<!here> look"}, true},
		{"text, @here with a label", Message{Text: "<!here|here> look"}, true},
		{"text, @channel", Message{Text: "<!channel> look"}, false},
		{"text, @everyone", Message{Text: "<!everyone> look"}, false},
		{"rich text, my group", Message{Blocks: rich(`{"type":"usergroup","usergroup_id":"SMINE0001"}`)}, true},
		{"rich text, another group", Message{Blocks: rich(`{"type":"usergroup","usergroup_id":"SOTHER001"}`)}, false},
		{"rich text, @here", Message{Blocks: rich(`{"type":"broadcast","range":"here"}`)}, true},
		{"rich text, @channel", Message{Blocks: rich(`{"type":"broadcast","range":"channel"}`)}, false},
		{"rich text, @everyone", Message{Blocks: rich(`{"type":"broadcast","range":"everyone"}`)}, false},
		{"an attachment's group", Message{Text: "see", Attachments: json.RawMessage(`[{"text":"for <!subteam^SMINE0001>"}]`)}, true},
		{"an attachment's rich @here", Message{Text: "see", Attachments: json.RawMessage(`[{"blocks":` + string(rich(`{"type":"broadcast","range":"here"}`)) + `}]`)}, false},
		{"pinned_item", Message{Subtype: "pinned_item", Text: "<!subteam^SMINE0001> pinned"}, false},
	} {
		if got := a.MentionsViewerInReply(&c.m); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	// HiddenMessageBody's options still leave @here out
	if a.MentionsViewerDirectly(&Message{Text: "<!here> look"}) {
		t.Error("MentionsViewerDirectly counts @here")
	}
}
