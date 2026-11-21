package richview

import (
	"image"
	"strconv"

	"gioui.org/font"

	"github.com/some-du6e/clack/internal/richtext"
)

// callCard lays out a call block the way Slack's PlatformCallSubtype
// (boot-shared 9CtN, build 846030e1ce97; client-boot-styles) draws it:
//
//   - "Call" in .c-message__body--automated (sk_foreground_max_solid,
//     the palette's Text2), the ExpandableTrigger right after it: the
//     legacy caret_down glyph (caret_right while collapsed) in the
//     text's colour; collapsed, nothing else shows. The body's :after
//     adds 4px under it all;
//   - .p-call_subtype, 4px under the line and 8px above the :after: at
//     most 440 wide, 1px otl-ter border, 8px corners, the primary
//     background;
//   - __header (padding 16, flex-start): the 36px __icon (4px corners,
//     12px right margin, #0b4c8c behind the app's icon or the white
//     phone glyph; #868686 once the call ended), then the title (15px
//     bold) over the status (15px, one line, ellipsis);
//   - __body (only with a display_id): 1px otl-ter top border on
//     sk_foreground_min .04, padding 10px 16px, "Meeting ID: …";
//   - __footer: the same border and fill, padding 16, its bottom corners
//     6px: the facepile (36px faces 8px apart, at most 5, the last one
//     "+N" over the rest; flex-grow) then the description (15px,
//     sk_foreground_max .7) and the medium c-button (36px, 15px bold,
//     padding 0 12px, min-width 80, 8px corners; primary while the call
//     runs, else outline). Without a facepile the description comes
//     first and the button sits at the far right.
func (l *lay) callCard(c *richtext.CallCard, x, y, w int) int {
	if c == nil {
		return 0
	}
	p := &l.st.Palette
	y0 := y
	fs := l.bodyStyle()
	fs.col = p.Text2
	if l.opts.Muted {
		fs.col = p.Text3
	}
	lh, tw := l.plainFlow("Call", x, y, w, &fs, -1)
	// ExpandableTrigger
	var key any = c // a call without an id: this Doc's card
	if c.ID != "" {
		key = "call:" + c.ID
	}
	collapsed := l.collapsed[key]
	tg := l.d.addTarget(target{kind: tCollapse, key: key})
	cx := x + tw + l.dp(4) + l.dp(4)
	cy := y + fs.lineH/2
	kind, cr := bCaret, image.Rect(cx-l.dp(4), cy-l.dp(2), cx+l.dp(4), cy+l.dp(3))
	if collapsed {
		kind, cr = bCaretRight, image.Rect(cx-l.dp(2), cy-l.dp(4), cx+l.dp(3), cy+l.dp(4))
	}
	l.d.boxes = append(l.d.boxes, box{kind: kind, r: cr, fill: fs.col, target: -1})
	l.d.hits = append(l.d.hits, hit{r: image.Rect(cx-l.dp(8), cy-l.dp(8), cx+l.dp(8), cy+l.dp(8)), t: tg})
	y += max(lh, fs.lineH)
	if collapsed {
		return y + l.dp(4) - y0
	}
	l.plainAdd("\n")
	y += l.dp(4) // .p-call_subtype margin-top
	cw := min(w, l.dp(440))
	bw := max(1, l.dp(1))
	card := len(l.d.boxes) // the card's box goes under everything in it
	top := y
	pad := l.dp(16)

	// header
	icon := l.dp(36)
	ir := image.Rect(x+bw+pad, y+bw+pad, x+bw+pad+icon, y+bw+pad+icon)
	iconBg := rgb(0x0b, 0x4c, 0x8c)
	if c.Ended {
		iconBg = rgb(0x86, 0x86, 0x86)
	}
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: ir, radius: l.dp(4), fill: iconBg, target: -1})
	if c.Icon != "" {
		rad := l.dp(4)
		l.d.imgs = append(l.d.imgs, imgPrim{r: ir, url: c.Icon, radii: [4]int{rad, rad, rad, rad}, target: -1, px: icon})
	} else {
		gs := l.dp(20)
		gr := image.Rect(0, 0, gs, gs).Add(image.Pt(ir.Min.X+(icon-gs)/2, ir.Min.Y+(icon-gs)/2))
		l.d.imgs = append(l.d.imgs, imgPrim{r: gr, url: iconURL + callGlyph(c.Glyph), target: -1, px: gs, tint: rgb(255, 255, 255)})
	}
	tx := ir.Max.X + l.dp(12)
	tw2 := max(x+cw-bw-pad-tx, l.dp(40))
	// .p-call_subtype__title and __status: rgba(var(--sk_primary_foreground),1)
	ts := l.bodyStyle()
	ts.face.Weight = font.Bold
	ts.col = Over(l.skRGBA("sk_primary_foreground", 1), p.Panel)
	th, _ := l.plainFlow(c.Title, tx, ir.Min.Y, tw2, &ts, -1)
	l.plainAdd("\n")
	ss := l.bodyStyle()
	ss.maxLines, ss.ellipsis = 1, true
	ss.col = ts.col
	sh, _ := l.plainFlow(c.Status, tx, ir.Min.Y+th, tw2, &ss, -1)
	y = ir.Min.Y + max(icon, th+sh) + pad

	// body and footer: a 1px top border on the faint fill
	panelFill := Over(rgba(p.Text.R, p.Text.G, p.Text.B, 0.04), p.Panel)
	descCol := Over(l.skRGBA("sk_foreground_max", 0.7), panelFill) // .p-call_subtype__description
	section := func(h int, last bool) image.Rectangle {
		r := image.Rect(x+bw, y, x+cw-bw, y+bw+h)
		b := box{kind: bFill, r: r, fill: panelFill, target: -1}
		if last {
			in := max(l.dp(8)-bw, 0)
			b.perCorner, b.corners = true, [4]int{0, 0, in, in}
		}
		l.d.boxes = append(l.d.boxes, b, box{kind: bFill, r: image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+bw), fill: p.Border, target: -1})
		return r
	}
	ds := l.bodyStyle()
	ds.col = descCol
	if c.MeetingID != "" {
		bp := l.dp(10)
		mt := "Meeting ID: " + c.MeetingID
		textW := cw - 2*bw - 2*pad
		mh := l.textWidthH(mt, textW, &ds)
		section(bp+mh+bp, c.NoFooter) // the card's last part when there's no footer
		l.plainAdd("\n")
		l.plainFlow(mt, x+bw+pad, y+bw+bp, textW, &ds, -1)
		y += bw + bp + mh + bp
	}

	// footer (renderCallFooter: none for a missed or declined DM call
	// with a join_url whose facepile is hidden)
	if !c.NoFooter {
		inner := cw - 2*bw - 2*pad
		btnH := l.dp(36)
		btnW := 0
		bk := faceKey{f: font.Font{Typeface: l.st.Face, Weight: font.Bold}, size: fixedPx(l.sp(15))}
		if c.Button != "" {
			btnW = max(l.dp(80), int(l.measure(c.Button, bk)+0.999)+2*l.dp(12))
		}
		rowW := inner
		if btnW > 0 {
			rowW -= btnW + l.dp(12)
		}
		faces := len(c.Faces) > 0
		var pileW, shown int
		descX, descW := x+bw+pad, rowW
		if faces {
			descMax := l.textWidth(c.Description, &ds)
			if c.Description == "" {
				descMax = 0
			}
			pileW = max(rowW-descMax-l.dp(12), l.dp(44))
			shown = facesShown(pileW, len(c.Faces), l.dp(44), l.dp(8))
			descX = x + bw + pad + pileW + l.dp(12)
			descW = max(rowW-pileW-l.dp(12), 0)
		}
		dh := 0
		if c.Description != "" && descW > 0 {
			dh = l.textWidthH(c.Description, descW, &ds)
		}
		rowH := max(dh, fs.lineH)
		if faces || btnW > 0 {
			rowH = max(rowH, l.dp(36))
		}
		fr := section(pad+rowH+pad, true)
		fy := fr.Min.Y + bw + pad
		if faces {
			l.facepile(c, x+bw+pad, fy+(rowH-l.dp(36))/2, shown)
		}
		if dh > 0 {
			l.plainAdd("\n")
			l.plainFlow(c.Description, descX, fy+(rowH-dh)/2, descW, &ds, -1)
		}
		if btnW > 0 {
			br := image.Rect(x+cw-bw-pad-btnW, fy+(rowH-btnH)/2, x+cw-bw-pad, fy+(rowH-btnH)/2+btnH)
			btg := l.d.addTarget(target{kind: tCard, url: c.JoinURL})
			col := p.Text
			if c.Primary {
				l.d.boxes = append(l.d.boxes, box{kind: bFill, r: br, radius: l.dp(8), fill: p.Primary, target: btg})
				col = p.PrimaryText
			} else {
				l.d.boxes = append(l.d.boxes, box{kind: bBorder, r: br, radius: l.dp(8), fill: p.Panel, stroke: p.ButtonBorder, bw: bw, target: btg, hover: p.ButtonHoverBg})
			}
			bs := flowStyle{face: bk.f, size: l.sp(15), lineH: btnH, col: col, emoji: l.dp(16), maxLines: 1, noSelect: true}
			tw := int(l.measure(c.Button, bk) + 0.999)
			l.plainFlow(c.Button, br.Min.X+(btnW-tw)/2, br.Min.Y-l.dp(1), tw+l.dp(2), &bs, -1)
			l.d.hits = append(l.d.hits, hit{r: br, t: btg})
		}
		y = fr.Max.Y + bw
	} else {
		y += bw
	}
	// the card itself, under its parts
	cardBox := box{kind: bBorder, r: image.Rect(x, top, x+cw, y), radius: l.dp(8), fill: p.Panel, stroke: p.Border, bw: bw, target: -1}
	l.d.boxes = append(l.d.boxes, box{})
	copy(l.d.boxes[card+1:], l.d.boxes[card:])
	l.d.boxes[card] = cardBox
	y += l.dp(8) + l.dp(4) // margin-bottom, then the body's :after
	return y - y0
}

// callGlyph is the icon set's name for renderCallIcon's y8g2 font icon:
// phone, else the font glyphs (icons/iconfont.go).
func callGlyph(g string) string {
	switch g {
	case "phone-flat":
		return "phone-flat"
	case "call-missed":
		return "call-missed-font"
	case "call-declined":
		return "call-declined-font"
	}
	return "phone"
}

// DefaultAvatar is the image URL of Slack's ava_0002 (messages sets it to
// its embedded copy): an external call participant without an avatar_url,
// or whose avatar_url failed to load (Facepile's
// renderExternalAvatarFallback: getMemberProfileImage's isBot branch).
var DefaultAvatar string

// facesShown is Facepile's maxNumFacesOverride (maxNumFaces 5): fewer
// faces when the pile is narrower than five 44px steps and than its
// faces need (44 each, less the last one's 8px margin).
func facesShown(width, n, step, gap int) int {
	if width < 5*step && !(n*step-gap <= width) {
		return max(width/step, 0)
	}
	return 5
}

// facepile draws Facepile's faces at (x, y): the first shown-1, then the
// last one shown, "+N" (9+ past nine) over it for the rest; greyed when
// the call ended (p-call_facepile--call_ended's grayscale, not drawn).
func (l *lay) facepile(c *richtext.CallCard, x, y, shown int) {
	n := len(c.Faces)
	if shown <= 0 || n == 0 {
		return
	}
	extra := 0
	if n > shown {
		extra = n - shown
	}
	s, gap := l.dp(36), l.dp(8)
	k := min(n, shown)
	for i := 0; i < k; i++ {
		f := c.Faces[i]
		r := image.Rect(x+i*(s+gap), y, x+i*(s+gap)+s, y+s)
		rad := l.dp(8) // the 36px ConnectedBaseAvatar's corners
		if f.UserID == "" {
			rad = l.dp(3) // p-call_facepile__face--external img
		}
		avatar, onFail := f.Avatar, ""
		if f.UserID == "" {
			// renderExternalAvatar: its avatar_url, whose renderError (and a
			// face with none) is renderExternalAvatarFallback, ConnectedBaseAvatar
			// with no member: ava_0002
			if avatar == "" {
				avatar = DefaultAvatar
			} else {
				onFail = DefaultAvatar
			}
		}
		if avatar != "" {
			l.d.imgs = append(l.d.imgs, imgPrim{r: r, url: avatar, radii: [4]int{rad, rad, rad, rad}, target: -1, px: s, onFail: onFail})
		} else {
			l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, radius: rad, fill: l.st.Palette.Placeholder, target: -1})
		}
		if i == k-1 && extra > 0 {
			t := "+" + strconv.Itoa(extra)
			if extra > 9 {
				t = "9+"
			}
			// .c-avatar__additional_overlay: over the face, under its text
			l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, radius: rad, fill: rgba(0, 0, 0, 0.5), target: -1, layer: 2})
			ks := faceKey{f: font.Font{Typeface: l.st.Face, Weight: font.Bold}, size: fixedPx(l.sp(15))}
			fs := flowStyle{face: ks.f, size: l.sp(15), lineH: s, col: rgb(255, 255, 255), emoji: l.dp(16), maxLines: 1, noSelect: true}
			tw := int(l.measure(t, ks) + 0.999)
			l.plainFlow(t, r.Min.X+(s-tw)/2, r.Min.Y, tw+l.dp(2), &fs, -1)
		}
	}
}

// textWidthH is the height s takes wrapped in w (one style), laid out
// on a scratch display so nothing of it is drawn.
func (l *lay) textWidthH(s string, w int, fs *flowStyle) int {
	if s == "" {
		return 0
	}
	saved, lead := l.d, l.lead
	var scratch display
	l.d, l.lead = &scratch, 0
	h, _ := l.plainFlow(s, 0, 0, w, fs, -1)
	l.d, l.lead = saved, lead
	return h
}
