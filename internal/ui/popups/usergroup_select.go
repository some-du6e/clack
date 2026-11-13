package popups

import (
	"context"
	"image"
	"image/color"
	"slices"
	"strings"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// ugSelect is the user group dialogs' LazyFilterSelect in multi mode
// (client-boot uUBF h → o17d ControlledMultiSelect → ASH1
// MultiSelectInput): the picked options as c-token chips inside the
// input, the filter text after them, and the searcher's options in a list
// under it (rows optionsRowHeight 32px, at most maxListHeight 240px).
//
//   - large (Edit members, size LARGE): multiSelectInput.large, min-height
//     44, 18px text, padding 0 32px 4px 4px; tokens get the select's size
//     (Token renders c-token--${size}): c-token--large, 32px with 18px
//     names, the avatar a 32px square at its left; the box grows with the
//     tokens up to maxHeight (148) and scrolls past it.
//   - otherwise (the default channel picker, MEDIUM): min-height 36, 15px
//     text, padding 0 28px 4px 4px; c-token--medium, 26px with 15px names.
//
// Each token ends with its remove icon (close, removeToken). A click on a
// token selects it (.c-token--selected, base-inv-hgl-1); Backspace with
// the caret at the filter's start selects the last token, then removes
// the selected one and selects the one before it (onBackspace); Delete
// removes the selected token. Up and Down move the active option, Enter
// picks it, Escape closes the list.
type ugSelect struct {
	h           Host
	large       bool
	placeholder string
	maxH        float32 // dp; 0 grows freely
	allowEmpty  bool    // allowEmptyQuery: the list opens on an empty filter
	member      bool    // member options (avatars) rather than channels
	search      func(ctx context.Context, q string) ([]ugOption, error)
	// changed runs after the picked options change (onSelectedItemsChange).
	changed func(ids []string)

	ed       widget.Editor
	picked   []ugOption
	sel      int // the selected token, -1 for none
	tokClk   []widget.Clickable
	rmClk    []widget.Clickable
	optClk   []widget.Clickable
	boxClk   widget.Clickable
	list     layout.List
	scroll   layout.List
	q        string
	gen      int
	loaded   string // the query results is for
	results  []ugOption
	active   int
	open     bool
	focusReq bool
}

// ugOption is one option: a member or a channel.
type ugOption struct {
	ID, Label, Sub string
	Avatar         string
	Conv           core.Conv // channels: the icon
	Deleted        bool
}

func newUGSelect(h Host, large bool, placeholder string) *ugSelect {
	s := &ugSelect{h: h, large: large, placeholder: placeholder, sel: -1}
	s.ed.SingleLine = true
	s.ed.Submit = true
	s.list.Axis = layout.Vertical
	s.scroll.Axis = layout.Vertical
	return s
}

// IDs are the picked options' ids, in order.
func (s *ugSelect) IDs() []string {
	out := make([]string, len(s.picked))
	for i, o := range s.picked {
		out[i] = o.ID
	}
	return out
}

// SetPicked replaces the picked options (no changed call).
func (s *ugSelect) SetPicked(os []ugOption) { s.picked, s.sel = append([]ugOption{}, os...), -1 }

// Pick adds o (once) and clears the filter.
func (s *ugSelect) Pick(o ugOption) {
	if !slices.ContainsFunc(s.picked, func(p ugOption) bool { return p.ID == o.ID }) {
		s.picked = append(s.picked, o)
	}
	s.ed.SetText("")
	s.q, s.sel, s.active = "", -1, 0
	s.notify()
	s.query("")
}

// Remove drops the option with id.
func (s *ugSelect) Remove(id string) {
	i := slices.IndexFunc(s.picked, func(p ugOption) bool { return p.ID == id })
	if i < 0 {
		return
	}
	s.picked = slices.Delete(s.picked, i, i+1)
	s.sel = -1
	s.notify()
}

func (s *ugSelect) notify() {
	if s.changed != nil {
		s.changed(s.IDs())
	}
}

// Type sets the filter text (tests; a keystroke does the same).
func (s *ugSelect) Type(q string) {
	s.ed.SetText(q)
	s.q = q
	s.open = true
	s.query(q)
}

// Options are the list's options for the current filter (picked ones
// left out).
func (s *ugSelect) Options() []ugOption {
	if s.loaded != s.q {
		return nil
	}
	out := make([]ugOption, 0, len(s.results))
	for _, o := range s.results {
		if !slices.ContainsFunc(s.picked, func(p ugOption) bool { return p.ID == o.ID }) {
			out = append(out, o)
		}
	}
	return out
}

func (s *ugSelect) query(q string) {
	if s.search == nil || (strings.TrimSpace(q) == "" && !s.allowEmpty) {
		s.results, s.loaded = nil, q
		return
	}
	s.gen++
	gen, search := s.gen, s.search
	run(s.h, func(ctx context.Context) ([]ugOption, error) { return search(ctx, q) }, func(os []ugOption, err error) {
		if gen != s.gen {
			return
		}
		s.results, s.loaded = os, q
		if err != nil {
			s.results = nil
		}
		s.active = 0
		s.h.Invalidate()
	})
}

// Focus asks for the input's focus next frame (focusOnMount, the edit
// dialog's focusKey).
func (s *ugSelect) Focus() { s.focusReq = true }

func (s *ugSelect) events(gtx layout.Context) {
	if s.focusReq {
		s.focusReq = false
		gtx.Execute(key.FocusCmd{Tag: &s.ed})
	}
	focused := gtx.Focused(&s.ed)
	opts := s.Options()
	// keys before the editor sees them
	for {
		filters := []event.Filter{
			key.Filter{Focus: &s.ed, Name: key.NameUpArrow},
			key.Filter{Focus: &s.ed, Name: key.NameDownArrow},
			key.Filter{Focus: &s.ed, Name: key.NameEscape},
		}
		if a, b := s.ed.Selection(); s.ed.Len() == 0 || s.sel >= 0 || a == 0 && b == 0 { // isCaretAtStartOfFilterQuery
			filters = append(filters, key.Filter{Focus: &s.ed, Name: key.NameDeleteBackward})
		}
		if s.sel >= 0 {
			filters = append(filters, key.Filter{Focus: &s.ed, Name: key.NameDeleteForward})
		}
		if s.open && len(opts) > 0 {
			filters = append(filters, key.Filter{Focus: &s.ed, Name: key.NameReturn}, key.Filter{Focus: &s.ed, Name: key.NameEnter})
		}
		ev, ok := gtx.Event(filters...)
		if !ok {
			break
		}
		ke, ok := ev.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		switch ke.Name {
		case key.NameUpArrow:
			if s.active > 0 {
				s.active--
			}
		case key.NameDownArrow:
			s.open = true
			if s.active < len(opts)-1 {
				s.active++
			}
		case key.NameEscape:
			s.open = false
		case key.NameDeleteBackward, key.NameDeleteForward:
			// MultiSelectInput's onBackspace: no token selected selects the
			// last; else it goes and the one before it is selected (none
			// after the first). onDelete removes the selected token.
			switch {
			case s.sel >= 0 && s.sel < len(s.picked):
				i := s.sel
				s.Remove(s.picked[i].ID)
				if ke.Name == key.NameDeleteBackward && i > 0 {
					s.sel = i - 1
				}
			case ke.Name == key.NameDeleteBackward && len(s.picked) > 0:
				s.sel = len(s.picked) - 1
			}
		case key.NameReturn, key.NameEnter:
			if s.active < len(opts) {
				s.Pick(opts[s.active])
			}
		}
	}
	for {
		ev, ok := s.ed.Update(gtx)
		if !ok {
			break
		}
		switch ev.(type) {
		case widget.ChangeEvent:
			if t := s.ed.Text(); t != s.q {
				s.q, s.sel, s.open = t, -1, true
				s.query(t)
			}
		case widget.SubmitEvent:
			if s.active < len(opts) {
				s.Pick(opts[s.active])
			}
		}
	}
	for i := range s.rmClk { // the token's remove icon
		if i < len(s.picked) && s.rmClk[i].Clicked(gtx) {
			s.Remove(s.picked[i].ID)
			gtx.Execute(key.FocusCmd{Tag: &s.ed})
			break
		}
	}
	for i := range s.tokClk {
		if i < len(s.picked) && s.tokClk[i].Clicked(gtx) && !s.rmPressed(i) {
			s.sel = i
			gtx.Execute(key.FocusCmd{Tag: &s.ed})
		}
	}
	for i := range s.optClk {
		if i < len(opts) && s.optClk[i].Clicked(gtx) {
			s.Pick(opts[i])
			gtx.Execute(key.FocusCmd{Tag: &s.ed})
			break
		}
	}
	if s.boxClk.Clicked(gtx) {
		gtx.Execute(key.FocusCmd{Tag: &s.ed})
		s.open = true
	}
	pressed := s.boxClk.Pressed()
	for i := range s.optClk {
		pressed = pressed || s.optClk[i].Pressed()
	}
	if !focused && !pressed {
		s.open = false
	} else if focused && !s.open && (s.q != "" || s.allowEmpty) {
		s.open = true
	}
	if s.open && s.allowEmpty && s.loaded != s.q && s.gen == 0 {
		s.query(s.q)
	}
}

// rmPressed reports token i's remove icon took the press.
func (s *ugSelect) rmPressed(i int) bool { return i < len(s.rmClk) && s.rmClk[i].Pressed() }

// tokenGeom is the token's measures for the select's size (the token
// gets the select's own size: c-token--medium, 26px with 15px names, or
// --large, 32px with 18px names; the remove icon takes the token's
// font-size, 18 or 20).
func (s *ugSelect) tokenGeom(gtx layout.Context) (h int, fs float32, ic int, memberPad int) {
	if s.large {
		return dp(gtx, 32), 18, dp(gtx, 20), dp(gtx, 8)
	}
	return dp(gtx, 26), 15, dp(gtx, 18), dp(gtx, 6)
}

// tokenW is a token's width for o: a member's avatar square, the inner
// label (padding-left 6 or 8, right 4), the remove icon and the token's
// 4px right padding; a channel's (c-token_channel, no padding) inner 0 4
// with its 16px icon 2px before the name, then the remove icon.
func (s *ugSelect) tokenW(gtx layout.Context, th *theme.Theme, o ugOption) int {
	h, fs, ic, mp := s.tokenGeom(gtx)
	lw := textW(gtx, th, fs, theme.Bold, o.Label)
	if s.member {
		return h + mp + lw + dp(gtx, 4) + ic + dp(gtx, 4)
	}
	return dp(gtx, 4) + dp(gtx, 16) + dp(gtx, 2) + lw + dp(gtx, 4) + ic
}

// layout draws the select in the constraint width and handles input;
// the options list goes on top of everything (op.Defer).
func (s *ugSelect) layout(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	s.events(gtx)
	w := gtx.Constraints.Max.X
	// multiSelectInput: min-height 36 (large 44), padding 0 28px 4px 4px
	// (large: right 32), 15px (large 18px); with nothing picked (initial)
	// padding-left 12
	fs, minH, padX, padR := float32(15), dp(gtx, 36), dp(gtx, 12), dp(gtx, 28)
	if s.large {
		fs, minH, padR = 18, dp(gtx, 44), dp(gtx, 32)
	}
	tokH, _, _, _ := s.tokenGeom(gtx)
	for len(s.tokClk) < len(s.picked) {
		s.tokClk = append(s.tokClk, widget.Clickable{})
		s.rmClk = append(s.rmClk, widget.Clickable{})
	}
	// flow: tokens (margin 4px 0 0 4px), then the input
	inner := maxInt(1, w-dp(gtx, 4)-padR)
	x, y, rowH := 0, 0, tokH+dp(gtx, 4)
	type place struct{ x, y, w int }
	places := make([]place, len(s.picked))
	for i, o := range s.picked {
		tw := s.tokenW(gtx, th, o)
		if x > 0 && x+dp(gtx, 4)+tw > inner {
			x, y = 0, y+rowH
		}
		places[i] = place{x + dp(gtx, 4), y + dp(gtx, 4), tw}
		x += dp(gtx, 4) + tw
	}
	inputMin := dp(gtx, 80)
	ix, iy := x+dp(gtx, 4), y
	if len(s.picked) == 0 {
		ix, iy = padX-dp(gtx, 4), 0
	} else if x+inputMin > inner {
		ix, iy = dp(gtx, 4), y+rowH
	}
	contentH := iy + maxInt(rowH, minH-2*dp(gtx, 4))
	if len(s.picked) == 0 {
		contentH = minH
	}
	h := maxInt(minH, contentH+dp(gtx, 4))
	viewH := h
	if s.maxH > 0 && viewH > dp(gtx, s.maxH) {
		viewH = dp(gtx, s.maxH)
	}
	r := image.Rect(0, 0, w, viewH)
	rad := dp(gtx, 8)
	focused := gtx.Focused(&s.ed)
	if focused {
		ring, one := dp(gtx, 5), maxInt(1, dp(gtx, 1))
		rrect(gtx, r.Inset(-ring), rad+ring, cl.FocusRing)
		rrect(gtx, r.Inset(-one), rad+one, cl.Focus)
		rrect(gtx, r, rad, cl.Bg)
	} else {
		bw := border1(gtx)
		rrect(gtx, r, rad, cl.InputBorder)
		rrect(gtx, r.Inset(bw), rad-bw, cl.Bg)
	}
	at(gtx, image.Point{}, func(gtx layout.Context) layout.Dimensions {
		return s.boxClk.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: r.Size()} })
	})
	body := func(gtx layout.Context) layout.Dimensions {
		for i, o := range s.picked {
			o, p, i := o, places[i], i
			at(gtx, image.Pt(p.x, p.y), func(gtx layout.Context) layout.Dimensions {
				return s.tokClk[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return s.token(gtx, th, cl, o, p.w, tokH, i == s.sel, i)
				})
			})
		}
		ih := maxInt(tokH, dp(gtx, 22))
		if len(s.picked) == 0 {
			ih = minH
		}
		ew := maxInt(1, w-ix-padR)
		at(gtx, image.Pt(ix, iy+ifInt(len(s.picked) > 0, dp(gtx, 4), 0)), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Min: image.Pt(ew, 0), Max: image.Pt(ew, ih)}
			return vcenter(gtx, ih, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Min: image.Pt(ew, 0), Max: image.Pt(ew, ih)}
				if s.ed.Len() == 0 && len(s.picked) == 0 && s.placeholder != "" {
					call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, fs, theme.Regular, cl.Placeholder, s.placeholder)
					})
					call.Add(gtx.Ops)
				}
				return th.Editor(gtx, &s.ed, unit.Sp(fs), cl.Fg, theme.WithAlpha(cl.Focus, 0x55))
			})
		})
		return layout.Dimensions{Size: image.Pt(w, h)}
	}
	func() {
		defer clipRRect(gtx, r, rad).Pop()
		if viewH < h { // maxHeight: the tokens scroll
			gtx := gtx
			gtx.Constraints = layout.Exact(r.Size())
			s.scroll.Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
				gtx.Constraints.Min = image.Point{}
				return body(gtx)
			})
			return
		}
		body(gtx)
	}()
	if s.open {
		s.drawOptions(gtx, th, cl, w, viewH)
	}
	return layout.Dimensions{Size: r.Size()}
}

// token is a c-token: surf-hgl-1, radius 4, bold label; selected:
// base-inv-hgl-1 with content-inv-pry text. Its remove icon (close,
// removeToken) sits after the label.
func (s *ugSelect) token(gtx layout.Context, th *theme.Theme, cl *Colors, o ugOption, w, h int, selected bool, i int) layout.Dimensions {
	bg, fg := ugSurfHgl1(th), cl.Fg
	if selected {
		bg, fg = woTok(th, "base-inv-hgl-1"), woTok(th, "content-inv-pry")
	}
	rrect(gtx, image.Rect(0, 0, w, h), dp(gtx, 4), bg)
	_, fs, ic, mp := s.tokenGeom(gtx)
	x := 0
	if s.member {
		func() { // c-token__avatar: the left corners round, the right square
			defer clipRRect(gtx, image.Rect(0, 0, h+dp(gtx, 4), h), dp(gtx, 4)).Pop()
			defer clipRRect(gtx, image.Rect(0, 0, h, h), 0).Pop()
			avatar(gtx, s.h, o.Avatar, float32(h)/gtx.Metric.PxPerDp)
		}()
		x = h + mp
	} else {
		icn := "hash"
		if o.Conv.Kind == "private" {
			icn = "lock"
		}
		at(gtx, image.Pt(dp(gtx, 4), (h-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, icn, 16, fg)
		})
		x = dp(gtx, 4) + dp(gtx, 16) + dp(gtx, 2)
	}
	lw := textW(gtx, th, fs, theme.Bold, o.Label)
	at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, lw), h)}
		return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, fs, theme.Bold, fg, o.Label) })
	})
	rx := x + lw + dp(gtx, 4)
	if i < len(s.rmClk) {
		at(gtx, image.Pt(rx, (h-ic)/2), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, &s.rmClk[i], func(gtx layout.Context, sz image.Point, hovered bool) {
				if hovered { // removeToken:hover: surf-pry (selected: surf-inv)
					c := woTok(th, "surf-pry")
					if selected {
						c = woTok(th, "surf-inv")
					}
					rrect(gtx, image.Rectangle{Max: sz}, dp(gtx, 4), c)
				}
			}, func(gtx layout.Context) layout.Dimensions {
				icon(gtx, "close", float32(ic)/gtx.Metric.PxPerDp, fg)
				return layout.Dimensions{Size: image.Pt(ic, ic)}
			})
		})
	}
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// drawOptions is the select's c-select_options_list under the box,
// at most 240px of 32px rows (maxListHeight), the active one base-inv-hgl-1 with white text; members
// show their avatar, name and real name, channels their icon and name.
func (s *ugSelect) drawOptions(gtx layout.Context, th *theme.Theme, cl *Colors, w, top int) {
	opts := s.Options()
	if len(opts) == 0 && s.loaded == s.q && strings.TrimSpace(s.q) == "" {
		return
	}
	for len(s.optClk) < len(opts) {
		s.optClk = append(s.optClk, widget.Clickable{})
	}
	if s.active >= len(opts) {
		s.active = maxInt(0, len(opts)-1)
	}
	rowH := dp(gtx, 32)
	pad := dp(gtx, 12)
	n := len(opts)
	if n == 0 {
		n = 1 // "No matches found" / "Searching…"
	}
	listH := minInt(n*rowH, dp(gtx, 240)) + 2*pad // renderList: maxListHeight 240, then it scrolls
	m := op.Record(gtx.Ops)
	func() {
		defer op.Offset(image.Pt(0, top+dp(gtx, 4))).Push(gtx.Ops).Pop()
		box := image.Rect(0, 0, w, listH)
		card(gtx, cl, box, dp(gtx, 8))
		defer clipRRect(gtx, box, dp(gtx, 8)).Pop()
		if len(opts) == 0 {
			msg := nsUG.T("No matches found")
			if s.loaded != s.q {
				msg = nsUG.T("Searching…")
			}
			at(gtx, image.Pt(dp(gtx, 24), pad), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 15, theme.Regular, cl.Fg2, msg) })
			})
			return
		}
		at(gtx, image.Pt(0, pad), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(w, listH-2*pad))
			return s.list.Layout(gtx, len(opts), func(gtx layout.Context, i int) layout.Dimensions {
				o := opts[i]
				return s.optClk[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					if s.optClk[i].Hovered() {
						s.active = i
					}
					act := i == s.active
					fg, fg2 := cl.Fg, cl.Fg2
					if act {
						fill(gtx, image.Rect(0, 0, w, rowH), cl.Highlight)
						fg, fg2 = cl.HighText, cl.HighText
					}
					x := dp(gtx, 24)
					if s.member {
						at(gtx, image.Pt(x, (rowH-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
							return avatar(gtx, s.h, o.Avatar, 20)
						})
						x += dp(gtx, 20) + dp(gtx, 8)
					} else {
						ic := "hash"
						if o.Conv.Kind == "private" {
							ic = "lock"
						}
						at(gtx, image.Pt(x, (rowH-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
							return icon(gtx, ic, 16, fg)
						})
						x += dp(gtx, 16) + dp(gtx, 6)
					}
					nw := textW(gtx, th, 15, theme.Bold, o.Label)
					at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-x-dp(gtx, 24)), rowH)}
						return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 15, theme.Bold, fg, o.Label) })
					})
					if o.Sub != "" && o.Sub != o.Label {
						sx := x + nw + dp(gtx, 8)
						at(gtx, image.Pt(sx, 0), func(gtx layout.Context) layout.Dimensions {
							gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-sx-dp(gtx, 24)), rowH)}
							return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 15, theme.Regular, fg2, o.Sub) })
						})
					}
					return layout.Dimensions{Size: image.Pt(w, rowH)}
				})
			})
		})
	}()
	op.Defer(gtx.Ops, m.Stop())
}

// ugSurfHgl1 is --dt_color-surf-hgl-1 (the token's fill).
func ugSurfHgl1(th *theme.Theme) color.NRGBA { return woTok(th, "surf-hgl-1") }

func ifInt(c bool, a, b int) int {
	if c {
		return a
	}
	return b
}

// ugMemberOption is u as a member option (getMemberName and the real
// name after it).
func ugMemberOption(u core.User) ugOption {
	sub := ""
	if u.DisplaySet {
		sub = u.RealName
	}
	return ugOption{ID: u.ID, Label: userName(u), Sub: sub, Avatar: u.Avatar, Deleted: u.Deleted}
}

// ugChannelOption is channel id as an option.
func ugChannelOption(c *core.App, id string) ugOption {
	cv := core.Conv{ID: id, Name: id, Kind: "channel"}
	if c != nil {
		if r := c.ConvRef(id); r.Name != "" {
			cv.Name, cv.Kind = r.Name, r.Kind
		}
	}
	return ugOption{ID: id, Label: cv.Name, Conv: cv}
}
