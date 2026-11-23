package richtext

// A share_prompt block: an agent's prompt the viewer can send it (Slack's
// SharePrompt, boot-shared JcWK; SharePromptButton CvC8; CDN build
// 9f450b8ad3db). SharePromptButton draws only when the prompt's agent
// member is known with a picture and is an agent: an Agentforce agent
// (isAgentforceMember), Slackbot with Slackbot AI on, or an AI app. Clack
// knows Agentforce agents only (no Slackbot AI, no AI app installs), so
// the other prompts draw nothing, as Slack draws them when it doesn't
// know the agent.

import "encoding/json"

// SharePrompt is a share_prompt block.
type SharePrompt struct {
	Label, Prompt string
	// Rich is prompt_rich_text's blocks (the preview draws them instead
	// of Prompt, cut as TruncatedBlocks cuts them); RichRaw the block
	// itself, which Send posts.
	Rich    []Block
	RichRaw json.RawMessage
	// PromptID, AppID, BotUserID and IsSlackbot are the block's.
	PromptID, AppID, BotUserID string
	IsSlackbot                 bool
	// AgentName is the agent's real name (getMemberRealName: "Send to
	// {agentName}"), AgentAvatar its picture; Shown is whether the card
	// draws.
	AgentName, AgentAvatar string
	Shown                  bool
}

// AgentResolver is an optional Resolver extension: a prompt's agent
// member (by its bot user id): its real name, picture and whether it's an
// Agentforce agent; ok is false while the member isn't known.
type AgentResolver interface {
	PromptAgent(id string) (name, avatar string, agentforce, ok bool)
}

// bkSharePrompt is a share_prompt block's keys, raw (toCamelCaseProps:
// either spelling).
type bkSharePrompt struct {
	Prompt     json.RawMessage `json:"prompt"`
	Rich       json.RawMessage `json:"prompt_rich_text"`
	RichCamel  json.RawMessage `json:"promptRichText"`
	Label      json.RawMessage `json:"label"`
	PromptID   json.RawMessage `json:"prompt_id"`
	PromptCaml json.RawMessage `json:"promptId"`
	AppID      json.RawMessage `json:"app_id"`
	AppCamel   json.RawMessage `json:"appId"`
	BotUser    json.RawMessage `json:"bot_user_id"`
	BotCamel   json.RawMessage `json:"botUserId"`
	Slackbot   json.RawMessage `json:"is_slackbot"`
	SlackCamel json.RawMessage `json:"isSlackbot"`
	BlockID    json.RawMessage `json:"block_id"`
}

// sharePromptBlock builds a share_prompt block.
func (c *builder) sharePromptBlock(b *bk) Block {
	v := b.prompt
	if v == nil {
		v = &bkSharePrompt{}
	}
	sp := &SharePrompt{Label: rawString(v.Label), Prompt: rawString(v.Prompt), PromptID: rawString(firstRaw(v.PromptID, v.PromptCaml)),
		AppID: rawString(firstRaw(v.AppID, v.AppCamel)), BotUserID: rawString(firstRaw(v.BotUser, v.BotCamel))}
	if s, ok := rawValue(firstRaw(v.Slackbot, v.SlackCamel)); ok {
		sp.IsSlackbot = jsTruthy(s)
	}
	if raw := firstRaw(v.Rich, v.RichCamel); len(raw) > 0 && raw[0] == '{' {
		var rb bk
		if json.Unmarshal(raw, &rb) == nil && rb.Type == "rich_text" {
			sp.Rich, sp.RichRaw = c.richText(rb.Elements), raw
		}
	}
	if ar, ok := c.r.(AgentResolver); ok && sp.BotUserID != "" {
		if name, avatar, agentforce, known := ar.PromptAgent(sp.BotUserID); known {
			sp.AgentName, sp.AgentAvatar = name, avatar
			sp.Shown = avatar != "" && agentforce
		}
	}
	return Block{Kind: SharePromptBlock, BlockID: b.BlockID, Prompt: sp}
}
