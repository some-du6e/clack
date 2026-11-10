package ui

// The header topic's width rule and its hover card, from Slack's
// ViewHeaderTopic / ModelInfoTopic (view-header chunk W58+, build
// 4d0c78085232) and client-boot-styles:
//
//   - ViewHeaderTopic measures its container (offset) and the topic text
//     (scroll) and marks the container --truncated when the text is wider
//     than the container less the Edit button and 8px (less 40px when
//     there is no Edit, a read-only topic). A truncated topic button is
//     min-width 96px, max-width min(70%, 540px), margin-right 8px.
//   - @container (width < 600px) hides .p-view_header__topic_container:
//     a header narrower than 600px has no topic.
//   - The hover card: hovering a truncated topic (the button, or the card
//     itself, which is inside it) turns its one ellipsized line into the
//     whole topic, wrapped (white-space normal, overflow visible), over
//     the content below (z-index 205), on a box drawn by the text's
//     ::before: inset -8px -12px around the text, radius 6px,
//     rgb(--sk_foreground_min_solid) (#f8f8f8; dark #222529), box-shadow
//     0 0 0 1px rgba(--sk_foreground_low, .13), 0 1px 3px #00000014. The
//     text keeps the button's width, or (ModelInfoTopic updateHoverSize)
//     half the content width when the content (the primary and secondary
//     columns) is under 800px. 13px/18px in content.sec; a click opens
//     the topic dialog when the topic is editable, and links in it open.

import (
	"image"
	"image/color"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/ctxclick"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// topicTruncated is ViewHeaderTopic's measure: the text (full wide) in a
// container avail wide, with the Edit button editW wide (0: none).
func topicTruncated(gtx layout.Context, full, avail, editW int) bool {
	room := avail - gtx.Dp(40)
	if editW > 0 {
		room = avail - editW - gtx.Dp(8)
	}
	return full > room
}

// topicButtonWidth is the topic button's width in a container avail
// wide: its natural width, or, truncated, between 96px and
// min(70%, 540px).
func topicButtonWidth(gtx layout.Context, full, avail int, truncated bool) int {
	if !truncated {
		return min(full, avail)
	}
	return min(max(min(avail*7/10, gtx.Dp(540)), gtx.Dp(96)), avail)
}

// topicCardWidth is the hover card's text width: the button's, or half
// the content width under 800px (--p-modal_info_topic__hover_size).
func topicCardWidth(gtx layout.Context, buttonW, contentW int) int {
	if contentW > 0 && contentW < gtx.Dp(800) {
		return contentW / 2
	}
	return buttonW
}

// topicCardView is the card's rich text view of ch's topic and its style
// (13/18 in fg, links in link blue).
func (a *App) topicCardView(ch string, fg color.NRGBA) (*richview.View, *richview.Style) {
	x := a.hdrX()
	core := a.Core()
	if core == nil {
		return nil, nil
	}
	cv, ok := core.Conv(ch)
	if !ok || cv.Topic == "" {
		return nil, nil
	}
	if x.cardStyle == nil || x.cardStyle.Shaper != a.th.Shaper || x.cardDark != a.th.Dark || x.cardFg != fg {
		var imgs richview.Images
		if a.images != nil {
			imgs = a.images
		}
		pal := richview.LightPalette()
		st := richview.NewStyle(a.th.Shaper, imgs, pal)
		st.Face = theme.Typeface
		st.EmojiURL = a.th.EmojiURL
		st.Embolden = theme.BodyEmboldenFor(theme.LightPalette.Fg)
		if a.th.Dark {
			pal = richview.DarkPalette()
			st.Embolden = theme.BodyEmboldenFor(theme.DarkPalette.Fg)
			st.LightTextFade = 0.235
		}
		pal.Text, pal.Link = fg, a.th.C.Link
		st.SetPalette(pal)
		x.cardStyle, x.cardDark, x.cardFg, x.card = st, a.th.Dark, fg, nil
	}
	key := ch + "\x00" + cv.Topic
	if x.card == nil || x.cardKey != key {
		blocks := cv.TopicDoc
		if len(blocks) == 0 {
			blocks = []richtext.Block{{Kind: richtext.Paragraph, Spans: []richtext.Span{{Text: cv.Topic}}}}
		}
		x.card, x.cardKey = richview.New(richtext.Doc{Blocks: blocks}, richview.Options{Compact: true}), key
	}
	return x.card, x.cardStyle
}

// layoutTopicCard draws the hover card for a topic whose text line starts
// at org (header coordinates, the line's top-left), wrapping at w,
// deferred above the panes. The card keeps itself open while the pointer
// is on it (hdr.topiccard); a click on it, not on a link, opens the topic
// dialog when the topic is editable.
func (a *App) layoutTopicCard(gtx layout.Context, ch string, conv *convInfo, org image.Point, w int, editable bool) {
	c := &a.th.C
	v, st := a.topicCardView(ch, c.FgMuted)
	if v == nil {
		return
	}
	card := a.clicks.get("hdr.topiccard")
	hdrTopicCardShown = true
	m := op.Record(gtx.Ops)
	func() {
		defer op.Offset(org).Push(gtx.Ops).Pop()
		tg := gtx
		tg.Constraints = layout.Constraints{Max: image.Pt(max(w, 1), 1<<20)}
		rec := op.Record(gtx.Ops)
		td := v.Layout(tg, st, cardHandler{a})
		text := rec.Stop()
		box := image.Rect(-gtx.Dp(12), -gtx.Dp(8), w+gtx.Dp(12), max(td.Size.Y, gtx.Dp(18))+gtx.Dp(8))
		// 0 1px 3px 0 #00000014, then the 1px ring, then the fill
		one := max(1, gtx.Dp(1))
		theme.Shadow(gtx, box.Add(image.Pt(0, one)), gtx.Dp(6), gtx.Dp(3), color.NRGBA{A: 0x14})
		theme.RRect(gtx, box.Inset(-one), gtx.Dp(6)+one, c.Border)
		theme.RRect(gtx, box, gtx.Dp(6), c.BgInset)
		func() {
			defer clip.Rect{Max: td.Size}.Push(gtx.Ops).Pop()
			text.Add(gtx.Ops)
		}()
		// the button's area on top, passing events through to the text's
		// links (Slack's onClick skips a click whose target is an <a>)
		defer op.Offset(box.Min).Push(gtx.Ops).Pop()
		bg := gtx
		bg.Constraints = layout.Exact(box.Size())
		if ctxclick.Clicked(bg, card) { // read before Layout drains them (clickArea)
			clickedSet[card] = true
		}
		pass := pointer.PassOp{}.Push(gtx.Ops)
		card.Layout(bg, func(gtx layout.Context) layout.Dimensions {
			if editable && !v.Hovered() {
				pointer.CursorPointer.Add(gtx.Ops)
			}
			return layout.Dimensions{Size: box.Size()}
		})
		pass.Pop()
	}()
	op.Defer(gtx.Ops, m.Stop())
	if clicked(card) && editable && !v.Hovered() {
		a.openTopicDialog(ch, conv)
	}
}

// cardHandler opens what the topic card's links point at.
type cardHandler struct{ a *App }

func (h cardHandler) OpenURL(u string)                  { h.a.OpenURL(u) }
func (h cardHandler) OpenUser(id string, _ image.Point) { h.a.OpenProfile(h.a.PointerPos(), id) }
func (h cardHandler) OpenChannel(id string)             { h.a.Navigate("c/" + id) }
func (cardHandler) OpenUsergroup(string, image.Point)   {}
func (cardHandler) OpenImage(richtext.Image)            {}
func (cardHandler) OpenFile(*richtext.File)             {}
func (cardHandler) Button(richtext.Button)              {}
