package quip

import (
	"html"
	"strings"
	"unicode/utf8"
)

// A Run is a piece of a text section's new content as an editor holds it:
// text with its formatting and link, a line break, or one of the
// section's controls (a mention, emoji or date: atomic, so an editor can
// keep, move or delete one but never change it).
type Run struct {
	Text    string // plain text, unescaped ("" for a break or a control)
	Break   bool   // a line break (<br>)
	Control string // the id of one of the base section's controls
	// formatting (b, i, u, del, code) and a link around the text
	Bold, Italic, Underline, Strike, Code bool
	Href                                  string
}

// unit is one character of a section's text as ParseRTML gives it: a
// character (its escaped form), a line break or a control.
type unit struct {
	s          string // as written in the text: "a", "&amp;", "<br>", `<control id="…"></control>`
	ctl        string // a control's id
	br         bool
	start, end int // its bytes in the text it came from
}

func (u unit) key() string {
	switch {
	case u.ctl != "":
		return "\x00c" + u.ctl
	case u.br:
		return "\x00b"
	}
	k := html.UnescapeString(u.s)
	if k == "\u00a0" { // an editor shows a no-break space as a space
		return " "
	}
	return k
}

// units splits ParseRTML's text into characters, line breaks and controls.
func units(text string) []unit {
	var out []unit
	for i := 0; i < len(text); {
		switch {
		case strings.HasPrefix(text[i:], "<br>"):
			out = append(out, unit{s: "<br>", br: true, start: i, end: i + 4})
			i += 4
			continue
		case strings.HasPrefix(text[i:], `<control id="`):
			if j := strings.Index(text[i:], `"></control>`); j > 0 {
				id := text[i+len(`<control id="`) : i+j]
				n := j + len(`"></control>`)
				out = append(out, unit{s: text[i : i+n], ctl: id, start: i, end: i + n})
				i += n
				continue
			}
		case text[i] == '&':
			if j := strings.IndexByte(text[i:], ';'); j > 0 && j < 12 {
				out = append(out, unit{s: text[i : i+j+1], start: i, end: i + j + 1})
				i += j + 1
				continue
			}
		}
		_, n := utf8.DecodeRuneInString(text[i:])
		out = append(out, unit{s: text[i : i+n], start: i, end: i + n})
		i += n
	}
	return out
}

// RTMLControls are the ids of the controls in a section's text, in order.
func RTMLControls(rtml string) ([]string, bool) {
	text, _, ok := ParseRTML(rtml)
	if !ok {
		return nil, false
	}
	var ids []string
	for _, u := range units(text) {
		if u.ctl != "" {
			ids = append(ids, u.ctl)
		}
	}
	return ids, true
}

// RTMLPlain is a section's text as plain text, each control as
// ControlMark and each line break as "\n": what an editor showing it
// holds, for telling whether the section is still what an edit began from.
func RTMLPlain(rtml string) (string, bool) {
	text, _, ok := ParseRTML(rtml)
	if !ok {
		return "", false
	}
	var b strings.Builder
	for _, u := range units(text) {
		switch {
		case u.ctl != "":
			b.WriteRune(ControlMark)
		case u.br:
			b.WriteByte('\n')
		default:
			b.WriteString(html.UnescapeString(u.s))
		}
	}
	return b.String(), true
}

// ControlMark stands for a control in RTMLPlain (U+FFFC, the object
// replacement character).
const ControlMark = '\ufffc'

// maxDiff bounds the diff that carries marks Clack's editor doesn't hold
// (comment anchors) through an edit: past it, only the common start and
// end of the text keep them.
const maxDiff = 4_000_000

// ApplyRuns is a text section's RTML after an edit: the base section's
// text replaced by runs, written as the editor's getCompactRtml writes
// text and marks. Formatting and links are the runs'. What the editor
// doesn't hold of the base keeps covering the characters it covered, as
// Slack's withRtml moves marks with their characters: comment anchors
// (annotation), and a link's nolink and slackFileId when the run keeps its
// href over the same text. A mark whose characters are all gone goes away,
// so no mark is ever empty (getCompactRtml would lose the text after it).
// It refuses a run naming a control the base doesn't have, or one used
// twice.
func ApplyRuns(base string, runs []Run) (string, error) {
	bt, bm, ok := ParseRTML(base)
	if !ok {
		return "", unsupported("RTML Slack can't parse")
	}
	bu := units(bt)
	have := map[string]bool{}
	for _, u := range bu {
		if u.ctl != "" {
			have[u.ctl] = true
		}
	}
	// the new units, each with the run it came from
	var nu []unit
	var from []int
	used := map[string]bool{}
	for ri, r := range runs {
		switch {
		case r.Control != "":
			if !have[r.Control] || used[r.Control] {
				return "", unsupported("a control the section doesn't hold (%s)", r.Control)
			}
			used[r.Control] = true
			nu = append(nu, unit{s: `<control id="` + r.Control + `"></control>`, ctl: r.Control})
			from = append(from, ri)
		case r.Break:
			nu = append(nu, unit{s: "<br>", br: true})
			from = append(from, ri)
		default:
			for _, c := range r.Text {
				e := EscapeText(string(c), false)
				if e == "" { // a control character: escapeText drops it
					continue
				}
				nu = append(nu, unit{s: e})
				from = append(from, ri)
			}
		}
	}
	match := matchUnits(bu, nu) // base unit -> new unit, -1 when gone
	kept := make([]bool, len(nu))
	for bi, ni := range match {
		if ni >= 0 {
			kept[ni] = true
			nu[ni].s = bu[bi].s // the base's own form: its entities, its no-break spaces
		}
	}
	if err := checkSpaces(nu, kept); err != nil {
		return "", err
	}
	// the new text, and where each unit sits in it
	var b strings.Builder
	for i := range nu {
		nu[i].start = b.Len()
		b.WriteString(nu[i].s)
		nu[i].end = b.Len()
	}
	text := b.String()

	var marks []Mark
	// formatting: a mark over each stretch of units whose runs share it
	style := func(name string, on func(Run) bool) {
		for i := 0; i < len(nu); {
			if !on(runs[from[i]]) {
				i++
				continue
			}
			j := i
			for j < len(nu) && on(runs[from[j]]) {
				j++
			}
			marks = append(marks, Mark{Name: name, Flag: true, Start: nu[i].start, End: nu[j-1].end})
			i = j
		}
	}
	style("b", func(r Run) bool { return r.Bold })
	style("i", func(r Run) bool { return r.Italic })
	style("u", func(r Run) bool { return r.Underline })
	style("del", func(r Run) bool { return r.Strike })
	style("code", func(r Run) bool { return r.Code })
	// links: one mark per stretch with the same href; the base link's
	// value when the stretch is all characters it covered
	baseLink := func(i int) (Mark, bool) { // the base link over new unit i's base character
		for bi, ni := range match {
			if ni != i {
				continue
			}
			for _, m := range bm {
				if m.Name == "a" && m.Start <= bu[bi].start && bu[bi].end <= m.End {
					return m, true
				}
			}
		}
		return Mark{}, false
	}
	for i := 0; i < len(nu); {
		h := runs[from[i]].Href
		if h == "" {
			i++
			continue
		}
		j := i
		for j < len(nu) && runs[from[j]].Href == h {
			j++
		}
		val := linkValue(h, false, "")
		if m, ok := baseLink(i); ok {
			if l, ok := parseLinkValue(m.Value); ok && l.href == h {
				same := true
				for k := i; k < j && same; k++ {
					m2, ok := baseLink(k)
					same = ok && m2 == m
				}
				if same {
					val = m.Value
				}
			}
		}
		if !IsSafeURI(h, false) {
			return "", unsupported("a link Slack wouldn't keep")
		}
		marks = append(marks, Mark{Name: "a", Value: val, Start: nu[i].start, End: nu[j-1].end})
		i = j
	}
	// comment anchors: over the first to the last of their characters
	// that are still there
	for _, m := range bm {
		if m.Name != "annotation" {
			continue
		}
		lo, hi := -1, -1
		for bi, u := range bu {
			if u.start >= m.Start && u.end <= m.End && match[bi] >= 0 {
				if lo < 0 || match[bi] < lo {
					lo = match[bi]
				}
				if match[bi] > hi {
					hi = match[bi]
				}
			}
		}
		if lo < 0 {
			continue // all its characters gone: the anchor goes with them
		}
		marks = append(marks, Mark{Name: "annotation", Value: m.Value, Start: nu[lo].start, End: nu[hi].end})
	}
	out, ok := CompactRTML(text, marks)
	if !ok {
		return "", unsupported("marks getCompactRtml can't write")
	}
	return out, nil
}

// checkSpaces refuses spaces typed where the browser would hold them as
// no-break spaces: a space run of two or more, or one at the text's start,
// its end or a line break, that the edit made or typed next to. A single
// typed space between two other characters is a plain space, as Chromium's
// whitespace rebalancing leaves it; runs the edit didn't touch keep the
// base's forms.
func checkSpaces(nu []unit, kept []bool) error {
	space := func(i int) bool { return i >= 0 && i < len(nu) && nu[i].key() == " " }
	for i := 0; i < len(nu); {
		if !space(i) {
			i++
			continue
		}
		j := i
		for space(j) {
			j++
		}
		touched := i > 0 && !kept[i-1] || j < len(nu) && !kept[j]
		for k := i; k < j && !touched; k++ {
			touched = !kept[k]
		}
		edge := i == 0 || j == len(nu) || nu[i-1].br || nu[j].br
		if touched && (j-i > 1 || edge) {
			return unsupported("spaces the browser would hold as no-break spaces")
		}
		if touched {
			nu[i].s = " "
		}
		i = j
	}
	return nil
}

// matchUnits pairs base units with the new units that are the same
// characters, as a diff does: the common start and end, then the longest
// common subsequence of the rest when it's small enough to work out.
func matchUnits(bu, nu []unit) []int {
	match := make([]int, len(bu))
	for i := range match {
		match[i] = -1
	}
	p := 0
	for p < len(bu) && p < len(nu) && bu[p].key() == nu[p].key() {
		match[p] = p
		p++
	}
	s := 0
	for s < len(bu)-p && s < len(nu)-p && bu[len(bu)-1-s].key() == nu[len(nu)-1-s].key() {
		match[len(bu)-1-s] = len(nu) - 1 - s
		s++
	}
	b, n := bu[p:len(bu)-s], nu[p:len(nu)-s]
	if len(b) == 0 || len(n) == 0 || len(b)*len(n) > maxDiff {
		return match
	}
	// LCS over the middle
	w := len(n) + 1
	t := make([]int32, (len(b)+1)*w)
	for i := len(b) - 1; i >= 0; i-- {
		for j := len(n) - 1; j >= 0; j-- {
			if b[i].key() == n[j].key() {
				t[i*w+j] = t[(i+1)*w+j+1] + 1
			} else {
				t[i*w+j] = max(t[(i+1)*w+j], t[i*w+j+1])
			}
		}
	}
	for i, j := 0, 0; i < len(b) && j < len(n); {
		switch {
		case b[i].key() == n[j].key():
			match[p+i] = p + j
			i++
			j++
		case t[(i+1)*w+j] >= t[i*w+j+1]:
			i++
		default:
			j++
		}
	}
	return match
}
