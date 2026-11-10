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
// load from corex cxWorkingHours and save on every change through
// cxSetWorkingHours (Slack applies preferences immediately).

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

// avFormatHM formats minutes after midnight as Slack's "H:MM".
func avFormatHM(m int) string {
	m = ((m % 1440) + 1440) % 1440
	mm := m % 60
	s := strconv.Itoa(m/60) + ":"
	if mm < 10 {
		s += "0"
	}
	return s + strconv.Itoa(mm)
}

// avFromDND maps Slack's stored do-not-disturb window (dnd_start_hour →
// dnd_end_hour) to the "Allow notifications" window the page shows:
// notifications start when DND ends and end when it starts. Unparsable
// values fall back to 9:00 AM – 5:00 PM.
func avFromDND(dndStart, dndEnd string) (from, to int) {
	from, ok1 := avParseHM(dndEnd)
	to, ok2 := avParseHM(dndStart)
	if !ok1 || !ok2 {
		return 9 * 60, 17 * 60
	}
	return from, to
}

// avToDND is the inverse of avFromDND.
func avToDND(from, to int) (dndStart, dndEnd string) {
	return avFormatHM(to), avFormatHM(from)
}

// ---- modal ----

type avPrefs struct {
	h       Host
	modal   *Modal
	nav     [len(avNav)]widget.Clickable
	hr      cxHours
	from    dtTime // Allow notifications from…
	to      dtTime // …to
	loaded  bool
	daysBtn widget.Clickable
	checks  [3]widget.Clickable
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
	p := &avPrefs{h: h, hr: cxHours{Enabled: true, Days: "every_day", HuddleStatus: true, FocusStatus: true}}
	p.from.Mins, p.to.Mins = 9*60, 17*60
	p.from.NoIcon, p.to.NoIcon = true, true
	p.from.Disabled, p.to.Disabled = true, true
	p.from.OnChange = p.hoursChanged
	p.to.OnChange = p.hoursChanged
	p.load()
	return p
}

func (p *avPrefs) load() {
	h := p.h
	if h.Core() == nil {
		h.Toast("Not connected yet")
		return
	}
	run(h, func(ctx context.Context) (cxHours, error) { return cxWorkingHours(ctx, h) }, func(hr cxHours, err error) {
		if err != nil {
			h.Toast("Couldn't load your working hours: " + errText(err))
			return
		}
		p.hr = hr
		// cxHours.End is dnd_start_hour and Start is dnd_end_hour.
		p.from.Mins, p.to.Mins = avFromDND(hr.End, hr.Start)
		p.from.Disabled, p.to.Disabled = false, false
		p.loaded = true
		h.Invalidate()
	})
}

func (p *avPrefs) hoursChanged() {
	p.hr.Enabled = true
	p.save()
}

// save writes the current state (async; toasts on error).
func (p *avPrefs) save() {
	if !p.loaded {
		return
	}
	hr := p.hr
	hr.End, hr.Start = avToDND(p.from.Mins, p.to.Mins)
	p.hr = hr
	h := p.h
	do(h, "", func(ctx context.Context) error { return cxSetWorkingHours(ctx, h, hr) })
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
			p.hr.HuddleStatus = !p.hr.HuddleStatus
		case 1:
			p.hr.FocusStatus = !p.hr.FocusStatus
		default:
			p.hr.OutsideStatus = !p.hr.OutsideStatus
		}
		p.save()
	}
	if p.daysBtn.Clicked(gtx) {
		items := make([]MenuItem, 0, len(avDays))
		for _, d := range avDays {
			code := d.code
			items = append(items, MenuItem{Label: d.label, Checked: p.hr.Days == code, Do: func() {
				if code == p.hr.Days {
					return
				}
				if code == "custom" {
					h.Toast("Custom working days aren't supported yet")
					return
				}
				p.hr.Days, p.hr.Enabled = code, true
				p.save()
				h.Invalidate()
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
	p.from.look, p.to.look = look, look
	cw := maxInt(1, bw-cx)
	at(gtx, image.Pt(cx, dp(gtx, 32)-dp(gtx, 14)), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, dp(gtx, 28), func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 18, theme.Bold, cl.Fg, "Availability")
		})
	})
	avText(gtx, th, cx, dp(gtx, 87), 15, cw, "Working hours", true)
	p.para(gtx, th, pl.para, cx, dp(gtx, 102), cw, "You’ll only receive notifications in the hours you choose. Outside of those times, notifications will be paused except for VIPs.")
	avText(gtx, th, cx, dp(gtx, 165), 15, cw, "Allow notifications:", true)

	sy := dp(gtx, 185)
	at(gtx, image.Pt(cx, sy), func(gtx layout.Context) layout.Dimensions {
		return dtFieldIn(gtx, th, look, &p.daysBtn, dp(gtx, 160), dtLarge, "", avDaysLabel(p.hr.Days), true, false, !p.loaded)
	})
	tx := cx + dp(gtx, 160+16)
	at(gtx, image.Pt(tx, sy), func(gtx layout.Context) layout.Dimensions {
		return p.from.Layout(gtx, p.h, org.Add(image.Pt(tx, sy)), dp(gtx, 130), dtLarge)
	})
	tx += dp(gtx, 130+8)
	toW := textW(gtx, th, 15, theme.Regular, "to")
	avText(gtx, th, tx, sy+dp(gtx, 18), 15, toW+1, "to", false)
	tx += toW + dp(gtx, 8)
	at(gtx, image.Pt(tx, sy), func(gtx layout.Context) layout.Dimensions {
		return p.to.Layout(gtx, p.h, org.Add(image.Pt(tx, sy)), dp(gtx, 130), dtLarge)
	})
	fill(gtx, image.Rect(cx, dp(gtx, 236), bw, dp(gtx, 236)+maxInt(1, dp(gtx, 1))), pl.hr)

	avText(gtx, th, cx, dp(gtx, 268), 15, cw, "Automatic statuses", true)
	p.para(gtx, th, pl.para, cx, dp(gtx, 283), cw, "Let people know when responses might be delayed. If you already have a status set, Slack won’t change it.")
	avText(gtx, th, cx, dp(gtx, 346), 15, cw, "Set status to:", true)
	rows := [3]struct {
		emoji, bold, rest string
		on                bool
	}{
		{"headphones", "In a huddle", " for huddles", p.hr.HuddleStatus},
		{"telescope", "In focus mode", " for focus mode", p.hr.FocusStatus},
		{"mantelpiece_clock", "Outside work hours", " for outside working hours", p.hr.OutsideStatus},
	}
	for i, r := range rows {
		cy := dp(gtx, 375) + i*dp(gtx, 26)
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
	fill(gtx, image.Rect(cx, dp(gtx, 463), bw, dp(gtx, 463)+maxInt(1, dp(gtx, 1))), pl.hr)

	avText(gtx, th, cx, dp(gtx, 495), 15, cw, "Additional options", true)
	avText(gtx, th, cx, dp(gtx, 529), 15, cw, "Turn on focus mode when:", true)
	{
		// .c-input_checkbox--disabled: the native box at opacity .38, its
		// .c-label--disabled label in --dt_color-content-sec
		cy := dp(gtx, 558)
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
	return dp(gtx, 580)
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
