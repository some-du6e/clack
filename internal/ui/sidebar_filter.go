package ui

import (
	"context"
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The sidebar text filter (sidebar_text_filter and its m2/m3 milestones;
// the rules and the evidence are in core/sidebar_filter.go). This file is
// Slack's SidebarTextFilterInput (client-channel-list-view SYcU) with its
// FilterInput (boot-shared onPg FilterInputBase), the provider's debounce
// (boot-render Yuqx), the filtered rows (B7kx) and
// TextFilterNoResultsRowElement (JS+i).

// sbFilterDebounce is SidebarTextFilterProvider's setTimeout.
const sbFilterDebounce = 150 * time.Millisecond

type sbTextFilter struct {
	ed        widget.Editor
	text      string    // filterText: what is typed
	debounced string    // debouncedFilterText: what the rows follow
	due       time.Time // when text becomes debounced
	reset     widget.Clickable
	search    widget.Clickable // the no-results row's "Search {team}"
	focused   bool
	fetching  bool // sidebar.dms in flight (createThunkFetchSidebarDms)
	inited    bool
	darkTh    *theme.Theme // the inverted sidebar's base tokens (the dark ramp), for Slack's Button
	darkGen   uint64
}

// sbFilterOn is isSidebarTextFilterEnabled (O2By ZN).
func (a *App) sbFilterOn() bool {
	c := a.Core()
	return c != nil && c.SidebarTextFilterOn()
}

// sbFilterKey is what the rows were filtered with: the debounced text
// (trimmed; "" while the experiment is off) and the milestones.
func (a *App) sbFilterKey() (q string, m2, m3 bool) {
	c := a.Core()
	if c == nil || !c.SidebarTextFilterOn() {
		return "", false, false
	}
	return strings.TrimSpace(a.sidebar.filter.debounced), c.SidebarTextFilterM2(), c.SidebarTextFilterM3()
}

// setText is setFilterText: the input follows at once, the rows 150 ms
// later.
func (f *sbTextFilter) setText(t string, now time.Time) {
	f.text = t
	f.due = now.Add(sbFilterDebounce)
}

// clear is the provider's clear: both texts empty at once.
func (f *sbTextFilter) clear() {
	f.text, f.debounced, f.due = "", "", time.Time{}
	if f.ed.Text() != "" {
		f.ed.SetText("")
	}
}

// ResetSidebarTextFilter is Slack's "sidebar-text-filter-reset" event (Yuqx
// FA): picking a conversation in the quick switcher and clicking Home on
// Home clear the filter.
func (a *App) ResetSidebarTextFilter() {
	if a.sidebar.filter.text == "" && a.sidebar.filter.debounced == "" {
		return
	}
	a.sidebar.filter.clear()
	a.Invalidate()
}

// tickSidebarFilter moves the typed text to the rows once the debounce has
// run out, and asks for a frame then.
func (a *App) tickSidebarFilter(gtx layout.Context) {
	f := &a.sidebar.filter
	if f.due.IsZero() {
		return
	}
	if !gtx.Now.Before(f.due) {
		f.debounced, f.due = f.text, time.Time{}
		return
	}
	gtx.Execute(op.InvalidateCmd{At: f.due})
}

// layoutSidebarFilter draws SidebarTextFilterInput in the header slot:
// .c-filter_input.c-filter_input--themed.c-filter_input--small (28px,
// radius 8, 1px theme-otl-inv-pry on theme-surf-inv-sec; padding 0 4px),
// the quick-switch icon at 16px, the 13px input ("Find a conversation…",
// aria-label "Channel or user name"), and the right icons slot (20px)
// with the close-circle-filled reset button while there is text. Focused
// (--active), the border goes and the focus ring shows.
func (a *App) layoutSidebarFilter(gtx layout.Context, sz image.Point) layout.Dimensions {
	f := &a.sidebar.filter
	c := &a.sbPal
	if !f.inited {
		f.ed.SingleLine = true
		f.inited = true
	}
	// FilterInputBase: Escape empties the input and the row text (the
	// keydown handler's clear), so it doesn't reach "Close / mark as read";
	// SidebarTextFilterInput's ArrowDown focuses the first row
	// (SIDEBAR_FIRST_VISIBLE_ROW, sidebar_keys.go)
	shortcut.OwnEscape(&f.ed)
	for {
		ev, ok := gtx.Event(key.Filter{Focus: &f.ed, Name: key.NameEscape}, key.Filter{Focus: &f.ed, Name: key.NameDownArrow})
		if !ok {
			break
		}
		if k, ok := ev.(key.Event); ok && k.State == key.Press {
			if k.Name == key.NameDownArrow {
				a.sbFocusFirstRow(gtx)
				continue
			}
			f.clear()
			gtx.Execute(op.InvalidateCmd{}) // the rows follow on the next frame
		}
	}
	for {
		ev, ok := f.ed.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.ChangeEvent); ok {
			if t := f.ed.Text(); t != f.text {
				f.setText(t, gtx.Now)
				gtx.Execute(op.InvalidateCmd{At: f.due}) // the debounce's frame, typing or not
			}
		}
	}
	focused := gtx.Focused(&f.ed)
	if focused && !f.focused {
		a.sidebarFilterFocused()
	}
	f.focused = focused
	if f.reset.Clicked(gtx) { // handleResetButtonClick + onReset: clear, keep the focus
		f.clear()
		gtx.Execute(key.FocusCmd{Tag: &f.ed})
		gtx.Execute(op.InvalidateCmd{})
	}

	r := image.Rectangle{Max: sz}
	rad := gtx.Dp(8)
	lilac := color.NRGBA{R: 0xf9, G: 0xed, B: 0xff} // theme-surf-inv-sec / theme-otl-inv-pry (jmNE)
	bg := theme.Over(theme.WithAlpha(lilac, 0x1c), a.sbBase)
	if a.th.Dark {
		bg = a.sbBase // dark: transparent box, outline only
	}
	if focused {
		// .c-filter_input--active: box-shadow 0 0 0 1px
		// --sk_focused-shadow-color, 0 0 0 5px of it at 30%; the border is
		// transparent
		ring := a.sbFocusColor()
		hw := gtx.Dp(5)
		theme.RRect(gtx, r.Inset(-hw), rad+hw, theme.Over(theme.WithAlpha(ring, 0x4d), a.sbBase))
		theme.RRect(gtx, r.Inset(-max(1, gtx.Dp(1))), rad+gtx.Dp(1), ring)
		theme.RRect(gtx, r, rad, bg)
	} else {
		theme.RRect(gtx, r, rad, bg)
		theme.Border(gtx, r, rad, max(1, gtx.Dp(1)), theme.Over(theme.WithAlpha(lilac, 0x38), a.sbBase))
	}
	// the box focuses the input (handleClick); the editor, laid out on
	// top, takes the presses over the text itself
	area := clip.Rect(r).Push(gtx.Ops)
	pointer.CursorText.Add(gtx.Ops)
	event.Op(gtx.Ops, f)
	area.Pop()
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: f, Kinds: pointer.Press})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok && e.Kind == pointer.Press && !focused {
			gtx.Execute(key.FocusCmd{Tag: &f.ed})
		}
	}
	pad := max(1, gtx.Dp(1)) + gtx.Dp(4)
	is := gtx.Dp(16)
	at(gtx, image.Pt(pad, (sz.Y-is)/2), image.Pt(is, is), func(gtx layout.Context) layout.Dimensions {
		return icons.Get("find")(gtx, 16, c.SidebarTextUnread)
	})
	tx := pad + is + gtx.Dp(4)
	slot := gtx.Dp(20)
	tw := sz.X - tx - gtx.Dp(4) - pad - slot
	at(gtx, image.Pt(tx, 0), image.Pt(tw, sz.Y), func(gtx layout.Context) layout.Dimensions {
		if f.ed.Len() == 0 { // the placeholder: theme-content-inv-sec
			vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
				return a.th.Label(gtx, 13, theme.Regular, c.SidebarText, i18n.T("channel_sidebar", "Find a conversation…"))
			})
		}
		return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return a.th.Editor(gtx, &f.ed, 13, c.SidebarTextUnread, theme.WithAlpha(a.th.C.Accent, 0x66))
		})
	})
	if f.text != "" { // c-filter_input__reset_button--visible: "Clear text"
		bs := gtx.Dp(16)
		at(gtx, image.Pt(sz.X-pad-slot+(slot-bs)/2, (sz.Y-bs)/2), image.Pt(bs, bs), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, &f.reset, nil, func(gtx layout.Context) layout.Dimensions {
				return icons.Get("close-circle-filled")(gtx, 16, c.SidebarTextUnread)
			})
		})
	}
	return layout.Dimensions{Size: sz}
}

// sidebarFilterFocused is the input's onFocus: with m2 on and sidebar.dms
// not loaded yet, load it (createThunkFetchSidebarDms, reason
// "sidebar-text-filter-dms"), so closed DMs can match.
func (a *App) sidebarFilterFocused() {
	c := a.Core()
	f := &a.sidebar.filter
	if c == nil || f.fetching || !c.SidebarTextFilterM2() || c.SidebarDMsKnown() {
		return
	}
	f.fetching = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = c.FetchSidebarDMs(ctx) // a failure only logs in Slack; the next focus tries again
		a.Post(func() { a.sidebar.filter.fetching = false })
	}()
}

// sbFilterSections are the model's sections as the filter reads them:
// every conversation each holds before its own filter.
func sbFilterSections(m *model) []core.FilterSection {
	out := make([]core.FilterSection, 0, len(m.Sections))
	for i := range m.Sections {
		s := &m.Sections[i]
		rows := s.All
		if len(rows) == 0 {
			rows = s.Rows
		}
		out = append(out, core.FilterSection{ID: s.ID, Type: s.Type, Rows: rows})
	}
	return out
}

// rebuildFiltered is getChannelListRows while filtering: no page links or
// their divider, no footers; each matching section's heading (shown
// expanded) and its matches, without "Add channels" and the like
// (shouldShowAddMoreLinks false); nothing matching is the one
// TextFilterNoResultsRow.
func (s *sidebarState) rebuildFiltered(m *model, rows []sbRow) []sbRow {
	res := core.FilterSidebar(sbFilterSections(m), s.fq, s.fm2, s.fm3, m.FilterNames)
	byID := make(map[string]*sbSection, len(m.Sections))
	for i := range m.Sections {
		byID[m.Sections[i].ID] = &m.Sections[i]
	}
	n := 0
	for _, fs := range res {
		sec := byID[fs.ID]
		if sec == nil {
			continue
		}
		var convs []string
		for _, id := range fs.Rows {
			if m.conv(id) != nil {
				convs = append(convs, id)
			}
		}
		if len(convs) == 0 {
			continue
		}
		if n > 0 {
			rows = append(rows, sbRow{kind: sbGap, gap: 10})
		}
		n++
		hr := sbRow{kind: sbHeading, key: sec.ID, label: sec.Name, icon: sectionIcon(sec.Type), emoji: sec.EmojiURL, section: sec.ID, open: true}
		if sec.DefaultName {
			hr.tr = "channel_sidebar"
		}
		rows = append(rows, hr)
		for _, id := range convs {
			rows = append(rows, sbRow{kind: sbConv, key: id, section: sec.ID})
		}
	}
	if n == 0 {
		rows = append(rows, sbRow{kind: sbNoResults, key: "textFilterNoResults"})
	}
	return rows
}

// layoutSidebarNoResults is TextFilterNoResultsRowElement in its 130px row
// (.container__QSsd8: a centred column, gap 16, padding 32): "No
// conversations to show…" (15/1.5, theme-content-inv-sec) and an outline
// small "Search {teamName}" that hands the text to search
// (openMethod sidebar_filter_handoff, commitSearch) and clears the filter.
func (a *App) layoutSidebarNoResults(gtx layout.Context, w int) layout.Dimensions {
	f := &a.sidebar.filter
	c := &a.sbPal
	h := gtx.Dp(130)
	if f.search.Clicked(gtx) {
		q := f.debounced
		f.clear()
		popups.CommitSearch(a, q)
		gtx.Execute(op.InvalidateCmd{})
	}
	txt := i18n.T("channel_sidebar", "No conversations to show…")
	lh := gtx.Dp(23) // 15px at line-height 1.5
	bh := gtx.Dp(28)
	gap := gtx.Dp(16)
	y := (h - lh - gap - bh) / 2
	tw := a.th.Measure(gtx, 15, theme.Regular, txt)
	at(gtx, image.Pt(max(0, (w-tw)/2), y), image.Pt(min(tw, w), lh), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 15, theme.Regular, c.SidebarText, txt)
		})
	})
	label := i18n.T("channel_sidebar", "Search {teamName}", "teamName", a.model().TeamName)
	th := a.sbDarkTheme()
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(w-gtx.Dp(64), bh)}
	d := popups.Button(g, th, &f.search, popups.BtnOutline, popups.BtnSmall, label, "", 0)
	call := m.Stop()
	t := op.Offset(image.Pt((w-d.Size.X)/2, y+lh+gap)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// sbDarkTheme is the theme Slack's components in the sidebar resolve
// their base tokens in: the inverted sidebar takes the dark ramp
// (--sk_foreground_low and the rest, as sbDivider notes).
func (a *App) sbDarkTheme() *theme.Theme {
	f := &a.sidebar.filter
	if f.darkTh == nil || f.darkGen != a.th.Gen || f.darkTh.Shaper != a.th.Shaper {
		t := &theme.Theme{Dark: true, Chrome: true, C: theme.DarkPalette, Shaper: a.th.Shaper, Images: a.th.Images, EmojiStyle: a.th.EmojiStyle}
		f.darkTh, f.darkGen = t, a.th.Gen
	}
	return f.darkTh
}
