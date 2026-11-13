package popups

import (
	"context"
	"image"
	"image/color"
	"slices"
	"strconv"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// A code channel's hover card. With session_channels on, ChannelHoverCard
// (boot-shared 7+JF) draws a code channel (aK5v $n: a record channel whose
// record_type is agent_channel or whose record_id starts with "AC:") as
// HoverCard > CodeChannelEntityCard({surface: ChannelHoverCard,
// channelId, borderless}) (boot-shared ICrU, build 3889f0c93d30), without
// the card's padding. The entity card has three layouts:
// session_channels_unfurls_redesign (CodeChannelRedesignCard),
// session_channels_live_previews (the header a button, live previews),
// and the classic one; neither experiment is assigned to the account
// (experiments.getByUser, 2026-11-13), so this is the classic card, whose
// live-message subscription (OfiY, enabled only with one of them) stays
// off:
//
//   - the whole card is one button (p.Nm, .card__iFien.borderless__gL7jI:
//     flex column, base-pry, radius 8, overflow hidden, no border, no
//     margin): a click goes to the channel (track 2172, then navigate).
//     The trigger unmounts with the view, which closes the card.
//   - header (.header__NmVyL: flex, centred, gap 12px, padding 8px 12px):
//     a 40×40 tile (.iconContainer__Rghrh: radius 8, aubergine-5 behind an
//     aubergine-60 icon, 20px) with getIconName: minus for a stale
//     channel, pr-open for an open or blocked PR, pr-closed, branch for a
//     merged one, else code; then a column (flex 1, min-width 0): the
//     title row (gap 8px) of the name (InlineChannelEntity boldIcon,
//     15px bold, one line with an ellipsis) and the status Tag (isMicro;
//     neutral, informative while Working, warning for Needs attention,
//     success when Done; "Ready" for an active channel, else
//     getAgentStatusLabel), and the subtitle "Code channel" (13px
//     content-sec, max-height 1.5em).
//   - body: the summary when there is one (.summary__ja4Yl: padding 0
//     12px 2px, column gap 4px): its text (15px content-pry, two lines
//     at most, pre-wrap; CodeChannelPreviewHoverCardTrigger is disabled
//     without session_channels_summary) and "{author} at {time}" (13px
//     content-ter, padding-bottom 4px).
//   - footer (.footer__EqD4q: border-top 1px otl-ter, flex, centred, gap
//     8px, padding 8px 12px): the facepile (r6f2: the agents, then the
//     channel's first members; the first three as 24px avatars with 4px
//     corners in a 2px base-pry ring, 4px over each other, the last one
//     "+N" past three; its tooltip lists the first five names, agents
//     first, then by name, you last, "{names} +{count} more" past five),
//     then "started {date} at {time}" (13px content-sec; the date as a
//     day divider: Today, Yesterday, "Monday, November 9th" or another
//     year's date) when the channel has a created time.

var nsCodeCard = i18n.NS("code_channel_entity_card")

// codeTok is Slack's --dt_color-<name> in th's mode, with a stylesheet
// theme's own value when it sets one.
func codeTok(th *theme.Theme, name string) color.NRGBA {
	expr := "var(--dt_color-" + name + ")"
	c, _ := css.ParseColor(css.SlackVars(th != nil && th.Dark)["--dt_color-"+name])
	if th != nil && th.Tokens != nil {
		theme.SetColor(th.Tokens, &c, expr)
	}
	return c
}

// CodeCardState is what the code channel card shows (tests).
type CodeCardState struct {
	Icon, Status, Tag, TagStyle, Subtitle string
	Summary, Attribution                  string
	Faces                                 []string // the facepile's people, in Slack's order
	Tip                                   string   // the facepile's tooltip
	Started                               string
}

// codeChannelCard reports whether ChannelHoverCard draws cv as a code
// channel (aK5v $n and session_channels).
func (p *ChannelHoverCard) codeChannelCard(cv core.Conv) bool {
	c := p.h.Core()
	return cv.Code != nil && c != nil && c.Experiment("session_channels") == "on"
}

// codeIcon is ICrU's getIconName.
func codeIcon(status, pr string) string {
	switch {
	case status == core.AgentStale:
		return "minus"
	case pr == "open" || pr == "blocked":
		return "pr-open"
	case pr == "closed":
		return "pr-closed"
	case pr == "merged":
		return "branch"
	}
	return "code"
}

// codeTagStyle is getTagStyle.
func codeTagStyle(status string) string {
	switch status {
	case core.AgentProcessing:
		return "informative"
	case core.AgentSuspended:
		return "warning"
	case core.AgentClosed:
		return "success"
	}
	return "neutral"
}

// codeTagLabel is t$: "Ready" for an active channel, else
// getAgentStatusLabel (translated, session_channels being on).
func codeTagLabel(status string) string {
	if status == core.AgentActive || status == "" {
		return nsCodeCard.T("Ready")
	}
	return i18n.NS("agent_tasks").T(core.AgentStatusLabel(status))
}

// codeSubtitle is tI: "Code channel" under code_channels_naming, else
// "Session channel".
func (p *ChannelHoverCard) codeSubtitle() string {
	if c := p.h.Core(); c != nil && c.Experiment("code_channels_naming") == "on" {
		return nsCodeCard.T("Code channel")
	}
	return nsCodeCard.T("Session channel")
}

// codeFaces is tg: the agents (encoded_agent_bot_user_ids), then the
// channel's first members that aren't agents (only for a channel the
// viewer is in or a public one).
func (p *ChannelHoverCard) codeFaces(cv core.Conv) []string {
	var out []string
	if cv.Code != nil {
		out = append(out, cv.Code.AgentBotIDs...)
	}
	if !cv.Member && cv.Kind != "channel" {
		return out
	}
	c := p.h.Core()
	if c == nil {
		return out
	}
	ms, _ := c.ChannelTopMembers(cv.ID)
	for _, id := range ms {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// codeFaceTip is the facepile's tooltip: the first five sorted (agents
// first, then by name, you last), listified, "{names} +{count} more" past
// five.
func (p *ChannelHoverCard) codeFaceTip(ids, agents []string) string {
	if len(ids) == 0 {
		return ""
	}
	c := p.h.Core()
	self := ""
	if c != nil {
		self = c.Self().ID
	}
	nsBrowser := i18n.NS("code-channels-browser")
	type nm struct {
		id, name string
	}
	shown := ids[:min(5, len(ids))]
	ns := make([]nm, 0, len(shown))
	for _, id := range shown {
		n := id
		if id == self {
			n = nsBrowser.T("you")
		} else if c != nil {
			if u, ok := c.User(id); ok && u.Name != "" {
				n = u.Name
			}
		}
		ns = append(ns, nm{id, n})
	}
	slices.SortStableFunc(ns, func(a, b nm) int {
		if a.id == self {
			return 1
		}
		if b.id == self {
			return -1
		}
		aa, ba := slices.Contains(agents, a.id), slices.Contains(agents, b.id)
		if aa != ba {
			if aa {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.name), strings.ToLower(b.name))
	})
	names := make([]string, len(ns))
	for i, n := range ns {
		names[i] = n.name
	}
	list := i18n.Listify(names, false)
	if more := len(ids) - len(shown); more > 0 {
		return nsBrowser.T("{names} +{count} more", "names", list, "count", more)
	}
	return list
}

// codeStarted is "started {date} at {time}" for the channel's created
// time ("" without one).
func codeStarted(created int64, now time.Time) string {
	if created <= 0 {
		return ""
	}
	t := time.Unix(created, 0).In(now.Location())
	loc := i18n.Locale()
	return nsCodeCard.T("started {date} at {time}", "date", i18n.DayDivider(loc, t, now, false), "time", clock(t))
}

// CodeCard is the code channel card's state (tests); ok false when the
// card isn't one.
func (p *ChannelHoverCard) CodeCard(now time.Time) (CodeCardState, bool) {
	cv, ok := p.conv()
	if !ok || !channelViewable(cv) || !p.codeChannelCard(cv) {
		return CodeCardState{}, false
	}
	pr := ""
	if cv.Code != nil {
		pr = cv.Code.PRStatus
	}
	st := CodeCardState{Icon: codeIcon(cv.CodeStatus, pr), Status: cv.CodeStatus, Tag: codeTagLabel(cv.CodeStatus),
		TagStyle: codeTagStyle(cv.CodeStatus), Subtitle: p.codeSubtitle(), Started: codeStarted(cv.Created, now)}
	if s, ok := p.codeSummary(); ok {
		st.Summary, st.Attribution = s.Text, p.codeAttribution(s)
	}
	st.Faces = p.codeFaces(cv)
	var agents []string
	if cv.Code != nil {
		agents = cv.Code.AgentBotIDs
	}
	st.Tip = p.codeFaceTip(st.Faces, agents)
	return st, true
}

func (p *ChannelHoverCard) codeSummary() (core.CodeSummary, bool) {
	c := p.h.Core()
	if c == nil {
		return core.CodeSummary{}, false
	}
	return c.CodeChannelSummary(p.id)
}

// codeAttribution is "{author} at {time}" (the message's time).
func (p *ChannelHoverCard) codeAttribution(s core.CodeSummary) string {
	sec, _, _ := strings.Cut(s.TS, ".")
	n, _ := strconv.ParseInt(sec, 10, 64)
	return nsCodeCard.T("{author} at {time}", "author", s.Author, "time", clock(time.Unix(n, 0)))
}

// codeLoad starts the code card's fetches once: the channel's first
// members (membershipOrdered's maybeRefetch) and the facepile's people.
func (p *ChannelHoverCard) codeLoad(cv core.Conv) {
	if p.codeAsked {
		return
	}
	p.codeAsked = true
	c := p.h.Core()
	if c == nil {
		return
	}
	id := p.id
	var agents []string
	if cv.Code != nil {
		agents = append(agents, cv.Code.AgentBotIDs...)
	}
	_, known := c.ChannelTopMembers(id)
	fetch := !known && (cv.Member || cv.Kind == "channel")
	run(p.h, func(ctx context.Context) (struct{}, error) {
		if fetch {
			_ = c.LoadChannelTopMembers(ctx, id)
		}
		ids := append([]string(nil), agents...)
		if ms, ok := c.ChannelTopMembers(id); ok {
			ids = append(ids, ms...)
		}
		c.EnsureUsers(ctx, ids)
		return struct{}{}, nil
	}, func(struct{}, error) {})
}

// codeCard is the card's body: CodeChannelEntityCard w wide at the
// origin (the HoverCard has no padding around it).
func (p *ChannelHoverCard) codeCard(gtx layout.Context, th *theme.Theme, cl *Colors, cv core.Conv, w int) layout.Dimensions {
	p.codeLoad(cv)
	pad := dp(gtx, 12)
	y := 0
	// header: padding 8px 12px, the 40px tile, gap 12px, the column
	tile := dp(gtx, 40)
	cx := pad + tile + dp(gtx, 12)
	cw := max(1, w-pad-cx)
	titleH := dp(gtx, 22)  // 15px × 1.46668
	subH := dp(gtx, 19.07) // 13px × 1.46668 (max-height 1.5em: one line)
	colH := titleH + subH
	rowH := max(tile, colH)
	top := y + dp(gtx, 8)
	pr := ""
	if cv.Code != nil {
		pr = cv.Code.PRStatus
	}
	tr := image.Rect(pad, top+(rowH-tile)/2, pad+tile, top+(rowH-tile)/2+tile)
	rrect(gtx, tr, dp(gtx, 8), codeTok(th, "plt-aubergine-5"))
	ig := dp(gtx, 20)
	at(gtx, image.Pt(tr.Min.X+(tile-ig)/2, tr.Min.Y+(tile-ig)/2), func(gtx layout.Context) layout.Dimensions {
		return icon(gtx, codeIcon(cv.CodeStatus, pr), 20, codeTok(th, "plt-aubergine-60"))
	})
	cy := top + (rowH-colH)/2
	// the title row: the name (shrinks, ellipsis), gap 8px, the status tag
	label := codeTagLabel(cv.CodeStatus)
	tagW := textW(gtx, th, 12, theme.Regular, label) + 2*dp(gtx, 4)
	nameMax := max(1, cw-dp(gtx, 8)-tagW)
	nw := p.inlineName(gtx, th, cl, cv, cx, cy, nameMax, titleH, 15, theme.Bold)
	tagH := dp(gtx, 20)
	tx := cx + nw + dp(gtx, 8)
	bg, fg := menuTagColors(th, codeTagStyle(cv.CodeStatus))
	tg := image.Rect(tx, cy+(titleH-tagH)/2, tx+tagW, cy+(titleH-tagH)/2+tagH)
	rrect(gtx, tg, dp(gtx, 4), bg)
	at(gtx, image.Pt(tg.Min.X+dp(gtx, 4), tg.Min.Y), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, tagH, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 12, theme.Regular, fg, label)
		})
	})
	at(gtx, image.Pt(cx, cy+titleH), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(cw, subH)}
		return vcenter(gtx, subH, func(gtx layout.Context) layout.Dimensions {
			return th.Layout(gtx, theme.TextStyle{Size: 13, Weight: theme.Regular, Color: codeTok(th, "content-sec"), LineHeight: 19.07, MaxLines: 1}, p.codeSubtitle())
		})
	})
	y = top + rowH + dp(gtx, 8)

	// body: the summary
	if s, ok := p.codeSummary(); ok {
		iw := max(1, w-2*pad)
		d := at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			return para(loose(gtx, image.Pt(iw, 1<<20)), th, 15, 22, theme.Regular, codeTok(th, "content-pry"), s.Text, 2)
		})
		y += d.Size.Y + dp(gtx, 4) // gap 4px
		d = at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			return para(loose(gtx, image.Pt(iw, 1<<20)), th, 13, 19.07, theme.Regular, codeTok(th, "content-ter"), p.codeAttribution(s), 0)
		})
		y += d.Size.Y + dp(gtx, 4) + dp(gtx, 2) // its padding-bottom, the summary's
	}

	// footer: border-top 1px otl-ter, padding 8px 12px
	b1 := border1(gtx)
	theme.Fill(gtx, image.Rect(0, y, w, y+b1), codeTok(th, "otl-ter"))
	y += b1
	fy := y + dp(gtx, 8)
	faces := p.codeFaces(cv)
	fh := dp(gtx, 24)
	started := codeStarted(cv.Created, gtx.Now)
	x := pad
	if len(faces) > 0 {
		x += p.codeFacepileWidth(gtx, faces) + dp(gtx, 8)
	}
	// the started time wraps (.startedTime__WMzI6 has no nowrap); the row
	// centres the facepile and the text
	var stCall op.CallOp
	var stD layout.Dimensions
	if started != "" {
		stCall, stD = record(gtx, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(max(1, w-pad-x), 1<<20)}
			return th.Layout(gtx, theme.TextStyle{Size: 13, Weight: theme.Regular, Color: codeTok(th, "content-sec"), LineHeight: 19.07}, started)
		})
	}
	rowH = 0
	if len(faces) > 0 {
		rowH = fh
	}
	rowH = max(rowH, stD.Size.Y)
	var agents []string
	if cv.Code != nil {
		agents = cv.Code.AgentBotIDs
	}
	var pile image.Rectangle // the facepile's box, for its tooltip
	if len(faces) > 0 {
		o := image.Pt(pad, fy+(rowH-fh)/2)
		fw := p.codeFacepile(gtx, th, faces, o)
		pile = image.Rectangle{Min: o, Max: o.Add(image.Pt(fw, fh))}
	}
	if started != "" {
		macroAt(gtx, stCall, image.Pt(x, fy+(rowH-stD.Size.Y)/2))
	}
	y = fy + rowH + dp(gtx, 8)

	// the card is the button: a click anywhere navigates; the facepile's
	// tooltip area lies above it and passes the pointer through
	sz := image.Pt(w, y)
	func() {
		defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
		gtx := gtx
		gtx.Constraints = layout.Exact(sz)
		p.codeBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			pointer.CursorPointer.Add(gtx.Ops)
			return layout.Dimensions{Size: sz}
		})
	}()
	p.pile = pile
	if !pile.Empty() {
		func() {
			defer op.Offset(pile.Min).Push(gtx.Ops).Pop()
			func() {
				defer pointer.PassOp{}.Push(gtx.Ops).Pop()
				defer clip.Rect{Max: pile.Size()}.Push(gtx.Ops).Pop()
				p.faceHover.Add(gtx.Ops)
			}()
			Tooltip(gtx, th, &p.faceTip, p.faceHover.Update(gtx.Source), pile.Size(), Above, p.codeFaceTip(faces, agents), "")
		}()
	}
	return layout.Dimensions{Size: sz}
}

// codeFacepileWidth is codeFacepile's width for ids.
func (p *ChannelHoverCard) codeFacepileWidth(gtx layout.Context, ids []string) int {
	return (min(3, len(ids))-1)*(dp(gtx, 24)-dp(gtx, 4)) + dp(gtx, 24)
}

// codeFacepile draws r6f2's facepile at p and returns its width: the
// first three of ids as 24px avatars (corners 4px, a 2px base-pry ring
// with 6px corners, each 4px over the one before), the last one under
// c-avatar__additional_overlay ("+N", "9+") when there are more (its
// tooltip is codeCard's, above the card's button).
func (p *ChannelHoverCard) codeFacepile(gtx layout.Context, th *theme.Theme, ids []string, at0 image.Point) int {
	s := dp(gtx, 24)
	ring := dp(gtx, 2)
	step := s - dp(gtx, 4)
	shown := ids[:min(3, len(ids))]
	rest := len(ids) - 3
	ringC := codeTok(th, "base-pry")
	c := p.h.Core()
	for i, id := range shown {
		o := at0.Add(image.Pt(i*step, 0))
		rrect(gtx, image.Rect(o.X-ring, o.Y-ring, o.X+s+ring, o.Y+s+ring), dp(gtx, 6), ringC)
		url := ""
		if c != nil {
			if u, ok := c.User(id); ok {
				url = pfAvatarURL(u, false)
			}
		}
		func() {
			defer op.Offset(o).Push(gtx.Ops).Pop()
			sz := image.Pt(s, s)
			drawn := false
			if url != "" && p.h.Images() != nil {
				if img, ok := p.h.Images().Get(url, s); ok {
					theme.Image(gtx, img, sz, dp(gtx, 4))
					drawn = true
				}
			}
			if !drawn {
				rrect(gtx, image.Rectangle{Max: sz}, dp(gtx, 4), colorsOf(th).Selected)
			}
			if i == len(shown)-1 && rest > 0 {
				lbl := "+" + strconv.Itoa(rest)
				if rest > 9 {
					lbl = "9+"
				}
				rrect(gtx, image.Rectangle{Max: sz}, dp(gtx, 4), color.NRGBA{A: 0x80})
				tw := textW(gtx, th, 15, theme.Bold, lbl)
				at(gtx, image.Pt((s-tw)/2, 0), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, s, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 15, theme.Bold, color.NRGBA{R: 255, G: 255, B: 255, A: 255}, lbl)
					})
				})
			}
		}()
	}
	return (len(shown)-1)*step + s
}

// inlineName draws InlineChannelEntity with boldIcon at (x, y) in a line
// lineH tall, at most maxW wide (one line, ellipsis), size px text of
// weight wt, and returns its width: the 20px icon box with the glyph at
// 1em, 2px + the avatar slot's 2px, then the name.
func (p *ChannelHoverCard) inlineName(gtx layout.Context, th *theme.Theme, cl *Colors, cv core.Conv, x, y, maxW, lineH int, size float32, wt font.Weight) int {
	name := cv.Name
	if cv.IsArchived && cv.Kind != "im" && cv.Kind != "mpim" {
		name += " (archived)" // ChannelName's suffix
	}
	var d layout.Dimensions
	at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxW, lineH)}
		ix := 0
		if cv.Kind != "im" && cv.Kind != "mpim" {
			box := dp(gtx, 20)
			g := dp(gtx, size)
			at(gtx, image.Pt((box-g)/2, (lineH-g)/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, channelCardIcon(cv), size, cl.Fg)
			})
			ix = box + dp(gtx, 4)
		}
		d = at(gtx, image.Pt(ix, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(max(1, maxW-ix), lineH)}
			return vcenter(gtx, lineH, func(gtx layout.Context) layout.Dimensions {
				return th.Layout(gtx, theme.TextStyle{Size: unit.Sp(size), Weight: wt, Color: cl.Fg, LineHeight: unit.Sp(float32(lineH) / gtx.Metric.PxPerDp), MaxLines: 1}, name)
			})
		})
		d.Size.X += ix
		return d
	})
	return min(d.Size.X, maxW)
}

// FacepileRect is the code card's facepile in window coordinates (tests;
// empty when it has none).
func (p *ChannelHoverCard) FacepileRect() image.Rectangle {
	if p.pile.Empty() {
		return image.Rectangle{}
	}
	return p.pile.Add(p.rect.Min)
}

// FacepileTipShowing reports whether the facepile's tooltip shows (tests).
func (p *ChannelHoverCard) FacepileTipShowing(now time.Time) bool { return p.faceTip.Showing(now) }

// codeClicks runs the card's button: the channel, and the card closes
// (its trigger goes with the view).
func (p *ChannelHoverCard) codeClicks(gtx layout.Context) {
	if p.codeBtn.Clicked(gtx) {
		p.h.Navigate("c/" + p.id)
		p.done = true
	}
}
