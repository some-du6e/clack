package richview

// Work object cards: Slack's MessageAttachmentWorkObject (boot-shared
// AwtQ) drawing ExternalWorkObjectEntity (K3AG) in an EntityContainer
// (E1Jc), from the client CDN build 4d0c78085232 (2026-11-10). Class
// names are the CSS modules HGu4 (the card), E1Jc (the container) and
// jKQ2 (the footer); every metric below is from client-boot-styles.
//
//	EntityContainer: radius 12, 1px otl-ter border (otl-sec and a
//	  0 1px 2px shadow on hover), base-pry, the width of its container
//	  between 200px and 480px (customMinWidth / customMaxWidth; search
//	  and canvases have no cap)
//	EntityHeader (BP+t): 12px padding, 12px gap: the 36px icon (the
//	  layout's product_icon at radius 4 when no app icon has the same
//	  image, else the app's icon at radius 8, with the app's 18px icon
//	  badged at bottom -2 right -4 in a 2px base-pry ring), then
//	  StackedLabels (0j4o): the title (body bold, 15px/1.5, one line),
//	  4px, the subtitle (13px/1.25, 600, content-sec, one line); with
//	  the card hovered the subtitle reads "Preview in Slack" ("View
//	  latest details" for a file without a preview, "Open in
//	  {productName}" with the new-window glyph when a click opens the
//	  item). Without work_objects_refresh_timestamp the header ends with
//	  the message's time (13px content-sec).
//	the preview (expanded, with fields): padding 0 12px 16px 60px, a
//	  wrapping row of fields 12px/24px apart, or a grid of columns no
//	  narrower than 62/83/119/191px when the fields don't fit on one
//	  line; each field a column: label (13px/1.38463 content-ter), 4px,
//	  value (15px/1.46668, one line, two when long, four alone)
//	EntityFooter (jKQ2, expanded): a 1px otl-ter top border, 8px 8px 8px
//	  12px, min-height 36: "As of today at 9:41 AM" (the message's ts)
//	  or "Updated 5 min ago" (the layout's updated_at) in 13px
//	  content-ter, "Refresh" after it while the card is hovered, and the
//	  layout's primary actions as small outline buttons 4px apart
//	the actions bar (hover, top 8px right 8px): ActionsBar (P/NP: 4px
//	  padding, radius 12, 1px otl-ter border, base shadow) of the quick
//	  action ("Open", new-window, and a divider; just the glyph under
//	  400px), Share…, Comments, View conversations and More actions as
//	  32px icon buttons (L5VL: 18px glyphs in content-ter, base-sec and
//	  content-pry on hover)
//
// The cards sit where MessageKitAttachments (jWdN) and MessageFiles
// (vzTO) put them: after the other attachments under
// MessageAttachmentWorkObjectsMetaTextWrapper's line (80Gy), or, with
// work_objects_rearrangement on, in the files' gallery (FileGallery
// T4f+: compact cards two to a row, a lone card expanded) under
// FilesMetaTextWrapper's line ("N attachments").

import (
	"encoding/json"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/workobject"
)

// WorkObjectFlags are the work object experiments and prefs the cards
// read (Style.SetWorkObjects): every View re-lays out when they change.
type WorkObjectFlags struct {
	Rearrange        bool // work_objects_rearrangement (cAEk U)
	RefreshTimestamp bool // work_objects_refresh_timestamp (hZia Ym)
	CommentSync      bool // wo_comment_sync_client_xp (hZia Y)
	EmbeddedPreview  bool // work_objects_embedded_preview (hZia ll)
	// CanShare: the host has the share dialog (ShareWorkObjectModal);
	// without it the actions bar leaves Share… out.
	CanShare   bool
	Locale     string
	Time24     bool   // the time24 pref (the footer's times)
	ClickPrefs string // work_objects_click_opens_url_prefs as stored (JSON)
	// Gen is the host's work object state generation (WorkObjectSource):
	// a refresh starting or ending, comments seen.
	Gen uint64
}

// WorkObjectState is a card's live state, from the host.
type WorkObjectState struct {
	Refreshing    bool // chat.refreshUnfurl sent, the new unfurl not in yet
	RefreshFailed bool // it failed or timed out (shown for 2s)
	NewerComments bool // comments.latest_timestamp is newer than the viewer last saw
}

// WorkObjectSource is the host's work object state (Style.WorkObjects).
type WorkObjectSource interface {
	WorkObjectState(channel, ts string, e *workobject.Entity) WorkObjectState
}

// WorkObjectAction is what was clicked on a work object card.
type WorkObjectAction uint8

const (
	WOClick         WorkObjectAction = iota // the card itself
	WOOpen                                  // the actions bar's Open (the item in the browser)
	WOPreview                               // Preview / Details: the flexpane
	WOShare                                 // Share…
	WOComments                              // the comments button: the flexpane's Comments tab
	WOConversations                         // View conversations: the flexpane's Conversations tab
	WOMore                                  // More actions: the card's menu
	WOSidePanel                             // Open in side panel (work_objects_embedded_preview)
	WORefresh                               // the footer's Refresh
	WOButton                                // a layout primary action (Button)
	WORemove                                // Remove preview (the viewer's own message)
)

// WorkObjectEvent is a click on a work object card.
type WorkObjectEvent struct {
	Att    *richtext.Attachment
	Action WorkObjectAction
	Button *workobject.Button // WOButton
	// Anchor is where to open a menu (window coordinates: the button's
	// bottom-left, plus View.Origin).
	Anchor image.Point
	// Channel and TS are the message's (Options.Channel / TS).
	Channel, TS string
}

// WorkObjectHandler is a Handler that acts on work object cards.
type WorkObjectHandler interface {
	WorkObject(ev WorkObjectEvent)
}

// SetWorkObjects sets the cards' experiments and state generation; a
// change re-lays every View out.
func (s *Style) SetWorkObjects(f WorkObjectFlags) {
	if s.woFlags != f {
		s.woFlags = f
		s.gen = styleGens.Add(1)
	}
}

// WorkObjectFlags returns the flags SetWorkObjects set.
func (s *Style) WorkObjectFlags() WorkObjectFlags { return s.woFlags }

// isWorkObjectBlock is an attachment drawn as a work object card.
func isWorkObjectBlock(b *richtext.Block) bool {
	return b.Kind == richtext.AttachmentBlock && b.Att != nil && b.Att.WorkObject != nil && b.Att.WorkObject.E != nil
}

// ---- tokens ----

// woTokCache holds Slack's tokens per mode, composited over base-pry
// (Gio blends translucent fills in linear light; Over matches CSS).
var woTokCache [2]map[string]color.NRGBA

// tok is Slack's --dt_color-<name> in the palette's mode (light or dark
// by the panel's luminance), over the panel.
func (l *lay) tok(name string) color.NRGBA {
	dark := lum(l.st.Palette.Panel) < 0.5
	i := 0
	if dark {
		i = 1
	}
	m := woTokCache[i]
	if m == nil {
		m = map[string]color.NRGBA{}
		woTokCache[i] = m
	}
	if c, ok := m[name]; ok {
		return c
	}
	s := css.SlackVars(dark)["--dt_color-"+name]
	if strings.Count(s, ",") == 2 && !strings.Contains(s, "(") { // a palette triplet: rgb(var(--…))
		s = "rgb(" + s + ")"
	}
	c, ok := css.ParseColor(s)
	if !ok {
		c = l.st.Palette.Text
	}
	c = Over(c, l.st.Palette.Panel)
	m[name] = c
	return c
}

// ---- grouping (MessageKitAttachments, MessageFiles) ----

// arrangeWorkObjects puts a message's work object cards where Slack
// draws them: with work_objects_rearrangement, MessageFiles (the files'
// place: right after the files, or before the attachments without any);
// without it, MessageKitAttachments' group after the other attachments.
// bs comes back unchanged when it has no card.
func (l *lay) arrangeWorkObjects(bs []richtext.Block) []richtext.Block {
	n := 0
	for i := range bs {
		if isWorkObjectBlock(&bs[i]) {
			n++
		}
	}
	if n == 0 {
		return bs
	}
	rest := make([]richtext.Block, 0, len(bs)-n)
	wos := make([]richtext.Block, 0, n)
	for i := range bs {
		if isWorkObjectBlock(&bs[i]) {
			wos = append(wos, bs[i])
		} else {
			rest = append(rest, bs[i])
		}
	}
	at := len(rest)
	if l.st.woFlags.Rearrange {
		lastFile, firstAtt := -1, -1
		for i := range rest {
			switch rest[i].Kind {
			case richtext.FileBlock:
				if lastFile < 0 || lastFile == i-1 {
					lastFile = i
				}
			case richtext.AttachmentBlock:
				if firstAtt < 0 {
					firstAtt = i
				}
			}
		}
		switch {
		case lastFile >= 0:
			at = lastFile + 1
		case firstAtt >= 0:
			at = firstAtt
		}
	} else {
		lastAtt := -1
		for i := range rest {
			if rest[i].Kind == richtext.AttachmentBlock {
				lastAtt = i
			}
		}
		if lastAtt >= 0 {
			at = lastAtt + 1
		}
	}
	out := make([]richtext.Block, 0, len(bs))
	out = append(out, rest[:at]...)
	out = append(out, wos...)
	return append(out, rest[at:]...)
}

// woKey is the cards' expandable key (MessageAttachmentWorkObjectsMeta
// TextWrapper's own open state; Slack keeps it per mount, Clack per
// message for the session).
func (l *lay) woKey(atts []*richtext.Block) string {
	ids := make([]string, len(atts))
	for i, b := range atts {
		ids[i] = b.Att.WorkObject.E.EntityID
	}
	return "wo/" + l.opts.Channel + "/" + l.opts.TS + "/" + strings.Join(ids, "_")
}

// woMetaLabel is the meta text: "N attachments" for several cards, else
// the entity's header_title (or app name), with " | {title}" while
// collapsed (80Gy A). Rearranged, MessageFiles' renderMetaText says the
// header title alone for one card and "N attachments" otherwise
// (getWorkObjectMetaLabel); files count with the cards there.
func woMetaLabel(e *workobject.Entity, n, files int, expanded, rearranged bool) string {
	ns := i18n.NS("work-objects")
	if rearranged {
		total := n + files
		lay := e.UnfurlLayout()
		head := ""
		if lay != nil {
			head = lay.HeaderTitle
		}
		if head == "" {
			head = e.AppName
		}
		if files == 0 && n == 1 && head != "" {
			return head
		}
		return i18n.T("files", "{fileCount, plural, =1 {attachment} other {# attachments}}", "fileCount", total)
	}
	if n > 1 {
		return ns.T("{attachmentCount} attachments", "attachmentCount", n)
	}
	lay := e.UnfurlLayout()
	if lay == nil {
		return ""
	}
	s := lay.HeaderTitle
	if s == "" {
		s = e.AppName
	}
	if s == "" {
		return ""
	}
	if !expanded && lay.Title != "" {
		return s + " | " + lay.Title
	}
	return s
}

// woMeta lays out the meta line (13px/1.38463, foreground_max .7, the
// 16px collapse caret 4px after the text, 4px under the line) and
// returns its height and whether the cards are shown.
func (l *lay) woMeta(label, key string, x, y, w int) (int, bool) {
	p := &l.st.Palette
	expanded := !kitIsExpanded(key) // toggled closed this session
	lineH := l.dp(18)
	fs := flowStyle{face: font.Font{Typeface: l.st.Face}, size: l.sp(13), lineH: lineH, col: p.Text2,
		emoji: l.dp(16), maxLines: 1, ellipsis: true}
	_, tw := l.plainFlow(label, x, y, max(w-l.dp(24), l.dp(40)), &fs, -1)
	l.plainAdd("\n")
	tg := l.d.addTarget(target{kind: tExpand, id: key})
	bx := x + tw + l.dp(4)
	br := image.Rect(bx, y+(lineH-l.dp(16))/2, bx+l.dp(16), y+(lineH-l.dp(16))/2+l.dp(16))
	icon := "caret-down-filled"
	if !expanded {
		icon = "caret-right-filled"
	}
	l.d.imgs = append(l.d.imgs, imgPrim{r: br, url: iconURL + icon, target: -1, px: br.Dx(), tint: p.Text})
	l.d.hits = append(l.d.hits, hit{r: br, t: tg})
	return lineH + l.dp(4), expanded
}

// workObjectGroup lays out consecutive work object cards (bs[i:j]) and
// returns their height. files is how many files the message shows above
// (rearranged: they share the meta line).
func (l *lay) workObjectGroup(bs []*richtext.Block, x, y, w, files int) int {
	if len(bs) == 0 {
		return 0
	}
	y0 := y
	fl := l.st.woFlags
	key := l.woKey(bs)
	if !l.opts.HoverCard && !l.inAtt && !(fl.Rearrange && files > 0) {
		mh, shown := l.woMeta(woMetaLabel(bs[0].Att.WorkObject.E, len(bs), files, !kitIsExpanded(key), fl.Rearrange), key, x, y, w)
		y += mh
		if !shown {
			return y - y0 - l.dp(4)
		}
	}
	if !fl.Rearrange {
		// MessageKitAttachments: one card under another, 8px apart
		// (c-message_attachment_v2: margin 0 0 8px)
		for i, b := range bs {
			if i > 0 {
				y += l.dp(8)
			}
			y += l.woCard(b, x, y, w, "", true)
		}
		return y - y0
	}
	// FileGallery's mixed_file_format grid: --v3 (two columns, 8px gap,
	// at most 716 wide, cells at most 354) from 408px and two items;
	// --v3-single-column otherwise. A lone card with an expanded layout
	// draws expanded, the rest compact (renderWorkObjects).
	if files > 0 {
		y += l.dp(8) // c-file_gallery__mixed_file_format--non_visual: margin-top 8px
	}
	gw := min(w, l.dp(716))
	total := len(bs) + files
	two := w >= l.dp(408) && len(bs) > 1
	if !two {
		for i, b := range bs {
			if i > 0 {
				y += l.dp(8)
			}
			size := workobject.Compact
			if total == 1 && b.Att.WorkObject.E.Expanded() != nil {
				size = workobject.Expanded
			}
			y += l.woCard(b, x, y, gw, size, true)
		}
		return y - y0
	}
	cell := min((gw-l.dp(8))/2, l.dp(354))
	for i := 0; i < len(bs); i += 2 {
		if i > 0 {
			y += l.dp(8)
		}
		h := l.woCard(bs[i], x, y, cell, workobject.Compact, true)
		if i+1 < len(bs) {
			h = max(h, l.woCard(bs[i+1], x+cell+l.dp(8), y, cell, workobject.Compact, true))
		}
		y += h
	}
	return y - y0
}

// ---- the card ----

// WorkObjectClickOpensURL reports whether a click on e's card opens the
// item (robR B) rather than its flexpane; prefs is the user's
// work_objects_click_opens_url_prefs.
func WorkObjectClickOpensURL(prefs string, e *workobject.Entity) bool {
	return e != nil && woClickOpensURL(prefs, e.AppID, e.ProductName, e)
}

// woClickOpensURL is robR B for an app: the user's
// work_objects_click_opens_url_prefs entry, else true for Google Drive,
// Microsoft and Box apps without a preview, false for the rest.
func woClickOpensURL(prefs, appID, product string, e *workobject.Entity) bool {
	if appID == "" {
		return false
	}
	key := appID
	if isGoogleDrive(appID) || isMicrosoft(appID) {
		if product != "" {
			key = appID + "__" + strings.Replace(strings.ToLower(product), " ", "_", 1)
		}
	}
	if prefs != "" {
		var m map[string]bool
		if json.Unmarshal([]byte(prefs), &m) == nil {
			if v, ok := m[key]; ok {
				if !v && isBoxApp(appID) {
					return boxNoPreview(e)
				}
				return v
			}
		}
	}
	return isMicrosoft(appID) || isGoogleDrive(appID) || boxNoPreview(e)
}

// App ids from oyZ+ (production): Google Drive, Microsoft, and the Box
// apps (ihYS rB).
func isGoogleDrive(id string) bool { return id == "A6NL8MJ6Q" || id == "A0BDLPR2L" }
func isMicrosoft(id string) bool   { return id == "AJBCW09GU" || id == "A012XDE27GB" }
func isBoxApp(id string) bool {
	switch id {
	case "ALQ9XG76D", "A1S4JKY37", "A1S36PTCD", "A042QCUUP7H", "A092NHP0N94":
		return true
	}
	return false
}

// boxNoPreview is plEK Z for a Box item: its full_size_preview says it
// isn't supported.
func boxNoPreview(e *workobject.Entity) bool {
	if e == nil || !isBoxApp(e.AppID) {
		return false
	}
	if p := e.FullSizePreview; p != nil {
		return !p.IsSupported
	}
	return true
}

// woAppIcon is the app's icon at about px: the entity's app_icons (the
// app's own images), the largest one up to 2x the size.
func woAppIcon(e *workobject.Entity) string {
	best, bw := "", 0
	for k, im := range e.AppIcons {
		if im == nil || im.ImageURL == "" {
			continue
		}
		n, _ := strconv.Atoi(strings.TrimPrefix(k, "image_"))
		if best == "" || n > bw {
			best, bw = im.ImageURL, n
		}
	}
	return best
}

// woUniqueProductIcon is BP+t isUniqueProductIcon: a product icon no app
// icon shares its image with.
func woUniqueProductIcon(icon *workobject.Image, e *workobject.Entity) bool {
	if icon == nil || icon.ImageURL == "" {
		return false
	}
	for _, im := range e.AppIcons {
		if im != nil && im.ImageURL == icon.ImageURL {
			return false
		}
	}
	return true
}

// hoverFrom starts a hover layer: primitives added until hoverEnd draw
// only while the card's targets are hovered (outside skip..skipTo).
func (l *lay) hoverFrom() (nb, ni, nf int) { return len(l.d.boxes), len(l.d.imgs), len(l.d.frags) }

// hoverEnd tags the primitives since nb/ni/nf as hover layer k (1-based).
func (l *lay) hoverEnd(k int, nb, ni, nf int) {
	tag := int16(k)
	for i := nb; i < len(l.d.boxes); i++ {
		l.d.boxes[i].hov = tag
	}
	for i := ni; i < len(l.d.imgs); i++ {
		l.d.imgs[i].hov = tag
	}
	for i := nf; i < len(l.d.frags); i++ {
		l.d.frags[i].hov = tag
	}
}

// newHover adds a hover layer for card targets card..cardTo (filled in
// by the caller once the card's last target exists).
func (l *lay) newHover() int {
	l.d.hovers = append(l.d.hovers, hoverLayer{card: -1, cardTo: -1, skip: -1, skipTo: -2})
	return len(l.d.hovers)
}

// woCard lays out one work object card at (x, y) in a container w wide
// and returns its height. size is the size prop (Expanded / Compact; ""
// is the layout's own). capped is customMaxWidth 480 (messages).
func (l *lay) woCard(b *richtext.Block, x, y, w int, size string, capped bool) int {
	att := b.Att
	wo := att.WorkObject
	e := wo.E
	lay := e.UnfurlLayout()
	if lay == nil {
		return 0
	}
	p := &l.st.Palette
	fl := l.st.woFlags
	if size == "" {
		size = lay.Key
		if lay.Key == workobject.Minimal {
			size = workobject.Compact
		}
	}
	cw := w
	if capped {
		cw = min(cw, l.dp(480))
	}
	cw = max(cw, l.dp(200))
	var st WorkObjectState
	if src := l.st.WorkObjects; src != nil {
		st = src.WorkObjectState(l.opts.Channel, l.opts.TS, e)
	}
	interactive := !l.opts.HoverCard
	product := e.ProductName
	opensURL := woClickOpensURL(fl.ClickPrefs, e.AppID, product, e)
	fileNoPreview := e.EntityType == workobject.EntityFile && !e.IsFullSizePreviewEnabled()

	card := l.d.addTarget(target{kind: tWorkObject, id: "click", att: att})
	frame := len(l.d.boxes)
	l.d.boxes = append(l.d.boxes, box{kind: bBorder, fill: p.Panel, stroke: l.tok("otl-ter"), bw: max(1, l.dp(1)),
		radius: l.dp(12), target: -1})
	cardHit := len(l.d.hits)
	l.d.hits = append(l.d.hits, hit{t: card})

	in := max(1, l.dp(1))
	ix, iy, iw := x+in, y+in, cw-2*in
	cy := iy

	// ---- EntityHeader ----
	pad := l.dp(12)
	ic := l.dp(36)
	icR := image.Rect(ix+pad, cy+pad, ix+pad+ic, cy+pad+ic)
	pi := lay.ProductIcon
	uniq := woUniqueProductIcon(pi, e)
	switch {
	case pi != nil && uniq:
		l.d.imgs = append(l.d.imgs, imgPrim{r: icR, url: pi.ImageURL, radii: uni4(l.dp(4)), target: -1, px: ic})
	case woAppIcon(e) != "":
		l.d.imgs = append(l.d.imgs, imgPrim{r: icR, url: woAppIcon(e), radii: uni4(l.dp(8)), target: -1, px: ic})
	default: // the app isn't known yet: its skeleton (bpOw over YHf7)
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: icR, radius: l.dp(8), fill: l.tok("base-ter"), target: -1})
	}
	if uniq && e.DisplayType != workobject.ReviewAIContent && !isMicrosoft(e.AppID) && !isGoogleDrive(e.AppID) &&
		!e.IsDraft() && e.EntityType != workobject.EntityCalendarEvent {
		if u := woAppIcon(e); u != "" {
			bs := l.dp(18)
			ring := l.dp(2)
			br := image.Rect(icR.Max.X+l.dp(4)-bs, icR.Max.Y+l.dp(2)-bs, icR.Max.X+l.dp(4), icR.Max.Y+l.dp(2))
			l.d.boxes = append(l.d.boxes, box{kind: bFill, r: br.Inset(-ring), radius: l.dp(4) + ring, fill: p.Panel, target: -1, layer: 2})
			l.d.imgs = append(l.d.imgs, imgPrim{r: br, url: u, radii: uni4(l.dp(4)), target: -1, px: bs})
		}
	}
	lx := icR.Max.X + pad
	lw := ix + iw - pad - lx
	// headerTrailing: the time label when the refresh timestamp is off,
	// or the Draft tag
	trail := ""
	if !fl.RefreshTimestamp {
		if t, ok := workobject.TSTime(l.opts.TS); ok {
			trail = workobject.HeaderTime(woLocale(fl), t, time.Now(), i18n.Default24h(woLocale(fl)))
		}
	}
	if trail != "" {
		ts := flowStyle{face: font.Font{Typeface: l.st.Face}, size: l.sp(13), lineH: l.dp(18), col: l.tok("content-sec"), noSelect: true}
		tw := l.textWidth(trail, &ts)
		l.plainFlow(trail, ix+iw-pad-tw, cy+pad+(l.dp(43)-l.dp(18))/2, tw+l.dp(2), &ts, -1)
		lw -= tw + l.dp(12)
	}
	lw = max(lw, l.dp(20))
	title := lay.Title
	ts := l.bodyStyle()
	ts.face.Weight = font.Bold
	ts.size, ts.lineH, ts.col = l.sp(15), l.dpHalf(22.5), l.tok("content-pry")
	ts.maxLines, ts.ellipsis, ts.emoji = 1, true, l.dp(18)
	th, _ := l.plainFlow(title, lx, cy+pad, lw, &ts, -1)
	if th == 0 {
		th = ts.lineH
	}
	l.plainAdd("\n")
	sy := cy + pad + th + l.dp(4)
	ss := flowStyle{face: font.Font{Typeface: l.st.Face, Weight: font.Bold}, size: l.sp(13), lineH: l.dpHalf(16.25),
		col: l.tok("content-sec"), emoji: l.dp(16), maxLines: 1, ellipsis: true}
	if lay.Subtitle != "" {
		l.plainFlow(lay.Subtitle, lx, sy, lw, &ss, -1)
		l.plainAdd("\n")
	}
	subH := max(ss.lineH, l.dp(16))
	headH := max(ic, th+l.dp(4)+subH) + 2*pad
	// the hovered subtitle: its container covers the default (base-pry)
	var subLayer int
	if interactive && !e.IsDraft() {
		subLayer = l.newHover()
		nb, ni, nf := l.hoverFrom()
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(lx, sy, lx+lw, sy+subH), fill: p.Panel, target: -1})
		hs := ss
		hs.noSelect = true
		label := i18n.T("work-objects", "Preview in Slack")
		if fileNoPreview {
			label = i18n.T("work-objects", "View latest details")
		}
		if opensURL {
			label = i18n.T("work-objects", "Open in {productName}", "productName", firstNonEmpty(product, e.AppName))
		}
		_, hw := l.plainFlow(label, lx, sy, lw, &hs, -1)
		if opensURL {
			g := l.dp(13)
			gx := lx + hw + l.dp(4)
			l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(gx, sy+l.dp(1)+(subH-g)/2, gx+g, sy+l.dp(1)+(subH-g)/2+g),
				url: iconURL + "new-window", target: -1, px: g, tint: hs.col})
		}
		l.hoverEnd(subLayer, nb, ni, nf)
	}
	cy += headH

	// ---- the preview: the expanded layout's fields ----
	showFields := size == workobject.Expanded && lay.HasFields()
	if showFields {
		fields := woVisibleFields(lay)
		thin := woHasImagePreview(lay)
		if len(fields) > 0 {
			if iw <= l.dp(240) && !thin { // @container (width<=240px): the header's bottom border
				l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(ix, cy-in, ix+iw, cy), fill: l.tok("otl-ter"), target: -1})
				cy += l.dp(12)
			}
			left := l.dp(60)
			right, bottom := l.dp(12), l.dp(16)
			if thin {
				left, bottom = l.dp(12), l.dp(12)
			} else if iw <= l.dp(240) {
				left = l.dp(12)
			}
			fh := l.woFields(wo, lay, fields, ix+left, cy, iw-left-right, card)
			cy += fh + bottom
		}
	}
	// contentContainer: at most 360px (widthCappedCard: max(360px,
	// 56.25cqw + 112px)), the rest hidden
	maxH := l.dp(360)
	if capped && cw >= l.dp(480) {
		maxH = max(maxH, int(0.5625*float64(iw))+l.dp(112))
	}
	if cy-iy > maxH {
		cy = iy + maxH
	}

	// ---- EntityFooter ----
	actions := woPrimaryActions(e, lay)
	if interactive && (lay.Key == workobject.Expanded || lay.Key == workobject.Compact) && size == workobject.Expanded &&
		(len(actions) > 0 || fl.RefreshTimestamp) {
		cy += l.woFooter(att, lay, actions, st, ix, cy, iw, card)
	}

	h := cy - iy + 2*in
	r := image.Rect(x, y, x+cw, y+h)
	l.d.boxes[frame].r = r
	l.d.hits[cardHit].r = r

	// ---- the actions bar ----
	if interactive && !e.IsDraft() {
		bar := l.newHover()
		nb, ni, nf := l.hoverFrom()
		l.woActionsBar(att, opensURL, fileNoPreview, cw, r, card)
		l.hoverEnd(bar, nb, ni, nf)
		l.d.hovers[bar-1].bar = true
		l.d.hovers[bar-1].card = card
		l.d.hovers[bar-1].cardTo = int16(len(l.d.targets) - 1)
		if subLayer > 0 {
			hv := &l.d.hovers[subLayer-1]
			hv.card, hv.cardTo = card, int16(len(l.d.targets)-1)
			hv.skip, hv.skipTo = l.d.hovers[bar-1].skip, l.d.hovers[bar-1].skipTo
			// withHover: the otl-sec border while the card (not its bar) is hovered
			l.d.boxes = append(l.d.boxes, box{kind: bBorder, r: r, radius: l.dp(12), stroke: l.tok("otl-sec"), bw: max(1, l.dp(1)),
				target: -1, hov: int16(subLayer)})
		}
		for i := range l.d.hovers {
			if hv := &l.d.hovers[i]; hv.card == -2 { // the footer's Refresh (woFooter)
				hv.card, hv.cardTo = card, int16(len(l.d.targets)-1)
				hv.skip, hv.skipTo = l.d.hovers[bar-1].skip, l.d.hovers[bar-1].skipTo
			}
		}
	}
	if l.opts.Own && att.ID != "" && att.FromURL != "" { // DeleteWorkObjectAttachmentButton
		l.woRemove(att, card, r)
	}
	return h
}

// dpHalf is a fractional dp line height, rounded like a browser's line box.
func (l *lay) dpHalf(v float32) int { return int(math.Round(float64(v * l.scale))) }

func uni4(v int) [4]int { return [4]int{v, v, v, v} }

func woLocale(f WorkObjectFlags) string {
	if f.Locale != "" {
		return f.Locale
	}
	return i18n.Locale()
}

// woVisibleFields is HSqQ P$: the layout's fields without image fields
// that have no size (Slack also keeps those whose Slack file it can
// preview; Clack has none of those files' thumbs here).
func woVisibleFields(lay *workobject.Layout) []*workobject.Field {
	var out []*workobject.Field
	for i := range lay.Fields {
		f := &lay.Fields[i]
		if f.IsImage() && !(len(f.Images) > 0 && f.Images[0].Width > 0 && f.Images[0].Height > 0) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// woHasImagePreview is HSqQ IQ: a field with an image Slack can size.
func woHasImagePreview(lay *workobject.Layout) bool {
	for i := range lay.Fields {
		if f := &lay.Fields[i]; f.IsImage() && len(f.Images) > 0 && f.Images[0].Width > 0 && f.Images[0].Height > 0 {
			return true
		}
	}
	return false
}

// woPrimaryActions is aS: the layout's primary actions (drafts get
// Slack's Edit and Save buttons, which Clack's drafts don't reach).
func woPrimaryActions(e *workobject.Entity, lay *workobject.Layout) []workobject.Button {
	if lay.Actions == nil || e.IsDraft() {
		return nil
	}
	var out []workobject.Button
	for _, b := range lay.Actions.Primary {
		if b.ActionID != "" && b.BlockID != "" {
			out = append(out, b)
		}
	}
	return out
}

// woFields lays out the expanded preview's fields (ExternalWorkObject
// FieldBlocks) at (x, y) in w and returns their height.
func (l *lay) woFields(wo *richtext.WorkObject, lay *workobject.Layout, fields []*workobject.Field, x, y, w int, card int16) int {
	gapX, gapY := l.dp(24), l.dp(12)
	n := len(fields)
	// natural widths, one line each (the hidden preview)
	nat := make([]int, n)
	sum, widest := 0, 0
	for i, f := range fields {
		nat[i] = l.woFieldNatural(wo, lay, f)
		sum += nat[i]
		if nat[i] <= w+gapX && nat[i] > widest {
			widest = nat[i]
		}
	}
	pads := l.dp(72) // 12 + 60
	if woHasImagePreview(lay) || w+pads <= l.dp(240) {
		pads = l.dp(24)
	}
	grid := sum+pads > w+pads
	if !grid {
		// a wrapping row: fields at their own width
		cx, rowH, cy := x, 0, y
		for i, f := range fields {
			fw := min(nat[i], w)
			if f.Long || f.IsImage() {
				fw = w
			}
			if cx > x && cx+fw > x+w {
				cy += rowH + gapY
				cx, rowH = x, 0
			}
			h := l.woField(wo, lay, f, cx, cy, fw, len(lay.Fields) == 1)
			rowH = max(rowH, h)
			cx += fw + gapX
		}
		return cy + rowH - y
	}
	minW := l.dp(83)
	switch {
	case widest <= l.dp(62):
		minW = l.dp(62)
	case widest > l.dp(119) && w+pads >= 2*l.dp(191)+l.dp(24)+pads:
		minW = l.dp(191)
	case widest > l.dp(83):
		minW = l.dp(119)
	}
	cols := max(1, (w+gapX)/(minW+gapX))
	colW := (w - gapX*(cols-1)) / cols
	cy, col, rowH := y, 0, 0
	for _, f := range fields {
		long := f.Long || (f.IsImage() && len(f.Images) > 0 && f.Images[0].Width >= w/2)
		if long && col > 0 {
			cy += rowH + gapY
			col, rowH = 0, 0
		}
		fw := colW
		if long {
			fw = w
		}
		h := l.woField(wo, lay, f, x+col*(colW+gapX), cy, fw, len(lay.Fields) == 1)
		rowH = max(rowH, h)
		col++
		if long || col >= cols {
			cy += rowH + gapY
			col, rowH = 0, 0
		}
	}
	if col == 0 && cy > y {
		cy -= gapY
	} else {
		cy += rowH
	}
	return cy - y
}

// woLabelStyle is contentItemLabel.expanded: 13px/1.38463 content-ter.
func (l *lay) woLabelStyle() flowStyle {
	return flowStyle{face: font.Font{Typeface: l.st.Face}, size: l.sp(13), lineH: l.dpHalf(18), col: l.tok("content-ter"),
		emoji: l.dp(16), maxLines: 1, ellipsis: true}
}

// woValueStyle is contentItemValue: 15px/1.46668 content-pry.
func (l *lay) woValueStyle() flowStyle {
	fs := l.bodyStyle()
	fs.size, fs.lineH, fs.col = l.sp(15), l.dp(22), l.tok("content-pry")
	return fs
}

// woFieldText is a field's value as one line of text (the natural width,
// and the value of the text-like fields).
func woFieldText(loc string, f *workobject.Field, now time.Time) string {
	switch f.Type {
	case workobject.FieldText:
		if f.Name == "remind_minutes_before" {
			return i18n.T("work-objects", "{reminder} minutes", "reminder", f.Text)
		}
		return f.Text
	case workobject.FieldTimestamp:
		if t, ok := workobject.ToTime(workobject.Number(f.Timestamp)); ok {
			return i18n.TimeAgo(loc, t, now, false, false)
		}
	case workobject.FieldDate:
		s := workobject.String(f.Date)
		if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
			return t.Format("Jan 2, 2006") // the Lists date cell's default MMM D, YYYY
		}
		if t, ok := workobject.ToTime(workobject.Number(f.Date)); ok {
			return t.Format("Jan 2, 2006")
		}
	case workobject.FieldEmail:
		return strings.Join(f.Emails, ", ")
	case workobject.FieldLink:
		return strings.Join(f.Links, ", ")
	case workobject.FieldHeaderWithBadge:
		if f.Header != nil {
			return f.Header.Text
		}
	}
	return ""
}

// woFieldNatural is a field's one-line width: the wider of its label and
// value.
func (l *lay) woFieldNatural(wo *richtext.WorkObject, lay *workobject.Layout, f *workobject.Field) int {
	ls := l.woLabelStyle()
	vs := l.woValueStyle()
	lw := l.textWidth(f.Label, &ls)
	vw := 0
	switch {
	case f.IsRichText() || f.IsUnknown():
		for i := range lay.Fields {
			if &lay.Fields[i] == f {
				vw = l.textWidth(richtext.PlainText(richtext.Doc{Blocks: wo.RichField(lay.Key, i)}), &vs)
			}
		}
	case f.Type == workobject.FieldTag:
		ts := l.woTagStyle()
		for i, t := range f.Tags {
			if i > 0 {
				vw += l.dp(2)
			}
			vw += l.textWidth(t.Text, &ts) + l.dp(8)
			if t.IconURL != "" {
				vw += l.dp(20)
			}
		}
	case f.IsUser():
		for i, u := range f.Users { // woUsers' tokens: the picture, 4px, the name, 4px
			if i > 0 {
				vw += l.dp(2)
			}
			s := l.dp(20)
			if u.UserType != "slack" {
				s = l.dp(22)
			}
			vw += s + l.dp(4) + l.textWidth(woUserName(wo, u), &vs) + l.dp(4)
		}
	case f.Type == workobject.FieldCheckbox:
		for _, c := range f.Checks {
			vw = max(vw, l.dp(24)+l.textWidth(c.Text, &vs))
		}
	case f.Type == workobject.FieldEntityRef:
		ts := l.woTagStyle()
		for _, r := range f.Refs {
			vw += l.textWidth(r.Title, &ts) + l.dp(8)
		}
	case f.Type == workobject.FieldChannel:
		for _, c := range f.Channels {
			vw += l.textWidth("#"+wo.Channel(c.ChannelID), &vs) + l.dp(4)
		}
	case f.IsImage():
		vw = l.dp(32)
		if len(f.Images) == 1 {
			vw = f.Images[0].Width
		}
	default:
		vw = l.textWidth(woFieldText(woLocale(l.st.woFlags), f, time.Now()), &vs)
	}
	return max(lw, vw)
}

// woTagStyle is tagField: 15px/1.46668 in a 24px pill.
func (l *lay) woTagStyle() flowStyle {
	fs := l.woValueStyle()
	fs.maxLines, fs.ellipsis = 1, true
	return fs
}

// woUserName is a user token's name: the member's (forceRealName), or an
// external user's text.
func woUserName(wo *richtext.WorkObject, u workobject.User) string {
	if u.UserType != "slack" {
		return u.Text
	}
	return wo.Member(u.UserID).Name
}

// woField lays out one field (label over value: contentItem expanded
// displayColumn, 4px apart) at (x, y) in w and returns its height.
func (l *lay) woField(wo *richtext.WorkObject, lay *workobject.Layout, f *workobject.Field, x, y, w int, alone bool) int {
	p := &l.st.Palette
	y0 := y
	if !f.IsImage() {
		ls := l.woLabelStyle()
		label := f.Label
		if f.Name == "attendees" && f.IsUser() {
			label += " (" + strconv.Itoa(len(f.Users)) + ")"
		}
		h, _ := l.plainFlow(label, x, y, w, &ls, -1)
		l.plainAdd("\n")
		y += max(h, ls.lineH) + l.dp(4)
	}
	vs := l.woValueStyle()
	vs.maxLines, vs.ellipsis = 1, true
	switch {
	case f.IsRichText() || f.IsUnknown():
		idx := -1
		for i := range lay.Fields {
			if &lay.Fields[i] == f {
				idx = i
			}
		}
		bs := wo.RichField(lay.Key, idx)
		// WorkObjectRichText in the expanded preview: four lines, 160
		// characters, no "Show more"
		text := richtext.PlainText(richtext.Doc{Blocks: bs})
		if r := []rune(text); len(r) > 160 {
			text = string(r[:160]) + "…"
		}
		vs.maxLines = 4
		h, _ := l.plainFlow(text, x, y, w, &vs, -1)
		y += h
	case f.Type == workobject.FieldTag:
		y += l.woTags(f.Tags, x, y, w)
	case f.Type == workobject.FieldEntityRef:
		tags := make([]workobject.Tag, len(f.Refs))
		for i, r := range f.Refs {
			tags[i] = workobject.Tag{Text: r.Title, Color: workobject.TagInformative}
		}
		y += l.woTags(tags, x, y, w)
	case f.IsUser():
		y += l.woUsers(wo, f.Users, x, y, w)
	case f.Type == workobject.FieldChannel:
		cx := x
		for _, c := range f.Channels {
			name := "#" + wo.Channel(c.ChannelID)
			tg := l.d.addTarget(target{kind: tChannel, id: c.ChannelID, cardless: true})
			fs := vs
			fs.col = p.Link
			_, cw := l.plainFlow(name, cx, y, max(x+w-cx, l.dp(20)), &fs, tg)
			cx += cw + l.dp(4)
		}
		y += vs.lineH
	case f.Type == workobject.FieldLink:
		cx := x
		for _, u := range f.Links {
			tg := l.d.addTarget(target{kind: tLink, url: richtext.SafeURL(u), underline: true, website: true})
			fs := vs
			fs.col = p.Link
			_, cw := l.plainFlow(u, cx, y, max(x+w-cx, l.dp(20)), &fs, tg)
			cx += cw + l.dp(4)
		}
		y += vs.lineH
	case f.Type == workobject.FieldCheckbox:
		for _, c := range f.Checks {
			y += l.woCheckbox(c, x, y, w)
		}
	case f.IsImage():
		y += l.woImage(f, x, y, w, alone)
	default:
		if f.Type == workobject.FieldText && f.Long {
			vs.maxLines = 2
			if alone {
				vs.maxLines = 4
			}
		}
		if f.Type == workobject.FieldTimestamp || f.Type == workobject.FieldDate {
			vs.col = l.tok("content-sec") // .c-timestamp__label
		}
		h, _ := l.plainFlow(woFieldText(woLocale(l.st.woFlags), f, time.Now()), x, y, w, &vs, -1)
		y += max(h, vs.lineH)
	}
	l.plainAdd("\n")
	return y - y0
}

// woTags lays out tag pills (WorkObjectTag: 24px, radius 4, the theme's
// plt-<color>-0 fill and -70 text, 4px text padding, a 20px icon) on one
// line that fades at the edge (unfurlTokenArray), 2px apart.
func (l *lay) woTags(tags []workobject.Tag, x, y, w int) int {
	h := l.dp(24)
	cx := x
	ts := l.woTagStyle()
	for i, t := range tags {
		if cx >= x+w {
			break
		}
		if i > 0 {
			cx += l.dp(2)
		}
		bg, fg := l.woTagColors(t.Color)
		tw := l.textWidth(t.Text, &ts) + l.dp(8)
		if t.IconURL != "" {
			tw += l.dp(20)
		}
		tw = min(tw, x+w-cx)
		r := image.Rect(cx, y, cx+tw, y+h)
		var tg int16 = -1
		if t.Link != "" {
			tg = l.d.addTarget(target{kind: tLink, url: richtext.SafeURL(t.Link), underline: true})
			l.d.hits = append(l.d.hits, hit{r: r, t: tg})
		}
		if bg.A > 0 {
			l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, radius: l.dp(4), fill: bg, target: -1})
		}
		tx := cx + l.dp(4)
		if t.IconURL != "" {
			l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(cx+l.dp(2), y+l.dp(2), cx+l.dp(22), y+l.dp(22)), url: t.IconURL, target: -1, px: l.dp(20)})
			tx += l.dp(20)
		}
		fs := ts
		fs.col = fg
		l.plainFlow(t.Text, tx, y+(h-fs.lineH)/2, max(cx+tw-tx-l.dp(4), l.dp(8)), &fs, tg)
		cx += tw
	}
	return h
}

// woTagColors is a tag theme's fill and text (WorkObjectTag's classes);
// no theme: plain text.
func (l *lay) woTagColors(theme string) (bg, fg color.NRGBA) {
	switch theme {
	case workobject.TagInformative:
		return l.tok("surf-hgl-1"), l.tok("content-hgl-1")
	case workobject.TagGray, workobject.TagHorchata, workobject.TagHoneycomb, workobject.TagFlamingo, workobject.TagAubergine,
		workobject.TagIndigo, workobject.TagLagoon, workobject.TagJade, workobject.TagGrass:
		return l.tok("plt-" + theme + "-0"), l.tok("plt-" + theme + "-70")
	}
	return color.NRGBA{}, l.tok("content-pry")
}

// woUsers lays out user tokens (c-token small: a 20px avatar and the
// name; an external user's 22px picture in a surf-hgl-1 13px bold chip),
// 2px apart on one line.
func (l *lay) woUsers(wo *richtext.WorkObject, us []workobject.User, x, y, w int) int {
	h := l.dp(24)
	cx := x
	vs := l.woValueStyle()
	vs.maxLines, vs.ellipsis = 1, true
	for i, u := range us {
		if cx >= x+w {
			break
		}
		if i > 0 {
			cx += l.dp(2)
		}
		name := woUserName(wo, u)
		av := ""
		if u.UserType == "slack" {
			av = wo.Member(u.UserID).Avatar
		} else if u.Image != nil {
			av = u.Image.ImageURL
		}
		s := l.dp(20)
		if u.UserType != "slack" {
			s = l.dp(22)
		}
		top := y + (h-s)/2
		if av != "" {
			l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(cx, top, cx+s, top+s), url: av, radii: [4]int{l.dp(4), 0, 0, l.dp(4)}, target: -1, px: s})
		} else {
			l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(cx, top, cx+s, top+s), radius: l.dp(4), fill: l.tok("base-ter"), target: -1})
		}
		var tg int16 = -1
		if u.UserType == "slack" && u.UserID != "" {
			tg = l.d.addTarget(target{kind: tUser, id: u.UserID})
		}
		fs := vs
		_, nw := l.plainFlow(name, cx+s+l.dp(4), y+(h-fs.lineH)/2, max(x+w-cx-s-l.dp(4), l.dp(8)), &fs, tg)
		cx += s + l.dp(4) + nw + l.dp(4)
	}
	return h
}

// woCheckbox is CheckboxFieldValue: a disabled 16px checkbox and the text.
func (l *lay) woCheckbox(c workobject.Checkbox, x, y, w int) int {
	p := &l.st.Palette
	vs := l.woValueStyle()
	vs.maxLines, vs.ellipsis = 1, true
	s := l.dp(16)
	top := y + (vs.lineH-s)/2
	r := image.Rect(x, top, x+s, top+s)
	if c.Checked {
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, radius: l.dp(4), fill: l.tok("content-ter"), target: -1})
		l.d.imgs = append(l.d.imgs, imgPrim{r: r.Inset(l.dp(2)), url: iconURL + "check", target: -1, px: s - l.dp(4), tint: p.Panel})
	} else {
		l.d.boxes = append(l.d.boxes, box{kind: bBorder, r: r, radius: l.dp(4), fill: p.Panel, stroke: l.tok("otl-pry"), bw: max(1, l.dp(1)), target: -1})
	}
	h, _ := l.plainFlow(c.Text, x+s+l.dp(8), y, max(w-s-l.dp(8), l.dp(8)), &vs, -1)
	return max(h, vs.lineH)
}

// woImage is an image field: one image as its image block (the field
// alone: 16:9, resizeToPreview), several as 32px thumbnails 2px apart.
func (l *lay) woImage(f *workobject.Field, x, y, w int, alone bool) int {
	if len(f.Images) == 0 {
		return 0
	}
	if len(f.Images) > 1 {
		cx := x
		s := l.dp(32)
		for _, im := range f.Images {
			if cx+s > x+w {
				break
			}
			l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(cx, y, cx+s, y+s), url: im.ImageURL, radii: uni4(l.dp(4)), target: -1, px: s})
			cx += s + l.dp(2)
		}
		return s
	}
	im := f.Images[0]
	iw, ih := im.Width, im.Height
	if alone || iw <= 0 || ih <= 0 {
		iw, ih = 1600, 900
	}
	dw := w
	dh := int(float64(dw) * float64(ih) / float64(iw))
	img := &richtext.Image{URL: im.ImageURL, Alt: im.AltText, W: im.Width, H: im.Height}
	tg := l.d.addTarget(target{kind: tImage, img: img, inCard: true})
	r := image.Rect(x, y, x+dw, y+dh)
	l.d.imgs = append(l.d.imgs, imgPrim{r: r, url: im.ImageURL, radii: uni4(l.dp(4)), target: -1, px: dw})
	l.d.hits = append(l.d.hits, hit{r: r, t: tg})
	return dh
}

// woFooter lays out EntityFooter at (x, y) in w and returns its height.
func (l *lay) woFooter(att *richtext.Attachment, lay *workobject.Layout, actions []workobject.Button, st WorkObjectState,
	x, y, w int, card int16) int {
	fl := l.st.woFlags
	e := att.WorkObject.E
	loc := woLocale(fl)
	p := &l.st.Palette
	bw := max(1, l.dp(1))
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(x, y, x+w, y+bw), fill: l.tok("otl-ter"), target: -1})
	// the buttons: small outline buttons (28px), 4px apart
	bs := flowStyle{face: font.Font{Typeface: l.st.Face, Weight: font.Bold}, size: l.sp(13), lineH: l.dp(18), col: l.tok("content-pry"),
		maxLines: 1, ellipsis: true, noSelect: true}
	bh := l.dp(28)
	h := max(l.dp(36), bh+l.dp(16))
	top := y + bw
	rx := x + w - l.dp(8)
	type btn struct {
		r     image.Rectangle
		label string
		b     *workobject.Button
	}
	var btns []btn
	for i := len(actions) - 1; i >= 0; i-- {
		label := actions[i].Label()
		tw := min(l.textWidth(label, &bs), l.dp(160)) + 2*l.dp(12)
		r := image.Rect(rx-tw, top+(h-bh)/2, rx, top+(h-bh)/2+bh)
		btns = append(btns, btn{r: r, label: label, b: &actions[i]})
		rx -= tw + l.dp(4)
	}
	for i := len(btns) - 1; i >= 0; i-- {
		b := btns[i]
		tg := l.d.addTarget(target{kind: tWorkObject, id: "button:" + b.b.ActionID, att: att, wob: b.b})
		fill, stroke, col := p.Panel, l.tok("otl-sec"), l.tok("content-pry")
		switch b.b.Style {
		case "primary":
			fill, stroke, col = l.tok("base-hgl-2"), l.tok("base-hgl-2"), rgb(255, 255, 255)
		case "danger":
			fill, stroke, col = l.tok("base-dgr-pry"), l.tok("base-dgr-pry"), rgb(255, 255, 255)
		}
		l.d.boxes = append(l.d.boxes, box{kind: bBorder, r: b.r, radius: l.dp(8), fill: fill, stroke: stroke, bw: bw, target: tg, hover: l.tok("base-sec")})
		l.d.hits = append(l.d.hits, hit{r: b.r, t: tg})
		fs := bs
		fs.col = col
		l.plainFlow(b.label, b.r.Min.X+l.dp(12), b.r.Min.Y+(bh-fs.lineH)/2, b.r.Dx()-2*l.dp(12), &fs, tg)
	}
	// the time: "As of …" (the message's ts) or "Updated …" (updated_at),
	// unless refreshing ("Refreshing…") or it failed ("Failed to refresh")
	if fl.RefreshTimestamp {
		ts := flowStyle{face: font.Font{Typeface: l.st.Face}, size: l.sp(13), lineH: l.dpHalf(19.5), col: l.tok("content-ter"),
			maxLines: 1, ellipsis: true, noSelect: true}
		tx := x + l.dp(12)
		ty := top + (h-ts.lineH)/2
		avail := max(rx-tx-l.dp(12), l.dp(40))
		now := time.Now()
		label, tip := "", ""
		upd, hasUpd := workobject.ToTime(lay.UpdatedAtUnix())
		switch {
		case st.Refreshing:
			s := l.dp(16)
			l.d.spinners = append(l.d.spinners, spinPrim{r: image.Rect(tx, top+(h-s)/2, tx+s, top+(h-s)/2+s)})
			tx += s + l.dp(4)
			label = i18n.T("work-objects", "Refreshing…")
		case st.RefreshFailed:
			label = i18n.T("work-objects", "Failed to refresh")
		case hasUpd:
			label = workobject.UpdatedLabel(loc, upd, now)
			tip = workobject.FooterTip(loc, upd, fl.Time24)
		default:
			if at, ok := workobject.TSTime(l.opts.TS); ok {
				from := ""
				if pu := e.PerspectiveUserID(); pu != "" {
					from = att.WorkObject.Member(pu).Name
				}
				label = workobject.AsOfLabel(loc, at, now, fl.Time24, from)
				tip = workobject.FooterTip(loc, at, fl.Time24)
			}
		}
		_ = tip
		if label != "" {
			_, lw := l.plainFlow(label, tx, ty, avail, &ts, -1)
			// "Refresh", while the card is hovered and it can refresh
			if !st.Refreshing && !st.RefreshFailed && l.opts.Channel != "" && l.opts.TS != "" && e.EntityID != "" && e.IsRefreshable() &&
				!isGoogleDrive(e.AppID) && !isMicrosoft(e.AppID) {
				k := l.newHover()
				l.d.hovers[k-1].card = -2 // woCard fills in the card's range
				nb, ni, nf := l.hoverFrom()
				rs := ts
				rs.col = p.Link
				rxx := tx + lw + l.dp(4)
				tg := l.d.addTarget(target{kind: tWorkObject, id: "refresh", att: att, underline: true})
				_, rw := l.plainFlow(i18n.T("work-objects", "Refresh"), rxx, ty, max(rx-rxx, l.dp(20)), &rs, tg)
				l.d.hits = append(l.d.hits, hit{r: image.Rect(rxx, ty, rxx+rw, ty+rs.lineH), t: tg})
				l.hoverEnd(k, nb, ni, nf)
			}
		}
	}
	return h + bw
}

// woActionsBar lays out the actions bar inside the card's top right
// (top 8px, right 8px) and records its targets as the hover layers' skip
// range (the hovered subtitle and Refresh hide while the bar is hovered).
func (l *lay) woActionsBar(att *richtext.Attachment, opensURL, fileNoPreview bool, cw int, card image.Rectangle, cardT int16) {
	e := att.WorkObject.E
	fl := l.st.woFlags
	bw := max(1, l.dp(1))
	btn := l.dp(32)
	gap := 0
	padB := l.dp(4)
	narrow := cw > 0 && cw < l.dp(400) // iconOnly: no text, no divider
	type item struct {
		icon, text string
		id         string
		w          int
		divider    bool
	}
	var items []item
	textW := func(s string) int {
		fs := flowStyle{face: font.Font{Typeface: l.st.Face, Weight: font.Bold}, size: l.sp(13), lineH: l.dp(18)}
		return l.textWidth(s, &fs)
	}
	switch {
	case e.IsPageBlock:
		items = append(items, item{icon: "eye-open", text: i18n.T("work-objects", "View"), id: "preview"})
	case opensURL:
		v := "eye-open"
		t := i18n.T("work-objects", "Preview")
		if fileNoPreview {
			v, t = "info", i18n.T("work-objects", "Details")
		}
		items = append(items, item{icon: v, text: t, id: "preview"})
	default:
		items = append(items, item{icon: "new-window", text: i18n.T("work-objects", "Open"), id: "open"})
	}
	if narrow {
		items[0].text = ""
	} else {
		items[0].divider = true
	}
	if !e.IsPageBlock {
		// Share…, the comments button (wo_comment_sync_client_xp, an
		// entity that carries comments), View conversations. With
		// work_objects_embedded_preview, an item whose full_size_preview
		// is an embed (DbXo O) gets "Open in side panel" in View
		// conversations' place; Clack draws no embedded previews (an
		// iframe), so it keeps View conversations.
		if fl.CanShare {
			items = append(items, item{icon: "share-message", id: "share"})
		}
		if fl.CommentSync && e.Comments != nil {
			items = append(items, item{icon: "comment", id: "comments"})
		}
		items = append(items, item{icon: "reference", id: "conversations"})
	}
	items = append(items, item{icon: "ellipsis-vertical-filled", id: "more"})
	total := 2*padB + 2*bw
	for i := range items {
		it := &items[i]
		if it.text != "" {
			it.w = l.dp(8) + l.dp(16) + l.dp(4) + textW(it.text) + l.dp(8)
		} else {
			it.w = btn
		}
		total += it.w + gap
		if it.divider {
			total += l.dp(9) // the 1px divider and its 4px margins
		}
	}
	h := btn + 2*padB + 2*bw
	bx := card.Max.X - l.dp(8) - total
	by := card.Min.Y + l.dp(8)
	bar := image.Rect(bx, by, bx+total, by+h)
	// the shadow (0 1px 3px #0000001a, 0 1px 2px -1px #0000001a), the bar
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: bar.Add(image.Pt(0, l.dp(1))).Inset(-l.dp(1)), radius: l.dp(13), fill: rgba(0, 0, 0, 0.06), target: -1})
	l.d.boxes = append(l.d.boxes, box{kind: bBorder, r: bar, radius: l.dp(12), fill: l.st.Palette.Panel, stroke: l.tok("otl-ter"), bw: bw, target: -1})
	cx := bx + bw + padB
	cy := by + bw + padB
	first := int16(-1)
	for _, it := range items {
		tg := l.d.addTarget(target{kind: tWorkObject, id: it.id, att: att})
		if first < 0 {
			first = tg
		}
		r := image.Rect(cx, cy, cx+it.w, cy+btn)
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, radius: l.dp(8), target: tg, hover: l.tok("base-sec")})
		l.d.hits = append(l.d.hits, hit{r: r, t: tg})
		col := l.tok("content-ter")
		if it.text != "" {
			// actionBarTextButton: content-sec text, the 16px glyph and 4px
			gs := l.dp(16)
			gx := cx + l.dp(8)
			l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(gx, cy+(btn-gs)/2, gx+gs, cy+(btn-gs)/2+gs), url: iconURL + it.icon, target: -1, px: gs, tint: l.tok("content-sec")})
			fs := flowStyle{face: font.Font{Typeface: l.st.Face, Weight: font.Bold}, size: l.sp(13), lineH: l.dp(18), col: l.tok("content-sec"), noSelect: true, maxLines: 1}
			l.plainFlow(it.text, gx+gs+l.dp(4), cy+(btn-fs.lineH)/2, it.w, &fs, -1)
		} else {
			gs := l.dp(18)
			if it.id == "open" || it.id == "preview" {
				gs = l.dp(16)
			}
			l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(cx+(it.w-gs)/2, cy+(btn-gs)/2, cx+(it.w-gs)/2+gs, cy+(btn-gs)/2+gs),
				url: iconURL + it.icon, target: -1, px: gs, tint: col})
		}
		if it.id == "comments" {
			// commentsBadgeIcon: a 4px flamingo-60 dot at top 4 right 4
			if st := l.cardState(att); st.NewerComments {
				d := l.dp(4)
				l.d.boxes = append(l.d.boxes, box{kind: bCircle, r: image.Rect(r.Max.X-l.dp(4)-d, r.Min.Y+l.dp(4), r.Max.X-l.dp(4), r.Min.Y+l.dp(4)+d),
					fill: l.tok("plt-flamingo-60"), target: -1, layer: 1})
			}
		}
		cx += it.w + gap
		if it.divider {
			l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(cx+l.dp(4), cy+l.dp(6), cx+l.dp(4)+bw, cy+btn-l.dp(6)), fill: l.tok("otl-ter"), target: -1})
			cx += l.dp(9)
		}
	}
	// the bar's own hover skip range: its buttons
	k := len(l.d.hovers) - 1
	if k >= 0 {
		l.d.hovers[k].skip, l.d.hovers[k].skipTo = first, int16(len(l.d.targets)-1)
	}
}

// cardState is the host's state for a card in this layout.
func (l *lay) cardState(att *richtext.Attachment) WorkObjectState {
	if src := l.st.WorkObjects; src != nil && att.WorkObject != nil && att.WorkObject.E != nil {
		return src.WorkObjectState(l.opts.Channel, l.opts.TS, att.WorkObject.E)
	}
	return WorkObjectState{}
}

// woRemove is DeleteWorkObjectAttachmentButton ("Remove preview",
// times-small): Slack's c-message_attachment_v2__delete, which Clack
// draws like the v3 remove overlay.
func (l *lay) woRemove(att *richtext.Attachment, cardT int16, r image.Rectangle) {
	s := l.dp(20)
	off := l.dp(9)
	br := image.Rect(r.Max.X+off-s, r.Min.Y-off, r.Max.X+off, r.Min.Y-off+s)
	t := l.d.addTarget(target{kind: tRemove, id: att.ID})
	l.d.hits = append(l.d.hits, hit{r: br, t: t})
	l.d.removes = append(l.d.removes, removeBtn{r: br, card: cardT, del: t, cardTo: t - 1})
}

// drawHoverLayers paints the hover layers whose card is hovered, over the
// static content.
func (v *View) drawHoverLayers(gtx layout.Context, st *Style) {
	if !v.hovering || v.hover < 0 || len(v.d.hovers) == 0 {
		return
	}
	for i := range v.d.hovers {
		if v.d.hovers[i].shown(v.hover) {
			v.hovPaint = int16(i + 1)
			v.paint(gtx.Ops, st, 0, nil)
		}
	}
	v.hovPaint = 0
}

// woActions maps a card target's id to its action.
var woActions = map[string]WorkObjectAction{
	"click": WOClick, "open": WOOpen, "preview": WOPreview, "share": WOShare, "comments": WOComments,
	"conversations": WOConversations, "more": WOMore, "side-panel": WOSidePanel, "refresh": WORefresh,
}

// woEvent is the event for a click on tg: its button's bottom-left is
// the menu anchor (window coordinates); the card itself anchors at the
// click.
func (v *View) woEvent(tg *target, anchor image.Point) WorkObjectEvent {
	ev := WorkObjectEvent{Att: tg.att, Anchor: anchor, Channel: v.opts.Channel, TS: v.opts.TS}
	if a, ok := woActions[tg.id]; ok {
		ev.Action = a
	} else if strings.HasPrefix(tg.id, "button:") {
		ev.Action, ev.Button = WOButton, tg.wob
	}
	if ev.Action != WOClick {
		for _, h := range v.d.hits {
			if h.t < 0 || int(h.t) >= len(v.d.targets) {
				continue
			}
			if o := &v.d.targets[h.t]; o.kind == tWorkObject && o.att == tg.att && o.id == tg.id {
				ev.Anchor = image.Pt(h.r.Min.X, h.r.Max.Y).Add(v.Origin)
			}
		}
	}
	return ev
}

// ---- the flexpane's Details tab ----

// woDetails lays out WorkObjectDetails (i4MO) for the entity at (x, y)
// in w: 20px padding (details__S9LRs), the full layout's fields
// (ExternalWorkObjectFieldBlocks, full style; the expanded layout's when
// there's no full one, forceFullLayoutStyle), then the updatedTsFooter
// ("Updated …" from the entity's ts: a 1px otl-ter top border, 12px over
// and 16px under 13px content-ter text) when there are fields to date.
func (l *lay) woDetails(b *richtext.Block, x, y, w int) int {
	wo := b.Att.WorkObject
	e := wo.E
	pad := l.dp(20)
	y0 := y
	y += pad
	x += pad
	w -= 2 * pad
	lay := e.Full()
	if lay == nil {
		lay = e.Expanded()
	}
	if lay == nil {
		return pad * 2
	}
	// container full-layout (width>=496px): label 150px and value in a row
	row := w >= l.dp(496)
	for i := range lay.Fields {
		f := &lay.Fields[i]
		if f.IsImage() && !(len(f.Images) > 0 && f.Images[0].Width > 0 && f.Images[0].Height > 0) && !woHasImagePreview(lay) {
			continue
		}
		y += l.woFullField(wo, lay, i, x, y, w, row) + l.dp(20) // contentItem.full: margin-bottom 20px
	}
	soleImage := len(lay.Fields) == 1 && woHasImagePreview(lay)
	if len(lay.Fields) > 0 && !soleImage {
		if t, ok := workobject.TSTime(e.TS); ok {
			fl := l.st.woFlags
			bw := max(1, l.dp(1))
			l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(x, y, x+w, y+bw), fill: l.tok("otl-ter"), target: -1})
			fs := flowStyle{face: font.Font{Typeface: l.st.Face}, size: l.sp(13), lineH: l.dpHalf(18), col: l.tok("content-ter"),
				maxLines: 1, ellipsis: true}
			h, _ := l.plainFlow(workobject.UpdatedLabel(woLocale(fl), t, time.Now()), x, y+bw+l.dp(12), w, &fs, -1)
			l.plainAdd("\n")
			y += bw + l.dp(12) + h + l.dp(16)
		}
	}
	return y - y0
}

// woFullField is one field in the full style (contentItem.full: 8px
// gap; label 15px/1.46668 content-ter, 150px wide in a row; value
// 15px/1.46668, whole: text links, rich text as rich text, tokens that
// wrap 4px apart) and returns its height.
func (l *lay) woFullField(wo *richtext.WorkObject, lay *workobject.Layout, i, x, y, w int, row bool) int {
	f := &lay.Fields[i]
	p := &l.st.Palette
	ls := l.woValueStyle()
	ls.col = l.tok("content-ter")
	gap := l.dp(8)
	label := f.Label
	if f.Name == "attendees" && f.IsUser() {
		label += " (" + strconv.Itoa(len(f.Users)) + ")"
	}
	vx, vy, vw := x, y, w
	lh := 0
	if !f.IsImage() || label != "" {
		lw := w
		if row {
			lw = l.dp(150)
		}
		ls.maxLines, ls.ellipsis = 1, true
		h, _ := l.plainFlow(label, x, y, lw, &ls, -1)
		l.plainAdd("\n")
		lh = max(h, ls.lineH)
		if row {
			vx, vw = x+l.dp(150)+gap, max(w-l.dp(150)-gap, l.dp(40))
		} else {
			vy = y + lh + gap
		}
	}
	vs := l.woValueStyle()
	vh := 0
	switch {
	case f.IsRichText() || f.IsUnknown():
		if bs := wo.RichField(lay.Key, i); len(bs) > 0 {
			vh = l.blocks(bs, vx, vy, vw, false)
		}
	case f.Type == workobject.FieldTag:
		vh = l.woTagsWrap(f.Tags, vx, vy, vw)
	case f.Type == workobject.FieldEntityRef:
		tags := make([]workobject.Tag, len(f.Refs))
		for k, r := range f.Refs {
			tags[k] = workobject.Tag{Text: r.Title, Color: workobject.TagInformative}
		}
		vh = l.woTagsWrap(tags, vx, vy, vw)
	case f.IsUser():
		vh = l.woUsers(wo, f.Users, vx, vy, vw)
	case f.Type == workobject.FieldChannel:
		cx := vx
		for _, c := range f.Channels {
			fs := vs
			fs.col = p.Link
			_, cw := l.plainFlow("#"+wo.Channel(c.ChannelID), cx, vy, max(vx+vw-cx, l.dp(20)), &fs, l.d.addTarget(target{kind: tChannel, id: c.ChannelID, cardless: true}))
			cx += cw + l.dp(4)
		}
		vh = vs.lineH
	case f.Type == workobject.FieldLink:
		for _, u := range f.Links {
			fs := vs
			fs.col = p.Link
			h, _ := l.plainFlow(u, vx, vy+vh, vw, &fs, l.d.addTarget(target{kind: tLink, url: richtext.SafeURL(u), underline: true, website: true}))
			vh += h
		}
	case f.Type == workobject.FieldCheckbox:
		for _, c := range f.Checks {
			vh += l.woCheckbox(c, vx, vy+vh, vw)
		}
	case f.IsImage():
		iw := vw
		if row {
			iw = min(iw, l.dp(320)) // previewImage: max-width 320px
		}
		vh = l.woImage(f, vx, vy, iw, len(lay.Fields) == 1)
	case f.Type == workobject.FieldText:
		h, _ := l.plainFlow(f.Text, vx, vy, vw, &vs, -1)
		vh = max(h, vs.lineH)
	default:
		if f.Type == workobject.FieldTimestamp || f.Type == workobject.FieldDate {
			vs.col = l.tok("content-sec")
		}
		h, _ := l.plainFlow(woFieldText(woLocale(l.st.woFlags), f, time.Now()), vx, vy, vw, &vs, -1)
		vh = max(h, vs.lineH)
	}
	l.plainAdd("\n")
	if row {
		return max(lh, vh)
	}
	return vy - y + vh
}

// woTagsWrap is the flexpane's tags (flexpaneTokenArray: wrapping, 4px
// apart) and returns their height.
func (l *lay) woTagsWrap(tags []workobject.Tag, x, y, w int) int {
	h := l.dp(24)
	cx, cy := x, y
	ts := l.woTagStyle()
	for _, t := range tags {
		tw := l.textWidth(t.Text, &ts) + l.dp(8)
		if t.IconURL != "" {
			tw += l.dp(20)
		}
		tw = min(tw, w)
		if cx > x && cx+tw > x+w {
			cx, cy = x, cy+h+l.dp(4)
		}
		l.woTags([]workobject.Tag{t}, cx, cy, tw)
		cx += tw + l.dp(4)
	}
	return cy + h - y
}

// WorkObjectRect is a work object card's box (View coordinates).
type WorkObjectRect struct {
	Att *richtext.Attachment
	R   image.Rectangle
}

// WorkObjectRects lists the laid-out work object cards (after the first
// Layout), in layout order.
func (v *View) WorkObjectRects() []WorkObjectRect {
	var out []WorkObjectRect
	for _, h := range v.d.hits {
		if h.t < 0 || int(h.t) >= len(v.d.targets) || h.r.Empty() {
			continue
		}
		if tg := &v.d.targets[h.t]; tg.kind == tWorkObject && tg.id == "click" && tg.att != nil {
			out = append(out, WorkObjectRect{Att: tg.att, R: h.r})
		}
	}
	return out
}
