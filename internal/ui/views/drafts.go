package views

// Drafts & sent (Home sidebar): unsent drafts, scheduled messages (drafts
// with a date_scheduled, the same set core.ScheduledMessages lists) and
// the viewer's sent messages (core.Sent, messages.sent.list).
//
// Writes go through core and run only on a click: save an edited draft
// (SaveDraft), delete (DeleteDraft / DeleteScheduled), send now
// (SendDraft) and schedule or reschedule (RescheduleMessage turns a draft
// into a scheduled message). Realtime "draft" events reload the lists.
//
// Measurements: /tmp/ui-views-spec/drafts.md. The header and tabs are
// white; the content below is #f8f8f8 with white cards.

import (
	"context"
	"image"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
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
)

type draftsResult struct {
	list []core.Draft
	seq  int
}

type sentResult struct {
	res    core.SentPage
	seq    int
	cursor string
}

type draftsOpened struct {
	conv core.Conv
	seq  int
}

type draftsRow struct {
	dlRowState
	del, edit, sched, send widget.Clickable
	more                   widget.Clickable // scheduled messages: the ⋮ menu
}

// draftsEdit is a draft being edited in place: the card holds the
// composer's rich editor seeded with the draft's document, with Cancel
// and Save (SaveDraft) like Slack's in-place message editing.
type draftsEdit struct {
	id           string
	ed           *composer.Editor
	st           composer.EditorStyle
	cancel, save widget.Clickable
	focused      bool
	busy         bool // a save is in flight
}

// draftsDone is a finished write, reported back to the UI goroutine.
type draftsDone struct {
	okMsg, errMsg string
	err           error
	saved         *core.Draft // SaveDraft / RescheduleMessage result
	closeEdit     string      // close the editor of this draft on success
	unhide        []string    // ids to show again on failure
}

// Schedule presets (Slack's composer menu).
const (
	draftsSchedTomorrow = iota
	draftsSchedMonday
	draftsSchedCustom
	draftsSchedN
)

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
	gone              map[string]time.Time // optimistic removals, hidden from reloads for a while

	sent, sentMore  async[sentResult]
	sentApplied     int
	sentMoreApplied int
	sentMsgs        []core.Message
	sentConvs       map[string]core.ConvRef
	sentPage        int    // pages appended (0 = none)
	sentNext        string // cursor of the next page ("" = end)
	sentTried       string // cursor last requested by loadMoreSent

	open        async[draftsOpened]
	openApplied int

	edit *draftsEdit

	schedID    string // draft whose schedule menu is open
	schedOpen  bool
	schedScrim widget.Clickable
	schedItems [draftsSchedN]widget.Clickable

	// moreID's scheduled message has its ⋮ menu open: "Cancel schedule
	// and save to drafts", "Delete message".
	moreID    string
	moreOpen  bool
	moreScrim widget.Clickable
	moreItems [2]widget.Clickable

	// Header "Edit": select drafts to delete several at once.
	selecting            bool
	selected             map[string]bool
	selCancel, selDelete widget.Clickable

	mu   sync.Mutex
	done []draftsDone

	view      []draftsEntry
	viewTab   int
	dirty     bool
	eduHidden bool

	lists             [3]widget.List
	kb                [3]*vkList // the Drafts and Scheduled lists' keyboard (vlist_keys.go)
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
		gone:      map[string]time.Time{},
		selected:  map[string]bool{},
		dirty:     true,
	}
	for i := range d.lists {
		d.lists[i].Axis = layout.Vertical
		d.kb[i] = newVKList(&d.lists[i])
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
					if d.selecting {
						// Select mode: Cancel, then Delete with the count.
						n := len(d.selectedIDs())
						label := "Delete"
						if n > 0 {
							label += " (" + strconv.Itoa(n) + ")"
						}
						return row(gtx, layout.Middle,
							layout.Rigid(func(gtx C) D { return d.button(gtx, &d.selCancel, btnOutline, true, "Cancel") }),
							hgap(8),
							layout.Rigid(func(gtx C) D {
								kind := btnDanger
								if n == 0 {
									kind = btnOutline
								}
								return d.button(gtx, &d.selDelete, kind, true, label)
							}),
						)
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
		c := d.h.Core()
		d.w.watch(d.h, func(ev core.Event) bool {
			switch ev.Type {
			case "resync", "draft":
				return true
			case "message":
				// The viewer is looked up per event: the view can be
				// built before boot knows who that is.
				return ev.UserID != "" && ev.UserID == c.Self().ID
			}
			return false
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
		d.endSelect()
		d.dirty = true
	}
	if d.w.due(gtx, 2*time.Second) {
		d.reload(d.tabs.Sel)
		if d.tabs.Sel != draftsTabSent && d.sent.started() {
			d.reload(draftsTabSent) // a send shows up there too
		}
	}
	d.apply()
	d.drainDone()
	if d.retry.Clicked(gtx) {
		d.reload(sel)
	}
	if d.eduClose.Clicked(gtx) {
		d.eduHidden, d.dirty = true, true
	}
	if d.editBtn.Clicked(gtx) {
		d.closeEdit()
		d.closeSched()
		d.selecting, d.dirty = true, true
		clear(d.selected)
	}
	if d.selCancel.Clicked(gtx) {
		d.endSelect()
	}
	if d.selDelete.Clicked(gtx) {
		d.deleteSelected()
	}
	if d.emptyBtn.Clicked(gtx) {
		// "New Message": like the shell's compose button, pick the
		// destination in the quick switcher.
		if ph, ok := d.h.(popups.Host); ok {
			ph.ShowOverlay(popups.NewQuickSwitcher(ph))
		}
	}
	d.updateEdit(gtx)
	d.updateSched(gtx)
	d.updateMore(gtx)
	for k, r := range d.rows {
		click, edit := r.click.Clicked(gtx), r.edit.Clicked(gtx)
		del, sched, send := r.del.Clicked(gtx), r.sched.Clicked(gtx), r.send.Clicked(gtx)
		more := r.more.Clicked(gtx)
		if !click && !edit && !del && !sched && !send && !more {
			continue
		}
		kind, id, _ := strings.Cut(k, ":")
		if kind == "s" {
			if m := d.findSent(id); m != nil && click {
				d.openMsg(m)
			}
			continue
		}
		dr := d.findDraft(id)
		if dr == nil {
			continue
		}
		switch {
		case d.selecting:
			if click {
				d.selected[id] = !d.selected[id]
			}
		case del:
			d.confirmDelete(dr)
		case more:
			if d.moreOpen && d.moreID == id {
				d.closeMore()
			} else {
				d.closeMore()
				d.closeSched()
				d.moreID, d.moreOpen, r.pin = id, true, true
			}
		case send:
			d.sendDraft(dr)
		case sched:
			if d.schedOpen && d.schedID == id {
				d.closeSched()
			} else {
				d.closeSched()
				d.schedID, d.schedOpen, r.pin = id, true, true
			}
		case edit:
			d.startEdit(dr)
		case click:
			if d.edit == nil || d.edit.id != id {
				d.openDraft(dr)
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
	d.sent.load(d.h, true, func(ctx context.Context) (sentResult, error) {
		r, err := c.Sent(ctx, "", 1)
		return sentResult{res: r, seq: seq}, err
	})
}

func (d *Drafts) loadMoreSent() {
	cur := d.sentNext
	if d.sentPage == 0 || cur == "" || cur == d.sentTried || d.sentMore.busy() || d.sent.busy() {
		return
	}
	d.sentTried = cur
	d.seq++
	seq, page := d.seq, d.sentPage+1
	c := d.h.Core()
	d.sentMore.load(d.h, true, func(ctx context.Context) (sentResult, error) {
		r, err := c.Sent(ctx, cur, page)
		return sentResult{res: r, seq: seq, cursor: cur}, err
	})
}

// hidden reports a draft removed optimistically in the last 15 seconds
// (drafts.list can lag behind a delete or send).
func (d *Drafts) hidden(id string) bool {
	at, ok := d.gone[id]
	if !ok {
		return false
	}
	if time.Since(at) > 15*time.Second {
		delete(d.gone, id)
		return false
	}
	return true
}

// place sorts a draft list into the Drafts and Scheduled tabs.
func (d *Drafts) place(list []core.Draft) {
	d.drafts, d.scheduled = d.drafts[:0:0], d.scheduled[:0:0]
	for _, dr := range list {
		switch {
		case d.hidden(dr.ID):
		case dr.DateScheduled > 0:
			d.scheduled = append(d.scheduled, dr)
		default:
			d.drafts = append(d.drafts, dr)
		}
	}
	sort.SliceStable(d.drafts, func(i, j int) bool { return draftTime(&d.drafts[i]).After(draftTime(&d.drafts[j])) })
	sort.SliceStable(d.scheduled, func(i, j int) bool { return d.scheduled[i].DateScheduled < d.scheduled[j].DateScheduled })
	d.dirty = true
}

func (d *Drafts) all() []core.Draft {
	return append(append([]core.Draft(nil), d.drafts...), d.scheduled...)
}

func (d *Drafts) apply() {
	if v, ok, _ := d.dl.get(); ok && v.seq != 0 && v.seq != d.dlApplied {
		d.dlApplied = v.seq
		d.place(v.list)
		if d.edit != nil && d.findDraft(d.edit.id) == nil {
			d.closeEdit() // deleted or sent elsewhere
		}
	}
	if v, ok, _ := d.sent.get(); ok && v.seq != 0 && v.seq != d.sentApplied {
		d.sentApplied, d.dirty = v.seq, true
		d.sentMsgs = append([]core.Message(nil), v.res.Messages...)
		d.sentConvs = map[string]core.ConvRef{}
		for id, r := range v.res.Convs {
			d.sentConvs[id] = r
		}
		d.sentPage, d.sentNext, d.sentTried = 1, v.res.Next, ""
	}
	if v, ok, _ := d.sentMore.get(); ok && v.seq != 0 && v.seq != d.sentMoreApplied {
		d.sentMoreApplied = v.seq
		if v.cursor == d.sentNext {
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
			d.sentPage++
			d.sentNext, d.dirty = v.res.Next, true
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

// ---- edit in place ----

// startEdit turns a draft's card into an editor seeded with the draft.
func (d *Drafts) startEdit(dr *core.Draft) {
	if d.edit != nil && d.edit.id == dr.ID {
		return
	}
	d.closeEdit()
	d.closeSched()
	doc := composer.DocFromRichtext(dr.Doc.Blocks)
	if doc == nil || doc.Len() == 0 {
		doc = composer.NewDoc()
		if t := strings.TrimSpace(dr.Text); t != "" {
			doc.Insert(0, t, composer.Attr{})
		}
	}
	e := &draftsEdit{id: dr.ID, ed: composer.NewEditor()}
	e.ed.SetDoc(doc)
	d.edit = e
	d.dirty = true
}

func (d *Drafts) closeEdit() {
	if d.edit == nil {
		return
	}
	d.edit = nil
	d.dirty = true
}

func (d *Drafts) updateEdit(gtx C) {
	e := d.edit
	if e == nil {
		return
	}
	if e.cancel.Clicked(gtx) {
		d.closeEdit()
		return
	}
	save := e.save.Clicked(gtx)
	for {
		ev, ok := e.ed.Update(gtx)
		if !ok {
			break
		}
		switch ev.Kind {
		case composer.EvSubmit:
			save = true
		case composer.EvCancel:
			d.closeEdit()
			return
		}
	}
	if save {
		d.saveEdit()
	}
}

// saveEdit writes the editor's document back to the draft (SaveDraft). A
// scheduled message keeps its send time.
func (d *Drafts) saveEdit() {
	e := d.edit
	dr := d.findDraft(e.id)
	if dr == nil || e.busy {
		return
	}
	doc := e.ed.Doc()
	if doc.Empty() {
		d.h.Toast("A draft can’t be empty. Delete it instead.")
		return
	}
	o := core.DraftOpts{ID: dr.ID, Channel: dr.Channel, Users: dr.Users, ThreadTS: dr.ThreadTS, Broadcast: dr.Broadcast,
		Text: doc.Mrkdwn(), Blocks: doc.RichTextBlocks(), FileIDs: dr.FileIDs}
	at := dr.DateScheduled
	e.busy = true
	if at > 0 && dr.Channel != "" {
		// drafts.update on a scheduled draft without date_scheduled answers
		// reattaching_not_allowed, so a scheduled message is replaced: the
		// new text is scheduled for the same time, then the old one goes.
		old := dr.ID
		d.remove(old)
		so := core.SendOpts{Channel: dr.Channel, ThreadTS: dr.ThreadTS, Broadcast: dr.Broadcast, Text: o.Text, Blocks: o.Blocks}
		d.run(draftsDone{okMsg: "Scheduled message saved", errMsg: "Couldn’t save your scheduled message", unhide: []string{old}},
			func(ctx context.Context, c *core.App) (*core.Draft, error) {
				if _, err := c.ScheduleMessage(ctx, so, at); err != nil {
					return nil, err
				}
				return nil, c.DeleteScheduled(ctx, so.Channel, old)
			})
		return
	}
	d.run(draftsDone{okMsg: "Draft saved", errMsg: "Couldn’t save your draft", closeEdit: dr.ID},
		func(ctx context.Context, c *core.App) (*core.Draft, error) {
			nd, err := c.SaveDraft(ctx, o)
			if err != nil {
				return nil, err
			}
			return &nd, nil
		})
}

func (d *Drafts) editStyle() composer.EditorStyle {
	p := &d.th.C
	st := composer.DefaultEditorStyle(d.th.Shaper)
	st.Color, st.Caret, st.LinkColor = p.Fg, p.Fg, p.Link
	st.MentionColor, st.MentionBg = p.Mention, p.MentionBg
	st.BroadcastColor, st.BroadcastBg = p.Mention, p.SelfMention
	st.CodeColor, st.CodeBg, st.CodeBorder = p.CodeFg, p.CodeBg, p.CodeBorder
	st.PreBg, st.PreBorder, st.QuoteBar = p.PreBg, p.CodeBorder, p.QuoteBar
	st.Selection, st.Placeholder = p.Selection, p.FgSubtle
	st.PlaceholderText = "Write a message"
	st.MaxHeight = 300
	if imgs := d.h.Images(); imgs != nil {
		st.Images = func(url string, px int) (paint.ImageOp, bool) {
			if url == "" {
				return paint.ImageOp{}, false
			}
			return imgs.Get(url, px)
		}
	}
	return st
}

// editCard is the card of the draft being edited: the destination line,
// then the editor in an input box (radius 8, 1px border, blue focus ring)
// and Cancel / Save at the bottom right.
func (d *Drafts) editCard(gtx C, dr *core.Draft) D {
	e := d.edit
	if e.st.Shaper != d.th.Shaper || e.st.Color != d.th.C.Fg {
		e.st = d.editStyle()
	}
	if !e.focused {
		e.focused = true
		e.ed.Focus(gtx)
	}
	dst := d.destOf(dr.Channel, dr.Users, dr.ThreadTS, nil)
	return layout.Inset{Left: draftsMargin, Right: draftsMargin, Bottom: 8}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		m := op.Record(gtx.Ops)
		dims := layout.UniformInset(12).Layout(gtx, func(gtx C) D {
			return col(gtx,
				layout.Rigid(func(gtx C) D {
					return row(gtx, layout.Middle,
						layout.Rigid(func(gtx C) D { return d.destTile(gtx, dst) }),
						hgap(draftsGap),
						layout.Flexed(1, func(gtx C) D { return d.destLine(gtx, dst) }),
						layout.Rigid(func(gtx C) D {
							if dr.DateScheduled == 0 {
								return D{}
							}
							soft := d.p.Text
							soft.A = 0xb3
							soft = flatten(soft, d.p.Bg)
							return d.para(gtx, 13, 18, wReg, soft, 1, draftsSendText(unixTime(dr.DateScheduled)))
						}),
					)
				}),
				vgap(12),
				layout.Rigid(func(gtx C) D { return d.editBox(gtx, e) }),
				vgap(8),
				layout.Rigid(func(gtx C) D {
					return layout.E.Layout(gtx, func(gtx C) D {
						return row(gtx, layout.Middle,
							layout.Rigid(func(gtx C) D { return d.button(gtx, &e.cancel, btnOutline, true, "Cancel") }),
							hgap(8),
							layout.Rigid(func(gtx C) D { return d.button(gtx, &e.save, btnPrimary, true, "Save") }),
						)
					})
				}),
			)
		})
		call := m.Stop()
		rect := image.Rectangle{Max: dims.Size}
		rad := gtx.Dp(12)
		theme.Shadow(gtx, rect, rad, gtx.Dp(3), argb(0x14000000))
		rrect(gtx, rect, rad, d.p.Bg)
		border(gtx, rect, rad, max(1, gtx.Dp(1)), d.p.Ring)
		call.Add(gtx.Ops)
		return dims
	})
}

// editBox: the input box around the editor, padding 8 12.
func (d *Drafts) editBox(gtx C, e *draftsEdit) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	m := op.Record(gtx.Ops)
	dims := layout.Inset{Top: 9, Bottom: 9, Left: 12, Right: 12}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		gtx.Constraints.Min.Y = gtx.Dp(22)
		return e.ed.Layout(gtx, e.st)
	})
	call := m.Stop()
	r := image.Rectangle{Max: dims.Size}
	rad := gtx.Dp(8)
	rrect(gtx, r, rad, d.p.Bg)
	if e.ed.Focused(gtx) {
		border(gtx, r.Inset(-gtx.Dp(2)), rad+gtx.Dp(2), gtx.Dp(3), d.p.Focus)
		border(gtx, r, rad, max(1, gtx.Dp(1)), d.p.Link)
	} else {
		border(gtx, r, rad, max(1, gtx.Dp(1)), d.p.BorderStr)
	}
	call.Add(gtx.Ops)
	return dims
}

// ---- schedule menu and custom time ----

// draftsSchedTime is a preset's send time: tomorrow or the next Monday
// after tomorrow, at 9:00 AM local time.
func draftsSchedTime(preset int, now time.Time) time.Time {
	t := time.Date(now.Year(), now.Month(), now.Day()+1, 9, 0, 0, 0, now.Location())
	if preset == draftsSchedMonday {
		for t = t.AddDate(0, 0, 1); t.Weekday() != time.Monday; t = t.AddDate(0, 0, 1) {
		}
	}
	return t
}

func (d *Drafts) closeSched() {
	if r, ok := d.rows["d:"+d.schedID]; ok {
		r.pin = false
	}
	d.schedOpen, d.schedID = false, ""
}

func (d *Drafts) updateSched(gtx C) {
	if d.schedID == "" {
		return
	}
	if !d.schedOpen { // closed by an outside click
		d.closeSched()
		return
	}
	for i := range d.schedItems {
		if !d.schedItems[i].Clicked(gtx) {
			continue
		}
		id := d.schedID
		d.closeSched()
		dr := d.findDraft(id)
		if dr == nil {
			return
		}
		if i == draftsSchedCustom {
			d.openPicker(dr)
			return
		}
		d.scheduleDraft(dr, draftsSchedTime(i, time.Now()))
		return
	}
}

// schedMenu: "Schedule message" heading (or "Reschedule message"), the
// two presets, a divider and "Custom time" (Slack's composer menu).
func (d *Drafts) schedMenu(gtx C) D {
	now := time.Now()
	head := "Schedule message"
	if dr := d.findDraft(d.schedID); dr != nil && dr.DateScheduled > 0 {
		head = "Reschedule message"
	}
	item := func(i int, label string) layout.FlexChild {
		return layout.Rigid(func(gtx C) D { return d.menuItem(gtx, &d.schedItems[i], "", label, false, false) })
	}
	mon := draftsSchedTime(draftsSchedMonday, now)
	return col(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 16, Right: 16, Top: 4, Bottom: 4}.Layout(gtx, func(gtx C) D {
				return d.para(gtx, 13, 18, wBold, d.p.Muted, 1, head)
			})
		}),
		item(draftsSchedTomorrow, "Tomorrow at "+clock(draftsSchedTime(draftsSchedTomorrow, now))),
		item(draftsSchedMonday, mon.Format("Monday")+" at "+clock(mon)),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 8, Bottom: 8}.Layout(gtx, func(gtx C) D { return hline(gtx, d.p.Border) })
		}),
		item(draftsSchedCustom, "Custom time"),
	)
}

// draftsPicker is the "Custom time" dialog: a day (the next 14 days) and
// a time (every 30 minutes) as chips.
type draftsPicker struct {
	d         *Drafts
	id        string
	day, slot int
	days      [14]widget.Clickable
	slots     [48]widget.Clickable
	list      widget.List
}

func (p *draftsPicker) at() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day()+p.day, p.slot/2, 30*(p.slot%2), 0, 0, now.Location())
}

func (d *Drafts) openPicker(dr *core.Draft) {
	ph, ok := d.h.(popups.Host)
	if !ok {
		return
	}
	p := &draftsPicker{d: d, id: dr.ID, day: 1, slot: 18} // tomorrow 9:00 AM
	if dr.DateScheduled > 0 {
		t := unixTime(dr.DateScheduled)
		now := time.Now()
		days := int(time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()).Sub(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())).Hours()+12) / 24
		if days >= 0 && days < len(p.days) {
			p.day, p.slot = days, t.Hour()*2+t.Minute()/30
		}
	}
	p.list.Axis = layout.Vertical
	ph.ShowOverlay(popups.NewModal(ph, popups.ModalOpts{
		Title: "Schedule message",
		Body:  p.body,
		Buttons: []*popups.ModalButton{
			{Label: "Cancel", Kind: popups.BtnOutline},
			{Label: "Schedule message", Kind: popups.BtnPrimary, Enabled: func() bool { return p.at().After(time.Now()) },
				Do: func() bool {
					if dr := d.findDraft(p.id); dr != nil {
						d.scheduleDraft(dr, p.at())
					}
					return true
				}},
		},
	}))
}

func (p *draftsPicker) body(gtx C) D {
	d := p.d
	for i := range p.days {
		if p.days[i].Clicked(gtx) {
			p.day = i
		}
	}
	for i := range p.slots {
		if p.slots[i].Clicked(gtx) {
			p.slot = i
		}
	}
	now := time.Now()
	dayLabel := func(i int) string {
		t := now.AddDate(0, 0, i)
		switch i {
		case 0:
			return "Today"
		case 1:
			return "Tomorrow"
		}
		return t.Format("Mon, Jan 2")
	}
	chips := func(n, perRow, sel int, clicks func(int) *widget.Clickable, label func(int) string, disabled func(int) bool) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			var rows []layout.FlexChild
			for r0 := 0; r0 < n; r0 += perRow {
				r0 := r0
				rows = append(rows, layout.Rigid(func(gtx C) D {
					var ch []layout.FlexChild
					for i := r0; i < min(n, r0+perRow); i++ {
						i := i
						ch = append(ch, layout.Rigid(func(gtx C) D {
							return layout.Inset{Right: 6, Bottom: 6}.Layout(gtx, func(gtx C) D {
								kind := btnOutline
								if i == sel {
									kind = btnPrimary
								}
								if disabled(i) && i != sel {
									kind = btnGhost
								}
								return d.button(gtx, clicks(i), kind, true, label(i))
							})
						}))
					}
					return row(gtx, layout.Middle, ch...)
				}))
			}
			return col(gtx, rows...)
		})
	}
	heading := func(s string) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D { return d.para(gtx, 15, 22, wBold, d.p.Text, 1, s) })
		})
	}
	past := func(i int) bool {
		t := time.Date(now.Year(), now.Month(), now.Day()+p.day, i/2, 30*(i%2), 0, 0, now.Location())
		return !t.After(now)
	}
	at := p.at()
	note := draftsSendText(at)
	if !at.After(now) {
		note = "Pick a time in the future."
	}
	return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D {
		return col(gtx,
			heading("Date"),
			chips(len(p.days), 5, p.day, func(i int) *widget.Clickable { return &p.days[i] }, dayLabel, func(int) bool { return false }),
			vgap(10),
			heading("Time"),
			chips(len(p.slots)-14, 6, p.slot-14, func(i int) *widget.Clickable { return &p.slots[i+14] },
				func(i int) string {
					return clock(time.Date(2000, 1, 1, (i+14)/2, 30*((i+14)%2), 0, 0, time.Local))
				}, func(i int) bool { return past(i + 14) }),
			vgap(6),
			layout.Rigid(func(gtx C) D { return d.para(gtx, 13, 18, wReg, d.p.Muted, 1, note) }),
		)
	})
}

// ---- draft writes (core) ----

// run performs a write off the UI goroutine and reports through drainDone.
func (d *Drafts) run(dn draftsDone, f func(ctx context.Context, c *core.App) (*core.Draft, error)) {
	c, h := d.h.Core(), d.h
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		saved, err := f(ctx, c)
		dn.saved, dn.err = saved, err
		d.mu.Lock()
		d.done = append(d.done, dn)
		d.mu.Unlock()
		h.Invalidate()
	}()
}

func (d *Drafts) drainDone() {
	d.mu.Lock()
	ds := d.done
	d.done = nil
	d.mu.Unlock()
	for _, x := range ds {
		if x.err != nil {
			for _, id := range x.unhide {
				delete(d.gone, id)
			}
			if e := d.edit; e != nil && e.id == x.closeEdit {
				e.busy = false
			}
			d.h.Toast(x.errMsg + ": " + friendlyErr(x.err))
		} else {
			if x.saved != nil {
				l := d.all()
				for i := range l {
					if l[i].ID == x.saved.ID {
						l[i] = *x.saved
					}
				}
				d.place(l)
			}
			if e := d.edit; e != nil && x.closeEdit != "" && e.id == x.closeEdit {
				d.closeEdit()
			}
			if x.okMsg != "" {
				d.h.Toast(x.okMsg)
			}
		}
		d.reload(draftsTabDrafts)
		if d.sent.started() {
			d.reload(draftsTabSent)
		}
	}
}

// remove hides drafts at once (the write confirms or restores them).
func (d *Drafts) remove(ids ...string) {
	for _, id := range ids {
		d.gone[id] = time.Now()
		if d.edit != nil && d.edit.id == id {
			d.closeEdit()
		}
		delete(d.selected, id)
	}
	d.place(d.all())
}

func (d *Drafts) deleteDraft(dr *core.Draft) {
	id, ch, sched := dr.ID, dr.Channel, dr.DateScheduled > 0
	d.remove(id)
	tt := d.tt
	d.run(draftsDone{errMsg: "Couldn’t delete that", unhide: []string{id}},
		func(ctx context.Context, c *core.App) (*core.Draft, error) {
			if sched {
				return nil, c.DeleteScheduled(ctx, ch, id)
			}
			return nil, deleteDraftThunk(ctx, tt, c, id, ch)
		})
}

func (d *Drafts) sendDraft(dr *core.Draft) {
	id := dr.ID
	d.remove(id)
	d.run(draftsDone{okMsg: "Message sent", errMsg: "Message not sent", unhide: []string{id}},
		func(ctx context.Context, c *core.App) (*core.Draft, error) {
			_, err := c.SendDraft(ctx, id)
			return nil, err
		})
}

// scheduleDraft schedules a draft (or reschedules a scheduled message)
// for at; it moves to the Scheduled tab right away.
func (d *Drafts) scheduleDraft(dr *core.Draft, at time.Time) {
	if !at.After(time.Now()) {
		d.h.Toast("Pick a time in the future")
		return
	}
	id, ts := dr.ID, at.Unix()
	moved := *dr
	moved.DateScheduled = ts
	l := d.all()
	for i := range l {
		if l[i].ID == id {
			l[i] = moved
		}
	}
	d.place(l)
	d.run(draftsDone{okMsg: "Scheduled for " + laterWhen(at), errMsg: "Couldn’t schedule your message"},
		func(ctx context.Context, c *core.App) (*core.Draft, error) {
			_, err := c.RescheduleMessage(ctx, id, ts)
			return nil, err
		})
}

// ---- scheduled message menu and the delete dialog ----

func (d *Drafts) closeMore() {
	if r, ok := d.rows["d:"+d.moreID]; ok {
		r.pin = false
	}
	d.moreOpen, d.moreID = false, ""
}

func (d *Drafts) updateMore(gtx C) {
	if d.moreID == "" {
		return
	}
	if !d.moreOpen {
		d.closeMore()
		return
	}
	for i := range d.moreItems {
		if !d.moreItems[i].Clicked(gtx) {
			continue
		}
		id := d.moreID
		d.closeMore()
		if dr := d.findDraft(id); dr != nil {
			if i == 0 {
				d.unscheduleDraft(dr)
			} else {
				d.confirmDelete(dr)
			}
		}
		return
	}
}

// moreMenu is the scheduled message's ⋮ menu (DraftItem's renderMenu):
// "Cancel schedule and save to drafts" and the danger "Delete message".
func (d *Drafts) moreMenu(gtx C) D {
	return col(gtx,
		layout.Rigid(func(gtx C) D {
			return d.menuItem(gtx, &d.moreItems[0], "", "Cancel schedule and save to drafts", false, false)
		}),
		layout.Rigid(func(gtx C) D { return d.dangerItem(gtx, &d.moreItems[1], "Delete message") }),
	)
}

// dangerItem is a 32px menu row in Slack's danger red (hover: red fill,
// white text).
func (d *Drafts) dangerItem(gtx C, c *widget.Clickable, label string) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		w, h := gtx.Constraints.Max.X, gtx.Dp(32)
		fg := d.th.C.Danger
		if c.Hovered() {
			fill(gtx, image.Rect(0, 0, w, h), d.th.C.Danger)
			fg = rgb(0xffffff)
		}
		g := gtx
		g.Constraints = layout.Exact(image.Pt(w, h))
		layout.Inset{Left: 16, Right: 16}.Layout(g, func(gtx C) D {
			return layout.W.Layout(gtx, func(gtx C) D { return d.label(gtx, 15, wReg, fg, label) })
		})
		return D{Size: image.Pt(w, h)}
	})
}

// deleteDialogText is DeleteDraftModal's title, question and buttons.
// The destination is bold in Slack's question.
func deleteDialogText(dst draftDest, scheduled, thread, hasDest bool) (title string, q []richtext.Span, del string, saveAsDraft bool) {
	title, del = "Delete draft?", "Delete Draft"
	if scheduled {
		title, del = "Delete scheduled message?", "Delete"
	}
	name := dst.name
	if dst.glyph != "" {
		name = "#" + name
	}
	plain := func(t string) richtext.Span { return richtext.Span{Kind: richtext.Text, Text: t} }
	bold := richtext.Span{Kind: richtext.Text, Text: name, Style: richtext.Bold}
	switch {
	case !hasDest:
		q = []richtext.Span{plain("Are you sure you want to delete this draft?")}
	case scheduled && !thread:
		q = []richtext.Span{plain("Are you sure you want to delete this message to "), bold, plain("?")}
	case thread:
		q = []richtext.Span{plain("Are you sure you want to delete this message to a thread in "), bold, plain("?")}
	default:
		q = []richtext.Span{plain("Are you sure you want to delete this draft to "), bold, plain("?")}
	}
	return title, q, del, scheduled && !thread
}

// confirmDelete opens Slack's DeleteDraftModal: "Delete draft?" (Cancel,
// Delete Draft) or "Delete scheduled message?" (Save as Draft on the
// left, Cancel, Delete).
func (d *Drafts) confirmDelete(dr *core.Draft) {
	ph, ok := d.h.(popups.Host)
	if !ok {
		d.deleteDraft(dr)
		return
	}
	id := dr.ID
	dst := d.destOf(dr.Channel, dr.Users, dr.ThreadTS, nil)
	title, q, del, saveAs := deleteDialogText(dst, dr.DateScheduled > 0, dr.ThreadTS != "", dr.Channel != "" || len(dr.Users) > 0)
	doc := richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: q}}}
	var btns []*popups.ModalButton
	if saveAs {
		btns = append(btns, &popups.ModalButton{Label: "Save as Draft", Kind: popups.BtnOutline, Left: true, Do: func() bool {
			if dr := d.findDraft(id); dr != nil {
				d.unscheduleDraft(dr)
			}
			return true
		}})
	}
	btns = append(btns,
		&popups.ModalButton{Label: "Cancel", Kind: popups.BtnOutline},
		&popups.ModalButton{Label: del, Kind: popups.BtnDanger, Do: func() bool {
			if dr := d.findDraft(id); dr != nil {
				d.deleteDraft(dr)
			}
			return true
		}})
	// DeleteDraftModal is a c-sk-modal--fixed (max-width 520px): title
	// bar padding 20px 28px, min-height 70px; body section padding 0 28px;
	// c-sk-modal_footer 84px with padding 24px 28px, actions 12px apart.
	// The Delete button has autoFocus (no visibleAutoFocus: no ring).
	ph.ShowOverlay(popups.NewModal(ph, popups.ModalOpts{
		Title:      title,
		Width:      520,
		FooterPad:  24,
		AutoFocus:  btns[len(btns)-1],
		QuietFocus: true,
		Body: func(gtx C) D {
			return d.richDoc(gtx, "draft-delete:"+id, title, doc, richview.Options{}, 0)
		},
		Buttons: btns,
	}))
}

// unscheduleDraft moves a scheduled message back to Drafts (drafts.update
// with date_scheduled 0).
func (d *Drafts) unscheduleDraft(dr *core.Draft) {
	id := dr.ID
	moved := *dr
	moved.DateScheduled = 0
	l := d.all()
	for i := range l {
		if l[i].ID == id {
			l[i] = moved
		}
	}
	d.place(l)
	d.run(draftsDone{errMsg: "Couldn’t save your draft"},
		func(ctx context.Context, c *core.App) (*core.Draft, error) {
			nd, err := c.UnscheduleDraft(ctx, id)
			if err != nil {
				return nil, err
			}
			return &nd, nil
		})
}

// ---- select mode (header "Edit") ----

func (d *Drafts) endSelect() {
	if d.selecting {
		d.selecting, d.dirty = false, true
	}
	clear(d.selected)
}

func (d *Drafts) selectedIDs() []string {
	var ids []string
	for _, dr := range d.drafts {
		if d.selected[dr.ID] {
			ids = append(ids, dr.ID)
		}
	}
	return ids
}

func (d *Drafts) deleteSelected() {
	ids := d.selectedIDs()
	if len(ids) == 0 {
		return
	}
	d.remove(ids...)
	d.endSelect()
	tt := d.tt
	d.run(draftsDone{okMsg: "Deleted " + plural(len(ids), "draft"), errMsg: "Couldn’t delete every draft", unhide: ids},
		func(ctx context.Context, c *core.App) (*core.Draft, error) {
			return nil, clearDraftsThunk(ctx, tt, c, ids)
		})
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
	kb := d.keys(gtx, sel)
	dims := d.scrollList(gtx, l, ne+extra, func(gtx C, i int) D {
		if i >= ne {
			return d.skeleton(gtx, 1, draftsTile)
		}
		if kb == nil {
			return d.entry(gtx, sel, i)
		}
		dd := d.entry(gtx, sel, i)
		if kb.ringOn(d.entryKey(sel, i)) {
			if d.view[i].kind == draftsEntryDraft {
				// .p-drafts_page__draft_list_item:after: radius 12,
				// inset 16px 0 0 — the card itself
				m := gtx.Dp(draftsMargin)
				d.vkRing(gtx, image.Rect(m, 0, dd.Size.X-m, dd.Size.Y-gtx.Dp(8)), gtx.Dp(12))
			} else { // the education card: the default inset 4px, radius 8
				in := gtx.Dp(4)
				d.vkRing(gtx, image.Rect(in, in, dd.Size.X-in, dd.Size.Y-in), gtx.Dp(8))
			}
		}
		return kb.elem(i, dd)
	})
	if kb != nil {
		kb.area(gtx, image.Rectangle{Max: dims.Size})
		kb.after()
	}
	if sel == draftsTabSent {
		if p := l.Position; p.First+p.Count >= ne-3 {
			d.loadMoreSent()
		}
	}
	return dims
}

// entry draws d.view[i].
func (d *Drafts) entry(gtx C, sel, i int) D {
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
		l, sch := d.drafts, false
		if sel == draftsTabScheduled {
			l, sch = d.scheduled, true
		}
		if d.edit != nil && d.edit.id == l[e.i].ID {
			return d.editCard(gtx, &l[e.i])
		}
		return d.draftCard(gtx, &l[e.i], sch)
	}
}

// entryKey is a view entry's list key (getDraftListKeys): the education
// card's, a draft's id, "" for Clack's spacers.
func (d *Drafts) entryKey(sel, i int) string {
	e := d.view[i]
	switch e.kind {
	case draftsEntryCard:
		return "sent_page_education_card"
	case draftsEntryDraft:
		l := d.drafts
		if sel == draftsTabScheduled {
			l = d.scheduled
		}
		if e.i < len(l) {
			return "d:" + l[e.i].ID
		}
	}
	return ""
}

// keys runs the Drafts and Scheduled lists' keyboard (DraftList: FSLM
// with useGlobalKeyboardShortcuts; every row is focusable, Enter is
// onItemClick: a draft opens for editing, the education card does
// nothing). Sent isn't a DraftList: nil.
func (d *Drafts) keys(gtx C, sel int) *vkList {
	if sel == draftsTabSent {
		return nil
	}
	kb := d.kb[sel]
	items := make([]vkItem, 0, len(d.view))
	for i := range d.view {
		k := d.entryKey(sel, i)
		if k == "" {
			k = "gap:" + strconv.Itoa(i)
		}
		items = append(items, vkItem{key: k, elem: i, focusable: !strings.HasPrefix(k, "gap:")})
	}
	kb.setItems(items)
	kb.begin()
	kb.update(gtx, gtx.Constraints.Max.Y, vkOpts{Click: func(k string) {
		// the row's click (not Clickable.Click: the row's Layout this
		// frame would swallow it)
		id, ok := strings.CutPrefix(k, "d:")
		dr := d.findDraft(id)
		switch {
		case !ok || dr == nil:
		case d.selecting:
			d.selected[id] = !d.selected[id]
		case d.edit == nil || d.edit.id != id:
			d.openDraft(dr)
		}
	}})
	return kb
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
			bg = rgb(0x2a1707) // plt-horchata-0
		}
		rrect(gtx, image.Rect(0, 0, w, h), gtx.Dp(12), bg)
		g := gtx
		g.Constraints = layout.Exact(image.Pt(w, h))
		// measured: title at (24, 45), body right under it at (24, 69)
		layout.Inset{Top: 45, Left: 24, Right: 64, Bottom: 24}.Layout(g, func(gtx C) D {
			gtx.Constraints.Min = image.Point{}
			return col(gtx,
				layout.Rigid(func(gtx C) D {
					return d.para(gtx, 18, 24, wBlack, d.p.Text, 1, "All your outgoing messages")
				}),
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
	tile := flatten(argb(0x80dddddd), d.p.Bg)
	if d.th != nil && d.th.Dark {
		tile = d.p.Track
	}
	rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(8), tile)
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
	soft = flatten(soft, d.p.Bg)
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
	prev := d.preview(m)
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
			paint.FillShape(gtx.Ops, d.p.CardLine, clip.Stroke{Path: theme.CachedRRect(rr), Width: float32(max(1, gtx.Dp(1)))}.Op())
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
	when := draftsListTime(draftTime(dr), time.Now())
	if scheduled {
		when = draftsSendText(unixTime(dr.DateScheduled))
	}
	prev := oneLine(richtext.PlainText(dr.Doc))
	if prev == "" && dr.Files > 0 {
		prev = draftFilesLine(dr)
	}
	return d.dlHoverRow(gtx, &r.dlRowState, dlRowOpts{
		Content: func(gtx C) D {
			return layout.Inset{Left: draftsMargin, Right: draftsMargin}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				t := when
				if r.hovered || r.pin {
					t = ""
				}
				m := op.Record(gtx.Ops)
				dims := layout.UniformInset(12).Layout(gtx, func(gtx C) D {
					if !d.selecting || scheduled {
						return d.rowContent(gtx, dst, t, prev)
					}
					// Select mode: a checkbox before the tile.
					return row(gtx, layout.Middle,
						layout.Rigid(func(gtx C) D { return d.todoCheck(gtx, d.selected[dr.ID]) }),
						hgap(12),
						layout.Flexed(1, func(gtx C) D { return d.rowContent(gtx, dst, when, prev) }),
					)
				})
				call := m.Stop()
				rect := image.Rectangle{Max: dims.Size}
				rad := gtx.Dp(12)
				if r.hovered {
					theme.Shadow(gtx, rect, rad, gtx.Dp(3), argb(0x14000000))
				}
				rrect(gtx, rect, rad, d.p.Bg)
				border(gtx, rect, rad, max(1, gtx.Dp(1)), d.p.Ring)
				call.Add(gtx.Ops)
				return dims
			})
		},
		Below: func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Max.X, gtx.Dp(8))} },
		Bar: func(gtx C) D {
			if d.selecting {
				return D{}
			}
			ib := func(c *widget.Clickable, icon string) layout.FlexChild {
				return layout.Rigid(func(gtx C) D { return d.iconButton(gtx, c, icon, 32, 18, d.p.Sec) })
			}
			// Slack's DraftItem actions: a draft has Delete draft, Edit
			// draft, Schedule message and Send message; a scheduled
			// message has Edit message, Reschedule message, Send message
			// and a ⋮ menu (a scheduled thread reply has Delete message
			// up front instead of the menu).
			var btns []layout.FlexChild
			thread := dr.ThreadTS != ""
			if !scheduled || thread {
				btns = append(btns, ib(&r.del, "trash"))
			}
			btns = append(btns, ib(&r.edit, "edit"), ib(&r.sched, "schedule"), ib(&r.send, "send"))
			if scheduled && !thread {
				btns = append(btns, ib(&r.more, "kebab"))
			}
			dims := d.draftsBar(gtx, btns...)
			if d.schedOpen && d.schedID == dr.ID {
				w := unit.Dp(260)
				d.dropdown(gtx, &d.schedOpen, &d.schedScrim, image.Pt(dims.Size.X-gtx.Dp(w), dims.Size.Y+gtx.Dp(4)), w, d.schedMenu)
			}
			if d.moreOpen && d.moreID == dr.ID {
				w := unit.Dp(300)
				d.dropdown(gtx, &d.moreOpen, &d.moreScrim, image.Pt(dims.Size.X-gtx.Dp(w), dims.Size.Y+gtx.Dp(4)), w, d.moreMenu)
			}
			return dims
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
	border(gtx, r, rad, max(1, gtx.Dp(1)), d.p.Ring)
	call.Add(gtx.Ops)
	return dims
}

// draftsSendText is Slack's DraftDateScheduled: "Send {date} at {time}"
// with {date_short_pretty} uncapitalized: "today", "tomorrow" or
// "yesterday", else "Nov 10" (MMM D), with the year when the date is more
// than about six months away (excludeYearHelper: ±262080 minutes).
func draftsSendText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return "Send " + shortPrettyDate(t, time.Now()) + " at " + clock(t)
}

// shortPrettyDate is Slack's {date_short_pretty}, uncapitalized.
func shortPrettyDate(t, now time.Time) string {
	switch {
	case sameDay(t, now):
		return "today"
	case sameDay(t, now.AddDate(0, 0, 1)):
		return "tomorrow"
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "yesterday"
	}
	d := t.Sub(now)
	if d < 0 {
		d = -d
	}
	if d < 262080*time.Minute {
		return t.Format("Jan 2")
	}
	return t.Format("Jan 2, 2006")
}

// draftsListTime is a draft's timestamp (DraftItem): the time today,
// else the short calendar date ("Nov 7th"; "Nov 7th, 2025" from another
// year).
func draftsListTime(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	if sameDay(t, now) {
		return clock(t)
	}
	s := t.Format("Jan 2") + ordinal(t.Day())
	if t.Year() != now.Year() {
		s += ", " + strconv.Itoa(t.Year())
	}
	return s
}

func draftFilesLine(dr *core.Draft) string {
	var names []string
	for _, f := range dr.FileInfo {
		n := f.Title
		if n == "" {
			n = f.Name
		}
		if n == "" {
			return plural(dr.Files, "file")
		}
		names = append(names, n)
	}
	if len(names) == 0 {
		return plural(dr.Files, "file")
	}
	return strings.Join(names, ", ")
}
