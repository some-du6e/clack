package composer

import (
	"image"
	"image/color"
	"strings"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// chrome_slack.go: the chrome's Slack boundaries (slack.go has the tree).
// Natives draw in composer-local coordinates; c.base is the composer-local
// position of the current gtx origin, so hit rects (menu anchors) stay
// composer-local. Under a patch the engine may place a native elsewhere;
// the anchors are then approximate.

// crFrame is one Layout's state, shared by the boundary natives.
type crFrame struct {
	h          Host
	inputP     slackc.Props // MessageInput's props under a patch (nil: none)
	wysP       slackc.Props // WysiwygContainer's
	acP        slackc.Props // TextyAutocomplete's (Composer.Layout applies them)
	input, att layout.Widget

	W, maxY, margin, hintH int
	box                    image.Rectangle // composer-local, set by layoutBox
	boxOK                  bool
	x0, x1                 int
	barBg                  color.NRGBA // formatting bar background (CSS)
	hide                   uint32      // buttons turned off (crBtn bits)
	minOverflow            int
	eq                     int // .c-texty_buttons' offsetWidth (TextyButtons' containerOffsetWidth), px; 0 beside the editor
	fold                   bool
	oneLine                bool // the footer sits beside the editor (layoutBox)
	rowH, textH, attH      int  // layoutBox's editor row, text area and attachments heights
	toolbarHidden          bool
	sufX                   int // composer-local x of the send group

	btn slackc.Styled // the footer button being drawn
}

// crItem is a native footer item: a button, or divider 1 or 2.
type crItem struct {
	id   crBtn
	div  uint8
	l, r unit.Dp // margins
}

// crRow is the footer toolbar left to right (TextyButtonOverflow's
// children, then the submit group). Margins reproduce Slack's spacing.
var crRow = [...]crItem{
	{id: crAttach, l: 6},
	{id: crAa, l: 2, r: 2},
	{id: crEmoji, l: 2, r: 2},
	{id: crMention, l: 2},
	{div: 1, l: 6, r: 6},
	{id: crVideo, l: 2, r: 2},
	{id: crAudio, l: 2, r: 2},
	{div: 2, l: 6, r: 6},
	{id: crShortcut, l: 2},
	{id: crFootMore, l: 4},
}

// crSlotName is the Slack name each footer item crosses to JS as.
func crSlotName(it crItem) string {
	if it.div != 0 {
		return crDividerName
	}
	switch it.id {
	case crAttach:
		return "ShortcutsMenuTriggerButton"
	case crAa:
		return "ComposerButton"
	case crEmoji:
		return "EmojiMenuTrigger"
	case crMention:
		return "MentionButton"
	case crVideo:
		return "VideoComposerButton"
	case crAudio:
		return "AudioComposerButton"
	case crShortcut:
		return "SlashCommandsButton"
	case crFootMore:
		return "TextyButtonOverflowMenu"
	}
	return ""
}

func crItemNamed(name string) (crItem, bool) {
	for _, it := range crRow {
		if crSlotName(it) == name {
			return it, true
		}
	}
	return crItem{}, false
}

func (c *chrome) props() slackc.Props {
	st := &c.cur
	p := slackc.Props{"$key": st.Key, "channelId": st.Channel, "draftId": st.DraftID, "viewContext": st.ViewContext}
	if st.ThreadTS != "" {
		p["threadTs"] = st.ThreadTS
	}
	return p
}

func (c *chrome) hidden(id crBtn) bool { return c.f.hide&(1<<id) != 0 }

// ---- WysiwygContainer (row 33): the box ----

func (c *chrome) wysiwygProps() slackc.Props {
	p := c.props()
	p["className"] = crWysiwygName
	p["numMinLines"] = 1
	p["isDisabled"] = false
	p["enableEmojiButton"] = c.cur.Hide&(1<<crEmoji) == 0
	p["enableComposerButton"] = c.cur.Hide&(1<<crAa) == 0
	p["children"] = []any{
		slackc.Child("texty_input_with_autocomplete", c.textyProps(), c.layoutTexty),
		slackc.Child("TextyButtons", c.textyButtonsProps(), c.layoutTextyButtons),
		slackc.Child("CharacterCount", c.charCountProps(), c.layoutCharCount),
	}
	return p
}

// layoutWysiwyg draws the box. It honours className (OwlMode's
// taut-owl-composer, matched by CSS), the --TextyTheme-* border and send
// colors, border-color (:focus-within) and display/visibility.
func (c *chrome) layoutWysiwyg(gtx layout.Context, p slackc.Props) layout.Dimensions {
	def := crWysiwygClass
	if p != nil {
		def = crWysiwygFor(p.StrOr("className", ""))
	}
	if !slackc.Styles.Empty() {
		var parts uint8
		if c.cur.Pending {
			parts |= crKidFiles
		}
		if c.cur.Mode == modeThread && !c.cur.NoBroadcast {
			parts |= crKidBroadcast
		}
		def = crWysiwygKids(def, c.cur.Mode, parts)
	}
	var cs css.State
	var mods uint64
	if c.cur.Focused {
		cs |= css.StateFocusWithin
		mods |= 1
	}
	if c.bg.Hovered() {
		cs |= css.StateHover
	}
	s := def.Enter(gtx, cs, mods)
	defer s.Leave()
	if s.Hidden() {
		return layout.Dimensions{}
	}
	c.f.wysP = p
	pal := c.pal // the frame's colors, overridden by CSS below
	c.fpal = pal
	if s.Active() {
		c.wysiwygCSS(s)
		if c.wBox == nil {
			c.wBox = c.layoutBoxAt
		}
		return s.Wrap(gtx, c.wBox)
	}
	return c.layoutBoxAt(gtx)
}

func (c *chrome) wysiwygCSS(s slackc.Styled) {
	st := s.Style()
	if st == nil {
		return
	}
	p := &c.fpal
	if v, ok := cssVarColor(st.Vars, "--TextyTheme-wysiwyg-border-color"); ok {
		p.border = opaqueOver(v, p.bg)
		p.borderFocus = p.border
	}
	if s.Has("border-color") {
		p.border = opaqueOver(st.BorderColor[0], p.bg)
		p.borderFocus = p.border
	}
	if v, ok := cssVarColor(st.Vars, "--TextyTheme-send-bg"); ok {
		p.send = opaqueOver(v, p.bg)
		p.sendHover = p.send
	}
	if v, ok := cssVarColor(st.Vars, "--TextyTheme-send-bg-hover"); ok {
		p.sendHover = opaqueOver(v, p.bg)
	}
	p.bg = s.Background(p.bg)
}

// layoutBoxAt draws the box in composer-local coordinates from the
// box's own origin (margin, 0).
func (c *chrome) layoutBoxAt(gtx layout.Context) layout.Dimensions {
	defer op.Offset(image.Pt(-c.f.margin, 0)).Push(gtx.Ops).Pop()
	return c.layoutBox(gtx)
}

// ---- texty_input_with_autocomplete (row 41): the editor ----

func (c *chrome) textyProps() slackc.Props {
	p := c.props()
	p["id"] = "msg_input_" + c.cur.DraftID
	p["placeholder"] = c.cur.Placeholder
	p["ariaLabel"] = c.cur.Placeholder
	p["isDisabled"] = false
	p["size"] = "medium"
	p["useWysiwyg"] = true
	p["completeOnEmoji"] = true
	p["completeOnMembers"] = true
	p["completeOnChannels"] = true
	p["children"] = []any{slackc.Child("TextyAutocomplete", c.textyACProps(), c.layoutTextyAC)}
	return p
}

// layoutTexty lays out the editor; a plugin's placeholder override shows
// in the empty composer.
func (c *chrome) layoutTexty(gtx layout.Context, p slackc.Props) layout.Dimensions {
	if p != nil {
		if ph, ok := p.Str("placeholder"); ok {
			c.cur.Placeholder = ph
		}
	}
	ts := crTextyClass.Enter(gtx, 0, 0)
	defer ts.Leave()
	var cs css.State
	if c.cur.Focused {
		cs = css.StateFocusWithin
	}
	qs := crQuillClass.Enter(gtx, cs, 0)
	defer qs.Leave()
	if e, patched := kid(p, "TextyAutocomplete"); patched {
		return slackc.LayoutElement(gtx, e)
	}
	return slackc.Component(gtx, "TextyAutocomplete", c, (*chrome).textyACProps, (*chrome).layoutTextyAC)
}

// TextyAutocomplete: the editor with its @ # : autocomplete (Slack's
// memo between TextyInputWithAutocomplete and TextyInput). Its native
// records what a plugin changed for the composer's Autocomplete
// (Composer.Layout applies it): includeAllBroadcastKeywords (bChannel
// offers every broadcast keyword) and disallowedResults (ids BetterBlock
// hides from the rows).
func (c *chrome) textyACProps() slackc.Props {
	p := c.props()
	p["completeOnEmoji"] = true
	p["completeOnMembers"] = true
	p["completeOnChannels"] = true
	p["includeAllBroadcastKeywords"] = false
	p["disallowedResults"] = []any{}
	p["children"] = []any{slackc.Child("TextyInput", c.textyInputProps(), c.layoutTextyInput)}
	return p
}

func (c *chrome) layoutTextyAC(gtx layout.Context, p slackc.Props) layout.Dimensions {
	c.f.acP = p
	if e, patched := kid(p, "TextyInput"); patched {
		return slackc.LayoutElement(gtx, e)
	}
	return slackc.Component(gtx, "TextyInput", c, (*chrome).textyInputProps, (*chrome).layoutTextyInput)
}

// TextyInput: the editor itself. In JS it has Slack's shape (taut's
// 04_react.js): a memo of a class with getNumCharsOverLimit, whose ref
// exposes a Quill-like editor fed from $texty (plugins.js;
// CharacterCounter counts with it).
func (c *chrome) textyInputProps() slackc.Props {
	p := c.props()
	p["maxLength"] = composerMaxLength // MessagePaneInput / InputContainer: maxLength 12e3
	p["placeholder"] = c.cur.Placeholder
	p["$texty"] = slackc.Props{"length": c.cur.Count, "index": c.cur.SelLo, "selected": c.cur.SelN}
	return p
}

func (c *chrome) layoutTextyInput(gtx layout.Context, p slackc.Props) layout.Dimensions {
	if c.f.input == nil {
		return layout.Dimensions{}
	}
	return c.f.input(gtx)
}

// ---- TextyButtons (row 34) and TextyButtonOverflow (row 35) ----

func (c *chrome) textyButtonsProps() slackc.Props {
	p := c.props()
	putEnableFlags(p, c.cur.Hide)
	p["collapseToolbarControls"] = false
	// MessageInput passes minButtonsForOverflow only to Slackbot's
	// composer (eT ? 2 : void 0), so TextyButtonOverflow's default of 5
	// applies
	p["textyRef"] = map[string]any{"current": nil}
	p["children"] = []any{slackc.Child("TextyButtonOverflow", c.overflowProps(), c.layoutOverflow)}
	return p
}

// layoutTextyButtons honours enable*Button (SlimMessageBox, OwlMode) and
// minButtonsForOverflow (TextyButtonOverflow's threshold, overflow.go).
func (c *chrome) layoutTextyButtons(gtx layout.Context, p slackc.Props) layout.Dimensions {
	c.f.hide = c.cur.Hide
	c.f.minOverflow = crOverflowDefault
	if p != nil {
		c.f.hide |= hideBits(p)
		if n, ok := p.Num("minButtonsForOverflow"); ok {
			c.f.minOverflow = int(n)
		}
	}
	c.f.fold = c.foldFooter()
	tb := crToolbarClass.Enter(gtx, 0, 0)
	defer tb.Leave()
	ts := crTextyButtonsClass.Enter(gtx, 0, 0)
	defer ts.Leave()
	c.f.toolbarHidden = tb.Hidden() || ts.Hidden()
	if e, patched := kid(p, "TextyButtonOverflow"); patched {
		return slackc.LayoutElement(gtx, e)
	}
	return slackc.Component(gtx, "TextyButtonOverflow", c, (*chrome).overflowProps, (*chrome).layoutOverflow)
}

// overflowProps: children are the footer buttons as slots (plugins find,
// hide, reorder them; composer.addButton appends its buttons), the
// submit group last.
func (c *chrome) overflowProps() slackc.Props {
	px := 1 / max(c.pxPerDp, 1e-3)
	kids := make([]any, 0, len(crRow)+1)
	for _, it := range crRow {
		if it.div == 0 && c.cur.Hide&(1<<it.id) != 0 {
			continue
		}
		it := it
		kids = append(kids, slackc.Child(crSlotName(it), c.itemProps(it), func(gtx layout.Context, p slackc.Props) layout.Dimensions {
			if it.id == crAttach && it.div == 0 {
				return c.layoutShortcuts(gtx, p)
			}
			return c.rowNative(gtx, it)
		}))
	}
	if c.cur.Hide&(1<<crSend) == 0 || c.cur.Mode == modeEdit {
		sp := slackc.Props{"isDisabled": c.cur.sendOff(), "isEditing": c.cur.Mode == modeEdit}
		if c.cur.Mode != modeEdit && c.cur.Hide&(1<<crSend) == 0 {
			sp["children"] = []any{slackc.Child("SendButton", c.sendButtonProps(), c.layoutSendButton)}
		}
		kids = append(kids, slackc.Child(crSubmitName, sp, func(gtx layout.Context, p slackc.Props) layout.Dimensions {
			return c.layoutSubmit(gtx, p)
		}))
	}
	pr := slackc.Props{
		"$key":                 c.cur.Key,
		"containerOffsetWidth": float64(c.f.eq) * float64(px),
		"textyHasFocus":        c.cur.Focused,
		"children":             kids,
	}
	if c.f.minOverflow != crOverflowDefault {
		pr["minButtonsForOverflow"] = c.f.minOverflow
	}
	return pr
}

func (c *chrome) itemProps(it crItem) slackc.Props {
	if it.div != 0 {
		return nil
	}
	if it.id == crAttach {
		return c.shortcutsProps()
	}
	p := slackc.Props{"aria-label": crInfo[it.id].tip, "isDisabled": false}
	if it.id == crAa {
		p["isFormattingEnabled"] = c.cur.ShowFormatting
		p["aria-label"] = "Show formatting"
		if c.cur.ShowFormatting {
			p["aria-label"] = "Hide formatting"
		}
	}
	return p
}

// layoutOverflow lays out the footer row: under a patch, the children
// the plugin left (our slots and its own buttons); else the native list.
func (c *chrome) layoutOverflow(gtx layout.Context, p slackc.Props) layout.Dimensions {
	if p == nil {
		return c.layoutRow(gtx, slackc.Element{}, false)
	}
	if n, ok := p.Num("minButtonsForOverflow"); ok {
		c.f.minOverflow = int(n)
		c.f.fold = c.foldFooter()
	}
	e, _ := slackc.PropChildren(p)
	return c.layoutRow(gtx, e, true)
}

// crSuf is one recorded item of the right-aligned submit group.
type crSuf struct {
	call op.CallOp
	size image.Point
	l, r int
	ids  uint32
}

// layoutRow lays the toolbar out left to right from the row origin, and
// the submit group (and whatever follows it, e.g. composer.addButton
// "end" buttons) right-aligned 6px inside the box. Each item is recorded
// and then placed; the hit rects of the buttons it drew move with it.
func (c *chrome) layoutRow(gtx layout.Context, kids slackc.Element, patched bool) layout.Dimensions {
	W := gtx.Constraints.Max.X
	bs := gtx.Dp(28)
	n, suffix := len(crRow), len(crRow)
	if patched {
		n, suffix = len(kids.Nodes), len(kids.Nodes)
		for i, nd := range kids.Nodes {
			if s, ok := slotName(nd); ok && s == crSubmitName {
				suffix = i
				break
			}
		}
	}
	x := 0
	for i := 0; i < suffix; i++ {
		call, d, l, r, ids := c.rowItem(gtx, kids, patched, i)
		if d.Size.X <= 0 {
			c.shown &^= ids
			continue
		}
		x += gtx.Dp(l)
		c.place(gtx, call, image.Pt(x, (bs-d.Size.Y)/2), ids)
		x += d.Size.X + gtx.Dp(r)
	}
	// taut's composer.addButton buttons ("native" placement) follow the
	// toolbar; free while no plugin adds one.
	if slackc.Patched(tautButtons) {
		m := op.Record(gtx.Ops)
		d := slackc.Render(gtx, tautButtons, c.tautButtonsProps("native"), noNative)
		if call := m.Stop(); d.Size.X > 0 {
			x += gtx.Dp(2)
			c.place(gtx, call, image.Pt(x, (bs-d.Size.Y)/2), 0)
			x += d.Size.X + gtx.Dp(2)
		}
	}

	c.suf = c.suf[:0]
	tot := 0
	ss := crSuffixClass.Enter(gtx, 0, 0)
	if !ss.Hidden() {
		if !patched {
			before := c.shown
			m := op.Record(gtx.Ops)
			d := c.layoutSubmit(gtx, nil)
			c.suf = append(c.suf, crSuf{call: m.Stop(), size: d.Size, ids: c.shown &^ before})
		} else {
			for i := suffix; i < n; i++ {
				call, d, l, r, ids := c.rowItem(gtx, kids, patched, i)
				if i == suffix {
					l, r = 0, 0
				}
				c.suf = append(c.suf, crSuf{call: call, size: d.Size, l: gtx.Dp(l), r: gtx.Dp(r), ids: ids})
			}
		}
	}
	if !ss.Hidden() && slackc.Patched(tautButtons) {
		m := op.Record(gtx.Ops)
		d := slackc.Render(gtx, tautButtons, c.tautButtonsProps("end"), noNative)
		c.suf = append(c.suf, crSuf{call: m.Stop(), size: d.Size, l: gtx.Dp(4)})
	}
	ss.Leave()
	for _, s := range c.suf {
		if s.size.X > 0 {
			tot += s.l + s.size.X + s.r
		}
	}
	sx := W - gtx.Dp(6) - tot
	if c.sufW != tot && !c.f.oneLine {
		// the toolbar's width follows the submit group (TextyButtons'
		// ResizeObserver, the character count beside the send button):
		// fold again with this width next frame if it changes the fold
		c.sufW = tot
		eq := c.f.eq
		c.f.eq = c.toolbarWidth(gtx, c.f.x1-c.f.x0)
		if c.foldFooter() != c.f.fold {
			c.f.h.Invalidate()
		}
		c.f.eq = eq
	}
	if !c.f.fold { // a folded row's width would flip a row-flex layout back and forth
		c.footNat = x + tot + gtx.Dp(6) // the row at its own width
	}
	c.f.sufX = c.base.X + sx
	for _, s := range c.suf {
		if s.size.X <= 0 {
			c.shown &^= s.ids
			continue
		}
		sx += s.l
		c.place(gtx, s.call, image.Pt(sx, (bs-s.size.Y)/2), s.ids)
		sx += s.size.X + s.r
	}
	return layout.Dimensions{Size: image.Pt(W, bs)}
}

// rowItem records item i at the row origin.
func (c *chrome) rowItem(gtx layout.Context, kids slackc.Element, patched bool, i int) (op.CallOp, layout.Dimensions, unit.Dp, unit.Dp, uint32) {
	before := c.shown
	m := op.Record(gtx.Ops)
	var d layout.Dimensions
	l, r := unit.Dp(2), unit.Dp(2)
	if !patched {
		it := crRow[i]
		l, r = it.l, it.r
		if it.div == 0 && it.id == crAttach {
			d = slackc.Component(gtx, "ShortcutsMenuTriggerButton", c, (*chrome).shortcutsProps, (*chrome).layoutShortcuts)
		} else {
			d = c.rowNative(gtx, it)
		}
	} else {
		nd := kids.Nodes[i]
		if s, ok := slackc.SlotOf(nd); ok && s.Native != nil {
			if it, ok := crItemNamed(s.Name); ok {
				l, r = it.l, it.r
			}
			d = s.Native(gtx, slotProps(nd))
		} else {
			c.one[0] = nd
			d = slackc.LayoutElement(gtx, slackc.Element{Nodes: c.one[:]})
			c.one[0] = nil
		}
	}
	return m.Stop(), d, l, r, c.shown &^ before
}

// place adds a recorded item at at (row-relative) and moves the hit
// rects of the buttons it drew there.
func (c *chrome) place(gtx layout.Context, call op.CallOp, at image.Point, ids uint32) {
	t := op.Offset(at).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	if ids == 0 {
		return
	}
	for id := crBtn(0); id < crNumBtn; id++ {
		if ids&(1<<id) != 0 {
			c.rect[id] = c.rect[id].Add(at)
		}
	}
}

func (c *chrome) rowNative(gtx layout.Context, it crItem) layout.Dimensions {
	if it.div != 0 {
		return c.footDivider(gtx, it.div)
	}
	d := c.footItem(gtx, it.id)
	if it.id == crFootMore && d.Size.X > 0 {
		c.layoutMoreRow(gtx, image.Rectangle{Max: d.Size}) // overflow.go
	}
	return d
}

// itemVisible: what the footer shows in this mode, fold state and with
// the buttons plugins turned off.
func (c *chrome) itemVisible(id crBtn) bool {
	if c.hidden(id) || c.f.toolbarHidden {
		return false
	}
	edit := c.cur.Mode == modeEdit
	if c.f.fold && c.inOverflow(id) {
		return false // in TextyButtonOverflow's row (overflow.go)
	}
	switch id {
	case crMention, crVideo, crAudio, crShortcut:
		return !edit
	case crFootMore:
		return !edit && c.f.fold
	}
	return true
}

// footItem draws a footer button at the gtx origin, tagged with its
// Slack classes: CSS can hide it (SlimMessageBox's hidden emoji button),
// recolor its icon and hover tile, and box-style it.
func (c *chrome) footItem(gtx layout.Context, id crBtn) layout.Dimensions {
	if !c.itemVisible(id) {
		return layout.Dimensions{}
	}
	var cs css.State
	if c.btn[id].Hovered() {
		cs |= css.StateHover
	}
	var mods uint64
	if id == crAa && c.cur.ShowFormatting {
		mods = 1
	}
	var s slackc.Styled
	if id == crAttach {
		qa := "shortcuts_menu_trigger__Channel"
		if c.cur.Mode == modeThread {
			qa = "shortcuts_menu_trigger__Thread"
		}
		s = crBtnClass[id].EnterAttr(gtx, cs, mods, "data-qa", qa)
	} else {
		s = crBtnClass[id].Enter(gtx, cs, mods)
	}
	defer s.Leave()
	if s.Hidden() {
		return layout.Dimensions{}
	}
	c.f.btn = s
	if s.Active() {
		if c.wFoot[id] == nil {
			c.wFoot[id] = func(gtx layout.Context) layout.Dimensions { return c.drawFoot(gtx, id) }
		}
		return s.Wrap(gtx, c.wFoot[id])
	}
	return c.drawFoot(gtx, id)
}

func (c *chrome) drawFoot(gtx layout.Context, id crBtn) layout.Dimensions {
	bs := gtx.Dp(28)
	r := image.Rect(0, 0, bs, bs)
	if id == crAttach {
		c.attachButton(gtx, c.f.h.Theme(), &c.fpal, &c.cur, r)
	} else {
		c.footButton(gtx, c.f.h.Theme(), &c.fpal, &c.cur, id, r)
	}
	return layout.Dimensions{Size: r.Size()}
}

// footDivider draws divider 1 (before video/audio) or 2 (before Run
// shortcut) in wide composers; SlimMessageBox hides them with CSS.
func (c *chrome) footDivider(gtx layout.Context, which uint8) layout.Dimensions {
	if c.f.fold || c.f.toolbarHidden || c.cur.Mode == modeEdit {
		return layout.Dimensions{}
	}
	if which == 1 && c.hidden(crVideo) && c.hidden(crAudio) || which == 2 && c.hidden(crShortcut) {
		return layout.Dimensions{}
	}
	s := crDividerClass.Enter(gtx, 0, 0)
	defer s.Leave()
	if s.Hidden() {
		return layout.Dimensions{}
	}
	sz := image.Pt(max(gtx.Dp(1), 1), gtx.Dp(20))
	fillRect(gtx, image.Rectangle{Max: sz}, s.Background(c.fpal.sepFoot))
	return layout.Dimensions{Size: sz}
}

// layoutSubmit is the submit group: the send split button (SendButton),
// or Cancel and Save while editing. p is TextySubmitButton's props under
// a patch (nil otherwise).
func (c *chrome) layoutSubmit(gtx layout.Context, p slackc.Props) layout.Dimensions {
	if c.cur.Mode == modeEdit {
		return c.layoutEditButtons(gtx, c.f.h.Theme(), &c.cur)
	}
	if c.hidden(crSend) {
		return layout.Dimensions{}
	}
	if e, patched := kid(p, "SendButton"); patched {
		return c.layoutInline(gtx, e)
	}
	return slackc.Component(gtx, "SendButton", c, (*chrome).sendButtonProps, (*chrome).layoutSendButton)
}

// ---- SendButton: the send split button ----

// SendButton is where CharacterCounter puts its count (before Original).
// Under a patch its nodes and the plugin's are laid out in a row
// (layoutInline), as in Slack's flex footer.
func (c *chrome) sendButtonProps() slackc.Props {
	p := c.props()
	p["isDisabled"] = c.cur.sendOff()
	p["type"] = "primary"
	return p
}

func (c *chrome) layoutSendButton(gtx layout.Context, p slackc.Props) layout.Dimensions {
	return c.layoutSend(gtx, &c.fpal, &c.cur)
}

// layoutInline lays e's top-level nodes out left to right, centred on the
// tallest. The hit rects of the buttons a node draws move with it.
func (c *chrome) layoutInline(gtx layout.Context, e slackc.Element) layout.Dimensions {
	c.inl = c.inl[:0]
	h := 0
	for _, nd := range e.Nodes {
		before := c.shown
		m := op.Record(gtx.Ops)
		var d layout.Dimensions
		if s, ok := slackc.SlotOf(nd); ok && s.Native != nil {
			d = s.Native(gtx, slotProps(nd)) // our own part: its native, as rowItem does
		} else {
			c.inlOne[0] = nd
			d = slackc.LayoutElement(gtx, slackc.Element{Nodes: c.inlOne[:]})
			c.inlOne[0] = nil
		}
		c.inl = append(c.inl, crSuf{call: m.Stop(), size: d.Size, ids: c.shown &^ before})
		h = max(h, d.Size.Y)
	}
	x := 0
	for _, s := range c.inl {
		if s.size.X <= 0 {
			c.shown &^= s.ids
			continue
		}
		c.place(gtx, s.call, image.Pt(x, (h-s.size.Y)/2), s.ids)
		x += s.size.X
	}
	return layout.Dimensions{Size: image.Pt(x, h)}
}

// ---- ShortcutsMenuTriggerButton (row 36): the + ----

// The + opens Slack's shortcuts/attach menu (live props: onFilesSelected,
// limitShortcutsMenuToFileUpload); OwlMode returns null for it in owl
// threads, which removes the button.
func (c *chrome) shortcutsProps() slackc.Props {
	p := c.props()
	p["isDisabled"] = false
	p["isComposerPage"] = false
	p["isEditMessageShortcutsButton"] = c.cur.Mode == modeEdit
	p["forceOpen"] = false
	p["forceFileMenuOpen"] = false
	p["limitShortcutsMenuToFilesOnly"] = false
	p["limitShortcutsMenuToFileUpload"] = false
	p["windowId"] = "main"
	return p
}

func (c *chrome) layoutShortcuts(gtx layout.Context, p slackc.Props) layout.Dimensions {
	return c.footItem(gtx, crAttach)
}

// ---- CharacterCount (row 42): empty, left of the send group ----

func (c *chrome) charCountProps() slackc.Props {
	return slackc.Props{"$key": c.cur.Key, "maxCharacterLimit": MaxMessageLength, "currentCount": c.cur.Count, "type": "multiline"}
}

func (c *chrome) layoutCharCount(gtx layout.Context, p slackc.Props) layout.Dimensions {
	return layout.Dimensions{}
}

// ---- ThreadTypingIndicator (row 39) and TypingNames (row 40) ----

func (c *chrome) threadTypingProps() slackc.Props {
	p := c.props()
	p["suppressBotTyping"] = false
	p["children"] = []any{slackc.Child("TypingNames", c.typingNamesProps(), c.layoutTypingNames)}
	return p
}

func (c *chrome) layoutThreadTyping(gtx layout.Context, p slackc.Props) layout.Dimensions {
	if e, patched := kid(p, "TypingNames"); patched {
		return slackc.LayoutElement(gtx, e)
	}
	return slackc.Component(gtx, "TypingNames", c, (*chrome).typingNamesProps, (*chrome).layoutTypingNames)
}

// typingNamesProps: Slack's firstTyper/secondTyper/severalPeopleAreTyping
// plus the typers' member ids (memberIds) and the conversation.
func (c *chrome) typingNamesProps() slackc.Props {
	p := c.props()
	names := c.cur.TypingNames
	switch {
	case len(names) > 2:
		p["severalPeopleAreTyping"] = true
	case len(names) == 2:
		p["firstTyper"], p["secondTyper"] = names[0], names[1]
		p["severalPeopleAreTyping"] = false
	case len(names) == 1:
		p["firstTyper"] = names[0]
		p["severalPeopleAreTyping"] = false
	}
	p["memberIds"] = c.cur.TypingIDs
	return p
}

// typingText is .p-notification_bar__section's text: font-size .7em of
// the pane's 15px, line-height 1rem, sk_foreground_max_solid; names in
// TypingNames' <b>.
func (c *chrome) typingText(bold bool) textStyle {
	s := textStyle{Size: 10.5, LineHeight: 16, Color: c.fpal.typing, MaxLines: 1}
	if bold {
		s.Weight = theme.Bold
	}
	return s
}

// barText is that section's style for plugin nodes laid out in it.
func (c *chrome) barText() slackc.InheritText {
	return slackc.InheritText{Size: 10.5, LineHeight: 16, Weight: 400, Color: c.fpal.typing}
}

// layoutTypingNames is Slack's TypingNames: "<b>{name}</b> is typing",
// "<b>{name}</b> and <b>{other_name}</b> are typing", or "Several people
// are typing", one line in the bar's section style (no ellipsis; the
// section clips with text-overflow: ellipsis). Under a patch firstTyper
// and secondTyper may be elements (BetterTypingIndicator's member links):
// they are laid out inside the template's <b>, inheriting its bold and
// the section's size and colour, like any text name. An app's assistant
// status is AiAppStatusIndicator's "<b>{app}</b> {status}".
func (c *chrome) layoutTypingNames(gtx layout.Context, p slackc.Props) layout.Dimensions {
	th := c.f.h.Theme()
	maxW := gtx.Constraints.Max.X
	x, h := 0, 0
	add := func(d layout.Dimensions) {
		x += d.Size.X
		h = max(h, d.Size.Y)
	}
	sp := c.space(gtx, th.Shaper, 2) // a 10.5px space
	text := func(s string, bold bool) {
		if strings.HasPrefix(s, " ") {
			x += sp
			s = s[1:]
		}
		if x < maxW && s != "" {
			add(crText(gtx, th.Shaper, x, 0, maxW-x, c.typingText(bold), s))
		}
	}
	name := func(prop string) {
		if x >= maxW {
			return
		}
		if e, ok := p.Elem(prop); ok {
			t := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
			g := gtx
			g.Constraints = layout.Constraints{Max: image.Pt(maxW-x, gtx.Constraints.Max.Y)}
			in := c.barText()
			in.Weight = 700 // <b>
			d := slackc.LayoutElementText(g, e, in)
			t.Pop()
			add(d)
			return
		}
		text(p.StrOr(prop, ""), true)
	}
	if c.cur.TypingStatus != "" {
		text(c.cur.TypingAgent, true)
		text(" "+c.cur.TypingStatus, false)
		return layout.Dimensions{Size: image.Pt(min(x, maxW), h)}
	}
	if p == nil {
		p = c.typingNamesProps()
	}
	switch {
	case p.Bool("severalPeopleAreTyping"):
		text("Several people are typing", false)
	case p.Has("firstTyper") && p.Has("secondTyper"):
		name("firstTyper")
		text(" and", false)
		x += sp
		name("secondTyper")
		text(" are typing", false)
	case p.Has("firstTyper"):
		name("firstTyper")
		text(" is typing", false)
	}
	return layout.Dimensions{Size: image.Pt(min(x, maxW), h)}
}

// tautButtons is internal/taut's boundary for composer.addButton entries
// (each an "@ComposerButton" drawn by slackc/kit).
const tautButtons = "TautComposerButtons"

func (c *chrome) tautButtonsProps(placement string) slackc.Props {
	st := &c.cur
	kind := "channel"
	switch {
	case st.Mode == modeEdit:
		kind = "edit"
	case st.ThreadTS != "":
		kind = "thread"
	}
	ctx := slackc.Props{"kind": kind, "channelId": st.Channel}
	if st.ThreadTS != "" {
		ctx["threadTs"] = st.ThreadTS
		// Taut's ComposerContext.broadcast: {active, channelType, set}.
		b := slackc.Props{"active": st.Broadcast, "channelType": st.ChannelType}
		if st.SetBroadcast != nil {
			b["set"] = st.SetBroadcast
		}
		ctx["broadcast"] = b
	}
	return slackc.Props{"$key": st.Key + "/" + placement, "placement": placement, "context": ctx}
}

func noNative(gtx layout.Context, p slackc.Props) layout.Dimensions { return layout.Dimensions{} }
