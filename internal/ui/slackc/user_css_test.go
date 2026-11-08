package slackc

import (
	"image/color"
	"testing"
)

// Slick's custom CSS (stylesheet "slick:user", installed after
// "slick:theme") styles native widgets like it styles Slack's page: an
// equally specific !important user rule beats the theme's, display:none
// hides a native element, and removing the custom CSS gives the theme back.
func TestUserCSSStylesNativeWidgets(t *testing.T) {
	defer func() { stack = stack[:0] }()
	SetStyle("slick:theme", ".x-ucss-sender{color:#00ff00 !important}")
	defer RemoveStyle("slick:theme")
	SetStyle("slick:user", ".x-ucss-sender{color:#ff0000 !important}\n.x-ucss-gone{display:none}")
	defer RemoveStyle("slick:user")

	fallback := color.NRGBA{R: 1, G: 2, B: 3, A: 255}
	colorOf := func() color.NRGBA {
		t.Helper()
		s := Class("x-ucss-sender").Enter(ctx(), 0, 0)
		defer s.Leave()
		return s.Color(fallback)
	}
	if c := colorOf(); c != (color.NRGBA{R: 255, A: 255}) {
		t.Errorf("with user.css: color %v, want user.css's #ff0000 over the theme's", c)
	}
	s := Class("x-ucss-gone").Enter(ctx(), 0, 0)
	hidden := s.Hidden()
	s.Leave()
	if !hidden {
		t.Error("user.css display:none didn't hide the native element")
	}
	RemoveStyle("slick:user")
	if c := colorOf(); c != (color.NRGBA{G: 255, A: 255}) {
		t.Errorf("user.css removed: color %v, want the theme's #00ff00", c)
	}
}
