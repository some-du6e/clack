package popups

// A timeline section's "(Sources)": Slack's TimelineSourcesModal (boot-
// shared +sSl, CDN build 9f450b8ad3db): "Sources" (820px at most), the
// section's title, then its sources (de-duplicated by getSourceKey)
// grouped as Messages, Files, Web and Memories. Each message is a link to
// it ("Source message from {sender} in {channel}" and its text when the
// client holds it, else the link itself), a file its name, a web page its title (else its
// address) and snippet, a memory its topic.
//
// Left out: the rows' looks (the message's avatar and timestamp, the
// file's thumbnail, the page's favicon) and the batch fetch of messages
// the client doesn't hold, which show as a link to the message.

import (
	"encoding/json"
	"net/url"
	"path"
	"strings"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
)

var nsTimeline = i18n.NS("timeline")

// OpenTimelineSources is a timeline section's "(Sources)".
func OpenTimelineSources(h Host, sec *richtext.TimelineSection) {
	if sec == nil {
		return
	}
	v := richview.New(timelineSourcesDoc(h, sec), richview.Options{NoHoverCards: true})
	m := NewModal(h, ModalOpts{Title: nsTimeline.T("Sources"), Width: 820, Body: func(gtx layout.Context) layout.Dimensions {
		if RichStyle == nil {
			return layout.Dimensions{}
		}
		st := RichStyle(h)
		if st == nil {
			return layout.Dimensions{}
		}
		return v.Layout(gtx, st, topicHandler{h})
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
	Topic     string          `json:"topic_name"`
}

// timelineSourcesDoc is TimelineSourcesBody as a document.
func timelineSourcesDoc(h Host, sec *richtext.TimelineSection) richtext.Doc {
	var d richtext.Doc
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
			label, text := u, ""
			if c := h.Core(); c != nil {
				if m, ok := c.S.FindHeld(ch, s.Timestamp); ok {
					sender := c.S.UserName(m.User)
					if sender == "" {
						sender = m.Username
					}
					label = nsTimeline.T("Source message from {sender} in {channel}", "sender", sender, "channel", "#"+strings.TrimPrefix(c.S.ChannelName(ch), "#"))
					text = m.Text
				}
			}
			spans := []richtext.Span{link(label, u)}
			if text != "" {
				spans = append(spans, richtext.Span{Kind: richtext.Break}, richtext.Span{Kind: richtext.Text, Text: text})
			}
			d.Blocks = append(d.Blocks, richtext.Block{Kind: richtext.Paragraph, Spans: spans})
		}
	}
	if len(files) > 0 {
		heading("Files")
		for _, s := range files {
			d.Blocks = append(d.Blocks, richtext.Block{Kind: richtext.Paragraph, Spans: []richtext.Span{link(firstNonEmptyStr(s.Title, s.Name, fileNameOf(s.URL)), s.URL)}})
		}
	}
	if len(web) > 0 {
		heading("Web")
		for _, s := range web {
			spans := []richtext.Span{link(firstNonEmptyStr(s.Title, s.URL), s.URL)}
			if s.Snippet != "" {
				spans = append(spans, richtext.Span{Kind: richtext.Break}, richtext.Span{Kind: richtext.Text, Text: s.Snippet})
			}
			d.Blocks = append(d.Blocks, richtext.Block{Kind: richtext.Paragraph, Spans: spans})
		}
	}
	if len(mems) > 0 {
		heading("Memories")
		for _, s := range mems {
			if s.Topic != "" {
				d.Blocks = append(d.Blocks, richtext.Block{Kind: richtext.Paragraph, Spans: []richtext.Span{{Kind: richtext.Text, Text: s.Topic}}})
			}
		}
	}
	return d
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
