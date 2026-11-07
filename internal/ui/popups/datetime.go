package popups

import (
	"image"
	"image/color"
	"strconv"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Slack's date and time controls (Out of office Start/End, Reminder,
// Pause notifications, working hours), measured from app.slack.com:
//
//   - date/time fields: radius 8, border 1px #7c7a7f, white bg. "small"
//     28px tall / 13px text (OOO rows), "large" 36px / 15px (dialogs).
//     Leading calendar or clock icon, trailing chevron. While the popover
//     is open the field gets the focus ring and the date shows in full
//     ("November 6th, 2026").
//   - calendar (datepick.png): card 350 wide, padding 24; header 48px
//     «  ‹  "November 2026" 15/700  ›  » (« » = year, ‹ › = month);
//     weekday row Su…Sa 13/700 #454447, 30px; 7×6 grid of 43×41 cells with
//     1px #dddddd lines, numbers 15px. Days before the minimum: bg #f8f8f8,
//     #616061, disabled. Selected: #1264a3 cell, 34px white 1px ring, white
//     number. Hover: #f8f8f8. Arrows move, Enter picks, Escape closes.
//   - time list (timepick.png): 138 wide (or the field's width if wider),
//     28px rows 15px with 24px padding, 12px top/bottom padding, max 264
//     tall (scrolls), 30-minute steps; the current value and the hovered
//     row are #1264a3 / white. Scrolled to the current value.
//
// Window coordinates: Gio exposes no "current transform", so a field
// cannot find out where it is on screen by itself. Owners pass the field's
// window-space origin to Layout. Modal bodies get theirs from
// dtModalOrigin: Modal.rect (set by Modal.Layout before it lays out the
// body, so it is current for the visible pass) + the body padding/header
// height - the Modal's own scroll offset; owners that scroll their body
// themselves subtract their list offset too. The field keeps the resulting
// rect and anchors its popover overlay (shown with h.ShowOverlay) to it.

type dtSize uint8

const (
	dtSmall dtSize = iota // 28px, 13px text
	dtLarge               // 36px, 15px text
)

type dtStyle uint8

const (
	dtRelative dtStyle = iota // Today / Tomorrow / Mon, Nov 9
	dtLong                    // November 6th, 2026
)

// ---- pure date/time math (tested) ----

// dtDay is local midnight of t's day.
func dtDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// dtAt is day at mins minutes after midnight (wall clock; overflow rolls
// into the next day).
func dtAt(day time.Time, mins int) time.Time {
	y, m, d := day.Date()
	return time.Date(y, m, d, 0, mins, 0, 0, day.Location())
}

// dtMins is t's wall-clock minutes after midnight.
func dtMins(t time.Time) int { return t.Hour()*60 + t.Minute() }

func dtSameDay(a, b time.Time) bool {
	y1, m1, d1 := a.Date()
	y2, m2, d2 := b.Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}

// dtCeil rounds t up to the next multiple of step wall-clock minutes (t's
// minute itself when t is exactly on one).
func dtCeil(t time.Time, step int) time.Time {
	if step < 1 {
		step = 1
	}
	m := dtMins(t)
	if t.Second() != 0 || t.Nanosecond() != 0 {
		m++
	}
	m = (m + step - 1) / step * step
	return dtAt(t, m)
}

// dtMonthStart is the first day of t's month.
func dtMonthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}

// dtMonthGrid lays month out on a Sunday-first 7×6 grid: day numbers, 0
// for cells outside the month.
func dtMonthGrid(month time.Time) [42]int {
	var g [42]int
	first := dtMonthStart(month)
	n := time.Date(first.Year(), first.Month()+1, 0, 0, 0, 0, 0, first.Location()).Day()
	off := int(first.Weekday())
	for i := 0; i < n; i++ {
		g[off+i] = i + 1
	}
	return g
}

// dtTimeSlots lists the times (minutes after midnight) from the first
// multiple of step at or after from, up to the end of the day.
func dtTimeSlots(from, step int) []int {
	if from < 0 {
		from = 0
	}
	start := (from + step - 1) / step * step
	var out []int
	for m := start; m < 24*60; m += step {
		out = append(out, m)
	}
	return out
}

// dtDateLabel formats a field's date.
func dtDateLabel(day, now time.Time, style dtStyle) string {
	if style == dtLong {
		return day.Format("January ") + ordinal(day.Day()) + day.Format(", 2006")
	}
	today := dtDay(now)
	switch {
	case dtSameDay(day, today):
		return "Today"
	case dtSameDay(day, today.AddDate(0, 0, 1)):
		return "Tomorrow"
	case day.Year() == now.Year():
		return day.Format("Mon, Jan 2")
	}
	return day.Format("Mon, Jan 2, 2006")
}

// dtTimeLabel formats minutes after midnight as "3:30 PM".
func dtTimeLabel(mins int) string {
	return clock(time.Date(2000, 1, 1, 0, mins, 0, 0, time.UTC))
}

// ---- drawing ----

// dtModalOrigin is the window position of m's body origin (see the note at
// the top of this file).
func dtModalOrigin(gtx layout.Context, m *Modal) image.Point {
	if m == nil {
		return image.Point{}
	}
	px, hh := float32(28), float32(70)
	if m.o.PadX > 0 {
		px = m.o.PadX
	}
	if m.o.HeadH > 0 {
		hh = m.o.HeadH
	}
	return m.rect.Min.Add(image.Pt(dp(gtx, px), dp(gtx, hh)-m.list.Position.Offset))
}

// dtBorder is Slack's select border (#7c7a7f).
func dtBorder(th *theme.Theme) color.NRGBA {
	if th != nil && th.Dark {
		return rgb(0x868686)
	}
	return rgb(0x7c7a7f)
}

func dtHeight(gtx layout.Context, sz dtSize) int {
	if sz == dtSmall {
		return dp(gtx, 28)
	}
	return dp(gtx, 36)
}

// dtField draws a Slack select box w px wide: optional leading icon, the
// label, optional chevron. open draws the focus ring. Clicks land on btn.
func dtField(gtx layout.Context, th *theme.Theme, btn *widget.Clickable, w int, sz dtSize, iconName, label string, chevron, open, disabled bool) layout.Dimensions {
	cl := colorsOf(th)
	fs, pad, isz, gap, csz := float32(13), float32(8), float32(14), float32(7), float32(14)
	if sz == dtLarge {
		fs, pad, isz, gap, csz = 15, 12, 16, 5, 16
	}
	hh := dtHeight(gtx, sz)
	draw := func(gtx layout.Context) layout.Dimensions {
		r := image.Rect(0, 0, w, hh)
		rad := dp(gtx, 8)
		switch {
		case open:
			ring := dp(gtx, 3)
			rrect(gtx, r.Inset(-ring), rad+ring, cl.FocusRing)
			rrect(gtx, r, rad, cl.Focus)
		case disabled:
			rrect(gtx, r, rad, theme.WithAlpha(dtBorder(th), 0x66))
		default:
			b := dtBorder(th)
			if btn.Hovered() {
				b = cl.Fg2
			}
			rrect(gtx, r, rad, b)
		}
		bg := cl.Bg
		if disabled {
			bg = cl.BgAlt
		}
		rrect(gtx, r.Inset(1), rad-1, bg)
		fg := cl.Fg
		if disabled {
			fg = cl.Muted
		}
		x := dp(gtx, pad)
		if iconName != "" {
			at(gtx, image.Pt(x, (hh-dp(gtx, isz))/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, iconName, isz, cl.Fg2)
			})
			x += dp(gtx, isz) + dp(gtx, gap)
		}
		right := w - dp(gtx, pad)
		if chevron {
			right -= dp(gtx, csz)
			at(gtx, image.Pt(right, (hh-dp(gtx, csz))/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, "chevron-down", csz, fg)
			})
			right -= dp(gtx, 4)
		}
		at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, right-x), hh)}
			return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, fs, theme.Regular, fg, label)
			})
		})
		return layout.Dimensions{Size: r.Max}
	}
	if disabled {
		return draw(gtx)
	}
	return clickBox(gtx, btn, nil, draw)
}

// ---- date field ----

// dtDate is a date select that opens the calendar popover.
type dtDate struct {
	Day      time.Time // local midnight
	Min      time.Time // earliest pickable day (zero: any)
	Style    dtStyle
	Plain    bool // no icon or chevron (Pause Notifications' input look)
	Disabled bool
	OnChange func()

	btn  widget.Clickable
	cal  *dtCalendar
	rect image.Rectangle // window coords, last layout
}

func (d *dtDate) open() bool { return d.cal != nil && !d.cal.Dismissed() }

// Layout draws the field w px wide at the current origin; org is that
// origin in window coordinates.
func (d *dtDate) Layout(gtx layout.Context, h Host, org image.Point, w int, sz dtSize) layout.Dimensions {
	th := h.Theme()
	d.rect = image.Rectangle{Min: org, Max: org.Add(image.Pt(w, dtHeight(gtx, sz)))}
	if d.Day.IsZero() {
		d.Day = dtDay(time.Now())
	}
	if d.btn.Clicked(gtx) && !d.open() && !d.Disabled {
		d.cal = dtNewCalendar(h, d.rect, d.Day, d.Min, func(day time.Time) {
			d.Day = day
			if d.OnChange != nil {
				d.OnChange()
			}
			h.Invalidate()
		})
		h.ShowOverlay(d.cal)
	}
	open := d.open()
	style := d.Style
	if open {
		style = dtLong
	}
	ic, chev := "calendar", true
	if d.Plain {
		ic, chev = "", false
	}
	return dtField(gtx, th, &d.btn, w, sz, ic, dtDateLabel(d.Day, time.Now(), style), chev, open, d.Disabled)
}

// ---- time field ----

// dtTime is a time select that opens the 30-minute time list.
type dtTime struct {
	Mins     int        // minutes after midnight
	From     func() int // earliest listed time (nil: midnight)
	NoIcon   bool
	Disabled bool
	OnChange func()

	btn  widget.Clickable
	list *dtTimeList
	rect image.Rectangle
}

func (t *dtTime) open() bool { return t.list != nil && !t.list.Dismissed() }

// Layout draws the field w px wide; org is its window-space origin.
func (t *dtTime) Layout(gtx layout.Context, h Host, org image.Point, w int, sz dtSize) layout.Dimensions {
	th := h.Theme()
	t.rect = image.Rectangle{Min: org, Max: org.Add(image.Pt(w, dtHeight(gtx, sz)))}
	if t.btn.Clicked(gtx) && !t.open() && !t.Disabled {
		from := 0
		if t.From != nil {
			from = t.From()
		}
		t.list = dtNewTimeList(h, t.rect, t.Mins, from, func(m int) {
			t.Mins = m
			if t.OnChange != nil {
				t.OnChange()
			}
			h.Invalidate()
		})
		h.ShowOverlay(t.list)
	}
	ic := "clock"
	if t.NoIcon {
		ic = ""
	}
	return dtField(gtx, th, &t.btn, w, sz, ic, dtTimeLabel(t.Mins), true, t.open(), t.Disabled)
}

// ---- calendar popover ----

type dtCalendar struct {
	base
	h      Host
	anchor image.Rectangle
	month  time.Time // first of the shown month
	sel    time.Time
	min    time.Time // zero: none
	cur    time.Time // keyboard cursor
	kbd    bool
	onPick func(time.Time)
	scrim  scrim
	block  blocker
	cells  [42]widget.Clickable
	nav    [4]widget.Clickable // « ‹ › »
	rect   image.Rectangle
	focus  bool
}

func dtNewCalendar(h Host, anchor image.Rectangle, sel, lo time.Time, onPick func(time.Time)) *dtCalendar {
	sel = dtDay(sel)
	c := &dtCalendar{h: h, anchor: anchor, sel: sel, cur: sel, onPick: onPick, month: dtMonthStart(sel)}
	if !lo.IsZero() {
		c.min = dtDay(lo)
	}
	return c
}

func (c *dtCalendar) disabled(day time.Time) bool { return !c.min.IsZero() && day.Before(c.min) }

func (c *dtCalendar) dayN(n int) time.Time {
	return time.Date(c.month.Year(), c.month.Month(), n, 0, 0, 0, 0, c.month.Location())
}

func (c *dtCalendar) pick(day time.Time) {
	if c.disabled(day) {
		return
	}
	c.Dismiss()
	if c.onPick != nil {
		c.onPick(day)
	}
}

func (c *dtCalendar) moveCur(days, months int) {
	n := c.cur.AddDate(0, months, days)
	if c.disabled(n) {
		n = c.min
	}
	c.cur, c.kbd = n, true
	c.month = dtMonthStart(n)
}

func (c *dtCalendar) keys(gtx layout.Context) {
	for {
		e, ok := gtx.Event(
			key.Filter{Focus: c, Name: key.NameLeftArrow},
			key.Filter{Focus: c, Name: key.NameRightArrow},
			key.Filter{Focus: c, Name: key.NameUpArrow},
			key.Filter{Focus: c, Name: key.NameDownArrow},
			key.Filter{Focus: c, Name: key.NamePageUp},
			key.Filter{Focus: c, Name: key.NamePageDown},
			key.Filter{Focus: c, Name: key.NameReturn},
			key.Filter{Focus: c, Name: key.NameEnter},
			key.Filter{Focus: c, Name: key.NameSpace},
		)
		if !ok {
			break
		}
		ke, ok := e.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		switch ke.Name {
		case key.NameLeftArrow:
			c.moveCur(-1, 0)
		case key.NameRightArrow:
			c.moveCur(1, 0)
		case key.NameUpArrow:
			c.moveCur(-7, 0)
		case key.NameDownArrow:
			c.moveCur(7, 0)
		case key.NamePageUp:
			c.moveCur(0, -1)
		case key.NamePageDown:
			c.moveCur(0, 1)
		default:
			c.pick(c.cur)
		}
		if c.done {
			return
		}
	}
}

// Layout draws the calendar.
func (c *dtCalendar) Layout(gtx layout.Context) layout.Dimensions {
	drain()
	if c.done {
		return layout.Dimensions{}
	}
	stacked(gtx, c)
	win := gtx.Constraints.Max
	gtx.Constraints.Min = image.Point{}
	if !c.focus {
		gtx.Execute(key.FocusCmd{Tag: c})
		c.focus = true
	}
	if isTop(c) && escape(gtx, nil) {
		c.Dismiss()
		return layout.Dimensions{}
	}
	c.keys(gtx)
	if c.done {
		return layout.Dimensions{}
	}
	if c.scrim.Layout(gtx) {
		c.Dismiss()
		return layout.Dimensions{}
	}
	for i := range c.nav {
		if c.nav[i].Clicked(gtx) {
			c.month = c.month.AddDate([4]int{-1, 0, 0, 1}[i], [4]int{0, -1, 1, 0}[i], 0)
			c.kbd = false
		}
	}
	grid := dtMonthGrid(c.month)
	for i := range c.cells {
		if grid[i] > 0 && c.cells[i].Clicked(gtx) {
			c.pick(c.dayN(grid[i]))
			if c.done {
				return layout.Dimensions{}
			}
		}
	}
	th := c.h.Theme()
	cl := colorsOf(th)
	pad, cw, ch := dp(gtx, 24), dp(gtx, 43), dp(gtx, 41)
	headH, wkH := dp(gtx, 48), dp(gtx, 30)
	sz := image.Pt(maxInt(dp(gtx, 350), 2*pad+7*cw), 2*pad+headH+wkH+6*ch)
	a := c.anchor
	a.Min.X = a.Max.X - sz.X // right-aligned with the field, like Slack
	pos := place(a, sz, win, dp(gtx, 8), Below)
	c.rect = image.Rectangle{Min: pos, Max: pos.Add(sz)}
	keyTarget(gtx, c, c.rect)
	st := dtDropdownClass.Enter(gtx, 0, 0)
	defer st.Leave()
	if !st.Hidden() {
		at(gtx, pos, func(gtx layout.Context) layout.Dimensions {
			return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
				pos := image.Point{}
				local := image.Rectangle{Max: sz}
				card(gtx, cl, local, dp(gtx, 8))
				c.block.Layout(gtx, local)

				// header: « ‹ Month Year › »
				bs := dp(gtx, 32)
				by := pos.Y + pad + (headH-bs)/2
				bx := [4]int{pos.X + pad, pos.X + pad + bs, pos.X + sz.X - pad - 2*bs, pos.X + sz.X - pad - bs}
				for i := range c.nav {
					at(gtx, image.Pt(bx[i], by), func(gtx layout.Context) layout.Dimensions {
						return clickBox(gtx, &c.nav[i], func(gtx layout.Context, s image.Point, hovered bool) {
							if hovered {
								rrect(gtx, image.Rectangle{Max: s}, dp(gtx, 4), cl.BgHover)
							}
						}, func(gtx layout.Context) layout.Dimensions {
							name := "chevron-left"
							if i >= 2 {
								name = "chevron-right"
							}
							// Slack's glyphs are ~16px tall chevrons (a 26px icon box);
							// the doubles are two of them 6px apart.
							const gs = 26
							is := dp(gtx, gs)
							o := (bs - is) / 2
							if i == 0 || i == 3 { // double chevron
								at(gtx, image.Pt(o-dp(gtx, 4), o), func(gtx layout.Context) layout.Dimensions { return icon(gtx, name, gs, cl.Fg2) })
								at(gtx, image.Pt(o+dp(gtx, 4), o), func(gtx layout.Context) layout.Dimensions { return icon(gtx, name, gs, cl.Fg2) })
							} else {
								at(gtx, image.Pt(o, o), func(gtx layout.Context) layout.Dimensions { return icon(gtx, name, gs, cl.Fg2) })
							}
							return layout.Dimensions{Size: image.Pt(bs, bs)}
						})
					})
				}
				title := c.month.Format("January 2006")
				tw := textW(gtx, th, 15, theme.Bold, title)
				at(gtx, image.Pt(pos.X+(sz.X-tw)/2, pos.Y+pad), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, headH, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 15, theme.Bold, cl.Fg, title)
					})
				})

				// weekdays
				gx := pos.X + (sz.X-7*cw)/2
				wy := pos.Y + pad + headH
				for i, wd := range [...]string{"Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"} {
					w := textW(gtx, th, 13, theme.Bold, wd)
					at(gtx, image.Pt(gx+i*cw+(cw-w)/2, wy), func(gtx layout.Context) layout.Dimensions {
						return vcenter(gtx, wkH, func(gtx layout.Context) layout.Dimensions {
							return text(gtx, th, 13, theme.Bold, cl.Fg2, wd)
						})
					})
				}

				// grid
				gy := wy + wkH
				line := cl.Tab
				for i, n := range grid {
					if n == 0 {
						continue
					}
					day := c.dayN(n)
					r := image.Rect(gx+(i%7)*cw, gy+(i/7)*ch, gx+(i%7+1)*cw, gy+(i/7+1)*ch)
					off := c.disabled(day)
					sel := day.Equal(c.sel)
					curs := c.kbd && day.Equal(c.cur)
					cell := func(gtx layout.Context) layout.Dimensions {
						s := r.Size()
						box := image.Rectangle{Max: s}
						if off || (!sel && (curs || c.cells[i].Hovered())) {
							fill(gtx, box, cl.BgAlt)
						}
						fg := cl.Fg
						if off {
							fg = cl.Muted
						}
						if sel {
							rrect(gtx, box, dp(gtx, 4), cl.Highlight)
							d := dp(gtx, 34)
							ctr := image.Pt(s.X/2, s.Y/2)
							theme.Circle(gtx, ctr, d, cl.HighText)
							theme.Circle(gtx, ctr, d-2*maxInt(1, dp(gtx, 1)), cl.Highlight)
							fg = cl.HighText
						} else if curs {
							stroke(gtx, box, dp(gtx, 4), maxInt(1, dp(gtx, 2)), cl.Focus)
						}
						num := dtNum(n)
						nw := textW(gtx, th, 15, theme.Regular, num)
						at(gtx, image.Pt((s.X-nw)/2, 0), func(gtx layout.Context) layout.Dimensions {
							return vcenter(gtx, s.Y, func(gtx layout.Context) layout.Dimensions {
								return text(gtx, th, 15, theme.Regular, fg, num)
							})
						})
						return layout.Dimensions{Size: s}
					}
					at(gtx, r.Min, func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints = layout.Exact(r.Size())
						if off {
							return cell(gtx)
						}
						return clickBox(gtx, &c.cells[i], nil, cell)
					})
				}
				// grid lines in one pass over the cells (every day cell gets all four
				// edges, so the last row is closed like Slack's).
				lw := maxInt(1, dp(gtx, 1))
				for i, n := range grid {
					if n == 0 {
						continue
					}
					r := image.Rect(gx+(i%7)*cw, gy+(i/7)*ch, gx+(i%7+1)*cw, gy+(i/7+1)*ch)
					fill(gtx, image.Rect(r.Min.X, r.Min.Y, r.Max.X+lw, r.Min.Y+lw), line)
					fill(gtx, image.Rect(r.Min.X, r.Max.Y, r.Max.X+lw, r.Max.Y+lw), line)
					fill(gtx, image.Rect(r.Min.X, r.Min.Y, r.Min.X+lw, r.Max.Y+lw), line)
					fill(gtx, image.Rect(r.Max.X, r.Min.Y, r.Max.X+lw, r.Max.Y+lw), line)
				}
				return layout.Dimensions{Size: sz}
			})
		})
	}
	return layout.Dimensions{Size: win}
}

// dtNums are the day-of-month labels (no per-frame formatting).
var dtNums = func() (s [32]string) {
	for i := range s {
		s[i] = strconv.Itoa(i)
	}
	return
}()

func dtNum(n int) string {
	if n < 0 || n >= len(dtNums) {
		return ""
	}
	return dtNums[n]
}

// ---- time list popover ----

type dtTimeList struct {
	base
	h      Host
	anchor image.Rectangle
	slots  []int
	labels []string
	sel    int // index of the current value (-1: not a slot)
	cur    int // keyboard row (-1: none)
	onPick func(int)
	list   layout.List
	clicks []widget.Clickable
	scrim  scrim
	block  blocker
	rect   image.Rectangle
	focus  bool
	rows   int // visible rows, last layout
}

func dtNewTimeList(h Host, anchor image.Rectangle, value, from int, onPick func(int)) *dtTimeList {
	slots := dtTimeSlots(from, 30)
	if len(slots) == 0 {
		slots = dtTimeSlots(0, 30)
	}
	t := &dtTimeList{h: h, anchor: anchor, slots: slots, sel: -1, cur: -1, onPick: onPick}
	t.list.Axis = layout.Vertical
	t.clicks = make([]widget.Clickable, len(slots))
	t.labels = make([]string, len(slots))
	first := -1
	for i, m := range slots {
		t.labels[i] = dtTimeLabel(m)
		if m == value {
			t.sel = i
		}
		if first < 0 && m >= value {
			first = i
		}
	}
	if first > 0 {
		t.list.Position.First = first
	}
	return t
}

func (t *dtTimeList) pick(i int) {
	if i < 0 || i >= len(t.slots) {
		return
	}
	t.Dismiss()
	if t.onPick != nil {
		t.onPick(t.slots[i])
	}
}

func (t *dtTimeList) move(d int) {
	i := t.cur
	if i < 0 {
		i = t.sel
	}
	if i < 0 {
		i = t.list.Position.First - d
	}
	i += d
	if i < 0 {
		i = 0
	}
	if i >= len(t.slots) {
		i = len(t.slots) - 1
	}
	t.cur = i
	// keep the cursor visible
	if i < t.list.Position.First {
		t.list.Position.First, t.list.Position.Offset = i, 0
	} else if n := maxInt(1, t.rows); i >= t.list.Position.First+n {
		t.list.Position.First, t.list.Position.Offset = i-n+1, 0
	}
}

// Layout draws the list.
func (t *dtTimeList) Layout(gtx layout.Context) layout.Dimensions {
	drain()
	if t.done {
		return layout.Dimensions{}
	}
	stacked(gtx, t)
	win := gtx.Constraints.Max
	gtx.Constraints.Min = image.Point{}
	if !t.focus {
		gtx.Execute(key.FocusCmd{Tag: t})
		t.focus = true
	}
	if isTop(t) && escape(gtx, nil) {
		t.Dismiss()
		return layout.Dimensions{}
	}
	for {
		e, ok := gtx.Event(
			key.Filter{Focus: t, Name: key.NameUpArrow},
			key.Filter{Focus: t, Name: key.NameDownArrow},
			key.Filter{Focus: t, Name: key.NameReturn},
			key.Filter{Focus: t, Name: key.NameEnter},
		)
		if !ok {
			break
		}
		ke, ok := e.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		switch ke.Name {
		case key.NameUpArrow:
			t.move(-1)
		case key.NameDownArrow:
			t.move(1)
		default:
			i := t.cur
			if i < 0 {
				i = t.sel
			}
			t.pick(i)
			return layout.Dimensions{}
		}
	}
	if t.scrim.Layout(gtx) {
		t.Dismiss()
		return layout.Dimensions{}
	}
	for i := range t.clicks {
		if t.clicks[i].Clicked(gtx) {
			t.pick(i)
			return layout.Dimensions{}
		}
	}
	th := t.h.Theme()
	cl := colorsOf(th)
	rowH, padY := dp(gtx, 28), dp(gtx, 12)
	w := maxInt(dp(gtx, 138), t.anchor.Dx())
	listH := minInt(len(t.slots)*rowH, dp(gtx, 264)-2*padY)
	t.rows = listH / maxInt(1, rowH)
	sz := image.Pt(w, listH+2*padY)
	a := t.anchor
	a.Min.X = (t.anchor.Min.X+t.anchor.Max.X)/2 - w/2 // centered under the field
	a.Max.X = a.Min.X + w
	pos := place(a, sz, win, dp(gtx, 8), Below)
	t.rect = image.Rectangle{Min: pos, Max: pos.Add(sz)}
	card(gtx, cl, t.rect, dp(gtx, 4))
	t.block.Layout(gtx, t.rect)
	keyTarget(gtx, t, t.rect)

	cs := clipRRect(gtx, t.rect, dp(gtx, 4))
	at(gtx, pos.Add(image.Pt(0, padY)), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(w, listH))
		return t.list.Layout(gtx, len(t.slots), func(gtx layout.Context, i int) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(w, rowH))
			return clickBox(gtx, &t.clicks[i], nil, func(gtx layout.Context) layout.Dimensions {
				s := image.Pt(w, rowH)
				fg := cl.Fg
				if i == t.sel || i == t.cur || t.clicks[i].Hovered() {
					fill(gtx, image.Rectangle{Max: s}, cl.Highlight)
					fg = cl.HighText
				}
				at(gtx, image.Pt(dp(gtx, 24), 0), func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-dp(gtx, 32)), rowH)}
					return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 15, theme.Regular, fg, t.labels[i])
					})
				})
				return layout.Dimensions{Size: s}
			})
		})
	})
	// scrollbar
	if total := len(t.slots) * rowH; total > listH {
		top := t.list.Position.First*rowH + t.list.Position.Offset
		thumb := maxInt(dp(gtx, 24), listH*listH/total)
		ty := pos.Y + padY + (listH-thumb)*minInt(top, total-listH)/maxInt(1, total-listH)
		bw := dp(gtx, 6)
		x := pos.X + w - bw - dp(gtx, 4)
		rrect(gtx, image.Rect(x, ty, x+bw, ty+thumb), bw/2, theme.WithAlpha(cl.Fg, 0x55))
	}
	cs.Pop()
	return layout.Dimensions{Size: win}
}
