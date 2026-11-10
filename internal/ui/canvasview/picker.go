package canvasview

// The editor's mention picker: typing "@name" lists people (core's
// directory search), "#name" lists channels; Up/Down move, Enter or Tab
// (or a click) inserts the mention as a pill, Escape closes.

import (
	"context"
	"image"
	"strings"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const pickMax = 6

type pickItem struct {
	kind           composer.AtomKind
	id, label, sub string
	avatar         string
}

type picker struct {
	on         bool
	trig       rune
	start, end int // the trigger's rune range in the editor doc
	query      string
	asked      string
	gen        int
	items      []pickItem
	sel        int
	clicks     [pickMax]widget.Clickable
	accept     int // index to accept at the next frame (-1 none); keys run inside the editor
	acceptSet  bool
}

func (p *picker) active() bool { return p.on && len(p.items) > 0 }

// key handles a key the editor offered (Intercept); true consumes it.
func (p *picker) key(name key.Name) bool {
	switch name {
	case key.NameUpArrow:
		p.sel = (p.sel + len(p.items) - 1) % len(p.items)
	case key.NameDownArrow:
		p.sel = (p.sel + 1) % len(p.items)
	case key.NameReturn, key.NameEnter, key.NameTab:
		p.accept, p.acceptSet = p.sel, true
	case key.NameEscape:
		p.on, p.items = false, nil
	default:
		return false
	}
	return true
}

// update finds the "@query" / "#query" before the caret and refreshes the
// suggestions (off the UI goroutine).
func (p *picker) update(v *View, s *session) {
	if p.acceptSet {
		p.acceptSet = false
		if p.accept >= 0 && p.accept < len(p.items) {
			p.insert(v, s, p.items[p.accept])
		}
		return
	}
	d := s.ed.Doc()
	c := cursor(s.ed)
	i := c
	for i > 0 {
		r := d.Runes[i-1]
		if r == ' ' || r == '\n' || r == '\t' || r == composer.ObjRune || c-i > 40 {
			break
		}
		i--
	}
	if i >= c || (d.Runes[i] != '@' && d.Runes[i] != '#') {
		p.on, p.items = false, nil
		return
	}
	trig, q := d.Runes[i], string(d.Runes[i+1:c])
	if p.on && p.trig == trig && p.start == i && p.query == q {
		p.end = c
		return
	}
	p.on, p.trig, p.start, p.end, p.query = true, trig, i, c, q
	p.asked = q
	p.gen++
	gen := p.gen
	a := v.h.Core()
	if a == nil || (trig == '@' && q == "") {
		p.items = nil
		return
	}
	e := v.edit
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var items []pickItem
		ql := strings.ToLower(q)
		if trig == '@' {
			for _, b := range []string{"here", "channel", "everyone"} {
				if strings.HasPrefix(b, ql) {
					items = append(items, pickItem{kind: composer.AtomBroadcast, id: b, label: "@" + b, sub: "Notify everyone"})
				}
			}
			us, _ := a.SearchUsers(ctx, q, pickMax)
			for _, u := range us {
				items = append(items, pickItem{kind: composer.AtomUser, id: u.ID, label: u.Name, sub: u.RealName, avatar: u.Avatar})
			}
		} else {
			snap := a.Snapshot()
			for _, c := range snap.Convs {
				if c.Kind != "channel" && c.Kind != "private" {
					continue
				}
				if strings.HasPrefix(strings.ToLower(c.Name), ql) {
					items = append(items, pickItem{kind: composer.AtomChannel, id: c.ID, label: c.Name})
				}
				if len(items) >= pickMax {
					break
				}
			}
		}
		if len(items) > pickMax {
			items = items[:pickMax]
		}
		v.post(func() {
			if e.pick.gen == gen {
				e.pick.items, e.pick.sel = items, 0
			}
		})
	}()
}

func (p *picker) insert(v *View, s *session, it pickItem) {
	at := &composer.Atom{Kind: it.kind, ID: it.id, Text: "@" + it.label}
	switch it.kind {
	case composer.AtomChannel:
		at.Text = "#" + it.label
	case composer.AtomBroadcast:
		at.Text = it.label
	}
	s.ed.InsertAtom(p.start, p.end, at)
	p.on, p.items, p.asked = false, nil, ""
	s.focus = true
	v.h.Invalidate()
}

// layout draws the suggestions at pos (the block's coordinates), on top of
// everything (op.Defer).
func (p *picker) layout(gtx layout.Context, v *View, s *session, pos image.Point, w int) {
	th := v.h.Theme()
	c := &th.C
	rowH := gtx.Dp(34)
	pw := min(gtx.Dp(300), max(gtx.Dp(200), w))
	ph := rowH*len(p.items) + gtx.Dp(8)
	m := op.Record(gtx.Ops)
	func() {
		defer op.Offset(pos).Push(gtx.Ops).Pop()
		r := image.Rect(0, 0, pw, ph)
		theme.Shadow(gtx, r.Add(image.Pt(0, gtx.Dp(2))), gtx.Dp(8), gtx.Dp(10), theme.WithAlpha(c.PanelShadow, 0x40))
		theme.RRect(gtx, r, gtx.Dp(8), c.BgRaised)
		theme.Border(gtx, r, gtx.Dp(8), gtx.Dp(1), c.Border)
		for i, it := range p.items {
			i, it := i, it
			y := gtx.Dp(4) + i*rowH
			clk := &p.clicks[i]
			hit := clk.Clicked(gtx) // before Layout, which drains a Clickable's events
			at(gtx, image.Pt(gtx.Dp(4), y), image.Pt(pw-gtx.Dp(8), rowH), func(gtx layout.Context) layout.Dimensions {
				return clk.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					sz := gtx.Constraints.Max
					if i == p.sel || clk.Hovered() {
						theme.RRect(gtx, image.Rectangle{Max: sz}, gtx.Dp(6), theme.WithAlpha(c.Accent, 0x22))
					}
					ax := gtx.Dp(8)
					as := gtx.Dp(20)
					switch {
					case it.avatar != "":
						if img, ok := v.h.Images().Get(it.avatar, as); ok {
							at(gtx, image.Pt(ax, (sz.Y-as)/2), image.Pt(as, as), func(gtx layout.Context) layout.Dimensions {
								return theme.Image(gtx, img, image.Pt(as, as), gtx.Dp(4))
							})
						} else {
							theme.RRect(gtx, image.Rect(ax, (sz.Y-as)/2, ax+as, (sz.Y+as)/2), gtx.Dp(4), c.Skeleton)
						}
					default:
						icon := "hash"
						if it.kind == composer.AtomBroadcast {
							icon = "at"
						}
						at(gtx, image.Pt(ax+gtx.Dp(2), (sz.Y-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
							return icons.Get(icon)(gtx, 16, c.FgMuted)
						})
					}
					tx := ax + as + gtx.Dp(8)
					lw := th.Measure(gtx, 14, theme.Bold, it.label)
					at(gtx, image.Pt(tx, 0), image.Pt(min(lw+2, sz.X-tx), sz.Y), func(gtx layout.Context) layout.Dimensions {
						return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
							return th.Label(gtx, 14, theme.Bold, c.Fg, it.label)
						})
					})
					if it.sub != "" && tx+lw+gtx.Dp(8) < sz.X {
						sx := tx + lw + gtx.Dp(8)
						at(gtx, image.Pt(sx, 0), image.Pt(sz.X-sx, sz.Y), func(gtx layout.Context) layout.Dimensions {
							return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
								return th.Text(gtx, theme.LabelOpts{Font: theme.Font(theme.Regular), Size: 13, Color: c.FgMuted, MaxLines: 1}, it.sub)
							})
						})
					}
					return layout.Dimensions{Size: sz}
				})
			})
			if hit {
				p.insert(v, s, it)
			}
		}
	}()
	op.Defer(gtx.Ops, m.Stop())
}
