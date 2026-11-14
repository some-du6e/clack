package icons

// Huddle controls Slack's set has no glyph for (internal/ui/huddleui;
// the rest are in redrawn.go), and the ones Slack draws as inline SVG.
func init() {
	reg("hand", s("M7.25 10.25 V5 A1.25 1.25 0 0 1 9.75 5 V9.5 M9.75 9.5 V3.75 A1.25 1.25 0 0 1 12.25 3.75 V9.5 M12.25 9.5 V4.75 A1.25 1.25 0 0 1 14.75 4.75 V11.5 A5.5 5.5 0 0 1 9.25 17 A5.25 5.25 0 0 1 5 14.75 L3.4 12.2 A1.3 1.3 0 0 1 5.6 10.8 L7.25 12.5"))
	reg("sticker", s("M4.75 3.25 H15.25 A1.5 1.5 0 0 1 16.75 4.75 V11.5 L11.5 16.75 H4.75 A1.5 1.5 0 0 1 3.25 15.25 V4.75 A1.5 1.5 0 0 1 4.75 3.25 Z"), s("M16.75 11.5 H13 A1.5 1.5 0 0 0 11.5 13 V16.75"))
	reg("notes", s(rrect(3.75, 2.75, 12.5, 14.5, 1.5)), s("M7 7 H13 M7 10 H13 M7 13 H10.5"))
	// The huddle header's connection button (boot-shared TgjK
	// useDetermineTroubleshootingIcon): inline SVGs, not SvgIcons. Four
	// bars (rects 2.5 wide, rx .5: fl parts 0.72 inside them) and, with no
	// connection, the × (M3 4 L8 9 and M8 4 L3 9, stroke 1.5, round caps)
	// over the bars at opacity .32.
	reg("huddle-connection", fl("M2.72 14.72 H3.78 V16.28 H2.72 Z"), fl("M7.22 11.72 H8.28 V16.28 H7.22 Z"), fl("M11.72 7.72 H12.78 V16.28 H11.72 Z"), fl("M16.22 3.72 H17.28 V16.28 H16.22 Z"))
	reg("huddle-connection-x", s("M3 4 L8 9 M8 4 L3 9"))
}
