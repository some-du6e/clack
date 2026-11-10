package composer

import (
	"image"
	"time"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// scheduleePicker is Slack's "Custom time" scheduler: a modal with a month
// calendar and hour/minute/AM-PM steppers, returning the chosen time.
type schedulePicker struct {
	h       Host
	sel     time.Time // chosen date+time (local)
	view    time.Time // first day of the shown month
	onPick  func(time.Time)
	prevMon widget.Clickable
	nextMon widget.Clickable
	days    [42]widget.Clickable
	hUp     widget.Clickable
	hDown   widget.Clickable
	mUp     widget.Clickable
	mDown   widget.Clickable
	ampm    widget.Clickable
}

// openSchedulePicker shows the custom-time modal (Slack's
// ScheduleMessageDialog: "Schedule message", Cancel and "Schedule
// Message", which is disabled outside 5 minutes to 120 days from now),
// starting at start (getDefaultCustomTime). onPick gets the chosen time.
func openSchedulePicker(h Host, start time.Time, onPick func(time.Time)) {
	p := &schedulePicker{h: h, sel: start, onPick: onPick}
	p.view = time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, start.Location())
	m := popups.NewModal(h, popups.ModalOpts{
		Title: "Schedule message",
		Width: 360,
		Body:  p.body,
		Buttons: []*popups.ModalButton{
			{Label: "Cancel", Kind: popups.BtnOutline},
			{Label: "Schedule Message", Kind: popups.BtnPrimary, Enabled: p.valid, Do: func() bool {
				if p.onPick != nil && p.valid() {
					p.onPick(p.sel)
				}
				return true
			}},
		},
	})
	h.ShowOverlay(m)
}

// valid is the dialog's submit rule: getMinimumCustomTime to
// getMaximumCustomTime.
func (p *schedulePicker) valid() bool {
	now := time.Now()
	return !p.sel.Before(schedMinTime(now)) && !p.sel.After(schedMaxTime(now))
}

func (p *schedulePicker) body(gtx layout.Context) layout.Dimensions {
	p.handle(gtx)
	th := p.h.Theme()
	w := gtx.Constraints.Max.X
	y := 0
	// Month header: ◀  November 2026  ▶
	hdrH := gtx.Dp(32)
	p.monthHeader(gtx, th, w, hdrH)
	y += hdrH + gtx.Dp(4)
	// Weekday labels.
	off := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
	p.weekdays(gtx, th, w)
	off.Pop()
	y += gtx.Dp(20)
	// Calendar grid (6 rows).
	off = op.Offset(image.Pt(0, y)).Push(gtx.Ops)
	rows := p.calendar(gtx, th, w)
	off.Pop()
	y += rows
	y += gtx.Dp(16)
	// Time row.
	off = op.Offset(image.Pt(0, y)).Push(gtx.Ops)
	p.timeRow(gtx, th, w)
	off.Pop()
	y += gtx.Dp(44)
	return layout.Dimensions{Size: image.Pt(w, y)}
}

func (p *schedulePicker) handle(gtx layout.Context) {
	if p.prevMon.Clicked(gtx) {
		p.view = p.view.AddDate(0, -1, 0)
	}
	if p.nextMon.Clicked(gtx) {
		p.view = p.view.AddDate(0, 1, 0)
	}
	for i := range p.days {
		if p.days[i].Clicked(gtx) {
			if d := p.dayAt(i); d > 0 {
				p.sel = time.Date(p.view.Year(), p.view.Month(), d, p.sel.Hour(), p.sel.Minute(), 0, 0, p.sel.Location())
			}
		}
	}
	step := func(d time.Duration) { p.sel = p.sel.Add(d) }
	if p.hUp.Clicked(gtx) {
		step(time.Hour)
	}
	if p.hDown.Clicked(gtx) {
		step(-time.Hour)
	}
	if p.mUp.Clicked(gtx) {
		step(5 * time.Minute)
	}
	if p.mDown.Clicked(gtx) {
		step(-5 * time.Minute)
	}
	if p.ampm.Clicked(gtx) {
		step(12 * time.Hour)
	}
}

func (p *schedulePicker) monthHeader(gtx layout.Context, th *theme.Theme, w, h int) {
	arrow := func(c *widget.Clickable, x int, next bool) {
		off := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
		sz := image.Pt(gtx.Dp(28), h)
		c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			if c.Hovered() {
				fillRRect(gtx, image.Rectangle{Max: sz}, gtx.Dp(4), withAlpha(th.C.Fg, 0x14))
				pointer.CursorPointer.Add(gtx.Ops)
			}
			lbl := "‹"
			if next {
				lbl = "›"
			}
			gtx.Constraints = layout.Exact(sz)
			return center(gtx, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, 18, theme.Bold, th.C.Fg, lbl)
			})
		})
		off.Pop()
	}
	arrow(&p.prevMon, 0, false)
	arrow(&p.nextMon, w-gtx.Dp(28), true)
	off := op.Offset(image.Pt(0, 0)).Push(gtx.Ops)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, h))
	center(g, func(gtx layout.Context) layout.Dimensions {
		return th.Label(gtx, 15, theme.Bold, th.C.Fg, p.view.Format("January 2006"))
	})
	off.Pop()
}

func (p *schedulePicker) weekdays(gtx layout.Context, th *theme.Theme, w int) {
	cw := w / 7
	for i, d := range [...]string{"Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"} {
		off := op.Offset(image.Pt(i*cw, 0)).Push(gtx.Ops)
		g := gtx
		g.Constraints = layout.Exact(image.Pt(cw, gtx.Dp(18)))
		center(g, func(gtx layout.Context) layout.Dimensions {
			return th.Label(gtx, 11, theme.Bold, th.C.FgMuted, d)
		})
		off.Pop()
	}
}

// firstWeekday is the column (0=Sun) of the 1st of the shown month.
func (p *schedulePicker) firstWeekday() int { return int(p.view.Weekday()) }

func (p *schedulePicker) daysInMonth() int {
	return p.view.AddDate(0, 1, -1).Day()
}

// dayAt maps a grid cell (0..41) to a day of the month, or 0 if empty.
func (p *schedulePicker) dayAt(i int) int {
	d := i - p.firstWeekday() + 1
	if d < 1 || d > p.daysInMonth() {
		return 0
	}
	return d
}

func (p *schedulePicker) calendar(gtx layout.Context, th *theme.Theme, w int) int {
	cw := w / 7
	ch := gtx.Dp(30)
	today := time.Now()
	rows := (p.firstWeekday() + p.daysInMonth() + 6) / 7
	for i := 0; i < rows*7; i++ {
		d := p.dayAt(i)
		col, row := i%7, i/7
		off := op.Offset(image.Pt(col*cw, row*ch)).Push(gtx.Ops)
		sz := image.Pt(cw, ch)
		if d == 0 {
			off.Pop()
			continue
		}
		date := time.Date(p.view.Year(), p.view.Month(), d, 0, 0, 0, 0, p.view.Location())
		past := date.Before(time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location()))
		selected := d == p.sel.Day() && p.view.Month() == p.sel.Month() && p.view.Year() == p.sel.Year()
		p.days[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(sz)
			pad := gtx.Dp(2)
			cell := image.Rect(pad, pad, sz.X-pad, sz.Y-pad)
			col := th.C.Fg
			switch {
			case selected:
				fillRRect(gtx, cell, gtx.Dp(6), th.C.Link)
				col = th.C.SendText
			case past:
				col = withAlpha(th.C.Fg, 0x55)
			case p.days[i].Hovered():
				fillRRect(gtx, cell, gtx.Dp(6), withAlpha(th.C.Fg, 0x12))
				pointer.CursorPointer.Add(gtx.Ops)
			}
			return center(gtx, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, 13, theme.Regular, col, itoa(d))
			})
		})
		off.Pop()
	}
	return rows * ch
}

func (p *schedulePicker) timeRow(gtx layout.Context, th *theme.Theme, w int) {
	h12 := p.sel.Hour() % 12
	if h12 == 0 {
		h12 = 12
	}
	ap := "AM"
	if p.sel.Hour() >= 12 {
		ap = "PM"
	}
	// stepper draws ▲ value ▼ for a label.
	x := 0
	stepper := func(up, down *widget.Clickable, val string) {
		colW := gtx.Dp(56)
		off := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
		// value box
		boxH := gtx.Dp(40)
		fillRRect(gtx, image.Rect(0, 0, colW, boxH), gtx.Dp(6), th.C.BgInset)
		g := gtx
		g.Constraints = layout.Exact(image.Pt(colW-gtx.Dp(18), boxH))
		vo := op.Offset(image.Pt(0, 0)).Push(gtx.Ops)
		center(g, func(gtx layout.Context) layout.Dimensions {
			return th.Label(gtx, 17, theme.Bold, th.C.Fg, val)
		})
		vo.Pop()
		arr := func(c *widget.Clickable, yy int, lbl string) {
			ao := op.Offset(image.Pt(colW-gtx.Dp(18), yy)).Push(gtx.Ops)
			c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				s := image.Pt(gtx.Dp(18), gtx.Dp(20))
				gtx.Constraints = layout.Exact(s)
				if c.Hovered() {
					pointer.CursorPointer.Add(gtx.Ops)
				}
				return center(gtx, func(gtx layout.Context) layout.Dimensions {
					return th.Label(gtx, 10, theme.Bold, th.C.FgMuted, lbl)
				})
			})
			ao.Pop()
		}
		arr(up, 0, "▲")
		arr(down, gtx.Dp(20), "▼")
		off.Pop()
		x += colW + gtx.Dp(8)
	}
	stepper(&p.hUp, &p.hDown, itoa(h12))
	// colon
	co := op.Offset(image.Pt(x-gtx.Dp(8)-gtx.Dp(2), gtx.Dp(8))).Push(gtx.Ops)
	g := gtx
	g.Constraints.Min = image.Point{}
	th.Label(g, 17, theme.Bold, th.C.Fg, ":")
	co.Pop()
	stepper(&p.mUp, &p.mDown, pad2(p.sel.Minute()))
	// AM/PM toggle
	ao := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
	p.ampm.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		s := image.Pt(gtx.Dp(48), gtx.Dp(40))
		gtx.Constraints = layout.Exact(s)
		fillRRect(gtx, image.Rectangle{Max: s}, gtx.Dp(6), th.C.BgInset)
		if p.ampm.Hovered() {
			pointer.CursorPointer.Add(gtx.Ops)
		}
		return center(gtx, func(gtx layout.Context) layout.Dimensions {
			return th.Label(gtx, 15, theme.Bold, th.C.Fg, ap)
		})
	})
	ao.Pop()
}

// center lays w centred in the current constraints.
func center(gtx layout.Context, w layout.Widget) layout.Dimensions {
	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Point{}
		return w(gtx)
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func pad2(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}
