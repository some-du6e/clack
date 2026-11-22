package core

import "github.com/some-du6e/clack/internal/store"

// botTeamFrom stores bot id's team where Slack's getTeamIdFromBotOrUser
// reads it: bots.info's team_id (fromBotsInfo), else a message's
// bot_profile.team_id. (bot_home_team_test.go)
func botTeamFrom(a *App, id, team string, fromBotsInfo bool) {
	if fromBotsInfo {
		a.S.PutBot(id, store.BotInfo{Name: "a bot", TeamID: team})
		return
	}
	a.noteBotProfileTeam(id, team)
}
