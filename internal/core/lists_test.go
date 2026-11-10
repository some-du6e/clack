package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/lists"
)

// The fixture list loads with every column type, its views and items.
func TestListLoad(t *testing.T) {
	a, _ := canvasApp(t)
	ctx := context.Background()
	d, err := a.List(ctx, fakeslack.ListKitchen)
	if err != nil {
		t.Fatal(err)
	}
	if d.Info.Title != "Launch tracker" || !d.Info.Editable || len(d.Info.Description) == 0 {
		t.Errorf("info %+v", d.Info)
	}
	types := map[string]bool{}
	for _, c := range d.Meta.Schema {
		types[c.Type] = true
	}
	for _, want := range []string{lists.Text, lists.Select, lists.User, lists.Date, lists.Checkbox, lists.Number, lists.Rating,
		lists.Vote, lists.Link, lists.Email, lists.Phone, lists.Channel, lists.Attachment, lists.Canvas, lists.Message,
		lists.CreatedBy, lists.CreatedTime, lists.LastEditedBy, lists.LastEditedTime} {
		if !types[want] {
			t.Errorf("column type %s missing", want)
		}
	}
	if len(d.Records) != 6 {
		t.Fatalf("records = %d", len(d.Records))
	}
	if tabs := d.Meta.TabViews(); len(tabs) != 4 || tabs[1].Type != lists.ViewBoard || tabs[1].GroupBy() != "Col0STATUS1" {
		t.Errorf("tabs %+v", tabs)
	}
	if v := d.Meta.DefaultTab(); v == nil || v.ID != "View0ALL001" {
		t.Errorf("default view %+v", v)
	}
	r := d.Record("Rec0ITEM001")
	if r == nil || r.Field("Col0LINK001") == nil || r.Field("Col0LINK001").Link[0].DisplayName != "Launch brief" {
		t.Fatalf("link cell: %+v", r)
	}
	// the "My open items" view: owner is me, status isn't done, by due date
	m := d.Meta.View("View0MINE01")
	rows := lists.Rows(&d.Meta, d.Records)
	lists.DefaultOrder(rows, m.ID)
	got := lists.Apply(&d.Meta, m, rows, "", lists.Env{Self: fakeslack.SelfID})
	var ids []string
	for _, r := range got {
		ids = append(ids, r.Rec.ID)
	}
	if strings.Join(ids, ",") != "Rec0ITEM001,Rec0ITEM003" {
		t.Errorf("My open items = %v", ids)
	}
}

// Editing round trip: cells, a new item, a comment and the realtime echo.
func TestListEditRoundTrip(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	evs, stop := a.Subscribe()
	defer stop()
	a.SetRealtime(true)
	waitEvent(t, evs, 5*time.Second, func(ev Event) bool { return ev.Type == "connection" && ev.Connected })
	d, err := a.List(ctx, fakeslack.ListKitchen)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.OpenList(ctx, fakeslack.ListKitchen); err != nil {
		t.Fatal(err)
	}
	defer a.CloseList(fakeslack.ListKitchen)
	col := func(key string) *lists.Column { return d.Meta.Column(key) }
	n := 6.0
	edits := []lists.Edit{
		{RowID: "Rec0ITEM003", Column: col("name"), Text: "Record the demo video (v2)"},
		{RowID: "Rec0ITEM003", Column: col("Col0STATUS1"), Values: []string{"OptDONE0001"}},
		{RowID: "Rec0ITEM003", Column: col("Col0DONE001"), Check: true},
		{RowID: "Rec0ITEM003", Column: col("Col0OWNER01"), Values: []string{"U0FAKE00002"}},
		{RowID: "Rec0ITEM003", Column: col("Col0EST0001"), Number: &n},
		{RowID: "Rec0ITEM003", Column: col("Col0LINK001"), Link: &lists.LinkValue{OriginalURL: "https://example.com/demo", DisplayName: "Demo"}},
	}
	if err := a.UpdateListCells(ctx, fakeslack.ListKitchen, edits...); err != nil {
		t.Fatal(err)
	}
	ups := fs.ListCellUpdates()
	if len(ups) != 1 {
		t.Fatalf("cells.update calls = %d", len(ups))
	}
	var cells []map[string]any
	if err := json.Unmarshal([]byte(ups[0].Get("cells")), &cells); err != nil || len(cells) != len(edits) {
		t.Fatalf("cells %v (%v)", ups[0].Get("cells"), err)
	}
	if cells[0]["row_id"] != "Rec0ITEM003" || cells[0]["column_id"] != "Col00000001" || cells[0]["rich_text"] == nil {
		t.Errorf("text cell %v", cells[0])
	}
	if l, _ := cells[5]["link"].([]any); len(l) != 1 || l[0].(map[string]any)["original_url"] != "https://example.com/demo" {
		t.Errorf("link cell %v", cells[5])
	}
	fl, _ := fs.List(fakeslack.ListKitchen)
	var stored *lists.Record
	for i := range fl.Records {
		if fl.Records[i].ID == "Rec0ITEM003" {
			stored = &fl.Records[i]
		}
	}
	if stored == nil || stored.Field("name").PlainText() != "Record the demo video (v2)" || stored.Field("Col0STATUS1").Select[0] != "OptDONE0001" ||
		!*stored.Field("Col0DONE001").Checkbox || stored.Field("Col0EST0001").Number[0] != 6 || stored.Field("Col0OWNER01").User[0] != "U0FAKE00002" {
		t.Errorf("server state %+v", stored)
	}
	// the local copy shows it at once
	if c := a.CachedList(fakeslack.ListKitchen).Record("Rec0ITEM003"); c.Field("name").PlainText() != "Record the demo video (v2)" {
		t.Errorf("cached name %q", c.Field("name").PlainText())
	}

	// the fidelity guard: formatted text and computed fields are refused, nothing sent
	if err := a.UpdateListCells(ctx, fakeslack.ListKitchen, lists.Edit{RowID: "Rec0ITEM002", Column: col("name"), Text: "x"}); err == nil {
		t.Error("formatted text cell was writable")
	}
	if err := a.UpdateListCells(ctx, fakeslack.ListKitchen, lists.Edit{RowID: "Rec0ITEM002", Column: col("Col0CRBY001"), Values: []string{"U1"}}); err == nil {
		t.Error("created_by was writable")
	}
	if err := a.UpdateListCells(ctx, fakeslack.ListKitchen, lists.Edit{RowID: "Rec0ITEM002", Column: col("Col0FILE001")}); err == nil {
		t.Error("attachment was writable")
	}
	if len(fs.ListCellUpdates()) != 1 {
		t.Error("a refused edit reached the server")
	}

	// a new item
	rec, err := a.CreateListRecord(ctx, fakeslack.ListKitchen, "", "", lists.Edit{Column: col("name"), Text: "Plan the party"})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Field("name").PlainText() != "Plan the party" {
		t.Errorf("created %+v", rec)
	}
	cr := fs.ListRecordCreates()
	if len(cr) != 1 || !strings.Contains(cr[0].Get("initial_fields"), `"column_id":"Col00000001"`) || cr[0].Get("updated_timestamp") == "" {
		t.Errorf("create call %v", cr)
	}

	// a comment on an item without a thread yet: the root is looked up first
	if _, err := a.CommentOnListRecord(ctx, fakeslack.ListKitchen, "Rec0ITEM004", "Looks good"); err != nil {
		t.Fatal(err)
	}
	ch, ts, err := a.ListRecordThread(ctx, fakeslack.ListKitchen, "Rec0ITEM004")
	if err != nil || ch != "C0LIST00001" {
		t.Fatalf("thread %s %s %v", ch, ts, err)
	}
	if cm := fs.ListComments(fakeslack.ListKitchen, ts); len(cm) != 2 || cm[1].Text != "Looks good" {
		t.Errorf("comments %+v", cm)
	}

	// someone else edits: the echo lands in the cached list
	fs.EditListAsOther(fakeslack.ListKitchen, "Rec0ITEM005", lists.Field{Key: "Col0STATUS1", ColumnID: "Col0STATUS1", Select: []string{"OptBLOK0001"}})
	deadline := time.After(10 * time.Second)
	for {
		if r := a.CachedList(fakeslack.ListKitchen).Record("Rec0ITEM005"); r != nil && len(r.Field("Col0STATUS1").Select) == 1 && r.Field("Col0STATUS1").Select[0] == "OptBLOK0001" {
			break
		}
		select {
		case <-evs:
		case <-deadline:
			t.Fatal("list_cells_updated never applied")
		}
	}
	id := fs.AddListRecordAsOther(fakeslack.ListKitchen, "Someone else's item")
	deadline = time.After(10 * time.Second)
	for a.CachedList(fakeslack.ListKitchen).Record(id) == nil {
		select {
		case <-evs:
		case <-deadline:
			t.Fatal("list_record_created never applied")
		}
	}
	if err := a.DeleteListRecord(ctx, fakeslack.ListKitchen, id); err != nil {
		t.Fatal(err)
	}
	if a.CachedList(fakeslack.ListKitchen).Record(id) != nil {
		t.Error("deleted record still cached")
	}
}

// Create menu: a blank list and one from a template.
func TestListCreate(t *testing.T) {
	a, _ := canvasApp(t)
	ctx := context.Background()
	tpls, err := a.ListTemplates(ctx)
	if err != nil || len(tpls) < 2 || !tpls[0].Starter {
		t.Fatalf("templates %+v %v", tpls, err)
	}
	id, err := a.CreateList(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	d, err := a.List(ctx, id)
	if err != nil || d.Info.Title != "Untitled list" || d.Meta.Primary() == nil {
		t.Fatalf("new list %+v %v", d, err)
	}
	id2, err := a.CreateList(ctx, "Chores", tpls[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if d2, err := a.List(ctx, id2); err != nil || !d2.Meta.TodoMode {
		t.Fatalf("template list %+v %v", d2, err)
	}
}
