package richview

// The /remind confirmation as Slack sends it (captured 2026-11-24 in a
// self-DM): one rich_text block of an emoji and a text element, no
// attachments. Slack draws its body at the sender name's x (p-rich_text_block
// in c-message_kit__blocks--rich_text: x 512, the name's 512). Its first
// frag sits at the view's left edge, with no card bar or padding before it.
// Synthetic text.

import (
	"encoding/json"
	"image"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

const remindConfirmation = `{"type":"message","subtype":"bot_message","user":"USLACKBOT","bot_id":"B01","is_ephemeral":true,"ts":"1795558164.745689",
"text":"I will remind you “stretch” at 3PM tomorrow.","blocks":[{"type":"rich_text","block_id":"aB1","elements":[{"type":"rich_text_section","elements":[
{"type":"emoji","name":"thumbsup","unicode":"1f44d"},{"type":"text","text":" I will remind you “stretch” at 3PM tomorrow."}]}]}]}`

func TestRemindConfirmationDrawsFlushLikeSlack(t *testing.T) {
	var m slack.Message
	if err := json.Unmarshal([]byte(remindConfirmation), &m); err != nil {
		t.Fatal(err)
	}
	d := rt.FromMessage(codeRes{}, &m)
	for _, b := range d.Blocks {
		if b.Kind == rt.AttachmentBlock {
			t.Fatalf("the confirmation converted to an attachment: %+v", b)
		}
	}
	st := NewStyle(themeShaper(), noImages{}, LightPalette())
	v := New(d, Options{})
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Constraints{Max: image.Pt(600, 400)}}
	v.Layout(gtx, st, &recHandler{})
	if len(v.d.frags) == 0 {
		t.Fatal("nothing drawn")
	}
	minX := v.d.frags[0].x
	for _, f := range v.d.frags {
		minX = min(minX, f.x)
	}
	if minX < 0 || minX > 1 {
		t.Errorf("the body's first frag at x %.1f: Slack draws a rich_text body at the view's (the name's) left edge", minX)
	}
	for _, b := range v.d.boxes {
		if b.kind == bFill && b.r.Min.X <= 1 && b.r.Dx() <= 6 && b.r.Dy() > 8 {
			t.Errorf("a card bar %v before the body", b.r)
		}
	}
}
