package popups

// Slack's Menu (the c-menu container MenuFromTemplate renders) and
// ContextMenuTriggerWithKeyboardFocus (docs/PLUGINS-components.md §9).
// Slick's MessageLogger appends rows to Menu's children inside
// MessageActionsMenu; AccountSwitcher clones its team-switcher rows; ChatTabs
// extends a context menu trigger's getTemplate.

import (
	"encoding/json"

	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
)

// menuProps are Slack's Menu props: its rows are MenuItem children (one
// slot per native item, data-qa "<key>-wrapper" like Slack's).
func (m *Menu) menuProps() slackc.Props {
	keys := menuKeys(m.orig)
	kids := make([]any, 0, len(m.orig))
	for i := range m.orig {
		it := &m.orig[i]
		ip := slackc.Props{"data-qa": keys[i] + "-wrapper", "danger": it.Danger, "disabled": it.Disabled}
		switch {
		case it.Separator:
			ip["type"] = "separator"
		case it.Header != "":
			ip["type"], ip["label"] = "header", it.Header
		case it.Label != "":
			ip["label"] = it.Label
		}
		kids = append(kids, slackc.Child("MenuItem", ip, pfNoNative))
	}
	return slackc.Props{
		"$key":                    m.key + ":menu",
		"menuClassNames":          m.opts.MenuClass,
		"isOpen":                  true,
		"position":                m.menuPosition(),
		"targetBounds":            rectProps(m.opts.Anchor, m.scale),
		"stripTerminalSeparators": true,
		"isMainWindow":            true,
		"isChildWindow":           false,
		"windowId":                "main",
		"onClose":                 m.closeAll,
		"onTriggerClose":          m.closeAll,
		"children":                kids,
	}
}

// layoutMenu is Menu's native: the card with the current items, plus any
// rows a plugin added to its children (host elements, not our MenuItem
// slots), drawn after the native rows with their own click handlers.
func (m *Menu) layoutMenu(gtx layout.Context, p slackc.Props) layout.Dimensions {
	var extra []MenuItem
	if e, ok := slackc.PropChildren(p); ok {
		th := m.h.Theme()
		for _, n := range e.Nodes {
			if _, isSlot := slackc.SlotOf(n); isSlot {
				continue
			}
			it := MenuItem{Key: "plugin-row"}
			it.Widget = menuElemRow(th, slackc.Element{Nodes: []*slackc.Node{n}}, &it)
			extra = append(extra, it)
		}
	}
	if len(extra) > 0 || m.nExtra > 0 {
		base := m.items[:len(m.items)-m.nExtra]
		items := append(base[:len(base):len(base)], extra...)
		m.setItems(items)
		m.nExtra = len(extra)
	}
	m.layoutCard(gtx, m.win)
	return layout.Dimensions{Size: m.win}
}

// ctxTemplate is the getTemplate a ContextMenuTriggerWithKeyboardFocus
// wrapper passes to JS: a taut function JS calls synchronously, returning
// the native template (a snapshot; native actions come back by key).
func (m *Menu) ctxTemplate() any {
	if m.ctxH != nil {
		return m.ctxH
	}
	t := tautRT.Load()
	if t == nil {
		return nil
	}
	tpl, err := json.Marshal(tautTemplate(m.orig))
	if err != nil {
		return nil
	}
	m.ctxH = t.Func(func([]json.RawMessage) (any, error) { return json.RawMessage(tpl), nil })
	return m.ctxH
}

// ctxPatched asks a plugin-wrapped getTemplate (ChatTabs adds "Open in new
// Slick tab") once and takes its template as the menu's items.
func (m *Menu) ctxPatched(p slackc.Props) {
	f, ok := p["getTemplate"].(slackc.JSFunc)
	if !ok || m.ctxDone {
		return
	}
	m.ctxDone = true
	f.CallResult(func(res any, err error) {
		list, ok := res.([]any)
		if err != nil || !ok || m.done {
			return
		}
		items := menuFromTemplate(m.h.Theme(), list, m.orig)
		m.orig, m.tplFrom = items, nil
		m.setItems(items)
		m.h.Invalidate()
	})
}
