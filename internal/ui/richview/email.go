package richview

// An email file in a message (mode "email": the Email app's messages, an
// email sent to a channel's address), as Slack's web client draws it with
// new_email_rendering on (a team experiment, on for Hack Club, read
// through core: Style.SetNewEmailRendering; off, email_legacy.go draws
// the legacy FileEmail; boot-shared 2bi5, build 031dbe6a7400):
//
//   - FilesMetaTextWrapper's line above it reads the email's kind
//     (renderEmailMetaText: "Email", or "Email from {appName}" when the
//     file names its app; filemeta.go), its title once collapsed.
//   - MessageFile classes it p-message_file--email (max-width 594px) and
//     has no click of its own (isExpandable is true for every email,
//     3tzn). Its MessageFileHeader is a role=button around the file
//     entity (+SxN): the email file icon (c-icon--file-email-svgicon:
//     email-5606c9a.svg, a #1D9BD1 tile and a white envelope), the title
//     (the subject) in bold, and under it the message's time as
//     toHumanReadableDateAndTimePure has it (LYtI: "Today at 4:16 PM",
//     "Yesterday at …", else the date: "Nov 17th"). A click, Enter or
//     Space opens the email in the secondary view (toFileDetails,
//     EmailFileDetails: OpenFile → messages.EmailOpener).
//   - FileActions shows Expand (expand-diagonal, renderEmailExpand) on a
//     collapsed email and Collapse (reduce-diagonal) on an expanded one
//     (file_actions.go). Expand is fetchAndExpand (files.info), the
//     snippets' expandable (core.ExpandFile).
//   - Expanded, FileEmailV2 draws under the header
//     (.c-pillow_file__email__unfurl: margin 12px -12px -12px, edge to
//     edge): the recipient rows (.c-pillow_file__email__meta__recipient:
//     a 1px sk_foreground_low_solid line above and below them and between
//     rows, margin 0 12px, padding 8px 0, gap 8: the 36px label in 15px
//     content-ter, then the values wrapping 4px apart), From with the
//     "Heads up" warning (13px, foreground .5) when the sender's DMARC
//     check failed, To, Cc; the body (EmailIframe: 400px tall, 150 for a
//     plain text of 150 characters and 3 lines or less, 300 for 400 and
//     8); the attachments (.c-pillow_file__email__attachments_container:
//     a line above, 8px padding).
//   - A recipient (q3ZI EmailRecipient): a member Slack knows (its
//     slack_user_id) is their member token (opens their profile); a name
//     that is a channel id with no address is the channel; an address is
//     the name (15px content-pry) and the address chip
//     (emailAddressContent: 22px, padding 0 4px, radius 4, surf-hgl-1, the
//     16px email glyph 4px before the address, a mailto link); else the
//     name, or "Email".
//
// The body is the email's own HTML in Slack's iframe; Clack lays out the
// same HTML as blocks (richtext.EmailBlocks), cut at the iframe's height.

import (
	"image"
	"strings"
	"time"
	"unicode/utf16"

	"gioui.org/font"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// emailFileMaxW is .p-message_file--email's max-width.
const emailFileMaxW = 594

// isEmailFile reports whether f draws as an email (getFileType "email").
func isEmailFile(f *richtext.File) bool { return f != nil && f.Email != nil }

// emailIconBlue is email-5606c9a.svg's tile.
var emailIconBlue = rgb(0x1d, 0x9b, 0xd1)

// emailIcon is the email file icon at (x, y), s px: the tile (radius
// 800/3600 of its side) and the envelope (icons' file-email).
func (l *lay) emailIcon(x, y, s int) { l.svgIconTile(x, y, s, emailIconBlue, "file-email") }

// humanTs is toHumanReadableDateAndTimePure (LYtI, uncapitalized false):
// "Today at 4:16 PM", "Yesterday at …" or "Tomorrow at …", else
// toCalendarDateOrNameDay's date alone ("Nov 17th", ", 2025" in another
// year).
func humanTs(ts string, now time.Time) string {
	t, date, ok := calendarDay(ts, now)
	if !ok {
		return ""
	}
	if sameDayAs(t, now) || sameDayAs(t, now.AddDate(0, 0, -1)) || sameDayAs(t, now.AddDate(0, 0, 1)) {
		return i18n.T("message", "{relativeDay} at {time}", "relativeDay", date, "time", uipref.Clock(t))
	}
	return date
}

func sameDayAs(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// emailBodyH is EmailIframe's height class for plain text pt
// (getEmailContentHeight): small 150px, medium 300px, else 400px.
func emailBodyH(pt string) int {
	n := len(utf16.Encode([]rune(pt))) // getEmailContentHeight's e.length: UTF-16 units
	lines := strings.Count(pt, "\n") + 1
	switch {
	case pt == "":
		return 400
	case n <= 150 && lines <= 3:
		return 150
	case n <= 400 && lines <= 8:
		return 300
	}
	return 400
}

// emailFile lays out an email file at (x, y) in w and returns its height.
func (l *lay) emailFile(f *richtext.File, x, y, w int) int {
	p := &l.st.Palette
	cw := min(w, l.dp(emailFileMaxW))
	pad := l.dp(12)
	ic := l.dp(36)
	// the pillow: the file's context menu; the header is the button
	card := l.d.addTarget(target{kind: tFile, file: f, menuOnly: true})
	bi := len(l.d.boxes)
	l.d.boxes = append(l.d.boxes, box{kind: bBorder, fill: p.Panel, stroke: p.Border, bw: l.dp(1), radius: l.dp(12), target: card, hover: p.BorderHover})
	hi := len(l.d.hits)
	l.d.hits = append(l.d.hits, hit{t: card})

	head := l.d.addTarget(target{kind: tFile, file: f})
	headH := ic + l.dp(2)
	l.d.hits = append(l.d.hits, hit{r: image.Rect(x, y, x+cw, y+pad+headH+pad), t: head})
	l.emailIcon(x+pad, y+pad, ic)
	tx := x + pad + ic + pad
	fs := l.bodyStyle()
	fs.face.Weight = font.Bold
	fs.size, fs.lineH = l.sp(16), l.dp(20)
	fs.maxLines, fs.ellipsis = 1, true
	l.plainFlow(fileName(f), tx, y+pad-l.dp(1), x+cw-tx-pad, &fs, head)
	ms := l.small(p.Text2)
	ms.maxLines, ms.ellipsis, ms.noSelect = 1, true, true
	l.plainFlow(humanTs(l.opts.TS, time.Now()), tx, y+pad+l.dp(20), x+cw-tx-pad, &ms, -1)
	h := pad + headH + pad
	if f.Expanded {
		l.plainAdd("\n")
		h = pad + headH + pad + l.emailUnfurl(f, x, y+pad+headH+pad, cw)
	}
	r := image.Rect(x, y, x+cw, y+h)
	l.d.boxes[bi].r = r
	l.d.hits[hi].r = r
	l.pillowBar(f, r) // FileActions (file_actions.go)
	return h
}

// emailUnfurl is FileEmailV2 from (x, y), the pillow's width w: the
// recipient rows, the body and the attachments. It returns its height.
func (l *lay) emailUnfurl(f *richtext.File, x, y, w int) int {
	p := &l.st.Palette
	e := f.Email
	y0 := y
	bw := l.dp(1) // the pillow's border: the unfurl runs edge to edge inside it
	rule := func(at int) {
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(x+bw, at, x+w-bw, at+l.dp(1)), fill: p.Border, target: -1})
	}
	type row struct {
		label string
		rcpt  []richtext.EmailAddress
		warn  bool
	}
	var rows []row
	if len(e.From) > 0 {
		rows = append(rows, row{label: i18n.T("files", "From"), rcpt: e.From[:1], warn: e.Unverified})
	}
	if len(e.To) > 0 {
		rows = append(rows, row{label: i18n.T("files", "To"), rcpt: e.To})
	}
	if len(e.Cc) > 0 {
		rows = append(rows, row{label: i18n.T("files", "Cc"), rcpt: e.Cc})
	}
	rule(y) // the container's border-top
	y += l.dp(1)
	for i, r := range rows {
		if i > 0 {
			rule(y) // :not(:last-child)'s border-bottom
			y += l.dp(1)
		}
		y += l.dp(8)
		lx := x + l.dp(12)
		ls := l.bodyStyle()
		ls.size, ls.lineH, ls.col = l.sp(15), l.dp(22), p.Text2
		ls.maxLines, ls.ellipsis, ls.noSelect = 1, true, true
		l.plainFlow(r.label, lx, y, l.dp(36), &ls, -1)
		vx := lx + l.dp(36) + l.dp(8)
		vw := x + w - l.dp(12) - vx
		hgt := l.emailRecipientRow(f, r.label, r.rcpt, vx, y, vw)
		if r.warn {
			ws := l.small(Over(rgba(p.Text.R, p.Text.G, p.Text.B, 0.5), p.Panel))
			wh, _ := l.plainFlow(i18n.T("files", "Heads up — we couldn’t verify this email’s sender. Make sure you know them before you open any links or attachments."), vx, y+hgt+l.dp(4), vw, &ws, -1)
			hgt += l.dp(4) + wh
		}
		y += max(hgt, l.dp(22)) + l.dp(8)
	}
	rule(y) // the container's border-bottom
	y += l.dp(1)

	// the body: EmailIframe's box, the email laid out and cut at it
	bh := l.dp(float32(emailBodyH(e.PlainText)))
	l.plainAdd("\n")
	m := l.mark()
	t0 := len(l.d.targets)
	bx := x + l.dp(12)
	limit := y + bh
	by := y + l.dp(8)
	for _, b := range richtext.EmailBlocksOf(e.HTML) {
		if by >= limit {
			break
		}
		bb := []richtext.Block{b}
		hh := l.blocks(bb, bx, by, x+w-l.dp(12)-bx, false)
		by += hh + l.dp(8)
	}
	l.markEmailLinks(t0)
	l.clipSince(m, limit)
	y = limit

	if len(e.Attachments) > 0 {
		rule(y)
		y += l.dp(1) + l.dp(8)
		for i := range e.Attachments {
			a := &e.Attachments[i]
			af := &richtext.File{ID: a.FileID, Name: a.Name, Title: a.Name, Mime: a.Mime, Size: a.Size, URL: a.URL, Type: attachmentType(a)}
			if i > 0 {
				y += l.dp(8)
				l.plainAdd("\n")
			} else {
				l.plainAdd("\n")
			}
			y += l.emailAttachment(af, x+l.dp(8), y, w-2*l.dp(8))
		}
		y += l.dp(8)
	}
	return y - y0
}

// markEmailLinks flags the links laid out since target t0 as an email
// body's.
func (l *lay) markEmailLinks(t0 int) {
	for i := t0; i < len(l.d.targets); i++ {
		if l.d.targets[i].kind == tLink {
			l.d.targets[i].emailLink = true
		}
	}
}

// fullEmailTime is the Timestamp EmailFileDetailsContent's header shows
// (full, showWeekday): "Monday, November 17th at 4:16 PM", the year after
// the date in another year.
func fullEmailTime(ts int64, now time.Time) string {
	if ts <= 0 {
		return ""
	}
	t := time.Unix(ts, 0).In(now.Location())
	d := t.Format("Monday, January 2") + ordinal(t.Day())
	if t.Year() != now.Year() {
		d += t.Format(", 2006")
	}
	return d + " at " + uipref.Clock(t)
}

// emailDetails lays out an email file as EmailFileDetailsContent (FkQ0)
// from (x, y) in the pane's width w:
//
//   - emailSubject (padding 16px 20px 0): the file entity, its time the
//     email's own (Timestamp full, showWeekday);
//   - content (a column 20px apart, padding 16px 0 0 20px): From, To and
//     Cc sections (EmailMetadataSection: the label in 15px content-ter
//     over the recipients, 8px apart), Body (the whole email; Slack's
//     iframe sits 12px further left, emailIframe), Attachments (the
//     files).
func (l *lay) emailDetails(f *richtext.File, x, y, w int) int {
	p := &l.st.Palette
	e := f.Email
	y0 := y
	ic := l.dp(36)
	hx, hy := x+l.dp(20), y+l.dp(16)
	l.emailIcon(hx, hy, ic)
	tx := hx + ic + l.dp(12)
	fs := l.bodyStyle()
	fs.face.Weight = font.Bold
	fs.size, fs.lineH = l.sp(16), l.dp(20)
	fs.maxLines, fs.ellipsis = 1, true
	l.plainFlow(fileName(f), tx, hy-l.dp(1), x+w-l.dp(20)-tx, &fs, -1)
	ms := l.small(p.Text2)
	ms.maxLines, ms.ellipsis = 1, true
	l.plainFlow(fullEmailTime(e.Timestamp, time.Now()), tx, hy+l.dp(20), x+w-l.dp(20)-tx, &ms, -1)
	l.plainAdd("\n")
	y = hy + ic + l.dp(2) + l.dp(16)
	cx := x + l.dp(20)
	cw := x + w - l.dp(20) - cx
	label := func(s string) {
		ls := l.bodyStyle()
		ls.size, ls.lineH, ls.col = l.sp(15), l.dp(22), p.Text2
		ls.noSelect = true
		lh, _ := l.plainFlow(s, cx, y, cw, &ls, -1)
		l.plainAdd("\n")
		y += lh + l.dp(8)
	}
	section := func(name string, rs []richtext.EmailAddress) {
		if len(rs) == 0 {
			return
		}
		label(name)
		y += l.emailRecipients(rs, cx, y, cw) + l.dp(20)
		l.plainAdd("\n")
	}
	if len(e.From) > 0 {
		section(i18n.T("email-file-details", "From"), e.From[:1])
	}
	section(i18n.T("email-file-details", "To"), e.To)
	section("Cc", e.Cc) // "Cc" untranslated without email_translation
	label(i18n.T("email-file-details", "Body"))
	t0 := len(l.d.targets)
	bx := cx
	for i, b := range richtext.EmailBlocksOf(e.HTML) {
		if i > 0 {
			y += l.dp(8)
		}
		y += l.blocks([]richtext.Block{b}, bx, y, x+w-l.dp(20)-bx, false)
	}
	l.markEmailLinks(t0)
	y += l.dp(20)
	if len(e.Attachments) > 0 {
		l.plainAdd("\n")
		label(i18n.T("email-file-details", "Attachments"))
		for i := range e.Attachments {
			a := &e.Attachments[i]
			af := &richtext.File{ID: a.FileID, Name: a.Name, Title: a.Name, Mime: a.Mime, Size: a.Size, URL: a.URL, Type: attachmentType(a)}
			if i > 0 {
				y += l.dp(8)
			}
			l.plainAdd("\n")
			y += l.emailAttachment(af, cx, y, cw)
		}
		y += l.dp(20)
	}
	return y - y0
}

// attachmentType is an email attachment's filetype for its icon: the
// name's extension.
func attachmentType(a *richtext.EmailAttachment) string {
	if i := strings.LastIndexByte(a.Name, '.'); i >= 0 && i < len(a.Name)-1 {
		return strings.ToLower(a.Name[i+1:])
	}
	return ""
}

// emailAttachment is one of the email's files: a file pillow (the
// attachments' FileList, isCEAEmail).
func (l *lay) emailAttachment(f *richtext.File, x, y, w int) int {
	p := &l.st.Palette
	cw := min(w, l.dp(426))
	pad := l.dp(12)
	ic := l.dp(36)
	h := ic + 2*pad + l.dp(2)
	tg := l.d.addTarget(target{kind: tLink, url: f.URL})
	l.cardFrame(x, y, cw, h, tg)
	l.fileIcon(f, x+pad, y+pad, ic)
	tx := x + pad + ic + pad
	fs := l.bodyStyle()
	fs.face.Weight = font.Bold
	fs.size, fs.lineH = l.sp(16), l.dp(20)
	fs.maxLines, fs.ellipsis = 1, true
	l.plainFlow(fileName(f), tx, y+pad-l.dp(1), x+cw-tx-pad, &fs, tg)
	ms := l.small(p.Text2)
	ms.maxLines, ms.ellipsis, ms.noSelect = 1, true, true
	meta := strings.ToUpper(f.Type)
	if s := sizeText(f.Size); s != "" {
		if meta != "" {
			meta += " · "
		}
		meta += s
	}
	l.plainFlow(meta, tx, y+pad+l.dp(20), x+cw-tx-pad, &ms, -1)
	return h
}

// emailRcptKey is the expandables key of the card's recipient row label
// for file f (SenderRecipientRow's open state).
func emailRcptKey(f *richtext.File, label string) string {
	return "emailRecipients\x00" + f.ID + "\x00" + label
}

// emailRecipientRow is SenderRecipientRow's values (FileEmailV2): rs laid
// out as emailRecipients does, and past three wrapped rows cut at the
// third's bottom (maxHeight, overflow hidden) with "Show more" 4px under
// them, the content's link colour; open, every row and "Show less". It
// returns the height.
func (l *lay) emailRecipientRow(f *richtext.File, label string, rs []richtext.EmailAddress, x, y, w int) int {
	rowH, gap := l.dp(22), l.dp(4)
	m := l.mark()
	h := l.emailRecipients(rs, x, y, w)
	if rows := (h + gap) / (rowH + gap); rows <= 3 {
		return h
	}
	key := emailRcptKey(f, label)
	open := kitIsExpanded(key)
	if !open {
		limit := y + 3*rowH + 2*gap
		l.clipSince(m, limit)
		h = limit - y
	}
	tg := l.d.addTarget(target{kind: tExpand, id: key, underline: true})
	fs := l.bodyStyle()
	fs.size, fs.lineH, fs.col = l.sp(15), l.dp(22), l.st.Palette.Link
	fs.maxLines, fs.noSelect = 1, true
	text := i18n.T("files", "Show more")
	if open {
		text = i18n.T("files", "Show less")
	}
	ly := y + h + gap
	tw := int(l.measure(text, l.faceFor(fs.face, fs.size, 0)) + 0.999)
	l.plainFlow(text, x, ly, tw+l.dp(2), &fs, tg)
	l.d.hits = append(l.d.hits, hit{r: image.Rect(x, ly, x+tw, ly+rowH), t: tg})
	return h + gap + rowH
}

// emailRecipients lays out rs from (x, y) in w (wrapping 4px apart, 22px
// rows) and returns their height.
func (l *lay) emailRecipients(rs []richtext.EmailAddress, x, y, w int) int {
	p := &l.st.Palette
	cx, cy := x, y
	rowH := l.dp(22)
	gap := l.dp(4)
	bs := l.bodyStyle()
	bs.size, bs.lineH, bs.col = l.sp(15), l.dp(22), p.Text
	bs.maxLines, bs.ellipsis = 1, true
	k := l.faceFor(bs.face, bs.size, 0)
	place := func(wd int) (int, int) {
		if cx > x && cx+wd > x+w {
			cx, cy = x, cy+rowH+gap
		}
		px, py := cx, cy
		cx += min(wd, w) + gap
		return px, py
	}
	for _, r := range rs {
		switch {
		case r.UserID != "": // EmailUserEntity: the member's token
			name := firstNonEmptyStr(r.Name, r.Address)
			tg := l.d.addTarget(target{kind: tUser, id: r.UserID})
			tw := int(l.measure(name, k)+0.999) + 2*l.dp(4)
			px, py := place(tw)
			cr := image.Rect(px, py, px+min(tw, w), py+rowH)
			l.d.boxes = append(l.d.boxes, box{kind: bFill, r: cr, radius: l.dp(4), fill: Over(p.MentionBg, p.Panel), target: tg, hover: Over(p.MentionHoverBg, p.Panel), hoverOver: true})
			l.d.hits = append(l.d.hits, hit{r: cr, t: tg})
			l.plainFlow(name, px+l.dp(4), py, cr.Dx()-2*l.dp(4), &bs, tg)
		case r.Address != "": // the name, then the address chip (a mailto link)
			if r.Name != "" {
				nw := int(l.measure(r.Name, k) + 0.999)
				px, py := place(nw)
				l.plainFlow(r.Name, px, py, min(nw+l.dp(1), w), &bs, -1)
			}
			tg := l.d.addTarget(target{kind: tLink, url: "mailto:" + r.Address})
			g := l.dp(16)
			tw := l.dp(4) + g + l.dp(4) + int(l.measure(r.Address, k)+0.999) + l.dp(4)
			px, py := place(tw)
			cr := image.Rect(px, py, px+min(tw, w), py+rowH)
			l.d.boxes = append(l.d.boxes, box{kind: bFill, r: cr, radius: l.dp(4), fill: Over(p.MentionBg, p.Panel), target: tg})
			l.d.hits = append(l.d.hits, hit{r: cr, t: tg})
			gr := image.Rect(px+l.dp(4), py+(rowH-g)/2, px+l.dp(4)+g, py+(rowH-g)/2+g)
			l.d.overlayIcons = append(l.d.overlayIcons, overlayIcon{r: gr, name: "email", col: p.Text})
			as := bs
			l.plainFlow(r.Address, gr.Max.X+l.dp(4), py, max(cr.Max.X-l.dp(4)-gr.Max.X-l.dp(4), 1), &as, tg)
		default: // a channel's name (resolved in richtext), or the name, or "Email"
			name := firstNonEmptyStr(r.Name, i18n.T("files", "Email"))
			nw := int(l.measure(name, k) + 0.999)
			px, py := place(nw)
			l.plainFlow(name, px, py, min(nw+l.dp(1), w), &bs, -1)
		}
		l.plainAdd(" ")
	}
	return cy + rowH - y
}

// clipSince drops what was laid out since m under limit: the lines past
// it and everything that starts under it, and cuts the boxes and hits
// that cross it (the iframe's box).
func (l *lay) clipSince(m layMark, limit int) {
	d := l.d
	for i := m.lines; i < len(d.lines); i++ {
		if d.lines[i].bot > limit {
			f0 := int(d.lines[i].f0)
			if f0 < len(d.frags) && f0 >= m.frags {
				d.frags = d.frags[:f0]
			}
			d.lines = d.lines[:i]
			break
		}
	}
	keepR := func(r image.Rectangle) (image.Rectangle, bool) {
		if r.Min.Y >= limit {
			return r, false
		}
		if r.Max.Y > limit {
			r.Max.Y = limit
		}
		return r, true
	}
	n := m.boxes
	for i := m.boxes; i < len(d.boxes); i++ {
		if r, ok := keepR(d.boxes[i].r); ok {
			d.boxes[i].r = r
			d.boxes[n] = d.boxes[i]
			n++
		}
	}
	d.boxes = d.boxes[:n]
	n = m.hits
	for i := m.hits; i < len(d.hits); i++ {
		if r, ok := keepR(d.hits[i].r); ok {
			d.hits[i].r = r
			d.hits[n] = d.hits[i]
			n++
		}
	}
	d.hits = d.hits[:n]
	n = m.imgs
	for i := m.imgs; i < len(d.imgs); i++ {
		if d.imgs[i].r.Max.Y <= limit {
			d.imgs[n] = d.imgs[i]
			n++
		}
	}
	d.imgs = d.imgs[:n]
	n = m.icons
	for i := m.icons; i < len(d.overlayIcons); i++ {
		if d.overlayIcons[i].r.Max.Y <= limit {
			d.overlayIcons[n] = d.overlayIcons[i]
			n++
		}
	}
	d.overlayIcons = d.overlayIcons[:n]
}
