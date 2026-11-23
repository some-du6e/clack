package hovercard

import (
	"image"

	"gioui.org/layout"
)

// layoutAsking is a sender's trigger as messages and search lay it out:
// bot(ask) works out the bot, ask true when the pointer is on the trigger
// (ask_timing_test.go).
func layoutAsking(t *Trigger, gtx layout.Context, user string, bot func(ask bool) string, size image.Point) {
	t.LayoutBotFn(gtx, user, func() string { return bot(true) }, size)
}
