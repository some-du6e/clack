package icons

// The search switcher's suggestion toolbar glyphs (getSuggestionSecondaryActions'
// iconProps in gantry-v2-async-client-boot-shared rs63, CDN build
// 4d0c78085232): Slack's SvgIcon outlines (icons.json) redrawn on the
// centrelines of their 1.5-unit lines. Against Slack's fills (shapely IoU,
// the lines buffered 0.75 with round caps): quick-switch 0.994, posts
// 1.000, app-directory 0.999, surface 0.992. surface's hatching is 1-unit
// lines whose ends hide in its frames, as move-to-split-view's is. Slack's
// user-directory is Clack's "directories" (slack.go).
func init() {
	// The results page's More > Celebration (search-filters_tsx, build
	// d3de3a487b1c): emoji-celebration, a gift box with its bow, on the
	// centrelines of Slack's outline (IoU 0.964, ink within 2%)
	reg("emoji-celebration",
		s("M3.75 7 H16.25 A1 1 0 0 1 17.25 8 V10.75 H2.75 V8 A1 1 0 0 1 3.75 7 Z"),
		s("M3.5 10.75 V16.75 A1 1 0 0 0 4.5 17.75 H15.5 A1 1 0 0 0 16.5 16.75 V10.75"), s("M10 7 V17.75"),
		s("M10.1 6.4 C9.85 4.8 9.35 3.45 8.6 2.55 C7.85 1.7 6.6 1.45 5.5 2.1 C4.75 2.55 4.55 3.25 4.6 3.75 C4.65 4.4 4.95 4.95 5.45 5.35 C6.4 6.05 8.1 6.4 10.1 6.4 Z"),
		s("M10.1 6.4 C10.45 5.1 11 4.4 11.4 4.1 C12.3 3.35 13.4 3.15 14.05 3.5 C14.6 3.8 14.75 4.6 14.25 5.25 C13.55 6 12.4 6.35 10.1 6.4 Z"))
	// Search in channel: three lines of text and a magnifier
	reg("quick-switch",
		s("M2.25 4.25 H14.75"), s("M2.25 8 H7.75"), s("M2.25 11.75 H6.25"), s("M2.25 15.5 H7.75"),
		s(circle(13.5, 12, 3.5)), s("M16.488 14.928 L18.182 16.622"))
	// Copy search query: a page with its corner folded and three lines
	reg("posts",
		s("M10.743 2.25 H5.25 A2 2 0 0 0 3.25 4.25 V15.75 A2 2 0 0 0 5.25 17.75 H14.75 A2 2 0 0 0 16.75 15.75 V8.257 A3 3 0 0 0 15.871 6.136 L12.864 3.129 A3 3 0 0 0 10.743 2.25 Z"),
		s("M6.5 8 H12.5"), s("M6.5 11 H13.5"), s("M6.5 14 H11.5"))
	// View app details: the directory card with a grid of apps
	reg("app-directory",
		s("M4.25 3.75 V3.25 A1 1 0 0 1 5.25 2.25 H15.25 A2 2 0 0 1 17.25 4.25 V15.75 A2 2 0 0 1 15.25 17.75 H5.25 A1 1 0 0 1 4.25 16.75 V16.25"),
		s("M2.75 6.75 H4.75"), s("M2.75 10 H4.75"), s("M2.75 13.25 H4.75"),
		disc(8.25, 7.25, .75), disc(11, 7.25, .75), disc(13.75, 7.25, .75),
		disc(8.25, 10, .75), disc(11, 10, .75), disc(13.75, 10, .75),
		disc(8.25, 12.75, .75), disc(11, 12.75, .75), disc(13.75, 12.75, .75))
	// Open in…: a window behind a hatched one
	reg("surface",
		s(rrect(6.25, 1.75, 12, 12, 2.25)),
		s("M6.25 6.25 H4 A2.25 2.25 0 0 0 1.75 8.5 V16 A2.25 2.25 0 0 0 4 18.25 H11.5 A2.25 2.25 0 0 0 13.75 16 V13.75"),
		sw(1, "M1.75 12.25 L6.25 7.75"), sw(1, "M1.75 16.25 L6.25 11.75"),
		sw(1, "M3.75 18.25 L8.25 13.75"), sw(1, "M7.75 18.25 L12.25 13.75"))
}
