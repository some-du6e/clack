package theme

import "image/color"

// Palette names follow Slack's design tokens loosely. Values are measured from
// the 2026 Slack web client (Aubergine theme for the chrome).
type Palette struct {
	// Window chrome (top nav, tab rail, sidebar sit on this gradient).
	ChromeA, ChromeB, ChromeC color.NRGBA // conic/radial gradient stops
	ChromeText                color.NRGBA // rail labels, top nav icons
	ChromeTextDim             color.NRGBA
	ChromeHover               color.NRGBA // rail icon hover / nav button hover
	ChromeActive              color.NRGBA // rail icon selected tile
	SearchBg, SearchHover     color.NRGBA
	SearchText                color.NRGBA

	// Sidebar (translucent over the chrome gradient).
	SidebarBg         color.NRGBA
	SidebarText       color.NRGBA // read channels
	SidebarTextUnread color.NRGBA // unread, bold
	SidebarHover      color.NRGBA
	SidebarActive     color.NRGBA // selected item background
	SidebarActiveText color.NRGBA
	SidebarDivider    color.NRGBA
	SidebarButton     color.NRGBA // compose button tile
	WorkspaceBorder   color.NRGBA // dark mode: 1px outline around sidebar+panel

	// Badges.
	Badge     color.NRGBA // mention count pill
	BadgeText color.NRGBA

	// Primary content panel.
	Bg           color.NRGBA
	BgHover      color.NRGBA // message hover
	BgRaised     color.NRGBA // menus, popovers
	BgInset      color.NRGBA // inputs, code blocks
	Fg           color.NRGBA // primary text
	FgMuted      color.NRGBA // secondary (timestamps, topic)
	FgSubtle     color.NRGBA // tertiary
	Border       color.NRGBA
	BorderStrong color.NRGBA
	Skeleton     color.NRGBA
	Overlay      color.NRGBA // modal scrim
	PanelShadow  color.NRGBA

	Link               color.NRGBA
	Accent             color.NRGBA // focus rings, selected tab underline, blue buttons
	Mention            color.NRGBA // @user pill text
	MentionBg          color.NRGBA
	SelfMention        color.NRGBA // @you / @here background
	Highlight          color.NRGBA // message highlight (jump-to, pinned)
	CodeFg             color.NRGBA
	CodeBg             color.NRGBA
	CodeBorder         color.NRGBA
	PreBg              color.NRGBA
	QuoteBar           color.NRGBA
	NewLine            color.NRGBA // "New" unread divider (red)
	Send               color.NRGBA // send button green
	SendText           color.NRGBA
	Danger             color.NRGBA
	Warning            color.NRGBA
	Presence           color.NRGBA // active dot
	PresenceAway       color.NRGBA
	Reaction           color.NRGBA
	ReactionSelf       color.NRGBA
	ReactionSelfBorder color.NRGBA
	Tooltip            color.NRGBA
	TooltipText        color.NRGBA
	Selection          color.NRGBA // text selection
	ScrollThumb        color.NRGBA
}

// SearchTint is the extra rgba(131,56,138,.08) layer on Slack's search pill.
var SearchTint = rgba(0x83388a, 0x14)

func rgb(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

func rgba(v uint32, a uint8) color.NRGBA {
	c := rgb(v)
	c.A = a
	return c
}

// LightPalette is Slack's default light mode with the Aubergine chrome
// (measured from app.slack.com, 2026-11).
var LightPalette = Palette{
	ChromeA:       rgb(0x4a154b), // radial center + conic light stop
	ChromeB:       rgb(0x39063a), // conic dark stop
	ChromeC:       rgb(0x4a154b),
	ChromeText:    rgb(0xffffff),
	ChromeTextDim: rgba(0xf6e4ff, 0xe6),
	ChromeHover:   rgba(0xf9edff, 0x26),
	ChromeActive:  rgba(0xf9edff, 0x40),
	SearchBg:      rgba(0xf9edff, 0x40),
	SearchHover:   rgba(0xf9edff, 0x55),
	SearchText:    rgb(0xffffff),

	SidebarBg:         rgba(0xf9edff, 0x1c),
	SidebarText:       rgba(0xf6e4ff, 0xe6),
	SidebarTextUnread: rgb(0xffffff),
	SidebarHover:      rgba(0xf9edff, 0x1a),
	SidebarActive:     rgb(0xf9edff),
	SidebarActiveText: rgb(0x39063a),
	SidebarDivider:    rgba(0xf9edff, 0x33),
	SidebarButton:     rgba(0xf9edff, 0x40),

	Badge:     rgb(0xeabdfb),
	BadgeText: rgb(0x39063a),

	Bg:           rgb(0xffffff),
	BgHover:      rgb(0xf8f8f8),
	BgRaised:     rgb(0xffffff),
	BgInset:      rgb(0xf8f8f8),
	Fg:           rgb(0x1d1c1d),
	FgMuted:      rgb(0x454447),
	FgSubtle:     rgb(0x868686),
	Border:       rgba(0x1d1c1d, 0x21),
	BorderStrong: rgba(0x1d1c1d, 0x4d),
	Skeleton:     rgb(0xeaeaea),
	Overlay:      rgba(0x000000, 0x99),
	PanelShadow:  rgba(0x000000, 0x17),

	Link:               rgb(0x1264a3),
	Accent:             rgb(0x83388a), // selected tab underline
	Mention:            rgb(0x1264a3),
	MentionBg:          rgba(0x1d9bd1, 0x1a),
	SelfMention:        rgba(0xf2c744, 0x66),
	Highlight:          rgba(0xf2c744, 0x33),
	CodeFg:             rgb(0xc01343),
	CodeBg:             rgba(0x1d1c1d, 0x0a),
	CodeBorder:         rgba(0x1d1c1d, 0x21),
	PreBg:              rgba(0x1d1c1d, 0x0a),
	QuoteBar:           rgb(0xdddddd),
	NewLine:            rgb(0xe01e5a),
	Send:               rgb(0x007a5a),
	SendText:           rgb(0xffffff),
	Danger:             rgb(0xe01e5a),
	Warning:            rgb(0xecb22e),
	Presence:           rgb(0x20a271),
	PresenceAway:       rgb(0x868686),
	Reaction:           rgba(0x1d1c1d, 0x0f),
	ReactionSelf:       rgba(0x1d9bd1, 0x1a),
	ReactionSelfBorder: rgb(0x1d9bd1),
	Tooltip:            rgb(0x1d1c1d),
	TooltipText:        rgb(0xffffff),
	Selection:          rgba(0x1d9bd1, 0x4d),
	ScrollThumb:        rgba(0x1d1c1d, 0x40),
}

// DarkPalette is Slack's dark mode.
var DarkPalette = Palette{
	// measured from app.slack.com in dark mode (Aubergine), 2026-11-07
	ChromeA:       rgb(0x431349),
	ChromeB:       rgb(0x200221),
	ChromeC:       rgb(0x431349),
	ChromeText:    rgb(0xffffff),
	ChromeTextDim: rgba(0xefe1f5, 0xe6),
	ChromeHover:   rgba(0xefe1f5, 0x26),
	ChromeActive:  rgba(0xefe1f5, 0x40),
	SearchBg:      rgba(0xefe1f5, 0x40),
	SearchHover:   rgba(0xefe1f5, 0x55),
	SearchText:    rgb(0xffffff),

	SidebarBg:         rgba(0x101214, 0x99),
	SidebarText:       rgba(0xe3ceeb, 0xcc),
	SidebarTextUnread: rgb(0xffffff),
	SidebarHover:      rgba(0xefe1f5, 0x14),
	SidebarActive:     rgb(0x7d3986),
	SidebarActiveText: rgb(0xefe1f5),
	SidebarDivider:    rgba(0xefe1f5, 0x26),
	SidebarButton:     rgba(0xefe1f5, 0x40),
	WorkspaceBorder:   rgba(0xefe1f5, 0x26),

	Badge:     rgb(0xeabdfb),
	BadgeText: rgb(0x39063a),

	Bg:           rgb(0x1a1d21),
	BgHover:      rgb(0x222529),
	BgRaised:     rgb(0x222529),
	BgInset:      rgb(0x222529),
	Fg:           rgb(0xd1d2d3),
	FgMuted:      rgb(0xb9babd),
	FgSubtle:     rgb(0xababad),
	Border:       rgba(0xd1d2d3, 0x21),
	BorderStrong: rgba(0xd1d2d3, 0x4d),
	Skeleton:     rgba(0xd1d2d3, 0x14),
	Overlay:      rgba(0x000000, 0xb3),
	PanelShadow:  rgba(0x000000, 0x60),

	Link:               rgb(0x1d9bd1),
	Accent:             rgb(0xd29bd6),
	Mention:            rgb(0x1d9bd1),
	MentionBg:          rgba(0x1d9bd1, 0x1a),
	SelfMention:        rgba(0xf2c744, 0x33),
	Highlight:          rgba(0xf2c744, 0x1a),
	CodeFg:             rgb(0xe8912d),
	CodeBg:             rgba(0xd1d2d3, 0x0f),
	CodeBorder:         rgba(0xd1d2d3, 0x21),
	PreBg:              rgba(0xd1d2d3, 0x0a),
	QuoteBar:           rgba(0xd1d2d3, 0x33),
	NewLine:            rgb(0xe01e5a),
	Send:               rgb(0x007a5a),
	SendText:           rgb(0xffffff),
	Danger:             rgb(0xe01e5a),
	Warning:            rgb(0xecb22e),
	Presence:           rgb(0x20a271),
	PresenceAway:       rgb(0x9a9b9e),
	Reaction:           rgba(0xd1d2d3, 0x14),
	ReactionSelf:       rgba(0x1d9bd1, 0x26),
	ReactionSelfBorder: rgb(0x1d9bd1),
	Tooltip:            rgb(0xe8e8e8),
	TooltipText:        rgb(0x1d1c1d),
	Selection:          rgba(0x1d9bd1, 0x4d),
	ScrollThumb:        rgba(0xd1d2d3, 0x40),
}

// Metrics shared across packages (dp unless noted). Taken from the Slack
// 2026 desktop layout.
const (
	TopNavH      = 40
	RailW        = 70
	SidebarW     = 232 // default; resizable
	SidebarMinW  = 160
	SidebarMaxW  = 480
	PanelRadius  = 6
	PanelInset   = 4 // gap between the panel and the window edges (right/bottom)
	SearchW      = 432
	HeaderH      = 49 // channel header row
	TabsH        = 38
	SecondaryW   = 400 // thread / details pane default
	SecondaryMin = 300
	AvatarMsg    = 36
	SidebarRowH  = 28
	MsgText      = 15 // sp
	MsgLine      = 22 // sp
	SmallText    = 13
	TinyText     = 12
)
