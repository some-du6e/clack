package views

// "Include conversation history?", the second step of MpdmAddPeopleModal
// (boot-shared TWss) with SelectHistoryOptionForm and HistoryPreview
// (boot-shared hLnn; gantry-v2 build d3de3a487b1c, 2026-11-12; the reads
// are core/history_preview.go):
//
//   - .p-mpdm_select_history_form (sk_foreground_min_solid, padding 0
//     24px) holds the radios (padding 12px 0): "Don’t include any
//     conversation history", "From today", "From the beginning" (with the
//     first message's date: "(Today)", "(Yesterday)" or "({date})",
//     MMMM Do and the year when it isn't this year), "Custom" and its
//     DatePicker (239 wide, 25 in; "Select Date"; disabled unless Custom;
//     days before the first message and after today can't be picked). A
//     first message from today hides From today and Custom and selects
//     From the beginning.
//   - for every choice but the first, HistoryPreview: "Preview what’s
//     included" (13/18 bold sk_foreground_max_solid, margin-bottom 8,
//     4 in), then a 300px box (1px sk_foreground_low border, radius 8,
//     sk_primary_background) with the messages from the start: day
//     dividers (56px) and PreviewMessage rows (not interactive), scrolled
//     to the bottom; past 200 rows "And all other messages in this
//     conversation" (13 bold italic, 56px). Empty: "Since there aren’t
//     any messages from this time, no conversation history will be
//     included.", or for Custom without a date "Select a date to preview
//     messages" (15/22, centred, 75% wide).
//   - Done stays disabled for Custom until a date is picked; the start
//     sent is that day's local midnight (getUnixSeconds). The server's
//     too_many_messages opens the "Too many messages selected" speed bump
//     (OK closes it, Cancel the modal).

import (
	"context"
	"image"
	"image/color"
	"strconv"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/popups"
)

var nsMpdmAudience = i18n.NS("mpdm_audience_expansion") // MpdmAddPeopleModal, SelectHistoryOptionForm, HistoryPreview

// cdHistState is the history step's part beyond the radios.
type cdHistState struct {
	oldest     async[string] // the first message's ts
	fromToday  bool          // k: the first message is from today
	day        time.Time     // Custom's date (local midnight; zero: none)
	dateBtn    widget.Clickable
	dateSize   image.Point
	prev       async[[]core.Message]
	prevStart  string
	list       widget.List
	tooMany    bool
	bumpOK     widget.Clickable
	bumpCancel widget.Clickable
}

// histOpen starts the modal's first read (fetchPreviewHistory with limit 1).
func (d *ChannelDetails) histOpen(a *cdAddPeople) {
	c, id := d.h.Core(), d.id
	a.hx.list.Axis = layout.Vertical
	a.hx.oldest.load(d.h, false, func(ctx context.Context) (string, error) { return c.HistoryOldest(ctx, id) })
}

// histStartTS is the start the choice sends ("" for none, or Custom
// without a date).
func histStartTS(a *cdAddPeople, now time.Time) string {
	switch a.hist {
	case cdHistToday:
		y, m, dd := now.Date()
		return cdUnixDay(time.Date(y, m, dd, 0, 0, 0, 0, now.Location()).Unix())
	case cdHistAll:
		return core.HistoryFromBeginning
	case cdHistCustom:
		if a.hx.day.IsZero() {
			return ""
		}
		return cdUnixDay(a.hx.day.Unix())
	}
	return ""
}

// histOldestTime is the first message's time (zero while unknown).
func (a *cdAddPeople) histOldestTime() time.Time {
	ts, ok, _ := a.hx.oldest.get()
	if !ok || ts == "" {
		return time.Time{}
	}
	f, err := strconv.ParseFloat(ts, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(int64(f), 0)
}

// updateHistory runs the step's own parts each frame (before the radios
// and Done are read).
func (d *ChannelDetails) updateHistory(gtx C, a *cdAddPeople) {
	hx := &a.hx
	now := gtx.Now
	if now.IsZero() {
		now = time.Now()
	}
	// a first message from today: no From today / Custom, From the beginning
	// selected unless the org default (none) stands
	if old := a.histOldestTime(); !old.IsZero() && sameDay(old, now) && !hx.fromToday {
		hx.fromToday = true
		c := d.h.Core()
		def := c.Experiment("mpdm_default_exclude_history_clients_webapp") == "on" && c.EnterpriseOrgPrefTrue("mpdm_add_people_default_exclude_history")
		if !(def && a.hist != cdHistToday && a.hist != cdHistCustom) {
			a.hist = cdHistAll
		}
	}
	if hx.dateBtn.Clicked(gtx) && a.hist == cdHistCustom {
		d.openHistoryCalendar(a, now)
	}
	if hx.tooMany {
		if hx.bumpOK.Clicked(gtx) {
			hx.tooMany = false
		}
		if hx.bumpCancel.Clicked(gtx) {
			d.closeDialog()
			return
		}
	}
	// HistoryPreview's fetch for the chosen start
	if a.hist != cdHistNone {
		if start := histStartTS(a, now); start != "" && start != hx.prevStart {
			hx.prevStart = start
			c, id := d.h.Core(), d.id
			hx.prev.load(d.h, false, func(ctx context.Context) ([]core.Message, error) {
				return c.HistoryPreview(ctx, id, start, core.HistoryPreviewLimit)
			})
			hx.list.ScrollToEnd = true
		}
	}
}

// openHistoryCalendar opens the DatePicker's calendar under the field.
func (d *ChannelDetails) openHistoryCalendar(a *cdAddPeople, now time.Time) {
	ph, ok := d.h.(popups.Host)
	if !ok {
		return
	}
	var at image.Point
	if pp, ok := d.h.(interface{ PointerPos() image.Point }); ok {
		at = pp.PointerPos()
	}
	anchor := image.Rectangle{Min: at, Max: at.Add(image.Pt(1, 1))}
	if hs := a.hx.dateBtn.History(); len(hs) > 0 {
		// the press's own position puts the anchor on the field
		p := hs[len(hs)-1].Position
		anchor = image.Rectangle{Min: at.Sub(p), Max: at.Sub(p).Add(a.hx.dateSize)}
	}
	sel := a.hx.day
	if sel.IsZero() {
		sel = now
	}
	earliest := a.histOldestTime()
	if !earliest.IsZero() {
		earliest = dayStart(earliest)
	}
	ph.ShowOverlay(popups.NewDatePickerRange(ph, anchor, sel, earliest, now, func(day time.Time) {
		a.hx.day = dayStart(day)
		a.hist = cdHistCustom // onDateChange: onSelectOption("custom", ts)
		d.h.Invalidate()
	}))
}

func dayStart(t time.Time) time.Time {
	y, m, dd := t.Date()
	return time.Date(y, m, dd, 0, 0, 0, 0, t.Location())
}

// histBeginningLabel is "From the beginning", with the first message's
// day.
func histBeginningLabel(old, now time.Time) string {
	ns := nsMpdmAudience
	switch {
	case old.IsZero():
		return ns.T("From the beginning")
	case sameDay(old, now.AddDate(0, 0, -1)):
		return ns.T("From the beginning (Yesterday)")
	case sameDay(old, now):
		return ns.T("From the beginning (Today)")
	}
	date := old.Format("January 2") + ordinal(old.Day()) // toCalendarDate MMMMDo
	if old.Year() != now.Year() {
		date += old.Format(", 2006") // MMMMDoYYYY
	}
	return ns.T("From the beginning ({date})", "date", date)
}

// historyOptions are the radios shown, in order (hist values).
func historyOptions(a *cdAddPeople) []int {
	if a.hx.fromToday {
		return []int{cdHistNone, cdHistAll}
	}
	return []int{cdHistNone, cdHistToday, cdHistAll, cdHistCustom}
}

// historyForm is SelectHistoryOptionForm and, unless none is chosen,
// HistoryPreview.
func (d *ChannelDetails) historyForm(gtx C, a *cdAddPeople) D {
	ns := nsMpdmAudience
	now := gtx.Now
	if now.IsZero() {
		now = time.Now()
	}
	m := op.Record(gtx.Ops)
	dims := layout.Inset{Left: 24, Right: 24}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		var ch []layout.FlexChild
		ch = append(ch, layout.Rigid(cdSpace(12)))
		for _, h := range historyOptions(a) {
			h := h
			var label string
			switch h {
			case cdHistNone:
				label = ns.T("Don’t include any conversation history")
			case cdHistToday:
				label = ns.T("From today")
			case cdHistAll:
				label = histBeginningLabel(a.histOldestTime(), now)
			case cdHistCustom:
				label = ns.T("Custom")
			}
			ch = append(ch, layout.Rigid(func(gtx C) D {
				return layout.Inset{Bottom: 4}.Layout(gtx, func(gtx C) D {
					return a.histClk[h].Layout(gtx, func(gtx C) D {
						pointerCursor(gtx)
						return row(gtx, layout.Middle,
							layout.Rigid(func(gtx C) D { return cdRadio(&d.base, gtx, a.hist == h) }),
							hgap(8),
							layout.Rigid(func(gtx C) D { return d.para(gtx, 15, 22, wReg, d.p.Text, 1, label) }),
						)
					})
				})
			}))
			if h == cdHistCustom {
				ch = append(ch, layout.Rigid(func(gtx C) D {
					return layout.Inset{Left: 25, Top: 4, Bottom: 4}.Layout(gtx, func(gtx C) D { return d.historyDateField(gtx, a) })
				}))
			}
		}
		ch = append(ch, layout.Rigid(cdSpace(12)))
		if a.hist != cdHistNone {
			ch = append(ch, layout.Rigid(func(gtx C) D { return d.historyPreview(gtx, a) }), layout.Rigid(cdSpace(16)))
		}
		return col(gtx, ch...)
	})
	call := m.Stop()
	fill(gtx, image.Rectangle{Max: dims.Size}, d.minSolid())
	call.Add(gtx.Ops)
	return dims
}

// minSolid is sk_foreground_min_solid (#f8f8f8; dark #1a1d21).
func (d *ChannelDetails) minSolid() color.NRGBA {
	return d.lt(rgb(0xf8f8f8), rgb(0x1a1d21))
}

// historyDateField is the DatePicker's select: the date (YYYY-MM-DD) or
// "Select Date", the caret-down trigger; dimmed when not Custom.
func (d *ChannelDetails) historyDateField(gtx C, a *cdAddPeople) D {
	ns := nsMpdmAudience
	w := min(gtx.Dp(239), gtx.Constraints.Max.X)
	h := gtx.Dp(36)
	sz := image.Pt(w, h)
	a.hx.dateSize = sz
	disabled := a.hist != cdHistCustom
	draw := func(gtx C) D {
		r := image.Rectangle{Max: sz}
		rad := gtx.Dp(8)
		bg := d.p.Surface
		if disabled {
			bg = d.minSolid()
		}
		rrect(gtx, r, rad, bg)
		border(gtx, r, rad, max(1, gtx.Dp(1)), d.p.BorderStr)
		txt, fg := ns.T("Select Date"), d.p.Faint
		if !a.hx.day.IsZero() {
			txt, fg = a.hx.day.Format("2006-01-02"), d.p.Text
		}
		if disabled {
			fg = cdDim(fg)
		}
		dirAt(gtx, image.Pt(gtx.Dp(12), (h-gtx.Sp(22))/2), func(gtx C) D { return d.label(gtx, 15, wReg, fg, txt) })
		ic := d.p.Sec
		if disabled {
			ic = cdDim(ic)
		}
		dirAt(gtx, image.Pt(w-gtx.Dp(8)-gtx.Dp(20), (h-gtx.Dp(20))/2), func(gtx C) D { return d.icon(gtx, "caret-down", 20, ic) })
		return D{Size: sz}
	}
	if disabled {
		return draw(gtx)
	}
	return a.hx.dateBtn.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		return draw(gtx)
	})
}

// historyPreview is HistoryPreview.
func (d *ChannelDetails) historyPreview(gtx C, a *cdAddPeople) D {
	ns := nsMpdmAudience
	hx := &a.hx
	label := func(gtx C) D {
		return layout.Inset{Left: 4, Bottom: 8}.Layout(gtx, func(gtx C) D {
			return d.para(gtx, 13, 18, wBold, d.p.Muted, 1, ns.T("Preview what’s included"))
		})
	}
	box := func(gtx C) D {
		w, h := gtx.Constraints.Max.X, gtx.Dp(300)
		r := image.Rect(0, 0, w, h)
		rad := gtx.Dp(8)
		one := max(1, gtx.Dp(1))
		rrect(gtx, r, rad, d.p.Border)
		rrect(gtx, r.Inset(one), max(0, rad-one), d.p.Surface)
		defer clip.UniformRRect(r.Inset(one), max(0, rad-one)).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Exact(r.Inset(one).Size())
		defer op.Offset(image.Pt(one, one)).Push(gtx.Ops).Pop()
		empty := ""
		msgs, loaded, err := hx.prev.get()
		start := histStartTS(a, gtx.Now)
		var rows []core.HistoryRow
		if loaded && err == nil && start != "" && start == hx.prevStart {
			rows = core.HistoryPreviewRows(msgs, start, time.Local)
		}
		switch {
		case a.hist == cdHistCustom && hx.day.IsZero():
			empty = ns.T("Select a date to preview messages")
		case loaded && !hx.prev.busy() && len(rows) == 0:
			empty = ns.T("Since there aren’t any messages from this time, no conversation history will be included.")
		}
		if empty != "" {
			return layout.Center.Layout(g, func(gtx C) D {
				gtx.Constraints.Max.X = gtx.Constraints.Max.X * 3 / 4
				return d.centered(gtx, 15, wReg, d.p.Muted, empty)
			})
		}
		if len(rows) == 0 { // loading_empty: the spinner
			return layout.Center.Layout(g, func(gtx C) D { return actSpinner(gtx, gtx.Dp(24), d.p.Muted) })
		}
		d.scrollList(g, &hx.list, len(rows), func(gtx C, i int) D {
			dims := d.historyRow(gtx, rows, i)
			if i == len(rows)-1 { // p-history_preview__list_component__item--bottom
				dims.Size.Y += gtx.Dp(16)
			}
			return dims
		})
		return D{Size: r.Size()}
	}
	return col(gtx, layout.Rigid(label), layout.Rigid(box))
}

// historyRow is one preview row.
func (d *ChannelDetails) historyRow(gtx C, rows []core.HistoryRow, i int) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	r := rows[i]
	switch r.Kind {
	case "day_divider":
		return d.historyDayDivider(gtx, r.Day)
	case "more_messages":
		h := gtx.Dp(56)
		g := gtx
		g.Constraints = layout.Exact(image.Pt(gtx.Constraints.Max.X, h))
		return layout.Center.Layout(g, func(gtx C) D {
			return d.label(gtx, 13, wBold, d.p.Muted, nsMpdmAudience.T("And all other messages in this conversation"))
		})
	}
	m := r.Msg
	adjacent := false
	if i > 0 {
		if p := rows[i-1]; p.Kind == "message" && p.Msg.User == m.User && m.User != "" && tsTime(m.TS).Sub(tsTime(p.Msg.TS)) < 5*time.Minute {
			adjacent = true
		}
	}
	name, av := d.authorOf(m)
	top := unit.Dp(8)
	if adjacent {
		top = 2
	}
	return layout.Inset{Top: top, Bottom: 4, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
		return row(gtx, layout.Start,
			layout.Rigid(func(gtx C) D {
				if adjacent {
					return D{Size: image.Pt(gtx.Dp(36), 0)}
				}
				return d.avatar(gtx, av, 36)
			}),
			hgap(8),
			layout.Flexed(1, func(gtx C) D {
				return col(gtx,
					layout.Rigid(func(gtx C) D {
						if adjacent {
							return D{}
						}
						return row(gtx, layout.Baseline,
							layout.Rigid(func(gtx C) D { return d.label(gtx, 15, wBlack, d.p.Text, name) }),
							hgap(8),
							layout.Rigid(func(gtx C) D {
								return d.label(gtx, 12, wReg, d.p.Muted, tsTime(m.TS).Format("3:04 PM"))
							}),
						)
					}),
					layout.Rigid(func(gtx C) D { return d.msgBody(gtx, m, false, 0) }),
				)
			}),
		)
	})
}

// historyDayDivider is DayDivider (not sticky) in its 56px row: the line
// and the day's pill.
func (d *ChannelDetails) historyDayDivider(gtx C, day time.Time) D {
	w, h := gtx.Constraints.Max.X, gtx.Dp(56)
	ly := h / 2
	fill(gtx, image.Rect(0, ly, w, ly+max(1, gtx.Dp(1))), d.p.Border)
	label := unrDayLabel(day, gtx.Now)
	tw := d.th.Measure(gtx, 13, wBold, label)
	ph := gtx.Dp(28)
	pw := gtx.Dp(16) + tw + gtx.Dp(4) + gtx.Dp(13) + gtx.Dp(8)
	r := image.Rect(w/2-pw/2, (h-ph)/2, w/2-pw/2+pw, (h-ph)/2+ph)
	rrect(gtx, r, ph/2, d.p.Surface)
	border(gtx, r, ph/2, max(1, gtx.Dp(1)), d.p.Border)
	dirAt(gtx, image.Pt(r.Min.X+gtx.Dp(16), r.Min.Y+(ph-gtx.Dp(18))/2), func(gtx C) D {
		return d.para(gtx, 13, 18, wBold, d.p.Text, 1, label)
	})
	dirAt(gtx, image.Pt(r.Min.X+gtx.Dp(16)+tw+gtx.Dp(4), r.Min.Y+(ph-gtx.Dp(13))/2), func(gtx C) D {
		return d.icon(gtx, "chevron-down", 13, d.p.Text)
	})
	return D{Size: image.Pt(w, h)}
}

// historyBump is the "Too many messages selected" speed bump's body.
func (d *ChannelDetails) historyBump() []W {
	ns := nsMpdmAudience
	return []W{
		cdSpace(8),
		func(gtx C) D { return d.para(gtx, 15, 22, wBold, d.p.Text, 0, ns.T("Too many messages selected")) },
		cdSpace(8),
		d.cdPara("toomany", ns.T("You can only include up to 100 messages. Select another option or create a private channel instead.")),
	}
}

// ---- test hooks (the ui package's shots) ----

// HistoryCustomFromOldest picks Custom with the first message's day, as
// the calendar does; false while the first message isn't known.
func (d *ChannelDetails) HistoryCustomFromOldest() bool {
	a := d.x.dlg.add
	if a == nil || a.step != 1 {
		return false
	}
	old := a.histOldestTime()
	if old.IsZero() {
		return false
	}
	a.hist, a.hx.day = cdHistCustom, dayStart(old)
	d.h.Invalidate()
	return true
}

// HistoryPreviewLoaded reports that the preview has its messages.
func (d *ChannelDetails) HistoryPreviewLoaded() bool {
	a := d.x.dlg.add
	if a == nil {
		return false
	}
	_, ok, _ := a.hx.prev.get()
	return ok && !a.hx.prev.busy()
}
