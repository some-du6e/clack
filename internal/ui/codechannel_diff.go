package ui

// The Code tab (Slack's CodeTabView, module lu3W, and the patch renderer
// Afoo), drawn in the code channel details pane: a code channel's diff
// (client.codeChannels.getDiff) or, for a pull_request view, the PR
// (client.codeChannels.getPullRequest). Sizes and colours are Slack's
// stylesheet (client-boot-styles: .patchTable__UELw3, .fileGroup__Ne7g2,
// .summaryLine__ku0Xz, …); docs/CODE-CHANNELS.md has the evidence.
//
// Branch mode (getDiff): the summary line ("Comparing {base} ← {head}" or
// "Branch {base}", "# commits", "# files changed", +additions −deletions),
// then a group per file (generated files last), each with a sticky header
// (toggle, path, copy, "Generated", +/−) and its patch: unified, 100 lines
// then "Show N more lines". Files past Slack's caps (15 files / 500
// lines, generated, >500 changed lines) start collapsed.
//
// PR mode adds the status pills, the Conversation / Files Changed
// subtabs, the files toolbar (file tree toggle, "Diff display options":
// Unified / Split, Hide whitespace), the file tree (container ≥ 720px) and
// review comments under their lines.

import (
	"context"
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// codeMono is .monospace__lFFWw: "SF Mono", var(--font-family-monospace).
var codeMono = font.Font{Typeface: font.Typeface("SF Mono, " + theme.MonoTypeface + ", Monaco, Menlo, Consolas, \"Courier New\", monospace")}

// the patch's colours (.patchLineAdded__fHq5q etc., the same in both modes)
var (
	patchAddBg    = color.NRGBA{0x2e, 0xa0, 0x43, 0x26}
	patchAddNumBg = color.NRGBA{0x2e, 0xa0, 0x43, 0x40}
	patchDelBg    = color.NRGBA{0xf8, 0x51, 0x49, 0x26}
	patchDelNumBg = color.NRGBA{0xf8, 0x51, 0x49, 0x40}
)

// codeDiffView is one CodeTabView.
type codeDiffView struct {
	a     *App
	ch    string
	prURL string // "" = the channel's branch diff

	started, done bool
	err           error
	diff          *core.CodeDiff
	pr            *core.CodePR
	byFile        map[string]map[string][]core.CodePRComment
	unanchored    []core.CodePRComment

	list      layout.List
	treeList  layout.List
	clicks    clickables
	collapsed map[string]bool // auto-collapsed, then the user's toggles
	showAll   map[string]bool // "Show N more lines"
	split     bool            // code-channels-diff-layout
	hideWS    bool            // code-channels-diff-hide-whitespace
	treeOff   bool            // code-channels-diff-file-tree (default shown)
	subtab    string          // conversation | files
	copied    string          // the file whose path was just copied
	copiedAt  time.Time
	jump      string // a file the tree asked to scroll to

	parsed map[string][]core.PatchLine
	toks   map[string][]core.CodeToken
	cw     int     // the mono font's advance at the current scale
	cwPx   float32 // the scale cw was measured at
}

func newCodeDiffView(a *App, ch, prURL string) *codeDiffView {
	v := &codeDiffView{a: a, ch: ch, prURL: prURL, clicks: clickables{}, collapsed: map[string]bool{}, showAll: map[string]bool{},
		parsed: map[string][]core.PatchLine{}, toks: map[string][]core.CodeToken{}, subtab: "conversation"}
	v.list.Axis = layout.Vertical
	v.treeList.Axis = layout.Vertical
	if prURL == "" {
		v.subtab = "files"
	}
	return v
}

func (v *codeDiffView) load() {
	v.started = true
	a := v.a
	if a.data == nil || a.data.core == nil {
		v.done, v.err = true, context.Canceled
		return
	}
	ca := a.data.core
	ch, pr := v.ch, v.prURL
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var d *core.CodeDiff
		var p *core.CodePR
		var err error
		if pr != "" {
			p, err = ca.CodeChannelPullRequest(ctx, ch, pr)
			if p != nil {
				d = &p.Diff
			}
		} else {
			d, err = ca.CodeChannelDiff(ctx, ch)
		}
		a.Post(func() {
			v.done, v.err, v.diff, v.pr = true, err, d, p
			if d != nil {
				commented := map[string]bool{}
				if p != nil {
					v.byFile, v.unanchored = core.AnchoredComments(p.Comments, d.Files)
					for f := range v.byFile {
						commented[f] = true
					}
				}
				v.collapsed = core.AutoCollapsed(core.OrderFiles(d.Files, v.prMode()), commented)
			}
			a.Invalidate()
		})
	}()
}

// prMode is Slack's nu: the PR layout (subtabs, file tree, layout menu).
func (v *codeDiffView) prMode() bool { return v.prURL != "" && v.pr != nil }

func (v *codeDiffView) files() []core.CodeDiffFile {
	if v.diff == nil {
		return nil
	}
	return core.OrderFiles(v.diff.Files, v.prMode())
}

func (v *codeDiffView) lines(f core.CodeDiffFile) []core.PatchLine {
	k := f.Filename + "\x00" + f.Patch
	ls, ok := v.parsed[k]
	if !ok {
		ls = core.ParsePatch(f.Patch)
		v.parsed[k] = ls
	}
	if v.hideWS && v.prMode() {
		return core.HideWhitespace(ls)
	}
	return ls
}

// item is one row of the scroller.
type diffItem struct {
	kind int
	file int // index into files()
	line int // line index (unified) or split row index
	key  string
	n    int
}

const (
	diSummary = iota
	diPRHeader
	diSubtabs
	diDescription
	diComment
	diToolbar
	diFileHead
	diLine
	diSplit
	diAnchored
	diMore
	diFileEnd
	diTruncated
	diPad
)

func (v *codeDiffView) items(files []core.CodeDiffFile) []diffItem {
	var out []diffItem
	if v.prMode() {
		out = append(out, diffItem{kind: diPRHeader})
	}
	out = append(out, diffItem{kind: diSummary})
	if v.prMode() {
		out = append(out, diffItem{kind: diSubtabs})
		if v.subtab == "conversation" {
			out = append(out, diffItem{kind: diDescription})
			for i := range v.unanchored {
				out = append(out, diffItem{kind: diComment, n: i})
			}
			return append(out, diffItem{kind: diPad})
		}
		if len(files) > 0 {
			out = append(out, diffItem{kind: diToolbar})
		}
	}
	for fi, f := range files {
		out = append(out, diffItem{kind: diFileHead, file: fi})
		if v.collapsed[f.Filename] || f.Patch == "" {
			out = append(out, diffItem{kind: diFileEnd, file: fi})
			continue
		}
		ls := v.lines(f)
		limit := len(ls)
		if !v.showAll[f.Filename] && limit > core.PatchCap {
			limit = core.PatchCap
		}
		comments := v.byFile[f.Filename]
		anchored := func(i int) {
			l := ls[i]
			keys := []string{l.Key()}
			if l.Type == core.PatchContext {
				keys = append(keys, "L:"+itoa(l.OldLine))
			}
			for _, k := range keys {
				if len(comments[k]) > 0 {
					out = append(out, diffItem{kind: diAnchored, file: fi, key: k})
				}
			}
		}
		if v.split && v.prMode() {
			for ri, r := range core.SplitRows(ls[:limit]) {
				out = append(out, diffItem{kind: diSplit, file: fi, line: ri})
				if !r.Hunk {
					for _, i := range dedupe(r.Left, r.Right) {
						anchored(i)
					}
				}
			}
		} else {
			for i := 0; i < limit; i++ {
				out = append(out, diffItem{kind: diLine, file: fi, line: i})
				if ls[i].Type != core.PatchHunk {
					anchored(i)
				}
			}
		}
		if limit < len(ls) {
			out = append(out, diffItem{kind: diMore, file: fi, n: len(ls) - limit})
		}
		out = append(out, diffItem{kind: diFileEnd, file: fi})
	}
	if v.diff != nil && v.diff.Truncated {
		out = append(out, diffItem{kind: diTruncated})
	}
	return append(out, diffItem{kind: diPad})
}

func dedupe(l, r int) []int {
	switch {
	case l >= 0 && r >= 0 && l != r:
		return []int{l, r}
	case l >= 0:
		return []int{l}
	case r >= 0:
		return []int{r}
	}
	return nil
}

func (v *codeDiffView) Layout(gtx layout.Context) layout.Dimensions {
	if !v.started {
		v.load()
	}
	a := v.a
	sz := gtx.Constraints.Max
	if !v.done {
		return (&codeStatusBody{a: a, loading: true}).Layout(gtx) // "Loading diff"
	}
	if v.err != nil || v.diff == nil {
		return v.layoutError(gtx)
	}
	if v.cwPx != gtx.Metric.PxPerSp {
		v.cwPx = gtx.Metric.PxPerSp
		v.cw = v.monoWidth(gtx, "0000000000") / 10
		if v.cw <= 0 {
			v.cw = gtx.Sp(7)
		}
	}
	files := v.files()
	items := v.items(files)
	// the file tree: PR layout, more than one file, the scroller ≥ 720px
	treeW := 0
	if v.prMode() && v.subtab == "files" && !v.treeOff && len(files) > 1 && sz.X >= gtx.Dp(720) {
		treeW = min(gtx.Dp(240), sz.X/2)
	}
	if v.jump != "" {
		for i, it := range items {
			if it.kind == diFileHead && files[it.file].Filename == v.jump {
				v.list.Position = layout.Position{First: i}
				break
			}
		}
		v.jump = ""
	}
	if treeW > 0 {
		atExact(gtx, image.Pt(0, 0), image.Pt(treeW, sz.Y), func(gtx layout.Context) layout.Dimensions {
			return v.layoutTree(gtx, files)
		})
	}
	lw := sz.X - treeW
	atExact(gtx, image.Pt(treeW, 0), image.Pt(lw, sz.Y), func(gtx layout.Context) layout.Dimensions {
		defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
		d := v.list.Layout(gtx, len(items), func(gtx layout.Context, i int) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Min: image.Pt(lw, 0), Max: image.Pt(lw, 1<<20)}
			return v.layoutItem(gtx, files, items[i], lw)
		})
		v.stickyHeader(gtx, files, items, lw)
		return d
	})
	return layout.Dimensions{Size: sz}
}

// stickyHeader keeps the file header of the group at the top in view, as
// .fileItem__Tg0hG is position: sticky.
func (v *codeDiffView) stickyHeader(gtx layout.Context, files []core.CodeDiffFile, items []diffItem, w int) {
	p := v.list.Position
	if p.First >= len(items) || p.Offset <= 0 && items[p.First].kind == diFileHead {
		return
	}
	it := items[p.First]
	switch it.kind {
	case diLine, diSplit, diAnchored, diMore, diFileHead:
	default:
		return
	}
	top := 0
	if v.prMode() { // under the sticky files toolbar
		top = gtx.Dp(46)
		v.layoutToolbar(gtx, files, w)
	}
	defer op.Offset(image.Pt(0, top)).Push(gtx.Ops).Pop()
	v.layoutFileHead(gtx, files[it.file], it.file, w)
}

func (v *codeDiffView) layoutError(gtx layout.Context) layout.Dimensions {
	a := v.a
	sz := gtx.Constraints.Max
	dark := a.th.Dark
	// .authPrompt__AxUTw: a centered column, gap 24, padding 64px 32px
	title, desc := "Unable to load diff", "Something went wrong fetching the diff. Please try again later."
	y := gtx.Dp(64)
	tw := a.th.Measure(gtx, 15, theme.Bold, title)
	at(gtx, image.Pt((sz.X-tw)/2, y), image.Pt(tw+1, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
		return a.th.Label(gtx, 15, theme.Bold, ccTok(dark, "content-pry"), title)
	})
	y += gtx.Dp(22) + gtx.Dp(24)
	dw := min(a.th.Measure(gtx, 13, theme.Regular, desc), sz.X-gtx.Dp(64))
	at(gtx, image.Pt((sz.X-dw)/2, y), image.Pt(dw+1, gtx.Dp(60)), func(gtx layout.Context) layout.Dimensions {
		return a.th.Layout(gtx, theme.TextStyle{Size: 13, Color: ccTok(dark, "content-sec"), Align: 1}, desc)
	})
	return layout.Dimensions{Size: sz}
}

// ---- text helpers ----

func (v *codeDiffView) monoWidth(gtx layout.Context, s string) int {
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(1<<20, 1<<20)}
	d := v.a.th.Text(g, theme.LabelOpts{Font: codeMono, Size: 12, MaxLines: 1}, s)
	m.Stop()
	return d.Size.X
}

func (v *codeDiffView) mono(gtx layout.Context, col color.NRGBA, s string) layout.Dimensions {
	return v.a.th.Text(gtx, theme.LabelOpts{Font: codeMono, Size: 12, MaxLines: 1, Color: col}, s)
}

// expandTabs is white-space: pre with the browser's tab-size 8.
func expandTabs(s string) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		if r == '\t' {
			n := 8 - col%8
			b.WriteString(strings.Repeat(" ", n))
			col += n
			continue
		}
		b.WriteRune(r)
		col++
	}
	return b.String()
}

// tokColor is the slalabaster theme (OH3S) for a token kind.
func tokColor(dark bool, kind int) color.NRGBA {
	switch kind {
	case core.TokComment:
		return ccPlt(dark, "tomato-70")
	case core.TokString:
		return ccPlt(dark, "cilantro-70")
	case core.TokEscape, core.TokPunct:
		return ccTok(dark, "content-ter")
	case core.TokConstant:
		return ccPlt(dark, "indigo-70")
	case core.TokEntity:
		return ccPlt(dark, "aubergine-70")
	}
	return ccTok(dark, "content-pry")
}

// ccPlt is a palette token (rgb(var(--dt_color-plt-…))).
func ccPlt(dark bool, name string) color.NRGBA { return ccTok(dark, "plt-"+name) }

// codeLine draws one line of code at x, highlighted for lang.
func (v *codeDiffView) codeLine(gtx layout.Context, lang, content string, x, y, w, h int) {
	dark := v.a.th.Dark
	content = expandTabs(content)
	if content == "" {
		return
	}
	k := lang + "\x00" + content
	toks, ok := v.toks[k]
	if !ok {
		toks = core.HighlightLine(lang, content)
		if toks == nil {
			toks = []core.CodeToken{{Kind: core.TokPlain, Text: content}}
		}
		if len(v.toks) > 20000 {
			v.toks = map[string][]core.CodeToken{}
		}
		v.toks[k] = toks
	}
	defer clip.Rect{Min: image.Pt(x, y), Max: image.Pt(x+w, y+h)}.Push(gtx.Ops).Pop()
	cx := x
	for _, t := range toks {
		if cx >= x+w {
			break
		}
		n := len([]rune(t.Text))
		tw := n * v.cw
		col := tokColor(dark, t.Kind)
		at(gtx, image.Pt(cx, y), image.Pt(tw+v.cw, h), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions { return v.mono(gtx, col, t.Text) })
		})
		cx += tw
	}
}

// ---- items ----

func (v *codeDiffView) layoutItem(gtx layout.Context, files []core.CodeDiffFile, it diffItem, w int) layout.Dimensions {
	switch it.kind {
	case diPRHeader:
		return v.layoutPRHeader(gtx, w)
	case diSummary:
		return v.layoutSummary(gtx, w)
	case diSubtabs:
		return v.layoutSubtabs(gtx, files, w)
	case diDescription:
		return v.layoutDescription(gtx, w)
	case diComment:
		return v.layoutComment(gtx, v.unanchored[it.n], gtx.Dp(16), w-gtx.Dp(32), false)
	case diToolbar:
		return v.layoutToolbar(gtx, files, w)
	case diFileHead:
		return v.layoutGroupTop(gtx, files[it.file], it.file, w)
	case diLine:
		return v.layoutUnified(gtx, files[it.file], it.line, w)
	case diSplit:
		return v.layoutSplit(gtx, files[it.file], it.line, w)
	case diAnchored:
		f := files[it.file]
		return v.layoutAnchored(gtx, v.byFile[f.Filename][it.key], w)
	case diMore:
		return v.layoutMore(gtx, files[it.file], it.n, w)
	case diFileEnd:
		return v.layoutGroupEnd(gtx, w)
	case diTruncated:
		return v.centerNote(gtx, w, "Diff truncated — too many files or changes to display")
	case diPad:
		return layout.Dimensions{Size: image.Pt(w, gtx.Dp(16))}
	}
	return layout.Dimensions{}
}

// layoutSummary is .summaryLine__ku0Xz (padding 0 16 16, 16 on top
// without a header; gap 8 16): the branches, then the stats.
func (v *codeDiffView) layoutSummary(gtx layout.Context, w int) layout.Dimensions {
	a := v.a
	dark := a.th.Dark
	d := v.diff
	pad := gtx.Dp(16)
	top := pad
	if v.prMode() {
		top = 0
	}
	lh := gtx.Dp(20)
	x := pad
	text := func(s string, col color.NRGBA, bold bool) {
		wt := theme.Regular
		if bold {
			wt = theme.Bold
		}
		tw := a.th.Measure(gtx, 13, wt, s)
		at(gtx, image.Pt(x, top), image.Pt(tw+1, lh), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions { return a.th.Label(gtx, 13, wt, col, s) })
		})
		x += tw
	}
	pill := func(s string) { // <Pill style="aubergine" isMicro> in .monospace
		tw := v.monoWidth(gtx, s)
		pw, ph := tw+gtx.Dp(8), gtx.Dp(18)
		r := image.Rect(x, top+(lh-ph)/2, x+pw, top+(lh-ph)/2+ph)
		theme.RRect(gtx, r, gtx.Dp(4), ccPlt(dark, "aubergine-0"))
		at(gtx, image.Pt(x+gtx.Dp(4), r.Min.Y), image.Pt(tw+1, ph), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, ph, func(gtx layout.Context) layout.Dimensions { return v.mono(gtx, ccPlt(dark, "aubergine-70"), s) })
		})
		x += pw
	}
	fg := ccTok(dark, "content-pry")
	if d.HeadBranch != "" {
		text("Comparing ", fg, false)
		pill(d.BaseBranch)
		text(" ← ", fg, false)
		pill(d.HeadBranch)
	} else {
		text("Branch ", fg, false)
		pill(d.BaseBranch)
	}
	// .stats__FkD_0: 13px content-sec, gap 16
	sx := x + pad
	y := top
	stats := []string{}
	if d.TotalCommits > 0 && v.prURL == "" {
		stats = append(stats, ccPlural(d.TotalCommits, "commit", "commits"))
	}
	stats = append(stats, ccPlural(len(d.Files), "file changed", "files changed"))
	widths := 0
	for _, s := range stats {
		widths += a.th.Measure(gtx, 13, theme.Regular, s) + pad
	}
	add, del, hasTotals := d.Totals()
	addS, delS := "+"+itoa(add), "−"+itoa(del)
	if hasTotals {
		widths += a.th.Measure(gtx, 13, theme.Regular, addS+" "+delS)
	}
	if sx+widths > w-pad { // flex-wrap: the stats go to the next line
		sx, y = pad, top+lh+gtx.Dp(8)
	}
	sec := ccTok(dark, "content-sec")
	for _, s := range stats {
		tw := a.th.Measure(gtx, 13, theme.Regular, s)
		at(gtx, image.Pt(sx, y), image.Pt(tw+1, lh), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions { return a.th.Label(gtx, 13, theme.Regular, sec, s) })
		})
		sx += tw + pad
	}
	if hasTotals {
		for _, p := range []struct {
			s   string
			col color.NRGBA
		}{{addS, ccTok(dark, "brand-core-slack-green")}, {" ", sec}, {delS, ccTok(dark, "brand-core-slack-red")}} {
			tw := a.th.Measure(gtx, 13, theme.Regular, p.s)
			at(gtx, image.Pt(sx, y), image.Pt(tw+1, lh), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions { return a.th.Label(gtx, 13, theme.Regular, p.col, p.s) })
			})
			sx += tw
		}
	}
	return layout.Dimensions{Size: image.Pt(w, y+lh+pad)}
}

func ccPlural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return itoa(n) + " " + many
}

// prPill is Slack's <Pill style>: 13px bold, radius 4.
func (v *codeDiffView) prPill(gtx layout.Context, x, y int, style, label string) int {
	a := v.a
	dark := a.th.Dark
	bg, fg := ccTok(dark, "base-sec"), ccTok(dark, "content-sec")
	switch style {
	case "success":
		bg, fg = ccPlt(dark, "cilantro-0"), ccPlt(dark, "cilantro-70")
	case "indigo":
		bg, fg = ccPlt(dark, "indigo-0"), ccPlt(dark, "indigo-70")
	case "flamingo":
		bg, fg = ccPlt(dark, "flamingo-0"), ccPlt(dark, "flamingo-70")
	case "warning":
		bg, fg = ccPlt(dark, "honeycomb-0"), ccPlt(dark, "honeycomb-90")
	case "urgent":
		bg, fg = ccPlt(dark, "tomato-0"), ccPlt(dark, "tomato-70")
	}
	tw := a.th.Measure(gtx, 13, theme.Bold, label)
	pw, ph := tw+gtx.Dp(12), gtx.Dp(22)
	theme.RRect(gtx, image.Rect(x, y, x+pw, y+ph), gtx.Dp(4), bg)
	at(gtx, image.Pt(x+gtx.Dp(6), y), image.Pt(tw+1, ph), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, ph, func(gtx layout.Context) layout.Dimensions { return a.th.Label(gtx, 13, theme.Bold, fg, label) })
	})
	return pw
}

// layoutPRHeader is the PR's .header__C8wno (padding 16, gap 16): the
// state pill (ty: open success, draft neutral, merged indigo, closed
// flamingo), the review decision and "Merge conflicts".
func (v *codeDiffView) layoutPRHeader(gtx layout.Context, w int) layout.Dimensions {
	pr := v.pr.PullRequest
	pad := gtx.Dp(16)
	x := pad
	styles := map[string]string{"open": "success", "draft": "neutral", "merged": "indigo", "closed": "flamingo"}
	labels := map[string]string{"open": "Open", "draft": "Draft", "merged": "Merged", "closed": "Closed"}
	if l, ok := labels[pr.State]; ok {
		x += v.prPill(gtx, x, pad, styles[pr.State], l) + pad
	}
	if pr.State == "open" || pr.State == "draft" {
		switch pr.ReviewDecision {
		case "approved":
			x += v.prPill(gtx, x, pad, "success", "Approved") + pad
		case "changes_requested":
			x += v.prPill(gtx, x, pad, "warning", "Changes requested") + pad
		}
		if pr.HasConflicts {
			v.prPill(gtx, x, pad, "urgent", "Merge conflicts")
		}
	}
	return layout.Dimensions{Size: image.Pt(w, pad+gtx.Dp(22)+pad)}
}

// layoutSubtabs is .prSubtabs__b8Gil: "Conversation" and "Files Changed
// {n}" buttons (26px, radius 5, 13px bold; selected base-inv-hgl-1).
func (v *codeDiffView) layoutSubtabs(gtx layout.Context, files []core.CodeDiffFile, w int) layout.Dimensions {
	a := v.a
	dark := a.th.Dark
	pad := gtx.Dp(16)
	x := pad
	h := gtx.Dp(26)
	for _, t := range []struct{ id, label, count string }{{"conversation", "Conversation", ""}, {"files", "Files Changed", itoa(len(files))}} {
		sel := v.subtab == t.id
		lw := a.th.Measure(gtx, 13, theme.Bold, t.label)
		cw := 0
		if t.count != "" && !(v.diff.Truncated && len(files) == 0) {
			cw = gtx.Dp(4) + a.th.Measure(gtx, 13, theme.Regular, t.count)
		}
		bw := gtx.Dp(11)*2 + lw + cw
		btn := v.clicks.get("subtab." + t.id)
		t := t
		at(gtx, image.Pt(x, 0), image.Pt(bw, h), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, btn, func(gtx layout.Context, s image.Point, hov bool) {
				r := image.Rectangle{Max: s}
				if sel {
					theme.RRect(gtx, r, gtx.Dp(5), ccTok(dark, "base-inv-hgl-1"))
					return
				}
				bg := ccTok(dark, "base-pry")
				if hov {
					bg = ccTok(dark, "base-sec")
				}
				theme.RRect(gtx, r, gtx.Dp(5), ccTok(dark, "otl-ter"))
				theme.RRect(gtx, r.Inset(gtx.Dp(1)), gtx.Dp(4), bg)
			}, func(gtx layout.Context) layout.Dimensions {
				col := ccTok(dark, "content-pry")
				if sel {
					col = ccTok(dark, "content-inv-pry")
				}
				at(gtx, image.Pt(gtx.Dp(11), 0), image.Pt(lw+1, h), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions { return a.th.Label(gtx, 13, theme.Bold, col, t.label) })
				})
				if cw > 0 {
					at(gtx, image.Pt(gtx.Dp(11)+lw+gtx.Dp(4), 0), image.Pt(cw, h), func(gtx layout.Context) layout.Dimensions {
						return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions { return a.th.Label(gtx, 13, theme.Regular, col, t.count) })
					})
				}
				return layout.Dimensions{Size: image.Pt(bw, h)}
			})
		})
		if clicked(btn) {
			v.subtab = t.id
			v.list.Position = layout.Position{}
			a.Invalidate()
		}
		x += bw + gtx.Dp(8)
	}
	return layout.Dimensions{Size: image.Pt(w, h+pad)}
}

// layoutDescription is the PR description card (.summary__tecC0: 1px
// otl-ter, radius 8, padding 16, 15px/1.5; the title 18px bold).
func (v *codeDiffView) layoutDescription(gtx layout.Context, w int) layout.Dimensions {
	a := v.a
	dark := a.th.Dark
	pr := v.pr.PullRequest
	pad := gtx.Dp(16)
	cw := w - 2*pad
	inner := cw - 2*pad
	m := op.Record(gtx.Ops)
	y := pad
	if pr.Title != "" {
		d := at(gtx, image.Pt(pad, y), image.Pt(inner, 1<<20), func(gtx layout.Context) layout.Dimensions {
			return a.th.Para(gtx, 18, 22, theme.Bold, ccTok(dark, "content-pry"), pr.Title)
		})
		y += d.Size.Y + gtx.Dp(8)
	}
	body := strings.TrimSpace(pr.Body)
	if body != "" {
		d := at(gtx, image.Pt(pad, y), image.Pt(inner, 1<<20), func(gtx layout.Context) layout.Dimensions {
			return a.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: ccTok(dark, "content-pry"), MaxLines: 10}, body)
		})
		y += d.Size.Y
	} else {
		y -= gtx.Dp(8)
	}
	y += pad
	call := m.Stop()
	r := image.Rect(pad, 0, pad+cw, y)
	theme.Border(gtx, r, gtx.Dp(8), gtx.Dp(1), ccTok(dark, "otl-ter"))
	defer op.Offset(image.Pt(pad, 0)).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
	return layout.Dimensions{Size: image.Pt(w, y+pad)}
}

// commentAuthor is the sender's name (G.d_): the Slack member's name, else
// the GitHub login.
func (v *codeDiffView) commentAuthor(s core.CodePRSender) string {
	if s.UserID != "" && v.a.data != nil && v.a.data.core != nil {
		if n := v.a.data.core.S.UserName(s.UserID); n != "" {
			return n
		}
	}
	if s.Name != "" {
		return s.Name
	}
	return s.Login
}

// layoutComment draws one comment: a 20px avatar (radius 4), the author
// (bold) and the text.
func (v *codeDiffView) layoutComment(gtx layout.Context, c core.CodePRComment, x, w int, inline bool) layout.Dimensions {
	a := v.a
	dark := a.th.Dark
	av := gtx.Dp(20)
	url := c.Sender.AvatarURL
	if c.Sender.UserID != "" {
		url = a.userAvatar(c.Sender.UserID)
	}
	at(gtx, image.Pt(x, gtx.Dp(6)), image.Pt(av, av), func(gtx layout.Context) layout.Dimensions {
		return a.avatar(gtx, url, v.commentAuthor(c.Sender), av, gtx.Dp(4))
	})
	tx := x + av + gtx.Dp(8)
	tw := w - av - gtx.Dp(8)
	name := v.commentAuthor(c.Sender)
	at(gtx, image.Pt(tx, gtx.Dp(4)), image.Pt(tw, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
		return a.th.Label(gtx, 15, theme.Bold, ccTok(dark, "content-pry"), name)
	})
	d := at(gtx, image.Pt(tx, gtx.Dp(26)), image.Pt(tw, 1<<20), func(gtx layout.Context) layout.Dimensions {
		return a.th.Para(gtx, 15, 22, theme.Regular, ccTok(dark, "content-pry"), strings.TrimSpace(c.Comment.Value))
	})
	h := gtx.Dp(26) + d.Size.Y + gtx.Dp(8)
	return layout.Dimensions{Size: image.Pt(x+w, h)}
}

// layoutAnchored draws review comments under their line (the patch's
// .lineCommentsCell, inside the file group's borders).
func (v *codeDiffView) layoutAnchored(gtx layout.Context, cs []core.CodePRComment, w int) layout.Dimensions {
	pad := gtx.Dp(16)
	y := 0
	m := op.Record(gtx.Ops)
	for _, c := range cs {
		d := func() layout.Dimensions {
			defer op.Offset(image.Pt(0, y)).Push(gtx.Ops).Pop()
			return v.layoutComment(gtx, c, pad+gtx.Dp(12), w-2*pad-gtx.Dp(24), true)
		}()
		y += d.Size.Y
	}
	call := m.Stop()
	y += gtx.Dp(4)
	dark := v.a.th.Dark
	theme.Fill(gtx, image.Rect(pad, 0, w-pad, y), ccTok(dark, "base-pry"))
	v.groupSides(gtx, 0, y, w)
	theme.Fill(gtx, image.Rect(pad, 0, w-pad, gtx.Dp(1)), ccTok(dark, "otl-ter"))
	theme.Fill(gtx, image.Rect(pad, y-gtx.Dp(1), w-pad, y), ccTok(dark, "otl-ter"))
	call.Add(gtx.Ops)
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// groupSides draws the file group's left and right border (1px otl-ter)
// for a row from y0 to y1.
func (v *codeDiffView) groupSides(gtx layout.Context, y0, y1, w int) {
	pad := gtx.Dp(16)
	col := ccTok(v.a.th.Dark, "otl-ter")
	theme.Fill(gtx, image.Rect(pad, y0, pad+gtx.Dp(1), y1), col)
	theme.Fill(gtx, image.Rect(w-pad-gtx.Dp(1), y0, w-pad, y1), col)
}

// layoutToolbar is .filesToolbar__VNQYv (40px, margin-top 6, padding 0
// 16, gap 8): the file tree toggle ("Show/Hide file tree", the
// container ≥ 720px) and "Diff display options".
func (v *codeDiffView) layoutToolbar(gtx layout.Context, files []core.CodeDiffFile, w int) layout.Dimensions {
	a := v.a
	dark := a.th.Dark
	h := gtx.Dp(40)
	top := gtx.Dp(6)
	theme.Fill(gtx, image.Rect(0, 0, w, top+h), ccTok(dark, "base-pry"))
	x := gtx.Dp(16)
	bs := gtx.Dp(28)
	if len(files) > 1 && w+gtx.Dp(240) >= gtx.Dp(720) {
		tb := v.clicks.get("tree")
		at(gtx, image.Pt(x, top+(h-bs)/2), image.Pt(bs, bs), func(gtx layout.Context) layout.Dimensions {
			return iconButton(gtx, tb, "sidebar", 28, 16, ccTok(dark, "content-sec"), a.th.C.BgHover, 6)
		})
		if clicked(tb) {
			v.treeOff = !v.treeOff
			a.Invalidate()
		}
		x += bs + gtx.Dp(8)
	}
	ob := v.clicks.get("options")
	at(gtx, image.Pt(x, top+(h-bs)/2), image.Pt(bs, bs), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, ob, "filter-sliders", 28, 16, ccTok(dark, "content-sec"), a.th.C.BgHover, 6)
	})
	if clicked(ob) {
		a.menu(a.PointerPos(), []MenuItem{
			{Label: "Layout", Header: true},
			{Label: "Unified", Checked: !v.split, Do: func() { v.split = false; a.Invalidate() }},
			{Label: "Split", Checked: v.split, Do: func() { v.split = true; a.Invalidate() }},
			{Divider: true},
			{Label: "Hide whitespace", Checked: v.hideWS, Do: func() { v.hideWS = !v.hideWS; a.Invalidate() }},
		})
	}
	return layout.Dimensions{Size: image.Pt(w, top+h)}
}

// layoutGroupTop is a file group's first row: the rounded top border and
// the header.
func (v *codeDiffView) layoutGroupTop(gtx layout.Context, f core.CodeDiffFile, fi, w int) layout.Dimensions {
	d := v.layoutFileHead(gtx, f, fi, w)
	return d
}

// layoutFileHead is .fileItem__Tg0hG (base-sec, 1px borders, padding 4px
// 8px, gap 8, 13px): the caret toggle, the path (12px mono, ellipsized),
// "Copy filepath", "Generated", then +additions -deletions.
func (v *codeDiffView) layoutFileHead(gtx layout.Context, f core.CodeDiffFile, fi, w int) layout.Dimensions {
	a := v.a
	dark := a.th.Dark
	pad := gtx.Dp(16)
	h := gtx.Dp(36)
	r := image.Rect(pad, 0, w-pad, h)
	collapsed := v.collapsed[f.Filename] || f.Patch == ""
	rad := gtx.Dp(8)
	// the group's rounded border, then the header's fill
	theme.RRect(gtx, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Max.Y+rad), rad, ccTok(dark, "otl-ter"))
	bottom := r.Max.Y - gtx.Dp(1)
	if collapsed {
		bottom = r.Max.Y + rad // the group ends with its header (.fileItemCollapsed: no bottom border)
	}
	func() {
		in := image.Rect(r.Min.X+gtx.Dp(1), r.Min.Y+gtx.Dp(1), r.Max.X-gtx.Dp(1), bottom)
		defer clip.Rect{Max: image.Pt(w, h)}.Push(gtx.Ops).Pop()
		theme.RRect(gtx, in, rad-gtx.Dp(1), ccTok(dark, "base-sec"))
	}()
	x := r.Min.X + gtx.Dp(8)
	tg := v.clicks.get("toggle." + f.Filename)
	caret := "caret-down"
	if collapsed {
		caret = "caret-right"
	}
	at(gtx, image.Pt(x, (h-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, tg, icons.Slack(caret), 16, 16, ccTok(dark, "content-sec"), color.NRGBA{}, 0)
	})
	if clicked(tg) && f.Patch != "" {
		v.collapsed[f.Filename] = !v.collapsed[f.Filename]
		a.Invalidate()
	}
	x += gtx.Dp(16) + gtx.Dp(8)
	// the stats, right-aligned (margin-left: auto)
	sx := r.Max.X - gtx.Dp(8)
	type st struct {
		s   string
		col color.NRGBA
	}
	var stats []st
	if f.Additions > 0 {
		stats = append(stats, st{"+" + itoa(f.Additions), ccTok(dark, "brand-core-slack-green")})
	}
	if f.Deletions > 0 {
		stats = append(stats, st{"-" + itoa(f.Deletions), ccTok(dark, "brand-core-slack-red")})
	}
	for i := len(stats) - 1; i >= 0; i-- {
		s := stats[i]
		tw := a.th.Measure(gtx, 13, theme.Regular, s.s)
		sx -= tw
		at(gtx, image.Pt(sx, 0), image.Pt(tw+1, h), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions { return a.th.Label(gtx, 13, theme.Regular, s.col, s.s) })
		})
		sx -= gtx.Dp(8)
	}
	extra := gtx.Dp(28) + gtx.Dp(8) // the copy button
	genW := 0
	if f.IsGenerated {
		genW = a.th.Measure(gtx, 11, theme.Bold, "Generated") + gtx.Dp(8) + gtx.Dp(8)
	}
	nameMax := sx - x - extra - genW
	nw := min(v.monoWidth(gtx, f.Filename), max(nameMax, 0))
	at(gtx, image.Pt(x, 0), image.Pt(nw, h), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions { return v.mono(gtx, ccTok(dark, "content-pry"), f.Filename) })
	})
	x += nw + gtx.Dp(8)
	cb := v.clicks.get("copy." + f.Filename)
	icon := "copy"
	if v.copied == f.Filename && time.Since(v.copiedAt) < 2*time.Second {
		icon = "check"
		a.Invalidate()
	}
	at(gtx, image.Pt(x, (h-gtx.Dp(28))/2), image.Pt(gtx.Dp(28), gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, cb, icons.Slack(icon), 28, 16, ccTok(dark, "content-sec"), a.th.C.BgHover, 6)
	})
	if clicked(cb) {
		a.CopyText(f.Filename) // "Copy filepath", then "Copied" for 2s
		v.copied, v.copiedAt = f.Filename, time.Now()
	}
	x += gtx.Dp(28) + gtx.Dp(8)
	if f.IsGenerated { // <Pill style="neutral" isMicro>Generated</Pill>
		tw := genW - gtx.Dp(16)
		ph := gtx.Dp(16)
		theme.RRect(gtx, image.Rect(x, (h-ph)/2, x+tw+gtx.Dp(8), (h+ph)/2), gtx.Dp(4), ccTok(dark, "base-ter"))
		at(gtx, image.Pt(x+gtx.Dp(4), (h-ph)/2), image.Pt(tw+1, ph), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, ph, func(gtx layout.Context) layout.Dimensions {
				return a.th.Label(gtx, 11, theme.Bold, ccTok(dark, "content-sec"), "Generated")
			})
		})
	}
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// layoutGroupEnd closes a file group: the rounded bottom border, then the
// list's 16px gap.
func (v *codeDiffView) layoutGroupEnd(gtx layout.Context, w int) layout.Dimensions {
	pad := gtx.Dp(16)
	rad := gtx.Dp(8)
	dark := v.a.th.Dark
	func() {
		defer clip.Rect{Max: image.Pt(w, rad)}.Push(gtx.Ops).Pop()
		theme.RRect(gtx, image.Rect(pad, -rad, w-pad, rad), rad, ccTok(dark, "otl-ter"))
		theme.RRect(gtx, image.Rect(pad+gtx.Dp(1), -rad, w-pad-gtx.Dp(1), rad-gtx.Dp(1)), rad-gtx.Dp(1), ccTok(dark, "base-pry"))
	}()
	return layout.Dimensions{Size: image.Pt(w, rad+gtx.Dp(16))}
}

// numWidth is a line number column: width 1%, min-width 48px, padding 0
// 8px, so the widest number plus 16px.
func (v *codeDiffView) numWidth(gtx layout.Context, ls []core.PatchLine) int {
	mx := 0
	for _, l := range ls {
		mx = max(mx, l.OldLine, l.NewLine)
	}
	return max(gtx.Dp(48), len(itoa(mx))*v.cw+gtx.Dp(16))
}

// layoutUnified is one unified row (.patchRow__SV5L_, 20px; a hunk row
// 4px padding): old number, new number, the code.
func (v *codeDiffView) layoutUnified(gtx layout.Context, f core.CodeDiffFile, i, w int) layout.Dimensions {
	dark := v.a.th.Dark
	ls := v.lines(f)
	l := ls[i]
	pad := gtx.Dp(16)
	x0, x1 := pad+gtx.Dp(1), w-pad-gtx.Dp(1)
	nw := v.numWidth(gtx, ls)
	h := gtx.Dp(20)
	if l.Type == core.PatchHunk {
		h = gtx.Dp(23)
	}
	theme.Fill(gtx, image.Rect(x0, 0, x1, h), ccTok(dark, "base-pry"))
	v.groupSides(gtx, 0, h, w)
	ter := ccTok(dark, "content-ter")
	switch l.Type {
	case core.PatchHunk:
		theme.Fill(gtx, image.Rect(x0, 0, x1, h), ccTok(dark, "base-hgl-1"))
		cx := x0 + 2*nw + pad
		at(gtx, image.Pt(cx, 0), image.Pt(max(x1-cx-pad, 0), h), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions { return v.mono(gtx, ccTok(dark, "content-sec"), l.Content) })
		})
		return layout.Dimensions{Size: image.Pt(w, h)}
	case core.PatchAdded:
		theme.Fill(gtx, image.Rect(x0, 0, x1, h), patchAddBg)
		theme.Fill(gtx, image.Rect(x0, 0, x0+2*nw, h), patchAddNumBg)
	case core.PatchRemoved:
		theme.Fill(gtx, image.Rect(x0, 0, x1, h), patchDelBg)
		theme.Fill(gtx, image.Rect(x0, 0, x0+2*nw, h), patchDelNumBg)
	}
	num := func(n, x int) {
		if n == 0 {
			return
		}
		s := itoa(n)
		at(gtx, image.Pt(x+gtx.Dp(8), 0), image.Pt(nw, h), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions { return v.mono(gtx, ter, s) })
		})
	}
	if l.Type != core.PatchAdded {
		num(l.OldLine, x0)
	}
	if l.Type != core.PatchRemoved {
		num(l.NewLine, x0+nw)
	}
	cx := x0 + 2*nw + pad
	v.codeLine(gtx, core.CodeLang(f.Filename), l.Content, cx, 0, max(x1-cx, 0), h)
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// layoutSplit is one split row (.splitTable__GHa_r: 80px number columns,
// table-layout fixed; an empty side is base-sec).
func (v *codeDiffView) layoutSplit(gtx layout.Context, f core.CodeDiffFile, ri, w int) layout.Dimensions {
	dark := v.a.th.Dark
	ls := v.lines(f)
	rows := core.SplitRows(ls)
	r := rows[ri]
	pad := gtx.Dp(16)
	x0, x1 := pad+gtx.Dp(1), w-pad-gtx.Dp(1)
	h := gtx.Dp(20)
	if r.Hunk {
		h = gtx.Dp(23)
	}
	theme.Fill(gtx, image.Rect(x0, 0, x1, h), ccTok(dark, "base-pry"))
	v.groupSides(gtx, 0, h, w)
	if r.Hunk {
		theme.Fill(gtx, image.Rect(x0, 0, x1, h), ccTok(dark, "base-hgl-1"))
		cx := x0 + gtx.Dp(80) + pad
		at(gtx, image.Pt(cx, 0), image.Pt(max(x1-cx-pad, 0), h), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
				return v.mono(gtx, ccTok(dark, "content-sec"), ls[r.Index].Content)
			})
		})
		return layout.Dimensions{Size: image.Pt(w, h)}
	}
	nw := gtx.Dp(80)
	half := (x1 - x0) / 2
	ter := ccTok(dark, "content-ter")
	lang := core.CodeLang(f.Filename)
	side := func(idx, x int, left bool) {
		if idx < 0 {
			theme.Fill(gtx, image.Rect(x, 0, x+half, h), ccTok(dark, "base-sec"))
			return
		}
		l := ls[idx]
		changed := l.Type != core.PatchContext
		if changed {
			bg, nb := patchAddBg, patchAddNumBg
			if left {
				bg, nb = patchDelBg, patchDelNumBg
			}
			theme.Fill(gtx, image.Rect(x, 0, x+half, h), bg)
			theme.Fill(gtx, image.Rect(x, 0, x+nw, h), nb)
		}
		n := l.NewLine
		if left {
			n = l.OldLine
		}
		if n > 0 {
			s := itoa(n)
			at(gtx, image.Pt(x+gtx.Dp(8), 0), image.Pt(nw, h), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions { return v.mono(gtx, ter, s) })
			})
		}
		cx := x + nw + pad
		v.codeLine(gtx, lang, l.Content, cx, 0, max(x+half-cx-gtx.Dp(4), 0), h)
	}
	side(r.Left, x0, true)
	side(r.Right, x0+half, false)
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// layoutMore is .expandBar__CZ8f6: "Show N more lines" (base-hgl-1, a
// top border, padding 12px 0, 12px content-hgl-1).
func (v *codeDiffView) layoutMore(gtx layout.Context, f core.CodeDiffFile, n, w int) layout.Dimensions {
	a := v.a
	dark := a.th.Dark
	pad := gtx.Dp(16)
	h := gtx.Dp(12)*2 + gtx.Dp(15)
	x0, x1 := pad+gtx.Dp(1), w-pad-gtx.Dp(1)
	theme.Fill(gtx, image.Rect(x0, 0, x1, h), ccTok(dark, "base-hgl-1"))
	theme.Fill(gtx, image.Rect(x0, 0, x1, gtx.Dp(1)), ccTok(dark, "otl-ter"))
	v.groupSides(gtx, 0, h, w)
	label := "Show " + itoa(n) + " more lines"
	tw := a.th.Measure(gtx, 12, theme.Regular, label)
	b := v.clicks.get("more." + f.Filename)
	at(gtx, image.Pt((w-tw)/2, gtx.Dp(12)), image.Pt(tw+1, gtx.Dp(15)), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, b, nil, func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 12, theme.Regular, ccTok(dark, "content-hgl-1"), label)
		})
	})
	if clicked(b) {
		v.showAll[f.Filename] = true
		a.Invalidate()
	}
	return layout.Dimensions{Size: image.Pt(w, h)}
}

func (v *codeDiffView) centerNote(gtx layout.Context, w int, s string) layout.Dimensions {
	a := v.a
	tw := min(a.th.Measure(gtx, 13, theme.Regular, s), w-gtx.Dp(32))
	at(gtx, image.Pt((w-tw)/2, gtx.Dp(16)), image.Pt(tw+1, gtx.Dp(40)), func(gtx layout.Context) layout.Dimensions {
		return a.th.Para(gtx, 13, 18, theme.Regular, ccTok(a.th.Dark, "content-sec"), s)
	})
	return layout.Dimensions{Size: image.Pt(w, gtx.Dp(16)*2+gtx.Dp(18))}
}

// layoutTree is the file tree (.fileTreePane__agnE3: padding 8 8 16 16;
// .row__bJA5r: 13px, padding 4px 8px, 12px a level, files 24px further
// in; a folder icon, or the change dot / renamed arrow).
func (v *codeDiffView) layoutTree(gtx layout.Context, files []core.CodeDiffFile) layout.Dimensions {
	a := v.a
	dark := a.th.Dark
	rows := core.FileTree(files)
	inView := ""
	if p := v.list.Position; p.First >= 0 {
		items := v.items(files)
		if p.First < len(items) {
			switch items[p.First].kind {
			case diFileHead, diLine, diSplit, diAnchored, diMore, diFileEnd:
				inView = files[items[p.First].file].Filename
			}
		}
	}
	return layout.Inset{Top: 8, Left: 16, Right: 8, Bottom: 16}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		w := gtx.Constraints.Max.X
		return v.treeList.Layout(gtx, len(rows), func(gtx layout.Context, i int) layout.Dimensions {
			r := rows[i]
			h := gtx.Dp(26)
			indent := gtx.Dp(8) + r.Depth*gtx.Dp(12)
			if !r.Folder {
				indent += gtx.Dp(24)
			}
			b := v.clicks.get("treerow." + r.Path)
			active := !r.Folder && r.Path == inView
			d := clickArea(gtx, b, func(gtx layout.Context, s image.Point, hov bool) {
				switch {
				case active:
					theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(4), ccTok(dark, "base-ter"))
				case hov:
					theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(4), ccTok(dark, "base-sec"))
				}
			}, func(gtx layout.Context) layout.Dimensions {
				x := indent
				if r.Folder {
					at(gtx, image.Pt(x, (h-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
						return icons.Get(icons.Slack("folder"))(gtx, 16, ccTok(dark, "content-sec"))
					})
					x += gtx.Dp(16) + gtx.Dp(8)
				} else {
					kind := core.FileChangeKind(r.File.Status)
					col := map[string]color.NRGBA{"added": ccTok(dark, "brand-core-slack-green"), "removed": ccTok(dark, "brand-core-slack-red"),
						"modified": ccTok(dark, "brand-core-slack-yellow"), "renamed": ccTok(dark, "content-sec")}[kind]
					if kind == "renamed" {
						at(gtx, image.Pt(x, (h-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
							return icons.Get(icons.Slack("arrow-right"))(gtx, 16, col)
						})
					} else {
						theme.Circle(gtx, image.Pt(x+gtx.Dp(8), h/2), gtx.Dp(6), col)
					}
					x += gtx.Dp(16) + gtx.Dp(8)
				}
				wt := theme.Regular
				if active {
					wt = theme.Bold
				}
				col := ccTok(dark, "content-pry")
				if r.File != nil && r.File.IsGenerated {
					col = ccTok(dark, "content-sec")
				}
				at(gtx, image.Pt(x, 0), image.Pt(max(w-x-gtx.Dp(8), 0), h), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions { return a.th.Label(gtx, 13, wt, col, r.Name) })
				})
				return layout.Dimensions{Size: image.Pt(w, h)}
			})
			if clicked(b) && !r.Folder {
				v.jump = r.Path
				v.collapsed[r.Path] = false
				a.Invalidate()
			}
			return d
		})
	})
}
