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
	LatestReply    string          `json:"latest_reply,omitempty"`
	Subscribed     bool            `json:"subscribed,omitempty"`
	Edited         *Edited         `json:"edited,omitempty"`
	Reactions      []Reaction      `json:"reactions,omitempty"`
	Files          []File          `json:"files,omitempty"`
	Blocks         json.RawMessage `json:"blocks,omitempty"`
	Attachments    []Attachment    `json:"attachments,omitempty"`
	Icons          *Icons          `json:"icons,omitempty"`
	BotProfile     *BotProfile     `json:"bot_profile,omitempty"`
	PinnedTo       []string        `json:"pinned_to,omitempty"`
	ClientMsgID    string          `json:"client_msg_id,omitempty"`
	Hidden         bool            `json:"hidden,omitempty"`
	Root           *Message        `json:"root,omitempty"` // thread_broadcast
	ReplyBroadcast bool            `json:"reply_broadcast,omitempty"`
	Upload         bool            `json:"upload,omitempty"`
	DisplayAsBot   bool            `json:"display_as_bot,omitempty"`
	Room           json.RawMessage `json:"room,omitempty"` // huddles
	Channel        string          `json:"channel,omitempty"`
	Inviter        string          `json:"inviter,omitempty"`
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
	Icons   Icons  `json:"icons"`
	Deleted bool   `json:"deleted"`
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
	URLPrivate         string          `json:"url_private"`
	URLPrivateDownload string          `json:"url_private_download"`
	Permalink          string          `json:"permalink"`
	Thumb360           string          `json:"thumb_360"`
	Thumb480           string          `json:"thumb_480"`
	Thumb720           string          `json:"thumb_720"`
	Thumb960           string          `json:"thumb_960"`
	Thumb1024          string          `json:"thumb_1024"`
	ThumbVideo         string          `json:"thumb_video"`
	OriginalW          int             `json:"original_w"`
	OriginalH          int             `json:"original_h"`
	Thumb360W          int             `json:"thumb_360_w"`
	Thumb360H          int             `json:"thumb_360_h"`
	Thumb720W          int             `json:"thumb_720_w"`
	Thumb720H          int             `json:"thumb_720_h"`
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
	IsRestricted      bool    `json:"is_restricted,omitempty"`
	IsUltraRestricted bool    `json:"is_ultra_restricted,omitempty"`
	Updated           int64   `json:"updated,omitempty"`
	Profile           Profile `json:"profile"`
	Presence          string  `json:"presence,omitempty"`
}

type Profile struct {
	Title              string          `json:"title,omitempty"`
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
	BotID              string          `json:"bot_id,omitempty"`
	APIAppID           string          `json:"api_app_id,omitempty"`
}

// Conversation is a channel, private channel, DM or group DM.
type Conversation struct {
	ID            string          `json:"id"`
	Name          string          `json:"name,omitempty"`
	IsChannel     bool            `json:"is_channel,omitempty"`
	IsGroup       bool            `json:"is_group,omitempty"`
	IsIM          bool            `json:"is_im,omitempty"`
	IsMPIM        bool            `json:"is_mpim,omitempty"`
	IsPrivate     bool            `json:"is_private,omitempty"`
	IsArchived    bool            `json:"is_archived,omitempty"`
	IsGeneral     bool            `json:"is_general,omitempty"`
	IsMember      bool            `json:"is_member,omitempty"`
	IsExtShared   bool            `json:"is_ext_shared,omitempty"`
	IsOrgShared   bool            `json:"is_org_shared,omitempty"`
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
}

type Purpose struct {
	Value   string `json:"value"`
	Creator string `json:"creator,omitempty"`
	LastSet int64  `json:"last_set,omitempty"`
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
	} `json:"threads"`
	Channels      []CountEntry    `json:"channels"`
	MPIMs         []CountEntry    `json:"mpims"`
	IMs           []CountEntry    `json:"ims"`
	ChannelBadges map[string]int  `json:"channel_badges"`
	Saved         json.RawMessage `json:"saved"`
}
