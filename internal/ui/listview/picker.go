package listview

// The list dialogs (docs/LISTS.md "Adding to lists"), from Slack's web
// client:
//
//   - "Add to list" (modern.vendor VzJM AddToListDialog), a message's
//     "Add to list…": the explorer preselects the pref
//     lists_add_to_list_default_list (shown first, cleared when the list
//     is gone), leaves out lists the viewer can only view, and warns
//     "This list doesn't have a message field. Continuing will
//     automatically create one." Add closes the dialog and runs
//     lists.addToList; on success the pref takes the list and the new
//     item opens (openRecord). Errors are only logged, as in Slack.
//     "Create New List" makes a list from the add_to_new_list template,
//     adds the message to it and opens the list.
//   - "Add a list" (client-boot-shared kwlN ListPicker, source
//     "channel-tab"), the channel tab bar's "+" > List: a list with more
//     than one view asks which one the tab opens on (B4EF, /DhB
//     ListPickerSpeedBump: "Select a view for {listName}"), then Insert
//     adds the tab (kXBb maybeOpenShareFileAsTabConfirmationModal):
//     straight away when the list is already shared with the channel,
//     else after "Share this list?" with its Can edit / Can view select.
//     "Create New List" makes a blank list for the tab
//     (useInsertNewListTab).
//
// Both are Slack's list explorer (i/CI useListExplorer) in a modal
// (.p-add_to_list_dialog__modal, .p-list_picker__modal): the search field
// ("Search by list name or keyword"), then the lists as c-pillow_file rows
// without borders, the most recently viewed first ("Last viewed …"), the
// picked one in #1264a3 (p-explorer_result--list--active), at least 400px
// of results, and a footer with Create New List on its left.

import (
	"context"
	"image"
	"image/color"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/lists"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// AddOpts wire a list dialog to the shell. Callbacks run on the UI
// goroutine.
type AddOpts struct {
	Post     func(f func())             // runs f on the UI goroutine (App.Post)
	OpenItem func(listID, recID string) // the item Add made (openRecord)
	OpenList func(id string)            // a list a dialog made (openList)
	Menu     func(items []MenuItem)     // the shell's menu at the pointer (selects)
	// Channel tab bar only: HasTab reports a list already in the tab bar,
	// ShowTab selects (or, once the tab bar reloads, finds) its tab.
	HasTab  func(listID string) bool
	ShowTab func(listID string)
}

type pickKind uint8

const (
	pickAddToList pickKind = iota // AddToListDialog
	pickTab                       // ListPicker, source "channel-tab"
)

// pickHdr is a list's files.info (its schema and access).
type pickHdr struct {
	info core.ListInfo
	meta lists.Meta
	err  error
}

type picker struct {
	h       popups.Host
	kind    pickKind
	o       AddOpts
	channel string // the message's channel, or the tab bar's
	ts      string // the message (add to list)
	def     string // the pref's list (add to list)

	modal *popups.Modal

	mu    sync.Mutex
	queue []func()

	// UI goroutine only
	search   widget.Editor
	typed    string    // the field's text at the last frame
	due      time.Time // when typed is searched
	query    string    // the query the results are for
	results  []core.ListResult
	loading  bool
	failed   bool
	gen      int
	sel      string
	hdrs     map[string]*pickHdr
	asked    map[string]bool // header fetches started
	rows     map[string]*widget.Clickable
	list     layout.List
	opened   bool // the first frame focused the search field
	creating bool // Create New List in flight

	// the speed bump (a channel tab for a list with several views)
	bump     bool
	bumpList string
	bumpView string
	viewSel  widget.Clickable
	back     widget.Clickable
	insert   widget.Clickable
	veil     int
	shared   *shareTab // the last "Share this list?" dialog
}

func newPicker(h popups.Host, kind pickKind, o AddOpts) *picker {
	p := &picker{h: h, kind: kind, o: o, hdrs: map[string]*pickHdr{}, asked: map[string]bool{}, rows: map[string]*widget.Clickable{},
		loading: true}
	p.search.SingleLine = true
	p.search.Submit = true
	p.list.Axis = layout.Vertical
	return p
}

// NewAddToList is Slack's "Add to list" dialog for message ts in channel.
func NewAddToList(h popups.Host, channel, ts string, o AddOpts) *popups.Modal {
	return newAddToList(h, channel, ts, o).modal
}

func newAddToList(h popups.Host, channel, ts string, o AddOpts) *picker {
	p := newPicker(h, pickAddToList, o)
	p.channel, p.ts = channel, ts
	if c := h.Core(); c != nil {
		p.def = c.PrefString(core.PrefAddToListDefault)
		p.sel = p.def
	}
	add := &popups.ModalButton{Label: "Add", Kind: popups.BtnPrimary,
		Enabled: func() bool { return p.sel != "" && !p.creating },
		Do: func() bool {
			p.addTo(p.sel)
			return true
		}}
	p.modal = popups.NewModal(h, popups.ModalOpts{
		Title: "Add to list",
		Body:  p.layout,
		Buttons: []*popups.ModalButton{
			{Label: "Create New List", Kind: popups.BtnOutline, Left: true, Enabled: func() bool { return !p.creating },
				Do: func() bool { p.createAndAdd(); return false }},
			{Label: "Cancel", Kind: popups.BtnOutline},
			add,
		},
		Border:     true,
		Flush:      true,
		Class:      "p-add_to_list_dialog__modal",
		FocusStops: func() []event.Tag { return []event.Tag{&p.search} },
	})
	p.start()
	return p
}

// NewListPicker is Slack's "Add a list" picker for channel's tab bar.
func NewListPicker(h popups.Host, channel string, o AddOpts) *popups.Modal {
	return newListPicker(h, channel, o).modal
}

func newListPicker(h popups.Host, channel string, o AddOpts) *picker {
	p := newPicker(h, pickTab, o)
	p.channel = channel
	insert := &popups.ModalButton{Label: "Insert", Kind: popups.BtnPrimary,
		Enabled: func() bool { return p.sel != "" && !p.bump },
		Do: func() bool {
			p.insertTab(p.sel, p.defaultView(p.sel))
			return true
		}}
	p.modal = popups.NewModal(h, popups.ModalOpts{
		Title: "Add a list",
		Width: 650,
		Body:  p.layout,
		Buttons: []*popups.ModalButton{
			{Label: "Create New List", Kind: popups.BtnOutline, Left: true, Enabled: func() bool { return !p.bump },
				Do: func() bool { p.newTabList(); return true }},
			{Label: "Cancel", Kind: popups.BtnOutline, Enabled: func() bool { return !p.bump }},
			insert,
		},
		Border:     true,
		Flush:      true,
		Class:      "p-list_picker__modal",
		FocusStops: func() []event.Tag { return []event.Tag{&p.search} },
		Over:       p.speedBump,
	})
	p.start()
	return p
}

// post runs f on the UI goroutine at the dialog's next frame.
func (p *picker) post(f func()) {
	p.mu.Lock()
	p.queue = append(p.queue, f)
	p.mu.Unlock()
	p.h.Invalidate()
}

func (p *picker) drain() {
	p.mu.Lock()
	q := p.queue
	p.queue = nil
	p.mu.Unlock()
	for _, f := range q {
		f()
	}
}

// start loads the first page (and the preselected list's header).
func (p *picker) start() {
	p.fetch("")
	if p.def != "" {
		p.header(p.def)
	}
}

// fetch searches the lists for q (search.modules.files type:lists).
func (p *picker) fetch(q string) {
	c := p.h.Core()
	if c == nil {
		p.loading = false
		return
	}
	p.gen++
	gen := p.gen
	p.query, p.loading, p.failed = q, true, false
	go func() {
		ctx, cancel := ctxTimeout(20 * time.Second)
		defer cancel()
		rs, _, err := c.SearchLists(ctx, q, 1)
		p.post(func() {
			if gen != p.gen {
				return
			}
			p.loading, p.failed = false, err != nil
			p.results = rs
		})
	}()
}

// header fetches a list's files.info once.
func (p *picker) header(id string) {
	c := p.h.Core()
	if c == nil || id == "" || p.asked[id] {
		return
	}
	p.asked[id] = true
	go func() {
		ctx, cancel := ctxTimeout(20 * time.Second)
		defer cancel()
		info, meta, err := c.ListHeader(ctx, id)
		p.post(func() {
			p.hdrs[id] = &pickHdr{info: info, meta: meta, err: err}
			if id == p.def && core.IsFileGone(err) {
				// the default list was deleted: Slack drops it and clears the pref
				if p.sel == id {
					p.sel = ""
				}
				p.def = ""
				go func() {
					ctx, cancel := ctxTimeout(15 * time.Second)
					defer cancel()
					_ = c.SetPref(ctx, core.PrefAddToListDefault, "")
				}()
			}
			if p.kind == pickTab && id == p.sel && err == nil {
				p.maybeBump(id)
			}
		})
	}()
}

// rowsShown are the explorer's rows: the default list first (prepended
// when the search didn't return it), view-only lists left out of "Add to
// list" (getListIsViewOnly).
func (p *picker) rowsShown() []core.ListResult {
	var out []core.ListResult
	viewOnly := func(id string) bool {
		if p.kind != pickAddToList {
			return false
		}
		if hd := p.hdrs[id]; hd != nil && hd.err == nil {
			return !hd.info.Editable
		}
		if c := p.h.Core(); c != nil {
			if d := c.CachedList(id); d != nil {
				return !d.Info.Editable
			}
		}
		return false
	}
	var first *core.ListResult
	for i := range p.results {
		r := p.results[i]
		if viewOnly(r.ID) {
			continue
		}
		if p.kind == pickAddToList && r.ID == p.def {
			rr := r
			first = &rr
			continue
		}
		out = append(out, r)
	}
	if p.kind == pickAddToList && p.def != "" && first == nil && p.query == "" {
		if hd := p.hdrs[p.def]; hd != nil && hd.err == nil && !viewOnly(p.def) {
			first = &core.ListResult{File: core.File{ID: p.def, Title: hd.info.Title, User: hd.info.Creator,
				Created: hd.info.Created, Modified: hd.info.Updated, IsList: true}}
		}
	}
	if first != nil {
		out = append([]core.ListResult{*first}, out...)
	}
	return out
}

func (p *picker) rowClick(id string) *widget.Clickable {
	c := p.rows[id]
	if c == nil {
		c = new(widget.Clickable)
		p.rows[id] = c
	}
	return c
}

// pick selects a row (onItemSelected for a "list" item).
func (p *picker) pick(id string) {
	p.sel = id
	p.header(id)
	if p.kind == pickTab {
		p.maybeBump(id)
	}
}

// tabViews are a list's views a tab can open on, the all-items view first
// (ListPickerSpeedBump: every view but the record view).
func tabViews(m lists.Meta) []lists.View {
	var vs []lists.View
	for _, v := range m.Views {
		if v.Type != lists.ViewRecord {
			vs = append(vs, v)
		}
	}
	sort.SliceStable(vs, func(i, j int) bool { return vs[i].IsAllItems && !vs[j].IsAllItems })
	return vs
}

// maybeBump shows the speed bump for a list with several views
// (maybeGetListViewSpeedbump).
func (p *picker) maybeBump(id string) {
	hd := p.hdrs[id]
	if hd == nil || hd.err != nil {
		return
	}
	if vs := tabViews(hd.meta); len(vs) > 1 {
		p.bump, p.bumpList, p.bumpView = true, id, vs[0].ID
	}
}

// defaultView is the view a tab opens on when there was no speed bump:
// the list's all-items view (ListPicker's ed).
func (p *picker) defaultView(id string) string {
	hd := p.hdrs[id]
	if hd == nil {
		return ""
	}
	for _, v := range hd.meta.Views {
		if v.IsAllItems && v.Type != lists.ViewRecord {
			return v.ID
		}
	}
	return ""
}

// ---- actions ----

// addTo is Add: lists.addToList, then the pref and the new item.
func (p *picker) addTo(id string) {
	c := p.h.Core()
	if c == nil || id == "" {
		return
	}
	ch, ts, o := p.channel, p.ts, p.o
	go func() {
		ctx, cancel := ctxTimeout(30 * time.Second)
		defer cancel()
		rec, err := c.AddToList(ctx, id, ch, ts)
		if err != nil {
			log.Printf("lists: error adding to list: %v", err) // Slack only logs it
			return
		}
		_ = c.SetPref(ctx, core.PrefAddToListDefault, id)
		go func() { // openRecord: lists.records.open, then the item opens
			ctx, cancel := ctxTimeout(20 * time.Second)
			defer cancel()
			_ = c.OpenListRecord(ctx, id, rec.ID)
		}()
		if o.Post != nil && o.OpenItem != nil {
			o.Post(func() { o.OpenItem(id, rec.ID) })
		}
	}()
}

// createAndAdd is "Create New List" in "Add to list" (addToNewList).
func (p *picker) createAndAdd() {
	c := p.h.Core()
	if c == nil || p.creating {
		return
	}
	p.creating = true
	ch, ts, o := p.channel, p.ts, p.o
	go func() {
		ctx, cancel := ctxTimeout(45 * time.Second)
		defer cancel()
		id, err := c.CreateListWith(ctx, core.CreateListOpts{TemplateID: "add_to_new_list", Reason: "create-list-from-add-to-new-list"})
		if err != nil {
			log.Printf("lists: error creating list: %v", err)
		} else if _, err = c.AddToList(ctx, id, ch, ts); err != nil {
			log.Printf("lists: error adding to list: %v", err)
		} else {
			_ = c.SetPref(ctx, core.PrefAddToListDefault, id)
		}
		p.post(func() {
			p.creating = false
			p.modal.Dismiss()
			if err == nil && o.OpenList != nil {
				o.OpenList(id)
			}
		})
	}()
}

// insertTab is Insert: the list joins the channel's tab bar.
func (p *picker) insertTab(id, viewID string) {
	if id == "" {
		return
	}
	if p.o.HasTab != nil && p.o.HasTab(id) {
		if p.o.ShowTab != nil {
			p.o.ShowTab(id) // "This list is already attached": its tab opens
		}
		return
	}
	hd := p.hdrs[id]
	if hd != nil && hd.err == nil && core.ListSharedWith(hd.info, p.channel) {
		addTab(p.h, p.o, p.channel, id, viewID, "")
		return
	}
	p.shared = newShareListTab(p.h, p.o, p.channel, id, viewID, hd)
	p.h.ShowOverlay(p.shared.modal)
}

// newTabList is "Create New List" in the tab bar's picker: a blank list
// made for the tab (useInsertNewListTab).
func (p *picker) newTabList() {
	c := p.h.Core()
	if c == nil {
		return
	}
	ch, h, o := p.channel, p.h, p.o
	go func() {
		ctx, cancel := ctxTimeout(45 * time.Second)
		defer cancel()
		id, err := c.CreateListWith(ctx, core.CreateListOpts{TemplateID: "blank", Reason: "create-list-from-channel-tabs",
			TodoMode: c.Experiment("lists_grid_todos") == "on"})
		if err != nil {
			log.Printf("lists: error creating list: %v", err)
			return
		}
		addTabNow(ctx, h, o, ch, id, "", "")
	}()
}

// addTab adds the tab (addTabToChannelAndNavigate) and shows it.
func addTab(h popups.Host, o AddOpts, ch, id, viewID, access string) {
	go func() {
		ctx, cancel := ctxTimeout(30 * time.Second)
		defer cancel()
		addTabNow(ctx, h, o, ch, id, viewID, access)
	}()
}

// addTabNow is addTab on the caller's goroutine; it reports success.
// Failures get addTabToChannel's toasts.
func addTabNow(ctx context.Context, h popups.Host, o AddOpts, ch, id, viewID, access string) bool {
	c := h.Core()
	err := c.AddListTab(ctx, ch, id, viewID, access, "")
	if o.Post == nil {
		return err == nil
	}
	o.Post(func() {
		switch {
		case err == core.ErrTooManyTabs:
			h.Toast("Maximum number of tabs added.")
		case err != nil:
			h.Toast("Cannot add tab.")
		case o.ShowTab != nil:
			o.ShowTab(id)
		}
	})
	return err == nil
}

// ---- drawing ----

func (p *picker) layout(gtx layout.Context) layout.Dimensions {
	p.drain()
	th := p.h.Theme()
	cl := popups.ColorsOf(th)
	if !p.opened {
		p.opened = true
		gtx.Execute(key.FocusCmd{Tag: &p.search})
	}
	// the typed query is searched once typing pauses
	for {
		ev, ok := p.search.Update(gtx)
		if !ok {
			break
		}
		if _, sub := ev.(widget.SubmitEvent); sub {
			p.due = gtx.Now
		}
	}
	if t := strings.TrimSpace(p.search.Text()); t != p.typed {
		p.typed = t
		p.due = gtx.Now.Add(250 * time.Millisecond)
	}
	if !p.due.IsZero() {
		if gtx.Now.Before(p.due) {
			gtx.Execute(op.InvalidateCmd{At: p.due})
		} else {
			p.due = time.Time{}
			if p.typed != p.query {
				p.fetch(p.typed)
			}
		}
	}
	w := gtx.Constraints.Max.X
	line := theme.WithAlpha(th.C.Fg, 26) // rgba(var(--sk_primary_foreground), .1)
	theme.Fill(gtx, image.Rect(0, 0, w, max(1, gtx.Dp(1))), line)
	y := max(1, gtx.Dp(1))
	pad := gtx.Dp(16)
	// .p-explorer_search__container: 10px above (in these dialogs), 16 below
	y += gtx.Dp(10)
	at(gtx, image.Pt(pad, y), image.Pt(w-2*pad, gtx.Dp(36)), func(gtx layout.Context) layout.Dimensions {
		return p.searchField(gtx, th, cl)
	})
	y += gtx.Dp(36) + gtx.Dp(16)
	resH := gtx.Dp(400)
	at(gtx, image.Pt(0, y), image.Pt(w, resH), func(gtx layout.Context) layout.Dimensions {
		return p.resultsArea(gtx, th, cl)
	})
	y += resH
	if msg := p.banner(); msg != "" {
		y += gtx.Dp(12)
		d := at(gtx, image.Pt(pad, y), image.Pt(w-2*pad, 0), func(gtx layout.Context) layout.Dimensions {
			return alertBox(gtx, th, cl, msg)
		})
		y += d.Size.Y + gtx.Dp(12)
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// banner is "Add to list"'s note for a list without a message column.
func (p *picker) banner() string {
	if p.kind != pickAddToList || p.sel == "" {
		return ""
	}
	hd := p.hdrs[p.sel]
	if hd == nil || hd.err != nil || len(hd.meta.Schema) == 0 {
		return ""
	}
	for _, c := range hd.meta.Schema {
		if c.Type == lists.Message {
			return ""
		}
	}
	return "This list doesn’t have a message field. Continuing will automatically create one."
}

// searchField is the explorer's input: 36px, 1px ring, radius 8, the
// magnifier, "Search by list name or keyword".
func (p *picker) searchField(gtx layout.Context, th *theme.Theme, cl *popups.Colors) layout.Dimensions {
	w, h := gtx.Constraints.Max.X, gtx.Dp(36)
	r := image.Rect(0, 0, w, h)
	rad := gtx.Dp(8)
	theme.RRect(gtx, r, rad, th.C.Bg)
	if gtx.Focused(&p.search) {
		theme.Border(gtx, r.Inset(-gtx.Dp(2)), rad+gtx.Dp(2), gtx.Dp(4), cl.FocusRing)
		theme.Border(gtx, r, rad, max(1, gtx.Dp(1)), cl.Focus)
	} else {
		theme.Border(gtx, r, rad, max(1, gtx.Dp(1)), cl.InputBorder)
	}
	at(gtx, image.Pt(gtx.Dp(14), (h-gtx.Dp(20))/2), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
		return icons.Get("search")(gtx, unit.Dp(20), cl.Fg3)
	})
	x := gtx.Dp(48)
	at(gtx, image.Pt(x, 0), image.Pt(w-x-gtx.Dp(12), h), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.Stack{Alignment: layout.W}.Layout(gtx,
				layout.Stacked(func(gtx layout.Context) layout.Dimensions {
					if p.search.Len() > 0 {
						return layout.Dimensions{}
					}
					return th.Label(gtx, 15, theme.Regular, cl.Placeholder, "Search by list name or keyword")
				}),
				layout.Stacked(func(gtx layout.Context) layout.Dimensions {
					return th.Editor(gtx, &p.search, 15, th.C.Fg, th.C.Selection)
				}),
			)
		})
	})
	return layout.Dimensions{Size: r.Max}
}

func (p *picker) resultsArea(gtx layout.Context, th *theme.Theme, cl *popups.Colors) layout.Dimensions {
	sz := gtx.Constraints.Max
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	pad := gtx.Dp(16)
	if p.loading && len(p.results) == 0 {
		for i, y := 0, 0; y < sz.Y; i, y = i+1, y+gtx.Dp(65) {
			at(gtx, image.Pt(pad, y), image.Pt(sz.X-2*pad, gtx.Dp(65)), func(gtx layout.Context) layout.Dimensions {
				return skeletonRow(gtx, th)
			})
		}
		return layout.Dimensions{Size: sz}
	}
	rows := p.rowsShown()
	if len(rows) == 0 {
		return emptyState(gtx, th, cl, p.failed)
	}
	g := gtx
	g.Constraints = layout.Exact(sz)
	p.list.Layout(g, len(rows), func(gtx layout.Context, i int) layout.Dimensions {
		r := rows[i]
		return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return p.row(gtx, th, cl, r)
		})
	})
	return layout.Dimensions{Size: sz}
}

// row is a list-result: the 36px list tile, the 15px bold title and
// "<creator> · Last viewed …" (FileDetailOverride, LAST_VIEWED).
func (p *picker) row(gtx layout.Context, th *theme.Theme, cl *popups.Colors, r core.ListResult) layout.Dimensions {
	clk := p.rowClick(r.ID)
	if clk.Clicked(gtx) {
		p.pick(r.ID)
	}
	w, h := gtx.Constraints.Max.X, gtx.Dp(65)
	active := r.ID == p.sel
	return clk.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		pointer.CursorPointer.Add(gtx.Ops)
		rr := image.Rect(0, 0, w, h)
		title, meta := th.C.Fg, cl.Muted
		switch {
		case active:
			theme.Fill(gtx, rr, cl.Highlight)
			title, meta = cl.HighText, cl.HighText
		case clk.Hovered():
			theme.Fill(gtx, rr, cl.BgAlt)
		}
		pad := gtx.Dp(12)
		s := gtx.Dp(36)
		at(gtx, image.Pt(pad, (h-s)/2), image.Pt(s, s), func(gtx layout.Context) layout.Dimensions {
			return listTile(gtx, th, s)
		})
		x := pad + s + gtx.Dp(12)
		tw := w - x - pad
		at(gtx, image.Pt(x, gtx.Dp(12)), image.Pt(tw, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = image.Point{}
			return th.Label(gtx, 15, theme.Bold, title, firstNonEmpty(r.Title, r.Name, "Untitled list"))
		})
		at(gtx, image.Pt(x, gtx.Dp(35)), image.Pt(tw, gtx.Dp(18)), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = image.Point{}
			return th.Label(gtx, 13, theme.Regular, meta, p.detail(r))
		})
		return layout.Dimensions{Size: rr.Max}
	})
}

// detail is "<creator> · Last viewed today" or "… · Last viewed on Nov 3"
// (last_read, else created).
func (p *picker) detail(r core.ListResult) string {
	t := r.LastRead
	if t == 0 {
		t = r.Created
	}
	s := lastViewed(t, time.Now())
	if c := p.h.Core(); c != nil && r.User != "" {
		if u, ok := c.User(r.User); ok {
			if n := firstNonEmpty(u.Name, u.RealName, u.Handle); n != "" {
				return n + " · " + s
			}
		}
	}
	return s
}

// lastViewed is Slack's LAST_VIEWED detail: a relative day reads "Last
// viewed today", an older one "Last viewed on Nov 3" (with the year when
// it isn't this one).
func lastViewed(unix int64, now time.Time) string {
	if unix <= 0 {
		return "Last viewed"
	}
	t := time.Unix(unix, 0).In(now.Location())
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	switch {
	case !t.Before(today):
		return "Last viewed today"
	case !t.Before(today.AddDate(0, 0, -1)):
		return "Last viewed yesterday"
	case t.Year() == now.Year():
		return "Last viewed on " + t.Format("Jan 2")
	}
	return "Last viewed on " + t.Format("Jan 2, 2006")
}

// listTile is Slack's list file tile: honeycomb-40 with the list glyph in
// honeycomb-0, radius 8.
func listTile(gtx layout.Context, th *theme.Theme, s int) layout.Dimensions {
	theme.RRect(gtx, image.Rect(0, 0, s, s), gtx.Dp(8), shade(th, "honeycomb-40"))
	is := gtx.Dp(20)
	at(gtx, image.Pt((s-is)/2, (s-is)/2), image.Pt(is, is), func(gtx layout.Context) layout.Dimensions {
		return icons.Get("list-view")(gtx, unit.Dp(20), shade(th, "honeycomb-0"))
	})
	return layout.Dimensions{Size: image.Pt(s, s)}
}

// skeletonRow is a loading row (the explorer's listLoadingFragment).
func skeletonRow(gtx layout.Context, th *theme.Theme) layout.Dimensions {
	w, h := gtx.Constraints.Max.X, gtx.Constraints.Max.Y
	pad, s := gtx.Dp(12), gtx.Dp(36)
	col := th.C.Skeleton
	theme.RRect(gtx, image.Rect(pad, (h-s)/2, pad+s, (h+s)/2), gtx.Dp(8), col)
	x := pad + s + gtx.Dp(12)
	theme.RRect(gtx, image.Rect(x, gtx.Dp(16), x+min(gtx.Dp(220), w-x-pad), gtx.Dp(28)), gtx.Dp(4), col)
	theme.RRect(gtx, image.Rect(x, gtx.Dp(36), x+min(gtx.Dp(140), w-x-pad), gtx.Dp(46)), gtx.Dp(4), col)
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// emptyState is the explorer's "No results".
func emptyState(gtx layout.Context, th *theme.Theme, cl *popups.Colors, failed bool) layout.Dimensions {
	sz := gtx.Constraints.Max
	title, desc := "No results", "You may want to try adjusting your filters."
	if failed {
		title, desc = "Something went wrong", "Lists couldn’t be loaded."
	}
	tw := th.Measure(gtx, 15, theme.Bold, title)
	dw := th.Measure(gtx, 13, theme.Regular, desc)
	top := sz.Y/2 - gtx.Dp(22)
	at(gtx, image.Pt((sz.X-tw)/2, top), image.Pt(tw+1, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
		return th.Label(gtx, 15, theme.Bold, th.C.Fg, title)
	})
	at(gtx, image.Pt((sz.X-dw)/2, top+gtx.Dp(24)), image.Pt(dw+1, gtx.Dp(18)), func(gtx layout.Context) layout.Dimensions {
		return th.Label(gtx, 13, theme.Regular, cl.Muted, desc)
	})
	return layout.Dimensions{Size: sz}
}

// alertBox is Slack's boxed default Alert (hty6): base-ter, radius 8,
// padding 12 16, the slack-logo icon in content-ter, 15/22 text.
func alertBox(gtx layout.Context, th *theme.Theme, cl *popups.Colors, msg string) layout.Dimensions {
	w := gtx.Constraints.Max.X
	px, py := gtx.Dp(16), gtx.Dp(12)
	is := gtx.Dp(20)
	tx := px + is + gtx.Dp(12)
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(max(1, w-tx-px), 1<<20)}
	td := th.Para(g, 15, 22, theme.Regular, th.C.Fg, msg)
	call := m.Stop()
	h := td.Size.Y + 2*py
	r := image.Rect(0, 0, w, h)
	theme.RRect(gtx, r, gtx.Dp(8), th.C.Skeleton) // --dt_color-base-ter
	if th.Dark {
		theme.Border(gtx, r, gtx.Dp(8), max(1, gtx.Dp(1)), th.C.BorderStrong) // light-dark(transparent, otl-sec)
	}
	at(gtx, image.Pt(px, py+gtx.Dp(1)), image.Pt(is, is), func(gtx layout.Context) layout.Dimensions {
		return icons.Get("slack-logo")(gtx, unit.Dp(20), cl.Fg3)
	})
	func() {
		defer op.Offset(image.Pt(tx, py)).Push(gtx.Ops).Pop()
		call.Add(gtx.Ops)
	}()
	return layout.Dimensions{Size: r.Max}
}

// speedBump is ListPickerSpeedBump over the picker (c-modal_speed_bump):
// the dialog veiled at 70%, and a sheet at its bottom with "Select a view
// for {listName}", the subtitle, the View select and Back / Insert.
func (p *picker) speedBump(gtx layout.Context) {
	if !p.bump {
		return
	}
	th := p.h.Theme()
	cl := popups.ColorsOf(th)
	if p.back.Clicked(gtx) { // Back: the pick is undone
		p.bump, p.sel, p.bumpList, p.bumpView = false, "", "", ""
		return
	}
	if p.insert.Clicked(gtx) {
		id, v := p.bumpList, p.bumpView
		p.bump = false
		p.modal.Dismiss()
		p.insertTab(id, v)
		return
	}
	hd := p.hdrs[p.bumpList]
	if hd == nil {
		return
	}
	views := tabViews(hd.meta)
	if p.viewSel.Clicked(gtx) && p.o.Menu != nil {
		var items []MenuItem
		for _, v := range views {
			v := v
			items = append(items, MenuItem{Label: v.Name, Icon: viewIcon(v), Do: func() { p.bumpView = v.ID; p.h.Invalidate() }})
		}
		p.o.Menu(items)
	}
	sz := gtx.Constraints.Max
	// the veil takes every press under it
	func() {
		defer clip.RRect{Rect: image.Rectangle{Max: sz}, NE: gtx.Dp(8), NW: gtx.Dp(8), SE: gtx.Dp(8), SW: gtx.Dp(8)}.Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &p.veil)
		for {
			if _, ok := gtx.Event(pointer.Filter{Target: &p.veil, Kinds: pointer.Press | pointer.Release | pointer.Scroll, ScrollY: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20}}); !ok {
				break
			}
		}
		theme.Fill(gtx, image.Rectangle{Max: sz}, theme.WithAlpha(th.C.Bg, 179))
	}()
	// the sheet: base-pry, 1px otl-ter top, padding 20 24
	px, py := gtx.Dp(24), gtx.Dp(20)
	iw := sz.X - 2*px
	m := op.Record(gtx.Ops)
	y := 0
	name := firstNonEmpty(hd.info.Title, "Untitled list")
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(iw, 1<<20)}
	d := th.Para(g, 18, 24, theme.Bold, th.C.Fg, "Select a view for "+name)
	y += d.Size.Y
	func() {
		defer op.Offset(image.Pt(0, y)).Push(gtx.Ops).Pop()
		d = th.Para(g, 13, 18, theme.Regular, cl.Fg2, "This will be the default view for anyone who opens the list")
	}()
	y += d.Size.Y + gtx.Dp(16)
	func() { // the View select
		defer op.Offset(image.Pt(0, y)).Push(gtx.Ops).Pop()
		d = th.Label(g, 15, theme.Bold, th.C.Fg, "View")
	}()
	y += d.Size.Y + gtx.Dp(8)
	var cur lists.View
	for _, v := range views {
		if v.ID == p.bumpView {
			cur = v
		}
	}
	func() {
		defer op.Offset(image.Pt(0, y)).Push(gtx.Ops).Pop()
		gg := gtx
		gg.Constraints = layout.Exact(image.Pt(iw, gtx.Dp(36)))
		p.viewSel.Layout(gg, func(gtx layout.Context) layout.Dimensions {
			pointer.CursorPointer.Add(gtx.Ops)
			return selectBox(gtx, th, cl, viewIcon(cur), cur.Name, p.viewSel.Hovered())
		})
	}()
	y += gtx.Dp(36) + gtx.Dp(24)
	bw1 := th.Measure(gtx, 15, theme.Bold, "Back") + gtx.Dp(24)
	bw2 := max(th.Measure(gtx, 15, theme.Bold, "Insert")+gtx.Dp(24), gtx.Dp(80))
	bw1 = max(bw1, gtx.Dp(80))
	func() {
		defer op.Offset(image.Pt(iw-bw2-gtx.Dp(12)-bw1, y)).Push(gtx.Ops).Pop()
		sheetButton(gtx, th, cl, &p.back, "Back", false, bw1)
	}()
	func() {
		defer op.Offset(image.Pt(iw-bw2, y)).Push(gtx.Ops).Pop()
		sheetButton(gtx, th, cl, &p.insert, "Insert", true, bw2)
	}()
	y += gtx.Dp(36)
	call := m.Stop()
	sh := y + 2*py
	top := sz.Y - sh
	sheet := image.Rect(0, top, sz.X, sz.Y)
	func() {
		defer clip.RRect{Rect: sheet, SE: gtx.Dp(8), SW: gtx.Dp(8)}.Push(gtx.Ops).Pop()
		theme.Fill(gtx, sheet, cl.ModalBg)
	}()
	theme.Fill(gtx, image.Rect(0, top, sz.X, top+max(1, gtx.Dp(1))), cl.Divider)
	func() {
		defer op.Offset(image.Pt(px, top+py)).Push(gtx.Ops).Pop()
		call.Add(gtx.Ops)
	}()
}

// viewIcon is the speed bump option's icon: home for all items, kanban
// for a board, spreadsheet for the rest.
func viewIcon(v lists.View) string {
	switch {
	case v.IsAllItems:
		return "home"
	case v.Type == lists.ViewBoard:
		return "kanban"
	}
	return "spreadsheet"
}

// selectBox is a Slack select button: 36px, 1px otl-sec, radius 8, the
// option's icon and label, a caret on the right.
func selectBox(gtx layout.Context, th *theme.Theme, cl *popups.Colors, ic, label string, hovered bool) layout.Dimensions {
	sz := gtx.Constraints.Max
	r := image.Rectangle{Max: sz}
	bg := th.C.Bg
	if hovered {
		bg = cl.BtnHover
	}
	theme.RRect(gtx, r, gtx.Dp(8), cl.BtnBorder)
	theme.RRect(gtx, r.Inset(max(1, gtx.Dp(1))), gtx.Dp(8)-1, bg)
	x := gtx.Dp(12)
	if ic != "" {
		at(gtx, image.Pt(x, (sz.Y-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
			return icons.Get(ic)(gtx, unit.Dp(16), th.C.Fg)
		})
		x += gtx.Dp(16) + gtx.Dp(8)
	}
	at(gtx, image.Pt(x, 0), image.Pt(max(1, sz.X-x-gtx.Dp(36)), sz.Y), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
			return th.Label(gtx, 15, theme.Regular, th.C.Fg, label)
		})
	})
	at(gtx, image.Pt(sz.X-gtx.Dp(28), (sz.Y-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
		return icons.Get("caret-down")(gtx, unit.Dp(16), th.C.Fg)
	})
	return layout.Dimensions{Size: sz}
}

// sheetButton is a medium (36px) outline or primary button.
func sheetButton(gtx layout.Context, th *theme.Theme, cl *popups.Colors, clk *widget.Clickable, label string, primary bool, w int) {
	h := gtx.Dp(36)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, h))
	clk.Layout(g, func(gtx layout.Context) layout.Dimensions {
		pointer.CursorPointer.Add(gtx.Ops)
		r := image.Rect(0, 0, w, h)
		bg, fg, bd := cl.BtnBg, th.C.Fg, cl.BtnBorder
		if primary {
			bg, fg, bd = cl.Primary, cl.PrimaryText, color.NRGBA{}
		}
		if clk.Hovered() {
			if primary {
				bg = theme.Mix(bg, color.NRGBA{A: 255}, .1)
			} else {
				bg = cl.BtnHover
			}
		}
		if bd.A > 0 {
			theme.RRect(gtx, r, gtx.Dp(8), bd)
			theme.RRect(gtx, r.Inset(max(1, gtx.Dp(1))), gtx.Dp(8)-1, bg)
		} else {
			theme.RRect(gtx, r, gtx.Dp(8), bg)
		}
		tw := th.Measure(gtx, 15, theme.Bold, label)
		at(gtx, image.Pt((w-tw)/2, 0), image.Pt(tw+1, h), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, 15, theme.Bold, fg, label)
			})
		})
		return layout.Dimensions{Size: r.Max}
	})
}

// ---- "Share this list?" ----

// shareTab is ShareFileAsTabConfirmationModal for a list: "When you
// share a list here, anyone in #channel can access it.", the access
// select on the footer's left, Cancel and Share.
type shareTab struct {
	h       popups.Host
	o       AddOpts
	ch, id  string
	viewID  string
	access  string // "write" | "read"
	canEdit bool   // the viewer can give the channel edit access
	busy    bool
	sel     widget.Clickable
	modal   *popups.Modal
}

func newShareListTab(h popups.Host, o AddOpts, ch, id, viewID string, hd *pickHdr) *shareTab {
	s := &shareTab{h: h, o: o, ch: ch, id: id, viewID: viewID, access: "read"}
	if hd != nil && hd.err == nil && (hd.info.Access == "owner" || hd.info.Access == "write" || hd.info.Editable) {
		s.canEdit, s.access = true, "write" // Slack's default: write when the viewer may grant it
	}
	share := &popups.ModalButton{Label: "Share", Kind: popups.BtnPrimary, Enabled: func() bool { return !s.busy },
		Do: func() bool { s.share(); return false }}
	s.modal = popups.NewModal(h, popups.ModalOpts{
		Title: "Share this list?",
		Body:  s.body,
		Buttons: []*popups.ModalButton{
			{Label: "Cancel", Kind: popups.BtnOutline},
			share,
		},
		AutoFocus: share,
		Over:      s.footerSelect,
	})
	return s
}

func (s *shareTab) channelName() string {
	if c := s.h.Core(); c != nil {
		if cv, ok := c.Conv(s.ch); ok && cv.Name != "" {
			return "#" + cv.Name
		}
	}
	return "this channel"
}

func (s *shareTab) body(gtx layout.Context) layout.Dimensions {
	th := s.h.Theme()
	return th.Para(gtx, 15, 22, theme.Regular, th.C.Fg, "When you share a list here, anyone in "+s.channelName()+" can access it.")
}

func accessLabel(a string) string {
	if a == "write" {
		return "Can edit"
	}
	return "Can view"
}

// footerSelect draws the access select at the footer's left (the
// modal's 28px side padding, its 20px footer padding).
func (s *shareTab) footerSelect(gtx layout.Context) {
	th := s.h.Theme()
	cl := popups.ColorsOf(th)
	if s.sel.Clicked(gtx) && s.canEdit && s.o.Menu != nil {
		s.o.Menu([]MenuItem{
			{Label: "Can edit", Do: func() { s.access = "write"; s.h.Invalidate() }},
			{Label: "Can view", Do: func() { s.access = "read"; s.h.Invalidate() }},
		})
	}
	sz := gtx.Constraints.Max
	label := accessLabel(s.access)
	w := th.Measure(gtx, 15, theme.Regular, label) + gtx.Dp(12+8+16+12)
	h := gtx.Dp(36)
	pos := image.Pt(gtx.Dp(28), sz.Y-gtx.Dp(20)-h)
	defer op.Offset(pos).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, h))
	s.sel.Layout(g, func(gtx layout.Context) layout.Dimensions {
		if s.canEdit {
			pointer.CursorPointer.Add(gtx.Ops)
		}
		return selectBox(gtx, th, cl, "", label, s.canEdit && s.sel.Hovered())
	})
}

// share adds the tab with the access picked; addTabToChannel's errors
// toast and keep the dialog open.
func (s *shareTab) share() {
	if s.busy {
		return
	}
	s.busy = true
	h, o, ch, id, view, access := s.h, s.o, s.ch, s.id, s.viewID, s.access
	post := o.Post
	go func() {
		ctx, cancel := ctxTimeout(30 * time.Second)
		defer cancel()
		err := h.Core().AddListTab(ctx, ch, id, view, access, "")
		if post == nil {
			return
		}
		post(func() {
			s.busy = false
			switch {
			case err == core.ErrTooManyTabs:
				h.Toast("Maximum number of tabs added.")
			case err != nil:
				h.Toast("Cannot add tab.")
			default:
				s.modal.Dismiss()
				if o.ShowTab != nil {
					o.ShowTab(id)
				}
			}
		})
	}()
}
