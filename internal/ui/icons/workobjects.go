package icons

// Slack's reference SvgIcon (a work object card's "View conversations",
// ExternalWorkObjectEntity's eX), redrawn on the centrelines of its
// 1.5-unit outlines: the document (radius 3 corners, open at the top
// left), its three lines, and the curved arrow coming up into it from the
// bottom left. IoU with Slack's glyph 0.994 (shapely);
// TestIconsMatchSlack holds it to Slack's.
func init() {
	reg("reference",
		s("M7.757 17.25 H14.257 A3 3 0 0 0 17.257 14.25 V5.75 A3 3 0 0 0 14.257 2.75 H6.757 A3 3 0 0 0 3.757 5.75 V6.25"),
		s("M7.757 6.25 H14.257"),
		s("M9.757 9.75 H14.257"),
		s("M9.757 13.25 H14.257"),
		s("M2.761 9.75 H6.261 V13.25"),
		s("M5.3 10.75 C4.6 11.5 3.9 12.3 3.38 13.16 C2.86 14.1 2.66 15.6 3.0 17.25"))
}
