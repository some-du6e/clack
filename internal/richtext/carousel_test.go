package richtext

import (
	"reflect"
	"testing"
)

// Synthetic fixtures only.

// A carousel's elements are its cards, built as blocks of their own
// (Kit); label and is_label_visible take either spelling; elements that
// aren't a list are the block's error line, and no elements an empty
// carousel.
func TestCarouselDecode(t *testing.T) {
	card := func(id string) string {
		return `{"type":"card","block_id":"` + id + `","title":{"type":"plain_text","text":"Card ` + id + `"}}`
	}
	for _, extra := range []string{`"label":"Picks","is_label_visible":true`, `"label":"Picks","isLabelVisible":true`} {
		d := blocksMsg(t, `[{"type":"carousel","block_id":"cr",`+extra+`,"elements":[`+card("a")+`,`+card("b")+`]}]`)
		if len(d.Blocks) != 1 || d.Blocks[0].Kind != CarouselBlock || d.Blocks[0].Carousel == nil {
			t.Fatalf("%s: %s", extra, dump(d.Blocks))
		}
		b := d.Blocks[0]
		if b.BlockID != "cr" || b.Carousel.Label != "Picks" || !b.Carousel.LabelVisible {
			t.Errorf("%s: carousel %+v", extra, b.Carousel)
		}
		if len(b.Children) != 2 || b.Children[0].Kind != CardBlock || !b.Children[0].Kit || b.Children[1].BlockID != "b" {
			t.Errorf("%s: cards %s", extra, dump(b.Children))
		}
	}
	if d := blocksMsg(t, `[{"type":"carousel","label":"Picks","elements":[`+card("a")+`]}]`); d.Blocks[0].Carousel.LabelVisible {
		t.Errorf("a label without is_label_visible shows")
	}
	if d := blocksMsg(t, `[{"type":"carousel","is_label_visible":true,"elements":[`+card("a")+`]}]`); d.Blocks[0].Carousel.LabelVisible {
		t.Errorf("is_label_visible without a label shows one")
	}
	if d := blocksMsg(t, `[{"type":"carousel","elements":"x"}]`); len(d.Blocks) != 1 || d.Blocks[0].Kind != Unsupported {
		t.Errorf("elements a string: %s, want the error line", dump(d.Blocks))
	}
	if d := blocksMsg(t, `[{"type":"carousel"}]`); len(d.Blocks) != 1 || d.Blocks[0].Kind != CarouselBlock || len(d.Blocks[0].Children) != 0 {
		t.Errorf("no elements: %s, want an empty carousel", dump(d.Blocks))
	}
}

// A carousel's plain text is its label when it shows (Slack's
// convertStringTitledBlockToPlainText); its cards give none outside a
// message summary (convertCardBlockToPlainText), so a carousel of cards
// reads as the message's own text (convertMessageToPlainText's
// getMessageText fallback).
func TestCarouselPlainText(t *testing.T) {
	cards := `"elements":[{"type":"card","title":{"type":"plain_text","text":"Pick 0"}}]`
	for _, c := range []struct {
		js, want string
	}{
		{`{"ts":"1.0","text":"Weekly picks","blocks":[{"type":"carousel",` + cards + `}]}`, "Weekly picks"},
		{`{"ts":"1.0","text":"Weekly picks","blocks":[{"type":"carousel","label":"Picks","is_label_visible":true,` + cards + `}]}`, "Picks"},
		{`{"ts":"1.0","text":"Weekly picks","blocks":[{"type":"carousel","label":"Picks",` + cards + `}]}`, "Weekly picks"},
	} {
		d := FromMessage(fakeRes{}, msg(t, c.js))
		if got := PlainText(d); got != c.want {
			t.Errorf("%s: plain text %q, want %q", c.js, got, c.want)
		}
	}
}

// recordingRes is fakeRes that keeps what it was asked, as core's
// refNames records the members, files and bots a conversion read.
type recordingRes struct {
	fakeRes
	asked []string
}

func (r *recordingRes) UserName(id string) string {
	r.asked = append(r.asked, id)
	return r.fakeRes.UserName(id)
}

// A message's Doc keeps nothing of the resolver it was converted through:
// converted again, through another one that has read other things since,
// it is DeepEqual to the first, as messages' takeConverted compares them
// (a Doc that held its resolver differed every time, and kept what that
// resolver held). The carousel's fallback, the message's text, is
// converted at once, its mention named then.
func TestCarouselDocKeepsNoResolver(t *testing.T) {
	m := msg(t, `{"ts":"1.0","text":"Picks for <@U1>","blocks":[{"type":"carousel","elements":[{"type":"card","title":{"type":"plain_text","text":"Pick 0"}}]}]}`)
	r1, r2 := &recordingRes{asked: []string{"UEARLIER"}}, &recordingRes{}
	d1, d2 := FromMessage(r1, m), FromMessage(r2, m)
	if !reflect.DeepEqual(d1, d2) {
		t.Errorf("two conversions of one message differ:\n%+v\n%+v", d1, d2)
	}
	if got := PlainText(d2); got != "Picks for @alice" {
		t.Errorf("plain text %q, want the message's text", got)
	}
	// a message whose blocks give text keeps no fallback
	d := FromMessage(fakeRes{}, msg(t, `{"ts":"1.0","text":"Picks","blocks":[{"type":"section","text":{"type":"mrkdwn","text":"Weekly"}}]}`))
	if d.Fallback != nil {
		t.Errorf("a section's message kept a fallback %+v", d.Fallback)
	}
}
