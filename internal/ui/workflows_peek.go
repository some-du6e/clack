package ui

import (
	"image"

	"gioui.org/layout"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/blockkit"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The Workflows tab's peek: WorkflowsPeek (boot-shared q5Cv, build
// 3889f0c93d30) is ChannelTabPeek around the tab's explorer rows
// (useChannelWorkflowsExplorer with isPreview, Jf9W):
//
//   - no featured section, no headers, no CTA cards, no education banner
//     or footer: only the bookmarked workflows (WorkflowItem rows,
//     isFirst/isLast off), a list-loading row while they load;
//   - the rows sit in .content__r_uvR, where .p-search_workflows_item has
//     no border but a 1px otl-ter bottom (none on the last);
//   - contentHeight 73 per row, or 420 when there are none: then
//     .emptyContainer__TCXcl (centred column, gap 8, margin 0 32) with
//     "There are no bookmarked workflows in this channel" (18px black)
//     and, for someone who may manage the channel's workflows
//     (canUserManageChannelWorkflows), an "Add Workflow" button (Button's
//     default primary, medium; margin 8 0) that opens the workflow picker
//     (useCreateWorkflow: source channel_workflows, automated triggers
//     too), bookmarks the pick and goes to the Workflows tab.
type workflowsPeekBody struct {
	a   *App
	p   *slugPeek
	ch  string
	st  *wfTabState
	add widget.Clickable
}

func (a *App) workflowsPeek(p *slugPeek, ch string) tabPeekBody {
	st := &wfTabState{starts: map[string]*widget.Clickable{}, rows: map[string]*widget.Clickable{}, tipStart: map[string]*popups.TipState{}, peek: true}
	st.list.Axis = layout.Vertical
	return &workflowsPeekBody{a: a, p: p, ch: ch, st: st}
}

func (b *workflowsPeekBody) data() (core.WorkflowsTab, bool) {
	b.a.loadWorkflowsTab(b.ch, b.st)
	b.st.mu.Lock()
	defer b.st.mu.Unlock()
	return b.st.tab, b.st.loaded
}

func (b *workflowsPeekBody) height(gtx layout.Context) (int, bool) {
	tab, loaded := b.data()
	switch {
	case !loaded:
		return gtx.Dp(73), true // the list-loading row
	case len(tab.Bookmarked) == 0:
		return gtx.Dp(420), true
	}
	return gtx.Dp(unit.Dp(73 * len(tab.Bookmarked))), true
}

func (b *workflowsPeekBody) layout(gtx layout.Context) layout.Dimensions {
	a := b.a
	th := a.th
	cl := popups.ColorsOf(th)
	sz := gtx.Constraints.Max
	tab, loaded := b.data()
	theme.Fill(gtx, image.Rectangle{Max: sz}, cl.Bg)
	if !loaded {
		// listLoading: a WorkflowItem skeleton (MissingText at 100/180/240)
		pad := gtx.Dp(16)
		at(gtx, image.Pt(pad, pad), image.Pt(gtx.Dp(36), gtx.Dp(36)), func(gtx layout.Context) layout.Dimensions {
			return th.Placeholder(gtx, image.Pt(gtx.Dp(36), gtx.Dp(36)), gtx.Dp(12))
		})
		at(gtx, image.Pt(pad+gtx.Dp(48), pad+gtx.Dp(4)), image.Pt(gtx.Dp(180), gtx.Dp(10)), func(gtx layout.Context) layout.Dimensions {
			return th.Placeholder(gtx, image.Pt(gtx.Dp(180), gtx.Dp(10)), gtx.Dp(4))
		})
		at(gtx, image.Pt(pad+gtx.Dp(48), pad+gtx.Dp(24)), image.Pt(gtx.Dp(240), gtx.Dp(8)), func(gtx layout.Context) layout.Dimensions {
			return th.Placeholder(gtx, image.Pt(gtx.Dp(240), gtx.Dp(8)), gtx.Dp(4))
		})
		return layout.Dimensions{Size: sz}
	}
	if len(tab.Bookmarked) == 0 {
		return b.empty(gtx, tab)
	}
	rows := a.wfRows(b.ch, b.st, tab.Bookmarked, false)
	return b.st.list.Layout(gtx, len(rows), func(gtx layout.Context, i int) layout.Dimensions {
		gtx.Constraints.Min.X = sz.X
		return rows[i](gtx)
	})
}

// empty is the peek's empty state.
func (b *workflowsPeekBody) empty(gtx layout.Context, tab core.WorkflowsTab) layout.Dimensions {
	a := b.a
	th := a.th
	cl := popups.ColorsOf(th)
	sz := gtx.Constraints.Max
	title := i18n.T("tabbed-channel", "There are no bookmarked workflows in this channel")
	label := i18n.T("tabbed-channel", "Add Workflow")
	w := max(1, sz.X-2*gtx.Dp(32))
	call, td := measured(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, sz.Y)}
		return th.Layout(gtx, theme.TextStyle{Size: 18, LineHeight: 24, Weight: theme.Black, Color: cl.Fg, Align: text.Middle}, title)
	})
	h := td.Size.Y
	bh, bw := gtx.Dp(36), 0
	if tab.CanManage {
		bw = buttonWidth(gtx, th, popups.BtnPrimary, popups.BtnMedium, label)
		h += gtx.Dp(8) + gtx.Dp(8) + bh + gtx.Dp(8) // gap 8, margin 8 0
	}
	y := (sz.Y - h) / 2
	place(gtx, call, image.Pt(gtx.Dp(32), y))
	if tab.CanManage {
		if b.add.Clicked(gtx) {
			b.addWorkflow(tab)
		}
		at(gtx, image.Pt((sz.X-bw)/2, y+td.Size.Y+gtx.Dp(16)), image.Pt(bw, bh), func(gtx layout.Context) layout.Dimensions {
			return popups.Button(gtx, th, &b.add, popups.BtnPrimary, popups.BtnMedium, label, "", 0)
		})
	}
	return layout.Dimensions{Size: sz}
}

// addWorkflow is useCreateWorkflow(channelId): the picker, then the
// bookmark (addWorkflowBookmarkForChannel, reason channel-tab-add-bookmark)
// and the channel's Workflows tab.
func (b *workflowsPeekBody) addWorkflow(tab core.WorkflowsTab) {
	a, ch := b.a, b.ch
	var ex []string
	for _, w := range tab.Bookmarked {
		ex = append(ex, w.Trigger.ID)
	}
	b.p.done = true
	blockkit.OpenWorkflowPicker(a, blockkit.WorkflowPickerOpts{Source: blockkit.PickerChannelWorkflows, ChannelID: ch, IncludeAutomated: true,
		Excluded: ex, OnInsert: func(t core.WorkflowTrigger) {
			a.addWorkflowBookmark(ch, t, t.ShortcutURL)
			a.openWorkflowsTab(ch)
			a.Navigate("c/" + ch)
		}})
}

// peekText is what the peek shows (tests).
func (b *workflowsPeekBody) peekText() string {
	tab, loaded := b.data()
	switch {
	case !loaded:
		return "loading"
	case len(tab.Bookmarked) == 0:
		return "empty"
	}
	out := ""
	for i, w := range tab.Bookmarked {
		if i > 0 {
			out += " | "
		}
		out += w.Trigger.Title()
	}
	return out
}
