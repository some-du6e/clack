package composer

import (
	"io"
	"runtime"
	"strings"
	"unicode"

	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/transfer"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/uipref"
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
		key.Filter{Focus: e, Name: "F", Required: sc | key.ModShift}, // "convert markdown" (mdconvert.go)
		key.Filter{Focus: e, Name: "I", Required: sc},
		key.Filter{Focus: e, Name: "O", Required: sc},
		key.Filter{Focus: e, Name: "U", Required: sc, Optional: key.ModShift},
		key.Filter{Focus: e, Name: "V", Required: sc, Optional: key.ModShift | key.ModAlt},
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
	// Mod+Shift+. is the keyboard module's "blockquote-alt" (keyCode
	// 190): "." or, shifted on X11, ">".
	for _, n := range []key.Name{"7", "8", "9", "&", "*", "(", ".", ">"} {
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
	if (k.Name == key.NameReturn || k.Name == key.NameEnter) && short && shift && e.Snippets {
		// MessageInput.onEnter: ⌘⇧Enter (Ctrl+Shift+Enter) with
		// enableSnippetCreation opens the snippet dialog with the message
		e.emit(EvSnippet)
		return
	}
	switch k.Name {
	case key.NameReturn, key.NameEnter:
		if e.anchor == e.caret {
			e.convertEmoticon(e.caret) // onEnter: convert_emoticons (emoticon.go)
		}
		e.enter(mods)
	case key.NameEscape:
		if mods == 0 && e.escapeClearsFormat() {
			// the keyboard module's Escape: an empty quote line, or a code
			// block line that is empty or has the caret at its start,
			// loses its block format (removeFormat) instead of going on
			e.clearLineFormat()
			return
		}
		e.emit(EvCancel)
	case key.NameTab:
		e.tab(shift)
	case key.NameDeleteBackward:
		e.deleteBackward(word, toLine)
	case key.NameDeleteForward:
		e.deleteForward(word, toLine)
	case key.NameLeftArrow:
		if mods == key.ModShortcut && e.doc.Len() == 0 {
			e.emit(EvNavBack) // onCmdLeftWhenEmpty: triggerNavigateBack
			return
		}
		e.moveH(-1, word, toLine, shift)
	case key.NameRightArrow:
		if mods == key.ModShortcut && e.doc.Len() == 0 {
			e.emit(EvNavForward) // onCmdRightWhenEmpty: triggerNavigateForward
			return
		}
		if mods == 0 && e.leaveInlineCode() {
			return
		}
		e.moveH(1, word, toLine, shift)
	case key.NameUpArrow:
		if mods == 0 && e.leaveCodeBlockUp() {
			return
		}
		if (mods == 0 || mods == key.ModShortcut) && e.upArrow(mods == key.ModShortcut) {
			return
		}
		if toLine {
			e.moveTo(0, shift)
			return
		}
		e.moveV(-1, shift)
		keepX = true
	case key.NameDownArrow:
		if mods == 0 && e.doc.Len() == 0 {
			// onDownArrowWhenEmpty's binding: {key: 40, handler: () =>
			// (1 === getLength() && r(), !0)}, then the default
			e.emit(EvDownEmpty)
		}
		if mods == 0 && e.leaveCodeBlockDown() {
			return
		}
		if toLine {
			e.moveTo(e.doc.Len(), shift)
			return
		}
		e.moveV(1, shift)
		keepX = true
	case key.NamePageUp:
		if e.PageKeysHandled && mods == 0 {
			return
		}
		e.movePage(-1, shift)
		keepX = true
	case key.NamePageDown:
		if e.PageKeysHandled && mods == 0 {
			return
		}
		e.movePage(1, shift)
		keepX = true
	case key.NameHome:
		// onHome / onEnd: on macOS Home and End go to the start and end
		// of the whole message; elsewhere the browser's line motion
		if short || edMacHomeEnd {
			e.moveTo(0, shift)
		} else {
			e.moveTo(e.visualLineStart(e.caret), shift)
		}
	case key.NameEnd:
		if short || edMacHomeEnd {
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
	case "F":
		// the keyboard module's "convert markdown" (Mod+Shift+F): typed
		// markdown in the message or the selection becomes formatting
		if !shift || alt {
			return false
		}
		return e.ConvertMarkdown()
	case "I":
		e.ToggleStyle(Italic)
	case "K":
		// SlackKeyboard {key:"K", shortKey:true}: with a selection,
		// onCmdKWhenSelected; the binding returns true, so the jumper
		// opens as well (the composer opens it)
		if shift || alt || !e.CmdKCoachmark || e.anchor == e.caret {
			return false
		}
		e.emit(EvCmdKSelected)
	case "O":
		if shift || alt {
			return false
		}
		e.emit(EvPlusMenu) // "file upload": onPlusMenuShortcut (isDesktopApp)
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
		// Shift: paste and match style (plain text), like Slack.
		e.plainPaste = shift
		e.richRead = nil
		if !shift {
			e.richRead = startClipHTML()
		}
		gtx.Execute(clipboard.ReadCmd{Tag: e})
		e.emit(EvPasted)
	case "Z":
		switch {
		case shift:
			e.Redo()
		case e.doc.Len() == 0: // quill.getLength() === 1: nothing at all
			e.emit(EvUndoEmpty) // Slack's onCmdZWhenEmpty: unsend the message just sent
		default:
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
	case "9", "(", ".", ">":
		if !shift {
			return false
		}
		e.ToggleBlock(Quote) // "blockquote", and "blockquote-alt" on Mod+Shift+.
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
	txt := sl.copyText()
	e.clipText, e.clipDoc = txt, sl
	gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(txt))})
	return true
}

// enter is Enter with mods, the way Slack's composer takes it (Texty's
// keyboard module, gantry client-boot-shared, CDN build bv1-13):
//
//   - Quill's "list empty enter" binding comes first (Keyboard.DEFAULTS;
//     getSlackKeyboardModule lodash-assigns Slack's bindings after it): a
//     bare Enter with a collapsed caret on an empty list item drops the
//     list format and the indent.
//   - Then Slack's "enter" bindings call on_enter_onEnter: a newline when
//     shouldInsertNewline(mods) (xrjr, insertsNewline) says so, else
//     MessageInput.onEnter, which sends (and never asks for a newline).
//     There is no list case: Enter in a list item with text sends, and
//     so does Enter on an empty quote line.
//
// Mod+Shift+Enter with snippets on (the snippet dialog) is caught before
// this, in command.
func (e *Editor) enter(mods key.Modifiers) {
	meta := mods.Contain(key.ModCommand) || mods.Contain(key.ModSuper)
	ctrl := mods.Contain(key.ModCtrl)
	shift := mods.Contain(key.ModShift)
	alt := mods.Contain(key.ModAlt)
	if !meta && !ctrl && !shift && !alt && e.anchor == e.caret {
		d := e.doc
		line := d.LineOf(e.caret)
		if ln := &d.Lines[line]; (ln.Kind == Bullet || ln.Kind == Ordered) && d.LineStart(line) == d.LineEnd(line) {
			e.begin(edOther, true)
			*ln = Line{}
			e.commit()
			return
		}
	}
	p := uipref.Get()
	if insertsNewline(meta, ctrl, shift, alt, edMac, p.SendOnModEnter, p.EnterIsSpecial, e.inCode()) {
		e.newline()
		return
	}
	e.emit(EvSubmit)
}

// insertsNewline is Slack's shouldInsertNewline (xrjr) as TextyInput calls
// it: newlinePref is Advanced > "When writing a message, press Enter
// to… Start a new line" (enterCreatesNewlinePref, msg_input_send_btn),
// preNewlines is "When typing code with ```, Enter should not send the
// message" (enter_is_special_in_tbt) and inPre isCursorInPreBlock. On
// macOS Ctrl+Enter always adds a line and ⌘+Enter always sends.
func insertsNewline(meta, ctrl, shift, alt, mac, newlinePref, preNewlines, inPre bool) bool {
	switch {
	case meta:
		return false
	case ctrl:
		return mac || !shift && !newlinePref
	case newlinePref:
		return true
	case preNewlines && inPre:
		return !shift
	}
	return shift || alt
}

// edMac is the platform test Slack's keyboard module makes (isMac, +oeN cX).
var edMac = runtime.GOOS == "darwin"

// inCode reports whether the caret is in code being typed with ```: a
// code block line, or after an unclosed ``` in the text.
func (e *Editor) inCode() bool {
	d := e.doc
	if d.Lines[d.LineOf(e.caret)].Kind == CodeBlock {
		return true
	}
	return strings.Count(d.String(0, e.caret), "```")%2 == 1
}

// edMacHomeEnd is onHome/onEnd's isMac: Home and End go to the start and
// end of the message.
var edMacHomeEnd = runtime.GOOS == "darwin"

// leaveInlineCode is "inline code right arrow": Right with the caret at
// the end of a line that ends in inline code types a space without the
// code format (keeping the run's other formats) and steps past it.
func (e *Editor) leaveInlineCode() bool {
	d := e.doc
	c := e.caret
	if e.anchor != c || c == 0 {
		return false
	}
	line := d.LineOf(c)
	if c != d.LineEnd(line) || c == d.LineStart(line) || d.Lines[line].Kind == CodeBlock {
		return false
	}
	prev := d.Attrs[c-1]
	if prev.Atom != nil || prev.Style&Code == 0 {
		return false
	}
	e.begin(edOther, true)
	pos := d.Insert(c, " ", Attr{Style: prev.Style &^ Code})
	e.anchor, e.caret = pos, pos
	e.commit()
	return true
}

// leaveCodeBlockDown is "block code down arrow": Down at the very end of
// the message, on a code block line with text, adds a plain line below
// and moves onto it.
func (e *Editor) leaveCodeBlockDown() bool {
	d := e.doc
	c := e.caret
	if e.anchor != c || c != d.Len() {
		return false
	}
	line := d.LineOf(c)
	if d.Lines[line].Kind != CodeBlock || d.LineStart(line) == c {
		return false
	}
	e.begin(edOther, true)
	pos := d.Insert(c, "\n", Attr{})
	d.Lines[line+1] = Line{}
	e.anchor, e.caret = pos, pos
	e.commit()
	return true
}

// leaveCodeBlockUp is the keyboard module's "up arrow out of code block"
// (boot-shared KT9Q, WYSIWYG only): Up with the caret collapsed at the
// very start of the message, on a code block that is the first block
// (getLine(index − 1) has no prev and its parent is the scroll, prefix
// empty, index 0), inserts " \n" at 0 with code-block off — a plain line
// holding one space above the block — and puts the caret at 0. The
// binding then lets Up go on, which moves nowhere from there.
func (e *Editor) leaveCodeBlockUp() bool {
	d := e.doc
	if e.plainText || e.anchor != e.caret || e.caret != 0 || d.Lines[0].Kind != CodeBlock {
		return false
	}
	e.begin(edOther, true)
	d.Insert(0, " \n", Attr{})
	d.Lines[0] = Line{}
	e.anchor, e.caret = 0, 0
	e.commit()
	return true
}

// escapeClearsFormat is the keyboard module's Escape condition (Escape
// binding, useWysiwyg): a collapsed caret on an empty quote line, or on a
// code block line that is empty or has the caret at its start after the
// block's first line.
func (e *Editor) escapeClearsFormat() bool {
	if e.plainText || e.anchor != e.caret {
		return false
	}
	d := e.doc
	line := d.LineOf(e.caret)
	ls, le := d.LineStart(line), d.LineEnd(line)
	switch d.Lines[line].Kind {
	case Quote:
		return ls == le
	case CodeBlock:
		return ls == le || e.caret == ls && line > 0 && d.Lines[line-1].Kind == CodeBlock
	}
	return false
}

// clearLineFormat is removeFormat at the caret: the line goes back to a
// paragraph.
func (e *Editor) clearLineFormat() {
	line := e.doc.LineOf(e.caret)
	e.begin(edOther, true)
	e.doc.Lines[line] = Line{}
	e.commit()
}

// newline splits the line at the caret; the new line keeps the block
// kind. On an empty list item or quote line it leaves the block instead
// (a nested list item outdents first).
func (e *Editor) newline() {
	d := e.doc
	lo, hi := e.sel()
	collapsed := lo == hi
	e.begin(edOther, true)
	d.Delete(lo, hi)
	line := d.LineOf(lo)
	ln := &d.Lines[line]
	ls, le := d.LineStart(line), d.LineEnd(line)
	switch {
	case collapsed && (ln.Kind == Bullet || ln.Kind == Ordered) && ls == le:
		// formatLine(index, 1, {list: false, indent: 0})
		*ln = Line{}
		e.anchor, e.caret = lo, lo
	case collapsed && e.secondEmptyBlockLine(line, lo):
		// the caret's line and the empty one above it (two "\n" of the
		// block before the caret) become one plain line
		prev := d.LineStart(line - 1)
		d.Delete(prev, ls)
		d.Lines[line-1] = Line{}
		e.anchor, e.caret = prev, prev
	default:
		if !e.plainText && collapsed {
			autoLinkBefore(d, lo) // SlackLinkModule.onEnter: maybeLinkify
		}
		pos := d.Insert(lo, "\n", Attr{})
		// Headings and dividers don't continue: the new line is a
		// paragraph; at a heading's start the new empty line above it is
		// (a.format.header && 0 === a.offset: removeFormat on it).
		if k := d.Lines[line].Kind; isHeading(k) && lo == ls {
			d.Lines[line] = Line{}
			d.Lines[line+1].Kind = k
		} else if isHeading(k) || k == Divider {
			d.Lines[line+1] = Line{}
		}
		e.anchor, e.caret = pos, pos
	}
	e.lastSpace = true
	e.commit()
}

// secondEmptyBlockLine is the newline branch's block exit for quotes and
// code blocks: the caret at the start of a quote line that is empty, or
// of a code line holding only whitespace, whose previous line is an empty
// line of the same block and the line before that the same block too
// (getContents(index-2, 2) is one "\n\n" op with blockquote or
// code-block).
func (e *Editor) secondEmptyBlockLine(line, pos int) bool {
	d := e.doc
	if line < 2 || pos != d.LineStart(line) {
		return false
	}
	k := d.Lines[line].Kind
	switch k {
	case Quote:
		if d.LineEnd(line) != pos {
			return false
		}
	case CodeBlock:
		for i := pos; i < d.LineEnd(line); i++ {
			if d.Attrs[i].Atom != nil || !unicode.IsSpace(d.Runes[i]) {
				return false
			}
		}
	default:
		return false
	}
	return d.Lines[line-1].Kind == k && d.LineStart(line-1) == d.LineEnd(line-1) && d.Lines[line-2].Kind == k
}

// tab indents (or with shift outdents) the selected list lines; in a code
// block it types a tab. Slack's "indent"/"outdent" bindings only act on a
// selection or with the caret at the start of the item (a collapsed
// caret elsewhere falls through to the browser).
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
	if lo, hi := e.sel(); list && lo == hi && lo != d.LineStart(d.LineOf(lo)) {
		return
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
	if ln := &d.Lines[line]; c == ls && ln.Kind != Para {
		switch {
		case (ln.Kind == Bullet || ln.Kind == Ordered) && ln.Indent > 0:
			// "outdent backspace": a nested item outdents a level first
			e.begin(edOther, true)
			ln.Indent--
			e.commit()
			return
		case ln.Kind == CodeBlock && ls == d.LineEnd(line) && ln.Quoted:
			// "backspace empty code block with border": format border "0"
			// (the line stays code; the next Backspace takes that)
			e.begin(edOther, true)
			ln.Quoted = false
			e.commit()
			return
		case ln.Kind == CodeBlock && ls != d.LineEnd(line):
			// a code line with text: "backspace empty code block" only
			// takes empty ones; this one joins the line above
		default:
			e.begin(edOther, true)
			d.Lines[line] = Line{}
			e.commit()
			return
		}
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

// upArrow is MessageInput.onUpArrow: shouldUpArrowMoveFocusToList, else
// shouldUpArrowTriggerEdit (Slack, boot-shared, build a84662d2d135). With
// the Accessibility choice LIST (1) Up at the start of the first line
// focuses the message list, LIST_IF_EMPTY (2) only in an empty field;
// unset, up_to_browse_kb_shortcut does what LIST does. Up in an empty
// field edits your last message with EDIT (3), or unset without
// up_to_browse; Mod+Up in an empty field always does. It reports whether
// it handled the key.
func (e *Editor) upArrow(cmd bool) bool {
	// Slack's Up binding skips code blocks (format: {"code-block": false})
	// and "empty" is texty.isEmpty: nothing at all, spaces count
	if e.doc.Lines[e.doc.LineOf(e.caret)].Kind == CodeBlock {
		return false
	}
	empty := e.doc.Len() == 0
	start := e.caret == 0 && e.anchor == 0
	var list bool
	switch e.upMode {
	case 1:
		list = start
	case 2:
		list = empty
	case 0:
		list = e.upBrowse && (empty || start)
	}
	if !cmd && list {
		e.emit(EvFocusList)
		return true
	}
	if empty && (cmd || e.upMode == 3 || e.upMode == 0 && !e.upBrowse) {
		e.emit(EvEditLast)
		return true
	}
	return false
}
