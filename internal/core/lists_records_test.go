package core

import (
	"context"
	"testing"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// Saved list records against Slack's CDN build fa938a086569: Jf4A
// addReminderToRecord's three branches with mheF's saved.add / saved.update
// / saved.delete args, ListRecordRemindMenu's onClear, the list records
// in saved.list and in saved_* events, and lH2I's file channels.
// Synthetic only.

func TestListRecordReminderCalls(t *testing.T) {
	a, f := testApp(t)
	ctx := context.Background()
	f.reply["saved.add"] = `{"ok":true}`
	f.reply["saved.update"] = `{"ok":true}`
	f.reply["saved.delete"] = `{"ok":true}`
	a.SetExperimentGroup("later_todos", "on")

	// not saved: addSavedListRecord {item_type list_record, item_id list, ts record, date_due}, no todo_state (Qq with a reminder)
	if err := a.RemindListRecord(ctx, "F1", "Rec1", 5000); err != nil {
		t.Fatal(err)
	}
	p := f.last("saved.add")
	if p.Get("item_type") != "list_record" || p.Get("item_id") != "F1" || p.Get("ts") != "Rec1" || p.Get("date_due") != "5000" || p.Has("todo_state") {
		t.Fatalf("add: %v", p)
	}
	m, ok := a.ListRecordSaved("F1", "Rec1")
	if !ok || m.DateDue != 5000 || m.TodoState != "to_do" {
		t.Errorf("after add: %+v %v", m, ok)
	}

	// saved and a time: updateSavedListRecord {date_due}; a completed one is marked uncompleted
	a.patchSavedMeta(listRecordSavedKey("F1"), "Rec1", func(m *SavedMeta) { m.DateCompleted = 4000 })
	if err := a.RemindListRecord(ctx, "F1", "Rec1", 7000); err != nil {
		t.Fatal(err)
	}
	p = f.last("saved.update")
	if p.Get("item_type") != "list_record" || p.Get("item_id") != "F1" || p.Get("ts") != "Rec1" || p.Get("date_due") != "7000" || p.Get("mark") != "uncompleted" {
		t.Fatalf("update: %v", p)
	}
	if m, _ := a.ListRecordSaved("F1", "Rec1"); m.DateDue != 7000 || m.DateCompleted != 0 {
		t.Errorf("after update: %+v", m)
	}

	// Clear reminder on a to-do: updateSavedListRecord {dateDue: dateCreated}
	m, _ = a.ListRecordSaved("F1", "Rec1")
	if err := a.ClearListRecordReminder(ctx, "F1", "Rec1"); err != nil {
		t.Fatal(err)
	}
	if p = f.last("saved.update"); p.Get("date_due") == "" || p.Get("date_due") != itoa64(m.DateCreated) || p.Has("mark") {
		t.Fatalf("clear a to-do: %v (created %d)", p, m.DateCreated)
	}

	// saved and no time: removeSavedListRecord
	if err := a.RemindListRecord(ctx, "F1", "Rec1", 0); err != nil {
		t.Fatal(err)
	}
	p = f.last("saved.delete")
	if p.Get("item_type") != "list_record" || p.Get("item_id") != "F1" || p.Get("ts") != "Rec1" {
		t.Fatalf("delete: %v", p)
	}
	if _, ok := a.ListRecordSaved("F1", "Rec1"); ok {
		t.Error("still saved after saved.delete")
	}

	// a saved (not to-do) record's Clear leaves Later
	a.setSavedMeta(listRecordSavedKey("F1"), "Rec2", SavedMeta{TodoState: "saved", DateCreated: 10, DateDue: 900})
	if err := a.ClearListRecordReminder(ctx, "F1", "Rec2"); err != nil || f.last("saved.delete").Get("ts") != "Rec2" {
		t.Fatalf("clear a bookmark: %v %v", err, f.last("saved.delete"))
	}
}

func TestListRecordsSavedFromListAndEvents(t *testing.T) {
	a, f := testApp(t)
	f.reply["saved.list"] = `{"ok":true,"saved_items":[` +
		`{"item_id":"F1","item_type":"list_record","ts":"Rec1","todo_state":"to_do","date_created":100,"date_due":5000},` +
		`{"item_id":"F1","item_type":"list_record","ts":"Rec2","todo_state":"saved","date_created":200}]}`
	if err := a.loadSaved(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m, ok := a.ListRecordSaved("F1", "Rec1"); !ok || m != (SavedMeta{TodoState: "to_do", DateCreated: 100, DateDue: 5000}) {
		t.Errorf("to-do record: %+v %v", m, ok)
	}
	if _, ok := a.ListRecordSaved("F1", "Rec2"); !ok {
		t.Error("saved record missing")
	}
	a.handle(rtm("saved_deleted", map[string]any{"saved": map[string]any{"item_id": "F1", "item_type": "list_record", "ts": "Rec2"}}))
	if _, ok := a.ListRecordSaved("F1", "Rec2"); ok {
		t.Error("saved_deleted left the record saved")
	}
	a.handle(rtm("saved_added", map[string]any{"saved": map[string]any{"item_id": "F2", "item_type": "list_record", "ts": "Rec9",
		"todo_state": "to_do", "date_created": 300, "date_due": 400}}))
	if m, ok := a.ListRecordSaved("F2", "Rec9"); !ok || m.DateDue != 400 {
		t.Errorf("saved_added: %+v %v", m, ok)
	}
}

func TestListRecordArchiveAndDuplicateArgs(t *testing.T) {
	a, f := testApp(t)
	ctx := context.Background()
	f.reply["lists.records.archive"] = `{"ok":true}`
	f.reply["lists.records.create"] = `{"ok":true,"record":{"id":"Rec3","list_id":"F1","fields":[]}}`
	if err := a.ArchiveListRecords(ctx, "F1", "Rec1"); err != nil {
		t.Fatal(err)
	}
	if p := f.last("lists.records.archive"); p.Get("list_id") != "F1" || p.Get("ids") != `["Rec1"]` {
		t.Errorf("archive: %v", p)
	}
	if _, err := a.DuplicateListRecord(ctx, "F1", "Rec1", ""); err != nil {
		t.Fatal(err)
	}
	if p := f.last("lists.records.create"); p.Get("list_id") != "F1" || p.Get("duplicated_record_id") != "Rec1" || p.Get("updated_timestamp") == "" {
		t.Errorf("duplicate: %v", p)
	}
	if got := ListRecordLink("https://x.slack.com/lists/T1/F1", "Rec1"); got != "https://x.slack.com/lists/T1/F1?record_id=Rec1" {
		t.Errorf("link %q", got)
	}
}

// lH2I: a file channel reads as its file only under
// search_file_channel_messages and only once the file is known.
func TestFileChannelFile(t *testing.T) {
	a, _ := testApp(t)
	a.S.PutConv(slack.Conversation{ID: "C0CANV1", Name: "plan", IsChannel: true, IsPrivate: true, IsFile: true})
	a.S.PutConv(slack.Conversation{ID: "C0LIST1", Name: "tracker", IsChannel: true, IsPrivate: true, IsFile: true})
	a.S.PutConv(slack.Conversation{ID: "C0PLAIN", Name: "general", IsChannel: true})
	a.SetExperimentGroup(ExpSearchFileChannelMessages, "on")
	if _, _, ok := a.FileChannelFile("C0CANV1"); ok {
		t.Error("a file channel whose file isn't loaded read as a file")
	}
	a.S.PutCanvasRef("F0CANV1", richtext.CanvasRef{Title: "Plan", Status: "normal", Kind: "canvas"})
	a.S.PutCanvasRef("F0LIST1", richtext.CanvasRef{Title: "Tracker", Status: "normal", Kind: "list"})
	a.S.PutCanvasRef("F0PLAIN", richtext.CanvasRef{Title: "x", Status: "normal", Kind: "canvas"})
	if k, id, ok := a.FileChannelFile("C0CANV1"); !ok || k != "canvas" || id != "F0CANV1" {
		t.Errorf("canvas channel: %q %q %v", k, id, ok)
	}
	if k, id, ok := a.FileChannelFile("C0LIST1"); !ok || k != "list" || id != "F0LIST1" {
		t.Errorf("list channel: %q %q %v", k, id, ok)
	}
	if _, _, ok := a.FileChannelFile("C0PLAIN"); ok {
		t.Error("a plain channel read as a file")
	}
	if fc, ok := a.S.FileChannel("C0LIST1"); !ok || fc.Kind != "list" {
		t.Errorf("store hook: %+v %v", fc, ok)
	}
	a.SetExperimentGroup(ExpSearchFileChannelMessages, "off")
	if _, _, ok := a.FileChannelFile("C0CANV1"); ok {
		t.Error("off: a file channel read as its file")
	}
}
