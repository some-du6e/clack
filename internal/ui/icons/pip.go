package icons

func init() {
	// the mini player's Jump to message (yF7D's PipControls): surface's
	// two windows with an arrow into the front one (its head at the back
	// window's lower left)
	reg("move-to-main-window",
		s(rrect(6.25, 1.75, 12, 12, 2.25)),
		s("M6.25 6.25 H4 A2.25 2.25 0 0 0 1.75 8.5 V16 A2.25 2.25 0 0 0 4 18.25 H11.5 A2.25 2.25 0 0 0 13.75 16 V13.75"),
		sw(1, "M1.75 12.25 L6.25 7.75"), sw(1, "M1.75 16.25 L6.25 11.75"),
		sw(1, "M3.75 18.25 L8.25 13.75"), sw(1, "M7.75 18.25 L12.25 13.75"),
		s("M10.13 5.57 V9.84 H14.4"), s("M10.13 9.84 L14.86 5.15"))
}
