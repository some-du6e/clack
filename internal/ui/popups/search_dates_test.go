package popups

// The quick filter row, the date pickers, the tokens' pills and the
// suggestion toolbar (search_quick_filters.go, search_date_picker.go,
// search_token_pills.go, search_toolbar.go) against internal/fakeslack,
// which has omni_date_quick_filters on.

import (
	"image"
	"slices"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// gtx is a context at the drivers' metric, for measuring.
func (d *srDrv) gtx() layout.Context {
	return layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Now: time.Now()}
}

// matchIncompleteDateFilter and getDateModifierAtCaret: before:, after:
// or on: (a "-" kept, any case) at the start or after whitespace, its
// value a partial date (isPartialDateValue takes one digit of the day:
// a whole date isn't partial any more); the date runs on through the
// word after the caret; a token ending the text before the caret matches
// nothing.
func TestSearchDateMatch(t *testing.T) {
	cases := []struct {
		text               string
		ok                 bool
		typ, prefix, value string
	}{
		{"before:", true, "before", "before:", ""},
		{"zephyr after:2026-1", true, "after", "after:", "2026-1"},
		{"-On:2026-11-0", true, "on", "-On:", "2026-11-0"},
		{"before:2026-11-0", true, "before", "before:", "2026-11-0"},
		{"before:2026-11-05", false, "", "", ""},
		{"before:2026-111", false, "", "", ""},
		{"before:today", false, "", "", ""},
		{"during:", false, "", "", ""},
		{"xbefore:", false, "", "", ""},
		{"before:2026 ", false, "", "", ""},
	}
	for _, c := range cases {
		typ, prefix, value, ok := srTrailingDate(c.text)
		if ok != c.ok || ok && (typ != c.typ || prefix != c.prefix || value != c.value) {
			t.Errorf("%q: %v %q %q %q, want %v %q %q %q", c.text, ok, typ, prefix, value, c.ok, c.typ, c.prefix, c.value)
		}
	}

	m, ok := srDateAt([]srPiece{srText("zephyr before:2026-0")}, -1)
	if !ok || m.typ != "before" || m.start != 7 || m.length != 13 || m.date != "2026-0" || m.prefixLen != 13 || m.key() != "7:before:" {
		t.Errorf("at the end: %v %+v", ok, m)
	}
	// the caret inside the date: dateValue runs through the word
	m, ok = srDateAt([]srPiece{srText("before:2026-03 more")}, 13)
	if !ok || m.value != "2026-0" || m.date != "2026-03" || m.length != 14 || m.prefixLen != 19 {
		t.Errorf("mid-date: %v %+v", ok, m)
	}
	// right after the modifier the date is empty whatever follows
	m, ok = srDateAt([]srPiece{srText("on:2026 x")}, 3)
	if !ok || m.value != "" || m.date != "" || m.length != 3 {
		t.Errorf("after the modifier: %v %+v", ok, m)
	}
	tok := srDateToken("before", "2026-11-04", false)
	if _, ok := srDateAt([]srPiece{srTokPiece(tok)}, -1); ok {
		t.Error("a completed token at the caret matched")
	}
	if _, ok := srDateAt([]srPiece{srTokPiece(tok), srText(" ")}, -1); ok {
		t.Error("the space after a token matched")
	}

	loc := time.UTC
	for v, want := range map[string]string{"2026": "2026-01", "2026-03": "2026-03", "2026-03-05": "2026-03",
		"2026-13": "2026-01", "2026-": "2026-01", "2026-1": "2026-01"} {
		got, ok := srCalendarMonth(v, loc)
		if !ok || got.Format("2006-01") != want {
			t.Errorf("getCalendarMonthForPartialDate(%q) = %v %v, want %s", v, got, ok, want)
		}
	}
	for _, v := range []string{"", "202", "20261"} {
		if _, ok := srCalendarMonth(v, loc); ok {
			t.Errorf("getCalendarMonthForPartialDate(%q) has a month", v)
		}
	}
	if n := srDateToken("after", "2026-11-04", true); n.label != "-after:2026-11-04" || n.mod != "-after" ||
		n.entity != "2026-11-04" || n.req != n.label || n.icon != "calendar" {
		t.Errorf("buildCompletedDateFilterOp negated: %+v", n)
	}
}

// opsWithCompletedDateToken, opsWithCompletedRangeToken,
// opsWithBareModifierRemoved and getRangeAnchorPrefixLength.
func TestSearchDateOps(t *testing.T) {
	ps := []srPiece{srText("zephyr before:2026-1")}
	m, _ := srDateAt(ps, -1)
	b := srJoin(srWithDateToken(ps, m, srDateToken("before", "2026-11-05", false)))
	if b.text != "zephyr before:2026-11-05 " || len(b.toks) != 1 || b.toks[0].start != 7 {
		t.Errorf("completed at the end: %q %+v", b.text, b.toks)
	}
	ps = []srPiece{srText("zephyr before:2026 more")}
	m, _ = srDateAt(ps, 18)
	if b = srJoin(srWithDateToken(ps, m, srDateToken("before", "2026-11-05", false))); b.text != "zephyr before:2026-11-05 more" {
		t.Errorf("completed before a space: %q", b.text)
	}

	ps = []srPiece{srText("zephyr after: before:")}
	if n, ok := srRangeAnchor(ps, "after"); !ok || n != 14 {
		t.Errorf("range anchor %d %v", n, ok)
	}
	ps = srWithRangeToken(ps, "after", srDateToken("after", "2026-11-01", false))
	if b = srJoin(ps); b.text != "zephyr after:2026-11-01 before:" || len(b.toks) != 1 {
		t.Errorf("range start %q %+v", b.text, b.toks)
	}
	if _, ok := srRangeAnchor(ps, "after"); ok {
		t.Error("the anchor is still there after the start")
	}
	bare := srJoin(srWithoutBareModifier(ps, "before"))
	if bare.text != "zephyr after:2026-11-01 " || len(bare.toks) != 1 {
		t.Errorf("bare before: dropped: %q", bare.text)
	}
	ps = srWithRangeToken(ps, "before", srDateToken("before", "2026-11-05", false))
	if b = srJoin(ps); b.text != "zephyr after:2026-11-01 before:2026-11-05 " || len(b.toks) != 2 || b.toks[1].start != 24 {
		t.Errorf("range end %q %+v", b.text, b.toks)
	}
	if srTrailing([]srPiece{srText("a"), srTokPiece(srDateToken("on", "2026-11-01", false))}) != "" {
		t.Error("getTrailingText of a token isn't empty")
	}
}

// The row's buttons and what they do to the box: a pill puts its token
// and a space at the end (the box's trailing whitespace trimmed first)
// and takes it out again; a mention typed after "from:" counts as the
// filter and goes with its "from:"; Last 7 days is after: a week ago and
// before: tomorrow; In this channel follows the conversation on screen.
func TestSearchQuickFilters(t *testing.T) {
	e := srNewEnv(t, false)
	p := NewSearchPopover(e.h, "", image.Rectangle{})
	d := &srDrv{size: image.Pt(1280, 800), w: p}
	d.frame()
	d.frame()
	self := e.h.a.Self()
	me := self.DisplayName
	if me == "" {
		me = self.RealName
	}
	var labels []string
	fs := p.quickFilters()
	for _, f := range fs {
		labels = append(labels, f.label)
	}
	if strings.Join(labels, ",") != "From me,Includes me,Has a link,Threads only,In Activity,Last 7 days" {
		t.Fatalf("buttons %q", labels)
	}
	p.qfToggle(fs[0])
	if p.ed.Text() != "from:@"+me+" " || len(p.tokens) != 1 || p.tokens[0].req != "from:<@"+self.ID+">" ||
		p.tokens[0].icon != "user" || !p.qfActive(fs[0], time.Now()) {
		t.Fatalf("From me on: %q %+v", p.ed.Text(), p.tokens)
	}
	p.qfToggle(fs[0])
	if p.ed.Text() != "" || len(p.tokens) != 0 {
		t.Fatalf("From me off: %q %+v", p.ed.Text(), p.tokens)
	}

	p.setBox(srBox{text: "zephyr  "}, -1)
	p.qfToggle(fs[2])
	if p.ed.Text() != "zephyr has:link " || p.reqQuery(p.ed.Text()) != "zephyr has:link " || p.tokens[0].icon != "link" {
		t.Errorf("Has a link: %q %+v", p.ed.Text(), p.tokens)
	}
	p.qfToggle(fs[5])
	a, b := srDatePresetWeek(time.Now())
	want := "zephyr has:link after:" + a + " before:" + b + " "
	if p.ed.Text() != want || len(p.tokens) != 3 || !p.qfWeekActive(time.Now()) {
		t.Fatalf("Last 7 days: %q, want %q", p.ed.Text(), want)
	}
	p.qfToggle(fs[5])
	if strings.TrimSpace(p.ed.Text()) != "zephyr has:link" || len(p.tokens) != 1 || p.qfWeekActive(time.Now()) {
		t.Errorf("Last 7 days off: %q %+v", p.ed.Text(), p.tokens)
	}
	// typed dates count too
	p.setBox(srBox{text: "after:" + a + " before:" + b}, -1)
	if !p.qfWeekActive(time.Now()) {
		t.Error("typed Last 7 days dates aren't active")
	}

	// a mention after "from:" is the From me filter
	mention := srToken{label: "@" + me, req: "<@" + self.ID + ">", mention: true, entity: self.ID}
	p.setBox(srJoin([]srPiece{srText("zephyr from:"), srTokPiece(mention), srSpace()}), -1)
	if !p.qfActive(fs[0], time.Now()) {
		t.Fatalf("from: and a mention of me isn't From me: %q %+v", p.ed.Text(), p.tokens)
	}
	p.qfToggle(fs[0])
	if p.ed.Text() != "zephyr  " || len(p.tokens) != 0 {
		t.Errorf("From me off took %q %+v", p.ed.Text(), p.tokens)
	}

	// In this channel: the conversation on screen
	ch := e.fs.ChannelIDs()[1]
	RecordRecentPlace(ch, "", "")
	fs = p.quickFilters()
	name := e.h.a.ConvRef(ch).Name
	if fs[0].label != "In this channel" || fs[0].entity != ch || fs[0].display != name {
		t.Fatalf("In this channel %+v", fs[0])
	}
	p.setBox(srBox{}, -1)
	p.qfToggle(fs[0])
	if p.ed.Text() != "in:"+name+" " || p.tokens[0].req != "in:<#"+ch+"|"+name+">" || p.tokens[0].icon != "channel" {
		t.Errorf("In this channel: %q %+v", p.ed.Text(), p.tokens)
	}

	// the + menu: everything fits, so More options only, with the Date
	// submenu (omni_date_quick_filters)
	p.qf.shown = len(fs)
	items := p.qfMenuItems(fs)
	var got []string
	for _, it := range items {
		s := it.Label + "=" + it.Hint
		if len(it.Sub) > 0 {
			var sub []string
			for _, x := range it.Sub {
				sub = append(sub, x.Label+"="+x.Hint)
			}
			s += "[" + strings.Join(sub, ",") + "]"
		}
		got = append(got, s)
	}
	wantMenu := "From someone=from:,Includes someone=with:,In channel=in:,Message has=has:,Message is=is:," +
		"File type=type:,My reaction=hasmy:,Date=[On=on:,Before=before:,After=after:,Range=after: before:]"
	if strings.Join(got, ",") != wantMenu {
		t.Errorf("+ menu %q\nwant %q", strings.Join(got, ","), wantMenu)
	}
	// two buttons left over: Quick filters first
	p.qf.shown = len(fs) - 2
	items = p.qfMenuItems(fs)
	if len(items) < 5 || items[0].Header != "Quick filters" || items[1].Label != "In Activity" || items[2].Label != "Last 7 days" ||
		!items[3].Separator || items[4].Header != "More options" {
		t.Errorf("+ menu with overflow: %+v", items[:min(5, len(items))])
	}
	// the menu's In Activity: "in:activity", no toggle
	p.setBox(srBox{}, -1)
	items[1].Do()
	if p.ed.Text() != "in:activity " || len(p.tokens) != 1 || p.tokens[0].icon != "notifications" {
		t.Errorf("menu In Activity: %q %+v", p.ed.Text(), p.tokens)
	}
	// From someone types "from:" and the completer opens on it
	p.setBox(srBox{}, -1)
	for _, it := range p.qfMenuItems(fs) {
		if it.Label == "From someone" {
			it.Do()
		}
	}
	if p.ed.Text() != "from:" || !p.acArmed {
		t.Fatalf("From someone: %q armed %v", p.ed.Text(), p.acArmed)
	}
	d.until(t, "the people menu", func() bool { return p.ac.open && p.ac.m.typ == "from" })
}

// The filter button toggles the row ("Show filters" / "Hide filters");
// the row hides under a completer's menu; a box opening with a token
// shows it; pills that don't fit go to the + menu.
func TestSearchQuickFilterRow(t *testing.T) {
	e := srNewEnv(t, false)
	p := NewSearchPopover(e.h, fakeslack.SearchWord, image.Rectangle{})
	d := &srDrv{size: image.Pt(1280, 800), w: p}
	d.until(t, "the switcher", func() bool { return p.snapOK })
	if p.qf.show {
		t.Fatal("the row shows before the filter button")
	}
	p.filter.Click()
	d.frame()
	d.frame()
	if !p.qf.show || !p.qfVisible() {
		t.Fatal("the filter button didn't show the row")
	}
	x := d.scan()
	all := []string{"From me", "Includes me", "Has a link", "Threads only", "In Activity", "Last 7 days"}
	// the 649px switcher holds the first few; the rest are the + menu's
	if p.qf.shown < 3 || p.qf.shown > len(all) {
		t.Fatalf("%d pills fit in the 649px switcher", p.qf.shown)
	}
	for i, l := range all {
		if srHas(x, l) != (i < p.qf.shown) {
			t.Errorf("pill %q drawn %v, %d fit: %q", l, srHas(x, l), p.qf.shown, x)
		}
	}
	if p.qf.shown < len(all) {
		items := p.qfMenuItems(p.quickFilters())
		if items[0].Header != "Quick filters" || items[1].Label != all[p.qf.shown] {
			t.Errorf("the + menu's Quick filters %+v", items[:2])
		}
	}
	p.qf.btns[2].Click() // Has a link
	d.frame()
	d.frame()
	if p.ed.Text() != fakeslack.SearchWord+" has:link " {
		t.Errorf("the pill typed %q", p.ed.Text())
	}
	p.filter.Click()
	d.frame()
	d.frame()
	if p.qf.show {
		t.Error("the filter button didn't hide the row")
	}

	// a token in the box shows it by itself; a completer's menu hides it
	who := srTestPerson(t, e)
	p2 := NewSearchPopover(e.h, "from:<@"+who.ID+"> zephyr", image.Rectangle{})
	d2 := &srDrv{size: image.Pt(1280, 800), w: p2}
	d2.frame()
	d2.frame()
	if !p2.qf.show {
		t.Error("a box opening with a token didn't show the row")
	}
	srType(d2, p2, " in:")
	d2.until(t, "the in: menu", func() bool { return p2.ac.open })
	if p2.qfVisible() {
		t.Error("the row shows under the completer's menu")
	}

	// narrow: the last pills go to the + menu
	p3 := NewSearchPopover(e.h, "", image.Rectangle{})
	d3 := &srDrv{size: image.Pt(520, 800), w: p3}
	p3.qfToggleRow()
	d3.frame()
	d3.frame()
	if p3.qf.shown >= 6 || p3.qf.shown == 0 {
		t.Fatalf("%d of 6 pills fit at 520", p3.qf.shown)
	}
	p3.qf.more.Click()
	d3.frame()
	d3.frame()
	m := srMenu(e.h)
	if m == nil || len(m.items) == 0 || m.items[0].Header != "Quick filters" {
		t.Fatalf("+ menu %v", m)
	}
}

// Typing a date modifier opens the calendar under the box (the list and
// its footer hide); ↓ moves into its grid, → a day, Enter completes the
// token; Escape closes it until the modifier changes; the typed month is
// the one shown.
func TestSearchDatePicker(t *testing.T) {
	e := srNewEnv(t, false)
	p := NewSearchPopover(e.h, "", image.Rectangle{})
	d := &srDrv{size: image.Pt(1280, 800), w: p}
	d.frame()
	d.frame()
	srType(d, p, "zephyr before:")
	d.until(t, "the date picker", func() bool { return p.dpk.open && p.dpk.cal != nil && !p.dpk.rect.Empty() })
	if p.rect.Dy() != dp(d.gtx(), 48)+1 {
		t.Errorf("card %v: the list didn't hide under the picker", p.rect)
	}
	x := d.scan()
	if !srHas(x, time.Now().Format("January 2006")) || srHas(x, "Give feedback") {
		t.Errorf("picker frame %q", x)
	}
	if w := p.dpk.rect.Dx(); w != 333 {
		t.Errorf("surface %d wide, want 333", w)
	}
	if p.dpk.rect.Min.Y != p.rect.Min.Y+49 {
		t.Errorf("surface top %d, card %v", p.dpk.rect.Min.Y, p.rect)
	}
	d.press(key.NameDownArrow)
	if !p.dpk.cal.kbd {
		t.Fatal("↓ didn't move into the grid")
	}
	d.press(key.NameRightArrow)
	day := dtDay(time.Now()).AddDate(0, 0, 1).Format(srDateFormat)
	d.press(key.NameReturn)
	want := "zephyr before:" + day + " "
	if p.ed.Text() != want || len(p.tokens) != 1 || p.tokens[0].label != "before:"+day || p.dpk.open {
		t.Fatalf("after Enter: %q %+v open %v, want %q", p.ed.Text(), p.tokens, p.dpk.open, want)
	}
	if q := p.reqQuery(p.ed.Text()); q != want {
		t.Errorf("request %q", q)
	}

	// Escape closes it; more digits keep it closed; a new modifier opens it
	p2 := NewSearchPopover(e.h, "", image.Rectangle{})
	d2 := &srDrv{size: image.Pt(1280, 800), w: p2}
	d2.frame()
	d2.frame()
	srType(d2, p2, "on:")
	d2.until(t, "the on: picker", func() bool { return p2.dpk.open })
	d2.press(key.NameEscape)
	if p2.dpk.open || p2.Dismissed() {
		t.Fatalf("Escape: open %v dismissed %v", p2.dpk.open, p2.Dismissed())
	}
	srType(d2, p2, "2026-0")
	if p2.dpk.open {
		t.Error("typing the date reopened a dismissed picker")
	}
	srType(d2, p2, " after:2026-03")
	d2.until(t, "the after: picker", func() bool { return p2.dpk.open && p2.dpk.cal != nil })
	if mo := p2.dpk.cal.month.Format("2006-01"); mo != "2026-03" {
		t.Errorf("the typed month shows %s", mo)
	}
	// a press elsewhere closes it too
	p2.dpk.shut = ""
	pt := f32.Pt(float32(p2.rect.Min.X+20), float32(p2.rect.Min.Y+24))
	d2.r.Queue(pointer.Event{Kind: pointer.Press, Position: pt, Buttons: pointer.ButtonPrimary, Source: pointer.Mouse},
		pointer.Event{Kind: pointer.Release, Position: pt, Source: pointer.Mouse})
	d2.frame()
	d2.frame()
	if p2.dpk.open {
		t.Error("a press on the box left the picker open")
	}
}

// Range: "after: before:" with the caret after "after:", a one-month
// calendar without future days; the first pick completes after:, the
// second before: and ends range mode; closing after the first pick drops
// the bare before:.
func TestSearchDateRange(t *testing.T) {
	e := srNewEnv(t, false)
	p := NewSearchPopover(e.h, "zephyr", image.Rectangle{})
	d := &srDrv{size: image.Pt(1280, 800), w: p}
	d.frame()
	d.frame()
	var rng MenuItem
	p.qf.shown = len(p.quickFilters())
	for _, it := range p.qfMenuItems(p.quickFilters()) {
		for _, s := range it.Sub {
			if s.Label == "Range" {
				rng = s
			}
		}
	}
	if rng.Do == nil {
		t.Fatal("no Date > Range")
	}
	rng.Do()
	if s, _ := p.ed.Selection(); p.ed.Text() != "zephyr after: before:" || s != len("zephyr after:") || !p.dpk.ranging {
		t.Fatalf("Range typed %q caret %d ranging %v", p.ed.Text(), s, p.dpk.ranging)
	}
	d.until(t, "the range picker", func() bool { return p.dpk.leftSet && !p.dpk.rect.Empty() })
	if w := p.dpk.rect.Dx(); w != 316 {
		t.Errorf("range surface %d wide, want 316", w)
	}
	cal := p.dpk.rcal
	today := dtDay(time.Now())
	cal.pick(today.AddDate(0, 0, 1)) // after today: disabled
	if p.ed.Text() != "zephyr after: before:" {
		t.Errorf("a future day was picked: %q", p.ed.Text())
	}
	s, en := today.AddDate(0, 0, -3), today.AddDate(0, 0, -1)
	cal.pick(s)
	if p.ed.Text() != "zephyr after:"+s.Format(srDateFormat)+" before:" || !p.dpk.ranging {
		t.Fatalf("start: %q ranging %v", p.ed.Text(), p.dpk.ranging)
	}
	d.frame()
	cal.pick(en)
	want := "zephyr after:" + s.Format(srDateFormat) + " before:" + en.Format(srDateFormat) + " "
	if p.ed.Text() != want || len(p.tokens) != 2 || p.dpk.ranging {
		t.Fatalf("end: %q %+v ranging %v, want %q", p.ed.Text(), p.tokens, p.dpk.ranging, want)
	}
	d.frame()
	if p.dpShown() {
		t.Error("a picker shows after the range")
	}

	p2 := NewSearchPopover(e.h, "", image.Rectangle{})
	d2 := &srDrv{size: image.Pt(1280, 800), w: p2}
	d2.frame()
	p2.enterRange()
	d2.until(t, "the second range picker", func() bool { return p2.dpk.leftSet })
	p2.dpk.rcal.pick(s)
	d2.press(key.NameEscape)
	if p2.ed.Text() != "after:"+s.Format(srDateFormat)+" " || p2.dpk.ranging || p2.Dismissed() {
		t.Errorf("closed after the start: %q ranging %v", p2.ed.Text(), p2.dpk.ranging)
	}
}

// A filter token's modifier sits after the pill's margin and icon column
// (24px), its value is bold; the icon's click deletes the token and the
// space after it.
func TestSearchTokenPills(t *testing.T) {
	e := srNewEnv(t, false)
	p := NewSearchPopover(e.h, "", image.Rectangle{})
	d := &srDrv{size: image.Pt(1280, 800), w: p}
	d.frame()
	tok := srToken{label: "from:@Ann", req: "from:<@U0FAKE0001>", icon: "user", mod: "from", entity: "U0FAKE0001"}
	p.setBox(srJoin([]srPiece{srText("zephyr "), srTokPiece(tok), srText(" more")}), -1)
	d.frame()
	d.frame()
	sp := p.ed.Spans()
	if len(sp) != 2 || sp[0].Start != 7 || sp[0].End != 12 || sp[0].Before != 24 || sp[1].Start != 12 || sp[1].End != 16 ||
		sp[1].Font == nil || sp[1].Font.Weight != srBoldEditorFont.Weight || sp[1].After != 4 {
		t.Fatalf("spans %+v", sp)
	}
	space := p.ed.Regions(6, 7, nil)
	more := p.ed.Regions(17, 18, nil) // the "m" after the pill's space
	mod, ok1 := p.ed.SpanBounds(0)
	val, ok2 := p.ed.SpanBounds(1)
	if len(space) == 0 || len(more) == 0 || !ok1 || !ok2 {
		t.Fatalf("no regions or span bounds: %v %v %v %v", space, more, ok1, ok2)
	}
	if gap := mod.Min.X - space[0].Bounds.Max.X; gap != 24 {
		t.Errorf("the modifier starts %dpx after the text, want 24", gap)
	}
	if val.Min.X != mod.Max.X {
		t.Errorf("the value starts at %d, the modifier ends at %d", val.Min.X, mod.Max.X)
	}
	g := d.gtx()
	th := p.h.Theme()
	bold, reg := textW(g, th, 15, theme.Bold, "@Ann"), textW(g, th, 15, theme.Regular, "@Ann")
	if w := val.Dx(); bold <= reg || w < bold-1 || w > bold+1 {
		t.Errorf("value %dpx wide, bold %d, regular %d", w, bold, reg)
	}
	// 4px after the value, then the space
	if sp, got := textW(g, th, 15, theme.Regular, " "), more[0].Bounds.Min.X-val.Max.X; got < sp+3 || got > sp+5 {
		t.Errorf("the text after the pill starts %dpx after the value (a space is %d), want the space and 4", got, sp)
	}
	// the icon's click (7.5 to 25.5px into the pill)
	pill := mod.Min.X - 23
	pt := f32.Pt(float32(p.rect.Min.X+p.edX+pill+12), float32(p.rect.Min.Y+24))
	d.r.Queue(pointer.Event{Kind: pointer.Move, Position: pt, Source: pointer.Mouse})
	d.frame()
	d.frame()
	if p.pills.hover != 1 {
		t.Errorf("hover %d over the icon", p.pills.hover)
	}
	d.r.Queue(pointer.Event{Kind: pointer.Press, Position: pt, Buttons: pointer.ButtonPrimary, Source: pointer.Mouse},
		pointer.Event{Kind: pointer.Release, Position: pt, Source: pointer.Mouse})
	d.frame()
	d.frame()
	if p.ed.Text() != "zephyr more" || len(p.tokens) != 0 {
		t.Errorf("after the icon's click: %q %+v", p.ed.Text(), p.tokens)
	}
	if len(p.ed.Spans()) != 0 {
		t.Errorf("spans left %+v", p.ed.Spans())
	}
}

// srSplitHost is a host with the split view and search in channel.
type srSplitHost struct {
	*tzHost
	splits, searches []string
}

func (h *srSplitHost) OpenSplit(route string)        { h.splits = append(h.splits, route) }
func (h *srSplitHost) SplitViewOK() bool             { return true }
func (h *srSplitHost) InSplitView() bool             { return false }
func (h *srSplitHost) OpenSearchInChannel(ch string) { h.searches = append(h.searches, ch) }

// The picked row's toolbar: a person's View profile and Open in…, a
// channel's Search in channel, a recent search's Copy search query and
// Delete; Tab focuses the first button, Enter runs it; the footer reads
// "Tab Actions".
func TestSearchToolbar(t *testing.T) {
	e := srNewEnv(t, false)
	sh := &srSplitHost{tzHost: e.h}
	labels := func(p *SearchPopover) string {
		var out []string
		for _, a := range p.tbFor() {
			out = append(out, a.label)
		}
		return strings.Join(out, "|")
	}

	// a channel
	ch := e.fs.ChannelIDs()[3]
	name := e.h.a.ConvRef(ch).Name
	p := NewSearchPopover(sh, name, image.Rectangle{})
	d := &srDrv{size: image.Pt(1280, 800), w: p}
	d.until(t, "the channel row", func() bool { return p.snapOK && len(p.rows) > 0 && p.rows[0].id == ch })
	d.press(key.NameDownArrow)
	if got := labels(p); got != "Search in channel|Open in…" {
		t.Fatalf("channel actions %q", got)
	}
	if x := d.scan(); !srHas(x, "Tab") || !srHas(x, "Actions") {
		t.Errorf("footer %q", x)
	}
	d.press(key.NameTab)
	if p.tb.focus != 1 {
		t.Fatalf("Tab: focus %d", p.tb.focus)
	}
	d.press(key.NameTab)
	if p.tb.focus != 2 {
		t.Errorf("second Tab: focus %d", p.tb.focus)
	}
	d.press(key.NameReturn) // Open in…
	m := srMenu(e.h)
	if m == nil || len(m.items) != 1 || m.items[0].Label != "Open in split view" {
		t.Fatalf("Open in… menu %v", m)
	}
	m.items[0].Do()
	if !slices.Equal(sh.splits, []string{"c/" + ch}) || !p.Dismissed() {
		t.Errorf("split %q dismissed %v", sh.splits, p.Dismissed())
	}

	p = NewSearchPopover(sh, name, image.Rectangle{})
	d = &srDrv{size: image.Pt(1280, 800), w: p}
	d.until(t, "the channel row again", func() bool { return p.snapOK && len(p.rows) > 0 && p.rows[0].id == ch })
	d.press(key.NameDownArrow)
	d.press(key.NameTab)
	d.press(key.NameReturn) // Search in channel
	if !slices.Equal(sh.searches, []string{ch}) || !p.Dismissed() || e.h.navs[len(e.h.navs)-1] != "c/"+ch {
		t.Errorf("search in channel %q navs %q dismissed %v", sh.searches, e.h.navs, p.Dismissed())
	}

	// a person: View profile
	who := srTestPerson(t, e)
	p = NewSearchPopover(sh, who.RealName, image.Rectangle{})
	d = &srDrv{size: image.Pt(1280, 800), w: p}
	d.until(t, "the person row", func() bool {
		return p.snapOK && slices.ContainsFunc(p.rows, func(r srRow) bool { return r.kind == srRUser && r.id == who.ID })
	})
	for i := range p.rows {
		if p.rows[i].kind == srRUser && p.rows[i].id == who.ID {
			p.pick(i)
		}
	}
	if got := labels(p); !strings.HasPrefix(got, "View profile") {
		t.Fatalf("person actions %q", got)
	}
	old := OpenFullProfile
	var opened string
	OpenFullProfile = func(id string) { opened = id }
	t.Cleanup(func() { OpenFullProfile = old })
	p.tb.run = 1
	d.frame()
	if opened != who.ID || !p.Dismissed() {
		t.Errorf("View profile opened %q dismissed %v", opened, p.Dismissed())
	}

	// a recent search: Copy search query, then Delete
	e.fs.SetSearchHistory("alpha launch")
	p = NewSearchPopover(sh, "", image.Rectangle{})
	d = &srDrv{size: image.Pt(1280, 800), w: p}
	d.until(t, "the snapshot", func() bool { return p.snapOK })
	p.history.Click()
	d.until(t, "the history row", func() bool { return slices.Contains(srTitles(p.rows, srRQuery), "alpha launch") })
	for i := range p.rows {
		if p.rows[i].kind == srRQuery {
			p.pick(i)
		}
	}
	if got := labels(p); got != "Copy search query|Delete" {
		t.Fatalf("history actions %q", got)
	}
	p.tb.run = 1
	d.frame()
	if _, data, ok := d.r.WriteClipboard(); !ok || string(data) != "alpha launch" {
		t.Errorf("copied %q %v", data, ok)
	}
	if p.tb.active != 1 {
		t.Error("Copy search query didn't show Query copied!")
	}
	p.tb.run = 2
	d.frame()
	d.until(t, "search.delete", func() bool { return len(e.fs.SearchForms("search.delete")) == 1 })
	d.until(t, "the row's removal", func() bool { return !slices.Contains(srTitles(p.rows, srRQuery), "alpha launch") })
}
