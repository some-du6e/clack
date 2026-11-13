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
	IsEphemeral    bool            `json:"is_ephemeral,omitempty"` // Slack's addEphemeralMessage (local, never from history)
	Root           *Message        `json:"root,omitempty"`         // thread_broadcast
	ReplyBroadcast bool            `json:"reply_broadcast,omitempty"`
	Upload         bool            `json:"upload,omitempty"`
	DisplayAsBot   bool            `json:"display_as_bot,omitempty"`
	Room           json.RawMessage `json:"room,omitempty"` // huddles
	Channel        string          `json:"channel,omitempty"`
	Inviter        string          `json:"inviter,omitempty"`
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
	// FunctionExecutionID marks a workflow step's message: its block
	// actions carry it (Slack's blocks.actions function_execution_id).
	FunctionExecutionID string `json:"function_execution_id,omitempty"`
	// IsLocked marks a thread root whose replies an admin turned off
	// (Slack's p6HR isLocked: `is_locked`); LockedByTeamID is the team
	// that locked it. RTM's message_locked updates both on the stored
	// root (core/thread_lock.go).
	IsLocked       bool   `json:"is_locked,omitempty"`
	LockedByTeamID string `json:"locked_by_team_id,omitempty"`
	// UploadStatus is a placeholder's PendingFilesMetaTextWrapper text
	// ("Waiting to upload…", "Uploading…", "Sending…") while the viewer's
	// own files upload: Clack only, never on the wire.
	UploadStatus string `json:"-"`
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
	DurationMS         int             `json:"duration_ms"`
	Transcription      json.RawMessage `json:"transcription,omitempty"`
	MP4                string          `json:"mp4"`
	HLS                string          `json:"hls"`
	IsExternal         bool            `json:"is_external"`
	ExternalType       string          `json:"external_type"`
	AudioWaveSamples   []int           `json:"audio_wave_samples"`
	Subtype            string          `json:"subtype"`
	FileAccess         string          `json:"file_access,omitempty"`
	// MediaDisplayType is Slack's media_display_type ("unknown" while an
	// upload is still being processed). Slack's web client doesn't draw by
	// it; Clack only reads it to spot unprocessed uploads.
	MediaDisplayType string `json:"media_display_type,omitempty"`

	// Clack only, never on the wire. Pending marks a file of the viewer's
	// optimistic send whose upload hasn't finished (Slack's pendingUpload
	// in a placeholder's pendingFiles); its thumbs are localimg URLs.
	// UploadPercent is its percentComplete (0 = not started, 100 = bytes
	// sent, processing).
	Pending       bool `json:"-"`
	UploadPercent int  `json:"-"`
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
	IsShare       bool              `json:"is_share"`
	ChannelID     string            `json:"channel_id"`
	ChannelName   string            `json:"channel_name"`
	MsgSubtype    string            `json:"msg_subtype"`
	Blocks        json.RawMessage   `json:"blocks,omitempty"`
	MessageBlocks []struct {
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
	IsBot             bool    `json:"is_bot,omitempty"`
	IsAppUser         bool    `json:"is_app_user,omitempty"`
	IsSlackbot        bool    `json:"is_slackbot,omitempty"` // gmjA: Slackbot (with id USLACKBOT)
	IsRestricted      bool    `json:"is_restricted,omitempty"`
	IsUltraRestricted bool    `json:"is_ultra_restricted,omitempty"`
	AgentStatus       string  `json:"agent_status,omitempty"`      // not_an_agent | consistently_an_agent | check_required (bot users)
	IsWorkflowBot     bool    `json:"is_workflow_bot,omitempty"`   // Workflow Builder's bot user: badged WORKFLOW
	IsAgentforceBot   bool    `json:"is_agentforce_bot,omitempty"` // a Salesforce Agentforce agent: badged AGENTFORCE
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
}

type Profile struct {
	Title              string          `json:"title,omitempty"`
	Email              string          `json:"email,omitempty"` // only visible for self on Hack Club
	Phone              string          `json:"phone,omitempty"`
	RealName           string          `json:"real_name,omitempty"`
	DisplayName        string          `json:"display_name,omitempty"`
	FirstName          string          `json:"first_name,omitempty"`
	LastName           string          `json:"last_name,omitempty"`
	Pronouns           string          `json:"pronouns,omitempty"`
	StatusText         string          `json:"status_text,omitempty"`
	StatusEmoji        string          `json:"status_emoji,omitempty"`
	StatusEmojiDisplay json.RawMessage `json:"status_emoji_display_info,omitempty"`
	StatusExpiration   int64           `json:"status_expiration,omitempty"`
	AvatarHash         string          `json:"avatar_hash,omitempty"`
	Image24            string          `json:"image_24,omitempty"`
	Image32            string          `json:"image_32,omitempty"`
	Image48            string          `json:"image_48,omitempty"`
	Image72            string          `json:"image_72,omitempty"`
	Image192           string          `json:"image_192,omitempty"`
	Image512           string          `json:"image_512,omitempty"`
	ImageOriginal      string          `json:"image_original,omitempty"`
	IsCustomImage      bool            `json:"is_custom_image,omitempty"`
	Team               string          `json:"team,omitempty"`
	Fields             json.RawMessage `json:"fields,omitempty"`
	HuddleState        string          `json:"huddle_state,omitempty"`
	HuddleChannel      string          `json:"huddle_state_channel_id,omitempty"`
	HuddleExpires      int64           `json:"huddle_state_expiration_ts,omitempty"`
	BotID              string          `json:"bot_id,omitempty"`
	APIAppID           string          `json:"api_app_id,omitempty"`

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
	IsOrgMandatory bool            `json:"is_org_mandatory,omitempty"`
	User           string          `json:"user,omitempty"` // IM partner
	Created        int64           `json:"created,omitempty"`
	Creator        string          `json:"creator,omitempty"`
	NumMembers     int             `json:"num_members,omitempty"`
	Topic          *Purpose        `json:"topic,omitempty"`
	Purpose        *Purpose        `json:"purpose,omitempty"`
	LastRead       string          `json:"last_read,omitempty"`
	Latest         json.RawMessage `json:"latest,omitempty"`
	Members        []string        `json:"members,omitempty"`
	Properties     json.RawMessage `json:"properties,omitempty"`
	Priority       float64         `json:"priority,omitempty"`
	IsOpen         bool            `json:"is_open,omitempty"`
	ContextTeamID  string          `json:"context_team_id,omitempty"`
	// IsFrozen and IsPendingExtShared keep a DM off Slack's DMs tab
	// (isFrozen, the pending Slack Connect IM check).
	IsFrozen           bool `json:"is_frozen,omitempty"`
	IsPendingExtShared bool `json:"is_pending_ext_shared,omitempty"`
	// InternalTeamIDs are the org's workspaces with access to the channel
	// (Enterprise Grid; the details "Workspaces with access" field).
	InternalTeamIDs []string `json:"internal_team_ids,omitempty"`

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
}

// UnmarshalJSON decodes a message and keeps the raw attachments JSON (for
// plugins that need fields the typed Attachment drops). Malformed
// attachments no longer fail the whole message.
func (m *Message) UnmarshalJSON(b []byte) error {
	type plain Message
	aux := struct {
		*plain
		RawAtt json.RawMessage `json:"attachments,omitempty"`
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
	return nil
}
