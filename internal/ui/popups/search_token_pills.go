package popups

// The search box's tokens as Slack's Texty draws them (fffk, build
// 4d0c78085232; client-boot-styles):
//
//   - a filter (ts-search-filter, .c-member_slug--search-filter):
//     content-hgl-1 15px text on surf-hgl-1, radius 3, margin-left 1,
//     padding 1px 4px 1px 23px; the value after the label's first ":" is
//     bold (slacksearchfiltervalue); its modifier icon
//     (getSearchFilterIconName, 14px, content-hgl-1) sits centred 11.5px
//     from the pill's left. Hovered, the text is #0b4c8c on
//     rgba(29,155,209,.2) and the icon is close; a click on the icon
//     (7.5 to 25.5px from the left) deletes the token and the space after
//     it (SearchFilterInputModule's onFilterClick). With the mention
//     attribute (the viewer) it is c-member_slug--mention: surf-hgl-3,
//     and in dark mode its icon content-hgl-3.
//   - a mention (c-member_slug--link): content-hgl-1 on surf-hgl-1,
//     padding 1px 2px, radius 3; the viewer's on surf-hgl-3.
//
// The editor lays the text out with the pills' space and the bold values
// (widget.TextSpan, Clack's Gio patch 6), so the caret and clicks follow
// what is drawn.

import (
	"image"
	"strings"
	"unicode/utf8"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// srBoldEditorFont is the editor's font in bold (a filter's value).
var srBoldEditorFont = theme.EditorFont(theme.Font(theme.Bold))

// srPillState is the pills' pointer state between frames.
type srPillState struct {
	hover   int // 1 + the token under the pointer
	pressed int // 1 + the token whose icon was pressed
	tags    []*srPillTag
	icons   []*srPillTag
	spans   []widget.TextSpan
}

type srPillTag struct{ i int }

// tokenSpans are the editor's spans for the tokens: a filter's margin and
// icon column before its modifier, its value bold with the right padding
// after it; a mention's padding on both sides.
func (p *SearchPopover) tokenSpans(gtx layout.Context) []widget.TextSpan {
	out := p.pills.spans[:0]
	n := p.ed.Len()
	for _, t := range p.tokens {
		if t.end() > n || t.label == "" {
			continue
		}
		if t.mention {
			out = append(out, widget.TextSpan{Start: t.start, End: t.end(), Before: dp(gtx, 2), After: dp(gtx, 2)})
			continue
		}
		before, after := dp(gtx, 1)+dp(gtx, 23), dp(gtx, 4)
		i := strings.IndexByte(t.label, ':')
		vs := t.start
		if i >= 0 {
			vs += utf8.RuneCountInString(t.label[:i+1])
		}
		if i < 0 || vs >= t.end() {
			out = append(out, widget.TextSpan{Start: t.start, End: t.end(), Before: before, After: after})
			continue
		}
		out = append(out, widget.TextSpan{Start: t.start, End: vs, Before: before},
			widget.TextSpan{Start: vs, End: t.end(), Font: &srBoldEditorFont, After: after})
	}
	p.pills.spans = out
	return out
}

// drawPills draws the tokens' pills from this frame's layout: the pill,
// the token's text again in the pill's colour, the modifier icon, and the
// pointer areas (the pill's hover, the icon's click).
func (p *SearchPopover) drawPills(gtx layout.Context, th *theme.Theme, cl *Colors) {
	st := &p.pills
	n := p.ed.Len()
	var regs []widget.Region
	for len(st.tags) < len(p.tokens) {
		st.tags = append(st.tags, &srPillTag{i: len(st.tags)})
		st.icons = append(st.icons, &srPillTag{i: len(st.icons)})
	}
	for i, t := range p.tokens {
		if t.end() > n {
			continue
		}
		regs = p.ed.Regions(t.start, t.end(), regs[:0])
		if len(regs) == 0 {
			continue
		}
		b := regs[0].Bounds
		for _, r := range regs[1:] {
			b = b.Union(r.Bounds)
		}
		left, right := dp(gtx, 2), dp(gtx, 2)
		if !t.mention {
			left, right = dp(gtx, 23), dp(gtx, 4)
		}
		r := image.Rect(b.Min.X-left, b.Min.Y-dp(gtx, 1), b.Max.X+right, b.Max.Y+dp(gtx, 1))
		hov := st.hover == i+1
		bg := tokSurfHgl1.of(th)
		fg := tokContentHgl1.of(th)
		iconFg := tokContentHgl1.of(th)
		self := t.self || t.mention && p.acSelfMention(t)
		switch {
		case self:
			bg = tokSurfHgl3.of(th)
			if th != nil && th.Dark && !t.mention {
				iconFg = tokContentHgl3.of(th)
			}
		case hov:
			bg = rgba(0x1d9bd1, .2)
		}
		if hov {
			fg = rgb(0x0b4c8c) // --sk_highlight_hover's fallback
		}
		func() {
			defer clip.UniformRRect(r, dp(gtx, 3)).Push(gtx.Ops).Pop()
			fill(gtx, r, theme.Over(bg, cl.Bg))
			th.Editor(gtx, &p.ed, unit.Sp(15), fg, theme.WithAlpha(cl.Focus, 0x55))
		}()
		icon14 := !t.mention && t.mod != "" && t.entity != ""
		if icon14 {
			name := t.icon
			if name == "" {
				name = "user"
			}
			if hov {
				name = "close"
			}
			is := dp(gtx, 14)
			at(gtx, image.Pt(r.Min.X+dp(gtx, 11.5)-is/2, (r.Min.Y+r.Max.Y-is)/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, name, 14, iconFg)
			})
		}
		// the pill's hover, and on a filter's icon the click that deletes it
		func() {
			defer pointer.PassOp{}.Push(gtx.Ops).Pop()
			defer clip.Rect(r).Push(gtx.Ops).Pop()
			event.Op(gtx.Ops, st.tags[i])
		}()
		if icon14 {
			ir := image.Rect(r.Min.X+dp(gtx, 7.5), r.Min.Y, r.Min.X+dp(gtx, 25.5), r.Max.Y)
			func() {
				defer clip.Rect(ir).Push(gtx.Ops).Pop()
				event.Op(gtx.Ops, st.icons[i])
				pointer.CursorPointer.Add(gtx.Ops)
			}()
		}
	}
}

// pillPointer applies last frame's pointer input on the pills (before
// the editor's layout: a click edits the box).
func (p *SearchPopover) pillPointer(gtx layout.Context) {
	st := &p.pills
	for i, tg := range st.tags {
		for {
			ev, ok := gtx.Event(pointer.Filter{Target: tg, Kinds: pointer.Enter | pointer.Move | pointer.Leave | pointer.Cancel})
			if !ok {
				break
			}
			if e, ok := ev.(pointer.Event); ok {
				switch e.Kind {
				case pointer.Enter, pointer.Move:
					st.hover = i + 1
				case pointer.Leave, pointer.Cancel:
					if st.hover == i+1 {
						st.hover = 0
					}
				}
			}
		}
	}
	remove := -1
	for i, tg := range st.icons {
		for {
			ev, ok := gtx.Event(pointer.Filter{Target: tg,
				Kinds: pointer.Enter | pointer.Move | pointer.Leave | pointer.Press | pointer.Release | pointer.Cancel})
			if !ok {
				break
			}
			e, ok := ev.(pointer.Event)
			if !ok {
				continue
			}
			switch e.Kind {
			case pointer.Enter, pointer.Move:
				st.hover = i + 1
			case pointer.Press:
				if e.Buttons.Contain(pointer.ButtonPrimary) || e.Source == pointer.Touch {
					st.pressed = i + 1
				}
			case pointer.Release:
				if st.pressed == i+1 {
					remove = i
				}
				st.pressed = 0
			case pointer.Cancel:
				st.pressed = 0
			}
		}
	}
	if remove >= 0 {
		p.removeToken(remove)
	}
}

// removeToken is onFilterClick: token i and the space after it go, as a
// user edit.
func (p *SearchPopover) removeToken(i int) {
	if i < 0 || i >= len(p.tokens) {
		return
	}
	t := p.tokens[i]
	rs := []rune(p.ed.Text())
	if t.end() > len(rs) {
		return
	}
	end := t.end()
	if end < len(rs) && rs[end] == ' ' {
		end++
	}
	caret, _ := p.ed.Selection()
	switch {
	case caret >= end:
		caret -= end - t.start
	case caret > t.start:
		caret = t.start
	}
	var out []srPiece
	at := 0
	for _, pc := range p.box().pieces() {
		l := utf8.RuneCountInString(pc.text)
		s, e := at, at+l
		at = e
		switch {
		case e <= t.start || s >= end:
			out = append(out, pc)
		case pc.tok != nil:
			// the token itself
		default:
			// the space after it
			rs := []rune(pc.text)
			var keep []rune
			for k := range rs {
				if s+k < t.start || s+k >= end {
					keep = append(keep, rs[k])
				}
			}
			if len(keep) > 0 {
				out = append(out, srText(string(keep)))
			}
		}
	}
	st := &p.pills
	st.hover, st.pressed = 0, 0
	p.setBox(srJoin(out), caret)
	p.acArmed = true // the "user" source
}
