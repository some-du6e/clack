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
// MessageActionsMenu, and Menu inside it. Its actions are the native rows as Slack's
// descriptors ({id, name, icon, "data-qa", shortcut, disabled, danger,
// submenuActions on the rows Slack gives them}), and the actions a patch
// hands its <Original> are what the menu draws: groups reordered, renamed,
// moved or hidden, a row's className (classes plugin CSS styles: Better
// Message Menu's big buttons, menu.go's inline rows), and rows of actions
// Clack doesn't have (Slick's api.menu.addMessageActions). The Organize
// row's submenu is the submenuActions its descriptor comes back with
// (Better Message Menu's Organize section; Slack's OrganizeMessageActions-
// Submenu draws the same rows: its own, or those Better Message Menu's
// context hands the submenu's MAMenu). Picking a row goes the way Slack's
// dispatchMessageAction goes, `onSelectAction(event, {id, uiElement},
// context) || native(…)`: once a patch replaced onSelectAction, a native
// row asks it first and runs only when it returns something falsy (JS
// decides at once: a returned Promise counts as truthy; a throw stops the
// pick), in the menu's view container, with the click's modifiers
// (maPick); a row only the plugin has goes to the plugin alone.
//
// Slack's Organize and Connect to apps rows render submenu components
// (OrganizeMessageActionsSubmenu, ConnectToAppsMessageActionsSubmenu)
// whose submenus are MAMenus of their own: their own actions (Organize's
// from the store, not the root descriptor's submenuActions), no
// autoClogProps, and MessageActionsMenu's own onSelectAction, so a patch
// on the root MAMenu isn't asked about their rows and one on theirs is.
// Clack draws an open one through the same pair of boundaries, nested in
// the root MAMenu's tree, so the context a root patch provides reaches
// them (Better Message Menu hands Organize its section that way). Its
// reminder presets (RemindSubmenu) pick through the root's
// dispatchMessageAction instead, as {id: "remind"}.

import (
	"image"
	"log"
	"strings"
	"sync"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// maMenu is a message menu's MAMenu state.
type maMenu struct {
	props  slackc.Props         // MAMenu's own props (MessageActionsProps), actions added at render
	native []MenuItem           // the native rows (Menu.orig as the menu opened)
	byID   map[string]*MenuItem // every native row and submenu row by its id (maID)
	target image.Rectangle      // what the menu opened from, window px (targetBounds)
	from   slackc.Props         // the patched props last applied (held: its address can't be reused)
	onSel  slackc.JSFunc        // the onSelectAction a patch passed, when it's JS's
	selFn  func(...any)         // the native onSelectAction Go passes (dispatch runs the row natively)
	opened time.Time            // the menu's first frame (maHold)
	held   bool                 // this frame the card waits for the plugin's layout (Menu.Layout)
	queued []key.Event          // the keys pressed meanwhile, for the rows it lays out (Menu.keys)
	wrap   string               // a submenu's component (maSubmenus); "" for the root menu
}

// newSubMAMenu is the MAMenu an open message actions submenu renders:
// its own actions (the row's native submenu, until a patch lays out
// others), the root MAMenu's message, and MessageActionsMenu's own
// onSelectAction (Go's: falsy, the row runs natively). Its rows are found
// by the root's ids: Slack's handler takes any message action's id from
// any of its MAMenus (Better Message Menu moves Save into Organize).
func newSubMAMenu(root *maMenu, row MenuItem) *maMenu {
	p := slackc.Props{}
	for _, k := range []string{"triggerId", "ts", "channelId", "threadTs", "messageContainerType", "rollup"} {
		if v, ok := root.props[k]; ok {
			p[k] = v
		}
	}
	return &maMenu{props: p, native: row.Sub, byID: root.byID, wrap: row.maSub}
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

// maSubmenus are the components those rows' submenus render, each with a
// MAMenu of its own (2rcC aq, ay).
var maSubmenus = map[string]string{
	"organize_actions_menu":        "OrganizeMessageActionsSubmenu",
	"connect_to_apps_actions_menu": "ConnectToAppsMessageActionsSubmenu",
}

// maRemind are the rows whose presets RemindSubmenu picks through the
// root MAMenu's dispatchMessageAction with the row's id (krBk
// RemindMenuItem: `u(e, {id, frecencyId, time, dateDue, uiElement})`).
var maRemind = map[string]bool{"remind": true, "remind_later": true}

// maQA is each action's data-qa where it isn't its id (Slack's selectors).
var maQA = map[string]string{
	"pin":               "pin_message",
	"subscribe":         "follow_message",
	"comment":           "start_thread",
	"broadcast_message": "broadcast-reply-action",
}

// newMAMenu indexes the menu's native rows for MAMenu. A row with a Slack
// action id (MenuItem.Action: Slack's own ids, an app shortcut's action id)
// is found by it wherever a patch moves it (Better Message Menu moves rows
// in and out of Organize); a row without one is known only under its own
// parent ("<parent id>/<key>"), so a Connect to apps shortcut named "Pin"
// can't turn into Organize's Pin, nor one row into another's twin.
func newMAMenu(props slackc.Props, native []MenuItem, target image.Rectangle) *maMenu {
	ma := &maMenu{props: props, native: native, byID: map[string]*MenuItem{}, target: target}
	var walk func(items []MenuItem, parent string)
	walk = func(items []MenuItem, parent string) {
		keys := menuKeys(items)
		for i := range items {
			it := &items[i]
			if it.Separator || it.Header != "" || it.Note != "" {
				continue
			}
			if it.Key == "" {
				it.Key = keys[i] // the template key it had, kept when a patch renames it
			}
			if it.Action == "" {
				it.maid = it.Key
				if parent != "" {
					it.maid = parent + "/" + it.Key
				}
			}
			id := maID(it)
			if _, dup := ma.byID[id]; !dup {
				ma.byID[id] = it
			}
			if parent == "" {
				it.maSub = maSubmenus[id] // drawn through its submenu component, patched or not
			}
			walk(it.Sub, id)
		}
	}
	walk(ma.native, "")
	return ma
}

// maID is a row's MAMenu action id: its Slack id (MenuItem.Action), else
// its key under its parent's id (newMAMenu), else its key.
func maID(it *MenuItem) string {
	switch {
	case it.Action != "":
		return it.Action
	case it.maid != "":
		return it.maid
	}
	return it.Key
}

// maDescriptor is a native row as Slack's message action descriptor.
func maDescriptor(it *MenuItem) slackc.Props {
	id := maID(it)
	qa := it.QA
	if qa == "" {
		qa = maQA[id]
	}
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
	p["onSelectAction"] = ma.nativeSelect()
	p["onTriggerClose"] = m.root().closeAll
	if ma.wrap == "" {
		p["targetBounds"] = rectProps(ma.target, m.scale) // the trigger's DOMRect, CSS px
	} else if m.parent != nil {
		// Submenu.renderSubmenu's own (client-boot /+Me)
		p["isSubmenu"] = true
		p["onCloseSubmenu"] = m.parent.closeSub
	}
	return p
}

// nativeSelect is the onSelectAction Go passes: Slack's own, which
// answering falsy runs the row natively (MessageActionsMenu's F).
func (ma *maMenu) nativeSelect() func(...any) {
	if ma.selFn == nil {
		ma.selFn = func(...any) {}
	}
	return ma.selFn
}

// maWrapProps are an open submenu component's props
// (OrganizeMessageActionsSubmenu's): its row's label, the message, and
// MessageActionsMenu's onSelectAction.
func (m *Menu) maWrapProps() slackc.Props {
	ma := m.ma
	p := make(slackc.Props, len(ma.props)+3)
	for k, v := range ma.props {
		p[k] = v
	}
	p["$key"] = m.key
	if par := m.parent; par != nil && par.subIdx >= 0 && par.subIdx < len(par.items) {
		p["label"] = par.items[par.subIdx].Label
	}
	p["onSelectAction"] = ma.nativeSelect()
	return p
}

// layoutSub draws an open submenu: a message actions submenu through its
// component and its own MAMenu (Slack's renderSubmenu), any other as is.
// Called from the parent card's layout, so the boundaries nest in the root
// MAMenu's tree (slackc mounts them as portals there).
func (m *Menu) layoutSub(gtx layout.Context, win image.Point) {
	if m.ma == nil || m.ma.wrap == "" {
		m.layoutCard(gtx, win)
		return
	}
	m.win, m.scale = win, gtx.Metric.PxPerDp
	slackc.RenderLazy(gtx, m.ma.wrap, m.maWrapProps, m.layoutSubWrap)
}

// layoutSubWrap is a submenu component's native: its MAMenu.
func (m *Menu) layoutSubWrap(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	return slackc.RenderLazy(gtx, "MAMenu", m.maProps, m.layoutMAMenu)
}

// maHold is how long a message menu whose MAMenu a plugin patched waits
// for the plugin's first layout before it draws its own rows: Slack draws
// the menu once, as the patch laid it out, and a card showing Clack's
// rows a frame before the plugin's jumped (Better Message Menu's shorter
// card, ChatTabs' group popping in under the cursor).
const maHold = 150 * time.Millisecond

// holding reports whether the card waits for the plugin's layout
// (asking for a frame when the wait runs out). Only while a patch can
// change the menu: Slick's own MAMenu patch comes with the first message
// action provider and goes with the last (20_api.js), so with none, and no
// plugin's patch, a message menu draws in its first frame.
func (ma *maMenu) holding(gtx layout.Context) bool {
	now := frameNow(gtx)
	if !ma.waiting(now) {
		return false
	}
	if ma.opened.IsZero() {
		ma.opened = now
	}
	gtx.Execute(op.InvalidateCmd{At: ma.opened.Add(maHold)})
	return true
}

// waiting is holding without its bookkeeping (a card not drawn yet waits).
func (ma *maMenu) waiting(now time.Time) bool {
	if ma.from != nil || !slackc.Patched("MAMenu") {
		return false
	}
	return ma.opened.IsZero() || now.Before(ma.opened.Add(maHold))
}

func frameNow(gtx layout.Context) time.Time {
	if gtx.Now.IsZero() {
		return time.Now()
	}
	return gtx.Now
}

// layoutMAMenu is MAMenu's native: it draws the actions the patch passed
// (p nil: the native rows) in Slack's Menu, as MAMenu renders <Menu
// menuClassNames="p-message_actions_menu">{groups}</Menu> itself (no
// MenuFromTemplate in a message menu: MenuFromTemplate patches don't
// reach it in Slack either).
func (m *Menu) layoutMAMenu(gtx layout.Context, p slackc.Props) layout.Dimensions {
	m.maApply(p)
	return slackc.RenderLazy(gtx, "Menu", m.menuProps, m.layoutMenu)
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

// maSet makes items the menu's rows (its Menu's children).
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
	sub := native && maSubmenus[id] != "" && m.ma.wrap == ""
	switch {
	case sub:
		// its submenu draws its own MAMenu (layoutSub): the native rows
		// until a patch on that MAMenu lays out others, never the root
		// descriptor's submenuActions (Slack's submenu reads the store)
		it.maSub = maSubmenus[id]
	case d.Has("submenuActions"):
		it.Sub = m.maRows(d.List("submenuActions"))
		it.SubCaret = len(it.Sub) == 0
	case !native:
		it.Sub, it.SubCaret = nil, false
	}
	if native && maRemind[id] && m.ma.onSel.ID != 0 && len(it.Sub) > 0 {
		it.Sub = m.maPresets(id, d["uiElement"], it.Sub)
	}
	if len(it.Sub) > 0 || it.SubCaret {
		it.Do, it.DoCtx = nil, nil
		return it, true
	}
	if !native || m.ma.onSel.ID != 0 {
		var nat *MenuItem
		if native {
			c := *n
			nat = &c
		}
		ui := d["uiElement"]
		it.Do, it.DoCtx = nil, func(gtx layout.Context) { m.maSelect(gtx, id, ui, nat) }
	}
	return it, true
}

// maPresets are a reminder row's presets (Custom… too) picking as Slack's
// RemindSubmenu does: through this MAMenu's dispatchMessageAction with
// the row's id, so the patched onSelectAction decides and the preset
// runs only on a falsy answer.
func (m *Menu) maPresets(id string, uiElement any, presets []MenuItem) []MenuItem {
	out := make([]MenuItem, len(presets))
	for i := range presets {
		p := presets[i]
		out[i] = p
		if p.Separator || p.Header != "" || p.Note != "" || p.Disabled || (p.Do == nil && p.DoCtx == nil) {
			continue
		}
		nat := p
		out[i].Do, out[i].DoCtx = nil, func(gtx layout.Context) { m.maSelect(gtx, id, uiElement, &nat) }
	}
	return out
}

// maSelect is MAMenu's dispatchMessageAction for row id, run from the
// menu's Layout (activate, a template click, a key): with no JS
// onSelectAction the native row now; else the patched onSelectAction
// (event, {id, uiElement}, context) decides, and the native row runs only
// on a falsy return (maPick).
func (m *Menu) maSelect(gtx layout.Context, id string, uiElement any, nat *MenuItem) {
	f := m.ma.onSel
	if f.ID == 0 {
		if nat != nil {
			maRunNative(gtx, *nat)
		}
		return
	}
	mods := clickModsOf(m.h)
	pick := &maPick{h: m.h, id: id, nat: nat, mods: mods, at: time.Now()}
	// shown now, from the menu's Layout: the App draws it in the menu's view
	// container, where Slack runs the action (a split view's Reply in
	// thread opens there)
	m.h.ShowOverlay(pick)
	action := slackc.Props{"id": id}
	if uiElement != nil {
		action["uiElement"] = uiElement
	}
	f.CallTruthy(pick.answer, slackc.Props{"$event": maEvent(mods)}, action, m.maContext())
}

// maEvent is the click a pick hands onSelectAction (React's synthetic
// event there: preventDefault, stopPropagation).
func maEvent(mods key.Modifiers) slackc.Props {
	return slackc.Props{
		"type": "click", "button": 0,
		"shiftKey": mods.Contain(key.ModShift), "altKey": mods.Contain(key.ModAlt),
		"ctrlKey": mods.Contain(key.ModCtrl), "metaKey": mods.Contain(key.ModCommand),
	}
}

// maContext is dispatchMessageAction's context (2rcC MAMenu p), from this
// MAMenu's props.
func (m *Menu) maContext() slackc.Props {
	c := slackc.Props{}
	for _, k := range []string{"triggerId", "ts", "threadTs", "channelId", "messageContainerType", "userId", "botId", "isOnSearchTab"} {
		if v, ok := m.ma.props[k]; ok {
			c[k] = v
		}
	}
	if m.ma.wrap == "" {
		c["targetBounds"] = rectProps(m.ma.target, m.scale)
	}
	return c
}

// maRunNative runs a native row with the frame's context (clipboard rows).
func maRunNative(gtx layout.Context, it MenuItem) {
	if it.DoCtx != nil {
		it.DoCtx(gtx)
	}
	if it.Do != nil {
		it.Do()
	}
}

// maVerdict is what a plugin's onSelectAction answered for a pick.
type maVerdict uint8

const (
	maWaiting maVerdict = iota
	maTaken             // truthy, or its handler threw: Slack's own handler doesn't run
	maPassed            // falsy (or no runtime answered): the native row runs
)

// maPick runs a native row once the plugin's onSelectAction passed on it.
// It is shown at the pick, from the menu's Layout, so the App draws it in
// the menu's view container; it waits for the answer and then runs the
// row there, with that frame's context and the modifiers of the click it
// was picked with (split clicks), unless the plugin took the pick. It
// draws nothing.
type maPick struct {
	base
	h    Host
	id   string
	nat  *MenuItem // nil: a row only the plugin has
	mods key.Modifiers
	at   time.Time // the pick

	mu       sync.Mutex
	verdict  maVerdict
	answered time.Time
}

// maStale is how long after the pick a passed-on row may still run. The
// call waits its turn on the JS thread, where Slack's runs in the click's
// handler while the page waits: a row that answers later, behind a busy
// runtime, would copy over what the user copied since or open a dialog in
// the middle of what they're doing, so it doesn't run (logged).
var maStale = time.Second

// answer is CallTruthy's done (any goroutine).
func (p *maPick) answer(truthy, threw bool, err error) {
	v := maTaken
	switch {
	case err != nil:
		v = maPassed // no runtime answered: no plugin to take the pick
	case threw:
	case !truthy:
		v = maPassed
	}
	p.mu.Lock()
	p.verdict, p.answered = v, time.Now()
	p.mu.Unlock()
	p.h.Invalidate()
}

func (p *maPick) Layout(gtx layout.Context) layout.Dimensions {
	if p.done {
		return layout.Dimensions{}
	}
	p.mu.Lock()
	v, late := p.verdict, p.answered.Sub(p.at)
	p.mu.Unlock()
	switch v {
	case maWaiting:
		return layout.Dimensions{}
	case maPassed:
		p.done = true
		if p.nat == nil {
			break
		}
		if late > maStale {
			log.Printf("popups: the plugin answered message menu pick %q after %v: not running it", p.id, late.Round(time.Millisecond))
			break
		}
		nat := *p.nat
		withClickMods(p.h, p.mods, func() { maRunNative(gtx, nat) })
	default:
		p.done = true
	}
	return layout.Dimensions{}
}

// Invisible: it covers nothing.
func (p *maPick) Invisible() bool { return true }

// clickModsHost is the App's split click (Slack's shouldSplit on the
// event): this frame's click's modifiers, and running f under the ones a
// row was picked with in an earlier frame.
type clickModsHost interface {
	ClickMods() key.Modifiers
	WithClickMods(m key.Modifiers, f func())
}

func clickModsOf(h Host) key.Modifiers {
	if c, ok := h.(clickModsHost); ok {
		return c.ClickMods()
	}
	return 0
}

func withClickMods(h Host, m key.Modifiers, f func()) {
	if c, ok := h.(clickModsHost); ok {
		c.WithClickMods(m, f)
		return
	}
	f()
}

// maSubKey is Slack's MessageActionsMenu onMenuKeyDown (2rcC D): a key of
// one of the submenu actions MessageActionsMenu built (Organize's Pin P,
// not a patch's rows) runs it from the root menu, as Slack's own handler
// (onSelectAction isn't asked), and closes the menu. The open levels' own
// row keys go first (Menu.keys).
func (m *Menu) maSubKey(gtx layout.Context, ke key.Event) bool {
	if m.ma == nil || m.parent != nil {
		return false
	}
	id := shortcut.EventID(ke.Name, ke.Modifiers)
	if id == "" || MenuTypeAhead && !strings.Contains(id, "+") {
		return false
	}
	it, ok := m.ma.subKeys()[id]
	if !ok {
		return false
	}
	m.closeAll()
	maRunNative(gtx, it)
	return true
}

// subKeys maps the key ids of the native rows' submenu actions (the rows
// whose descriptors carry submenuActions) to them, later ones winning.
func (ma *maMenu) subKeys() map[string]MenuItem {
	var out map[string]MenuItem
	for i := range ma.native {
		row := &ma.native[i]
		if row.Disabled || !maSubmenuActions[maID(row)] {
			continue
		}
		for j := range row.Sub {
			s := &row.Sub[j]
			if s.Disabled || s.Separator {
				continue
			}
			for _, k := range s.Keys {
				if kid := shortcut.ID(k); kid != "" {
					if out == nil {
						out = map[string]MenuItem{}
					}
					out[kid] = *s
				}
			}
		}
	}
	return out
}
