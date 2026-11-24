package richview

// A share_prompt block: Slack's SharePromptButton (boot-shared CvC8, its
// header SharePromptHeader 9eVz; CDN build 9f450b8ad3db), from
// client-boot-styles:
//
//   - the card (Card elevation 2, size medium: radius 12, the 1px ring;
//     .promptCard: width 100%, max-width 426px outside canvases, margin
//     8px 0 outside them: blocks.go's margins);
//   - .promptCardHeader (padding 12px 12px 0): the 40px tile
//     (.promptCardHeaderIconBackground: #83388a, radius 4, 8px around the
//     24px white ai-thread-summary), 12px before the label (bold, one
//     line) over "Prompt" (13px content-sec); with no label "Prompt"
//     alone, centred on the tile (.promptCardHeaderInfoTitleEmpty);
//   - .promptCardBody (padding 12px): the prompt when it isn't empty
//     (SharePromptButton's `e && ex`). Plain text is
//     TruncatedExpandableText's, three lines and an ellipsis with "Show
//     more" under them when it's cut; expanded, all of it with "Less"
//     after it. prompt_rich_text is TruncatedBlocks' instead: cut at 150
//     characters or 3 line breaks with " Show more", "Show less" under it
//     expanded. Then .promptCardActions (margin-top 12px): Send to
//     {agentName}, a medium primary button (LoadingButton) with
//     send-filled 4px before its label, its spinner over it while the
//     send runs (no click then: popups' SharePromptSend).
//
// Only a SharePrompt richtext Shows draws. Left out: Forward prompt (the
// header's hover button opens PromptShareDialog, a side effect this build
// leaves to a module Clack doesn't have), the attached skills, the card's
// drop shadow, and the glyphs (sparkles for ai-thread-summary, send for
// send-filled).

import (
	"image"
	"image/color"

	"gioui.org/font"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
)

var (
	nsAgents       = i18n.NS("agents")
	nsSlackConnect = i18n.NS("slack_connect")
)

// SharePromptHandler is optional on a Handler: a share_prompt's Send to
// {agentName} was clicked.
type SharePromptHandler interface {
	SharePromptSend(p *richtext.SharePrompt)
}

// sharePromptKey is a prompt's expanded state in the View's collapsed map
// (true: expanded, as TruncatedExpandableText's and TruncatedBlocks'
// useState): by block_id and prompt_id, so a message decoded again keeps
// it.
type sharePromptKey struct{ block, id string }

// Share prompt geometry (Dp).
const (
	spMaxW    = 426 // .promptCardMaxWidth
	spPad     = 12  // the header's and the body's padding
	spTile    = 40  // the icon tile: 24 and 8px around it
	spIcon    = 24
	spBtnH    = 36 // Button size medium
	spBtnIcon = 20 // send-filled, size 20
)

// sharePrompt lays out a share_prompt block.
func (l *lay) sharePrompt(b *richtext.Block, x, y, w int) int {
	sp := b.Prompt
	if sp == nil || !sp.Shown {
		return 0
	}
	p := &l.st.Palette
	cw := w
	if !l.opts.Canvas {
		cw = min(w, l.dp(spMaxW))
	}
	pad := l.dp(spPad)
	// the card under everything; its height is known at the end
	bi := len(l.d.boxes)
	l.d.boxes = append(l.d.boxes, box{kind: bBorder, fill: p.Panel, stroke: p.Border, bw: l.dp(1), radius: l.dp(12), target: -1})

	// the header: the tile, then the label over "Prompt"
	tile := l.dp(spTile)
	hy := y + pad
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(x+pad, hy, x+pad+tile, hy+tile), radius: l.dp(4), fill: color.NRGBA{0x83, 0x38, 0x8a, 0xff}, target: -1})
	is := l.dp(spIcon)
	ix, iy := x+pad+(tile-is)/2, hy+(tile-is)/2
	l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(ix, iy, ix+is, iy+is), url: iconURL + "sparkles", target: -1, px: is, tint: color.NRGBA{0xff, 0xff, 0xff, 0xff}})
	tx := x + pad + tile + pad
	tw := max(x+cw-pad-tx, 1)
	ss := l.small(p.Text2)
	ss.maxLines, ss.ellipsis = 1, true
	ay := hy + (tile-ss.lineH)/2 // no label: "Prompt" centred on the tile
	if sp.Label != "" {
		fs := l.bodyStyle()
		fs.face.Weight = font.Bold
		fs.maxLines, fs.ellipsis = 1, true
		l.plainAdd("\n")
		th, _ := l.plainFlow(sp.Label, tx, hy, tw, &fs, -1)
		ay = hy + th
	}
	l.plainAdd("\n")
	l.plainFlow(nsAgents.T("Prompt"), tx, ay, tw, &ss, -1)

	// the body: the prompt, then the actions
	bx, bw := x+pad, max(cw-2*pad, 1)
	yy := hy + tile + pad
	if sp.Prompt != "" {
		key := sharePromptKey{b.BlockID, sp.PromptID}
		if len(sp.Rich) > 0 {
			yy += l.promptRich(sp, key, bx, yy, bw)
		} else {
			yy += l.promptPlain(sp, key, bx, yy, bw)
		}
	}
	yy += l.dp(12) // .promptCardActions' margin-top
	yy += l.promptSend(sp, bx, yy, bw)
	h := yy + pad - y
	l.d.boxes[bi].r = image.Rect(x, y, x+cw, y+h)
	return h
}

// promptOpen reports whether the prompt's text was expanded.
func (l *lay) promptOpen(key sharePromptKey) bool {
	return l.collapsed != nil && l.collapsed[key]
}

// promptPlain is TruncatedExpandableText (contractedLines 3, "Show more"):
// cut at three lines, "Show more" under them when the text ran over;
// expanded, the text with "Less" after it.
func (l *lay) promptPlain(sp *richtext.SharePrompt, key sharePromptKey, x, y, w int) int {
	p := &l.st.Palette
	ps := l.bodyStyle()
	l.plainAdd("\n")
	if l.promptOpen(key) {
		k := l.faceFor(ps.face, ps.size, 0)
		l.addText(sp.Prompt+" ", &ps, k, ps.col, 0, -1, -1)
		tg := l.d.addTarget(target{kind: tCollapse, key: key, underline: true})
		l.addText(nsSlackConnect.T("Less"), &ps, k, p.Link, 0, -1, tg)
		h, _ := l.flow(x, y, w, &ps)
		return h
	}
	ps.maxLines, ps.ellipsis = 3, true
	h, _ := l.plainFlow(sp.Prompt, x, y, w, &ps, -1)
	if !l.lastTruncated {
		return h
	}
	ls := l.bodyStyle()
	ls.col, ls.maxLines, ls.noSelect = p.Link, 1, true
	tg := l.d.addTarget(target{kind: tCollapse, key: key, underline: true})
	text := nsAgents.T("Show more")
	sw := int(l.measure(text, l.faceFor(ls.face, ls.size, 0)) + 0.999)
	l.plainAdd("\n")
	sh, _ := l.plainFlow(text, x, y+h, min(sw+l.dp(2), w), &ls, tg)
	l.d.hits = append(l.d.hits, hit{r: image.Rect(x, y+h, x+min(sw, w), y+h+sh), t: tg})
	return h + sh
}

// promptRich is TruncatedBlocks (maxLines 3, maxChars 150) of the
// prompt's rich text: TruncatedRichText's cut with " Show more", and
// "Show less" under it expanded.
func (l *lay) promptRich(sp *richtext.SharePrompt, key sharePromptKey, x, y, w int) int {
	text := sp.Rich
	if short, cut := truncateBlocks(text, 150, 3); cut {
		if l.promptOpen(key) {
			text = withExpander(append([]richtext.Block(nil), text...), false)
		} else {
			text = withExpander(short, true)
		}
	}
	l.expandColl = key
	h := l.blocks(text, x, y, w, false)
	l.expandColl = nil
	l.plainAdd("\n")
	return h
}

// promptSend is Send to {agentName}: a medium primary button (Slack Kit's
// LoadingButton), send-filled 4px before the label (one line, cut with an
// ellipsis).
func (l *lay) promptSend(sp *richtext.SharePrompt, x, y, w int) int {
	p := &l.st.Palette
	label := nsAgents.T("Send to {agentName}", "agentName", sp.AgentName)
	bh, icon, in := l.dp(spBtnH), l.dp(spBtnIcon), l.dp(12)
	bs := l.bodyStyle()
	bs.face.Weight = font.Bold
	bs.lineH, bs.maxLines, bs.ellipsis, bs.noSelect = bh, 1, true, true
	bs.col = p.PrimaryText
	lw := int(l.measure(label, l.faceFor(bs.face, bs.size, 0)) + 0.999)
	bw := min(in+icon+l.dp(4)+lw+in, w)
	r := image.Rect(x, y, x+bw, y+bh)
	tg := l.d.addTarget(target{kind: tSharePrompt, prompt: sp})
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, radius: l.dp(8), fill: p.Primary, target: tg})
	l.d.busyBtns = append(l.d.busyBtns, busyBtn{r: r, radius: l.dp(8), key: SharePromptKey(sp), act: "send", bg: p.Primary})
	iy := y + (bh-icon)/2
	l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(x+in, iy, x+in+icon, iy+icon), url: iconURL + "send", target: tg, px: icon, tint: p.PrimaryText})
	lx := x + in + icon + l.dp(4)
	l.plainAdd("\n")
	l.plainFlow(label, lx, y-l.dp(1), max(x+bw-in-lx, 1), &bs, tg)
	l.d.hits = append(l.d.hits, hit{r: r, t: tg})
	return bh
}

// SharePromptRect is a laid out share_prompt (tests): its card and its
// Send to button, view-local.
type SharePromptRect struct {
	Prompt     *richtext.SharePrompt
	Card, Send image.Rectangle
}

// SharePromptRects lists the view's share_prompt cards (tests).
func (v *View) SharePromptRects() []SharePromptRect {
	var out []SharePromptRect
	for i := range v.d.hits {
		hr := &v.d.hits[i]
		if hr.t < 0 || int(hr.t) >= len(v.d.targets) || v.d.targets[hr.t].kind != tSharePrompt {
			continue
		}
		r := SharePromptRect{Prompt: v.d.targets[hr.t].prompt, Send: hr.r}
		for j := range v.d.boxes {
			// the card: the radius-12 frame around the button
			if b := &v.d.boxes[j]; b.kind == bBorder && b.target < 0 && b.r.Min.X <= hr.r.Min.X && hr.r.Max.Y <= b.r.Max.Y && b.r.Min.Y <= hr.r.Min.Y {
				r.Card = b.r
			}
		}
		out = append(out, r)
	}
	return out
}
