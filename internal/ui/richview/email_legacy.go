package richview

import (
	"image"
	"image/color"
	"strconv"
	"strings"
	"time"

	"gioui.org/font"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
)

// The email file with new_email_rendering off: Slack's FileEmail
// (boot-shared 2bi5, `ee = lk(FileEmail)`, build 031dbe6a7400), a file
// pillow (.c-pillow_file: 12px in, the card's border and radius) at most
// .p-message_file--email's 594px, in .c-pillow_file__email's 15px text on
// 22px lines.
//
//   - Collapsed (an expandable file not yet expanded): the small density
//     view (file-email-collapsed-small-view), a click expands it
//     (expandEmail: fetchAndExpand). Its first line is the subject in bold,
//     cut with "…", and the email's date on the right
//     (toHumanReadableDateAndTimePure, sk_foreground_max_solid, 8px from
//     the subject); its second " From {name or address} " in that grey, 8px
//     before the start of preview_plain_text ("(No content)"), cut on one
//     line. Then the attachments, at most four (past four, three and
//     "+ {count} attachments").
//   - Expanded: the header table (From, To, Cc, Subject, Date; the labels
//     right-aligned in .5 grey, 8px before their values; a sender is its
//     name then <address> in .5 grey), ruled off under the pillow's top
//     12px, then the email in EmailIframe's box (getEmailContentHeight is
//     "max" with the experiment off: 400px), then the attachments with
//     their count ("{count} attachments", and "{count} attachments
//     removed" when Slack dropped some).
//
// An attachment is .c-pillow_file__email__attachment_alt: 12px under the
// one before, 8px in, the 24px #1D9BD1 tile with the white paperclip 8px
// before its name in bold (cut with "…") and its size and type in 13px
// grey; the list is ruled off 12px under what's above it. A click opens
// the file (openLinkWithIpCheck's "view").

// emailLegacy lays out f as FileEmail from (x, y) in w; it returns the
// pillow's height.
func (l *lay) emailLegacy(f *richtext.File, x, y, w int) int {
	p := &l.st.Palette
	e := f.Email
	cw := min(w, l.dp(emailFileMaxW))
	pad := l.dp(12)
	ix, iw := x+pad, cw-2*pad
	// the pillow: the file's context menu (and FileRects); collapsed, a
	// click anywhere on it but its attachments expands it (expandEmail):
	// that hit sits over the pillow's, under the attachments' links
	card := l.d.addTarget(target{kind: tFile, file: f, menuOnly: true})
	boxT, expand := card, int16(-1)
	if !f.Expanded {
		expand = l.d.addTarget(target{kind: tSnippet, file: f, act: FileShowMore})
		boxT = expand
	}
	bi := len(l.d.boxes)
	l.d.boxes = append(l.d.boxes, box{kind: bBorder, fill: p.Panel, stroke: p.Border, bw: l.dp(1), radius: l.dp(12), target: boxT, hover: p.BorderHover})
	hi := len(l.d.hits)
	l.d.hits = append(l.d.hits, hit{t: card})
	if expand >= 0 {
		l.d.hits = append(l.d.hits, hit{t: expand})
	}
	grey := Over(rgba(p.Text.R, p.Text.G, p.Text.B, 0.5), p.Panel)
	date := ""
	if e.Timestamp > 0 {
		date = humanTs(strconv.FormatInt(e.Timestamp, 10), time.Now())
	}
	text := func(s string, x, y, w int, bold bool, col colorRef) int {
		fs := l.bodyStyle()
		fs.size, fs.lineH = l.sp(15), l.dp(22)
		fs.maxLines, fs.ellipsis = 1, true
		if bold {
			fs.face.Weight = font.Bold
		}
		if col.set {
			fs.col = col.c
		}
		h, _ := l.plainFlow(s, x, y, w, &fs, -1)
		return h
	}
	subject := firstNonEmptyStr(e.Subject, i18n.T("files", "(No Subject)"))
	cy := y + pad
	if !f.Expanded {
		// the small density view: subject … date, then From … the text
		dw := 0
		if date != "" {
			dw = int(l.measure(date, l.faceFor(l.bodyStyle().face, l.sp(15), 0)) + 0.999)
			text(date, ix+iw-dw, cy, dw+l.dp(2), false, colorRef{set: true, c: p.Text2})
			dw += l.dp(8)
		}
		text(subject, ix, cy, iw-dw, true, colorRef{})
		cy += l.dp(22)
		l.plainAdd("\n")
		from := " From " + emailSenderName(e) + " " // renderSmallContent's template, not a translation
		fw := int(l.measure(from, l.faceFor(l.bodyStyle().face, l.sp(15), 0)) + 0.999)
		text(from, ix, cy, min(fw+l.dp(2), iw), false, colorRef{set: true, c: p.Text2})
		bx := ix + fw + l.dp(8)
		text(firstNonEmptyStr(oneLine(e.Preview), i18n.T("files", "(No content)")), bx, cy, max(ix+iw-bx, 0), false, colorRef{})
		cy += l.dp(22)
		cy = l.emailLegacyAttachments(e, ix, cy, iw, true)
	} else {
		// the header table, ruled off from the email (margin -12px,
		// padding 12px: the rule runs edge to edge)
		type row struct {
			label string
			value func(x, y, w int) int
		}
		lw := 0
		labels := []string{i18n.T("files", "From"), i18n.T("files", "To"), i18n.T("files", "Cc"), i18n.T("files", "Subject"), i18n.T("files", "Date")}
		for _, s := range labels {
			lw = max(lw, int(l.measure(s, l.faceFor(l.bodyStyle().face, l.sp(15), 0))+0.999))
		}
		senders := func(as []richtext.EmailAddress) func(x, y, w int) int {
			return func(x, y, w int) int { return l.emailSenders(as, x, y, w, grey) }
		}
		rows := []row{{labels[0], senders(firstAddress(e.From))}, {labels[1], senders(e.To)}}
		if len(e.Cc) > 0 {
			rows = append(rows, row{labels[2], senders(e.Cc)})
		}
		rows = append(rows, row{labels[3], func(x, y, w int) int {
			fs := l.bodyStyle()
			fs.size, fs.lineH = l.sp(15), l.dp(22)
			fs.face.Weight = font.Bold
			h, _ := l.plainFlow(subject, x, y, w, &fs, -1)
			return h
		}})
		if date != "" {
			rows = append(rows, row{labels[4], func(x, y, w int) int { return text(date, x, y, w, false, colorRef{}) }})
		}
		vx := ix + lw + l.dp(8)
		for _, r := range rows {
			text(r.label, ix+lw-int(l.measure(r.label, l.faceFor(l.bodyStyle().face, l.sp(15), 0))+0.999), cy, lw+l.dp(2), false, colorRef{set: true, c: grey})
			h := max(r.value(vx, cy, ix+iw-vx), l.dp(22))
			cy += h
			l.plainAdd("\n")
		}
		cy += pad
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(x+l.dp(1), cy, x+cw-l.dp(1), cy+l.dp(1)), fill: Over(rgba(p.Text.R, p.Text.G, p.Text.B, 0.13), p.Panel), target: -1})
		cy += l.dp(1)
		// the email: EmailIframe's box, 400px ("max")
		limit := cy + l.dp(400)
		m := l.mark()
		t0 := len(l.d.targets)
		by := cy + l.dp(8)
		for _, b := range richtext.EmailBlocksOf(e.HTML) {
			if by >= limit {
				break
			}
			by += l.blocks([]richtext.Block{b}, ix, by, iw, false) + l.dp(8)
		}
		l.markEmailLinks(t0)
		l.clipSince(m, limit)
		cy = limit
		cy = l.emailLegacyAttachments(e, ix, cy, iw, false)
	}
	h := cy + pad - y
	r := image.Rect(x, y, x+cw, y+h)
	l.d.boxes[bi].r = r
	l.d.hits[hi].r = r
	if expand >= 0 {
		l.d.hits[hi+1].r = r
	}
	l.pillowBar(f, r) // FileActions (file_actions.go)
	return h
}

// colorRef is an optional text colour.
type colorRef struct {
	set bool
	c   color.NRGBA
}

// emailSenderName is renderSmallContent's sender: the first From's name,
// else its address.
func emailSenderName(e *richtext.Email) string {
	if len(e.From) == 0 {
		return ""
	}
	return firstNonEmptyStr(e.From[0].Name, e.From[0].Address)
}

func firstAddress(as []richtext.EmailAddress) []richtext.EmailAddress {
	if len(as) > 1 {
		return as[:1]
	}
	return as
}

// oneLine is s's line breaks as spaces (a line cut with "…").
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// emailSenders is formatSender for each of as, comma-separated: the name
// (bold-free, the text's colour), a space, then <address> in grey. It
// returns the height.
func (l *lay) emailSenders(as []richtext.EmailAddress, x, y, w int, grey color.NRGBA) int {
	fs := l.bodyStyle()
	fs.size, fs.lineH = l.sp(15), l.dp(22)
	k := l.faceFor(fs.face, fs.size, 0)
	for i, a := range as {
		if a.Name != "" {
			l.addText(a.Name+" ", &fs, k, fs.col, 0, -1, -1)
		}
		if a.Address != "" {
			l.addText("<"+a.Address+">", &fs, k, grey, 0, -1, -1)
		}
		if i < len(as)-1 {
			l.addText(", ", &fs, k, fs.col, 0, -1, -1)
		}
	}
	h, _ := l.flow(x, y, w, &fs)
	return h
}

// emailLegacyAttachments is FileEmail's attachments from cy: small is the
// collapsed view's (renderSmallViewAttachments: no count, at most four),
// else renderAttachments' (the count first). It returns the new cy.
func (l *lay) emailLegacyAttachments(e *richtext.Email, x, cy, w int, small bool) int {
	p := &l.st.Palette
	n := len(e.Attachments)
	if n == 0 && (small || e.Removed == 0) {
		return cy
	}
	grey := Over(rgba(p.Text.R, p.Text.G, p.Text.B, 0.5), p.Panel)
	cy += l.dp(12) // .c-pillow_file__email__attachments: margin-top 12px, a rule
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(x, cy, x+w, cy+l.dp(1)), fill: Over(rgba(p.Text.R, p.Text.G, p.Text.B, 0.13), p.Panel), target: -1})
	cy += l.dp(1)
	line := func(s string) {
		cy += l.dp(12) // __attachments__count: margin-top 12px
		fs := l.bodyStyle()
		fs.size, fs.lineH, fs.col = l.sp(15), l.dp(22), grey
		fs.maxLines, fs.ellipsis, fs.noSelect = 1, true, true
		h, _ := l.plainFlow(s, x, cy, w, &fs, -1)
		cy += h
		l.plainAdd("\n")
	}
	shown := e.Attachments
	more := 0
	if small && n > 4 {
		shown, more = e.Attachments[:3], n-3
	}
	if !small {
		if e.Removed > 0 {
			line(i18n.T("files", "{count, plural, =1 {# attachment} other {# attachments}} removed", "count", e.Removed))
		} else {
			line(i18n.T("files", "{count, plural, =1 {# attachment} other {# attachments}}", "count", n))
		}
	}
	for i := range shown {
		a := &shown[i]
		cy += l.dp(12)
		tg := l.d.addTarget(target{kind: tLink, url: a.URL})
		pad := l.dp(8)
		rh := l.dp(22) + 2*pad
		l.d.hits = append(l.d.hits, hit{r: image.Rect(x, cy, x+w, cy+rh), t: tg})
		ic := l.dp(24)
		l.svgIconTile(x+pad, cy+pad-l.dp(1), ic, emailIconBlue, "attach")
		tx := x + pad + ic + l.dp(8)
		fs := l.bodyStyle()
		fs.size, fs.lineH = l.sp(15), l.dp(22)
		fs.face.Weight = font.Bold
		fs.maxLines, fs.ellipsis = 1, true
		meta := strings.TrimSpace(sizeText(a.Size) + " " + a.Mime)
		ms := l.small(grey)
		ms.maxLines, ms.ellipsis, ms.noSelect = 1, true, true
		mw := int(l.measure(meta, l.faceFor(ms.face, ms.size, 0)) + 0.999)
		nw := max(x+w-pad-tx-mw-l.dp(8), l.dp(40))
		l.plainFlow(a.Name, tx, cy+pad, nw, &fs, tg)
		l.plainFlow(meta, tx+min(int(l.measure(a.Name, l.faceFor(fs.face, fs.size, 0))+0.999), nw)+l.dp(8), cy+pad+l.dp(3), mw+l.dp(2), &ms, -1)
		cy += rh
		l.plainAdd("\n")
	}
	if more > 0 {
		line(i18n.T("files", "+ {count} attachments", "count", more))
	}
	return cy
}
