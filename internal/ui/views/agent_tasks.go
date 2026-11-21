package views

// The agent DM's Tasks tab: Slack's AgentTasksTabView (the
// agent-tasks-tab-view chunk's I66T, AgentTasksTabViewContent) for a
// third-party agent (tab type App3pAgentTasks), its explorer rows from
// useAgentTasksExplorer and boot-shared NQBk's AgentTaskItem. Build
// 9685e6ce144b (2026-11-18); docs/APP-TASKS.md names the modules and CSS.
//
//   - The header (ExplorerHeader with explorerHeader__JyK7k: 16px over and
//     under, the container at most 1000 wide with 16px insets): "Tasks"
//     (tabTitle__tTPP1: 18px Bold, line-height 1.2, 8px into the inset
//     by tabHeader__aG_ot's margin; Black under wfb_agent_tasks_recent_
//     prompts, tabTitleHeavy). The Add Task button and its Create task
//     modal (TaskModal) aren't in Clack; neither is Edit task.
//   - The search (the sticky results header, only while the tab isn't in
//     its empty state): SearchInput "Search your tasks", 16px under it,
//     at most 968 wide (p-explorer_search__content). It filters the loaded
//     tasks by title as you type; with more pages than the first it also
//     asks the server (functions.workflows.list name), 300ms after typing.
//   - The rows (p-search_workflows_item p-search_workflows_agent_task_item,
//     at most 968 wide, cards whose first and last round off by 12): the
//     36px icon box (indigo-5, the ai-workflow glyph in indigo-60; a
//     paused task's in gray with the pause-filled badge), the bold title
//     with PAUSED after it, the schedule line (core.TaskScheduleText). On
//     hover an ActionsMenu: Run task now (play), Pause task (pause) /
//     Restart task (rotate), More actions (Delete task: "Permanently
//     delete this task?", Confirm). Published tasks first, then by title.
//   - Loading is the explorer's twelve SearchWorkflowSkeleton rows; an
//     error the empty-state row "There was a problem loading tasks." with
//     Retry; no tasks ThirdPartyAgentTasksEmptyState's card ("Set
//     repetitive tasks on autopilot"); a search with none "No tasks
//     found". More pages load as the list's end comes into view.
//   - Live: core's agent_task events (workflow, hermes_trigger_updated)
//     drop a deleted task at once and read the list again otherwise.

import (
	"context"
	"image"
	"image/color"
	"math"
	"strings"
	"sync"
	"time"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

var (
	nsSlackbotAI = i18n.NS("slackbot-ai")       // the tab, rows, empty states
	nsShortcuts  = i18n.NS("shortcuts_gallery") // ConfirmDeleteWorkflowModal
)

// AgentTasksTexts are Slack's strings (en-US) the tab draws; tests read
// them.
const (
	AgentTasksTitle        = "Tasks"
	AgentTasksSearch       = "Search your tasks"
	AgentTasksEmptyTitle   = "Set repetitive tasks on autopilot"
	AgentTasksEmptyBody    = "Add a task to have this agent run a prompt on a schedule."
	AgentTasksErrorTitle   = "There was a problem loading tasks."
	AgentTasksErrorBody    = "Give it another try in a few minutes."
	AgentTasksNoMatchTitle = "No tasks found"
	AgentTasksNoMatchBody  = "Try a different search term."
	AgentTasksPaused       = "PAUSED"
	AgentTasksFailed       = "Something went wrong. Please try again."
)

// AgentTasks is one agent DM's Tasks tab.
type AgentTasks struct {
	base
	ch string

	mu      sync.Mutex
	tasks   []core.AgentTask // the loaded pages, as the server sent them
	cursor  string           // the next page ("" none)
	loaded  bool
	loading bool
	failed  bool
	gen     int    // drops answers of an older request
	query   string // the query the server answered (more pages only)
	stale   bool   // a live event asked for the list again
	// removed are the tasks deleted here or by a live event, by the list
	// generation (gen) current when they went: a first page keeps those
	// at or after its own request, so an answer read before a delete
	// committed can't bring the task back (Slack removes the item on the
	// delete's success and on workflowChanged)
	removed map[string]int
	// override is the optimistic is_published of a Pause or Restart: in
	// flight, and after its success until a list agrees or 30 s pass
	// (vRD3 holdPublishedState); holds numbers each hold, so an old timer
	// can't end a newer one
	override map[string]bool
	holds    map[string]int
	inflight map[string]bool
	ui       []func() // results applied on the UI goroutine
	subbed   bool

	srch       *search
	typed      string
	typedAt    time.Time
	asked      string // the query last sent to the server
	debounced  string // the query a debounce timer is out for
	list       widget.List
	retry      widget.Clickable
	rows       map[string]*taskRowUI
	forceHover string // HoverForTest
	glow       paint.ImageOp
	glowOK     bool
	glowAt     glowKey
	shown      shownTracker
}

type taskRowUI struct {
	run, pause, more         widget.Clickable
	tipRun, tipPause, tipMor popups.TipState
	hover                    widget.Clickable // the row's hover area
	menuOpen                 bool
	moreSize                 image.Point
}

// RowHoveredForTest and ActionHoveredForTest are whether the pointer is
// on task id's row, and on its hover action (run, pause, more), as the
// router last told the row (tests that click them as a person would).
func (v *AgentTasks) RowHoveredForTest(id string) bool {
	u := v.rows[id]
	return u != nil && u.hover.Hovered()
}

func (v *AgentTasks) ActionHoveredForTest(id, action string) bool {
	u := v.rows[id]
	if u == nil {
		return false
	}
	switch action {
	case "run":
		return u.run.Hovered()
	case "pause":
		return u.pause.Hovered()
	case "more":
		return u.more.Hovered()
	}
	return false
}

// NewAgentTasks makes the Tasks tab of the agent DM ch.
func NewAgentTasks(h Host, ch string) *AgentTasks {
	v := &AgentTasks{base: newBase(h), ch: ch, srch: newSearchIn(nsSlackbotAI, AgentTasksSearch),
		removed: map[string]int{}, override: map[string]bool{}, holds: map[string]int{}, inflight: map[string]bool{}, rows: map[string]*taskRowUI{}}
	v.list.Axis = layout.Vertical
	return v
}

// Channel is the DM the tab belongs to.
func (v *AgentTasks) Channel() string { return v.ch }

// agent is the DM's agent: its bot user and app id ("" until known: the
// tab draws its skeleton, Slack's O).
func (v *AgentTasks) agent() (user, app string) {
	c := v.h.Core()
	if c == nil {
		return "", ""
	}
	return c.AgentTasksAgent(v.ch)
}

// post queues f for the UI goroutine and redraws.
func (v *AgentTasks) post(f func()) {
	v.mu.Lock()
	v.ui = append(v.ui, f)
	v.mu.Unlock()
	v.h.Invalidate()
}

func (v *AgentTasks) drain() {
	v.mu.Lock()
	fs := v.ui
	v.ui = nil
	v.mu.Unlock()
	for _, f := range fs {
		f()
	}
}

// subscribe follows core's agent_task events (workflowChanged,
// workflowTriggerUpdated): a deleted workflow leaves the list at once,
// anything else reads it again.
func (v *AgentTasks) subscribe() {
	v.mu.Lock()
	if v.subbed {
		v.mu.Unlock()
		return
	}
	v.subbed = true
	v.mu.Unlock()
	ch, _ := v.h.Core().Subscribe()
	go func() {
		for ev := range ch {
			if ev.Type != "agent_task" {
				continue
			}
			v.mu.Lock()
			if ev.Name == "deleted" && ev.TS != "" {
				v.removed[ev.TS] = v.gen
			} else {
				v.stale = true
			}
			v.mu.Unlock()
			if v.shown.shown(v.h) {
				v.h.Invalidate()
			}
		}
	}()
}

// Refresh reads the list again (resetQueryAndFetch).
func (v *AgentTasks) Refresh() {
	v.mu.Lock()
	v.stale = true
	v.mu.Unlock()
	v.h.Invalidate()
}

// load reads the first page for query (or the next page with more);
// retry is the error row's Retry (past the client's failure backoff).
func (v *AgentTasks) load(app, query string, more bool, retry ...bool) {
	c := v.h.Core()
	v.mu.Lock()
	if more && (v.loading || v.cursor == "") {
		v.mu.Unlock()
		return
	}
	v.gen++
	gen, cursor := v.gen, ""
	if more {
		cursor = v.cursor
	}
	v.loading, v.stale = true, false
	v.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var page core.AgentTasksPage
		var err error
		if len(retry) > 0 && retry[0] {
			page, err = c.AgentTasksRetry(ctx, app, query)
		} else {
			page, err = c.AgentTasks(ctx, app, query, cursor)
		}
		v.post(func() {
			v.mu.Lock()
			defer v.mu.Unlock()
			if gen != v.gen {
				return
			}
			v.loading = false
			if err != nil {
				v.failed = true
				return
			}
			v.failed, v.loaded, v.query = false, true, query
			if more {
				v.tasks = append(v.tasks, page.Tasks...)
				v.releaseHolds(page.Tasks)
			} else {
				v.tasks = page.Tasks
				v.releaseHolds(page.Tasks)
				for id, g := range v.removed {
					if g < gen {
						delete(v.removed, id) // the page was asked for after it went
					}
				}
			}
			v.cursor = page.Cursor
		})
	}()
}

// Tasks returns the rows as the tab draws them: the live list without
// removed tasks, the optimistic publish states applied, sorted and
// filtered by the search.
func (v *AgentTasks) Tasks() []core.AgentTask {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.rowsLocked(v.typed)
}

func (v *AgentTasks) rowsLocked(q string) []core.AgentTask {
	out := make([]core.AgentTask, 0, len(v.tasks))
	seen := map[string]bool{}
	for _, t := range v.tasks {
		if _, gone := v.removed[t.WorkflowID]; gone || seen[t.WorkflowID] {
			continue
		}
		seen[t.WorkflowID] = true
		if p, ok := v.override[t.WorkflowID]; ok {
			t.Published, t.Known = p, true
		}
		out = append(out, t)
	}
	core.SortAgentTasks(out)
	if q = strings.ToLower(strings.TrimSpace(q)); q != "" {
		f := out[:0]
		for _, t := range out {
			if strings.Contains(strings.ToLower(t.Title), q) {
				f = append(f, t)
			}
		}
		out = f
	}
	return out
}

// State is what the tab shows: "skeleton" (the agent or its first page
// isn't in), "error", "empty", "no-match" or "list".
func (v *AgentTasks) State() string {
	_, app := v.agent()
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.stateLocked(app)
}

func (v *AgentTasks) stateLocked(app string) string {
	switch {
	case app == "":
		return "skeleton"
	case v.failed && !v.loading:
		return "error"
	case !v.loaded:
		return "skeleton"
	}
	if len(v.rowsLocked("")) == 0 && strings.TrimSpace(v.typed) == "" && v.query == "" {
		return "empty"
	}
	if len(v.rowsLocked(v.typed)) == 0 && !v.loading {
		return "no-match"
	}
	return "list"
}

func (v *AgentTasks) rowUI(id string) *taskRowUI {
	u := v.rows[id]
	if u == nil {
		u = &taskRowUI{}
		v.rows[id] = u
	}
	return u
}

// Layout draws the tab.
func (v *AgentTasks) Layout(gtx C) D {
	v.frame()
	v.shown.mark(v.h)
	v.drain()
	sz := gtx.Constraints.Max
	fill(gtx, image.Rectangle{Max: sz}, v.p.Bg)
	c := v.h.Core()
	if c == nil {
		return D{Size: sz}
	}
	v.subscribe()
	user, app := v.agent()

	// the search: local filter as you type, the server's name filter
	// (300ms debounce) once there is more than one page
	for {
		ev, ok := v.srch.ed.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.ChangeEvent); ok {
			v.typed, v.typedAt = v.srch.ed.Text(), gtx.Now
		}
	}
	v.mu.Lock()
	start := app != "" && (!v.loaded && !v.loading && !v.failed || v.stale && !v.loading)
	paged := v.cursor != "" || v.query != ""
	v.mu.Unlock()
	q := strings.TrimSpace(v.typed)
	if start {
		v.load(app, v.asked, false)
	} else if app != "" && paged && q != v.asked && gtx.Now.Sub(v.typedAt) >= 300*time.Millisecond {
		v.asked = q
		v.load(app, q, false)
	} else if app != "" && paged && q != v.asked && v.debounced != q {
		v.debounced = q
		time.AfterFunc(300*time.Millisecond, v.h.Invalidate) // no mid-frame InvalidateCmd
	}
	if v.retry.Clicked(gtx) && app != "" {
		v.mu.Lock()
		v.failed = false
		v.mu.Unlock()
		v.load(app, v.asked, false, true)
	}

	v.mu.Lock()
	state := v.stateLocked(app)
	rows := v.rowsLocked(v.typed)
	more := v.cursor != ""
	v.mu.Unlock()

	var items []W
	items = append(items, v.header)
	switch state {
	case "skeleton":
		items = append(items, v.skeletonRows)
	case "error":
		items = append(items, func(gtx C) D {
			return v.emptyRow(gtx, nsSlackbotAI.T(AgentTasksErrorTitle), nsSlackbotAI.T(AgentTasksErrorBody), &v.retry, nsSlackbotAI.T("Retry"))
		})
	case "empty":
		items = append(items, v.emptyCard)
	default:
		items = append(items, v.searchRow)
		if state == "no-match" {
			items = append(items, func(gtx C) D {
				return v.emptyRow(gtx, nsSlackbotAI.T(AgentTasksNoMatchTitle), nsSlackbotAI.T(AgentTasksNoMatchBody), nil, "")
			})
		}
		for i := range rows {
			t, first, last := rows[i], i == 0, i == len(rows)-1
			items = append(items, func(gtx C) D { return v.taskRow(gtx, t, user, first, last) })
		}
		if more {
			items = append(items, func(gtx C) D {
				v.load(app, v.asked, true) // the infinite-pagination row came into view
				return v.skeletonRow(gtx, 0, true, true)
			})
		}
		items = append(items, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Max.X, gtx.Dp(16))} }) // p-explorer_results__container's 16px
	}
	return v.scrollList(gtx, &v.list, len(items), func(gtx C, i int) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return items[i](gtx)
	})
}

// contentW is the results' width: calc(100% - 32px), at most 968.
func contentW(gtx C, w int) int { return max(0, min(gtx.Dp(968), w-gtx.Dp(32))) }

// header is ExplorerHeader: "Tasks" 16px down, 8px into the container's
// 16px inset.
func (v *AgentTasks) header(gtx C) D {
	w := gtx.Constraints.Max.X
	cw := min(w, gtx.Dp(1000))
	x := (w-cw)/2 + gtx.Dp(16) - gtx.Dp(8)
	lh := gtx.Dp(22) // 18px × 1.2
	h := gtx.Dp(16) + lh + gtx.Dp(16)
	weight := wBold
	if c := v.h.Core(); c != nil && c.Experiment("wfb_agent_tasks_recent_prompts") == "on" {
		weight = wBlack // tabTitleHeavy
	}
	dirAt(gtx, image.Pt(x, gtx.Dp(16)), func(gtx C) D {
		gtx.Constraints.Max.X = max(0, cw-gtx.Dp(24))
		return v.th.Layout(gtx, theme.TextStyle{Size: 18, LineHeight: 22, Weight: weight, Color: v.p.Text, MaxLines: 1}, nsSlackbotAI.T(AgentTasksTitle))
	})
	return D{Size: image.Pt(w, h)}
}

// searchRow is the sticky results header: the SearchInput (medium, 36
// tall) with 16px under it.
func (v *AgentTasks) searchRow(gtx C) D {
	w := gtx.Constraints.Max.X
	iw := contentW(gtx, w)
	x := (w - iw) / 2
	h := gtx.Dp(36)
	dirAt(gtx, image.Pt(x, 0), func(gtx C) D {
		gtx.Constraints = layout.Exact(image.Pt(iw, h))
		return v.searchBox(gtx, v.srch, 36)
	})
	return D{Size: image.Pt(w, h+gtx.Dp(16))}
}

// Slack's palette tokens per mode (--dt_color-plt-*: the live client's
// light and dark values) for the row's icon box and paused badge.
var (
	tasksIndigo5  = [2]color.NRGBA{rgb(0xdce3fa), rgb(0x0a165a)}
	tasksIndigo60 = [2]color.NRGBA{rgb(0x3651c0), rgb(0x657ace)}
	tasksGray5    = [2]color.NRGBA{rgb(0xeaeaea), rgb(0x1a1d21)}
	tasksGray20   = [2]color.NRGBA{rgb(0xcbcccf), rgb(0x323538)}
	tasksGray30   = [2]color.NRGBA{rgb(0xb5b5ba), rgb(0x414549)}
	tasksGray60   = [2]color.NRGBA{rgb(0x5e5d60), rgb(0x9a9b9e)}
	tasksInvPry   = [2]color.NRGBA{rgb(0xffffff), rgb(0xf8f8f8)} // content-inv-pry
	tasksSec      = [2]color.NRGBA{rgb(0x454447), rgb(0xb9babd)} // content-sec
	tasksGlow     = [2]color.NRGBA{rgb(0x1264a3), rgb(0x0a77a7)} // base-inv-hgl-1
)

func (v *AgentTasks) mode() int {
	if v.th != nil && v.th.Dark {
		return 1
	}
	return 0
}

// taskRow is AgentTaskItem.
func (v *AgentTasks) taskRow(gtx C, t core.AgentTask, user string, first, last bool) D {
	w := gtx.Constraints.Max.X
	rw := contentW(gtx, w)
	x0 := (w - rw) / 2
	u := v.rowUI(t.WorkflowID)
	m := v.mode()
	c := v.h.Core()
	self := ""
	if c != nil {
		self = c.Self().ID
	}

	// the buttons' clicks first (the row's hover area mustn't take them)
	if u.run.Clicked(gtx) && t.CanRunNow() {
		v.runNow(user, t)
	}
	if u.pause.Clicked(gtx) {
		v.setPublished(t, t.Paused())
	}
	if u.more.Clicked(gtx) {
		v.openMore(t, self, u)
	}

	pad, is := gtx.Dp(12), gtx.Dp(36)
	nameH, descH := gtx.Dp(22), gtx.Dp(18)
	h := 2*pad + max(is, nameH+descH)
	rad := gtx.Dp(12)
	hover := u.hover.Hovered() || u.run.Hovered() || u.pause.Hovered() || u.more.Hovered() || u.menuOpen || v.forceHover == t.WorkflowID

	defer op.Offset(image.Pt(x0, 0)).Push(gtx.Ops).Pop()
	r := image.Rect(0, 0, rw, h)
	// the card: rows share their borders; the first and last round off
	line := v.p.CardLine
	if hover {
		line = theme.WithAlpha(v.p.Text, 0x26) // :hover rgba(fg, .15)
		theme.Shadow(gtx, r.Add(image.Pt(0, gtx.Dp(1))), 0, gtx.Dp(1), color.NRGBA{A: 0x0f})
	}
	radii := clip.RRect{Rect: r}
	if first {
		radii.NW, radii.NE = rad, rad
	}
	if last {
		radii.SW, radii.SE = rad, rad
	}
	func() {
		defer radii.Push(gtx.Ops).Pop()
		fill(gtx, r, line)
	}()
	in := r
	in.Min.X++
	in.Max.X--
	in.Min.Y++
	if last {
		in.Max.Y--
	}
	innerR := clip.RRect{Rect: in}
	if first {
		innerR.NW, innerR.NE = rad-1, rad-1
	}
	if last {
		innerR.SW, innerR.SE = rad-1, rad-1
	}
	func() {
		defer innerR.Push(gtx.Ops).Pop()
		fill(gtx, in, v.p.Bg)
	}()

	// the icon box
	paused := t.Paused()
	box := image.Rect(pad, (h-is)/2, pad+is, (h-is)/2+is)
	bg, fg := tasksIndigo5[m], tasksIndigo60[m]
	if paused {
		bg, fg = tasksGray5[m], tasksGray60[m]
		if m == 1 {
			bg = tasksGray20[m] // light-dark(gray-5, gray-20)
		}
	}
	rrect(gtx, box, gtx.Dp(8), bg)
	gs := gtx.Dp(24)
	dirAt(gtx, box.Min.Add(image.Pt((is-gs)/2, (is-gs)/2)), func(gtx C) D { return v.icon(gtx, "workflow", 24, fg) })
	if paused { // --paused_overlay: 16px, bottom -2, right -6, a 1px ring
		ov := image.Rect(box.Max.X+gtx.Dp(6)-gtx.Dp(16), box.Max.Y+gtx.Dp(2)-gtx.Dp(16), box.Max.X+gtx.Dp(6), box.Max.Y+gtx.Dp(2))
		ring := rgb(0xffffff)
		if m == 1 {
			ring = tasksGray5[m]
		}
		rrect(gtx, ov, ov.Dx()/2, ring)
		rrect(gtx, ov.Inset(max(1, gtx.Dp(1))), (ov.Dx()-2*max(1, gtx.Dp(1)))/2, tasksGray30[m])
		ps := gtx.Dp(10)
		dirAt(gtx, ov.Min.Add(image.Pt((ov.Dx()-ps)/2, (ov.Dy()-ps)/2)), func(gtx C) D { return v.icon(gtx, "pause", 10, tasksInvPry[m]) })
	}

	// the title (with PAUSED) over the schedule line
	tx := pad + is + gtx.Dp(12)
	tw := max(0, rw-tx-pad)
	ty := (h - nameH - descH) / 2
	badge := nsSlackbotAI.T(AgentTasksPaused)
	bw := 0
	if paused {
		bw = v.th.Measure(gtx, 10, wBold, badge) + 2*gtx.Dp(3) + gtx.Dp(8)
	}
	name := t.Title
	nw := min(v.th.Measure(gtx, 15, wBold, name), max(0, tw-bw))
	dirAt(gtx, image.Pt(tx, ty), func(gtx C) D {
		gtx.Constraints.Max.X = nw
		return v.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: wBold, Color: v.p.Text, MaxLines: 1}, name)
	})
	if paused { // --unpublished_badge: 10px Bold on 1.25, 14 tall, padding 1 3, radius 2, 8px after the title
		bx := tx + nw + gtx.Dp(8)
		bh := gtx.Dp(14)
		by := ty + (nameH-bh)/2 - gtx.Dp(2) // vertical-align .125rem
		br := image.Rect(bx, by, bx+bw-gtx.Dp(8), by+bh)
		rrect(gtx, br, gtx.Dp(2), theme.WithAlpha(v.p.Text, 0x21))
		dirAt(gtx, image.Pt(bx+gtx.Dp(3), by+gtx.Dp(1)), func(gtx C) D {
			return v.th.Layout(gtx, theme.TextStyle{Size: 10, LineHeight: 12, Weight: wBold, Color: theme.WithAlpha(v.p.Text, 0xb3), MaxLines: 1}, badge)
		})
	}
	desc := core.TaskScheduleText(t, v.textOpts())
	dirAt(gtx, image.Pt(tx, ty+nameH), func(gtx C) D {
		gtx.Constraints.Max.X = tw
		return v.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Weight: wReg, Color: tasksSec[m], MaxLines: 1}, desc)
	})

	// the hover area, then the ActionsMenu over the text (absolute)
	func() {
		g := gtx
		g.Constraints = layout.Exact(image.Pt(rw, h))
		u.hover.Layout(g, func(gtx C) D { return D{Size: image.Pt(rw, h)} })
	}()
	if hover {
		v.rowActions(gtx, t, u, rw, h, pad)
	}
	if last {
		return D{Size: image.Pt(w, h)}
	}
	return D{Size: image.Pt(w, h-1)} // the next row's top border is this row's bottom
}

// textOpts are the row's schedule-line switches: the viewer's time24
// pref and the experiments AgentTaskItem passes.
func (v *AgentTasks) textOpts() core.TaskTextOpts {
	o := core.TaskTextOpts{}
	if c := v.h.Core(); c != nil {
		o.Time24 = c.Prefs().Time24
		o.TimeBuckets = c.Experiment("wfb_scheduled_trigger_time_buckets") == "on"
		o.LastDayOfMonth = c.Experiment("wfb_schedule_on_last_day_of_month") == "on"
		o.Window = c.Experiment("wfb_agents_can_create_simple_automations_tab_ms3") == "on"
	}
	return o
}

// rowActions is the row's ActionsMenu (showOnlyOnHover): 32px buttons
// (18px glyphs) in a padded card at the row's right, centred.
func (v *AgentTasks) rowActions(gtx C, t core.AgentTask, u *taskRowUI, w, h, pad int) {
	type btn struct {
		c    *widget.Clickable
		tip  *popups.TipState
		icon string
		text string
	}
	var bs []btn
	for _, a := range taskActions(t) {
		switch a.key {
		case "run":
			bs = append(bs, btn{&u.run, &u.tipRun, a.icon, a.label})
		case "pause", "restart":
			bs = append(bs, btn{&u.pause, &u.tipPause, a.icon, a.label})
		default:
			bs = append(bs, btn{&u.more, &u.tipMor, a.icon, a.label})
		}
	}
	bsz, ins := gtx.Dp(32), gtx.Dp(4)
	card := image.Rect(0, 0, 2*ins+len(bs)*bsz, 2*ins+bsz)
	card = card.Add(image.Pt(w-pad-card.Dx(), (h-card.Dy())/2))
	theme.Shadow(gtx, card.Add(image.Pt(0, gtx.Dp(1))), gtx.Dp(12), gtx.Dp(3), theme.WithAlpha(v.th.C.PanelShadow, 0x14))
	rrect(gtx, card.Inset(-max(1, gtx.Dp(1))), gtx.Dp(12)+1, v.p.Ring)
	rrect(gtx, card, gtx.Dp(12), v.p.Bg)
	for i, b := range bs {
		p := image.Pt(card.Min.X+ins+i*bsz, card.Min.Y+ins)
		b := b
		dirAt(gtx, p, func(gtx C) D {
			gtx.Constraints = layout.Exact(image.Pt(bsz, bsz))
			d := b.c.Layout(gtx, func(gtx C) D {
				if b.c.Hovered() {
					rrect(gtx, image.Rect(0, 0, bsz, bsz), gtx.Dp(8), v.p.Page) // --action_button:hover base-sec
				}
				col := v.p.Sec
				if b.c.Hovered() {
					col = v.p.Text
				}
				off := (bsz - gtx.Dp(18)) / 2
				dirAt(gtx, image.Pt(off, off), func(gtx C) D { return v.icon(gtx, b.icon, 18, col) })
				pointerCursor(gtx)
				return D{Size: image.Pt(bsz, bsz)}
			})
			popups.Tooltip(gtx, v.th, b.tip, b.c.Hovered(), d.Size, popups.Above, b.text, "")
			return d
		})
		if b.c == &u.more {
			u.moreSize = image.Pt(bsz, bsz)
		}
	}
}

// taskAction is one of the row's ActionsMenu buttons.
type taskAction struct{ key, icon, label string }

// taskActions is AgentTaskItem's et for a third-party agent's task in
// the Tasks tab (showUnpublished): Run task now while canRunNow and not
// paused, Pause task or Restart task, then More actions. Edit task (the
// edit task modal) isn't in Clack.
func taskActions(t core.AgentTask) []taskAction {
	var out []taskAction
	if t.CanRunNow() {
		out = append(out, taskAction{"run", "play-line", nsSlackbotAI.T("Run task now")})
	}
	if t.WorkflowID != "" {
		if t.Paused() {
			out = append(out, taskAction{"restart", "rotate", nsSlackbotAI.T("Restart task")})
		} else {
			out = append(out, taskAction{"pause", "pause-line", nsSlackbotAI.T("Pause task")})
		}
	}
	return append(out, taskAction{"more", "kebab", nsSlackbotAI.T("More actions")})
}

// RowActions lists the row's action labels (tests).
func RowActions(t core.AgentTask) []string {
	var out []string
	for _, a := range taskActions(t) {
		out = append(out, a.label)
	}
	return out
}

// HoverForTest draws the row of workflow id as hovered (its ActionsMenu
// shown) without a pointer: the shots' hover state.
func (v *AgentTasks) HoverForTest(id string) {
	v.forceHover = id
	v.h.Invalidate()
}

// Retry is the error row's Retry (tests).
func (v *AgentTasks) Retry() {
	_, app := v.agent()
	if app == "" {
		return
	}
	v.mu.Lock()
	v.failed = false
	v.mu.Unlock()
	v.load(app, v.asked, false, true)
}

// Removed reports whether a live event (or Delete task) took the task
// with workflow id out of the list (tests).
func (v *AgentTasks) Removed(id string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	_, gone := v.removed[id]
	return gone
}

// buttonRect is the clicked button's window rectangle (the pointer less
// its offset in the button).
func (v *AgentTasks) buttonRect(c *widget.Clickable, size image.Point) image.Rectangle {
	var at image.Point
	if pp, ok := v.h.(interface{ PointerPos() image.Point }); ok {
		at = pp.PointerPos()
	}
	if hs := c.History(); len(hs) > 0 {
		at = at.Sub(hs[len(hs)-1].Position)
	}
	return image.Rectangle{Min: at, Max: at.Add(size)}
}

// runNow is useRunTaskNow's runTaskNow for a third-party agent's task
// (runPromptFor3pAgent): the prompt goes to the agent's DM as the
// viewer's message and the DM's Messages tab opens on it; a failure
// toasts "Something went wrong. Please try again."
func (v *AgentTasks) runNow(user string, t core.AgentTask) {
	c := v.h.Core()
	if c == nil || user == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		ch, ts, err := c.RunAgentTaskNow(ctx, user, t)
		v.post(func() {
			if err != nil {
				v.h.Toast(nsSlackbotAI.T(AgentTasksFailed))
				return
			}
			if o, ok := v.h.(interface{ OpenAppDMMessages(ch, ts string) }); ok {
				o.OpenAppDMMessages(ch, ts)
			}
		})
	}()
}

// setPublished is setTaskPublished: the row shows the new state at once,
// one request per task at a time; a failure puts the old state back and
// toasts "Something went wrong. Please try again."
func (v *AgentTasks) setPublished(t core.AgentTask, publish bool) {
	c := v.h.Core()
	if c == nil || t.WorkflowID == "" {
		return
	}
	id := t.WorkflowID
	v.mu.Lock()
	if v.inflight[id] {
		v.mu.Unlock()
		return
	}
	v.inflight[id] = true
	v.override[id] = publish
	v.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := c.SetAgentTaskPublished(ctx, id, publish)
		v.post(func() {
			v.mu.Lock()
			delete(v.inflight, id)
			if err != nil {
				delete(v.override, id) // the old state, at once
			} else {
				for i := range v.tasks {
					if v.tasks[i].WorkflowID == id {
						v.tasks[i].Published, v.tasks[i].Known = publish, true
					}
				}
				// markSucceeded: the state holds over a list that hasn't
				// caught up, until one agrees or 30 s pass, then the task
				// reads again
				v.holds[id]++
				n := v.holds[id]
				time.AfterFunc(agentTaskHold, func() {
					v.post(func() {
						v.mu.Lock()
						if v.holds[id] == n && !v.inflight[id] {
							delete(v.override, id)
							delete(v.holds, id)
							v.stale = true
						}
						v.mu.Unlock()
					})
				})
			}
			v.mu.Unlock()
			if err != nil {
				v.h.Toast(nsSlackbotAI.T(AgentTasksFailed))
			}
		})
	}()
}

// agentTaskHold is how long a Pause or Restart that succeeded holds its
// state over lists that disagree (holdPublishedState's setTimeout 3e4).
const agentTaskHold = 30 * time.Second

// releaseHolds ends the holds a list agrees with (v.mu held): a task it
// reports in the requested state, with no request in flight.
func (v *AgentTasks) releaseHolds(ts []core.AgentTask) {
	for _, t := range ts {
		if p, ok := v.override[t.WorkflowID]; ok && !v.inflight[t.WorkflowID] && v.holds[t.WorkflowID] > 0 && t.Published == p {
			delete(v.override, t.WorkflowID)
			delete(v.holds, t.WorkflowID)
		}
	}
}

// moreItems is AgentTaskMoreActionsMenu for a third-party agent's task:
// Delete task (danger, trash) when the viewer may delete it; Edit task
// with Slackbot is Slackbot's tasks only.
func (v *AgentTasks) moreItems(t core.AgentTask, self string) []popups.MenuItem {
	if !t.CanDelete(self) {
		return nil
	}
	return []popups.MenuItem{{Key: "delete_automation", Label: nsSlackbotAI.T("Delete task"), Icon: "trash", Danger: true,
		Do: func() { v.confirmDelete(t) }}}
}

// MoreItems is the row's More actions menu (tests).
func (v *AgentTasks) MoreItems(t core.AgentTask) []popups.MenuItem {
	self := ""
	if c := v.h.Core(); c != nil {
		self = c.Self().ID
	}
	return v.moreItems(t, self)
}

func (v *AgentTasks) openMore(t core.AgentTask, self string, u *taskRowUI) {
	ph, ok := v.h.(popups.Host)
	items := v.moreItems(t, self)
	if !ok || len(items) == 0 {
		return
	}
	u.menuOpen = true
	ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: v.buttonRect(&u.more, u.moreSize), AlignRight: true,
		OnClose: func() { u.menuOpen = false; v.h.Invalidate() }}))
}

// ConfirmDelete opens the Delete task confirm (tests and the menu).
func (v *AgentTasks) ConfirmDelete(t core.AgentTask) { v.confirmDelete(t) }

// confirmDelete is ConfirmDeleteWorkflowModal (variant automation): its
// Confirm deletes the trigger and the workflow; the row leaves on
// success, a failure toasts "Something went wrong. Please try again."
func (v *AgentTasks) confirmDelete(t core.AgentTask) {
	ph, ok := v.h.(popups.Host)
	c := v.h.Core()
	if !ok || c == nil {
		return
	}
	popups.Confirm(ph, nsShortcuts.T("Permanently delete this task?"),
		nsShortcuts.T("When you delete this task, it’ll stop working and you won’t be able to use it again."),
		nsDialog.T("Confirm"), false, nil, func() {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				err := c.DeleteAgentTask(ctx, t.TriggerID, t.WorkflowID)
				v.post(func() {
					if err != nil {
						v.h.Toast(nsSlackbotAI.T(AgentTasksFailed))
						return
					}
					v.mu.Lock()
					v.removed[t.WorkflowID] = v.gen
					v.mu.Unlock()
				})
			}()
		})
}

// RunNow, SetPublished and Delete are the row actions (tests).
func (v *AgentTasks) RunNow(t core.AgentTask) {
	user, _ := v.agent()
	if t.CanRunNow() {
		v.runNow(user, t)
	}
}

func (v *AgentTasks) SetPublished(t core.AgentTask, publish bool) { v.setPublished(t, publish) }

// skeletonRows is the explorer's list loading: twelve
// SearchWorkflowSkeleton rows (first, ten, last).
func (v *AgentTasks) skeletonRows(gtx C) D {
	w := gtx.Constraints.Max.X
	y := 0
	for i := 0; i < 12; i++ {
		d := func() D {
			defer op.Offset(image.Pt(0, y)).Push(gtx.Ops).Pop()
			return v.skeletonRow(gtx, i, i == 0, i == 11)
		}()
		y += d.Size.Y
	}
	return D{Size: image.Pt(w, y)}
}

// skeletonRow is SearchWorkflowSkeleton: a p-search_workflows_item (16px
// in) with the 36px icon placeholder (radius 12) and two MissingText bars
// (120, 200 or 360 wide).
func (v *AgentTasks) skeletonRow(gtx C, i int, first, last bool) D {
	w := gtx.Constraints.Max.X
	rw := contentW(gtx, w)
	x0 := (w - rw) / 2
	pad, is := gtx.Dp(16), gtx.Dp(36)
	h := 2*pad + max(is, gtx.Dp(40))
	defer op.Offset(image.Pt(x0, 0)).Push(gtx.Ops).Pop()
	r := image.Rect(0, 0, rw, h)
	rad := gtx.Dp(12)
	radii := clip.RRect{Rect: r}
	if first {
		radii.NW, radii.NE = rad, rad
	}
	if last {
		radii.SW, radii.SE = rad, rad
	}
	func() {
		defer radii.Push(gtx.Ops).Pop()
		fill(gtx, r, v.p.CardLine)
	}()
	in := r.Inset(1)
	in.Max.Y = r.Max.Y
	if last {
		in.Max.Y--
	}
	innerR := clip.RRect{Rect: in}
	if first {
		innerR.NW, innerR.NE = rad-1, rad-1
	}
	if last {
		innerR.SW, innerR.SE = rad-1, rad-1
	}
	func() {
		defer innerR.Push(gtx.Ops).Pop()
		fill(gtx, in, v.p.Bg)
	}()
	dirAt(gtx, image.Pt(pad, (h-is)/2), func(gtx C) D {
		return v.th.Placeholder(gtx, image.Pt(is, is), gtx.Dp(12))
	})
	widths := [...]unit.Dp{120, 200, 360}
	tx := pad + is + gtx.Dp(12)
	ty := (h - gtx.Dp(40)) / 2
	w1 := min(gtx.Dp(widths[i%3]), max(0, rw-tx-pad))
	w2 := min(gtx.Dp(widths[(i+1)%3]), max(0, rw-tx-pad))
	rrect(gtx, image.Rect(tx, ty+gtx.Dp(5), tx+w1, ty+gtx.Dp(17)), gtx.Dp(6), v.p.Skel)
	rrect(gtx, image.Rect(tx, ty+gtx.Dp(25), tx+w2, ty+gtx.Dp(37)), gtx.Dp(6), v.p.Skel)
	if last {
		return D{Size: image.Pt(w, h)}
	}
	return D{Size: image.Pt(w, h-1)}
}

// emptyRow is the explorer's empty-state row: .p-explorer_empty_state
// (200 tall, centred), EmptyState's title (18px/24 Black), description
// (15/22) and its button (Retry).
func (v *AgentTasks) emptyRow(gtx C, title, desc string, btn *widget.Clickable, label string) D {
	w := gtx.Constraints.Max.X
	h := gtx.Dp(200)
	dirAt(gtx, image.Point{}, func(gtx C) D {
		gtx.Constraints = layout.Exact(image.Pt(w, h))
		return layout.Center.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(380))
			gtx.Constraints.Min.X = 0
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D {
						return v.th.Layout(gtx, theme.TextStyle{Size: 18, LineHeight: 24, Weight: wBlack, Color: v.p.Text, Align: text.Middle}, title)
					})
				}),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D {
						return v.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: v.p.Text, Align: text.Middle}, desc)
					})
				}),
				layout.Rigid(func(gtx C) D {
					if btn == nil {
						return D{}
					}
					return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D {
						bw := v.th.Measure(gtx, 15, wBold, label) + 2*gtx.Dp(12) + 2*max(1, gtx.Dp(1))
						gtx.Constraints.Min.X, gtx.Constraints.Max.X = bw, bw
						return popups.Button(gtx, v.th, btn, popups.BtnOutline, popups.BtnMedium, label, "", 0)
					})
				}),
			)
		})
	})
	return D{Size: image.Pt(w, h)}
}

// emptyCard is ThirdPartyAgentTasksEmptyState without recent prompts
// (wfb_agent_tasks_recent_prompts off): container__eB2B7 (16px in) round a
// card__F5pMa (1px otl-ter, radius 8) whose header (48 24 48) has the
// :before glow (a radial base-inv-hgl-1 at 30%, blurred 30px, centred on
// the card's top edge), the 22px Black title and the content-sec
// description, centred.
func (v *AgentTasks) emptyCard(gtx C) D {
	w := gtx.Constraints.Max.X
	cw := max(0, w-2*gtx.Dp(16))
	m := v.mode()
	title := nsSlackbotAI.T(AgentTasksEmptyTitle)
	body := nsSlackbotAI.T(AgentTasksEmptyBody)
	tw := max(0, cw-2*gtx.Dp(24)-2)
	rec := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Min: image.Pt(tw, 0), Max: image.Pt(tw, 1<<20)}
	td := v.th.Layout(g, theme.TextStyle{Size: 22, LineHeight: 32, Weight: wBlack, Color: v.p.Text, Align: text.Middle}, title)
	tcall := rec.Stop()
	rec = op.Record(gtx.Ops)
	bd := v.th.Layout(g, theme.TextStyle{Size: 15, LineHeight: 22, Color: tasksSec[m], Align: text.Middle}, body)
	bcall := rec.Stop()
	ch := 1 + gtx.Dp(48) + td.Size.Y + gtx.Dp(8) + bd.Size.Y + gtx.Dp(48) + 1
	defer op.Offset(image.Pt(gtx.Dp(16), gtx.Dp(16))).Push(gtx.Ops).Pop()
	card := image.Rect(0, 0, cw, ch)
	rad := gtx.Dp(8)
	border(gtx, card, rad, max(1, gtx.Dp(1)), v.p.Border)
	func() { // the glow, clipped to the card (overflow hidden)
		defer clip.UniformRRect(card.Inset(1), rad-1).Push(gtx.Ops).Pop()
		v.drawGlow(gtx, card, tasksGlow[m])
	}()
	func() {
		defer op.Offset(image.Pt(1+gtx.Dp(24), 1+gtx.Dp(48))).Push(gtx.Ops).Pop()
		tcall.Add(gtx.Ops)
	}()
	func() {
		defer op.Offset(image.Pt(1+gtx.Dp(24), 1+gtx.Dp(48)+td.Size.Y+gtx.Dp(8))).Push(gtx.Ops).Pop()
		bcall.Add(gtx.Ops)
	}()
	return D{Size: image.Pt(w, ch+2*gtx.Dp(16))}
}

// drawGlow paints the header's :before: a box 310px tall (20px wider than
// the card each side) centred on the card's top edge, filled with a
// circle radial-gradient from col to transparent (farthest-corner), at
// opacity .3, blurred: a small image scaled up with linear filtering. The
// card's background is the view's, so the gradient is mixed over it in
// sRGB here (as a browser blends) and drawn opaque: Gio blends in linear
// light, which would wash the 30% tint out to grey.
func (v *AgentTasks) drawGlow(gtx C, card image.Rectangle, col color.NRGBA) {
	gw := card.Dx() + 2*gtx.Dp(20)
	gh := gtx.Dp(310)
	if gw <= 0 || gh <= 0 {
		return
	}
	key := glowKey{w: gw, h: gh, col: col, bg: v.p.Bg}
	if !v.glowOK || v.glowAt != key {
		const iw, ih = 64, 32
		img := image.NewRGBA(image.Rect(0, 0, iw, ih))
		rmax := float64(gw*gw)/4 + float64(gh*gh)/4
		for y := 0; y < ih; y++ {
			for x := 0; x < iw; x++ {
				dx := (float64(x) + .5 - iw/2) / (iw / 2)
				dy := (float64(y) + .5 - ih/2) / (ih / 2)
				// a circle, sized to the farthest corner of the box (in
				// its own pixels)
				d := dx*dx*float64(gw*gw)/4 + dy*dy*float64(gh*gh)/4
				a := (1 - math.Sqrt(d/rmax)) * .3
				if a < 0 {
					a = 0
				}
				c := theme.Mix(v.p.Bg, col, float32(a))
				img.SetRGBA(x, y, color.RGBA{R: c.R, G: c.G, B: c.B, A: 0xff})
			}
		}
		v.glow = paint.NewImageOp(img)
		v.glow.Filter = paint.FilterLinear
		v.glowOK, v.glowAt = true, key
	}
	defer op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(float32(gw)/64, float32(gh)/32)).
		Offset(f32.Pt(float32(card.Min.X-gtx.Dp(20)), float32(card.Min.Y-gh/2)))).Push(gtx.Ops).Pop()
	v.glow.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// glowKey is what the glow image was made for.
type glowKey struct {
	w, h    int
	col, bg color.NRGBA
}

// TaskIDs lists the drawn rows' workflow ids in order (tests).
func (v *AgentTasks) TaskIDs() []string {
	ts := v.Tasks()
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.WorkflowID)
	}
	return out
}

// SetQuery types q into the search (tests).
func (v *AgentTasks) SetQuery(q string) {
	v.srch.ed.SetText(q)
	v.typed = q
	v.h.Invalidate()
}
