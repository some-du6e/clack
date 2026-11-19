package theme

import "image/color"

// Palette names follow Slack's design tokens loosely. Values are measured from
// the 2026 Slack web client (Aubergine theme for the chrome).
type Palette struct {
	// Window chrome (top nav, tab rail, sidebar sit on this gradient).
	ChromeA, ChromeB, ChromeC color.NRGBA // conic/radial gradient stops
	ChromeText                color.NRGBA // rail labels, top nav icons
	ChromeTextDim             color.NRGBA
	ChromeHover               color.NRGBA // top nav buttons' :hover tile (theme-surf-inv-pry)
	ChromePressed             color.NRGBA // top nav buttons' :active tile (theme-surf-inv-sec)
	ChromeActive              color.NRGBA // rail icon selected tile
	// ChromeHoverTint is the chrome scope's theme-surf-inv-pry-hover,
	// the background-image layer Slack blends (--dt_blend-mode-interaction)
	// over theme-surf-inv-pry on the search pill and a hovered control
	// strip button, pre-blended so it composites over SearchBg or
	// ChromeActive with Over (IATheme.hoverTint).
	ChromeHoverTint color.NRGBA
	SearchBg        color.NRGBA
	SearchText      color.NRGBA
	// ChromeBasePry and ChromeContentTer are the control strip buttons'
	// :active and --active fill and ink (theme-base-pry and
	// theme-content-ter in the rail's scope: the focus button while focus
	// mode is on, + while the Create menu is open), ChromeBasePryHover the
	// fill with :hover's blended surf-inv-pry-hover layer over it.
	ChromeBasePry, ChromeContentTer, ChromeBasePryHover color.NRGBA

	// Sidebar (translucent over the chrome gradient).
	SidebarBg         color.NRGBA
	SidebarText       color.NRGBA // read channels
	SidebarTextUnread color.NRGBA // unread, bold
	SidebarHover      color.NRGBA
	SidebarActive     color.NRGBA // selected item background
	SidebarActiveText color.NRGBA
	SidebarDivider    color.NRGBA
	SidebarButton     color.NRGBA // compose button tile
	SidebarTextTer    color.NRGBA // theme-content-inv-ter: muted rows, org captions, "(you)"
	SidebarRule       color.NRGBA // rgba(var(--sk_foreground_low), .13) in the sidebar's scope
	WorkspaceBorder   color.NRGBA // dark mode: 1px outline around sidebar+panel

	// Badges.
	Badge     color.NRGBA // mention count pill
	BadgeText color.NRGBA
	// RailBadge and RailBadgeText are the rail's count pill, in the
	// chrome's theme scope (the sidebar's can be the light one; Fields).
	RailBadge, RailBadgeText color.NRGBA

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

	Link   color.NRGBA
	Accent color.NRGBA // focus rings, selected tab underline, blue buttons
	// ThemeImp and ThemeImpText are --dt_color-theme-base-imp and
	// -theme-content-imp when an account theme changes them (zero with
	// Slack's default theme: packages keep their own default then), and
	// ThemeInvSec, ThemeInvTer and ThemeOtlInvPry the root scope's
	// --dt_color-theme-content-inv-sec / -inv-ter and -otl-inv-pry, the
	// same way: what the peek cards (a portal under document.body,
	// outside .p-ia4_client) draw the DMs' timestamps and rows' rules with.
	ThemeImp, ThemeImpText color.NRGBA
	ThemeInvSec            color.NRGBA
	ThemeInvTer            color.NRGBA
	ThemeOtlInvPry         color.NRGBA
	Mention                color.NRGBA // @user pill text
	MentionBg              color.NRGBA
	SelfMention            color.NRGBA // @you / @here background
	Highlight              color.NRGBA // message highlight (jump-to, pinned)
	CodeFg                 color.NRGBA
	CodeBg                 color.NRGBA
	CodeBorder             color.NRGBA
	PreBg                  color.NRGBA
	QuoteBar               color.NRGBA
	NewLine                color.NRGBA // "New" unread divider (red)
	Send                   color.NRGBA // send button green
	SendText               color.NRGBA
	Danger                 color.NRGBA
	Warning                color.NRGBA
	Presence               color.NRGBA // active dot
	PresenceAway           color.NRGBA
	Reaction               color.NRGBA
	ReactionSelf           color.NRGBA
	ReactionSelfBorder     color.NRGBA
	Tooltip                color.NRGBA // base-inv-pry (.c-tooltip__tip, toasts; tooltip.go)
	TooltipText            color.NRGBA // content-inv-pry
	TooltipSub             color.NRGBA // content-inv-sec (TooltipSubtitle)
	TooltipGlow            color.NRGBA // dark mode drop-shadow, otl-pry (zero in light)
	Selection              color.NRGBA // text selection
	ScrollThumb            color.NRGBA
}

func rgb(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

func rgba(v uint32, a uint8) color.NRGBA {
	c := rgb(v)
	c.A = a
	return c
}

// LightPalette is Slack's default light mode with the Aubergine chrome
// (measured from app.slack.com, 2026-11). Its chrome and sidebar fields are
// the default theme's tokens as Slack's code computes them
// (theme_contrast_test.go), so a custom theme's colours and these never
// part where Slack's don't.
var LightPalette = Palette{
	ChromeA:         rgb(0x4a154b), // radial center + conic light stop
	ChromeB:         rgb(0x39063a), // conic dark stop
	ChromeC:         rgb(0x4a154b),
	ChromeText:      rgb(0xffffff),
	ChromeTextDim:   rgba(0xf6e4ff, 0xe6),
	ChromeHover:     rgba(0xf9edff, 0x40),
	ChromePressed:   rgba(0xf9edff, 0x1c),
	ChromeActive:    rgba(0xf9edff, 0x40),
	ChromeHoverTint: rgba(0x82378a, 0x14), // aubergine-70 at .08, multiplied over aubergine-0 at .25
	SearchBg:        rgba(0xf9edff, 0x40),
	SearchText:      rgb(0xffffff),

	ChromeBasePry:      rgb(0xf9edff), // the inverted scope's aubergine-0
	ChromeContentTer:   rgb(0x39063a), // aubergine-100
	ChromeBasePryHover: rgb(0xefdef6), // aubergine-70 at .08 multiplied over it

	SidebarBg:         rgba(0xf9edff, 0x1c),
	SidebarText:       rgba(0xf6e4ff, 0xe6),
	SidebarTextUnread: rgb(0xffffff),
	SidebarHover:      rgba(0xf9edff, 0x14),
	SidebarActive:     rgb(0xf9edff),
	SidebarActiveText: rgb(0x39063a),
	SidebarDivider:    rgba(0xf9edff, 0x38),
	SidebarButton:     rgba(0xf9edff, 0x40),
	SidebarTextTer:    rgba(0xf9edff, 0x99),
	SidebarRule:       rgba(0xe8e8e8, 0x21),

	Badge:         rgb(0xeabdfb),
	BadgeText:     rgb(0x39063a),
	RailBadge:     rgb(0xeabdfb),
	RailBadgeText: rgb(0x39063a),

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
	TooltipSub:         rgb(0xcbcccf),
	Selection:          rgba(0x1d9bd1, 0x4d),
	ScrollThumb:        rgba(0x1d1c1d, 0x40),
}

// DarkPalette is Slack's dark mode (its chrome and sidebar fields Slack's
// computed default tokens too).
var DarkPalette = Palette{
	// measured from app.slack.com in dark mode (Aubergine), 2026-11-07
	ChromeA:         rgb(0x431349),
	ChromeB:         rgb(0x200221),
	ChromeC:         rgb(0x431349),
	ChromeText:      rgb(0xf8f8f8), // theme-content-inv-pry: gray-100
	ChromeTextDim:   rgba(0xe3ceeb, 0xcc),
	ChromeHover:     rgba(0xefe1f5, 0x40),
	ChromePressed:   rgba(0x101214, 0x99),
	ChromeActive:    rgba(0xefe1f5, 0x40),
	ChromeHoverTint: rgba(0xefe1f5, 0x14), // aubergine-100 at .08, lighten over itself
	SearchBg:        rgba(0xefe1f5, 0x40),
	SearchText:      rgb(0xf8f8f8),

	ChromeBasePry:      rgb(0xe3ceeb), // aubergine-90
	ChromeContentTer:   rgb(0x200221), // aubergine-0
	ChromeBasePryHover: rgb(0xe4d0ec), // aubergine-100 at .08, lighten over it

	SidebarBg:         rgba(0x101214, 0x99),
	SidebarText:       rgba(0xe3ceeb, 0xcc),
	SidebarTextUnread: rgb(0xf8f8f8), // --dt_color-theme-content-inv-pry: gray-100 (sidebar-rows side-by-sides)
	SidebarHover:      rgba(0xefe1f5, 0x14),
	SidebarActive:     rgb(0x7d3986),
	SidebarActiveText: rgb(0xefe1f5),
	SidebarDivider:    rgba(0xefe1f5, 0x38),
	SidebarButton:     rgba(0xefe1f5, 0x40),
	SidebarTextTer:    rgba(0xe3ceeb, 0x99),
	SidebarRule:       rgba(0xe8e8e8, 0x21),
	WorkspaceBorder:   rgba(0xefe1f5, 0x26),

	Badge:         rgb(0xcfa1de), // theme-base-imp: aubergine-70
	BadgeText:     rgb(0x200221), // theme-content-imp: aubergine-0
	RailBadge:     rgb(0xcfa1de),
	RailBadgeText: rgb(0x200221),

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
	Accent:             rgb(0xe3ceeb), // theme-base-pry: aubergine-90
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
	Presence:           rgb(0x3daa7c), // theme-base-inv-hgl-2: jade-60
	PresenceAway:       rgb(0x9a9b9e),
	Reaction:           rgba(0xd1d2d3, 0x14),
	ReactionSelf:       rgba(0x1d9bd1, 0x26),
	ReactionSelfBorder: rgb(0x1d9bd1),
	Tooltip:            rgb(0x212428), // base-inv-pry
	TooltipText:        rgb(0xf8f8f8), // content-inv-pry
	TooltipSub:         rgb(0xb9babd), // content-inv-sec
	TooltipGlow:        rgb(0x797c81), // otl-pry
	Selection:          rgba(0x1d9bd1, 0x4d),
	ScrollThumb:        rgba(0xd1d2d3, 0x40),
}

// Metrics shared across packages (dp unless noted). Taken from the Slack
// 2026 desktop layout.
const (
	TopNavH      = 40
	RailW        = 70
	SidebarW     = 232 // before the first layout; then Slack's auto share of the window (ui/sidebar_width.go)
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
