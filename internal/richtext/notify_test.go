package richtext

import "testing"

// TestNotificationText: a notification's body is Slack's
// convertMessageToPlainText (client-boot 2xQY, through boot-shared Eley
// getContentForNotification): conditions stay unexpanded and add
// nothing, so do fallback_canary and types with no converter (no
// fallback chain, no card), and blocks that add no text leave the
// message text. What the message draws after its blocks follows.
func TestNotificationText(t *testing.T) {
	sec := func(s string) string { return `{"type":"section","text":{"type":"mrkdwn","text":"` + s + `"}}` }
	cases := []struct{ name, msg, want string }{
		{"a condition isn't expanded: the message text",
			`{"ts":"1.0","text":"Deploy finished","blocks":[{"type":"condition","cases":[],"default":[` + sec("Deploy finished on desktop") + `]}]}`,
			"Deploy finished"},
		{"a type with no renderable adds nothing, not the card",
			`{"ts":"1.0","text":"x","blocks":[{"type":"expandable"}]}`, "x"},
		{"a fallback_canary's fallback isn't read",
			`{"ts":"1.0","text":"canary text","blocks":[{"type":"fallback_canary","fallback":[` + sec("degraded") + `]}]}`, "canary text"},
		{"blocks with text are their text",
			`{"ts":"1.0","text":"t","blocks":[` + sec("hello") + `,{"type":"expandable"}]}`, "hello"},
		{"an attachment follows the blocks",
			`{"ts":"1.0","text":"t","blocks":[` + sec("hello") + `],"attachments":[{"id":1,"text":"att body","fallback":"att body"}]}`,
			"hello\natt body"},
	}
	for _, c := range cases {
		if got := notificationTextOf(t, c.msg); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// TestSearchText: search previews give MessageBlocks
// NoUnsupportedBlockNotice (boot-shared vVRo), so the unsupported-block
// card adds nothing to a result's text; a fallback payload still does.
func TestSearchText(t *testing.T) {
	d := blocksMsg(t, `[{"type":"section","text":{"type":"mrkdwn","text":"release notes"}},{"type":"expandable"},
		{"type":"fallback_canary","fallback":[{"type":"section","text":{"type":"mrkdwn","text":"degraded"}}]}]`)
	if got, want := searchTextOf(*d), "release notes\ndegraded"; got != want {
		t.Errorf("search text %q, want %q", got, want)
	}
}
