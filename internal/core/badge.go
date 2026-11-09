package core

// The app badge after a message sender's name, as Slack's web client
// decides it (CDN build a84662d2d135, 2026-11-08):
//
//   - BaseMessageSender.renderBotLabel (boot-shared zWB7 / BaseMessageSender):
//     no badge when memberIsHuman (the message has a user who is neither
//     is_bot nor is_app_user, nor "USLACK"; Slackbot is served is_bot
//     false, so it has none) or for huddle, canvas-update, welcome,
//     custom-response and AI huddle-summary messages. A user-less message
//     always gets one.
//   - BaseAppBadge (members, client-boot): EXPERIMENTAL (sidekick, not
//     passed by messages) > AGENTFORCE > AGENT > WORKFLOW > APP.
//     AGENTFORCE: the member's is_agentforce_bot. WORKFLOW: the member's
//     or the bot's is_workflow_bot.
//   - AGENT (isAgentApp, /ly8): with support_lots_of_agents off (Hack
//     Club, 2026-11-08), hasAgentBadgeByAppId: show_as_agent and
//     agent_badge on and the app's full profile (apps.profile.get, kept
//     as legacy_app) is_shown_as_agent. The member's agent_status isn't
//     read. Slack loads that profile when it opens the app's card; the
//     thin Flannel apps/info row it usually holds has no legacy_app, so
//     agent apps mostly show APP (kyto in #clack, 2026-11-08). With
//     support_lots_of_agents on: hasAgentBadgeByBotUserId: the member is
//     an Agentforce or sidekick bot, or agent_status is
//     consistently_an_agent (resolveShownAsAgent), still behind
//     show_as_agent and agent_badge.

// Sender badge labels (BaseAppBadge).
const (
	BadgeApp        = "APP"
	BadgeAgent      = "AGENT"
	BadgeWorkflow   = "WORKFLOW"
	BadgeAgentforce = "AGENTFORCE"
)

// SenderBadge is the badge Slack draws after the sender of a message from
// user (or, user-less, from bot botID): "" for people and Slackbot.
func (a *App) SenderBadge(user, botID string) string {
	if user != "" {
		u, ok := a.User(user)
		if !ok {
			return "" // Slack draws the unknown-sender placeholder
		}
		human := user != "USLACKBOT" && !u.IsBot && !u.AppUser && user != "USLACK"
		if human || user == "USLACKBOT" {
			return ""
		}
		switch {
		case u.Agentforce:
			return BadgeAgentforce
		case a.memberIsAgent(u):
			return BadgeAgent
		case u.Workflow:
			return BadgeWorkflow
		}
		return BadgeApp
	}
	if botID == "" {
		return BadgeApp
	}
	b, _ := a.S.Bot(botID)
	switch {
	case a.botIsAgent(botID):
		return BadgeAgent
	case b.Workflow:
		return BadgeWorkflow
	}
	return BadgeApp
}

// agentBadgesOn: show_as_agent and agent_badge, the gate on every AGENT.
func (a *App) agentBadgesOn() bool {
	return a.experiment("show_as_agent") == "on" && a.experiment("agent_badge") == "on"
}

// memberIsAgent is /ly8 hh for a bot user.
func (a *App) memberIsAgent(u User) bool {
	if !a.agentBadgesOn() {
		return false
	}
	if a.experiment("support_lots_of_agents") == "on" {
		return u.Agent // agent_status consistently_an_agent
	}
	card, ok := a.CachedAppCard(u.ID, "")
	return ok && card.ShownAsAgent
}

// botIsAgent is hasAgentBadgeByAppId for a user-less bot message.
func (a *App) botIsAgent(botID string) bool {
	if !a.agentBadgesOn() {
		return false
	}
	card, ok := a.CachedAppCard("", botID)
	if !ok {
		return false
	}
	if a.experiment("support_lots_of_agents") == "on" && card.AppUserID != "" {
		if u, ok := a.User(card.AppUserID); ok {
			return u.Agent || u.Agentforce
		}
	}
	return card.ShownAsAgent
}
