package messages

// Find in thread: Slack's ThreadSearchButton and ThreadSearchBar
// (client-boot-shared puxw), useThreadSearch and the threadMetadata
// search actions (CSo3) and selectors (lmNU), under the find_in_thread
// experiment (off for the account; Taut's ThreadJump forces it on with
// its findInThread option).
//
//   - ThreadSearchButton, in the header row before More actions: Slack
//     Kit's IconButton (36px, search, className p-flexpane_header__control)
//     in a Tooltip "Search in thread" (delay 300, bottom). A click is
//     openThreadSearch: an empty query (the bar shows) and the input
//     focused. Mod+F does the same while the pane has the focus (rn2N).
//   - ThreadSearchBar, over p-threads_flexpane__content's top right
//     (.bar__fJyxQ: absolute, top 8 right 8, 33% wide within 300px and
//     100% - 16px, padding 4px 8px, a 1px otl-ter border, radius 8,
//     base-pry, two drop shadows, a flex row 4px apart): the small
//     TextInput (borderless, padding-right 80, placeholder "Search in
//     thread") with MatchCount 12px from its right (13px content-ter: the
//     small spinner while searching, "No results", "{current} of
//     {total}"), then the small IconButtons caret-up "Older result",
//     caret-down "Newer result" (aria-disabled while searching or without
//     a query) and close "Close search".
//   - useThreadSearch: a query shows the spinner at once and is searched
//     300ms after the last change (searchInThread: core.SearchInThread);
//     an answer sets the results (newest first), the highlight words
//     ([query]) and index 0, and scrolls the first 200px below the list's
//     top and flashes it (flashSearchResult: p-threads_flexpane__find_flash,
//     3s). Enter is the next (older) result, Shift+Enter the previous,
//     both wrapping round; Escape and Close search clear the query
//     (results and highlights go) and the focus goes back where it was
//     (useRestoreFocus).
//   - every message of the thread with blocks marks the query in its rich
//     text (richtext.FindHighlighted), the current result's in the darker
//     colour (c-message_kit__blocks--focused-result).

import (
	"context"
	"image"
	"image/color"
	"math"
	"sync/atomic"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const (
	// findDebounce is useThreadSearch's debounce of the query.
	findDebounce = 300 * time.Millisecond
	// findFlashFor is the flash's animation (3s forwards).
	findFlashFor = 3 * time.Second
)

// threadFind is one thread's search: Slack's threadMetadata searchQuery
// (active and query), searchResults, searchResultsHighlightWords (words),
// searchResultIndex, searchLoading and searchFocusKey (focus).
type threadFind struct {
	active  bool
	ed      widget.Editor
	query   string
	results []string // timestamps, newest first
	words   string   // the query the results are for, "" without results
	index   int
	loading bool
	focus   bool // focus the input in the next frame
	restore bool // the bar took the focus from the reply composer
	due     time.Time
	gen     uint64 // the search an answer must be for
	cancel  context.CancelFunc
	answer  atomic.Pointer[findAnswer]

	btn               widget.Clickable // ThreadSearchButton
	btnRect           image.Rectangle  // where it was drawn unpatched, view coordinates (tests)
	tip               popups.TipState
	up, down, closeBn widget.Clickable
	block             bool // the bar's area's tag: the list under it gets no pointer
	bar               image.Rectangle
}

type findAnswer struct {
	gen     uint64
	query   string
	results []string
	err     error
}

// findOn is experimentFindInThreadGroupOn.
func (v *ThreadView) findOn() bool {
	app := v.h.Core()
	return app != nil && app.Experiment("find_in_thread") == "on"
}

// OpenFind is Mod+F with the thread pane focused under find_in_thread
// (rn2N's openThreadSearch): the bar shows with an empty query and its
// input focused, and closing it focuses the reply composer again. False
// when the experiment is off (Mod+F is then the window's own find).
func (v *ThreadView) OpenFind() bool {
	if !v.findOn() {
		return false
	}
	v.openFind(true)
	return true
}

// FindBar is the search bar as drawn last frame, in window coordinates
// (empty while it isn't shown).
func (v *ThreadView) FindBar() image.Rectangle {
	if !v.find.active || v.find.bar.Empty() {
		return image.Rectangle{}
	}
	return v.find.bar.Add(v.Origin)
}

// FindState is the search's query, results (newest first) and current
// index, for tests and the ui root.
func (v *ThreadView) FindState() (query string, results []string, index int, loading bool) {
	f := &v.find
	return f.query, f.results, f.index, f.loading
}

// SetFindQuery types q into the open bar (tests).
func (v *ThreadView) SetFindQuery(q string) { v.find.ed.SetText(q) }

// FindFocused reports whether the key focus is in the open bar's input.
func (v *ThreadView) FindFocused(gtx layout.Context) bool {
	return v.find.active && gtx.Focused(&v.find.ed)
}

func (v *ThreadView) openFind(restore bool) {
	f := &v.find
	f.ed.SingleLine = true
	if !f.active {
		f.restore = restore
	}
	f.active, f.focus = true, true
	f.ed.SetText("")
	v.setFindQuery("", time.Time{})
	v.h.Invalidate()
}

// closeFind is setThreadSearchQuery(null): no query, results or
// highlights; with restore the focus goes back to the reply composer when
// the bar took it from there.
func (v *ThreadView) closeFind(restore bool) {
	f := &v.find
	if !f.active {
		return
	}
	f.active = false
	v.abortFind()
	f.query, f.results, f.words, f.index, f.loading = "", nil, "", 0, false
	f.ed.SetText("")
	if fc, ok := v.h.(ThreadComposerFocuser); ok && restore && f.restore {
		fc.FocusThreadComposer(v.ch, v.ts)
	}
	f.restore = false
	v.h.Invalidate()
}

// abortFind drops the debounced search and the one running.
func (v *ThreadView) abortFind() {
	f := &v.find
	f.due = time.Time{}
	f.gen++
	if f.cancel != nil {
		f.cancel()
		f.cancel = nil
	}
}

// setFindQuery is useThreadSearch's setQuery and its query effect: a
// query shows the spinner and (re)starts the debounce; an empty one ends
// the search with no results.
func (v *ThreadView) setFindQuery(q string, now time.Time) {
	f := &v.find
	f.query = q
	if q == "" {
		v.abortFind()
		f.results, f.words, f.index, f.loading = nil, "", 0, false
		return
	}
	f.loading = true
	f.due = now.Add(findDebounce)
}

// findTick applies a search's answer and starts a due one (UI goroutine,
// every frame the pane draws).
func (v *ThreadView) findTick(gtx layout.Context) {
	f := &v.find
	if f.active && !v.findOn() {
		v.closeFind(false) // the bar unmounts with the experiment
	}
	if a := f.answer.Swap(nil); a != nil && a.gen == f.gen && f.active {
		v.findAnswered(gtx, a)
	}
	if f.due.IsZero() || !f.active {
		return
	}
	if gtx.Now.Before(f.due) {
		gtx.Execute(op.InvalidateCmd{At: f.due})
		return
	}
	f.due = time.Time{}
	v.startFind(f.query)
}

// startFind runs searchInThread for q, aborting the search before it.
func (v *ThreadView) startFind(q string) {
	f := &v.find
	app := v.h.Core()
	if app == nil {
		f.loading = false
		return
	}
	if f.cancel != nil {
		f.cancel()
	}
	f.gen++
	gen := f.gen
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	f.cancel = cancel
	ch, ts, h, ans := v.ch, v.ts, v.h, &f.answer
	go func() {
		defer cancel()
		res, err := app.SearchInThread(ctx, ch, ts, q)
		ans.Store(&findAnswer{gen: gen, query: q, results: res, err: err})
		h.Invalidate()
	}()
}

// findAnswered is the debounced run's end: an error or no match ends with
// no results; matches become the results, the first one scrolled to and
// flashed.
func (v *ThreadView) findAnswered(gtx layout.Context, a *findAnswer) {
	f := &v.find
	f.cancel = nil
	f.loading = false
	if a.err != nil || len(a.results) == 0 {
		f.results, f.words, f.index = nil, "", 0
		return
	}
	f.results, f.words = a.results, a.query
	v.findGo(gtx, 0)
}

// findGo makes result i the current one: setSearchResultIndex, then
// scrollToTs(ts, {scrollOffset: -200}) and flashSearchResult.
func (v *ThreadView) findGo(gtx layout.Context, i int) {
	f := &v.find
	if i < 0 || i >= len(f.results) {
		return
	}
	f.index = i
	ts := f.results[i]
	v.s.scrollToFind(ts, dp(gtx, 200))
	v.s.findFlash, v.s.findFlashAt = ts, gtx.Now
}

// findStep is cycleNext (d 1: Enter, Older result) and cyclePrev (d -1:
// Shift+Enter, Newer result), wrapping round; without results nothing
// (Slack only announces "No results").
func (v *ThreadView) findStep(gtx layout.Context, d int) {
	n := len(v.find.results)
	if n == 0 {
		return
	}
	v.findGo(gtx, ((v.find.index+d)%n+n)%n)
}

// findCurrent is the current result's ts while there are results.
func (v *ThreadView) findCurrent() string {
	f := &v.find
	if f.words == "" || f.index >= len(f.results) {
		return ""
	}
	return f.results[f.index]
}

// searchControlClass is ThreadSearchButton's IconButton.
var searchControlClass = slackc.Class("c-button-unstyled c-icon_button c-icon_button--size_medium c-icon_button--default p-flexpane_header__control",
	"aria-label", "Search in thread").Tag("button")

// layoutSearchButton is ThreadSearchButton's native: under find_in_thread
// (the thread type is supported: isThreadTypeSupported) a 36px IconButton
// (radius 8; content-sec, on hover content-pry over base-ter; a 20px
// search icon) in its tooltip; else nothing (Slack renders null).
func (v *ThreadView) layoutSearchButton(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	if !v.findOn() {
		return layout.Dimensions{}
	}
	f := &v.find
	th := v.h.Theme()
	c := colorsFor(th)
	if f.btn.Clicked(gtx) {
		v.openFind(false)
	}
	st := searchControlClass.Enter(gtx, btnState(gtx, &f.btn), 0)
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{}
	}
	bs := dp(gtx, 36)
	gtx.Constraints = layout.Exact(image.Pt(bs, bs))
	draw := func(gtx layout.Context) layout.Dimensions {
		return f.btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			col := c.Text3
			if f.btn.Hovered() {
				rrect(gtx.Ops, image.Rectangle{Max: image.Pt(bs, bs)}, dp(gtx, 8), st.Background(c.CloseHover))
				col = c.Text
			}
			is := dp(gtx, 20)
			at(gtx, (bs-is)/2, (bs-is)/2, func(gtx layout.Context) layout.Dimensions {
				return icons.Get(icons.Slack("search"))(gtx, unit.Dp(20), st.Color(col))
			})
			return layout.Dimensions{Size: image.Pt(bs, bs)}
		})
	}
	var d layout.Dimensions
	if st.Active() {
		d = st.Wrap(gtx, draw)
	} else {
		d = draw(gtx)
	}
	f.tip.Delay = 300 * time.Millisecond
	popups.Tooltip(gtx, th, &f.tip, f.btn.Hovered(), d.Size, popups.Below, threadsNS.T("Search in thread"), "")
	return d
}

// layoutFindBar draws ThreadSearchBar in the content area (w wide, its
// top-left at the origin) and handles its keys and buttons.
func (v *ThreadView) layoutFindBar(gtx layout.Context, th *theme.Theme, c *colors, w int) {
	f := &v.find
	f.bar = image.Rectangle{}
	// Enter, Shift+Enter and Escape are the bar's (its onKeyDown), not
	// the input's
	for {
		e, ok := gtx.Event(
			key.Filter{Focus: &f.ed, Name: key.NameReturn, Optional: key.ModShift},
			key.Filter{Focus: &f.ed, Name: key.NameEnter, Optional: key.ModShift},
			key.Filter{Focus: &f.ed, Name: key.NameEscape},
		)
		if !ok {
			break
		}
		ke, ok := e.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		switch {
		case ke.Name == key.NameEscape:
			v.closeFind(true)
			return
		case ke.Modifiers.Contain(key.ModShift):
			v.findStep(gtx, -1)
		default:
			v.findStep(gtx, 1)
		}
	}
	disabled := f.loading || f.query == "" // the arrows' aria-disabled
	if f.closeBn.Clicked(gtx) {
		v.closeFind(true)
		return
	}
	if f.up.Clicked(gtx) && !disabled {
		v.findStep(gtx, 1) // Older result
	}
	if f.down.Clicked(gtx) && !disabled {
		v.findStep(gtx, -1) // Newer result
	}

	m8 := dp(gtx, 8)
	bw := max(min(int(float32(w)*0.33), w-2*m8), dp(gtx, 300)) // min-width wins over max-width
	bd := max(1, dp(gtx, 1))
	padX, padY := dp(gtx, 8), dp(gtx, 4)
	ih := dp(gtx, 28) // the small TextInput and IconButtons
	r := image.Rect(w-m8-bw, m8, w-m8, m8+2*bd+2*padY+ih)
	rad := dp(gtx, 8)
	sh := color.NRGBA{A: 0x19}
	cssShadow(gtx.Ops, r, rad, dp(gtx, 2), dp(gtx, 4), -dp(gtx, 2), sh)
	cssShadow(gtx.Ops, r, rad, dp(gtx, 4), dp(gtx, 6), -dp(gtx, 1), sh)
	rrect(gtx.Ops, r, rad, c.Bg)
	border(gtx.Ops, r, rad, bd, c.FindBorder)
	area := clip.Rect(r).Push(gtx.Ops)
	event.Op(gtx.Ops, &f.block)
	area.Pop()
	for {
		if _, ok := gtx.Event(pointer.Filter{Target: &f.block, Kinds: pointer.Press | pointer.Release}); !ok {
			break
		}
	}
	f.bar = r

	// the row: the input's wrap (flex 1), 4px, the three buttons
	bs, gap := ih, dp(gtx, 4)
	navW := 3*bs + 2*gap
	x0, y0 := r.Min.X+bd+padX, r.Min.Y+bd+padY
	iw := max(0, r.Dx()-2*bd-2*padX-gap-navW)

	// the input: 13px/18px, padding 8px 8px 7px and 80px on the right,
	// its line centred in the 28px box
	ts := textStyle{size: 13, col: c.Text, lineH: 18}
	ew := max(0, iw-dp(gtx, 8)-dp(gtx, 80))
	lh := gtx.Sp(unit.Sp(ts.lineH))
	ly := y0 + (ih-lh)/2
	if f.focus {
		gtx.Execute(key.FocusCmd{Tag: &f.ed})
		f.focus = false
	}
	at(gtx, x0+dp(gtx, 8), ly, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Min: image.Pt(ew, 0), Max: image.Pt(ew, lh)}
		if f.ed.Len() == 0 {
			ps := ts
			ps.col = c.FindMuted
			lineBox(gtx, ps, func(gtx layout.Context) layout.Dimensions {
				return label(gtx, th, ps, threadsNS.T("Search in thread"))
			})
		}
		rec := op.Record(gtx.Ops)
		d := th.Editor(gtx, &f.ed, unit.Sp(ts.size), c.Text, c.Selection)
		call := rec.Stop()
		bl := cssBaseline(gtx, ts.size, ts.lineH)
		t := op.Offset(image.Pt(0, bl-(d.Size.Y-d.Baseline))).Push(gtx.Ops)
		call.Add(gtx.Ops)
		t.Pop()
		return layout.Dimensions{Size: image.Pt(ew, lh)}
	})
	if q := f.ed.Text(); q != f.query {
		v.setFindQuery(q, gtx.Now)
	}

	// MatchCount, 12px from the input's right edge, centred
	mx := x0 + iw - dp(gtx, 12)
	switch {
	case f.query == "":
	case f.loading:
		sd := dp(gtx, 12) // InfiniteSpinner size small
		at(gtx, mx-sd, y0+(ih-sd)/2, func(gtx layout.Context) layout.Dimensions {
			findSpinner(gtx, sd, c.FindMuted, c.Link)
			return layout.Dimensions{Size: image.Pt(sd, sd)}
		})
	default:
		txt := threadsNS.T("No results")
		if n := len(f.results); n > 0 {
			txt = threadsNS.T("{current} of {total}", "current", f.index+1, "total", n)
		}
		ms := textStyle{size: 13, col: c.FindMuted, lineH: 18}
		tw := measure(gtx, th, ms, txt).X
		at(gtx, mx-tw, y0+(ih-lh)/2, func(gtx layout.Context) layout.Dimensions {
			return lineBox(gtx, ms, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, ms, txt) })
		})
	}

	// the buttons: small IconButtons (28px, radius 8, a 16px icon)
	nx := r.Max.X - bd - padX - navW
	findButton(gtx, c, &f.up, nx, y0, bs, "caret-up", disabled)
	findButton(gtx, c, &f.down, nx+bs+gap, y0, bs, "caret-down", disabled)
	findButton(gtx, c, &f.closeBn, nx+2*(bs+gap), y0, bs, "close", false)
}

// findButton is a small default IconButton at (x, y): content-sec, on
// hover content-pry over base-ter; disabled (c-button--disabled) at half
// opacity with no hover and no clicks.
func findButton(gtx layout.Context, c *colors, btn *widget.Clickable, x, y, s int, icon string, disabled bool) {
	at(gtx, x, y, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(s, s))
		draw := func(gtx layout.Context) layout.Dimensions {
			col := c.Text3
			if disabled {
				col.A /= 2
			} else if btn.Hovered() {
				rrect(gtx.Ops, image.Rectangle{Max: image.Pt(s, s)}, dp(gtx, 8), c.CloseHover)
				col = c.Text
			}
			is := dp(gtx, 16)
			at(gtx, (s-is)/2, (s-is)/2, func(gtx layout.Context) layout.Dimensions {
				return icons.Get(icons.Slack(icon))(gtx, unit.Dp(16), col)
			})
			return layout.Dimensions{Size: image.Pt(s, s)}
		}
		if disabled {
			return draw(gtx)
		}
		return btn.Layout(gtx, draw)
	})
}

// cssShadow approximates box-shadow 0 dy blur spread c around r (Gio has
// no blur): rounded rects from the spread's edge out through the blur,
// each fainter, under the box.
func cssShadow(ops *op.Ops, r image.Rectangle, radius, dy, blur, spread int, c color.NRGBA) {
	steps := max(blur, 1)
	base := r.Add(image.Pt(0, dy)).Inset(-spread)
	for i := steps; i >= 1; i-- {
		grow := i - steps/2 - 1
		a := float32(c.A) / float32(steps)
		col := c
		col.A = uint8(a + .5)
		rr := base.Inset(-grow)
		if col.A == 0 || rr.Empty() {
			continue
		}
		rrect(ops, rr, max(0, radius+grow), col)
	}
}

// findSpinner is Slack Kit's InfiniteSpinner (size small: 12px, blue) in
// a d box: the track ring (content-ter at .25, stroke 8 of 100) and the
// two quarter arcs (content-hgl-1), the ring's and the tail's, each
// turning once per 1.2s on its own easing. It keeps frames coming.
func findSpinner(gtx layout.Context, d int, track, arc color.NRGBA) {
	sw := float32(d) * 8 / 100
	rr := float32(d) * 35 / 100
	ctr := f32.Pt(float32(d)/2, float32(d)/2)
	t := float64(gtx.Now.UnixMilli()%1200) / 1200
	track.A = uint8(float32(track.A)*0.25 + .5)
	ring := func(a0 float64, col color.NRGBA, sweep float64) {
		var p clip.Path
		p.Begin(gtx.Ops)
		p.MoveTo(f32.Pt(ctr.X+rr*float32(math.Cos(a0)), ctr.Y+rr*float32(math.Sin(a0))))
		p.ArcTo(ctr, ctr, float32(sweep))
		paint.FillShape(gtx.Ops, col, clip.Stroke{Path: p.End(), Width: sw}.Op())
	}
	// two half turns: one ArcTo of a full turn draws nothing
	ring(0, track, math.Pi)
	ring(math.Pi, track, math.Pi)
	// stroke-dasharray 55 200, dashoffset 90: the dash is the quarter
	// before the circle's start, 12 to 3 o'clock, turned by each ring
	q := 1.5 * math.Pi
	ring(q+2*math.Pi*cubicBez(.25, .29, .54, .86, t), arc, math.Pi/2)
	ring(q+2*math.Pi*cubicBez(.41, .24, .64, .69, t), arc, math.Pi/2)
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(16 * time.Millisecond)})
}

// findFlashAlpha is the flash's background at el into its 3s: the
// keyframes 20% and 60% are --dt_color-base-hgl-3, eased (CSS's default
// ease) in from none and back out.
func findFlashAlpha(el time.Duration) float32 {
	t := float64(el) / float64(findFlashFor)
	switch {
	case t <= 0 || t >= 1:
		return 0
	case t < 0.2:
		return float32(cubicBez(.25, .1, .25, 1, t/0.2))
	case t < 0.6:
		return 1
	}
	return float32(1 - cubicBez(.25, .1, .25, 1, (t-0.6)/0.4))
}

// cubicBez evaluates the CSS timing function cubic-bezier(x1,y1,x2,y2) at x.
func cubicBez(x1, y1, x2, y2, x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	bez := func(a, b, t float64) float64 { u := 1 - t; return 3*u*u*t*a + 3*u*t*t*b + t*t*t }
	lo, hi, t := 0.0, 1.0, x
	for i := 0; i < 30; i++ {
		v := bez(x1, x2, t)
		if math.Abs(v-x) < 1e-5 {
			break
		}
		if v < x {
			lo = t
		} else {
			hi = t
		}
		t = (lo + hi) / 2
	}
	return bez(y1, y2, t)
}
