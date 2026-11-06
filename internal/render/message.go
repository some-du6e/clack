package render

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// View is the frontend-ready form of a message.
type View struct {
	TS          string          `json:"ts"`
	Channel     string          `json:"channel,omitempty"`
	User        string          `json:"user,omitempty"`
	BotID       string          `json:"bot_id,omitempty"`
	Name        string          `json:"name,omitempty"`   // display name override (bots, username)
	Avatar      string          `json:"avatar,omitempty"` // avatar override (bots/icons)
	IsBot       bool            `json:"is_bot,omitempty"`
	Subtype     string          `json:"subtype,omitempty"`
	HTML        string          `json:"html"`
	Text        string          `json:"text"` // raw text (for editing/search/copy)
	Edited      bool            `json:"edited,omitempty"`
	ThreadTS    string          `json:"thread_ts,omitempty"`
	ReplyCount  int             `json:"reply_count,omitempty"`
	ReplyUsers  []string        `json:"reply_users,omitempty"`
	LatestReply string          `json:"latest_reply,omitempty"`
	Reactions   []ReactionView  `json:"reactions,omitempty"`
	Pinned      bool            `json:"pinned,omitempty"`
	Broadcast   bool            `json:"broadcast,omitempty"`
	System      bool            `json:"system,omitempty"` // join/leave/topic etc: rendered compactly
	Files       []FileView      `json:"files,omitempty"`
	Huddle      json.RawMessage `json:"huddle,omitempty"`
	Pending     bool            `json:"pending,omitempty"`
}

type ReactionView struct {
	Name  string   `json:"name"`
	Count int      `json:"count"`
	Users []string `json:"users"`
	URL   string   `json:"url"`
	Self  bool     `json:"self,omitempty"`
}

type FileView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Title     string `json:"title,omitempty"`
	Mime      string `json:"mime"`
	Filetype  string `json:"filetype,omitempty"`
	URL       string `json:"url"`
	Download  string `json:"download,omitempty"`
	Thumb     string `json:"thumb,omitempty"`
	W         int    `json:"w,omitempty"`
	H         int    `json:"h,omitempty"`
	Size      int64  `json:"size,omitempty"`
	Permalink string `json:"permalink,omitempty"`
}

var systemSubtypes = map[string]bool{
	"channel_join": true, "channel_leave": true, "channel_topic": true, "channel_purpose": true,
	"channel_name": true, "channel_archive": true, "channel_unarchive": true, "group_join": true,
	"group_leave": true, "group_topic": true, "group_purpose": true, "group_name": true,
	"pinned_item": true, "unpinned_item": true, "channel_canvas_updated": true, "channel_posting_permissions": true,
	"channel_convert_to_private": true, "channel_convert_to_public": true, "bookmark_added": true,
}

// Message renders one Slack message.
func Message(r Resolver, ch string, m *slack.Message) View {
	v := View{
		TS: m.TS, Channel: ch, User: m.User, BotID: m.BotID, Subtype: m.Subtype,
		Text: m.Text, Edited: m.Edited != nil, ThreadTS: m.ThreadTS, ReplyCount: m.ReplyCount,
		ReplyUsers: m.ReplyUsers, LatestReply: m.LatestReply, Pinned: len(m.PinnedTo) > 0,
		Broadcast: m.Subtype == "thread_broadcast" || m.ReplyBroadcast, Huddle: m.Room,
	}
	if m.Subtype == "bot_message" || (m.BotID != "" && m.User == "") || m.DisplayAsBot {
		v.IsBot = true
	}
	if m.BotProfile != nil && m.User == "" {
		v.IsBot = true
		v.Name = m.BotProfile.Name
		v.Avatar = firstNonEmpty(m.BotProfile.Icons.Image72, m.BotProfile.Icons.Image48, m.BotProfile.Icons.Image36)
	}
	if m.Username != "" && (m.User == "" || m.Subtype == "bot_message") {
		v.Name = m.Username
	}
	if m.Icons != nil {
		if m.Icons.Emoji != "" {
			if u, _, ok := r.Emoji(strings.Trim(m.Icons.Emoji, ":")); ok {
				v.Avatar = u
			}
		} else if a := firstNonEmpty(m.Icons.Image72, m.Icons.Image64, m.Icons.Image48, m.Icons.Image36); a != "" {
			v.Avatar = a
		}
	}
	v.System = systemSubtypes[m.Subtype]
	self := r.SelfID()
	for _, re := range m.Reactions {
		rv := ReactionView{Name: re.Name, Count: re.Count, Users: re.Users, URL: re.URL}
		if rv.URL == "" {
			rv.URL, _, _ = r.Emoji(re.Name)
		}
		for _, u := range re.Users {
			if u == self {
				rv.Self = true
			}
		}
		v.Reactions = append(v.Reactions, rv)
	}
	for _, f := range m.Files {
		v.Files = append(v.Files, FileView{
			ID: f.ID, Name: f.Name, Title: f.Title, Mime: f.Mimetype, Filetype: f.Filetype,
			URL: r.Proxy(f.URLPrivate), Download: r.Proxy(f.URLPrivateDownload),
			Thumb: r.Proxy(firstNonEmpty(f.Thumb720, f.Thumb480, f.Thumb360, f.ThumbVideo)),
			W:     f.OriginalW, H: f.OriginalH, Size: f.Size, Permalink: f.Permalink,
		})
	}
	v.HTML = Body(r, m)
	return v
}

// Body renders the message content (text/blocks, files, attachments).
func Body(r Resolver, m *slack.Message) string {
	var b strings.Builder
	if m.Subtype == "message_deleted" || m.Subtype == "tombstone" {
		return `<span class="c-message__body--tombstone">This message was deleted.</span>`
	}
	body := ""
	if hasRich(m.Blocks) {
		body = Blocks(r, m.Blocks)
	}
	if body == "" && m.Text != "" {
		body = `<div class="p-rich_text_block" dir="auto"><div class="p-rich_text_section">` + Mrkdwn(r, m.Text) + `</div></div>`
	}
	if body != "" {
		b.WriteString(`<div class="c-message_kit__blocks"><div class="c-message__message_blocks">`)
		b.WriteString(body)
		if m.Edited != nil {
			b.WriteString(`<span class="c-message__edited_label" data-ts="` + esc(m.Edited.TS) + `">(edited)</span>`)
		}
		b.WriteString(`</div></div>`)
	}
	if len(m.Files) > 0 {
		b.WriteString(Files(r, m.Files))
	}
	for i := range m.Attachments {
		b.WriteString(Attachment(r, &m.Attachments[i]))
	}
	return b.String()
}

func hasRich(raw json.RawMessage) bool {
	return len(raw) > 2 && string(raw) != "null"
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

// Files renders file attachments: images inline, everything else as a card.
func Files(r Resolver, files []slack.File) string {
	var b strings.Builder
	b.WriteString(`<div class="c-message_kit__file_list p-file_list">`)
	for i := range files {
		f := &files[i]
		if f.Mode == "tombstone" || f.Mode == "hidden_by_limit" {
			b.WriteString(`<div class="c-file__tombstone">This file was deleted.</div>`)
			continue
		}
		switch {
		case strings.HasPrefix(f.Mimetype, "image/") && f.Thumb360 != "":
			thumb := firstNonEmpty(f.Thumb720, f.Thumb480, f.Thumb360)
			w, h := f.Thumb720W, f.Thumb720H
			if w == 0 {
				w, h = f.Thumb360W, f.Thumb360H
			}
			w, h = fit(w, h, 360, 360)
			b.WriteString(fmt.Sprintf(`<div class="p-file_image_thumbnail__wrapper" data-file-id="%s"><div class="c-file_container__title"><span class="c-file__title">%s</span><button class="c-file__collapse" type="button" aria-label="Toggle file">▾</button></div><a class="p-file_image_thumbnail" href="%s" target="_blank" data-zoom="%s" style="width:%dpx;height:%dpx"><img src="%s" alt="%s" loading="lazy" width="%d" height="%d"></a></div>`,
				esc(f.ID), esc(f.Name), esc(r.Proxy(f.URLPrivate)), esc(r.Proxy(f.URLPrivate)), w, h, esc(r.Proxy(thumb)), esc(f.Name), w, h))
		case strings.HasPrefix(f.Mimetype, "video/"):
			poster := ""
			if f.ThumbVideo != "" {
				poster = ` poster="` + esc(r.Proxy(f.ThumbVideo)) + `"`
			}
			src := firstNonEmpty(f.MP4, f.URLPrivate)
			b.WriteString(`<div class="p-file_video" data-file-id="` + esc(f.ID) + `"><video controls preload="metadata"` + poster + ` src="` + esc(r.Proxy(src)) + `"></video></div>`)
		case strings.HasPrefix(f.Mimetype, "audio/") || f.Subtype == "slack_audio":
			b.WriteString(`<div class="p-file_audio" data-file-id="` + esc(f.ID) + `"><audio controls preload="metadata" src="` + esc(r.Proxy(firstNonEmpty(f.MP4, f.URLPrivate))) + `"></audio></div>`)
		default:
			b.WriteString(fileCard(r, f))
		}
	}
	b.WriteString(`</div>`)
	return b.String()
}

func fileCard(r Resolver, f *slack.File) string {
	kind := firstNonEmpty(f.PrettyType, strings.ToUpper(f.Filetype), "File")
	meta := kind
	if f.Size > 0 {
		meta = kind + " · " + humanSize(f.Size)
	}
	preview := ""
	if f.Preview != "" && (f.Mode == "snippet" || f.Filetype == "text" || f.Lines > 0) {
		preview = `<pre class="c-file__snippet_preview">` + esc(f.Preview) + `</pre>`
	}
	return `<div class="c-file_entity c-message_attachment__file" data-file-id="` + esc(f.ID) + `"><a class="c-file_entity__item" href="` + esc(SafeURL(r.Proxy(f.URLPrivate))) + `" target="_blank" rel="noopener"><span class="c-file_entity__icon" data-type="` + esc(f.Filetype) + `">` + fileIcon(f.Filetype) + `</span><span class="c-file_entity__content"><span class="c-file_entity__title">` + esc(firstNonEmpty(f.Title, f.Name)) + `</span><span class="c-file_entity__meta">` + esc(meta) + `</span></span></a>` + preview + `</div>`
}

func fileIcon(ft string) string {
	switch ft {
	case "pdf":
		return "PDF"
	case "zip", "gzip", "tar":
		return "ZIP"
	case "text", "markdown", "javascript", "go", "python", "json", "html", "css":
		return "&lt;/&gt;"
	}
	return "FILE"
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return strconv.FormatFloat(float64(n)/(1<<30), 'f', 1, 64) + " GB"
	case n >= 1<<20:
		return strconv.FormatFloat(float64(n)/(1<<20), 'f', 1, 64) + " MB"
	case n >= 1<<10:
		return strconv.FormatFloat(float64(n)/(1<<10), 'f', 0, 64) + " KB"
	}
	return strconv.FormatInt(n, 10) + " B"
}

func fit(w, h, mw, mh int) (int, int) {
	if w <= 0 || h <= 0 {
		return mw, mh
	}
	if w > mw {
		h = h * mw / w
		w = mw
	}
	if h > mh {
		w = w * mh / h
		h = mh
	}
	return w, h
}

// Attachment renders a legacy attachment / link unfurl.
func Attachment(r Resolver, a *slack.Attachment) string {
	if a.IsMsgUnfurl || a.IsShare {
		return sharedMessage(r, a)
	}
	var b strings.Builder
	color := strings.TrimPrefix(a.Color, "#")
	style := ""
	if color != "" {
		if _, err := strconv.ParseUint(color, 16, 32); err == nil {
			style = ` style="--attachment-color:#` + esc(color) + `"`
		}
	}
	isUnfurl := a.ServiceName != "" || a.FromURL != "" || a.OriginalURL != ""
	cls := "c-message_attachment"
	if isUnfurl && color == "" {
		cls += " c-message_attachment--unfurl"
	}
	if color != "" {
		cls += " c-message_attachment--colored"
	}
	b.WriteString(`<div class="` + cls + `"` + style + `>`)
	if a.Pretext != "" {
		b.WriteString(`<div class="c-message_attachment__pretext">` + Mrkdwn(r, a.Pretext) + `</div>`)
	}
	b.WriteString(`<div class="c-message_attachment__border"></div><div class="c-message_attachment__body"><div class="c-message_attachment__row c-message_attachment__main">`)
	b.WriteString(`<div class="c-message_attachment__content">`)
	if a.ServiceName != "" {
		icon := ""
		if a.ServiceIcon != "" {
			icon = `<img class="c-message_attachment__author_icon" src="` + esc(SafeImg(a.ServiceIcon)) + `" alt="" loading="lazy">`
		}
		b.WriteString(`<div class="c-message_attachment__row c-message_attachment__service">` + icon + `<span class="c-message_attachment__service_name">` + esc(a.ServiceName) + `</span></div>`)
	}
	if a.AuthorName != "" {
		icon := ""
		if a.AuthorIcon != "" {
			icon = `<img class="c-message_attachment__author_icon" src="` + esc(SafeImg(a.AuthorIcon)) + `" alt="" loading="lazy">`
		}
		name := esc(a.AuthorName)
		if h := SafeURL(a.AuthorLink); h != "" {
			name = `<a class="c-link" href="` + esc(h) + `" target="_blank" rel="noopener">` + name + `</a>`
		}
		b.WriteString(`<div class="c-message_attachment__row c-message_attachment__author">` + icon + `<span class="c-message_attachment__author_name">` + name + `</span>` + subname(a) + `</div>`)
	}
	if a.Title != "" {
		t := emojify(r, esc(slackUnescape(a.Title)), false)
		if h := SafeURL(a.TitleLink); h != "" {
			t = `<a class="c-link c-message_attachment__title_link" href="` + esc(h) + `" target="_blank" rel="noopener">` + t + `</a>`
		}
		b.WriteString(`<div class="c-message_attachment__row c-message_attachment__title">` + t + `</div>`)
	}
	if a.Text != "" {
		b.WriteString(`<div class="c-message_attachment__row c-message_attachment__text"><span dir="auto">` + Mrkdwn(r, a.Text) + `</span></div>`)
	}
	if hasRich(a.Blocks) {
		b.WriteString(`<div class="c-message_attachment__row">` + Blocks(r, a.Blocks) + `</div>`)
	}
	if len(a.Fields) > 0 {
		b.WriteString(`<div class="c-message_attachment__fields">`)
		for _, f := range a.Fields {
			cls := "c-message_attachment__field"
			if f.Short {
				cls += " c-message_attachment__field--short"
			}
			b.WriteString(`<div class="` + cls + `"><div class="c-message_attachment__field_title">` + esc(f.Title) + `</div><div class="c-message_attachment__field_value">` + Mrkdwn(r, f.Value) + `</div></div>`)
		}
		b.WriteString(`</div>`)
	}
	if img := SafeImg(r.Proxy(a.ImageURL)); img != "" {
		w, h := fit(a.ImageWidth, a.ImageHeight, 360, 270)
		b.WriteString(fmt.Sprintf(`<div class="c-message_attachment__row c-message_attachment__image"><a href="%s" target="_blank" rel="noopener" data-zoom="%s"><img src="%s" alt="" loading="lazy" style="max-width:%dpx;max-height:%dpx"></a></div>`, esc(firstNonEmpty(SafeURL(a.FromURL), img)), esc(img), esc(img), w, h))
	}
	if a.Footer != "" || len(a.TS) > 0 {
		icon := ""
		if a.FooterIcon != "" {
			icon = `<img class="c-message_attachment__footer_icon" src="` + esc(SafeImg(a.FooterIcon)) + `" alt="" loading="lazy">`
		}
		ts := ""
		if sec := tsSeconds(a.TS); sec > 0 {
			ts = `<span class="c-timestamp-token" data-ts="` + strconv.FormatInt(sec, 10) + `" data-format="{date_short_pretty}"></span>`
		}
		b.WriteString(`<div class="c-message_attachment__row c-message_attachment__footer">` + icon + `<span class="c-message_attachment__footer_text">` + Mrkdwn(r, a.Footer) + `</span>` + ts + `</div>`)
	}
	b.WriteString(`</div>`)
	if a.ThumbURL != "" && a.ImageURL == "" {
		b.WriteString(`<div class="c-message_attachment__thumb"><img src="` + esc(SafeImg(r.Proxy(a.ThumbURL))) + `" alt="" loading="lazy"></div>`)
	}
	b.WriteString(`</div>`)
	if len(a.Files) > 0 {
		b.WriteString(Files(r, a.Files))
	}
	b.WriteString(`</div></div>`)
	return b.String()
}

func subname(a *slack.Attachment) string {
	if a.AuthorSubname == "" {
		return ""
	}
	return `<span class="c-message_attachment__author_subname">` + esc(a.AuthorSubname) + `</span>`
}

func tsSeconds(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	s := strings.Trim(string(raw), `"`)
	if i := strings.IndexByte(s, '.'); i >= 0 {
		s = s[:i]
	}
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

func sharedMessage(r Resolver, a *slack.Attachment) string {
	var b strings.Builder
	b.WriteString(`<div class="c-message_attachment c-message_attachment--share"><div class="c-message_attachment__border"></div><div class="c-message_attachment__body">`)
	icon := ""
	if a.AuthorIcon != "" {
		icon = `<img class="c-message_attachment__author_icon c-message_attachment__author_icon--share" src="` + esc(SafeImg(a.AuthorIcon)) + `" alt="" loading="lazy">`
	}
	b.WriteString(`<div class="c-message_attachment__row c-message_attachment__author">` + icon + `<span class="c-message_attachment__author_name">` + esc(firstNonEmpty(a.AuthorName, a.AuthorSubname)) + `</span></div>`)
	body := ""
	for _, mb := range a.MessageBlocks {
		body += Blocks(r, mb.Message.Blocks)
	}
	if body == "" {
		body = Mrkdwn(r, a.Text)
	}
	b.WriteString(`<div class="c-message_attachment__row c-message_attachment__text">` + body + `</div>`)
	if len(a.Files) > 0 {
		b.WriteString(Files(r, a.Files))
	}
	foot := ""
	if a.ChannelName != "" || a.ChannelID != "" {
		foot = "Posted in #" + esc(firstNonEmpty(a.ChannelName, r.ChannelName(a.ChannelID)))
	} else if a.Footer != "" {
		foot = Mrkdwn(r, a.Footer)
	}
	ts := ""
	if sec := tsSeconds(a.TS); sec > 0 {
		ts = ` | <span class="c-timestamp-token" data-ts="` + strconv.FormatInt(sec, 10) + `" data-format="{date_short_pretty}"></span>`
	}
	link := ""
	if h := SafeURL(a.FromURL); h != "" {
		link = ` | <a class="c-link" href="` + esc(h) + `" target="_blank" rel="noopener">View message</a>`
	}
	b.WriteString(`<div class="c-message_attachment__row c-message_attachment__footer"><span class="c-message_attachment__footer_text">` + foot + ts + link + `</span></div>`)
	b.WriteString(`</div></div>`)
	return b.String()
}

// ProxyURL builds a same-origin proxy URL for an authenticated Slack file.
func ProxyURL(u string) string {
	if u == "" {
		return ""
	}
	if strings.HasPrefix(u, "https://files.slack.com/") || strings.Contains(u, ".slack.com/files") {
		return "/file?u=" + url.QueryEscape(u)
	}
	return u
}
