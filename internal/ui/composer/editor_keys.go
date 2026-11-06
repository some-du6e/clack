package composer

import (
	"io"
	"strings"
	"unicode"

	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/transfer"
	"gioui.org/layout"
)

// edMaxIndent is the deepest list nesting level (0-based).
const edMaxIndent = 5

// keyFilters lists every key the editor handles. Shift+7/8/9 arrive as
// "&", "*", "(" on X11 (the keysym of the shifted key) but as "7", "8",
// "9" with ModShift elsewhere, so both are registered.
func (e *Editor) keyFilters() []event.Filter {
	sc := key.ModShortcut
	anyMod := key.ModShift | key.ModCtrl | key.ModAlt | key.ModCommand
	move := key.ModShortcutAlt | key.ModShift | key.ModShortcut
	fs := []event.Filter{
		key.FocusFilter{Target: e},
		transfer.TargetFilter{Target: e, Type: "application/text"},
		key.Filter{Focus: e, Name: key.NameEnter, Optional: anyMod},
		key.Filter{Focus: e, Name: key.NameReturn, Optional: anyMod},
		key.Filter{Focus: e, Name: key.NameTab, Optional: key.ModShift},
		key.Filter{Focus: e, Name: key.NameEscape, Optional: anyMod},

		key.Filter{Focus: e, Name: "A", Required: sc},
		key.Filter{Focus: e, Name: "B", Required: sc},
		key.Filter{Focus: e, Name: "C", Required: sc, Optional: key.ModShift | key.ModAlt},
		key.Filter{Focus: e, Name: "I", Required: sc},
		key.Filter{Focus: e, Name: "U", Required: sc, Optional: key.ModShift},
		key.Filter{Focus: e, Name: "V", Required: sc, Optional: key.ModShift},
		key.Filter{Focus: e, Name: "X", Required: sc, Optional: key.ModShift},
		key.Filter{Focus: e, Name: "Y", Required: sc},
		key.Filter{Focus: e, Name: "Z", Required: sc, Optional: key.ModShift},

		key.Filter{Focus: e, Name: key.NameDeleteBackward, Optional: move},
		key.Filter{Focus: e, Name: key.NameDeleteForward, Optional: move},
		key.Filter{Focus: e, Name: key.NameHome, Optional: sc | key.ModShift},
		key.Filter{Focus: e, Name: key.NameEnd, Optional: sc | key.ModShift},
		key.Filter{Focus: e, Name: key.NamePageUp, Optional: key.ModShift},
		key.Filter{Focus: e, Name: key.NamePageDown, Optional: key.ModShift},
		key.Filter{Focus: e, Name: key.NameLeftArrow, Optional: move},
		key.Filter{Focus: e, Name: key.NameRightArrow, Optional: move},
		key.Filter{Focus: e, Name: key.NameUpArrow, Optional: move},
		key.Filter{Focus: e, Name: key.NameDownArrow, Optional: move},
	}
	for _, n := range []key.Name{"7", "8", "9", "&", "*", "("} {
		fs = append(fs, key.Filter{Focus: e, Name: n, Required: sc | key.ModShift})
	}
	return fs
}

// command handles one key press.
func (e *Editor) command(gtx layout.Context, k key.Event) {
	keepX := false
	defer func() {
		if !keepX {
			e.desiredX = -1
		}
	}()
	mods := k.Modifiers
	switch k.Name {
	case key.NameReturn, key.NameEnter, key.NameTab, key.NameUpArrow, key.NameDownArrow, key.NameEscape:
		if e.Intercept != nil && e.Intercept(k.Name, mods) {
			return
		}
	}
	shift := mods.Contain(key.ModShift)
	word := mods.Contain(key.ModShortcutAlt)
	short := mods.Contain(key.ModShortcut)
	// toLine is Cmd+arrow/backspace on macOS (where ModShortcut is Cmd and
	// word motion is Alt); on other platforms both are Ctrl and word wins.
	toLine := short && !word
	if short && e.shortcut(gtx, k.Name, shift, mods.Contain(key.ModAlt)) {
		return
	}
	switch k.Name {
	case key.NameReturn, key.NameEnter:
		e.enter(mods != 0)
	case key.NameEscape:
		e.emit(EvCancel)
	case key.NameTab:
		e.tab(shift)
	case key.NameDeleteBackward:
		e.deleteBackward(word, toLine)
	case key.NameDeleteForward:
		e.deleteForward(word, toLine)
	case key.NameLeftArrow:
		e.moveH(-1, word, toLine, shift)
	case key.NameRightArrow:
		e.moveH(1, word, toLine, shift)
	case key.NameUpArrow:
		if mods == 0 && e.doc.Empty() {
			e.emit(EvEditLast)
			return
		}
		if toLine {
			e.moveTo(0, shift)
			return
		}
		e.moveV(-1, shift)
		keepX = true
	case key.NameDownArrow:
		if toLine {
			e.moveTo(e.doc.Len(), shift)
			return
		}
		e.moveV(1, shift)
		keepX = true
	case key.NamePageUp:
		e.movePage(-1, shift)
		keepX = true
	case key.NamePageDown:
		e.movePage(1, shift)
		keepX = true
	case key.NameHome:
		if short {
			e.moveTo(0, shift)
		} else {
			e.moveTo(e.visualLineStart(e.caret), shift)
		}
	case key.NameEnd:
		if short {
			e.moveTo(e.doc.Len(), shift)
		} else {
			e.moveTo(e.visualLineEnd(e.caret), shift)
		}
	}
}

// shortcut handles Ctrl/Cmd combinations; it reports whether name was one.
func (e *Editor) shortcut(gtx layout.Context, name key.Name, shift, alt bool) bool {
	switch name {
	case "A":
		e.anchor, e.caret = 0, e.doc.Len()
		e.lastKind = edNone
		e.resetBlink = true
	case "B":
		e.ToggleStyle(Bold)
	case "I":
		e.ToggleStyle(Italic)
	case "U":
		if shift {
			e.emit(EvLinkDialog)
		} else {
			e.ToggleStyle(Underline)
		}
	case "C":
		switch {
		case shift && alt:
			e.ToggleBlock(CodeBlock)
		case shift:
			e.ToggleStyle(Code)
		default:
			e.copySel(gtx)
		}
	case "X":
		if shift {
			e.ToggleStyle(Strike)
		} else if e.copySel(gtx) {
			e.deleteSel()
		}
	case "V":
		e.plainPaste = shift
		gtx.Execute(clipboard.ReadCmd{Tag: e})
		e.emit(EvPasted)
	case "Z":
		if shift {
			e.Redo()
		} else {
			e.Undo()
		}
	case "Y":
		e.Redo()
	case "7", "&":
		if !shift {
			return false
		}
		e.ToggleBlock(Ordered)
	case "8", "*":
		if !shift {
			return false
		}
		e.ToggleBlock(Bullet)
	case "9", "(":
		if !shift {
			return false
		}
		e.ToggleBlock(Quote)
	default:
		return false
	}
	return true
}

// copySel writes the selection to the clipboard as plain text and keeps
// the rich slice for pasting back into the composer.
func (e *Editor) copySel(gtx layout.Context) bool {
	lo, hi := e.sel()
	if lo == hi {
		return false
	}
	sl := e.doc.Slice(lo, hi)
	txt := sl.Text()
	e.clipText, e.clipDoc = txt, sl
	gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(txt))})
	return true
}

// enter handles Enter (soft: with a modifier, i.e. Shift+Enter).
// Plain Enter submits, except in lists (new item, or leave the list on an
// empty item) and on an empty quote line (leave the quote).
func (e *Editor) enter(soft bool) {
	d := e.doc
	line := d.LineOf(e.caret)
	k := d.Lines[line].Kind
	if !soft {
		empty := d.LineStart(line) == d.LineEnd(line)
		if k != Bullet && k != Ordered && !(k == Quote && empty) {
			e.emit(EvSubmit)
			return
		}
	}
	e.newline()
}

// newline splits the line at the caret; the new line keeps the block
// kind. On an empty list item or quote line it leaves the block instead
// (a nested list item outdents first).
func (e *Editor) newline() {
	d := e.doc
	lo, hi := e.sel()
	e.begin(edOther, true)
	d.Delete(lo, hi)
	line := d.LineOf(lo)
	ln := &d.Lines[line]
	if (ln.Kind == Bullet || ln.Kind == Ordered || ln.Kind == Quote) && d.LineStart(line) == d.LineEnd(line) {
		if ln.Kind != Quote && ln.Indent > 0 {
			ln.Indent--
		} else {
			*ln = Line{}
		}
		e.anchor, e.caret = lo, lo
	} else {
		pos := d.Insert(lo, "\n", Attr{})
		// Headings and dividers don't continue: the new line is a paragraph.
		if k := d.Lines[line].Kind; isHeading(k) || k == Divider {
			d.Lines[line+1] = Line{}
		}
		e.anchor, e.caret = pos, pos
	}
	e.lastSpace = true
	e.commit()
}

// tab indents (or with shift outdents) the selected list lines; in a code
// block it types a tab.
func (e *Editor) tab(shift bool) {
	d := e.doc
	l0, l1 := e.selLines()
	list := true
	for i := l0; i <= l1 && i < len(d.Lines); i++ {
		if k := d.Lines[i].Kind; k != Bullet && k != Ordered {
			list = false
			break
		}
	}
	if list {
		e.begin(edOther, true)
		for i := l0; i <= l1 && i < len(d.Lines); i++ {
			if shift {
				d.Lines[i].Indent = max(d.Lines[i].Indent-1, 0)
			} else {
				d.Lines[i].Indent = min(d.Lines[i].Indent+1, edMaxIndent)
			}
		}
		e.commit()
		return
	}
	if !shift && d.Lines[l0].Kind == CodeBlock {
		lo, hi := e.sel()
		e.typeAt(lo, hi, "\t")
	}
}

func (e *Editor) deleteSel() {
	lo, hi := e.sel()
	if lo == hi {
		return
	}
	e.begin(edOther, true)
	e.doc.Delete(lo, hi)
	e.anchor, e.caret = lo, lo
	e.commit()
}

// deleteBackward is Backspace: a selection, a block format at the line
// start, a word, the line up to the caret, or one grapheme (atoms are one
// rune, so they go as a unit).
func (e *Editor) deleteBackward(word, toLine bool) {
	d := e.doc
	if lo, hi := e.sel(); lo != hi {
		e.deleteSel()
		return
	}
	c := e.caret
	line := d.LineOf(c)
	ls := d.LineStart(line)
	if c == ls && d.Lines[line].Kind != Para {
		e.begin(edOther, true)
		d.Lines[line] = Line{}
		e.commit()
		return
	}
	if c == 0 {
		return
	}
	from := edPrevStop(d, c)
	switch {
	case word:
		from = edWordLeft(d, c)
	case toLine && ls < c:
		from = ls
	}
	e.begin(edDeleting, word || toLine)
	d.Delete(from, c)
	e.anchor, e.caret = from, from
	e.lastSpace = false
	e.commit()
}

// deleteForward is Delete.
func (e *Editor) deleteForward(word, toLine bool) {
	d := e.doc
	if lo, hi := e.sel(); lo != hi {
		e.deleteSel()
		return
	}
	c := e.caret
	if c >= d.Len() {
		return
	}
	to := edNextStop(d, c)
	switch {
	case word:
		to = edWordRight(d, c)
	case toLine:
		if le := d.LineEnd(d.LineOf(c)); le > c {
			to = le
		}
	}
	e.begin(edDeleting, true)
	d.Delete(c, to)
	e.anchor, e.caret = c, c
	e.commit()
}

// moveTo moves the caret, extending the selection or collapsing it.
func (e *Editor) moveTo(p int, extend bool) {
	e.caret = e.doc.clamp(p)
	if !extend {
		e.anchor = e.caret
	}
	e.pendingOK = false
	e.lastKind = edNone
	e.scrollCaret = true
}

func (e *Editor) moveH(dir int, word, line, extend bool) {
	d := e.doc
	lo, hi := e.sel()
	if !extend && lo != hi && !word && !line {
		p := lo
		if dir > 0 {
			p = hi
		}
		e.moveTo(p, false)
		return
	}
	p := e.caret
	switch {
	case line && dir < 0:
		p = e.visualLineStart(p)
	case line:
		p = e.visualLineEnd(p)
	case word && dir < 0:
		p = edWordLeft(d, p)
	case word:
		p = edWordRight(d, p)
	case dir < 0:
		p = edPrevStop(d, p)
	default:
		p = edNextStop(d, p)
	}
	e.moveTo(p, extend)
}

// moveV moves the caret one visual line up or down, keeping its x.
func (e *Editor) moveV(dir int, extend bool) {
	d := e.doc
	if !e.ensureLayout() {
		cur := d.LineOf(e.caret)
		col := e.caret - d.LineStart(cur)
		line := cur + dir
		switch {
		case line < 0:
			e.moveTo(0, extend)
		case line >= len(d.Lines):
			e.moveTo(d.Len(), extend)
		default:
			e.moveTo(min(d.LineStart(line)+col, d.LineEnd(line)), extend)
		}
		return
	}
	lay := &e.lay
	x, _, _, _, li := lay.caretInfo(e.caret)
	if e.desiredX >= 0 {
		x = e.desiredX
	}
	n := li + dir
	switch {
	case n < 0:
		e.moveTo(0, extend)
	case n >= len(lay.lines):
		e.moveTo(d.Len(), extend)
	default:
		e.moveTo(lay.posAt(n, x), extend)
	}
	e.desiredX = x
}

// movePage moves the caret by the visible height.
func (e *Editor) movePage(dir int, extend bool) {
	d := e.doc
	if !e.ensureLayout() {
		if dir < 0 {
			e.moveTo(0, extend)
		} else {
			e.moveTo(d.Len(), extend)
		}
		return
	}
	lay := &e.lay
	x, _, _, _, li := lay.caretInfo(e.caret)
	if e.desiredX >= 0 {
		x = e.desiredX
	}
	page := max(e.viewH, lay.m.lh)
	y := lay.lines[li].top + dir*page
	switch {
	case y < 0:
		e.moveTo(0, extend)
	case y >= lay.height:
		e.moveTo(d.Len(), extend)
	default:
		e.moveTo(lay.posAt(lay.lineAtY(y), x), extend)
		e.scrollY += dir * page
	}
	e.desiredX = x
}

func (e *Editor) visualLineStart(p int) int {
	if e.ensureLayout() {
		return e.lay.lines[e.lay.lineOf(p)].a
	}
	return e.doc.LineStart(e.doc.LineOf(p))
}

func (e *Editor) visualLineEnd(p int) int {
	if e.ensureLayout() {
		return e.lay.lineEnd(e.doc, e.lay.lineOf(p))
	}
	return e.doc.LineEnd(e.doc.LineOf(p))
}

// Rune classes for word motion.
const (
	edClsSpace = iota
	edClsWord
	edClsPunct
	edClsAtom
	edClsNL
)

func edClass(d *Doc, i int) int {
	if d.Attrs[i].Atom != nil {
		return edClsAtom
	}
	r := d.Runes[i]
	switch {
	case r == '\n':
		return edClsNL
	case edSpace(r):
		return edClsSpace
	case edWordRune(r):
		return edClsWord
	}
	return edClsPunct
}

// edWordLeft returns the start of the word before pos (Ctrl+Left,
// Ctrl+Backspace). A line start steps over the newline; an atom is a word.
func edWordLeft(d *Doc, pos int) int {
	p := d.clamp(pos)
	if p == 0 {
		return 0
	}
	if edClass(d, p-1) == edClsNL {
		return p - 1
	}
	for p > 0 && edClass(d, p-1) == edClsSpace {
		p--
	}
	if p == 0 {
		return 0
	}
	c := edClass(d, p-1)
	switch c {
	case edClsNL:
		return p
	case edClsAtom:
		return p - 1
	}
	for p > 0 && edClass(d, p-1) == c {
		p--
	}
	return p
}

// edWordRight returns the end of the word after pos (Ctrl+Right,
// Ctrl+Delete).
func edWordRight(d *Doc, pos int) int {
	n := d.Len()
	p := d.clamp(pos)
	if p == n {
		return n
	}
	if edClass(d, p) == edClsNL {
		return p + 1
	}
	for p < n && edClass(d, p) == edClsSpace {
		p++
	}
	if p == n {
		return n
	}
	c := edClass(d, p)
	switch c {
	case edClsNL:
		return p
	case edClsAtom:
		return p + 1
	}
	for p < n && edClass(d, p) == c {
		p++
	}
	return p
}

// edWordAt returns the word (or space run, punctuation run, atom) around
// pos, for double-click selection.
func edWordAt(d *Doc, pos int) (lo, hi int) {
	n := d.Len()
	pos = d.clamp(pos)
	i := pos
	if i == n || edClass(d, i) == edClsNL || (edClass(d, i) == edClsSpace && i > 0 && edClass(d, i-1) == edClsWord) {
		if i == 0 || edClass(d, i-1) == edClsNL {
			return pos, pos
		}
		i--
	}
	c := edClass(d, i)
	if c == edClsAtom {
		return i, i + 1
	}
	lo, hi = i, i+1
	for lo > 0 && edClass(d, lo-1) == c {
		lo--
	}
	for hi < n && edClass(d, hi) == c {
		hi++
	}
	return lo, hi
}

func edRegional(r rune) bool { return r >= 0x1f1e6 && r <= 0x1f1ff }

// edJoinsPrev reports whether rune i continues the grapheme cluster of
// rune i-1 (a cheap approximation of UAX #29 covering combining marks,
// variation selectors, ZWJ emoji sequences, skin tones, tags and flags).
func edJoinsPrev(d *Doc, i int) bool {
	if i <= 0 || i >= d.Len() || d.Attrs[i].Atom != nil || d.Attrs[i-1].Atom != nil {
		return false
	}
	r, prev := d.Runes[i], d.Runes[i-1]
	if r == '\n' || prev == '\n' {
		return false
	}
	switch {
	case r == 0x200d || prev == 0x200d:
		return true
	case r >= 0xfe00 && r <= 0xfe0f:
		return true
	case r >= 0x1f3fb && r <= 0x1f3ff:
		return true
	case r >= 0xe0020 && r <= 0xe007f:
		return true
	case unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r):
		return true
	case edRegional(r) && edRegional(prev):
		n := 0
		for j := i - 1; j >= 0 && edRegional(d.Runes[j]) && d.Attrs[j].Atom == nil; j-- {
			n++
		}
		return n%2 == 1
	}
	return false
}

// edPrevStop is the caret stop before pos (one grapheme back).
func edPrevStop(d *Doc, pos int) int {
	p := d.clamp(pos) - 1
	for p > 0 && edJoinsPrev(d, p) {
		p--
	}
	return max(p, 0)
}

// edNextStop is the caret stop after pos (one grapheme forward).
func edNextStop(d *Doc, pos int) int {
	n := d.Len()
	p := d.clamp(pos) + 1
	for p < n && edJoinsPrev(d, p) {
		p++
	}
	return min(p, n)
}
