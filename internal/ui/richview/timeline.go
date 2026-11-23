package richview

// A timeline block: Slack's TimelineCondensed (boot-shared 9B8s; CDN build
// 9f450b8ad3db), from client-boot-styles' timeline rules:
//
//   - .container: content-ter, 13px, margin 12px 0 (none above the
//     message's first block);
//   - the header (a button when it expands: more than one point, or a
//     point with steps), flex with 4px gaps: the text, with the last
//     three distinct point icons before it (20px SvgIcons, a 16px image
//     for an icon_url; a stack masks all but the last to 11px), then the
//     caret (caret-right, caret-down while open; margin-top 4px). Its
//     text is the only point's title; with more points, the summary, else
//     "Using N tools" while one runs and "Used N tools" after, except that
//     a collapsed running timeline without a summary shows its last
//     point's title;
//   - open: .pointList (padding 2px 0 0 8px): with more than one point a
//     row per point (its icon in the 16px step indicator column, its
//     title), then a row per step section (a 5px otl-pry dot on the
//     first and last, its title, "(Sources)" when it has sources); the
//     column's 1px otl-ter line runs from row to row, ending at the last.
//
// Left out: the running header's shimmer (drawn still in plt-gray-50).

import (
	"image"
	"image/color"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/icons"
)

var nsTimeline = i18n.NS("timeline_condensed")

// TimelineHandler is optional on a Handler: a timeline section's
// "(Sources)" was clicked (btn in window coordinates).
type TimelineHandler interface {
	TimelineSources(sec *richtext.TimelineSection, btn image.Rectangle)
}

// timelineOpenKey is the toggle's key in the View's collapsed map (true:
// open), by block id so it outlives a rebuilt doc.
type timelineOpenKey string

func timelineKey(t *richtext.Timeline) any {
	if t.BlockID != "" {
		return timelineOpenKey(t.BlockID)
	}
	return t
}

// timelineIcon is a point's icon: an image, else Slack's icon by Clack's
// name ("" none).
type timelineIcon struct{ name, url string }

func timelineIcons(pts []richtext.TimelinePoint) []timelineIcon {
	var out []timelineIcon
	for _, p := range pts {
		ic := timelineIcon{url: p.IconURL}
		if n := richtext.TimelineIcon(p.Icon); n != "" && ic.url == "" {
			ic.name = n
		}
		if ic.name == "" && ic.url == "" {
			continue
		}
		dup := false
		for _, o := range out {
			if ic.url != "" && o.url == ic.url || ic.url == "" && o.url == "" && o.name == ic.name {
				dup = true
			}
		}
		if !dup {
			out = append(out, ic)
		}
	}
	if len(out) > 3 {
		out = out[len(out)-3:]
	}
	return out
}

// timelineFlow is the timeline's text style: 13px content-ter, 19px lines
// (13px at the message's 1.46668).
func (l *lay) timelineFlow() flowStyle {
	fs := l.bodyStyle()
	fs.size, fs.lineH = l.sp(13), l.dp(19)
	fs.col = l.tok("content-ter")
	fs.emoji = l.dp(16)
	return fs
}

// timeline lays out a timeline block.
func (l *lay) timeline(b *richtext.Block, x, y, w int) int {
	tl := b.Timeline
	if tl == nil || tl.Hidden || len(tl.Points) == 0 {
		return 0
	}
	k := timelineKey(tl)
	open := l.collapsed != nil && l.collapsed[k]
	n := len(tl.Points)
	last := &tl.Points[n-1]
	steps := false
	for i := range tl.Points {
		if len(tl.Points[i].Sections) > 0 {
			steps = true
		}
	}
	expandable := n > 1 || steps
	running := tl.Running
	// the header's text
	var spans []richtext.Span
	switch {
	case n != 1 && (open || !running || tl.HasSummary):
		switch {
		case tl.HasSummary:
			spans = tl.Summary
		case running:
			spans = []richtext.Span{{Kind: richtext.Text, Text: nsTimeline.T("Using {count, plural, one {# tool} other {# tools}}", "count", n)}}
		default:
			spans = []richtext.Span{{Kind: richtext.Text, Text: nsTimeline.T("Used {count, plural, one {# tool} other {# tools}}", "count", n)}}
		}
	default:
		spans = last.Title
	}
	fs := l.timelineFlow()
	if running && !open { // .headerTextShimmer (still)
		fs.col = l.tok("plt-gray-50")
	}
	gap := l.dp(4)
	caret := l.dp(20)
	textW := w
	if expandable {
		textW -= gap + caret
	}
	var tg int16 = -1
	if expandable {
		tg = l.d.addTarget(target{kind: tCollapse, key: k})
	}
	// the icons before the text (shown with one icon, or while closed)
	ics := timelineIcons(tl.Points)
	iconW := 0
	if len(ics) > 0 && (len(ics) <= 1 || !open) {
		iconW = l.timelineIcons(ics, x, y, fs.lineH)
		iconW += gap // margin-right 4px
	}
	l.plainAdd("\n")
	l.lead = float32(iconW)
	h, tw := l.textFlowTarget(spans, x, y, max(textW, 1), &fs, tg)
	l.lead = 0
	h = max(h, fs.lineH)
	if expandable {
		cx := x + max(tw, iconW) + gap
		if tw >= textW-iconW {
			cx = x + textW + gap
		}
		name := "chevron-right" // Slack's caret-right
		if open {
			name = "chevron-down" // Slack's caret-down
		}
		cy := y + l.dp(4)
		l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(cx, cy, cx+caret, cy+caret), url: iconURL + name, target: -1, px: caret, tint: fs.col})
		h = max(h, l.dp(4)+caret)
		hr := image.Rect(x, y, x+w, y+h) // the header button: the row's width
		l.d.hits = append(l.d.hits, hit{r: hr, t: tg})
	}
	yy := y + h
	if open {
		yy += l.timelineList(tl, x, yy, w)
	}
	return yy - y
}

// textFlowTarget lays spans out with every text span on target tg (-1:
// as addSpans does).
func (l *lay) textFlowTarget(spans []richtext.Span, x, y, w int, fs *flowStyle, tg int16) (int, int) {
	if tg >= 0 {
		l.addSpansTarget(spans, fs, tg)
	} else {
		l.addSpans(spans, fs)
	}
	return l.flow(x, y, w, fs)
}

// timelineIcons draws IconStack at (x, y), centred on a line lineH tall,
// and returns its width.
func (l *lay) timelineIcons(ics []timelineIcon, x, y, lineH int) int {
	s := l.dp(20)
	img := l.dp(16)
	masked := l.dp(11)
	cx := x
	for i, ic := range ics {
		top := y + (lineH-s)/2 - l.dp(1) // top: -1px
		var r image.Rectangle
		if ic.url != "" {
			off := (s - img) / 2
			r = image.Rect(cx, top+off, cx+img, top+off+img)
			l.d.imgs = append(l.d.imgs, imgPrim{r: r, url: ic.url, radii: [4]int{l.dp(4), l.dp(4), l.dp(4), l.dp(4)}, target: -1, px: img})
		} else {
			r = image.Rect(cx, top, cx+s, top+s)
			l.d.imgs = append(l.d.imgs, imgPrim{r: r, url: iconURL + icons.Slack(ic.name), target: -1, px: s, tint: l.timelineIconTint(ic.name)})
		}
		if i < len(ics)-1 {
			cx += masked // .iconStackItemMasked: 11px, its right faded
		} else {
			cx = r.Max.X
		}
	}
	return cx - x
}

// timelineIconTint is the icon's colour: content-ter, Salesforce's in
// plt-lagoon-50 (.pointIconSfColor).
func (l *lay) timelineIconTint(name string) color.NRGBA {
	if name == "sf-cloud-filled" || name == "agentforce-filled" {
		return l.tok("plt-lagoon-50")
	}
	return l.tok("content-ter")
}

// timelineList is ExpandedPointList: the points (with more than one) and
// their steps' sections, one 16px step column down their left.
func (l *lay) timelineList(tl *richtext.Timeline, x, y, w int) int {
	type row struct {
		pt   *richtext.TimelinePoint
		sec  *richtext.TimelineSection
		dot  bool
		icon *timelineIcon
	}
	var rows []row
	for i := range tl.Points {
		p := &tl.Points[i]
		if len(tl.Points) > 1 {
			r := row{pt: p}
			if ics := timelineIcons([]richtext.TimelinePoint{*p}); len(ics) == 1 {
				r.icon = &ics[0]
			}
			rows = append(rows, r)
		}
		for j := range p.Sections {
			rows = append(rows, row{sec: &p.Sections[j], dot: j == 0 || j == len(p.Sections)-1})
		}
	}
	fs := l.timelineFlow()
	px, py := x+l.dp(8), y+l.dp(2)
	col := l.dp(16)
	gap := l.dp(4)
	tx := px + col + gap
	line := l.tok("otl-ter")
	lw := max(1, l.dp(1))
	lx := px + (col-lw)/2
	yy := py
	for ri, r := range rows {
		top := yy
		var spans []richtext.Span
		if r.pt != nil {
			spans = r.pt.Title
		} else {
			spans = r.sec.Title
		}
		l.plainAdd("\n")
		th, tw := l.textFlowTarget(spans, tx, top+l.dp(2), max(w-(tx-x), 1), &fs, -1)
		rh := max(th, fs.lineH) + 2*l.dp(2)
		// "(Sources)": its section's sources modal
		if r.sec != nil && r.sec.Sources > 0 {
			sx := tx + tw + gap
			sfs := fs
			sfs.col = l.tok("content-hgl-1")
			stg := l.d.addTarget(target{kind: tTimeline, timeline: r.sec})
			l.plainAdd(" ")
			_, sw := l.textFlowTarget([]richtext.Span{{Kind: richtext.Text, Text: nsTimeline.T("(Sources)")}}, sx, top+l.dp(2), max(w-(sx-x), 1), &sfs, stg)
			l.d.hits = append(l.d.hits, hit{r: image.Rect(sx, top+l.dp(2), sx+sw, top+l.dp(2)+fs.lineH), t: stg})
		}
		// the step column: line, mark, line (the last row's bottom line
		// clear)
		lastRow := ri == len(rows)-1
		topLen := l.dp(7)
		var mark int
		switch {
		case r.icon != nil:
			topLen = l.dp(3)
			mark = l.dp(20)
			if r.icon.url != "" {
				mark = l.dp(16)
			}
		case r.dot:
			mark = l.dp(5)
		}
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(lx, top, lx+lw, top+topLen), fill: line, target: -1})
		my := top + topLen
		if mark > 0 {
			my += l.dp(2)
			mx := px + (col-mark)/2
			mr := image.Rect(mx, my, mx+mark, my+mark)
			switch {
			case r.icon != nil && r.icon.url != "":
				l.d.imgs = append(l.d.imgs, imgPrim{r: mr, url: r.icon.url, radii: [4]int{l.dp(4), l.dp(4), l.dp(4), l.dp(4)}, target: -1, px: mark})
			case r.icon != nil:
				l.d.imgs = append(l.d.imgs, imgPrim{r: mr, url: iconURL + icons.Slack(r.icon.name), target: -1, px: mark, tint: l.timelineIconTint(r.icon.name)})
			default:
				l.d.boxes = append(l.d.boxes, box{kind: bFill, r: mr, radius: mark / 2, fill: l.tok("otl-pry"), target: -1})
			}
			my += mark + l.dp(2)
			rh = max(rh, my-top)
		}
		if !lastRow {
			l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(lx, my, lx+lw, top+rh), fill: line, target: -1})
		}
		yy = top + rh
	}
	return yy - y
}

// TimelineOpen reports whether the View's timeline t is open (tests).
func (v *View) TimelineOpen(t *richtext.Timeline) bool {
	return v.collapsed != nil && v.collapsed[timelineKey(t)]
}
