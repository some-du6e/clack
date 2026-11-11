package views

// To-dos (rail tab, route "later") and Saved (Home sidebar, route
// "saved"): the 2026 replacements for the old Later tabs. Both read
// saved.list with the web client's filters: To-dos uses
// core.SavedFilterTodos (to-dos and completed ones; the status and
// reminder filters and the sort run client-side), Saved uses
// core.SavedFilterSaved and leaves out anything that is a to-do.
//
// Writes go through core (AddTodo, CompleteTodo, MoveToSaved,
// MoveToTodos, Unsave, UpdateSaved) and run only when the user clicks.
// Realtime "saved" events reload the list.
//
// Measurements: /tmp/ui-views-spec/later.md. Rows are the spec's
// "inferred" row (the Activity frame with a 36px avatar).
//
// Also home to the hover-row helpers shared by DMs, To-dos, Saved and
// Drafts (dlHoverRow, dlActionBar).

import (
	"context"
	"image"
	"image/color"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/font"
	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const (
	laterAvatar unit.Dp = 36
	laterAvGap  unit.Dp = 8
	laterCtxH   unit.Dp = 20 // context line (holds the 20px chip)

	todoHeadH  unit.Dp = 50
	savedHeadH unit.Dp = 49
	todoToolY  unit.Dp = 16 // toolbar gap below the header
	todoToolH  unit.Dp = 32
)

// laterPreset is a "Set reminder" choice.
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

func laterKey(it *core.SavedItem) string { return it.ItemID + "/" + it.TS }

func laterCompleted(it *core.SavedItem) bool {
	return it.TodoState == "completed" || it.DateCompleted > 0 || it.State == "completed" || it.Progress == "completed"
}

// laterOverdue reports a due date before today (a to-do due earlier
// today, e.g. one added without a reminder, counts as due today).
func laterOverdue(it *core.SavedItem) bool {
	if it.DateDue <= 0 {
		return false
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return unixTime(it.DateDue).Before(today)
}

// laterIsTodo reports a to-do (in progress or completed).
func laterIsTodo(it *core.SavedItem) bool {
	return it.TodoState == "to_do" || it.TodoState == "completed"
}

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

// ---- shared saved.list state (To-dos and Saved) ----

type laterResult struct {
	page   core.SavedPage
	seq    int
	cursor string
}

// savedFeed is one saved.list filter, paged.
type savedFeed struct {
	filter      string
	first, more async[laterResult]

	firstApplied, moreApplied int
	moreTried                 string

	items  []core.SavedItem
	next   string
	pages  int
	counts []byte // the first page's saved.list counts (later_sections.go)
}

type laterRow struct {
	dlRowState
	done, remind, toSaved, toTodos, unsave, more, clearDue widget.Clickable
	presets                                                [len(laterPresets)]widget.Clickable
}

// laterDone is a finished write, reported back to the UI goroutine.
type laterDone struct {
	key, msg string
	reload   bool
	err      error
}

// savedBase is embedded by To-dos and Saved.
type savedBase struct {
	base

	todos    bool // the To-dos page (else Saved)
	feeds    []*savedFeed
	convs    map[string]core.ConvRef
	rows     map[string]*laterRow
	gone     map[string]time.Time // optimistic removals, hidden from reloads for a while
	seq      int
	last     time.Time
	dirty    bool
	watching bool
	retry    widget.Clickable

	w     watcher
	mu    sync.Mutex
	done  []laterDone
	known map[string]bool // "channel/ts" of listed items (mu)
}

func newSavedBase(h Host) savedBase {
	return savedBase{
		base:  newBase(h),
		convs: map[string]core.ConvRef{},
		rows:  map[string]*laterRow{},
		gone:  map[string]time.Time{},
		known: map[string]bool{},
		dirty: true,
	}
}

// begin runs at the top of update: subscribe once and report whether the
// view is being shown again after a while (then it should reload).
func (s *savedBase) begin(gtx C) bool {
	if !s.watching {
		s.watching = true
		s.w.watch(s.h, s.match)
	}
	again := !s.last.IsZero() && gtx.Now.Sub(s.last) > 10*time.Second
	s.last = gtx.Now
	if s.w.due(gtx, 2*time.Second) {
		again = true
	}
	return again
}

func (s *savedBase) reload(f *savedFeed) {
	s.seq++
	seq, filter := s.seq, f.filter
	c := s.h.Core()
	f.first.load(s.h, true, func(ctx context.Context) (laterResult, error) {
		p, err := c.Saved(ctx, filter, "")
		return laterResult{page: p, seq: seq}, err
	})
}

func (s *savedBase) loadMore(f *savedFeed) {
	if f.next == "" || f.next == f.moreTried || f.more.busy() || f.first.busy() {
		return
	}
	s.seq++
	seq, filter, cur := s.seq, f.filter, f.next
	f.moreTried = cur
	c := s.h.Core()
	f.more.load(s.h, true, func(ctx context.Context) (laterResult, error) {
		p, err := c.Saved(ctx, filter, cur)
		return laterResult{page: p, seq: seq, cursor: cur}, err
	})
}

// applyFeeds takes finished loads into the feeds.
func (s *savedBase) applyFeeds() {
	changed := false
	for _, f := range s.feeds {
		if s.applyFeed(f) {
			changed = true
		}
	}
	if !changed {
		return
	}
	s.dirty = true
	s.mu.Lock()
	clear(s.known)
	for _, f := range s.feeds {
		for i := range f.items {
			s.known[f.items[i].Channel+"/"+f.items[i].TS] = true
		}
	}
	s.mu.Unlock()
}

func (s *savedBase) applyFeed(f *savedFeed) bool {
	changed := false
	if v, ok, _ := f.first.get(); ok && v.seq != 0 && v.seq != f.firstApplied {
		f.firstApplied = v.seq
		seen := map[string]bool{}
		var out []core.SavedItem
		for _, it := range v.page.Items {
			k := laterKey(&it)
			if seen[k] || s.hidden(k) || !s.wants(&it) {
				continue
			}
			seen[k] = true
			out = append(out, it)
		}
		if f.pages > 1 {
			for _, it := range f.items {
				if k := laterKey(&it); !seen[k] && !s.hidden(k) {
					seen[k] = true
					out = append(out, it)
				}
			}
		} else {
			f.next, f.pages, f.moreTried = v.page.Next, 1, ""
		}
		f.items = out
		f.counts = []byte(v.page.Counts)
		for id, r := range v.page.Convs {
			s.convs[id] = r
		}
		changed = true
	}
	if v, ok, _ := f.more.get(); ok && v.seq != 0 && v.seq != f.moreApplied {
		f.moreApplied = v.seq
		if v.cursor == f.next {
			have := map[string]bool{}
			for j := range f.items {
				have[laterKey(&f.items[j])] = true
			}
			for _, it := range v.page.Items {
				if k := laterKey(&it); !have[k] && !s.hidden(k) && s.wants(&it) {
					have[k] = true
					f.items = append(f.items, it)
				}
			}
			for id, r := range v.page.Convs {
				s.convs[id] = r
			}
			f.next = v.page.Next
			f.pages++
			changed = true
		}
	}
	return changed
}

// hidden reports an item removed optimistically in the last 15 seconds
// (saved.list can lag behind saved.update).
func (s *savedBase) hidden(k string) bool {
	at, ok := s.gone[k]
	if !ok {
		return false
	}
	if time.Since(at) > 15*time.Second {
		delete(s.gone, k)
		return false
	}
	return true
}

// wants reports whether an item belongs on this page: To-dos lists
// to-dos, Saved everything else.
func (s *savedBase) wants(it *core.SavedItem) bool {
	return laterIsTodo(it) == s.todos
}

// match runs on the event goroutine.
func (s *savedBase) match(ev core.Event) bool {
	switch ev.Type {
	case "resync", "saved":
		return true
	case "update", "delete":
		s.mu.Lock()
		k := s.known[ev.Channel+"/"+ev.TS]
		s.mu.Unlock()
		return k
	}
	return false
}

func (s *savedBase) find(k string) (*savedFeed, int) {
	for _, f := range s.feeds {
		for i := range f.items {
			if laterKey(&f.items[i]) == k {
				return f, i
			}
		}
	}
	return nil, -1
}

func (s *savedBase) remove(k string) {
	s.gone[k] = time.Now()
	if f, i := s.find(k); f != nil {
		f.items = append(f.items[:i:i], f.items[i+1:]...)
	}
	if r, ok := s.rows[k]; ok {
		r.pin = false
	}
	s.dirty = true
}

// write runs f off the UI goroutine and reports back through drainDone.
// A failure un-hides key (if any) and reloads.
func (s *savedBase) write(key, okMsg string, reload bool, f func(ctx context.Context, c *core.App) error) {
	c, h := s.h.Core(), s.h
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		err := f(ctx, c)
		s.mu.Lock()
		s.done = append(s.done, laterDone{key: key, msg: okMsg, reload: reload, err: err})
		s.mu.Unlock()
		h.Invalidate()
	}()
}

func (s *savedBase) drainDone() {
	s.mu.Lock()
	ds := s.done
	s.done = nil
	s.mu.Unlock()
	for _, d := range ds {
		if d.err != nil {
			if d.key != "" {
				delete(s.gone, d.key)
			}
			s.h.Toast("Couldn’t update your saved items: " + friendlyErr(d.err))
		} else if d.msg != "" {
			s.h.Toast(d.msg)
		}
		if d.err != nil || d.reload {
			for _, f := range s.feeds {
				if f.first.started() {
					s.reload(f)
				}
			}
		}
	}
}

func (s *savedBase) rowState(k string) *laterRow {
	r, ok := s.rows[k]
	if !ok {
		r = new(laterRow)
		s.rows[k] = r
	}
	return r
}

// rowActions handles clicks on every row's controls (call before layout).
func (s *savedBase) rowActions(gtx C) {
	for k, r := range s.rows {
		click := r.click.Clicked(gtx)
		done := r.done.Clicked(gtx)
		remind := r.remind.Clicked(gtx)
		toSaved := r.toSaved.Clicked(gtx)
		toTodos := r.toTodos.Clicked(gtx)
		clearDue := r.clearDue.Clicked(gtx)
		unsave := r.unsave.Clicked(gtx)
		more := r.more.Clicked(gtx)
		preset := -1
		for i := range r.presets {
			if r.presets[i].Clicked(gtx) {
				preset = i
			}
		}
		if !click && !done && !remind && !toSaved && !toTodos && !clearDue && !unsave && !more && preset < 0 {
			continue
		}
		f, i := s.find(k)
		if f == nil {
			continue
		}
		it := f.items[i]
		ch, ts := it.Channel, it.TS
		switch {
		case click:
			s.open(&it)
		case done:
			s.toggleDone(f, i)
		case remind:
			r.pin = !r.pin
		case toSaved:
			s.write1(it, true, "Moved to Saved", func(ctx context.Context, c *core.App) error {
				return c.MoveToSaved(ctx, ch, ts)
			})
		case toTodos:
			s.write1(it, true, "Added to To-dos", func(ctx context.Context, c *core.App) error {
				return c.MoveToTodos(ctx, ch, ts, 0)
			})
		case clearDue:
			var zero int64
			f.items[i].DateDue = 0
			r.pin, s.dirty = false, true
			s.write1(it, false, "Reminder removed", func(ctx context.Context, c *core.App) error {
				return c.UpdateSaved(ctx, ch, ts, core.SavedUpdate{DateDue: &zero})
			})
		case unsave:
			s.write1(it, true, "Removed from Saved", func(ctx context.Context, c *core.App) error {
				return c.Unsave(ctx, ch, ts)
			})
		case more:
			if it.Msg != nil {
				s.h.OpenMessageMenu(image.Point{}, *it.Msg)
			}
		case preset >= 0:
			// A reminder makes the item a to-do due then (it leaves Saved).
			at := laterPresets[preset].at(time.Now())
			due := at.Unix()
			f.items[i].DateDue = due
			r.pin, s.dirty = false, true
			s.write1(it, !s.todos, "Reminder set for "+laterWhen(at), func(ctx context.Context, c *core.App) error {
				return c.MoveToTodos(ctx, ch, ts, due)
			})
		}
	}
}

// toggleDone marks f.items[i] complete, or back in progress when it is.
func (s *savedBase) toggleDone(f *savedFeed, i int) {
	it := f.items[i]
	ch, ts := it.Channel, it.TS
	if laterCompleted(&it) {
		f.items[i].TodoState, f.items[i].DateCompleted, s.dirty = "to_do", 0, true
		s.write1(it, false, "Moved back to in progress", func(ctx context.Context, c *core.App) error {
			// MoveToTodos alone leaves the item completed (it sends no
			// mark=uncompleted), so un-complete it first.
			no := false
			if err := c.UpdateSaved(ctx, ch, ts, core.SavedUpdate{Completed: &no}); err != nil {
				return err
			}
			return c.MoveToTodos(ctx, ch, ts, 0)
		})
		return
	}
	f.items[i].TodoState, f.items[i].DateCompleted, s.dirty = "completed", time.Now().Unix(), true
	s.write1(it, false, "Marked as complete", func(ctx context.Context, c *core.App) error {
		return c.CompleteTodo(ctx, ch, ts)
	})
}

// write1 runs one item's write; remove hides it from this page at once.
func (s *savedBase) write1(it core.SavedItem, remove bool, okMsg string, f func(ctx context.Context, c *core.App) error) {
	if it.Channel == "" || it.TS == "" {
		return
	}
	k := laterKey(&it)
	if remove {
		s.remove(k)
	}
	s.write(k, okMsg, true, f)
}

// open jumps to the saved message: the thread for replies, else the
// conversation.
func (s *savedBase) open(it *core.SavedItem) {
	if it.Channel == "" {
		return
	}
	if m := it.Msg; m != nil && m.ThreadTS != "" && m.ThreadTS != m.TS {
		s.h.OpenThread(it.Channel, m.ThreadTS)
		return
	}
	s.h.Navigate("c/" + it.Channel)
}

// ---- rows ----

// itemRow: padding 12/20/12/16, 1px rule, 36px avatar + 8px gap; context
// line (13px bold muted) with the chip at its right, then sender (15px
// black) + time (13px), then 3 lines of text. todo picks the To-dos hover
// actions (Move to Saved) over the Saved ones (Remove from Saved).
func (s *savedBase) itemRow(gtx C, it *core.SavedItem, todo bool) D {
	r := s.rowState(laterKey(it))
	var bar, below W
	if it.Channel != "" {
		bar = func(gtx C) D { return s.rowBar(gtx, it, r, todo) }
	}
	if r.pin && !laterCompleted(it) {
		below = func(gtx C) D { return s.remindMenu(gtx, it, r) }
	}
	return s.dlHoverRow(gtx, &r.dlRowState, dlRowOpts{
		HoverBg: s.p.Hover,
		Rule:    s.p.Border,
		Content: func(gtx C) D {
			return layout.Inset{Top: 12, Right: 20, Bottom: 12, Left: 16}.Layout(gtx, func(gtx C) D {
				return s.itemBody(gtx, it)
			})
		},
		Below:    below,
		Bar:      bar,
		BarTop:   8,
		BarRight: 16,
	})
}

func (s *savedBase) rowBar(gtx C, it *core.SavedItem, r *laterRow, todo bool) D {
	ib := func(c *widget.Clickable, icon string) layout.FlexChild {
		return layout.Rigid(func(gtx C) D { return s.iconButton(gtx, c, icon, 32, 18, s.p.Text) })
	}
	var btns []layout.FlexChild
	switch {
	case !todo: // Saved: Add to To-dos, Set reminder, Remove from Saved
		btns = append(btns, ib(&r.toTodos, "check-circle"), ib(&r.remind, "clock"), ib(&r.unsave, "bookmark-filled"))
	case laterCompleted(it): // completed to-do: Mark incomplete, Move to Saved
		btns = append(btns, ib(&r.done, "history"), ib(&r.toSaved, "bookmark"))
	default: // to-do: Mark complete, Set reminder, Move to Saved
		btns = append(btns, ib(&r.done, "check"), ib(&r.remind, "clock"), ib(&r.toSaved, "bookmark"))
	}
	if it.Msg != nil {
		btns = append(btns, ib(&r.more, "more"))
	}
	return s.dlActionBar(gtx, btns...)
}

// remindMenu is the inline "Set reminder" preset row under an item.
func (s *savedBase) remindMenu(gtx C, it *core.SavedItem, r *laterRow) D {
	return layout.Inset{Left: laterAvatar + laterAvGap + 16, Right: 20, Bottom: 12}.Layout(gtx, func(gtx C) D {
		ch := []layout.FlexChild{
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Right: 8}.Layout(gtx, func(gtx C) D { return s.label(gtx, 13, wBold, s.p.Muted, "Remind me:") })
			}),
		}
		for i := range laterPresets {
			i := i
			ch = append(ch, layout.Rigid(func(gtx C) D {
				return layout.Inset{Right: 8}.Layout(gtx, func(gtx C) D {
					return s.button(gtx, &r.presets[i], btnOutline, true, laterPresets[i].label)
				})
			}))
		}
		if it.DateDue > 0 && laterIsTodo(it) {
			ch = append(ch, layout.Rigid(func(gtx C) D {
				return s.button(gtx, &r.clearDue, btnGhost, true, "Remove reminder")
			}))
		}
		return row(gtx, layout.Middle, ch...)
	})
}

func (s *savedBase) itemBody(gtx C, it *core.SavedItem) D {
	m := it.Msg
	name, av := s.authorOf(m)
	return row(gtx, layout.Start,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: laterCtxH}.Layout(gtx, func(gtx C) D { return s.avatar(gtx, av, laterAvatar) })
		}),
		hgap(laterAvGap),
		layout.Flexed(1, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return col(gtx,
				layout.Rigid(func(gtx C) D { return s.contextLine(gtx, it) }),
				layout.Rigid(func(gtx C) D {
					if m == nil {
						msg := "This message is no longer available."
						if it.ItemType != "" && it.ItemType != "message" {
							msg = "Saved " + it.ItemType
						}
						return s.para(gtx, 15, 22, wReg, s.p.Muted, 1, msg)
					}
					return row(gtx, layout.Baseline,
						layout.Rigid(func(gtx C) D { return s.para(gtx, 15, 22, wBlack, s.p.Text, 1, name) }),
						hgap(8),
						layout.Rigid(func(gtx C) D { return s.label(gtx, 13, wReg, s.p.Muted, listTime(tsTime(m.TS))) }),
					)
				}),
				layout.Rigid(func(gtx C) D { return s.msgBody(gtx, m, false, 3) }),
			)
		}),
	)
}

// contextLine: "#channel" / "Direct message" (13px/18px bold muted) and
// the reminder or completed chip at the right, in a 20px band.
func (s *savedBase) contextLine(gtx C, it *core.SavedItem) D {
	ctx := ""
	if it.Channel != "" {
		kind := ""
		if r, ok := s.convs[it.Channel]; ok {
			kind = r.Kind
		} else if c, ok := s.h.Core().Conv(it.Channel); ok {
			kind = c.Kind
		}
		if isDMKind(kind) {
			ctx = "Direct message"
		} else {
			ctx = s.convTitle(it.Channel, s.convs)
		}
		if m := it.Msg; m != nil && m.ThreadTS != "" && m.ThreadTS != m.TS {
			ctx = "Thread in " + ctx
			if isDMKind(kind) {
				ctx = "Thread in a direct message"
			}
		}
	}
	gtx.Constraints.Min.Y = gtx.Dp(laterCtxH)
	return layout.W.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.Y = 0
		return row(gtx, layout.Middle,
			layout.Flexed(1, func(gtx C) D { return s.para(gtx, 13, 18, wBold, s.p.Muted, 1, ctx) }),
			layout.Rigid(func(gtx C) D { return s.savedChip(gtx, it) }),
		)
	})
}

// savedChip: overdue #ffe8ef/#c01343, upcoming #f8f8f8/#454447 (h 20,
// padding 0 8, pill, 12px bold, 12px clock), or a green "Completed" check.
func (s *savedBase) savedChip(gtx C, it *core.SavedItem) D {
	if laterCompleted(it) {
		green := s.p.Green
		return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D { return s.icon(gtx, "check", 14, green) }),
				hgap(4),
				layout.Rigid(func(gtx C) D { return s.label(gtx, 12, wBold, green, "Completed") }),
			)
		})
	}
	if it.DateDue <= 0 {
		return D{}
	}
	due := unixTime(it.DateDue)
	txt, over := dueText(due)
	if over && !laterOverdue(it) {
		// Slack stamps a to-do added without a reminder as due "now";
		// that reads as due today, not overdue.
		txt, over = "Due today", false
	}
	fg, bg := s.p.Sec, s.p.ReadFill
	if over {
		fg, bg = rgb(0xc01343), rgb(0xffe8ef)
		if s.th != nil && s.th.Dark {
			fg, bg = rgb(0xde678a), rgb(0x300005) // content-imp / base-imp
		}
	}
	return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D {
		m := op.Record(gtx.Ops)
		d := layout.Inset{Left: 8, Right: 8}.Layout(gtx, func(gtx C) D {
			g := gtx
			g.Constraints.Min.Y = gtx.Dp(20)
			return layout.W.Layout(g, func(gtx C) D {
				gtx.Constraints.Min.Y = 0
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D { return s.icon(gtx, "clock", 12, fg) }),
					hgap(4),
					layout.Rigid(func(gtx C) D { return s.label(gtx, 12, wBold, fg, txt) }),
				)
			})
		})
		call := m.Stop()
		rrect(gtx, image.Rectangle{Max: d.Size}, d.Size.Y/2, bg)
		call.Add(gtx.Ops)
		return d
	})
}

// savedEmpty is the spec's empty state without the Slack illustration: a
// 64px accent check, 24px gap, 18/24 black title, 8px, 15/22 text, 8px,
// and an optional medium outline button.
func (s *savedBase) savedEmpty(gtx C, title, body string, btn *widget.Clickable, label string) D {
	return layout.Center.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(420))
		gtx.Constraints.Min = image.Point{}
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				// Slack's illustration is a 157×140 SVG (empty-later-light);
				// keep its box so the title lands where Slack's does.
				g := gtx
				g.Constraints = layout.Exact(image.Pt(gtx.Dp(157), gtx.Dp(140)))
				return layout.Center.Layout(g, func(gtx C) D { return s.icon(gtx, "check-circle", 64, s.p.Accent) })
			}),
			vgap(24),
			layout.Rigid(func(gtx C) D {
				return s.th.Layout(gtx, theme.TextStyle{Size: 18, LineHeight: 24, Weight: wBlack, Color: s.p.Text, Align: text.Middle}, title)
			}),
			vgap(8),
			layout.Rigid(func(gtx C) D {
				return s.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: s.p.Text, Align: text.Middle}, body)
			}),
			layout.Rigid(func(gtx C) D {
				if btn == nil {
					return D{}
				}
				return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D {
					return s.todoOutline(gtx, btn, "", label, 36, 12, 15, s.p.BorderStr, false)
				})
			}),
		)
	})
}

// ---- To-dos ----

const (
	todoFInProg = iota
	todoFDone
	todoFOverdue
	todoFUpcoming
	todoFNone
	todoFilters
)

var todoFilterLabels = [todoFilters]string{"In-progress", "Completed", "Overdue to-dos", "Upcoming reminders", "No reminder scheduled"}

var todoSortLabels = [3]string{"Due date", "Earliest date created", "Latest date created"}

var todoCleanLabels = [4]string{"Move to-dos to Saved", "Complete to-dos", "Clear to-dos", "Remove deleted messages"}

const (
	todoMenuNone = iota
	todoMenuFilter
	todoMenuSort
	todoMenuClean
)

// Later is the To-dos page (route "later").
type Later struct {
	savedBase

	feed savedFeed
	list widget.List
	view []core.SavedItem

	f      [todoFilters]bool
	sortBy int
	menu   int

	addBtn, emptyAdd, cleanBtn, filterBtn, sortBtn widget.Clickable
	selAll                                         widget.Clickable
	fclicks                                        [todoFilters]widget.Clickable
	sclicks                                        [len(todoSortLabels)]widget.Clickable
	cclicks                                        [len(todoCleanLabels)]widget.Clickable
	scrim                                          widget.Clickable
	popTag                                         int

	kb *vkList // TodosPage's InteractiveList keyboard (todoKey)

	// the sections' collapsed state and dividers (later_sections.go)
	secCollapsed [3]bool
	secClick     [3]widget.Clickable
}

// NewLater builds the To-dos page.
func NewLater(h Host) *Later {
	l := &Later{savedBase: newSavedBase(h)}
	l.todos = true
	l.feed.filter = core.SavedFilterTodos
	l.feeds = []*savedFeed{&l.feed}
	l.list.Axis = layout.Vertical
	l.kb = newVKList(&l.list)
	return l
}

// statusWant: no status checked means In-progress.
func (l *Later) statusWant() (inprog, done bool) {
	inprog, done = l.f[todoFInProg], l.f[todoFDone]
	if !inprog && !done {
		inprog = true
	}
	return
}

func (l *Later) active() []*savedFeed { return l.feeds }

// inProgress lists the loaded to-dos that aren't completed.
func (l *Later) inProgress() []core.SavedItem {
	var out []core.SavedItem
	for _, it := range l.feed.items {
		if !laterCompleted(&it) && it.Channel != "" && it.TS != "" {
			out = append(out, it)
		}
	}
	return out
}

func (l *Later) update(gtx C) {
	again := l.begin(gtx)
	for _, f := range l.active() {
		if !f.first.started() || again {
			l.reload(f)
		}
	}
	l.applyFeeds()
	l.drainDone()
	if l.retry.Clicked(gtx) {
		for _, f := range l.active() {
			l.reload(f)
		}
	}
	l.rowActions(gtx)

	if l.addBtn.Clicked(gtx) || l.emptyAdd.Clicked(gtx) {
		l.openAdd()
	}
	toggle := func(m int) {
		if l.menu == m {
			l.menu = todoMenuNone
		} else {
			l.menu = m
		}
	}
	if l.filterBtn.Clicked(gtx) {
		toggle(todoMenuFilter)
	}
	if l.sortBtn.Clicked(gtx) {
		toggle(todoMenuSort)
	}
	if l.cleanBtn.Clicked(gtx) {
		toggle(todoMenuClean)
	}
	if l.scrim.Clicked(gtx) {
		l.menu = todoMenuNone
	}
	if l.selAll.Clicked(gtx) {
		all := true
		for _, v := range l.f {
			all = all && v
		}
		for i := range l.f {
			l.f[i] = !all
		}
		l.filtersChanged()
	}
	for i := range l.fclicks {
		if l.fclicks[i].Clicked(gtx) {
			l.f[i] = !l.f[i]
			l.filtersChanged()
		}
	}
	for i := range l.sclicks {
		if l.sclicks[i].Clicked(gtx) {
			l.sortBy, l.menu, l.dirty = i, todoMenuNone, true
			l.list.Position = layout.Position{}
		}
	}
	for i := range l.cclicks {
		if l.cclicks[i].Clicked(gtx) {
			l.menu = todoMenuNone
			l.cleanUp(i)
		}
	}
	if l.dirty {
		l.rebuild()
	}
}

func (l *Later) filtersChanged() {
	l.dirty = true
	l.list.Position = layout.Position{}
}

// cleanUp runs a clean-up menu action.
func (l *Later) cleanUp(i int) {
	todo := l.inProgress()
	switch i {
	case 0: // Move to-dos to Saved
		if len(todo) == 0 {
			l.h.Toast("No to-dos to move")
			return
		}
		for j := range todo {
			l.remove(laterKey(&todo[j]))
		}
		l.write("", "Moved "+plural(len(todo), "to-do")+" to Saved", true, func(ctx context.Context, c *core.App) error {
			return laterEach(ctx, todo, func(it core.SavedItem) error { return c.MoveToSaved(ctx, it.Channel, it.TS) })
		})
	case 1: // Complete to-dos
		if len(todo) == 0 {
			l.h.Toast("No to-dos to complete")
			return
		}
		for j := range l.feed.items {
			if it := &l.feed.items[j]; !laterCompleted(it) {
				it.TodoState, it.DateCompleted = "completed", time.Now().Unix()
			}
		}
		l.dirty = true
		l.write("", "Completed "+plural(len(todo), "to-do"), true, func(ctx context.Context, c *core.App) error {
			return laterEach(ctx, todo, func(it core.SavedItem) error { return c.CompleteTodo(ctx, it.Channel, it.TS) })
		})
	case 2: // Clear to-dos: remove every to-do (asks first)
		l.confirmClear()
	case 3: // Remove deleted messages: unsave the tombstones
		var gone []core.SavedItem
		for _, it := range l.feed.items {
			if it.ItemType == "message" && it.Msg == nil && it.Channel != "" && it.TS != "" {
				gone = append(gone, it)
			}
		}
		if len(gone) == 0 {
			l.h.Toast("No deleted messages to remove")
			return
		}
		for j := range gone {
			l.remove(laterKey(&gone[j]))
		}
		l.write("", "Removed "+plural(len(gone), "deleted message"), true, func(ctx context.Context, c *core.App) error {
			return laterEach(ctx, gone, func(it core.SavedItem) error { return c.Unsave(ctx, it.Channel, it.TS) })
		})
	}
}

// confirmClear asks before removing every loaded to-do (in progress and
// completed) from To-dos and Saved.
func (l *Later) confirmClear() {
	var all []core.SavedItem
	for _, it := range l.feed.items {
		if it.Channel != "" && it.TS != "" {
			all = append(all, it)
		}
	}
	if len(all) == 0 {
		l.h.Toast("No to-dos to clear")
		return
	}
	ph, ok := l.h.(popups.Host)
	if !ok {
		return
	}
	body := "This removes " + plural(len(all), "to-do") + " from To-dos and Saved. The messages themselves stay where they are."
	ph.ShowOverlay(popups.NewModal(ph, popups.ModalOpts{
		Title: "Clear to-dos?",
		Body: func(gtx C) D {
			return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D { return l.para(gtx, 15, 22, wReg, l.p.Text, 0, body) })
		},
		Buttons: []*popups.ModalButton{
			{Label: "Cancel", Kind: popups.BtnOutline},
			{Label: "Clear to-dos", Kind: popups.BtnDanger, Do: func() bool {
				for j := range all {
					l.remove(laterKey(&all[j]))
				}
				l.write("", "Cleared "+plural(len(all), "to-do"), true, func(ctx context.Context, c *core.App) error {
					return laterEach(ctx, all, func(it core.SavedItem) error { return c.Unsave(ctx, it.Channel, it.TS) })
				})
				return true
			}},
		},
	}))
}

// todoAdd is the "Add To-do" dialog. Core makes to-dos out of messages
// (AddTodo / MoveToTodos), so it lists the Saved items, each with Add.
type todoAdd struct {
	l     *Later
	res   async[laterResult]
	adds  map[string]*widget.Clickable
	added map[string]bool
}

func (l *Later) openAdd() {
	ph, ok := l.h.(popups.Host)
	if !ok {
		return
	}
	a := &todoAdd{l: l, adds: map[string]*widget.Clickable{}, added: map[string]bool{}}
	c := l.h.Core()
	a.res.load(l.h, false, func(ctx context.Context) (laterResult, error) {
		p, err := c.Saved(ctx, core.SavedFilterSaved, "")
		return laterResult{page: p, seq: 1}, err
	})
	ph.ShowOverlay(popups.NewModal(ph, popups.ModalOpts{
		Title:   "Add a to-do",
		Body:    a.body,
		Buttons: []*popups.ModalButton{{Label: "Done", Kind: popups.BtnOutline}},
	}))
}

func (a *todoAdd) body(gtx C) D {
	l := a.l
	v, loaded, err := a.res.get()
	var items []core.SavedItem
	for _, it := range v.page.Items {
		if !laterIsTodo(&it) && it.Channel != "" && it.TS != "" && it.Msg != nil && len(items) < 20 {
			items = append(items, it)
		}
	}
	for k, c := range a.adds {
		if !c.Clicked(gtx) || a.added[k] {
			continue
		}
		for _, it := range items {
			if laterKey(&it) == k {
				ch, ts := it.Channel, it.TS
				a.added[k] = true
				l.write("", "Added to To-dos", true, func(ctx context.Context, c *core.App) error {
					return c.MoveToTodos(ctx, ch, ts, 0)
				})
			}
		}
	}
	msg := func(s string) D {
		return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D { return l.para(gtx, 15, 22, wReg, l.p.Text, 0, s) })
	}
	switch {
	case !loaded:
		return l.skeleton(gtx, 3, laterAvatar)
	case err != nil:
		return msg("Couldn’t load your saved items: " + friendlyErr(err))
	case len(items) == 0:
		return msg("Pick a saved message to turn into a to-do. Nothing is saved yet: save a message, or use Add to-do on any message.")
	}
	ch := []layout.FlexChild{
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D {
				return l.para(gtx, 13, 18, wReg, l.p.Muted, 1, "Pick a saved message to turn into a to-do.")
			})
		}),
	}
	for _, it := range items {
		it := it
		k := laterKey(&it)
		c, ok := a.adds[k]
		if !ok {
			c = new(widget.Clickable)
			a.adds[k] = c
		}
		name, av := l.authorOf(it.Msg)
		ch = append(ch, layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 6, Bottom: 6}.Layout(gtx, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D { return l.avatar(gtx, av, 28) }),
					hgap(8),
					layout.Flexed(1, func(gtx C) D {
						return col(gtx,
							layout.Rigid(func(gtx C) D {
								return l.para(gtx, 13, 18, wBold, l.p.Muted, 1, l.convTitle(it.Channel, v.page.Convs)+" · "+name)
							}),
							layout.Rigid(func(gtx C) D { return l.para(gtx, 15, 22, wReg, l.p.Text, 1, l.preview(it.Msg)) }),
						)
					}),
					hgap(8),
					layout.Rigid(func(gtx C) D {
						if a.added[k] {
							return l.para(gtx, 13, 18, wBold, l.p.Green, 1, "Added")
						}
						return l.button(gtx, c, btnOutline, true, "Add")
					}),
				)
			})
		}))
	}
	return col(gtx, ch...)
}

// laterEach applies f to every item, continuing past failures, and
// returns the first error.
func laterEach(ctx context.Context, items []core.SavedItem, f func(core.SavedItem) error) error {
	var first error
	for _, it := range items {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := f(it); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// rebuild applies the reminder filters and the sort.
func (l *Later) rebuild() {
	l.dirty = false
	l.view = l.view[:0]
	wantIn, wantDone := l.statusWant()
	anyRem := l.f[todoFOverdue] || l.f[todoFUpcoming] || l.f[todoFNone]
	keep := func(it *core.SavedItem) bool {
		if done := laterCompleted(it); (done && !wantDone) || (!done && !wantIn) {
			return false
		}
		if !anyRem {
			return true
		}
		switch {
		case it.DateDue == 0:
			return l.f[todoFNone]
		case laterOverdue(it):
			return l.f[todoFOverdue]
		default:
			return l.f[todoFUpcoming]
		}
	}
	for _, f := range l.active() {
		for i := range f.items {
			if keep(&f.items[i]) {
				l.view = append(l.view, f.items[i])
			}
		}
	}
	v := l.view
	switch l.sortBy {
	case 1:
		sort.SliceStable(v, func(i, j int) bool { return v[i].DateCreated < v[j].DateCreated })
	case 2:
		sort.SliceStable(v, func(i, j int) bool { return v[i].DateCreated > v[j].DateCreated })
	default: // due date first (soonest), then the rest newest first
		sort.SliceStable(v, func(i, j int) bool {
			a, b := v[i].DateDue, v[j].DateDue
			switch {
			case a > 0 && b > 0:
				return a < b
			case a > 0 || b > 0:
				return a > 0
			}
			return v[i].DateCreated > v[j].DateCreated
		})
	}
	// W9x8 n: the in-progress to-dos, then the completed ones
	sort.SliceStable(v, func(i, j int) bool { return !laterCompleted(&v[i]) && laterCompleted(&v[j]) })
}

// Layout draws the header, the toolbar, the list and any open menu.
func (l *Later) Layout(gtx C) D {
	l.frame()
	l.update(gtx)
	size := gtx.Constraints.Max
	gtx.Constraints = layout.Exact(size)
	fill(gtx, image.Rectangle{Max: size}, l.p.Bg)
	col(gtx,
		layout.Rigid(l.todoHeader),
		vgap(todoToolY),
		layout.Rigid(l.toolbar),
		vgap(16),
		layout.Flexed(1, l.layoutList),
	)
	l.menus(gtx)
	return D{Size: size}
}

// todoHeader: h 50, padding 0 8 0 20, 1px bottom border; "To-dos" 18/24
// black #1d1c1d; "Add To-do" (outline h 28) and the clean-up menu button.
func (l *Later) todoHeader(gtx C) D {
	w := gtx.Constraints.Max.X
	h := gtx.Dp(todoHeadH)
	hl := max(1, gtx.Dp(1))
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, h-hl))
	layout.Inset{Left: 20, Right: 8}.Layout(g, func(gtx C) D {
		return layout.W.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.Y = 0
			return row(gtx, layout.Middle,
				layout.Flexed(1, func(gtx C) D { return l.para(gtx, 18, 24, wBlack, l.p.Text, 1, "To-dos") }),
				layout.Rigid(func(gtx C) D {
					return l.todoOutline(gtx, &l.addBtn, "check-circle", "Add To-do", 28, 12, 13, l.p.BorderStr, false)
				}),
				hgap(8),
				layout.Rigid(func(gtx C) D {
					return l.todoOutline(gtx, &l.cleanBtn, "sparkle", "", 28, 8, 13, l.p.Border, true)
				}),
			)
		})
	})
	fill(gtx, image.Rect(0, h-hl, w, h), l.p.Border)
	return D{Size: image.Pt(w, h)}
}

// toolbar: "Filter" (h 32, padding 0 12) on the left, the sort button
// (h 28) on the right, 16px side padding.
func (l *Later) toolbar(gtx C) D {
	w := gtx.Constraints.Max.X
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, gtx.Dp(todoToolH)))
	label := "Filter"
	if n := l.filterCount(); n > 0 {
		label += " (" + strconv.Itoa(n) + ")"
	}
	layout.Inset{Left: 16, Right: 16}.Layout(g, func(gtx C) D {
		return layout.W.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.Y = 0
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D {
					return l.todoOutline(gtx, &l.filterBtn, "filter", label, 32, 12, 13, l.p.Border, true)
				}),
				layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Max.X, 0)} }),
				layout.Rigid(func(gtx C) D {
					return l.todoOutline(gtx, &l.sortBtn, "sort", "", 28, 8, 13, l.p.Border, true)
				}),
			)
		})
	})
	return D{Size: image.Pt(w, gtx.Dp(todoToolH))}
}

func (l *Later) filterCount() int {
	n := 0
	for _, v := range l.f {
		if v {
			n++
		}
	}
	return n
}

func (l *Later) layoutList(gtx C) D {
	feeds := l.active()
	loaded, total := true, 0
	var err error
	for _, f := range feeds {
		_, ok, e := f.first.get()
		loaded = loaded && ok
		if e != nil {
			err = e
		}
		total += len(f.items)
	}
	n := len(l.view)
	switch {
	case n == 0 && !loaded:
		return l.skeleton(gtx, 6, laterAvatar)
	case total == 0 && err != nil:
		return l.errorState(gtx, err, &l.retry)
	case total == 0:
		return l.savedEmpty(gtx, "No to-dos yet", "Add a to-do from any message, or write your own.", &l.emptyAdd, "Add To-do")
	case n == 0:
		return l.savedEmpty(gtx, "No matching to-dos", "Try changing your filters.", nil, "")
	}
	busy := false
	for _, f := range feeds {
		busy = busy || f.more.busy()
	}
	extra := 0
	if busy {
		extra = 1
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	// the rows: section dividers and to-dos (later_sections.go)
	rows := l.todoRows(todoNow())
	n = len(rows)
	items := make([]vkItem, 0, len(l.view))
	for ri, r := range rows {
		if r.kind == tdItem {
			items = append(items, vkItem{key: laterKey(&l.view[r.item]), elem: ri, focusable: true})
		}
	}
	l.kb.setItems(items)
	if l.kb.initial == "" && len(items) > 0 { // InteractiveList: the first row
		l.kb.initial = items[0].key
	}
	l.kb.update(gtx, gtx.Constraints.Max.Y, vkOpts{Key: l.todoKey, Extra: todoKeyFilters(l.kb), NoGlobal: true})
	l.kb.begin()
	dims := l.scrollList(gtx, &l.list, n+extra, func(gtx C, i int) D {
		if i >= n {
			return l.skeleton(gtx, 1, laterAvatar)
		}
		r := rows[i]
		if r.kind != tdItem {
			return l.kb.elem(i, l.todoSectionRow(gtx, r))
		}
		it := &l.view[r.item]
		d := l.itemRow(gtx, it, true)
		if l.kb.ringOn(laterKey(it)) {
			// .todoRow[role=listitem]:focus-visible:after: inset 5px,
			// radius 8
			in := gtx.Dp(5)
			l.vkRing(gtx, image.Rect(in, in, d.Size.X-in, d.Size.Y-in), gtx.Dp(8))
		}
		return l.kb.elem(i, d)
	})
	l.kb.area(gtx, image.Rectangle{Max: dims.Size})
	if p := l.list.Position; p.First+p.Count >= n-3 {
		for _, f := range feeds {
			l.loadMore(f)
		}
	}
	return dims
}

// menus draws the open popover over a transparent scrim that closes it.
func (l *Later) menus(gtx C) {
	if l.menu == todoMenuNone {
		return
	}
	size := gtx.Constraints.Max
	g := gtx
	g.Constraints = layout.Exact(size)
	l.scrim.Layout(g, func(gtx C) D { return D{Size: size} })
	toolBottom := gtx.Dp(todoHeadH) + gtx.Dp(todoToolY) + gtx.Dp(todoToolH)
	switch l.menu {
	case todoMenuFilter:
		l.todoPopover(gtx, image.Pt(gtx.Dp(16), toolBottom+gtx.Dp(4)), 260, &l.popTag, l.filterMenu)
	case todoMenuSort:
		w := unit.Dp(240)
		l.todoPopover(gtx, image.Pt(size.X-gtx.Dp(16)-gtx.Dp(w), toolBottom+gtx.Dp(2)), w, &l.popTag, l.sortMenu)
	case todoMenuClean:
		w := unit.Dp(260)
		y := (gtx.Dp(todoHeadH)+gtx.Dp(28))/2 + gtx.Dp(4)
		l.todoPopover(gtx, image.Pt(size.X-gtx.Dp(8)-gtx.Dp(w), y), w, &l.popTag, l.cleanMenu)
	}
}

func (l *Later) filterMenu(gtx C) D {
	all := true
	for _, v := range l.f {
		all = all && v
	}
	check := func(on bool) W { return func(gtx C) D { return l.todoCheck(gtx, on) } }
	ch := []layout.FlexChild{
		layout.Rigid(func(gtx C) D { return l.menuHeading(gtx, "Filter by", 15, wBold, l.p.Text, 32) }),
		layout.Rigid(func(gtx C) D { return l.menuRow(gtx, &l.selAll, check(all), "Select all", nil) }),
		layout.Rigid(l.menuDivider),
		layout.Rigid(func(gtx C) D { return l.menuHeading(gtx, "Status", 13, wReg, l.p.Muted, 24) }),
	}
	item := func(i int) layout.FlexChild {
		return layout.Rigid(func(gtx C) D { return l.menuRow(gtx, &l.fclicks[i], check(l.f[i]), todoFilterLabels[i], nil) })
	}
	ch = append(ch, item(todoFInProg), item(todoFDone),
		layout.Rigid(l.menuDivider),
		layout.Rigid(func(gtx C) D { return l.menuHeading(gtx, "Reminders", 13, wReg, l.p.Muted, 24) }),
		item(todoFOverdue), item(todoFUpcoming), item(todoFNone),
	)
	return col(gtx, ch...)
}

func (l *Later) sortMenu(gtx C) D {
	ch := []layout.FlexChild{
		layout.Rigid(func(gtx C) D { return l.menuHeading(gtx, "Sort by", 13, wReg, l.p.Muted, 24) }),
	}
	for i := range todoSortLabels {
		i := i
		var trail W
		if i == l.sortBy {
			trail = func(gtx C) D { return l.icon(gtx, "check", 16, l.p.Text) }
		}
		ch = append(ch, layout.Rigid(func(gtx C) D { return l.menuRow(gtx, &l.sclicks[i], nil, todoSortLabels[i], trail) }))
	}
	return col(gtx, ch...)
}

func (l *Later) cleanMenu(gtx C) D {
	ch := []layout.FlexChild{
		layout.Rigid(func(gtx C) D { return l.menuHeading(gtx, "Clean up", 13, wReg, l.p.Muted, 24) }),
	}
	for i := range todoCleanLabels {
		i := i
		ch = append(ch, layout.Rigid(func(gtx C) D { return l.menuRow(gtx, &l.cclicks[i], nil, todoCleanLabels[i], nil) }))
	}
	return col(gtx, ch...)
}

// ---- Saved ----

// Saved is the Home-sidebar Saved page (route "saved"): saved messages
// that aren't to-dos (core.SavedFilterSaved), newest saved first.
type Saved struct {
	savedBase
	feed savedFeed
	list widget.List
}

// NewSaved builds the Saved page.
func NewSaved(h Host) *Saved {
	s := &Saved{savedBase: newSavedBase(h)}
	s.feed.filter = core.SavedFilterSaved
	s.feeds = []*savedFeed{&s.feed}
	s.list.Axis = layout.Vertical
	return s
}

// Layout draws the 49px header and the list.
func (s *Saved) Layout(gtx C) D {
	s.frame()
	if again := s.begin(gtx); again || !s.feed.first.started() {
		s.reload(&s.feed)
	}
	s.applyFeeds()
	s.drainDone()
	if s.retry.Clicked(gtx) {
		s.reload(&s.feed)
	}
	s.rowActions(gtx)
	s.dirty = false

	size := gtx.Constraints.Max
	gtx.Constraints = layout.Exact(size)
	fill(gtx, image.Rectangle{Max: size}, s.p.Bg)
	return col(gtx,
		layout.Rigid(func(gtx C) D {
			w, h := gtx.Constraints.Max.X, gtx.Dp(savedHeadH)
			hl := max(1, gtx.Dp(1))
			g := gtx
			g.Constraints = layout.Exact(image.Pt(w, h-hl))
			layout.Inset{Left: 18, Right: 12}.Layout(g, func(gtx C) D {
				return layout.W.Layout(gtx, func(gtx C) D {
					gtx.Constraints.Min.Y = 0
					return s.para(gtx, 18, 24, wBlack, s.p.Text, 1, "Saved")
				})
			})
			// Slack's empty Saved page has no line under the header.
			if _, loaded, _ := s.feed.first.get(); !loaded || len(s.feed.items) > 0 {
				fill(gtx, image.Rect(0, h-hl, w, h), s.p.Border)
			}
			return D{Size: image.Pt(w, h)}
		}),
		layout.Flexed(1, s.layoutList),
	)
}

func (s *Saved) layoutList(gtx C) D {
	_, loaded, err := s.feed.first.get()
	items := s.feed.items
	n := len(items)
	switch {
	case n == 0 && !loaded:
		return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D { return s.skeleton(gtx, 6, laterAvatar) })
	case n == 0 && err != nil:
		return s.errorState(gtx, err, &s.retry)
	case n == 0:
		return s.savedEmpty(gtx, "Nothing saved yet", "Save a message to keep it here.", nil, "")
	}
	extra := 0
	if s.feed.more.busy() {
		extra = 1
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	dims := s.scrollList(gtx, &s.list, n+extra, func(gtx C, i int) D {
		if i >= n {
			return s.skeleton(gtx, 1, laterAvatar)
		}
		return s.itemRow(gtx, &items[i], false)
	})
	if p := s.list.Position; p.First+p.Count >= n-3 {
		s.loadMore(&s.feed)
	}
	return dims
}

// ---- small widgets (To-dos/Saved) ----

// todoOutline is an outline button: h hgt, padding 0 padX, radius 8, 1px
// border bc, optional 16px icon, bold label and 16px caret (gap 4, or 2
// for icon + caret only).
func (b *base) todoOutline(gtx C, c *widget.Clickable, icon, label string, hgt, padX unit.Dp, size unit.Sp, bc color.NRGBA, caret bool) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		gtx.Constraints.Min = image.Point{}
		gap := unit.Dp(4)
		if label == "" {
			gap = 2
		}
		m := op.Record(gtx.Ops)
		d := layout.Inset{Left: padX, Right: padX}.Layout(gtx, func(gtx C) D {
			g := gtx
			g.Constraints.Min.Y = gtx.Dp(hgt)
			g.Constraints.Max.Y = gtx.Dp(hgt)
			return layout.W.Layout(g, func(gtx C) D {
				gtx.Constraints.Min.Y = 0
				var ch []layout.FlexChild
				if icon != "" {
					ch = append(ch, layout.Rigid(func(gtx C) D { return b.icon(gtx, icon, 16, b.p.Text) }))
				}
				if label != "" {
					if icon != "" {
						ch = append(ch, hgap(gap))
					}
					ch = append(ch, layout.Rigid(func(gtx C) D { return b.label(gtx, size, wBold, b.p.Text, label) }))
				}
				if caret {
					ch = append(ch, hgap(gap), layout.Rigid(func(gtx C) D { return b.icon(gtx, "caret-down", 16, b.p.Sec) }))
				}
				return row(gtx, layout.Middle, ch...)
			})
		})
		call := m.Stop()
		r := image.Rectangle{Max: d.Size}
		rad := gtx.Dp(8)
		rrect(gtx, r, rad, b.p.Bg)
		if c.Hovered() {
			rrect(gtx, r, rad, b.p.Hover)
		}
		border(gtx, r, rad, max(1, gtx.Dp(1)), bc)
		call.Add(gtx.Ops)
		return d
	})
}

// todoPopover draws content in Slack's menu card (w wide, padding 8 0,
// radius 8, 1px ring + 0 4px 12px shadow) at a view-local point; clicks
// on its padding don't reach the scrim below.
func (b *base) todoPopover(gtx C, at image.Point, w unit.Dp, tag *int, content W) {
	pw := gtx.Dp(w)
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Min: image.Pt(pw, 0), Max: image.Pt(pw, gtx.Constraints.Max.Y)}
	d := layout.Inset{Top: 8, Bottom: 8}.Layout(g, content)
	call := m.Stop()
	if at.X+d.Size.X > gtx.Constraints.Max.X-gtx.Dp(8) {
		at.X = gtx.Constraints.Max.X - gtx.Dp(8) - d.Size.X
	}
	at.X = max(at.X, 0)
	defer op.Offset(at).Push(gtx.Ops).Pop()
	r := image.Rectangle{Max: d.Size}
	rad := gtx.Dp(8)
	theme.Shadow(gtx, r, rad, gtx.Dp(6), argb(0x1f000000))
	rrect(gtx, r, rad, b.p.Surface)
	border(gtx, r, rad, max(1, gtx.Dp(1)), b.p.Ring)
	blk := clip.Rect(r).Push(gtx.Ops)
	event.Op(gtx.Ops, tag)
	blk.Pop()
	call.Add(gtx.Ops)
}

// menuRow: 28px row, padding 0 16, optional leading widget (12px gap),
// 15px label, optional trailing widget; hover fill.
func (b *base) menuRowTodo(gtx C, c *widget.Clickable, lead W, label string, trail W) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		w, h := gtx.Constraints.Max.X, gtx.Dp(28)
		if c.Hovered() {
			fill(gtx, image.Rect(0, 0, w, h), b.p.Hover)
		}
		g := gtx
		g.Constraints = layout.Exact(image.Pt(w, h))
		layout.Inset{Left: 16, Right: 16}.Layout(g, func(gtx C) D {
			return layout.W.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.Y = 0
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D {
						if lead == nil {
							return D{}
						}
						return layout.Inset{Right: 12}.Layout(gtx, lead)
					}),
					layout.Flexed(1, func(gtx C) D { return b.label(gtx, 15, wReg, b.p.Text, label) }),
					layout.Rigid(func(gtx C) D {
						if trail == nil {
							return D{}
						}
						return trail(gtx)
					}),
				)
			})
		})
		return D{Size: image.Pt(w, h)}
	})
}

func (l *Later) menuRow(gtx C, c *widget.Clickable, lead W, label string, trail W) D {
	return l.menuRowTodo(gtx, c, lead, label, trail)
}

func (l *Later) menuHeading(gtx C, s string, size unit.Sp, w font.Weight, col color.NRGBA, h unit.Dp) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	g := gtx
	g.Constraints = layout.Exact(image.Pt(gtx.Constraints.Max.X, gtx.Dp(h)))
	layout.Inset{Left: 16, Right: 16}.Layout(g, func(gtx C) D {
		return layout.W.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.Y = 0
			return l.label(gtx, size, w, col, s)
		})
	})
	return D{Size: g.Constraints.Max}
}

func (l *Later) menuDivider(gtx C) D {
	return layout.Inset{Top: 8, Bottom: 8}.Layout(gtx, func(gtx C) D { return hline(gtx, l.p.Border) })
}

// todoCheck: 14px checkbox, radius 4, 1px #5e5d60@.45 border; checked is
// link blue with a white check.
func (b *base) todoCheck(gtx C, on bool) D {
	s := gtx.Dp(14)
	sz := image.Pt(s, s)
	r := image.Rectangle{Max: sz}
	rad := gtx.Dp(4)
	if on {
		rrect(gtx, r, rad, b.p.Link)
		g := gtx
		g.Constraints = layout.Exact(sz)
		layout.Center.Layout(g, func(gtx C) D { return b.icon(gtx, "check", 11, rgb(0xffffff)) })
	} else {
		rrect(gtx, r, rad, b.p.Bg)
		border(gtx, r, rad, max(1, gtx.Dp(1)), b.p.BorderStr)
	}
	return D{Size: sz}
}

// ---- shared hover rows (DMs, To-dos, Saved, Drafts) ----

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
	border(gtx, r, rad, max(1, gtx.Dp(1)), b.p.Ring)
	call.Add(gtx.Ops)
	return d
}

// ---- keyboard (TodosPage) ----

// todoKeyFilters are the InteractiveList keys besides ↑/↓, PageUp/Down,
// Home/End and Enter, and the row keys (Delete, ⌫ on a Mac, E, W).
func todoKeyFilters(tag *vkList) []key.Filter {
	fs := []key.Filter{
		{Focus: tag, Name: key.NameLeftArrow}, {Focus: tag, Name: key.NameRightArrow},
		{Focus: tag, Name: key.NameDeleteForward}, {Focus: tag, Name: "E"}, {Focus: tag, Name: "W"},
	}
	if runtime.GOOS == "darwin" {
		fs = append(fs, key.Filter{Focus: tag, Name: key.NameDeleteBackward})
	}
	return fs
}

// todoKey is InteractiveList's onKeyDown (shared-js later-todos-ui
// todos-page, DERT): ↑/← and ↓/→ step without wrapping, PageUp/PageDown
// jump by pages of 10 rows, Home/End go to either end; a key first brings
// focus back to the initial row when another one is active. Moving
// focuses the row, which the browser scrolls into view. Then the row's
// onKeyDown (rT, no modifiers): Enter opens the item, Delete (⌫ on a Mac)
// clears it with an undoable "To-do cleared" toast, E toggles it complete
// and W archived.
func (l *Later) todoKey(e key.Event, active string) bool {
	k := l.kb
	plain := e.Modifiers == 0
	ids := make([]string, len(k.items))
	for i, it := range k.items {
		ids[i] = it.key
	}
	at := -1
	for i, id := range ids {
		if id == k.initial {
			at = i
		}
	}
	set := func(i int) {
		if i >= 0 && i < len(ids) {
			k.setActive(ids[i], true)
			k.reveal(ids[i])
		}
	}
	back := func() bool { // z(): refocus the initial row
		if k.initial != k.active {
			set(at)
			return true
		}
		return false
	}
	const page = 10
	switch e.Name {
	case key.NameUpArrow, key.NameLeftArrow:
		if plain && !back() && at > 0 {
			set(at - 1)
		}
		return true
	case key.NameDownArrow, key.NameRightArrow:
		if plain && !back() && at < len(ids)-1 {
			set(at + 1)
		}
		return true
	case key.NamePageUp:
		if !back() {
			p := 0
			if at > 0 {
				p = at / page
			}
			if p > 1 {
				set((p - 1) * page)
			} else {
				set(0)
			}
		}
		return true
	case key.NamePageDown:
		if !back() {
			n := len(ids)
			pages := (n + page - 1) / page
			p := 0
			if at > 0 {
				p = at / page
			}
			if p == pages-1 {
				set(n - 1)
			} else {
				set((p + 1) * page)
			}
		}
		return true
	case key.NameHome:
		if !back() {
			set(0)
		}
		return true
	case key.NameEnd:
		if !back() {
			set(len(ids) - 1)
		}
		return true
	}
	if !plain || active == "" {
		return true
	}
	f, i := l.find(active)
	if f == nil {
		return true
	}
	it := f.items[i]
	switch e.Name {
	case key.NameReturn, key.NameEnter:
		l.open(&it)
	case key.NameDeleteForward, key.NameDeleteBackward:
		l.clearTodo(it)
	case "E":
		l.toggleDone(f, i)
	case "W":
		if it.ItemType == "reminder" {
			return true
		}
		on := !it.Archived
		ch, ts := it.Channel, it.TS
		l.write1(it, true, "", func(ctx context.Context, c *core.App) error {
			return c.UpdateSaved(ctx, ch, ts, core.SavedUpdate{Archived: &on})
		})
	}
	return true
}

// clearTodo is "Clear a saved item with a deferred, undoable toast"
// (WHky): the row goes at once, "To-do cleared" offers Undo, and the
// clear is sent once the toast is gone.
func (l *Later) clearTodo(it core.SavedItem) {
	if it.Channel == "" || it.TS == "" {
		return
	}
	k := laterKey(&it)
	ch, ts := it.Channel, it.TS
	l.remove(k)
	send := func() {
		l.write(k, "", true, func(ctx context.Context, c *core.App) error { return c.Unsave(ctx, ch, ts) })
	}
	tu, ok := l.h.(interface{ ToastUndo(string, func()) })
	if !ok {
		l.h.Toast("To-do cleared")
		send()
		return
	}
	var undone atomic.Bool
	tu.ToastUndo("To-do cleared", func() {
		undone.Store(true)
		delete(l.gone, k)
		for _, f := range l.feeds {
			l.reload(f)
		}
	})
	time.AfterFunc(todoUndoFor, func() {
		if !undone.Load() {
			send()
		}
	})
}

// todoUndoFor is how long the Undo toast stays (App.ToastUndo).
var todoUndoFor = 6 * time.Second
