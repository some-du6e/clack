package icons

// Slack's SvgIcon "command" (⌘): the Keyboard Shortcuts pane's Cmd key
// chip (client-boot HgMH KeyboardKey). Its icons.json outline is a
// 1.5-unit line, drawn on its centreline: the square's sides run on into
// four loops of radius 2.5 (IoU with Slack's shape 1.000).
func init() {
	reg("command", s("M12.75 7.25 V4.75 A2.5 2.5 0 1 1 15.25 7.25 H4.75 A2.5 2.5 0 1 1 7.25 4.75 V15.25 "+
		"A2.5 2.5 0 1 1 4.75 12.75 H15.25 A2.5 2.5 0 1 1 12.75 15.25 V7.25 Z"))
}
