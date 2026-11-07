package composer

import (
	"image"
	"image/color"
	"strconv"
	"strings"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// slack.go wraps the composer at Slack's component boundaries
// (docs/PLUGINS-components.md §1.4, Index rows 30–43) so Taut plugins can
// patch it by name, and tags its parts with Slack's classes (§2.3) so
// plugin CSS matches them.
//
// The tree, with each boundary's nested boundaries passed to JS as named
// slackc.Child slots (so a patch's React context reaches the parts below
// it, as BetterTypingIndicator, OwlMode and the composer.addButton infra
// need):
//
//	MessagePaneInput (channel) | InputContainer (thread)
//	├─ BaseContextBarV2                       empty, above the box
//	└─ MessageInputRoadblock                  native: the composer
//	   └─ MessageInput                        (edit composers start here)
//	      ├─ WysiwygContainer                 the bordered box
//	      │  ├─ texty_input_with_autocomplete the editor
//	      │  │  └─ TextyAutocomplete          @ # : rows (includeAllBroadcastKeywords, disallowedResults)
//	      │  │     └─ TextyInput              Slack's memo(class) + Quill ref (plugins.js)
//	      │  ├─ TextyButtons                  the footer toolbar
//	      │  │  └─ TextyButtonOverflow        children: the buttons
//	      │  │     ├─ ShortcutsMenuTriggerButton (+), ComposerButton (Aa), …
//	      │  │     └─ TextySubmitButton       send split / Save + Cancel
//	      │  │        └─ SendButton           the send split
//	      │  └─ CharacterCount                empty, left of send
//	      │  (attachments: MultiFileUploadItem per chip, with FileFieldset >
//	      │   EditFileDialogTitle empty at its edit spot; attach_slack.go)
//	      └─ ThreadTypingIndicator > TypingNames (thread) | TypingNames
//
// A native whose props are nil (its parent unpatched) renders each nested
// boundary with slackc.Component, its own root. Under a patch the nested
// parts already live in the parent's React tree and come back as children
// (kid): the native lays out those nodes at the part's place instead.
//
// The link dialog is ComposerLinkModal (linkdialog.go).

// Slack's viewContext names (taut-api's views.viewContexts) and draft ids
// ("edit-" marks an edit, as in Slack).
const (
	ViewContextMessagePane = "MESSAGE_PANE"
	ViewContextThread      = "THREADS_FLEXPANE"
)

// MaxMessageLength is Slack's message limit (CharacterCount's
// maxCharacterLimit).
const MaxMessageLength = 40000

// Slot names for the composer's own parts inside TextyButtonOverflow.
const (
	crSubmitName  = "TextySubmitButton"
	crDividerName = "TextyButtonsDivider"
)

// ---- classes (§2.3) ----

var (
	crPaneClass   = slackc.Class("p-message_pane_input", "data-qa", "message_input_container")
	crThreadClass = slackc.Class("p-threads_footer__input_container p-threads_footer__input_container--sticky_composer", "data-qa", "reply_container")
	crInputClass  = [...]*slackc.ClassDef{
		modeChannel: slackc.Class("p-message_input_unstyled p-workspace__input"),
		modeThread:  slackc.Class("p-message_input_unstyled p-workspace__input p-threads_footer__input"),
		modeEdit:    slackc.Class("c-message__editor p-message_input_unstyled"),
	}

	crBodyClass  = slackc.Class("c-basic_container__body")
	crFmtClass   = slackc.Class("c-wysiwyg_container__formatting", "data-qa", "wysiwyg-container_formatting-enabled")
	crTextyClass = slackc.Class("c-texty_input_unstyled__container c-texty_input_unstyled__container--size_medium c-texty_input_unstyled__container--multi_line")
	crQuillClass = slackc.Class("ql-container")
	crAttClass   = [...]*slackc.ClassDef{
		modeChannel: slackc.Class("c-wysiwyg_container__attachments c-pending_files"),
		modeThread:  slackc.Class("p-message_input__attachments c-pending_files"),
		modeEdit:    slackc.Class("c-message__editor__composer_attachments c-pending_files"),
	}
	crBroadcastClass    = slackc.Class("p-threads_footer__input_container__broadcast_controls")
	crFootClass         = slackc.Class("c-wysiwyg_container__footer").Mods("c-wysiwyg_container__footer--with_formatting")
	crToolbarClass      = slackc.Class("c-wysiwyg_container__toolbar_buttons")
	crTextyButtonsClass = slackc.Class("c-texty_buttons")
	crDividerClass      = slackc.Class("c-wysiwyg_container__footer_divider", "data-qa", "texty_buttons_divider").Tag("span")
	crSuffixClass       = slackc.Class("c-wysiwyg_container__suffix")
	crSendClass         = slackc.Class("c-wysiwyg_container__button c-wysiwyg_container__button--send", "data-qa", "texty_send_button", "aria-label", "Send now").Tag("button")

	// crBtnClass tags the footer buttons (the toolbar inside .c-texty_buttons).
	crBtnClass = [crNumBtn]*slackc.ClassDef{
		crAttach:   crButton("c-wysiwyg_container__button--shortcuts p-shortcuts_menu_trigger_button--composer_ia", "", "Attach"),
		crAa:       crButton("c-wysiwyg_container__button--composer", "texty_composer_button", "Formatting").Mods("c-wysiwyg_container__button--composer_active"),
		crEmoji:    crButton("c-wysiwyg_container__button--emoji", "texty_emoji_button", "Emoji"),
		crMention:  crButton("c-wysiwyg_container__button--mention", "texty_mention_button", "Mention someone"),
		crVideo:    crButton("c-wysiwyg_container__button--story_meeting p-video_composer_button", "texty_video_button", "Record video clip"),
		crAudio:    crButton("p-audio_composer_button", "texty_audio_button", "Record audio clip"),
		crShortcut: crButton("c-wysiwyg_container__button--slash_commands", "texty_slash_commands_button", "Run shortcut"),
		crFootMore: crButton("c-texty_buttons__overflow_button", "texty_buttons_overflow", "More options"),
	}
)

func crButton(classes, qa, label string) *slackc.ClassDef {
	attrs := []string{"aria-label", label}
	if qa != "" {
		attrs = append(attrs, "data-qa", qa)
	}
	return slackc.Class("c-button-unstyled c-icon_button c-icon_button--size_smedium c-icon_button--default c-wysiwyg_container__button "+classes, attrs...).Tag("button")
}

// The box: Slack puts the input-container scope (SlimMessageBox's SCOPE)
// on the WysiwygContainer root, next to c-wysiwyg_container.
const (
	crWysiwygName    = "p-message_input__input_container_unstyled"
	crWysiwygClasses = "c-wysiwyg_container c-wysiwyg_container--with_footer c-basic_container c-basic_container--size_medium c-basic_container--bordered"
)

var (
	crWysiwygClass = crWysiwygDef(crWysiwygName)
	// crWysiwygByName caches the box's ClassDef per className a plugin
	// passes (OwlMode adds taut-owl-composer). UI goroutine only.
	crWysiwygByName = map[string]*slackc.ClassDef{}
)

func crWysiwygDef(className string) *slackc.ClassDef {
	return slackc.Class(crWysiwygClasses + " " + className).Mods("c-wysiwyg_container--input_focused")
}

func crWysiwygFor(className string) *slackc.ClassDef {
	if className == "" || className == crWysiwygName {
		return crWysiwygClass
	}
	d := crWysiwygByName[className]
	if d == nil {
		if !strings.Contains(" "+className+" ", " "+crWysiwygName+" ") {
			className = crWysiwygName + " " + className
		}
		d = crWysiwygDef(className)
		crWysiwygByName[className] = d
	}
	return d
}

// ---- nested boundaries under a patch ----

// kid returns the host nodes standing for the nested boundary name in a
// patched native's children. patched is false when p is nil (the parent
// was not patched: render the boundary yourself). Under a patch the
// nested part is the top-level node that is (or wraps, like
// BetterTypingIndicator's span around TypingNames) the slot named name,
// plus the plugin nodes after it (and before it, when nothing before
// holds a slot), up to the next node holding a slot. When the plugin
// dropped it, e is empty.
func kid(p slackc.Props, name string) (e slackc.Element, patched bool) {
	if p == nil {
		return slackc.Element{}, false
	}
	ch, ok := slackc.PropChildren(p)
	if !ok {
		return slackc.Element{}, true
	}
	ns := ch.Nodes
	start := -1
	for i, n := range ns {
		if holdsSlot(n, name) {
			start = i
			break
		}
	}
	if start < 0 {
		return slackc.Element{}, true
	}
	first := start
	for first > 0 && !holdsSlot(ns[first-1], "") {
		first--
	}
	if first > 0 {
		first = start // plugin nodes before it belong to the previous part
	}
	end := start + 1
	for end < len(ns) && !holdsSlot(ns[end], "") {
		end++
	}
	return slackc.Element{Nodes: ns[first:end]}, true
}

// holdsSlot reports whether n is, or wraps, a native slot named name (any
// slot when name is ""). It doesn't look inside slots.
func holdsSlot(n *slackc.Node, name string) bool {
	if n == nil {
		return false
	}
	if s, ok := slotName(n); ok {
		return name == "" || s == name
	}
	for _, k := range n.C {
		if holdsSlot(k, name) {
			return true
		}
	}
	return false
}

// slotName reports the Slack name of a native slot node.
func slotName(n *slackc.Node) (string, bool) {
	if n == nil || !n.IsComponent() {
		return "", false
	}
	if s, ok := slackc.SlotOf(n); ok {
		return s.Name, true
	}
	return "", false
}

// slotProps is what a slot's native gets when the composer calls it
// itself for a node of a patched tree (the footer row): the node's props
// plus its rendered children under "children", as the layout engine
// passes them (so kid finds nested parts, e.g. TextySubmitButton's
// SendButton).
func slotProps(nd *slackc.Node) slackc.Props {
	if len(nd.C) == 0 {
		return nd.P
	}
	p := make(slackc.Props, len(nd.P)+1)
	for k, v := range nd.P {
		p[k] = v
	}
	p["children"] = slackc.Element{Nodes: nd.C}
	return p
}

// noneNative draws nothing (props that are elements, never laid out).
func noneNative(layout.Context, slackc.Props) layout.Dimensions { return layout.Dimensions{} }

// ---- enable* flags (MessageInput, TextyButtons, WysiwygContainer) ----

var crEnableFlags = [...]struct {
	prop string
	btn  crBtn
}{
	{"enableFileButton", crAttach},
	{"enableComposerButton", crAa},
	{"enableEmojiButton", crEmoji},
	{"enableMentionButton", crMention},
	{"enableStoryButton", crVideo},
	{"enableAudioButton", crAudio},
	{"enableSlashCommandsButton", crShortcut},
	{"enableShortcutsButton", crShortcut},
	{"enableScheduleSendButton", crSchedule},
	{"enableSubmitButton", crSend},
}

// hideBits returns the buttons a plugin turned off (enable…: false).
func hideBits(p slackc.Props) uint32 {
	var h uint32
	for _, f := range crEnableFlags {
		if p.Has(f.prop) && !p.Bool(f.prop) {
			h |= 1 << f.btn
		}
	}
	return h
}

func putEnableFlags(p slackc.Props, hide uint32) {
	for _, f := range crEnableFlags {
		p[f.prop] = hide&(1<<f.btn) == 0
	}
}

// ---- composer-level boundaries ----

// key and draftID are fixed for a composer; init computes them once so
// the frame doesn't build strings.
func (c *Composer) key() string     { return c.keyS }
func (c *Composer) draftID() string { return c.draftS }

func (c *Composer) initIDs() {
	c.keyS = draftKey(c.ch, c.ts)
	switch {
	case c.edit != nil:
		c.keyS = "edit/" + c.edit.Channel + "/" + c.edit.TS
		c.draftS = "edit-" + c.edit.Channel + "-" + c.edit.TS
	case c.ts != "":
		// Slack's composer draftId: "<channel>-<threadTs>" for a thread
		// (OwlMode parses /^([A-Z0-9]+)-(\d+\.\d+)$/), the channel id
		// for a channel composer.
		c.draftS = c.ch + "-" + c.ts
	default:
		c.draftS = c.ch
	}
}

func (c *Composer) viewContext() string {
	if c.ts != "" {
		return ViewContextThread
	}
	return ViewContextMessagePane
}

// convProps are the props every composer boundary shares.
func (c *Composer) convProps() slackc.Props {
	p := slackc.Props{"$key": c.key(), "channelId": c.ch, "draftId": c.draftID(), "viewContext": c.viewContext()}
	if c.ts != "" {
		p["threadTs"] = c.ts
	}
	return p
}

// paneProps: MessagePaneInput (channel) and InputContainer (thread).
func (c *Composer) paneProps() slackc.Props {
	p := c.convProps()
	p["prepareAndSendMessage"] = c.prepareAndSend
	if c.ts != "" {
		p["dontShowBroadcastControls"] = false
	}
	p["children"] = []any{
		slackc.Child("BaseContextBarV2", c.contextBarProps(), c.layoutContextBar),
		slackc.Child("MessageInputRoadblock", c.roadblockProps(), c.layoutRoadblock),
	}
	return p
}

// layoutPane is MessagePaneInput / InputContainer: the context bar, then
// the roadblock (the composer). It honours a patched
// prepareAndSendMessage (send checks: composer.go sendViaJS) and
// dontShowBroadcastControls (OwlMode, SlimMessageBox).
func (c *Composer) layoutPane(gtx layout.Context, p slackc.Props) layout.Dimensions {
	c.sendJS = slackc.JSFunc{}
	c.cs.NoBroadcast = false
	if p != nil {
		if f, ok := p["prepareAndSendMessage"].(slackc.JSFunc); ok {
			c.sendJS = f
		}
		c.cs.NoBroadcast = p.Bool("dontShowBroadcastControls")
	}
	def := crPaneClass
	if c.ts != "" {
		def = crThreadClass
	}
	s := def.Enter(gtx, 0, 0)
	defer s.Leave()
	if s.Hidden() {
		return layout.Dimensions{}
	}
	c.paneP = p
	if s.Active() {
		return s.Wrap(gtx, c.wPane)
	}
	return c.layoutPaneBody(gtx)
}

func (c *Composer) layoutPaneBody(gtx layout.Context) layout.Dimensions {
	p := c.paneP
	W := gtx.Constraints.Max.X
	m := gtx.Dp(20)
	if c.ts != "" {
		m = gtx.Dp(16)
	}
	cg := gtx
	cg.Constraints = layout.Constraints{Max: image.Pt(max(W-2*m, 0), gtx.Constraints.Max.Y)}
	t := op.Offset(image.Pt(m, 0)).Push(gtx.Ops)
	var cb layout.Dimensions
	if e, patched := kid(p, "BaseContextBarV2"); patched {
		cb = slackc.LayoutElement(cg, e)
	} else {
		cb = slackc.Component(cg, "BaseContextBarV2", c, (*Composer).contextBarProps, (*Composer).layoutContextBar)
	}
	t.Pop()
	c.top = max(cb.Size.Y, 0)
	c.cs.SquareTop = c.top > 0

	rg := gtx
	rg.Constraints.Max.Y = max(gtx.Constraints.Max.Y-c.top, 0)
	rg.Constraints.Min.Y = min(rg.Constraints.Min.Y, rg.Constraints.Max.Y)
	t = op.Offset(image.Pt(0, c.top)).Push(gtx.Ops)
	var rd layout.Dimensions
	if e, patched := kid(p, "MessageInputRoadblock"); patched {
		rd = slackc.LayoutElement(rg, e)
	} else {
		rd = slackc.Component(rg, "MessageInputRoadblock", c, (*Composer).roadblockProps, (*Composer).layoutRoadblock)
	}
	t.Pop()
	return layout.Dimensions{Size: image.Pt(W, c.top+rd.Size.Y)}
}

// BaseContextBarV2: the context bar slot between the message list and the
// box (HaikuWarning, OwlMode). Natively empty; when a plugin draws a bar
// the box's top corners square off under it.
func (c *Composer) contextBarProps() slackc.Props {
	p := c.convProps()
	p["ariaId"] = "context_bar_" + strings.ReplaceAll(c.draftID(), ".", "_")
	p["onIsContextBarShowing"] = c.setContextBarShowing
	return p
}

func (c *Composer) layoutContextBar(gtx layout.Context, p slackc.Props) layout.Dimensions {
	return layout.Dimensions{}
}

func (c *Composer) setContextBarShowing(on bool) {
	if c.ctxShowing != on {
		c.ctxShowing = on
		c.h.Invalidate()
	}
}

// MessageInputRoadblock: around the composer. Its native is the composer
// itself unless a plugin passes a roadblockMessage (RestrictedChannelWarning),
// which is drawn in a box instead of the editor.
func (c *Composer) roadblockProps() slackc.Props {
	p := c.convProps()
	p["roadblockMessage"] = ""
	p["roadblockMessageId"] = "message_input_roadblock_" + strings.ReplaceAll(c.draftID(), ".", "_")
	p["allowWrap"] = false
	p["dataQa"] = "message_input_roadblock"
	p["className"] = ""
	p["children"] = []any{slackc.Child("MessageInput", c.inputProps(), c.layoutInput)}
	return p
}

func (c *Composer) layoutRoadblock(gtx layout.Context, p slackc.Props) layout.Dimensions {
	if p != nil {
		if e, ok := p.Elem("roadblockMessage"); ok && len(e.Nodes) > 0 {
			return c.drawRoadblock(gtx, p)
		}
		if p.StrOr("roadblockMessage", "") != "" {
			return c.drawRoadblock(gtx, p)
		}
	}
	if e, patched := kid(p, "MessageInput"); patched {
		return slackc.LayoutElement(gtx, e)
	}
	return slackc.Component(gtx, "MessageInput", c, (*Composer).inputProps, (*Composer).layoutInput)
}

// drawRoadblock draws Slack's roadblock: the message centred in a
// bordered box where the composer would be, then the hint bar's space.
func (c *Composer) drawRoadblock(gtx layout.Context, p slackc.Props) layout.Dimensions {
	th := c.h.Theme()
	W := gtx.Constraints.Max.X
	m := gtx.Dp(20)
	if c.ts != "" {
		m = gtx.Dp(16)
	}
	box := image.Rect(m, 0, max(W-m, m+2), 0)
	pad := gtx.Dp(12)
	inner := max(box.Dx()-2*pad, 1)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(inner, max(gtx.Constraints.Max.Y, gtx.Dp(22)))}
	rec := op.Record(gtx.Ops)
	var d layout.Dimensions
	if e, ok := p.Elem("roadblockMessage"); ok {
		d = slackc.LayoutElement(g, e)
	} else {
		s := textStyle{Size: 15, LineHeight: 22, Color: th.C.FgMuted}
		if !p.Bool("allowWrap") {
			s.MaxLines = 1
		}
		d = label(g, th.Shaper, s, p.StrOr("roadblockMessage", ""))
	}
	call := rec.Stop()
	box.Max.Y = max(d.Size.Y+2*pad, gtx.Dp(56))
	bw := max(gtx.Dp(1), 1)
	rad := gtx.Dp(8)
	fillRRect(gtx, box, rad, crFade(th.C.Fg, th.C.Bg, .13))
	fillRRect(gtx, box.Inset(bw), max(rad-bw, 0), th.C.Bg)
	t := op.Offset(image.Pt(box.Min.X+(box.Dx()-d.Size.X)/2, (box.Dy()-d.Size.Y)/2)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	return layout.Dimensions{Size: image.Pt(W, box.Max.Y+gtx.Dp(24))}
}

// MessageInput: the composer (box + hint bar). It honours the enable*
// flags (OwlMode turns most buttons off) and passes broadcastControls,
// the "Also send to" checkbox's state, as an element (the
// composer.addButton infra's broadcast context).
func (c *Composer) inputProps() slackc.Props {
	p := c.convProps()
	putEnableFlags(p, 0)
	p["enableGifPicker"] = true
	if c.edit != nil {
		p["enableScheduleSendButton"] = false
		p["enableMentionButton"] = false
	}
	if c.ts != "" && c.edit == nil {
		p["broadcastControls"] = slackc.Child("BroadcastControls", slackc.Props{
			"broadcast":         c.broadcast,
			"channelType":       c.channelType(),
			"onBroadcastChange": c.onBroadcastChange,
		}, noneNative)
	} else {
		p["broadcastControls"] = false
	}
	kids := []any{slackc.Child("WysiwygContainer", c.ui.wysiwygProps(), c.ui.layoutWysiwyg)}
	if c.cs.Typing != "" {
		if c.ts != "" {
			kids = append(kids, slackc.Child("ThreadTypingIndicator", c.ui.threadTypingProps(), c.ui.layoutThreadTyping))
		} else {
			kids = append(kids, slackc.Child("TypingNames", c.ui.typingNamesProps(), c.ui.layoutTypingNames))
		}
	}
	p["children"] = kids
	return p
}

func (c *Composer) channelType() string {
	if cv, ok := c.api.conv(c.ch); ok {
		switch cv.Kind {
		case "im", "mpim":
			return cv.Kind
		case "private":
			return "group"
		}
	}
	return "channel"
}

// onBroadcastChange is the checkbox's onChange({target: {checked}}).
func (c *Composer) onBroadcastChange(e slackc.Props) {
	on := e.Map("target").Bool("checked")
	if c.broadcast != on {
		c.broadcast = on
		c.h.Invalidate()
	}
}

func (c *Composer) layoutInput(gtx layout.Context, p slackc.Props) layout.Dimensions {
	c.cs.Hide = 0
	if p != nil {
		c.cs.Hide = hideBits(p)
	}
	c.cs.Origin = c.Origin.Add(image.Pt(0, c.top))
	s := crInputClass[c.cs.Mode].Enter(gtx, 0, 0)
	defer s.Leave()
	if s.Hidden() {
		return layout.Dimensions{}
	}
	c.inputP = p
	if s.Active() {
		return s.Wrap(gtx, c.wInput)
	}
	return c.layoutInputBody(gtx)
}

func (c *Composer) layoutInputBody(gtx layout.Context) layout.Dimensions {
	c.drewInput = true
	return c.ui.Layout(gtx, c.h, c.cs, c.wEditor, c.wAtt, c.inputP)
}

func (c *Composer) layoutEditor(gtx layout.Context) layout.Dimensions {
	return c.ed.Layout(gtx, c.st)
}

// ---- CSS values the natives read ----

// cssVarColor reads a custom property (--TextyTheme-send-bg, …) as a
// color, resolving var() references against the same scope.
func cssVarColor(vars map[string]string, name string) (color.NRGBA, bool) {
	v, ok := vars[name]
	if !ok {
		return color.NRGBA{}, false
	}
	for i := 0; i < 16; i++ {
		j := strings.Index(v, "var(")
		if j < 0 {
			break
		}
		k, depth := j+4, 1
		for ; k < len(v) && depth > 0; k++ {
			switch v[k] {
			case '(':
				depth++
			case ')':
				depth--
			}
		}
		if depth != 0 {
			return color.NRGBA{}, false
		}
		ref, fb, _ := strings.Cut(v[j+4:k-1], ",")
		rep, ok := vars[strings.TrimSpace(ref)]
		if !ok {
			rep = strings.TrimSpace(fb)
		}
		v = v[:j] + rep + v[k:]
	}
	return parseCSSColor(v)
}

// parseCSSColor parses #rgb, #rgba, #rrggbb, #rrggbbaa and rgb()/rgba()
// (comma or space separated, alpha as a number or percentage).
func parseCSSColor(s string) (color.NRGBA, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if strings.HasPrefix(s, "#") {
		h := s[1:]
		if len(h) == 3 || len(h) == 4 {
			var b strings.Builder
			for _, r := range h {
				b.WriteRune(r)
				b.WriteRune(r)
			}
			h = b.String()
		}
		if len(h) == 6 {
			h += "ff"
		}
		if len(h) != 8 {
			return color.NRGBA{}, false
		}
		n, err := strconv.ParseUint(h, 16, 32)
		if err != nil {
			return color.NRGBA{}, false
		}
		return color.NRGBA{R: uint8(n >> 24), G: uint8(n >> 16), B: uint8(n >> 8), A: uint8(n)}, true
	}
	open := strings.IndexByte(s, '(')
	if open < 0 || !strings.HasSuffix(s, ")") {
		return color.NRGBA{}, false
	}
	if fn := s[:open]; fn != "rgb" && fn != "rgba" {
		return color.NRGBA{}, false
	}
	args := strings.FieldsFunc(s[open+1:len(s)-1], func(r rune) bool { return r == ',' || r == ' ' || r == '/' })
	if len(args) != 3 && len(args) != 4 {
		return color.NRGBA{}, false
	}
	var out [4]float64
	out[3] = 1
	for i, a := range args {
		pct := strings.HasSuffix(a, "%")
		f, err := strconv.ParseFloat(strings.TrimSuffix(a, "%"), 64)
		if err != nil {
			return color.NRGBA{}, false
		}
		switch {
		case i == 3 && pct:
			f /= 100
		case i == 3:
		case pct:
			f = f * 255 / 100
		}
		out[i] = f
	}
	clamp := func(f, hi float64) uint8 {
		f = min(max(f, 0), hi)
		if hi == 1 {
			f *= 255
		}
		return uint8(f + .5)
	}
	return color.NRGBA{R: clamp(out[0], 255), G: clamp(out[1], 255), B: clamp(out[2], 255), A: clamp(out[3], 1)}, true
}

// opaqueOver composites col over an opaque under (CSS colors may be
// translucent; Gio blends in linear light).
func opaqueOver(col, under color.NRGBA) color.NRGBA {
	if col.A == 0xff {
		return col
	}
	return theme.Over(col, under)
}
