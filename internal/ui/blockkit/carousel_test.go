package blockkit

import (
	"encoding/json"
	"image"
	"testing"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// Synthetic fixtures only.

// A carousel's cards' buttons are the message's element widgets, drawn
// through richview's Elements host in each card's view: blockkit nests a
// carousel's cards, as Slack's CarouselBlock renders each with
// renderBlocks, so a press sends the card's block_id, action_id and value
// with the message, as Slack's card actions do.
func TestCarouselCardElements(t *testing.T) {
	e := newBKEnv(t, false, false)
	base, ok := e.msgs["nested"]
	if !ok {
		t.Fatal("no nested gallery message to borrow")
	}
	raw, _ := json.Marshal([]any{map[string]any{"type": "carousel", "block_id": "cr", "elements": []any{
		map[string]any{"type": "card", "block_id": "card0", "title": pt("Pick 0"), "actions": []any{visBtn("open0")}},
		map[string]any{"type": "card", "block_id": "card1", "title": pt("Pick 1"), "actions": []any{visBtn("open1")}},
	}}})
	sm := slack.Message{TS: base.TS, User: base.User, BotID: base.BotID, Blocks: raw}
	m := base
	m.Blocks, m.Doc = raw, richtext.FromMessage(nilResolver{}, &sm)
	me := MessageElements(e.h, &m)
	if me == nil {
		t.Fatal("the carousel's message isn't interactive")
	}
	v := richview.New(m.Doc, richview.Options{Elements: me})
	for range 2 {
		v.Layout(e.context(), styleFor(e.h.th, e.h.img), &surfHandler{h: e.h})
	}
	for _, k := range []string{"card0\x00open0", "card1\x00open1"} {
		if w := me.ws[k]; w == nil || w.main.sz == (image.Point{}) {
			t.Fatalf("%q isn't a drawn element widget", k)
		}
	}
	me.ws["card1\x00open1"].press()
	got := e.calls(1)[0]
	if len(got.Actions) != 1 || got.Actions[0]["block_id"] != "card1" || got.Actions[0]["action_id"] != "open1" || got.Actions[0]["type"] != "button" {
		t.Errorf("actions %v, want the second card's button", got.Actions)
	}
	if got.Container["type"] != "message" || got.Container["message_ts"] != base.TS {
		t.Errorf("container %v, want the message %s", got.Container, base.TS)
	}
}
