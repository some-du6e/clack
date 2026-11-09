// Package core is yoloslack's in-process service API for the native UI:
// boot, reads, writes and typed realtime events, on top of internal/slack
// (network) and internal/store (memory). It has no HTTP surface.
//
// Scraping policy: message contents only ever live in memory. Nothing in
// this package writes message text, files or rendered documents to disk or
// logs; logs carry method names, ids, counts and timings only.
package core

import (
	"encoding/json"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
)

// Message is a UI-ready message: metadata plus a structured document.
type Message struct {
	TS, Channel, User, BotID string
	Name                     string       // the sender's name when it isn't a member's (user-less bots: username or the bot's name); sender.go
	Avatar                   string       // the avatar of a user-less sender (icon_emoji, icons, the bot's, Slack's default); sender.go
	Username                 string       // msg.username as sent (chat.postMessage username)
	Icons                    *SenderIcons // msg.icons as sent (icon_emoji / icon_url)
	Subtype, Text            string       // Text: raw mrkdwn (editing, copy, search)
	ThreadTS, LatestReply    string
	EditedTS                 string
	Permalink                string // set on search results
	ClientMsgID              string
	PinnedBy                 string // who pinned it (when Pinned)
	IsBot, Edited, Pinned    bool
	Broadcast, System        bool // System: join/leave/topic… drawn compactly
	// Tombstone: a deleted message kept for its replies (Slack's subtype
	// "tombstone", user USLACKBOT, hidden): drawn as Slack's
	// MessageTombstone, never as a Slackbot message.
	Tombstone       bool
	Pending         bool // optimistic local send not yet confirmed (TS is local)
	Failed          bool // optimistic send failed: offer Resend / DiscardPending
	Saved           bool // in the viewer's Saved / To-dos
	FollowingThread bool // the viewer follows this thread (root messages)
	ReplyCount      int
	ReplyUsers      []string
	Reactions       []Reaction
	Huddle          json.RawMessage
	Doc             richtext.Doc
	// Raw Slack data for plugins (Taut): memory only, never written to disk.
	Blocks      json.RawMessage // Block Kit blocks as sent
	Attachments json.RawMessage // legacy attachments / unfurls as sent
	Metadata    json.RawMessage // app metadata {event_type, event_payload}
	BotProfile  *BotProfile     // bot_profile of bot messages
	// StreamingState is an app's chat stream (chat.startStream /
	// appendStream / stopStream): "in_progress" while open, then
	// "completed" or "errored" (agent.go).
	StreamingState string
}

// SubtypeSlackbotResponse is the subtype of Slackbot's answers to the
// workspace's custom responses (Customize Slack > Slackbot, the
// slackbot.responses API): user USLACKBOT, subtype "slackbot_response".
const SubtypeSlackbotResponse = "slackbot_response"

// SlackbotCustomResponse reports whether m is Slackbot answering with one
// of the workspace's custom responses.
func (m *Message) SlackbotCustomResponse() bool { return m.Subtype == SubtypeSlackbotResponse }

// CustomResponse is Slack's isCustomResponse (REqS ZO): a custom response
// posted by Slackbot itself, which the reskin_custom_responses experiment
// shows as the team (App.CustomResponseTeam) with an info badge.
// SlackbotCustomResponse, the subtype alone, is what MessageLabels and the
// row's custom_response class check.
func (m *Message) CustomResponse() bool {
	return m.Subtype == SubtypeSlackbotResponse && m.User == "USLACKBOT"
}

// BotProfile is the bot_profile Slack attaches to app messages.
type BotProfile struct {
	ID, Name, AppID, UserID, TeamID string
	Icon                            string      // best icon URL
	Icons                           SenderIcons // bot_profile.icons as sent
	Deleted                         bool
}

type Reaction struct {
	Name  string
	Count int
	Users []string
	URL   string // emoji image
	Self  bool   // the viewer reacted
}

// User is a person or bot as the UI shows it.
type User struct {
	ID, Name, RealName, Handle string // Name: display name as Slack shows it
	// DisplaySet: profile.display_name is set, so Name is it and Slack's
	// member rows (MediumMemberEntity) show RealName after it as the
	// secondary name; otherwise Name is the real name and there is none.
	DisplaySet                       bool
	Avatar, Avatar192, Avatar512     string
	Title, Pronouns                  string
	Email                            string // only when Slack shows it (Hack Club: the viewer's own)
	StatusText, StatusEmoji          string
	StatusEmojiURL                   string
	StatusExp                        int64
	TZ, TZLabel                      string
	TZOffset                         int
	IsBot, Deleted, IsAdmin, IsOwner bool
	Guest                            bool
	Color, Presence                  string // Presence: active | away | ""
	Huddle                           bool
	// HuddleChannel and HuddleExpires are profile.huddle_state_channel_id
	// and huddle_state_expiration_ts: the huddle's channel and when the
	// state lapses (Slack shows the sender's headphones only while both
	// are set and the time is in the future).
	HuddleChannel                 string
	HuddleExpires                 int64
	Agent                         bool // agent_status consistently_an_agent (read for the badge only under support_lots_of_agents; sender.go)
	Workflow, Agentforce, AppUser bool // is_workflow_bot, is_agentforce_bot, is_app_user (the sender badge; sender.go)

	// TeamID is the member's team as Slack's computeLocality reads it
	// (the org on Enterprise Grid for org members, else team_id).
	// External: a Slack Connect member (neither on the viewer's team nor
	// in its org); Stranger: users.info is_stranger.
	TeamID   string
	External bool
	Stranger bool
	// BotID and AppID are a bot user's profile.bot_id and api_app_id
	// (Slack opens AppHoverCard for them instead of the member card).
	BotID, AppID string
}

// Conv is a sidebar conversation.
type Conv struct {
	ID, Name   string
	Kind       string // channel | private | im | mpim
	User       string // IM partner
	Members    []string
	Topic      string
	TopicDoc   []richtext.Block
	Purpose    string
	PurposeDoc []richtext.Block
	NumMembers int
	IsArchived bool
	IsGeneral  bool
	IsShared   bool
	Created    int64
	Creator    string
	Priority   float64
	Muted      bool
	Starred    bool
	// Preview: a channel the viewer isn't in (Slack's isPreview), shown
	// with JoinChannelPane instead of the composer (preview.go).
	Preview bool
	// TempUntil: a temporary membership's expiration_ts ("Join…"), 0 when
	// the membership is permanent.
	TempUntil int64
	// Code is set for a code channel (codechannel.go); CodeStatus is its
	// effective agent status (active | processing | suspended | closed |
	// stale) and AgentName its agent's name ("Agent" when unknown).
	Code       *CodeChannel
	CodeStatus string
	AgentName  string
}

// ConvRef describes a conversation that may not be in the sidebar
// (search, activity and saved results).
type ConvRef struct {
	ID, Name, Kind, User string
}

// Section is a sidebar section in display order.
type Section struct {
	ID, Name string
	Type     string // stars | channels | direct_messages | standard | recent_apps | apps | …
	Emoji    string // Slack emoji name ("" when the section has none)
	EmojiURL string // its image (custom or standard), "" when unknown
	Channels []string
	// CollapsedChannels is what the section shows while collapsed: Slack
	// keeps its unread rows there, muted ones included (same order).
	CollapsedChannels []string
	// Collapsed is the user's saved collapsed state (channel_sections pref "c").
	Collapsed bool
	// All is every conversation the section holds before its filter
	// (lJBI channelsAssociatedOrdered), sorted like Channels: "Mark all as
	// read" and "Mute all conversations" act on it.
	All []string
	// Rows the filter hides, by reason (lJBI readHiddenChannelsOrdered,
	// mutedHiddenChannelsOrdered, dormantHiddenChannelsOrdered,
	// unreadWithNoMentionsHiddenChannelsOrdered): the heading's right-click
	// menu lists them.
	HiddenRead, HiddenMuted, HiddenDormant, HiddenUnread []string
	// Filter and Sort are the section's effective "sidebar" and "sort"
	// values (all | active | hid | mentions | today | d7 | d30; default |
	// recent | priority).
	Filter, Sort string
}

// Count is a conversation's unread state. DMs count every unread message
// as a mention, like Slack's badge.
type Count struct {
	LastRead, Latest string
	Unread           bool
	Mentions         int
	Muted            bool
}

// ThreadsState is the unread state of the Threads view.
type ThreadsState struct {
	Unread   bool
	Mentions int
}

type Team struct {
	ID, Name, Icon, Domain string
}

// Prefs are the user preferences that change rendering.
type Prefs = store.Prefs

// Snapshot is everything the sidebar needs, at one instant.
type Snapshot struct {
	Self         User
	Team         Team
	Convs        []Conv // sorted by name
	Sections     []Section
	Counts       map[string]Count
	Threads      ThreadsState
	Users        map[string]User // users the sidebar shows (IM partners, self)
	Connected    bool
	Prefs        Prefs
	EmojiCount   int
	EnterpriseID string
	Rail         RailState // the left rail from Navigation prefs and experiments (rail.go)
}

// Page is a window of a conversation's history.
type Page struct {
	Messages []Message // ascending
	HasMore  bool      // older messages exist
	LastRead string
	Conv     *Conv
}

// Event is a realtime change. Type is one of: message, update, delete,
// typing, marked, presence, user, conv, conv_removed, counts, connection,
// emoji_changed, sidebar_changed, prefs, huddle, resync, poll (realtime
// is off or down: refetch what is open), realtime (live updates turned
// on/off, in Connected), plus raw
// passthroughs (thread_marked, update_thread_state, dnd_updated,
// dnd_updated_user, pref_change, badge_counts_updated) in Raw.
type Event struct {
	Type      string
	Channel   string
	TS        string
	ThreadTS  string
	UserID    string
	Name      string // typing: display name
	Msg       *Message
	User      *User
	Conv      *Conv
	Count     *Count // message, marked: the conversation's new unread state
	Reply     bool   // message: a thread reply
	Mention   bool   // message: mentions the viewer
	ConvKind  string // message: kind of the conversation
	Connected bool   // connection: socket up; realtime: live updates on
	Presence  string // presence: active | away
	Users     []string
	Prefs     *Prefs
	// sent: the confirmed message replaces the local pending row whose TS
	// is ReplacesTS. message/sent/update/delete of own sends carry ClientMsgID.
	ReplacesTS  string
	ClientMsgID string
	ID          string        // draft id (draft), activity key (activity)
	Draft       *Draft        // draft: create | update | send
	Saved       *SavedItem    // saved: add | update | delete
	Activity    *ActivityItem // activity: updated item (hydrated when possible)
	Raw         json.RawMessage
}

// SendOpts are the options for Send.
type SendOpts struct {
	Channel, Text, ThreadTS string
	Broadcast               bool
	Blocks                  json.RawMessage
}

// UploadOpts share one or more files (held in memory, never spooled to disk).
type UploadOpts struct {
	Channel, ThreadTS, Comment string
	Broadcast                  bool             // a thread reply also sent to the channel
	Blocks                     json.RawMessage  // the message's rich_text blocks (Comment is then unused)
	Files                      []UploadFile     // uploaded now
	Prepared                   []PreparedUpload // uploaded earlier with PrepareUpload
	Progress                   func(sent, total int64)
}

// PreparedUpload is a file uploaded with PrepareUpload but not yet shared.
type PreparedUpload struct {
	ID, Name, Title string
	Size            int64
}

type UploadFile struct {
	Name, Title string
	Data        []byte
}

type UploadedFile struct {
	ID, Name, Title string
}

type SearchOpts struct {
	Query       string
	Page, Count int
	Sort        string // score (default) | timestamp | oldest
	Module      string // "messages" (default) | "dms"
	Context     string // search_context ("" = the module's results tab)
	ExactOnly   bool   // More > Exact matches only
	Session     SearchSession
	// InChannel is search in channel (⌘F): the query runs as
	// "in:<#InChannel> …" with count 10.
	InChannel      string
	RecentChannels []string // recent_channels
	ActiveCID      string   // request_context.active_cid: the open view
}

type SearchResult struct {
	Messages             []Message
	Total, Page, Pages   int
	Convs                map[string]ConvRef
	FilterIn, FilterFrom []FilterSuggestion // filter_suggestions
}

// ChannelHit is a channel search / browse result.
type ChannelHit struct {
	ID, Name, Kind string
	IsMember       bool
	IsArchived     bool
	NumMembers     int
	Purpose, Topic string
	Created        int64
}

type ChannelPage struct {
	Channels           []ChannelHit
	Total, Pages, Page int
}

type Usergroup struct {
	ID, Handle, Name, Description string
	UserCount                     int
}

type Pin struct {
	Type      string
	Created   int64
	CreatedBy string
	Msg       *Message
}

type Bookmark struct {
	ID, Title, Link, Emoji, EmojiURL, Icon, Type, EntityID, Rank, ParentID string
	Created                                                                int64
}

// ChannelDetails is the channel-details view.
type ChannelDetails struct {
	Conv                     Conv
	Managers                 []string // channel managers ("Managed by")
	IsMember                 bool
	IsOrgShared, IsExtShared bool
	// Teams are the org workspaces with access (internal_team_ids, else
	// context_team_id), their names and icons loaded (App.Team).
	Teams        []string
	LastRead     string
	TopicSetBy   string
	TopicSetAt   int64
	PurposeSetBy string
	PurposeSetAt int64
}

type ActivityOpts struct {
	View     string // all | mentions | threads | reactions | invites | apps
	Types    string // explicit activity.feed types (overrides View)
	Unread   bool
	Archived bool // cleared items instead of the inbox
	Cursor   string
	Limit    int
}

type ActivityItem struct {
	Key, Type, FeedTS string
	Unread            bool
	Channel, TS       string
	ThreadTS, Author  string
	Msg               *Message
	Root              *Message // thread parent, for thread items
	Reaction          *ActivityReaction
	Count             int // unread replies / bundled messages / posts

	// Items that aren't (only) a message: the person behind it when that
	// isn't the message author (inviter, invitee, assigner), and the
	// payload's own text.
	Actor     string // user id
	ActorName string // when the payload names a user outside the workspace
	Title     string // invite channel name, calendar title, alert category
	Team      string // Slack Connect invite: the inviting organisation
	Text      string // alert text, invite message, reminder detail

	// list_* items: the list and the item (list_*_payload list_id, row_id).
	ListID, ListRecord string
}

type ActivityReaction struct {
	User, Name, URL string
}

type ActivityPage struct {
	Items []ActivityItem
	Next  string
	Convs map[string]ConvRef
}

type Thread struct {
	Channel     string
	Root        Message
	Replies     []Message
	UnreadCount int
}

type ThreadsPage struct {
	Threads     []Thread
	HasMore     bool
	Next        string // pass back as the cursor
	TotalUnread int
	State       ThreadsState
	Convs       map[string]ConvRef
}

type SavedItem struct {
	ItemID, ItemType, Channel, TS       string
	State                               string // todo_state if set, else state (legacy)
	TodoState                           string // to_do | saved | completed
	Progress                            string // in_progress | completed | archived …
	DateCreated, DateDue, DateCompleted int64
	Archived                            bool
	Msg                                 *Message
}

type SavedPage struct {
	Items  []SavedItem
	Counts json.RawMessage
	Next   string
	Convs  map[string]ConvRef
}

// SavedUpdate changes a saved item; nil fields are left alone.
type SavedUpdate struct {
	Completed, Archived *bool
	DateDue             *int64
}

// Draft is a server-side draft (synced across clients). DateScheduled > 0
// makes it a scheduled message.
type Draft struct {
	ID, Channel, ThreadTS string
	ClientMsgID           string
	Users                 []string // DM / group DM destination without a channel yet
	Broadcast             bool
	Text                  string // plain text of Doc
	Doc                   richtext.Doc
	Blocks                json.RawMessage
	DateCreated           int64
	DateScheduled         int64
	LastUpdated           string
	Files                 int
	FileIDs               []string
	FileInfo              []DraftFile // the attached files as drafts.list describes them (by FileIDs order when known)
}

// DraftFile is a file attached to a draft: uploaded but not shared yet
// (files.getUploadURL, the bytes, files.completeUpload; no files.share).
type DraftFile struct {
	ID, Name, Title, Mimetype, PrettyType string
	Size                                  int64
	Thumb                                 string // thumb_360 for images ("" for other files)
}

// DraftOpts create (ID == "") or update a draft. Without Blocks, Text is
// stored as one plain rich_text section.
type DraftOpts struct {
	ID, Channel, ThreadTS string
	Users                 []string
	Broadcast             bool
	Text                  string
	Blocks                json.RawMessage
	FileIDs               []string
	DateScheduled         int64 // set when editing a scheduled draft (keeps it scheduled)
}

// ScheduledMessage is a message waiting to be posted at PostAt (unix s).
type ScheduledMessage struct {
	ID, Channel, ThreadTS string
	PostAt, DateCreated   int64
	Text                  string
	Doc                   richtext.Doc
	Blocks                json.RawMessage
}

type DM struct {
	Channel string
	Conv    Conv
	Latest  *Message
	Count   Count
}

type DMPage struct {
	DMs  []DM // newest first
	Next string
}

type Command struct {
	Name, Desc, Usage, Type, AppID string
	AppName                        string // app commands: the app's display name
	IconURL                        string // app commands: the app's icon (Slack CDN)
}

type CommandOpts struct {
	Channel, Command, Text, ThreadTS string
}

type CommandResult struct {
	Msg         *Message
	Response    string
	ResponseDoc []richtext.Block
}
