package slackc

import (
	"image/color"
	"testing"
)

// Slack keeps sk-client-theme--dark or --light on <body> (jsrt's document
// switches it with the color scheme); a theme rule keyed on it must reach
// native widgets through the document at the bottom of the element stack.
func TestDocumentBodyThemeClassReachesNativeWidgets(t *testing.T) {
	SetStyle("test:theme", ".sk-client-theme--dark .x-doc-themed{color:#ff0000}")
	defer RemoveStyle("test:theme")
	defer func() {
		doc.Lock()
		doc.html, doc.body = nil, nil
		doc.ver++
		doc.Unlock()
		stack = stack[:0]
	}()
	def := Class("x-doc-themed")
	fallback := color.NRGBA{R: 1, G: 2, B: 3, A: 255}
	colorWith := func(bodyClass string) color.NRGBA {
		t.Helper()
		if err := SetDocumentProps([]byte(`{"lang":"en-US"}`), []byte(`{"className":"`+bodyClass+`"}`)); err != nil {
			t.Fatal(err)
		}
		pushDocument()
		s := def.Enter(ctx(), 0, 0)
		defer s.Leave()
		return s.Color(fallback)
	}
	if c := colorWith("sk-client-theme--dark"); c != (color.NRGBA{R: 255, A: 255}) {
		t.Errorf("dark: color %v, want the theme's #ff0000", c)
	}
	if c := colorWith("sk-client-theme--light"); c != fallback {
		t.Errorf("light: color %v, want the widget's own", c)
	}
}
