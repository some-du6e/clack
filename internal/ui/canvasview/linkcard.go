package canvasview

// Link cards (docs/CANVASES.md "Link cards"). Quip's export holds only a
// card's placeholder, "Link URL: <url>"; Slack's client draws the card
// from Quip's model: a type-56 section whose ContentLinkUnfurlCard names
// an unfurl container message (core.CanvasCards), fetched as Slack's
// ensureMessages fetches it (core.FetchCanvasCardContainers) and drawn by
// Slack's Card (client-boot-shared, useEmbeddedUnfurlType's kinds) in the
// webapp's card-view: EmptyUnfurl, LoadingFallbackUnfurl, or
// EmbeddedFallbackUnfurl's borderless pillow around the container's
// attachment. Never the export's text.

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"math"
	"strconv"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/quip"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// nsFiles is EmptyUnfurl's namespace (module As10).
var nsFiles = i18n.NS("files")

type cardKind uint8

const (
	cardEmpty    cardKind = iota // EmptyUnfurl
	cardLoading                  // LoadingFallbackUnfurl
	cardFallback                 // EmbeddedFallbackUnfurl's MessagePillow
)

// Card's waits: a fallback card stops waiting for its container's
// attachment after 20 s (setTimeout(…, 2e4)), at once for a container over
// 10 minutes old.
const (
	cardTimeout   = 20 * time.Second
	cardOldUnfurl = 10 * time.Minute
)

// linkCard is an Embed block's card.
type linkCard struct {
	url     string
	modelOK bool            // core holds the canvas's model
	cc      core.CanvasCard // its card section, paired by url
	held    bool            // the store holds cc's container
	msg     slack.Message   // the container
	attKey  string          // what msg's attachments are (body's key)
	asked   bool            // its container was fetched
	sig     string          // cc.Sig last seen: a change fetches again
	mounted time.Time       // Card's mount: its first frame
	kind    cardKind        // what the views below draw
	line    string          // EmptyUnfurl's content line
	drawn   string          // kind, line and attKey the views are of
	title   *richview.View  // EmptyUnfurl's title (the card's URL)
	content *richview.View  // EmptyUnfurl's line
	body    *richview.View  // the fallback's attachment
	hover   bool
	tag     struct{ _ byte } // the hover area
	size    image.Point      // the card as last drawn
}

// blockedStates are canvas_unfurl_state values that are an error state even
// with an attachment (useUnfurlErrorState).
var blockedStates = map[string]bool{"exact_link_blocked": true, "all_links_from_domain": true, "all_links_under_path": true}

// emptyLine is EmptyUnfurl's content line for an error state (As10).
func emptyLine(state string) string {
	switch state {
	case "exact_link_blocked":
		return nsFiles.T("Your admins have turned off previews for this link.")
	case "all_links_from_domain", "all_links_under_path":
		return nsFiles.T("Your admins have turned off previews for this domain.")
	case "msg_non_existent":
		return nsFiles.T("This message has been deleted or does not exist.")
	case "msg_tombstoned":
		return nsFiles.T("This message can’t be shown.")
	case "msg_private":
		return nsFiles.T("You don’t have access to this private message.")
	case "file_deleted":
		return nsFiles.T("This file was deleted.")
	case "file_private":
		return nsFiles.T("You don’t have access to this file.")
	case "file_not_found":
		return nsFiles.T("This file was not found.")
	}
	return nsFiles.T("Unable to create preview.")
}

// tsAge is how long ago Slack ts was (0 when it doesn't parse).
func tsAge(ts string, now time.Time) time.Duration {
	f, err := strconv.ParseFloat(ts, 64)
	if err != nil || f <= 0 {
		return 0
	}
	sec, frac := math.Modf(f)
	return now.Sub(time.Unix(int64(sec), int64(frac*1e9)))
}

// decide is what Card draws now (useEmbeddedUnfurlType, then Card's
// "fallback" case), with EmptyUnfurl's line, and when that may change by
// itself (Card's timeout; zero: never).
func (c *linkCard) decide(now time.Time) (k cardKind, line string, at time.Time) {
	unable := emptyLine("")
	// no model, no card section for the export's card, a kind Clack can't
	// draw yet, or a Slack permalink (Slack routes those to their own
	// cards): EmptyUnfurl, never the export's placeholder
	if !c.modelOK || c.cc.Section == "" || c.cc.Type != quip.CardSlackUnfurl || !c.cc.HasContainer() || slackPage(c.url) {
		return cardEmpty, unable, time.Time{}
	}
	var att *slack.Attachment
	if c.held && len(c.msg.Attachments) > 0 {
		att = &c.msg.Attachments[0]
	}
	du := c.msg.DocumentUnfurl
	unfurled := du != nil && du.IsUnfurled
	loading := c.held && !unfurled // isLoading: a stored container not unfurled yet
	errLine := ""
	if p := uipref.Get(); p.NoInlineImgs && p.NoTextPreviews { // hideImgAndContent: no line of its own
		errLine = unable
	} else if c.held && du != nil && du.CanvasUnfurlState != "" && (att == nil || blockedStates[du.CanvasUnfurlState]) {
		errLine = emptyLine(du.CanvasUnfurlState)
	}
	if !loading && (!c.held || att == nil || errLine != "") {
		return cardEmpty, firstNonEmpty(errLine, unable), time.Time{}
	}
	// "fallback" (a public link card is drawn as one too until
	// EmbeddedPublicLinkUnfurl is ported)
	deadline := c.mounted.Add(cardTimeout)
	timedOut := !c.mounted.IsZero() && !now.Before(deadline) || c.held && du != nil && tsAge(c.msg.TS, now) > cardOldUnfurl
	if timedOut && att == nil {
		return cardEmpty, unable, time.Time{}
	}
	if loading { // EmbeddedFallbackUnfurl while isUnfurling
		if att == nil && !timedOut {
			at = deadline
		}
		return cardLoading, "", at
	}
	return cardFallback, "", time.Time{}
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

// cardBlocks are the view's link card blocks in document order.
func cardBlocks(bs []*blk) []*blk {
	var out []*blk
	var walk func(bs []*blk)
	walk = func(bs []*blk) {
		for _, b := range bs {
			if b.card != nil {
				out = append(out, b)
			}
			walk(b.kids)
			for _, c := range b.cols {
				walk(c)
			}
		}
	}
	walk(bs)
	return out
}

// hasLinkCards: the document has a link card (its model is then loaded,
// read-only canvases and peeks too: Slack always draws from the model).
func hasLinkCards(bs []canvas.Block) bool {
	for i := range bs {
		b := &bs[i]
		if b.Kind == canvas.Embed || hasLinkCards(b.Children) {
			return true
		}
		for _, c := range b.Columns {
			if hasLinkCards(c) {
				return true
			}
		}
	}
	return false
}

// refreshCards pairs the view's link cards with the model's card sections
// and reads their containers from the store, fetching the ones it doesn't
// hold (and a changed section's anew); it runs again once they're in.
func (v *View) refreshCards() {
	a := v.h.Core()
	bs := cardBlocks(v.blocks)
	if a == nil || len(bs) == 0 {
		return
	}
	urls := make([]string, len(bs))
	for i, b := range bs {
		urls[i] = b.card.url
	}
	cards, ok := a.CanvasCards(v.id, urls)
	var fetch, again []core.CanvasCard
	for i, b := range bs {
		c := b.card
		c.modelOK = ok
		c.cc = core.CanvasCard{URL: c.url}
		if ok {
			c.cc = cards[i]
		}
		c.msg, c.held = a.CanvasCardContainer(c.cc)
		c.attKey = ""
		if c.held {
			k, _ := json.Marshal(struct {
				A []slack.Attachment
				D *slack.DocumentUnfurl
			}{c.msg.Attachments, c.msg.DocumentUnfurl})
			c.attKey = string(k)
		}
		switch {
		case !c.cc.HasContainer():
		case c.sig != "" && c.cc.Sig != c.sig: // a document_update changed the card
			again = append(again, c.cc)
		case !c.held && !c.asked:
			fetch = append(fetch, c.cc)
		}
		if c.cc.HasContainer() {
			c.asked = true
		}
		c.sig = c.cc.Sig
	}
	for _, f := range []struct {
		cards []core.CanvasCard
		again bool
	}{{fetch, false}, {again, true}} {
		if len(f.cards) == 0 {
			continue
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			a.FetchCanvasCardContainers(ctx, f.cards, f.again)
			v.post(v.refreshCards)
		}()
	}
	v.h.Invalidate()
}

// cardViews makes the card's views for what it draws now.
func (v *View) cardViews(c *linkCard) {
	key := string(rune('0'+c.kind)) + "\x00" + c.line + "\x00" + c.url + "\x00" + c.attKey
	if key == c.drawn {
		return
	}
	c.drawn = key
	c.title, c.content, c.body = nil, nil, nil
	switch c.kind {
	case cardEmpty:
		if c.url != "" {
			c.title = richview.New(richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph,
				Spans: []richtext.Span{{Kind: richtext.Link, Text: c.url, URL: c.url, Style: richtext.Bold}}}}}, richview.Options{})
		}
		c.content = richview.New(richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph,
			Spans: []richtext.Span{{Kind: richtext.Text, Text: c.line}}}}}, richview.Options{})
	case cardFallback:
		a := v.h.Core()
		if a == nil {
			return
		}
		m := c.msg
		m.Text, m.Blocks, m.Files = "", nil, nil // the pillow draws the attachment alone (renderMessageAttachment)
		d := richtext.FromMessage(a.S, &m)
		for _, b := range d.Blocks {
			if b.Kind != richtext.AttachmentBlock || b.Att == nil {
				continue
			}
			att := *b.Att
			att.HideBorder, att.NoMaxWidth = true, true // hideBorder, unsetWidthStyle
			b.Att = &att
			c.body = richview.New(richtext.Doc{Blocks: []richtext.Block{b}}, richview.Options{})
			break
		}
	}
}

// layoutCard draws an Embed block's card w wide: Slack's card-view
// generic (.section[data-section-type="56"] .card-view: base-pry, radius
// 12, a 1px otl-ter ring, the column's full width; on hover a
// 0 1px 3px #00000014 shadow under the ring) around what Card draws.
func (v *View) layoutCard(gtx layout.Context, b *blk, w int) int {
	c := b.card
	now := gtx.Now
	if now.IsZero() {
		now = time.Now()
	}
	if c.mounted.IsZero() {
		c.mounted = now
	}
	k, line, at := c.decide(now)
	c.kind, c.line = k, line
	if !at.IsZero() {
		gtx.Execute(op.InvalidateCmd{At: at})
	}
	v.cardViews(c)
	st := v.styles.card
	p := &st.Palette
	pad := gtx.Dp(12)
	iw := max(0, w-2*pad)
	m := op.Record(gtx.Ops)
	h := 0
	switch {
	case c.kind == cardLoading:
		h = v.layoutCardSkeleton(gtx, w, now)
	case c.kind == cardFallback && c.body != nil:
		// .p-embedded_fallback_unfurl: the pillow without its border, 12px all round
		func() {
			defer op.Offset(image.Pt(pad, pad)).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Constraints{Max: image.Pt(iw, gtx.Constraints.Max.Y)}
			h = c.body.Layout(g, st, v.handler).Size.Y
		}()
		h += 2 * pad
	default: // .p-empty_unfurl: a column, padded 12
		y := pad
		if c.title != nil {
			func() {
				defer op.Offset(image.Pt(pad, y)).Push(gtx.Ops).Pop()
				g := gtx
				g.Constraints = layout.Constraints{Max: image.Pt(iw, gtx.Constraints.Max.Y)}
				y += c.title.Layout(g, v.styles.cardTitle, v.handler).Size.Y + gtx.Dp(12) // __title's margin-bottom
			}()
		}
		if c.content != nil {
			func() {
				defer op.Offset(image.Pt(pad, y)).Push(gtx.Ops).Pop()
				g := gtx
				g.Constraints = layout.Constraints{Max: image.Pt(iw, gtx.Constraints.Max.Y)}
				y += c.content.Layout(g, v.styles.cardSm, v.handler).Size.Y
			}()
		}
		h = y + pad
	}
	call := m.Stop()
	r := image.Rect(0, 0, w, h)
	c.size = r.Size()
	c.hoverEvents(gtx)
	if c.hover {
		theme.Shadow(gtx, r.Add(image.Pt(0, gtx.Dp(1))), gtx.Dp(12), gtx.Dp(3), color.NRGBA{A: 0x14})
	}
	ring := gtx.Dp(1)
	theme.RRect(gtx, r.Inset(-ring), gtx.Dp(12)+ring, p.Border) // box-shadow 0 0 0 1px: outside the box
	theme.RRect(gtx, r, gtx.Dp(12), p.Panel)
	call.Add(gtx.Ops)
	func() { // the hover area, over the card; presses pass through
		defer clip.Rect(r).Push(gtx.Ops).Pop()
		defer pointer.PassOp{}.Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &c.tag)
	}()
	return h
}

func (c *linkCard) hoverEvents(gtx layout.Context) {
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &c.tag, Kinds: pointer.Enter | pointer.Leave | pointer.Move | pointer.Cancel})
		if !ok {
			return
		}
		pe, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Enter, pointer.Move:
			c.hover = true
		case pointer.Leave, pointer.Cancel:
			c.hover = false
		}
	}
}

// layoutCardSkeleton is LoadingFallbackUnfurl: four MissingText lines (the
// whole width, the whole width, half, half; 8px tall, fully rounded, 8px
// above and below each) in a column padded 12, in base-ter
// (.c-missing_text--unknown), the whole shimmering (p-hub--shimmer: opacity
// 1, .65, 1 over 1s, ease-in-out) unless a11y_animations is off.
func (v *View) layoutCardSkeleton(gtx layout.Context, w int, now time.Time) int {
	pad, lh, mg := gtx.Dp(12), gtx.Dp(8), gtx.Dp(8)
	col := v.styles.card.Palette.Missing
	if !uipref.Get().NoAnimations {
		t := float64(now.UnixNano()%int64(time.Second)) / float64(time.Second)
		e := easeInOut(1 - math.Abs(2*t-1)) // 0 at the ends, 1 at 50%
		col.A = uint8(float64(col.A)*(1-0.35*e) + 0.5)
		gtx.Execute(op.InvalidateCmd{})
	}
	iw := max(0, w-2*pad)
	y := pad
	for _, f := range []int{1, 1, 2, 2} {
		y += mg
		theme.RRect(gtx, image.Rect(pad, y, pad+iw/f, y+lh), lh/2, col)
		y += lh + mg
	}
	return y + pad
}

// easeInOut is CSS's ease-in-out, cubic-bezier(.42, 0, .58, 1).
func easeInOut(x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	bez := func(a, b, t float64) float64 { u := 1 - t; return 3*u*u*t*a + 3*u*t*t*b + t*t*t }
	lo, hi, t := 0.0, 1.0, x
	for i := 0; i < 30; i++ {
		switch d := bez(0.42, 0.58, t) - x; {
		case math.Abs(d) < 1e-5:
			return bez(0, 1, t)
		case d < 0:
			lo = t
		default:
			hi = t
		}
		t = (lo + hi) / 2
	}
	return bez(0, 1, t)
}
