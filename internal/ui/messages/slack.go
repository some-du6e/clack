package messages

// Slack-shaped props for Taut plugins (docs/PLUGINS-components.md §1): a
// core.Message as Slack's message object, and the way back for the fields
// plugins override (ShowRealUser's sender, ShowSendingBot's blocks). Every
// function here runs only on the patched path, inside a props func or a
// native that got non-nil props.

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// blockIDPrefix marks the blocks slackMessage emits, so the native can map
// a plugin's (filtered, reordered) msg.blocks back to the parsed Doc.
const blockIDPrefix = "clack-"

// slackMessage is m as the web client's message object (the msg prop of
// MessageWrapper, MessageBackground, MessageSender, Blocks, …).
func slackMessage(m *core.Message) slackc.Props {
	p := slackc.Props{"type": "message", "ts": m.TS, "channel": m.Channel, "text": m.Text}
	set := func(k, v string) {
		if v != "" {
			p[k] = v
		}
	}
	set("user", m.User)
	set("bot_id", m.BotID)
	set("subtype", m.Subtype)
	// username and icons as Slack sent them; a user-less bot with neither
	// gets its resolved avatar as bot_profile icons (as Slack's store bot
	// would supply them to ConnectedBaseAvatar).
	set("username", m.Username)
	if m.Username == "" && m.User == "" && m.BotProfile == nil {
		set("username", m.Name)
	}
	set("thread_ts", m.ThreadTS)
	set("client_msg_id", m.ClientMsgID)
	if ic := m.Icons.Map(); ic != nil {
		p["icons"] = slackc.Props(ic)
	}
	if bp := m.BotProfile; bp != nil {
		prof := slackc.Props{"id": bp.ID, "name": bp.Name, "deleted": bp.Deleted}
		for k, v := range map[string]string{"app_id": bp.AppID, "user_id": bp.UserID, "team_id": bp.TeamID} {
			if v != "" {
				prof[k] = v
			}
		}
		if ic := bp.Icons.Map(); ic != nil {
			prof["icons"] = slackc.Props(ic)
		} else if bp.Icon != "" {
			prof["icons"] = slackc.Props{"image_36": bp.Icon, "image_48": bp.Icon, "image_72": bp.Icon}
		}
		p["bot_profile"] = prof
	} else if m.BotID != "" && m.User == "" && m.Avatar != "" {
		av := slackc.Props{"image_36": m.Avatar, "image_48": m.Avatar, "image_72": m.Avatar}
		p["bot_profile"] = slackc.Props{"id": m.BotID, "name": m.Name, "icons": av}
	}
	if len(m.Metadata) > 2 {
		var md any
		if json.Unmarshal(m.Metadata, &md) == nil {
			p["metadata"] = md
		}
	}
	if m.ReplyCount > 0 {
		p["reply_count"] = m.ReplyCount
		p["reply_users"] = strList(m.ReplyUsers)
		p["reply_users_count"] = len(m.ReplyUsers)
		set("latest_reply", m.LatestReply)
	}
	if m.Edited {
		p["edited"] = slackc.Props{"ts": m.EditedTS, "user": m.User}
	}
	if m.Pinned {
		p["pinned_to"] = []any{m.Channel}
	}
	if len(m.Reactions) > 0 {
		p["reactions"] = slackReactions(m.Reactions)
	}
	if len(m.Huddle) > 2 {
		var room any
		if json.Unmarshal(m.Huddle, &room) == nil {
			p["room"] = room
		}
	}
	blocks, files, atts := slackDoc(m.Doc)
	if raw := rawBlocks(m.Blocks); raw != nil {
		// the blocks as Slack sent them, as the web client's message
		// carries them: a message_mention, a channel tab slug or any
		// element the Doc keeps only as text stays itself
		blocks = raw
	}
	if len(blocks) > 0 {
		p["blocks"] = blocks
	}
	if len(files) > 0 {
		p["files"] = files
	}
	if len(atts) > 0 {
		p["attachments"] = atts
	}
	return p
}

func strList(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// slackReactions is Slack's reactions array: [{name, count, users}].
func slackReactions(rs []core.Reaction) []any {
	out := make([]any, len(rs))
	for i := range rs {
		out[i] = slackReaction(&rs[i])
	}
	return out
}

func slackReaction(re *core.Reaction) slackc.Props {
	return slackc.Props{"name": re.Name, "baseName": strings.SplitN(re.Name, "::", 2)[0], "count": re.Count, "users": strList(re.Users)}
}

// reactionKey is Slack's ReactionBar key, "message-<ts>-<channel>".
func reactionKey(m *core.Message) string { return "message-" + m.TS + "-" + m.Channel }

// slackDoc splits a parsed Doc back into Slack's blocks, files and
// attachments. Block i carries block_id "clack-i".
func slackDoc(d richtext.Doc) (blocks, files, atts []any) {
	for i := range d.Blocks {
		b := &d.Blocks[i]
		switch b.Kind {
		case richtext.FileBlock:
			if b.File != nil {
				files = append(files, slackFile(b.File))
			}
			continue
		case richtext.AttachmentBlock:
			if b.Att != nil && b.Att.ServiceName != "Huddle" {
				a := slackAttachment(b)
				a["id"] = len(atts) + 1 // Slack numbers a message's attachments from 1
				atts = append(atts, a)
			}
			continue
		case richtext.Tombstone:
			continue
		}
		if sb := slackBlock(b); sb != nil {
			sb["block_id"] = blockIDPrefix + strconv.Itoa(i)
			blocks = append(blocks, sb)
		}
	}
	return
}

func slackBlock(b *richtext.Block) slackc.Props {
	rich := func(typ string, extra slackc.Props) slackc.Props {
		el := slackc.Props{"type": typ, "elements": slackElements(b.Spans)}
		for k, v := range extra {
			el[k] = v
		}
		return slackc.Props{"type": "rich_text", "elements": []any{el}}
	}
	switch b.Kind {
	case richtext.Paragraph:
		return rich("rich_text_section", nil)
	case richtext.Preformatted:
		return rich("rich_text_preformatted", slackc.Props{"border": boolInt(b.Border)})
	case richtext.Quote:
		return rich("rich_text_quote", nil)
	case richtext.List:
		items := make([]any, len(b.Items))
		for i, it := range b.Items {
			items[i] = slackc.Props{"type": "rich_text_section", "elements": slackElements(it)}
		}
		style := "bullet"
		if b.Ordered {
			style = "ordered"
		}
		return slackc.Props{"type": "rich_text", "elements": []any{slackc.Props{
			"type": "rich_text_list", "style": style, "indent": b.Indent, "offset": b.Offset, "border": boolInt(b.Border), "elements": items,
		}}}
	case richtext.Header:
		return slackc.Props{"type": "header", "text": plainText(richtext.SpansText(b.Spans))}
	case richtext.Divider:
		return slackc.Props{"type": "divider"}
	case richtext.Context:
		els := []any{}
		if b.Image != nil {
			els = append(els, slackImage(b.Image))
		}
		if len(b.Spans) > 0 {
			els = append(els, mrkdwnText(b.Spans))
		}
		return slackc.Props{"type": "context", "elements": els}
	case richtext.Section:
		s := slackc.Props{"type": "section"}
		if len(b.Spans) > 0 {
			s["text"] = mrkdwnText(b.Spans)
		}
		if len(b.Fields) > 0 {
			fs := make([]any, len(b.Fields))
			for i, f := range b.Fields {
				fs[i] = mrkdwnText(f)
			}
			s["fields"] = fs
		}
		switch {
		case b.Image != nil:
			s["accessory"] = slackImage(b.Image)
		case len(b.Buttons) > 0:
			s["accessory"] = slackButton(&b.Buttons[0])
		}
		return s
	case richtext.ImageBlock:
		if b.Image == nil {
			return nil
		}
		s := slackImage(b.Image)
		s["type"] = "image"
		if b.Image.Title != "" {
			s["title"] = plainText(b.Image.Title)
		}
		return s
	case richtext.Actions:
		els := make([]any, len(b.Buttons))
		for i := range b.Buttons {
			els[i] = slackButton(&b.Buttons[i])
		}
		return slackc.Props{"type": "actions", "elements": els}
	case richtext.Table:
		rows := make([]any, len(b.Rows))
		for i, r := range b.Rows {
			cells := make([]any, len(r))
			for j, c := range r {
				cells[j] = slackc.Props{"type": "rich_text", "elements": []any{slackc.Props{"type": "rich_text_section", "elements": slackElements(c)}}}
			}
			rows[i] = cells
		}
		return slackc.Props{"type": "table", "rows": rows}
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func plainText(s string) slackc.Props {
	return slackc.Props{"type": "plain_text", "text": s, "emoji": true}
}

func mrkdwnText(spans []richtext.Span) slackc.Props {
	return slackc.Props{"type": "mrkdwn", "text": spansMrkdwn(spans)}
}

func slackImage(im *richtext.Image) slackc.Props {
	return slackc.Props{"type": "image", "image_url": im.URL, "alt_text": im.Alt}
}

func slackButton(bt *richtext.Button) slackc.Props {
	kind := bt.Kind
	if kind == "" {
		kind = "button"
	}
	s := slackc.Props{"type": kind, "text": plainText(bt.Text)}
	for k, v := range map[string]string{"url": bt.URL, "style": bt.Style, "action_id": bt.ActionID, "value": bt.Value} {
		if v != "" {
			s[k] = v
		}
	}
	return s
}

// slackElements is rich_text_section elements for inline spans.
func slackElements(spans []richtext.Span) []any {
	out := make([]any, 0, len(spans))
	for i := range spans {
		sp := &spans[i]
		var el slackc.Props
		switch sp.Kind {
		case richtext.Break:
			// Slack keeps newlines inside text elements.
			if n := len(out); n > 0 {
				if prev, ok := out[n-1].(slackc.Props); ok && prev["type"] == "text" && prev["style"] == nil {
					prev["text"] = prev["text"].(string) + "\n"
					continue
				}
			}
			el = slackc.Props{"type": "text", "text": "\n"}
		case richtext.Link:
			el = slackc.Props{"type": "link", "url": sp.URL, "text": sp.Text}
		case richtext.User:
			el = slackc.Props{"type": "user", "user_id": sp.ID}
		case richtext.Channel:
			el = slackc.Props{"type": "channel", "channel_id": sp.ID}
		case richtext.Usergroup:
			el = slackc.Props{"type": "usergroup", "usergroup_id": sp.ID}
		case richtext.Broadcast:
			el = slackc.Props{"type": "broadcast", "range": sp.ID}
		case richtext.Emoji:
			name, skin, _ := strings.Cut(sp.ID, "::skin-tone-")
			el = slackc.Props{"type": "emoji", "name": name}
			if n, err := strconv.Atoi(skin); err == nil {
				el["skin_tone"] = n
			}
		case richtext.Date:
			ts, _ := strconv.ParseInt(sp.ID, 10, 64)
			el = slackc.Props{"type": "date", "timestamp": ts, "format": "{date_short}", "fallback": sp.Text}
		case richtext.ColorSwatch:
			el = slackc.Props{"type": "color", "value": sp.Text}
		default:
			el = slackc.Props{"type": "text", "text": sp.Text}
		}
		if s := sp.Style; s != 0 {
			st := slackc.Props{}
			for _, f := range [...]struct {
				bit  richtext.Style
				name string
			}{{richtext.Bold, "bold"}, {richtext.Italic, "italic"}, {richtext.Strike, "strike"}, {richtext.Code, "code"}, {richtext.Underline, "underline"}, {richtext.Highlight, "highlight"}} {
				if s&f.bit != 0 {
					st[f.name] = true
				}
			}
			el["style"] = st
		}
		out = append(out, el)
	}
	return out
}

// spansMrkdwn writes spans as Slack mrkdwn ("*Sent using* <@U1>").
func spansMrkdwn(spans []richtext.Span) string {
	var b strings.Builder
	esc := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	for i := range spans {
		sp := &spans[i]
		var s string
		switch sp.Kind {
		case richtext.Break:
			b.WriteByte('\n')
			continue
		case richtext.Link:
			s = "<" + sp.URL + "|" + esc.Replace(sp.Text) + ">"
		case richtext.User:
			s = "<@" + sp.ID + ">"
		case richtext.Channel:
			s = "<#" + sp.ID + ">"
		case richtext.Usergroup:
			s = "<!subteam^" + sp.ID + ">"
		case richtext.Broadcast:
			s = "<!" + sp.ID + ">"
		case richtext.Emoji:
			s = ":" + sp.ID + ":"
		case richtext.Date:
			s = "<!date^" + sp.ID + "^{date_short}|" + esc.Replace(sp.Text) + ">"
		default:
			s = esc.Replace(sp.Text)
		}
		for _, f := range [...]struct {
			bit  richtext.Style
			mark string
		}{{richtext.Code, "`"}, {richtext.Strike, "~"}, {richtext.Italic, "_"}, {richtext.Bold, "*"}} {
			if sp.Style&f.bit != 0 && strings.TrimSpace(s) != "" {
				s = f.mark + s + f.mark
			}
		}
		b.WriteString(s)
	}
	return b.String()
}

func slackFile(f *richtext.File) slackc.Props {
	p := slackc.Props{"id": f.ID, "name": f.Name, "title": f.Title, "mimetype": f.Mime, "filetype": f.Type,
		"pretty_type": f.PrettyType, "size": f.Size, "url_private": f.URL, "url_private_download": f.Download, "permalink": f.Permalink}
	if f.Thumb != nil {
		p["thumb_360"], p["thumb_360_w"], p["thumb_360_h"] = f.Thumb.URL, f.Thumb.W, f.Thumb.H
	}
	if f.DurationMS > 0 {
		p["duration_ms"] = f.DurationMS
	}
	return p
}

// slackAttachment is a legacy attachment / unfurl / shared message.
func slackAttachment(b *richtext.Block) slackc.Props {
	a := b.Att
	p := slackc.Props{"fallback": richtext.SpansText(a.Title)}
	set := func(k, v string) {
		if v != "" {
			p[k] = v
		}
	}
	set("color", strings.TrimPrefix(a.Color, "#"))
	set("service_name", a.ServiceName)
	set("service_icon", a.ServiceIcon)
	set("author_name", a.AuthorName)
	set("author_icon", a.AuthorIcon)
	set("author_link", a.AuthorLink)
	set("author_subname", a.AuthorSubname)
	set("title_link", a.TitleLink)
	set("from_url", a.FromURL)
	set("original_url", a.FromURL)
	set("channel_id", a.ShareChannelID)
	set("channel_name", a.ShareChannel)
	if len(a.Pretext) > 0 {
		p["pretext"] = spansMrkdwn(a.Pretext)
	}
	if len(a.Title) > 0 {
		p["title"] = richtext.SpansText(a.Title)
	}
	if len(a.Text) > 0 {
		p["text"] = richtext.PlainText(richtext.Doc{Blocks: a.Text})
	}
	if len(a.Footer) > 0 {
		p["footer"] = spansMrkdwn(a.Footer)
	}
	set("footer_icon", a.FooterIcon)
	if a.TS > 0 {
		p["ts"] = a.TS
	}
	if a.Image != nil {
		p["image_url"], p["image_width"], p["image_height"] = a.Image.URL, a.Image.W, a.Image.H
	}
	if a.Thumb != nil {
		p["thumb_url"], p["thumb_width"], p["thumb_height"] = a.Thumb.URL, a.Thumb.W, a.Thumb.H
	}
	if a.VideoURL != "" {
		p["video_html"] = `<iframe src="` + a.VideoURL + `"></iframe>`
	}
	if a.IsShare {
		p["is_share"], p["is_msg_unfurl"] = true, true
	}
	if len(b.Fields) > 0 {
		fs := make([]any, len(b.Fields))
		for i, f := range b.Fields {
			fs[i] = slackc.Props{"value": spansMrkdwn(f), "short": i < len(b.FieldShort) && b.FieldShort[i]}
		}
		p["fields"] = fs
	}
	return p
}

// ---- the way back: overrides a plugin passed to <Original> ----

// propsID identifies a decoded props object: host trees are immutable, so
// the same pointer means the same override (0 = none).
func propsID(p slackc.Props) uintptr {
	if p == nil {
		return 0
	}
	return reflect.ValueOf(p).Pointer()
}

// applySlackMessage returns src with the sender, blocks and attachments a
// plugin set on msg (ShowRealUser's relay-bot fix rewrites user, drops
// bot_id/username/icons and rewrites forwarded attachments' authors).
// Identity and state (ts, channel, reactions, thread) stay src's.
func applySlackMessage(app *core.App, src *core.Message, msg slackc.Props) core.Message {
	m := *src
	m.User = msg.StrOr("user", "")
	m.BotID = msg.StrOr("bot_id", "")
	m.Subtype = msg.StrOr("subtype", "")
	m.Username = msg.StrOr("username", "")
	m.Icons = core.SenderIconsOf(msg.Map("icons"))
	senderSame := m.User == src.User && m.BotID == src.BotID && m.Username == src.Username && core.SameIcons(m.Icons, src.Icons)
	switch {
	case m.User != "":
		// Slack's sender is the member's: username, icons and bot_profile
		// aren't read (getMsgSenderEntity, ConnectedBaseAvatar).
		m.Name, m.Avatar = "", ""
	case senderSame:
		// unchanged: keep core's resolved name and avatar (emoji, bot, default)
	default:
		m.Name = m.Username
		if m.Name == "" {
			m.Name = msg.Map("bot_profile").StrOr("name", "")
		}
		m.Avatar = ""
		for _, ic := range []slackc.Props{msg.Map("icons"), msg.Map("bot_profile").Map("icons")} {
			if u := iconURL(ic); u != "" {
				m.Avatar = u
				break
			}
		}
		if m.Avatar == "" && m.BotID == src.BotID {
			m.Avatar = src.Avatar
		}
	}
	if src.BotID != "" && m.BotID == "" || m.Subtype != "bot_message" && src.Subtype == "bot_message" {
		m.IsBot = false
	}
	if t, ok := msg.Str("text"); ok {
		m.Text = t
	}
	m.Doc.Blocks = docBlocks(app, src.Doc.Blocks, src.Blocks, msg)
	if list, ok := msg["blocks"].([]any); ok && len(src.Blocks) > 0 && !sameSlice(m.Doc.Blocks, src.Doc.Blocks) {
		// the parts inside get the plugin's blocks (Slack's msg.blocks)
		if b, err := json.Marshal(jsonable(list)); err == nil {
			m.Blocks = b
		}
	}
	return m
}

// sameSlice reports whether a and b are the same slice (docBlocks returns
// orig itself when nothing changed).
func sameSlice(a, b []richtext.Block) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}

// rawBlocks is a message's Block Kit blocks as Slack sent them (nil when
// there are none or they don't parse).
func rawBlocks(raw json.RawMessage) []any {
	if len(raw) < 2 || raw[0] != '[' {
		return nil
	}
	var out []any
	if json.Unmarshal(raw, &out) != nil || len(out) == 0 {
		return nil
	}
	return out
}

// sameBlocks reports whether a plugin's msg.blocks are the blocks
// slackMessage sent (raw), compared as JSON.
func sameBlocks(list []any, raw json.RawMessage) bool { return sameJSON(list, rawBlocks(raw)) }

// sameJSON reports whether a and b encode to the same JSON (keys sorted).
func sameJSON(a, b any) bool {
	x, err := json.Marshal(jsonable(a))
	if err != nil {
		return false
	}
	y, err := json.Marshal(jsonable(b))
	return err == nil && string(x) == string(y)
}

// avatarURL picks the avatar for ConnectedBaseAvatar props (botProfile
// icons, icons, or the userId's profile image). def is the native's own
// avatar for user (kept while the props name the same user).
func avatarURL(h Host, p slackc.Props, user, def string) string {
	id := p.StrOr("userId", user)
	if id == "" { // ConnectedBaseAvatar: icons or botProfile only without a userId
		if u := iconURL(p.Map("icons")); u != "" {
			return u
		}
		if u := iconURL(p.Map("botProfile").Map("icons")); u != "" {
			return u
		}
		return def
	}
	if id == user && def != "" {
		return def
	}
	if id == "" || h == nil || h.Core() == nil {
		return def
	}
	if u, ok := h.Core().User(id); ok {
		return u.Avatar
	}
	ensureUser(h, id)
	return ""
}

func iconURL(ic slackc.Props) string {
	for _, k := range [...]string{"image_48", "image_72", "image_36", "image_64", "image_32"} {
		if u := ic.StrOr(k, ""); u != "" {
			return u
		}
	}
	return ""
}

// docBlocks rebuilds a Doc's blocks from msg.blocks / msg.attachments.
// When the msg went out with the message's real blocks (raw, as Slack
// sent them), the same blocks back keep orig and any other list is
// parsed like the message itself: Slack renders msg.blocks. Otherwise
// blocks the native emitted ("clack-i") map back to orig[i] unchanged
// and blocks a plugin made up are parsed. Attachment author fields are
// taken from msg.attachments. A msg without blocks keeps orig's. Returns
// orig itself when nothing changed.
func docBlocks(app *core.App, orig []richtext.Block, raw json.RawMessage, msg slackc.Props) []richtext.Block {
	changed := false
	var head, tail []richtext.Block // tail: files, attachments, huddle card (not in msg.blocks)
	var want []int                  // orig indices slackDoc emitted, in order
	var body []richtext.Block       // orig's blocks from the message's blocks or text
	for i := range orig {
		switch orig[i].Kind {
		case richtext.FileBlock, richtext.AttachmentBlock:
			tail = append(tail, orig[i])
		case richtext.Tombstone:
			head = append(head, orig[i])
		default:
			body = append(body, orig[i])
			if slackBlock(&orig[i]) != nil {
				want = append(want, i)
			}
		}
	}
	if list, ok := msg["blocks"].([]any); ok && rawBlocks(raw) != nil {
		if sameBlocks(list, raw) {
			head = append(head, body...)
		} else {
			changed = true
			head = append(head, parseBlocks(app, list)...)
		}
	} else if list, ok := msg["blocks"].([]any); ok {
		changed = len(list) != len(want)
		for k, v := range list {
			b := slackc.Props(asMap(v))
			id := b.StrOr("block_id", "")
			i, err := strconv.Atoi(strings.TrimPrefix(id, blockIDPrefix))
			if strings.HasPrefix(id, blockIDPrefix) && err == nil && i >= 0 && i < len(orig) {
				// orig[i], unless the plugin changed what it says
				if sb := slackBlock(&orig[i]); sb != nil {
					sb["block_id"] = id
					if sameJSON(b, sb) {
						head = append(head, orig[i])
						changed = changed || k >= len(want) || want[k] != i
						continue
					}
				}
			}
			changed = true
			head = append(head, parseBlocks(app, []any{b})...)
		}
	} else if rawBlocks(raw) != nil {
		head = append(head, body...)
	} else {
		for _, i := range want {
			head = append(head, orig[i])
		}
	}
	// Attachment authors (ShowRealUser rewrites forwarded messages).
	atts := msg.List("attachments")
	ai := 0
	for i := range tail {
		b := &tail[i]
		if b.Kind != richtext.AttachmentBlock || b.Att == nil || b.Att.ServiceName == "Huddle" {
			continue
		}
		if ai < len(atts) {
			a := slackc.Props(asMap(atts[ai]))
			na := *b.Att
			na.AuthorName = a.StrOr("author_name", "")
			na.AuthorIcon = a.StrOr("author_icon", "")
			na.AuthorLink = a.StrOr("author_link", "")
			na.AuthorSubname = a.StrOr("author_subname", "")
			if na.AuthorName != b.Att.AuthorName || na.AuthorIcon != b.Att.AuthorIcon || na.AuthorLink != b.Att.AuthorLink || na.AuthorSubname != b.Att.AuthorSubname {
				b.Att = &na
				changed = true
			}
		}
		ai++
	}
	if !changed {
		return orig
	}
	return append(head, tail...)
}

func asMap(v any) map[string]any {
	switch m := v.(type) {
	case slackc.Props:
		return m
	case map[string]any:
		return m
	}
	return nil
}

// parseBlocks parses Slack Block Kit blocks (from a plugin) into Doc
// blocks with core's resolver; without core it keeps their text.
func parseBlocks(app *core.App, blocks []any) []richtext.Block {
	raw, err := json.Marshal(jsonable(blocks))
	if err != nil {
		return nil
	}
	if app != nil && app.S != nil {
		return richtext.FromMessage(app.S, &slack.Message{Blocks: raw}).Blocks
	}
	return nil
}

// parseMessage parses a whole Slack message object a plugin rendered
// (api.elements.Blocks, MrkdwnElement…).
func parseMessage(app *core.App, msg slackc.Props) richtext.Doc {
	raw, err := json.Marshal(jsonable(msg))
	if err != nil {
		return richtext.Doc{}
	}
	var m slack.Message
	if json.Unmarshal(raw, &m) != nil {
		return richtext.Doc{}
	}
	if app == nil || app.S == nil {
		if m.Text == "" {
			return richtext.Doc{}
		}
		return richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: []richtext.Span{{Text: m.Text}}}}}
	}
	return richtext.FromMessage(app.S, &m)
}

// jsonable drops what can't cross to JSON (elements, functions).
func jsonable(v any) any {
	switch v := v.(type) {
	case slackc.Props:
		return jsonable(map[string]any(v))
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, x := range v {
			if j := jsonable(x); j != nil {
				out[k] = j
			}
		}
		return out
	case []any:
		out := make([]any, 0, len(v))
		for _, x := range v {
			if j := jsonable(x); j != nil {
				out = append(out, j)
			}
		}
		return out
	case string, bool, float64, float32, int, int64, int32, uint64, nil:
		return v
	}
	return nil
}

// docTail reports whether blocks have file cards and attachment cards
// (MessageFiles / MessageKitAttachments render only then, like Slack's).
func docTail(blocks []richtext.Block) (files, atts bool) {
	for i := range blocks {
		switch b := &blocks[i]; b.Kind {
		case richtext.FileBlock:
			files = files || b.File != nil
		case richtext.AttachmentBlock:
			atts = atts || b.Att != nil && b.Att.ServiceName != "Huddle"
		}
	}
	return
}

// filterTail drops the file and attachment cards a plugin left out of
// MessageFiles' msg.files / MessageKitAttachments' msg.attachments
// (matched by file id / attachment id). A list it can't match (missing,
// or entries without ids) filters nothing. Returns blocks itself when
// nothing is dropped.
func filterTail(blocks []richtext.Block, files, atts slackc.Props) []richtext.Block {
	fk, ak := idSet(files, "files"), idSet(atts, "attachments")
	if fk == nil && ak == nil {
		return blocks
	}
	var out []richtext.Block
	ai := 0
	for i := range blocks {
		b := &blocks[i]
		drop := false
		switch {
		case b.Kind == richtext.FileBlock && b.File != nil:
			drop = fk != nil && !fk[b.File.ID]
		case b.Kind == richtext.AttachmentBlock && b.Att != nil && b.Att.ServiceName != "Huddle":
			ai++
			drop = ak != nil && !ak[strconv.Itoa(ai)]
		}
		if drop && out == nil {
			out = append(make([]richtext.Block, 0, len(blocks)), blocks[:i]...)
		}
		if !drop && out != nil {
			out = append(out, *b)
		}
	}
	if out == nil {
		return blocks
	}
	return out
}

// idSet is the ids in msg[key] (strings, or objects with an id); nil when
// there is no such list or an entry has no id.
func idSet(msg slackc.Props, key string) map[string]bool {
	if msg == nil {
		return nil
	}
	list, ok := msg[key].([]any)
	if !ok {
		return nil
	}
	set := make(map[string]bool, len(list))
	for _, v := range list {
		if m := asMap(v); m != nil {
			v = m["id"]
		}
		switch id := v.(type) {
		case string:
			set[id] = true
		case float64:
			set[strconv.FormatFloat(id, 'f', -1, 64)] = true
		case int:
			set[strconv.Itoa(id)] = true
		default:
			return nil
		}
	}
	return set
}

// docChannels lists the channels a message mentions (at most 3, in order).
func docChannels(blocks []richtext.Block) []string {
	var out []string
	add := func(spans []richtext.Span) {
		for i := range spans {
			if sp := &spans[i]; sp.Kind == richtext.Channel && sp.ID != "" && len(out) < 3 {
				dup := false
				for _, id := range out {
					dup = dup || id == sp.ID
				}
				if !dup {
					out = append(out, sp.ID)
				}
			}
		}
	}
	for i := range blocks {
		b := &blocks[i]
		add(b.Spans)
		for _, it := range b.Items {
			add(it)
		}
		for _, f := range b.Fields {
			add(f)
		}
	}
	return out
}
