package popups

// The search box's completer menu and its tokens (search_filter_ac.go has
// the matching and the sources).
//
// The menu is the search box's Texty autocomplete
// (.c-search__input__texty_autocomplete--adaptive, client-boot-styles):
// 480 wide, at most 260 tall with the help row, rows at least 40 high,
// rounded only below and clipped at the top edge (clip-path inset(1px
// -10px -10px)). OmniswitcherContent's positionTextyAutocompleteMenu puts
// it under the input row, its left 17 before the anchor and kept 8 inside
// the row. The first row is pseudo-selected (base-ter, the Enter hint);
// the first ↑/↓ selects it (#1264a3), Tab and Enter take the selection,
// Escape closes the menu until the match changes, and a press and release
// on a row takes it.
//
// The tokens' pills are search_token_pills.go.

import (
	"context"
	"image"
	"image/color"
	"sort"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/acrank"
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// srACState is the box's completer between frames.
type srACState struct {
	m         srACMatch
	open      bool // the match has rows: the menu shows, the list hides
	items     []srACItem
	sel       int
	pseudo    bool   // nothing selected yet: row 0 is the gray pseudo-selection
	dismissed string // Escape closed the menu for this match (srACMatch.key)
	rk        *srACRanker

	users map[string]map[string]core.User // the server's people per query
	chans map[string][]core.ChannelHit    // the server's channels per query
	asked map[string]bool

	tags    []*srACRowTag
	list    layout.List
	pressed int // 1 + the pressed row
	scroll  bool
	bg      int
	rowPx   int
	rowW    int
}

type srACRowTag struct{ i int }

// acFind is the completers' match at the caret: none with a selection or
// inside a token; tokens before the caret are atoms (the text after the
// last one is matched).
func (p *SearchPopover) acFind() (srACMatch, bool) {
	s, e := p.ed.Selection()
	if s != e {
		return srACMatch{}, false
	}
	rs := []rune(p.ed.Text())
	if s > len(rs) {
		s = len(rs)
	}
	from := 0
	for _, t := range p.tokens {
		if t.start < s && s < t.end() {
			return srACMatch{}, false
		}
		if t.end() <= s && t.end() > from {
			from = t.end()
		}
	}
	before := rs[from:s]
	dq := false
	if c := p.h.Core(); c != nil {
		dq = c.Experiment("omni_date_quick_filters") == "on"
	}
	m, ok := srFilterMatch(before, dq) // highPriority: before Texty's own
	if !ok {
		m, ok = srMentionMatch(before)
	}
	if !ok {
		return srACMatch{}, false
	}
	m.start += from
	m.anchor += from
	m.end += from
	return m, true
}

// acUpdate follows the text and the caret: a new match is searched (the
// server asked for its query once), the same one keeps its rows.
func (p *SearchPopover) acUpdate() {
	a := &p.ac
	m, ok := p.acFind()
	if !p.acArmed {
		// Texty completes what the user types, not the query the box
		// opened with
		ok = false
	}
	if !ok {
		a.m, a.open, a.dismissed = srACMatch{}, false, ""
		return
	}
	if m.key() == a.dismissed {
		a.m, a.open = m, false
		return
	}
	a.dismissed = ""
	if m != a.m {
		a.m = m
		a.items = p.acItems(m)
		a.sel, a.pseudo, a.scroll, a.pressed = 0, true, true, 0
		a.list.Position = layout.Position{}
		p.acFetch(m)
	}
	a.open = len(a.items) > 0
}

// acItems is the completer's search for m.
func (p *SearchPopover) acItems(m srACMatch) []srACItem {
	a := &p.ac
	if a.rk == nil {
		a.rk = newSRACRanker(p.h)
	}
	rk, c := a.rk, p.h.Core()
	if rk == nil || c == nil {
		return nil
	}
	switch m.kind {
	case srACMember:
		return rk.rank(m.value, a.users[m.value], true, false, nil)
	case srACChannel:
		return rk.rank(m.value, nil, false, true, a.chans[m.value])
	}
	vals := srACValues(m.typ, c.Experiment("later_todos") == "on", c.Experiment("surfaces_synth_view") == "on")
	switch {
	case srACMemberTypes[m.typ]:
		return rk.rank(m.value, a.users[m.value], true, false, nil)
	case m.typ == "in":
		// searchStaticValues, then searchChannelsMembersAndWorkspaces
		return append(srACStatic(vals, m.typ, m.value), rk.rankIn(m.value, a.users[m.value], a.chans[m.value])...)
	case m.typ == "has":
		return append(srACStatic(vals, m.typ, m.value), srACEmoji(p.h, m.value, rk.opts)...)
	case m.typ == "hasmy":
		return srACEmoji(p.h, m.value, rk.opts)
	}
	return srACStatic(vals, m.typ, m.value)
}

// acFetch asks the server for m's people and channels (the Searcher's
// remote tier: edge users/search and channels/search, as the composer's
// lists do), once per query.
func (p *SearchPopover) acFetch(m srACMatch) {
	c := p.h.Core()
	q := m.value
	if c == nil || strings.TrimSpace(acrank.SearcherQuery(q)) == "" {
		return
	}
	a := &p.ac
	if a.asked == nil {
		a.asked, a.users, a.chans = map[string]bool{}, map[string]map[string]core.User{}, map[string][]core.ChannelHit{}
	}
	people := m.kind == srACMember || m.kind == srACFilter && (srACMemberTypes[m.typ] || m.typ == "in")
	chans := m.kind == srACChannel || m.kind == srACFilter && m.typ == "in"
	sq := acrank.SearcherQuery(q)
	if people && !a.asked["u:"+q] {
		a.asked["u:"+q] = true
		run(p.h, func(ctx context.Context) ([]core.User, error) { return c.AutocompleteUsers(ctx, sq, "") },
			func(us []core.User, err error) {
				if p.done || err != nil {
					return
				}
				hits := make(map[string]core.User, len(us))
				for _, u := range us {
					hits[u.ID] = u
				}
				a.users[q] = hits
				p.acRefresh(q)
			})
	}
	if chans && !a.asked["c:"+q] {
		a.asked["c:"+q] = true
		run(p.h, func(ctx context.Context) ([]core.ChannelHit, error) { return c.AutocompleteChannels(ctx, sq) },
			func(hs []core.ChannelHit, err error) {
				if p.done || err != nil {
					return
				}
				a.chans[q] = hs
				p.acRefresh(q)
			})
	}
}

// acRefresh re-searches the open match when the server answered its query.
func (p *SearchPopover) acRefresh(q string) {
	a := &p.ac
	if a.m.kind == srACNone || a.m.value != q {
		return
	}
	a.items = p.acItems(a.m)
	if a.sel >= len(a.items) {
		a.sel = max(len(a.items)-1, 0)
	}
	a.open = len(a.items) > 0 && a.m.key() != a.dismissed
	p.h.Invalidate()
}

// acMove steps the selection, wrapping; the first move only turns the
// pseudo-selection into a real one.
func (p *SearchPopover) acMove(d int) {
	a := &p.ac
	a.scroll = true
	if a.pseudo {
		a.pseudo = false
		return
	}
	if n := len(a.items); n > 0 {
		a.sel = ((a.sel+d)%n + n) % n
	}
}

// acDismiss is Escape with the menu open.
func (p *SearchPopover) acDismiss() {
	p.ac.dismissed, p.ac.open = p.ac.m.key(), false
}

// acAccept takes row i: the match's text becomes the token and a space
// (getInsertData; Texty's recordFrecencyEvent for people and channels).
func (p *SearchPopover) acAccept(i int) {
	a := &p.ac
	if !a.open || i < 0 || i >= len(a.items) {
		return
	}
	m, it := a.m, a.items[i]
	tok := srACPick(m, it)
	rs := []rune(p.ed.Text())
	if m.end > len(rs) || m.start > m.end {
		return
	}
	ins := tok.label
	end := m.end
	if end < len(rs) && rs[end] == ' ' {
		end++ // the space is already there
	}
	ins += " "
	delta := len([]rune(ins)) - (end - m.start)
	var toks []srToken
	for _, t := range p.tokens {
		switch {
		case t.end() <= m.start:
			toks = append(toks, t)
		case t.start >= end:
			t.start += delta
			toks = append(toks, t)
		}
	}
	tok.start = m.start
	toks = append(toks, tok)
	sort.Slice(toks, func(i, j int) bool { return toks[i].start < toks[j].start })
	p.tokens = toks
	p.ed.SetCaret(end, m.start)
	p.ed.Insert(ins)
	p.query = p.ed.Text()
	if c := p.h.Core(); c != nil && (it.kind == srAIMember || it.kind == srAIChannel) {
		c.RecordFrecency(it.id, m.match)
	}
	a.m, a.open, a.dismissed, a.items = srACMatch{}, false, "", nil
	if strings.TrimSpace(p.query) != "" {
		p.histMode = false
	}
	p.queryChanged(time.Now())
	p.acUpdate()
}

// srShiftTokens follows an edit of the box from old to cur: tokens before
// or after the changed text stay (moved), the ones it touched are plain
// text again (Slack unwraps a filter whose text changed).
func srShiftTokens(toks []srToken, old, cur string) []srToken {
	if len(toks) == 0 {
		return nil
	}
	o, n := []rune(old), []rune(cur)
	pre := 0
	for pre < len(o) && pre < len(n) && o[pre] == n[pre] {
		pre++
	}
	suf := 0
	for suf < len(o)-pre && suf < len(n)-pre && o[len(o)-1-suf] == n[len(n)-1-suf] {
		suf++
	}
	oEnd, delta := len(o)-suf, len(n)-len(o)
	var out []srToken
	for _, t := range toks {
		switch {
		case t.end() <= pre:
		case t.start >= oEnd:
			t.start += delta
		default:
			continue
		}
		if t.start < 0 || t.end() > len(n) || string(n[t.start:t.end()]) != t.label {
			continue
		}
		out = append(out, t)
	}
	return out
}

// srRequestQuery is the query the search gets: the box's text with each
// token's label replaced by its request form ("from:<@U…>").
func srRequestQuery(text string, toks []srToken) string {
	if len(toks) == 0 {
		return text
	}
	rs := []rune(text)
	var b strings.Builder
	at := 0
	for _, t := range toks {
		if t.start < at || t.end() > len(rs) || string(rs[t.start:t.end()]) != t.label {
			continue
		}
		b.WriteString(string(rs[at:t.start]))
		b.WriteString(t.req)
		at = t.end()
	}
	b.WriteString(string(rs[at:]))
	return b.String()
}

// reqQuery is srRequestQuery for the box.
func (p *SearchPopover) reqQuery(text string) string { return srRequestQuery(text, p.tokens) }

// ---- drawing ----

// acSelfMention reports a mention of the viewer (c-member_slug--mention).
func (p *SearchPopover) acSelfMention(t srToken) bool {
	c := p.h.Core()
	return c != nil && t.req == "<@"+c.Self().ID+">"
}

// acAnchorX is the x of the match's anchor in the editor.
func (p *SearchPopover) acAnchorX() int {
	at := p.ac.m.anchor
	if at <= 0 {
		return 0
	}
	regs := p.ed.Regions(at-1, at, nil)
	x := 0
	for _, r := range regs {
		x = max(x, r.Bounds.Max.X)
	}
	return x
}

// acLayout draws the open menu under the input row (window coordinates).
func (p *SearchPopover) acLayout(gtx layout.Context, th *theme.Theme, cl *Colors, win image.Point, inputH int) {
	a := &p.ac
	p.acPointer(gtx)
	if !a.open {
		return
	}
	n := len(a.items)
	rowH, pad, footH := dp(gtx, 40), dp(gtx, 8), dp(gtx, 32)
	content := 2*pad + n*rowH
	bodyH := min(content, dp(gtx, 260)-footH)
	w := min(dp(gtx, 480), win.X)
	ax := p.rect.Min.X + p.edX + p.acAnchorX()
	left := max(p.rect.Min.X+dp(gtx, 8), min(ax-dp(gtx, 17), p.rect.Max.X-dp(gtx, 8)-w))
	top := p.rect.Min.Y + inputH
	if bodyH+footH > win.Y-top {
		bodyH = max(win.Y-top-footH, rowH)
	}
	r := image.Rect(left, top, left+w, top+bodyH+footH)
	rad := dp(gtx, 8)
	shape := func(r image.Rectangle, rad int) clip.RRect { return clip.RRect{Rect: r, SE: rad, SW: rad} }
	func() {
		// the shadow and ring below and at the sides (clip-path inset(1px -10px -10px))
		m := dp(gtx, 10)
		defer clip.Rect(image.Rect(r.Min.X-m, r.Min.Y+1, r.Max.X+m, r.Max.Y+m)).Push(gtx.Ops).Pop()
		softShadow(gtx, r, rad, dp(gtx, 4), dp(gtx, 12), rgba(0x000000, .08))
		func() {
			defer shape(r.Inset(-1), rad+1).Push(gtx.Ops).Pop()
			fill(gtx, r.Inset(-1), cl.Ring)
		}()
		func() {
			defer shape(r, rad).Push(gtx.Ops).Pop()
			fill(gtx, r, cl.Bg)
		}()
	}()
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	box := image.Rectangle{Max: r.Size()}
	defer shape(box, rad).Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &a.bg)
	for {
		if _, ok := gtx.Event(pointer.Filter{Target: &a.bg, Kinds: pointer.Press | pointer.Release}); !ok {
			break
		}
	}
	for len(a.tags) < n {
		a.tags = append(a.tags, &srACRowTag{i: len(a.tags)})
	}
	a.rowPx, a.rowW = rowH, w
	p.acKeepVisible(pad, rowH, bodyH)
	func() {
		gtx := gtx
		gtx.Constraints = layout.Exact(image.Pt(w, bodyH))
		defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
		a.list.Axis = layout.Vertical
		a.list.Layout(gtx, n+2, func(gtx layout.Context, i int) layout.Dimensions {
			if i == 0 || i == n+1 {
				return layout.Dimensions{Size: image.Pt(w, pad)}
			}
			return p.acRow(gtx, th, cl, i-1, w, rowH)
		})
	}()
	p.acFooter(gtx, th, cl, image.Rect(0, bodyH, w, bodyH+footH))
}

// acKeepVisible scrolls to the selection after keyboard moves.
func (p *SearchPopover) acKeepVisible(pad, rowH, bodyH int) {
	a := &p.ac
	if !a.scroll {
		return
	}
	a.scroll = false
	pos := &a.list.Position
	top := pos.Offset
	if pos.First > 0 {
		top += pad + (pos.First-1)*rowH
	}
	r0 := pad + a.sel*rowH
	r1 := r0 + rowH
	switch {
	case a.sel == 0:
		top = 0
	case r0 < top:
		top = r0
	case r1 > top+bodyH:
		top = r1 - bodyH
	default:
		return
	}
	if top < pad {
		pos.First, pos.Offset = 0, top
	} else {
		pos.First, pos.Offset = (top-pad)/rowH+1, (top-pad)%rowH
	}
}

// acPointer applies last frame's pointer input: moving over a row selects
// it, a press and release on the same row takes it.
func (p *SearchPopover) acPointer(gtx layout.Context) {
	a := &p.ac
	for i := range a.tags {
		for {
			ev, ok := gtx.Event(pointer.Filter{Target: a.tags[i],
				Kinds: pointer.Move | pointer.Enter | pointer.Press | pointer.Release | pointer.Cancel})
			if !ok {
				break
			}
			e, ok := ev.(pointer.Event)
			if !ok || i >= len(a.items) || !a.open {
				continue
			}
			switch e.Kind {
			case pointer.Move, pointer.Enter:
				a.sel, a.pseudo = i, false
			case pointer.Press:
				if e.Buttons.Contain(pointer.ButtonPrimary) || e.Source == pointer.Touch {
					a.pressed = i + 1
					a.sel, a.pseudo = i, false
				}
			case pointer.Release:
				in := e.Position.X >= 0 && e.Position.Y >= 0 && e.Position.X < float32(a.rowW) && e.Position.Y < float32(a.rowPx)
				if a.pressed == i+1 && in {
					a.pressed = 0
					p.acAccept(i)
					return
				}
				a.pressed = 0
			case pointer.Cancel:
				a.pressed = 0
			}
		}
	}
}

// acRow draws row i.
func (p *SearchPopover) acRow(gtx layout.Context, th *theme.Theme, cl *Colors, i, w, rowH int) layout.Dimensions {
	a := &p.ac
	it := a.items[i]
	hot := i == a.sel
	sel := hot && !a.pseudo
	sz := image.Pt(w, rowH)
	switch {
	case sel:
		fill(gtx, image.Rectangle{Max: sz}, cl.Highlight)
	case hot:
		fill(gtx, image.Rectangle{Max: sz}, cl.Selected)
	}
	area := clip.Rect{Max: sz}.Push(gtx.Ops)
	event.Op(gtx.Ops, a.tags[i])
	pointer.CursorPointer.Add(gtx.Ops)
	area.Pop()
	fg, fg2 := cl.Fg, cl.Fg2
	if sel {
		fg, fg2 = cl.HighText, cl.HighText
	}
	x, right := dp(gtx, 16), w-dp(gtx, 16)
	if hot && !sel {
		right -= srEnterChip(gtx, th, cl, right, rowH) + dp(gtx, 16)
	}
	draw := func(size float32, wt font.Weight, col color.NRGBA, s string) {
		if x >= right || s == "" {
			return
		}
		d := at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(right-x, rowH)}
			return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, size, wt, col, s)
			})
		})
		x += d.Size.X
	}
	square := func(s int) image.Point {
		pt := image.Pt(x, (rowH-s)/2)
		return pt
	}
	switch it.kind {
	case srAIValue:
		if it.icon != "" {
			is := dp(gtx, 18)
			at(gtx, square(is), func(gtx layout.Context) layout.Dimensions { return icon(gtx, it.icon, 18, fg) })
			x += is + dp(gtx, 12)
		}
		draw(16, theme.Bold, fg, it.label)
	case srAIEmoji:
		es := dp(gtx, 22)
		at(gtx, square(es), func(gtx layout.Context) layout.Dimensions { return emojiImage(gtx, p.h, it.id, 22) })
		x += es + dp(gtx, 12)
		draw(16, theme.Bold, fg, ":"+it.label+":")
	case srAIChannel:
		is := dp(gtx, 18)
		ic := "hash"
		if it.private {
			ic = "lock"
		}
		at(gtx, square(is), func(gtx layout.Context) layout.Dimensions { return icon(gtx, ic, 18, fg) })
		x += is + dp(gtx, 8)
		draw(15, theme.Bold, fg, it.label)
	case srAIMpim:
		// TextyAutocompleteChannelResult's SmallMpimListEntity: the
		// group-DM tile, then the members' names
		av := dp(gtx, 20)
		rowBg := cl.Bg
		switch {
		case sel:
			rowBg = cl.Highlight
		case hot:
			rowBg = cl.Selected
		}
		at(gtx, square(av), func(gtx layout.Context) layout.Dimensions {
			return mpimTile(gtx, p.h, it.members, 20, rowBg, sel)
		})
		x += av + dp(gtx, 8)
		draw(15, theme.Bold, fg, it.label)
	case srAITeam:
		// SmallTeamListEntity: the team icon, then its name
		av := dp(gtx, 20)
		pfTeamIcon(gtx, p.h, th, it.label, it.teamIcon, square(av), 20, false)
		x += av + dp(gtx, 8)
		draw(15, theme.Bold, fg, it.label)
	default:
		av := dp(gtx, 20)
		at(gtx, square(av), func(gtx layout.Context) layout.Dimensions { return avatar(gtx, p.h, it.avatar, 20) })
		x += av + dp(gtx, 8)
		draw(15, theme.Bold, fg, it.label)
		if it.self {
			x += dp(gtx, 4)
			draw(15, theme.Bold, fg, "(you)")
		}
		x += dp(gtx, 4)
		if !it.bot && x < right {
			box := dp(gtx, 20)
			d := dp(gtx, 9)
			c := image.Pt(x+box/2, rowH/2)
			at(gtx, c.Sub(image.Pt(d/2, d/2)), func(gtx layout.Context) layout.Dimensions {
				if sel && it.presence != "active" {
					stroke(gtx, image.Rect(0, 0, d, d), d/2, border1(gtx), fg)
					return layout.Dimensions{Size: image.Pt(d, d)}
				}
				return presenceDot(gtx, th, it.presence == "active", 9)
			})
			x += box + dp(gtx, 4)
		}
		draw(15, theme.Regular, fg2, it.alt)
	}
	return layout.Dimensions{Size: sz}
}

// acFooter is TextyAutocompleteMenu's keyboard help (show_autocomplete_help):
// "↑ ↓ to navigate  ↵ to select  esc to dismiss" (search variant: the
// primary background, content-sec text, a 1px otl-ter line above).
func (p *SearchPopover) acFooter(gtx layout.Context, th *theme.Theme, cl *Colors, r image.Rectangle) {
	fill(gtx, r, cl.Bg)
	fill(gtx, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+border1(gtx)), cl.Divider)
	x := r.Min.X + dp(gtx, 20)
	h := r.Dy()
	glyph := func(name string, s float32) {
		px := dp(gtx, s)
		at(gtx, image.Pt(x, r.Min.Y+(h-px)/2), func(gtx layout.Context) layout.Dimensions { return icon(gtx, name, s, cl.Fg2) })
		x += px + dp(gtx, 3)
	}
	word := func(wt font.Weight, s string) {
		d := at(gtx, image.Pt(x, r.Min.Y), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 12, wt, cl.Fg2, s)
			})
		})
		x += d.Size.X
	}
	glyph("arrow-up", 10)
	glyph("arrow-down", 10)
	word(theme.Regular, "to navigate")
	x += dp(gtx, 16)
	glyph("enter", 12)
	word(theme.Regular, "to select")
	x += dp(gtx, 16)
	word(theme.Bold, "esc")
	x += dp(gtx, 3)
	word(theme.Regular, "to dismiss")
}
