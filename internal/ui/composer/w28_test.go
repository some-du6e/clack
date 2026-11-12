package composer

import (
	"encoding/json"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// gap-w28's ports (docs/GAPS-2026-11-12.md gap 17, docs/COMPOSER-AUDIT.md).
// Synthetic data and internal/fakeslack only.

// cmdKHost counts the jumpers the composer opens.
type cmdKHost struct {
	*ovHost
	switchers int
}

func (h *cmdKHost) OpenQuickSwitcher() { h.switchers++ }

func (h *cmdKHost) coachmarks() int {
	n := 0
	for _, o := range h.overlays {
		if _, ok := o.(*cmdKCoachmark); ok && !o.Dismissed() {
			n++
		}
	}
	return n
}

// TestW28CmdKCoachmark: MessageInput's onCmdKWhenSelected (rs9k). Mod+K
// with text selected shows MessageInputCmdKCoachmark and the jumper
// opens too (the binding returns true); with nothing selected the key
// isn't the editor's; a second Mod+K while it shows adds none; the edit
// composer has no coachmark; the overlay and Esc (Dismiss) close it, and
// so does its composer going away.
func TestW28CmdKCoachmark(t *testing.T) {
	app, _ := fakeslackApp(t)
	h := &cmdKHost{ovHost: &ovHost{slickHost: &slickHost{app: app, th: theme.New(false)}}}
	ch := "C0W28CMDK"
	forgetDraft(ch)
	t.Cleanup(func() { forgetDraft(ch) })
	c := New(h, ch, "")
	t.Cleanup(c.Close)
	if !c.ed.CmdKCoachmark {
		t.Fatal("the message input has no onCmdKWhenSelected")
	}
	d := NewDoc()
	d.Insert(0, "see the docs", Attr{})
	c.ed.SetDoc(d)

	edPress(c.ed, "K", key.ModShortcut)
	if edHas(c.ed, EvCmdKSelected) {
		t.Fatal("Mod+K with nothing selected went to the coachmark")
	}
	// the filter list the editor reads: Mod+K only with a selection
	hasK := func() bool {
		fs := c.ed.filters
		if c.ed.CmdKCoachmark && c.ed.anchor != c.ed.caret {
			fs = c.ed.filtersSel
		}
		for _, f := range fs {
			if kf, ok := f.(key.Filter); ok && kf.Name == "K" {
				return true
			}
		}
		return false
	}
	if hasK() {
		t.Error("the editor reads Mod+K with no selection (the shell's jumper should get it)")
	}
	c.ed.SetSelection(8, 12)
	if !hasK() {
		t.Error("the editor doesn't read Mod+K over a selection")
	}
	edPress(c.ed, "K", key.ModShortcut|key.ModShift) // DMs, not this
	if edHas(c.ed, EvCmdKSelected) {
		t.Error("Mod+Shift+K went to the coachmark")
	}
	edPress(c.ed, "K", key.ModShortcut)
	if !edHas(c.ed, EvCmdKSelected) {
		t.Fatal("Mod+K over a selection: no EvCmdKSelected")
	}
	c.cmdKSelected()
	if h.coachmarks() != 1 || h.switchers != 1 {
		t.Fatalf("after Mod+K: %d coachmarks, %d jumpers; want 1 and 1", h.coachmarks(), h.switchers)
	}
	c.cmdKSelected() // cmdKCoachmarkVisible already: no second one
	if h.coachmarks() != 1 || h.switchers != 2 {
		t.Fatalf("second Mod+K: %d coachmarks, %d jumpers; want 1 and 2", h.coachmarks(), h.switchers)
	}
	cm := c.cmdK

	// drawn with its composer; gone the frame the composer isn't
	now := time.Now()
	var r input.Router
	ops := new(op.Ops)
	frame := func(drawComposer bool) {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Source: r.Source(), Now: now,
			Constraints: layout.Constraints{Max: image.Pt(800, 600)}}
		slackc.Frame(gtx)
		if drawComposer {
			st := op.Offset(image.Pt(0, 400)).Push(gtx.Ops)
			c.Origin = image.Pt(0, 400)
			c.Layout(gtx)
			st.Pop()
		}
		g := gtx
		g.Constraints = layout.Exact(image.Pt(800, 600))
		cm.Layout(g)
		r.Frame(ops)
		now = now.Add(16 * time.Millisecond)
	}
	frame(true)
	if cm.Dismissed() {
		t.Fatal("the coachmark closed while its composer was drawn")
	}
	at, ok := cm.anchor()
	if !ok || at.Y < 400 {
		t.Fatalf("anchor %v %v: want the composer box's top-left", at, ok)
	}
	// a press on the card stays open; on the overlay it closes
	gtx := layout.Context{Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	lines := cmdKLines(gtx, h.th, cmdKRuns(gtx, h.th), gtx.Dp(cmdKW-2*cmdKPadX))
	card := cmdKCardRect(gtx, at, cmdKCardSize(gtx, lines))
	press := func(p image.Point) {
		pos := f32.Pt(float32(p.X), float32(p.Y))
		r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pos},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pos})
		frame(true)
		frame(true)
	}
	press(card.Min.Add(image.Pt(30, 30)))
	if cm.Dismissed() {
		t.Fatal("a press on the card closed it")
	}
	press(image.Pt(790, 10))
	if !cm.Dismissed() || c.cmdK != nil {
		t.Fatal("a press on the overlay didn't close it (shouldCloseOnOverlayClick)")
	}
	// again, and the composer goes away (another conversation)
	c.cmdKSelected()
	cm = c.cmdK
	frame(true)
	frame(false)
	if !cm.Dismissed() {
		t.Error("the coachmark outlived its MessageInput")
	}
	// Esc: the shell's dismissTop
	c.cmdKSelected()
	c.cmdK.Dismiss()
	if c.cmdK != nil || h.coachmarks() != 0 {
		t.Error("Dismiss left it showing")
	}

	// the edit composer (cjbg) has none: Mod+K is the jumper's
	ec := NewEdit(h, core.Message{Channel: ch, TS: "1700000000.000100", Text: "edit me"}, nil)
	t.Cleanup(ec.Close)
	ec.ed.SetSelection(0, 4)
	edPress(ec.ed, "K", key.ModShortcut)
	if ec.ed.CmdKCoachmark || edHas(ec.ed, EvCmdKSelected) {
		t.Error("the edit composer shows the coachmark")
	}
}

// TestW28CmdKCopyAndGeometry: msg_input's "Trying to add a link? Use
// {commandKey} + {vKey} instead" with KeyboardKey chips (CmdOrCtrl is Ctrl
// off macOS, the command icon on it); the card is 360 wide at the anchor's
// left, its top at anchor.top + 20 − 104% of its height (offsetY 20,
// translate(0, -104%)), padded 20/24/24.
func TestW28CmdKCopyAndGeometry(t *testing.T) {
	parts := cmdKCopy()
	var args []string
	for _, p := range parts {
		if p.Arg != "" {
			args = append(args, p.Arg)
		}
	}
	if got := strings.Join(args, ","); got != "commandKey,vKey" {
		t.Fatalf("chips %q", got)
	}
	if got := i18n.Plain(parts); got != "Trying to add a link? Use \x00 + \x00 instead" {
		t.Errorf("copy %q", got)
	}
	old := shortcut.GOOS
	t.Cleanup(func() { shortcut.GOOS = old })
	shortcut.GOOS = "windows"
	if got := cmdKKeyLabel("commandKey"); got != "Ctrl" {
		t.Errorf("Windows CmdOrCtrl %q, want Ctrl", got)
	}
	shortcut.GOOS = "darwin"
	if got := cmdKKeyLabel("commandKey"); got != "" {
		t.Errorf("macOS CmdOrCtrl %q, want the command icon", got)
	}
	if got := cmdKKeyLabel("vKey"); got != "V" {
		t.Errorf("V chip %q", got)
	}
	shortcut.GOOS = "windows"

	gtx := layout.Context{Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	if r := cmdKCardRect(gtx, image.Pt(100, 500), image.Pt(360, 90)); r != image.Rect(100, 426, 460, 516) {
		t.Errorf("card %v, want (100,426)-(460,516): top 500+20-94", r)
	}
	th := theme.New(false)
	lines := cmdKLines(gtx, th, cmdKRuns(gtx, th), gtx.Dp(cmdKW-2*cmdKPadX))
	if len(lines) == 0 || len(lines) > 2 {
		t.Fatalf("%d lines", len(lines))
	}
	sz := cmdKCardSize(gtx, lines)
	want := 20 + 24
	for _, ln := range lines {
		want += cmdKLineHeight(gtx, ln)
	}
	if sz != image.Pt(360, want) {
		t.Errorf("card %v, want 360×%d", sz, want)
	}
	// lines fit the 312px content box
	for _, ln := range lines {
		w := 0
		for _, r := range ln {
			w += r.w
		}
		if last := ln[len(ln)-1]; !last.chip && strings.HasSuffix(last.text, " ") {
			w -= th.Measure(gtx, 15, theme.Regular, " ")
		}
		if w > 312+2 {
			t.Errorf("a line %dpx wide in a 312px box", w)
		}
	}
	// colours: the card is sk_primary_background, its ring
	// rgba(sk_foreground_low, .13), the overlay rgba(0,0,0,.3)
	p := cmdKColors(th)
	if p.bg.A != 255 || p.bg.R != 255 || p.overlay.A != 77 || p.ring.A != 33 {
		t.Errorf("light colours %+v", p)
	}
	if d := cmdKColors(theme.New(true)); d.bg.R != 26 || d.bg.G != 29 || d.bg.B != 33 {
		t.Errorf("dark card %v, want 26,29,33", d.bg)
	}
}

// TestW28QuotedCodeBlocks: code blocks inside a quote, Quill's border 1 on
// a code-block (BorderAttributor). The block formatter's toggles
// (setCodeBlockOnLine, setBlockQuoteOnLine and their removes), the ```
// shortcut on a quote line (buildLocalCodeBlockMarkdownChangeDelta),
// Enter keeping the border, Backspace on an empty bordered line ("backspace
// empty code block with border"), and the wire: rich_text_preformatted
// border 1, the fallback text, the delta, the blocks read back.
func TestW28QuotedCodeBlocks(t *testing.T) {
	line := func(e *Editor, i int) Line { return e.Doc().Lines[i] }
	e := NewEditor()
	edType(e, ">")
	edType(e, "go run .")
	if l := line(e, 0); l.Kind != Quote {
		t.Fatalf("setup: %+v", l)
	}
	e.ToggleBlock(CodeBlock) // code block on a quote line: {blockquote: false, border: "1", "code-block": true}
	if l := line(e, 0); l != (Line{Kind: CodeBlock, Quoted: true}) {
		t.Fatalf("code block on a quote line: %+v, want a quoted code line", l)
	}
	raw := string(e.Doc().RichTextBlocks())
	if !strings.Contains(raw, `"type":"rich_text_preformatted"`) || !strings.Contains(raw, `"border":1`) {
		t.Fatalf("blocks %s", raw)
	}
	if got := e.Doc().Mrkdwn(); !strings.HasPrefix(got, "> ```") {
		t.Errorf("fallback %q, want it to start \"> ```\" (convertBlocksToMrkdwnDelta)", got)
	}
	back, err := DocFromBlocksJSON(e.Doc().RichTextBlocks(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Lines) != 1 || back.Lines[0] != (Line{Kind: CodeBlock, Quoted: true}) {
		t.Fatalf("read back %+v", back.Lines)
	}
	dl, _ := json.Marshal(e.Doc().Delta())
	if !strings.Contains(string(dl), `"border":"1"`) || !strings.Contains(string(dl), `"code-block":true`) {
		t.Errorf("delta %s", dl)
	}
	e.ToggleBlock(CodeBlock) // removeCodeBlockOnLine on a bordered line: {border: "0", blockquote: true}
	if l := line(e, 0); l != (Line{Kind: Quote}) {
		t.Fatalf("code block off a quoted code line: %+v, want the quote back", l)
	}

	// the quote toggle on a code line sets and clears its border
	e = NewEditor()
	edType(e, "x := 1")
	e.ToggleBlock(CodeBlock)
	e.ToggleBlock(Quote)
	if l := line(e, 0); l != (Line{Kind: CodeBlock, Quoted: true}) {
		t.Fatalf("quote on a code line: %+v", l)
	}
	e.ToggleBlock(Quote)
	if l := line(e, 0); l != (Line{Kind: CodeBlock}) {
		t.Fatalf("quote off a quoted code line: %+v", l)
	}
	// a code block on a quoted list item keeps the border
	e = NewEditor()
	edType(e, ">")
	edType(e, "- item")
	e.ToggleBlock(CodeBlock)
	if l := line(e, 0); l != (Line{Kind: CodeBlock, Quoted: true}) {
		t.Fatalf("code block on a quoted item: %+v", l)
	}

	// ``` on a quote line; Enter keeps the border; Backspace on the empty
	// line takes the border first
	e = NewEditor()
	edType(e, ">")
	edType(e, "```")
	if l := line(e, 0); l != (Line{Kind: CodeBlock, Quoted: true}) {
		t.Fatalf("``` on a quote line: %+v", l)
	}
	edType(e, "a")
	edPress(e, key.NameReturn, key.ModShift)
	if l := line(e, 1); l != (Line{Kind: CodeBlock, Quoted: true}) {
		t.Fatalf("the new code line: %+v, want the border kept", l)
	}
	edPress(e, key.NameDeleteBackward, 0)
	if l := line(e, 1); l != (Line{Kind: CodeBlock}) {
		t.Fatalf("Backspace on the empty bordered line: %+v, want plain code", l)
	}
	// "hello ```" on a quote line: the code block below is inside it
	e = NewEditor()
	edType(e, ">")
	edType(e, "hello ```")
	if len(e.Doc().Lines) != 2 || line(e, 0).Kind != Quote || line(e, 1) != (Line{Kind: CodeBlock, Quoted: true}) {
		t.Fatalf("hello ``` in a quote: %+v", e.Doc().Lines)
	}
	// a plain code block stays unbordered
	e = NewEditor()
	edType(e, "```")
	if l := line(e, 0); l != (Line{Kind: CodeBlock}) {
		t.Fatalf("``` on a plain line: %+v", l)
	}
	if strings.Contains(string(e.Doc().RichTextBlocks()), `"border":1`) {
		t.Errorf("plain code blocks %s", e.Doc().RichTextBlocks())
	}
}

// TestW28QuotedCodeLayout: .ql-code-block[class*=ql-border-] moves the box
// 11px in (margin-left 11px, width calc(100% - 11px)) and the quote bar at
// 0 runs from the quote above down the box to its bottom cap.
func TestW28QuotedCodeLayout(t *testing.T) {
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
	plain.Lines[0] = Line{Kind: CodeBlock}
	quoted := NewDoc()
	quoted.Insert(0, "q\nx\ny", Attr{})
	quoted.Lines[0] = Line{Kind: Quote}
	quoted.Lines[1] = Line{Kind: CodeBlock, Quoted: true}
	quoted.Lines[2] = Line{Kind: CodeBlock, Quoted: true}
	pe, qe := lay(plain), lay(quoted)
	if got, want := qe.lay.paras[1].left, pe.lay.paras[0].left+11; got != want {
		t.Errorf("quoted code text at x=%d, want %d", got, want)
	}
	if !qe.lay.barred(0) || !qe.lay.barred(1) || !qe.lay.barred(2) || pe.lay.barred(0) {
		t.Error("the bar should run down the quote and its code, not plain code")
	}
	last := qe.lay.paras[2]
	if last.boxBot <= last.boxTop || last.groupTop != last.boxTop || qe.lay.paras[1].groupTop != last.boxTop {
		t.Errorf("one box over both code lines: %+v", last)
	}
	if qe.lay.paras[1].boxBot != 0 {
		t.Error("the box closed after the first quoted code line")
	}
}

// TestW28OverflowRow: below the fold point the footer's ⋯ (TextyButtonOverflow)
// holds Run shortcut; a click opens .c-texty_buttons--overflow over it
// (right −36px, top −40px, 4px padding), a press outside closes it, and
// Run shortcut's click closes it too.
func TestW28OverflowRow(t *testing.T) {
	gtx := layout.Context{Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	btn := image.Rect(300, 500, 328, 528)
	if r := crMoreRowRect(gtx, btn, 1); r != image.Rect(328, 458, 364, 498) {
		t.Errorf("one-button row %v, want (328,458)-(364,498)", r)
	}
	if r := crMoreRowRect(gtx, btn, 3); r.Dx() != 4+3*28+2*4+4 || r.Dy() != 40 {
		t.Errorf("three-button row %v", r)
	}

	app, _ := fakeslackApp(t)
	h := &ovHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	ch := "C0W28FOLD"
	forgetDraft(ch)
	t.Cleanup(func() { forgetDraft(ch) })
	c := New(h, ch, "")
	t.Cleanup(c.Close)
	var r input.Router
	ops := new(op.Ops)
	now := time.Now()
	w := 520
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Source: r.Source(), Now: now,
			Constraints: layout.Constraints{Max: image.Pt(w, 400)}}
		slackc.Frame(gtx)
		c.Layout(gtx)
		r.Frame(ops)
		now = now.Add(16 * time.Millisecond)
	}
	for range 3 {
		frame()
	}
	if c.ui.f.eq <= 0 || !c.ui.f.fold {
		t.Fatalf("a %dpx composer: toolbar %d, fold %v; want folded", w, c.ui.f.eq, c.ui.f.fold)
	}
	more := c.ui.rect[crFootMore]
	if more.Empty() || c.ui.shown&(1<<crShortcut) != 0 {
		t.Fatalf("folded footer: ⋯ %v, Run shortcut shown %v", more, c.ui.shown&(1<<crShortcut) != 0)
	}
	click := func(p image.Point) {
		p = p.Add(image.Pt(0, c.top)) // the chrome's rects are below the composer's top
		pos := f32.Pt(float32(p.X), float32(p.Y))
		NotePointer(p)
		NotePress()
		r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pos},
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pos},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pos})
		frame()
		frame()
	}
	click(more.Min.Add(image.Pt(14, 14)))
	if !c.ui.more.open {
		t.Fatal("⋯ didn't open the overflow row")
	}
	frame()
	sc := c.ui.rect[crShortcut]
	if row := crMoreRowRect(gtx, more, 1); !sc.In(row) || c.ui.shown&(1<<crShortcut) == 0 {
		t.Fatalf("Run shortcut at %v, the row %v", sc, row)
	}
	click(image.Pt(5, -c.top+5)) // outside both, at the window's corner
	if c.ui.more.open {
		t.Fatal("a press outside didn't close the row")
	}
	// wide again: nothing folds
	w = 900
	for range 3 {
		frame()
	}
	if c.ui.f.fold || c.ui.shown&(1<<crShortcut) == 0 {
		t.Errorf("a 900px composer folded (toolbar %d)", c.ui.f.eq)
	}
}
