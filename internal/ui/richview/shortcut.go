package richview

import (
	"image"
	"strings"

	"gioui.org/font"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/icons"
)

// WorkflowCardHandler is a Handler for share_shortcut cards: act "open"
// (a click on the card: the workflow details view), "start" (Start
// Workflow: trip the link trigger) or "more" (the hover bar's ⋮: its
// menu, r the button in view coordinates).
type WorkflowCardHandler interface {
	WorkflowCard(act string, c *richtext.ShortcutCard, r image.Rectangle)
}

// DefaultAppIcon is the image URL of Slack's default app icon (the
// bundled default_new_app_icon; popups sets it): ShortcutIcon's last
// resort while the trigger's own icon isn't known.
var DefaultAppIcon string

// shortcutCard lays out a share_shortcut block: WorkflowEntity (boot-shared
// KrN7) on Slack Kit's entity card (VLgN on aJif; client-boot-styles
// .workflow*Container), customWidth 420:
//
//   - the base entity: 1px otl-ter border, radius 12, base-pry; with
//     withHover the otl-sec border while the card (not its bar) is
//     hovered;
//   - .workflowHeaderContainer: padding 12, items centred, gap 12: the
//     36px ThumbnailContainer (radius 8, surface.secondary behind the
//     workflow's icon), then StackedLabels: the title (body, bold, one
//     line) over the trigger type's byline (caption, tertiary, 4px
//     under), a lone label centred on the row; the "Uses AI" sparkle
//     (plt-gray-50, 8px after, 4px up) for contains_ai_steps;
//   - expanded: PreviewContainer (padding 0 16px, no top border) around
//     .workflowContentContainer (line-height 1.5): the description, then
//     16px, or 64px when the Start Workflow button sits in the card's
//     bottom left (.workflowButtonContainer: padding 16; a primary 36px
//     button, the play glyph 4px before "Start Workflow");
//   - the actions bar on hover: More actions (⋮, content-pry while
//     hovered; its menu opens from the button). Forward workflow (the
//     share dialog for a workflow) isn't in Clack, so it isn't drawn.
//
// While the workflow's bot member loads the card is the loading entity
// (skeletons, contentHeight 86); a deleted bot makes it the compact
// card "This workflow no longer exists." with the trash glyph.
func (l *lay) shortcutCard(sc *richtext.ShortcutCard, x, y, w int) int {
	if sc == nil {
		return 0
	}
	p := &l.st.Palette
	cw := min(w, l.dp(420))
	bw := max(1, l.dp(1))
	pad := l.dp(12)
	enabled, loading, deleted := sc.State == "enabled", sc.State == "loading", sc.State == "deleted"
	// KrN7: isInteractive and onClick for every state but deleted (the
	// loading card opens the details and has its actions bar too)
	interactive := !deleted && !l.opts.HoverCard
	box0 := len(l.d.boxes)
	card := int16(-1)
	cardHit := -1
	if interactive {
		card = l.d.addTarget(target{kind: tWorkflowCard, id: "open", key: sc})
		cardHit = len(l.d.hits)
		l.d.hits = append(l.d.hits, hit{t: card})
	}

	// ---- the header ----
	ic := l.dp(36)
	hx, hy := x+bw+pad, y+bw+pad
	// Panda's text styles: body 15px/1.5, caption 13px/1.25
	ts := l.bodyStyle()
	ts.face.Weight = font.Bold
	ts.lineH = int(l.dpf(22.5) + 0.5)
	ts.maxLines, ts.ellipsis = 1, true
	// the byline is WorkflowTriggerTypeLabel (.p-workflow_trigger_type_label:
	// content-sec); the deleted card's caption is StackedLabels' tertiary
	cs := flowStyle{face: font.Font{Typeface: l.st.Face}, size: l.sp(13), lineH: int(l.dpf(16.25) + 0.5), col: l.tok("content-sec"),
		emoji: l.dp(16), maxLines: 1, ellipsis: true}
	title, byline := sc.Title, sc.Byline
	if deleted {
		title, byline = "", "This workflow no longer exists."
		cs.col = l.tok("content-ter")
	}
	labelsH := 0
	switch {
	case loading:
		labelsH = l.dp(8) + l.dp(12) + l.dp(8) // the two skeleton rows, 8px margin and the 4px row gap
	case title != "" && byline != "":
		labelsH = ts.lineH + l.dp(4) + cs.lineH
	case title != "":
		labelsH = ts.lineH
	case byline != "":
		labelsH = cs.lineH
	}
	rowH := max(ic, labelsH)
	iy := hy + (rowH-ic)/2
	ir := image.Rect(hx, iy, hx+ic, iy+ic)
	rad := l.dp(8)
	switch {
	case loading: // LoadingBox: surface.secondary
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: ir, radius: rad, fill: l.tok("surf-sec"), target: -1})
	case deleted:
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: ir, radius: rad, fill: l.tok("surf-sec"), target: -1})
		gs := l.dp(20)
		l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(0, 0, gs, gs).Add(image.Pt(ir.Min.X+(ic-gs)/2, ir.Min.Y+(ic-gs)/2)),
			url: iconURL + "trash", target: -1, px: gs, tint: p.Text})
	default:
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: ir, radius: rad, fill: l.tok("surf-sec"), target: -1})
		if u := firstNonEmpty(sc.Icon, DefaultAppIcon); u != "" {
			l.d.imgs = append(l.d.imgs, imgPrim{r: ir, url: u, radii: [4]int{rad, rad, rad, rad}, target: -1, px: ic})
		}
	}
	lx := ir.Max.X + l.dp(12)
	lw := max(x+cw-bw-pad-lx, l.dp(20))
	if sc.AI { // the badge: 12px gap, its 8px margin, the 15px sparkle
		lw -= l.dp(12) + l.dp(8) + l.dp(15)
	}
	ly := hy + (rowH-labelsH)/2
	labelsEnd := lx // the labels div's right edge: its wider label's
	switch {
	case loading:
		labelsEnd = lx + l.dp(193)
		sk := l.tok("surf-sec")
		r8 := l.dp(8)
		l.d.boxes = append(l.d.boxes,
			box{kind: bFill, r: image.Rect(lx, ly, lx+l.dp(100), ly+r8), radius: r8, fill: sk, target: -1},
			box{kind: bFill, r: image.Rect(lx+l.dp(108), ly, lx+l.dp(193), ly+r8), radius: r8, fill: sk, target: -1},
			box{kind: bFill, r: image.Rect(lx, ly+l.dp(20), lx+l.dp(180), ly+l.dp(28)), radius: r8, fill: sk, target: -1})
	default:
		if title != "" {
			_, tw := l.plainFlow(title, lx, ly, lw, &ts, -1)
			labelsEnd = max(labelsEnd, lx+tw)
			ly += ts.lineH + l.dp(4)
		}
		if byline != "" {
			l.plainAdd("\n")
			bx := lx
			if sc.BylineIcon != "" && (icons.Has(sc.BylineIcon) || icons.HasSlack(sc.BylineIcon)) && !deleted {
				gs := l.dp(13)
				l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(bx, ly+(cs.lineH-gs)/2, bx+gs, ly+(cs.lineH-gs)/2+gs),
					url: iconURL + sc.BylineIcon, target: -1, px: gs, tint: cs.col})
				bx += gs + l.dp(4)
			}
			_, bwid := l.plainFlow(byline, bx, ly, max(lx+lw-bx, l.dp(20)), &cs, -1)
			labelsEnd = max(labelsEnd, bx+bwid)
		}
	}
	if sc.AI { // WorkflowAiBadge, in every state (VLgN): 12px gap, its 8px margin, the sparkle at 1em, 4px up
		gs := l.dp(15)
		gx := min(labelsEnd, lx+lw) + l.dp(12) + l.dp(8)
		gy := hy + (rowH-gs)/2 - l.dp(2)
		l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(gx, gy, gx+gs, gy+gs), url: iconURL + "sparkle", target: -1, px: gs, tint: l.tok("plt-gray-50")})
	}
	yy := hy + rowH + pad

	// ---- the content (expanded only: not for a deleted workflow) ----
	start := enabled && sc.TriggerType == "shortcut" && sc.URL != ""
	btnH := l.dp(36)
	switch {
	case loading: // the 86px LoadingBox, clipped to the frame's corners (overflow: hidden)
		sk := image.Rect(x+bw, yy, x+cw-bw, yy+l.dp(86))
		in := l.dp(12) - bw
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: sk, fill: l.tok("surf-sec"), target: -1, perCorner: true, corners: [4]int{0, 0, in, in}})
		yy += l.dp(86)
	case enabled:
		cx := x + bw + l.dp(16)
		cwid := cw - 2*bw - 2*l.dp(16)
		if sc.Description != "" {
			ds := l.bodyStyle()
			ds.lineH = int(l.dpf(22.5) + 0.5)
			l.plainAdd("\n")
			// a text node without pre-wrap: its newlines collapse to spaces
			h, _ := l.plainFlow(strings.Join(strings.Fields(sc.Description), " "), cx, yy, cwid, &ds, -1)
			yy += h
		}
		if start {
			yy += l.dp(64)
		} else {
			yy += l.dp(16)
		}
	}
	h := yy + bw - y
	r := image.Rect(x, y, x+cw, y+h)
	frame := box{kind: bBorder, r: r, radius: l.dp(12), fill: p.Panel, stroke: l.tok("otl-ter"), bw: bw, target: -1}
	l.d.boxes = append(l.d.boxes, box{})
	copy(l.d.boxes[box0+1:], l.d.boxes[box0:])
	l.d.boxes[box0] = frame
	if cardHit >= 0 {
		l.d.hits[cardHit].r = r
	}
	if deleted && !l.opts.HoverCard {
		// withHover on the deleted card too (KrN7 sets it in every state):
		// the otl-sec border while hovered, with no click (menuOnly)
		tg := l.d.addTarget(target{kind: tWorkflowCard, id: "deleted", key: sc, menuOnly: true})
		l.d.hits = append(l.d.hits, hit{r: r, t: tg})
		border := l.newHover()
		l.d.boxes = append(l.d.boxes, box{kind: bBorder, r: r, radius: l.dp(12), stroke: l.tok("otl-sec"), bw: bw, target: -1, hov: int16(border)})
		l.d.hovers[border-1].card, l.d.hovers[border-1].cardTo = tg, tg
	}

	// ---- Start Workflow ----
	if start {
		bk := faceKey{f: font.Font{Typeface: l.st.Face, Weight: font.Bold}, size: fixedPx(l.sp(15))}
		label := "Start Workflow"
		gs := l.dp(18)
		bwid := l.dp(12) + gs + l.dp(4) + int(l.measure(label, bk)+0.999) + l.dp(12)
		// .workflowButtonContainer{padding:16px;position:absolute;bottom:0;left:0}
		// in the borderless .group: 16px from the card's outer edge
		br := image.Rect(x+l.dp(16), r.Max.Y-l.dp(16)-btnH, x+l.dp(16)+bwid, r.Max.Y-l.dp(16))
		stg := l.d.addTarget(target{kind: tWorkflowCard, id: "start", key: sc})
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: br, radius: l.dp(8), fill: p.Primary, target: stg})
		l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(br.Min.X+l.dp(12), br.Min.Y+(btnH-gs)/2, br.Min.X+l.dp(12)+gs, br.Min.Y+(btnH-gs)/2+gs),
			url: iconURL + "play", target: -1, px: gs, tint: p.PrimaryText})
		fs := flowStyle{face: bk.f, size: l.sp(15), lineH: btnH, col: p.PrimaryText, emoji: l.dp(16), maxLines: 1, noSelect: true}
		l.plainFlow(label, br.Min.X+l.dp(12)+gs+l.dp(4), br.Min.Y-l.dp(1), bwid, &fs, -1)
		l.d.hits = append(l.d.hits, hit{r: br, t: stg})
	}

	// ---- the actions bar on hover, and withHover's border ----
	if interactive {
		bar := l.newHover()
		nb, ni, nf := l.hoverFrom()
		btn := l.dp(32)
		padB := l.dp(4)
		total := 2*padB + 2*bw + btn
		bh := btn + 2*padB + 2*bw
		bx, by := r.Max.X-l.dp(8)-total, r.Min.Y+l.dp(8)
		barR := image.Rect(bx, by, bx+total, by+bh)
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: barR.Add(image.Pt(0, l.dp(1))).Inset(-l.dp(1)), radius: l.dp(13), fill: rgba(0, 0, 0, 0.06), target: -1})
		l.d.boxes = append(l.d.boxes, box{kind: bBorder, r: barR, radius: l.dp(12), fill: p.Panel, stroke: l.tok("otl-ter"), bw: bw, target: -1})
		mtg := l.d.addTarget(target{kind: tWorkflowCard, id: "more", key: sc})
		mr := image.Rect(bx+bw+padB, by+bw+padB, bx+bw+padB+btn, by+bw+padB+btn)
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: mr, radius: l.dp(8), target: mtg, hover: l.tok("base-sec")})
		l.d.hits = append(l.d.hits, hit{r: mr, t: mtg})
		gs := l.dp(18)
		l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(mr.Min.X+(btn-gs)/2, mr.Min.Y+(btn-gs)/2, mr.Min.X+(btn-gs)/2+gs, mr.Min.Y+(btn-gs)/2+gs),
			url: iconURL + "ellipsis-vertical-filled", target: -1, px: gs, tint: l.tok("content-ter")})
		l.hoverEnd(bar, nb, ni, nf)
		hv := &l.d.hovers[bar-1]
		hv.bar, hv.card, hv.cardTo = true, card, int16(len(l.d.targets)-1)
		hv.skip, hv.skipTo = mtg, mtg
		// withHover: the otl-sec border while the card is hovered
		border := l.newHover()
		l.d.boxes = append(l.d.boxes, box{kind: bBorder, r: r, radius: l.dp(12), stroke: l.tok("otl-sec"), bw: bw, target: -1, hov: int16(border)})
		bv := &l.d.hovers[border-1]
		bv.card, bv.cardTo, bv.skip, bv.skipTo = card, int16(len(l.d.targets)-1), mtg, mtg
		// ActionButton's _hover (uJed): the ⋮ turns content-pry
		tint := l.newHover()
		nb, ni, nf = l.hoverFrom()
		l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(mr.Min.X+(btn-gs)/2, mr.Min.Y+(btn-gs)/2, mr.Min.X+(btn-gs)/2+gs, mr.Min.Y+(btn-gs)/2+gs),
			url: iconURL + "ellipsis-vertical-filled", target: -1, px: gs, tint: l.tok("content-pry")})
		l.hoverEnd(tint, nb, ni, nf)
		l.d.hovers[tint-1].card, l.d.hovers[tint-1].cardTo = mtg, mtg
	}
	return h
}
