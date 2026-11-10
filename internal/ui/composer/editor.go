package composer

import (
	"image"
	"image/color"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"gioui.org/f32"
	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/io/semantic"
	"gioui.org/io/system"
	"gioui.org/io/transfer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/emoji"
	"github.com/some-du6e/clack/internal/ui/refocus"
)

// Editor is the composer's WYSIWYG rich text editor over a *Doc. Input
// handling mirrors gioui.org/widget.Editor (focus, key filters, IME
// snippets and selection, clipboard, caret blinking); layout and painting
// live in textlayout.go, key commands in editor_keys.go and live markdown
// in autoformat.go.

// EditorStyle is the look of the editor. Sizes are scaled by the metric.
type EditorStyle struct {
	Shaper                                               *text.Shaper
	TextSize, LineHeight                                 unit.Sp // 15, 22
	Color, Placeholder, Selection, Caret                 color.NRGBA
	LinkColor                                            color.NRGBA
	CodeColor, CodeBg, CodeBorder                        color.NRGBA // inline code
	PreBg, PreBorder                                     color.NRGBA // code block
	QuoteBar                                             color.NRGBA
	MentionColor, MentionBg, BroadcastColor, BroadcastBg color.NRGBA
	PlaceholderText                                      string
	MaxHeight                                            unit.Dp
	EmojiSize                                            unit.Dp
	// Embolden strokes text glyphs this many pixels wider (0 = off), to
	// match richview's text where the editor replaces it (canvas blocks).
	Embolden float32
	// Images returns an emoji image scaled for px pixels, if loaded.
	Images func(url string, px int) (paint.ImageOp, bool)
}

// DefaultEditorStyle returns Slack's light composer look.
func DefaultEditorStyle(sh *text.Shaper) EditorStyle {
	return EditorStyle{
		Shaper:          sh,
		TextSize:        15,
		LineHeight:      22,
		Color:           rgb(0x1d1c1d),
		Placeholder:     rgb(0x868686),
		Selection:       argb(0x661d9bd1),
		Caret:           rgb(0x1d1c1d),
		LinkColor:       rgb(0x1264a3),
		CodeColor:       rgb(0xe01e5a),
		CodeBg:          rgb(0xf8f8f8),
		CodeBorder:      rgb(0xdddddd),
		PreBg:           rgb(0xf8f8f8),
		PreBorder:       rgb(0xdddddd),
		QuoteBar:        rgb(0xdddddd),
		MentionColor:    rgb(0x1264a3),
		MentionBg:       argb(0x1a1d9bd1),
		BroadcastColor:  rgb(0x1d1c1d),
		BroadcastBg:     argb(0x66f2c744),
		PlaceholderText: "Message",
		MaxHeight:       300,
		EmojiSize:       22,
	}
}

// EditorEventKind is the kind of an EditorEvent.
type EditorEventKind uint8

const (
	EvChange     EditorEventKind = iota // content changed (Version moved)
	EvSubmit                            // Enter: send
	EvEditLast                          // Up in an empty editor: edit the last message
	EvCancel                            // Escape
	EvLinkDialog                        // Ctrl+Shift+U: open the link dialog
	EvPasted                            // Ctrl+V pressed (text arrives async); probe the clipboard for an image
	EvFocus                             // gained keyboard focus
	EvBlur                              // lost keyboard focus
	EvUndoEmpty                         // Mod+Z in an empty editor: unsend, else undo (the composer decides)
	EvFocusList                         // Up: move focus to the message list (UpArrowShortcut)
)

// EditorEvent is reported by Editor.Update.
type EditorEvent struct{ Kind EditorEventKind }

type edEditKind uint8

const (
	edNone edEditKind = iota
	edTyping
	edDeleting
	edOther
)

const (
	edUndoCap          = 200
	edBlinksPerSecond  = 1
	edMaxBlinkDuration = 10 * time.Second
)

type edSnap struct {
	doc           *Doc
	anchor, caret int
}

type edDragMode uint8

const (
	edDragChar edDragMode = iota
	edDragWord
	edDragLine
)

// Editor is a rich text editor widget. Create it with NewEditor (the zero
// value also works).
type Editor struct {
	missed []event.Event // key events KeepFocus read while it wasn't drawn (editor_focus.go)
	// Intercept, if set, is offered Enter, Tab, Up, Down, Escape (with
	// modifiers) before the editor handles them; returning true consumes
	// the key (used by the autocomplete popup).
	Intercept func(name key.Name, mods key.Modifiers) bool

	doc           *Doc
	anchor, caret int
	version       uint64

	// pending is the style typed text gets at pendingPos (toggled with a
	// collapsed caret); valid only while the caret and content stay put.
	pendingOK    bool
	pendingStyle Style
	pendingPos   int
	pendingVer   uint64

	undo, redo []edSnap
	lastKind   edEditKind
	lastCaret  int
	lastVer    uint64
	lastSpace  bool

	clipText   string
	clipDoc    *Doc
	slugs      edSlugs // pasted Slack links being resolved (slugs.go)
	plainPaste bool
	richRead   *clipRead     // HTML read started by Ctrl/Cmd+V
	pendPaste  *pendingPaste // plain text waiting for richRead

	events  []EditorEvent
	filters []event.Filter

	// Input state.
	clicker     gesture.Click
	dragger     gesture.Drag
	scroller    gesture.Scroll
	dragging    bool
	dragMode    edDragMode
	dragLo      int
	dragHi      int
	blinkStart  time.Time
	resetBlink  bool
	scrollCaret bool
	desiredX    int
	ime         struct {
		sel         key.Range // the selection last reported with SelectionCmd
		echo        key.Range // the window's own model after its last EditEvent
		echoOK      bool
		caret       key.Caret
		bounds      image.Rectangle
		snippet     key.Snippet
		snipStart   int
		snipEnd     int
		composition key.Range
	}

	// Layout state from the last Layout call.
	st        EditorStyle
	hasStyle  bool
	metric    unit.Metric
	loc       system.Locale
	width     int
	lay       tlLayout
	scrollY   int
	viewH     int
	contentH  int
	caretRect image.Rectangle

	// emoji resolves typed ":name:" shortcodes (emoji_typed.go); nil
	// leaves them as text.
	emoji emojiLookup
	// plainText is Slack's plain_text_mode ("Format messages with
	// markup"): no live markup, the characters stay as typed.
	plainText bool
	// smart is "Enable smart quotes and symbols" (smart_symbols.go).
	smart bool
	// upMode is UpArrowShortcut's value and upBrowse
	// up_to_browse_kb_shortcut, for upArrow.
	upMode   int
	upBrowse bool
}

// NewEditor returns an empty editor.
func NewEditor() *Editor {
	e := &Editor{doc: NewDoc(), desiredX: -1}
	e.ime.composition = key.Range{Start: -1, End: -1}
	return e
}

func (e *Editor) init() {
	if e.doc == nil {
		e.doc = NewDoc()
		e.desiredX = -1
		e.ime.composition = key.Range{Start: -1, End: -1}
	}
	if e.filters == nil {
		e.filters = e.keyFilters()
	}
}

// Doc returns the document. Call Touch after mutating it directly.
func (e *Editor) Doc() *Doc { e.init(); return e.doc }

// Touch records an external mutation of Doc(): bumps the version, clamps
// the selection and reports EvChange.
func (e *Editor) Touch() {
	e.init()
	e.anchor, e.caret = e.doc.clamp(e.anchor), e.doc.clamp(e.caret)
	e.lastKind = edNone
	e.commit()
}

// SetDoc replaces the content, puts the caret at the end and clears the
// undo history. It bumps Version but does not report EvChange.
func (e *Editor) SetDoc(d *Doc) {
	e.init()
	if d == nil {
		d = NewDoc()
	}
	if len(d.Lines) == 0 {
		d.Lines = []Line{{}}
	}
	e.doc = d
	e.anchor, e.caret = d.Len(), d.Len()
	e.undo, e.redo = nil, nil
	e.lastKind = edNone
	e.pendingOK = false
	e.version++
	e.lastVer = e.version
	e.scrollCaret = true
	e.desiredX = -1
}

// Selection returns the anchor and the caret; start may be > end.
func (e *Editor) Selection() (start, end int) { return e.anchor, e.caret }

// SetSelection sets the anchor and the caret.
func (e *Editor) SetSelection(anchor, caret int) {
	e.init()
	e.anchor, e.caret = e.doc.clamp(anchor), e.doc.clamp(caret)
	e.pendingOK = false
	e.scrollCaret = true
	e.resetBlink = true
	e.desiredX = -1
}

// sel returns the selection ordered.
func (e *Editor) sel() (lo, hi int) {
	if e.anchor <= e.caret {
		return e.anchor, e.caret
	}
	return e.caret, e.anchor
}

// Focus requests keyboard focus.
func (e *Editor) Focus(gtx layout.Context) {
	e.init()
	gtx.Execute(key.FocusCmd{Tag: e})
	gtx.Execute(key.SoftKeyboardCmd{Show: true})
}

// Blur gives up keyboard focus if the editor has it.
func (e *Editor) Blur(gtx layout.Context) {
	if gtx.Focused(e) {
		gtx.Execute(key.FocusCmd{Tag: nil})
	}
}

// Focused reports whether the editor has keyboard focus.
func (e *Editor) Focused(gtx layout.Context) bool { return gtx.Focused(e) }

// Version increments on every content change.
func (e *Editor) Version() uint64 { return e.version }

// Empty reports whether the document has no visible content.
func (e *Editor) Empty() bool { e.init(); return e.doc.Empty() }

// CaretRect is the caret rectangle in the editor's local pixel coordinates
// as of the last Layout.
func (e *Editor) CaretRect() image.Rectangle { return e.caretRect }

// ContentHeight is the unscrolled height of the content from the last
// Layout, in pixels.
func (e *Editor) ContentHeight() int { return e.contentH }

// typingStyle is the style text typed at pos gets.
func (e *Editor) typingStyle(pos int) Style {
	if e.pendingValid() && pos == e.caret {
		return e.pendingStyle
	}
	return e.doc.StyleAt(pos)
}

func (e *Editor) pendingValid() bool {
	return e.pendingOK && e.anchor == e.caret && e.caret == e.pendingPos && e.pendingVer == e.version
}

func (e *Editor) setPending(s Style) {
	e.pendingOK = true
	e.pendingStyle = s
	e.pendingPos = e.caret
	e.pendingVer = e.version
}

// typingAttr is the attribute for text replacing [lo, hi).
func (e *Editor) typingAttr(lo, hi int) Attr {
	d := e.doc
	if hi > lo {
		for i := lo; i < hi; i++ {
			if d.Attrs[i].Atom == nil && d.Runes[i] != '\n' {
				return Attr{Style: d.Attrs[i].Style}
			}
		}
		return Attr{Style: d.StyleAt(lo)}
	}
	a := Attr{Style: e.typingStyle(lo)}
	// Typing inside a link extends it; typing at its edges doesn't.
	if lo > 0 && lo < d.Len() && d.Attrs[lo-1].Link != "" && d.Attrs[lo].Link == d.Attrs[lo-1].Link {
		a.Link = d.Attrs[lo].Link
	}
	return a
}

// ActiveStyle returns the styles at the caret (including a pending
// toggle), or the styles common to the whole selection.
func (e *Editor) ActiveStyle() Style {
	e.init()
	lo, hi := e.sel()
	if lo == hi {
		return e.typingStyle(e.caret)
	}
	var s Style
	for _, b := range []Style{Bold, Italic, Underline, Strike, Code} {
		if e.doc.HasStyle(lo, hi, b) {
			s |= b
		}
	}
	return s
}

// ActiveBlock returns the block kind of the caret's line.
func (e *Editor) ActiveBlock() BlockKind {
	e.init()
	return e.doc.Lines[min(e.doc.LineOf(e.caret), len(e.doc.Lines)-1)].Kind
}

// ActiveLink reports whether the caret is in a link, or the whole
// selection is linked.
func (e *Editor) ActiveLink() bool {
	e.init()
	lo, hi := e.sel()
	if lo == hi {
		_, _, u := e.doc.LinkAt(lo)
		return u != ""
	}
	found := false
	for i := lo; i < hi; i++ {
		if e.doc.Runes[i] == '\n' || e.doc.Attrs[i].Atom != nil {
			continue
		}
		if e.doc.Attrs[i].Link == "" {
			return false
		}
		found = true
	}
	return found
}

// LinkAt returns the link around the caret (for prefilling the link
// dialog), or the selection and "" if there is none.
func (e *Editor) LinkAt() (start, end int, url string) {
	e.init()
	lo, hi := e.sel()
	if lo == hi {
		if s, en, u := e.doc.LinkAt(lo); u != "" {
			return s, en, u
		}
	}
	return lo, hi, ""
}

// ToggleStyle toggles s on the selection, or as a pending style for the
// next typed text when the caret is collapsed.
func (e *Editor) ToggleStyle(s Style) {
	e.init()
	lo, hi := e.sel()
	if lo == hi {
		e.setPending(e.typingStyle(e.caret) ^ s)
		e.lastKind = edNone
		e.resetBlink = true
		return
	}
	e.begin(edOther, false)
	e.doc.ToggleStyle(lo, hi, s)
	e.commit()
}

// selLines returns the lines the selection covers; a selection ending at
// the start of a line doesn't include that line.
func (e *Editor) selLines() (l0, l1 int) {
	lo, hi := e.sel()
	l0, l1 = e.doc.LineOf(lo), e.doc.LineOf(hi)
	if hi > lo && l1 > l0 && hi == e.doc.LineStart(l1) {
		l1--
	}
	return l0, l1
}

// ToggleBlock sets the selection's lines to k, or back to paragraphs if
// they all already are k.
func (e *Editor) ToggleBlock(k BlockKind) {
	e.init()
	l0, l1 := e.selLines()
	e.begin(edOther, false)
	e.doc.ToggleLineKind(l0, l1, k)
	e.commit()
}

// SetBlock sets the selection's lines to k unconditionally (the ¶ menu picks
// Paragraph or a heading like a radio group, so it never toggles back).
func (e *Editor) SetBlock(k BlockKind) {
	e.init()
	l0, l1 := e.selLines()
	e.begin(edOther, false)
	e.doc.SetLineKind(l0, l1, k)
	e.commit()
}

// InsertDivider inserts a divider on its own line at the caret and leaves the
// caret on a fresh paragraph line below it, like Slack's divider button.
func (e *Editor) InsertDivider() {
	e.init()
	d := e.doc
	lo, hi := e.sel()
	e.begin(edOther, true)
	d.Delete(lo, hi)
	line := d.LineOf(lo)
	if d.LineStart(line) == d.LineEnd(line) {
		// Reuse the empty caret line as the divider; add a paragraph after.
		d.Lines[line] = Line{Kind: Divider}
		pos := d.Insert(lo, "\n", Attr{})
		d.Lines[line+1] = Line{}
		e.anchor, e.caret = pos, pos
	} else {
		// Split the line: the divider sits on its own line between the two
		// halves, and the caret moves onto the (tail) paragraph below.
		pos := d.Insert(lo, "\n\n", Attr{})
		d.Lines[line+1] = Line{Kind: Divider}
		d.Lines[line+2] = Line{}
		e.anchor, e.caret = pos, pos
	}
	e.lastSpace = true
	e.commit()
}

// SetLink replaces the selection with text linked to url, or links the
// selection if text is the selected text. With a collapsed caret inside a
// link, that link is edited. An empty url removes the link; an empty text
// uses the url as text.
func (e *Editor) SetLink(txt, url string) {
	e.init()
	d := e.doc
	lo, hi, _ := e.LinkAt()
	e.begin(edOther, false)
	if url == "" {
		d.SetLink(lo, hi, "")
		e.anchor, e.caret = hi, hi
		e.commit()
		return
	}
	if txt == "" {
		txt = url
	}
	end := hi
	if hi > lo && (txt == d.String(lo, hi) || txt == d.Slice(lo, hi).Text()) {
		d.SetLink(lo, hi, url)
	} else {
		a := e.typingAttr(lo, hi)
		d.Delete(lo, hi)
		end = d.Insert(lo, txt, Attr{Style: a.Style})
		d.SetLink(lo, end, url)
	}
	e.anchor, e.caret = end, end
	e.commit()
}

// SelectedText returns the selection as plain text.
func (e *Editor) SelectedText() string {
	e.init()
	lo, hi := e.sel()
	return e.doc.Slice(lo, hi).Text()
}

// InsertText replaces the selection with s (no autoformat) and puts the
// caret after it.
func (e *Editor) InsertText(s string) {
	e.init()
	lo, hi := e.sel()
	e.begin(edOther, false)
	a := e.typingAttr(lo, hi)
	e.doc.Delete(lo, hi)
	end := e.doc.Insert(lo, s, a)
	e.anchor, e.caret = end, end
	e.commit()
}

// InsertAtom replaces [start, end) (e.g. the "@ka" trigger text) with the
// atom plus a trailing space and puts the caret after it.
func (e *Editor) InsertAtom(start, end int, a *Atom) {
	e.init()
	if a == nil {
		return
	}
	d := e.doc
	start, end = d.clamp(start), d.clamp(end)
	if start > end {
		start, end = end, start
	}
	e.begin(edOther, false)
	d.Delete(start, end)
	pos := d.InsertAtom(start, a)
	pos = d.Insert(pos, " ", Attr{})
	e.anchor, e.caret = pos, pos
	e.commit()
}

// Replace replaces [start, end) with s and puts the caret after it.
func (e *Editor) Replace(start, end int, s string) {
	e.init()
	d := e.doc
	start, end = d.clamp(start), d.clamp(end)
	if start > end {
		start, end = end, start
	}
	e.begin(edOther, false)
	a := Attr{Style: d.StyleAt(start)}
	d.Delete(start, end)
	pos := d.Insert(start, s, a)
	e.anchor, e.caret = pos, pos
	e.commit()
}

// begin records an undo step before an edit of kind k. Consecutive typing
// (or deleting) at the same caret coalesces into one step unless brk.
func (e *Editor) begin(k edEditKind, brk bool) {
	lo, hi := e.sel()
	coalesce := !brk && k != edOther && k == e.lastKind && lo == hi &&
		e.caret == e.lastCaret && e.lastVer == e.version
	if !coalesce {
		e.pushUndo(e.snap())
	}
	e.redo = e.redo[:0]
	e.lastKind = k
}

func (e *Editor) snap() edSnap {
	return edSnap{doc: e.doc.Clone(), anchor: e.anchor, caret: e.caret}
}

func (e *Editor) pushUndo(s edSnap) {
	if len(e.undo) >= edUndoCap {
		n := copy(e.undo, e.undo[1:])
		e.undo = e.undo[:n]
	}
	e.undo = append(e.undo, s)
}

// commit finishes a content change.
func (e *Editor) commit() {
	e.version++
	e.lastCaret = e.caret
	e.lastVer = e.version
	e.scrollCaret = true
	e.resetBlink = true
	e.desiredX = -1
	e.emit(EvChange)
}

// Undo restores the previous undo step.
func (e *Editor) Undo() {
	e.init()
	if len(e.undo) == 0 {
		return
	}
	s := e.undo[len(e.undo)-1]
	e.undo = e.undo[:len(e.undo)-1]
	e.redo = append(e.redo, e.snap())
	e.restore(s)
}

// Redo re-applies an undone step.
func (e *Editor) Redo() {
	e.init()
	if len(e.redo) == 0 {
		return
	}
	s := e.redo[len(e.redo)-1]
	e.redo = e.redo[:len(e.redo)-1]
	e.pushUndo(e.snap())
	e.restore(s)
}

func (e *Editor) restore(s edSnap) {
	e.doc = s.doc
	e.anchor, e.caret = e.doc.clamp(s.anchor), e.doc.clamp(s.caret)
	e.lastKind = edNone
	e.pendingOK = false
	e.commit()
}

func (e *Editor) emit(k EditorEventKind) {
	if k == EvChange && len(e.events) > 0 && e.events[len(e.events)-1].Kind == EvChange {
		return
	}
	e.events = append(e.events, EditorEvent{Kind: k})
}

func (e *Editor) pop() (EditorEvent, bool) {
	if len(e.events) == 0 {
		return EditorEvent{}, false
	}
	ev := e.events[0]
	e.events = e.events[:copy(e.events, e.events[1:])]
	return ev, true
}

// typeAt replaces [lo, hi) with typed text s, applying the pending style
// and live markdown.
func (e *Editor) typeAt(lo, hi int, s string) {
	d := e.doc
	lo, hi = d.clamp(lo), d.clamp(hi)
	if lo > hi {
		lo, hi = hi, lo
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	if s == "" && lo == hi {
		return
	}
	first, _ := utf8.DecodeRuneInString(s)
	last, _ := utf8.DecodeLastRuneInString(s)
	brk := e.lastSpace && !edSpace(first) || strings.ContainsRune(s, '\n')
	a := e.typingAttr(lo, hi)
	if lo != e.caret || hi != e.caret {
		// Typing elsewhere than the caret (IME replacement, selection).
		brk = true
	}
	e.begin(edTyping, brk)
	// A divider holds no text; typing on it turns it back into a paragraph.
	if ln := d.LineOf(lo); ln < len(d.Lines) && d.Lines[ln].Kind == Divider {
		d.Lines[ln] = Line{}
	}
	d.Delete(lo, hi)
	end := d.Insert(lo, s, a)
	e.anchor, e.caret = end, end
	e.lastSpace = edSpace(last) || last == '\n'
	e.pendingOK = false
	e.commit()
	if utf8.RuneCountInString(s) == 1 && !emoji.MayHaveEmoji(s) {
		if e.smartSymbols() {
			return
		}
		e.autoformat()
	} else if !e.composing() {
		// Several runes in one input event (an IME commit, a fast
		// batch, an emoji from the OS picker): shortcodes they close
		// and the unicode emoji they hold still convert.
		e.convertEmoji(lo, end)
	}
}

// composing reports whether an IME composition is open (Slack leaves
// emoji alone until it is committed).
func (e *Editor) composing() bool {
	c := e.ime.composition
	return c.Start >= 0 && c.End > c.Start
}

// convertEmoji converts the shortcodes whose closing ':' lies in
// [from, to), keeping the literal text as its own undo step. The caret
// and anchor follow the edits.
func (e *Editor) convertEmoji(from, to int) bool {
	if e.emoji == nil {
		return false
	}
	literal := e.snap()
	eds := convertEmojiRange(e.doc, from, to, e.emoji)
	for _, ed := range eds {
		to = ed.mapPos(to)
	}
	eds = append(eds, convertNativeEmojiRange(e.doc, from, to, e.emoji)...)
	if len(eds) == 0 {
		return false
	}
	for _, ed := range eds {
		e.anchor, e.caret = ed.mapPos(e.anchor), ed.mapPos(e.caret)
	}
	e.pushUndo(literal)
	e.redo = e.redo[:0]
	e.lastKind = edOther
	e.commit()
	return true
}

// ConvertEmoji converts every complete shortcode in the document (the
// custom emoji set landed after they were typed). It reports a change.
func (e *Editor) ConvertEmoji() bool {
	e.init()
	if !hasShortcodeColon(e.doc) {
		return false
	}
	return e.convertEmoji(0, e.doc.Len())
}

// autoformat applies live markdown after a typed rune, keeping the literal
// text as its own undo step.
func (e *Editor) autoformat() {
	c := e.caret
	if c == 0 {
		return
	}
	r := e.doc.Runes[c-1]
	if r == ':' && e.emoji != nil {
		literal := e.snap()
		if ed, ok := convertEmojiAt(e.doc, c-1, e.emoji); ok {
			e.pushUndo(literal)
			e.anchor, e.caret = ed.mapPos(e.anchor), ed.mapPos(e.caret)
			e.lastKind = edOther
			e.commit()
		}
		return
	}
	if r == ' ' && e.convertEmoticon(c-1) { // convert_emoticons (emoticon.go)
		return
	}
	if e.plainText {
		return // "Format messages with markup": markup stays as typed
	}
	custom := InlineMarkup != nil
	if r != ' ' && r != '`' && r != '*' && r != '_' && r != '~' && !(custom && markupTrigger(r)) {
		return
	}
	literal := e.snap()
	if r == ' ' || r == '`' {
		if nc, _, ok := autoformatBlock(e.doc, c); ok {
			e.pushUndo(literal)
			e.anchor, e.caret = nc, nc
			e.lastKind = edOther
			e.commit()
			return
		}
	}
	if r == '`' {
		if nc, ok := autoformatFence(e.doc, c); ok {
			e.pushUndo(literal)
			e.anchor, e.caret = nc, nc
			e.lastKind = edOther
			e.commit()
			e.setPending(0)
			return
		}
	}
	if r == ' ' {
		return
	}
	inline := autoformatInline
	if custom {
		inline = markupInline // a plugin's markup replaces Slack's
	}
	if nc, st, ok := inline(e.doc, c); ok {
		e.pushUndo(literal)
		e.anchor, e.caret = nc, nc
		e.lastKind = edOther
		e.commit()
		e.setPending(e.doc.StyleAt(nc) &^ st)
	}
}

// paste inserts clipboard text: the rich copy if it is ours, a link on the
// selection if it is a single URL, plain text otherwise.
func (e *Editor) paste(s string) { e.pasteHTML(s, "") }

// pasteHTML is paste with the clipboard's HTML, if any: the rich copy if
// it is ours, a link on the selection if the text is a single URL, the
// HTML's formatting, a list if the text looks like one, else plain text.
// Code blocks and inline code take plain text, as does Shift (paste and
// match style).
func (e *Editor) pasteHTML(s, html string) {
	e.init()
	// Under "Format messages with markup" the composer shows no
	// formatting, so pastes go in as text like typing does.
	plain := e.plainPaste || e.plainText
	e.plainPaste = false
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if s == "" {
		return
	}
	d := e.doc
	lo, hi := e.sel()
	if u := strings.TrimSpace(s); hi > lo && edIsURL(u) {
		e.begin(edOther, false)
		d.SetLink(lo, hi, transformLink(u))
		e.anchor, e.caret = hi, hi
		e.commit()
		return
	}
	e.begin(edOther, false)
	a := e.typingAttr(lo, hi)
	d.Delete(lo, hi)
	var end int
	line := d.LineOf(lo)
	own := !plain && e.clipDoc != nil && s == strings.ReplaceAll(e.clipText, "\r\n", "\n")
	var rich *Doc
	if !plain && !own && a.Style&Code == 0 && d.Lines[line].Kind != CodeBlock {
		if html != "" {
			rich = docFromHTML(html)
		}
		if rich == nil {
			rich = docFromPlainText(s, lo == d.LineStart(line))
		}
	}
	switch {
	case own:
		end = d.InsertDoc(lo, e.clipDoc)
	case rich != nil:
		end = e.pasteRich(lo, rich)
		if LinkTransforms != nil {
			end = transformPastedLinks(d, lo, end)
		}
	default:
		end = d.Insert(lo, s, Attr{Style: a.Style})
		if LinkTransforms != nil && a.Style&Code == 0 {
			end = transformPastedLinks(d, lo, end)
		}
	}
	e.anchor, e.caret = end, end
	e.commit()
	e.convertEmoji(lo, end) // pasted ":name:" shortcodes, like Slack
	if !plain {
		e.notePastedLinks(lo, min(e.caret, e.doc.Len())) // Slack links → slugs (slugs.go)
	}
}

// edIsURL reports whether s is a single http(s) URL token.
func edIsURL(s string) bool {
	if strings.ContainsAny(s, " \t\n") {
		return false
	}
	l := strings.ToLower(s)
	return (strings.HasPrefix(l, "http://") && len(l) > 7) || (strings.HasPrefix(l, "https://") && len(l) > 8)
}

// ensureLayout relayouts with the style from the last Layout if the
// content or geometry changed. It reports whether a layout is available.
func (e *Editor) ensureLayout() bool {
	if !e.hasStyle || e.st.Shaper == nil {
		return false
	}
	k := tlKey{
		version: e.version,
		width:   e.width,
		metric:  e.metric,
		ts:      e.st.TextSize,
		lh:      e.st.LineHeight,
		es:      e.st.EmojiSize,
		images:  e.st.Images != nil,
		loc:     e.loc,
	}
	if !e.lay.valid || e.lay.key != k || e.lay.sh != e.st.Shaper {
		e.lay.build(e.doc, &e.st, e.st.Shaper, k)
	}
	return true
}

// Update processes input and returns the next event. Call it until it
// returns false; Layout also processes (and discards) events.
func (e *Editor) Update(gtx layout.Context) (EditorEvent, bool) {
	e.init()
	if ev, ok := e.pop(); ok {
		return ev, true
	}
	e.flushPaste(gtx)
	e.processPointer(gtx)
	if len(e.events) == 0 {
		e.processKeys(gtx)
	}
	e.updateIME(gtx)
	return e.pop()
}

func (e *Editor) processPointer(gtx layout.Context) {
	sr := pointer.ScrollRange{Min: -e.scrollY, Max: max(0, e.contentH-e.viewH-e.scrollY)}
	if d := e.scroller.Update(gtx.Metric, gtx.Source, gtx.Now, gesture.Vertical, pointer.ScrollRange{}, sr); d != 0 {
		e.scrollY = min(max(e.scrollY+d, 0), max(0, e.contentH-e.viewH))
	}
	for {
		ev, ok := e.clicker.Update(gtx.Source)
		if !ok {
			break
		}
		e.onClick(gtx, ev)
	}
	for {
		ev, ok := e.dragger.Update(gtx.Metric, gtx.Source, gesture.Both)
		if !ok {
			break
		}
		e.onDrag(gtx, ev)
	}
}

// hitPos maps a point in local (viewport) coordinates to a position.
func (e *Editor) hitPos(pt image.Point) int {
	if !e.ensureLayout() {
		return e.doc.Len()
	}
	pt.Y += e.scrollY
	return e.doc.clamp(e.lay.hit(pt))
}

func (e *Editor) onClick(gtx layout.Context, ev gesture.ClickEvent) {
	if !((ev.Kind == gesture.KindPress && ev.Source == pointer.Mouse) ||
		(ev.Kind == gesture.KindClick && ev.Source != pointer.Mouse)) {
		return
	}
	gtx.Execute(key.FocusCmd{Tag: e})
	gtx.Execute(key.SoftKeyboardCmd{Show: true})
	e.blinkStart = gtx.Now
	e.desiredX = -1
	e.lastKind = edNone
	e.pendingOK = false
	if e.scroller.State() != gesture.StateFlinging {
		e.scrollCaret = true
	}
	pos := e.hitPos(ev.Position)
	d := e.doc
	switch {
	case ev.NumClicks == 2:
		lo, hi := edWordAt(d, pos)
		e.anchor, e.caret = lo, hi
		e.dragMode, e.dragLo, e.dragHi = edDragWord, lo, hi
	case ev.NumClicks >= 3:
		l := d.LineOf(pos)
		lo, hi := d.LineStart(l), d.LineEnd(l)
		e.anchor, e.caret = lo, hi
		e.dragMode, e.dragLo, e.dragHi = edDragLine, lo, hi
	default:
		if ev.Modifiers.Contain(key.ModShift) {
			e.caret = pos
		} else {
			e.anchor, e.caret = pos, pos
		}
		e.dragMode = edDragChar
	}
	e.dragging = true
}

func (e *Editor) onDrag(gtx layout.Context, ev pointer.Event) {
	if ev.Source != pointer.Mouse || !e.dragging {
		return
	}
	if ev.Kind != pointer.Drag && ev.Kind != pointer.Release {
		if ev.Kind == pointer.Cancel {
			e.dragging = false
		}
		return
	}
	e.blinkStart = gtx.Now
	pos := e.hitPos(ev.Position.Round())
	d := e.doc
	switch e.dragMode {
	case edDragWord, edDragLine:
		var lo, hi int
		if e.dragMode == edDragWord {
			lo, hi = edWordAt(d, pos)
		} else {
			l := d.LineOf(pos)
			lo, hi = d.LineStart(l), d.LineEnd(l)
		}
		if lo < e.dragLo {
			e.anchor, e.caret = e.dragHi, lo
		} else {
			e.anchor, e.caret = e.dragLo, max(hi, e.dragHi)
		}
	default:
		e.caret = pos
	}
	e.scrollCaret = true
	if ev.Kind == pointer.Release {
		e.dragging = false
	}
}

func (e *Editor) processKeys(gtx layout.Context) {
	for {
		ev, ok := e.nextKeyEvent(gtx)
		if !ok {
			break
		}
		e.blinkStart = gtx.Now
		switch ev := ev.(type) {
		case key.FocusEvent:
			e.ime.sel = key.Range{}
			e.ime.echoOK = false
			e.ime.caret = key.Caret{}
			e.ime.snippet = key.Snippet{}
			e.ime.composition = key.Range{Start: -1, End: -1}
			if ev.Focus {
				gtx.Execute(key.SoftKeyboardCmd{Show: true})
				e.emit(EvFocus)
			} else {
				e.dragging = false
				e.emit(EvBlur)
			}
		case key.CompositionEvent:
			was := e.ime.composition
			e.ime.composition = key.Range(ev)
			if was.Start >= 0 && was.End > was.Start && !e.composing() {
				// committed: its emoji convert now
				lo, hi := min(was.Start, was.End), max(was.Start, was.End)
				e.convertEmoji(lo, hi)
			}
		case key.Event:
			if !gtx.Focused(e) || ev.State != key.Press {
				break
			}
			e.scrollCaret = true
			e.scroller.Stop()
			e.command(gtx, ev)
		case key.SnippetEvent:
			e.updateSnippet(gtx, ev.Start, ev.End)
		case key.EditEvent:
			e.onEdit(ev)
		case transfer.DataEvent:
			e.scrollCaret = true
			e.scroller.Stop()
			rc := ev.Open()
			b, err := io.ReadAll(rc)
			rc.Close()
			if err == nil {
				e.receivePaste(gtx, string(b))
			}
		case key.SelectionEvent:
			if e.ime.echoOK && key.Range(ev) == e.ime.echo {
				// The window echoes its own model of an insertion; our
				// selection is authoritative (autoformat may have moved it).
				break
			}
			e.ime.echoOK = false
			e.scrollCaret = true
			e.scroller.Stop()
			e.ime.sel = key.Range(ev)
			e.caret, e.anchor = e.doc.clamp(ev.Start), e.doc.clamp(ev.End)
			e.desiredX = -1
			e.pendingOK = false
		}
		if len(e.events) > 0 {
			break
		}
	}
}

// onEdit applies an input method edit. Edits aimed at the selection the
// window believes we have (the one we last reported, or its own model
// after an insertion) go to our real selection: they differ after
// autoformat or atom edits within one frame.
func (e *Editor) onEdit(ev key.EditEvent) {
	e.scrollCaret = true
	e.scroller.Stop()
	r := edNormRange(ev.Range)
	lo, hi := e.sel()
	if r != edNormRange(e.ime.sel) && !(e.ime.echoOK && r == e.ime.echo) {
		lo, hi = r.Start, r.End
	}
	e.typeAt(lo, hi, ev.Text)
	n := r.Start + utf8.RuneCountInString(ev.Text)
	e.ime.echo = key.Range{Start: n, End: n}
	e.ime.echoOK = true
}

func edNormRange(r key.Range) key.Range {
	if r.Start > r.End {
		r.Start, r.End = r.End, r.Start
	}
	return r
}

// updateIME tells the input method about the selection and caret.
func (e *Editor) updateIME(gtx layout.Context) {
	rng := key.Range{Start: e.caret, End: e.anchor}
	var caret key.Caret
	var bounds image.Rectangle
	if e.ensureLayout() {
		x, base, asc, desc, _ := e.lay.caretInfo(e.caret)
		caret = key.Caret{
			Pos:     f32.Pt(float32(x), float32(base-e.scrollY)),
			Ascent:  float32(asc),
			Descent: float32(desc),
		}
		if c := e.ime.composition; c.Start >= 0 && c.End > c.Start {
			e.lay.forRange(c.Start, c.End, func(li, x0, x1 int, nl bool) {
				ln := &e.lay.lines[li]
				r := image.Rect(x0, ln.top-e.scrollY, x1, ln.top+ln.h-e.scrollY)
				if bounds.Empty() {
					bounds = r
				} else {
					bounds = bounds.Union(r)
				}
			})
		}
	}
	if rng != e.ime.sel || caret != e.ime.caret || bounds != e.ime.bounds {
		e.ime.sel, e.ime.caret, e.ime.bounds = rng, caret, bounds
		gtx.Execute(key.SelectionCmd{Tag: e, Range: rng, Caret: caret, CompositionBounds: bounds})
	}
	e.updateSnippet(gtx, e.ime.snipStart, e.ime.snipEnd)
}

// updateSnippet sends the text around [start, end) to the input method.
func (e *Editor) updateSnippet(gtx layout.Context, start, end int) {
	if start > end {
		start, end = end, start
	}
	start, end = e.doc.clamp(start), e.doc.clamp(end)
	e.ime.snipStart, e.ime.snipEnd = start, end
	if e.ime.snippet.Start == start && e.ime.snippet.End == end && e.ime.snippet.Text == e.doc.String(start, end) {
		return
	}
	e.ime.snippet = key.Snippet{Range: key.Range{Start: start, End: end}, Text: e.doc.String(start, end)}
	gtx.Execute(key.SnippetCmd{Tag: e, Snippet: e.ime.snippet})
}

// Layout processes input, lays out and paints the editor. It fills the
// available width and grows with the content up to MaxHeight, then
// scrolls.
func (e *Editor) Layout(gtx layout.Context, st EditorStyle) layout.Dimensions {
	e.init()
	if refocus.Take(gtx, e) { // a modal that opened over it closed
		e.Focus(gtx)
	}
	e.st = st
	e.hasStyle = st.Shaper != nil
	e.metric = gtx.Metric
	e.loc = gtx.Locale
	e.width = gtx.Constraints.Max.X
	for {
		if _, ok := e.Update(gtx); !ok {
			break
		}
	}
	if !e.ensureLayout() {
		return layout.Dimensions{Size: gtx.Constraints.Min}
	}
	lay := &e.lay
	e.contentH = max(lay.height, lay.m.lh)
	maxH := gtx.Constraints.Max.Y
	if st.MaxHeight > 0 {
		maxH = min(maxH, gtx.Dp(st.MaxHeight))
	}
	viewH := min(e.contentH, maxH)
	viewH = min(max(viewH, gtx.Constraints.Min.Y), gtx.Constraints.Max.Y)
	e.viewH = viewH

	cx, cbase, casc, cdesc, cli := lay.caretInfo(e.caret)
	if e.scrollCaret {
		e.scrollCaret = false
		ln := &lay.lines[cli]
		top, bot := ln.top, ln.top+ln.h
		p := &lay.paras[ln.para]
		if p.kind == CodeBlock && cli == p.l1-1 && p.boxBot > p.boxTop {
			bot = p.boxBot
		}
		if top < e.scrollY {
			e.scrollY = top
		}
		if bot > e.scrollY+viewH {
			e.scrollY = bot - viewH
		}
	}
	e.scrollY = min(max(e.scrollY, 0), max(0, e.contentH-viewH))
	e.caretRect = image.Rect(cx, cbase-casc-e.scrollY, cx+lay.m.caretW, cbase+cdesc-e.scrollY)
	if e.resetBlink {
		e.resetBlink = false
		e.blinkStart = gtx.Now
	}
	e.updateIME(gtx)

	size := image.Pt(e.width, viewH)
	defer clip.Rect(image.Rectangle{Max: size}).Push(gtx.Ops).Pop()
	pointer.CursorText.Add(gtx.Ops)
	event.Op(gtx.Ops, e)
	key.InputHintOp{Tag: e, Hint: key.HintText}.Add(gtx.Ops)
	e.noteLive(gtx) // editor_focus.go
	refocus.Note(gtx, e)
	e.scroller.Add(gtx.Ops)
	e.clicker.Add(gtx.Ops)
	e.dragger.Add(gtx.Ops)
	semantic.Editor.Add(gtx.Ops)

	focused := gtx.Focused(e)
	showCaret := false
	if focused {
		now := gtx.Now
		dt := now.Sub(e.blinkStart)
		blinking := dt < edMaxBlinkDuration
		const timePerBlink = time.Second / edBlinksPerSecond
		nextBlink := now.Add(timePerBlink/2 - dt%(timePerBlink/2))
		if blinking {
			gtx.Execute(op.InvalidateCmd{At: nextBlink})
		}
		showCaret = !blinking || dt%timePerBlink < timePerBlink/2
	}

	lay.paintStatic(gtx, tlColorsOf(&st))
	t := op.Offset(image.Pt(0, -e.scrollY)).Push(gtx.Ops)
	lay.bgCall.Add(gtx.Ops)
	if lo, hi := e.sel(); lo != hi {
		col := st.Selection
		if !focused {
			col.A /= 2
		}
		lay.forRange(lo, hi, func(li, x0, x1 int, nl bool) {
			ln := &lay.lines[li]
			if nl {
				x1 += lay.m.newlineW
			}
			fillRect(gtx, image.Rect(x0, ln.top, x1, ln.top+ln.h), col)
		})
	}
	lay.fgCall.Add(gtx.Ops)
	lay.paintEmoji(gtx, st.Images, st.Color, e.scrollY, e.scrollY+viewH)
	if c := e.ime.composition; focused && c.Start >= 0 && c.End > c.Start {
		lay.forRange(c.Start, c.End, func(li, x0, x1 int, nl bool) {
			y := lay.lines[li].base + max(lay.m.ppem.Round()/10, 1)
			fillRect(gtx, image.Rect(x0, y, x1, y+lay.m.deco), st.Color)
		})
	}
	if e.doc.Len() == 0 {
		lay.paintPlaceholder(gtx, st.PlaceholderText, st.Placeholder)
	}
	if showCaret && gtx.Enabled() {
		fillRect(gtx, image.Rect(cx, cbase-casc, cx+lay.m.caretW, cbase+cdesc), st.Caret)
	}
	t.Pop()
	return layout.Dimensions{Size: size}
}
