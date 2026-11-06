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
	Name                     string // display-name override (bots, custom usernames)
	Avatar                   string // avatar override (bot icons, icon_emoji)
	Subtype, Text            string // Text: raw mrkdwn (editing, copy, search)
	ThreadTS, LatestReply    string
	EditedTS                 string
	Permalink                string // set on search results
	IsBot, Edited, Pinned    bool
	Broadcast, System        bool // System: join/leave/topic… drawn compactly
	Pending                  bool // optimistic local send not yet confirmed
	ReplyCount               int
	ReplyUsers               []string
	Reactions                []Reaction
	Huddle                   json.RawMessage
	Doc                      richtext.Doc
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
	ID, Name, RealName, Handle       string // Name: display name as Slack shows it
	Avatar, Avatar192, Avatar512     string
	Title, Pronouns                  string
	StatusText, StatusEmoji          string
	StatusEmojiURL                   string
	StatusExp                        int64
	TZ, TZLabel                      string
	TZOffset                         int
	IsBot, Deleted, IsAdmin, IsOwner bool
	Guest                            bool
	Color, Presence                  string // Presence: active | away | ""
	Huddle                           bool
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
}

// ConvRef describes a conversation that may not be in the sidebar
// (search, activity and saved results).
type ConvRef struct {
	ID, Name, Kind, User string
}

// Section is a sidebar section in display order.
type Section struct {
	ID, Name string
	Type     string // stars | channels | direct_messages | standard | apps | …
	Emoji    string
	Channels []string
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
// emoji_changed, sidebar_changed, prefs, huddle, resync, plus raw
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
	Connected bool   // connection
	Presence  string // presence: active | away
	Users     []string
	Prefs     *Prefs
	Raw       json.RawMessage
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
	Files                      []UploadFile
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
}

type SearchResult struct {
	Messages           []Message
	Total, Page, Pages int
	Convs              map[string]ConvRef
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
	IsMember                 bool
	IsOrgShared, IsExtShared bool
	LastRead                 string
	TopicSetBy               string
	TopicSetAt               int64
	PurposeSetBy             string
	PurposeSetAt             int64
}

type ActivityOpts struct {
	View   string // all | mentions | threads | reactions | invites | apps
	Types  string // explicit activity.feed types (overrides View)
	Unread bool
	Cursor string
	Limit  int
}

type ActivityItem struct {
	Key, Type, FeedTS string
	Unread            bool
	Channel, TS       string
	ThreadTS, Author  string
	Msg               *Message
	Root              *Message // thread parent, for thread items
	Reaction          *ActivityReaction
	Count             int // unread replies / bundled messages
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
	ItemID, ItemType, Channel, TS, State string
	DateCreated, DateDue, DateCompleted  int64
	Archived                             bool
	Msg                                  *Message
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

type Draft struct {
	ID, Channel, ThreadTS string
	Users                 []string
	Broadcast             bool
	Doc                   richtext.Doc
	Blocks                json.RawMessage
	DateCreated           int64
	DateScheduled         int64
	LastUpdated           string
	Files                 int
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
}

type CommandOpts struct {
	Channel, Command, Text, ThreadTS string
}

type CommandResult struct {
	Msg         *Message
	Response    string
	ResponseDoc []richtext.Block
}
