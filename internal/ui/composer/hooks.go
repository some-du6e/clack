package composer

import (
	"encoding/json"
	"unicode"
)

// Plugin hooks (docs/PLUGINS-components.md §3.2). taut-api installs one
// dispatcher per hook when the plugin runtime starts, on the UI goroutine
// (or before the first frame). Each is a plain func var, nil-checked at
// the call site, so the composer pays nothing while it is unset.

// SendCheck is what composer.addSendCheck sees of an outgoing message.
type SendCheck struct {
	ChannelID string
	ThreadTS  string          // "" in a channel composer
	Blocks    json.RawMessage // the rich_text blocks being sent
	Text      string          // the mrkdwn fallback text
}

// SendChecks runs composer.addSendCheck before every message a channel
// or thread composer sends (not edits, slash commands or scheduled
// messages). The composer clears at once and waits for done, which may
// be called later on any goroutine (a check can ask for confirmation).
// done(false) puts the message back in the composer, if the user hasn't
// typed since, and keeps any attached files.
var SendChecks func(m SendCheck, done func(ok bool))

// LinkTransforms rewrites a URL (composer.addLinkTransform, e.g.
// ClearURLs). The composer applies it to URLs pasted outside code, to a
// URL pasted over a selection, and in the link dialog's save
// (ComposerLinkModal.saveLink). Return the URL unchanged to skip it.
var LinkTransforms func(url string) string

// MarkupSpan is one piece of inline markup found by InlineMarkup, as
// rune offsets into the text it was given.
type MarkupSpan struct {
	// Start and End bound the markup, opening and closing markers
	// included; [ContentStart, ContentEnd) is the visible part.
	Start, End               int
	ContentStart, ContentEnd int
	// Format is "bold", "italic", "strike", "code" or "link" (with Link
	// set). "" only removes the markers (an escape).
	Format string
	Link   string
}

// InlineMarkup replaces the composer's live *bold* _italic_ ~strike~
// `code` shortcuts (composer.patchInlineMarkup's parse, e.g.
// RealMarkdown). It gets the caret's line up to the caret, embeds and
// inline code as U+FFFC, and returns the spans it finds; a span ending at
// the caret is applied as you type. It runs on the UI goroutine after a
// typed punctuation or symbol character, so it must be quick. Offsets are
// runes (taut-api converts from JS's UTF-16).
var InlineMarkup func(line string) []MarkupSpan

// markupTrigger reports whether typing r may close custom markup.
func markupTrigger(r rune) bool { return unicode.IsPunct(r) || unicode.IsSymbol(r) }

// markupInline applies the InlineMarkup span that the rune at caret-1
// closed, like autoformatInline does for Slack's markup. It returns the
// new caret (the end of the formatted text) and the style applied.
func markupInline(d *Doc, caret int) (newCaret int, st Style, ok bool) {
	parse := InlineMarkup
	if parse == nil || caret < 1 || caret > d.Len() {
		return caret, 0, false
	}
	k := caret - 1
	if d.Attrs[k].Atom != nil || d.Attrs[k].Style&Code != 0 {
		return caret, 0, false
	}
	line := d.LineOf(k)
	if line < len(d.Lines) && d.Lines[line].Kind == CodeBlock {
		return caret, 0, false
	}
	ls := d.LineStart(line)
	rs := make([]rune, caret-ls)
	for i := ls; i < caret; i++ {
		r := d.Runes[i]
		if d.Attrs[i].Atom != nil || d.Attrs[i].Style&Code != 0 {
			r = ObjRune
		}
		rs[i-ls] = r
	}
	n := len(rs)
	for _, s := range parse(string(rs)) {
		if s.End != n || s.Start < 0 || s.Start > s.ContentStart || s.ContentStart > s.ContentEnd || s.ContentEnd > s.End {
			continue
		}
		if s.Start == s.ContentStart && s.ContentEnd == s.End {
			continue // nothing to remove
		}
		if !plainRunes(rs[s.Start:s.ContentStart]) || !plainRunes(rs[s.ContentEnd:s.End]) {
			continue // markers are text, never an embed or code
		}
		link := ""
		switch s.Format {
		case "bold":
			st = Bold
		case "italic":
			st = Italic
		case "strike":
			st = Strike
		case "code":
			st = Code
		case "link":
			if s.Link == "" {
				continue
			}
			link = s.Link
		case "":
		default:
			continue
		}
		d.Delete(ls+s.ContentEnd, ls+s.End)
		d.Delete(ls+s.Start, ls+s.ContentStart)
		a := ls + s.Start
		b := a + s.ContentEnd - s.ContentStart
		if b > a && st != 0 {
			d.SetStyle(a, b, st, true)
		}
		if b > a && link != "" {
			d.SetLink(a, b, link)
		}
		return b, st, true
	}
	return caret, 0, false
}

func plainRunes(rs []rune) bool {
	for _, r := range rs {
		if r == ObjRune {
			return false
		}
	}
	return true
}

// transformLink runs LinkTransforms on one URL.
func transformLink(u string) string {
	if f := LinkTransforms; f != nil && u != "" {
		if v := f(u); v != "" {
			return v
		}
	}
	return u
}

// transformPastedLinks runs LinkTransforms over the bare URLs in the
// pasted range [lo, hi), skipping code and existing links, and returns
// the range's new end.
func transformPastedLinks(d *Doc, lo, hi int) int {
	f := LinkTransforms
	if f == nil || hi <= lo {
		return hi
	}
	ms := rtFindURLs(d.Runes[lo:hi])
	for i := len(ms) - 1; i >= 0; i-- {
		m := ms[i]
		a, b := lo+m.start, lo+m.end
		at := d.Attrs[a]
		if at.Style&Code != 0 || at.Link != "" || at.Atom != nil {
			continue
		}
		if l := d.LineOf(a); l < len(d.Lines) && d.Lines[l].Kind == CodeBlock {
			continue
		}
		u := f(m.url)
		if u == "" || u == m.url {
			continue
		}
		d.Delete(a, b)
		nb := d.Insert(a, u, Attr{Style: at.Style})
		hi += nb - b
	}
	return hi
}
