package icons

// The canvas toolbars' glyphs (the insertion toolbar's checklist and
// layouts buttons, the formatting menus' headings, the floating bar's
// callout), on the centrelines of Slack's checkbox, column-two,
// heading-1/2/3 and callout SvgIcons.
func init() {
	reg("checkbox", s(rrect(2.25, 2.25, 15.5, 15.5, 2.25)), s("M6.75 10 L8.75 12.7 L13.25 7.25"))
	reg("column-two", s(rrect(2.25, 2.25, 15.5, 15.5, 3)), s("M10 2.25 V17.75"))
	heading := s("M2.25 3.25 V16.75 M9.95 3.25 V16.75 M2.25 10 H9.95")
	reg("heading-1", heading, s("M13.95 10.5 L16.7 8.5 V16.75"))
	reg("heading-2", heading, s("M13.25 10.4 C13.25 9.2 14.2 8.5 15.25 8.5 C16.35 8.5 17.4 9.3 17.4 10.75 C17.4 12.2 16.3 13 13.25 16.75 H17.6"))
	reg("heading-3", heading, s("M12.95 8.75 H17.65 L15.25 11.3 C16.6 11 17.65 12 17.65 14.4 C17.65 16.9 13.9 17.6 12.65 14.9"))
	reg("callout", s("M3.25 2.25 H6.25"), s("M3.25 13.75 H15.25"), s("M3.25 17.75 H11.25"), s(rrect(3.25, 5.75, 13.5, 4.05, 1)))
}
