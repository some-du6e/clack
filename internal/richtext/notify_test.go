package richtext

import "testing"

// TestNotificationText: a notification's body is Slack's
// convertMessageToPlainText (client-boot 2xQY, through boot-shared Eley
// extractTextFromMessage, blockSpacing "single"): conditions stay
// unexpanded and add nothing, so do fallback_canary and types with no
// converter (no fallback chain, no card); every other block goes through
// its own converter, and blocks that add no text leave the message text.
// The blocks' or the text's body is the whole body: only when both are
// empty do the attachments speak (a message unfurl's blocks, else the
// first fallback, text, pretext or footer).
func TestNotificationText(t *testing.T) {
	sec := func(s string) string { return `{"type":"section","text":{"type":"mrkdwn","text":"` + s + `"}}` }
	rts := func(s string) string {
		return `{"type":"rich_text_section","elements":[{"type":"text","text":"` + s + `"}]}`
	}
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
		{"the blocks' text is the whole body: an attachment adds nothing",
			`{"ts":"1.0","text":"t","blocks":[` + sec("hello") + `],"attachments":[{"id":1,"text":"att body","fallback":"att body"}]}`,
			"hello"},
		// 2xQY's eh table: alerts aren't supported in messages, a callout
		// converts only for a message summary, cards and calls not at all
		{"an alert adds nothing: the message text",
			`{"ts":"1.0","text":"Release failed","blocks":[{"type":"alert","level":"error","text":{"type":"mrkdwn","text":"the release failed"}}]}`,
			"Release failed"},
		{"a callout adds nothing, not its children",
			`{"ts":"1.0","text":"callout text","blocks":[{"type":"callout","child_blocks":[` + sec("inside") + `]}]}`,
			"callout text"},
		{"a card and a call add nothing",
			`{"ts":"1.0","text":"card text","blocks":[{"type":"card","title":{"type":"plain_text","text":"A card"}},{"type":"call","call_id":"R1","call":{"v1":{"id":"R1","name":"Sync"}}}]}`,
			"card text"},
		{"a container is its title, not its children",
			`{"ts":"1.0","text":"t","blocks":[{"type":"container","title":{"type":"plain_text","text":"Deploy"},"child_blocks":[` + sec("inside") + `]}]}`,
			"Deploy"},
		{"a container's rich_text_title",
			`{"ts":"1.0","text":"t","blocks":[{"type":"container","rich_text_title":{"type":"rich_text","elements":[` + rts("Rich ") + `]},"child_blocks":[]}]}`,
			"Rich"},
		{"a share_shortcut is its workflow's title",
			`{"ts":"1.0","text":"A workflow link","blocks":[{"type":"share_shortcut","function_trigger_id":"Ft0GALLERY1","title":"Request time off"}]}`,
			"Request time off"},
		{"a video block is its title (convertVideoBlockToPlainText), not its description",
			`{"ts":"1.0","text":"","blocks":[{"type":"video","video_url":"https://v.test/v.mp4","thumbnail_url":"https://v.test/t.png","alt_text":"a clip",` +
				`"title":{"type":"plain_text","text":"Launch recap"},"description":{"type":"plain_text","text":"Its description"}}]}`,
			"Launch recap"},
		{"an image block is its title, not its alt text",
			`{"ts":"1.0","text":"t","blocks":[{"type":"image","image_url":"https://i.test/a.png","alt_text":"alt","title":{"type":"plain_text","text":"Chart"}},` +
				`{"type":"image","image_url":"https://i.test/b.png","alt_text":"only alt"}]}`,
			"Chart"},
		{"a section's text and fields, a line each; a context's texts joined by spaces",
			`{"ts":"1.0","text":"t","blocks":[{"type":"section","text":{"type":"mrkdwn","text":"top"},"fields":[{"type":"mrkdwn","text":"f1"},{"type":"plain_text","text":"f2"}],` +
				`"accessory":{"type":"button","action_id":"a","text":{"type":"plain_text","text":"Go"}}},` +
				`{"type":"context","elements":[{"type":"mrkdwn","text":"by"},{"type":"image","image_url":"https://i.test/c.png","alt_text":"pic"},{"type":"plain_text","text":"me"}]},` +
				`{"type":"actions","elements":[{"type":"button","action_id":"b","text":{"type":"plain_text","text":"Ship"}}]}]}`,
			"top\nf1\nf2\nby me"},
		{"a table's rows, cells joined by commas",
			`{"ts":"1.0","text":"t","blocks":[{"type":"table","rows":[[{"type":"raw_text","text":"Item"},{"type":"raw_text","text":"Count"}],[{"type":"raw_text","text":"apples"},{"type":"raw_number","value":4,"text":"4"}]]}]}`,
			"Item, Count\napples, 4"},
		// convertRichTextBlockToPlainText's lists: "<marker> <text>." with
		// no indent, •/◦/■ and 1./a./i. by indent, a check list unordered
		{"list items end with a period and mark their nesting",
			`{"ts":"1.0","text":"t","blocks":[{"type":"rich_text","elements":[` +
				`{"type":"rich_text_list","style":"bullet","elements":[` + rts("one") + `,` + rts("two") + `]},` +
				`{"type":"rich_text_list","style":"bullet","indent":1,"elements":[` + rts("nested") + `]},` +
				`{"type":"rich_text_list","style":"bullet","indent":2,"elements":[` + rts("deeper") + `]},` +
				`{"type":"rich_text_list","style":"ordered","elements":[` + rts("first") + `]},` +
				`{"type":"rich_text_list","style":"ordered","indent":1,"offset":1,"elements":[` + rts("second") + `]},` +
				`{"type":"rich_text_list","style":"ordered","indent":2,"offset":3,"elements":[` + rts("fourth") + `]},` +
				`{"type":"rich_text_list","style":"check","elements":[` + rts("todo") + `]}]}]}`,
			"• one.\n• two.\n◦ nested.\n■ deeper.\n1. first.\nb. second.\niv. fourth.\n• todo."},
		// only when the blocks and the text are both empty: a message
		// unfurl's blocks, else the first attachment's fallback
		{"no text anywhere: a message unfurl's blocks",
			`{"ts":"1.0","text":"","blocks":[{"type":"divider"}],"attachments":[{"id":1,"is_msg_unfurl":true,"fallback":"fb","blocks":[` + sec("the shared message") + `]}]}`,
			"the shared message"},
		// extractTextFromMessage converts them without blockSpacing "single"
		{"a message unfurl's blocks, a blank line apart",
			`{"ts":"1.0","text":"","attachments":[{"id":1,"is_msg_unfurl":true,"fallback":"fb","blocks":[` + sec("first") + `,` + sec("second") + `]}]}`,
			"first\n\nsecond"},
		{"no text anywhere: the attachment's fallback",
			`{"ts":"1.0","text":"","blocks":[{"type":"divider"}],"attachments":[{"id":1,"text":"att body","fallback":"att fallback"}]}`,
			"att fallback"},
	}
	for _, c := range cases {
		if got := notificationTextOf(t, c.msg); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// asTextRes is a viewer with emoji_mode as_text.
type asTextRes struct{ fakeRes }

func (asTextRes) EmojiAsText() bool { return true }

// TestNotificationTextEmoji: extractTextFromMessage converts emoji to
// their text under shouldRenderEmojiAsText (client-boot pfK8), else to
// unicode, in the blocks and in the text alike.
func TestNotificationTextEmoji(t *testing.T) {
	for _, c := range []struct{ name, msg string }{
		{"blocks", `{"ts":"1.0","text":"hi :smile:","blocks":[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[` +
			`{"type":"text","text":"hi "},{"type":"emoji","name":"smile"}]}]}]}`},
		{"text", `{"ts":"1.0","text":"hi :smile:"}`},
	} {
		m := msg(t, c.msg)
		if got := NotificationMessageText(R, m, nil); got != "hi \U0001F604" {
			t.Errorf("%s: %q, want the emoji itself", c.name, got)
		}
		var as asTextRes
		if got := NotificationMessageText(as, m, nil); got != "hi :smile:" {
			t.Errorf("%s with emoji_mode as_text: %q, want \"hi :smile:\"", c.name, got)
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
