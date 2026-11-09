package icons

// Slack Lists' view tabs: table views use Slack's spreadsheet glyph, board
// views its kanban glyph (q1RT getLayoutConfig), redrawn on the centrelines
// of Slack's outlines like redrawn.go.
func init() {
	reg("spreadsheet", s(rrect(2.25, 3.25, 15.5, 13.5, 2.25)), s("M8 3.25 V16.75"), s("M2.25 7.75 H17.75"), s("M2.25 12.25 H17.75"))
	reg("kanban", s(rrect(2.25, 2.25, 6.25, 12.5, 1.75)), s(rrect(11.5, 2.25, 6.25, 6.5, 1.75)), s(rrect(11.5, 12.25, 6.25, 5.5, 1.75)))
}
