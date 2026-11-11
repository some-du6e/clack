package richview

import (
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"

	"gioui.org/font"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

func (l *lay) bodyStyle() flowStyle {
	p := &l.st.Palette
	col := p.Text
	if l.opts.Muted || l.opts.Secondary {
		col = p.Text3
	}
	size, lh := l.st.TextSize, l.st.LineHeight
	if l.opts.Compact {
		size, lh = 13, 19.5
	}
	fs := flowStyle{
		face:  font.Font{Typeface: l.st.Face},
		size:  l.sp(size),
		lineH: l.dp(lh),
		col:   col,
		emoji: l.dp(22),
	}
	if l.opts.Compact {
		fs.emoji = l.dp(16)
	}
	if l.jumbo {
		fs.emoji = l.dp(32)
	}
	return fs
}

// headerScale is .p-header_block__text by level (client-boot-styles):
// 15px/1.46668 black with no level (and at level 3 or 4, which Header
// classes level_3), level_1 22px/1.36365, level_2 18px/1.33334, all
// black. The flat scale (p-header_block--flat_scale) moves each level
// down one step: level_1 18px/1.33334, level_2 15px/1.46668 black,
// level_3 15px/1.46668 bold. Sizes in Sp, line heights in Dp.
func headerScale(level int, flat bool) (size, lineH float32, w font.Weight) {
	switch {
	case flat && level == 1, !flat && level == 2:
		return 18, 24, font.Black
	case !flat && level == 1:
		return 22, 30, font.Black
	case flat && (level == 3 || level == 4):
		return 15, 22, font.Bold
	}
	return 15, 22, font.Black
}

func isTextual(k richtext.BlockKind) bool {
	switch k {
	case richtext.Paragraph, richtext.Quote, richtext.List, richtext.Tombstone:
		return true
	}
	return false
}

// Block Kit margins, from Slack's renderer (client CSS, 2026-11): every
// top-level block sits in its own .p-block_kit_renderer__block_wrapper
// (display: flex), so its margins add up with the next block's instead of
// collapsing.
//
//	block         margin              first in the message
//	section       8px 0 4px           top 0
//	header        1em(15px) 0 4px     top 4px
//	divider       8px 0               (1px border, otl-sec)
//	context       4px 0
//	image         8px 0               top 0
//	actions       elements 8px top, block 8px bottom
//	table         padding 4px 0 (tableMargin)
//	error         0 0 6px             ("This content could not be displayed.")
//
// rich_text has none of its own; its quotes and pre blocks keep their 4px.
func (l *lay) margins(b *richtext.Block) (top, first, bottom int) {
	if !b.Kit {
		switch b.Kind {
		case richtext.Quote, richtext.Preformatted:
			return l.dp(4), 0, l.dp(4)
		case richtext.FileBlock, richtext.AttachmentBlock:
			return l.dp(4), 0, l.dp(8)
		}
		return 0, 0, 0
	}
	switch b.Kind {
	case richtext.Section:
		return l.dp(8), 0, l.dp(4)
	case richtext.Header:
		return l.dp(15), l.dp(4), l.dp(4)
	case richtext.Divider, richtext.Actions:
		return l.dp(8), l.dp(8), l.dp(8)
	case richtext.Context, richtext.Table:
		return l.dp(4), l.dp(4), l.dp(4)
	case richtext.ImageBlock:
		return l.dp(8), 0, l.dp(8)
	case richtext.Unsupported:
		return 0, 0, l.dp(6)
	case richtext.UnknownBlock:
		return l.dp(8), 0, l.dp(8)
	}
	return 0, 0, 0
}

// gapBetween is the vertical space between two consecutive blocks.
func (l *lay) gapBetween(prev, cur *richtext.Block) int {
	if prev.Kit || cur.Kit {
		_, _, b := l.margins(prev)
		t, _, _ := l.margins(cur)
		return b + t
	}
	return l.gap(prev.Kind, cur.Kind)
}

// gap is the vertical space between two consecutive non-Block Kit blocks.
func (l *lay) gap(prev, cur richtext.BlockKind) int {
	if isTextual(prev) && isTextual(cur) && prev != richtext.Quote && cur != richtext.Quote {
		return 0
	}
	if prev == richtext.FileBlock || prev == richtext.AttachmentBlock {
		return l.dp(8)
	}
	if prev == richtext.FileStatus { // its own 4px margin, then the gallery
		return 0
	}
	return l.dp(4)
}

// lastTextual returns the index of the block that gets the "(edited)" label.
func lastTextual(bs []richtext.Block) int {
	for i := len(bs) - 1; i >= 0; i-- {
		switch bs[i].Kind {
		case richtext.Paragraph, richtext.Quote, richtext.List, richtext.Section, richtext.Header, richtext.Preformatted:
			if len(bs[i].Children) > 0 {
				return -1
			}
			return i
		case richtext.Tombstone:
			return -1
		default:
			// attachments, files, actions, images, context: the label goes
			// after the last text above them, like Slack.
			continue
		}
	}
	return -1
}

// editedPlace is where a message's "(edited)" goes. Text without Block
// Kit layout blocks (mrkdwn, rich_text) takes it after its last text
// block (lastTextual). With Block Kit blocks Slack's
// insertMessageLabelIntoBlocks (boot-shared bhnT) puts it inside the last
// block only when that one is rich_text, and otherwise appends it as a
// block of its own after the message's blocks: after is that line's
// place, the last block before the files and attachments that follow.
func editedPlace(bs []richtext.Block) (in, after int) {
	kit := false
	for i := range bs {
		if bs[i].Kit {
			kit = true
			break
		}
	}
	if !kit {
		return lastTextual(bs), -1
	}
	last := len(bs) - 1
	for last >= 0 && (bs[last].Kind == richtext.FileBlock || bs[last].Kind == richtext.FileStatus || bs[last].Kind == richtext.AttachmentBlock) {
		last--
	}
	if last < 0 {
		return -1, -1
	}
	switch b := &bs[last]; {
	case b.Kit:
	case b.Kind == richtext.Paragraph || b.Kind == richtext.Quote || b.Kind == richtext.List || b.Kind == richtext.Preformatted:
		if len(b.Children) == 0 {
			return last, -1
		}
	}
	return -1, last
}

func (l *lay) addEdited() {
	p := &l.st.Palette
	k := faceKey{f: font.Font{Typeface: l.st.Face}, size: fixedPx(l.sp(13))}
	sp := l.st.spaceOf(faceKey{f: font.Font{Typeface: l.st.Face}, size: fixedPx(l.sp(15))})
	l.items = append(l.items, item{kind: itSpace, w: sp.adv, deco: -1, target: -1, p0: int32(len(l.d.plain)), p1: int32(len(l.d.plain)), part: l.curPart})
	var pt uint8
	if l.css != nil {
		pp := l.ctr.with(editedClass)
		pt = l.partFor(&pp)
		k = l.partFace(k, pt)
	}
	w := l.st.word(k, i18n.T("message_kit", "(edited)"))
	n := int32(len(l.d.plain))
	var tip int16
	if l.opts.EditedTS != "" { // EditedLabel's Tooltip (delay 300): the edit's time
		l.d.tips = append(l.d.tips, editedTipKey)
		tip = int16(len(l.d.tips))
	}
	l.items = append(l.items, item{kind: itWord, sh: w, face: k, w: w.adv, col: p.Edited, deco: -1, target: -1, p0: n, p1: n, part: pt, tip: tip})
}

// richBlock enters Slack's .p-rich_text_block (once: blocks nested in a
// quote stay in it) and then d; restore with leave.
func (l *lay) richBlock(d *slackc.ClassDef) partPath {
	old := l.ctr
	if l.css == nil {
		return old
	}
	if !l.inBlock {
		l.ctr = l.ctr.with(richBlockClass)
	}
	l.ctr = l.ctr.with(d)
	l.curPart = l.partFor(&l.ctr)
	return old
}

// block lays out one block in its Slack container (css.go).
func (l *lay) block(b *richtext.Block, x, y, w int, edited bool) int {
	if l.css == nil {
		return l.block1(b, x, y, w, edited)
	}
	var old partPath
	inB := l.inBlock
	switch b.Kind {
	case richtext.Paragraph:
		old = l.richBlock(richSectionClass)
	case richtext.Preformatted:
		old = l.richBlock(preClass)
	case richtext.Quote:
		old = l.richBlock(quoteClass)
	case richtext.List:
		d := listBulletClass
		if b.Ordered {
			d = listOrderedClass
		}
		old = l.richBlock(d)
	case richtext.Tombstone:
		old = l.enter(tombstoneClass)
	case richtext.Header:
		old = l.enter(headerClass)
	case richtext.Context:
		old = l.enter(contextBlkClass)
		l.enter(mrkdwnElemClass)
	case richtext.Section:
		old = l.enter(sectionBlkClass)
	case richtext.AttachmentBlock:
		old = l.enter(attachmentClass)
	case richtext.FileBlock:
		old = l.enter(fileMetaClass)
	default:
		old = l.ctr
	}
	switch b.Kind {
	case richtext.Paragraph, richtext.Preformatted, richtext.Quote, richtext.List:
		l.inBlock = true
	}
	h := l.block1(b, x, y, w, edited)
	l.inBlock = inB
	l.leave(old)
	return h
}

// blocks lays out bs at (x, y) in width w and returns the height.
func (l *lay) blocks(bs []richtext.Block, x, y, w int, top bool) int {
	y0 := y
	edited, editedAfter := -1, -1
	if top && l.opts.Edited {
		edited, editedAfter = editedPlace(bs)
	}
	editedDone := false
	var gal []*richtext.File
	if top && len(bs) > 0 && bs[0].Kit {
		_, first, _ := l.margins(&bs[0])
		y += first
	}
	if top && (len(bs) == 0 || bs[0].Kind != richtext.Paragraph) {
		l.lead = 0 // a list, quote, code block or card starts under the sender
	}
	skipGap := false
	prev := -1
	for i := 0; i < len(bs); i++ {
		b := &bs[i]
		if b.Kind == richtext.AttachmentBlock && b.Att != nil && !b.Att.MsgCard && !shown(b.Att) {
			continue // getVisibleAttachmentsForMessage dropped it
		}
		if prev >= 0 {
			if !skipGap {
				y += l.gapBetween(&bs[prev], b)
			}
			skipGap = false
			// cleanHTMLForExternal adds a <br> after a section only when
			// it doesn't already end in one (a trailing "\n" text is a
			// <br>), so a block break never doubles a newline
			if n := len(l.d.plain); n == 0 || l.d.plain[n-1] != '\n' {
				l.plainAdd("\n")
			}
		}
		prev = i
		// v3 link previews flow in one wrapping row (unfurl.go); their
		// wrappers hold the 8px under each card.
		if b.Kind == richtext.AttachmentBlock && isV3(b) && !(l.mode&modeAtts != 0 && !l.inAtt) {
			var grp []*richtext.Block
			j := i
			for j < len(bs) && bs[j].Kind == richtext.AttachmentBlock && isV3(&bs[j]) {
				grp = append(grp, &bs[j])
				j++
			}
			y += l.v3Group(grp, x, y, w)
			i, prev = j-1, j-1
			skipGap = true
			continue
		}
		// MessageFiles' FilesMetaTextWrapper (filemeta.go): the title
		// line over the message's files, unless the placeholder's
		// PendingFilesAtom line ("Uploading…") is there instead
		if b.Kind == richtext.FileBlock && top && !l.inAtt && l.mode&modeAtts == 0 && !l.opts.CompactFiles && b.File != nil &&
			(i == 0 || bs[i-1].Kind != richtext.FileStatus && bs[i-1].Kind != richtext.FileBlock) {
			var files []*richtext.File
			j := i
			for j < len(bs) && bs[j].Kind == richtext.FileBlock && bs[j].File != nil {
				files = append(files, bs[j].File)
				j++
			}
			mh, shown := l.fileMeta(files, x, y, w)
			y += mh
			if !shown {
				y -= l.dp(4) // collapsed: only the line
				i, prev = j-1, j-1
				continue
			}
		}
		// Slack's FileGallery: consecutive visual files (images, videos)
		// lay out as one media gallery.
		if isMediaBlock(b) {
			gal = gal[:0]
			j := i
			for j < len(bs) && isMediaBlock(&bs[j]) {
				gal = append(gal, bs[j].File)
				j++
			}
			y += l.mediaGallery(gal, x, y, w)
			i, prev = j-1, j-1
			continue
		}
		// two or more document pillows: FileGallery's mixed_file_format grid
		if isPillowBlock(b) && i+1 < len(bs) && isPillowBlock(&bs[i+1]) {
			gal = gal[:0]
			j := i
			for j < len(bs) && isPillowBlock(&bs[j]) {
				gal = append(gal, bs[j].File)
				j++
			}
			y += l.pillowGrid(gal, x, y, w)
			i, prev = j-1, j-1
			continue
		}
		y += l.block(b, x, y, w, i == edited)
		if i == editedAfter {
			// insertMessageLabelIntoBlocks' standalone label block (a
			// rich_text after the block's own bottom margin, no leading
			// space: omitWhitespace)
			if b.Kit {
				_, _, bottom := l.margins(b)
				y += bottom
			}
			if n := len(l.d.plain); n > 0 && l.d.plain[n-1] != '\n' {
				l.plainAdd("\n")
			}
			fs := l.bodyStyle()
			l.addEdited()
			l.items = l.items[1:]
			h, _ := l.flow(x, y, w, &fs)
			y += h
			editedDone = true
		}
	}
	if edited < 0 && !editedDone && top && l.opts.Edited {
		// No text block to attach to: a line of its own.
		fs := l.bodyStyle()
		l.addEdited()
		l.items = l.items[1:]
		h, _ := l.flow(x, y, w, &fs)
		y += h
	}
	if top && len(bs) > 0 && bs[len(bs)-1].Kit && !(editedDone && editedAfter == len(bs)-1) {
		_, _, bottom := l.margins(&bs[len(bs)-1])
		y += bottom
	}
	return y - y0
}

// isPillowBlock is a file drawn as a plain pillow (not media, audio or a
// snippet).
func isPillowBlock(b *richtext.Block) bool {
	f := b.File
	return b.Kind == richtext.FileBlock && f != nil && !mediaFile(f) && !f.IsAudio && !f.IsSnippet && f.Preview == ""
}

func isMediaBlock(b *richtext.Block) bool {
	return b.Kind == richtext.FileBlock && mediaFile(b.File)
}

func (l *lay) block1(b *richtext.Block, x, y, w int, edited bool) int {
	p := &l.st.Palette
	switch b.Kind {
	case richtext.Paragraph:
		fs := l.bodyStyle()
		l.addSpans(b.Spans, &fs)
		if edited {
			l.addEdited()
		}
		h, _ := l.flow(x, y, w, &fs)
		return h

	case richtext.Tombstone:
		fs := l.bodyStyle()
		fs.col = p.Text2
		spans := b.Spans
		if len(spans) == 0 {
			spans = []richtext.Span{{Text: "This message was deleted."}}
		}
		l.addSpans(spans, &fs)
		h, _ := l.flow(x, y, w, &fs)
		return h

	case richtext.Preformatted:
		pad, padL, bar := l.dp(8), l.dp(8), -1
		if b.Border {
			// .c-mrkdwn__pre[data-stringify-border="1"]: 13px in, 13px
			// left padding, and the list/quote border bar 1px left of x
			bar = len(l.d.boxes)
			l.d.boxes = append(l.d.boxes, box{kind: bFill, fill: p.QuoteBar, radius: l.dp(8), target: -1})
			x, w, padL = x+l.dp(13), w-l.dp(13), l.dp(13)
		}
		fs := l.bodyStyle()
		fs.face = font.Font{Typeface: l.st.Mono}
		fs.size = l.sp(12)
		fs.lineH = l.dp(18)
		if l.st.PreLine > 0 {
			fs.lineH = l.dp(l.st.PreLine)
		}
		fs.pre = true
		fs.emoji = l.dp(16)
		fs.col = p.Text
		if l.opts.Muted {
			fs.col = p.Text2
		}
		bi := len(l.d.boxes)
		l.d.boxes = append(l.d.boxes, box{kind: bBorder, fill: p.PreBg, stroke: p.PreBorder, bw: l.dp(1), radius: l.dp(4), target: -1, part: l.curPart})
		c0 := int32(len(l.d.plain))
		l.addSpans(b.Spans, &fs)
		c1 := int32(len(l.d.plain))
		if edited {
			l.addEdited()
		}
		// .c-mrkdwn__pre[data-copy-code-block]{padding-right:32px}: room
		// for CopyCodeBlockButton (codecopy.go)
		h, _ := l.flow(x+padL, y+pad, w-padL-l.dp(copyPadR), &fs)
		hh := h + 2*pad
		l.d.boxes[bi].r = image.Rect(x, y, x+w, y+hh)
		l.d.codes = append(l.d.codes, codeRun{p0: c0, p1: c1, block: true, r: l.d.boxes[bi].r})
		if bar >= 0 {
			l.d.boxes[bar].r = image.Rect(x-l.dp(14), y, x-l.dp(10), y+hh)
		}
		return hh

	case richtext.Quote:
		bar := l.dp(4)
		in := l.dp(16)
		bi := len(l.d.boxes)
		l.d.boxes = append(l.d.boxes, box{kind: bFill, fill: p.QuoteBar, radius: l.dp(8), target: -1})
		if b.Border {
			// .c-mrkdwn__quote[data-stringify-border="1"]: a second bar at
			// 15px, the text at 31px
			l.d.boxes = append(l.d.boxes, box{kind: bFill, fill: p.QuoteBar, radius: l.dp(8), target: -1})
			in = l.dp(31)
		}
		var h int
		if len(b.Children) > 0 {
			h = l.blocks(b.Children, x+in, y, w-in, false)
		} else {
			fs := l.bodyStyle()
			l.addSpans(b.Spans, &fs)
			if edited {
				l.addEdited()
			}
			h, _ = l.flow(x+in, y, w-in, &fs)
		}
		l.d.boxes[bi].r = image.Rect(x, y, x+bar, y+h)
		if b.Border {
			l.d.boxes[bi+1].r = image.Rect(x+l.dp(15), y, x+l.dp(15)+bar, y+h)
		}
		return h

	case richtext.List:
		return l.list(b, x, y, w, edited)

	case richtext.Header:
		fs := l.bodyStyle()
		fs.face.Weight = font.Black
		if !l.opts.Compact {
			size, lh, wt := headerScale(b.Level, l.st.flatHeadings)
			fs.size, fs.lineH, fs.face.Weight = l.sp(size), l.dp(lh), wt
		}
		l.addSpans(b.Spans, &fs)
		if edited {
			l.addEdited()
		}
		h, _ := l.flow(x, y, w, &fs)
		return h

	case richtext.Divider:
		m := l.dp(8)
		if b.Kit { // the margins are the block's (margins)
			m = 0
		}
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(x, y+m, x+w, y+m+l.dp(1)), fill: p.Divider, target: -1})
		return 2*m + l.dp(1)

	case richtext.Unsupported:
		// Slack's BlockError: <small class="p-block_kit_error_message">,
		// rgba(foreground_high, .5).
		fs := l.bodyStyle()
		fs.size, fs.lineH = l.sp(13), l.dp(18)
		fs.col = Over(color.NRGBA{R: p.Text.R, G: p.Text.G, B: p.Text.B, A: 128}, p.Panel)
		l.addSpans(b.Spans, &fs)
		h, _ := l.flow(x, y, w, &fs)
		return h

	case richtext.UnknownBlock:
		// UnsupportedBlockFallback: .fallback (width 100%, max 426px) around
		// an entity card (1px border, radius 12, base-pry) with .content:
		// 13px content-sec, line-height 1.25, padding 12px.
		cw := min(w, l.dp(426))
		pad := l.dp(12)
		fs := l.bodyStyle()
		fs.size, fs.lineH, fs.col = l.sp(13), l.dp(16), p.Text2
		l.addSpans(b.Spans, &fs)
		th, _ := l.flow(x+pad, y+pad, cw-2*pad, &fs)
		h := th + 2*pad
		r := image.Rect(x, y, x+cw, y+h)
		l.d.boxes = append(l.d.boxes, box{kind: bBorder, r: r, fill: p.Panel, stroke: p.Border, bw: l.dp(1), radius: l.dp(12), target: -1})
		return h

	case richtext.Context:
		return l.context(b, x, y, w)

	case richtext.Section:
		return l.section(b, x, y, w, edited)

	case richtext.ImageBlock:
		if b.Image == nil {
			return 0
		}
		if b.Kit {
			return l.kitImage(b.Image, x, y, w)
		}
		h := 0
		if b.Image.Title != "" {
			fs := l.bodyStyle()
			fs.size, fs.lineH, fs.col = l.sp(13), l.dp(18), p.Text2
			th, _ := l.plainFlow(b.Image.Title, x, y, w, &fs, -1)
			h += th + l.dp(4)
		}
		tg := l.d.addTarget(target{kind: tImage, img: b.Image})
		return h + l.image(b.Image, x, y+h, min(w, l.dp(360)), l.dp(360), l.dp(4), tg)

	case richtext.Actions:
		return l.buttons(b.Buttons, x, y, w)

	case richtext.Agent:
		return l.agent(b, x, y, w)

	case richtext.Table:
		return l.table(b, x, y, w)

	case richtext.AttachmentBlock:
		if b.Att == nil {
			return 0
		}
		if l.mode&modeAtts != 0 && !l.inAtt {
			return l.compBlock(b, x, y, w)
		}
		return l.attachment(b, x, y, w)

	case richtext.FileBlock:
		if b.File == nil {
			return 0
		}
		if l.mode&modeAtts != 0 && !l.inAtt {
			return l.compFile(b, x, y, w)
		}
		if b.File.Pending { // c-pending_files__generic_file--pending: the pillow at .5
			nb, nf := len(l.d.boxes), len(l.d.frags)
			h := l.file(b.File, x, y, w)
			l.fade(nb, nf)
			return h
		}
		return l.file(b.File, x, y, w)

	case richtext.FileStatus:
		return l.fileStatus(b, x, y, w)
	}
	// Unknown: lay out whatever text it has.
	if len(b.Spans) > 0 {
		fs := l.bodyStyle()
		l.addSpans(b.Spans, &fs)
		h, _ := l.flow(x, y, w, &fs)
		return h
	}
	return 0
}

// ---------- lists ----------

func toAlpha(n int) string {
	s := ""
	for n > 0 {
		n--
		s = string(rune('a'+n%26)) + s
		n /= 26
	}
	return s
}

func toRoman(n int) string {
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"m", "cm", "d", "cd", "c", "xc", "l", "xl", "x", "ix", "v", "iv", "i"}
	var sb strings.Builder
	for i, v := range vals {
		for n >= v {
			sb.WriteString(syms[i])
			n -= v
		}
	}
	return sb.String()
}

func (l *lay) list(b *richtext.Block, x, y, w int, edited bool) int {
	p := &l.st.Palette
	y0 := y
	indentW := l.dp(28)
	cx := x + indentW*(b.Indent+1)
	if b.Border {
		cx += l.dp(11) // .p-rich_text_list[data-border="1"] padding-inline-start
	}
	fs := l.bodyStyle()
	for i, it := range b.Items {
		if i > 0 {
			l.plainAdd("\n")
		}
		li := l.enter(listItemClass)
		l.addSpans(it, &fs)
		if edited && i == len(b.Items)-1 {
			l.addEdited()
		}
		lineStart := len(l.d.lines)
		h, _ := l.flow(cx, y, x+w-cx, &fs)
		if h == 0 {
			h = fs.lineH
		}
		// Marker aligned with the first line's baseline.
		baseline := y + fs.lineH*3/4
		if lineStart < len(l.d.lines) {
			ln := l.d.lines[lineStart]
			for fi := ln.f0; fi < ln.f1; fi++ {
				if l.d.frags[fi].kind == fText {
					baseline = l.d.frags[fi].y
					break
				}
			}
		}
		if b.Ordered {
			n := b.Offset + i + 1
			var label string
			switch b.Indent % 3 {
			case 1:
				label = toAlpha(n)
			case 2:
				label = toRoman(n)
			default:
				label = strconv.Itoa(n)
			}
			label += "."
			k := l.faceFor(fs.face, fs.size, 0)
			wd := l.st.word(k, label)
			gapW := l.dpf(8)
			l.d.frags = append(l.d.frags, frag{kind: fText, x: float32(cx) - gapW - wd.adv, y: baseline, w: wd.adv, size: fs.size, sh: wd, col: fs.col, target: -1, top: y, bot: y + fs.lineH, line: -1, p0: -1, p1: -1, part: l.curPart})
		} else {
			// disc, circle, square by level (Slack cycles every 3).
			r := l.dpf(2.75)
			cyf := float32(baseline) - float32(fs.size)*0.32
			cxf := float32(cx) - l.dpf(17)
			rect := image.Rect(int(cxf-r+0.5), int(cyf-r+0.5), int(cxf+r+0.5), int(cyf+r+0.5))
			kind := bCircle
			switch b.Indent % 3 {
			case 1:
				kind = bRing
			case 2:
				kind = bSquare
			}
			l.d.boxes = append(l.d.boxes, box{kind: kind, r: rect, fill: fs.col, bw: l.dp(1), target: -1, part: l.curPart, colFill: true})
		}
		l.leave(li)
		y += h
	}
	if b.Border {
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(x, y0, x+l.dp(4), y), fill: p.QuoteBar, target: -1})
	}
	return y - y0
}

// ---------- block kit ----------

func (l *lay) context(b *richtext.Block, x, y, w int) int {
	if len(b.Children) > 0 {
		return l.contextRow(b.Children, x, y, w)
	}
	p := &l.st.Palette
	fs := l.bodyStyle()
	fs.size, fs.lineH, fs.col, fs.emoji = l.sp(13), l.dp(18), p.Text2, l.dp(16)
	cx := x
	if b.Image != nil {
		s := l.dp(20)
		l.image(b.Image, cx, y-l.dp(1), s, s, l.dp(2), -1)
		cx += s + l.dp(4)
	}
	for i := range b.Children {
		if c := &b.Children[i]; c.Kind == richtext.ImageBlock && c.Image != nil {
			s := l.dp(20)
			l.image(c.Image, cx, y-l.dp(1), s, s, l.dp(2), -1)
			cx += s + l.dp(4)
		}
	}
	l.addSpans(b.Spans, &fs)
	h, _ := l.flow(cx, y, x+w-cx, &fs)
	if b.Image != nil && h < l.dp(18) {
		h = l.dp(18)
	}
	return h
}

// section lays out a section block. In Slack's renderer it is a row
// (flex-direction: row-reverse) of the text content (flex: 1) and the
// accessory (margin 0 0 4px 8px; an image is 88×88 with 4px more on its
// left); with an accessory the block is at most 600 wide. radio_buttons
// and checkboxes accessories stack under the text instead (margin 4px 0).
func (l *lay) section(b *richtext.Block, x, y, w int, edited bool) int {
	y0 := y
	hasAcc := b.Image != nil || len(b.Buttons) > 0
	accGap, accBottom := l.dp(12), 0
	if b.Kit {
		accGap, accBottom = l.dp(8), l.dp(4)
		if hasAcc {
			w = min(w, l.dp(600))
		}
	}
	tw := w
	accW, accH := 0, 0
	stacked := false
	if b.Image != nil {
		accW = l.dp(88)
		tw = w - accW - l.dp(12)
	} else if len(b.Buttons) > 0 {
		if el := l.opts.Elements; el != nil {
			sz, stack := el.ElementsSize(b.Buttons[:1], w, true, l.metric())
			if !stack {
				accW, accH = sz.X, sz.Y
				tw = w - accW - accGap
			} else {
				stacked = true
			}
		} else {
			accW = l.buttonWidth(b.Buttons[0])
			tw = w - accW - accGap
		}
	}
	spans, children := b.Spans, b.Children
	key := l.kitTruncKey(b)
	if key != "" {
		l.expandKey = key
		defer func() { l.expandKey = "" }()
		if len(spans) > 0 || len(children) > 0 {
			tb := children
			if len(tb) == 0 {
				tb = []richtext.Block{{Kind: richtext.Paragraph, Spans: spans}}
			}
			tb = kitTruncate(tb, key)
			if len(tb) == 1 && tb[0].Kind == richtext.Paragraph {
				spans, children = tb[0].Spans, nil
			} else {
				spans, children = nil, tb
			}
		}
	}
	h := 0
	if len(children) > 0 {
		h = l.blocks(children, x, y, tw, false)
	} else if len(spans) > 0 {
		fs := l.bodyStyle()
		st := l.enter(sectionTextClass)
		l.enter(mrkdwnElemClass)
		l.addSpans(spans, &fs)
		if edited {
			l.addEdited()
		}
		h, _ = l.flow(x, y, tw, &fs)
		l.leave(st)
	}
	if len(b.Fields) > 0 {
		if b.Kit {
			if h > 0 {
				h += l.dp(12) // .p-section_block__fields:not(:first-child)
			}
			h += l.kitFields(b.Fields, x, y+h, tw, key)
		} else {
			if h > 0 {
				h += l.dp(4)
			}
			h += l.fields(b.Fields, nil, x, y+h, tw)
		}
	}
	if b.Image != nil {
		tg := l.d.addTarget(target{kind: tImage, img: b.Image})
		l.image(b.Image, x+w-accW, y, accW, accW, l.dp(8), tg)
		h = max(h, accW+accBottom)
	} else if len(b.Buttons) > 0 && l.opts.Elements != nil {
		if stacked {
			gap := l.dp(12)
			if b.Kit {
				gap = l.dp(4)
			}
			if h > 0 || b.Kit {
				h += gap
			}
			sz, _ := l.opts.Elements.ElementsSize(b.Buttons[:1], w, true, l.metric())
			l.addSlot(b.Buttons[:1], image.Rect(x, y+h, x+sz.X, y+h+sz.Y), true)
			h += sz.Y + accBottom
		} else {
			l.addSlot(b.Buttons[:1], image.Rect(x+w-accW, y, x+w, y+accH), true)
			h = max(h, accH+accBottom)
		}
	} else if len(b.Buttons) > 0 {
		bh := l.button(b.Buttons[0], x+w-accW, y)
		h = max(h, bh+accBottom)
	}
	return y + h - y0
}

// kitFields lays out a section's fields: Slack's .p-section_block__fields
// is a wrapping flex row (at most 600 wide in messages) of
// .p-field_section boxes calc(50% - 16px) wide, every second one 16px
// from the first; rows have no gap.
//
// With key set each field truncates on its own (kitTruncate).
func (l *lay) kitFields(fields [][]richtext.Span, x, y, w int, key string) int {
	w = min(w, l.dp(600))
	gut := l.dp(16)
	colW := w/2 - gut
	fy, rowH := y, 0
	for i, f := range fields {
		col := i % 2
		fs := l.bodyStyle()
		l.plainAdd("\n")
		if key != "" {
			f = flattenBreaks(kitTruncate([]richtext.Block{{Kind: richtext.Paragraph, Spans: f}}, key))
		}
		l.addSpans(f, &fs)
		fh, _ := l.flow(x+col*(colW+gut), fy, colW, &fs)
		rowH = max(rowH, fh)
		if col == 1 || i == len(fields)-1 {
			fy += rowH
			rowH = 0
		}
	}
	return fy - y
}

// contextRow lays out a context block's elements like Slack's Context:
// a wrapping flex row (.p-context_block) of one box per element; a box is
// its text (13px, rgb(97,96,97), in the message's 15px/22px line box) or a
// 20×20 image (radius 2), then a spacer before the next element, 16px
// after text and 4px after an image. A box wider than the row starts a
// new line and wraps inside the row's width.
func (l *lay) contextRow(els []richtext.Block, x, y, w int) int {
	p := &l.st.Palette
	fs := l.bodyStyle()
	fs.size, fs.lineH, fs.col, fs.emoji = l.sp(13), l.dp(22), p.Text2, l.dp(16)
	if l.opts.Muted {
		fs.col = p.Text3
	}
	imgS := l.dp(20)
	// an inline-block image sits on the 15px/22px strut's baseline:
	// 20px above it, the strut's 5px below
	imgH := imgS + l.dp(5)
	cx, cy, rowH, texts := x, y, 0, 0
	newLine := func() {
		cx, cy, rowH = x, cy+rowH, 0
	}
	for i := range els {
		c := &els[i]
		sp := 0
		if i < len(els)-1 {
			sp = l.dp(16)
			if c.Kind == richtext.ImageBlock {
				sp = l.dp(4)
			}
		}
		if c.Kind == richtext.ImageBlock {
			if c.Image == nil {
				continue
			}
			if cx > x && cx+imgS > x+w {
				newLine()
			}
			r := image.Rect(cx, cy, cx+imgS, cy+imgS)
			rad := l.dp(2)
			l.d.imgs = append(l.d.imgs, imgPrim{r: r, url: c.Image.URL, radii: [4]int{rad, rad, rad, rad}, target: -1, px: imgS})
			cx += imgS + sp
			rowH = max(rowH, imgH)
			continue
		}
		if texts > 0 {
			l.plainAdd(" ")
		}
		texts++
		l.addSpans(c.Spans, &fs)
		nat := naturalWidth(l.items)
		if cx > x && cx+nat > x+w {
			newLine()
		}
		avail := x + w - cx
		h, _ := l.flow(cx, cy, min(nat+1, avail), &fs)
		cx += min(nat, avail) + sp
		rowH = max(rowH, h)
	}
	return cy + rowH - y
}

// flattenBreaks joins paragraphs' spans with hard breaks.
func flattenBreaks(bs []richtext.Block) []richtext.Span {
	if len(bs) == 1 {
		return bs[0].Spans
	}
	var out []richtext.Span
	for i, b := range bs {
		if i > 0 {
			out = append(out, richtext.Span{Kind: richtext.Break})
		}
		out = append(out, b.Spans...)
	}
	return out
}

// naturalWidth is the max-content width of pending flow items: their
// widest hard-broken line.
func naturalWidth(items []item) int {
	var w, line float32
	for i := range items {
		if items[i].kind == itBreak {
			w, line = max(w, line), 0
			continue
		}
		line += items[i].w
	}
	return int(max(w, line) + 0.999)
}

// kitImage lays out an image block like Slack's Image: a title row
// (.p-image_block_title: the title in 15px rgb(97,96,97), " (12 kB)" when
// Slack sent image_bytes, and the hide/show toggle), then 5px down the
// image, at most 600×400 (scaled by min(600/w, 400/h) and floored, as
// Slack does) with radius 8 and a 1px inset otl-ter border. The toggle
// hides the image until it's clicked again.
func (l *lay) kitImage(img *richtext.Image, x, y, w int) int {
	p := &l.st.Palette
	fs := l.bodyStyle()
	fs.col = p.Text2
	if l.opts.Muted {
		fs.col = p.Text3
	}
	rowH, endX := fs.lineH, x
	if img.Title != "" {
		h, tw := l.plainFlow(img.Title, x, y, max(w-l.dp(24), l.dp(40)), &fs, -1)
		rowH = max(h, fs.lineH)
		endX = x + tw
		if n := len(l.d.lines); n > 0 {
			ln := l.d.lines[n-1]
			if ln.f1 > ln.f0 {
				f := l.d.frags[ln.f1-1]
				endX = int(f.x + f.w + 0.999)
			}
		}
	}
	lastTop := y + rowH - fs.lineH
	if img.Bytes > 0 {
		cs := fs
		cs.size = l.sp(13)
		_, sw := l.plainFlow(" ("+sizeText(img.Bytes)+")", endX, lastTop, l.dp(120), &cs, -1)
		endX += sw
	}
	// the toggle: a small blue caret after a space, centred on the line
	collapsed := l.collapsed[img]
	tg := l.d.addTarget(target{kind: tCollapse, img: img})
	cx := endX + l.dp(4) + l.dp(4)
	cy := lastTop + fs.lineH/2
	kind, cr := bCaret, image.Rect(cx-l.dp(4), cy-l.dp(2), cx+l.dp(4), cy+l.dp(3))
	if collapsed {
		kind, cr = bCaretRight, image.Rect(cx-l.dp(2), cy-l.dp(4), cx+l.dp(3), cy+l.dp(4))
	}
	l.d.boxes = append(l.d.boxes, box{kind: kind, r: cr, fill: p.Link, target: -1})
	l.d.hits = append(l.d.hits, hit{r: image.Rect(cx-l.dp(8), cy-l.dp(8), cx+l.dp(8), cy+l.dp(8)), t: tg})
	if collapsed {
		return rowH
	}
	iy := y + rowH + l.dp(5)
	iw, ih := l.kitImageSize(img, w)
	r := image.Rect(x, iy, x+iw, iy+ih)
	rad := l.dp(8)
	itg := l.d.addTarget(target{kind: tImage, img: img})
	l.d.imgs = append(l.d.imgs, imgPrim{r: r, url: img.URL, radii: [4]int{rad, rad, rad, rad}, target: itg, px: max(iw, ih)})
	l.d.hits = append(l.d.hits, hit{r: r, t: itg})
	l.d.boxes = append(l.d.boxes, box{kind: bBorder, r: r, radius: rad, stroke: p.Border, bw: max(1, l.dp(1)), target: -1, layer: 1})
	return iy + ih - y
}

// kitImageSize is an image block's size in px: Slack's min(600/w, 400/h)
// scale (floored, CSS px) when the image is bigger, then no wider than the
// pane. Without image_width/height Slack waits for the image; this uses
// the attachment default box.
func (l *lay) kitImageSize(img *richtext.Image, w int) (int, int) {
	if img.W <= 0 || img.H <= 0 {
		return fitSize(0, 0, w, l.dp(400), l)
	}
	cw, ch := float64(img.W), float64(img.H)
	if img.W > 600 || img.H > 400 {
		s := math.Min(600/cw, 400/ch)
		cw, ch = math.Floor(cw*s), math.Floor(ch*s)
	}
	pw, ph := cw*float64(l.scale), ch*float64(l.scale)
	if pw > float64(w) {
		ph, pw = ph*float64(w)/pw, float64(w)
	}
	return max(1, int(pw+0.5)), max(1, int(ph+0.5))
}

func (l *lay) metric() unit.Metric { return unit.Metric{PxPerDp: l.scale, PxPerSp: l.spx} }

// addSlot reserves r for a run of elements Options.Elements draws.
func (l *lay) addSlot(bs []richtext.Button, r image.Rectangle, acc bool) {
	l.d.slots = append(l.d.slots, slot{r: r, bs: bs, acc: acc})
	// a box so the doc's size covers the slot
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, target: -1})
}

// fields lays out Block Kit / attachment fields in two columns. short (may
// be nil) marks fields that share a row; nil means all are short.
func (l *lay) fields(fields [][]richtext.Span, short []bool, x, y, w int) int {
	colW := (w - l.dp(12)) / 2
	fy := y
	rowH := 0
	col := 0
	for i, f := range fields {
		isShort := short == nil || (i < len(short) && short[i])
		if col == 1 && !isShort {
			fy += rowH + l.dp(4)
			rowH, col = 0, 0
		}
		fx := x + col*(colW+l.dp(12))
		fw := colW
		if !isShort {
			fw = w
		}
		fs := l.bodyStyle()
		l.plainAdd("\n")
		l.addSpans(f, &fs)
		fh, _ := l.flow(fx, fy, fw, &fs)
		if fh > rowH {
			rowH = fh
		}
		if isShort && col == 0 {
			col = 1
		} else {
			fy += rowH + l.dp(4)
			rowH, col = 0, 0
		}
	}
	if col == 1 {
		fy += rowH + l.dp(4)
	}
	return fy - l.dp(4) - y
}

func (l *lay) buttonWidth(bt richtext.Button) int {
	k := faceKey{f: font.Font{Typeface: l.st.Face, Weight: font.Bold}, size: fixedPx(l.sp(13))}
	return max(l.dp(56), int(l.measure(bt.Text, k)+0.999)+2*l.dp(8))
}

// button draws one Block Kit button with its top-left at (x, y).
func (l *lay) button(bt richtext.Button, x, y int) int {
	p := &l.st.Palette
	bw := l.buttonWidth(bt)
	bh := l.dp(28)
	b := &bt
	tg := l.d.addTarget(target{kind: tButton, btn: b, url: bt.URL})
	r := image.Rect(x, y, x+bw, y+bh)
	col := p.Text
	switch bt.Style {
	case "primary":
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, radius: l.dp(4), fill: p.Primary, target: tg})
		col = p.PrimaryText
	case "danger":
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, radius: l.dp(4), fill: p.Danger, target: tg})
		col = p.DangerText
	default:
		l.d.boxes = append(l.d.boxes, box{kind: bBorder, r: r, radius: l.dp(4), fill: p.Panel, stroke: p.ButtonBorder, bw: l.dp(1), target: tg, hover: p.ButtonHoverBg})
	}
	fs := flowStyle{face: font.Font{Typeface: l.st.Face, Weight: font.Bold}, size: l.sp(13), lineH: bh, col: col, emoji: l.dp(16), maxLines: 1, noSelect: true}
	tw := int(l.measure(bt.Text, l.faceFor(fs.face, fs.size, 0)) + 0.999)
	l.plainFlow(bt.Text, x+(bw-tw)/2, y-l.dp(1), tw+l.dp(2), &fs, -1)
	l.d.hits = append(l.d.hits, hit{r: r, t: tg})
	return bh
}

func (l *lay) buttons(bs []richtext.Button, x, y, w int) int {
	if el := l.opts.Elements; el != nil {
		sz, _ := el.ElementsSize(bs, w, false, l.metric())
		l.addSlot(bs, image.Rect(x, y, x+sz.X, y+sz.Y), false)
		return sz.Y
	}
	cx, cy := x, y
	rowH := 0
	for _, bt := range bs {
		bw := l.buttonWidth(bt)
		if cx > x && cx+bw > x+w {
			cx = x
			cy += rowH + l.dp(8)
			rowH = 0
		}
		h := l.button(bt, cx, cy)
		if h > rowH {
			rowH = h
		}
		cx += bw + l.dp(8)
	}
	return cy + rowH - y
}

// image places an image scaled to fit (maxW, maxH) preserving its aspect
// ratio; returns its height.
func (l *lay) image(img *richtext.Image, x, y, maxW, maxH, radius int, tg int16) int {
	w, h := fitSize(img.W, img.H, maxW, maxH, l)
	r := image.Rect(x, y, x+w, y+h)
	l.d.imgs = append(l.d.imgs, imgPrim{r: r, url: img.URL, radii: [4]int{radius, radius, radius, radius}, target: tg, px: max(w, h)})
	if tg >= 0 {
		l.d.hits = append(l.d.hits, hit{r: r, t: tg})
	}
	return h
}

func fitSize(iw, ih, maxW, maxH int, l *lay) (int, int) {
	if iw <= 0 || ih <= 0 {
		return min(maxW, l.dp(360)), min(maxH, l.dp(240))
	}
	// Image sizes are CSS px; scale to device px.
	w := float32(iw) * l.scale
	h := float32(ih) * l.scale
	if w > float32(maxW) {
		h = h * float32(maxW) / w
		w = float32(maxW)
	}
	if h > float32(maxH) {
		w = w * float32(maxH) / h
		h = float32(maxH)
	}
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return int(w + 0.5), int(h + 0.5)
}

// table lays out a table: a rounded bordered grid, header row in bold,
// content-sized columns shrunk to fit, per-column alignment. A Block Kit
// table (Kit) is Slack's BaseTable: cells padding 2px 12px 0 and at least
// 28px tall (border included), 1px otl-ter borders, 4px outer radius; with
// more than 10 rows the message shows the first 10 under a 40px fade to
// the background (TablePreview; clicking opens the full table).
func (l *lay) table(b *richtext.Block, x, y, w int) int {
	p := &l.st.Palette
	rows := b.Rows
	preview := b.Kit && len(rows) > 10
	if preview {
		rows = rows[:10]
	}
	ncol := 0
	for _, r := range rows {
		ncol = max(ncol, len(r))
	}
	if ncol == 0 || len(rows) == 0 {
		return 0
	}
	padX, padT, padB, radius, minRow := l.dp(12), l.dp(8), l.dp(8), l.dp(8), 0
	if b.Kit {
		padT, padB, radius, minRow = l.dp(2), 0, l.dp(4), l.dp(27)
	}
	bw := l.dp(1)
	// natural (single-line) column widths
	nat := make([]int, ncol)
	k := l.faceFor(font.Font{Typeface: l.st.Face}, l.sp(l.st.TextSize), 0)
	kb := l.faceFor(font.Font{Typeface: l.st.Face}, l.sp(l.st.TextSize), richtext.Bold)
	for ri, r := range rows {
		for ci, cell := range r {
			kk := k
			if ri == 0 {
				kk = kb
			}
			cw := 0
			for _, line := range strings.Split(richtext.SpansText(cell), "\n") {
				cw = max(cw, int(l.measure(line, kk)+0.999))
			}
			nat[ci] = max(nat[ci], min(cw, l.dp(400))+2*padX)
		}
	}
	total := 0
	for _, c := range nat {
		total += c
	}
	avail := w - bw*(ncol+1)
	cols := make([]int, ncol)
	for i := range cols {
		cols[i] = nat[i]
		if total > avail && total > 0 {
			cols[i] = max(l.dp(48), nat[i]*avail/total)
		}
	}
	tw := bw * (ncol + 1)
	for _, c := range cols {
		tw += c
	}
	bi := len(l.d.boxes)
	l.d.boxes = append(l.d.boxes, box{kind: bBorder, fill: p.Panel, stroke: p.Border, bw: bw, radius: radius, target: -1})
	cy := y + bw
	for ri, r := range rows {
		if ri > 0 {
			l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(x+bw, cy, x+tw-bw, cy+bw), fill: p.Border, target: -1})
			cy += bw
		}
		rowH := 0
		cx := x + bw
		for ci := 0; ci < ncol; ci++ {
			if ci > 0 {
				cx += bw
			}
			cw := cols[ci]
			if ci < len(r) && len(r[ci]) > 0 {
				fs := l.bodyStyle()
				if ri == 0 {
					fs.face.Weight = font.Bold
				}
				if ci > 0 {
					l.plainAdd("\t")
				}
				f0, b0, h0 := len(l.d.frags), len(l.d.boxes), len(l.d.hits)
				l.addSpans(r[ci], &fs)
				h, wd := l.flow(cx+padX, cy+padT, cw-2*padX, &fs)
				align := ""
				if ci < len(b.ColAlign) {
					align = b.ColAlign[ci]
				}
				if dx := cw - 2*padX - wd; dx > 0 && (align == "right" || align == "center") {
					if align == "center" {
						dx /= 2
					}
					l.shift(f0, b0, h0, dx)
				}
				rowH = max(rowH, h)
			}
			cx += cw
		}
		rowH = max(max(rowH, l.dp(l.st.LineHeight))+padT+padB, minRow)
		// column separators for this row
		sx := x + bw
		for ci := 0; ci < ncol-1; ci++ {
			sx += cols[ci]
			l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(sx, cy, sx+bw, cy+rowH), fill: p.Border, target: -1})
			sx += bw
		}
		cy += rowH
		l.plainAdd("\n")
	}
	cy += bw
	l.d.boxes[bi].r = image.Rect(x, y, x+tw, cy)
	if preview {
		// linear-gradient(to top, background, transparent 40px) in bands
		const n = 10
		fh := l.dp(40)
		for i := 0; i < n; i++ {
			c := p.Panel
			c.A = uint8(255 * (float32(i) + 0.5) / n)
			r := image.Rect(x, cy-fh+fh*i/n, x+tw, cy-fh+fh*(i+1)/n)
			l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, fill: c, target: -1, layer: 1})
		}
	}
	return cy - y
}

// shift moves the frags, boxes and hit areas laid out since (f0, b0, h0)
// right by dx (aligned table cells).
func (l *lay) shift(f0, b0, h0, dx int) {
	for i := f0; i < len(l.d.frags); i++ {
		l.d.frags[i].x += float32(dx)
	}
	for i := b0; i < len(l.d.boxes); i++ {
		l.d.boxes[i].r = l.d.boxes[i].r.Add(image.Pt(dx, 0))
	}
	for i := h0; i < len(l.d.hits); i++ {
		l.d.hits[i].r = l.d.hits[i].r.Add(image.Pt(dx, 0))
	}
}
