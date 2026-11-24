package composer

import (
	"context"
	"encoding/json"
	"image"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gesture"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Editing links in the composer, through the real composer: a router,
// the composer at the bottom of an 800×600 window, and the overlays it
// opens laid out over it, as the shell does (plusHost). Karim,
// 2026-11-22, on a pasted link's slug in the thread composer: "u can right
// click this shit to reanme it or smth" (#clack 1795376079.711999). In
// Slack (boot-shared, build 83aff18a1847) a right-click on a link opens
// LinkContextMenu and on a slug SlugContextMenu, a click in a link opens
// ComposerEditLinkPopover, and both edit it in ComposerLinkModal.
// internal/fakeslack only, synthetic URLs.

type lkEnv struct {
	t      *testing.T
	app    *core.App
	fs     *fakeslack.Server
	h      *plusHost
	c      *Composer
	r      input.Router
	ops    op.Ops
	now    time.Time
	ptr    time.Duration
	size   image.Point
	ch     string
	items  []popups.MenuItem // the last link or slug menu's rows
	dlg    *crLinkDlg        // the last link dialog
	pop    *linkPopover      // the last edit-link popover
	opened []string          // links opened in the browser
	desk   []popups.MenuItem // the last desktop (spelling) menu's rows
}

const (
	lkURL   = "https://example.com/docs"
	lkBench = "https://github.com/some-du6e/clack/blob/benchmark/docs/BENCHMARK.md"
	// lkBenchLabel is chat.slugifyUrl's display_url for lkBench (four
	// segments at most: fakeslack's slug_display.go)
	lkBenchLabel = "github.com/some-du6e/clack/blob/…/BENCHMARK.md"
	lkRename     = "more information about the benchmark found here"
)

func newLinkEnv(t *testing.T) *lkEnv {
	t.Helper()
	app, fs := fakeslackApp(t)
	stubTyping(t)
	sh := theme.NewShaper()
	th := theme.New(false)
	th.SetShapers(sh, sh)
	e := &lkEnv{t: t, app: app, fs: fs, h: &plusHost{slickHost: &slickHost{app: app, th: th}},
		now: time.Now(), ptr: 10 * time.Second, size: image.Pt(800, 600), ch: fs.ChannelIDs()[0]}
	prevOpen := linkOpenURL
	linkOpenURL = func(u string) { e.opened = append(e.opened, u) }
	linkMenuTap = func(it []popups.MenuItem) { e.items = it }
	linkDialogTap = func(d *crLinkDlg) { e.dlg = d }
	linkPopoverTap = func(p *linkPopover) { e.pop = p }
	t.Cleanup(func() { linkOpenURL, linkMenuTap, linkDialogTap, linkPopoverTap = prevOpen, nil, nil, nil })
	// the desktop app's own menu, drawn into the env's overlays
	prevHost := MenuHost
	MenuHost = e.h
	spellMenuTap = func(it []popups.MenuItem) { e.desk = it }
	t.Cleanup(func() { MenuHost, spellMenuTap = prevHost, nil })
	clearDraft(e.ch, "")
	serverDraftIDs.Delete(draftKey(e.ch, ""))
	t.Cleanup(func() { clearDraft(e.ch, ""); serverDraftIDs.Delete(draftKey(e.ch, "")) })
	e.use(New(e.h, e.ch, ""))
	return e
}

func (e *lkEnv) use(c *Composer) {
	e.t.Cleanup(c.Close)
	e.c, e.h.overlays = c, nil
	e.frame()
	e.frame()
}

// frame lays the composer out at y 400 and the open overlays over the
// window, and moves both clocks 16 ms.
func (e *lkEnv) frame() {
	e.ops.Reset()
	paint.FillShape(&e.ops, e.h.th.C.Bg, clip.Rect{Max: e.size}.Op())
	gtx := layout.Context{Ops: &e.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Source: e.r.Source(), Now: e.now,
		Constraints: layout.Exact(e.size)}
	slackc.Frame(gtx)
	e.c.Origin = image.Pt(0, 400)
	st := op.Offset(e.c.Origin).Push(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(e.size.X, 200)}
	e.c.Layout(g)
	st.Pop()
	live := e.h.overlays[:0]
	for _, o := range e.h.overlays {
		if o.Layout(gtx); !o.Dismissed() {
			live = append(live, o)
		}
	}
	e.h.overlays = live
	e.r.Frame(&e.ops)
	e.now = e.now.Add(16 * time.Millisecond)
	e.ptr += 16 * time.Millisecond
}

// setDoc puts d in the editor, the caret at its end.
func (e *lkEnv) setDoc(d *Doc) {
	e.c.ed.SetDoc(d)
	e.c.ed.SetSelection(d.Len(), d.Len())
	e.items, e.dlg, e.pop = nil, nil, nil
	e.frame()
	e.frame()
}

// edOrigin is the editor's top-left in the window.
func (e *lkEnv) edOrigin() image.Point {
	return e.c.Origin.Add(e.c.ui.InputOffset()).Add(image.Pt(0, e.c.top))
}

// runeAt is the middle of rune i's box in the window.
func (e *lkEnv) runeAt(i int) image.Point {
	e.t.Helper()
	r := e.c.ed.rangeBox(i, i+1)
	if r.Empty() {
		e.t.Fatalf("rune %d has no box", i)
	}
	r = r.Add(e.edOrigin())
	return image.Pt((r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2)
}

func (e *lkEnv) rightClick(p image.Point) {
	NotePointer(p)
	pos := f32.Pt(float32(p.X), float32(p.Y))
	e.items, e.desk = nil, nil
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pos, Time: e.ptr},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonSecondary, Position: pos, Time: e.ptr},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pos, Time: e.ptr + 40*time.Millisecond})
	e.ptr += 40 * time.Millisecond
	e.frame()
	e.frame()
}

func (e *lkEnv) click(p image.Point) {
	pos := f32.Pt(float32(p.X), float32(p.Y))
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pos, Time: e.ptr},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pos, Time: e.ptr},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pos, Time: e.ptr + 40*time.Millisecond})
	e.ptr += gesture.DoubleClickTime() + 100*time.Millisecond // past the double-click window
	e.frame()
	e.frame()
}

// key presses name with mods on the focused editor.
func (e *lkEnv) key(name key.Name, mods key.Modifiers) {
	e.r.Queue(key.Event{Name: name, Modifiers: mods, State: key.Press}, key.Event{Name: name, Modifiers: mods, State: key.Release})
	e.frame()
	e.frame()
}

// rows names the menu's rows: keys, "---" for a separator, "#Label" for a
// header.
func (e *lkEnv) rows() []string {
	var out []string
	for _, it := range e.items {
		switch {
		case it.Separator:
			out = append(out, "---")
		case it.Header != "":
			out = append(out, "#"+it.Header)
		default:
			out = append(out, it.Key)
		}
	}
	return out
}

// do runs the menu row key.
func (e *lkEnv) do(k string) {
	e.t.Helper()
	for _, it := range e.items {
		if it.Key != k || it.Separator || it.Header != "" {
			continue
		}
		// a click on the row: the menu closes, then the row acts
		for _, o := range e.h.overlays {
			if m, ok := o.(*popups.Menu); ok {
				m.Dismiss()
			}
		}
		switch {
		case it.DoCtx != nil:
			it.DoCtx(layout.Context{Ops: new(op.Ops), Source: e.r.Source(), Now: e.now})
		case it.Do != nil:
			it.Do()
		}
		e.frame()
		e.frame()
		return
	}
	e.t.Fatalf("no row %q in %v", k, e.rows())
}

// fill types into the dialog's fields (nil: leave it) and lays a frame
// out, as onChange sees each edit.
func (e *lkEnv) fill(text, link *string) {
	e.t.Helper()
	if e.dlg == nil {
		e.t.Fatal("no link dialog")
	}
	if text != nil {
		e.dlg.text.SetText(*text)
	}
	if link != nil {
		e.dlg.link.SetText(*link)
	}
	e.frame()
	e.dlg.noteEdits() // onChange
}

// save clicks the dialog's Save.
func (e *lkEnv) save() {
	e.t.Helper()
	d := e.dlg
	if !d.canSave() {
		e.t.Fatalf("Save is disabled (text %q, link %q)", d.text.Text(), d.link.Text())
	}
	d.save()
	d.modal.Dismiss()
	e.frame()
	e.frame()
}

func sp(s string) *string { return &s }

// linkRuns describes d's links: "text→url" for each run of one URL, "[slug
// label→url]" for a link slug.
func linkRuns(d *Doc) []string {
	var out []string
	for i := 0; i < d.Len(); {
		a := d.Attrs[i]
		switch {
		case a.Atom != nil && a.Atom.Kind == AtomLink:
			out = append(out, "["+a.Atom.Text+"→"+a.Atom.URL+"]")
			i++
		case a.Link != "":
			j := i
			for j < d.Len() && d.Attrs[j].Link == a.Link {
				j++
			}
			out = append(out, d.String(i, j)+"→"+a.Link)
			i = j
		default:
			i++
		}
	}
	return out
}

// lkDoc is "see the docs now", "the docs" linked to lkURL (runes 4-12).
func lkDoc() *Doc {
	d := NewDoc()
	a := d.Insert(0, "see ", Attr{})
	b := d.Insert(a, "the docs", Attr{})
	d.SetLink(a, b, lkURL)
	d.Insert(b, " now", Attr{})
	return d
}

// A right-click on a link opens LinkContextMenu: Copy link, Edit link,
// "Display as" URL, Remove link; Edit link renames it, changes its URL
// (normalizeLinkUrl), and undo and redo walk the edits back and forth;
// Remove link and Display as URL do what they say; Copy link copies the
// URL. The desktop app's own menu doesn't come (preventDefault).
func TestLinkMenuEditsALink(t *testing.T) {
	e := newLinkEnv(t)
	e.setDoc(lkDoc())
	e.rightClick(e.runeAt(6))
	if want := []string{"copy", "edit-link", "---", "#Display as", "url", "---", "remove-link"}; !slices.Equal(e.rows(), want) {
		t.Fatalf("link menu %v, want %v", e.rows(), want)
	}
	for _, it := range e.items {
		if it.Key == "remove-link" && (!it.Danger || it.Label != "Remove link") || it.Key == "edit-link" && it.Label != "Edit link" || it.Key == "copy" && it.Label != "Copy link" {
			t.Errorf("row %q: %q, danger %v", it.Key, it.Label, it.Danger)
		}
	}
	if len(e.h.overlays) != 1 || e.h.menus != 1 || e.desk != nil {
		t.Fatalf("%d overlays (%d menus), desktop rows %d: want the link menu alone", len(e.h.overlays), e.h.menus, len(e.desk))
	}

	// rename
	e.do("edit-link")
	if e.dlg == nil {
		t.Fatal("Edit link opened no dialog")
	}
	if e.dlg.text.Text() != "the docs" || e.dlg.link.Text() != lkURL || e.dlg.canSave() {
		t.Fatalf("dialog text %q link %q, Save enabled %v; want the link's, Save off until an edit", e.dlg.text.Text(), e.dlg.link.Text(), e.dlg.canSave())
	}
	if lo, hi := e.c.ed.Selection(); lo != 4 || hi != 12 {
		t.Errorf("selection %d-%d, want the link (4-12)", lo, hi)
	}
	e.fill(sp("our docs"), nil)
	e.save()
	d := e.c.ed.Doc()
	if d.Text() != "see our docs now" || !slices.Equal(linkRuns(d), []string{"our docs→" + lkURL}) {
		t.Fatalf("after the rename: %q %v", d.Text(), linkRuns(d))
	}
	if lo, hi := e.c.ed.Selection(); lo != 12 || hi != 12 {
		t.Errorf("caret %d-%d, want after the link (12)", lo, hi)
	}
	e.c.ed.Undo()
	if d := e.c.ed.Doc(); d.Text() != "see the docs now" || !slices.Equal(linkRuns(d), []string{"the docs→" + lkURL}) {
		t.Errorf("undo: %q %v", d.Text(), linkRuns(d))
	}
	e.c.ed.Redo()
	if d := e.c.ed.Doc(); d.Text() != "see our docs now" {
		t.Errorf("redo: %q", d.Text())
	}

	// change the URL: the text stays, linked in place
	e.frame()
	e.rightClick(e.runeAt(6))
	e.do("edit-link")
	e.fill(nil, sp("example.org/guide"))
	e.save()
	if d := e.c.ed.Doc(); d.Text() != "see our docs now" || !slices.Equal(linkRuns(d), []string{"our docs→http://example.org/guide"}) {
		t.Fatalf("after the URL change: %q %v", d.Text(), linkRuns(d))
	}

	// Copy link
	e.rightClick(e.runeAt(6))
	for _, it := range e.items {
		if it.Key == "copy" && it.DoCtx != nil {
			it.DoCtx(layout.Context{Ops: new(op.Ops), Source: e.r.Source(), Now: e.now})
		}
	}
	for _, o := range e.h.overlays {
		if m, ok := o.(*popups.Menu); ok {
			m.Dismiss()
		}
	}
	if _, b, ok := e.r.WriteClipboard(); !ok || string(b) != "http://example.org/guide" {
		t.Errorf("Copy link put %q (%v) on the clipboard", b, ok)
	}
	e.frame() // the menu's scrim gone from the window

	// Remove link
	e.rightClick(e.runeAt(6))
	e.do("remove-link")
	if d := e.c.ed.Doc(); d.Text() != "see our docs now" || len(linkRuns(d)) != 0 {
		t.Fatalf("after Remove link: %q %v", d.Text(), linkRuns(d))
	}
	e.c.ed.Undo()
	if d := e.c.ed.Doc(); len(linkRuns(d)) != 1 {
		t.Errorf("undo of Remove link: %v", linkRuns(d))
	}

	// Display as URL: the URL, linked; one undo step back
	e.setDoc(lkDoc())
	e.rightClick(e.runeAt(6))
	e.do("url")
	if d := e.c.ed.Doc(); d.Text() != "see "+lkURL+" now" || !slices.Equal(linkRuns(d), []string{lkURL + "→" + lkURL}) {
		t.Fatalf("after Display as URL: %q %v", d.Text(), linkRuns(d))
	}
	e.c.ed.Undo()
	if d := e.c.ed.Doc(); d.Text() != "see the docs now" {
		t.Errorf("undo of Display as URL: %q", d.Text())
	}
	// a link whose text is its URL has no Display as
	bare := NewDoc()
	end := bare.Insert(0, lkURL, Attr{})
	bare.SetLink(0, end, lkURL)
	e.setDoc(bare)
	e.rightClick(e.runeAt(6))
	if want := []string{"copy", "edit-link", "---", "remove-link"}; !slices.Equal(e.rows(), want) {
		t.Errorf("a bare URL's menu %v, want %v", e.rows(), want)
	}
}

// Links in a quote and in a list are edited where they are (the line
// keeps its kind); a link in inline code is <a><code>, whose right-click
// targets the <code>: Texty opens no menu for it.
func TestLinkMenuInQuoteListAndCode(t *testing.T) {
	e := newLinkEnv(t)
	d := NewDoc()
	end := d.Insert(0, "a ", Attr{})
	q := d.Insert(end, "quoted", Attr{})
	d.SetLink(end, q, lkURL)
	end = d.Insert(q, "\nb ", Attr{})
	l := d.Insert(end, "listed", Attr{})
	d.SetLink(end, l, "https://example.com/list")
	end = d.Insert(l, "\nc ", Attr{})
	cd := d.Insert(end, "coded", Attr{Style: Code})
	d.SetLink(end, cd, "https://example.com/code")
	d.Lines[0].Kind, d.Lines[1].Kind = Quote, Bullet
	e.setDoc(d)

	e.rightClick(e.runeAt(3))
	e.do("edit-link")
	e.fill(sp("QUOTED"), nil)
	e.save()
	got := e.c.ed.Doc()
	if got.Lines[0].Kind != Quote || !strings.HasPrefix(got.Text(), "a QUOTED\n") || linkRuns(got)[0] != "QUOTED→"+lkURL {
		t.Fatalf("quote: %q kind %v %v", got.Text(), got.Lines[0].Kind, linkRuns(got))
	}
	e.rightClick(e.runeAt(12))
	e.do("remove-link")
	got = e.c.ed.Doc()
	if got.Lines[1].Kind != Bullet || len(linkRuns(got)) != 2 {
		t.Fatalf("list: kind %v, links %v", got.Lines[1].Kind, linkRuns(got))
	}
	e.rightClick(e.runeAt(got.Len() - 2))
	if e.items != nil {
		t.Errorf("a code link's right-click opened Texty's menu %v", e.rows())
	}
	if !slices.ContainsFunc(e.desk, func(it popups.MenuItem) bool { return it.Key == "cut" }) {
		t.Errorf("a code link's right-click: desktop rows %d, want the desktop menu (Cut, Copy, Paste)", len(e.desk))
	}
}

// A click in a link opens ComposerEditLinkPopover over it, the link
// selected: Remove takes the link off (the caret where the click left it),
// Edit opens the dialog, Esc puts the click's caret back. A click within
// 5px of a link's end only places the caret. A long bare URL offers
// "Shorten link" (truncateUrl).
func TestLinkPopover(t *testing.T) {
	e := newLinkEnv(t)
	e.setDoc(lkDoc())
	e.click(e.runeAt(7))
	p := e.pop
	if p == nil {
		t.Fatal("the click opened no popover")
	}
	if p.text != "the docs" || p.url != lkURL || p.lo != 4 || p.hi != 12 || p.tertiary() != "" {
		t.Fatalf("popover %q %q %d-%d tertiary %q", p.text, p.url, p.lo, p.hi, p.tertiary())
	}
	if lo, hi := e.c.ed.Selection(); lo != 4 || hi != 12 {
		t.Errorf("selection %d-%d, want the link", lo, hi)
	}
	a := e.c.ed.rangeBox(4, 12).Add(e.edOrigin())
	if r := p.Rect(); r.Empty() || r.Max.Y > a.Min.Y || r.Dx() != 400 {
		t.Errorf("popover card %v over the link %v: want a 400px card above it", r, a)
	}
	caret := p.initHi
	p.onRemove()
	e.frame()
	if d := e.c.ed.Doc(); len(linkRuns(d)) != 0 || d.Text() != "see the docs now" {
		t.Fatalf("after Remove: %q %v", d.Text(), linkRuns(d))
	}
	if lo, hi := e.c.ed.Selection(); lo != caret || hi != caret {
		t.Errorf("caret %d-%d, want where the click left it (%d)", lo, hi, caret)
	}

	e.setDoc(lkDoc())
	e.click(e.runeAt(7))
	e.pop.onEdit()
	e.frame()
	if e.dlg == nil || e.dlg.text.Text() != "the docs" || e.dlg.link.Text() != lkURL {
		t.Fatalf("Edit: dialog %+v", e.dlg)
	}
	e.fill(nil, sp("https://example.com/new"))
	e.save()
	if d := e.c.ed.Doc(); !slices.Equal(linkRuns(d), []string{"the docs→https://example.com/new"}) {
		t.Fatalf("after Edit: %v", linkRuns(d))
	}

	e.setDoc(lkDoc())
	e.click(e.runeAt(7))
	at0 := e.pop.initHi
	e.pop.Dismiss() // Esc
	e.frame()
	if lo, hi := e.c.ed.Selection(); lo != at0 || hi != at0 {
		t.Errorf("Esc: selection %d-%d, want the click's caret %d", lo, hi, at0)
	}

	// near the link's start: the caret only
	e.pop = nil
	r := e.c.ed.rangeBox(4, 12).Add(e.edOrigin())
	e.click(image.Pt(r.Min.X+2, (r.Min.Y+r.Max.Y)/2))
	if e.pop != nil {
		t.Error("a click 2px into the link opened the popover")
	}

	// a long bare URL: Shorten link swaps in truncateUrl's form, linked
	long := "https://example.com/" + strings.Repeat("abcdefghij", 18)
	d := NewDoc()
	end := d.Insert(0, long, Attr{})
	d.SetLink(0, end, long)
	e.setDoc(d)
	e.click(e.runeAt(30))
	if e.pop == nil || e.pop.tertiary() != "Shorten link" {
		t.Fatalf("long URL popover %+v", e.pop)
	}
	e.pop.onToggleTruncation()
	short := truncateURL(long)
	if got := e.c.ed.Doc(); short == long || got.Text() != short || !slices.Equal(linkRuns(got), []string{short + "→" + long}) {
		t.Errorf("after Shorten link: %q %v (truncateUrl %q)", got.Text(), linkRuns(got), short)
	}
}

// Ctrl+Shift+U on a selection opens the dialog over it, trimmed of the
// punctuation and blanks around it (getTrimLength); Save links it. With
// the caret in a link it opens the edit-link popover instead.
func TestLinkShortcutOnSelection(t *testing.T) {
	e := newLinkEnv(t)
	d := NewDoc()
	d.Insert(0, "read  docs. now", Attr{})
	e.setDoc(d)
	e.click(e.runeAt(1))       // the editor focused
	e.c.ed.SetSelection(5, 11) // " docs."
	e.key("U", key.ModShortcut|key.ModShift)
	if e.dlg == nil {
		t.Fatal("Ctrl+Shift+U opened no dialog")
	}
	if e.dlg.text.Text() != "docs" || e.dlg.link.Text() != "" || e.dlg.canSave() {
		t.Fatalf("dialog text %q link %q, Save %v", e.dlg.text.Text(), e.dlg.link.Text(), e.dlg.canSave())
	}
	e.fill(nil, sp("example.com/docs"))
	e.save()
	if got := e.c.ed.Doc(); got.Text() != "read  docs. now" || !slices.Equal(linkRuns(got), []string{"docs→http://example.com/docs"}) {
		t.Fatalf("after Save: %q %v", got.Text(), linkRuns(got))
	}

	e.setDoc(lkDoc())
	e.click(e.runeAt(1))
	e.c.ed.SetSelection(7, 7)
	e.dlg, e.pop = nil, nil
	e.key("U", key.ModShortcut|key.ModShift)
	if e.dlg != nil || e.pop == nil || e.pop.lo != 4 || e.pop.hi != 12 {
		t.Fatalf("caret in a link: dialog %v, popover %+v; want the popover over the link", e.dlg != nil, e.pop)
	}
	if lo, hi := e.c.ed.Selection(); lo != 7 || hi != 7 {
		t.Errorf("selection %d-%d, want the caret left where it was", lo, hi)
	}
}

// A URL pasted over selected text links that text (acceptSingleUrl).
func TestLinkPasteOverSelection(t *testing.T) {
	e := newLinkEnv(t)
	d := NewDoc()
	d.Insert(0, "see the docs", Attr{})
	e.setDoc(d)
	e.c.ed.SetSelection(4, 12)
	e.c.PasteText(lkURL)
	e.frame()
	if got := e.c.ed.Doc(); got.Text() != "see the docs" || !slices.Equal(linkRuns(got), []string{"the docs→" + lkURL}) {
		t.Fatalf("after the paste: %q %v", got.Text(), linkRuns(got))
	}
}

// Karim's slug: a pasted link drawn as a link slug. Its right-click opens
// SlugContextMenu (Copy link, Edit link, Open in browser, "Display as"
// URL); Edit link starts with an empty Text and the URL, and the text
// typed there makes it a link with that text, sent as Slack sends it
// (fakeslack's chat.postMessage): the rich_text link element with the
// text, and <url|text> in text. Left with no text it turns back into the
// slug (chat.slugifyUrl).
func TestLinkSlugRenameSends(t *testing.T) {
	e := newLinkEnv(t)
	posts := make(chan url.Values, 4)
	e.fs.OnPost = func(f url.Values, _ map[string]any) { posts <- f }
	slugDoc := func() *Doc {
		d := NewDoc()
		end := d.Insert(0, "see ", Attr{})
		d.InsertAtom(end, linkAtom(lkBench, lkBenchLabel))
		return d
	}
	e.setDoc(slugDoc())
	e.rightClick(e.runeAt(4))
	if want := []string{"copy", "edit-link", "---", "open-in-browser", "---", "#Display as", "url"}; !slices.Equal(e.rows(), want) {
		t.Fatalf("slug menu %v, want %v", e.rows(), want)
	}
	e.do("open-in-browser")
	if !slices.Equal(e.opened, []string{lkBench}) {
		t.Errorf("Open in browser opened %v", e.opened)
	}
	e.rightClick(e.runeAt(4))
	e.do("edit-link")
	if e.dlg == nil || e.dlg.text.Text() != "" || e.dlg.link.Text() != lkBench {
		t.Fatalf("slug dialog: %+v", e.dlg)
	}
	e.fill(sp(lkRename), nil)
	e.save()
	d := e.c.ed.Doc()
	if d.Text() != "see "+lkRename || !slices.Equal(linkRuns(d), []string{lkRename + "→" + lkBench}) {
		t.Fatalf("after the rename: %q %v", d.Text(), linkRuns(d))
	}
	e.c.submit()
	select {
	case f := <-posts:
		el := `{"type":"link","url":"` + lkBench + `","text":"` + lkRename + `"}`
		if b := f.Get("blocks"); !strings.Contains(b, el) {
			t.Errorf("blocks %s\nwant the element %s", b, el)
		}
		if got, want := f.Get("text"), "see <"+lkBench+"|"+lkRename+">"; got != want {
			t.Errorf("text %q, want %q", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no chat.postMessage")
	}

	// no text: the URL again, which turns back into the slug
	e.setDoc(slugDoc())
	e.rightClick(e.runeAt(4))
	e.do("edit-link")
	e.fill(nil, sp(lkBench+" "))
	e.save()
	waitUntil(t, "the slug back", func() bool {
		e.frame()
		runRestores()
		d := e.c.ed.Doc()
		return d.Len() == 5 && d.Attrs[4].Atom != nil && d.Attrs[4].Atom.Kind == AtomLink && d.Attrs[4].Atom.URL == lkBench
	})
}

// The renamed link survives the draft's trip to Slack and back: the
// drafts.create blocks, restored as another device's draft into another
// conversation's composer, hold the link with its text.
func TestLinkRenameDraftRoundTrip(t *testing.T) {
	e := newLinkEnv(t)
	var mu sync.Mutex
	var saved string
	e.fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "drafts.create" || method == "drafts.update" {
			mu.Lock()
			saved = f.Get("blocks")
			mu.Unlock()
		}
		return nil, false
	})
	blocksNow := func() string { mu.Lock(); defer mu.Unlock(); return saved }
	var blocks string
	t.Cleanup(func() { e.fs.SetHook(nil) })
	d := NewDoc()
	end := d.Insert(0, "see ", Attr{})
	d.InsertAtom(end, linkAtom(lkBench, lkBenchLabel))
	e.setDoc(d)
	e.rightClick(e.runeAt(4))
	e.do("edit-link")
	e.fill(sp(lkRename), nil)
	e.save()
	e.c.syncDraftNow()
	waitUntil(t, "drafts.create", func() bool { e.frame(); blocks = blocksNow(); return blocks != "" })
	if !strings.Contains(blocks, `"text":"`+lkRename+`"`) {
		t.Fatalf("draft blocks %s", blocks)
	}
	ch2 := e.fs.ChannelIDs()[1]
	clearDraft(ch2, "")
	serverDraftIDs.Delete(draftKey(ch2, ""))
	c2 := New(e.h, ch2, "")
	t.Cleanup(func() { c2.Close(); clearDraft(ch2, ""); serverDraftIDs.Delete(draftKey(ch2, "")) })
	ops := new(op.Ops)
	frame2 := func() {
		ops.Reset()
		c2.Layout(layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Constraints{Max: image.Pt(800, 200)}, Now: time.Now()})
	}
	frame2()
	raw, _ := json.Marshal(map[string]any{"type": "draft_create", "draft": map[string]any{
		"id": "Dr0LINKTRIP", "last_updated_ts": "1700000000000",
		"destinations": []any{map[string]any{"channel_id": ch2}}, "blocks": json.RawMessage(blocks)}})
	e.app.Inject(slack.Event{Type: "draft_create", Raw: raw})
	waitUntil(t, "the draft restored", func() bool { frame2(); return c2.ed.Doc().Len() > 0 })
	if got := c2.ed.Doc(); got.Text() != "see "+lkRename || !slices.Equal(linkRuns(got), []string{lkRename + "→" + lkBench}) {
		t.Errorf("restored draft %q %v", got.Text(), linkRuns(got))
	}
}

// Editing a sent message: its renamed link comes back as a link with its
// text (not a slug), and renaming it again in the edit composer saves the
// new text.
func TestLinkRenameInMessageEdit(t *testing.T) {
	e := newLinkEnv(t)
	blocks := `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[` +
		`{"type":"text","text":"see "},{"type":"link","url":"` + lkBench + `","text":"` + lkRename + `"}]}]}]`
	m := core.Message{Channel: e.ch, TS: "1700000100.000100", User: "U0SELF", Text: "see <" + lkBench + "|" + lkRename + ">",
		Blocks: json.RawMessage(blocks)}
	m.Doc.Blocks = []richtext.Block{{Kind: richtext.Paragraph, Spans: []richtext.Span{
		{Kind: richtext.Text, Text: "see "}, {Kind: richtext.Link, Text: lkRename, URL: lkBench}}}}
	saved := make(chan [2]string, 2)
	c := NewEdit(e.h, m, func() {})
	c.SaveEdit = func(_ context.Context, _, _, text string, b json.RawMessage, _ []string) error {
		saved <- [2]string{text, string(b)}
		return nil
	}
	e.use(c)
	if d := c.ed.Doc(); d.Text() != "see "+lkRename || !slices.Equal(linkRuns(d), []string{lkRename + "→" + lkBench}) {
		t.Fatalf("the edit opens with %q %v", d.Text(), linkRuns(d))
	}
	e.rightClick(e.runeAt(6))
	e.do("edit-link")
	e.fill(sp("the benchmark"), nil)
	e.save()
	c.submit()
	select {
	case s := <-saved:
		el := `{"type":"link","url":"` + lkBench + `","text":"the benchmark"}`
		if !strings.Contains(s[1], el) || s[0] != "see <"+lkBench+"|the benchmark>" {
			t.Errorf("saved %q\n%s\nwant the element %s", s[0], s[1], el)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Save didn't save")
	}
}

// Remove link on a link whose text is its URL keeps it unlinked (Texty's
// {link: false, unlink: true}): the send carries the URL as text, and a
// space typed after it doesn't link it again.
func TestLinkRemoveStaysUnlinked(t *testing.T) {
	e := newLinkEnv(t)
	posts := make(chan url.Values, 2)
	e.fs.OnPost = func(f url.Values, _ map[string]any) { posts <- f }
	d := NewDoc()
	a := d.Insert(0, "see ", Attr{})
	b := d.Insert(a, lkURL, Attr{})
	d.SetLink(a, b, lkURL)
	e.setDoc(d)
	e.rightClick(e.runeAt(6))
	e.do("remove-link")
	e.c.ed.SetSelection(b, b)
	e.typeOver(" ")
	if got := e.c.ed.Doc(); len(linkRuns(got)) != 0 {
		t.Fatalf("after Remove link and a space: links %v", linkRuns(got))
	}
	e.c.submit()
	select {
	case f := <-posts:
		bl := f.Get("blocks")
		if strings.Contains(bl, `"type":"link"`) || !strings.Contains(bl, `{"type":"text","text":"see `+lkURL+` "}`) {
			t.Errorf("blocks %s, want the URL as text", bl)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no chat.postMessage")
	}
}

// The dialog through real input: Ctrl+Shift+U on a selection focuses Link
// (the text is set), the URL typed there and Enter save it; on an existing
// link, emptying Link and clicking Save takes the link off, and a new text
// with Link emptied leaves that text, plain (onSubmit's insertPlainText).
// Enter in Text saves too (jRNC: the form submits), it doesn't move to Link.
func TestLinkDialogRealInput(t *testing.T) {
	e := newLinkEnv(t)
	d := NewDoc()
	d.Insert(0, "read docs now", Attr{})
	e.setDoc(d)
	e.click(e.runeAt(1))
	e.c.ed.SetSelection(5, 9)
	e.key("U", key.ModShortcut|key.ModShift)
	if e.dlg == nil {
		t.Fatal("no dialog")
	}
	e.frame()
	field := func(ed *widget.Editor, s string) { // s typed over all of the focused field
		e.r.Queue(key.EditEvent{Range: key.Range{Start: 0, End: ed.Len()}, Text: s})
		e.frame()
		e.frame()
	}
	field(&e.dlg.link, "example.com/docs")
	if e.dlg.link.Text() != "example.com/docs" || !e.dlg.canSave() {
		t.Fatalf("typed into Link: %q, Save %v", e.dlg.link.Text(), e.dlg.canSave())
	}
	e.key(key.NameReturn, 0)
	if got := e.c.ed.Doc(); !slices.Equal(linkRuns(got), []string{"docs→http://example.com/docs"}) {
		t.Fatalf("after Enter: %q %v", got.Text(), linkRuns(got))
	}

	// emptied Link, Save clicked: the link comes off, the text stays
	e.setDoc(lkDoc())
	e.rightClick(e.runeAt(6))
	e.do("edit-link")
	e.frame()
	field(&e.dlg.link, "")
	r := e.dlg.saveBtn.Rect()
	if r.Empty() || !e.dlg.canSave() {
		t.Fatalf("Save %v at %v", e.dlg.canSave(), r)
	}
	e.click(r.Min.Add(r.Size().Div(2)))
	if got := e.c.ed.Doc(); got.Text() != "see the docs now" || len(linkRuns(got)) != 0 {
		t.Fatalf("emptied Link, Save: %q %v", got.Text(), linkRuns(got))
	}

	// a new text with Link emptied: that text, plain
	e.setDoc(lkDoc())
	e.rightClick(e.runeAt(6))
	e.do("edit-link")
	e.frame()
	field(&e.dlg.link, "")
	e.key(key.NameTab, key.ModShift) // back to Text
	field(&e.dlg.text, "our docs")
	e.key(key.NameReturn, 0)
	if got := e.c.ed.Doc(); got.Text() != "see our docs now" || len(linkRuns(got)) != 0 {
		t.Fatalf("new text, emptied Link: %q %v", got.Text(), linkRuns(got))
	}
}

// The popover's buttons clicked: Edit opens the dialog over the link,
// the close button closes it with the click's caret back.
func TestLinkPopoverClicks(t *testing.T) {
	e := newLinkEnv(t)
	e.setDoc(lkDoc())
	e.click(e.runeAt(7))
	p := e.pop
	if p == nil || p.rEdit.Empty() {
		t.Fatalf("popover %+v", p)
	}
	e.click(p.rEdit.Min.Add(p.rEdit.Size().Div(2)))
	if !p.Dismissed() || e.dlg == nil || e.dlg.text.Text() != "the docs" {
		t.Fatalf("Edit clicked: popover open %v, dialog %+v", !p.Dismissed(), e.dlg)
	}
	e.dlg.modal.Dismiss()
	e.frame()

	e.setDoc(lkDoc())
	e.click(e.runeAt(7))
	p = e.pop
	at := p.initHi
	e.click(p.rClose.Min.Add(p.rClose.Size().Div(2)))
	if !p.Dismissed() {
		t.Fatal("the close button left the popover open")
	}
	if lo, hi := e.c.ed.Selection(); lo != at || hi != at {
		t.Errorf("close: selection %d-%d, want %d", lo, hi, at)
	}
}

// TestLinkDialogIsDeprecatedDialog: ComposerLinkModal is a _FormDialog,
// FlDO's DeprecatedDialog (boot-shared qph5): c-dialog's 68px header,
// its close button at top 16px, right 16px (align-self flex-start,
// margin-right -8px in the 24px padding), not c-sk-modal's 20px.
func TestLinkDialogIsDeprecatedDialog(t *testing.T) {
	e := newLinkEnv(t)
	e.setDoc(lkDoc())
	e.rightClick(e.runeAt(6))
	e.do("edit-link")
	e.frame()
	if e.dlg == nil {
		t.Fatal("Edit link opened no dialog")
	}
	e.frame()
	m := e.dlg.modal
	w := m.Rect().Dx()
	if bar := m.TitleBar().Dy(); bar != 68 {
		t.Errorf("the header is %dpx, want c-dialog's 68px", bar)
	}
	if cr := m.CloseRect(); cr.Min.Y != 16 || cr.Max.X != w-16 || cr.Dx() != 36 {
		t.Errorf("the close button is at %v in a %dpx card, want 36px at top 16px, right 16px", cr, w)
	}
}
