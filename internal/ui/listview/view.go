// Package listview draws a Slack list natively (docs/LISTS.md): the header
// (icon, title, description), the view switcher with the view's filter and
// sort pills, the table (columns by type, sorted, filtered and grouped the
// way Slack's grid does) and the board, the item pane with its comments,
// and editing through core's lists.* calls behind the fidelity guard.
//
// Three modes, as in Slack: a channel's list tab (in the channel's body),
// the side panel (lists opened from a message, a link, a DM or Activity)
// and the full view (Files → Lists, "Open in full view").
//
// Scraping policy: list contents live in memory only.
package listview

import (
	"context"
	"image"
	"image/color"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/lists"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/richview"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Host is what the view needs from the ui root (the shared names of
// docs/NATIVE.md).
type Host interface {
	Core() *core.App
	Theme() *theme.Theme
	Images() *imgcache.Cache
	Invalidate()
	Navigate(route string)
	OpenProfile(anchor image.Point, userID string)
	Toast(text string)
}

// Mode is where the list is shown.
type Mode uint8

const (
	Tab  Mode = iota // a channel's list tab: no top bar of its own
	Side             // the side panel: a 49px bar with expand / more / close
	Full             // the main view: Slack's 50px list header
)

// Actions are the shell's handlers (nil hides a button or does nothing).
type Actions struct {
	Close     func()                             // side panel ×
	FullView  func()                             // "Open in full view"
	SideView  func()                             // "Open in side panel"
	OpenURL   func(url string)                   // links that aren't Slack's own
	Canvas    func(id string)                    // a canvas cell or link
	OpenItem  func(listID, recordID string)      // an item's pane (the secondary view)
	Menu      func(m []MenuItem)                 // menus: the shell's menu at the pointer
	Copy      func(s string)                     // clipboard
	People    func() []string                    // people to offer in a person field (besides the list's)
	OpenFile  func(id string)                    // an attachment
	OpenRoute func(channel, ts, threadTS string) // a message cell
}

// MenuItem is one row of a menu.
type MenuItem struct {
	Label, Icon string
	Disabled    bool
	Do          func()
}

// View is one open list.
type View struct {
	ctl
	mode Mode

	mu      sync.Mutex
	doc     *core.ListDoc
	err     error
	loading bool
	gen     int
	queue   []func()

	// UI goroutine only
	viewID   string // selected view ("" = the list's default)
	search   widget.Editor
	searchOn bool
	btns     map[string]*widget.Clickable
	styles   styles
	desc     *richview.View
	descDoc  *core.ListDoc
	handler  *handler
	grid     gridState
	board    boardState
	rows     rowCache

	live     atomic.Bool
	lastSeen atomic.Int64
	stop     context.CancelFunc
}

// New returns a view of list id. It loads on its first frame.
func New(h Host, id string, mode Mode, act Actions) *View {
	v := &View{ctl: ctl{h: h, id: id, act: act}, mode: mode, btns: map[string]*widget.Clickable{}}
	v.search.SingleLine = true
	v.handler = &handler{h: h, act: &v.act}
	return v
}

// ID is the list's file id.
func (v *View) ID() string { return v.id }

// Mode is where the view is shown.
func (v *View) Mode() Mode { return v.mode }

// Doc returns the list as last loaded (nil until it arrives).
func (v *View) Doc() *core.ListDoc {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.doc
}

// Title is the list's title once loaded.
func (v *View) Title() string {
	if d := v.Doc(); d != nil {
		return d.Info.Title
	}
	return ""
}

// SelectView switches the view tab (tests, deep links).
func (v *View) SelectView(id string) {
	v.post(func() { v.viewID = id; v.grid.scrollY, v.grid.scrollX = 0, 0 })
}

// Reload refetches the list, keeping what is shown until it arrives.
func (v *View) Reload() { v.load(true) }

func (v *View) load(keep bool) {
	a := v.h.Core()
	if a == nil {
		return
	}
	v.mu.Lock()
	v.gen++
	gen := v.gen
	v.loading = true
	if !keep {
		v.doc, v.err = nil, nil
	}
	v.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		d, err := a.List(ctx, v.id)
		v.mu.Lock()
		if gen == v.gen {
			v.loading = false
			if err == nil || !keep || v.doc == nil {
				v.doc, v.err = d, err
			}
		}
		v.mu.Unlock()
		v.h.Invalidate()
	}()
}

// refresh takes core's live copy (events already applied).
func (v *View) refresh() {
	a := v.h.Core()
	if a == nil {
		return
	}
	if d := a.CachedList(v.id); d != nil {
		v.mu.Lock()
		v.doc = d
		v.mu.Unlock()
		v.h.Invalidate()
	}
}

// goLive opens the list (lists.open) and follows its events until the view
// stops being drawn for a while (the shell has no unmount hook).
func (v *View) goLive() {
	a := v.h.Core()
	if a == nil || !v.live.CompareAndSwap(false, true) {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	evs, unsub := a.Subscribe()
	go func() {
		defer unsub()
		oc, ocancel := context.WithTimeout(ctx, 20*time.Second)
		_ = a.OpenList(oc, v.id)
		ocancel()
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		lastPoll := time.Now()
		var lastUpd int64
		if d := v.Doc(); d != nil {
			lastUpd = d.Info.Updated
		}
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-evs:
				if !ok {
					v.live.Store(false)
					return
				}
				if ev.Type == "list" && ev.ID == v.id {
					v.refresh()
				}
			case <-tick.C:
				if time.Since(time.UnixMilli(v.lastSeen.Load())) > 6*time.Second {
					a.CloseList(v.id)
					v.live.Store(false)
					cancel()
					return
				}
				// realtime off: the echo never comes, so poll the header
				if !a.Realtime() && time.Since(lastPoll) >= core.PollInterval {
					lastPoll = time.Now()
					pc, pcancel := context.WithTimeout(ctx, 20*time.Second)
					info, err := a.ListInfo(pc, v.id)
					pcancel()
					if err == nil && info.Updated != lastUpd {
						if lastUpd != 0 {
							v.load(true)
						}
						lastUpd = info.Updated
					}
				}
			}
		}
	}()
	v.stop = cancel
}

func (v *View) btn(key string) *widget.Clickable {
	b := v.btns[key]
	if b == nil {
		b = new(widget.Clickable)
		v.btns[key] = b
	}
	return b
}

// post runs f on the UI goroutine at the next frame.
func (v *View) post(f func()) {
	v.mu.Lock()
	v.queue = append(v.queue, f)
	v.mu.Unlock()
	v.h.Invalidate()
}

// current is the selected view of d.
func (v *View) current(d *core.ListDoc) *lists.View {
	if d == nil {
		return nil
	}
	if v.viewID != "" {
		if lv := d.Meta.View(v.viewID); lv != nil && lv.Tab() {
			return lv
		}
	}
	return d.Meta.DefaultTab()
}

// Layout draws the view filling the constraints.
func (v *View) Layout(gtx layout.Context) layout.Dimensions {
	v.lastSeen.Store(time.Now().UnixMilli())
	v.mu.Lock()
	q := v.queue
	v.queue = nil
	started := v.gen > 0
	v.mu.Unlock()
	for _, f := range q {
		f()
	}
	if !started {
		v.load(false)
	}
	if !v.live.Load() {
		v.goLive()
	}
	v.mu.Lock()
	d, err := v.doc, v.err
	v.mu.Unlock()
	th := v.h.Theme()
	sz := gtx.Constraints.Max
	gtx.Constraints.Min = sz
	theme.Fill(gtx, image.Rectangle{Max: sz}, th.C.Bg)
	v.styles.sync(th, v.h)
	top := 0
	if v.mode != Tab {
		top = gtx.Dp(theme.HeaderH)
		if v.mode == Full {
			top = gtx.Dp(50)
		}
		v.layoutBar(gtx, d, image.Pt(sz.X, top))
	}
	body := image.Rect(0, top, sz.X, sz.Y)
	defer op.Offset(body.Min).Push(gtx.Ops).Pop()
	gtx.Constraints = layout.Exact(body.Size())
	defer clip.Rect{Max: body.Size()}.Push(gtx.Ops).Pop()
	switch {
	case d == nil && err != nil:
		return v.layoutError(gtx, err)
	case d == nil:
		return v.layoutSkeleton(gtx)
	}
	return v.layoutList(gtx, d)
}

// layoutBar is the side panel's bar or the full view's list header
// (p-list_header: 50px, icon, 18px black title, actions).
func (v *View) layoutBar(gtx layout.Context, d *core.ListDoc, size image.Point) {
	th := v.h.Theme()
	c := &th.C
	theme.Fill(gtx, image.Rect(0, size.Y-gtx.Dp(1), size.X, size.Y), c.Border)
	title := "List"
	if d != nil {
		title = d.Info.Title
	}
	type b struct{ key, icon string }
	var bs []b
	switch v.mode {
	case Side:
		if v.act.FullView != nil {
			bs = append(bs, b{"full", "expand"})
		}
		bs = append(bs, b{"more", "more"})
		if v.act.Close != nil {
			bs = append(bs, b{"close", "close"})
		}
	case Full:
		if v.act.SideView != nil {
			bs = append(bs, b{"side", "panel-right"})
		}
		bs = append(bs, b{"more", "more"})
	}
	x := size.X - gtx.Dp(16)
	for i := len(bs) - 1; i >= 0; i-- {
		bb := bs[i]
		w := gtx.Dp(28)
		x -= w
		clk := v.btn("bar." + bb.key)
		at(gtx, image.Pt(x, (size.Y-w)/2), image.Pt(w, w), func(gtx layout.Context) layout.Dimensions {
			return iconButton(gtx, th, clk, bb.icon, 18)
		})
		if clk.Clicked(gtx) {
			v.barAction(bb.key)
		}
		x -= gtx.Dp(8)
	}
	if d != nil && d.Info.Updated > 0 && v.mode == Full {
		s := "Last edited " + relTime(d.Info.Updated)
		w := th.Measure(gtx, 13, theme.Regular, s)
		x -= w + gtx.Dp(8)
		at(gtx, image.Pt(x, 0), image.Pt(w+2, size.Y), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, size.Y, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, 13, theme.Regular, c.FgMuted, s)
			})
		})
	}
	ix := gtx.Dp(12)
	isz := gtx.Dp(20)
	if d != nil && d.Meta.Icon != "" {
		at(gtx, image.Pt(ix, (size.Y-isz)/2), image.Pt(isz, isz), func(gtx layout.Context) layout.Dimensions {
			return v.emoji(gtx, d.Meta.Icon, 20)
		})
	} else {
		at(gtx, image.Pt(ix, (size.Y-isz)/2), image.Pt(isz, isz), func(gtx layout.Context) layout.Dimensions {
			return icons.Get("list-view")(gtx, 20, c.Fg)
		})
	}
	tx := ix + isz + gtx.Dp(8)
	tsize := unit.Sp(18)
	if v.mode == Side {
		tsize = 16
	}
	at(gtx, image.Pt(tx, 0), image.Pt(max(0, x-tx-gtx.Dp(8)), size.Y), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, size.Y, func(gtx layout.Context) layout.Dimensions {
			return th.Text(gtx, theme.LabelOpts{Font: theme.Font(theme.Black), Size: tsize, Color: c.Fg, MaxLines: 1}, title)
		})
	})
}

func (v *View) barAction(key string) {
	switch key {
	case "full":
		if v.act.FullView != nil {
			v.act.FullView()
		}
	case "side":
		if v.act.SideView != nil {
			v.act.SideView()
		}
	case "close":
		if v.act.Close != nil {
			v.act.Close()
		}
	case "more":
		v.openMore()
	}
}

func (v *View) openMore() {
	if v.act.Menu == nil {
		return
	}
	d := v.Doc()
	var items []MenuItem
	if d != nil && d.Info.Permalink != "" {
		link := d.Info.Permalink
		items = append(items, MenuItem{Label: "Copy link to list", Icon: "link", Do: func() {
			if v.act.Copy != nil {
				v.act.Copy(link)
			}
		}})
		items = append(items, MenuItem{Label: "Open in browser", Icon: "external", Do: func() {
			if v.act.OpenURL != nil {
				v.act.OpenURL(link)
			}
		}})
	}
	if v.mode == Side && v.act.FullView != nil {
		items = append(items, MenuItem{Label: "Open in full view", Icon: "expand", Do: v.act.FullView})
	}
	if v.mode == Full && v.act.SideView != nil {
		items = append(items, MenuItem{Label: "Open in side panel", Icon: "panel-right", Do: v.act.SideView})
	}
	items = append(items, MenuItem{Label: "Refresh", Icon: "refresh", Do: v.Reload})
	v.act.Menu(items)
}

// layoutList is the loaded list: title header, toolbar, then the view.
func (v *View) layoutList(gtx layout.Context, d *core.ListDoc) layout.Dimensions {
	sz := gtx.Constraints.Max
	lv := v.current(d)
	y := v.layoutTitle(gtx, d, sz.X)
	y += v.layoutToolbar(gtx, d, lv, y, sz.X)
	area := image.Rect(0, y, sz.X, sz.Y)
	rows := v.rowsFor(d, lv)
	at(gtx, area.Min, area.Size(), func(gtx layout.Context) layout.Dimensions {
		defer clip.Rect{Max: area.Size()}.Push(gtx.Ops).Pop()
		if lv != nil && lv.Type == lists.ViewBoard {
			return v.layoutBoard(gtx, d, lv, rows)
		}
		return v.layoutGrid(gtx, d, lv, rows)
	})
	return layout.Dimensions{Size: sz}
}

// pad is the list's left inset (p-list_title_header__content left: 36px;
// the side panel uses 16).
func (v *View) pad(gtx layout.Context) int {
	if v.mode == Side {
		return gtx.Dp(16)
	}
	return gtx.Dp(36)
}

// layoutTitle is p-list_title_header: the icon and 28px black title, then
// the description; returns its height.
func (v *View) layoutTitle(gtx layout.Context, d *core.ListDoc, w int) int {
	if v.mode != Tab {
		// the side panel and full view carry the title in their bar; the
		// description still shows under it
		return v.layoutDesc(gtx, d, gtx.Dp(12), w)
	}
	th := v.h.Theme()
	c := &th.C
	x := v.pad(gtx)
	y := gtx.Dp(24)
	lh := gtx.Dp(36)
	if d.Meta.Icon != "" {
		at(gtx, image.Pt(x, y+(lh-gtx.Dp(28))/2), image.Pt(gtx.Dp(28), gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
			return v.emoji(gtx, d.Meta.Icon, 28)
		})
		x += gtx.Dp(28 + 8)
	}
	at(gtx, image.Pt(x, y), image.Pt(max(0, w-x-gtx.Dp(16)), lh), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
			return th.Text(gtx, theme.LabelOpts{Font: theme.Font(theme.Black), Size: 28, Color: c.Fg, MaxLines: 1}, d.Info.Title)
		})
	})
	return v.layoutDesc(gtx, d, y+lh+gtx.Dp(4), w)
}

func (v *View) layoutDesc(gtx layout.Context, d *core.ListDoc, y, w int) int {
	if len(d.Info.Description) == 0 {
		return y
	}
	if v.desc == nil || v.descDoc == nil || v.descDoc.At != d.At {
		v.desc = richview.New(richtext.Doc{Blocks: d.Info.Description}, richview.Options{Secondary: true})
		v.descDoc = d
	}
	x := v.pad(gtx)
	dims := at(gtx, image.Pt(x, y), image.Pt(max(0, w-x-gtx.Dp(16)), gtx.Dp(44)), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Point{}
		return v.desc.Layout(gtx, v.styles.desc, v.handler)
	})
	_ = dims
	return y + gtx.Dp(26)
}

// layoutToolbar is the refine bar: the view switcher (the view's layout
// icon, its name, caret), the view's filter / sort / group pills, then
// search and Slack's Filter and Sort refine buttons on the right.
func (v *View) layoutToolbar(gtx layout.Context, d *core.ListDoc, lv *lists.View, y, w int) int {
	th := v.h.Theme()
	c := &th.C
	h := gtx.Dp(32)
	top := y + gtx.Dp(12)
	x := v.pad(gtx) - gtx.Dp(8)
	// view switcher
	name, icon := "All items", "spreadsheet"
	if lv != nil {
		if lv.Name != "" {
			name = lv.Name
		}
		if lv.Type == lists.ViewBoard {
			icon = "kanban"
		}
	}
	nw := th.Measure(gtx, 15, theme.Bold, name)
	sw := gtx.Dp(8+20+8) + nw + gtx.Dp(4+16+8)
	clk := v.btn("views")
	at(gtx, image.Pt(x, top), image.Pt(sw, h), func(gtx layout.Context) layout.Dimensions {
		return clk.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			s := gtx.Constraints.Max
			if clk.Hovered() {
				theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(8), c.BgHover)
			}
			at(gtx, image.Pt(gtx.Dp(8), (s.Y-gtx.Dp(20))/2), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
				return icons.Get(icon)(gtx, 20, c.Fg)
			})
			at(gtx, image.Pt(gtx.Dp(36), 0), image.Pt(nw+2, s.Y), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, s.Y, func(gtx layout.Context) layout.Dimensions {
					return th.Label(gtx, 15, theme.Bold, c.Fg, name)
				})
			})
			at(gtx, image.Pt(gtx.Dp(36+4)+nw, (s.Y-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
				return icons.Get("caret-up-down")(gtx, 16, c.FgMuted)
			})
			return layout.Dimensions{Size: s}
		})
	})
	if clk.Clicked(gtx) {
		v.viewMenu(d)
	}
	x += sw + gtx.Dp(8)
	// right-hand refine buttons
	rx := w - gtx.Dp(16)
	type rb struct{ key, icon, label string }
	rbs := []rb{{"filter", "filter-sliders", "Filter"}, {"sort", "sort", "Sort"}}
	for i := len(rbs) - 1; i >= 0; i-- {
		b := rbs[i]
		lw := th.Measure(gtx, 13, theme.Bold, b.label)
		bw := gtx.Dp(12+16+4+12) + lw
		rx -= bw
		k := v.btn("refine." + b.key)
		at(gtx, image.Pt(rx, top), image.Pt(bw, h), func(gtx layout.Context) layout.Dimensions {
			return refineButton(gtx, th, k, b.icon, b.label, lw)
		})
		if k.Clicked(gtx) {
			v.refineMenu(d, lv, b.key)
		}
		rx -= gtx.Dp(8)
	}
	// search
	if v.searchOn {
		sw := gtx.Dp(200)
		rx -= sw
		at(gtx, image.Pt(rx, top), image.Pt(sw, h), func(gtx layout.Context) layout.Dimensions {
			s := gtx.Constraints.Max
			theme.Border(gtx, image.Rectangle{Max: s}, gtx.Dp(8), gtx.Dp(1), c.Accent)
			at(gtx, image.Pt(gtx.Dp(8), (s.Y-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
				return icons.Get("search")(gtx, 16, c.FgMuted)
			})
			return at(gtx, image.Pt(gtx.Dp(30), (s.Y-gtx.Dp(20))/2), image.Pt(s.X-gtx.Dp(38), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
				if v.search.Len() == 0 {
					th.Label(gtx, 13, theme.Regular, c.FgSubtle, "Search")
				}
				return th.Editor(gtx, &v.search, 13, c.Fg, c.Selection)
			})
		})
		for {
			ev, ok := gtx.Event(key.Filter{Focus: &v.search, Name: key.NameEscape})
			if !ok {
				break
			}
			if k, ok := ev.(key.Event); ok && k.State == key.Press {
				v.searchOn = false
				v.search.SetText("")
			}
		}
	} else {
		sw := gtx.Dp(32)
		rx -= sw
		k := v.btn("search")
		at(gtx, image.Pt(rx, top), image.Pt(sw, h), func(gtx layout.Context) layout.Dimensions {
			return iconButton(gtx, th, k, "search", 18)
		})
		if k.Clicked(gtx) {
			v.searchOn = true
			gtx.Execute(key.FocusCmd{Tag: &v.search})
		}
	}
	rx -= gtx.Dp(8)
	// the view's saved pills (p-refine_pill--saved)
	if lv != nil {
		for i, p := range v.pills(d, lv) {
			pw := th.Measure(gtx, 13, theme.Bold, p.text) + gtx.Dp(24)
			if p.icon != "" {
				pw += gtx.Dp(18)
			}
			if x+pw > rx {
				break
			}
			k := v.btn("pill." + itoa(i))
			p := p
			at(gtx, image.Pt(x, top), image.Pt(pw, h), func(gtx layout.Context) layout.Dimensions {
				return pill(gtx, th, k, p.icon, p.text)
			})
			if k.Clicked(gtx) && p.do != nil {
				p.do()
			}
			x += pw + gtx.Dp(8)
		}
	}
	return gtx.Dp(12) + h + gtx.Dp(12)
}

type pillInfo struct {
	icon, text string
	do         func()
}

// pills describe the view's saved filters, sorts and grouping the way
// Slack's refine pills do ("Status does not include Done").
func (v *View) pills(d *core.ListDoc, lv *lists.View) []pillInfo {
	var out []pillInfo
	for _, f := range lv.Filters {
		c := d.Meta.Column(f.Key)
		if c == nil {
			continue
		}
		t := c.Name + " " + lists.OperatorLabel(f.Operator, c)
		if vals := v.filterValues(c, f); vals != "" {
			t += " " + vals
		}
		out = append(out, pillInfo{icon: "filter-sliders", text: t})
	}
	for _, s := range lv.Sorts {
		if c := d.Meta.Column(s.Key); c != nil {
			dir := "Descending"
			if s.Ascending {
				dir = "Ascending"
			}
			out = append(out, pillInfo{icon: "sort", text: c.Name + ": " + dir})
		}
	}
	if g := lv.GroupBy(); g != "" && lv.Type != lists.ViewBoard {
		if c := d.Meta.Column(g); c != nil {
			out = append(out, pillInfo{text: "Group by: " + c.Name})
		}
	}
	return out
}

func (v *View) filterValues(c *lists.Column, f lists.Filter) string {
	var out []string
	a := v.h.Core()
	for _, x := range f.Values {
		switch {
		case x == "viewer":
			out = append(out, "me")
		case c.Type == lists.Select:
			if ch, ok := c.Choice(x); ok {
				out = append(out, ch.Label)
			} else {
				out = append(out, x)
			}
		case strings.HasPrefix(x, "U") && a != nil:
			if u, ok := a.User(x); ok {
				out = append(out, u.Name)
				continue
			}
			out = append(out, x)
		default:
			out = append(out, dynamicDateLabel(x))
		}
	}
	return strings.Join(out, ", ")
}

func dynamicDateLabel(s string) string {
	switch s {
	case lists.DateToday:
		return "Today"
	case lists.DateYesterday:
		return "Yesterday"
	case lists.DateTomorrow:
		return "Tomorrow"
	case lists.DateThisWeek:
		return "This week"
	case lists.DateNextWeek:
		return "Next week"
	case lists.DateLastWeek:
		return "Last week"
	case lists.DateUpcoming:
		return "Upcoming"
	case lists.DateOverdue:
		return "Overdue"
	case lists.DateLater:
		return "Later"
	case lists.DateSinceILastViewed:
		return "Since I last viewed"
	}
	return s
}

// viewMenu is the view switcher's menu: the list's views.
func (v *View) viewMenu(d *core.ListDoc) {
	if v.act.Menu == nil || d == nil {
		return
	}
	cur := v.current(d)
	var items []MenuItem
	for _, lv := range d.Meta.TabViews() {
		lv := lv
		icon := "spreadsheet"
		if lv.Type == lists.ViewBoard {
			icon = "kanban"
		}
		name := lv.Name
		if name == "" {
			name = "All items"
		}
		if cur != nil && cur.ID == lv.ID {
			icon = "check"
		}
		items = append(items, MenuItem{Label: name, Icon: icon, Do: func() {
			v.viewID = lv.ID
			v.grid.scrollX, v.grid.scrollY, v.board.scrollX = 0, 0, 0
			v.h.Invalidate()
		}})
	}
	v.act.Menu(items)
}

// refineMenu lists the view's filters or sorts (Clack doesn't save view
// changes: views are read as Slack stores them).
func (v *View) refineMenu(d *core.ListDoc, lv *lists.View, which string) {
	if v.act.Menu == nil || lv == nil {
		return
	}
	var items []MenuItem
	for _, p := range v.pills(d, lv) {
		if (which == "filter") == (p.icon == "filter-sliders") && p.icon != "" {
			items = append(items, MenuItem{Label: p.text, Icon: p.icon, Disabled: true})
		}
	}
	if len(items) == 0 {
		lbl := "No filters in this view"
		if which == "sort" {
			lbl = "No sorts in this view"
		}
		items = append(items, MenuItem{Label: lbl, Disabled: true})
	}
	v.act.Menu(items)
}

// rowCache keeps the view's rows until the list, view or search changes.
type rowCache struct {
	doc    *core.ListDoc
	view   string
	search string
	rows   []lists.Row
}

func (v *View) rowsFor(d *core.ListDoc, lv *lists.View) []lists.Row {
	vid := ""
	if lv != nil {
		vid = lv.ID
	}
	q := strings.TrimSpace(v.search.Text())
	if v.rows.doc == d && v.rows.view == vid && v.rows.search == q {
		return v.rows.rows
	}
	var top []lists.Record
	for _, r := range d.Records {
		if r.ParentRecordID == "" {
			top = append(top, r)
		}
	}
	rows := lists.Rows(&d.Meta, top)
	lists.DefaultOrder(rows, vid)
	env := v.env(d)
	rows = lists.Apply(&d.Meta, lv, rows, q, env)
	if d.Meta.TodoMode && lv != nil && lv.ShowCompleted != nil && !*lv.ShowCompleted {
		var keep []lists.Row
		for _, r := range rows {
			if !r.Flat[lists.TodoCompleted].B {
				keep = append(keep, r)
			}
		}
		rows = keep
	}
	v.rows = rowCache{doc: d, view: vid, search: q, rows: rows}
	return rows
}

func (v *View) env(d *core.ListDoc) lists.Env {
	env := lists.Env{TodoMode: d.Meta.TodoMode, UserNames: map[string]string{}, ChannelNames: map[string]string{}}
	a := v.h.Core()
	if a == nil {
		return env
	}
	env.Self = a.Self().ID
	for _, r := range d.Records {
		for _, f := range r.Fields {
			for _, u := range f.User {
				if _, ok := env.UserNames[u]; !ok {
					if us, ok := a.User(u); ok {
						env.UserNames[u] = firstNonEmpty(us.RealName, us.Name)
					}
				}
			}
			for _, ch := range f.Channel {
				if _, ok := env.ChannelNames[ch]; !ok {
					if c, ok := a.Conv(ch); ok {
						env.ChannelNames[ch] = c.Name
					}
				}
			}
		}
	}
	return env
}

func (v *View) layoutError(gtx layout.Context, err error) layout.Dimensions {
	th := v.h.Theme()
	sz := gtx.Constraints.Max
	msg := "Couldn't load this list"
	if s := err.Error(); strings.Contains(s, "not a list") {
		msg = "This file isn't a list"
	} else if strings.Contains(s, "file_not_found") || strings.Contains(s, "list_not_found") || strings.Contains(s, "file_deleted") {
		msg = "This list was deleted or you don't have access to it"
	}
	y := sz.Y / 3
	w := th.Measure(gtx, 15, theme.Bold, msg)
	at(gtx, image.Pt((sz.X-w)/2, y), image.Pt(w+2, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
		return th.Label(gtx, 15, theme.Bold, th.C.Fg, msg)
	})
	lw := th.Measure(gtx, 13, theme.Bold, "Try again")
	bw := lw + gtx.Dp(24)
	k := v.btn("retry")
	at(gtx, image.Pt((sz.X-bw)/2, y+gtx.Dp(36)), image.Pt(bw, gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
		return refineButton(gtx, th, k, "", "Try again", lw)
	})
	if k.Clicked(gtx) {
		v.load(false)
	}
	return layout.Dimensions{Size: sz}
}

// layoutSkeleton is the loading state: the title bar and grid rows as
// neutral tiles.
func (v *View) layoutSkeleton(gtx layout.Context) layout.Dimensions {
	th := v.h.Theme()
	sz := gtx.Constraints.Max
	x := v.pad(gtx)
	theme.RRect(gtx, image.Rect(x, gtx.Dp(28), x+gtx.Dp(260), gtx.Dp(28+28)), gtx.Dp(6), th.C.Skeleton)
	for i := 0; i < 6; i++ {
		y := gtx.Dp(unit.Dp(110 + 48*i))
		if y > sz.Y {
			break
		}
		theme.RRect(gtx, image.Rect(x, y, min(sz.X-gtx.Dp(16), x+gtx.Dp(640)), y+gtx.Dp(20)), gtx.Dp(4), th.C.Skeleton)
	}
	return layout.Dimensions{Size: sz}
}

// ---- shared drawing helpers ----

func at(gtx layout.Context, p, size image.Point, w layout.Widget) layout.Dimensions {
	defer op.Offset(p).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints = layout.Exact(size)
	return w(g)
}

func vcenter(gtx layout.Context, h int, w layout.Widget) layout.Dimensions {
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints.Min = image.Point{}
	d := w(g)
	call := m.Stop()
	defer op.Offset(image.Pt(0, (h-d.Size.Y)/2)).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
	return layout.Dimensions{Size: image.Pt(d.Size.X, h)}
}

func iconButton(gtx layout.Context, th *theme.Theme, clk *widget.Clickable, icon string, isz unit.Dp) layout.Dimensions {
	s := gtx.Constraints.Max
	return clk.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		if clk.Hovered() {
			theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(6), th.C.BgHover)
		}
		p := gtx.Dp(isz)
		at(gtx, image.Pt((s.X-p)/2, (s.Y-p)/2), image.Pt(p, p), func(gtx layout.Context) layout.Dimensions {
			return icons.Get(icon)(gtx, isz, th.C.FgMuted)
		})
		return layout.Dimensions{Size: s}
	})
}

// refineButton is Slack's p-refine_button: 32px, 1px otl-ter border,
// radius 8, 13px bold label with a 16px icon.
func refineButton(gtx layout.Context, th *theme.Theme, clk *widget.Clickable, icon, label string, lw int) layout.Dimensions {
	c := &th.C
	s := gtx.Constraints.Max
	return clk.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := c.Bg
		if clk.Hovered() {
			bg = c.BgHover
		}
		theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(8), bg)
		theme.Border(gtx, image.Rectangle{Max: s}, gtx.Dp(8), gtx.Dp(1), c.Border)
		x := gtx.Dp(12)
		if icon != "" {
			at(gtx, image.Pt(x, (s.Y-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
				return icons.Get(icon)(gtx, 16, c.Fg)
			})
			x += gtx.Dp(20)
		}
		at(gtx, image.Pt(x, 0), image.Pt(lw+2, s.Y), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, s.Y, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, 13, theme.Bold, c.Fg, label)
			})
		})
		return layout.Dimensions{Size: s}
	})
}

// pill is a saved refine pill (p-refine_pill--saved).
func pill(gtx layout.Context, th *theme.Theme, clk *widget.Clickable, icon, label string) layout.Dimensions {
	c := &th.C
	s := gtx.Constraints.Max
	return clk.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := c.Bg
		if clk.Hovered() {
			bg = c.BgHover
		}
		theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(8), bg)
		theme.Border(gtx, image.Rectangle{Max: s}, gtx.Dp(8), gtx.Dp(1), c.Border)
		x := gtx.Dp(12)
		if icon != "" {
			at(gtx, image.Pt(x, (s.Y-gtx.Dp(14))/2), image.Pt(gtx.Dp(14), gtx.Dp(14)), func(gtx layout.Context) layout.Dimensions {
				return icons.Get(icon)(gtx, 14, c.FgMuted)
			})
			x += gtx.Dp(18)
		}
		at(gtx, image.Pt(x, 0), image.Pt(s.X-x, s.Y), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, s.Y, func(gtx layout.Context) layout.Dimensions {
				return th.Text(gtx, theme.LabelOpts{Font: theme.Font(theme.Bold), Size: 13, Color: c.Fg, MaxLines: 1}, label)
			})
		})
		return layout.Dimensions{Size: s}
	})
}

// shade is a Slack palette ramp colour in the theme's mode.
func shade(th *theme.Theme, name string) color.NRGBA {
	p, ok := palette[name]
	if !ok {
		return th.C.Border
	}
	if th.Dark {
		return p.dark
	}
	return p.light
}

// rampOf maps a select option colour to Slack's plt ramp.
func rampOf(color string) string {
	switch color {
	case "gray":
		return "gray"
	case "brown":
		return "horchata"
	case "orange":
		return "honeycomb"
	case "yellow":
		return "sunflower"
	case "green":
		return "grass"
	case "blue":
		return "lagoon"
	case "indigo":
		return "indigo"
	case "purple":
		return "aubergine"
	case "pink":
		return "flamingo"
	case "red":
		return "tomato"
	case "cyan":
		return "jade"
	}
	return "gray"
}

// optionBg is a select option's pill colour in the (saturated) grid.
func optionBg(th *theme.Theme, color string) color.NRGBA {
	r := rampOf(color)
	if r == "aubergine" {
		return shade(th, r+"-10")
	}
	return shade(th, r+"-5")
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	n := len(b)
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		n--
		b[n] = '-'
	}
	return string(b[n:])
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func relTime(unix int64) string {
	d := time.Since(time.Unix(unix, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		n := int(d / time.Minute)
		if n == 1 {
			return "1 minute ago"
		}
		return itoa(n) + " minutes ago"
	case d < 24*time.Hour:
		n := int(d / time.Hour)
		if n == 1 {
			return "1 hour ago"
		}
		return itoa(n) + " hours ago"
	}
	return time.Unix(unix, 0).Format("Jan 2")
}
