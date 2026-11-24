package fakeslack

// The share_prompt block (Slack's SharePromptButton): an agent's prompt
// with Send to {agentName}. Synthetic: the Agentforce agent
// (prompt_agent.go), and a bot nobody knows, whose prompt draws nothing.

func init() { RegisterGallery("19-share-prompt", galleryPrompt) }

func galleryPrompt(e GalleryEnv) []GalleryMessage {
	prompt := func(id, label, text, bot, extra string) string {
		return `{"type":"share_prompt","block_id":"sp-` + id + `","prompt_id":"PR0` + id + `","label":"` + label + `","prompt":"` + text +
			`","app_id":"` + PromptAgentAppID + `","bot_user_id":"` + bot + `","is_slackbot":false` + extra + `}`
	}
	long := "Go through this week's pipeline changes by region and owner, list every deal that moved stage or slipped its close date, " +
		"say what changed and who changed it, flag the deals at risk with the reason, and end with three things the team should do next week."
	rich := `,"prompt_rich_text":{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"Summarise "},` +
		`{"type":"text","text":"this week's pipeline","style":{"bold":true}},{"type":"text","text":" for the Monday review."}]}]}`
	return []GalleryMessage{
		{Name: "share prompt/short", Text: "A prompt to try", Blocks: `[` + prompt("1", "Weekly pipeline", "Summarise this week's pipeline changes.", PromptAgentID, "") + `]`},
		{Name: "share prompt/long (Show more)", Text: "A long prompt", Blocks: `[` + prompt("2", "Pipeline review", long, PromptAgentID, "") + `]`},
		{Name: "share prompt/rich text", Text: "A rich text prompt", Blocks: `[` + prompt("3", "Monday review", "Summarise this week's pipeline for the Monday review.", PromptAgentID, rich) + `]`},
		{Name: "share prompt/no label", Text: "A prompt without a label", Blocks: `[` + prompt("4", "", "Draft the follow-up email.", PromptAgentID, "") + `]`},
		{Name: "share prompt/unknown agent", Text: "A prompt for an agent nobody knows (draws nothing)", Blocks: `[{"type":"section","text":{"type":"mrkdwn","text":"A prompt for an unknown agent:"}},` + prompt("5", "Unknown", "x", "U0NOSUCH02", "") + `]`},
	}
}
