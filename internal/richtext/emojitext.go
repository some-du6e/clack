package richtext

// EmojiResolver resolves emoji names (Resolver's Emoji).
type EmojiResolver interface {
	Emoji(name string) (url, text string, ok bool)
}

// EmojiText is Slack's FormatTextWithEmoji (G6jU): plain text whose
// :shortcodes: become emoji, everything else staying text (no mrkdwn).
// Unknown names stay as typed. Nil when s has no emoji, so callers can
// draw plain text.
func EmojiText(r EmojiResolver, s string) []Span {
	var out []Span
	last := 0
	for i := 0; i < len(s); i++ {
		if s[i] != ':' {
			continue
		}
		name, n := emojiAt(s, i)
		if n == 0 {
			continue
		}
		u, text, ok := r.Emoji(name)
		if !ok {
			continue
		}
		if text == "" {
			text = ":" + name + ":"
		}
		if i > last {
			out = append(out, Span{Kind: Text, Text: s[last:i]})
		}
		out = append(out, Span{Kind: Emoji, Text: text, URL: u, ID: name})
		i += n - 1
		last = i + 1
	}
	if out == nil {
		return nil
	}
	if last < len(s) {
		out = append(out, Span{Kind: Text, Text: s[last:]})
	}
	return out
}
