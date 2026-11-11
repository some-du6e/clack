package ui

import (
	"context"
	"image"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/blockkit"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The channel's Workflows tab: Slack's useChannelWorkflowsExplorer
// (boot-render Jf9W, build a84662d2d135; docs/WORKFLOWS.md) in its
// WorkflowsTabView (.workflows_tab_view: the base-sec background, 20px
// down, the results at most 968 wide). Featured workflows (the channel's
// channel_workflows) under "Featured workflows", else the "Feature a
// workflow" card; bookmarked ones under "Bookmarked workflows", else the
// "Bookmark a workflow" card; then "Got repetitive tasks? Try
// workflows." Rows are the explorer's WorkflowItem (boot-shared 31Gm):
// the 36px icon, the bold name over the description, ▶ "Start workflow"
// on hover for a link trigger; a workflow the viewer may not see is
// AccessDeniedWorkflowItem.

type wfTabState struct {
	mu      sync.Mutex
	loaded  bool
	stale   bool      // reload, still showing the last answer
	seen    time.Time // the last frame the tab was on screen
	loading bool
	tab     core.WorkflowsTab
	err     error

	list                   layout.List
	featureHdr, addHdr     widget.Clickable
	featureCTA, addCTA     widget.Clickable
	buildScratch, moreInfo widget.Clickable
	starts, rows           map[string]*widget.Clickable
	tipStart               map[string]*popups.TipState
	featureTip             popups.TipState
	lastW                  int
}

var (
	wfTabs       sync.Map // *App -> map[string]*wfTabState (UI goroutine only)
	wfTabPending sync.Map // *App -> map[string]bool: "Add a workflow" wants the tab open
)

func (a *App) wfTab(ch string) *wfTabState {
	m, _ := wfTabs.Load(a)
	states, _ := m.(map[string]*wfTabState)
	if states == nil {
		states = map[string]*wfTabState{}
		wfTabs.Store(a, states)
	}
	st := states[ch]
	if st == nil {
		st = &wfTabState{starts: map[string]*widget.Clickable{}, rows: map[string]*widget.Clickable{}, tipStart: map[string]*popups.TipState{}}
		st.list.Axis = layout.Vertical
		states[ch] = st
	}
	return st
}

// openWorkflowsTab asks for ch's Workflows tab to open once its tab bar
// has it (Slack opens the tab after "Add a workflow"), and reloads it.
func (a *App) openWorkflowsTab(ch string) {
	m, _ := wfTabPending.Load(a)
	p, _ := m.(map[string]bool)
	if p == nil {
		p = map[string]bool{}
		wfTabPending.Store(a, p)
	}
	p[ch] = true
	a.reloadWorkflowsTab(ch)
}

func (a *App) reloadWorkflowsTab(ch string) {
	st := a.wfTab(ch)
	st.mu.Lock()
	st.stale = true
	st.mu.Unlock()
	a.Invalidate()
}

// workflowsTabView is the body of ch's selected tab when it is the
// Workflows tab.
func (a *App) workflowsTabView(ch string) View {
	tabs := a.channelTabs(ch)
	if m, _ := wfTabPending.Load(a); m != nil {
		if p := m.(map[string]bool); p[ch] {
			for i, t := range tabs {
				if t.icon == "workflow" {
					if a.chTab == nil {
						a.chTab = map[string]int{}
					}
					a.chTab[ch] = i
					delete(p, ch)
					break
				}
			}
		}
	}
	i := a.chTab[ch]
	if i <= 0 || i >= len(tabs) || tabs[i].icon != "workflow" {
		return nil
	}
	return wfTabView{a: a, ch: ch}
}

type wfTabView struct {
	a  *App
	ch string
}

func (v wfTabView) Layout(gtx layout.Context) layout.Dimensions {
	return v.a.layoutWorkflowsTab(gtx, v.ch)
}

// load reads the tab's data off the UI goroutine.
func (a *App) loadWorkflowsTab(ch string, st *wfTabState) {
	c := a.Core()
	st.mu.Lock()
	if st.loading || st.loaded && !st.stale || c == nil {
		st.mu.Unlock()
		return
	}
	st.loading, st.stale = true, false
	st.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		tab, err := c.Interactive().ChannelWorkflowsTab(ctx, ch)
		a.Post(func() {
			st.mu.Lock()
			if err == nil || !st.loaded { // a failed reload keeps the last answer
				st.tab = tab
			}
			st.loading, st.loaded, st.err = false, true, err
			st.mu.Unlock()
			a.Invalidate()
		})
	}()
}

func (a *App) layoutWorkflowsTab(gtx layout.Context, ch string) layout.Dimensions {
	sz := gtx.Constraints.Max
	th := a.th
	cl := popups.ColorsOf(th)
	theme.Fill(gtx, image.Rectangle{Max: sz}, cl.BgAlt) // --dt_color-base-sec
	st := a.wfTab(ch)
	if !st.seen.IsZero() && gtx.Now.Sub(st.seen) > 2*time.Second {
		st.stale = true // opened again: the stores refetch (maybeRefetch)
	}
	st.seen = gtx.Now
	a.loadWorkflowsTab(ch, st)
	st.mu.Lock()
	tab, loaded := st.tab, st.loaded
	st.mu.Unlock()
	a.workflowsTabClicks(gtx, ch, st, tab)

	w := min(gtx.Dp(968), sz.X-gtx.Dp(32))
	x0 := (sz.X - w) / 2
	var rows []layout.Widget
	gap := func(v unit.Dp) layout.Widget {
		return func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: image.Pt(w, gtx.Dp(v))} }
	}
	rows = append(rows, gap(20))
	if loaded {
		const featuredSub = "Feature up to 15 workflows, so people can use them instead of sending a message."
		const bookmarkSub = "Help people find and use workflows relevant to this channel."
		if len(tab.Featured) > 0 {
			var act *widget.Clickable
			if tab.CanManage {
				act = &st.featureHdr
			}
			rows = append(rows, a.wfSection("Featured workflows", featuredSub, act, "Feature workflow", !tab.FeaturedFull))
			rows = append(rows, a.wfRows(ch, st, tab.Featured, true)...)
		} else {
			rows = append(rows, a.wfCTA("star", "Feature a workflow", featuredSub, "Feature Workflow", &st.featureCTA, tab.CanManage))
		}
		if len(tab.Bookmarked) > 0 {
			var act *widget.Clickable
			if tab.CanManage {
				act = &st.addHdr
			}
			rows = append(rows, a.wfSection("Bookmarked workflows", bookmarkSub, act, "Add workflow", true))
			rows = append(rows, a.wfRows(ch, st, tab.Bookmarked, false)...)
		} else {
			rows = append(rows, a.wfCTA("bookmark", "Bookmark a workflow", bookmarkSub, "Add Workflow", &st.addCTA, tab.CanManage))
		}
		rows = append(rows, a.wfFooter(st), gap(24))
	}
	atExact(gtx, image.Pt(x0, 0), image.Pt(w, sz.Y), func(gtx layout.Context) layout.Dimensions {
		return st.list.Layout(gtx, len(rows), func(gtx layout.Context, i int) layout.Dimensions {
			gtx.Constraints.Min.X, gtx.Constraints.Max.X = w, w
			return rows[i](gtx)
		})
	})
	return layout.Dimensions{Size: sz}
}

// workflowsTabClicks runs the tab's buttons: Feature workflow opens the
// picker as channel-primary-action (link triggers only, the featured
// ones left out) and features the pick (setChannelWorkflowActions); Add
// workflow opens it as channel_workflows (automated triggers too, the
// bookmarked ones left out) and bookmarks the pick with its shortcut URL
// (addWorkflowBookmarkForChannel); Build from scratch is Workflow
// Builder's create_empty; Get more info the help article.
func (a *App) workflowsTabClicks(gtx layout.Context, ch string, st *wfTabState, tab core.WorkflowsTab) {
	feature := st.featureHdr.Clicked(gtx) || st.featureCTA.Clicked(gtx)
	add := st.addHdr.Clicked(gtx) || st.addCTA.Clicked(gtx)
	if feature && tab.CanManage && !tab.FeaturedFull {
		var ex []string
		for _, f := range tab.Featured {
			ex = append(ex, f.Trigger.ID)
		}
		blockkit.OpenWorkflowPicker(a, blockkit.WorkflowPickerOpts{Source: blockkit.PickerChannelPrimary, ChannelID: ch, Excluded: ex,
			OnInsert: func(t core.WorkflowTrigger) { a.featureWorkflow(ch, t) }})
	}
	if add && tab.CanManage {
		var ex []string
		for _, b := range tab.Bookmarked {
			ex = append(ex, b.Trigger.ID)
		}
		blockkit.OpenWorkflowPicker(a, blockkit.WorkflowPickerOpts{Source: blockkit.PickerChannelWorkflows, ChannelID: ch, IncludeAutomated: true,
			Excluded: ex, OnInsert: func(t core.WorkflowTrigger) {
				a.addWorkflowBookmark(ch, t, t.ShortcutURL)
			}})
	}
	if st.buildScratch.Clicked(gtx) {
		if c := a.Core(); c != nil && c.Interactive().TeamID() != "" {
			u := "https://app.slack.com/workflow-builder/" + c.Interactive().TeamID() + "/workflow/create-empty"
			go func() { _ = openurl.Open(u) }()
		}
	}
	if st.moreInfo.Clicked(gtx) {
		go func() { _ = openurl.Open("https://slack.com/help/articles/16583775096083") }()
	}
}

// featureWorkflow features t in ch, then reloads the tab (Slack shows
// nothing when it fails).
func (a *App) featureWorkflow(ch string, t core.WorkflowTrigger) {
	c := a.Core()
	if c == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := c.Interactive().FeatureWorkflow(ctx, ch, t.ID); err != nil {
			return
		}
		a.Post(func() { a.reloadWorkflowsTab(ch) })
	}()
}

// wfSection is a section-label row: .heading (13/18 bold, 70%, 24px
// above), the subtitle (13px, 8px below), and the action button at the
// right (p-explorer_sections--actions).
func (a *App) wfSection(title, sub string, act *widget.Clickable, label string, enabled bool) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		th := a.th
		cl := popups.ColorsOf(th)
		w := gtx.Constraints.Max.X
		top := gtx.Dp(24)
		bw := 0
		if act != nil {
			kind := popups.BtnOutline
			if !enabled {
				kind = popups.BtnDisabled
			}
			bw = buttonWidth(gtx, th, kind, popups.BtnSmall, label)
			at(gtx, image.Pt(w-bw, top), image.Pt(bw, gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
				return popups.Button(gtx, th, act, kind, popups.BtnSmall, label, "", 0)
			})
		}
		tw := w - bw - gtx.Dp(8)
		d1 := at(gtx, image.Pt(0, top), image.Pt(tw, gtx.Dp(18)), func(gtx layout.Context) layout.Dimensions {
			return th.Label(gtx, 13, theme.Bold, theme.WithAlpha(cl.Fg, 0xb3), title)
		})
		d2 := at(gtx, image.Pt(0, top+d1.Size.Y+gtx.Dp(2)), image.Pt(tw, gtx.Dp(40)), func(gtx layout.Context) layout.Dimensions {
			return th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 16, Weight: theme.Regular, Color: theme.WithAlpha(cl.Fg, 0xb3)}, sub)
		})
		h := max(top+d1.Size.Y+gtx.Dp(2)+d2.Size.Y, top+gtx.Dp(28)) + gtx.Dp(8)
		return layout.Dimensions{Size: image.Pt(w, h)}
	}
}

// wfRows are the explorer list: a card of rows (1px border, 12px corners
// at the ends), 16px padding.
func (a *App) wfRows(ch string, st *wfTabState, items []core.TabWorkflow, featured bool) []layout.Widget {
	out := make([]layout.Widget, 0, len(items))
	for i := range items {
		it, first, last := items[i], i == 0, i == len(items)-1
		out = append(out, func(gtx layout.Context) layout.Dimensions { return a.wfRow(gtx, ch, st, it, first, last, featured) })
	}
	return out
}

func (a *App) wfRow(gtx layout.Context, ch string, st *wfTabState, it core.TabWorkflow, first, last, featured bool) layout.Dimensions {
	th := a.th
	cl := popups.ColorsOf(th)
	w := gtx.Constraints.Max.X
	key := it.Trigger.ID
	if !featured {
		key = "b:" + key
	}
	row := st.rows[key]
	if row == nil {
		row = new(widget.Clickable)
		st.rows[key] = row
	}
	start := st.starts[key]
	if start == nil {
		start = new(widget.Clickable)
		st.starts[key] = start
	}
	tip := st.tipStart[key]
	if tip == nil {
		tip = new(popups.TipState)
		st.tipStart[key] = tip
	}
	runnable := !it.Denied && it.Trigger.IsShortcut() && it.Trigger.ShortcutURL != "" && it.Trigger.Published
	if start.Clicked(gtx) && runnable {
		// onExecute with the explorer's context: location bookmark, the
		// trigger as bookmark_id (useChannelWorkflowsExplorer's M)
		blockkit.TripWorkflow(a, it.Trigger.ShortcutURL, core.BookmarkTripContext(ch, it.Trigger.ID), "channel_workflows")
	}
	pad, is := gtx.Dp(16), gtx.Dp(36)
	nameH, descH := gtx.Dp(22), gtx.Dp(18)
	h := 2*pad + max(is, nameH+descH)
	r := image.Rect(0, 0, w, h)
	rad := gtx.Dp(12)
	hover := row.Hovered() || start.Hovered()
	// the card: rows share their borders; the first and last round off
	border := cl.Ring
	if hover && !it.Denied {
		border = theme.WithAlpha(cl.Fg, 0x26)
	}
	var radii clip.RRect
	radii.Rect = r
	if first {
		radii.NW, radii.NE = rad, rad
	}
	if last {
		radii.SW, radii.SE = rad, rad
	}
	func() {
		defer radii.Push(gtx.Ops).Pop()
		theme.Fill(gtx, r, border)
	}()
	inner := r
	inner.Min.X++
	inner.Max.X--
	inner.Min.Y++
	if last {
		inner.Max.Y--
	}
	innerR := clip.RRect{Rect: inner}
	if first {
		innerR.NW, innerR.NE = rad-1, rad-1
	}
	if last {
		innerR.SW, innerR.SE = rad-1, rad-1
	}
	func() {
		defer innerR.Push(gtx.Ops).Pop()
		theme.Fill(gtx, inner, cl.Bg)
	}()
	// the icon: the workflow app's (radius 12), or the lock in a grey box
	at(gtx, image.Pt(pad, (h-is)/2), image.Pt(is, is), func(gtx layout.Context) layout.Dimensions {
		if it.Denied {
			theme.RRect(gtx, image.Rect(0, 0, is, is), gtx.Dp(8), cl.BgAlt)
			ic := gtx.Dp(20)
			at(gtx, image.Pt((is-ic)/2, (is-ic)/2), image.Pt(ic, ic), func(gtx layout.Context) layout.Dimensions {
				return icons.Get("lock")(gtx, 20, cl.Fg3)
			})
			return layout.Dimensions{Size: image.Pt(is, is)}
		}
		if img, ok := a.Images().Get(it.Trigger.Icon, is); ok && it.Trigger.Icon != "" {
			return theme.Image(gtx, img, image.Pt(is, is), gtx.Dp(12))
		}
		return th.Placeholder(gtx, image.Pt(is, is), gtx.Dp(12))
	})
	name, desc := it.Trigger.Title(), it.Trigger.Desc()
	if it.Denied {
		if !featured || name == "" {
			name = "Private workflow"
		}
		desc = "Only certain people can use this workflow"
	}
	tx := pad + is + gtx.Dp(12)
	bw := 0
	if runnable && hover {
		bw = gtx.Dp(28) + gtx.Dp(8)
	}
	tw := w - tx - pad - bw
	ty := (h - nameH - descH) / 2
	at(gtx, image.Pt(tx, ty), image.Pt(tw, nameH), func(gtx layout.Context) layout.Dimensions {
		return th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: theme.Bold, Color: cl.Fg, MaxLines: 1}, name)
	})
	at(gtx, image.Pt(tx, ty+nameH), image.Pt(tw, descH), func(gtx layout.Context) layout.Dimensions {
		return th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Weight: theme.Regular, Color: cl.Muted, MaxLines: 1}, desc)
	})
	// the hover area under the actions
	atExact(gtx, image.Point{}, image.Pt(w, h), func(gtx layout.Context) layout.Dimensions {
		return row.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: image.Pt(w, h)} })
	})
	if runnable && hover {
		bs := gtx.Dp(28)
		p := image.Pt(w-pad-bs, (h-bs)/2)
		at(gtx, p, image.Pt(bs, bs), func(gtx layout.Context) layout.Dimensions {
			d := iconButton(gtx, start, "play", 28, 16, cl.Fg2, cl.BgHover, 6)
			popups.Tooltip(gtx, th, tip, start.Hovered(), d.Size, popups.Above, "Start workflow", "")
			return d
		})
	}
	if last {
		return layout.Dimensions{Size: image.Pt(w, h)}
	}
	return layout.Dimensions{Size: image.Pt(w, h-1)} // the next row's top border is this row's bottom
}

// wfCTA is WorkflowTabCTACard: 24px above, a 1px-bordered card (radius
// 12, padding 16) with the icon in a 36px base-ter box, the bold title
// over the 13px subtitle, and the outline button (disabled without the
// permission to manage the channel's workflows).
func (a *App) wfCTA(icon, title, sub, label string, c *widget.Clickable, enabled bool) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		th := a.th
		cl := popups.ColorsOf(th)
		w := gtx.Constraints.Max.X
		top, pad, is := gtx.Dp(24), gtx.Dp(16), gtx.Dp(36)
		kind := popups.BtnOutline
		if !enabled {
			kind = popups.BtnDisabled
		}
		bd := image.Pt(buttonWidth(gtx, th, kind, popups.BtnMedium, label), gtx.Dp(36))
		h := 2*pad + max(is, gtx.Dp(22)+gtx.Dp(18), bd.Y)
		r := image.Rect(0, top, w, top+h)
		theme.RRect(gtx, r, gtx.Dp(12), cl.Ring)
		theme.RRect(gtx, r.Inset(1), gtx.Dp(12)-1, cl.Bg)
		at(gtx, image.Pt(pad, top+(h-is)/2), image.Pt(is, is), func(gtx layout.Context) layout.Dimensions {
			theme.RRect(gtx, image.Rect(0, 0, is, is), gtx.Dp(12), cl.BgAlt)
			ic := gtx.Dp(18)
			at(gtx, image.Pt((is-ic)/2, (is-ic)/2), image.Pt(ic, ic), func(gtx layout.Context) layout.Dimensions {
				return icons.Get(icon)(gtx, 18, cl.Fg)
			})
			return layout.Dimensions{Size: image.Pt(is, is)}
		})
		tx := pad + is + gtx.Dp(12)
		tw := w - tx - pad - bd.X - gtx.Dp(12)
		ty := top + (h-gtx.Dp(40))/2
		at(gtx, image.Pt(tx, ty), image.Pt(tw, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
			return th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: theme.Bold, Color: cl.Fg, MaxLines: 1}, title)
		})
		at(gtx, image.Pt(tx, ty+gtx.Dp(22)), image.Pt(tw, gtx.Dp(18)), func(gtx layout.Context) layout.Dimensions {
			return th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Weight: theme.Regular, Color: cl.Muted, MaxLines: 1}, sub)
		})
		at(gtx, image.Pt(w-pad-bd.X, top+(h-bd.Y)/2), bd, func(gtx layout.Context) layout.Dimensions {
			return popups.Button(gtx, th, c, kind, popups.BtnMedium, label, "", 0)
		})
		return layout.Dimensions{Size: image.Pt(w, top+h)}
	}
}

// wfFooter is WorkflowsTabFooterContent: centred, 20px down, "Got
// repetitive tasks? Try workflows." then the links with "·" between them.
// Slack's "Browse templates" opens its template gallery, which Clack
// doesn't have, so the links are Build from scratch (when the workspace
// has Workflow Builder) and Get more info.
func (a *App) wfFooter(st *wfTabState) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		th := a.th
		cl := popups.ColorsOf(th)
		w := gtx.Constraints.Max.X
		type part struct {
			s    string
			c    *widget.Clickable
			link bool
		}
		parts := []part{{s: "Got repetitive tasks? Try workflows."}}
		if a.workflowBuilderEnabled() {
			parts = append(parts, part{s: "Build from scratch", c: &st.buildScratch, link: true})
		}
		parts = append(parts, part{s: "Get more info", c: &st.moreInfo, link: true})
		gapPx, dot := gtx.Dp(8), "·"
		total := 0
		for i, p := range parts {
			total += th.Measure(gtx, 15, theme.Regular, p.s)
			if i > 0 {
				total += gapPx
				if i > 1 {
					total += th.Measure(gtx, 15, theme.Regular, dot) + gapPx
				}
			}
		}
		x, y, lh := (w-total)/2, gtx.Dp(20), gtx.Dp(22)
		for i, p := range parts {
			if i > 1 {
				at(gtx, image.Pt(x, y), image.Pt(w, lh), func(gtx layout.Context) layout.Dimensions {
					return th.Label(gtx, 15, theme.Regular, cl.Fg, dot)
				})
				x += th.Measure(gtx, 15, theme.Regular, dot) + gapPx
			}
			col := cl.Fg
			if p.link {
				col = cl.Link
			}
			pw := th.Measure(gtx, 15, theme.Regular, p.s)
			at(gtx, image.Pt(x, y), image.Pt(pw, lh), func(gtx layout.Context) layout.Dimensions {
				if p.c == nil {
					return th.Label(gtx, 15, theme.Regular, col, p.s)
				}
				return clickArea(gtx, p.c, nil, func(gtx layout.Context) layout.Dimensions {
					d := th.Label(gtx, 15, theme.Regular, col, p.s)
					if p.c.Hovered() {
						theme.Fill(gtx, image.Rect(0, d.Size.Y-gtx.Dp(2), d.Size.X, d.Size.Y-gtx.Dp(1)), col)
					}
					return d
				})
			})
			x += pw + gapPx
		}
		return layout.Dimensions{Size: image.Pt(w, y+lh)}
	}
}

// workflowBuilderEnabled: the workspace's workflow_builder_enabled team
// pref (one of userCanCreateWorkflows' checks, PsN/), true until the
// prefs say otherwise.
func (a *App) workflowBuilderEnabled() bool {
	c := a.Core()
	if c == nil {
		return false
	}
	v, ok := c.TeamPref("workflow_builder_enabled")
	return !ok || string(v) != "false"
}

// buttonWidth measures popups.Button without drawing it (a discarded
// macro).
func buttonWidth(gtx layout.Context, th *theme.Theme, kind popups.BtnKind, size popups.BtnSize, label string) int {
	gtx.Constraints = layout.Constraints{Max: gtx.Constraints.Max} // the row's Min.X would stretch it
	m := op.Record(gtx.Ops)
	var c widget.Clickable
	d := popups.Button(gtx, th, &c, kind, size, label, "", 0)
	_ = m.Stop()
	return d.Size.X
}
