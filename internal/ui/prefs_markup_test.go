package ui

import (
	"encoding/json"
	"image"
	"os"
	"testing"

	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/composer"
)

// TestFormatWithMarkupPref: Preferences > Advanced > "Format messages with
// markup" (Slack's plain_text_mode) draws, flips every composer's mode and
// survives a restart through ui.json.
func TestFormatWithMarkupPref(t *testing.T) {
	a, _ := ttApp(t, t.TempDir())
	t.Cleanup(func() { composer.SetPlainTextMode(false) })
	if a.plainTextOn() {
		t.Fatal("markup mode on by default (Slack formats live by default)")
	}
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}, section: "advanced"}
	gtx := ttContext(new(op.Ops), image.Pt(800, 560))
	a.slackcFrame(gtx)
	p.layout(gtx)
	if _, ok := p.clicks["plaintext"]; !ok {
		t.Fatal("Advanced has no Format messages with markup checkbox")
	}

	a.togglePlainText()
	a.dispatchWait()
	if !a.plainTextOn() || !composer.PlainTextMode(a.Core()) {
		t.Fatal("toggle didn't turn markup mode on")
	}
	b, err := os.ReadFile(a.prefsPath())
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		PlainText *bool `json:"plain_text_mode"`
	}
	if json.Unmarshal(b, &saved) != nil || saved.PlainText == nil || !*saved.PlainText {
		t.Fatalf("ui.json = %s, want plain_text_mode true", b)
	}

	a.togglePlainText()
	a.dispatchWait()
	if a.plainTextOn() {
		t.Error("second toggle didn't turn it off")
	}
}
