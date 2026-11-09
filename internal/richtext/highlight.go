package richtext

import (
	"sort"
	"strings"
	"unicode"
)

// Keyword highlighting: Slack's highlight words (the viewer's Channel
// keywords, all_notifications_prefs.global.global_keywords) act like
// mentions and are drawn highlighted (c-mrkdwn__highlight) wherever they
// match in a message. This is client-boot lxEU, CDN build a84662d2d135:
//
//   - normalizeText lowercases and straightens curly quotes; blank words
//     are dropped, longer words are matched first (sortHighlights) and a
//     character already matched isn't matched again;
//   - isMatch: a match inside an @mention ("@" then no space before it),
//     inside <…> (mrkdwn only) or in an HTML entity doesn't count; the
//     characters on either side must not be letters, digits or marks,
//     unless they or the word's ends are CJK/kana/hangul (s8g3 kw);
//   - overlapping matches merge (combineOverlappingMatches).

// highlighter is a Resolver that knows the viewer's highlight words.
type highlighter interface{ HighlightWords() []string }

// cjk is s8g3's kw ranges: scripts without word boundaries.
var cjk = []*unicode.RangeTable{unicode.Hiragana, unicode.Katakana, unicode.Hangul, unicode.Han,
	{R16: []unicode.Range16{{Lo: 0xFF00, Hi: 0xFFEF, Stride: 1}, {Lo: 0x3001, Hi: 0x303F, Stride: 1}, {Lo: 0x2E80, Hi: 0x2FFF, Stride: 1},
		{Lo: 0x31C0, Hi: 0x31FF, Stride: 1}, {Lo: 0x3200, Hi: 0x33FF, Stride: 1}, {Lo: 0xFE30, Hi: 0xFE4F, Stride: 1}}}}

func isCJK(r rune) bool { return r != 0 && unicode.IsOneOf(cjk, r) }

// wordChar is lxEU's S: /[\p{N}\p{M}\p{L}a-z0-9]/u.
func wordChar(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r) }

func validEdge(r rune) bool { return r == 0 || !wordChar(r) || isCJK(r) }

// normalizeRunes is normalizeText, rune for rune (so indexes carry over).
func normalizeRunes(s []rune) []rune {
	out := make([]rune, len(s))
	for i, r := range s {
		switch r {
		case '‘', '’':
			r = '\''
		case '“', '”':
			r = '"'
		default:
			r = unicode.ToLower(r)
		}
		out[i] = r
	}
	return out
}

// Match is one highlighted run, in runes.
type Match struct{ Index, Length int }

// prepWords is sortHighlights(normalize(words without blank ones)).
func prepWords(words []string) [][]rune {
	var ws [][]rune
	for _, w := range words {
		if strings.TrimSpace(w) == "" {
			continue
		}
		ws = append(ws, normalizeRunes([]rune(w)))
	}
	sort.SliceStable(ws, func(i, j int) bool { return len(ws[i]) > len(ws[j]) })
	return ws
}

// LocateHighlightWords finds words in text (lxEU locateHighlightWords for
// mrkdwn, rich: locateRichTextHighlightWords, which doesn't skip <…>).
func LocateHighlightWords(text string, words []string, rich bool) []Match {
	if text == "" || len(words) == 0 {
		return nil
	}
	return locate([]rune(text), prepWords(words), rich)
}

func locate(text []rune, ws [][]rune, rich bool) []Match {
	if len(ws) == 0 {
		return nil
	}
	s := normalizeRunes(text)
	taken := make([]bool, len(s))
	var out []Match
	for _, w := range ws {
		for i := indexRunes(s, w, 0); i >= 0; i = indexRunes(s, w, i+1) {
			if isMatch(s, w, i, taken, rich) {
				for k := 0; k < len(w); k++ {
					taken[i+k] = true
				}
				out = append(out, Match{i, len(w)})
			}
		}
	}
	return combineMatches(out)
}

func indexRunes(s, w []rune, from int) int {
	for i := from; i+len(w) <= len(s); i++ {
		ok := true
		for k := range w {
			if s[i+k] != w[k] {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

func isMatch(s, w []rune, i int, taken []bool, rich bool) bool {
	if taken[i] || inMention(s, i) || (!rich && inAngles(s, i, len(w))) {
		return false
	}
	var before, after rune
	if i > 0 {
		before = s[i-1]
	}
	if i+len(w) < len(s) {
		after = s[i+len(w)]
	}
	if w[0] == '&' && (after == ';' || containsRune(w, ';')) { // isPartOfHtmlEntity
		return false
	}
	ends := isCJK(w[0]) && isCJK(w[len(w)-1])
	return (validEdge(before) || ends) && (validEdge(after) || ends)
}

// inMention is /@[^\s]*$/ on the text before i.
func inMention(s []rune, i int) bool {
	for j := i - 1; j >= 0; j-- {
		if unicode.IsSpace(s[j]) {
			return false
		}
		if s[j] == '@' {
			return true
		}
	}
	return false
}

// inAngles is /<[^>]*$/ before and /^[^<]*>/ after.
func inAngles(s []rune, i, n int) bool {
	open := false
	for j := i - 1; j >= 0; j-- {
		if s[j] == '>' {
			return false
		}
		if s[j] == '<' {
			open = true
			break
		}
	}
	if !open {
		return false
	}
	for j := i + n; j < len(s); j++ {
		if s[j] == '<' {
			return false
		}
		if s[j] == '>' {
			return true
		}
	}
	return false
}

func containsRune(w []rune, r rune) bool {
	for _, x := range w {
		if x == r {
			return true
		}
	}
	return false
}

func combineMatches(ms []Match) []Match {
	sort.Slice(ms, func(i, j int) bool { return ms[i].Index < ms[j].Index })
	for i := 0; i < len(ms)-1; {
		a, b := &ms[i], ms[i+1]
		end := a.Index + a.Length
		if b.Index >= a.Index && b.Index <= end {
			if e := b.Index + b.Length; e > end {
				a.Length = e - a.Index
			}
			ms = append(ms[:i+1], ms[i+2:]...)
		} else {
			i++
		}
	}
	return ms
}

// HighlightDoc marks the viewer's highlight words in a message's blocks
// (Style Highlight, which draws as c-mrkdwn__highlight).
func HighlightDoc(blocks []Block, words []string) {
	ws := prepWords(words)
	if len(ws) == 0 {
		return
	}
	for i := range blocks {
		highlightBlock(&blocks[i], ws)
	}
}

func highlightBlock(b *Block, ws [][]rune) {
	switch b.Kind {
	case Paragraph, Quote, Preformatted, Header, Section, Context:
		b.Spans = highlightSpans(b.Spans, ws)
	case List:
		for i := range b.Items {
			b.Items[i] = highlightSpans(b.Items[i], ws)
		}
	}
	for i := range b.Fields {
		b.Fields[i] = highlightSpans(b.Fields[i], ws)
	}
	for i := range b.Children {
		highlightBlock(&b.Children[i], ws)
	}
}

// highlightSpans is processChunk + the split: text and links contribute
// their text, a line break "\n", anything else (mentions, emoji) one
// space; matched runs of Text and Link spans get Style Highlight.
func highlightSpans(spans []Span, ws [][]rune) []Span {
	if len(spans) == 0 {
		return spans
	}
	var all []rune
	starts := make([]int, len(spans))
	for i, sp := range spans {
		starts[i] = len(all)
		switch sp.Kind {
		case Text, Link, Date:
			all = append(all, []rune(sp.Text)...)
		case Break:
			all = append(all, '\n')
		default:
			all = append(all, ' ')
		}
	}
	ms := locate(all, ws, true)
	if len(ms) == 0 {
		return spans
	}
	on := make([]bool, len(all))
	for _, m := range ms {
		for k := m.Index; k < m.Index+m.Length && k < len(on); k++ {
			on[k] = true
		}
	}
	out := make([]Span, 0, len(spans)+2*len(ms))
	for i, sp := range spans {
		if sp.Kind != Text && sp.Kind != Link {
			out = append(out, sp)
			continue
		}
		rs := []rune(sp.Text)
		base := starts[i]
		for j := 0; j < len(rs); {
			k := j
			for k < len(rs) && on[base+k] == on[base+j] {
				k++
			}
			part := sp
			part.Text = string(rs[j:k])
			if on[base+j] {
				part.Style |= Highlight
			}
			out = append(out, part)
			j = k
		}
	}
	return out
}

// emojiTexter is a Resolver that knows the viewer's emoji_mode.
type emojiTexter interface{ EmojiAsText() bool }

// EmojiToText is convertEmojiToText (boot-shared BaseRichText, under
// shouldRenderEmojiAsText, emoji_mode=as_text): every emoji becomes its
// text, ":name:" (":name::skin-tone-N:"), keeping its style.
func EmojiToText(blocks []Block) {
	for i := range blocks {
		b := &blocks[i]
		emojiSpansToText(b.Spans)
		for _, it := range b.Items {
			emojiSpansToText(it)
		}
		for _, f := range b.Fields {
			emojiSpansToText(f)
		}
		for _, row := range b.Rows {
			for _, cell := range row {
				emojiSpansToText(cell)
			}
		}
		EmojiToText(b.Children)
	}
}

func emojiSpansToText(spans []Span) {
	for i := range spans {
		if sp := &spans[i]; sp.Kind == Emoji && sp.ID != "" {
			*sp = Span{Kind: Text, Text: ":" + sp.ID + ":", Style: sp.Style}
		}
	}
}
