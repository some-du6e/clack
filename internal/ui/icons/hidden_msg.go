package icons

// HiddenMessageBody's links (boot-shared aOPx; internal/ui/messages
// hidden_msg.go): "Show anyway" ends in caret-right-full, "Hide message"
// in caret-down-full. Both are redrawn on the centrelines of Slack's
// SvgIcons (bv1-13-br icons.json: 1.5-wide chevrons with round ends),
// like bbm.go's caret-left-full.
func init() {
	reg("caret-right-full", s("M6.25 2.25 L14 10 L6.25 17.75"))
	reg("caret-down-full", s("M2.25 6.25 L10 14 L17.75 6.25"))
}
