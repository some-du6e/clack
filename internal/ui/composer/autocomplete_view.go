package composer

import (
	"image"
	"image/color"
	"strconv"
	"strings"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
)

// Popup metrics and colors measured from the Slack desktop client
// (div.c-texty_autocomplete). Every list is flat: no section headers.
const (
	acPadY       = unit.Dp(8)  // body padding above and below the rows (scrolls with them)
	acRowH       = unit.Dp(32) // @ # : rows
	acCmdRowH    = unit.Dp(60) // / rows
	acRowPadX    = unit.Dp(16)
	acFooterH    = unit.Dp(32)
	acFooterPadX = unit.Dp(20)
	acScrollW    = unit.Dp(15) // native scrollbar gutter

	acAvatar  = unit.Dp(20)
	acAvatarR = unit.Dp(4.44)
	acIcon    = unit.Dp(18) // channel / usergroup icon
	acEmojiPx = unit.Dp(22)
	acStatus  = unit.Dp(16)
	acPresBox = unit.Dp(20)
	acPresDot = unit.Dp(9)

	acTextSize  = unit.Sp(15)
	acSmallSize = unit.Sp(13)
	acHelpSize  = unit.Sp(12)
)

var (
	acBg        = rgb(0xf8f8f8)
	acRing      = argb(0x211d1c1d) // 0 0 0 1px rgba(29,28,29,.13)
	acPseudoBg  = argb(0x0f1d1c1d) // rgba(29,28,29,.06)
	acSelBg     = rgb(0x1264a3)
	acWhite     = rgb(0xffffff)
	acText      = rgb(0x1d1c1d)
	acContext   = rgb(0x454447) // "Not in channel", archived channels
	acHelpText  = rgb(0x616061)
	acHelpBg    = argb(0x0a1d1c1d) // rgba(29,28,29,.04)
	acCmdDesc   = argb(0xb31d1c1d) // rgba(29,28,29,.7)
	acPlaceBg   = rgb(0xeaeaea)
	acActive    = rgb(0x20a271)
	acAway      = rgb(0x616061)
	acHiBg      = argb(0x2effc600) // rgba(255,198,0,.18)
	acHiSelBg   = rgb(0xf2c744)
	acHiSelText = rgb(0x0b4c8c)
	acChipBg    = rgb(0xeaeaea)
	acChipLine  = argb(0x215e5d60) // rgba(94,93,96,.13)
	acTrack     = rgb(0xf1f1f1)
	acThumb     = rgb(0xc1c1c1)
	acArrow     = rgb(0x505050)
)

// acBox is the container spec for one trigger kind.
type acBox struct {
	w, minW, maxW unit.Dp
	maxH          unit.Dp // whole popup, footer included
	footer        bool
	radius        unit.Dp
	shadow        float32 // alpha of the 0 4px 12px drop shadow
	rowH          unit.Dp
}

func acBoxFor(k acKind) acBox {
	switch k {
	case acEmoji:
		return acBox{w: 420, minW: 420, maxW: 540, maxH: 220, footer: true, radius: 6, shadow: .08, rowH: acRowH}
	case acCommand:
		return acBox{w: 450, minW: 450, maxW: 575, maxH: 262, radius: 8, shadow: .10, rowH: acCmdRowH}
	}
	return acBox{w: 540, minW: 420, maxW: 540, maxH: 220, footer: true, radius: 6, shadow: .08, rowH: acRowH}
}

// acView is the popup's per-frame state.
type acView struct {
	list    layout.List
	rows    []*acRowTag
	pressed int // 1 + the pressed row index, 0 for none
	bgTag   int
	rowPx   int
	rowW    int
	// command popups are content-fit: width cached per item set
	cmdW   int
	cmdVer uint64
}

// acRowTag is a stable pointer tag per row index.
type acRowTag struct{ i int }

// Layout draws the popup at the current offset (its top-left corner) and
// returns its size. gtx.Constraints.Max is the space available: Max.Y the
// room above the anchor line, Max.X the room to the right of the popup's
// left edge. maxWidth, if > 0, further caps the width (pixels).
func (ac *Autocomplete) Layout(gtx layout.Context, maxWidth int) layout.Dimensions {
	if !ac.Open() {
		return layout.Dimensions{}
	}
	acApplyTheme(ac.h != nil && ac.h.Theme() != nil && ac.h.Theme().Dark)
	v := &ac.view
	ac.pointerEvents(gtx)
	if !ac.Open() { // a click accepted a row
		return layout.Dimensions{}
	}

	spec := acBoxFor(ac.trig.Kind)
	n := len(ac.items)
	rowH := gtx.Dp(spec.rowH)
	pad := gtx.Dp(acPadY)
	footH := 0
	if spec.footer {
		footH = gtx.Dp(acFooterH)
	}
	content := 2*pad + n*rowH
	bodyH := min(content, gtx.Dp(spec.maxH)-footH)
	if maxY := gtx.Constraints.Max.Y; maxY > 0 && bodyH+footH > maxY {
		bodyH = max(maxY-footH, rowH)
	}
	overflow := content > bodyH
	scrollW := 0
	if overflow {
		scrollW = gtx.Dp(acScrollW)
	}

	avail := gtx.Constraints.Max.X
	if maxWidth > 0 {
		avail = min(avail, maxWidth)
	}
	w := gtx.Dp(spec.w)
	if ac.trig.Kind == acCommand {
		w = ac.commandWidth(gtx, spec, scrollW)
	}
	w = max(min(w, avail), 1)
	h := bodyH + footH
	v.rowPx, v.rowW = rowH, w-scrollW

	box := image.Rect(0, 0, w, h)
	rad := gtx.Dp(spec.radius)
	drawDropShadow(gtx, box, rad, spec.shadow)
	ring := gtx.Dp(1)
	fillRRect(gtx, box.Inset(-ring), rad+ring, acRing)
	fillRRect(gtx, box, rad, acBg)

	// everything inside is clipped to the rounded box and swallows input
	area := clip.UniformRRect(box, rad).Push(gtx.Ops)
	event.Op(gtx.Ops, &v.bgTag)
	for {
		if _, ok := gtx.Event(pointer.Filter{Target: &v.bgTag, Kinds: pointer.Press | pointer.Release}); !ok {
			break
		}
	}

	ac.keepSelectionVisible(pad, rowH, bodyH)
	for len(v.rows) < n {
		v.rows = append(v.rows, &acRowTag{i: len(v.rows)})
	}
	func() {
		gtx := gtx
		gtx.Constraints = layout.Exact(image.Pt(v.rowW, bodyH))
		defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
		v.list.Axis = layout.Vertical
		// elements 0 and n+1 are the body padding, so it scrolls like CSS
		v.list.Layout(gtx, n+2, func(gtx layout.Context, i int) layout.Dimensions {
			if i == 0 || i == n+1 {
				return layout.Dimensions{Size: image.Pt(v.rowW, pad)}
			}
			return ac.layoutRow(gtx, i-1, v.rowW, rowH)
		})
	}()
	if overflow {
		top := ac.scrollTop(pad, rowH)
		drawScrollbar(gtx, image.Rect(w-scrollW, 0, w, bodyH), top, content, bodyH)
	}
	if footH > 0 {
		ac.layoutFooter(gtx, image.Rect(0, bodyH, w, h))
	}
	area.Pop()
	return layout.Dimensions{Size: box.Size()}
}

// scrollTop is the list's scroll offset in pixels (padding included).
func (ac *Autocomplete) scrollTop(pad, rowH int) int {
	p := ac.view.list.Position
	top := p.Offset
	if p.First > 0 {
		top += pad + (p.First-1)*rowH
	}
	return max(top, 0)
}

// keepSelectionVisible scrolls the list after keyboard moves.
func (ac *Autocomplete) keepSelectionVisible(pad, rowH, bodyH int) {
	if !ac.scroll {
		return
	}
	ac.scroll = false
	top := ac.scrollTop(pad, rowH)
	r0 := pad + ac.sel*rowH
	r1 := r0 + rowH
	switch {
	case ac.sel == 0:
		top = 0
	case r0 < top:
		top = r0
	case r1 > top+bodyH:
		top = r1 - bodyH
	default:
		return
	}
	p := &ac.view.list.Position
	if top < pad {
		p.First, p.Offset = 0, top
	} else {
		p.First, p.Offset = (top-pad)/rowH+1, (top-pad)%rowH
	}
}

// pointerEvents applies last frame's hover and clicks: moving over a row
// selects it, a press and release on the same row accepts it.
func (ac *Autocomplete) pointerEvents(gtx layout.Context) {
	v := &ac.view
	for i := range v.rows {
		for {
			ev, ok := gtx.Event(pointer.Filter{
				Target: v.rows[i],
				Kinds:  pointer.Move | pointer.Enter | pointer.Leave | pointer.Press | pointer.Release | pointer.Cancel,
			})
			if !ok {
				break
			}
			e, ok := ev.(pointer.Event)
			if !ok || i >= len(ac.items) {
				continue
			}
			switch e.Kind {
			case pointer.Move:
				ac.hover(i)
			case pointer.Press:
				if e.Buttons.Contain(pointer.ButtonPrimary) || e.Source == pointer.Touch {
					v.pressed = i + 1
					ac.hover(i)
				}
			case pointer.Release:
				inside := e.Position.X >= 0 && e.Position.Y >= 0 &&
					e.Position.X < float32(v.rowW) && e.Position.Y < float32(v.rowPx)
				if v.pressed == i+1 && inside {
					v.pressed = 0
					ac.accept(i)
					return
				}
				v.pressed = 0
			case pointer.Cancel:
				v.pressed = 0
			}
		}
	}
}

// layoutRow draws row i in a w×rowH box.
func (ac *Autocomplete) layoutRow(gtx layout.Context, i, w, rowH int) layout.Dimensions {
	it := ac.items[i]
	hot := i == ac.sel
	sel := hot && !ac.pseudo
	r := image.Rect(0, 0, w, rowH)
	switch {
	case sel:
		fillRect(gtx, r, acSelBg)
	case hot:
		fillRect(gtx, r, acPseudoBg)
	}
	area := clip.Rect(r).Push(gtx.Ops)
	event.Op(gtx.Ops, ac.view.rows[i])
	pointer.CursorPointer.Add(gtx.Ops)
	area.Pop()

	rc := acRowCtx{gtx: gtx, ac: ac, rowH: rowH, sel: sel, x: gtx.Dp(acRowPadX), right: w - gtx.Dp(acRowPadX)}
	if hot && !sel {
		rc.right = rc.enterHint(rc.right, 0, rowH) - gtx.Dp(8)
	}
	switch {
	case it.Kind == acCommand:
		rc.command(it)
	case it.Kind == acEmoji:
		rc.emoji(it, ac.trig.Query)
	case it.Kind == acChannel:
		rc.channel(it)
	case it.IsGroup():
		rc.group(it, ac.teamName)
	case it.IsBroadcast():
		rc.broadcast(it)
	default:
		rc.member(it)
	}
	return layout.Dimensions{Size: r.Size()}
}

// acRowCtx draws one row left to right; x advances, right is the limit.
type acRowCtx struct {
	gtx      layout.Context
	ac       *Autocomplete
	rowH     int
	sel      bool
	x, right int
}

func (rc *acRowCtx) color(c color.NRGBA) color.NRGBA {
	if rc.sel {
		return acWhite
	}
	return c
}

func (rc *acRowCtx) gap(dp unit.Dp) { rc.x += rc.gtx.Dp(dp) }

// text draws txt vertically centred in [y, y+h) at rc.x and advances.
func (rc *acRowCtx) text(y, h int, s textStyle, txt string) int {
	tw := rc.ac.drawText(rc.gtx, rc.x, y, h, rc.right-rc.x, s, txt)
	rc.x += tw
	return tw
}

func (rc *acRowCtx) line(size unit.Sp, w font.Weight, col color.NRGBA, txt string) int {
	return rc.text(0, rc.rowH, textStyle{Size: size, Weight: w, Color: rc.color(col), MaxLines: 1}, txt)
}

// square returns an s×s rect at rc.x, vertically centred, and advances
// past it plus gap.
func (rc *acRowCtx) square(s, gap unit.Dp) image.Rectangle {
	px := rc.gtx.Dp(s)
	y := (rc.rowH - px) / 2
	r := image.Rect(rc.x, y, rc.x+px, y+px)
	rc.x += px + rc.gtx.Dp(gap)
	return r
}

// enterHint draws the "Enter" chip ending at right, centred in [y, y+h),
// and returns its left edge.
func (rc *acRowCtx) enterHint(right, y, h int) int {
	gtx := rc.gtx
	s := textStyle{Size: acSmallSize, LineHeight: acSmallSize, Color: acText, MaxLines: 1}
	tw := rc.ac.measure(gtx, s, "Enter")
	cw, ch := tw+gtx.Dp(8), gtx.Dp(20)
	r := image.Rect(right-cw, y+(h-ch)/2, right, y+(h-ch)/2+ch)
	rad := gtx.Dp(4)
	fillRRect(gtx, r, rad, acChipLine)
	fillRRect(gtx, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Max.Y-max(gtx.Dp(1), 1)), rad, acChipBg)
	rc.ac.drawText(gtx, r.Min.X+gtx.Dp(4), r.Min.Y, ch, tw+1, s, "Enter")
	return r.Min.X
}

// rightText draws a label right-aligned at rc.right and pulls rc.right in.
func (rc *acRowCtx) rightText(s textStyle, txt string) {
	tw := rc.ac.measure(rc.gtx, s, txt)
	x := rc.right - tw
	if x <= rc.x {
		return
	}
	rc.ac.drawText(rc.gtx, x, 0, rc.rowH, tw+1, s, txt)
	rc.right = x
}

func (rc *acRowCtx) member(it acItem) {
	gtx := rc.gtx
	if it.NotIn {
		rc.rightText(textStyle{Size: acSmallSize, LineHeight: unit.Sp(18), Color: rc.color(acContext), MaxLines: 1}, "Not in channel")
		rc.right -= gtx.Dp(12)
	}
	av := rc.square(acAvatar, 8)
	rc.ac.drawImage(gtx, it.Avatar, av, gtx.Dp(acAvatarR), acPlaceBg)
	rc.line(acTextSize, font.Bold, acText, it.Name)
	if it.Self {
		rc.gap(4)
		rc.line(acTextSize, font.Bold, acText, "(you)")
	}
	rc.gap(4)
	if it.Kind == acUser && it.Atom != nil && it.Atom.Kind == AtomUser && !it.IsBot && rc.x < rc.right {
		box := rc.square(acPresBox, 4)
		drawPresence(gtx, box, it.Presence == "active", rc.sel)
	}
	if it.Secondary != "" && rc.x < rc.right {
		rc.line(acTextSize, font.Normal, acText, it.Secondary)
		rc.gap(4)
	}
	if (it.Status != "" || it.StatusURL != "") && rc.x+gtx.Dp(8) < rc.right {
		rc.gap(4)
		if it.StatusURL != "" {
			em := rc.square(acStatus, 4)
			rc.ac.drawImage(gtx, it.StatusURL, em, 0, color.NRGBA{})
		}
		if it.Status != "" && rc.x < rc.right {
			rc.line(acTextSize, font.Normal, acText, it.Status)
		}
	}
}

func (rc *acRowCtx) group(it acItem, team string) {
	gtx := rc.gtx
	ic := rc.square(acIcon, 8)
	drawAt(gtx, ic, func(gtx layout.Context, s int) { drawPeopleIcon(gtx, s, rc.color(acText)) })
	rc.line(acTextSize, font.Bold, acText, it.Name)
	rc.gap(8)
	members := "members"
	if it.Count == 1 {
		members = "member"
	}
	if rc.x < rc.right {
		rc.line(acTextSize, font.Normal, acText, "("+strconv.Itoa(it.Count)+" "+members+")")
	}
	if team != "" && rc.x+gtx.Dp(4) < rc.right {
		rc.gap(4)
		rc.line(acSmallSize, font.Normal, withAlpha(rc.color(acText), 0xb3), team)
	}
}

func (rc *acRowCtx) broadcast(it acItem) {
	gtx := rc.gtx
	ic := rc.square(acIcon, 8)
	drawAt(gtx, ic, func(gtx layout.Context, s int) { drawPeopleIcon(gtx, s, rc.color(acText)) })
	rc.line(acTextSize, font.Bold, acText, "@"+it.Name)
	if it.Secondary != "" && rc.x+gtx.Dp(8) < rc.right {
		rc.gap(8)
		rc.line(acTextSize, font.Normal, acText, it.Secondary)
	}
}

func (rc *acRowCtx) channel(it acItem) {
	gtx := rc.gtx
	col := acText
	name := it.Name
	if it.Archived {
		col, name = acContext, name+" (archived)"
	}
	ic := rc.square(acIcon, 8)
	c := rc.color(col)
	drawAt(gtx, ic, func(gtx layout.Context, s int) {
		switch {
		case it.Archived:
			drawArchiveIcon(gtx, s, c)
		case it.Private:
			drawLockIcon(gtx, s, c)
		default:
			drawHashIcon(gtx, s, c)
		}
	})
	rc.line(acTextSize, font.Bold, col, name)
}

func (rc *acRowCtx) emoji(it acItem, q string) {
	gtx := rc.gtx
	img := rc.square(acEmojiPx, 8)
	rc.ac.drawImage(gtx, it.Avatar, img, 0, color.NRGBA{})
	s := textStyle{Size: acTextSize, Color: rc.color(acText), MaxLines: 1}
	q = strings.ToLower(strings.Trim(q, ":"))
	at := -1
	if q != "" {
		at = strings.Index(strings.ToLower(it.Name), q)
	}
	if at < 0 || at+len(q) > len(it.Name) {
		rc.text(0, rc.rowH, s, ":"+it.Name+":")
		return
	}
	rc.text(0, rc.rowH, s, ":"+it.Name[:at])
	hi := it.Name[at : at+len(q)]
	hs := s
	bg := acHiBg
	if rc.sel {
		hs.Color, bg = acHiSelText, acHiSelBg
	}
	if rc.x < rc.right {
		tw := min(rc.ac.measure(gtx, hs, hi), rc.right-rc.x)
		lh := gtx.Sp(unit.Sp(22))
		y := (rc.rowH - lh) / 2
		fillRect(gtx, image.Rect(rc.x, y, rc.x+tw, y+lh), bg)
		rc.text(0, rc.rowH, hs, hi)
	}
	if rc.x < rc.right {
		rc.text(0, rc.rowH, s, it.Name[at+len(q):]+":")
	}
}

// command rows are 60dp: a 20dp icon and two lines (name + usage, then
// "Slack · description").
func (rc *acRowCtx) command(it acItem) {
	gtx := rc.gtx
	top := gtx.Dp(10)
	ic := gtx.Dp(acAvatar)
	icon := image.Rect(rc.x, top+gtx.Dp(4), rc.x+ic, top+gtx.Dp(4)+ic)
	if it.Command != nil && it.Command.IconURL != "" {
		// App command: the app's icon (placeholder gray while it loads).
		fillRRect(gtx, icon, gtx.Dp(acAvatarR), acPlaceBg)
		ac.drawImage(gtx, it.Command.IconURL, icon, gtx.Dp(acAvatarR), acPlaceBg)
	} else {
		// Built-in (core) command: the slash glyph.
		fillRRect(gtx, icon, gtx.Dp(acAvatarR), acPlaceBg)
		drawAt(gtx, icon, func(gtx layout.Context, s int) { drawSlashGlyph(gtx, s, acHelpText) })
	}
	x0 := rc.x + ic + gtx.Dp(8)

	rc.x = x0
	l1 := gtx.Dp(22)
	bold := textStyle{Size: acTextSize, LineHeight: unit.Sp(22), Weight: font.Bold, Color: rc.color(acText), MaxLines: 1}
	rc.text(top, l1, bold, "/"+it.Name)
	if it.Command != nil && it.Command.Usage != "" && rc.x+gtx.Dp(4) < rc.right {
		rc.gap(4)
		rc.text(top, l1, bold, it.Command.Usage)
	}

	rc.x = x0
	l2 := gtx.Dp(18)
	small := textStyle{Size: acSmallSize, LineHeight: unit.Sp(18), Color: rc.color(acCmdDesc), MaxLines: 1}
	tb := small
	tb.Weight = font.Bold
	rc.text(top+l1, l2, tb, cmdSource(it.Command))
	if it.Secondary != "" && rc.x < rc.right {
		rc.text(top+l1, l2, small, " · "+it.Secondary)
	}
}

// cmdSource is the bold lead of a command's description line: "Slack" for
// built-ins, "App · <AppName>" for app commands (matching Slack).
func cmdSource(c *core.Command) string {
	if c == nil || c.Type == "" || c.Type == "core" {
		return "Slack"
	}
	if c.AppName != "" {
		return "App · " + c.AppName
	}
	return "App"
}

// commandWidth is the content-fit width of a / popup, clamped to the
// measured min and max.
func (ac *Autocomplete) commandWidth(gtx layout.Context, spec acBox, scrollW int) int {
	v := &ac.view
	if v.cmdVer == ac.itemsVer && v.cmdW > 0 {
		return v.cmdW
	}
	bold := textStyle{Size: acTextSize, Weight: font.Bold, MaxLines: 1}
	small := textStyle{Size: acSmallSize, MaxLines: 1}
	tb := small
	tb.Weight = font.Bold
	widest := 0
	for _, it := range ac.items {
		l1 := ac.measure(gtx, bold, "/"+it.Name)
		if it.Command != nil && it.Command.Usage != "" {
			l1 += gtx.Dp(4) + ac.measure(gtx, bold, it.Command.Usage)
		}
		l2 := ac.measure(gtx, tb, cmdSource(it.Command))
		if it.Secondary != "" {
			l2 += ac.measure(gtx, small, " · "+it.Secondary)
		}
		widest = max(widest, l1, l2)
	}
	chip := ac.measure(gtx, textStyle{Size: acSmallSize, MaxLines: 1}, "Enter") + gtx.Dp(8+8)
	w := gtx.Dp(acRowPadX) + gtx.Dp(acAvatar) + gtx.Dp(8) + widest + chip + gtx.Dp(acRowPadX) + scrollW
	w = min(max(w, gtx.Dp(spec.minW)), gtx.Dp(spec.maxW))
	v.cmdW, v.cmdVer = w, ac.itemsVer
	return w
}

// layoutFooter draws the "↑↓ to navigate  ↵ to select  esc to dismiss"
// help row.
func (ac *Autocomplete) layoutFooter(gtx layout.Context, r image.Rectangle) {
	fillRect(gtx, r, acHelpBg)
	line := max(gtx.Dp(1), 1)
	fillRect(gtx, image.Rect(r.Min.X, r.Max.Y-line, r.Max.X, r.Max.Y), acRing)
	s := textStyle{Size: acHelpSize, Color: acHelpText, MaxLines: 1}
	x := r.Min.X + gtx.Dp(acFooterPadX)
	right := r.Max.X - gtx.Dp(acFooterPadX)
	y, h := r.Min.Y, r.Dy()
	icon := func(wdp unit.Dp, draw func(gtx layout.Context, w, h int)) {
		iw, ih := gtx.Dp(wdp), gtx.Dp(12)
		func() {
			defer op.Offset(image.Pt(x, y+(h-ih)/2)).Push(gtx.Ops).Pop()
			draw(gtx, iw, ih)
		}()
		x += iw
	}
	txt := func(st textStyle, t string) {
		if x < right {
			x += ac.drawText(gtx, x, y, h, right-x, st, t)
		}
	}
	space := gtx.Dp(3)
	icon(10, func(gtx layout.Context, w, h int) { drawArrowGlyph(gtx, w, h, true, acHelpText) })
	x += space
	icon(10, func(gtx layout.Context, w, h int) { drawArrowGlyph(gtx, w, h, false, acHelpText) })
	x += space
	txt(s, "to navigate")
	x += gtx.Dp(16)
	icon(12, func(gtx layout.Context, w, h int) { drawReturnGlyph(gtx, w, h, acHelpText) })
	x += space
	txt(s, "to select")
	x += gtx.Dp(16)
	b := s
	b.Weight = font.Bold
	txt(b, "esc")
	x += space
	txt(s, "to dismiss")
}

// drawText draws a one-line label vertically centred in [y, y+h) at x,
// at most maxW wide, and returns its width.
func (ac *Autocomplete) drawText(gtx layout.Context, x, y, h, maxW int, s textStyle, txt string) int {
	if maxW <= 0 || txt == "" {
		return 0
	}
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(maxW, max(h, 1)*4)}
	dims := label(g, ac.h.Theme().Shaper, s, txt)
	call := m.Stop()
	defer op.Offset(image.Pt(x, y+(h-dims.Size.Y)/2)).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
	return dims.Size.X
}

// measure returns the width of a one-line label without drawing it.
func (ac *Autocomplete) measure(gtx layout.Context, s textStyle, txt string) int {
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(1<<20, 1<<10)}
	dims := label(g, ac.h.Theme().Shaper, s, txt)
	m.Stop()
	return dims.Size.X
}

// drawImage paints the cached image at url fitted into r with corner
// radius rad, or placeholder (if non-transparent) while it loads.
func (ac *Autocomplete) drawImage(gtx layout.Context, url string, r image.Rectangle, rad int, placeholder color.NRGBA) {
	size := r.Dx()
	var img paint.ImageOp
	ok := false
	if c := ac.h.Images(); c != nil && url != "" {
		img, ok = c.Get(url, size)
	}
	sz := img.Size()
	if !ok || sz.X == 0 || sz.Y == 0 {
		if placeholder.A != 0 {
			fillRRect(gtx, r, rad, placeholder)
		}
		return
	}
	scale := float32(size) / float32(max(sz.X, sz.Y))
	dw, dh := int(float32(sz.X)*scale+.5), int(float32(sz.Y)*scale+.5)
	at := image.Pt(r.Min.X+(r.Dx()-dw)/2, r.Min.Y+(r.Dy()-dh)/2)
	defer op.Offset(at).Push(gtx.Ops).Pop()
	defer clip.UniformRRect(image.Rect(0, 0, dw, dh), rad).Push(gtx.Ops).Pop()
	defer op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(scale, scale))).Push(gtx.Ops).Pop()
	img.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// drawDropShadow approximates box-shadow 0 4px 12px rgba(0,0,0,alpha)
// with stacked translucent rounded rects spreading out to the blur radius.
func drawDropShadow(gtx layout.Context, box image.Rectangle, rad int, alpha float32) {
	const steps = 6
	blur := gtx.Dp(6) // half the 12px blur on each side
	dy := gtx.Dp(4)
	a := uint8(alpha*255/steps + .5)
	for k := steps; k >= 1; k-- {
		g := blur * k / steps
		r := image.Rect(box.Min.X-g, box.Min.Y-g+dy, box.Max.X+g, box.Max.Y+g+dy)
		fillRRect(gtx, r, rad+g, color.NRGBA{A: a})
	}
}

// drawScrollbar draws a Chrome-style classic scrollbar (arrow buttons,
// track, thumb) in r for a viewport of view pixels over content pixels
// scrolled by top.
func drawScrollbar(gtx layout.Context, r image.Rectangle, top, content, view int) {
	fillRect(gtx, r, acTrack)
	bw := r.Dx()
	tri := func(cy int, up bool) {
		cx := r.Min.X + bw/2
		hw := float32(gtx.Dp(3.5))
		hh := float32(gtx.Dp(2))
		var p clip.Path
		p.Begin(gtx.Ops)
		if up {
			p.MoveTo(f32.Pt(float32(cx)-hw, float32(cy)+hh))
			p.LineTo(f32.Pt(float32(cx)+hw, float32(cy)+hh))
			p.LineTo(f32.Pt(float32(cx), float32(cy)-hh))
		} else {
			p.MoveTo(f32.Pt(float32(cx)-hw, float32(cy)-hh))
			p.LineTo(f32.Pt(float32(cx)+hw, float32(cy)-hh))
			p.LineTo(f32.Pt(float32(cx), float32(cy)+hh))
		}
		p.Close()
		defer clip.Outline{Path: p.End()}.Op().Push(gtx.Ops).Pop()
		paint.ColorOp{Color: acArrow}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
	}
	tri(r.Min.Y+bw/2, true)
	tri(r.Max.Y-bw/2, false)
	trackTop, trackLen := r.Min.Y+bw, r.Dy()-2*bw
	if trackLen <= 0 || content <= view {
		return
	}
	thumb := max(trackLen*view/content, gtx.Dp(17))
	thumb = min(thumb, trackLen)
	y := trackTop + (trackLen-thumb)*min(top, content-view)/(content-view)
	inset := gtx.Dp(2)
	fillRect(gtx, image.Rect(r.Min.X+inset, y, r.Max.X-inset, y+thumb), acThumb)
}

// drawPresence draws the 9dp presence glyph centred in box: a filled
// green dot when active, a gray ring when away (white on a selected row).
func drawPresence(gtx layout.Context, box image.Rectangle, active, sel bool) {
	d := gtx.Dp(acPresDot)
	c := box.Min.Add(image.Pt(box.Dx()/2, box.Dy()/2))
	r := image.Rect(c.X-d/2, c.Y-d/2, c.X-d/2+d, c.Y-d/2+d)
	col := acAway
	if active {
		col = acActive
	}
	if sel {
		col = acWhite
	}
	if active {
		defer clip.Ellipse(r).Push(gtx.Ops).Pop()
		paint.ColorOp{Color: col}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		return
	}
	strokeRRect(gtx, r, d/2, max(gtx.Dp(2), 1), col)
}

// drawAt runs f translated to r.Min with the square side r.Dx().
func drawAt(gtx layout.Context, r image.Rectangle, f func(gtx layout.Context, s int)) {
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	f(gtx, r.Dx())
}

func strokePath(gtx layout.Context, build func(p *clip.Path), width float32, col color.NRGBA) {
	var p clip.Path
	p.Begin(gtx.Ops)
	build(&p)
	defer clip.Stroke{Path: p.End(), Width: width}.Op().Push(gtx.Ops).Pop()
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// drawHashIcon draws the channel '#' in an s×s box.
func drawHashIcon(gtx layout.Context, s int, col color.NRGBA) {
	f := float32(s)
	strokePath(gtx, func(p *clip.Path) {
		p.MoveTo(f32.Pt(f*0.43, f*0.17))
		p.LineTo(f32.Pt(f*0.34, f*0.83))
		p.MoveTo(f32.Pt(f*0.68, f*0.17))
		p.LineTo(f32.Pt(f*0.59, f*0.83))
		p.MoveTo(f32.Pt(f*0.2, f*0.38))
		p.LineTo(f32.Pt(f*0.84, f*0.38))
		p.MoveTo(f32.Pt(f*0.16, f*0.62))
		p.LineTo(f32.Pt(f*0.8, f*0.62))
	}, f*0.09, col)
}

// drawLockIcon draws a padlock in an s×s box.
func drawLockIcon(gtx layout.Context, s int, col color.NRGBA) {
	f := float32(s)
	body := image.Rect(int(f*0.22), int(f*0.45), int(f*0.78), int(f*0.86))
	fillRRect(gtx, body, max(int(f*0.08), 1), col)
	strokePath(gtx, func(p *clip.Path) {
		p.MoveTo(f32.Pt(f*0.33, f*0.47))
		p.LineTo(f32.Pt(f*0.33, f*0.33))
		p.CubeTo(f32.Pt(f*0.33, f*0.12), f32.Pt(f*0.67, f*0.12), f32.Pt(f*0.67, f*0.33))
		p.LineTo(f32.Pt(f*0.67, f*0.47))
	}, f*0.09, col)
}

// drawArchiveIcon draws an archive box (lid plus body) in an s×s box.
func drawArchiveIcon(gtx layout.Context, s int, col color.NRGBA) {
	f := float32(s)
	w := f * 0.08
	strokePath(gtx, func(p *clip.Path) {
		// lid
		p.MoveTo(f32.Pt(f*0.14, f*0.2))
		p.LineTo(f32.Pt(f*0.86, f*0.2))
		p.LineTo(f32.Pt(f*0.86, f*0.36))
		p.LineTo(f32.Pt(f*0.14, f*0.36))
		p.Close()
		// body
		p.MoveTo(f32.Pt(f*0.2, f*0.36))
		p.LineTo(f32.Pt(f*0.2, f*0.82))
		p.LineTo(f32.Pt(f*0.8, f*0.82))
		p.LineTo(f32.Pt(f*0.8, f*0.36))
		// handle
		p.MoveTo(f32.Pt(f*0.4, f*0.5))
		p.LineTo(f32.Pt(f*0.6, f*0.5))
	}, w, col)
}

// drawPeopleIcon draws the two-heads usergroup outline in an s×s box.
func drawPeopleIcon(gtx layout.Context, s int, col color.NRGBA) {
	f := float32(s)
	w := f * 0.08
	circle := func(p *clip.Path, cx, cy, r float32) {
		const k = 0.5523
		p.MoveTo(f32.Pt(cx+r, cy))
		p.CubeTo(f32.Pt(cx+r, cy+r*k), f32.Pt(cx+r*k, cy+r), f32.Pt(cx, cy+r))
		p.CubeTo(f32.Pt(cx-r*k, cy+r), f32.Pt(cx-r, cy+r*k), f32.Pt(cx-r, cy))
		p.CubeTo(f32.Pt(cx-r, cy-r*k), f32.Pt(cx-r*k, cy-r), f32.Pt(cx, cy-r))
		p.CubeTo(f32.Pt(cx+r*k, cy-r), f32.Pt(cx+r, cy-r*k), f32.Pt(cx+r, cy))
		p.Close()
	}
	strokePath(gtx, func(p *clip.Path) {
		circle(p, f*0.38, f*0.32, f*0.15)
		p.MoveTo(f32.Pt(f*0.1, f*0.86))
		p.CubeTo(f32.Pt(f*0.12, f*0.58), f32.Pt(f*0.64, f*0.58), f32.Pt(f*0.66, f*0.86))
		circle(p, f*0.7, f*0.36, f*0.11)
		p.MoveTo(f32.Pt(f*0.7, f*0.6))
		p.CubeTo(f32.Pt(f*0.84, f*0.6), f32.Pt(f*0.92, f*0.7), f32.Pt(f*0.92, f*0.86))
	}, w, col)
}

// drawSlashGlyph draws a '/' in an s×s box (placeholder app icon).
func drawSlashGlyph(gtx layout.Context, s int, col color.NRGBA) {
	f := float32(s)
	strokePath(gtx, func(p *clip.Path) {
		p.MoveTo(f32.Pt(f*0.62, f*0.25))
		p.LineTo(f32.Pt(f*0.38, f*0.75))
	}, f*0.1, col)
}

// drawArrowGlyph draws an up or down arrow in a w×h box.
func drawArrowGlyph(gtx layout.Context, w, h int, up bool, col color.NRGBA) {
	fw, fh := float32(w), float32(h)
	sw := float32(gtx.Dp(1.25))
	strokePath(gtx, func(p *clip.Path) {
		cx := fw / 2
		t, b := fh*0.12, fh*0.88
		p.MoveTo(f32.Pt(cx, t))
		p.LineTo(f32.Pt(cx, b))
		if up {
			p.MoveTo(f32.Pt(fw*0.15, fh*0.42))
			p.LineTo(f32.Pt(cx, t))
			p.LineTo(f32.Pt(fw*0.85, fh*0.42))
		} else {
			p.MoveTo(f32.Pt(fw*0.15, fh*0.58))
			p.LineTo(f32.Pt(cx, b))
			p.LineTo(f32.Pt(fw*0.85, fh*0.58))
		}
	}, sw, col)
}

// drawReturnGlyph draws the ↵ return arrow in a w×h box.
func drawReturnGlyph(gtx layout.Context, w, h int, col color.NRGBA) {
	fw, fh := float32(w), float32(h)
	sw := float32(gtx.Dp(1.25))
	strokePath(gtx, func(p *clip.Path) {
		p.MoveTo(f32.Pt(fw*0.88, fh*0.15))
		p.LineTo(f32.Pt(fw*0.88, fh*0.62))
		p.LineTo(f32.Pt(fw*0.12, fh*0.62))
		p.MoveTo(f32.Pt(fw*0.38, fh*0.36))
		p.LineTo(f32.Pt(fw*0.12, fh*0.62))
		p.LineTo(f32.Pt(fw*0.38, fh*0.88))
	}, sw, col)
}

// acLight is the measured light palette; acApplyTheme swaps the package
// colors for dark mode (Slack's dark popover tones) and back.
var acLight = [...]color.NRGBA{acBg, acRing, acPseudoBg, acText, acContext, acHelpText, acHelpBg, acCmdDesc, acPlaceBg, acAway, acChipBg, acChipLine, acTrack, acThumb, acArrow}

var acDarkOn bool

func acApplyTheme(dark bool) {
	if dark == acDarkOn {
		return
	}
	acDarkOn = dark
	p := acLight
	if dark {
		p = [...]color.NRGBA{
			rgb(0x222529), argb(0x21e8e8e8), argb(0x0fe8e8e8), rgb(0xd1d2d3), rgb(0xababad), rgb(0xababad),
			argb(0x0ae8e8e8), argb(0xb3d1d2d3), rgb(0x313338), rgb(0xababad), rgb(0x313338), argb(0x21e8e8e8),
			rgb(0x2c2d30), rgb(0x5d5e61), rgb(0xababad),
		}
	}
	acBg, acRing, acPseudoBg, acText, acContext, acHelpText, acHelpBg, acCmdDesc, acPlaceBg, acAway, acChipBg, acChipLine, acTrack, acThumb, acArrow = p[0], p[1], p[2], p[3], p[4], p[5], p[6], p[7], p[8], p[9], p[10], p[11], p[12], p[13], p[14]
}
