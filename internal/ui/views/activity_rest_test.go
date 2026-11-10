package views

// Activity's bulk selection, saved views, VIP filter and sort, the
// settings menu's Sort and Density, the keyboard and live updates
// (activity_bulk.go, activity_views.go, activity_keys.go,
// activity_live.go) against internal/fakeslack (Size.Views). Synthetic
// content only; every write is fakeslack's recorded one.

import (
	"context"
	"encoding/json"
	"image"
	"net/url"
	"strings"
	"testing"

	"gioui.org/io/key"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
)

func actLastModal(h *vaHost) *popups.Modal {
	open := h.open()
	if len(open) == 0 {
		return nil
	}
	m, _ := open[len(open)-1].(*popups.Modal)
	return m
}

func actTabsNow(v *Activity) string { return strings.Join(v.tabs.Labels, ",") }

// Custom select is bulk action mode with nothing picked: the bar holds
// only the bulk group, every row leads with its checkbox, the banner
// counts; a checkbox picks, Shift picks the range; "Select everything in
// All" turns on server-side select-all, whose "Clear everything" sends the
// view's filters to activity.clearAll.
func TestActivityTriageLikeSlack(t *testing.T) {
	v, h, e, fs := vaFakeActivity(t, false)
	v.selC[3].Click() // Custom select
	e.frame()
	if !v.triage() || len(v.picked) != 0 {
		t.Fatalf("Custom select: triage %v, %d picked", v.triage(), len(v.picked))
	}
	if txt, link := v.bannerTexts(); txt != "0 items selected on page." || link != "" {
		t.Errorf("banner %q / %q", txt, link)
	}
	keys := v.listKeys()
	if len(keys) < 6 {
		t.Fatalf("%d rows", len(keys))
	}
	r := v.rowFor(keys[1])
	r.check.Click()
	e.frame()
	if !v.picked[keys[1]] || v.lastBulk != keys[1] {
		t.Fatalf("checkbox: picked %v last %q", v.picked, v.lastBulk)
	}
	v.pickRow(keys[4], true) // Shift: the range keys[1]..keys[4]
	for _, k := range keys[1:5] {
		if !v.picked[k] {
			t.Errorf("range: %s not picked", k)
		}
	}
	if txt, link := v.bannerTexts(); txt != "4 items selected on page." || link != "Select everything in All" {
		t.Errorf("banner %q / %q", txt, link)
	}
	vaShot(t, e, "activity-triage", false)
	v.dense, v.rows, v.dirty = true, map[string]*actRow{}, true
	e.frame()
	vaShot(t, e, "activity-triage-dense", false)
	v.dense, v.rows, v.dirty = false, map[string]*actRow{}, true
	e.frame()
	// a row click still opens the row
	it := actItem(t, v, keys[2])
	v.rowFor(keys[2]).click.Click()
	e.frame()
	if !v.selOK || v.selKey != actKey(it) || !v.picked[keys[2]] {
		t.Errorf("row click in triage mode: open %v %q, still picked %v", v.selOK, v.selKey, v.picked[keys[2]])
	}
	// Shift on a picked row unpicks the range
	v.pickRow(keys[3], true)
	if v.picked[keys[3]] || v.picked[keys[4]] {
		t.Errorf("Shift on a picked row left %v", v.picked)
	}
	v.bannerLink.Click()
	e.frame()
	if !v.serverAll || len(v.picked) != len(keys) {
		t.Fatalf("select everything: serverAll %v, %d picked of %d", v.serverAll, len(v.picked), len(keys))
	}
	if txt, link := v.bannerTexts(); txt != "Everything in All is selected." || link != "" {
		t.Errorf("banner %q / %q", txt, link)
	}
	vaShot(t, e, "activity-select-everything", false)
	n := len(h.open())
	v.bulkClear.Click()
	e.frame()
	if len(h.open()) != n+1 {
		t.Fatal("Clear everything opened no dialog")
	}
	vaShot(t, e, "activity-clear-everything", false)
	actLastModal(h).PressDefault()
	e.until(t, "activity.clearAll with the filters", func() bool {
		return actFakeWrite(fs, "activity.clearAll", func(f url.Values) bool {
			return f.Get("reason") == "server-side-select-all-clear" && f.Get("filters") == `{"read_state":"reads_and_unreads"}`
		})
	})
	e.until(t, "the selection to end", func() bool { return !v.triage() })
}

// The bulk buttons write once with every entry; Clear has no toast with
// the activity inbox; "Mark selected as unread" only with the
// experiment.
func TestActivityBulkWritesLikeSlack(t *testing.T) {
	v, h, e, fs := vaFakeActivity(t, false)
	keys := v.listKeys()
	v.pickRow(keys[0], false)
	v.pickRow(keys[1], false)
	e.frame()
	v.bulkClear.Click()
	e.frame()
	e.until(t, "one bulk activity.archive", func() bool {
		return actFakeWrite(fs, "activity.archive", func(f url.Values) bool {
			return f.Get("reason") == "bulk_archive" && strings.Count(f.Get("entries"), `"key"`) == 2
		})
	})
	for _, s := range h.toasts() {
		if strings.Contains(s, "cleared") {
			t.Errorf("a clear toast %q (Slack has none with the activity inbox)", s)
		}
	}
	if v.bulkUnreadTool() {
		t.Fatal("activity_bulk_unread_tool is on in the fixture")
	}
	v.h.Core().SetExperimentGroup("activity_bulk_unread_tool", "on")
	keys = v.listKeys()
	v.pickRow(keys[0], false)
	e.frame()
	v.bulkUnread.Click()
	e.frame()
	e.until(t, "one bulk activity.markUnread", func() bool {
		return actFakeWrite(fs, "activity.markUnread", func(f url.Values) bool { return f.Get("reason") == "bulk_mark_unread" })
	})
}

// The "+" menu suggests what no view has yet (Unreads, Reactions, the
// sidebar's Starred and sections); a suggestion is a saved view
// (activity.views.create) shown at once; "Create a new view" saves its
// own filters; Rename and Delete go through activity.views.update /
// delete; Hide view hides a default tab, which the "+" menu brings back.
func TestActivityCustomViewsLikeSlack(t *testing.T) {
	v, h, e, fs := vaFakeActivity(t, false)
	e.until(t, "the saved views", func() bool { return v.viewsLoaded })
	if got := actTabsNow(v); got != "All,DMs,Mentions,Threads" {
		t.Fatalf("tabs %s", got)
	}
	// the sidebar's Starred and a custom section (fakeslack's ids)
	st := v.h.Core().S
	st.Lock()
	st.Sections = []slack.Section{{ID: "L0FAKE00", Name: "Starred", Type: "stars"}, {ID: "L0FAKE01", Name: "Synthetic projects", Type: "standard"}}
	st.Unlock()
	acts, secs := v.suggestions()
	var names []string
	for _, g := range acts {
		names = append(names, g.name)
	}
	if strings.Join(names, ",") != "Unreads,Reactions" {
		t.Errorf("default suggestions %v", names)
	}
	if len(secs) != 2 || secs[0].name != "Starred" || secs[0].icon != "star" || secs[1].name != "Synthetic projects" ||
		secs[1].icon != "section" || len(secs[1].st.sections) != 1 || secs[1].st.sections[0] != "L0FAKE01" {
		t.Errorf("sidebar suggestions %+v", secs)
	}
	v.plus.Click()
	e.frame()
	if v.menu != actMenuPlus {
		t.Fatalf("plus: menu %d", v.menu)
	}
	vaShot(t, e, "activity-plus-menu", false)
	var reactions func()
	for _, it := range v.plusItems() {
		if it.label == "Reactions" {
			reactions = it.do
		}
	}
	v.menu = actMenuNone // as the row's click does
	reactions()
	e.until(t, "the Reactions view", func() bool { return actTabsNow(v) == "All,DMs,Mentions,Threads,Reactions" })
	if !actFakeWrite(fs, "activity.views.create", func(f url.Values) bool {
		return f.Get("name") == "Reactions" && f.Get("filters") == `{"entry_types":["message_reaction"]}` && f.Get("sort") == "newest"
	}) {
		t.Error("activity.views.create for the suggestion")
	}
	if vw, ok := v.customView(); !ok || vw.Name != "Reactions" || !v.filters[actFilterReactions] {
		t.Errorf("shown view %+v %v, reactions row %v", vw, ok, v.filters[actFilterReactions])
	}
	e.until(t, "the reactions feed", func() bool { return len(actPage(v).Items) > 0 })
	for _, it := range actPage(v).Items {
		if it.Type != "message_reaction" {
			t.Errorf("%s in the Reactions view", it.Type)
		}
	}
	vaShot(t, e, "activity-custom-tab", false)
	// Create a custom view: name, Only include unreads, Threads
	v.openViewModal(actCreateTab, core.ActivityView{})
	e.frame()
	m := actLastModal(h)
	if m == nil {
		t.Fatal("no Create a new view modal")
	}
	vaShot(t, e, "activity-create-view", false)
	// the modal's state through its own fields
	mm := v.lastViewModal
	mm.ed.SetText("Synthetic threads")
	mm.st.unread = true
	mm.st.filters[2] = true
	if !mm.canSave() {
		t.Fatalf("can't save: %q", mm.alert())
	}
	m.PressDefault()
	e.until(t, "the new view", func() bool { return strings.HasSuffix(actTabsNow(v), ",Synthetic threads") })
	if !actFakeWrite(fs, "activity.views.create", func(f url.Values) bool {
		return f.Get("name") == "Synthetic threads" && f.Get("filters") == `{"entry_types":["thread_v2","thread_reply"],"unread_only":true}`
	}) {
		t.Error("activity.views.create for Create a new view")
	}
	// a taken name
	v.openViewModal(actCreateTab, core.ActivityView{})
	e.frame()
	v.lastViewModal.ed.SetText("Reactions")
	if v.lastViewModal.alert() != "There’s already a view with this name." || v.lastViewModal.canSave() {
		t.Errorf("taken name: %q", v.lastViewModal.alert())
	}
	actLastModal(h).Dismiss()
	e.frame()
	// Rename the shown view
	vw, _ := v.customView()
	v.openViewModal(actRenameView, vw)
	e.frame()
	v.lastViewModal.ed.SetText("Synthetic replies")
	actLastModal(h).PressDefault()
	e.until(t, "the renamed tab", func() bool { return strings.HasSuffix(actTabsNow(v), ",Synthetic replies") })
	if !actFakeWrite(fs, "activity.views.update", func(f url.Values) bool {
		return f.Get("view_id") == vw.ID && f.Get("name") == "Synthetic replies"
	}) {
		t.Error("activity.views.update for the rename")
	}
	// Delete it: back to All
	vw, _ = v.customView()
	v.confirmDeleteView(vw)
	e.frame()
	actLastModal(h).PressDefault()
	e.until(t, "the deleted tab", func() bool { return actTabsNow(v) == "All,DMs,Mentions,Threads,Reactions" })
	if !actFakeWrite(fs, "activity.views.delete", func(f url.Values) bool { return f.Get("view_id") == vw.ID }) {
		t.Error("activity.views.delete")
	}
	if v.slot() != 0 {
		t.Errorf("after deleting the shown view: slot %d", v.slot())
	}
	// Hide Mentions, bring it back from the "+" menu
	v.confirmHideView("mentions")
	e.frame()
	actLastModal(h).PressDefault()
	e.frame()
	if actTabsNow(v) != "All,DMs,Threads,Reactions" {
		t.Errorf("after hiding Mentions: %s", actTabsNow(v))
	}
	e.until(t, "activity.views.hide", func() bool {
		return actFakeWrite(fs, "activity.views.hide", func(f url.Values) bool { return f.Get("view_type") == "mentions" })
	})
	items := v.plusItems()
	if items[0].header != "Hidden views" || items[1].label != "Mentions" {
		t.Fatalf("plus menu %+v", items[:2])
	}
	items[1].do()
	e.until(t, "activity.views.unhide", func() bool {
		return actFakeWrite(fs, "activity.views.unhide", func(f url.Values) bool { return f.Get("view_type") == "mentions" })
	})
	if actTabsNow(v) != "All,DMs,Mentions,Threads,Reactions" {
		t.Errorf("after unhiding: %s", actTabsNow(v))
	}
	// the filter menu's Create Custom View saves the current filters
	v.filters[2] = true
	if !v.hasUnsavedChanges() {
		t.Fatal("no unsaved changes with Threads on")
	}
	v.menu = actMenuFilter
	e.frame()
	vaShot(t, e, "activity-filter-create-view", false)
	v.openViewModal(actSaveView, core.ActivityView{})
	e.frame()
	v.lastViewModal.ed.SetText("Synthetic saved")
	actLastModal(h).PressDefault()
	e.until(t, "the saved view", func() bool { return strings.HasSuffix(actTabsNow(v), ",Synthetic saved") })
	if !actFakeWrite(fs, "activity.views.create", func(f url.Values) bool {
		return f.Get("name") == "Synthetic saved" && f.Get("filters") == `{"entry_types":["thread_v2","thread_reply"]}`
	}) {
		t.Error("activity.views.create for Save this view")
	}
}

// The VIP filter asks for priority_only; VIP unreads on top (the
// settings menu's Sort with activity_in_home_prototype) writes the global
// sort, asks for priority_unreads_first_v1 and lists the unread VIP
// items under "VIP unreads", then "Everything else"; Density writes the
// global density.
func TestActivityVIPAndSortLikeSlack(t *testing.T) {
	v, _, e, fs := vaFakeActivity(t, false)
	if err := v.h.Core().AddVIP(context.Background(), "U0FAKE00001"); err != nil {
		t.Fatal(err)
	}
	got := make(chan url.Values, 8)
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "activity.feed" {
			select {
			case got <- f:
			default:
			}
		}
		return nil, false
	})
	v.extraC[0].Click() // VIP
	e.frame()
	e.until(t, "the VIP feed", func() bool {
		for {
			select {
			case f := <-got:
				if f.Get("priority_only") == "true" {
					return true
				}
			default:
				return false
			}
		}
	})
	e.until(t, "the VIP items", func() bool { return len(actPage(v).Items) == 2 })
	if v.filterCount() != 1 {
		t.Errorf("filter count %d", v.filterCount())
	}
	v.extraC[0].Click()
	e.frame()
	e.until(t, "the whole feed", func() bool { return len(actPage(v).Items) > 2 })
	// the settings menu's sections behind activity_in_home_prototype
	if v.settingsSortOn() {
		t.Fatal("Sort section without the prototype flag")
	}
	v.h.Core().SetExperimentGroup("activity_in_home_prototype", "on")
	v.menu = actMenuSettings
	e.frame()
	vaShot(t, e, "activity-settings-sections", false)
	v.setSortC[1].Click()
	e.frame()
	e.until(t, "activity.prefs.update sort", func() bool {
		return actFakeWrite(fs, "activity.prefs.update", func(f url.Values) bool { return f.Get("sort") == "vip_unreads_first" })
	})
	e.until(t, "the VIP order", func() bool { return len(v.vis) > 0 && v.vis[0] == actVisVIP })
	page := actPage(v)
	if i := v.vis[1]; i < 0 || !page.Items[i].Priority || !page.Items[i].Unread {
		t.Errorf("first row under VIP unreads: %+v", page.Items[i])
	}
	found := false
	for _, i := range v.vis {
		found = found || i == actVisElse
	}
	if !found {
		t.Error("no Everything else header")
	}
	vaShot(t, e, "activity-vip-sort", false)
	v.menu = actMenuSettings
	e.frame()
	v.setDenseC[1].Click()
	e.frame()
	e.until(t, "activity.prefs.update density", func() bool {
		return actFakeWrite(fs, "activity.prefs.update", func(f url.Values) bool { return f.Get("density") == "dense" })
	})
	if !v.dense {
		t.Error("Single-line rows didn't switch to Dense")
	}
	// without the prototype the header carries the sort (VIPs, global settings)
	v.h.Core().SetExperimentGroup("activity_in_home_prototype", "off")
	if v.globalSettings() && !v.headerSort() {
		t.Error("no header sort with VIPs")
	}
}

// The list's keys: Down focuses the first row and moves on, Enter opens
// the focused one, U marks it read or unread, X picks it, C clears it,
// ⌘Z restores it, Alt+Down opens the next row.
func TestActivityKeyboardLikeSlack(t *testing.T) {
	v, _, e, fs := vaFakeActivity(t, false)
	keys := v.listKeys()
	press := func(name key.Name, mods key.Modifiers) {
		v.key(layout.Context{}, key.Event{Name: name, Modifiers: mods, State: key.Press})
		e.frame()
	}
	v.kbFocused = true // as the list has the focus when its keys come
	press(key.NameDownArrow, 0)
	if v.focusKey != keys[0] || !v.kbMode {
		t.Fatalf("Down: focus %q, want %q", v.focusKey, keys[0])
	}
	press(key.NameDownArrow, 0)
	press(key.NameDownArrow, 0)
	press(key.NameUpArrow, 0)
	if v.focusKey != keys[1] {
		t.Fatalf("Down Down Up: focus %q, want %q", v.focusKey, keys[1])
	}
	vaShot(t, e, "activity-keyboard-focus", false)
	press(key.NameReturn, 0)
	if !v.selOK || v.selKey != keys[1] {
		t.Fatalf("Enter: open %v %q", v.selOK, v.selKey)
	}
	it := actItem(t, v, keys[1])
	was := v.isUnread(it, keys[1])
	press("U", 0)
	if v.isUnread(it, keys[1]) == was {
		t.Error("U didn't change the read state")
	}
	press("X", 0)
	if !v.picked[keys[1]] || !v.triage() {
		t.Error("X didn't pick the row")
	}
	press(key.NameSpace, 0)
	if v.picked[keys[1]] {
		t.Error("Space in triage mode didn't unpick the row")
	}
	press("C", 0)
	if !v.cleared[keys[1]] {
		t.Fatal("C didn't clear the row")
	}
	e.until(t, "activity.archive", func() bool {
		return actFakeWrite(fs, "activity.archive", func(f url.Values) bool { return f.Get("key") == keys[1] })
	})
	press("Z", key.ModShortcut)
	e.until(t, "activity.unarchive", func() bool {
		return actFakeWrite(fs, "activity.unarchive", func(f url.Values) bool { return f.Get("key") == keys[1] })
	})
	press(key.NameDownArrow, key.ModAlt)
	if !v.selOK || v.selKey == keys[1] {
		t.Errorf("Alt+Down: open %v %q", v.selOK, v.selKey)
	}
	// a press on a row focuses the list (the keys' target)
	v.selOK, v.kbFocused = false, false
	e.frame()
	e.click(image.Pt(300, int(actHeadH+actBarTop+actBarH+16)+60))
	e.frame()
	if !v.kbFocused {
		t.Fatal("a press on the list didn't focus it")
	}
}

// Realtime activity_updated: a new entry goes in at its feed_ts, an entry
// read elsewhere turns read, one cleared elsewhere leaves; a new unread
// entry while scrolled down raises "New activity"; activity_views_updated
// brings a view made in another client.
func TestActivityLiveUpdatesLikeSlack(t *testing.T) {
	v, _, e, _ := vaFakeActivity(t, false)
	app := v.h.Core()
	inject := func(sub string, entry map[string]any) {
		raw, _ := json.Marshal(map[string]any{"type": "activity", "subtype": sub, "entry": entry})
		app.Inject(slack.Event{Type: "activity", Subtype: sub, Raw: raw})
	}
	n := len(actPage(v).Items)
	inject("activity_updated", map[string]any{"key": "fa-live", "feed_ts": "1762702100.000100", "is_unread": true,
		"item": map[string]any{"type": "at_user", "message": map[string]any{"ts": "1762700100.000100", "channel": fakeslack.ViewsChannel, "author_user_id": "U0FAKE00001"}}})
	e.until(t, "the live entry", func() bool { return len(actPage(v).Items) == n+1 })
	if k := actKey(&actPage(v).Items[0]); k != "fa-live" {
		t.Errorf("first row %s, want the live entry", k)
	}
	inject("activity_updated", map[string]any{"key": "fa-live", "feed_ts": "1762702100.000100", "is_unread": false,
		"item": map[string]any{"type": "at_user", "message": map[string]any{"ts": "1762700100.000100", "channel": fakeslack.ViewsChannel, "author_user_id": "U0FAKE00001"}}})
	e.until(t, "the live entry read", func() bool { return !actPage(v).Items[0].Unread })
	inject("activity_updated", map[string]any{"key": "fa-live", "feed_ts": "1762702100.000100", "is_unread": false, "is_archived": true,
		"item": map[string]any{"type": "at_user", "message": map[string]any{"ts": "1762700100.000100", "channel": fakeslack.ViewsChannel, "author_user_id": "U0FAKE00001"}}})
	e.until(t, "the cleared entry gone", func() bool { return len(actPage(v).Items) == n })
	// under the VIP filter a non-VIP entry stays out
	v.vip = true
	inject("activity_updated", map[string]any{"key": "fa-live-2", "feed_ts": "1762702200.000100", "is_unread": true,
		"item": map[string]any{"type": "keyword", "message": map[string]any{"ts": "1762700400.000100", "channel": fakeslack.ViewsChannel, "author_user_id": "U0FAKE00002"}}})
	for range 20 {
		e.frame()
	}
	for _, it := range actPage(v).Items {
		if it.Key == "fa-live-2" {
			t.Error("a non-VIP entry went into the VIP filter's list")
		}
	}
	v.vip = false
	// scrolled down: the banner
	v.list.Position.First = 3
	inject("activity_updated", map[string]any{"key": "fa-live-3", "feed_ts": "1762702300.000100", "is_unread": true,
		"item": map[string]any{"type": "keyword", "message": map[string]any{"ts": "1762700400.000100", "channel": fakeslack.ViewsChannel, "author_user_id": "U0FAKE00002"}}})
	e.until(t, "New activity", func() bool { return v.newUnreadTS == "1762702300.000100" })
	vaShot(t, e, "activity-new-activity", false)
	v.bannerNew.Click()
	e.frame()
	if v.list.Position.First != 0 || v.newUnreadTS != "" {
		t.Errorf("the banner's click: first %d, ts %q", v.list.Position.First, v.newUnreadTS)
	}
	// a view made elsewhere
	raw := []byte(`{"type":"activity_views_updated","views":[{"id":"Vw0FAKE0","view_type":"all","position":"0"},{"id":"Vw0FAKE1","view_type":"dms","position":"1"},
		{"id":"Vw0FAKE2","view_type":"mentions","position":"2"},{"id":"Vw0FAKE3","view_type":"threads","position":"3"},
		{"id":"VwELSEWHERE","view_type":"custom","name":"Synthetic elsewhere","emoji":":tada:","position":"4","filters":{"unread_only":true}}]}`)
	app.Inject(slack.Event{Type: "activity_views_updated", Raw: raw})
	e.until(t, "the view from elsewhere", func() bool { return strings.HasSuffix(actTabsNow(v), ",Synthetic elsewhere") })
}
