package richtext

import (
	"encoding/json"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// A timeline block: Slack's Timeline, drawn as TimelineCondensed (boot-
// shared 9B8s, its helpers rSPu and IiSM; CDN build 9f450b8ad3db): an
// agent's tool calls as one line that expands into each point and the
// sections of its steps. Its keys are decoded on their own
// (bkTimeline), so a point or a summary shaped unlike another block's
// same key never fails the message.

// Timeline is a timeline block.
type Timeline struct {
	BlockID string
	// Hidden: a task_attribution_ block (rSPu xA) Slack draws only with
	// its task attribution feature, which Clack's accounts don't have.
	Hidden bool
	Points []TimelinePoint
	// Summary is the summary's rich text (HasSummary: it has one).
	Summary    []Span
	HasSummary bool
	// Running: a point is in_progress and the message's stream hasn't
	// finished (IiSM u: ai_context.result_status, else streaming_state).
	Running bool
	// Thinking: a point is the "thinking" placeholder.
	Thinking bool
}

// TimelinePoint is one point (a tool call).
type TimelinePoint struct {
	ID     string
	Status string
	// Title is rich_text_title, else text (rSPu rA: a string as itself,
	// a rich_text block drawn, anything else its JSON).
	Title []Span
	// Icon is the point's icon (rSPu V1: Slack's icon names), IconURL an
	// image that takes its place.
	Icon, IconURL string
	// Sections are its steps' sections that have a rich_text_title, in
	// order (a "sources" step's sources gathered on its first section).
	Sections []TimelineSection
}

// TimelineSection is a step's section.
type TimelineSection struct {
	Title   []Span
	Sources int // sources the "(Sources)" link opens
	// SourceList are those sources as the block has them (the sources
	// modal's messages, files, web pages and memories).
	SourceList []json.RawMessage
}

// timelineIcons is rSPu V1: the icons a point may name, as Slack's SvgIcon
// names.
var timelineIcons = map[string]string{
	"ai-sparkle": "ai-sparkle", "agentforce--filled": "agentforce-filled", "agentforce-filled": "agentforce-filled",
	"calendar": "calendar", "book": "book", "mentions": "mentions", "ai-file-summary": "ai-file-summary", "user": "user",
	"search": "search", "search-files": "search-files", "settings": "settings", "canvas-content": "canvas-content",
	"error": "warning", "warning": "warning", "clock": "clock", "lists": "lists", "sf-cloud": "sf-cloud-filled",
	"email": "email", "ai-workflow": "ai-workflow",
}

// TimelineIcon is a point's icon as Slack names it ("" when the point
// names none Slack knows).
func TimelineIcon(name string) string { return timelineIcons[name] }

// bkTimeline is a timeline block's keys, raw.
type bkTimeline struct {
	Points  json.RawMessage `json:"points"`
	Summary json.RawMessage `json:"summary"`
	BlockID json.RawMessage `json:"block_id"`
}

type bkTimelinePoint struct {
	ID            json.RawMessage `json:"id"`
	Status        json.RawMessage `json:"status"`
	Text          json.RawMessage `json:"text"`
	RichTextTitle json.RawMessage `json:"rich_text_title"`
	RichCamel     json.RawMessage `json:"richTextTitle"`
	Icon          json.RawMessage `json:"icon"`
	IconURL       json.RawMessage `json:"icon_url"`
	IconCamel     json.RawMessage `json:"iconUrl"`
	Steps         json.RawMessage `json:"steps"`
}

type bkTimelineStep struct {
	ID       json.RawMessage `json:"id"`
	Sections json.RawMessage `json:"sections"`
}

type bkTimelineSection struct {
	RichTextTitle json.RawMessage `json:"rich_text_title"`
	RichCamel     json.RawMessage `json:"richTextTitle"`
	Sources       json.RawMessage `json:"sources"`
}

// timelineBlock builds a timeline block (nothing for no points:
// TimelineCondensed returns null).
func (c *builder) timelineBlock(out []Block, b *bk) []Block {
	v := b.timeline
	if v == nil {
		return out
	}
	tl := &Timeline{BlockID: b.BlockID, Hidden: strings.HasPrefix(b.BlockID, "task_attribution_")}
	var points []json.RawMessage
	if json.Unmarshal(v.Points, &points) != nil {
		return out
	}
	for _, raw := range points {
		var p bkTimelinePoint
		if json.Unmarshal(raw, &p) != nil {
			continue
		}
		pt := TimelinePoint{ID: rawString(p.ID), Status: rawString(p.Status), Icon: rawString(p.Icon), IconURL: SafeImage(rawString(firstRaw(p.IconURL, p.IconCamel)))}
		title := firstRaw(p.RichTextTitle, p.RichCamel)
		if len(title) == 0 || string(title) == "null" {
			title = p.Text
		}
		pt.Title = c.timelineText(title)
		var steps []bkTimelineStep
		_ = json.Unmarshal(p.Steps, &steps)
		for _, st := range steps {
			var secs []bkTimelineSection
			if json.Unmarshal(st.Sections, &secs) != nil {
				continue
			}
			// rSPu Gw: a "sources" step's sources go on its first
			// section without any of its own (de-duplicated)
			if rawString(st.ID) == "sources" {
				secs = gatherSources(secs)
			}
			for _, sec := range secs {
				t := firstRaw(sec.RichTextTitle, sec.RichCamel)
				if len(t) == 0 || t[0] != '{' { // i.richTextTitle && …: a section without one is skipped
					continue
				}
				var srcs []json.RawMessage
				_ = json.Unmarshal(sec.Sources, &srcs)
				pt.Sections = append(pt.Sections, TimelineSection{Title: c.timelineText(t), Sources: len(srcs), SourceList: srcs})
			}
		}
		if pt.Status == "in_progress" {
			tl.Running = true
		}
		if pt.ID == "thinking" {
			tl.Thinking = true
		}
		tl.Points = append(tl.Points, pt)
	}
	if len(tl.Points) == 0 {
		return out
	}
	if len(v.Summary) > 0 && string(v.Summary) != "null" {
		tl.Summary, tl.HasSummary = c.timelineText(v.Summary), true
	}
	if tl.Running && c.streamDone {
		tl.Running = false
	}
	return append(out, Block{Kind: TimelineBlock, BlockID: b.BlockID, Timeline: tl})
}

// timelineText is rSPu rA: a string as itself, a rich_text block drawn
// (its sections inline), anything else its JSON.
func (c *builder) timelineText(raw json.RawMessage) []Span {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		return plainSpans(c.r, t)
	case map[string]any:
		if typ, _ := t["type"].(string); typ == "rich_text" {
			var b bk
			if json.Unmarshal(raw, &b) == nil {
				return flattenSpans(c.richText(b.Elements))
			}
			return nil
		}
	}
	return []Span{{Kind: Text, Text: string(raw)}}
}

// gatherSources is rSPu Gw: every section's sources, de-duplicated, on
// the first section that has none of its own (the others keep theirs
// out); unchanged when no section has sources or every one does.
func gatherSources(secs []bkTimelineSection) []bkTimelineSection {
	seen := map[string]bool{}
	var all []json.RawMessage
	for _, s := range secs {
		var srcs []json.RawMessage
		_ = json.Unmarshal(s.Sources, &srcs)
		for _, src := range srcs {
			k := TimelineSourceKey(src)
			if !seen[k] {
				seen[k] = true
				all = append(all, src)
			}
		}
	}
	if len(all) == 0 {
		return secs
	}
	var without []bkTimelineSection
	for _, s := range secs {
		var srcs []json.RawMessage
		if json.Unmarshal(s.Sources, &srcs) != nil || len(srcs) == 0 {
			without = append(without, s)
		}
	}
	if len(without) == 0 {
		return secs
	}
	merged, _ := json.Marshal(all)
	without[0].Sources = merged
	return without
}

// TimelineSourceKey is rSPu tm (getSourceKey): a source's identity.
func TimelineSourceKey(raw json.RawMessage) string {
	var s map[string]any
	if json.Unmarshal(raw, &s) != nil {
		return "richtext-"
	}
	str := func(k string) string { v, _ := s[k].(string); return v }
	if _, ok := s["memory_id"]; ok {
		return "memory-" + jsJoinString(s["memory_id"], true)
	}
	if _, ok := s["type"]; ok {
		switch str("type") {
		case "message":
			return "message-" + firstNonEmpty(str("channelId"), str("channel_id")) + "-" + str("timestamp")
		case "file":
			return "file-" + str("url")
		case "calendar_event":
			return "calendar-" + str("url")
		case "web":
			return "web-" + str("url")
		}
	}
	return "richtext-" + str("blockId")
}

// streamDone is IiSM u: a message whose agent turn has finished (its
// ai_context.result_status isn't pending, streaming or
// awaiting_user_action; without one, its streaming_state is completed or
// errored).
func streamDone(m *slack.Message) bool {
	if m == nil {
		return false
	}
	if m.AIContext != nil && m.AIContext.ResultStatus != "" {
		switch m.AIContext.ResultStatus {
		case "pending", "streaming", "awaiting_user_action":
			return false
		}
		return true
	}
	return m.StreamingState == "completed" || m.StreamingState == "errored"
}
