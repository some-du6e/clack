package render

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Block is a generic Block Kit block / element node.
type Block struct {
	Type        string          `json:"type"`
	BlockID     string          `json:"block_id,omitempty"`
	Elements    []Block         `json:"elements,omitempty"`
	Text        json.RawMessage `json:"text,omitempty"` // string (rich_text) or text object
	Style       json.RawMessage `json:"style,omitempty"`
	URL         string          `json:"url,omitempty"`
	UserID      string          `json:"user_id,omitempty"`
	ChannelID   string          `json:"channel_id,omitempty"`
	UsergroupID string          `json:"usergroup_id,omitempty"`
	Name        string          `json:"name,omitempty"`
	Unicode     string          `json:"unicode,omitempty"`
	SkinTone    int             `json:"skin_tone,omitempty"`
	Range       string          `json:"range,omitempty"`
	Timestamp   json.RawMessage `json:"timestamp,omitempty"`
	Format      string          `json:"format,omitempty"`
	Fallback    string          `json:"fallback,omitempty"`
	Value       string          `json:"value,omitempty"`
	Indent      int             `json:"indent,omitempty"`
	Offset      int             `json:"offset,omitempty"`
	Border      int             `json:"border,omitempty"`
	Fields      []TextObj       `json:"fields,omitempty"`
	Accessory   *Block          `json:"accessory,omitempty"`
	ImageURL    string          `json:"image_url,omitempty"`
	AltText     string          `json:"alt_text,omitempty"`
	Title       *TextObj        `json:"title,omitempty"`
	ActionID    string          `json:"action_id,omitempty"`
	Placeholder *TextObj        `json:"placeholder,omitempty"`
	Options     []struct {
		Text  TextObj `json:"text"`
		Value string  `json:"value"`
	} `json:"options,omitempty"`
	ImageWidth   int      `json:"image_width,omitempty"`
	ImageHeight  int      `json:"image_height,omitempty"`
	VideoURL     string   `json:"video_url,omitempty"`
	ThumbnailURL string   `json:"thumbnail_url,omitempty"`
	AuthorName   string   `json:"author_name,omitempty"`
	ProviderName string   `json:"provider_name,omitempty"`
	TitleURL     string   `json:"title_url,omitempty"`
	Description  *TextObj `json:"description,omitempty"`
	SlackFile    *struct {
		URL string `json:"url"`
		ID  string `json:"id"`
	} `json:"slack_file,omitempty"`
	Label      *TextObj `json:"label,omitempty"`
	Element    *Block   `json:"element,omitempty"`
	Hint       *TextObj `json:"hint,omitempty"`
	ExternalID string   `json:"external_id,omitempty"`
	FileID     string   `json:"file_id,omitempty"`
	Source     string   `json:"source,omitempty"`
	CallID     string   `json:"call_id,omitempty"`
}

type TextObj struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Emoji    bool   `json:"emoji,omitempty"`
	Verbatim bool   `json:"verbatim,omitempty"`
}

type textStyle struct {
	Bold, Italic, Strike, Code, Underline, Highlight, ClientHighlight, Unlink bool
}

func (b *Block) textString() string {
	if len(b.Text) == 0 {
		return ""
	}
	if b.Text[0] == '"' {
		var s string
		_ = json.Unmarshal(b.Text, &s)
		return s
	}
	var t TextObj
	_ = json.Unmarshal(b.Text, &t)
	return t.Text
}

func (b *Block) textObj() *TextObj {
	if len(b.Text) == 0 || b.Text[0] != '{' {
		return nil
	}
	var t TextObj
	if json.Unmarshal(b.Text, &t) != nil {
		return nil
	}
	return &t
}

func (b *Block) style() textStyle {
	var s textStyle
	if len(b.Style) > 0 && b.Style[0] == '{' {
		var m map[string]bool
		_ = json.Unmarshal(b.Style, &m)
		s = textStyle{Bold: m["bold"], Italic: m["italic"], Strike: m["strike"], Code: m["code"], Underline: m["underline"], Highlight: m["highlight"], ClientHighlight: m["client_highlight"], Unlink: m["unlink"]}
	}
	return s
}

func (b *Block) styleString() string {
	if len(b.Style) > 0 && b.Style[0] == '"' {
		var s string
		_ = json.Unmarshal(b.Style, &s)
		return s
	}
	return ""
}

// TextObject renders a Block Kit text object (plain_text or mrkdwn).
func TextObject(r Resolver, t *TextObj) string {
	if t == nil {
		return ""
	}
	if t.Type == "mrkdwn" {
		if t.Verbatim {
			return linkify(r, t.Text)
		}
		return mrkdwn(r, t.Text, false)
	}
	s := emojify(r, esc(t.Text), false)
	return strings.ReplaceAll(s, "\n", "<br>")
}

// Blocks renders a list of Block Kit blocks.
func Blocks(r Resolver, raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var bs []Block
	if json.Unmarshal(raw, &bs) != nil {
		return ""
	}
	return BlockList(r, bs)
}

func BlockList(r Resolver, bs []Block) string {
	big := richOnlyEmoji(r, bs)
	var out strings.Builder
	for i := range bs {
		out.WriteString(block(r, &bs[i], big))
	}
	return out.String()
}

func block(r Resolver, b *Block, big bool) string {
	switch b.Type {
	case "rich_text":
		var out strings.Builder
		out.WriteString(`<div class="p-rich_text_block" dir="auto">`)
		for i := range b.Elements {
			out.WriteString(richElement(r, &b.Elements[i], big))
		}
		out.WriteString(`</div>`)
		return out.String()
	case "section":
		var out strings.Builder
		out.WriteString(`<div class="p-block_kit_renderer__block_wrapper"><div class="p-section_block">`)
		out.WriteString(`<div class="p-section_block__content">`)
		if t := b.textObj(); t != nil {
			out.WriteString(`<div class="p-section_block_text_content"><div class="p-mrkdwn_element">` + TextObject(r, t) + `</div></div>`)
		}
		if len(b.Fields) > 0 {
			out.WriteString(`<div class="p-section_block__fields">`)
			for i := range b.Fields {
				out.WriteString(`<div class="p-section_block__field">` + TextObject(r, &b.Fields[i]) + `</div>`)
			}
			out.WriteString(`</div>`)
		}
		out.WriteString(`</div>`)
		if b.Accessory != nil {
			out.WriteString(`<div class="p-section_block__accessory">` + element(r, b.Accessory) + `</div>`)
		}
		out.WriteString(`</div></div>`)
		return out.String()
	case "context":
		var out strings.Builder
		out.WriteString(`<div class="p-block_kit_renderer__block_wrapper"><div class="p-context_block">`)
		for i := range b.Elements {
			e := &b.Elements[i]
			if e.Type == "image" {
				out.WriteString(`<img class="p-context_block__image" src="` + esc(imgURL(r, e)) + `" alt="` + esc(e.AltText) + `">`)
			} else {
				var t TextObj
				_ = json.Unmarshal(mustJSON(e), &t)
				out.WriteString(`<span class="p-context_block__text">` + TextObject(r, &t) + `</span>`)
			}
		}
		out.WriteString(`</div></div>`)
		return out.String()
	case "header":
		return `<div class="p-block_kit_renderer__block_wrapper"><div class="p-header_block">` + TextObject(r, b.textObj()) + `</div></div>`
	case "divider":
		return `<div class="p-block_kit_renderer__block_wrapper"><hr class="p-divider_block"></div>`
	case "image":
		title := ""
		if b.Title != nil {
			title = `<div class="p-image_block__title">` + TextObject(r, b.Title) + `</div>`
		}
		return `<div class="p-block_kit_renderer__block_wrapper"><div class="p-image_block">` + title + `<img class="p-image_block__image" src="` + esc(imgURL(r, b)) + `" alt="` + esc(b.AltText) + `" loading="lazy" data-zoom></div></div>`
	case "actions":
		var out strings.Builder
		out.WriteString(`<div class="p-block_kit_renderer__block_wrapper"><div class="p-actions_block">`)
		for i := range b.Elements {
			out.WriteString(`<div class="p-actions_block__action">` + element(r, &b.Elements[i]) + `</div>`)
		}
		out.WriteString(`</div></div>`)
		return out.String()
	case "video":
		t := ""
		if b.Title != nil {
			t = esc(b.Title.Text)
		}
		return `<div class="p-block_kit_renderer__block_wrapper"><div class="p-video_block"><a class="c-link p-video_block__title" href="` + esc(SafeURL(b.TitleURL)) + `" target="_blank" rel="noopener">` + t + `</a><img src="` + esc(SafeImg(b.ThumbnailURL)) + `" alt="` + esc(b.AltText) + `" loading="lazy"></div></div>`
	case "file":
		return ""
	case "call":
		return `<div class="p-block_kit_renderer__block_wrapper"><div class="p-call_block">📞 Call</div></div>`
	case "input":
		label := ""
		if b.Label != nil {
			label = TextObject(r, b.Label)
		}
		el := ""
		if b.Element != nil {
			el = element(r, b.Element)
		}
		return `<div class="p-block_kit_renderer__block_wrapper"><div class="p-input_block"><label>` + label + `</label>` + el + `</div></div>`
	}
	if t := b.textObj(); t != nil {
		return `<div class="p-block_kit_renderer__block_wrapper">` + TextObject(r, t) + `</div>`
	}
	return ""
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

var hexColorRe = regexp.MustCompile(`^#?[0-9a-fA-F]{3,8}$`)

// SafeImg returns u if it is an http(s) or same-origin image URL, else "".
func SafeImg(u string) string {
	if strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "/file?") {
		return u
	}
	return ""
}

func imgURL(r Resolver, b *Block) string {
	if b.ImageURL != "" {
		return SafeImg(r.Proxy(b.ImageURL))
	}
	if b.SlackFile != nil && b.SlackFile.URL != "" {
		return r.Proxy(b.SlackFile.URL)
	}
	return ""
}

func element(r Resolver, e *Block) string {
	switch e.Type {
	case "button":
		cls := "c-button c-button--outline c-button--small p-block_kit_button_element"
		switch e.styleString() {
		case "primary":
			cls = "c-button c-button--primary c-button--small p-block_kit_button_element"
		case "danger":
			cls = "c-button c-button--danger c-button--small p-block_kit_button_element"
		}
		label := TextObject(r, e.textObj())
		attrs := ` data-action-id="` + esc(e.ActionID) + `" data-value="` + esc(e.Value) + `"`
		if h := SafeURL(e.URL); h != "" {
			return `<a class="` + cls + `" href="` + esc(h) + `" target="_blank" rel="noopener"` + attrs + `>` + label + `</a>`
		}
		return `<button class="` + cls + `" type="button"` + attrs + `>` + label + `</button>`
	case "image":
		return `<img class="p-section_block__accessory_image" src="` + esc(imgURL(r, e)) + `" alt="` + esc(e.AltText) + `" loading="lazy">`
	case "overflow":
		return `<button class="c-button-unstyled p-block_kit_overflow" type="button">⋮</button>`
	case "static_select", "external_select", "users_select", "conversations_select", "channels_select", "multi_static_select", "multi_users_select":
		ph := "Select an item"
		if e.Placeholder != nil {
			ph = e.Placeholder.Text
		}
		return `<button class="c-select_button p-block_kit_select" type="button">` + esc(ph) + `<span class="c-select_button__arrow">▾</span></button>`
	case "datepicker":
		return `<button class="c-select_button p-block_kit_select" type="button">📅 Select a date</button>`
	case "plain_text_input":
		return `<input class="c-input_text p-block_kit_plain_text_input" placeholder="">`
	}
	return ""
}

func richOnlyEmoji(r Resolver, bs []Block) bool {
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

func richElement(r Resolver, e *Block, big bool) string {
	switch e.Type {
	case "rich_text_section":
		return richInline(r, e.Elements, big)
	case "rich_text_preformatted":
		var b strings.Builder
		b.WriteString(`<pre class="c-mrkdwn__pre" data-stringify-type="pre"><div class="p-rich_text_block--no-overflow">`)
		b.WriteString(richInline(r, e.Elements, false))
		b.WriteString(`</div></pre>`)
		return b.String()
	case "rich_text_quote":
		return `<blockquote type="cite" class="c-mrkdwn__quote" data-stringify-type="quote">` + richInline(r, e.Elements, false) + `</blockquote>`
	case "rich_text_list":
		tag := "ul"
		cls := "p-rich_text_list p-rich_text_list__bullet"
		if e.styleString() == "ordered" {
			tag = "ol"
			cls = "p-rich_text_list p-rich_text_list__ordered"
		}
		if e.Border > 0 {
			cls += " p-rich_text_list--nested-border"
		}
		var b strings.Builder
		b.WriteString(`<` + tag + ` class="` + cls + `" data-stringify-type="` + map[bool]string{true: "ordered-list", false: "unordered-list"}[tag == "ol"] + `" data-indent="` + strconv.Itoa(e.Indent) + `" data-border="` + strconv.Itoa(e.Border) + `"`)
		if e.Offset > 0 {
			b.WriteString(` start="` + strconv.Itoa(e.Offset+1) + `"`)
		}
		b.WriteString(` style="--indent:` + strconv.Itoa(e.Indent) + `">`)
		for i := range e.Elements {
			b.WriteString(`<li data-stringify-indent="` + strconv.Itoa(e.Indent) + `">` + richElement(r, &e.Elements[i], false) + `</li>`)
		}
		b.WriteString(`</` + tag + `>`)
		return b.String()
	}
	return richInline(r, []Block{*e}, big)
}

func richInline(r Resolver, els []Block, big bool) string {
	var out strings.Builder
	for i := range els {
		e := &els[i]
		var s string
		st := e.style()
		switch e.Type {
		case "text":
			s = strings.ReplaceAll(esc(e.textString()), "\n", "<br>")
		case "link":
			label := e.textString()
			if label == "" {
				label = e.URL
			}
			if st.Unlink {
				s = esc(label)
			} else {
				s = linkHTML(e.URL, label)
			}
		case "user":
			s = UserMention(r, e.UserID, "")
		case "usergroup":
			s = `<span class="c-mention_badge c-usergroup_entity" data-usergroup-id="` + esc(e.UsergroupID) + `">@` + esc(r.UsergroupName(e.UsergroupID)) + `</span>`
		case "channel":
			s = `<a class="c-channel_entity c-link" data-channel-id="` + esc(e.ChannelID) + `" href="#/c/` + esc(e.ChannelID) + `">#` + esc(r.ChannelName(e.ChannelID)) + `</a>`
		case "emoji":
			name := e.Name
			if e.SkinTone >= 2 {
				name += "::skin-tone-" + strconv.Itoa(e.SkinTone)
			}
			s = EmojiHTML(r, name, big)
		case "broadcast":
			s = `<span class="c-mention_badge">@` + esc(e.Range) + `</span>`
		case "date":
			var sec int64
			_ = json.Unmarshal(e.Timestamp, &sec)
			txt := e.Fallback
			if sec > 0 {
				txt = FormatDate(time.Unix(sec, 0), e.Format)
			}
			s = `<span class="c-timestamp-token" data-ts="` + strconv.FormatInt(sec, 10) + `" data-format="` + esc(e.Format) + `">` + esc(txt) + `</span>`
			if h := SafeURL(e.URL); h != "" {
				s = `<a class="c-link" href="` + esc(h) + `" target="_blank" rel="noopener">` + s + `</a>`
			}
		case "color":
			s = esc(e.Value)
			if hexColorRe.MatchString(e.Value) {
				s = `<span class="c-color_swatch" style="background:` + e.Value + `"></span>` + s
			}
		case "team":
			s = `<span class="c-team_entity">` + esc(e.Value) + `</span>`
		default:
			s = esc(e.textString())
		}
		if st.Code {
			s = `<code class="c-mrkdwn__code" data-stringify-type="code">` + s + `</code>`
		}
		if st.Strike {
			s = `<s data-stringify-type="strike">` + s + `</s>`
		}
		if st.Italic {
			s = `<i data-stringify-type="italic">` + s + `</i>`
		}
		if st.Bold {
			s = `<b data-stringify-type="bold">` + s + `</b>`
		}
		if st.Underline {
			s = `<u data-stringify-type="underline">` + s + `</u>`
		}
		if st.Highlight || st.ClientHighlight {
			s = `<span class="c-mrkdwn__highlight">` + s + `</span>`
		}
		out.WriteString(s)
	}
	return out.String()
}
