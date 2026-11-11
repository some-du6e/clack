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
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/ctxclick"
	"github.com/some-du6e/clack/internal/ui/hovercard"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const actMentionTypes = "at_user,at_user_group,at_channel,at_everyone,keyword,unjoined_channel_mention"

// Tabs. "" types means core's "all" view; DMs filters "all" client-side.
var (
	actTabLabels = []string{"All", "DMs", "Mentions", "Threads"}
	actTabIcons  = []string{"", "dms", "at", "threads"}
	actTabTypes  = []string{"", "", actMentionTypes, "thread_v2"}
	actSegIcons  = []string{"table", "menu"} // Detailed, Dense (ActivityFilterLayoutToggle's icons)
)

// actFilter is one checkbox row of the filter menu.
type actFilter struct {
	label, icon, types string
	dm                 bool   // restrict to DMs (client-side)
	off                bool   // not supported: drawn disabled
	trail              string // trailing icon
	nocheck            bool
}

// Slack's ActivityFilterByMenu rows (icons are Slack's SvgIcon names;
// the label keeps Slack's straight quotes).
var actFilters = [...]actFilter{
	{label: "DMs", icon: "direct-messages", dm: true},
	{label: "Mentions", icon: "mentions", types: actMentionTypes, trail: "caret-down"},
	{label: "Threads", icon: "threads", types: "thread_v2"},
	{label: `Channels set to "all new posts"`, icon: "channel", types: "channel"},
	{label: "Reactions", icon: "emoji", types: "message_reaction"},
	{label: "Invitations", icon: "paper-plane", types: "internal_channel_invite,external_channel_invite,external_dm_invite,quietly_added_to_channel"},
	{label: "Apps", icon: "apps", types: "bot_dm_bundle"},
	{label: "Reminders", icon: "reminder", types: "saved_reminder"},
}

const actFilterReactions = 4

var actExtras = [...]actFilter{
	{label: "VIP", icon: "vip"},
	{label: "Cleared items", icon: "eraser"},
	{label: "Specific channel or section", icon: "channel", off: true, nocheck: true},
}

const (
	actHeadH   unit.Dp = 87 // header (53) + tabs (33) + 1px border
	actBarTop  unit.Dp = 16 // filter bar offset below the header
	actBarH    unit.Dp = 32
	actAvatar  unit.Dp = 28
	actDenseAv unit.Dp = 20
	actLimit           = 20 // Slack's activity.feed limit
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
	actMenuSelect   // the select-all caret: Select all / reads / unreads, Custom select
	actMenuSettings // the header kebab (ActivitySettingsMenu)
)

// actDesc is the precomputed text of one feed row, built once per load so
// Layout doesn't re-resolve names or flatten documents every frame.
type actDesc struct {
	name      string // sender (reactor, inviter, app)
	body      string // plain body when there is no message (invites)
	prev      string // one-line preview (dense rows)
	foot      string // "Mention in #general"
	footIcon  string
	footEmoji string  // reaction emoji image (replaces footIcon)
	footAlt   string  // the reaction's ":name:", drawn when footEmoji is ""
	footBold  bool    // footer is a bold label ("App")
	reacted   bool    // the pill is "Reacted" followed by the emoji
	pill      bool    // the row has a footer (text or its type icon)
	footText  bool    // the footer has text (ft)
	ft        actFoot // the footer's text parts
	loc       string  // Dense: the location column
	locIcon   string  // Dense: an icon before loc (a list's lists icon)
	locOn     bool
	typeIcon  string // Dense: the 20px type icon after the status
	list      *actListBody
	nameDense string // the sender in Dense rows ("Ana +2")
	team      string // a Slack Connect inviter's organisation, after the name
	teamIcon  string // its icon (TeamIcon, 16px), in both densities
	subtext   string // before the message body ("You: ", "Ana Example: ")
	lines     int    // the body's line cap (0: the density's)
	subReact  bool   // ReactionContent's "Reacted {emoji} to:" / "Reacted to:"
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
	click widget.Clickable
	check widget.Clickable // the triage row's checkbox (RowCheckbox)
	bar   widget.Clickable // hover area of the action bar
	btns  [actBtnCount]widget.Clickable
	ctx   struct{} // the row's context-menu press target
	d     actDesc
	built bool

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
	bodyCol color.NRGBA
	// Overrides from ActivityThread (BetterBlock): the message the row
	// previews instead of it.Msg, and the unread count.
	pm    *core.Message
	tm    *core.Message // the last thread message pm was looked up as
	count int

	// nameHov is Slack's ProfileHoverTrigger on a reaction row's reactor
	// (ActivityItemMember in "X reacted to your message").
	nameHov hovercard.Trigger

	// The read row's mark (actMark) and its tooltip; the Detailed
	// reminder's Mark Complete button; the invite's text actions.
	mark         actMark
	markOK       bool
	markHov      gesture.Hover
	markTipState popups.TipState
	complete     widget.Clickable
	invC         [3]widget.Clickable
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

	// Bulk selection (ActivityFilterBulkAction, activity_bulk.go): picked
	// holds the selected item keys (P0K2 bulkItems), bulkMode is Custom
	// select's bulk action mode, serverAll server-side select-all, lastBulk
	// the last row picked (Shift ranges).
	bulkMode, serverAll             bool
	lastBulk                        string
	picked                          map[string]bool
	selAllBtn, selMenuBtn           widget.Clickable
	selC                            [4]widget.Clickable
	bulkRead, bulkClear, bulkUnread widget.Clickable
	bannerLink                      widget.Clickable
	setC                            [4]widget.Clickable // Settings menu rows
	emptyPick                       int                 // which random "all caught up" Unreads empty state

	// Saved views (activity_views.go): the VIP filter and the view's
	// sections and channels (with filters, unread and showCleared: the
	// view's actState), the custom views, the hidden default types, each
	// view's filters by key, the tab strip's slots and their context
	// targets, the global sort, the header sort and the settings menu's
	// Sort and Density rows, the filter menu's view buttons; selUnread is
	// the opened row's unread state when it was opened.
	vip                        bool
	sections, channels         []string
	customs                    []core.ActivityView
	hidden                     map[string]bool
	stateOf                    map[string]actState
	slots                      []int
	tabCtx                     []actTabCtx
	viewsLoaded                bool
	globalSort                 string
	sortBtn                    widget.Clickable
	setSortC, setDenseC        [2]widget.Clickable
	saveViewBtn, deleteViewBtn widget.Clickable
	selUnread                  bool
	lastViewModal              *actViewModal // the save-view modal last opened (tests)

	// Keyboard (activity_keys.go): the list's focus target, whether it
	// has the focus and came by keyboard (the ring), the focused row and
	// the change ⌘Z undoes.
	kbTag             actKbTag
	kbFocused, kbMode bool
	focusKey          string
	lastUndo          *actUndo

	// Live updates (activity_live.go): the newest unread entry the
	// "New activity" banner announces.
	newUnreadTS string
	bannerNew   widget.Clickable

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
	plusC       []widget.Clickable // the "+" menu's rows (plusItems)

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
	wmu      sync.Mutex      // serializes core activity writes
	listsReq map[string]bool // lists asked for so list rows can name their item
	// densityReq: the saved layout was asked for (activity.views);
	// densitySet: the user picked one since, which wins over the answer.
	densityReq, densitySet bool
	// With activity_global_settings off each tab is a saved view with its
	// own density (activity.views; updateActivityViewsByApi saves it with
	// activity.views.update, except for All): views by tab, their density.
	views     [len(actTabIDs)]core.ActivityView
	viewDense [len(actTabIDs)]bool

	// Reminder rows' saved items (saved.get), by compound key: known,
	// non-existent, asked for; the files reminder footers name.
	saved      map[string]core.SavedItem
	savedGone  map[string]bool
	savedReq   map[string]bool
	fileReq    map[string]bool
	fileTitles map[string]string
	// clearAll is the "Notifications cleared." toast (realtime
	// activity_clear_all_completed); offline: the last page failed for
	// want of a network (ActivityRowLoader's offline indicator).
	clearAll actClearToast
	offline  bool
	// forked is the open IgnoreForkedModal's two choices (tests press
	// them; the modal's rows call them).
	forked [2]func()

	// undo is the "cleared" snackbar after Clear, with its Undo link.
	undo struct {
		item  core.ActivityItem
		key   string
		until time.Time
		btn   widget.Clickable
	}
	vis   []int // indices into the page after client-side filters; -1-i is Slack's day divider before item i
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
		picked:   map[string]bool{},
		hidden:   map[string]bool{},
		stateOf:  map[string]actState{},

		saved:      map[string]core.SavedItem{},
		savedGone:  map[string]bool{},
		savedReq:   map[string]bool{},
		fileReq:    map[string]bool{},
		fileTitles: map[string]string{},
	}
	v.list.Axis = layout.Vertical
	v.prevList.Axis = layout.Vertical
	return v
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
			if ev.Type != "activity" {
				continue
			}
			// handleActivityRTMEvent: activity_deleted drops the entry,
			// activity_updated carries it (activity_invalidated reloads,
			// through the watcher).
			v.notes.mu.Lock()
			switch {
			case ev.Name == "activity_deleted" && ev.ID != "":
				v.notes.gone = append(v.notes.gone, ev.ID)
			case ev.Name == "activity_clear_all_completed" && ev.ID != "":
				key := ev.ID
				v.notes.fns = append(v.notes.fns, func() { v.clearAllCompleted(key) })
			case ev.Name == "activity_invalidated":
				// activityInvalidated: fetch the feed again
				v.notes.fns = append(v.notes.fns, func() {
					if !(v.data.busy() && v.mode == actFresh) {
						v.load(actRefresh)
					}
				})
			case ev.Name == "activity_views_updated":
				raw := ev.Raw
				v.notes.fns = append(v.notes.fns, func() { v.viewsEvent(raw) })
			case ev.Activity != nil:
				v.notes.items = append(v.notes.items, *ev.Activity)
			}
			v.notes.mu.Unlock()
			v.h.Invalidate()
		}
	}()
}

// applyPatches updates loaded rows from realtime activity items: the
// server's read state replaces any local override for that row.
func (v *Activity) applyPatches() {
	v.notes.mu.Lock()
	items, gone := v.notes.items, v.notes.gone
	v.notes.items, v.notes.gone = nil, nil
	v.notes.mu.Unlock()
	if len(items) == 0 && len(gone) == 0 {
		return
	}
	v.data.mutate(func(p *core.ActivityPage) {
		if len(gone) > 0 {
			drop := map[string]bool{}
			for _, k := range gone {
				drop[k] = true
			}
			kept := p.Items[:0:0]
			for i := range p.Items {
				if it := &p.Items[i]; !drop[actKey(it)] && !drop[it.Key] {
					kept = append(kept, *it)
				}
			}
			p.Items = kept
		}
		for _, in := range items {
			v.liveUpsert(p, in) // activity_live.go
		}
	})
	v.dirty = true
}

// Layout draws the view.
func (v *Activity) Layout(gtx C) D {
	v.frame()
	v.watchItems()
	v.loadDensity()
	if !v.data.started() {
		v.load(actFresh)
	}
	v.notes.drain(v.h)
	v.applyPatches()
	if v.viewsLoaded {
		v.syncTabs()
	}
	v.keys(gtx)
	v.tabMenus(gtx)
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
	v.clearAllToast(gtx)
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
	prev := v.slot()
	if v.tabs.update(gtx) {
		v.switchView(prev) // activity_views.go: each view keeps its filters and layout
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
		// "Show all activity": the All view, nothing filtered
		was := v.slot()
		v.tabs.Sel = 0
		if was != 0 {
			v.switchView(was)
		}
		v.setState(actState{})
		v.query.ed.SetText("")
		reload = true
	}
	if v.kebab.Clicked(gtx) {
		v.menu = actToggleMenu(v.menu, actMenuSettings)
	}
	if v.sortBtn.Clicked(gtx) {
		// the header's ActivityFilterSort (bottom-right of its 28px button)
		v.sortMenu(image.Pt(gtx.Constraints.Max.X-gtx.Dp(16+28+8), gtx.Dp(14+28+4)))
	}
	v.bulkClicks(gtx) // activity_bulk.go
	v.settingsClicks(gtx)
	if v.filterBtn.Clicked(gtx) {
		v.menu = actToggleMenu(v.menu, actMenuFilter)
	}
	if v.plus.Clicked(gtx) {
		v.plusClick()
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
	if v.extraC[0].Clicked(gtx) {
		// VIP (filterVipSelected: activity.feed priority_only)
		v.vip, reload = !v.vip, true
	}
	if v.extraC[1].Clicked(gtx) {
		// "Cleared items" shows the archive (activity.feed archive_only).
		v.showCleared, reload = !v.showCleared, true
	}
	if v.undo.btn.Clicked(gtx) && !v.undo.until.IsZero() {
		v.unclear(&v.undo.item, v.undo.key)
		v.undo.until = time.Time{}
	}
	v.extraC[2].Clicked(gtx)
	v.plusClicks(gtx) // activity_views.go
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
		v.endSelect()
		v.emptyPick = int(time.Now().UnixNano() % int64(len(actUnreadsEmpty)))
		v.load(actFresh)
	}
}

// actUI is the input-driven view state compared around update.
type actUI struct {
	menu                                   actMenu
	sel, search, triage, dense, unread, cl bool
	picked                                 int
	tab, filters                           int
	vip, serverAll                         bool
}

func (v *Activity) uiState() actUI {
	return actUI{menu: v.menu, sel: v.selOK, search: v.searchOn, triage: v.triage(), dense: v.dense, picked: len(v.picked),
		unread: v.unread.Value, cl: v.showCleared, tab: v.tabs.Sel, filters: v.filterCount(), vip: v.vip, serverAll: v.serverAll}
}

func actToggleMenu(cur, m actMenu) actMenu {
	if cur == m {
		return actMenuNone
	}
	return m
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
	if v.vip {
		n++
	}
	if _, ok := v.customView(); ok && v.unread.Value {
		n++ // eZ keeps Unreads in a custom view's count
	}
	return n
}

func (v *Activity) dmOnly() bool {
	if v.slot() == 1 {
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
	tab := ""
	if s := v.slot(); s < len(actTabTypes) {
		tab = actTabTypes[s]
	}
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
	_, _, err := v.data.get()
	v.offline = err != nil && actOffline(err)
	if v.paging {
		v.paging = false
		if err != nil {
			v.failedCursor = v.pageCursor
		}
	}
}

func (v *Activity) load(mode actLoad) {
	c := v.h.Core()
	types, none := v.types()
	opts := core.ActivityOpts{View: "all", Types: types, Unread: v.unread.Value, Archived: v.showCleared, Limit: actLimit,
		Priority: v.vip, SectionIDs: v.sections, ChannelIDs: v.channels, Mode: v.feedMode()}
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

// actAlertLabel is Slack's footer for a generic_system_alert category.
func actAlertLabel(category string) string {
	switch category {
	case "CHANNEL":
		return "Channel"
	case "USER_GROUP":
		return "User group"
	case "EXTERNAL_CHANNEL":
		return "External channel"
	case "WORKFLOW":
		return "Workflow"
	case "INVITATION":
		return "Invitation"
	}
	return "Notification"
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
	dest := v.destOf(it, kind)

	// The sender (ActivityRowContentSenderName): the message's author, or
	// the person behind an item without one.
	name, av := v.authorOf(it.Msg)
	author := ""
	if it.Msg != nil {
		author = it.Msg.User
	}
	if it.Author != "" {
		author = firstNonEmpty(author, it.Author)
		if name == "" {
			name = v.userName(it.Author)
		}
		if av == "" {
			if u, ok := v.user(it.Author); ok {
				av = u.Avatar
			}
		}
	}
	actor, actorAv := it.ActorName, ""
	if it.Actor != "" {
		if u, ok := v.user(it.Actor); ok {
			actor, actorAv = displayName(u), u.Avatar
		} else if actor == "" {
			actor = v.userName(it.Actor)
		}
	}
	if name == "" && actor != "" {
		name, av = actor, actorAv
	}
	if author == "USLACKBOT" && it.Type != "message_reaction" && it.Type != "thread_v2" {
		name = "Slackbot"
	}
	d.name, d.avatar = name, av
	d.prev = v.preview(it.Msg)
	self := v.selfID()

	// body sets the text Slack draws instead of a message.
	body := func(s string) {
		d.body, d.invite = s, true
		d.prev = s
	}
	switch {
	case it.Type == "generic_system_alert":
		d.name, d.avatar = "Slack", ""
		body(oneLine(it.Text))
	case it.Type == "saved_reminder":
		d.name, d.avatar = "Reminder", ""
		switch {
		case it.Msg != nil:
			// ReminderMessage: "{author}: " before the message
			if u, ok := v.user(it.Msg.User); ok {
				d.subtext = firstNonEmpty(u.RealName, displayName(u)) + ": "
			} else if name != "" {
				d.subtext = name + ": "
			}
			d.lines = 1
		case it.ReminderItem == "file":
			body("You set a reminder about this file")
		default:
			body(oneLine(it.Text))
		}
	case it.Type == "message_reaction":
		v.reactionSender(it, &d)
		// QTZf ReactionContent with the layout revisions: "Reacted
		// {emoji} to:" in Detailed, "Reacted to:" in Dense; one line
		d.subReact, d.lines = true, 1
	case it.Type == "thread_v2":
		v.threadSender(it, &d, author)
		if it.Msg != nil && it.Msg.User == self {
			d.subtext = "You: "
		}
	case actExternal(it.Type):
		// InviteSenderName: "{user} from {team}", else "Slack Connect
		// Invitation"; ExternalInviteContent for the body.
		d.name, d.avatar = firstNonEmpty(actor, "Slack Connect Invitation"), actorAv
		if actor != "" {
			d.team = it.Team
			if it.Invite != nil {
				d.teamIcon = it.Invite.TeamIcon
			}
		}
		switch {
		case it.InviteStatus == "revoked" || it.InviteStatus == "expired" || actor == "":
			body("You received an invitation that has been ignored, expired, or is no longer available for another reason.")
		case it.Type == "external_dm_invite":
			body("invited you to direct message")
		case it.Title != "":
			body("invited you to #" + strings.TrimPrefix(it.Title, "#"))
		default:
			body("")
		}
		d.tile = d.avatar == ""
	case it.Type == "internal_channel_invite" || it.Type == "quietly_added_to_channel":
		// InviteContent: the message text becomes "invited you to <#C>".
		if it.Type == "quietly_added_to_channel" && actor != "" {
			d.name, d.avatar = actor, actorAv
		}
		body("invited you to " + d.title)
		d.tile = d.avatar == ""
	case it.Type == "shared_workspace_invite":
		body("")
	case it.Type == "team_joiner_invite":
		if actor != "" {
			d.name, d.avatar = actor, actorAv
		}
		if it.JoinerReminder {
			body("Joined your workspace. A welcome from you will help them get started.")
		} else {
			body("Accepted your invitation to join Slack \u2014\u00a0take a second to say hello.")
		}
	case it.Type == "prejoin_dm_welcome_party_alert":
		if actor != "" {
			d.name, d.avatar = actor, actorAv
		}
		body("Was invited to join " + v.teamName() + " \u2014 take a second to say hello.")
	case actIsList(it.Type):
		// the sender (hliv EM): assigned_by, edited_by, mentioned_by; the
		// rest name the message author, which they don't have
		d.name, d.avatar = "", ""
		switch it.Type {
		case "list_record_assigned", "list_record_edited", "list_user_mentioned":
			d.name, d.avatar = actor, actorAv
		}
		if lb, ok := v.listBody(it); ok {
			d.list = &lb
			body(lb.pre + lb.item + lb.post)
		} else {
			body("")
		}
	default: // mentions, DMs, apps, channel posts
		if it.Msg != nil && it.Msg.User == self {
			d.subtext = "You: "
		}
	}
	if d.nameDense == "" {
		d.nameDense = d.name
	}
	if d.avatar == "" && !d.tile && it.Msg == nil && it.Actor == "" && it.Author == "" {
		d.tile = true
	}

	// The footer (Detailed) and the location and type icon (Dense), as
	// ActivityRowContentMultiLine / SingleLine draw them.
	icon := actEntryKind(it)
	if f, ok := v.footerText(it, dest, d.title); ok {
		d.ft, d.footText = f, true
	}
	if actStandaloneIcon(icon) {
		d.footIcon = actIconName(icon)
	}
	d.pill = d.footText || d.footIcon != ""
	d.foot = d.ft.text()
	d.loc, d.locIcon, d.locOn = v.location(it, dest, d.title)
	if dest != destUnknown {
		d.typeIcon = actIconName(icon)
	}
	if icon == "reaction" && it.Reaction != nil {
		d.footEmoji = it.Reaction.URL
		d.footAlt = actEmojiText(it.Reaction.Name)
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
	// plugins' member/channel/message patches land asynchronously: re-derive
	// row text while any is active (otherwise rows stay cached).
	patchedViews := v.viewsPatched()
	dots := v.slot() == 0 && v.filterCount() == 0
	if dots {
		v.dots = [4]int{}
	}
	var lastDay time.Time
	now := time.Now()
	v.ensureLists(page)
	v.ensureSaved(page)
	v.ensureFileTitles(page)
	// VIP unreads on top (activity_views.go actVIPOrder): the unread VIP
	// items under "VIP unreads", then "Everything else"; no day dividers
	// in the first group nor on the first row after the second header
	order, split := []int(nil), -1
	if v.sortVIP() {
		order, split = v.actVIPOrder(page)
	}
	shown := 0
	for n := range page.Items {
		i := n
		if order != nil {
			i = order[n]
			switch {
			case split > 0 && n == 0:
				v.vis = append(v.vis, actVisVIP)
			case split > 0 && n == split:
				v.vis = append(v.vis, actVisElse)
			}
		}
		inVIP := split > 0 && n < split
		it := &page.Items[i]
		key := actKey(it)
		r := v.rowFor(key)
		if !r.built || patchedViews {
			r.d, r.built = v.describe(it, page.Convs), true
		}
		// the store-backed marks (drafts, your replies and reactions,
		// the reminder's saved item) follow every rebuild
		r.mark, r.markOK = v.readMark(it)
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
		if q != "" && !strings.Contains(r.d.hay, q) {
			continue
		}
		// Slack groups the feed by day: no divider for today, then
		// "Yesterday", "Monday, November 3rd", … before each day's first row.
		if t := tsTime(it.FeedTS); !t.IsZero() {
			boundary := split > 0 && n == split
			if !inVIP && !boundary && !sameDay(t, now) && (lastDay.IsZero() || !sameDay(t, lastDay)) {
				v.vis = append(v.vis, -1-i)
			}
			if !inVIP {
				lastDay = t
			}
		}
		v.vis = append(v.vis, i)
		shown++
	}
	if split > 0 && shown == 0 {
		v.vis = v.vis[:0]
	}
	v.visN, v.dirty = len(page.Items), false
}

// actVisVIP and actVisElse mark ActivitySectionHeader's "VIP unreads" and
// "Everything else" rows in v.vis (below every day divider's -1-i).
const (
	actVisVIP  = -1 << 30
	actVisElse = actVisVIP + 1
)

// ---- header ----

func (v *Activity) head(gtx C) D {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return v.header(gtx, "Activity",
				layout.Rigid(func(gtx C) D {
					if !v.globalSettings() {
						return D{} // ActivityFilterLayoutToggle sits in the filter bar
					}
					return v.layoutToggle(gtx)
				}),
				layout.Rigid(func(gtx C) D {
					// ActivityFilterSort (HEADER): the 20px sort icon button
					if !v.headerSort() {
						return D{}
					}
					return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return v.iconButton(gtx, &v.sortBtn, "sort", 28, 20, v.p.Sec) })
				}),
				hgap(8),
				layout.Rigid(func(gtx C) D { return v.iconButton(gtx, &v.kebab, "more", 28, 20, v.p.Sec) }),
			)
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 16, Right: 16}.Layout(gtx, func(gtx C) D {
				tg := gtx
				tg.Constraints.Min.X = 0 // natural width: the "+" sits right after the tabs
				// activity_views.go: the saved views
				d := v.viewTabs(tg)
				x := d.Size.X + gtx.Dp(8)
				bs := gtx.Dp(24)
				st := op.Offset(image.Pt(x, (d.Size.Y-bs)/2)).Push(gtx.Ops)
				g := gtx
				g.Constraints.Min = image.Point{}
				plusCol := v.p.Sec
				if v.viewCount() >= actMaxViews {
					plusCol = v.p.Faint // isAddTabDisabled: 15 views
				}
				v.iconButton(g, &v.plus, "plus", 24, 16, plusCol)
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
	top := 0
	d := layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: actBarTop, Bottom: 16}.Layout(gtx, v.filterBar)
		}),
		layout.Rigid(func(gtx C) D {
			// ActivityBulkSelectionBanner under the bar in triage mode
			if !v.triage() {
				return D{}
			}
			return v.banner(gtx)
		}),
		layout.Flexed(1, func(gtx C) D {
			top = gtx.Constraints.Max.Y
			return v.items(gtx)
		}),
	)
	v.newActivityBanner(gtx, d.Size.Y-top)
	return d
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
	x += actAt(gtx, x, v.selAllGroup).Size.X
	if v.triage() {
		// triage mode: ActivityFilterBulkAction alone (the bulk buttons
		// after the select group, margin-left 8px each)
		v.bulkRows(gtx, x)
		return D{Size: image.Pt(gtx.Constraints.Max.X, gtx.Dp(actBarH))}
	}
	x += gap
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
	if !v.globalSettings() {
		// p-activity_ia4_page__filter_bar_right: the layout toggle
		// (entry point FILTER_BAR) at the bar's end, 16px in.
		m := op.Record(gtx.Ops)
		g := gtx
		g.Constraints.Min = image.Point{}
		d := v.layoutToggle(g)
		call := m.Stop()
		at := image.Pt(max(x, gtx.Constraints.Max.X-gtx.Dp(16)-d.Size.X), (gtx.Dp(actBarH)-d.Size.Y)/2)
		st := op.Offset(at).Push(gtx.Ops)
		call.Add(gtx.Ops)
		st.Pop()
	}
	return D{Size: image.Pt(gtx.Constraints.Max.X, gtx.Dp(actBarH))}
}

// layoutToggle is ActivityFilterLayoutToggle: Detailed (table) and Dense
// (menu).
func (v *Activity) layoutToggle(gtx C) D {
	sel := 0
	if v.dense {
		sel = 1
	}
	d, c := v.segmented(gtx, v.seg[:], actSegIcons, sel)
	if c >= 0 && v.dense != (c == 1) {
		v.setDense(c == 1)
	}
	return d
}

// unreadsBtn is the Unreads quick filter ("Filter unreads"); pressed (aria-pressed) it also
// gets Slack's small lift shadow.
func (v *Activity) unreadsBtn(gtx C) D {
	m := op.Record(gtx.Ops)
	d := v.filterButton(gtx, &v.unread, "mark-as-unread", "Unreads") // Slack's svg mark-as-unread
	call := m.Stop()
	if v.unread.Value {
		theme.Shadow(gtx, image.Rectangle{Max: d.Size}.Add(image.Pt(0, gtx.Dp(1))), gtx.Dp(8), gtx.Dp(2), argb(0x19000000))
	}
	call.Add(gtx.Ops)
	return d
}

// selAllGroup is ActivityFilterBulkAction's select group (54x32): the
// "Select all" checkbox (28 wide, checked while anything is selected)
// and the outline "Select options" caret button (24 wide, a 16px
// caret-down) that opens Select all / reads / unreads / Custom select.
func (v *Activity) selAllGroup(gtx C) D {
	sz := image.Pt(gtx.Dp(54), gtx.Dp(actBarH))
	r := image.Rectangle{Max: sz}
	rad := gtx.Dp(8)
	rrect(gtx, r, rad, v.p.Bg)
	cw := gtx.Dp(28)
	func() {
		g := gtx
		g.Constraints = layout.Exact(image.Pt(cw, sz.Y))
		v.selAllBtn.Layout(g, func(gtx C) D {
			pointerCursor(gtx)
			if v.selAllBtn.Hovered() {
				rrect(gtx, image.Rect(0, 0, cw, sz.Y), rad, v.p.Hover)
			}
			cb := gtx.Dp(16)
			defer op.Offset(image.Pt(gtx.Dp(6), (sz.Y-cb)/2)).Push(gtx.Ops).Pop()
			// checked with anything picked; indeterminate in triage mode
			// while only some of the rows shown are
			if n := len(v.picked); v.triage() && n > 0 && n != v.shownCount() {
				v.actCheckDash(gtx, 16)
			} else {
				v.actCheck(gtx, 16, len(v.picked) > 0)
			}
			return D{Size: image.Pt(cw, sz.Y)}
		})
	}()
	func() {
		defer op.Offset(image.Pt(cw, 0)).Push(gtx.Ops).Pop()
		bw := sz.X - cw
		g := gtx
		g.Constraints = layout.Exact(image.Pt(bw, sz.Y))
		v.selMenuBtn.Layout(g, func(gtx C) D {
			pointerCursor(gtx)
			if v.selMenuBtn.Hovered() || v.menu == actMenuSelect {
				rrect(gtx, image.Rect(0, 0, bw, sz.Y), rad, v.p.Hover)
			}
			cs := gtx.Dp(16)
			defer op.Offset(image.Pt((bw-cs)/2-gtx.Dp(1), (sz.Y-cs)/2)).Push(gtx.Ops).Pop()
			v.icon(gtx, "caret-down", 16, v.p.Sec)
			return D{Size: image.Pt(bw, sz.Y)}
		})
	}()
	border(gtx, r, rad, max(1, gtx.Dp(1)), v.p.Border)
	return D{Size: sz}
}

// bulkButton is a small outline p-refine_button: a 16px icon and the
// label, disabled (faint) while nothing is selected.
func (v *Activity) bulkButton(gtx C, c *widget.Clickable, icon, label string, on bool) D {
	fg := v.p.Text
	if !on {
		fg = v.p.Sec // c-button--disabled: content-sec on base-ter, no border
	}
	return c.Layout(gtx, func(gtx C) D {
		if on {
			pointerCursor(gtx)
		}
		m := op.Record(gtx.Ops)
		d := layout.Inset{Left: 8, Right: 8}.Layout(gtx, func(gtx C) D {
			g := gtx
			g.Constraints.Min.Y = gtx.Dp(actBarH)
			return layout.W.Layout(g, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D { return v.icon(gtx, icon, 16, fg) }),
					layout.Rigid(func(gtx C) D {
						if label == "" {
							return D{} // isSmallWidth: the icon alone
						}
						return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D { return v.label(gtx, 13, wBold, fg, label) })
					}),
				)
			})
		})
		call := m.Stop()
		r := image.Rectangle{Max: d.Size}
		rad := gtx.Dp(8)
		if !on {
			rrect(gtx, r, rad, v.p.Track)
		} else {
			rrect(gtx, r, rad, v.p.Bg)
			if c.Hovered() {
				rrect(gtx, r, rad, v.p.Hover)
			}
			border(gtx, r, rad, max(1, gtx.Dp(1)), v.p.Border)
		}
		call.Add(gtx.Ops)
		return d
	})
}

// shownCount is how many items the list shows (after client-side
// filters).
func (v *Activity) shownCount() int {
	n := 0
	for _, i := range v.vis {
		if i >= 0 {
			n++
		}
	}
	return n
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

// actCheckDash is the indeterminate checkbox: link blue under a white
// 8x2 dash.
func (b *base) actCheckDash(gtx C, size unit.Dp) D {
	s := gtx.Dp(size)
	rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(4), b.p.Link)
	w, h := gtx.Dp(8), max(1, gtx.Dp(2))
	fill(gtx, image.Rect((s-w)/2, (s-h)/2, (s+w)/2, (s-h)/2+h), rgb(0xffffff))
	return D{Size: image.Pt(s, s)}
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
	// the list is the keyboard's focus target (activity_keys.go), the rows
	// inside its area
	v.listFocusEvents(gtx)
	area := v.listFocusArea(gtx, image.Rectangle{Max: gtx.Constraints.Max})
	d := v.scrollList(gtx, &v.list, n+1, func(gtx C, i int) D {
		if i >= n {
			return v.footer(gtx, failed)
		}
		idx := v.vis[i]
		switch {
		case idx == actVisVIP || idx == actVisElse:
			return v.sectionHeader(gtx, idx == actVisVIP)
		case idx < 0:
			if j := -1 - idx; j < len(page.Items) {
				return v.dayDivider(gtx, tsTime(page.Items[j].FeedTS))
			}
			return D{}
		}
		if idx >= len(page.Items) {
			return D{}
		}
		return v.row(gtx, &page.Items[idx], i)
	})
	area.Pop()
	if page.Next != "" && !failed && !v.data.busy() && v.list.Position.First+v.list.Position.Count >= n-5 {
		v.load(actMore)
	}
	return d
}

// dayDivider is Slack's p-activity_page__date_divider: a 50px row with the
// day in 700 13px/18 (#454447) at (16, 16) on the list background.
func (v *Activity) dayDivider(gtx C, t time.Time) D {
	h := gtx.Dp(50)
	w := gtx.Constraints.Max.X
	func() {
		defer op.Offset(image.Pt(gtx.Dp(16), gtx.Dp(16))).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(max(w-gtx.Dp(32), 0), gtx.Dp(18))}
		v.th.Layout(g, theme.TextStyle{Size: 13, Weight: theme.Bold, Color: v.p.Sec, LineHeight: 18, MaxLines: 1}, draftsDay(t))
	}()
	return D{Size: image.Pt(w, h)}
}

func (v *Activity) avSize() unit.Dp {
	if v.dense {
		return actDenseAv
	}
	return actAvatar
}

func (v *Activity) filtered() bool {
	return v.slot() != 0 || v.unread.Value || v.filterCount() > 0 || (v.searchOn && v.query.text() != "")
}

// actEmptyState is one of Slack's Activity empty states
// (getActivityEmptyStateData, client-boot MmpO): the large emoji, the
// title, the description and whether "Show all activity" shows.
type actEmptyState struct {
	emoji, title, body string
	showAll            bool
}

// actUnreadsEmpty is getUnreadsEmptyPageData: Unreads on in the All tab
// picks one at random.
var actUnreadsEmpty = [...]actEmptyState{
	{"raised_hands", "You’re all caught up", "Nice work!", true},
	{"sun_with_face", "Nothing new", "Carry on.", true},
	{"tada", "Ta-da!", "You’re up to date.", true},
	{"horse", "You’ve read all the unreads", "Here’s a pony.", true},
	{"otter", "No unread activity", "Isn’t that nice?", true},
}

// actTabIDs are the saved views' ids, which Slack's "No unread {filter}"
// prints as they are.
var actTabIDs = [...]string{"all", "dms", "mentions", "threads"}

// emptyState picks the empty state the way Slack does with the activity
// inbox on: Unreads in All picks a random getUnreadsEmptyPageData entry,
// Unreads in another tab its getUnreadsEmptyPageDataForFilter entry ("No
// unread mentions"), a search finds "Nothing found", anything else is
// "All caught up" (with "Show all activity" when filters narrow it).
func (v *Activity) emptyState() actEmptyState {
	pick := v.emptyPick % len(actUnreadsEmpty)
	switch {
	case v.unread.Value && (v.slot() == 0 || v.slot() >= actSlotN):
		return actUnreadsEmpty[pick]
	case v.unread.Value:
		e := actUnreadsEmpty[pick]
		e.title = "No unread " + actTabIDs[v.slot()]
		return e
	case v.searchOn && v.query.text() != "":
		return actEmptyState{"thinking_face", "Nothing found", "Filtering from recent notifications.", true}
	}
	return actEmptyState{"white_check_mark", "All caught up",
		"Looks like things are quiet for now. When there’s new activity, it’ll be here.", v.filtered() || v.showCleared}
}

// actEmpty draws the empty state: the 32px emoji, the 16px bold title,
// the 15px description and the "Show all activity" link.
func (v *Activity) actEmpty(gtx C) D {
	e := v.emptyState()
	return layout.Center.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(372))
		gtx.Constraints.Min.X = 0
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D {
					if u, _, ok := v.h.Core().Emoji(e.emoji); ok && u != "" {
						return v.emojiImg(gtx, u, 32)
					}
					return v.icon(gtx, "check-circle", 32, v.p.Presence)
				})
			}),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Bottom: 4}.Layout(gtx, func(gtx C) D { return v.centered(gtx, 16, wBold, v.p.Text, e.title) })
			}),
			layout.Rigid(func(gtx C) D {
				return v.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: v.p.Text, Align: text.Middle}, e.body)
			}),
			layout.Rigid(func(gtx C) D {
				if !e.showAll {
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
		// ActivityRowLoader: Slack's loading spinner under the last row.
		return layout.Inset{Top: 12, Bottom: 16}.Layout(gtx, func(gtx C) D {
			return layout.Center.Layout(gtx, func(gtx C) D { return actSpinner(gtx, gtx.Dp(20), v.p.Muted) })
		})
	case failed && v.offline:
		// ActivityRowLoader without a connection: InlineOfflineIndicator
		return v.offlineFooter(gtx)
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
		// a click opens the row, in triage mode too (its checkbox picks
		// it); the list's focus moves to it
		v.focusKey = key
		switch {
		case v.splitOpen(it, key): // a split modifier click (activity_split.go)
		case v.selOK && v.selKey == key:
			v.selOK = false
		default:
			v.choose(it, key, r)
		}
	}
	unread := v.isUnread(it, key)
	for b := range r.btns {
		if !ctxclick.Clicked(gtx, &r.btns[b]) {
			continue
		}
		switch actBtn(b) {
		case actBtnMarkRead:
			v.markRead(it, key)
			unread = false
		case actBtnMarkUnread:
			v.markUnread(it, key)
			unread = true
		case actBtnClear:
			v.clear(it, key)
		case actBtnRestore:
			v.unclear(it, key)
		case actBtnMore:
			v.actMenuOpen(v.pointerAt(), *it, key, unread)
		case actBtnComplete:
			v.markComplete(it)
		case actBtnReschedule:
			if ph, ok := v.h.(popups.Host); ok {
				if items := v.rescheduleMenu(ph, it); len(items) > 0 {
					ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: image.Rectangle{Min: v.pointerAt(), Max: v.pointerAt()}, Width: 240}))
				}
			}
		}
	}
	if r.complete.Clicked(gtx) {
		v.markComplete(it)
	}
	for i, a := range v.inviteActions(it) {
		if i < len(r.invC) && r.invC[i].Clicked(gtx) {
			v.inviteClicked(it, a)
		}
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &r.ctx, Kinds: pointer.Press})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok && ctxclick.Press(e) {
			v.actMenuOpen(v.pointerAt(), *it, key, unread)
		}
	}
	if v.dirty {
		// A click above changed rows (or the pane) already drawn this
		// frame: draw again.
		gtx.Execute(op.InvalidateCmd{})
	}
	sel := v.selOK && v.selKey == key
	focused := v.kbFocused && v.focusKey == key
	r.v, r.it, r.key, r.unread = v, it, key, unread
	var d D
	if v.triage() {
		// activity_multi_line_row_triage_container: 22px, the 16px
		// checkbox, the card 10px after it, 16px at its right
		lead, end := gtx.Dp(actTriageLead), gtx.Dp(actTriageEnd)
		func() {
			defer op.Offset(image.Pt(lead, 0)).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints.Max.X = max(0, gtx.Constraints.Max.X-lead-end)
			g.Constraints.Min.X = g.Constraints.Max.X
			d = slackc.Component(g, "ActivityItem", r, (*actRow).itemProps, (*actRow).layoutItem)
		}()
		v.rowCheck(gtx, r, d.Size.Y)
		if focused && v.kbMode {
			func() {
				defer op.Offset(image.Pt(lead, 0)).Push(gtx.Ops).Pop()
				v.focusRing(gtx, d.Size)
			}()
		}
		d.Size.X = gtx.Constraints.Max.X
	} else {
		d = slackc.Component(gtx, "ActivityItem", r, (*actRow).itemProps, (*actRow).layoutItem)
		if focused && v.kbMode {
			v.focusRing(gtx, d.Size)
		}
	}
	// Drain the bar's hover events even when it isn't drawn this frame, so
	// moving from the row onto the bar keeps it up.
	for {
		if _, ok := r.bar.Update(gtx); !ok {
			break
		}
	}
	if sel || focused || r.click.Hovered() || r.bar.Hovered() {
		y := -gtx.Dp(25)
		if v.dense {
			// activity_single_line_row_content__actions_toggle: centred on
			// the row (top 50%, translateY(-50%)), right -4px
			y = (d.Size.Y - gtx.Dp(40)) / 2
		} else if i == v.list.Position.First {
			// Keep the first visible row's bar inside the list's clip.
			y = min(max(y, v.list.Position.Offset), d.Size.Y-gtx.Dp(40))
		}
		barW := d.Size.X
		if v.triage() {
			barW -= gtx.Dp(actTriageEnd) // the card's right edge
		}
		v.hoverBar(gtx, r, unread, barW, y)
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
		"density": r.density(), "showAsCard": false, "inTriageMode": false, "isDmTab": v.dmOnly()}
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
				r.pd.name, r.pd.avatar, r.pd.nameDense = n, av, n
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
			if v.picked[r.key] {
				// --bulk-selected (base-hgl-1) beats --unread's base-pry;
				// a read row's --read-fill wins over it (Slack's cascade)
				bg = v.p.PillOnBg
			}
		}
		if sel {
			bg = v.p.SelBg
		}
		if r.click.Hovered() {
			// bordered separators: a hovered row is base-pry under the
			// base-pry-hover overlay, read or not (selected rows keep theirs)
			if !sel {
				bg = v.actHover(v.p.Bg)
			}
		}
		fill(gtx, rect, st.Background(bg))
		call.Add(gtx.Ops)
		cl := clip.Rect(rect).Push(gtx.Ops)
		pass := pointer.PassOp{}.Push(gtx.Ops)
		event.Op(gtx.Ops, &r.ctx)
		pass.Pop()
		cl.Pop()
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
	if !r.unread { // entry_avatar / single_line avatar: opacity .8 until unread
		defer paint.PushOpacity(gtx.Ops, 0.8).Pop()
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
	if v.dense && r.cur.nameDense != "" {
		name = r.cur.nameDense
	}
	if name == "" { // the sender isn't known yet: Slack's loading shimmer
		return v.shimmerLine(gtx, 120)
	}
	if r.nameMax > 0 {
		gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, r.nameMax)
	}
	var d D
	switch {
	case blur:
		return v.blurPara(gtx, size, 22, r.nameW, col, name)
	case v.dense:
		d = v.label(gtx, size, r.nameW, col, name)
	default:
		d = v.para(gtx, size, 22, r.nameW, col, 1, name)
	}
	r.nameHov.Layout(gtx, r.reactor(), d.Size)
	return d
}

// reactor is the member a reaction row's name stands for ("" otherwise).
func (r *actRow) reactor() string {
	if it := r.it; it != nil && it.Type == "message_reaction" && it.Reaction != nil && r.cur == &r.d {
		return it.Reaction.User
	}
	return ""
}

// preview is the message body, tagged data-qa="activity-item-message".
func (r *actRow) preview(gtx C, lines int) D {
	st := actMsgClass.Enter(gtx, 0, 0)
	defer st.Leave()
	if st.Hidden() {
		return D{}
	}
	if !st.Active() {
		return r.v.msgBodyMuted(gtx, r.msg(), false, !r.unread, lines)
	}
	return st.Wrap(gtx, func(gtx C) D { return r.v.msgBodyMuted(gtx, r.msg(), false, !r.unread, lines) })
}

func (v *Activity) choose(it *core.ActivityItem, key string, r *actRow) {
	v.selUnread = v.isUnread(it, key) // selectedItemUnreadState (the VIP order keeps it)
	v.sel, v.selKey, v.selOK, v.selDesc = *it, key, true, r.d
	v.focusKey = key
	v.markRead(it, key)
	if it.ListID != "" && it.ListRecord != "" { // a list item opens its pane (mount_features_lists.go)
		if lo, ok := v.h.(interface{ OpenListItem(listID, recID string) }); ok {
			lo.OpenListItem(it.ListID, it.ListRecord)
		}
	}
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
			if fail != "" {
				v.notes.msgs = append(v.notes.msgs, fail+friendlyErr(err))
			}
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
	gone  []string            // keys of realtime activity_deleted items
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

// rowContent is the inside of an ActivityItem: the row's colours and
// time for this frame, then the Slack boundaries Slack renders there
// (rowKind). Read rows: sender and body content-sec, 400; unread:
// content-pry, the sender 900 in Detailed (activity_multi_line_row_content
// --unread) and 700 in Dense (activity_single_line_row_content--unread).
func (v *Activity) rowContent(gtx C, r *actRow) D {
	nameW, nameCol, bodyCol := wReg, v.p.Sec, v.p.Sec
	if r.unread {
		nameW, nameCol, bodyCol = wBlack, v.p.Text, v.p.Text
		if v.dense {
			nameW = wBold
		}
	}
	r.nameW, r.nameCol, r.nameMax, r.bodyCol = nameW, nameCol, 0, bodyCol
	r.pm, r.count = nil, actUnreadCount(r.it)
	return r.rowKind(gtx)
}

// detailedContent is ActivityRowContentMultiLine (gantry boot-render
// U5s4, CSS activity_multi_line_row_content*): the container's padding
// 12px 20px 12px 16px (grouped, bordered separators); avatar padding-top
// 6px; the stack padding-top 2px: name row 22px (sender, 8px, a 54px
// 13px timestamp), the body (15px/22px, 2 lines), the footer (padding-top
// 8px, 13px/18px: the 16px type icon, 4px, the text); then the status
// column (padding 5px 0 2px), 8px from the stack.
func (v *Activity) detailedContent(gtx C, r *actRow) D {
	d, unread := r.cur, r.unread
	bodyCol, when := r.bodyCol, v.rowWhen(gtx, d)
	return layout.Inset{Top: 12, Bottom: 12, Left: 16, Right: 20}.Layout(gtx, func(gtx C) D {
		return row(gtx, layout.Start,
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 6}.Layout(gtx, func(gtx C) D {
					return slackc.Component(gtx, "ActivityRowContentSenderAvatarLayout", r, (*actRow).avatarProps, (*actRow).layoutAvatar)
				})
			}),
			hgap(8),
			layout.Flexed(1, func(gtx C) D {
				return layout.Inset{Top: 2}.Layout(gtx, func(gtx C) D {
					return col(gtx,
						layout.Rigid(func(gtx C) D {
							g := gtx
							g.Constraints.Min.Y = gtx.Dp(22)
							return row(g, layout.Middle,
								layout.Flexed(1, func(gtx C) D {
									gtx.Constraints.Min.X = 0
									return row(gtx, layout.Middle,
										layout.Rigid(func(gtx C) D {
											gtx.Constraints.Max.X = max(0, gtx.Constraints.Max.X-gtx.Dp(62))
											return slackc.Component(gtx, "ActivityPrivacyBlurredName", r, (*actRow).nameProps, (*actRow).layoutName)
										}),
										layout.Rigid(func(gtx C) D { return v.teamIcon(gtx, d) }),
										layout.Rigid(func(gtx C) D {
											if d.team == "" { // InviteSenderName: the inviter's organisation (Detailed)
												return D{}
											}
											return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D { return v.para(gtx, 15, 22, wReg, r.nameCol, 1, d.team) })
										}),
										hgap(8),
										layout.Rigid(func(gtx C) D {
											// entry_name_timestamp: 54px wide, padding-top 2px
											return layout.Inset{Top: 2}.Layout(gtx, func(gtx C) D { return v.actTime(gtx, when, false) })
										}),
									)
								}),
							)
						}),
						layout.Rigid(func(gtx C) D { return v.actBody(gtx, r, 2, bodyCol) }),
						layout.Rigid(func(gtx C) D {
							// ReminderMessage / ReminderText: Mark Complete
							// under the body while the to-do is incomplete
							// (cZ "detailed" "details")
							if r.it.Type != "saved_reminder" || r.it.ReminderItem != "message" && r.it.ReminderItem != "reminder" || !v.reminderIncomplete(r.it) {
								return D{}
							}
							return v.markCompleteButton(gtx, r)
						}),
						layout.Rigid(func(gtx C) D {
							if !d.pill {
								return D{}
							}
							return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D { return v.actFooter(gtx, d, unread) })
						}),
						layout.Rigid(func(gtx C) D {
							// entry_extras: the Slack Connect invite's
							// text actions (ActivityRowContentInviteActions)
							acts := v.inviteActions(r.it)
							if len(acts) == 0 {
								return D{}
							}
							// entry_extras is a column flex and the text
							// actions' ul is align-self: center
							gtx.Constraints.Min.X = gtx.Constraints.Max.X
							return layout.N.Layout(gtx, func(gtx C) D { return v.inviteButtons(gtx, r, acts) })
						}),
					)
				})
			}),
			hgap(8),
			layout.Rigid(func(gtx C) D {
				switch {
				case unread:
					return layout.Inset{Top: 5, Bottom: 2}.Layout(gtx, func(gtx C) D { return v.actStatus(gtx, r.count, unread) })
				case r.markOK: // read rows: the completed to-do, Draft, replied, reacted
					return layout.Inset{Top: 5, Bottom: 2}.Layout(gtx, func(gtx C) D { return v.markIcon(gtx, r) })
				}
				return D{}
			}),
		)
	})
}

// actDenseH is the Dense row's height: activity_row_content_container
// --dense --design-v3, 50px (padding 10px 12px; the grouped container's
// sides 20px), its bottom border included.
const actDenseH unit.Dp = 50

// denseContent is ActivityRowContentSingleLine (gantry boot-render U5s4,
// CSS activity_single_line_row_content*), one line, 8px gaps: the leading
// block (200px; 100px in a container 300px or narrower) with the 20px
// avatar, the sender (15px, flexible, ellipsis) and the status (the unread
// badge, then the 20px type icon); then the centre (gaps 16px, 4px at
// 625px or narrower): the message (15px, one line), the location (13px,
// 100px wide, 224px from 800px) and the time (13px, 54px, right-aligned,
// hidden while the row is hovered).
func (v *Activity) denseContent(gtx C, r *actRow) D {
	d, unread := r.cur, r.unread
	bodyCol, when := r.bodyCol, v.rowWhen(gtx, d)
	hov := r.click.Hovered() || r.bar.Hovered()
	g := gtx
	g.Constraints = layout.Exact(image.Pt(gtx.Constraints.Max.X, gtx.Dp(actDenseH))) // the bottom border is drawn inside
	return layout.Inset{Left: 20, Right: 20}.Layout(g, func(gtx C) D {
		cw := gtx.Constraints.Max.X
		lead, locW, centreGap := gtx.Dp(200), gtx.Dp(100), unit.Dp(16)
		switch {
		case cw <= gtx.Dp(300):
			lead = gtx.Dp(100)
		case cw >= gtx.Dp(800):
			locW = gtx.Dp(224)
		}
		if cw <= gtx.Dp(625) {
			centreGap = 4
		}
		return layout.W.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D {
					gtx.Constraints = layout.Exact(image.Pt(lead, gtx.Dp(30)))
					return row(gtx, layout.Middle,
						layout.Flexed(1, func(gtx C) D {
							// __user: flex 1, so the status sits at the leading block's end
							gtx.Constraints.Min.X = gtx.Constraints.Max.X
							return row(gtx, layout.Middle,
								layout.Rigid(func(gtx C) D {
									return slackc.Component(gtx, "ActivityRowContentSenderAvatarLayout", r, (*actRow).avatarProps, (*actRow).layoutAvatar)
								}),
								hgap(8),
								layout.Flexed(1, func(gtx C) D {
									gtx.Constraints.Min.X = 0
									r.nameMax = gtx.Constraints.Max.X
									if d.teamIcon != "" {
										r.nameMax = max(0, r.nameMax-gtx.Dp(20))
									}
									return row(gtx, layout.Middle,
										layout.Rigid(func(gtx C) D {
											return slackc.Component(gtx, "ActivityPrivacyBlurredName", r, (*actRow).nameProps, (*actRow).layoutName)
										}),
										layout.Rigid(func(gtx C) D { return v.teamIcon(gtx, d) }),
									)
								}),
							)
						}),
						layout.Rigid(func(gtx C) D {
							switch {
							case unread:
								return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return v.actStatus(gtx, r.count, unread) })
							case r.markOK:
								return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return v.markIcon(gtx, r) })
							}
							return D{}
						}),
						layout.Rigid(func(gtx C) D {
							if d.typeIcon == "" {
								return D{}
							}
							return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return v.actTypeIcon(gtx, d) })
						}),
					)
				}),
				hgap(8),
				layout.Flexed(1, func(gtx C) D {
					gtx.Constraints.Min.X = 0
					return row(gtx, layout.Middle,
						layout.Flexed(1, func(gtx C) D {
							gtx.Constraints.Min.X = gtx.Constraints.Max.X
							if m := r.msg(); m != nil && !d.invite && len(m.Doc.Blocks) > 0 {
								// __message: one 15px/22px line
								gtx.Constraints.Min.Y, gtx.Constraints.Max.Y = gtx.Dp(22), gtx.Dp(22)
								defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
								return v.actBody(gtx, r, 1, bodyCol)
							}
							return layout.W.Layout(gtx, func(gtx C) D { return v.label(gtx, 15, wReg, bodyCol, d.prev) })
						}),
						layout.Rigid(func(gtx C) D {
							if !d.locOn {
								return D{}
							}
							return layout.Inset{Left: centreGap}.Layout(gtx, func(gtx C) D {
								gtx.Constraints = layout.Exact(image.Pt(locW, gtx.Dp(18)))
								w := wReg
								if unread {
									w = wBold
								}
								return row(gtx, layout.Middle,
									layout.Rigid(func(gtx C) D {
										if d.locIcon == "" {
											return D{}
										}
										return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D { return v.icon(gtx, icons.Slack(d.locIcon), 13, v.p.Muted) })
									}),
									layout.Flexed(1, func(gtx C) D {
										gtx.Constraints.Min.X = 0
										return v.para(gtx, 13, 18, w, v.p.Muted, 1, d.loc)
									}),
								)
							})
						}),
						layout.Rigid(func(gtx C) D {
							return layout.Inset{Left: centreGap}.Layout(gtx, func(gtx C) D {
								if hov {
									return D{Size: image.Pt(gtx.Dp(54), gtx.Dp(18))}
								}
								return v.actTime(gtx, when, true)
							})
						}),
					)
				}),
			)
		})
	})
}

// actTypeIcon is the Dense row's ActivityDecoupledTypeIcon: a 20px box,
// content-ter, holding the type's 20px icon or the reaction's emoji.
func (v *Activity) actTypeIcon(gtx C, d *actDesc) D {
	s := gtx.Dp(20)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(s, s))
	return layout.Center.Layout(g, func(gtx C) D {
		if d.typeIcon == "reaction" {
			if d.footEmoji != "" {
				return v.emojiImg(gtx, d.footEmoji, 16)
			}
			return v.emojiAlt(gtx, d.footAlt, 16)
		}
		return v.icon(gtx, icons.Slack(d.typeIcon), 20, v.p.Muted)
	})
}

// actFooter draws the Detailed footer (activity_multi_line_row_content
// __entry_footer): 13px/18px content-ter; the 16px type icon and 4px when
// the type has one; the text with the destination in bold
// (activity_row_content__footer_name, 700), a list's lists icon before
// its title.
func (v *Activity) actFooter(gtx C, d *actDesc, unread bool) D {
	f := d.ft
	ch := []layout.FlexChild{
		layout.Rigid(func(gtx C) D {
			if d.footIcon == "" {
				return D{}
			}
			return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D { return v.icon(gtx, icons.Slack(d.footIcon), 16, v.p.Muted) })
		}),
	}
	if d.footText {
		if f.pre != "" {
			ch = append(ch, layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wReg, v.p.Muted, f.pre) }))
		}
		if f.nameIcon != "" {
			ch = append(ch, layout.Rigid(func(gtx C) D {
				return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D { return v.icon(gtx, icons.Slack(f.nameIcon), 13, v.p.Muted) })
			}))
		}
		if f.name != "" {
			ch = append(ch, layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wBold, v.p.Muted, f.name) }))
		}
		if f.post != "" {
			ch = append(ch, layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wReg, v.p.Muted, f.post) }))
		}
	}
	g := gtx
	g.Constraints.Min.Y = gtx.Dp(18)
	return row(g, layout.Middle, ch...)
}

// actEmojiText is the text aJri gives the reaction's Emoji (":name:"),
// which vbPY keeps as its displayText (an English name is its own
// localized one).
func actEmojiText(name string) string {
	if name == "" {
		return ""
	}
	return ":" + strings.Trim(name, ":") + ":"
}

// emojiAlt is vbPY's Emoji with no image URL (a custom emoji the store
// doesn't have): renderEmojiFallback still draws the <img>, with no src
// and alt ":name:", and Chrome paints the alt text from the top left of
// the size x size box (c-emoji__small, overflow hidden), clipped. A
// ":tada_custom:" reaction reads "Reacted :ta".
func (v *Activity) emojiAlt(gtx C, alt string, size unit.Dp) D {
	sz := gtx.Dp(size)
	lg := gtx
	lg.Constraints = layout.Constraints{Max: image.Pt(1<<20, 1<<20)}
	m := op.Record(gtx.Ops)
	v.label(lg, 13, wReg, v.p.Muted, alt)
	call := m.Stop()
	defer clip.Rect{Max: image.Pt(sz, sz)}.Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
	return D{Size: image.Pt(sz, sz)}
}

// actBody is ActivityRowContentDetails' message: the subtext ("You: ")
// then the message clipped to lines (the item's own cap when it has one),
// or the text Slack draws instead of a message, or Slack's loading
// shimmer while the message isn't known.
func (v *Activity) actBody(gtx C, r *actRow, lines int, col color.NRGBA) D {
	d := r.cur
	if d.lines > 0 {
		lines = d.lines
	}
	if lb := d.list; lb != nil {
		// ListContent: the sentence around ListItemName (its list-item icon
		// before the title), one line
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D { return v.label(gtx, 15, wReg, col, lb.pre) }),
			layout.Rigid(func(gtx C) D {
				if !lb.icon {
					return D{}
				}
				return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D { return v.icon(gtx, icons.Slack("list-item"), 16, col) })
			}),
			layout.Flexed(1, func(gtx C) D {
				gtx.Constraints.Min.X = 0
				return v.para(gtx, 15, 22, wReg, col, 1, lb.item+lb.post)
			}),
		)
	}
	if d.invite {
		if d.body == "" {
			return D{}
		}
		return v.para(gtx, 15, 22, wReg, col, lines, d.body)
	}
	if r.msg() == nil {
		if r.it.Channel == "" {
			return D{}
		}
		return v.shimmerLine(gtx, 240)
	}
	if d.subReact {
		pre := []layout.FlexChild{layout.Rigid(func(gtx C) D { return v.label(gtx, 15, wReg, col, "Reacted ") })}
		if !v.dense {
			pre = append(pre, layout.Rigid(func(gtx C) D {
				if d.footEmoji != "" {
					return v.emojiImg(gtx, d.footEmoji, 16)
				}
				return v.emojiAlt(gtx, d.footAlt, 16)
			}), layout.Rigid(func(gtx C) D { return v.label(gtx, 15, wReg, col, " ") }))
		}
		pre = append(pre,
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D { return v.label(gtx, 15, wReg, col, "to:") })
			}),
			layout.Flexed(1, func(gtx C) D { return r.preview(gtx, lines) }))
		return row(gtx, layout.Middle, pre...)
	}
	if d.subtext == "" {
		return r.preview(gtx, lines)
	}
	return row(gtx, layout.Middle,
		layout.Rigid(func(gtx C) D { return v.para(gtx, 15, 22, wReg, col, 1, d.subtext) }),
		layout.Flexed(1, func(gtx C) D { return r.preview(gtx, lines) }),
	)
}

// shimmerLine is Slack's loading placeholder (applyShimmer) for a body
// whose message hasn't loaded: one rounded bar.
func (v *Activity) shimmerLine(gtx C, width unit.Dp) D {
	w := min(gtx.Constraints.Max.X, gtx.Dp(width))
	h := gtx.Dp(22)
	bar := image.Rect(0, gtx.Dp(5), w, h-gtx.Dp(5))
	rrect(gtx, bar, gtx.Dp(4), v.p.Skel)
	return D{Size: image.Pt(w, h)}
}

// actStatus is the right column: the unread count pill, or the gray
// "done" check for read items.
func (v *Activity) actStatus(gtx C, count int, unread bool) D {
	if unread {
		return v.countBadge(gtx, max(1, count))
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

// hoverBar is the row's floating action bar (ActivityRowContentActions):
// at the top-right, straddling the row's top border; 32x32 buttons with
// 18px icons, 4px padding, as many as the item has (hoverButtons).
func (v *Activity) hoverBar(gtx C, r *actRow, unread bool, rowW, y int) {
	btns := v.hoverButtons(r.it, unread)
	if len(btns) == 0 {
		return
	}
	bw, bh := gtx.Dp(8)+len(btns)*gtx.Dp(32), gtx.Dp(40)
	if len(btns) > 0 && btns[0] == actBtnComplete {
		bw += gtx.Dp(12) // markCompleteIconButton: margin-right 12px
	}
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
		func() {
			defer op.Offset(image.Pt(gtx.Dp(4), gtx.Dp(4))).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints.Min = image.Point{}
			ch := make([]layout.FlexChild, 0, len(btns))
			for _, b := range btns {
				b := b
				icon, _ := actBtnIcon(b)
				ch = append(ch, layout.Rigid(func(gtx C) D {
					switch b {
					case actBtnComplete:
						// markCompleteIconButton: base-inv-hgl-2 under
						// content-inv-pry, then 12px and the bar's 2px rule
						bg, fg := rgb(0x007a5a), rgb(0xffffff)
						if v.th.Dark {
							bg, fg = rgb(0x177e56), rgb(0xf8f8f8)
						}
						s := gtx.Dp(32)
						rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(8), bg)
						d := v.iconButton(gtx, &r.btns[b], icons.Slack(icon), 32, 18, fg)
						rrect(gtx, image.Rect(s+gtx.Dp(5), (s-gtx.Dp(22))/2, s+gtx.Dp(7), (s+gtx.Dp(22))/2), 0, v.p.Border)
						return D{Size: image.Pt(d.Size.X+gtx.Dp(12), d.Size.Y)}
					case actBtnReschedule:
						if _, loading, _ := v.reminderSaved(r.it); loading {
							return v.iconButton(gtx, &r.btns[b], icons.Slack(icon), 32, 18, v.p.Muted) // isDisabled
						}
					}
					return v.iconButton(gtx, &r.btns[b], icons.Slack(icon), 32, 18, v.p.Text)
				}))
			}
			row(g, layout.Middle, ch...)
		}()
		return D{Size: rect.Max}
	})
}

// pointerAt is where the pointer is, for menus opened from a press.
func (v *Activity) pointerAt() image.Point {
	if pp, ok := v.h.(interface{ PointerPos() image.Point }); ok {
		return pp.PointerPos()
	}
	return image.Point{}
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
			if !d.pill {
				return D{Size: image.Pt(0, gtx.Dp(8))}
			}
			return layout.Inset{Top: 8, Bottom: 4, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D { return v.actFooter(gtx, d, false) })
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
	case actMenuSelect:
		y := gtx.Dp(actHeadH+actBarTop+actBarH) + gtx.Dp(4)
		v.popover(gtx, image.Pt(gtx.Dp(16), y), 220, 8, v.selectMenu)
	case actMenuSettings:
		// right-aligned under the 28px kebab (header row: 14px down, 16px in)
		w := gtx.Dp(300)
		v.popover(gtx, image.Pt(gtx.Constraints.Max.X-gtx.Dp(16)-w, gtx.Dp(14+28+4)), 300, 8, v.settingsMenu)
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
				return v.menuRow(gtx, &v.extraC[i], &actExtras[i], i == 0 && v.vip || i == 1 && v.showCleared)
			})
		}),
		// ActivityFilterActionButtons (activity_views.go)
		layout.Rigid(v.filterActions),
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
		left := unit.Dp(16)
		if f.nocheck {
			left = 12 // p-activity_ia4_page__filter-menu__icon
		}
		layout.Inset{Left: left, Right: 16}.Layout(g, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D {
					if f.nocheck {
						return D{}
					}
					return layout.Inset{Right: 12}.Layout(gtx, func(gtx C) D { return v.actCheck(gtx, 13, on) })
				}),
				layout.Rigid(func(gtx C) D { return v.icon(gtx, f.icon, 20, ic) }),
				hgap(8),
				layout.Flexed(1, func(gtx C) D { return v.label(gtx, 15, wReg, fg, f.label) }),
				layout.Rigid(func(gtx C) D {
					if f.trail == "" {
						return D{}
					}
					// "Toggle mention sub-filters": a 24px button, 20px caret
					return layout.UniformInset(2).Layout(gtx, func(gtx C) D { return v.icon(gtx, f.trail, 20, ic) })
				}),
			)
		})
		return D{Size: image.Pt(w, h)}
	})
}

// selectMenu is the "Select options" menu.
func (v *Activity) selectMenu(gtx C) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	labels := [4]string{"Select all", "Select reads", "Select unreads", "Custom select"}
	return actStack(gtx, len(labels), func(gtx C, i int) D {
		return v.menuItem(gtx, &v.selC[i], "", labels[i], false, false)
	})
}

// actSettings are ActivitySettingsMenu's "What to show in Activity"
// rows: DMs and group DMs (the user pref activity_dms_opt_out, inverted),
// Channels set to “All new posts” and Reminders from to-dos (the global
// notification prefs global_channels_in_activity and
// global_reminders_in_activity, default on), then "See all notification
// settings".
func (v *Activity) settingState() (dms, chans, rem bool) {
	c := v.h.Core()
	return !c.PrefBool("activity_dms_opt_out", false), c.GlobalNotifyBool("global_channels_in_activity", false),
		c.GlobalNotifyBool("global_reminders_in_activity", true)
}

func (v *Activity) settingsClicks(gtx C) {
	v.settingsSectionClicks(gtx) // Sort and Density (activity_views.go)
	dms, chans, rem := v.settingState()
	c := v.h.Core()
	set := func(f func(ctx context.Context) error) {
		v.menu = actMenuNone
		v.write("Couldn’t save the setting: ", func(ctx context.Context, _ *core.App) error { return f(ctx) }, func() {})
	}
	switch {
	case v.setC[0].Clicked(gtx):
		set(func(ctx context.Context) error { return c.SetPref(ctx, "activity_dms_opt_out", dms) })
	case v.setC[1].Clicked(gtx):
		set(func(ctx context.Context) error {
			return c.SetGlobalNotifyPref(ctx, "global_channels_in_activity", !chans)
		})
	case v.setC[2].Clicked(gtx):
		set(func(ctx context.Context) error {
			return c.SetGlobalNotifyPref(ctx, "global_reminders_in_activity", !rem)
		})
	case v.setC[3].Clicked(gtx):
		v.menu = actMenuNone
		if o, ok := v.h.(interface{ OpenPreferences(section string) }); ok {
			o.OpenPreferences("notifications")
		}
	}
}

func (v *Activity) settingsMenu(gtx C) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	dms, chans, rem := v.settingState()
	check := func(i int, label string, on bool) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			f := actFilter{label: label, nocheck: false}
			return v.settingRow(gtx, &v.setC[i], &f, on)
		})
	}
	// vzfm: [Sort by …] [Density …] (activity_in_home_prototype), then
	// What to show in Activity
	rows := v.settingsSections()
	rows = append(rows,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 4, Bottom: 4, Left: 16, Right: 16}.Layout(gtx, func(gtx C) D {
				return v.label(gtx, 13, wBold, v.p.Muted, "What to show in Activity")
			})
		}),
		check(0, "DMs and group DMs", dms),
		check(1, "Channels set to “All new posts”", chans),
		check(2, "Reminders from to-dos", rem),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 8, Bottom: 8}.Layout(gtx, func(gtx C) D { return hline(gtx, v.p.Border) })
		}),
		layout.Rigid(func(gtx C) D { return v.menuItem(gtx, &v.setC[3], "", "See all notification settings", false, false) }),
	)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
}

// settingRow is a menuitemcheckbox: a 28px row with the checkbox and the
// 15px label (no icon).
func (v *Activity) settingRow(gtx C, c *widget.Clickable, f *actFilter, on bool) D {
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
				layout.Rigid(func(gtx C) D { return v.actCheck(gtx, 13, on) }),
				hgap(12),
				layout.Flexed(1, func(gtx C) D { return v.label(gtx, 15, wReg, v.p.Text, f.label) }),
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
							return layout.Inset{Top: 4}.Layout(gtx, func(gtx C) D { return b.actReactions(gtx, m) })
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

// actReactions lays msg's reaction pills out left to right, wrapping
// (each at Slack's Reaction boundary: reactions_slack.go).
func (b *base) actReactions(gtx C, msg *core.Message) D {
	rs := msg.Reactions
	maxW := gtx.Constraints.Max.X
	gap := gtx.Dp(4)
	x, y, lineH := 0, 0, 0
	for i := range rs {
		m := op.Record(gtx.Ops)
		cg := gtx
		cg.Constraints.Min = image.Point{}
		d := b.actReaction(cg, msg, &rs[i])
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
	_, fg := b.actPillColors(r)
	b.actPillBox(gtx, r, image.Pt(w, h))
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

// actPillColors are a pill's fill and text: blue when the viewer reacted.
func (b *base) actPillColors(r *core.Reaction) (bg, fg color.NRGBA) {
	if r.Self {
		return argb(0x1a1d9bd1), b.p.Link
	}
	return b.p.Chip, b.p.Muted
}

// actPillBox draws a pill's rounded fill (and the reacted border) at size.
func (b *base) actPillBox(gtx C, r *core.Reaction, size image.Point) {
	rect := image.Rectangle{Max: size}
	bg, _ := b.actPillColors(r)
	rrect(gtx, rect, size.Y/2, bg)
	if r.Self {
		border(gtx, rect, size.Y/2, max(1, gtx.Dp(1)), rgb(0x1d9bd1))
	}
}

// actSpinner is Slack's loading ring (a 3/4 arc turning once a second) in
// a d-pixel box; it keeps frames coming while drawn.
func actSpinner(gtx C, d int, c color.NRGBA) D {
	t := float64(gtx.Now.UnixMilli()%1000) / 1000
	r := float32(d)/2 - float32(gtx.Dp(2))/2
	ctr := f32.Pt(float32(d)/2, float32(d)/2)
	a0 := float32(t * 2 * math.Pi)
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(f32.Pt(ctr.X+r*float32(math.Cos(float64(a0))), ctr.Y+r*float32(math.Sin(float64(a0)))))
	p.ArcTo(ctr, ctr, 1.5*math.Pi)
	st := clip.Stroke{Path: p.End(), Width: float32(gtx.Dp(2))}.Op().Push(gtx.Ops)
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	st.Pop()
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(33 * time.Millisecond)})
	return D{Size: image.Pt(d, d)}
}

// ensureLists loads the lists list items point at (ListContent and the
// pill read the list's record and column names), then redraws those rows.
func (v *Activity) ensureLists(page *core.ActivityPage) {
	c := v.h.Core()
	for i := range page.Items {
		id := page.Items[i].ListID
		if id == "" || v.listsReq[id] || c.CachedList(id) != nil {
			continue
		}
		if v.listsReq == nil {
			v.listsReq = map[string]bool{}
		}
		v.listsReq[id] = true
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if _, err := c.List(ctx, id); err != nil {
				return
			}
			v.notes.mu.Lock()
			v.notes.fns = append(v.notes.fns, func() {
				pg, _, _ := v.data.get()
				for j := range pg.Items {
					if pg.Items[j].ListID == id {
						v.rowFor(actKey(&pg.Items[j])).built = false
					}
				}
				v.dirty = true
			})
			v.notes.mu.Unlock()
			v.h.Invalidate()
		}()
	}
}

// loadDensity reads the saved views once (fetchActivityViews: the views,
// their densities and the global prefs, activity_views.go loadViews) and
// applies the layout unless the toggle was used meanwhile: with
// activity_global_settings the prefs' density (formatFilterViewState lays
// it over every view), else the view's own.
func (v *Activity) loadDensity() {
	if v.densityReq {
		return
	}
	v.densityReq = true
	v.loadViews()
}

// setDense is the layout toggle (ActivityFilterLayoutToggle in the
// header): the page switches at once and Slack's global prefs are
// written (core SetActivityDensity: activity.prefs.update {density}); a
// failed write puts the old layout back.
func (v *Activity) setDense(dense bool) {
	was := v.dense
	v.dense, v.densitySet, v.dirty = dense, true, true
	if !v.globalSettings() {
		v.setViewDense(dense, was)
		return
	}
	v.write("Couldn’t save the layout: ", func(ctx context.Context, c *core.App) error {
		_, err := c.SetActivityDensity(ctx, dense)
		return err
	}, func() { v.dense, v.dirty = was, true })
}

// globalSettings is activity_global_settings (5Ns/ XW): one layout for
// every view, its toggle in the page header. Off, each saved view has its
// own and the toggle sits in the filter bar.
func (v *Activity) globalSettings() bool {
	return v.h.Core().Experiment("activity_global_settings") == "on"
}

// setViewDense is updateActivityViewsByApi {density, isAutoSave} from the
// filter bar's toggle: the All view only changes the page; any other
// saved view (a custom one too) writes activity.views.update {view_id,
// sort, density}.
func (v *Activity) setViewDense(dense, was bool) {
	slot := v.slot()
	vw := v.slotView(slot)
	if slot < actSlotN {
		v.viewDense[slot] = dense
	} else {
		v.customs[slot-actSlotN].Density = map[bool]string{true: "dense", false: "compact"}[dense]
	}
	if slot == 0 || vw.ID == "" {
		return
	}
	v.write("Couldn’t save the layout: ", func(ctx context.Context, c *core.App) error {
		return c.UpdateActivityView(ctx, vw, dense)
	}, func() {
		if slot < actSlotN {
			v.viewDense[slot] = was
		} else if k := slot - actSlotN; k < len(v.customs) && v.customs[k].ID == vw.ID {
			v.customs[k].Density = map[bool]string{true: "dense", false: "compact"}[was]
		}
		if v.slot() == slot {
			v.dense, v.dirty = was, true
		}
	})
}

// actTime is the rows' 54px timestamp column (white-space: nowrap, no
// ellipsis): a time that fits sits at its start, or its end when end is
// set (Dense: text-align end); a longer one ("Nov 14, 2023") overflows to
// the right as Chrome draws it.
func (v *Activity) actTime(gtx C, when string, end bool) D {
	w := gtx.Dp(54)
	lg := gtx
	lg.Constraints = layout.Constraints{Max: image.Pt(1<<20, gtx.Dp(18))}
	m := op.Record(gtx.Ops)
	d := v.label(lg, 13, wReg, v.p.Muted, when)
	call := m.Stop()
	x := 0
	if end && d.Size.X < w {
		x = w - d.Size.X
	}
	func() {
		defer op.Offset(image.Pt(x, 0)).Push(gtx.Ops).Pop()
		call.Add(gtx.Ops)
	}()
	return D{Size: image.Pt(w, max(d.Size.Y, gtx.Dp(18)))}
}
