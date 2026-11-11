package popups

// The search box's date pickers, which omni_date_quick_filters turns on
// (gantry-v2-async-client-boot-shared, build 4d0c78085232):
//
//   - SearchDatePickerPopover (Fc3Y): before:, after: or on: being typed
//     at the caret (h4Ql getDateModifierAtCaret, ltuT
//     matchIncompleteDateFilter: the value a partial date, digits and
//     dashes) opens a calendar under the box, 17px left of the
//     modifier's right edge and kept 8px inside the input row (48Zn). It
//     shows the typed month (getCalendarMonthForPartialDate) and takes no
//     focus: ↓ or ↑ in the box moves into the grid (vu7Q), where the
//     arrows move the day and Enter picks it. A pick replaces the
//     modifier and its value with a completed token ("before:2026-11-05",
//     buildCompletedDateFilterOp) and a space (opsWithCompletedDateToken).
//     Escape or a click elsewhere closes it until the match changes
//     (useOpenUntilDismissed: keyed by where the modifier starts and how
//     it was typed). While it is open the suggestion list, its footer and
//     the quick filter row hide (OmniswitcherContent's eu).
//   - SearchDateRangePickerPopover (agO0): the quick filter menu's Date >
//     Range types "after: before:" and enters range mode: a one-month
//     DateRangePickerCalendar (no days after today) anchored at "after:".
//     The first pick completes after:, the second before:, and range mode
//     ends; closing it after the first pick drops the bare before:.
//
// Both draw popups' calendar (datetime.go dtCalendar) inline on Slack's
// search surface (.datePickerSurface__CEVHz: base-pry, a 1px ring and a
// 0 4px 12px shadow, clipped at the top, radius 0 0 8px 8px, padding 8).

import (
	"image"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
)

// srDateFormat is the search date format (d4lr $R: "YYYY-MM-DD").
const srDateFormat = "2006-01-02"

// srPartialDateRe is isPartialDateValue.
var srPartialDateRe = regexp.MustCompile(`^\d{0,4}(-\d{0,2}(-\d?)?)?$`)

// srDateTypes is fsda's Xy, the modifiers the date picker takes.
var srDateTypes = [...]string{"before", "after", "on"}

// srTrailingDate is matchIncompleteDateFilter: a date modifier at the end
// of s with a partial date after it.
func srTrailingDate(s string) (typ, prefix, value string, ok bool) {
	rs := []rune(s)
	for _, t := range srDateTypes {
		idx, _, found := srTryFilter(rs, t)
		if !found {
			continue
		}
		match := string(rs[idx:])
		colon := strings.IndexByte(match, ':')
		if colon < 0 {
			continue
		}
		mod, val := match[:colon+1], match[colon+1:]
		if srPartialDateRe.MatchString(val) && strings.HasSuffix(s, mod+val) {
			return t, mod, val, true
		}
	}
	return "", "", "", false
}

// srDateMatch is getDateModifierAtCaret's result (rune offsets).
type srDateMatch struct {
	typ    string // "before", "after", "on"
	prefix string // typedPrefix: the modifier as typed ("-Before:")
	value  string // typedValue: the partial date before the caret
	date   string // dateValue: the partial date through the caret's word
	start  int    // tokenStart
	length int    // tokenLength
	// prefixLen is prefixLength: from the modifier to the end of the box
	prefixLen int
}

// key is useOpenUntilDismissed's key, `${tokenStart}:${typedPrefix}`.
func (m srDateMatch) key() string { return strconv.Itoa(m.start) + ":" + m.prefix }

// srDateAt is getDateModifierAtCaret over the box's pieces with the caret
// at caret runes.
func srDateAt(ps []srPiece, caret int) (srDateMatch, bool) {
	text := []rune(srJoin(ps).text)
	n := len(text)
	if caret < 0 || caret > n {
		caret = n
	}
	head := srCutPieces(ps, caret)
	typ, prefix, value, ok := srTrailingDate(srTrailing(head))
	if !ok {
		return srDateMatch{}, false
	}
	// the word after the caret
	word := 0
	for caret+word < n && !srJSSpace(text[caret+word]) {
		word++
	}
	date := value
	if value != "" {
		_, _, v2, ok2 := srTrailingDate(srTrailing(srCutPieces(ps, caret+word)))
		if !ok2 {
			return srDateMatch{}, false
		}
		date = v2
	}
	pl, vl := utf8.RuneCountInString(prefix), utf8.RuneCountInString(value)
	start := caret - pl - vl
	return srDateMatch{typ: typ, prefix: prefix, value: value, date: date, start: start,
		length: pl + utf8.RuneCountInString(date), prefixLen: n - start}, true
}

// srCutPieces is delta.slice(0, at): the pieces up to at runes.
func srCutPieces(ps []srPiece, at int) []srPiece {
	var out []srPiece
	n := 0
	for _, pc := range ps {
		l := utf8.RuneCountInString(pc.text)
		if n+l <= at {
			out = append(out, pc)
			n += l
			continue
		}
		if n < at {
			rs := []rune(pc.text)
			cut := pc
			cut.text = string(rs[:at-n])
			if cut.tok != nil {
				// a cut token keeps its attributes in a delta; as text it
				// ends the trailing text all the same
				t := *cut.tok
				cut.tok = &t
			}
			out = append(out, cut)
		}
		break
	}
	return out
}

// srCalendarMonth is getCalendarMonthForPartialDate: the month a partial
// date shows ("2026" → January 2026, "2026-03" → March); ok false without
// a four-digit year.
func srCalendarMonth(v string, loc *time.Location) (time.Time, bool) {
	parts := strings.Split(v, "-")
	y, m := parts[0], ""
	if len(parts) > 1 {
		m = parts[1]
	}
	if len(y) != 4 {
		return time.Time{}, false
	}
	yr := 0
	for _, r := range y {
		yr = yr*10 + int(r-'0')
	}
	mo := 0
	for _, r := range m {
		if r < '0' || r > '9' {
			mo = 0
			break
		}
		mo = mo*10 + int(r-'0')
	}
	if mo < 1 || mo > 12 {
		mo = 1
	}
	return time.Date(yr, time.Month(mo), 1, 0, 0, 0, 0, loc), true
}

// srDateToken is buildCompletedDateFilterOp: "before:2026-11-05", "-"
// kept, the modifier's own name (not as typed).
func srDateToken(typ, date string, neg bool) srToken {
	label := typ + ":" + date
	mod := typ
	if neg {
		label, mod = "-"+label, "-"+typ
	}
	return srToken{label: label, req: label, icon: "calendar", mod: mod, entity: date}
}

// srWithDateToken is opsWithCompletedDateToken: the match's text replaced
// by tok, a space after it unless whitespace follows.
func srWithDateToken(ps []srPiece, m srDateMatch, tok srToken) []srPiece {
	b := srJoin(ps)
	rs := []rune(b.text)
	end := m.start + m.length
	if m.start < 0 || end > len(rs) {
		return ps
	}
	var out []srPiece
	out = append(out, srCutPieces(ps, m.start)...)
	out = append(out, srTokPiece(tok))
	if end >= len(rs) || !unicode.IsSpace(rs[end]) {
		out = append(out, srSpace())
	}
	return append(out, srTail(ps, end)...)
}

// srTail is delta.slice(at): the pieces after at runes (a token cut in
// two is plain text).
func srTail(ps []srPiece, at int) []srPiece {
	var out []srPiece
	n := 0
	for _, pc := range ps {
		l := utf8.RuneCountInString(pc.text)
		switch {
		case n >= at:
			out = append(out, pc)
		case n+l > at:
			out = append(out, srText(string([]rune(pc.text)[at-n:])))
		}
		n += l
	}
	return out
}

// srRangeAnchor is getRangeAnchorPrefixLength: how far "mod:" in the
// trailing text is from the box's end; ok false when it isn't there.
func srRangeAnchor(ps []srPiece, mod string) (int, bool) {
	t := srTrailing(ps)
	i := strings.Index(t, mod+":")
	if i < 0 {
		return 0, false
	}
	return utf8.RuneCountInString(t[i:]), true
}

// srWithRangeToken is opsWithCompletedRangeToken: "mod:" in the trailing
// text becomes tok; what came after it stays (a space when nothing did).
// Without "mod:" tok and a space go at the end.
func srWithRangeToken(ps []srPiece, mod string, tok srToken) []srPiece {
	if len(ps) == 0 {
		return []srPiece{srTokPiece(tok), srSpace()}
	}
	t := srTrailing(ps)
	head := append([]srPiece(nil), ps[:len(ps)-1]...)
	i := strings.Index(t, mod+":")
	if i < 0 {
		if t != "" {
			head = append(head, srText(t))
		}
		return append(head, srTokPiece(tok), srSpace())
	}
	before, after := t[:i], t[i+len(mod)+1:]
	if before != "" {
		head = append(head, srText(before))
	}
	head = append(head, srTokPiece(tok))
	if after != "" {
		return append(head, srText(after))
	}
	return append(head, srSpace())
}

// srWithoutBareModifier is opsWithBareModifierRemoved: the trailing text
// from "mod:" on dropped, its trailing spaces too, then a space.
func srWithoutBareModifier(ps []srPiece, mod string) []srPiece {
	t := srTrailing(ps)
	i := strings.Index(t, mod+":")
	if i < 0 {
		return ps
	}
	r := strings.TrimRightFunc(t[:i], unicode.IsSpace)
	out := append([]srPiece(nil), ps[:len(ps)-1]...)
	if r != "" {
		out = append(out, srText(r))
	}
	return append(out, srSpace())
}

// srDatePick is the date pickers' state between frames.
type srDatePick struct {
	m       srDateMatch
	ok      bool   // a date modifier is at the caret
	shut    string // useOpenUntilDismissed's state: the key it was dismissed for ("" none)
	shutSet bool
	open    bool // SearchDatePickerPopover shows (onOpenChange)
	cal     *dtCalendar
	view    time.Time // the calendar's initial view (it starts over when this changes)

	// range mode (SearchDateRangePickerPopover)
	ranging bool
	anchor  int // getRangeAnchorPrefixLength, -1: none
	snap    []srPiece
	snapSet bool
	start   string // the picked start (N.current)
	rcal    *dtCalendar

	// useSearchDatePickerPosition keeps the last position while the
	// anchor is gone
	left    int
	leftSet bool
	rect    image.Rectangle // the surface, window coords
	press   int             // the outside press detector's tag
	block   blocker
}

// dpOn is omni_date_quick_filters on (3D8U ZP): the date pickers and the
// quick filter menu's Date submenu.
func (p *SearchPopover) dpOn() bool {
	c := p.h.Core()
	return c != nil && c.Experiment("omni_date_quick_filters") == "on"
}

// dpUpdate follows the box: the date at the caret, the picker opened or
// closed for it, range mode's anchor.
func (p *SearchPopover) dpUpdate() {
	d := &p.dpk
	if !p.dpOn() {
		d.ok, d.open, d.ranging = false, false, false
		return
	}
	ps := p.box().pieces()
	if d.ranging {
		d.open, d.cal = false, nil
		d.anchor = -1
		if a, ok := srRangeAnchor(ps, "after"); ok {
			if !d.snapSet {
				d.snap, d.snapSet = ps, true
			}
			d.anchor = a
		}
		if d.rcal == nil {
			now := time.Now()
			d.rcal = &dtCalendar{h: p.h, inline: true, month: dtMonthStart(now), cur: dtDay(now), max: dtDay(now),
				rng: &dtRange{onStart: p.rangeStartPicked, onEnd: p.rangeEndPicked}}
			d.leftSet = false
		}
		return
	}
	// getCaretIndex: the selection's start
	caret, _ := p.ed.Selection()
	m, ok := srDateAt(ps, caret)
	key := ""
	if ok {
		key = m.key()
	}
	if !d.shutSet {
		// useState(a): the match the box opened with stays closed
		d.shut, d.shutSet = key, true
	}
	if d.shut != "" && key == "" {
		d.shut = ""
	}
	d.m, d.ok = m, ok
	open := ok && key != d.shut
	if open && !d.open {
		d.cal, d.leftSet = nil, false
	}
	d.open = open
	if !open {
		d.cal = nil
		return
	}
	now := time.Now()
	view := dtMonthStart(now)
	if v, ok := srCalendarMonth(m.date, now.Location()); ok {
		view = v
	}
	if d.cal == nil || !view.Equal(d.view) {
		// key: w: a new month view starts a new calendar, its active day
		// today in this month, else the 1st
		d.view = view
		cur := view
		if view.Equal(dtMonthStart(now)) {
			cur = dtDay(now)
		}
		d.cal = &dtCalendar{h: p.h, inline: true, month: view, cur: cur, onPick: p.dpPicked}
	}
}

// dpPicked is SearchDatePickerPopover's onDateChange.
func (p *SearchPopover) dpPicked(day time.Time) {
	d := &p.dpk
	if !d.open {
		return
	}
	m := d.m
	tok := srDateToken(m.typ, day.Format(srDateFormat), strings.HasPrefix(m.prefix, "-"))
	d.shut = m.key() // dismiss
	d.open, d.cal = false, nil
	p.filterInsert(srWithDateToken(p.box().pieces(), m, tok), true, false, -1)
}

// dpDismiss closes the picker without a pick (onClose: dismiss, then
// onDismissWithoutSelection: the box gets the focus back).
func (p *SearchPopover) dpDismiss() {
	d := &p.dpk
	if d.ranging {
		p.rangeClose()
		return
	}
	d.shut = d.m.key()
	d.open, d.cal = false, nil
	p.refocus = true
}

// enterRange is the quick filter menu's Range (QuickFilterRow z): it
// types "after: before:" with the caret after "after:", the completers
// run there, and range mode starts.
func (p *SearchPopover) enterRange() {
	p.filterInsert([]srPiece{srText("after: before:")}, false, true, len("after:"))
	d := &p.dpk
	d.ranging, d.snap, d.snapSet, d.start, d.rcal = true, nil, false, "", nil
	d.open, d.cal = false, nil
	p.dpUpdate()
}

// rangeStartPicked is onStartDateChange: the start completes after:.
func (p *SearchPopover) rangeStartPicked(day time.Time) {
	d := &p.dpk
	if day.IsZero() {
		d.start = ""
		return
	}
	d.start = day.Format(srDateFormat)
	p.rangeApply(d.start, "")
}

// rangeEndPicked is onEndDateChange: with an end, both tokens, and range
// mode ends.
func (p *SearchPopover) rangeEndPicked(day time.Time) {
	if day.IsZero() {
		return
	}
	p.rangeApply(p.dpk.start, day.Format(srDateFormat))
	p.rangeExit()
}

// rangeApply is the range popover's P: the box as range mode found it,
// with after: and before: completed for the picks so far.
func (p *SearchPopover) rangeApply(start, end string) {
	d := &p.dpk
	if !d.snapSet {
		return
	}
	ps := d.snap
	if start != "" {
		ps = srWithRangeToken(ps, "after", srDateToken("after", start, false))
	}
	if end != "" {
		ps = srWithRangeToken(ps, "before", srDateToken("before", end, false))
	}
	p.filterInsert(ps, true, false, -1)
}

// rangeClose is the range popover's onClose: after a start, the bare
// before: goes; range mode ends and the box gets the focus back.
func (p *SearchPopover) rangeClose() {
	if p.dpk.start != "" {
		p.filterInsert(srWithoutBareModifier(p.box().pieces(), "before"), true, false, -1)
	}
	p.rangeExit()
	p.refocus = true
}

// rangeExit is onExitDateRangeMode. The single-date picker comes back
// closed for the match at the caret (it mounts with useState(a)).
func (p *SearchPopover) rangeExit() {
	d := &p.dpk
	d.ranging, d.snap, d.snapSet, d.start, d.rcal = false, nil, false, "", nil
	d.shutSet, d.leftSet = false, false
}

// dpShown reports a date picker open (onOpenChange(true)): the list, its
// footer and the quick filter row hide under it.
func (p *SearchPopover) dpShown() bool {
	d := &p.dpk
	return d.open || d.ranging
}

// dpCal is the calendar on screen.
func (p *SearchPopover) dpCal() *dtCalendar {
	d := &p.dpk
	switch {
	case d.ranging && d.leftSet:
		return d.rcal
	case d.open:
		return d.cal
	}
	return nil
}

// dpKeys runs the calendar's keys while it has the focus: the arrows,
// Enter, and Escape (Popover's onClose).
func (p *SearchPopover) dpKeys(gtx layout.Context) {
	cal := p.dpCal()
	if cal == nil {
		return
	}
	for {
		e, ok := gtx.Event(key.Filter{Focus: cal, Name: key.NameEscape})
		if !ok {
			break
		}
		if ke, ok := e.(key.Event); ok && ke.State == key.Press {
			p.dpDismiss()
			return
		}
	}
	cal.keys(gtx)
}

// dpFocusGrid is vu7Q's keydown listener on the input container: ↓ or ↑
// moves the focus to the grid's active day.
func (p *SearchPopover) dpFocusGrid(gtx layout.Context) bool {
	cal := p.dpCal()
	if cal == nil {
		return false
	}
	cal.kbd = true
	gtx.Execute(key.FocusCmd{Tag: cal})
	return true
}

// dpAnchorX is the modifier's right edge in the editor
// (getModifierTokenBounds: the bounds of the text prefixLen before the
// box's end through its first ":").
func (p *SearchPopover) dpAnchorX(prefixLen int) (int, bool) {
	rs := []rune(p.ed.Text())
	start := len(rs) - prefixLen
	if prefixLen < 0 || start < 0 || start >= len(rs) {
		return 0, false
	}
	end := start
	for i := start; i < len(rs); i++ {
		if rs[i] == ':' {
			end = i + 1
			break
		}
	}
	if end <= start {
		return 0, false
	}
	x, found := 0, false
	for _, r := range p.ed.Regions(start, end, nil) {
		x, found = max(x, r.Bounds.Max.X), true
	}
	return x, found
}

// dpLayout draws the open picker under the input row (window
// coordinates): useSearchDatePickerPosition's left (the modifier's right
// edge less 17, kept 8px inside the input row and 333 from its right),
// the top at the input row's bottom (48) plus Popover's offsetY 1: just
// under the wrapper's 1px line, where inputH ends.
func (p *SearchPopover) dpLayout(gtx layout.Context, inputH int) {
	d := &p.dpk
	d.rect = image.Rectangle{}
	cal, prefixLen := d.cal, d.m.prefixLen
	if d.ranging {
		cal, prefixLen = d.rcal, d.anchor
	}
	if cal == nil || !p.dpShown() {
		return
	}
	cal.clicks(gtx)
	if !p.dpShown() || cal != d.cal && cal != d.rcal {
		return // a pick closed it
	}
	if ax, ok := p.dpAnchorX(prefixLen); ok {
		d.left, d.leftSet = p.rect.Min.X+p.edX+ax-dp(gtx, 17), true
	}
	if !d.leftSet || cal != p.dpCal() {
		return
	}
	th := p.h.Theme()
	cl := colorsOf(th)
	pad := dp(gtx, 8)
	bodyPad := 2 * pad // .c-date_picker_calendar's own padding 8
	var sz image.Point
	if d.ranging {
		// DateRangePickerCalendar in singleMonthMode: the 300px month,
		// .c-date_range_picker_calendar__calendars' min-height 340 and
		// margin-bottom 16, no padding of its own
		bodyPad = pad
		sz = image.Pt(2*pad+dp(gtx, 300), 2*pad+dp(gtx, 340)+dp(gtx, 16))
	} else {
		sz = cal.size(gtx, bodyPad)
	}
	left := max(p.rect.Min.X+dp(gtx, 8), min(d.left, p.rect.Max.X-dp(gtx, 8)-dp(gtx, 333)))
	top := p.rect.Min.Y + inputH
	r := image.Rectangle{Min: image.Pt(left, top), Max: image.Pt(left, top).Add(sz)}
	d.rect = r

	// a press elsewhere closes it (Popover's onClose) and still reaches
	// what it was on
	func() {
		defer pointer.PassOp{}.Push(gtx.Ops).Pop()
		defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &d.press)
	}()

	rad := dp(gtx, 8)
	shape := func(r image.Rectangle, rad int) clip.RRect { return clip.RRect{Rect: r, SE: rad, SW: rad} }
	func() {
		// clip-path: inset(0 -16px -16px): no ring or shadow above
		m := dp(gtx, 16)
		defer clip.Rect(image.Rect(r.Min.X-m, r.Min.Y, r.Max.X+m, r.Max.Y+m)).Push(gtx.Ops).Pop()
		softShadow(gtx, r, rad, dp(gtx, 4), dp(gtx, 12), rgba(0x000000, .08))
		func() {
			defer shape(r.Inset(-1), rad+1).Push(gtx.Ops).Pop()
			fill(gtx, r.Inset(-1), cl.Ring)
		}()
		func() {
			defer shape(r, rad).Push(gtx.Ops).Pop()
			fill(gtx, r, cl.Bg)
		}()
	}()
	d.block.Layout(gtx, r)
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	local := image.Rectangle{Max: sz}
	defer shape(local, rad).Push(gtx.Ops).Pop()
	keyTarget(gtx, cal, local)
	cal.drawBody(gtx, th, cl, sz, bodyPad)
}

// dpPointer applies last frame's presses outside the picker.
func (p *SearchPopover) dpPointer(gtx layout.Context) {
	d := &p.dpk
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &d.press, Kinds: pointer.Press})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok || e.Kind != pointer.Press || !p.dpShown() || d.rect.Empty() {
			continue
		}
		if !image.Pt(int(e.Position.X), int(e.Position.Y)).In(d.rect) {
			p.dpDismiss()
		}
	}
}
