package richview

import (
	"image"
	"math"
	"strconv"
	"strings"

	"gioui.org/font"

	"github.com/some-du6e/clack/internal/i18n"
)

// The collapsible media's own parts (boot-shared, build 9f450b8ad3db):
//
//   - AttachmentMediaTrigger (2mYZ) after an image block's title: " ",
//     the image's size "(12 kB)" (convertFilesize, R9ib) in
//     c-message_attachment__media_trigger--caption (13px, line-height
//     1.5), or when the image is too large (isImageTooLarge) the warning
//     in --too_large (15px, line-height 1.5, content-ter, wrapping):
//     "(Not automatically expanded because {filesize} is too large. You
//     can expand it anyway or open it in a new window.)", then the
//     ExpandableTrigger (/wuE: the legacy caret_down / caret_right in
//     content-hgl-1, title "Collapse" / "Expand"; a click sets the
//     opposite of what it shows).
//   - UnverifiedDisclosure (ljcx) where a collapsed ExpandableContainer
//     was collapsed by the unverified sender rule (expander.go): 13px
//     (line-height 1.38463), 4px under what's above, "This preview is
//     hidden because {memberName} is from outside your organization."
//     and 8px under it "Allow all previews from {memberName}, or just
//     show this one." (KHV7 link buttons: the first adds the sender to
//     allowed_unfurl_senders, the second expands this one). The file
//     list's is the pillow (p-unverified_disclosure--pillow: a 426px card
//     with a 1px foreground_low .13 ring, primary background, radius 4,
//     padding 12 16, the exclamation-circle font icon at margin-left −2px
//     and 12px left of the text, 4px above it and 8px under).

var (
	nsFilesize    = i18n.NS("filesize")
	nsAttachments = i18n.NS("attachments")
	nsFiles       = i18n.NS("files")
)

// convertFilesize is Slack's convertFilesize(n) (R9ib): SI units of
// 1000, the value rounded to a whole number, "0 bytes" for nothing.
func convertFilesize(n int64) string {
	if n == 0 {
		return nsFilesize.T("0 bytes")
	}
	if n < 1000 && n > -1000 {
		return nsFilesize.T("{bytes} B", "bytes", n)
	}
	units := [...]string{"{size} kB", "{size} MB", "{size} GB", "{size} TB", "{size} PB", "{size} EB", "{size} ZB", "{size} YB"}
	s, r := float64(n), -1
	for {
		s /= 1000
		r++
		if !(math.Abs(s) >= 1000 && r < 7) {
			break
		}
	}
	return nsFilesize.T(units[r], "size", strconv.FormatInt(int64(math.Floor(s+0.5)), 10))
}

// giphyGIF is mLfb's i: a Giphy or Tenor GIF (or WebP), whose size the
// image block's trigger doesn't show.
func giphyGIF(u string) bool {
	if !strings.Contains(u, "giphy.com") && !strings.Contains(u, "media.tenor.com") {
		return false
	}
	p, _, _ := strings.Cut(u, "?")
	return strings.HasSuffix(p, ".gif") || strings.HasSuffix(p, ".webp")
}

// mediaTrigger lays out an AttachmentMediaTrigger after a title that
// ended at (endX, lastTop) on lines lineH tall, starting a line at x when
// it wraps, and returns where it ends. bytes is the trigger's imageBytes
// (0: none); open is what the caret shows; key the expandable it sets.
func (l *lay) mediaTrigger(key, url string, bytes int64, open, tooLarge bool, x, endX, lastTop, w int, fs flowStyle) (int, int) {
	p := &l.st.Palette
	if bytes > 0 && tooLarge {
		// --too_large: 15px/1.5 in content-ter, wrapping like the title
		ts := fs
		ts.size, ts.lineH, ts.col = l.sp(15), l.dp(22.5), p.Text3
		k := l.faceFor(ts.face, ts.size, 0)
		l.addText(" ", &ts, k, ts.col, 0, -1, -1)
		parts := nsAttachments.RT(`(Not automatically expanded because {filesize} is too large. You can <a className="expand">expand it anyway</a> or <a className="open">open it in a new window</a>.)`,
			"filesize", convertFilesize(bytes))
		for _, pt := range parts {
			tg, col := int16(-1), ts.col
			switch pt.Class {
			case "expand":
				tg, col = l.d.addTarget(target{kind: tExpandable, id: key, on: true, underline: true}), p.Link
			case "open":
				tg, col = l.d.addTarget(target{kind: tLink, url: url, underline: true}), p.Link
			}
			l.addText(pt.Text, &ts, k, col, 0, -1, tg)
		}
		l.lead = float32(endX - x) // on from the title's last line
		l.flow(x, lastTop, w, &ts)
		l.plainAdd("\n")
		endX, lastTop = l.lastLineEnd(endX, lastTop)
	} else if bytes > 0 {
		cs := fs
		cs.size = l.sp(13)
		_, sw := l.plainFlow(" ("+convertFilesize(bytes)+")", endX, lastTop, l.dp(120), &cs, -1)
		endX += sw
	}
	// the ExpandableTrigger: a small blue caret after a space, centred on
	// the line; it sets the opposite of what it shows
	tg := l.d.addTarget(target{kind: tExpandable, id: key, on: !open})
	cx := endX + l.dp(4) + l.dp(4)
	cy := lastTop + fs.lineH/2
	kind, cr := bCaret, image.Rect(cx-l.dp(4), cy-l.dp(2), cx+l.dp(4), cy+l.dp(3))
	if !open {
		kind, cr = bCaretRight, image.Rect(cx-l.dp(2), cy-l.dp(4), cx+l.dp(3), cy+l.dp(4))
	}
	l.d.boxes = append(l.d.boxes, box{kind: kind, r: cr, fill: p.Link, target: -1})
	l.d.hits = append(l.d.hits, hit{r: image.Rect(cx-l.dp(8), cy-l.dp(8), cx+l.dp(8), cy+l.dp(8)), t: tg})
	return cx + l.dp(4), lastTop
}

// lastLineEnd is where the last laid-out line ends: its last frag's right
// edge and its top (the fallback when no line was laid out).
func (l *lay) lastLineEnd(x, top int) (int, int) {
	n := len(l.d.lines)
	if n == 0 {
		return x, top
	}
	ln := l.d.lines[n-1]
	if ln.f1 > ln.f0 {
		f := l.d.frags[ln.f1-1]
		return int(f.x + f.w + 0.999), int(f.top)
	}
	return x, top
}

// unverifiedDisclosure lays out an UnverifiedDisclosure for the media
// under key, sent by sender, at (x, y) and returns its height with its
// margins (4px over it; the pillow's 8px under it too).
func (l *lay) unverifiedDisclosure(key string, x, y, w int, pillow bool) int {
	p := &l.st.Palette
	sender := l.opts.Sender
	name := xp().PrefMemberName(sender)
	y0 := y
	y += l.dp(4)
	cx, cw := x, w
	pad, padX := 0, 0
	bi := -1
	if pillow {
		pad, padX = l.dp(12), l.dp(16)
		cw = min(w, l.dp(426))
		bi = len(l.d.boxes)
		l.d.boxes = append(l.d.boxes, box{kind: bBorder, fill: p.Panel, stroke: rgba(p.Text.R, p.Text.G, p.Text.B, 0.13), bw: max(1, l.dp(1)), radius: l.dp(4), target: -1})
		// the c-icon--exclamation-circle glyph (U+E210) in its 20px c-icon
		// box, foreground_max .7: .c-icon--exclamation-circle's margin-left
		// -2px, .p-unverified_disclosure__icon's margin-right 12px
		s := l.dp(20)
		ix := x + padX - l.dp(2)
		l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(ix, y+pad, ix+s, y+pad+s), url: iconURL + "exclamation-circle-font", target: -1, px: s,
			tint: rgba(p.Text.R, p.Text.G, p.Text.B, 0.7)})
		cx = ix + s + l.dp(12)
		cw = x + cw - padX - cx
	}
	fs := flowStyle{face: font.Font{Typeface: l.st.Face}, size: l.sp(13), lineH: l.dp(18), col: p.Text, emoji: l.dp(16)}
	ty := y + pad
	h, _ := l.plainFlow(nsFiles.T("This preview is hidden because {memberName} is from outside your organization.", "memberName", name), cx, ty, cw, &fs, -1)
	l.plainAdd("\n")
	ty += h + l.dp(8)
	k := l.faceFor(fs.face, fs.size, 0)
	for _, pt := range nsFiles.RT("<all>Allow all previews from {memberName}</all>, or\u00a0<single>just show this one</single>.", "memberName", name) {
		tg, col := int16(-1), fs.col
		switch pt.Tag {
		case "all":
			tg, col = l.d.addTarget(target{kind: tAllowPreviews, id: sender, underline: true}), p.Link
		case "single":
			tg, col = l.d.addTarget(target{kind: tExpandable, id: key, on: true, underline: true}), p.Link
		}
		l.addText(pt.Text, &fs, k, col, 0, -1, tg)
	}
	ah, _ := l.flow(cx, ty, cw, &fs)
	l.plainAdd("\n")
	ty += ah + pad
	if pillow {
		l.d.boxes[bi].r = image.Rect(x, y, x+min(w, l.dp(426)), ty)
		ty += l.dp(8)
	}
	return ty - y0
}
