package blockkit

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/some-du6e/clack/internal/emoji"
	"github.com/some-du6e/clack/internal/richtext"
)

// Text is a Block Kit text object.
type Text struct {
	Type string `json:"type"` // plain_text | mrkdwn
	Text string `json:"text"`
	// Emoji is plain_text's emoji: PlainText (iY3m) draws :shortcodes:
	// as emoji unless it is false (nil: not given, on)
	Emoji    *bool `json:"emoji,omitempty"`
	Verbatim bool  `json:"verbatim,omitempty"`
}

// emojiOn reports whether t's :shortcodes: draw as emoji.
func (t *Text) emojiOn() bool { return t == nil || t.Emoji == nil || *t.Emoji }

func (t *Text) str() string {
	if t == nil {
		return ""
	}
	return t.Text
}

// Option is an option object; Raw is exactly what the app sent (Slack
// echoes it back in selected_option).
type Option struct {
	Text        Text            `json:"text"`
	Value       string          `json:"value"`
	Description *Text           `json:"description,omitempty"`
	URL         string          `json:"url,omitempty"`
	Raw         json.RawMessage `json:"-"`
}

func (o *Option) UnmarshalJSON(b []byte) error {
	type plain Option
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*o = Option(p)
	o.Raw = append(json.RawMessage(nil), b...)
	return nil
}

// payload is the option as Slack puts it in selected_option(s): the
// app's own object (text, value, description).
func (o *Option) payload() json.RawMessage {
	if len(o.Raw) > 0 {
		return o.Raw
	}
	b, _ := json.Marshal(struct {
		Text        Text   `json:"text"`
		Value       string `json:"value"`
		Description *Text  `json:"description,omitempty"`
	}{o.Text, o.Value, o.Description})
	return b
}

// OptionGroup is an option_groups entry.
type OptionGroup struct {
	Label   Text     `json:"label"`
	Options []Option `json:"options"`
}

// Confirm is a confirmation dialog object.
type Confirm struct {
	Title   *Text  `json:"title"`
	Text    *Text  `json:"text"`
	Confirm *Text  `json:"confirm"`
	Deny    *Text  `json:"deny"`
	Style   string `json:"style"` // primary | danger
}

// ConvFilter is a conversations_select filter.
type ConvFilter struct {
	Include                       []string `json:"include"` // im mpim private public
	ExcludeExternalSharedChannels bool     `json:"exclude_external_shared_channels"`
	ExcludeBotUsers               bool     `json:"exclude_bot_users"`
}

// DispatchConfig is dispatch_action_config.
type DispatchConfig struct {
	TriggerActionsOn []string `json:"trigger_actions_on"` // on_enter_pressed | on_character_entered
}

// Element is an interactive (block) element.
type Element struct {
	Type     string `json:"type"`
	ActionID string `json:"action_id"`

	// button / workflow_button
	Text  *Text  `json:"text"`
	Value string `json:"value"`
	URL   string `json:"url"`
	Style string `json:"style"`
	Icon  string `json:"icon"` // icon_button
	// VisibleTo is visible_to_user_ids: an actions block's button or
	// checkboxes the viewer isn't listed in isn't drawn, and an
	// icon_button hides itself (hiddenFrom)
	VisibleTo []string `json:"visible_to_user_ids"`

	Placeholder         *Text `json:"placeholder"`
	Confirm             *Confirm
	FocusOnLoad         bool   `json:"focus_on_load"`
	AccessibilityLabel  string `json:"accessibility_label"`
	Options             []Option
	OptionGroups        []OptionGroup `json:"option_groups"`
	InitialOption       *Option       `json:"initial_option"`
	InitialOptions      []Option      `json:"initial_options"`
	MinQueryLength      *int          `json:"min_query_length"`
	MaxSelectedItems    int           `json:"max_selected_items"`
	InitialUser         string        `json:"initial_user"`
	InitialUsers        []string      `json:"initial_users"`
	InitialConversation string        `json:"initial_conversation"`
	InitialConvs        []string      `json:"initial_conversations"`
	InitialChannel      string        `json:"initial_channel"`
	InitialChannels     []string      `json:"initial_channels"`
	DefaultToCurrent    bool          `json:"default_to_current_conversation"`
	ResponseURLEnabled  bool          `json:"response_url_enabled"`
	Filter              *ConvFilter   `json:"filter"`

	InitialDate     string `json:"initial_date"` // YYYY-MM-DD
	InitialTime     string `json:"initial_time"` // HH:mm
	Timezone        string `json:"timezone"`
	InitialDateTime int64  `json:"initial_date_time"`

	InitialValue     json.RawMessage `json:"initial_value"` // string, or rich_text for rich_text_input
	Multiline        bool            `json:"multiline"`
	MinLength        int             `json:"min_length"`
	MaxLength        int             `json:"max_length"`
	IsDecimalAllowed bool            `json:"is_decimal_allowed"`
	MinValue         string          `json:"min_value"`
	MaxValue         string          `json:"max_value"`
	Dispatch         *DispatchConfig `json:"dispatch_action_config"`

	FileTypes []string `json:"filetypes"`
	MaxFiles  int      `json:"max_files"`

	Workflow json.RawMessage `json:"workflow"` // workflow_button trigger

	Raw json.RawMessage `json:"-"`
}

func (e *Element) UnmarshalJSON(b []byte) error {
	type plain Element
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*e = Element(p)
	e.Raw = append(json.RawMessage(nil), b...)
	return nil
}

// initialText is initial_value as a string (text inputs).
func (e *Element) initialText() string {
	var s string
	if json.Unmarshal(e.InitialValue, &s) == nil {
		return s
	}
	return ""
}

// allOptions flattens options and option_groups.
func (e *Element) allOptions() []Option {
	if len(e.OptionGroups) == 0 {
		return e.Options
	}
	var out []Option
	for _, g := range e.OptionGroups {
		out = append(out, g.Options...)
	}
	return out
}

// hiddenFrom reports whether an actions block's element e isn't drawn
// for the viewer self. Only the Actions block reads visible_to_user_ids
// (boot-shared /jJF maybeRenderActionsElement): a button or checkboxes
// whose list is set, with a viewer known, who isn't in it. A section's
// accessory and an input's element draw for everyone; an icon_button
// hides itself wherever it is (iconButtonHidden).
func hiddenFrom(e *Element, self string) bool {
	if e == nil {
		return false
	}
	switch e.Type {
	case "button", "checkboxes":
		return len(e.VisibleTo) > 0 && self != "" && !slices.Contains(e.VisibleTo, self)
	}
	return iconButtonHidden(e, self)
}

// iconButtonHidden is IconButton's own check (boot-shared X2Vm): a list
// that leaves out the current user, or any list while there's none.
func iconButtonHidden(e *Element, self string) bool {
	return e != nil && e.Type == "icon_button" && len(e.VisibleTo) > 0 && (self == "" || !slices.Contains(e.VisibleTo, self))
}

// Kinds of elements.
func isSelect(t string) bool {
	switch t {
	case "static_select", "external_select", "users_select", "conversations_select", "channels_select":
		return true
	}
	return false
}

func isMulti(t string) bool {
	return strings.HasPrefix(t, "multi_") && isSelect(strings.TrimPrefix(t, "multi_"))
}

func isTextInput(t string) bool {
	switch t {
	case "plain_text_input", "email_text_input", "url_text_input", "number_input", "rich_text_input":
		return true
	}
	return false
}

// stateful elements keep a value in the surface's state.
func stateful(t string) bool {
	switch t {
	case "button", "workflow_button", "icon_button", "overflow", "image", "":
		return false
	}
	return true
}

// Block is one layout block (raw kept for the richtext renderer).
type Block struct {
	Type      string    `json:"type"`
	BlockID   string    `json:"block_id"`
	Text      *Text     `json:"text"`
	Fields    []Text    `json:"fields"`
	Accessory *Element  `json:"accessory"`
	Elements  []Element `json:"-"` // actions: interactive elements
	// input
	Label          *Text           `json:"label"`
	Element        *Element        `json:"element"`
	Hint           *Text           `json:"hint"`
	Optional       bool            `json:"optional"`
	DispatchAction bool            `json:"dispatch_action"`
	Raw            json.RawMessage `json:"-"`
	// Nested is the blocks richtext draws inside this one: a container's
	// or a callout's child_blocks, or the fallback payload of a type with
	// no renderable (richtext.FallbackItems). Slack draws them through
	// the same renderables with the surface's onAction (Container yWQF's
	// childBlocks slot, Callout's createRenderBlocks a+Oj, Q5CE
	// resolveFallback), so their elements are the surface's own: find,
	// initValues and the view's state walk them (allBlocks).
	Nested []Block `json:"-"`
	// kids are Nested's blocks as UnmarshalJSON found them, until
	// parseBlocks decodes them at their depth (nest).
	kids []json.RawMessage
}

// maxNesting is how deep blocks inside blocks are followed (containers'
// and callouts' child_blocks, fallback payloads), as richtext follows
// fallbacks (maxFallbackDepth); deeper ones are dropped. Without it a
// sender's containers nested 30 deep held the UI goroutine.
const maxNesting = 8

func (b *Block) UnmarshalJSON(raw []byte) error {
	type plain Block
	var p struct {
		plain
		Elements    json.RawMessage `json:"elements"`
		Actions     json.RawMessage `json:"actions"` // a card's buttons
		ChildBlocks json.RawMessage `json:"child_blocks"`
		Fallback    json.RawMessage `json:"fallback"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return err
	}
	*b = Block(p.plain)
	b.Raw = append(json.RawMessage(nil), raw...)
	switch {
	case b.Type == "actions" && len(p.Elements) > 0:
		_ = json.Unmarshal(p.Elements, &b.Elements)
	case b.Type == "card" && len(p.Actions) > 0: // Card's actions (7Xkb), its block id theirs
		_ = json.Unmarshal(p.Actions, &b.Elements)
	}
	// the nested blocks wait for parseBlocks, which knows their depth
	switch {
	case b.Type == "container" || b.Type == "callout":
		b.kids = blockParts(p.ChildBlocks)
	case len(p.Fallback) > 0:
		b.kids = richtext.FallbackItems(b.Type, p.Fallback)
	}
	return nil
}

// blockParts splits a blocks array into its blocks, undecoded (nil for
// anything but an array).
func blockParts(raw json.RawMessage) []json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		return nil
	}
	return parts
}

// parseBlocks decodes each of parts on its own (one bad block doesn't drop
// the rest, and nothing is decoded twice) and the blocks nested in them,
// depth levels down so far, to maxNesting.
func parseBlocks(parts []json.RawMessage, depth int) []Block {
	if parts == nil {
		return nil
	}
	bs := make([]Block, 0, len(parts))
	for _, p := range parts {
		var b Block
		if json.Unmarshal(p, &b) != nil {
			continue
		}
		kids := b.kids
		b.kids = nil
		if len(kids) > 0 && depth < maxNesting {
			b.Nested = parseBlocks(kids, depth+1)
		}
		bs = append(bs, b)
	}
	return bs
}

// allBlocks is every block of bs with the blocks nested in it, each
// before its own (Block.Nested): the tree richtext draws.
func allBlocks(bs []Block) []*Block {
	var out []*Block
	var walk func(bs []Block)
	walk = func(bs []Block) {
		for i := range bs {
			out = append(out, &bs[i])
			walk(bs[i].Nested)
		}
	}
	walk(bs)
	return out
}

// expandConditions is richtext.ExpandConditions with the viewer's
// workspace: every Block Kit surface (InteractiveBlocks, ReadonlyBlocks,
// AppHomeBlocks, ViewBlocks) expands condition blocks before drawing.
func expandConditions(h Host, raw json.RawMessage) json.RawMessage {
	var team, ent string
	if h != nil {
		if c := h.Core(); c != nil && c.S != nil {
			team, ent = c.S.TeamIDs()
		}
	}
	return richtext.ExpandConditions(raw, team, ent)
}

// ParseBlocks decodes a Block Kit blocks array (unknown fields ignored),
// block by block, with the blocks nested in them (parseBlocks).
func ParseBlocks(raw json.RawMessage) []Block {
	return parseBlocks(blockParts(raw), 0)
}

// ref is one element's place in a surface.
type ref struct {
	block *Block
	el    *Element
}

// elementsOf lists the interactive elements of a block.
func elementsOf(b *Block) []*Element {
	var out []*Element
	switch b.Type {
	case "actions", "card":
		for i := range b.Elements {
			out = append(out, &b.Elements[i])
		}
	case "section":
		if b.Accessory != nil && b.Accessory.Type != "image" {
			out = append(out, b.Accessory)
		}
	case "input":
		if b.Element != nil {
			out = append(out, b.Element)
		}
	}
	return out
}

// find returns the element with blockID/actionID (actionID may be empty
// when the app didn't set one: then the first of its type at index n).
func find(bs []Block, blockID, actionID string) (ref, bool) {
	for _, b := range allBlocks(bs) {
		if blockID != "" && b.BlockID != blockID {
			continue
		}
		for _, e := range elementsOf(b) {
			if e.ActionID == actionID {
				return ref{b, e}, true
			}
		}
	}
	return ref{}, false
}

// textOf renders a text object as plain text for labels (mrkdwn markers
// stripped the simple way; rich rendering goes through richtext).
func textOf(t *Text) string {
	if t == nil {
		return ""
	}
	s := t.Text
	if t.Type == "mrkdwn" {
		s = stripMrkdwn(s)
	}
	return emojize(unescape(s))
}

// emojize turns standard :shortcodes: into their unicode emoji (labels
// draw them as Slack's emoji images), like Slack's plain_text emoji.
func emojize(s string) string {
	if strings.Count(s, ":") < 2 {
		return s
	}
	var b strings.Builder
	for {
		i := strings.IndexByte(s, ':')
		if i < 0 {
			break
		}
		j := strings.IndexByte(s[i+1:], ':')
		if j < 0 {
			break
		}
		name := s[i+1 : i+1+j]
		if _, uni, ok := emoji.Standard(name); ok && name != "" && !strings.ContainsAny(name, " \t") {
			b.WriteString(s[:i])
			b.WriteString(uni)
			s = s[i+j+2:]
			continue
		}
		b.WriteString(s[:i+1])
		s = s[i+1:]
	}
	b.WriteString(s)
	return b.String()
}

func unescape(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	return strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&").Replace(s)
}

// stripMrkdwn removes *bold* _italic_ ~strike~ `code` markers and turns
// <url|label> into label, for one-line labels (option texts).
func stripMrkdwn(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '*', '_', '~', '`':
			continue
		case '<':
			if j := strings.IndexByte(s[i:], '>'); j > 0 {
				inner := s[i+1 : i+j]
				if k := strings.IndexByte(inner, '|'); k >= 0 {
					b.WriteString(inner[k+1:])
				} else if strings.HasPrefix(inner, "@") || strings.HasPrefix(inner, "#") || strings.HasPrefix(inner, "!") {
					b.WriteString(inner)
				} else {
					b.WriteString(inner)
				}
				i += j
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}
