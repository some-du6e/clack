package popups

// A result card's body, attachments and files, as SearchResult (boot-shared
// IT2K, CDN build a09b625d90aa) draws them with the density card (FvhF Ui,
// everyone's but a dogfood user's "original" density; search_card.go):
//
//   - renderBlocks: the message's blocks through the rich text renderer,
//     so mentions, channel links, emoji, links and code look as they do
//     in the channel. Compacted (not expanded), a message of more than
//     three blocks shows compactCardBlocks' three from the first
//     highlighted one. The body sits in SearchMessageClampedBody
//     (.c-search_message__body--card_clamp, max-height 132px), clipped
//     (.c-search_message__body--card_clipped: overflow hidden,
//     mask-image linear-gradient(#000 60%, #0000)) when its content is
//     taller or isCardBodyClipped's heuristic holds; renderExpandButton's
//     "... Show more" follows. Expanded, the blocks are drawn whole, with
//     no Show less. A message without blocks (renderText) is drawn the
//     same way here: Clack keeps no search extracts, which Slack's
//     renderText would show instead.
//   - The query's words are highlighted (c-mrkdwn__highlight,
//     surf-hgl-3). Slack marks the runs the server put highlight markers
//     around; Clack strips those and marks what the results rows mark
//     (srHighlight: the words a term starts, a wildcard term's stem).
//   - renderVisibleAttachmentsGrid, renderAttachments, renderMediaFiles,
//     renderAudioFiles: only the attachments and files holding a match
//     (core.SearchMatches: attachmentHasSearchMatch, fileHasSearchMatch),
//     or all of them when nothing in the message matched
//     (hasNoVisibleContent). Hidden attachments never show, nor does a
//     forwarded message without a match. While compacted at most two
//     attachments show (capForCard), in .c-search_message__attachments
//     (margin-top 8px, max-width 600px), clipped like the body
//     (willClipCardAttachments: no body Show more, and capped or their
//     text fields past 600 characters) with its own "... Show more".
//     They draw as in the channel (renderFullAttachment).
//
// The card used to flatten all of this into plain text lines
// (richtext.SearchText): an X/Twitter unfurl came out as its author and
// text under the message, mentions as plain "@name", and the clamp left a
// gap for lines it didn't draw.

import (
	"encoding/json"
	"image"
	"image/color"
	"strconv"
	"strings"
	"unicode/utf8"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// srClampDp is .c-search_message__body--card_clamp's (and
// --card_clipped's) max-height.
const srClampDp = 132

// srAttsMaxDp is .c-search_message__attachments' max-width.
const srAttsMaxDp = 600

// srCardRich is a card's rich body and its attachments and files.
type srCardRich struct {
	key   string         // what the views were built from
	body  *richview.View // nil: no body (only attachments or files)
	extra *richview.View // the shown attachments and files; nil: none
	// capped: capForCard left attachments out; long: the shown
	// attachments' text past 600 characters (getTextFieldsLength)
	capped, long bool
	moreAtt      widget.Clickable // the attachments' "... Show more"
}

// srRichParts are a card's laid-out parts for one frame.
type srRichParts struct {
	body, extra         op.CallOp
	bodyH, extraH       int // the drawn (clamped) heights
	bodyClip, extraClip bool
	h                   int // everything, the Show more lines included
}

// srRichDocs splits doc into the body and the shown attachments and
// files for a card (compact: not expanded), as SearchResult picks them.
func srRichDocs(doc richtext.Doc, sm *core.SearchMatches, compact bool) (body, extra []richtext.Block, capped bool) {
	var atts, files []richtext.Block
	for _, b := range doc.Blocks {
		switch {
		case b.Kind == richtext.FileStatus:
		case b.Kind == richtext.FileBlock && b.File != nil:
			files = append(files, b)
		case b.Kind == richtext.AttachmentBlock && b.Att != nil && b.Att.Huddle == nil:
			atts = append(atts, b)
		default:
			body = append(body, b)
		}
	}
	// keyed by the attachment's index in the answer's attachments
	// (core.searchMatches), which counts what isn't an attachment block
	// here: an agent channel unfurl, an attachment that draws nothing
	attMatch := func(a *richtext.Attachment) bool {
		return sm != nil && sm.Attachments[core.AttachmentKey(a.ID, a.Index)]
	}
	fileMatch := func(f *richtext.File) bool { return sm != nil && sm.Files[f.ID] }
	matched := false
	for _, b := range atts {
		matched = matched || attMatch(b.Att)
	}
	for _, b := range files {
		matched = matched || fileMatch(b.File)
	}
	none := len(body) == 0 && !matched // hasNoVisibleContent
	var shown []richtext.Block
	for _, b := range atts {
		a := b.Att
		switch {
		case a.Hidden:
		case (a.IsShare || a.MsgCard) && !attMatch(a): // a forwarded message only with a match
		case none || attMatch(a):
			shown = append(shown, b)
		}
	}
	if compact && len(shown) > 2 {
		shown, capped = shown[:2], true
	}
	extra = shown
	for _, b := range files {
		if none || fileMatch(b.File) {
			extra = append(extra, b)
		}
	}
	return body, extra, capped
}

// srCompactBody is renderBlocks' compactCardBlocks body for m, when it
// keeps fewer blocks than the message has (nil otherwise).
func (v *SearchView) srCompactBody(m *core.Message) []richtext.Block {
	app := v.h.Core()
	if app == nil || app.S == nil || len(m.Blocks) == 0 {
		return nil
	}
	var blocks []json.RawMessage
	if json.Unmarshal(m.Blocks, &blocks) != nil || len(blocks) <= 3 {
		return nil
	}
	kept := srCompactBlocks(blocks, m.SearchHitBlock)
	raw, err := json.Marshal(kept)
	if err != nil {
		return nil
	}
	d := richtext.FromMessage(app.S, &slack.Message{Type: "message", TS: m.TS, User: m.User, Text: m.Text, Blocks: raw})
	return d.Blocks
}

// srRichOf is card i's views, built again when its message, the plugin
// text, the query or its expanded state changes.
func (v *SearchView) srRichOf(i int) *srCardRich {
	c := &v.cards[i]
	m := &v.msgs[i]
	terms := v.q.Highlights()
	key := m.Channel + "/" + m.TS + "\x00" + m.EditedTS + "\x00" + c.ovText + "\x00" + strings.Join(terms, "\x01") +
		"\x00" + strconv.FormatBool(c.expanded)
	r := &c.rich
	if r.key == key {
		return r
	}
	r.key = key
	doc := m.Doc
	if c.ovText != "" {
		doc = c.ovDoc
	}
	if txt := firstNonEmptyStr(c.ovText, m.Text); len(doc.Blocks) == 0 && strings.TrimSpace(txt) != "" { // read without its document
		if app := v.h.Core(); app != nil && app.S != nil {
			doc = richtext.Doc{Blocks: richtext.FromMrkdwn(app.S, txt)}
		} else {
			doc = richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: []richtext.Span{{Kind: richtext.Text, Text: txt}}}}}
		}
	}
	compact := !c.expanded
	body, extra, capped := srRichDocs(doc, m.SearchMatches, compact)
	if compact && c.ovText == "" && c.dense.on {
		if kb := v.srCompactBody(m); kb != nil {
			body, _, _ = srRichDocs(richtext.Doc{Blocks: kb}, nil, true)
		}
	}
	r.capped = capped
	r.long = len([]rune(richtext.PlainText(richtext.Doc{Blocks: extra}))) > 600
	r.body, r.extra = nil, nil
	o := richview.Options{Channel: m.Channel, TS: m.TS}
	if len(body) > 0 {
		bo := o
		bo.Edited = m.DisplaysEdited()
		r.body = richview.New(richtext.Doc{Blocks: srHighlightCopy(body, terms)}, bo)
	}
	if len(extra) > 0 {
		r.extra = richview.New(richtext.Doc{Blocks: srHighlightCopy(extra, terms)}, o)
	}
	return r
}

// srHighlightCopy is blocks with the query's terms marked as srHighlight
// marks them in the rows (a word a term starts, as Slack highlights
// "annoying" for "ann"; a wildcard term's stem; a phrase anywhere),
// copying what it changes.
func srHighlightCopy(blocks []richtext.Block, terms []string) []richtext.Block {
	if len(terms) == 0 {
		return blocks
	}
	return richtext.HighlightedCopy(blocks, func(text []rune) []richtext.Match {
		var out []richtext.Match
		at := 0
		for _, sg := range srHighlight(string(text), terms) {
			n := utf8.RuneCountInString(sg.Text)
			if sg.Hit {
				out = append(out, richtext.Match{Index: at, Length: n})
			}
			at += n
		}
		return out
	})
}

// srRichStyle is the message rows' rich style (RichStyle), else one of
// the theme's own (tests without the ui root).
func (v *SearchView) srRichStyle(th *theme.Theme) *richview.Style {
	if RichStyle != nil {
		if st := RichStyle(v.h); st != nil {
			return st
		}
	}
	dark := th != nil && th.Dark
	if v.richSt == nil || v.richDark != dark {
		p := richview.LightPalette()
		if dark {
			p = richview.DarkPalette()
		}
		v.richSt, v.richDark = richview.NewStyle(th.Shaper, nil, p), dark
	}
	return v.richSt
}

// srRichMeasure lays out card i's body and extras bw wide (recorded, not
// yet drawn) and works out the clamps.
func (v *SearchView) srRichMeasure(gtx layout.Context, th *theme.Theme, i, bw, lh int) srRichParts {
	c := &v.cards[i]
	r := v.srRichOf(i)
	st := v.srRichStyle(th)
	hd := srCardHandler{srWOHandler{v}, i}
	clamp := dp(gtx, srClampDp)
	var p srRichParts
	if r.body != nil {
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(bw, 1<<20)}
		rec := op.Record(gtx.Ops)
		d := r.body.Layout(g, st, hd)
		p.body = rec.Stop()
		p.bodyH = d.Size.Y
		if !c.expanded && (c.dense.heur || d.Size.Y > clamp) {
			p.bodyClip = true
			p.bodyH = min(p.bodyH, clamp)
		}
		p.h += p.bodyH
		if p.bodyClip {
			p.h += lh
		}
	}
	if r.extra != nil {
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(min(bw, dp(gtx, srAttsMaxDp)), 1<<20)}
		rec := op.Record(gtx.Ops)
		d := r.extra.Layout(g, st, hd)
		p.extra = rec.Stop()
		p.extraH = d.Size.Y
		// willClipCardAttachments
		if !c.expanded && !p.bodyClip && (r.capped || r.long) {
			p.extraClip = true
			p.extraH = min(p.extraH, clamp)
		}
		if r.body != nil {
			p.h += dp(gtx, 8) // .c-search_message__attachments: margin-top 8px
		}
		p.h += p.extraH
		if p.extraClip {
			p.h += lh
		}
	}
	return p
}

// srRichDraw draws card c's laid-out parts at (x, y), bw wide, over bg,
// and returns the height they take.
func (v *SearchView) srRichDraw(gtx layout.Context, th *theme.Theme, cl *Colors, c *srCard, p srRichParts, x, y, bw, lh int, bg color.NRGBA) int {
	y0 := y
	part := func(call op.CallOp, h int, clipped bool, more *widget.Clickable) {
		st := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
		cs := clip.Rect(image.Rect(0, 0, bw, h)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		cs.Pop()
		st.Pop()
		if !clipped {
			y += h
			return
		}
		srFade(gtx, image.Rect(x, y, x+bw, y+h), bg)
		y += h
		srShowMore(gtx, th, cl, more, x, y, lh)
		y += lh
	}
	if c.rich.body != nil {
		part(p.body, p.bodyH, p.bodyClip, &c.more)
	}
	if c.rich.extra != nil {
		if c.rich.body != nil {
			y += dp(gtx, 8)
		}
		part(p.extra, p.extraH, p.extraClip, &c.rich.moreAtt)
	}
	return y - y0
}

// srFade is the clipped box's mask-image: linear-gradient(#000 60%,
// #0000), drawn as the card's background fading in over the box's last
// 40%.
func srFade(gtx layout.Context, r image.Rectangle, bg color.NRGBA) {
	from := float32(r.Min.Y) + float32(r.Dy())*0.6
	to := float32(r.Max.Y)
	paint.LinearGradientOp{Stop1: f32.Pt(0, from), Stop2: f32.Pt(0, to),
		Color1: color.NRGBA{R: bg.R, G: bg.G, B: bg.B}, Color2: bg}.Add(gtx.Ops)
	st := clip.Rect(image.Rect(r.Min.X, int(from), r.Max.X, r.Max.Y)).Push(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	st.Pop()
}

// srShowMore is renderExpandButton (c-search__expand: 13px, margin 0 4px,
// foreground_max_solid, highlight hovered) on its own line at (x, y).
func srShowMore(gtx layout.Context, th *theme.Theme, cl *Colors, more *widget.Clickable, x, y, lh int) {
	at(gtx, image.Pt(x+dp(gtx, 4), y), func(gtx layout.Context) layout.Dimensions {
		return clickBox(gtx, more, nil, func(gtx layout.Context) layout.Dimensions {
			fg := cl.Muted // --sk_foreground_max_solid
			if more.Hovered() {
				fg = cl.Link // --sk_highlight
			}
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, fg, srShowMoreLabel())
			})
		})
	})
}

// srCardHandler routes card i's rich content clicks: as a work object
// card's, a file opens its permalink in the lightbox, and a click on the
// text opens the message.
type srCardHandler struct {
	srWOHandler
	i int
}

// PlainClick is a click on the card's text or between it, not on a link,
// mention or button: SearchResult's onClick opens the message for it, as
// the card's own click target does (the text is drawn over that target
// and takes its presses).
func (h srCardHandler) PlainClick() {
	if h.i >= len(h.v.msgs) {
		return
	}
	m := h.v.msgs[h.i]
	RecordRecentConversation(m.Channel)
	openResult(h.v.h, m)
}

func (h srCardHandler) OpenFile(f *richtext.File) {
	if f == nil {
		return
	}
	switch {
	case f.Permalink != "":
		lbOpen(h.v.h, f.Permalink)
	case f.URL != "":
		lbOpen(h.v.h, f.URL)
	}
}

// richPlain is the card's drawn text (tests): the body's and extras'
// plain text.
func (c *srCard) richPlain() string {
	var parts []string
	for _, rv := range []*richview.View{c.rich.body, c.rich.extra} {
		if rv != nil {
			parts = append(parts, richtext.PlainText(rv.Doc()))
		}
	}
	return strings.Join(parts, "\n")
}
