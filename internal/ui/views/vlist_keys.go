package views

// Keyboard navigation for the rail list views (Threads, Drafts & sent,
// DMs), ported from Slack's virtual list: client-boot 67/z
// BaseKeyboardNavigableList, the base of FSLM BaseDynamicList (build
// a84662d2):
//
//   - ↑/↓ with the list focused move the active item to the previous or
//     next focusable row (isFocusableItem) and scroll it to the top of
//     the list unless its top is already in view (focusNextItem,
//     scrollToKey). With no active item the first press takes the
//     initial active item if it's in view, else the first focusable row
//     in view (getNextActiveItem).
//   - PageUp/PageDown scroll a page (the list height − 21): focused,
//     PageDown activates the last focusable row of the new view and
//     PageUp the first one, scrolled to the top (onKeyDownPageUp/Down;
//     at the top or bottom they take the first or last row in view).
//   - Home/End activate the first/last focusable row and scroll to it
//     (in view at the top or bottom already: the first/last row in view).
//   - Without focus (useGlobalKeyboardShortcuts, nothing focused) ↑/↓
//     scroll 40px, PageUp/PageDown a page and Home/End to either end.
//   - Enter on the active row is a click (ListItem.onKeyDown calls
//     onClick after onItemKeyDown).
//   - A press on a row makes it the active item without the ring
//     (onItemMouseDown, then the item's focus); losing focus drops the
//     active item and remembers it as the initial one (onFocusLeave).
//   - The ring (.c-virtual_list__item--focus:focus:after) is drawn on the
//     active row only in keyboard mode (WithKeyboardFocusListItem's
//     hasKeyboardFocusVisual): 1px otl-hgl-1 plus 5px of it at 30%,
//     radius 8, inset 4px; views move the inset as Slack's CSS does.

import (
	"image"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/popups"
)

// vkItem is one row of the virtual list: a key, the widget.List element
// that draws it and whether the keyboard stops on it.
type vkItem struct {
	key       string
	elem      int
	focusable bool
	noInitial bool // keysToExcludeFromRovingTabIndex
}

// vkList is a list's keyboard state (Slack's activeItem,
// initialActiveItem, hasKeyboardFocus and the keyboard mode).
type vkList struct {
	list    *widget.List
	items   []vkItem
	idx     map[string]int
	focused bool   // the list holds key focus
	active  string // activeItem ("": none)
	initial string // initialActiveItem
	ring    bool   // keyboard mode: the active row shows the ring
	focusIt bool   // take key focus next frame
	viewH   int    // the list viewport height (px)

	sizes, prev map[int]int               // element heights this frame / last frame
	subs        map[string][2]int         // a row's top and height inside its element (sub-rows)
	pending     int                       // vkPageFirst / vkPageLast after a page scroll
	pressed     bool                      // a press focused the list this frame
	onFocus     func(key string, kb bool) // onItemFocusEnter
}

const (
	vkNone = iota
	vkPageFirst
	vkPageLast
)

// vkOpts are the list's per-view hooks.
type vkOpts struct {
	// Click is onItemClick: Enter on the active row (and the view's own
	// clicks elsewhere).
	Click func(key string)
	// Key handles the view's keyCommands (extra filters) for the active
	// row; it reports whether it used the key.
	Key   func(e key.Event, active string) bool
	Extra []key.Filter
	// NoGlobal leaves the keys alone while nothing is focused (lists
	// without useGlobalKeyboardShortcuts).
	NoGlobal bool
}

func newVKList(l *widget.List) *vkList {
	return &vkList{list: l, idx: map[string]int{}, sizes: map[int]int{}, prev: map[int]int{}, subs: map[string][2]int{}}
}

// setItems installs this frame's rows; an active or initial item that's
// gone is dropped (onFocusedElementRemoved refocuses the list).
func (k *vkList) setItems(items []vkItem) {
	k.items = items
	clear(k.idx)
	for i, it := range items {
		k.idx[it.key] = i
	}
	if _, ok := k.idx[k.active]; !ok {
		k.active = ""
	}
	if _, ok := k.idx[k.initial]; !ok {
		k.initial = ""
	}
}

// begin starts a frame's layout: element sizes are measured afresh.
func (k *vkList) begin() {
	k.prev, k.sizes = k.sizes, k.prev
	clear(k.sizes)
}

// elem records an element's height as the list lays it out.
func (k *vkList) elem(i int, d layout.Dimensions) layout.Dimensions {
	k.sizes[i] = d.Size.Y
	return d
}

// sub records a sub-row's top and height within its element.
func (k *vkList) sub(key string, top, h int) { k.subs[key] = [2]int{top, h} }

func (k *vkList) size(e int) (int, bool) {
	if h, ok := k.sizes[e]; ok {
		return h, true
	}
	h, ok := k.prev[e]
	return h, ok
}

// top is an item's top in the viewport (ok false when its element isn't
// laid out; above reports which side it's on then).
func (k *vkList) top(key string) (y int, ok, above bool) {
	i, found := k.idx[key]
	if !found {
		return 0, false, false
	}
	it := k.items[i]
	p := k.list.Position
	if it.elem < p.First {
		return 0, false, true
	}
	if it.elem >= p.First+p.Count {
		return 0, false, false
	}
	y = -p.Offset
	for e := p.First; e < it.elem; e++ {
		h, ok := k.size(e)
		if !ok {
			return 0, false, false
		}
		y += h
	}
	if s, ok := k.subs[key]; ok {
		y += s[0]
	}
	return y, true, false
}

func (k *vkList) height(key string) int {
	if s, ok := k.subs[key]; ok {
		return s[1]
	}
	if i, ok := k.idx[key]; ok {
		h, _ := k.size(k.items[i].elem)
		return h
	}
	return 0
}

// inView is isItemInView: the item's top is inside the viewport.
func (k *vkList) inView(key string) bool {
	y, ok, _ := k.top(key)
	return ok && y >= 0 && y <= k.viewH
}

// fullyInView is getVisibleRange's test: neither edge cut off (1px slack).
func (k *vkList) fullyInView(key string) bool {
	y, ok, _ := k.top(key)
	return ok && y >= -1 && y+k.height(key) <= k.viewH+1
}

// inViewFocusable is getFocusableItemsInView.
func (k *vkList) inViewFocusable() []string {
	var out []string
	for _, it := range k.items {
		if it.focusable && k.fullyInView(it.key) {
			out = append(out, it.key)
		}
	}
	if len(out) == 0 { // one tall row (start == end): the one under the top
		for _, it := range k.items {
			if y, ok, _ := k.top(it.key); it.focusable && ok && y <= 0 && y+k.height(it.key) > 0 {
				return []string{it.key}
			}
		}
	}
	return out
}

func (k *vkList) focusableAt(i int) bool {
	return i >= 0 && i < len(k.items) && k.items[i].focusable
}

// next is getNextActiveItem: up (d < 0) or down from the active item.
func (k *vkList) next(d int) string {
	if k.active == "" {
		if k.initial != "" && k.inView(k.initial) {
			return k.initial
		}
		return k.firstInView()
	}
	i, ok := k.idx[k.active]
	if !ok {
		return ""
	}
	for j := i + d; j >= 0 && j < len(k.items); j += d {
		if k.focusableAt(j) {
			return k.items[j].key
		}
	}
	return ""
}

// firstInView is getNextFirstFocusableItemInView.
func (k *vkList) firstInView() string {
	v := k.inViewFocusable()
	if len(v) == 0 || v[0] == k.active {
		if k.active != "" {
			return k.next(-1)
		}
		return ""
	}
	return v[0]
}

// lastInView is getNextLastFocusableItemInView.
func (k *vkList) lastInView() string {
	v := k.inViewFocusable()
	if len(v) == 0 || v[len(v)-1] == k.active {
		if k.active != "" {
			return k.next(1)
		}
		return ""
	}
	return v[len(v)-1]
}

// scrollTo is scrollToKey: the item's top at the top of the list.
func (k *vkList) scrollTo(key string) {
	i, ok := k.idx[key]
	if !ok {
		return
	}
	off := 0
	if s, ok := k.subs[key]; ok {
		off = s[0]
	}
	k.list.Position.First = k.items[i].elem
	k.list.Position.Offset = off
	k.list.Position.BeforeEnd = true
}

func (k *vkList) scrollBy(px int) {
	k.list.Position.Offset += px
	k.list.Position.BeforeEnd = true
}

func (k *vkList) atTop() bool {
	p := k.list.Position
	return p.First == 0 && p.Offset <= 1
}

func (k *vkList) atBottom() bool {
	p := k.list.Position
	n := 0
	if len(k.items) > 0 {
		n = k.items[len(k.items)-1].elem + 1
	}
	return p.First+p.Count >= n && p.OffsetLast >= -1
}

// setActive is setActiveItem (with the keyboard mode: ring).
func (k *vkList) setActive(key string, ring bool) {
	if key == "" {
		return
	}
	k.active, k.ring = key, ring
	if k.validInitial(key) {
		k.initial = key
	}
	if k.onFocus != nil {
		k.onFocus(key, ring)
	}
}

// focusOn is focusOnKey: activate key (ring on) and scroll to it unless
// it's in view; it takes key focus.
func (k *vkList) focusOn(key string) bool {
	if _, ok := k.idx[key]; !ok {
		return false
	}
	k.setActive(key, true)
	if !k.inView(key) {
		k.scrollTo(key)
	}
	k.focusIt = true
	return true
}

// validInitial is isValidInitialActiveItem.
func (k *vkList) validInitial(key string) bool {
	i, ok := k.idx[key]
	return ok && k.items[i].focusable && !k.items[i].noInitial
}

// reveal scrolls key into view the least it can (the browser's focus()
// scrolling, block "nearest").
func (k *vkList) reveal(key string) {
	i, ok := k.idx[key]
	if !ok {
		return
	}
	y, laid, above := k.top(key)
	h := k.height(key)
	switch {
	case laid && y < 0, !laid && above:
		k.scrollTo(key)
	case laid && y+h > k.viewH:
		k.scrollBy(y + h - k.viewH)
	case !laid && h == 0: // never measured
		k.scrollTo(key)
	case !laid: // below: its bottom at the bottom of the list
		off := 0
		if s, ok := k.subs[key]; ok {
			off = s[0]
		}
		k.list.Position = layout.Position{First: k.items[i].elem, Offset: off + h - k.viewH, BeforeEnd: true}
	}
}

// ringOn reports whether key's row draws the focus ring.
func (k *vkList) ringOn(key string) bool {
	return k.focused && k.ring && key != "" && key == k.active
}

func (k *vkList) pageH() int { return max(0, k.viewH-21) }

// update handles key focus, presses and keys; call before layout with
// the list's viewport height.
func (k *vkList) update(gtx C, viewH int, o vkOpts) {
	k.viewH = viewH
	k.pressed = false
	if k.focusIt && gtx.Source.Enabled() {
		k.focusIt = false
		gtx.Execute(key.FocusCmd{Tag: k})
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: k, Kinds: pointer.Press})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok {
			// onItemMouseDown: no keyboard mode; the pressed row becomes
			// the active item as it takes focus
			k.ring, k.pressed = false, true
			if at := k.at(int(e.Position.Y)); at != "" {
				k.setActive(at, false)
			}
			gtx.Execute(key.FocusCmd{Tag: k})
		}
	}
	nav := []key.Name{key.NameUpArrow, key.NameDownArrow, key.NamePageUp, key.NamePageDown, key.NameHome, key.NameEnd}
	filters := []event.Filter{key.FocusFilter{Target: k}}
	for _, n := range nav {
		filters = append(filters, key.Filter{Focus: k, Name: n})
	}
	filters = append(filters, key.Filter{Focus: k, Name: key.NameReturn}, key.Filter{Focus: k, Name: key.NameEnter})
	for _, f := range o.Extra {
		filters = append(filters, f)
	}
	for {
		ev, ok := gtx.Event(filters...)
		if !ok {
			break
		}
		switch e := ev.(type) {
		case key.FocusEvent:
			if e.Focus && !k.focused && !k.pressed && k.active == "" {
				// focus(): the initial item if it's in view, else the
				// first one in view
				if k.initial != "" && k.inView(k.initial) {
					k.setActive(k.initial, true)
				} else if f := k.firstInView(); f != "" {
					k.setActive(f, true)
					k.scrollTo(f)
				}
			}
			if !e.Focus && k.focused {
				if k.validInitial(k.active) {
					k.initial = k.active
				}
				k.active = ""
			}
			k.focused = e.Focus
		case key.Event:
			if e.State != key.Press {
				continue
			}
			k.ring = true
			if o.Key != nil && o.Key(e, k.active) {
				continue
			}
			k.key(e, o)
		}
	}
	// useGlobalKeyboardShortcuts: with nothing focused the keys scroll
	for !o.NoGlobal && gtx.Focused(nil) {
		var fs []event.Filter
		for _, n := range nav {
			fs = append(fs, key.Filter{Name: n})
		}
		ev, ok := gtx.Event(fs...)
		if !ok {
			break
		}
		if e, ok := ev.(key.Event); ok && e.State == key.Press {
			k.scrollKey(e.Name)
		}
	}
}

func (k *vkList) key(e key.Event, o vkOpts) {
	switch e.Name {
	case key.NameUpArrow, key.NameDownArrow:
		d := 1
		if e.Name == key.NameUpArrow {
			d = -1
		}
		if n := k.next(d); n != "" {
			k.setActive(n, true)
			if !k.inView(n) {
				k.scrollTo(n)
			}
		}
	case key.NamePageUp:
		if k.atTop() {
			if v := k.inViewFocusable(); len(v) > 0 && v[0] != k.active {
				k.setActive(v[0], true)
			}
			return
		}
		k.scrollBy(-k.pageH())
		k.pending = vkPageFirst
	case key.NamePageDown:
		if k.atBottom() {
			if v := k.inViewFocusable(); len(v) > 0 && v[len(v)-1] != k.active {
				k.setActive(v[len(v)-1], true)
			}
			return
		}
		k.scrollBy(k.pageH())
		k.pending = vkPageLast
	case key.NameHome:
		if k.atTop() {
			if v := k.inViewFocusable(); len(v) > 0 && v[0] != k.active {
				k.setActive(v[0], true)
			}
			return
		}
		for _, it := range k.items {
			if it.focusable {
				k.setActive(it.key, true)
				k.scrollTo(it.key)
				return
			}
		}
	case key.NameEnd:
		if k.atBottom() {
			if v := k.inViewFocusable(); len(v) > 0 && v[len(v)-1] != k.active {
				k.setActive(v[len(v)-1], true)
			}
			return
		}
		for i := len(k.items) - 1; i >= 0; i-- {
			if k.items[i].focusable {
				k.setActive(k.items[i].key, true)
				k.scrollTo(k.items[i].key)
				return
			}
		}
	case key.NameReturn, key.NameEnter:
		if k.active != "" && o.Click != nil {
			o.Click(k.active)
		}
	}
}

// scrollKey is the unfocused list's scrolling.
func (k *vkList) scrollKey(n key.Name) {
	switch n {
	case key.NameUpArrow:
		k.scrollBy(-40)
	case key.NameDownArrow:
		k.scrollBy(40)
	case key.NamePageUp:
		k.scrollBy(-k.pageH())
	case key.NamePageDown:
		k.scrollBy(k.pageH())
	case key.NameHome:
		k.list.Position = layout.Position{BeforeEnd: true}
	case key.NameEnd:
		n := 0
		if len(k.items) > 0 {
			n = k.items[len(k.items)-1].elem + 1
		}
		k.list.Position = layout.Position{First: n}
	}
}

// at is the focusable row under y (viewport px), from the last layout.
func (k *vkList) at(y int) string {
	best := ""
	for _, it := range k.items {
		t, ok, _ := k.top(it.key)
		if !ok {
			continue
		}
		if y >= t && y < t+k.height(it.key) {
			if it.focusable {
				best = it.key
			}
			if _, sub := k.subs[it.key]; sub {
				return best
			}
		}
	}
	return best
}

// area registers the list's tag over r, above the rows (presses pass
// through to them), so a press focuses the list; call after the list's
// layout.
func (k *vkList) area(gtx C, r image.Rectangle) {
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, k)
}

// after resolves a page scroll's focus once the list has moved.
func (k *vkList) after() {
	switch k.pending {
	case vkPageFirst:
		if f := k.firstInView(); f != "" {
			k.setActive(f, true)
			k.scrollTo(f)
		}
	case vkPageLast:
		if l := k.lastInView(); l != "" {
			k.setActive(l, true)
		}
	}
	k.pending = vkNone
}

// vkRing draws Slack's list focus ring on r (the row's box after its
// inset), radius rad.
func (b *base) vkRing(gtx C, r image.Rectangle, rad int) {
	popups.FocusRing(gtx, b.th, r, rad)
}
