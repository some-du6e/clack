package listview

import (
	"context"
	"image"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/lists"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Item is an item's pane (Slack's ListRecordDetails secondary view): the
// item's fields, editable in place, and its comments thread with a
// composer. The thread is a message thread in the list's companion
// channel (core.ListRecordThread).
type Item struct {
	ctl
	rec string

	mu       sync.Mutex
	doc      *core.ListDoc
	err      error
	loaded   bool
	comments []core.Message
	cerr     error
	cload    bool
	channel  string
	thread   string
	queue    []func()

	// UI goroutine only
	btns     map[string]*widget.Clickable
	styles   styles
	rvs      map[string]*richview.View
	rvDoc    map[string]*core.Message
	handler  *handler
	scroll   int
	tag      int
	press    f32.Point
	pressed  bool
	hits     []itemHit
	composer widget.Editor
	sending  bool

	live     atomic.Bool
	lastSeen atomic.Int64
}

type itemHit struct {
	r   image.Rectangle
	col *lists.Column
	x0  int
}

// NewItem returns the pane of item recID in list listID.
func NewItem(h Host, listID, recID string, act Actions) *Item {
	it := &Item{ctl: ctl{h: h, id: listID, act: act}, rec: recID, btns: map[string]*widget.Clickable{},
		rvs: map[string]*richview.View{}, rvDoc: map[string]*core.Message{}}
	it.handler = &handler{h: h, act: &it.act}
	it.composer.Submit = true
	return it
}

// ListID and RecordID name the item.
func (it *Item) ListID() string   { return it.id }
func (it *Item) RecordID() string { return it.rec }

func (it *Item) btn(k string) *widget.Clickable {
	b := it.btns[k]
	if b == nil {
		b = new(widget.Clickable)
		it.btns[k] = b
	}
	return b
}

func (it *Item) post(f func()) {
	it.mu.Lock()
	it.queue = append(it.queue, f)
	it.mu.Unlock()
	it.h.Invalidate()
}

// load reads the list (core's cached copy when there is one) and the
// item's thread.
func (it *Item) load() {
	a := it.h.Core()
	if a == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		d := a.CachedList(it.id)
		var err error
		if d == nil {
			d, err = a.List(ctx, it.id)
		}
		it.mu.Lock()
		it.doc, it.err, it.loaded = d, err, true
		it.mu.Unlock()
		it.h.Invalidate()
		it.loadComments()
	}()
}

func (it *Item) loadComments() {
	a := it.h.Core()
	if a == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ch, ts, err := a.ListRecordThread(ctx, it.id, it.rec)
	var msgs []core.Message
	if err == nil {
		msgs, err = a.Replies(ctx, ch, ts)
	}
	it.mu.Lock()
	it.channel, it.thread, it.cload = ch, ts, true
	if err == nil {
		it.comments, it.cerr = msgs, nil
	} else {
		it.cerr = err
	}
	it.mu.Unlock()
	it.h.Invalidate()
}

// goLive follows list and thread events while the pane is drawn.
func (it *Item) goLive() {
	a := it.h.Core()
	if a == nil || !it.live.CompareAndSwap(false, true) {
		return
	}
	evs, unsub := a.Subscribe()
	go func() {
		defer unsub()
		oc, ocancel := context.WithTimeout(context.Background(), 20*time.Second)
		_ = a.OpenList(oc, it.id)
		ocancel()
		defer a.CloseList(it.id)
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		for {
			select {
			case ev, ok := <-evs:
				if !ok {
					it.live.Store(false)
					return
				}
				switch {
				case ev.Type == "list" && ev.ID == it.id:
					if d := a.CachedList(it.id); d != nil {
						it.mu.Lock()
						it.doc = d
						it.mu.Unlock()
						it.h.Invalidate()
					}
				case ev.Channel != "" && ev.Channel == it.channelID() && (ev.Type == "message" || ev.Type == "update" || ev.Type == "delete" || ev.Type == "sent"):
					go it.loadComments()
				}
			case <-tick.C:
				if time.Since(time.UnixMilli(it.lastSeen.Load())) > 6*time.Second {
					it.live.Store(false)
					return
				}
			}
		}
	}()
}

func (it *Item) channelID() string {
	it.mu.Lock()
	defer it.mu.Unlock()
	return it.channel
}

// Layout draws the pane.
func (it *Item) Layout(gtx layout.Context) layout.Dimensions {
	it.lastSeen.Store(time.Now().UnixMilli())
	it.mu.Lock()
	q := it.queue
	it.queue = nil
	d, loaded, err := it.doc, it.loaded, it.err
	comments, cload := it.comments, it.cload
	it.mu.Unlock()
	for _, f := range q {
		f()
	}
	if d == nil && !loaded && err == nil {
		it.mu.Lock()
		it.loaded = true
		it.mu.Unlock()
		it.load()
	}
	if !it.live.Load() {
		it.goLive()
	}
	th := it.h.Theme()
	c := &th.C
	sz := gtx.Constraints.Max
	gtx.Constraints.Min = sz
	theme.Fill(gtx, image.Rectangle{Max: sz}, c.Bg)
	it.styles.sync(th, it.h)
	bar := gtx.Dp(theme.HeaderH)
	it.layoutBar(gtx, d, image.Pt(sz.X, bar))
	defer op.Offset(image.Pt(0, bar)).Push(gtx.Ops).Pop()
	body := image.Pt(sz.X, sz.Y-bar)
	defer clip.Rect{Max: body}.Push(gtx.Ops).Pop()
	gtx.Constraints = layout.Exact(body)
	var rec *lists.Record
	if d != nil {
		rec = d.Record(it.rec)
	}
	switch {
	case d == nil && err != nil:
		at(gtx, image.Pt(gtx.Dp(20), gtx.Dp(24)), image.Pt(body.X-gtx.Dp(40), gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
			return th.Label(gtx, 15, theme.Bold, c.Fg, "Couldn't load this item")
		})
		return layout.Dimensions{Size: sz}
	case d == nil:
		theme.RRect(gtx, image.Rect(gtx.Dp(20), gtx.Dp(24), gtx.Dp(260), gtx.Dp(48)), gtx.Dp(6), c.Skeleton)
		return layout.Dimensions{Size: sz}
	case rec == nil:
		at(gtx, image.Pt(gtx.Dp(20), gtx.Dp(24)), image.Pt(body.X-gtx.Dp(40), gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
			return th.Label(gtx, 15, theme.Bold, c.Fg, nsLists.T("This item was deleted"))
		})
		return layout.Dimensions{Size: sz}
	}
	it.events(gtx, d)
	// content, scrolled
	m := op.Record(gtx.Ops)
	h := it.layoutContent(gtx, d, rec, comments, cload, body.X)
	call := m.Stop()
	maxS := max(0, h-body.Y+gtx.Dp(80))
	it.scroll = min(max(0, it.scroll), maxS)
	func() {
		defer op.Offset(image.Pt(0, -it.scroll)).Push(gtx.Ops).Pop()
		call.Add(gtx.Ops)
	}()
	// the composer pinned at the bottom
	it.layoutComposer(gtx, d, body)
	func() {
		defer clip.Rect{Max: image.Pt(body.X, body.Y-gtx.Dp(72))}.Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &it.tag)
	}()
	return layout.Dimensions{Size: sz}
}

func (it *Item) layoutBar(gtx layout.Context, d *core.ListDoc, size image.Point) {
	th := it.h.Theme()
	c := &th.C
	theme.Fill(gtx, image.Rect(0, size.Y-gtx.Dp(1), size.X, size.Y), c.Border)
	x := size.X - gtx.Dp(12) - gtx.Dp(28)
	if it.act.Close != nil {
		clk := it.btn("close")
		at(gtx, image.Pt(x, (size.Y-gtx.Dp(28))/2), image.Pt(gtx.Dp(28), gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
			return iconButton(gtx, th, clk, "close", 18)
		})
		if clk.Clicked(gtx) {
			it.act.Close()
		}
	}
	ix := gtx.Dp(16)
	at(gtx, image.Pt(ix, (size.Y-gtx.Dp(18))/2), image.Pt(gtx.Dp(18), gtx.Dp(18)), func(gtx layout.Context) layout.Dimensions {
		return icons.Get("list-view")(gtx, 18, c.Fg)
	})
	title := nsLists.T("Item")
	sub := ""
	if d != nil {
		sub = d.Info.Title
	}
	tx := ix + gtx.Dp(26)
	tw := th.Measure(gtx, 18, theme.Black, title)
	at(gtx, image.Pt(tx, 0), image.Pt(tw+2, size.Y), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, size.Y, func(gtx layout.Context) layout.Dimensions {
			return th.Label(gtx, 18, theme.Black, c.Fg, title)
		})
	})
	if sub != "" {
		sx := tx + tw + gtx.Dp(8)
		at(gtx, image.Pt(sx, 0), image.Pt(max(0, x-sx-gtx.Dp(8)), size.Y), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, size.Y, func(gtx layout.Context) layout.Dimensions {
				return th.Text(gtx, theme.LabelOpts{Font: theme.Font(theme.Regular), Size: 13, Color: c.FgMuted, MaxLines: 1}, sub)
			})
		})
	}
}

// itemColumns are the fields the pane shows: the list's record view, else
// the schema (never the primary, which is the pane's title).
func itemColumns(d *core.ListDoc) []*lists.Column {
	var rv *lists.View
	for i := range d.Meta.Views {
		if d.Meta.Views[i].Type == lists.ViewRecord {
			rv = &d.Meta.Views[i]
		}
	}
	var out []*lists.Column
	for _, c := range d.Meta.VisibleColumns(rv) {
		if !c.IsPrimary {
			out = append(out, c)
		}
	}
	return out
}

func (it *Item) layoutContent(gtx layout.Context, d *core.ListDoc, rec *lists.Record, comments []core.Message, cload bool, w int) int {
	th := it.h.Theme()
	c := &th.C
	it.hits = it.hits[:0]
	pad := gtx.Dp(20)
	y := gtx.Dp(20)
	self := ""
	if a := it.h.Core(); a != nil {
		self = a.Self().ID
	}
	// title: the primary field (editable like the grid's)
	if p := d.Meta.Primary(); p != nil {
		r := image.Rect(pad, y, w-pad, y+gtx.Dp(36))
		if it.edit.editing(rec.ID, p) {
			at(gtx, r.Min, r.Size(), func(gtx layout.Context) layout.Dimensions {
				return it.layoutEditor(gtx, d, r.Dx(), r.Dy())
			})
		} else {
			name := rec.Field(p.Key).PlainText()
			col := c.Fg
			if name == "" {
				name, col = nsLists.T("Untitled item"), c.FgSubtle
			}
			at(gtx, r.Min, r.Size(), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, r.Dy(), func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Max.X = r.Dx()
					return th.Text(gtx, theme.LabelOpts{Font: theme.Font(theme.Black), Size: 22, Color: col, MaxLines: 2}, name)
				})
			})
		}
		it.hits = append(it.hits, itemHit{r: r, col: p, x0: r.Min.X})
		y += gtx.Dp(36 + 12)
	}
	// fields
	labW := min(gtx.Dp(160), (w-2*pad)/3)
	rowHt := gtx.Dp(40)
	for _, col := range itemColumns(d) {
		col := col
		lc := shade(th, "gray-70")
		at(gtx, image.Pt(pad, y), image.Pt(labW-gtx.Dp(8), rowHt), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, rowHt, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Max.X = labW - gtx.Dp(8)
				return th.Text(gtx, theme.LabelOpts{Font: theme.Font(theme.Regular), Size: 13, Color: lc, MaxLines: 1}, col.Name)
			})
		})
		vx := pad + labW
		vw := w - pad - vx
		r := image.Rect(vx, y, vx+vw, y+rowHt)
		if it.edit.editing(rec.ID, col) {
			at(gtx, r.Min, r.Size(), func(gtx layout.Context) layout.Dimensions {
				return it.layoutEditor(gtx, d, vw, rowHt)
			})
		} else {
			f := rec.Field(col.Key)
			if lists.Flatten(f).Empty && col.Type != lists.Checkbox && col.Type != lists.TodoCompleted {
				at(gtx, image.Pt(vx, y), image.Pt(vw, rowHt), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, rowHt, func(gtx layout.Context) layout.Dimensions {
						return th.Label(gtx, 15, theme.Regular, c.FgSubtle, nsLists.T("Empty"))
					})
				})
			} else {
				at(gtx, image.Pt(vx, y), image.Pt(vw, rowHt), func(gtx layout.Context) layout.Dimensions {
					it.drawField(gtx, col, f, vw, rowHt, self)
					return layout.Dimensions{Size: gtx.Constraints.Max}
				})
			}
		}
		it.hits = append(it.hits, itemHit{r: r, col: col, x0: vx})
		y += rowHt
	}
	// comments
	y += gtx.Dp(16)
	theme.Fill(gtx, image.Rect(pad, y, w-pad, y+gtx.Dp(1)), c.Border)
	y += gtx.Dp(16)
	at(gtx, image.Pt(pad, y), image.Pt(w-2*pad, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
		return th.Label(gtx, 15, theme.Black, c.Fg, nsLists.T("Comments"))
	})
	y += gtx.Dp(30)
	var replies []core.Message
	for _, m := range comments {
		if m.Subtype == "list_record_comment" || m.TS == m.ThreadTS && strings.TrimSpace(m.Text) == "" {
			continue // the item's root message
		}
		replies = append(replies, m)
	}
	switch {
	case !cload:
		theme.RRect(gtx, image.Rect(pad, y, pad+gtx.Dp(200), y+gtx.Dp(16)), gtx.Dp(4), c.Skeleton)
		y += gtx.Dp(32)
	case len(replies) == 0:
		// Slack's BaseListThreadZeroState
		at(gtx, image.Pt(pad, y+gtx.Dp(3)), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
			return icons.Get("list-view")(gtx, 16, c.Fg)
		})
		at(gtx, image.Pt(pad+gtx.Dp(22), y), image.Pt(w-2*pad, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
			return th.Label(gtx, 15, theme.Bold, c.Fg, nsLists.T("Every item has a thread"))
		})
		y += gtx.Dp(26)
		dims := at(gtx, image.Pt(pad, y), image.Pt(w-2*pad, gtx.Dp(66)), func(gtx layout.Context) layout.Dimensions {
			return th.Text(gtx, theme.LabelOpts{Font: theme.Font(theme.Regular), Size: 15, LineHeight: 22, Color: c.FgMuted},
				"Add comments to the thread to collaborate with your coworkers. Comments stay with the item no matter where it is shared.")
		})
		_ = dims
		y += gtx.Dp(70)
	default:
		a := it.h.Core()
		for i := range replies {
			m := &replies[i]
			name, url := m.User, ""
			if a != nil {
				if u, ok := a.User(m.User); ok {
					name, url = u.Name, u.Avatar
				}
			}
			at(gtx, image.Pt(pad, y+gtx.Dp(4)), image.Pt(gtx.Dp(36), gtx.Dp(36)), func(gtx layout.Context) layout.Dimensions {
				return avatar(gtx, it.h, url, 36)
			})
			tx := pad + gtx.Dp(44)
			nw := th.Measure(gtx, 15, theme.Black, name)
			at(gtx, image.Pt(tx, y), image.Pt(nw+2, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, 15, theme.Black, c.Fg, name)
			})
			when := tsTime(m.TS)
			at(gtx, image.Pt(tx+nw+gtx.Dp(8), y+gtx.Dp(3)), image.Pt(gtx.Dp(120), gtx.Dp(18)), func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, 12, theme.Regular, c.FgMuted, when)
			})
			rv := it.rvs[m.TS]
			if rv == nil || it.rvDoc[m.TS] == nil || it.rvDoc[m.TS].Text != m.Text {
				rv = richview.New(m.Doc, richview.Options{Channel: m.Channel, TS: m.TS})
				it.rvs[m.TS] = rv
				it.rvDoc[m.TS] = m
			}
			dims := at(gtx, image.Pt(tx, y+gtx.Dp(22)), image.Pt(w-tx-pad, gtx.Dp(400)), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min = image.Point{}
				return rv.Layout(gtx, it.styles.body, it.handler)
			})
			y += gtx.Dp(22) + max(dims.Size.Y, gtx.Dp(22)) + gtx.Dp(12)
		}
	}
	return y
}

func (it *Item) drawField(gtx layout.Context, c *lists.Column, f *lists.Field, w, h int, self string) {
	drawCell(gtx, it.h, c, f, w, h, cellOpts{self: self})
}

func tsTime(ts string) string {
	sec, _, _ := strings.Cut(ts, ".")
	var n int64
	for _, r := range sec {
		if r < '0' || r > '9' {
			return ""
		}
		n = n*10 + int64(r-'0')
	}
	t := time.Unix(n, 0)
	if time.Since(t) < 24*time.Hour {
		return t.Format("3:04 PM")
	}
	return t.Format("Jan 2 at 3:04 PM")
}

// layoutComposer is the thread's message box ("Add a comment…").
func (it *Item) layoutComposer(gtx layout.Context, d *core.ListDoc, body image.Point) {
	th := it.h.Theme()
	c := &th.C
	pad := gtx.Dp(20)
	h := gtx.Dp(44)
	r := image.Rect(pad, body.Y-h-gtx.Dp(16), body.X-pad, body.Y-gtx.Dp(16))
	theme.Fill(gtx, image.Rect(0, r.Min.Y-gtx.Dp(12), body.X, body.Y), c.Bg)
	border := c.BorderStrong
	if gtx.Focused(&it.composer) {
		border = c.FgMuted
	}
	theme.RRect(gtx, r, gtx.Dp(8), c.Bg)
	theme.Border(gtx, r, gtx.Dp(8), gtx.Dp(1), border)
	for {
		ev, ok := it.composer.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			it.send()
		}
	}
	at(gtx, image.Pt(r.Min.X+gtx.Dp(12), r.Min.Y+(h-gtx.Dp(22))/2), image.Pt(r.Dx()-gtx.Dp(24), gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
		if it.composer.Len() == 0 {
			th.Label(gtx, 15, theme.Regular, c.FgSubtle, nsLists.T("Add a comment…"))
		}
		return th.Editor(gtx, &it.composer, 15, c.Fg, c.Selection)
	})
}

// send posts the composer's text as a comment (the item's thread reply).
func (it *Item) send() {
	txt := strings.TrimSpace(it.composer.Text())
	a := it.h.Core()
	if txt == "" || a == nil || it.sending {
		return
	}
	it.sending = true
	it.composer.SetText("")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := a.CommentOnListRecord(ctx, it.id, it.rec, txt)
		it.post(func() {
			it.sending = false
			if err != nil {
				it.h.Toast("Couldn't post the comment: " + err.Error())
				it.composer.SetText(txt)
			}
		})
		it.loadComments()
	}()
}

// events: scrolling and clicks on fields.
func (it *Item) events(gtx layout.Context, d *core.ListDoc) {
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &it.tag, Kinds: pointer.Press | pointer.Release | pointer.Scroll,
			ScrollY: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20}})
		if !ok {
			break
		}
		pe, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Scroll:
			it.scroll += int(pe.Scroll.Y)
		case pointer.Press:
			it.press, it.pressed = pe.Position, true
		case pointer.Release:
			mv := pe.Position.Sub(it.press)
			if !it.pressed || mv.X*mv.X+mv.Y*mv.Y > 36 {
				it.pressed = false
				continue
			}
			it.pressed = false
			p := image.Pt(int(pe.Position.X), int(pe.Position.Y)+it.scroll)
			if it.edit.active {
				it.commit(d)
			}
			for _, h := range it.hits {
				if p.In(h.r) {
					if rec := d.Record(it.rec); rec != nil {
						it.cellClick(d, rec, h.col, p.X-h.x0, gtx)
					}
					break
				}
			}
		}
	}
	for {
		ev, ok := gtx.Event(key.Filter{Focus: &it.composer, Name: key.NameEscape})
		if !ok {
			break
		}
		if k, ok := ev.(key.Event); ok && k.State == key.Press {
			gtx.Execute(key.FocusCmd{})
		}
	}
}
