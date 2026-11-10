package composer

import "github.com/some-du6e/clack/internal/richtext"

// parseMarkup is the send side of "Format messages with markup" (Slack's
// plain_text_mode): the composer kept *bold*, _italic_, ~strike~, `code`,
// ```code blocks``` and "> " quotes as typed, and the message goes out
// formatted, the way Slack's send runs its markdown parser over the
// draft. The round trip goes through mrkdwn, so pills and emoji come back
// as the same mention and emoji elements. An empty parse keeps d.
func parseMarkup(d *Doc, r richtext.Resolver) *Doc {
	blocks := richtext.FromMrkdwn(r, d.Mrkdwn())
	out := DocFromRichtext(blocks)
	if out == nil || out.Empty() && !d.Empty() {
		return d
	}
	return out
}
