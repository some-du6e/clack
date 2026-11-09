package richtext

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// bk is a generic Block Kit block / element / rich_text node.
type bk struct {
	bkAgent          // plan, task_card (agent.go)
	Type      string `json:"type"`
	BlockID   string `json:"block_id"`
	Elements  []bk   `json:"elements"`
	Rows      [][]bk `json:"rows"` // table
	ColumnSet []struct {
		Align     string `json:"align"`
		IsWrapped bool   `json:"is_wrapped"`
	} `json:"column_settings"` // table
	Text        json.RawMessage `json:"text"` // string (rich_text) or text object
	Style       json.RawMessage `json:"style"`
	URL         string          `json:"url"`
	UserID      string          `json:"user_id"`
	ChannelID   string          `json:"channel_id"`
	TabID       string          `json:"tab_id"`     // channel: a channel-tab slug
	MessageTS   string          `json:"message_ts"` // message_mention
	AuthorID    string          `json:"author_id"`  // message_mention
	UsergroupID string          `json:"usergroup_id"`
	TeamID      string          `json:"team_id"`
	Name        string          `json:"name"`
	Unicode     string          `json:"unicode"`
	SkinTone    int             `json:"skin_tone"`
	Range       string          `json:"range"`
	Timestamp   json.RawMessage `json:"timestamp"`
	Format      string          `json:"format"`
	Fallback    bkFallback      `json:"fallback"`
	Value       string          `json:"value"`
	Indent      int             `json:"indent"`
	Offset      int             `json:"offset"`
	Border      int             `json:"border"`
	Fields      []textObj       `json:"fields"`
	Accessory   *bk             `json:"accessory"`
	ImageURL    string          `json:"image_url"`
	AltText     string          `json:"alt_text"`
	Title       *textObj        `json:"title"`
	ActionID    string          `json:"action_id"`
	Placeholder *textObj        `json:"placeholder"`
	Options     []struct {
		Text  textObj `json:"text"`
		Value string  `json:"value"`
	} `json:"options"`
	InitialOption *struct {
		Text textObj `json:"text"`
	} `json:"initial_option"`
	ImageWidth      int      `json:"image_width"`
	ImageHeight     int      `json:"image_height"`
	ImageBytes      int64    `json:"image_bytes"`
	VideoURL        string   `json:"video_url"`
	ThumbnailURL    string   `json:"thumbnail_url"`
	AuthorName      string   `json:"author_name"`
	ProviderName    string   `json:"provider_name"`
	ProviderIconURL string   `json:"provider_icon_url"`
	TitleURL        string   `json:"title_url"`
	Description     *textObj `json:"description"`
	SlackFile       *struct {
		URL string `json:"url"`
		ID  string `json:"id"`
	} `json:"slack_file"`
	Label      *textObj `json:"label"`
	Element    *bk      `json:"element"`
	ExternalID string   `json:"external_id"`
	FileID     string   `json:"file_id"`
	Source     string   `json:"source"`
	Expand     bool     `json:"expand"` // section: never truncate
	CallID     string   `json:"call_id"`
	Call       *struct {
		V1 struct {
			Name        string `json:"name"`
			JoinURL     string `json:"join_url"`
			DateStart   int64  `json:"date_start"`
			DateEnd     int64  `json:"date_end"`
			AppIconURLs struct {
				Image32 string `json:"image_32"`
			} `json:"app_icon_urls"`
		} `json:"v1"`
	} `json:"call"`
}

type textObj struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Emoji    *bool  `json:"emoji"` // plain_text: false keeps :name: as typed (Slack stores true when unset)
	Verbatim bool   `json:"verbatim"`
}

func (b *bk) textString() string {
	if len(b.Text) == 0 {
		return ""
	}
	if b.Text[0] == '"' {
		var s string
		_ = json.Unmarshal(b.Text, &s)
		return s
	}
	var t textObj
	_ = json.Unmarshal(b.Text, &t)
	return t.Text
}

func (b *bk) textObj() *textObj {
	if len(b.Text) == 0 || b.Text[0] != '{' {
		return nil
	}
	var t textObj
	if json.Unmarshal(b.Text, &t) != nil {
		return nil
	}
	return &t
}

// style decodes a rich_text inline style object.
func (b *bk) style() (st Style, unlink bool) {
	if len(b.Style) == 0 || b.Style[0] != '{' {
		return 0, false
	}
	var m map[string]bool
	_ = json.Unmarshal(b.Style, &m)
	if m["bold"] {
		st |= Bold
	}
	if m["italic"] {
		st |= Italic
	}
	if m["strike"] {
		st |= Strike
	}
	if m["code"] {
		st |= Code
	}
	if m["underline"] {
		st |= Underline
	}
	if m["highlight"] || m["client_highlight"] {
		st |= Highlight
	}
	return st, m["unlink"]
}

func (b *bk) styleString() string {
	if len(b.Style) > 0 && b.Style[0] == '"' {
		var s string
		_ = json.Unmarshal(b.Style, &s)
		return s
	}
	return ""
}

// builder carries state while converting one message.
type builder struct {
	r     Resolver
	files map[string]*slack.File // message files by id, for file blocks
	used  map[string]bool        // files already placed by a file block
}

func parseBlocks(raw json.RawMessage) []bk {
	if len(raw) <= 2 || string(raw) == "null" {
		return nil
	}
	var bs []bk
	if json.Unmarshal(raw, &bs) == nil {
		return bs
	}
	// One malformed block shouldn't drop the rest: decode them one by one;
	// the bad ones become Slack's "This content could not be displayed."
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		return nil
	}
	bs = bs[:0]
	for _, p := range parts {
		var b bk
		if json.Unmarshal(p, &b) != nil {
			b = bk{Type: badBlock}
		}
		bs = append(bs, b)
	}
	return bs
}

// bkFallback is a "fallback" field: a string (a date element's text) or,
// on a block, the payload Slack draws when it can't draw the block (one
// block or a list; resolveFallback).
type bkFallback struct {
	Text string
	raw  json.RawMessage
}

func (f *bkFallback) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		return json.Unmarshal(b, &f.Text)
	}
	f.raw = append(json.RawMessage(nil), b...)
	return nil
}

func (f bkFallback) blocks() []bk {
	if len(f.raw) == 0 {
		return nil
	}
	var one bk
	if f.raw[0] == '{' {
		if json.Unmarshal(f.raw, &one) == nil && one.Type != "" && slackBlockTypes[one.Type] {
			return []bk{one}
		}
		return nil
	}
	var list []bk
	if json.Unmarshal(f.raw, &list) != nil {
		return nil
	}
	out := list[:0]
	for _, b := range list {
		if b.Type != "" && slackBlockTypes[b.Type] { // a nested unknown block doesn't recurse
			out = append(out, b)
		}
	}
	return out
}

// badBlock marks a block that didn't decode.
const badBlock = "\x00bad"

// textBlocks converts a Block Kit text object.
func (c *builder) textBlocks(t *textObj) []Block {
	if t == nil || t.Text == "" {
		return nil
	}
	if t.Type == "mrkdwn" {
		return FromMrkdwn(c.r, t.Text)
	}
	if t.Emoji != nil && !*t.Emoji {
		return []Block{{Kind: Paragraph, Spans: plainSpansNoEmoji(t.Text)}}
	}
	return []Block{{Kind: Paragraph, Spans: plainSpans(c.r, t.Text)}}
}

func (c *builder) textSpans(t *textObj) []Span { return flatten(c.textBlocks(t)) }

// plainSpans renders plain_text: unescape, resolve :emoji:, newlines as Breaks.
func plainSpans(r Resolver, s string) []Span {
	var out []Span
	for i, ln := range strings.Split(s, "\n") {
		if i > 0 {
			out = append(out, Span{Kind: Break})
		}
		out = append(out, emojify(r, unescape(ln), 0)...)
	}
	return out
}

// plainSpansNoEmoji is plain_text with "emoji": false: :name: stays text.
func plainSpansNoEmoji(s string) []Span {
	var out []Span
	for i, ln := range strings.Split(s, "\n") {
		if i > 0 {
			out = append(out, Span{Kind: Break})
		}
		if ln = unescape(ln); ln != "" {
			out = append(out, Span{Kind: Text, Text: ln})
		}
	}
	return out
}

// emojify splits unescaped text into Text and resolved Emoji spans.
func emojify(r Resolver, s string, st Style) []Span {
	if s == "" {
		return nil
	}
	var out []Span
	last := 0
	for i := 0; i < len(s); i++ {
		if s[i] != ':' {
			continue
		}
		name, n := emojiAt(s, i)
		if n == 0 {
			continue
		}
		sp, ok := emojiSpan(r, name)
		if !ok {
			continue
		}
		if i > last {
			out = append(out, Span{Kind: Text, Text: s[last:i], Style: st})
		}
		sp.Style = st
		out = append(out, sp)
		i += n - 1
		last = i + 1
	}
	if last < len(s) {
		out = append(out, Span{Kind: Text, Text: s[last:], Style: st})
	}
	return out
}

// flatten joins the inline content of blocks with Breaks (for places that
// only hold spans: fields, pretext, footers, context).
func flatten(bs []Block) []Span {
	if len(bs) == 1 && bs[0].Kind != List {
		return bs[0].Spans
	}
	var out []Span
	add := func(ss []Span) {
		if len(out) > 0 {
			out = append(out, Span{Kind: Break})
		}
		out = append(out, ss...)
	}
	for _, b := range bs {
		switch b.Kind {
		case List:
			for i, it := range b.Items {
				bullet := "• "
				if b.Ordered {
					bullet = strconv.Itoa(b.Offset+i+1) + ". "
				}
				add(append([]Span{{Kind: Text, Text: strings.Repeat("    ", b.Indent) + bullet}}, it...))
			}
		default:
			if len(b.Spans) > 0 {
				add(b.Spans)
			}
		}
	}
	return out
}

// blocks converts a list of Block Kit blocks.
func (c *builder) blocks(bs []bk) []Block {
	var out []Block
	for i := range bs {
		n := len(out)
		out = c.block(out, &bs[i])
		if kitLayout(bs[i].Type) {
			for j := n; j < len(out); j++ {
				out[j].Kit = true
			}
		}
	}
	return out
}

// kitLayout reports whether t is a Block Kit layout block richview draws
// with Slack's p-*_block margins (rich_text has none of its own).
func kitLayout(t string) bool {
	switch t {
	case "section", "context", "header", "divider", "image", "actions", "table", "input", badBlock:
		return true
	}
	return false
}

// slackBlockTypes is every block type Slack's client knows (its block
// type enum, client-boot, build a84662d2d135); any other type draws
// UnsupportedBlockFallback (UnknownBlock).
var slackBlockTypes = map[string]bool{}

func init() {
	for _, t := range strings.Fields(`callout canvas_table context context_actions section image divider actions file
		message expandable canvas error rich_text call input group header prototype table loading event contact_card
		share_shortcut share_prompt share_skill share_workflow video message_embed condition channel_set channel_section
		timeline plan task_card alert card related_automation_decorator carousel data_table container
		salesforce_record_card search_top_results mcp_app data_visualization layout page auth_provider
		attachment_reference fallback_canary markdown`) {
		slackBlockTypes[t] = true
	}
}

// unsupported is Slack's per-block error fallback.
func unsupported() Block {
	return Block{Kind: Unsupported, Spans: []Span{{Kind: Text, Text: "This content could not be displayed."}}}
}

func (c *builder) block(out []Block, b *bk) []Block {
	switch b.Type {
	case "rich_text":
		return append(out, c.richText(b.Elements)...)
	case "section":
		blk := Block{Kind: Section, BlockID: b.BlockID, Expand: b.Expand}
		tb := c.textBlocks(b.textObj())
		if len(tb) == 1 && tb[0].Kind == Paragraph {
			blk.Spans = tb[0].Spans
		} else {
			blk.Children = tb
		}
		for i := range b.Fields {
			blk.Fields = append(blk.Fields, c.textSpans(&b.Fields[i]))
			blk.FieldShort = append(blk.FieldShort, true)
		}
		if a := b.Accessory; a != nil {
			if a.Type == "image" {
				blk.Image = c.imageOf(a)
			} else if btn, ok := c.element(a, b.BlockID); ok {
				blk.Buttons = append(blk.Buttons, btn)
			}
		}
		if blk.Spans == nil && blk.Children == nil && blk.Fields == nil && blk.Image == nil && blk.Buttons == nil {
			return out
		}
		return append(out, blk)
	case "context":
		// Slack's Context is a wrapping flex row of its elements (each
		// image or text its own box): Children keeps them apart, Spans
		// has them joined for plain-text consumers.
		blk := Block{Kind: Context}
		for i := range b.Elements {
			e := &b.Elements[i]
			var ss []Span
			if e.Type == "image" {
				if img := c.imageOf(e); img != nil {
					ss = []Span{{Kind: InlineImage, Text: e.AltText, URL: img.URL}}
					blk.Children = append(blk.Children, Block{Kind: ImageBlock, Image: img})
				}
			} else {
				t := &textObj{Type: e.Type, Text: e.textString()}
				if o := e.textObj(); o != nil {
					t.Emoji = o.Emoji
				}
				ss = c.textSpans(t)
				if len(ss) > 0 {
					blk.Children = append(blk.Children, Block{Kind: Paragraph, Spans: ss})
				}
			}
			if len(ss) == 0 {
				continue
			}
			if len(blk.Spans) > 0 {
				blk.Spans = append(blk.Spans, Span{Kind: Text, Text: " "})
			}
			blk.Spans = append(blk.Spans, ss...)
		}
		if len(blk.Spans) == 0 {
			return out
		}
		return append(out, blk)
	case "header":
		ss := c.textSpans(b.textObj())
		if len(ss) == 0 {
			return out
		}
		return append(out, Block{Kind: Header, Spans: ss})
	case "divider":
		return append(out, Block{Kind: Divider})
	case "image":
		img := c.imageOf(b)
		if img == nil {
			return append(out, unsupported())
		}
		if b.Title != nil {
			img.Title = unescape(b.Title.Text)
		}
		img.Bytes = b.ImageBytes
		return append(out, Block{Kind: ImageBlock, Image: img})
	case "actions":
		blk := Block{Kind: Actions}
		for i := range b.Elements {
			if btn, ok := c.element(&b.Elements[i], b.BlockID); ok {
				blk.Buttons = append(blk.Buttons, btn)
			}
		}
		if len(blk.Buttons) == 0 {
			return out
		}
		return append(out, blk)
	case "video":
		a := &Attachment{
			ServiceName: b.ProviderName, ServiceIcon: SafeImage(b.ProviderIconURL), AuthorName: b.AuthorName,
			TitleLink: SafeURL(b.TitleURL), VideoURL: SafeURL(b.VideoURL), IsUnfurl: true,
			Image: image(b.ThumbnailURL, b.AltText, 0, 0),
		}
		if b.Title != nil {
			a.Title = plainSpans(c.r, b.Title.Text)
		}
		a.Text = c.textBlocks(b.Description)
		return append(out, Block{Kind: AttachmentBlock, Att: a})
	case "file":
		id := firstNonEmpty(b.FileID, b.ExternalID)
		if f, ok := c.files[id]; ok && !c.used[id] {
			c.used[id] = true
			return append(out, c.fileBlock(f))
		}
		if id == "" {
			return out
		}
		return append(out, Block{Kind: FileBlock, File: &File{ID: id, Type: b.Source}})
	case "call":
		a := &Attachment{ServiceName: "Call", IsUnfurl: true}
		name := "Call"
		if b.Call != nil {
			name = firstNonEmpty(b.Call.V1.Name, name)
			a.TitleLink = SafeURL(b.Call.V1.JoinURL)
			a.ServiceIcon = SafeImage(b.Call.V1.AppIconURLs.Image32)
			a.TS = b.Call.V1.DateStart
			if b.Call.V1.DateEnd > 0 {
				a.Text = []Block{{Kind: Paragraph, Spans: []Span{{Kind: Text, Text: "Ended"}}}}
			}
		}
		a.Title = []Span{{Kind: Text, Text: name}}
		return append(out, Block{Kind: AttachmentBlock, Att: a})
	case "input":
		blk := Block{Kind: Section, Spans: c.textSpans(b.Label)}
		for i := range blk.Spans {
			blk.Spans[i].Style |= Bold
		}
		if b.Element != nil {
			if btn, ok := c.element(b.Element, b.BlockID); ok {
				blk.Buttons = append(blk.Buttons, btn)
			}
		}
		return append(out, blk)
	case "markdown":
		return append(out, markdownBlocks(c.r, b.textString())...)
	case "table":
		return append(out, c.table(b))
	case "plan", "task_card", "context_actions":
		return c.agentBlock(out, b)
	case badBlock:
		return append(out, unsupported())
	}
	if !slackBlockTypes[b.Type] {
		// SUIRendererItem: no renderer for the type, so the block's own
		// fallback payload (a block or a list), else UnsupportedBlockFallback
		if fb := b.Fallback.blocks(); len(fb) > 0 {
			return append(out, c.blocks(fb)...)
		}
		return append(out, Block{Kind: UnknownBlock, Spans: []Span{{Kind: Text, Text: "This message can’t be fully displayed."}}})
	}
	if t := b.textObj(); t != nil {
		return append(out, c.textBlocks(t)...)
	}
	return out
}

// markdownBlocks converts a "markdown" block (standard Markdown, which
// Slack draws with its rich text styles): ATX headings in bold, - * + and
// 1. lists (nested by two-space indents), ``` fences, > quotes, and the
// inline differences from mrkdwn (**bold** / __bold__, *italic*,
// ~~strike~~, [label](url), ![alt](url)).
func markdownBlocks(r Resolver, s string) []Block {
	var out []Block
	var para []string
	flush := func() {
		if len(para) > 0 {
			out = append(out, FromMrkdwn(r, strings.Join(para, "\n"))...)
			para = para[:0]
		}
	}
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		ln := lines[i]
		t := strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(t, "```"):
			flush()
			var code []string
			for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```"); i++ {
				code = append(code, lines[i])
			}
			out = append(out, Block{Kind: Preformatted, Spans: preSpans(r, strings.Join(code, "\n"))})
		case mdHeadRe.MatchString(t):
			flush()
			h := mdHeadRe.FindStringSubmatch(t)[1]
			ss := flatten(FromMrkdwn(r, mdInline(h)))
			for k := range ss {
				ss[k].Style |= Bold
			}
			out = append(out, Block{Kind: Paragraph, Spans: ss})
		case mdItemRe.MatchString(ln):
			flush()
			m := mdItemRe.FindStringSubmatch(ln)
			indent := len(strings.ReplaceAll(m[1], "\t", "  ")) / 2
			ordered := m[2] != "-" && m[2] != "*" && m[2] != "+"
			item := flatten(FromMrkdwn(r, mdInline(m[3])))
			if n := len(out); n > 0 && out[n-1].Kind == List && out[n-1].Indent == indent && out[n-1].Ordered == ordered {
				out[n-1].Items = append(out[n-1].Items, item)
				continue
			}
			blk := Block{Kind: List, Ordered: ordered, Indent: indent, Items: [][]Span{item}}
			if ordered {
				if v, err := strconv.Atoi(strings.TrimRight(m[2], ".)")); err == nil && v > 1 {
					blk.Offset = v - 1
				}
			}
			out = append(out, blk)
		case t == "":
			flush()
		default:
			para = append(para, mdInline(ln))
		}
	}
	flush()
	return out
}

var (
	mdHeadRe   = regexp.MustCompile(`^#{1,6}\s+(.*?)\s*#*$`)
	mdItemRe   = regexp.MustCompile(`^(\s*)([-*+]|\d{1,9}[.)])\s+(.*)$`)
	mdBoldRe   = regexp.MustCompile(`\*\*([^*\n]+)\*\*|__([^_\n]+)__`)
	mdItalRe   = regexp.MustCompile(`(^|[^*\w])\*([^*\s](?:[^*\n]*[^*\s])?)\*($|[^*\w])`)
	mdStrikeRe = regexp.MustCompile(`~~([^~\n]+)~~`)
	mdImageRe  = regexp.MustCompile(`!\[([^\]\n|<>]*)\]\((https?://[^)\s<>|]+)\)`)
	mdLinkRe   = regexp.MustCompile(`\[([^\]\n|<>]+)\]\((https?://[^)\s<>|]+)\)`)
)

// mdInline rewrites one line of Markdown inline syntax as mrkdwn, leaving
// `code` spans alone.
func mdInline(s string) string {
	parts := strings.Split(s, "`")
	for i := 0; i < len(parts); i += 2 { // even parts are outside code
		if i == len(parts)-1 && i > 0 && len(parts)%2 == 0 {
			break // an unclosed backtick: the rest is literal
		}
		p := parts[i]
		p = mdBoldRe.ReplaceAllStringFunc(p, func(m string) string { return "\x01" + m[2:len(m)-2] + "\x01" })
		p = mdItalRe.ReplaceAllString(p, "${1}_${2}_${3}")
		p = strings.ReplaceAll(p, "\x01", "*")
		p = mdStrikeRe.ReplaceAllString(p, "~$1~")
		p = mdImageRe.ReplaceAllString(p, "<$2|$1>")
		p = mdLinkRe.ReplaceAllString(p, "<$2|$1>")
		parts[i] = p
	}
	return strings.Join(parts, "`")
}

func (c *builder) imageOf(b *bk) *Image {
	u := b.ImageURL
	if u == "" && b.SlackFile != nil {
		u = b.SlackFile.URL
	}
	return image(u, b.AltText, b.ImageWidth, b.ImageHeight)
}

// element converts an interactive element into a Button.
func (c *builder) element(e *bk, blockID string) (Button, bool) {
	btn := Button{Kind: e.Type, ActionID: e.ActionID, Value: e.Value, BlockID: blockID}
	switch e.Type {
	case "button", "workflow_button":
		btn.Kind = "button"
		btn.Text = PlainText(Doc{Blocks: []Block{{Kind: Paragraph, Spans: c.textSpans(e.textObj())}}})
		btn.URL = SafeURL(e.URL)
		if s := e.styleString(); s == "primary" || s == "danger" {
			btn.Style = s
		}
	case "overflow":
		btn.Text = "⋮"
	case "icon_button":
		// drawn by blockkit (a 28px icon with the text as its tooltip)
		btn.Text = PlainText(Doc{Blocks: []Block{{Kind: Paragraph, Spans: c.textSpans(e.textObj())}}})
	case "static_select", "external_select", "users_select", "conversations_select", "channels_select",
		"multi_static_select", "multi_external_select", "multi_users_select", "multi_conversations_select", "multi_channels_select":
		btn.Kind = "select"
		btn.Text = "Select an item"
		if e.InitialOption != nil && e.InitialOption.Text.Text != "" {
			btn.Text = e.InitialOption.Text.Text
		} else if e.Placeholder != nil && e.Placeholder.Text != "" {
			btn.Text = e.Placeholder.Text
		}
	case "datepicker", "timepicker", "datetimepicker":
		btn.Text = map[string]string{"datepicker": "Select a date", "timepicker": "Select time", "datetimepicker": "Select date and time"}[e.Type]
		if e.Placeholder != nil && e.Placeholder.Text != "" {
			btn.Text = e.Placeholder.Text
		}
	case "plain_text_input", "email_text_input", "url_text_input", "number_input", "rich_text_input":
		btn.Kind = "input"
		if e.Placeholder != nil {
			btn.Text = e.Placeholder.Text
		}
	case "checkboxes", "radio_buttons":
		var names []string
		for _, o := range e.Options {
			names = append(names, o.Text.Text)
		}
		btn.Text = strings.Join(names, " · ")
	default:
		return Button{}, false
	}
	btn.Text = unescape(btn.Text)
	return btn, true
}

// richText converts the elements of a rich_text block.
func (c *builder) richText(els []bk) []Block {
	var out []Block
	for i := range els {
		e := &els[i]
		switch e.Type {
		case "rich_text_section":
			if ss := c.richInline(e.Elements, false); len(ss) > 0 {
				out = append(out, Block{Kind: Paragraph, Spans: ss})
			}
		case "rich_text_preformatted":
			out = append(out, Block{Kind: Preformatted, Spans: c.richInline(e.Elements, true), Border: e.Border > 0})
		case "rich_text_quote":
			out = append(out, Block{Kind: Quote, Spans: c.richInline(e.Elements, false), Border: e.Border > 0})
		case "rich_text_list":
			blk := Block{Kind: List, Ordered: e.styleString() == "ordered", Indent: e.Indent, Offset: e.Offset, Border: e.Border > 0}
			for j := range e.Elements {
				it := &e.Elements[j]
				if it.Type == "rich_text_section" {
					blk.Items = append(blk.Items, c.richInline(it.Elements, false))
				} else {
					blk.Items = append(blk.Items, c.richInline([]bk{*it}, false))
				}
			}
			out = append(out, blk)
		default:
			if ss := c.richInline([]bk{*e}, false); len(ss) > 0 {
				out = append(out, Block{Kind: Paragraph, Spans: ss})
			}
		}
	}
	return out
}

var hexColorRe = regexp.MustCompile(`^#?[0-9a-fA-F]{3,8}$`)

// richInline converts rich_text inline elements. In pre blocks styles other
// than links are dropped (Slack shows them monospace and unstyled).
func (c *builder) richInline(els []bk, pre bool) []Span {
	var out []Span
	for i := range els {
		e := &els[i]
		st, unlink := e.style()
		if pre {
			st = 0
		}
		n := len(out)
		switch e.Type {
		case "text":
			for k, ln := range strings.Split(e.textString(), "\n") {
				if k > 0 {
					out = append(out, Span{Kind: Break})
				}
				if ln != "" {
					out = append(out, Span{Kind: Text, Text: ln})
				}
			}
		case "link":
			label := firstNonEmpty(e.textString(), strings.TrimPrefix(e.URL, "mailto:"))
			if unlink {
				out = append(out, Span{Kind: Text, Text: label})
			} else {
				out = append(out, linkSpan(e.URL, label))
			}
		case "user":
			out = append(out, userSpan(c.r, e.UserID, ""))
		case "usergroup":
			out = append(out, usergroupSpan(c.r, e.UsergroupID, ""))
		case "channel":
			sp := channelSpan(c.r, e.ChannelID, "")
			if e.TabID != "" { // a channel-tab slug: getChannelTabPermalink
				sp.URL = "/archives/" + e.ChannelID + "/tabs/" + e.TabID
			}
			out = append(out, sp)
		case "emoji":
			out = append(out, richEmoji(c.r, e))
		case "message_mention":
			out = append(out, messageMentionSpan(c.r, e))
		case "broadcast":
			out = append(out, broadcastSpan(firstNonEmpty(e.Range, "here")))
		case "date":
			sec := tsSeconds(e.Timestamp)
			sp := Span{Kind: Date, Text: e.Fallback.Text, ID: strconv.FormatInt(sec, 10), URL: SafeURL(e.URL)}
			if sec > 0 && e.Format != "" {
				sp.Text = FormatDate(time.Unix(sec, 0), e.Format)
			}
			if sp.Text == "" {
				sp.Text = FormatDate(time.Unix(sec, 0), "{date_short_pretty} at {time}")
			}
			out = append(out, sp)
		case "color":
			sp := Span{Kind: Text, Text: e.Value}
			if hexColorRe.MatchString(e.Value) {
				sp.Kind = ColorSwatch
			}
			out = append(out, sp)
		case "team":
			out = append(out, Span{Kind: Text, Text: firstNonEmpty(e.Value, e.TeamID)})
		default:
			if t := e.textString(); t != "" {
				out = append(out, Span{Kind: Text, Text: t})
			}
		}
		for k := n; k < len(out); k++ {
			if out[k].Kind != Break {
				out[k].Style |= st
			}
		}
	}
	return out
}

func richEmoji(r Resolver, e *bk) Span {
	name := e.Name
	if e.SkinTone >= 2 && e.SkinTone <= 6 {
		name += "::skin-tone-" + strconv.Itoa(e.SkinTone)
	}
	if sp, ok := emojiSpan(r, name); ok {
		return sp
	}
	if name != e.Name {
		if sp, ok := emojiSpan(r, e.Name); ok {
			return sp
		}
	}
	if t := unicodeText(e.Unicode); t != "" {
		return Span{Kind: Emoji, Text: t, ID: name}
	}
	return Span{Kind: Text, Text: ":" + name + ":"}
}

// unicodeText turns "1f44d-1f3fb" into the emoji's characters.
func unicodeText(u string) string {
	if u == "" {
		return ""
	}
	var b strings.Builder
	for _, p := range strings.Split(u, "-") {
		v, err := strconv.ParseUint(p, 16, 32)
		if err != nil || v > 0x10FFFF {
			return ""
		}
		b.WriteRune(rune(v))
	}
	return b.String()
}

// richOnlyEmoji reports whether rich_text blocks hold only 1..23 emoji.
func richOnlyEmoji(bs []bk) bool {
	n := 0
	for _, b := range bs {
		if b.Type != "rich_text" {
			return false
		}
		for _, sec := range b.Elements {
			if sec.Type != "rich_text_section" {
				return false
			}
			for _, el := range sec.Elements {
				switch el.Type {
				case "emoji":
					n++
				case "text":
					if strings.TrimSpace(el.textString()) != "" {
						return false
					}
				default:
					return false
				}
			}
		}
	}
	return n > 0 && n <= 23
}

// table converts a Block Kit table block. Cells are raw_text or rich_text.
func (c *builder) table(b *bk) Block {
	blk := Block{Kind: Table}
	for _, cs := range b.ColumnSet {
		blk.ColAlign = append(blk.ColAlign, cs.Align)
		blk.ColWrap = append(blk.ColWrap, cs.IsWrapped)
	}
	for _, row := range b.Rows {
		cells := make([][]Span, 0, len(row))
		for i := range row {
			cells = append(cells, c.tableCell(&row[i]))
		}
		blk.Rows = append(blk.Rows, cells)
	}
	return blk
}

func (c *builder) tableCell(cell *bk) []Span {
	switch cell.Type {
	case "rich_text":
		return flattenSpans(c.richText(cell.Elements))
	case "raw_text", "plain_text":
		if t := cell.textString(); t != "" {
			return []Span{{Kind: Text, Text: t}}
		}
		return nil
	}
	if t := cell.textObj(); t != nil {
		return flattenSpans(c.textBlocks(t))
	}
	return nil
}

// flattenSpans joins the inline content of blocks with hard breaks.
func flattenSpans(bs []Block) []Span {
	var out []Span
	for _, b := range bs {
		var sp []Span
		switch {
		case len(b.Items) > 0:
			for i, it := range b.Items {
				if i > 0 {
					sp = append(sp, Span{Kind: Break})
				}
				sp = append(sp, Span{Kind: Text, Text: "• "})
				sp = append(sp, it...)
			}
		default:
			sp = b.Spans
		}
		if len(sp) == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, Span{Kind: Break})
		}
		out = append(out, sp...)
	}
	return out
}
