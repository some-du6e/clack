package huddleui

import (
	"strings"

	"github.com/some-du6e/clack/internal/i18n"
)

// Slack's strings for the huddle UI (internal/i18n, docs/I18N.md): the
// huddles namespace, which Slack's huddle window, bar, invite window,
// captions and menus all use.
var nsHuddles = i18n.NS("huddles")

// boldSpans formats a huddles string whose string placeholders rt() draws
// as bold elements ("{senderName} is inviting you to a huddle in
// {channelName}"): the parts in the locale's order, the placeholders'
// values bold.
func boldSpans(src string, args ...any) []span {
	for i := 1; i < len(args); i += 2 {
		if s, ok := args[i].(string); ok {
			args[i] = "\x01" + s + "\x02"
		}
	}
	s := i18n.StripTags(nsHuddles.T(src, args...))
	var out []span
	for s != "" {
		i := strings.IndexByte(s, 1)
		if i < 0 {
			out = append(out, span{s, false})
			break
		}
		if i > 0 {
			out = append(out, span{s[:i], false})
		}
		j := strings.IndexByte(s[i:], 2)
		if j < 0 {
			out = append(out, span{s[i+1:], true})
			break
		}
		out = append(out, span{s[i+1 : i+j], true})
		s = s[i+j+1:]
	}
	return out
}

// listifyNames is Slack's listify (client-boot nTGe): "A and B", "A, B,
// and C" in en-US; the locale's separators and the general namespace's
// "and" elsewhere ("A、B、C" in ja-JP).
func listifyNames(items []string) string {
	n := len(items)
	if n == 0 {
		return ""
	}
	sep, serial, conj := ",", false, true
	word := i18n.T("general", "and")
	switch i18n.Locale() {
	case i18n.EnUS:
		serial = true
	case "ja-JP":
		conj, sep = false, "、"
	case "zh-CN", "zh-TW":
		sep = "、"
	}
	last := sep + " "
	switch {
	case serial && conj && n > 2:
		last = sep + " " + word + " "
	case conj:
		last = " " + word + " "
	}
	var b strings.Builder
	for i, s := range items {
		b.WriteString(s)
		switch {
		case i < n-2:
			b.WriteString(sep + " ")
		case i < n-1:
			b.WriteString(last)
		}
	}
	return b.String()
}

// listifyNoConj is listify with noConj (HuddleChannelName's other
// members): the locale's separator between every name, no "and".
func listifyNoConj(items []string) string {
	sep := ","
	switch i18n.Locale() {
	case "ja-JP", "zh-CN", "zh-TW":
		sep = "、"
	}
	return strings.Join(items, sep+" ")
}

// linkParts splits a huddles string with one <a>…</a> (or <l>…</l>) link
// into the text before it, the link's text and the text after, tags
// stripped and the edges trimmed.
func linkParts(src string) (before, link, after string) {
	s := nsHuddles.T(src)
	for _, tag := range []string{"a", "l"} {
		i := strings.Index(s, "<"+tag+">")
		j := strings.Index(s, "</"+tag+">")
		if i >= 0 && j > i {
			return strings.TrimSpace(i18n.StripTags(s[:i])), i18n.StripTags(s[i+len(tag)+2 : j]), strings.TrimSpace(i18n.StripTags(s[j+len(tag)+3:]))
		}
	}
	return strings.TrimSpace(i18n.StripTags(s)), "", ""
}

// dmNames is a DM's or group DM's other people, listed ("" when none is
// known): getStartHuddleTip's {dmPersons}.
func (c *Controller) dmNames(ch string) string {
	app := c.h.Core()
	if app == nil {
		return ""
	}
	conv, ok := app.Conv(ch)
	if !ok {
		return ""
	}
	if conv.Kind == "im" {
		return c.userName(conv.User)
	}
	var names []string
	self := c.selfID()
	for _, m := range conv.Members {
		if m != self {
			if n := c.userName(m); n != "" {
				names = append(names, n)
			}
		}
	}
	return strings.Join(names, ", ")
}
