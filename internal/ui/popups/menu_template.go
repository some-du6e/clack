package popups

// MenuFromTemplate (docs/PLUGINS-components.md §1.10, Index row 67). Every
// Menu crosses Slack's MenuFromTemplate boundary as a MenuTemplateItem[]
// (key, label, description, type, checked, template, icon, click,
// disabled, danger), and draws the template that comes back, so plugins
// can add, remove and modify items (AccountSwitcher, AdminBackend,
// Nicknames). Items whose key matches a native item keep its action and
// custom row; the rest are built from the template.

import (
	"image"
	"reflect"
	"strconv"
	"strings"
	"unicode"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// menuKeys returns each item's template key: Key, or a slug of the label
// ("Copy member ID" → "copy_member_id"), made unique within the list.
func menuKeys(items []MenuItem) []string {
	keys := make([]string, len(items))
	seen := make(map[string]bool, len(items))
	for i := range items {
		k := menuKey(&items[i], i)
		if seen[k] {
			k += "_" + strconv.Itoa(i)
		}
		seen[k] = true
		keys[i] = k
	}
	return keys
}

// LabelKey is the key a row with label and no Key gets (callers that
// translate a label pin the English one's key first).
func LabelKey(label string) string {
	if label == "" {
		return ""
	}
	return menuKey(&MenuItem{Label: label}, 0)
}

func menuKey(it *MenuItem, i int) string {
	if it.Key != "" {
		return it.Key
	}
	s := it.Label
	if s == "" {
		s = it.Header
	}
	if s == "" {
		if it.Separator {
			return "separator_" + strconv.Itoa(i)
		}
		return "item_" + strconv.Itoa(i)
	}
	var b strings.Builder
	us := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			us = false
		} else if !us && b.Len() > 0 {
			b.WriteByte('_')
			us = true
		}
	}
	k := strings.TrimSuffix(b.String(), "_")
	if k == "" {
		return "item_" + strconv.Itoa(i)
	}
	return k
}

// menuTemplate converts items to MenuTemplateItem[]. A native item's
// click queues its action on the root menu (it runs next frame with the
// frame context, so clipboard actions work when JS calls it).
func (m *Menu) menuTemplate(items []MenuItem) []any {
	keys := menuKeys(items)
	out := make([]any, 0, len(items))
	for i := range items {
		it := items[i]
		e := slackc.Props{"key": keys[i]}
		switch {
		case it.Separator:
			e["type"] = "separator"
		case it.Header != "":
			e["type"] = "header"
			e["label"] = it.Header
		case it.Note != "":
			e["type"] = "custom"
			e["label"] = it.Note
		default:
			switch {
			case it.Label != "":
				e["label"] = it.Label
			case it.Widget != nil:
				w := it.Widget
				e["type"] = "custom"
				e["label"] = slackc.Child("MenuItemLabel", nil, func(gtx layout.Context, _ slackc.Props) layout.Dimensions {
					return w(gtx, false)
				})
			}
			if it.Icon != "" {
				e["icon"] = it.Icon
			}
			if sc := shortcutProp(&it); sc != nil {
				e["shortcut"] = sc
			}
			if it.Danger {
				e["danger"] = true
			}
			if it.Disabled {
				e["disabled"] = true
			}
			if it.Checked {
				e["type"] = "checkbox"
				e["checked"] = true
			}
			if len(it.Sub) > 0 {
				e["type"] = "submenu"
				e["template"] = m.menuTemplate(it.Sub)
			}
			if it.Do != nil || it.DoCtx != nil {
				e["click"] = func() {
					r := m.root()
					r.pending = append(r.pending, it)
					m.h.Invalidate()
				}
			}
		}
		out = append(out, e)
	}
	return out
}

// menuFromTemplate converts the template a plugin passed to <Original>
// back to rows. base is what was sent (its keys map items back to their
// native actions and custom rows).
func menuFromTemplate(th *theme.Theme, list []any, base []MenuItem) []MenuItem {
	keys := menuKeys(base)
	byKey := make(map[string]int, len(base))
	for i, k := range keys {
		byKey[k] = i
	}
	out := make([]MenuItem, 0, len(list))
	for _, v := range list {
		e := asProps(v)
		if e == nil {
			continue
		}
		key := e.StrOr("key", "")
		typ := e.StrOr("type", "")
		bi, isBase := byKey[key]
		isBase = isBase && key != ""
		var it MenuItem
		if isBase {
			it = base[bi]
		}
		it.Key = key
		switch typ {
		case "separator", "divider":
			out = append(out, MenuItem{Key: key, Separator: true})
			continue
		case "header":
			if s, ok := e["label"].(string); ok {
				it = MenuItem{Key: key, Header: s}
			}
			if it.Header != "" {
				out = append(out, it)
			}
			continue
		}
		switch l := e["label"].(type) {
		case string:
			if it.Note != "" {
				it.Note = l
			} else {
				it.Label, it.Widget = l, nil
			}
		case slackc.Element:
			// a native custom row's label comes back as its own slot:
			// keep the native row; other ReactNode labels draw as given
			if !isBase || base[bi].Widget == nil {
				it.Label, it.Note = "", ""
				it.Widget = menuElemRow(th, l, &it)
			}
		}
		if e.Has("icon") {
			it.Icon, _ = e["icon"].(string)
		}
		if e.Has("shortcut") {
			it.Hint, it.Keys = templateShortcut(e["shortcut"])
		}
		if e.Has("danger") {
			it.Danger = e.Bool("danger")
		}
		if e.Has("disabled") {
			it.Disabled = e.Bool("disabled")
		}
		if typ == "checkbox" || typ == "radio" || e.Has("checked") {
			it.Checked = e.Bool("checked")
		}
		if sub := e.List("template"); sub != nil || typ == "submenu" {
			var bs []MenuItem
			if isBase {
				bs = base[bi].Sub
			}
			it.Sub = menuFromTemplate(th, sub, bs)
		}
		switch f := e["click"].(type) {
		case slackc.JSFunc:
			it.Do, it.DoCtx = func() { f.Call() }, nil
		case nil:
			if !isBase {
				it.Do, it.DoCtx = nil, nil
			}
		default:
			if !isBase {
				if fn, ok := e.Func("click"); ok {
					it.Do, it.DoCtx = func() { slackc.Props{"f": fn}.Invoke("f") }, nil
				}
			}
		}
		if !isBase && it.Label == "" && it.Widget == nil && it.Note == "" {
			continue
		}
		out = append(out, it)
	}
	return trimSeps(out)
}

// trimSeps drops leading, trailing and doubled separators, like Slack.
func trimSeps(items []MenuItem) []MenuItem {
	out := items[:0]
	for _, it := range items {
		if it.Separator && (len(out) == 0 || out[len(out)-1].Separator) {
			continue
		}
		out = append(out, it)
	}
	for len(out) > 0 && out[len(out)-1].Separator {
		out = out[:len(out)-1]
	}
	return out
}

// menuElemRow draws a ReactNode label (AccountSwitcher's account rows) as
// a highlightable menu row.
func menuElemRow(th *theme.Theme, e slackc.Element, it *MenuItem) func(gtx layout.Context, hl bool) layout.Dimensions {
	return func(gtx layout.Context, hl bool) layout.Dimensions {
		danger := it.Danger
		cl := colorsOf(th)
		w := gtx.Constraints.Max.X
		px := dp(gtx, 24)
		call, d := record(loose(gtx, image.Pt(maxInt(1, w-2*px), 1<<20)), func(gtx layout.Context) layout.Dimensions {
			return slackc.LayoutElement(gtx, e)
		})
		h := maxInt(dp(gtx, 28), d.Size.Y+dp(gtx, 8))
		if hl {
			bg := cl.Highlight
			if danger {
				bg = cl.DangerBg
			}
			fill(gtx, image.Rect(0, 0, w, h), bg)
		}
		macroAt(gtx, call, image.Pt(px, (h-d.Size.Y)/2))
		return layout.Dimensions{Size: image.Pt(w, h)}
	}
}

// ---- the boundary ----

var (
	menuClass     = slackc.Class("c-menu", "data-qa", "menu", "role", "menu")
	menuItemClass = slackc.Class("c-menu_item__li", "role", "presentation").Tag("li").
			Mods("c-menu_item__li--highlighted", "c-menu_item__li--danger", "c-menu_item__li--disabled")
	menuSepClass = slackc.Class("c-menu_separator__li").Tag("li")
	menuHdrClass = slackc.Class("c-menu_item__header").Tag("li")
)

// menuPosition is Slack's position name for the menu's side.
func (m *Menu) menuPosition() string {
	switch m.opts.Side {
	case Above:
		if m.opts.AlignRight {
			return "top-right"
		}
		return "top-left"
	case Right:
		return "right"
	case Left:
		return "left"
	}
	if m.opts.AlignRight {
		return "bottom-right"
	}
	return "bottom-left"
}

// tplProps are MenuFromTemplate's props for the native items.
func (m *Menu) tplProps() slackc.Props {
	return slackc.Props{
		"template":                m.menuTemplate(m.orig),
		"isOpen":                  true,
		"position":                m.menuPosition(),
		"targetBounds":            rectProps(m.opts.Anchor, m.scale),
		"stripTerminalSeparators": true,
		"onClose":                 m.closeAll,
		// Slack's MenuFromTemplate renders a Menu: its slot, so context a
		// wrapper provides (MessageLogger's rows) reaches it
		"children": slackc.Child("Menu", m.menuProps(), m.layoutMenu),
	}
}

func (m *Menu) mftProps() slackc.Props {
	p := m.tplProps()
	p["$key"] = m.key
	return p
}

// wrapProps are the props of the i-th component the menu is rendered
// inside (m.wraps), with the next one (or the MenuFromTemplate) as its
// children slot.
func (m *Menu) wrapProps(i int) slackc.Props {
	w := m.wraps[i]
	p := make(slackc.Props, len(w.Props)+4)
	for k, v := range w.Props {
		p[k] = v
	}
	p["$key"] = m.key + ":" + w.Name
	if !p.Has("onTriggerClose") {
		p["onTriggerClose"] = m.closeAll
	}
	if w.Name == "ContextMenuTriggerWithKeyboardFocus" {
		if h := m.ctxTemplate(); h != nil {
			p["getTemplate"] = h
		}
	}
	if i+1 < len(m.wraps) {
		p["children"] = slackc.Child(m.wraps[i+1].Name, m.wrapProps(i+1), m.wrapNative(i+1))
	} else {
		p["children"] = slackc.Child("MenuFromTemplate", m.tplProps(), m.layoutTemplate)
	}
	return p
}

// wrapNative draws the i-th wrapper: its children slot, or the next
// boundary on its own.
func (m *Menu) wrapNative(i int) slackc.Native {
	return func(gtx layout.Context, p slackc.Props) layout.Dimensions {
		if m.wraps[i].Name == "ContextMenuTriggerWithKeyboardFocus" {
			m.ctxPatched(p)
		}
		if i+1 < len(m.wraps) {
			return childrenOr(gtx, p, m.wraps[i+1].Name, func() slackc.Props { return m.wrapProps(i + 1) }, m.wrapNative(i+1))
		}
		return childrenOr(gtx, p, "MenuFromTemplate", m.mftProps, m.layoutTemplate)
	}
}

// layoutRoot draws the root card through the boundaries.
func (m *Menu) layoutRoot(gtx layout.Context, win image.Point) {
	m.win, m.scale = win, gtx.Metric.PxPerDp
	if m.opts.Raw {
		// the items came out of a plugin's MenuFromTemplate, which renders
		// a Menu
		slackc.RenderLazy(gtx, "Menu", m.menuProps, m.layoutMenu)
		return
	}
	if len(m.wraps) > 0 {
		slackc.RenderLazy(gtx, m.wraps[0].Name, func() slackc.Props { return m.wrapProps(0) }, m.wrapNative(0))
		return
	}
	slackc.RenderLazy(gtx, "MenuFromTemplate", m.mftProps, m.layoutTemplate)
}

// layoutTemplate is MenuFromTemplate's native: it applies the template the
// plugin passed (p nil: the native items) and draws its Menu.
func (m *Menu) layoutTemplate(gtx layout.Context, p slackc.Props) layout.Dimensions {
	m.applyTemplate(p)
	return childrenOr(gtx, p, "Menu", m.menuProps, m.layoutMenu)
}

func (m *Menu) applyTemplate(p slackc.Props) {
	if p == nil || !p.Has("template") {
		if m.tplFrom != nil {
			m.tplFrom = nil
			m.setItems(m.orig)
		}
		return
	}
	if m.tplFrom != nil && sameMap(m.tplFrom, p) {
		return
	}
	m.tplFrom = p // held, so its address can't be reused by another map
	m.setItems(menuFromTemplate(m.h.Theme(), p.List("template"), m.orig))
}

func sameMap(a, b slackc.Props) bool {
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}

func (m *Menu) setItems(items []MenuItem) {
	m.items = items
	m.nExtra = 0
	if len(m.clicks) != len(items) {
		m.clicks = make([]widget.Clickable, len(items))
	}
	if m.sel >= len(items) {
		m.sel = -1
	}
	if m.sub != nil && (m.subIdx >= len(items) || len(items[m.subIdx].Sub) == 0) {
		m.closeSub()
	}
}

// runPending runs native item actions JS invoked through the template.
func (m *Menu) runPending(gtx layout.Context) {
	q := m.pending
	m.pending = nil
	for _, it := range q {
		if !it.KeepOpen {
			m.closeAll()
		}
		if it.DoCtx != nil {
			it.DoCtx(gtx)
		}
		if it.Do != nil {
			it.Do()
		}
	}
}

// rectProps is r (window px) as a DOMRect in CSS px (scale: px per CSS
// px, gtx.Metric.PxPerDp).
func rectProps(r image.Rectangle, scale float32) slackc.Props {
	if scale <= 0 {
		scale = 1
	}
	f := func(v int) float64 { return float64(float32(v) / scale) }
	return slackc.Props{
		"left": f(r.Min.X), "top": f(r.Min.Y), "right": f(r.Max.X), "bottom": f(r.Max.Y),
		"x": f(r.Min.X), "y": f(r.Min.Y), "width": f(r.Dx()), "height": f(r.Dy()),
	}
}
