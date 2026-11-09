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
	Fallback    string          `json:"fallback"`
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
	Emoji    bool   `json:"emoji"`
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
	if json.Unmarshal(raw, &bs) != nil {
		return nil
	}
	return bs
}

// textBlocks converts a Block Kit text object.
func (c *builder) textBlocks(t *textObj) []Block {
	if t == nil || t.Text == "" {
		return nil
	}
	if t.Type == "mrkdwn" {
		return FromMrkdwn(c.r, t.Text)
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
		out = c.block(out, &bs[i])
	}
	return out
}

func (c *builder) block(out []Block, b *bk) []Block {
	switch b.Type {
	case "rich_text":
		return append(out, c.richText(b.Elements)...)
	case "section":
		blk := Block{Kind: Section}
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
		blk := Block{Kind: Context}
		for i := range b.Elements {
			e := &b.Elements[i]
			var ss []Span
			if e.Type == "image" {
				if img := c.imageOf(e); img != nil {
					ss = []Span{{Kind: InlineImage, Text: e.AltText, URL: img.URL}}
				}
			} else {
				ss = c.textSpans(&textObj{Type: e.Type, Text: e.textString()})
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
			return out
		}
		if b.Title != nil {
			img.Title = unescape(b.Title.Text)
		}
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
		return append(out, FromMrkdwn(c.r, markdownToMrkdwn(b.textString()))...)
	case "table":
		return append(out, c.table(b))
	}
	if t := b.textObj(); t != nil {
		return append(out, c.textBlocks(t)...)
	}
	return out
}

var mdBoldRe = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)

// markdownToMrkdwn handles the common differences of the "markdown" block
// (standard Markdown) from mrkdwn: **bold** and [label](url).
func markdownToMrkdwn(s string) string {
	s = mdBoldRe.ReplaceAllString(s, "*$1*")
	return mdLinkRe.ReplaceAllString(s, "<$2|$1>")
}

var mdLinkRe = regexp.MustCompile(`\[([^\]\n|<>]+)\]\((https?://[^)\s<>|]+)\)`)

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
			name := firstNonEmpty(c.r.ChannelName(e.ChannelID), e.ChannelID)
			out = append(out, Span{Kind: Channel, Text: "#" + strings.TrimPrefix(name, "#"), ID: e.ChannelID})
		case "emoji":
			out = append(out, richEmoji(c.r, e))
		case "message_mention":
			out = append(out, messageMentionSpan(c.r, e))
		case "broadcast":
			out = append(out, broadcastSpan(firstNonEmpty(e.Range, "here")))
		case "date":
			sec := tsSeconds(e.Timestamp)
			sp := Span{Kind: Date, Text: e.Fallback, ID: strconv.FormatInt(sec, 10), URL: SafeURL(e.URL)}
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
