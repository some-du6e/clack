package icons

// Clack's own glyphs for plugin systems' marks (drawn here, on the same
// 20×20 grid and stroke; not the projects' artwork).
func init() {
	// Slick's tab and overview mark: a bold S in a rounded tile.
	reg("slick-mark", sw(1.5, rrect(2.75, 2.75, 14.5, 14.5, 4)),
		sw(2, "M12.6 7.2 C12.2 6.1 11.2 5.6 10 5.6 C8.5 5.6 7.4 6.4 7.4 7.6 C7.4 10.2 12.6 9.6 12.6 12.3 C12.6 13.6 11.5 14.4 10 14.4 C8.6 14.4 7.6 13.8 7.2 12.7"))
	// Hack Club category: an outline tile with a lowercase h.
	reg("hackclub", s(rrect(3, 3, 14, 14, 4)), s("M7.75 6 V14 M7.75 10.4 C8.4 9.5 9.2 9.1 10.1 9.1 C11.4 9.1 12.25 9.9 12.25 11.3 V14"))
}
