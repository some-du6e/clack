package messages

import (
	"image/color"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc/css"
)

// Slack's class names and attributes on the native widgets
// (docs/PLUGINS-components.md §2.3), so plugin and user CSS match them.
// Each Enter costs one atomic load while no CSS is installed.
var (
	paneClass   = slackc.Class("p-message_pane")
	threadClass = slackc.Class("p-flexpane p-threads_flexpane")
	listClass   = slackc.Class("c-message_list c-virtual_list", "role", "list", "data-qa", "slack_kit_list")
	itemClass   = slackc.Class("c-virtual_list__item", "role", "listitem")

	// MessageBackground; mods: hovered, thread message, thread root,
	// Slackbot custom response (core.Message.SlackbotCustomResponse).
	msgClass = slackc.Class("c-message_kit__background c-message_kit__message", "data-qa", "message_container").
			Mods("c-message_kit__background--hovered", "c-message_kit__thread_message", "c-message_kit__thread_message--root",
			"c-message_kit__background--labels--custom_response")
	gutterClass      = slackc.Class("c-message_kit__gutter")
	gutterLeftClass  = slackc.Class("c-message_kit__gutter__left")
	gutterRightClass = slackc.Class("c-message_kit__gutter__right")
	avatarClass      = slackc.Class("c-avatar c-base_icon c-message_kit__avatar").Tag("span")

	compactTSClass       = slackc.Class("c-link c-timestamp p-message_pane_message__compact_timestamp").Tag("a")
	threadCompactTSClass = slackc.Class("c-link c-timestamp p-thread_compact_gutter_generic").Tag("a")
	senderClass          = slackc.Class("c-message__sender c-message_kit__sender", "data-qa", "message_sender").Tag("span")
	senderBtnClass       = slackc.Class("c-link--button c-message__sender_button", "data-qa", "message_sender_name").Tag("button")
	tsClass              = slackc.Class("c-link c-timestamp").Tag("a")

	msgBlocksClass = slackc.Class("c-message__message_blocks", "data-qa", "message-text")
	blocksClass    = slackc.Class("c-message_kit__blocks c-message_kit__blocks--rich_text")

	reactionBarClass = slackc.Class("c-message_kit__reaction_bar c-reaction_bar", "data-qa", "reaction_bar")
	// mods: the viewer reacted.
	reactionClass    = slackc.Class("c-button-unstyled c-reaction", "data-qa", "reactji").Tag("button").Mods("c-reaction--reacted")
	reactionAddClass = slackc.Class("c-button-unstyled c-reaction_add", "data-qa", "add_reaction_button").Tag("button")

	actionsClass   = slackc.Class("c-message_kit__actions c-message_kit__actions--default c-message_actions__container c-message__actions")
	actionBtnClass = slackc.Class("c-button-unstyled c-icon_button c-icon_button--size_small c-message_actions__button c-icon_button--default").Tag("button")

	flexHeaderClass  = slackc.Class("p-flexpane_header")
	flexControlClass = slackc.Class("c-button-unstyled c-icon_button c-icon_button--size_medium c-icon_button--default p-flexpane_header__control", "aria-label", "Close").Tag("button")

	mrkdwnClass        = slackc.Class("p-mrkdwn_element")
	memberLinkClass    = slackc.Class("c-link c-member_slug c-member_slug--link").Tag("button")
	channelEntityClass = slackc.Class("c-inline_channel_entity").Tag("span")
)

// tbQA are the toolbar buttons' data-qa values (live client).
var tbQA = [tbCount]string{"add_reaction", "start_thread", "share_message", "save_message", "add_to_list", "more_message_actions"}

var colorNone color.NRGBA

// btnState is a button's CSS state (hover, keyboard focus, active); 0
// without asking anything while no CSS is installed.
func btnState(gtx layout.Context, b *widget.Clickable) css.State {
	if slackc.Styles.Empty() {
		return 0
	}
	var s css.State
	if b.Hovered() {
		s |= css.StateHover
	}
	if gtx.Focused(b) {
		s |= css.StateFocus | css.StateFocusVisible
	}
	if b.Pressed() {
		s |= css.StateActive
	}
	return s
}

func hoverState(hovered bool) css.State {
	if hovered {
		return css.StateHover
	}
	return 0
}
