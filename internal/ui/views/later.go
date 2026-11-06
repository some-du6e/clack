package views

// Later (To-dos): saved messages in three underline tabs, In progress /
// Archived / Completed (saved.list filters saved, archived, completed).
// Rows show the conversation and due chip, the author line and a 3-line
// body; hovering reveals Complete, Remind, Archive and More. Writes run
// only when the user clicks one of those.
//
// Also home to the hover-row helpers shared by DMs, Later and Drafts
// (dlHoverRow, dlActionBar).

import (
	"context"
	"image"
	"image/color"
	"sync"
	"time"

	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Labels (the rail calls the view "Later"; see /tmp/ui-views-spec/later.md).
const (
	laterTitle = "Later"
)

var (
	laterTabLabels  = []string{"In progress", "Archived", "Completed"}
	laterTabFilters = [3]string{"saved", "archived", "completed"}
)

const (
	laterAvatar unit.Dp = 36
	laterAvGap  unit.Dp = 8
)

// laterPreset is a "Remind me" choice.
type laterPreset struct {
	label string
	at    func(now time.Time) time.Time
}

var laterPresets = [5]laterPreset{
	{"In 20 minutes", func(n time.Time) time.Time { return n.Add(20 * time.Minute) }},
	{"In 1 hour", func(n time.Time) time.Time { return n.Add(time.Hour) }},
	{"In 3 hours", func(n time.Time) time.Time { return n.Add(3 * time.Hour) }},
	{"Tomorrow", func(n time.Time) time.Time {
		y, m, d := n.AddDate(0, 0, 1).Date()
		return time.Date(y, m, d, 9, 0, 0, 0, n.Location())
	}},
	{"Next week", func(n time.Time) time.Time {
		days := (8 - int(n.Weekday())) % 7 // to next Monday
		if days == 0 {
			days = 7
		}
		y, m, d := n.AddDate(0, 0, days).Date()
		return time.Date(y, m, d, 9, 0, 0, 0, n.Location())
	}},
}

type laterResult struct {
	page   core.SavedPage
	seq    int
	cursor string
}

type laterTab struct {
	filter      string
	first, more async[laterResult]

	firstApplied, moreApplied int
	moreTried                 string

	items []core.SavedItem
	convs map[string]core.ConvRef
	next  string
	pages int
	list  widget.List
}

type laterRow struct {
	dlRowState
	done, remind, archive, restore, more, clearDue widget.Clickable
	presets                                        [len(laterPresets)]widget.Clickable
}

// laterDone is a finished write, reported back to the UI goroutine.
type laterDone struct {
	key, msg string
	tab      int
	err      error
}

// Later is the saved-items view.
type Later struct {
	base

	tabs  *tabs
	t     [3]laterTab
	rows  map[string]*laterRow
	retry widget.Clickable
	seq   int
	last  time.Time
	gone  map[string]time.Time // optimistic removals, hidden from reloads for a while

	w     watcher
	mu    sync.Mutex
	done  []laterDone
	known map[string]bool // "channel/ts" of listed items (mu)
}

// NewLater builds the Later view.
func NewLater(h Host) *Later {
	l := &Later{
		base:  newBase(h),
		tabs:  newTabs(laterTabLabels...),
		rows:  map[string]*laterRow{},
		gone:  map[string]time.Time{},
		known: map[string]bool{},
	}
	for i := range l.t {
		l.t[i].filter = laterTabFilters[i]
		l.t[i].convs = map[string]core.ConvRef{}
		l.t[i].list.Axis = layout.Vertical
	}
	return l
}

func laterKey(it *core.SavedItem) string { return it.ItemID + "/" + it.TS }

// Layout draws the header, tabs and the selected tab's list.
func (l *Later) Layout(gtx C) D {
	l.frame()
	l.update(gtx)
	size := gtx.Constraints.Max
	gtx.Constraints = layout.Exact(size)
	fill(gtx, image.Rectangle{Max: size}, l.p.Bg)
	return col(gtx,
		layout.Rigid(func(gtx C) D { return l.header(gtx, laterTitle) }),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 16, Right: 16}.Layout(gtx, func(gtx C) D { return l.tabStrip(gtx, l.tabs, nil) })
		}),
		layout.Rigid(func(gtx C) D { return hline(gtx, l.p.Border) }),
		layout.Flexed(1, l.layoutList),
	)
}

// ---- state ----

func (l *Later) update(gtx C) {
	sel := l.tabs.Sel
	switch {
	case !l.t[sel].first.started():
		l.reload(sel)
		l.w.watch(l.h, l.match)
	case !l.last.IsZero() && gtx.Now.Sub(l.last) > 10*time.Second:
		l.reload(sel) // shown again after a while
	}
	l.last = gtx.Now
	if l.tabs.update(gtx) {
		sel = l.tabs.Sel
		l.reload(sel)
	}
	if l.w.due(gtx, 2*time.Second) {
		l.reload(sel)
	}
	for i := range l.t {
		l.apply(i)
	}
	l.drainDone()
	if l.retry.Clicked(gtx) {
		l.reload(sel)
	}
	for k, r := range l.rows {
		l.rowActions(gtx, sel, k, r)
	}
}

func (l *Later) rowActions(gtx C, tab int, k string, r *laterRow) {
	click := r.click.Clicked(gtx)
	done := r.done.Clicked(gtx)
	archive := r.archive.Clicked(gtx)
	restore := r.restore.Clicked(gtx)
	remind := r.remind.Clicked(gtx)
	more := r.more.Clicked(gtx)
	clearDue := r.clearDue.Clicked(gtx)
	preset := -1
	for i := range r.presets {
		if r.presets[i].Clicked(gtx) {
			preset = i
		}
	}
	if !click && !done && !archive && !restore && !remind && !more && !clearDue && preset < 0 {
		return
	}
	idx := l.indexOf(tab, k)
	if idx < 0 {
		return
	}
	it := l.t[tab].items[idx]
	switch {
	case click:
		l.open(&it)
	case done:
		l.mark(tab, it, core.SavedUpdate{Completed: laterBool(true)}, true, "Marked as complete")
	case archive:
		l.mark(tab, it, core.SavedUpdate{Archived: laterBool(true)}, true, "Archived")
	case restore:
		u := core.SavedUpdate{Completed: laterBool(false)}
		if tab == 1 {
			u = core.SavedUpdate{Archived: laterBool(false)}
		}
		l.mark(tab, it, u, true, "Moved to In progress")
	case remind:
		r.pin = !r.pin
	case more:
		if it.Msg != nil {
			l.h.OpenMessageMenu(image.Point{}, *it.Msg)
		}
	case clearDue:
		var zero int64
		l.t[tab].items[idx].DateDue = 0
		r.pin = false
		l.mark(tab, it, core.SavedUpdate{DateDue: &zero}, false, "Reminder removed")
	case preset >= 0:
		at := laterPresets[preset].at(time.Now())
		ts := at.Unix()
		l.t[tab].items[idx].DateDue = ts
		r.pin = false
		l.mark(tab, it, core.SavedUpdate{DateDue: &ts}, false, "Reminder set for "+laterWhen(at))
	}
}

func laterBool(v bool) *bool { return &v }

// laterWhen formats a reminder time for the confirmation toast.
func laterWhen(t time.Time) string {
	now := time.Now()
	switch {
	case sameDay(t, now):
		return "today at " + clock(t)
	case sameDay(t, now.AddDate(0, 0, 1)):
		return "tomorrow at " + clock(t)
	}
	return t.Format("Monday, January 2") + " at " + clock(t)
}

// open jumps to the saved message: the thread for replies, else the
// conversation.
func (l *Later) open(it *core.SavedItem) {
	if it.Channel == "" {
		return
	}
	if m := it.Msg; m != nil && m.ThreadTS != "" && m.ThreadTS != m.TS {
		l.h.OpenThread(it.Channel, m.ThreadTS)
		return
	}
	l.h.Navigate("c/" + it.Channel)
}

// mark runs saved.update off the UI goroutine. remove hides the row
// optimistically; a failure brings it back on the next reload.
func (l *Later) mark(tab int, it core.SavedItem, u core.SavedUpdate, remove bool, okMsg string) {
	if it.Channel == "" || it.TS == "" {
		return
	}
	k := laterKey(&it)
	if remove {
		l.gone[k] = time.Now()
		if i := l.indexOf(tab, k); i >= 0 {
			items := l.t[tab].items
			l.t[tab].items = append(items[:i:i], items[i+1:]...)
		}
		if r, ok := l.rows[k]; ok {
			r.pin = false
		}
	}
	c, h := l.h.Core(), l.h
	ch, ts := it.Channel, it.TS
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		err := c.UpdateSaved(ctx, ch, ts, u)
		l.mu.Lock()
		l.done = append(l.done, laterDone{key: k, msg: okMsg, tab: tab, err: err})
		l.mu.Unlock()
		h.Invalidate()
	}()
}

func (l *Later) drainDone() {
	l.mu.Lock()
	ds := l.done
	l.done = nil
	l.mu.Unlock()
	for _, d := range ds {
		if d.err != nil {
			delete(l.gone, d.key)
			l.h.Toast("Couldn’t update that item: " + friendlyErr(d.err))
			l.reload(d.tab)
			continue
		}
		if d.msg != "" {
			l.h.Toast(d.msg)
		}
	}
}

func (l *Later) indexOf(tab int, k string) int {
	for i := range l.t[tab].items {
		if laterKey(&l.t[tab].items[i]) == k {
			return i
		}
	}
	return -1
}

func (l *Later) reload(i int) {
	t := &l.t[i]
	l.seq++
	seq, f := l.seq, t.filter
	c := l.h.Core()
	t.first.load(l.h, true, func(ctx context.Context) (laterResult, error) {
		p, err := c.Saved(ctx, f, "")
		return laterResult{page: p, seq: seq}, err
	})
}

func (l *Later) loadMore(i int) {
	t := &l.t[i]
	if t.next == "" || t.next == t.moreTried || t.more.busy() || t.first.busy() {
		return
	}
	l.seq++
	seq, f, cur := l.seq, t.filter, t.next
	t.moreTried = cur
	c := l.h.Core()
	t.more.load(l.h, true, func(ctx context.Context) (laterResult, error) {
		p, err := c.Saved(ctx, f, cur)
		return laterResult{page: p, seq: seq, cursor: cur}, err
	})
}

func (l *Later) apply(i int) {
	t := &l.t[i]
	changed := false
	if v, ok, _ := t.first.get(); ok && v.seq != 0 && v.seq != t.firstApplied {
		t.firstApplied = v.seq
		seen := map[string]bool{}
		var out []core.SavedItem
		for _, it := range v.page.Items {
			k := laterKey(&it)
			if seen[k] || l.hidden(k) {
				continue
			}
			seen[k] = true
			out = append(out, it)
		}
		if t.pages > 1 {
			for _, it := range t.items {
				if k := laterKey(&it); !seen[k] && !l.hidden(k) {
					seen[k] = true
					out = append(out, it)
				}
			}
		} else {
			t.next, t.pages, t.moreTried = v.page.Next, 1, ""
		}
		t.items = out
		for id, r := range v.page.Convs {
			t.convs[id] = r
		}
		changed = true
	}
	if v, ok, _ := t.more.get(); ok && v.seq != 0 && v.seq != t.moreApplied {
		t.moreApplied = v.seq
		if v.cursor == t.next {
			have := map[string]bool{}
			for j := range t.items {
				have[laterKey(&t.items[j])] = true
			}
			for _, it := range v.page.Items {
				if k := laterKey(&it); !have[k] && !l.hidden(k) {
					have[k] = true
					t.items = append(t.items, it)
				}
			}
			for id, r := range v.page.Convs {
				t.convs[id] = r
			}
			t.next = v.page.Next
			t.pages++
			changed = true
		}
	}
	if changed {
		l.mu.Lock()
		clear(l.known)
		for j := range l.t {
			for k := range l.t[j].items {
				it := &l.t[j].items[k]
				l.known[it.Channel+"/"+it.TS] = true
			}
		}
		l.mu.Unlock()
	}
}

// hidden reports an item removed optimistically in the last 15 seconds
// (saved.list can lag behind saved.update).
func (l *Later) hidden(k string) bool {
	at, ok := l.gone[k]
	if !ok {
		return false
	}
	if time.Since(at) > 15*time.Second {
		delete(l.gone, k)
		return false
	}
	return true
}

// match runs on the event goroutine.
func (l *Later) match(ev core.Event) bool {
	switch ev.Type {
	case "resync":
		return true
	case "update", "delete":
		l.mu.Lock()
		k := l.known[ev.Channel+"/"+ev.TS]
		l.mu.Unlock()
		return k
	}
	return false
}

func (l *Later) rowState(k string) *laterRow {
	r, ok := l.rows[k]
	if !ok {
		r = new(laterRow)
		l.rows[k] = r
	}
	return r
}

// ---- list ----

func (l *Later) layoutList(gtx C) D {
	sel := l.tabs.Sel
	t := &l.t[sel]
	_, loaded, err := t.first.get()
	switch {
	case len(t.items) == 0 && !loaded:
		return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D { return l.skeleton(gtx, 6, laterAvatar) })
	case len(t.items) == 0 && err != nil:
		return l.errorState(gtx, err, &l.retry)
	case len(t.items) == 0:
		return l.emptyFor(gtx, sel)
	}
	n := len(t.items)
	extra := 0
	if t.more.busy() {
		extra = 1
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	dims := l.scrollList(gtx, &t.list, n+extra, func(gtx C, i int) D {
		if i >= n {
			return l.skeleton(gtx, 1, laterAvatar)
		}
		return l.itemRow(gtx, sel, &t.items[i], t.convs)
	})
	if p := t.list.Position; p.First+p.Count >= n-3 {
		l.loadMore(sel)
	}
	return dims
}

func (l *Later) emptyFor(gtx C, tab int) D {
	switch tab {
	case 1:
		return l.emptyState(gtx, "download", "Nothing archived",
			"Archive items you’re done thinking about. They’ll stay here so you can find them later.", nil, "")
	case 2:
		return l.emptyState(gtx, "check-circle", "Nothing completed yet",
			"When you mark an item complete, it moves here.", nil, "")
	}
	return l.emptyState(gtx, "later", "Save messages for later",
		"Save any message to keep track of it here, and set a reminder so nothing slips through the cracks.", nil, "")
}

func (l *Later) itemRow(gtx C, tab int, it *core.SavedItem, convs map[string]core.ConvRef) D {
	k := laterKey(it)
	r := l.rowState(k)
	var bar, below W
	if it.Channel != "" {
		bar = func(gtx C) D { return l.actionBar(gtx, tab, it, r) }
	}
	if r.pin && tab == 0 {
		below = func(gtx C) D { return l.remindMenu(gtx, it, r) }
	}
	return l.dlHoverRow(gtx, &r.dlRowState, dlRowOpts{
		HoverBg: l.p.Hover,
		Rule:    l.p.Border,
		Content: func(gtx C) D {
			return layout.Inset{Top: 12, Right: 20, Bottom: 12, Left: 16}.Layout(gtx, func(gtx C) D {
				return l.itemBody(gtx, tab, it, convs)
			})
		},
		Below:    below,
		Bar:      bar,
		BarTop:   8,
		BarRight: 16,
	})
}

func (l *Later) actionBar(gtx C, tab int, it *core.SavedItem, r *laterRow) D {
	ib := func(c *widget.Clickable, icon string) layout.FlexChild {
		return layout.Rigid(func(gtx C) D { return l.iconButton(gtx, c, icon, 32, 18, l.p.Text) })
	}
	var btns []layout.FlexChild
	if tab == 0 {
		btns = append(btns, ib(&r.done, "check"), ib(&r.remind, "remind"), ib(&r.archive, "download"))
	} else {
		btns = append(btns, layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 2, Right: 4, Top: 2, Bottom: 2}.Layout(gtx, func(gtx C) D {
				return l.button(gtx, &r.restore, btnGhost, true, "Move to In progress")
			})
		}))
	}
	if it.Msg != nil {
		btns = append(btns, ib(&r.more, "more"))
	}
	return l.dlActionBar(gtx, btns...)
}

// remindMenu is the inline "Remind me" preset row under an item.
func (l *Later) remindMenu(gtx C, it *core.SavedItem, r *laterRow) D {
	left := laterAvatar + laterAvGap + 16
	return layout.Inset{Left: left, Right: 20, Bottom: 12}.Layout(gtx, func(gtx C) D {
		ch := []layout.FlexChild{
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Right: 8}.Layout(gtx, func(gtx C) D { return l.label(gtx, 13, wBold, l.p.Muted, "Remind me:") })
			}),
		}
		for i := range laterPresets {
			i := i
			ch = append(ch, layout.Rigid(func(gtx C) D {
				return layout.Inset{Right: 8}.Layout(gtx, func(gtx C) D {
					return l.button(gtx, &r.presets[i], btnOutline, true, laterPresets[i].label)
				})
			}))
		}
		if it.DateDue > 0 {
			ch = append(ch, layout.Rigid(func(gtx C) D {
				return l.button(gtx, &r.clearDue, btnGhost, true, "Remove reminder")
			}))
		}
		return row(gtx, layout.Middle, ch...)
	})
}

func (l *Later) itemBody(gtx C, tab int, it *core.SavedItem, convs map[string]core.ConvRef) D {
	return col(gtx,
		layout.Rigid(func(gtx C) D { return l.contextLine(gtx, tab, it, convs) }),
		vgap(8),
		layout.Rigid(func(gtx C) D {
			m := it.Msg
			if m == nil {
				return layout.Inset{Left: laterAvatar + laterAvGap}.Layout(gtx, func(gtx C) D {
					msg := "This message is no longer available."
					if it.ItemType != "" && it.ItemType != "message" {
						msg = "Saved " + it.ItemType
					}
					return l.para(gtx, 15, 22, wReg, l.p.Muted, 1, msg)
				})
			}
			name, av := l.authorOf(m)
			return row(gtx, layout.Start,
				layout.Rigid(func(gtx C) D { return l.avatar(gtx, av, laterAvatar) }),
				hgap(laterAvGap),
				layout.Flexed(1, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return col(gtx,
						layout.Rigid(func(gtx C) D {
							return row(gtx, layout.Baseline,
								layout.Rigid(func(gtx C) D { return l.para(gtx, 15, 22, wBlack, l.p.Text, 1, name) }),
								hgap(8),
								layout.Rigid(func(gtx C) D { return l.label(gtx, 13, wReg, l.p.Muted, listTime(tsTime(m.TS))) }),
							)
						}),
						layout.Rigid(func(gtx C) D { return l.msgBody(gtx, m, false, 3) }),
					)
				}),
			)
		}),
	)
}

// contextLine: the conversation (13px muted, with its icon) and the due or
// completed chip.
func (l *Later) contextLine(gtx C, tab int, it *core.SavedItem, convs map[string]core.ConvRef) D {
	title, icon := "", "hash"
	if it.Channel != "" {
		title = l.convTitle(it.Channel, convs)
		kind := ""
		if r, ok := convs[it.Channel]; ok {
			kind = r.Kind
		} else if c, ok := l.h.Core().Conv(it.Channel); ok {
			kind = c.Kind
		}
		switch kind {
		case "private":
			icon = "lock"
		case "im", "mpim":
			icon = "dms"
		}
		if m := it.Msg; m != nil && m.ThreadTS != "" && m.ThreadTS != m.TS {
			icon, title = "threads", "Thread in "+title
		}
	}
	return row(gtx, layout.Middle,
		layout.Rigid(func(gtx C) D {
			if title == "" {
				return D{}
			}
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D { return l.icon(gtx, icon, 14, l.p.Muted) }),
				hgap(4),
				layout.Rigid(func(gtx C) D { return l.label(gtx, 13, wReg, l.p.Muted, title) }),
			)
		}),
		layout.Rigid(func(gtx C) D {
			var txt string
			over := false
			switch {
			case tab == 2 && it.DateCompleted > 0:
				txt = "Completed " + listTime(unixTime(it.DateCompleted))
			case tab == 0 && it.DateDue > 0:
				txt, over = dueText(unixTime(it.DateDue))
			}
			if txt == "" {
				return D{}
			}
			return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return l.dueChip(gtx, txt, over) })
		}),
	)
}

// dueChip: 20px pill, radius 10, 12px clock + 12px bold text; red tint
// when overdue.
func (l *Later) dueChip(gtx C, txt string, overdue bool) D {
	fg, bg := l.p.Sec, l.p.Chip
	if overdue {
		fg, bg = l.p.Red, argb(0x1ae01e5a)
	}
	m := op.Record(gtx.Ops)
	d := layout.Inset{Left: 6, Right: 8, Top: 2, Bottom: 2}.Layout(gtx, func(gtx C) D {
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D { return l.icon(gtx, "clock", 12, fg) }),
			hgap(4),
			layout.Rigid(func(gtx C) D { return l.label(gtx, 12, wBold, fg, txt) }),
		)
	})
	call := m.Stop()
	rrect(gtx, image.Rectangle{Max: d.Size}, d.Size.Y/2, bg)
	call.Add(gtx.Ops)
	return d
}

// ---- shared hover rows (DMs, Later, Drafts) ----

// dlRowState is the persistent state of a hover row.
type dlRowState struct {
	click   widget.Clickable
	hover   gesture.Hover
	hovered bool
	pin     bool // keep the action bar shown (e.g. while its menu is open)
	barTag  int  // blocks clicks in the bar's padding from reaching the row
}

// dlRowOpts describes one hover row.
type dlRowOpts struct {
	Bg, HoverBg color.NRGBA // base fill; hover overlay (skipped when equal to Bg)
	Rule        color.NRGBA // 1px bottom border (zero alpha = none)
	Content     W           // the clickable part
	Below       W           // optional non-clickable part under Content
	Bar         W           // optional floating action bar shown on hover
	BarTop      unit.Dp     // bar offset from the row's top
	BarRight    unit.Dp     // and from its right edge
}

// dlHoverRow draws a full-width row whose hover reveals a floating action
// bar. The bar is drawn above the row's clickable as a sibling, so its
// buttons don't also click the row; the hover area wraps both, so moving
// onto the bar keeps the row hovered.
func (b *base) dlHoverRow(gtx C, st *dlRowState, o dlRowOpts) D {
	st.hovered = st.hover.Update(gtx.Source)
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	gtx.Constraints.Min.Y = 0
	m := op.Record(gtx.Ops)
	d := col(gtx,
		layout.Rigid(func(gtx C) D {
			return st.click.Layout(gtx, func(gtx C) D {
				pointerCursor(gtx)
				return o.Content(gtx)
			})
		}),
		layout.Rigid(func(gtx C) D {
			if o.Below == nil {
				return D{}
			}
			return o.Below(gtx)
		}),
	)
	call := m.Stop()
	hl := 0
	if o.Rule.A > 0 {
		hl = max(1, gtx.Dp(1))
	}
	size := image.Pt(d.Size.X, d.Size.Y+hl)
	body := image.Rectangle{Max: d.Size}
	if o.Bg.A > 0 {
		fill(gtx, body, o.Bg)
	}
	if st.hovered && o.HoverBg.A > 0 && o.HoverBg != o.Bg {
		fill(gtx, body, o.HoverBg)
	}
	area := clip.Rect(image.Rectangle{Max: size}).Push(gtx.Ops)
	st.hover.Add(gtx.Ops)
	call.Add(gtx.Ops)
	if hl > 0 {
		fill(gtx, image.Rect(0, d.Size.Y, d.Size.X, size.Y), o.Rule)
	}
	if o.Bar != nil && (st.hovered || st.pin) {
		mb := op.Record(gtx.Ops)
		g := gtx
		g.Constraints.Min = image.Point{}
		bd := o.Bar(g)
		bc := mb.Stop()
		off := op.Offset(image.Pt(size.X-gtx.Dp(o.BarRight)-bd.Size.X, gtx.Dp(o.BarTop))).Push(gtx.Ops)
		blk := clip.Rect(image.Rectangle{Max: bd.Size}).Push(gtx.Ops)
		event.Op(gtx.Ops, &st.barTag)
		blk.Pop()
		bc.Add(gtx.Ops)
		off.Pop()
	}
	area.Pop()
	return D{Size: size}
}

// dlActionBar is Slack's floating row toolbar: white, radius 12, padding
// 4, a 1px #1d1c1d21 ring and a soft 0 1px 3px shadow; put 32x32 icon
// buttons (18px icons) in it.
func (b *base) dlActionBar(gtx C, btns ...layout.FlexChild) D {
	m := op.Record(gtx.Ops)
	d := layout.UniformInset(4).Layout(gtx, func(gtx C) D { return row(gtx, layout.Middle, btns...) })
	call := m.Stop()
	r := image.Rectangle{Max: d.Size}
	rad := gtx.Dp(12)
	theme.Shadow(gtx, r, rad, gtx.Dp(3), argb(0x14000000))
	rrect(gtx, r, rad, b.p.Surface)
	border(gtx, r, rad, max(1, gtx.Dp(1)), argb(0x211d1c1d))
	call.Add(gtx.Ops)
	return d
}
