package fakeslack

import (
	"fmt"
	"strings"
)

// The carousel block (Slack's CarouselBlock in its GalleryScroller):
// cards side by side, scrolled sideways with the arrows. Synthetic.

func init() { RegisterGallery("21-carousel", galleryCarousel) }

func galleryCarousel(e GalleryEnv) []GalleryMessage {
	card := func(i int, body string) string {
		c := fmt.Sprintf(`{"type":"card","block_id":"crd%d","title":{"type":"plain_text","text":"Option %d"},"subtitle":{"type":"plain_text","text":"Team pick"}`, i, i)
		if body != "" {
			c += `,"body":{"type":"mrkdwn","text":"` + body + `"}`
		}
		return c + `,"actions":[{"type":"button","action_id":"pick` + fmt.Sprint(i) + `","text":{"type":"plain_text","text":"Choose"},"value":"` + fmt.Sprint(i) + `"}]}`
	}
	cards := func(n int) string {
		var cs []string
		for i := range n {
			body := ""
			if i%2 == 1 {
				body = "A longer body for this one, so the row stretches every card to the tallest."
			}
			cs = append(cs, card(i+1, body))
		}
		return strings.Join(cs, ",")
	}
	return []GalleryMessage{
		{Name: "carousel/five cards", Text: "Five options", Blocks: `[{"type":"carousel","block_id":"cr1","elements":[` + cards(5) + `]}]`},
		{Name: "carousel/labelled", Text: "A labelled carousel", Blocks: `[{"type":"carousel","block_id":"cr2","label":"Recommended for you","is_label_visible":true,"elements":[` + cards(3) + `]}]`},
		{Name: "carousel/one card", Text: "One card (no arrows)", Blocks: `[{"type":"carousel","block_id":"cr3","elements":[` + card(1, "") + `]}]`},
		{Name: "carousel/a section among the cards", Text: "Mixed elements", Blocks: `[{"type":"carousel","block_id":"cr4","elements":[` + card(1, "") +
			`,{"type":"section","text":{"type":"mrkdwn","text":"A *section* as an element"}},` + card(2, "") + `]}]`},
	}
}
