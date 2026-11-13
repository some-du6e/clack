package ui

// A channel-tab slug's peek, as Slack's web client wires it (build
// 3889f0c93d30, read from the public CDN without a cookie):
//
//   - NY16 MrkdwnChannelTab resolves the slug's tab (core.ChannelTabSlugPeek)
//     and ResolvedChannelTabSlug renders renderChannelTabSlugPeek (zDOB),
//     which D3FY's ChannelTabSlugPeekTrigger fills: with
//     session_channels_slug_peek on and a tab type isPreviewEnabled
//     (mwX5) lists, a HoverCardTrigger (0oEe) whose card is useTabPeek's
//     (q5Cv) view of the tab; otherwise the channel card (hover_card.go).
//   - HoverCardTrigger: position "top", offsetY -12, 350ms open, 250ms
//     close, tryOppositePosition, the Popover's 80ms fade; a click on the
//     slug closes it and the click goes on to the slug (navigateToChannelTab:
//     codechannel_pane.go openChannelTab). The hovercard controller runs it
//     (hovercard Request.Tab).
//   - The card is ChannelTabPeek (qjnr) → ChannelHeaderPeekContainer (07S2):
//     .container__ZCbmL 360px wide (popups.PeekContainer), slid down 8px
//     and faded in over .16s unless a11y_animations is off
//     (.containerAnimateIn__p1IDs); .content__p5cYb is a flex box at most
//     75vh tall whose height is the peek's contentHeight (a number of px,
//     "auto", or ChannelTabPeek's own calc(85vh - 300px)). A click inside
//     stops there (onClick stopPropagation) unless the peek has its own
//     (ListsTabPeek opens the tab).
//   - useTabPeek's views by tab type: Code (CodeTabPeek), agent_view
//     (AgentViewTabPeek), canvas (CanvasTabPeek), files (FilesTabPeek),
//     bookmarks (PinsTabPeekV2), list (ListsTabPeek), workflows
//     (WorkflowsPeek), folder (FolderTabPeek); "channel", "pins" and any
//     type without a case render an empty fragment, so nothing shows. A
//     view that returns null (no data yet, nothing to show) shows nothing
//     either: the Popover has no chrome of its own.

import (
	"context"
	"encoding/json"
	"image"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/blockkit"
	"github.com/some-du6e/clack/internal/ui/hovercard"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// tabPeekBody is one of useTabPeek's views.
type tabPeekBody interface {
	// height is the view's ChannelTabPeek contentHeight in px: > 0 a
	// fixed height, peekAuto its content's, peekDefault the container's
	// own calc(85vh - 300px). ok false: the view renders null.
	height(gtx layout.Context) (h int, ok bool)
	// layout draws the content (gtx.Constraints.Max is its box; Max.Y
	// is the 75vh cap for an auto height).
	layout(gtx layout.Context) layout.Dimensions
}

const (
	peekAuto    = -1
	peekDefault = 0
)

// slugPeek is the card over a channel-tab slug (hovercard Request.Tab).
type slugPeek struct {
	a      *App
	anchor image.Rectangle
	ch     string
	tab    core.SlugTab
	body   tabPeekBody
	rect   image.Rectangle
	opened time.Time
	done   bool
}

// newSlugPeek is the peek for ch's tab id over anchor (window coordinates);
// nil when the slug no longer resolves to a tab with a peek.
func (a *App) newSlugPeek(anchor image.Rectangle, ch, tab string) *slugPeek {
	c := a.Core()
	if c == nil {
		return nil
	}
	t, ok := c.ChannelTabSlugPeek(ch, tab)
	if !ok {
		return nil
	}
	p := &slugPeek{a: a, anchor: anchor, ch: ch, tab: t}
	p.body = a.tabPeekBodyFor(p, ch, t)
	return p
}

// tabPeekBodyFor is useTabPeek's switch.
func (a *App) tabPeekBodyFor(p *slugPeek, ch string, t core.SlugTab) tabPeekBody {
	switch t.Type {
	case "code":
		return &codeTabPeek{a: a, ch: ch}
	case "agent_view":
		return &agentViewPeek{a: a, ch: ch, tab: t}
	}
	return a.tabPeekBodyMore(p, ch, t)
}

// Rect is the drawn card (window coordinates; empty when nothing shows).
func (p *slugPeek) Rect() image.Rectangle { return p.rect }

// Dismissed reports whether an action on it closed it.
func (p *slugPeek) Dismissed() bool { return p.done }

// Layout draws the card in the window (gtx at the window origin).
func (p *slugPeek) Layout(gtx layout.Context) layout.Dimensions {
	a := p.a
	win := gtx.Constraints.Max
	p.rect = image.Rectangle{}
	if p.body == nil {
		return layout.Dimensions{Size: win}
	}
	h, ok := p.body.height(gtx)
	if !ok {
		return layout.Dimensions{Size: win}
	}
	w := gtx.Dp(360)
	maxH := int(float32(win.Y) * .75) // .content__p5cYb max-height: 75vh
	switch h {
	case peekDefault: // .content__r_uvR: calc(85vh - 300px)
		h = max(0, int(float32(win.Y)*.85)-gtx.Dp(300))
	}
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(w, maxH)}
	if h != peekAuto {
		h = min(h, maxH)
		g.Constraints = layout.Exact(image.Pt(w, h))
	}
	m := op.Record(gtx.Ops)
	d := func() layout.Dimensions {
		defer clip.Rect{Max: g.Constraints.Max}.Push(g.Ops).Pop()
		return p.body.layout(g)
	}()
	call := m.Stop()
	if h == peekAuto {
		h = min(d.Size.Y, maxH)
	}
	sz := image.Pt(w, h)
	pos := hovercard.PlaceTop(p.anchor, sz, win, gtx.Dp(hovercard.OffsetY), gtx.Dp(hovercard.AllowanceX), gtx.Dp(hovercard.AllowanceY))
	p.rect = image.Rectangle{Min: pos, Max: pos.Add(sz)}
	if p.opened.IsZero() {
		p.opened = gtx.Now
	}
	draw := pos
	if !a.reduceMotion() { // .containerAnimateIn__p1IDs (off with a11y_animations off)
		if el := gtx.Now.Sub(p.opened); el < popups.PeekAnimateIn {
			x := float64(el) / float64(popups.PeekAnimateIn)
			draw.Y += int(float64(gtx.Dp(-8)) * (1 - ccBezier(.25, 1, .5, 1, x)))
			defer paint.PushOpacity(gtx.Ops, float32(ccBezier(.33, 1, .68, 1, x))).Pop()
			gtx.Execute(op.InvalidateCmd{})
		}
	}
	r := image.Rectangle{Min: draw, Max: draw.Add(sz)}
	popups.PeekContainer(gtx, a.th, r)
	func() {
		defer op.Offset(draw).Push(gtx.Ops).Pop()
		defer clip.UniformRRect(image.Rectangle{Max: sz}, gtx.Dp(12)).Push(gtx.Ops).Pop()
		call.Add(gtx.Ops)
	}()
	return layout.Dimensions{Size: win}
}

// ---- CodeTabPeek ----

// codeTabPeek is CodeTabPeek (q5Cv): the branch and the diff's stats,
// from client.codeChannels.getDiff (reason "code_tab_peek"):
//
//   - nothing without session_channels (biaa.Jv) or a repo / git repo /
//     diff_file_id, or once the fetch has no diff.
//   - loading: contentHeight 100 with the jumbo spinner ("Loading diff")
//     in .loading__rbEGK (centred).
//   - else contentHeight "auto": .peekContainer__z6Ho2 (column, gap 12,
//     padding 16) of .branchInfo__LOf2t (13px): base_branch " ← "
//     head_branch, each a micro aubergine Tag around a one-line Truncate
//     in "SF Mono" (.monospace__tc6Rw); and .stats__FkD_0 (13px
//     content-sec, wrap, gap 16): "{n} commit(s)" when total_commits > 0,
//     "{n} file(s) changed", and unless truncated "+{additions}"
//     (brand-core-slack-green) " " "−{deletions}" (brand-core-slack-red).
type codeTabPeek struct {
	a       *App
	ch      string
	started bool
	done    bool
	diff    *core.CodeDiff
}

func (v *codeTabPeek) load() {
	v.started = true
	a := v.a
	c := a.Core()
	if c == nil {
		v.done = true
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		d, err := c.CodeTabPeekDiff(ctx, v.ch)
		a.Post(func() {
			v.done = true
			if err == nil {
				v.diff = d
			}
			a.Invalidate()
		})
	}()
}

func (v *codeTabPeek) height(gtx layout.Context) (int, bool) {
	c := v.a.Core()
	if c == nil || !c.CodeTabPeekOn(v.ch) {
		return 0, false
	}
	if !v.started {
		v.load()
	}
	if !v.done {
		return gtx.Dp(100), true
	}
	if v.diff == nil {
		return 0, false
	}
	return peekAuto, true
}

// codePeekStats are the stats line's parts, in order.
func codePeekStats(d *core.CodeDiff) []string {
	var out []string
	if d.TotalCommits > 0 {
		out = append(out, peekPlural(d.TotalCommits, "commit", "commits"))
	}
	out = append(out, peekPlural(len(d.Files), "file changed", "files changed"))
	return out
}

func peekPlural(n int, one, other string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + other
}

func (v *codeTabPeek) layout(gtx layout.Context) layout.Dimensions {
	a := v.a
	if !v.done {
		return (&codeStatusBody{a: a, loading: true}).Layout(gtx)
	}
	d := v.diff
	dark := a.th.Dark
	w := gtx.Constraints.Max.X
	pad := gtx.Dp(16)
	cw := w - 2*pad
	y := pad
	// .branchInfo__LOf2t: base ← head, inline (a tag that doesn't fit
	// goes to the next line)
	lineH := gtx.Dp(20)
	arrow := " ← "
	aw := a.th.Measure(gtx, 13, theme.Regular, arrow)
	bw := v.tagWidth(gtx, d.BaseBranch, cw)
	hw := v.tagWidth(gtx, d.HeadBranch, cw)
	x := pad
	v.tag(gtx, d.BaseBranch, image.Pt(x, y), bw)
	x += bw
	if x+aw+hw > pad+cw {
		if x+aw > pad+cw {
			y += lineH
			x = pad
		}
	}
	at(gtx, image.Pt(x, y), image.Pt(aw+1, lineH), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, lineH, func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 13, theme.Regular, ccTok(dark, "content-pry"), arrow)
		})
	})
	x += aw
	if x+hw > pad+cw {
		y += lineH
		x = pad
	}
	v.tag(gtx, d.HeadBranch, image.Pt(x, y), hw)
	y += lineH + gtx.Dp(12)
	// .stats__FkD_0
	sec := ccTok(dark, "content-sec")
	statH := gtx.Dp(18)
	x = pad
	put := func(wd int, draw func(gtx layout.Context)) {
		if x > pad && x+wd > pad+cw {
			y += statH
			x = pad
		}
		at(gtx, image.Pt(x, y), image.Pt(wd+1, statH), func(gtx layout.Context) layout.Dimensions {
			draw(gtx)
			return layout.Dimensions{Size: image.Pt(wd, statH)}
		})
		x += wd + gtx.Dp(16)
	}
	for _, s := range codePeekStats(d) {
		s := s
		put(a.th.Measure(gtx, 13, theme.Regular, s), func(gtx layout.Context) {
			a.th.Label(gtx, 13, theme.Regular, sec, s)
		})
	}
	if add, del, ok := d.Totals(); ok {
		as, ds := "+"+strconv.Itoa(add), "−"+strconv.Itoa(del)
		sw := a.th.Measure(gtx, 13, theme.Regular, " ")
		w1, w2 := a.th.Measure(gtx, 13, theme.Regular, as), a.th.Measure(gtx, 13, theme.Regular, ds)
		put(w1+sw+w2, func(gtx layout.Context) {
			a.th.Label(gtx, 13, theme.Regular, ccTok(dark, "brand-core-slack-green"), as)
			defer op.Offset(image.Pt(w1+sw, 0)).Push(gtx.Ops).Pop()
			a.th.Label(gtx, 13, theme.Regular, ccTok(dark, "brand-core-slack-red"), ds)
		})
	}
	y += statH + pad
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// tagWidth is a micro Tag around s: padding-inline 4, 12px "SF Mono",
// at most maxW (the Truncate ellipsizes).
func (v *codeTabPeek) tagWidth(gtx layout.Context, s string, maxW int) int {
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(1<<20, gtx.Dp(20))}
	d := v.a.th.Text(g, theme.LabelOpts{Font: codeMono, Size: 12, MaxLines: 1}, s)
	m.Stop()
	return min(maxW, d.Size.X+2*gtx.Dp(4))
}

// tag draws the micro aubergine Tag (jF6I: height 20, radius 4,
// aubergine-10 behind aubergine-70) at p, w wide.
func (v *codeTabPeek) tag(gtx layout.Context, s string, p image.Point, w int) {
	a := v.a
	dark := a.th.Dark
	h := gtx.Dp(20)
	theme.RRect(gtx, image.Rectangle{Min: p, Max: p.Add(image.Pt(w, h))}, gtx.Dp(4), ccTok(dark, "plt-aubergine-10"))
	at(gtx, image.Pt(p.X+gtx.Dp(4), p.Y), image.Pt(max(1, w-2*gtx.Dp(4)), h), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
			return a.th.Text(gtx, theme.LabelOpts{Font: codeMono, Size: 12, MaxLines: 1, Color: ccTok(dark, "plt-aubergine-70")}, s)
		})
	})
}

// ---- AgentViewTabPeek ----

// agentViewPeek is AgentViewTabPeek (q5Cv): with session_channels
// (biaa.YH), useViewType's block_kit view is BlockKitViewPeek, an html
// view (or any tab with a file id) HtmlViewPeek; anything else nothing.
//
//   - BlockKitViewPeek: client.codeChannels.getBlockKitView {viewId: the
//     tab's id, reason "agent_view_tab_peek"}; with data, contentHeight
//     240 and .peekContent__bsgQJ (padding 16, scrolls) around the blocks
//     (BlockKitRenderer, containerContext "code_channel_view").
//   - HtmlViewPeek: nothing without the tab's data.fileId; else
//     client.codeChannels.getView {reason "agent_view_tab_peek"} and, with
//     data, contentHeight 240 around HtmlSurfaceIframe (variant
//     agent_view). Clack builds no sandbox origin for agent HTML, which is
//     the iframe's own no-sandbox branch: "View is not available in this
//     environment." in .htmlSurfaceSpinner__Bb7AZ (centred over the box).
type agentViewPeek struct {
	a       *App
	ch      string
	tab     core.SlugTab
	kind    string // block_kit | html | ""
	started bool
	done    bool
	ok      bool
	blocks  json.RawMessage
	sid     string
	appID   string
	surf    *blockkit.Surface
}

func (v *agentViewPeek) load() {
	v.started = true
	a := v.a
	c := a.Core()
	if c == nil || c.Experiment("session_channels") != "on" {
		v.done = true
		return
	}
	switch typ := c.AgentViewType(v.ch, v.tab.ID); {
	case typ == "block_kit":
		v.kind = "block_kit"
	case typ == "html" || v.tab.FileID != "":
		v.kind = "html"
	}
	if v.kind == "" || v.kind == "html" && v.tab.FileID == "" {
		v.done = true
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var err error
		var blocks json.RawMessage
		var sid, app string
		if v.kind == "block_kit" {
			blocks, sid, app, err = c.AgentViewPeekBlocks(ctx, v.ch, v.tab.ID)
		} else {
			_, err = c.AgentViewPeekHTML(ctx, v.ch, v.tab.ID)
		}
		a.Post(func() {
			v.done, v.ok = true, err == nil
			v.blocks, v.sid, v.appID = blocks, sid, app
			a.Invalidate()
		})
	}()
}

func (v *agentViewPeek) height(gtx layout.Context) (int, bool) {
	if !v.started {
		v.load()
	}
	if !v.done || !v.ok {
		return 0, false
	}
	return gtx.Dp(240), true
}

func (v *agentViewPeek) layout(gtx layout.Context) layout.Dimensions {
	a := v.a
	sz := gtx.Constraints.Max
	if v.kind == "html" {
		msg := "View is not available in this environment."
		col := a.th.C.Fg
		mw := min(a.th.Measure(gtx, 15, theme.Regular, msg), sz.X-gtx.Dp(32))
		at(gtx, image.Pt((sz.X-mw)/2, sz.Y/2-gtx.Dp(11)), image.Pt(mw+1, gtx.Dp(44)), func(gtx layout.Context) layout.Dimensions {
			return a.th.Para(gtx, 15, 22, theme.Regular, col, msg)
		})
		return layout.Dimensions{Size: sz}
	}
	if v.surf == nil {
		v.surf = blockkit.NewCodeChannelSurface(a, v.blocks, v.ch, v.tab.ID, v.sid, v.appID)
	}
	pad := gtx.Dp(16)
	return at(gtx, image.Pt(pad, pad), image.Pt(max(1, sz.X-2*pad), 1<<20), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Point{}
		v.surf.Layout(gtx)
		return layout.Dimensions{Size: sz}
	})
}

// slugPeekTabText is the peek's state for tests: the tab type and what
// it draws.
func (p *slugPeek) slugPeekTabText() string {
	if p == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(p.tab.Type)
	if s, ok := p.body.(interface{ peekText() string }); ok {
		b.WriteString(": ")
		b.WriteString(s.peekText())
	}
	return b.String()
}

// peekText is what the Code peek says (tests).
func (v *codeTabPeek) peekText() string {
	if !v.done {
		return "loading"
	}
	if v.diff == nil {
		return ""
	}
	parts := []string{v.diff.BaseBranch + " ← " + v.diff.HeadBranch}
	parts = append(parts, codePeekStats(v.diff)...)
	if add, del, ok := v.diff.Totals(); ok {
		parts = append(parts, "+"+strconv.Itoa(add)+" −"+strconv.Itoa(del))
	}
	return strings.Join(parts, " | ")
}

// peekText is what the agent view peek shows (tests).
func (v *agentViewPeek) peekText() string {
	if !v.done {
		return "loading"
	}
	if !v.ok {
		return ""
	}
	return v.kind
}
