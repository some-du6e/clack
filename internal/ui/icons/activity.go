package icons

// Slack SvgIcons only the Activity page draws, filled from the paths
// Slack's icon set ships (the public web client's, via slack-datamine-plus
// icons.json; TestIconsMatchSlack holds them to it).
func init() {
	// section: ActivitySuggestedTabsMenu's row for a sidebar section
	// without an emoji (9av4 getSectionIcon)
	reg("section",
		f("M13.251 15.25a.75.75 0 0 1 0 1.5h-10a.75.75 0 0 1 0-1.5zm0-4a.75.75 0 0 1 0 1.5h-10a.75.75 0 0 1 0-1.5z"),
		f("M15.75 3.25c.966 0 1.75.784 1.75 1.75v2.049a1.75 1.75 0 0 1-1.75 1.75H4.25a1.75 1.75 0 0 1-1.75-1.75V5c0-.966.784-1.75 1.75-1.75zm-11.5 1.5A.25.25 0 0 0 4 5v2.049c0 .138.112.25.25.25h11.5a.25.25 0 0 0 .25-.25V5a.25.25 0 0 0-.25-.25z"))
	// mark-as-important-filled: ActivitySectionHeader's "VIP unreads" tile
	// (oDbF renderSectionIconTile)
	reg("mark-as-important-filled",
		f("M10 1a9 9 0 1 1 0 18 9 9 0 0 1 0-18m0 8.35a.75.75 0 0 0-.416.127l-.114.093-3.1 3.1a.75.75 0 0 0 1.06 1.06L10 11.16l2.57 2.57a.75.75 0 0 0 1.06-1.06l-3.1-3.1a.75.75 0 0 0-.53-.22m0-4.2a.75.75 0 0 0-.53.22l-3.1 3.1a.75.75 0 0 0 1.06 1.06L10 6.96l2.57 2.57a.75.75 0 0 0 1.06-1.06l-3.1-3.1a.75.75 0 0 0-.53-.22"))
}
