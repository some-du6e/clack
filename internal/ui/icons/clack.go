package icons

// Clack's own glyphs for plugin systems' marks (drawn here, on the same
// 20×20 grid and stroke; not the projects' artwork).
func init() {
	// Slick's tab and overview mark: a bold S in a rounded tile.
	reg("slick-mark", sw(1.5, rrect(2.75, 2.75, 14.5, 14.5, 4)),
		sw(2, "M12.6 7.2 C12.2 6.1 11.2 5.6 10 5.6 C8.5 5.6 7.4 6.4 7.4 7.6 C7.4 10.2 12.6 9.6 12.6 12.3 C12.6 13.6 11.5 14.4 10 14.4 C8.6 14.4 7.6 13.8 7.2 12.7"))
	// Hack Club category: an outline tile with a lowercase h.
	reg("hackclub", s(rrect(3, 3, 14, 14, 4)), s("M7.75 6 V14 M7.75 10.4 C8.4 9.5 9.2 9.1 10.1 9.1 C11.4 9.1 12.25 9.9 12.25 11.3 V14"))
	// Clack's own mark, monochrome (Preferences > Clack): the white C, its
	// speech-bubble tail and the amber dash of assets/icon/clack-small.svg,
	// without the gradient tile, all in the icon colour. The 1024 artwork
	// maps by 0.02371 about its ink box's centre (508, 506.4), 16.5 units
	// wide (the info ring's span). The C is 160 of the artwork's 196 units
	// thick (3.79 here) and the dash 92 of 112, so at the nav's 15px the
	// dash keeps a 1px gap from the C's mouth (at 196 it is 0.6px and the
	// two run together); the arc, dash and tail sit where the artwork has
	// them. The tail is filled with a 1-unit edge (its 36-unit round join
	// is 0.85).
	reg("clack-logo",
		sw(3.79, "M13.13 5.82 A5.60 5.60 0 1 0 13.13 13.87"),
		sw(2.18, "M15.26 9.85 H17.16"),
		flw(1, "M2.31 11.83 Q2.25 14.57 2.83 17.22 Q5.54 16.25 7.74 16.90Z"))
}
