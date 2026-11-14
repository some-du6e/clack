package icons

// Glyphs of Slack's icon font ("Slack v2", slack-icons-v2-05e8fec.woff2
// on the CDN, the c-icon--* classes), which a few places draw instead of
// an SvgIcon, in the set's 20-unit view: the font's 2000-unit em at the
// 20px c-icon:before font size, the baseline at its 1700 ascent (x/100,
// (1700-y)/100). Line glyphs are drawn on the centrelines of their
// outlines like redrawn.go's (a glyph's outline filled reads 20-28% heavy
// in dark mode), solid ones as fl parts. Slack's fonts are not in the repo:
// the icon-shots workflow dumps the font's glyphs and TestIconsMatchSlack
// holds each glyph here to its codepoint's (fontRef).
func init() {
	// U+E096 icon_stop_screen_sharing (c-icon--stop-screen-sharing): the
	// presenter's mini panel's Stop Sharing button (HuddleMiniPanel, a y8g2
	// font icon)
	reg("stop-screen-sharing", s(rrect(1.75, 2.75, 16.5, 10.5, 1)), sw(2, "M10 13.5 V17"), s("M5.77 17.25 H14.27"), fl("M8.72 6.17 L8.72 9.83 L12.38 8 Z"))
	// U+E125 icon_hash_small (c-icon--hash-small): the # before the channel
	// name in the composer's placeholder (MessagePaneInputPlaceholder). Its
	// strokes are one unit wide, the weight of Lato's own at 15px.
	reg("hash-small", sw(1, "M9.25 5.5 L7.25 15.5 M12.74 5.5 L10.74 15.5 M6.5 8.5 H14.3 M5.7 12.5 H13.49"))
	// U+E157 icon_bullet_list (c-icon--bullet-list): a list's sidebar row
	// without programmable_sidebar (boot-render UGOO
	// getIconTypeForChannelPrefix, core/sidebar_docs.go); font
	// slack-icons-v2-05e8fec
	reg("bullet-list", disc(3.25, 5.25, 1.25), disc(3.25, 10.25, 1.25), disc(3.25, 15.25, 1.25), s("M9.25 5.25 H18.75 M9.25 10.25 H18.75 M9.25 15.25 H18.75"))
	// U+E281 icon_plus_small (c-icon--plus-small): the Apps section's Add
	// apps row (boot-render FmLP's add-more-items row; its
	// BaseChannelPrefixIcon, akoD, draws an iconType that isn't one of its
	// SvgIcons as the inline y8g2 font icon, size inherit)
	reg("plus-small", s("M10 6.5 V13.5 M6.5 10 H13.5"))
	// U+E294 icon_times_small (c-icon--times-small): AttachmentDeleteButton's
	// rail-style remove button left of classic and Block Kit unfurls.
	reg("times-small", s("M7.25 7.25 L12.75 12.75 M12.75 7.25 L7.25 12.75"))

	// The Activity rows' Slack Connect Ignore flow (rPCk IgnoreForkedModal,
	// IgnoreForkedModalButton: the y8g2 font icons times-circle-medium,
	// disable and chevron-medium-right).
	// U+E229 icon_times_circle_medium
	reg("times-circle-medium", s(circle(10, 10, 7.25)), s("M7.53 7.53 L12.47 12.47 M12.47 7.53 L7.53 12.47"))
	// U+E149 icon_disable
	reg("disable-font", s(circle(10, 10, 7.25)), s("M4.87 4.87 L15.13 15.13"))
	// U+E256 icon_chevron_medium_down: the chevron after the channel join
	// banner's section name (ChannelJoinBannerActionButtons'
	// p-about_modal__action_icon_suffix, a y8g2 font icon)
	reg("chevron-medium-down", s("M5.25 8 L10 12.75 L14.75 8"))
	// U+E500 icon_star (c-icon--star): the channel join banner's Starred
	// section, an inline y8g2 icon
	reg("star-font", fl("M18.42 8.05 L18.39 7.85 L18.32 7.71 L18.17 7.57 L18 7.46 L17.43 7.25 L16.82 7.13 L16.04 7.06 L15.11 7.03 L12.47 7.13 L12.03 5.24 L11.74 4.33 L11.41 3.5 L11.05 2.79 L10.68 2.24 L10.34 1.89 L10.16 1.77 L10 1.72 L9.84 1.77 L9.66 1.89 L9.32 2.24 L8.95 2.79 L8.59 3.5 L8.26 4.33 L7.97 5.24 L7.53 7.13 L5.2 7.03 L3.66 7.08 L3.16 7.14 L2.54 7.26 L2.1 7.41 L1.82 7.57 L1.67 7.72 L1.61 7.86 L1.58 8.05 L1.63 8.3 L1.87 8.79 L2.38 9.39 L2.91 9.89 L3.8 10.6 L4.86 11.33 L6 12.03 L5.55 13.03 L5.21 13.87 L4.86 14.9 L4.66 15.69 L4.52 16.43 L4.46 17.22 L4.5 17.75 L4.56 17.97 L4.64 18.14 L4.72 18.23 L4.87 18.27 L5.34 18.23 L6 18.02 L6.78 17.63 L7.62 17.09 L8.48 16.44 L9.1 15.9 L10 15.02 L10.9 15.9 L11.52 16.44 L12.38 17.09 L13.01 17.51 L13.62 17.84 L14.35 18.15 L14.91 18.27 L15.13 18.27 L15.29 18.21 L15.38 18.1 L15.47 17.89 L15.54 17.34 L15.5 16.6 L15.38 15.88 L15.2 15.1 L14.87 14.08 L14.54 13.24 L14 12.03 L15.14 11.33 L15.95 10.78 L16.67 10.24 L17.28 9.72 L17.77 9.23 L18.13 8.79 L18.34 8.41 Z"))
	// U+E254 icon_chevron_medium_right
	reg("chevron-medium-right", s("M8 5.25 L12.75 10 L8 14.75"))
	// The sidebar's organize mode checkbox (boot-render PZNh Channel's
	// renderEditModeCheckbox, an inline c-icon at the row's font size):
	// U+E322 icon_real_checkbox_empty and U+E323 icon_real_checkbox_checked
	// (its check a hole).
	reg("real-checkbox-empty", s("M6.1 1.75 H13.9 C16.45 1.75 18.25 3.56 18.25 6.1 V13.9 C18.25 16.45 16.45 18.25 13.9 18.25 H6.1 C3.56 18.25 1.75 16.45 1.75 13.9 V6.1 C1.75 3.56 3.56 1.75 6.1 1.75 Z"))
	reg("real-checkbox-checked", f("M6.1 1C3.14 1 1 3.14 1 6.1V13.9C1 16.86 3.14 19 6.1 19H13.9C16.86 19 19 16.86 19 13.9V6.1C19 3.14 16.86 1 13.9 1ZM13.99 7.4C13.99 7.63 13.92 7.85 13.76 8.03L9.17 13.46C8.97 13.7 8.7 13.82 8.41 13.82C8.07 13.82 7.78 13.66 7.58 13.38L6.03 11.17C5.91 10.99 5.85 10.79 5.85 10.58C5.85 10.23 6 9.95 6.28 9.77C6.45 9.66 6.64 9.6 6.85 9.6C7.18 9.6 7.47 9.76 7.66 10.02L8.49 11.19L12.23 6.75C12.43 6.52 12.69 6.4 12.98 6.4C13.23 6.4 13.44 6.47 13.63 6.62C13.87 6.81 13.99 7.1 13.99 7.4Z"))
	// U+E706 icon_form_checkbox_dash (c-menu_item__checkbox_dash): an
	// indeterminate menu row's mark (the Move to… menu over several
	// conversations).
	reg("form-checkbox-dash", sw(2, "M7 10 H13"))

	// TextyAutocompleteMenu's keyboard help (boot-shared WOwF):
	// "{arrowUp} {arrowDown} to navigate" draws the inline y8g2 icons
	// arrow-up-medium (U+E292) and arrow-down-medium (U+E293) at the
	// c-icon:before 20px, "{symbol} to select" enter (U+E302) at the help
	// row's 12px (size inherit). enter-font is the font's enter, not the
	// SvgIcon enter.
	reg("arrow-up-medium", s("M6.75 9 L10 5.75 L13.25 9 M10 5.75 V14.25"))
	reg("arrow-down-medium", s("M6.75 11 L10 14.25 L13.25 11 M10 14.25 V5.75"))
	reg("enter-font", s("M16.25 4.25 V11.25 H3.75 M7 8 L3.75 11.25 L7 14.5"))
	// U+E106 icon_broadcast (c-icon--broadcast): the @here / @channel /
	// @everyone rows' icon (SmallBroadcastEntity's y8g2 Icon type
	// "broadcast", size inherit): the megaphone's outline and its two
	// inner walls.
	reg("broadcast", s("M16.54 1.53 L16.15 1.58 L15.66 1.79 L15.22 2.07 L14.01 2.93 L13.13 3.44 L12.25 3.82 L11.47 4.06 L10.81 4.19 L10.15 4.25 L3.55 4.26 L3.26 4.33 L2.91 4.51 L2.62 4.76 L2.41 5.08 L2.28 5.45 L2.25 5.76 L2.25 9.24 L2.28 9.55 L2.37 9.83 L2.51 10.09 L2.69 10.31 L2.91 10.49 L3.17 10.63 L3.65 10.75 L4.77 10.76 L5 10.86 L5.22 11.05 L5.32 11.24 L7.37 16.86 L7.47 17.04 L7.65 17.28 L8.05 17.58 L8.42 17.71 L8.72 17.75 L9.1 17.71 L9.39 17.61 L9.64 17.46 L9.91 17.2 L10.11 16.88 L10.22 16.52 L10.24 16.14 L10.19 15.84 L8.68 11.65 L8.66 11.43 L8.73 11.18 L8.88 10.97 L9.09 10.82 L9.41 10.75 L10.18 10.75 L10.65 10.79 L11.45 10.93 L12.24 11.17 L12.85 11.43 L13.57 11.8 L14.18 12.18 L15.44 13.08 L15.86 13.31 L16.15 13.42 L16.54 13.47 L17 13.4 L17.31 13.22 L17.54 12.91 L17.7 12.4 L17.75 11.84 L17.74 2.86 L17.62 2.27 L17.5 2.02 L17.36 1.83 L17.2 1.7 L17 1.6 Z"), s("M8.75 4.25 V10.75"), s("M4.9 10.75 H9.75"))
	// U+E029 icon_lock_o (c-icon--lock-o): the composer placeholder's
	// private-channel icon (txEg getPlaceholderText, size inherit under
	// c-texty_input__placeholder_icon--private's 14px)
	reg("lock-o", s("M6.75 9.25 V7.05 C6.75 5.12 8.08 3.75 10 3.75 C11.94 3.75 13.25 5.12 13.25 7.05 V9.25"), s(rrect(4.75, 9.25, 10.5, 7, 1.09)))
	// Slack's cloud-offline SvgIcon (InlineOfflineIndicator: Activity's
	// "No connection. Go online to load more activity."), on the
	// centrelines of its outlines: the left lobe (r 3 about 4.75,13.75),
	// the bump, the top arc (r 5 about 11.75,8.25), the right side (r 4.5
	// about 13.75,12.25) to the base, and the slash between its cap centres.
	reg("cloud-offline", s("M2.75 16.05 A3 3 0 0 1 3.55 11 C2.8 8.6 4.56 6.16 6.88 6.26 A5 5 0 0 1 13.7 3.65"),
		s("M16.6 7.2 C16.95 8.05 17.05 8.85 16.95 9.55 A4.5 4.5 0 0 1 13.75 16.75 H8.1"),
		s("M17 3.5 L4 18"))
}
