package popups

import (
	"image"

	"gioui.org/layout"
)

// huddleHost is the ui root's huddle side (ui/huddle_wire.go, through
// internal/ui/huddleui).
type huddleHost interface {
	// HuddleMemberMenu is the call menu for a person: Start / Join / Leave
	// Huddle, and Copy huddle link when your DM with them exists.
	HuddleMemberMenu(user string) []MenuItem
}

// pfHuddleMenuAt builds the profile Huddle button's menu, as Slack's
// WithCallMenu opens CallMenu ("Start a call", 305px wide) for
// MemberProfileButtons: centered under the button (position bottom),
// 10px below it. btn is the button in window coordinates; closeCard (if
// set) runs after an item. nil when the host has no huddles.
func pfHuddleMenuAt(gtx layout.Context, h Host, user string, btn image.Rectangle, closeCard func()) *Menu {
	hh, ok := h.(huddleHost)
	if !ok {
		return nil
	}
	items := hh.HuddleMemberMenu(user)
	if len(items) == 0 {
		return nil
	}
	if closeCard != nil {
		for i := range items {
			if do := items[i].Do; do != nil {
				items[i].Do = func() { do(); closeCard() }
			}
		}
	}
	const w = 305
	wpx := dp(gtx, w)
	x := btn.Min.X + (btn.Dx()-wpx)/2
	anchor := image.Rect(x, btn.Min.Y, x+wpx, btn.Max.Y+dp(gtx, 10))
	return NewMenuOpts(h, items, MenuOpts{Width: w, Anchor: anchor, Side: Below})
}
