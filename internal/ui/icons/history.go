package icons

// Slack's SvgIcon "clear" (a broom over three lines): the History dialog's
// Clear history button (boot-render +gg6 ModalMoreHistory). Line art, so
// drawn the way the set draws Slack's 1.5-unit glyphs: on the centrelines
// of its icons.json outline (the head's wall inset 0.75 with shapely; the
// ferrule and the two bristles 1 unit wide), within 2% of Slack's shape
// (IoU 0.982).
func init() {
	reg("clear",
		s("M12.77 1.76 L8.55 8.42"), // the handle
		s("M2.32 17.77 L11.89 17.77 L10.93 16.92 L10.24 16.14 L9.73 15.38 L9.34 14.53 L9.13 13.77 L9.04 13.02 L9.11 10.3 "+
			"L8.95 9.73 L8.48 9.02 L8.07 8.77 L6.76 8.65 L6.24 8.74 L5.64 8.97 L4.5 9.76 L3.5 10.93 L2.94 11.87 L2.59 12.66 "+
			"L2.13 14.37 L2.04 15.28 L2.05 16.21 Z"), // the head
		sw(1, "M5.17 10.21 C5.75 10.91 6.83 11.67 8.32 11.86"), // the ferrule
		sw(1, "M4.75 14.4 L4.78 15.6 L4.9 17"),
		sw(1.1, "M6.8 14 L6.85 15.4 L7.15 16.5 L7.57 17"),
		s("M12.75 10.75 H17.75"), s("M12.75 13.75 H17.75"), s("M14.75 16.75 H17.75"))
}
