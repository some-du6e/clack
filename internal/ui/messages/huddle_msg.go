package messages

import (
	"image"
	"image/color"
	"strconv"
	"strings"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Huddle messages, the row around richtext's HuddleMsgText sentence
// (richview huddle.go draws the sentence and the Join Huddle button),
// ported from Slack's client (build 4d0c78085232):
//
//   - isHuddleMsg (client-boot p6HR mb): the message has a room_id, which
//     makeHuddleObject copies from msg.room for a message with no
//     subtype, thread_broadcast, huddle_message or huddle_thread.
//   - getMsgSenderEntity (client-boot 33X/): a huddle message's sender is
//     {username: the status line (IQT5), isHuddleMsg, huddleActivityStatus
//     (zq6z K8)}, never the member who posted it (Slackbot).
//   - BaseMessageSender (boot-shared TwxH): that name as a plain span
//     (huddle_message_user_name, not a button), no APP badge
//     (renderBotLabel skips huddle messages), and renderStatusBadge: LIVE
//     for an "active" huddle, MISSED for a missed one (K+HP; the MISSED
//     text only with huddles_missed_notification).
//   - BaseMessageAvatar (boot-shared vXN3): with system_messages_redesign_m1
//     HuddleSystemMessageIcon (the 16px headphones in .systemIcon__mZbDL,
//     content-hgl-2 while live), else HuddleMsgAvatar (vjFA).
//   - Message (NnKv) and ThreadRootGeneric (3ZeZ): with
//     system_messages_redesign_m1 on, a huddle_message or huddle_thread
//     (R+b2 kn) is a redesigned system message: no sender line, and
//     HuddleMsgText's compact form in SystemMessage (I0jQ): "{status}.
//     {sentence}" (the status bold in a thread root), the time, and the
//     small primary Join Huddle (no faces) 4px under the text.

// huddleSubtype is R+b2 kn isHuddleMessage.
func huddleSubtype(sub string) bool { return sub == "huddle_thread" || sub == "huddle_message" }

// isHuddleMsg is p6HR mb.
func isHuddleMsg(m *core.Message) bool {
	if len(m.Huddle) <= 2 {
		return false
	}
	switch m.Subtype {
	case "", "thread_broadcast", "huddle_message", "huddle_thread":
		return true
	}
	return false
}

// huddleMsg is what a huddle message's row needs: the room, Slack's
// status line and the activity state.
type huddleMsg struct {
	room   *richtext.HuddleRoom
	status string // IQT5: the sender's name, the system line's lead
	act    string // zq6z K8: "active", "inactive-but-live", "missed" or ""
}

// huddleOf reads m's huddle ("" room: none).
func huddleOf(app *core.App, m *core.Message) (huddleMsg, bool) {
	if app == nil || !isHuddleMsg(m) {
		return huddleMsg{}, false
	}
	room, ok := richtext.ParseHuddleRoom(m.Huddle)
	if !ok {
		return huddleMsg{}, false
	}
	if m.Channel != "" {
		room.Channel = m.Channel
	}
	self := app.Self().ID
	missedOn := app.Experiment("huddles_missed_notification") == "on" // pSxm Z0
	missed := missedOn && room.HasEnded && room.Missed[self]          // zq6z Qw getDidUserMissHuddleInvite
	room.Joinable = room.Live && !containsID(room.Active, self)       // EKdW shouldAddJoinButton
	h := huddleMsg{room: room}
	switch {
	case room.Live:
		h.act = "active"
	case !room.HasEnded:
		h.act = "inactive-but-live"
	case missed:
		h.act = "missed"
	}
	conv, _ := app.Conv(m.Channel)
	dm := conv.Kind == "im" || conv.Kind == "mpim" // aK5v yz || LI
	h.status = huddleStatus(app, room, self, dm, missed, systemRedesignApp(app))
	return h, true
}

// huddleStatus is IQT5: the huddle's one-line status.
func huddleStatus(app *core.App, room *richtext.HuddleRoom, self string, dm, missed, redesign bool) string {
	topic := strings.TrimSpace(room.Name)
	youIn := room.Live && containsID(room.Active, self) // b0mi E$
	switch {
	case room.Live && !youIn:
		if topic != "" {
			return topic + " is happening"
		}
		if dm && len(room.Active) > 0 {
			names := make([]string, 0, 2)
			for _, id := range room.Active {
				if len(names) == 2 {
					break
				}
				names = append(names, memberName(app, id))
			}
			n := len(room.Active) // b0mi _5 (Slack members) + l (guests, none here)
			switch {
			case n == 1:
				return names[0] + " is in the huddle."
			case n == 2:
				return names[0] + " and " + names[1] + " are in the huddle."
			case redesign:
				rest := "1 other"
				if n-2 != 1 {
					rest = strconv.Itoa(n-2) + " others"
				}
				return names[0] + ", " + names[1] + ", and " + rest + " are in the huddle."
			}
			return names[0] + " and " + strconv.Itoa(n-1) + " others are in the huddle."
		}
		return "A huddle is happening"
	case room.Live:
		if topic != "" {
			return "You joined " + topic
		}
		return "You joined the huddle"
	case missed:
		return "You missed a huddle"
	case topic != "" && len(room.All) > 0:
		return topic + " happened"
	case topic != "":
		return topic + " happened, but no one joined"
	}
	return "A huddle happened"
}

// memberName is eBtJ's name for a member: the display name, else the
// full name.
func memberName(app *core.App, id string) string {
	if u, ok := app.User(id); ok {
		if u.Name != "" {
			return u.Name
		}
		if u.RealName != "" {
			return u.RealName
		}
	}
	return id
}

func containsID(l []string, id string) bool {
	for _, x := range l {
		if x == id {
			return true
		}
	}
	return false
}

// systemRedesignApp is systemRedesign for an App.
func systemRedesignApp(app *core.App) bool {
	return app != nil && app.Experiment("system_messages_redesign_m1") == "on"
}

// huddleSystemDoc is HuddleMsgText's compact body (joinCompactHuddleMsgText):
// "{status}. {sentence}", or the status alone when there's no sentence,
// the status bold in a thread root (boldHuddleTopicInStatus bolds a
// topic inside it).
func huddleSystemDoc(h huddleMsg, doc richtext.Doc, root bool) richtext.Doc {
	var sentence []richtext.Span
	for i := range doc.Blocks {
		if a := doc.Blocks[i].Att; a != nil && a.Huddle != nil && len(a.Text) > 0 {
			sentence = a.Text[0].Spans
		}
	}
	lead := richtext.Span{Kind: richtext.Text, Text: h.status}
	if root {
		lead.Style |= richtext.Bold
	}
	spans := []richtext.Span{lead}
	if len(sentence) > 0 {
		spans = append(spans, richtext.Span{Kind: richtext.Text, Text: ". "})
		spans = append(spans, sentence...)
	}
	return richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: spans}}}
}

// huddleJoinButton is HuddleMsgJoinButton's compact form in a system row:
// a c-button--small c-button--primary "Join Huddle" (28px, min-width 56,
// padding 0 12px 1px, 13px bold white on base-inv-hgl-2, 4px corners;
// :hover lays base-inv-hgl-2-hover over it) at p, starting the huddle
// through startHuddle with entryPoint huddle_message_button. Returns its
// height.
func (r *row) huddleJoinButton(gtx layout.Context, rc *rowCtx, hm huddleMsg, p image.Point) int {
	c := rc.c
	if r.hmJoin.Clicked(gtx) && hm.room.Channel != "" {
		r.handler.JoinHuddle(hm.room.Channel)
	}
	ts := textStyle{size: 13, weight: theme.Bold, col: rgb(0xffffff), lineH: 27}
	txt := "Join Huddle"
	tw := measure(gtx, rc.th, ts, txt).X
	bw := max(dp(gtx, 56), tw+2*dp(gtx, 12))
	bh := dp(gtx, 28)
	at(gtx, p.X, p.Y, func(gtx layout.Context) layout.Dimensions {
		return r.hmJoin.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			bg := c.HuddleJoin
			if r.hmJoin.Hovered() {
				bg = richview.Over(c.HuddleJoinHover, bg)
			}
			rrect(gtx.Ops, image.Rect(0, 0, bw, bh), dp(gtx, 4), bg)
			at(gtx, (bw-tw)/2, 0, func(gtx layout.Context) layout.Dimensions { return label(gtx, rc.th, ts, txt) })
			pointer.CursorPointer.Add(gtx.Ops)
			return layout.Dimensions{Size: image.Pt(bw, bh)}
		})
	})
	return bh
}

// layoutHuddleAvatar is HuddleMsgAvatar (vjFA, .p-huddle_msg_avatar): a
// 36px tile (corners clamp(4px, 22.222%, 12px)) with the headphones
// glyph, rgba(fg_low,.13) under rgba(fg_max,.7); jade-60 under white
// while the huddle is live or not ended (--live); base-imp under
// content-imp when you missed it (--missed).
func (r *row) layoutHuddleAvatar(gtx layout.Context, rc *rowCtx, s int) layout.Dimensions {
	c := rc.c
	bg, fg := c.HuddleTile, c.Muted70
	switch r.hm.act {
	case "active", "inactive-but-live":
		bg, fg = c.HuddleTileLive, rgb(0xffffff)
	case "missed":
		bg, fg = c.HuddleMissedBg, c.Important
	}
	rad := min(max(dp(gtx, 4), s*2222/10000), dp(gtx, 12))
	rrect(gtx.Ops, image.Rect(0, 0, s, s), rad, bg)
	is := dp(gtx, 20) // .c-icon at its 20px
	at(gtx, (s-is)/2, (s-is)/2, func(gtx layout.Context) layout.Dimensions {
		return icons.Get(icons.Slack("headphones"))(gtx, unit.Dp(20), fg)
	})
	return layout.Dimensions{Size: image.Pt(s, s)}
}

// huddleBadge is renderStatusBadge's p-huddle_msg_status_badge after the
// name: LIVE for an active huddle, MISSED for a missed one (the text only
// with huddles_missed_notification), 11px bold, padding 2px 8px, 14px
// line, 12px corners, 4px after the name, 1px up. Returns its width.
func (r *row) huddleBadge(gtx layout.Context, rc *rowCtx, x, baseline int) int {
	c := rc.c
	var txt string
	var bg, fg color.NRGBA
	switch r.hm.act {
	case "active":
		txt, bg, fg = "LIVE", c.HuddleLiveBadge, c.HuddleLiveBadgeText
	case "missed":
		bg, fg = c.HuddleMissedBg, c.Important
		if app := rc.h.Core(); app != nil && app.Experiment("huddles_missed_notification") == "on" {
			txt = "MISSED"
		}
	default:
		return 0
	}
	ts := textStyle{size: 11, weight: theme.Bold, col: fg, lineH: 14}
	sz := measure(gtx, rc.th, ts, txt)
	x += dp(gtx, 4)
	bw, bh := sz.X+2*dp(gtx, 8), dp(gtx, 14)+2*dp(gtx, 2)
	// inline on the name's baseline, the 11px line's baseline 1px up
	top := baseline - dp(gtx, 2) - dp(gtx, 11) - dp(gtx, 1)
	rrect(gtx.Ops, image.Rect(x, top, x+bw, top+bh), dp(gtx, 12), bg)
	at(gtx, x+dp(gtx, 8), top+dp(gtx, 2), func(gtx layout.Context) layout.Dimensions { return label(gtx, rc.th, ts, txt) })
	return dp(gtx, 4) + bw
}
