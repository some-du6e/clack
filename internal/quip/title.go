package quip

import (
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// The title metadata every save sends (extractTitleMetadata(true), in the
// webapp chunk's document model): the title section exported twice, once
// for its text (CITi's lE, the default target: every control writes its
// altRtml) and once for its rich text (target 5, forRichText: Slack's
// object controls stay <control> elements, which 30B3 reads through
// getControlForId), and the emoji info of its first ten loaded emoji. The
// quip-save-title snapshot holds these to Slack's modules run in node.

// TitleMeta is extractTitleMetadata(true)'s answer: the form's title, the
// title's emoji info and its rich text.
type TitleMeta struct {
	Title     string
	EmojiInfo []EmojiInfo
	RichText  *RichTextAPI
}

// MaxTitleLength is MAX_TITLE_LENGTH, in UTF-16 units.
const MaxTitleLength = 255

// titleRichTextMax is convertSectionsToRichTextApiPb's maxLength for the
// title.
const titleRichTextMax = 2000

// titleEmojiMax is how many emoji's info extractEmojiDataFromSection takes.
const titleEmojiMax = 10

// titleTags are the formatting tags a title's rich text carries as
// styles; any other markup is refused for now.
var titleTags = map[string]func(*RichTextStyle){
	"b":    func(s *RichTextStyle) { s.Bold = Ptr(true) },
	"i":    func(s *RichTextStyle) { s.Italic = Ptr(true) },
	"del":  func(s *RichTextStyle) { s.Strike = Ptr(true) },
	"code": func(s *RichTextStyle) { s.Code = Ptr(true) },
	"u":    func(s *RichTextStyle) { s.Underline = Ptr(true) },
}

// TitleControls is what TitleMetadata reads besides the title's RTML.
type TitleControls struct {
	// Section is a control's section by the id the RTML names (nil when
	// the document has none: the editor has no control for it, and the
	// export leaves it out). With no Section func, a title with a control
	// is refused.
	Section func(id string) *Section
	// EmojiInfo is the title section's emoji info (ContentTitle.EmojiInfo,
	// as Slack's last save left it, if the server keeps it: no client code
	// is seen writing it). Slack's editor takes an emoji's info from its
	// emoji store (uu.U), which Clack doesn't have: an emoji whose name is
	// there counts as the model loaded, with that info (emojiInfoOf's
	// answer then), and any other as the model still loading
	// (emojiControl).
	EmojiInfo []EmojiInfo
	// ChannelPrivate is a channel's is_private, as Slack's channel store
	// has it (known is false when Clack doesn't know the channel, or it's
	// a DM, which the title is then refused for).
	ChannelPrivate func(id string) (private, known bool)
}

// TitleControls is what TitleMetadata reads of d for its title: its
// controls' sections, the title section's emoji info and d's
// ChannelPrivate.
func (d *Doc) TitleControls() TitleControls {
	tc := TitleControls{Section: func(id string) *Section { return d.Section(d.RealID(id)) }, ChannelPrivate: d.ChannelPrivate}
	if t := d.titleSection(); t != nil && t.Content != nil && t.Content.Title != nil {
		tc.EmojiInfo = t.Content.Title.EmojiInfo
	}
	return tc
}

// TitleMetadata is what every save sends for the title section's RTML:
// its text (W77Y.extractText of the export) trimmed and cut to
// MaxTitleLength, "Untitled" when empty; the rich text 30B3 makes of the
// rich-text export, cut to titleRichTextMax (truncateRichTextApiPb); and
// the emoji info. Text, formatting, links, line breaks, mentions of users
// and channels and emoji are ported; a date, a broadcast, a usergroup, a
// loading emoji with no name, any other control, link or markup is
// refused. The mention labels are Slack's en-US strings ("user",
// "channel", "private channel"): a client in another locale sends its own.
func TitleMetadata(rtml string, tc TitleControls) (*TitleMeta, error) {
	if _, _, ok := ParseRTML(rtml); !ok {
		return nil, unsupported("the title's RTML doesn't parse")
	}
	root := rtmlRoot(rtml)
	if root == nil {
		return nil, unsupported("the title's RTML doesn't parse")
	}
	h := &titleHandler{tc: tc}
	walkRTML(root, h)
	h.flush()
	if h.err != nil {
		return nil, h.err
	}
	var all strings.Builder
	for _, p := range h.pieces {
		all.WriteString(p.text)
	}
	title := strings.TrimFunc(all.String(), jsSpace)
	if title == "" {
		return &TitleMeta{Title: "Untitled", RichText: plainRichText("Untitled")}, nil
	}
	cut, ok := cutUTF16(title, MaxTitleLength)
	if !ok {
		return nil, unsupported("the title's 255th character splits a surrogate pair")
	}
	// truncateRichTextApiPb: a text or link element's text counts its
	// UTF-16 units and is cut where the limit falls, any other element
	// counts one, and the elements past the limit are dropped
	sec := &RichTextSection{Type: Ptr("rich_text_section")}
	n := 0
	for _, p := range h.pieces {
		if p.elem == nil {
			continue
		}
		if n >= titleRichTextMax {
			break
		}
		b := *p.elem
		if t := str(b.Type); t == "text" || t == "link" {
			s := str(b.Text)
			if l := utf16Len(s); n+l > titleRichTextMax {
				c, ok := cutUTF16(s, titleRichTextMax-n)
				if !ok {
					return nil, unsupported("the title's rich text cut splits a surrogate pair")
				}
				b.Text, n = Ptr(c), titleRichTextMax
			} else {
				n += l
			}
		} else {
			n++
		}
		sec.Elements = append(sec.Elements, b)
	}
	return &TitleMeta{Title: cut, EmojiInfo: h.emoji, RichText: &RichTextAPI{Type: Ptr("rich_text"), Elements: []RichTextSection{*sec}}}, nil
}

func plainRichText(t string) *RichTextAPI {
	return &RichTextAPI{Type: Ptr("rich_text"), Elements: []RichTextSection{{Type: Ptr("rich_text_section"),
		Elements: []RichTextBlock{{Type: Ptr("text"), Text: Ptr(t)}}}}}
}

// titlePiece is one part of the title in order: its text in the title
// (the default export's) and its rich text element (nil for none).
type titlePiece struct {
	text string
	elem *RichTextBlock
}

// titleHandler is CITi's lE over W77Y.parse of the title, both exports at
// once, read back as extractText and 30B3 read them. The rich-text
// export's text nodes are the runs lE writes in a row: a formatting tag, a
// <br>, a link or a <control> it writes ends one, a control it leaves out
// or an annotation (lE drops those) doesn't.
type titleHandler struct {
	tc     TitleControls
	buf    strings.Builder
	open   []string
	pieces []titlePiece
	dirty  bool
	emoji  []EmojiInfo
	err    error
	// a link being read: its text, url and the style around it; plain
	// when its href is empty or "#" (lE writes no <a> for it)
	inLink, plainLink bool
	linkText          strings.Builder
	linkURL           string
	linkStyle         *RichTextStyle
	linkOpen          []string
}

func (h *titleHandler) fail(format string, a ...any) {
	if h.err == nil {
		h.err = unsupported(format, a...)
	}
}

func (h *titleHandler) style() *RichTextStyle {
	if len(h.open) == 0 {
		return nil
	}
	s := &RichTextStyle{}
	for _, t := range h.open {
		titleTags[t](s)
	}
	return s
}

// add appends a piece with the style around it on its element.
func (h *titleHandler) add(text string, elem *RichTextBlock) {
	if elem != nil {
		elem.Style = h.style()
	}
	h.pieces = append(h.pieces, titlePiece{text: text, elem: elem})
}

func (h *titleHandler) flush() {
	if !h.dirty {
		return
	}
	// the export's text reads back as html (entities decoded)
	t := html.UnescapeString(h.buf.String())
	h.add(t, &RichTextBlock{Type: Ptr("text"), Text: Ptr(t)})
	h.buf.Reset()
	h.dirty = false
}

func (h *titleHandler) text(s string) {
	s = strings.NewReplacer("\u00a0", " ", "\u200b", "", "\ufeff", "").Replace(s)
	s = EscapeText(s, false)
	if h.inLink && !h.plainLink {
		h.linkText.WriteString(s)
		return
	}
	if s == "" { // lE writes nothing: no text node of its own
		return
	}
	h.buf.WriteString(s)
	h.dirty = true
}

func (h *titleHandler) lineBreak() {
	if h.inLink {
		h.fail("the title has a line break in a link")
		return
	}
	// lE writes <br>, which ends the text node; 30B3 leaves it out
	// (omitBreakNewlines) and extractText reads only text
	h.flush()
}

func (h *titleHandler) annotationStart(string) {}

func (h *titleHandler) control(id string) {
	if h.inLink {
		h.fail("the title has a control in a link")
		return
	}
	if h.tc.Section == nil {
		h.fail("the title has a control")
		return
	}
	sec := h.tc.Section(id)
	if sec == nil {
		return // no control: lE writes nothing for it, in either export
	}
	c := sec.Content
	switch {
	case sec.Deleted != nil && *sec.Deleted || c == nil:
		h.fail("the title's control %s is deleted", id)
	case c.SlackUser != nil:
		// a user or a usergroup (su:S…): the rendered link's label is
		// "user" (not for an external paste); a display name stored is
		// the chip label, kept only under keepSlackObjectMentions
		// a usergroup's (su:S…) label depends on whether the mention
		// draws a link (a profile url): refused for now
		u := c.SlackUser
		uid, ok := parseSlackSyncerID(str(u.SlackUserID), "su:")
		if !ok || strings.HasPrefix(uid, "S") || str(u.DisplayName) != "" {
			h.fail("the title's mention %s isn't one Clack saves", id)
			return
		}
		h.flush()
		h.add("@user", &RichTextBlock{Type: Ptr("user"), UserID: Ptr(uid)})
	case c.SlackChannel != nil:
		ch := c.SlackChannel
		cid, ok := parseSlackSyncerID(str(ch.SlackChannelID), "sc:")
		if !ok || str(ch.Name) != "" {
			h.fail("the title's channel mention %s isn't one Clack saves", id)
			return
		}
		var private, known bool
		if h.tc.ChannelPrivate != nil {
			private, known = h.tc.ChannelPrivate(cid)
		}
		if !known {
			h.fail("the title mentions a channel Clack doesn't know")
			return
		}
		label := "#channel"
		if private {
			label = "#private channel"
		}
		h.flush()
		h.add(label, &RichTextBlock{Type: Ptr("channel"), ChannelID: Ptr(cid)})
	case c.SlackEmoji != nil:
		h.emojiControl(id, c.SlackEmoji)
	case c.SlackBroadcast != nil:
		// a broadcast isn't allowed in a title (allowInTitles false):
		// Slack's editor takes it out of the title when it draws it
		// (maybeConvertToHyperlinkOnRender, an empty link, the control
		// removed), so no save of Slack's carries one; refused, as Clack
		// doesn't make that edit
		h.fail("the title has a broadcast")
	case c.SlackDate != nil:
		h.fail("the title has a date")
	default:
		h.fail("the title has a control Clack doesn't save")
	}
}

// emojiControl is an emoji control as Slack's editor has it (the
// editor's emoji control content, um): its model is the emoji store's
// for its id (uu.U.get, made loading when the store has none, so never
// missing for a well-formed id). Clack has no emoji store: an emoji the
// title section's stored info names (the stored emoji_name, else the
// id's) is the model loaded, with that info; any other is the model
// still loading. name_ is the stored emoji_name, the model's display name
// once loaded; the title text is altRtml's ":name_:"; the info is
// emojiInfoOf(model, name_, fallbackUrl_), fallbackUrl_ being the model's
// display url or else the stored custom_emoji_url (a loading model's is
// {team_id "", display_name name_, display_url fallbackUrl_}); 30B3's
// element is built from that info. A loading emoji with no stored name
// (Slack's text then is empty: the canvas would lose the emoji from its
// name) and a malformed id are refused.
func (h *titleHandler) emojiControl(id string, e *SlackEmoji) {
	idName, ok := parseSlackEmojiID(str(e.SlackEmojiID))
	if !ok {
		h.fail("the title's emoji %s isn't one Clack saves", id)
		return
	}
	name := str(e.EmojiName)
	key := name
	if key == "" {
		key = idName
	}
	var info EmojiInfo
	loaded := false
	for _, x := range h.tc.EmojiInfo {
		if dn := str(x.DisplayName); dn != "" && dn == key {
			info, loaded = x, true
			break
		}
	}
	if loaded {
		name = str(info.DisplayName)
	} else {
		if name == "" {
			h.fail("the title's emoji %s has no name, and its info isn't loaded", id)
			return
		}
		info = EmojiInfo{TeamID: Ptr(""), DisplayName: Ptr(name)}
		if u := str(e.CustomEmojiURL); u != "" {
			info.DisplayURL = Ptr(u)
		}
	}
	el := slackEmojiElement(firstOf(str(info.DisplayName), idName), str(info.Unicode), str(info.DisplayURL))
	if el == nil {
		h.fail("the title's emoji %s isn't one Clack saves", id)
		return
	}
	h.flush()
	h.add(":"+name+":", el)
	if len(h.emoji) < titleEmojiMax {
		h.emoji = append(h.emoji, info)
	}
}

func firstOf(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// parseSlackSyncerID is 2U5W.parseSlackSyncerId: after prefix, the id
// (the second part of a two-part one), of letters, digits, - and _.
func parseSlackSyncerID(s, prefix string) (string, bool) {
	rest, ok := strings.CutPrefix(s, prefix)
	if !ok {
		return "", false
	}
	parts := strings.Split(rest, "/")
	n := ""
	switch len(parts) {
	case 1:
		n = parts[0]
	case 2:
		n = parts[1]
	}
	if n == "" {
		return "", false
	}
	for _, r := range n {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return "", false
		}
	}
	return n, true
}

// emojiIDName is e9rp's emoji name pattern (Slack's boot chunk), which
// 2U5W.parseSlackEmojiId holds an id's name to.
var emojiIDName = regexp.MustCompile(`^` + `[a-zA-Z0-9\x{e0}\x{e1}\x{e2}\x{e4}\x{e3}\x{e8}\x{e9}\x{ea}\x{eb}\x{ed}\x{ee}\x{ef}\x{f3}\x{f6}\x{f4}\x{f5}\x{0153}\x{fa}\x{f9}\x{fb}\x{fc}\x{ff}\x{e7}\x{df}\x{f1}\x{c0}\x{c1}\x{c2}\x{c4}\x{c3}\x{c8}\x{c9}\x{ca}\x{cb}\x{cd}\x{ce}\x{cf}\x{d3}\x{d6}\x{d4}\x{d5}\x{0152}\x{da}\x{d9}\x{db}\x{dc}\x{0178}\x{c7}\x{d1}\x{FF41}-\x{FF5A}\x{FF10}-\x{FF19}\-_+'\x{FF3F}\x{FF0B}\x{30FC}\x{3005}\x{3041}-\x{3096}\x{30A0}-\x{30FF}\x{FF61}-\x{FF9F}\x{3400}-\x{4DB5}\x{4E00}-\x{9FFF}\x{2E80}-\x{2FD5}\x{1100}-\x{11FF}\x{A960}-\x{A97F}\x{AC00}-\x{D7A3}\x{D7B0}-\x{D7FF}\x{3139}-\x{318F}\x{F900}-\x{FAFF}\x{0400}-\x{04FF}\x{0500}-\x{052F}]+(::skin-tone-[2-6](-[2-6])*)?` + `$`)

// parseSlackEmojiID is 2U5W.parseSlackEmojiId's id: se:<name>/<team>,
// the team of letters, digits, - and _.
func parseSlackEmojiID(id string) (string, bool) {
	rest, ok := strings.CutPrefix(id, "se:")
	if !ok {
		return "", false
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || !emojiIDName.MatchString(parts[0]) || parts[1] == "" {
		return "", false
	}
	for _, r := range parts[1] {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return "", false
		}
	}
	return parts[0], true
}

// skinToneRx is 2U5W.EMOJI_SKIN_TONE_RX.
var skinToneRx = regexp.MustCompile(`::[^:]+-([2-6]-[2-6])|::[^:]+-([2-6])$`)

// slackEmojiElement is 30B3's slackEmojiTextElement: the name parsed by
// XIeE.parseSlackEmoji (lowercased, colons off, a skin tone split off);
// one skin tone is skin_tone, several stay in the name unless the emoji
// has a unicode. nil where Slack's gives null.
func slackEmojiElement(name, unicode, url string) *RichTextBlock {
	n := strings.ToLower(name)
	n = strings.TrimPrefix(n, ":")
	n = strings.TrimSuffix(n, ":")
	m := skinToneRx.FindStringSubmatchIndex(n)
	base := n
	tone := ""
	if m != nil {
		base = n[:m[0]] + n[m[1]:]
		if m[2] >= 0 {
			tone = n[m[2]:m[3]]
		} else {
			tone = n[m[4]:m[5]]
		}
	}
	if base == "" || strings.Contains(base, ":") {
		return nil
	}
	for _, p := range strings.Split(tone, "-") {
		if tone == "" {
			break
		}
		if v, err := strconv.Atoi(p); err != nil || v < 2 || v > 6 {
			return nil
		}
	}
	b := &RichTextBlock{Type: Ptr("emoji"), Name: Ptr(base)}
	switch {
	case strings.Contains(tone, "-"):
		if unicode == "" {
			b.Name = Ptr(base + "::skin-tone-" + tone)
		}
	case tone != "":
		b.SkinTone = Ptr(tone)
	}
	if unicode != "" {
		b.Unicode = Ptr(unicode)
	}
	if url != "" {
		b.DisplayURL = Ptr(url)
	}
	return b
}

// slackUserLink and slackChannelLink are 5doc's pA and rZ: a link to a
// Slack user's or channel's page, which 30B3 makes a mention of.
var (
	slackUserLink    = regexp.MustCompile(`https://((.+?)(\.dev[0-9]*?|\.staging)?\.)?slack\.com/team/((U|W)[A-Z0-9]{8,})`)
	slackChannelLink = regexp.MustCompile(`https://((.+?)(\.dev[0-9]*?|\.staging)?\.)?slack\.com/archives/((C|G|D)[A-Z0-9]{8,})$`)
)

func (h *titleHandler) linkStart(href string, nolink bool, fileID string) {
	if h.inLink {
		h.fail("the title has a link in a link")
		return
	}
	h.inLink = true
	// lE (fromLocalDisplay, not to it): an empty or "#" href writes no
	// <a>, the text goes on as it is
	if href == "" || href == "#" {
		h.plainLink = true
		return
	}
	// an http(s) href passes isKeptLinkUri as it is, and 30B3's url is
	// the href as typed (urlAsTyped); a Slack user's or channel's page
	// is a mention, a {{ a workflow variable's
	if nolink || fileID != "" || !(strings.HasPrefix(href, "https://") || strings.HasPrefix(href, "http://")) ||
		strings.Contains(href, "{{") || slackUserLink.MatchString(href) || slackChannelLink.MatchString(href) ||
		strings.Contains(strings.ToLower(href), "slack.com") { // 30B3 matches the resolved href: any other spelling of a Slack page too
		h.fail("the title has a link Clack doesn't save")
		return
	}
	h.flush()
	h.linkURL, h.linkStyle = href, h.style()
}

func (h *titleHandler) endLink() {
	h.inLink = false
	if h.plainLink {
		h.plainLink = false
		return
	}
	// 30B3 takes the anchor's whole text (useTextTypeForLinks: a text
	// element with the url), with the styles around the anchor only
	t := html.UnescapeString(h.linkText.String())
	h.linkText.Reset()
	h.linkOpen = nil
	if t == "" {
		h.fail("the title has a link with no text")
		return
	}
	h.pieces = append(h.pieces, titlePiece{text: t, elem: &RichTextBlock{Type: Ptr("text"), Text: Ptr(t), URL: Ptr(h.linkURL), Style: h.linkStyle}})
}

func (h *titleHandler) formattingStart(name, attrs string, flag bool) {
	if titleTags[name] == nil || !flag {
		h.fail("the title has %s markup", name)
		return
	}
	if h.inLink && !h.plainLink {
		h.linkOpen = append(h.linkOpen, name)
		return
	}
	h.flush()
	h.open = append(h.open, name)
}

func (h *titleHandler) markEnd(name string) bool {
	switch name {
	case "annotation":
	case "a":
		if h.inLink {
			h.endLink()
		}
	default:
		if h.inLink && !h.plainLink {
			if i := len(h.linkOpen) - 1; i >= 0 && h.linkOpen[i] == name {
				h.linkOpen = h.linkOpen[:i]
			}
			return true
		}
		if titleTags[name] == nil {
			return true // refused at its start
		}
		h.flush()
		for i := len(h.open) - 1; i >= 0; i-- {
			if h.open[i] == name {
				h.open = append(h.open[:i], h.open[i+1:]...)
				break
			}
		}
	}
	return true
}
