package core

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// The tab kinds as AkE0 and mwX5 sort them (tabs-3).
func TestTabKinds(t *testing.T) {
	for _, c := range []struct {
		id, typ                              string
		static, known, rename, edit, sys, dr bool
	}{
		{"channel", "channel", true, true, false, false, false, false},
		{"files", "files", false, true, false, true, true, true},
		{"bookmarks", "bookmarks", false, true, false, true, true, true},
		{"workflows", "workflows", false, true, false, true, true, true},
		{"CtCANVAS01", "canvas", false, false, true, true, false, true},
		{"CtLIST0001", "list", false, false, true, true, false, true},
		{"CtFOLDER01", "folder", false, false, true, true, false, true},
		{"CtAB12CD34", "code_channel_view", true, false, false, false, false, true},
		{"Ctlower123", "code_channel_view", true, false, false, false, false, false},
	} {
		if got := StaticTabType(c.typ); got != c.static {
			t.Errorf("%s static %v", c.id, got)
		}
		if got := KnownTabID(c.id); got != c.known {
			t.Errorf("%s known %v", c.id, got)
		}
		if got := RenameableTab(c.typ); got != c.rename {
			t.Errorf("%s renameable %v", c.id, got)
		}
		if got := EditableTab(c.typ); got != c.edit {
			t.Errorf("%s editable %v", c.id, got)
		}
		if got := EditableSystemTab(c.typ); got != c.sys {
			t.Errorf("%s editable system %v", c.id, got)
		}
		if got := DraggableTab(c.id, c.typ); got != c.dr {
			t.Errorf("%s draggable %v", c.id, got)
		}
	}
}

// updateTabOrderByIndex: the bar's drop index (calculateDropIndex) to
// conversations.moveTab's id and after.
func TestMoveTabByIndex(t *testing.T) {
	bar := []TabState{{ID: "channel", Type: "channel"}, {ID: "files", Type: "files"}, {ID: "CtA", Type: "canvas"},
		{ID: "CtB", Type: "list"}, {ID: "CtF", Type: "folder"}}
	for _, c := range []struct {
		src, dst  int
		id, after string
		ok        bool
	}{
		{3, 1, "CtB", "", true},      // onto Files & links' left: first after Messages
		{3, 2, "CtB", "files", true}, // onto the canvas's left
		{1, 2, "files", "CtA", true}, // dragged right: lands behind the tab it was dropped on's left neighbour
		{1, 4, "files", "CtF", true}, // the last tab's right side
		{1, 3, "files", "CtB", true},
		{2, 2, "", "", false},
		{9, 1, "", "", false},
	} {
		id, after, ok := MoveTabByIndex(bar, c.src, c.dst)
		if id != c.id || after != c.after || ok != c.ok {
			t.Errorf("%d→%d: %q %q %v, want %q %q %v", c.src, c.dst, id, after, ok, c.id, c.after, c.ok)
		}
	}
}

// Rename tab and Remove tab: conversations.updateTab and removeTab
// (keep_file_share only from Keep); maybeOpenRemoveFileTabConfirmationModal
// asks only for a canvas or list shared nowhere else.
func TestTabRenameRemove(t *testing.T) {
	a, f := testApp(t)
	ctx := context.Background()
	f.reply["conversations.updateTab"] = `{"ok":true}`
	f.reply["conversations.removeTab"] = `{"ok":true}`
	if err := a.RenameTab(ctx, "C1", "CtA", "Plan"); err != nil {
		t.Fatal(err)
	}
	if got := f.last("conversations.updateTab"); got.Get("id") != "CtA" || got.Get("channel_id") != "C1" || got.Get("label") != "Plan" ||
		got.Get("reason") != "updateTabLabel" {
		t.Errorf("updateTab %v", got)
	}
	if err := a.RemoveChannelTab(ctx, "C1", "CtA", true); err != nil {
		t.Fatal(err)
	}
	if got := f.last("conversations.removeTab"); got.Get("id") != "CtA" || got.Get("keep_file_share") != "true" ||
		got.Get("reason") != "remove-channel-tab" {
		t.Errorf("removeTab (keep) %v", got)
	}
	if err := a.RemoveChannelTab(ctx, "C1", "CtA", false); err != nil {
		t.Fatal(err)
	}
	if got := f.last("conversations.removeTab"); got.Has("keep_file_share") {
		t.Errorf("removeTab (remove access) %v", got)
	}

	f.reply["files.info"] = `{"ok":true,"file":{"id":"F1","mode":"quip","filetype":"quip"}}`
	f.reply["files.hasShares"] = `{"ok":true,"has_shares":false}`
	if what, list, err := a.FileTabRemoval(ctx, "C1", "F1"); err != nil || what != FileTabRemoveAsk || list {
		t.Errorf("a canvas shared only here: %v %v %v", what, list, err)
	}
	if got := f.last("files.hasShares"); got.Get("file_id") != "F1" || got.Get("channel_id") != "C1" ||
		got.Get("exclude_channel_tab_shares") != "true" {
		t.Errorf("hasShares %v", got)
	}
	f.reply["files.hasShares"] = `{"ok":true,"has_shares":true}`
	if what, _, _ := a.FileTabRemoval(ctx, "C1", "F1"); what != FileTabRemoveNow {
		t.Errorf("a canvas shared elsewhere asks (%v)", what)
	}
	f.reply["files.hasShares"] = `{"ok":true,"has_shares":false}`
	f.reply["files.info"] = `{"ok":true,"file":{"id":"F1","mode":"list","filetype":"list","org_or_workspace_access":"read"}}`
	if what, list, _ := a.FileTabRemoval(ctx, "C1", "F1"); what != FileTabRemoveNow || !list {
		t.Errorf("a list shared with the org asks (%v %v)", what, list)
	}
	f.reply["files.info"] = `{"ok":true,"file":{"id":"F1","mode":"list","filetype":"list","org_or_workspace_access":"none"}}`
	if what, list, _ := a.FileTabRemoval(ctx, "C1", "F1"); what != FileTabRemoveAsk || !list {
		t.Errorf("a list shared nowhere: %v %v", what, list)
	}
	a.S.PutConv(slack.Conversation{ID: "C1", Name: "general", IsChannel: true, IsMember: true,
		Properties: json.RawMessage(`{"channel_knowledge_canvas":{"file_id":"F1"}}`)})
	if what, _, _ := a.FileTabRemoval(ctx, "C1", "F1"); what != FileTabRemoveKnowledge {
		t.Errorf("the knowledge canvas: %v", what)
	}
	a.S.PutConv(slack.Conversation{ID: "C1", Name: "general", IsChannel: true, IsMember: true,
		Properties: json.RawMessage(`{"channel_knowledge_canvas":{"file_id":"F1","is_enabled":false}}`)})
	if what, _, _ := a.FileTabRemoval(ctx, "C1", "F1"); what == FileTabRemoveKnowledge {
		t.Error("a disabled knowledge canvas still asks about the refresh")
	}
}
