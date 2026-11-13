package composer

import (
	"context"
	"image"
	"sort"
	"strings"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// The snippet dialog's conversation select is Slack's
// ShareDialogChannelPicker (boot-shared eKtn): a searcher you type into,
// over channels (archived ones out, private ones and group DMs in, only
// where you can post), group DMs and people, at most 100 results, the
// empty query allowed (getSearcherSearchSelectOptions). Clack searches
// the sidebar's conversations as you type and adds the workspace's
// channels from the channel search (search.modules's autocomplete), so a
// public channel you're not in can be picked too (sharing joins it:
// ShareDialogAlert's willJoinChannel). People without a DM in the
// sidebar aren't offered: picking one means opening a new DM, which the
// share would have to wait for.

const (
	convPickLimit  = 100
	convPickRows   = 8 // rows shown before the list scrolls
	convPickRowH   = 36
	convPickSearch = 250 * time.Millisecond // the searcher's debounce
)

type convPicker struct {
	d      *snippetDialog
	anchor image.Rectangle // the select, window coordinates
	ed     widget.Editor
	all    []core.Conv // the sidebar's, sorted
	server []core.Conv // the channel search's for the current query
	rows   []core.Conv
	clicks []widget.Clickable
	cur    int
	list   layout.List
	query  string
	seq    int
	due    time.Time
	done   bool
	focus  bool
	block  int // the card's pointer shield tag
	bg     int // the outside-click tag
}

func (p *convPicker) Dismissed() bool { return p.done }

func newConvPicker(d *snippetDialog, anchor image.Rectangle) *convPicker {
	p := &convPicker{d: d, anchor: anchor, cur: 0}
	p.ed.SingleLine, p.ed.Submit = true, true
	p.list.Axis = layout.Vertical
	for _, cv := range d.c.api.snapshot().Convs {
		if cv.IsArchived || cv.Name == "" {
			continue
		}
		p.all = append(p.all, cv)
	}
	sort.SliceStable(p.all, func(i, j int) bool {
		ci, cj := p.all[i].Kind == "im" || p.all[i].Kind == "mpim", p.all[j].Kind == "im" || p.all[j].Kind == "mpim"
		if ci != cj {
			return !ci
		}
		return strings.ToLower(p.all[i].Name) < strings.ToLower(p.all[j].Name)
	})
	p.refilter()
	return p
}

// convMatch reports whether q matches cv: the name, or one of its words,
// starting with q (case-insensitive), or containing it.
func convMatch(cv core.Conv, q string) bool {
	if q == "" {
		return true
	}
	n := strings.ToLower(cv.Name)
	return strings.Contains(n, q)
}

// refilter recomputes the rows from the sidebar's matches and the server
// results for the query.
func (p *convPicker) refilter() {
	q := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(p.query), "#")))
	seen := map[string]bool{}
	p.rows = p.rows[:0]
	add := func(cv core.Conv) {
		if seen[cv.ID] || len(p.rows) >= convPickLimit {
			return
		}
		seen[cv.ID] = true
		p.rows = append(p.rows, cv)
	}
	var starts, contains []core.Conv
	for _, cv := range p.all {
		n := strings.ToLower(cv.Name)
		switch {
		case q == "" || strings.HasPrefix(n, q):
			starts = append(starts, cv)
		case convMatch(cv, q):
			contains = append(contains, cv)
		}
	}
	for _, cv := range starts {
		add(cv)
	}
	for _, cv := range contains {
		add(cv)
	}
	for _, cv := range p.server {
		add(cv)
	}
	for len(p.clicks) < len(p.rows) {
		p.clicks = append(p.clicks, widget.Clickable{})
	}
	if p.cur >= len(p.rows) {
		p.cur = max(0, len(p.rows)-1)
	}
}

// search asks the channel search for q (debounced; stale answers drop).
func (p *convPicker) search(q string) {
	p.seq++
	seq, api, c := p.seq, p.d.c.api, p.d.c
	if strings.TrimSpace(q) == "" || api.a == nil {
		p.server = nil
		p.refilter()
		return
	}
	go func() {
		hits, err := api.searchChannels(context.Background(), strings.TrimPrefix(strings.TrimSpace(q), "#"))
		c.later(func() {
			if seq != p.seq || err != nil {
				return
			}
			p.server = p.server[:0]
			for _, h := range hits {
				if h.IsArchived {
					continue
				}
				p.server = append(p.server, core.Conv{ID: h.ID, Name: h.Name, Kind: h.Kind, NumMembers: h.NumMembers, Preview: !h.IsMember})
			}
			p.refilter()
			p.d.h.Invalidate()
		})
	}()
}

func (p *convPicker) pick(i int) {
	if i < 0 || i >= len(p.rows) {
		return
	}
	cv := p.rows[i]
	p.d.ch = cv.ID
	p.d.picked = cv
	p.done = true
	p.d.h.Invalidate()
}

func (p *convPicker) Layout(gtx layout.Context) layout.Dimensions {
	if p.done {
		return layout.Dimensions{}
	}
	th := p.d.h.Theme()
	cl := popups.ColorsFor(th)
	win := gtx.Constraints.Max
	// a click outside closes it
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &p.bg, Kinds: pointer.Press})
		if !ok {
			break
		}
		if _, ok := e.(pointer.Event); ok {
			p.done = true
			return layout.Dimensions{}
		}
	}
	bgArea := clip.Rect{Max: win}.Push(gtx.Ops)
	event.Op(gtx.Ops, &p.bg)
	bgArea.Pop()

	// keys: arrows move, Enter picks, Escape closes
	for {
		e, ok := gtx.Event(key.Filter{Focus: &p.ed, Name: key.NameUpArrow}, key.Filter{Focus: &p.ed, Name: key.NameDownArrow},
			key.Filter{Focus: &p.ed, Name: key.NameEscape})
		if !ok {
			break
		}
		ke, ok := e.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		switch ke.Name {
		case key.NameUpArrow:
			p.cur = max(0, p.cur-1)
		case key.NameDownArrow:
			p.cur = min(len(p.rows)-1, p.cur+1)
		case key.NameEscape:
			p.done = true
			return layout.Dimensions{}
		}
	}
	for {
		e, ok := p.ed.Update(gtx)
		if !ok {
			break
		}
		switch e.(type) {
		case widget.SubmitEvent:
			p.pick(p.cur)
			return layout.Dimensions{}
		case widget.ChangeEvent:
			if q := p.ed.Text(); q != p.query {
				p.query = q
				p.cur = 0
				p.refilter()
				p.due = gtx.Now.Add(convPickSearch)
			}
		}
	}
	if !p.due.IsZero() && !gtx.Now.Before(p.due) {
		p.due = time.Time{}
		p.search(p.query)
	} else if !p.due.IsZero() {
		gtx.Execute(op.InvalidateCmd{At: p.due})
	}
	for i := range p.rows {
		if p.clicks[i].Clicked(gtx) {
			p.pick(i)
			return layout.Dimensions{}
		}
	}
	if !p.focus {
		p.focus = true
		gtx.Execute(key.FocusCmd{Tag: &p.ed})
	}

	// the card: the search field over the rows, the select's width,
	// above the select (the footer sits at the dialog's bottom)
	rowH, fh := gtx.Dp(convPickRowH), gtx.Dp(40)
	n := min(len(p.rows), convPickRows)
	pad := gtx.Dp(8)
	h := pad + fh + pad + max(n, 1)*rowH + pad
	w := max(p.anchor.Dx(), gtx.Dp(260))
	x := min(p.anchor.Min.X, win.X-w)
	y := p.anchor.Min.Y - h - gtx.Dp(4)
	if y < 0 {
		y = p.anchor.Max.Y + gtx.Dp(4)
	}
	r := image.Rect(x, y, x+w, y+h)
	rad := gtx.Dp(6)
	popups.CardRing(gtx, r, rad, cl.Ring, th.C.Bg) // a box-shadow spread
	fillRRect(gtx, r, rad, th.C.Bg)
	shield := clip.Rect(r).Push(gtx.Ops)
	event.Op(gtx.Ops, &p.block)
	shield.Pop()

	off := op.Offset(image.Pt(x+pad, y+pad)).Push(gtx.Ops)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w-2*pad, fh))
	popups.Field(g, th, &p.ed, i18n.T("share-dialog", "Search for channel or person"), "search", 40, 15)
	off.Pop()

	ly := y + pad + fh + pad
	if len(p.rows) == 0 {
		return layout.Dimensions{Size: win}
	}
	lst := clip.Rect(image.Rect(x, ly, x+w, ly+n*rowH)).Push(gtx.Ops)
	off = op.Offset(image.Pt(x, ly)).Push(gtx.Ops)
	g = gtx
	g.Constraints = layout.Exact(image.Pt(w, n*rowH))
	p.list.Layout(g, len(p.rows), func(gtx layout.Context, i int) layout.Dimensions {
		return p.clicks[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(w, rowH)
			if p.clicks[i].Hovered() {
				p.cur = i
			}
			fg := th.C.Fg
			if i == p.cur {
				paint.FillShape(gtx.Ops, cl.Highlight, clip.Rect{Max: sz}.Op())
				fg = cl.HighText
			}
			cv := p.rows[i]
			ic := "channel"
			switch cv.Kind {
			case "private":
				ic = "lock"
			case "im", "mpim":
				ic = "user"
			}
			io := op.Offset(image.Pt(gtx.Dp(16), (rowH-gtx.Dp(16))/2)).Push(gtx.Ops)
			icons.Get(ic)(gtx, unit.Dp(16), fg)
			io.Pop()
			g := gtx
			g.Constraints = layout.Constraints{Max: image.Pt(w-gtx.Dp(56), rowH)}
			m := op.Record(gtx.Ops)
			ld := label(g, th.Shaper, textStyle{Size: 15, LineHeight: 22, Color: fg, MaxLines: 1}, cv.Name)
			call := m.Stop()
			to := op.Offset(image.Pt(gtx.Dp(40), (rowH-ld.Size.Y)/2)).Push(gtx.Ops)
			call.Add(gtx.Ops)
			to.Pop()
			pointer.CursorPointer.Add(gtx.Ops)
			return layout.Dimensions{Size: sz}
		})
	})
	off.Pop()
	lst.Pop()
	return layout.Dimensions{Size: win}
}
