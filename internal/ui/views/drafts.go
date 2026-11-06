package views

// Drafts & sent (Home sidebar): unsent drafts, scheduled messages (drafts
// with a date_scheduled) and the viewer's sent messages. Slack reads Sent
// from messages.sent.list, which core doesn't expose yet, so Sent is a
// search for from:<@self> sorted by time.
//
// Measurements: /tmp/ui-views-spec/drafts.md. The header and tabs are
// white; the content below is #f8f8f8 with white cards.

import (
	"context"
	"image"
	"sort"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

var draftsTabLabels = []string{"Drafts", "Scheduled", "Sent"}

const (
	draftsTabDrafts = iota
	draftsTabScheduled
	draftsTabSent
)

const (
	draftsHeadH  unit.Dp = 49
	draftsTile   unit.Dp = 36
	draftsGap    unit.Dp = 12 // tile → text
	draftsMargin unit.Dp = 20 // list and card side margin
	draftsSentN          = 30
)

type draftsResult struct {
	list []core.Draft
	seq  int
}

type sentResult struct {
	res  core.SearchResult
	seq  int
	page int
}

type draftsOpened struct {
	conv core.Conv
	seq  int
}

type draftsRow struct {
	dlRowState
	del, edit, sched, send widget.Clickable
}

// draftsEntry is one element of the visible list.
type draftsEntry struct {
	kind        int // draftsEntry* below
	i           int // index into the tab's slice
	label       string
	first, last bool // sent rows: ends of a day group
}

const (
	draftsEntryCard = iota
	draftsEntryGap
	draftsEntryDay
	draftsEntrySent
	draftsEntryDraft
	draftsEntryEnd
)

// Drafts is the Drafts & sent view.
type Drafts struct {
	base

	tabs     *tabs
	seq      int
	last     time.Time
	watching bool

	dl                async[draftsResult]
	dlApplied         int
	drafts, scheduled []core.Draft

	sent, sentMore  async[sentResult]
	sentApplied     int
	sentMoreApplied int
	sentMsgs        []core.Message
	sentConvs       map[string]core.ConvRef
	sentPage        int // last page appended (1-based; 0 = none)
	sentMax         int // result pages available
	sentTried       int // page last requested by loadMoreSent

	open        async[draftsOpened]
	openApplied int

	view      []draftsEntry
	viewTab   int
	dirty     bool
	eduHidden bool

	lists             [3]widget.List
	rows              map[string]*draftsRow
	retry             widget.Clickable
	editBtn, eduClose widget.Clickable
	emptyBtn          widget.Clickable
	w                 watcher
}

// NewDrafts builds the Drafts & sent view.
func NewDrafts(h Host) *Drafts {
	d := &Drafts{
		base:      newBase(h),
		tabs:      newTabs(draftsTabLabels...),
		rows:      map[string]*draftsRow{},
		sentConvs: map[string]core.ConvRef{},
		dirty:     true,
	}
	for i := range d.lists {
		d.lists[i].Axis = layout.Vertical
	}
	return d
}

// Layout draws the header, the tabs and the selected tab's content.
func (d *Drafts) Layout(gtx C) D {
	d.frame()
	d.update(gtx)
	size := gtx.Constraints.Max
	gtx.Constraints = layout.Exact(size)
	fill(gtx, image.Rectangle{Max: size}, d.p.Bg)
	return col(gtx,
		layout.Rigid(d.layoutHeader),
		layout.Rigid(d.layoutTabs),
		layout.Rigid(func(gtx C) D { return hline(gtx, d.p.Border) }),
		layout.Flexed(1, func(gtx C) D {
			fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, d.p.ReadFill)
			return d.layoutList(gtx)
		}),
	)
}

// layoutHeader: h 49, "Drafts & sent" 18/24 black #1d1c1d at 18px, and
// the "Edit" button on Drafts when there are drafts.
func (d *Drafts) layoutHeader(gtx C) D {
	w, h := gtx.Constraints.Max.X, gtx.Dp(draftsHeadH)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, h))
	layout.Inset{Left: 18, Right: 28}.Layout(g, func(gtx C) D {
		return layout.W.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.Y = 0
			return row(gtx, layout.Middle,
				layout.Flexed(1, func(gtx C) D { return d.para(gtx, 18, 24, wBlack, d.p.Text, 1, "Drafts & sent") }),
				layout.Rigid(func(gtx C) D {
					if d.tabs.Sel != draftsTabDrafts || len(d.drafts) == 0 {
						return D{}
					}
					return d.editButton(gtx)
				}),
			)
		})
	})
	return D{Size: image.Pt(w, h)}
}

// editButton: outline small, h 28, radius 4, pencil + 13px bold "Edit".
func (d *Drafts) editButton(gtx C) D {
	c := &d.editBtn
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		m := op.Record(gtx.Ops)
		dims := layout.Inset{Left: 8, Right: 10}.Layout(gtx, func(gtx C) D {
			g := gtx
			g.Constraints.Min.Y, g.Constraints.Max.Y = gtx.Dp(28), gtx.Dp(28)
			return layout.W.Layout(g, func(gtx C) D {
				gtx.Constraints.Min.Y = 0
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D { return d.icon(gtx, "edit", 16, d.p.Text) }),
					hgap(4),
					layout.Rigid(func(gtx C) D { return d.label(gtx, 13, wBold, d.p.Text, "Edit") }),
				)
			})
		})
		call := m.Stop()
		r := image.Rectangle{Max: dims.Size}
		rad := gtx.Dp(4)
		rrect(gtx, r, rad, d.p.Bg)
		if c.Hovered() {
			rrect(gtx, r, rad, d.p.Hover)
		}
		border(gtx, r, rad, max(1, gtx.Dp(1)), d.p.BorderStr)
		call.Add(gtx.Ops)
		return dims
	})
}

// layoutTabs: tabs padded 8px 0 with margins 0 20 0 8 (the last 0 8),
// 13/16 bold, an optional 13px regular count 4px after the label, and a
// 2px accent underline on the selected tab. Strip height 32.
func (d *Drafts) layoutTabs(gtx C) D {
	t := d.tabs
	counts := [3]int{len(d.drafts), len(d.scheduled), 0}
	ch := make([]layout.FlexChild, 0, len(t.Labels))
	for i := range t.Labels {
		i := i
		right := unit.Dp(20)
		if i == len(t.Labels)-1 {
			right = 8
		}
		ch = append(ch, layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 8, Right: right}.Layout(gtx, func(gtx C) D {
				return t.clicks[i].Layout(gtx, func(gtx C) D {
					pointerCursor(gtx)
					sel := i == t.Sel
					fg := d.p.Sec
					if sel || t.clicks[i].Hovered() {
						fg = d.p.Text
					}
					dims := layout.Inset{Top: 8, Bottom: 8}.Layout(gtx, func(gtx C) D {
						return row(gtx, layout.Baseline,
							layout.Rigid(func(gtx C) D { return d.para(gtx, 13, 16, wBold, fg, 1, t.Labels[i]) }),
							layout.Rigid(func(gtx C) D {
								if counts[i] <= 0 {
									return D{}
								}
								return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D {
									return d.para(gtx, 13, 16, wReg, d.p.Sec, 1, strconv.Itoa(counts[i]))
								})
							}),
						)
					})
					if sel {
						h := gtx.Dp(2)
						fill(gtx, image.Rect(0, dims.Size.Y-h, dims.Size.X, dims.Size.Y), d.p.TabLine)
					}
					return dims
				})
			})
		}))
	}
	return layout.Inset{Left: 10}.Layout(gtx, func(gtx C) D { return row(gtx, layout.End, ch...) })
}

// ---- state ----

func (d *Drafts) update(gtx C) {
	sel := d.tabs.Sel
	if !d.watching {
		d.watching = true
		self := d.selfID()
		d.w.watch(d.h, func(ev core.Event) bool {
			return ev.Type == "resync" || (ev.Type == "message" && self != "" && ev.UserID == self)
		})
	}
	switch {
	case !d.started(sel):
		d.reload(sel)
	case !d.last.IsZero() && gtx.Now.Sub(d.last) > 10*time.Second:
		d.reload(sel) // shown again after a while
	}
	d.last = gtx.Now
	if d.tabs.update(gtx) {
		sel = d.tabs.Sel
		d.reload(sel)
		d.dirty = true
	}
	if d.w.due(gtx, 2*time.Second) {
		d.reload(sel)
	}
	d.apply()
	if d.retry.Clicked(gtx) {
		d.reload(sel)
	}
	if d.eduClose.Clicked(gtx) {
		d.eduHidden, d.dirty = true, true
	}
	if d.editBtn.Clicked(gtx) || d.emptyBtn.Clicked(gtx) {
		d.h.Toast("Not supported yet")
	}
	for k, r := range d.rows {
		click, edit := r.click.Clicked(gtx), r.edit.Clicked(gtx)
		del, sched, send := r.del.Clicked(gtx), r.sched.Clicked(gtx), r.send.Clicked(gtx)
		if del || sched || send {
			// Core has no drafts.delete / schedule / send-draft yet.
			d.h.Toast("Not supported yet")
			continue
		}
		if !click && !edit {
			continue
		}
		kind, id, _ := strings.Cut(k, ":")
		switch kind {
		case "d":
			if dr := d.findDraft(id); dr != nil {
				d.openDraft(dr)
			}
		case "s":
			if m := d.findSent(id); m != nil {
				d.openMsg(m)
			}
		}
	}
	if d.dirty || d.viewTab != sel {
		d.rebuild(sel)
	}
}

func (d *Drafts) started(tab int) bool {
	if tab == draftsTabSent {
		return d.sent.started()
	}
	return d.dl.started()
}

func (d *Drafts) reload(tab int) {
	d.seq++
	seq := d.seq
	c := d.h.Core()
	if tab != draftsTabSent {
		d.dl.load(d.h, true, func(ctx context.Context) (draftsResult, error) {
			l, err := c.Drafts(ctx)
			return draftsResult{list: l, seq: seq}, err
		})
		return
	}
	q := "from:<@" + d.selfID() + ">"
	d.sent.load(d.h, true, func(ctx context.Context) (sentResult, error) {
		r, err := c.SearchMessages(ctx, core.SearchOpts{Query: q, Sort: "timestamp", Count: draftsSentN, Page: 1})
		return sentResult{res: r, seq: seq, page: 1}, err
	})
}

func (d *Drafts) loadMoreSent() {
	next := d.sentPage + 1
	if d.sentPage == 0 || next > d.sentMax || next == d.sentTried || d.sentMore.busy() || d.sent.busy() {
		return
	}
	d.sentTried = next
	d.seq++
	seq := d.seq
	c := d.h.Core()
	q := "from:<@" + d.selfID() + ">"
	d.sentMore.load(d.h, true, func(ctx context.Context) (sentResult, error) {
		r, err := c.SearchMessages(ctx, core.SearchOpts{Query: q, Sort: "timestamp", Count: draftsSentN, Page: next})
		return sentResult{res: r, seq: seq, page: next}, err
	})
}

func (d *Drafts) apply() {
	if v, ok, _ := d.dl.get(); ok && v.seq != 0 && v.seq != d.dlApplied {
		d.dlApplied, d.dirty = v.seq, true
		d.drafts, d.scheduled = d.drafts[:0:0], d.scheduled[:0:0]
		for _, dr := range v.list {
			if dr.DateScheduled > 0 {
				d.scheduled = append(d.scheduled, dr)
			} else {
				d.drafts = append(d.drafts, dr)
			}
		}
		sort.SliceStable(d.drafts, func(i, j int) bool { return draftTime(&d.drafts[i]).After(draftTime(&d.drafts[j])) })
		sort.SliceStable(d.scheduled, func(i, j int) bool { return d.scheduled[i].DateScheduled < d.scheduled[j].DateScheduled })
	}
	if v, ok, _ := d.sent.get(); ok && v.seq != 0 && v.seq != d.sentApplied {
		d.sentApplied, d.dirty = v.seq, true
		d.sentMsgs = append([]core.Message(nil), v.res.Messages...)
		d.sentConvs = map[string]core.ConvRef{}
		for id, r := range v.res.Convs {
			d.sentConvs[id] = r
		}
		d.sentPage, d.sentMax, d.sentTried = 1, v.res.Pages, 0
	}
	if v, ok, _ := d.sentMore.get(); ok && v.seq != 0 && v.seq != d.sentMoreApplied {
		d.sentMoreApplied = v.seq
		if v.page == d.sentPage+1 {
			have := make(map[string]bool, len(d.sentMsgs))
			for i := range d.sentMsgs {
				have[d.sentMsgs[i].Channel+"/"+d.sentMsgs[i].TS] = true
			}
			for _, m := range v.res.Messages {
				if !have[m.Channel+"/"+m.TS] {
					d.sentMsgs = append(d.sentMsgs, m)
				}
			}
			for id, r := range v.res.Convs {
				d.sentConvs[id] = r
			}
			d.sentPage, d.dirty = v.page, true
		}
	}
	if v, ok, err := d.open.get(); ok && v.seq != 0 && v.seq != d.openApplied {
		d.openApplied = v.seq
		if err != nil {
			d.h.Toast("Couldn’t open that conversation: " + friendlyErr(err))
		} else if v.conv.ID != "" {
			d.h.Navigate("c/" + v.conv.ID)
		}
	}
}

// rebuild lays out the element list: education card, gap, then day
// groups (Sent) or cards (Drafts, Scheduled), then bottom padding.
func (d *Drafts) rebuild(tab int) {
	d.dirty, d.viewTab = false, tab
	d.view = d.view[:0]
	n := d.tabLen(tab)
	if n == 0 {
		return
	}
	if !d.eduHidden {
		d.view = append(d.view, draftsEntry{kind: draftsEntryCard})
	}
	d.view = append(d.view, draftsEntry{kind: draftsEntryGap})
	if tab != draftsTabSent {
		for i := 0; i < n; i++ {
			d.view = append(d.view, draftsEntry{kind: draftsEntryDraft, i: i})
		}
	} else {
		day := ""
		for i := range d.sentMsgs {
			t := tsTime(d.sentMsgs[i].TS)
			lbl := draftsDay(t)
			if lbl != day {
				if len(d.view) > 0 && d.view[len(d.view)-1].kind == draftsEntrySent {
					d.view[len(d.view)-1].last = true
				}
				d.view = append(d.view, draftsEntry{kind: draftsEntryDay, label: lbl})
				day = lbl
				d.view = append(d.view, draftsEntry{kind: draftsEntrySent, i: i, first: true})
				continue
			}
			d.view = append(d.view, draftsEntry{kind: draftsEntrySent, i: i})
		}
		if len(d.view) > 0 && d.view[len(d.view)-1].kind == draftsEntrySent {
			d.view[len(d.view)-1].last = true
		}
	}
	d.view = append(d.view, draftsEntry{kind: draftsEntryEnd})
}

func (d *Drafts) tabLen(tab int) int {
	switch tab {
	case draftsTabSent:
		return len(d.sentMsgs)
	case draftsTabScheduled:
		return len(d.scheduled)
	}
	return len(d.drafts)
}

// draftsDay is the Sent day divider label: Today, Yesterday, or
// "Monday, November 3rd".
func draftsDay(t time.Time) string {
	now := time.Now()
	switch {
	case t.IsZero():
		return ""
	case sameDay(t, now):
		return "Today"
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "Yesterday"
	}
	return t.Format("Monday") + ", " + longDate(t)
}

// draftTime is when a draft was last edited.
func draftTime(dr *core.Draft) time.Time {
	if t := tsTime(dr.LastUpdated); !t.IsZero() {
		return t
	}
	return unixTime(dr.DateCreated)
}

func (d *Drafts) findDraft(id string) *core.Draft {
	for _, l := range [][]core.Draft{d.drafts, d.scheduled} {
		for i := range l {
			if l[i].ID == id {
				return &l[i]
			}
		}
	}
	return nil
}

func (d *Drafts) findSent(key string) *core.Message {
	for i := range d.sentMsgs {
		if d.sentMsgs[i].Channel+"/"+d.sentMsgs[i].TS == key {
			return &d.sentMsgs[i]
		}
	}
	return nil
}

// openDraft goes to where the draft will be sent, where the composer
// holds it: the thread, the conversation, or a DM opened on demand.
func (d *Drafts) openDraft(dr *core.Draft) {
	switch {
	case dr.Channel != "" && dr.ThreadTS != "":
		d.h.OpenThread(dr.Channel, dr.ThreadTS)
	case dr.Channel != "":
		d.h.Navigate("c/" + dr.Channel)
	case len(dr.Users) > 0 && !d.open.busy():
		users := append([]string(nil), dr.Users...)
		d.seq++
		seq := d.seq
		c := d.h.Core()
		d.open.load(d.h, false, func(ctx context.Context) (draftsOpened, error) {
			cv, err := c.OpenDM(ctx, users...)
			return draftsOpened{conv: cv, seq: seq}, err
		})
	}
}

func (d *Drafts) openMsg(m *core.Message) {
	if m.ThreadTS != "" && m.ThreadTS != m.TS {
		d.h.OpenThread(m.Channel, m.ThreadTS)
		return
	}
	d.h.Navigate("c/" + m.Channel)
}

func (d *Drafts) rowState(k string) *draftsRow {
	r, ok := d.rows[k]
	if !ok {
		r = new(draftsRow)
		d.rows[k] = r
	}
	return r
}

// ---- list ----

func (d *Drafts) layoutList(gtx C) D {
	sel := d.tabs.Sel
	var (
		loaded bool
		err    error
	)
	if sel == draftsTabSent {
		_, loaded, err = d.sent.get()
	} else {
		_, loaded, err = d.dl.get()
	}
	n := d.tabLen(sel)
	switch {
	case n == 0 && !loaded:
		return layout.Inset{Top: 16, Left: draftsMargin, Right: draftsMargin}.Layout(gtx, func(gtx C) D {
			return d.skeleton(gtx, 6, draftsTile)
		})
	case n == 0 && err != nil:
		return d.errorState(gtx, err, &d.retry)
	case n == 0:
		return d.emptyFor(gtx, sel)
	}
	ne := len(d.view)
	extra := 0
	if sel == draftsTabSent && d.sentMore.busy() {
		extra = 1
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	l := &d.lists[sel]
	dims := d.scrollList(gtx, l, ne+extra, func(gtx C, i int) D {
		if i >= ne {
			return d.skeleton(gtx, 1, draftsTile)
		}
		e := d.view[i]
		switch e.kind {
		case draftsEntryCard:
			return d.eduCard(gtx)
		case draftsEntryGap:
			return D{Size: image.Pt(gtx.Constraints.Max.X, gtx.Dp(16))}
		case draftsEntryEnd:
			return D{Size: image.Pt(gtx.Constraints.Max.X, gtx.Dp(draftsMargin))}
		case draftsEntryDay:
			return layout.Inset{Top: 24, Bottom: 12, Left: draftsMargin + 16, Right: draftsMargin + 16}.Layout(gtx, func(gtx C) D {
				return d.para(gtx, 13, 18, wBold, d.p.Text, 1, e.label)
			})
		case draftsEntrySent:
			return d.sentRow(gtx, &d.sentMsgs[e.i], e.first, e.last)
		default:
			if sel == draftsTabScheduled {
				return d.draftCard(gtx, &d.scheduled[e.i], true)
			}
			return d.draftCard(gtx, &d.drafts[e.i], false)
		}
	})
	if sel == draftsTabSent {
		if p := l.Position; p.First+p.Count >= ne-3 {
			d.loadMoreSent()
		}
	}
	return dims
}

// emptyFor: the spec's copy, with a 64px accent icon instead of Slack's
// illustration, then an outline medium button.
func (d *Drafts) emptyFor(gtx C, tab int) D {
	icon, title, body, btn := "edit", "Draft messages to send when you’re ready",
		"Start typing a message anywhere, then find it here. Re-read, revise, and send whenever you’d like.", "New Message"
	switch tab {
	case draftsTabScheduled:
		icon, title, body, btn = "clock", "Write now, send later",
			"Schedule messages to be sent at a later time, or another day altogether. They’ll wait here until they’re delivered.", "Start New Message"
	case draftsTabSent:
		icon, title, body, btn = "send", "Nothing sent yet", "Messages you send will show up here.", ""
	}
	return layout.Center.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(420))
		gtx.Constraints.Min = image.Point{}
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return d.icon(gtx, icon, 64, d.p.Accent) }),
			vgap(24),
			layout.Rigid(func(gtx C) D {
				return d.th.Layout(gtx, theme.TextStyle{Size: 18, LineHeight: 24, Weight: wBlack, Color: d.p.Text, Align: text.Middle}, title)
			}),
			vgap(8),
			layout.Rigid(func(gtx C) D {
				return d.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: d.p.Text, Align: text.Middle}, body)
			}),
			layout.Rigid(func(gtx C) D {
				if btn == "" {
					return D{}
				}
				return layout.Inset{Top: 16}.Layout(gtx, func(gtx C) D {
					return d.todoOutline(gtx, &d.emptyBtn, "", btn, 36, 12, 15, d.p.BorderStr, false)
				})
			}),
		)
	})
}

// eduCard: the dismissible "All your outgoing messages" card: margin 20
// 20 0, h 139, #f4ede4, radius 12, padding 20 0 24 24, close "x" at the
// top-right. (Slack's illustration is left out.)
func (d *Drafts) eduCard(gtx C) D {
	return layout.Inset{Top: draftsMargin, Left: draftsMargin, Right: draftsMargin}.Layout(gtx, func(gtx C) D {
		w, h := gtx.Constraints.Max.X, gtx.Dp(139)
		bg := rgb(0xf4ede4)
		if d.th != nil && d.th.Dark {
			bg = rgb(0x2f2a24)
		}
		rrect(gtx, image.Rect(0, 0, w, h), gtx.Dp(12), bg)
		g := gtx
		g.Constraints = layout.Exact(image.Pt(w, h))
		layout.Inset{Top: 20, Left: 24, Right: 64, Bottom: 24}.Layout(g, func(gtx C) D {
			gtx.Constraints.Min = image.Point{}
			return col(gtx,
				layout.Rigid(func(gtx C) D {
					return d.para(gtx, 18, 24, wBlack, d.p.Text, 1, "All your outgoing messages")
				}),
				vgap(4),
				layout.Rigid(func(gtx C) D {
					return d.para(gtx, 15, 22, wReg, d.p.Text, 2, "Everything you send, draft, and schedule can now be found here.")
				}),
			)
		})
		st := opOffset(gtx, image.Pt(w-gtx.Dp(16)-gtx.Dp(28), gtx.Dp(16)))
		d.iconButton(gtx, &d.eduClose, "close", 28, 20, d.p.Sec)
		st.Pop()
		return D{Size: image.Pt(w, h)}
	})
}

// draftDest describes where a message goes: "Thread in " prefix, the
// inline channel glyph, the name, and the tile to show.
type draftDest struct {
	prefix  string
	glyph   string // "hash" / "lock" before the name; "" for people
	name    string
	avatars []string
}

func (d *Drafts) destOf(ch string, users []string, threadTS string, refs map[string]core.ConvRef) draftDest {
	var dst draftDest
	switch {
	case ch != "":
		var r core.ConvRef
		if rr, ok := refs[ch]; ok {
			r = rr
		} else if c, ok := d.h.Core().Conv(ch); ok {
			r = core.ConvRef{ID: c.ID, Name: c.Name, Kind: c.Kind, User: c.User}
		} else {
			r = d.h.Core().ConvRef(ch)
		}
		switch r.Kind {
		case "im":
			u, _ := d.user(r.User)
			dst.name, dst.avatars = d.refTitle(r), []string{u.Avatar}
		case "mpim":
			dst.name = d.refTitle(r)
			if c, ok := d.h.Core().Conv(ch); ok {
				self := d.selfID()
				for _, id := range c.Members {
					if id != self && len(dst.avatars) < 2 {
						u, _ := d.user(id)
						dst.avatars = append(dst.avatars, u.Avatar)
					}
				}
			}
			for len(dst.avatars) == 1 {
				dst.avatars = append(dst.avatars, "")
			}
		default:
			dst.glyph, dst.name = "hash", r.Name
			if r.Kind == "private" {
				dst.glyph = "lock"
			}
			if dst.name == "" {
				dst.name = ch
			}
		}
	case len(users) > 0:
		names := make([]string, 0, len(users))
		for _, id := range users {
			u, _ := d.user(id)
			names = append(names, d.userName(id))
			if len(dst.avatars) < 2 {
				dst.avatars = append(dst.avatars, u.Avatar)
			}
		}
		dst.name = strings.Join(names, ", ")
	default:
		dst.glyph, dst.name = "hash", "Draft"
	}
	if threadTS != "" {
		dst.prefix = "Thread in "
	}
	return dst
}

// destTile: 36px tile #dddddd@.5, radius 8, with the channel glyph, or
// the recipient's avatar(s).
func (d *Drafts) destTile(gtx C, dst draftDest) D {
	switch {
	case len(dst.avatars) >= 2:
		return d.groupAvatar(gtx, dst.avatars[0], dst.avatars[1], draftsTile)
	case len(dst.avatars) == 1:
		return d.avatar(gtx, dst.avatars[0], draftsTile)
	}
	s := gtx.Dp(draftsTile)
	rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(8), argb(0x80dddddd))
	g := gtx
	g.Constraints = layout.Exact(image.Pt(s, s))
	glyph := dst.glyph
	if glyph == "" {
		glyph = "hash"
	}
	layout.Center.Layout(g, func(gtx C) D { return d.icon(gtx, glyph, 16, d.p.Text) })
	return D{Size: image.Pt(s, s)}
}

// destLine: 15px/22px bold "Thread in # name" with a 15px inline glyph
// (2px gap), truncated.
func (d *Drafts) destLine(gtx C, dst draftDest) D {
	return row(gtx, layout.Middle,
		layout.Rigid(func(gtx C) D {
			if dst.prefix == "" {
				return D{}
			}
			return d.para(gtx, 15, 22, wBold, d.p.Text, 1, dst.prefix)
		}),
		layout.Rigid(func(gtx C) D {
			if dst.glyph == "" {
				return D{}
			}
			return layout.Inset{Right: 2}.Layout(gtx, func(gtx C) D { return d.icon(gtx, dst.glyph, 15, d.p.Text) })
		}),
		layout.Flexed(1, func(gtx C) D { return d.para(gtx, 15, 22, wBold, d.p.Text, 1, dst.name) }),
	)
}

// rowContent: tile, 12px gap, destination line with the time (13px,
// text@.7) at the right, then a one-line 15px preview in text@.7.
func (d *Drafts) rowContent(gtx C, dst draftDest, when, prev string) D {
	soft := d.p.Text
	soft.A = 0xb3
	return row(gtx, layout.Start,
		layout.Rigid(func(gtx C) D { return d.destTile(gtx, dst) }),
		hgap(draftsGap),
		layout.Flexed(1, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return col(gtx,
				layout.Rigid(func(gtx C) D {
					return row(gtx, layout.Middle,
						layout.Flexed(1, func(gtx C) D { return d.destLine(gtx, dst) }),
						layout.Rigid(func(gtx C) D {
							if when == "" {
								return D{}
							}
							return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return d.para(gtx, 13, 18, wReg, soft, 1, when) })
						}),
					)
				}),
				layout.Rigid(func(gtx C) D { return d.para(gtx, 15, 22, wReg, soft, 1, prev) }),
			)
		}),
	)
}

// sentRow: a joined-list row, h 68, padding 12 16, white with a 1px
// #000@.1 frame; the first and last rows of a day round their outer
// corners (12px). Hover fills #f8f8f8. No action bar.
func (d *Drafts) sentRow(gtx C, m *core.Message, first, last bool) D {
	r := d.rowState("s:" + m.Channel + "/" + m.TS)
	thread := ""
	if m.ThreadTS != "" && m.ThreadTS != m.TS {
		thread = m.ThreadTS
	}
	dst := d.destOf(m.Channel, nil, thread, d.sentConvs)
	when := clock(tsTime(m.TS))
	prev := preview(m)
	return layout.Inset{Left: draftsMargin, Right: draftsMargin}.Layout(gtx, func(gtx C) D {
		return r.click.Layout(gtx, func(gtx C) D {
			pointerCursor(gtx)
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			rec := op.Record(gtx.Ops)
			dims := layout.Inset{Top: 12, Bottom: 12, Left: 16, Right: 16}.Layout(gtx, func(gtx C) D {
				return d.rowContent(gtx, dst, when, prev)
			})
			call := rec.Stop()
			rad := gtx.Dp(12)
			rr := clip.RRect{Rect: image.Rectangle{Max: dims.Size}}
			if first {
				rr.NW, rr.NE = rad, rad
			}
			if last {
				rr.SW, rr.SE = rad, rad
			}
			bg := d.p.Bg
			if r.click.Hovered() {
				bg = d.p.ReadFill
			}
			paint.FillShape(gtx.Ops, bg, rr.Op(gtx.Ops))
			paint.FillShape(gtx.Ops, argb(0x1a000000), clip.Stroke{Path: rr.Path(gtx.Ops), Width: float32(max(1, gtx.Dp(1)))}.Op())
			call.Add(gtx.Ops)
			return dims
		})
	})
}

// draftCard: its own card (h 68, padding 12, white, radius 12, 1px ring,
// a soft shadow on hover) with the hover bar inset 12px at the top-right;
// the time hides while the bar shows. Scheduled cards show "Send … at …"
// in place of the time.
func (d *Drafts) draftCard(gtx C, dr *core.Draft, scheduled bool) D {
	r := d.rowState("d:" + dr.ID)
	dst := d.destOf(dr.Channel, dr.Users, dr.ThreadTS, nil)
	when := listTime(draftTime(dr))
	if scheduled {
		when = draftsSendText(unixTime(dr.DateScheduled))
	}
	prev := oneLine(richtext.PlainText(dr.Doc))
	if prev == "" && dr.Files > 0 {
		prev = plural(dr.Files, "file")
	}
	return d.dlHoverRow(gtx, &r.dlRowState, dlRowOpts{
		Content: func(gtx C) D {
			return layout.Inset{Left: draftsMargin, Right: draftsMargin}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				t := when
				if r.hovered {
					t = ""
				}
				m := op.Record(gtx.Ops)
				dims := layout.UniformInset(12).Layout(gtx, func(gtx C) D { return d.rowContent(gtx, dst, t, prev) })
				call := m.Stop()
				rect := image.Rectangle{Max: dims.Size}
				rad := gtx.Dp(12)
				if r.hovered {
					theme.Shadow(gtx, rect, rad, gtx.Dp(3), argb(0x14000000))
				}
				rrect(gtx, rect, rad, d.p.Bg)
				border(gtx, rect, rad, max(1, gtx.Dp(1)), argb(0x211d1c1d))
				call.Add(gtx.Ops)
				return dims
			})
		},
		Below: func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Max.X, gtx.Dp(8))} },
		Bar: func(gtx C) D {
			ib := func(c *widget.Clickable, icon string) layout.FlexChild {
				return layout.Rigid(func(gtx C) D { return d.iconButton(gtx, c, icon, 32, 18, d.p.Sec) })
			}
			return d.draftsBar(gtx, ib(&r.del, "trash"), ib(&r.edit, "edit"), ib(&r.sched, "clock"), ib(&r.send, "send"))
		},
		BarTop:   12,
		BarRight: draftsMargin + 12,
	})
}

// draftsBar: the drafts hover bar: white, radius 4, padding 2, 1px ring
// and a soft 0 1px 3px shadow.
func (d *Drafts) draftsBar(gtx C, btns ...layout.FlexChild) D {
	m := op.Record(gtx.Ops)
	dims := layout.UniformInset(2).Layout(gtx, func(gtx C) D { return row(gtx, layout.Middle, btns...) })
	call := m.Stop()
	r := image.Rectangle{Max: dims.Size}
	rad := gtx.Dp(4)
	theme.Shadow(gtx, r, rad, gtx.Dp(3), argb(0x14000000))
	rrect(gtx, r, rad, d.p.Surface)
	border(gtx, r, rad, max(1, gtx.Dp(1)), argb(0x211d1c1d))
	call.Add(gtx.Ops)
	return dims
}

// draftsSendText formats a scheduled time: "Send today at 3:00 PM",
// "Send tomorrow at 9:00 AM", "Send Monday, November 10 at 9:00 AM".
func draftsSendText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	now := time.Now()
	switch {
	case sameDay(t, now):
		return "Send today at " + clock(t)
	case sameDay(t, now.AddDate(0, 0, 1)):
		return "Send tomorrow at " + clock(t)
	case t.Year() == now.Year():
		return "Send " + t.Format("Monday, January 2") + " at " + clock(t)
	}
	return "Send " + t.Format("January 2, 2006") + " at " + clock(t)
}
