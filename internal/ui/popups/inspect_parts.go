package popups

import "github.com/some-du6e/clack/internal/ui/slackc"

// Popups for the inspect tool (internal/ui/inspect; Karim, 2026-11-18:
// "you cant select the popup smh"). Each popup is a layer of the tool's
// record (ui's drawOverlay, slackc.InspectOverlay), its card the layer's
// box (slackc.InspectCard), and the parts the picker should reach are
// recorded by their Slack names where Clack draws them without entering
// a class: ClassDef.Inspect, which nothing styles see, so no selector
// matches them and nothing is drawn differently. A popup's own elements
// entered for CSS (a menu's rows, a modal's buttons) get their boxes in
// a layer only (slackc.InspectAreaInLayer): the window's stay as the
// picker has known them.

// The SK modal's parts, as Slack's Modal draws them: ModalHeader,
// ModalTitleBar with its text (the h1), ModalCloseIconButton,
// ModalContent and ModalFooter (client-boot, 2026-11).
var (
	skHeaderClass   = slackc.Class("c-sk-modal_header")
	skTitleBarClass = slackc.Class("c-sk-modal_title_bar c-sk-modal_title_bar--close_button_visible")
	skTitleClass    = slackc.Class("c-sk-modal_title_bar__text")
	skCloseClass    = slackc.Class("c-button-unstyled c-icon_button c-icon_button--size_medium c-sk-modal__close_button", "data-qa", "sk_close_modal_button").Tag("button")
	skContentClass  = slackc.Class("c-sk-modal_content")
	skFooterClass   = slackc.Class("c-sk-modal_footer")
)

// The emoji picker's parts, as Slack's EmojiPicker draws them:
// EmojiPickerInput's container, EmojiListRow, EmojiListGroupItem's
// heading and EmojiListEmojiButton (docs/PLUGINS-props-live.md).
var (
	epInputClass   = slackc.Class("p-emoji_picker__input_container")
	epRowClass     = slackc.Class("p-emoji_picker__row")
	epHeadingClass = slackc.Class("p-emoji_picker__heading").Tag("span")
	// clsx(…, "p-emoji_picker__list_item", {"…--a11y-not-selected":
	// !selected, key_selection: selected by the keyboard}), data-qa
	// emoji_list_item
	epItemClass = slackc.Class("c-link--button p-emoji_picker__list_item", "data-qa", "emoji_list_item").Tag("button").
			Mods("p-emoji_picker__list_item--a11y-not-selected", "key_selection")
)
