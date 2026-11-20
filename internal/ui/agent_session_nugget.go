package ui

// The reply bar's agent nugget (web client build 9685e6ce144b, r0Yq
// ThreadReplies' AppSessionReplyNugget, which app_nugget_refinement makes
// AppSessionOpenThreadReplyNugget). messages draws ThreadReplies through
// SessionNuggetHost for a root with no reply, draft or scheduled reply, or a
// broadcast, and core.SessionReplyNugget decides what it shows:
//
//	.nuggetContainer  a column, the row's width, padding-bottom 4px
//	.nugget           the button: 600px wide at most (max-width 100%),
//	                  margin-left -4px, padding 4px, a 1px transparent
//	                  border, radius 8, gap 12px, centred; hovered or
//	                  focused, base-pry with an otl-ter border, and the
//	                  caret shows
//	.icon             the agent's 24px avatar (its status's icon_url or
//	                  icon_emoji, else the bot user's), or, for two agents
//	                  and more, .collapsedIconTile (24px, base-ter, radius
//	                  4, ai-agents at 16px in content-pry)
//	.statusShimmer    15px/1.46668, wrapping, the shimmer gradient
//	.arrow            caret-right 18px in content-ter, margin-left auto,
//	                  hidden until hovered or focused
//
// The label (useAppAgentNuggetLabels) is the agent's loading_message, else
// "{app} is working"; "Multiple agents are working" for two and more; for
// an app status with no agent processing, its loading messages in turn
// (2.4s each), else "{app} is working". A click opens the thread.

import (
	"image"
	"strings"
	"time"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

var nsMessageKit = i18n.NS("message_kit")

// sessionNuggetState is one thread's reply-bar nugget: its button, when
// its status's labels started turning, and what it last drew (tests).
type sessionNuggetState struct {
	btn   widget.Clickable
	since time.Time
	key   string
	label string
	drawn time.Time
}

var sessionNuggets = map[*App]map[string]*sessionNuggetState{} // UI goroutine only

func (a *App) sessionNuggetState(ch, ts string) *sessionNuggetState {
	m := sessionNuggets[a]
	if m == nil {
		m = map[string]*sessionNuggetState{}
		sessionNuggets[a] = m
	}
	k := ch + "/" + ts
	st := m[k]
	if st == nil {
		st = &sessionNuggetState{}
		m[k] = st
	}
	return st
}

// sessionNuggetRoot is the thread m's nugget is for.
func sessionNuggetRoot(m *core.Message) string {
	if m.ThreadTS != "" {
		return m.ThreadTS
	}
	return m.TS
}

// SessionNuggetShown is messages.SessionNuggetHost's: whether m's reply
// bar is the agent nugget.
func (a *App) SessionNuggetShown(m *core.Message) bool {
	if a.data == nil || a.data.core == nil || m == nil {
		return false
	}
	_, ok := a.data.core.SessionReplyNugget(m, time.Now())
	return ok
}

// LayoutSessionNugget is messages.SessionNuggetHost's: the nugget for m at
// the origin, as wide as the constraints.
func (a *App) LayoutSessionNugget(gtx layout.Context, m *core.Message) (layout.Dimensions, bool) {
	if a.data == nil || a.data.core == nil || m == nil {
		return layout.Dimensions{}, false
	}
	n, ok := a.data.core.SessionReplyNugget(m, gtx.Now)
	if !ok {
		return layout.Dimensions{}, false
	}
	root := sessionNuggetRoot(m)
	st := a.sessionNuggetState(m.Channel, root)
	if st.btn.Clicked(gtx) {
		a.OpenThread(m.Channel, root)
	}
	agentName := func(s string) string {
		if s == "" {
			return nsMessageKit.T("Agent") // useFallbackAgentName
		}
		return s
	}
	var label string
	var who core.AppNugget
	collapsed := len(n.Agents) > 1
	switch {
	case collapsed:
		label = nsMessageKit.T("Multiple agents are working")
	case len(n.Agents) == 1:
		ag := n.Agents[0]
		name := agentName(ag.Name)
		label = ag.LoadingMessage
		if label == "" {
			label = nsMessageKit.T("{appName} is working", "appName", name)
		}
		who = core.AppNugget{BotUserID: ag.BotUserID, Name: name, IconEmoji: ag.IconEmoji, IconURL: ag.IconURL}
	default: // the app's status: its loading messages in turn
		who = n.Status
		who.Name = agentName(who.Name)
		if k := who.BotUserID + "\x00" + strings.Join(who.LoadingMessages, "\x00"); st.key != k || st.since.IsZero() {
			st.key, st.since = k, gtx.Now
		}
		label = ""
		if len(who.LoadingMessages) > 0 {
			label = appNuggetLabel(who, st.since, gtx.Now)
			if len(who.LoadingMessages) > 1 {
				next := st.since.Add((gtx.Now.Sub(st.since)/(2400*time.Millisecond) + 1) * 2400 * time.Millisecond)
				gtx.Execute(op.InvalidateCmd{At: next})
			}
		}
		if label == "" {
			label = nsMessageKit.T("{appName} is working", "appName", who.Name)
		}
	}
	st.label, st.drawn = label, gtx.Now

	w := gtx.Constraints.Max.X
	bw := max(1, int(gtx.Metric.PxPerDp)) // a 1px CSS border, snapped as Chromium does
	pad, gap := gtx.Dp(4), gtx.Dp(12)
	ic, arrow := gtx.Dp(24), gtx.Dp(18)
	lh := gtx.Dp(22) // 15px × 1.46668
	boxW := min(gtx.Dp(600), w+gtx.Dp(4))
	tw := max(boxW-2*bw-2*pad-ic-gap-gap-arrow, 1)
	lines := a.wrapNugget(gtx, label, tw)
	textW := 0
	for _, ln := range lines {
		textW = max(textW, min(a.th.Measure(gtx, 15, theme.Regular, ln), tw))
	}
	inner := max(ic, len(lines)*lh)
	boxH := 2*bw + 2*pad + inner
	hot := st.btn.Hovered() || gtx.Source.Focused(&st.btn)

	off := op.Offset(image.Pt(-gtx.Dp(4), 0)).Push(gtx.Ops) // margin-left -4px
	box := image.Rectangle{Max: image.Pt(boxW, boxH)}
	if hot {
		theme.RRect(gtx, box, gtx.Dp(8), a.nuggetTok("otl-ter"))
		theme.RRect(gtx, box.Inset(bw), max(gtx.Dp(8)-bw, 0), a.nuggetTok("base-pry"))
	}
	x := bw + pad
	iy := bw + pad + (inner-ic)/2
	if collapsed {
		tile := image.Rect(x, iy, x+ic, iy+ic)
		theme.RRect(gtx, tile, gtx.Dp(4), a.nuggetTok("base-ter"))
		g := gtx.Dp(16)
		at(gtx, image.Pt(x+(ic-g)/2, iy+(ic-g)/2), image.Pt(g, g), func(gtx layout.Context) layout.Dimensions {
			return icons.Get("agents")(gtx, unit.Dp(16), a.nuggetTok("content-pry")) // Slack's ai-agents
		})
	} else {
		a.sessionNuggetAvatar(gtx, who, image.Pt(x, iy), ic)
	}
	tx := x + ic + gap
	ty := bw + pad + (inner-len(lines)*lh)/2
	for i, ln := range lines {
		a.shimmerLine(gtx, ln, image.Pt(tx, ty+i*lh), lh, max(textW, 1))
	}
	if hot {
		ax := boxW - bw - pad - arrow
		at(gtx, image.Pt(ax, bw+pad+(inner-arrow)/2), image.Pt(arrow, arrow), func(gtx layout.Context) layout.Dimensions {
			return icons.Get("chevron-right")(gtx, unit.Dp(18), a.nuggetTok("content-ter")) // Slack's caret-right
		})
	}
	func() {
		defer clip.Rect(box).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Exact(box.Size())
		st.btn.Layout(g, func(gtx layout.Context) layout.Dimensions {
			pointer.CursorPointer.Add(gtx.Ops)
			return layout.Dimensions{Size: box.Size()}
		})
	}()
	off.Pop()
	indicatorFrame(gtx, 33*time.Millisecond) // the shimmer moves
	return layout.Dimensions{Size: image.Pt(w, boxH+gtx.Dp(4))}, true
}

// sessionNuggetAvatar is the nugget's .icon: the agent's status icon_url or
// icon_emoji, else its bot user's avatar, s px with Slack's avatar corners
// (clamp(4px, 22.222%, 12px): 5.33px at 24).
func (a *App) sessionNuggetAvatar(gtx layout.Context, n core.AppNugget, p image.Point, s int) {
	off := op.Offset(p).Push(gtx.Ops)
	defer off.Pop()
	url := n.IconURL
	if url == "" && n.IconEmoji != "" {
		a.appNuggetAvatar(gtx, n, image.Point{}, s) // the emoji's image, as the footer draws it
		return
	}
	if url == "" {
		url = a.userAvatar(n.BotUserID)
	}
	a.avatar(gtx, url, n.Name, s, gtx.Dp(5.333))
}

// SessionNuggetLabel is the reply-bar nugget's label for thread ch/ts as
// last drawn, and when (tests).
func (a *App) SessionNuggetLabel(ch, ts string) (string, time.Time) {
	st := sessionNuggets[a][ch+"/"+ts]
	if st == nil {
		return "", time.Time{}
	}
	return st.label, st.drawn
}
