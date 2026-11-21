package canvasview

import (
	"image"
	"testing"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
)

// The header's title draws an emoji the emoji map learned after the
// canvas loaded (richtext.TitleBlocks keeps it for the view to resolve as
// it draws, as Slack's QuipTitleParsedId reads the emoji store on every
// render). Synthetic canvas.
func TestHeaderTitleEmojiLate(t *testing.T) {
	h, fs := newEnv(t, false)
	const id, name = "F0HDREMJ01", "late-header-fixture"
	fs.AddCanvas(fakeslack.FakeCanvas{ID: id, Title: "Header plan :" + name + ":",
		TitleBlocks: []any{map[string]any{"type": "rich_text", "elements": []any{map[string]any{"type": "rich_text_section",
			"elements": []any{map[string]any{"type": "text", "text": "Header plan "}, map[string]any{"type": "emoji", "name": name}}}}}},
		Doc: canvas.Doc{Blocks: []canvas.Block{{ID: "temp:C:HDRE000001", Kind: canvas.Paragraph,
			Spans: []richtext.Span{{Kind: richtext.Text, Text: "A synthetic body."}}}}}})
	v := New(h, id, Side, Actions{})
	s := newShooter(t, image.Pt(520, 400))
	settle(t, s, v, "the canvas", func() bool { return v.Doc() != nil && v.shown != nil })
	emoji := func() *richtext.Span {
		d := v.Doc()
		for i := range d.Info.TitleSpans {
			if sp := &d.Info.TitleSpans[i]; sp.Kind == richtext.Emoji && sp.ID == name {
				return sp
			}
		}
		return nil
	}
	if sp := emoji(); sp == nil || sp.URL != "" {
		t.Fatalf("precondition: the title's emoji is %+v, want an Emoji span without an image", sp)
	}
	url := fs.URL + "img/64x64/" + fakeslack.TeamID + "/" + name + "/00000000000000f1.png"
	h.a.S.CustomEmoji.Put(name, url)
	settle(t, s, v, "the header's emoji as its image", func() bool { sp := emoji(); return sp != nil && sp.URL == url })
}
