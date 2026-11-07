package views

// Activity: Slack's 2026 Activity page (/client/<team>/activity-inbox).
// Full width: a header (title, Detailed/Dense toggle, kebab, tabs All /
// DMs / Mentions / Threads / "+"), a filter bar (select-all, Unreads,
// filter menu, search) and an edge-to-edge virtualized feed. Clicking a
// row selects it and opens a 50/50 preview pane on the right (the Host's
// real thread/channel pane when it implements Panes). Measurements are
// from /tmp/ui-views-spec/activity.md.

import (
	"context"
	"image"
	"image/color"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/richview"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

const actMentionTypes = "at_user,at_user_group,at_channel,at_everyone,keyword"

// Tabs. "" types means core's "all" view; DMs filters "all" client-side.
var (
	actTabLabels = []string{"All", "DMs", "Mentions", "Threads"}
	actTabIcons  = []string{"", "dms", "at", "threads"}
	actTabTypes  = []string{"", "", actMentionTypes, "thread_v2"}
	actSegIcons  = []string{"list-view", "list-bullet"} // Detailed, Dense
)

// actFilter is one checkbox row of the filter menu.
type actFilter struct {
	label, icon, types string
	dm                 bool   // restrict to DMs (client-side)
	off                bool   // not supported: drawn disabled
	trail              string // trailing icon
	nocheck            bool
}

var actFilters = [...]actFilter{
	{label: "DMs", icon: "dms", dm: true},
	{label: "Mentions", icon: "at", types: actMentionTypes, trail: "chevron-down"},
	{label: "Threads", icon: "threads", types: "thread_v2"},
	{label: "Channels set to “all new posts”", icon: "hash", off: true},
	{label: "Reactions", icon: "emoji", types: "message_reaction"},
	{label: "Invitations", icon: "user-plus", types: "internal_channel_invite,external_channel_invite"},
	{label: "Apps", icon: "apps", types: "bot_dm_bundle"},
	{label: "Reminders", icon: "remind", off: true},
}

const actFilterReactions = 4

var actExtras = [...]actFilter{
	{label: "VIP", icon: "star", off: true},
	{label: "Cleared items", icon: "check-circle"},
	{label: "Specific channel or section", icon: "hash", off: true, nocheck: true},
}

const (
	actHeadH   unit.Dp = 87 // header (53) + tabs (33) + 1px border
	actBarTop  unit.Dp = 16 // filter bar offset below the header
	actBarH    unit.Dp = 32
	actAvatar  unit.Dp = 28
	actDenseAv unit.Dp = 20
	actLimit           = 30
)

type actLoad uint8

const (
	actFresh   actLoad = iota // tab/filter change: clear and load page 1
	actRefresh                // realtime: reload page 1, keep later pages
	actMore                   // pagination: append the next page
)

type actMenu uint8

const (
	actMenuNone actMenu = iota
	actMenuFilter
	actMenuPlus
)

// actDesc is the precomputed text of one feed row, built once per load so
// Layout doesn't re-resolve names or flatten documents every frame.
type actDesc struct {
	name      string // sender (reactor, inviter, app)
	body      string // plain body when there is no message (invites)
	prev      string // one-line preview (dense rows)
	foot      string // "Mention in #general"
	footIcon  string
	footEmoji string // reaction emoji image (replaces footIcon)
	footBold  bool   // footer is a bold label ("App")
	avatar    string
	tile      bool // channel tile instead of an avatar
	at        time.Time
	when      string // non-relative timestamp
	title     string // conversation title
	topic     string
	thread    string // thread ts the item opens into
	invite    bool
	dm        bool
	hay       string // lower-cased search haystack
}

type actRow struct {
	click             widget.Clickable
	bar               widget.Clickable // hover area of the action bar
	mark, clear, more widget.Clickable
	d                 actDesc
	built             bool

	// This frame's row context, read by the Slack boundaries
	// (ActivityItem and its parts) through method expressions.
	v       *Activity
	it      *core.ActivityItem
	key     string
	unread  bool
	cur     *actDesc // d, or pd when a plugin overrode msg
	pd      actDesc
	nameW   font.Weight
	nameCol color.NRGBA
	nameMax int // dense rows: the name's width cap (px)
}

// Activity is the Activity view.
type Activity struct {
	base

	tabs  *tabs
	dots  [4]int
	plus  widget.Clickable
	plusX int
	seg   [2]widget.Clickable
	dense bool
	kebab widget.Clickable

	allSel    bool
	selAllBtn widget.Clickable
	markAll   widget.Clickable
	unread    toggle
	filterBtn widget.Clickable
	filterX   int
	searchBtn widget.Clickable
	searchOn  bool
	query     *search

	menu        actMenu
	scrim       widget.Clickable
	menuBg      widget.Clickable
	menuClose   widget.Clickable
	filters     [len(actFilters)]bool
	filterC     [len(actFilters)]widget.Clickable
	extraC      [len(actExtras)]widget.Clickable
	showCleared bool
	plusC       [4]widget.Clickable
	starred     map[string]bool // non-nil: only starred conversations

	data     async[core.ActivityPage]
	list     widget.List
	rows     map[string]*actRow
	override map[string]bool // local read state: true = read, false = unread
	cleared  map[string]bool
	// hideRead holds the items already read when an Unreads load started:
	// Slack's Unreads filter keeps items you read while it's on (until the
	// filters change or the feed reloads) but doesn't bring back ones read
	// before.
	hideRead map[string]bool
	notes    actNotes
	patchOn  bool
	wmu      sync.Mutex // serializes core activity writes

	// undo is the "cleared" snackbar after Clear, with its Undo link.
	undo struct {
		item  core.ActivityItem
		key   string
		until time.Time
		btn   widget.Clickable
	}
	vis   []int // indices into the page after client-side filters
	visN  int
	dirty bool
	w     watcher

	mode         actLoad
	wasBusy      bool
	paging       bool
	pageCursor   string
	failedCursor string
	retry        widget.Clickable
	retryMore    widget.Clickable
	showAll      widget.Clickable

	sel      core.ActivityItem
	selKey   string
	selOK    bool
	selDesc  actDesc
	open     widget.Clickable
	nameBtn  widget.Clickable
	closeBtn widget.Clickable
	prevList widget.List
}

// NewActivity builds the Activity view; it loads on first Layout.
func NewActivity(h Host) *Activity {
	v := &Activity{
		base:     newBase(h),
		tabs:     newTabs(actTabLabels...),
		query:    newSearch("Search activity"),
		rows:     map[string]*actRow{},
		override: map[string]bool{},
		cleared:  map[string]bool{},
	}
	v.list.Axis = layout.Vertical
	v.prevList.Axis = layout.Vertical
	return v
}

func actMatch(ev core.Event) bool {
	return ev.Type == "message" && (ev.Mention || ev.Reply) || ev.Type == "counts" || ev.Type == "activity"
}

// watchItems collects realtime "activity" events that carry an item, so
// Layout can patch the loaded row (read state from another client, or the
// echo of our own write) without waiting for the throttled reload.
func (v *Activity) watchItems() {
	if v.patchOn {
		return
	}
	v.patchOn = true
	ch, _ := v.h.Core().Subscribe()
	go func() {
		for ev := range ch {
			if ev.Type != "activity" || ev.Activity == nil {
				continue
			}
			v.notes.mu.Lock()
			v.notes.items = append(v.notes.items, *ev.Activity)
			v.notes.mu.Unlock()
			v.h.Invalidate()
		}
	}()
}

// applyPatches updates loaded rows from realtime activity items: the
// server's read state replaces any local override for that row.
func (v *Activity) applyPatches() {
	v.notes.mu.Lock()
	items := v.notes.items
	v.notes.items = nil
	v.notes.mu.Unlock()
	if len(items) == 0 {
		return
	}
	v.data.mutate(func(p *core.ActivityPage) {
		for _, in := range items {
			key := actKey(&in)
			for i := range p.Items {
				if actKey(&p.Items[i]) == key {
					p.Items[i].Unread = in.Unread
					delete(v.override, key)
				}
			}
		}
	})
	v.dirty = true
}

// Layout draws the view.
func (v *Activity) Layout(gtx C) D {
	v.frame()
	v.w.watch(v.h, actMatch)
	v.watchItems()
	if !v.data.started() {
		v.load(actFresh)
	}
	v.notes.drain(v.h)
	v.applyPatches()
	v.update(gtx)
	v.settle()

	st := actPageClass.Enter(gtx, 0, 0)
	defer st.Leave()
	size := gtx.Constraints.Max
	fill(gtx, image.Rectangle{Max: size}, st.Background(v.p.Bg))
	layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(v.head),
		layout.Flexed(1, v.main),
	)
	v.undoBar(gtx)
	v.menus(gtx)
	return D{Size: size}
}

// update handles input that changes what is loaded or shown.
func (v *Activity) update(gtx C) {
	before := v.uiState()
	defer func() {
		// Input handled here can change what the rest of this frame (or
		// the previous one) drew: make sure the change reaches the screen.
		if v.uiState() != before {
			gtx.Execute(op.InvalidateCmd{})
		}
	}()
	reload := false
	if v.tabs.update(gtx) {
		reload = true
	}
	if v.unread.Update(gtx) {
		reload = true
	}
	if v.retry.Clicked(gtx) {
		reload = true
	}
	if v.retryMore.Clicked(gtx) {
		v.failedCursor = ""
	}
	if v.showAll.Clicked(gtx) {
		v.tabs.Sel, v.unread.Value, v.filters, v.starred, v.showCleared = 0, false, [len(actFilters)]bool{}, nil, false
		v.query.ed.SetText("")
		reload = true
	}
	if v.kebab.Clicked(gtx) {
		v.h.Toast("More Activity options aren't available yet")
	}
	if v.selAllBtn.Clicked(gtx) {
		v.allSel = !v.allSel
	}
	if v.markAll.Clicked(gtx) {
		v.markAllRead()
		v.allSel = false
	}
	if v.filterBtn.Clicked(gtx) {
		v.menu = actToggleMenu(v.menu, actMenuFilter)
	}
	if v.plus.Clicked(gtx) {
		v.menu = actToggleMenu(v.menu, actMenuPlus)
	}
	if v.scrim.Clicked(gtx) || v.menuClose.Clicked(gtx) {
		v.menu = actMenuNone
	}
	for i := range v.filterC {
		if v.filterC[i].Clicked(gtx) && !actFilters[i].off {
			v.filters[i] = !v.filters[i]
			if actFilters[i].dm {
				v.dirty = true
			} else {
				reload = true
			}
		}
	}
	if v.extraC[1].Clicked(gtx) {
		// "Cleared items" shows the archive (activity.feed archive_only).
		v.showCleared, reload = !v.showCleared, true
	}
	if v.undo.btn.Clicked(gtx) && !v.undo.until.IsZero() {
		v.unclear(&v.undo.item, v.undo.key)
		v.undo.until = time.Time{}
	}
	v.extraC[0].Clicked(gtx)
	v.extraC[2].Clicked(gtx)
	switch {
	case v.plusC[0].Clicked(gtx):
		v.unread.Value, v.menu, reload = !v.unread.Value, actMenuNone, true
	case v.plusC[1].Clicked(gtx):
		on := !v.onlyReactions()
		v.filters = [len(actFilters)]bool{}
		v.filters[actFilterReactions] = on
		v.menu, reload = actMenuNone, true
	case v.plusC[2].Clicked(gtx):
		v.toggleStarred()
		v.menu, v.dirty = actMenuNone, true
	case v.plusC[3].Clicked(gtx):
		v.menu = actMenuNone
		v.h.Toast("Custom views aren't available yet")
	}
	if v.searchBtn.Clicked(gtx) {
		v.searchOn = !v.searchOn
		if v.searchOn {
			gtx.Execute(key.FocusCmd{Tag: &v.query.ed})
		} else {
			v.query.ed.SetText("")
			v.dirty = true
		}
	}
	if v.searchOn {
		if _, changed, _ := v.query.update(gtx); changed {
			v.dirty = true
		}
	}
	if v.closeBtn.Clicked(gtx) {
		v.selOK = false
	}
	if v.selOK && v.sel.Channel != "" && (v.open.Clicked(gtx) || v.nameBtn.Clicked(gtx)) {
		if v.selDesc.thread != "" {
			v.h.OpenThread(v.sel.Channel, v.selDesc.thread)
		} else {
			v.h.Navigate("c/" + v.sel.Channel)
		}
	}
	if reload {
		v.load(actFresh)
	} else if v.w.due(gtx, 5*time.Second) && !(v.data.busy() && v.mode == actFresh) {
		v.load(actRefresh)
	}
}

// actUI is the input-driven view state compared around update.
type actUI struct {
	menu                                   actMenu
	sel, search, allSel, dense, unread, cl bool
	tab, filters                           int
	starred                                bool
}

func (v *Activity) uiState() actUI {
	return actUI{menu: v.menu, sel: v.selOK, search: v.searchOn, allSel: v.allSel, dense: v.dense,
		unread: v.unread.Value, cl: v.showCleared, tab: v.tabs.Sel, filters: v.filterCount(), starred: v.starred != nil}
}

func actToggleMenu(cur, m actMenu) actMenu {
	if cur == m {
		return actMenuNone
	}
	return m
}

func (v *Activity) onlyReactions() bool {
	for i, on := range v.filters {
		if on != (i == actFilterReactions) {
			return false
		}
	}
	return true
}

func (v *Activity) toggleStarred() {
	if v.starred != nil {
		v.starred = nil
		return
	}
	v.starred = map[string]bool{}
	for _, s := range v.h.Core().Snapshot().Sections {
		if s.Type == "stars" {
			for _, ch := range s.Channels {
				v.starred[ch] = true
			}
		}
	}
}

// filterCount is the "(N)" shown on the filter menu button.
func (v *Activity) filterCount() int {
	n := 0
	for _, on := range v.filters {
		if on {
			n++
		}
	}
	if v.showCleared {
		n++
	}
	return n
}

func (v *Activity) dmOnly() bool {
	if v.tabs.Sel == 1 {
		return true
	}
	for i, f := range actFilters {
		if f.dm && v.filters[i] {
			return true
		}
	}
	return false
}

// types returns the activity.feed types for the tab and filter menu, and
// whether the combination can't match anything.
func (v *Activity) types() (string, bool) {
	tab := actTabTypes[v.tabs.Sel]
	var fl []string
	for i, f := range actFilters {
		if v.filters[i] && f.types != "" {
			fl = append(fl, f.types)
		}
	}
	if len(fl) == 0 {
		return tab, false
	}
	ft := strings.Join(fl, ",")
	if tab == "" {
		return ft, false
	}
	x := actIntersect(tab, ft)
	return x, x == ""
}

func actIntersect(a, b string) string {
	in := map[string]bool{}
	for _, t := range strings.Split(b, ",") {
		in[t] = true
	}
	var out []string
	for _, t := range strings.Split(a, ",") {
		if in[t] {
			out = append(out, t)
		}
	}
	return strings.Join(out, ",")
}

// settle runs once after each load finishes: rebuilds cached row text
// and records a failed pagination cursor so it is not retried in a loop.
func (v *Activity) settle() {
	if !v.wasBusy || v.data.busy() {
		return
	}
	v.wasBusy = false
	for _, r := range v.rows {
		r.built = false
	}
	v.dirty = true
	if v.paging {
		v.paging = false
		if _, _, err := v.data.get(); err != nil {
			v.failedCursor = v.pageCursor
		}
	}
}

func (v *Activity) load(mode actLoad) {
	c := v.h.Core()
	types, none := v.types()
	opts := core.ActivityOpts{View: "all", Types: types, Unread: v.unread.Value, Archived: v.showCleared, Limit: actLimit}
	old, _, _ := v.data.get()
	if mode == actMore {
		opts.Cursor = old.Next
		v.pageCursor = old.Next
	}
	if mode == actFresh {
		v.rows = map[string]*actRow{}
		v.list.Position = layout.Position{}
		v.failedCursor = ""
		v.vis = v.vis[:0]
		v.dirty = true
		v.hideRead = nil
		if opts.Unread {
			v.hideRead = map[string]bool{}
			for k, read := range v.override {
				if read {
					v.hideRead[k] = true
				}
			}
		}
	}
	sticky := opts.Unread
	v.mode, v.paging, v.wasBusy = mode, mode == actMore, true
	v.data.load(v.h, mode != actFresh, func(ctx context.Context) (core.ActivityPage, error) {
		if none {
			return core.ActivityPage{}, nil
		}
		p, err := c.Activity(ctx, opts)
		if err != nil {
			return p, err
		}
		switch mode {
		case actMore:
			return actAppend(old, p), nil
		case actRefresh:
			if sticky {
				// Unreads: rows already shown stay until the filters change.
				return actSticky(p, old), nil
			}
			return actMerge(p, old), nil
		}
		return p, nil
	})
}

// actAppend adds the next page after old, skipping duplicates.
func actAppend(old, p core.ActivityPage) core.ActivityPage {
	seen := make(map[string]bool, len(old.Items))
	items := make([]core.ActivityItem, 0, len(old.Items)+len(p.Items))
	for i := range old.Items {
		seen[actKey(&old.Items[i])] = true
		items = append(items, old.Items[i])
	}
	for i := range p.Items {
		if !seen[actKey(&p.Items[i])] {
			items = append(items, p.Items[i])
		}
	}
	return core.ActivityPage{Items: items, Next: p.Next, Convs: actRefs(old.Convs, p.Convs)}
}

// actMerge replaces the first page with fresh and keeps the older pages
// already loaded, so a realtime refresh doesn't lose the scroll position.
func actMerge(fresh, old core.ActivityPage) core.ActivityPage {
	if len(fresh.Items) == 0 || len(old.Items) <= len(fresh.Items) {
		return fresh
	}
	seen := make(map[string]bool, len(fresh.Items))
	items := make([]core.ActivityItem, 0, len(old.Items)+len(fresh.Items))
	for i := range fresh.Items {
		seen[actKey(&fresh.Items[i])] = true
		items = append(items, fresh.Items[i])
	}
	last := tsTime(fresh.Items[len(fresh.Items)-1].FeedTS)
	for i := range old.Items {
		it := &old.Items[i]
		if !seen[actKey(it)] && tsTime(it.FeedTS).Before(last) {
			items = append(items, *it)
		}
	}
	return core.ActivityPage{Items: items, Next: old.Next, Convs: actRefs(old.Convs, fresh.Convs)}
}

// actSticky is the Unreads refresh: the fresh unread page plus every row
// already loaded (now read or not), newest first.
func actSticky(fresh, old core.ActivityPage) core.ActivityPage {
	seen := make(map[string]bool, len(fresh.Items))
	items := make([]core.ActivityItem, 0, len(old.Items)+len(fresh.Items))
	for i := range fresh.Items {
		seen[actKey(&fresh.Items[i])] = true
		items = append(items, fresh.Items[i])
	}
	for i := range old.Items {
		if !seen[actKey(&old.Items[i])] {
			items = append(items, old.Items[i])
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return actTSLess(items[j].FeedTS, items[i].FeedTS) })
	next := fresh.Next
	if len(old.Items) > len(fresh.Items) {
		next = old.Next
	}
	return core.ActivityPage{Items: items, Next: next, Convs: actRefs(old.Convs, fresh.Convs)}
}

// actTSLess compares Slack timestamps ("1730000000.123456") numerically.
func actTSLess(a, b string) bool {
	as, af, _ := strings.Cut(a, ".")
	bs, bf, _ := strings.Cut(b, ".")
	if len(as) != len(bs) {
		return len(as) < len(bs)
	}
	if as != bs {
		return as < bs
	}
	for len(af) < len(bf) {
		af += "0"
	}
	for len(bf) < len(af) {
		bf += "0"
	}
	return af < bf
}

// actRefs merges conversation refs into a new map (never mutating maps the
// UI goroutine may be reading).
func actRefs(a, b map[string]core.ConvRef) map[string]core.ConvRef {
	out := make(map[string]core.ConvRef, len(a)+len(b))
	for k, r := range a {
		out[k] = r
	}
	for k, r := range b {
		out[k] = r
	}
	return out
}

func actKey(it *core.ActivityItem) string {
	if it.Key != "" {
		return it.Key
	}
	return it.Type + "/" + it.Channel + "/" + it.TS + "/" + it.FeedTS
}

// actThread returns the thread an item opens into, or "".
func actThread(it *core.ActivityItem) string {
	if it.ThreadTS != "" && (it.ThreadTS != it.TS || it.Type == "thread_v2") {
		return it.ThreadTS
	}
	return ""
}

func actIsMention(t string) bool {
	switch t {
	case "at_user", "at_user_group", "at_channel", "at_everyone", "keyword":
		return true
	}
	return false
}

func actReplies(n int, adj string) string {
	w := "replies"
	if n == 1 {
		w = "reply"
	}
	s := strconv.Itoa(n) + " "
	if adj != "" {
		s += adj + " "
	}
	return s + w
}

// actRel is Slack's relative row time for items under an hour old, and
// how long until it changes.
func actRel(age time.Duration) (string, time.Duration) {
	if age < time.Minute {
		return "Just now", time.Minute - age
	}
	m := int(age / time.Minute)
	left := time.Minute - age%time.Minute
	if m == 1 {
		return "1 min", left
	}
	return strconv.Itoa(m) + " mins", left
}

// actStamp is the message timestamp next to a name: "3:04 PM" today, else
// "Nov 4th at 3:04 PM" (with the year when it differs).
func actStamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	now := time.Now()
	if sameDay(t, now) {
		return clock(t)
	}
	s := t.Format("Jan 2") + ordinal(t.Day())
	if t.Year() != now.Year() {
		s += ", " + strconv.Itoa(t.Year())
	}
	return s + " at " + clock(t)
}

func (v *Activity) rowFor(key string) *actRow {
	r := v.rows[key]
	if r == nil {
		r = &actRow{}
		v.rows[key] = r
	}
	return r
}

func (v *Activity) isUnread(it *core.ActivityItem, key string) bool {
	if read, ok := v.override[key]; ok {
		return !read
	}
	return it.Unread
}

func (v *Activity) describe(it *core.ActivityItem, refs map[string]core.ConvRef) actDesc {
	d := actDesc{at: tsTime(it.FeedTS), thread: actThread(it)}
	d.when = listTime(d.at)
	kind := ""
	if it.Channel != "" {
		d.title = v.convTitle(it.Channel, refs)
		if r, ok := refs[it.Channel]; ok {
			kind = r.Kind
		} else {
			kind = v.h.Core().ConvRef(it.Channel).Kind
		}
		if c, ok := v.h.Core().Conv(it.Channel); ok {
			d.topic = oneLine(c.Topic)
		}
	}
	d.dm = isDMKind(kind) || strings.HasPrefix(it.Channel, "D")
	place := d.title
	switch kind {
	case "im":
		place = "direct message"
	case "mpim":
		place = "group message"
	}
	name, av := v.authorOf(it.Msg)
	if it.Author != "" {
		if name == "" {
			name = v.userName(it.Author)
		}
		if av == "" {
			if u, ok := v.user(it.Author); ok {
				av = u.Avatar
			}
		}
	}
	d.name, d.avatar = name, av
	d.prev = preview(it.Msg)
	switch {
	case actIsMention(it.Type):
		d.footIcon = "at"
		d.foot = "Mention in " + place
		if it.Type == "keyword" {
			d.foot = "Keyword in " + place
		}
		if d.dm && it.Type == "at_user" {
			d.footIcon, d.foot = "dms", "Mention in "+place
		}
	case it.Type == "thread_v2":
		d.footIcon, d.foot = "threads", "Thread in "+place
	case it.Type == "message_reaction":
		d.footIcon, d.foot = "emoji", "Reaction in "+place
		if r := it.Reaction; r != nil {
			d.footEmoji = r.URL
			d.name, d.avatar = v.userName(r.User), ""
			if u, ok := v.user(r.User); ok {
				d.avatar = u.Avatar
			}
		}
	case it.Type == "internal_channel_invite" || it.Type == "external_channel_invite":
		d.invite, d.footIcon, d.foot = true, "user-plus", "Invitation to "+d.title
		if d.name != "" {
			d.body = d.name + " invited you to join " + d.title
		} else {
			d.name = d.title
			d.body = "You were invited to join " + d.title
		}
		d.tile = d.avatar == ""
		d.prev = d.body
	case it.Type == "bot_dm_bundle":
		if d.name == "" {
			d.name = d.title
		}
		d.foot, d.footBold = "App", true
		if it.Count > 1 && d.prev != "" {
			d.prev = strconv.Itoa(it.Count) + " new messages · " + d.prev
		}
	case it.Type == "list_record_assigned":
		d.footIcon, d.foot = "list-view", "Assigned to you in a list"
	case it.Type == "list_user_mentioned":
		d.footIcon, d.foot = "list-view", "Mention in a list"
	default:
		if d.dm {
			d.footIcon, d.foot = "dms", "Direct message"
		} else if place != "" {
			d.foot = "In " + place
		}
	}
	if d.name == "" {
		d.name = d.title
	}
	d.hay = strings.ToLower(d.name + "\n" + d.prev + "\n" + d.title + "\n" + d.foot)
	return d
}

// rebuild recomputes row text and the client-side filtered index list.
func (v *Activity) rebuild(page *core.ActivityPage) {
	v.vis = v.vis[:0]
	q := strings.ToLower(v.query.text())
	if !v.searchOn {
		q = ""
	}
	dm := v.dmOnly()
	dots := v.tabs.Sel == 0 && v.filterCount() == 0
	if dots {
		v.dots = [4]int{}
	}
	for i := range page.Items {
		it := &page.Items[i]
		key := actKey(it)
		r := v.rowFor(key)
		if !r.built {
			r.d, r.built = v.describe(it, page.Convs), true
		}
		// cleared: true = cleared here (hide from the inbox), false =
		// restored here (hide from the archive).
		if c, ok := v.cleared[key]; ok && c != v.showCleared {
			continue
		}
		if v.unread.Value && v.hideRead[key] {
			continue
		}
		if dots && v.isUnread(it, key) {
			v.dots[0] = 1
			if r.d.dm {
				v.dots[1] = 1
			}
			switch {
			case actIsMention(it.Type):
				v.dots[2] = 1
			case it.Type == "thread_v2":
				v.dots[3] = 1
			}
		}
		if dm && !r.d.dm {
			continue
		}
		if v.starred != nil && !v.starred[it.Channel] {
			continue
		}
		if q != "" && !strings.Contains(r.d.hay, q) {
			continue
		}
		v.vis = append(v.vis, i)
	}
	v.visN, v.dirty = len(page.Items), false
}

// ---- header ----

func (v *Activity) head(gtx C) D {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return v.header(gtx, "Activity",
				layout.Rigid(func(gtx C) D {
					sel := 0
					if v.dense {
						sel = 1
					}
					d, c := v.segmented(gtx, v.seg[:], actSegIcons, sel)
					if c >= 0 {
						v.dense = c == 1
					}
					return d
				}),
				hgap(8),
				layout.Rigid(func(gtx C) D { return v.iconButton(gtx, &v.kebab, "more", 28, 20, v.p.Sec) }),
			)
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 16, Right: 16}.Layout(gtx, func(gtx C) D {
				tg := gtx
				tg.Constraints.Min.X = 0 // natural width: the "+" sits right after the tabs
				d := v.tabStripIcons(tg, v.tabs, actTabIcons, v.dots[:])
				x := d.Size.X + gtx.Dp(8)
				bs := gtx.Dp(24)
				st := op.Offset(image.Pt(x, (d.Size.Y-bs)/2)).Push(gtx.Ops)
				g := gtx
				g.Constraints.Min = image.Point{}
				v.iconButton(g, &v.plus, "plus", 24, 16, v.p.Sec)
				st.Pop()
				v.plusX = gtx.Dp(16) + x
				return D{Size: image.Pt(gtx.Constraints.Max.X, d.Size.Y)}
			})
		}),
		layout.Rigid(func(gtx C) D { return hline(gtx, v.p.Border) }),
	)
}

// ---- main area: list (+ preview pane) ----

func (v *Activity) main(gtx C) D {
	size := gtx.Constraints.Max
	listW := size.X
	if v.selOK {
		listW = size.X / 2
	}
	func() {
		g := gtx
		g.Constraints = layout.Exact(image.Pt(listW, size.Y))
		defer clipMax(g).Pop()
		v.listArea(g)
	}()
	if v.selOK {
		pr := image.Rect(listW, 0, size.X, size.Y)
		func() {
			defer clip.Rect(image.Rectangle{Max: size}).Push(gtx.Ops).Pop()
			theme.Shadow(gtx, pr, 0, gtx.Dp(12), argb(0x1a000000))
		}()
		func() {
			defer op.Offset(pr.Min).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Exact(pr.Size())
			v.pane(g)
		}()
	}
	return D{Size: size}
}

func (v *Activity) listArea(gtx C) D {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: actBarTop, Bottom: 16}.Layout(gtx, v.filterBar)
		}),
		layout.Flexed(1, v.items),
	)
}

// actAt lays w out at x with loose constraints and returns its size.
func actAt(gtx C, x int, w W) D {
	defer op.Offset(image.Pt(x, 0)).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints.Min = image.Point{}
	g.Constraints.Max.X = max(0, g.Constraints.Max.X-x)
	return w(g)
}

func (v *Activity) filterBar(gtx C) D {
	gap := gtx.Dp(8)
	x := gtx.Dp(16)
	x += actAt(gtx, x, v.selAllGroup).Size.X + gap
	x += actAt(gtx, x, v.unreadsBtn).Size.X + gap
	v.filterX = x
	x += actAt(gtx, x, v.filterMenuBtn).Size.X + gap
	x += actAt(gtx, x, func(gtx C) D { return v.outlineIcon(gtx, &v.searchBtn, "search", false) }).Size.X + gap
	if v.searchOn {
		x += actAt(gtx, x, func(gtx C) D {
			gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(260))
			return v.searchBox(gtx, v.query, actBarH)
		}).Size.X + gap
	}
	if v.allSel {
		actAt(gtx, x, func(gtx C) D {
			return layout.Inset{Top: 2}.Layout(gtx, func(gtx C) D {
				return v.button(gtx, &v.markAll, btnOutline, true, "Mark as read")
			})
		})
	}
	return D{Size: image.Pt(gtx.Constraints.Max.X, gtx.Dp(actBarH))}
}

// unreadsBtn is the Unreads quick filter; pressed (aria-pressed) it also
// gets Slack's small lift shadow.
func (v *Activity) unreadsBtn(gtx C) D {
	m := op.Record(gtx.Ops)
	d := v.filterButton(gtx, &v.unread, "unreads", "Unreads")
	call := m.Stop()
	if v.unread.Value {
		theme.Shadow(gtx, image.Rectangle{Max: d.Size}.Add(image.Pt(0, gtx.Dp(1))), gtx.Dp(8), gtx.Dp(2), argb(0x19000000))
	}
	call.Add(gtx.Ops)
	return d
}

// selAllGroup is the select-all checkbox with its caret: 54x32 outline.
func (v *Activity) selAllGroup(gtx C) D {
	return v.selAllBtn.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		sz := image.Pt(gtx.Dp(54), gtx.Dp(actBarH))
		r := image.Rectangle{Max: sz}
		rad := gtx.Dp(8)
		rrect(gtx, r, rad, v.p.Bg)
		if v.selAllBtn.Hovered() {
			rrect(gtx, r, rad, v.p.Hover)
		}
		border(gtx, r, rad, max(1, gtx.Dp(1)), v.p.Border)
		cb := gtx.Dp(16)
		func() {
			defer op.Offset(image.Pt(gtx.Dp(6), (sz.Y-cb)/2)).Push(gtx.Ops).Pop()
			v.actCheck(gtx, 16, v.allSel)
		}()
		cs := gtx.Dp(20)
		func() {
			defer op.Offset(image.Pt(gtx.Dp(28), (sz.Y-cs)/2)).Push(gtx.Ops).Pop()
			v.icon(gtx, "caret-down", 20, v.p.Sec)
		}()
		return D{Size: sz}
	})
}

// filterMenuBtn is the filter icon + caret outline button; it grows to
// show "(N)" when filters are applied (58 -> 73 wide).
func (v *Activity) filterMenuBtn(gtx C) D {
	n := v.filterCount()
	return v.filterBtn.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		on := n > 0 || v.menu == actMenuFilter
		fg := v.p.Sec
		if n > 0 {
			fg = v.p.PillOnFg
		}
		m := op.Record(gtx.Ops)
		d := layout.Inset{Left: 9, Right: 9}.Layout(gtx, func(gtx C) D {
			g := gtx
			g.Constraints.Min.Y = gtx.Dp(actBarH)
			return layout.W.Layout(g, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D { return v.icon(gtx, "filter", 20, fg) }),
					layout.Rigid(func(gtx C) D {
						if n == 0 {
							return D{}
						}
						return layout.Inset{Left: 2}.Layout(gtx, func(gtx C) D {
							return v.label(gtx, 13, wBold, fg, "("+strconv.Itoa(n)+")")
						})
					}),
					layout.Rigid(func(gtx C) D { return v.icon(gtx, "caret-down", 20, fg) }),
				)
			})
		})
		call := m.Stop()
		r := image.Rectangle{Max: d.Size}
		rad := gtx.Dp(8)
		switch {
		case n > 0:
			rrect(gtx, r, rad, v.p.PillOnBg)
			border(gtx, r, rad, max(1, gtx.Dp(1)), v.p.PillOnFg)
		default:
			rrect(gtx, r, rad, v.p.Bg)
			if on || v.filterBtn.Hovered() {
				rrect(gtx, r, rad, v.p.Hover)
			}
			border(gtx, r, rad, max(1, gtx.Dp(1)), v.p.Border)
		}
		call.Add(gtx.Ops)
		return d
	})
}

// actCheck draws a checkbox: radius 4, 1px border; checked is link blue
// with a white check.
func (b *base) actCheck(gtx C, size unit.Dp, on bool) D {
	s := gtx.Dp(size)
	sz := image.Pt(s, s)
	r := image.Rectangle{Max: sz}
	rad := gtx.Dp(4)
	if on {
		rrect(gtx, r, rad, b.p.Link)
		g := gtx
		g.Constraints = layout.Exact(sz)
		layout.Center.Layout(g, func(gtx C) D { return b.icon(gtx, "check", size-3, rgb(0xffffff)) })
	} else {
		rrect(gtx, r, rad, b.p.Bg)
		border(gtx, r, rad, max(1, gtx.Dp(1)), b.p.BorderStr)
	}
	return D{Size: sz}
}

func (v *Activity) items(gtx C) D {
	page, loaded, err := v.data.get()
	if v.dirty || v.visN != len(page.Items) {
		v.rebuild(&page)
	}
	n := len(v.vis)
	switch {
	case !loaded && len(page.Items) == 0:
		return v.skeleton(gtx, 6, v.avSize())
	case err != nil && len(page.Items) == 0:
		return v.errorState(gtx, err, &v.retry)
	}
	failed := v.failedCursor != "" && v.failedCursor == page.Next
	if n == 0 {
		// Client-side filters (DMs, Starred, search) can hide a whole page:
		// keep paging for a while before calling it empty.
		if page.Next != "" && !failed && (v.data.busy() || len(page.Items) < 300) {
			if !v.data.busy() {
				v.load(actMore)
			}
			return v.skeleton(gtx, 6, v.avSize())
		}
		return v.actEmpty(gtx)
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	d := v.scrollList(gtx, &v.list, n+1, func(gtx C, i int) D {
		if i >= n {
			return v.footer(gtx, failed)
		}
		idx := v.vis[i]
		if idx >= len(page.Items) {
			return D{}
		}
		return v.row(gtx, &page.Items[idx], i)
	})
	if page.Next != "" && !failed && !v.data.busy() && v.list.Position.First+v.list.Position.Count >= n-5 {
		v.load(actMore)
	}
	return d
}

func (v *Activity) avSize() unit.Dp {
	if v.dense {
		return actDenseAv
	}
	return actAvatar
}

func (v *Activity) filtered() bool {
	return v.tabs.Sel != 0 || v.unread.Value || v.filterCount() > 0 || v.starred != nil || (v.searchOn && v.query.text() != "")
}

// actEmpty is Slack's empty state: 32px ✅, "All caught up", description
// and a "Show all activity" link when anything narrows the feed.
func (v *Activity) actEmpty(gtx C) D {
	filtered := v.filtered()
	return layout.Center.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(372))
		gtx.Constraints.Min.X = 0
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D {
					if u, _, ok := v.h.Core().Emoji("white_check_mark"); ok && u != "" {
						return v.emojiImg(gtx, u, 32)
					}
					return v.icon(gtx, "check-circle", 32, v.p.Presence)
				})
			}),
			layout.Rigid(func(gtx C) D {
				title := "All caught up"
				if v.showCleared {
					title = "No cleared items"
				}
				return layout.Inset{Bottom: 4}.Layout(gtx, func(gtx C) D { return v.centered(gtx, 16, wBold, v.p.Text, title) })
			}),
			layout.Rigid(func(gtx C) D {
				return v.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: v.p.Text, Align: text.Middle},
					v.emptyBody())
			}),
			layout.Rigid(func(gtx C) D {
				if !filtered {
					return D{}
				}
				return layout.Inset{Top: 12}.Layout(gtx, func(gtx C) D { return v.linkButton(gtx, &v.showAll, 15, "Show all activity") })
			}),
		)
	})
}

// emptyBody is the empty-state description; the archive's copy is ours
// (not observed in Slack).
func (v *Activity) emptyBody() string {
	if v.showCleared {
		return "Items you clear from Activity show up here, so you can restore them."
	}
	return "Looks like things are quiet for now. When there’s new activity, it’ll be here."
}

func (v *Activity) footer(gtx C, failed bool) D {
	switch {
	case v.paging:
		return v.skeleton(gtx, 2, v.avSize())
	case failed:
		return layout.Inset{Top: 12, Bottom: 16}.Layout(gtx, func(gtx C) D {
			return layout.Center.Layout(gtx, func(gtx C) D {
				return v.button(gtx, &v.retryMore, btnOutline, true, "Load more")
			})
		})
	}
	return D{Size: image.Pt(gtx.Constraints.Min.X, gtx.Dp(16))}
}

// ---- rows ----

func (v *Activity) row(gtx C, it *core.ActivityItem, i int) D {
	key := actKey(it)
	r := v.rowFor(key)
	if r.click.Clicked(gtx) {
		if v.selOK && v.selKey == key {
			v.selOK = false
		} else {
			v.choose(it, key, r)
		}
	}
	unread := v.isUnread(it, key)
	if r.mark.Clicked(gtx) {
		if unread {
			v.markRead(it, key)
		} else {
			v.markUnread(it, key)
		}
		unread = !unread
	}
	if r.clear.Clicked(gtx) {
		if v.showCleared {
			v.unclear(it, key) // the archive's X restores the item
		} else {
			v.clear(it, key)
		}
	}
	if r.more.Clicked(gtx) && it.Msg != nil {
		v.h.OpenMessageMenu(image.Point{}, *it.Msg)
	}
	if v.dirty {
		// A click above changed rows (or the pane) already drawn this
		// frame: draw again.
		gtx.Execute(op.InvalidateCmd{})
	}
	sel := v.selOK && v.selKey == key
	r.v, r.it, r.key, r.unread = v, it, key, unread
	d := slackc.Component(gtx, "ActivityItem", r, (*actRow).itemProps, (*actRow).layoutItem)
	// Drain the bar's hover events even when it isn't drawn this frame, so
	// moving from the row onto the bar keeps it up.
	for {
		if _, ok := r.bar.Update(gtx); !ok {
			break
		}
	}
	if sel || r.click.Hovered() || r.bar.Hovered() {
		y := -gtx.Dp(25)
		if i == v.list.Position.First {
			// Keep the first visible row's bar inside the list's clip.
			y = min(max(y, v.list.Position.Offset), d.Size.Y-gtx.Dp(40))
		}
		v.hoverBar(gtx, r, unread, d.Size.X, y)
	}
	return d
}

// --- Slack boundaries of an activity row (docs/PLUGINS-components.md §1.7) ---

// itemShape is Slack's activityItem prop.
func (r *actRow) itemShape() slackc.Props {
	it := r.it
	p := slackc.Props{"key": r.key, "type": it.Type, "channelId": it.Channel, "messageTs": it.TS,
		"feedTs": it.FeedTS, "ts": it.FeedTS, "isUnread": r.unread, "isArchived": false, "isPriority": false}
	if it.ThreadTS != "" {
		p["threadTs"] = it.ThreadTS
	}
	if it.Count > 0 {
		p["unreadCount"] = it.Count
	}
	if rx := it.Reaction; rx != nil {
		p["reactionName"], p["reactionUser"] = rx.Name, rx.User
	}
	return p
}

func (r *actRow) density() string {
	if r.v.dense {
		return "dense"
	}
	return "detailed"
}

// itemProps: ActivityItem {activityItem, msg, className, isRead, density}.
func (r *actRow) itemProps() slackc.Props {
	v := r.v
	return slackc.Props{"$key": r.key, "activityItem": r.itemShape(), "msg": msgProps(r.it.Msg),
		"className": "", "isRead": !r.unread, "isSelected": v.selOK && v.selKey == r.key,
		"density": r.density(), "showAsCard": false, "inTriageMode": false}
}

// layoutItem is the row (ActivityItem's native). Under a patch it honours
// className (the known StreamerMode classes, for CSS) and msg (the sender's
// name and avatar, e.g. ShowRealUser).
func (r *actRow) layoutItem(gtx C, p slackc.Props) D {
	v, it := r.v, r.it
	r.cur = &r.d
	sel := v.selOK && v.selKey == r.key
	hov := r.click.Hovered()
	var mods uint64
	if p != nil {
		mods = classMods(p.StrOr("className", ""), actItemMods)
		if mp := p.Map("msg"); mp != nil && it.Msg != nil && !r.d.invite && it.Reaction == nil {
			om := msgOverride(it.Msg, mp)
			r.pd = r.d
			if n, av := v.authorOf(&om); n != "" {
				r.pd.name, r.pd.avatar = n, av
			}
			r.cur = &r.pd
		}
	}
	if hov {
		mods |= actModHovered
	}
	if sel {
		mods |= actModSelected
	}
	st := actItemClass.Enter(gtx, hoverState(hov), mods)
	defer st.Leave()
	if st.Hidden() {
		return D{}
	}
	if !st.Active() {
		return r.itemBody(gtx, st, sel)
	}
	return st.Wrap(gtx, func(gtx C) D { return r.itemBody(gtx, st, sel) })
}

func (r *actRow) itemBody(gtx C, st slackc.Styled, sel bool) D {
	v := r.v
	return r.click.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		m := op.Record(gtx.Ops)
		dims := v.rowContent(gtx, r)
		call := m.Stop()
		rect := image.Rectangle{Max: dims.Size}
		bg := v.p.ReadFill
		if r.unread {
			bg = v.p.Bg
		}
		if sel {
			bg = v.p.SelBg
		}
		if r.click.Hovered() {
			bg = v.actHover(bg)
		}
		fill(gtx, rect, st.Background(bg))
		call.Add(gtx.Ops)
		h := max(1, gtx.Dp(1))
		fill(gtx, image.Rect(0, dims.Size.Y-h, dims.Size.X, dims.Size.Y), v.p.Border)
		return dims
	})
}

func (r *actRow) avSize() unit.Dp {
	if r.v.dense {
		return actDenseAv
	}
	return actAvatar
}

// avatarProps: ActivityRowContentSenderAvatarLayout {activityItem, size,
// density, children: the avatar}.
func (r *actRow) avatarProps() slackc.Props {
	size := "large"
	if r.v.dense {
		size = "small"
	}
	av := slackc.Props{"size": int(r.avSize()), "isInteractive": false}
	if m := r.it.Msg; m != nil {
		if m.User != "" {
			av["userId"] = m.User
		}
		if m.BotID != "" {
			av["botId"] = m.BotID
		}
	}
	return slackc.Props{"$key": r.key + "/avatar", "activityItem": r.itemShape(), "size": size, "density": r.density(),
		"children": slackc.Child("ConnectedBaseAvatar", av, func(gtx C, _ slackc.Props) D {
			return r.v.actAvatarOf(gtx, r.cur, r.avSize())
		})}
}

func (r *actRow) layoutAvatar(gtx C, p slackc.Props) D {
	var mods uint64
	if r.v.dense {
		mods = 1
	}
	st := actAvatarClass.Enter(gtx, 0, mods)
	defer st.Leave()
	if st.Hidden() {
		return D{}
	}
	if e, ok := slackc.PropChildren(p); ok {
		return slackc.LayoutElement(gtx, e)
	}
	if !st.Active() {
		return r.v.actAvatarOf(gtx, r.cur, r.avSize())
	}
	return st.Wrap(gtx, func(gtx C) D { return r.v.actAvatarOf(gtx, r.cur, r.avSize()) })
}

// nameProps: ActivityPrivacyBlurredName {activityItem, children: the name}.
// Clack's own prop "blurred" draws the blurred variant.
func (r *actRow) nameProps() slackc.Props {
	return slackc.Props{"$key": r.key + "/name", "activityItem": r.itemShape(), "blurred": false,
		"children": slackc.Child("ActivityItemName", slackc.Props{"name": r.cur.name}, func(gtx C, _ slackc.Props) D {
			return r.nameText(gtx, slackc.Styled{}, false)
		})}
}

// layoutName is the sender/conversation name (ActivityPrivacyBlurredName's
// native): plugin children laid out in its place, or the name, blurred
// when the blurred prop is set.
func (r *actRow) layoutName(gtx C, p slackc.Props) D {
	st := actNameClass.Enter(gtx, 0, 0)
	defer st.Leave()
	if st.Hidden() {
		return D{}
	}
	if r.nameMax > 0 {
		gtx.Constraints.Max.X = r.nameMax
	}
	if e, ok := slackc.PropChildren(p); ok {
		return slackc.LayoutElement(gtx, e)
	}
	blur := p.Bool("blurred") || p.Bool("isBlurred")
	if !st.Active() {
		return r.nameText(gtx, st, blur)
	}
	return st.Wrap(gtx, func(gtx C) D { return r.nameText(gtx, st, blur) })
}

func (r *actRow) nameText(gtx C, st slackc.Styled, blur bool) D {
	v := r.v
	col := st.Color(r.nameCol)
	size := st.FontSize(15)
	name := r.cur.name
	if r.nameMax > 0 {
		gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, r.nameMax)
	}
	switch {
	case blur:
		return v.blurPara(gtx, size, 22, r.nameW, col, name)
	case v.dense:
		return v.label(gtx, size, r.nameW, col, name)
	}
	return v.para(gtx, size, 22, r.nameW, col, 1, name)
}

// preview is the message body, tagged data-qa="activity-item-message".
func (r *actRow) preview(gtx C, lines int) D {
	st := actMsgClass.Enter(gtx, 0, 0)
	defer st.Leave()
	if st.Hidden() {
		return D{}
	}
	if !st.Active() {
		return r.v.msgBodyMuted(gtx, r.it.Msg, false, !r.unread, lines)
	}
	return st.Wrap(gtx, func(gtx C) D { return r.v.msgBodyMuted(gtx, r.it.Msg, false, !r.unread, lines) })
}

func (v *Activity) choose(it *core.ActivityItem, key string, r *actRow) {
	v.sel, v.selKey, v.selOK, v.selDesc = *it, key, true, r.d
	v.markRead(it, key)
	v.prevList.Position = layout.Position{}
	v.dirty = true
}

// markRead marks an item read: locally right away, then in Slack
// (activity.markRead). A failed write puts the row back.
func (v *Activity) markRead(it *core.ActivityItem, key string) {
	if !v.isUnread(it, key) {
		return
	}
	v.setRead(key, true, *it, "Couldn’t mark as read: ", func(ctx context.Context, c *core.App, item core.ActivityItem) error {
		return c.MarkActivityRead(ctx, item)
	})
}

// markUnread is the hover bar's "Mark as unread" (activity.markUnread).
func (v *Activity) markUnread(it *core.ActivityItem, key string) {
	v.setRead(key, false, *it, "Couldn’t mark as unread: ", func(ctx context.Context, c *core.App, item core.ActivityItem) error {
		return c.MarkActivityUnread(ctx, item)
	})
}

func (v *Activity) setRead(key string, read bool, item core.ActivityItem, fail string, write func(context.Context, *core.App, core.ActivityItem) error) {
	prev, had := v.override[key]
	v.override[key] = read
	v.dirty = true
	v.write(fail, func(ctx context.Context, c *core.App) error { return write(ctx, c, item) }, func() {
		if had {
			v.override[key] = prev
		} else {
			delete(v.override, key)
		}
		v.dirty = true
	})
}

// markAllRead is select-all's "Mark as read": the whole inbox at once
// (activity.markAllRead) when nothing narrows it, else the unread rows
// shown.
func (v *Activity) markAllRead() {
	page, _, _ := v.data.get()
	var items []core.ActivityItem
	var keys []string
	for _, i := range v.vis {
		if i >= len(page.Items) {
			continue
		}
		it := &page.Items[i]
		if k := actKey(it); v.isUnread(it, k) {
			items, keys = append(items, *it), append(keys, k)
		}
	}
	if len(items) == 0 {
		return
	}
	prev := make(map[string]bool, len(keys))
	for _, k := range keys {
		if r, ok := v.override[k]; ok {
			prev[k] = r
		}
		v.override[k] = true
	}
	v.dirty = true
	all := !v.filtered() && !v.showCleared
	v.write("Couldn’t mark as read: ", func(ctx context.Context, c *core.App) error {
		if all {
			return c.MarkAllActivityRead(ctx, "all")
		}
		return c.MarkActivityRead(ctx, items...)
	}, func() {
		for _, k := range keys {
			if r, ok := prev[k]; ok {
				v.override[k] = r
			} else {
				delete(v.override, k)
			}
		}
		v.dirty = true
	})
}

// clear archives an item (activity.archive): it leaves the inbox at once
// and a snackbar offers Undo, like Slack.
func (v *Activity) clear(it *core.ActivityItem, key string) {
	v.cleared[key] = true
	if v.selOK && v.selKey == key {
		v.selOK = false
	}
	v.dirty = true
	item := *it
	v.undo.item, v.undo.key, v.undo.until = item, key, time.Now().Add(6*time.Second)
	v.write("Couldn’t clear the item: ", func(ctx context.Context, c *core.App) error {
		return c.ClearActivity(ctx, item)
	}, func() {
		delete(v.cleared, key)
		v.undo.until = time.Time{}
		v.dirty = true
	})
}

// unclear restores a cleared item (activity.unarchive).
func (v *Activity) unclear(it *core.ActivityItem, key string) {
	prev, had := v.cleared[key]
	v.cleared[key] = false
	v.dirty = true
	item := *it
	v.write("Couldn’t restore the item: ", func(ctx context.Context, c *core.App) error {
		err := c.UnclearActivity(ctx, item)
		if err == nil {
			// A refresh may have dropped the row while it was cleared:
			// reload so a restored inbox row comes back.
			v.notes.mu.Lock()
			v.notes.fns = append(v.notes.fns, func() {
				if !v.showCleared {
					v.load(actRefresh)
				}
			})
			v.notes.mu.Unlock()
			v.h.Invalidate()
		}
		return err
	}, func() {
		if had {
			v.cleared[key] = prev
		} else {
			delete(v.cleared, key)
		}
		v.dirty = true
	})
}

// write runs a core activity write off the UI goroutine; on failure it
// toasts and runs revert on the UI goroutine. Writes run one at a time so
// Clear then Undo reach Slack in that order.
func (v *Activity) write(fail string, f func(context.Context, *core.App) error, revert func()) {
	c := v.h.Core()
	go func() {
		v.wmu.Lock()
		defer v.wmu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := f(ctx, c); err != nil {
			v.notes.mu.Lock()
			v.notes.msgs = append(v.notes.msgs, fail+friendlyErr(err))
			v.notes.fns = append(v.notes.fns, revert)
			v.notes.mu.Unlock()
			v.h.Invalidate()
		}
	}()
}

// undoBar is the snackbar after Clear: a dark pill at the bottom center
// of the list with "Item cleared" and an Undo link, for six seconds.
func (v *Activity) undoBar(gtx C) {
	if v.undo.until.IsZero() {
		return
	}
	left := v.undo.until.Sub(gtx.Now)
	if left <= 0 {
		v.undo.until = time.Time{}
		return
	}
	gtx.Execute(op.InvalidateCmd{At: v.undo.until})
	size := gtx.Constraints.Max
	listW := size.X
	if v.selOK {
		listW = size.X / 2
	}
	bg, fg, link := rgb(0x1d1c1d), rgb(0xffffff), rgb(0x1d9bd1)
	if v.th.Dark {
		// base-inv-pry / content-inv-pry / content-inv-hgl-1 in dark
		bg, fg, link = rgb(0x212428), rgb(0xf8f8f8), rgb(0x2ba5ce)
	}
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints.Min = image.Point{}
	d := layout.Inset{Top: 10, Bottom: 10, Left: 16, Right: 16}.Layout(g, func(gtx C) D {
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D { return v.label(gtx, 15, wReg, fg, "Item cleared") }),
			hgap(16),
			layout.Rigid(func(gtx C) D {
				return v.undo.btn.Layout(gtx, func(gtx C) D {
					pointerCursor(gtx)
					return v.label(gtx, 15, wBold, link, "Undo")
				})
			}),
		)
	})
	call := m.Stop()
	at := image.Pt(max(0, (listW-d.Size.X)/2), size.Y-d.Size.Y-gtx.Dp(24))
	defer op.Offset(at).Push(gtx.Ops).Pop()
	r := image.Rectangle{Max: d.Size}
	theme.Shadow(gtx, r, gtx.Dp(8), gtx.Dp(6), argb(0x29000000))
	rrect(gtx, r, gtx.Dp(8), bg)
	call.Add(gtx.Ops)
}

// actNotes queues toasts from background goroutines; Layout shows them on
// the UI goroutine (Host.Toast isn't goroutine-safe).
type actNotes struct {
	mu    sync.Mutex
	msgs  []string
	fns   []func()            // reverts of failed writes (Activity)
	items []core.ActivityItem // realtime activity items (Activity)
}

func (n *actNotes) add(h Host, s string) {
	n.mu.Lock()
	n.msgs = append(n.msgs, s)
	n.mu.Unlock()
	h.Invalidate()
}

func (n *actNotes) drain(h Host) {
	n.mu.Lock()
	msgs, fns := n.msgs, n.fns
	n.msgs, n.fns = nil, nil
	n.mu.Unlock()
	for _, f := range fns {
		f()
	}
	for _, s := range msgs {
		h.Toast(s)
	}
}

func (v *Activity) rowWhen(gtx C, d *actDesc) string {
	if d.at.IsZero() {
		return ""
	}
	if age := gtx.Now.Sub(d.at); age >= 0 && age < time.Hour {
		s, left := actRel(age)
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(left)})
		return s
	}
	return d.when
}

func (v *Activity) rowContent(gtx C, r *actRow) D {
	it, d, unread := r.it, r.cur, r.unread
	nameW, nameCol, bodyCol := wReg, v.p.Sec, v.p.Sec
	if unread {
		nameW, nameCol, bodyCol = wBlack, v.p.Text, v.p.Text
	}
	r.nameW, r.nameCol, r.nameMax = nameW, nameCol, 0
	when := v.rowWhen(gtx, d)
	if v.dense {
		return v.denseContent(gtx, r, bodyCol, when)
	}
	return layout.Inset{Top: 12, Bottom: 12, Left: 16, Right: 20}.Layout(gtx, func(gtx C) D {
		return row(gtx, layout.Start,
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 6}.Layout(gtx, func(gtx C) D {
					return slackc.Component(gtx, "ActivityRowContentSenderAvatarLayout", r, (*actRow).avatarProps, (*actRow).layoutAvatar)
				})
			}),
			hgap(8),
			layout.Flexed(1, func(gtx C) D {
				return col(gtx,
					layout.Rigid(func(gtx C) D {
						g := gtx
						g.Constraints.Min.Y = gtx.Dp(22)
						return row(g, layout.Middle,
							layout.Rigid(func(gtx C) D {
								return slackc.Component(gtx, "ActivityPrivacyBlurredName", r, (*actRow).nameProps, (*actRow).layoutName)
							}),
							hgap(8),
							layout.Rigid(func(gtx C) D {
								return layout.Inset{Top: 2}.Layout(gtx, func(gtx C) D { return v.para(gtx, 13, 18, wReg, v.p.Muted, 1, when) })
							}),
						)
					}),
					layout.Rigid(func(gtx C) D {
						if it.Msg != nil && !d.invite {
							return r.preview(gtx, 2)
						}
						if d.body == "" {
							return D{}
						}
						return v.para(gtx, 15, 22, wReg, bodyCol, 2, d.body)
					}),
					layout.Rigid(func(gtx C) D {
						if d.foot == "" {
							return D{}
						}
						return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D { return v.actFooter(gtx, d) })
					}),
				)
			}),
			hgap(8),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 5, Bottom: 2}.Layout(gtx, func(gtx C) D { return v.actStatus(gtx, it, unread) })
			}),
		)
	})
}

// denseContent is Slack's Dense layout: one 22px line per item. The
// spec didn't measure it (switching saves a Slack pref), so it is derived
// from the Detailed row: same 16/20 side padding and status column, the
// 20px compact avatar, the sender (15px, 900 when unread), the context
// ("Mention in #general", 13px muted with its 16px icon) and the one-line
// preview, then the time pushed to the right.
func (v *Activity) denseContent(gtx C, r *actRow, bodyCol color.NRGBA, when string) D {
	it, d, unread := r.it, r.cur, r.unread
	return layout.Inset{Top: 8, Bottom: 8, Left: 16, Right: 20}.Layout(gtx, func(gtx C) D {
		g := gtx
		g.Constraints.Min.Y = gtx.Dp(22)
		full := gtx.Constraints.Max.X
		return row(g, layout.Middle,
			layout.Rigid(func(gtx C) D {
				return slackc.Component(gtx, "ActivityRowContentSenderAvatarLayout", r, (*actRow).avatarProps, (*actRow).layoutAvatar)
			}),
			hgap(8),
			layout.Rigid(func(gtx C) D {
				r.nameMax = min(gtx.Constraints.Max.X, gtx.Dp(200), full/4)
				return slackc.Component(gtx, "ActivityPrivacyBlurredName", r, (*actRow).nameProps, (*actRow).layoutName)
			}),
			hgap(8),
			layout.Rigid(func(gtx C) D {
				if d.foot == "" {
					return D{}
				}
				gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(240), full/3)
				return layout.Inset{Right: 8}.Layout(gtx, func(gtx C) D { return v.actContext(gtx, d) })
			}),
			layout.Flexed(1, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				if it.Msg != nil && !d.invite && !(it.Type == "bot_dm_bundle" && it.Count > 1) && len(it.Msg.Doc.Blocks) > 0 {
					// The message's first line, drawn like the detailed body
					// (emoji images, mentions) and clipped to one line.
					return r.preview(gtx, 1)
				}
				return layout.W.Layout(gtx, func(gtx C) D { return v.label(gtx, 15, wReg, bodyCol, d.prev) })
			}),
			hgap(12),
			layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wReg, v.p.Muted, when) }),
			hgap(8),
			layout.Rigid(func(gtx C) D { return v.actStatus(gtx, it, unread) }),
		)
	})
}

// actContext is the footer as an inline, natural-width label for Dense
// rows.
func (v *Activity) actContext(gtx C, d *actDesc) D {
	w := wReg
	if d.footBold {
		w = wBold
	}
	return row(gtx, layout.Middle,
		layout.Rigid(func(gtx C) D {
			switch {
			case d.footEmoji != "":
				return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D { return v.emojiImg(gtx, d.footEmoji, 16) })
			case d.footIcon != "":
				return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D { return v.icon(gtx, d.footIcon, 16, v.p.Muted) })
			}
			return D{}
		}),
		layout.Rigid(func(gtx C) D { return v.label(gtx, 13, w, v.p.Muted, d.foot) }),
	)
}

// actFooter draws the row footer: 16px type icon (or the reaction emoji),
// 4px gap, 13px/18px muted context; "App" style labels are bold.
func (v *Activity) actFooter(gtx C, d *actDesc) D {
	w := wReg
	if d.footBold {
		w = wBold
	}
	return row(gtx, layout.Middle,
		layout.Rigid(func(gtx C) D {
			switch {
			case d.footEmoji != "":
				return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D { return v.emojiImg(gtx, d.footEmoji, 16) })
			case d.footIcon != "":
				return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D { return v.icon(gtx, d.footIcon, 16, v.p.Muted) })
			}
			return D{}
		}),
		layout.Flexed(1, func(gtx C) D {
			gtx.Constraints.Min.X = 0
			return v.para(gtx, 13, 18, w, v.p.Muted, 1, d.foot)
		}),
	)
}

// actStatus is the right column: the unread count pill, or the gray
// "done" check for read items.
func (v *Activity) actStatus(gtx C, it *core.ActivityItem, unread bool) D {
	if unread {
		return v.countBadge(gtx, max(1, it.Count))
	}
	s := gtx.Dp(20)
	c := rgb(0xd7d7da)
	if v.th.Dark {
		c = rgb(0x212428) // plt-gray-10
	}
	theme.Circle(gtx, image.Pt(s/2, s/2), s, c)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(s, s))
	layout.Center.Layout(g, func(gtx C) D { return v.icon(gtx, "check", 12, v.p.Text) })
	return D{Size: image.Pt(s, s)}
}

// actAvatarOf draws the row's avatar, or a channel tile for invitations.
func (v *Activity) actAvatarOf(gtx C, d *actDesc, size unit.Dp) D {
	if !d.tile {
		return v.avatar(gtx, d.avatar, size)
	}
	s := gtx.Dp(size)
	sz := image.Pt(s, s)
	v.th.Placeholder(gtx, sz, avatarRadius(gtx, size))
	g := gtx
	g.Constraints = layout.Exact(sz)
	layout.Center.Layout(g, func(gtx C) D { return v.icon(gtx, "hash", size*0.6, v.p.Sec) })
	return D{Size: sz}
}

// hoverBar is the row's floating action bar: 104x40 at the top-right,
// straddling the row's top border; three 32x32 buttons with 18px icons.
func (v *Activity) hoverBar(gtx C, r *actRow, unread bool, rowW, y int) {
	bw, bh := gtx.Dp(104), gtx.Dp(40)
	x := max(0, rowW-bw-gtx.Dp(16))
	defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints = layout.Exact(image.Pt(bw, bh))
	r.bar.Layout(g, func(gtx C) D {
		rect := image.Rect(0, 0, bw, bh)
		rad := gtx.Dp(12)
		theme.Shadow(gtx, rect, rad, gtx.Dp(3), argb(0x14000000))
		rrect(gtx, rect, rad, v.p.Surface)
		border(gtx, rect, rad, max(1, gtx.Dp(1)), v.p.Border)
		markIcon := "check"
		if !unread {
			markIcon = "mark-unread"
		}
		func() {
			defer op.Offset(image.Pt(gtx.Dp(4), gtx.Dp(4))).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints.Min = image.Point{}
			row(g, layout.Middle,
				layout.Rigid(func(gtx C) D { return v.iconButton(gtx, &r.mark, markIcon, 32, 18, v.p.Text) }),
				layout.Rigid(func(gtx C) D { return v.iconButton(gtx, &r.clear, "close", 32, 18, v.p.Text) }),
				layout.Rigid(func(gtx C) D { return v.iconButton(gtx, &r.more, "more", 32, 18, v.p.Text) }),
			)
		}()
		return D{Size: rect.Max}
	})
}

// ---- preview pane ----

func (v *Activity) pane(gtx C) D {
	defer clipMax(gtx).Pop()
	fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, v.p.Bg)
	if ps, ok := v.h.(Panes); ok && v.sel.Channel != "" {
		if v.selDesc.thread != "" {
			return ps.ThreadPane(v.sel.Channel, v.selDesc.thread)(gtx)
		}
		return ps.ChannelPane(v.sel.Channel)(gtx)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(v.previewHead),
		layout.Flexed(1, v.previewBody),
	)
}

// previewHead: h 49, padding 0 12px 0 18px, gap 4px, no bottom border;
// channel name button (15px 900, padding 2px 4px, radius 4), topic 13px
// #454447, open button, close.
func (v *Activity) previewHead(gtx C) D {
	d := &v.selDesc
	w, h := gtx.Constraints.Max.X, gtx.Dp(49)
	btn := "Open in channel"
	switch {
	case d.invite:
		btn = "Open channel"
	case d.thread != "":
		btn = "View thread"
	}
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, h))
	layout.Inset{Left: 18, Right: 12}.Layout(g, func(gtx C) D {
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D {
				if d.title == "" {
					return D{}
				}
				return v.nameBtn.Layout(gtx, func(gtx C) D {
					pointerCursor(gtx)
					m := op.Record(gtx.Ops)
					dd := layout.Inset{Top: 2, Bottom: 2, Left: 4, Right: 4}.Layout(gtx, func(gtx C) D {
						return v.label(gtx, 15, wBlack, v.p.Text, d.title)
					})
					call := m.Stop()
					if v.nameBtn.Hovered() {
						rrect(gtx, image.Rectangle{Max: dd.Size}, gtx.Dp(4), v.p.Hover)
					}
					call.Add(gtx.Ops)
					return dd
				})
			}),
			hgap(4),
			layout.Flexed(1, func(gtx C) D { return v.label(gtx, 13, wReg, v.p.Sec, d.topic) }),
			hgap(4),
			layout.Rigid(func(gtx C) D {
				if v.sel.Channel == "" {
					return D{}
				}
				return v.button(gtx, &v.open, btnOutline, true, btn)
			}),
			hgap(4),
			layout.Rigid(func(gtx C) D { return v.iconButton(gtx, &v.closeBtn, "close", 28, 16, v.p.Sec) }),
		)
	})
	return D{Size: image.Pt(w, h)}
}

func (v *Activity) previewBody(gtx C) D {
	d, it := &v.selDesc, &v.sel
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return v.scrollList(gtx, &v.prevList, 5, func(gtx C, i int) D {
		switch i {
		case 0:
			if d.foot == "" {
				return D{Size: image.Pt(0, gtx.Dp(8))}
			}
			return layout.Inset{Top: 8, Bottom: 4, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D { return v.actFooter(gtx, d) })
		case 1:
			if it.Root == nil {
				return D{}
			}
			return v.actMsg(gtx, it.Root, color.NRGBA{}, 20)
		case 2:
			if it.Root == nil || it.Root.ReplyCount == 0 {
				return D{}
			}
			return layout.Inset{Top: 4, Bottom: 4, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wReg, v.p.Muted, actReplies(it.Root.ReplyCount, "")) }),
					hgap(12),
					layout.Flexed(1, func(gtx C) D { return hline(gtx, v.p.Border) }),
				)
			})
		case 3:
			if it.Msg == nil || d.invite {
				if d.body == "" {
					return D{}
				}
				return layout.Inset{Top: 8, Bottom: 8, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
					return v.para(gtx, 15, 22, wReg, v.p.Text, 0, d.body)
				})
			}
			var hl color.NRGBA
			if it.Root != nil {
				hl = v.p.Yellow // the linked reply, like Slack's jump highlight
			}
			return v.actMsg(gtx, it.Msg, hl, 20)
		}
		return D{Size: image.Pt(0, gtx.Dp(16))}
	})
}

// ---- popovers ----

// menus draws the open popover above everything, with a transparent scrim
// that closes it on an outside click.
func (v *Activity) menus(gtx C) {
	if v.menu == actMenuNone {
		return
	}
	size := gtx.Constraints.Max
	g := gtx
	g.Constraints = layout.Exact(size)
	v.scrim.Layout(g, func(gtx C) D { return D{Size: size} })
	switch v.menu {
	case actMenuFilter:
		y := gtx.Dp(actHeadH+actBarTop+actBarH) + gtx.Dp(4)
		v.popover(gtx, image.Pt(v.filterX, y), 300, 4, v.filterMenu)
	case actMenuPlus:
		v.popover(gtx, image.Pt(v.plusX, gtx.Dp(actHeadH)-gtx.Dp(1)), 280, 8, v.plusMenu)
	}
}

// popover: bg #fff, padding 8px 0, shadow 0 0 0 1px #1d1c1d21 + 0 4px
// 12px rgba(0,0,0,.12).
func (v *Activity) popover(gtx C, at image.Point, w, rad unit.Dp, content W) {
	size := gtx.Constraints.Max
	pw := gtx.Dp(w)
	if at.X+pw > size.X {
		at.X = max(0, size.X-pw-gtx.Dp(8))
	}
	defer op.Offset(at).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints = layout.Constraints{Min: image.Pt(pw, 0), Max: image.Pt(pw, max(0, size.Y-at.Y))}
	m := op.Record(gtx.Ops)
	d := v.menuBg.Layout(g, func(gtx C) D {
		return layout.Inset{Top: 8, Bottom: 8}.Layout(gtx, content)
	})
	call := m.Stop()
	r := image.Rectangle{Max: d.Size}
	radius := gtx.Dp(rad)
	theme.Shadow(gtx, r, radius, gtx.Dp(12), argb(0x1f000000))
	rrect(gtx, r, radius, v.p.Surface)
	call.Add(gtx.Ops)
	border(gtx, r, radius, max(1, gtx.Dp(1)), flatten(actRaw(v.th.Dark).Ring, v.p.Surface))
}

// actStack lays out n children top to bottom (a tiny non-scrolling list).
func actStack(gtx C, n int, el layout.ListElement) D {
	y := 0
	w := gtx.Constraints.Max.X
	for i := 0; i < n; i++ {
		m := op.Record(gtx.Ops)
		g := gtx
		g.Constraints.Min.Y = 0
		d := el(g, i)
		call := m.Stop()
		st := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		st.Pop()
		y += d.Size.Y
	}
	return D{Size: image.Pt(w, y)}
}

func (v *Activity) filterMenu(gtx C) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 16, Right: 8, Bottom: 8}.Layout(gtx, func(gtx C) D {
				g := gtx
				g.Constraints.Min.Y = gtx.Dp(32)
				return row(g, layout.Middle,
					layout.Flexed(1, func(gtx C) D { return v.label(gtx, 15, wBold, v.p.Text, "Filter by") }),
					layout.Rigid(func(gtx C) D { return v.iconButton(gtx, &v.menuClose, "close", 28, 16, v.p.Sec) }),
				)
			})
		}),
		layout.Rigid(func(gtx C) D {
			return actStack(gtx, len(actFilters), func(gtx C, i int) D {
				return v.menuRow(gtx, &v.filterC[i], &actFilters[i], v.filters[i])
			})
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 8, Bottom: 8}.Layout(gtx, func(gtx C) D { return hline(gtx, v.p.Border) })
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 16, Bottom: 8}.Layout(gtx, func(gtx C) D {
				return v.label(gtx, 13, wReg, v.p.Muted, "Additional Filters")
			})
		}),
		layout.Rigid(func(gtx C) D {
			return actStack(gtx, len(actExtras), func(gtx C, i int) D {
				return v.menuRow(gtx, &v.extraC[i], &actExtras[i], i == 1 && v.showCleared)
			})
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 8, Bottom: 8, Left: 16, Right: 16}.Layout(gtx, func(gtx C) D {
				sz := image.Pt(gtx.Constraints.Max.X, gtx.Dp(32))
				rrect(gtx, image.Rectangle{Max: sz}, gtx.Dp(8), v.p.Track)
				g := gtx
				g.Constraints = layout.Exact(sz)
				layout.Center.Layout(g, func(gtx C) D { return v.label(gtx, 13, wBold, v.p.Muted, "Create Custom View") })
				return D{Size: sz}
			})
		}),
	)
}

// menuRow: 28px tall, padding 0 16px, 13px checkbox (12px right margin),
// 18px icon, 15px label.
func (v *Activity) menuRow(gtx C, c *widget.Clickable, f *actFilter, on bool) D {
	return c.Layout(gtx, func(gtx C) D {
		w, h := gtx.Constraints.Max.X, gtx.Dp(28)
		fg, ic := v.p.Text, v.p.Sec
		if f.off {
			fg, ic = v.p.Faint, v.p.Faint
		} else {
			pointerCursor(gtx)
			if c.Hovered() {
				fill(gtx, image.Rect(0, 0, w, h), v.actHover(v.p.Surface))
			}
		}
		g := gtx
		g.Constraints = layout.Exact(image.Pt(w, h))
		layout.Inset{Left: 16, Right: 16}.Layout(g, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D {
					if f.nocheck {
						return D{}
					}
					return layout.Inset{Right: 12}.Layout(gtx, func(gtx C) D { return v.actCheck(gtx, 13, on) })
				}),
				layout.Rigid(func(gtx C) D { return v.icon(gtx, f.icon, 18, ic) }),
				hgap(8),
				layout.Flexed(1, func(gtx C) D { return v.label(gtx, 15, wReg, fg, f.label) }),
				layout.Rigid(func(gtx C) D {
					if f.trail == "" {
						return D{}
					}
					return v.icon(gtx, f.trail, 16, ic)
				}),
			)
		})
		return D{Size: image.Pt(w, h)}
	})
}

func (v *Activity) plusMenu(gtx C) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	section := func(s string) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 4, Bottom: 4, Left: 16, Right: 16}.Layout(gtx, func(gtx C) D {
				return v.label(gtx, 13, wReg, v.p.Muted, s)
			})
		})
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		section("Default suggestions"),
		layout.Rigid(func(gtx C) D { return v.plusRow(gtx, &v.plusC[0], "unreads", "Unreads", v.unread.Value) }),
		layout.Rigid(func(gtx C) D { return v.plusRow(gtx, &v.plusC[1], "emoji", "Reactions", v.filters[actFilterReactions]) }),
		section("Suggestions from your sidebar"),
		layout.Rigid(func(gtx C) D { return v.plusRow(gtx, &v.plusC[2], "star", "Starred", v.starred != nil) }),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 8, Bottom: 8}.Layout(gtx, func(gtx C) D { return hline(gtx, v.p.Border) })
		}),
		layout.Rigid(func(gtx C) D { return v.plusRow(gtx, &v.plusC[3], "plus", "Create a custom view", false) }),
	)
}

// plusRow: 28px, 15px label, 16px icon + 8px gap, a check when active.
func (v *Activity) plusRow(gtx C, c *widget.Clickable, icon, label string, on bool) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		w, h := gtx.Constraints.Max.X, gtx.Dp(28)
		if c.Hovered() {
			fill(gtx, image.Rect(0, 0, w, h), v.actHover(v.p.Surface))
		}
		g := gtx
		g.Constraints = layout.Exact(image.Pt(w, h))
		layout.Inset{Left: 16, Right: 16}.Layout(g, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D { return v.icon(gtx, icon, 16, v.p.Sec) }),
				hgap(8),
				layout.Flexed(1, func(gtx C) D { return v.label(gtx, 15, wReg, v.p.Text, label) }),
				layout.Rigid(func(gtx C) D {
					if !on {
						return D{}
					}
					return v.icon(gtx, "check", 16, v.p.Link)
				}),
			)
		})
		return D{Size: image.Pt(w, h)}
	})
}

// ---- shared with Threads ----

// actRaw is the unflattened palette: palOf pre-flattens translucent
// tokens over Bg only, so overlays on other fills (read rows, cards,
// popovers) flatten the raw token over their own base.
func actRaw(dark bool) *pal {
	if dark {
		return &darkPal
	}
	return &lightPal
}

// actHover is the hover overlay (#454447@.06) flattened over base.
func (b *base) actHover(base color.NRGBA) color.NRGBA {
	return flatten(actRaw(b.th.Dark).Hover, base)
}

// actMsg draws a message the way Slack's message list does: 36px avatar,
// 15px 900 name, 12px muted time, rich body, reactions. bg (if any) fills
// the full row width; padX is the horizontal padding.
func (b *base) actMsg(gtx C, m *core.Message, bg color.NRGBA, padX unit.Dp) D {
	name, av := b.authorOf(m)
	return background(gtx, 0, bg, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return layout.Inset{Top: 8, Bottom: 8, Left: padX, Right: padX}.Layout(gtx, func(gtx C) D {
			return row(gtx, layout.Start,
				layout.Rigid(func(gtx C) D { return b.avatar(gtx, av, 36) }),
				hgap(8),
				layout.Flexed(1, func(gtx C) D {
					return col(gtx,
						layout.Rigid(func(gtx C) D {
							return row(gtx, layout.Baseline,
								layout.Rigid(func(gtx C) D { return b.label(gtx, 15, wBlack, b.p.Text, name) }),
								hgap(8),
								layout.Rigid(func(gtx C) D { return b.label(gtx, 12, wReg, b.p.Muted, actStamp(tsTime(m.TS))) }),
							)
						}),
						layout.Rigid(func(gtx C) D { return b.actPaneBody(gtx, m) }),
						layout.Rigid(func(gtx C) D {
							if len(m.Reactions) == 0 {
								return D{}
							}
							return layout.Inset{Top: 4}.Layout(gtx, func(gtx C) D { return b.actReactions(gtx, m.Reactions) })
						}),
					)
				}),
			)
		})
	})
}

// actPaneBody draws a message body in the preview pane. It must not share
// the list row's richview.View (msgBody keys by channel/ts): a View
// records into its own op.Ops, so laying the same View out twice in one
// frame at two widths resets the ops the first CallOp points to, and the
// rest of the frame (footer, later rows, the pane) never paints.
func (b *base) actPaneBody(gtx C, m *core.Message) D {
	if len(m.Doc.Blocks) == 0 {
		return b.body(gtx, m.Doc, m.Text, 0)
	}
	ver := m.EditedTS + ":" + strconv.Itoa(len(m.Text)) + ":" + strconv.Itoa(len(m.Doc.Blocks))
	return b.richDoc(gtx, "pane:"+m.Channel+"/"+m.TS, ver, m.Doc, richview.Options{Edited: m.Edited, Muted: m.System}, 0)
}

// actReactions lays reaction pills out left to right, wrapping.
func (b *base) actReactions(gtx C, rs []core.Reaction) D {
	maxW := gtx.Constraints.Max.X
	gap := gtx.Dp(4)
	x, y, lineH := 0, 0, 0
	for i := range rs {
		m := op.Record(gtx.Ops)
		cg := gtx
		cg.Constraints.Min = image.Point{}
		d := b.actPill(cg, &rs[i])
		call := m.Stop()
		if x > 0 && x+d.Size.X > maxW {
			x, y, lineH = 0, y+lineH+gap, 0
		}
		st := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		st.Pop()
		x += d.Size.X + gap
		lineH = max(lineH, d.Size.Y)
	}
	return D{Size: image.Pt(maxW, y+lineH)}
}

// actPill is one reaction: 24px pill, 16px emoji, 11px bold count; blue
// tint and border when the viewer reacted.
func (b *base) actPill(gtx C, r *core.Reaction) D {
	h := gtx.Dp(24)
	cnt := strconv.Itoa(r.Count)
	tw := b.th.Measure(gtx, 11, wBold, cnt)
	es := gtx.Dp(16)
	padL, gap, padR := gtx.Dp(6), gtx.Dp(4), gtx.Dp(8)
	w := padL + es + gap + tw + padR
	rect := image.Rect(0, 0, w, h)
	bg, fg := b.p.Chip, b.p.Muted
	if r.Self {
		bg, fg = argb(0x1a1d9bd1), b.p.Link
	}
	rrect(gtx, rect, h/2, bg)
	if r.Self {
		border(gtx, rect, h/2, max(1, gtx.Dp(1)), rgb(0x1d9bd1))
	}
	st := op.Offset(image.Pt(padL, (h-es)/2)).Push(gtx.Ops)
	b.emojiImg(gtx, r.URL, 16)
	st.Pop()
	st = op.Offset(image.Pt(padL+es+gap, 0)).Push(gtx.Ops)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(tw, h))
	layout.W.Layout(g, func(gtx C) D { return b.label(gtx, 11, wBold, fg, cnt) })
	st.Pop()
	return D{Size: image.Pt(w, h)}
}
