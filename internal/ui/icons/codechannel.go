package icons

// Code channel glyphs (internal/ui/codechannel.go): the sidebar's agent
// status dots (getAgentStatusDisplay), the context bar's working chip and
// link icons. Redrawn on the 20×20 grid with Slack's 1.5 stroke, under the
// names Slack's SvgIcon uses.
func init() {
	// idle: a small ring where a channel's # would be
	reg("status-member", sw(1.5, circle(10, 10, 4)))
	// suspended / PR blocked: a ring with an exclamation mark
	reg("needs-attention", s(circle(10, 10, 6.75)), s("M10 6.5 V10.75"), f(circle(10, 13.4, 0.95)))
	// pull request open: two stacked commits joined, a third fed from the right
	reg("pr-open", s(circle(5.75, 4.75, 1.75)), s(circle(5.75, 15.25, 1.75)), s("M5.75 6.5 V13.5"),
		s(circle(14.25, 15.25, 1.75)), s("M14.25 13.5 V8.25 A2.25 2.25 0 0 0 12 6 H9.5 M11.25 4.25 L9.5 6 L11.25 7.75"))
	// pull request closed: the stacked commits and a cross
	reg("pr-closed", s(circle(5.75, 4.75, 1.75)), s(circle(5.75, 15.25, 1.75)), s("M5.75 6.5 V13.5"),
		s(circle(14.25, 15.25, 1.75)), s("M14.25 10.75 V13.5"), s("M12.5 3.75 L16 7.25 M16 3.75 L12.5 7.25"))
	// merged / a branch: a trunk and a branch curving off it
	reg("branch", s(circle(6, 4.75, 1.75)), s(circle(6, 15.25, 1.75)), s("M6 6.5 V13.5"),
		s(circle(14, 6.25, 1.75)), s("M14 8 C14 11 10.5 11.25 6.75 13"))
	// the working chip's stop: a filled rounded square
	reg("stop-filled", f(rrect(5, 5, 10, 10, 2)))
	reg("archive", s(rrect(2.75, 3.75, 14.5, 3.75, 1)), s("M4 7.5 V14.75 A1.5 1.5 0 0 0 5.5 16.25 H14.5 A1.5 1.5 0 0 0 16 14.75 V7.5"), s("M8 10.5 H12"))
	// context bar link icons
	reg("terminal", s(rrect(2.75, 3.75, 14.5, 12.5, 2)), s("M6 8 L8.5 10.25 L6 12.5 M10 12.5 H14"))
	reg("hierarchy", s(rrect(7.5, 2.75, 5, 4, 1)), s(rrect(2.75, 13.25, 5, 4, 1)), s(rrect(12.25, 13.25, 5, 4, 1)),
		s("M10 6.75 V10 M5.25 13.25 V11 A1 1 0 0 1 6.25 10 H13.75 A1 1 0 0 1 14.75 11 V13.25"))
	reg("life-ring", s(circle(10, 10, 7)), s(circle(10, 10, 3)), s("M5.05 5.05 L7.9 7.9 M14.95 5.05 L12.1 7.9 M5.05 14.95 L7.9 12.1 M14.95 14.95 L12.1 12.1"))
}
