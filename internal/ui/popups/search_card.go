package popups

// The search results' density card: SearchResult (boot-shared Wzwx, build
// c07ab895506f) with experimentSrpCardExploreGroupOn, which `+zqw` Ui
// sets with srp_card_explore on (core.SearchCardExplore). The account is
// "on". In that group:
//
//   - renderBlocks (a message with blocks) wraps the body in
//     SearchMessageClampedBody: .c-search_message__body--card_clamp
//     (max-height 132px, six 22px lines). It's clipped
//     (.c-search_message__body--card_clipped: overflow hidden, mask-image
//     linear-gradient(#000 60%, #0000)) when its content is taller than
//     that, or when isCardBodyClipped's heuristic holds: compactCardBlocks
//     kept fewer blocks than the message has (more than three: the three
//     from the first one holding a highlight), or their text fields are
//     longer than 600 characters (getTextFieldsLength). A clipped body is
//     followed by renderExpandButton, "... Show more" (c-search__expand:
//     13px, margin 0 4px, foreground_max_solid, highlight on hover); there
//     is no Show less. Expanded, the body is the message's blocks whole.
//     A message without blocks (renderText) draws as before.
//   - renderReactionsAndReplies is the card row
//     (.c-search_message__reactions--card: margin-top 8, flex-wrap, gap 8)
//     of count pills (.c-search_message__count_pill: 24px, padding 0 8,
//     gap 4, 12px, 1px otl-ter border, otl-sec hovered, base-pry,
//     content-ter, the 14px icon): SearchRepliesAction (comment, the
//     reply count; it opens the thread), renderReactionsAction (reaction,
//     every reaction's count summed; it opens the message, scrolled to
//     its reactions) and renderAttachmentAction (attachment, the
//     attachments and files counted; a click opens the Attachments card).
//     Off, the row is the message's reactions and reply bar.
//   - The pills' tooltips (BaseTooltip, delay 300): the replies' is
//     getThreadLatestReplyText with alwaysShowLastReplyLabel ("Last reply
//     today at 3:04 PM", "Last reply 2 days ago"), or "{n} replies"
//     without a latest reply; when requestor_replied, a second line
//     "You replied" with the viewer's latest reply, ': "…"' quoted
//     (useFetchLatestReplyTimestamp, core.SearchViewerReply;
//     useReplyMessagePreview cuts it at 60, or "[file name]").
//     SearchReactionsTooltip lists the reactions (skin tones folded into
//     their base emoji) as "emoji count" items joined by " · " in at most
//     176px, the rest as "{n} more", then "You reacted with" and the
//     viewer's emoji.
//
// Not drawn by Clack in either group (it draws no attachments, files or
// media in result cards yet): the capped attachments (two at most,
// forwarded ones only with a match), the visible attachments grid and
// the media filters.

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// srClampLines is .c-search_message__body--card_clamp's 132px in the
// body's 22px lines.
const srClampLines = 6

// srTipDelay is the pills' BaseTooltip delay.
const srTipDelay = 300 * time.Millisecond

// srExplore is `+zqw` Ui for h's account.
func srExplore(h Host) bool {
	app := h.Core()
	return app != nil && app.SearchCardExplore()
}

// srDense is a result's density body: on when the message has blocks
// (renderBlocks); segs, compactCardBlocks' text highlighted; heur,
// isCardBodyClipped.
type srDense struct {
	on   bool
	segs []srSeg
	heur bool
}

// srDenseOf builds m's density body for the search's terms.
func srDenseOf(h Host, m *core.Message, terms []string) srDense {
	var blocks []json.RawMessage
	if len(m.Blocks) == 0 || json.Unmarshal(m.Blocks, &blocks) != nil || blocks == nil {
		return srDense{}
	}
	kept := srCompactBlocks(blocks, m.SearchHitBlock)
	d := srDense{on: true, heur: len(kept) < len(blocks) || srTextFieldsLen(kept) > 600}
	body := srBody(m)
	if len(kept) < len(blocks) {
		if app := h.Core(); app != nil && app.S != nil {
			raw, _ := json.Marshal(kept)
			doc := richtext.FromMessage(app.S, &slack.Message{Type: "message", TS: m.TS, User: m.User, Text: m.Text, Blocks: raw})
			if s := richtext.SearchText(doc); strings.TrimSpace(s) != "" {
				body = s
			}
		}
	}
	d.segs = srHighlight(strings.TrimSpace(body), terms)
	return d
}

// srCompactBlocks is compactCardBlocks: up to three blocks keep whole;
// longer, the three from the first highlighted one (hit, from
// core's SearchHitBlock; 0 when none).
func srCompactBlocks(blocks []json.RawMessage, hit int) []json.RawMessage {
	if len(blocks) <= 3 {
		return blocks
	}
	a := 0
	if hit > 0 {
		a = hit
	}
	return blocks[a:min(a+3, len(blocks))]
}

// srTextFieldsLen is getTextFieldsLength: the lengths (UTF-16, as
// JavaScript counts) of every string under a "text" key, at any depth.
func srTextFieldsLen(blocks []json.RawMessage) int {
	n := 0
	for _, b := range blocks {
		var v any
		if json.Unmarshal(b, &v) == nil {
			n += srTextLen(v)
		}
	}
	return n
}

func srTextLen(v any) int {
	switch x := v.(type) {
	case []any:
		n := 0
		for _, e := range x {
			n += srTextLen(e)
		}
		return n
	case map[string]any:
		n := 0
		for k, e := range x {
			if s, ok := e.(string); ok && k == "text" {
				n += len(utf16.Encode([]rune(s)))
				continue
			}
			n += srTextLen(e)
		}
		return n
	}
	return 0
}

// srDenseBody lays out the density body of card c at (tx, y), bw wide,
// lines lh apart, over bg: it returns the height it takes, "... Show
// more" included.
func (v *SearchView) srDenseBody(gtx layout.Context, th *theme.Theme, cl *Colors, c *srCard, tx, y, bw, lh int, bg color.NRGBA) int {
	lines, clipped := v.srDenseWrap(gtx, th, c, bw)
	bodyH := lines * lh
	at(gtx, image.Pt(tx, y), func(gtx layout.Context) layout.Dimensions {
		srDrawToks(gtx, th, cl, c.dToks, lh, cl.Fg)
		return layout.Dimensions{}
	})
	if !clipped {
		return bodyH
	}
	// mask-image: linear-gradient(#000 60%, #0000) over the clamped box
	from := float32(y) + float32(bodyH)*0.6
	to := float32(y + bodyH)
	paint.LinearGradientOp{Stop1: f32.Pt(0, from), Stop2: f32.Pt(0, to),
		Color1: color.NRGBA{R: bg.R, G: bg.G, B: bg.B}, Color2: bg}.Add(gtx.Ops)
	st := clip.Rect(image.Rect(tx, int(from), tx+bw, y+bodyH)).Push(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	st.Pop()
	// renderExpandButton on the line under it
	at(gtx, image.Pt(tx+dp(gtx, 4), y+bodyH), func(gtx layout.Context) layout.Dimensions {
		return clickBox(gtx, &c.more, nil, func(gtx layout.Context) layout.Dimensions {
			fg := cl.Muted // --sk_foreground_max_solid
			if c.more.Hovered() {
				fg = cl.Link // --sk_highlight
			}
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, fg, srShowMoreLabel())
			})
		})
	})
	return bodyH + lh
}

// srShowMoreLabel is renderExpandButton's c-search__expand_ellipsis:
// "... " and search's "Show more".
func srShowMoreLabel() string { return "... " + nsSearch.T("Show more") }

// srDenseWrap wraps c's density body to bw: the lines drawn (six at most
// while collapsed) and whether it is clipped (the expand button shows).
func (v *SearchView) srDenseWrap(gtx layout.Context, th *theme.Theme, c *srCard, bw int) (int, bool) {
	if c.dW != bw || c.dExp != c.expanded {
		c.dW, c.dExp = bw, c.expanded
		if c.expanded {
			c.dToks, c.dLines, _ = srWrapFit(gtx, th, c.full, bw, 0, 0)
			c.dOver = false
		} else {
			c.dToks, c.dLines, c.dOver = srWrapFit(gtx, th, c.dense.segs, bw, srClampLines, 0)
		}
	}
	lines := maxInt(1, c.dLines)
	if c.expanded {
		return lines, false
	}
	return min(lines, srClampLines), c.dense.heur || c.dOver
}

// srDenseHeight is the density body's height before it is drawn.
func (v *SearchView) srDenseHeight(gtx layout.Context, th *theme.Theme, c *srCard, bw, lh int) int {
	lines, clipped := v.srDenseWrap(gtx, th, c, bw)
	h := lines * lh
	if clipped {
		h += lh
	}
	return h
}

// ---- the count pills ----

// srPill is one of renderReactionsAndReplies' count pills.
type srPill struct {
	kind  int // srPillReplies, srPillReactions, srPillAttachments
	icon  string
	count int
	click *widget.Clickable
}

const (
	srPillReplies = iota
	srPillReactions
	srPillAttachments
)

// srPills are m's pills in Slack's order: replies, reactions,
// attachments.
func srPills(c *srCard, m *core.Message) []srPill {
	var out []srPill
	if m.ReplyCount > 0 {
		out = append(out, srPill{srPillReplies, "comment", m.ReplyCount, &c.chip})
	}
	if n := srReactionTotal(m.Reactions); len(m.Reactions) > 0 {
		out = append(out, srPill{srPillReactions, "reaction", n, &c.reacts})
	}
	if n := c.attN; n > 0 {
		out = append(out, srPill{srPillAttachments, "attachment", n, &c.atts})
	}
	return out
}

func srReactionTotal(rs []core.Reaction) int {
	n := 0
	for _, r := range rs {
		n += r.Count
	}
	return n
}

// srPillW is a pill's border-box width.
func srPillW(gtx layout.Context, th *theme.Theme, n int) int {
	return 2*border1(gtx) + 2*dp(gtx, 8) + dp(gtx, 14) + dp(gtx, 4) + textW(gtx, th, 12, theme.Regular, strconv.Itoa(n))
}

// srPillsHeight is the card row's height (its margin-top 8 included) in
// w px, wrapped as flex-wrap does; 0 without pills.
func srPillsHeight(gtx layout.Context, th *theme.Theme, pills []srPill, w int) int {
	if len(pills) == 0 {
		return 0
	}
	rows, x := 1, 0
	gap := dp(gtx, 8)
	for i, p := range pills {
		pw := srPillW(gtx, th, p.count)
		if i > 0 && x+gap+pw > w {
			rows, x = rows+1, pw
			continue
		}
		if i > 0 {
			x += gap
		}
		x += pw
	}
	ph := dp(gtx, 24)
	return dp(gtx, 8) + rows*ph + (rows-1)*gap
}

// srTokens are the pills' theme colours.
type srPillColors struct{ border, borderHover, bg, fg color.NRGBA }

func srPillPalette(th *theme.Theme) srPillColors {
	dark := th != nil && th.Dark
	p := srPillColors{border: argb(0x215e5d60), borderHover: argb(0x735e5d60), bg: rgb(0xffffff), fg: rgb(0x5e5d60)}
	if dark {
		p = srPillColors{border: argb(0x4d797c81), borderHover: argb(0x80797c81), bg: rgb(0x1a1d21), fg: rgb(0x9a9b9e)}
	}
	if th != nil && th.Tokens != nil {
		theme.SetTok(th.Tokens, &p.border, theme.Both("var(--dt_color-otl-ter)"), dark)
		theme.SetTok(th.Tokens, &p.borderHover, theme.Both("var(--dt_color-otl-sec)"), dark)
		theme.SetTok(th.Tokens, &p.bg, theme.Both("var(--dt_color-base-pry)"), dark)
		theme.SetTok(th.Tokens, &p.fg, theme.Both("var(--dt_color-content-ter)"), dark)
	}
	return p
}

func argb(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: uint8(v >> 24)}
}

// srDrawPills lays the card row out at (x, y), w wide.
func (v *SearchView) srDrawPills(gtx layout.Context, th *theme.Theme, cl *Colors, c *srCard, m *core.Message, pills []srPill, x, y, w int) {
	if len(pills) == 0 {
		return
	}
	pc := srPillPalette(th)
	gap, ph := dp(gtx, 8), dp(gtx, 24)
	px, py := 0, y+dp(gtx, 8)
	for i := range pills {
		p := pills[i]
		pw := srPillW(gtx, th, p.count)
		if i > 0 && px+gap+pw > w {
			px, py = 0, py+ph+gap
		} else if i > 0 {
			px += gap
		}
		at(gtx, image.Pt(x+px, py), func(gtx layout.Context) layout.Dimensions {
			d := clickBox(gtx, p.click, func(gtx layout.Context, sz image.Point, hovered bool) {
				r := image.Rectangle{Max: sz}
				bc := pc.border
				if hovered {
					bc = pc.borderHover
				}
				rrect(gtx, r, sz.Y/2, bc)
				rrect(gtx, r.Inset(border1(gtx)), sz.Y/2-border1(gtx), pc.bg)
			}, func(gtx layout.Context) layout.Dimensions {
				ix := border1(gtx) + dp(gtx, 8)
				at(gtx, image.Pt(ix, (ph-dp(gtx, 14))/2), func(gtx layout.Context) layout.Dimensions {
					return icon(gtx, p.icon, 14, pc.fg)
				})
				at(gtx, image.Pt(ix+dp(gtx, 14)+dp(gtx, 4), 0), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, ph, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 12, theme.Regular, pc.fg, strconv.Itoa(p.count))
					})
				})
				return layout.Dimensions{Size: image.Pt(pw, ph)}
			})
			pointerCursor(gtx, d.Size)
			switch p.kind {
			case srPillReplies:
				v.srRepliesTip(gtx, th, c, m, d.Size)
			case srPillReactions:
				v.srReactionsTip(gtx, th, c, m, d.Size)
			case srPillAttachments:
				c.attR = image.Rectangle{Max: d.Size}.Add(image.Pt(x+px, py)) // card coords
			}
			return d
		})
		px += pw
	}
}

// ---- the replies pill's tooltip ----

// srViewerReply is useFetchLatestReplyTimestamp's answer for a card: the
// viewer's latest reply's preview (useReplyMessagePreview), asked once.
type srViewerReply struct {
	asked   bool
	preview string
}

// srAskViewerReply fetches the viewer's latest reply for card i when its
// result has requestor_replied, as SearchRepliesAction does once it's
// drawn.
func (v *SearchView) srAskViewerReply(i int) {
	c, m := &v.cards[i], v.msgs[i]
	if c.reply.asked || !m.RequestorReplied || m.ReplyCount == 0 {
		return
	}
	c.reply.asked = true
	app := v.h.Core()
	if app == nil {
		return
	}
	seq := v.seq
	thread := m.ThreadTS
	if thread == "" {
		thread = m.TS
	}
	run(v.h, func(ctx context.Context) (core.Message, error) {
		r, ok, err := app.SearchViewerReply(ctx, m.Channel, thread)
		if err == nil && !ok {
			r = core.Message{}
		}
		return r, err
	}, func(r core.Message, err error) {
		if err != nil || seq != v.seq || i >= len(v.cards) || r.TS == "" {
			return
		}
		v.cards[i].reply.preview = srReplyPreview(&r)
	})
}

// srReplyPreview is useReplyMessagePreview: the reply's plain text cut at
// 60 (s21a), else its first file's name in brackets.
func srReplyPreview(m *core.Message) string {
	if s := strings.TrimSpace(srBody(m)); s != "" {
		return srTruncate(s, 60)
	}
	for _, b := range m.Doc.Blocks {
		if b.File != nil && b.File.Name != "" {
			return "[" + b.File.Name + "]"
		}
	}
	return ""
}

// srTruncate is s21a: s cut to n UTF-16 units, at the last space when
// that is within 15 of the end, with "…".
func srTruncate(s string, n int) string {
	u := utf16.Encode([]rune(s))
	if len(u) <= n {
		return s
	}
	cut := string(utf16.Decode(u[:n]))
	if a := strings.LastIndex(cut, " "); a > 0 && len(utf16.Encode([]rune(cut[:a]))) > n-15 {
		return cut[:a] + "…"
	}
	return string(utf16.Decode(u[:n-1])) + "…"
}

// srLastReplyText is getThreadLatestReplyText with
// alwaysShowLastReplyLabel: "Last reply today at {time}", else "Last
// reply {time_ago}".
func srLastReplyText(t, now time.Time) string {
	if dtSameDay(t, now) {
		return nsThreads.T("Last reply today at {time}", "time", clock(t))
	}
	return nsThreads.T("Last reply {time_ago}", "time_ago", i18n.TimeAgo(i18n.Locale(), t, now, false, false))
}

// srRepliesLabel is the pill's countTip, search's "{count, plural, =1
// {# reply} other {# replies}}".
func srRepliesLabel(n int) string {
	return nsSearch.T("{count, plural, =1 {# reply} other {# replies}}", "count", n)
}

func (v *SearchView) srRepliesTip(gtx layout.Context, th *theme.Theme, c *srCard, m *core.Message, sz image.Point) {
	c.tipReplies.Delay = srTipDelay
	g := srRepliesLabel(m.ReplyCount)
	if m.LatestReply != "" {
		g = srLastReplyText(tsTime(m.LatestReply), time.Now())
	}
	if !m.RequestorReplied {
		Tooltip(gtx, th, &c.tipReplies, c.chip.Hovered(), sz, Above, g, "")
		return
	}
	// .c-search_message__replies_tip_content: g, then the personal line
	// (nowrap, ellipsis, max-width 180): "You replied", ': "', the quote
	// in content-inv-sec, '"'
	you := nsSearch.T("You replied")
	quote := c.reply.preview
	TooltipBox(gtx, th, &c.tipReplies, c.chip.Hovered(), sz, Above, func(gtx layout.Context) layout.Dimensions {
		fg, sub := th.C.TooltipText, th.C.TooltipSub
		px, py := dp(gtx, 12), dp(gtx, 8)
		lh := dp(gtx, 18)
		w1 := textW(gtx, th, 13, theme.Bold, g)
		at(gtx, image.Pt(px, py), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Bold, fg, g)
			})
		})
		maxW := dp(gtx, 180)
		y2 := py + lh + dp(gtx, 8)
		x := 0
		runs := []struct {
			s string
			c color.NRGBA
		}{{you, fg}}
		if quote != "" {
			runs = append(runs, struct {
				s string
				c color.NRGBA
			}{`: "`, fg}, struct {
				s string
				c color.NRGBA
			}{quote, sub}, struct {
				s string
				c color.NRGBA
			}{`"`, fg})
		}
		for _, r := range runs {
			if x >= maxW {
				break
			}
			rw := textW(gtx, th, 13, theme.Bold, r.s)
			at(gtx, image.Pt(px+x, y2), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(maxW-x, lh)}
				return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Bold, r.c, r.s)
				})
			})
			x += rw
		}
		w := max(w1, min(x, maxW))
		return layout.Dimensions{Size: image.Pt(w+2*px, y2+lh+py)}
	})
}

// ---- the reactions pill's tooltip ----

// srReactionGroup is one of condenseSkinToneReactions' folded reactions.
type srReactionGroup struct {
	name  string
	count int
}

// srSkinTone is W90V's skin tone suffix ("::skin-tone-2").
var srSkinTone = regexp.MustCompile(`::[^:]+-([2-6]-[2-6])|::[^:]+-([2-6])$`)

// srBaseEmoji is baseEmojiName: the name without its skin tone.
func srBaseEmoji(name string) string {
	n := strings.Trim(strings.ToLower(name), ":")
	s := srSkinTone.ReplaceAllString(n, "")
	if s == "" || strings.Contains(s, ":") {
		return name
	}
	return s
}

// srCondense is getReactionGroups over names: grouped by base emoji in
// first-seen order, the counts summed.
func srCondense(rs []core.Reaction) []srReactionGroup {
	var out []srReactionGroup
	at := map[string]int{}
	for _, r := range rs {
		b := srBaseEmoji(r.Name)
		if i, ok := at[b]; ok {
			out[i].count += r.Count
			continue
		}
		at[b] = len(out)
		out = append(out, srReactionGroup{b, r.Count})
	}
	return out
}

// srReactionsTipLayout places SearchReactionsTooltip's items: each one's
// x (the separator before it included) and width, how many show in the
// 176px row, and the "{n} more" label's text.
func srReactionsTipLayout(gtx layout.Context, th *theme.Theme, gs []srReactionGroup) (xs, ws []int, shown int, more string) {
	sep := textW(gtx, th, 13, theme.Bold, " · ")
	gap4, gap8, em := dp(gtx, 4), dp(gtx, 8), dp(gtx, 16)
	x := 0
	for i, g := range gs {
		if i > 0 {
			x += gap8
		}
		w := em + gap4 + textW(gtx, th, 13, theme.Bold, strconv.Itoa(g.count))
		if i > 0 {
			w += sep + gap4
		}
		xs, ws = append(xs, x), append(ws, w)
		x += w
	}
	// measure(): the items whose right edge fits the tip (the row's 176px
	// less the "more" label and its gap once it shows), until it settles
	maxW := dp(gtx, 176)
	hidden := 0
	for range 4 {
		avail := maxW
		if hidden > 0 {
			avail -= gap8 + textW(gtx, th, 13, theme.Bold, srReactionsMore(hidden))
		}
		n := 0
		for i := range xs {
			if xs[i]+ws[i] > avail+1 {
				break
			}
			n++
		}
		if len(gs)-n == hidden {
			break
		}
		hidden = len(gs) - n
	}
	more = ""
	if hidden > 0 {
		more = srReactionsMore(hidden)
	}
	return xs, ws, len(gs) - hidden, more
}

// srReactionsMore is search's "{count, plural, one {# more} other {# more}}".
func srReactionsMore(n int) string {
	return nsSearch.T("{count, plural, one {# more} other {# more}}", "count", n)
}

func (v *SearchView) srReactionsTip(gtx layout.Context, th *theme.Theme, c *srCard, m *core.Message, sz image.Point) {
	c.tipReacts.Delay = srTipDelay
	gs := srCondense(m.Reactions)
	mine := srCondense(srOwnReactions(m.Reactions)) // condenseSkinToneNames
	TooltipBox(gtx, th, &c.tipReacts, c.reacts.Hovered(), sz, Above, func(gtx layout.Context) layout.Dimensions {
		fg, sub := th.C.TooltipText, th.C.TooltipSub
		px, py := dp(gtx, 12), dp(gtx, 8)
		lh := dp(gtx, 18)
		gap4, gap8, em := dp(gtx, 4), dp(gtx, 8), dp(gtx, 16)
		xs, ws, shown, more := srReactionsTipLayout(gtx, th, gs)
		sep := " · "
		sepW := textW(gtx, th, 13, theme.Bold, sep)
		rowW := 0
		for i := 0; i < shown; i++ {
			g := gs[i]
			x := px + xs[i]
			if i > 0 {
				at(gtx, image.Pt(x, py), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 13, theme.Bold, fg, sep)
					})
				})
				x += sepW + gap4
			}
			at(gtx, image.Pt(x, py+(lh-em)/2), func(gtx layout.Context) layout.Dimensions {
				return emojiImage(gtx, v.h, g.name, 16)
			})
			at(gtx, image.Pt(x+em+gap4, py), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Bold, fg, strconv.Itoa(g.count))
				})
			})
			rowW = xs[i] + ws[i]
		}
		if more != "" {
			at(gtx, image.Pt(px+rowW+gap8, py), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Bold, sub, more)
				})
			})
			rowW += gap8 + textW(gtx, th, 13, theme.Bold, more)
		}
		w, h := rowW, py+lh
		if len(mine) > 0 {
			// .c-search_message__reactions_tip_personal: centred, gap 4,
			// content-inv-sec: "You reacted with", " ", the emoji
			label := nsSearch.T("You reacted with")
			lw := textW(gtx, th, 13, theme.Bold, label)
			pw := lw + gap4 + len(mine)*em + (len(mine)-1)*gap4
			w = max(w, pw)
			y := h + gap8
			x := px + (w-pw)/2
			at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Bold, sub, label)
				})
			})
			x += lw + gap4
			for _, g := range mine {
				at(gtx, image.Pt(x, y+(lh-em)/2), func(gtx layout.Context) layout.Dimensions {
					return emojiImage(gtx, v.h, g.name, 16)
				})
				x += em + gap4
			}
			h = y + lh
		}
		return layout.Dimensions{Size: image.Pt(w+2*px, h+py)}
	})
}

// srOwnReactions are the reactions the viewer added.
func srOwnReactions(rs []core.Reaction) []core.Reaction {
	var out []core.Reaction
	for _, r := range rs {
		if r.Self {
			out = append(out, r)
		}
	}
	return out
}

// ---- the attachments pill and its card ----

// srAttItem is one of getAttachmentPreviewItems' rows.
type srAttItem struct {
	title, link, icon, kind string
	file                    *richtext.File
}

// srAttachmentItems are getAttachmentPreviewItems for m: the counted
// attachments (getCountedAttachments: not hidden), then the counted
// files (getCountedFiles: not one an attachment carries as file_id).
func srAttachmentItems(m *core.Message) []srAttItem {
	var atts []map[string]any
	_ = json.Unmarshal(m.Attachments, &atts)
	var out []srAttItem
	carried := map[string]bool{}
	var files []*richtext.File
	for _, b := range m.Doc.Blocks {
		if b.File != nil {
			files = append(files, b.File)
		}
	}
	for _, a := range atts {
		if srBool(a["hidden"]) {
			continue
		}
		if id := srStr(a["file_id"]); id != "" {
			carried[id] = true
		}
		wo := srMap(a["work_object_entity"])
		if wo == nil {
			wo = srMap(a["work_object"])
		}
		forwarded := srBool(a["is_msg_unfurl"]) || srBool(a["is_share"]) && srBool(a["is_forwarded"])
		woTitle, woLink := srWorkObjectPreview(wo)
		var file *richtext.File
		if id := srStr(a["file_id"]); id != "" {
			for _, f := range files {
				if f.ID == id {
					file = f
				}
			}
		}
		it := srAttItem{file: file}
		if caption, ok := srTableAttachment(a); ok {
			it.kind, it.icon, it.title = "table", "table", caption
			if it.title == "" {
				it.title = nsSearch.T("Table")
			}
		} else {
			for _, s := range []string{woTitle, srStr(a["title"]), srStr(a["fallback"]), srStr(a["text"]), srStr(a["service_name"]), srStr(a["author_name"])} {
				if s != "" && !srSerializedBlocks(s) {
					it.title = s
					break
				}
			}
			it.link = woLink
			if it.link == "" {
				it.link = srStr(a["title_link"])
			}
			if it.link == "" {
				it.link = srStr(a["from_url"])
			}
			switch {
			case file != nil:
				it.kind, it.icon = "file", srFileIcon(file)
			case forwarded:
				it.kind, it.icon = "message", "message"
			default:
				it.kind, it.icon = "link", srAttachmentIcon(a, wo != nil)
			}
		}
		out = append(out, it)
	}
	for _, f := range files {
		if carried[f.ID] {
			continue
		}
		t := f.Title
		if t == "" {
			t = f.Name
		}
		out = append(out, srAttItem{title: t, link: f.Permalink, icon: srFileIcon(f), kind: "file", file: f})
	}
	return out
}

// srWorkObjectPreview is getWorkObjectPreview's title and link.
func srWorkObjectPreview(wo map[string]any) (string, string) {
	if wo == nil {
		return "", ""
	}
	title := ""
	if srStr(wo["work_object_type"]) == "external" {
		ls := srMap(wo["layouts"])
		for _, k := range []string{"compact", "minimal", "expanded", "full"} {
			if t := srStr(srMap(srMap(ls[k])["title"])["text"]); t != "" {
				title = t
				break
			}
		}
	}
	link := srStr(wo["external_url"])
	if link == "" {
		link = srStr(wo["link"])
	}
	return title, link
}

// srTableAttachment is getTableAttachmentPreview: blocks that are all
// table or data_table blocks; the caption of the first with one.
func srTableAttachment(a map[string]any) (string, bool) {
	bs, _ := a["blocks"].([]any)
	if len(bs) == 0 {
		return "", false
	}
	for _, b := range bs {
		t := srStr(srMap(b)["type"])
		if t != "table" && t != "data_table" {
			return "", false
		}
	}
	for _, b := range bs {
		if c := srStr(srMap(b)["caption"]); c != "" {
			return c, true
		}
	}
	return "", true
}

// srSerializedBlocks is isSerializedBlocks.
var srSerialRe = regexp.MustCompile(`"(?:type|block_id|elements)":`)

func srSerializedBlocks(s string) bool {
	t := strings.TrimSpace(s)
	return t != "" && (t[0] == '[' || t[0] == '{') && srSerialRe.MatchString(t)
}

// srAttachmentIcon is getAttachmentPreviewIcon for a link attachment.
func srAttachmentIcon(a map[string]any, workObject bool) string {
	switch {
	case a["list_record_id"] != nil || a["list_records"] != nil || a["list_record"] != nil:
		return "list-item"
	case workObject:
		return "document"
	case srStr(a["media_type"]) == "video" || srStr(a["video_html"]) != "" || srVideoType(srStr(a["filetype"])):
		return "video"
	case srStr(a["file_id"]) != "" || srStr(a["filetype"]) != "":
		return "file"
	case srStr(a["image_url"]) != "" || srStr(a["thumb_url"]) != "":
		return "image"
	}
	return "link"
}

func srVideoType(t string) bool { return t == "mp4" || t == "mov" || t == "webm" }

// srFileIcon is getFilePreviewIcon.
func srFileIcon(f *richtext.File) string {
	switch {
	case f.Mode == "quip" || f.Type == "quip":
		return "canvas-content"
	case f.Mode == "list" || f.Type == "list":
		return "lists"
	case f.IsVideo:
		return "video"
	case f.IsImage:
		return "image"
	}
	return "file"
}

func srStr(v any) string { s, _ := v.(string); return s }
func srBool(v any) bool  { b, _ := v.(bool); return b }
func srMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// srOpenAttachments opens SearchAttachmentPreviewList for card i at the
// pill (HoverCardTrigger position "top-left", openOnClick).
func (v *SearchView) srOpenAttachments(gtx layout.Context, i int) {
	c, m := &v.cards[i], v.msgs[i]
	items := srAttachmentItems(&m)
	y, ok := v.cardY(i)
	if len(items) == 0 || !ok {
		return
	}
	pv := &srAttCard{v: v, m: m, items: items, slugs: make([]widget.Clickable, len(items))}
	anchor := v.win(gtx, c.attR.Add(image.Pt(v.cardX, y)))
	p := NewPopover(v.h, pv.layout, PopoverOpts{Anchor: anchor, Popover: "top-left", Radius: 12})
	pv.pop = p
	v.h.ShowOverlay(p)
}

// srAttCard is the Attachments card's content.
type srAttCard struct {
	v     *SearchView
	m     core.Message
	items []srAttItem
	slugs []widget.Clickable
	close widget.Clickable
	pop   *Popover
}

// layout draws .container__y1znI (a column, gap 8, 300–400px, padding
// 12): the "Attachments" heading (12px bold, content-sec), the close
// IconButton (small) at top/right 8, and the list (gap 4) of slugs
// (content-hgl-1 on surf-hgl-1, radius 3, padding 1 2, gap 4, the 16px
// icon and the title on one line, ellipsized).
func (a *srAttCard) layout(gtx layout.Context) layout.Dimensions {
	th := a.v.h.Theme()
	cl := colorsOf(th)
	if a.close.Clicked(gtx) {
		a.pop.Dismiss()
	}
	for i := range a.slugs {
		if a.slugs[i].Clicked(gtx) {
			a.pop.Dismiss()
			a.open(a.items[i])
		}
	}
	dark := th != nil && th.Dark
	slugFg, slugBg := rgb(0x1264a3), argb(0x331d9bd1)
	if dark {
		slugFg, slugBg = rgb(0x2ba5ce), argb(0x40036696)
	}
	hovFg, hovBg := rgb(0x0b4c8c), rgba(0x1d9bd1, .2) // sk_highlight_hover, sk_highlight_accent .2
	if dark {
		hovFg = rgb(0x40b3e4)
	}
	if th != nil && th.Tokens != nil {
		theme.SetTok(th.Tokens, &slugFg, theme.Both("var(--dt_color-content-hgl-1)"), dark)
		theme.SetTok(th.Tokens, &slugBg, theme.Both("var(--dt_color-surf-hgl-1)"), dark)
	}
	pad, gap := dp(gtx, 12), dp(gtx, 8)
	minW, maxW := dp(gtx, 300), dp(gtx, 400)
	head := nsSearch.T("Attachments")
	headH := dp(gtx, 16)
	lh := dp(gtx, 22)
	slugH := lh + 2*dp(gtx, 1)
	inner := maxW - 2*pad
	// fit-content: the widest slug, the heading and its 40px padding
	w := textW(gtx, th, 12, theme.Bold, head) + dp(gtx, 40)
	for _, it := range a.items {
		w = max(w, 2*dp(gtx, 2)+dp(gtx, 16)+dp(gtx, 4)+textW(gtx, th, 15, theme.Regular, srAttLabel(it)))
	}
	w = min(max(w+2*pad, minW), maxW)
	inner = w - 2*pad
	at(gtx, image.Pt(pad, pad), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, headH, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 12, theme.Bold, cl.Fg2, head)
		})
	})
	at(gtx, image.Pt(w-dp(gtx, 8)-dp(gtx, 28), dp(gtx, 8)), func(gtx layout.Context) layout.Dimensions {
		col := cl.Fg2
		if a.close.Hovered() {
			col = cl.Fg
		}
		return iconButton(gtx, &a.close, "close", 28, 16, col, cl.BgHover, 8)
	})
	y := pad + headH + gap
	for i, it := range a.items {
		if i > 0 {
			y += dp(gtx, 4)
		}
		label := srAttLabel(it)
		sw := min(inner, 2*dp(gtx, 2)+dp(gtx, 16)+dp(gtx, 4)+textW(gtx, th, 15, theme.Regular, label))
		clickable := it.link != "" || it.file != nil
		at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			draw := func(gtx layout.Context) layout.Dimensions {
				fg, bg := slugFg, slugBg
				if clickable && a.slugs[i].Hovered() {
					fg, bg = hovFg, hovBg
				}
				rrect(gtx, image.Rect(0, 0, sw, slugH), dp(gtx, 3), bg)
				x := dp(gtx, 2)
				at(gtx, image.Pt(x, (slugH-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
					return icon(gtx, it.icon, 16, fg)
				})
				x += dp(gtx, 16) + dp(gtx, 4)
				at(gtx, image.Pt(x, dp(gtx, 1)), func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints = layout.Constraints{Max: image.Pt(max(1, sw-x-dp(gtx, 2)), lh)}
					return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 15, theme.Regular, fg, label)
					})
				})
				return layout.Dimensions{Size: image.Pt(sw, slugH)}
			}
			if !clickable {
				return draw(gtx)
			}
			d := a.slugs[i].Layout(gtx, draw)
			pointerCursor(gtx, d.Size)
			return d
		})
		y += slugH
	}
	return layout.Dimensions{Size: image.Pt(w, y+pad)}
}

// srAttLabel is AttachmentPreviewPill's text: the title, else the link.
func srAttLabel(it srAttItem) string {
	if it.title != "" {
		return it.title
	}
	return it.link
}

// open is onAttachmentPreviewClick: a file opens as the card's files do
// (onFileClick); a forwarded message's permalink opens the message;
// a link opens in the browser (the slug's href, target _blank).
func (a *srAttCard) open(it srAttItem) {
	h := a.v.h
	if it.kind == "file" && it.file != nil {
		f := it.file
		cf := core.File{ID: f.ID, Name: f.Name, Title: f.Title, Mime: f.Mime, URL: f.URL, Permalink: f.Permalink,
			Mode: f.Mode, IsCanvas: f.Mode == "quip" || f.Type == "quip", IsList: f.Mode == "list" || f.Type == "list", IsImage: f.IsImage}
		if f.Thumb != nil {
			cf.ThumbW, cf.ThumbH = f.Thumb.W, f.Thumb.H
		}
		srOpenFile(h, cf)
		return
	}
	if it.kind == "message" {
		link := it.link
		if link == "" {
			link = a.m.Permalink
		}
		if pl, ok := core.ParsePermalink(link); ok && pl.Kind == "message" {
			if lf, ok := h.(LinkFollower); ok {
				// Slack's Link: openPermalink's jumpToMessage (a reply opens in
				// its thread); one it can't place (another workspace's) opens
				// in a new tab, and only a conversation the client opens is a
				// recent one
				if lf.FollowLink(link, false) {
					RecordRecentConversation(pl.ChannelID)
				} else {
					openBrowser(link)
				}
				return
			}
			RecordRecentConversation(pl.ChannelID)
			if pl.ThreadTS != "" {
				h.Navigate("c/" + pl.ChannelID + "/t/" + pl.ThreadTS)
			} else {
				h.Navigate("c/" + pl.ChannelID + "/m/" + pl.TS)
			}
			return
		}
	}
	if it.link != "" {
		if lf, ok := h.(LinkFollower); ok && lf.FollowLink(it.link, false) {
			return
		}
		openBrowser(it.link)
	}
}

// srAttachmentCount is renderAttachmentAction's count.
func srAttachmentCount(m *core.Message) int { return len(srAttachmentItems(m)) }
