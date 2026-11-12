package views

import (
	"context"
	"image"
	"image/color"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// The history rows: HuddlesHistoryListItem (shared ZvF8) in HuddlesListRow
// (JZX4), its CSS from client-boot-styles:
//
//   - the row: sk_primary_background between 1px otl-ter sides (no line
//     below but the last row's), padding 10px 12px; the first and last
//     rows of a run round their outer corners 8 (.p-huddles_page_history__
//     first_el/last_el); a click goes to the conversation at the huddle
//     message (jumpToMessage) under the "Go to conversation" tooltip;
//   - the headphones in a 36px tile (foreground_soft .06, radius 6; a
//     missed huddle's base-imp with content-imp);
//   - the title (15px bold): the topic, else the channel's name with its
//     filled icon, else the people; a topic in a channel adds "in
//     #channel" from 640 wide;
//   - the second row (13px content-sec, gap 4): when (toTimeAgo; under
//     huddle_page_enhancements the HUDDLE_HISTORY_ROW format: within a day
//     the time ago, else {date_long}), "·" and how long (use_huddle_
//     duration_text), then "· N replies" (the thread) and, with
//     huddles_history_files_section, "· N files"; "· From a private
//     conversation" (info tooltip) for a conversation you can't see, and
//     a missed huddle's "· Missed huddle invite" in content-imp (no
//     duration);
//   - on the right (gap 8, 12 after the text): under
//     huddle_page_enhancements the thread / files & links / notes buttons
//     (28px, 1px otl-ter, radius 4; from 640 wide with their counts), the
//     participants (24px faces overlapping 4px, at most three, radius 6,
//     a 2px ring; padding 6, surf-pry on hover; "See N participants"
//     opens ParticipantsModalView), the AI notes button
//     (huddle_page_enhancements off: an icon under 640, "icon + label"
//     from 640) and the ⋯ HuddleHistoryOverflowMenu.

// Slack's row breakpoints (es): the item's own width.
const (
	hdRowSmall     = 300
	hdRowArtifacts = 320
	hdRowFacepile  = 480
	hdRowMedium    = 640
)

// hdArtifacts is useHuddleArtifactActions: what a past huddle left.
type hdArtifacts struct {
	replies     int    // the huddle message's replies
	messageTS   string // thread_root_ts when it's a real one
	thread      bool   // a thread with replies
	files       int    // artifacts.file_ids + links
	notes       bool   // transcript_file_id
	notesLabel  string // viewRecordingCopy
	transcript  string // the transcript (AI notes canvas) file id
	attachments int    // attached_file_ids (huddles_history_files_section)
}

func (v *Huddles) artifacts(h *core.Huddle) hdArtifacts {
	a := hdArtifacts{transcript: h.TranscriptFileID, attachments: len(h.AttachedFileIDs)}
	a.replies = v.replyCount(*h)
	if h.ThreadTS != "" && h.ThreadTS != "0000000000.000000" {
		a.messageTS = h.ThreadTS
	}
	a.thread = a.messageTS != "" && a.replies > 0
	a.files = len(h.FileIDs) + len(h.Links)
	a.notes = h.TranscriptFileID != ""
	// useHuddleRecordingCopy (8Lec): the notes canvas's label by
	// recording type
	a.notesLabel = nsHuddles.T("View AI notes")
	if h.RecordingType == "transcription_only" {
		a.notesLabel = nsHuddles.T("View transcript")
	}
	return a
}

// hdDurationText is use_huddle_duration_text's timeOnlyText: "N hours N
// minutes", "N minutes", "Less than a minute".
func hdDurationText(h *core.Huddle) string {
	if h.End <= 0 || h.Start <= 0 {
		return ""
	}
	secs := h.End - h.Start
	hrs := secs / 3600 % 24
	mins := secs / 60 % 60
	switch {
	case hrs > 0 && mins > 0:
		return nsHuddles.T("{hours, plural, =1 {# hour} other {# hours}} {mins, plural, =1 {# minute} other {# minutes}}", "hours", hrs, "mins", mins)
	case hrs > 0:
		return nsHuddles.T("{hours, plural, =1 {# hour} other {# hours}}", "hours", hrs)
	case mins > 0:
		return nsHuddles.T("{mins, plural, =1 {# minute} other {# minutes}}", "mins", mins)
	}
	return nsHuddles.T("Less than a minute")
}

// hdDateText is the row's Timestamp: toTimeAgo (relative, alwaysAgo), or
// under huddle_page_enhancements the HUDDLE_HISTORY_ROW format: the time
// ago within a day, else {date_long}.
func (v *Huddles) hdDateText(start int64, now time.Time) string {
	t := unixTime(start)
	loc := i18n.Locale()
	if v.enhancements() && now.Sub(t).Abs() >= 24*time.Hour {
		return i18n.LongDate(loc, t, now)
	}
	return i18n.TimeAgo(loc, t, now, false, true)
}

// title is use_past_huddles_data's title: the topic, else the channel's
// name (a public or private channel you can see), else the people.
func (v *Huddles) historyTitle(h *core.Huddle) (title, icon string, isChannel bool) {
	ch := roomChannel(*h)
	c, known := v.h.Core().Conv(ch)
	v.mu.Lock()
	gone := v.missing[ch]
	v.mu.Unlock()
	if !known && !gone {
		ref := v.h.Core().ConvRef(ch)
		c = core.Conv{ID: ch, Name: ref.Name, Kind: ref.Kind}
	}
	switch c.Kind {
	case "channel":
		icon = "channel"
	case "private":
		icon = "lock"
	}
	if h.Name != "" {
		return h.Name, icon, false
	}
	if !gone && icon != "" {
		_, name := v.channelIconAndName(ch, h.History)
		return name, icon, true
	}
	return v.truncatedNames(h.History), "", false
}

func (v *Huddles) layoutHistoryRow(gtx C, it hpItem) D {
	w := gtx.Constraints.Max.X
	x, cw := hdColumn(gtx, w)
	defer opOffset(gtx, image.Pt(x, 0)).Pop()
	g := gtx
	g.Constraints = layout.Constraints{Min: image.Pt(cw, 0), Max: image.Pt(cw, gtx.Constraints.Max.Y)}
	d := v.historyRow(g, it.h, it.first, it.last)
	return D{Size: image.Pt(w, d.Size.Y)}
}

// historyRow is HuddlesHistoryListItem in HuddlesListRow.
func (v *Huddles) historyRow(gtx C, h *core.Huddle, first, last bool) D {
	B := gtx.Constraints.Max.X
	ch := roomChannel(*h)
	self := v.selfID()
	missed := contains(h.Missed, self)
	art := v.artifacts(h)
	enh := v.enhancements()
	key := "hist:" + h.ID
	rowC := v.clk(key)
	faceC := v.clk(key + ":faces")
	threadC := v.clk(key + ":thread")
	filesC := v.clk(key + ":files")
	notesC := v.clk(key + ":notes")
	moreC := v.clk(key + ":more")
	v.mu.Lock()
	gone := v.missing[ch]
	v.mu.Unlock()

	// the buttons inside take their clicks first (stopPropagation)
	inner := false
	if faceC.Clicked(gtx) {
		inner = true
		v.openParticipants(h)
	}
	if threadC.Clicked(gtx) {
		inner = true
		if art.messageTS != "" {
			v.h.OpenThread(ch, art.messageTS)
		}
	}
	if notesC.Clicked(gtx) {
		inner = true
		if co, ok := v.h.(interface{ OpenCanvasFull(id string) }); ok && art.transcript != "" {
			co.OpenCanvasFull(art.transcript)
		}
	}
	if filesC.Clicked(gtx) {
		inner = true // Slack opens the huddle's files view in the sidebar; Clack has none
	}
	if moreC.Clicked(gtx) {
		inner = true
		v.openRowMenu(gtx, h, art, moreC)
	}
	if rowC.Clicked(gtx) && !inner && ch != "" {
		if art.messageTS != "" {
			v.h.Navigate("c/" + ch + "/m/" + art.messageTS) // jumpToMessage
		} else {
			v.h.Navigate("c/" + ch)
		}
	}

	bg := v.tok("dt_color-base-pry") // sk_primary_background
	line := v.tokOver("dt_color-otl-ter", bg)
	ter := v.tok("dt_color-content-ter")
	sec := v.tok("dt_color-content-sec")
	imp := v.tok("dt_color-content-imp")
	fg := v.tok("dt_color-content-pry")
	if missed {
		fg = imp
	}
	pad := gtx.Dp(12)
	// the right side first: its width bounds the text
	showFaces := !(enh && B < gtx.Dp(hdRowFacepile)) && !(!enh && B <= gtx.Dp(hdRowSmall))
	showArtifacts := enh && B >= gtx.Dp(hdRowArtifacts)
	expanded := B >= gtx.Dp(hdRowMedium)
	right := recordTo(gtx, func(gtx C) D {
		gtx.Constraints = layout.Constraints{Max: image.Pt(B, 1<<20)}
		var kids []layout.FlexChild
		gap := func() {
			if len(kids) > 0 {
				kids = append(kids, hgap(8))
			}
		}
		if showArtifacts {
			if art.thread {
				gap()
				kids = append(kids, layout.Rigid(func(gtx C) D {
					return v.artifactButton(gtx, threadC, "threads", art.replies, expanded,
						nsHuddles.T("View thread"), nsHuddles.T("{replyCount, plural, =1 {# reply} other {# replies}}", "replyCount", art.replies))
				}))
			}
			if art.files > 0 {
				gap()
				kids = append(kids, layout.Rigid(func(gtx C) D {
					return v.artifactButton(gtx, filesC, "canvas-browser", art.files, expanded,
						nsHuddles.T("View files & links"), nsHuddles.T("{itemCount, plural, =1 {# item} other {# items}}", "itemCount", art.files))
				}))
			}
			if art.notes {
				gap()
				kids = append(kids, layout.Rigid(func(gtx C) D {
					return v.artifactButton(gtx, notesC, "ai-summary", -1, expanded, art.notesLabel, "")
				}))
			}
		}
		if showFaces {
			gap()
			kids = append(kids, layout.Rigid(func(gtx C) D {
				d := v.historyFaces(gtx, faceC, h.History, bg)
				if showArtifacts && d.Size.X < gtx.Dp(76) { // .p-huddles_history_list_item_v2_content_section__facepile_slot
					return D{Size: image.Pt(gtx.Dp(76), d.Size.Y)}
				}
				return d
			}))
		}
		if !enh && art.notes {
			gap()
			kids = append(kids, layout.Rigid(func(gtx C) D { return v.notesButton(gtx, notesC, art.notesLabel, expanded) }))
		}
		if len(kids) == 0 {
			return D{}
		}
		return row(gtx, layout.Middle, kids...)
	})
	moreW := gtx.Dp(28)
	rightW := right.d.Size.X
	if rightW > 0 && B > gtx.Dp(hdRowSmall) {
		rightW += gtx.Dp(12) // margin-left 12
	}
	textW := max(0, B-2*pad-gtx.Dp(36)-gtx.Dp(12)-rightW-moreW)
	if B <= gtx.Dp(hdRowSmall) {
		textW = max(0, textW-gtx.Dp(12)) // __left's margin-right
	}
	left := recordTo(gtx, func(gtx C) D {
		gtx.Constraints = layout.Constraints{Max: image.Pt(textW, 1<<20)}
		return col(gtx,
			layout.Rigid(func(gtx C) D { return v.historyTitleRow(gtx, h, B, fg) }),
			layout.Rigid(func(gtx C) D {
				return v.historySecondRow(gtx, h, art, B, missed, gone, threadC, filesC, sec, ter, imp)
			}),
		)
	})
	contentH := max(left.d.Size.Y, right.d.Size.Y, gtx.Dp(36))
	hgt := gtx.Dp(10) + contentH + gtx.Dp(10)
	px1 := max(1, gtx.Dp(1))
	if last {
		hgt += px1
	}
	rad := gtx.Dp(8)
	var nw, se int
	if first {
		nw = rad
	}
	if last {
		se = rad
	}
	r := image.Rect(0, 0, B, hgt)
	rowC.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		func() {
			defer clipRR(gtx, r, nw, nw, se, se).Pop()
			fill(gtx, r, line)
			in := image.Rect(px1, px1, B-px1, hgt)
			if last {
				in.Max.Y = hgt - px1
			}
			defer clipRR(gtx, in, max(0, nw-px1), max(0, nw-px1), max(0, se-px1), max(0, se-px1)).Pop()
			fill(gtx, in, bg)
		}()
		// the icon tile
		func() {
			defer opOffset(gtx, image.Pt(pad, gtx.Dp(10))).Pop()
			s := gtx.Dp(36)
			tile := flatten(argb(0x0f1d1c1d), bg)
			ic := v.tok("dt_color-content-pry")
			if v.th.Dark {
				tile = flatten(argb(0x0fe8e8e8), bg)
			}
			if missed {
				tile, ic = v.tok("dt_color-base-imp"), imp
			}
			rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(6), tile)
			func() {
				defer opOffset(gtx, image.Pt(gtx.Dp(8), gtx.Dp(8))).Pop()
				v.icon(gtx, "headphones", 20, ic)
			}()
		}()
		tx := pad + gtx.Dp(36) + gtx.Dp(12)
		func() {
			defer opOffset(gtx, image.Pt(tx, gtx.Dp(10)+(contentH-left.d.Size.Y)/2)).Pop()
			left.call.Add(gtx.Ops)
		}()
		rx := B - pad - moreW - right.d.Size.X
		func() {
			defer opOffset(gtx, image.Pt(rx, gtx.Dp(10)+(contentH-right.d.Size.Y)/2)).Pop()
			right.call.Add(gtx.Ops)
		}()
		// the ⋯ (ellipsis-vertical-filled, "More actions" above it)
		func() {
			s := moreW
			defer opOffset(gtx, image.Pt(B-pad-s, (hgt-s)/2)).Pop()
			v.iconButton(gtx, moreC, "ellipsis-vertical-filled", 28, 18, v.tok("dt_color-content-sec"))
			popups.Tooltip(gtx, v.th, v.tip(key+":more"), moreC.Hovered(), image.Pt(s, s), popups.Above, nsHuddles.T("More actions"), "")
		}()
		return D{Size: image.Pt(B, hgt)}
	})
	if !rowHoverInner(faceC, threadC, filesC, notesC, moreC) {
		// GoToTooltip: "Go to conversation" and the people
		popups.TooltipLines(gtx, v.th, v.tip(key), rowC.Hovered(), image.Pt(B, hgt), popups.Above, []popups.TipLine{
			{Text: nsHuddles.T("Go to conversation")}, {Text: v.truncatedNames(h.History), Subtitle: true},
		})
	}
	return D{Size: image.Pt(B, hgt)}
}

func rowHoverInner(cs ...*widget.Clickable) bool {
	for _, c := range cs {
		if c.Hovered() {
			return true
		}
	}
	return false
}

// historyTitleRow is createTitleText.
func (v *Huddles) historyTitleRow(gtx C, h *core.Huddle, B int, fg color.NRGBA) D {
	title, icon, isChannel := v.historyTitle(h)
	ch := roomChannel(*h)
	conv, _ := v.h.Core().Conv(ch)
	topicInChannel := h.Name != "" && (conv.Kind == "channel" || conv.Kind == "private")
	wide := B >= gtx.Dp(hdRowMedium)
	nameWithIcon := func(gtx C, name, icon string) D {
		ic := "channel-filled"
		if icon == "lock" {
			ic = "lock"
		}
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D { return v.icon(gtx, ic, 20, fg) }),
			layout.Flexed(1, func(gtx C) D { return v.label(gtx, 15, wBold, fg, name) }),
		)
	}
	switch {
	case topicInChannel:
		in := strings.TrimSpace(i18n.StripTags(nsHuddles.T("{channelTopic}<wrapper>in</wrapper>{channelName}", "channelTopic", "\x00", "channelName", "\x00")))
		in = strings.TrimSpace(strings.ReplaceAll(in, "\x00", ""))
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D { return v.label(gtx, 15, wBold, fg, title) }),
			layout.Rigid(func(gtx C) D {
				if !wide {
					return D{}
				}
				return v.label(gtx, 15, wBold, fg, " "+in+" ")
			}),
			layout.Flexed(1, func(gtx C) D {
				if !wide {
					return D{}
				}
				_, name := v.channelIconAndName(ch, h.History)
				return nameWithIcon(gtx, name, icon)
			}),
		)
	case isChannel:
		return nameWithIcon(gtx, title, icon)
	}
	return v.label(gtx, 15, wBold, fg, title)
}

// historySecondRow is the row's second line.
func (v *Huddles) historySecondRow(gtx C, h *core.Huddle, art hdArtifacts, B int, missed, gone bool,
	threadC, filesC *widget.Clickable, sec, ter, imp color.NRGBA) D {
	enh := v.enhancements()
	date := sec
	if missed {
		date = imp
	}
	small := B < gtx.Dp(hdRowSmall)
	dot := func(c color.NRGBA) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 4, Right: 4}.Layout(gtx, func(gtx C) D { return v.dot(gtx, 2, c) })
		})
	}
	kids := []layout.FlexChild{
		layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wReg, date, v.hdDateText(h.Start, gtx.Now)) }),
	}
	if !missed {
		if d := hdDurationText(h); d != "" {
			kids = append(kids, dot(ter), layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wReg, sec, d) }))
		}
	}
	if !enh && art.thread && !small {
		kids = append(kids, dot(ter), layout.Rigid(func(gtx C) D {
			return v.linkText(gtx, threadC, 13, nsHuddles.T("{replyCount, plural, =1 {# reply} other {# replies}}", "replyCount", art.replies))
		}))
	}
	if !enh && v.exp("huddles_history_files_section") && art.attachments > 0 && !small {
		kids = append(kids, dot(ter), layout.Rigid(func(gtx C) D {
			return v.linkText(gtx, filesC, 13, nsHuddles.T("{fileCount, plural, =1 {# file} other {# files}}", "fileCount", art.attachments))
		}))
	}
	if gone {
		c := ter
		if missed {
			c = imp
		}
		kids = append(kids, dot(c), layout.Rigid(func(gtx C) D {
			fg := sec
			if missed {
				fg = imp
			}
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wReg, fg, nsHuddles.T("From a private conversation")) }),
				hgap(4),
				layout.Rigid(func(gtx C) D { return v.icon(gtx, "info", 12, fg) }),
			)
		}))
	}
	if missed {
		kids = append(kids, dot(imp), layout.Rigid(func(gtx C) D {
			return v.label(gtx, 13, wReg, imp, nsHuddles.T("Missed huddle invite"))
		}))
	}
	return row(gtx, layout.Middle, kids...)
}

// historyFaces is HuddleParticipantsFacepileButton: the participants as
// 24px faces overlapping 4px (radius 6, a 2px sk_primary_background
// ring), at most three, the third with "+N" on #00000080; padding 6,
// surf-pry under the pointer; its tooltip is "See N participants" over
// "N members".
func (v *Huddles) historyFaces(gtx C, c *widget.Clickable, ids []string, bg color.NRGBA) D {
	n := min(len(ids), 3)
	if n == 0 {
		return D{}
	}
	s, step, ring, pad := gtx.Dp(24), gtx.Dp(20), gtx.Dp(2), gtx.Dp(6)
	w := pad + (n-1)*step + s + pad
	h := pad + s + pad
	d := c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		hov := c.Hovered()
		if hov {
			rrect(gtx, image.Rect(0, 0, w, h), gtx.Dp(6), v.tokOver("dt_color-surf-pry", bg))
		}
		ringC := bg
		if hov {
			ringC = v.tok("dt_color-base-sec")
		}
		for i := 0; i < n; i++ {
			func() {
				defer opOffset(gtx, image.Pt(pad+i*step, pad)).Pop()
				rrect(gtx, image.Rect(-ring, -ring, s+ring, s+ring), gtx.Dp(6)+ring, ringC)
				u, _ := v.user(ids[i])
				v.avatarRad(gtx, u.Avatar, s, gtx.Dp(6))
				if extra := len(ids) - n; i == n-1 && extra > 0 {
					label := "+" + formatInt(int64(extra))
					if extra > 9 {
						label = "9+"
					}
					rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(6), argb(0x80000000))
					g := gtx
					g.Constraints = layout.Exact(image.Pt(s, s))
					layout.Center.Layout(g, func(gtx C) D { return v.label(gtx, 12, wBold, v.tok("dt_color-constants-white"), label) })
				}
			}()
		}
		return D{Size: image.Pt(w, h)}
	})
	label := nsHuddles.T("See {memberCount, plural, =1 {participant} other {participants}}", "memberCount", len(ids))
	if v.enhancements() {
		label = nsHuddles.T("View {memberCount, plural, =1 {participant} other {participants}}", "memberCount", len(ids))
	}
	popups.TooltipLines(gtx, v.th, v.tip("faces:"+strings.Join(ids, ",")), c.Hovered(), d.Size, popups.Above, []popups.TipLine{
		{Text: label}, {Text: nsHuddles.T("{participantCount, plural, =1 {# member} other {# members}}", "participantCount", len(ids)), Subtitle: true},
	})
	return d
}

// avatarRad is an s px face with corner radius rad.
func (v *Huddles) avatarRad(gtx C, url string, s, rad int) {
	sz := image.Pt(s, s)
	if url != "" && v.h.Images() != nil {
		if img, ok := v.h.Images().Get(url, s); ok {
			themeImage(gtx, img, sz, rad)
			return
		}
	}
	v.th.Placeholder(gtx, sz, rad)
}

// artifactButton is HuddleArtifactButton: 28px, 1px otl-ter, radius 4,
// base-sec on hover; expanded (from 640) at least 52 wide with the count
// (13px bold) 4px after the 16px icon. Its tooltip is the title over the
// count.
func (v *Huddles) artifactButton(gtx C, c *widget.Clickable, icon string, count int, expanded bool, title, countText string) D {
	fg := v.tok("dt_color-content-pry")
	cs := ""
	if expanded && count >= 0 {
		cs = formatInt(int64(count))
	}
	d := c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		h := gtx.Dp(28)
		w := h
		tw := 0
		if cs != "" {
			tw = v.th.Measure(gtx, 13, wBold, cs)
			w = max(gtx.Dp(52), gtx.Dp(12)+gtx.Dp(16)+gtx.Dp(4)+tw+gtx.Dp(12))
		}
		r := image.Rect(0, 0, w, h)
		bg := v.tok("dt_color-base-pry")
		if c.Hovered() {
			bg = v.tok("dt_color-base-sec")
		}
		rrect(gtx, r, gtx.Dp(4), bg)
		border(gtx, r, gtx.Dp(4), max(1, gtx.Dp(1)), v.tokOver("dt_color-otl-ter", bg))
		x := (w - gtx.Dp(16) - tw - map[bool]int{true: gtx.Dp(4), false: 0}[tw > 0]) / 2
		func() {
			defer opOffset(gtx, image.Pt(x, (h-gtx.Dp(16))/2)).Pop()
			v.icon(gtx, icon, 16, fg)
		}()
		if cs != "" {
			func() {
				defer opOffset(gtx, image.Pt(x+gtx.Dp(16)+gtx.Dp(4), 0)).Pop()
				g := gtx
				g.Constraints = layout.Exact(image.Pt(tw, h))
				layout.W.Layout(g, func(gtx C) D { return v.label(gtx, 13, wBold, fg, cs) })
			}()
		}
		return D{Size: image.Pt(w, h)}
	})
	lines := []popups.TipLine{{Text: title}}
	if countText != "" {
		lines = append(lines, popups.TipLine{Text: countText, Subtitle: true})
	}
	popups.TooltipLines(gtx, v.th, v.tip("art:"+icon+":"+title+countText), c.Hovered(), d.Size, popups.Above, lines)
	return d
}

// notesButton is renderSmallAiSummaryButton (a 26px icon button, 1px
// surf-sec, radius 5) under 640 wide and renderLargeAiSummaryButton (28px,
// padding 0 11, the icon and the label in 13px bold) from 640.
func (v *Huddles) notesButton(gtx C, c *widget.Clickable, label string, large bool) D {
	fg := v.tok("dt_color-content-pry")
	d := c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		bg := v.tok("dt_color-base-pry")
		if large && c.Hovered() {
			bg = v.tok("dt_color-base-sec")
		}
		h, w := gtx.Dp(26), gtx.Dp(26)
		tw := 0
		if large {
			h = gtx.Dp(28)
			tw = v.th.Measure(gtx, 13, wBold, label)
			w = gtx.Dp(11) + gtx.Dp(16) + gtx.Dp(4) + tw + gtx.Dp(11)
		}
		r := image.Rect(0, 0, w, h)
		rrect(gtx, r, gtx.Dp(5), bg)
		border(gtx, r, gtx.Dp(5), max(1, gtx.Dp(1)), v.tokOver("dt_color-surf-sec", bg))
		x := (w - gtx.Dp(16)) / 2
		if large {
			x = gtx.Dp(11)
		}
		func() {
			defer opOffset(gtx, image.Pt(x, (h-gtx.Dp(16))/2)).Pop()
			v.icon(gtx, "ai-summary", 16, fg)
		}()
		if large {
			func() {
				defer opOffset(gtx, image.Pt(x+gtx.Dp(20), 0)).Pop()
				g := gtx
				g.Constraints = layout.Exact(image.Pt(tw, h))
				layout.W.Layout(g, func(gtx C) D { return v.label(gtx, 13, wBold, fg, label) })
			}()
		}
		return D{Size: image.Pt(w, h)}
	})
	if !large {
		popups.Tooltip(gtx, v.th, v.tip("notes:"+label), c.Hovered(), d.Size, popups.Above, label, "")
	}
	return d
}

// ---- the participants modal ----

// openParticipants is useOpenHuddleParticipantModal: ParticipantsModalView
// with the conversation's name as its title, "You huddled here in
// {channelName} for {huddleDuration}" (with, for a DM, group DM or a
// conversation you can't see; nothing for a missed one) and everyone who
// was there, each opening their profile.
func (v *Huddles) openParticipants(h *core.Huddle) {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	ch := roomChannel(*h)
	icon, name := v.channelIconAndName(ch, h.History)
	conv, known := v.h.Core().Conv(ch)
	v.mu.Lock()
	gone := v.missing[ch]
	v.mu.Unlock()
	dur := hdDurationText(h)
	sub := ""
	if dur != "" && name != "" && !contains(h.Missed, v.selfID()) {
		if gone || (known && (conv.Kind == "im" || conv.Kind == "mpim")) {
			sub = nsHuddles.T("You huddled here with { channelName } for { huddleDuration }", "channelName", name, "huddleDuration", dur)
		} else {
			sub = nsHuddles.T("You huddled here in { channelName } for { huddleDuration }", "channelName", name, "huddleDuration", dur)
		}
	}
	title := name
	if icon == "channel" {
		title = "#" + name
	}
	ids := append([]string(nil), h.History...)
	rows := make([]*widget.Clickable, len(ids))
	for i := range rows {
		rows[i] = new(widget.Clickable)
	}
	var m *popups.Modal
	m = popups.NewModal(ph, popups.ModalOpts{
		Title:    title,
		Subtitle: oneSpace(sub),
		Width:    520,
		Body: func(gtx C) D {
			var kids []layout.FlexChild
			for i, id := range ids {
				i, id := i, id
				if rows[i].Clicked(gtx) {
					v.h.OpenProfile(image.Point{}, id)
					m.Dismiss()
				}
				kids = append(kids, layout.Rigid(func(gtx C) D { return v.participantRow(gtx, rows[i], id) }))
			}
			return col(gtx, kids...)
		},
	})
	ph.ShowOverlay(m)
}

// participantRow is ParticipantListItem's member row: the 36px face and
// the name (useShortMode), the row highlighted under the pointer.
func (v *Huddles) participantRow(gtx C, c *widget.Clickable, uid string) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		w, h := gtx.Constraints.Max.X, gtx.Dp(44)
		if c.Hovered() {
			rrect(gtx, image.Rect(0, 0, w, h), gtx.Dp(6), v.tokOver("dt_color-base-pry-hover", v.tok("dt_color-base-pry")))
		}
		u, _ := v.user(uid)
		g := gtx
		g.Constraints = layout.Exact(image.Pt(w, h))
		layout.Inset{Left: 8, Right: 8}.Layout(g, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D { return v.avatar(gtx, u.Avatar, 36) }),
				hgap(12),
				layout.Flexed(1, func(gtx C) D { return v.label(gtx, 15, wBold, v.tok("dt_color-content-pry"), displayName(u)) }),
			)
		})
		return D{Size: image.Pt(w, h)}
	})
}

// ---- the ⋯ menu ----

// huddleLinkCopier is the ui root's copyHuddleLinkByChannelId (huddleui's
// CopyLink: rooms.getLink, the clipboard and Slack's toast).
type huddleLinkCopier interface{ CopyHuddleLink(ch string) }

// openRowMenu is HuddleHistoryOverflowMenu (bottom-left of the ⋯): "See N
// participants" (N members), under huddle_page_enhancements the thread,
// files & links and notes rows and a separator; "Save for later"; "Copy
// huddle link" (not for a conversation you can't see); "Edit huddle
// topic"; "Start a huddle from thread" (Slack's thread huddles, which
// Clack doesn't start, so the row isn't offered).
func (v *Huddles) openRowMenu(gtx C, h *core.Huddle, art hdArtifacts, anchor *widget.Clickable) {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	ch := roomChannel(*h)
	v.mu.Lock()
	gone := v.missing[ch]
	v.mu.Unlock()
	n := len(h.History)
	members := nsHuddles.T("{memberCount, plural, =1 {# member} other {# members}}", "memberCount", n)
	see := nsHuddles.T("See {memberCount, plural, =1 {participant} other {participants}}", "memberCount", n)
	enh := v.enhancements()
	if enh {
		see = nsHuddles.T("View {memberCount, plural, =1 {participant} other {participants}}", "memberCount", n)
	}
	hc := *h
	items := []popups.MenuItem{{Key: "participants", Label: see, Right: members, Do: func() { v.openParticipants(&hc) }}}
	if enh {
		if art.thread {
			items = append(items, popups.MenuItem{Key: "thread", Label: nsHuddles.T("View thread"),
				Right: nsHuddles.T("{replyCount, plural, =1 {# reply} other {# replies}}", "replyCount", art.replies),
				Do:    func() { v.h.OpenThread(ch, art.messageTS) }})
		}
		if art.files > 0 {
			items = append(items, popups.MenuItem{Key: "files", Label: nsHuddles.T("View files & links"),
				Right: nsHuddles.T("{itemCount, plural, =1 {# item} other {# items}}", "itemCount", art.files), Disabled: true})
		}
		if art.notes {
			tid := art.transcript
			items = append(items, popups.MenuItem{Key: "notes", Label: art.notesLabel, Do: func() {
				if co, ok := v.h.(interface{ OpenCanvasFull(id string) }); ok {
					co.OpenCanvasFull(tid)
				}
			}})
		}
		if art.thread || art.files > 0 || art.notes {
			items = append(items, popups.Sep())
		}
	}
	items = append(items, popups.MenuItem{Key: "save", Label: nsHuddles.T("Save for later"), Do: func() { v.saveForLater(ch, hc.ThreadTS) }})
	if !gone {
		if lc, ok := v.h.(huddleLinkCopier); ok {
			items = append(items, popups.MenuItem{Key: "copy-link", Label: nsHuddles.T("Copy huddle link"), Do: func() { lc.CopyHuddleLink(ch) }})
		}
	}
	if title, _, _ := v.historyTitle(h); title != "" {
		items = append(items, popups.MenuItem{Key: "edit-topic", Label: nsHuddles.T("Edit huddle topic"), Do: func() { v.openEditTopic(hc) }})
	}
	at := v.buttonRect(anchor, image.Pt(gtx.Dp(28), gtx.Dp(28)))
	ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: at, Width: 300, Popover: "bottom-left",
		MenuClass: "p-huddles_history_overflow_menu"}))
}

// buttonRect is a clicked button's rectangle in the window, for
// anchoring its menu: the pointer less where the press landed in it.
func (v *Huddles) buttonRect(c *widget.Clickable, size image.Point) image.Rectangle {
	var at image.Point
	if pp, ok := v.h.(interface{ PointerPos() image.Point }); ok {
		at = pp.PointerPos()
	}
	if hs := c.History(); len(hs) > 0 {
		at = at.Sub(hs[len(hs)-1].Position)
	}
	return image.Rectangle{Min: at, Max: at.Add(size)}
}

// saveForLater is the menu's Save for later: saved.add for the huddle
// message (saveMessageForLater) and Slack's "Saved for later" toast.
func (v *Huddles) saveForLater(ch, ts string) {
	if ch == "" || ts == "" {
		return
	}
	app := v.h.Core()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := app.Save(ctx, ch, ts, 0); err != nil {
			return
		}
		v.h.Toast(nsHuddles.T("Saved for later"))
	}()
}

// ---- Edit Topic ----

// hdTopicLimit is HuddlesEditTopicModal's CHAR_LIMIT.
const hdTopicLimit = 250

// openEditTopic is HuddlesEditTopicModal: "Edit Topic", the "New topic"
// field (placeholder "Enter new topic", "The topic is visible to everyone
// in the huddle" under it, or past 250 characters "The topic can’t be
// longer than 250 characters."), Cancel and Save; Save (and Enter) sends
// huddles.update {huddle_id, topic} (sendNewTopic), closes and fetches
// the history again.
func (v *Huddles) openEditTopic(h core.Huddle) {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	ed := &widget.Editor{SingleLine: true, Submit: true}
	ed.SetText(h.Name)
	focused := false
	var busy, done atomic.Bool
	var m *popups.Modal
	valid := func() bool {
		t := strings.TrimSpace(ed.Text())
		return t != "" && utf8.RuneCountInString(ed.Text()) <= hdTopicLimit && !busy.Load()
	}
	save := func() {
		if !valid() {
			return
		}
		busy.Store(true)
		topic := strings.TrimSpace(ed.Text())
		app := v.h.Core()
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := app.UpdateHuddleTopic(ctx, h.ID, topic); err == nil {
				done.Store(true)
			}
			busy.Store(false)
			v.h.Invalidate()
		}()
	}
	m = popups.NewModal(ph, popups.ModalOpts{
		Title: nsHuddles.T("Edit Topic"),
		Body: func(gtx C) D {
			if done.Load() {
				// saved: close, and fetchHuddleHistory({}) again
				v.mu.Lock()
				v.hist.huddles, v.hist.cursor = nil, ""
				v.mu.Unlock()
				v.mounted = false
				m.Dismiss()
			}
			if !focused {
				gtx.Execute(key.FocusCmd{Tag: ed})
				focused = true
			}
			for {
				ev, ok := ed.Update(gtx)
				if !ok {
					break
				}
				if _, ok := ev.(widget.SubmitEvent); ok {
					save()
				}
			}
			return v.topicField(gtx, ed)
		},
		FocusStops: func() []event.Tag { return []event.Tag{ed} },
		Buttons: []*popups.ModalButton{
			{Label: nsHuddles.T("Cancel"), Kind: popups.BtnOutline},
			{Label: nsHuddles.T("Save"), Kind: popups.BtnPrimary, Enabled: valid, Do: func() bool { save(); return false }},
		},
	})
	ph.ShowOverlay(m)
}

// topicField is the modal's "New topic" input (medium: 36px, 1px otl-sec,
// radius 8, the focus ring) with its hint or error below.
func (v *Huddles) topicField(gtx C, ed *widget.Editor) D {
	fg := v.tok("dt_color-content-pry")
	tooLong := utf8.RuneCountInString(ed.Text()) > hdTopicLimit
	return col(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D { return v.label(gtx, 15, wBold, fg, nsHuddles.T("New topic")) })
		}),
		layout.Rigid(func(gtx C) D {
			w, h := gtx.Constraints.Max.X, gtx.Dp(36)
			r := image.Rect(0, 0, w, h)
			rad := gtx.Dp(8)
			bg := v.tok("dt_color-base-pry")
			rrect(gtx, r, rad, bg)
			switch {
			case tooLong:
				border(gtx, r, rad, max(1, gtx.Dp(1)), v.tok("dt_color-content-imp"))
			case gtx.Focused(ed):
				border(gtx, r.Inset(-gtx.Dp(2)), rad+gtx.Dp(2), gtx.Dp(3), v.p.Focus)
				border(gtx, r, rad, max(1, gtx.Dp(1)), v.tok("dt_color-otl-hgl-1"))
			default:
				border(gtx, r, rad, max(1, gtx.Dp(1)), v.tokOver("dt_color-otl-sec", bg))
			}
			g := gtx
			g.Constraints = layout.Exact(image.Pt(w, h))
			layout.Inset{Left: 12, Right: 12}.Layout(g, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.Stack{Alignment: layout.W}.Layout(gtx,
					layout.Stacked(func(gtx C) D {
						if ed.Len() > 0 {
							return D{}
						}
						return v.label(gtx, 15, wReg, v.tok("dt_color-content-ter"), nsHuddles.T("Enter new topic"))
					}),
					layout.Stacked(func(gtx C) D { return v.th.Editor(gtx, ed, 15, fg, v.p.Mention) }),
				)
			})
			return D{Size: image.Pt(w, h)}
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D {
				if tooLong {
					return v.para(gtx, 13, 18, wReg, v.tok("dt_color-content-imp"), 0,
						nsHuddles.T("The topic can’t be longer than {CHAR_LIMIT} characters.", "CHAR_LIMIT", hdTopicLimit))
				}
				return v.para(gtx, 13, 18, wReg, v.tok("dt_color-content-sec"), 0, nsHuddles.T("The topic is visible to everyone in the huddle"))
			})
		}),
	)
}
