package views

// DMs tab: the "Direct messages" list (newest first, "Find a DM…" filter
// with directory people results, "Unreads" toggle) in the themed sidebar
// column, and the selected conversation in the white pane on the right
// (through the Host's Panes when it offers them).
//
// Measurements: /tmp/ui-views-spec/dms.md (Slack 2026, Aubergine theme).

import (
	"context"
	"image"
	"image/color"
	"sort"
	"strings"
	"sync"
	"time"

	"gioui.org/io/event"
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

// Measurements of Slack's DMs list.
const (
	dmsPaneFrac         = 0.38 // list width: 519 of the 1366 workspace
	dmsPaneMin  unit.Dp = 300
	dmsPaneMax  unit.Dp = 640
	dmsAvatar   unit.Dp = 36
	dmsPad      unit.Dp = 16 // row padding on all sides
	dmsAvGap    unit.Dp = 8  // avatar → text
	dmsNameSize unit.Sp = 15
	dmsTimeSize unit.Sp = 13
	dmsLine     unit.Sp = 22
	dmsSearchH  unit.Dp = 28
	dmsInsetX   unit.Dp = 8 // header button and search input side inset
)

// Aubergine sidebar colors from the spec (the DM list sits on the chrome).
var (
	dmsChrome    = rgb(0x3f0e40)    // flat stand-in for the chrome gradient
	dmsTint      = argb(0x14f9edff) // hover / selected row (#f9edff@.08)
	dmsRule      = argb(0x38f9edff) // row border, input border (@.22)
	dmsInputBg   = argb(0x1cf9edff) // "Find a DM…" fill (@.11)
	dmsSoft      = argb(0xe6f6e4ff) // labels, icons, placeholder (#f6e4ff@.9)
	dmsPreview   = argb(0xe6ffffff) // read preview (white@.9)
	dmsTimeC     = argb(0xb3f6e4ff) // timestamps (#f6e4ff@.7)
	dmsWhite     = rgb(0xffffff)
	dmsGreen     = rgb(0x20a271)
	dmsPillBg    = rgb(0xeabdfb)
	dmsPillFg    = rgb(0x39063a)
	dmsSkelColor = argb(0x1af9edff)
)

type dmsResult struct {
	page   core.DMPage
	seq    int
	cursor string
}

type dmsPeople struct {
	q     string
	users []core.User
	seq   int
}

type dmsOpened struct {
	conv core.Conv
	seq  int
}

type dmsSaved struct {
	err error
	seq int
}

// dmsEntry is one row of the visible list: a DM (dm ≥ 0), a section
// heading, or a directory person without a DM in the list.
type dmsEntry struct {
	dm   int
	head string
	user core.User
}

// dmsRow is the persistent per-row widget state.
type dmsRow struct {
	dlRowState
	todo, more widget.Clickable
}

// DMs is the Direct messages tab.
type DMs struct {
	base

	first  async[dmsResult]
	more   async[dmsResult]
	people async[dmsPeople]
	open   async[dmsOpened]
	save   async[dmsSaved]
	seq    int

	firstApplied, moreApplied, peopleApplied, openApplied, saveApplied int
	moreTried                                                          string

	items []core.DM
	index map[string]int // channel → index into items
	next  string
	pages int

	sel  string
	list widget.List
	view []dmsEntry

	rows    map[string]*dmsRow           // by channel
	pclicks map[string]*widget.Clickable // directory people, by user id
	find    *search
	deb     debounce
	unreads toggle
	compose widget.Clickable
	retry   widget.Clickable

	dirty   bool
	lastQ   string
	lastUnr bool

	w     watcher
	evMu  sync.Mutex
	evq   []core.Event
	known map[string]bool // channels in items, read by the event goroutine (evMu)
}

// NewDMs builds the DMs tab.
func NewDMs(h Host) *DMs {
	d := &DMs{
		base:    newBase(h),
		index:   map[string]int{},
		rows:    map[string]*dmsRow{},
		pclicks: map[string]*widget.Clickable{},
		find:    newSearch("Find a DM…"),
		known:   map[string]bool{},
		dirty:   true,
	}
	d.list.Axis = layout.Vertical
	return d
}

// Selected returns the selected conversation id ("" when none).
func (d *DMs) Selected() string { return d.sel }

// Layout draws the list pane in the themed column and the selected
// conversation in the white pane beside it.
func (d *DMs) Layout(gtx C) D {
	d.frame()
	size := gtx.Constraints.Max
	lw := int(float32(size.X) * dmsPaneFrac)
	lw = max(min(lw, gtx.Dp(dmsPaneMax)), gtx.Dp(dmsPaneMin))
	lw = min(lw, size.X)
	gtx.Constraints = layout.Exact(size)
	return row(gtx, layout.Start,
		layout.Rigid(func(gtx C) D {
			// The list is translucent over the chrome; paint a flat chrome
			// color when hosted as a full page.
			fill(gtx, image.Rect(0, 0, lw, size.Y), dmsChrome)
			fill(gtx, image.Rect(0, 0, lw, size.Y), d.sideBg())
			return fixed(gtx, image.Pt(lw, size.Y), d.LayoutList)
		}),
		layout.Flexed(1, func(gtx C) D {
			sz := image.Pt(gtx.Constraints.Max.X, size.Y)
			fill(gtx, image.Rectangle{Max: sz}, d.p.Bg)
			return fixed(gtx, sz, d.layoutPane)
		}),
	)
}

// LayoutList draws only the list pane, filling the constraints. It paints
// no background so the shell can host it on its themed sidebar.
func (d *DMs) LayoutList(gtx C) D {
	d.frame()
	d.update(gtx)
	size := gtx.Constraints.Max
	gtx.Constraints = layout.Exact(size)

	// The pane is the keyboard focus target for Up/Down.
	area := clip.Rect(image.Rectangle{Max: size}).Push(gtx.Ops)
	event.Op(gtx.Ops, d)
	col(gtx,
		vgap(9),
		layout.Rigid(d.layoutHeader),
		vgap(8),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: dmsInsetX, Right: dmsInsetX}.Layout(gtx, d.layoutFind)
		}),
		vgap(4),
		layout.Flexed(1, d.layoutRows),
	)
	area.Pop()
	return D{Size: size}
}

func (d *DMs) layoutPane(gtx C) D {
	if d.sel != "" {
		if p, ok := d.h.(Panes); ok {
			if w := p.ChannelPane(d.sel); w != nil {
				return w(gtx)
			}
		}
	}
	return d.emptyState(gtx, "dms", "Your direct messages",
		"Pick a conversation from the list, or use Find a DM to start one.", nil, "")
}

func (d *DMs) sideBg() color.NRGBA {
	if d.th != nil && d.th.C.SidebarBg.A > 0 {
		return d.th.C.SidebarBg
	}
	return argb(0x1cf9edff)
}

// ringColor approximates the opaque sidebar color under a row, for the
// presence ring and the group-avatar cutout.
func (d *DMs) ringColor(tinted bool) color.NRGBA {
	c := dmsOver(dmsChrome, d.sideBg())
	if tinted {
		c = dmsOver(c, dmsTint)
	}
	return c
}

func dmsOver(bg, fg color.NRGBA) color.NRGBA {
	top := fg
	top.A = 0xff
	out := theme.Mix(bg, top, float32(fg.A)/255)
	out.A = 0xff
	return out
}

// ---- header and filter ----

func (d *DMs) layoutHeader(gtx C) D {
	gtx.Constraints.Min.Y = gtx.Dp(32)
	gtx.Constraints.Max.Y = gtx.Dp(32)
	return layout.Inset{Left: dmsInsetX, Right: dmsInsetX}.Layout(gtx, func(gtx C) D {
		return layout.W.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.Y = 0
			return row(gtx, layout.Middle,
				layout.Flexed(1, func(gtx C) D {
					return layout.Inset{Left: 8, Right: 8, Top: 4, Bottom: 4}.Layout(gtx, func(gtx C) D {
						return row(gtx, layout.Middle,
							layout.Rigid(func(gtx C) D { return d.label(gtx, 18, wBlack, dmsWhite, "Direct messages") }),
							hgap(4),
							layout.Rigid(func(gtx C) D { return d.icon(gtx, "caret-down", 16, dmsWhite) }),
						)
					})
				}),
				layout.Rigid(d.layoutUnreads),
				hgap(6),
				layout.Rigid(func(gtx C) D {
					return d.compose.Layout(gtx, func(gtx C) D {
						pointerCursor(gtx)
						s := gtx.Dp(28)
						if d.compose.Hovered() {
							rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(8), dmsTint)
						}
						g := gtx
						g.Constraints = layout.Exact(image.Pt(s, s))
						layout.Center.Layout(g, func(gtx C) D { return d.icon(gtx, "compose", 20, dmsSoft) })
						return D{Size: image.Pt(s, s)}
					})
				}),
			)
		})
	})
}

// layoutUnreads is the themed "Unreads" switch: 12px bold label, 8px gap,
// a 28x18 outlined track with a 12px handle inset 3px.
func (d *DMs) layoutUnreads(gtx C) D {
	return d.unreads.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D { return d.label(gtx, 12, wBold, dmsSoft, "Unreads") }),
			hgap(8),
			layout.Rigid(func(gtx C) D {
				w, h := gtx.Dp(28), gtx.Dp(18)
				r := image.Rect(0, 0, w, h)
				k, in := gtx.Dp(12), gtx.Dp(3)
				knob := dmsSoft
				if d.unreads.Value {
					rrect(gtx, r, h/2, dmsSoft)
					knob = dmsPillFg
				} else {
					border(gtx, r, h/2, max(1, gtx.Dp(1)), dmsSoft)
				}
				x := in
				if d.unreads.Value {
					x = w - in - k
				}
				theme.Circle(gtx, image.Pt(x+k/2, h/2), k, knob)
				return D{Size: image.Pt(w, h)}
			}),
		)
	})
}

// layoutFind is the themed "Find a DM…" input: h 28, radius 8, 16px icon,
// 13px text.
func (d *DMs) layoutFind(gtx C) D {
	s := d.find
	w, h := gtx.Constraints.Max.X, gtx.Dp(dmsSearchH)
	r := image.Rect(0, 0, w, h)
	rad := gtx.Dp(8)
	rrect(gtx, r, rad, dmsInputBg)
	bc := dmsRule
	if gtx.Focused(&s.ed) {
		bc = dmsSoft
	}
	border(gtx, r, rad, max(1, gtx.Dp(1)), bc)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, h))
	layout.Inset{Left: 8, Right: 8}.Layout(g, func(gtx C) D {
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D { return d.icon(gtx, "filter", 16, dmsSoft) }),
			hgap(8),
			layout.Flexed(1, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.Stack{Alignment: layout.W}.Layout(gtx,
					layout.Stacked(func(gtx C) D {
						if s.ed.Len() > 0 {
							return D{}
						}
						return d.label(gtx, 13, wReg, dmsSoft, s.ph)
					}),
					layout.Stacked(func(gtx C) D {
						return d.th.Editor(gtx, &s.ed, 13, dmsWhite, argb(0x66f9edff))
					}),
				)
			}),
		)
	})
	return D{Size: image.Pt(w, h)}
}

// ---- state ----

func (d *DMs) update(gtx C) {
	if !d.first.started() {
		d.reload()
		d.w.watch(d.h, d.match)
	}
	if d.w.due(gtx, 2*time.Second) {
		d.reload()
	}
	d.apply()
	d.drainEvents()

	q, changed, submitted := d.find.update(gtx)
	if changed {
		d.dirty = true
		d.deb.set(q, gtx.Now)
	}
	if dq, ok := d.deb.due(gtx, 250*time.Millisecond); ok && dq != "" {
		d.searchPeople(dq)
	}
	d.unreads.Update(gtx)
	if d.unreads.Value != d.lastUnr {
		d.lastUnr, d.dirty = d.unreads.Value, true
		d.list.Position = layout.Position{}
	}
	if q != d.lastQ {
		d.lastQ, d.dirty = q, true
		d.list.Position = layout.Position{}
	}
	if d.dirty {
		d.rebuild()
	}

	if d.compose.Clicked(gtx) {
		gtx.Execute(key.FocusCmd{Tag: &d.find.ed})
	}
	for id, r := range d.rows {
		if r.todo.Clicked(gtx) {
			d.addTodo(id)
		}
		if r.more.Clicked(gtx) {
			if i, ok := d.index[id]; ok && d.items[i].Conv.User != "" {
				d.h.OpenProfile(image.Point{}, d.items[i].Conv.User)
			}
		}
		if r.click.Clicked(gtx) {
			d.choose(id)
			gtx.Execute(key.FocusCmd{Tag: d})
		}
	}
	for id, c := range d.pclicks {
		if c.Clicked(gtx) {
			d.openDM(id)
		}
	}
	if d.retry.Clicked(gtx) {
		d.reload()
	}
	if submitted {
		for _, e := range d.view {
			if e.dm >= 0 {
				d.choose(d.items[e.dm].Channel)
				break
			}
			if e.user.ID != "" {
				d.openDM(e.user.ID)
				break
			}
		}
	}
	d.keys(gtx)

	// With Panes the right side always shows a conversation.
	if d.sel == "" && len(d.items) > 0 {
		if _, ok := d.h.(Panes); ok {
			d.sel = d.items[0].Channel
		}
	}
}

func (d *DMs) reload() {
	d.seq++
	seq := d.seq
	c := d.h.Core()
	d.first.load(d.h, true, func(ctx context.Context) (dmsResult, error) {
		p, err := c.DMs(ctx, "")
		return dmsResult{page: p, seq: seq}, err
	})
}

func (d *DMs) loadMore() {
	if d.next == "" || d.next == d.moreTried || d.more.busy() || d.first.busy() {
		return
	}
	d.seq++
	seq, cur := d.seq, d.next
	d.moreTried = cur
	c := d.h.Core()
	d.more.load(d.h, true, func(ctx context.Context) (dmsResult, error) {
		p, err := c.DMs(ctx, cur)
		return dmsResult{page: p, seq: seq, cursor: cur}, err
	})
}

func (d *DMs) searchPeople(q string) {
	d.seq++
	seq := d.seq
	c := d.h.Core()
	d.people.load(d.h, true, func(ctx context.Context) (dmsPeople, error) {
		us, err := c.SearchUsers(ctx, q, 20)
		return dmsPeople{q: q, users: us, seq: seq}, err
	})
}

// openDM opens (or resumes) a DM with a directory person, then selects it.
// Runs only from a user click.
func (d *DMs) openDM(userID string) {
	if d.open.busy() {
		return
	}
	d.seq++
	seq := d.seq
	c := d.h.Core()
	d.open.load(d.h, false, func(ctx context.Context) (dmsOpened, error) {
		cv, err := c.OpenDM(ctx, userID)
		return dmsOpened{conv: cv, seq: seq}, err
	})
}

// addTodo is the row's "Add to-do" action: it saves the DM's latest
// message to To-dos (saved.add). Runs only from a user click.
func (d *DMs) addTodo(ch string) {
	i, ok := d.index[ch]
	if !ok || d.items[i].Latest == nil || d.save.busy() {
		return
	}
	ts := d.items[i].Latest.TS
	d.seq++
	seq := d.seq
	c := d.h.Core()
	d.save.load(d.h, false, func(ctx context.Context) (dmsSaved, error) {
		err := c.Save(ctx, ch, ts, 0)
		return dmsSaved{err: err, seq: seq}, err
	})
}

func (d *DMs) choose(id string) {
	if id == "" {
		return
	}
	d.sel = id
	if d.unreads.Value {
		d.dirty = true
	}
	if _, ok := d.h.(Panes); !ok {
		d.h.Navigate("c/" + id)
	}
	d.h.Invalidate()
}

// apply takes finished loads into the list.
func (d *DMs) apply() {
	if v, ok, _ := d.first.get(); ok && v.seq != 0 && v.seq != d.firstApplied {
		d.firstApplied = v.seq
		d.mergeFirst(v.page)
	}
	if v, ok, _ := d.more.get(); ok && v.seq != 0 && v.seq != d.moreApplied {
		d.moreApplied = v.seq
		if v.cursor == d.next {
			d.appendPage(v.page)
		}
	}
	if v, ok, _ := d.people.get(); ok && v.seq != 0 && v.seq != d.peopleApplied {
		d.peopleApplied, d.dirty = v.seq, true
	}
	if v, ok, _ := d.save.get(); ok && v.seq != 0 && v.seq != d.saveApplied {
		d.saveApplied = v.seq
		if v.err != nil {
			d.h.Toast("Couldn’t add a to-do: " + friendlyErr(v.err))
		} else {
			d.h.Toast("Added to your to-dos")
		}
	}
	if v, ok, err := d.open.get(); ok && v.seq != 0 && v.seq != d.openApplied {
		d.openApplied = v.seq
		if err != nil {
			d.h.Toast("Couldn’t open that conversation: " + friendlyErr(err))
		} else if v.conv.ID != "" {
			if _, have := d.index[v.conv.ID]; !have {
				dm := core.DM{Channel: v.conv.ID, Conv: v.conv, Count: d.h.Core().Count(v.conv.ID)}
				d.items = append([]core.DM{dm}, d.items...)
				d.changed()
			}
			d.find.ed.SetText("")
			d.choose(v.conv.ID)
			d.reload()
		}
	}
}

func (d *DMs) mergeFirst(p core.DMPage) {
	seen := make(map[string]bool, len(p.DMs)+len(d.items))
	out := make([]core.DM, 0, len(p.DMs)+len(d.items))
	for _, dm := range p.DMs {
		if !seen[dm.Channel] {
			seen[dm.Channel] = true
			out = append(out, dm)
		}
	}
	if d.pages > 1 {
		// Keep the older pages already scrolled in.
		for _, dm := range d.items {
			if !seen[dm.Channel] {
				seen[dm.Channel] = true
				out = append(out, dm)
			}
		}
		dmsSort(out)
	} else {
		d.next, d.pages, d.moreTried = p.Next, 1, ""
	}
	d.items = out
	d.changed()
}

func (d *DMs) appendPage(p core.DMPage) {
	for _, dm := range p.DMs {
		if _, ok := d.index[dm.Channel]; ok {
			continue
		}
		d.index[dm.Channel] = len(d.items)
		d.items = append(d.items, dm)
	}
	d.next = p.Next
	d.pages++
	d.changed()
}

// changed reindexes after items changed.
func (d *DMs) changed() {
	clear(d.index)
	for i := range d.items {
		d.index[d.items[i].Channel] = i
	}
	d.evMu.Lock()
	clear(d.known)
	for id := range d.index {
		d.known[id] = true
	}
	d.evMu.Unlock()
	d.dirty = true
}

func dmsLatestTS(dm *core.DM) string {
	if dm.Latest != nil {
		return dm.Latest.TS
	}
	return dm.Count.Latest
}

func dmsSort(s []core.DM) {
	sort.SliceStable(s, func(i, j int) bool {
		return tsTime(dmsLatestTS(&s[i])).After(tsTime(dmsLatestTS(&s[j])))
	})
}

func dmsUnread(dm *core.DM) bool { return dm.Count.Unread || dm.Count.Mentions > 0 }

// dmsIsDMID guesses from the id alone (D… are IMs; G… may be MPIMs).
func dmsIsDMID(id string) bool { return strings.HasPrefix(id, "D") || strings.HasPrefix(id, "G") }

// ---- realtime ----

// match runs on the event goroutine.
func (d *DMs) match(ev core.Event) bool {
	switch ev.Type {
	case "message", "update", "delete":
		if !isDMKind(ev.ConvKind) && !dmsIsDMID(ev.Channel) {
			return false
		}
		d.evMu.Lock()
		known := d.known[ev.Channel]
		if known {
			d.evq = append(d.evq, ev)
		}
		d.evMu.Unlock()
		if !known {
			// A DM that isn't listed yet: fetch the first page again.
			return ev.Type == "message" && !ev.Reply
		}
		d.h.Invalidate()
		return ev.Type == "delete" // the preview needs the previous message
	case "marked", "counts":
		d.evMu.Lock()
		d.evq = append(d.evq, ev)
		d.evMu.Unlock()
		d.h.Invalidate()
	case "presence", "user":
		d.h.Invalidate() // presence and names are read live from core
	case "resync":
		return true
	case "conv":
		return ev.Conv != nil && isDMKind(ev.Conv.Kind)
	}
	return false
}

// drainEvents applies simple in-place updates on the UI goroutine.
func (d *DMs) drainEvents() {
	d.evMu.Lock()
	evs := d.evq
	d.evq = nil
	d.evMu.Unlock()
	for _, ev := range evs {
		switch ev.Type {
		case "message":
			i, ok := d.index[ev.Channel]
			if !ok || ev.Msg == nil || (ev.Reply && ev.Msg.Subtype != "thread_broadcast") {
				continue
			}
			m := *ev.Msg
			d.items[i].Latest = &m
			if ev.Count != nil {
				d.items[i].Count = *ev.Count
			}
			if i > 0 {
				dm := d.items[i]
				copy(d.items[1:i+1], d.items[:i])
				d.items[0] = dm
				for j := 0; j <= i; j++ {
					d.index[d.items[j].Channel] = j
				}
			}
			d.dirty = true
		case "update":
			i, ok := d.index[ev.Channel]
			if ok && ev.Msg != nil && d.items[i].Latest != nil && d.items[i].Latest.TS == ev.TS {
				m := *ev.Msg
				d.items[i].Latest = &m
				d.dirty = true
			}
		case "marked":
			if i, ok := d.index[ev.Channel]; ok && ev.Count != nil {
				d.items[i].Count = *ev.Count
				d.dirty = true
			}
		case "counts":
			counts := d.h.Core().Counts()
			for i := range d.items {
				if c, ok := counts[d.items[i].Channel]; ok {
					d.items[i].Count = c
				}
			}
			d.dirty = true
		}
	}
}

// ---- keyboard ----

func (d *DMs) keys(gtx C) {
	for {
		ev, ok := gtx.Event(
			key.FocusFilter{Target: d},
			key.Filter{Focus: d, Name: key.NameUpArrow},
			key.Filter{Focus: d, Name: key.NameDownArrow},
		)
		if !ok {
			return
		}
		e, ok := ev.(key.Event)
		if !ok || e.State != key.Press {
			continue
		}
		delta := 1
		if e.Name == key.NameUpArrow {
			delta = -1
		}
		d.step(delta)
	}
}

func (d *DMs) step(delta int) {
	cur := -1
	for i, e := range d.view {
		if e.dm >= 0 && d.items[e.dm].Channel == d.sel {
			cur = i
			break
		}
	}
	for i := cur + delta; i >= 0 && i < len(d.view); i += delta {
		if d.view[i].dm < 0 {
			continue
		}
		d.choose(d.items[d.view[i].dm].Channel)
		pos := d.list.Position
		if i < pos.First {
			d.list.ScrollTo(i)
		} else if pos.Count > 0 && i >= pos.First+pos.Count-1 {
			d.list.ScrollBy(1)
		}
		return
	}
}

// ---- list ----

func (d *DMs) dmName(dm *core.DM) string {
	switch {
	case dm.Conv.User != "":
		return d.userName(dm.Conv.User)
	case len(dm.Conv.Members) > 0:
		return d.groupNames(dm.Conv.Members)
	}
	return d.convTitle(dm.Channel, nil)
}

// matchText is what "Find a DM" searches: names, real names and handles.
func (d *DMs) matchText(dm *core.DM) string {
	ids := dm.Conv.Members
	if dm.Conv.User != "" {
		ids = []string{dm.Conv.User}
	}
	var sb strings.Builder
	sb.WriteString(d.dmName(dm))
	for _, id := range ids {
		if u, ok := d.user(id); ok {
			sb.WriteString(" " + u.RealName + " " + u.Handle)
		}
	}
	return strings.ToLower(sb.String())
}

func (d *DMs) rebuild() {
	d.dirty = false
	q := strings.ToLower(d.find.text())
	d.view = d.view[:0]
	have := map[string]bool{}
	for i := range d.items {
		dm := &d.items[i]
		if d.unreads.Value && !dmsUnread(dm) && dm.Channel != d.sel {
			continue
		}
		if q != "" && !strings.Contains(d.matchText(dm), q) {
			continue
		}
		d.view = append(d.view, dmsEntry{dm: i})
		if dm.Conv.User != "" {
			have[dm.Conv.User] = true
		}
	}
	if q == "" {
		return
	}
	v, ok, _ := d.people.get()
	if !ok || v.q == "" || !strings.HasPrefix(q, strings.ToLower(v.q)) {
		return
	}
	head := false
	for _, u := range v.users {
		if have[u.ID] {
			continue
		}
		t := strings.ToLower(displayName(u) + " " + u.RealName + " " + u.Handle)
		if !strings.Contains(t, q) {
			continue
		}
		if !head {
			d.view = append(d.view, dmsEntry{dm: -1, head: "People"})
			head = true
		}
		d.view = append(d.view, dmsEntry{dm: -1, user: u})
	}
}

func (d *DMs) rowState(id string) *dmsRow {
	r, ok := d.rows[id]
	if !ok {
		r = new(dmsRow)
		d.rows[id] = r
	}
	return r
}

func (d *DMs) personClick(id string) *widget.Clickable {
	c, ok := d.pclicks[id]
	if !ok {
		c = new(widget.Clickable)
		d.pclicks[id] = c
	}
	return c
}

func (d *DMs) layoutRows(gtx C) D {
	_, loaded, err := d.first.get()
	q := d.find.text()
	switch {
	case len(d.items) == 0 && !loaded:
		return d.dmsSkeleton(gtx, 8)
	case len(d.items) == 0 && err != nil:
		return d.notice(gtx, "Couldn’t load your direct messages", friendlyErr(err), &d.retry, "Try again")
	case len(d.view) == 0 && q != "":
		if d.people.busy() || d.deb.q != d.deb.sent {
			return d.dmsSkeleton(gtx, 2)
		}
		return d.notice(gtx, "No results", "Nothing matches “"+q+"”.", nil, "")
	case len(d.view) == 0 && d.unreads.Value:
		return d.notice(gtx, "You’re all caught up", "There are no unread direct messages.", nil, "")
	case len(d.view) == 0:
		return d.notice(gtx, "No direct messages yet", "Use Find a DM to start a conversation.", nil, "")
	}
	n := len(d.view)
	extra := 0
	if d.more.busy() {
		extra = 1
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	dims := d.scrollList(gtx, &d.list, n+extra, func(gtx C, i int) D {
		if i >= n {
			return d.dmsSkeleton(gtx, 1)
		}
		e := d.view[i]
		switch {
		case e.dm >= 0:
			return d.dmRow(gtx, &d.items[e.dm])
		case e.head != "":
			return layout.Inset{Top: 16, Bottom: 4, Left: dmsPad, Right: dmsPad}.Layout(gtx, func(gtx C) D {
				return d.label(gtx, 13, wBold, dmsSoft, e.head)
			})
		default:
			return d.personRow(gtx, e.user)
		}
	})
	if p := d.list.Position; p.First+p.Count >= n-3 {
		d.loadMore()
	}
	return dims
}

// dmRow: padding 16, 36px avatar + 8px gap, name 15/22 bold with the time
// right-aligned (13px), up to two preview lines, 1px bottom rule.
func (d *DMs) dmRow(gtx C, dm *core.DM) D {
	r := d.rowState(dm.Channel)
	sel := dm.Channel == d.sel
	unread := dmsUnread(dm) && !sel
	ring := d.ringColor(sel || r.hovered)

	name := d.dmName(dm)
	self := d.selfID()
	isSelf := dm.Conv.User != "" && dm.Conv.User == self
	var statusURL string
	if dm.Conv.User != "" {
		if u, ok := d.user(dm.Conv.User); ok {
			statusURL = u.StatusEmojiURL
		}
	}
	when, txt := "", ""
	if dm.Latest != nil {
		when = listTime(tsTime(dm.Latest.TS))
		txt = preview(dm.Latest)
		if dm.Latest.User != "" && dm.Latest.User == self {
			txt = "You: " + txt
		}
	}
	prevC := dmsPreview
	if unread {
		prevC = dmsWhite
	}
	bg := color.NRGBA{}
	if sel {
		bg = dmsTint
	}

	var bar W
	if dm.Latest != nil {
		bar = func(gtx C) D {
			btns := []layout.FlexChild{
				layout.Rigid(func(gtx C) D { return d.iconButton(gtx, &r.todo, "check-circle", 32, 18, d.p.Sec) }),
			}
			if dm.Conv.User != "" {
				btns = append(btns, layout.Rigid(func(gtx C) D { return d.iconButton(gtx, &r.more, "more", 32, 18, d.p.Sec) }))
			}
			return d.dlActionBar(gtx, btns...)
		}
	}

	return d.dlHoverRow(gtx, &r.dlRowState, dlRowOpts{Bg: bg, HoverBg: dmsTint, Rule: dmsRule, Bar: bar, BarTop: 4, BarRight: 12, Content: func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return layout.UniformInset(dmsPad).Layout(gtx, func(gtx C) D {
			align := layout.Start
			if txt == "" {
				align = layout.Middle
			}
			return row(gtx, align,
				layout.Rigid(func(gtx C) D { return d.dmAvatar(gtx, dm, ring) }),
				hgap(dmsAvGap),
				layout.Flexed(1, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return col(gtx,
						layout.Rigid(func(gtx C) D {
							return d.nameLine(gtx, name, isSelf, statusURL, when)
						}),
						layout.Rigid(func(gtx C) D {
							if txt == "" {
								return D{}
							}
							return row(gtx, layout.Start,
								layout.Flexed(1, func(gtx C) D {
									return d.para(gtx, 15, dmsLine, wReg, prevC, 2, txt)
								}),
								layout.Rigid(func(gtx C) D {
									if !unread || dm.Count.Mentions <= 0 {
										return D{}
									}
									return layout.Inset{Left: 8, Top: 1}.Layout(gtx, func(gtx C) D {
										return d.dmsPill(gtx, dm.Count.Mentions)
									})
								}),
							)
						}),
					)
				}),
			)
		})
	}})
}

// nameLine is line 1 of a row: bold name (truncated), optional " (you)"
// and 16px status emoji, then the timestamp at the right.
func (d *DMs) nameLine(gtx C, name string, isSelf bool, statusURL, when string) D {
	gtx.Constraints.Min.Y = 0
	youW := 0
	if isSelf {
		youW = d.th.Measure(gtx, dmsNameSize, wReg, " (you)")
	}
	emW := 0
	if statusURL != "" {
		emW = gtx.Dp(24)
	}
	return row(gtx, layout.Middle,
		layout.Flexed(1, func(gtx C) D {
			gtx.Constraints.Min.X = 0
			nameMax := max(0, gtx.Constraints.Max.X-youW-emW)
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D {
					gtx.Constraints.Max.X = nameMax
					return d.para(gtx, dmsNameSize, dmsLine, wBold, dmsWhite, 1, name)
				}),
				layout.Rigid(func(gtx C) D {
					if !isSelf {
						return D{}
					}
					return d.para(gtx, dmsNameSize, dmsLine, wReg, dmsWhite, 1, " (you)")
				}),
				layout.Rigid(func(gtx C) D {
					if statusURL == "" {
						return D{}
					}
					return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return d.emojiImg(gtx, statusURL, 16) })
				}),
			)
		}),
		layout.Rigid(func(gtx C) D {
			if when == "" {
				return D{}
			}
			return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return d.label(gtx, dmsTimeSize, wReg, dmsTimeC, when) })
		}),
	)
}

// dmAvatar draws the 36px avatar with the presence badge (a 20x20 badge
// centered 32px in, ringed in the sidebar color), or the stacked group
// avatar for group DMs.
func (d *DMs) dmAvatar(gtx C, dm *core.DM, ring color.NRGBA) D {
	if dm.Conv.User != "" || dm.Conv.Kind == "im" {
		u, _ := d.user(dm.Conv.User)
		dims := d.avatar(gtx, u.Avatar, dmsAvatar)
		d.presenceBadge(gtx, image.Pt(gtx.Dp(32), gtx.Dp(32)), u.Presence, ring)
		return dims
	}
	self := d.selfID()
	urls := make([]string, 0, 2)
	for _, id := range dm.Conv.Members {
		if id == self {
			continue
		}
		u, _ := d.user(id)
		urls = append(urls, u.Avatar)
		if len(urls) == 2 {
			break
		}
	}
	for len(urls) < 2 {
		urls = append(urls, "")
	}
	// Two 24px avatars: the back one top-left, the front one bottom-right
	// with a ring cut out in the sidebar color.
	s := gtx.Dp(dmsAvatar)
	sub := unit.Dp(24)
	sp := gtx.Dp(sub)
	d.avatar(gtx, urls[0], sub)
	off := s - sp
	st := opOffset(gtx, image.Pt(off, off))
	rw := gtx.Dp(2)
	rrect(gtx, image.Rect(-rw, -rw, sp+rw, sp+rw), avatarRadius(gtx, sub)+rw, ring)
	d.avatar(gtx, urls[1], sub)
	st.Pop()
	return D{Size: image.Pt(s, s)}
}

func (d *DMs) presenceBadge(gtx C, c image.Point, presence string, ring color.NRGBA) {
	if presence == "" {
		return
	}
	theme.Circle(gtx, c, gtx.Dp(14), ring)
	dot := gtx.Dp(9)
	if presence == "active" {
		theme.Circle(gtx, c, dot, dmsGreen)
		return
	}
	theme.Circle(gtx, c, dot, dmsSoft)
	theme.Circle(gtx, c, dot-2*gtx.Dp(1.5), ring)
}

// dmsPill is the unread count pill in the themed list.
func (d *DMs) dmsPill(gtx C, n int) D {
	s := humanCount(n)
	if n > 99 {
		s = "99+"
	}
	tw := d.th.Measure(gtx, 12, wBold, s)
	h := gtx.Dp(20)
	w := max(gtx.Dp(22), tw+gtx.Dp(16))
	rrect(gtx, image.Rect(0, 0, w, h), h/2, dmsPillBg)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, h))
	layout.Center.Layout(g, func(gtx C) D { return d.label(gtx, 12, wBold, dmsPillFg, s) })
	return D{Size: image.Pt(w, h)}
}

func (d *DMs) personRow(gtx C, u core.User) D {
	c := d.personClick(u.ID)
	sub := u.RealName
	if sub == "" || sub == displayName(u) {
		sub = u.Title
	}
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		ring := d.ringColor(c.Hovered())
		m := op.Record(gtx.Ops)
		dims := layout.UniformInset(dmsPad).Layout(gtx, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D {
					ad := d.avatar(gtx, u.Avatar, dmsAvatar)
					d.presenceBadge(gtx, image.Pt(gtx.Dp(32), gtx.Dp(32)), u.Presence, ring)
					return ad
				}),
				hgap(dmsAvGap),
				layout.Flexed(1, func(gtx C) D {
					return col(gtx,
						layout.Rigid(func(gtx C) D {
							return d.para(gtx, dmsNameSize, dmsLine, wBold, dmsWhite, 1, displayName(u))
						}),
						layout.Rigid(func(gtx C) D {
							if sub == "" {
								return D{}
							}
							return d.label(gtx, 13, wReg, dmsTimeC, sub)
						}),
					)
				}),
			)
		})
		call := m.Stop()
		if c.Hovered() {
			fill(gtx, image.Rectangle{Max: dims.Size}, dmsTint)
		}
		call.Add(gtx.Ops)
		hl := max(1, gtx.Dp(1))
		fill(gtx, image.Rect(0, dims.Size.Y, dims.Size.X, dims.Size.Y+hl), dmsRule)
		return D{Size: image.Pt(dims.Size.X, dims.Size.Y+hl)}
	})
}

// notice is the list pane's own empty/error state (white text on the
// themed column).
func (d *DMs) notice(gtx C, title, body string, btn *widget.Clickable, btnLabel string) D {
	return layout.Inset{Top: 32, Left: 24, Right: 24}.Layout(gtx, func(gtx C) D {
		return layout.N.Layout(gtx, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return d.centered(gtx, 15, wBold, dmsWhite, title) }),
				vgap(4),
				layout.Rigid(func(gtx C) D {
					return d.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Color: dmsSoft, Align: text.Middle}, body)
				}),
				layout.Rigid(func(gtx C) D {
					if btn == nil {
						return D{}
					}
					return layout.Inset{Top: 12}.Layout(gtx, func(gtx C) D {
						return btn.Layout(gtx, func(gtx C) D {
							pointerCursor(gtx)
							ld := d.label(gtx, 13, wBold, dmsWhite, btnLabel)
							if btn.Hovered() {
								fill(gtx, image.Rect(0, ld.Size.Y-gtx.Dp(1), ld.Size.X, ld.Size.Y), dmsWhite)
							}
							return ld
						})
					})
				}),
			)
		})
	})
}

func (d *DMs) dmsSkeleton(gtx C, rows int) D {
	ch := make([]layout.FlexChild, 0, rows)
	for i := 0; i < rows; i++ {
		i := i
		ch = append(ch, layout.Rigid(func(gtx C) D {
			return layout.UniformInset(dmsPad).Layout(gtx, func(gtx C) D {
				w := gtx.Constraints.Max.X
				a := gtx.Dp(dmsAvatar)
				rrect(gtx, image.Rect(0, 0, a, a), avatarRadius(gtx, dmsAvatar), dmsSkelColor)
				x := a + gtx.Dp(dmsAvGap)
				w1 := (w - x) * (35 + (i*17)%30) / 100
				w2 := (w - x) * (55 + (i*23)%35) / 100
				rrect(gtx, image.Rect(x, gtx.Dp(4), x+w1, gtx.Dp(16)), gtx.Dp(4), dmsSkelColor)
				rrect(gtx, image.Rect(x, gtx.Dp(24), x+w2, gtx.Dp(34)), gtx.Dp(4), dmsSkelColor)
				return D{Size: image.Pt(w, a)}
			})
		}))
	}
	return col(gtx, ch...)
}
