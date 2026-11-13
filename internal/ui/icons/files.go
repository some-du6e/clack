package icons

// Slack SvgIcons the Files tab draws (Slack's icon set, the public web
// client's, via slack-datamine-plus icons.json; TestIconsMatchSlack holds
// them to it). Slack's glyphs are 1.5-unit strokes outlined into fills;
// they're drawn here on their centrelines, as the set's other stroked
// glyphs are, so they carry Chrome's ink.
func init() {
	// canvas-browser: the sidebar's All files (cNNe). A canvas (4.5-18.5,
	// corners 3.336 outside / 1.836 inside, the bottom-right cut by the
	// fold) in front of a second one whose top and left edges show
	// (1.5-…, the same corners).
	reg("canvas-browser",
		s("M4.837 14.751 A2.587 2.587 0 0 1 2.25 12.164 V4.836 A2.586 2.586 0 0 1 4.836 2.25 H12.164 A2.587 2.587 0 0 1 14.751 4.837"),
		s("M7.836 5.25 H15.164 A2.586 2.586 0 0 1 17.75 7.836 V11.805 A2 2 0 0 1 17.164 13.219 L13.219 17.164 A2 2 0 0 1 11.805 17.75 H7.836 A2.586 2.586 0 0 1 5.25 15.164 V7.836 A2.586 2.586 0 0 1 7.836 5.25 Z"),
		s("M12.25 17.75 V13 A0.75 0.75 0 0 1 13 12.25 H17.75"))
	// canvas-browser-filled: the Files & links tab while selected
	// (useTabIconProps). The back canvas's band as canvas-browser's, the
	// front canvas filled (fl: its edge 0.72 in, the corners 3.336 → 2.616,
	// the fold's 2.75 → 2.03) with the fold's L cut out: Slack's 1.5-wide
	// round-capped L (centreline 12.25,15.75 → 12.25,13 → 13,12.25 →
	// 15.75,12.25) grown by 0.72 and wound the other way.
	reg("canvas-browser-filled",
		s("M4.837 14.751 A2.587 2.587 0 0 1 2.25 12.164 V4.836 A2.586 2.586 0 0 1 4.836 2.25 H12.164 A2.587 2.587 0 0 1 14.751 4.837"),
		fl("M7.836 5.22 H15.164 A2.616 2.616 0 0 1 17.78 7.836 V11.804 A2.03 2.03 0 0 1 17.185 13.239 L13.239 17.185 A2.03 2.03 0 0 1 11.804 17.78 H7.836 A2.616 2.616 0 0 1 5.22 15.164 V7.836 A2.616 2.616 0 0 1 7.836 5.22 Z"+
			" "+"M10.78 13 V15.75 A1.47 1.47 0 0 0 13.72 15.75 V13.72 H15.75 A1.47 1.47 0 0 0 15.75 10.78 H13 A2.22 2.22 0 0 0 10.78 13 Z"))
	// canvas-content: the Canvases view and every canvas row's icon
	// (SmallFileListEntity): Slack's canvas (1.5-18.5, corners 3.75
	// outside / 2.25 inside, the bottom-right cut by the fold) with two
	// rules, 6.25-13.75 at y 6.25 and 6.25-8.25 at y 9.25.
	reg("canvas-content",
		s("M5.25 2.25 H14.75 A3 3 0 0 1 17.75 5.25 V10.986 A2 2 0 0 1 17.164 12.4 L12.4 17.164 A2 2 0 0 1 10.986 17.75 H5.25 A3 3 0 0 1 2.25 14.75 V5.25 A3 3 0 0 1 5.25 2.25 Z"),
		s("M11.5 17.75 V12.25 A0.75 0.75 0 0 1 12.25 11.5 H17.75"),
		s("M6.25 6.25 H13.75"),
		s("M6.25 9.25 H8.25"))
	// close-filled: the section modal's Clear all (vBP0): two 2.5-wide
	// round-capped bars between (4,4)-(16,16) and (16,4)-(4,16).
	reg("close-filled", sw(2.5, "M4 4 L16 16"), sw(2.5, "M16 4 L4 16"))
}
