package ui

import (
	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
)

// model is the shell's view of the workspace, rebuilt from core snapshots
// and events (never per frame).
type model struct {
	version int

	TeamName, TeamIcon string
	Rail               core.RailState // the left rail (core/rail.go)
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
	Collapsed             bool     // saved collapsed state (seeds the sidebar's toggle once)
	Rows                  []string // conv ids
	CollapsedRows         []string // what shows while collapsed (unread rows, muted included)
	// For the heading menus (section_menu.go): every conversation before
	// the filter, the hidden ones by reason, and the effective filter and
	// sort (core.Section).
	All                                                  []string
	HiddenRead, HiddenMuted, HiddenDormant, HiddenUnread []string
	Filter, Sort                                         string
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
	Preview        bool   // a channel the viewer isn't in (core.Conv.Preview): the join pane
	Purpose        string // the join pane's description
	Unread         bool
	Mentions       int
	Self           bool // DM with yourself
	Active         bool // partner presence
	Bot            bool
	Starred        bool
	LastRead       string
	Latest         string
	StatusEmoji    string // DM partner's status emoji image (shown after the name)
	// Code is set for a code channel (core.Conv.Code); CodeStatus is its
	// agent status and AgentName the agent's name (codechannel.go).
	Code       *core.CodeChannel
	CodeStatus string
	AgentName  string
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
