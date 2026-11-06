package popups

import (
	"context"
	"image"
	"sort"
	"strings"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// "Forward this message" modal: a conversation picker (fuzzy over the
// sidebar's conversations, DMs included; picks become chips), an optional
// message, a quoted preview, and [Copy link] [Forward]. Forwarding posts
// the optional note plus the permalink, which Slack unfurls as a shared
// message.

const actFwdMax = 6

type actFwdTarget struct {
	id, title, lower, kind, avatar string
}

type actFwdChip struct {
	t  *actFwdTarget
	rm widget.Clickable
}

type actForward struct {
	h     Host
	m     core.Message
	modal *Modal
	fwd   *ModalButton
	link  string

	to, note widget.Editor
	toBox    widget.Clickable
	noteBox  widget.Clickable
	all      []actFwdTarget
	match    []int
	sel      int
	sugg     [actFwdMax]widget.Clickable
	chips    []*actFwdChip
	query    string
	focusTo  bool
	copyLink bool
}

// NewForwardModal builds the forward dialog for m. Show it with
// h.ShowOverlay.
func NewForwardModal(h Host, m core.Message) *Modal {
	f := &actForward{h: h, m: m, focusTo: true}
	f.to.SingleLine = true
	f.to.Submit = true
	f.link = m.Permalink
	if f.link == "" {
		f.link = permalink(h, m.Channel, m.TS, m.ThreadTS)
	}
	f.all = actFwdTargets(h)
	f.fwd = &ModalButton{Label: "Forward", Kind: BtnPrimary, Enabled: func() bool { return len(f.chips) > 0 }, Do: func() bool {
		f.send()
		return true
	}}
	f.modal = NewModal(h, ModalOpts{
		Title: "Forward this message",
		Body:  f.layout,
		Buttons: []*ModalButton{
			{Label: "Copy link", Kind: BtnOutline, Do: func() bool {
				f.copyLink = true // copied by the body, which has the frame context
				return false
			}},
			f.fwd,
		},
	})
	return f.modal
}

// actFwdTargets lists forwardable conversations from the snapshot.
func actFwdTargets(h Host) []actFwdTarget {
	snap, ok := actSnapshot(h)
	if !ok {
		return nil
	}
	out := make([]actFwdTarget, 0, len(snap.Convs))
	for _, c := range snap.Convs {
		if c.IsArchived {
			continue
		}
		t := actFwdTarget{id: c.ID, kind: c.Kind, title: convTitle(h, c)}
		if c.Kind == "im" {
			u, ok := snap.Users[c.User]
			if !ok {
				u, ok = lookupUser(h, c.User)
			}
			if ok {
				if u.Deleted {
					continue
				}
				t.avatar = u.Avatar
			}
		}
		if t.title == "" {
			continue
		}
		t.lower = strings.ToLower(t.title)
		out = append(out, t)
	}
	return out
}

// actFuzzy scores how well s (lowercase) matches q (lowercase): exact >
// prefix > word prefix > substring > subsequence; -1 = no match.
func actFuzzy(s, q string) int {
	if q == "" {
		return -1
	}
	switch {
	case s == q:
		return 1000
	case strings.HasPrefix(s, q):
		return 800 - minInt(len(s)-len(q), 99)
	}
	if i := strings.Index(s, q); i >= 0 {
		for j := i; j >= 0; j = actIndexFrom(s, q, j+1) {
			if j > 0 && strings.ContainsRune(" -_.,", rune(s[j-1])) {
				return 600 - minInt(j, 99)
			}
		}
		return 400 - minInt(i, 99)
	}
	// subsequence: every rune of q in order.
	gaps, k, last := 0, 0, -1
	qr := []rune(q)
	for i, r := range s {
		if k < len(qr) && r == qr[k] {
			if last >= 0 {
				gaps += i - last - 1
			}
			last = i
			k++
		}
	}
	if k < len(qr) {
		return -1
	}
	return 200 - minInt(gaps, 199)
}

func actIndexFrom(s, q string, from int) int {
	if from >= len(s) {
		return -1
	}
	i := strings.Index(s[from:], q)
	if i < 0 {
		return -1
	}
	return from + i
}

func (f *actForward) chosen(id string) bool {
	for _, c := range f.chips {
		if c.t.id == id {
			return true
		}
	}
	return false
}

func (f *actForward) refilter() {
	q := strings.ToLower(strings.TrimSpace(f.to.Text()))
	if q == f.query {
		return
	}
	f.query = q
	f.match = f.match[:0]
	f.sel = 0
	if q == "" {
		return
	}
	type hit struct{ i, score int }
	var hits []hit
	for i := range f.all {
		t := &f.all[i]
		if sc := actFuzzy(t.lower, q); sc >= 0 && !f.chosen(t.id) {
			hits = append(hits, hit{i, sc})
		}
	}
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].score != hits[b].score {
			return hits[a].score > hits[b].score
		}
		return len(f.all[hits[a].i].title) < len(f.all[hits[b].i].title)
	})
	for k := 0; k < len(hits) && k < actFwdMax; k++ {
		f.match = append(f.match, hits[k].i)
	}
}

func (f *actForward) pick(i int) {
	if i < 0 || i >= len(f.match) {
		return
	}
	t := &f.all[f.match[i]]
	if !f.chosen(t.id) {
		f.chips = append(f.chips, &actFwdChip{t: t})
	}
	f.to.SetText("")
	f.query = ""
	f.match = f.match[:0]
	f.focusTo = true
}

func (f *actForward) send() {
	ids := make([]string, len(f.chips))
	for i, c := range f.chips {
		ids[i] = c.t.id
	}
	msg := f.link
	if note := strings.TrimSpace(f.note.Text()); note != "" {
		msg = note + "\n" + f.link
	}
	actDo(f.h, "Message forwarded", func(ctx context.Context, a *core.App) error {
		for _, id := range ids {
			if _, err := a.Send(ctx, core.SendOpts{Channel: id, Text: msg}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (f *actForward) events(gtx layout.Context) {
	// keys the picker handles before the editor sees them.
	filters := []event.Filter{}
	if len(f.match) > 0 {
		filters = append(filters,
			key.Filter{Focus: &f.to, Name: key.NameUpArrow},
			key.Filter{Focus: &f.to, Name: key.NameDownArrow},
			key.Filter{Focus: &f.to, Name: key.NameReturn},
			key.Filter{Focus: &f.to, Name: key.NameEnter},
			key.Filter{Focus: &f.to, Name: key.NameTab},
		)
	}
	if f.to.Len() == 0 && len(f.chips) > 0 {
		filters = append(filters, key.Filter{Focus: &f.to, Name: key.NameDeleteBackward})
	}
	for len(filters) > 0 {
		e, ok := gtx.Event(filters...)
		if !ok {
			break
		}
		ke, isKey := e.(key.Event)
		if !isKey || ke.State != key.Press {
			continue
		}
		switch ke.Name {
		case key.NameUpArrow:
			if n := len(f.match); n > 0 {
				f.sel = (f.sel - 1 + n) % n
			}
		case key.NameDownArrow:
			if n := len(f.match); n > 0 {
				f.sel = (f.sel + 1) % n
			}
		case key.NameReturn, key.NameEnter, key.NameTab:
			f.pick(f.sel)
		case key.NameDeleteBackward:
			if f.to.Len() == 0 && len(f.chips) > 0 {
				f.chips = f.chips[:len(f.chips)-1]
			}
		}
	}
	for {
		e, ok := f.to.Update(gtx)
		if !ok {
			break
		}
		switch e.(type) {
		case widget.ChangeEvent:
			f.refilter()
		case widget.SubmitEvent:
			if len(f.match) > 0 {
				f.pick(f.sel)
			} else if len(f.chips) > 0 {
				f.modal.press(f.fwd)
				return
			}
		}
	}
	f.refilter()
	for i := range f.match {
		if f.sugg[i].Clicked(gtx) {
			f.pick(i)
			break
		}
	}
	for i := 0; i < len(f.chips); i++ {
		if f.chips[i].rm.Clicked(gtx) {
			f.chips = append(f.chips[:i], f.chips[i+1:]...)
			f.query = "\x00" // force a refilter (removed targets come back)
			f.refilter()
			f.focusTo = true
			break
		}
	}
	if f.toBox.Clicked(gtx) {
		f.focusTo = true
	}
	if f.noteBox.Clicked(gtx) {
		gtx.Execute(key.FocusCmd{Tag: &f.note})
	}
}

func (f *actForward) layout(gtx layout.Context) layout.Dimensions {
	if f.modal.done {
		return layout.Dimensions{}
	}
	if f.copyLink {
		f.copyLink = false
		copyText(gtx, f.link)
		f.h.Toast("Link copied")
	}
	f.events(gtx)
	if f.modal.done {
		return layout.Dimensions{}
	}
	if f.focusTo {
		gtx.Execute(key.FocusCmd{Tag: &f.to})
		f.focusTo = false
	}
	th := f.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	y := 0
	put := func(wd layout.Widget) int {
		call, d := record(loose(gtx, image.Pt(w, 1<<20)), wd)
		macroAt(gtx, call, image.Pt(0, y))
		return d.Size.Y
	}
	y += put(func(gtx layout.Context) layout.Dimensions { return f.picker(gtx, th, cl) })
	if len(f.match) > 0 {
		y += dp(gtx, 4)
		y += put(func(gtx layout.Context) layout.Dimensions { return f.suggestions(gtx, th, cl) })
	}
	y += dp(gtx, 16)
	y += put(func(gtx layout.Context) layout.Dimensions { return f.noteField(gtx, th, cl) })
	y += dp(gtx, 16)
	y += put(func(gtx layout.Context) layout.Dimensions { return actMsgPreview(gtx, f.h, f.m, false) })
	return layout.Dimensions{Size: image.Pt(w, y+dp(gtx, 4))}
}

// actFocusBox paints an input box (focus ring when focused).
func actFocusBox(gtx layout.Context, cl *Colors, r image.Rectangle, focused bool) {
	rad := dp(gtx, 8)
	if focused {
		ring := dp(gtx, 4)
		rrect(gtx, r.Inset(-ring), rad+ring, cl.FocusRing)
		rrect(gtx, r, rad, cl.Focus)
	} else {
		rrect(gtx, r, rad, cl.InputBorder)
	}
	rrect(gtx, r.Inset(1), rad-1, cl.Bg)
}

// picker draws the "Add a channel or person" field with chips.
func (f *actForward) picker(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	w := gtx.Constraints.Max.X
	padX, padY := dp(gtx, 8), dp(gtx, 7)
	lineH, gap := dp(gtx, 28), dp(gtx, 4)
	innerW := maxInt(1, w-2*padX)
	content, cd := record(gtx, func(gtx layout.Context) layout.Dimensions {
		x, y := 0, 0
		for _, c := range f.chips {
			call, d := record(gtx, func(gtx layout.Context) layout.Dimensions { return f.chip(gtx, th, cl, c, innerW) })
			if x > 0 && x+d.Size.X > innerW {
				x, y = 0, y+lineH+gap
			}
			macroAt(gtx, call, image.Pt(x, y+(lineH-d.Size.Y)/2))
			x += d.Size.X + gap
		}
		ew := innerW - x
		if ew < dp(gtx, 120) {
			x, y, ew = 0, y+lineH+gap, innerW
		}
		at(gtx, image.Pt(x+dp(gtx, 4), y), func(gtx layout.Context) layout.Dimensions {
			ew := maxInt(1, ew-dp(gtx, 4))
			return vcenter(gtx, lineH, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Min: image.Pt(ew, 0), Max: image.Pt(ew, lineH)}
				if f.to.Len() == 0 && len(f.chips) == 0 {
					call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 15, theme.Regular, cl.Placeholder, "Add a channel or person")
					})
					call.Add(gtx.Ops)
				}
				return th.Editor(gtx, &f.to, unit.Sp(15), cl.Fg, theme.WithAlpha(cl.Focus, 0x55))
			})
		})
		return layout.Dimensions{Size: image.Pt(innerW, y+lineH)}
	})
	r := image.Rect(0, 0, w, cd.Size.Y+2*padY)
	actFocusBox(gtx, cl, r, gtx.Focused(&f.to))
	// background click target (siblings drawn later sit on top of it).
	f.toBox.Layout(exact(gtx, r.Max), func(gtx layout.Context) layout.Dimensions {
		defer clip.Rect(r).Push(gtx.Ops).Pop()
		pointer.CursorText.Add(gtx.Ops)
		return layout.Dimensions{Size: r.Max}
	})
	macroAt(gtx, content, image.Pt(padX, padY))
	return layout.Dimensions{Size: r.Max}
}

func (f *actForward) chip(gtx layout.Context, th *theme.Theme, cl *Colors, c *actFwdChip, maxW int) layout.Dimensions {
	ch := dp(gtx, 26)
	tw := textW(gtx, th, 15, theme.Bold, c.t.title)
	iconW := dp(gtx, 20)
	rmW := dp(gtx, 20)
	w := dp(gtx, 4) + iconW + dp(gtx, 6) + tw + dp(gtx, 2) + rmW + dp(gtx, 2)
	if w > maxW {
		tw -= w - maxW
		w = maxW
	}
	rrect(gtx, image.Rect(0, 0, w, ch), dp(gtx, 4), theme.WithAlpha(cl.Link, 0x1f))
	x := dp(gtx, 4)
	at(gtx, image.Pt(x, (ch-iconW)/2), func(gtx layout.Context) layout.Dimensions {
		if c.t.kind == "im" {
			return avatar(gtx, f.h, c.t.avatar, 20)
		}
		at(gtx, image.Pt(dp(gtx, 2), dp(gtx, 2)), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, convIcon(c.t.kind), 16, cl.Fg2)
		})
		return layout.Dimensions{Size: image.Pt(iconW, iconW)}
	})
	x += iconW + dp(gtx, 6)
	at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, tw), ch)}
		return vcenter(gtx, ch, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Bold, cl.Fg, c.t.title)
		})
	})
	x += maxInt(1, tw) + dp(gtx, 2)
	at(gtx, image.Pt(x, (ch-rmW)/2), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, &c.rm, "close-small", 20, 14, cl.Fg2, theme.WithAlpha(cl.Link, 0x26), 4)
	})
	return layout.Dimensions{Size: image.Pt(w, ch)}
}

// suggestions draws up to six matching conversations under the picker.
func (f *actForward) suggestions(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	w := gtx.Constraints.Max.X
	rh := dp(gtx, 36)
	pad := dp(gtx, 6)
	n := len(f.match)
	r := image.Rect(0, 0, w, n*rh+2*pad)
	rrect(gtx, r, dp(gtx, 8), cl.Ring)
	rrect(gtx, r.Inset(1), dp(gtx, 8)-1, cl.Bg)
	for i := range f.match {
		if f.sugg[i].Hovered() {
			f.sel = i
		}
	}
	for i, ti := range f.match {
		t := &f.all[ti]
		at(gtx, image.Pt(0, pad+i*rh), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(w, rh))
			return clickBox(gtx, &f.sugg[i], nil, func(gtx layout.Context) layout.Dimensions {
				fg, fg2 := cl.Fg, cl.Fg2
				if i == f.sel {
					fill(gtx, image.Rect(0, 0, w, rh), cl.Highlight)
					fg, fg2 = cl.HighText, cl.HighText
				}
				x := dp(gtx, 16)
				at(gtx, image.Pt(x, (rh-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
					if t.kind == "im" {
						return avatar(gtx, f.h, t.avatar, 20)
					}
					at(gtx, image.Pt(dp(gtx, 2), dp(gtx, 2)), func(gtx layout.Context) layout.Dimensions {
						return icon(gtx, convIcon(t.kind), 16, fg2)
					})
					return layout.Dimensions{Size: image.Pt(dp(gtx, 20), dp(gtx, 20))}
				})
				x += dp(gtx, 30)
				at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-x-dp(gtx, 16)), rh)}
					return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 15, theme.Regular, fg, t.title)
					})
				})
				return layout.Dimensions{Size: image.Pt(w, rh)}
			})
		})
	}
	return layout.Dimensions{Size: r.Max}
}

// noteField draws the optional multi-line "Add a message" input.
func (f *actForward) noteField(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	w := gtx.Constraints.Max.X
	padX, padY := dp(gtx, 12), dp(gtx, 10)
	tw := maxInt(1, w-2*padX)
	content, cd := record(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Min: image.Pt(tw, 0), Max: image.Pt(tw, dp(gtx, 160))}
		if f.note.Len() == 0 {
			call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, cl.Placeholder, "Add a message, if you'd like.")
			})
			call.Add(gtx.Ops)
		}
		return th.Editor(gtx, &f.note, unit.Sp(15), cl.Fg, theme.WithAlpha(cl.Focus, 0x55))
	})
	hh := maxInt(cd.Size.Y+2*padY, dp(gtx, 64))
	r := image.Rect(0, 0, w, hh)
	actFocusBox(gtx, cl, r, gtx.Focused(&f.note))
	f.noteBox.Layout(exact(gtx, r.Max), func(gtx layout.Context) layout.Dimensions {
		defer clip.Rect(r).Push(gtx.Ops).Pop()
		pointer.CursorText.Add(gtx.Ops)
		return layout.Dimensions{Size: r.Max}
	})
	macroAt(gtx, content, image.Pt(padX, padY))
	return layout.Dimensions{Size: r.Max}
}
