package richtext

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// A rich_text text element is one string in Slack's store, newlines and
// all, and BaseText (modern.vendor KYzC, build 35b42de8b870) cuts it at
// its newlines only as it renders: insertTextBreakPlaceholders, then a
// <br> or a c-mrkdwn__br for each placeholder. Slack's message pane cuts
// nothing off (Message, boot-shared b5pm, renders Blocks lby4 without
// truncateLimits; MessageBlocks u33y and RichTextRenderer XglZ draw every
// element), so a message of 908,000 characters in 92,000 lines is 92,000
// lines on screen.
//
// Clack's Doc holds a text element's lines as Text spans with a Break
// between each two (appendRichText), two 144-byte spans a line: 26 MB
// for such a message, 2.6 GB for a channel of a hundred. An element with
// RawLines newlines or more is kept the way Slack's store keeps it: one
// Text span holding the whole text, its Br one of the raw kinds (BrRaw,
// BrRawLead, BrRawFirst for text, BrRawPre for preformatted text). It
// stands for exactly the spans the element would otherwise be cut into,
// which Pieces yields one at a time: richview lays those out, PlainText
// writes the same text, the truncation budgets count the same newlines,
// and HighlightDoc and FindHighlighted mark the same runs.

// RawLines is how many newlines a rich_text text element has before its
// Doc keeps its text whole (a raw span) rather than as a span a line.
var RawLines = 256

// IsRaw reports whether sp is a raw span: a rich_text text element's
// text kept whole (see RawLines).
func IsRaw(sp *Span) bool { return sp.Kind == Text && sp.Br >= BrRaw }

// rawKind is the raw kind of a text element's span: lead is
// isFirstElementInSection, first isFirstElementInFirstSection.
func rawKind(lead, first bool) BrKind {
	switch {
	case first:
		return BrRawFirst
	case lead:
		return BrRawLead
	}
	return BrRaw
}

// Pieces calls fn with the spans sp stands for, in order: for a raw span
// its text pieces (sp with that text, Br 0) and the Breaks between them,
// exactly as appendRichText (or a preformatted element's split) would have
// made them; any other span is passed as it is. fn returning false stops
// the walk; Pieces reports whether it ran to the end.
func Pieces(sp Span, fn func(Span) bool) bool {
	if !IsRaw(&sp) {
		return fn(sp)
	}
	piece := sp
	piece.Br = 0
	text := func(t string) bool {
		if t == "" {
			return true
		}
		piece.Text = t
		return fn(piece)
	}
	if sp.Br == BrRawPre {
		s := sp.Text
		for {
			i := strings.IndexByte(s, '\n')
			if i < 0 {
				return text(s)
			}
			if !text(s[:i]) || !fn(Span{Kind: Break}) {
				return false
			}
			s = s[i+1:]
		}
	}
	return eachRichText(sp.Text, sp.Br != BrRaw, sp.Br == BrRawFirst, text, func(k BrKind) bool { return fn(Span{Kind: Break, Br: k}) })
}

// Expand is spans with every raw span cut into the spans it stands for
// (Pieces). spans comes back as it is when none is raw.
func Expand(spans []Span) []Span {
	raw := false
	for i := range spans {
		if IsRaw(&spans[i]) {
			raw = true
			break
		}
	}
	if !raw {
		return spans
	}
	out := make([]Span, 0, len(spans))
	for _, sp := range spans {
		Pieces(sp, func(p Span) bool { out = append(out, p); return true })
	}
	return out
}

// runEnd is where a run of newlines that starts with s[i] == '\n' ends:
// past its last newline, each further one after nothing but spaces, tabs
// and no-break spaces (richRunRe's repetition, greedy). end == i+1 is a
// lone newline.
func runEnd(s string, i int) int {
	end := i + 1
	for j := end; j < len(s); {
		switch {
		case s[j] == ' ' || s[j] == '\t':
			j++
		case s[j] == 0xc2 && j+1 < len(s) && s[j+1] == 0xa0:
			j += 2
		case s[j] == '\n':
			j++
			end = j
		default:
			return end
		}
	}
	return end
}

// nextRun is richRunRe.FindStringIndex(s) without the regexp (it scanned
// a 908,000-character text a byte at a time): the first newline a run of
// two or more starts at, and the run's end; nil when there is none.
func nextRun(s string) []int {
	for i := 0; i < len(s); {
		k := strings.IndexByte(s[i:], '\n')
		if k < 0 {
			return nil
		}
		i += k
		if e := runEnd(s, i); e > i+1 {
			return []int{i, e}
		}
		i++
	}
	return nil
}

// eachRichText cuts a rich_text text element's text s the way Slack's
// BaseText draws it (BrKind): text calls for its pieces (some empty),
// brk for the breaks between them, in order, as appendRichText's spans.
// lead is isFirstElementInSection, first isFirstElementInFirstSection.
// A callback returning false stops; eachRichText reports whether it ran
// to the end. It allocates nothing a line, so a giant element can be
// walked whole.
func eachRichText(s string, lead, first bool, text func(string) bool, brk func(BrKind) bool) bool {
	if first {
		// trimStart (h5n5): the leading whitespace is gone, so its
		// newlines draw nothing and the rest starts on the first line
		// (spaces at a line's start draw nothing either)
		ws := len(s) - len(strings.TrimLeftFunc(s, unicode.IsSpace))
		for _, ln := range strings.SplitAfter(s[:ws], "\n") {
			if t, ok := strings.CutSuffix(ln, "\n"); ok {
				if !text(t) || !brk(BrNone) {
					return false
				}
			} else if !text(ln) {
				return false
			}
		}
		s, lead = s[ws:], false
	}
	// isFirstElementInSection && /^\n/: one more "\n" in front (no span
	// of its own), which makes a leading newline a run whose every
	// newline is a c-mrkdwn__br
	if lead && strings.HasPrefix(s, "\n") {
		run := s[:runEnd(s, 0)]
		for _, ln := range strings.Split(run, "\n")[1:] {
			if !brk(BrPara) || !text(ln) {
				return false
			}
		}
		s = s[len(run):]
	}
	for s != "" {
		loc := nextRun(s)
		if loc == nil {
			loc = []int{len(s), len(s)}
		}
		// before the run: single newlines are <br>
		for seg, first := s[:loc[0]], true; ; first = false {
			if !first && !brk(BrLine) {
				return false
			}
			i := strings.IndexByte(seg, '\n')
			if i < 0 {
				if !text(seg) {
					return false
				}
				break
			}
			if !text(seg[:i]) {
				return false
			}
			seg = seg[i+1:]
		}
		if loc[0] == loc[1] {
			break
		}
		// the run: k newlines are k-1 c-mrkdwn__br. The last draws
		// nothing; the spaces between them stay text at the start of an
		// empty line, where they draw nothing either.
		run := s[loc[0]:loc[1]]
		k := strings.Count(run, "\n")
		for j, ln := range strings.Split(run, "\n") {
			switch {
			case j == 0: // "" before the run's first "\n"
			case j == k:
				if !brk(BrNone) {
					return false
				}
			default:
				if !brk(BrPara) {
					return false
				}
			}
			if !text(ln) {
				return false
			}
		}
		s = s[loc[1]:]
	}
	return true
}

// rawCutSafe reports whether raw text s cut at byte i into two raw spans
// stands for the same spans as s whole: a cut inside the whitespace and
// newlines of a paragraph run would end the run early, and the leading
// whitespace of a first element is trimmed only at the element's start.
func rawCutSafe(s string, i int, br BrKind) bool {
	if i <= 0 || i >= len(s) {
		return true
	}
	isWS := func(r rune) bool { return r == '\n' || r == ' ' || r == '\t' || r == 0xa0 }
	a, b := i, i
	for a > 0 {
		r, n := utf8.DecodeLastRuneInString(s[:a])
		if !isWS(r) {
			break
		}
		a -= n
	}
	for b < len(s) {
		r, n := utf8.DecodeRuneInString(s[b:])
		if !isWS(r) {
			break
		}
		b += n
	}
	if strings.Contains(s[a:b], "\n") {
		return false
	}
	if br == BrRawFirst && strings.TrimLeftFunc(s[:i], unicode.IsSpace) == "" {
		return false // inside the leading whitespace trimStart drops
	}
	return true
}
