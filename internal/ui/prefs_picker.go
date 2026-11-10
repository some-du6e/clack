package ui

import (
	"image"
	"sort"
	"strings"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Advanced > "Exclude these channels from search results:" is Slack's
// channel multi-select (search_exclude_channels_picker, i8.h): the chosen
// channels as removable tokens, then a "Type a channel name…" field whose
// matches drop down under it. onSelectedItemsChange writes
// search_exclude_channels as the ids joined by "," (SearchPref _); the
// selected options are the pref's ids that resolve to channels (i6.uz).

type channelPicker struct {
	ed widget.Editor
}

// pickerOption is a channel the field offers.
type pickerOption struct{ id, name string }

// excludeOptions are the pref's channels, as tokens.
func (p *prefsModal) excludeOptions() []pickerOption {
	m := p.a.model()
	var out []pickerOption
	for _, id := range core.SearchExcludedChannels(p.prefStr("search_exclude_channels")) {
		if c := m.conv(id); c != nil && c.Kind != "im" && c.Kind != "mpim" && c.Kind != "app" {
			out = append(out, pickerOption{id, c.Name})
		}
	}
	return out
}

func (p *prefsModal) setExcluded(opts []pickerOption) {
	ids := make([]string, len(opts))
	for i, o := range opts {
		ids[i] = o.id
	}
	p.setPref("search_exclude_channels", strings.Join(ids, ","))
}

// pickerMatches are the channels whose name contains q (prefix matches
// first), not already chosen, at most 8.
func (p *prefsModal) pickerMatches(q string, chosen []pickerOption) []pickerOption {
	q = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(q), "#"))
	if q == "" {
		return nil
	}
	in := map[string]bool{}
	for _, o := range chosen {
		in[o.id] = true
	}
	var out []pickerOption
	for id, c := range p.a.model().Convs {
		if in[id] || c.Kind == "im" || c.Kind == "mpim" || c.Kind == "app" || c.Name == "" {
			continue
		}
		if strings.Contains(strings.ToLower(c.Name), q) {
			out = append(out, pickerOption{id, c.Name})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		pi, pj := strings.HasPrefix(strings.ToLower(out[i].name), q), strings.HasPrefix(strings.ToLower(out[j].name), q)
		if pi != pj {
			return pi
		}
		return out[i].name < out[j].name
	})
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

// channelPicker draws the field, its tokens and its matches.
func (f *prefsForm) channelPicker(label string) {
	gtx, p, a := f.gtx, f.p, f.p.a
	c := &a.th.C
	pk := &p.picker
	pk.ed.SingleLine = true
	chosen := p.excludeOptions()
	f.subheading(label)
	for {
		ev, ok := gtx.Event(key.Filter{Focus: &pk.ed, Name: key.NameDeleteBackward})
		if !ok {
			break
		}
		if e, ok := ev.(key.Event); ok && e.State == key.Press && pk.ed.Text() == "" && len(chosen) > 0 {
			p.setExcluded(chosen[:len(chosen)-1]) // Backspace in the empty field drops the last token
			chosen = chosen[:len(chosen)-1]
		}
	}
	for {
		ev, ok := pk.ed.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			if ms := p.pickerMatches(pk.ed.Text(), chosen); len(ms) > 0 {
				p.setExcluded(append(chosen, ms[0]))
				pk.ed.SetText("")
			}
		}
	}
	pk.ed.Submit = true
	f.record("select", "picker.search_exclude_channels", strings.Join(names(chosen), ", "), false, true)

	// tokens wrap inside the box; the field takes the rest of the last line
	pad, th := gtx.Dp(6), gtx.Dp(26)
	x, y := pad, pad
	type placed struct {
		o pickerOption
		r image.Rectangle
	}
	var toks []placed
	for _, o := range chosen {
		w := a.th.Measure(gtx, 15, theme.Regular, "#"+o.name) + gtx.Dp(36)
		if x+w > f.w-pad && x > pad {
			x, y = pad, y+th+gtx.Dp(4)
		}
		toks = append(toks, placed{o, image.Rect(x, y, x+w, y+th)})
		x += w + gtx.Dp(4)
	}
	if f.w-pad-x < gtx.Dp(160) && x > pad {
		x, y = pad, y+th+gtx.Dp(4)
	}
	boxH := y + th + pad
	focused := gtx.Focused(&pk.ed)
	at(gtx, image.Pt(0, f.y), image.Pt(f.w, boxH), func(gtx layout.Context) layout.Dimensions {
		r := image.Rectangle{Max: image.Pt(f.w, boxH)}
		pc := p.colors() // an input: base-pry, otl-pry; :focus otl-hgl-1
		theme.RRect(gtx, r, gtx.Dp(8), pc.field)
		bc := pc.on(a.th, pc.fieldBorder)
		if focused {
			bc = pc.focus
		}
		theme.Border(gtx, r, gtx.Dp(8), gtx.Dp(1), bc)
		for _, t := range toks {
			t := t
			theme.RRect(gtx, t.r, gtx.Dp(4), c.BgHover)
			at(gtx, t.r.Min.Add(image.Pt(gtx.Dp(8), 0)), image.Pt(t.r.Dx(), th), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, th, func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 15, theme.Regular, c.Fg, "#"+t.o.name)
				})
			})
			rm := p.btn("picker.remove." + t.o.id)
			at(gtx, image.Pt(t.r.Max.X-gtx.Dp(24), t.r.Min.Y+(th-gtx.Dp(20))/2), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
				return iconButton(gtx, rm, "close", 20, 14, c.FgMuted, c.BgHover, 4)
			})
			if clicked(rm) {
				var keep []pickerOption
				for _, o := range chosen {
					if o.id != t.o.id {
						keep = append(keep, o)
					}
				}
				p.setExcluded(keep)
			}
		}
		at(gtx, image.Pt(x+gtx.Dp(4), y), image.Pt(f.w-pad-x-gtx.Dp(4), th), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, th, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				if pk.ed.Len() == 0 && len(chosen) == 0 {
					a.th.Label(gtx, 15, theme.Regular, p.colors().subtext, "Type a channel name…")
				}
				return a.th.Editor(gtx, &pk.ed, 15, c.Fg, c.Link)
			})
		})
		return layout.Dimensions{Size: r.Max}
	})
	f.y += boxH + gtx.Dp(4)

	// matches
	for _, o := range p.pickerMatches(pk.ed.Text(), chosen) {
		o := o
		b := p.btn("picker.add." + o.id)
		f.record("list", "picker.add."+o.id, "#"+o.name, false, true)
		rh := gtx.Dp(32)
		at(gtx, image.Pt(0, f.y), image.Pt(f.w, rh), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, b, func(gtx layout.Context, s image.Point, hov bool) {
				if hov {
					theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(6), c.BgHover)
				}
			}, func(gtx layout.Context) layout.Dimensions {
				at(gtx, image.Pt(gtx.Dp(10), (rh-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get("channel")(gtx, 16, c.FgMuted)
				})
				at(gtx, image.Pt(gtx.Dp(34), 0), image.Pt(f.w-gtx.Dp(40), rh), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions {
						return a.th.Label(gtx, 15, theme.Regular, c.Fg, o.name)
					})
				})
				return layout.Dimensions{Size: image.Pt(f.w, rh)}
			})
		})
		if clicked(b) {
			p.setExcluded(append(chosen, o))
			pk.ed.SetText("")
			gtx.Execute(key.FocusCmd{Tag: &pk.ed})
		}
		f.y += rh
	}
	f.y += gtx.Dp(10)
}

func names(os []pickerOption) []string {
	out := make([]string, len(os))
	for i, o := range os {
		out[i] = "#" + o.name
	}
	return out
}
