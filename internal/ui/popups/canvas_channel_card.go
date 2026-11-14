package popups

import (
	"context"
	"image"
	"image/color"
	"net/url"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// A canvas's channel mention card: Slack's canvas editor wraps a
// channel mention (quip-collab-editor's ChannelMentionView) in
// CardViewHovercardTrigger, whose content is the editor's HOVERCARD
// embed; the client fills it with UnfurlHovercard → ChannelHovercard
// (boot-shared UyZX, build 3889f0c93d30, read from the public CDN without
// a cookie), not the message ChannelHoverCard:
//
//   - the trigger: HovercardImpl (quip-collab-webapp) shows the card 250ms
//     after the pointer enters the mention (hovercard.CanvasShowDelay),
//     drops a pending open when it leaves, and closes the card 250ms
//     after the pointer leaves the mention and the card
//     (dismissDelayMs 250). A channel it can't name gets a tooltip, no
//     card. The bubble (bubbleProps hideBoxShadow, hideArrow, arrow
//     directions up and down, alignment start) sits under or over the
//     mention, its left edge on the mention's, shifted into the window
//     when neither fits; .parts-bubble.hide-box-shadow>.body is radius 8
//     with box-shadow 0 0 0 1px otl-ter, 0 4px 12px #00000014.
//   - HovercardContainer (jNWP → VWYn): .p-embedded_hovercard_container
//     322px, padding 16 (--capped in a hovercard: max-height clamp(200px,
//     50vh - 40px, 512px), scrolls); .p-embedded_hovercard__top is a row
//     (space-between, align start) of the header and, unless hideMenu, the
//     menu trigger (YdSa MenuTriggerButton: a 28×28 icon button,
//     ellipsis-vertical-filled 16, translate(6px, -6px), "More actions"
//     tooltip at the top after 60ms).
//   - the header: MediumChannelEntity (macg) with showUnreadBadge,
//     showPurpose false and showTeam (.channel_entity__UtQDZ: flex 1,
//     min-width 0): BaseEntity medium (min-height 36, a column): the name
//     row (.c-medium_channel_entity__name: 15px, the InlineChannelIcon —
//     archive / archive-filled, lock-filled, channel — then the name in
//     15px bold, margin-right 8) with the mention count's red badge
//     (margin-left 8, 3px down), an empty subtext, then the metadata
//     (margin-top 4, one line with an ellipsis): BrowsePageChannelSubtext —
//     "✓ Joined" (check-small, lilypad green, "Joined" bold) for a member,
//     "{n} member(s)" once the count is known (counts.member_count), and
//     the purpose after "  ·  " (mrkdwn flattened to one line).
//   - the menu (HHTx useGetEmbeddedMenuTemplate, the Menu's default
//     width) at "right-bottom" of the trigger: for a channel
//     object in the hovercard context, "View channel details" ("View
//     conversation details" for a DM or group DM: openChannelDetails) and
//     "Copy name" (copyChannelNameById: the name with its #), both
//     disabled for an unknown or non-existent channel; then, after a
//     separator, Report to Slack (getReportSectionMenuTemplate: with
//     isReportToSlackEnabled, the section's anchor link — the canvas's
//     permalink with focus_section_id — on the team's
//     /help/requests/report-content?u=…&type=canvas_section), and after
//     another, Remove mention (getRemoveSectionMenuTemplate, when the
//     control isRemovable: the section saved without the mention,
//     canvasview/mention.go). The rows Quip writes through its binary
//     edit-document — Add a comment / View comments and Add a reaction /
//     View reactions (a highlight control, the comment thread's anchor)
//     and Display as (the control's unfurl type) — aren't offered:
//     canvases.edit has no form for them (docs/CANVASES.md "Comments").
type CanvasChannelCard struct {
	h      Host
	anchor image.Rectangle
	id     string
	// file and section are the canvas and the section the mention is in
	// ("" when the trigger didn't say)
	file, section string

	rect image.Rectangle
	done bool

	resolving, missing bool
	countAsked         bool

	more    widget.Clickable
	moreTip TipState
	moreR   image.Rectangle // the trigger, window coordinates
	sub     Overlay
}

// NewCanvasChannelCard is the card for the channel id over anchor (the
// mention's bounds, window coordinates).
func NewCanvasChannelCard(h Host, anchor image.Rectangle, id string) *CanvasChannelCard {
	return NewCanvasMentionCard(h, anchor, id, "", "")
}

// NewCanvasMentionCard is NewCanvasChannelCard for the mention in canvas
// file's section.
func NewCanvasMentionCard(h Host, anchor image.Rectangle, id, file, section string) *CanvasChannelCard {
	return &CanvasChannelCard{h: h, anchor: anchor, id: id, file: file, section: section, moreTip: TipState{Delay: 60 * time.Millisecond}}
}

// CanvasMentions is optionally implemented by the Host: the canvas a
// mention is in, for the card's section rows.
type CanvasMentions interface {
	// CanvasPermalink is canvas file's permalink ("" when unknown).
	CanvasPermalink(file string) string
	// CanvasMentionRemovable and RemoveCanvasMention are Remove mention.
	CanvasMentionRemovable(file, section, channel string) bool
	RemoveCanvasMention(file, section, channel string)
}

// SectionLink is a canvas section's anchor link: the permalink with
// focus_section_id appended (URL.searchParams.append, so the id is
// percent-encoded), as Quip's getUrl builds it.
func SectionLink(permalink, section string) string {
	sep := "?"
	if strings.Contains(permalink, "?") {
		sep = "&"
	}
	return permalink + sep + "focus_section_id=" + url.QueryEscape(section)
}

// Rect is the drawn card (window coordinates).
func (p *CanvasChannelCard) Rect() image.Rectangle { return p.rect }

// Dismissed reports whether an action on it closed it.
func (p *CanvasChannelCard) Dismissed() bool { return p.done }

// Holding reports whether its menu is open (the card stays under it).
func (p *CanvasChannelCard) Holding() bool { return p.sub != nil && !p.sub.Dismissed() }

// MoreRect is the menu trigger (tests).
func (p *CanvasChannelCard) MoreRect() image.Rectangle { return p.moreR }

// CanvasCardState is what the card shows (tests).
type CanvasCardState struct {
	Name     string
	Icon     string
	Metadata []string // the BrowsePageChannelSubtext parts
	Mentions int
}

// State is the card's content (tests).
func (p *CanvasChannelCard) State() CanvasCardState {
	cv, ok := p.conv()
	if !ok {
		return CanvasCardState{}
	}
	return CanvasCardState{Name: cv.Name, Icon: canvasChannelIcon(cv), Metadata: p.metadata(cv), Mentions: p.mentions(cv)}
}

func (p *CanvasChannelCard) conv() (core.Conv, bool) {
	c := p.h.Core()
	if c == nil {
		return core.Conv{}, false
	}
	return c.Conv(p.id)
}

// canvasChannelIcon is InlineChannelIcon's getChannelIconType with filled
// for a private channel: archive (archive-filled when private),
// lock-filled, else channel.
func canvasChannelIcon(cv core.Conv) string {
	private := cv.Kind == "private"
	switch {
	case cv.IsArchived && private:
		return "archive-filled"
	case cv.IsArchived:
		return "archive"
	case private:
		return "lock-filled"
	}
	return "channel"
}

func (p *CanvasChannelCard) mentions(cv core.Conv) int {
	c := p.h.Core()
	if c == nil || cv.IsArchived {
		return 0
	}
	return c.Count(cv.ID).Mentions
}

// metadata is BrowsePageChannelSubtext's parts in order.
func (p *CanvasChannelCard) metadata(cv core.Conv) []string {
	var out []string
	if cv.Member {
		out = append(out, nsSearch.T("Joined"))
	}
	if c := p.h.Core(); c != nil {
		if n, ok := c.ChannelPeopleCount(cv.ID); ok && n >= 0 {
			out = append(out, nsSearch.A("{count, plural, =1 {# member} other {# members}}", i18n.Args{"count": n}))
		}
	}
	if s := canvasPurposeLine(cv.Purpose); s != "" {
		out = append(out, s)
	}
	return out
}

// canvasPurposeLine is the purpose as its one-line mrkdwn (formatting
// dropped, line breaks as spaces: customContentHandler).
func canvasPurposeLine(s string) string {
	s = strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " ")), " ")
	return s
}

// load fetches what's missing: the channel (ResolveConv), the member
// count (getMembershipCountsByChannelId's refetch).
func (p *CanvasChannelCard) load(cv core.Conv, ok bool) {
	c := p.h.Core()
	if c == nil {
		return
	}
	if !ok {
		if p.resolving || p.missing {
			return
		}
		p.resolving = true
		id := p.id
		run(p.h, func(ctx context.Context) (bool, error) {
			_, found := c.ResolveConv(ctx, id)
			return found, nil
		}, func(found bool, _ error) {
			p.resolving, p.missing = false, !found
		})
		return
	}
	if p.countAsked {
		return
	}
	p.countAsked = true
	if _, known := c.ChannelPeopleCount(p.id); !known {
		id := p.id
		run(p.h, func(ctx context.Context) (struct{}, error) {
			return struct{}{}, c.LoadPeopleCount(ctx, id)
		}, func(struct{}, error) {})
	}
}

// MenuItems is the ⋯ menu (EmbeddedMenu's template for a channel in a
// hovercard).
func (p *CanvasChannelCard) MenuItems() []MenuItem {
	cv, ok := p.conv()
	unknown := !ok || p.missing
	label := "View channel details"
	if cv.Kind == "im" || cv.Kind == "mpim" {
		label = "View conversation details"
	}
	id := p.id
	name := cv.Name
	if cv.Kind != "im" && cv.Kind != "mpim" {
		name = "#" + name // publicChannelIncludeHashSign, privateChannelUseHashSign
	}
	items := []MenuItem{
		MenuItem{Key: "view_channel_details", Label: label, Disabled: unknown, Do: func() {
			p.done = true
			if OpenChannelDetails != nil {
				OpenChannelDetails(id)
			}
		}}.In(nsMenu),
		MenuItem{Key: "copy_channel_name", Label: "Copy name", Disabled: unknown, DoCtx: func(gtx layout.Context) {
			copyText(gtx, name)
		}}.In(nsMenu),
	}
	cm, _ := p.h.(CanvasMentions)
	if cm == nil || p.file == "" || p.section == "" {
		return items
	}
	file, section := p.file, p.section
	if link := cm.CanvasPermalink(file); link != "" && actReporting(p.h) {
		items = append(items, MenuItem{Separator: true},
			MenuItem{Key: "remove", Label: "Report to Slack", Do: func() { // getReportSectionMenuTemplate's key and data-qa
				p.done = true
				reportSection(p.h, SectionLink(link, section))
			}}.In(nsFiles))
	}
	if cm.CanvasMentionRemovable(file, section, id) {
		items = append(items, MenuItem{Separator: true},
			MenuItem{Key: "remove", Label: "Remove mention", Do: func() {
				p.done = true
				cm.RemoveCanvasMention(file, section, id)
			}}.In(nsFiles))
	}
	return items
}

// Layout draws the card in the window (gtx at the window origin).
func (p *CanvasChannelCard) Layout(gtx layout.Context) layout.Dimensions {
	drain()
	th := p.h.Theme()
	cl := colorsOf(th)
	win := gtx.Constraints.Max
	cv, ok := p.conv()
	p.load(cv, ok)
	if p.more.Clicked(gtx) && !p.Holding() {
		m := NewMenuOpts(p.h, p.MenuItems(), MenuOpts{Anchor: p.moreR, Popover: "right-bottom"})
		p.sub = m
		p.h.ShowOverlay(m)
	}
	w := dp(gtx, 322)
	call, d := record(loose(gtx, image.Pt(w, win.Y)), func(gtx layout.Context) layout.Dimensions {
		return p.content(gtx, th, cl, cv, ok, w)
	})
	capH := max(dp(gtx, 200), min(win.Y/2-dp(gtx, 40), dp(gtx, 512))) // --capped
	sz := image.Pt(w, min(d.Size.Y, capH))
	p.rect = image.Rectangle{Min: canvasBubblePlace(p.anchor, sz, win), Max: image.Point{}}
	p.rect.Max = p.rect.Min.Add(sz)
	rad := dp(gtx, 8)
	ring := tokOtlTer.of(th)
	softShadow(gtx, p.rect, rad, dp(gtx, 4), dp(gtx, 12), color.NRGBA{A: 0x14})
	cardRing(gtx, p.rect, rad, theme.Over(ring, cl.Bg), cl.Bg)
	rrect(gtx, p.rect, rad, cl.Bg)
	cs := clipRRect(gtx, p.rect, rad)
	macroAt(gtx, call, p.rect.Min)
	cs.Pop()
	if !p.moreR.Empty() {
		// MenuTriggerButton's tooltip, "More actions" above the button
		at(gtx, p.moreR.Min, func(gtx layout.Context) layout.Dimensions {
			Tooltip(gtx, th, &p.moreTip, p.more.Hovered(), p.moreR.Size(), Above, nsEmbedUnfurl.T("More actions"), "")
			return layout.Dimensions{}
		})
	}
	return layout.Dimensions{Size: win}
}

var nsEmbedUnfurl = i18n.NS("embedded_unfurl_in_hovercard")

// canvasBubblePlace is the parts bubble's placement with arrows up or
// down and start alignment: right under the mention (the hidden arrow's
// overlap is 0) when it fits, else over it, else shifted into the window
// (shiftIntoViewportIfNoDirectionFits); the left edge is the mention's,
// kept inside the window.
func canvasBubblePlace(anchor image.Rectangle, sz, win image.Point) image.Point {
	x := anchor.Min.X
	if x+sz.X > win.X {
		x = max(0, win.X-sz.X)
	}
	if x < 0 {
		x = 0
	}
	if y := anchor.Max.Y; y+sz.Y <= win.Y {
		return image.Pt(x, y)
	}
	if y := anchor.Min.Y - sz.Y; y >= 0 {
		return image.Pt(x, y)
	}
	return image.Pt(x, max(0, min(anchor.Max.Y, win.Y-sz.Y)))
}

// content lays out the container w wide at the origin.
func (p *CanvasChannelCard) content(gtx layout.Context, th *theme.Theme, cl *Colors, cv core.Conv, ok bool, w int) layout.Dimensions {
	pad := dp(gtx, 16)
	x, y := pad, pad
	cw := w - 2*pad
	// the menu trigger: 28×28 at the row's right, translate(6px, -6px)
	bs := dp(gtx, 28)
	br := image.Rect(x+cw-bs+dp(gtx, 6), y-dp(gtx, 6), x+cw+dp(gtx, 6), y-dp(gtx, 6)+bs)
	ew := max(1, cw-bs)
	h := p.entity(gtx, th, cl, cv, ok, x, y, ew)
	at(gtx, br.Min, func(gtx layout.Context) layout.Dimensions {
		return p.more.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			if p.more.Hovered() || p.Holding() {
				rrect(gtx, image.Rectangle{Max: br.Size()}, dp(gtx, 6), cl.BgHover)
			}
			g := dp(gtx, 16)
			at(gtx, image.Pt((bs-g)/2, (bs-g)/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, icons.Slack("ellipsis-vertical-filled"), 16, cl.Fg2)
			})
			return layout.Dimensions{Size: br.Size()}
		})
	})
	p.moreR = br.Add(p.rect.Min)
	return layout.Dimensions{Size: image.Pt(w, y+max(h, bs-dp(gtx, 6))+pad)}
}

// entity draws MediumChannelEntity at (x, y), w wide, and returns its
// height (min-height 36).
func (p *CanvasChannelCard) entity(gtx layout.Context, th *theme.Theme, cl *Colors, cv core.Conv, ok bool, x, y, w int) int {
	lineH := dp(gtx, 22) // 15px × 1.46668
	if !ok {
		// unknown: MissingChannel (type unknown, medium, the hash)
		at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, lineH)}
			return vcenter(gtx, lineH, func(gtx layout.Context) layout.Dimensions {
				return th.Layout(gtx, theme.TextStyle{Size: 15, Weight: theme.Bold, Color: cl.Fg2, LineHeight: 22, MaxLines: 1}, "#")
			})
		})
		return dp(gtx, 36)
	}
	right := x + w
	if n := p.mentions(cv); n > 0 {
		at(gtx, image.Pt(0, y+dp(gtx, 3)), func(gtx layout.Context) layout.Dimensions {
			right = drawMenuBadge(gtx, th, n, "red", false, right, dp(gtx, 18)) - dp(gtx, 8)
			return layout.Dimensions{}
		})
	}
	nameW := max(1, right-x)
	at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(nameW, lineH)}
		g := dp(gtx, 15) // the icon font's 1em
		at(gtx, image.Pt(0, (lineH-g)/2), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, icons.Slack(canvasChannelIcon(cv)), 15, cl.Fg)
		})
		ix := g + dp(gtx, 4) // c-inline_channel_icon--space
		return at(gtx, image.Pt(ix, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(max(1, nameW-ix-dp(gtx, 8)), lineH)}
			return vcenter(gtx, lineH, func(gtx layout.Context) layout.Dimensions {
				return th.Layout(gtx, theme.TextStyle{Size: 15, Weight: theme.Bold, Color: cl.Fg, LineHeight: 22, MaxLines: 1}, cv.Name)
			})
		})
	})
	h := lineH
	parts := p.metadata(cv)
	if len(parts) > 0 {
		h += dp(gtx, 4) // subtext ~ metadata
		at(gtx, image.Pt(x, y+h), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, lineH)}
			return p.metadataLine(gtx, th, cl, cv, parts, w, lineH)
		})
		h += lineH
	}
	return max(h, dp(gtx, 36))
}

// metadataLine draws BrowsePageChannelSubtext's parts on one line with
// "  ·  " between them and an ellipsis at w.
func (p *CanvasChannelCard) metadataLine(gtx layout.Context, th *theme.Theme, cl *Colors, cv core.Conv, parts []string, w, lineH int) layout.Dimensions {
	sep := "  ·  "
	x := 0
	style := func(bold bool, col color.NRGBA) theme.TextStyle {
		wt := theme.Regular
		if bold {
			wt = theme.Bold
		}
		return theme.TextStyle{Size: 15, Weight: wt, Color: col, LineHeight: 22, MaxLines: 1}
	}
	put := func(s string, ts theme.TextStyle) {
		if x >= w {
			return
		}
		at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(max(1, w-x), lineH)}
			d := vcenter(gtx, lineH, func(gtx layout.Context) layout.Dimensions { return th.Layout(gtx, ts, s) })
			x += d.Size.X
			return d
		})
	}
	for i, s := range parts {
		if i > 0 {
			put(sep, style(false, cl.Fg))
		}
		if i == 0 && cv.Member {
			// c-medium_channel_entity__joined_status sk_lilypad_green: the
			// check-small glyph, a space, <strong>Joined</strong>
			green := tokLilypad.of(th)
			g := dp(gtx, 15)
			at(gtx, image.Pt(x, (lineH-g)/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, icons.Slack("check-small"), 15, green)
			})
			x += g + textW(gtx, th, 15, theme.Regular, " ")
			put(s, style(true, green))
			continue
		}
		put(s, style(false, cl.Fg))
	}
	return layout.Dimensions{Size: image.Pt(w, lineH)}
}

// tokLilypad is .sk_lilypad_green: color #007a5a in both modes.
var tokLilypad = chipTok{"", rgb(0x007a5a), rgb(0x007a5a)}
