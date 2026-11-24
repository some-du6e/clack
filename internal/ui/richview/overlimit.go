package richview

import (
	"image"
	"strconv"

	"gioui.org/font"

	"github.com/some-du6e/clack/internal/expandable"
	"github.com/some-du6e/clack/internal/richtext"
)

// MessageKitAttachments' limit (boot-shared moBr, build 9f450b8ad3db): a
// message draws its first 20 attachments, 10 once one of them has Block
// Kit blocks. The rest are an ExpandableContainer collapsed by default
// (useAttachmentRules false, shouldBeExpandedByDefault false) under
// getExpandableKey("attachmentsOverLimit", ts, {limit, viewContext}),
// with its ExpandableTrigger after them, MessageAttachmentOverLimitButton:
// a classic attachment row (.c-message_attachment: max-width 600, 8px
// under; its 4px base-ter __border, radius 8; the __body 12px in, 15px /
// 1.5) holding the button (.c-message_attachment__over_limit_button,
// content-hgl-1) and its bold text (.c-message_attachment__over_limit:
// padding 8px 0): "+ N more attachments" ("+ 1 more attachment") while
// they're hidden, "Show fewer attachments" under them once open. The
// state is the account's (the expandables store, expander.go); a doc
// drawn without a message ts keeps it in the View.

// attOverLimit and attOverLimitKit are MessageKitAttachments' el.
const attOverLimit, attOverLimitKit = 20, 10

// overLimitBtn is the button's state.
type overLimitBtn struct {
	key   string // the store's key; "" outside a message
	local string // then the View's toggle (tCollapse)
	n     int    // the attachments past the limit
	open  bool
}

// attachmentsOverLimit applies the limit to a message's top-level blocks:
// past it, the attachments go while collapsed, and the button's block
// (an attachment of its own, overLimit's) follows the last one drawn.
func (l *lay) attachmentsOverLimit(bs []richtext.Block) []richtext.Block {
	var idx []int
	limit := attOverLimit
	for i := range bs {
		b := &bs[i]
		if !l.overLimitCounts(b) {
			continue
		}
		idx = append(idx, i)
		if b.Att.Kit { // an attachment with blocks: el = 10
			limit = attOverLimitKit
		}
	}
	if len(idx) <= limit {
		return bs
	}
	ol := overLimitBtn{n: len(idx) - limit,
		key: expandable.Key(expandable.AttachmentsOverLimit, l.opts.TS, expandable.Opts{Limit: limit, ViewContext: l.opts.ViewContext})}
	if ol.key != "" {
		ol.open = xpOn(ol.key) // shouldBeExpandedByDefault false: no state is collapsed
	} else {
		ol.local = "attach_over:" + strconv.Itoa(limit)
		ol.open = l.collapsed[ol.local] // opened in this View
	}
	after := idx[len(idx)-1]
	hide := map[int]bool{}
	if !ol.open {
		after = idx[limit-1]
		for _, i := range idx[limit:] {
			hide[i] = true
		}
	}
	btn := &richtext.Attachment{}
	if l.overLimit == nil {
		l.overLimit = map[*richtext.Attachment]overLimitBtn{}
	}
	l.overLimit[btn] = ol
	out := make([]richtext.Block, 0, len(bs)+1)
	for i := range bs {
		if hide[i] {
			continue
		}
		out = append(out, bs[i])
		if i == after {
			out = append(out, richtext.Block{Kind: richtext.AttachmentBlock, Att: btn})
		}
	}
	return out
}

// overLimitCounts reports whether b is one of MessageKitAttachments'
// elements the limit counts: an attachment of the message's (not a
// message_embed block's card, nor a huddle_thread's), one it shows
// (getVisibleAttachmentsForMessage), and a work object only where it
// draws among them (not rearranged into the files' gallery).
func (l *lay) overLimitCounts(b *richtext.Block) bool {
	if b.Kind != richtext.AttachmentBlock || b.Att == nil {
		return false
	}
	a := b.Att
	if a.Embed || a.Huddle != nil || !shown(a) {
		return false
	}
	return !isWorkObjectBlock(b) || !l.st.woFlags.Rearrange
}

// overLimitButton lays out MessageAttachmentOverLimitButton at (x, y) and
// returns its height (the row's 8px margin is the next block's gap).
func (l *lay) overLimitButton(ol overLimitBtn, x, y, w int) int {
	p := &l.st.Palette
	w = min(w, l.dp(600))
	bar := l.dp(4)
	bi := len(l.d.boxes)
	l.d.boxes = append(l.d.boxes, box{kind: bFill, fill: p.Placeholder, radius: l.dp(8), target: -1})
	var tg int16
	if ol.key != "" {
		tg = l.d.addTarget(target{kind: tExpandable, id: ol.key, on: !ol.open})
	} else {
		tg = l.d.addTarget(target{kind: tCollapse, key: ol.local})
	}
	label := nsAttachments.T("{count, plural, =1 {+ # more attachment} other {+ # more attachments}}", "count", ol.n)
	if ol.open {
		label = nsAttachments.T("Show fewer attachments")
	}
	fs := flowStyle{face: font.Font{Typeface: l.st.Face, Weight: font.Bold}, size: l.sp(15), lineH: l.dp(22.5), col: p.Link, emoji: l.dp(22)}
	cx := x + bar + l.dp(12)
	pad := l.dp(8)
	th, tw := l.plainFlow(label, cx, y+pad, max(1, x+w-l.dp(12)-cx), &fs, tg)
	l.plainAdd("\n")
	h := th + 2*pad
	l.d.boxes[bi].r = image.Rect(x, y, x+bar, y+h)
	l.d.hits = append(l.d.hits, hit{r: image.Rect(cx, y, cx+tw, y+h), t: tg}) // the button: its text and padding
	return h
}
