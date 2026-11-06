package views

// Drafts & sent: unsent drafts, scheduled messages (drafts with a
// date_scheduled) and the viewer's sent messages (a search for
// from:<@self>, since core has no sent list).

import (
	"context"
	"image"
	"sort"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/richview"
)

const draftsTitle = "Drafts & sent"

var draftsTabLabels = []string{"Drafts", "Scheduled", "Sent"}

const (
	draftsTabDrafts = iota
	draftsTabScheduled
	draftsTabSent
)

const (
	draftsAvatar unit.Dp = 36
	draftsAvGap  unit.Dp = 8
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
	edit, more widget.Clickable
}

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

	lists [3]widget.List
	rows  map[string]*draftsRow
	retry widget.Clickable
	w     watcher
}

// NewDrafts builds the Drafts & sent view.
func NewDrafts(h Host) *Drafts {
	d := &Drafts{
		base:      newBase(h),
		tabs:      newTabs(draftsTabLabels...),
		rows:      map[string]*draftsRow{},
		sentConvs: map[string]core.ConvRef{},
	}
	for i := range d.lists {
		d.lists[i].Axis = layout.Vertical
	}
	return d
}

// Layout draws the header, tabs and the selected tab's list.
func (d *Drafts) Layout(gtx C) D {
	d.frame()
	d.update(gtx)
	size := gtx.Constraints.Max
	gtx.Constraints = layout.Exact(size)
	fill(gtx, image.Rectangle{Max: size}, d.p.Bg)
	return col(gtx,
		layout.Rigid(func(gtx C) D { return d.header(gtx, draftsTitle) }),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 16, Right: 16}.Layout(gtx, func(gtx C) D { return d.tabStrip(gtx, d.tabs, nil) })
		}),
		layout.Rigid(func(gtx C) D { return hline(gtx, d.p.Border) }),
		layout.Flexed(1, d.layoutList),
	)
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
	}
	if d.w.due(gtx, 2*time.Second) {
		d.reload(sel)
	}
	d.apply()
	if d.retry.Clicked(gtx) {
		d.reload(sel)
	}
	for k, r := range d.rows {
		click, edit, more := r.click.Clicked(gtx), r.edit.Clicked(gtx), r.more.Clicked(gtx)
		if !click && !edit && !more {
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
				if more {
					d.h.OpenMessageMenu(image.Point{}, *m)
				} else {
					d.openMsg(m)
				}
			}
		}
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
		d.dlApplied = v.seq
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
		d.sentApplied = v.seq
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
			d.sentPage = v.page
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
		n      int
		loaded bool
		err    error
	)
	if sel == draftsTabSent {
		_, loaded, err = d.sent.get()
		n = len(d.sentMsgs)
	} else {
		_, loaded, err = d.dl.get()
		n = len(d.drafts)
		if sel == draftsTabScheduled {
			n = len(d.scheduled)
		}
	}
	switch {
	case n == 0 && !loaded:
		return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D { return d.skeleton(gtx, 6, draftsAvatar) })
	case n == 0 && err != nil:
		return d.errorState(gtx, err, &d.retry)
	case n == 0:
		return d.emptyFor(gtx, sel)
	}
	extra := 0
	if sel == draftsTabSent && d.sentMore.busy() {
		extra = 1
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	l := &d.lists[sel]
	dims := d.scrollList(gtx, l, n+extra, func(gtx C, i int) D {
		switch {
		case i >= n:
			return d.skeleton(gtx, 1, draftsAvatar)
		case sel == draftsTabSent:
			return d.sentRow(gtx, &d.sentMsgs[i])
		case sel == draftsTabScheduled:
			return d.draftRow(gtx, &d.scheduled[i], true)
		default:
			return d.draftRow(gtx, &d.drafts[i], false)
		}
	})
	if sel == draftsTabSent {
		if p := l.Position; p.First+p.Count >= n-3 {
			d.loadMoreSent()
		}
	}
	return dims
}

func (d *Drafts) emptyFor(gtx C, tab int) D {
	switch tab {
	case draftsTabScheduled:
		return d.emptyState(gtx, "schedule", "No scheduled messages",
			"Schedule a message to send it at a better time. It’ll wait here until it’s sent.", nil, "")
	case draftsTabSent:
		return d.emptyState(gtx, "send", "No sent messages",
			"Messages you send will show up here.", nil, "")
	}
	return d.emptyState(gtx, "drafts", "No drafts",
		"Messages you start but don’t send are saved here as drafts.", nil, "")
}

// draftDest describes where a draft goes: its title line, the avatar or
// tile to show and whether it is a thread reply.
type draftDest struct {
	title   string
	icon    string // channel tile icon when there is no avatar
	avatars []string
	isDM    bool
}

func (d *Drafts) destOf(ch string, users []string, threadTS string, refs map[string]core.ConvRef) draftDest {
	var dst draftDest
	switch {
	case ch != "":
		dst.title = d.convTitle(ch, refs)
		kind, partner := "", ""
		var members []string
		if r, ok := refs[ch]; ok {
			kind, partner = r.Kind, r.User
		}
		if c, ok := d.h.Core().Conv(ch); ok {
			kind, partner, members = c.Kind, c.User, c.Members
		} else if kind == "" {
			r := d.h.Core().ConvRef(ch)
			kind, partner = r.Kind, r.User
		}
		switch kind {
		case "im":
			dst.isDM = true
			u, _ := d.user(partner)
			dst.avatars = []string{u.Avatar}
		case "mpim":
			dst.isDM = true
			self := d.selfID()
			for _, id := range members {
				if id != self && len(dst.avatars) < 2 {
					u, _ := d.user(id)
					dst.avatars = append(dst.avatars, u.Avatar)
				}
			}
		case "private":
			dst.icon = "lock"
		default:
			dst.icon = "hash"
		}
	case len(users) > 0:
		dst.isDM = true
		names := make([]string, 0, len(users))
		for _, id := range users {
			u, _ := d.user(id)
			names = append(names, d.userName(id))
			if len(dst.avatars) < 2 {
				dst.avatars = append(dst.avatars, u.Avatar)
			}
		}
		dst.title = strings.Join(names, ", ")
	default:
		dst.title, dst.icon = "Draft", "drafts"
	}
	if threadTS != "" {
		dst.title = "Thread in " + dst.title
	}
	return dst
}

func (d *Drafts) destTile(gtx C, dst draftDest) D {
	switch {
	case len(dst.avatars) >= 2:
		return d.groupAvatar(gtx, dst.avatars[0], dst.avatars[1], draftsAvatar)
	case len(dst.avatars) == 1:
		return d.avatar(gtx, dst.avatars[0], draftsAvatar)
	}
	s := gtx.Dp(draftsAvatar)
	rrect(gtx, image.Rect(0, 0, s, s), avatarRadius(gtx, draftsAvatar), d.p.Track)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(s, s))
	layout.Center.Layout(g, func(gtx C) D { return d.icon(gtx, dst.icon, 20, d.p.Sec) })
	return D{Size: image.Pt(s, s)}
}

// scheduledText formats "Scheduled for today at 3:00 PM".
func scheduledText(t time.Time) string {
	now := time.Now()
	switch {
	case sameDay(t, now):
		return "Scheduled for today at " + clock(t)
	case sameDay(t, now.AddDate(0, 0, 1)):
		return "Scheduled for tomorrow at " + clock(t)
	case t.Year() == now.Year():
		return "Scheduled for " + t.Format("Monday, January 2") + " at " + clock(t)
	}
	return "Scheduled for " + t.Format("January 2, 2006") + " at " + clock(t)
}

func (d *Drafts) draftRow(gtx C, dr *core.Draft, scheduled bool) D {
	r := d.rowState("d:" + dr.ID)
	dst := d.destOf(dr.Channel, dr.Users, dr.ThreadTS, nil)
	when := listTime(draftTime(dr))
	empty := strings.TrimSpace(richtext.PlainText(dr.Doc)) == ""
	return d.dlHoverRow(gtx, &r.dlRowState, dlRowOpts{
		HoverBg: d.p.Hover,
		Rule:    d.p.Border,
		Bar: func(gtx C) D {
			return d.dlActionBar(gtx, layout.Rigid(func(gtx C) D { return d.iconButton(gtx, &r.edit, "edit", 32, 18, d.p.Text) }))
		},
		BarTop:   8,
		BarRight: 16,
		Content: func(gtx C) D {
			return d.rowFrame(gtx, dst, when, func(gtx C) D {
				return col(gtx,
					layout.Rigid(func(gtx C) D {
						if !scheduled {
							return D{}
						}
						return layout.Inset{Bottom: 2}.Layout(gtx, func(gtx C) D {
							return row(gtx, layout.Middle,
								layout.Rigid(func(gtx C) D { return d.icon(gtx, "schedule", 14, d.p.Muted) }),
								hgap(4),
								layout.Rigid(func(gtx C) D {
									return d.label(gtx, 13, wReg, d.p.Muted, scheduledText(unixTime(dr.DateScheduled)))
								}),
							)
						})
					}),
					layout.Rigid(func(gtx C) D {
						if empty {
							t := "No text"
							if dr.Files > 0 {
								t = plural(dr.Files, "file")
							}
							return d.para(gtx, 15, 22, wReg, d.p.Muted, 1, t)
						}
						return d.richDoc(gtx, "draft/"+dr.ID, dr.LastUpdated, dr.Doc, richview.Options{}, 2)
					}),
				)
			})
		},
	})
}

func (d *Drafts) sentRow(gtx C, m *core.Message) D {
	r := d.rowState("s:" + m.Channel + "/" + m.TS)
	thread := ""
	if m.ThreadTS != "" && m.ThreadTS != m.TS {
		thread = m.ThreadTS
	}
	dst := d.destOf(m.Channel, nil, thread, d.sentConvs)
	when := listTime(tsTime(m.TS))
	return d.dlHoverRow(gtx, &r.dlRowState, dlRowOpts{
		HoverBg: d.p.Hover,
		Rule:    d.p.Border,
		Bar: func(gtx C) D {
			return d.dlActionBar(gtx, layout.Rigid(func(gtx C) D { return d.iconButton(gtx, &r.more, "more", 32, 18, d.p.Text) }))
		},
		BarTop:   8,
		BarRight: 16,
		Content: func(gtx C) D {
			return d.rowFrame(gtx, dst, when, func(gtx C) D { return d.msgBody(gtx, m, false, 2) })
		},
	})
}

// rowFrame: padding 12/20/12/16, 36px tile + 8px gap, destination 15px
// black with the time at the right (13px muted), then the body.
func (d *Drafts) rowFrame(gtx C, dst draftDest, when string, bodyW W) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return layout.Inset{Top: 12, Right: 20, Bottom: 12, Left: 16}.Layout(gtx, func(gtx C) D {
		return row(gtx, layout.Start,
			layout.Rigid(func(gtx C) D { return d.destTile(gtx, dst) }),
			hgap(draftsAvGap),
			layout.Flexed(1, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return col(gtx,
					layout.Rigid(func(gtx C) D {
						return row(gtx, layout.Baseline,
							layout.Flexed(1, func(gtx C) D { return d.para(gtx, 15, 22, wBlack, d.p.Text, 1, dst.title) }),
							layout.Rigid(func(gtx C) D {
								if when == "" {
									return D{}
								}
								return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return d.label(gtx, 13, wReg, d.p.Muted, when) })
							}),
						)
					}),
					layout.Rigid(bodyW),
				)
			}),
		)
	})
}
