package ui

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/pluginlib"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Taut's settings screens, drawn natively from taut.Settings with the
// metrics of Taut's own settings tab (its README and source). Clack shows
// Taut and Slick as one plugin system (plugins_prefs.go): a Taut plugin's
// row in Preferences > Plugins opens its page here (description, meta
// line, "Turn on", errors, Settings with one control per option kind, a
// reset button, React editors through Bridge.RenderEditor, and Data: the
// plugin's data panel through Bridge.RenderDataPanel, clear data/cache,
// update code, delete), and Preferences > Plugin settings opens the custom
// CSS box, config.json and About.

type tautPage struct {
	a       *App
	screen  string // main (none: the list is Preferences > Plugins) | plugin | appearance | advanced | about
	plugin  string
	list    widget.List
	clicks  map[string]*widget.Clickable
	eds     map[string]*widget.Editor
	subs    map[string]*slackc.Sub
	handles map[string]*taut.GoHandle
	subGen  int // App.tautGen the subs were mounted for

	// async state
	busy    string
	errText string
	errFor  string // the run errText came from
	note    string

	urlEd    widget.Editor
	cssEd    widget.Editor
	cssInit  bool
	cssSaved string // user.css as last loaded or saved (dirty = editor differs)
	cfgEd    widget.Editor
	cfgInit  bool
	cfgSaved string
	cfgErr   string // config.json parse error (Not saved: …)
	jsonEdit bool
	jsonEd   widget.Editor
	replace  bool
	reveal   map[string]bool
	text     prefsText // mrkdwn lines (meta, credits) through richview

	gtxNow   layout.Context // this frame's context (editor events)
	focusWas map[*widget.Editor]bool
}

func newTautPage(a *App) *tautPage {
	p := &tautPage{a: a, screen: "main", clicks: map[string]*widget.Clickable{},
		eds: map[string]*widget.Editor{}, subs: map[string]*slackc.Sub{}, handles: map[string]*taut.GoHandle{},
		reveal: map[string]bool{}, focusWas: map[*widget.Editor]bool{}}
	p.urlEd.SingleLine = true
	p.urlEd.Submit = true
	p.list.Axis = layout.Vertical
	return p
}

// close unmounts every JS root the page mounted.
func (p *tautPage) close() {
	for k, s := range p.subs {
		s.Close()
		delete(p.subs, k)
	}
	for k, h := range p.handles {
		h.Release()
		delete(p.handles, k)
	}
}

func (p *tautPage) btn(k string) *widget.Clickable {
	b := p.clicks[k]
	if b == nil {
		b = new(widget.Clickable)
		p.clicks[k] = b
	}
	return b
}

func (p *tautPage) ed(k string, single bool) *widget.Editor {
	e := p.eds[k]
	if e == nil {
		e = &widget.Editor{SingleLine: single, Submit: single}
		p.eds[k] = e
	}
	return e
}

func (p *tautPage) go_(screen, plugin string) {
	if screen != p.screen || plugin != p.plugin {
		p.close() // editors/panels belong to the screen they were on
		p.errText, p.note, p.replace, p.jsonEdit = "", "", false, false
	}
	p.screen, p.plugin = screen, plugin
	p.list.Position = layout.Position{}
	p.a.Invalidate()
}

// run does a Taut action off the UI goroutine and shows its error.
func (p *tautPage) run(busy string, f func(ctx context.Context, t *taut.Taut) error, done func()) {
	t := p.a.Taut()
	if t == nil || p.busy != "" {
		return
	}
	p.busy, p.errText, p.errFor = busy, "", busy
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		err := f(ctx, t)
		p.a.Post(func() {
			p.busy = ""
			if err != nil {
				p.errText = err.Error()
			} else if done != nil {
				done()
			}
			p.a.Invalidate()
		})
	}()
}

// ---- colors and drawing primitives (Slack's measured values) ----

type tpColors struct {
	fg, fg2, fg3, link, err, warn, border, borderHover, check, checkBorder, bg, hover, pillBg, pillFg, focus, codeBg color.NRGBA
}

func (p *tautPage) colors() tpColors {
	c := &p.a.th.C
	if p.a.th.Dark {
		return tpColors{fg: c.Fg, fg2: rgbT(0xababad), fg3: rgbT(0x9a9b9e), link: rgbT(0x1d9bd1), err: rgbT(0xe01e5a), warn: rgbT(0xe8912d),
			border: p.a.th.OnBg(rgbaT(0xd1d2d3, .3)), borderHover: p.a.th.OnBg(rgbaT(0xd1d2d3, .5)), check: rgbT(0x1d9bd1),
			checkBorder: p.a.th.OnBg(rgbaT(0xd1d2d3, .5)), bg: c.Bg, hover: p.a.th.OnBg(rgbaT(0xd1d2d3, .04)),
			pillBg: p.a.th.OnBg(rgbaT(0x1d9bd1, .15)), pillFg: rgbT(0x1d9bd1), focus: rgbT(0x1d9bd1), codeBg: p.a.th.OnBg(rgbaT(0xd1d2d3, .04))}
	}
	return tpColors{fg: c.Fg, fg2: rgbT(0x454447), fg3: rgbT(0x5e5d60), link: rgbT(0x1264a3), err: rgbT(0xe01e5a), warn: rgbT(0xe8912d),
		border: p.a.th.OnBg(rgbaT(0x1d1c1d, .3)), borderHover: p.a.th.OnBg(rgbaT(0x1d1c1d, .5)), check: rgbT(0x1264a3),
		checkBorder: p.a.th.OnBg(rgbaT(0x1d1c1d, .5)), bg: c.Bg, hover: p.a.th.OnBg(rgbaT(0x1d1c1d, .04)),
		pillBg: p.a.th.OnBg(rgbaT(0x1d9bd1, .1)), pillFg: rgbT(0x1264a3), focus: rgbT(0x1264a3), codeBg: p.a.th.OnBg(rgbaT(0x1d1c1d, .04))}
}

func rgbT(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 255}
}
func rgbaT(v uint32, a float32) color.NRGBA {
	c := rgbT(v)
	c.A = uint8(a*255 + .5)
	return c
}

// flow is a top-to-bottom cursor over the page width.
type flow struct {
	gtx layout.Context
	w   int
	y   int
}

func (f *flow) gap(dp unit.Dp) { f.y += f.gtx.Dp(dp) }

// put lays out w at the cursor and advances by its height.
func (f *flow) put(x int, w layout.Widget) layout.Dimensions {
	d := at(f.gtx, image.Pt(x, f.y), image.Pt(f.w-x, 1<<20), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Point{}
		return w(gtx)
	})
	f.y += d.Size.Y
	return d
}

func (p *tautPage) label(gtx layout.Context, size unit.Sp, w font.Weight, col color.NRGBA, s string) layout.Dimensions {
	return p.a.th.Label(gtx, size, w, col, s)
}

func (p *tautPage) para(gtx layout.Context, size, lh unit.Sp, col color.NRGBA, s string) layout.Dimensions {
	return p.a.th.Layout(gtx, theme.TextStyle{Size: size, LineHeight: lh, Color: col, Wrap: text.WrapWords}, s)
}

// linkButton is Taut's LinkButton: link-colored text, underlined on hover.
func (p *tautPage) linkButton(gtx layout.Context, k, label, icon string) (layout.Dimensions, bool) {
	return p.linkButtonW(gtx, k, label, icon, theme.Regular, 4) // taut-settings__back: gap 4
}

// linkButtonW is linkButton with a weight and an icon gap (Slick's back
// button is bold with a 6px gap).
func (p *tautPage) linkButtonW(gtx layout.Context, k, label, icon string, weight font.Weight, gap unit.Dp) (layout.Dimensions, bool) {
	cl := p.colors()
	b := p.btn(k)
	d := clickArea(gtx, b, nil, func(gtx layout.Context) layout.Dimensions {
		x := 0
		if icon != "" {
			at(gtx, image.Pt(0, gtx.Dp(3)), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
				return icons.Get(icon)(gtx, 16, cl.link)
			})
			x = gtx.Dp(16 + gap)
		}
		ld := at(gtx, image.Pt(x, 0), image.Pt(1<<20, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
			return p.label(gtx, 15, weight, cl.link, label)
		})
		if b.Hovered() {
			theme.Fill(gtx, image.Rect(x, ld.Size.Y-gtx.Dp(3), x+ld.Size.X, ld.Size.Y-gtx.Dp(2)), cl.link)
		}
		return layout.Dimensions{Size: image.Pt(x+ld.Size.X, gtx.Dp(22))}
	})
	return d, clicked(b)
}

// checkbox is Slack's 16px checkbox; reports a toggle.
func (p *tautPage) checkbox(gtx layout.Context, k string, on bool) (layout.Dimensions, bool) {
	cl := p.colors()
	b := p.btn(k)
	s := gtx.Dp(16)
	d := clickArea(gtx, b, nil, func(gtx layout.Context) layout.Dimensions {
		r := image.Rect(0, 0, s, s)
		rad := gtx.Dp(4)
		if on {
			fill := cl.check
			if b.Hovered() {
				fill = theme.Mix(fill, rgbT(0), .1)
			}
			theme.RRect(gtx, r, rad, fill)
			at(gtx, image.Pt(gtx.Dp(1), gtx.Dp(1)), image.Pt(gtx.Dp(14), gtx.Dp(14)), func(gtx layout.Context) layout.Dimensions {
				return icons.Get("check")(gtx, 14, rgbT(0xffffff))
			})
		} else {
			bc := cl.checkBorder
			if b.Hovered() {
				bc = cl.fg2
			}
			theme.RRect(gtx, r, rad, cl.bg)
			theme.Border(gtx, r, rad, gtx.Dp(1), bc)
		}
		return layout.Dimensions{Size: image.Pt(s, s)}
	})
	return d, clicked(b)
}

// button is a Slack button (popups.Button: outline/primary/danger, small 28px).
func (p *tautPage) button(gtx layout.Context, k, label string, kind popups.BtnKind) (layout.Dimensions, bool) {
	b := p.btn(k)
	disabled := p.busy != ""
	if disabled {
		kind = popups.BtnDisabled
	}
	hit := b.Clicked(gtx) // popups.Button lays the Clickable out itself
	d := popups.Button(gtx, p.a.th, b, kind, popups.BtnSmall, label, "", 0)
	return d, hit && !disabled
}

// input draws Slack's c-input_text around an editor (36px, or the given
// height for multi-line boxes); mono uses a code font size.
func (p *tautPage) input(gtx layout.Context, e *widget.Editor, w, h int, placeholder string, mono bool) layout.Dimensions {
	cl := p.colors()
	r := image.Rect(0, 0, w, h)
	rad := gtx.Dp(8)
	theme.RRect(gtx, r, rad, cl.bg)
	bc := cl.border
	if gtx.Focused(e) {
		bc = cl.focus
		theme.Border(gtx, r.Inset(-gtx.Dp(3)), rad+gtx.Dp(3), gtx.Dp(3), theme.WithAlpha(cl.focus, 0x30))
	}
	theme.Border(gtx, r, rad, gtx.Dp(1), bc)
	size := unit.Sp(15)
	if mono {
		size = 12
	}
	padX, padY := gtx.Dp(12), gtx.Dp(8)
	if e.SingleLine {
		padY = (h - gtx.Sp(size)*4/3) / 2
	}
	at(gtx, image.Pt(padX, padY), image.Pt(w-2*padX, h-2*padY), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		if e.Len() == 0 && placeholder != "" {
			p.label(gtx, size, theme.Regular, p.a.th.C.FgSubtle, placeholder)
		}
		return p.a.th.Editor(gtx, e, size, cl.fg, p.a.th.C.Selection)
	})
	return layout.Dimensions{Size: r.Max}
}

// alert is Slack's InlineAlert: warning icon and red 13px text.
func (p *tautPage) alert(gtx layout.Context, s string) layout.Dimensions {
	cl := p.colors()
	w := gtx.Constraints.Max.X
	at(gtx, image.Pt(0, gtx.Dp(1)), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
		return icons.Get("warning")(gtx, 16, cl.err)
	})
	d := at(gtx, image.Pt(gtx.Dp(22), 0), image.Pt(w-gtx.Dp(22), 1<<20), func(gtx layout.Context) layout.Dimensions {
		return p.para(gtx, 13, 18, cl.err, s)
	})
	return layout.Dimensions{Size: image.Pt(w, max(d.Size.Y, gtx.Dp(18)))}
}

// legend is a FieldSet legend (Slack: 15/700, 8px below).
func (p *tautPage) legend(f *flow, s string) {
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		return p.label(gtx, 15, theme.Bold, p.colors().fg, s)
	})
	f.gap(8)
}

func (p *tautPage) hr(f *flow) {
	f.gap(20)
	theme.Fill(f.gtx, image.Rect(0, f.y, f.w, f.y+f.gtx.Dp(1)), p.a.th.C.Border)
	f.gap(21)
}

func (p *tautPage) muted(f *flow, s string) {
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		return p.para(gtx, 15, 22, p.colors().fg2, s)
	})
}

// ---- page entry ----

// layout draws the current screen inside the Preferences panel: a
// plugin's page (under Plugins) or custom CSS, config.json or About
// (under Plugin settings).
func (p *tautPage) layout(gtx layout.Context) layout.Dimensions {
	if p.subGen != p.a.tautGen { // a fresh plugin runtime: remount
		p.close()
		p.subGen = p.a.tautGen
	}
	sz := gtx.Constraints.Max
	return p.list.List.Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Min: image.Pt(sz.X, 0), Max: image.Pt(sz.X-gtx.Dp(8), 1<<24)}
		f := &flow{gtx: gtx, w: gtx.Constraints.Max.X}
		p.gtxNow = gtx
		t := p.a.Taut()
		if t == nil {
			p.muted(f, "Plugins start once Clack has signed in to Slack.")
			return layout.Dimensions{Size: image.Pt(f.w, f.y)}
		}
		s := t.Settings()
		switch p.screen {
		case "plugin":
			p.pluginScreen(f, t, s)
		case "appearance":
			p.appearanceScreen(f, t, s)
		case "advanced":
			p.advancedScreen(f, t, s)
		case "about":
			p.aboutScreen(f, t, s)
		} // "main": the list is Preferences > Plugins (plugins_prefs.go)
		f.gap(24)
		return layout.Dimensions{Size: image.Pt(f.w, f.y)}
	})
}

// subScreen draws the back link and title.
func (p *tautPage) subScreen(f *flow, title, back string) {
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		d, c := p.linkButton(gtx, "back", back, "back")
		if c {
			p.go_("main", "")
		}
		return d
	})
	f.gap(12)
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		return p.label(gtx, 18, theme.Bold, p.colors().fg, title)
	})
	f.gap(16)
}

// ---- plugin page ----

func (p *tautPage) findPlugin(s taut.Settings, id string) (taut.PluginInfo, string, bool) {
	for _, c := range s.Categories {
		for _, pl := range c.Plugins {
			if pl.ID == id {
				return pl, c.Label, true
			}
		}
	}
	return taut.PluginInfo{}, "", false
}

func (p *tautPage) pluginScreen(f *flow, t *taut.Taut, s taut.Settings) {
	cl := p.colors()
	pl, _, ok := p.findPlugin(s, p.plugin) // its group: the shared taxonomy below
	if !ok {
		p.subScreen(f, "Plugin not found", "Plugins")
		p.muted(f, p.plugin+" isn't installed any more.")
		return
	}
	p.subScreen(f, pl.Name, "Plugins")
	if d := plainMrkdwn(pl.Description); d != "" {
		p.muted(f, d)
		f.gap(4)
	}
	// Taut's PluginHeader: one small mrkdwn line, "Messages · by <authors> ·
	// user plugin `Id`" (authors are mentions in the Hack Club Slack)
	meta := []string{mrkdwnEscape(sharedCategory(pluginlib.Taut, pl.Category).Label)}
	if a := tautAuthorsMrkdwn(pl, t.IsHackClub()); a != "" {
		meta = append(meta, "by "+a)
	}
	if pl.IsUser {
		meta = append(meta, "user plugin `"+pl.ID+"`")
	}
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		return p.mrkdwn(gtx, "meta."+pl.ID, strings.Join(meta, " · "), true, cl.fg3)
	})
	f.gap(12)
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		d, c := p.checkbox(gtx, "turnon."+pl.ID, pl.Enabled)
		if c {
			on := !pl.Enabled
			p.run("enable", func(ctx context.Context, t *taut.Taut) error { return t.SetEnabled(ctx, pl.ID, on) }, nil)
		}
		at(gtx, image.Pt(d.Size.X+gtx.Dp(8), -gtx.Dp(3)), image.Pt(f.w, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
			return p.label(gtx, 15, theme.Regular, cl.fg, "Turn on "+pl.Name)
		})
		return layout.Dimensions{Size: image.Pt(f.w, gtx.Dp(18))}
	})
	if pl.Enabled && !pl.Running && pl.Error == "" && p.busy == "" {
		f.gap(4)
		f.put(0, func(gtx layout.Context) layout.Dimensions {
			return p.para(gtx, 13, 18, cl.fg3, "Starting…")
		})
	}
	if pl.Error != "" {
		f.gap(8)
		f.put(0, func(gtx layout.Context) layout.Dimensions { return p.alert(gtx, pl.Error) })
	}
	if len(pl.Options) > 0 {
		p.hr(f)
		p.legend(f, "Settings")
		if p.jsonEdit {
			p.jsonEditor(f, t, pl)
		} else {
			for _, o := range pl.Options {
				p.optionField(f, t, pl, o)
			}
		}
		f.put(0, func(gtx layout.Context) layout.Dimensions {
			label := "Edit as JSON"
			if p.jsonEdit {
				label = "Back to the form"
			}
			d, c := p.linkButton(gtx, "jsonedit", label, "")
			if c {
				p.jsonEdit = !p.jsonEdit
				p.eds["json."+pl.ID] = nil
			}
			return d
		})
	}
	p.dataSection(f, t, pl)
}

// optionField draws one option: label + comment, the control, a reset
// button, and its problem.
func (p *tautPage) optionField(f *flow, t *taut.Taut, pl taut.PluginInfo, o taut.OptionState) {
	cl := p.colors()
	key := pl.ID + "/" + o.Key
	label := o.Label
	if label == "" {
		label = o.Key
	}
	comment := ""
	if o.Comment != nil {
		comment = *o.Comment
	}
	set := func(v any) {
		p.run("opt", func(ctx context.Context, t *taut.Taut) error { return t.SetOption(ctx, pl.ID, o.Key, v) }, nil)
	}
	reset := func() {
		p.run("opt", func(ctx context.Context, t *taut.Taut) error { return t.ResetOption(ctx, pl.ID, o.Key) }, func() {
			p.eds["opt."+key] = nil
		})
	}
	resetBtn := func(gtx layout.Context) layout.Dimensions {
		b := p.btn("reset." + key)
		s := gtx.Dp(36)
		d := clickArea(gtx, b, nil, func(gtx layout.Context) layout.Dimensions {
			r := image.Rect(0, 0, s, s)
			if o.Set {
				bg := cl.bg
				if b.Hovered() {
					bg = p.a.th.C.BgHover
				}
				theme.RRect(gtx, r, gtx.Dp(8), bg)
				theme.Border(gtx, r, gtx.Dp(8), gtx.Dp(1), cl.border)
			} else {
				theme.RRect(gtx, r, gtx.Dp(8), p.a.th.OnBg(rgbaT(0x1d1c1d, .06)))
			}
			col := cl.fg
			if !o.Set {
				col = cl.fg3
			}
			at(gtx, image.Pt((s-gtx.Dp(16))/2, (s-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
				return icons.Get("refresh")(gtx, 16, col)
			})
			return layout.Dimensions{Size: image.Pt(s, s)}
		})
		if clicked(b) && o.Set {
			reset()
		}
		return d
	}
	withReset := func(control func(gtx layout.Context, w int) layout.Dimensions) {
		f.put(0, func(gtx layout.Context) layout.Dimensions {
			bw := gtx.Dp(36 + 8)
			d := control(gtx, f.w-bw)
			at(gtx, image.Pt(f.w-gtx.Dp(36), 0), image.Pt(gtx.Dp(36), gtx.Dp(36)), resetBtn)
			return layout.Dimensions{Size: image.Pt(f.w, max(d.Size.Y, gtx.Dp(36)))}
		})
	}
	labelBlock := func() {
		f.put(0, func(gtx layout.Context) layout.Dimensions {
			return p.label(gtx, 15, theme.Bold, cl.fg, label)
		})
		if comment != "" {
			f.put(0, func(gtx layout.Context) layout.Dimensions { return p.para(gtx, 13, 18, cl.fg2, comment) })
		}
		f.gap(4)
	}

	switch {
	case o.HasEditor:
		labelBlock()
		p.editorSub(f, t, pl, o)
	case o.Kind.Type == "boolean":
		var on bool
		_ = json.Unmarshal(o.Current, &on)
		withReset(func(gtx layout.Context, w int) layout.Dimensions {
			d, c := p.checkbox(gtx, "opt."+key, on)
			if c {
				set(!on)
			}
			ld := at(gtx, image.Pt(d.Size.X+gtx.Dp(8), -gtx.Dp(3)), image.Pt(w-d.Size.X-gtx.Dp(8), 1<<20), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min = image.Point{}
				dd := p.label(gtx, 15, theme.Regular, cl.fg, label)
				if comment != "" {
					at(gtx, image.Pt(0, dd.Size.Y), image.Pt(gtx.Constraints.Max.X, 1<<20), func(gtx layout.Context) layout.Dimensions {
						dd.Size.Y += p.para(gtx, 13, 18, cl.fg2, comment).Size.Y
						return dd
					})
				}
				return dd
			})
			return layout.Dimensions{Size: image.Pt(w, max(ld.Size.Y, gtx.Dp(36)))}
		})
	case o.Kind.Type == "select":
		labelBlock()
		var cur string
		_ = json.Unmarshal(o.Current, &cur)
		curLabel := cur
		for _, ch := range o.Kind.Options {
			if ch.Value == cur {
				curLabel = ch.Label
			}
		}
		withReset(func(gtx layout.Context, w int) layout.Dimensions {
			b := p.btn("sel." + key)
			d := clickArea(gtx, b, nil, func(gtx layout.Context) layout.Dimensions {
				h := gtx.Dp(36)
				r := image.Rect(0, 0, w, h)
				theme.RRect(gtx, r, gtx.Dp(8), cl.bg)
				bc := cl.border
				if b.Hovered() {
					bc = cl.borderHover
				}
				theme.Border(gtx, r, gtx.Dp(8), gtx.Dp(1), bc)
				at(gtx, image.Pt(gtx.Dp(12), 0), image.Pt(w-gtx.Dp(40), h), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions { return p.label(gtx, 15, theme.Regular, cl.fg, curLabel) })
				})
				at(gtx, image.Pt(w-gtx.Dp(28), (h-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get("chevron-down")(gtx, 16, cl.fg2)
				})
				return layout.Dimensions{Size: r.Max}
			})
			if clicked(b) {
				var items []MenuItem
				for _, ch := range o.Kind.Options {
					ch := ch
					lbl := ch.Label
					if ch.Value == cur {
						lbl = "✓ " + lbl
					}
					items = append(items, MenuItem{Label: lbl, Do: func() { set(ch.Value) }})
				}
				p.a.menu(p.a.PointerPos(), items)
			}
			return d
		})
	case o.Kind.Type == "list":
		labelBlock()
		p.listControl(f, pl, o, set)
	case o.Kind.Type == "json":
		labelBlock()
		p.codeControl(f, key, o, set)
	case o.Kind.Type == "secret":
		labelBlock()
		e := p.ed("opt."+key, true)
		reveal := p.reveal[key]
		e.Mask = '•'
		if reveal {
			e.Mask = 0
		}
		if e.Len() == 0 && reveal && o.SecretSet {
			e.SetText(t.Secret(pl.ID, o.Key))
		}
		p.submitOn(e, func(v string) {
			if v == "" || reveal && v == t.Secret(pl.ID, o.Key) {
				return // reset clears it; unchanged isn't a save
			}
			p.run("opt", func(ctx context.Context, t *taut.Taut) error { return t.SetSecret(ctx, pl.ID, o.Key, v) }, nil)
		})
		withReset(func(gtx layout.Context, w int) layout.Dimensions {
			ph := "Not set"
			if o.SecretSet {
				ph = "•••••••• (saved)"
			}
			bw := gtx.Dp(70)
			d := p.input(gtx, e, w-bw-gtx.Dp(8), gtx.Dp(36), ph, false)
			at(gtx, image.Pt(w-bw, gtx.Dp(4)), image.Pt(bw, gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
				lbl := "Show"
				if reveal {
					lbl = "Hide"
				}
				d, c := p.button(gtx, "reveal."+key, lbl, popups.BtnOutline)
				if c {
					p.reveal[key] = !reveal
					e.SetText("")
				}
				return d
			})
			return d
		})
	default: // number | string | color
		labelBlock()
		e := p.ed("opt."+key, true)
		cur := jsonScalar(o.Current)
		if !f.gtx.Focused(e) && e.Text() != cur {
			e.SetText(cur)
		}
		commit := func(v string) {
			if v == cur {
				return
			}
			switch o.Kind.Type {
			case "number":
				n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
				if err != nil {
					p.errText = label + ": not a number"
					return
				}
				if o.Kind.Min != nil && n < *o.Kind.Min || o.Kind.Max != nil && n > *o.Kind.Max {
					p.errText = label + ": " + rangeText(o.Kind.Min, o.Kind.Max)
					return
				}
				set(n)
			default:
				set(v)
			}
		}
		p.submitOn(e, commit)
		withReset(func(gtx layout.Context, w int) layout.Dimensions {
			x := 0
			if o.Kind.Type == "color" {
				// swatch box beside the field, like Taut
				s := gtx.Dp(36)
				r := image.Rect(0, 0, s, s)
				theme.RRect(gtx, r, gtx.Dp(8), cl.bg)
				theme.Border(gtx, r, gtx.Dp(8), gtx.Dp(1), cl.border)
				theme.RRect(gtx, r.Inset(gtx.Dp(5)), gtx.Dp(4), parseHexColor(e.Text()))
				x = s + gtx.Dp(8)
			}
			at(gtx, image.Pt(x, 0), image.Pt(w-x, gtx.Dp(36)), func(gtx layout.Context) layout.Dimensions {
				return p.input(gtx, e, w-x, gtx.Dp(36), "", false)
			})
			return layout.Dimensions{Size: image.Pt(w, gtx.Dp(36))}
		})
		if o.Kind.Type == "number" && (o.Kind.Min != nil || o.Kind.Max != nil) {
			f.put(0, func(gtx layout.Context) layout.Dimensions {
				return p.para(gtx, 13, 18, cl.fg3, rangeText(o.Kind.Min, o.Kind.Max))
			})
		}
	}
	if o.Problem != "" {
		f.gap(4)
		f.put(0, func(gtx layout.Context) layout.Dimensions {
			return p.alert(gtx, "config.json: "+o.Problem+". Using the default.")
		})
	}
	f.gap(20)
}

// submitOn commits an editor's text on Enter or when it loses focus.
func (p *tautPage) submitOn(e *widget.Editor, commit func(string)) {
	for {
		ev, ok := e.Update(p.gtxNow)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			commit(e.Text())
		}
	}
	k := e
	was := p.focusWas[k]
	now := p.gtxNow.Focused(e)
	if was && !now {
		commit(e.Text())
	}
	p.focusWas[k] = now
}

// listControl: one text row per item with a remove button, plus Add.
func (p *tautPage) listControl(f *flow, pl taut.PluginInfo, o taut.OptionState, set func(any)) {
	var items []string
	_ = json.Unmarshal(o.Current, &items)
	key := pl.ID + "/" + o.Key
	save := func(next []string) {
		if next == nil {
			next = []string{}
		}
		set(next)
	}
	for i := range items {
		i := i
		e := p.ed("list."+key+"."+strconv.Itoa(i), true)
		if !p.gtxNow.Focused(e) && e.Text() != items[i] {
			e.SetText(items[i])
		}
		p.submitOn(e, func(v string) {
			if v == items[i] {
				return
			}
			next := append([]string(nil), items...)
			next[i] = v
			save(next)
		})
		f.put(0, func(gtx layout.Context) layout.Dimensions {
			bw := gtx.Dp(36 + 8)
			p.input(gtx, e, f.w-bw, gtx.Dp(36), "", false)
			at(gtx, image.Pt(f.w-gtx.Dp(36), 0), image.Pt(gtx.Dp(36), gtx.Dp(36)), func(gtx layout.Context) layout.Dimensions {
				return iconButton(gtx, p.btn("rm."+key+"."+strconv.Itoa(i)), "close", 36, 16, p.colors().fg2, p.a.th.C.BgHover, 8)
			})
			if clicked(p.btn("rm." + key + "." + strconv.Itoa(i))) {
				next := append(append([]string(nil), items[:i]...), items[i+1:]...)
				for j := i; j <= len(items); j++ {
					p.eds["list."+key+"."+strconv.Itoa(j)] = nil
				}
				save(next)
			}
			return layout.Dimensions{Size: image.Pt(f.w, gtx.Dp(36))}
		})
		f.gap(8)
	}
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		d, c := p.button(gtx, "add."+key, "Add", popups.BtnOutline)
		if c {
			save(append(append([]string(nil), items...), ""))
		}
		x := d.Size.X + gtx.Dp(8)
		if o.Set {
			at(gtx, image.Pt(x, 0), image.Pt(1<<20, gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
				d, c := p.button(gtx, "listreset."+key, "Reset to default", popups.BtnOutline)
				if c {
					p.run("opt", func(ctx context.Context, t *taut.Taut) error { return t.ResetOption(ctx, pl.ID, o.Key) }, nil)
				}
				return d
			})
		}
		return d
	})
}

// codeControl is a JSON option's code box with Save.
func (p *tautPage) codeControl(f *flow, key string, o taut.OptionState, set func(any)) {
	e := p.ed("code."+key, false)
	if e.Len() == 0 && !p.gtxNow.Focused(e) {
		e.SetText(prettyJSON(o.Current))
	}
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		return p.input(gtx, e, f.w, gtx.Dp(140), "", true)
	})
	f.gap(8)
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		d, c := p.button(gtx, "codesave."+key, "Save", popups.BtnOutline)
		if c {
			var v any
			if err := json.Unmarshal([]byte(e.Text()), &v); err != nil {
				p.errText = "Not saved: " + err.Error()
			} else {
				set(v)
			}
		}
		return d
	})
}

// jsonEditor edits every option of a plugin as one JSON object.
func (p *tautPage) jsonEditor(f *flow, t *taut.Taut, pl taut.PluginInfo) {
	e := p.eds["json."+pl.ID]
	if e == nil {
		e = &widget.Editor{}
		obj := map[string]json.RawMessage{}
		for _, o := range pl.Options {
			if o.Kind.Type != "secret" {
				obj[o.Key] = o.Current
			}
		}
		b, _ := json.MarshalIndent(obj, "", "  ")
		e.SetText(string(b))
		p.eds["json."+pl.ID] = e
	}
	f.put(0, func(gtx layout.Context) layout.Dimensions { return p.input(gtx, e, f.w, gtx.Dp(240), "", true) })
	f.gap(8)
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		d, c := p.button(gtx, "jsonsave."+pl.ID, "Save", popups.BtnOutline)
		if c {
			var obj map[string]json.RawMessage
			if err := json.Unmarshal([]byte(e.Text()), &obj); err != nil {
				p.errText = "Not saved: " + err.Error()
				return d
			}
			cur := map[string]string{}
			for _, o := range pl.Options {
				cur[o.Key] = string(o.Current)
			}
			keys := make([]string, 0, len(obj))
			for k := range obj {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			p.run("opt", func(ctx context.Context, t *taut.Taut) error {
				for _, k := range keys {
					if cur[k] == string(obj[k]) {
						continue
					}
					var v any
					_ = json.Unmarshal(obj[k], &v)
					if err := t.SetOption(ctx, pl.ID, k, v); err != nil {
						return err
					}
				}
				return nil
			}, func() { p.jsonEdit = false })
		}
		return d
	})
	f.gap(8)
}

// editorSub mounts an option's React editor (Bridge.RenderEditor).
func (p *tautPage) editorSub(f *flow, t *taut.Taut, pl taut.PluginInfo, o taut.OptionState) {
	k := "editor." + pl.ID + "/" + o.Key
	s := p.subs[k]
	if s == nil {
		s = slackc.NewSub()
		p.subs[k] = s
		id, key := pl.ID, o.Key
		p.handles[k] = t.Bridge().RenderEditor(s.Root(), id, key, o.Current, func(v json.RawMessage) {
			var x any
			if json.Unmarshal(v, &x) == nil {
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					if err := t.SetOption(ctx, id, key, x); err != nil {
						p.a.Post(func() { p.errText = err.Error() })
					}
				}()
			}
		})
	}
	p.subLayout(f, s, "Loading editor…")
}

// subLayout lays out a JS-rendered subtree, or a placeholder until it arrives.
func (p *tautPage) subLayout(f *flow, s *slackc.Sub, waiting string) {
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		if !s.Ready() {
			return p.para(gtx, 13, 18, p.colors().fg3, waiting)
		}
		gtx.Constraints.Max.X = f.w
		return s.Layout(gtx)
	})
}

func (p *tautPage) dataSection(f *flow, t *taut.Taut, pl taut.PluginInfo) {
	p.hr(f)
	p.legend(f, "Data")
	if pl.HasDataPanel {
		if pl.Running {
			k := "panel." + pl.ID + "." + strconv.Itoa(pl.RunID) // remount on restart
			s := p.subs[k]
			if s == nil {
				for kk, old := range p.subs {
					if strings.HasPrefix(kk, "panel."+pl.ID+".") {
						old.Close()
						delete(p.subs, kk)
					}
				}
				s = slackc.NewSub()
				p.subs[k] = s
				t.Bridge().RenderDataPanel(s.Root(), pl.ID)
			}
			p.subLayout(f, s, "Loading…")
			f.gap(12)
		} else {
			p.muted(f, "Turn it on to see its data.")
			f.gap(8)
		}
	}
	switch {
	case pl.HasStorage:
		p.muted(f, pl.Name+" has saved data on this device.")
	case pl.HasCache:
		p.muted(f, pl.Name+" only has a cache, which it can rebuild.")
	default:
		p.muted(f, pl.Name+" hasn't stored anything.")
	}
	f.gap(8)
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		x := 0
		add := func(k, label string, kind popups.BtnKind, do func()) {
			d := at(gtx, image.Pt(x, 0), image.Pt(1<<20, gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
				d, c := p.button(gtx, k, label, kind)
				if c {
					do()
				}
				return d
			})
			x += d.Size.X + gtx.Dp(8)
		}
		if pl.HasCache {
			add("clearcache", p.busyLabel("cache", "Clear cache", "Clearing..."), popups.BtnOutline, func() {
				p.confirm("Clear "+pl.Name+" cache?", pl.Name+" will rebuild it as needed.", "Clear cache", false, func() {
					p.run("cache", func(ctx context.Context, t *taut.Taut) error { return t.ClearData(ctx, pl.ID, "cache") }, nil)
				})
			})
		}
		if pl.HasStorage {
			add("cleardata", p.busyLabel("storage", "Clear data", "Clearing..."), popups.BtnOutline, func() {
				p.confirm("Clear "+pl.Name+" data?", "Everything "+pl.Name+" saved on this device will be deleted.", "Clear data", true, func() {
					p.run("storage", func(ctx context.Context, t *taut.Taut) error { return t.ClearData(ctx, pl.ID, "storage") }, nil)
				})
			})
		}
		if pl.IsUser {
			add("update", "Update code", popups.BtnOutline, func() { p.replace = !p.replace })
			add("delete", p.busyLabel("delete", "Delete plugin", "Deleting..."), popups.BtnDanger, func() {
				p.confirm("Delete "+pl.Name+"?", "Its code, settings and data are removed from this device.", "Delete", true, func() {
					p.run("delete", func(ctx context.Context, t *taut.Taut) error { return t.Remove(ctx, pl.ID) }, func() { p.go_("main", "") })
				})
			})
		}
		return layout.Dimensions{Size: image.Pt(x, gtx.Dp(28))}
	})
	if p.replace && pl.IsUser {
		f.gap(12)
		p.muted(f, "Only install code you trust! Plugins can do anything you can do in Slack.")
		f.gap(8)
		p.importControls(f, pl.ID)
	}
	if p.errText != "" {
		f.gap(8)
		f.put(0, func(gtx layout.Context) layout.Dimensions { return p.alert(gtx, p.errText) })
	}
}

func (p *tautPage) busyLabel(kind, idle, busy string) string {
	if p.busy == kind {
		return busy
	}
	return idle
}

func (p *tautPage) confirm(title, body, action string, danger bool, ok func()) {
	p.a.ShowOverlay(popups.Confirm(p.a, title, body, action, danger, nil, ok))
}

// importControls: Choose file... | URL field | Import URL.
func (p *tautPage) importControls(f *flow, replacing string) {
	done := func(id string) {
		p.urlEd.SetText("")
		p.replace = false
		p.note = ""
		if id != "" {
			p.go_("plugin", id)
		}
	}
	for {
		ev, ok := p.urlEd.Update(p.gtxNow)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok && strings.TrimSpace(p.urlEd.Text()) != "" {
			p.importURL(replacing, done)
		}
	}
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		d, c := p.button(gtx, "choose", "Choose file...", popups.BtnOutline)
		if c {
			p.importFile(replacing, done)
		}
		bw := gtx.Dp(110)
		x := d.Size.X + gtx.Dp(8)
		at(gtx, image.Pt(x, 0), image.Pt(f.w-x-bw-gtx.Dp(8), gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
			return p.input(gtx, &p.urlEd, f.w-x-bw-gtx.Dp(8), gtx.Dp(28), "or paste a URL to a .js file", false)
		})
		at(gtx, image.Pt(f.w-bw, 0), image.Pt(bw, gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
			d, c := p.button(gtx, "importurl", p.busyLabel("import", "Import URL", "Importing..."), popups.BtnOutline)
			if c && strings.TrimSpace(p.urlEd.Text()) != "" {
				p.importURL(replacing, done)
			}
			return d
		})
		return layout.Dimensions{Size: image.Pt(f.w, gtx.Dp(28))}
	})
}

func (p *tautPage) importURL(replacing string, done func(string)) {
	u := strings.TrimSpace(p.urlEd.Text())
	var id string
	p.run("import", func(ctx context.Context, t *taut.Taut) error {
		var err error
		if replacing == "" {
			id, err = t.InstallURL(ctx, u)
			return err
		}
		code, err := t.FetchPlugin(ctx, u)
		if err != nil {
			return err
		}
		id, err = t.Import(ctx, code, replacing)
		return err
	}, func() { done(id) })
}

func (p *tautPage) importFile(replacing string, done func(string)) {
	a := p.a
	go func() {
		files, err := a.pickFiles(".js,.mjs", false)
		if err != nil || len(files) == 0 {
			if err != nil {
				a.Post(func() { p.errText = err.Error() })
			}
			return
		}
		code := files[0].Data
		a.Post(func() {
			var id string
			p.run("import", func(ctx context.Context, t *taut.Taut) error {
				var err error
				id, err = t.Import(ctx, code, replacing)
				return err
			}, func() { done(id) })
		})
	}()
}

// ---- Appearance, Advanced, About ----

// fieldGap separates two of Slack's FieldSets on one screen (Taut's
// Advanced and About stack them without rules).
func (p *tautPage) fieldGap(f *flow) { f.gap(28) }

// editorBar is Taut's DocumentEditor bar: an outline small save button
// (off until there are changes), Discard while there are, extra buttons,
// and "Unsaved changes" / "Saved" at the right.
func (p *tautPage) editorBar(f *flow, k, saveLabel string, dirty bool, save, discard func(), extra ...struct {
	k, label string
	do       func()
}) {
	cl := p.colors()
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		x := 0
		add := func(k, label string, enabled bool, do func()) {
			d := at(gtx, image.Pt(x, 0), image.Pt(1<<20, gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
				b := p.btn(k)
				kind := popups.BtnOutline
				if !enabled || p.busy != "" {
					kind = popups.BtnDisabled
				}
				hit := b.Clicked(gtx)
				d := popups.Button(gtx, p.a.th, b, kind, popups.BtnSmall, label, "", 0)
				if hit && kind != popups.BtnDisabled {
					do()
				}
				return d
			})
			x += d.Size.X + gtx.Dp(8)
		}
		label := saveLabel
		if p.busy == k {
			label = "Saving..."
		}
		add("save."+k, label, dirty, save)
		if dirty {
			add("discard."+k, "Discard", true, discard)
		}
		for _, e := range extra {
			add(e.k, e.label, true, e.do)
		}
		st := "Saved"
		if dirty {
			st = "Unsaved changes"
		}
		sw := p.a.th.Measure(gtx, 13, theme.Regular, st)
		at(gtx, image.Pt(f.w-sw, 0), image.Pt(sw+2, gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, gtx.Dp(28), func(gtx layout.Context) layout.Dimensions {
				return p.label(gtx, 13, theme.Regular, cl.fg3, st)
			})
		})
		return layout.Dimensions{Size: image.Pt(f.w, gtx.Dp(28))}
	})
}

func (p *tautPage) appearanceScreen(f *flow, t *taut.Taut, s taut.Settings) {
	cl := p.colors()
	p.subScreen(f, "Taut custom CSS", "Plugin settings")
	p.legend(f, "Custom CSS")
	note := "Saved to `" + filepath.Join(t.Dir(), "user.css") + "`. It styles " + AppName + "'s Slack class names, like Taut's custom CSS."
	f.put(0, func(gtx layout.Context) layout.Dimensions { return p.mrkdwn(gtx, "css.note", note, false, cl.fg2) })
	f.gap(20)
	if !p.cssInit {
		p.cssEd.SetText(s.UserCSS)
		p.cssSaved = s.UserCSS
		p.cssInit = true
	}
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		return p.input(gtx, &p.cssEd, f.w, gtx.Dp(420), "/* your CSS */", true)
	})
	f.gap(8)
	if p.errText != "" {
		f.put(0, func(gtx layout.Context) layout.Dimensions { return p.alert(gtx, p.errText) })
		f.gap(8)
	}
	p.editorBar(f, "css", "Save user.css", p.cssEd.Text() != p.cssSaved, func() {
		css := p.cssEd.Text()
		p.run("css", func(ctx context.Context, t *taut.Taut) error { return t.SetUserCSS(css) }, func() { p.cssSaved = css })
	}, func() { p.cssEd.SetText(p.cssSaved); p.errText = "" })
}

func (p *tautPage) advancedScreen(f *flow, t *taut.Taut, s taut.Settings) {
	cl := p.colors()
	// installing is Preferences > Plugins' "Install from file or URL"
	p.subScreen(f, "Taut config.json", "Plugin settings")
	p.legend(f, "config.json")
	path := filepath.Join(t.Dir(), "config.json")
	note := "Saved to `" + path + "`. Edits made here or in a text editor apply right away."
	f.put(0, func(gtx layout.Context) layout.Dimensions { return p.mrkdwn(gtx, "cfg.note", note, false, cl.fg2) })
	f.gap(20)
	if s.ConfigError != "" {
		f.put(0, func(gtx layout.Context) layout.Dimensions {
			return p.alert(gtx, "Can't read config.json: "+s.ConfigError)
		})
		f.gap(8)
	}
	if !p.cfgInit {
		b, err := os.ReadFile(path)
		if err != nil {
			b = []byte("{}\n")
		}
		p.cfgEd.SetText(string(b))
		p.cfgSaved = string(b)
		p.cfgInit = true
	}
	f.put(0, func(gtx layout.Context) layout.Dimensions { return p.input(gtx, &p.cfgEd, f.w, gtx.Dp(420), "", true) })
	f.gap(8)
	if e := p.cfgErr; e != "" || p.errFor == "cfg" && p.errText != "" {
		if e == "" {
			e = p.errText
		}
		f.put(0, func(gtx layout.Context) layout.Dimensions { return p.alert(gtx, e) })
		f.gap(8)
	}
	p.editorBar(f, "cfg", "Save config.json", p.cfgEd.Text() != p.cfgSaved, func() {
		text := p.cfgEd.Text()
		var probe any
		if err := json.Unmarshal([]byte(text), &probe); err != nil {
			p.cfgErr = "Not saved: " + err.Error()
			return
		}
		if _, ok := probe.(map[string]any); !ok {
			p.cfgErr = "Not saved: expected a JSON object"
			return
		}
		p.cfgErr = ""
		p.run("cfg", func(ctx context.Context, t *taut.Taut) error {
			return t.WriteConfig(ctx, []byte(text))
		}, func() { p.cfgSaved = text })
	}, func() { p.cfgEd.SetText(p.cfgSaved); p.cfgErr = "" }, struct {
		k, label string
		do       func()
	}{"reloadcfg", "Reload from disk", func() {
		p.cfgInit, p.cfgErr = false, ""
		p.run("cfg", func(ctx context.Context, t *taut.Taut) error { return t.ReloadConfig(ctx) }, nil)
	}})
}

func (p *tautPage) aboutScreen(f *flow, t *taut.Taut, s taut.Settings) {
	cl := p.colors()
	hc := t.IsHackClub()
	p.subScreen(f, "About Taut", "Plugin settings")
	p.legend(f, "Taut")
	about := tautMrkdwnHere("<https://github.com/jeremy46231/taut|Taut> is a client mod for Slack, made by <@U06UYA5GMB5> (GPLv3 or later). "+
		AppName+" runs Taut plugins unmodified on its own native implementation of the Taut API; no Taut code is included. Come say hi in <#C0A057686SF>.", hc)
	f.put(0, func(gtx layout.Context) layout.Dimensions { return p.mrkdwn(gtx, "about", about, false, cl.fg) })
	p.fieldGap(f)
	p.legend(f, "Plugins")
	n := 0
	for _, c := range s.Categories {
		for _, pl := range c.Plugins {
			line := "*" + mrkdwnEscape(pl.Name) + "*"
			if a := tautAuthorsMrkdwn(pl, hc); a != "" {
				line += " by " + a
			}
			if n > 0 {
				f.gap(6)
			}
			n++
			f.put(0, func(gtx layout.Context) layout.Dimensions { return p.mrkdwn(gtx, "credit."+pl.ID, line, false, cl.fg) })
		}
	}
	if n == 0 {
		p.muted(f, "No plugins installed.")
	}
}

// ---- helpers ----

func appVersion() string {
	if v := buildVersion; v != "" {
		return v
	}
	return "dev"
}

// buildVersion is set by cmd/clack (SetVersion).
var buildVersion string

// SetVersion records the build version (shown in Preferences > Taut).
func SetVersion(v string) { buildVersion = v }

// plainMrkdwn flattens a mrkdwn description to one plain line.
func plainMrkdwn(s string) string {
	if s == "" {
		return ""
	}
	t := richtext.PlainText(richtext.Doc{Blocks: richtext.FromMrkdwn(nil, s)})
	return strings.Join(strings.Fields(t), " ")
}

func jsonScalar(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n float64
	if json.Unmarshal(raw, &n) == nil {
		return strconv.FormatFloat(n, 'f', -1, 64)
	}
	return strings.TrimSpace(string(raw))
}

func prettyJSON(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

func rangeText(min, max *float64) string {
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	switch {
	case min != nil && max != nil:
		return f(*min) + " to " + f(*max)
	case min != nil:
		return f(*min) + " or more"
	case max != nil:
		return f(*max) + " or less"
	}
	return ""
}

// parseHexColor reads #rgb / #rrggbb (transparent when invalid).
func parseHexColor(s string) color.NRGBA {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return color.NRGBA{}
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.NRGBA{}
	}
	return rgbT(uint32(v))
}
