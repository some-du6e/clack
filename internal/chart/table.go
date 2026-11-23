package chart

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Cell is a table cell: its text, or none (Null: a series with no value at
// a category).
type Cell struct {
	Text string
	Null bool
}

// Table is convertDataVisualizationToTable (boot-shared F8Uw) over the
// raw payload: a pie's "Label" and "Value" per segment, a cartesian
// chart's "Series" and its categories, then each series' name and its
// value at each category (looked up by the raw label, as Map does).
// View as table draws it (convertDataVisualizationToTableBlock's header
// and body cells); Download chart data writes it as TSV.
func (d *Data) Table() (headers []string, rows [][]Cell) {
	if d == nil || d.Err != nil || !d.Supported {
		return nil, nil
	}
	if d.Type == "pie" {
		headers = []string{"Label", "Value"}
		for _, p := range d.raw.segments {
			rows = append(rows, []Cell{cellOf(p.label, p.hasLabel), cellOf(p.value, p.hasValue)})
		}
		return
	}
	headers = []string{"Series"}
	for _, c := range d.raw.categories {
		headers = append(headers, jsString(c, true))
	}
	for i, s := range d.raw.series {
		row := []Cell{cellOf(s.name, s.hasName)}
		byLabel := map[jsKey]rawPoint{}
		for j, p := range s.points {
			byLabel[keyOf(p.label, p.hasLabel, -1-j-i*1000)] = p // a.set(t.label, t.value): the last one stays
		}
		for k, c := range d.raw.categories {
			if p, ok := byLabel[keyOf(c, true, k)]; ok {
				row = append(row, cellOf(p.value, p.hasValue))
			} else {
				row = append(row, Cell{Null: true})
			}
		}
		rows = append(rows, row)
	}
	return
}

// cellOf is a raw value as a table cell: null is no value, the rest their
// String() (undefined joins as "").
func cellOf(v any, present bool) Cell {
	if present && v == nil {
		return Cell{Null: true}
	}
	if !present {
		return Cell{Text: ""}
	}
	return Cell{Text: jsString(v, true)}
}

var tsvSpecial = regexp.MustCompile("[\t\"\r\n]")

// TSV is convertDataVisualizationToTsv: the table's rows, cells joined by
// tabs, a cell holding a tab, a quote or a line break quoted (its quotes
// doubled); "" when there are no headers.
func (d *Data) TSV() string {
	headers, rows := d.Table()
	if len(headers) == 0 {
		return ""
	}
	cells := func(cs []Cell) string {
		out := make([]string, len(cs))
		for i, c := range cs {
			if c.Null {
				continue
			}
			t := c.Text
			if tsvSpecial.MatchString(t) {
				t = `"` + strings.ReplaceAll(t, `"`, `""`) + `"`
			}
			out[i] = t
		}
		return strings.Join(out, "\t")
	}
	hc := make([]Cell, len(headers))
	for i, h := range headers {
		hc[i] = Cell{Text: h}
	}
	lines := []string{cells(hc)}
	for _, r := range rows {
		lines = append(lines, cells(r))
	}
	return strings.Join(lines, "\n")
}

// FilenameSlug is SAx4's filenameSlugFromLabel: NFKD, without what isn't a
// word character, a space or a hyphen, each run of spaces and hyphens one
// hyphen, lowercased; fallback when nothing is left.
func FilenameSlug(label, fallback string) string {
	s := norm.NFKD.String(label)
	var kept strings.Builder
	for _, r := range s {
		switch {
		case r < 0x80 && (r == '_' || r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r)):
			kept.WriteRune(r)
		case jsSpace(r):
			kept.WriteRune(r)
		}
	}
	var b strings.Builder
	dash := false
	for _, r := range strings.TrimFunc(kept.String(), jsSpace) {
		if jsSpace(r) || r == '-' {
			if !dash {
				b.WriteByte('-')
				dash = true
			}
			continue
		}
		b.WriteRune(unicode.ToLower(r))
		dash = false
	}
	if b.Len() == 0 {
		return fallback
	}
	return b.String()
}

// jsSpace is JavaScript's \s: white space and line terminators.
func jsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}
