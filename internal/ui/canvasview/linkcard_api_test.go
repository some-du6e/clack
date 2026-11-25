package canvasview

import (
	"image"
	"image/color"
	"strings"
	"time"

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
	for _, rv := range c.views() {
		if rv != nil {
			out = append(out, rv.TextRange(0, rv.PlainLen()))
		}
	}
	for _, s := range []string{c.cv.title, c.cv.subtitle} { // NoAccessCanvas's, drawn as theme text
		if s != "" {
			out = append(out, s)
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
	kind = map[cardKind]string{cardEmpty: "empty", cardLoading: "loading", cardFallback: "fallback", cardPublic: "public",
		cardPublicLoading: "public-loading", cardWorkObject: "work-object", cardMessage: "message", cardMessageLoading: "message-loading",
		cardMember: "member", cardMemberLoading: "member-loading", cardFileLoading: "file-loading", cardFileImage: "file-image",
		cardFileVideo: "file-video", cardFileAudio: "file-audio", cardFileGeneric: "file-generic", cardCanvasShown: "canvas",
		cardCanvasNoAccess: "canvas-no-access", cardMemberNone: "member-none", cardList: "list", cardListLoading: "list-loading", cardListError: "list-error"}[c.kind]
	if c.kind == cardFallback && c.body != nil {
		att := c.body.Doc().Blocks[0].Att
		borderless = att != nil && att.HideBorder && att.NoMaxWidth
	}
	return kind, c.size, borderless
}

// urlSafeOf is isUrlSafeFromXSS (false: the "Double-check this link" banner).
func urlSafeOf(s string) bool { return urlSafe(s) }

// permalinkOf is parsePermalink's type and ts.
func permalinkOf(s string) (typ, ts string, ok bool) {
	p, ok := parsePermalink(s)
	return p.typ, p.ts, ok
}

// publicShape is shouldRenderPublicLinkCard.
func publicShape(a *slack.Attachment) bool { return publicLinkCard(a) }

// titleHot: the first card's title last drew in its hover colour.
func titleHot(v *View) bool {
	b := embedBlk(v)
	return b != nil && b.card.titleHot
}

// titleLinkColours are the empty card's title link colours at rest and
// while hovered.
func titleLinkColours(v *View) (rest, hot color.NRGBA) {
	return v.styles.cardTitle.Palette.Link, v.styles.cardTitleHot.Palette.Link
}

// memberShown is the member the first card draws ("" with none).
func memberShown(v *View) string {
	if b := embedBlk(v); b != nil && b.card.mb.known {
		return b.card.mb.u.ID
	}
	return ""
}

// setCardTimeout makes Card's wait d for a test; the func puts it back.
func setCardTimeout(d time.Duration) func() {
	old := cardTimeout
	cardTimeout = d
	return func() { cardTimeout = old }
}

// bodyWidth is the first card's work object card's width as drawn.
func bodyWidth(v *View) int {
	if b := embedBlk(v); b != nil {
		return b.card.bodyW
	}
	return 0
}

// publicText is the first card's public link text view (nil: none).
func publicText(v *View) *richview.View {
	if b := embedBlk(v); b != nil && b.card.pub != nil {
		return b.card.pub.text
	}
	return nil
}
