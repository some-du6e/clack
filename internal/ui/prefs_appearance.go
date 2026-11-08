package ui

import (
	"gioui.org/layout"
)

// appearancePage is Preferences > Appearance.
func (p *prefsModal) appearancePage(gtx layout.Context) layout.Dimensions {
	a := p.a
	f := p.form(gtx)
	f.heading("Theme")
	f.option("light", "Light", "", !a.th.Dark, true, func() {
		if a.th.Dark {
			a.ToggleDark()
		}
	})
	f.option("dark", "Dark", "", a.th.Dark, true, func() {
		if !a.th.Dark {
			a.ToggleDark()
		}
	})
	f.gap()
	f.heading("Emoji")
	style := a.th.EmojiStyle
	f.option("apple", "Apple", "Slack's default emoji set", style == "" || style == "apple", true, func() { a.setEmojiStyle("apple") })
	f.option("google", "Google", "", style == "google", true, func() { a.setEmojiStyle("google") })
	return f.dims()
}
