package richclip

import (
	"encoding/json"
	"strings"

	"github.com/some-du6e/clack/internal/richtext"
)

// TextyDelta is a message's "slack/texty": the Quill delta Slack's Copy
// message puts on the clipboard (message-actions-copy-text 4EhV:
// convertBlocksToDelta, V/2c), JSON-encoded as {"ops": […]}. Text keeps
// its bold, italic, strike and code; a link its link; a member, user
// group, channel or broadcast is a slackmention (makeSlackmention 1trP:
// id, label, mention, unverified) on its label, which Slack's composer
// (and Clack's, paste_texty.go) pastes as a mention; quotes, lists and
// code blocks are the newline's line format. "" for an empty doc.
func TextyDelta(d richtext.Doc) string {
	type op struct {
		Insert     string         `json:"insert"`
		Attributes map[string]any `json:"attributes,omitempty"`
	}
	var ops []op
	add := func(s string, a map[string]any) {
		if s == "" {
			return
		}
		if len(a) == 0 && len(ops) > 0 && len(ops[len(ops)-1].Attributes) == 0 {
			ops[len(ops)-1].Insert += s
			return
		}
		ops = append(ops, op{Insert: s, Attributes: a})
	}
	styled := func(st richtext.Style) map[string]any {
		a := map[string]any{}
		if st&richtext.Bold != 0 {
			a["bold"] = true
		}
		if st&richtext.Italic != 0 {
			a["italic"] = true
		}
		if st&richtext.Strike != 0 {
			a["strike"] = true
		}
		if st&richtext.Code != 0 {
			a["code"] = true
		}
		return a
	}
	mention := func(id, label string, self bool, st richtext.Style) {
		a := styled(st)
		a["slackmention"] = map[string]any{"id": id, "label": label, "mention": self, "unverified": false}
		add(label, a)
	}
	spans := func(sp []richtext.Span, nl map[string]any) {
		for _, s := range sp {
			switch s.Kind {
			case richtext.Break:
				add("\n", nl)
			case richtext.User:
				mention(s.ID, s.Text, s.Self, s.Style)
			case richtext.Usergroup:
				mention(s.ID, s.Text, s.Self, s.Style)
			case richtext.Broadcast:
				mention("BK"+s.ID, s.Text, s.Self, s.Style)
			case richtext.Channel:
				if s.ID != "" && !s.Off {
					mention(s.ID, s.Text, false, s.Style)
				} else {
					add(s.Text, styled(s.Style))
				}
			case richtext.Emoji:
				add(":"+s.ID+":", nil)
			case richtext.Link:
				a := styled(s.Style)
				if s.URL != "" {
					a["link"] = s.URL
				}
				add(s.Text, a)
			default:
				add(s.Text, styled(s.Style))
			}
		}
	}
	for i := range d.Blocks {
		b := &d.Blocks[i]
		switch b.Kind {
		case richtext.Paragraph, richtext.Header, richtext.Context, richtext.Section:
			spans(b.Spans, nil)
			add("\n", nil)
		case richtext.Quote:
			nl := map[string]any{"blockquote": true}
			spans(b.Spans, nl)
			ops = append(ops, op{Insert: "\n", Attributes: nl})
		case richtext.Preformatted:
			nl := map[string]any{"code-block": true}
			for _, ln := range strings.Split(richtext.SpansText(b.Spans), "\n") {
				add(ln, nil)
				ops = append(ops, op{Insert: "\n", Attributes: nl})
			}
		case richtext.List:
			kind := "bullet"
			if b.Ordered {
				kind = "ordered"
			}
			for _, it := range b.Items {
				nl := map[string]any{"list": kind}
				if b.Indent > 0 {
					nl["indent"] = b.Indent
				}
				spans(it, nil)
				ops = append(ops, op{Insert: "\n", Attributes: nl})
			}
		}
	}
	if len(ops) == 0 {
		return ""
	}
	out, err := json.Marshal(struct {
		Ops []op `json:"ops"`
	}{ops})
	if err != nil {
		return ""
	}
	return string(out)
}
