package composer

import (
	"strings"
	"unicode"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/uipref"
)

// Typed emoticons, the way Slack's composer converts them with
// convert_emoticons on (Messages & media > "Convert my typed emoticons to
// emoji, so :D becomes 😃"): the texty emoji module's
// maybeConvertEmoticonFromKeyboardBinding, on Space and on Enter. The line
// before the caret is matched against L3r4's regex,
// (^|\s)(emoticon)(?=$|[\s|?.,!]), and the last emoticon found becomes the
// emoji s8g3 dq names, unless it's in a link or code, the rune before it
// isn't whitespace or an emoji, or it's the "8)" of a list after "7)".
// Off under "Display emoji as plain text" (shouldRenderEmojiAsText).

// emoticons is s8g3's dq.
var emoticons = map[string]string{
	":o)": "monkey_face", "</3": "broken_heart", "=)": "smiley", "=-)": "smiley", ":D": "smile", ":-D": "smile",
	":>": "laughing", ":->": "laughing", ";)": "wink", ";-)": "wink", "8)": "sunglasses", ":|": "neutral_face",
	":-|": "neutral_face", ":\\": "confused", ":-\\": "confused", ":/": "confused", ":-/": "confused",
	":*": "kissing_heart", ":-*": "kissing_heart", ":p": "stuck_out_tongue", ":-p": "stuck_out_tongue",
	":P": "stuck_out_tongue", ":-P": "stuck_out_tongue", ":b": "stuck_out_tongue", ":-b": "stuck_out_tongue",
	";p": "stuck_out_tongue_winking_eye", ";-p": "stuck_out_tongue_winking_eye", ";b": "stuck_out_tongue_winking_eye",
	";-b": "stuck_out_tongue_winking_eye", ";P": "stuck_out_tongue_winking_eye", ";-P": "stuck_out_tongue_winking_eye",
	"):": "disappointed", ":(": "disappointed", ":-(": "disappointed", ">:(": "angry", ">:-(": "angry",
	":'(": "cry", "D:": "anguished", ":o": "open_mouth", ":-o": "open_mouth", ":O": "open_mouth", ":-O": "open_mouth",
	":)": "slightly_smiling_face", "(:": "slightly_smiling_face", ":-)": "slightly_smiling_face", "<3": "heart",
}

// emoticonKeys are dq's keys in the regex's alternation order (insertion
// order; JS takes the first alternative that matches at a position).
var emoticonKeys = []string{":o)", "</3", "=)", "=-)", ":D", ":-D", ":>", ":->", ";)", ";-)", "8)", ":|", ":-|", ":\\", ":-\\",
	":/", ":-/", ":*", ":-*", ":p", ":-p", ":P", ":-P", ":b", ":-b", ";p", ";-p", ";b", ";-b", ";P", ";-P", "):", ":(", ":-(",
	">:(", ">:-(", ":'(", "D:", ":o", ":-o", ":O", ":-O", ":)", "(:", ":-)", "<3"}

// emoticonFollow is the regex's lookahead class: end, \s, |, ?, ., ,, !.
func emoticonFollow(r []rune, i int) bool {
	if i >= len(r) {
		return true
	}
	c := r[i]
	return unicode.IsSpace(c) || strings.ContainsRune("|?.,!", c)
}

// lastEmoticon is the replace loop over the prefix: the start and text of
// the last match (-1 if none).
func lastEmoticon(prefix []rune) (int, string) {
	at, found := -1, ""
	for i := 0; i < len(prefix); {
		if i > 0 && !unicode.IsSpace(prefix[i-1]) {
			i++
			continue
		}
		matched := false
		for _, k := range emoticonKeys {
			kr := []rune(k)
			if i+len(kr) <= len(prefix) && string(prefix[i:i+len(kr)]) == k && emoticonFollow(prefix, i+len(kr)) {
				at, found = i, k
				i += len(kr)
				matched = true
				break
			}
		}
		if !matched {
			i++
		}
	}
	return at, found
}

// convertEmoticon converts the emoticon before pos (the caret before the
// Space or Enter). It reports whether the document changed.
func (e *Editor) convertEmoticon(pos int) bool {
	p := uipref.Get()
	if !p.ConvertEmoticons || p.EmojiAsText || e.emoji == nil || e.composing() {
		return false
	}
	d := e.doc
	pos = d.clamp(pos)
	line := d.LineOf(pos)
	if line < len(d.Lines) && d.Lines[line].Kind == CodeBlock {
		return false
	}
	ls := d.LineStart(line)
	prefix := make([]rune, pos-ls)
	for i := ls; i < pos; i++ {
		prefix[i-ls] = d.Runes[i]
		if d.Attrs[i].Atom != nil {
			prefix[i-ls] = '￼' // an embed: not whitespace, not text
		}
	}
	r, s := lastEmoticon(prefix)
	if r < 0 {
		return false
	}
	o := ls + r
	end := o + len([]rune(s))
	for i := o; i < end; i++ {
		if a := d.Attrs[i]; a.Atom != nil || a.Style&Code != 0 || a.Link != "" {
			return false
		}
	}
	if s == "8)" && o == ls && line > 0 { // a list's "8)" after "7)"
		prev := d.LineStart(line - 1)
		if prev+2 <= len(d.Runes) && string(d.Runes[prev:prev+2]) == "7)" {
			return false
		}
	}
	if o > 0 && o > ls { // wysiwyg: whitespace or an emoji before it
		if b := d.Attrs[o-1].Atom; !(b != nil && b.Kind == AtomEmoji) && !unicode.IsSpace(d.Runes[o-1]) {
			return false
		}
	}
	at, ok := e.emoji(emoticons[s])
	if !ok {
		return false
	}
	literal := e.snap()
	d.Delete(o, end)
	d.InsertAtom(o, at)
	ed := emojiEdit{start: o, end: end, n: 1}
	e.anchor, e.caret = ed.mapPos(e.anchor), ed.mapPos(e.caret)
	e.pushUndo(literal)
	e.redo = e.redo[:0]
	e.lastKind = edOther
	e.commit()
	return true
}
