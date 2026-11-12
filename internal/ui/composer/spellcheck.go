package composer

import (
	"image"
	"image/color"
	"math"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	"gioui.org/f32"
	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/spell"
	"github.com/some-du6e/clack/internal/ui/ctxclick"
	"github.com/some-du6e/clack/internal/ui/nativemenu"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/shortcut"
)

// Spellcheck as Slack's desktop app does it: Texty's contenteditable
// carries spellcheck={webapp_spellcheck} (handlePropChanges), and
// Chromium checks it.
//
//   - What's checked: the text. Code blocks are not (Texty's
//     CodeBlockContainer sets spellcheck=false); mentions, channel links,
//     broadcasts, message slugs and emoji are atoms (SlackMention,
//     c-slackslug and SlackTag set spellcheck=false, emoji are images).
//     Inline code and link text are checked, as Chromium does: their
//     blots leave the attribute alone.
//   - When (Blink's IdleSpellCheckController): every change or caret move
//     runs a hot-mode check soon after, around the caret, unless you're
//     typing inside a word (the last edit was typing and the caret is
//     still at its end: that word's underline goes away and nothing is
//     checked). One second after a hot check, the cold mode checks the
//     whole field the first time, then ±64 characters around the caret
//     (the whole field again after 1024 characters of change), which is
//     how a word you stopped typing gets its underline. Only the focused
//     field is checked; underlines stay when it loses focus.
//   - Answers come from the spell service (internal/spell) off the UI
//     goroutine; a frame only reads its cache, and checks whose answers
//     are missing wait for the service's ping. A check whose text changed
//     meanwhile is dropped, like SpellChecker::MarkAndReplaceFor.
//   - Markers follow edits; one an edit touches goes away
//     (DocumentMarkerListEditor's content-dependent shift).
//   - The right-click menu is the desktop shell's buildMenuForTextInput.

// spellMarker is a misspelled word, runes [lo, hi).
type spellMarker struct {
	lo, hi int
	word   string
}

type spellReq struct {
	lo, hi int
	ver    uint64
}

type edSpell struct {
	off      bool // NoSpellcheck
	markers  []spellMarker
	text     []rune // the runes the markers belong to
	ver      uint64
	init     bool
	svcGen   uint64
	sel      [2]int
	hot      bool
	coldAt   time.Time
	full     bool
	fullLen  int
	accDelta int
	coldVer  uint64
	pending  []spellReq
	ctxTag   int // context-click handler
	menuWait *spellMenuWait
	native   *spellNative // a native menu that's open (nativemenu)
}

// spellNative is a native menu's items and, once it closed, the pick
// (set from the menu's goroutine; the editor runs it in its next frame).
type spellNative struct {
	items []popups.MenuItem
	mu    sync.Mutex
	done  bool
	pick  int
}

// Chromium's constants (hot_mode_spell_check_requester.cc,
// cold_mode_spell_check_requester.cc, idle_spell_check_controller.cc).
const (
	spHotCheckAll  = 128
	spHotChunk     = 1024
	spColdLocal    = 128
	spRecheckDelta = 1024
	spColdDelay    = time.Second
)

// SetSpellcheck turns checking off for an editor Slack doesn't check
// (on by default; the pref still applies).
func (e *Editor) SetSpellcheck(on bool) { e.spell.off = !on }

// Misspelled returns the marked words (tests).
func (e *Editor) Misspelled() []string {
	var out []string
	for _, m := range e.spell.markers {
		out = append(out, m.word)
	}
	return out
}

// spellSvc is the service to use, nil when checking is off.
func (e *Editor) spellSvc() *spell.Service {
	if e.spell.off {
		return nil
	}
	s := spell.Default()
	if s == nil || !s.Enabled() {
		return nil
	}
	return s
}

// spellUpdate runs once per frame before painting.
func (e *Editor) spellUpdate(gtx layout.Context, focused bool) {
	sp := &e.spell
	svc := e.spellSvc()
	if svc == nil {
		sp.markers, sp.pending, sp.full, sp.hot = nil, nil, false, false
		sp.coldAt = time.Time{}
		sp.init = false
		return
	}
	d := e.doc
	contentChanged := false
	if !sp.init {
		sp.init = true
		sp.text = append(sp.text[:0], d.Runes...)
		sp.ver = e.version
		sp.svcGen = svc.Gen()
		sp.hot = true
	}
	if sp.ver != e.version {
		e.spellShift(d.Runes)
		sp.ver = e.version
		contentChanged = true
	}
	if g := svc.Gen(); g != sp.svcGen {
		// languages, a word added, the pref: drop what's now correct and
		// check everything again
		sp.svcGen = g
		kept := sp.markers[:0]
		for _, m := range sp.markers {
			if c, ok := svc.Lookup(spell.Word{Text: m.word}); ok && c {
				continue
			}
			kept = append(kept, m)
		}
		sp.markers = kept
		sp.full = false
		sp.hot = true
	}
	if s := [2]int{e.anchor, e.caret}; s != sp.sel {
		sp.sel = s
		sp.hot = true
	}
	if contentChanged {
		sp.hot = true
	}
	if focused && sp.hot {
		sp.hot = false
		e.spellHot(svc.Tokenizer(), contentChanged)
		sp.coldAt = gtx.Now.Add(spColdDelay)
	}
	if focused && !sp.coldAt.IsZero() {
		if !gtx.Now.Before(sp.coldAt) {
			sp.coldAt = time.Time{}
			e.spellCold()
		} else {
			gtx.Execute(op.InvalidateCmd{At: sp.coldAt})
		}
	}
	e.spellResolve(svc)
}

// spellShift moves the markers through the edit from sp.text to now.
func (e *Editor) spellShift(now []rune) {
	sp := &e.spell
	old := sp.text
	p := 0
	for p < len(old) && p < len(now) && old[p] == now[p] {
		p++
	}
	s := 0
	for s < len(old)-p && s < len(now)-p && old[len(old)-1-s] == now[len(now)-1-s] {
		s++
	}
	oldEnd := len(old) - s
	delta := len(now) - len(old)
	kept := sp.markers[:0]
	for _, m := range sp.markers {
		switch {
		case m.hi <= p:
			kept = append(kept, m)
		case m.lo >= oldEnd:
			m.lo += delta
			m.hi += delta
			kept = append(kept, m)
		}
	}
	sp.markers = kept
	sp.text = append(sp.text[:0], now...)
}

// spellSkip reports runes that aren't checked: atoms and code blocks.
func (e *Editor) spellSkip() func(i int) bool {
	d := e.doc
	return func(i int) bool {
		if d.Attrs[i].Atom != nil {
			return true
		}
		return d.Lines[d.LineOf(i)].Kind == CodeBlock
	}
}

// currentWord is CurrentWordIfTypingInPartialWord: while the last edit
// was typing and the caret hasn't moved since, the word it touches.
func (e *Editor) currentWord(tok spell.Tokenizer) (lo, hi int, ok bool) {
	if e.anchor != e.caret || e.lastKind != edTyping || e.lastCaret != e.caret || e.lastVer != e.version {
		return 0, 0, false
	}
	pos := e.caret
	l := e.doc.LineOf(pos)
	ls, le := e.doc.LineStart(l), e.doc.LineEnd(l)
	for _, w := range tok.Words(e.doc.Runes[ls:le], nil) {
		if ls+w.Start < pos && pos <= ls+w.End {
			return ls + w.Start, ls + w.End, true
		}
	}
	return 0, 0, false
}

func (e *Editor) spellHot(tok spell.Tokenizer, contentChanged bool) {
	sp := &e.spell
	if !contentChanged && sp.full {
		return // a caret move in a field the cold mode already checked
	}
	if lo, hi, ok := e.currentWord(tok); ok {
		e.spellUnmark(lo, hi)
		return
	}
	n := e.doc.Len()
	if n <= spHotCheckAll {
		e.spellRequest(0, n)
		return
	}
	l := e.doc.LineOf(e.caret)
	ls, le := e.doc.LineStart(l), e.doc.LineEnd(l)
	if le-ls <= spHotChunk {
		e.spellRequest(ls, le)
		return
	}
	e.spellRequest(e.caret-spHotChunk/2, e.caret+spHotChunk/2)
}

func (e *Editor) spellCold() {
	sp := &e.spell
	n := e.doc.Len()
	if !sp.full {
		sp.full, sp.fullLen, sp.accDelta, sp.coldVer = true, n, 0, e.version
		e.spellRequest(0, n)
		return
	}
	if sp.coldVer == e.version {
		return // unchanged since the last cold check (same DOM version)
	}
	sp.coldVer = e.version
	sp.accDelta += spAbs(n - sp.fullLen)
	sp.fullLen = n
	if sp.accDelta > spRecheckDelta {
		sp.full = false
		e.spellCold()
		return
	}
	e.spellRequest(e.caret-spColdLocal/2, e.caret+spColdLocal/2)
}

// spellRequest queues a check of [lo, hi), widened to whole words.
func (e *Editor) spellRequest(lo, hi int) {
	d := e.doc
	lo, hi = max(lo, 0), min(hi, d.Len())
	for lo > 0 && !spSpace(d.Runes[lo-1]) {
		lo--
	}
	for hi < d.Len() && !spSpace(d.Runes[hi]) {
		hi++
	}
	if lo > hi {
		return
	}
	e.spell.pending = append(e.spell.pending, spellReq{lo, hi, e.version})
}

func spSpace(r rune) bool { return r == ' ' || r == '\n' || r == '\t' || r == ObjRune }

func spAbs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// spellUnmark drops the markers overlapping [lo, hi].
func (e *Editor) spellUnmark(lo, hi int) {
	kept := e.spell.markers[:0]
	for _, m := range e.spell.markers {
		if m.hi < lo || m.lo > hi {
			kept = append(kept, m)
		}
	}
	e.spell.markers = kept
}

// spellResolve applies the checks whose answers are all cached.
func (e *Editor) spellResolve(svc *spell.Service) {
	sp := &e.spell
	if len(sp.pending) == 0 {
		return
	}
	tok := svc.Tokenizer()
	skip := e.spellSkip()
	rs := e.doc.Runes
	var keep []spellReq
	for _, rq := range sp.pending {
		if rq.ver != e.version || rq.hi > len(rs) {
			continue // the text changed: a new check is on its way
		}
		sk := func(i int) bool { return skip(rq.lo + i) }
		ws := tok.Words(rs[rq.lo:rq.hi], sk)
		var bad []spellMarker
		all := true
		for _, w := range ws {
			c, ok := svc.Lookup(w)
			if !ok {
				all = false
				continue
			}
			if !c {
				bad = append(bad, spellMarker{rq.lo + w.Start, rq.lo + w.End, w.Text})
			}
		}
		if !all {
			keep = append(keep, rq)
			continue
		}
		// clear the stale markers in the range, then mark
		kept := sp.markers[:0]
		for _, m := range sp.markers {
			if m.hi <= rq.lo || m.lo >= rq.hi {
				kept = append(kept, m)
			}
		}
		sp.markers = append(kept, bad...)
	}
	sp.pending = keep
	sortMarkers(sp.markers)
}

func sortMarkers(ms []spellMarker) {
	for i := 1; i < len(ms); i++ {
		for j := i; j > 0 && ms[j].lo < ms[j-1].lo; j-- {
			ms[j], ms[j-1] = ms[j-1], ms[j]
		}
	}
}

// markerAt is the marker containing pos (Blink's marker at the hit
// position: start ≤ pos ≤ end).
func (e *Editor) markerAt(pos int) (spellMarker, bool) {
	for _, m := range e.spell.markers {
		if pos >= m.lo && pos <= m.hi {
			return m, true
		}
	}
	return spellMarker{}, false
}

// SpellMacStyle selects Chromium's macOS marker (dots) over the others'
// wave; tests set it.
var SpellMacStyle = runtime.GOOS == "darwin"

// Chromium's spelling marker colours: LayoutTheme (255, 0, 0) and
// LayoutThemeMac (255, 59, 48, 191), WebKit's.
var (
	spellRed    = color.NRGBA{R: 255, A: 255}
	spellMacRed = color.NRGBA{R: 255, G: 59, B: 48, A: 191}
)

// paintSpelling draws the markers (in content coordinates).
func (e *Editor) paintSpelling(gtx layout.Context) {
	if len(e.spell.markers) == 0 {
		return
	}
	lay := &e.lay
	z := gtx.Metric.PxPerDp
	for _, m := range e.spell.markers {
		if m.hi > e.doc.Len() {
			continue
		}
		lay.forRange(m.lo, m.hi, func(li, x0, x1 int, nl bool) {
			ln := &lay.lines[li]
			if x1 <= x0 {
				return
			}
			if SpellMacStyle {
				paintSpellDots(gtx, float32(x0), float32(x1), float32(ln.base), z)
			} else {
				paintSpellWave(gtx, float32(x0), float32(x1), float32(ln.base), z)
			}
		})
	}
}

// paintSpellWave is Blink's non-Apple spelling decoration
// (text_decoration_info.cc MakeSpellingGrammarWave, decoration_line_painter.cc):
// a 1px stroke of cubic waves, wavelength 6, control points ±5 at the
// half wavelength, phase −¾ wavelength, all times the zoom. The line sits
// at the auto underline offset (ascent + max(1, ⌈thickness/2⌉), i.e. that
// far below the baseline) plus the wavy offset (thickness + 1) and the
// half-pixel centring; it's clipped to the word's width.
func paintSpellWave(gtx layout.Context, x0, x1, base, z float32) {
	th := z
	gap := float32(math.Max(1, math.Ceil(float64(th/2))))
	y := base + gap + (th + 1) + 0.5
	wl, cp := 6*z, 5*z
	phase := -0.75 * wl
	defer clip.Rect(image.Rect(int(x0), int(y-cp), int(math.Ceil(float64(x1))), int(y+cp+1))).Push(gtx.Ops).Pop()
	var p clip.Path
	p.Begin(gtx.Ops)
	x := x0 + phase
	p.MoveTo(f32.Pt(x, y))
	for x < x1+wl {
		p.CubeTo(f32.Pt(x+wl/2, y+cp), f32.Pt(x+wl/2, y-cp), f32.Pt(x+wl, y))
		x += wl
	}
	st := clip.Stroke{Path: p.End(), Width: th}.Op().Push(gtx.Ops)
	paint.ColorOp{Color: spellRed}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	st.Pop()
}

// paintSpellDots is Blink's Apple spelling decoration: a dotted stroke 2
// thick, which StyledStrokeData draws as 2×2 dashes with 2 gaps (dotted
// lines up to 3 wide are dashed), centred floor(offset + 1) below the
// baseline where offset is ascent + 1; under 4 wide it's solid, under 6
// two proportional dashes.
func paintSpellDots(gtx layout.Context, x0, x1, base, z float32) {
	th := 2 * z
	top := base + z + float32(math.Floor(float64(th/2))) - th/2
	w := x1 - x0
	dash, gapw := th, th
	fill := func(a, b float32) {
		r := image.Rect(int(math.Round(float64(a))), int(math.Round(float64(top))), int(math.Round(float64(b))), int(math.Round(float64(top+th))))
		if r.Dx() <= 0 {
			return
		}
		st := clip.Rect(r).Push(gtx.Ops)
		paint.ColorOp{Color: spellMacRed}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		st.Pop()
	}
	switch {
	case w <= 2*dash:
		fill(x0, x1)
	case w <= 2*dash+gapw:
		k := w / (2*dash + gapw)
		fill(x0, x0+dash*k)
		fill(x1-dash*k, x1)
	default:
		for x := x0; x < x1; x += dash + gapw {
			fill(x, min(x+dash, x1))
		}
	}
}

// ---- the context menu ----

// MenuHost opens the editor's context menu (the ui root sets it; nil, no
// menu). winPointer is the pointer in window coordinates.
var (
	MenuHost   popups.Host
	winPointer image.Point
)

// spellMenuTap sees the context menu's items (tests).
var spellMenuTap func([]popups.MenuItem)

// NotePointer records the pointer's window position (the ui root calls it
// for every root pointer event, before widgets see them).
func NotePointer(p image.Point) { winPointer = p }

// spellContextEvents opens the menu on a context press (secondary button,
// or Ctrl+click on macOS).
func (e *Editor) spellContextEvents(gtx layout.Context) {
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &e.spell.ctxTag, Kinds: pointer.Press | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		pe, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		if pe.Kind != pointer.Press {
			if w := e.spell.menuWait; w != nil {
				w.held = false
			}
			continue
		}
		if !ctxclick.Press(pe) {
			continue
		}
		e.onContextPress(gtx, pe.Position.Round())
	}
	if w := e.spell.menuWait; w != nil {
		e.menuPoll(gtx)
	}
	e.nativePick(gtx)
}

// nativePick runs the item a native menu's pick names, in a frame (Cut,
// Copy and Paste need its context).
func (e *Editor) nativePick(gtx layout.Context) {
	n := e.spell.native
	if n == nil {
		return
	}
	n.mu.Lock()
	done, pick := n.done, n.pick
	n.mu.Unlock()
	if !done {
		return
	}
	e.spell.native = nil
	if pick < 0 || pick >= len(n.items) {
		return
	}
	it := n.items[pick]
	if it.Disabled || it.Separator {
		return
	}
	gtx.Execute(key.FocusCmd{Tag: e})
	switch {
	case it.DoCtx != nil:
		it.DoCtx(gtx)
	case it.Do != nil:
		it.Do()
	}
}

// onContextPress is Blink's mouse-down plus UpdateSelectionForContextMenuEvent:
// outside the selection the caret moves there; on a misspelled word the
// word is selected; on macOS (ShouldSelectOnContextualMenuClick) the word
// under the pointer is.
func (e *Editor) onContextPress(gtx layout.Context, pt image.Point) {
	gtx.Execute(key.FocusCmd{Tag: e})
	pos := e.hitPos(pt)
	lo, hi := e.sel()
	inSel := lo != hi && pos >= lo && pos < hi
	misspelled := ""
	var mlo, mhi int
	if !inSel {
		e.anchor, e.caret = pos, pos
		e.pendingOK = false
		e.desiredX = -1
		e.lastKind = edNone
		if m, ok := e.markerAt(pos); ok {
			e.anchor, e.caret = m.lo, m.hi
			misspelled, mlo, mhi = m.word, m.lo, m.hi
		} else if ctxclick.Mac {
			wlo, whi := edWordAt(e.doc, pos)
			e.anchor, e.caret = wlo, whi
		}
	} else if m, ok := e.markerAt(pos); ok && m.lo == lo && m.hi == hi {
		misspelled, mlo, mhi = m.word, m.lo, m.hi
	}
	at := winPointer
	if MenuHost == nil {
		return
	}
	w := &spellMenuWait{at: at, word: misspelled, lo: mlo, hi: mhi, ver: e.version, since: gtx.Now, held: true}
	e.spell.menuWait = w
	e.menuPoll(gtx)
}

// spellMenuWait is a menu waiting for its suggestions: the service
// computes them off the UI goroutine; the menu opens when they arrive, or
// after 300 ms without them.
type spellMenuWait struct {
	at      image.Point
	word    string
	lo, hi  int
	ver     uint64
	since   time.Time
	pending bool
	held    bool // the button that opened it is still down
}

const spellMenuWaitMax = 300 * time.Millisecond

func (e *Editor) menuPoll(gtx layout.Context) {
	w := e.spell.menuWait
	var sugg []string
	if w.word != "" {
		if svc := spell.Default(); svc != nil {
			s, ok := svc.Suggestions(w.word)
			if !ok && gtx.Now.Sub(w.since) < spellMenuWaitMax {
				gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(16 * time.Millisecond)})
				return
			}
			sugg = s
		}
	}
	if w.held && nativeOnRelease && nativemenu.Supported() {
		return // Windows: the menu comes with WM_CONTEXTMENU, after the button's release
	}
	e.spell.menuWait = nil
	items := e.contextItems(w, sugg)
	if spellMenuTap != nil {
		spellMenuTap(items)
	}
	if e.popupNative(items, w.at) {
		return
	}
	MenuHost.ShowOverlay(e.menuFor(w, items))
}

// nativeOnRelease: on Windows a context menu opens when the button goes
// up (WM_CONTEXTMENU follows WM_RBUTTONUP, and Chromium's contextmenu
// event with it), and TrackPopupMenuEx must not start under a held
// button, whose release it would take as a pick; AppKit's opens on the
// press (rightMouseDown: menuForEvent).
var nativeOnRelease = runtime.GOOS == "windows"

// popupNative shows the menu the way Electron's Menu.popup does, drawn
// by the OS (nativemenu: NSMenu on macOS, the system popup menu on
// Windows). It reports false where there's none (Linux, tests).
func (e *Editor) popupNative(items []popups.MenuItem, at image.Point) bool {
	if !nativemenu.Supported() {
		return false
	}
	nat := make([]nativemenu.Item, len(items))
	for i, it := range items {
		nat[i] = nativemenu.Item{Label: it.Label, Hint: it.Hint, Sep: it.Separator, Disabled: it.Disabled}
		switch it.Key {
		case "cut":
			nat[i].Key = "x"
		case "copy":
			nat[i].Key = "c"
		case "paste":
			nat[i].Key = "v"
		}
		if runtime.GOOS == "darwin" {
			nat[i].Hint = "" // the key equivalent draws it
		}
	}
	n := &spellNative{items: items}
	host := MenuHost
	ok := nativemenu.Popup(nat, at, func(pick int) {
		n.mu.Lock()
		n.done, n.pick = true, pick
		n.mu.Unlock()
		if host != nil {
			host.Invalidate() // the editor runs the pick in its next frame
		}
	})
	if ok {
		e.spell.native = n
	}
	return ok
}

// menuFor is the drawn menu (where there's no native one).
func (e *Editor) menuFor(w *spellMenuWait, items []popups.MenuItem) *popups.Menu {
	return popups.NewMenuOpts(MenuHost, items, popups.MenuOpts{Anchor: image.Rectangle{Min: w.at, Max: w.at.Add(image.Pt(1, 1))}, Width: 220})
}

// contextItems are contextMenu's rows.
func (e *Editor) contextItems(w *spellMenuWait, sugg []string) []popups.MenuItem {
	var items []popups.MenuItem
	ver := w.ver
	for _, s := range sugg {
		s := s
		items = append(items, popups.MenuItem{Label: s, Key: "spell-" + s, Do: func() { e.replaceMisspelling(w.lo, w.hi, ver, s) }})
	}
	if len(sugg) > 0 {
		items = append(items, popups.Sep())
	}
	if w.word != "" {
		word := w.word
		items = append(items, popups.MenuItem{Label: "Add to Dictionary", Key: "add-to-dictionary", Do: func() {
			if svc := spell.Default(); svc != nil {
				svc.Add(word)
			}
		}})
	}
	lo, hi := e.sel()
	selText := strings.TrimSpace(e.doc.String(lo, hi))
	if selText != "" {
		q := selText
		items = append(items, popups.MenuItem{Label: "Search with Google", Key: "search-google", Do: func() {
			_ = openurl.Open("https://www.google.com/search?q=" + url.QueryEscape(q))
		}})
		items = append(items, popups.Sep())
	}
	has := lo != hi
	items = append(items,
		popups.MenuItem{Label: "Cut", Key: "cut", Hint: shortcut.Format("Mod+X"), Disabled: !has, DoCtx: func(gtx layout.Context) {
			if e.copySel(gtx) {
				e.deleteSel()
			}
		}},
		popups.MenuItem{Label: "Copy", Key: "copy", Hint: shortcut.Format("Mod+C"), Disabled: !has, DoCtx: func(gtx layout.Context) { e.copySel(gtx) }},
		popups.MenuItem{Label: "Paste", Key: "paste", Hint: shortcut.Format("Mod+V"), DoCtx: func(gtx layout.Context) {
			e.plainPaste = false
			e.richRead = startClipHTML()
			gtx.Execute(clipboard.ReadCmd{Tag: e})
			e.emit(EvPasted)
		}},
	)
	return items
}

// replaceMisspelling is webContents.replaceMisspelling: the selected
// misspelled word becomes s, as one undo step.
func (e *Editor) replaceMisspelling(lo, hi int, ver uint64, s string) {
	if e.version != ver || hi > e.doc.Len() {
		return // the text changed under the menu
	}
	e.Replace(lo, hi, s)
	e.spellUnmark(lo, lo+len([]rune(s)))
}

// spellHandler registers the context press handler in the editor's area.
func (e *Editor) spellHandler(gtx layout.Context) {
	event.Op(gtx.Ops, &e.spell.ctxTag)
}
