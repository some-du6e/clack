package slack

import "encoding/json"

// Message is a Slack message as returned by conversations.history and RTM.
// Unknown/rarely used fields are kept in raw form so renderers can reach them.
type Message struct {
	Type           string          `json:"type,omitempty"`
	Subtype        string          `json:"subtype,omitempty"`
	TS             string          `json:"ts"`
	User           string          `json:"user,omitempty"`
	BotID          string          `json:"bot_id,omitempty"`
	AppID          string          `json:"app_id,omitempty"` // the app a bot message came from (getBotByMsg passes it to bots.info)
	Username       string          `json:"username,omitempty"`
	Text           string          `json:"text"`
	ThreadTS       string          `json:"thread_ts,omitempty"`
	ParentUser     string          `json:"parent_user_id,omitempty"`
	ReplyCount     int             `json:"reply_count,omitempty"`
	ReplyUsers     []string        `json:"reply_users,omitempty"`
	ReplyUserIcons []ReplyUserIcon `json:"reply_user_icons,omitempty"` // a bot replier's identity in the reply bar
	ReplyUsersN    int             `json:"reply_users_count,omitempty"`
	LatestReply    string          `json:"latest_reply,omitempty"`
	Subscribed     bool            `json:"subscribed,omitempty"`
	LastRead       string          `json:"last_read,omitempty"` // a followed thread's root (conversations.replies): the viewer's last read reply
	// SubscribedSet: the copy had a "subscribed" field of its own (history
	// and replies roots do; the socket's message_changed doesn't), which
	// is what Slack's slurpThreadSubscriptions takes a thread's
	// subscription from (store/thread_subs.go). Decoding sets it.
	SubscribedSet  bool            `json:"-"`
	Edited         *Edited         `json:"edited,omitempty"`
	Reactions      []Reaction      `json:"reactions,omitempty"`
	Files          []File          `json:"files,omitempty"`
	Blocks         json.RawMessage `json:"blocks,omitempty"`
	Attachments    []Attachment    `json:"attachments,omitempty"`
	RawAttachments json.RawMessage `json:"-"`                  // attachments exactly as Slack sent them (memory only)
	Metadata       json.RawMessage `json:"metadata,omitempty"` // app metadata {event_type, event_payload}
	Icons          *Icons          `json:"icons,omitempty"`
	BotProfile     *BotProfile     `json:"bot_profile,omitempty"`
	PinnedTo       []string        `json:"pinned_to,omitempty"`
	PinnedInfo     *PinnedInfo     `json:"pinned_info,omitempty"`
	ClientMsgID    string          `json:"client_msg_id,omitempty"`
	Hidden         bool            `json:"hidden,omitempty"`
	// NoDisplay and NoNotifications hold the message's desktop
	// notification back (client-boot Fpyc's reasons 4 and 5).
	NoDisplay       bool            `json:"no_display,omitempty"`
	NoNotifications bool            `json:"no_notifications,omitempty"`
	IsEphemeral     bool            `json:"is_ephemeral,omitempty"` // Slack's addEphemeralMessage (local, never from history)
	Root            *Message        `json:"root,omitempty"`         // thread_broadcast
	ReplyBroadcast  bool            `json:"reply_broadcast,omitempty"`
	Upload          bool            `json:"upload,omitempty"`
	DisplayAsBot    bool            `json:"display_as_bot,omitempty"`
	Room            json.RawMessage `json:"room,omitempty"` // huddles
	Channel         string          `json:"channel,omitempty"`
	Inviter         string          `json:"inviter,omitempty"`
	// System messages' own fields: channel_topic's topic, channel_purpose's
	// purpose, channel_name's old_name and name (the new one).
	Topic          string `json:"topic,omitempty"`
	Purpose        string `json:"purpose,omitempty"`
	OldName        string `json:"old_name,omitempty"`
	NewName        string `json:"name,omitempty"`
	StreamingState string `json:"streaming_state,omitempty"` // chat.startStream: in_progress | completed | errored
	WorkflowID     string `json:"workflow_id,omitempty"`     // a workflow's message (the menu's Copy workflow ID)
	TriggerID      string `json:"trigger_id,omitempty"`      // the workflow trigger that posted it
	// AgentSession is a thread session's state on its root message
	// (Slack's iR00 getMessageByTimestamp(…)?.agent_session).
	AgentSession *MessageAgentSession `json:"agent_session,omitempty"`
	// AssistantAppThread marks an AI app's assistant thread root
	// (assistant_app_thread: {title, …}); the thread pane's header reads
	// its title (srGL renderThreadTitle, Aa1F AssistantThreadTitleLabel).
	AssistantAppThread *MessageAssistantThread `json:"assistant_app_thread,omitempty"`
	// AIContext is an agent's reply's ai_context: result_status
	// "stopped_by_user" when the person stopped its stream (Slack's
	// getMessageLabelValues isStopped, stopped_by_user_id).
	AIContext *MessageAIContext `json:"ai_context,omitempty"`
	// FunctionExecutionID marks a workflow step's message: its block
	// actions carry it (Slack's blocks.actions function_execution_id).
	FunctionExecutionID string `json:"function_execution_id,omitempty"`
	// IsLocked marks a thread root whose replies an admin turned off
	// (Slack's p6HR isLocked: `is_locked`); LockedByTeamID is the team
	// that locked it. RTM's message_locked updates both on the stored
	// root (core/thread_lock.go).
	IsLocked       bool   `json:"is_locked,omitempty"`
	LockedByTeamID string `json:"locked_by_team_id,omitempty"`
	// DocumentComment is a canvas comment thread's root (subtype
	// document_comment_root, in the canvas's file channel): Slack's
	// msg.document_comment (j6Z8 S: thread_id; REqS isArchivedDocumentComment,
	// getCanvasRootLastSeen, isDocumentCommentVisible; thrK most_recent_editor;
	// v+tj authors).
	DocumentComment *MessageDocumentComment `json:"document_comment,omitempty"`
	// What getMsgSenderEntity (hVKV) reads for a sender besides user: a
	// legacy file_comment's comment.user, a list_edit's
	// list_edit.created_by, a channel_tab_added's tab_added_user_id and a
	// folder_item_added's item_added_user_id.
	Comment         *MessageComment  `json:"comment,omitempty"`
	ListEdit        *MessageListEdit `json:"list_edit,omitempty"`
	TabAddedUserID  string           `json:"tab_added_user_id,omitempty"`
	ItemAddedUserID string           `json:"item_added_user_id,omitempty"`
	// DocumentMention is a canvas mention (subtype document_mention, in
	// the canvas's file channel): Slack's msg.document_mention {file_id,
	// section_id, mentioning_user_ids}, which its jump to the message
	// (lFpV openDocWithThreadTs) opens the canvas at, and Activity's
	// header counts ("You were mentioned {mentionCount} times in {space}").
	DocumentMention *MessageDocumentMention `json:"document_mention,omitempty"`
	// SlackList is a list record's comment thread's message: its list and
	// record (GMkG and wc0P read slack_list.list_record_id)
	SlackList *MessageSlackList `json:"slack_list,omitempty"`
	// UploadStatus is a placeholder's PendingFilesMetaTextWrapper text
	// ("Waiting to upload…", "Uploading…", "Sending…") while the viewer's
	// own files upload: Clack only, never on the wire.
	UploadStatus string `json:"-"`
}

// MessageDocumentComment is a canvas comment root's document_comment.
// ThreadID is the Quip comment thread (a "temp:C:…"-style id; Slack's
// thread ts for it is "Qpc:" + the id with "temp:" shortened to "t:").
// LastSeen is the viewer's last seen reply in microseconds.
type MessageDocumentComment struct {
	ThreadID         string      `json:"thread_id,omitempty"`
	Authors          []string    `json:"authors,omitempty"`
	IsArchived       bool        `json:"is_archived,omitempty"`
	IsVisible        bool        `json:"is_visible,omitempty"`
	LastSeen         json.Number `json:"last_seen,omitempty"`
	MostRecentEditor string      `json:"most_recent_editor,omitempty"`
}

// MessageSlackList is a list record's comment-thread message's
// slack_list: the list and the record whose thread it is.
type MessageSlackList struct {
	ListID       string `json:"list_id,omitempty"`
	ListRecordID string `json:"list_record_id,omitempty"`
}

// MessageDocumentMention is a canvas mention's document_mention: the
// canvas, the section the mention sits in, and who mentioned the viewer.
type MessageDocumentMention struct {
	FileID            string   `json:"file_id,omitempty"`
	SectionID         string   `json:"section_id,omitempty"`
	MentioningUserIDs []string `json:"mentioning_user_ids,omitempty"`
}

// MessageAssistantThread is a message's assistant_app_thread.
type MessageAssistantThread struct {
	Title string `json:"title,omitempty"`
}

// TitleSet is agent_session.title when it is a string: Slack's
// `agent_session?.title ?? …` falls through only when it's missing.
func (s *MessageAgentSession) TitleSet() (string, bool) {
	var t string
	if s == nil || json.Unmarshal(s.RawTitle, &t) != nil {
		return "", false
	}
	return t, true
}

// MessageAgentSession is a message's agent_session: the status (active,
// processing, suspended, closed) and when processing lapses
// (date_status_processing_expire, unix seconds; iR00
// readProcessingExpireSeconds).
type MessageAgentSession struct {
	Status           string      `json:"status,omitempty"`
	ProcessingExpire json.Number `json:"date_status_processing_expire,omitempty"`
	// The Agents & tools peek's fields (RkDi getAgentSessionTitle, XsVf
	// AgentThreadPeekItem, iR00 getAgentSessionIdentityOverrideFromSession),
	// kept raw so a shape Clack doesn't expect can't fail the message's
	// decoding: read them with SessionTitle, BotUserIDs and Statuses.
	RawTitle         json.RawMessage `json:"title,omitempty"`
	RawAgentBotUsers json.RawMessage `json:"agent_bot_user_ids,omitempty"`
	RawAgentStatuses json.RawMessage `json:"agent_statuses,omitempty"`
}

// SessionTitle is agent_session.title ("" when absent or not a string).
func (s *MessageAgentSession) SessionTitle() string {
	var t string
	if s == nil || json.Unmarshal(s.RawTitle, &t) != nil {
		return ""
	}
	return t
}

// BotUserIDs is agent_session.agent_bot_user_ids.
func (s *MessageAgentSession) BotUserIDs() []string {
	var ids []string
	if s == nil || json.Unmarshal(s.RawAgentBotUsers, &ids) != nil {
		return nil
	}
	return ids
}

// Statuses is agent_session.agent_statuses.
func (s *MessageAgentSession) Statuses() []MessageAgentStatus {
	var st []MessageAgentStatus
	if s == nil || json.Unmarshal(s.RawAgentStatuses, &st) != nil {
		return nil
	}
	return st
}

// MessageAgentStatus is one agent_session.agent_statuses entry.
type MessageAgentStatus struct {
	AgentBotUserID string `json:"agent_bot_user_id,omitempty"`
	IconURL        string `json:"icon_url,omitempty"`
	IconEmoji      string `json:"icon_emoji,omitempty"`
	Username       string `json:"username,omitempty"`
}

// PinnedInfo says who pinned a message (history / messages.list).
type PinnedInfo struct {
	Channel  string `json:"channel"`
	PinnedBy string `json:"pinned_by"`
	PinnedTS int64  `json:"pinned_ts"`
}

type Edited struct {
	User string `json:"user"`
	TS   string `json:"ts"`
}

type Reaction struct {
	Name  string   `json:"name"`
	Count int      `json:"count"`
	Users []string `json:"users"`
	URL   string   `json:"url,omitempty"`
}

// ReplyUserIcon is one reply_user_icons entry: the picture the reply bar
// shows for a bot replier (an emoji or an image).
type ReplyUserIcon struct {
	ID        string `json:"id"`
	IconEmoji string `json:"icon_emoji,omitempty"`
	IconURL   string `json:"icon_url,omitempty"`
}

type Icons struct {
	Emoji   string `json:"emoji,omitempty"`
	Image36 string `json:"image_36,omitempty"`
	Image48 string `json:"image_48,omitempty"`
	Image64 string `json:"image_64,omitempty"`
	Image72 string `json:"image_72,omitempty"`
}

type BotProfile struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	AppID   string `json:"app_id"`
	UserID  string `json:"user_id,omitempty"`
	TeamID  string `json:"team_id,omitempty"`
	Icons   Icons  `json:"icons"`
	Deleted bool   `json:"deleted"`
	// IsWorkflowBot: a Workflow Builder bot (bots.info), badged WORKFLOW.
	IsWorkflowBot bool `json:"is_workflow_bot,omitempty"`
}

// MediaProgress is a file's media_progress: where the viewer stopped
// watching it (offset_ms), and whether they watched it to its end.
type MediaProgress struct {
	OffsetMS     int  `json:"offset_ms"`
	MediaWatched bool `json:"media_watched"`
}

// FileVideo is a file's video (only its outputs are read).
type FileVideo struct {
	Outputs json.RawMessage `json:"outputs,omitempty"`
}

type File struct {
	ID                 string          `json:"id"`
	Name               string          `json:"name"`
	Title              string          `json:"title"`
	Mimetype           string          `json:"mimetype"`
	Filetype           string          `json:"filetype"`
	PrettyType         string          `json:"pretty_type"`
	Size               int64           `json:"size"`
	Mode               string          `json:"mode"`
	TitleBlocks        json.RawMessage `json:"title_blocks,omitempty"` // canvases: the title as rich text (emoji)
	URLPrivate         string          `json:"url_private"`
	URLPrivateDownload string          `json:"url_private_download"`
	Permalink          string          `json:"permalink"`
	Thumb64            string          `json:"thumb_64,omitempty"`
	Thumb80            string          `json:"thumb_80,omitempty"`
	Thumb160           string          `json:"thumb_160,omitempty"`
	Thumb360           string          `json:"thumb_360"`
	Thumb480           string          `json:"thumb_480"`
	Thumb720           string          `json:"thumb_720"`
	Thumb960           string          `json:"thumb_960"`
	Thumb1024          string          `json:"thumb_1024"`
	Thumb800           string          `json:"thumb_800,omitempty"` // HEIC/HEIF: fileIsImage takes it in thumb_360's place
	ThumbVideo         string          `json:"thumb_video"`
	ThumbPDF           string          `json:"thumb_pdf"`
	ThumbPDFW          int             `json:"thumb_pdf_w"`
	ThumbPDFH          int             `json:"thumb_pdf_h"`
	OriginalW          int             `json:"original_w"`
	OriginalH          int             `json:"original_h"`
	Thumb360W          int             `json:"thumb_360_w"`
	Thumb360H          int             `json:"thumb_360_h"`
	Thumb720W          int             `json:"thumb_720_w"`
	Thumb720H          int             `json:"thumb_720_h"`
	Thumb480W          int             `json:"thumb_480_w"`
	Thumb480H          int             `json:"thumb_480_h"`
	Thumb960W          int             `json:"thumb_960_w"`
	Thumb960H          int             `json:"thumb_960_h"`
	Thumb1024W         int             `json:"thumb_1024_w"`
	Thumb1024H         int             `json:"thumb_1024_h"`
	ThumbVideoW        int             `json:"thumb_video_w"`
	ThumbVideoH        int             `json:"thumb_video_h"`
	User               string          `json:"user,omitempty"`
	Created            int64           `json:"created,omitempty"`
	Preview            string          `json:"preview"`
	PreviewHighlight   string          `json:"preview_highlight"`
	Lines              int             `json:"lines"`
	LinesMore          int             `json:"lines_more,omitempty"`           // snippets: lines past the preview
	PreviewIsTruncated bool            `json:"preview_is_truncated,omitempty"` // snippets: the preview is cut
	EditLink           string          `json:"edit_link,omitempty"`            // snippets and posts: the web editor
	DurationMS         int             `json:"duration_ms"`
	Transcription      json.RawMessage `json:"transcription,omitempty"`
	// IsTranscriptionRegionSupported is whether the file's region can be
	// transcribed (MediaTranscriptPopoverButton shows only then).
	IsTranscriptionRegionSupported bool   `json:"is_transcription_region_supported,omitempty"`
	MP4                            string `json:"mp4"`
	HLS                            string `json:"hls"`
	// A video's: the poster Slack draws (VideoControlsOverlay's
	// thumb_video_ts ?? thumb_video), the HLS stream it prefers in
	// Electron, its captions and its audio-only source (MediaViewerContent's
	// audio ? aac : mp4), and how far the viewer watched it
	// (media.progress.update): where a player starts (xnwb).
	ThumbVideoTS  string         `json:"thumb_video_ts,omitempty"`
	HLSEmbed      string         `json:"hls_embed,omitempty"`
	VTT           string         `json:"vtt,omitempty"`
	AAC           string         `json:"aac,omitempty"`
	MediaProgress *MediaProgress `json:"media_progress,omitempty"`
	// Video is the file's video, whose outputs only fileIsHosted reads: an
	// external file with transcoded outputs plays as a hosted one.
	Video            *FileVideo `json:"video,omitempty"`
	IsExternal       bool       `json:"is_external"`
	ExternalType     string     `json:"external_type"`
	AudioWaveSamples []int      `json:"audio_wave_samples"`
	Subtype          string     `json:"subtype"`
	FileAccess       string     `json:"file_access,omitempty"`
	// InitialComment is the file's initial_comment (an old file_share's
	// comment object) as Slack sent it: Taut's mirror hands plugins
	// Slack's own value, and a notification's meta line reads whether
	// there is one (chooseFileMetaTemplate's !!initial_comment, Truthy:
	// "uploaded and commented on this file").
	InitialComment json.RawMessage `json:"initial_comment,omitempty"`
	// BotID is the bot that posted the file (the email bot's forward,
	// whose message Slackbot sends): getMsgSenderEntity's E.bot_id, and
	// getFileEntityId's.
	BotID string `json:"bot_id,omitempty"`
	// What getFileStatus (SRWA) reads besides mode and file_access: a
	// deleted file, one hidden by the free plan's storage limit, one DLP
	// tombstoned. IsSkill marks a skill canvas, FolderType a folder's kind
	// ("skill_set": a skill set).
	IsDeleted       bool   `json:"is_deleted,omitempty"`
	IsHiddenByLimit bool   `json:"is_hidden_by_limit,omitempty"`
	IsTombstoned    bool   `json:"is_tombstoned,omitempty"`
	IsSkill         bool   `json:"is_skill,omitempty"`
	FolderType      string `json:"folder_type,omitempty"`
	// MediaDisplayType is Slack's media_display_type ("unknown" while an
	// upload is still being processed). Slack's web client doesn't draw by
	// it; Clack only reads it to spot unprocessed uploads.
	MediaDisplayType string `json:"media_display_type,omitempty"`
	// IsSalesforceFile: Slack's fileIsImage takes a Salesforce image
	// without thumbs (9NKl).
	IsSalesforceFile bool `json:"is_salesforce_file,omitempty"`
	// An animated GIF's animated thumbs (thumb_360_w/h and thumb_480_w/h
	// are their sizes too; FileImageThumbnail plays them in messages) and
	// its first frame as a still at the original's size (deanimate is
	// what Accessibility > "Allow animations" off draws, and what its play
	// button and Expand go by; deanimate_gif is the older name).
	Thumb360GIF  string `json:"thumb_360_gif,omitempty"`
	Thumb480GIF  string `json:"thumb_480_gif,omitempty"`
	Deanimate    string `json:"deanimate,omitempty"`
	DeanimateGIF string `json:"deanimate_gif,omitempty"`

	// An email file (mode "email": an email Slack's email app or a
	// forwarding address brought in; filetype "email", text/html). Read
	// from Hack Club's (2026-11-18): from, to and cc as {address, name,
	// original, slack_user_id when a member sent it}, the subject, the
	// body as plain text (plain_text) and its first 500 characters
	// (preview_plain_text), Preview its HTML, the attachments the email
	// carried as Slack files (slack_file_id), the header date, and the
	// sender's DMARC check. SimplifiedHTML is files.info's.
	From                    EmailAddresses   `json:"from,omitempty"`
	To                      EmailAddresses   `json:"to,omitempty"`
	Cc                      EmailAddresses   `json:"cc,omitempty"`
	Subject                 string           `json:"subject,omitempty"`
	PlainText               string           `json:"plain_text,omitempty"`
	PreviewPlainText        string           `json:"preview_plain_text,omitempty"`
	SimplifiedHTML          string           `json:"simplified_html,omitempty"`
	EmailAttachments        EmailAttachments `json:"attachments,omitempty"`
	OriginalAttachmentCount int              `json:"original_attachment_count,omitempty"`
	InlineAttachmentCount   int              `json:"inline_attachment_count,omitempty"`
	HasMore                 bool             `json:"has_more,omitempty"`
	SentToSelf              bool             `json:"sent_to_self,omitempty"`
	EmailHeaders            *EmailHeaders    `json:"headers,omitempty"`
	EmailSecurity           *EmailSecurity   `json:"email_security,omitempty"`
	Timestamp               int64            `json:"timestamp,omitempty"`
	AppID                   string           `json:"app_id,omitempty"`
	AppName                 string           `json:"app_name,omitempty"`

	// Clack only, never on the wire. Pending marks a file of the viewer's
	// optimistic send whose upload hasn't finished (Slack's pendingUpload
	// in a placeholder's pendingFiles); its thumbs are localimg URLs.
	// UploadPercent is its percentComplete (0 = not started, 100 = bytes
	// sent, processing).
	Pending       bool `json:"-"`
	UploadPercent int  `json:"-"`
}

// EmailAddress is one of an email file's from, to or cc: the address,
// the display name, the header as written (original), and the member who
// sent it (slack_user_id).
type EmailAddress struct {
	Address     string `json:"address"`
	Name        string `json:"name"`
	Original    string `json:"original,omitempty"`
	SlackUserID string `json:"slack_user_id,omitempty"`
}

// EmailAttachment is a file an email carried: Slack keeps each as a file
// of its own (slack_file_id).
type EmailAttachment struct {
	Filename    string          `json:"filename"`
	Size        int64           `json:"size"`
	Mimetype    string          `json:"mimetype"`
	URL         string          `json:"url"`
	SlackFileID string          `json:"slack_file_id,omitempty"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
}

// EmailHeaders are an email file's headers.
type EmailHeaders struct {
	Date      string `json:"date,omitempty"`
	MessageID string `json:"message_id,omitempty"`
}

// EmailSecurity is an email file's sender checks.
type EmailSecurity struct {
	HasValidDMARC *bool `json:"has_valid_dmarc,omitempty"`
}

// The email fields decode leniently: another kind of file may carry a
// field of the same name in another shape, and a message must never fail
// to decode over one.
type (
	EmailAddresses   []EmailAddress
	EmailAttachments []EmailAttachment
)

func (a *EmailAddresses) UnmarshalJSON(b []byte) error {
	var v []EmailAddress
	if json.Unmarshal(b, &v) == nil {
		*a = v
	}
	return nil
}

func (a *EmailAttachments) UnmarshalJSON(b []byte) error {
	var v []EmailAttachment
	if json.Unmarshal(b, &v) == nil {
		*a = v
	}
	return nil
}

func (h *EmailHeaders) UnmarshalJSON(b []byte) error {
	type plain EmailHeaders
	var v plain
	if json.Unmarshal(b, &v) == nil {
		*h = EmailHeaders(v)
	}
	return nil
}

func (e *EmailSecurity) UnmarshalJSON(b []byte) error {
	type plain EmailSecurity
	var v plain
	if json.Unmarshal(b, &v) == nil {
		*e = EmailSecurity(v)
	}
	return nil
}

type AttachmentField struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short"`
}

type Attachment struct {
	ID            int               `json:"id"`
	Color         string            `json:"color"`
	Fallback      string            `json:"fallback"`
	Pretext       string            `json:"pretext"`
	AuthorName    string            `json:"author_name"`
	AuthorSubname string            `json:"author_subname"`
	AuthorLink    string            `json:"author_link"`
	AuthorIcon    string            `json:"author_icon"`
	Title         string            `json:"title"`
	TitleLink     string            `json:"title_link"`
	Text          string            `json:"text"`
	Fields        []AttachmentField `json:"fields"`
	ImageURL      string            `json:"image_url"`
	ImageWidth    int               `json:"image_width"`
	ImageHeight   int               `json:"image_height"`
	ThumbURL      string            `json:"thumb_url"`
	ThumbWidth    int               `json:"thumb_width"`
	ThumbHeight   int               `json:"thumb_height"`
	Footer        string            `json:"footer"`
	FooterIcon    string            `json:"footer_icon"`
	TS            json.RawMessage   `json:"ts"`
	ServiceName   string            `json:"service_name"`
	ServiceIcon   string            `json:"service_icon"`
	FromURL       string            `json:"from_url"`
	OriginalURL   string            `json:"original_url"`
	VideoHTML     string            `json:"video_html"`
	AudioHTML     string            `json:"audio_html,omitempty"`
	AudioHTMLH    int               `json:"audio_html_height,omitempty"`
	OtherHTML     string            `json:"other_html,omitempty"`
	ImageBytes    int64             `json:"image_bytes,omitempty"`
	MediaType     string            `json:"media_type,omitempty"` // "image" on image-only link unfurls
	ServiceURL    string            `json:"service_url,omitempty"`
	BotID         string            `json:"bot_id,omitempty"`
	IsAppUnfurl   bool              `json:"is_app_unfurl,omitempty"`
	Hidden        bool              `json:"hidden,omitempty"`
	IsMsgUnfurl   bool              `json:"is_msg_unfurl"`
	// AgentChannelUnfurl is the card of a code channel an agent made
	// (MessageKitAttachments draws it as CodeChannelEntityCard)
	AgentChannelUnfurl *AgentChannelUnfurl `json:"agent_channel_unfurl,omitempty"`
	IsShare            bool                `json:"is_share"`
	ChannelID          string              `json:"channel_id"`
	ChannelName        string              `json:"channel_name"`
	MsgSubtype         string              `json:"msg_subtype"`
	Blocks             json.RawMessage     `json:"blocks,omitempty"`
	MessageBlocks      []struct {
		Message struct {
			Blocks json.RawMessage `json:"blocks"`
		} `json:"message"`
	} `json:"message_blocks,omitempty"`
	Files    []File          `json:"files,omitempty"`
	MrkdwnIn []string        `json:"mrkdwn_in"`
	Actions  json.RawMessage `json:"actions,omitempty"`

	// Message permalink unfurls (is_msg_unfurl): the linked message's
	// author and where it sits (Slack's MessageAttachmentSlackMessage).
	AuthorID           string `json:"author_id,omitempty"`
	ChannelTeam        string `json:"channel_team,omitempty"`
	IsReplyUnfurl      bool   `json:"is_reply_unfurl,omitempty"`
	IsThreadRootUnfurl bool   `json:"is_thread_root_unfurl,omitempty"`

	// Work object unfurls (internal/workobject): the entity an app
	// unfurled the link as (BOLE getWorkObject: work_object_entity, else
	// work_object) and the app's unfurl URL (app_unfurl_url).
	WorkObjectEntity json.RawMessage `json:"work_object_entity,omitempty"`
	WorkObject       json.RawMessage `json:"work_object,omitempty"`
	AppUnfurlURL     string          `json:"app_unfurl_url,omitempty"`
	AppID            string          `json:"app_id,omitempty"`

	// Canvas-section unfurls (MessageAttachmentQuipAnchorLink): the
	// canvas and the section the link points at.
	FileID          string           `json:"file_id,omitempty"`
	DocumentSection *DocumentSection `json:"document_section,omitempty"`
}

// DocumentSection is a canvas-section unfurl's section: its id and who
// wrote it (snippet).
type DocumentSection struct {
	SectionID string `json:"section_id"`
	Snippet   struct {
		Authors []struct {
			UserID             string `json:"user_id"`
			IsAuthor           bool   `json:"is_author"`
			IsRecentEditor     bool   `json:"is_recent_editor"`
			IsMostRecentEditor bool   `json:"is_most_recent_editor"`
		} `json:"authors"`
		SectionEditedTS int64    `json:"section_edited_ts"`
		FileIDs         []string `json:"file_ids"`
	} `json:"snippet"`
}

// User is a (subset of a) Slack user object.
type User struct {
	ID                string  `json:"id"`
	TeamID            string  `json:"team_id,omitempty"`
	Name              string  `json:"name"`
	Deleted           bool    `json:"deleted,omitempty"`
	Color             string  `json:"color,omitempty"`
	RealName          string  `json:"real_name,omitempty"`
	TZ                string  `json:"tz,omitempty"`
	TZLabel           string  `json:"tz_label,omitempty"`
	TZOffset          int     `json:"tz_offset,omitempty"`
	IsAdmin           bool    `json:"is_admin,omitempty"`
	IsOwner           bool    `json:"is_owner,omitempty"`
	IsPrimaryOwner    bool    `json:"is_primary_owner,omitempty"`
	IsBot             bool    `json:"is_bot,omitempty"`
	IsAppUser         bool    `json:"is_app_user,omitempty"`
	IsSlackbot        bool    `json:"is_slackbot,omitempty"` // gmjA: Slackbot (with id USLACKBOT)
	IsRestricted      bool    `json:"is_restricted,omitempty"`
	IsUltraRestricted bool    `json:"is_ultra_restricted,omitempty"`
	AgentStatus       string  `json:"agent_status,omitempty"`      // not_an_agent | consistently_an_agent | check_required (bot users)
	IsWorkflowBot     bool    `json:"is_workflow_bot,omitempty"`   // Workflow Builder's bot user: badged WORKFLOW
	IsAgentforceBot   bool    `json:"is_agentforce_bot,omitempty"` // a Salesforce Agentforce agent: badged AGENTFORCE
	IsSidekickBot     bool    `json:"is_sidekick_bot,omitempty"`   // 7jev isSidekickMember (bD9l keeps it out of Slack Connect channels)
	Updated           int64   `json:"updated,omitempty"`
	Profile           Profile `json:"profile"`
	Presence          string  `json:"presence,omitempty"`
	// EnterpriseUser lists the Grid workspaces the user belongs to.
	EnterpriseUser *EnterpriseUser `json:"enterprise_user,omitempty"`
	// IsStranger: a Slack Connect user from an org the viewer shares no
	// channel with (users.info is_stranger).
	IsStranger bool `json:"is_stranger,omitempty"`

	// RestrictedUserType is "frontline_worker" for a frontline worker
	// (Slack's u+N9 W), else empty.
	RestrictedUserType string `json:"restricted_user_type,omitempty"`
}

type EnterpriseUser struct {
	ID             string   `json:"id,omitempty"`
	EnterpriseID   string   `json:"enterprise_id,omitempty"`
	EnterpriseName string   `json:"enterprise_name,omitempty"`
	Teams          []string `json:"teams,omitempty"`
	IsAdmin        bool     `json:"is_admin,omitempty"`
	IsOwner        bool     `json:"is_owner,omitempty"`
	IsPrimaryOwner bool     `json:"is_primary_owner,omitempty"`
}

type Profile struct {
	Title       string `json:"title,omitempty"`
	Email       string `json:"email,omitempty"` // only visible for self on Hack Club
	Phone       string `json:"phone,omitempty"`
	RealName    string `json:"real_name,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	// RealNameNormalized and DisplayNameNormalized are the names folded
	// to ASCII: Slack's _real_name_normalized_lc / _display_name_normalized_lc
	// sort keys are their lower case.
	RealNameNormalized    string          `json:"real_name_normalized,omitempty"`
	DisplayNameNormalized string          `json:"display_name_normalized,omitempty"`
	FirstName             string          `json:"first_name,omitempty"`
	LastName              string          `json:"last_name,omitempty"`
	Pronouns              string          `json:"pronouns,omitempty"`
	StatusText            string          `json:"status_text,omitempty"`
	StatusEmoji           string          `json:"status_emoji,omitempty"`
	StatusEmojiDisplay    json.RawMessage `json:"status_emoji_display_info,omitempty"`
	StatusExpiration      int64           `json:"status_expiration,omitempty"`
	AvatarHash            string          `json:"avatar_hash,omitempty"`
	Image24               string          `json:"image_24,omitempty"`
	Image32               string          `json:"image_32,omitempty"`
	Image48               string          `json:"image_48,omitempty"`
	Image72               string          `json:"image_72,omitempty"`
	Image192              string          `json:"image_192,omitempty"`
	Image512              string          `json:"image_512,omitempty"`
	ImageOriginal         string          `json:"image_original,omitempty"`
	IsCustomImage         bool            `json:"is_custom_image,omitempty"`
	Team                  string          `json:"team,omitempty"`
	Fields                json.RawMessage `json:"fields,omitempty"`
	HuddleState           string          `json:"huddle_state,omitempty"`
	HuddleChannel         string          `json:"huddle_state_channel_id,omitempty"`
	HuddleExpires         int64           `json:"huddle_state_expiration_ts,omitempty"`
	BotID                 string          `json:"bot_id,omitempty"`
	APIAppID              string          `json:"api_app_id,omitempty"`

	// StatusTextCanonical is the preset a status came from ("Out of
	// office"), whatever its text says.
	StatusTextCanonical string `json:"status_text_canonical,omitempty"`
}

// Conversation is a channel, private channel, DM or group DM.
type Conversation struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	IsChannel   bool   `json:"is_channel,omitempty"`
	IsGroup     bool   `json:"is_group,omitempty"`
	IsIM        bool   `json:"is_im,omitempty"`
	IsMPIM      bool   `json:"is_mpim,omitempty"`
	IsPrivate   bool   `json:"is_private,omitempty"`
	IsArchived  bool   `json:"is_archived,omitempty"`
	IsGeneral   bool   `json:"is_general,omitempty"`
	IsMember    bool   `json:"is_member,omitempty"`
	IsExtShared bool   `json:"is_ext_shared,omitempty"`
	IsOrgShared bool   `json:"is_org_shared,omitempty"`
	// IsFile: a canvas's or list's hidden comment conversation (its id
	// is the file's with F → C; core/sidebar_docs.go).
	IsFile bool `json:"is_file,omitempty"`
	// IsOrgMandatory: a channel everyone in the org is in (Slack's gx
	// is_general || is_org_mandatory: it can't be left).
	IsOrgMandatory bool `json:"is_org_mandatory,omitempty"`
	// IsOrgDefault: a channel new org members join (Opi6 isOrgDefaultChannel
	// with a public, org-shared channel): it can't be deleted or made private.
	IsOrgDefault  bool            `json:"is_org_default,omitempty"`
	User          string          `json:"user,omitempty"` // IM partner
	Created       int64           `json:"created,omitempty"`
	Creator       string          `json:"creator,omitempty"`
	NumMembers    int             `json:"num_members,omitempty"`
	Topic         *Purpose        `json:"topic,omitempty"`
	Purpose       *Purpose        `json:"purpose,omitempty"`
	LastRead      string          `json:"last_read,omitempty"`
	Latest        json.RawMessage `json:"latest,omitempty"`
	Members       []string        `json:"members,omitempty"`
	Properties    json.RawMessage `json:"properties,omitempty"`
	Priority      float64         `json:"priority,omitempty"`
	IsOpen        bool            `json:"is_open,omitempty"`
	ContextTeamID string          `json:"context_team_id,omitempty"`
	// IsFrozen and IsPendingExtShared keep a DM off Slack's DMs tab
	// (isFrozen, the pending Slack Connect IM check).
	IsFrozen           bool `json:"is_frozen,omitempty"`
	IsPendingExtShared bool `json:"is_pending_ext_shared,omitempty"`
	// InternalTeamIDs are the org's workspaces with access to the channel
	// (Enterprise Grid; the details "Workspaces with access" field).
	InternalTeamIDs []string `json:"internal_team_ids,omitempty"`
	// ConnectedTeamIDs are a Slack Connect channel's connected teams
	// (connected_team_ids; Slack's setConnectedTeamIds keeps them minus
	// the viewer's own top-level team).
	ConnectedTeamIDs []string `json:"connected_team_ids,omitempty"`

	// Posting permissions as the server states them; nil when absent, and
	// then they come from properties.posting_restricted_to /
	// threads_restricted_to (core/posting.go).
	IsReadOnly         *bool  `json:"is_read_only,omitempty"`
	IsThreadOnly       *bool  `json:"is_thread_only,omitempty"`
	IsNonThreadable    bool   `json:"is_non_threadable,omitempty"`
	ConversationHostID string `json:"conversation_host_id,omitempty"`
}

type Purpose struct {
	Value   string `json:"value"`
	Creator string `json:"creator,omitempty"`
	LastSet int64  `json:"last_set,omitempty"`
}

// Dormant reports Slack's server-side "inactive" flag
// (properties.is_dormant), which the sidebar's "Active only" filter hides.
func (c *Conversation) Dormant() bool {
	if len(c.Properties) == 0 {
		return false
	}
	var p struct {
		IsDormant bool `json:"is_dormant"`
	}
	_ = json.Unmarshal(c.Properties, &p)
	return p.IsDormant
}

// HasCustomMpdmName reports a group DM renamed by its members
// (properties.has_custom_mpdm_name): its name is the custom name, not
// "mpdm-a--b--c-1".
func (c *Conversation) HasCustomMpdmName() bool {
	if len(c.Properties) == 0 {
		return false
	}
	var p struct {
		HasCustomMpdmName bool `json:"has_custom_mpdm_name"`
	}
	_ = json.Unmarshal(c.Properties, &p)
	return p.HasCustomMpdmName
}

// Section is a sidebar section from users.channelSections.list.
type Section struct {
	ID             string `json:"channel_section_id"`
	Name           string `json:"name"`
	Type           string `json:"type"`
	Emoji          string `json:"emoji"`
	Next           string `json:"next_channel_section_id"`
	IsRedacted     bool   `json:"is_redacted"`
	ChannelIDsPage struct {
		ChannelIDs []string `json:"channel_ids"`
		Count      int      `json:"count"`
		Cursor     string   `json:"cursor"`
	} `json:"channel_ids_page"`
}

// CountEntry is a per-conversation entry from client.counts.
type CountEntry struct {
	ID           string `json:"id"`
	LastRead     string `json:"last_read"`
	Latest       string `json:"latest"`
	Updated      string `json:"updated"`
	MentionCount int    `json:"mention_count"`
	HasUnreads   bool   `json:"has_unreads"`
}

type Counts struct {
	Threads struct {
		HasUnreads   bool `json:"has_unreads"`
		MentionCount int  `json:"mention_count"`
		// UnreadCountByChannel is unread_count_by_channel: unread threads
		// per conversation (Slack's threadsUnreadCountByChannel).
		UnreadCountByChannel map[string]int `json:"unread_count_by_channel,omitempty"`
	} `json:"threads"`
	Channels      []CountEntry    `json:"channels"`
	MPIMs         []CountEntry    `json:"mpims"`
	IMs           []CountEntry    `json:"ims"`
	ChannelBadges map[string]int  `json:"channel_badges"`
	Saved         json.RawMessage `json:"saved"`
	// ActivityV2 is the Activity inbox's unread badge count by entry type
	// (Slack's transformClientCountsOutput activityV2).
	ActivityV2 map[string]int `json:"activity_v2"`
	// Alerts is the unread count by user alert type (Slack's userAlerts;
	// the classic Activity badge sums some: core classic_badges.go).
	Alerts map[string]int `json:"alerts"`
}

// UnmarshalJSON decodes a message and keeps the raw attachments JSON (for
// plugins that need fields the typed Attachment drops). Malformed
// attachments no longer fail the whole message. SubscribedSet records
// whether "subscribed" was there at all.
func (m *Message) UnmarshalJSON(b []byte) error {
	type plain Message
	aux := struct {
		*plain
		RawAtt json.RawMessage `json:"attachments,omitempty"`
		RawSub json.RawMessage `json:"subscribed,omitempty"`
	}{plain: (*plain)(m)}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	m.Attachments = nil
	m.RawAttachments = nil
	if len(aux.RawAtt) > 0 && string(aux.RawAtt) != "null" {
		m.RawAttachments = append(json.RawMessage(nil), aux.RawAtt...)
		_ = json.Unmarshal(aux.RawAtt, &m.Attachments)
	}
	m.Subscribed, m.SubscribedSet = false, false
	if len(aux.RawSub) > 0 && string(aux.RawSub) != "null" {
		m.SubscribedSet = json.Unmarshal(aux.RawSub, &m.Subscribed) == nil
	}
	return nil
}

// MessageAIContext is a message's ai_context (the fields Slack's client
// reads).
type MessageAIContext struct {
	ResultStatus    string `json:"result_status,omitempty"`
	StoppedByUserID string `json:"stopped_by_user_id,omitempty"`
	// Type "slackbot_ai" marks Slackbot AI's reply (X1VC
	// isSlackbotAiMessage); Summary.Type "huddle" an AI huddle summary
	// (getMsgSenderEntity's isAiHuddleSummary).
	Type    string            `json:"type,omitempty"`
	Summary *MessageAISummary `json:"summary,omitempty"`
}

// MessageAISummary is ai_context.summary.
type MessageAISummary struct {
	Type string `json:"type,omitempty"`
}

// MessageComment is a legacy file_comment message's comment: its author
// is the message's sender when the message has no user (getMsgSenderEntity:
// t.user || t.comment.user).
type MessageComment struct {
	User string `json:"user,omitempty"`
}

// MessageListEdit is a list_edit message's list_edit: who made the edit
// (getMsgSenderEntity's isListEditMsg sender).
type MessageListEdit struct {
	CreatedBy string `json:"created_by,omitempty"`
}

// AgentChannelUnfurl is an attachment's agent_channel_unfurl: the code
// channel an agent session made.
type AgentChannelUnfurl struct {
	ChannelID             string `json:"channel_id"`
	ChannelName           string `json:"channel_name"`
	AgentBotUserID        string `json:"agent_bot_user_id"`
	IsPrivate             bool   `json:"is_private"`
	JoinableFromChannelID string `json:"joinable_from_channel_id"`
	WorkflowName          string `json:"workflow_name,omitempty"`
	WorkflowTriggerID     string `json:"workflow_trigger_id,omitempty"`
}
