package popups

// The search box's completers (search_filter_ac.go, search_filter_menu.go):
// ltuT's getMatch over the text before the caret, Texty's @ and #
// triggers, getInsertData's tokens and their request forms, and the
// popover flow against internal/fakeslack.

import (
	"image"
	"net/url"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/core"
)

// getMatch: a supported modifier at the start or after whitespace, its
// value the rest of the text (single spaces allowed, no double space, no
// space before #, @ or another modifier, at most four spaces in all);
// before:, after: and on: go to the date picker while
// omni_date_quick_filters is on.
func TestSearchFilterMatch(t *testing.T) {
	type want struct {
		typ, mod, value string
		start, anchor   int
	}
	cases := []struct {
		text      string
		dateQuick bool
		ok        bool
		w         want
	}{
		{"from:", true, true, want{"from", "from:", "", 0, 5}},
		{"from:kar", true, true, want{"from", "from:", "kar", 0, 5}},
		{"FROM:Kar", true, true, want{"from", "FROM:", "Kar", 0, 5}},
		{"zephyr from:kar", true, true, want{"from", "from:", "kar", 7, 12}},
		{"-in:gen", true, true, want{"in", "-in:", "gen", 0, 4}},
		{"zephyr -with:ann b", true, true, want{"with", "-with:", "ann b", 7, 13}},
		{"from: kar", true, true, want{"from", "from:", "kar", 0, 5}},
		{"from:@kar", true, true, want{"from", "from:", "@kar", 0, 5}},
		{"from:kar in:", true, true, want{"in", "in:", "", 9, 12}},
		{"has::smi", true, true, want{"has", "has:", ":smi", 0, 4}},
		{"is:th", true, true, want{"is", "is:", "th", 0, 3}},
		{"during:", true, true, want{"during", "during:", "", 0, 7}},
		{"before:", false, true, want{"before", "before:", "", 0, 7}},
		{"before:", true, false, want{}},
		{"on:2026", true, false, want{}},
		{"xfrom:kar", true, false, want{}},
		{"from:  kar", true, false, want{}},
		{"from: #gen", true, false, want{}},
		{"from: @kar", true, false, want{}},
		{"from::kar", true, false, want{}},
		{"from:a b c d e", true, true, want{"from", "from:", "a b c d e", 0, 5}},
		{"from:a b c d e f", true, false, want{}},
		{"from:kar #gen", true, false, want{}},
		{"zephyr", true, false, want{}},
		{"from", true, false, want{}},
	}
	for _, c := range cases {
		m, ok := srFilterMatch([]rune(c.text), c.dateQuick)
		if ok != c.ok {
			t.Errorf("%q: match %v, want %v (%+v)", c.text, ok, c.ok, m)
			continue
		}
		if !ok {
			continue
		}
		got := want{m.typ, m.mod, m.value, m.start, m.anchor}
		if got != c.w || m.end != len([]rune(c.text)) || m.kind != srACFilter {
			t.Errorf("%q: %+v (end %d), want %+v", c.text, got, m.end, c.w)
		}
	}
	if q := srFilterQuery("-from: kar "); q != "kar" {
		t.Errorf("normalizeQuery: %q", q)
	}
}

// Texty's @ and # completers: the word before the caret, or an @ at a word
// boundary with single spaces after it.
func TestSearchMentionMatch(t *testing.T) {
	cases := []struct {
		text  string
		kind  srACKind
		value string
		start int
	}{
		{"@kar", srACMember, "kar", 0},
		{"@", srACMember, "", 0},
		{"zephyr @kar", srACMember, "kar", 7},
		{"@karim h", srACMember, "karim h", 0},
		{"#gen", srACChannel, "gen", 0},
		{"in #gen-er", srACChannel, "gen-er", 3},
		{"a@kar", srACNone, "", 0},
		{"@kar!", srACNone, "", 0},
		{"zephyr", srACNone, "", 0},
	}
	for _, c := range cases {
		m, ok := srMentionMatch([]rune(c.text))
		if ok != (c.kind != srACNone) || ok && (m.kind != c.kind || m.value != c.value || m.start != c.start || m.anchor != c.start) {
			t.Errorf("%q: %v %+v, want kind %v value %q at %d", c.text, ok, m, c.kind, c.value, c.start)
		}
	}
}

// getInsertData: what a pick puts in the box and what the search gets.
func TestSearchFilterPick(t *testing.T) {
	from, _ := srFilterMatch([]rune("from:kar"), true)
	notFrom, _ := srFilterMatch([]rune("-From:kar"), true)
	in, _ := srFilterMatch([]rune("in:gen"), true)
	is, _ := srFilterMatch([]rune("is:th"), true)
	typ, _ := srFilterMatch([]rune("type:can"), true)
	has, _ := srFilterMatch([]rune("has::smi"), true)
	at, _ := srMentionMatch([]rune("@kar"))
	hash, _ := srMentionMatch([]rune("#gen"))
	karim := srACItem{kind: srAIMember, id: "U1", label: "karim", mention: "Karim"}
	gen := srACItem{kind: srAIChannel, id: "C1", label: "general"}
	priv := srACItem{kind: srAIChannel, id: "C2", label: "secret", private: true}
	cases := []struct {
		m                srACMatch
		it               srACItem
		label, req, icon string
		mention          bool
	}{
		{from, karim, "from:@Karim", "from:<@U1>", "user", false},
		{notFrom, karim, "-From:@Karim", "-from:<@U1>", "user", false},
		{in, gen, "in:general", "in:<#C1|general>", "channel", false},
		{in, priv, "in:secret", "in:<#C2|secret>", "lock-filled", false},
		{in, karim, "in:@Karim", "in:<@U1>", "user", false},
		{in, srACStatic(srACValues("in", true, false), "in", "")[0], "in:activity", "in:activity", "notifications", false},
		{is, srACStatic(srACValues("is", true, false), "is", "th")[0], "is:thread", "is:thread", "threads", false},
		{typ, srACStatic(srACValues("type", true, false), "type", "can")[0], "type:canvases & posts", "type:canvases", "canvas", false},
		{has, srACItem{kind: srAIEmoji, id: "smile", label: "smile"}, "has::smile:", "has::smile:", "reaction", false},
		{at, karim, "@Karim", "<@U1>", "", true},
		{hash, gen, "#general", "<#C1|general>", "", true},
	}
	for _, c := range cases {
		tok := srACPick(c.m, c.it)
		if tok.label != c.label || tok.req != c.req || tok.icon != c.icon || tok.mention != c.mention {
			t.Errorf("%q + %s: %+v, want %q %q %q", c.m.match, c.it.label, tok, c.label, c.req, c.icon)
		}
	}
	// searchStaticValues: labels starting with the query, in Slack's order
	var labels []string
	for _, it := range srACStatic(srACValues("is", true, false), "is", "") {
		labels = append(labels, it.label)
	}
	if strings.Join(labels, ",") != "Saved,To-dos,Thread,DM" {
		t.Errorf("is: values %q", labels)
	}
	if v := srACStatic(srACValues("is", false, false), "is", "t"); len(v) != 1 || v[0].id != "thread" {
		t.Errorf("is:t (no to-dos) %+v", v)
	}
	if v := srACStatic(srACValues("during", true, false), "during", "this"); len(v) != 3 || v[0].text != "week" {
		t.Errorf("during:this %+v", v)
	}
}

// Tokens follow edits: kept before and after the change, unwrapped when it
// touches them; the request query swaps each label for its ref.
func TestSearchTokens(t *testing.T) {
	tok := srToken{start: 7, label: "from:@Karim", req: "from:<@U1>"}
	text := "zephyr from:@Karim "
	toks := []srToken{tok}
	if q := srRequestQuery(text, toks); q != "zephyr from:<@U1> " {
		t.Errorf("request query %q", q)
	}
	if got := srShiftTokens(toks, text, text+"x"); len(got) != 1 || got[0].start != 7 {
		t.Errorf("typing after: %+v", got)
	}
	if got := srShiftTokens(toks, text, "a "+text); len(got) != 1 || got[0].start != 9 {
		t.Errorf("typing before: %+v", got)
	}
	if got := srShiftTokens(toks, text, "zephyr from:@Karm "); len(got) != 0 {
		t.Errorf("an edit inside kept the token: %+v", got)
	}
	if q := srRequestQuery("zephyr from:@Karm ", nil); q != "zephyr from:@Karm " {
		t.Errorf("plain text changed: %q", q)
	}
}

// The popover: typing from: and a name opens the people menu under the
// box (the suggestions and the footer hide); Enter takes the person as a
// from:@Name token and a second Enter searches from:<@U…>; Escape closes
// the menu but not the popover; in: lists Activity first; @name picks a
// mention; a query that holds refs opens with its tokens.
func TestSearchPopoverFilterCompleter(t *testing.T) {
	e := srNewEnv(t, false)
	var who core.User
	for _, u := range e.h.a.Snapshot().Users {
		if u.ID != e.h.a.Self().ID && !u.IsBot && !u.Deleted && u.DisplayName != "" && strings.Contains(u.RealName, " ") &&
			(who.ID == "" || u.ID < who.ID) {
			who = u
		}
	}
	if who.ID == "" {
		t.Fatal("no person in the snapshot")
	}
	p := NewSearchPopover(e.h, "", image.Rectangle{})
	d := &srDrv{size: image.Pt(1280, 800), w: p}
	d.frame()
	d.frame()
	srType(d, p, "from:"+who.RealName)
	d.until(t, "the from: match", func() bool { return p.ac.m.kind == srACFilter && p.ac.m.typ == "from" })
	d.until(t, "the people menu", func() bool {
		return p.ac.open && slices.ContainsFunc(p.ac.items, func(it srACItem) bool { return it.id == who.ID })
	})
	if p.ac.m.value != who.RealName || !p.ac.pseudo || p.ac.sel != 0 {
		t.Fatalf("match %+v pseudo %v sel %d", p.ac.m, p.ac.pseudo, p.ac.sel)
	}
	x := d.scan()
	if srHas(x, "Show results for: ") || srHas(x, "Give feedback") {
		t.Errorf("the suggestion list or its footer shows under the menu: %q", x)
	}
	if !srHas(x, "to navigate") || !srHas(x, who.Name) {
		t.Errorf("no menu row or help: %q", x)
	}
	i := slices.IndexFunc(p.ac.items, func(it srACItem) bool { return it.id == who.ID })
	p.ac.sel, p.ac.pseudo = i, false
	d.press(key.NameReturn)
	want := "from:@" + who.DisplayName + " "
	if p.Dismissed() || p.ed.Text() != want || len(p.tokens) != 1 || p.tokens[0].req != "from:<@"+who.ID+">" {
		t.Fatalf("after Enter: dismissed %v text %q tokens %+v", p.Dismissed(), p.ed.Text(), p.tokens)
	}
	if p.ac.open || !slices.Contains(srTitles(p.rows, srRShow), strings.TrimSpace(want)) {
		t.Fatalf("menu open %v, Show results rows %q", p.ac.open, srTitles(p.rows, srRShow))
	}
	for i := range p.rows {
		if p.rows[i].kind == srRShow {
			p.setSel(i)
		}
	}
	d.press(key.NameReturn)
	if got := e.h.navs[len(e.h.navs)-1]; got != "search?q="+url.QueryEscape("from:<@"+who.ID+">") {
		t.Errorf("searched %q", got)
	}

	// Escape closes the menu, not the popover; in: lists Activity first
	p2 := NewSearchPopover(e.h, "", image.Rectangle{})
	d2 := &srDrv{size: image.Pt(1280, 800), w: p2}
	d2.frame()
	d2.frame()
	srType(d2, p2, "in:")
	d2.until(t, "the in: menu", func() bool { return p2.ac.open && len(p2.ac.items) > 1 })
	if it := p2.ac.items[0]; it.kind != srAIValue || it.label != "Activity" {
		t.Errorf("in: first row %+v", it)
	}
	d2.press(key.NameDownArrow)
	if p2.ac.pseudo || p2.ac.sel != 0 {
		t.Errorf("the first ↓: pseudo %v sel %d", p2.ac.pseudo, p2.ac.sel)
	}
	d2.press(key.NameEscape)
	if p2.Dismissed() || p2.ac.open {
		t.Fatalf("Escape: dismissed %v menu %v", p2.Dismissed(), p2.ac.open)
	}
	d2.frame()
	if p2.ac.open {
		t.Error("the menu came back without typing")
	}

	// @name: Tab takes the person as a mention
	p3 := NewSearchPopover(e.h, "", image.Rectangle{})
	d3 := &srDrv{size: image.Pt(1280, 800), w: p3}
	d3.frame()
	d3.frame()
	srType(d3, p3, "@"+who.RealName)
	d3.until(t, "the @ match", func() bool { return p3.ac.m.kind == srACMember && p3.ac.m.value == who.RealName })
	d3.until(t, "the @ menu", func() bool {
		return p3.ac.open && p3.ac.m.kind == srACMember && slices.ContainsFunc(p3.ac.items, func(it srACItem) bool { return it.id == who.ID })
	})
	p3.ac.sel = slices.IndexFunc(p3.ac.items, func(it srACItem) bool { return it.id == who.ID })
	p3.ac.pseudo = false
	d3.press(key.NameTab)
	if p3.ed.Text() != "@"+who.DisplayName+" " || len(p3.tokens) != 1 || !p3.tokens[0].mention || p3.reqQuery(p3.ed.Text()) != "<@"+who.ID+"> " {
		t.Errorf("after Tab: %q %+v", p3.ed.Text(), p3.tokens)
	}

	// a query with refs opens as its tokens, and the box's own query opens
	// no menu (Texty completes what the user types)
	p4 := NewSearchPopover(e.h, "from:<@"+who.ID+"> zephyr", image.Rectangle{})
	if p4.ed.Text() != "from:@"+who.DisplayName+" zephyr" || len(p4.tokens) != 1 || p4.reqQuery(p4.query) != "from:<@"+who.ID+"> zephyr" {
		t.Errorf("display query %q tokens %+v", p4.ed.Text(), p4.tokens)
	}
	if s := SearchDisplayText(e.h, "in:<#C0FAKE00001|general> <@"+who.ID+">"); s != "in:general @"+who.DisplayName {
		t.Errorf("SearchDisplayText %q", s)
	}
	p5 := NewSearchPopover(e.h, "zephyr from:", image.Rectangle{})
	d5 := &srDrv{size: image.Pt(1280, 800), w: p5}
	for i := 0; i < 5; i++ {
		d5.frame()
	}
	if p5.ac.open {
		t.Error("the query the box opened with opened the menu")
	}
}

// srType types s at the end of the popover's box: the edit, then the
// caret after it (the window's EditorInsert sends both).
func srType(d *srDrv, p *SearchPopover, s string) {
	n := p.ed.Len()
	c := n + utf8.RuneCountInString(s)
	d.r.Queue(key.EditEvent{Range: key.Range{Start: n, End: n}, Text: s}, key.SelectionEvent{Start: c, End: c})
	d.frame()
	d.frame()
}
