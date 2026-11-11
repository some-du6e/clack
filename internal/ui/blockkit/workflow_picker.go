package blockkit

import (
	"context"
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The workflow picker is Slack's WorkflowPicker modal (boot-shared MUtn,
// build a84662d2d135): the composer's + menu opens it to attach a
// workflow to the draft, the channel menu's "Add a workflow" to bookmark
// one in the channel. A search box over the workflows the viewer may use
// (search.autocomplete.triggers), a 300px list of them (WorkflowListItem:
// the app icon, the title, the description; the picked one highlighted),
// and a footer with "Create a Workflow", Cancel and the source's button.

// Picker sources (Slack's source prop).
const (
	PickerShortcutsMenu    = "shortcuts_menu"         // "Attach a workflow" / Insert
	PickerChannelWorkflows = "channel_workflows"      // "Add workflow" / Add
	PickerChannelPrimary   = "channel-primary-action" // "Feature workflow" / Feature
)

// WorkflowPickerOpts configure OpenWorkflowPicker.
type WorkflowPickerOpts struct {
	Source string
	// ChannelID is the conversation the picker is about (the search's
	// current_channel_id).
	ChannelID string
	// IncludeAutomated lists every trigger kind, not only link triggers
	// (includeAutomatedTriggers).
	IncludeAutomated bool
	// Excluded are trigger ids left out (excludedIds).
	Excluded []string
	// OnInsert runs with the picked trigger (onInsertWorkflow), then the
	// picker closes.
	OnInsert func(t core.WorkflowTrigger)
}

// OpenWorkflowPicker opens the picker.
func OpenWorkflowPicker(h Host, o WorkflowPickerOpts) {
	p := &workflowPicker{h: h, o: o, loading: true}
	p.filter.SingleLine = true
	title, action := "Add workflow", "Add"
	switch o.Source {
	case PickerShortcutsMenu:
		title, action = "Attach a workflow", "Insert"
	case PickerChannelPrimary:
		title, action = "Feature workflow", "Feature"
	}
	p.primary = &popups.ModalButton{Label: action, Kind: popups.BtnPrimary,
		Enabled: func() bool { return p.selected() != nil }, Do: p.insert}
	// WorkflowPickerV2FooterActions: "Create a Workflow" ("Create From
	// Scratch" with nothing to pick) launches Workflow Builder; Cancel
	create := &popups.ModalButton{Kind: popups.BtnOutline, Left: true, Do: func() bool {
		launchWorkflowBuilder(h)
		return false
	}}
	p.create = create
	create.Label = "Create a Workflow"
	p.modal = popups.NewModal(h, popups.ModalOpts{Title: title, Width: 650, Flush: true, Border: true, Body: p.layout,
		Class:   "p-workflow_picker__modal p-workflow_picker__modal--new_style",
		Buttons: []*popups.ModalButton{create, {Label: "Cancel", Kind: popups.BtnOutline}, p.primary}})
	h.ShowOverlay(p.modal)
	if pickerShown != nil {
		pickerShown(p)
	}
	p.search("")
}

// pickerShown, when set (tests), sees each picker as it opens.
var pickerShown func(p *workflowPicker)

// launchWorkflowBuilder is Slack's launchWorkflowBuilder with route
// create_empty: Workflow Builder in its own window
// (/workflow-builder/<team>/workflow/create-empty, 1024×700). Clack has no
// builder of its own, so that page opens in the browser.
func launchWorkflowBuilder(h Host) {
	team := ""
	if c := h.Core(); c != nil {
		team = c.Interactive().TeamID()
	}
	if team == "" {
		return
	}
	openURL("https://app.slack.com/workflow-builder/" + team + "/workflow/create-empty")
}

type workflowPicker struct {
	h       Host
	o       WorkflowPickerOpts
	modal   *popups.Modal
	primary *popups.ModalButton
	create  *popups.ModalButton

	filter  widget.Editor
	query   string
	gen     int // the newest search; older answers are dropped
	due     time.Time
	loading bool
	results []core.WorkflowTrigger
	known   bool // a search ever found something (Slack's empty state is "no workflows at all")
	picked  string
	rows    []tap
	list    layout.List
	focused bool
}

func (p *workflowPicker) selected() *core.WorkflowTrigger {
	for i := range p.results {
		if p.results[i].ID == p.picked {
			return &p.results[i]
		}
	}
	return nil
}

func (p *workflowPicker) insert() bool {
	t := p.selected()
	if t == nil {
		return false
	}
	if p.o.OnInsert != nil {
		p.o.OnInsert(*t)
	}
	return true
}

// search asks for q (Slack's debounced fetchAndUpsertTriggersWithQuery:
// 500 ms, leading and trailing).
func (p *workflowPicker) search(q string) {
	p.gen++
	gen := p.gen
	if len(p.results) == 0 {
		p.loading = true
	}
	ix := p.h.Core().Interactive()
	o := p.o
	run(p.h, 15*time.Second, func(ctx context.Context) ([]core.WorkflowTrigger, error) {
		return ix.SearchTriggers(ctx, q, o.ChannelID, o.IncludeAutomated)
	}, func(ts []core.WorkflowTrigger, err error) {
		if gen != p.gen {
			return
		}
		p.loading = false
		if err != nil {
			p.results = nil
			return
		}
		p.results = p.visible(ts)
		if len(p.results) > 0 {
			p.known = true
		}
		if p.selected() == nil {
			p.picked = ""
		}
		p.h.Invalidate()
	})
}

// visible drops excluded triggers and, without automated ones, every
// trigger that isn't a link trigger (the searcher's filter).
func (p *workflowPicker) visible(ts []core.WorkflowTrigger) []core.WorkflowTrigger {
	out := ts[:0:0]
	for _, t := range ts {
		skip := !p.o.IncludeAutomated && !t.IsShortcut()
		for _, x := range p.o.Excluded {
			skip = skip || x == t.ID
		}
		if !skip {
			out = append(out, t)
		}
	}
	return out
}

// emptyState: no workflows to choose from at all (Slack hides the search
// box then and shows its illustration).
func (p *workflowPicker) emptyState() bool {
	return !p.loading && !p.known && len(p.results) == 0 && p.query == ""
}

func (p *workflowPicker) layout(gtx C) D {
	th := p.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	// typing: debounce the remote search
	if q := strings.TrimSpace(p.filter.Text()); q != p.query {
		p.query = q
		p.due = gtx.Now.Add(500 * time.Millisecond)
	}
	if !p.due.IsZero() {
		if !gtx.Now.Before(p.due) {
			p.due = time.Time{}
			p.search(p.query)
		} else {
			gtx.Execute(op.InvalidateCmd{At: p.due})
		}
	}
	if p.emptyState() {
		p.create.Label = "Create From Scratch"
	} else {
		p.create.Label = "Create a Workflow"
	}
	// the header's 1px bottom border (.p-workflow_picker__header, new style)
	theme.Fill(gtx, image.Rect(0, 0, w, dp(gtx, 1)), cl.FooterBorder)
	y := dp(gtx, 1) + dp(gtx, 12) // .p-workflow_picker__section margin-top
	if p.emptyState() {
		// WorkflowPickerV2EmptyState without templates: Slack's
		// illustration (workflow-picker-empty-state.svg) 20px above and
		// below; Clack draws no SVG, so its place stays empty
		is := image.Pt(dp(gtx, 260), dp(gtx, 160))
		return D{Size: image.Pt(w, y+dp(gtx, 20)+is.Y+dp(gtx, 20))}
	}
	// WorkflowPickerFilter: 16px margin, the search icon, autofocus
	if !p.focused {
		gtx.Execute(key.FocusCmd{Tag: &p.filter})
		p.focused = true
	}
	fh := dp(gtx, 36)
	at(gtx, image.Pt(dp(gtx, 16), y+dp(gtx, 16)), func(gtx C) D {
		gtx.Constraints = layout.Exact(image.Pt(w-2*dp(gtx, 16), fh))
		return popups.Field(gtx, th, &p.filter, "Search by workflow name or keyword", "search", 36, 15)
	})
	y += dp(gtx, 16) + fh + dp(gtx, 16)
	lh := dp(gtx, 300)
	if p.loading {
		sd := dp(gtx, 24)
		at(gtx, image.Pt((w-sd)/2, y+(lh-sd)/2), func(gtx C) D { spinner(gtx, sd, cl.Link); return D{} })
		return D{Size: image.Pt(w, y+lh)}
	}
	if len(p.rows) < len(p.results) {
		p.rows = append(p.rows, make([]tap, len(p.results)-len(p.rows))...)
	}
	for i := range p.results {
		if p.rows[i].clicked(gtx, p.h) {
			p.picked = p.results[i].ID
		}
	}
	p.list.Axis = layout.Vertical
	at(gtx, image.Pt(0, y), func(gtx C) D {
		gtx.Constraints = layout.Exact(image.Pt(w, lh))
		defer clip.Rect{Max: image.Pt(w, lh)}.Push(gtx.Ops).Pop()
		return p.list.Layout(gtx, len(p.results), func(gtx C, i int) D { return p.row(gtx, i, w) })
	})
	return D{Size: image.Pt(w, y+lh)}
}

// row is WorkflowListItem: 12px 25px padding, the 36px icon 12px before
// the title (15px) over the description (13px, 70%); white on #1264a3
// when picked, the min-solid background on hover.
func (p *workflowPicker) row(gtx C, i, w int) D {
	th := p.h.Theme()
	cl := colorsOf(th)
	t := p.results[i]
	sel := t.ID == p.picked
	is := dp(gtx, 36)
	tx := dp(gtx, 25) + is + dp(gtx, 12)
	tw := w - tx - dp(gtx, 25)
	name, desc := t.Title(), t.Desc()
	nameC, descC := cl.Text, theme.WithAlpha(cl.Text, 0xb3)
	if sel {
		nameC, descC = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	}
	// measure the text block first: the row is as tall as its content
	nameH := dp(gtx, 22)
	descH := 0
	if desc != "" {
		descH = dp(gtx, 18)
	}
	h := max(is, nameH+descH) + 2*dp(gtx, 12)
	switch {
	case sel:
		theme.Fill(gtx, image.Rect(0, 0, w, h), color.NRGBA{R: 0x12, G: 0x64, B: 0xa3, A: 0xff})
	case p.rows[i].g.Hovered():
		theme.Fill(gtx, image.Rect(0, 0, w, h), cl.Hover)
	}
	at(gtx, image.Pt(dp(gtx, 25), (h-is)/2), func(gtx C) D {
		if img, ok := p.h.Images().Get(t.Icon, is); ok && t.Icon != "" {
			return theme.Image(gtx, img, image.Pt(is, is), dp(gtx, 4))
		}
		return th.Placeholder(gtx, image.Pt(is, is), dp(gtx, 4))
	})
	ty := (h - nameH - descH) / 2
	at(gtx, image.Pt(tx, ty), func(gtx C) D {
		gtx.Constraints = layout.Constraints{Max: image.Pt(tw, nameH)}
		return label(gtx, th, 15, theme.Regular, nameC, name)
	})
	if desc != "" {
		at(gtx, image.Pt(tx, ty+nameH), func(gtx C) D {
			gtx.Constraints = layout.Constraints{Max: image.Pt(tw, descH)}
			return label(gtx, th, 13, theme.Regular, descC, desc)
		})
	}
	p.rows[i].add(gtx, image.Pt(w, h))
	return D{Size: image.Pt(w, h)}
}
