package popups

// A timeline section's "(Sources)": Slack's TimelineSourcesModal (boot-
// shared +sSl, CDN build 9f450b8ad3db): "Sources" (820px at most), the
// section's title, then its sources (de-duplicated by getSourceKey)
// grouped as Messages, Files, Web and Memories:
//
//   - a message (SourceMessageItem): its sender, its channel and its time
//     ("{date_short_pretty} {time}", the link), then its text cut at 165
//     characters or 3 lines ("…"); one the client doesn't hold, its
//     channel and time only;
//   - a file (FileSourceItem): its title, name, or the URL's file name;
//   - a web page (WebSourceItem): its title, else the URL's host, and its
//     snippet;
//   - a memory (MemorySourceItem): a topic's rich_text_title, else its
//     rich_text_content.
//
// A message's or a file's link closes the modal before it opens (their
// closeModal); a web page's leaves it open.
//
// Left out: the rows' looks (the message's avatar, the file's thumbnail,
// the page's favicon), the message's text drawn as its blocks (Clack
// draws its plain text), the batch fetch of messages the client doesn't
// hold, and a memory's click (Slackbot's Memories tab).

import (
	"encoding/json"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/richview"
)

var nsTimeline = i18n.NS("timeline")

// sourcesHandler opens the modal's links: a message's or a file's after
// closing the modal (closes), a web page's with it left open.
type sourcesHandler struct {
	topicHandler
	m      **Modal
	closes map[string]bool
}

func (s sourcesHandler) OpenURL(u string) {
	if s.closes[u] && *s.m != nil {
		(*s.m).Dismiss()
	}
	s.topicHandler.OpenURL(u)
}

// OpenTimelineSources is a timeline section's "(Sources)".
func OpenTimelineSources(h Host, sec *richtext.TimelineSection) {
	if sec == nil {
		return
	}
	doc, closes := timelineSourcesDoc(h, sec)
	v := richview.New(doc, richview.Options{NoHoverCards: true})
	var m *Modal
	hd := sourcesHandler{topicHandler{h}, &m, closes}
	m = NewModal(h, ModalOpts{Title: nsTimeline.T("Sources"), Width: 820, Body: func(gtx layout.Context) layout.Dimensions {
		if RichStyle == nil {
			return layout.Dimensions{}
		}
		st := RichStyle(h)
		if st == nil {
			return layout.Dimensions{}
		}
		return v.Layout(gtx, st, hd)
	}})
	h.ShowOverlay(m)
}

// timelineSource is one source as the modal reads it.
type timelineSource struct {
	Type      string          `json:"type"`
	ChannelID string          `json:"channel_id"`
	ChanCamel string          `json:"channelId"`
	Timestamp string          `json:"timestamp"`
	URL       string          `json:"url"`
	Title     string          `json:"title"`
	Name      string          `json:"name"`
	Snippet   string          `json:"snippet"`
	MemoryID  json.RawMessage `json:"memory_id"`
	// a memory's: memory_type "topic" shows rich_text_title, else
	// rich_text_content
	MemoryType  string          `json:"memory_type"`
	RichTitle   json.RawMessage `json:"rich_text_title"`
	RichContent json.RawMessage `json:"rich_text_content"`
}

// sourceTextMax and sourceLinesMax cut a source message's text
// (TruncatedText's maxCharacters 165, maxNewlines 3).
const (
	sourceTextMax  = 165
	sourceLinesMax = 3
)

// cutSourceText is TruncatedText's cut: at 165 characters or the third
// line break, with "…".
func cutSourceText(s string) string {
	r := []rune(s)
	nl := 0
	for i, c := range r {
		if c == '\n' {
			if nl++; nl >= sourceLinesMax {
				return strings.TrimRight(string(r[:i]), " ") + "…"
			}
		}
		if i >= sourceTextMax {
			return strings.TrimRight(string(r[:i]), " ") + "…"
		}
	}
	return s
}

// timelineSourcesDoc is TimelineSourcesBody as a document, and the links
// that close the modal.
func timelineSourcesDoc(h Host, sec *richtext.TimelineSection) (richtext.Doc, map[string]bool) {
	var d richtext.Doc
	closes := map[string]bool{}
	if len(sec.Title) > 0 {
		d.Blocks = append(d.Blocks, richtext.Block{Kind: richtext.Paragraph, Spans: sec.Title})
	}
	seen := map[string]bool{}
	var msgs, files, web, mems []timelineSource
	for _, raw := range sec.SourceList {
		k := richtext.TimelineSourceKey(raw)
		if seen[k] {
			continue
		}
		seen[k] = true
		var s timelineSource
		if json.Unmarshal(raw, &s) != nil {
			continue
		}
		switch {
		case len(s.MemoryID) > 0:
			mems = append(mems, s)
		case s.Type == "message":
			msgs = append(msgs, s)
		case s.Type == "file":
			files = append(files, s)
		case s.Type == "web":
			web = append(web, s)
		}
	}
	heading := func(t string) {
		d.Blocks = append(d.Blocks, richtext.Block{Kind: richtext.Paragraph, Spans: []richtext.Span{{Kind: richtext.Text, Text: nsTimeline.T(t), Style: richtext.Bold}}})
	}
	link := func(label, u string) richtext.Span {
		if u = richtext.SafeURL(u); u == "" {
			return richtext.Span{Kind: richtext.Text, Text: label}
		}
		return richtext.Span{Kind: richtext.Link, Text: label, URL: u}
	}
	if len(msgs) > 0 {
		heading("Messages")
		for _, s := range msgs {
			ch := firstNonEmptyStr(s.ChannelID, s.ChanCamel)
			u := permalink(h, ch, s.Timestamp, "")
			var spans []richtext.Span
			var m slack.Message
			held := false
			if c := h.Core(); c != nil {
				m, held = c.S.FindHeld(ch, s.Timestamp)
				if held {
					// getMemberRealName of its sender
					if usr, ok := c.S.User(m.User); ok && usr.RealName != "" {
						spans = append(spans, richtext.Span{Kind: richtext.Text, Text: usr.RealName, Style: richtext.Bold}, richtext.Span{Kind: richtext.Text, Text: " "})
					}
				}
				if n := strings.TrimPrefix(c.S.ChannelName(ch), "#"); n != "" && n != ch {
					spans = append(spans, richtext.Span{Kind: richtext.Text, Text: "#" + n + " "})
				}
			}
			when := s.Timestamp
			if sec, err := strconv.ParseFloat(s.Timestamp, 64); err == nil {
				when = richtext.FormatDate(time.Unix(int64(sec), 0), "{date_short_pretty} {time}")
			}
			spans = append(spans, link(when, u))
			closes[richtext.SafeURL(u)] = true
			if held {
				if c := h.Core(); c != nil {
					if t := cutSourceText(strings.TrimSpace(richtext.PlainText(richtext.FromMessage(c.S, &m)))); t != "" {
						spans = append(spans, richtext.Span{Kind: richtext.Break}, richtext.Span{Kind: richtext.Text, Text: t})
					}
				}
			}
			d.Blocks = append(d.Blocks, richtext.Block{Kind: richtext.Paragraph, Spans: spans})
		}
	}
	if len(files) > 0 {
		heading("Files")
		for _, s := range files {
			closes[richtext.SafeURL(s.URL)] = true
			d.Blocks = append(d.Blocks, richtext.Block{Kind: richtext.Paragraph, Spans: []richtext.Span{link(firstNonEmptyStr(s.Title, s.Name, fileNameOf(s.URL)), s.URL)}})
		}
	}
	if len(web) > 0 {
		heading("Web")
		for _, s := range web {
			spans := []richtext.Span{link(firstNonEmptyStr(s.Title, hostOf(s.URL)), s.URL)}
			if s.Snippet != "" {
				spans = append(spans, richtext.Span{Kind: richtext.Break}, richtext.Span{Kind: richtext.Text, Text: s.Snippet})
			}
			d.Blocks = append(d.Blocks, richtext.Block{Kind: richtext.Paragraph, Spans: spans})
		}
	}
	if len(mems) > 0 {
		heading("Memories")
		for _, s := range mems {
			raw := s.RichContent
			if s.MemoryType == "topic" && len(s.RichTitle) > 0 && string(s.RichTitle) != "null" {
				raw = s.RichTitle
			}
			if spans := richTextSpans(h, raw); len(spans) > 0 {
				d.Blocks = append(d.Blocks, richtext.Block{Kind: richtext.Paragraph, Spans: spans})
			}
		}
	}
	delete(closes, "")
	return d, closes
}

// richTextSpans is a rich_text block's spans (a memory's title or
// content), nothing for anything else.
func richTextSpans(h Host, raw json.RawMessage) []richtext.Span {
	if len(raw) == 0 || raw[0] != '{' {
		return nil
	}
	c := h.Core()
	if c == nil {
		return nil
	}
	d := richtext.FromMessage(c.S, &slack.Message{Blocks: json.RawMessage("[" + string(raw) + "]")})
	var out []richtext.Span
	for _, b := range d.Blocks {
		if len(out) > 0 {
			out = append(out, richtext.Span{Kind: richtext.Break})
		}
		out = append(out, b.Spans...)
	}
	return out
}

// hostOf is new URL(url).hostname, else the URL itself.
func hostOf(u string) string {
	if p, err := url.Parse(u); err == nil && p.Hostname() != "" {
		return p.Hostname()
	}
	return u
}

// fileNameOf is getFileNameFromUrl: the URL path's last part.
func fileNameOf(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return u
	}
	if b := path.Base(p.Path); b != "." && b != "/" {
		return b
	}
	return u
}
