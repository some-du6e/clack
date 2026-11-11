package icons

// Slack's move-to-split-view SvgIcon (its menus' "Open in split view"
// rows, ClientSplitView's drop target), redrawn on the centrelines of its
// 1.5-unit outlines: the arrow arcing up and over to its corner head, the
// left pane with its 1-unit diagonal hatching (stroked lines whose ends
// hide in the pane's frame) and the empty right pane. TestIconsMatchSlack
// holds it to Slack's glyph.
func init() {
	reg("move-to-split-view",
		s("M5.5 5.5 C5.79 3.15 7.83 1.794 10.264 1.794 C12.28 1.794 14.2 2.75 15 4.3"),
		s("M16.089 2.323 V5.5 H12.912"),
		s(rrect(1.794, 8.25, 9, 10, 2.383)),
		sw(1, "M7.1 8.5 L2.04 13.56"),
		sw(1, "M10.13 9.37 L2.42 17.07"),
		sw(1, "M10.54 12.86 L5.41 18"),
		s(rrect(13.25, 8.25, 5, 10, 2.383)))
}
