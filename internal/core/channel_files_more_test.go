package core

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// The explorer search's suggestions are fetchFilesWithQuery with the
// channel: search.autocomplete.files {query: "in:<#C> q", include_shares}.
func TestChannelFileSuggestions(t *testing.T) {
	a, fs := channelFilesFake(t)
	ctx := context.Background()
	got, err := a.ChannelFileSuggestions(ctx, fakeslack.FilesChannelID, " report ")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "F0FILESPDF01" {
		t.Fatalf("suggestions %+v", got)
	}
	forms := fs.SearchForms("search.autocomplete.files")
	if len(forms) != 1 {
		t.Fatalf("%d calls", len(forms))
	}
	f := forms[0]
	if f.Get("query") != "in:<#"+fakeslack.FilesChannelID+"> report" || f.Get("include_shares") != "true" {
		t.Errorf("form %v", f)
	}
	if none, _ := a.ChannelFileSuggestions(ctx, fakeslack.FilesChannelID, "  "); none != nil || len(fs.SearchForms("search.autocomplete.files")) != 1 {
		t.Error("an empty query asked the server")
	}
}

// Share: files.getOverview {file_id}, then files.share with the dialog's
// args; the answer's file_msg_ts comes back.
func TestShareFileTo(t *testing.T) {
	a, fs := channelFilesFake(t)
	ctx := context.Background()
	ov, err := a.FileOverview(ctx, "F0FILESPDF01")
	if err != nil {
		t.Fatal(err)
	}
	if ov.Users != 6 || ov.Channels != 2 || ov.Files != 0 || ov.MaxUsers {
		t.Errorf("overview %+v", ov)
	}
	ts, err := a.ShareFileTo(ctx, "F0FILESPDF01", "C0FAKE00000", " have a look ")
	if err != nil {
		t.Fatal(err)
	}
	sh := fs.TabShares()
	if len(sh) != 1 || ts == "" || sh[0].TS != ts {
		t.Fatalf("shares %+v, ts %q", sh, ts)
	}
	wantForm(t, "files.share", sh[0].Args, map[string]string{"files": "F0FILESPDF01", "channel": "C0FAKE00000",
		"blocks": "*", "resharing_aware": "true", "skip_dlp_user_warning": "false", "from_share_modal": "false"})
	var blocks []struct {
		Type     string `json:"type"`
		Elements []struct {
			Elements []struct{ Text string } `json:"elements"`
		} `json:"elements"`
	}
	if err := json.Unmarshal([]byte(sh[0].Args.Get("blocks")), &blocks); err != nil || len(blocks) != 1 ||
		blocks[0].Elements[0].Elements[0].Text != "have a look" {
		t.Errorf("blocks %s", sh[0].Args.Get("blocks"))
	}
	if _, err := a.ShareFileTo(ctx, "F0FILESPDF01", "C0FAKE00000", ""); err != nil {
		t.Fatal(err)
	}
	if b := fs.TabShares()[1].Args.Get("blocks"); b != "[]" {
		t.Errorf("no note sent blocks %q", b)
	}
}

// View link details: fetchSearchModule for the URL, oldest first, in the
// desktop_link_details context; one share per conversation.
func TestLinkShares(t *testing.T) {
	wantForm(t, "link details", linkDetailsParams("https://x.example/a"), map[string]string{"module": "messages",
		"query": "https://x.example/a", "page": "1", "extracts": "1", "highlight": "1", "max_extract_len": "200",
		"extra_message_data": "1", "no_user_profile": "1", "count": "20", "file_title_only": "false",
		"query_rewrite_disabled": "false", "include_files_shares": "1", "search_context": "desktop_link_details",
		"sort": "timestamp", "sort_dir": "asc"})
	a, fs := channelFilesFake(t)
	ctx := context.Background()
	got, err := a.LinkShares(ctx, fakeslack.LinkSharedTwice)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Channel != fakeslack.FilesChannelID || got[0].Channel == fakeslack.FilesChannelID {
		t.Fatalf("shares %+v", got)
	}
	if got[0].Message.TS >= got[1].Message.TS || got[0].Message.User == "" {
		t.Errorf("not oldest first: %s, %s", got[0].Message.TS, got[1].Message.TS)
	}
	if forms := fs.SearchForms("search.modules.messages"); len(forms) != 1 || forms[0].Get("search_context") != "desktop_link_details" {
		t.Errorf("forms %v", forms)
	}
	one, err := a.LinkShares(ctx, "https://status.example.net/")
	if err != nil || len(one) != 1 {
		t.Errorf("one share: %+v %v", one, err)
	}
}

// Hide, show and move: conversations.removeTab {id, channel_id},
// conversations.addTab {type, channel_id}, conversations.moveTab
// {channel_id, id, after}; TabOrder sees hidden tabs.
func TestChannelTabWrites(t *testing.T) {
	a, fs := channelFilesFake(t)
	ctx := context.Background()
	ch := fakeslack.FilesChannelID
	if err := a.HideTab(ctx, ch, "files"); err != nil {
		t.Fatal(err)
	}
	order, err := a.TabOrder(ctx, ch)
	if err != nil || len(order) != 1 || !order[0].Disabled {
		t.Fatalf("after hide %+v %v", order, err)
	}
	if tabs, _ := a.Tabs(ctx, ch); len(tabs) != 0 {
		t.Errorf("the bar still shows %+v", tabs)
	}
	if err := a.ShowTab(ctx, ch, "files"); err != nil {
		t.Fatal(err)
	}
	if order, _ = a.TabOrder(ctx, ch); len(order) != 1 || order[0].Disabled {
		t.Fatalf("after show %+v", order)
	}
	fs.SetGalleryTabs("files", "workflows", "pins")
	got, err := a.MoveTab(ctx, ch, "files", "pins")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != "workflows" || got[2].ID != "files" {
		t.Fatalf("moved %+v", got)
	}
	if _, err := a.MoveTab(ctx, ch, "pins", ""); err != nil {
		t.Fatal(err)
	}
	if g := fs.GalleryTabs(); len(g) != 3 || g[0] != "pins" {
		t.Errorf("tabs %v", g)
	}
	w := fs.TabWrites()
	if len(w) != 4 {
		t.Fatalf("%d writes", len(w))
	}
	wantForm(t, "removeTab", w[0], map[string]string{"method": "conversations.removeTab", "id": "files", "channel_id": ch})
	wantForm(t, "addTab", w[1], map[string]string{"method": "conversations.addTab", "type": "files", "channel_id": ch})
	wantForm(t, "moveTab", w[2], map[string]string{"method": "conversations.moveTab", "id": "files", "after": "pins", "channel_id": ch})
	wantForm(t, "moveTab first", w[3], map[string]string{"method": "conversations.moveTab", "id": "pins", "channel_id": ch})
}

// MoveTabAfter is ManageChannelTabs' after: the last tab before the new
// place, never Messages.
func TestMoveTabAfter(t *testing.T) {
	o := []TabState{{ID: "channel", Type: "channel"}, {ID: "files", Type: "files"}, {ID: "pins", Type: "pins"}, {ID: "Ct1", Type: "canvas"}}
	for _, c := range []struct {
		from, to  int
		id, after string
		ok        bool
	}{
		{1, 2, "files", "pins", true},
		{2, 1, "pins", "", true}, // first after Messages: no after
		{3, 2, "Ct1", "files", true},
		{1, 3, "files", "Ct1", true},
		{1, 1, "", "", false},
		{0, 9, "", "", false},
	} {
		id, after, ok := MoveTabAfter(o, c.from, c.to)
		if id != c.id || after != c.after || ok != c.ok {
			t.Errorf("%d→%d: %q %q %v, want %q %q %v", c.from, c.to, id, after, ok, c.id, c.after, c.ok)
		}
	}
}

// useCanEditTabs: a member of a live channel may edit; archived or not a
// member, no; channel_managers keeps it to admins, managers and the
// creator.
func TestCanEditTabsChecked(t *testing.T) {
	a, fs := channelFilesFake(t)
	ctx := context.Background()
	ch := fakeslack.FilesChannelID
	if a.CanEditTabsChecked(ctx, ch) {
		t.Error("an unknown conversation is editable")
	}
	a.S.PutConv(slack.Conversation{ID: ch, Name: "files-gallery", IsChannel: true, IsMember: true})
	if a.CanEditTabsChecked(ctx, ch) {
		t.Error("on Grid without MANAGE_CHANNEL_TABS a member edits tabs")
	}
	fs.Permissions = map[string][]string{ch: {PermManageChannelTabs}}
	if !a.CanEditTabsChecked(ctx, ch) {
		t.Error("a member with MANAGE_CHANNEL_TABS can't edit tabs")
	}
	a.S.PutConv(slack.Conversation{ID: ch, Name: "files-gallery", IsChannel: true, IsMember: true, IsArchived: true})
	if a.CanEditTabsChecked(ctx, ch) {
		t.Error("an archived channel's tabs are editable")
	}
	a.S.PutConv(slack.Conversation{ID: ch, Name: "files-gallery", IsChannel: true, IsMember: true, Creator: "U0SOMEONE",
		Properties: json.RawMessage(`{"who_can_manage_tabs":"channel_managers"}`)})
	if a.CanEditTabsChecked(ctx, ch) {
		t.Error("channel_managers: a member who manages nothing can edit")
	}
}
