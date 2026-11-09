package core

// Message sender identity as Slack's web client resolves it (CDN build
// a84662d2d135, 2026-11-08; module ids from gantry-v2-async-client-boot-*
// and client-boot):
//
//   - getMsgSenderEntity (33X/): a message with a user is that member's
//     ({userId, botId, appId}, no username); one without is
//     {botId, username}.
//   - getMessageSenderNameFromState (4ubN): member ? the member's name :
//     botId && username ? username : bot ? bot.name : …username. So
//     username only names user-less messages.
//   - ConnectedBaseAvatar (ztjr) + getMemberProfileImage (yEEQ): with a
//     userId the avatar is the member's picture (avatar_hash on
//     ca.slack-edge.com); msg.icons and bot_profile are not read. Without
//     one: msg.icons || bot_profile.icons, then the bot's own icons
//     (Workflow Builder's bot always uses its own); icons.emoji draws that
//     emoji; else image_<n>, n the smallest size >= the one drawn
//     (getIconSizeToFit, wqqh); no images at all: ava_0002, the blue
//     person silhouette.
//   - display_as_bot appears nowhere in the client.
//
// The sender badge is in badge.go, grouping in Adjacent.

import (
	"sort"
	"strconv"
	"strings"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// SenderIcons are a message's icons (icon_emoji / icon_url) or a
// bot_profile's.
type SenderIcons struct {
	Emoji                              string // ":robot_face:" (icon_emoji)
	Image36, Image48, Image64, Image72 string
}

func iconsOf(ic *slack.Icons) *SenderIcons {
	if ic == nil || *ic == (slack.Icons{}) {
		return nil
	}
	return &SenderIcons{Emoji: ic.Emoji, Image36: ic.Image36, Image48: ic.Image48, Image64: ic.Image64, Image72: ic.Image72}
}

// Empty reports whether ic has neither an emoji nor an image.
func (ic *SenderIcons) Empty() bool { return ic == nil || *ic == (SenderIcons{}) }

// SameIcons is Slack's isSameBotIcon: emoji and every image equal (two
// missing icon sets are the same).
func SameIcons(a, b *SenderIcons) bool {
	var x, y SenderIcons
	if a != nil {
		x = *a
	}
	if b != nil {
		y = *b
	}
	return x == y
}

// Pick is getIconSizeToFit over the images present: the smallest
// image_<n> with n >= px, else the largest. "" when there are none.
func (ic *SenderIcons) Pick(px int) string {
	if ic == nil {
		return ""
	}
	type sz struct {
		n   int
		url string
	}
	var have []sz
	for _, s := range []sz{{36, ic.Image36}, {48, ic.Image48}, {64, ic.Image64}, {72, ic.Image72}} {
		if s.url != "" {
			have = append(have, s)
		}
	}
	if len(have) == 0 {
		return ""
	}
	sort.Slice(have, func(i, j int) bool { return have[i].n < have[j].n })
	for _, s := range have {
		if s.n >= px {
			return s.url
		}
	}
	return have[len(have)-1].url
}

// SenderIconsOf reads icons from a JSON-shaped map (a plugin's msg.icons).
func SenderIconsOf(m map[string]any) *SenderIcons {
	str := func(k string) string { s, _ := m[k].(string); return s }
	ic := &SenderIcons{Emoji: str("emoji"), Image36: str("image_36"), Image48: str("image_48"), Image64: str("image_64"), Image72: str("image_72")}
	if ic.Empty() {
		return nil
	}
	return ic
}

// Map is ic as Slack's icons object (nil when empty).
func (ic *SenderIcons) Map() map[string]any {
	if ic.Empty() {
		return nil
	}
	m := map[string]any{}
	for k, v := range map[string]string{"emoji": ic.Emoji, "image_36": ic.Image36, "image_48": ic.Image48, "image_64": ic.Image64, "image_72": ic.Image72} {
		if v != "" {
			m[k] = v
		}
	}
	return m
}

// DefaultBotAvatar is Slack's avatar for a bot with no icons at all
// (getMemberProfileImage's isBot fallback: ava_0002, a white person on
// blue; the web client bundles it, this is the same image on the CDN).
const DefaultBotAvatar = "https://a.slack-edge.com/df10d/img/avatars/ava_0002-72.png"

// defaultBotAvatarAt is DefaultBotAvatar at Slack's sizes (24 32 48 72 192).
func defaultBotAvatarAt(px int) string {
	n := 192
	for _, s := range []int{24, 32, 48, 72, 192} {
		if s >= px {
			n = s
			break
		}
	}
	return "https://a.slack-edge.com/df10d/img/avatars/ava_0002-" + strconv.Itoa(n) + ".png"
}

// workflowBuilderApps are Workflow Builder's app ids (vn40: O on GovSlack,
// U elsewhere); its messages take the bot's icons over the message's.
var workflowBuilderApps = map[string]bool{"A01126R8Y2X": true, "A0112L5RZ5E": true}

// avatarPx is the avatar size Clack picks images for: a 36 px message
// avatar on a 2x display (Slack's imageUrl2x).
const avatarPx = 72

// senderOf fills v's sender fields from m as Slack's message sender reads
// them (see the top of this file).
func (a *App) senderOf(v *Message, m *slack.Message) {
	v.Username = m.Username
	v.Icons = iconsOf(m.Icons)
	if v.BotProfile != nil {
		if ic := iconsOf(&m.BotProfile.Icons); ic != nil {
			v.BotProfile.Icons = *ic
		}
	}
	if m.User == "" && (m.BotID != "" || m.BotProfile != nil || m.Subtype == "bot_message") {
		v.IsBot = true
	}
	if m.User != "" {
		return // the member's name and picture (the UI reads the user)
	}
	// the bot: the store's (bots.info), else the message's bot_profile
	var bot *botIdentity
	if m.BotID != "" {
		if b, ok := a.S.Bot(m.BotID); ok {
			bot = &botIdentity{name: b.Name, icon: b.Icon, appID: b.AppID}
		} else if m.BotProfile == nil {
			a.S.NoteMissingBot(m.BotID) // resolved in the background (names event)
		}
	}
	if bot == nil && m.BotProfile != nil {
		bot = &botIdentity{name: m.BotProfile.Name, appID: m.BotProfile.AppID}
	}
	switch {
	case m.BotID != "" && m.Username != "":
		v.Name = m.Username
	case bot != nil && bot.name != "":
		v.Name = bot.name
	case m.BotID == "" && m.Username != "":
		v.Name = m.Username
	case m.BotProfile != nil:
		v.Name = m.BotProfile.Name
	}
	v.Avatar = a.botAvatar(v.Icons, v.BotProfile, bot)
}

type botIdentity struct{ name, icon, appID string }

// botAvatar is ConnectedBaseAvatar's picture for a user-less sender.
func (a *App) botAvatar(msgIcons *SenderIcons, bp *BotProfile, bot *botIdentity) string {
	ic := msgIcons
	if ic.Empty() && bp != nil && !bp.Icons.Empty() {
		bi := bp.Icons
		ic = &bi
	}
	if bot != nil && bot.icon != "" && (ic.Empty() || workflowBuilderApps[bot.appID]) {
		ic = &SenderIcons{Image72: bot.icon}
	}
	if ic != nil && ic.Emoji != "" {
		if u := a.emojiURL(ic.Emoji); u != "" {
			return u
		}
	}
	if u := ic.Pick(avatarPx); u != "" {
		return u
	}
	if ic.Empty() {
		return defaultBotAvatarAt(avatarPx)
	}
	return "" // an emoji Clack can't draw yet: Slack's emoji avatar
}

// emojiURL is the image of an emoji name (":name:" or "name", skin tones
// kept as Slack sends them).
func (a *App) emojiURL(name string) string {
	u, _, ok := a.S.Emoji(strings.Trim(name, ":"))
	if !ok {
		return ""
	}
	return u
}

// Adjacent is Slack's isAdjacentMessage sender rule (Ag38): whether msg,
// right after prev, continues prev's group. The time, day, subtype and
// reply rules are the list's (messages.groupable).
//
//   - Two different users never group (msg from Slackbot is exempt).
//   - A message with a user groups with the same user, whatever its
//     username, icons or bot_id.
//   - A user-less bot message groups with the same bot_id when its
//     username (if any) and msg.icons match prev's.
//   - Otherwise a username-only message groups with the same username.
func Adjacent(prev, msg *Message) bool {
	if prev.User != "" && msg.User != "" && msg.User != "USLACKBOT" && prev.User != msg.User {
		return false
	}
	if msg.User != "" {
		return msg.User == prev.User
	}
	prevUsername := prev.Username
	if prev.User != "" {
		prevUsername = "" // a member's sender entity has no username
	}
	if msg.BotID != "" {
		if msg.BotID != prev.BotID {
			return false
		}
		return (msg.Username == "" || msg.Username == prevUsername) && SameIcons(msg.Icons, prev.Icons)
	}
	return msg.Username != "" && msg.Username == prevUsername
}

// pinnedInfoOf is a pin_added event's pinned_info: the item message's own
// when it carries one, else {channel, the pinner, when}.
func pinnedInfoOf(ch string, own *slack.PinnedInfo, by string, created int64, eventTS string) *slack.PinnedInfo {
	if own != nil && own.PinnedBy != "" {
		return own
	}
	if created == 0 && eventTS != "" {
		if f, err := strconv.ParseFloat(eventTS, 64); err == nil {
			created = int64(f)
		}
	}
	return &slack.PinnedInfo{Channel: ch, PinnedBy: by, PinnedTS: created}
}
