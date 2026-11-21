package richtext

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/some-du6e/clack/internal/emoji"
	"github.com/some-du6e/clack/internal/slack"
)

// bk is a generic Block Kit block / element / rich_text node.
type bk struct {
	bkAgent          // plan, task_card (agent.go)
	Type      string `json:"type"`
	BlockID   string `json:"block_id"`
	Elements  []bk   `json:"elements"`
	Rows      bkRows `json:"rows"` // table
	ColumnSet []struct {
		Align     string `json:"align"`
		IsWrapped bool   `json:"is_wrapped"`
	} `json:"column_settings"` // table
	Text        json.RawMessage `json:"text"` // string (rich_text) or text object
	Style       json.RawMessage `json:"style"`
	URL         string          `json:"url"`
	Truncated   bool            `json:"truncated"` // link: a link slug's label (Slack draws a chip)
	UserID      string          `json:"user_id"`
	ChannelID   string          `json:"channel_id"`
	TabID       string          `json:"tab_id"`     // channel: a channel-tab slug
	FromLLM     bool            `json:"from_llm"`   // channel: written by an LLM (no tab slug)
	MessageTS   string          `json:"message_ts"` // message_mention
	SectionID   string          `json:"section_id"` // canvas: a canvas-section link
	HideTitle   bool            `json:"hide_title"` // canvas
	RecordID    string          `json:"record_id"`  // list_record: an item link
	ViewID      string          `json:"view_id"`    // list_record: a list view link
	AuthorID    string          `json:"author_id"`  // message_mention
	UsergroupID string          `json:"usergroup_id"`
	TeamID      string          `json:"team_id"`
	Name        bkStr           `json:"name"` // emoji; auth_provider's is a text object
	Unicode     string          `json:"unicode"`
	SkinTone    int             `json:"skin_tone"`
	Range       string          `json:"range"`
	Timestamp   json.RawMessage `json:"timestamp"`
	Format      string          `json:"format"`
	Fallback    bkFallback      `json:"fallback"`
	Value       bkStr           `json:"value"` // a string (buttons, colors) or a number (raw_number cells)
	Indent      int             `json:"indent"`
	Offset      int             `json:"offset"`
	Border      int             `json:"border"`
	Fields      []textObj       `json:"fields"`
	Accessory   *bk             `json:"accessory"`
	ImageURL    string          `json:"image_url"`
	AltText     string          `json:"alt_text"`
	Title       *textObj        `json:"title"`
	ActionID    string          `json:"action_id"`
	VisibleTo   []string        `json:"visible_to_user_ids"` // button, checkboxes, icon_button (actionHidden)
	Checked     *bool           `json:"checked"`             // a check list's, or one of its items'
	Checklist   bool            `json:"checklist"`
	// a citation element's details.citation_type (either spelling): only
	// then does Slack's Citation take it (unknownInline)
	CitationType      json.RawMessage `json:"citation_type"`
	CitationTypeCamel json.RawMessage `json:"citationType"`
	Placeholder       *textObj        `json:"placeholder"`
	Options           []struct {
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
	Level      bkLevel  `json:"level"`  // header: 1–4 (Slack's Header: p-header_block--level_N); alert: a severity (kitcards.go)
	CallID     string   `json:"call_id"`
	Call       *bkCall  `json:"call"` // call.go
	// callout, container (kitcards.go)
	ChildBlocks      []bk            `json:"child_blocks"`
	BackgroundColor  string          `json:"background_color"`
	RichTextTitle    *bk             `json:"rich_text_title"`
	Subtitle         *textObj        `json:"subtitle"`
	Icon             json.RawMessage `json:"icon"`  // container: an image element (icon_button: a name)
	Width            json.RawMessage `json:"width"` // container: a width class (canvas_table cells: px)
	IsCollapsible    bool            `json:"is_collapsible"`
	DefaultCollapsed bool            `json:"default_collapsed"`
	HasHeaderDivider bool            `json:"has_header_divider"`

	bkWorkflow // workflow_mention (workflow_mention.go)
	bkShortcut // share_shortcut (shortcut.go)
	bkCard     // card (card.go)
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
	r      Resolver
	files  map[string]*slack.File // message files by id, for file blocks
	used   map[string]bool        // files already placed by a file block
	hints  canvasHints            // the message's canvas unfurls (canvasslug.go)
	sender string                 // the message's user (canvasfile.go: a file's owner other than them)
	chID   string                 // the message's channel (tabslug.go: a slug to its own tab is the tab alone)
	// msgCh and msgTS are the message (its files' expandables)
	msgCh, msgTS string
	// depth is how deep resolveFallback is in fallbacks, noCard how deep
	// in callouts' children (no fallback card there, fallback.go)
	depth, noCard int
	// titles is how many canvas titles deep the builder is (titleBlocks)
	titles int
	// containers counts the message's container blocks (Container.Key)
	containers int
	// missing: a block Slack draws but Clack can't yet drew nothing (the
	// message text then stands in, FromMessageIn)
	missing bool
	// notify converts for a notification's text (NotificationText): a
	// type with no renderable adds nothing, with no fallback chain
	notify bool
	// keepEmoji: an emoji element the resolver doesn't know is an Emoji
	// span without a URL, not its ":name:" text (CanvasTitle)
	keepEmoji bool
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
// block or a list; resolveFallback, fallback.go).
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

// bkRows is a table's "rows": arrays of cells, decoded as strictly as
// before. Other blocks' rows aren't arrays (canvas_table's and layout's
// are objects): those are no rows rather than a failed block.
type bkRows [][]bk

func (r *bkRows) UnmarshalJSON(b []byte) error {
	var rows []json.RawMessage
	if err := json.Unmarshal(b, &rows); err != nil {
		*r = nil
		return nil
	}
	for _, row := range rows {
		if len(row) == 0 || row[0] != '[' {
			*r = nil
			return nil
		}
	}
	var rs [][]bk
	if err := json.Unmarshal(b, &rs); err != nil {
		return err
	}
	*r = rs
	return nil
}

// bkStr is a field that is usually a string but may come as another
// JSON scalar (a raw_number cell's numeric "value"): it keeps the
// scalar's text instead of failing the whole block.
type bkStr string

func (v *bkStr) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*v = bkStr(s)
		return nil
	}
	if string(b) == "null" || len(b) == 0 || b[0] == '{' || b[0] == '[' {
		*v = ""
		return nil
	}
	*v = bkStr(b)
	return nil
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
					bullet = strconv.Itoa(ListNumber(&b, i)) + ". "
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
	case "section", "context", "header", "divider", "image", "actions", "table", "input", "call",
		"alert", "callout", "loading", "container", "share_shortcut", "card", badBlock:
		return true
	}
	return false
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
			// Section (bzDH) draws its accessory for every member; only
			// an icon_button hides itself (X2Vm). Slack keeps that one's
			// empty accessory box (8px left margin); Clack draws nothing.
			switch {
			case a.Type == "image":
				blk.Image = c.imageOf(a)
			case a.Type == "icon_button" && iconButtonHidden(a.VisibleTo, c.r.SelfID()):
			default:
				if btn, ok := c.element(a, b.BlockID); ok {
					blk.Buttons = append(blk.Buttons, btn)
				}
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
		return append(out, Block{Kind: Header, Spans: ss, Level: b.Level.N})
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
		self := c.r.SelfID()
		for i := range b.Elements {
			e := &b.Elements[i]
			if btn, ok := c.element(e, b.BlockID); ok {
				btn.Invisible = actionHidden(e, self)
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
		return append(out, c.callBlock(b))
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
	case "alert":
		return append(out, c.alertBlock(b))
	case "callout":
		return append(out, c.calloutBlock(b))
	case "loading":
		return append(out, Block{Kind: LoadingBlock})
	case "container":
		return append(out, c.containerBlock(b))
	case "share_shortcut":
		return append(out, c.shortcutBlock(b))
	case "card":
		return append(out, c.cardBlock(b))
	case "markdown":
		return append(out, markdownBlocks(c.r, b.textString())...)
	case "mrkdwn", "plain_text":
		// a text object as a block: the mrkdwn (nABn) and plain_text
		// (CnXr) renderables draw it, with no block margins
		t := &textObj{Type: b.Type, Text: b.textString()}
		if o := b.textObj(); o != nil {
			t = o
			t.Type = b.Type
		}
		return append(out, c.textBlocks(t)...)
	case "table":
		return append(out, c.table(b))
	case "plan", "task_card", "context_actions":
		return c.agentBlock(out, b)
	case badBlock:
		return append(out, unsupported())
	case "rich_text_section", "rich_text_list", "rich_text_quote", "rich_text_preformatted", "rich_text_header", "rich_text_divider":
		// a rich_text piece at the top level (or as a fallback payload)
		// draws through its own renderable, as inside a rich_text
		return append(out, c.richText([]bk{*b})...)
	}
	if !messageRenderables[b.Type] {
		if c.notify { // convertMessageToPlainText: no converter, no text
			return out
		}
		// SUIRendererItem: no renderable for the type (fallback.go)
		return c.resolveFallback(out, b)
	}
	// an inline element at the top level draws as it does in a section
	if inlineTypes[b.Type] {
		if bs := c.richText([]bk{{Type: "rich_text_section", Elements: []bk{*b}}}); len(bs) > 0 {
			return append(out, bs...)
		}
		return out
	}
	// a type Slack draws that Clack doesn't yet: its text object, if any
	// (docs/BLOCKKIT-COVERAGE.md)
	if t := b.textObj(); t != nil {
		return append(out, c.textBlocks(t)...)
	}
	c.missing = true
	return out
}

// inlineTypes are the inline rich text elements Slack's renderables take
// at any level (jBv3's list): one at the top level draws as in a section.
var inlineTypes = map[string]bool{}

func init() {
	for _, t := range strings.Fields(`text emoji link channel user usergroup broadcast color date variable workflowtoken
		workflow_mention work_object_mention tag canvas canvas_user_mention message_mention list_record skill rich_text_file
		folder_mention salesforce_data_field citation attachment_mention`) {
		inlineTypes[t] = true
	}
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
			indent := listIndent(len(strings.ReplaceAll(m[1], "\t", "  ")) / 2)
			ordered := m[2] != "-" && m[2] != "*" && m[2] != "+"
			item := flatten(FromMrkdwn(r, mdInline(m[3])))
			if n := len(out); n > 0 && out[n-1].Kind == List && out[n-1].Indent == indent && out[n-1].Ordered == ordered {
				out[n-1].Items = append(out[n-1].Items, item)
				continue
			}
			blk := Block{Kind: List, Ordered: ordered, Indent: indent, Items: [][]Span{item}}
			if ordered {
				if v, err := strconv.Atoi(strings.TrimRight(m[2], ".)")); err == nil && v > 1 {
					blk.Offset = listOffset(v - 1)
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

// actionHidden is whether an Actions block's element e isn't drawn for
// the viewer self (Button.Invisible). Only the Actions block reads
// visible_to_user_ids (boot-shared /jJF maybeRenderActionsElement): a
// button or checkboxes whose list is set, with a viewer known, who isn't
// in it. Section accessories and input elements draw for everyone. An
// icon_button hides itself wherever it is (iconButtonHidden).
func actionHidden(e *bk, self string) bool {
	switch e.Type {
	case "button", "checkboxes":
		return len(e.VisibleTo) > 0 && self != "" && !slices.Contains(e.VisibleTo, self)
	case "icon_button":
		return iconButtonHidden(e.VisibleTo, self)
	}
	return false
}

// iconButtonHidden is IconButton's own check (boot-shared X2Vm): a list
// that leaves out the current user, or any list while there's none.
func iconButtonHidden(ids []string, self string) bool {
	return len(ids) > 0 && (self == "" || !slices.Contains(ids, self))
}

// element converts an interactive element into a Button.
func (c *builder) element(e *bk, blockID string) (Button, bool) {
	btn := Button{Kind: e.Type, ActionID: e.ActionID, Value: string(e.Value), BlockID: blockID}
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
			// RichTextSection passes isFirst (its index is 0) down: its
			// first element is the first in the first section
			if ss := c.richInline(e.Elements, brSection(i == 0)); len(ss) > 0 {
				out = append(out, Block{Kind: Paragraph, Spans: ss})
			}
		case "rich_text_header":
			// RichTextHeader: nothing without elements; the level
			// classes 1–3 (4 draws as 3, anything else as 2)
			if len(e.Elements) > 0 {
				lv := e.Level.N
				if lv < 1 || lv > 4 {
					lv = 2
				}
				out = append(out, Block{Kind: RichHeader, Spans: c.richInline(e.Elements, brMode{lead: true}), Level: lv, First: i == 0})
			}
		case "rich_text_divider":
			out = append(out, Block{Kind: RichDivider})
		case "rich_text_preformatted":
			out = append(out, Block{Kind: Preformatted, Spans: c.richInline(e.Elements, brRaw), Border: e.Border > 0})
		case "rich_text_quote":
			out = append(out, Block{Kind: Quote, Spans: c.richInline(e.Elements, brSection(false)), Border: e.Border > 0})
		case "rich_text_list":
			blk := Block{Kind: List, Ordered: e.styleString() == "ordered", Indent: listIndent(e.Indent), Offset: listOffset(e.Offset), Border: e.Border > 0}
			// a check list (BS) draws checkboxes or, where they aren't
			// allowed, bullets: never the ordered markers
			blk.Check = e.Checklist || e.styleString() == "check"
			if blk.Check {
				blk.Ordered = false
			}
			for j := range e.Elements {
				it := &e.Elements[j]
				if it.Type == "rich_text_section" {
					blk.Items = append(blk.Items, c.richInline(it.Elements, brSection(false)))
				} else {
					blk.Items = append(blk.Items, c.richInline([]bk{*it}, brSection(false)))
				}
				if blk.Check { // e.checked ?? the list's
					on := it.Checked
					if on == nil {
						on = e.Checked
					}
					blk.Checked = append(blk.Checked, on != nil && *on)
				}
			}
			out = append(out, blk)
		default:
			// an inline element straight in the block: the first one is
			// isFirstElementInSection (index 0), never in a first section
			if ss := c.richInline([]bk{*e}, brMode{lead: i == 0}); len(ss) > 0 {
				out = append(out, Block{Kind: Paragraph, Spans: ss})
			}
		}
	}
	return out
}

var hexColorRe = regexp.MustCompile(`^#?[0-9a-fA-F]{3,8}$`)

// brMode is where a run of rich_text inline elements sits, for the
// newlines of its text elements (BrKind, Slack's BaseText).
type brMode struct {
	pre   bool // rich_text_preformatted: rawLineBreaks, plain line breaks
	lead  bool // element 0 is isFirstElementInSection
	first bool // element 0 is isFirstElementInFirstSection (trimStart)
}

var brRaw = brMode{pre: true}

// brSection is the elements of a section, a quote or a list item:
// RichTextRenderer's index-0 element is first in its section.
func brSection(first bool) brMode { return brMode{lead: true, first: first} }

// richInline converts rich_text inline elements. In pre blocks styles other
// than links are dropped (Slack shows them monospace and unstyled).
func (c *builder) richInline(els []bk, m brMode) []Span {
	pre := m.pre
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
			if pre {
				for k, ln := range strings.Split(e.textString(), "\n") {
					if k > 0 {
						out = append(out, Span{Kind: Break})
					}
					if ln != "" {
						out = append(out, Span{Kind: Text, Text: ln})
					}
				}
				break
			}
			out = appendRichText(out, e.textString(), i == 0 && m.lead, i == 0 && m.first)
		case "link":
			label := firstNonEmpty(e.textString(), strings.TrimPrefix(e.URL, "mailto:"))
			if unlink {
				out = append(out, Span{Kind: Text, Text: label})
			} else {
				sp := linkSpan(e.URL, label)
				// +W5o Link: a truncated link's text is drawn as it is
				// (getLinkContents), inside RichTextSlug's chip
				sp.Truncated = e.Truncated && sp.Kind == Link
				out = append(out, sp)
			}
		case "user":
			out = append(out, userSpan(c.r, e.UserID, ""))
		case "usergroup":
			out = append(out, usergroupSpan(c.r, e.UsergroupID, ""))
		case "channel":
			if e.TabID != "" && !e.FromLLM {
				if sp, ok := c.channelTabSpan(e); ok { // tabslug.go
					out = append(out, sp)
					break
				}
			}
			sp := channelSpan(c.r, e.ChannelID, "")
			if e.TabID != "" { // a channel-tab slug: getChannelTabPermalink
				sp.URL = "/archives/" + e.ChannelID + "/tabs/" + e.TabID
			}
			out = append(out, sp)
		case "emoji":
			sp := richEmoji(c.r, e)
			if c.keepEmoji && sp.Kind == Text {
				// an emoji the resolver can't name yet stays one (a
				// title converted before the emoji map loaded): the
				// surface resolves it when it draws (CanvasTitle)
				sp = Span{Kind: Emoji, ID: strings.Trim(sp.Text, ":"), Text: sp.Text}
			}
			out = append(out, sp)
		case "message_mention":
			out = append(out, messageMentionSpan(c.r, e))
		case "canvas":
			out = append(out, c.canvasSpan(e))
		case "file", "rich_text_file": // fileslug.go
			if sp, ok := c.fileSlugSpan(e); ok {
				out = append(out, sp)
			}
		case "workflow_mention": // workflow_mention.go
			if sp, ok := c.workflowMentionSpan(e); ok {
				out = append(out, sp)
			}
		case "list_record": // listslug.go
			if sp, ok := c.listRecordSpan(e); ok {
				out = append(out, sp)
			}
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
			sp := Span{Kind: Text, Text: string(e.Value)}
			if hexColorRe.MatchString(string(e.Value)) {
				sp.Kind = ColorSwatch
			}
			out = append(out, sp)
		default:
			out = append(out, c.unknownInline(e, m)...)
		}
		for k := n; k < len(out); k++ {
			if out[k].Kind != Break {
				out[k].Style |= st
			}
		}
	}
	return out
}

// richTextTypes are Slack's rich text element types (N8wa Lv's values):
// UnknownElement's text and url gates (BeP8, tlJF) refuse them.
var richTextTypes = map[string]bool{}

func init() {
	for _, t := range strings.Fields(`rich_text expanded_rich_text rich_text_section rich_text_header rich_text_divider
		rich_text_quote rich_text_preformatted rich_text_list text user usergroup channel broadcast emoji link date color
		workflowtoken truncation unknown file canvas message_mention workflow_mention canvas_message_unfurl list_record skill
		citation tag salesforce_data_field canvas_user_mention attachment_mention work_object_mention _edited_label
		_stopped_label _emoji_group _quote_group _theme_install_button _truncation_expand_button
		_truncation_collapse_button _file_mention _canvas_mention _unfurl_mention _canvas_user_mention _variable
		slackhorizontalrule`) {
		richTextTypes[t] = true
	}
}

// unknownInline draws an inline element none of Clack's renderers take.
//
// One Slack draws with its own component (messageRenderables) keeps its
// text and link here, save a citation without details.citation_type,
// which Slack's Citation doesn't take: it falls through to the last
// branches below, where Lv's gate leaves it empty.
//
// A type Slack has no renderer for either (team among them) gets
// RichTextRenderer's (J825) last branches: a null fallback draws
// nothing, an object fallback draws in its place (nesting up to
// maxFallbackDepth), else UnknownElement (Dvg3) with the element's text
// and url only when each is a string and the type isn't one of Slack's
// rich text types (BeP8, tlJF against richTextTypes): a link to the url,
// else the text, else nothing.
func (c *builder) unknownInline(e *bk, m brMode) []Span {
	if messageRenderables[e.Type] {
		if e.Type != "citation" || e.Details != nil && (len(e.Details.CitationType) > 0 || len(e.Details.CitationTypeCamel) > 0) {
			return unknownElement(e.textString(), e.URL)
		}
	}
	switch fb := e.Fallback.raw; {
	case string(fb) == "null":
		return nil
	case len(fb) > 0 && fb[0] == '{':
		if c.depth >= maxFallbackDepth {
			return nil
		}
		c.depth++
		defer func() { c.depth-- }()
		return c.richInline([]bk{decodeBlock(fb)}, m)
	}
	if richTextTypes[e.Type] {
		return nil
	}
	text := ""
	if len(e.Text) > 0 && e.Text[0] == '"' {
		text = e.textString()
	}
	return unknownElement(text, e.URL)
}

// unknownElement is UnknownElement (Dvg3): a link to url (labelled with
// the text, else the url), else the text, else nothing. Inside
// preformatted text the link stays a link.
func unknownElement(text, url string) []Span {
	switch {
	case url != "":
		return []Span{linkSpan(url, firstNonEmpty(text, url))}
	case text != "":
		return []Span{{Kind: Text, Text: text}}
	}
	return nil
}

// richRunRe is insertTextBreakPlaceholders' paragraph run (kWQh m): two
// or more newlines with only spaces, tabs and no-break spaces between.
var richRunRe = regexp.MustCompile(`(?:\n[ \t\x{00A0}]*)+\n`)

// appendRichText appends a rich_text text element's text: its lines as
// Text spans, its newlines as Breaks drawn the way Slack's BaseText
// draws them (BrKind). lead is isFirstElementInSection, first
// isFirstElementInFirstSection.
func appendRichText(out []Span, s string, lead, first bool) []Span {
	text := func(t string) {
		if t != "" {
			out = append(out, Span{Kind: Text, Text: t})
		}
	}
	brk := func(k BrKind) { out = append(out, Span{Kind: Break, Br: k}) }
	if first {
		// trimStart (h5n5): the leading whitespace is gone, so its
		// newlines draw nothing and the rest starts on the first line
		// (spaces at a line's start draw nothing either)
		ws := len(s) - len(strings.TrimLeftFunc(s, unicode.IsSpace))
		for _, ln := range strings.SplitAfter(s[:ws], "\n") {
			if t, ok := strings.CutSuffix(ln, "\n"); ok {
				text(t)
				brk(BrNone)
			} else {
				text(ln)
			}
		}
		s, lead = s[ws:], false
	}
	// isFirstElementInSection && /^\n/: one more "\n" in front (virt, no
	// span of its own), which makes a leading newline a run
	virt := lead && strings.HasPrefix(s, "\n")
	if virt {
		s = "\n" + s
	}
	for s != "" {
		loc := richRunRe.FindStringIndex(s)
		if loc == nil {
			loc = []int{len(s), len(s)}
		}
		// before the run: single newlines are <br>
		for k, ln := range strings.Split(s[:loc[0]], "\n") {
			if k > 0 {
				brk(BrLine)
			}
			text(ln)
		}
		if loc[0] == loc[1] {
			break
		}
		// the run: k newlines are k-1 c-mrkdwn__br. The one left over
		// (the added "\n", else the last) draws nothing; the spaces
		// between them stay text at the start of an empty line, where
		// they draw nothing either.
		run := s[loc[0]:loc[1]]
		k := strings.Count(run, "\n")
		for j, ln := range strings.Split(run, "\n") {
			switch {
			case j == 0: // "" before the run's first "\n"
			case virt && j == 1: // the added "\n"
			case !virt && j == k:
				brk(BrNone)
			default:
				brk(BrPara)
			}
			text(ln)
		}
		s, virt = s[loc[1]:], false
	}
	return out
}

func richEmoji(r Resolver, e *bk) Span {
	name := string(e.Name)
	if e.SkinTone >= 2 && e.SkinTone <= 6 {
		name += "::skin-tone-" + strconv.Itoa(e.SkinTone)
	}
	// Slack's Emoji component (vbPY) names an element's emoji from its
	// unicode (getEmojiNameFromUnicodeString). A unicode the standard set
	// in use doesn't list (a Unicode 17 emoji or tone with emoji_v17 off)
	// gets no name, so Slack draws the characters (renderUnicodeAsEmoji),
	// never the CDN image or a custom emoji that shares the name.
	if emoji.Hidden(e.Unicode) {
		if t := unicodeText(e.Unicode); t != "" {
			return Span{Kind: Emoji, Text: t, ID: name}
		}
	}
	if sp, ok := emojiSpan(r, name); ok {
		return sp
	}
	if name != string(e.Name) {
		if sp, ok := emojiSpan(r, string(e.Name)); ok {
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
	case "raw_text", "plain_text", "raw_number", "cell_text":
		// bx/I renders raw_text, cell_text and raw_number cells by their
		// text (BaseText)
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
