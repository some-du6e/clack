package blockkit

// The card look of Slack's plan and task_card blocks: what its client
// draws while agent_streaming_slimmer_task_cards is off or not assigned
// (n7nl: slim mode only when the group is "on"; agent.go draws slim).
// From the web client (CDN build 4d0c78085232, 2026-11-10): PlanBlock
// (E5Xj), TaskCard (wdtw), ExpandableContent (qWkG), ToggleBar (uPDO),
// TaskItem (SkmW), TaskIcon (6G6y), Title (pwIw), AiShimmerText (cllB),
// the status icons (gXOk) and the CSS modules kFCn / Dl0Z:
//
//	.block                 theme-surf-ter on 1px otl-ter, radius 12, width
//	                       100% up to 480px, margin 8px 0, overflow hidden;
//	                       ExpandableContent's height: a collapsed plan is
//	                       40px (initialHeight), a task card fits its bar,
//	                       an open one its content; .25s height transition
//	.toggleBarButton       flex, align flex-start, min-height 30px, padding
//	                       0 12px, radius 12; surf-pry on hover while it
//	                       opens something
//	.toggleBarIcon         18 × 40, centred: the large InfiniteSpinner (24px,
//	                       theme-base-pry) in progress, else TaskIcon primary
//	                       at 18px (check-circle-filled; warning-filled in
//	                       content-sec); the caret's box at the end (caret-up
//	                       open, caret-down closed, 18px)
//	.toggleBarTitleContainer flex 1, margin 8px: the title, .headerTitle 15px
//	                       bold content-pry, line-height 1.5, margin-right
//	                       8px; AiShimmerText while in progress
//	.blockContent          padding 0 4px; text at the message's size
//	.taskItem              padding 1px; a plan task's .taskHeader (margin 0
//	                       8px, padding-left 1px): .taskIcon 20 × 20 (margin
//	                       2px 0; hourglass, ai-sparkle-filled shimmering in
//	                       theme-base-pry, check-filled, warning-filled in
//	                       content-sec) and .subheaderTitle (15px bold,
//	                       margin 2px 0 2px 2px, padding 0 2px)
//	.taskContentSection    details: margin 4px 12px, padding 0 20px, a 4px
//	                       base-ter bar (radius 8) at left 2px
//	.taskContent           margin 2px 0 8px, flex: with output,
//	                       .taskContentIcons (margin 8px 16px 0: the 8px
//	                       placeholder dot in content-ter, padding-bottom 2px,
//	                       over a 4px base-ter divider, margin-top 8px) beside
//	                       .taskContentBody (the output, margin-bottom 4px,
//	                       then the sources); without output it is a
//	                       .taskContentSection too
//
// Plan tasks have no toggle here (TaskItem collapses only slim plan
// tasks): their content always shows. Under plan_block_coding_enhancements
// in-progress icons lose the theme-base-pry colour and the shimmer.

import (
	"image"
	"image/color"
	"math"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// slimTaskCards is agent_streaming_slimmer_task_cards (n7nl.n). Without a
// core (offline previews) the blocks keep the test account's group, slim.
func (me *Message) slimTaskCards() bool {
	c := me.h.Core()
	if c == nil {
		return true
	}
	return c.Experiment("agent_streaming_slimmer_task_cards") == "on"
}

// planCodingEnhancements is plan_block_coding_enhancements (FiNg.XI).
func (me *Message) planCodingEnhancements() bool {
	c := me.h.Core()
	return c != nil && c.Experiment("plan_block_coding_enhancements") == "on"
}

// noAnimations is Accessibility > "Allow animations" off (a11y_animations
// false): AiShimmerText draws plain text.
func (me *Message) noAnimations() bool {
	c := me.h.Core()
	return c != nil && c.S != nil && c.Prefs().NoAnimations
}

// cardPal is the card's design tokens (light / dark).
type cardPal struct {
	surfTer, otlTer, surfPry, baseTer color.NRGBA
	themePry, themeSec, pry, sec, ter color.NRGBA
	link                              color.NRGBA
}

func cardColors(th *theme.Theme, pal agentPal) cardPal {
	v := css.SlackVars(th.Dark)
	tok := func(name string) color.NRGBA {
		c, _ := css.ParseColor(v["--dt_color-"+name])
		return c
	}
	return cardPal{surfTer: tok("theme-surf-ter"), otlTer: tok("otl-ter"), surfPry: tok("surf-pry"), baseTer: tok("base-ter"),
		themePry: tok("theme-base-pry"), themeSec: tok("theme-base-sec"), pry: tok("content-pry"), sec: pal.sec, ter: pal.ter,
		link: pal.link}
}

// layoutTaskCard draws a plan or task_card as Slack's card.
func (me *Message) layoutTaskCard(gtx C, st *agentState, b *AgentBlock, first bool, w int) image.Point {
	th := me.h.Theme()
	pal := agentColors(th)
	cp := cardColors(th, pal)
	top := dp(gtx, 4) // margin 8px; richview's block gap gives the other 4
	if first {
		top = dp(gtx, 8) // only slim blocks drop it on the first block
	}
	bottom := dp(gtx, 4)
	key := b.Type + "\x00" + b.BlockID
	var status, title string
	var hasContent bool
	def := false
	if b.Type == "plan" {
		status, title, hasContent = PlanStatus(b.Tasks), PlanTitle(b), len(b.Tasks) > 0
	} else {
		status, title, hasContent = CardStatus(b.Status), b.Title, b.hasContent()
		def = strings.HasPrefix(b.BlockID, "mcp_task_")
	}
	open := hasContent && st.isOpen(key, def)
	t := st.tap(key)
	if hasContent && t.clicked(gtx, me.h) {
		st.open[key] = !open
		open = !open
		st.anim[key] = &agentAnim{from: st.shown[key], start: gtx.Now}
		me.changed()
	}
	cw := min(w, dp(gtx, 480))
	bw := max(dp(gtx, 1), 1)
	inner := max(cw-2*bw, 1)
	m := op.Record(gtx.Ops)
	barH := me.cardBar(gtx, cp, t, status, title, hasContent, open, inner)
	content := barH
	if open {
		pad := dp(gtx, 4) // .blockContent padding 0 4px
		h := at(gtx, image.Pt(pad, barH), func(gtx C) D {
			var ch int
			if b.Type == "plan" {
				ch = me.cardPlanTasks(gtx, st, cp, b, inner-2*pad)
			} else {
				ch = me.cardTaskBody(gtx, st, cp, "card\x00"+b.BlockID, &b.AgentTask, inner-2*pad)
			}
			return D{Size: image.Pt(inner-2*pad, ch)}
		}).Size.Y
		content += h
	}
	call := m.Stop()
	target := content
	if !open && b.Type == "plan" {
		target = dp(gtx, 40) // initialHeight; a plan doesn't fit its collapsed height to the bar
	}
	h := me.animated(gtx, st, key, min(barH, target), target)
	st.shown[key] = h
	r := image.Rect(0, top, cw, top+h+2*bw)
	rad := dp(gtx, 12)
	rrect(gtx, r, rad, cp.surfTer) // translucent: over the message row, hovered or not
	theme.Border(gtx, r, rad, bw, cp.otlTer)
	inside := r.Inset(bw)
	cl := clip.UniformRRect(inside, max(rad-bw, 0)).Push(gtx.Ops)
	off := op.Offset(inside.Min).Push(gtx.Ops)
	call.Add(gtx.Ops)
	off.Pop()
	cl.Pop()
	return image.Pt(w, top+h+2*bw+bottom)
}

// cardBar is ToggleBar outside slim mode; it returns its height.
func (me *Message) cardBar(gtx C, cp cardPal, t *tap, status, title string, hasContent, open bool, w int) int {
	th := me.h.Theme()
	padX, iconW, box := dp(gtx, 12), dp(gtx, 18), dp(gtx, 40)
	caretW := 0
	if hasContent {
		caretW = iconW
	}
	titleX := padX + iconW + dp(gtx, 8)
	titleW := max(w-titleX-dp(gtx, 8)-dp(gtx, 8)-caretW-padX, 1) // container margin 8, headerTitle margin-right 8
	lines := wrapWeight(gtx, th, 15, theme.Bold, title, titleW)
	lh := dp(gtx, 22.5) // 15px × 1.5
	n := max(len(lines), 1)
	h := max(box, dp(gtx, 8)+n*lh+dp(gtx, 8), dp(gtx, 30))
	if hasContent && t.g.Hovered() {
		rrect(gtx, image.Rect(0, 0, w, h), dp(gtx, 12), cp.surfPry)
	}
	// the status icon, centred in its 18 × 40 box
	switch status {
	case StatusInProgress:
		s := dp(gtx, 24)
		at(gtx, image.Pt(padX+(iconW-s)/2, (box-s)/2), func(gtx C) D { cardSpinner(gtx, s, cp.themePry); return D{} })
	case StatusError:
		at(gtx, image.Pt(padX, (box-iconW)/2), func(gtx C) D { return agentIcon(gtx, "warning-filled", 18, cp.sec) })
	default:
		at(gtx, image.Pt(padX, (box-iconW)/2), func(gtx C) D { return agentIcon(gtx, "check-circle-filled", 18, cp.pry) })
	}
	shimmer := status == StatusInProgress && !me.noAnimations()
	y := dp(gtx, 8)
	for i, ln := range lines {
		at(gtx, image.Pt(titleX, y), func(gtx C) D {
			return vcenter(gtx, lh, func(gtx C) D {
				if shimmer {
					return me.shimmerText(gtx, ln, i, lines, cp)
				}
				return label(gtx, th, 15, theme.Bold, cp.pry, ln)
			})
		})
		y += lh
	}
	if hasContent {
		name := "caret-down"
		if open {
			name = "caret-up"
		}
		at(gtx, image.Pt(w-padX-iconW, (box-iconW)/2), func(gtx C) D { return agentIcon(gtx, name, 18, cp.pry) })
		t.add(gtx, image.Pt(w, h))
	}
	if shimmer {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(33 * time.Millisecond)})
	}
	return h
}

// cardPlanTasks is PlanTaskList outside slim mode: TaskItems with their
// header, details and content, all open.
func (me *Message) cardPlanTasks(gtx C, st *agentState, cp cardPal, b *AgentBlock, w int) int {
	th := me.h.Theme()
	enh := me.planCodingEnhancements()
	y := 0
	for i := range b.Tasks {
		task := &b.Tasks[i]
		key := "task\x00" + b.BlockID + "\x00" + task.TaskID + "\x00" + itoa(i)
		// .taskItem padding 1px; .taskHeader margin 0 8px, padding-left 1px
		itemX := dp(gtx, 1)
		x := itemX + dp(gtx, 8) + dp(gtx, 1)
		iy := y + dp(gtx, 1)
		lh := dp(gtx, 22)
		tx := x + dp(gtx, 20) + dp(gtx, 2) + dp(gtx, 2) // the 20px icon, then the title's margin-left 2px and padding 2px
		lines := wrapWeight(gtx, th, 15, theme.Bold, task.Title, max(w-tx-dp(gtx, 2)-dp(gtx, 9), 1))
		n := max(len(lines), 1)
		head := max(dp(gtx, 2)+dp(gtx, 20)+dp(gtx, 2), dp(gtx, 2)+n*lh+dp(gtx, 2))
		at(gtx, image.Pt(x, iy+dp(gtx, 2)), func(gtx C) D { return me.cardTaskIcon(gtx, cp, task.Status, enh) })
		ty := iy + dp(gtx, 2)
		for _, ln := range lines {
			at(gtx, image.Pt(tx, ty), func(gtx C) D {
				return vcenter(gtx, lh, func(gtx C) D { return label(gtx, th, 15, theme.Bold, cp.pry, ln) })
			})
			ty += lh
		}
		h := dp(gtx, 1) + head
		h += me.cardTaskBodyAt(gtx, st, cp, key, task, itemX, iy+head, w-2*itemX)
		y += h + dp(gtx, 1)
	}
	return y
}

// cardTaskIcon is TaskIcon (secondary) outside slim mode, in its 20 × 20
// box: hourglass, ai-sparkle-filled (shimmering, theme-base-pry, until
// plan_block_coding_enhancements), check-filled, warning-filled
// (content-sec), 15px (size inherit).
func (me *Message) cardTaskIcon(gtx C, cp cardPal, status string, enh bool) D {
	box := dp(gtx, 20)
	name, col := "check-filled", cp.pry
	switch status {
	case StatusPending:
		name = "hourglass"
	case StatusInProgress:
		name = "ai-sparkle-filled"
		if !enh {
			col = cp.themePry
			if !me.noAnimations() {
				col = shimmerColor(gtx.Now, 0, 2, cp.themePry, cp.themeSec)
				gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(33 * time.Millisecond)})
			}
		}
	case StatusError:
		name, col = "warning-filled", cp.sec
	}
	s := dp(gtx, 15)
	at(gtx, image.Pt((box-s)/2, (box-s)/2), func(gtx C) D { return agentIcon(gtx, name, 15, col) })
	return D{Size: image.Pt(box, box)}
}

// cardTaskBody is a task_card's TaskItem (no header) at the origin.
func (me *Message) cardTaskBody(gtx C, st *agentState, cp cardPal, key string, t *AgentTask, w int) int {
	return me.cardTaskBodyAt(gtx, st, cp, key, t, 0, 0, w)
}

// cardTaskBodyAt draws a TaskItem's details section and content row at
// (x, y), w wide, and returns their height (margins included; adjacent
// vertical margins collapse).
func (me *Message) cardTaskBodyAt(gtx C, st *agentState, cp cardPal, key string, t *AgentTask, x, y, w int) int {
	h := 0
	prevMargin := 0
	place := func(marginTop, marginBottom int, draw func(y int) int) {
		gap := max(prevMargin, marginTop)
		if h == 0 {
			gap = marginTop
		}
		ch := draw(y + h + gap)
		h += gap + ch
		prevMargin = marginBottom
	}
	secX, secW := x+dp(gtx, 12), max(w-2*dp(gtx, 12), 1) // .taskContentSection margin 4px 12px
	bar := func(top, height int) {
		rrect(gtx, image.Rect(secX+dp(gtx, 2), top, secX+dp(gtx, 2)+dp(gtx, 4), top+height), dp(gtx, 8), cp.baseTer)
	}
	if isRichText(t.Details) {
		place(dp(gtx, 4), dp(gtx, 4), func(y int) int {
			tv := me.agentRich(st, key+"\x00d", t.Details)
			tw := max(secW-2*dp(gtx, 20), 1)
			ch := at(gtx, image.Pt(secX+dp(gtx, 20), y), func(gtx C) D { return D{Size: image.Pt(tw, tv.layout(gtx, tw, cp.pry))} }).Size.Y
			bar(y, ch)
			return ch
		})
	}
	hasOut := isRichText(t.Output)
	if hasOut {
		place(dp(gtx, 2), dp(gtx, 8), func(y int) int {
			// .taskContentIcons: margin 8px 16px 0 around the 8px dot, then the divider
			iconX := x + dp(gtx, 16)
			bx := iconX + dp(gtx, 8) + dp(gtx, 16)
			bwid := max(x+w-bx, 1)
			tv := me.agentRich(st, key+"\x00o", t.Output)
			ch := at(gtx, image.Pt(bx, y), func(gtx C) D { return D{Size: image.Pt(bwid, tv.layout(gtx, bwid, cp.pry))} }).Size.Y
			ch += dp(gtx, 4) // .taskContentOutput margin-bottom
			if len(t.Sources) > 0 {
				ch += me.cardSources(gtx, st, cp, key, t.Sources, bx, y+ch, bwid)
			}
			dy := y + dp(gtx, 8)
			at(gtx, image.Pt(iconX, dy), func(gtx C) D { agentIconPx(gtx, "icon-placeholder-filled", dp(gtx, 8), cp.ter); return D{} })
			ly := dy + dp(gtx, 8) + dp(gtx, 2) + dp(gtx, 8)
			if ly < y+ch {
				cx := iconX + (dp(gtx, 8)-dp(gtx, 4))/2
				rrect(gtx, image.Rect(cx, ly, cx+dp(gtx, 4), y+ch), dp(gtx, 8), cp.baseTer)
			}
			return max(ch, ly-y)
		})
	} else {
		// no output: .taskContent is a section too (the empty output, then the sources)
		place(dp(gtx, 4), dp(gtx, 4), func(y int) int {
			tw := max(secW-2*dp(gtx, 20), 1)
			ch := dp(gtx, 4) // the empty .taskContentOutput's margin-bottom
			if len(t.Sources) > 0 {
				ch += me.cardSources(gtx, st, cp, key, t.Sources, secX+dp(gtx, 20), y+ch, tw)
			}
			bar(y, ch)
			return ch
		})
	}
	return h + prevMargin
}

// cardSources lists a task's sources outside slim mode: a link per line
// at the message's size.
func (me *Message) cardSources(gtx C, st *agentState, cp cardPal, key string, ss []AgentSource, x, y, w int) int {
	th := me.h.Theme()
	lh := dp(gtx, 22)
	h := 0
	for i, s := range ss {
		txt := strings.TrimSpace(s.Text)
		if txt == "" {
			txt = s.URL
		}
		u := richtext.SafeURL(s.URL)
		tp := st.tap(key + "\x00src" + itoa(i))
		if u != "" && tp.clicked(gtx, me.h) {
			openURL(u)
		}
		lines := wrapWeight(gtx, th, 15, font.Normal, txt, w)
		for j, ln := range lines {
			d := at(gtx, image.Pt(x, y+h), func(gtx C) D {
				return vcenter(gtx, lh, func(gtx C) D { return label(gtx, th, 15, font.Normal, cp.link, ln) })
			})
			if u != "" && j == 0 {
				at(gtx, image.Pt(x, y+h), func(gtx C) D {
					tp.add(gtx, image.Pt(max(d.Size.X, 1), lh*len(lines)))
					return D{}
				})
			}
			if tp.g.Hovered() {
				rrect(gtx, image.Rect(x, y+h+lh-dp(gtx, 3), x+d.Size.X, y+h+lh-dp(gtx, 2)), 0, cp.link)
			}
			h += lh
		}
	}
	return h
}

// wrapWeight is wrapText for any weight.
func wrapWeight(gtx C, th *theme.Theme, size float32, wt font.Weight, s string, w int) []string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if s == "" {
		return nil
	}
	if measure(gtx, th, size, wt, s) <= w {
		return []string{s}
	}
	var lines []string
	cur := ""
	for _, wd := range strings.Fields(s) {
		next := wd
		if cur != "" {
			next = cur + " " + wd
		}
		if cur != "" && measure(gtx, th, size, wt, next) > w {
			lines = append(lines, cur)
			cur = wd
			continue
		}
		cur = next
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// shimmerText is AiShimmerText for one line of a (wrapped) title: every
// character's colour runs theme-base-pry → theme-base-sec → theme-base-pry
// over .8s (easeInOut), started .8·i·.5/n s after the first and repeated
// after .05·n/2 s, n being the title's characters (ShimmerAnimation).
func (me *Message) shimmerText(gtx C, ln string, lineIdx int, lines []string, cp cardPal) D {
	th := me.h.Theme()
	n := 0
	offset := 0
	for i, l := range lines {
		n += len([]rune(l))
		if i < lineIdx {
			offset += len([]rune(l))
		}
	}
	x := 0
	h := 0
	for i, r := range []rune(ln) {
		ch := string(r)
		col := shimmerColor(gtx.Now, offset+i, n, cp.themePry, cp.themeSec)
		d := at(gtx, image.Pt(x, 0), func(gtx C) D { return label(gtx, th, 15, theme.Bold, col, ch) })
		x = measure(gtx, th, 15, theme.Bold, string([]rune(ln)[:i+1])) // the prefix's width keeps the line's spacing
		h = max(h, d.Size.Y)
	}
	return D{Size: image.Pt(x, h)}
}

// shimmerColor is ShimmerAnimation's colour for character i of n at now.
func shimmerColor(now time.Time, i, n int, from, to color.NRGBA) color.NRGBA {
	if n <= 0 {
		return from
	}
	const dur = 0.8
	delay := dur * float64(i) * 0.5 / float64(n)
	period := dur + 0.05*float64(n)/2
	t := float64(now.UnixNano())/1e9 - delay
	if t < 0 {
		return from
	}
	ph := math.Mod(t, period)
	if ph >= dur {
		return from
	}
	p := ph / dur // keyframes [from, to, from], easeInOut on each half
	var f float64
	if p < 0.5 {
		f = easeInOut(p / 0.5)
	} else {
		f = 1 - easeInOut((p-0.5)/0.5)
	}
	mix := func(a, b uint8) uint8 { return uint8(math.Round(float64(a) + (float64(b)-float64(a))*f)) }
	return color.NRGBA{R: mix(from.R, to.R), G: mix(from.G, to.G), B: mix(from.B, to.B), A: mix(from.A, to.A)}
}

// easeInOut is framer-motion's easeInOut (cubic-bezier(.42, 0, .58, 1)).
func easeInOut(x float64) float64 { return cubicBezier(0.42, 0, 0.58, 1, x) }

// cardSpinner is InfiniteSpinner with color inherit: a .25-opacity track
// and two 90° arcs in col, once around per 1.2s.
func cardSpinner(gtx C, d int, col color.NRGBA) {
	sc := float32(d) / 100
	r := 35 * sc
	sw := 8 * sc
	ctr := f32.Pt(float32(d)/2, float32(d)/2)
	arc := func(a0, sweep float32, c color.NRGBA) {
		var p clip.Path
		p.Begin(gtx.Ops)
		p.MoveTo(f32.Pt(ctr.X+r*float32(math.Cos(float64(a0))), ctr.Y+r*float32(math.Sin(float64(a0)))))
		p.ArcTo(ctr, ctr, sweep)
		s := clip.Stroke{Path: p.End(), Width: sw}.Op().Push(gtx.Ops)
		paint.ColorOp{Color: c}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		s.Pop()
	}
	track := col
	track.A = uint8(float32(col.A)*.25 + .5)
	arc(0, 2*math.Pi-0.001, track)
	t := float64(gtx.Now.UnixMilli()%1200) / 1200
	dash := float32(55.0 / 219.9 * 2 * math.Pi)
	base := float32(-90.0/219.9*2*math.Pi) - float32(math.Pi/4)
	arc(base+float32(2*math.Pi*cubicBezier(.25, .29, .54, .86, t)), dash, col)
	arc(base+float32(2*math.Pi*cubicBezier(.41, .24, .64, .69, t)), dash, col)
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(33 * time.Millisecond)})
}
