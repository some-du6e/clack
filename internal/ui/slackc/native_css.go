package slackc

import (
	"strings"

	"github.com/some-du6e/clack/internal/ui/slackc/css"
)

// nativeCSS is Slack's own stylesheet, reduced to which native parts keep
// their own value for a property (css.NativeValue). In Slack's DOM a rule
// on a message row reaches the row's descendants two ways: directly, when
// a selector matches the descendant (".slick-ml-deleted *"), or by
// inheritance, where the descendant has no value of its own. A link, a
// mention, inline code or the timestamp have their own color in Slack's
// stylesheet, so a row's inherited color doesn't reach them, but a direct
// rule with enough specificity (or !important) does. These rules make the
// cascade decide that exactly as in the browser: natives answer
// Styled.Color & co. with CSS only where a plugin or user rule wins over
// them, and an inherited value stops at them.
//
// Selectors follow Slack's class names (docs/PLUGINS-components.md §2.3);
// the UA rules (b, i) are :where() so any author rule beats them, as the
// user-agent origin does. It's installed as the engine's base sheet, so it
// never makes the CSS engine non-empty (no cost while no CSS is set).
const nativeCSS = `
:where(b, strong) { font-weight: N }
:where(i, em) { font-style: N }
.c-message__sender_button { color: N; font-weight: N }
.c-timestamp { color: N }
.c-link { color: N }
.c-member_slug--link { color: N; background-color: N }
.c-inline_channel_entity { color: N; background-color: N }
.c-mrkdwn__subteam--link { color: N; background-color: N }
.c-mrkdwn__broadcast--mention { color: N; background-color: N; font-weight: N }
.c-mrkdwn__code { color: N; background-color: N }
.c-mrkdwn__pre { background-color: N }
.c-search__highlight { background-color: N }
.c-message__edited_label { color: N }
.c-message__body--tombstone { color: N }
.c-reaction { color: N; background-color: N }
.c-reaction_add { color: N; background-color: N }
.c-icon_button { color: N }
.c-message_attachment__title { font-weight: N }
.c-message_attachment__author_name, .c-message_attachment__service_name { font-weight: N }
.c-message_attachment__service_name, .c-message_attachment__footer { color: N }
.c-message__reply_count { color: N; font-weight: N }
.c-message__reply_bar_description { color: N }
.c-message_kit__labels__text { color: N }
.c-tooltip__tip { color: N; background-color: N }
`

func init() {
	Styles.SetBase(strings.ReplaceAll(nativeCSS, ": N", ": "+css.NativeValue))
}

// stripNative drops the base sheet's NativeValue winners from a host
// node's cascade: a plugin's own element with a Slack class has no native
// look to keep, so for it they are as if absent (the property inherits or
// takes its initial value, as before, and UA defaults still apply).
func stripNative(c *css.Cascaded) {
	n := 0
	for i := range c.Decls {
		if c.Decls[i].Value == css.NativeValue {
			n++
		}
	}
	if n == 0 {
		return
	}
	out := make(css.Decls, 0, len(c.Decls)-n)
	for _, d := range c.Decls {
		if d.Value != css.NativeValue {
			out = append(out, d)
		}
	}
	c.Decls = out
}
