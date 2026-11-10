package core

import (
	"context"
	"testing"
)

// The channel details reads and writes send Slack's params and read its
// answers (chdetails.go; synthetic fixtures only).
func TestChannelDetailsAPIs(t *testing.T) {
	a, f := testApp(t)
	ctx := context.Background()

	f.reply["permissions.info"] = `{"ok":true,"allowed":["RENAME_CHANNEL"]}`
	got, err := a.ChannelPermissions(ctx, "C1", PermRenameChannel, PermArchiveChannel)
	if err != nil || !got[PermRenameChannel] || got[PermArchiveChannel] {
		t.Errorf("ChannelPermissions = %v, %v", got, err)
	}
	if p := f.last("permissions.info"); p.Get("user_id") != "USELF" || p.Get("channel_id") != "C1" ||
		p.Get("permissions") != `["RENAME_CHANNEL","ARCHIVE_CHANNEL"]` {
		t.Errorf("permissions.info params %v", p)
	}
	if _, err := a.ChannelPermissions(ctx, "D1", PermRenameChannel); err != nil || f.last("permissions.info").Has("channel_id") {
		t.Errorf("an IM sent channel_id: %v", f.last("permissions.info"))
	}

	f.reply["conversations.permissions.accountTypes.list"] = `{"ok":true,"account_types":[{"account_type":"FULL_MEMBER","permissions":[` +
		`{"permission":"ARCHIVE_CHANNEL","is_configurable":true,"is_allowed":true,"localized_permission_display_name":"Archive the channel"},{"permission":""}]}]}`
	at, err := a.AccountTypePermissions(ctx, "C1")
	if err != nil || len(at[AccountFullMember]) != 1 || at[AccountFullMember][0].Name != "Archive the channel" ||
		!at[AccountFullMember][0].Allowed || !at[AccountFullMember][0].Configurable || at[AccountMultiChannelGuest] == nil {
		t.Errorf("AccountTypePermissions = %+v, %v", at, err)
	}
	f.reply["conversations.permissions.accountTypes.set"] = `{"ok":true}`
	if err := a.SetAccountTypePermissions(ctx, "C1", AccountFullMember, []AccountTypePermission{{Permission: "ARCHIVE_CHANNEL"}}); err != nil {
		t.Fatal(err)
	}
	if p := f.last("conversations.permissions.accountTypes.set"); p.Get("channel_id") != "C1" || p.Get("account_type") != "FULL_MEMBER" ||
		p.Get("permissions") != `[{"permission":"ARCHIVE_CHANNEL","is_allowed":false}]` {
		t.Errorf("accountTypes.set params %v", p)
	}

	f.reply["conversations.canSetProperties"] = `{"ok":true,"props":[{"prop":"restrict_guests","can_set":false,"reason":"scg_present"}]}`
	cp, err := a.CanSetProperties(ctx, "C1", "restrict_guests")
	if err != nil || cp["restrict_guests"].CanSet || cp["restrict_guests"].Reason != "scg_present" {
		t.Errorf("CanSetProperties = %v, %v", cp, err)
	}
	if p := f.last("conversations.canSetProperties"); p.Get("props") != `["restrict_guests"]` {
		t.Errorf("canSetProperties params %v", p)
	}
	f.reply["conversations.setProperties"] = `{"ok":true}`
	if err := a.SetChannelProperties(ctx, "C9", "test", map[string]any{"restrict_guests": true}); err != nil {
		t.Fatal(err)
	}
	if p := f.last("conversations.setProperties"); p.Get("channel_id") != "C9" || p.Get("props") != `{"restrict_guests":true}` {
		t.Errorf("setProperties params %v", p)
	}

	if p := ParseChannelProps([]byte(`{"exclude_from_slack_ai":true,"who_can_manage_tabs":"channel_managers"}`)); !p.ExcludeFromAI || p.WhoCanManageTabs != "channel_managers" {
		t.Errorf("ParseChannelProps = %+v", p)
	}

	f.reply["conversations.createChild"] = `{"ok":true,"channel":{"id":"G7","is_mpim":true,"members":["USELF","U1","U2"]}}`
	f.reply["conversations.info"] = `{"ok":true,"channel":{"id":"G7","is_mpim":true,"members":["USELF","U1","U2"]}}`
	cv, err := a.AddToConversation(ctx, "D1", []string{"U2"}, "0000000000.000001")
	if err != nil || cv.ID != "G7" {
		t.Fatalf("AddToConversation = %+v, %v", cv, err)
	}
	if p := f.last("conversations.createChild"); p.Get("channel") != "D1" || p.Get("user_ids") != "U2" || p.Get("migrate_history_start_ts") != "0000000000.000001" {
		t.Errorf("createChild params %v", p)
	}

	a.S.Lock()
	a.S.Presence["U2"] = "active"
	a.S.Unlock()
	if p := a.UserPresence("U2"); p != "active" {
		t.Errorf("UserPresence = %q", p)
	}
	if p := a.UserPresence("U1"); p != "" {
		t.Errorf("UserPresence of an unknown presence = %q", p)
	}
}
