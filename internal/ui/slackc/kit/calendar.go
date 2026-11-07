package kit

import (
	"image"
	"image/color"
	"strconv"
	"strings"
	"time"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Slack's calendar (the date pickers' popover), measured like popups'
// datetime.go: card padding 24, header 48px with ‹ "November 2026" 15/700
// ›, weekday row Su…Sa 13/700 30px, 7×6 grid of 43×41 cells with 1px
// lines, numbers 15px. Disabled days: #f8f8f8 cells, muted numbers.
// Selected: #1264a3 cell with a 34px white ring. Range days in between get
// a light blue tint. Two months sit side by side 24px apart.

// ---- pure date math (tested) ----

// pkDay is local midnight of t's day.
func pkDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func pkMonthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}

// pkMonthGrid lays month out on a Sunday-first 7×6 grid: day numbers, 0
// for cells outside the month.
func pkMonthGrid(month time.Time) [42]int {
	var g [42]int
	first := pkMonthStart(month)
	n := time.Date(first.Year(), first.Month()+1, 0, 0, 0, 0, 0, first.Location()).Day()
	off := int(first.Weekday())
	for i := 0; i < n && off+i < len(g); i++ {
		g[off+i] = i + 1
	}
	return g
}

func pkSameDay(a, b time.Time) bool {
	if a.IsZero() || b.IsZero() {
		return false
	}
	y1, m1, d1 := a.Date()
	y2, m2, d2 := b.Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}

func pkOrdinal(d int) string {
	s := strconv.Itoa(d)
	switch {
	case d%100 >= 11 && d%100 <= 13:
		return s + "th"
	case d%10 == 1:
		return s + "st"
	case d%10 == 2:
		return s + "nd"
	case d%10 == 3:
		return s + "rd"
	}
	return s + "th"
}

// ---- moment-style date formats ----

// pkDefaultDateFormat is Slack's date picker default.
const pkDefaultDateFormat = "YYYY-MM-DD"

var pkTokens = []string{"YYYY", "MMMM", "dddd", "MMM", "ddd", "YY", "MM", "Do", "DD", "M", "D"}

type pkTok struct {
	s   string
	lit bool
}

// pkTokenize splits a moment format into tokens and literals ("[...]"
// escapes literal text).
func pkTokenize(f string) []pkTok {
	var out []pkTok
	for i := 0; i < len(f); {
		if f[i] == '[' {
			if j := strings.IndexByte(f[i+1:], ']'); j >= 0 {
				if j > 0 {
					out = append(out, pkTok{s: f[i+1 : i+1+j], lit: true})
				}
				i += j + 2
				continue
			}
		}
		matched := false
		for _, t := range pkTokens {
			if strings.HasPrefix(f[i:], t) {
				out = append(out, pkTok{s: t})
				i += len(t)
				matched = true
				break
			}
		}
		if !matched {
			out = append(out, pkTok{s: f[i : i+1], lit: true})
			i++
		}
	}
	return out
}

func pkPad2(n int) string {
	if n < 10 && n >= 0 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// pkFormatDate formats t with a moment-style format (YYYY, YY, MMMM, MMM,
// MM, M, Do, DD, D, dddd, ddd; "[text]" literal).
func pkFormatDate(t time.Time, f string) string {
	if f == "" {
		f = pkDefaultDateFormat
	}
	var b strings.Builder
	for _, tk := range pkTokenize(f) {
		if tk.lit {
			b.WriteString(tk.s)
			continue
		}
		switch tk.s {
		case "YYYY":
			y := strconv.Itoa(t.Year())
			for len(y) < 4 {
				y = "0" + y
			}
			b.WriteString(y)
		case "YY":
			b.WriteString(pkPad2(t.Year() % 100))
		case "MMMM":
			b.WriteString(t.Month().String())
		case "MMM":
			b.WriteString(t.Month().String()[:3])
		case "MM":
			b.WriteString(pkPad2(int(t.Month())))
		case "M":
			b.WriteString(strconv.Itoa(int(t.Month())))
		case "Do":
			b.WriteString(pkOrdinal(t.Day()))
		case "DD":
			b.WriteString(pkPad2(t.Day()))
		case "D":
			b.WriteString(strconv.Itoa(t.Day()))
		case "dddd":
			b.WriteString(t.Weekday().String())
		case "ddd":
			b.WriteString(t.Weekday().String()[:3])
		}
	}
	return b.String()
}

// pkParseDate parses s with a moment-style format into local midnight.
func pkParseDate(s, f string) (time.Time, bool) {
	if f == "" {
		f = pkDefaultDateFormat
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	y, m, d := time.Now().Year(), 1, 1
	i := 0
	digits := func(min, max int) (int, bool) {
		j := i
		for j < len(s) && j-i < max && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j-i < min {
			return 0, false
		}
		n, err := strconv.Atoi(s[i:j])
		if err != nil {
			return 0, false
		}
		i = j
		return n, true
	}
	name := func(full bool, names []string) (int, bool) {
		rest := strings.ToLower(s[i:])
		for k, n := range names {
			n = strings.ToLower(n)
			if !full {
				n = n[:3]
			}
			if strings.HasPrefix(rest, n) {
				i += len(n)
				return k, true
			}
		}
		// lenient: MMMM also takes "Nov", MMM also takes "November"
		for k, n := range names {
			n = strings.ToLower(n)
			if full {
				n = n[:3]
			}
			if strings.HasPrefix(rest, n) {
				i += len(n)
				return k, true
			}
		}
		return 0, false
	}
	months := make([]string, 12)
	for k := range months {
		months[k] = time.Month(k + 1).String()
	}
	days := make([]string, 7)
	for k := range days {
		days[k] = time.Weekday(k).String()
	}
	for _, tk := range pkTokenize(f) {
		if tk.lit {
			for _, c := range []byte(tk.s) {
				switch {
				case c == ' ':
					for i < len(s) && s[i] == ' ' {
						i++
					}
				case i < len(s) && s[i] == c:
					i++
				default:
					return time.Time{}, false
				}
			}
			continue
		}
		var ok bool
		switch tk.s {
		case "YYYY":
			y, ok = digits(4, 4)
		case "YY":
			y, ok = digits(2, 2)
			y += 2000
		case "MMMM", "MMM":
			var k int
			k, ok = name(tk.s == "MMMM", months)
			m = k + 1
		case "MM", "M":
			m, ok = digits(1, 2)
		case "DD", "D":
			d, ok = digits(1, 2)
		case "Do":
			d, ok = digits(1, 2)
			if ok {
				low := strings.ToLower(s[i:])
				for _, suf := range []string{"st", "nd", "rd", "th"} {
					if strings.HasPrefix(low, suf) {
						i += 2
						break
					}
				}
			}
		case "dddd", "ddd":
			_, ok = name(tk.s == "dddd", days)
		}
		if !ok {
			return time.Time{}, false
		}
	}
	if strings.TrimSpace(s[i:]) != "" {
		return time.Time{}, false
	}
	if m < 1 || m > 12 || d < 1 {
		return time.Time{}, false
	}
	t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.Local)
	if t.Day() != d || int(t.Month()) != m {
		return time.Time{}, false
	}
	return t, true
}

// ---- disabled days ----

// pkRules are a picker's disabledDates / disableDatesBefore / After.
type pkRules struct {
	off    map[string]bool // "2006-01-02"
	before time.Time       // days before are disabled
	after  time.Time       // days after are disabled
}

func pkMakeRules(p slackc.Props, format string) pkRules {
	var r pkRules
	for _, v := range p.List("disabledDates") {
		if s, ok := v.(string); ok {
			if t, ok := pkParseDate(s, format); ok {
				if r.off == nil {
					r.off = map[string]bool{}
				}
				r.off[t.Format("2006-01-02")] = true
			}
		}
	}
	if s, ok := p.Str("disableDatesBefore"); ok {
		if t, ok := pkParseDate(s, format); ok {
			r.before = t
		}
	}
	if s, ok := p.Str("disableDatesAfter"); ok {
		if t, ok := pkParseDate(s, format); ok {
			r.after = t
		}
	}
	return r
}

func (r pkRules) disabled(day time.Time) bool {
	day = pkDay(day)
	if !r.before.IsZero() && day.Before(pkDay(r.before)) {
		return true
	}
	if !r.after.IsZero() && day.After(pkDay(r.after)) {
		return true
	}
	return r.off[day.Format("2006-01-02")]
}

// pkRangeStep applies a click on day to a range being picked. focus is
// the end being picked (0 start, 1 end). It returns the new range, the
// next focus and whether the range is complete.
func pkRangeStep(start, end, day time.Time, focus int) (time.Time, time.Time, int, bool) {
	day = pkDay(day)
	if focus == 0 {
		if !end.IsZero() && end.Before(day) {
			end = time.Time{}
		}
		return day, end, 1, false
	}
	if start.IsZero() || day.Before(start) {
		return day, time.Time{}, 1, false
	}
	return start, day, 0, true
}

// ---- the calendar popover ----

type pkCal struct {
	month    time.Time // first displayed month
	months   int       // 1 or 2 side by side
	rng      bool      // range mode
	start    time.Time // selection (single mode: start)
	end      time.Time
	focus    int // range mode: 0 picking start, 1 end
	maxRange int // days, 0 = any
	rules    pkRules

	// onPick reports a single-mode pick; onRange a range-mode click
	// (done: both ends set).
	onPick  func(day time.Time)
	onRange func(start, end time.Time, done bool)

	nav   [2]widget.Clickable
	cells [2][42]widget.Clickable
	pop   *popups.Popover
}

func (c *pkCal) isOpen() bool { return c.pop != nil && !c.pop.Dismissed() }

func (c *pkCal) close() {
	if c.isOpen() {
		c.pop.Dismiss()
	}
}

// open shows the calendar under anchor (window coordinates).
func (c *pkCal) open(anchor image.Rectangle, onClose func()) {
	c.close()
	if c.months < 1 {
		c.months = 1
	}
	c.pop = popups.NewPopover(ph, c.layout, popups.PopoverOpts{Anchor: anchor, Side: popups.Below, OnClose: onClose})
	showOverlay(c.pop)
}

func (c *pkCal) disabled(day time.Time) bool {
	if c.rules.disabled(day) {
		return true
	}
	if c.rng && c.focus == 1 && c.maxRange > 0 && !c.start.IsZero() {
		if day.After(c.start.AddDate(0, 0, c.maxRange)) {
			return true
		}
	}
	return false
}

func (c *pkCal) click(day time.Time) {
	if c.disabled(day) {
		return
	}
	if !c.rng {
		c.start = day
		c.close()
		if c.onPick != nil {
			c.onPick(day)
		}
		return
	}
	s, e, f, done := pkRangeStep(c.start, c.end, day, c.focus)
	c.start, c.end, c.focus = s, e, f
	if c.onRange != nil {
		c.onRange(s, e, done)
	}
}

func pkGridLine(th *theme.Theme) color.NRGBA {
	if th != nil && th.Dark {
		return rgb(0x35373b)
	}
	return rgb(0xdddddd)
}

func pkIcon(gtx layout.Context, name string, size float32, c color.NRGBA) layout.Dimensions {
	return icons.Get(name)(gtx, unit.Dp(size), c)
}

// pkClick lays w out inside c with a hand cursor.
func pkClick(gtx layout.Context, c *widget.Clickable, w layout.Widget) layout.Dimensions {
	return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		d := w(gtx)
		cursor(gtx, d.Size, pointer.CursorPointer)
		return d
	})
}

// layout draws the calendar card content (the Popover paints the card).
func (c *pkCal) layout(gtx layout.Context) layout.Dimensions {
	th := thm()
	cl := colorsOf(th)
	if c.month.IsZero() {
		c.month = pkMonthStart(time.Now())
	}
	if c.nav[0].Clicked(gtx) {
		c.month = pkMonthStart(c.month).AddDate(0, -1, 0)
	}
	if c.nav[1].Clicked(gtx) {
		c.month = pkMonthStart(c.month).AddDate(0, 1, 0)
	}
	n := c.months
	if n < 1 {
		n = 1
	}
	if n > 2 {
		n = 2
	}
	pad, cw, ch := dp(gtx, 24), dp(gtx, 43), dp(gtx, 41)
	headH, wkH, gapM := dp(gtx, 48), dp(gtx, 30), dp(gtx, 24)
	gridW := 7 * cw
	inner := n*gridW + (n-1)*gapM
	w := max(dp(gtx, 350), 2*pad+inner)
	h := 2*pad + headH + wkH + 6*ch
	x0 := (w - inner) / 2
	today := pkDay(time.Now())
	lw := max(1, dp(gtx, 1))
	line := pkGridLine(th)
	tint := theme.Mix(cl.Bg, cl.Highlight, .15)

	for mi := 0; mi < n; mi++ {
		month := pkMonthStart(c.month).AddDate(0, mi, 0)
		gx := x0 + mi*(gridW+gapM)
		title := month.Format("January 2006")
		tw := textW(gtx, th, 15, theme.Bold, title)
		at(gtx, image.Pt(gx+(gridW-tw)/2, pad), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, headH, func(gtx layout.Context) layout.Dimensions {
				return label(gtx, th, 15, theme.Bold, cl.Fg, title)
			})
		})
		wy := pad + headH
		for i, wd := range [...]string{"Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"} {
			ww := textW(gtx, th, 13, theme.Bold, wd)
			at(gtx, image.Pt(gx+i*cw+(cw-ww)/2, wy), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, wkH, func(gtx layout.Context) layout.Dimensions {
					return label(gtx, th, 13, theme.Bold, cl.Fg2, wd)
				})
			})
		}
		gy := wy + wkH
		grid := pkMonthGrid(month)
		for i, dn := range grid {
			if dn == 0 {
				continue
			}
			day := time.Date(month.Year(), month.Month(), dn, 0, 0, 0, 0, month.Location())
			off := c.disabled(day)
			btn := &c.cells[mi][i]
			if !off && btn.Clicked(gtx) {
				c.click(day)
			}
			sel := pkSameDay(day, c.start) || (c.rng && pkSameDay(day, c.end))
			between := c.rng && !c.start.IsZero() && !c.end.IsZero() && day.After(c.start) && day.Before(c.end)
			isToday := pkSameDay(day, today)
			r := image.Rect(gx+(i%7)*cw, gy+(i/7)*ch, gx+(i%7+1)*cw, gy+(i/7+1)*ch)
			num := strconv.Itoa(dn)
			cell := func(gtx layout.Context) layout.Dimensions {
				s := r.Size()
				box := image.Rectangle{Max: s}
				switch {
				case off:
					theme.Fill(gtx, box, cl.BgAlt)
				case between:
					theme.Fill(gtx, box, tint)
				case !sel && btn.Hovered():
					theme.Fill(gtx, box, cl.BgAlt)
				}
				fg := cl.Fg
				wt := theme.Regular
				if off {
					fg = cl.Muted
				}
				if isToday {
					wt = theme.Bold
					if !off {
						fg = cl.Link
					}
				}
				if sel {
					theme.RRect(gtx, box, dp(gtx, 4), cl.Highlight)
					d := dp(gtx, 34)
					ctr := image.Pt(s.X/2, s.Y/2)
					theme.Circle(gtx, ctr, d, cl.HighText)
					theme.Circle(gtx, ctr, d-2*lw, cl.Highlight)
					fg = cl.HighText
				}
				nw := textW(gtx, th, 15, wt, num)
				at(gtx, image.Pt((s.X-nw)/2, 0), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, s.Y, func(gtx layout.Context) layout.Dimensions {
						return label(gtx, th, 15, wt, fg, num)
					})
				})
				return layout.Dimensions{Size: s}
			}
			at(gtx, r.Min, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Exact(r.Size())
				if off {
					return cell(gtx)
				}
				return pkClick(gtx, btn, cell)
			})
		}
		for i, dn := range grid {
			if dn == 0 {
				continue
			}
			r := image.Rect(gx+(i%7)*cw, gy+(i/7)*ch, gx+(i%7+1)*cw, gy+(i/7+1)*ch)
			theme.Fill(gtx, image.Rect(r.Min.X, r.Min.Y, r.Max.X+lw, r.Min.Y+lw), line)
			theme.Fill(gtx, image.Rect(r.Min.X, r.Max.Y, r.Max.X+lw, r.Max.Y+lw), line)
			theme.Fill(gtx, image.Rect(r.Min.X, r.Min.Y, r.Min.X+lw, r.Max.Y+lw), line)
			theme.Fill(gtx, image.Rect(r.Max.X, r.Min.Y, r.Max.X+lw, r.Max.Y+lw), line)
		}
	}

	// ‹ › month navigation
	bs := dp(gtx, 32)
	by := pad + (headH-bs)/2
	for i, bx := range [2]int{x0, x0 + inner - bs} {
		name := "chevron-left"
		if i == 1 {
			name = "chevron-right"
		}
		btn := &c.nav[i]
		at(gtx, image.Pt(bx, by), func(gtx layout.Context) layout.Dimensions {
			return pkClick(gtx, btn, func(gtx layout.Context) layout.Dimensions {
				if btn.Hovered() {
					theme.RRect(gtx, image.Rect(0, 0, bs, bs), dp(gtx, 4), cl.BgHover)
				}
				is := dp(gtx, 20)
				o := (bs - is) / 2
				at(gtx, image.Pt(o, o), func(gtx layout.Context) layout.Dimensions {
					return pkIcon(gtx, name, 20, cl.Fg2)
				})
				return layout.Dimensions{Size: image.Pt(bs, bs)}
			})
		})
	}
	return layout.Dimensions{Size: image.Pt(w, h)}
}
