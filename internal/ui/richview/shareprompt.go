package richview

// A share_prompt block: Slack's SharePromptButton (boot-shared CvC8, its
// header SharePromptHeader 9eVz; CDN build 9f450b8ad3db), from
// client-boot-styles:
//
//   - the card (Card elevation 2, size medium: .c-card_container, radius
//     12, the 1px ring; .promptCard: no padding, width 100%, max-width
//     426px and margin 8px 0 outside canvases);
//   - .promptCardHeader (padding 12px 12px 0): the 40px tile
//     (.promptCardHeaderIconBackground: #83388a, radius 4, 8px around the
//     24px white ai-thread-summary) 12px before the label (bold, one line)
//     over "Prompt" (13px content-sec);
//   - .promptCardBody (padding 12px): the prompt, three lines and Slack's
//     "Show more" (ExpandableText's contractedLines 3), then
//     .promptCardActions (margin-top 12px): Send to {agentName}, a medium
//     button with send-filled 4px before its label.
//
// Only a SharePrompt richtext Shows draws. Left out: Forward prompt (the
// header's hover button opens PromptShareDialog, which Clack doesn't
// have), the attached skills, the card's 3px drop shadow, and the
// ai-thread-summary glyph (Clack draws its sparkles).

import (
	"image"
	"image/color"

	"gioui.org/font"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
)

var nsAgents = i18n.NS("agents")

// SharePromptHandler is optional on a Handler: a share_prompt's Send to
// {agentName} was clicked.
type SharePromptHandler interface {
	SharePromptSend(p *richtext.SharePrompt)
}

// sharePromptKey is a prompt's "Show more" toggle in the View's collapsed
// map (true: expanded).
type sharePromptKey struct{ p *richtext.SharePrompt }

// sharePrompt lays out a share_prompt block.
func (l *lay) sharePrompt(b *richtext.Block, x, y, w int) int {
	sp := b.Prompt
	if sp == nil || !sp.Shown {
		return 0
	}
	p := &l.st.Palette
	cw := w
	if !l.opts.Canvas {
		cw = min(w, l.dp(426))
	}
	pad := l.dp(12)
	// the header
	tile := l.dp(40)
	hy := y + pad
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(x+pad, hy, x+pad+tile, hy+tile), radius: l.dp(4), fill: color.NRGBA{0x83, 0x38, 0x8a, 0xff}, target: -1})
	is := l.dp(24)
	ix, iy := x+pad+(tile-is)/2, hy+(tile-is)/2
	l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(ix, iy, ix+is, iy+is), url: iconURL + "sparkles", target: -1, px: is, tint: color.NRGBA{0xff, 0xff, 0xff, 0xff}})
	tx := x + pad + tile + pad
	tw := max(x+cw-pad-tx, 1)
	fs := l.bodyStyle()
	fs.face.Weight = font.Bold
	fs.maxLines, fs.ellipsis = 1, true
	l.plainAdd("\n")
	th, _ := l.plainFlow(sp.Label, tx, hy, tw, &fs, -1)
	ss := l.small(l.tok("content-sec"))
	l.plainAdd("\n")
	l.plainFlow(nsAgents.T("Prompt"), tx, hy+th, tw, &ss, -1)
	by := hy + tile + pad
	// the prompt: three lines, or all of it once Show more was clicked
	bw := cw - 2*pad
	key := sharePromptKey{sp}
	open := l.collapsed != nil && l.collapsed[key]
	ps := l.bodyStyle()
	if !open {
		ps.maxLines, ps.ellipsis = 3, true
	}
	l.plainAdd("\n")
	var ph int
	if len(sp.Rich) > 0 {
		l.addSpans(sp.Rich, &ps)
		ph, _ = l.flow(x+pad, by, bw, &ps)
	} else {
		ph, _ = l.plainFlow(sp.Prompt, x+pad, by, bw, &ps, -1)
	}
	yy := by + ph
	if !open && l.lastTruncated {
		ls := l.bodyStyle()
		ls.col = p.Link
		tg := l.d.addTarget(target{kind: tCollapse, key: key, underline: true})
		l.plainAdd("\n")
		sh, sw := l.plainFlow(i18n.T("agents", "Show more"), x+pad, yy, bw, &ls, tg)
		l.d.hits = append(l.d.hits, hit{r: image.Rect(x+pad, yy, x+pad+sw, yy+sh), t: tg})
		yy += sh
	}
	// Send to {agentName}
	yy += l.dp(12)
	label := nsAgents.T("Send to {agentName}", "agentName", sp.AgentName)
	btnH := l.dp(36)
	icon := l.dp(20)
	bs := l.bodyStyle()
	bs.face.Weight = font.Bold
	bs.maxLines, bs.ellipsis, bs.noSelect = 1, true, true
	lw := int(l.measure(label, l.faceFor(bs.face, bs.size, 0)) + 0.999)
	inner := icon + l.dp(4) + lw
	btnW := min(inner+2*l.dp(12), bw)
	br := image.Rect(x+pad, yy, x+pad+btnW, yy+btnH)
	tg := l.d.addTarget(target{kind: tSharePrompt, prompt: sp})
	l.d.boxes = append(l.d.boxes, box{kind: bBorder, r: br, radius: l.dp(8), fill: p.Panel, stroke: p.ButtonBorder, bw: l.dp(1), target: tg, hover: p.ButtonHoverBg})
	l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(br.Min.X+l.dp(12), yy+(btnH-icon)/2, br.Min.X+l.dp(12)+icon, yy+(btnH-icon)/2+icon), url: iconURL + "send", target: tg, px: icon, tint: p.Text})
	bs.lineH = btnH
	l.plainAdd("\n")
	l.plainFlow(label, br.Min.X+l.dp(12)+icon+l.dp(4), yy, max(btnW-2*l.dp(12)-icon-l.dp(4), 1), &bs, tg)
	l.d.hits = append(l.d.hits, hit{r: br, t: tg})
	h := yy + btnH + pad - y
	// the card under all of it
	l.d.boxes = append(l.d.boxes, box{})
	copy(l.d.boxes[l.boxBase+1:], l.d.boxes[l.boxBase:])
	l.d.boxes[l.boxBase] = box{kind: bBorder, r: image.Rect(x, y, x+cw, y+h), radius: l.dp(12), fill: p.Panel, stroke: p.Border, bw: l.dp(1), target: -1}
	return h
}
