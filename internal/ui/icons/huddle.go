package icons

// Huddle controls Slack's set has no glyph for (internal/ui/huddleui;
// the rest are in redrawn.go).
func init() {
	reg("hand", s("M7.25 10.25 V5 A1.25 1.25 0 0 1 9.75 5 V9.5 M9.75 9.5 V3.75 A1.25 1.25 0 0 1 12.25 3.75 V9.5 M12.25 9.5 V4.75 A1.25 1.25 0 0 1 14.75 4.75 V11.5 A5.5 5.5 0 0 1 9.25 17 A5.25 5.25 0 0 1 5 14.75 L3.4 12.2 A1.3 1.3 0 0 1 5.6 10.8 L7.25 12.5"))
	reg("sticker", s("M4.75 3.25 H15.25 A1.5 1.5 0 0 1 16.75 4.75 V11.5 L11.5 16.75 H4.75 A1.5 1.5 0 0 1 3.25 15.25 V4.75 A1.5 1.5 0 0 1 4.75 3.25 Z"), s("M16.75 11.5 H13 A1.5 1.5 0 0 0 11.5 13 V16.75"))
	reg("notes", s(rrect(3.75, 2.75, 12.5, 14.5, 1.5)), s("M7 7 H13 M7 10 H13 M7 13 H10.5"))
}
