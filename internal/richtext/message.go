package richtext

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// FromMessage builds the document for a message: its rich blocks (or the
// mrkdwn text when there are none), then huddle cards, files and
// attachments/unfurls, in Slack's order. Deleted messages become a single
// Tombstone block.
func FromMessage(r Resolver, m *slack.Message) Doc {
	if m == nil {
		return Doc{}
	}
	if m.Subtype == "message_deleted" || m.Subtype == "tombstone" {
		return Doc{Blocks: []Block{tombstone("This message was deleted.")}}
	}
	c := &builder{r: r}
	if len(m.Files) > 0 {
		c.files = make(map[string]*slack.File, len(m.Files))
		c.used = map[string]bool{}
		for i := range m.Files {
			c.files[m.Files[i].ID] = &m.Files[i]
		}
	}
	var d Doc
	if bs := parseBlocks(m.Blocks); len(bs) > 0 {
		d.Blocks = c.blocks(bs)
		d.Jumbo = richOnlyEmoji(bs)
	}
	if len(d.Blocks) == 0 && m.Text != "" {
		d.Blocks = FromMrkdwn(r, m.Text)
		d.Jumbo = onlyEmoji(r, m.Text)
	}
	if m.Subtype == "huddle_thread" || len(m.Room) > 2 {
		if b, ok := huddleBlock(r, m.Room); ok {
			d.Blocks = append(d.Blocks, b)
		}
	}
	for i := range m.Files {
		if !c.used[m.Files[i].ID] {
			d.Blocks = append(d.Blocks, c.fileBlock(&m.Files[i]))
		}
	}
	for i := range m.Attachments {
		if b, ok := c.attachment(&m.Attachments[i]); ok {
			d.Blocks = append(d.Blocks, b)
		}
	}
	return d
}

func tombstone(s string) Block {
	return Block{Kind: Tombstone, Spans: []Span{{Kind: Text, Text: s}}}
}

// fileBlock converts a shared file. Deleted/hidden files become Tombstones.
func (c *builder) fileBlock(f *slack.File) Block {
	switch f.Mode {
	case "tombstone":
		return tombstone("This file was deleted.")
	case "hidden_by_limit":
		return tombstone("This file is hidden because it is older than the workspace's visible history.")
	}
	return Block{Kind: FileBlock, File: convertFile(f)}
}

func convertFile(f *slack.File) *File {
	out := &File{
		ID: f.ID, Name: f.Name, Title: f.Title, Mime: f.Mimetype, Type: f.Filetype, PrettyType: f.PrettyType,
		Size: f.Size, URL: SafeURL(f.URLPrivate), Download: SafeURL(f.URLPrivateDownload), Permalink: SafeURL(f.Permalink),
		Lines: f.Lines, DurationMS: f.DurationMS, OrigW: f.OriginalW, OrigH: f.OriginalH,
		Subtype: f.Subtype, Waveform: f.AudioWaveSamples, User: f.User, Created: f.Created, IsExternal: f.IsExternal,
	}
	if out.Title == out.Name {
		out.Title = ""
	}
	mime := strings.ToLower(f.Mimetype)
	switch {
	case strings.HasPrefix(mime, "image/"):
		out.IsImage = true
		out.Thumb = imageThumb(f)
	case strings.HasPrefix(mime, "video/"):
		out.IsVideo = true
		out.URL = SafeURL(firstNonEmpty(f.MP4, f.URLPrivate))
		// FileGalleryVideoFile: original_w || thumb_video_w for the box,
		// thumb_video_w || original_w for the poster's own size
		out.Thumb = image(f.ThumbVideo, f.Name, firstPos(f.ThumbVideoW, f.OriginalW), firstPos(f.ThumbVideoH, f.OriginalH))
		out.OrigW, out.OrigH = firstPos(f.OriginalW, f.ThumbVideoW), firstPos(f.OriginalH, f.ThumbVideoH)
	case strings.HasPrefix(mime, "audio/") || f.Subtype == "slack_audio":
		out.IsAudio = true
		if f.MP4 != "" {
			out.URL = SafeURL(f.MP4)
		}
	default:
		out.Thumb = largestThumb(f)
	}
	if !out.IsImage && !out.IsVideo && !out.IsAudio && f.Preview != "" &&
		(f.Mode == "snippet" || f.Mode == "post" || f.Filetype == "text" || f.Lines > 0 || strings.HasPrefix(mime, "text/")) {
		out.IsSnippet = true
		out.Preview = f.Preview
	}
	return out
}

// imageThumb picks the thumbnail Slack shows inline (thumb_1024 first, then
// the next largest) with its pixel size: Slack sizes inline images from the
// 1024 thumb's dimensions, up to the gallery width. Animated GIFs use the
// original.
func imageThumb(f *slack.File) *Image {
	if strings.EqualFold(f.Mimetype, "image/gif") && f.URLPrivate != "" && (f.Size == 0 || f.Size <= 8<<20) {
		return image(f.URLPrivate, f.Name, f.OriginalW, f.OriginalH)
	}
	type cand struct {
		u    string
		w, h int
		side int
	}
	cs := []cand{
		{f.Thumb1024, f.Thumb1024W, f.Thumb1024H, 1024},
		{f.Thumb960, f.Thumb960W, f.Thumb960H, 960},
		{f.Thumb720, f.Thumb720W, f.Thumb720H, 720},
		{f.Thumb480, f.Thumb480W, f.Thumb480H, 480},
		{f.Thumb360, f.Thumb360W, f.Thumb360H, 360},
		{f.URLPrivate, f.OriginalW, f.OriginalH, 0},
	}
	for _, c := range cs {
		if c.u == "" {
			continue
		}
		w, h := c.w, c.h
		if w == 0 || h == 0 {
			w, h = fitSide(f.OriginalW, f.OriginalH, c.side)
		}
		return image(c.u, f.Name, w, h)
	}
	return nil
}

// largestThumb returns any thumbnail (pdfs, docs) or nil.
func largestThumb(f *slack.File) *Image {
	for _, u := range []string{f.Thumb720, f.Thumb480, f.Thumb360, f.Thumb960, f.Thumb1024} {
		if u != "" {
			if u == f.Thumb720 && f.Thumb720W > 0 {
				return image(u, f.Name, f.Thumb720W, f.Thumb720H)
			}
			if u == f.Thumb360 && f.Thumb360W > 0 {
				return image(u, f.Name, f.Thumb360W, f.Thumb360H)
			}
			return image(u, f.Name, 0, 0)
		}
	}
	if f.ThumbPDF != "" { // first page of a PDF
		return image(f.ThumbPDF, f.Name, f.ThumbPDFW, f.ThumbPDFH)
	}
	return nil
}

// firstPos returns the first positive value (0 if none).
func firstPos(v ...int) int {
	for _, x := range v {
		if x > 0 {
			return x
		}
	}
	return 0
}

// fitSide scales w×h so the longer side is at most side (0: unchanged).
func fitSide(w, h, side int) (int, int) {
	if w <= 0 || h <= 0 || side <= 0 || (w <= side && h <= side) {
		return w, h
	}
	if w >= h {
		return side, max(1, h*side/w)
	}
	return max(1, w*side/h), side
}

var legacyColors = map[string]string{"good": "#2EB886", "warning": "#DAA038", "danger": "#A30200"}

func attachmentColor(c string) string {
	if v, ok := legacyColors[c]; ok {
		return v
	}
	h := strings.TrimPrefix(c, "#")
	if (len(h) == 3 || len(h) == 6) && hexColorRe.MatchString(h) {
		return "#" + strings.ToUpper(h)
	}
	return ""
}

// attachment converts a legacy attachment, link/app unfurl or shared message.
func (c *builder) attachment(a *slack.Attachment) (Block, bool) {
	att := &Attachment{
		Color: attachmentColor(a.Color), TS: tsSeconds(a.TS),
		AuthorName: a.AuthorName, AuthorSubname: a.AuthorSubname,
		AuthorIcon: SafeImage(a.AuthorIcon), AuthorLink: SafeURL(a.AuthorLink),
		FromURL: SafeURL(firstNonEmpty(a.FromURL, a.OriginalURL)),
	}
	blk := Block{Kind: AttachmentBlock, Att: att}
	if a.IsMsgUnfurl {
		c.msgCard(a, att)
	} else if a.IsShare {
		att.IsShare = true
		if att.AuthorName == "" {
			att.AuthorName, att.AuthorSubname = a.AuthorSubname, ""
		}
		att.ShareChannelID = a.ChannelID
		att.ShareChannel = strings.TrimPrefix(a.ChannelName, "#")
		if att.ShareChannel == "" && a.ChannelID != "" {
			att.ShareChannel = strings.TrimPrefix(c.r.ChannelName(a.ChannelID), "#")
		}
		for _, mb := range a.MessageBlocks {
			att.Text = append(att.Text, c.blocks(parseBlocks(mb.Message.Blocks))...)
		}
		if len(att.Text) == 0 {
			att.Text = FromMrkdwn(c.r, a.Text)
		}
		att.Footer = flatten(FromMrkdwn(c.r, a.Footer))
		att.FooterIcon = SafeImage(a.FooterIcon)
	} else {
		att.IsUnfurl = a.ServiceName != "" || a.FromURL != "" || a.OriginalURL != ""
		att.ServiceName = a.ServiceName
		att.ServiceIcon = SafeImage(a.ServiceIcon)
		att.Pretext = flatten(FromMrkdwn(c.r, a.Pretext))
		if a.Title != "" {
			att.Title = emojify(c.r, unescape(a.Title), 0)
		}
		att.TitleLink = SafeURL(a.TitleLink)
		att.Text = c.attachmentText(a)
		att.Text = append(att.Text, c.blocks(parseBlocks(a.Blocks))...)
		for _, f := range a.Fields {
			var ss []Span
			if f.Title != "" {
				ss = append(ss, Span{Kind: Text, Text: unescape(f.Title), Style: Bold})
			}
			if v := flatten(FromMrkdwn(c.r, f.Value)); len(v) > 0 {
				if len(ss) > 0 {
					ss = append(ss, Span{Kind: Break})
				}
				ss = append(ss, v...)
			}
			blk.Fields = append(blk.Fields, ss)
			blk.FieldShort = append(blk.FieldShort, f.Short)
		}
		att.Image = image(a.ImageURL, "", a.ImageWidth, a.ImageHeight)
		if att.Image == nil {
			att.Thumb = image(a.ThumbURL, "", a.ThumbWidth, a.ThumbHeight)
		}
		att.Footer = flatten(FromMrkdwn(c.r, a.Footer))
		att.FooterIcon = SafeImage(a.FooterIcon)
		blk.Buttons = legacyActions(a.Actions)
		if att.Title == nil && att.Text == nil && att.Pretext == nil && blk.Fields == nil && att.Image == nil &&
			att.Thumb == nil && att.AuthorName == "" && att.ServiceName == "" && blk.Buttons == nil && len(a.Files) == 0 {
			if a.Fallback == "" {
				return Block{}, false
			}
			att.Text = FromMrkdwn(c.r, a.Fallback)
		}
	}
	for i := range a.Files {
		blk.Children = append(blk.Children, c.fileBlock(&a.Files[i]))
	}
	return blk, true
}

// attachmentText is mrkdwn unless mrkdwn_in is present and omits "text".
func (c *builder) attachmentText(a *slack.Attachment) []Block {
	if a.Text == "" {
		return nil
	}
	if a.MrkdwnIn != nil {
		in := false
		for _, f := range a.MrkdwnIn {
			if f == "text" {
				in = true
			}
		}
		if !in {
			var out []Block
			for _, para := range strings.Split(a.Text, "\n\n") {
				out = append(out, Block{Kind: Paragraph, Spans: c.plainLinked(para)})
			}
			return out
		}
	}
	return FromMrkdwn(c.r, a.Text)
}

// plainLinked expands <tokens> and emoji but no formatting markers.
func (c *builder) plainLinked(s string) []Span {
	var out []Span
	for i, ln := range strings.Split(s, "\n") {
		if i > 0 {
			out = append(out, Span{Kind: Break})
		}
		for _, sp := range literal(c.r, ln, 0) {
			if sp.Kind == Text {
				out = append(out, emojify(c.r, sp.Text, sp.Style)...)
			} else {
				out = append(out, sp)
			}
		}
	}
	return out
}

func legacyActions(raw json.RawMessage) []Button {
	if len(raw) <= 2 {
		return nil
	}
	var as []struct {
		ID    json.RawMessage `json:"id"`
		Name  string          `json:"name"`
		Text  string          `json:"text"`
		Type  string          `json:"type"`
		URL   string          `json:"url"`
		Style string          `json:"style"`
		Value string          `json:"value"`
	}
	if json.Unmarshal(raw, &as) != nil {
		return nil
	}
	var out []Button
	for _, a := range as {
		b := Button{Kind: "button", Text: unescape(a.Text), URL: SafeURL(a.URL), ActionID: firstNonEmpty(a.Name, strings.Trim(string(a.ID), `"`)), Value: a.Value}
		if a.Type == "select" {
			b.Kind = "select"
		}
		if a.Style == "primary" || a.Style == "danger" {
			b.Style = a.Style
		}
		out = append(out, b)
	}
	return out
}

// huddleBlock renders the card for a huddle_thread message.
func huddleBlock(r Resolver, raw json.RawMessage) (Block, bool) {
	var room struct {
		Name               string   `json:"name"`
		DateStart          int64    `json:"date_start"`
		DateEnd            int64    `json:"date_end"`
		HasEnded           bool     `json:"has_ended"`
		Participants       []string `json:"participants"`
		ParticipantHistory []string `json:"participant_history"`
	}
	if len(raw) <= 2 || json.Unmarshal(raw, &room) != nil {
		return Block{}, false
	}
	a := &Attachment{ServiceName: "Huddle", TS: room.DateStart}
	people := room.Participants
	title := "Huddle in progress"
	if room.HasEnded || room.DateEnd > 0 {
		title = "A huddle happened"
		people = firstNonEmptySlice(room.ParticipantHistory, room.Participants)
	}
	if room.Name != "" {
		title = room.Name
	}
	a.Title = []Span{{Kind: Text, Text: title}}
	var info string
	if room.DateEnd > room.DateStart && room.DateStart > 0 {
		info = "Lasted " + duration(room.DateEnd-room.DateStart)
	}
	if n := len(people); n > 0 {
		if info != "" {
			info += " · "
		}
		info += strconv.Itoa(n) + " participant"
		if n != 1 {
			info += "s"
		}
	}
	if info != "" {
		a.Text = []Block{{Kind: Paragraph, Spans: []Span{{Kind: Text, Text: info}}}}
	}
	for i, id := range people {
		if i > 0 {
			a.Footer = append(a.Footer, Span{Kind: Text, Text: ", "})
		}
		a.Footer = append(a.Footer, userSpan(r, id, ""))
	}
	return Block{Kind: AttachmentBlock, Att: a}, true
}

func firstNonEmptySlice(a, b []string) []string {
	if len(a) > 0 {
		return a
	}
	return b
}

func duration(sec int64) string {
	switch {
	case sec < 60:
		return strconv.FormatInt(sec, 10) + "s"
	case sec < 3600:
		return strconv.FormatInt(sec/60, 10) + "m"
	}
	s := strconv.FormatInt(sec/3600, 10) + "h"
	if m := sec % 3600 / 60; m > 0 {
		s += " " + strconv.FormatInt(m, 10) + "m"
	}
	return s
}
