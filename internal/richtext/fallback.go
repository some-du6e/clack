package richtext

// How Slack's client falls back on a block it can't draw, and its
// condition blocks (web client build 846030e1ce97, 2026-11-17; refs in
// docs/BLOCKKIT-COVERAGE.md).
//
// Messages draw their blocks through InteractiveBlocks (boot-shared VoyB)
// or ReadonlyBlocks (+Yyz). Both first expand condition blocks
// (useExpandedConditionBlocks, CRur), then hand each block to
// SUIRendererItem (Q5CE), which looks the type up in the renderables
// registered for messages (jBv3's list). A type without one, whether
// Slack's enum names it or not, goes to resolveFallback: the block's own
// "fallback" (a list draws each of its payload items through the same
// lookup, one payload item draws itself), else the fallback component,
// UnsupportedBlockFallback (1G65, "This message can’t be fully
// displayed.").

import (
	"encoding/json"
	"strings"
)

// messageRenderables is every type SUIRendererItem has a renderable for in
// messages: jBv3's list (blocks, elements, the rich_text pieces and their
// inline elements share one type-keyed map, so an element type at the top
// level is "known" too). Clack draws most of them; the ones it doesn't yet
// keep their text object (docs/BLOCKKIT-COVERAGE.md). Absent on purpose:
// condition (expanded first), fallback_canary, expandable, error, group
// and markdown, which Slack's server translates before a client sees it.
var messageRenderables = map[string]bool{}

func init() {
	for _, t := range strings.Fields(`actions alert attachment_reference auth_provider button call callout
		canvas_mermaid canvas_table card carousel channel_section channel_set channels_select checkboxes
		contact_card container context_actions context data_table data_visualization datepicker
		datetimepicker divider email_text_input event external_select feedback_buttons file_input file
		header icon_button conversations_select image input layout loading mcp_app message message_embed
		multi_external_select multi_channels_select multi_static_select multi_users_select
		multi_conversations_select mrkdwn number_input overflow page password plan plain_text
		plain_text_input prototype radio_buttons related_automation_decorator rich_text_input
		salesforce_record_card search_top_results section share_prompt share_shortcut share_workflow
		share_skill split_button static_select table task_card timepicker url_text_input users_select video
		workflow_button timeline rich_text
		rich_text_section rich_text_quote rich_text_preformatted rich_text_list rich_text_header rich_text_divider
		text emoji link channel user usergroup broadcast color date variable workflowtoken workflow_mention
		work_object_mention tag canvas _canvas_mention canvas_user_mention canvas_message_unfurl
		message_mention list_record skill rich_text_file folder_mention salesforce_data_field
		_theme_install_button citation attachment_mention unknown`) {
		messageRenderables[t] = true
	}
}

// maxFallbackDepth bounds a chain of fallbacks inside fallbacks (Slack's
// recursion has none; a hostile payload shouldn't take the stack).
const maxFallbackDepth = 8

// unknownCard is UnsupportedBlockFallback.
func unknownCard() Block {
	return Block{Kind: UnknownBlock, Spans: []Span{{Kind: Text, Text: "This message can’t be fully displayed."}}}
}

// isPayloadItem is Q5CE's isPayloadItem: an object whose type is a string.
func isPayloadItem(raw json.RawMessage) bool {
	var probe map[string]json.RawMessage
	if json.Unmarshal(raw, &probe) != nil {
		return false
	}
	t := probe["type"]
	return len(t) > 0 && t[0] == '"'
}

// resolveFallback draws b, a block without a renderable, the way Q5CE's
// resolveFallback does: its fallback list's payload items (others are
// skipped, and an empty list draws nothing), its single payload item, or
// the UnsupportedBlockFallback card. Child blocks (a callout's, a
// container's) render through createRenderBlocks with no fallback
// component, so there the last resort is nothing.
func (c *builder) resolveFallback(out []Block, b *bk) []Block {
	raw := b.Fallback.raw
	if c.depth >= maxFallbackDepth {
		return c.lastResort(out)
	}
	c.depth++
	defer func() { c.depth-- }()
	switch {
	case len(raw) > 0 && raw[0] == '[':
		var parts []json.RawMessage
		if json.Unmarshal(raw, &parts) != nil {
			return c.lastResort(out)
		}
		var items []bk
		for _, p := range parts {
			if !isPayloadItem(p) {
				continue
			}
			items = append(items, decodeBlock(p))
		}
		return append(out, c.blocks(items)...)
	case isPayloadItem(raw):
		return append(out, c.blocks([]bk{decodeBlock(raw)})...)
	}
	return c.lastResort(out)
}

// FallbackItems is what resolveFallback draws for a block of type typ
// whose fallback field is raw: nothing when Slack has a renderable for
// typ (or Clack converts it itself, markdown); else its list's payload
// items, or its single payload item. Blockkit walks them for the
// elements richtext draws there.
func FallbackItems(typ string, raw json.RawMessage) []json.RawMessage {
	if messageRenderables[typ] || typ == "markdown" || len(raw) == 0 {
		return nil
	}
	if raw[0] == '[' {
		var parts, out []json.RawMessage
		_ = json.Unmarshal(raw, &parts)
		for _, p := range parts {
			if isPayloadItem(p) {
				out = append(out, p)
			}
		}
		return out
	}
	if isPayloadItem(raw) {
		return []json.RawMessage{raw}
	}
	return nil
}

// lastResort is resolveFallback's fallback component: the card, save
// among a callout's children (noCard).
func (c *builder) lastResort(out []Block) []Block {
	if c.noCard > 0 {
		return out
	}
	return append(out, unknownCard())
}

// children converts a block's child blocks. A container's are its
// childBlocks slot (yWQF), whose SUIRendererItems keep the container's
// own fallback component (in a message, its caller's: the card, or
// nothing inside a callout); a callout's go through createRenderBlocks
// (a+Oj) with no fallback component, so there an unknown child draws
// nothing.
func (c *builder) children(bs []bk, callout bool) []Block {
	if callout {
		c.noCard++
		defer func() { c.noCard-- }()
	}
	return c.blocks(bs)
}

// decodeBlock decodes one block; one that doesn't decode is badBlock
// (the per-block "This content could not be displayed.").
func decodeBlock(raw json.RawMessage) bk {
	var b bk
	if json.Unmarshal(raw, &b) != nil {
		return bk{Type: badBlock}
	}
	return b
}

// ---- condition blocks ----

// TeamResolver is an optional Resolver extension: the viewer's workspace
// and enterprise ids (Slack's selectTeamId and selectEnterpriseId), which
// a condition block's "team" conditions compare against.
type TeamResolver interface {
	TeamIDs() (team, enterprise string)
}

// conditionClient is the client CRur evaluates "client" conditions
// against: the web and desktop app pass "desktop".
const conditionClient = "desktop"

// conditionContentTypes are the content types CRur matches ("condition_demo").
var conditionContentTypes = []string{"condition_demo"}

type bkCondition struct {
	Type  string `json:"type"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

type bkCase struct {
	Conditions []bkCondition     `json:"conditions"`
	Blocks     []json.RawMessage `json:"blocks"`
}

// evalCondition is CRur's evalCondition.
func evalCondition(cd bkCondition, team, enterprise string) bool {
	str := func(vals ...string) bool { // evalStringCondition
		switch cd.Op {
		case "=":
			for _, v := range vals {
				if cd.Value == v {
					return true
				}
			}
			return false
		case "!=":
			for _, v := range vals {
				if cd.Value == v {
					return false
				}
			}
			return true
		}
		return false
	}
	switch cd.Type {
	case "client":
		return str(conditionClient)
	case "content_type": // the op isn't read
		for _, v := range conditionContentTypes {
			if cd.Value == v {
				return true
			}
		}
		return false
	case "team":
		return str(team, enterprise)
	}
	return false
}

// expandCondition is one condition block's blocks: the first case whose
// conditions all hold, else its default, else none. ok is false when b
// isn't a condition block (its cases aren't a list).
func expandCondition(raw json.RawMessage, team, enterprise string) (blocks []json.RawMessage, ok bool) {
	var cb struct {
		Type    string          `json:"type"`
		Cases   json.RawMessage `json:"cases"`
		Default json.RawMessage `json:"default"`
	}
	if json.Unmarshal(raw, &cb) != nil || cb.Type != "condition" || len(cb.Cases) == 0 || cb.Cases[0] != '[' {
		return nil, false
	}
	var cases []json.RawMessage
	if json.Unmarshal(cb.Cases, &cases) != nil {
		return nil, false
	}
	for _, cr := range cases {
		var cs bkCase
		if json.Unmarshal(cr, &cs) != nil || cs.Conditions == nil {
			continue
		}
		all := true
		for _, cd := range cs.Conditions {
			if !evalCondition(cd, team, enterprise) {
				all = false
				break
			}
		}
		if all {
			return cs.Blocks, true
		}
	}
	switch d := cb.Default; {
	case len(d) > 0 && d[0] == '[':
		var bs []json.RawMessage
		_ = json.Unmarshal(d, &bs)
		return bs, true
	case len(d) > 0 && d[0] == '{': // flatten keeps a lone object
		return []json.RawMessage{d}, true
	}
	return nil, true
}

// ExpandConditions is Slack's expandConditionBlocks (CRur) over a blocks
// array: each condition block becomes the blocks of its first matching
// case (or its default, or nothing), the rest stay. raw comes back as it
// was when it has no condition block.
func ExpandConditions(raw json.RawMessage, team, enterprise string) json.RawMessage {
	if len(raw) == 0 || !strings.Contains(string(raw), `"condition"`) {
		return raw
	}
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		return raw
	}
	out := make([]json.RawMessage, 0, len(parts))
	changed := false
	for _, p := range parts {
		if bs, ok := expandCondition(p, team, enterprise); ok {
			out = append(out, bs...)
			changed = true
			continue
		}
		out = append(out, p)
	}
	if !changed {
		return raw
	}
	b, err := json.Marshal(out)
	if err != nil {
		return raw
	}
	return b
}

// teamIDs is the resolver's workspace and enterprise ids, if it knows them.
func (c *builder) teamIDs() (team, enterprise string) {
	if tr, ok := c.r.(TeamResolver); ok {
		return tr.TeamIDs()
	}
	return "", ""
}

// expanded is a message's (or attachment's) own blocks array with its
// condition blocks expanded, as InteractiveBlocks and ReadonlyBlocks do
// before drawing. Fallback payloads skip that step, like resolveFallback.
func (c *builder) expanded(raw json.RawMessage) json.RawMessage {
	if !strings.Contains(string(raw), `"condition"`) {
		return raw
	}
	team, ent := c.teamIDs()
	return ExpandConditions(raw, team, ent)
}

// topBlocks converts a message's (or attachment's) own blocks array.
func (c *builder) topBlocks(raw json.RawMessage) []Block {
	return c.blocks(parseBlocks(c.expanded(raw)))
}
