package blockkit

import "github.com/some-du6e/clack/internal/ui/slackc"

// A Block Kit modal for the inspect tool (internal/ui/inspect; Karim,
// 2026-11-18: his capture over an app's modal said "region primary", and
// nothing in the modal could be picked). Its parts are recorded as Slack's
// BlockKitModal draws them, a WizardModal over DeprecatedBaseDialog
// (gantry-v2 client-boot-render and boot-shared, 2026-11): c-dialog__header
// with the h1 c-dialog__title (BlockKitModal's p-block_kit_modal__title:
// its app icon and its text) and the c-dialog__close button,
// c-dialog__body, and c-dialog__footer holding WizardModal's footer with
// its back and next buttons; each block of a view in its
// p-block_kit_renderer__block_wrapper. ClassDef.Inspect records them for
// the tool alone: nothing styles see.
var (
	bkHeaderClass  = slackc.Class("c-dialog__header", "data-qa", "wizard_modal_header")
	bkTitleClass   = slackc.Class("c-dialog__title").Tag("h1")
	bkTitleDiv     = slackc.Class("p-block_kit_modal__title")      // BlockKitModal.renderTitle: a div
	bkOptionClass  = slackc.Class("c-select_options_list__option") // a select's option row
	bkTitleIcon    = slackc.Class("p-block_kit_modal__title_icon").Tag("img")
	bkTitleText    = slackc.Class("p-block_kit_modal__title_text").Tag("span")
	bkCloseClass   = slackc.Class("c-button-unstyled c-icon_button c-icon_button--size_medium c-dialog__close", "data-qa", "wizard_modal_close").Tag("button")
	bkIconBtnClass = slackc.Class("c-button-unstyled c-icon_button c-icon_button--size_medium").Tag("button")
	bkBodyClass    = slackc.Class("c-dialog__body", "data-qa", "wizard_modal_body")
	bkFooterClass  = slackc.Class("c-dialog__footer", "data-qa", "wizard_modal_footer")
	bkWizardFooter = slackc.Class("c-wizard_modal__footer")
	bkNextClass    = slackc.Class("c-button c-button--primary c-button--medium c-wizard_modal__next", "data-qa", "wizard_modal_next").Tag("button").
			Mods("c-button--disabled")
	bkBackClass  = slackc.Class("c-button c-button--outline c-button--medium c-wizard_modal__back", "data-qa", "wizard_modal_back").Tag("button")
	bkBlockClass = slackc.Class("p-block_kit_renderer__block_wrapper").
			Mods("p-block_kit_renderer__block_wrapper--first", "p-section_block", "p-rich_text_block", "p-header_block",
			"p-actions_block", "p-image_block", "p-context_block", "p-divider_block", "p-input_block")
)

// blockMods are a block wrapper's modifiers: the first block's, and its
// type's class.
func blockMods(typ string, first bool) uint64 {
	var m uint64
	if first {
		m = 1
	}
	for i, t := range []string{"section", "rich_text", "header", "actions", "image", "context", "divider", "input"} {
		if t == typ {
			m |= 1 << (i + 1)
		}
	}
	return m
}
