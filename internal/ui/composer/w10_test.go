package composer

import (
	"image"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/ctxclick"
	"github.com/some-du6e/clack/internal/ui/nativemenu"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// gap-w10 (docs/GAPS-2026-11-11.md W10, docs/COMPOSER-AUDIT.md), ported
// from Slack's CDN build d3de3a487b1c: boot-shared KT9Q (the keyboard
// module, SlackMarkdown), EkaK/XmNU (the markdown patterns), WOwF
// (TextyAutocompleteMenu), NTqE/3D1w (the link dialog), 352G
// (ThreadFooter), client-threads-view (onDownArrowInEmptyInput), and
// Electron's Menu.popup for the spellcheck menu.

// TestW10WideMarkers: SlackMarkdown's buildLocalMarkdownChangeDelta with
// XmNU's full-width markers ＊ (H_), ＿ (QX) and ｀ (j5).
func TestW10WideMarkers(t *testing.T) {
	cases := []struct {
		typed, want string
		st          Style
		lo, hi      int // the styled runes
	}{
		{"＊太字＊", "太字", Bold, 0, 2},
		{"これは＊太字＊", "これは太字", Bold, 3, 5},
		{"＿斜体＿", "斜体", Italic, 0, 2},
		{"a ＿it＿", "a it", Italic, 2, 4},
		{"｀コード｀", "コード", Code, 0, 3},
		{"run ｀ls｀", "run ls", Code, 4, 6},
	}
	for _, c := range cases {
		e := NewEditor()
		edType(e, c.typed)
		d := e.Doc()
		if got := d.Text(); got != c.want {
			t.Errorf("%q: text %q, want %q", c.typed, got, c.want)
			continue
		}
		if !d.HasStyle(c.lo, c.hi, c.st) || c.lo > 0 && d.HasStyle(0, c.lo, c.st) {
			t.Errorf("%q: styles %v, want %v on [%d,%d)", c.typed, edStyles(d), c.st, c.lo, c.hi)
		}
		if _, car := e.Selection(); car != c.hi {
			t.Errorf("%q: caret %d, want %d (after the styled text)", c.typed, car, c.hi)
		}
		e.Undo()
		if got := e.Doc().Text(); got != c.typed {
			t.Errorf("%q: undo gave %q, want the markers back", c.typed, got)
		}
	}
	// No pair, an empty pair, a space inside, a code block line: nothing.
	for _, s := range []string{"＊", "＊＊", "＊ x＊", "｀｀"} {
		e := NewEditor()
		edType(e, s)
		if got := e.Doc().Text(); got != s {
			t.Errorf("%q became %q", s, got)
		}
	}
	e := NewEditor()
	edType(e, "```")
	edType(e, "＊x＊")
	if got := e.Doc().Text(); got != "＊x＊" || e.Doc().Lines[0].Kind != CodeBlock {
		t.Errorf("in a code block: %q %+v", got, e.Doc().Lines)
	}
	// ｀｀｀ is a fence like ``` (buildLocalCodeBlockMarkdownChangeDelta)
	e = NewEditor()
	edType(e, "｀｀｀")
	if e.Doc().Len() != 0 || e.Doc().Lines[0].Kind != CodeBlock {
		t.Errorf("｀｀｀: %q %+v", e.Doc().Text(), e.Doc().Lines)
	}
}

// TestW10FenceEndsCodeLine: "```" typed at the end of a code block line
// (the "remove" action) drops the fence and adds a plain line, with the
// caret on it (Quill's shiftRange through the queue's diff).
func TestW10FenceEndsCodeLine(t *testing.T) {
	e := NewEditor()
	edType(e, "```foo")
	edType(e, "```")
	d := e.Doc()
	wantRunes(t, d, "foo\n")
	if got := lineKinds(d); !sameKinds(got, []BlockKind{CodeBlock, Para}) {
		t.Fatalf("kinds %v", got)
	}
	if _, car := e.Selection(); car != 4 {
		t.Fatalf("caret %d, want 4 (the new plain line)", car)
	}
	edType(e, "after")
	if d := e.Doc(); d.Text() != "foo\nafter" || d.Lines[1].Kind != Para {
		t.Fatalf("typed after: %q %+v", d.Text(), d.Lines)
	}
	// one undo step back to the literal fence
	e.Undo()
	e.Undo()
	if got := e.Doc().Text(); got != "foo```" || e.Doc().Lines[0].Kind != CodeBlock {
		t.Fatalf("undo: %q %+v", got, e.Doc().Lines)
	}
	// a code line with text after the fence keeps it
	e = NewEditor()
	edType(e, "```foobar")
	e.SetSelection(3, 3)
	edType(e, "```")
	if got := e.Doc().Text(); got != "foo```bar" || len(e.Doc().Lines) != 1 {
		t.Fatalf("mid-line fence: %q %+v", got, e.Doc().Lines)
	}
	// in the middle of a block the plain line splits it
	e = NewEditor()
	edType(e, "```a")
	edPress(e, key.NameReturn, key.ModShift)
	edType(e, "b")
	e.SetSelection(1, 1)
	edType(e, "```")
	if got := lineKinds(e.Doc()); e.Doc().Text() != "a\n\nb" || !sameKinds(got, []BlockKind{CodeBlock, Para, CodeBlock}) {
		t.Fatalf("split: %q %v", e.Doc().Text(), got)
	}
}

// TestW10UpOutOfLeadingCodeBlock: "up arrow out of code block" inserts
// " \n" (code-block off) at 0 when the caret is at the start of a message
// that begins with a code block.
func TestW10UpOutOfLeadingCodeBlock(t *testing.T) {
	e := NewEditor()
	edType(e, "```code")
	e.SetSelection(0, 0)
	edPress(e, key.NameUpArrow, 0)
	d := e.Doc()
	wantRunes(t, d, " \ncode")
	if got := lineKinds(d); !sameKinds(got, []BlockKind{Para, CodeBlock}) {
		t.Fatalf("kinds %v", got)
	}
	if lo, hi := e.Selection(); lo != 0 || hi != 0 {
		t.Fatalf("caret [%d,%d), want 0", lo, hi)
	}
	// not mid-line, not on a later block, not with Shift
	for _, c := range []struct {
		name  string
		setup func(e *Editor)
		mods  key.Modifiers
	}{
		{"mid-line", func(e *Editor) { edType(e, "```code"); e.SetSelection(2, 2) }, 0},
		{"second block", func(e *Editor) {
			edType(e, "a")
			edPress(e, key.NameReturn, key.ModShift)
			edType(e, "```code")
			e.SetSelection(2, 2)
		}, 0},
		{"shift", func(e *Editor) { edType(e, "```code"); e.SetSelection(0, 0) }, key.ModShift},
	} {
		e := NewEditor()
		c.setup(e)
		before := e.Doc().Text()
		edPress(e, key.NameUpArrow, c.mods)
		if got := e.Doc().Text(); got != before {
			t.Errorf("%s: %q became %q", c.name, before, got)
		}
	}
}

// TestW10QuotedLists: list autofill on a quote line sets border "1"
// with the list; the send writes rich_text_list border 1, the fallback
// text quotes it, and the blocks read back as a list inside a quote.
func TestW10QuotedLists(t *testing.T) {
	e := NewEditor()
	edType(e, ">")
	edType(e, "- one")
	edPress(e, key.NameReturn, key.ModShift)
	edType(e, "two")
	d := e.Doc()
	wantRunes(t, d, "one\ntwo")
	for i, l := range d.Lines {
		if l.Kind != Bullet || !l.Quoted {
			t.Fatalf("line %d: %+v, want a quoted bullet", i, l)
		}
	}
	raw := string(d.RichTextBlocks())
	if !strings.Contains(raw, `"type":"rich_text_list"`) || !strings.Contains(raw, `"border":1`) {
		t.Fatalf("blocks %s", raw)
	}
	if got := d.Mrkdwn(); got != "> • one\n> • two" {
		t.Errorf("mrkdwn %q", got)
	}
	back, err := DocFromBlocksJSON(d.RichTextBlocks(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Lines) != 2 || !back.Lines[0].Quoted || back.Lines[0].Kind != Bullet {
		t.Fatalf("read back %+v", back.Lines)
	}
	// "1. " too; an empty item's Enter leaves the list (and the quote)
	e = NewEditor()
	edType(e, "＞")
	edType(e, "1. a")
	if l := e.Doc().Lines[0]; l.Kind != Ordered || !l.Quoted {
		t.Fatalf("ordered: %+v", l)
	}
	edPress(e, key.NameReturn, key.ModShift)
	edPress(e, key.NameReturn, 0) // "list empty enter"
	if l := e.Doc().Lines[1]; l != (Line{}) {
		t.Fatalf("after the empty item: %+v, want a plain line", l)
	}
	// a plain list isn't quoted, and ``` on a quote line stays text
	e = NewEditor()
	edType(e, "- x")
	if e.Doc().Lines[0].Quoted {
		t.Fatal("a plain list came out quoted")
	}
	if !strings.Contains(string(e.Doc().RichTextBlocks()), `"border":0`) {
		t.Errorf("plain list blocks %s", e.Doc().RichTextBlocks())
	}
	// Mod+Shift+F's quoted lists (border 1) are quoted lists now
	e = NewEditor()
	e.InsertText("> - item\n> - item2")
	e.ConvertMarkdown()
	for i, l := range e.Doc().Lines {
		if l.Kind != Bullet || !l.Quoted {
			t.Fatalf("converted line %d: %+v", i, l)
		}
	}
}

// TestW10QuotedListLayout: a quoted list's text sits 11px further in
// (padding-left 11px) and the quote bar runs down it and the quote line
// above it as one bar.
func TestW10QuotedListLayout(t *testing.T) {
	sh := theme.NewShaper()
	st := DefaultEditorStyle(sh)
	lay := func(d *Doc) *Editor {
		e := NewEditor()
		e.SetDoc(d)
		gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(600, 300)), Now: time.Now()}
		e.Layout(gtx, st)
		return e
	}
	plain := NewDoc()
	plain.Insert(0, "x", Attr{})
	plain.Lines[0] = Line{Kind: Bullet}
	quoted := NewDoc()
	quoted.Insert(0, "q\nx", Attr{})
	quoted.Lines[0] = Line{Kind: Quote}
	quoted.Lines[1] = Line{Kind: Bullet, Quoted: true}
	pe, qe := lay(plain), lay(quoted)
	if got, want := qe.lay.paras[1].left, pe.lay.paras[0].left+11; got != want {
		t.Errorf("quoted item's text at x=%d, want %d", got, want)
	}
	if !qe.lay.barred(0) || !qe.lay.barred(1) || pe.lay.barred(0) {
		t.Error("the bar should run down the quote and its list, not a plain list")
	}
	// margin 4px above and below the list (collapsing with the quote's)
	q, l := qe.lay.paras[0], qe.lay.paras[1]
	if gap := l.top - q.bot; gap != 4 {
		t.Errorf("quote → quoted list gap %d, want 4", gap)
	}
	if got := qe.lay.height - l.bot; got != 4 {
		t.Errorf("below the list %d, want 4", got)
	}
}

// TestW10DownWhenEmpty: onDownArrowWhenEmpty's binding fires only in an
// empty box, and the composer hands it to the view.
func TestW10DownWhenEmpty(t *testing.T) {
	e := NewEditor()
	edPress(e, key.NameDownArrow, 0)
	if !edHas(e, EvDownEmpty) {
		t.Fatal("no EvDownEmpty in an empty box")
	}
	e = NewEditor()
	edType(e, "x")
	e.events = nil
	edPress(e, key.NameDownArrow, 0)
	if edHas(e, EvDownEmpty) {
		t.Fatal("EvDownEmpty with text")
	}
	e = NewEditor()
	edPress(e, key.NameDownArrow, key.ModShift) // the binding has no modifiers
	if edHas(e, EvDownEmpty) {
		t.Fatal("EvDownEmpty for Shift+Down")
	}
	app, _ := fakeslackApp(t)
	h := &slickHost{app: app, th: theme.New(false)}
	c := New(h, "C0W10DOWN", "1700000000.000100")
	t.Cleanup(c.Close)
	var n atomic.Int32
	c.OnDownWhenEmpty = func() { n.Add(1) }
	c.ed.emit(EvDownEmpty)
	c.handleEditor(layout.Context{Ops: new(op.Ops)})
	if n.Load() != 1 {
		t.Fatalf("OnDownWhenEmpty ran %d times", n.Load())
	}
}

// TestW10PageKeys: ThreadFooter's onPageUp / onPageDown return false, so
// a reply box does nothing with PageUp / PageDown; the channel's and the
// edit composer keep the browser's page motion.
func TestW10PageKeys(t *testing.T) {
	app, _ := fakeslackApp(t)
	h := &slickHost{app: app, th: theme.New(false)}
	thread := New(h, "C0W10PAGE", "1700000000.000200")
	t.Cleanup(thread.Close)
	channel := New(h, "C0W10PAGE", "")
	t.Cleanup(channel.Close)
	if !thread.ed.PageKeysHandled || channel.ed.PageKeysHandled {
		t.Fatalf("PageKeysHandled thread %v channel %v", thread.ed.PageKeysHandled, channel.ed.PageKeysHandled)
	}
	e := NewEditor()
	e.PageKeysHandled = true
	edType(e, "a\nb\nc")
	before := e.caret
	edPress(e, key.NamePageUp, 0)
	edPress(e, key.NamePageDown, 0)
	if e.caret != before || e.anchor != before {
		t.Fatalf("PageUp/PageDown moved the caret in a reply box: %d → %d", before, e.caret)
	}
	edPress(e, key.NamePageUp, key.ModShift) // not the binding's: the browser's
	if e.anchor == e.caret {
		t.Error("Shift+PageUp should still select")
	}
}

// TestW10OfflineHelp: TextyAutocompleteMenu's showOfflineHelp replaces
// the keyboard help while isClientOnline is false, in the / popup too.
func TestW10OfflineHelp(t *testing.T) {
	parts := acOfflineParts()
	var b strings.Builder
	icon, bold := false, ""
	for _, p := range parts {
		if p.Arg == "iconOffline" {
			icon = true
			continue
		}
		if p.Tag == "strong" {
			bold += p.Text
		}
		b.WriteString(p.Text)
	}
	if !icon || bold != "No connection." || b.String() != " No connection. Some results may not be available." {
		t.Fatalf("parts %+v", parts)
	}
	if got := i18n.Plain(parts); !strings.HasPrefix(got, "☁ ") {
		t.Errorf("plain %q", got)
	}
	prev := acClientOnline
	t.Cleanup(func() { acClientOnline = prev })
	for _, online := range []bool{true, false} {
		acClientOnline = func(Host) bool { return online }
		h := &slickHost{th: theme.New(false)}
		ac := newAutocomplete(h, "C1")
		ac.trig = acTrigger{Kind: acCommand}
		ac.active = true
		ac.items = []acItem{{Kind: acCommand, ID: "/remind", Name: "remind", Secondary: "Set a reminder",
			Command: &core.Command{Name: "/remind", Desc: "Set a reminder"}}}
		gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Constraints{Max: image.Pt(800, 600)}, Now: time.Now()}
		dims := ac.Layout(gtx, 0)
		want := 8 + 60 + 8 // the / popup has no keyboard help
		if !online {
			want += 32
		}
		if dims.Size.Y != want {
			t.Errorf("online=%v: popup %dpx tall, want %d", online, dims.Size.Y, want)
		}
	}
}

// TestW10LinkDialogRichText: a selection with an emoji opens the dialog
// with textIsEditable false (the rich preview), and Save links the text
// in place, leaving the emoji.
func TestW10LinkDialogRichText(t *testing.T) {
	app, _ := fakeslackApp(t)
	h := &ovHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	c := New(h, "C0W10LINK", "")
	t.Cleanup(c.Close)
	var dlg *crLinkDlg
	linkDialogTap = func(d *crLinkDlg) { dlg = d }
	t.Cleanup(func() { linkDialogTap = nil })

	d := NewDoc()
	end := d.Insert(0, "see ", Attr{})
	end = d.InsertAtom(end, &Atom{Kind: AtomEmoji, ID: "tada", Text: ":tada:", Uni: "🎉"})
	d.Insert(end, " docs", Attr{})
	c.ed.SetDoc(d)
	c.ed.SetSelection(0, c.ed.Doc().Len())
	c.openLink()
	if dlg == nil || dlg.preview == nil {
		t.Fatalf("dialog %+v: want the rich preview", dlg)
	}
	if p := dlg.props(); p["textIsEditable"] != false {
		t.Errorf("textIsEditable %v", p["textIsEditable"])
	}
	layOverlay(h.last())
	dlg.link.SetText("example.com/docs")
	dlg.save()
	got := c.ed.Doc()
	if got.Len() != 10 || got.Attrs[4].Atom == nil {
		t.Fatalf("doc %q: the emoji should stay", got.Text())
	}
	for i := 0; i < got.Len(); i++ {
		want := "https://example.com/docs"
		if got.Attrs[i].Atom != nil {
			want = ""
		}
		if got.Attrs[i].Link != want {
			t.Fatalf("rune %d link %q, want %q", i, got.Attrs[i].Link, want)
		}
	}

	// plain text, or a link being edited: the editable field
	dlg = nil
	d = NewDoc()
	d.Insert(0, "plain words", Attr{})
	c.ed.SetDoc(d)
	c.ed.SetSelection(0, 5)
	c.openLink()
	if dlg == nil || dlg.preview != nil {
		t.Fatalf("plain selection: %+v", dlg)
	}
	// a new link over two lines can't edit its text either
	dlg = nil
	d = NewDoc()
	d.Insert(0, "a\nb", Attr{})
	c.ed.SetDoc(d)
	c.ed.SetSelection(0, 3)
	c.openLink()
	if dlg == nil || dlg.preview == nil {
		t.Fatalf("two lines: %+v", dlg)
	}
}

// TestW10NativeSpellMenu: with a native menu (nativemenu, faked here)
// the spellcheck menu goes to the OS with the same rows, and the pick
// runs in the editor's next frame.
func TestW10NativeSpellMenu(t *testing.T) {
	var got []nativemenu.Item
	nativemenu.Fake = func(items []nativemenu.Item, pt image.Point) int {
		got = items
		return 0 // the first suggestion
	}
	t.Cleanup(func() { nativemenu.Fake = nil })
	for _, onRelease := range []bool{false, true} {
		prev := nativeOnRelease
		nativeOnRelease = onRelease
		e := newSpellEnv(t, 0)
		ctxclick.Mac = false
		d := NewDoc()
		d.Insert(0, "hello teh dog", Attr{})
		e.ed.SetDoc(d)
		e.frame()
		e.settle(1200 * time.Millisecond)
		x, base, _, _, _ := e.ed.lay.caretInfo(7)
		pt := f32.Pt(float32(x), float32(base-4))
		got = nil
		e.r.Queue(
			pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt},
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonSecondary, Position: pt},
		)
		e.settle(100 * time.Millisecond)
		if onRelease && e.ed.spell.native != nil {
			t.Fatal("Windows: the native menu opened before the button's release")
		}
		e.r.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt})
		deadline := time.Now().Add(5 * time.Second)
		for e.ed.doc.Text() != "hello the dog" {
			if time.Now().After(deadline) {
				t.Fatalf("onRelease=%v: the pick never ran: %q", onRelease, e.ed.doc.Text())
			}
			e.frame()
			time.Sleep(2 * time.Millisecond)
		}
		if len(e.host.shown) != 0 {
			t.Errorf("a drawn menu opened too: %d", len(e.host.shown))
		}
		var labels []string
		for _, it := range got {
			if it.Sep {
				labels = append(labels, "---")
				continue
			}
			labels = append(labels, it.Label)
		}
		want := []string{"the", "tea", "ten", "---", "Add to Dictionary", "Search with Google", "---", "Cut", "Copy", "Paste"}
		if !slices.Equal(labels, want) {
			t.Fatalf("native rows %q, want %q", labels, want)
		}
		for _, it := range got {
			if it.Label == "Paste" && (it.Key != "v" || it.Disabled) {
				t.Errorf("Paste %+v", it)
			}
			if it.Label == "Cut" && it.Key != "x" {
				t.Errorf("Cut %+v", it)
			}
		}
		nativeOnRelease = prev
	}
}
