package canvasview

import (
	"image"
	"strings"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// setUnfurl makes link's cards name the container ts in the fake's card
// channel, answered with m (nil: without it).
func setUnfurl(fs *fakeslack.Server, link, ts string, m *slack.Message) {
	fs.SetLinkUnfurl(link, fakeslack.CardChannel, ts, m)
}

// unfurledAs gives container m its document_unfurl.is_unfurled.
func unfurledAs(m *slack.Message, on bool) *slack.Message {
	m.DocumentUnfurl = &slack.DocumentUnfurl{IsUnfurled: on}
	return m
}

// The link card tests read the view only through these (a red tree swaps
// this file for the old code's equivalents: the Embed block's own text).

// drawnText is what block b draws as text: a link card's text, else its
// richview's.
func drawnText(b *blk) string {
	if b.card != nil {
		return cardTextOf(b.card)
	}
	if b.rv == nil {
		return ""
	}
	return b.rv.TextRange(0, b.rv.PlainLen())
}

// embedBlk is the view's first Embed block (nil: none).
func embedBlk(v *View) *blk {
	if bs := cardBlocks(v.blocks); len(bs) > 0 {
		return bs[0]
	}
	return nil
}

// embedText is the first Embed block's text ("" with none).
func embedText(v *View) string {
	if b := embedBlk(v); b != nil {
		return drawnText(b)
	}
	return ""
}

func cardTextOf(c *linkCard) string {
	var out []string
	for _, rv := range []*richview.View{c.title, c.content, c.body} {
		if rv != nil {
			out = append(out, rv.TextRange(0, rv.PlainLen()))
		}
	}
	return strings.Join(out, "\n")
}

// cardState is the first card's kind ("empty", "loading", "fallback";
// "" with no card), its drawn size, and whether its attachment (fallback)
// is drawn without its colour bar and width cap.
func cardState(v *View) (kind string, size image.Point, borderless bool) {
	b := embedBlk(v)
	if b == nil {
		return "", image.Point{}, false
	}
	c := b.card
	kind = map[cardKind]string{cardEmpty: "empty", cardLoading: "loading", cardFallback: "fallback"}[c.kind]
	if c.kind == cardFallback && c.body != nil {
		att := c.body.Doc().Blocks[0].Att
		borderless = att != nil && att.HideBorder && att.NoMaxWidth
	}
	return kind, c.size, borderless
}
