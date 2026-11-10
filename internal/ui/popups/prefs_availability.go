package popups

import (
	"context"
	"image"
	"image/color"
	"strconv"
	"strings"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// Preferences modal (workhours.png): 800x700, radius 8, header 76 with a
// 1px bottom border, "Preferences" 22/900. Left nav 205 wide at x=16, rows
// 29px at a 33px pitch, 15px labels at x=32 with 16px icons at x=8;
// selected = #1264a3, white, radius 6. Content from x=249 to 28 from the
// right edge.
//
// Only the Availability page is built: "Availability" 18/700; "Working
// hours" 15/700 + a 15px #454447 paragraph; "Allow notifications:" with
// [Every day ▾ 160] [12:30 AM ▾ 130] to [12:00 AM ▾ 130] (36px); divider
// #dddddd; "Automatic statuses" (three 16px checkboxes with emoji);
// divider; "Additional options" with a disabled calendar checkbox. Values
// load through avReadHours and each change writes what it changed, as
// Slack does (prefs_availability_hours.go). Custom days add a row per day
// (p-dnd_preferences__custom_day_section: 36 tall, 16 apart, a 120px day
// label), Weekdays a 13px subtext, and "VIPs" in the paragraph is a link to
// the VIP section.

type avNavItem struct{ id, label, icon string }

// prefsLook is the Preferences surface and the tokens the page's controls
// take, from Slack's stylesheets (client-boot-styles cf726c4f2eaecc74 for
// the rules, slack-kit-gantry-v2-styles f859766a17f82b2a for the values),
// matching getComputedStyle in the live dialog (2026-11-09):
//
//	.c-sk-modal, .c-select_button    background var(--dt_color-base-pry)   #fff / #1a1d21
//	.c-select_button                 border var(--dt_color-otl-pry)        #7c7a7f / #797c81
//	the Working hours paragraph      var(--dt_color-content-sec)           #454447 / #b9babd
//	.c-label--disabled               var(--dt_color-content-sec)
//	hr                               rgb(var(--sk_foreground_low_solid))   #dddddd / #35373b
//
// None is an account-theme token; a stylesheet overriding them (a Slick
// theme, user.css) recolours them.
type prefsLook struct {
	surface, fieldBorder, para, hr color.NRGBA
	dark                           bool
}

func prefsLookOf(th *theme.Theme) prefsLook {
	l := prefsLook{surface: rgb(0xffffff), fieldBorder: rgb(0x7c7a7f), para: rgb(0x454447), hr: rgb(0xdddddd)}
	if th != nil && th.Dark {
		l = prefsLook{surface: rgb(0x1a1d21), fieldBorder: rgb(0x797c81), para: rgb(0xb9babd), hr: rgb(0x35373b), dark: true}
	}
	if th == nil || th.Tokens == nil || !th.Tokens.Overridden() {
		return l
	}
	t := th.Tokens
	theme.SetColor(t, &l.surface, "var(--dt_color-base-pry)")
	theme.SetColor(t, &l.fieldBorder, "var(--dt_color-otl-pry)")
	theme.SetColor(t, &l.para, "var(--dt_color-content-sec)")
	if theme.SetColor(t, &l.hr, "rgb(var(--sk_foreground_low_solid))") && l.hr.A != 0xff {
		l.hr = theme.Over(l.hr, l.surface)
	}
	return l
}

// BasePryOf is --dt_color-base-pry, the background of Slack's .c-sk-modal
// and of its inputs and select buttons: #fff in light, #1a1d21 in dark
// (slack-kit's token sheet), or a stylesheet's override. ModalBg keeps
// #222529 in dark for the other modals, whose fields are drawn on it.
func BasePryOf(th *theme.Theme) color.NRGBA { return prefsLookOf(th).surface }

var avNav = [...]avNavItem{
	{"availability", "Availability", "clock"},
	{"notifications", "Notifications", "bell"},
	{"vip", "VIP", "star"},
	{"navigation", "Navigation", "sidebar"},
	{"home", "Home", "home"},
	{"appearance", "Appearance", "edit"},
	{"messages", "Messages & media", "dms"},
	{"language", "Language & region", "globe"},
	{"accessibility", "Accessibility", "eye"},
	{"mark_read", "Mark as read", "check-circle"},
	{"audio_video", "Audio & video", "video"},
	{"salesforce", "Salesforce", "apps"},
	{"connected", "Connected accounts", "link"},
	{"privacy", "Privacy & visibility", "lock"},
	{"advanced", "Advanced", "settings"},
}

var avDays = [...]struct{ code, label string }{
	{"every_day", "Every day"},
	{"weekdays", "Weekdays"},
	{"custom", "Custom"},
}

func avDaysLabel(code string) string {
	for _, d := range avDays {
		if d.code == code {
			return d.label
		}
	}
	return avDays[0].label
}

// ---- working-hours mapping (pure; tested) ----

// avParseHM parses Slack's "H:MM" 24h pref ("22:00", "8:30") into minutes
// after midnight; ok false for anything else.
func avParseHM(s string) (int, bool) {
	hs, ms, found := strings.Cut(strings.TrimSpace(s), ":")
	if !found {
		return 0, false
	}
	hh, err1 := strconv.Atoi(hs)
	mm, err2 := strconv.Atoi(ms)
	if err1 != nil || err2 != nil || hh < 0 || hh > 24 || mm < 0 || mm > 59 {
		return 0, false
	}
	return (hh*60 + mm) % (24 * 60), true
}

// ---- modal ----

type avPrefs struct {
	h       Host
	modal   *Modal
	nav     [len(avNav)]widget.Clickable
	hr      avHours
	from    avSelect // Allow notifications from… (dnd_end_hour)
	to      avSelect // …to (dnd_start_hour)
	dayFrom [7]avSelect
	dayTo   [7]avSelect
	loaded  bool
	daysBtn widget.Clickable
	checks  [3]widget.Clickable
	vipBtn  widget.Clickable
	// toSection switches the Preferences dialog to another section (the
	// "VIPs" link: setActiveSection vip); nil opens Preferences there.
	toSection func(id string)
}

// avSelect is one of the page's time selects (Slack's Select, 130 wide):
// a field and, open, its option list.
type avSelect struct {
	btn  widget.Clickable
	list Overlay
}

func (s *avSelect) open() bool { return s.list != nil && !s.list.Dismissed() }

// layout draws the select at (x, y) showing value; a click opens opts and
// pick gets the chosen value.
func (s *avSelect) layout(gtx layout.Context, p *avPrefs, org image.Point, x, y int, value string, opts []string, pick func(string)) {
	th := p.h.Theme()
	pl := prefsLookOf(th)
	look := dtLook{bg: pl.surface, border: pl.fieldBorder}
	w := dp(gtx, 130)
	if s.btn.Clicked(gtx) && !s.open() && p.loaded {
		labels := make([]string, len(opts))
		sel := 0
		for i, o := range opts {
			labels[i] = avLabel(o)
			if o == value {
				sel = i
			}
		}
		r := image.Rectangle{Min: org.Add(image.Pt(x, y)), Max: org.Add(image.Pt(x+w, y+dtHeight(gtx, dtLarge)))}
		s.list = NewOptionList(p.h, r, labels, sel, func(i int) {
			if i >= 0 && i < len(opts) && opts[i] != value {
				pick(opts[i])
			}
			p.h.Invalidate()
		})
		p.h.ShowOverlay(s.list)
	}
	at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		return dtFieldIn(gtx, th, look, &s.btn, w, dtLarge, "", avLabel(value), true, s.open(), !p.loaded)
	})
}

// NewPreferencesModal builds the Preferences dialog opened on section
// ("availability"; the other pages aren't built yet, so any section shows
// Availability and their nav items call the OpenPreferences hook, or toast
// when it is unset). Show it with h.ShowOverlay.
func NewPreferencesModal(h Host, section string) *Modal {
	_ = section // only Availability exists so far
	p := newAvPrefs(h)
	p.modal = NewModal(h, ModalOpts{Title: "Preferences", Width: 800, HeadH: 76, Body: p.layout})
	return p.modal
}

func newAvPrefs(h Host) *avPrefs {
	p := &avPrefs{h: h, hr: avHours{Enabled: true, Days: "every_day", Start: "17:00", End: "09:00", Huddle: true}}
	p.load()
	return p
}

func (p *avPrefs) load() {
	h := p.h
	a := h.Core()
	if a == nil {
		h.Toast("Not connected yet")
		return
	}
	run(h, func(ctx context.Context) (avHours, error) { return avReadHours(ctx, a) }, func(hr avHours, err error) {
		if err != nil {
			h.Toast("Couldn't load your working hours: " + errText(err))
			return
		}
		p.hr = hr
		p.loaded = true
		h.Invalidate()
	})
}

// write sends a change's prefs (async; toasts on error).
func (p *avPrefs) write(prefs map[string]any) {
	if !p.loaded {
		return
	}
	a := p.h.Core()
	if a == nil {
		return
	}
	do(p.h, "", func(ctx context.Context) error { return avWritePrefs(ctx, a, prefs) })
	p.h.Invalidate()
}

// setPref writes one pref (setUserPrefByApi).
func (p *avPrefs) setPref(name string, value any) {
	a := p.h.Core()
	if a == nil || !p.loaded {
		return
	}
	do(p.h, "", func(ctx context.Context) error { return a.SetPref(ctx, name, value) })
	p.h.Invalidate()
}

func (p *avPrefs) events(gtx layout.Context, org image.Point, cx int) {
	h := p.h
	for i := range p.nav {
		if p.modal == nil {
			break
		}
		if !p.nav[i].Clicked(gtx) || i == 0 {
			continue
		}
		if OpenPreferences != nil {
			p.modal.Dismiss()
			OpenPreferences()
			return
		}
		h.Toast(avNav[i].label + " preferences aren't available in yoloslack yet")
	}
	if !p.loaded {
		return
	}
	for i := range p.checks {
		if !p.checks[i].Clicked(gtx) {
			continue
		}
		switch i {
		case 0:
			p.hr.Huddle = !p.hr.Huddle
			p.setPref("huddles_set_status", p.hr.Huddle)
		case 1:
			p.hr.Focus = avToggleShow(p.hr.Focus, p.hr.FocusShown())
			p.setPref("focus_mode_status", p.hr.Focus)
		default:
			p.hr.Outside = avToggleShow(p.hr.Outside, p.hr.OutsideShown())
			p.setPref("scheduled_dnd_status", p.hr.Outside)
		}
	}
	if p.vipBtn.Clicked(gtx) {
		if p.toSection != nil {
			p.toSection("vip")
		} else {
			if p.modal != nil {
				p.modal.Dismiss()
			}
			openPreferences(h, "vip")
		}
		return
	}
	if p.daysBtn.Clicked(gtx) {
		items := make([]MenuItem, 0, len(avDays))
		for _, d := range avDays {
			code := d.code
			items = append(items, MenuItem{Label: d.label, Checked: p.hr.shownDays() == code, Do: func() {
				if code == p.hr.shownDays() {
					return
				}
				p.write(p.hr.avSetDays(code))
			}})
		}
		r := image.Rectangle{Min: org.Add(image.Pt(cx, dp(gtx, 185))), Max: org.Add(image.Pt(cx+dp(gtx, 160), dp(gtx, 221)))}
		h.ShowOverlay(NewMenuOpts(h, items, MenuOpts{Anchor: r, Width: 160}))
	}
}

// avText draws one line vertically centered on cy.
func avText(gtx layout.Context, th *theme.Theme, x, cy int, size float32, w int, s string, bold bool) layout.Dimensions {
	cl := colorsOf(th)
	wt := theme.Regular
	if bold {
		wt = theme.Bold
	}
	return at(gtx, image.Pt(x, cy-dp(gtx, 12)), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w), dp(gtx, 24))}
		return vcenter(gtx, dp(gtx, 24), func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, size, wt, cl.Fg, s)
		})
	})
}

func (p *avPrefs) layout(gtx layout.Context) layout.Dimensions {
	if p.modal.done {
		return layout.Dimensions{}
	}
	org := dtModalOrigin(gtx, p.modal)
	p.events(gtx, org, dp(gtx, 221))
	if p.modal.done {
		return layout.Dimensions{}
	}
	th := p.h.Theme()
	cl := colorsOf(th)
	bw := gtx.Constraints.Max.X
	padX := dp(gtx, 28)
	bh := dp(gtx, 700-76-28) // the Modal adds 28 below a buttonless body

	// header border (full modal width)
	fill(gtx, image.Rect(-padX, 0, bw+padX, maxInt(1, dp(gtx, 1))), cl.Ring)

	// left nav
	nx, nw := dp(gtx, 16)-padX, dp(gtx, 205)
	rowH, pitch := dp(gtx, 29), dp(gtx, 33)
	for i := range avNav {
		it := &avNav[i]
		sel := i == 0
		y := dp(gtx, 16) + i*pitch
		at(gtx, image.Pt(nx, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(nw, rowH))
			return clickBox(gtx, &p.nav[i], nil, func(gtx layout.Context) layout.Dimensions {
				sz := image.Pt(nw, rowH)
				fg, ic := cl.Fg, cl.Fg2
				switch {
				case sel:
					rrect(gtx, image.Rectangle{Max: sz}, dp(gtx, 6), cl.Highlight)
					fg, ic = cl.HighText, cl.HighText
				case p.nav[i].Hovered():
					rrect(gtx, image.Rectangle{Max: sz}, dp(gtx, 6), cl.BgHover)
				}
				at(gtx, image.Pt(dp(gtx, 8), (rowH-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
					return icon(gtx, it.icon, 16, ic)
				})
				at(gtx, image.Pt(dp(gtx, 32), 0), func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints = layout.Constraints{Max: image.Pt(nw-dp(gtx, 40), rowH)}
					return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 15, theme.Regular, fg, it.label)
					})
				})
				return layout.Dimensions{Size: sz}
			})
		})
	}

	p.page(gtx, org, dp(gtx, 221), bw)
	return layout.Dimensions{Size: image.Pt(bw, bh)}
}

// page draws the Availability page body with its left edge at cx and its
// right edge at bw (body coordinates); org is the body origin in window
// coordinates (for the dropdown anchors). Returns the page height.
func (p *avPrefs) page(gtx layout.Context, org image.Point, cx, bw int) int {
	th := p.h.Theme()
	cl := colorsOf(th)
	pl := prefsLookOf(th)
	look := dtLook{bg: pl.surface, border: pl.fieldBorder}
	cw := maxInt(1, bw-cx)
	at(gtx, image.Pt(cx, dp(gtx, 32)-dp(gtx, 14)), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, dp(gtx, 28), func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 18, theme.Bold, cl.Fg, "Availability")
		})
	})
	avText(gtx, th, cx, dp(gtx, 87), 15, cw, "Working hours", true)
	p.vipPara(gtx, th, cl, pl.para, cx, dp(gtx, 102), cw)
	avText(gtx, th, cx, dp(gtx, 165), 15, cw, "Allow notifications:", true)

	sy := dp(gtx, 185)
	days := p.hr.shownDays()
	at(gtx, image.Pt(cx, sy), func(gtx layout.Context) layout.Dimensions {
		return dtFieldIn(gtx, th, look, &p.daysBtn, dp(gtx, 160), dtLarge, "", avDaysLabel(days), true, false, !p.loaded)
	})
	toW := textW(gtx, th, 15, theme.Regular, "to")
	// the two selects and "to" between them (p-dnd_preferences__time_select_to: margin 0 8)
	pair := func(x, y int, from string, fromOpts []string, pickFrom func(string), to string, toOpts []string, pickTo func(string), fromSel, toSel *avSelect, hideTo bool) {
		fromSel.layout(gtx, p, org, x, y, from, fromOpts, pickFrom)
		if hideTo {
			return
		}
		x += dp(gtx, 130+8)
		avText(gtx, th, x, y+dp(gtx, 18), 15, toW+1, "to", false)
		x += toW + dp(gtx, 8)
		toSel.layout(gtx, p, org, x, y, to, toOpts, pickTo)
	}
	hrY := dp(gtx, 236)
	if days == "custom" {
		// DndCustomPreferences: a 36px row per day, 16 apart, below the
		// "Allow notifications:" line; the selects keep their wrapper's
		// 16px bottom margin, so they sit 8px above the row's center
		y := dp(gtx, 237)
		none := p.hr.avNoneCount()
		for i, d := range avWeek {
			i := i
			before, after := p.hr.dayShown(i)
			allDay := p.hr.Week[i].Enabled == avAllDay
			first := before
			if allDay {
				first = avAllDay
			}
			avText(gtx, th, cx, y+dp(gtx, 18)+dp(gtx, 1), 15, dp(gtx, 120), d.label, false)
			pair(cx+dp(gtx, 120), y-dp(gtx, 8),
				first, avStartOpts(before, allDay || none != 6), func(v string) { p.write(p.hr.avSetDayFrom(i, v)) },
				after, avEndOpts(before, after, false), func(v string) { p.write(p.hr.avSetDayTo(i, v)) },
				&p.dayFrom[i], &p.dayTo[i], allDay)
			y += dp(gtx, 36+16)
		}
		hrY = y
	} else {
		from, to := p.hr.shownFrom(), p.hr.shownTo()
		pair(cx+dp(gtx, 160+16), sy,
			from, avStartOpts(from, false), func(v string) { p.write(p.hr.avSetFrom(v)) },
			to, avEndOpts(from, to, true), func(v string) { p.write(p.hr.avSetTo(v)) },
			&p.from, &p.to, false)
		if days == "weekdays" {
			// p-dnd_preferences__subtext: 13px at 70%, margin-top -8 (and a
			// p's 1em below)
			sub := dp(gtx, 229)
			at(gtx, image.Pt(cx, sub), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(cw, dp(gtx, 19))}
				return vcenter(gtx, dp(gtx, 19), func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Regular, theme.WithAlpha(cl.Fg, 0xb3), "You won’t receive notifications on Saturday or Sunday.")
				})
			})
			hrY = sub + dp(gtx, 19+13)
		}
	}
	fill(gtx, image.Rect(cx, hrY, bw, hrY+maxInt(1, dp(gtx, 1))), pl.hr)
	off := hrY - dp(gtx, 236) // what follows moves down with the rows

	avText(gtx, th, cx, off+dp(gtx, 268), 15, cw, "Automatic statuses", true)
	p.para(gtx, th, pl.para, cx, off+dp(gtx, 283), cw, "Let people know when responses might be delayed. If you already have a status set, Slack won’t change it.")
	avText(gtx, th, cx, off+dp(gtx, 346), 15, cw, "Set status to:", true)
	rows := [3]struct {
		emoji, bold, rest string
		on                bool
	}{
		{"headphones", "In a huddle", " for huddles", p.hr.Huddle},
		{"telescope", "In focus mode", " for focus mode", p.hr.FocusShown()},
		{"mantelpiece_clock", "Outside work hours", " for outside working hours", p.hr.OutsideShown()},
	}
	for i, r := range rows {
		cy := off + dp(gtx, 375) + i*dp(gtx, 26)
		rh := dp(gtx, 26)
		at(gtx, image.Pt(cx, cy-rh/2), func(gtx layout.Context) layout.Dimensions {
			draw := func(gtx layout.Context) layout.Dimensions {
				// Slack's c-input_checkbox is the browser's own (appearance:auto)
				at(gtx, image.Pt(0, (rh-dp(gtx, theme.NativeCheckSize))/2), func(gtx layout.Context) layout.Dimensions {
					return theme.NativeCheck(gtx, pl.dark, theme.NativeCheckState{On: r.on, Hover: p.checks[i].Hovered()})
				})
				at(gtx, image.Pt(dp(gtx, 25), (rh-dp(gtx, 22))/2), func(gtx layout.Context) layout.Dimensions {
					return emojiImage(gtx, p.h, r.emoji, 22)
				})
				x := dp(gtx, 51)
				avText(gtx, th, x, rh/2, 15, cw-x, r.bold, true)
				x += textW(gtx, th, 15, theme.Bold, r.bold)
				avText(gtx, th, x, rh/2, 15, maxInt(1, cw-x), r.rest, false)
				x += textW(gtx, th, 15, theme.Regular, r.rest)
				return layout.Dimensions{Size: image.Pt(minInt(cw, x), rh)}
			}
			if !p.loaded {
				return draw(gtx)
			}
			return clickBox(gtx, &p.checks[i], nil, draw)
		})
	}
	fill(gtx, image.Rect(cx, off+dp(gtx, 463), bw, off+dp(gtx, 463)+maxInt(1, dp(gtx, 1))), pl.hr)

	avText(gtx, th, cx, off+dp(gtx, 495), 15, cw, "Additional options", true)
	avText(gtx, th, cx, off+dp(gtx, 529), 15, cw, "Turn on focus mode when:", true)
	{
		// .c-input_checkbox--disabled: the native box at opacity .38, its
		// .c-label--disabled label in --dt_color-content-sec
		cy := off + dp(gtx, 558)
		sz := dp(gtx, theme.NativeCheckSize)
		at(gtx, image.Pt(cx, cy-sz/2), func(gtx layout.Context) layout.Dimensions {
			return theme.NativeCheck(gtx, pl.dark, theme.NativeCheckState{Opacity: .38})
		})
		at(gtx, image.Pt(cx+dp(gtx, 25), cy-dp(gtx, 12)), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, dp(gtx, 24), func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, pl.para, "Your calendar has a focus block")
			})
		})
	}
	return off + dp(gtx, 580)
}

// avWorkingPara is the Working hours paragraph; its last word, "VIPs", is
// the link (LegendSubtext with vipsLink).
const avWorkingPara = "You’ll only receive notifications in the hours you choose. Outside of those times, notifications will be paused except for "

// avWrap breaks words greedily into lines no wider than w (fits measures).
func avWrap(words []string, w int, fits func(s string) int) []string {
	var lines []string
	cur := ""
	for _, wd := range words {
		try := wd
		if cur != "" {
			try = cur + " " + wd
		}
		if cur != "" && fits(try) > w {
			lines = append(lines, cur)
			cur = wd
			continue
		}
		cur = try
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// vipPara draws the paragraph (15/22) with "VIPs" as a link to the VIP
// section: link colour, underlined on hover.
func (p *avPrefs) vipPara(gtx layout.Context, th *theme.Theme, cl *Colors, col color.NRGBA, x, y, w int) {
	words := append(strings.Fields(avWorkingPara), "VIPs.")
	measure := func(s string) int { return textW(gtx, th, 15, theme.Regular, s) }
	lines := avWrap(words, w, measure)
	lh := dp(gtx, 22)
	for i, ln := range lines {
		ly := y + i*lh
		last := i == len(lines)-1
		body := ln
		if last {
			body = strings.TrimSuffix(strings.TrimSuffix(ln, "VIPs."), " ")
		}
		if body != "" {
			at(gtx, image.Pt(x, ly), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(w, lh)}
				return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 15, theme.Regular, col, body)
				})
			})
		}
		if !last {
			continue
		}
		lx := x
		if body != "" {
			lx += measure(body + " ")
		}
		lw := measure("VIPs")
		at(gtx, image.Pt(lx, ly), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, &p.vipBtn, nil, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(lw+1, lh)}
				vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 15, theme.Regular, cl.Link, "VIPs")
				})
				if p.vipBtn.Hovered() {
					fill(gtx, image.Rect(0, lh-dp(gtx, 5), lw, lh-dp(gtx, 5)+maxInt(1, dp(gtx, 1))), cl.Link)
				}
				return layout.Dimensions{Size: image.Pt(lw, lh)}
			})
		})
		at(gtx, image.Pt(lx+lw, ly), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(measure(".")+1, lh)}
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, col, ".")
			})
		})
	}
}

// ---- the page as a widget (ui-shell's single Preferences modal) ----

// OpenPreferencesSection, when set by the ui root, opens its Preferences
// modal on a section ("availability"); "Edit your working hours" uses it.
var OpenPreferencesSection func(section string)

// AvailabilityPage is the Preferences > Availability page body (working
// hours and automatic statuses) without a modal or nav, for the ui root's
// Preferences modal. It loads the prefs on creation and saves on change.
type AvailabilityPage struct {
	p      *avPrefs
	origin image.Point
}

// NewAvailabilityPage builds the page. Call SetOrigin each frame with the
// page's top-left in window coordinates so its dropdowns open in place.
func NewAvailabilityPage(h Host) *AvailabilityPage {
	return &AvailabilityPage{p: newAvPrefs(h)}
}

// SetOrigin sets the page's top-left corner in window coordinates.
func (a *AvailabilityPage) SetOrigin(pt image.Point) { a.origin = pt }

// SetSectionNav gives the page the dialog's section switch, which the
// "VIPs" link calls with "vip" (Slack's setActiveSection).
func (a *AvailabilityPage) SetSectionNav(f func(id string)) { a.p.toSection = f }

// Layout draws the page body using the constraint width.
func (a *AvailabilityPage) Layout(gtx layout.Context) layout.Dimensions {
	drain()
	gtx.Constraints.Min = image.Point{}
	w := gtx.Constraints.Max.X
	a.p.events(gtx, a.origin, 0)
	h := a.p.page(gtx, a.origin, 0, w)
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// openPreferences opens Preferences on section: the root's modal when it
// installed OpenPreferencesSection, else this package's own.
func openPreferences(h Host, section string) {
	if OpenPreferencesSection != nil {
		OpenPreferencesSection(section)
		return
	}
	h.ShowOverlay(NewPreferencesModal(h, section))
}

// para draws a 15/22 paragraph (--dt_color-content-sec) with its top at y.
func (p *avPrefs) para(gtx layout.Context, th *theme.Theme, col color.NRGBA, x, y, w int, s string) {
	at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		return para(gtx, th, 15, 22, theme.Regular, col, s, 0)
	})
}
