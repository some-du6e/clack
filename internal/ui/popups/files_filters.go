package popups

// The Files browser's Filters modal: useUnifiedFilesFilters'
// showAdvancedFilterModal (the hosted-browser chunk's jnm4 module te+0,
// CDN build 8f19be21ed7e) opens the connected BaseAdvancedSearchModal
// with entity "files", filtersExcluded [file type, with] (and from under
// "Created by you"), hideFilterCount, excludeFileTypesFromFilterCount,
// disableClearBasedOnFilterCount and searchButtonText "Done".
//
//   - c-sk-modal (520 wide, centred): the title "Filter by", then one
//     ModalContentSection (0 28px) of Labels (c-label--block: the 15px
//     bold text, 8px under it, then the control): From and In
//     (AdvancedSearchSelect, qrHQ: ControlledMultiSelect with tokens,
//     medium, maxHeight 130, 32px option rows, the Searcher over
//     members, and channels, group DMs and members), Date
//     (SearchFilterDateRangeSelect, CniK, behind an outline medium button
//     with the range's label and caret-down). Each control has
//     .c-advanced_search_modal__select's 12px margin under it.
//   - The footer (84px, padding 24 28): "ⓘ Learn more about search"
//     (HelpLink, article 202528808) at the left; Clear Filters (outline,
//     off while nothing counts: disableClearBasedOnFilterCount) and Done
//     (primary, off until something changed since the modal opened: the
//     ax state an effect's cleanup sets).
//   - The count (aI) is the filters set plus the search_exclude_bots pref
//     (mapStateToProps' excludeBots, a modal state Clear Filters turns
//     off); Done hands it to the browser as the Filters button's count
//     (setUnifiedFilesFilterCount).
//   - The Date menu (280 wide, radio rows): Any time (before: tomorrow,
//     no after), Today, Yesterday, Last 7 days, Last 30 days, Last 3
//     months, Last 12 months, Last 3 years (getDatePresetStringValuesMap,
//     fhw7), the range's own label when no preset is it, a divider, then
//     On…, Before…, After… and Range…. Slack asks those in a small dialog
//     (a date input with its calendar, or the range picker); Clack picks
//     them on its calendar (NewDatePickerRange), Range… as two picks.

import (
	"context"
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/acrank"
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// FilesFilters are the Files browser's advanced filters (the browser
// instance's from, in, after and before): people's and conversations'
// ids, and exclusive YYYY-MM-DD dates ("" for none).
type FilesFilters struct {
	From, In      []string
	After, Before string
}

// Set reports whether any filter is set (hasAdvancedFiltersSet: the
// filters differ from the browser's initial ones, which have none).
func (f FilesFilters) Set() bool {
	return len(f.From) > 0 || len(f.In) > 0 || f.After != "" || f.Before != ""
}

// Count is countAdvancedFilters: from, in and the date range count one
// each.
func (f FilesFilters) Count() int {
	n := 0
	for _, on := range []bool{len(f.From) > 0, len(f.In) > 0, f.After != "" || f.Before != ""} {
		if on {
			n++
		}
	}
	return n
}

// FilesFiltersOpts configure the modal.
type FilesFiltersOpts struct {
	// HideFrom leaves From out ("Created by you": filtersExcluded lo).
	HideFrom bool
	// OnSave gets the filters and the modal's count when Done is pressed.
	OnSave func(f FilesFilters, count int)
}

// filesHelpURL is the modal's "Learn more about search" (HelpLink
// articleId 0xc125828).
const filesHelpURL = "https://slack.com/help/articles/202528808"

// FilesFiltersModal is the Files browser's Filters modal.
type FilesFiltersModal struct {
	h           Host
	o           FilesFiltersOpts
	modal       *Modal
	rk          *srACRanker
	from, in    *ugSelect
	after       string
	before      string
	excludeBots bool // li.excludeBots: the search_exclude_bots pref until Clear Filters
	touched     bool // a change since the modal opened (Done's ax)
	dateBtn     widget.Clickable
	dateR       image.Rectangle // the Date trigger in window coordinates, last frame
	help        widget.Clickable
	clear, done *ModalButton
	// the remote tier's answers, by query
	users map[string]map[string]core.User
	chans map[string][]core.ChannelHit
	now   func() time.Time
}

// OpenFilesFilters opens the Filters modal over the browser with its
// current filters.
func OpenFilesFilters(h Host, cur FilesFilters, o FilesFiltersOpts) *FilesFiltersModal {
	m := &FilesFiltersModal{h: h, o: o, after: cur.After, before: cur.Before, users: map[string]map[string]core.User{},
		chans: map[string][]core.ChannelHit{}, now: time.Now}
	c := h.Core()
	if c != nil {
		m.excludeBots = c.PrefBool("search_exclude_bots", false)
	}
	m.rk = newSRACRanker(h)
	m.from = newUGSelect(h, false, nsAdvSearch.T("ex. Zoe Maxwell"))
	m.from.member, m.from.allowEmpty, m.from.maxH = true, true, 130
	m.from.rank = func(q string) []ugOption { return m.rankFrom(q) }
	m.from.fetch = func(ctx context.Context, q string) (func(), error) { return m.fetch(ctx, q, false) }
	m.in = newUGSelect(h, false, nsAdvSearch.T("ex. #project-unicorn"))
	m.in.allowEmpty, m.in.maxH = true, 130
	m.in.rank = func(q string) []ugOption { return m.rankIn(q) }
	m.in.fetch = func(ctx context.Context, q string) (func(), error) { return m.fetch(ctx, q, true) }
	var from, in []ugOption
	for _, id := range cur.From {
		from = append(from, ffOption(h, id))
	}
	for _, id := range cur.In {
		in = append(in, ffOption(h, id))
	}
	m.from.SetPicked(from)
	m.in.SetPicked(in)
	m.from.changed = func([]string) { m.touched = true }
	m.in.changed = func([]string) { m.touched = true }
	m.from.query("")
	m.in.query("")
	m.clear = &ModalButton{Label: nsAdvSearch.T("Clear Filters"), Kind: BtnOutline,
		Enabled: func() bool { return m.Count() > 0 }, // disableClearBasedOnFilterCount
		Do:      func() bool { m.Clear(); return false }}
	m.done = &ModalButton{Label: nsBrowsers.T("Done"), Kind: BtnPrimary,
		Enabled: func() bool { return m.touched },
		Do:      func() bool { m.Save(); return true }}
	m.modal = NewModal(h, ModalOpts{
		Title:      nsAdvSearch.T("Filter by"),
		Body:       m.layout,
		Buttons:    []*ModalButton{m.clear, m.done},
		FooterPad:  24, // c-sk-modal_footer: 84px, padding 24px 28px
		FooterLeft: m.helpLink,
		// an open From or In list takes Escape first (onEscape)
		OnEscape: ugEscape(m.from, m.in),
		FocusStops: func() []event.Tag {
			stops := []event.Tag{&m.in.ed, &m.dateBtn, &m.help}
			if !o.HideFrom {
				stops = append([]event.Tag{&m.from.ed}, stops...)
			}
			return stops
		},
	})
	h.ShowOverlay(m.modal)
	return m
}

// ffOption is a picked id as an option: a person or a conversation.
// A group DM is named as its option row was (the Searcher's
// getDisplayNameForChannel: the custom name, else its members), not by its
// raw mpdm- name.
func ffOption(h Host, id string) ugOption {
	c := h.Core()
	if strings.HasPrefix(id, "U") || strings.HasPrefix(id, "W") {
		if c != nil {
			if u, ok := c.User(id); ok {
				return ugMemberOption(u)
			}
		}
		return ugOption{ID: id, Label: id, User: true}
	}
	if c != nil {
		if cv, ok := c.Conv(id); ok && cv.Kind == "mpim" {
			name := convTitle(h, cv)
			return ugOption{ID: id, Label: name, Conv: core.Conv{ID: id, Name: name, Kind: "mpim"}}
		}
	}
	return ugChannelOption(c, id)
}

// Modal is the modal overlay (tests).
func (m *FilesFiltersModal) Modal() *Modal { return m.modal }

// From and In are the selects (tests).
func (m *FilesFiltersModal) From() *ugSelect { return m.from }
func (m *FilesFiltersModal) In() *ugSelect   { return m.in }

// Filters are the modal's filters now. From goes with them even while
// the modal hides it (onSave passes fromSelections whatever
// filtersExcluded says): a From set under All files stays under Created
// by you.
func (m *FilesFiltersModal) Filters() FilesFilters {
	f := FilesFilters{From: m.from.IDs(), In: m.in.IDs(), After: m.after, Before: m.before}
	if len(f.From) == 0 {
		f.From = nil
	}
	if len(f.In) == 0 {
		f.In = nil
	}
	return f
}

// Count is the modal's filter count (aI with hideFilterCount and
// excludeFileTypesFromFilterCount): the excludeBots state, From, In and
// the date range.
func (m *FilesFiltersModal) Count() int {
	n := FilesFilters{From: m.from.IDs(), In: m.in.IDs(), After: m.after, Before: m.before}.Count()
	if m.excludeBots {
		n++
	}
	return n
}

// Clear is Clear Filters (a2): every filter of the modal off; nothing is
// searched until Done.
func (m *FilesFiltersModal) Clear() {
	m.from.SetPicked(nil)
	m.in.SetPicked(nil)
	m.after, m.before, m.excludeBots = "", "", false
	m.touched = true
	m.h.Invalidate()
}

// Save is Done (onSave): the filters and the count go to the browser.
func (m *FilesFiltersModal) Save() {
	if m.o.OnSave != nil {
		m.o.OnSave(m.Filters(), m.Count())
	}
}

// SetDates sets the date range (the date menu's onChange: before and
// after as getDatePresetStringValuesMap writes them).
func (m *FilesFiltersModal) SetDates(after, before string) {
	if after != m.after || before != m.before {
		m.touched = true
	}
	m.after, m.before = after, before
	m.h.Invalidate()
}

// ---- the selects' Searcher ----

// fetch asks edge users/search (and channels/search for In) for q once,
// as the search box's completer does.
func (m *FilesFiltersModal) fetch(ctx context.Context, q string, chans bool) (func(), error) {
	c := m.h.Core()
	sq := acrank.SearcherQuery(q)
	if c == nil || strings.TrimSpace(sq) == "" {
		return nil, nil
	}
	us, err := c.AutocompleteUsers(ctx, sq, "")
	if err != nil {
		return nil, err
	}
	var hs []core.ChannelHit
	if chans {
		if hs, err = c.AutocompleteChannels(ctx, sq); err != nil {
			hs = nil
		}
	}
	return func() {
		hits := make(map[string]core.User, len(us))
		for _, u := range us {
			hits[u.ID] = u
		}
		m.users[q] = hits
		if chans {
			m.chans[q] = hs
		}
	}, nil
}

// rankFrom is From's searcherOptions: members (self, bots and deleted
// people included; no Slackbot, no workflows).
func (m *FilesFiltersModal) rankFrom(q string) []ugOption {
	if m.rk == nil {
		return nil
	}
	return m.options(m.rk.rank(q, m.users[q], true, false, nil))
}

// rankIn is In's: channels (public, private, group DMs; not archived),
// channel sections (none show: no includeTypes) and members.
func (m *FilesFiltersModal) rankIn(q string) []ugOption {
	if m.rk == nil {
		return nil
	}
	its := m.rk.rank(q, m.users[q], true, true, m.chans[q])
	return m.options(append(its, m.rk.mpims(q)...))
}

func (m *FilesFiltersModal) options(its []srACItem) []ugOption {
	out := make([]ugOption, 0, len(its))
	for _, it := range its {
		switch it.kind {
		case srAIMember:
			if it.id == "USLACKBOT" { // includeSlackbot: false
				continue
			}
			out = append(out, ugOption{ID: it.id, Label: it.label, Sub: it.alt, Avatar: it.avatar, User: true})
		case srAIChannel:
			kind := "channel"
			if it.private {
				kind = "private"
			}
			out = append(out, ugOption{ID: it.id, Label: it.label, Conv: core.Conv{ID: it.id, Name: it.label, Kind: kind}})
		case srAIMpim:
			out = append(out, ugOption{ID: it.id, Label: it.label, Conv: core.Conv{ID: it.id, Name: it.label, Kind: "mpim"}})
		}
	}
	return out
}

// ---- the date ----

// ffDay is a day as the date filters write it (YYYY-MM-DD).
func ffDay(t time.Time) string { return t.Format("2006-01-02") }

// ffAddMonths is dayjs's add(n, "months"): the day kept, or the month's
// last when it has fewer days.
func ffAddMonths(t time.Time, n int) time.Time {
	y, mo, d := t.Date()
	first := time.Date(y, mo+time.Month(n), 1, 0, 0, 0, 0, t.Location())
	last := first.AddDate(0, 1, -1).Day()
	return time.Date(first.Year(), first.Month(), min(d, last), 0, 0, 0, 0, t.Location())
}

// ffPreset is one of getDatePresetStringValuesMap's ranges.
type ffPreset struct{ label, after, before string }

// ffPresets are the date menu's presets for now (fhw7).
func ffPresets(now time.Time) []ffPreset {
	tomorrow := ffDay(now.AddDate(0, 0, 1))
	return []ffPreset{
		{"Today", ffDay(now.AddDate(0, 0, -1)), tomorrow},
		{"Yesterday", ffDay(now.AddDate(0, 0, -2)), ffDay(now)},
		{"Last 7 days", ffDay(now.AddDate(0, 0, -7)), tomorrow},
		{"Last 30 days", ffDay(now.AddDate(0, 0, -30)), tomorrow},
		{"Last 3 months", ffDay(ffAddMonths(now, -3)), tomorrow},
		{"Last 12 months", ffDay(ffAddMonths(now, -12)), tomorrow},
		{"Last 3 years", ffDay(ffAddMonths(now, -36)), tomorrow},
	}
}

// ffAnyTime is isAnyTimeRange: no after, and no before or tomorrow's.
func ffAnyTime(after, before string, now time.Time) bool {
	return after == "" && (before == "" || before == ffDay(now.AddDate(0, 0, 1)))
}

// ffLast3Years is isLast3YearsRange: within two days of the preset at
// both ends.
func ffLast3Years(after, before string, now time.Time) bool {
	if after == "" || before == "" {
		return false
	}
	p := ffPresets(now)[6]
	near := func(a, b string) bool {
		x, e1 := time.ParseInLocation("2006-01-02", a, now.Location())
		y, e2 := time.ParseInLocation("2006-01-02", b, now.Location())
		if e1 != nil || e2 != nil {
			return false
		}
		d := x.Sub(y).Hours() / 24
		return d >= -2 && d <= 2
	}
	return near(before, p.before) && near(after, p.after)
}

// ffPresetOf is the date menu's selected preset (its effect's C): "Any
// time", a preset's label, or "" for none.
func ffPresetOf(after, before string, now time.Time) string {
	if ffAnyTime(after, before, now) {
		return "Any time"
	}
	ps := ffPresets(now)
	for _, p := range ps[:6] {
		if p.after == after && p.before == before {
			return p.label
		}
	}
	if ffLast3Years(after, before, now) {
		return ps[6].label
	}
	return ""
}

// ffDateShow is getDateDisplayValue: "Nov 3, 2026", or "Nov 3" without
// the year.
func ffDateShow(s string, year bool, loc *time.Location) string {
	t, err := time.ParseInLocation("2006-01-02", s, loc)
	if err != nil {
		return ""
	}
	if year {
		return t.Format("Jan 2, 2006")
	}
	return t.Format("Jan 2")
}

// FilesDateLabel is the Date trigger's label (+hyT S, "Any time" with no
// range): After …, Before … ("Any time" for tomorrow), Today, Yesterday,
// On … for one day, a preset's name, else "start - end" (the start's year
// when the years differ, the end's when it isn't this year).
func FilesDateLabel(after, before string, now time.Time) string {
	loc := now.Location()
	tomorrow := ffDay(now.AddDate(0, 0, 1))
	switch {
	case after == "" && before == "":
		return nsAdvSearch.T("Any time")
	case before == "":
		return nsSearch.T("After {dateString}", "dateString", ffDateShow(after, true, loc))
	case after == "":
		if before == tomorrow {
			return nsSearch.T("Any time")
		}
		return nsSearch.T("Before {dateString}", "dateString", ffDateShow(before, true, loc))
	}
	a, e1 := time.ParseInLocation("2006-01-02", after, loc)
	b, e2 := time.ParseInLocation("2006-01-02", before, loc)
	if e1 != nil || e2 != nil {
		return nsSearch.T("Invalid date")
	}
	first, last := a.AddDate(0, 0, 1), b.AddDate(0, 0, -1)
	if ffDay(first) == ffDay(last) {
		switch ffDay(last) {
		case ffDay(now):
			return nsSearch.T("Today")
		case ffDay(now.AddDate(0, 0, -1)):
			return nsSearch.T("Yesterday")
		}
		return nsSearch.T("On {dateString}", "dateString", ffDateShow(ffDay(last), true, loc))
	}
	ps := ffPresets(now)
	for _, p := range ps[2:6] {
		if p.after == after && p.before == before {
			return nsSearch.T(p.label)
		}
	}
	if ffLast3Years(after, before, now) {
		return nsSearch.T("Last 3 years")
	}
	return nsSearch.T("{startDate} - {endDate}", "startDate", ffDateShow(ffDay(first), first.Year() != last.Year(), loc),
		"endDate", ffDateShow(ffDay(last), last.Year() != now.Year(), loc))
}

// DateItems are the Date menu's rows (SearchFilterDateRangeSelect).
func (m *FilesFiltersModal) DateItems() []MenuItem {
	now := m.now()
	sel := ffPresetOf(m.after, m.before, now)
	tomorrow := ffDay(now.AddDate(0, 0, 1))
	items := []MenuItem{MenuItem{Label: "Any time", Radio: true, Checked: ffAnyTime(m.after, m.before, now),
		Do: func() { m.SetDates("", tomorrow) }}.In(nsSearch)}
	for _, p := range ffPresets(now) {
		items = append(items, MenuItem{Label: p.label, Radio: true, Checked: sel == p.label,
			Do: func() { m.SetDates(p.after, p.before) }}.In(nsSearch))
	}
	if sel == "" && (m.after != "" || m.before != "") { // the range's own row
		after, before := m.after, m.before
		items = append(items, MenuItem{Label: FilesDateLabel(after, before, now), Radio: true, Checked: true,
			Do: func() { m.SetDates(after, before) }})
	}
	pick := func(latest time.Time, selected string, done func(day time.Time)) func() {
		return func() {
			sel := time.Time{}
			if t, err := time.ParseInLocation("2006-01-02", selected, now.Location()); err == nil {
				sel = t
			}
			m.h.ShowOverlay(NewDatePickerRange(m.h, m.dateWin(), sel, time.Time{}, latest, done))
		}
	}
	day := func(s string, d int) string {
		t, err := time.ParseInLocation("2006-01-02", s, now.Location())
		if err != nil {
			return ""
		}
		return ffDay(t.AddDate(0, 0, d))
	}
	today, yesterday := now, now.AddDate(0, 0, -1)
	return append(items, Sep(),
		MenuItem{Label: "On…", Do: pick(today, day(m.after, 1), func(d time.Time) { // before the day after, after the day before
			m.SetDates(ffDay(d.AddDate(0, 0, -1)), ffDay(d.AddDate(0, 0, 1)))
		})}.In(nsSearch),
		MenuItem{Label: "Before…", Do: pick(today, m.before, func(d time.Time) { m.SetDates("", ffDay(d)) })}.In(nsSearch),
		MenuItem{Label: "After…", Do: pick(yesterday, m.after, func(d time.Time) { m.SetDates(ffDay(d), "") })}.In(nsSearch),
		MenuItem{Label: "Range…", Do: pick(today, day(m.after, 1), func(start time.Time) {
			// the range's start, then its end (no earlier than the start)
			m.h.ShowOverlay(NewDatePickerRange(m.h, m.dateWin(), start, start, today, func(end time.Time) {
				m.SetDates(ffDay(start.AddDate(0, 0, -1)), ffDay(end.AddDate(0, 0, 1)))
			}))
		})}.In(nsSearch))
}

// OpenDateMenu opens the Date menu under its trigger (bottom-left,
// offsetY -4, 280 wide: p-search_filter_menu).
func (m *FilesFiltersModal) OpenDateMenu() {
	m.h.ShowOverlay(NewMenuOpts(m.h, m.DateItems(), MenuOpts{Anchor: m.dateWin(), Width: 280, Popover: "bottom-left", OffsetY: -4}))
}

// dateWin is the Date trigger in window coordinates (last frame's).
func (m *FilesFiltersModal) dateWin() image.Rectangle { return m.dateR }

// ---- layout ----

func (m *FilesFiltersModal) layout(gtx layout.Context) layout.Dimensions {
	th := m.h.Theme()
	cl := colorsOf(th)
	if m.dateBtn.Clicked(gtx) {
		m.OpenDateMenu()
	}
	for { // openOnDownArrow
		ev, ok := gtx.Event(key.Filter{Focus: &m.dateBtn, Name: key.NameDownArrow})
		if !ok {
			break
		}
		if ke, ok := ev.(key.Event); ok && ke.State == key.Press {
			m.OpenDateMenu()
		}
	}
	w := gtx.Constraints.Max.X
	y := 0
	// Label: c-label--block, its text 15px bold (line-height 1.46667)
	// with 8px under it, then the control with 12px under it
	field := func(label string, ctl func(gtx layout.Context) layout.Dimensions) {
		lh := dp(gtx, 22)
		at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, lh)}
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Bold, cl.Fg, label)
			})
		})
		y += lh + dp(gtx, 8)
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, gtx.Constraints.Max.Y)}
			return ctl(gtx)
		})
		y += d.Size.Y + dp(gtx, 12)
	}
	// the options lists draw on top (op.Defer): In's over Date, From's
	// over both
	if !m.o.HideFrom {
		field(nsAdvSearch.T("From"), func(gtx layout.Context) layout.Dimensions { return m.from.layout(gtx, th, cl) })
	}
	field(nsAdvSearch.T("In"), func(gtx layout.Context) layout.Dimensions { return m.in.layout(gtx, th, cl) })
	field(nsAdvSearch.T("Date"), func(gtx layout.Context) layout.Dimensions {
		d := m.dateTrigger(gtx, th, cl, w)
		// in the window: the card's corner, the body's offset (28 in,
		// under the 70px title bar), the trigger's place in the body
		body := m.modal.rect.Min.Add(image.Pt(dp(gtx, 28), dp(gtx, 70)))
		m.dateR = image.Rectangle{Min: image.Pt(0, y), Max: image.Pt(w, y+d.Size.Y)}.Add(body)
		return d
	})
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// dateTrigger is the Date select's trigger: an outline medium button
// (36px, radius 8, padding 0 12px) across the body, its label (15px,
// weight 500: c-advanced_search_modal__select) at the left and the 18px
// caret-down at the right (justify-content: space-between).
func (m *FilesFiltersModal) dateTrigger(gtx layout.Context, th *theme.Theme, cl *Colors, w int) layout.Dimensions {
	h := dp(gtx, 36)
	sz := image.Pt(w, h)
	label := FilesDateLabel(m.after, m.before, m.now())
	return m.dateBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		r := image.Rectangle{Max: sz}
		rad := dp(gtx, 8)
		border := cl.BtnBorder
		if m.modal.ringOn(gtx, &m.dateBtn) {
			focusRing(gtx, cl, r, rad)
			border = color.NRGBA{}
		}
		bg := cl.BtnBg
		if m.dateBtn.Hovered() {
			bg = cl.BtnHover
		}
		if border.A > 0 {
			bw := border1(gtx)
			rrect(gtx, r, rad, border)
			rrect(gtx, r.Inset(bw), rad-bw, bg)
		} else {
			rrect(gtx, r, rad, bg)
		}
		pad, ic := dp(gtx, 12), dp(gtx, 18)
		at(gtx, image.Pt(pad, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-2*pad-ic-dp(gtx, 4)), h)}
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Medium, cl.Fg, label)
			})
		})
		at(gtx, image.Pt(w-pad-ic, (h-ic)/2), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, "caret-down", 18, cl.Fg)
		})
		pointerCursor(gtx, sz)
		return layout.Dimensions{Size: sz}
	})
}

// helpLink is the footer's HelpLink: the info icon (1em), a space and
// "Learn more about search" in the link colour, underlined on hover.
func (m *FilesFiltersModal) helpLink(gtx layout.Context) layout.Dimensions {
	th := m.h.Theme()
	cl := colorsOf(th)
	if m.help.Clicked(gtx) {
		if o, ok := m.h.(interface{ OpenURL(string) }); ok {
			o.OpenURL(filesHelpURL)
		} else {
			_ = openurl.Open(filesHelpURL)
		}
	}
	label := nsAdvSearch.T("Learn more about search")
	return m.help.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		h := dp(gtx, 22)
		is := dp(gtx, 15)
		gap := textW(gtx, th, 15, theme.Regular, " ")
		tw := textW(gtx, th, 15, theme.Regular, label)
		sz := image.Pt(is+gap+tw, h)
		if m.modal.ringOn(gtx, &m.help) {
			focusRing(gtx, cl, image.Rectangle{Max: sz}.Inset(-dp(gtx, 2)), dp(gtx, 4))
		}
		at(gtx, image.Pt(0, (h-is)/2), func(gtx layout.Context) layout.Dimensions { return icon(gtx, "info", 15, cl.Link) })
		at(gtx, image.Pt(is+gap, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(tw, h)}
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, cl.Link, label)
			})
		})
		if m.help.Hovered() {
			fill(gtx, image.Rect(is+gap, h-dp(gtx, 3), is+gap+tw, h-dp(gtx, 3)+maxInt(1, dp(gtx, 1))), cl.Link)
		}
		pointerCursor(gtx, sz)
		return layout.Dimensions{Size: sz}
	})
}
