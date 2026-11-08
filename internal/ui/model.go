package ui

import "github.com/some-du6e/yoloslackgoclient/internal/richtext"

// model is the shell's view of the workspace, rebuilt from core snapshots
// and events (never per frame).
type model struct {
	version int

	TeamName, TeamIcon string
	SelfID             string
	SelfName           string
	SelfAvatar         string
	SelfActive         bool
	Connected          bool

	Sections []sbSection
	Convs    map[string]*convInfo

	ActivityCount int  // unread mentions/reactions for the Activity tab
	DMUnread      bool // dot on the DMs tab
	DMCount       int
	HomeUnread    bool
	HomeCount     int // mention total shown on Home when not on Home

	threadsUnread bool
}

type sbSection struct {
	ID, Name, Type, Emoji string
	EmojiURL              string   // the section emoji's image ("" = Slack's default icon)
	Rows                  []string // conv ids
}

type convInfo struct {
	TopicSpans     []richtext.Span // first line of the parsed topic
	ID, Name, Kind string          // channel | private | im | mpim | app
	User           string          // im partner
	Avatar         string
	Topic          string
	NumMembers     int
	Members        []string
	Muted          bool
	Archived       bool
	Unread         bool
	Mentions       int
	Self           bool // DM with yourself
	Active         bool // partner presence
	Bot            bool
	Starred        bool
	LastRead       string
	Latest         string
	StatusEmoji    string // DM partner's status emoji image (shown after the name)
}

func (m *model) conv(id string) *convInfo {
	if m == nil || m.Convs == nil {
		return nil
	}
	return m.Convs[id]
}

// displayName is the sidebar/header label for a conversation.
func (c *convInfo) displayName() string {
	if c == nil {
		return ""
	}
	return c.Name
}
