package views

// Channel details > Automations: Slack's AutomationsSettings and
// WorkflowsSettings (chunk client-channel-settings-automations-view,
// module 2MVc, build a84662d2d135; docs/WORKFLOWS.md). The tab lists the
// channel's workflows: the workflows whose bots are members
// (core.ChannelWorkflowApps, Slack's workflowApps) first, then its legacy
// workflows (core.ChannelActions, Slack's channelActions) by name, at
// most 100, in a "Published Automations" field (ListField, p-field--list)
// whose rows are Slack's WorkflowItem and AppActionItem
// (get-integrations-sections 2Lul, boot-shared ARyf). With none, Slack's
// empty state: "Let workflows take tasks off your plate" (with the
// channel_details_agents_apps_tab experiment; "Supercharge your channel"
// without), its description and Learn More.

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// TripWorkflow, set by the ui root (workflows_wire.go), runs a link
// trigger the way Slack's workflowsTriggersTrip does (blockkit).
var TripWorkflow func(h Host, link string, tc core.TripContext, uiPage string)

// automationsHelpURL is Learn More's help article (articleId 0xf1536060d13).
const automationsHelpURL = "https://slack.com/help/articles/16583775096083"

type cdAutomation struct {
	app    *core.WorkflowApp   // a workflow whose bot is in the channel
	action *core.ChannelAction // a legacy workflow
}

func (a cdAutomation) key() string {
	if a.app != nil {
		return "wf:" + a.app.AppID
	}
	return "act:" + a.action.ID
}

type cdAutos struct {
	data   async[[]cdAutomation]
	learn  widget.Clickable
	rows   map[string]*widget.Clickable
	starts map[string]*widget.Clickable
	menus  map[string]*widget.Clickable
}

var cdAutoStates sync.Map // *ChannelDetails -> *cdAutos

func (d *ChannelDetails) autos() *cdAutos {
	v, _ := cdAutoStates.LoadOrStore(d, &cdAutos{rows: map[string]*widget.Clickable{}, starts: map[string]*widget.Clickable{},
		menus: map[string]*widget.Clickable{}})
	return v.(*cdAutos)
}

// loadAutomations is AutomationsSettings' data: both stores, merged the
// way WorkflowsSettings merges them (workflow apps, then the legacy
// actions sorted by name, at most 100).
func loadAutomations(ctx context.Context, c *core.App, ch string) ([]cdAutomation, error) {
	ix := c.Interactive()
	var (
		wg              sync.WaitGroup
		apps            []core.WorkflowApp
		acts            []core.ChannelAction
		appsErr, actErr error
	)
	wg.Add(2)
	go func() { defer wg.Done(); apps, appsErr = ix.ChannelWorkflowApps(ctx, ch) }()
	go func() { defer wg.Done(); acts, actErr = ix.ChannelActions(ctx, ch) }()
	wg.Wait()
	if appsErr != nil && actErr != nil {
		return nil, appsErr
	}
	sort.SliceStable(acts, func(i, j int) bool { return strings.ToLower(acts[i].Name) < strings.ToLower(acts[j].Name) })
	var out []cdAutomation
	for i := range apps {
		out = append(out, cdAutomation{app: &apps[i]})
	}
	for i := range acts {
		out = append(out, cdAutomation{action: &acts[i]})
	}
	if len(out) > 100 {
		out = out[:100]
	}
	return out, nil
}

// automationsItems is the Automations tab's body.
func (d *ChannelDetails) automationsItems() []W {
	st := d.autos()
	if !st.data.started() {
		c, id := d.h.Core(), d.id
		st.data.load(d.h, false, func(ctx context.Context) ([]cdAutomation, error) { return loadAutomations(ctx, c, id) })
	}
	items, loaded, _ := st.data.get()
	if !loaded {
		return []W{cdGap(16), d.cdCard(func(gtx C) D {
			return layout.Inset{Top: 12, Bottom: 12}.Layout(gtx, func(gtx C) D { return d.skeleton(gtx, 3, 36) })
		})}
	}
	k := d.h.Core().Experiment("channel_details_agents_apps_tab") == "on"
	title := "Supercharge your channel"
	desc := "There’s a few automations that will make your life easier. Set them up in a flash."
	if k {
		title = "Let workflows take tasks off your plate"
		desc = "Workflow Builder lets you automate your team’s daily routines and everyday processes, all without writing a single line of code."
	}
	// eu(): Slack's "Go to Your Workflows" / "Add Automation" (members
	// only) open its Workflows › Managed by you view, which Clack doesn't
	// have; Learn More opens the help article
	actions := func(gtx C) D {
		if st.learn.Clicked(gtx) {
			go func() { _ = openurl.Open(automationsHelpURL) }()
		}
		return d.button(gtx, &st.learn, btnOutline, true, "Learn More")
	}
	var visible []cdAutomation
	for _, it := range items {
		if it.app != nil && (it.app.Title() == "" || it.app.Desc() == "") {
			continue // WorkflowItem draws nothing without both
		}
		visible = append(visible, it)
	}
	if len(visible) == 0 {
		// emptyState__RhLrW: column, centred, 16px apart, padding 16 24;
		// c-empty_state: at most 380 wide, title 18/24 900 8px above the
		// description (15/22), 16px above the buttons. Slack's
		// illustration (/img/ia-details/workflows-empty.png) is served from
		// app.slack.com only, which Clack doesn't fetch.
		return []W{cdGap(16), func(gtx C) D {
			return layout.Inset{Top: 16, Bottom: 16, Left: 24, Right: 24}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(380))
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						return col(gtx,
							layout.Rigid(func(gtx C) D { return d.centered(gtx, 18, wBlack, d.p.Text, title) }),
							vgap(8),
							layout.Rigid(func(gtx C) D { return d.paraCentered(gtx, desc) }),
						)
					}),
					vgap(16),
					layout.Rigid(actions),
				)
			})
		}, cdGap(24)}
	}
	head := func(gtx C) D {
		return layout.Inset{Top: 16, Left: 20, Right: 20, Bottom: 4}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return d.para(gtx, 15, 22, wBold, d.p.Text, 1, "Published Automations")
		})
	}
	rows := []W{head}
	for _, it := range visible {
		it := it
		rows = append(rows, func(gtx C) D { return d.automationRow(gtx, st, it) })
	}
	rows = append(rows, cdGap(12))
	footer := func(gtx C) D { // p-field__sectioned_footer: a top border, the buttons
		return layout.Inset{Top: 12, Bottom: 12, Left: 20, Right: 20}.Layout(gtx, actions)
	}
	return []W{cdGap(16), d.cdCard(func(gtx C) D {
		ch := make([]layout.FlexChild, len(rows))
		for i, r := range rows {
			r := r
			ch[i] = layout.Rigid(func(gtx C) D { gtx.Constraints.Min.X = gtx.Constraints.Max.X; return r(gtx) })
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, ch...)
	}, footer), cdGap(24)}
}

// paraCentered is centred wrapping 15/22 text (the empty state's
// description).
func (d *ChannelDetails) paraCentered(gtx C, s string) D {
	gtx.Constraints.Min.Y = 0
	return d.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: wReg, Color: d.p.Text, Align: text.Middle}, s)
}

// automationRow is one WorkflowItem / AppActionItem: 18px above and below
// (p-integrations__list_entity_row), the 36px icon 12px before a bold
// title (a legacy workflow's has the WORKFLOW badge) over its
// description (13px, tertiary); a click runs a link trigger (location
// channel_details) or a legacy workflow, and on hover the ▶ "Start
// workflow" button does the same.
func (d *ChannelDetails) automationRow(gtx C, st *cdAutos, it cdAutomation) D {
	k := it.key()
	clk, start := dirClick(st.rows, k), dirClick(st.starts, k)
	runnable := it.action != nil || it.app != nil && it.app.Trigger.IsShortcut() && it.app.Trigger.ShortcutURL != ""
	if (clk.Clicked(gtx) || start.Clicked(gtx)) && runnable {
		d.runAutomation(it)
	}
	icon, title, desc, badge := "", "", "", false
	if it.app != nil {
		icon, title, desc = dirFirst(it.app.Icon, it.app.Trigger.Icon), it.app.Title(), it.app.Desc()
	} else {
		icon, title, desc, badge = it.action.Icon, it.action.Name, it.action.Description, true
	}
	return d.hoverable(gtx, clk, false, 0, func(gtx C) D {
		return layout.Inset{Top: 18, Bottom: 18, Left: 12, Right: 12}.Layout(gtx, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D { return d.avatar(gtx, icon, 36) }),
				hgap(12),
				layout.Flexed(1, func(gtx C) D {
					return dirFill(gtx, func(gtx C) D {
						return col(gtx,
							layout.Rigid(func(gtx C) D {
								return row(gtx, layout.Middle,
									layout.Flexed(1, func(gtx C) D { return d.para(gtx, 15, 22, wBold, d.p.Text, 1, title) }),
									layout.Rigid(func(gtx C) D {
										if !badge {
											return D{}
										}
										return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D { return cdAppBadge(&d.base, gtx, "WORKFLOW") })
									}),
								)
							}),
							layout.Rigid(func(gtx C) D { return d.para(gtx, 13, 18, wReg, d.p.Faint, 1, desc) }),
						)
					})
				}),
				layout.Rigid(func(gtx C) D {
					if !runnable || !clk.Hovered() && !start.Hovered() {
						return D{}
					}
					return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D {
						return d.hoverable(gtx, start, false, 6, func(gtx C) D {
							return layout.UniformInset(unit.Dp(6)).Layout(gtx, func(gtx C) D { return d.icon(gtx, "play", 16, d.p.Muted) })
						})
					})
				}),
			)
		})
	})
}

// runAutomation is a row's click: a workflow app's link trigger trips
// with Slack's channel_details context (WorkflowAppItem); a legacy
// workflow runs through apps.actions.run (runChannelAction), with
// Slack's "You used the workflow …" toast or its retry toast.
func (d *ChannelDetails) runAutomation(it cdAutomation) {
	if it.app != nil {
		if TripWorkflow != nil {
			TripWorkflow(d.h, it.app.Trigger.ShortcutURL, core.ChannelDetailsTripContext(d.id), "channel_details")
		}
		return
	}
	c, ch, a, h := d.h.Core(), d.id, *it.action, d.h
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		err := c.Interactive().RunChannelAction(ctx, a, ch)
		post := func(f func()) {
			if p, ok := h.(interface{ Post(func()) }); ok {
				p.Post(f)
				return
			}
			f()
		}
		post(func() {
			if err != nil {
				h.Toast("Sorry, that didn’t work. Try again?")
				return
			}
			h.Toast("You used the workflow " + a.Name)
		})
	}()
}
