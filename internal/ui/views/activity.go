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
	"strconv"
	"strings"
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
	vis      []int // indices into the page after client-side filters
	visN     int
	dirty    bool
	w        watcher

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
	return ev.Type == "message" && (ev.Mention || ev.Reply) || ev.Type == "counts"
}

// Layout draws the view.
func (v *Activity) Layout(gtx C) D {
	v.frame()
	v.w.watch(v.h, actMatch)
	if !v.data.started() {
		v.load(actFresh)
	}
	v.update(gtx)
	v.settle()

	size := gtx.Constraints.Max
	fill(gtx, image.Rectangle{Max: size}, v.p.Bg)
	layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(v.head),
		layout.Flexed(1, v.main),
	)
	v.menus(gtx)
	return D{Size: size}
}

// update handles input that changes what is loaded or shown.
func (v *Activity) update(gtx C) {
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
		page, _, _ := v.data.get()
		for _, i := range v.vis {
			if i < len(page.Items) {
				v.override[actKey(&page.Items[i])] = true
			}
		}
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
		v.showCleared, v.dirty = !v.showCleared, true
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
	opts := core.ActivityOpts{View: "all", Types: types, Unread: v.unread.Value, Limit: actLimit}
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
	}
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
		if v.cleared[key] && !v.showCleared {
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
				d := v.tabStripIcons(gtx, v.tabs, actTabIcons, v.dots[:])
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
	x += actAt(gtx, x, func(gtx C) D { return v.filterButton(gtx, &v.unread, "unreads", "Unreads") }).Size.X + gap
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
		return v.skeleton(gtx, 6, actAvatar)
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
			return v.skeleton(gtx, 6, actAvatar)
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
				return layout.Inset{Bottom: 4}.Layout(gtx, func(gtx C) D { return v.centered(gtx, 16, wBold, v.p.Text, "All caught up") })
			}),
			layout.Rigid(func(gtx C) D {
				return v.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: v.p.Text, Align: text.Middle},
					"Looks like things are quiet for now. When there’s new activity, it’ll be here.")
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

func (v *Activity) footer(gtx C, failed bool) D {
	switch {
	case v.paging:
		return v.skeleton(gtx, 2, actAvatar)
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
		v.override[key] = unread // unread -> mark read; read -> mark unread
		unread = !unread
		v.dirty = true
	}
	if r.clear.Clicked(gtx) {
		v.cleared[key] = !v.cleared[key]
		v.override[key] = true
		v.dirty = true
	}
	if r.more.Clicked(gtx) && it.Msg != nil {
		v.h.OpenMessageMenu(image.Point{}, *it.Msg)
	}
	sel := v.selOK && v.selKey == key
	d := r.click.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		m := op.Record(gtx.Ops)
		dims := v.rowContent(gtx, it, &r.d, unread)
		call := m.Stop()
		rect := image.Rectangle{Max: dims.Size}
		bg := v.p.ReadFill
		if unread {
			bg = v.p.Bg
		}
		if sel {
			bg = v.p.SelBg
		}
		fill(gtx, rect, bg)
		if r.click.Hovered() {
			fill(gtx, rect, v.p.Hover)
		}
		call.Add(gtx.Ops)
		h := max(1, gtx.Dp(1))
		fill(gtx, image.Rect(0, dims.Size.Y-h, dims.Size.X, dims.Size.Y), v.p.Border)
		return dims
	})
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

func (v *Activity) choose(it *core.ActivityItem, key string, r *actRow) {
	v.sel, v.selKey, v.selOK, v.selDesc = *it, key, true, r.d
	v.override[key] = true
	v.prevList.Position = layout.Position{}
	v.dirty = true
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

func (v *Activity) rowContent(gtx C, it *core.ActivityItem, d *actDesc, unread bool) D {
	nameW, nameCol, bodyCol := wReg, v.p.Sec, v.p.Sec
	if unread {
		nameW, nameCol, bodyCol = wBlack, v.p.Text, v.p.Text
	}
	when := v.rowWhen(gtx, d)
	if v.dense {
		return v.denseContent(gtx, it, d, unread, nameW, nameCol, bodyCol, when)
	}
	return layout.Inset{Top: 12, Bottom: 12, Left: 16, Right: 20}.Layout(gtx, func(gtx C) D {
		return row(gtx, layout.Start,
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 6}.Layout(gtx, func(gtx C) D { return v.actAvatarOf(gtx, d, actAvatar) })
			}),
			hgap(8),
			layout.Flexed(1, func(gtx C) D {
				return col(gtx,
					layout.Rigid(func(gtx C) D {
						g := gtx
						g.Constraints.Min.Y = gtx.Dp(22)
						return row(g, layout.Middle,
							layout.Rigid(func(gtx C) D { return v.para(gtx, 15, 22, nameW, nameCol, 1, d.name) }),
							hgap(8),
							layout.Rigid(func(gtx C) D {
								return layout.Inset{Top: 2}.Layout(gtx, func(gtx C) D { return v.para(gtx, 13, 18, wReg, v.p.Muted, 1, when) })
							}),
						)
					}),
					layout.Rigid(func(gtx C) D {
						if it.Msg != nil && !d.invite {
							return v.msgBodyMuted(gtx, it.Msg, false, !unread, 2)
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

// denseContent is the single-line Dense layout (not measured by the spec;
// derived from Detailed with Slack's 20px compact avatar).
func (v *Activity) denseContent(gtx C, it *core.ActivityItem, d *actDesc, unread bool, nameW font.Weight, nameCol, bodyCol color.NRGBA, when string) D {
	return layout.Inset{Top: 8, Bottom: 8, Left: 16, Right: 20}.Layout(gtx, func(gtx C) D {
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D { return v.actAvatarOf(gtx, d, actDenseAv) }),
			hgap(8),
			layout.Rigid(func(gtx C) D {
				gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(200))
				return v.label(gtx, 15, nameW, nameCol, d.name)
			}),
			hgap(8),
			layout.Flexed(1, func(gtx C) D { return v.label(gtx, 15, wReg, bodyCol, d.prev) }),
			hgap(8),
			layout.Rigid(func(gtx C) D {
				if d.foot == "" {
					return D{}
				}
				gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(240))
				return v.actFooter(gtx, d)
			}),
			hgap(8),
			layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wReg, v.p.Muted, when) }),
			hgap(8),
			layout.Rigid(func(gtx C) D { return v.actStatus(gtx, it, unread) }),
		)
	})
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
		c = rgb(0x3b3d42)
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
	border(gtx, r, radius, max(1, gtx.Dp(1)), argb(0x211d1c1d))
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
				fill(gtx, image.Rect(0, 0, w, h), v.p.Hover)
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
			fill(gtx, image.Rect(0, 0, w, h), v.p.Hover)
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
						layout.Rigid(func(gtx C) D { return b.msgBody(gtx, m, false, 0) }),
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
