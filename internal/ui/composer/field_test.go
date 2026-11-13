package composer

import (
	"image"
	"testing"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Block Kit's rich_text_input is the composer's WysiwygContainer
// (RichTextInputElement: enableEmojiButton, enableComposerButton,
// forcePlainTextModeDisabled, numMaxLines 8): the formatting bar's
// buttons, Aa and emoji in the footer and nothing else (no +, @, clips,
// shortcuts, send split), no hint bar, "Write something" without a
// placeholder, Enter for a new line (enterCreatesNewlineOverride), and no
// draft or typing (karim-1113c).
func TestFieldComposerChrome(t *testing.T) {
	app, _ := syntheticApp(t, erSeed)
	stubTyping(t)
	h := &keysHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	changes := 0
	c := NewField(h, FieldOpts{OnChange: func() { changes++ }})
	t.Cleanup(c.Close)
	ops := new(op.Ops)
	var dims layout.Dimensions
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Constraints{Max: image.Pt(560, 600)}, Now: time.Now()}
		slackc.Frame(gtx)
		dims = c.Layout(gtx)
	}
	frame()
	if c.cs.Mode != modeField || c.cs.Placeholder != "Write something" {
		t.Fatalf("mode %v placeholder %q, want the field's \"Write something\"", c.cs.Mode, c.cs.Placeholder)
	}
	for id, want := range map[crBtn]bool{crBold: true, crStrike: true, crCode: true, crLink: true, crBullet: true,
		crQuote: true, crCodeBlock: true, crAa: true, crEmoji: true,
		crAttach: false, crMention: false, crVideo: false, crAudio: false, crShortcut: false, crFootMore: false} {
		if got := c.ui.itemVisible(id); got != want {
			t.Errorf("button %d visible %v, want %v", id, got, want)
		}
	}
	if c.ui.f.hintH != 0 {
		t.Errorf("the field has a %dpx hint bar", c.ui.f.hintH)
	}
	if c.ui.rect[crSend] != (image.Rectangle{}) || c.ui.rect[crSave] != (image.Rectangle{}) {
		t.Error("the field draws a send or Save button")
	}
	if dims.Size.X != 560 || dims.Size.Y == 0 {
		t.Errorf("field box %v, want the full 560 width", dims.Size)
	}
	if c.st.MaxHeight != 8*22 {
		t.Errorf("max editor height %v, want 8 lines of 22", c.st.MaxHeight)
	}

	// typing changes the value; Enter is a new line, not a send
	c.ed.InsertText("mango")
	frame()
	edPress(c.ed, key.NameReturn, 0)
	frame()
	if got := c.FieldDoc().Text(); got != "mango\n" {
		t.Errorf("after Enter the field holds %q, want a new line", got)
	}
	if changes == 0 {
		t.Error("no OnChange while typing")
	}
	if d := loadDraft("", ""); d != nil && d.Len() > 0 {
		t.Errorf("the field saved a draft %q", d.Text())
	}
}
