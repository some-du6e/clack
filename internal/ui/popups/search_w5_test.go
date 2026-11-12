package popups

// gap-w5: setText (↑ / ↓ onto a query suggestion shows its text in the
// box without a new search; other rows and none put the user's query back;
// the pointer and the caret keys leave the box alone) and the footer's
// offline notice (isClientOnline false: the notice in place of the key
// hints and the feedback link).

import (
	"image"
	"net/url"
	"slices"
	"strings"
	"testing"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// srIndexOf is the first row of kind k titled title (-1: none).
func srIndexOf(p *SearchPopover, k srRowKind, title string) int {
	for i := range p.rows {
		if p.rows[i].kind == k && p.rows[i].title == title {
			return i
		}
	}
	return -1
}

func TestSearchSetText(t *testing.T) {
	e := srNewEnv(t, false)
	ch := e.fs.ChannelIDs()[1]
	cn := e.h.a.ConvRef(ch).Name
	e.fs.SetSearchHistory("alpha launch", "roadmap in:<#"+ch+"|"+cn+">")

	// history mode: the recent searches
	p := NewSearchPopover(e.h, "", image.Rectangle{})
	d := &srDrv{size: image.Pt(1280, 800), w: p}
	d.until(t, "the snapshot", func() bool { return p.snapOK })
	p.history.Click()
	d.until(t, "the history rows", func() bool { return len(srTitles(p.rows, srRQuery)) == 2 })
	rows := srTitles(p.rows, srRQuery)
	seq := p.seq

	// ↓ picks the newest, and the box shows it
	d.press(key.NameDownArrow)
	r := p.rows[p.sel]
	if !p.picked || r.kind != srRQuery {
		t.Fatalf("↓ picked %v %+v", p.picked, r)
	}
	if got := p.ed.Text(); got != r.title || p.query != r.title {
		t.Errorf("box %q (query %q), want the recent search %q", got, p.query, r.title)
	}
	if p.userQuery() != "" || !p.uqOn {
		t.Errorf("user query %q (shown %v), want the empty one kept", p.userQuery(), p.uqOn)
	}
	if p.seq != seq || !slices.Equal(srTitles(p.rows, srRQuery), rows) || !p.histMode {
		t.Errorf("setText started a search (seq %d → %d) or changed the rows %q", seq, p.seq, srTitles(p.rows, srRQuery))
	}
	if g := p.ghostText(); g != " — Search" {
		t.Errorf("ghost %q, want \" — Search\" after the shown query", g)
	}

	// the other one: its in: ref is a token in the box
	d.press(key.NameDownArrow)
	r = p.rows[p.sel]
	if r.kind != srRQuery {
		t.Fatalf("second ↓ on %+v", r)
	}
	want, toks := srDisplayQuery(e.h, r.id)
	if p.ed.Text() != want || len(p.tokens) != len(toks) {
		t.Errorf("box %q with %d tokens, want %q with %d", p.ed.Text(), len(p.tokens), want, len(toks))
	}
	if strings.Contains(r.id, "<#") && len(p.tokens) != 1 {
		t.Errorf("the in: ref isn't a token: %d tokens", len(p.tokens))
	}

	// past the end: nothing selected, the user's (empty) query back
	d.press(key.NameDownArrow)
	if p.picked || p.ed.Text() != "" || p.uqOn || len(p.tokens) != 0 {
		t.Errorf("past the end: picked %v box %q shown %v tokens %d", p.picked, p.ed.Text(), p.uqOn, len(p.tokens))
	}
	// ↑ from nothing picks the last
	d.press(key.NameUpArrow)
	if p.rows[p.sel].kind != srRQuery || p.ed.Text() != SearchDisplayText(e.h, p.rows[p.sel].id) {
		t.Errorf("↑ box %q on %+v", p.ed.Text(), p.rows[p.sel])
	}

	// the pointer (onMouseMove: setText false) picks without the text
	d.press(key.NameDownArrow) // none again
	first := srIndexOf(p, srRQuery, rows[0])
	p.pick(first)
	d.frame()
	if p.ed.Text() != "" {
		t.Errorf("a pointer pick changed the box to %q", p.ed.Text())
	}

	// the caret keys: nothing selected, the shown text stays
	p.unpick()
	d.press(key.NameDownArrow)
	shown := p.ed.Text()
	d.press(key.NameLeftArrow)
	if p.picked || p.ed.Text() != shown {
		t.Errorf("← picked %v box %q, want nothing picked and %q kept", p.picked, p.ed.Text(), shown)
	}

	// typing makes what the box shows the user's query: a new search
	d.press(key.NameDownArrow) // the first again
	srType(d, p, " x")
	if p.uqOn || p.userQuery() != rows[0]+" x" || p.seq == seq {
		t.Errorf("after typing: shown %v user query %q seq %d", p.uqOn, p.userQuery(), p.seq)
	}

	// Enter on a shown recent search runs it
	p = NewSearchPopover(e.h, "", image.Rectangle{})
	d = &srDrv{size: image.Pt(1280, 800), w: p}
	d.until(t, "the snapshot again", func() bool { return p.snapOK })
	p.history.Click()
	d.until(t, "the history rows again", func() bool { return len(srTitles(p.rows, srRQuery)) == 2 })
	d.press(key.NameDownArrow)
	q := p.rows[p.sel].id
	d.press(key.NameReturn)
	if n := len(e.h.navs); n == 0 || e.h.navs[n-1] != "search?q="+url.QueryEscape(q) {
		t.Errorf("Enter navigated %q, want the recent search %q", e.h.navs, q)
	}

	// typing: ↓ onto a conversation keeps the typed text; the "Show
	// results for" row's replacement is the typed text too
	tp := NewSearchPopover(e.h, cn, image.Rectangle{})
	d = &srDrv{size: image.Pt(1280, 800), w: tp}
	d.until(t, "the typed rows", func() bool {
		return tp.snapOK && slices.ContainsFunc(tp.rows, func(r srRow) bool { return r.kind == srRShow })
	})
	for range len(tp.rows) + 1 {
		d.press(key.NameDownArrow)
		if tp.ed.Text() != cn || tp.uqOn {
			t.Fatalf("↓ onto %+v: box %q shown %v, want %q", tp.rows[tp.sel], tp.ed.Text(), tp.uqOn, cn)
		}
	}
}

func TestSearchOfflineNotice(t *testing.T) {
	e := srNewEnv(t, false)
	online := true
	old := srClientOnline
	srClientOnline = func(Host) bool { return online }
	t.Cleanup(func() { srClientOnline = old })

	p := NewSearchPopover(e.h, fakeslack.SearchWord, image.Rectangle{})
	d := &srDrv{size: image.Pt(1280, 800), w: p}
	d.until(t, "suggestions", func() bool { return p.snapOK && len(p.rows) > 0 })
	g := d.gtx()
	if h := p.footerH(g, e.h.Theme(), 649); h != 45 {
		t.Errorf("online footer %d, want 45", h)
	}
	if x := d.scan(); !srHas(x, "Select") || !srHas(x, "Give feedback") || srHas(x, "connection.") {
		t.Errorf("online footer %q", x)
	}

	online = false
	x := d.scan()
	// drawn word by word: "No" "connection." (bold) "Search" … "limited."
	if !srHas(x, "No") || !srHas(x, "connection.") || !srHas(x, "offline,") || !srHas(x, "limited.") {
		t.Errorf("offline footer %q, want the notice", x)
	}
	for _, gone := range []string{"Select", "Give feedback"} {
		if srHas(x, gone) {
			t.Errorf("offline footer still has %q", gone)
		}
	}
	// .offlineNotice__a6x3P in a 649px card: 649 - 32 - 48 = 569 to
	// wrap in, two 16px lines; 12 + 12 padding and the border
	if h := p.footerH(g, e.h.Theme(), 649); h != 1+24+32 {
		t.Errorf("offline footer %d, want 57 (two lines)", h)
	}
	if h := p.footerH(g, e.h.Theme(), 2000); h != 1+24+16 {
		t.Errorf("offline footer in a wide card %d, want 41 (one line)", h)
	}
	// a picked row: still no key hints
	d.press(key.NameDownArrow)
	if x := d.scan(); srHas(x, "Actions") || srHas(x, "Tab") || !srHas(x, "connection.") {
		t.Errorf("offline footer with a pick %q", x)
	}
	// back online
	online = true
	if x := d.scan(); srHas(x, "connection.") || !srHas(x, "Actions") {
		t.Errorf("back online %q", x)
	}
}
