package composer

import (
	"image"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/spell"
	"github.com/some-du6e/clack/internal/ui/ctxclick"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// spellFake is an engine with a small word list; delay slows every check
// (the frame must not notice).
type spellFake struct {
	mu    sync.Mutex
	good  map[string]bool
	delay time.Duration
}

func (f *spellFake) Check(w string) bool {
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.good[strings.ToLower(w)]
}
func (f *spellFake) Suggest(w string) []string {
	switch w {
	case "teh":
		return []string{"the", "tea", "ten"}
	case "wrold":
		return []string{"world"}
	}
	return nil
}
func (f *spellFake) Add(w string) { f.mu.Lock(); f.good[strings.ToLower(w)] = true; f.mu.Unlock() }
func (f *spellFake) Close()       {}

func spellWords() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields("the a an hello world is this code quick fox jumps over dog say hi to and link ok") {
		m[w] = true
	}
	return m
}

// spellEnv is an editor laid out through Gio's router with a spell
// service over spellFake, its clock stepped by hand.
type spellEnv struct {
	t     *testing.T
	ed    *Editor
	svc   *spell.Service
	eng   *spellFake
	r     input.Router
	ops   op.Ops
	now   time.Time
	st    EditorStyle
	host  *keysHost
	items []popups.MenuItem
	slow  time.Duration            // the slowest frame
	scale float32                  // px per dp (0: 1)
	size  image.Point              // the editor's size in dp (0: 600×120)
	paint func(gtx layout.Context) // draws before the editor (shots)
}

func newSpellEnv(t *testing.T, delay time.Duration, opts ...func(*spellEnv)) *spellEnv {
	t.Helper()
	e := &spellEnv{t: t, ed: NewEditor(), eng: &spellFake{good: spellWords(), delay: delay}, now: time.Now()}
	for _, o := range opts {
		o(e)
	}
	e.svc = spell.New(func([]string, string) ([]spell.Engine, error) { return []spell.Engine{e.eng}, nil }, "", t.TempDir(), nil)
	e.svc.SetLanguages([]string{"en-US"})
	e.svc.Start()
	spell.SetDefault(e.svc)
	if e.host == nil {
		e.host = &keysHost{slickHost: &slickHost{th: theme.New(false)}}
	}
	prevHost, prevMac := MenuHost, ctxclick.Mac
	MenuHost = e.host
	spellMenuTap = func(it []popups.MenuItem) { e.items = it }
	t.Cleanup(func() {
		e.svc.Stop()
		spell.SetDefault(nil)
		MenuHost, spellMenuTap, ctxclick.Mac = prevHost, nil, prevMac
	})
	if e.st.Shaper == nil {
		e.st = DefaultEditorStyle(theme.NewShaper())
	}
	e.frame()
	e.ed.Focus(e.gtx())
	e.frame()
	return e
}

func (e *spellEnv) gtx() layout.Context {
	sc := e.scale
	if sc == 0 {
		sc = 1
	}
	sz := e.size
	if sz == (image.Point{}) {
		sz = image.Pt(600, 120)
	}
	return layout.Context{Ops: &e.ops, Source: e.r.Source(), Now: e.now, Metric: unit.Metric{PxPerDp: sc, PxPerSp: sc},
		Constraints: layout.Exact(image.Pt(int(float32(sz.X)*sc), int(float32(sz.Y)*sc)))}
}

func (e *spellEnv) frame() {
	e.ops.Reset()
	g := e.gtx()
	if e.paint != nil {
		e.paint(g)
	}
	start := time.Now()
	e.ed.Layout(g, e.st)
	if d := time.Since(start); d > e.slow {
		e.slow = d
	}
	e.r.Frame(&e.ops)
	e.now = e.now.Add(16 * time.Millisecond)
}

// settle runs frames (the service answers on its own goroutine) for at
// most d of the editor's clock, until no check waits.
func (e *spellEnv) settle(d time.Duration) {
	end := e.now.Add(d)
	deadline := time.Now().Add(5 * time.Second)
	for e.now.Before(end) || len(e.ed.spell.pending) > 0 {
		if time.Now().After(deadline) {
			e.t.Fatalf("checks never finished: %+v", e.ed.spell.pending)
		}
		e.frame()
		time.Sleep(time.Millisecond)
	}
}

func (e *spellEnv) typ(s string) {
	for _, r := range s {
		e.ed.typeAt(e.ed.caret, e.ed.caret, string(r))
		e.frame()
	}
}

func (e *spellEnv) bad() []string { return e.ed.Misspelled() }

// Typing: no underline inside the word being typed; a space marks it; a
// word you stop typing is marked by the cold check a second later.
func TestSpellcheckWhileTyping(t *testing.T) {
	e := newSpellEnv(t, 0)
	e.typ("teh")
	e.settle(200 * time.Millisecond)
	if got := e.bad(); len(got) != 0 {
		t.Fatalf("the word being typed is marked: %q", got)
	}
	e.typ(" ")
	e.settle(100 * time.Millisecond)
	if got := e.bad(); !slices.Equal(got, []string{"teh"}) {
		t.Fatalf("after a space: %q, want [teh]", got)
	}
	e.typ("quick wrold")
	e.settle(300 * time.Millisecond)
	if got := e.bad(); !slices.Equal(got, []string{"teh"}) {
		t.Fatalf("while typing wrold: %q", got)
	}
	e.settle(1200 * time.Millisecond) // the cold mode, 1 s after the last hot check
	if got := e.bad(); !slices.Equal(got, []string{"teh", "wrold"}) {
		t.Fatalf("after a pause: %q, want [teh wrold]", got)
	}
	// typing back inside a marked word removes its underline at once
	e.ed.SetSelection(3, 3)
	e.frame()
	e.ed.typeAt(3, 3, "e")
	e.settle(100 * time.Millisecond)
	if got := e.bad(); slices.Contains(got, "teh") || slices.Contains(got, "tehe") {
		t.Fatalf("editing a marked word kept a marker: %q", got)
	}
	// moving the caret away: the field was checked already, so the cold
	// mode marks it a second later (Blink skips hot mode for a caret move
	// in a fully checked field)
	e.ed.SetSelection(e.ed.doc.Len(), e.ed.doc.Len())
	e.settle(100 * time.Millisecond)
	if got := e.bad(); slices.Contains(got, "tehe") {
		t.Fatalf("marked on the caret move: %q", got)
	}
	e.settle(1200 * time.Millisecond)
	if got := e.bad(); !slices.Contains(got, "tehe") {
		t.Fatalf("after moving away: %q", got)
	}
}

// Code blocks, mentions, emoji and message slugs aren't checked; inline
// code and link text are, as in Slack's Texty.
func TestSpellcheckSkips(t *testing.T) {
	e := newSpellEnv(t, 0)
	d := NewDoc()
	end := d.Insert(0, "say hi to ", Attr{})
	end = d.InsertAtom(end, &Atom{Kind: AtomUser, ID: "U1", Text: "@bobb"})
	end = d.Insert(end, " and ", Attr{})
	end = d.InsertAtom(end, &Atom{Kind: AtomEmoji, ID: "thumbsup", Text: ":thumbsup:"})
	end = d.Insert(end, " lnik ", Attr{Link: "https://example.com"})
	c0 := end
	end = d.Insert(end, "codde", Attr{})
	d.SetStyle(c0, end, Code, true)
	end = d.Insert(end, "\nfmt.Prinln(x)", Attr{})
	d.Lines[1].Kind = CodeBlock
	e.ed.SetDoc(d)
	e.frame()
	e.settle(1200 * time.Millisecond)
	got := e.bad()
	slices.Sort(got)
	if !slices.Equal(got, []string{"codde", "lnik"}) {
		t.Fatalf("marked %q, want [codde lnik] (inline code and links checked; code blocks, atoms not)", got)
	}
}

// Markers move with edits before them and go away with edits in them.
func TestSpellMarkersFollowEdits(t *testing.T) {
	e := newSpellEnv(t, 0)
	d := NewDoc()
	d.Insert(0, "hello wrold ok", Attr{})
	e.ed.SetDoc(d)
	e.frame()
	e.settle(1200 * time.Millisecond)
	if got := e.bad(); !slices.Equal(got, []string{"wrold"}) {
		t.Fatalf("marked %q", got)
	}
	e.ed.Replace(0, 0, "a ")
	e.frame()
	m := e.ed.spell.markers
	if len(m) != 1 || m[0].lo != 8 || m[0].hi != 13 {
		t.Fatalf("marker after an insert before it: %+v", m)
	}
}

// The right-click menu: the desktop shell's buildMenuForTextInput, its
// suggestion replacing the word and Add to Dictionary clearing it.
func TestSpellContextMenu(t *testing.T) {
	e := newSpellEnv(t, 0)
	ctxclick.Mac = false // Windows and Linux first (newSpellEnv restores it)
	d := NewDoc()
	d.Insert(0, "hello teh dog", Attr{})
	e.ed.SetDoc(d)
	e.frame()
	e.settle(1200 * time.Millisecond)
	if got := e.bad(); !slices.Equal(got, []string{"teh"}) {
		t.Fatalf("marked %q", got)
	}
	x, base, _, _, _ := e.ed.lay.caretInfo(7)
	pt := f32.Pt(float32(x), float32(base-4))
	rightClick := func(mods ...pointer.Buttons) {
		e.items = nil
		e.r.Queue(
			pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt, Time: time.Since(e.now.Add(-time.Hour))},
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonSecondary, Position: pt},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt},
		)
		e.settle(100 * time.Millisecond)
	}
	rightClick()
	if len(e.host.shown) == 0 {
		t.Fatal("no menu")
	}
	m, ok := e.host.shown[len(e.host.shown)-1].(*popups.Menu)
	if !ok {
		t.Fatalf("shown %T", e.host.shown[0])
	}
	want := []string{"the", "tea", "ten", "---", "Add to Dictionary", "Search with Google", "---", "Cut", "Copy", "Paste"}
	if got := m.Labels(); !slices.Equal(got, want) {
		t.Fatalf("menu %q,\nwant %q", got, want)
	}
	if lo, hi := e.ed.sel(); lo != 6 || hi != 9 {
		t.Errorf("the right-click selected [%d,%d), want the misspelled word [6,9)", lo, hi)
	}
	// the first suggestion replaces the word (one undo step)
	e.items[0].Do()
	e.settle(100 * time.Millisecond)
	if got := e.ed.doc.Text(); got != "hello the dog" {
		t.Fatalf("after the suggestion: %q", got)
	}
	if len(e.bad()) != 0 {
		t.Errorf("still marked: %q", e.bad())
	}
	e.ed.Undo()
	e.frame()
	if got := e.ed.doc.Text(); got != "hello teh dog" {
		t.Fatalf("undo: %q", got)
	}
	e.settle(1200 * time.Millisecond)
	// Add to Dictionary: the word is fine from now on
	rightClick()
	var add *popups.MenuItem
	for i := range e.items {
		if e.items[i].Label == "Add to Dictionary" {
			add = &e.items[i]
		}
	}
	if add == nil {
		t.Fatalf("no Add to Dictionary in %q", m.Labels())
	}
	add.Do()
	e.settle(200 * time.Millisecond)
	if len(e.bad()) != 0 {
		t.Errorf("an added word is still marked: %q", e.bad())
	}
	// a right-click on a correct word: no spelling rows, no selection
	// (not macOS), just Cut / Copy / Paste with Cut and Copy greyed
	x, base, _, _, _ = e.ed.lay.caretInfo(1)
	pt = f32.Pt(float32(x), float32(base-4))
	rightClick()
	var labels []string
	for _, it := range e.items {
		if it.Separator {
			labels = append(labels, "---")
			continue
		}
		l := it.Label
		if it.Disabled {
			l += " (off)"
		}
		labels = append(labels, l)
	}
	if !slices.Equal(labels, []string{"Cut (off)", "Copy (off)", "Paste"}) {
		t.Errorf("plain right-click menu %q", labels)
	}
	// macOS selects the word under the pointer (ShouldSelectOnContextualMenuClick)
	ctxclick.Mac = true
	e.ed.SetSelection(0, 0)
	e.frame()
	rightClick()
	if lo, hi := e.ed.sel(); lo != 0 || hi != 5 {
		t.Errorf("macOS right-click selected [%d,%d), want hello [0,5)", lo, hi)
	}
	if !slices.Contains(e.labels(), "Search with Google") {
		t.Errorf("macOS menu for a selected word %q lacks Search with Google", e.labels())
	}
}

func (e *spellEnv) labels() []string {
	var out []string
	for _, it := range e.items {
		if it.Separator {
			out = append(out, "---")
		} else {
			out = append(out, it.Label)
		}
	}
	return out
}

// webapp_spellcheck off: no underlines, nothing checked.
func TestSpellcheckPrefOff(t *testing.T) {
	e := newSpellEnv(t, 0)
	d := NewDoc()
	d.Insert(0, "teh wrold", Attr{})
	e.ed.SetDoc(d)
	e.frame()
	e.settle(1200 * time.Millisecond)
	if len(e.bad()) != 2 {
		t.Fatalf("marked %q", e.bad())
	}
	e.svc.SetEnabled(false)
	e.frame()
	if len(e.bad()) != 0 {
		t.Fatalf("pref off kept %q", e.bad())
	}
	e.ed.SetSpellcheck(false)
	e.svc.SetEnabled(true)
	e.settle(1200 * time.Millisecond)
	if len(e.bad()) != 0 {
		t.Fatalf("an editor with spellcheck off got %q", e.bad())
	}
}

// A slow engine (50 ms a word) never shows in a frame: frames only read
// the cache, and the underlines arrive later.
func TestSpellcheckNeverBlocksFrame(t *testing.T) {
	e := newSpellEnv(t, 50*time.Millisecond)
	d := NewDoc()
	d.Insert(0, strings.Repeat("teh quick wrold jumps ovr the dog ", 6), Attr{})
	e.ed.SetDoc(d)
	e.slow = 0
	for i := 0; i < 80; i++ {
		e.frame()
	}
	e.typ(" more wrods hrere")
	if e.slow > 40*time.Millisecond {
		t.Errorf("a frame took %v with a 50 ms engine", e.slow)
	}
	e.settle(1500 * time.Millisecond)
	if !slices.Contains(e.bad(), "wrold") {
		t.Errorf("the slow answers never arrived: %q", e.bad())
	}
	if e.slow > 40*time.Millisecond {
		t.Errorf("a frame took %v", e.slow)
	}
}
