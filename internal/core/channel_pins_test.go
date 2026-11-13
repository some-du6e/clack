package core

// The Pins tab's store (channel_pins.go) and the folders' bookmarks
// (bookmark_folders.go) against internal/fakeslack's #pins-gallery: the
// calls Slack makes, with its args, the order, and the realtime events.

import (
	"context"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

func pfApp(t *testing.T) (*App, *fakeslack.Server, *eventLog) {
	t.Helper()
	sz := fakeslack.Small
	sz.PinsFolders = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	evs, stop := a.Subscribe()
	t.Cleanup(stop)
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.SetRealtime(true)
	waitEvent(t, evs, 5*time.Second, func(ev Event) bool { return ev.Type == "connection" && ev.Connected })
	l := &eventLog{}
	go func() {
		for ev := range evs {
			l.add(ev)
		}
	}()
	return a, fs, l
}

func waitPins(t *testing.T, a *App, what string, ok func([]PinsTabItem) bool) []PinsTabItem {
	t.Helper()
	dl := time.Now().Add(5 * time.Second)
	for {
		items, loaded := a.PinsTab(fakeslack.PinsChannelID)
		if loaded && ok(items) {
			return items
		}
		if time.Now().After(dl) {
			t.Fatalf("%s: loaded %v, items %+v", what, loaded, items)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func pinKeys(items []PinsTabItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Key()
	}
	return out
}

// fetchPinsList: pins.list {channel, reason "fetchPinsList"} once, the
// rows in its order (newest pin first, the file after); pin_added
// prepends, pin_removed removes; Remove pin is pins.remove {channel,
// timestamp, reason "unpin-message"} with the row gone at once.
func TestPinsTabLikeSlack(t *testing.T) {
	a, fs, _ := pfApp(t)
	ch := fakeslack.PinsChannelID
	if items, loaded := a.PinsTab(ch); loaded || len(items) != 0 {
		t.Fatalf("first read: loaded %v items %d", loaded, len(items))
	}
	items := waitPins(t, a, "the pins", func(items []PinsTabItem) bool { return len(items) == 4 })
	want := append(append([]string{}, fakeslack.PinnedTS...), fakeslack.PinsFileID)
	if got := pinKeys(items); !equalStrings(got, want) {
		t.Errorf("rows %v, want %v", got, want)
	}
	if items[3].Type != "file" || items[3].File == nil || items[3].File.Title != "Q3 metrics" {
		t.Errorf("file row %+v", items[3])
	}
	for _, it := range items[:3] {
		if it.Type != "message" || it.Msg == nil || it.Channel != ch {
			t.Errorf("message row %+v", it)
		}
	}
	forms := fs.PinsFoldersForms("pins.list")
	if len(forms) != 1 || forms[0].Get("channel") != ch || forms[0].Get("reason") != "fetchPinsList" {
		t.Errorf("pins.list forms %v", forms)
	}
	a.PinsTab(ch) // fresh: no second fetch
	if n := len(fs.PinsFoldersForms("pins.list")); n != 1 {
		t.Errorf("%d pins.list calls", n)
	}

	// pin_added: first
	ts := fakeslack.PinsTS(1)
	ev, code := fs.Pin(fakeslack.SelfID, ch, ts, true)
	if code != "" {
		t.Fatal(code)
	}
	fs.Push(ev)
	waitPins(t, a, "pin_added", func(items []PinsTabItem) bool { return len(items) == 5 && items[0].TS == ts })
	// pin_removed: gone
	ev, _ = fs.Pin(fakeslack.SelfID, ch, ts, false)
	fs.Push(ev)
	waitPins(t, a, "pin_removed", func(items []PinsTabItem) bool { return len(items) == 4 && items[0].TS != ts })

	// Remove pin from the tab
	items, _ = a.PinsTab(ch)
	if err := a.UnpinFromTab(context.Background(), items[0]); err != nil {
		t.Fatal(err)
	}
	if items2, _ := a.PinsTab(ch); len(items2) != 3 || items2[0].TS == items[0].TS {
		t.Errorf("after Remove pin: %v", pinKeys(items2))
	}
	rf := fs.PinsFoldersForms("pins.remove")
	if len(rf) != 1 || rf[0].Get("channel") != ch || rf[0].Get("timestamp") != items[0].TS || rf[0].Get("reason") != "unpin-message" {
		t.Errorf("pins.remove forms %v", rf)
	}
	// a file's Remove pin is removePinnedItem alone
	file := items[len(items)-1]
	if err := a.UnpinFromTab(context.Background(), file); err != nil {
		t.Fatal(err)
	}
	if items3, _ := a.PinsTab(ch); len(items3) != 2 {
		t.Errorf("after the file's Remove pin: %v", pinKeys(items3))
	}
	if n := len(fs.PinsFoldersForms("pins.remove")); n != 1 {
		t.Errorf("%d pins.remove calls, want the message's alone", n)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func waitBookmarks(t *testing.T, a *App, what string, ok func([]Bookmark) bool) []Bookmark {
	t.Helper()
	dl := time.Now().Add(5 * time.Second)
	for {
		all, loaded := a.ChannelBookmarks(fakeslack.PinsChannelID)
		if loaded && ok(all) {
			return all
		}
		if time.Now().After(dl) {
			t.Fatalf("%s: loaded %v, %+v", what, loaded, all)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func bmIDs(bs []Bookmark) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.ID
	}
	return out
}

// The folders: bookmarks.list {channel, include_folders,
// include_legacy_workflows}; a folder's rows by rank, the loose ones
// apart; add, edit, remove and reorder with Slack's args, their
// bookmark_* events keeping the store current.
func TestBookmarkFoldersLikeSlack(t *testing.T) {
	a, fs, _ := pfApp(t)
	ch := fakeslack.PinsChannelID
	all := waitBookmarks(t, a, "the bookmarks", func(b []Bookmark) bool { return len(b) == 8 })
	lf := fs.PinsFoldersForms("bookmarks.list")
	if len(lf) == 0 || lf[0].Get("channel") != ch || lf[0].Get("include_folders") != "true" ||
		lf[0].Get("include_legacy_workflows") != "true" || lf[0].Get("reason") != "bookmarks-list-api" {
		t.Errorf("bookmarks.list forms %v", lf)
	}
	rows, _ := FolderRows(all, fakeslack.PinsFolderID)
	if got := bmIDs(rows); !equalStrings(got, []string{"Bk0PINSL1", "Bk0PINSL2", "Bk0PINSL3", "Bk0PINSF1"}) {
		t.Errorf("Design docs rows %v", got)
	}
	loose, _ := FolderRows(all, "")
	if got := bmIDs(loose); !equalStrings(got, []string{"Bk0PINSL4", "Bk0PINSL5"}) {
		t.Errorf("loose rows %v (the folders aren't rows)", got)
	}
	if e, _ := FolderRows(all, fakeslack.PinsEmptyFolderID); len(e) != 0 {
		t.Errorf("empty folder rows %v", bmIDs(e))
	}

	// add
	ctx := context.Background()
	b, err := a.AddFolderLink(ctx, ch, fakeslack.PinsFolderID, "https://new.example.com/x", "New thing")
	if err != nil {
		t.Fatal(err)
	}
	af := fs.PinsFoldersForms("bookmarks.add")
	if len(af) != 1 || af[0].Get("channel") != ch || af[0].Get("link") != "https://new.example.com/x" || af[0].Get("title") != "New thing" ||
		af[0].Get("type") != "link" || af[0].Get("parent_id") != fakeslack.PinsFolderID || af[0].Get("reason") != "bookmarks-add-api" {
		t.Errorf("bookmarks.add forms %v", af)
	}
	waitBookmarks(t, a, "the added link", func(all []Bookmark) bool {
		r, _ := FolderRows(all, fakeslack.PinsFolderID)
		return len(r) == 5 && r[4].ID == b.ID
	})
	// edit
	if _, err := a.EditFolderLink(ctx, ch, b, "https://new.example.com/y", "Renamed"); err != nil {
		t.Fatal(err)
	}
	ef := fs.PinsFoldersForms("bookmarks.edit")
	if len(ef) != 1 || ef[0].Get("bookmark") != b.ID || ef[0].Get("link") != "https://new.example.com/y" || ef[0].Get("title") != "Renamed" ||
		ef[0].Get("type") != "link" || ef[0].Get("channel") != ch {
		t.Errorf("bookmarks.edit forms %v", ef)
	}
	// reorder: the last row to the top
	if err := a.ReorderFolderBookmark(ctx, ch, fakeslack.PinsFolderID, b.ID, 0); err != nil {
		t.Fatal(err)
	}
	of := fs.PinsFoldersForms("bookmarks.reorder")
	if len(of) != 1 || of[0].Get("center") != b.ID || of[0].Get("left") != "" || of[0].Get("right") != "Bk0PINSL1" ||
		of[0].Get("parent_id") != fakeslack.PinsFolderID || of[0].Get("bookmark_type") != "link" || of[0].Get("channel") != ch {
		t.Errorf("bookmarks.reorder forms %v", of)
	}
	waitBookmarks(t, a, "the reordered rows", func(all []Bookmark) bool {
		r, _ := FolderRows(all, fakeslack.PinsFolderID)
		return len(r) == 5 && r[0].ID == b.ID && r[1].ID == "Bk0PINSL1"
	})
	// remove
	if err := a.RemoveFolderBookmark(ctx, ch, Bookmark{ID: "Bk0PINSL2", Type: "link"}); err != nil {
		t.Fatal(err)
	}
	rf := fs.PinsFoldersForms("bookmarks.remove")
	if len(rf) != 1 || rf[0].Get("bookmark") != "Bk0PINSL2" || rf[0].Get("type") != "link" || rf[0].Get("reason") != "bookmarks-remove-api" {
		t.Errorf("bookmarks.remove forms %v", rf)
	}
	waitBookmarks(t, a, "the removed link", func(all []Bookmark) bool {
		r, _ := FolderRows(all, fakeslack.PinsFolderID)
		for _, x := range r {
			if x.ID == "Bk0PINSL2" {
				return false
			}
		}
		return len(r) == 4
	})
	// rename the folder tab
	if err := a.RenameFolderTab(ctx, ch, fakeslack.PinsFolderTabID, "Design", "folder_tab_view_header"); err != nil {
		t.Fatal(err)
	}
	uf := fs.PinsFoldersForms("conversations.updateTab")
	if len(uf) != 1 || uf[0].Get("id") != fakeslack.PinsFolderTabID || uf[0].Get("channel_id") != ch || uf[0].Get("label") != "Design" ||
		uf[0].Get("reason") != "folder_tab_view_header" {
		t.Errorf("conversations.updateTab forms %v", uf)
	}
}

// shouldShowBookmark: workflows (shortcuts) aren't rows; a "Workflows"
// folder shows only with something shown in it.
func TestFolderRowsShouldShow(t *testing.T) {
	all := []Bookmark{
		{ID: "F1", Type: "local_folder", Title: "Workflows"},
		{ID: "S1", Type: "shortcut", ParentID: "F1", Rank: "a"},
		{ID: "L1", Type: "link", Rank: "b"},
		{ID: "L2", Type: "link", Rank: "a"},
		{ID: "M1", Type: "pinned_message", Rank: "c"},
	}
	rows, wf := FolderRows(all, "F1")
	if len(rows) != 0 || !wf {
		t.Errorf("F1 rows %v, workflows %v", bmIDs(rows), wf)
	}
	rows, _ = FolderRows(all, "")
	if got := bmIDs(rows); !equalStrings(got, []string{"L2", "L1"}) {
		t.Errorf("loose rows %v", got)
	}
	if shouldShowBookmark(all, all[0]) {
		t.Error("an empty Workflows folder is shown")
	}
}

// The pins refetcher (LoZG, no maxAge) keeps a channel's pins until a
// slow reconnect: a fast reconnect (the gateway's reconnect_url) leaves
// them, any other connect bumps slowConnectionCount and the next read
// clears the rows and runs pins.list again.
func TestPinsRefetchAfterSlowReconnect(t *testing.T) {
	a, fs, l := pfApp(t)
	ch := fakeslack.PinsChannelID
	a.PinsTab(ch)
	waitPins(t, a, "the pins", func(items []PinsTabItem) bool { return len(items) == 4 })
	if n := len(fs.PinsFoldersForms("pins.list")); n != 1 {
		t.Fatalf("%d pins.list calls, want 1", n)
	}
	a.socketConnected(true)
	if _, loaded := a.PinsTab(ch); !loaded {
		t.Fatal("a fast reconnect made the pins stale")
	}
	time.Sleep(50 * time.Millisecond)
	if n := len(fs.PinsFoldersForms("pins.list")); n != 1 {
		t.Fatalf("a fast reconnect refetched: %d pins.list calls", n)
	}
	before := len(l.all())
	a.socketConnected(false)
	l.wait(t, "the pins_tab event after the slow reconnect", func(evs []Event) bool {
		for _, ev := range evs[min(before, len(evs)):] {
			if ev.Type == "pins_tab" && ev.Channel == ch {
				return true
			}
		}
		return false
	})
	if items, loaded := a.PinsTab(ch); loaded || len(items) != 0 {
		t.Fatalf("after a slow reconnect the first read is loading with no rows (clearPinnedItems): loaded %v, %d rows", loaded, len(items))
	}
	waitPins(t, a, "the refetched pins", func(items []PinsTabItem) bool { return len(items) == 4 })
	if f := fs.PinsFoldersForms("pins.list"); len(f) != 2 || f[1].Get("reason") != "fetchPinsList" {
		t.Fatalf("pins.list after the slow reconnect: %v", f)
	}
}
