package ui

import (
	"context"
	"image"
	"image/color"
	"io"
	"strings"
	"sync"
	"time"

	"gioui.org/io/clipboard"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/blockkit"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The workflow details view: Slack's WorkflowDetails (client-workflow-
// details HRfd, build 4d0c78085232), the viewType WorkflowDetails route
// "View workflow details" opens (openWorkflowDetailsModalCircular, 6e8N
// toWorkflowDetails {triggerId}) in the flexpane:
//
//   - the trigger comes from the triggers store (SzL7: workflows.triggers.
//     info with include_workflow_details), its workflow from the workflows
//     store (Okt3 over JPWF: functions.workflows.get); the view draws
//     createShortcutFromTriggerAndWorkflow's shortcut (jsMl). An error
//     says "Workflow not found" / "This workflow no longer exists."
//     (trigger_not_found), "Permission denied" (access_denied) or
//     "Something’s gone wrong"; until the trigger has come, nothing.
//   - Flexpane "Workflow" with its close button.
//   - WorkflowDetailsInfo (padding 24, rows 8px apart, an otl-ter line
//     under it): the workflow's icon (36, 12px before the title), the
//     title (18px bold, one line), the description, the trigger type line
//     for automated triggers (WorkflowTriggerTypeLabel: a 20px icon, 13px
//     text), then the buttons: ▶ Start Workflow across the panel for a
//     link trigger (WorkflowTripButton, uiPage workflow_details_view), and
//     a row with Share (or Edit for a workflow app's collaborators), the
//     VIP button for the workflow's bot user ("Add to VIP" / "Remove from
//     VIP") and the ⋮ menu (MoreActionsMenu isDetailsView).
//   - WorkflowDetailsTab: Information (13px, content-sec: "Managed by"
//     the collaborators, "Created by" the owning team when it isn't the
//     viewer's) and Steps (the trigger as a step, an arrow down, then
//     each level's steps as one entity list, a switch's cases after it as
//     tinted branch boxes, "Branch A"… / "Fallback", and loops in an
//     outlined "Repeat" box).
//
// Left out: Share and Edit (Slack's share-workflow dialog and Workflow
// Builder, which Clack doesn't have), the ⋮ menu's builder rows (edit,
// unpublish, copy, export, add to channel: a workflow's collaborators and
// admins), Connections (third-party steps' accounts), the VIP badge after
// the title, and the "and N others" managers dialog.

// wfDetailsNS and friends are the view's I18n namespaces: its own
// (workflow_details), Workflow Builder's for branches and loops, the
// shortcuts' for trigger types and the ⋮ menu rows.
var (
	wfDetailsNS = i18n.NS("workflow_details")
	wfBuilderNS = i18n.NS("workflow_builder")
	wfShortNS   = i18n.NS("hermes_shortcuts")
)

// OpenWorkflowDetails opens the workflow details view for a trigger, from
// a message in ch (popups.OpenWorkflowDetails).
func (a *App) OpenWorkflowDetails(trigger, ch string) {
	if trigger == "" {
		return
	}
	if p, ok := a.secondary.view.(*wfDetailsPane); ok && a.secondary.kind == secWorkflow && a.secondary.open() && p.trigger == trigger {
		return
	}
	p := &wfDetailsPane{a: a, trigger: trigger, ch: ch}
	p.list.Axis = layout.Vertical
	a.secondary = secondaryState{kind: secWorkflow, channel: ch, width: a.secondary.width}
	a.secondary.view = p
	p.load()
	a.Invalidate()
}

type wfDetailsPane struct {
	a           *App
	trigger, ch string

	mu      sync.Mutex
	loaded  bool
	t       core.WorkflowTrigger
	errCode string
	wf      *core.WorkflowInfo

	close, start, vip, more widget.Clickable
	vipTip, moreTip         popups.TipState
	list                    layout.List
	moreRect                image.Rectangle // the ⋮ in window coordinates
	org                     originTracker   // the info section's window origin
}

// load asks for the trigger (with its workflow details), then its
// workflow.
func (p *wfDetailsPane) load() {
	c := p.a.Core()
	if c == nil {
		return
	}
	ix := c.Interactive()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		t, err := ix.TriggerInfo(ctx, p.trigger)
		var wf *core.WorkflowInfo
		if err == nil && t.WorkflowID != "" {
			if w, err := ix.Workflow(ctx, t.WorkflowID); err == nil {
				wf = &w
			}
		}
		p.mu.Lock()
		p.loaded, p.t, p.wf = true, t, wf
		if err != nil {
			p.errCode = core.ErrCode(err)
			if p.errCode == "" {
				p.errCode = "error"
			}
		}
		p.mu.Unlock()
		p.a.Invalidate()
	}()
}

// wfShortcut is the shortcut createShortcutFromTriggerAndWorkflow makes
// of the trigger and its workflow.
type wfShortcut struct {
	title, desc, kind     string
	workflowID, shareURL  string
	botUser, owningTeam   string
	collaborators         []string
	steps                 []core.WorkflowStep
	runnable, workflowApp bool
	link                  string
	icon                  string
}

func (p *wfDetailsPane) shortcut() (wfShortcut, string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.loaded {
		return wfShortcut{}, "", false
	}
	if p.errCode != "" {
		return wfShortcut{}, p.errCode, true
	}
	t, w := p.t, p.wf
	s := wfShortcut{title: t.Title(), desc: t.Desc(), kind: t.Type, workflowID: t.WorkflowID, shareURL: t.ShareURL,
		botUser: t.BotUserID, owningTeam: t.OwningTeam, collaborators: t.Collaborators, steps: t.Steps,
		workflowApp: t.IsWorkflowApp, link: t.ShortcutURL, icon: t.Icon}
	if t.Type == "" && t.ShortcutURL != "" {
		s.kind = "shortcut"
	}
	s.runnable = t.IsShortcut() && t.ShortcutURL != "" && t.Published
	if w != nil {
		s.workflowID = firstNonEmptyStr(s.workflowID, w.ID)
		s.title = firstNonEmptyStr(s.title, w.Title)
		s.desc = firstNonEmptyStr(s.desc, w.Description)
		s.botUser = firstNonEmptyStr(s.botUser, w.BotUserID)
		if len(w.Collaborators) > 0 {
			s.collaborators = w.Collaborators
		}
		s.steps = w.Steps // workflowDetails: the workflow when it has come
		if s.icon == "" {
			s.icon = w.Icon
		}
	}
	return s, "", true
}

func firstNonEmptyStr(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// Layout draws the flexpane.
func (p *wfDetailsPane) Layout(gtx layout.Context) layout.Dimensions {
	a := p.a
	th := a.th
	c := &th.C
	sz := gtx.Constraints.Max
	// the flexpane header: "Workflow", 18px/24px Black at 16px; the close
	// button 28x28 (radius 8) 12px from the right
	hh := gtx.Dp(theme.HeaderH)
	at(gtx, image.Pt(gtx.Dp(16), 0), image.Pt(sz.X-gtx.Dp(60), hh), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
			return th.Label(gtx, 18, theme.Black, c.Fg, wfDetailsNS.T("Workflow"))
		})
	})
	bs := gtx.Dp(28)
	at(gtx, image.Pt(sz.X-gtx.Dp(12)-bs, (hh-bs)/2), image.Pt(bs, bs), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, &p.close, "close", 28, 18, c.FgMuted, c.BgHover, 8)
	})
	theme.Fill(gtx, image.Rect(0, hh-gtx.Dp(1), sz.X, hh), c.Border)
	if clicked(&p.close) {
		a.CloseSecondary()
		return layout.Dimensions{Size: sz}
	}
	s, errCode, ok := p.shortcut()
	if !ok {
		return layout.Dimensions{Size: sz}
	}
	atExact(gtx, image.Pt(0, hh), image.Pt(sz.X, sz.Y-hh), func(gtx layout.Context) layout.Dimensions {
		if errCode != "" {
			return p.errorBody(gtx, errCode)
		}
		if s.workflowID == "" {
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}
		p.clicks(gtx, s)
		parts := []layout.Widget{
			func(gtx layout.Context) layout.Dimensions { return p.info(gtx, s) },
			func(gtx layout.Context) layout.Dimensions { return p.information(gtx, s) },
		}
		if len(s.steps) > 0 {
			parts = append(parts, func(gtx layout.Context) layout.Dimensions { return p.stepsSection(gtx, s) })
		}
		return p.list.Layout(gtx, len(parts), func(gtx layout.Context, i int) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return parts[i](gtx)
		})
	})
	return layout.Dimensions{Size: sz}
}

// clicks runs the info section's buttons.
func (p *wfDetailsPane) clicks(gtx layout.Context, s wfShortcut) {
	a := p.a
	if p.start.Clicked(gtx) && s.runnable {
		blockkit.TripWorkflow(a, s.link, core.FlexpaneTripContext(""), "workflow_details_view")
	}
	if p.vip.Clicked(gtx) && s.botUser != "" {
		p.toggleVIP(s.botUser)
	}
	if p.more.Clicked(gtx) {
		p.openMore(s)
	}
}

// isVIP is U8c3 mF: the bot user is one of the viewer's VIPs.
func (p *wfDetailsPane) isVIP(id string) bool {
	c := p.a.Core()
	if c == nil {
		return false
	}
	for _, v := range c.VIPs() {
		if v == id {
			return true
		}
	}
	return false
}

// toggleVIP is 77my: add or remove the workflow's bot user as a VIP.
func (p *wfDetailsPane) toggleVIP(id string) {
	c := p.a.Core()
	if c == nil {
		return
	}
	add := !p.isVIP(id)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if add {
			_ = c.AddVIP(ctx, id)
		} else {
			_ = c.RemoveVIP(ctx, id)
		}
		p.a.Invalidate()
	}()
}

// openMore is MoreActionsMenu isDetailsView for a viewer who can't edit
// the workflow: Copy workflow link (a link trigger's share_url; copied
// with Slack's "Workflow link copied" toast, Zvd/), then Copy workflow ID
// (the clipboard only).
func (p *wfDetailsPane) openMore(s wfShortcut) {
	var items []popups.MenuItem
	if s.shareURL != "" {
		u := s.shareURL
		items = append(items, popups.MenuItem{Key: "copy_workflow", Label: wfShortNS.T("Copy workflow link"), DoCtx: func(gtx layout.Context) {
			wfCopy(gtx, u)
			p.a.Toast(wfShortNS.T("Workflow link copied"))
		}})
	}
	if s.workflowID != "" {
		if len(items) > 0 {
			items = append(items, popups.Sep())
		}
		id := s.workflowID
		items = append(items, popups.MenuItem{Key: "copy_workflow_id", Label: wfShortNS.T("Copy workflow ID"), DoCtx: func(gtx layout.Context) {
			wfCopy(gtx, id)
		}})
	}
	if len(items) == 0 {
		return
	}
	p.a.ShowOverlay(popups.NewMenuOpts(p.a, items, popups.MenuOpts{Anchor: p.moreRect, AlignRight: true}))
}

// errorBody is renderBodyContent's error: the title (18px bold) over the
// message (15px), padding 24, 8px apart.
func (p *wfDetailsPane) errorBody(gtx layout.Context, code string) layout.Dimensions {
	th := p.a.th
	title, msg := wfDetailsNS.T("Something’s gone wrong"), wfDetailsNS.T("Oops, there was an error loading this workflow.")
	switch code {
	case "trigger_not_found":
		title, msg = wfDetailsNS.T("Workflow not found"), wfDetailsNS.T("This workflow no longer exists.")
	case "access_denied":
		title, msg = wfDetailsNS.T("Permission denied"), wfDetailsNS.T("You don’t have permissions to view workflow details.")
	}
	pad := gtx.Dp(24)
	w := gtx.Constraints.Max.X - 2*pad
	d1 := at(gtx, image.Pt(pad, pad), image.Pt(w, gtx.Dp(200)), func(gtx layout.Context) layout.Dimensions {
		return th.Layout(gtx, theme.TextStyle{Size: 18, LineHeight: 24, Weight: theme.Bold, Color: th.C.Fg}, title)
	})
	at(gtx, image.Pt(pad, pad+d1.Size.Y+gtx.Dp(8)), image.Pt(w, gtx.Dp(200)), func(gtx layout.Context) layout.Dimensions {
		return th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: theme.Regular, Color: th.C.Fg}, msg)
	})
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// info is WorkflowDetailsInfo.
func (p *wfDetailsPane) info(gtx layout.Context, s wfShortcut) layout.Dimensions {
	a := p.a
	th := a.th
	c := &th.C
	w := gtx.Constraints.Max.X
	p.org.track(gtx, a) // where the ⋮ is in the window, for its menu
	pad, gap := gtx.Dp(24), gtx.Dp(8)
	iw := w - 2*pad
	y := pad
	// the header: the icon (36) 12px before the title
	is := gtx.Dp(36)
	at(gtx, image.Pt(pad, y), image.Pt(is, is), func(gtx layout.Context) layout.Dimensions {
		if img, ok := a.Images().Get(s.icon, is); ok && s.icon != "" {
			return theme.Image(gtx, img, image.Pt(is, is), gtx.Dp(8))
		}
		return th.Placeholder(gtx, image.Pt(is, is), gtx.Dp(8))
	})
	tx := pad + is + gtx.Dp(12)
	at(gtx, image.Pt(tx, y), image.Pt(max(1, w-tx-pad), is), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, is, func(gtx layout.Context) layout.Dimensions {
			return th.Layout(gtx, theme.TextStyle{Size: 18, LineHeight: 26, Weight: theme.Bold, Color: c.Fg, MaxLines: 1}, s.title)
		})
	})
	y += is + gap
	if s.desc != "" {
		d := at(gtx, image.Pt(pad, y), image.Pt(iw, gtx.Dp(400)), func(gtx layout.Context) layout.Dimensions {
			return th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: theme.Regular, Color: c.Fg}, s.desc)
		})
		y += d.Size.Y + gap
	}
	if icon, label := wfTriggerType(s.kind); icon != "" {
		// WorkflowTriggerTypeLabel (.triggerDetails: 13px, 8px gap)
		at(gtx, image.Pt(pad, y), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
			return icons.Get(icon)(gtx, unit.Dp(20), c.Fg)
		})
		at(gtx, image.Pt(pad+gtx.Dp(28), y), image.Pt(max(1, iw-gtx.Dp(28)), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, gtx.Dp(20), func(gtx layout.Context) layout.Dimensions {
				return th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Weight: theme.Regular, Color: c.Fg, MaxLines: 1}, label)
			})
		})
		y += gtx.Dp(20) + gap
	}
	// the buttons: Start Workflow on its own row, then the VIP and ⋮
	bh := gtx.Dp(36)
	if s.runnable {
		at(gtx, image.Pt(pad, y), image.Pt(iw, bh), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = iw
			return popups.Button(gtx, th, &p.start, popups.BtnPrimary, popups.BtnMedium, wfDetailsNS.T("Start Workflow"), "play", 0)
		})
		y += bh + gap
	}
	x := pad
	if s.botUser != "" {
		vip := p.isVIP(s.botUser)
		ic, tip := "user-add", wfDetailsNS.T("Add to VIP")
		if vip {
			ic, tip = "user-hide", wfDetailsNS.T("Remove from VIP")
		}
		d := at(gtx, image.Pt(x, y), image.Pt(iw, bh), func(gtx layout.Context) layout.Dimensions {
			d := popups.Button(gtx, th, &p.vip, popups.BtnOutline, popups.BtnMedium, "  "+wfDetailsNS.T("VIP"), ic, 0)
			p.vipTip.Delay = 150 * time.Millisecond
			popups.Tooltip(gtx, th, &p.vipTip, p.vip.Hovered(), d.Size, popups.Above, tip, "")
			return d
		})
		x += d.Size.X + gap
	}
	d := at(gtx, image.Pt(x, y), image.Pt(bh, bh), func(gtx layout.Context) layout.Dimensions {
		d := popups.Button(gtx, th, &p.more, popups.BtnOutline, popups.BtnMedium, "", "ellipsis-vertical-filled", 0)
		p.moreTip.Delay = 150 * time.Millisecond
		popups.Tooltip(gtx, th, &p.moreTip, p.more.Hovered(), d.Size, popups.Above, wfDetailsNS.T("More actions"), "")
		return d
	})
	p.moreRect = image.Rectangle{Min: image.Pt(x, y), Max: image.Pt(x, y).Add(d.Size)}.Add(p.org.get(a.ptrWin.Sub(image.Pt(x, y))))
	y += bh + pad
	theme.Fill(gtx, image.Rect(0, y-gtx.Dp(1), w, y), theme.OtlTer(th))
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// wfTriggerType is getTriggerTypeIcon and getTriggerTypeLabel for the
// trigger kinds Clack keeps (a link trigger has neither; an event
// trigger's event type isn't kept, so it has none either).
func wfTriggerType(kind string) (icon, label string) {
	switch kind {
	case "scheduled":
		return "clock", wfShortNS.T("Starts on a schedule")
	case "webhook":
		return "webhook", wfShortNS.T("Starts with a Webhook event")
	case "external":
		return "code", wfShortNS.T("Starts with a Salesforce event")
	case "service":
		return "code", wfShortNS.T("Starts with a third party event")
	}
	return "", ""
}

// information is the details tab's Information section.
func (p *wfDetailsPane) information(gtx layout.Context, s wfShortcut) layout.Dimensions {
	a := p.a
	th := a.th
	c := &th.C
	w := gtx.Constraints.Max.X
	pad, gap := gtx.Dp(24), gtx.Dp(8)
	iw := w - 2*pad
	sec := c.FgMuted // .informationContainer: content-sec
	y := pad
	d := at(gtx, image.Pt(pad, y), image.Pt(iw, gtx.Dp(18)), func(gtx layout.Context) layout.Dimensions {
		return th.Label(gtx, 13, theme.Bold, sec, wfDetailsNS.T("Information"))
	})
	y += d.Size.Y
	item := func(icon, text string) {
		y += gap
		at(gtx, image.Pt(pad, y), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
			return icons.Get(icon)(gtx, unit.Dp(20), sec)
		})
		d := at(gtx, image.Pt(pad+gtx.Dp(28), y), image.Pt(max(1, iw-gtx.Dp(28)), gtx.Dp(200)), func(gtx layout.Context) layout.Dimensions {
			return th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 20, Weight: theme.Regular, Color: sec}, text)
		})
		y += max(d.Size.Y, gtx.Dp(20))
	}
	if len(s.collaborators) > 0 {
		item("wrench", wfDetailsNS.T("Managed by")+" "+p.managers(s))
	}
	if c0 := a.Core(); c0 != nil && s.owningTeam != "" && s.owningTeam != c0.Interactive().TeamID() {
		name := c0.Interactive().OwningTeamName(core.WorkflowTrigger{OwningTeam: s.owningTeam})
		item("workspace", wfDetailsNS.T("Created by")+" "+name)
	}
	y += pad
	theme.Fill(gtx, image.Rect(0, y-gtx.Dp(1), w, y), theme.OtlTer(th))
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// managers is ShortcutByline's LinkifyMembers: a workflow app's
// collaborators' names, ", " between them, within 30 characters, then
// "and {othersCount} others"; another workflow's, its app's name.
func (p *wfDetailsPane) managers(s wfShortcut) string {
	c := p.a.Core()
	if c == nil {
		return ""
	}
	var b strings.Builder
	n := 0
	for i, id := range s.collaborators {
		u, ok := c.User(id)
		if !ok {
			continue
		}
		name, _ := c.S.MemberName(id)
		if name == "" {
			name = u.Name
		}
		if i > 0 && n+len([]rune(name)) >= 30 {
			rest := len(s.collaborators) - i
			b.WriteString(i18n.Plain(wfShortNS.RT("and <l>{othersCount} {othersCount, plural, =1 {other} other {others}}</l>", "othersCount", rest)))
			break
		}
		sep := ""
		if i < len(s.collaborators)-1 {
			sep = ", "
		}
		b.WriteString(name + sep)
		n += len([]rune(name)) + len(sep)
	}
	return b.String()
}

// stepsSection is the Steps section: the trigger as the first step
// (its type's icon, else right-click, on aquarium; its label, else "When
// someone clicks the workflow link"), an arrow down, then the steps.
func (p *wfDetailsPane) stepsSection(gtx layout.Context, s wfShortcut) layout.Dimensions {
	th := p.a.th
	c := &th.C
	w := gtx.Constraints.Max.X
	pad, gap := gtx.Dp(24), gtx.Dp(8)
	iw := w - 2*pad
	y := pad
	d := at(gtx, image.Pt(pad, y), image.Pt(iw, gtx.Dp(18)), func(gtx layout.Context) layout.Dimensions {
		return th.Label(gtx, 13, theme.Bold, c.Fg, wfDetailsNS.T("Steps"))
	})
	y += d.Size.Y + gap
	icon, label := wfTriggerType(s.kind)
	if icon == "" {
		icon, label = "right-click", wfDetailsNS.T("When someone clicks the workflow link")
	}
	d = at(gtx, image.Pt(pad, y), image.Pt(iw, gtx.Dp(400)), func(gtx layout.Context) layout.Dimensions {
		return p.entityList(gtx, []wfEntity{{icon: icon, theme: "aquarium", label: label}})
	})
	y += d.Size.Y + gap
	at(gtx, image.Pt(pad+(iw-gtx.Dp(20))/2, y), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
		return icons.Get("arrow-down")(gtx, unit.Dp(20), popups.ColorsOf(th).Fg3) // content-ter
	})
	y += gtx.Dp(20) + gap
	d = at(gtx, image.Pt(pad, y), image.Pt(iw, 1<<20), func(gtx layout.Context) layout.Dimensions {
		return p.stepsWithBranches(gtx, s.steps, "")
	})
	y += d.Size.Y + pad // the last section has no line under it
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// wfEntity is one step entity (WorkflowStepEntity): its icon in a themed
// 32px tile and its label.
type wfEntity struct{ icon, theme, label string }

// stepEntity is renderStepEntities' entity for a step: the callback's
// icon (8uOE) and theme (Eu1t), "branch" for a switch.
func stepEntity(st core.WorkflowStep) wfEntity {
	e := wfEntity{icon: wfStepIcon(st.CallbackID), theme: wfStepTheme(st.CallbackID), label: st.Title}
	if st.Type == "switch" {
		e.icon = "branch"
	}
	return e
}

// stepsWithBranches is renderStepsWithBranches: the level's steps but
// loops as one entity list, then each switch's cases ("Branch {letter}",
// or "Fallback" without an expression) in their colour, then each loop.
func (p *wfDetailsPane) stepsWithBranches(gtx layout.Context, steps []core.WorkflowStep, prefix string) layout.Dimensions {
	th := p.a.th
	w := gtx.Constraints.Max.X
	y := 0
	var ents []wfEntity
	for _, st := range steps {
		if st.Type != "loop" {
			ents = append(ents, stepEntity(st))
		}
	}
	if len(ents) > 0 {
		d := at(gtx, image.Pt(0, y), image.Pt(w, 1<<20), func(gtx layout.Context) layout.Dimensions { return p.entityList(gtx, ents) })
		y += d.Size.Y
	}
	for _, st := range steps {
		switch st.Type {
		case "switch":
			for i, cs := range st.Steps {
				letter := string(rune('A' + i))
				path := letter
				if prefix != "" {
					path = prefix + letter
				}
				title := wfBuilderNS.T("Branch {branchIndex}", "branchIndex", path)
				if !cs.Expression {
					title = wfBuilderNS.T("Fallback")
				}
				bg, fg := wfBranchColors(th, cs.Color)
				y += gtx.Dp(12) // margin-top 12, margin-left 16, padding 8
				x0 := gtx.Dp(16)
				bw := w - x0
				inner := gtx.Dp(8)
				body := func(gtx layout.Context) layout.Dimensions {
					ty := 0
					at(gtx, image.Pt(inner, inner), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
						return icons.Get("branch")(gtx, unit.Dp(20), fg)
					})
					d := at(gtx, image.Pt(inner+gtx.Dp(20)+gtx.Dp(12), inner), image.Pt(max(1, bw-4*inner-gtx.Dp(32)), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
						return vcenter(gtx, gtx.Dp(20), func(gtx layout.Context) layout.Dimensions {
							return th.Label(gtx, 13, theme.Bold, fg, title)
						})
					})
					ty = inner + max(d.Size.Y, gtx.Dp(20)) + inner // .branchTitle padding 8
					d = at(gtx, image.Pt(0, ty), image.Pt(bw-2*inner, 1<<20), func(gtx layout.Context) layout.Dimensions {
						return p.stepsWithBranches(gtx, cs.Steps, path)
					})
					return layout.Dimensions{Size: image.Pt(bw-2*inner, ty+d.Size.Y)}
				}
				call, bd := recordW(gtx, image.Pt(bw-2*inner, 1<<20), body)
				h := bd.Size.Y + 2*inner
				theme.RRect(gtx, image.Rect(x0, y, w, y+h), gtx.Dp(12), bg)
				macroAtUI(gtx, call, image.Pt(x0+inner, y+inner))
				y += h
			}
		case "loop":
			// .loopWrapper: otl-ter outline, radius 12, margin-top 12,
			// padding 8; .loopTitle: bold, 12px gap, padding 8, margin-
			// bottom 8; the loop icon a 36px campfire tile
			y += gtx.Dp(12)
			inner := gtx.Dp(8)
			body := func(gtx layout.Context) layout.Dimensions {
				ts := gtx.Dp(36)
				bgc, fgc := wfPalette(th, "campfire0"), wfPalette(th, "campfire70")
				theme.RRect(gtx, image.Rect(inner, inner, inner+ts, inner+ts), gtx.Dp(8), bgc)
				at(gtx, image.Pt(inner+(ts-gtx.Dp(20))/2, inner+(ts-gtx.Dp(20))/2), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get("refresh")(gtx, unit.Dp(20), fgc)
				})
				at(gtx, image.Pt(inner+ts+gtx.Dp(12), inner), image.Pt(max(1, w-6*inner-ts), ts), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, ts, func(gtx layout.Context) layout.Dimensions {
						return th.Label(gtx, 13, theme.Bold, th.C.Fg, wfBuilderNS.T("Repeat"))
					})
				})
				ty := inner + ts + inner + gtx.Dp(8)
				d := at(gtx, image.Pt(0, ty), image.Pt(w-2*inner, 1<<20), func(gtx layout.Context) layout.Dimensions {
					return p.stepsWithBranches(gtx, st.Steps, prefix)
				})
				return layout.Dimensions{Size: image.Pt(w-2*inner, ty+d.Size.Y)}
			}
			call, bd := recordW(gtx, image.Pt(w-2*inner, 1<<20), body)
			h := bd.Size.Y + 2*inner
			theme.Border(gtx, image.Rect(0, y, w, y+h), gtx.Dp(12), max(1, gtx.Dp(1)), theme.OtlTer(th))
			macroAtUI(gtx, call, image.Pt(inner, y+inner))
			y += h
		}
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// entityList is EntityList over step entities: one bordered card
// (otl-ter, radius 12, base-pry) whose entities share their borders;
// each is .headerContainer (padding 12, 12px gap) with its 32px icon
// tile (1px otl-ter, radius 8, the theme's -0 fill and -60 icon;
// charcoal: no fill or border, content-pry) and its label (13px bold,
// line-height 1.25, two lines at most).
func (p *wfDetailsPane) entityList(gtx layout.Context, ents []wfEntity) layout.Dimensions {
	th := p.a.th
	c := &th.C
	w := gtx.Constraints.Max.X
	pad, ts := gtx.Dp(12), gtx.Dp(32)
	lh := gtx.Dp(16) // 13 × 1.25
	one := max(1, gtx.Dp(1))
	heights := make([]int, len(ents))
	labels := make([]func(gtx layout.Context) layout.Dimensions, len(ents))
	total := 0
	for i, e := range ents {
		e := e
		tw := max(1, w-2*pad-ts-pad)
		labels[i] = func(gtx layout.Context) layout.Dimensions {
			return th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 16.25, Weight: theme.Bold, Color: c.Fg, MaxLines: 2}, e.label)
		}
		_, d := recordW(gtx, image.Pt(tw, 2*lh), labels[i])
		heights[i] = 2*pad + max(ts, d.Size.Y)
		total += heights[i]
	}
	total += one * (len(ents) + 1)
	r := image.Rect(0, 0, w, total)
	rad := gtx.Dp(12)
	theme.RRect(gtx, r, rad, theme.OtlTer(th))
	theme.RRect(gtx, r.Inset(one), rad-one, c.Bg)
	y := one
	for i, e := range ents {
		h := heights[i]
		if i > 0 {
			theme.Fill(gtx, image.Rect(one, y-one, w-one, y), theme.OtlTer(th))
		}
		ty := y + (h-ts)/2
		bg, fg, border := wfEntityTheme(th, e.theme)
		if border {
			theme.RRect(gtx, image.Rect(pad, ty, pad+ts, ty+ts), gtx.Dp(8), theme.OtlTer(th))
			theme.RRect(gtx, image.Rect(pad+one, ty+one, pad+ts-one, ty+ts-one), gtx.Dp(8)-one, bg)
		} else if bg.A > 0 {
			theme.RRect(gtx, image.Rect(pad, ty, pad+ts, ty+ts), gtx.Dp(8), bg)
		}
		is := gtx.Dp(20)
		at(gtx, image.Pt(pad+(ts-is)/2, ty+(ts-is)/2), image.Pt(is, is), func(gtx layout.Context) layout.Dimensions {
			return icons.Get(e.icon)(gtx, unit.Dp(20), fg)
		})
		tx := pad + ts + pad
		at(gtx, image.Pt(tx, y), image.Pt(max(1, w-tx-pad), h), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, h, labels[i])
		})
		y += h + one
	}
	return layout.Dimensions{Size: r.Max}
}

// wfStepIcon is 8uOE's icon for a step's callback id.
func wfStepIcon(cb string) string {
	switch cb {
	case "send_message":
		return "message"
	case "send_dm":
		return "direct-messages"
	case "open_form":
		return "form"
	case "create_channel":
		return "channel"
	case "archive_channel":
		return "archive"
	case "unarchive_channel":
		return "unarchive"
	case "add_pin", "remove_pin":
		return "pin"
	case "update_channel_topic":
		return "megaphone"
	case "invite_user_to_channel":
		return "email"
	case "create_usergroup":
		return "user-groups"
	case "summarize_channel", "search_summary":
		return "ai-summary"
	case "ai_answer":
		return "ai-sparkle"
	case "send_slackbot_prompt", "send_prompt_to_agent":
		return "slackbot-color"
	case "open_3p_agent_conversation":
		return "ai-agents"
	case "create_code_channel":
		return "terminal"
	case "loop_step":
		return "refresh"
	}
	return "slack-logo"
}

// wfStepTheme is Eu1t's icon theme for a step's callback id.
func wfStepTheme(cb string) string {
	switch cb {
	case "send_message", "send_dm":
		return "aubergine"
	case "open_form":
		return "jade"
	case "create_channel":
		return "flamingo"
	case "archive_channel", "remove_pin":
		return "indigo"
	case "unarchive_channel", "add_pin":
		return "tomato"
	case "update_channel_topic":
		return "tangerine"
	case "invite_user_to_channel":
		return "cilantro"
	case "create_usergroup", "loop_step":
		return "campfire"
	case "send_slackbot_prompt", "send_prompt_to_agent":
		return "charcoal"
	}
	return "aquarium"
}

// wfPalettes are the --dt_color-plt-* values these surfaces use
// (slack-kit-gantry-v2-styles: :root, .sk-client-theme--dark).
var wfPalettes = map[string][2]uint32{
	"aquarium0": {0xe3ffff, 0x011c23}, "aquarium60": {0x167d8e, 0x43a1ab},
	"aubergine0": {0xf9edff, 0x200221}, "aubergine60": {0xa350af, 0xbc80ce},
	"jade0": {0xe3fff3, 0x05241b}, "jade60": {0x178f65, 0x3daa7c}, "jade80": {0x0e674d, 0x8dd2b5},
	"flamingo0": {0xffe8ef, 0x300005}, "flamingo60": {0xe01e5a, 0xde678a},
	"indigo0": {0xf0f3fc, 0x040b3d}, "indigo60": {0x3651c0, 0x657ace}, "indigo80": {0x1e328f, 0xabb7e1},
	"tomato0": {0xffebeb, 0x350803}, "tomato60": {0xc73c1c, 0xd86555},
	"tangerine0": {0xffede5, 0x381000}, "tangerine60": {0xdc540c, 0xda8056},
	"cilantro0": {0xf0ffe9, 0x102310}, "cilantro60": {0x228b07, 0x5da442},
	"campfire0": {0xfff2e0, 0x321600}, "campfire60": {0xbc6000, 0xcc771f}, "campfire70": {0xa35200, 0xdc9d56},
	"honeycomb0": {0xfcf2d9, 0x361e00}, "honeycomb60": {0xc07f00, 0xcd9625},
	"gray0": {0xf8f8f8, 0x101214}, "gray80": {0x333133, 0xd1d2d3},
	"sunflower0": {0xfffae0, 0x2f1e00}, "sunflower80": {0x8b6800, 0xe6d16e},
	"paprika0": {0xfff0eb, 0x380d00}, "paprika80": {0xa23004, 0xddad9e},
	"lagoon0": {0xe3f8ff, 0x001a2d}, "lagoon80": {0x0b527b, 0x87cce3},
}

func wfPalette(th *theme.Theme, name string) color.NRGBA {
	v := wfPalettes[name][0]
	if th.Dark {
		v = wfPalettes[name][1]
	}
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

// wfEntityTheme is IconContainer's theme: the tile's fill and icon colour
// and whether it keeps its border (charcoal has none and no fill).
func wfEntityTheme(th *theme.Theme, name string) (bg, fg color.NRGBA, border bool) {
	if name == "charcoal" {
		return color.NRGBA{}, th.C.Fg, false
	}
	if _, ok := wfPalettes[name+"0"]; !ok {
		return th.C.Bg, popups.ColorsOf(th).Fg3, true // content-ter
	}
	return wfPalette(th, name+"0"), wfPalette(th, name+"60"), true
}

// wfBranchColors are a branch box's --branch-background-color (-0) and
// --branch-text-color (-80): gray by default, tomato as paprika.
func wfBranchColors(th *theme.Theme, color string) (bg, fg color.NRGBA) {
	switch color {
	case "tomato":
		color = "paprika"
	case "sunflower", "jade", "indigo", "lagoon":
	default:
		color = "gray"
	}
	return wfPalette(th, color+"0"), wfPalette(th, color+"80")
}

// recordW records w laid out within max.
func recordW(gtx layout.Context, max image.Point, w layout.Widget) (op.CallOp, layout.Dimensions) {
	g := gtx
	g.Constraints = layout.Constraints{Max: max}
	m := op.Record(g.Ops)
	d := w(g)
	return m.Stop(), d
}

// macroAtUI replays call with its origin at pt.
func macroAtUI(gtx layout.Context, call op.CallOp, pt image.Point) {
	defer op.Offset(pt).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
}

// wfCopy puts s on the clipboard.
func wfCopy(gtx layout.Context, s string) {
	gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(s))})
}
