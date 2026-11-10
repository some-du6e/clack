package kit

import (
	"image"
	"image/color"
	"strconv"
	"strings"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Selects and pickers: BasicSelect, TimePicker, DatePickerTrigger and
// DateRangePicker. Fields are Slack's select box (popups' datetime.go
// measurements): radius 8, 1px #7c7a7f border, small 28px / 13px, medium
// 36px / 15px, large 44px / 15px, a leading icon and a trailing chevron;
// the focus ring while open.

func init() {
	add(basicSelect, "BasicSelect")
	add(timePicker, "TimePicker")
	add(datePickerTrigger, "DatePickerTrigger")
	add(dateRangePicker, "DateRangePicker")
}

// ---- shared field ----

type pkMetrics struct {
	h, fs, pad, isz, gap, csz float32
}

func pkMetricsFor(size int) pkMetrics {
	switch size {
	case 0:
		return pkMetrics{28, 13, 8, 14, 7, 14}
	case 2:
		return pkMetrics{44, 15, 12, 18, 6, 18}
	}
	return pkMetrics{36, 15, 12, 16, 5, 16}
}

// pkBorder is Slack's select border (#7c7a7f).
func pkBorder(th *theme.Theme) color.NRGBA {
	if th != nil && th.Dark {
		return rgb(0x868686)
	}
	return rgb(0x7c7a7f)
}

type pkFieldOpts struct {
	W       int
	Size    int
	Icon    string // Clack icon name, "" none
	Chevron bool
	Open    bool
	Hover   bool
	Off     bool // disabled
	Invalid bool
}

// pkField draws a select box w px wide and lays content out in the text
// area (constraints: the area's width and the field height; content is
// vertically centered). fg/fs are the text color and size.
func pkField(gtx layout.Context, th *theme.Theme, o pkFieldOpts, content func(gtx layout.Context, fg color.NRGBA, fs float32) layout.Dimensions) layout.Dimensions {
	cl := colorsOf(th)
	m := pkMetricsFor(o.Size)
	hh := dp(gtx, m.h)
	w := o.W
	r := image.Rect(0, 0, w, hh)
	rad := dp(gtx, 8)
	switch {
	case o.Open:
		rg := dp(gtx, 3)
		theme.RRect(gtx, r.Inset(-rg), rad+rg, cl.FocusRing)
		theme.RRect(gtx, r, rad, cl.Focus)
	case o.Invalid:
		theme.RRect(gtx, r, rad, cl.Error)
	case o.Off:
		theme.RRect(gtx, r, rad, theme.WithAlpha(pkBorder(th), 0x66))
	default:
		b := pkBorder(th)
		if o.Hover {
			b = cl.Fg2
		}
		theme.RRect(gtx, r, rad, b)
	}
	bg := cl.Bg
	if o.Off {
		bg = cl.BgAlt
	}
	theme.RRect(gtx, r.Inset(1), rad-1, bg)
	fg := cl.Fg
	if o.Off {
		fg = cl.Muted
	}
	x := dp(gtx, m.pad)
	if o.Icon != "" {
		at(gtx, image.Pt(x, (hh-dp(gtx, m.isz))/2), func(gtx layout.Context) layout.Dimensions {
			return pkIcon(gtx, o.Icon, m.isz, cl.Fg2)
		})
		x += dp(gtx, m.isz) + dp(gtx, m.gap)
	}
	right := w - dp(gtx, m.pad)
	if o.Chevron {
		right -= dp(gtx, m.csz)
		at(gtx, image.Pt(right, (hh-dp(gtx, m.csz))/2), func(gtx layout.Context) layout.Dimensions {
			return pkIcon(gtx, "chevron-down", m.csz, fg)
		})
		right -= dp(gtx, 4)
	}
	if content != nil {
		at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(max(1, right-x), hh)}
			return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
				return content(gtx, fg, m.fs)
			})
		})
	}
	return layout.Dimensions{Size: r.Max}
}

// pkTextContent is field content drawing one line (muted when placeholder).
func pkTextContent(th *theme.Theme, s string, placeholder bool) func(gtx layout.Context, fg color.NRGBA, fs float32) layout.Dimensions {
	return func(gtx layout.Context, fg color.NRGBA, fs float32) layout.Dimensions {
		if placeholder {
			fg = colorsOf(th).Placeholder
		}
		return label(gtx, th, fs, theme.Regular, fg, s)
	}
}

// pkWidth resolves a width prop; def (dp) when unset (negative: fill the
// container), capped by the constraints.
func pkWidth(gtx layout.Context, v any, def float32) int {
	full := gtx.Constraints.Max.X
	w, ok := cssLen(gtx, v, full)
	if !ok {
		switch {
		case def >= 0:
			w = dp(gtx, def)
		case full > 0 && full < 1<<20:
			w = full // fill the container, like Slack's 100% select
		default:
			w = dp(gtx, 300)
		}
	}
	if full > 0 && full < 1<<20 && w > full {
		w = full
	}
	if w < dp(gtx, 40) {
		w = dp(gtx, 40)
	}
	return w
}

func pkToDp(gtx layout.Context, px int) float32 {
	s := gtx.Metric.PxPerDp
	if s == 0 {
		s = 1
	}
	return float32(px) / s
}

// pkRect is a widget's window rectangle from its origin tracker.
func pkRect(o *origin, sz image.Point) image.Rectangle {
	p := o.toWin(image.Point{})
	return image.Rectangle{Min: p, Max: p.Add(sz)}
}

// ---- BasicSelect ----

func pkAsProps(v any) slackc.Props {
	switch m := v.(type) {
	case slackc.Props:
		return m
	case map[string]any:
		return slackc.Props(m)
	}
	return nil
}

// pkValKey is a comparable key for an option value: strings and numbers
// compare by their text ("1" matches 1), like loose DOM select values.
func pkValKey(v any) (string, bool) {
	switch v := v.(type) {
	case string:
		return v, true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32), true
	case int:
		return strconv.Itoa(v), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case bool:
		return strconv.FormatBool(v), true
	}
	return "", false
}

// pkOptionValue is an option's value (the option itself when it is a
// bare value).
func pkOptionValue(o any) any {
	if p := pkAsProps(o); p != nil {
		return p["value"]
	}
	return o
}

// pkMatchOption returns the index of the option whose value matches sel's
// (an option object or a bare value), or -1.
func pkMatchOption(opts []any, sel any) int {
	if sel == nil {
		return -1
	}
	k, ok := pkValKey(pkOptionValue(sel))
	if !ok {
		return -1
	}
	for i, o := range opts {
		if k2, ok := pkValKey(pkOptionValue(o)); ok && k2 == k {
			return i
		}
	}
	return -1
}

// pkOptionLabel is an option's label as text (Element labels flattened).
func pkOptionLabel(o any) string {
	p := pkAsProps(o)
	if p == nil {
		return valueText(o)
	}
	if s := valueText(p["label"]); s != "" {
		return s
	}
	return valueText(p["value"])
}

type selectState struct {
	props slackc.Props
	btn   widget.Clickable
	org   origin
	menu  *popups.Menu
	local int // uncontrolled selection (index+1; 0 none)
}

func (s *selectState) open() bool { return s.menu != nil && !s.menu.Dismissed() }

func basicSelect(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	st := stateOf[selectState](h)
	st.props = h.Props
	th := thm()
	p := h.Props
	opts := p.List("options")
	off := p.Bool("isDisabled")
	sel := -1
	if p.Has("selectedOption") {
		sel = pkMatchOption(opts, p["selectedOption"])
	} else if st.local > 0 && st.local <= len(opts) {
		sel = st.local - 1
	}
	w := pkWidth(gtx, p["width"], -1)
	size := sizeOf(p, 1)
	hh := dp(gtx, pkMetricsFor(size).h)

	if !off && st.btn.Clicked(gtx) && !st.open() {
		items := make([]popups.MenuItem, len(opts))
		for i, o := range opts {
			opt := o
			idx := i
			items[i] = popups.MenuItem{Label: pkOptionLabel(o), Checked: i == sel, Do: func() {
				st.local = idx + 1
				st.props.Invoke("onSelectionChange", opt)
				invalidate()
			}}
		}
		st.menu = popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: pkRect(&st.org, image.Pt(w, hh)), Width: pkToDp(gtx, w), Raw: true})
		showOverlay(st.menu)
	}

	content := func(gtx layout.Context, fg color.NRGBA, fs float32) layout.Dimensions {
		if sel < 0 {
			return layout.Dimensions{}
		}
		o := pkAsProps(opts[sel])
		if o != nil {
			if el, ok := o["label"].(slackc.Element); ok {
				wt := 400
				return styled(h, fg, fs, wt, func() layout.Dimensions { return h.Nodes(gtx, el.Nodes) })
			}
		}
		return label(gtx, th, fs, theme.Regular, fg, pkOptionLabel(opts[sel]))
	}
	draw := func(gtx layout.Context) layout.Dimensions {
		d := pkField(gtx, th, pkFieldOpts{W: w, Size: size, Chevron: true, Open: st.open(), Hover: st.btn.Hovered(), Off: off}, content)
		if !off {
			cursor(gtx, d.Size, pointer.CursorPointer)
		}
		return d
	}
	var d layout.Dimensions
	if off {
		d = draw(gtx)
	} else {
		d = st.btn.Layout(gtx, draw)
	}
	st.org.track(gtx, image.Rectangle{Max: d.Size})
	return d
}

// ---- TimePicker ----

// pkParseHM parses a 24-hour "HH:MM" value into minutes after midnight.
func pkParseHM(s string) (int, bool) {
	s = strings.TrimSpace(s)
	i := strings.IndexByte(s, ':')
	if i < 1 || i == len(s)-1 {
		return 0, false
	}
	h, err1 := strconv.Atoi(s[:i])
	m, err2 := strconv.Atoi(s[i+1:])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// pkFormatHM formats minutes after midnight as "HH:MM".
func pkFormatHM(mins int) string {
	mins = ((mins % 1440) + 1440) % 1440
	return pkPad2(mins/60) + ":" + pkPad2(mins%60)
}

// pkTimeLabel formats minutes after midnight like Slack: "9:30 AM".
func pkTimeLabel(mins int) string {
	mins = ((mins % 1440) + 1440) % 1440
	h, m := mins/60, mins%60
	suf := " AM"
	if h >= 12 {
		suf = " PM"
	}
	h12 := h % 12
	if h12 == 0 {
		h12 = 12
	}
	return strconv.Itoa(h12) + ":" + pkPad2(m) + suf
}

func pkAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// pkParseTyped parses a typed time: "930", "9:30pm", "21:30", "9 am",
// "12a", "0930".
func pkParseTyped(s string) (int, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, " ", "")
	mer := 0
	switch {
	case strings.HasSuffix(s, "am"):
		mer, s = 1, strings.TrimSuffix(s, "am")
	case strings.HasSuffix(s, "pm"):
		mer, s = 2, strings.TrimSuffix(s, "pm")
	case strings.HasSuffix(s, "a"):
		mer, s = 1, strings.TrimSuffix(s, "a")
	case strings.HasSuffix(s, "p"):
		mer, s = 2, strings.TrimSuffix(s, "p")
	}
	var hs, ms string
	if i := strings.IndexByte(s, ':'); i >= 0 {
		hs, ms = s[:i], s[i+1:]
		if len(ms) != 2 {
			return 0, false
		}
	} else {
		switch len(s) {
		case 1, 2:
			hs, ms = s, "0"
		case 3:
			hs, ms = s[:1], s[1:]
		case 4:
			hs, ms = s[:2], s[2:]
		default:
			return 0, false
		}
	}
	if !pkAllDigits(hs) || !pkAllDigits(ms) || len(hs) > 2 {
		return 0, false
	}
	h, _ := strconv.Atoi(hs)
	m, _ := strconv.Atoi(ms)
	if m > 59 {
		return 0, false
	}
	if mer != 0 {
		if h < 1 || h > 12 {
			return 0, false
		}
		if h == 12 {
			h = 0
		}
		if mer == 2 {
			h += 12
		}
	} else if h > 23 {
		return 0, false
	}
	return h*60 + m, true
}

// pkIncrement maps optionsHourIncrement to minutes (default half).
func pkIncrement(s string) int {
	switch s {
	case "five_minute":
		return 5
	case "ten_minute":
		return 10
	case "quarter":
		return 15
	case "full":
		return 60
	}
	return 30
}

// pkSlots lists the times from lo to hi (inclusive) on step boundaries.
func pkSlots(step, lo, hi int) []int {
	if step < 1 {
		step = 30
	}
	if lo < 0 {
		lo = 0
	}
	if hi <= 0 || hi > 1439 {
		hi = 1439
	}
	var out []int
	for t := (lo + step - 1) / step * step; t <= hi; t += step {
		out = append(out, t)
	}
	return out
}

type timeState struct {
	props   slackc.Props
	ed      widget.Editor
	chev    widget.Clickable
	org     origin
	pop     *popups.Popover
	list    layout.List
	slots   []int
	clicks  []widget.Clickable
	fieldW  int
	cur     int // current value (minutes), -1 none
	local   int // uncontrolled value+1
	focused bool
	shown   string // label the editor was last synced to
	init    bool
}

func (s *timeState) open() bool { return s.pop != nil && !s.pop.Dismissed() }

func (s *timeState) commit(mins int) {
	s.local = mins + 1
	s.cur = mins
	s.props.Invoke("onChange", pkFormatHM(mins))
	l := pkTimeLabel(mins)
	s.ed.SetText(l)
	s.shown = l
	invalidate()
}

// commitTyped applies the editor's text (reverting when it doesn't parse
// or is outside min/max).
func (s *timeState) commitTyped() {
	txt := strings.TrimSpace(s.ed.Text())
	if txt == s.shown {
		return
	}
	m, ok := pkParseTyped(txt)
	if ok {
		if lo, ok2 := pkParseHM(s.props.StrOr("min", "")); ok2 && m < lo {
			ok = false
		}
		if hi, ok2 := pkParseHM(s.props.StrOr("max", "")); ok2 && m > hi {
			ok = false
		}
	}
	if ok {
		s.commit(m)
		return
	}
	s.shown = ""
}

func (s *timeState) listLayout(gtx layout.Context) layout.Dimensions {
	th := thm()
	cl := colorsOf(th)
	for i := range s.clicks {
		if s.clicks[i].Clicked(gtx) {
			s.commit(s.slots[i])
			if s.pop != nil {
				s.pop.Dismiss()
			}
		}
	}
	rowH, padY := dp(gtx, 28), dp(gtx, 12)
	w := max(dp(gtx, 138), s.fieldW)
	listH := min(len(s.slots)*rowH, dp(gtx, 264)-2*padY)
	at(gtx, image.Pt(0, padY), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(w, max(1, listH)))
		return s.list.Layout(gtx, len(s.slots), func(gtx layout.Context, i int) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(w, rowH))
			return pkClick(gtx, &s.clicks[i], func(gtx layout.Context) layout.Dimensions {
				sz := image.Pt(w, rowH)
				fg := cl.Fg
				if s.slots[i] == s.cur || s.clicks[i].Hovered() {
					theme.Fill(gtx, image.Rectangle{Max: sz}, cl.Highlight)
					fg = cl.HighText
				}
				at(gtx, image.Pt(dp(gtx, 24), 0), func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints = layout.Constraints{Max: image.Pt(max(1, w-dp(gtx, 32)), rowH)}
					return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions {
						return label(gtx, th, 15, theme.Regular, fg, pkTimeLabel(s.slots[i]))
					})
				})
				return layout.Dimensions{Size: sz}
			})
		})
	})
	return layout.Dimensions{Size: image.Pt(w, listH+2*padY)}
}

func (s *timeState) openList(gtx layout.Context, anchor image.Rectangle) {
	lo, _ := pkParseHM(s.props.StrOr("min", ""))
	hi, ok := pkParseHM(s.props.StrOr("max", ""))
	if !ok {
		hi = 1439
	}
	s.slots = pkSlots(pkIncrement(s.props.StrOr("optionsHourIncrement", "")), lo, hi)
	s.clicks = make([]widget.Clickable, len(s.slots))
	s.list = layout.List{Axis: layout.Vertical}
	for i, m := range s.slots {
		if m >= s.cur {
			s.list.Position.First = max(0, i-2)
			break
		}
	}
	s.pop = popups.NewPopover(ph, s.listLayout, popups.PopoverOpts{Anchor: anchor, Side: popups.Below, Radius: 4})
	showOverlay(s.pop)
}

func timePicker(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	st := stateOf[timeState](h)
	st.props = h.Props
	p := h.Props
	th := thm()
	cl := colorsOf(th)
	if !st.init {
		st.init = true
		st.ed.SingleLine, st.ed.Submit = true, true
		st.cur = -1
	}
	off := p.Bool("isDisabled")
	st.ed.ReadOnly = off
	// current value: controlled value, else what the user picked
	st.cur = -1
	if m, ok := pkParseHM(p.StrOr("value", "")); ok {
		st.cur = m
	} else if st.local > 0 {
		st.cur = st.local - 1
	}
	size := 0
	if sizeOf(p, 1) >= 1 {
		size = 1
	}
	m := pkMetricsFor(size)
	tz := ""
	if p.Bool("showTimeZone") {
		if c := envOf().Core; c != nil {
			tz = c.Self().TZLabel
		}
	}
	defW := float32(140)
	if tz != "" {
		defW = 240
	}
	w := pkWidth(gtx, p["width"], defW)
	hh := dp(gtx, m.h)
	st.fieldW = w

	// editor events: Enter commits, blur commits
	for {
		e, ok := st.ed.Update(gtx)
		if !ok {
			break
		}
		if _, ok := e.(widget.SubmitEvent); ok {
			st.commitTyped()
			gtx.Execute(key.FocusCmd{})
		}
	}
	focused := gtx.Focused(&st.ed)
	if st.focused && !focused {
		st.commitTyped()
	}
	st.focused = focused
	lbl := ""
	if st.cur >= 0 {
		lbl = pkTimeLabel(st.cur)
	}
	if !focused && st.shown != lbl {
		st.ed.SetText(lbl)
		st.shown = lbl
	}
	if !off && st.chev.Clicked(gtx) && !st.open() {
		st.openList(gtx, pkRect(&st.org, image.Pt(w, hh)))
	}

	d := pkField(gtx, th, pkFieldOpts{W: w, Size: size, Icon: "clock", Chevron: true, Open: focused || st.open(), Hover: st.chev.Hovered(), Off: off},
		func(gtx layout.Context, fg color.NRGBA, fs float32) layout.Dimensions {
			aw := gtx.Constraints.Max.X
			tw := 0
			if tz != "" {
				tw = textW(gtx, th, fs, theme.Regular, tz) + dp(gtx, 6)
				if tw > aw/2 {
					tw = aw / 2
				}
			}
			ew := max(1, aw-tw)
			call, ed := record(gtx, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Min: image.Pt(ew, 0), Max: image.Pt(ew, hh)}
				return th.Editor(gtx, &st.ed, unit.Sp(fs), fg, theme.WithAlpha(cl.Focus, 0x55))
			})
			call.Add(gtx.Ops)
			if tz != "" {
				at(gtx, image.Pt(ew+dp(gtx, 6), 0), func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Max.X = max(1, tw-dp(gtx, 6))
					return vcenter(gtx, ed.Size.Y, func(gtx layout.Context) layout.Dimensions {
						return label(gtx, th, fs, theme.Regular, cl.Muted, tz)
					})
				})
			}
			return layout.Dimensions{Size: image.Pt(aw, ed.Size.Y)}
		})
	if !off {
		// the clock and the chevron open the list; the text area types
		cw := dp(gtx, m.csz) + dp(gtx, m.pad) + dp(gtx, 4)
		for _, x := range [2]int{0, w - cw} {
			bw := cw
			if x == 0 {
				bw = dp(gtx, m.pad) + dp(gtx, m.isz) + dp(gtx, m.gap)/2
			}
			at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Exact(image.Pt(bw, hh))
				return pkClick(gtx, &st.chev, func(gtx layout.Context) layout.Dimensions {
					return layout.Dimensions{Size: image.Pt(bw, hh)}
				})
			})
		}
	}
	st.org.track(gtx, image.Rectangle{Max: d.Size})
	return d
}

// ---- DatePickerTrigger ----

type dateTrigState struct {
	props   slackc.Props
	org     origin
	tag     int
	pressed bool
	cal     pkCal
	size    image.Point
}

func datePickerTrigger(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	st := stateOf[dateTrigState](h)
	st.props = h.Props
	p := h.Props
	clicked := false
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &st.tag, Kinds: pointer.Press | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		pe, ok := e.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Press:
			st.pressed = pe.Buttons == 0 || pe.Buttons.Contain(pointer.ButtonPrimary)
		case pointer.Release:
			in := pe.Position.X >= 0 && pe.Position.Y >= 0 && int(pe.Position.X) < st.size.X && int(pe.Position.Y) < st.size.Y
			if st.pressed && in {
				clicked = true
			}
			st.pressed = false
		case pointer.Cancel:
			st.pressed = false
		}
	}
	if clicked && !st.cal.isOpen() {
		f := p.StrOr("dateFormat", pkDefaultDateFormat)
		sel, ok := pkParseDate(p.StrOr("selectedDate", ""), f)
		if !ok {
			sel = time.Time{}
		}
		st.cal.rng, st.cal.months, st.cal.start, st.cal.end = false, 1, sel, time.Time{}
		if sel.IsZero() {
			st.cal.month = pkMonthStart(time.Now())
		} else {
			st.cal.month = pkMonthStart(sel)
		}
		st.cal.rules = pkMakeRules(p, f)
		st.cal.onPick = func(day time.Time) {
			st.props.Invoke("onDateChange", pkFormatDate(day, st.props.StrOr("dateFormat", pkDefaultDateFormat)))
			invalidate()
		}
		st.cal.open(pkRect(&st.org, st.size), nil)
	}
	d := h.Children(gtx)
	st.size = d.Size
	r := image.Rectangle{Max: d.Size}
	func() {
		defer pointer.PassOp{}.Push(gtx.Ops).Pop()
		defer clip.Rect(r).Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &st.tag)
	}()
	st.org.track(gtx, r)
	return d
}

// ---- DateRangePicker ----

type rangeState struct {
	props  slackc.Props
	init   bool
	start  time.Time
	end    time.Time
	fields [2]widget.Clickable
	clear  widget.Clickable
	org    origin
	cal    pkCal
	closed bool // the calendar was opened and closed (endDateRequired)
}

func (s *rangeState) format() string { return s.props.StrOr("dateFormat", pkDefaultDateFormat) }

func (s *rangeState) display(t time.Time) string {
	f := s.format()
	if df, ok := s.props.Str("displayFormat"); ok && df != "" {
		f = df
	}
	return pkFormatDate(t, f)
}

func (s *rangeState) report(start, end time.Time) {
	f := s.format()
	changed := func(a, b time.Time) bool {
		if a.IsZero() || b.IsZero() {
			return a.IsZero() != b.IsZero()
		}
		return !pkSameDay(a, b)
	}
	if changed(start, s.start) {
		v := ""
		if !start.IsZero() {
			v = pkFormatDate(start, f)
		}
		s.props.Invoke("onStartDateChange", slackc.Props{"selectedStartDate": v})
	}
	if changed(end, s.end) {
		v := ""
		if !end.IsZero() {
			v = pkFormatDate(end, f)
		}
		s.props.Invoke("onEndDateChange", slackc.Props{"selectedEndDate": v})
	}
	s.start, s.end = start, end
	invalidate()
}

func dateRangePicker(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	st := stateOf[rangeState](h)
	st.props = h.Props
	p := h.Props
	th := thm()
	cl := colorsOf(th)
	if !st.init {
		st.init = true
		f := st.format()
		if t, ok := pkParseDate(p.StrOr("selectedStartDate", ""), f); ok {
			st.start = t
		}
		if t, ok := pkParseDate(p.StrOr("selectedEndDate", ""), f); ok {
			st.end = t
		}
	}
	size := sizeOf(p, 1)
	m := pkMetricsFor(size)
	hh := dp(gtx, m.h)
	sepW := dp(gtx, 24)
	showClear := p.Bool("showClearSelection") && (!st.start.IsZero() || !st.end.IsZero())
	clearW := 0
	if showClear {
		clearW = hh
	}
	total := pkWidth(gtx, p["width"], 2*160+24)
	fw := max(dp(gtx, 60), (total-sepW-clearW)/2)
	total = 2*fw + sepW + clearW

	if showClear && st.clear.Clicked(gtx) {
		st.cal.close()
		st.cal.start, st.cal.end, st.cal.focus = time.Time{}, time.Time{}, 0
		st.report(time.Time{}, time.Time{})
	}
	for i := range st.fields {
		if !st.fields[i].Clicked(gtx) {
			continue
		}
		st.cal.focus = i
		if st.cal.isOpen() {
			continue
		}
		st.cal.rng = true
		st.cal.months = 2
		if p.Bool("singleMonthMode") {
			st.cal.months = 1
		}
		st.cal.start, st.cal.end = st.start, st.end
		base := st.start
		if i == 1 && !st.end.IsZero() {
			base = st.end
		}
		if base.IsZero() {
			base = time.Now()
		}
		st.cal.month = pkMonthStart(base)
		if p.Bool("showPreviousMonth") && st.cal.months == 2 {
			st.cal.month = st.cal.month.AddDate(0, -1, 0)
		}
		st.cal.maxRange = p.IntOr("maxRange", 0)
		st.cal.rules = pkMakeRules(p, st.format())
		st.cal.onRange = func(s, e time.Time, done bool) {
			st.report(s, e)
			if done && st.props.Bool("closeAfterSelection") {
				st.cal.close()
			}
		}
		st.closed = false
		st.cal.open(pkRect(&st.org, image.Pt(total, hh)), func() {
			st.closed = true
			st.props.Invoke("onCalendarClose")
			invalidate()
		})
	}
	open := st.cal.isOpen()
	ph0 := p.StrOr("placeholderText", "")
	holders := [2]string{p.StrOr("startInputPlaceholder", ph0), p.StrOr("endInputPlaceholder", ph0)}
	if holders[0] == "" {
		holders[0] = "Start date"
	}
	if holders[1] == "" {
		holders[1] = "End date"
	}
	vals := [2]time.Time{st.start, st.end}
	for i := range st.fields {
		x := i * (fw + sepW)
		btn := &st.fields[i]
		txt, empty := holders[i], true
		if !vals[i].IsZero() {
			txt, empty = st.display(vals[i]), false
		}
		invalid := i == 1 && p.Bool("endDateRequired") && st.closed && !st.start.IsZero() && st.end.IsZero()
		o := pkFieldOpts{W: fw, Size: size, Icon: "calendar", Open: open && st.cal.focus == i, Hover: btn.Hovered(), Invalid: invalid}
		at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			return pkClick(gtx, btn, func(gtx layout.Context) layout.Dimensions {
				return pkField(gtx, th, o, pkTextContent(th, txt, empty))
			})
		})
	}
	// en dash between the fields
	dw := textW(gtx, th, m.fs, theme.Regular, "–")
	at(gtx, image.Pt(fw+(sepW-dw)/2, 0), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
			return label(gtx, th, m.fs, theme.Regular, cl.Fg2, "–")
		})
	})
	if showClear {
		at(gtx, image.Pt(2*fw+sepW, 0), func(gtx layout.Context) layout.Dimensions {
			return pkClick(gtx, &st.clear, func(gtx layout.Context) layout.Dimensions {
				sz := image.Pt(clearW, hh)
				if st.clear.Hovered() {
					theme.RRect(gtx, image.Rectangle{Max: sz}.Inset(dp(gtx, 4)), dp(gtx, 4), cl.BgHover)
				}
				is := dp(gtx, 16)
				at(gtx, image.Pt((sz.X-is)/2, (sz.Y-is)/2), func(gtx layout.Context) layout.Dimensions {
					return pkIcon(gtx, "close", 16, cl.Fg2)
				})
				return layout.Dimensions{Size: sz}
			})
		})
	}
	sz := image.Pt(total, hh)
	st.org.track(gtx, image.Rectangle{Max: sz})
	return layout.Dimensions{Size: sz}
}
