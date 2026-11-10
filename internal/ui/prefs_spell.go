package ui

import (
	"image"
	"strings"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/spell"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Language & region > Spellcheck is Slack's SpellcheckPrefs
// (gantry-v2-shared-boot-async): the "Enable spellcheck on your messages
// and canvases" checkbox (webapp_spellcheck), then on the desktop app
// renderSpellcheckLanguagesV3. On macOS (spellCheckerV3
// isLanguageSelectionEnabled is !darwin) that's the hint "We spellcheck
// the languages specified in your system settings…"; on Windows and Linux
// a large, full-width multi-select of getAvailableLanguages (labelled,
// sorted by label) holding getCurrentLanguages, greyed while the box is
// off, whose changes go to setCurrentLanguages, with "Choose the languages
// you’d like Slack to spellcheck as you type." under it.

const (
	spellMacHint  = "We spellcheck the languages specified in your system settings. You can find those under System Preferences > Keyboard > Text."
	spellLangDesc = "Choose the languages you’d like Slack to spellcheck as you type."
)

// spellMac picks the macOS rows (tests set it).
var spellMac = spell.SystemLanguages

type langPicker struct {
	ed widget.Editor
}

func (p *prefsModal) spellcheckPrefs(f *prefsForm) {
	f.subheading("Spellcheck")
	on := p.prefBool("webapp_spellcheck", true)
	if s := spell.Default(); s != nil {
		s.SetEnabled(on) // follows the box at once, before users.prefs answers
	}
	f.check("webapp_spellcheck", "Enable spellcheck on your messages and canvases", "", true, false)
	if spellMac {
		f.para(spellMacHint)
		return
	}
	f.langPicker(on)
	f.para(spellLangDesc)
}

// langPicker draws the languages as removable tokens in a field; typing
// filters the other languages under it.
func (f *prefsForm) langPicker(enabled bool) {
	gtx, p, a := f.gtx, f.p, f.p.a
	c := &a.th.C
	pk := &p.langs
	pk.ed.SingleLine = true
	chosen := a.spellLanguages()
	label := func(code string) string {
		if l, ok := spell.Labels[code]; ok {
			return l
		}
		return code
	}
	set := func(langs []string) { a.setSpellLanguages(langs) }
	if enabled {
		for {
			ev, ok := gtx.Event(key.Filter{Focus: &pk.ed, Name: key.NameDeleteBackward})
			if !ok {
				break
			}
			if e, ok := ev.(key.Event); ok && e.State == key.Press && pk.ed.Text() == "" && len(chosen) > 0 {
				chosen = chosen[:len(chosen)-1]
				set(chosen)
			}
		}
		for {
			ev, ok := pk.ed.Update(gtx)
			if !ok {
				break
			}
			if _, ok := ev.(widget.SubmitEvent); ok {
				if ms := langMatches(pk.ed.Text(), chosen); len(ms) > 0 {
					chosen = append(chosen, ms[0][0])
					set(chosen)
					pk.ed.SetText("")
				}
			}
		}
	}
	pk.ed.Submit = true
	var labels []string
	for _, l := range chosen {
		labels = append(labels, label(l))
	}
	f.record("select", "picker.spellcheck_languages", strings.Join(labels, ", "), false, enabled)

	pad, th := gtx.Dp(6), gtx.Dp(26)
	x, y := pad, pad
	type placed struct {
		code string
		r    image.Rectangle
	}
	var toks []placed
	for _, l := range chosen {
		w := a.th.Measure(gtx, 15, theme.Regular, label(l)) + gtx.Dp(36)
		w = min(w, f.w-2*pad)
		if x+w > f.w-pad && x > pad {
			x, y = pad, y+th+gtx.Dp(4)
		}
		toks = append(toks, placed{l, image.Rect(x, y, x+w, y+th)})
		x += w + gtx.Dp(4)
	}
	if f.w-pad-x < gtx.Dp(160) && x > pad {
		x, y = pad, y+th+gtx.Dp(4)
	}
	boxH := max(y+th+pad, gtx.Dp(44)) // size "large"
	focused := enabled && gtx.Focused(&pk.ed)
	fg := c.Fg
	if !enabled {
		fg = p.colors().subtext
	}
	at(gtx, image.Pt(0, f.y), image.Pt(f.w, boxH), func(gtx layout.Context) layout.Dimensions {
		r := image.Rectangle{Max: image.Pt(f.w, boxH)}
		pc := p.colors()
		theme.RRect(gtx, r, gtx.Dp(8), pc.field)
		bc := pc.on(a.th, pc.fieldBorder)
		if focused {
			bc = pc.focus
		}
		theme.Border(gtx, r, gtx.Dp(8), gtx.Dp(1), bc)
		for _, t := range toks {
			t := t
			theme.RRect(gtx, t.r, gtx.Dp(4), c.BgHover)
			at(gtx, t.r.Min.Add(image.Pt(gtx.Dp(8), 0)), image.Pt(t.r.Dx()-gtx.Dp(32), th), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, th, func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 15, theme.Regular, fg, label(t.code))
				})
			})
			if !enabled {
				continue
			}
			rm := p.btn("picker.spell.remove." + t.code)
			at(gtx, image.Pt(t.r.Max.X-gtx.Dp(24), t.r.Min.Y+(th-gtx.Dp(20))/2), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
				return iconButton(gtx, rm, "close", 20, 14, c.FgMuted, c.BgHover, 4)
			})
			if clicked(rm) {
				var keep []string
				for _, l := range chosen {
					if l != t.code {
						keep = append(keep, l)
					}
				}
				set(keep)
			}
		}
		if enabled {
			at(gtx, image.Pt(x+gtx.Dp(4), y), image.Pt(f.w-pad-x-gtx.Dp(4), th), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, th, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return a.th.Editor(gtx, &pk.ed, 15, c.Fg, c.Link)
				})
			})
		}
		return layout.Dimensions{Size: r.Max}
	})
	f.y += boxH + gtx.Dp(4)
	if !focused {
		f.y += gtx.Dp(6)
		return
	}
	for _, o := range langMatches(pk.ed.Text(), chosen) {
		o := o
		b := p.btn("picker.spell.add." + o[0])
		f.record("list", "picker.spell.add."+o[0], o[1], false, true)
		rh := gtx.Dp(32)
		at(gtx, image.Pt(0, f.y), image.Pt(f.w, rh), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, b, func(gtx layout.Context, s image.Point, hov bool) {
				if hov {
					theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(6), c.BgHover)
				}
			}, func(gtx layout.Context) layout.Dimensions {
				at(gtx, image.Pt(gtx.Dp(12), 0), image.Pt(f.w-gtx.Dp(24), rh), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions {
						return a.th.Label(gtx, 15, theme.Regular, c.Fg, o[1])
					})
				})
				return layout.Dimensions{Size: image.Pt(f.w, rh)}
			})
		})
		if clicked(b) {
			set(append(chosen, o[0]))
			pk.ed.SetText("")
			gtx.Execute(key.FocusCmd{Tag: &pk.ed})
		}
		f.y += rh
	}
	f.y += gtx.Dp(10)
}

// langMatches are the unchosen languages whose label contains q (all of
// them for an empty q, in label order), at most 8.
func langMatches(q string, chosen []string) [][2]string {
	q = strings.ToLower(strings.TrimSpace(q))
	in := map[string]bool{}
	for _, l := range chosen {
		in[l] = true
	}
	var out [][2]string
	for _, o := range spell.Available() {
		if in[o[0]] {
			continue
		}
		if q == "" || strings.Contains(strings.ToLower(o[1]), q) {
			out = append(out, o)
		}
		if len(out) == 8 {
			break
		}
	}
	return out
}
