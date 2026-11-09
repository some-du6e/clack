package composer

import (
	"strings"
	"sync"
	"unicode"

	"github.com/some-du6e/yoloslackgoclient/internal/emoji"
)

// Typed emoji shortcodes, the way Slack's composer handles them: a complete
// ":name:" becomes the emoji atom as soon as its closing colon is typed,
// whether or not the autocomplete popup opened, as long as name is a known
// standard or custom emoji (aliases and ::skin-tone-N included). Paste and
// send convert the same way, and so does the frame after a late custom
// emoji set lands. Unknown names, inline code and code blocks stay text.

// emojiLookup resolves an emoji name to a fresh atom. It must be cheap: it
// runs on every typed ':' (the custom set is a map; see coreAPI.emoji).
type emojiLookup func(name string) (*Atom, bool)

// emojiNameRune reports whether r can be part of an emoji name. Slack's
// names are lowercase letters, digits, '_', '-', '+' and apostrophes; custom names
// may use other scripts.
func emojiNameRune(r rune) bool {
	return r == '_' || r == '-' || r == '+' || r == '\'' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// emojiShortcodeAt finds the shortcode closed by the ':' at k: the opening
// ':' j and the name between them. The opener starts the line or follows a
// non-word rune (so "12:30:" or "a:b:" stay text). Nothing matches in
// inline code, links or code blocks.
func emojiShortcodeAt(d *Doc, k int) (j int, name string, ok bool) {
	if k < 2 || k >= d.Len() || d.Runes[k] != ':' || d.Attrs[k].Atom != nil {
		return 0, "", false
	}
	line := d.LineOf(k)
	if line < len(d.Lines) && d.Lines[line].Kind == CodeBlock {
		return 0, "", false
	}
	ls := d.LineStart(line)
	j = k - 1
	for j >= ls && d.Runes[j] != ':' {
		if d.Attrs[j].Atom != nil || !emojiNameRune(d.Runes[j]) {
			return 0, "", false
		}
		j--
	}
	if j < ls || j == k-1 || d.Attrs[j].Atom != nil {
		return 0, "", false
	}
	if j > ls && d.Attrs[j-1].Atom == nil {
		if p := d.Runes[j-1]; unicode.IsLetter(p) || unicode.IsDigit(p) {
			return 0, "", false
		}
	}
	for i := j; i <= k; i++ {
		if a := d.Attrs[i]; a.Style&Code != 0 || a.Link != "" {
			return 0, "", false
		}
	}
	return j, string(d.Runes[j+1 : k]), true
}

// emojiSkinTone reports the N of a "skin-tone-N" name (2-6).
func emojiSkinTone(name string) (string, bool) {
	n, ok := strings.CutPrefix(name, "skin-tone-")
	return n, ok && len(n) == 1 && n[0] >= '2' && n[0] <= '6'
}

// emojiEdit is one conversion: [start, end) became n runes (the atom).
type emojiEdit struct{ start, end, n int }

// mapPos moves p across the edit: positions after it shift, positions
// inside it land after the atom.
func (ed emojiEdit) mapPos(p int) int {
	switch {
	case p >= ed.end:
		return p - (ed.end - ed.start - ed.n)
	case p > ed.start:
		return ed.start + ed.n
	}
	return p
}

// convertEmojiAt converts the shortcode closed by the ':' at k, if its name
// resolves. ":skin-tone-N:" right after an emoji atom that has skin tones
// re-tones that atom (":thumbsup::skin-tone-3:" typed or pasted is one
// emoji, like Slack).
func convertEmojiAt(d *Doc, k int, look emojiLookup) (emojiEdit, bool) {
	if look == nil {
		return emojiEdit{}, false
	}
	j, name, ok := emojiShortcodeAt(d, k)
	if !ok {
		return emojiEdit{}, false
	}
	if tone, isTone := emojiSkinTone(name); isTone && j > 0 {
		if prev := d.Attrs[j-1].Atom; prev != nil && prev.Kind == AtomEmoji && !strings.Contains(prev.ID, "::skin-tone-") {
			if at, ok := look(prev.ID + "::skin-tone-" + tone); ok && at.URL != prev.URL {
				d.Delete(j, k+1)
				d.Attrs[j-1].Atom = at
				return emojiEdit{start: j - 1, end: k + 1, n: 1}, true
			}
		}
	}
	at, ok := look(name)
	if !ok {
		return emojiEdit{}, false
	}
	d.Delete(j, k+1)
	d.InsertAtom(j, at)
	return emojiEdit{start: j, end: k + 1, n: 1}, true
}

// convertEmojiRange converts every complete shortcode whose closing ':'
// lies in [from, to) (positions before any conversion). It returns the
// edits in order; map positions through each in turn.
func convertEmojiRange(d *Doc, from, to int, look emojiLookup) []emojiEdit {
	if look == nil {
		return nil
	}
	var eds []emojiEdit
	from, to = d.clamp(from), d.clamp(to)
	for k := from; k < to && k < d.Len(); k++ {
		if d.Runes[k] != ':' {
			continue
		}
		if ed, ok := convertEmojiAt(d, k, look); ok {
			eds = append(eds, ed)
			shift := ed.end - ed.start - ed.n
			to -= shift
			k = ed.start // the next rune after the atom is k+1 after the loop's k++
		}
	}
	return eds
}

// Native unicode emoji, the way Slack's message input handles them
// (replaceNativeEmoji in its emoji module): an emoji typed, picked from the
// OS picker, committed by an IME or pasted becomes the same emoji blot a
// picked ":name:" would, named after the standard emoji (👍 is "+1", 👍🏽
// "+1::skin-tone-4"), so it draws as Slack's image and is sent as an emoji
// element. Like Slack, inline code, links and code blocks keep the text,
// as does an emoji right after a ZWJ.

const zwj = '\u200d'

// nativeEmojiOK reports whether runes [k, end) may become an emoji atom:
// plain text on a non-code line.
func nativeEmojiOK(d *Doc, k, end int) bool {
	if k > 0 && d.Runes[k-1] == zwj {
		return false
	}
	if l := d.LineOf(k); l < len(d.Lines) && d.Lines[l].Kind == CodeBlock {
		return false
	}
	for i := k; i < end; i++ {
		if a := d.Attrs[i]; a.Atom != nil || a.Style&Code != 0 || a.Link != "" || d.Runes[i] == '\n' {
			return false
		}
	}
	return true
}

// convertNativeEmojiRange converts the unicode emoji starting in
// [from, to) into emoji atoms. It returns the edits in order.
func convertNativeEmojiRange(d *Doc, from, to int, look emojiLookup) []emojiEdit {
	if look == nil {
		return nil
	}
	from, to = d.clamp(from), d.clamp(to)
	if !emoji.MayHaveEmoji(string(d.Runes[from:to])) {
		return nil
	}
	var eds []emojiEdit
	for k := from; k < to && k < d.Len(); k++ {
		if d.Attrs[k].Atom != nil {
			continue
		}
		name, end, ok := emoji.MatchUnicode(d.Runes, k)
		if !ok || !nativeEmojiOK(d, k, end) {
			continue
		}
		at, ok := look(name)
		if !ok {
			continue
		}
		d.Delete(k, end)
		d.InsertAtom(k, at)
		ed := emojiEdit{start: k, end: end, n: 1}
		eds = append(eds, ed)
		to -= ed.end - ed.start - ed.n
	}
	return eds
}

// hasShortcodeColon reports whether d has a ':' outside atoms (a cheap
// precheck before rescanning for a late emoji set).
func hasShortcodeColon(d *Doc) bool {
	for i, r := range d.Runes {
		if r == ':' && d.Attrs[i].Atom == nil {
			return true
		}
	}
	return false
}

// emojiAtomFrom builds the atom for a resolved emoji.
func emojiAtomFrom(name, url, text string) *Atom {
	a := &Atom{Kind: AtomEmoji, ID: name, Text: ":" + name + ":", URL: url}
	if text != "" && !strings.HasPrefix(text, ":") {
		a.Uni = text
	}
	return a
}

var warmStandard sync.Once

// warmEmoji parses the standard emoji table in the background, so the
// first typed ':' doesn't pay for it (the custom set is already a map).
func warmEmoji() {
	warmStandard.Do(func() { go emoji.Standard("smile") })
}
