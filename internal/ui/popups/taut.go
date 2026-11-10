package popups

// Taut plugin boundaries owned by popups (docs/PLUGINS-components.md §Index
// rows 3, 4, 28, 29, 55–60, 67–69): Slack-shaped props for the natives,
// and the class tags of §2.3. Everything here is free while no plugin
// patches a name and no CSS is set (slackc's fast paths).

import (
	"strconv"
	"strings"
	"sync/atomic"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// Class tags (§2.3) of popups' generic widgets. Feature popups declare
// theirs next to the widget (menu_template.go, profile_card.go,
// emoji_picker.go, search_popover.go).
var (
	popoverClass    = slackc.Class("c-popover c-popover--fade", "role", "dialog")
	modalClass      = slackc.Class("ReactModal__Content c-sk-modal c-sk-modal--fixed", "role", "dialog", "data-qa", "sk-modal")
	dtDropdownClass = slackc.Class("c-date_picker__dropdown", "role", "dialog")
	buttonClass     = slackc.Class("c-button", "type", "button").Tag("button").
			Mods("c-button--outline", "c-button--primary", "c-button--danger", "c-button--disabled",
			"c-button--small", "c-button--medium", "c-button--icon")
	inputTextClass   = slackc.Class("c-input_text", "type", "text").Tag("input")
	statusInputClass = slackc.Class("p-custom_status_modal__input")
)

// instSeq hands out "$key"s so each popup instance keeps its plugin hook
// state across prop changes.
var instSeq atomic.Int64

func nextKey(prefix string) string {
	return prefix + ":" + strconv.FormatInt(instSeq.Add(1), 10)
}

// slackMember is u as a Slack member object (state.members[id]).
func slackMember(u core.User) slackc.Props {
	presence := u.Presence
	if presence == "" {
		presence = "away"
	}
	img := func(s string) string {
		if s == "" {
			return u.Avatar
		}
		return s
	}
	prof := slackc.Props{
		"display_name":      u.Name,
		"real_name":         u.RealName,
		"title":             u.Title,
		"pronouns":          u.Pronouns,
		"image_24":          u.Avatar,
		"image_32":          u.Avatar,
		"image_48":          u.Avatar,
		"image_72":          u.Avatar,
		"image_192":         img(u.Avatar192),
		"image_512":         img(u.Avatar512),
		"status_text":       u.StatusText,
		"status_emoji":      u.StatusEmoji,
		"status_expiration": u.StatusExp,
	}
	if u.StatusEmojiURL != "" {
		prof["status_emoji_display_info"] = []any{slackc.Props{"emoji_name": strings.Trim(u.StatusEmoji, ":"), "display_url": u.StatusEmojiURL}}
	}
	if u.Email != "" {
		prof["email"] = u.Email
	}
	return slackc.Props{
		"id":            u.ID,
		"name":          u.Handle,
		"real_name":     u.RealName,
		"deleted":       u.Deleted,
		"is_bot":        u.IsBot,
		"is_admin":      u.IsAdmin,
		"is_owner":      u.IsOwner,
		"is_restricted": u.Guest,
		"color":         u.Color,
		"tz":            u.TZ,
		"tz_label":      u.TZLabel,
		"tz_offset":     u.TZOffset,
		"presence":      presence,
		"profile":       prof,
	}
}

// slackMessage is m with Slack's message fields (state.messages[ch][ts]).
func slackMessage(m *core.Message) slackc.Props {
	p := slackc.Props{
		"type":    "message",
		"ts":      m.TS,
		"channel": m.Channel,
		"text":    m.Text,
	}
	if m.User != "" {
		p["user"] = m.User
	}
	if m.BotID != "" {
		p["bot_id"] = m.BotID
		if m.Name != "" {
			p["username"] = m.Name
		}
	}
	if b := m.BotProfile; b != nil {
		p["bot_profile"] = slackc.Props{"id": b.ID, "name": b.Name, "app_id": b.AppID, "user_id": b.UserID,
			"team_id": b.TeamID, "deleted": b.Deleted}
	}
	if m.Subtype != "" {
		p["subtype"] = m.Subtype
	}
	if m.ThreadTS != "" {
		p["thread_ts"] = m.ThreadTS
	}
	if m.ReplyCount > 0 {
		p["reply_count"] = m.ReplyCount
		if m.LatestReply != "" {
			p["latest_reply"] = m.LatestReply
		}
		if len(m.ReplyUsers) > 0 {
			p["reply_users"] = m.ReplyUsers
		}
	}
	if m.Edited || m.EditedTS != "" {
		p["edited"] = slackc.Props{"ts": m.EditedTS}
	}
	if m.Permalink != "" {
		p["permalink"] = m.Permalink
	}
	if m.ClientMsgID != "" {
		p["client_msg_id"] = m.ClientMsgID
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

// asProps returns v as Props when it is an object.
func asProps(v any) slackc.Props {
	switch m := v.(type) {
	case slackc.Props:
		return m
	case map[string]any:
		return slackc.Props(m)
	}
	return nil
}

// slotOr lays out the slot a patched parent handed back in prop key (so
// the plugin's context reaches it); otherwise it renders the boundary
// name on its own. A slot that came back empty rendered null (Slick's
// LocalTime hides Slack's LocalTime inside its own rows): nothing draws.
func slotOr(gtx layout.Context, parent slackc.Props, key, name string, props func() slackc.Props, native slackc.Native) layout.Dimensions {
	if e, ok := parent.Elem(key); ok {
		return slackc.LayoutElement(gtx, e)
	}
	return slackc.RenderLazy(gtx, name, props, native)
}

// childrenOr is slotOr for the "children" prop. Props that came from a
// plugin's <Original> carry the rendered children as an Element, and none
// at all when they rendered null (then nothing draws); the native's own
// props (no plugin, or its tree not here yet) carry the Slot.
func childrenOr(gtx layout.Context, parent slackc.Props, name string, props func() slackc.Props, native slackc.Native) layout.Dimensions {
	switch c := parent["children"].(type) {
	case slackc.Element:
		return slackc.LayoutElement(gtx, c)
	case nil:
		if parent != nil {
			return layout.Dimensions{}
		}
	}
	return slackc.RenderLazy(gtx, name, props, native)
}
