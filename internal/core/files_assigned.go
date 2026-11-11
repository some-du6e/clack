package core

// The list items assigned to the viewer: the Files sidebar's "Assigned to
// you" (client-unified-files-sidebar-view cNNe, build 4d0c78085232),
// shown while lists_grid_todos is on and fetchAssignedItems (boot-shared
// q/55) found any.

import (
	"context"
	"net/url"
	"strconv"
	"sync"

	"github.com/some-du6e/clack/internal/lists"
)

// MyListItem is one of lists.getMyItems' records.
type MyListItem struct {
	ListID string
	Record lists.Record
}

type myItemsStore struct {
	mu      sync.Mutex
	items   map[string]MyListItem // myItemMap, by record id
	fetched bool
}

var myItemsStores sync.Map // *App -> *myItemsStore

func (a *App) myItems() *myItemsStore {
	if v, ok := myItemsStores.Load(a); ok {
		return v.(*myItemsStore)
	}
	v, _ := myItemsStores.LoadOrStore(a, &myItemsStore{})
	return v.(*myItemsStore)
}

// FetchMyListItems is fetchAssignedItems with no options:
// lists.getMyItems {include_subtasks (lists_sub_tasks), include_approvals
// (approvals_in_todos)}, the records keyed by id (setMyItems).
func (a *App) FetchMyListItems(ctx context.Context) error {
	v := url.Values{
		"include_subtasks":  {strconv.FormatBool(a.Experiment("lists_sub_tasks") == "on")},
		"include_approvals": {strconv.FormatBool(a.Experiment("approvals_in_todos") == "on")},
	}
	var r struct {
		Records []lists.Record `json:"records"`
	}
	if err := a.C.Call(ctx, "lists.getMyItems", v, &r); err != nil {
		return err
	}
	m := map[string]MyListItem{}
	for _, rec := range r.Records {
		if rec.ID != "" {
			m[rec.ID] = MyListItem{ListID: rec.ListID, Record: rec}
		}
	}
	s := a.myItems()
	s.mu.Lock()
	s.items, s.fetched = m, true
	s.mu.Unlock()
	return nil
}

// MyListItemCount is how many items lists.getMyItems returned (0 before
// it answered).
func (a *App) MyListItemCount() int {
	s := a.myItems()
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}
