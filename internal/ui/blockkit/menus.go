package blockkit

import (
	"context"
	"encoding/json"
	"image"
	"sort"
	"strings"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// openURL opens http(s)/mailto links in the browser (openurl checks them
// and starts the browser off the UI goroutine). A var for tests.
var openURL = func(u string) { _ = openurl.Open(u) }

// ---- overflow (popups' menu, Slack's look) ----

func openOverflow(w *ew) {
	h := w.o.host()
	items := make([]popups.MenuItem, 0, len(w.e.Options))
	for i := range w.e.Options {
		o := w.e.Options[i]
		items = append(items, popups.MenuItem{Label: textOf(&o.Text), External: o.URL != "", Do: func() {
			if o.URL != "" {
				openURL(o.URL)
			}
			payload := overflowPayload(w.bid, w.e, &o)
			if w.e.Confirm != nil {
				showConfirm(h, w.e.Confirm, func() { w.send(payload) }, nil)
				return
			}
			w.send(payload)
		}})
	}
	w.open = true
	r := w.main.rect()
	m := popups.NewMenuOpts(h, items, popups.MenuOpts{Anchor: r, Width: 220, OnClose: func() { w.open = false; w.o.changed() }})
	h.ShowOverlay(m)
}

// ---- date & time (popups' Slack calendar and time list) ----

func openDate(w *ew, anchor image.Rectangle, onPick func(time.Time)) {
	var sel time.Time
	if d, ok := parseDate(w.value().SelectedDate); ok {
		sel = d
	}
	openDateAt(w, anchor, sel, onPick)
}

func openDateAt(w *ew, anchor image.Rectangle, sel time.Time, onPick func(time.Time)) {
	h := w.o.host()
	w.open = true
	w.o.changed()
	ov := popups.NewDatePicker(h, anchor, sel, time.Time{}, func(day time.Time) {
		w.open = false
		onPick(day)
	})
	h.ShowOverlay(&watch{Overlay: ov, onDone: func() { w.open = false; w.o.changed() }})
}

func openTime(w *ew, anchor image.Rectangle, cur string, onPick func(hm string)) {
	h := w.o.host()
	m, ok := parseHM(cur)
	if !ok {
		n := time.Now()
		m = (n.Hour()*60 + n.Minute() + 14) / 15 * 15 % (24 * 60)
	}
	w.open = true
	w.o.changed()
	ov := popups.NewTimePicker(h, anchor, m, 0, 15, func(mins int) {
		w.open = false
		onPick(formatHM(mins))
	})
	h.ShowOverlay(&watch{Overlay: ov, onDone: func() { w.open = false; w.o.changed() }})
}

// watch wraps an overlay to learn when it closes.
type watch struct {
	popups.Overlay
	onDone func()
	fired  bool
}

func (x *watch) Layout(gtx C) D {
	d := x.Overlay.Layout(gtx)
	if x.Overlay.Dismissed() && !x.fired {
		x.fired = true
		x.onDone()
	}
	return d
}

func (x *watch) Dismiss() {
	if d, ok := x.Overlay.(interface{ Dismiss() }); ok {
		d.Dismiss()
	}
	if !x.fired {
		x.fired = true
		x.onDone()
	}
}

// ---- confirm dialogs ----

// showConfirm shows a Block Kit confirmation dialog; ok runs on
// Confirm, deny (may be nil) on Cancel / Escape.
func showConfirm(h Host, c *Confirm, ok, deny func()) {
	title := textOf(c.Title)
	if title == "" {
		title = appViewNS.T("Are you sure?")
	}
	body := ""
	if c.Text != nil {
		body = textOf(c.Text)
	}
	// openConfirm's defaults: "OK", "Cancel", body "Are you sure?"
	yes, no := textOf(c.Confirm), textOf(c.Deny)
	if yes == "" {
		yes = appViewNS.T("OK")
	}
	if no == "" {
		no = appViewNS.T("Cancel")
	}
	if c.Text == nil {
		body = appViewNS.T("Are you sure?")
	}
	kind := popups.BtnPrimary
	if c.Style == "danger" {
		kind = popups.BtnDanger
	}
	decided := false
	var tv *textView
	if c.Text != nil {
		tv = newTextView(h, *c.Text)
	}
	btns := []*popups.ModalButton{
		{Label: no, Kind: popups.BtnOutline, Do: func() bool { return true }},
		{Label: yes, Kind: kind, Do: func() bool { decided = true; ok(); return true }},
	}
	m := popups.NewModal(h, popups.ModalOpts{
		Title: title,
		Width: 440,
		Body: func(gtx C) D {
			if tv == nil {
				return para(gtx, h.Theme(), 15, 22, theme.Regular, colorsOf(h.Theme()).Text, body)
			}
			hh := tv.layout(gtx, gtx.Constraints.Max.X, colorsOf(h.Theme()).Text)
			return D{Size: image.Pt(gtx.Constraints.Max.X, hh)}
		},
		Buttons: btns,
		// AttachmentActionConfirmDialog is a DeprecatedBaseDialog with no
		// focusOnPrimaryButton: react-modal focuses the dialog itself, so
		// Enter and Space do nothing until Tab reaches a button (the close
		// button first, from the header)
		CloseFirst: true,
		OnClose: func() {
			if !decided && deny != nil {
				deny()
			}
		},
	})
	if modalShown != nil {
		modalShown(m, btns)
	}
	h.ShowOverlay(m)
}

// modalShown, when set (tests), sees each dialog blockkit opens with its
// buttons.
var modalShown func(m popups.Overlay, buttons []*popups.ModalButton)

// ---- select dropdown ----

// item is one row of a select dropdown.
type item struct {
	opt    *Option // static / external
	id     string  // users / conversations / channels
	label  string
	sub    string // secondary text (real name, member count)
	avatar string
	header bool // option group label
}

// selectMenu is Slack's select dropdown: the field turns into a search
// box (focus ring) and the options list opens under it.
type selectMenu struct {
	w      *ew
	anchor image.Rectangle // field rect, window coords
	ed     widget.Editor
	items  []item
	rows   []tap
	list   layout.List
	sel    int
	kbd    bool
	query  string
	gen    int
	busy   bool
	errMsg string
	done   bool
	scrim  int
	focus  bool
	local  []item // conversations from the sidebar
	tag    int
}

func openSelect(w *ew, anchor image.Rectangle) {
	h := w.o.host()
	m := &selectMenu{w: w, anchor: anchor, sel: 0}
	m.ed.SingleLine = true
	m.list.Axis = layout.Vertical
	w.open = true
	w.o.changed()
	m.refresh()
	h.ShowOverlay(m)
}

func (m *selectMenu) Dismissed() bool { return m.done }

func (m *selectMenu) Dismiss() {
	if m.done {
		return
	}
	m.done = true
	m.w.open = false
	m.w.o.changed()
}

func (m *selectMenu) base() string { return strings.TrimPrefix(m.w.e.Type, "multi_") }

// minQuery is the characters an external select needs before asking.
func (m *selectMenu) minQuery() int {
	if m.w.e.MinQueryLength != nil {
		return *m.w.e.MinQueryLength
	}
	return 3
}

// refresh recomputes the rows for the current query.
func (m *selectMenu) refresh() {
	q := strings.ToLower(strings.TrimSpace(m.query))
	m.gen++
	gen := m.gen
	h := m.w.o.host()
	switch m.base() {
	case "static_select":
		m.items = m.items[:0]
		e := m.w.e
		add := func(os []Option) {
			for i := range os {
				o := &os[i]
				l := textOf(&o.Text)
				if q == "" || strings.Contains(strings.ToLower(l), q) {
					m.items = append(m.items, item{opt: o, label: l, sub: textOf(o.Description)})
				}
			}
		}
		if len(e.OptionGroups) > 0 {
			for gi := range e.OptionGroups {
				g := &e.OptionGroups[gi]
				n := len(m.items)
				m.items = append(m.items, item{header: true, label: textOf(&g.Label)})
				add(g.Options)
				if len(m.items) == n+1 {
					m.items = m.items[:n]
				}
			}
		} else {
			add(e.Options)
		}
	case "external_select":
		if len([]rune(m.query)) < m.minQuery() {
			m.items = nil
			m.busy = false
			return
		}
		m.busy = true
		b := m.w.o.base()
		e := m.w.e
		bid := m.w.bid
		query := m.query
		run(h, 10*time.Second, func(ctx context.Context) ([]item, error) {
			time.Sleep(200 * time.Millisecond) // Slack debounces typing
			if gen != m.gen {
				return nil, nil
			}
			opts, groups, err := h.Core().Interactive().Suggestions(ctx, b, e.ActionID, bid, query)
			if err != nil {
				return nil, err
			}
			return suggestionItems(opts, groups), nil
		}, func(its []item, err error) {
			if gen != m.gen {
				return
			}
			m.busy = false
			m.errMsg = ""
			if err != nil {
				m.errMsg = err.Error()
			}
			m.items = its
			m.sel = firstRow(m.items)
			h.Invalidate()
		})
		return
	case "users_select":
		// the member searcher allows an empty query (allowEmptyQuery,
		// includeSelf): opening the menu lists people right away
		m.busy = true
		run(h, 10*time.Second, func(ctx context.Context) ([]item, error) {
			var us []core.User
			if q == "" {
				us = directoryUsers(h.Core(), 50)
			} else {
				var err error
				if us, err = h.Core().SearchUsers(ctx, q, 20); err != nil {
					return nil, err
				}
			}
			var its []item
			for _, u := range us {
				if u.Deleted {
					continue
				}
				if f := m.w.e.Filter; f != nil && f.ExcludeBotUsers && u.IsBot {
					continue
				}
				name := u.Name
				if name == "" {
					name = u.RealName
				}
				its = append(its, item{id: u.ID, label: name, sub: u.RealName, avatar: u.Avatar})
			}
			return its, nil
		}, func(its []item, err error) {
			if gen != m.gen {
				return
			}
			m.busy = false
			m.items = its
			m.sel = firstRow(m.items)
		})
		return
	case "conversations_select", "channels_select":
		if m.local == nil {
			m.local = []item{}
			channelsOnly := m.base() == "channels_select"
			filter := m.w.e.Filter
			run(h, 10*time.Second, func(ctx context.Context) ([]item, error) {
				snap := h.Core().Snapshot()
				var its []item
				for _, c := range snap.Convs {
					if c.IsArchived || !convAllowed(c.Kind, channelsOnly, filter) {
						continue
					}
					label := "#" + c.Name
					if c.Kind == "im" {
						if u, ok := snap.Users[c.User]; ok {
							label = u.Name
						} else {
							label = c.Name
						}
					} else if c.Kind == "mpim" {
						label = c.Name
					}
					its = append(its, item{id: c.ID, label: label})
				}
				return its, nil
			}, func(its []item, err error) {
				m.local = its
				m.refresh()
			})
		}
		m.items = m.items[:0]
		for _, it := range m.local {
			if q == "" || strings.Contains(strings.ToLower(it.label), q) {
				m.items = append(m.items, it)
				if len(m.items) >= 50 {
					break
				}
			}
		}
		if q != "" {
			m.busy = true
			channelsOnly := m.base() == "channels_select"
			filter := m.w.e.Filter
			run(h, 10*time.Second, func(ctx context.Context) ([]item, error) {
				hs, err := h.Core().SearchChannels(ctx, q, 20)
				if err != nil {
					return nil, err
				}
				var its []item
				for _, c := range hs {
					if c.IsArchived || !convAllowed(c.Kind, channelsOnly, filter) {
						continue
					}
					its = append(its, item{id: c.ID, label: "#" + c.Name})
				}
				return its, nil
			}, func(its []item, err error) {
				if gen != m.gen {
					return
				}
				m.busy = false
				seen := map[string]bool{}
				for _, it := range m.items {
					seen[it.id] = true
				}
				for _, it := range its {
					if !seen[it.id] {
						m.items = append(m.items, it)
					}
				}
			})
		}
	}
	m.sel = firstRow(m.items)
}

func convAllowed(kind string, channelsOnly bool, f *ConvFilter) bool {
	if channelsOnly {
		return kind == "channel" || kind == "public" || kind == ""
	}
	if f == nil || len(f.Include) == 0 {
		return true
	}
	k := kind
	switch kind {
	case "channel":
		k = "public"
	}
	for _, inc := range f.Include {
		if inc == k {
			return true
		}
	}
	return false
}

func firstRow(its []item) int {
	for i, it := range its {
		if !it.header {
			return i
		}
	}
	return -1
}

func suggestionItems(opts, groups json.RawMessage) []item {
	var its []item
	if len(groups) > 0 {
		var gs []OptionGroup
		_ = json.Unmarshal(groups, &gs)
		for gi := range gs {
			its = append(its, item{header: true, label: textOf(&gs[gi].Label)})
			for i := range gs[gi].Options {
				o := gs[gi].Options[i]
				its = append(its, item{opt: &o, label: textOf(&o.Text), sub: textOf(o.Description)})
			}
		}
		return its
	}
	var os []Option
	_ = json.Unmarshal(opts, &os)
	for i := range os {
		o := os[i]
		its = append(its, item{opt: &o, label: textOf(&o.Text), sub: textOf(o.Description)})
	}
	return its
}

// selected reports whether row it is part of the current value.
func (m *selectMenu) selected(it *item) bool {
	v := m.w.value()
	switch m.w.e.Type {
	case "static_select", "external_select":
		return v.SelectedOption != nil && it.opt != nil && v.SelectedOption.Value == it.opt.Value
	case "multi_static_select", "multi_external_select":
		for _, o := range v.SelectedOptions {
			if it.opt != nil && o.Value == it.opt.Value {
				return true
			}
		}
	case "users_select":
		return v.SelectedUser == it.id
	case "conversations_select":
		return v.SelectedConv == it.id
	case "channels_select":
		return v.SelectedChan == it.id
	case "multi_users_select":
		return contains(v.SelectedUsers, it.id)
	case "multi_conversations_select":
		return contains(v.SelectedConvs, it.id)
	case "multi_channels_select":
		return contains(v.SelectedChans, it.id)
	}
	return false
}

func contains(s []string, x string) bool {
	for _, v := range s {
		if v == x {
			return true
		}
	}
	return false
}

// pick applies row i.
func (m *selectMenu) pick(i int) {
	if i < 0 || i >= len(m.items) || m.items[i].header {
		return
	}
	it := m.items[i]
	w := m.w
	prev := w.snapshot()
	v := w.value()
	multi := isMulti(w.e.Type)
	switch w.e.Type {
	case "static_select", "external_select":
		o := *it.opt
		v.SelectedOption = &o
	case "multi_static_select", "multi_external_select":
		// past max_selected_items is allowed: Slack flags it (the alert,
		// a disabled Go) instead of refusing the pick
		if m.selected(&it) {
			return
		}
		v.SelectedOptions = append(append([]Option(nil), v.SelectedOptions...), *it.opt)
	case "users_select":
		v.SelectedUser = it.id
	case "conversations_select":
		v.SelectedConv = it.id
	case "channels_select":
		v.SelectedChan = it.id
	case "multi_users_select":
		if !contains(v.SelectedUsers, it.id) {
			v.SelectedUsers = append(append([]string(nil), v.SelectedUsers...), it.id)
		}
	case "multi_conversations_select":
		if !contains(v.SelectedConvs, it.id) {
			v.SelectedConvs = append(append([]string(nil), v.SelectedConvs...), it.id)
		}
	case "multi_channels_select":
		if !contains(v.SelectedChans, it.id) {
			v.SelectedChans = append(append([]string(nil), v.SelectedChans...), it.id)
		}
	}
	if !multi {
		m.Dismiss()
	} else {
		m.ed.SetText("")
		m.query = ""
		m.refresh()
	}
	w.commit(prev)
}

const rowH = 36 // Slack's select rows (c-select_options_list__option): 36px, 15px text, padding 0 24

func (m *selectMenu) Layout(gtx C) D {
	drain()
	if m.done {
		return D{}
	}
	w := m.w
	h := w.o.host()
	th := h.Theme()
	cl := colorsOf(th)
	win := gtx.Constraints.Max
	gtx.Constraints.Min = image.Point{}

	// outside presses close
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &m.scrim, Kinds: pointer.Press})
		if !ok {
			break
		}
		if pe, ok := e.(pointer.Event); ok && pe.Kind == pointer.Press {
			m.Dismiss()
			return D{}
		}
	}
	func() {
		defer clip.Rect{Max: win}.Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &m.scrim)
	}()

	if !m.focus {
		m.focus = true
		gtx.Execute(key.FocusCmd{Tag: &m.ed})
	}
	// keys
	for {
		e, ok := gtx.Event(
			key.Filter{Focus: &m.ed, Name: key.NameUpArrow}, key.Filter{Focus: &m.ed, Name: key.NameDownArrow},
			key.Filter{Focus: &m.ed, Name: key.NameReturn}, key.Filter{Focus: &m.ed, Name: key.NameEnter},
			key.Filter{Focus: &m.ed, Name: key.NameEscape},
		)
		if !ok {
			break
		}
		ke, ok := e.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		switch ke.Name {
		case key.NameEscape:
			m.Dismiss()
			return D{}
		case key.NameUpArrow:
			m.move(-1)
		case key.NameDownArrow:
			m.move(1)
		default:
			m.pick(m.sel)
			if m.done {
				return D{}
			}
		}
	}
	for {
		e, ok := m.ed.Update(gtx)
		if !ok {
			break
		}
		if _, ok := e.(widget.ChangeEvent); ok {
			m.query = m.ed.Text()
			m.refresh()
		}
	}

	a := m.anchor
	fw := max(a.Dx(), dp(gtx, 260))
	fh := max(a.Dy(), dp(gtx, 28))
	// the field becomes the search box
	at(gtx, a.Min, func(gtx C) D {
		r := image.Rect(0, 0, a.Dx(), fh)
		ring := dp(gtx, 4)
		rad := dp(gtx, 8)
		rrect(gtx, r.Inset(-ring), rad+ring, cl.FocusRing)
		outline(gtx, r, rad, max(1, dp(gtx, 1)), cl.Focus, cl.Bg)
		fs := float32(15)
		if fh < dp(gtx, 32) {
			fs = 13
		}
		pad := dp(gtx, 12)
		if fh < dp(gtx, 32) {
			pad = dp(gtx, 8)
		}
		at(gtx, image.Pt(pad, 0), func(gtx C) D {
			gtx.Constraints = layout.Constraints{Max: image.Pt(a.Dx()-2*pad-dp(gtx, 24), fh)}
			return vcenter(gtx, fh, func(gtx C) D {
				if m.ed.Len() == 0 {
					txt, ph := w.fieldText()
					if isMulti(w.e.Type) || ph {
						txt = textOf(w.e.Placeholder)
						if txt == "" {
							txt = "Search"
						}
					}
					label(gtx, th, fs, theme.Regular, cl.Placeholder, txt)
				}
				return th.Editor(gtx, &m.ed, unitSp(fs), cl.InputText, cl.FocusRing)
			})
		})
		cs := dp(gtx, 20)
		at(gtx, image.Pt(a.Dx()-pad-cs, (fh-cs)/2), func(gtx C) D { return icon(gtx, "chevron-up", 20, cl.InputText) })
		// the field area isn't "outside"
		func() {
			defer clip.Rect(r).Push(gtx.Ops).Pop()
			event.Op(gtx.Ops, &m.tag)
		}()
		return D{}
	})

	// options card
	n := len(m.items)
	rh := dp(gtx, rowH)
	if hasSubs(m.items) {
		rh = dp(gtx, 44)
	}
	padY := dp(gtx, 12)
	visible := min(n, 8)
	ch := visible*rh + 2*padY
	empty := n == 0
	if empty {
		ch = dp(gtx, 44) + 2*padY
	}
	pos := image.Pt(a.Min.X, a.Min.Y+fh+dp(gtx, 4))
	if pos.Y+ch > win.Y-dp(gtx, 8) && a.Min.Y-ch-dp(gtx, 4) > dp(gtx, 8) {
		pos.Y = a.Min.Y - ch - dp(gtx, 4)
	}
	if pos.X+fw > win.X-dp(gtx, 8) {
		pos.X = max(dp(gtx, 8), win.X-dp(gtx, 8)-fw)
	}
	if len(m.rows) < n {
		m.rows = append(m.rows, make([]tap, n-len(m.rows))...)
	}
	for i := 0; i < n; i++ {
		if m.rows[i].clicked(gtx, h) {
			m.pick(i)
			if m.done {
				return D{}
			}
		}
	}
	at(gtx, pos, func(gtx C) D {
		r := image.Rect(0, 0, fw, ch)
		theme.Shadow(gtx, r, dp(gtx, 8), dp(gtx, 12), cl.Shadow)
		rrect(gtx, r.Inset(-1), dp(gtx, 8)+1, cl.Ring)
		rrect(gtx, r, dp(gtx, 8), cl.Bg)
		defer clip.UniformRRect(r, dp(gtx, 8)).Push(gtx.Ops).Pop()
		func() {
			defer clip.Rect(r).Push(gtx.Ops).Pop()
			event.Op(gtx.Ops, &m.tag)
		}()
		if empty {
			msg := m.emptyMessage()
			at(gtx, image.Pt(dp(gtx, 24), padY), func(gtx C) D {
				gtx.Constraints = layout.Constraints{Max: image.Pt(fw-dp(gtx, 48), dp(gtx, 44))}
				return vcenter(gtx, dp(gtx, 44), func(gtx C) D { return label(gtx, th, 15, theme.Regular, cl.Text2, msg) })
			})
			return D{Size: r.Max}
		}
		at(gtx, image.Pt(0, padY), func(gtx C) D {
			gtx.Constraints = layout.Exact(image.Pt(fw, visible*rh))
			return m.list.Layout(gtx, n, func(gtx C, i int) D {
				it := &m.items[i]
				sz := image.Pt(fw, rh)
				if it.header {
					at(gtx, image.Pt(dp(gtx, 24), 0), func(gtx C) D {
						return vcenter(gtx, rh, func(gtx C) D { return label(gtx, th, 13, theme.Bold, cl.Text2, it.label) })
					})
					return D{Size: sz}
				}
				row := &m.rows[i]
				hot := i == m.sel && m.kbd || row.g.Hovered()
				if row.g.Hovered() && !m.kbd {
					m.sel = i
				}
				fg, sub := cl.Text, cl.Text2
				if hot {
					rrect(gtx, image.Rectangle{Max: sz}, 0, cl.Highlight)
					fg, sub = cl.HighText, cl.HighText
				}
				x := dp(gtx, 24)
				if it.avatar != "" {
					as := dp(gtx, 20)
					if img, ok := h.Images().Get(it.avatar, as); ok {
						at(gtx, image.Pt(x, (rh-as)/2), func(gtx C) D { return theme.Image(gtx, img, image.Pt(as, as), dp(gtx, 4)) })
					}
					x += as + dp(gtx, 8)
				}
				right := fw - dp(gtx, 24)
				if m.selected(it) {
					right -= dp(gtx, 20)
					at(gtx, image.Pt(right, (rh-dp(gtx, 16))/2), func(gtx C) D { return icon(gtx, "check", 16, fg) })
					right -= dp(gtx, 8)
				}
				at(gtx, image.Pt(x, 0), func(gtx C) D {
					gtx.Constraints = layout.Constraints{Max: image.Pt(max(right-x, 0), rh)}
					if it.sub != "" && rh > dp(gtx, rowH) {
						at(gtx, image.Pt(0, dp(gtx, 3)), func(gtx C) D { return label(gtx, th, 15, theme.Regular, fg, it.label) })
						at(gtx, image.Pt(0, dp(gtx, 23)), func(gtx C) D { return label(gtx, th, 13, theme.Regular, sub, it.sub) })
						return D{}
					}
					return vcenter(gtx, rh, func(gtx C) D {
						d := label(gtx, th, 15, theme.Regular, fg, it.label)
						if it.sub != "" && it.sub != it.label {
							at(gtx, image.Pt(d.Size.X+dp(gtx, 8), 0), func(gtx C) D {
								gtx.Constraints.Max.X = max(0, gtx.Constraints.Max.X-d.Size.X-dp(gtx, 8))
								return label(gtx, th, 13, theme.Regular, sub, it.sub)
							})
						}
						return d
					})
				})
				row.add(gtx, sz)
				return D{Size: sz}
			})
		})
		return D{Size: r.Max}
	})
	return D{Size: win}
}

// emptyMessage is the element's renderEmptyStateMessage (searched: a
// query was typed).
func (m *selectMenu) emptyMessage() string {
	searched := strings.TrimSpace(m.query) != ""
	pick := func(yes, no string) string {
		if searched {
			return yes
		}
		return no
	}
	switch {
	case m.busy:
		return "Loading…"
	case m.errMsg != "":
		return m.errMsg
	}
	switch m.base() {
	case "external_select":
		if n := m.minQuery(); n > 0 && len([]rune(m.query)) < n {
			return "Type a minimum of " + itoa(n) + " characters to see options."
		}
		return pick("😕 Nothing could be found.", "😕 No result")
	case "users_select":
		return pick("😕 No member matches these keywords.", "😕 No member found")
	case "conversations_select":
		return pick("😕 No conversation matches these keywords.", "😕 No conversation found")
	case "channels_select":
		return pick("😕 No channel matches these keywords.", "😕 No channel found")
	}
	return pick("😕 Nothing could be found.", "😕 No result")
}

// directoryUsers is the people list of an empty users_select query:
// the cached directory, self included, by name.
func directoryUsers(c *core.App, n int) []core.User {
	snap := c.Snapshot()
	var out []core.User
	for _, u := range snap.Users {
		if u.Deleted {
			continue
		}
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(firstNonEmpty(out[i].Name, out[i].RealName)), strings.ToLower(firstNonEmpty(out[j].Name, out[j].RealName))
		if a != b {
			return a < b
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func hasSubs(its []item) bool {
	for _, it := range its {
		if it.opt != nil && it.sub != "" {
			return true
		}
	}
	return false
}

func (m *selectMenu) move(d int) {
	m.kbd = true
	n := len(m.items)
	if n == 0 {
		return
	}
	i := m.sel
	for k := 0; k < n; k++ {
		i = (i + d + n) % n
		if !m.items[i].header {
			m.sel = i
			break
		}
	}
	if m.sel < m.list.Position.First {
		m.list.Position.First = m.sel
		m.list.Position.Offset = 0
	} else if m.sel >= m.list.Position.First+8 {
		m.list.Position.First = m.sel - 7
		m.list.Position.Offset = 0
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// sortItems orders rows by label (conversation lists).
func sortItems(its []item) {
	sort.SliceStable(its, func(i, j int) bool { return strings.ToLower(its[i].label) < strings.ToLower(its[j].label) })
}
