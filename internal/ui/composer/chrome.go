package composer

import (
	"image"
	"image/color"
	"strconv"
	"strings"

	"gioui.org/f32"
	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// The composer's chrome: the bordered box with the formatting bar, the text
// area around the editor, attachments, the thread broadcast row, the footer
// toolbar with the send split button, and the hint bar under the box.
// Measurements are Slack's (CSS px at dpr 1, used as dp); see the composer
// spec for the source of each number.

type chromeMode uint8

const (
	modeChannel chromeMode = iota
	modeThread
	modeEdit
)

type chromeKind uint8

const (
	caFocus      chromeKind = iota // click on the box background / text area padding: focus the editor
	caStyle                        // a formatting button: Style set
	caBlock                        // list / quote / code-block button: Block set
	caLink                         // link button
	caFormatting                   // Aa toggle
	caAttach                       // round + button
	caEmoji                        // emoji button
	caMention                      // @ button
	caShortcut                     // slash-box "Run shortcut"
	caVideo                        // record video clip
	caAudio                        // record audio clip
	caSend                         // send part of the split button
	caSchedule                     // send-options chevron
	caBroadcast                    // "Also send to #channel" checkbox toggled
	caSave                         // edit mode
	caCancel                       // edit mode
	caParagraph                    // ¶▾ button
	caDivider                      // divider button: insert a divider line
)

// chromeAction is something the user did in the chrome.
type chromeAction struct {
	Kind  chromeKind
	Style Style
	Block BlockKind
	// Anchor is the button's rect in composer-local pixels (composer.go
	// adds its window Origin).
	Anchor image.Rectangle
}

// chromeState is what the chrome draws this frame.
type chromeState struct {
	Mode           chromeMode
	Focused        bool
	ShowFormatting bool
	Active         Style     // styles active at the caret (pressed buttons)
	Block          BlockKind // caret line kind (pressed list/quote/code-block buttons)
	Link           bool      // caret in a link (link button pressed)
	InCode         bool      // caret in a code block: B I U S link code divider disabled
	HintInPre      bool      // caret in code typed with ``` under enter_is_special_in_tbt (the hint bar's Enter)
	CanSend        bool
	Broadcast      bool // thread: checkbox checked
	// ChannelType ("channel", "im", "mpim", …) and SetBroadcast feed the
	// broadcast context plugin composer buttons get ({active, channelType, set}).
	ChannelType   string
	SetBroadcast  func(bool)
	ChannelName   string
	Private, IsDM bool
	Group         bool // a group DM (IsDM too): "Also send to the group"
	// Placeholder is "Message general" (channel: drawn as "Message " + the
	// hash/lock icon + name), "Message Karim" (DM), "Reply to thread…".
	Placeholder     string
	ShowPlaceholder bool
	// PlaceholderMsg: Placeholder holds a channel's {msg} (its icon and
	// name), with PlaceholderPre before it and PlaceholderPost after, as
	// the locale words it ("Message " and ""; ja-JP "" and " へのメッセージ").
	PlaceholderMsg                  bool
	PlaceholderPre, PlaceholderPost string
	Typing                          string // "Karim is typing" for the hint bar, "" none
	// ScrolledTop / ScrolledBottom: editor content is scrolled under the
	// top / there is more below; draws the 1px edge separators.
	ScrolledTop, ScrolledBottom bool
	// Origin is the composer's window position (composer.Origin); the
	// chrome anchors its own "More options" menus with it.
	Origin image.Point

	// Slack-shaped identity for the plugin boundaries (slack.go).
	Key, Channel, ThreadTS, DraftID, ViewContext string
	// Hide is the buttons plugins turned off (MessageInput's enable*
	// flags, crBtn bits). NoBroadcast hides the "Also send to" row
	// (dontShowBroadcastControls). SquareTop squares the box's top
	// corners under a plugin's context bar.
	Hide        uint32
	NoBroadcast bool
	SquareTop   bool
	// Pending: files wait in the attachments row, so the box holds
	// Slack's .c-pending_files container (crWysiwygKids)
	Pending bool
	Count   int // characters in the message (CharacterCount)
	// HeadingFormats is the rich_text_headers_and_dividers experiment:
	// italic, underline, strike and code stay on in headings.
	HeadingFormats bool
	// InputEmpty is MessageInput's isTextyInputEmpty (empty of text and
	// block formats): Run shortcut is disabled once anything is typed.
	InputEmpty bool
	// OverLimit is how many characters the message is past the 12,000
	// limit (TextyLengthWarning's numCharsOverLimit; charlimit.go).
	OverLimit int
	SelLo     int // selection start (TextyInput's quill.getSelection)
	SelN      int // selected characters
	// TypingIDs / TypingNames: who Typing names, sorted by name.
	TypingIDs, TypingNames []string
	// TypingStatus is an app's assistant status (Typing is then
	// "<app> <status>", Slack's AiAppStatusIndicator), TypingAgent the
	// app's name (bold).
	TypingStatus, TypingAgent string
}

// sendOff is isSendingDisabledForTextState: nothing to send, or the
// message is over the 12,000-character limit (isOverMaxLength).
func (st *chromeState) sendOff() bool { return !st.CanSend || st.OverLimit > 0 }

// crBtn names every clickable in the chrome.
type crBtn uint8

const (
	crBold crBtn = iota
	crItalic
	crUnderline
	crStrike
	crPara
	crLink
	crOrdered
	crBullet
	crQuote
	crCode
	crCodeBlock
	crDivider
	crFmtMore
	crAttach
	crAa
	crEmoji
	crMention
	crVideo
	crAudio
	crShortcut
	crFootMore
	crSend
	crSchedule
	crCancel
	crSave
	crBroadcast
	crNumBtn
)

type crBtnInfo struct {
	tip, keys string     // tooltip label and shortcut chips ("" none)
	icon      icons.Icon // nil: drawn by chrome_icons.go
	menuIcon  string     // icon name in the overflow menu
	kind      chromeKind
	style     Style
	block     BlockKind
}

// Package vars initialize after the icons package's init has registered
// its icons, so icons.Get is safe here (and resolved once, not per frame).
var crInfo = [crNumBtn]crBtnInfo{
	crBold:      {tip: "Bold", keys: "Mod+B", icon: icons.Get("bold"), kind: caStyle, style: Bold},
	crItalic:    {tip: "Italic", keys: "Mod+I", icon: icons.Get("italic"), kind: caStyle, style: Italic},
	crUnderline: {tip: "Underline", keys: "Mod+U", icon: icons.Get("underline"), kind: caStyle, style: Underline},
	crStrike:    {tip: "Strikethrough", keys: "Mod+Shift+X", icon: icons.Get("strike"), kind: caStyle, style: Strike},
	crPara:      {kind: caParagraph},
	crLink:      {tip: "Link", keys: "Mod+Shift+U", icon: icons.Get("link"), menuIcon: "link", kind: caLink},
	crOrdered:   {tip: "Ordered list", keys: "Mod+Shift+7", icon: icons.Get("list-ordered"), menuIcon: "list-ordered", kind: caBlock, block: Ordered},
	crBullet:    {tip: "Bulleted list", keys: "Mod+Shift+8", icon: icons.Get("list-bullet"), menuIcon: "list-bullet", kind: caBlock, block: Bullet},
	crQuote:     {tip: "Blockquote", keys: "Mod+Shift+9", icon: icons.Get("quote"), menuIcon: "quote", kind: caBlock, block: Quote},
	crCode:      {tip: "Code", keys: "Mod+Shift+C", icon: icons.Get("code"), menuIcon: "code", kind: caStyle, style: Code},
	crCodeBlock: {tip: "Code block", keys: "Mod+Alt+Shift+C", icon: icons.Get("code-block"), menuIcon: "code-block", kind: caBlock, block: CodeBlock},
	crDivider:   {tip: "Divider", icon: icons.Get("divider"), menuIcon: "divider", kind: caDivider},
	crFmtMore:   {tip: "More options", icon: icons.Get("more")},
	crAttach:    {tip: "Attach", icon: icons.Get("plus"), kind: caAttach},
	crAa:        {kind: caFormatting},
	crEmoji:     {tip: "Emoji", icon: icons.Get("emoji"), kind: caEmoji},
	crMention:   {tip: "Mention someone", icon: icons.Get("mention"), kind: caMention},
	crVideo:     {tip: "Record video clip", icon: icons.Get("video"), menuIcon: "video", kind: caVideo},
	crAudio:     {tip: "Record audio clip", icon: icons.Get("mic"), menuIcon: "mic", kind: caAudio},
	crShortcut:  {tip: "Run shortcut", icon: icons.Get("slash"), menuIcon: "slash", kind: caShortcut},
	crFootMore:  {tip: "More options", icon: icons.Get("more")},
	crSend:      {kind: caSend},
	crSchedule:  {kind: caSchedule},
	crCancel:    {kind: caCancel},
	crSave:      {kind: caSave},
	crBroadcast: {kind: caBroadcast},
}

var crIcChevron, crIcPara, crIcAa = icons.Get("chevron-down"), icons.Get("paragraph"), icons.Get("format")

// crIcHeading is the ¶ trigger's icon in a heading: ComposerMenuTrigger
// shows the active menu item's IconForFormat (heading-1/2/3).
var crIcHeading = [3]icons.Icon{icons.Get("heading-1"), icons.Get("heading-2"), icons.Get("heading-3")}

// crParaIcon is the ¶ trigger's icon for the caret's line kind.
func crParaIcon(k BlockKind) icons.Icon {
	switch k {
	case Header1:
		return crIcHeading[0]
	case Header2:
		return crIcHeading[1]
	case Header3:
		return crIcHeading[2]
	}
	return crIcPara
}

// crFmtOrder is the formatting bar left to right; crFmtSep marks the
// buttons preceded by a separator.
var crFmtOrder = [...]crBtn{crBold, crItalic, crUnderline, crStrike, crPara, crLink, crOrdered, crBullet, crQuote, crCode, crCodeBlock, crDivider}

var crFmtSep = [len(crFmtOrder)]bool{5: true, 8: true}

// crFootOverflow is what the footer's ⋯ holds on narrow composers, in
// TextyButtons' order: the composer, emoji and mention buttons once the
// toolbar is under 65, 105 and 160px (crFootMoves), the clip buttons, Run
// shortcut (overflow.go).
var crFootOverflow = [...]crBtn{crAa, crEmoji, crMention, crVideo, crAudio, crShortcut}

// crPal is the chrome's colors, pre-composited where Slack's are
// translucent (Gio blends in linear light; theme.Over matches browsers).
type crPal struct {
	bg, bar, fg, icon, iconDis             color.NRGBA
	pressed, pressedHover, pressTint       color.NRGBA
	sepBar, sepFoot                        color.NRGBA
	footHover, plus, plusHover             color.NRGBA
	border, borderFocus, line, lineFocus   color.NRGBA
	ph, hint, typing                       color.NRGBA
	send, sendHover, sendText, sendDiv     color.NRGBA
	sendDisIcon, sendDisDiv                color.NRGBA
	yellow, emojiInk                       color.NRGBA
	check, checkHover, checkMark           color.NRGBA
	checkBorder, checkBorderHover, checkBg color.NRGBA
	shadow                                 [3]color.NRGBA // the shadow rect, 1 and 2px out (crShadow)
}

// crFade is col at opacity a over an opaque under.
func crFade(col, under color.NRGBA, a float32) color.NRGBA {
	col.A = uint8(float32(col.A)*a + .5)
	return theme.Over(col, under)
}

func crMakePal(th *theme.Theme) crPal {
	c := &th.C
	ink := c.Fg // #1d1c1d in light, Slack's tints are this ink at an alpha
	ter := rgb(0x5e5d60)
	// .p-notification_bar__section: sk_foreground_max_solid, 97,96,97
	// light and 171,171,173 dark (slack-kit-gantry-v2-styles)
	typing := rgb(0x616061)
	if th.Dark {
		ter, typing = c.FgSubtle, rgb(0xababad)
	}
	var p crPal
	p.bg = c.Bg
	p.bar = c.BgInset // #f8f8f8
	p.fg = c.Fg
	p.icon = c.FgMuted // #454447
	p.iconDis = crFade(ter, p.bar, .3)
	if th.Dark {
		p.pressed = crFade(ink, p.bar, .13)
		p.footHover = crFade(ink, p.bg, .1)
	} else {
		p.pressed = rgb(0xeaeaea)
		p.footHover = rgb(0xeaeaea)
	}
	p.pressedHover = crFade(ink, p.bar, .2)
	p.pressTint = withAlpha(c.FgMuted, 0x21)
	p.sepBar = crFade(ter, p.bar, .13)
	p.sepFoot = crFade(ter, p.bg, .13)
	p.plus = crFade(ink, p.bg, .06)
	p.plusHover = crFade(ink, p.bg, .13)
	p.border = crFade(ink, p.bg, .13)
	p.borderFocus = crFade(ter, p.bg, .45)
	p.line = p.border
	p.lineFocus = crFade(ink, p.bg, .3)
	p.ph = ter
	p.hint = crFade(ink, p.bg, .7)
	p.typing = typing
	p.send = c.Send
	p.sendHover = rgb(0x148567)
	p.sendText = c.SendText
	p.sendDiv = crFade(c.SendText, c.Send, .4)
	p.sendDisIcon = crFade(ink, p.bg, .3)
	p.sendDisDiv = crFade(p.footHover, p.bg, .3)
	p.yellow = rgb(0xecb22e) // --dt_color-brand-core-slack-yellow
	// the hovered emoji button's icon: the button's hover colour
	// (content-pry) in light, .sk-client-theme--dark …--emoji:hover
	// {color: var(--dt_color-constants-black)} in dark
	p.emojiInk = rgb(0x1d1c1d)
	if th.Dark {
		p.emojiInk = rgb(0x000000)
	}
	// Chrome's native checkbox (Slack doesn't style this one).
	p.check, p.checkHover, p.checkMark = rgb(0x0075ff), rgb(0x005cc8), rgb(0xffffff)
	p.checkBorder, p.checkBorderHover, p.checkBg = rgb(0x767676), rgb(0x4f4f4f), rgb(0xffffff)
	if th.Dark {
		p.check, p.checkHover, p.checkMark = rgb(0x99c8ff), rgb(0xd1e6ff), rgb(0x3b3b3b)
		p.checkBorder, p.checkBorderHover, p.checkBg = rgb(0x858585), rgb(0xacacac), rgb(0x3b3b3b)
	}
	// 0 1px 3px #00000014 (.p-message_input__input_container_unstyled:
	// focus-within) as Chrome 157 draws it around a 1px-bordered box on
	// white: black at 12/255 on the shadow rect's own edge (the row under
	// the box), 7/255 one px out, 2/255 two px out, nothing beyond. Above
	// the box only the 2/255 row shows (the rect is 1px down, its first
	// ring is under the top border): 253 over white. Measured on a page
	// with just that box (composer-gap-light).
	p.shadow = [3]color.NRGBA{theme.Over(color.NRGBA{A: 12}, p.bg), theme.Over(color.NRGBA{A: 7}, p.bg), theme.Over(color.NRGBA{A: 2}, p.bg)}
	return p
}

// chrome is the composer's box and toolbars. The zero value is ready.
type chrome struct {
	btn   [crNumBtn]widget.Clickable
	tip   [crNumBtn]popups.TipState
	rect  [crNumBtn]image.Rectangle // last frame, composer-local
	shown uint32                    // buttons laid out this frame
	bg    gesture.Click

	acts, spare []chromeAction
	inputOff    image.Point
	fmtHidden   int // trailing formatting buttons folded into ⋯

	// TextyLengthWarning's hover (its tooltip) while over the limit.
	warn    widget.Clickable
	warnTip popups.TipState

	pal            crPal
	palOK, palDark bool
	palBg, palFg   color.NRGBA

	spKey float32
	spSh  *text.Shaper // the shaper spW was measured with
	spW   [3]int       // space advance at crSpaceSizes

	// Per-frame state for the Slack boundary natives (chrome_slack.go).
	cur     chromeState
	f       crFrame
	fpal    crPal       // this frame's colors: pal with CSS overrides
	base    image.Point // composer-local position of the current gtx origin
	footNat int         // the footer row's own width as last laid out (a row-flex body, layoutBox)
	sufW    int         // the submit group's width as last laid out (the toolbar's width, overflow.go)
	more    crMore      // TextyButtonOverflow's open state (overflow.go)
	pxPerDp float32
	wBox    layout.Widget
	wFoot   [crNumBtn]layout.Widget
	one     [1]*slackc.Node
	suf     []crSuf
	inlOne  [1]*slackc.Node // layoutInline's (it may run inside a row item)
	inl     []crSuf
}

var crSpaceSizes = [3]unit.Sp{15, 12, 10.5}

// Actions returns what happened since the last call and resets the queue.
// The slice is valid until the next call.
func (c *chrome) Actions() []chromeAction {
	if len(c.acts) == 0 {
		return nil
	}
	out := c.acts
	c.acts, c.spare = c.spare[:0], out
	return out
}

// InputOffset is the editor's top-left (text origin) relative to the
// composer's origin, from the last Layout.
func (c *chrome) InputOffset() image.Point { return c.inputOff }

func (c *chrome) emit(a chromeAction) { c.acts = append(c.acts, a) }

func (c *chrome) palette(th *theme.Theme) *crPal {
	if !c.palOK || c.palDark != th.Dark || c.palBg != th.C.Bg || c.palFg != th.C.Fg {
		c.pal = crMakePal(th)
		c.palOK, c.palDark, c.palBg, c.palFg = true, th.Dark, th.C.Bg, th.C.Fg
	}
	return &c.pal
}

// space returns the width of a space at crSpaceSizes[i] (Lato regular),
// measured once per scale: labels don't reliably include trailing spaces.
func (c *chrome) space(gtx layout.Context, sh *text.Shaper, i int) int {
	if c.spKey != gtx.Metric.PxPerSp || c.spSh != sh {
		c.spKey, c.spSh = gtx.Metric.PxPerSp, sh
		for j, sz := range crSpaceSizes {
			s := textStyle{Size: sz}
			_, a := crRec(gtx, sh, 1<<16, s, "x x")
			_, b := crRec(gtx, sh, 1<<16, s, "xx")
			c.spW[j] = max(a.Size.X-b.Size.X, 1)
		}
	}
	return c.spW[i]
}

// ---- state helpers ----

func (c *chrome) disabled(st *chromeState, id crBtn) bool {
	switch id {
	case crBold:
		// shouldDisableButton: no bold in a heading
		return st.InCode || isHeading(st.Block)
	case crItalic, crUnderline, crStrike, crCode:
		// TextyFormattingBar's disabledFormats: in a heading the other
		// emphasis formats too, unless rich_text_headers_and_dividers
		return st.InCode || isHeading(st.Block) && !st.HeadingFormats
	case crLink, crDivider, crPara:
		// in a code block only the lists, quote and code block stay
		// ("menu", the ¶ trigger, included)
		return st.InCode
	case crSend, crSchedule:
		// isSendingDisabledForTextState: nothing to send, or over the
		// 12,000 limit (isOverMaxLength)
		return st.sendOff()
	case crShortcut:
		// SlashCommandsButton isDisabled: !isTextyInputEmpty
		return !st.InputEmpty
	}
	return false
}

func crPressed(st *chromeState, id crBtn) bool {
	switch id {
	case crBold, crItalic, crUnderline, crStrike, crCode:
		return !st.InCode && st.Active&crInfo[id].style != 0
	case crLink:
		return !st.InCode && st.Link
	case crOrdered, crBullet, crQuote, crCodeBlock:
		return st.Block == crInfo[id].block
	}
	return false
}

func (c *chrome) actionFor(id crBtn, anchor image.Rectangle) chromeAction {
	inf := &crInfo[id]
	return chromeAction{Kind: inf.kind, Style: inf.style, Block: inf.block, Anchor: anchor}
}

// update turns last frame's pointer input into actions. It runs before
// anything is laid out: Clickable.Layout drops unread clicks.
func (c *chrome) update(gtx layout.Context, h Host, st *chromeState) {
	c.moreOutside(gtx, st) // overflow.go
	c.more.shown = false
	for {
		e, ok := c.bg.Update(gtx.Source)
		if !ok {
			break
		}
		if e.Kind == gesture.KindPress {
			c.emit(chromeAction{Kind: caFocus})
		}
	}
	for id := crBtn(0); id < crNumBtn; id++ {
		if !c.btn[id].Clicked(gtx) || c.disabled(st, id) {
			continue
		}
		switch id {
		case crFmtMore:
			c.openMore(h, st, id)
		case crFootMore:
			c.toggleMore() // TextyButtonOverflow's toggle (overflow.go)
		case crMention, crShortcut:
			// MentionButton's and SlashCommandsButton's handlers close
			// the overflow (eH.current.close())
			c.more.open = false
			c.emit(c.actionFor(id, c.rect[id]))
		default:
			c.emit(c.actionFor(id, c.rect[id]))
		}
	}
}

// openMore shows the formatting bar's ⋯ menu; its items queue the same
// actions the hidden buttons would. (The footer's ⋯ is TextyButtonOverflow's
// row of buttons, overflow.go.)
func (c *chrome) openMore(h Host, st *chromeState, id crBtn) {
	anchor := c.rect[id]
	ids := crFmtOrder[len(crFmtOrder)-c.fmtHidden:]
	if len(ids) == 0 {
		return
	}
	items := make([]popups.MenuItem, 0, len(ids))
	for _, b := range ids {
		inf := &crInfo[b]
		items = append(items, popups.MenuItem{
			Label:    inf.tip,
			Icon:     inf.menuIcon,
			Hint:     shortcut.Format(inf.keys),
			Disabled: c.disabled(st, b),
			Do: func() {
				c.emit(c.actionFor(b, anchor))
				h.Invalidate()
			},
		})
	}
	h.ShowOverlay(popups.NewMenuOpts(h, items, popups.MenuOpts{Width: 248, Anchor: anchor.Add(st.Origin), Side: popups.Above}))
}

// ---- layout ----

// prime sets the state the boundary props read before Layout runs (the
// composer builds MessageInput's props, which nest the chrome's, first).
func (c *chrome) prime(st *chromeState) { c.cur = *st }

// Layout draws the composer chrome around input (the editor) and
// attachments, and returns the full size: for channel/thread composers the
// side margins and the hint bar included, for edit composers just the box.
// inputP is MessageInput's props under a patch (nil otherwise).
func (c *chrome) Layout(gtx layout.Context, h Host, st chromeState, input, attachments layout.Widget, inputP slackc.Props) layout.Dimensions {
	th := h.Theme()
	p := c.palette(th)
	c.update(gtx, h, &st)
	c.shown = 0
	c.cur = st
	c.fpal = *p
	c.pxPerDp = gtx.Metric.PxPerDp
	c.f = crFrame{h: h, inputP: inputP, input: input, att: attachments, barBg: p.bar, hide: st.Hide, minOverflow: crOverflowDefault}

	edit := st.Mode == modeEdit
	W := gtx.Constraints.Max.X
	margin := 0
	switch st.Mode {
	case modeChannel:
		margin = gtx.Dp(20)
	case modeThread:
		margin = gtx.Dp(16)
	}
	hintH := 0
	var editWarn op.CallOp
	var editWarnD layout.Dimensions
	switch {
	case !edit:
		hintH = gtx.Dp(24)
	case st.OverLimit > 0:
		// the edit's .c-message__editor__footer: "text is too long!"
		// (.c-message__editor__warning: --dt_color-content-imp, .8rem,
		// margin-top 8px, right-aligned in __messages)
		col, ok := cssVarColor(css.SlackVars(th.Dark), "--dt_color-content-imp")
		if !ok {
			col = rgb(0xc01343)
		}
		editWarn, editWarnD = crRec(gtx, th.Shaper, W, textStyle{Size: 12.8, Color: col, MaxLines: 1}, "text is too long!")
		hintH = gtx.Dp(8) + editWarnD.Size.Y
	}
	c.f.W, c.f.maxY, c.f.margin, c.f.hintH = W, gtx.Constraints.Max.Y, margin, hintH

	// The thread pane's .p-threads_footer__input_container top margin is
	// the composer's (Composer.layoutPaneBody: 12px, 4px under the app
	// nugget, 0 under a context bar or in the Threads view), not the
	// chrome's: the box starts at the chrome's top. Adding it here too put
	// 24px between the last reply and the box (Karim, 2026-11-12).
	bg := gtx
	bg.Constraints = layout.Constraints{Max: image.Pt(max(W-2*margin, 0), gtx.Constraints.Max.Y)}
	t := op.Offset(image.Pt(margin, 0)).Push(gtx.Ops)
	var bd layout.Dimensions
	if e, patched := kid(inputP, "WysiwygContainer"); patched {
		bd = slackc.LayoutElement(bg, e)
	} else {
		bd = slackc.Component(bg, "WysiwygContainer", c, (*chrome).wysiwygProps, (*chrome).layoutWysiwyg)
	}
	t.Pop()
	boxH := max(bd.Size.Y, 0)
	box := image.Rect(margin, 0, margin+bd.Size.X, boxH)
	if c.f.boxOK {
		box.Min.X, box.Max.X = c.f.box.Min.X, c.f.box.Max.X
	}
	switch {
	case edit && hintH > 0:
		t := op.Offset(image.Pt(box.Max.X-editWarnD.Size.X, box.Max.Y+gtx.Dp(8))).Push(gtx.Ops)
		editWarn.Add(gtx.Ops)
		t.Pop()
	case hintH > 0:
		c.layoutHint(gtx, th, &c.fpal, &c.cur, box)
	}

	// Buttons that weren't drawn can't receive the Leave that ends their
	// hover; reset them so a tooltip doesn't pop when they come back.
	for id := crBtn(0); id < crNumBtn; id++ {
		if c.shown&(1<<id) == 0 {
			c.rect[id] = image.Rectangle{}
			if c.btn[id].Hovered() || c.btn[id].Pressed() {
				c.btn[id] = widget.Clickable{}
			}
		}
	}
	return layout.Dimensions{Size: image.Pt(W, boxH+hintH)}
}

// layoutBox draws the bordered box (WysiwygContainer's native) in
// composer-local coordinates and returns its size.
func (c *chrome) layoutBox(gtx layout.Context) layout.Dimensions {
	th := c.f.h.Theme()
	p := &c.fpal
	st := &c.cur
	edit := st.Mode == modeEdit
	W, margin := c.f.W, c.f.margin
	bw := max(gtx.Dp(1), 1)
	rad := gtx.Dp(8)
	if edit {
		rad = gtx.Dp(4)
	}
	box := image.Rect(margin, 0, max(W-margin, margin+2*bw+2), 0)
	x0, x1 := box.Min.X+bw, box.Max.X-bw
	innerW := x1 - x0

	body := crBodyClass.Enter(gtx, 0, 0)
	defer body.Leave()
	barH := 0
	if st.ShowFormatting {
		fs := crFmtClass.Enter(gtx, 0, 0)
		hidden := fs.Hidden()
		c.f.barBg = fs.Background(p.bar)
		fs.Leave()
		if !hidden {
			barH = gtx.Dp(38)
		}
	}
	padX, padY := gtx.Dp(12), gtx.Dp(8)
	footH := gtx.Dp(40)
	var fmods uint64
	if st.ShowFormatting {
		fmods = 1
	}
	if fs := crFootClass.Enter(gtx, 0, fmods); fs.Hidden() {
		footH = 0
		fs.Leave()
	} else {
		fs.Leave()
	}
	bcH := 0
	if st.Mode == modeThread && !st.NoBroadcast {
		bs := crBroadcastClass.Enter(gtx, 0, 0)
		hidden := bs.Hidden()
		bs.Leave()
		if !hidden {
			bcH = gtx.Dp(26)
		}
	}
	hintH := c.f.hintH

	// A row-flex body (SlimMessageBox's Discord-style layout:
	// .c-basic_container__body {display: flex; flex-direction: row;
	// flex-wrap: wrap; align-items: flex-end; column-gap: 6px}) puts the
	// footer, at its own width (flex: 1 1 auto), beside the editor
	// (min-width: min(450px, 100%)) when both fit, bottom-aligned with it;
	// else the footer wraps to its own row as without it, and so does a
	// footer whose flex-basis is 100%. The formatting bar, attachments and
	// broadcast controls keep their rows. The footer's width is the one it
	// had last frame (footNat). Clack's natives answer :has() with false and
	// @container queries always apply, so attachments don't put the footer
	// back on its own row as they do in Slack, and only the editor's
	// min-width decides when it wraps.
	oneLine, gap, footW := false, 0, 0
	if bst := body.Style(); footH > 0 && body.Active() && bst != nil && bst.Display == css.DisplayFlex && (bst.FlexDirection == css.Row || bst.FlexDirection == css.RowReverse) {
		px := max(gtx.Metric.PxPerDp, 0.01)
		base, vw, vh := float32(innerW)/px, float32(gtx.Constraints.Max.X)/px, float32(gtx.Constraints.Max.Y)/px
		gap = int(bst.ColGap.Resolve(base, vw, vh, 0) * px)
		ownRow := false
		if fst := body.Child(crFootClass, fmods).Style(); fst != nil {
			ownRow = fst.FlexBasis.Kind == css.LenFixed && fst.FlexBasis.Pct >= 100
		}
		minEd := 0
		if tst := body.Child(crTextyClass, 0).Style(); tst != nil {
			minEd = int(tst.MinWidth.Resolve(base, vw, vh, 0) * px)
		}
		footW = min(c.footNat, innerW)
		oneLine = !ownRow && footW > 0 && innerW-gap-footW >= max(minEd, 2*padX+1)
	}

	// The editor first (recorded) to know the box height.
	tw := max(innerW-2*padX, 1)
	fixed := 2*bw + barH + 2*padY + bcH + footH + hintH
	if oneLine {
		tw = max(innerW-gap-footW-2*padX, 1)
		fixed -= footH
	}
	ig := gtx
	ig.Constraints = layout.Constraints{Max: image.Pt(tw, max(c.f.maxY-fixed, gtx.Dp(22)))}
	rec := op.Record(gtx.Ops)
	var ed layout.Dimensions
	if e, patched := kid(c.f.wysP, "texty_input_with_autocomplete"); patched {
		ed = slackc.LayoutElement(ig, e)
	} else {
		ed = slackc.Component(ig, "texty_input_with_autocomplete", c, (*chrome).textyProps, (*chrome).layoutTexty)
	}
	inCall := rec.Stop()
	textTop := bw + barH
	textH := max(ed.Size.Y+2*padY, gtx.Dp(38))
	rowH, edX1 := textH, x1
	if oneLine {
		rowH, edX1 = max(textH, footH), x1-gap-footW
	}

	attTop := textTop + rowH
	ah := 0
	var attCall op.CallOp
	if c.f.att != nil {
		ag := gtx
		ag.Constraints = layout.Constraints{Max: image.Pt(innerW, max(c.f.maxY-fixed-textH, gtx.Dp(120)))}
		rec = op.Record(gtx.Ops)
		as := crAttClass[st.Mode].Enter(gtx, 0, 0)
		ad := c.f.att(ag)
		as.Leave()
		attCall = rec.Stop()
		ah = ad.Size.Y
	}
	bcTop := attTop + ah
	footTop := bcTop + bcH
	box.Max.Y = footTop + footH + bw
	if oneLine {
		footTop = textTop + rowH - footH // align-items: flex-end
		box.Max.Y = bcTop + bcH + bw
	}
	c.f.box, c.f.boxOK = box, true
	c.f.rowH, c.f.textH, c.f.attH = rowH, textH, ah

	// Box: shadow, border, fill, formatting bar background. Under a
	// plugin's context bar the top corners are square.
	if st.Focused && !edit {
		crShadow(gtx, box, rad, p)
	}
	border := p.border
	if st.Focused || edit {
		border = p.borderFocus
	}
	ir := max(rad-bw, 0)
	if st.SquareTop {
		paint.FillShape(gtx.Ops, border, clip.RRect{Rect: box, SE: rad, SW: rad}.Op(gtx.Ops))
		paint.FillShape(gtx.Ops, p.bg, clip.RRect{Rect: box.Inset(bw), SE: ir, SW: ir}.Op(gtx.Ops))
	} else {
		fillRRect(gtx, box, rad, border)
		fillRRect(gtx, box.Inset(bw), ir, p.bg)
	}
	if barH > 0 {
		r := image.Rect(x0, bw, x1, bw+barH)
		tr := ir
		if st.SquareTop {
			tr = 0
		}
		paint.FillShape(gtx.Ops, c.f.barBg, clip.RRect{Rect: r, NW: tr, NE: tr}.Op(gtx.Ops))
	}

	// A press anywhere on the box that no button or the editor takes
	// focuses the editor; the text area padding gets the text cursor.
	area := clip.Rect(box).Push(gtx.Ops)
	c.bg.Add(gtx.Ops)
	ta := clip.Rect(image.Rect(x0, textTop, edX1, attTop)).Push(gtx.Ops)
	pointer.CursorText.Add(gtx.Ops)
	ta.Pop()
	area.Pop()

	// Text area (at the bottom of a taller one-line row: flex-end).
	org := image.Pt(x0+padX, textTop+rowH-textH+padY)
	c.inputOff = org
	if st.ShowPlaceholder && st.Placeholder != "" {
		c.placeholder(gtx, th, p, st, org, tw)
	}
	t := op.Offset(org).Push(gtx.Ops)
	inCall.Add(gtx.Ops)
	t.Pop()
	if st.ScrolledTop {
		fillRect(gtx, image.Rect(x0, textTop, x1, textTop+bw), p.line)
	}
	if ah > 0 {
		t = op.Offset(image.Pt(x0, attTop)).Push(gtx.Ops)
		attCall.Add(gtx.Ops)
		t.Pop()
	}
	if st.ScrolledBottom {
		col := p.line
		if st.Focused {
			col = p.lineFocus
		}
		if oneLine {
			fillRect(gtx, image.Rect(x0, attTop-bw, edX1, attTop), col)
		} else {
			fillRect(gtx, image.Rect(x0, footTop-bw, x1, footTop), col)
		}
	}

	if barH > 0 {
		fs := crFmtClass.Enter(gtx, 0, 0)
		c.layoutBar(gtx, th, p, st, x0, innerW, bw)
		fs.Leave()
	} else {
		c.fmtHidden = 0
	}
	if bcH > 0 {
		c.layoutBroadcast(gtx, th, p, st, x0, x1, bcTop)
	}
	if footH > 0 {
		fs := crFootClass.Enter(gtx, 0, fmods)
		c.f.oneLine = oneLine
		if oneLine {
			c.layoutFooter(gtx, x1-footW, x1, footTop)
		} else {
			c.layoutFooter(gtx, x0, x1, footTop)
		}
		fs.Leave()
	}
	if st.OverLimit > 0 {
		c.lengthWarning(gtx, th, st, image.Pt(x1, box.Max.Y-bw))
	}
	return layout.Dimensions{Size: image.Pt(box.Dx(), box.Max.Y)}
}

// lengthWarning draws TextyLengthWarning: "-N" in .c-texty_input_unstyled
// __warning (12px/18px, padding 0 4px, radius 2px, background
// --dt_color-base-inv-imp, text --sk_inverted_foreground), placed by
// `.c-wysiwyg_container .c-texty_input_unstyled__warning {bottom: 44px;
// right: 4px}` against the box's inner bottom-right corner (br), with
// its tooltip "Your message is N characters too long".
func (c *chrome) lengthWarning(gtx layout.Context, th *theme.Theme, st *chromeState, br image.Point) {
	vars := css.SlackVars(th.Dark)
	bg, ok := cssVarColor(vars, "--dt_color-base-inv-imp")
	if !ok {
		bg = rgb(0xe01e5a)
	}
	fg := rgb(0xffffff)
	if v, ok := vars["--sk_inverted_foreground"]; ok {
		if col, ok := crRGBTriple(v); ok {
			fg = col
		}
	}
	s := textStyle{Size: 12, LineHeight: 18, Color: fg, MaxLines: 1}
	call, d := crRec(gtx, th.Shaper, 1<<16, s, lengthWarningText(st.OverLimit))
	padX := gtx.Dp(4)
	sz := image.Pt(d.Size.X+2*padX, max(d.Size.Y, gtx.Dp(18)))
	r := image.Rectangle{Min: image.Pt(br.X-gtx.Dp(4)-sz.X, br.Y-gtx.Dp(44)-sz.Y)}
	r.Max = r.Min.Add(sz)
	fillRRect(gtx, r, gtx.Dp(2), bg)
	t := op.Offset(image.Pt(r.Min.X+padX, r.Min.Y+(sz.Y-d.Size.Y)/2)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	t = op.Offset(r.Min).Push(gtx.Ops)
	g := gtx
	g.Constraints = layout.Exact(sz)
	c.warn.Layout(g, crPlainArea) // cursor: default
	popups.Tooltip(gtx, th, &c.warnTip, c.warn.Hovered(), sz, popups.Above, lengthWarningTip(st.OverLimit), "")
	t.Pop()
}

// crRGBTriple parses a Slack "r, g, b" custom property value.
func crRGBTriple(v string) (color.NRGBA, bool) {
	parts := strings.Split(v, ",")
	if len(parts) != 3 {
		return color.NRGBA{}, false
	}
	var rgbv [3]uint8
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 0 || n > 255 {
			return color.NRGBA{}, false
		}
		rgbv[i] = uint8(n)
	}
	return color.NRGBA{R: rgbv[0], G: rgbv[1], B: rgbv[2], A: 0xff}, true
}

// crShadow paints a small drop shadow (0 1px 3px) around r, as opaque rings
// pre-composited over the pane background: r moved down 1dp, then grown
// by 0, 1 and 2dp (p.shadow, Chrome's values). r is painted over it.
func crShadow(gtx layout.Context, r image.Rectangle, rad int, p *crPal) {
	dy := gtx.Dp(1)
	for i := len(p.shadow) - 1; i >= 0; i-- {
		g := gtx.Dp(unit.Dp(i))
		rr := image.Rect(r.Min.X-g, r.Min.Y-g+dy, r.Max.X+g, r.Max.Y+g+dy)
		fillRRect(gtx, rr, rad+g, p.shadow[i])
	}
}

// hit lays out the click area of button id at r (composer-local).
func (c *chrome) hit(gtx layout.Context, id crBtn, r image.Rectangle, enabled bool) {
	c.rect[id] = r.Add(c.base)
	c.shown |= 1 << id
	t := op.Offset(r.Min).Push(gtx.Ops)
	g := gtx
	g.Constraints = layout.Exact(r.Size())
	if enabled {
		c.btn[id].Layout(g, crPointerArea)
	} else {
		c.btn[id].Layout(g, crPlainArea)
	}
	t.Pop()
}

func crPointerArea(gtx layout.Context) layout.Dimensions {
	pointer.CursorPointer.Add(gtx.Ops)
	return layout.Dimensions{Size: gtx.Constraints.Min}
}

func crPlainArea(gtx layout.Context) layout.Dimensions {
	return layout.Dimensions{Size: gtx.Constraints.Min}
}

// tooltip shows button id's Slack tooltip above it while hovered.
func (c *chrome) tooltip(gtx layout.Context, th *theme.Theme, st *chromeState, id crBtn, r image.Rectangle) {
	label, keys := crInfo[id].tip, crInfo[id].keys
	if id == crAa {
		label = "Show formatting"
		if st.ShowFormatting {
			label = "Hide formatting"
		}
	}
	if id == crEmoji && c.f.h != nil && gifPickerOn(c.f.h.Core()) {
		// EmojiToolbarButton: emoji_button "Add emoji and GIFs" when the
		// GIF picker is on (XC3J BH && enableGifPicker), else "Emoji"
		label = emojiButtonNS.T("Add emoji and GIFs")
	} else if id == crEmoji {
		label = emojiButtonNS.T("Emoji")
	}
	if label == "" {
		return
	}
	t := op.Offset(r.Min).Push(gtx.Ops)
	popups.Tooltip(gtx, th, &c.tip[id], c.btn[id].Hovered(), r.Size(), popups.Above, label, keys)
	t.Pop()
}

// icon draws button id's glyph centred in r.
func (c *chrome) icon(gtx layout.Context, st *chromeState, id crBtn, r image.Rectangle, col color.NRGBA) {
	is := gtx.Dp(18)
	if id == crPara {
		// ¶ (18) at padding 4, then the 20px c-menu_item__icon at
		// margin-left -2 (x 20, y 4), its caret-down svg 20px too.
		t := op.Offset(r.Min.Add(image.Pt(gtx.Dp(4), (r.Dy()-is)/2))).Push(gtx.Ops)
		crParaIcon(st.Block)(gtx, 18, col)
		t.Pop()
		cs := gtx.Dp(20)
		t = op.Offset(r.Min.Add(image.Pt(gtx.Dp(20), (r.Dy()-cs)/2))).Push(gtx.Ops)
		crIcChevron(gtx, 20, col)
		t.Pop()
		return
	}
	t := op.Offset(r.Min.Add(image.Pt((r.Dx()-is)/2, (r.Dy()-is)/2))).Push(gtx.Ops)
	switch id {
	case crAa:
		crIcAa(gtx, 18, col) // Slack's formatting glyph
	default:
		if f := crInfo[id].icon; f != nil {
			f(gtx, 18, col)
		}
	}
	t.Pop()
	if id == crAa && st.ShowFormatting {
		crAaUnderline(gtx, r, col)
	}
}

// crAaUnderline is the Aa button's c-wysiwyg_container__button--
// composer_active:after while the formatting bar shows: currentColor,
// 75% of the button wide (21px), 1.5px tall, fully rounded, its bottom 4px
// above the button's, centred.
func crAaUnderline(gtx layout.Context, r image.Rectangle, col color.NRGBA) {
	k := gtx.Metric.PxPerDp
	w, h := float32(r.Dx())*0.75, 1.5*k
	x0 := float32(r.Min.X) + (float32(r.Dx())-w)/2
	y := float32(r.Max.Y) - 4*k - h/2
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(f32.Pt(x0+h/2, y))
	p.LineTo(f32.Pt(x0+w-h/2, y))
	paint.FillShape(gtx.Ops, col, clip.Stroke{Path: p.End(), Width: h}.Op())
}

// ---- formatting bar ----

// crBar is the formatting bar's spacing in pixels.
type crBar struct {
	pad, btn, para, m, sepW, sepM int
	slot                          int // Slack's 36px per button when deciding the overflow
}

func crBarOf(gtx layout.Context) crBar {
	return crBar{pad: gtx.Dp(4), btn: gtx.Dp(28), para: gtx.Dp(44), m: gtx.Dp(2), sepW: max(gtx.Dp(1), 1), sepM: gtx.Dp(4), slot: gtx.Dp(36)}
}

// width is the bar's content width with the last k buttons folded into ⋯.
// Buttons have 2px side margins (none outside the first and last),
// separators 4px; the bar has 4px padding.
func (b crBar) width(k int) int {
	n := len(crFmtOrder) - k
	x := b.pad - b.m
	for i := 0; i < n; i++ {
		if crFmtSep[i] {
			x += 2*b.sepM + b.sepW
		}
		w := b.btn
		if crFmtOrder[i] == crPara {
			w = b.para
		}
		x += 2*b.m + w
	}
	if k > 0 {
		x += 2*b.m + b.btn
	}
	return x - b.m + b.pad
}

// hidden is how many trailing buttons fold into ⋯ in a box avail wide
// inside its border. Slack's sticky Composer (module bpQI) counts slots,
// not pixels: getAvailableButtonSlots is floor((width - trailingWidth) /
// 36), width being the bar's AutoSizer width (the box less the formatting
// row's 4px padding each side; nothing trails it there). Every button
// shows while the slots hold them all (a bar of 432px: a box of 442 with
// its border); otherwise getReducedButtonGroups keeps the first slots-1
// and ⋯ takes the last slot, down to ⋯ alone.
func (b crBar) hidden(avail int) int {
	n := len(crFmtOrder)
	slot := max(b.slot, 1)
	slots := (avail - 2*b.pad) / slot
	if slots >= n {
		return 0
	}
	return n - max(slots-1, 0)
}

func (c *chrome) layoutBar(gtx layout.Context, th *theme.Theme, p *crPal, st *chromeState, x0, innerW, top int) {
	b := crBarOf(gtx)
	k := b.hidden(innerW)
	c.fmtHidden = k
	y := top + gtx.Dp(6)
	sy0, sy1 := top+gtx.Dp(9), top+gtx.Dp(29)
	x := x0 + b.pad - b.m
	for i, n := 0, len(crFmtOrder)-k; i < n; i++ {
		id := crFmtOrder[i]
		if crFmtSep[i] {
			x += b.sepM
			fillRect(gtx, image.Rect(x, sy0, x+b.sepW, sy1), p.sepBar)
			x += b.sepW + b.sepM
		}
		w := b.btn
		if id == crPara {
			w = b.para
		}
		x += b.m
		c.fmtButton(gtx, th, p, st, id, image.Rect(x, y, x+w, y+b.btn))
		x += w + b.m
	}
	if k > 0 {
		x += b.m
		c.fmtButton(gtx, th, p, st, crFmtMore, image.Rect(x, y, x+b.btn, y+b.btn))
	}
}

// fmtButton draws a formatting-bar button: #454447 icons, no hover tile,
// pressed #eaeaea + #1d1c1d, disabled #5e5d60 at .3, and the whole button
// at opacity .3 while the composer is unfocused (unless hovered).
func (c *chrome) fmtButton(gtx layout.Context, th *theme.Theme, p *crPal, st *chromeState, id crBtn, r image.Rectangle) {
	dis := c.disabled(st, id)
	hov := c.btn[id].Hovered()
	var bg color.NRGBA
	ic := p.icon
	switch {
	case dis:
		ic = p.iconDis
	case crPressed(st, id):
		bg, ic = p.pressed, p.fg
		if hov {
			bg = p.pressedHover
		}
	}
	if !dis && c.btn[id].Pressed() {
		if bg.A == 0 {
			bg = p.bar
		}
		bg = theme.Over(p.pressTint, bg)
	}
	if !st.Focused && !hov && !dis {
		under := p.bar
		if bg.A != 0 {
			bg = crFade(bg, p.bar, .3)
			under = bg
		}
		ic = crFade(ic, under, .3)
	}
	if bg.A != 0 {
		fillRRect(gtx, r, gtx.Dp(4), bg)
	}
	c.icon(gtx, st, id, r, ic)
	c.hit(gtx, id, r, !dis)
	c.tooltip(gtx, th, st, id, r)
}

// ---- footer ----

// layoutFooter lays out the footer: TextyButtons (the toolbar and the
// submit group, chrome_slack.go) and CharacterCount left of the send
// group.
func (c *chrome) layoutFooter(gtx layout.Context, x0, x1, top int) {
	bs := gtx.Dp(28)
	y := top + (gtx.Dp(40)-bs)/2
	c.f.x0, c.f.x1 = x0, x1
	// beside the editor the footer is as wide as its buttons: Slack's
	// overflow test sees the display: contents toolbar as 0 wide there
	// and folds nothing
	c.f.eq = 0
	if !c.f.oneLine {
		c.f.eq = c.toolbarWidth(gtx, x1-x0)
	}
	c.f.fold = c.foldFooter()
	c.f.sufX = x1 - gtx.Dp(6)
	org := image.Pt(x0, y)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(max(x1-x0, 0), bs)}
	t := op.Offset(org).Push(gtx.Ops)
	c.base = org
	if e, patched := kid(c.f.wysP, "TextyButtons"); patched {
		slackc.LayoutElement(g, e)
	} else {
		slackc.Component(g, "TextyButtons", c, (*chrome).textyButtonsProps, (*chrome).layoutTextyButtons)
	}
	c.base = image.Point{}
	t.Pop()

	rec := op.Record(gtx.Ops)
	var d layout.Dimensions
	if e, patched := kid(c.f.wysP, "CharacterCount"); patched {
		d = slackc.LayoutElement(g, e)
	} else {
		d = slackc.Component(g, "CharacterCount", c, (*chrome).charCountProps, (*chrome).layoutCharCount)
	}
	call := rec.Stop()
	if d.Size.X > 0 {
		t = op.Offset(image.Pt(c.f.sufX-gtx.Dp(8)-d.Size.X, y+(bs-d.Size.Y)/2)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		t.Pop()
	}
}

// footButton: #454447 icon; hover #eaeaea tile (radius 4) + #1d1c1d icon.
// The emoji button swaps to the yellow-disc face on hover.
func (c *chrome) footButton(gtx layout.Context, th *theme.Theme, p *crPal, st *chromeState, id crBtn, r image.Rectangle) {
	hov := c.btn[id].Hovered()
	dis := c.disabled(st, id)
	ic := p.icon
	switch {
	case dis:
		// .c-wysiwyg_container__button--workflows.…--disabled: no hover
		// tile, rgba(sk_foreground_max, .7) at opacity .3, default
		// cursor; the tooltip still shows (pointer-events: auto)
		ic = crFade(p.fg, p.bg, .7*.3)
	case hov:
		fillRRect(gtx, r, gtx.Dp(4), c.f.btn.Background(p.footHover))
		ic = p.fg
	}
	ic = c.f.btn.Color(ic)
	if id == crEmoji && hov {
		// EmojiToolbarButton (boot-shared FLB+): on hover the :after disc
		// (1em of the 18px icon font, slack-yellow or the skin tone's
		// colour) and .is-second's emoji-filled at scale(1.05)
		// rotate(15deg), with no transition
		is := gtx.Dp(18)
		t := op.Offset(r.Min.Add(image.Pt((r.Dx()-is)/2, (r.Dy()-is)/2))).Push(gtx.Ops)
		crIconEmojiHover(gtx, is, crEmojiDisc(c.skinTone(), p.yellow), p.emojiInk)
		t.Pop()
	} else {
		c.icon(gtx, st, id, r, ic)
	}
	c.hit(gtx, id, r, !dis)
	c.tooltip(gtx, th, st, id, r)
}

// crEmojiDisc is the hovered emoji button's disc: .c-wysiwyg_container__
// button--emoji.skin-tone-N:after for preferred_skin_tone 2–6, else
// slack-yellow.
func crEmojiDisc(tone string, yellow color.NRGBA) color.NRGBA {
	switch tone {
	case "2":
		return rgb(0xffdbbe)
	case "3":
		return rgb(0xe6b892)
	case "4":
		return rgb(0xd4a37c)
	case "5":
		return rgb(0xc18e65)
	case "6":
		return rgb(0xb47f55)
	}
	return yellow
}

// skinTone is preferred_skin_tone ("" when unset or offline).
func (c *chrome) skinTone() string {
	if c.f.h == nil {
		return ""
	}
	a := c.f.h.Core()
	if a == nil {
		return ""
	}
	return a.Prefs().SkinTone
}

// attachButton is the round + (rgba(29,28,29,.06), hover .13 + dark icon).
func (c *chrome) attachButton(gtx layout.Context, th *theme.Theme, p *crPal, st *chromeState, r image.Rectangle) {
	hov := c.btn[crAttach].Hovered()
	bg, ic := p.plus, p.icon
	if hov {
		bg, ic = p.plusHover, p.fg
	}
	bg, ic = c.f.btn.Background(bg), c.f.btn.Color(ic)
	fillRRect(gtx, r, r.Dx()/2, bg)
	// Slack's plus svg at 18px, centred (the 28px button's 5px padding).
	is := gtx.Dp(18)
	t := op.Offset(r.Min.Add(image.Pt((r.Dx()-is)/2, (r.Dy()-is)/2))).Push(gtx.Ops)
	crInfo[crAttach].icon(gtx, 18, ic)
	t.Pop()
	c.hit(gtx, crAttach, r, true)
	c.tooltip(gtx, th, st, crAttach, r)
}

// layoutSend draws the 60×28 split button (send 32 + chevron 28; 32×28
// when a plugin turns scheduling off) at the gtx origin. Disabled:
// transparent at .3; enabled #007a5a (--TextyTheme-send-bg) with the
// hovered half #148567 and a small shadow.
func (c *chrome) layoutSend(gtx layout.Context, p *crPal, st *chromeState) layout.Dimensions {
	h, sw, cw := gtx.Dp(28), gtx.Dp(32), gtx.Dp(28)
	sched := !c.hidden(crSchedule)
	if !sched {
		cw = 0
	}
	en := !st.sendOff()
	hs, hc := c.btn[crSend].Hovered(), sched && c.btn[crSchedule].Hovered()
	var cs css.State
	if !en {
		cs |= css.StateDisabled
	}
	if hs || hc {
		cs |= css.StateHover
	}
	s := crSendClass.Enter(gtx, cs, 0)
	defer s.Leave()
	if s.Hidden() {
		return layout.Dimensions{}
	}
	r := image.Rect(0, 0, sw+cw, h)
	rs := image.Rect(0, 0, sw, h)
	rc := image.Rect(sw, 0, sw+cw, h)
	rad := gtx.Dp(4)
	icol, div := p.sendDisIcon, p.sendDisDiv
	if en {
		if hs || hc {
			crShadow(gtx, r, rad, p)
		}
		fillRRect(gtx, r, rad, s.Background(p.send))
		if hs {
			if sched {
				paint.FillShape(gtx.Ops, p.sendHover, clip.RRect{Rect: rs, NW: rad, SW: rad}.Op(gtx.Ops))
			} else {
				fillRRect(gtx, rs, rad, p.sendHover)
			}
		}
		if hc {
			paint.FillShape(gtx.Ops, p.sendHover, clip.RRect{Rect: rc, NE: rad, SE: rad}.Op(gtx.Ops))
		}
		icol, div = s.Color(p.sendText), p.sendDiv
	}
	is := gtx.Dp(16)
	if sched {
		dx := rs.Min.X + gtx.Dp(31)
		fillRect(gtx, image.Rect(dx, gtx.Dp(4), dx+max(gtx.Dp(1), 1), gtx.Dp(24)), div)
	}
	t := op.Offset(image.Pt(rs.Min.X+gtx.Dp(8), (h-is)/2)).Push(gtx.Ops)
	crIconSend(gtx, is, icol)
	t.Pop()
	if sched {
		t = op.Offset(image.Pt(rc.Min.X+(cw-is)/2, (h-is)/2)).Push(gtx.Ops)
		crIcChevron(gtx, 16, icol)
		t.Pop()
	}
	c.hit(gtx, crSend, rs, en)
	if sched {
		c.hit(gtx, crSchedule, rc, en)
	}
	if en { // disabled, the halves take no pointer (pointer-events: none)
		// SendButton's tooltips: "Send now" with its key (Return, or
		// CmdOrCtrl+Return under "Enter starts a new line") and "Schedule
		// for later" on the chevron
		th := c.f.h.Theme()
		keys := "Enter"
		if uipref.Get().SendOnModEnter {
			keys = "Mod+Enter"
		}
		popups.Tooltip(gtx, th, &c.tip[crSend], hs, rs.Size(), popups.Above, draftsNS.T("Send now"), keys) // ScheduleSendMenu's
		if sched {
			t := op.Offset(rc.Min).Push(gtx.Ops)
			popups.Tooltip(gtx, th, &c.tip[crSchedule], hc, rc.Size(), popups.Above, draftsNS.T("Schedule for later"), "")
			t.Pop()
		}
	}
	return layout.Dimensions{Size: r.Size()}
}

// layoutEditButtons draws Cancel (outline) and Save (primary), small,
// 8px apart, at the gtx origin (the row right-aligns them 6px inside the
// box).
func (c *chrome) layoutEditButtons(gtx layout.Context, th *theme.Theme, st *chromeState) layout.Dimensions {
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(max(c.f.x1, 0), gtx.Dp(28))}
	rec := op.Record(gtx.Ops)
	ds := popups.Button(g, th, &c.btn[crSave], popups.BtnPrimary, popups.BtnSmall, "Save", "", 0)
	save := rec.Stop()
	rec = op.Record(gtx.Ops)
	dc := popups.Button(g, th, &c.btn[crCancel], popups.BtnOutline, popups.BtnSmall, "Cancel", "", 0)
	cancel := rec.Stop()
	rc := image.Rect(0, 0, dc.Size.X, dc.Size.Y)
	rs := image.Rect(rc.Max.X+gtx.Dp(8), 0, rc.Max.X+gtx.Dp(8)+ds.Size.X, ds.Size.Y)
	cancel.Add(gtx.Ops)
	t := op.Offset(rs.Min).Push(gtx.Ops)
	save.Add(gtx.Ops)
	t.Pop()
	c.rect[crSave], c.rect[crCancel] = rs.Add(c.base), rc.Add(c.base)
	c.shown |= 1<<crSave | 1<<crCancel
	return layout.Dimensions{Size: image.Pt(rs.Max.X, max(ds.Size.Y, dc.Size.Y))}
}

// ---- thread broadcast row ----

// layoutBroadcast draws BroadcastControls (63o2): "☐ Also send to
// #channel" in div.p-threads_footer__input_container__broadcast_controls
// (padding-top 8px, 70% ink) holding a c-label--inline label (12px/18px,
// padding-left 13.5px). The label is a baseline-aligned flex row: the
// native 13px checkbox (c-input_checkbox, margin-right 12px) first, its
// bottom edge on the text's baseline (a checkbox's synthesized baseline),
// then c-label__text (400, top -1px). The channel is an InlineChannelEntity
// with class "bold": InlineChannelIcon's "channel" (or "lock") SvgIcon at
// 1em (12px), is-inline's vertical-align -0.125em, margin-right 2px, then
// the name, all 700. A DM reads "Also send as direct message", a group DM
// "Also send to the group".
func (c *chrome) layoutBroadcast(gtx layout.Context, th *theme.Theme, p *crPal, st *chromeState, x0, x1, top int) {
	cx := x0 + gtx.Dp(13.5)
	cy := top + gtx.Dp(8)
	cs := gtx.Dp(13)
	hov := c.btn[crBroadcast].Hovered()
	bs := crBroadcastClass.Enter(gtx, 0, 0)
	defer bs.Leave()

	s := textStyle{Size: bs.FontSize(12), LineHeight: 18, Color: bs.Color(p.hint), MaxLines: 1}
	x := cx + cs + gtx.Dp(12)
	maxX := x1 - gtx.Dp(12)
	ty := cy - gtx.Dp(1) // c-label--inline .c-label__text: top -1px
	txt := "Also send to"
	if st.IsDM || st.ChannelName == "" {
		txt = "Also send to channel"
		switch {
		case st.Group:
			txt = "Also send to the group"
		case st.IsDM:
			txt = "Also send as direct message"
		}
	}
	call, d := crRec(gtx, th.Shaper, maxX-x, s, txt)
	base := d.Size.Y - d.Baseline // the baseline within the 18px line
	crCheckbox(gtx, image.Rect(cx, cy+base-cs, cx+cs, cy+base), st.Broadcast, hov, p)
	t := op.Offset(image.Pt(x, ty)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	end := x + d.Size.X
	if !st.IsDM && st.ChannelName != "" {
		x = end + c.space(gtx, th.Shaper, 1)
		is := gtx.Dp(12)
		// is-inline: an inline-block 1em box whose bottom sits 0.125em
		// below the baseline
		iy := ty + base + gtx.Dp(1.5) - is
		t := op.Offset(image.Pt(x, iy)).Push(gtx.Ops)
		name := "channel"
		if st.Private {
			name = "lock"
		}
		icons.GetSlack(name)(gtx, unit.Dp(float32(is)/gtx.Metric.PxPerDp), s.Color)
		t.Pop()
		x += is + gtx.Dp(2)
		s.Weight = theme.Bold
		d = crText(gtx, th.Shaper, x, ty, maxX-x, s, st.ChannelName)
		end = x + d.Size.X
	}
	c.hit(gtx, crBroadcast, image.Rect(cx, cy, max(end, cx+cs), top+gtx.Dp(26)), true)
}

// crCheckbox draws Chrome's default checkbox (radius 2).
func crCheckbox(gtx layout.Context, r image.Rectangle, on, hov bool, p *crPal) {
	rad := gtx.Dp(2)
	if on {
		col := p.check
		if hov {
			col = p.checkHover
		}
		fillRRect(gtx, r, rad, col)
		t := op.Offset(r.Min).Push(gtx.Ops)
		crIconCheck(gtx, r.Dx(), p.checkMark)
		t.Pop()
		return
	}
	col := p.checkBorder
	if hov {
		col = p.checkBorderHover
	}
	b := max(gtx.Dp(1), 1)
	fillRRect(gtx, r, rad, col)
	fillRRect(gtx, r.Inset(b), max(rad-b, 0), p.checkBg)
}

// ---- placeholder and hint bar ----

// placeholder draws the empty-composer hint at the text origin (#5e5d60,
// 15/22). Channel placeholders put a 20px hash (lock) box with -4px side
// margins before the name, where the locale's string has {msg}
// ("Message {msg}{govSlackSuffix}": after "Message ", or first in ja-JP).
func (c *chrome) placeholder(gtx layout.Context, th *theme.Theme, p *crPal, st *chromeState, org image.Point, tw int) {
	s := textStyle{Size: 15, LineHeight: 22, Color: p.ph, MaxLines: 1}
	ph := st.Placeholder
	if st.Mode == modeChannel && !st.IsDM && st.ChannelName != "" && st.PlaceholderMsg {
		x := org.X
		var d layout.Dimensions
		if pre := strings.TrimRight(st.PlaceholderPre, " "); pre != "" {
			d = crText(gtx, th.Shaper, org.X, org.Y, tw, s, pre)
			x += d.Size.X
			if pre != st.PlaceholderPre {
				x += c.space(gtx, th.Shaper, 0)
			}
		} else {
			_, d = crRec(gtx, th.Shaper, tw, s, st.ChannelName) // the line's baseline, for the icon
		}
		box := gtx.Dp(20)
		if st.Private {
			is := gtx.Dp(16)
			t := op.Offset(image.Pt(x-gtx.Dp(4)+(box-is)/2, org.Y+(gtx.Dp(22)-is)/2)).Push(gtx.Ops)
			drawLockIcon(gtx, is, p.ph)
			t.Pop()
		} else {
			// Slack's c-icon--hash-small: the icon font's U+E125 at the
			// c-icon:before 20px in a 20px inline box with -4px margins
			// (placeholder_icon--public). Centred on Chrome's 22px line its
			// ink ends on the text's baseline (the font's baseline is 17 of
			// its 20px em, the glyph's foot 1px above), so it is placed on
			// the label's baseline. Not a stroked #: that drew bolder and
			// taller than the text.
			base := org.Y + d.Size.Y - d.Baseline
			t := op.Offset(image.Pt(x-gtx.Dp(4), base-gtx.Dp(16))).Push(gtx.Ops)
			icons.Get("hash-small")(gtx, 20, p.ph)
			t.Pop()
		}
		x += box - 2*gtx.Dp(4)
		nd := crText(gtx, th.Shaper, x, org.Y, org.X+tw-x, s, st.ChannelName)
		if post := st.PlaceholderPost; post != "" {
			x += nd.Size.X
			crText(gtx, th.Shaper, x, org.Y, org.X+tw-x, s, post)
		}
		return
	}
	crText(gtx, th.Shaper, org.X, org.Y, tw, s, ph)
}

// emojiButtonNS is EmojiToolbarButton's "emoji_button" I18n.
var emojiButtonNS = i18n.NS("emoji_button")

// msgInputNS is Slack's "msg_input" I18n: MessageInput's placeholder
// (getPlaceholderText, txEg).
var msgInputNS = i18n.NS("msg_input")

// placeholderText is getPlaceholderText for a conversation named name:
// "Message {msg}{govSlackSuffix}" (no GovSlack suffix), as text and as
// the text before and after {msg}.
func placeholderText(name string) (text, pre, post string) {
	parts := msgInputNS.RT("Message {msg}{govSlackSuffix}", "msg", name, "govSlackSuffix", "")
	seen := false
	for _, p := range parts {
		switch {
		case p.Arg == "msg":
			seen = true
		case !seen:
			pre += p.Text
		default:
			post += p.Text
		}
	}
	return i18n.Plain(parts), pre, post
}

// layoutHint draws the 24px bar under the box (.p-notification_bar: 24px,
// 12px left padding, inside the pane): the typing indicator on the left
// (.p-notification_bar__section: .7em = 10.5px, 1rem line height,
// sk_foreground_max_solid), or "Shift + Enter to add a new line" on the
// right (bold keys, 70% ink, ending 8px inside) once there's something to
// send. A plugin's TypingNames (BetterTypingIndicator) is laid out in that
// section's text style, as it is in Slack's DOM.
func (c *chrome) layoutHint(gtx layout.Context, th *theme.Theme, p *crPal, st *chromeState, box image.Rectangle) {
	ty := box.Max.Y + (gtx.Dp(24)-gtx.Dp(16))/2
	maxW := box.Dx() - gtx.Dp(20)
	if st.Typing != "" {
		t := op.Offset(image.Pt(box.Min.X+gtx.Dp(12), ty)).Push(gtx.Ops)
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(max(maxW, 0), gtx.Dp(16))}
		bar := c.barText()
		switch e, patched := kid(c.f.inputP, "ThreadTypingIndicator"); {
		case st.Mode == modeThread && patched:
			slackc.LayoutElementText(g, e, bar)
		case st.Mode == modeThread:
			slackc.Component(g, "ThreadTypingIndicator", c, (*chrome).threadTypingProps, (*chrome).layoutThreadTyping)
		default:
			if e, patched := kid(c.f.inputP, "TypingNames"); patched {
				slackc.LayoutElementText(g, e, bar)
			} else {
				slackc.Component(g, "TypingNames", c, (*chrome).typingNamesProps, (*chrome).layoutTypingNames)
			}
		}
		t.Pop()
		return
	}
	if !st.CanSend {
		return
	}
	// rt("<b>Shift + Enter</b> to add a new line") (SyrM, the
	// notification_bar I18n): the keys bold, wherever the locale has them
	pref := uipref.Get()
	src := hintSource(shortcut.GOOS, pref.SendOnModEnter, pref.EnterIsSpecial, st.HintInPre)
	type seg struct {
		call op.CallOp
		w    int
		gap  bool // a space before it
	}
	var segs []seg
	total, space := 0, false
	for _, pt := range notificationBarNS.RT(src) {
		txt := strings.TrimSpace(pt.Text)
		gap := space || strings.HasPrefix(pt.Text, " ")
		space = strings.HasSuffix(pt.Text, " ")
		if txt == "" {
			space = space || gap
			continue
		}
		s := textStyle{Size: 10.5, LineHeight: 16, Color: p.hint, MaxLines: 1}
		if pt.Tag == "b" {
			s.Weight = theme.Bold
		}
		call, d := crRec(gtx, th.Shaper, maxW, s, txt)
		gap = gap && len(segs) > 0
		segs = append(segs, seg{call, d.Size.X, gap})
		total += d.Size.X
		if gap {
			total += c.space(gtx, th.Shaper, 2)
		}
	}
	x := box.Max.X - gtx.Dp(8) - total
	for _, sg := range segs {
		if sg.gap {
			x += c.space(gtx, th.Shaper, 2)
		}
		t := op.Offset(image.Pt(x, ty)).Push(gtx.Ops)
		sg.call.Add(gtx.Ops)
		t.Pop()
		x += sg.w
	}
}

// hintSource is NotificationBar's renderFormattingTips (SyrM): Windows
// says Enter and every other platform, Linux too, says Return (+oeN uF is
// isWindows, cX isMac). newlinePref is msg_input_send_btn (Enter starts a
// new line), preNewlines enter_is_special_in_tbt and inPre the caret in
// code being typed with ```.
func hintSource(goos string, newlinePref, preNewlines, inPre bool) string {
	win := goos == "windows"
	switch {
	case (newlinePref || preNewlines && inPre) && win:
		return "<b>Enter</b> to add a new line"
	case newlinePref || preNewlines && inPre:
		return "<b>Return</b> to add a new line"
	case win:
		return "<b>Shift + Enter</b> to add a new line"
	}
	return "<b>Shift + Return</b> to add a new line"
}

// notificationBarNS is the hint bar's I18n (MessagePaneNotificationBar,
// SyrM).
var notificationBarNS = i18n.NS("notification_bar")

// crText draws one line of text at (x, y), at most maxW wide.
func crText(gtx layout.Context, sh *text.Shaper, x, y, maxW int, s textStyle, txt string) layout.Dimensions {
	if maxW <= 0 || txt == "" {
		return layout.Dimensions{}
	}
	t := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(maxW, 1<<16)}
	d := label(g, sh, s, txt)
	t.Pop()
	return d
}

// crRec records one line of text for placing after measuring.
func crRec(gtx layout.Context, sh *text.Shaper, maxW int, s textStyle, txt string) (op.CallOp, layout.Dimensions) {
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(max(maxW, 1), 1<<16)}
	d := label(g, sh, s, txt)
	return m.Stop(), d
}
