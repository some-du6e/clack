package popups

// MAMenu, the renderer of Slack's message actions menu (gantry-v2-async-
// client-boot-shared 2rcC, build 2c0d02ca130f). MessageActionsMenu renders
// <MAMenu actions targetBounds onSelectAction renderMenuItem autoClogProps
// onTriggerClose ts threadTs channelId messageContainerType …>, and MAMenu
// draws the menu (menuClassNames p-message_actions_menu): each group of
// action descriptors through renderMenuItem, the button classed
// p-message_actions_menu__<id> plus the descriptor's className, a
// separator between groups (renderActionGroup), and a selection through
// dispatchMessageAction: onSelectAction(event, {id, uiElement}, context),
// then Slack's own handler when that answers falsy. Its submenus (Organize,
// Connect to apps) render MAMenu again, without autoClogProps.
//
// Clack's message menus (the More actions menu, the message context menu,
// a search result's More actions) draw MAMenu as a boundary inside
// MessageActionsMenu. Its actions are the native rows as Slack's
// descriptors ({id, name, icon, "data-qa", shortcut, disabled, danger,
// submenuActions on the rows Slack gives them}), and the actions a patch
// hands its <Original> are what the menu draws: groups reordered, renamed,
// moved or hidden, a row's className (classes plugin CSS styles: Better
// Message Menu's big buttons, menu.go's inline rows), and rows of actions
// Clack doesn't have (Slick's api.menu.addMessageActions). The Organize
// row's submenu is the submenuActions its descriptor comes back with
// (Better Message Menu's Organize section; Slack's OrganizeMessageActions-
// Submenu draws the same rows: its own, or those Better Message Menu's
// context hands the submenu's MAMenu). Picking a row of an action Clack
// doesn't have goes to the patched onSelectAction; a native row asks it
// too once a patch replaced it, and runs natively when it answers falsy.

import (
	"strings"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/slackc"
)

// maMenu is a message menu's MAMenu state.
type maMenu struct {
	props  slackc.Props         // MAMenu's own props (MessageActionsProps), actions added at render
	native []MenuItem           // the native rows (Menu.orig as the menu opened)
	byID   map[string]*MenuItem // every native row and submenu row by its action id
	from   slackc.Props         // the patched props last applied (held: its address can't be reused)
	onSel  slackc.JSFunc        // the onSelectAction a patch passed, when it's JS's
	selFn  func(...any)         // the native onSelectAction Go passes (dispatch runs the row natively)
}

// MessageActionsProps are MessageActionsMenu's MAMenu props for message m
// (2rcC MessageActionsMenu): what the menu's Within props say about the
// message, the menu's aria-label, and autoClogProps naming the root
// message actions menu ("overflow" for the More actions button, "context"
// for a right-click). Pass them as MenuOpts.MAMenu.
func MessageActionsProps(within slackc.Props, usage string) slackc.Props {
	variant := "message_actions_menu_overflow_menu"
	if usage == "context" {
		variant = "message_actions_menu_context_menu"
	}
	p := slackc.Props{"aria-label": "Message actions"}
	for _, k := range []string{"ts", "threadTs", "channelId", "messageContainerType", "userId", "botId", "triggerId", "isOnSearchTab", "targetBounds"} {
		if v, ok := within[k]; ok {
			p[k] = v
		}
	}
	p["autoClogProps"] = slackc.Props{
		"uiComponentName":    "message_actions_menu",
		"uiComponentVariant": variant,
		"uiPage":             within.StrOr("messageContainerType", ""),
	}
	return p
}

// maSubmenuActions are the rows whose descriptors carry their submenu's
// actions in Slack (tk Organize, e7 Connect to apps); the reminder row's
// submenu is drawn by its own component (a8 RemindSubmenu).
var maSubmenuActions = map[string]bool{"organize_actions_menu": true, "connect_to_apps_actions_menu": true}

// maQA is each action's data-qa where it isn't its id (Slack's selectors).
var maQA = map[string]string{
	"pin":               "pin_message",
	"subscribe":         "follow_message",
	"comment":           "start_thread",
	"broadcast_message": "broadcast-reply-action",
}

// newMAMenu indexes the menu's native rows for MAMenu.
func newMAMenu(props slackc.Props, native []MenuItem) *maMenu {
	ma := &maMenu{props: props, native: native, byID: map[string]*MenuItem{}}
	var walk func(items []MenuItem)
	walk = func(items []MenuItem) {
		keys := menuKeys(items)
		for i := range items {
			it := &items[i]
			if it.Separator || it.Header != "" || it.Note != "" {
				continue
			}
			if it.Key == "" {
				it.Key = keys[i] // the template key it had, kept when a patch renames it
			}
			id := maID(it)
			if _, dup := ma.byID[id]; !dup {
				ma.byID[id] = it
			}
			walk(it.Sub)
		}
	}
	walk(ma.native)
	return ma
}

// maID is a row's action id: Slack's (MenuItem.Action), else its key.
func maID(it *MenuItem) string {
	if it.Action != "" {
		return it.Action
	}
	return it.Key
}

// maDescriptor is a native row as Slack's message action descriptor.
func maDescriptor(it *MenuItem) slackc.Props {
	id := maID(it)
	qa := maQA[id]
	if qa == "" {
		qa = id
	}
	d := slackc.Props{"id": id, "name": it.Label, "data-qa": qa}
	if it.Icon != "" {
		d["icon"] = it.Icon
	}
	if sc := shortcutProp(it); sc != nil {
		d["shortcut"] = sc
	}
	if it.Disabled {
		d["disabled"] = true
	}
	if it.Danger {
		d["danger"] = true
	}
	if maSubmenuActions[id] {
		sub := make([]any, 0, len(it.Sub))
		for i := range it.Sub {
			if c := &it.Sub[i]; !c.Separator && c.Header == "" && c.Note == "" {
				sub = append(sub, maDescriptor(c))
			}
		}
		d["submenuActions"] = sub
	}
	return d
}

// actions are the native rows as MAMenu's actions: one group per run of
// rows between separators.
func (ma *maMenu) actions() []any {
	var groups []any
	var g []any
	for i := range ma.native {
		it := &ma.native[i]
		switch {
		case it.Separator:
			if len(g) > 0 {
				groups = append(groups, g)
				g = nil
			}
		case it.Header != "" || it.Note != "" || (it.Label == "" && it.Widget != nil):
		default:
			g = append(g, maDescriptor(it))
		}
	}
	if len(g) > 0 {
		groups = append(groups, g)
	}
	return groups
}

// maProps are MAMenu's props: Go's own, never what its tree handed back
// (the props of a boundary carry only what flows into it).
func (m *Menu) maProps() slackc.Props {
	ma := m.ma
	p := make(slackc.Props, len(ma.props)+5)
	for k, v := range ma.props {
		p[k] = v
	}
	p["$key"] = m.key + ":MAMenu"
	p["actions"] = ma.actions()
	if ma.selFn == nil {
		ma.selFn = func(...any) {} // Slack's onSelectAction; answering falsy, the row runs natively
	}
	p["onSelectAction"] = ma.selFn
	p["onTriggerClose"] = m.closeAll
	if !p.Has("targetBounds") {
		p["targetBounds"] = rectProps(m.opts.Anchor, m.scale)
	}
	return p
}

// layoutMAMenu is MAMenu's native: it draws the actions the patch passed
// (p nil: the native rows) through MenuFromTemplate, as every Clack menu.
func (m *Menu) layoutMAMenu(gtx layout.Context, p slackc.Props) layout.Dimensions {
	m.maApply(p)
	return slackc.RenderLazy(gtx, "MenuFromTemplate", m.mftProps, m.layoutTemplate)
}

// maApply takes the actions a patched MAMenu rendered with as the menu's
// rows (a new tree only; p nil goes back to the native rows).
func (m *Menu) maApply(p slackc.Props) {
	ma := m.ma
	if p == nil || !p.Has("actions") {
		if ma.from != nil {
			ma.from, ma.onSel = nil, slackc.JSFunc{}
			m.maSet(ma.native)
		}
		return
	}
	if ma.from != nil && sameMap(ma.from, p) {
		return
	}
	ma.from = p
	ma.onSel, _ = p["onSelectAction"].(slackc.JSFunc)
	var out []MenuItem
	for _, g := range p.List("actions") {
		list, ok := g.([]any)
		if !ok {
			continue
		}
		rows := m.maRows(list)
		if len(rows) == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, Sep())
		}
		out = append(out, rows...)
	}
	m.maSet(trimSeps(out))
}

// maSet makes items the rows the menu's MenuFromTemplate gets.
func (m *Menu) maSet(items []MenuItem) {
	m.orig, m.tplFrom = items, nil
	m.setItems(items)
}

func (m *Menu) maRows(list []any) []MenuItem {
	out := make([]MenuItem, 0, len(list))
	for _, v := range list {
		if d := asProps(v); d != nil {
			if it, ok := m.maRow(d); ok {
				out = append(out, it)
			}
		}
	}
	return out
}

// maRow is one descriptor's row, as MessageActionMenuItem draws it: the
// label (label, else name), the icon (a string: an icon name), danger,
// disabled, the shortcut, the classes renderMenuItem gives the button.
func (m *Menu) maRow(d slackc.Props) (MenuItem, bool) {
	id := d.StrOr("id", "")
	var it MenuItem
	n, native := m.ma.byID[id]
	if native && id != "" {
		it = *n
	} else {
		it = MenuItem{Key: id, Action: id}
	}
	// MessageActionMenuItem's label: e.label || e.name
	label, _ := d["label"].(string)
	if label == "" {
		label, _ = d["name"].(string)
	}
	if e, ok := d["name"].(slackc.Element); ok && label == "" {
		it.Label, it.Note = "", ""
		it.Widget = menuElemRow(m.h.Theme(), e, &it)
	} else {
		it.Label, it.Widget = label, nil
	}
	switch ic := d["icon"].(type) {
	case string:
		it.Icon = ic
	case slackc.Element:
		// a ReactNode icon: the row keeps its own
	default:
		it.Icon = ""
	}
	it.Danger = d.Bool("danger")
	it.Disabled = d.Bool("disabled")
	if d.Has("shortcut") {
		it.Hint, it.Keys = templateShortcut(d["shortcut"])
	} else {
		it.Hint, it.Keys = "", nil
	}
	cls := "p-message_actions_menu__" + id
	if c := strings.TrimSpace(d.StrOr("className", "")); c != "" {
		cls += " " + c
	}
	it.Class = cls
	if d.Has("submenuActions") {
		it.Sub = m.maRows(d.List("submenuActions"))
		it.SubCaret = len(it.Sub) == 0
	} else if !native {
		it.Sub, it.SubCaret = nil, false
	}
	if len(it.Sub) > 0 || it.SubCaret {
		it.Do, it.DoCtx = nil, nil
		return it, true
	}
	if !native || m.ma.onSel.ID != 0 {
		nat := (*MenuItem)(nil)
		if native {
			c := *n
			nat = &c
		}
		ui := d["uiElement"]
		it.Do, it.DoCtx = func() { m.maSelect(id, ui, nat) }, nil
	}
	return it, true
}

// maSelect is MAMenu's dispatchMessageAction for row id: the patched
// onSelectAction(event, {id, uiElement}, context) first, the native row
// when it answers falsy (or nothing patched it).
func (m *Menu) maSelect(id string, uiElement any, nat *MenuItem) {
	f := m.ma.onSel
	if f.ID == 0 {
		if nat != nil {
			maRunNative(m.h, *nat)
		}
		return
	}
	action := slackc.Props{"id": id}
	if uiElement != nil {
		action["uiElement"] = uiElement
	}
	h := m.h
	f.CallResult(func(res any, err error) {
		if err == nil && maTruthy(res) {
			return // the plugin's action ran (it closed the menu)
		}
		if nat != nil {
			maRunNative(h, *nat)
		}
	}, slackc.Props{"type": "click"}, action, m.maContext())
}

// maContext is dispatchMessageAction's context (2rcC MAMenu p).
func (m *Menu) maContext() slackc.Props {
	c := slackc.Props{}
	for _, k := range []string{"triggerId", "ts", "threadTs", "channelId", "messageContainerType", "targetBounds", "userId", "botId", "isOnSearchTab"} {
		if v, ok := m.ma.props[k]; ok {
			c[k] = v
		}
	}
	return c
}

// maRunNative runs a native row after a plugin passed on it; a row that
// needs the frame (the clipboard) gets the next one.
func maRunNative(h Host, it MenuItem) {
	if it.DoCtx == nil {
		if it.Do != nil {
			it.Do()
		}
		return
	}
	h.ShowOverlay(&frameRun{f: it.DoCtx, then: it.Do})
	h.Invalidate()
}

func maTruthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0
	}
	return true
}

// frameRun runs f once with the next frame's context, then then: a native
// menu action that writes the clipboard, after the menu that started it
// closed. It draws nothing.
type frameRun struct {
	base
	f    func(gtx layout.Context)
	then func()
}

func (r *frameRun) Layout(gtx layout.Context) layout.Dimensions {
	if r.done {
		return layout.Dimensions{}
	}
	r.done = true
	if r.f != nil {
		r.f(gtx)
	}
	if r.then != nil {
		r.then()
	}
	return layout.Dimensions{}
}

// Invisible: it covers nothing.
func (r *frameRun) Invisible() bool { return true }
