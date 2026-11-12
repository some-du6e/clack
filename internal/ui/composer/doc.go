package composer

import (
	"strings"
	"unicode/utf8"
)

// The composer edits a Doc: a flat rune buffer with one Attr per rune and
// one Line per '\n'-separated line. Positions are rune offsets in
// [0, Len()]. Mentions and emoji are atoms: a single ObjRune whose Attr
// points at an Atom, so the caret and deletion treat them as one unit.
//
// The flat layout keeps editing simple (every operation is an insert or a
// delete over a range) and undo cheap (messages are short, so a snapshot is
// a copy of three slices).

// Style is an inline format bitmask.
type Style uint8

const (
	Bold Style = 1 << iota
	Italic
	Underline
	Strike
	Code
)

// BlockKind is the block format of a line. Consecutive lines with the same
// kind form one block (one list, one quote, one code block).
type BlockKind uint8

const (
	Para BlockKind = iota
	Bullet
	Ordered
	Quote
	CodeBlock
	// Header1/2/3 (Slack's Big/Medium/Small heading) and Divider are
	// single-line kinds: never grouped and never indented. Appended after
	// CodeBlock so the earlier constants keep their values.
	Header1
	Header2
	Header3
	Divider
)

// isHeading reports whether k is one of the heading kinds.
func isHeading(k BlockKind) bool { return k == Header1 || k == Header2 || k == Header3 }

// AtomKind is the kind of an inline object.
type AtomKind uint8

const (
	AtomUser AtomKind = iota + 1
	AtomChannel
	AtomUsergroup
	AtomBroadcast // ID is here | channel | everyone
	AtomEmoji     // ID is the emoji name, with ::skin-tone-N
	// AtomMessage is a Slack message permalink turned into Slack's message
	// slug (sent as message_mention): ID is the message ts, Text the label
	// ("karim in #clack"), URL the permalink, Channel/Author/Thread where
	// the message is and who wrote it.
	AtomMessage
)

// ObjRune stands in for an atom in Doc.Runes.
const ObjRune = '￼'

// Atom is an inline object (a mention pill or an emoji).
type Atom struct {
	Kind AtomKind
	ID   string // user/channel/usergroup id, broadcast range, emoji name
	Text string // display text: "@karim", "#general", "@here", ":smile:"
	URL  string // emoji image (custom or CDN); "" for unicode-only emoji
	Uni  string // emoji: unicode text, if standard

	Channel, Author, Thread string // AtomMessage
}

// Attr is the formatting of one rune.
type Attr struct {
	Style Style
	Link  string // link target, "" for none
	Atom  *Atom  // set iff the rune is ObjRune
}

// Line is the block formatting of one line.
type Line struct {
	Kind   BlockKind
	Indent int // list nesting level, 0-based
	// Quoted is a list item inside a quote: Quill's "border" line format
	// 1 on a list (BorderAttributor, boot-shared KT9Q), which list
	// autofill sets when it runs on a quote line, and rich_text_list's
	// border: 1. Only list lines carry it.
	Quoted bool
}

// Doc is the editable document.
type Doc struct {
	Runes []rune
	Attrs []Attr
	Lines []Line
}

// NewDoc returns an empty document with one paragraph line.
func NewDoc() *Doc { return &Doc{Lines: []Line{{}}} }

// Len is the number of runes.
func (d *Doc) Len() int { return len(d.Runes) }

// Empty reports whether the document has no visible content.
func (d *Doc) Empty() bool {
	for _, r := range d.Runes {
		if r != '\n' && r != ' ' && r != '\t' && r != ' ' {
			return false
		}
	}
	return true
}

// Clone deep-copies the document (atoms are immutable and shared).
func (d *Doc) Clone() *Doc {
	return &Doc{
		Runes: append([]rune(nil), d.Runes...),
		Attrs: append([]Attr(nil), d.Attrs...),
		Lines: append([]Line(nil), d.Lines...),
	}
}

// LineOf returns the index of the line containing pos.
func (d *Doc) LineOf(pos int) int {
	pos = d.clamp(pos)
	n := 0
	for i := 0; i < pos; i++ {
		if d.Runes[i] == '\n' {
			n++
		}
	}
	return n
}

// LineStart returns the offset of the first rune of line i.
func (d *Doc) LineStart(i int) int {
	if i <= 0 {
		return 0
	}
	n := 0
	for p, r := range d.Runes {
		if r == '\n' {
			n++
			if n == i {
				return p + 1
			}
		}
	}
	return len(d.Runes)
}

// LineEnd returns the offset just past the last rune of line i (the
// position of its '\n', or Len()).
func (d *Doc) LineEnd(i int) int {
	for p := d.LineStart(i); p < len(d.Runes); p++ {
		if d.Runes[p] == '\n' {
			return p
		}
	}
	return len(d.Runes)
}

// LineText returns line i as a string (atoms as ObjRune).
func (d *Doc) LineText(i int) string {
	return string(d.Runes[d.LineStart(i):d.LineEnd(i)])
}

func (d *Doc) clamp(p int) int {
	if p < 0 {
		return 0
	}
	if p > len(d.Runes) {
		return len(d.Runes)
	}
	return p
}

// Insert inserts text with attr at pos and returns the position after it.
// Each '\n' splits the current line; the new line copies the kind and
// indent of the line it was split from. Atoms cannot be inserted this way.
func (d *Doc) Insert(pos int, text string, a Attr) int {
	pos = d.clamp(pos)
	a.Atom = nil
	rs := []rune(strings.ReplaceAll(text, "\r\n", "\n"))
	for i, r := range rs {
		if r == '\r' || r == ObjRune {
			rs[i] = ' '
		}
	}
	if len(rs) == 0 {
		return pos
	}
	line := d.LineOf(pos)
	var newLines []Line
	for _, r := range rs {
		if r == '\n' {
			newLines = append(newLines, d.Lines[line])
		}
	}
	attrs := make([]Attr, len(rs))
	for i := range attrs {
		attrs[i] = a
		if rs[i] == '\n' {
			attrs[i] = Attr{}
		}
	}
	d.Runes = insertAt(d.Runes, pos, rs)
	d.Attrs = insertAt(d.Attrs, pos, attrs)
	if len(newLines) > 0 {
		d.Lines = insertAt(d.Lines, line+1, newLines)
	}
	return pos + len(rs)
}

// InsertAtom inserts an atom at pos and returns the position after it.
// Atoms carry no inline style besides links (never) and keep their own look.
func (d *Doc) InsertAtom(pos int, at *Atom) int {
	pos = d.clamp(pos)
	d.Runes = insertAt(d.Runes, pos, []rune{ObjRune})
	d.Attrs = insertAt(d.Attrs, pos, []Attr{{Atom: at}})
	return pos + 1
}

// Delete removes [start, end). Joined lines keep the first line's format.
func (d *Doc) Delete(start, end int) {
	start, end = d.clamp(start), d.clamp(end)
	if start > end {
		start, end = end, start
	}
	if start == end {
		return
	}
	first := d.LineOf(start)
	nl := 0
	for _, r := range d.Runes[start:end] {
		if r == '\n' {
			nl++
		}
	}
	d.Runes = append(d.Runes[:start], d.Runes[end:]...)
	d.Attrs = append(d.Attrs[:start], d.Attrs[end:]...)
	if nl > 0 {
		d.Lines = append(d.Lines[:first+1], d.Lines[first+1+nl:]...)
	}
}

// Slice returns a copy of [start, end) as a standalone document, used for
// copy/paste within the composer.
func (d *Doc) Slice(start, end int) *Doc {
	start, end = d.clamp(start), d.clamp(end)
	if start > end {
		start, end = end, start
	}
	out := &Doc{
		Runes: append([]rune(nil), d.Runes[start:end]...),
		Attrs: append([]Attr(nil), d.Attrs[start:end]...),
	}
	l0, l1 := d.LineOf(start), d.LineOf(end)
	out.Lines = append([]Line(nil), d.Lines[l0:l1+1]...)
	return out
}

// InsertDoc pastes src at pos, keeping its inline and block formats, and
// returns the position after it. The first pasted line merges into the
// current line (keeping the current line's kind).
func (d *Doc) InsertDoc(pos int, src *Doc) int {
	pos = d.clamp(pos)
	if len(src.Runes) == 0 {
		return pos
	}
	line := d.LineOf(pos)
	d.Runes = insertAt(d.Runes, pos, append([]rune(nil), src.Runes...))
	d.Attrs = insertAt(d.Attrs, pos, append([]Attr(nil), src.Attrs...))
	if len(src.Lines) > 1 {
		// The tail of the split line takes the last pasted line's kind only
		// if text follows; keep it simple: lines after the first take the
		// source kinds.
		d.Lines = insertAt(d.Lines, line+1, append([]Line(nil), src.Lines[1:]...))
	}
	return pos + len(src.Runes)
}

// StyleAt returns the style the caret at pos would type with: the style of
// the rune before pos on the same line, ignoring atoms.
func (d *Doc) StyleAt(pos int) Style {
	pos = d.clamp(pos)
	if pos == 0 || d.Runes[pos-1] == '\n' || d.Attrs[pos-1].Atom != nil {
		return 0
	}
	return d.Attrs[pos-1].Style
}

// HasStyle reports whether every non-newline, non-atom rune in [start, end)
// has s.
func (d *Doc) HasStyle(start, end int, s Style) bool {
	any := false
	for i := d.clamp(start); i < d.clamp(end); i++ {
		if d.Runes[i] == '\n' || d.Attrs[i].Atom != nil {
			continue
		}
		any = true
		if d.Attrs[i].Style&s == 0 {
			return false
		}
	}
	return any
}

// SetStyle adds (on) or removes s over [start, end). Inline code is
// exclusive with links in Slack's editor, so it is not special-cased here.
func (d *Doc) SetStyle(start, end int, s Style, on bool) {
	for i := d.clamp(start); i < d.clamp(end); i++ {
		if d.Runes[i] == '\n' || d.Attrs[i].Atom != nil {
			continue
		}
		if on {
			d.Attrs[i].Style |= s
		} else {
			d.Attrs[i].Style &^= s
		}
	}
}

// ToggleStyle sets s on [start, end) unless all of it already has s.
func (d *Doc) ToggleStyle(start, end int, s Style) {
	d.SetStyle(start, end, s, !d.HasStyle(start, end, s))
}

// SetLink sets (or clears with "") the link target of [start, end).
func (d *Doc) SetLink(start, end int, url string) {
	for i := d.clamp(start); i < d.clamp(end); i++ {
		if d.Runes[i] == '\n' || d.Attrs[i].Atom != nil {
			continue
		}
		d.Attrs[i].Link = url
	}
}

// LinkAt returns the link range around pos (pos inside or at the end of a
// link run) and its URL.
func (d *Doc) LinkAt(pos int) (start, end int, url string) {
	pos = d.clamp(pos)
	i := pos
	if i == len(d.Runes) || d.Attrs[i].Link == "" {
		i--
	}
	if i < 0 || d.Attrs[i].Link == "" {
		return pos, pos, ""
	}
	url = d.Attrs[i].Link
	start, end = i, i+1
	for start > 0 && d.Attrs[start-1].Link == url {
		start--
	}
	for end < len(d.Runes) && d.Attrs[end].Link == url {
		end++
	}
	return start, end, url
}

// SetLineKind sets the block kind of lines [from, to] (inclusive).
func (d *Doc) SetLineKind(from, to int, k BlockKind) {
	if from > to {
		from, to = to, from
	}
	for i := from; i <= to && i < len(d.Lines); i++ {
		if i < 0 {
			continue
		}
		d.Lines[i].Kind = k
		if k != Bullet && k != Ordered {
			d.Lines[i].Indent, d.Lines[i].Quoted = 0, false
		}
	}
}

// ToggleLineKind sets lines [from, to] to k, or back to Para if they all
// already are k.
func (d *Doc) ToggleLineKind(from, to int, k BlockKind) {
	if from > to {
		from, to = to, from
	}
	all := true
	for i := from; i <= to && i < len(d.Lines); i++ {
		if d.Lines[i].Kind != k {
			all = false
		}
	}
	if all {
		k = Para
	}
	d.SetLineKind(from, to, k)
}

// Text returns the plain text with atoms as their display text.
func (d *Doc) Text() string {
	var b strings.Builder
	for i, r := range d.Runes {
		if a := d.Attrs[i].Atom; a != nil {
			if a.Kind == AtomEmoji && a.Uni != "" {
				b.WriteString(a.Uni)
			} else {
				b.WriteString(a.Text)
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// WordBefore returns the start of the run of non-space, non-atom runes that
// ends at pos on the same line (for autocomplete triggers).
func (d *Doc) WordBefore(pos int) int {
	pos = d.clamp(pos)
	i := pos
	for i > 0 {
		r := d.Runes[i-1]
		if r == '\n' || r == ' ' || r == '\t' || r == ' ' || d.Attrs[i-1].Atom != nil {
			break
		}
		i--
	}
	return i
}

// String returns [start, end) as raw text (atoms as ObjRune).
func (d *Doc) String(start, end int) string {
	start, end = d.clamp(start), d.clamp(end)
	if start > end {
		start, end = end, start
	}
	return string(d.Runes[start:end])
}

func insertAt[T any](s []T, at int, v []T) []T {
	s = append(s, v...) // grow
	copy(s[at+len(v):], s[at:len(s)-len(v)])
	copy(s[at:], v)
	return s
}

// runeOffset converts a byte offset in s to a rune offset.
func runeOffset(s string, byteOff int) int { return utf8.RuneCountInString(s[:byteOff]) }
