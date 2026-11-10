package ui

import (
	"image"
	"strconv"

	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// prefsForm lays out one Preferences page top to bottom: Slack's section
// headings (700 18px/24), checkbox and radio rows (the browser's 13px box,
// 15px label, hint) and muted notes, in prefsColors. Rows keep their own clickables on the modal
// (keyed), so a page is just a sequence of calls each frame.
type prefsForm struct {
	p   *prefsModal
	gtx layout.Context
	w   int
	y   int
}

// prefsDrawn is one thing a page drew, in order: tests read a page back
// through prefsModal.rec (nil outside tests).
type prefsDrawn struct {
	Kind    string // h2, legend, text, check, radio, select, button, link, list
	Key     string // the row's clickable key ("" for text)
	Label   string
	On      bool // checkbox/radio state
	Enabled bool // false: shown the way Slack does, greyed (naNote)
}

func (f *prefsForm) record(kind, key, label string, on, enabled bool) {
	if f.p.rec != nil {
		*f.p.rec = append(*f.p.rec, prefsDrawn{Kind: kind, Key: key, Label: label, On: on, Enabled: enabled})
	}
}

func (p *prefsModal) form(gtx layout.Context) *prefsForm {
	return &prefsForm{p: p, gtx: gtx, w: gtx.Constraints.Max.X}
}

// dims is the page's size so far.
func (f *prefsForm) dims() layout.Dimensions {
	return layout.Dimensions{Size: image.Pt(f.w, f.y)}
}

// heading is a section title (Slack's h2: 700 18px/24, 30px stride).
func (f *prefsForm) heading(t string) {
	gtx, a := f.gtx, f.p.a
	f.record("h2", "", t, false, true)
	at(gtx, image.Pt(0, f.y), image.Pt(f.w, gtx.Dp(24)), func(gtx layout.Context) layout.Dimensions {
		return a.th.Label(gtx, 18, theme.Bold, a.th.C.Fg, t)
	})
	f.y += gtx.Dp(30)
}

// gap is the space between sections.
func (f *prefsForm) gap() { f.y += f.gtx.Dp(14) }

// note is muted 13px text, wrapped to lines (indent: under a row's label).
func (f *prefsForm) note(text string, indent bool, lines int) {
	gtx, a := f.gtx, f.p.a
	x := 0
	if indent {
		x = gtx.Dp(28)
	}
	if lines < 1 {
		lines = 1
	}
	f.record("text", "", text, false, true)
	h := gtx.Dp(18) * lines
	pc := f.p.colors()
	at(gtx, image.Pt(x, f.y), image.Pt(f.w-x, h+gtx.Dp(2)), func(gtx layout.Context) layout.Dimensions {
		return a.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Color: pc.on(a.th, pc.note), MaxLines: lines}, text)
	})
	f.y += h + gtx.Dp(10)
}

// option is a checkbox (square) or radio (round) row; do runs on click.
func (f *prefsForm) option(key, label, hint string, on, radio bool, do func()) {
	f.optionAt(key, label, hint, on, radio, true, do)
}

// optionAt is option; enabled=false greys the row and ignores clicks
// (settings Clack shows the way Slack does but can't change yet).
func (f *prefsForm) optionAt(key, label, hint string, on, radio, enabled bool, do func()) {
	gtx, a := f.gtx, f.p.a
	pc := f.p.colors()
	b := f.p.btn(key)
	kind := "check"
	if radio {
		kind = "radio"
	}
	f.record(kind, key, label, on, enabled)
	w := f.w
	// the box is the browser's (appearance:auto, 13px) with
	// .c-input_checkbox's margin-right 12px before the label
	lx := gtx.Dp(theme.NativeCheckSize + 12)
	textW := max(1, w-lx)
	labelLines := 1 + a.th.Measure(gtx, 15, theme.Regular, label)/textW // long labels wrap, like Slack's
	rowH := gtx.Dp(6) + labelLines*gtx.Dp(22)
	hintLines := 0
	if hint != "" { // Slack's c-label__subtext: 15/22 under the label, wrapped
		hintLines = 1 + a.th.Measure(gtx, 15, theme.Regular, hint)/textW
		rowH += gtx.Dp(2) + hintLines*gtx.Dp(22)
	}
	at(gtx, image.Pt(0, f.y), image.Pt(w, rowH), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, b, nil, func(gtx layout.Context) layout.Dimensions {
			fg, sub := pc.text, pc.subtext
			st := theme.NativeCheckState{Radio: radio, On: on, Hover: enabled && b.Hovered()}
			if !enabled { // Slack's .c-input_checkbox--disabled and .c-label--disabled
				st.Opacity = .38
				fg = pc.disText
			}
			// baseline-aligned with the label's first line (.c-label--inline)
			at(gtx, image.Pt(0, gtx.Dp(4)), image.Pt(gtx.Dp(theme.NativeCheckSize), gtx.Dp(theme.NativeCheckSize)), func(gtx layout.Context) layout.Dimensions {
				return theme.NativeCheck(gtx, pc.dark, st)
			})
			at(gtx, image.Pt(lx, 0), image.Pt(w-lx, labelLines*gtx.Dp(22)+gtx.Dp(2)), func(gtx layout.Context) layout.Dimensions {
				if labelLines == 1 {
					return a.th.Label(gtx, 15, theme.Regular, fg, label)
				}
				return a.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: fg, MaxLines: labelLines + 1}, label)
			})
			if hint != "" {
				at(gtx, image.Pt(lx, labelLines*gtx.Dp(22)+gtx.Dp(2)), image.Pt(w-lx, hintLines*gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
					return a.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: sub, MaxLines: hintLines + 1}, hint)
				})
			}
			return layout.Dimensions{Size: image.Pt(w, rowH)}
		})
	})
	if clicked(b) && enabled {
		do()
		a.Invalidate()
	}
	f.y += rowH + gtx.Dp(4)
}

// naNote is the note under settings Clack shows disabled.
const naNote = "Not available in Clack yet"

// subheading is a group legend (Slack's fieldset legend: 700 15px/22).
func (f *prefsForm) subheading(t string) {
	gtx, a := f.gtx, f.p.a
	f.record("legend", "", t, false, true)
	at(gtx, image.Pt(0, f.y), image.Pt(f.w, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
		return a.th.Label(gtx, 15, theme.Bold, a.th.C.Fg, t)
	})
	f.y += gtx.Dp(26)
}

// para is body text (15px/22), wrapped to the page width.
func (f *prefsForm) para(t string) {
	gtx, a := f.gtx, f.p.a
	f.record("text", "", t, false, true)
	d := at(gtx, image.Pt(0, f.y), image.Pt(f.w, gtx.Dp(400)), func(gtx layout.Context) layout.Dimensions {
		return a.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: a.th.C.Fg}, t)
	})
	f.y += d.Size.Y + gtx.Dp(10)
}

// muted is muted 13px text wrapped to the page width (indent: under a
// row's label).
func (f *prefsForm) muted(t string, indent bool) {
	gtx, a := f.gtx, f.p.a
	f.record("text", "", t, false, true)
	x := 0
	if indent {
		x = gtx.Dp(28)
	}
	pc := f.p.colors() // the dialog's p: rgba(var(--sk_foreground_max),.7)
	d := at(gtx, image.Pt(x, f.y), image.Pt(f.w-x, gtx.Dp(400)), func(gtx layout.Context) layout.Dimensions {
		return a.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Color: pc.on(a.th, pc.note)}, t)
	})
	f.y += d.Size.Y + gtx.Dp(10)
}

// na is the "Not available in Clack yet" note.
func (f *prefsForm) na() { f.muted(naNote, false) }

// check is a checkbox bound to a users.prefs boolean (def when absent),
// written with setUserPrefByApi. invert: the box shows the pref's negation
// (Slack's "Display color swatches" is !hide_hex_swatch).
func (f *prefsForm) check(name, label, hint string, def, invert bool) {
	on := f.p.prefBool(name, def) != invert
	f.option("pref."+name, label, hint, on, false, func() { f.p.setPref(name, !on != invert) })
}

// radio is a radio row bound to a users.prefs string value.
func (f *prefsForm) radio(name, value, label, hint string) {
	on := f.p.prefStr(name) == value
	f.option("pref."+name+"="+value, label, hint, on, true, func() { f.p.setPref(name, value) })
}

// off is a disabled row the way Slack shows it (on: its current state).
func (f *prefsForm) off(key, label, hint string, on, radio bool) {
	f.optionAt("na."+key, label, hint, on, radio, false, func() {})
}

// selectBox is Slack's select (a 36px box with the value and a caret);
// enabled, a click opens items as a menu at the pointer.
func (f *prefsForm) selectBox(key, label, value string, enabled bool, items []MenuItem) {
	f.selectBoxW(key, label, value, enabled, items, 300, nil)
}

// selectBoxW is selectBox wd dp wide; onOpen runs as the menu opens.
func (f *prefsForm) selectBoxW(key, label, value string, enabled bool, items []MenuItem, wd int, onOpen func()) {
	gtx, a := f.gtx, f.p.a
	pc := f.p.colors()
	if label != "" {
		f.subheading(label)
	}
	f.record("select", "sel."+key, value, false, enabled)
	b := f.p.btn("sel." + key)
	bw, bh := min(f.w, gtx.Dp(unit.Dp(wd))), gtx.Dp(36)
	// .c-select_button: base-pry over a 1px otl-pry border (no hover
	// change), its text the body colour, the caret content-pry
	fg, ic := pc.text, pc.fieldFg
	if !enabled {
		fg, ic = pc.disText, pc.disText
	}
	at(gtx, image.Pt(0, f.y), image.Pt(bw, bh), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, b, func(gtx layout.Context, s image.Point, hov bool) {
			r := image.Rectangle{Max: s}
			theme.RRect(gtx, r, gtx.Dp(8), pc.field)
			theme.Border(gtx, r, gtx.Dp(8), gtx.Dp(1), pc.on(a.th, pc.fieldBorder))
		}, func(gtx layout.Context) layout.Dimensions {
			at(gtx, image.Pt(gtx.Dp(12), 0), image.Pt(bw-gtx.Dp(40), bh), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 15, theme.Regular, fg, value)
				})
			})
			at(gtx, image.Pt(bw-gtx.Dp(28), (bh-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
				return icons.Get("caret-down")(gtx, 16, ic)
			})
			return layout.Dimensions{Size: image.Pt(bw, bh)}
		})
	})
	if clicked(b) && enabled && len(items) > 0 {
		if onOpen != nil {
			onOpen()
		}
		a.menu(a.anchor(image.Point{}), items)
	}
	f.y += bh + gtx.Dp(12)
}

// listRow is a 36px row: an avatar, a name and an action link on the
// right (VIP list "Remove").
func (f *prefsForm) listRow(key, avatar, name, action string, do func()) {
	gtx, a := f.gtx, f.p.a
	c := &a.th.C
	pc := f.p.colors()
	f.record("list", "row."+key, name, false, action != "")
	rh := gtx.Dp(36)
	s := gtx.Dp(24)
	at(gtx, image.Pt(0, f.y+(rh-s)/2), image.Pt(s, s), func(gtx layout.Context) layout.Dimensions {
		return a.avatar(gtx, avatar, name, s, gtx.Dp(6))
	})
	at(gtx, image.Pt(s+gtx.Dp(10), 0).Add(image.Pt(0, f.y)), image.Pt(f.w-gtx.Dp(120), rh), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 15, theme.Regular, c.Fg, name)
		})
	})
	if action != "" {
		b := f.p.btn("row." + key)
		aw := a.th.Measure(gtx, 13, theme.Bold, action) + gtx.Dp(16)
		at(gtx, image.Pt(f.w-aw, f.y+(rh-gtx.Dp(28))/2), image.Pt(aw, gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, b, func(gtx layout.Context, s image.Point, hov bool) {
				r := image.Rectangle{Max: s}
				// .c-button--outline: base-pry, otl-sec border, base-sec on hover
				fill := pc.field
				if hov {
					fill = pc.outlineHover
				}
				theme.RRect(gtx, r, gtx.Dp(6), fill)
				theme.Border(gtx, r, gtx.Dp(6), gtx.Dp(1), pc.on(a.th, pc.outline))
			}, func(gtx layout.Context) layout.Dimensions {
				sz := image.Pt(aw, gtx.Dp(28))
				at(gtx, image.Pt(gtx.Dp(8), 0), sz, func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
						return a.th.Label(gtx, 13, theme.Bold, pc.fieldFg, action)
					})
				})
				return layout.Dimensions{Size: sz}
			})
		})
		if clicked(b) {
			do()
			a.Invalidate()
		}
	}
	f.y += rh + gtx.Dp(4)
}

// listRowIcon is a 36px row (an avatar when given, the name) with
// Slack's times-circle icon button at the right (aria-label label):
// Privacy & visibility's blocked invitations and hidden people.
func (f *prefsForm) listRowIcon(key, avatar, name, label string, do func()) {
	gtx, a := f.gtx, f.p.a
	c := &a.th.C
	f.record("list", "row."+key, name+" · "+label, false, true)
	rh := gtx.Dp(36)
	x := 0
	if avatar != "" {
		s := gtx.Dp(24)
		at(gtx, image.Pt(0, f.y+(rh-s)/2), image.Pt(s, s), func(gtx layout.Context) layout.Dimensions {
			return a.avatar(gtx, avatar, name, s, gtx.Dp(6))
		})
		x = s + gtx.Dp(10)
	}
	at(gtx, image.Pt(x, f.y), image.Pt(f.w-x-gtx.Dp(40), rh), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 15, theme.Bold, c.Fg, name)
		})
	})
	b := f.p.btn("row." + key)
	at(gtx, image.Pt(f.w-gtx.Dp(28), f.y+(rh-gtx.Dp(24))/2), image.Pt(gtx.Dp(24), gtx.Dp(24)), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, b, "x-circle", 24, 18, c.FgMuted, c.BgHover, 6)
	})
	if clicked(b) && do != nil {
		do()
		a.Invalidate()
	}
	f.y += rh + gtx.Dp(4)
}

// notifyCheck is a checkbox bound to a global notification pref
// (all_notifications_prefs.global, def when absent), written with
// users.prefs.setNotifications like Slack's NotificationsGlobalPrefCheckbox.
func (f *prefsForm) notifyCheck(name, label, hint string, def bool) {
	on := f.p.notifyBool(name, def)
	f.option("notify."+name, label, hint, on, false, func() { f.p.setNotify(name, !on) })
}

// hr is Slack's <hr> between a page's groups.
func (f *prefsForm) hr() {
	gtx, a := f.gtx, f.p.a
	f.y += gtx.Dp(10)
	pc := f.p.colors() // hr: rgb(var(--sk_foreground_low_solid))
	theme.Fill(gtx, image.Rect(0, f.y, f.w, f.y+max(1, gtx.Dp(1))), pc.on(a.th, pc.hr))
	f.y += gtx.Dp(22)
}

// prefsOpenURL opens Preferences links in the browser (tests swap it).
var prefsOpenURL = openURL

// slackHelp is a Slack Help Center article.
func slackHelp(id string) string { return "https://slack.com/help/articles/" + id }

// link is a 15px link on its own line (Slack's "Learn more", "Get help").
func (f *prefsForm) link(key, label, url string) {
	gtx, a := f.gtx, f.p.a
	f.record("link", "link."+key, label, false, true)
	b := f.p.btn("link." + key)
	w := a.th.Measure(gtx, 15, theme.Regular, label)
	at(gtx, image.Pt(0, f.y), image.Pt(w, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, b, nil, func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 15, theme.Regular, f.p.colors().link, label)
		})
	})
	if clicked(b) {
		prefsOpenURL(url)
	}
	f.y += gtx.Dp(28)
}

// headingLink is a page heading with a help link at its right (Slack's
// "Messaging defaults" … "About notifications").
func (f *prefsForm) headingLink(t, label, url string) {
	gtx, a := f.gtx, f.p.a
	f.record("h2", "", t, false, true)
	at(gtx, image.Pt(0, f.y), image.Pt(f.w, gtx.Dp(24)), func(gtx layout.Context) layout.Dimensions {
		return a.th.Label(gtx, 18, theme.Bold, a.th.C.Fg, t)
	})
	f.record("link", "link."+label, label, false, true)
	b := f.p.btn("link." + label)
	w := a.th.Measure(gtx, 15, theme.Regular, label)
	at(gtx, image.Pt(f.w-w, f.y+gtx.Dp(2)), image.Pt(w, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, b, nil, func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 15, theme.Regular, f.p.colors().link, label)
		})
	})
	if clicked(b) {
		prefsOpenURL(url)
	}
	f.y += gtx.Dp(30)
}

// button is Slack's outline button (36px, 700 15px); disabled it greys
// and ignores clicks.
func (f *prefsForm) button(key, label string, enabled bool, do func()) {
	f.buttons(prefsButton{key, label, enabled, do})
}

// prefsButton is one button of a row.
type prefsButton struct {
	key, label string
	enabled    bool
	do         func()
}

// buttons draws outline buttons side by side, 8px apart (Slack's Theme
// colors Share / Import / Surprise me).
func (f *prefsForm) buttons(bs ...prefsButton) {
	gtx, a := f.gtx, f.p.a
	pc := f.p.colors()
	bh := gtx.Dp(36)
	x := 0
	for _, pb := range bs {
		pb := pb
		f.record("button", "button."+pb.key, pb.label, false, pb.enabled)
		b := f.p.btn("button." + pb.key)
		bw := a.th.Measure(gtx, 15, theme.Bold, pb.label) + gtx.Dp(32)
		fg := pc.fieldFg // .c-button--outline: content-pry
		if !pb.enabled {
			fg = pc.disText
		}
		at(gtx, image.Pt(x, f.y), image.Pt(bw, bh), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, b, func(gtx layout.Context, s image.Point, hov bool) {
				r := image.Rectangle{Max: s}
				fill := pc.field
				if hov && pb.enabled {
					fill = pc.outlineHover
				}
				theme.RRect(gtx, r, gtx.Dp(8), fill)
				theme.Border(gtx, r, gtx.Dp(8), gtx.Dp(1), pc.on(a.th, pc.outline))
			}, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Exact(image.Pt(bw, bh))
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 15, theme.Bold, fg, pb.label)
				})
			})
		})
		if clicked(b) && pb.enabled && pb.do != nil {
			pb.do()
			a.Invalidate()
		}
		x += bw + gtx.Dp(8)
	}
	f.y += bh + gtx.Dp(12)
}

// textBox is a read-only stand-in for one of Slack's text fields (Channel
// keywords): the value in a bordered box, greyed.
func (f *prefsForm) textBox(key, value, placeholder string) {
	gtx, a := f.gtx, f.p.a
	pc := f.p.colors()
	f.record("select", "text."+key, value, false, false)
	bh := gtx.Dp(36)
	at(gtx, image.Pt(0, f.y), image.Pt(f.w, bh), func(gtx layout.Context) layout.Dimensions {
		r := image.Rectangle{Max: image.Pt(f.w, bh)}
		theme.RRect(gtx, r, gtx.Dp(8), pc.field) // .c-input_text: base-pry, otl-pry
		theme.Border(gtx, r, gtx.Dp(8), gtx.Dp(1), pc.on(a.th, pc.fieldBorder))
		t, col := value, pc.disText
		if t == "" {
			t = placeholder
		}
		at(gtx, image.Pt(gtx.Dp(12), 0), image.Pt(f.w-gtx.Dp(24), bh), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
				return a.th.Label(gtx, 15, theme.Regular, col, t)
			})
		})
		return layout.Dimensions{Size: r.Max}
	})
	f.y += bh + gtx.Dp(8)
}

// linkDo is link with an action in Clack (another tab, a dialog).
func (f *prefsForm) linkDo(key, label string, do func()) {
	gtx, a := f.gtx, f.p.a
	f.record("link", "link."+key, label, false, true)
	b := f.p.btn("link." + key)
	w := a.th.Measure(gtx, 15, theme.Regular, label)
	at(gtx, image.Pt(0, f.y), image.Pt(w, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, b, nil, func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 15, theme.Regular, f.p.colors().link, label)
		})
	})
	if clicked(b) && do != nil {
		do()
		a.Invalidate()
	}
	f.y += gtx.Dp(28)
}

// radio2 is a radio row bound to a boolean users.prefs value (Slack's
// value="true"/"false" radios: msg_input_send_btn).
func (f *prefsForm) radio2(name string, value bool, label, hint string) {
	on := f.p.prefBool(name, false) == value
	f.option("pref."+name+"="+strconv.FormatBool(value), label, hint, on, true, func() { f.p.setPref(name, value) })
}
