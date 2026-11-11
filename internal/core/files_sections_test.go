package core

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// File collections (custom_file_sections): files.collections.list with
// the names unescaped and the starred one first by position; a move and
// its revert; create, rename, sort and delete as Slack's thunks send them;
// the collapsed state the client keeps across a reload.
func TestFilesViewCollections(t *testing.T) {
	a, fs := filesViewFake(t)
	ctx := context.Background()
	launch := fs.AddFileCollection("Q&amp;A", ":rocket:", fakeslack.CanvasShared)
	if err := a.FetchFileCollections(ctx); err != nil {
		t.Fatal(err)
	}
	if cs, ok := a.FileCollections(); ok || len(cs) != 0 {
		t.Fatalf("custom_file_sections off: %d collections, loaded %v", len(cs), ok)
	}
	a.SetExperimentGroup("custom_file_sections", "on")
	if err := a.FetchFileCollections(ctx); err != nil {
		t.Fatal(err)
	}
	cs, ok := a.FileCollections()
	if !ok || len(cs) != 2 || !cs[0].Starred() || cs[1].Name != "Q&A" || cs[1].Emoji != ":rocket:" || cs[1].SortOrDefault() != CollectionSortDateAdded {
		t.Fatalf("collections %+v", cs)
	}
	if c, in := a.FileCollectionOf(fakeslack.CanvasShared); !in || c.ID != launch {
		t.Errorf("FileCollectionOf = %+v %v", c, in)
	}
	// move to Starred: out of the section, into the starred collection
	a.AddOpenFile(fakeslack.CanvasShared)
	from, err := a.MoveFileToCollection(ctx, fakeslack.CanvasShared, fakeslack.StarredCollectionID)
	if err != nil || from != launch {
		t.Fatalf("move: from %q, %v", from, err)
	}
	if c, _ := a.FileCollectionOf(fakeslack.CanvasShared); c.ID != fakeslack.StarredCollectionID {
		t.Errorf("after the move it's in %q", c.ID)
	}
	if ids := a.RecentlyViewedFileIDs(); len(ids) != 0 {
		t.Errorf("the moved file stayed in Recently viewed: %v", ids)
	}
	calls := fs.FilesViewCalls()
	if last := calls[len(calls)-1]; last.Method != "files.favorites.add" || last.Args.Get("collection_id") != fakeslack.StarredCollectionID ||
		last.Args.Get("file_id") != fakeslack.CanvasShared || last.Args.Has("reason") {
		t.Errorf("move call %+v", last)
	}
	// a failed move puts it back
	fs.SetFault("files.favorites.add", fakeslack.Fault{Error: "internal_error", Times: 1})
	if _, err := a.MoveFileToCollection(ctx, fakeslack.CanvasShared, launch); err == nil {
		t.Fatal("the faulted move succeeded")
	}
	if c, _ := a.FileCollectionOf(fakeslack.CanvasShared); c.ID != fakeslack.StarredCollectionID {
		t.Errorf("after the failed move it's in %q", c.ID)
	}
	// create / rename / sort / delete
	c, err := a.CreateFileCollection(ctx, "Design", "")
	if err != nil || c.ID == "" || c.Name != "Design" {
		t.Fatalf("create %+v %v", c, err)
	}
	if err := a.RenameFileCollection(ctx, c.ID, "Design docs", ":art:"); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.FileCollection(c.ID); got.Name != "Design docs" || got.Emoji != ":art:" {
		t.Errorf("renamed %+v", got)
	}
	if err := a.SetFileCollectionSort(ctx, c.ID, CollectionSortAlpha); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.FileCollection(c.ID); got.Sort != CollectionSortAlpha {
		t.Errorf("sort %q", got.Sort)
	}
	a.ToggleFileCollectionCollapsed(c.ID)
	a.fileCollections().fetched = a.fileCollections().fetched.AddDate(0, 0, -2) // a day later: refetched
	if err := a.FetchFileCollections(ctx); err != nil {
		t.Fatal(err)
	}
	if !a.FileCollectionCollapsed(c.ID) {
		t.Error("the reload dropped the client's collapsed state")
	}
	if err := a.DeleteFileCollection(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.FileCollection(c.ID); ok {
		t.Error("the deleted collection stayed")
	}
	var methods []string
	for _, call := range fs.FilesViewCalls() {
		if strings.HasPrefix(call.Method, "files.collections.") {
			methods = append(methods, call.Method+":"+call.Args.Get("name")+call.Args.Get("sort"))
		}
	}
	if got := strings.Join(methods, " "); got != "files.collections.create:Design files.collections.update:Design docs files.collections.update:alpha files.collections.delete:" {
		t.Errorf("collection calls %q", got)
	}
	// Recently viewed's collapsed state is the client's flag
	a.ToggleFileCollectionCollapsed(RecentlyViewedCollection)
	if !a.FileCollectionCollapsed(RecentlyViewedCollection) {
		t.Error("Recently viewed didn't collapse")
	}
	a.SetAllFileCollectionsCollapsed(true)
	if a.FileCollectionCollapsed(RecentlyViewedCollection) || !a.FileCollectionCollapsed(launch) {
		t.Error("collapseAllCollections: Recently viewed stays open, the rest collapse")
	}
}

// filesOpenInSessionStore: new files go to the top of the display order,
// reopened ones keep their place; past twenty the least recently opened
// leaves; the sidebar shows the ten most recently opened in display
// order; the saver sees every change.
func TestFilesViewOpenFiles(t *testing.T) {
	a, _ := filesViewFake(t)
	saves := 0
	a.SetOpenFilesSaver(func(display, cache []string) { saves++ })
	id := func(i int) string { return "F0OPEN" + strconv.Itoa(1000+i) }
	a.AddOpenFile("not-a-file") // looksLikeFileId
	a.AddOpenFile(id(1))
	a.AddOpenFile(id(2))
	a.AddOpenFile(id(1)) // reopened: the display order keeps it second
	if got := strings.Join(a.RecentlyViewedFileIDs(), " "); got != id(2)+" "+id(1) {
		t.Errorf("display %q", got)
	}
	for i := 3; i <= 22; i++ {
		a.AddOpenFile(id(i))
	}
	got := a.RecentlyViewedFileIDs()
	if len(got) != 10 || got[0] != id(22) || got[9] != id(13) {
		t.Errorf("Recently viewed %v", got)
	}
	s := a.openFiles()
	if len(s.display) != 20 || len(s.cache) != 20 {
		t.Errorf("store %d / %d, want 20", len(s.display), len(s.cache))
	}
	a.RemoveOpenFile(id(22))
	if got := a.RecentlyViewedFileIDs(); got[0] != id(21) {
		t.Errorf("after remove %v", got)
	}
	if saves != 24 { // every accepted add (23) and the remove; not the bad id
		t.Errorf("saves %d, want 24", saves)
	}
	// rehydrate: ids only in the display order join the cache order
	b, _ := filesViewFake(t)
	b.RehydrateOpenFiles([]string{id(1), id(2)}, []string{id(2)})
	if got := strings.Join(b.RecentlyViewedFileIDs(), " "); got != id(1)+" "+id(2) {
		t.Errorf("rehydrated %q", got)
	}
	// FileOpened: starred files (or with sections, files in one) stay out
	c, fs := filesViewFake(t)
	c.SetFileStarred(fakeslack.CanvasDM, true)
	c.FileOpened(fakeslack.CanvasDM)
	c.FileOpened(fakeslack.CanvasShared)
	if got := strings.Join(c.RecentlyViewedFileIDs(), " "); got != fakeslack.CanvasShared {
		t.Errorf("FileOpened %q", got)
	}
	fs.AddFileCollection("Launch", "", fakeslack.CanvasChannel)
	c.SetExperimentGroup("custom_file_sections", "on")
	if err := c.FetchFileCollections(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.FileOpened(fakeslack.CanvasChannel)
	if got := strings.Join(c.RecentlyViewedFileIDs(), " "); got != fakeslack.CanvasShared {
		t.Errorf("a file in a section joined Recently viewed: %q", got)
	}
}

// generateKeyBetween(a, null) and sortCollectionFiles.
func TestFilesViewOrderKeys(t *testing.T) {
	for in, want := range map[string]string{"": "5000000000", "5000000000": "5000000001", "0000000041.V": "0000000042"} {
		if got := OrderKeyAfter(in); got != want {
			t.Errorf("OrderKeyAfter(%q) = %q, want %q", in, got, want)
		}
	}
	files := []CollectionFile{{ID: "F1", Position: "5000000001"}, {ID: "F2", Position: "5000000003"}, {ID: "F3", Position: "5000000002"}}
	meta := map[string]File{
		"F1": {ID: "F1", Title: "beta", Modified: 300, LastRead: 1700000000000},
		"F2": {ID: "F2", Title: "⭐ alpha", Modified: 100, LastRead: 1600000000},
	}
	look := func(id string) (File, bool) { f, ok := meta[id]; return f, ok }
	ids := func(fs []CollectionFile) string {
		var out []string
		for _, f := range fs {
			out = append(out, f.ID)
		}
		return strings.Join(out, " ")
	}
	for sortBy, want := range map[string]string{
		CollectionSortDateAdded:      "F2 F3 F1",
		CollectionSortRecentlyViewed: "F1 F2 F3",
		CollectionSortLastUpdated:    "F1 F2 F3",
		CollectionSortAlpha:          "F2 F1 F3",
	} {
		if got := ids(SortCollectionFiles(files, sortBy, look)); got != want {
			t.Errorf("%s: %s, want %s", sortBy, got, want)
		}
	}
}

// The browser's fileType filter and sort reach search.modules.files.
func TestFilesViewSearchParams(t *testing.T) {
	p := filesParams(FilesOpts{Types: []string{"quip", "images"}, Sort: "timestamp", Page: 2}, "U1")
	if q := p.Get("query"); q != "type:quip type:images" || p.Get("sort") != "timestamp" || p.Get("page") != "2" {
		t.Errorf("params %v", p)
	}
	if p := filesParams(FilesOpts{Query: "plan"}, "U1"); p.Get("sort") != "score" || !strings.HasPrefix(p.Get("query"), fileTypesAll) {
		t.Errorf("search params %v", p)
	}
}

// isOnFreePlan: no plan and not an enterprise team, once team.info said.
func TestFilesViewFreePlan(t *testing.T) {
	a, _ := filesViewFake(t)
	if a.OnFreePlan() {
		t.Error("free before team.info answered")
	}
	a.setTeamPlan("", "")
	if !a.OnFreePlan() && a.C.Creds.EnterpriseID == "" {
		t.Error("no plan, no enterprise: not free")
	}
	a.setTeamPlan("std", "")
	if a.OnFreePlan() {
		t.Error("a paid plan counted as free")
	}
}
