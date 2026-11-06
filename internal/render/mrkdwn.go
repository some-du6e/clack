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
			out.WriteString(linkify(r, p, false))
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
	for i, ln := range lines {
		q := strings.HasPrefix(ln, "&gt; ") || ln == "&gt;" || strings.HasPrefix(ln, "> ")
		if q {
			ln = strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(ln, "&gt;"), ">"), " ")
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

var (
	codeRe   = regexp.MustCompile("`([^`\n]+)`")
	boldRe   = regexp.MustCompile(`(^|[\s(\[{>_~"'])\*([^*\n]*[^*\s\n])\*`)
	italRe   = regexp.MustCompile(`(^|[\s(\[{>*~"'])_([^_\n]*[^_\s\n])_`)
	strikeRe = regexp.MustCompile(`(^|[\s(\[{>*_"'])~([^~\n]*[^~\s\n])~`)
)

// inline formats one line: code spans, links/mentions, bold/italic/strike, emoji.
func inline(r Resolver, s string, big bool) string {
	// protect code spans
	var codes []string
	s = codeRe.ReplaceAllStringFunc(s, func(m string) string {
		codes = append(codes, `<code class="c-mrkdwn__code" data-stringify-type="code">`+linkify(r, m[1:len(m)-1], false)+`</code>`)
		return "\x00" + strconv.Itoa(len(codes)-1) + "\x00"
	})
	s = linkify(r, s, true)
	s = boldRe.ReplaceAllString(s, `$1<b data-stringify-type="bold">$2</b>`)
	s = italRe.ReplaceAllString(s, `$1<i data-stringify-type="italic">$2</i>`)
	s = strikeRe.ReplaceAllString(s, `$1<s data-stringify-type="strike">$2</s>`)
	s = emojify(r, s, big)
	if len(codes) > 0 {
		for i, c := range codes {
			s = strings.Replace(s, "\x00"+strconv.Itoa(i)+"\x00", c, 1)
		}
	}
	return s
}

var angleRe = regexp.MustCompile(`&lt;([^&\s][^>]*?)&gt;|<([^<>\s][^<>]*?)>`)

// linkify escapes text and expands <...> tokens (links, mentions, dates).
// The input is Slack-escaped text (&amp; &lt; &gt;).
func linkify(r Resolver, s string, rich bool) string {
	var out strings.Builder
	last := 0
	for _, m := range angleRe.FindAllStringSubmatchIndex(s, -1) {
		out.WriteString(esc(slackUnescape(s[last:m[0]])))
		var inner string
		if m[2] >= 0 {
			inner = s[m[2]:m[3]]
		} else {
			inner = s[m[4]:m[5]]
		}
		out.WriteString(token(r, inner, rich))
		last = m[1]
	}
	out.WriteString(esc(slackUnescape(s[last:])))
	return out.String()
}

func token(r Resolver, inner string, rich bool) string {
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
		return `<a class="c-channel_entity c-link" data-channel-id="` + esc(id) + `" href="#/c/` + esc(id) + `">#` + esc(name) + `</a>`
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
		label = href
		label = strings.TrimPrefix(label, "mailto:")
	}
	if !rich {
		return `<a class="c-link" href="` + esc(href) + `" target="_blank" rel="noopener noreferrer">` + esc(label) + `</a>`
	}
	return `<a class="c-link" href="` + esc(href) + `" target="_blank" rel="noopener noreferrer" data-stringify-link="` + esc(href) + `">` + esc(label) + `</a>`
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
