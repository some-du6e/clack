package fakeslack

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

// The strict chat.command check reads convertDeltaToBlocks' K with
// JavaScript's classes: a no-break or ideographic space before the spaces
// is white space, so "a<N\P> \nb" is what Slack's client sends; trailing
// spaces after a word or a line's start (a CR, U+2028) before a line
// break are refused.
func TestCommandTextSpacesBeforeBreak(t *testing.T) {
	block := func(text string) url.Values {
		b, _ := json.Marshal([]any{map[string]any{"type": "rich_text", "elements": []any{map[string]any{
			"type": "rich_text_section", "elements": []any{map[string]any{"type": "text", "text": text}}}}}})
		return url.Values{"blocks": {string(b)}}
	}
	for _, c := range []struct {
		text string
		bad  bool
	}{
		{"a\u00a0 \nb", false},
		{"a\u3000 \nb", false},
		{"a\t \nb", false},
		{"a b\nc", false},
		{"a \nb", true},
		{"a  \nb", true},
		{"a\r \nb", true},
		{"a\u2028 \nb", true},
	} {
		_, err := commandText(block(c.text))
		if refused := err != nil && strings.Contains(err.Error(), "spaces before a line break"); refused != c.bad {
			t.Errorf("%q: %v, want refused %v", c.text, err, c.bad)
		}
	}
}
