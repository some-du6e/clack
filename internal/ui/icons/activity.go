package icons

// Slack SvgIcons only the Activity page draws (Slack's icon set, the
// public web client's, via slack-datamine-plus icons.json;
// TestIconsMatchSlack holds them to it).
func init() {
	// section: ActivitySuggestedTabsMenu's row for a sidebar section
	// without an emoji (9av4 getSectionIcon). Slack's glyph is 1.5-unit
	// strokes outlined into fills (two capped rules, a frame with 1.75
	// corners outside, .25 inside); drawn here on their centrelines, as the
	// set's other stroked glyphs are, so it carries Chrome's ink
	reg("section",
		s("M3.25 16 H13.25"),
		s("M3.25 12 H13.25"),
		s("M4.25 4 H15.75 A1 1 0 0 1 16.75 5 V7.05 A1 1 0 0 1 15.75 8.05 H4.25 A1 1 0 0 1 3.25 7.05 V5 A1 1 0 0 1 4.25 4 Z"))
	// mark-as-important-filled: ActivitySectionHeader's "VIP unreads" tile
	// (oDbF renderSectionIconTile), Slack's path filled
	reg("mark-as-important-filled",
		f("M10 1a9 9 0 1 1 0 18 9 9 0 0 1 0-18m0 8.35a.75.75 0 0 0-.416.127l-.114.093-3.1 3.1a.75.75 0 0 0 1.06 1.06L10 11.16l2.57 2.57a.75.75 0 0 0 1.06-1.06l-3.1-3.1a.75.75 0 0 0-.53-.22m0-4.2a.75.75 0 0 0-.53.22l-3.1 3.1a.75.75 0 0 0 1.06 1.06L10 6.96l2.57 2.57a.75.75 0 0 0 1.06-1.06l-3.1-3.1a.75.75 0 0 0-.53-.22"))
}
