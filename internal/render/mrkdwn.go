// Package render turns Slack messages (mrkdwn, Block Kit, attachments, files)
// into HTML that mirrors the markup and classes of Slack's web client.
package render

import (
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Resolver supplies names and URLs needed while rendering.
type Resolver interface {
	UserName(id string) string
	ChannelName(id string) string
	UsergroupName(id string) string
	Emoji(name string) (url, text string, ok bool)
	Proxy(u string) string
	SelfID() string
}

var esc = html.EscapeString

// slackUnescape undoes Slack's &amp; &lt; &gt; text escaping.
func slackUnescape(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	return strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&").Replace(s)
}

// EmojiHTML renders one emoji image the way Slack does.
func EmojiHTML(r Resolver, name string, big bool) string {
	u, text, ok := r.Emoji(name)
	if !ok {
		return ":" + esc(name) + ":"
	}
	cls := "c-emoji c-emoji__medium"
	if big {
		cls = "c-emoji c-emoji__large c-emoji--inline"
	}
	alt := ":" + name + ":"
	if text != "" {
		alt = text
	}
	return `<img class="` + cls + `" src="` + esc(u) + `" alt="` + esc(alt) + `" data-stringify-emoji=":` + esc(name) + `:" title="` + esc(":"+name+":") + `" loading="lazy" draggable="false">`
}

var emojiRe = regexp.MustCompile(`:([a-z0-9_+'\-]+(?:::skin-tone-[2-6])?):`)

// emojify replaces :name: tokens in already-escaped HTML text.
func emojify(r Resolver, s string, big bool) string {
	if !strings.Contains(s, ":") {
		return s
	}
	return emojiRe.ReplaceAllStringFunc(s, func(m string) string {
		name := m[1 : len(m)-1]
		if _, _, ok := r.Emoji(name); !ok {
			return m
		}
		return EmojiHTML(r, name, big)
	})
}

// OnlyEmoji reports whether text consists solely of 1..23 emoji (Slack "jumbomoji").
func OnlyEmoji(r Resolver, text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	n := 0
	rest := emojiRe.ReplaceAllStringFunc(t, func(m string) string {
		if _, _, ok := r.Emoji(m[1 : len(m)-1]); ok {
			n++
			return ""
		}
		return m
	})
	return n > 0 && n <= 23 && strings.TrimSpace(rest) == ""
}

// Mrkdwn renders Slack mrkdwn text to HTML.
func Mrkdwn(r Resolver, text string) string {
	return mrkdwn(r, text, OnlyEmoji(r, text))
}

func mrkdwn(r Resolver, text string, big bool) string {
	var out strings.Builder
	// Split out ``` blocks first.
	parts := strings.Split(text, "```")
	for i, p := range parts {
		if i%2 == 1 && i != len(parts)-1 {
			p = strings.TrimPrefix(p, "\n")
			out.WriteString(`<pre class="c-mrkdwn__pre" data-stringify-type="pre"><div class="p-rich_text_block--no-overflow">`)
			out.WriteString(linkify(r, p))
			out.WriteString(`</div></pre>`)
			continue
		}
		if i%2 == 1 { // unterminated fence: keep literal
			p = "```" + p
		}
		out.WriteString(mrkdwnLines(r, p, big))
	}
	return out.String()
}

func mrkdwnLines(r Resolver, s string, big bool) string {
	lines := strings.Split(s, "\n")
	var out strings.Builder
	inQuote := false
	rest := false // >>> quotes everything that follows
	for i, ln := range lines {
		q := rest
		if !q && strings.HasPrefix(ln, "&gt;&gt;&gt;") {
			rest, q = true, true
			ln = strings.TrimPrefix(strings.TrimPrefix(ln, "&gt;&gt;&gt;"), " ")
		} else if !q && strings.HasPrefix(ln, "&gt;") {
			q = true
			ln = strings.TrimPrefix(strings.TrimPrefix(ln, "&gt;"), " ")
		}
		if q {
			if !inQuote {
				out.WriteString(`<blockquote type="cite" class="c-mrkdwn__quote" data-stringify-type="quote">`)
				inQuote = true
			} else {
				out.WriteString("<br>")
			}
		} else if inQuote {
			out.WriteString("</blockquote>")
			inQuote = false
		} else if i > 0 {
			out.WriteString("<br>")
		}
		out.WriteString(inline(r, ln, big))
	}
	if inQuote {
		out.WriteString("</blockquote>")
	}
	return out.String()
}

// Formatting works on plain text in which code spans and <...> tokens have
// been replaced by placeholders (\x01N\x01), so the style regexes can never
// touch generated HTML. Styles are first marked with control characters and
// only turned into tags after HTML escaping.
const (
	bOpen, bClose = "\x10", "\x11"
	iOpen, iClose = "\x12", "\x13"
	sOpen, sClose = "\x14", "\x15"
	ph            = "\x01"
)

const fmtBound = "\\s(\\[{>\"'\x01\x10-\x15"
const fmtEnd = "\\s.,!?;:)\\]}>\"'\x01\x10-\x15\\-"

var (
	codeRe   = regexp.MustCompile("`([^`\n]+)`")
	boldRe   = regexp.MustCompile(`(^|[` + fmtBound + `_~])\*([^*\n]*[^*\s\n])\*($|[` + fmtEnd + `_~])`)
	italRe   = regexp.MustCompile(`(^|[` + fmtBound + `*~])_([^_\n]*[^_\s\n])_($|[` + fmtEnd + `*~])`)
	strikeRe = regexp.MustCompile(`(^|[` + fmtBound + `*_])~([^~\n]*[^~\s\n])~($|[` + fmtEnd + `*_])`)
	phRe     = regexp.MustCompile("\x01([0-9]+)\x01")
	styleRep = strings.NewReplacer(
		bOpen, `<b data-stringify-type="bold">`, bClose, `</b>`,
		iOpen, `<i data-stringify-type="italic">`, iClose, `</i>`,
		sOpen, `<s data-stringify-type="strike">`, sClose, `</s>`,
	)
)

func applyStyle(re *regexp.Regexp, s, open, close string) string {
	for n := 0; n < 4; n++ { // repeat: adjacent spans share boundary characters
		next := re.ReplaceAllString(s, "${1}"+open+"${2}"+close+"${3}")
		if next == s {
			break
		}
		s = next
	}
	return s
}

// inline formats one line: code spans, links/mentions, bold/italic/strike, emoji.
// The input is Slack-escaped text (&amp; &lt; &gt;).
func inline(r Resolver, s string, big bool) string {
	var frags []string
	hold := func(html string) string {
		frags = append(frags, html)
		return ph + strconv.Itoa(len(frags)-1) + ph
	}
	s = strings.ReplaceAll(s, ph, "")
	s = codeRe.ReplaceAllStringFunc(s, func(m string) string {
		return hold(`<code class="c-mrkdwn__code" data-stringify-type="code">` + linkify(r, m[1:len(m)-1]) + `</code>`)
	})
	s = tokenRe.ReplaceAllStringFunc(s, func(m string) string {
		return hold(token(r, m[1:len(m)-1]))
	})
	s = slackUnescape(s)
	s = applyStyle(boldRe, s, bOpen, bClose)
	s = applyStyle(italRe, s, iOpen, iClose)
	s = applyStyle(strikeRe, s, sOpen, sClose)
	s = esc(s)
	s = styleRep.Replace(s)
	s = emojify(r, s, big)
	if len(frags) > 0 {
		s = phRe.ReplaceAllStringFunc(s, func(m string) string {
			i, err := strconv.Atoi(m[1 : len(m)-1])
			if err != nil || i >= len(frags) {
				return ""
			}
			return frags[i]
		})
	}
	return s
}

// tokenRe matches Slack's <...> control sequences. A user-typed "<" arrives
// escaped as &lt;, so only real tokens use literal angle brackets.
var tokenRe = regexp.MustCompile(`<([^<>\s][^<>]*?)>`)

// linkify escapes text and expands <...> tokens (links, mentions, dates)
// without applying any formatting (used in code spans and pre blocks).
func linkify(r Resolver, s string) string {
	var out strings.Builder
	last := 0
	for _, m := range tokenRe.FindAllStringSubmatchIndex(s, -1) {
		out.WriteString(esc(slackUnescape(s[last:m[0]])))
		out.WriteString(token(r, s[m[2]:m[3]]))
		last = m[1]
	}
	out.WriteString(esc(slackUnescape(s[last:])))
	return out.String()
}

// SafeURL returns u if its scheme is safe to put in an href, else "".
func SafeURL(u string) string {
	t := strings.TrimSpace(u)
	i := strings.IndexByte(t, ':')
	if i < 0 {
		if strings.HasPrefix(t, "/") || strings.HasPrefix(t, "#") {
			return t
		}
		return ""
	}
	switch strings.ToLower(t[:i]) {
	case "http", "https", "mailto", "tel", "slack", "ftp", "sms":
		return t
	}
	return ""
}

func linkHTML(href, label string) string {
	if h := SafeURL(href); h != "" {
		return `<a class="c-link" href="` + esc(h) + `" target="_blank" rel="noopener noreferrer" data-stringify-link="` + esc(h) + `">` + esc(label) + `</a>`
	}
	return esc(label)
}

func token(r Resolver, inner string) string {
	target, label, _ := strings.Cut(inner, "|")
	label = slackUnescape(label)
	switch {
	case strings.HasPrefix(target, "@"):
		id := target[1:]
		return UserMention(r, id, label)
	case strings.HasPrefix(target, "#"):
		id := target[1:]
		name := label
		if name == "" {
			name = r.ChannelName(id)
		}
		return `<a class="c-channel_entity c-link" data-channel-id="` + esc(id) + `" href="#/c/` + esc(id) + `">#` + esc(strings.TrimPrefix(name, "#")) + `</a>`
	case strings.HasPrefix(target, "!subteam^"):
		id := strings.TrimPrefix(target, "!subteam^")
		name := label
		if name == "" {
			name = "@" + r.UsergroupName(id)
		}
		return `<span class="c-mention_badge c-usergroup_entity" data-usergroup-id="` + esc(id) + `">` + esc(name) + `</span>`
	case target == "!here" || target == "!channel" || target == "!everyone":
		return `<span class="c-mention_badge">@` + esc(target[1:]) + `</span>`
	case strings.HasPrefix(target, "!date^"):
		return dateToken(target, label)
	case strings.HasPrefix(target, "!"):
		if label != "" {
			return esc(label)
		}
		return esc(target[1:])
	}
	href := slackUnescape(target)
	if label == "" {
		label = strings.TrimPrefix(href, "mailto:")
	}
	return linkHTML(href, label)
}

// UserMention renders an @user badge (highlighted when it is the viewer).
func UserMention(r Resolver, id, label string) string {
	name := r.UserName(id)
	if name == "" {
		name = label
	}
	if name == "" {
		name = id
	}
	name = strings.TrimPrefix(name, "@")
	cls := "c-member_slug c-member_slug--link"
	if id == r.SelfID() {
		cls += " c-member_slug--mention"
	}
	return `<a class="` + cls + `" data-member-id="` + esc(id) + `" data-stringify-type="mention" href="#/u/` + esc(id) + `">@` + esc(name) + `</a>`
}

func dateToken(target, fallback string) string {
	// !date^1392734382^{date_short} at {time}^optional_link
	parts := strings.Split(strings.TrimPrefix(target, "!date^"), "^")
	if len(parts) < 2 {
		return esc(fallback)
	}
	sec, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return esc(fallback)
	}
	return `<span class="c-timestamp-token" data-ts="` + strconv.FormatInt(sec, 10) + `" data-format="` + esc(parts[1]) + `">` + esc(FormatDate(time.Unix(sec, 0), parts[1])) + `</span>`
}

// FormatDate implements Slack's {date_*}/{time} tokens (server local time;
// the frontend re-renders data-ts in the viewer's zone).
func FormatDate(t time.Time, f string) string {
	o := ordinal(t.Day())
	rep := strings.NewReplacer(
		"{date_num}", t.Format("2006-01-02"),
		"{date_slash}", t.Format("01/02/2006"),
		"{date_long_full}", t.Format("Monday, January 2, 2006"),
		"{date_long}", t.Format("Monday, January ")+o,
		"{date_long_pretty}", t.Format("Monday, January ")+o,
		"{date_short}", t.Format("Jan 2, 2006"),
		"{date_short_pretty}", t.Format("Jan 2, 2006"),
		"{date}", t.Format("January ")+o+t.Format(", 2006"),
		"{date_pretty}", t.Format("January ")+o+t.Format(", 2006"),
		"{time_secs}", t.Format("3:04:05 PM"),
		"{time}", t.Format("3:04 PM"),
		"{ago}", t.Format("Jan 2, 2006"),
	)
	return rep.Replace(f)
}

func ordinal(d int) string {
	s := strconv.Itoa(d)
	switch {
	case d >= 11 && d <= 13:
		return s + "th"
	case d%10 == 1:
		return s + "st"
	case d%10 == 2:
		return s + "nd"
	case d%10 == 3:
		return s + "rd"
	}
	return s + "th"
}
