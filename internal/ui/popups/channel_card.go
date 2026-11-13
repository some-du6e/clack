package popups

import (
	"context"
	"image"
	"image/color"
	"strings"
	"sync"
	"time"

	"gioui.org/gesture"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/hovercard"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Slack's ChannelHoverCard (gantry-v2-async-client-boot-shared module
// 7+JF, build 3889f0c93d30, read from the public CDN without a cookie).
// BaseMrkdwnChannel (lH2I) wraps every #channel in a message, the
// unknown and the non-existent ones included (but not an LLM's citation
// or a canvas's or list's file channel), in HoverCardTrigger (0oEe:
// position "top", offsetY -12, 350ms open, 250ms close) whose card is
// renderChannelHoverCard → ChannelHoverCard({channelId, enableButtons:
// true}); a channel-tab slug (NY16) and a canvas's channel mention do
// the same. Texty's TextyTooltip opens it over a composer's channel slug
// without buttons. The card:
//
//   - HoverCard (Q7nt): div.elevation_3.sk_primary_background.rounded-lg
//     340 wide (radius 8, box-shadow 0 0 0 1px #1d1c1d21, 0 4px 12px
//     #00000014).
//   - an unknown channel (still being fetched): "Loading…" (padding 16).
//   - not isChannelViewable (HY4m: non-existent, or private and the
//     viewer isn't in it): "You don’t have access to this channel (ex.
//     it’s private or in a workspace you’re not a part of), so its name
//     isn’t shown" (padding 16).
//   - a code channel (agent_channel record) under session_channels:
//     CodeChannelEntityCard instead (code_card.go).
//   - else, padding 16: the header (margin-bottom 12) is a row
//     (margin-bottom 4, centred) of the name, flex 1 in text_subtitle
//     (18px/1.25, weight 900) with ellipsis, InlineChannelEntity with
//     boldIcon (getChannelIconType: archive / archive-filled, lock-filled,
//     channel-filled; a 20px inline box, 2+2px before the name), then a
//     group (gap 4px between what it holds) of the huddle badge
//     (ChannelHoverCardHuddleBadge: huddleui's Badge, ChannelHuddleBadge)
//     and the mention count (Badge style themed: theme-base-imp /
//     theme-content-imp); under it the topic (topic.value, not the
//     purpose) as mrkdwn (i0IN with noHighlights, noJumbomoji,
//     showTooltips false: emoji, mentions and links) in two lines at most
//     (multiline_ellipsis_2 break_word). An archived channel stops there;
//     any other gets a column (gap 4) of ChannelHoverCardBodyItems
//     (display flex: the 20px icon nudged 2px down, the text 8px after
//     it): user-groups "{n, plural, =1 {# person} other {# people}} in
//     this channel", clock "Last message today | yesterday | on {MMM Do}
//     | in {YYYY}" or "No messages", and for a Slack Connect channel the
//     first team's 20px icon with "{teamName} is in this channel",
//     "{teamName1} and {teamName2} are in this channel" or "{teamName}
//     and <t1>{n} other team(s)</t1> are in this channel" (bold team
//     names: TeamName; the t1 run is strong with a tooltip listing the
//     others). Until a count or the last message is known the item shows
//     a large MissingText (160×8).
//   - enableButtons: margin-top 12, one full-width small outline button:
//     "View Channel" for an archived channel or one the viewer is in
//     (and not joining), which goes to the channel and closes the card;
//     else "Join Channel" (LoadingButton: its spinner while joinChannel
//     runs; the joining set is shared by every card of the channel).

// ChannelJoinHook, set by the ui root, joins a channel the way Slack's
// joinChannel thunk does (conversations.join); nil uses core directly.
var ChannelJoinHook func(ch string, done func(error))

// ChannelHuddleBadge, set by the ui root, draws ChannelHoverCardHuddleBadge
// for ch ending at right in a row rowH tall (huddleui's CardBadge) and
// returns its left edge, right when no huddle is on there; nil draws none.
var ChannelHuddleBadge func(gtx layout.Context, th *theme.Theme, ch string, right, rowH int) int

var (
	nsTeam = i18n.NS("team")

	chJoinMu sync.Mutex
	chJoin   = map[string]bool{} // channels being joined (Slack's module-level Set S)
)

// ChannelHoverCard is the card over a #channel (hovercard Request.ChannelID).
type ChannelHoverCard struct {
	h       Host
	anchor  image.Rectangle
	id      string
	buttons bool
	texty   bool

	rect image.Rectangle
	done bool

	resolving, missing bool
	latestAsked        bool // the count, last message and teams fetches started
	latestOK           bool
	latest             string

	btn     widget.Clickable
	others  widget.Clickable // the "N other teams" run (its tooltip)
	otherTp TipState

	topic    *richview.View // the topic as mrkdwn
	topicKey string
	huddle   bool // the name row drew ChannelHoverCardHuddleBadge

	// the code channel card (code_card.go)
	codeAsked bool
	codeBtn   widget.Clickable
	faceHover gesture.Hover
	faceTip   TipState
	pile      image.Rectangle // the facepile, card coordinates
}

// NewChannelHoverCard is the card for the channel id over anchor (window
// coordinates). buttons is enableButtons; texty is TextyTooltip's Popover
// (allowance 0, no opposite position).
func NewChannelHoverCard(h Host, anchor image.Rectangle, id string, buttons, texty bool) *ChannelHoverCard {
	return &ChannelHoverCard{h: h, anchor: anchor, id: id, buttons: buttons, texty: texty, otherTp: TipState{Delay: 150 * time.Millisecond},
		faceTip: TipState{Delay: 150 * time.Millisecond}}
}

// Rect is the drawn card (window coordinates).
func (p *ChannelHoverCard) Rect() image.Rectangle { return p.rect }

// Dismissed reports whether an action on it closed it (View Channel).
func (p *ChannelHoverCard) Dismissed() bool { return p.done }

// Holding: the card has no menu of its own.
func (p *ChannelHoverCard) Holding() bool { return false }

// ChannelCardState is what the card shows (tests).
type ChannelCardState int

const (
	ChannelCardLoading  ChannelCardState = iota // "Loading…"
	ChannelCardNoAccess                         // the no-access text
	ChannelCardFull                             // name, topic, items, buttons
	ChannelCardCode                             // CodeChannelEntityCard (code_card.go)
)

// State reports which of Slack's cards is drawn.
func (p *ChannelHoverCard) State() ChannelCardState {
	cv, ok := p.conv()
	switch {
	case !ok && !p.missing:
		return ChannelCardLoading
	case !ok || !channelViewable(cv):
		return ChannelCardNoAccess
	case p.codeChannelCard(cv):
		return ChannelCardCode
	}
	return ChannelCardFull
}

// Button is the action the card offers ("" without buttons).
func (p *ChannelHoverCard) Button() string {
	cv, ok := p.conv()
	if !p.buttons || !ok || !channelViewable(cv) || p.codeChannelCard(cv) {
		return ""
	}
	if channelCardView(cv) {
		return nsTeam.T("View Channel")
	}
	return nsTeam.T("Join Channel")
}

// Items are the body items' texts (tests): member count, last message,
// external teams; "" for one still loading.
func (p *ChannelHoverCard) Items() []string {
	cv, ok := p.conv()
	if !ok || cv.IsArchived || p.codeChannelCard(cv) {
		return nil
	}
	c := p.h.Core()
	out := []string{"", ""}
	if n, ok := c.ChannelPeopleCount(p.id); ok {
		out[0] = memberCountText(n)
	}
	if p.latestOK {
		out[1] = core.LastMessageText(p.latest, time.Now(), nsTeam.T)
	}
	if len(cv.ConnectedTeams) > 0 {
		out = append(out, i18n.Plain(p.teamsParts(cv.ConnectedTeams)))
	}
	return out
}

func (p *ChannelHoverCard) conv() (core.Conv, bool) {
	c := p.h.Core()
	if c == nil {
		return core.Conv{}, false
	}
	return c.Conv(p.id)
}

// HuddleBadge reports whether the name row shows the huddle badge
// (tests).
func (p *ChannelHoverCard) HuddleBadge() bool { return p.huddle }

// channelViewable is isChannelViewable: public, or one the viewer is in
// (Clack keeps no record channels).
func channelViewable(cv core.Conv) bool { return cv.Kind == "channel" || cv.Member }

// channelCardView is ChannelHoverCardActions' View Channel test: archived,
// or the viewer is in it and isn't joining.
func channelCardView(cv core.Conv) bool {
	if cv.IsArchived {
		return true
	}
	chJoinMu.Lock()
	joining := chJoin[cv.ID]
	chJoinMu.Unlock()
	return cv.Member && !joining
}

func memberCountText(n int) string {
	return nsTeam.A("{channelMemberCount, plural, =1 {# person} other {# people}} in this channel", i18n.Args{"channelMemberCount": n})
}

// teamsParts is getExternalTeamsTextElement for the connected teams.
func (p *ChannelHoverCard) teamsParts(ids []string) []i18n.Part {
	name := func(id string) string {
		if c := p.h.Core(); c != nil {
			if n, _, ok := c.Team(id, 20); ok && n != "" {
				return n
			}
		}
		return ""
	}
	switch len(ids) {
	case 0:
		return nil
	case 1:
		return nsTeam.RT("{teamName} is in this channel", "teamName", name(ids[0]))
	case 2:
		return nsTeam.RT("{teamName1} and {teamName2} are in this channel", "teamName1", name(ids[0]), "teamName2", name(ids[1]))
	}
	return nsTeam.RT("{teamName} and <t1>{numOtherTeams, plural, =1 {# other team} other {# other teams}}</t1> are in this channel",
		"numOtherTeams", len(ids)-1, "teamName", name(ids[0]))
}

// load starts the card's fetches once: the channel (conversations.info
// through ResolveConv), its member count, its last message, the
// connected teams' names and icons.
func (p *ChannelHoverCard) load(cv core.Conv, ok bool) {
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
			p.resolving = false
			p.missing = !found
		})
		return
	}
	if !channelViewable(cv) || cv.IsArchived || p.codeChannelCard(cv) {
		return
	}
	if !p.latestAsked {
		p.latestAsked = true
		id := p.id
		if _, known := c.ChannelPeopleCount(id); !known {
			// getMembershipCountsByChannelId's maybeRefetch
			run(p.h, func(ctx context.Context) (struct{}, error) {
				return struct{}{}, c.LoadPeopleCount(ctx, id)
			}, func(struct{}, error) {})
		}
		run(p.h, func(ctx context.Context) (string, error) {
			return c.ChannelLatestTS(ctx, id)
		}, func(ts string, err error) {
			// a failed fetch ends the loading state with what the store
			// has: nothing, "No messages"
			p.latest, p.latestOK = ts, true
		})
		for _, t := range cv.ConnectedTeams {
			if _, _, known := c.Team(t, 20); !known {
				t := t
				run(p.h, func(ctx context.Context) (struct{}, error) {
					c.EnsureTeam(ctx, t)
					return struct{}{}, nil
				}, func(struct{}, error) {})
			}
		}
	}
}

// Layout draws the card in the window (gtx at the window origin).
func (p *ChannelHoverCard) Layout(gtx layout.Context) layout.Dimensions {
	drain()
	th := p.h.Theme()
	cl := colorsOf(th)
	win := gtx.Constraints.Max
	cv, ok := p.conv()
	p.load(cv, ok)
	p.clicks(gtx, cv, ok)
	p.codeClicks(gtx)

	w := dp(gtx, 340)
	if mw := win.X - 2*dp(gtx, hovercard.AllowanceX); w > mw && !p.texty {
		w = mw
	}
	call, d := record(loose(gtx, image.Pt(w, win.Y)), func(gtx layout.Context) layout.Dimensions {
		return p.content(gtx, th, cl, cv, ok, w)
	})
	sz := image.Pt(w, d.Size.Y)
	var pos image.Point
	if p.texty {
		pos, _ = theme.KeepInBounds(p.anchor, sz, image.Rectangle{Max: win}, theme.TipTop, 0, -dp(gtx, hovercard.OffsetY), 0, 0, false)
	} else {
		pos = hovercard.PlaceTop(p.anchor, sz, win, dp(gtx, hovercard.OffsetY), dp(gtx, hovercard.AllowanceX), dp(gtx, hovercard.AllowanceY))
	}
	p.rect = image.Rectangle{Min: pos, Max: pos.Add(sz)}
	rad := dp(gtx, 8)
	card(gtx, cl, p.rect, rad)
	cs := clipRRect(gtx, p.rect, rad)
	macroAt(gtx, call, pos)
	cs.Pop()
	return layout.Dimensions{Size: win}
}

// clicks runs the button: View Channel navigates to the channel and
// closes the card; Join Channel joins it (the button spins meanwhile).
func (p *ChannelHoverCard) clicks(gtx layout.Context, cv core.Conv, ok bool) {
	if !p.btn.Clicked(gtx) || !ok || !p.buttons {
		return
	}
	if channelCardView(cv) {
		p.h.Navigate("c/" + p.id)
		p.done = true // closeCard
		return
	}
	chJoinMu.Lock()
	if chJoin[p.id] {
		chJoinMu.Unlock()
		return
	}
	chJoin[p.id] = true
	chJoinMu.Unlock()
	id := p.id
	finish := func(error) {
		chJoinMu.Lock()
		delete(chJoin, id)
		chJoinMu.Unlock()
	}
	if ChannelJoinHook != nil {
		ChannelJoinHook(id, func(err error) { post(p.h, func() { finish(err) }) })
		return
	}
	c := p.h.Core()
	run(p.h, func(ctx context.Context) (struct{}, error) {
		_, err := c.Join(ctx, id)
		return struct{}{}, err
	}, func(_ struct{}, err error) { finish(err) })
}

// content lays out the card's body w wide at the origin.
func (p *ChannelHoverCard) content(gtx layout.Context, th *theme.Theme, cl *Colors, cv core.Conv, ok bool, w int) layout.Dimensions {
	pad := dp(gtx, 16)
	inner := max(1, w-2*pad)
	msg := func(s string) layout.Dimensions {
		d := at(gtx, image.Pt(pad, pad), func(gtx layout.Context) layout.Dimensions {
			return para(loose(gtx, image.Pt(inner, 1<<20)), th, 15, 22, theme.Regular, cl.Fg, s, 0)
		})
		return layout.Dimensions{Size: image.Pt(w, d.Size.Y+2*pad)}
	}
	if !ok && !p.missing {
		return msg(nsTeam.T("Loading…"))
	}
	if !ok || !channelViewable(cv) {
		return msg(nsTeam.T("You don’t have access to this channel (ex. it’s private or in a workspace you’re not a part of), so its name isn’t shown"))
	}
	if p.codeChannelCard(cv) {
		return p.codeCard(gtx, th, cl, cv, w) // the HoverCard without padding_100
	}
	y := pad
	y += p.header(gtx, th, cl, cv, pad, y, inner)
	y += dp(gtx, 12) // the header's margin-bottom
	if !cv.IsArchived {
		y += p.items(gtx, th, cl, cv, pad, y, inner)
	}
	if p.buttons {
		y += dp(gtx, 12)
		label := nsTeam.T("Join Channel")
		view := channelCardView(cv)
		if view {
			label = nsTeam.T("View Channel")
		}
		chJoinMu.Lock()
		joining := chJoin[p.id]
		chJoinMu.Unlock()
		d := at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			if !joining || view {
				return Button(gtx, th, &p.btn, BtnOutline, BtnSmall, label, "", float32(inner)/gtx.Metric.PxPerDp)
			}
			// LoadingButton: the children hidden, the small spinner in
			// their place
			call, d := record(gtx, func(gtx layout.Context) layout.Dimensions {
				return Button(gtx, th, &p.btn, BtnOutline, BtnSmall, " ", "", float32(inner)/gtx.Metric.PxPerDp)
			})
			call.Add(gtx.Ops)
			s := dp(gtx, 16)
			at(gtx, image.Pt((d.Size.X-s)/2, (d.Size.Y-s)/2), func(gtx layout.Context) layout.Dimensions {
				return abSpinner(gtx, s, cl.Fg)
			})
			return d
		})
		y += d.Size.Y
	}
	return layout.Dimensions{Size: image.Pt(w, y+pad)}
}

// channelCardIcon is getChannelIconType with boldIcon (filled for a
// private channel).
func channelCardIcon(cv core.Conv) string {
	switch {
	case cv.IsArchived && cv.Kind == "private":
		return "archive-filled"
	case cv.IsArchived:
		return "archive"
	case cv.Kind == "private":
		return "lock-filled"
	}
	return "channel-filled"
}

// header draws the name row and the topic at (x, y) and returns their
// height (without the 12px margin under them).
func (p *ChannelHoverCard) header(gtx layout.Context, th *theme.Theme, cl *Colors, cv core.Conv, x, y, w int) int {
	lineH := dp(gtx, 22.5) // 18px × 1.25
	right := x + w
	// the right group (gap 4px between its children): the huddle badge,
	// then the mention count
	mention := false
	if c := p.h.Core(); c != nil {
		if n := c.Count(cv.ID).Mentions; n > 0 {
			mention = true
			at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
				right = drawMenuBadge(gtx, th, n, "themed", false, right, lineH)
				return layout.Dimensions{}
			})
		}
	}
	if ChannelHuddleBadge != nil {
		at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			r := right
			if mention {
				r -= dp(gtx, 4)
			}
			l := ChannelHuddleBadge(gtx, th, cv.ID, r, lineH)
			if p.huddle = l != r; p.huddle {
				right = l
			}
			return layout.Dimensions{}
		})
	}
	nameW := max(1, right-x)
	name := cv.Name // a DM's or group DM's: DeprecatedChannelName
	if cv.IsArchived && cv.Kind != "im" && cv.Kind != "mpim" {
		name += " (archived)" // ChannelName's suffix
	}
	at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(nameW, lineH)}
		ix := 0
		if cv.Kind != "im" && cv.Kind != "mpim" {
			// InlineChannelIcon: a 20px inline box, the glyph 1em (18px)
			box := dp(gtx, 20)
			g := dp(gtx, 18)
			at(gtx, image.Pt((box-g)/2, (lineH-g)/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, channelCardIcon(cv), 18, cl.Fg)
			})
			ix = box + dp(gtx, 4) // --space 2px + the avatar slot's 2px
		}
		return at(gtx, image.Pt(ix, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(max(1, nameW-ix), lineH)}
			return vcenter(gtx, lineH, func(gtx layout.Context) layout.Dimensions {
				return th.Layout(gtx, theme.TextStyle{Size: 18, Weight: theme.Black, Color: cl.Fg, LineHeight: 22.5, MaxLines: 1}, name)
			})
		})
	})
	h := lineH + dp(gtx, 4)                // the name row's margin_bottom_25
	if strings.TrimSpace(cv.Topic) != "" { // getChannelTopicText
		d := at(gtx, image.Pt(x, y+h), func(gtx layout.Context) layout.Dimensions {
			return p.topicView(gtx, th, cl, cv, w)
		})
		h += d.Size.Y
	}
	return h
}

// topicView draws the topic w wide: i0IN's mrkdwn (emoji, mentions and
// links through richview, no keyword highlights, no jumbomoji, no emoji
// tooltips) in multiline_ellipsis_2's two lines. The topic's blocks run
// as one paragraph (the clamp is the whole box's), and its mentions open
// no card of their own. Without the message rows' style (RichStyle) it is
// the plain text.
func (p *ChannelHoverCard) topicView(gtx layout.Context, th *theme.Theme, cl *Colors, cv core.Conv, w int) layout.Dimensions {
	var st *richview.Style
	if RichStyle != nil {
		st = RichStyle(p.h)
	}
	if st == nil {
		topic := strings.TrimSpace(richtext.PlainText(richtext.Doc{Blocks: cv.TopicDoc}))
		if topic == "" {
			topic = strings.TrimSpace(cv.Topic)
		}
		return para(loose(gtx, image.Pt(w, 1<<20)), th, 15, 22, theme.Regular, cl.Fg, topic, 2)
	}
	if p.topic == nil || p.topicKey != cv.Topic {
		var spans []richtext.Span
		for i, b := range cv.TopicDoc {
			if i > 0 {
				spans = append(spans, richtext.Span{Kind: richtext.Break})
			}
			spans = append(spans, topicSpans(b)...)
		}
		if len(spans) == 0 {
			spans = []richtext.Span{{Kind: richtext.Text, Text: cv.Topic}}
		}
		doc := richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: spans}}}
		p.topic = richview.New(doc, richview.Options{MaxLines: 2, NoEmojiTips: true, NoHoverCards: true})
		p.topicKey = cv.Topic
	}
	gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
	return p.topic.Layout(gtx, st, topicHandler{p.h})
}

// topicSpans are a topic block's inline spans (a list's items and a
// quote's lines one after another, line breaks between them).
func topicSpans(b richtext.Block) []richtext.Span {
	out := append([]richtext.Span(nil), b.Spans...)
	for _, it := range b.Items {
		if len(out) > 0 {
			out = append(out, richtext.Span{Kind: richtext.Break})
		}
		out = append(out, it...)
	}
	for _, c := range b.Children {
		if len(out) > 0 {
			out = append(out, richtext.Span{Kind: richtext.Break})
		}
		out = append(out, topicSpans(c)...)
	}
	return out
}

// topicHandler routes clicks in the topic: links open, a member opens
// their profile, a channel goes there.
type topicHandler struct{ h Host }

func (t topicHandler) OpenURL(u string)                  { _ = openurl.Open(u) }
func (t topicHandler) OpenUser(id string, a image.Point) { t.h.OpenProfile(a, id) }
func (t topicHandler) OpenChannel(id string)             { t.h.Navigate("c/" + id) }
func (t topicHandler) OpenUsergroup(string, image.Point) {}
func (t topicHandler) OpenImage(img richtext.Image)      { _ = openurl.Open(img.URL) }
func (t topicHandler) OpenFile(f *richtext.File)         {}
func (t topicHandler) Button(b richtext.Button)          {}

// items draws the body items' column (gap 4) at (x, y) and returns its
// height.
func (p *ChannelHoverCard) items(gtx layout.Context, th *theme.Theme, cl *Colors, cv core.Conv, x, y, w int) int {
	c := p.h.Core()
	gap := dp(gtx, 4)
	lineH := dp(gtx, 22)
	h := 0
	item := func(iconDraw func(gtx layout.Context), body func(gtx layout.Context, w int) int) {
		if h > 0 {
			h += gap
		}
		top := y + h
		at(gtx, image.Pt(x, top+dp(gtx, 2)), func(gtx layout.Context) layout.Dimensions {
			iconDraw(gtx)
			return layout.Dimensions{}
		})
		tx := dp(gtx, 20) + dp(gtx, 8)
		var bh int
		at(gtx, image.Pt(x+tx, top), func(gtx layout.Context) layout.Dimensions {
			bh = body(gtx, max(1, w-tx))
			return layout.Dimensions{}
		})
		h += max(lineH, bh)
	}
	glyph := func(name string) func(gtx layout.Context) {
		return func(gtx layout.Context) { icon(gtx, name, 20, cl.Fg) }
	}
	missing := func(gtx layout.Context, _ int) int {
		// MissingText size large: a 160×8 pill on the line's baseline
		srcBar(gtx, th, 0, dp(gtx, 11), srcSkelWidths[2], 1)
		return lineH
	}
	plain := func(s string) func(gtx layout.Context, w int) int {
		return func(gtx layout.Context, w int) int {
			return para(loose(gtx, image.Pt(w, 1<<20)), th, 15, 22, theme.Regular, cl.Fg, s, 0).Size.Y
		}
	}
	if n, ok := c.ChannelPeopleCount(cv.ID); ok {
		item(glyph("user-groups"), plain(memberCountText(n)))
	} else {
		item(glyph("user-groups"), missing)
	}
	if p.latestOK {
		item(glyph("clock"), plain(core.LastMessageText(p.latest, gtx.Now, nsTeam.T)))
	} else {
		item(glyph("clock"), missing)
	}
	if teams := cv.ConnectedTeams; len(teams) > 0 {
		name, url, _ := c.Team(teams[0], 20)
		item(func(gtx layout.Context) {
			pfTeamIcon(gtx, p.h, th, name, url, image.Point{}, 20, false)
		}, func(gtx layout.Context, w int) int {
			return p.teamsPara(gtx, th, cl, teams, w)
		})
	}
	return h
}

// teamsPara draws getExternalTeamsTextElement: team names bold (TeamName
// bold), the <t1> run strong with the other teams' names in a tooltip.
func (p *ChannelHoverCard) teamsPara(gtx layout.Context, th *theme.Theme, cl *Colors, teams []string, w int) int {
	parts := p.teamsParts(teams)
	type word struct {
		s    string
		bold bool
		t1   bool
	}
	var words []word
	for _, pt := range parts {
		bold := pt.Arg == "teamName" || pt.Arg == "teamName1" || pt.Arg == "teamName2" || pt.Tag == "t1" || pt.Tag == "strong" || pt.Tag == "b"
		for _, f := range strings.Fields(pt.Text) {
			words = append(words, word{f, bold, pt.Tag == "t1"})
		}
	}
	x, y, lh := 0, 0, dp(gtx, 22)
	space := textW(gtx, th, 15, theme.Regular, " ")
	var t1 image.Rectangle
	for _, wd := range words {
		wt := theme.Regular
		if wd.bold {
			wt = theme.Bold
		}
		ww := textW(gtx, th, 15, wt, wd.s)
		if x > 0 && x+ww > w {
			x, y = 0, y+lh
		}
		at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 15, wt, cl.Fg, wd.s) })
		})
		if wd.t1 {
			t1 = t1.Union(image.Rect(x, y, x+ww, y+lh))
		}
		x += ww + space
	}
	if !t1.Empty() && len(teams) > 1 {
		at(gtx, t1.Min, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(t1.Size())
			p.others.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: t1.Size()} })
			var lines []TipLine
			for _, id := range teams[1:] {
				n := ""
				if c := p.h.Core(); c != nil {
					n, _, _ = c.Team(id, 20)
				}
				lines = append(lines, TipLine{Text: n})
			}
			TooltipLines(gtx, th, &p.otherTp, p.others.Hovered(), t1.Size(), Above, lines)
			return layout.Dimensions{Size: t1.Size()}
		})
	}
	return y + lh
}

// themedBadge are Badge style "themed"'s colours (c-badge--themed:
// --dt_color-theme-base-imp, --dt_color-theme-content-imp; the default
// theme's aubergine 70 and 0 in each mode).
var themedBadge = [2]chipTok{
	{"var(--dt_color-theme-base-imp)", color.NRGBA{R: 131, G: 56, B: 138, A: 255}, color.NRGBA{R: 207, G: 161, B: 222, A: 255}},
	{"var(--dt_color-theme-content-imp)", color.NRGBA{R: 249, G: 237, B: 255, A: 255}, color.NRGBA{R: 32, G: 2, B: 33, A: 255}},
}
