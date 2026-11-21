package fakeslack

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Size.SystemDM: the viewer's DM with the Slack system user (SlackUserID,
// "USLACK"), in the shape the real account serves it. Read-only probes of
// one such DM (2026-11-21) printed key names, types and subtypes only; the
// content here is made up.
//
//   - users/info USLACK (systemUserJSON): name "slack", real_name
//     "Slack", is_bot and is_app_user false, profile.always_active true,
//     color 757575, the team's team_id, tz America/Los_Angeles, an
//     enterprise_user with no teams, and every profile image the same SVG
//     on Slack's CDN, SystemUserAvatarURL
//     (a.slack-edge.com/<hash>/img/avatars/slack_system_user/slack_logo_mark.svg).
//   - conversations.info (systemDMInfo): is_im, is_open, is_org_shared and
//     is_shared, priority 0, user USLACK, context_team_id the enterprise,
//     last_read, latest (the newest message), unread_count and
//     unread_count_display. users.conversations and sidebar.dms list it
//     open.
//   - the history: an app posts into it (Hack Club's welcome app does).
//     Every message is subtype bot_message with no user: a bot_id and
//     app_id, a username override (SystemDMUsername), an icons override
//     (only image_48, a file in Slack's S3 bot_icons), a bot_profile with
//     its own name, icons and the app's bot user (user_id), the team, the
//     fallback text and blocks. The older post is an image, a header, a
//     section and actions; the newer one a section. Both are read.
//     slack.Message decodes no "team" or bot_profile "updated", so the
//     history pages leave those two out; the realtime event
//     (PostSystemDMQuiet) carries every key.
//   - client.counts (systemDMCount): the DM's entry from its history and
//     read cursor (conversations.mark moves it), every unread post a
//     mention, as a DM's messages are, unless SetSystemDMMentions(false)
//     has the answer say has_unreads with mention_count 0.
//
// The app's bot user (SystemDMBotUserID) answers users/info as a bot, and
// bots.info answers its bot. Slackbot (USLACKBOT) is a different user
// with a DM of its own.
//
// Clients routed with RouteSlackHosts load the avatar SVG, the posts' S3
// icon and the bot_profile's icons from the fake (asSlackHost,
// serveSystemDMAsset); the UI tests' image cache (Fetch) serves every
// outside URL from the fake anyway.

const (
	// SystemDMID is the viewer's DM with the Slack system user.
	SystemDMID = "D0SYSDM0001"

	// SystemDMAppID is the app that posts into the DM, SystemDMBotID its
	// bot and SystemDMBotUserID the bot's user (bot_profile.user_id).
	SystemDMAppID     = "A0SYSDMAPP1"
	SystemDMBotID     = "B0SYSDMAPP1"
	SystemDMBotUserID = "U0SYSDMAPP1"
	// SystemDMUsername is the username override the posts carry,
	// SystemDMBotName their bot_profile's name.
	SystemDMUsername = "Fake Club Welcome Committee"
	SystemDMBotName  = "fake welcome"

	// SystemUserAvatarURL is every one of USLACK's profile images (24 to
	// 512): one SVG on Slack's CDN. The hash segment is made up.
	SystemUserAvatarURL = "https://a.slack-edge.com/5c3e1f0/img/avatars/slack_system_user/slack_logo_mark.svg"
	// SystemDMIconURL is the posts' icons.image_48, a file in Slack's S3
	// bot_icons bucket.
	SystemDMIconURL = "https://s3-us-west-2.amazonaws.com/slack-files2/bot_icons/2026-11-06/1000000000001_48.png"
)

// SystemDMBotIcon is the bot_profile's icon at px (36, 48 or 72) on
// avatars.slack-edge.com.
func SystemDMBotIcon(px int) string {
	return "https://avatars.slack-edge.com/2026-11-06/1000000000002_" + systemDMIconHash + "_" + strconv.Itoa(px) + ".png"
}

const systemDMIconHash = "0a1b2c3d4e5f60718293"

// systemDMBase is the fixture's clock: 2026-11-06 10:00 UTC, older than the
// generated DMs' messages (as dm_badges.go's).
const systemDMBase = 1793959200

// systemDMState is Size.SystemDM's: the DM's read cursor (set before
// serving) and whether client.counts leaves the mentions out.
type systemDMState struct {
	read       string
	noMentions atomic.Bool
}

// SetSystemDMMentions(false) makes client.counts answer the system DM's
// unread posts with has_unreads true and mention_count 0 (true, the
// default: one mention per unread post). Slack's client reads such an IM
// as one unread message (client-boot applyCountsToChannels:
// mention_count || has_unreads).
func (s *Server) SetSystemDMMentions(on bool) { s.sysDM.noMentions.Store(!on) }

// addSystemDM adds the DM and its two posts (newest first), read up to the
// newest.
func (s *Server) addSystemDM() {
	s.convs = append(s.convs, slack.Conversation{ID: SystemDMID, IsIM: true, IsOpen: true, IsOrgShared: true,
		User: SlackUserID, Created: systemDMBase - 9e5, ContextTeamID: EnterpriseID})
	older := fmt.Sprintf("%d.%06d", systemDMBase, 100)
	newer := fmt.Sprintf("%d.%06d", systemDMBase+3600, 200)
	welcome := "Welcome to Fake Club! A synthetic welcome note."
	tip := "A synthetic follow-up tip from the welcome committee."
	s.hist[SystemDMID] = []slack.Message{
		s.systemDMMessage(newer, tip, systemDMSection("sysdm-tip", tip)),
		s.systemDMMessage(older, welcome, s.systemDMWelcomeBlocks()),
	}
	s.sysDM.read = newer
}

// systemDMWelcomeBlocks are the older post's blocks: an image, a header, a
// section and actions.
func (s *Server) systemDMWelcomeBlocks() json.RawMessage {
	b, _ := json.Marshal([]any{
		map[string]any{"type": "image", "block_id": "sysdm-img", "image_url": s.URL + "img/600x200/sysdm-banner.png",
			"alt_text": "A synthetic welcome banner", "image_width": 600, "image_height": 200, "image_bytes": 12000,
			"is_animated": false, "fallback": "600x200px image"},
		map[string]any{"type": "header", "block_id": "sysdm-head",
			"text": map[string]any{"type": "plain_text", "text": "Welcome to Fake Club!", "emoji": true}},
		map[string]any{"type": "section", "block_id": "sysdm-body",
			"text": map[string]any{"type": "mrkdwn", "text": "A synthetic welcome note: pick the channels you'd like to join.", "verbatim": false}},
		map[string]any{"type": "actions", "block_id": "sysdm-acts", "elements": []any{
			map[string]any{"type": "button", "action_id": "sysdm-start", "value": "start", "style": "primary",
				"text": map[string]any{"type": "plain_text", "text": "Get started", "emoji": true}},
		}},
	})
	return b
}

// systemDMSection is a post's one section block.
func systemDMSection(id, text string) json.RawMessage {
	b, _ := json.Marshal([]any{map[string]any{"type": "section", "block_id": id,
		"text": map[string]any{"type": "mrkdwn", "text": text, "verbatim": false}}})
	return b
}

// systemDMBotProfile is the posts' bot_profile.
func systemDMBotProfile() slack.BotProfile {
	return slack.BotProfile{ID: SystemDMBotID, Name: SystemDMBotName, AppID: SystemDMAppID, UserID: SystemDMBotUserID,
		TeamID: TeamID, Icons: slack.Icons{Image36: SystemDMBotIcon(36), Image48: SystemDMBotIcon(48), Image72: SystemDMBotIcon(72)}}
}

// systemDMMessage is one post as the history serves it.
func (s *Server) systemDMMessage(ts, text string, blocks json.RawMessage) slack.Message {
	bp := systemDMBotProfile()
	return slack.Message{Type: "message", Subtype: "bot_message", TS: ts, Text: text, BotID: SystemDMBotID, AppID: SystemDMAppID,
		Username: SystemDMUsername, Icons: &slack.Icons{Image48: SystemDMIconURL}, BotProfile: &bp, Blocks: blocks}
}

// systemDMEvent is the realtime message event for a post: every key the
// real one has.
func systemDMEvent(ts, text string, blocks json.RawMessage) map[string]any {
	return map[string]any{"type": "message", "subtype": "bot_message", "channel": SystemDMID, "text": text,
		"ts": ts, "event_ts": ts, "username": SystemDMUsername, "icons": map[string]any{"image_48": SystemDMIconURL},
		"bot_id": SystemDMBotID, "app_id": SystemDMAppID, "team": TeamID, "blocks": blocks,
		"bot_profile": map[string]any{"id": SystemDMBotID, "deleted": false, "name": SystemDMBotName, "updated": systemDMBase,
			"app_id": SystemDMAppID, "user_id": SystemDMBotUserID, "team_id": TeamID,
			"icons": map[string]any{"image_36": SystemDMBotIcon(36), "image_48": SystemDMBotIcon(48), "image_72": SystemDMBotIcon(72)}}}
}

// PostSystemDMQuiet adds a post from the app to the system DM's history
// (newest, one section block with text) and returns its ts and the
// realtime message event the socket would carry, unpushed: PostQuiet's
// counterpart for the DM (inject ev with core.Inject, or Push it with
// realtime on).
func (s *Server) PostSystemDMQuiet(text string) (ts string, ev map[string]any) {
	s.histMu.Lock()
	h := s.histLocked(SystemDMID)
	now := time.Now()
	sec, us := now.Unix(), int64(now.Nanosecond()/1000)
	if len(h) > 0 { // after the newest, whatever the clock says
		if a, b, ok := strings.Cut(h[0].TS, "."); ok {
			hs, _ := strconv.ParseInt(a, 10, 64)
			hu, _ := strconv.ParseInt(b, 10, 64)
			if hs > sec || hs == sec && hu >= us {
				sec, us = hs+1, 0
			}
		}
	}
	ts = fmt.Sprintf("%d.%06d", sec, us)
	blocks := systemDMSection("sysdm-"+ts, text)
	s.hist[SystemDMID] = append([]slack.Message{s.systemDMMessage(ts, text, blocks)}, h...)
	s.histMu.Unlock()
	return ts, systemDMEvent(ts, text, blocks)
}

// systemDMCursor is the DM's read cursor: conversations.mark's, else the
// fixture's.
func (s *Server) systemDMCursor() string {
	if m := s.Marked(SystemDMID); m != "" {
		return m
	}
	return s.sysDM.read
}

// systemDMUnread is the DM's newest ts and how many posts are newer than
// lastRead.
func (s *Server) systemDMUnread(lastRead string) (latest string, unread int) {
	s.histMu.Lock()
	defer s.histMu.Unlock()
	latest = lastRead
	for _, m := range s.hist[SystemDMID] {
		if slackTSLess(latest, m.TS) {
			latest = m.TS
		}
		if slackTSLess(lastRead, m.TS) && m.User != SelfID {
			unread++
		}
	}
	return latest, unread
}

// systemDMCount is client.counts' entry for the system DM (any other
// conversation's e comes back as it was).
func (s *Server) systemDMCount(c slack.Conversation, e slack.CountEntry) slack.CountEntry {
	if !s.Size.SystemDM || c.ID != SystemDMID {
		return e
	}
	lr := s.systemDMCursor()
	latest, unread := s.systemDMUnread(lr)
	mentions := unread
	if s.sysDM.noMentions.Load() {
		mentions = 0
	}
	return slack.CountEntry{ID: SystemDMID, LastRead: lr, Latest: latest, HasUnreads: unread > 0, MentionCount: mentions}
}

// systemDMInfo is conversations.info's channel for the system DM.
func (s *Server) systemDMInfo() map[string]any {
	lr := s.systemDMCursor()
	_, unread := s.systemDMUnread(lr)
	var latest any
	s.histMu.Lock()
	if h := s.hist[SystemDMID]; len(h) > 0 {
		latest = h[0]
	}
	s.histMu.Unlock()
	return map[string]any{"id": SystemDMID, "created": systemDMBase - 900000, "is_archived": false, "is_frozen": false,
		"is_im": true, "is_open": true, "is_org_shared": true, "is_shared": true,
		"is_slackbot_channel_eligible": false, "is_slackbot_channel_enabled": false,
		"context_team_id": EnterpriseID, "last_read": lr, "latest": latest, "priority": 0,
		"unread_count": unread, "unread_count_display": unread, "updated": int64(systemDMBase) * 1000, "user": SlackUserID}
}

// systemUserJSON is users/info's USLACK with every key the real answer
// has.
func systemUserJSON() map[string]any {
	prof := map[string]any{"always_active": true, "avatar_hash": "0a1b2c3d4e5f", "display_name": "Slack",
		"display_name_normalized": "Slack", "fields": map[string]any{}, "first_name": "Slack", "last_name": "", "phone": "",
		"real_name": "Slack", "real_name_normalized": "Slack", "skype": "", "status_emoji": "",
		"status_emoji_display_info": []any{}, "status_expiration": 0, "status_text": "", "status_text_canonical": "",
		"team": TeamID, "title": ""}
	for _, px := range []int{24, 32, 48, 72, 192, 512} {
		prof["image_"+strconv.Itoa(px)] = SystemUserAvatarURL
	}
	return map[string]any{"id": SlackUserID, "team_id": TeamID, "name": "slack", "deleted": false, "color": "757575",
		"real_name": "Slack", "tz": "America/Los_Angeles", "tz_label": "Pacific Standard Time", "tz_offset": -28800,
		"profile": prof, "is_admin": false, "is_owner": false, "is_primary_owner": false, "is_restricted": false,
		"is_ultra_restricted": false, "is_bot": false, "is_app_user": false, "is_email_confirmed": false,
		"updated": systemDMBase, "who_can_share_contact_card": "EVERYONE",
		"enterprise_user": map[string]any{"id": SlackUserID, "enterprise_id": EnterpriseID, "enterprise_name": "Fake Club",
			"is_admin": false, "is_owner": false, "is_primary_owner": false, "teams": []any{}}}
}

// systemDMBotUser is the app's bot user.
func systemDMBotUser() slack.User {
	img := SystemDMBotIcon(72)
	return slack.User{ID: SystemDMBotUserID, TeamID: TeamID, Name: "fakewelcome", RealName: SystemDMBotName, IsBot: true,
		Updated: systemDMBase, Profile: slack.Profile{RealName: SystemDMBotName, Team: TeamID, BotID: SystemDMBotID,
			APIAppID: SystemDMAppID, Fields: json.RawMessage(`{}`), Image24: img, Image32: img, Image48: SystemDMBotIcon(48),
			Image72: img, Image192: img, Image512: img}}
}

// systemDMUserJSON is users/info's answer for the fixture's users: USLACK
// with the real answer's keys, the bot user as slack.User has it.
func (s *Server) systemDMUserJSON(id string) (any, bool) {
	if !s.Size.SystemDM {
		return nil, false
	}
	switch id {
	case SlackUserID:
		return systemUserJSON(), true
	case SystemDMBotUserID:
		return systemDMBotUser(), true
	}
	return nil, false
}

// systemDMUser is userByID's answer for the fixture's users.
func (s *Server) systemDMUser(id string) (slack.User, bool) {
	raw, ok := s.systemDMUserJSON(id)
	if !ok {
		return slack.User{}, false
	}
	if u, isUser := raw.(slack.User); isUser {
		return u, true
	}
	b, _ := json.Marshal(raw)
	var u slack.User
	if json.Unmarshal(b, &u) != nil {
		return slack.User{}, false
	}
	return u, true
}

// handleSystemDM answers conversations.info for the system DM and
// bots.info for its app's bot.
func (s *Server) handleSystemDM(method string, f url.Values) (any, bool) {
	if !s.Size.SystemDM {
		return nil, false
	}
	switch method {
	case "conversations.info":
		if f.Get("channel") == SystemDMID {
			return ok("channel", s.systemDMInfo()), true
		}
	case "bots.info":
		for _, id := range strings.Split(f.Get("bots")+","+f.Get("bot"), ",") {
			if id == SystemDMBotID {
				return ok("bots", []slack.BotProfile{systemDMBotProfile()}), true
			}
		}
	}
	return nil, false
}

// systemUserSVG stands in for USLACK's avatar: a made-up four-square mark.
const systemUserSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 54 54">` +
	`<rect width="54" height="54" rx="12" fill="#ffffff"/>` +
	`<rect x="8" y="8" width="17" height="17" rx="4" fill="#36c5f0"/>` +
	`<rect x="29" y="8" width="17" height="17" rx="4" fill="#2eb67d"/>` +
	`<rect x="8" y="29" width="17" height="17" rx="4" fill="#e01e5a"/>` +
	`<rect x="29" y="29" width="17" height="17" rx="4" fill="#ecb22e"/></svg>`

// serveSystemDMAsset answers a routed request (asSlackHost) for the
// fixture's images on Slack's hosts: USLACK's avatar SVG on
// a.slack-edge.com, the posts' icon in Slack's S3 bot_icons and the
// bot_profile's icons on avatars.slack-edge.com (PNGs from the fake's
// image generator). It reports whether it answered.
func (s *Server) serveSystemDMAsset(w http.ResponseWriter, r *http.Request, host string) bool {
	if !s.Size.SystemDM {
		return false
	}
	p := r.URL.Path
	switch {
	case host == "a.slack-edge.com" && strings.Contains(p, "/img/avatars/slack_system_user/") && strings.HasSuffix(p, ".svg"):
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write([]byte(systemUserSVG))
	case host == "s3-us-west-2.amazonaws.com" && strings.HasPrefix(p, "/slack-files2/bot_icons/"),
		host == "avatars.slack-edge.com" && strings.Contains(p, "_"+systemDMIconHash+"_"):
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/img/72x72/sysdm-icon.png"
		s.img(w, r2)
	default:
		return false
	}
	return true
}
