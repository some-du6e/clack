package views

// Slack component boundaries and CSS identities of the views' widgets,
// for Taut plugins (docs/PLUGINS-components.md §1.7, §5). Unpatched and
// unstyled, every boundary is one atomic load: props are only built when
// a plugin patches the name.

import (
	"image"
	"image/color"
	"strings"

	"gioui.org/font"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc/css"
)

var (
	// Activity page (§2.3 activity rows; StreamerMode's blur rules).
	actPageClass = slackc.Class("p-activity_ia p-activity_ia4_page p-activity_page")
	actItemClass = slackc.Class("c-message_kit__hover p-activity_ia4_page__item", "data-qa", "activity-item-container").
			Mods(actItemMods...)
	actMsgClass  = slackc.Class("p-activity_item__message", "data-qa", "activity-item-message")
	actNameClass = slackc.Class("c-inline_channel_entity c-base_inline_entity c-message__sender c-message_kit__sender p-activity_page__item__link",
		"data-qa", "inline_channel_entity").Tag("span")
	actAvatarClass = slackc.Class("activity_row_content__avatar_and_icon").Mods("activity_row_content__avatar_and_icon_small")

	// Threads view.
	thrViewClass   = slackc.Class("p-threads_view")
	thrItemClass   = slackc.Class("c-virtual_list__item")
	thrMsgClass    = slackc.Class("c-message_kit__background c-message_kit__thread_message").Mods("c-message_kit__background--hovered")
	thrSenderClass = slackc.Class("c-message__sender c-message_kit__sender", "data-qa", "message_sender").Tag("span")

	// DMs list.
	dmRowClass   = slackc.Class("p-activity_ia4_page__item", "data-qa", "dms_channel").Mods("p-activity_ia4_page__item--selected")
	dmNameClass  = slackc.Class("p-activity_ia4_page__item__name", "data-qa", "dms-channel-sender-name").Tag("span")
	dmMsgClass   = slackc.Class("p-activity_ia4_page__item__message", "data-qa", "activity-item-message")
	dmStackClass = slackc.Class("c-base_icon_image_stacked")

	// BasicSelect (directories, channel details).
	selectClass = slackc.Class("c-basic-select")
)

// actItemMods are the ActivityItem row's optional classes: its own states,
// then the classes plugins add through the className prop (StreamerMode).
// Bit i of the Enter mods selects actItemMods[i].
var actItemMods = []string{
	"c-message_kit__hover--hovered",
	"p-activity_ia4_page__item--selected",
	"taut-streamer-mode__activity",
	"taut-streamer-mode__activity--dm",
	"taut-streamer-mode__activity--group",
	"taut-streamer-mode__activity--private",
}

const (
	actModHovered uint64 = 1 << iota
	actModSelected
)

// classMods maps a className prop to the mod bits of the known classes in
// mods. Patched path only.
func classMods(className string, mods []string) uint64 {
	var bits uint64
	for _, c := range strings.Fields(className) {
		for i, m := range mods {
			if c == m {
				bits |= 1 << uint(i)
			}
		}
	}
	return bits
}

func hoverState(h bool) css.State {
	if h {
		return css.StateHover
	}
	return 0
}

// msgProps is a core.Message in Slack's message shape (the msg prop of
// ActivityItem, ThreadRootGeneric, ThreadSenderAndTimestampGeneric…).
func msgProps(m *core.Message) slackc.Props {
	if m == nil {
		return nil
	}
	p := slackc.Props{"type": "message", "channel": m.Channel, "ts": m.TS, "text": m.Text}
	set := func(k, v string) {
		if v != "" {
			p[k] = v
		}
	}
	set("user", m.User)
	set("bot_id", m.BotID)
	set("subtype", m.Subtype)
	set("thread_ts", m.ThreadTS)
	set("latest_reply", m.LatestReply)
	set("client_msg_id", m.ClientMsgID)
	set("username", m.Name)
	if m.Avatar != "" {
		p["icons"] = slackc.Props{"image_48": m.Avatar}
	}
	if m.EditedTS != "" {
		p["edited"] = slackc.Props{"ts": m.EditedTS}
	}
	if m.ReplyCount > 0 {
		p["reply_count"] = m.ReplyCount
		p["reply_users"] = m.ReplyUsers
		p["reply_users_count"] = len(m.ReplyUsers)
	}
	if len(m.Reactions) > 0 {
		rs := make([]any, len(m.Reactions))
		for i, r := range m.Reactions {
			rs[i] = slackc.Props{"name": r.Name, "count": r.Count, "users": r.Users}
		}
		p["reactions"] = rs
	}
	return p
}

// msgOverride applies a plugin's msg prop (ShowRealUser: a relay bot's
// message rewritten to its real sender, bot fields dropped) to m: the
// fields Clack draws the sender's name and avatar from.
func msgOverride(m *core.Message, mp slackc.Props) core.Message {
	out := *m
	out.User = mp.StrOr("user", "")
	out.BotID = mp.StrOr("bot_id", "")
	out.Subtype = mp.StrOr("subtype", "")
	out.Name = mp.StrOr("username", "")
	out.Avatar = ""
	if ic := mp.Map("icons"); ic != nil {
		for _, k := range [...]string{"image_48", "image_72", "image_36", "image_64", "image_32"} {
			if s := ic.StrOr(k, ""); s != "" {
				out.Avatar = s
				break
			}
		}
	}
	if out.BotID == "" && m.BotID != "" {
		out.IsBot = false
	}
	return out
}

// blurPara draws one line of s as a privacy blur (Slack's
// ActivityPrivacyBlurredName in its blurred state, CSS filter: blur):
// the text is smeared over a few px at low alpha so its length and
// colour show but no glyph reads.
func (b *base) blurPara(gtx C, size, lh unit.Sp, w font.Weight, col color.NRGBA, s string) D {
	gtx.Constraints.Min.Y = 0
	r := max(1, gtx.Dp(3))
	h := max(1, r/2)
	col.A = uint8(uint32(col.A) * 28 / 100)
	var d D
	for _, o := range [...]image.Point{{-r, 0}, {r, 0}, {0, -h}, {0, h}, {-h, -h}, {h, h}, {h, -h}, {-h, h}, {}} {
		st := op.Offset(o).Push(gtx.Ops)
		d = b.para(gtx, size, lh, w, col, 1, s)
		st.Pop()
	}
	return d
}
