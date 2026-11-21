package blockkit

// Slack's rich text truncation, as a slim plan task's and a slim task
// card's details and output use it (TaskItem's q: ZJNP TruncatedBlocks
// with maxLines 3, maxChars 250, forceBlockLevelExpandButton): client
// boot-shared BLFT truncateRichTextElements, build 9685e6ce144b. Lengths
// are JavaScript's (UTF-16 code units); the cut never splits a surrogate
// pair (sliceWholeCodePoints).
//
// A block boundary costs a line, as does each list item but the last; a
// divider costs nothing. A text or link run that brings the lines past
// the budget is cut at the last line that fits ("section": no ellipsis);
// one that runs past the characters is cut there, its trailing space and
// any trailing ellipsis or ".,_-" trimmed, and gets the truncation element
// ("inline", drawn as an ellipsis). Then a section of its own holds
// _truncation_expand_button (Show more); an expanded one ends with
// _truncation_collapse_button (Show less).

import (
	"encoding/json"
	"math"
	"strings"
	"unicode"
	"unicode/utf16"
)

// Slack's truncation elements (BLFT j and E).
const (
	rtTruncation   = "truncation"
	rtExpandButton = "_truncation_expand_button"
)

// truncatedRich is the result of truncating a rich_text block's elements.
type truncatedRich struct {
	Elements       []any `json:"elements"`
	IsTruncated    bool  `json:"isTruncated"`
	CharsConsumed  int   `json:"charsConsumed"`
	LinesConsumed  int   `json:"linesConsumed"`
	truncationType string
}

// jsLen is a string's JavaScript length (UTF-16 code units).
func jsLen(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// jsSlice is s.slice(0, n) in UTF-16 code units, less a trailing high
// surrogate (sliceWholeCodePoints).
func jsSlice(s string, n int) string {
	if n <= 0 {
		return ""
	}
	u := utf16.Encode([]rune(s))
	if n >= len(u) {
		return s
	}
	u = u[:n]
	if c := u[len(u)-1]; c >= 0xD800 && c <= 0xDBFF {
		u = u[:len(u)-1]
	}
	return string(utf16.Decode(u))
}

// jsSpace is JavaScript's \s (lodash trimEnd without chars).
func jsSpace(r rune) bool { return r != 0x85 && unicode.IsSpace(r) || r == 0xfeff }

// trimEndChars is lodash trimEnd(s, chars).
func trimEndChars(s, chars string) string { return strings.TrimRight(s, chars) }

// chunkLen is BLFT getChunkLength for a leaf element.
func chunkLen(e map[string]any) int {
	str := func(k string) (string, bool) { v, ok := e[k].(string); return v, ok }
	idLen := func(k string) int {
		if v, ok := str(k); ok && v != "" {
			return jsLen(v) + 1
		}
		return 10
	}
	switch e["type"] {
	case "user":
		if _, ok := e["user_id"]; ok {
			return idLen("user_id")
		}
	case "usergroup":
		if _, ok := e["usergroup_id"]; ok {
			return idLen("usergroup_id")
		}
	case "channel":
		if _, ok := e["channel_id"]; ok {
			return idLen("channel_id")
		}
	case "broadcast":
		return idLen("range")
	case "date":
		v, _ := str("fallback")
		return jsLen(v)
	case "color":
		if v, ok := str("value"); ok && v != "" {
			return jsLen(v) + 1
		}
		return 7
	case "tag":
		v, _ := str("text")
		return jsLen(v)
	case "work_object_mention":
		if v, ok := str("text"); ok && v != "" {
			return jsLen(v)
		}
		v, _ := str("url")
		return jsLen(v)
	case "emoji":
		return 1
	case "text", "link":
		v, _ := str("text")
		return jsLen(v)
	}
	return 0
}

func cloneMap(m map[string]any) map[string]any {
	c := make(map[string]any, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

// truncateRichTextElements is BLFT's, with includeTruncationElements and
// forceBlockLevelExpandButton on (a task's content) and not expanded.
func truncateRichTextElements(els []any, maxChars, maxLines int) truncatedRich {
	if len(els) == 0 || maxChars <= 0 {
		return truncatedRich{Elements: els}
	}
	m := maxChars
	inf := maxLines <= 0
	p := maxLines
	if inf {
		p = math.MaxInt32
	}
	h := "none"
	var y []any
	process := func(lf any) any {
		e, ok := lf.(map[string]any)
		if !ok {
			return lf
		}
		t := chunkLen(e)
		if typ := e["type"]; typ == "text" || typ == "link" {
			a := cloneMap(e)
			text, _ := e["text"].(string)
			var lines []string
			if text != "" {
				lines = strings.Split(text, "\n")
			}
			o := text
			if !inf && len(lines) > p {
				o = strings.Join(lines[:max(p, 0)], "\n")
				p, h = 0, "section"
			}
			if t = jsLen(o); t > m {
				o = trimEndChars(strings.TrimRightFunc(jsSlice(text, m), jsSpace), "\u2026.,_-")
				h = "inline"
			}
			a["text"] = o
			if h == "none" {
				n := len(lines) - 1
				if strings.HasSuffix(o, "\n") {
					n = len(lines) - 2
				}
				if n > 0 && !inf {
					p -= n
				}
			}
			m -= t
			return a
		}
		m -= t
		return lf
	}
	for i, nAny := range els {
		last := i == len(els)-1
		n, ok := nAny.(map[string]any)
		sub, has := n["elements"].([]any)
		if !ok || !has || n["type"] == "rich_text_divider" {
			y = append(y, nAny)
			continue
		}
		c := cloneMap(n)
		var ce []any
		if n["type"] == "rich_text_list" {
			for j, itAny := range sub {
				lastItem := j == len(sub)-1
				it, _ := itAny.(map[string]any)
				ic := cloneMap(it)
				var ie []any
				leaves, _ := it["elements"].([]any)
				for _, lf := range leaves {
					ie = append(ie, process(lf))
					if h != "none" {
						break
					}
				}
				ic["elements"] = nonNil(ie)
				ce = append(ce, ic)
				if !lastItem {
					if !inf {
						p--
					}
					if h == "none" && p <= 0 {
						h = "section"
					}
				}
				if h != "none" {
					break
				}
			}
		} else {
			for _, lf := range sub {
				ce = append(ce, process(lf))
				if h != "none" {
					break
				}
			}
		}
		c["elements"] = nonNil(ce)
		y = append(y, c)
		if h != "none" {
			break
		}
		if !last {
			if !inf {
				p--
			}
			if p <= 0 {
				h = "section"
			}
		}
		if h != "none" {
			break
		}
	}
	lines := 0
	if !inf {
		lines = maxLines - p
	}
	out := truncatedRich{CharsConsumed: maxChars - m, LinesConsumed: lines, truncationType: h}
	if h == "none" {
		out.Elements = els
		return out
	}
	out.IsTruncated = true
	trunc := map[string]any{"type": rtTruncation, "isMessageTruncation": true}
	if h == "inline" {
		lastEl := y[len(y)-1].(map[string]any)
		switch lastEl["type"] {
		case "rich_text_quote", "rich_text_preformatted":
			lastEl["elements"] = append(lastEl["elements"].([]any), trunc)
		case "rich_text_list":
			items := lastEl["elements"].([]any)
			li := items[len(items)-1].(map[string]any)
			li["elements"] = append(li["elements"].([]any), trunc)
		default:
			lastEl["elements"] = append(lastEl["elements"].([]any), trunc)
		}
	}
	y = append(y, map[string]any{"type": "rich_text_section", "elements": []any{
		map[string]any{"type": rtExpandButton, "isMessageTruncation": true}}})
	out.Elements = y
	return out
}

func nonNil(xs []any) []any {
	if xs == nil {
		return []any{}
	}
	return xs
}

// Slim task content's limits (TaskItem q).
const (
	taskContentMaxChars = 250
	taskContentMaxLines = 3
)

// truncateTaskContent cuts a task's details or output rich_text block the
// way the slim TaskItem draws it collapsed: the block with the cut
// elements, the truncation element drawn as an ellipsis (a text run here), and the
// expand section left out (the caller draws Show more under it).
func truncateTaskContent(raw json.RawMessage) (json.RawMessage, bool) {
	var blk map[string]any
	if json.Unmarshal(raw, &blk) != nil {
		return raw, false
	}
	els, _ := blk["elements"].([]any)
	r := truncateRichTextElements(els, taskContentMaxChars, taskContentMaxLines)
	if !r.IsTruncated {
		return raw, false
	}
	var kept []any
	var ell func(v any) any
	ell = func(v any) any {
		e, ok := v.(map[string]any)
		if !ok {
			return v
		}
		if e["type"] == rtTruncation {
			return map[string]any{"type": "text", "text": "\u2026"}
		}
		if sub, ok := e["elements"].([]any); ok {
			c := cloneMap(e)
			out := make([]any, len(sub))
			for i, s := range sub {
				out[i] = ell(s)
			}
			c["elements"] = out
			return c
		}
		return v
	}
	for _, e := range r.Elements {
		if m, ok := e.(map[string]any); ok && m["type"] == "rich_text_section" {
			if sub, _ := m["elements"].([]any); len(sub) == 1 {
				if b, ok := sub[0].(map[string]any); ok && b["type"] == rtExpandButton {
					continue
				}
			}
		}
		kept = append(kept, ell(e))
	}
	c := cloneMap(blk)
	c["elements"] = nonNil(kept)
	out, err := json.Marshal(c)
	if err != nil {
		return raw, false
	}
	return out, true
}
