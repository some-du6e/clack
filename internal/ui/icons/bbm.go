package icons

// Simplified layout mode's glyphs (internal/ui/bbm.go), redrawn on the
// centrelines of Slack's SvgIcons (bv1-13-br icons.json): ExitBBMButton's
// sign-out (a bracket open to the right with an arrow leaving it) and the
// secondary header's back button, caret-left-full.
func init() {
	reg("sign-out",
		s("M12.75 2.25 H6.25 A3 3 0 0 0 3.25 5.25 V14.75 A3 3 0 0 0 6.25 17.75 H12.75"),
		s("M8.75 10 H16.5 M13.75 7 L16.75 10 L13.75 13"))
	reg("caret-left-full", s("M14 2.25 L6.25 10 L14 17.75"))
}
