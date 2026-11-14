package views

// Activity's leftovers (docs/ACTIVITY.md "Leftovers 2", branch
// activity-leftovers): the list's lead-in and the VIP header's stuck
// mark, the importance icon's header, agent entries' Block Kit actions,
// the In picker's blank-query suggestions, the tab strip's More, the
// "Discard changes?" speed bump, the sort button's "new" badge and the
// tooltips, against internal/fakeslack (Size.Views). Synthetic content
// only; every write is fakeslack's recorded one.

import (
	"context"
	"encoding/json"
	"image"
	"net/url"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// loListTop is the list's top in the window (the header, the filter bar
// 16px under it and 16px over the list).
func loListTop() int { return int(actHeadH + actBarTop + actBarH + 16) }

func loHover(e *vaEnv, p image.Point) {
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y))})
	e.frame()
}

// loFind moves the pointer along y = y from x0 to x1 until hovered says
// it's over the widget; it returns that point.
func loFind(t *testing.T, e *vaEnv, x0, x1, y int, hovered func() bool) image.Point {
	t.Helper()
	for x := x0; x <= x1; x += 3 {
		p := image.Pt(x, y)
		loHover(e, p)
		if hovered() {
			return p
		}
	}
	t.Fatalf("nothing hovered along y=%d from %d to %d", y, x0, x1)
	return image.Point{}
}

func loItem(v *Activity, key string) *core.ActivityItem {
	page := actPage(v)
	for i := range page.Items {
		if actKey(&page.Items[i]) == key {
			return &page.Items[i]
		}
	}
	return nil
}

// loVIPSort turns on VIP unreads on top (the settings menu's Sort rows).
func loVIPSort(t *testing.T, v *Activity, e *vaEnv, fs *fakeslack.Server) {
	t.Helper()
	if err := v.h.Core().AddVIP(context.Background(), "U0FAKE00001"); err != nil {
		t.Fatal(err)
	}
	v.h.Core().SetExperimentGroup("activity_in_home_prototype", "on")
	v.menu = actMenuSettings
	e.frame()
	v.setSortC[1].Click() // VIP unreads on top
	e.frame()
	e.until(t, "activity.prefs.update sort", func() bool {
		return actFakeWrite(fs, "activity.prefs.update", func(f url.Values) bool { return f.Get("sort") == "vip_unreads_first" })
	})
	e.until(t, "the VIP order", func() bool { return len(v.vis) > 0 && v.vis[0] == actVisVIP })
	v.menu = actMenuNone
	e.frame()
}

// ---- the lead-in ----

// The list starts 9px down (the 8px --activity-first-item-lead-in and
// the 1px top-anchor-hack item): a press there is on no row; 4px +1
// without the revisions, the anchor alone in unified triage and in the
// peek. The first row's hover bar sits 4px into its content box
// (--firstEntry), the others' above them (translateY(-100%)), the peek's
// 4px over it at its right edge (--peek).
func TestActivityLeadInLikeSlack(t *testing.T) { w15Modes(t, loLeadIn) }

func loLeadIn(t *testing.T, dark bool) {
	v, _, e, _ := vaFakeActivity(t, dark)
	app := v.h.Core()
	gtx := w15Gtx(time.Now())
	if got := v.leadIn(gtx); got != 9 || v.stk.lead != 9 {
		t.Fatalf("lead-in %d (list %d), want 9", got, v.stk.lead)
	}
	if v.list.Position.First != 0 || v.list.Position.Offset != 0 || len(v.vis) == 0 || v.vis[0] < 0 {
		t.Fatalf("not at the top: %+v, first item %v", v.list.Position, v.vis[:min(1, len(v.vis))])
	}
	vaShot(t, e, "activity-lead-in", dark)
	top := loListTop()
	e.click(image.Pt(400, top+4))
	if v.selOK {
		t.Fatal("a press in the lead-in opened the first row")
	}
	// the first row's hover bar: --firstEntry, inside it
	first := v.rowFor(actKey(&actPage(v).Items[v.vis[0]]))
	loHover(e, image.Pt(400, top+9+40))
	e.frame()
	if first.barY != 12+4 || first.barRight != 16 {
		t.Errorf("first entry's bar at %d (right %d), want 16 (16)", first.barY, first.barRight)
	}
	vaShot(t, e, "activity-lead-in-hover", dark)
	// the second row's: above it
	second := v.rowFor(actKey(&actPage(v).Items[v.vis[1]]))
	loHover(e, image.Pt(400, top+9+int(v.stk.h[0]-9)+40))
	e.frame()
	if second.barY != -25 {
		t.Errorf("second row's bar at %d, want -25", second.barY)
	}
	e.click(image.Pt(400, top+9+20))
	if !v.selOK {
		t.Error("a press on the first row didn't open it")
	}
	app.SetExperimentGroup("activity_entry_layout_revisions_2", "off")
	if got := v.leadIn(gtx); got != 5 {
		t.Errorf("without the revisions: %d, want 5", got)
	}
	app.SetExperimentGroup("activity_entry_layout_revisions_2", "on")
	app.SetExperimentGroup("unified_global_triage", "on")
	if got := v.leadIn(gtx); got != 1 {
		t.Errorf("unified triage: %d, want 1", got)
	}
	app.ClearExperimentGroup("unified_global_triage")
	v.pk.peeking = true
	if got := v.leadIn(gtx); got != 1 {
		t.Errorf("the peek: %d, want 1", got)
	}
	v.pk.peeking = false
}

// The VIP sort's "VIP unreads" sits 9px down at scroll 0, in its place,
// not --stuck (oDbF's IntersectionObserver: less than all of it inside
// the root's 1px-in top); 8px down it's still clear; at 9px it reaches
// the top and sticks, its mark (the inset otl-ter line over its border)
// fading in over 80ms, and fading out once it's back down.
func TestActivityVIPHeaderNotStuckAtTopLikeSlack(t *testing.T) { w15Modes(t, loVIPTop) }

func loVIPTop(t *testing.T, dark bool) {
	v, _, e, fs := vaFakeActivity(t, dark)
	loVIPSort(t, v, e, fs)
	e.size = image.Pt(1000, 520)
	v.list.Position.First, v.list.Position.Offset = 0, 0
	e.frame()
	e.frame()
	if v.stk.on || v.stk.stuck[0] {
		t.Fatalf("stuck at scroll 0: on %v stuck %v", v.stk.on, v.stk.stuck)
	}
	vaShot(t, e, "activity-vip-top", dark)
	v.list.Position.First, v.list.Position.Offset = 0, 8
	e.frame()
	e.frame()
	if v.stk.on || v.stk.stuck[0] {
		t.Fatalf("stuck 1px from the top: on %v stuck %v", v.stk.on, v.stk.stuck)
	}
	v.list.Position.First, v.list.Position.Offset = 0, 9
	e.frame()
	if !v.stk.on || !v.stk.vip || v.stk.y != 0 || !v.stk.stuck[0] {
		t.Fatalf("at the top: on %v vip %v y %d stuck %v", v.stk.on, v.stk.vip, v.stk.y, v.stk.stuck)
	}
	if a, run := v.stk.stuckAlpha(0, time.Now()); a >= 1 || !run {
		t.Errorf("the mark's fade: %v running %v", a, run)
	}
	time.Sleep(actStuckFade + 20*time.Millisecond)
	e.frame()
	if a, run := v.stk.stuckAlpha(0, time.Now()); a != 1 || run {
		t.Errorf("after 80ms: %v running %v", a, run)
	}
	vaShot(t, e, "activity-vip-stuck", dark)
	v.list.Position.First, v.list.Position.Offset = 0, 0
	e.frame()
	if v.stk.stuck[0] {
		t.Error("still stuck back at the top")
	}
	if a, _ := v.stk.stuckAlpha(0, time.Now().Add(actStuckFade)); a != 0 {
		t.Errorf("the mark after fading out: %v", a)
	}
}

// ---- the importance icon ----

// ActivitySectionHeader's importance icon: VIP unreads' tile
// (mark-as-important-filled) with layout revisions, 20px in and 12px to
// its title; in Dense the title gap is 4px and Everything else sits 16px
// in; without the revisions there's no tile (F needs A), the label 24px
// in.
func TestActivityImportanceIconLikeSlack(t *testing.T) { w15Modes(t, loImportance) }

func loImportance(t *testing.T, dark bool) {
	v, _, e, fs := vaFakeActivity(t, dark)
	gtx := w15Gtx(time.Now())
	type m struct {
		x, gap int
		tile   bool
	}
	get := func(vip bool) m {
		x, gap, tile := v.sectionHeaderMetrics(gtx, vip)
		return m{x, gap, tile}
	}
	if got := get(true); got != (m{20, 12, true}) {
		t.Errorf("VIP unreads: %+v", got)
	}
	if got := get(false); got != (m{20, 12, true}) {
		t.Errorf("Everything else: %+v", got)
	}
	v.dense = true
	if got := get(true); got != (m{20, 4, true}) {
		t.Errorf("Dense VIP unreads: %+v", got)
	}
	if got := get(false); got != (m{16, 4, true}) {
		t.Errorf("Dense Everything else: %+v", got)
	}
	v.dense = false
	loVIPSort(t, v, e, fs)
	vaShot(t, e, "activity-importance", dark)
	v.dense, v.rows, v.dirty = true, map[string]*actRow{}, true
	e.frame()
	e.frame()
	vaShot(t, e, "activity-importance-dense", dark)
	v.dense, v.rows, v.dirty = false, map[string]*actRow{}, true
	v.h.Core().SetExperimentGroup("activity_entry_layout_revisions_2", "off")
	if got := get(true); got != (m{24, 0, false}) {
		t.Errorf("without the revisions: %+v", got)
	}
}

// ---- agent entries' Block Kit actions ----

// rZx3 Rq: with agent_command_center, the entry's own message (its ts the
// feed_ts) with exactly one actions block, every element a button whose
// value is at most 50 characters (JavaScript's length), shows that block.
func TestActivityAgentActionsRulesLikeSlack(t *testing.T) {
	v, _, _, _ := vaFakeActivity(t, false)
	app := v.h.Core()
	btn := func(aid, val string) string {
		b, _ := json.Marshal(map[string]any{"type": "button", "action_id": aid, "value": val, "text": map[string]any{"type": "plain_text", "text": aid}})
		return string(b)
	}
	actions := func(bid string, els ...string) string {
		return `{"type":"actions","block_id":"` + bid + `","elements":[` + strings.Join(els, ",") + `]}`
	}
	doc := richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Actions, Buttons: []richtext.Button{{BlockID: "b1", ActionID: "go", Text: "Go", Kind: "button"}}}}}
	msg := func(blocks ...string) *core.Message {
		return &core.Message{Channel: "D0FAKEAGNT1", TS: "1762702050.000100", Blocks: json.RawMessage("[" + strings.Join(blocks, ",") + "]"), Doc: doc}
	}
	it := &core.ActivityItem{Key: "k", Type: "dm", FeedTS: "1762702050.000100"}
	section := `{"type":"section","block_id":"s1","text":{"type":"mrkdwn","text":"x"}}`
	cases := []struct {
		name string
		it   *core.ActivityItem
		m    *core.Message
		want bool
	}{
		{"one actions block of buttons", it, msg(section, actions("b1", btn("go", "x"))), true},
		{"a value of 50", it, msg(actions("b1", btn("go", strings.Repeat("v", 50)))), true},
		{"a value of 51", it, msg(actions("b1", btn("go", strings.Repeat("v", 51)))), false},
		{"25 emoji are 50 code units", it, msg(actions("b1", btn("go", strings.Repeat("😀", 25)))), true},
		{"26 emoji are 52", it, msg(actions("b1", btn("go", strings.Repeat("😀", 26)))), false},
		{"no value", it, msg(actions("b1", `{"type":"button","action_id":"go","text":{"type":"plain_text","text":"Go"}}`)), true},
		{"two actions blocks", it, msg(actions("b1", btn("go", "x")), actions("b2", btn("no", "y"))), false},
		{"a select", it, msg(actions("b1", btn("go", "x"), `{"type":"static_select","action_id":"pick"}`)), false},
		{"another message than the entry's", &core.ActivityItem{Key: "k", Type: "dm", FeedTS: "1762700000.000100"}, msg(actions("b1", btn("go", "x"))), false},
		{"no feed_ts", &core.ActivityItem{Key: "k", Type: "dm"}, msg(actions("b1", btn("go", "x"))), true},
		{"no blocks", it, &core.Message{Channel: "D0FAKEAGNT1", TS: "1762702050.000100", Doc: doc}, false},
	}
	app.SetExperimentGroup("agent_command_center", "on")
	for _, c := range cases {
		if _, _, got := v.agentActionsBlock(c.it, c.m); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	app.ClearExperimentGroup("agent_command_center") // the account's: unassigned is off
	if _, _, ok := v.agentActionsBlock(it, msg(actions("b1", btn("go", "x")))); ok {
		t.Error("shown without agent_command_center")
	}
}

// An agent's DM entry with an actions block: with agent_command_center
// its Detailed row draws the buttons in entry_extras (live: the
// activity_actions context), a click on Approve sends blocks.actions for
// the message, marks the row read and doesn't open it; without the
// experiment nothing shows.
func TestActivityAgentActionsLikeSlack(t *testing.T) { w15Modes(t, loAgent) }

func loAgent(t *testing.T, dark bool) {
	v, _, e, fs := vaFakeActivity(t, dark)
	app := v.h.Core()
	app.S.PutUsers([]slack.User{{ID: fakeslack.ActivityAgentUser, Name: "synthetic-agent", RealName: "Synthetic Agent", IsBot: true}})
	app.S.PutConv(slack.Conversation{ID: fakeslack.ActivityAgentDM, IsIM: true, User: fakeslack.ActivityAgentUser, IsMember: true})
	fs.SetActivityAgentActions(true)
	v.load(actFresh)
	e.until(t, "the agent entry", func() bool {
		it := loItem(v, "fa-agent")
		return it != nil && it.Msg != nil && len(it.Msg.Blocks) > 0
	})
	e.frame()
	it := loItem(v, "fa-agent")
	if _, _, ok := v.agentActionsBlock(it, it.Msg); ok {
		t.Fatal("actions without agent_command_center")
	}
	app.SetExperimentGroup("agent_command_center", "on")
	bid, blk, ok := v.agentActionsBlock(it, it.Msg)
	if !ok || bid != fakeslack.ActivityAgentBlockID || len(blk.Buttons) != 2 {
		t.Fatalf("Rq: %v %q %d buttons", ok, bid, len(blk.Buttons))
	}
	v.rows, v.dirty = map[string]*actRow{}, true
	w15ScrollTo(t, v, e, "fa-agent")
	r := v.rowFor("fa-agent")
	e.until(t, "the buttons", func() bool { return r.actEls != nil && len(r.actEls.me.RunRects(bid)) == 2 })
	vaShot(t, e, "activity-agent-actions", dark)
	// Approve's place: the extras end the stack, 12px over the row's
	// bottom; the stack starts after the 28px avatar
	idx := -1
	for i, x := range v.vis {
		if x >= 0 && actKey(&actPage(v).Items[x]) == "fa-agent" {
			idx = i
		}
	}
	top, rowH := loListTop()+v.stk.tops[idx], v.stk.h[idx]
	if idx == 0 {
		top, rowH = top+v.stk.lead, rowH-v.stk.lead
	}
	ent := v.rich["activity_actions/"+fakeslack.ActivityAgentDM+"/"+fakeslack.ActivityAgentTS]
	if ent == nil || len(ent.v.ElementSlots()) != 1 {
		t.Fatal("no actions view")
	}
	slot, btn := ent.v.ElementSlots()[0], r.actEls.me.RunRects(bid)[0]
	org := image.Pt(16+int(actAvatar)+8, top+rowH-12-r.actSize.Y).Add(slot.Min)
	p := org.Add(btn.Min).Add(btn.Size().Div(2))
	if v.isUnread(it, "fa-agent") == false {
		t.Fatal("the agent entry starts read")
	}
	e.click(p)
	e.until(t, "blocks.actions for Approve", func() bool {
		for _, c := range fs.BlockActionCalls() {
			if len(c.Actions) > 0 && c.Actions[0]["action_id"] == "agent-approve" {
				return c.Container["message_ts"] == fakeslack.ActivityAgentTS && c.Container["channel_id"] == fakeslack.ActivityAgentDM
			}
		}
		return false
	})
	e.until(t, "the row read", func() bool {
		return actFakeWrite(fs, "activity.markRead", func(f url.Values) bool { return f.Get("key") == "fa-agent" })
	})
	if v.selOK {
		t.Error("a click on Approve opened the row")
	}
	vaShot(t, e, "activity-agent-actions-read", dark)
	// Dense rows have no extras
	v.dense, v.rows, v.dirty = true, map[string]*actRow{}, true
	e.frame()
	e.frame()
	if rr := v.rowFor("fa-agent"); rr.actEls != nil {
		t.Error("a Dense row drew the actions")
	}
}

// ---- the In picker's suggestions ----

// IKpm with no query: after Recent, "Suggestions": the window's last
// search's "in" suggestions the store knows, not picked; three loading
// rows while a messages search runs and there are none yet. A pick
// filters by it; the new view's In lists the same.
func TestActivityInPickerSuggestionsLikeSlack(t *testing.T) { w15Modes(t, loInSuggest) }

func loInSuggest(t *testing.T, dark bool) {
	v, h, e, fs := vaFakeActivity(t, dark)
	app := v.h.Core()
	ctx := context.Background()
	var rec actFeedForms
	rec.hook(fs)
	open := func() {
		v.menu = actMenuFilter
		e.frame()
		v.extraC[2].Click() // Specific channel or section
		e.frame()
		e.frame()
		if !v.in.pick.open {
			t.Fatal("the In picker didn't open")
		}
	}
	// a slow first search: the loading rows
	fs.SetFault("search.modules.messages", fakeslack.Fault{Delay: 3 * time.Second, Times: 1})
	type answer struct {
		r   core.SearchResult
		err error
	}
	done := make(chan answer, 1)
	go func() {
		r, err := app.SearchMessages(ctx, core.SearchOpts{Query: fakeslack.SearchWord, FetchSuggestions: true})
		done <- answer{r, err}
	}()
	e.until(t, "the search in flight", app.SearchMessagesLoading)
	open()
	var loaders int
	for _, o := range v.in.opts {
		if o.loading {
			loaders++
		}
	}
	if loaders != 3 {
		t.Fatalf("%d loading rows while the search runs: %+v", loaders, v.in.opts)
	}
	vaShot(t, e, "activity-in-loading", dark)
	a := <-done
	if a.err != nil {
		t.Fatal(a.err)
	}
	in, _ := app.SearchSuggestions()
	if len(in) == 0 {
		t.Fatal("no in suggestions from the search")
	}
	// the store learns the channels it didn't know (getChannelOrMemberById)
	for _, s := range in {
		if _, known := app.Conv(s.ID); known {
			continue
		}
		if ref, ok := a.r.Convs[s.ID]; ok && (ref.Kind == "channel" || ref.Kind == "private") {
			app.S.PutConv(slack.Conversation{ID: s.ID, Name: ref.Name, IsChannel: true, IsPrivate: ref.Kind == "private", IsMember: true})
		}
	}
	e.frame()
	e.frame()
	var got []string
	head := -1
	for i, o := range v.in.opts {
		switch {
		case o.header && o.label == "Suggestions":
			head = i
		case head >= 0 && o.group:
			got = append(got, o.value)
		}
	}
	var want []string
	for _, s := range in {
		if _, ok := app.Conv(s.ID); ok {
			want = append(want, s.ID)
		}
	}
	if head < 0 || len(got) == 0 || strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Suggestions %v (heading at %d), want the search's %v", got, head, want)
	}
	vaShot(t, e, "activity-in-suggestions", dark)
	// pick the first: the feed filters by it, and it leaves the suggestions
	pick := got[0]
	for i, o := range v.in.opts {
		if o.value == pick && i > head {
			v.in.pick.rows[i].Click()
		}
	}
	e.frame()
	e.until(t, "the channel's feed", func() bool {
		return rec.any(func(f url.Values) bool { return strings.Contains(f.Get("channel_ids"), pick) })
	})
	e.frame()
	inSugg := false
	for _, o := range v.in.opts {
		switch {
		case o.header:
			inSugg = o.label == "Suggestions"
		case inSugg && o.value == pick:
			t.Errorf("%s still suggested after the pick", pick)
		}
	}
	if len(v.in.opts) == 0 || v.in.opts[0].value != pick {
		t.Errorf("the pick doesn't lead the list: %+v", v.in.opts[:min(2, len(v.in.opts))])
	}
	v.in.pick.open = false
	e.frame()
	// the new view's In: the same list
	v.openViewModal(actCreateTab, core.ActivityView{})
	e.frame()
	m := v.lastViewModal
	m.in.init()
	m.in.pick.toggle()
	e.frame()
	e.frame()
	found := false
	for _, o := range m.in.opts {
		found = found || o.header && o.label == "Suggestions"
	}
	if !found {
		t.Errorf("the new view's In has no Suggestions: %+v", m.in.opts)
	}
	vaShot(t, e, "activity-create-view-in-suggestions", dark)
	m.in.pick.open = false
	m.bump.Discard()
	e.frame()
	if open := h.open(); len(open) > 0 {
		if _, ok := open[len(open)-1].(*popups.Modal); ok {
			t.Error("the create modal stayed open")
		}
	}
}

// ---- the tab strip's More ----

// measureVisibleTabs: all the tabs when every button fits (right ≤ width
// + 1); else those within width − 80, the first at least; the tabs after
// the last shown sit past More and the "+", so it takes more room to show
// them all again.
func TestActivityStripMeasureLikeSlack(t *testing.T) {
	bw, mr := []int{50, 60, 70}, []int{0, 4, 4}
	cases := []struct {
		name                     string
		prev, extra, width, want int
	}{
		{"all fit", 2, 100, 200, 2},
		{"overflow: 80px clear", 2, 100, 150, 0},
		{"the first at least", 2, 100, 40, 0},
		{"just fits", 2, 100, 187, 2},
		{"shifted past More: stays", 0, 100, 190, 0},
		{"wide enough with the shift", 0, 100, 290, 2},
	}
	for _, c := range cases {
		if got := actStripLast(bw, mr, c.prev, c.extra, c.width, actMoreReserve); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}

// Tabs that don't fit go into More ("More", the 13px caret): a click
// opens its 200px menu of radio rows, the hidden tabs with their labels;
// picking one shows it, and More takes the selected tab's underline.
func TestActivityTabsMoreLikeSlack(t *testing.T) { w15Modes(t, loMore) }

func loMore(t *testing.T, dark bool) {
	v, _, e, _ := vaFakeActivity(t, dark)
	e.until(t, "the saved views", func() bool { return v.viewsLoaded })
	names := []string{"Synthetic launches", "Synthetic design reviews", "Synthetic incidents", "Synthetic partner asks",
		"Synthetic hiring loop", "Synthetic weekly notes", "Synthetic budget threads"}
	for _, n := range names {
		v.createView(core.ActivityView{Name: n, Sort: "newest", Density: "compact"}, func(string) {})
		// tabs sort by position as strings ("10" before "2")
		e.until(t, "the view "+n, func() bool { return strings.Contains(","+actTabsNow(v)+",", ","+n+",") })
	}
	v.pickTab(0) // back to All
	e.frame()
	if v.tabs.Sel != 0 {
		t.Fatalf("shown tab %d, want All", v.tabs.Sel)
	}
	e.size = image.Pt(900, 900)
	e.frame()
	e.frame()
	if len(v.lo.hidden) == 0 || v.lo.last >= len(v.tabs.Labels)-1 {
		t.Fatalf("no overflow at 900px: last %d of %d", v.lo.last, len(v.tabs.Labels))
	}
	for _, i := range v.lo.hidden {
		if i <= v.lo.last || v.strip.ws[i] != 0 {
			t.Errorf("tab %d hidden but laid out (last %d, width %d)", i, v.lo.last, v.strip.ws[i])
		}
	}
	if v.lo.moreW == 0 || v.plusX < v.lo.moreX+v.lo.moreW {
		t.Errorf("More at %d (%d wide), the + at %d", v.lo.moreX, v.lo.moreW, v.plusX)
	}
	vaShot(t, e, "activity-tabs-more", dark)
	v.lo.moreBtn.Click()
	e.frame()
	m := v.lo.moreMenu
	if m == nil || m.Dismissed() {
		t.Fatal("More opened no menu")
	}
	want := make([]string, 0, len(v.lo.hidden))
	for _, i := range v.lo.hidden {
		want = append(want, v.tabs.label(i))
	}
	if got := m.Labels(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("menu %v, want %v", got, want)
	}
	for _, it := range v.moreItems(v.lo.last, v.tabs.Sel) {
		if !it.Radio || it.Checked {
			t.Errorf("row %s: radio %v checked %v", it.Label, it.Radio, it.Checked)
		}
	}
	vaShot(t, e, "activity-tabs-more-menu", dark)
	pick := v.lo.hidden[0]
	r := m.RowRect(v.tabs.label(pick))
	e.click(r.Min.Add(r.Size().Div(2)))
	e.frame()
	if v.tabs.Sel != pick {
		t.Fatalf("picked %d, shown %d", pick, v.tabs.Sel)
	}
	if items := v.moreItems(v.lo.last, v.tabs.Sel); len(items) == 0 || !items[0].Checked {
		t.Error("the shown tab isn't checked in the menu")
	}
	vaShot(t, e, "activity-tabs-more-selected", dark)
}

// ---- "Discard changes?" ----

// Create a new view with nothing changed closes at once; with a name
// typed, Cancel (and Escape, and the close button) show the footer's
// speed bump: "Discard changes?" / "If you close without saving, you’ll
// lose all the changes you’ve made." with Keep Editing and Discard. Keep
// Editing goes back, Discard closes without saving.
func TestActivityDiscardChangesLikeSlack(t *testing.T) { w15Modes(t, loDiscard) }

func loDiscard(t *testing.T, dark bool) {
	v, h, e, fs := vaFakeActivity(t, dark)
	e.until(t, "the saved views", func() bool { return v.viewsLoaded })
	press := func(b *popups.ModalButton) {
		t.Helper()
		r := b.Rect()
		if r.Empty() {
			t.Fatalf("%s isn't drawn", b.Label)
		}
		e.click(r.Min.Add(r.Size().Div(2)))
	}
	v.openViewModal(actCreateTab, core.ActivityView{})
	e.frame()
	e.frame()
	m, modal := v.lastViewModal, actLastModal(h)
	press(m.cancel)
	if !modal.Dismissed() || m.bump.Up() {
		t.Fatalf("Cancel with nothing changed: closed %v, speed bump %v", modal.Dismissed(), m.bump.Up())
	}
	v.openViewModal(actCreateTab, core.ActivityView{})
	e.frame()
	m, modal = v.lastViewModal, actLastModal(h)
	m.ed.SetText("Synthetic drafts")
	e.frame()
	press(m.cancel)
	e.frame()
	if modal.Dismissed() || !m.bump.Up() {
		t.Fatalf("Cancel with a name typed: closed %v, speed bump %v", modal.Dismissed(), m.bump.Up())
	}
	if m.bump.Heading != "Discard changes?" || m.bump.Description != "If you close without saving, you’ll lose all the changes you’ve made." ||
		m.bump.Keep.Label != "Keep Editing" || m.bump.Continue.Label != "Discard" || m.bump.Continue.Kind != popups.BtnDanger {
		t.Errorf("speed bump %q %q %q %q", m.bump.Heading, m.bump.Description, m.bump.Keep.Label, m.bump.Continue.Label)
	}
	vaShot(t, e, "activity-discard", dark)
	press(m.bump.Keep)
	if m.bump.Up() || modal.Dismissed() {
		t.Fatalf("Keep Editing: speed bump %v, closed %v", m.bump.Up(), modal.Dismissed())
	}
	// Escape (the close path) asks again; Escape on it cancels it
	modal.Dismiss()
	e.frame()
	if !m.bump.Up() {
		t.Fatal("Escape with changes didn't ask")
	}
	modal.Dismiss()
	e.frame()
	if m.bump.Up() || modal.Dismissed() {
		t.Fatal("Escape on the speed bump didn't go back to the modal")
	}
	modal.Dismiss()
	e.frame()
	press(m.bump.Continue)
	if !modal.Dismissed() {
		t.Fatal("Discard didn't close the modal")
	}
	if actFakeWrite(fs, "activity.views.create", func(f url.Values) bool { return f.Get("name") == "Synthetic drafts" }) {
		t.Error("Discard saved the view")
	}
	// a filter alone is a change too (the new tab's state isn't sTqq wn)
	v.openViewModal(actCreateTab, core.ActivityView{})
	e.frame()
	m, modal = v.lastViewModal, actLastModal(h)
	m.st.unread = true
	modal.Dismiss()
	e.frame()
	if !m.bump.Up() {
		t.Error("Only include unreads on: no speed bump")
	}
	m.bump.Discard()
	e.frame()
}

// ---- the sort button's new badge ----

// useActivitySortNewBadge: with activity_global_settings and
// activity_sort_new_badge in the megaphone's generic_new_badge space the
// header's sort button glows with EducationTag's micro "New"; its menu's
// close records the dismissal (megaphone.setNotificationAsSeen
// new_badge_generic / dismiss / step activity_sort_new_badge) and the
// badge goes. "Sort by" is its tooltip below.
func TestActivitySortNewBadgeLikeSlack(t *testing.T) { w15Modes(t, loSortBadge) }

func loSortBadge(t *testing.T, dark bool) {
	v, _, e, fs := vaFakeActivity(t, dark)
	app := v.h.Core()
	if err := app.AddVIP(context.Background(), "U0FAKE00001"); err != nil {
		t.Fatal(err)
	}
	fs.SetNewBadges(ActivitySortNewBadge)
	if err := app.MegaphoneBoot(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	e.until(t, "the badge", v.sortNewBadge)
	e.frame()
	if !v.headerSort() {
		t.Fatal("no header sort button")
	}
	p := loFind(t, e, e.size.X-200, e.size.X-30, 26, v.sortBtn.Hovered)
	time.Sleep(popups.TipDelay + 50*time.Millisecond)
	loHover(e, p)
	if !v.lo.sortTip.Showing(time.Now()) {
		t.Error("no Sort by tooltip")
	}
	vaShot(t, e, "activity-sort-new", dark)
	v.sortBtn.Click()
	e.frame()
	if v.lo.sortMenu == nil {
		t.Fatal("the sort menu didn't open")
	}
	vaShot(t, e, "activity-sort-new-open", dark)
	v.lo.sortMenu.Dismiss()
	e.frame()
	e.until(t, "the dismissal", func() bool {
		for _, c := range fs.UnreadsCalls() {
			if c.Method == "megaphone.setNotificationAsSeen" && c.Form.Get("notification") == "new_badge_generic" &&
				c.Form.Get("action") == "dismiss" && c.Form.Get("step") == ActivitySortNewBadge && c.Form.Get("reason") == "record-notification-event" {
				return true
			}
		}
		return false
	})
	if v.sortNewBadge() {
		t.Error("the badge stayed after the menu closed")
	}
	e.frame()
	vaShot(t, e, "activity-sort-after", dark)
	// without activity_global_settings it never shows
	app.SetExperimentGroup("activity_global_settings", "off")
	fs.SetNewBadges(ActivitySortNewBadge)
	if v.sortNewBadge() {
		t.Error("the badge without activity_global_settings")
	}
}

// ---- the tooltips ----

// ActivityFilterBulkAction's tooltips (on top): with the activity inbox
// the read and clear buttons show their keys (Alt+U, Alt+C; Option on a
// Mac), their labels when they're icons alone; Clear everything and Mark
// selected as unread show their labels. The "+" says "Add new view", or
// the 15-view limit.
func TestActivityBulkTooltipsLikeSlack(t *testing.T) {
	v, _, e, _ := vaFakeActivity(t, false)
	alt := actAltKey()
	if got := v.bulkTips(false); got != [3]string{alt + "+U", alt + "+C", "Mark selected as unread"} {
		t.Errorf("wide: %q", got)
	}
	if got := v.bulkTips(true); got != [3]string{"Mark selected as read", "Clear selected", "Mark selected as unread"} {
		t.Errorf("icons alone: %q", got)
	}
	v.serverAll = true
	if got := v.bulkTips(false); got[1] != "Clear everything" {
		t.Errorf("select-all: %q", got)
	}
	v.serverAll = false
	v.h.Core().SetExperimentGroup("activity_inbox", "off")
	if got := v.bulkTips(false); got[0] != "Mark selected as read" || got[1] != "Clear selected" {
		t.Errorf("without the inbox: %q", got)
	}
	v.h.Core().ClearExperimentGroup("activity_inbox")
	// a real hover: the read button's tip
	v.pick(func(_ *core.ActivityItem, unread bool) bool { return unread }, true)
	e.frame()
	if !v.triage() {
		t.Fatal("no triage mode")
	}
	y := int(actHeadH+actBarTop) + 16
	p := loFind(t, e, 60, 400, y, v.bulkRead.Hovered)
	time.Sleep(popups.TipDelay + 50*time.Millisecond)
	loHover(e, p)
	if !v.lo.bulkTips[0].Showing(time.Now()) {
		t.Error("no tooltip on Mark selected as read")
	}
	vaShot(t, e, "activity-bulk-tooltip", false)
	v.endSelect()
	e.frame()
	// the "+"
	pp := loFind(t, e, v.plusX-4, v.plusX+30, int(actHeadH)-17, v.plus.Hovered)
	time.Sleep(popups.TipDelay + 50*time.Millisecond)
	loHover(e, pp)
	if !v.lo.plusTip.Showing(time.Now()) {
		t.Error("no tooltip on the +")
	}
	vaShot(t, e, "activity-plus-tooltip", false)
}
