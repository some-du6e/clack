package listview

// The header's More actions (more.go): Slack's MoreActionsMenu items, the
// download per lists_export_enrichment, Make a copy, Delete list.

import (
	"strings"
	"testing"
	"time"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
)

func moreLabels(items []MenuItem) string {
	var ls []string
	for _, it := range items {
		switch {
		case it.Divider:
			ls = append(ls, "─")
		case len(it.Sub) > 0:
			ls = append(ls, it.Label+" ▸ ("+moreLabels(it.Sub)+")")
		default:
			ls = append(ls, it.Label)
		}
	}
	return strings.Join(ls, ", ")
}

func (e *pickEnv) openListView(id string, m Mode, act Actions) *View {
	v := New(e.h, id, m, act)
	deadline := time.Now().Add(20 * time.Second)
	for v.Doc() == nil {
		if time.Now().After(deadline) {
			e.t.Fatal("the list didn't load")
		}
		e.listFrame(v)
		time.Sleep(10 * time.Millisecond)
	}
	e.listFrame(v)
	return v
}

func (e *pickEnv) listFrame(v *View) {
	ops := new(op.Ops)
	v.Layout(gtxFor(ops, e.sz))
	popups.Pump()
}

func (e *pickEnv) waitList(v *View, what string, cond func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			e.t.Fatalf("timed out waiting for %s", what)
		}
		e.listFrame(v)
		time.Sleep(10 * time.Millisecond)
	}
}

func TestMoreActionsMenu(t *testing.T) {
	e := newPickEnv(t, false)
	t.Setenv("HOME", t.TempDir()) // downloads land in a temporary Downloads folder
	act := Actions{SideView: func() {}, Copy: func(string) {}, Menu: func([]MenuItem) {}}
	v := e.openListView(fakeslack.ListKitchen, Full, act)

	// lists_export_enrichment on (the account): Download opens the modal
	if got := moreLabels(v.moreItems()); got != "Add to starred, ─, Save for later, ─, Copy link to list, Make a copy, Download, ─, Open in split view" {
		t.Errorf("menu, on: %s", got)
	}
	d := v.Doc()
	m, buttons := v.downloadModal(d)
	if len(buttons) != 3 || buttons[1].Label != "Download JSON" || buttons[2].Label != "Download CSV" {
		t.Fatalf("modal buttons %+v", buttons)
	}
	if !buttons[2].Enabled() {
		t.Error("Download CSV disabled with nothing switched on")
	}
	m.threads = true
	if buttons[2].Enabled() {
		t.Error("Download CSV enabled with message threads on")
	}
	m.archived = true
	buttons[1].Do() // Download JSON
	e.waitList(v, "the JSON download", func() bool { return e.h.lastToast() == "JSON downloaded successfully" })
	dls := e.fs.ListDownloads()
	if len(dls) != 1 || !strings.Contains(dls[0], "/files-pri/"+fakeslack.TeamID+"-"+fakeslack.ListKitchen+"/json/") ||
		!strings.Contains(dls[0], "lists_include_archived=true") || !strings.Contains(dls[0], "lists_include_threads=true") ||
		!strings.Contains(dls[0], "origin_team="+fakeslack.TeamID) || strings.Contains(dls[0], "attachments") {
		t.Errorf("JSON download %v", dls)
	}

	// off: Download CSV ▸ without / with archived items
	e.h.a.SetExperimentGroup("lists_export_enrichment", "off")
	items := v.moreItems()
	if got := moreLabels(items); got != "Add to starred, ─, Save for later, ─, Copy link to list, Make a copy, Download CSV ▸ (Download without archived items, Download with archived items), ─, Open in split view" {
		t.Errorf("menu, off: %s", got)
	}
	for _, it := range items {
		if it.Label == "Download CSV" {
			it.Sub[1].Do()
		}
	}
	e.waitList(v, "the CSV download", func() bool { return len(e.fs.ListDownloads()) == 2 })
	e.waitList(v, "the CSV toast", func() bool { return e.h.lastToast() == "CSV downloaded successfully" })
	if dl := e.fs.ListDownloads()[1]; !strings.Contains(dl, "/csv/launch_tracker.csv?") || !strings.Contains(dl, "lists_include_archived=true") {
		t.Errorf("CSV download %s", dl)
	}
	e.h.a.ClearExperimentGroup("lists_export_enrichment")

	// Make a copy: lists.create with the source, then the copy opens
	v.makeCopy(d)
	e.waitList(v, "the copy", func() bool {
		e.h.mu.Lock()
		defer e.h.mu.Unlock()
		return len(e.h.routes) > 0
	})
	cs := e.fs.ListCreates()
	if len(cs) != 1 || cs[0].Get("copy_from_list_id") != fakeslack.ListKitchen || cs[0].Get("name") != "Copy of Launch tracker" ||
		cs[0].Get("reason") != "create-list-from-copy-list-menu" {
		t.Errorf("lists.create %v", cs)
	}
	e.h.mu.Lock()
	route := e.h.routes[len(e.h.routes)-1]
	e.h.mu.Unlock()
	if !strings.HasPrefix(route, "lists/F0LISTNEW") {
		t.Errorf("opened %q", route)
	}

	// star and Later
	for _, it := range v.moreItems() {
		if it.Label == "Add to starred" {
			it.Do()
		}
	}
	e.waitList(v, "the star", func() bool { return e.h.a.FileStarred(fakeslack.ListKitchen) })
	if got := v.moreItems()[0].Label; got != "Remove from starred" {
		t.Errorf("starred: %q", got)
	}

	// your own list can be deleted
	own := e.openListView(fakeslack.ListTodos, Side, Actions{Close: func() {}, Menu: func([]MenuItem) {}})
	if got := moreLabels(own.moreItems()); !strings.HasSuffix(got, "─, Delete list") || strings.Contains(got, "split view") {
		t.Errorf("own list, side panel: %s", got)
	}
}
