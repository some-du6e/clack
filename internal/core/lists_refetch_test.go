package core

import (
	"context"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/lists"
)

// A refetch is out when someone else edits a cell: its read took the list
// before the edit (the answer held after it's made), the edit's
// list_cells_updated lands in the cached list, and the refetch landing
// after it doesn't put the old cell back; the next read brings the list
// with the edit. TestListEditRoundTrip's flake (thread-reply-toast-3's
// full-test 45010174728): the cache's OptPROG0001 against the server's
// OptBLOK0001, the event received.
func TestListRefetchKeepsALaterEdit(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	evs, stop := a.Subscribe()
	defer stop()
	a.SetRealtime(true)
	waitEvent(t, evs, 5*time.Second, func(ev Event) bool { return ev.Type == "connection" && ev.Connected })
	id := fakeslack.ListKitchen
	if _, err := a.List(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := a.OpenList(ctx, id); err != nil {
		t.Fatal(err)
	}
	defer a.CloseList(id)
	status := func() string {
		r := a.CachedList(id).Record("Rec0ITEM005")
		if r == nil || r.Field("Col0STATUS1") == nil || len(r.Field("Col0STATUS1").Select) != 1 {
			return ""
		}
		return r.Field("Col0STATUS1").Select[0]
	}
	if s := status(); s == "" || s == "OptBLOK0001" {
		t.Fatalf("the item's status at the start: %q", s)
	}
	n := fs.CallCount("lists.records.list")
	fs.SetFault("lists.records.list", fakeslack.Fault{ReplyDelay: 1500 * time.Millisecond, Times: 1})
	a.listRefetch(id)
	for deadline := time.Now().Add(5 * time.Second); fs.CallCount("lists.records.list") == n; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the refetch's read never went out")
		}
	}
	fs.EditListAsOther(id, "Rec0ITEM005", lists.Field{Key: "Col0STATUS1", ColumnID: "Col0STATUS1", Select: []string{"OptBLOK0001"}})
	waitEvent(t, evs, 10*time.Second, func(ev Event) bool {
		return ev.Type == "list" && ev.ID == id && ev.Name == ListChanged && status() == "OptBLOK0001"
	})
	// the next reload: the held read's, or the one after it
	waitEvent(t, evs, 10*time.Second, func(ev Event) bool { return ev.Type == "list" && ev.ID == id && ev.Name == ListReloaded })
	if s := status(); s != "OptBLOK0001" {
		t.Errorf("after the refetch the item's status is %q, want the edit's OptBLOK0001", s)
	}
}
