package composer

import (
	"encoding/json"
	"strings"
)

// Slack's own copies carry the message as the composer's Quill delta,
// "slack/texty" (Copy message: message-actions-copy-text 4EhV, the
// blocks through convertBlocksToDelta), and the selection as "slack/html"
// (the clipboard handler SC+N: the DOM with its data-stringify marks).
// Chromium keeps both in its web custom data (internal/ui/richclip).
// Texty pastes them before text/html, so a mention stays a mention and a
// quote a quote.
//
// textyDoc reads a slack/texty delta the way Quill applies it: each op's
// text with its inline formats (bold, italic, strike, code, underline, a
// link), a slackmention as its mention, an embedded emoji as its :name:
// (the paste's convertEmoji makes it the emoji again), a divider
// (slackhorizontalrule) a plain line break (prepareDeltaForPaste, +hBz,
// with no portable content), and each "\n" ending its line with that
// line's format: a quote, a list item and its indent, a code line, a list
// or code line inside a quote (border). The delta's last "\n" ends the
// last line. nil for anything that isn't a delta.
func textyDoc(s string) *Doc {
	if s == "" || len(s) > phMaxHTML {
		return nil
	}
	var d struct {
		Ops []struct {
			Insert     json.RawMessage `json:"insert"`
			Attributes map[string]any  `json:"attributes"`
		} `json:"ops"`
	}
	if json.Unmarshal([]byte(s), &d) != nil || len(d.Ops) == 0 {
		return nil
	}
	doc := &Doc{}
	endsLine := false // the last rune is a line's closing "\n"
	bare := false     // the last op inserted was a bare "\n"
	endLine := func(at qAttrs) {
		doc.Lines = append(doc.Lines, qLine(at))
		doc.Runes = append(doc.Runes, '\n')
		doc.Attrs = append(doc.Attrs, Attr{})
		endsLine = true
	}
	for _, op := range d.Ops {
		at := textyAttrs(op.Attributes)
		var text string
		if json.Unmarshal(op.Insert, &text) != nil {
			var emb struct {
				Emoji *struct {
					Text string `json:"text"`
				} `json:"slackemoji"`
				Rule any `json:"slackhorizontalrule"`
			}
			switch {
			case json.Unmarshal(op.Insert, &emb) != nil:
				continue
			case emb.Emoji != nil:
				text, at = emb.Emoji.Text, nil
			case truthy(emb.Rule):
				endLine(nil)
				continue
			default:
				continue
			}
		}
		if m, ok := op.Attributes["slackmention"].(map[string]any); ok && !strings.Contains(text, "\n") {
			id, _ := m["id"].(string)
			label, _ := m["label"].(string)
			if label == "" {
				label = text
			}
			if a := mentionAtom(strings.TrimSpace(id), strings.TrimSpace(label)); a != nil && len(id) <= 64 && len(label) <= 256 {
				doc.Runes = append(doc.Runes, ObjRune)
				doc.Attrs = append(doc.Attrs, Attr{Atom: a})
				endsLine = false
				continue
			}
		}
		bare = text == "\n"
		a := qAttr(at)
		if a.Link != "" {
			// as the HTML path's links: javascript:, file: and the like go
			a.Link = phSafeURL(a.Link)
		}
		for _, r := range text {
			switch r {
			case '\n':
				endLine(at)
				continue
			case '\r', ObjRune:
				r = ' '
			}
			doc.Runes = append(doc.Runes, r)
			doc.Attrs = append(doc.Attrs, a)
			endsLine = false
		}
		if len(doc.Runes) > phMaxRunes {
			return nil
		}
	}
	if endsLine {
		// the delta's last "\n" closes the last line: no line after it
		doc.Runes, doc.Attrs = doc.Runes[:len(doc.Runes)-1], doc.Attrs[:len(doc.Attrs)-1]
		// prepareDeltaForPaste into a quote: a last op that isn't a bare
		// "\n" gets another, a quote line of its own
		doc.quoteTail = !bare
	} else {
		doc.Lines = append(doc.Lines, Line{}) // no closing "\n": a paragraph
	}
	if len(doc.Runes) == 0 && len(doc.Lines) == 1 {
		return nil
	}
	return doc
}

// textyAttrs are a delta op's attributes as the composer's delta reads
// them: JSON numbers (an indent, a border, a header level) as ints.
func textyAttrs(m map[string]any) qAttrs {
	if len(m) == 0 {
		return nil
	}
	at := make(qAttrs, len(m))
	for k, v := range m {
		if f, ok := v.(float64); ok {
			v = int(f)
		}
		at[k] = v
	}
	return at
}
