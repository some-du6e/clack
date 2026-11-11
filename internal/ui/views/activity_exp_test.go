package views

import (
	"encoding/json"
	"image"
	"net/url"
	"strings"
	"sync"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// The Activity experiments (activity_filters.go, activity_in.go,
// activity_tabs.go, activity_panel.go) against internal/fakeslack, in
// the account's groups and with each group flipped. Synthetic content
// only.

// actFilterLabels is the menu's list as labels, "—" for a rule.
func actFilterLabels(v *Activity, m actMenuList) string {
	var out []string
	for _, e := range m.entries {
		if e.opt == optDivider {
			out = append(out, "—")
			continue
		}
		out = append(out, v.entryLabel(e))
	}
	return strings.Join(out, "|")
}

// actFeedForms records activity.feed's forms.
type actFeedForms struct {
	mu    sync.Mutex
	forms []url.Values
}

func (r *actFeedForms) hook(fs *fakeslack.Server) {
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "activity.feed" {
			r.mu.Lock()
			r.forms = append(r.forms, f)
			r.mu.Unlock()
		}
		return nil, false
	})
}

func (r *actFeedForms) any(ok func(url.Values) bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, f := range r.forms {
		if ok(f) {
			return true
		}
	}
	return false
}

const actTypesRows = "DMs|Mentions|Threads|Channels set to \"all new posts\"|Reactions|Invitations|Apps|Reminders"

// vQqF and lCwf: the rows, rules and headings in each group of
// exclude_automations_filter, activity_bot_item_type_reads and
// activity_automations_only, on the All tab and in a custom view.
func TestActivityFilterMenuGroups(t *testing.T) {
	v, _, e, _ := vaFakeActivity(t, false)
	c := v.h.Core()
	// the account: exclude_automations_filter on
	m := v.menuEntries(true, v.curState())
	if got, want := actFilterLabels(v, m), actTypesRows+"|—|VIP|Cleared items|Specific channel or section"; got != want {
		t.Errorf("All tab:\n got %s\nwant %s", got, want)
	}
	if m.typeHead != -1 || m.extHead != 8 || m.height != 364 {
		t.Errorf("All tab: headings %d %d, height %d (want -1, 8, 364)", m.typeHead, m.extHead, m.height)
	}
	m = v.menuEntries(false, v.curState())
	if got, want := actFilterLabels(v, m), actTypesRows+"|—|Unreads|VIP|Cleared items|Specific channel or section"; got != want {
		t.Errorf("custom view:\n got %s\nwant %s", got, want)
	}
	if m.typeHead != 0 || m.extHead != 8 || m.height != 416 {
		t.Errorf("custom view: headings %d %d, height %d (want 0, 8, 416)", m.typeHead, m.extHead, m.height)
	}
	// without the select: the automation rows, Unreads first
	c.SetExperimentGroup("exclude_automations_filter", "off")
	m = v.menuEntries(true, v.curState())
	if got, want := actFilterLabels(v, m), actTypesRows+"|—|VIP|Cleared items|Exclude automations|Automations only|Specific channel or section"; got != want {
		t.Errorf("UK off, All tab:\n got %s\nwant %s", got, want)
	}
	m = v.menuEntries(false, v.curState())
	if got, want := actFilterLabels(v, m), "Unreads|—|"+actTypesRows+"|—|VIP|Cleared items|Exclude automations|Automations only|Specific channel or section"; got != want {
		t.Errorf("UK off, custom view:\n got %s\nwant %s", got, want)
	}
	if m.typeHead != 2 || m.extHead != 10 {
		t.Errorf("UK off, custom view: headings %d %d, want 2 (DMs) and 10", m.typeHead, m.extHead)
	}
	c.SetExperimentGroup("activity_bot_item_type_reads", "off")
	c.SetExperimentGroup("activity_automations_only", "")
	m = v.menuEntries(true, v.curState())
	if got := actFilterLabels(v, m); strings.Contains(got, "automations") || strings.Contains(got, "Automations") {
		t.Errorf("zK and LS off: %s", got)
	}
	c.SetExperimentGroup("activity_automations_only", "on")
	if got := actFilterLabels(v, v.menuEntries(true, v.curState())); !strings.HasSuffix(got, "|Automations only|Specific channel or section") || strings.Contains(got, "Exclude") {
		t.Errorf("LS alone: %s", got)
	}
	for _, x := range []string{"exclude_automations_filter", "activity_bot_item_type_reads", "activity_automations_only"} {
		c.ClearExperimentGroup(x)
	}
	// reminders off in Activity, DMs opted out
	v.menu = actMenuFilter
	e.frame()
	vaShot(t, e, "activity-filter-source", false)
}

// "Notifications from": People only asks for exclude_automations and
// disables Apps, Apps and agents only asks for automations_only, lists
// the automations' entries and disables DMs and Reminders.
func TestActivityFilterSourceLikeSlack(t *testing.T) {
	v, _, e, fs := vaFakeActivity(t, false)
	var rec actFeedForms
	rec.hook(fs)
	v.menu = actMenuFilter
	e.frame()
	e.frame()
	if len(v.fl.src.items) != 3 || v.fl.src.sel != 0 {
		t.Fatalf("the select: %d items, sel %d", len(v.fl.src.items), v.fl.src.sel)
	}
	v.filterC[6].Click() // Apps
	e.frame()
	if !v.filters[6] {
		t.Fatal("Apps didn't turn on")
	}
	v.fl.src.items[1].Click() // People only
	e.frame()
	e.until(t, "exclude_automations", func() bool {
		return rec.any(func(f url.Values) bool {
			return f.Get("exclude_automations") == "true" && f.Get("automations_only") == "false"
		})
	})
	if !v.exclAuto || v.autoOnly || v.filters[6] {
		t.Errorf("People only: excl %v only %v, Apps %v (wanted dropped)", v.exclAuto, v.autoOnly, v.filters[6])
	}
	apps := actEntry{opt: optType, i: 6}
	if !actSourceDisables(v.curState().source(), apps) || v.filterChecked(v.curState(), apps) {
		t.Error("Apps isn't disabled under People only")
	}
	v.filterC[6].Click()
	e.frame()
	if v.filters[6] {
		t.Error("a disabled row took the click")
	}
	vaShot(t, e, "activity-filter-people-only", false)
	v.fl.src.items[2].Click() // Apps and agents only
	e.frame()
	e.until(t, "the automations' entries", func() bool {
		p := actPage(v)
		return len(p.Items) == 1 && p.Items[0].Key == "fa-slackbot" && p.Items[0].IsBot
	})
	if !rec.any(func(f url.Values) bool {
		return f.Get("automations_only") == "true" && f.Get("exclude_automations") == "false"
	}) {
		t.Error("no automations_only feed")
	}
	st := v.curState()
	for _, i := range []int{0, 7} {
		if !actSourceDisables(st.source(), actEntry{opt: optType, i: i}) {
			t.Errorf("row %d isn't disabled under Apps and agents only", i)
		}
	}
	if v.filterCount() != 1 {
		t.Errorf("filter count %d, want 1 (Automations only)", v.filterCount())
	}
	v.fl.src.items[0].Click()
	e.frame()
	if v.exclAuto || v.autoOnly {
		t.Error("everyone kept a flag")
	}
}

// The mention sub-filters: the caret opens them under Mentions, one asks
// for its types, all four check Mentions and count four; Apps and agents
// only closes them and widens a partial pick.
func TestActivityMentionSubFiltersLikeSlack(t *testing.T) {
	v, _, e, fs := vaFakeActivity(t, false)
	var rec actFeedForms
	rec.hook(fs)
	v.menu = actMenuFilter
	e.frame()
	v.fl.caretC.Click()
	e.frame()
	m := v.menuEntries(true, v.curState())
	got := actFilterLabels(v, m)
	if !strings.HasPrefix(got, "DMs|Mentions|@Test Self|@channel|@user-group|keyword|Threads") {
		t.Fatalf("sub-filters under Mentions: %s", got)
	}
	if m.height != 476 { // (16 + 1) × 28
		t.Errorf("height %d", m.height)
	}
	vaShot(t, e, "activity-filter-mentions", false)
	v.fl.subC[0].Click() // @you
	e.frame()
	e.until(t, "the @you feed", func() bool {
		return rec.any(func(f url.Values) bool { return f.Get("types") == "at_user,unjoined_channel_mention" })
	})
	if st := v.curState(); !st.mentionsSome() || st.mentionsOn() {
		t.Error("one sub-filter: Mentions isn't partly checked")
	}
	v.filterC[actRowMentions].Click() // Mentions: all four
	e.frame()
	if st := v.curState(); !st.mentionsOn() || st.subs != [actSubN]bool{true, true, true, true} {
		t.Errorf("Mentions: subs %v", st.subs)
	}
	if v.filterCount() != 4 {
		t.Errorf("filter count %d, want 4 (each sub-filter)", v.filterCount())
	}
	e.until(t, "the four sub-filters' feed", func() bool {
		return rec.any(func(f url.Values) bool {
			return f.Get("types") == "at_user,unjoined_channel_mention,at_channel,at_everyone,at_user_group,keyword"
		})
	})
	// a saved view keeps RmAw's entry types and comes back as sub-filters
	vf := v.curState().viewFilters()
	if strings.Join(vf.EntryTypes, ",") != "at_user,at_channel,at_everyone,at_user_group,keyword" {
		t.Errorf("entry types %v", vf.EntryTypes)
	}
	if back := actStateOf(vf); back.subs != [actSubN]bool{true, true, true, true} || back.filters[actRowMentions] {
		t.Errorf("back from the view: %+v", back)
	}
	// a suggested Mentions view: the whole type (RmAw Mentions); a
	// sub-filter under it takes the other three
	var whole actState
	whole.filters[actRowMentions] = true
	if got := strings.Join(whole.viewFilters().EntryTypes, ","); got != strings.Join(actEntryTypes[actRowMentions], ",") {
		t.Errorf("whole Mentions saves %s", got)
	}
	if whole.mentionFeedTypes() != actMentionsFeed {
		t.Errorf("whole Mentions asks for %s", whole.mentionFeedTypes())
	}
	whole.toggleSub(1)
	if whole.filters[actRowMentions] || whole.subs != [actSubN]bool{true, false, true, true} {
		t.Errorf("a sub-filter under Mentions: %+v", whole)
	}
	// Apps and agents only closes the caret's rows and widens a partial pick
	v.filterC[actRowMentions].Click() // off
	e.frame()
	v.fl.subC[2].Click()
	e.frame()
	v.fl.src.items[2].Click()
	e.frame()
	if st := v.curState(); st.subs != [actSubN]bool{true, true, true, true} {
		t.Errorf("Apps and agents only: subs %v, want all four", st.subs)
	}
	if got := actFilterLabels(v, v.menuEntries(true, v.curState())); strings.Contains(got, "@channel") {
		t.Errorf("sub-filters open under Apps and agents only: %s", got)
	}
}

// IKpm: "Specific channel or section" turns the picker on and opens it;
// a pick filters the feed by channel_ids and names the pill; closing it
// empty turns it off.
func TestActivityInPickerLikeSlack(t *testing.T) {
	v, _, e, fs := vaFakeActivity(t, false)
	var rec actFeedForms
	rec.hook(fs)
	v.menu = actMenuFilter
	e.frame()
	v.extraC[2].Click()
	e.frame()
	e.frame()
	if !v.showIn || !v.in.pick.open || v.menu != actMenuNone {
		t.Fatalf("In: showIn %v open %v menu %d", v.showIn, v.in.pick.open, v.menu)
	}
	if txt, _, _ := v.inPillText(); txt != "In" {
		t.Errorf("pill %q", txt)
	}
	vaShot(t, e, "activity-in-open", false)
	v.in.pick.srch.ed.SetText("views")
	e.frame()
	e.frame()
	idx := -1
	for i, o := range v.in.opts {
		if o.value == fakeslack.ViewsChannel {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("no #views-fixtures in %+v", v.in.opts)
	}
	v.in.pick.rows[idx].Click()
	e.frame()
	e.until(t, "the channel's feed", func() bool {
		return rec.any(func(f url.Values) bool { return f.Get("channel_ids") == fakeslack.ViewsChannel })
	})
	if txt, _, _ := v.inPillText(); txt != "In: #views-fixtures" {
		t.Errorf("pill %q", txt)
	}
	for _, it := range actPage(v).Items {
		if it.Channel != fakeslack.ViewsChannel {
			t.Errorf("%s in %s", it.Key, it.Channel)
		}
	}
	vaShot(t, e, "activity-in-picked", false)
	// close with a pick: the picker stays; unpick and close: it goes
	v.in.pick.open = false
	e.frame()
	if !v.showIn {
		t.Error("the picker went with a pick")
	}
	v.in.pick.toggle()
	e.frame()
	v.in.pick.srch.ed.SetText("views")
	e.frame()
	e.frame()
	for i, o := range v.in.opts {
		if o.value == fakeslack.ViewsChannel {
			v.in.pick.rows[i].Click()
		}
	}
	e.frame()
	if len(v.channels) != 0 {
		t.Fatalf("channels %v after unpicking", v.channels)
	}
	v.in.pick.catcher.Click()
	e.frame()
	e.frame()
	if v.showIn {
		t.Error("the picker stayed with nothing picked")
	}
	// a saved view naming channels starts with it on
	if st := actStateOf(core.ActivityViewFilters{ChannelIDs: []string{fakeslack.ViewsChannel}}); !st.showIn {
		t.Error("RY04 showFilterIn")
	}
}

// The tabs: views by position (the default ones too), the badges from
// activity_v2 in each group of activity_entry_layout_revisions_2, the
// icon-only strip without it, and a drag that writes next_view_id.
func TestActivityTabsLikeSlack(t *testing.T) {
	v, _, e, fs := vaFakeActivity(t, false)
	e.until(t, "the saved views", func() bool { return v.viewsLoaded })
	app := v.h.Core()
	raw, _ := json.Marshal(map[string]any{"type": "badge_counts_updated",
		"activity_v2": map[string]int{"at_user": 2, "dm": 1, "thread_v2": 3, "message_reaction": 5}})
	app.Inject(slack.Event{Type: "badge_counts_updated", Raw: raw})
	if got := v.tabCounts(); got != [4]int{6, 1, 2, 3} {
		t.Errorf("tab counts %v, want [6 1 2 3] (All without reactions)", got)
	}
	for _, c := range []struct {
		slot, n int
		xi      bool
		want    string
	}{{0, 6, true, "dot"}, {1, 1, true, ""}, {2, 2, true, ""}, {0, 6, false, "pill"}, {1, 1, false, "count"},
		{3, 3, false, "count"}, {0, 0, true, ""}, {actSlotN, 4, false, ""}} {
		if got := actTabBadgeKind(c.slot, c.n, c.xi); got != c.want {
			t.Errorf("badge slot %d n %d xi %v: %q, want %q", c.slot, c.n, c.xi, got, c.want)
		}
	}
	e.frame()
	vaShot(t, e, "activity-tabs-badges", false)
	// icon-only: 60 + 3 × 90 + 36 = 366, 20 more to come back
	g := layout.Context{Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	if v.stripIconOnly(300, g) {
		t.Error("icon-only with the layout revisions")
	}
	app.SetExperimentGroup("activity_entry_layout_revisions_2", "off")
	if !v.stripIconOnly(300, g) || !v.stripIconOnly(380, g) || v.stripIconOnly(400, g) || v.stripIconOnly(380, g) {
		t.Error("icon-only thresholds")
	}
	if actTabTip(1, 1) != "1 unread DM" || actTabTip(2, 3) != "3 unread mentions" || actTabTip(3, 0) != "Threads" {
		t.Error("icon tabs' tooltips")
	}
	e.size = image.Pt(400, 900)
	e.frame()
	e.frame()
	vaShot(t, e, "activity-tabs-icon-only", false)
	e.size = image.Pt(1200, 1400)
	app.ClearExperimentGroup("activity_entry_layout_revisions_2")
	e.frame()
	// calculateDropIndex
	for _, c := range []struct {
		base     int
		side     string
		last     bool
		from, to int
	}{{1, "left", false, 3, 1}, {2, "left", false, 1, 1}, {3, "left", true, 1, 2}, {3, "right", true, 1, 3}} {
		if got := actDropIndex(c.base, c.side, c.last, c.from); got != c.to {
			t.Errorf("drop on %d (%s, last %v) from %d: %d, want %d", c.base, c.side, c.last, c.from, got, c.to)
		}
	}
	// drag Threads onto DMs: it goes before DMs
	if actTabsNow(v) != "All,DMs,Mentions,Threads" {
		t.Fatalf("tabs %s", actTabsNow(v))
	}
	st := &v.strip
	y := float32(53 + st.h/2)
	at := func(i int, frac float32) f32.Point {
		return f32.Pt(float32(16+st.xs[i])+frac*float32(st.ws[i]), y)
	}
	from, to := at(3, .5), at(1, .2)
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: from},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: from},
	)
	e.frame()
	mid := f32.Pt((from.X+to.X)/2, y)
	e.r.Queue(pointer.Event{Kind: pointer.Drag, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: mid})
	e.frame()
	e.r.Queue(pointer.Event{Kind: pointer.Drag, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: to})
	e.frame()
	if !st.dragging {
		t.Fatal("no drag")
	}
	if tgt, side := v.dropTarget(); tgt != 1 || side != "left" {
		t.Errorf("drop target %d %s, want DMs", tgt, side)
	}
	vaShot(t, e, "activity-tab-drag", false)
	e.r.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: to})
	e.frame()
	e.frame()
	if got := actTabsNow(v); got != "All,Threads,DMs,Mentions" {
		t.Errorf("after the drop: %s", got)
	}
	if v.tabs.Sel != 0 {
		t.Errorf("the drag switched tabs: %d", v.tabs.Sel)
	}
	e.until(t, "activity.views.update next_view_id", func() bool {
		return actFakeWrite(fs, "activity.views.update", func(f url.Values) bool {
			return f.Get("view_id") == "Vw0FAKE3" && f.Get("next_view_id") == "Vw0FAKE1" && f.Get("density") == "compact" && f.Get("sort") == "newest"
		})
	})
	// the server keeps the order
	v.loadViews()
	e.until(t, "the views again", func() bool { return actTabsNow(v) == "All,Threads,DMs,Mentions" && v.views[3].Position == "1" })
}

// activity_inbox_remove_persistent_detail_panel: the inbox at the full
// width (the account), else in the sidebar column beside Primary, which
// shows ActivityEmptyView until a row is opened.
func TestActivityDetailPanelGroups(t *testing.T) {
	v, _, e, _ := vaFakeActivity(t, false)
	app := v.h.Core()
	e.frame()
	if v.pageW != 1200 {
		t.Errorf("Bb on: page %d wide", v.pageW)
	}
	for _, f := range []func(){
		func() { app.SetExperimentGroup("activity_inbox_remove_persistent_detail_panel", "off") },
		func() { app.SetExperimentGroup("nested_secondary_view", "") },
		func() { app.SetExperimentGroup("agentic_ia", "on") },
	} {
		f()
		e.frame()
		if v.pageW != 456 { // 38% of 1200
			t.Errorf("Bb off: page %d wide, want 456", v.pageW)
		}
		app.ClearExperimentGroup("activity_inbox_remove_persistent_detail_panel")
		app.ClearExperimentGroup("nested_secondary_view")
		app.ClearExperimentGroup("agentic_ia")
	}
	app.SetExperimentGroup("activity_inbox_remove_persistent_detail_panel", "off")
	e.frame()
	vaShot(t, e, "activity-detail-panel-empty", false)
	page := actPage(v)
	v.choose(&page.Items[0], actKey(&page.Items[0]), v.rowFor(actKey(&page.Items[0])))
	e.frame()
	if !v.selOK {
		t.Fatal("no item open")
	}
	vaShot(t, e, "activity-detail-panel-item", false)
	app.ClearExperimentGroup("activity_inbox_remove_persistent_detail_panel")
	e.frame()
	if v.pageW != 1200 {
		t.Errorf("Bb back on: page %d wide", v.pageW)
	}
}

// agent_status_in_activity: a channel post in a session channel that's
// processing shows the spinner ("Working") ahead of its badge, a
// suspended thread root "Needs attention"; none with the experiment off.
func TestActivityAgentStatusRowsLikeSlack(t *testing.T) {
	v, _, e, _ := vaFakeActivity(t, false)
	app := v.h.Core()
	props, _ := json.Marshal(map[string]any{"agent_session": map[string]any{"status": "processing"}, "code_channel": map[string]any{}})
	app.S.PutConv(slack.Conversation{ID: fakeslack.ViewsChannel, Name: "views-fixtures", IsChannel: true, IsMember: true, NumMembers: 3, Properties: props})
	root := slack.Message{Type: "message", TS: fakeslack.ViewsThreadUnread, ThreadTS: fakeslack.ViewsThreadUnread, User: "U0FAKE00001",
		Text: "a synthetic question for the team", AgentSession: &slack.MessageAgentSession{Status: "suspended"}}
	app.S.SetLatest(fakeslack.ViewsChannel, []slack.Message{root}, false)
	v.dirty = true
	e.frame()
	rowOf := func(key string) *actRow { return v.rowFor(key) }
	if r := rowOf("fa-channel"); r.agent != core.AgentProcessing {
		t.Errorf("fa-channel agent %q", r.agent)
	}
	if r := rowOf("fa-thread"); r.agent != core.AgentSuspended {
		t.Errorf("fa-thread agent %q", r.agent)
	}
	if r := rowOf("fa-at-user"); r.agent != "" {
		t.Errorf("a mention got %q", r.agent)
	}
	vaShot(t, e, "activity-agent-status", false)
	app.SetExperimentGroup("agent_status_in_activity", "off")
	v.dirty = true
	e.frame()
	if r := rowOf("fa-channel"); r.agent != "" {
		t.Errorf("experiment off: %q", r.agent)
	}
}

// Live entries follow the automation filters (sD/F activityUpdatedV2):
// an automation's entry skips "Exclude automations" with
// activity_bot_item_type_reads, a person's skips "Automations only" with
// activity_automations_only.
func TestActivityLiveAutomationFilters(t *testing.T) {
	v, _, e, _ := vaFakeActivity(t, false)
	app := v.h.Core()
	entry := func(key string, bot bool) map[string]any {
		return map[string]any{"key": key, "feed_ts": "1762702100.000100", "is_unread": true, "is_bot": bot,
			"item": map[string]any{"type": "at_user", "message": map[string]any{"ts": "1762700100.000100", "channel": fakeslack.ViewsChannel, "author_user_id": "U0FAKE00001"}}}
	}
	live := func(it map[string]any) bool {
		ev, _ := json.Marshal(map[string]any{"type": "activity", "subtype": "activity_updated", "entry": it})
		app.Inject(slack.Event{Type: "activity", Subtype: "activity_updated", Raw: ev})
		key := it["key"].(string)
		for i := 0; i < 40; i++ {
			e.frame()
			for _, x := range actPage(v).Items {
				if x.Key == key {
					return true
				}
			}
		}
		return false
	}
	v.exclAuto = true
	if live(entry("fa-live-bot", true)) {
		t.Error("an automation's entry joined Exclude automations")
	}
	if !live(entry("fa-live-person", false)) {
		t.Error("a person's entry didn't join Exclude automations")
	}
	app.SetExperimentGroup("activity_bot_item_type_reads", "off")
	if !live(entry("fa-live-bot-2", true)) {
		t.Error("without activity_bot_item_type_reads the entry should join")
	}
	v.exclAuto, v.autoOnly = false, true
	if live(entry("fa-live-person-2", false)) {
		t.Error("a person's entry joined Automations only")
	}
}

// The new view's grid (lCwf ADD_TAB): the sub-filters open after
// Mentions, the select over it, a row the source disables.
func TestActivityAddTabRowsLikeSlack(t *testing.T) {
	v, _, e, _ := vaFakeActivity(t, false)
	m := &actViewModal{v: v, kind: actCreateTab}
	var labels []string
	for _, x := range m.addTabEntries() {
		labels = append(labels, v.entryLabel(x))
	}
	if got, want := strings.Join(labels, "|"), "DMs|Mentions|@Test Self|@channel|@user-group|keyword|Threads|"+
		`Channels set to "all new posts"|Reactions|Invitations|Apps|Reminders|VIP|Cleared items`; got != want {
		t.Errorf("grid:\n got %s\nwant %s", got, want)
	}
	m.st.setSource(srcAutomations)
	if !m.st.autoOnly || !actSourceDisables(m.st.source(), actEntry{opt: optType, i: 0}) {
		t.Error("Apps and agents only in the new view")
	}
	for _, x := range m.addTabEntries() {
		if x.opt == optSub {
			t.Error("sub-filters open under Apps and agents only")
			break
		}
	}
	if f := m.st.viewFilters(); !f.AutomationsOnly || f.ExcludeAutomations {
		t.Errorf("new view filters %+v", f)
	}
	v.h.Core().SetExperimentGroup("exclude_automations_filter", "off")
	m.st = actState{}
	labels = labels[:0]
	for _, x := range m.addTabEntries() {
		labels = append(labels, v.entryLabel(x))
	}
	if got := strings.Join(labels, "|"); !strings.HasSuffix(got, "|VIP|Cleared items|Exclude automations|Automations only") {
		t.Errorf("UK off grid: %s", got)
	}
	v.h.Core().ClearExperimentGroup("exclude_automations_filter")
	v.openViewModal(actCreateTab, core.ActivityView{})
	e.frame()
	vaShot(t, e, "activity-create-view-source", false)
}

// activity_entry_layout_revisions_2's day dividers: none over the first
// row with it, "Yesterday"-style over it without (the fixture's days are
// all in the past).
func TestActivityDayDividersGroups(t *testing.T) {
	v, _, e, _ := vaFakeActivity(t, false)
	app := v.h.Core()
	e.frame()
	if len(v.vis) == 0 || v.vis[0] < 0 {
		t.Errorf("layout revisions: the list starts with %d", v.vis[0])
	}
	app.SetExperimentGroup("activity_entry_layout_revisions_2", "off")
	v.dirty = true
	e.frame()
	if len(v.vis) == 0 || v.vis[0] >= 0 {
		t.Errorf("without revisions: the list starts with %v, want a divider", v.vis[:min(3, len(v.vis))])
	}
	vaShot(t, e, "activity-no-revisions", false)
}
