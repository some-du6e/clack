package fakeslack

import "github.com/some-du6e/clack/internal/slack"

// A share_prompt's agent: Slack's SharePromptButton draws a prompt only
// for an agent it knows with a picture (an Agentforce agent, the one
// kind Clack knows). Synthetic: a bot user users.info knows by id only,
// so no list, count or search changes.

// PromptAgentID is the Agentforce agent's bot user; PromptAgentBotID and
// PromptAgentAppID its bot and app.
const (
	PromptAgentID    = "U0FAKEAFAG"
	PromptAgentBotID = "B0FAKEAFAG"
	PromptAgentAppID = "A0FAKEAFAG"
)

func (s *Server) promptAgentUser() slack.User {
	p := slack.Profile{RealName: "Pipeline Agent", Team: TeamID, Fields: []byte(`{}`), BotID: PromptAgentBotID, APIAppID: PromptAgentAppID,
		Image24: s.identityImg(24, "promptagent"), Image32: s.identityImg(32, "promptagent"), Image48: s.identityImg(48, "promptagent"),
		Image72: s.identityImg(72, "promptagent"), Image192: s.identityImg(192, "promptagent"), Image512: s.identityImg(512, "promptagent")}
	return slack.User{ID: PromptAgentID, TeamID: TeamID, Name: "pipeline", RealName: "Pipeline Agent", IsBot: true, IsAgentforceBot: true, Profile: p}
}
