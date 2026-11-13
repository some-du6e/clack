package richview

import (
	"context"
	"html"
	"io"
	"strings"

	"gioui.org/io/clipboard"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/richclip"
)

// Copying a selection as Slack does (the clipboard handler, boot-shared
// SC+N, with Djgz's builders): text/plain is the selection's text
// ("@karim" for a mention), text/html the cleaned selection
// (cleanHTMLForExternal: inline styles, no classes or data- attributes,
// so a mention is a styled "@karim" link), and slack/html the selection's
// own markup, where a mention keeps MemberSlug's data-stringify-type,
// -id and -label (and a user group or broadcast the same marks), which
// Slack's composer, and Clack's, paste as a mention.

// Djgz's palette for the cleaned HTML.
const (
	chLink    = "#1264a3"
	chMention = "#e8f5fa"
	chText    = "#1d1c1d"
)

// HTMLRange is the plain-text range [a, b)'s markup: Slack's slack/html
// (data-stringify marks kept) and its text/html (cleaned).
func (v *View) HTMLRange(a, b int32) (slackHTML, cleanHTML string) {
	n := int32(len(v.d.plain))
	a, b = min(max(a, 0), n), min(max(b, 0), n)
	if a >= b {
		return "", ""
	}
	// each byte's target (-1: plain text)
	tg := make([]int16, b-a)
	for i := range tg {
		tg[i] = -1
	}
	for i := range v.d.frags {
		f := &v.d.frags[i]
		if f.target < 0 || int(f.target) >= len(v.d.targets) || f.p1 <= a || f.p0 >= b {
			continue
		}
		for p := max(f.p0, a); p < min(f.p1, b); p++ {
			tg[p-a] = f.target
		}
	}
	var s, c strings.Builder
	text := func(t string) {
		for i, part := range strings.Split(t, "\n") {
			if i > 0 {
				s.WriteString("<br>")
				c.WriteString("<br>")
			}
			e := html.EscapeString(part)
			s.WriteString(e)
			c.WriteString(e)
		}
	}
	for i := int32(0); i < b-a; {
		j := i
		for j < b-a && tg[j] == tg[i] {
			j++
		}
		seg := string(v.d.plain[a+i : a+j])
		t := tg[i]
		i = j
		if t < 0 {
			text(seg)
			continue
		}
		tgt := &v.d.targets[t]
		e := html.EscapeString(seg)
		full := html.EscapeString(strings.TrimSpace(v.targetText(t)))
		id := html.EscapeString(tgt.id)
		mentionStyle := "color: " + chLink + "; text-decoration: none; background-color: " + chMention + "; color: " + chLink + "; padding: 0 2px; border-radius: 3px; font-weight: 500"
		switch {
		case tgt.kind == tUser && tgt.id != "":
			s.WriteString(`<a target="_blank" class="c-link c-member_slug c-member_slug--link" data-member-id="` + id + `" data-member-label="` + full +
				`" data-stringify-type="mention" data-stringify-id="` + id + `" data-stringify-label="` + full + `" href="/team/` + id + `">` + e + `</a>`)
			c.WriteString(`<a target="_blank" href="/team/` + id + `" style="` + mentionStyle + `">` + e + `</a>`)
		case tgt.kind == tUsergroup && tgt.id != "":
			s.WriteString(`<span class="c-mrkdwn__user_group" data-stringify-type="mention" data-stringify-id="` + id + `" data-stringify-label="` + full + `">` + e + `</span>`)
			c.WriteString(`<span style="` + mentionStyle + `">` + e + `</span>`)
		case tgt.kind == tChannel && tgt.id != "":
			name := html.EscapeString(strings.TrimPrefix(strings.TrimSpace(v.targetText(t)), "#"))
			s.WriteString(`<a class="c-link c-mrkdwn__channel" data-channel-id="` + id + `" data-channel-name="` + name +
				`" data-stringify-type="channel" href="/archives/` + id + `">` + e + `</a>`)
			c.WriteString(`<a href="/archives/` + id + `" style="color: ` + chLink + `; text-decoration: none; color: ` + chLink + `; text-decoration: none; font-weight: 500">` + e + `</a>`)
		case tgt.kind == tLink && tgt.url != "":
			u := html.EscapeString(tgt.url)
			s.WriteString(`<a target="_blank" class="c-link" data-stringify-link="` + u + `" href="` + u + `" rel="noopener noreferrer">` + e + `</a>`)
			c.WriteString(`<a target="_blank" href="` + u + `" rel="noopener noreferrer" style="color: ` + chLink + `; text-decoration: none">` + e + `</a>`)
		default:
			text(seg)
		}
	}
	return `<meta charset="utf-8">` + s.String(), `<div style="margin: 0; padding: 0; color: ` + chText + `">` + c.String() + `</div>`
}

// CopyRich puts a copy on the clipboard: Slack's three types through
// richclip where the platform has its rich clipboard (Windows, macOS),
// else the text through Gio.
func CopyRich(gtx layout.Context, text, slackHTML, cleanHTML string) {
	if text == "" {
		return
	}
	if !richclip.Supported() {
		gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(text))})
		return
	}
	c := richclip.Copy{Text: text, HTML: cleanHTML}
	if slackHTML != "" {
		c.Custom = map[string]string{"slack/html": slackHTML}
	}
	go func() { _ = richclip.Write(context.Background(), c) }()
}
