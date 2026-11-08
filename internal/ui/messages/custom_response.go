package messages

import (
	"encoding/json"
	"image"
	"image/color"
	"strings"
	"time"
	"unicode"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Slackbot custom responses (Customize Slack > Slackbot) as Slack's
// reskin_custom_responses experiment draws them (modern.vendor, 2026-11):
//
//   - REqS ZO isCustomResponse: subtype "slackbot_response" from
//     USLACKBOT (core.Message.CustomResponse). Hack Club's arrive with no
//     bot_profile, icons or username, so the client supplies the sender.
//   - BaseMessageAvatar: `if (isCustomResponse(msg) && reskin) return
//     <TeamIcon id={selfTeamIds.enterpriseId || teamId} size className>`,
//     a plain picture: no profile card, no click.
//   - MessageSender: with Slackbot AI on, a Slackbot message is not
//     interactive (a <span data-qa="message_sender_name">, no button,
//     hover card or member menu), and a custom response's name is the
//     team's name.
//   - BaseMessageSender: after the name, BaseCustomResponseInfoBadge:
//     <Tooltip position="top" tip=…><span
//     className="customResponseInfoBadgeIcon__mN56C"
//     data-qa="custom_response_info_badge" role="img"><Icon name="info"
//     size="18" inline/></span></Tooltip>; the tip is "This automated
//     message was set up by a workspace admin." for workspace admins
//     (Xfgd Zo), else "…by someone in your workspace.".
//     .customResponseInfoBadgeIcon__mN56C{color:var(--dt_color-content-pry);margin-left:4px}
//   - MessageLabels: the "Custom response" label line is dropped.
//
// Slackbot AI is an entitlement (ACCESS_SLACK_AI_SLACKBOT) Clack can't
// read; Hack Club's client shows the team's name, so it is on there and
// Clack treats it as on.

// crTipDelay is Slack's BaseTooltip default delay (150 ms).
const crTipDelay = 150 * time.Millisecond

var (
	// the sender name of a non-interactive message (no c-message__sender_button)
	senderNameClass = slackc.Class("", "data-qa", "message_sender_name").Tag("span")
	crBadgeClass    = slackc.Class("customResponseInfoBadgeIcon__mN56C", "data-qa", "custom_response_info_badge", "role", "img").Tag("span")
	teamAvatarClass = slackc.Class("c-team_icon c-message_kit__avatar").Tag("i")
)

// customReskin reports whether reskin_custom_responses is on: core's
// group (experiments.getByUser), unless a plugin forces it through Taut's
// experiments slice (BringBackSlackbot sets it "off").
func customReskin(h Host) bool {
	if t := tautOf(); t != nil && t.SlicePatched("experiments") {
		if raw, ok := t.View("experiments", "reskin_custom_responses"); ok {
			if g, ok := experimentGroup(raw); ok {
				return g == "on"
			}
		}
	}
	return h.Core() != nil && h.Core().CustomResponseReskin()
}

// experimentGroup reads Slack's experiments-slice entry ({group: "on"})
// or a bare group string.
func experimentGroup(raw json.RawMessage) (string, bool) {
	var e struct {
		Group *string `json:"group"`
	}
	if json.Unmarshal(raw, &e) == nil && e.Group != nil {
		return *e.Group, true
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return s, true
	}
	return "", false
}

// customResponse decides, every frame, whether the row is drawn as the
// team (the experiment and the team icon can change at any time; Hack
// Club's icon rotates).
func (r *row) customResponse(rc *rowCtx) {
	r.custom = false
	if !r.m.CustomResponse() || rc.h.Core() == nil || !customReskin(rc.h) {
		return
	}
	r.custom = true
	r.crName, r.crIcon = rc.h.Core().CustomResponseTeam(int(mt.avatar))
	if r.crName == "" {
		r.crName = r.name
	}
}

// senderName is the name the header shows (and copy writes).
func (r *row) senderName() string {
	if r.custom {
		return r.crName
	}
	return r.name
}

// layoutTeamAvatar is the custom response's TeamIcon in the avatar slot:
// a CSS background-image still replaces it (CustomSlackbot's rule hits
// .c-message_kit__avatar, which Slack puts on the TeamIcon too).
func (r *row) layoutTeamAvatar(gtx layout.Context, p slackc.Props, s int) layout.Dimensions {
	st := teamAvatarClass.EnterClass(gtx, 0, 0, slackc.ExtraClass(p))
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{}
	}
	url := st.BackgroundImage(r.crIcon)
	r.avDrawn = url
	size := image.Pt(s, s)
	gtx.Constraints = layout.Exact(size)
	draw := func(gtx layout.Context) layout.Dimensions {
		drawTeamIcon(gtx, r.rc.h, url, r.crName, image.Rectangle{Max: size}, r.rc.c)
		return layout.Dimensions{Size: size}
	}
	if st.Active() {
		return st.Wrap(gtx, draw)
	}
	return draw(gtx)
}

// drawTeamIcon is BaseTeamIcon: the picture in the avatar's rounded square
// (.c-team_icon: clamp(4px, 22.222%, 12px), the 1px #00000014 inset
// ring), or the team's initials on #616061 for the default icon.
func drawTeamIcon(gtx layout.Context, h Host, url, name string, r image.Rectangle, c *colors) {
	radius := avatarRadius(r.Dx())
	if url != "" {
		drawAvatar(gtx, h, url, r, c)
	} else {
		rrect(gtx.Ops, r, radius, rgb(0x616061))
		if in := teamInitials(name); in != "" {
			ts := textStyle{size: float32(r.Dx()) * 0.4 / gtx.Metric.PxPerSp, weight: theme.Bold, col: rgb(0xffffff)}
			sz := measure(gtx, h.Theme(), ts, in)
			at(gtx, r.Min.X+(r.Dx()-sz.X)/2, r.Min.Y+(r.Dy()-sz.Y)/2, func(gtx layout.Context) layout.Dimensions {
				return label(gtx, h.Theme(), ts, in)
			})
		}
	}
	ring(gtx, r, radius, color.NRGBA{A: 0x14})
}

// ring strokes a 1px inset outline (the ::after inset box-shadow): a
// 2px stroke on the edge, clipped to the shape.
func ring(gtx layout.Context, r image.Rectangle, radius int, col color.NRGBA) {
	w := float32(2 * max(1, dp(gtx, 1)))
	defer clip.UniformRRect(r, radius).Push(gtx.Ops).Pop()
	paint.FillShape(gtx.Ops, col, clip.Stroke{Path: clip.UniformRRect(r, radius).Path(gtx.Ops), Width: w}.Op())
}

// teamInitials is Slack's default team icon text: the first letters of
// the first two words.
func teamInitials(name string) string {
	var b strings.Builder
	n := 0
	for _, w := range strings.Fields(name) {
		for _, r := range w {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				b.WriteRune(r)
				n++
			}
			break
		}
		if n == 2 {
			break
		}
	}
	return b.String()
}

// nameSpan is the non-interactive sender (MessageSender with
// isInteractive false): the name only, no underline, click or menu.
func (r *row) nameSpan(gtx layout.Context, bs slackc.Styled) layout.Dimensions {
	c := r.rc.c
	nameS := textStyle{size: float32(bs.FontSize(15)), weight: theme.Black, col: bs.Color(c.Text), lineH: 22}
	before, after := bs.Generated()
	r.nameShown = before + after
	if nameS.size <= 0 {
		return layout.Dimensions{Size: image.Pt(0, dp(gtx, nameS.lineH)), Baseline: dp(gtx, 5)}
	}
	r.nameShown = before + r.crName + after
	return label(gtx, r.rc.th, nameS, r.crName)
}

// crHoverTag is the info badge's hover target.
type crHoverTag struct{ _ byte }

// customBadge draws BaseCustomResponseInfoBadge at x on a name line of
// height lineH and returns its width (margin included); 0 when CSS hides
// it (CustomSlackbot's "hide badge").
func (r *row) customBadge(gtx layout.Context, x, lineH int) int {
	rc := r.rc
	st := crBadgeClass.Enter(gtx, hoverState(r.crHovered), 0)
	defer st.Leave()
	if st.Hidden() {
		r.crHovered = false
		return 0
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &r.crTag, Kinds: pointer.Enter | pointer.Leave | pointer.Cancel})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok {
			r.crHovered = e.Kind == pointer.Enter
		}
	}
	is := dp(gtx, 18)
	x += dp(gtx, 4)
	top := (lineH - is) / 2
	t := op.Offset(image.Pt(x, top)).Push(gtx.Ops)
	area := clip.Rect{Max: image.Pt(is, is)}.Push(gtx.Ops)
	event.Op(gtx.Ops, &r.crTag)
	area.Pop()
	icons.Get("info")(gtx, unit.Dp(18), st.Color(rc.c.Text))
	tip := ""
	if r.crHovered {
		tip = "This automated message was set up by someone in your workspace."
		if rc.h.Core().Self().IsAdmin {
			tip = "This automated message was set up by a workspace admin."
		}
	}
	r.crTipText = tip
	r.crTip.Delay, r.crTip.MaxWidth = crTipDelay, 200
	popups.Tooltip(gtx, rc.th, &r.crTip, r.crHovered, image.Pt(is, is), popups.Above, tip, "")
	t.Pop()
	return dp(gtx, 4) + is
}
