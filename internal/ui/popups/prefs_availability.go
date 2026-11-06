package popups

import (
	"context"
	"image"
	"strconv"
	"strings"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
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
	p := &avPrefs{h: h, hr: cxHours{Enabled: true, Days: "every_day", HuddleStatus: true, FocusStatus: true}}
	p.from.Mins, p.to.Mins = 9*60, 17*60
	p.from.NoIcon, p.to.NoIcon = true, true
	p.from.Disabled, p.to.Disabled = true, true
	p.from.OnChange = p.hoursChanged
	p.to.OnChange = p.hoursChanged
	p.modal = NewModal(h, ModalOpts{Title: "Preferences", Width: 800, HeadH: 76, Body: p.layout})
	p.load()
	return p.modal
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

func (p *avPrefs) events(gtx layout.Context, org image.Point) {
	h := p.h
	for i := range p.nav {
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
		r := image.Rectangle{Min: org.Add(image.Pt(dp(gtx, 221), dp(gtx, 185))), Max: org.Add(image.Pt(dp(gtx, 221+160), dp(gtx, 221)))}
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
	p.events(gtx, org)
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

	// Availability page
	cx := dp(gtx, 221)
	cw := maxInt(1, bw-cx)
	at(gtx, image.Pt(cx, dp(gtx, 32)-dp(gtx, 14)), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, dp(gtx, 28), func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 18, theme.Bold, cl.Fg, "Availability")
		})
	})
	avText(gtx, th, cx, dp(gtx, 87), 15, cw, "Working hours", true)
	p.para(gtx, th, cl, cx, dp(gtx, 102), cw, "You’ll only receive notifications in the hours you choose. Outside of those times, notifications will be paused except for VIPs.")
	avText(gtx, th, cx, dp(gtx, 165), 15, cw, "Allow notifications:", true)

	sy := dp(gtx, 185)
	at(gtx, image.Pt(cx, sy), func(gtx layout.Context) layout.Dimensions {
		return dtField(gtx, th, &p.daysBtn, dp(gtx, 160), dtLarge, "", avDaysLabel(p.hr.Days), true, false, !p.loaded)
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
	fill(gtx, image.Rect(cx, dp(gtx, 236), bw, dp(gtx, 236)+maxInt(1, dp(gtx, 1))), cl.Tab)

	avText(gtx, th, cx, dp(gtx, 268), 15, cw, "Automatic statuses", true)
	p.para(gtx, th, cl, cx, dp(gtx, 283), cw, "Let people know when responses might be delayed. If you already have a status set, Slack won’t change it.")
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
				actCheckbox(gtx, cl, image.Pt(0, (rh-dp(gtx, 16))/2), r.on)
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
	fill(gtx, image.Rect(cx, dp(gtx, 463), bw, dp(gtx, 463)+maxInt(1, dp(gtx, 1))), cl.Tab)

	avText(gtx, th, cx, dp(gtx, 495), 15, cw, "Additional options", true)
	avText(gtx, th, cx, dp(gtx, 529), 15, cw, "Turn on focus mode when:", true)
	{
		cy := dp(gtx, 558)
		sz := dp(gtx, 16)
		box := image.Rect(cx, cy-sz/2, cx+sz, cy-sz/2+sz)
		rrect(gtx, box, dp(gtx, 4), theme.WithAlpha(cl.Fg, 0x33))
		rrect(gtx, box.Inset(1), dp(gtx, 4)-1, cl.BgAlt)
		at(gtx, image.Pt(cx+dp(gtx, 25), cy-dp(gtx, 12)), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, dp(gtx, 24), func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, cl.Muted, "Your calendar has a focus block")
			})
		})
	}
	return layout.Dimensions{Size: image.Pt(bw, bh)}
}

// para draws a 15/22 #454447 paragraph with its top at y.
func (p *avPrefs) para(gtx layout.Context, th *theme.Theme, cl *Colors, x, y, w int, s string) {
	at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		return para(gtx, th, 15, 22, theme.Regular, cl.Fg2, s, 0)
	})
}
