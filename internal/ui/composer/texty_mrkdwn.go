package composer

// A stored mrkdwn string (a channel's topic or description) in and out of
// a Texty input, the way Slack's useChannelTopicInput and
// useChannelPurposeInput do it (gantry-v2-async-client-boot-shared wD23
// and QBdp, build 13b0d600b14fab13):
//
//   - The input's initialText is message_to_contents_string (client-boot
//     L9Xf) of the stored text: TSF's EDIT tokens (segv getTokensArray,
//     j+eK), one string per token, then convertContentsStringToContents
//     (rKd0) turns that string into Texty contents. A channel is
//     <#C…|#name> while the client knows it and <#C…|#C…> while it doesn't
//     (a private channel the viewer isn't in, or one not fetched yet), a
//     member <@U…|@name> (getMemberMentionName: display name, else real
//     name) or <@U…|@U…>, a broadcast <@BKhere|@here>, a user group
//     <@S…|@handle> ("@unknown-group" while it's unknown), a date its
//     formatted text, any other <!…> nothing, a link its label (EDIT mode
//     keeps no URL) and the rest unescaped. Each <[@#%]id|label> becomes a
//     slackmention over its label and each known :name: an emoji.
//   - Save compares the input's value() with convertContentsToString (QgaI)
//     of that initialText: unchanged text sends nothing. Otherwise
//     submitChannelTopic / submitChannelPurpose (s0VR, uqxK) send
//     linkifyPhoneNumbers (cyvU) of clean_message_text_with_state (client-
//     boot DfFm): mentions back to <#C…|name>, <@U…>, <!here>,
//     <!subteam^S…|@handle>, typed emoticons to emoji, TSF's CLEAN escaping
//     of & < >, and phone numbers as <tel:…|…> links.
//
// The expected values in texty_mrkdwn_test.go come from those modules run
// in node over the CDN chunks (/root/clack-ops/scratch/topic-edit-mentions/
// harness/oracle.cjs).

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// TextyNames is what a Texty input names a stored text's mentions with
// (*store.Store): the member, the channel's name (its id while it's
// unknown) and the user group's handle (its id while it's unknown).
type TextyNames interface {
	User(id string) (*slack.User, bool)
	ChannelName(id string) string
	UsergroupName(id string) string
	UsergroupsLoaded() bool
}

var (
	nsMessageTexty   = i18n.NS("message")
	nsBroadcastTexty = i18n.NS("broadcast_keywords")
)

// jsTrim is lodash trim (String.prototype.trim's whitespace).
func jsTrim(s string) string { return strings.TrimFunc(s, jsSpace) }

// hxTrim is Haxe's StringTools.trim: only codes 9-13 and 32.
func hxTrim(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return r == ' ' || (r > 8 && r < 14) })
}

// lodashEscape is lodash escape.
var lodashEscape = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;").Replace

// lodashUnescape is lodash unescape.
var lodashUnescape = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'").Replace

// tsfLines is TSF's line ending pass: \r\n, \r, U+2028 and U+2029 become \n.
var tsfLines = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\u2028", "\n", "\u2029", "\n").Replace

// tsfStripZWSP is TSF's /^[U+200B]+|[U+200B]+$/gm.
func tsfStripZWSP(s string) string {
	ls := strings.Split(s, "\n")
	for i, l := range ls {
		ls[i] = strings.Trim(l, "\u200b")
	}
	return strings.Join(ls, "\n")
}

// tsfToken is TSF's /<(.*?)>/g ('.' stops at line ends).
var tsfToken = regexp.MustCompile(`<([^\n\r\x{2028}\x{2029}]*?)>`)

// tsfEdit is TSF.getTokensString(text, "EDIT"): links become their label
// (< and > escaped), <#…>, <@…>, <!…> and <%…> stay.
func tsfEdit(text string) string {
	s := hxTrim(text)
	if s == "" {
		return ""
	}
	s = tsfLines(s)
	s = tsfToken.ReplaceAllStringFunc(s, func(m string) string {
		z := m[1 : len(m)-1]
		if z != "" && strings.ContainsRune("#@!%", rune(z[0])) {
			return m
		}
		parts := strings.Split(z, "|")
		u := strings.ReplaceAll(parts[0], `"`, "&quot;")
		label := strings.Join(parts[1:], "|")
		if label == "" {
			label = u
		}
		label = hxTrim(label)
		if strings.HasPrefix(u, "<") {
			return "&lt;" + strings.NewReplacer("<", "&lt;", ">", "&gt;").Replace(z) + "&gt;"
		}
		return strings.NewReplacer("<", "&lt;", ">", "&gt;").Replace(label)
	})
	return tsfStripZWSP(s)
}

// broadcastKeywords are getMentionBroadcastKeywords (nPsK): kSm/'s keys and
// BKgroup.
var broadcastKeywords = []string{"BKeveryone", "BKchannel", "BKhere", "BKsf:accountteam", "BKsf:dealteam", "BKassistant:einstein", "BKgroup"}

// broadcastMsNames are kSm/'s ms_names (isBroadcastKeywordMsName, 6fpO).
var broadcastMsNames = []string{"everyone", "channel", "here", "sf:accountteam", "sf:dealteam", "assistant:einstein"}

// textyMention is rKd0's S: <[@|#%]id|[@|#%]label>.
var textyMention = regexp.MustCompile(`<[@|#%]((?:(?:U|W|S|C|G|Z|L|T)[A-Z0-9]{8,})|BKeveryone|BKchannel|BKhere|BKsf:accountteam|BKsf:dealteam|BKassistant:einstein|BKgroup|UNVERIFIED)\|([@|#%][^\n\r\x{2028}\x{2029}]*?)>`)

var (
	idChannel   = regexp.MustCompile(`^(C|G|D|Z)[A-Z0-9]{8,}$`) // looksLikeChannelId (nQiy)
	idUsergroup = regexp.MustCompile(`^S[A-Za-z0-9]{8,}$`)      // looksLikeUserGroupId (emAa)
	idSection   = regexp.MustCompile(`^L[A-Z0-9]{1,}$`)         // looksLikeChannelSectionId (8DoG)
	idTeam      = regexp.MustCompile(`^T[A-Z0-9]{8,}$`)         // looksLikeTeamId (uVRc)
)

func isKeyword(id string) bool {
	for _, k := range broadcastKeywords {
		if k == id {
			return true
		}
	}
	return false
}

// memberMentionName is getMemberMentionName (hWCs Gt): the profile's
// display name, else its real name (or the member's).
func memberMentionName(u *slack.User) string {
	if u.Profile.DisplayName != "" {
		return u.Profile.DisplayName
	}
	if u.Profile.RealName != "" {
		return u.Profile.RealName
	}
	return u.RealName
}

// editString is message_to_contents_string (L9Xf) in EDIT mode: the
// stored text as the string Texty's contents are made from.
func editString(r TextyNames, text string) string {
	var b strings.Builder
	for _, tok := range tsfEditTokens(tsfEdit(text)) {
		switch {
		case !strings.HasPrefix(tok, "<"):
			b.WriteString(lodashUnescape(tok))
		case strings.HasPrefix(tok, "<!"):
			b.WriteString(editCommand(r, tok))
		case strings.HasPrefix(tok, "<@"):
			id, _, _ := strings.Cut(tok[2:len(tok)-1], "|")
			if id == "" {
				continue
			}
			name := ""
			if u, ok := r.User(id); ok && u != nil {
				name = memberMentionName(u)
			}
			if name == "" {
				name = id
			}
			b.WriteString("<@" + id + "|@" + name + ">")
		case strings.HasPrefix(tok, "<#"):
			id, _, _ := strings.Cut(tok[2:len(tok)-1], "|")
			if id == "" {
				continue
			}
			// getChannelById: the store's channel, else an unknown object
			// (the fetch Slack starts); getDisplayNameForChannel without
			// the hash sign
			name := r.ChannelName(id)
			if name == "" || name == id {
				name = id
			}
			b.WriteString("<#" + id + "|#" + name + ">")
		default:
			b.WriteString(tok) // an unknown token: its text
		}
	}
	return b.String()
}

// tsfEditTokens is getTokensArray's split: text runs and <…> tokens.
func tsfEditTokens(s string) []string {
	var out []string
	at := 0
	for _, m := range tsfToken.FindAllStringIndex(s, -1) {
		if m[0] > at {
			out = append(out, s[at:m[0]])
		}
		out = append(out, s[m[0]:m[1]])
		at = m[1]
	}
	if at < len(s) {
		out = append(out, s[at:])
	}
	return out
}

// editCommand is L9Xf's "command" case for a <!…> token (KONw
// parseCommandToken).
func editCommand(r TextyNames, tok string) string {
	// tokenParts: the label is the second |-part only
	tp := strings.Split(tok[2:len(tok)-1], "|")
	head, label := tp[0], ""
	if len(tp) > 1 {
		label = tp[1]
	}
	parts := strings.Split(head, "^")
	typ, args := parts[0], parts[1:]
	switch {
	case typ == "group":
		return "<@BKgroup|@group>" // formatBroadcastCommand, EDIT
	case typ == "here" || typ == "channel" || typ == "everyone":
		// getBroadcastKeywordName (SFTn): the keyword in the viewer's language
		return "<@BK" + typ + "|@" + nsBroadcastTexty.T(typ) + ">"
	case typ == "subteam":
		id := ""
		if len(args) > 0 {
			id = args[0]
		}
		return editUsergroup(r, id, label)
	case typ == "date":
		if len(args) < 2 {
			return label
		}
		sec, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return label
		}
		return richtext.FormatDate(time.Unix(sec, 0), args[1])
	}
	return ""
}

// editUsergroup is formatUserGroupCommand (L9Xf) in EDIT mode.
func editUsergroup(r TextyNames, id, label string) string {
	// getUserGroupById: the store's group; a miss is non-existent once the
	// list was read, unknown (being fetched) before
	handle, known := "", false
	if h := r.UsergroupName(id); h != "" && h != id {
		handle, known = h, true
	}
	unknown := !known && !r.UsergroupsLoaded()
	nonExistent := !known && !unknown
	// formatUserGroupHandle (XXup)
	s := handle
	switch {
	case nonExistent:
		s = label
		if s == "" {
			s = nsMessageTexty.T("unknown-group")
		}
	case unknown:
		s = nsMessageTexty.T("unknown-group")
	case s == "":
		s = label
	}
	if s == "" {
		return "<@" + id + "|>"
	}
	return "<@" + id + "|@" + strings.TrimPrefix(s, "@") + ">"
}

// textyEmojiRx is pH4l's emoji pattern, global.
var textyEmojiRx = regexp.MustCompile(`:[a-zA-Z0-9àáâäãèéêëíîïóöôõœúùûüÿçßñÀÁÂÄÃÈÉÊËÍÎÏÓÖÔÕŒÚÙÛÜŸÇÑ\x{1100}-\x{11FF}\x{A960}-\x{A97F}\x{AC00}-\x{D7A3}\x{D7B0}-\x{D7FF}\x{3139}-\x{318F}\x{3041}-\x{3096}\x{30A0}-\x{30FF}\x{FF61}-\x{FF9F}\x{3400}-\x{4DB5}\x{4E00}-\x{9FCB}\x{F900}-\x{FA6A}\x{2E80}-\x{2FD5}\x{FF41}-\x{FF5A}\x{FF10}-\x{FF19}\-_+'\x{FF3F}\x{FF0B}\x{30FC}\x{3005}]+:(:skin-tone-[2-6](?:-[2-6])?:)?`)

// textyURLRx is WZII's i$ (buildURLRanges).
var textyURLRx = regexp.MustCompile(`(?i)((ftp|http|https)://|\bw{3}\.)[a-z0-9\-.]+[a-z]+(:[a-z0-9]*)?/?([@a-z0-9\-._?,'/\\+&amp;%:!$#=~])*`)

// inCodeBefore is isRangeInCode (hmcY) over the text before an emoji.
func inCodeBefore(s string) bool {
	if s == "" {
		return false
	}
	if strings.Count(s, "```")%2 == 1 { // isRangeInPreBlock (yzsb)
		return true
	}
	s = preFenceRx.ReplaceAllString(s, "")
	return strings.Count(s, "`")%2 == 1
}

// preFenceRx is hmcY's /`{3}([\s\S]*?)`{3}?/g (the trailing ? only
// makes {3} lazy).
var preFenceRx = regexp.MustCompile("```([\\s\\S]*?)```")

// textyMentionAtom is the composer's atom for a slackmention over label.
func textyMentionAtom(id, label string) *Atom {
	switch {
	case isKeyword(id):
		return &Atom{Kind: AtomBroadcast, ID: strings.TrimPrefix(id, "BK"), Text: label}
	case idUsergroup.MatchString(id):
		return &Atom{Kind: AtomUsergroup, ID: id, Text: label}
	case idChannel.MatchString(id):
		return &Atom{Kind: AtomChannel, ID: id, Text: label}
	}
	return &Atom{Kind: AtomUser, ID: id, Text: label}
}

// textyMentionID is the slackmention id an atom stands for.
func textyMentionID(a *Atom) string {
	if a.Kind == AtomBroadcast {
		return "BK" + a.ID
	}
	return a.ID
}

// contentsDoc is convertContentsStringToContents (rKd0) with
// useChannelTagging: s as a document of text, mention atoms and, unless
// emoji show as text, emoji atoms for the names look knows.
func contentsDoc(s string, look emojiLookup, emojiAsText bool) *Doc {
	type repl struct {
		at, n int // byte offset and length in s
		atom  *Atom
	}
	var rs []repl
	for _, m := range textyMention.FindAllStringSubmatchIndex(s, -1) {
		id, raw := s[m[2]:m[3]], s[m[4]:m[5]]
		label := lodashUnescape(raw)
		if (idSection.MatchString(id) || idTeam.MatchString(id)) && strings.HasPrefix(raw, "%") {
			label = label[1:]
		}
		rs = append(rs, repl{m[0], m[1] - m[0], textyMentionAtom(id, label)})
	}
	if !emojiAsText && look != nil {
		var urls [][]int
		for _, m := range textyURLRx.FindAllStringIndex(s, -1) {
			urls = append(urls, m)
		}
		inURL := func(at int) bool {
			for _, u := range urls {
				if at >= u[0] && at <= u[1] {
					return true
				}
			}
			return false
		}
		for _, m := range textyEmojiRx.FindAllStringIndex(s, -1) {
			if inURL(m[0]) || inCodeBefore(s[:m[0]]) {
				continue
			}
			name := strings.Trim(s[m[0]:m[1]], ":")
			at, ok := look(name)
			if !ok {
				continue
			}
			rs = append(rs, repl{m[0], m[1] - m[0], at})
		}
	}
	// sortBy(index): stable, mentions before emoji at the same index
	for i := 1; i < len(rs); i++ {
		for j := i; j > 0 && rs[j].at < rs[j-1].at; j-- {
			rs[j], rs[j-1] = rs[j-1], rs[j]
		}
	}
	d := NewDoc()
	at := 0
	for _, r := range rs {
		if r.at < at {
			// an emoji code inside a pill's label (a member named
			// "bob :smile: x"): Slack's forEach calls T.substring(k,
			// e.index) with index < k, which swaps its arguments and
			// writes the label's tail twice (shown "@bob :smile: x:smile:
			// x> x>", sent with that tail). Clack keeps the pill alone, a
			// deliberate departure from rKd0 for that one garbling case.
			continue
		}
		d.Insert(d.Len(), s[at:r.at], Attr{})
		d.InsertAtom(d.Len(), r.atom)
		at = r.at + r.n
	}
	d.Insert(d.Len(), s[at:], Attr{})
	return d
}

// MrkdwnTextyDoc is a stored topic or description as Slack's Texty input
// holds it (message_to_contents_string, then its contents).
func MrkdwnTextyDoc(r TextyNames, text string, look func(name string) (*Atom, bool), emojiAsText bool) *Doc {
	return contentsDoc(editString(r, text), look, emojiAsText)
}

// TextyValue is the input's value(): convertContentsToString (QgaI) of
// its contents. A mention is <#id|label>, <%id|%label> (a section or team)
// or <@id|label>, an emoji :name:, the rest as typed.
func TextyValue(d *Doc) string {
	var b strings.Builder
	for i, r := range d.Runes {
		a := d.Attrs[i].Atom
		if a == nil {
			b.WriteRune(r)
			continue
		}
		switch a.Kind {
		case AtomEmoji:
			b.WriteString(":" + a.ID + ":")
		case AtomUser, AtomChannel, AtomUsergroup, AtomBroadcast:
			id := textyMentionID(a)
			ctl, pct := "@", ""
			switch {
			case idChannel.MatchString(id):
				ctl = "#"
			case strings.HasPrefix(id, "L") || strings.HasPrefix(id, "T"):
				ctl, pct = "%", "%"
			}
			b.WriteString("<" + ctl + id + "|" + pct + lodashEscape(a.Text) + ">")
		case AtomMessage, AtomLink:
			b.WriteString(a.URL)
		default:
			b.WriteString(a.Text)
		}
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// ---------------------------------------------------------------------------
// clean_message_text_with_state (DfFm), as submitChannelTopic and
// submitChannelPurpose call it (not an internal message).

// tsfPlaceholderBase is TSF's placeholder_base before main() shuffles it.
const tsfPlaceholderBase = "\ue022-\ue022"

type tsfHold struct{ str, placeholder string }

// tsfPrePrefix and tsfPreFollow are pre_rx's prefix and lookahead classes.
func tsfPrePrefix(r rune) bool {
	return jsSpace(r) || strings.ContainsRune(`_*?.,-!^;:{([%$#+="`, r) || (r >= 0x2000 && r <= 0x206f) || (r >= 0x2e00 && r <= 0x2e7f)
}

func tsfPreFollow(r rune) bool {
	return jsSpace(r) || strings.ContainsRune(`_*?.,-!^;:})]%$#+=…"`, r) || (r >= 0x2000 && r <= 0x206f) || (r >= 0x2e00 && r <= 0x2e7f)
}

// tsfPreMatches finds pre_rx's matches: (^|prefix)```(…)?```(?=$|follow),
// the shortest content first. Each is [start, prefixEnd, end) in runes.
func tsfPreMatches(rs []rune) [][3]int {
	fence := func(i int) bool { return i+3 <= len(rs) && rs[i] == '`' && rs[i+1] == '`' && rs[i+2] == '`' }
	closeAt := func(from int) int {
		for j := from; j+3 <= len(rs); j++ {
			if fence(j) && (j+3 == len(rs) || tsfPreFollow(rs[j+3])) {
				return j + 3
			}
		}
		return -1
	}
	var out [][3]int
	for i := 0; i < len(rs); {
		if i == 0 && fence(0) {
			if e := closeAt(3); e > 0 {
				out = append(out, [3]int{0, 0, e})
				i = e
				continue
			}
		}
		if tsfPrePrefix(rs[i]) && fence(i+1) {
			if e := closeAt(i + 4); e > 0 {
				out = append(out, [3]int{i, i + 1, e})
				i = e
				continue
			}
		}
		i++
	}
	return out
}

// jsS and jsNS are JavaScript's \s and \S for Go patterns.
const jsS = `[\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`
const jsNS = `[^\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

// tsfCodeRx is code_rx.
var tsfCodeRx = regexp.MustCompile("(^|" + jsS + `|[~_*\\/?.,\-!^;:{(\[%$#+=\x{2000}-\x{206F}\x{2E00}-\x{2E7F}"])` + "`([^`\\n]*?" + jsNS + " *)?`")

// tsfReplaceFormatContents is TSF.replaceFormatContents with replace_pre
// and replace_code: code blocks, then inline code, become placeholders.
func tsfReplaceFormatContents(holds *[]tsfHold, s string) string {
	hold := func(str string) string {
		if str == "" {
			return ""
		}
		p := tsfPlaceholderBase + strconv.Itoa(len(*holds)) + "-"
		if strings.HasPrefix(str, "\n") {
			p = "\n" + p
		}
		*holds = append(*holds, tsfHold{str, p})
		return p
	}
	rs := []rune(s)
	var b strings.Builder
	at := 0
	for _, m := range tsfPreMatches(rs) {
		b.WriteString(string(rs[at:m[1]]))
		b.WriteString(hold(string(rs[m[1]:m[2]])))
		at = m[2]
	}
	b.WriteString(string(rs[at:]))
	s = b.String()
	b.Reset()
	at = 0
	for _, m := range tsfCodeRx.FindAllStringSubmatchIndex(s, -1) {
		b.WriteString(s[at:m[3]]) // up to the end of the prefix
		b.WriteString(hold(s[m[3]:m[1]]))
		at = m[1]
	}
	b.WriteString(s[at:])
	return b.String()
}

// tsfSwapOut is swapOutPlaceholders: last placeholder first, every
// occurrence.
func tsfSwapOut(holds []tsfHold, s string) string {
	for i := len(holds) - 1; i >= 0; i-- {
		s = strings.ReplaceAll(s, holds[i].placeholder, holds[i].str)
	}
	return s
}

// tsfClean is TSF.getTokensString(s, "CLEAN").
func tsfClean(s string) string {
	s = hxTrim(s)
	if s == "" {
		return ""
	}
	s = tsfStripZWSP(tsfLines(s))
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// trailingSpaces is /(^|\S)( +)(\n|$)/gm → "$1$3": spaces ending a line
// (after a non-space or alone on it) go.
func trailingSpaces(s string) string {
	rs := []rune(s)
	var out []rune
	lineStart := 0
	for i := 0; i <= len(rs); i++ {
		if i < len(rs) && rs[i] != '\n' && rs[i] != 0x2028 && rs[i] != 0x2029 {
			continue
		}
		line := rs[lineStart:i]
		j := len(line)
		for j > 0 && line[j-1] == ' ' {
			j--
		}
		if j < len(line) && (j == 0 || !jsSpace(line[j-1])) {
			line = line[:j]
		}
		out = append(out, line...)
		if i < len(rs) {
			out = append(out, rs[i])
		}
		lineStart = i + 1
	}
	return string(out)
}

// cleanMentions is replaceFormatMentions (DfFm) with escaping and channel
// tagging: each mention becomes a placeholder for what Slack sends.
func cleanMentions(s string, held *[][2]string) string {
	return textyMention.ReplaceAllStringFunc(s, func(m string) string {
		sm := textyMention.FindStringSubmatch(m)
		id, label := sm[1], sm[2]
		var out string
		switch {
		case isKeyword(id):
			out = "<!" + strings.TrimPrefix(id, "BK") + ">"
		case id == "UNVERIFIED":
			out = lodashEscape(label)
		case idUsergroup.MatchString(id):
			out = "<!subteam^" + id + "|" + label + ">"
		case idChannel.MatchString(id):
			_, size := utf8.DecodeRuneInString(label)
			out = "<#" + id + "|" + label[size:] + ">"
		default:
			out = "<@" + id + ">"
		}
		p := "mention-" + strconv.Itoa(len(*held)+1) + "-" + strconv.FormatInt(time.Now().UnixMilli(), 10)
		*held = append(*held, [2]string{p, out})
		return p
	})
}

// quoteRuns finds replaceQuotedText's quotes: (^|\n)>(?!…)([^\n]*)(\n>[^\n]*)*.
func quoteRuns(rs []rune) [][2]int {
	var out [][2]int
	for i := 0; i < len(rs); i++ {
		start := i
		gt := i
		if rs[i] == '\n' {
			gt = i + 1
		} else if i != 0 {
			continue
		}
		if gt >= len(rs) || rs[gt] != '>' {
			continue
		}
		// (?![\W_](?:<|>|[|/\\[\]{}()Dpb](?=\s|$)))
		if k := gt + 1; k < len(rs) && !(rs[k] < 0x80 && (unicode.IsLetter(rs[k]) || unicode.IsDigit(rs[k]))) && k+1 < len(rs) {
			c := rs[k+1]
			if c == '<' || c == '>' || (strings.ContainsRune(`|/\[]{}()Dpb`, c) && (k+2 == len(rs) || jsSpace(rs[k+2]))) {
				continue
			}
		}
		end := gt + 1
		for end < len(rs) && rs[end] != '\n' {
			end++
		}
		for end+1 < len(rs) && rs[end] == '\n' && rs[end+1] == '>' {
			end += 2
			for end < len(rs) && rs[end] != '\n' {
				end++
			}
		}
		out = append(out, [2]int{start, end})
		i = end - 1
	}
	return out
}

// convertEmoticonsText is DfFm's emoticon pass: UDJi's
// /(^|\s)(emoticon)(?=$|[\s|?.,!])/g with its callback (an emoticon closing
// a parenthesis opened since the last match stays, so does the "8)" of a
// list after "6)" or "7)"), then the parenthesized pass over what had a
// parenthesis.
func convertEmoticonsText(s string) string {
	rs := []rune(s)
	try := func(at int) string {
		for _, k := range emoticonKeys {
			kr := []rune(k)
			if at+len(kr) <= len(rs) && string(rs[at:at+len(kr)]) == k && emoticonFollow(rs, at+len(kr)) {
				return k
			}
		}
		return ""
	}
	var b strings.Builder
	last, copied := 0, 0
	var parens []string
	for i := 0; i < len(rs); {
		matched, pre := "", 0
		if i == 0 {
			matched = try(0)
		}
		if matched == "" && jsSpace(rs[i]) {
			if k := try(i + 1); k != "" {
				matched, pre = k, 1
			}
		}
		if matched == "" {
			i++
			continue
		}
		o, end := i, i+pre+len([]rune(matched))
		d := last
		last = end
		c, u := strings.Contains(matched, "("), strings.Contains(matched, ")")
		if (c || u) && !slices.Contains(parens, matched) {
			parens = append(parens, matched)
		}
		repl := string(rs[o:end])
		keep := false
		if u && !c {
			if t := string(rs[d:o]); strings.Contains(t, "(") && !strings.Contains(t, ")") {
				keep = true
			}
		}
		if !keep && repl == "\n8)" && listEightRx.MatchString(string(rs[:o])) {
			keep = true
		}
		if name, ok := emoticons[matched]; ok && !keep {
			repl = string(rs[o:o+pre]) + ":" + name + ":"
		}
		b.WriteString(string(rs[copied:o]))
		b.WriteString(repl)
		copied, i = end, end
	}
	b.WriteString(string(rs[copied:]))
	out := b.String()
	if len(parens) > 0 {
		alts := make([]string, len(parens))
		for i, p := range parens {
			alts[i] = regexp.QuoteMeta(p)
		}
		rx := regexp.MustCompile(`(\([^\n\r\x{2028}\x{2029}]+)(` + strings.Join(alts, "|") + `)([^\n\r\x{2028}\x{2029}]+\))`)
		out = rx.ReplaceAllStringFunc(out, func(m string) string {
			sm := rx.FindStringSubmatch(m)
			if name, ok := emoticons[sm[2]]; ok {
				return sm[1] + ":" + name + ":" + sm[3]
			}
			return m
		})
	}
	return out
}

// listEightRx is DfFm's /\n?(6\)|7\))/.
var listEightRx = regexp.MustCompile(`\n?(6\)|7\))`)

// mentionLabels is convertContentsToStringForDisplay (lgkT) of
// convertContentsStringToContents (rKd0) for text in code: each mention
// its label.
func mentionLabels(s string) string {
	return textyMention.ReplaceAllStringFunc(s, func(m string) string {
		sm := textyMention.FindStringSubmatch(m)
		id, raw := sm[1], sm[2]
		label := lodashUnescape(raw)
		if (idSection.MatchString(id) || idTeam.MatchString(id)) && strings.HasPrefix(raw, "%") {
			label = label[1:]
		}
		return label
	})
}

// CleanMessageText is clean_message_text_with_state (DfFm) for text
// typed by the viewer (not an internal message). emoticons is
// convert_emoticons on with emoji drawn as images.
func CleanMessageText(text string, emoticons bool) string {
	if text == "" {
		return ""
	}
	s := strings.NewReplacer("\u00a0", " ", "\r", "\n").Replace(text) // Xsp0
	var holds []tsfHold
	s = tsfReplaceFormatContents(&holds, s)
	var mentions [][2]string
	s = cleanMentions(s, &mentions)
	s = trailingSpaces(s)
	if emoticons {
		rs := []rune(s)
		var quoted [][2]string
		var b strings.Builder
		at := 0
		for n, q := range quoteRuns(rs) {
			p := "quoted-text-placeholder-" + strconv.FormatInt(time.Now().UnixMilli(), 10) + "-" + strconv.Itoa(n+1)
			b.WriteString(string(rs[at:q[0]]))
			b.WriteString(p)
			quoted = append(quoted, [2]string{p, string(rs[q[0]:q[1]])})
			at = q[1]
		}
		b.WriteString(string(rs[at:]))
		s = convertEmoticonsText(b.String())
		for _, q := range quoted {
			s = strings.Replace(s, q[0], q[1], 1)
		}
	}
	s = tsfClean(s)
	for _, m := range mentions {
		s = strings.Replace(s, m[0], m[1], 1)
	}
	for i := range holds {
		holds[i].str = tsfClean(mentionLabels(holds[i].str))
	}
	s = tsfSwapOut(holds, s)
	return jsTrim(s)
}

// ---------------------------------------------------------------------------
// linkifyPhoneNumbers (cyvU) over detectPhoneNumbers (L9Ir).

var (
	phoneRx         = regexp.MustCompile(`(?:(?:(?:(\+)?\d{1,3}[-\x20.]?)?\(?\d{3}\)?[-\x20.]?\d{3}[-\x20.]?\d{4})|(?:(\+)(?:9[976]\d|8[987530]\d|6[987]\d|5[90]\d|42\d|3[875]\d|2[98654321]\d|9[8543210]|8[6421]|6[6543210]|5[87654321]|4[987654310]|3[9643210]|2[70]|7|1)[-\x20.]?(?:\d[-\x20.]?){6,12}\d+))|(0([1-9]{1}-?[1-9]\d{3}|[1-9]{2}-?\d{3}|[1-9]{2}\d{1}-?\d{2}|[1-9]{2}\d{2}-?\d{1})-?\d{4}|0[789]0-?\d{4}-?\d{4}|050-?\d{4}-?\d{4})`)
	phoneDigitsOnly = regexp.MustCompile(`^\d+$`)
	phoneTenAfter   = regexp.MustCompile(`^\d{1,3}[-\x20.]\d{10}$`)
	phoneSkip       = regexp.MustCompile("```[\\s\\S]*?```|`[^`\\n]*?`|<[^<>]*?>")
)

// cjkRune is CwFm: a character in ZOnG's kw script ranges.
func cjkRune(r rune) bool {
	for _, g := range [][2]rune{{0x3040, 0x309f}, {0x30a0, 0x30ff}, {0x31f0, 0x31ff}, {0xff00, 0xffef}, {0xac00, 0xd7af}, {0x1100, 0x11ff},
		{0x3130, 0x318f}, {0xa960, 0xa97f}, {0xd7b0, 0xd7ff}, {0x3400, 0x4dbf}, {0x4e00, 0x9fff}, {0x2e80, 0x2eff}, {0x2f00, 0x2fdf},
		{0x2ff0, 0x2fff}, {0x3001, 0x303f}, {0x31c0, 0x31ef}, {0x3200, 0x32ff}, {0x3300, 0x33ff}, {0xf900, 0xfaff}, {0xfe30, 0xfe4f}} {
		if r >= g[0] && r <= g[1] {
			return true
		}
	}
	return false
}

type phoneMatch struct {
	at, n     int // UTF-16 offset and length, as JavaScript counts
	text, url string
}

// telURL is getTelUrlForPhoneNumber (PY28).
func telURL(s string) string {
	var b strings.Builder
	b.WriteString("tel:")
	if strings.HasPrefix(s, "+") {
		b.WriteByte('+')
	}
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// detectPhones is detectPhoneNumbers (L9Ir) without the tel: prefix form.
func detectPhones(s string) []phoneMatch {
	u := []rune(s)
	var out []phoneMatch
	for _, m := range phoneRx.FindAllStringIndex(s, -1) {
		t := s[m[0]:m[1]]
		if phoneDigitsOnly.MatchString(t) || phoneTenAfter.MatchString(t) {
			continue
		}
		at := utf8.RuneCountInString(s[:m[0]])
		txt := t
		if strings.HasPrefix(txt, "(") && !strings.Contains(txt, ")") {
			at++
			txt = txt[1:]
		}
		end := at + utf8.RuneCountInString(txt)
		before := at == 0 || strings.ContainsRune(" \u3000\n([{'\"", u[at-1]) || cjkRune(u[at-1])
		after := end == len(u) || strings.ContainsRune(" \u3000\n.)]}'\":?!;,", u[end]) || cjkRune(u[end])
		if before && after {
			out = append(out, phoneMatch{at: at, n: end - at, text: txt, url: telURL(t)})
		}
	}
	return out
}

// LinkifyPhoneNumbers is linkifyPhoneNumbers (cyvU): each phone number
// detectLinks (nVll, texty_links.go) keeps, outside code and <…>, becomes
// <tel:…|number>; one inside a URL or an e-mail address stays text.
func LinkifyPhoneNumbers(s string) string {
	u := []rune(s)
	var skip [][2]int
	for _, m := range phoneSkip.FindAllStringIndex(s, -1) {
		skip = append(skip, [2]int{utf8.RuneCountInString(s[:m[0]]), utf8.RuneCountInString(s[:m[1]])})
	}
	var phones []linkMatch
	for _, p := range detectLinks(s) {
		if p.kind != "phone" {
			continue
		}
		in := false
		for _, k := range skip {
			if p.at < k[1] && p.at+p.n > k[0] {
				in = true
			}
		}
		if !in {
			phones = append(phones, p)
		}
	}
	if len(phones) == 0 {
		return s
	}
	var b strings.Builder
	at := 0
	for _, p := range phones {
		b.WriteString(string(u[at:p.at]))
		b.WriteString("<" + p.url + "|" + p.text + ">")
		at = p.at + p.n
	}
	b.WriteString(string(u[at:]))
	return b.String()
}
