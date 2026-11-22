package richtext

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/localimg"
	"github.com/some-du6e/clack/internal/slack"
)

// FromMessage builds the document for a message: its rich blocks (or the
// mrkdwn text when there are none), then huddle cards, files and
// attachments/unfurls, in Slack's order. Deleted messages become a single
// Tombstone block.
func FromMessage(r Resolver, m *slack.Message) Doc {
	if m == nil {
		return Doc{}
	}
	return FromMessageIn(r, m, m.Channel)
}

// newBuilder is a builder for message m shown in channel ch: its sender,
// its canvas unfurls' hints and its files.
func newBuilder(r Resolver, m *slack.Message, ch string) *builder {
	c := &builder{r: r, sender: m.User, chID: ch, msgCh: firstNonEmpty(ch, m.Channel), msgTS: m.TS}
	c.hintsFrom(m)
	if len(m.Files) > 0 {
		c.files = make(map[string]*slack.File, len(m.Files))
		c.used = map[string]bool{}
		for i := range m.Files {
			c.files[m.Files[i].ID] = &m.Files[i]
		}
	}
	return c
}

// hasMessageBlocks is shouldRenderBlocksInsteadOfText's test (client-boot
// iub5 hasMessageBlocks: a non-empty blocks array). Slack then draws the
// blocks instead of the text even when every block is null or a rich_text
// without elements: Blocks (lby4) draws nothing for those, so the body is
// empty.
func hasMessageBlocks(raw json.RawMessage) bool {
	var bs []json.RawMessage
	return json.Unmarshal(raw, &bs) == nil && len(bs) > 0
}

// rendersBlocks is shouldRenderBlocksInsteadOfText (client-boot iub5)
// without its experiment-gated cases (a retention message under
// system_messages_redesign_m1, the meeting-channel and system-metadata
// redesigns). It is false for the messages MessageText leaves to another
// renderer and draws the text of: a meeting's artifacts DM, a canvas share
// that carries files, a channel tab or folder message, an automated
// subtype (h3YX isAutomatedSubtype), a huddle (a room, with no subtype, a
// broadcast's or a huddle's) or its notes, a scheduled or canceled
// huddle, a list record's or a surface card's comment root, a calendar
// event and a meeting reminder (a meeting_join_ accessory); anything else
// draws its blocks when it has any (hasMessageBlocks). FromMessageIn lets
// the text stand in for blocks that draw nothing by it.
func rendersBlocks(m *slack.Message) bool {
	if metaEventType(m.Metadata) == "slack_system.meeting.artifacts_ready" {
		return false
	}
	if m.Subtype == "canvas_sharing_message" {
		return m.Files == nil
	}
	switch m.Subtype {
	case "channel_tab_added", "channel_tab_added_unread_ignore", "folder_item_added", "folder_item_added_unread_ignore",
		"folders_organized", "folders_organized_unread_ignore",
		"huddle_notes_started", "scheduled_huddle_message", "scheduled_huddle_canceled", "surface_card_comment":
		return false
	case "list_record_comment":
		if m.SlackList != nil {
			return false
		}
	}
	if automatedSubtypes[m.Subtype] {
		return false
	}
	if len(m.Room) > 2 && (m.Subtype == "" || m.Subtype == "thread_broadcast" || m.Subtype == "huddle_message" || m.Subtype == "huddle_thread") {
		return false
	}
	if hasCalendarEvent(m.RawAttachments) || meetingReminder(m.Blocks) {
		return false
	}
	return hasMessageBlocks(m.Blocks)
}

// automatedSubtypes is h3YX's list (isAutomatedSubtype).
var automatedSubtypes = map[string]bool{}

func init() {
	for _, s := range strings.Fields(`agent_view_added code_channel_review_submitted code_channel_lifecycle channel_join
		channel_join_request channel_leave channel_topic channel_purpose channel_archive channel_unarchive
		channel_convert_to_private channel_convert_to_public channel_convert_to_linked_private
		channel_convert_to_linked_private_read channel_convert_to_linked_private_edit
		channel_convert_to_linked_private_transfer channel_convert_shared group_join group_leave group_topic
		group_purpose group_archive group_unarchive group_name channel_name play_sound pinned_item unpinned_item
		sh_room_shared sh_room_created bot_enable bot_disable bot_add bot_remove bot_updated reminder_add
		reminder_delete reply_broadcast app_conversation_join app_conversation_leave app_conversation_created
		channel_posting_permissions retention_threshold joiner_notification joiner_notification_for_inviter
		prejoin_dm_welcome_party_notification slack_connect_dm_acceptance canvas mpdm_move canvas_open_by_default
		canvas_editing_permissions_changed channel_canvas_updated channel_canvas_updated_unread_ignore
		tabbed_canvas_updated tabbed_canvas_updated_unread_ignore automatic_ai_huddle_notes_enabled
		automatic_ai_huddle_notes_disabled workflow_channel_opt_in_enabled workflow_channel_opt_in_disabled
		welcome_party`) {
		automatedSubtypes[s] = true
	}
}

// metaEventType is a message's metadata.event_type ("" without one).
func metaEventType(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var md struct {
		EventType string `json:"event_type"`
	}
	_ = json.Unmarshal(raw, &md)
	return md.EventType
}

// hasCalendarEvent is isCalendarEventMessage: an attachment with a
// calendar_event.
func hasCalendarEvent(raw json.RawMessage) bool {
	if !strings.Contains(string(raw), `"calendar_event"`) {
		return false
	}
	var as []struct {
		CalendarEvent json.RawMessage `json:"calendar_event"`
	}
	if json.Unmarshal(raw, &as) != nil {
		return false
	}
	for _, a := range as {
		switch string(a.CalendarEvent) { // JS truthiness
		case "", "null", "false", `""`, "0":
		default:
			return true
		}
	}
	return false
}

// meetingReminder is isMeetingReminderMsg (kbmb): a block whose
// accessory's action_id starts with "meeting_join_".
func meetingReminder(raw json.RawMessage) bool {
	if !strings.Contains(string(raw), "meeting_join_") {
		return false
	}
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		return false
	}
	for _, p := range parts { // each block on its own: one odd accessory doesn't hide the rest
		var b struct {
			Accessory struct {
				ActionID json.RawMessage `json:"action_id"`
			} `json:"accessory"`
		}
		if json.Unmarshal(p, &b) != nil {
			continue
		}
		var id string
		if json.Unmarshal(b.Accessory.ActionID, &id) == nil && strings.HasPrefix(id, "meeting_join_") {
			return true
		}
	}
	return false
}

// blocksDrawNothing is Blocks' (lby4) check before it renders anything:
// every block null or a rich_text without elements. bs is raw parsed; a
// block without a type ({}) isn't null, so the raw JSON decides those.
func blocksDrawNothing(raw json.RawMessage, bs []bk) bool {
	for i := range bs {
		if b := &bs[i]; b.Type != "" && (b.Type != "rich_text" || len(b.Elements) > 0) {
			return false
		}
	}
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		return false
	}
	for _, p := range parts {
		if string(p) == "null" {
			continue
		}
		var rt struct {
			Type     string            `json:"type"`
			Elements []json.RawMessage `json:"elements"`
		}
		if json.Unmarshal(p, &rt) != nil || rt.Type != "rich_text" || len(rt.Elements) > 0 {
			return false
		}
	}
	return true
}

// FromMessageIn is FromMessage for a message shown in channel ch (Slack's
// MessageContext channelId: a channel-tab slug to ch is the tab's label
// alone).
func FromMessageIn(r Resolver, m *slack.Message, ch string) Doc {
	if m == nil {
		return Doc{}
	}
	if m.Subtype == "message_deleted" || m.Subtype == "tombstone" {
		return Doc{Blocks: []Block{tombstone("This message was deleted.")}}
	}
	c := newBuilder(r, m, ch)
	var d Doc
	blocks := c.expanded(m.Blocks)
	if bs := parseBlocks(blocks); len(bs) > 0 && !blocksDrawNothing(blocks, bs) {
		d.Blocks = c.blocks(bs)
		d.Jumbo = richOnlyEmoji(bs)
	}
	// Slack draws the blocks instead of the text whenever the message has
	// any (shouldRenderBlocksInsteadOfText), even if they draw nothing (a
	// condition with no case for the desktop, an empty fallback list),
	// except for the messages another renderer draws (rendersBlocks: the
	// automated subtypes' MessageAutomatedSubtype, which shows
	// agent_view_added's blocks through GenericSystemMessage, huddles,
	// calendar events…): for those the text stands in when the blocks draw
	// nothing. It does too when a block Clack can't draw yet drew nothing.
	if len(d.Blocks) == 0 && m.Text != "" && (!rendersBlocks(m) || c.missing) {
		d.Blocks = FromMrkdwn(r, m.Text)
		d.Jumbo = onlyEmoji(r, m.Text)
	}
	if m.Subtype == "huddle_thread" || len(m.Room) > 2 {
		if b, ok := huddleBlock(r, m.Room, m.Channel); ok {
			if m.Subtype == "huddle_thread" {
				// TestableAutomatedSubtype: the body is HuddleMsgText, not the text
				d.Blocks, d.Jumbo = nil, false
			}
			d.Blocks = append(d.Blocks, b)
		}
	}
	status := m.UploadStatus
	for i := range m.Files {
		if !c.used[m.Files[i].ID] {
			if status != "" { // PendingFilesContent: the meta line, then the gallery
				d.Blocks = append(d.Blocks, Block{Kind: FileStatus, Spans: []Span{{Kind: Text, Text: status}}})
				status = ""
			}
			d.Blocks = append(d.Blocks, c.fileBlock(&m.Files[i]))
		}
	}
	var agentChannels []Block
	for i := range m.Attachments {
		a := &m.Attachments[i]
		if u := a.AgentChannelUnfurl; u != nil && u.ChannelID != "" {
			// MessageKitAttachments: an agent_channel_unfurl is never a
			// legacy attachment; it is a CodeChannelEntityCard (surface
			// MessageUnfurl), after the message's other attachments. The
			// element host draws it (Buttons[0]: kind agent_channel_unfurl,
			// the channel id); Spans is the fallback without one.
			agentChannels = append(agentChannels, Block{Kind: Agent,
				Buttons: []Button{{Kind: AgentChannelUnfurlKind, BlockID: u.ChannelID}},
				Spans:   plainSpans(c.r, firstNonEmpty(a.Fallback, u.ChannelName))})
			continue
		}
		if b, ok := c.attachment(a); ok {
			if at := b.Att; at != nil {
				if a.ID != 0 {
					at.ID = strconv.Itoa(a.ID)
				}
				at.Index = i
				at.BotID = firstNonEmpty(a.BotID, m.BotID)
				at.BotName = botName(r, m, at.BotID)
				at.LinkedInText = a.FromURL != "" && linksURL(m, a.FromURL)
			}
			d.Blocks = append(d.Blocks, b)
		}
	}
	d.Blocks = append(d.Blocks, agentChannels...)
	if e, ok := r.(emojiTexter); ok && e.EmojiAsText() { // emoji_mode as_text (highlight.go)
		EmojiToText(d.Blocks)
		d.Jumbo = false
	}
	if h, ok := r.(highlighter); ok { // the viewer's Channel keywords (highlight.go)
		HighlightDoc(d.Blocks, h.HighlightWords())
	}
	return d
}

// botName is getBotById's name for id: the resolver's bots cache, else
// the message's bot_profile when it is the same bot ("" unknown).
func botName(r Resolver, m *slack.Message, id string) string {
	if id == "" {
		return ""
	}
	if bn, ok := r.(BotNamer); ok {
		if n := bn.BotName(id); n != "" {
			return n
		}
	}
	if bp := m.BotProfile; bp != nil && bp.ID == id {
		return bp.Name
	}
	return ""
}

// linksURL reports whether m's text or rich text links u (Slack's
// shouldShow, djIX: msg.text includes from_url with & as &amp;, or the
// blocks' link entities match it).
func linksURL(m *slack.Message, u string) bool {
	if m.Text != "" && strings.Contains(m.Text, strings.ReplaceAll(u, "&", "&amp;")) {
		return true
	}
	if len(m.Blocks) == 0 {
		return false
	}
	return blocksLink(parseBlocks(m.Blocks), u)
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
	out := convertFile(f)
	c.canvasFile(f, out) // canvasfile.go
	c.expandable(out)
	if out.Email != nil {
		c.emailNames(out.Email)
	}
	if f.User != "" && c.sender != "" && f.User != c.sender {
		out.UploaderID = f.User
		if mn, ok := c.r.(MemberNamer); ok {
			out.Uploader, _ = mn.MemberName(f.User)
		} else {
			out.Uploader = strings.TrimPrefix(c.r.UserName(f.User), "@")
		}
	}
	return Block{Kind: FileBlock, File: out}
}

// transcriptStatus is file.transcription.status.
func transcriptStatus(raw json.RawMessage) string {
	var t struct {
		Status string `json:"status"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &t) != nil {
		return ""
	}
	return t.Status
}

func convertFile(f *slack.File) *File {
	out := &File{
		ID: f.ID, Name: f.Name, Title: f.Title, Mime: f.Mimetype, Type: f.Filetype, PrettyType: f.PrettyType,
		Size: f.Size, URL: SafeURL(f.URLPrivate), Download: SafeURL(f.URLPrivateDownload), Permalink: SafeURL(f.Permalink),
		Lines: f.Lines, DurationMS: f.DurationMS, OrigW: f.OriginalW, OrigH: f.OriginalH,
		Subtype: f.Subtype, Waveform: f.AudioWaveSamples, User: f.User, Created: f.Created, IsExternal: f.IsExternal,
		Mode: f.Mode,
	}
	if out.Title == out.Name {
		out.Title = ""
	}
	if f.Pending {
		out.Pending, out.Percent = true, f.UploadPercent
		if f.Mimetype != "" && strings.HasPrefix(strings.ToLower(f.Mimetype), "image/") && localimg.Is(f.Thumb360) {
			// PendingFilesGalleryGridImage: the File's own bytes
			out.IsImage = true
			// sized like the largest thumb Slack will make (thumbs go up
			// to the original, thumb_1024 at most): the same gallery box
			// before and after the swap
			w, h := fitSide(f.OriginalW, f.OriginalH, 1024)
			out.Thumb = &Image{URL: f.Thumb360, Alt: f.Name, W: w, H: h}
			return out
		}
	}
	mime := strings.ToLower(f.Mimetype)
	switch {
	case strings.HasPrefix(mime, "image/"):
		if !slack.FileIsImage(f) {
			break // getFileType "generic": a file card, no thumb (FileIsImage)
		}
		out.IsImage = true
		out.Thumb = imageThumb(f)
		if st := stillThumb(f); st != nil && (out.Thumb == nil || st.URL != out.Thumb.URL) {
			out.Still = st
		}
		out.Deanimate = f.Deanimate != ""
	case slack.FileIsPlayableAudio(f):
		// fileIsPlayableAudio: FileAudio's AudioPlayer, playing
		// getMediaURL's aac || url_private; any other audio/* is a plain
		// file card (an ogg upload) or a video (a webm one)
		out.IsAudio = true
		out.MediaURL = SafeURL(f.AAC)       // URL stays the upload's url_private
		if p := f.MediaProgress; p != nil { // use_media_elapsed
			out.MediaOffsetMS, out.MediaWatched = p.OffsetMS, p.MediaWatched
		}
		out.Transcript, out.TranscriptRegion, out.VTT = transcriptStatus(f.Transcription), f.IsTranscriptionRegionSupported, SafeURL(f.VTT)
	case slack.FileIsPlayableVideo(f):
		// fileIsPlayableVideo; any other video/* is a plain file card
		out.IsVideo = true
		out.URL = SafeURL(firstNonEmpty(f.MP4, f.URLPrivate))
		// FileGalleryVideoFile: original_w || thumb_video_w for the box,
		// thumb_video_w || original_w for the poster's own size; the poster
		// is thumb_video_ts ?? thumb_video (an https URL only)
		out.Thumb = image(videoPoster(f), f.Name, firstPos(f.ThumbVideoW, f.OriginalW), firstPos(f.ThumbVideoH, f.OriginalH))
		out.OrigW, out.OrigH = firstPos(f.OriginalW, f.ThumbVideoW), firstPos(f.OriginalH, f.ThumbVideoH)
		if p := f.MediaProgress; p != nil {
			out.MediaOffsetMS, out.MediaWatched = p.OffsetMS, p.MediaWatched
		}
		if f.IsSalesforceFile {
			out.Salesforce = true
		}
	default:
		out.Thumb = largestThumb(f)
	}
	// FileSnippet draws a file whose mode is "snippet" (getFileType, e4n/:
	// images, audio, video, posts, lists and canvases come first); an
	// uploaded text or code file (mode "hosted") is a file pillow
	if f.Mode == "snippet" && !out.IsImage && !out.IsVideo && !out.IsAudio && f.Filetype != "quip" && f.Filetype != "list" {
		out.IsSnippet = true
		out.Preview = f.Preview
		out.Highlight = ParseCodeMirror(f.PreviewHighlight)
		out.LinesMore, out.PreviewTruncated = f.LinesMore, f.PreviewIsTruncated
	}
	out.EditLink, out.ExternalType = SafeURL(f.EditLink), f.ExternalType
	if f.Mode == "email" && !out.IsImage && !out.IsVideo && !out.IsAudio {
		out.Email = convertEmail(f)
	}
	return out
}

// EmailFile is f, an email file from files.info, as a message's file
// block has it, its senders and recipients named through r (the email's
// FileDetails view, which reads the whole file).
func EmailFile(f *slack.File, r Resolver) *File {
	out := convertFile(f)
	if out.Email != nil && r != nil {
		(&builder{r: r}).emailNames(out.Email)
	}
	return out
}

// emailNames names an email's senders and recipients as EmailRecipient
// shows them: a member by their name (EmailUserEntity's token), a channel
// id with no address (the email went to a channel's address) by the
// channel's name (looksLikeChannelId, nQiy).
func (c *builder) emailNames(e *Email) {
	for _, list := range [][]EmailAddress{e.From, e.To, e.Cc} {
		for i := range list {
			a := &list[i]
			switch {
			case a.UserID != "":
				if mn, ok := c.r.(MemberNamer); ok {
					if n, ok := mn.MemberName(a.UserID); ok && n != "" {
						a.Name = n
					}
				}
			case a.Address == "" && looksLikeChannelID(a.Name):
				if n := c.r.ChannelName(a.Name); n != "" && n != a.Name {
					a.Name = n
				}
			}
		}
	}
}

// looksLikeChannelID is looksLikeChannelId (nQiy): /^(C|G|D|Z)[A-Z0-9]{8,}$/.
func looksLikeChannelID(s string) bool {
	if len(s) < 9 || !strings.ContainsRune("CGDZ", rune(s[0])) {
		return false
	}
	for _, r := range s[1:] {
		if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// convertEmail is an email file's message (getFileType "email": mode
// "email", after images, audio and video).
func convertEmail(f *slack.File) *Email {
	addrs := func(in []slack.EmailAddress) []EmailAddress {
		var out []EmailAddress
		for _, a := range in {
			out = append(out, EmailAddress{Name: a.Name, Address: a.Address, UserID: a.SlackUserID})
		}
		return out
	}
	e := &Email{From: addrs(f.From), To: addrs(f.To), Cc: addrs(f.Cc), Subject: f.Subject,
		HTML: firstNonEmpty(f.SimplifiedHTML, f.Preview), PlainText: f.PlainText, Preview: f.PreviewPlainText,
		Timestamp: f.Timestamp, AppID: f.AppID, AppName: f.AppName}
	if n := len(f.EmailAttachments) + f.InlineAttachmentCount; f.OriginalAttachmentCount > 0 && f.OriginalAttachmentCount != n {
		e.Removed = f.OriginalAttachmentCount - n // renderAttachments' "{count} attachments removed"
	}
	if s := f.EmailSecurity; s != nil && s.HasValidDMARC != nil && !*s.HasValidDMARC {
		e.Unverified = true
	}
	for _, a := range f.EmailAttachments {
		e.Attachments = append(e.Attachments, EmailAttachment{Name: a.Filename, Mime: a.Mimetype, URL: SafeURL(a.URL), FileID: a.SlackFileID, Size: a.Size})
	}
	return e
}

// FileExpander is an optional Resolver extension (store.Store): a message
// file's expandable (Slack's expandedFiles, by file, channel and
// message: fetchAndExpand going, or done) and a snippet's highlighted
// content once files.info brought it (cut for display: truncated).
type FileExpander interface {
	FileExpansion(ch, ts, fileID string) (expanded, expanding bool)
	SnippetContent(fileID string) (code *CodeMirror, truncated, ok bool)
}

// expandable fills a snippet's expandable state and fetched content.
func (c *builder) expandable(out *File) {
	fe, ok := c.r.(FileExpander)
	if !ok || !out.IsSnippet && out.Email == nil || out.ID == "" {
		return
	}
	out.Expanded, out.Expanding = fe.FileExpansion(c.msgCh, c.msgTS, out.ID)
	if code, cut, ok := fe.SnippetContent(out.ID); ok {
		out.Content, out.ContentTruncated = code, cut
	}
}

// imageThumb picks the thumbnail Slack shows inline (thumb_1024 first, then
// the next largest) with its pixel size: Slack sizes inline images from the
// 1024 thumb's dimensions, up to the gallery width. An animated GIF plays
// its animated thumb (FileImageThumbnail's getThumbSrcForFile at maxSize
// 720, 2HRR: thumb_480_gif, then thumb_360_gif); one without either draws
// its static thumbs, as in Slack. Small images have no thumb past their
// own size (a 223px screenshot stops at thumb_360, which is the
// original's size). Only files slack.FileIsImage takes get here: thumb_360 is
// there, or a HEIC's thumb_800; the original (url_private) and the small
// thumbs are left for a Salesforce file.
func imageThumb(f *slack.File) *Image {
	for _, g := range []struct {
		u    string
		w, h int
		side int
	}{{f.Thumb480GIF, f.Thumb480W, f.Thumb480H, 480}, {f.Thumb360GIF, f.Thumb360W, f.Thumb360H, 360}} {
		if g.u != "" {
			w, h := g.w, g.h
			if w == 0 || h == 0 {
				w, h = fitSide(f.OriginalW, f.OriginalH, g.side)
			}
			return image(g.u, f.Name, w, h)
		}
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
		{f.Thumb800, 0, 0, 800},
		{f.URLPrivate, f.OriginalW, f.OriginalH, 0},
		{f.Thumb160, 0, 0, 160},
		{f.Thumb80, 0, 0, 80},
		{f.Thumb64, 0, 0, 64},
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

// stillThumb is what Slack draws for f with Accessibility > "Allow
// animations" off (getThumbSrcForFile with suppressAnimations, 2HRR), for
// the GIFs, PNGs and WebPs it applies to: the deanimated copy (at the
// original's size), else a static thumb, thumb_360 only for files made
// after Slack's thumb_360 stopped animating (mayThumb360BeAnimated:
// created before 2022-06-04). nil: none, or not one of those types.
func stillThumb(f *slack.File) *Image {
	switch strings.ToLower(f.Filetype) {
	case "gif", "png", "webp":
	default:
		return nil
	}
	if f.Deanimate != "" {
		return image(f.Deanimate, f.Name, f.OriginalW, f.OriginalH)
	}
	if f.DeanimateGIF != "" {
		return image(f.DeanimateGIF, f.Name, f.OriginalW, f.OriginalH)
	}
	for _, c := range []struct {
		u    string
		w, h int
		side int
		ok   bool
	}{
		{f.Thumb1024, f.Thumb1024W, f.Thumb1024H, 1024, true},
		{f.Thumb960, f.Thumb960W, f.Thumb960H, 960, true},
		{f.Thumb720, f.Thumb720W, f.Thumb720H, 720, true},
		{f.Thumb480, f.Thumb480W, f.Thumb480H, 480, true},
		{f.Thumb360, f.Thumb360W, f.Thumb360H, 360, f.Created >= 1654300800},
		{f.Thumb160, 0, 0, 160, true},
	} {
		if c.u == "" || !c.ok {
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
	if IsWorkObject(a) {
		// MessageKitAttachments hands every attachment with a work
		// object to MessageAttachmentWorkObject, never to the unfurl
		// renderers (workobject.go)
		e := workObjectOf(a)
		if e == nil {
			return Block{}, false
		}
		return Block{Kind: AttachmentBlock, Att: &Attachment{WorkObject: c.workObject(a, e),
			FromURL: SafeURL(firstNonEmpty(a.FromURL, a.OriginalURL)), OriginalURL: SafeURL(a.OriginalURL)}}, true
	}
	att := &Attachment{
		Color: attachmentColor(a.Color), TS: tsSeconds(a.TS),
		AuthorName: a.AuthorName, AuthorSubname: a.AuthorSubname,
		AuthorIcon: SafeImage(a.AuthorIcon), AuthorLink: SafeURL(a.AuthorLink),
		FromURL: SafeURL(firstNonEmpty(a.FromURL, a.OriginalURL)),
	}
	att.Legacy = true
	blk := Block{Kind: AttachmentBlock, Att: att}
	if a.DocumentSection != nil && a.FileID != "" { // MessageAttachmentQuipAnchorLink
		c.canvasAnchor(a, att)
		return blk, true
	}
	if a.IsMsgUnfurl {
		c.msgCard(a, att)
	} else if a.IsShare {
		att.IsShare = true
		// the shared message's ts as Slack's API sends it, a string
		// ("1794369255.504199"); TS above is only its whole seconds
		if t := strings.TrimSpace(string(a.TS)); strings.HasPrefix(t, `"`) {
			att.MsgTS = strings.Trim(t, `"`)
		}
		if att.AuthorName == "" {
			att.AuthorName, att.AuthorSubname = a.AuthorSubname, ""
		}
		att.ShareChannelID = a.ChannelID
		att.ShareChannel = strings.TrimPrefix(a.ChannelName, "#")
		if att.ShareChannel == "" && a.ChannelID != "" {
			att.ShareChannel = strings.TrimPrefix(c.r.ChannelName(a.ChannelID), "#")
		}
		for _, mb := range a.MessageBlocks {
			att.Text = append(att.Text, c.topBlocks(mb.Message.Blocks)...)
		}
		if len(att.Text) == 0 {
			att.Text = FromMrkdwn(c.r, a.Text)
		}
		att.Footer = flatten(FromMrkdwn(c.r, a.Footer))
		att.FooterIcon = SafeImage(a.FooterIcon)
	} else {
		att.IsUnfurl = a.ServiceName != "" || a.FromURL != "" || a.OriginalURL != ""
		att.OriginalURL = SafeURL(a.OriginalURL)
		att.AppUnfurlURL = SafeURL(a.AppUnfurlURL)
		att.ServiceURL = SafeURL(a.ServiceURL)
		att.VideoHTML, att.AudioHTML, att.OtherHTML = a.VideoHTML != "", a.AudioHTML != "", a.OtherHTML != ""
		att.AudioHTMLCode, att.AudioHTMLHeight = a.AudioHTML, a.AudioHTMLH
		att.ImageBytes, att.IsAppUnfurl, att.Hidden = a.ImageBytes, a.IsAppUnfurl, a.Hidden
		att.ServiceName = a.ServiceName
		att.ServiceIcon = SafeImage(a.ServiceIcon)
		att.Pretext = flatten(FromMrkdwn(c.r, a.Pretext))
		if a.Title != "" {
			// AttachmentTitle (boot-shared yvlf): Mrkdwn with noSpecials
			// (TSF's NOMRKDWN): its <links>, mentions and :emoji:, no
			// *bold* or `code`. A title that is one <url|label> token
			// (GitHub's "#4045 Update record for …") is that link.
			att.Title = c.plainLinked(a.Title)
		}
		att.TitleLink = SafeURL(a.TitleLink)
		att.Text = c.attachmentText(a)
		if kb := c.topBlocks(a.Blocks); len(kb) > 0 {
			att.Kit, att.KitFrom = true, len(att.Text)
			att.Text = append(att.Text, kb...)
		}
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
		if att.Image != nil {
			att.Image.Bytes = a.ImageBytes // obey_inline_img_limit (isImageTooLarge)
		}
		if att.Image == nil || att.VideoHTML {
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

// huddleBlock is a huddle_thread message's HuddleMsgText (huddle.go):
// the automated sentence, with the room for the Join Huddle button.
func huddleBlock(r Resolver, raw json.RawMessage, channel string) (Block, bool) {
	h, ok := ParseHuddleRoom(raw)
	if !ok {
		return Block{}, false
	}
	if channel != "" {
		h.Channel = channel
	}
	var o HuddleTextOpts
	if hr, ok := r.(HuddleResolver); ok {
		o = hr.HuddleTextOpts(h.Channel)
	}
	h.Joinable = h.Live && !in(h.Active, r.SelfID()) // shouldAddJoinButton
	if ar, ok := r.(AvatarResolver); ok && h.Joinable {
		for _, u := range h.Active {
			if len(h.Faces) == 3 {
				break
			}
			if url := ar.UserAvatar(u); url != "" {
				h.Faces = append(h.Faces, url)
			}
		}
	}
	a := &Attachment{ServiceName: "Huddle", TS: h.DateStart, Huddle: h}
	if sp := HuddleMsgText(r, h, o, time.Now()); len(sp) > 0 {
		a.Text = []Block{{Kind: Paragraph, Spans: sp}}
	}
	return Block{Kind: AttachmentBlock, Att: a}, true
}

func firstNonEmptySlice(a, b []string) []string {
	if len(a) > 0 {
		return a
	}
	return b
}

// blocksLink reports whether a link element in bs (at any depth) points
// at u (8bIy entityTypes.links.matchUrl).
func blocksLink(bs []bk, u string) bool {
	for i := range bs {
		b := &bs[i]
		if b.Type == "link" && b.URL == u {
			return true
		}
		if blocksLink(b.Elements, u) {
			return true
		}
	}
	return false
}
