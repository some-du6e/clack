package views

import (
	"encoding/json"
	"image"
	"strings"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// The Threads view draws messages as Slack's thread list does (duay,
// behind should_show_message_contextmenu like the channel's list), so a
// right-click on a message's body opens what it opens in a channel: on
// an image block's image (AttachmentImage, its <a href>'s background) the
// desktop shell's link menu for image_url; on the text, nothing of the
// body's (the message's own actions menu).
func TestThreadsBodyContentMenu(t *testing.T) {
	app, _ := vaFake(t)
	h := newW26Host(app, false)
	v := NewThreads(h)
	const img = "https://example.com/img/400x200/chart.png"
	sm := slack.Message{Type: "message", TS: "1762700000.000500", User: "UALICE", Text: "chart below",
		Blocks: json.RawMessage(`[{"type":"rich_text","block_id":"r1","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"chart below"}]}]},` +
			`{"type":"image","block_id":"i1","image_url":"` + img + `","alt_text":"chart"}]`)}
	m := core.Message{Channel: "C0FAKE00001", TS: sm.TS, User: sm.User, Text: sm.Text, Doc: richtext.FromMessage(app.S, &sm)}
	var got []popups.Overlay
	e := &vaEnv{size: image.Pt(900, 700)}
	e.draw = func(gtx layout.Context) {
		v.frame() // the view's theme, palette and plugins, as its Layout sets them first
		v.msgBody(gtx, &m, false, 0)
		if o, ok := bodyMenu(&v.base, gtx, h, image.Pt(10, 10), m); ok {
			got = append(got, o)
		}
	}
	e.frame()
	e.frame()
	ent := v.rich[m.Channel+"/"+m.TS]
	if ent == nil {
		t.Fatal("the body wasn't drawn")
	}
	rs := ent.v.ImageRects()
	if len(rs) != 1 {
		t.Fatalf("%d images in the body, want 1", len(rs))
	}
	right := func(p f32.Point) {
		e.r.Queue(
			pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p},
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonSecondary, Position: p},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p},
		)
		e.frame()
		e.frame()
	}
	c := rs[0].R.Min.Add(rs[0].R.Size().Div(2))
	right(f32.Pt(float32(c.X)+.5, float32(c.Y)+.5))
	if len(got) != 1 {
		t.Fatalf("a right-click on the image block opened %d body menus, want 1", len(got))
	}
	mm, ok := got[0].(*popups.Menu)
	if !ok {
		t.Fatalf("the body menu is a %T", got[0])
	}
	var labels []string
	for _, it := range mm.Items() {
		labels = append(labels, it.Label)
	}
	if strings.Join(labels, "|") != "Copy Link|Open Link" {
		t.Errorf("image block menu %q, want the link menu", labels)
	}
	// the text: the message's own menu (no body menu)
	var tp f32.Point
	found := false
	for y := 1; y < 40 && !found; y++ {
		for x := 1; x < 300 && !found; x++ {
			p := f32.Pt(float32(x)+.5, float32(y)+.5)
			if hit := ent.v.HitAt(p); hit.Text && hit.Kind == richview.HitNone {
				tp, found = p, true
			}
		}
	}
	if !found {
		t.Fatal("no text in the body")
	}
	right(tp)
	if len(got) != 1 {
		t.Errorf("a right-click on the text opened a body menu (%d)", len(got))
	}
}
