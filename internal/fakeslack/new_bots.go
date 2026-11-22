package fakeslack

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// Size.NewBots: bot users and apps in the shapes of the ones in Hack
// Club's #new-bots (C0BKP49D58E, read 2026-11-22 through the probe for
// ids and flags only, never message text; every name and id here is made
// up). All seven there are app bot users (is_bot, no is_workflow_bot),
// in three shapes:
//
//   - stale: the member's profile.bot_id is a bot bots.info no longer
//     finds (bot_not_found) for an app that's still installed (five of
//     the seven). apps.profile.get answers that bot alone as the org's
//     app, not installed (team_id the enterprise, no auth); with
//     bot_home_team the workspace, installed but still with no auth or
//     config (so not active: "This app is unavailable"); with app= and
//     no bot, installed with its installer (auth.created_by).
//   - two bots: the member's profile.bot_id is stale as above, but its
//     messages carry the app's current bot_id (bots.info finds it, and
//     apps.profile.get answers it installed with its installer), with a
//     bot_profile of image_36/48/72 and the workspace's team_id. No
//     app_card_color.
//   - current: profile.bot_id is the app's live bot (installed, active,
//     one slash command), always_active.
//
// Their App Homes differ as there: home tab off with messages read-only,
// both tabs on, messages on and read-only.
const (
	NewBotsStaleUserID = "U0NBSTALE1" // "Shelf Keeper"
	NewBotsStaleBotID  = "B0NBSTALE0" // its profile.bot_id: bots.info bot_not_found
	NewBotsStaleAppID  = "A0NBSTALE1"
	NewBotsTwoUserID   = "U0NBTWOBOT" // "Who Posts"
	NewBotsTwoOldBotID = "B0NBTWOOLD" // its profile.bot_id: stale
	NewBotsTwoBotID    = "B0NBTWONEW" // the bot_id its messages carry
	NewBotsTwoAppID    = "A0NBTWOBOT"
	NewBotsLiveUserID  = "U0NBLIVE01" // "Fish Finder"
	NewBotsLiveBotID   = "B0NBLIVE01"
	NewBotsLiveAppID   = "A0NBLIVE01"
)

// newBotsUsers are Size.NewBots' bot users.
func (s *Server) newBotsUsers(prof func(display, real, title, n string) slack.Profile) []slack.User {
	if !s.Size.NewBots {
		return nil
	}
	st := prof("", "Shelf Keeper", "", "nbstale")
	st.BotID, st.APIAppID = NewBotsStaleBotID, NewBotsStaleAppID
	two := prof("", "Who Posts", "", "nbtwo")
	two.BotID, two.APIAppID = NewBotsTwoOldBotID, NewBotsTwoAppID
	two.ImageOriginal = ""
	live := prof("", "Fish Finder", "", "nblive")
	live.BotID, live.APIAppID = NewBotsLiveBotID, NewBotsLiveAppID
	return []slack.User{
		{ID: NewBotsStaleUserID, TeamID: TeamID, Name: "shelfkeeper", RealName: "Shelf Keeper", IsBot: true, Profile: st},
		{ID: NewBotsTwoUserID, TeamID: TeamID, Name: "whoposts", RealName: "Who Posts", IsBot: true, Profile: two},
		{ID: NewBotsLiveUserID, TeamID: TeamID, Name: "fishfinder", RealName: "Fish Finder", IsBot: true, Profile: live, Presence: "active"},
	}
}

// NewBotsTwoBotProfile is the bot_profile the two-bot app's messages
// carry (its current bot, not the member's profile.bot_id).
func (s *Server) NewBotsTwoBotProfile() slack.BotProfile {
	return s.newBotsBot(NewBotsTwoBotID)
}

func (s *Server) newBotsBot(id string) slack.BotProfile {
	img := func(px int, n string) string {
		return s.URL + "img/" + strconv.Itoa(px) + "x" + strconv.Itoa(px) + "/" + n + ".png"
	}
	switch id {
	case NewBotsTwoBotID:
		return slack.BotProfile{ID: id, Name: "Who Posts", AppID: NewBotsTwoAppID, UserID: NewBotsTwoUserID, TeamID: TeamID,
			Icons: slack.Icons{Image36: img(36, "nbtwo"), Image48: img(48, "nbtwo"), Image72: img(72, "nbtwo")}}
	case NewBotsLiveBotID:
		return slack.BotProfile{ID: id, Name: "Fish Finder", AppID: NewBotsLiveAppID, UserID: NewBotsLiveUserID, TeamID: TeamID,
			Icons: slack.Icons{Image36: img(36, "nblive"), Image48: img(48, "nblive"), Image72: img(72, "nblive")}}
	}
	return slack.BotProfile{}
}

// handleNewBots answers bots.info and apps.profile.get for Size.NewBots
// the way Slack's server does for the #new-bots shapes.
func (s *Server) handleNewBots(method string, f url.Values) (any, bool) {
	if !s.Size.NewBots {
		return nil, false
	}
	switch method {
	case "bots.info":
		var bots []slack.BotProfile
		mine := false
		for _, id := range strings.Split(f.Get("bots")+","+f.Get("bot"), ",") {
			switch id {
			case NewBotsTwoBotID, NewBotsLiveBotID:
				bots, mine = append(bots, s.newBotsBot(id)), true
			case NewBotsStaleBotID, NewBotsTwoOldBotID:
				mine = true
			}
		}
		switch {
		case len(bots) > 0:
			return ok("bots", bots), true
		case mine:
			return map[string]any{"ok": false, "error": "bot_not_found"}, true
		}
	case "apps.profile.get":
		if p, found := s.newBotsProfile(f.Get("bot"), f.Get("app"), f.Get("bot_home_team")); found {
			return ok("app_profile", p), true
		}
	}
	return nil, false
}

// newBotsProfile is apps.profile.get's app_profile for the #new-bots
// shapes, by what the request names.
func (s *Server) newBotsProfile(bot, app, home string) (map[string]any, bool) {
	icons := func(n string) map[string]any {
		m := map[string]any{}
		for _, px := range []int{32, 36, 48, 64, 72, 96, 128, 192, 512, 1024} {
			m["image_"+strconv.Itoa(px)] = s.URL + "img/" + strconv.Itoa(px) + "x" + strconv.Itoa(px) + "/" + n + ".png"
		}
		return m
	}
	homeMeta := func(home, msgs, ro bool) map[string]any {
		return map[string]any{"home_tab_enabled": home, "messages_tab_enabled": msgs, "messages_tab_read_only_enabled": ro}
	}
	base := func(id, name, desc, user, n string) map[string]any {
		return map[string]any{"id": id, "name": name, "desc": desc, "app_user_id": "", "enterprise_id": EnterpriseID,
			"is_xoxa_app": false, "is_slack_integration": false, "is_workflow_app": false, "is_directory_published": false,
			"is_distributed": false, "is_agent_app": false, "icons": icons(n), "installation_summary": "",
			"bot_user": map[string]any{"id": user, "username": strings.ToLower(strings.ReplaceAll(name, " ", "")), "memberships_count": 3}}
	}
	// installed is the workspace install: with its installer (auth) when
	// the request named a live bot or the app alone, without when it
	// named a stale bot with bot_home_team; not installed (the org's
	// app, no auth) for a stale bot alone
	install := func(p map[string]any, stale bool) map[string]any {
		switch {
		case stale && home == "":
			p["is_installed"], p["team_id"] = false, EnterpriseID
		case stale:
			p["is_installed"], p["team_id"] = true, home
		default:
			p["is_installed"], p["team_id"] = true, TeamID
			p["auth"] = map[string]any{"created_by": AdminID, "date_created": "1700000000"}
		}
		return p
	}
	switch {
	case bot == NewBotsStaleBotID || bot == "" && app == NewBotsStaleAppID:
		p := base(NewBotsStaleAppID, "Shelf Keeper", "Keeps the reading list tidy", NewBotsStaleUserID, "nbstale")
		p["app_card_color"] = "8a3c2b"
		p["app_home_metadata"] = homeMeta(false, false, true)
		return install(p, bot != ""), true
	case bot == NewBotsTwoOldBotID || bot == NewBotsTwoBotID || bot == "" && app == NewBotsTwoAppID:
		p := base(NewBotsTwoAppID, "Who Posts", "Tells you who is new", NewBotsTwoUserID, "nbtwo")
		p["app_home_metadata"] = homeMeta(true, true, false) // no app_card_color
		return install(p, bot == NewBotsTwoOldBotID), true
	case bot == NewBotsLiveBotID || bot == "" && app == NewBotsLiveAppID:
		p := base(NewBotsLiveAppID, "Fish Finder", "Finds the fish you were looking for", NewBotsLiveUserID, "nblive")
		p["app_card_color"] = "2b6a8a"
		p["app_home_metadata"] = homeMeta(false, true, true)
		p["commands"] = map[string]any{"fish": map[string]any{"desc": "Find a fish", "usage": "[name]"}}
		return install(p, false), true
	}
	return nil, false
}
