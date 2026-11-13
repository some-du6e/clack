package ui

import (
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
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
	// FilterNames is what the sidebar text filter matches (core
	// sidebar_filter.go).
	FilterNames core.FilterNames

	ActivityCount int  // unread mentions/reactions for the Activity tab
	DMUnread      bool // dot on the DMs tab
	DMCount       int
	HomeUnread    bool
	HomeCount     int // mention total shown on Home when not on Home

	threadsUnread bool
}

type sbSection struct {
	ID, Name, Type, Emoji string
	DefaultName           bool     // Name is Slack's default for Type (drawn in the locale)
	EmojiURL              string   // the section emoji's image ("" = Slack's default icon)
	Collapsed             bool     // the account's collapsed state (channel_sections "c"; core/section_collapse.go)
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
	// File: a canvas's or list's conversation (core.Conv.File); FileID is
	// its file. Doc ("canvas" | "list") makes its sidebar row a document
	// row: the document's icon, NameSpans as its name (emoji drawn), and
	// a click opens the document (sidebar_docs.go).
	File      bool
	FileID    string
	Doc       string
	NameSpans []richtext.Span
	// Slack Connect (sidebar_connect.go): Shared is is_ext_shared, Teams
	// the other organizations (core.Conv.ConnectedTeams); PeerTeam is a
	// DM partner's team when they're external (isExternal, another org).
	Shared   bool
	Teams    []string
	PeerTeam string
}

// mentionTotal is the workspace's mention count (unmuted conversations'
// mentions), the simplified layout's workspace badge.
func (m *model) mentionTotal() int {
	n := 0
	for _, c := range m.Convs {
		if !c.Muted {
			n += c.Mentions
		}
	}
	return n
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

// unsuppressedUnreads is Slack's totalUnsuppressedUnreadsInChannels > 0
// (82Kz f3): a conversation the unread model counts (not muted, a
// member, not archived) has unreads.
func (m *model) unsuppressedUnreads() bool {
	for _, c := range m.Convs {
		if (c.Unread || c.Mentions > 0) && !c.Muted && !c.Archived && !c.Preview {
			return true
		}
	}
	return false
}
