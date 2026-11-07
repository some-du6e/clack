package slackc

import (
	"image"
	"strconv"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc/css"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Pointer and key input become DOM events dispatched to JS, which runs
// capture and bubble itself. Every scope has one pass-through pointer
// handler over everything it drew and does its own hit testing.

type evSig struct {
	t    time.Duration
	kind pointer.Kind
	id   pointer.ID
	src  pointer.Source
	btn  pointer.Buttons
}

// dup reports an event an inner scope already handled.
func (v *View) dup(e *pointer.Event) bool {
	sig := evSig{e.Time, e.Kind, e.PointerID, e.Source, e.Buttons}
	n := min(v.seenN, len(v.seen))
	for i := 0; i < n; i++ {
		if v.seen[i] == sig {
			return true
		}
	}
	v.seen[v.seenN%len(v.seen)] = sig
	v.seenN++
	return false
}

func (s *scope) hitTest(pos f32.Point) *box {
	for i := len(s.hits) - 1; i >= 0; i-- {
		e := &s.hits[i]
		q := pos
		if e.hasInv {
			q = e.inv.Transform(pos)
		}
		if !e.clip.contains(q.X, q.Y) {
			continue
		}
		if e.r.contains(q.X, q.Y) {
			return e.b
		}
	}
	return nil
}

// scrollable returns the innermost scroll container in the hovered chain
// that can still scroll in some direction.
func (v *View) scroller() *box {
	for _, b := range v.hover {
		if b.isScroller() {
			return b
		}
	}
	return nil
}

func (b *box) isScroller() bool {
	st := &b.st
	sy := st.OverflowY == css.OverflowScroll || st.OverflowY == css.OverflowAuto
	sx := st.OverflowX == css.OverflowScroll || st.OverflowX == css.OverflowAuto
	return sy && b.contentH > b.h-b.bd[0]-b.bd[2]+0.5 || sx && b.contentW > b.w-b.bd[1]-b.bd[3]+0.5
}

func (b *box) scrollMax() (float32, float32) {
	return max(b.contentW-(b.w-b.bd[1]-b.bd[3]), 0), max(b.contentH-(b.h-b.bd[0]-b.bd[2]), 0)
}

func (s *scope) events() {
	v := s.v
	gtx := s.gtx
	var sx, sy pointer.ScrollRange
	wheel := false
	for _, b := range v.hover {
		if b.listens&lWheel != 0 {
			wheel = true
		}
	}
	if sc := v.scroller(); sc != nil && sc.scope == s {
		ns := v.state(sc)
		mx, my := sc.scrollMax()
		sx = pointer.ScrollRange{Min: -int(ns.scrollX), Max: int(mx - ns.scrollX + 0.5)}
		sy = pointer.ScrollRange{Min: -int(ns.scrollY), Max: int(my - ns.scrollY + 0.5)}
	} else if wheel {
		sx = pointer.ScrollRange{Min: -1e6, Max: 1e6}
		sy = sx
	}
	moved := false
	var last pointer.Event
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &s.ptag, Kinds: pointer.Move | pointer.Enter | pointer.Leave | pointer.Press | pointer.Release | pointer.Drag | pointer.Scroll | pointer.Cancel, ScrollX: sx, ScrollY: sy})
		if !ok {
			break
		}
		pe, ok := e.(pointer.Event)
		if !ok {
			continue
		}
		if pe.Kind != pointer.Leave && v.dup(&pe) {
			continue
		}
		s.learnOrigin(pe.Position)
		switch pe.Kind {
		case pointer.Move, pointer.Drag, pointer.Enter:
			s.ptr, s.ptrIn = pe.Position, true
			s.move(&pe)
			moved = true
			last = pe
		case pointer.Leave:
			if s.host == nil {
				s.ptrIn = false
				v.setHover(nil, s, &pe)
			}
		case pointer.Press:
			s.ptr = pe.Position
			s.press(&pe)
		case pointer.Release:
			s.release(&pe)
		case pointer.Scroll:
			s.scroll(&pe)
		case pointer.Cancel:
			v.pressK = 0
			v.setActive(nil)
		}
	}
	if moved {
		// mousemove, throttled to one per frame and to listening subtrees
		if t := s.hitTest(last.Position); t != nil && chainListens(t, lMove) {
			v.dispatchPointer(s, t, "mousemove", &last, 0)
			v.dispatchPointer(s, t, "pointermove", &last, 0)
		}
	}
}

func chainListens(t *box, bit uint32) bool {
	for x := t; x != nil; x = x.parent {
		if x.listens&bit != 0 {
			return true
		}
	}
	return false
}

func (s *scope) learnOrigin(local f32.Point) {
	if f := env().PointerWin; f != nil {
		w := f()
		s.origin = f32.Pt(float32(w.X)-local.X, float32(w.Y)-local.Y)
		if !s.originOK {
			s.rectsDirty = true
		}
		s.originOK = true
	}
}

func (s *scope) move(pe *pointer.Event) {
	v := s.v
	pos := pe.Position
	if s.dropdown.open {
		s.dropdownMove(pos)
	}
	// scrollbar drag
	if sc := v.byK[v.dragK]; sc != nil && v.dragK != 0 && pe.Kind == pointer.Drag {
		ns := v.state(sc)
		_, my := sc.scrollMax()
		ch := sc.h - sc.bd[0] - sc.bd[2]
		thh := max(ch*ch/sc.contentH, 20*v.dp)
		if ch-thh > 0 {
			ns.scrollY = clampF((pos.Y-ns.dragOff-sc.ay-sc.bd[0])/(ch-thh)*my, 0, my)
			s.rectsDirty = true
			v.invalidate(s)
		}
		return
	}
	if b := v.byK[v.rangeK]; b != nil && v.rangeK != 0 && pe.Kind == pointer.Drag {
		v.rangeSet(s, b, pos.X, false)
		return
	}
	t := s.hitTest(pos)
	v.setHover(t, s, pe)
}

// setHover updates the hovered chain, sending enter/leave/over/out.
func (v *View) setHover(t *box, s *scope, pe *pointer.Event) {
	chain := v.chainBuf[:0]
	for x := t; x != nil; x = x.parent {
		if x.isElem() {
			chain = append(chain, x)
		}
	}
	v.chainBuf = chain
	if len(chain) == len(v.hover) {
		same := true
		for i := range chain {
			if chain[i] != v.hover[i] {
				same = false
				break
			}
		}
		if same {
			return
		}
	}
	var oldT, newT *box
	if len(v.hover) > 0 {
		oldT = v.hover[0]
	}
	if len(chain) > 0 {
		newT = chain[0]
	}
	for _, o := range v.hover {
		if !inChain(chain, o) {
			v.dispatchPointer(o.scope, o, "mouseleave", pe, 0)
			v.dispatchPointer(o.scope, o, "pointerleave", pe, 0)
			v.setState(o, css.StateHover, false)
		}
	}
	if oldT != newT {
		if oldT != nil {
			v.dispatchPointer(oldT.scope, oldT, "mouseout", pe, 0)
		}
		if newT != nil {
			v.dispatchPointer(s, newT, "mouseover", pe, 0)
		}
	}
	for i := len(chain) - 1; i >= 0; i-- {
		if !inChain(v.hover, chain[i]) {
			v.dispatchPointer(s, chain[i], "mouseenter", pe, 0)
			v.dispatchPointer(s, chain[i], "pointerenter", pe, 0)
			v.setState(chain[i], css.StateHover, true)
		}
	}
	v.hover = append(v.hover[:0], chain...)
	v.hoverK = 0
	if newT != nil {
		v.hoverK = newT.k()
	}
	v.tipUpdate(s)
	v.invalidate(s)
}

func inChain(c []*box, b *box) bool {
	for _, x := range c {
		if x == b {
			return true
		}
	}
	return false
}

// setState flips a dynamic pseudo-class bit on b.
func (v *View) setState(b *box, bit css.State, on bool) {
	ns := v.state(b)
	old := ns.st
	if on {
		ns.st |= bit
	} else {
		ns.st &^= bit
	}
	if bit == css.StateHover {
		ns.hover = on
	}
	b.state = ns.st
	if ns.st != old && (!Styles.Empty() || styleHook != nil) {
		v.needStyle = true
		v.dirtyGen++
	}
}

func (v *View) invalidate(s *scope) {
	if s != nil && s.gtx.Ops != nil {
		s.gtx.Execute(op.InvalidateCmd{})
		return
	}
	invalidate()
}

func (v *View) setActive(t *box) {
	for _, k := range v.activeK {
		if b := v.byK[k]; b != nil {
			v.setState(b, css.StateActive, false)
		}
	}
	v.activeK = v.activeK[:0]
	for x := t; x != nil; x = x.parent {
		if x.isElem() && x.k() != 0 {
			v.activeK = append(v.activeK, x.k())
			v.setState(x, css.StateActive, true)
		}
	}
}

func buttonIndex(b pointer.Buttons) int {
	switch {
	case b&pointer.ButtonSecondary != 0:
		return 2
	case b&pointer.ButtonTertiary != 0:
		return 1
	case b&pointer.ButtonQuaternary != 0:
		return 3
	case b&pointer.ButtonQuinary != 0:
		return 4
	}
	return 0
}

func (s *scope) press(pe *pointer.Event) {
	v := s.v
	pos := pe.Position
	if s.dropdown.open {
		if s.dropdownPress(pos) {
			return
		}
	}
	t := s.hitTest(pos)
	v.setHover(t, s, pe)
	v.pressBtn = pe.Buttons
	v.pressK = 0
	if t != nil {
		v.pressK = t.k()
	}
	// scrollbar thumb
	for x := t; x != nil; x = x.parent {
		if x.ns != nil && x.isScroller() && x.ns.thumb.contains(pos.X, pos.Y) {
			v.dragK = x.k()
			x.ns.dragging = true
			x.ns.dragOff = pos.Y - x.ns.thumb.y0
			return
		}
	}
	if t == nil {
		return
	}
	// focus
	if pe.Buttons&pointer.ButtonPrimary != 0 {
		if f := focusTarget(t); f != nil {
			v.focusBox(s, f)
		} else if v.focusK != 0 {
			s.gtx.Execute(key.FocusCmd{Tag: nil})
		}
		if t.repl == rRange && !t.disabled() {
			v.rangeK = t.k()
			v.rangeSet(s, t, pos.X, false)
		}
	}
	v.setActive(t)
	btn := buttonIndex(pe.Buttons)
	v.dispatchPointer(s, t, "pointerdown", pe, btn)
	v.dispatchPointer(s, t, "mousedown", pe, btn)
	if btn == 2 {
		v.dispatchPointer(s, t, "contextmenu", pe, btn)
	}
}

func (s *scope) release(pe *pointer.Event) {
	v := s.v
	pos := pe.Position
	if v.dragK != 0 {
		if b := v.byK[v.dragK]; b != nil && b.ns != nil {
			b.ns.dragging = false
		}
		v.dragK = 0
		return
	}
	if v.rangeK != 0 {
		if b := v.byK[v.rangeK]; b != nil {
			v.rangeSet(s, b, pos.X, true)
		}
		v.rangeK = 0
	}
	t := s.hitTest(pos)
	btn := buttonIndex(v.pressBtn)
	if t != nil {
		v.dispatchPointer(s, t, "pointerup", pe, btn)
		v.dispatchPointer(s, t, "mouseup", pe, btn)
	}
	pb := v.byK[v.pressK]
	v.pressK = 0
	v.setActive(nil)
	if t == nil || pb == nil {
		return
	}
	target := commonAncestor(pb, t)
	if target == nil {
		return
	}
	if btn == 0 {
		v.click(s, target, pe)
	} else if btn == 1 {
		v.dispatchPointer(s, target, "auxclick", pe, btn)
	}
}

func commonAncestor(a, b *box) *box {
	for x := a; x != nil; x = x.parent {
		for y := b; y != nil; y = y.parent {
			if x == y {
				for x != nil && !x.isElem() {
					x = x.parent
				}
				return x
			}
		}
	}
	return nil
}

// click dispatches click (and dblclick) and runs the default actions:
// links, labels, checkboxes, selects and form submission.
func (v *View) click(s *scope, t *box, pe *pointer.Event) {
	if t.disabled() {
		return
	}
	now := int64(pe.Time / time.Millisecond)
	k := t.k()
	if v.lastClk.k == k && now-v.lastClk.t < 500 {
		v.lastClk.n++
	} else {
		v.lastClk.n = 1
	}
	v.lastClk.k, v.lastClk.t = k, now
	// checkbox/radio state flips before click handlers see it
	ctrl := t
	if t.repl != rCheck && t.repl != rRadio {
		ctrl = nil
		for x := t; x != nil; x = x.parent {
			if x.n != nil && x.n.T == "label" {
				ctrl = v.labelControl(x)
				break
			}
		}
	}
	v.dispatchPointer(s, t, "click", pe, 0)
	if v.lastClk.n == 2 {
		v.dispatchPointer(s, t, "dblclick", pe, 0)
	}
	if ctrl != nil && ctrl != t && !ctrl.disabled() {
		switch ctrl.repl {
		case rCheck, rRadio:
			v.toggle(s, ctrl)
		case rInput, rTextarea, rSelect:
			v.focusBox(s, ctrl)
		}
	} else if ctrl != nil && ctrl == t {
		v.toggle(s, ctrl)
	}
	for x := t; x != nil; x = x.parent {
		if x.n == nil {
			continue
		}
		switch x.n.T {
		case "a":
			if href, ok := x.n.AttrString("href"); ok && href != "" && x.n.On["click"] == 0 {
				followLink(href)
				return
			}
		case "select":
			if x.repl == rSelect {
				s.toggleDropdown(x)
				return
			}
		case "button":
			if typ, _ := x.n.AttrString("type"); typ == "" || typ == "submit" {
				if f := formOf(x); f != nil {
					v.dispatchSimple(f, "submit", nil)
				}
			}
			return
		}
	}
}

func formOf(b *box) *box {
	for x := b.parent; x != nil; x = x.parent {
		if x.n != nil && x.n.T == "form" {
			return x
		}
	}
	return nil
}

func (b *box) disabled() bool {
	if b.n == nil {
		return false
	}
	return truthy(b.n.P["disabled"])
}

// labelControl finds the control a label activates.
func (v *View) labelControl(l *box) *box {
	if id, ok := l.n.AttrString("for"); ok && id != "" {
		var find func(list []*box) *box
		find = func(list []*box) *box {
			for _, b := range list {
				if b.n != nil && b.n.ID == id {
					return b
				}
				if r := find(b.kids); r != nil {
					return r
				}
			}
			return nil
		}
		if r := find(v.top); r != nil {
			return r
		}
	}
	var first func(b *box) *box
	first = func(b *box) *box {
		for _, k := range b.kids {
			if k.repl == rInput || k.repl == rTextarea || k.repl == rCheck || k.repl == rRadio || k.repl == rSelect || k.repl == rRange {
				return k
			}
			if r := first(k); r != nil {
				return r
			}
		}
		return nil
	}
	return first(l)
}

func followLink(href string) {
	e := env()
	switch {
	case strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") || strings.HasPrefix(href, "mailto:"):
		if r := slackRoute(href); r != "" && e.Navigate != nil {
			e.Navigate(r)
			return
		}
		if e.OpenURL != nil {
			e.OpenURL(href)
		}
	default:
		if r := slackRoute(href); r != "" && e.Navigate != nil {
			e.Navigate(r)
		}
	}
}

// slackRoute maps Slack links (slack://channel?id=, /archives/C…/p…) to
// Clack routes.
func slackRoute(href string) string {
	if strings.HasPrefix(href, "slack://") {
		q := href[strings.IndexByte(href, '?')+1:]
		var id, msg string
		for _, kv := range strings.Split(q, "&") {
			k, val, _ := strings.Cut(kv, "=")
			switch k {
			case "id":
				id = val
			case "message":
				msg = val
			}
		}
		if id == "" {
			return ""
		}
		if msg != "" {
			return "c/" + id + "/m/" + msg
		}
		return "c/" + id
	}
	i := strings.Index(href, "/archives/")
	if i < 0 {
		return ""
	}
	if strings.HasPrefix(href, "http") && !strings.Contains(href[:i], "slack.com") {
		return ""
	}
	parts := strings.Split(strings.SplitN(href[i+len("/archives/"):], "?", 2)[0], "/")
	if len(parts) == 0 || parts[0] == "" {
		return ""
	}
	if len(parts) > 1 && len(parts[1]) > 7 && parts[1][0] == 'p' {
		ts := parts[1][1:]
		if len(ts) > 6 {
			ts = ts[:len(ts)-6] + "." + ts[len(ts)-6:]
		}
		return "c/" + parts[0] + "/m/" + ts
	}
	return "c/" + parts[0]
}

func (s *scope) scroll(pe *pointer.Event) {
	v := s.v
	t := s.hitTest(pe.Position)
	if t != nil && chainListens(t, lWheel) {
		v.dispatchPointer(s, t, "wheel", pe, 0)
	}
	sc := v.scroller()
	if sc == nil || sc.scope != s {
		return
	}
	ns := v.state(sc)
	mx, my := sc.scrollMax()
	ox, oy := ns.scrollX, ns.scrollY
	ns.scrollX = clampF(ns.scrollX+pe.Scroll.X, 0, mx)
	ns.scrollY = clampF(ns.scrollY+pe.Scroll.Y, 0, my)
	if ns.scrollX != ox || ns.scrollY != oy {
		s.rectsDirty = true
		v.dispatchSimple(sc, "scroll", nil)
		v.invalidate(s)
	}
}

// focusTarget is the focusable element at or above t.
func focusTarget(t *box) *box {
	for x := t; x != nil; x = x.parent {
		if x.focusable() {
			return x
		}
	}
	return nil
}

func (b *box) focusable() bool {
	if b.n == nil || b.kind != kElem || b.disabled() {
		return false
	}
	if ti, ok := b.n.AttrString("tabindex"); ok {
		n, err := strconv.Atoi(ti)
		return err == nil && n >= -1
	}
	switch b.n.T {
	case "button", "select", "textarea", "summary":
		return true
	case "input":
		return true
	case "a":
		_, ok := b.n.AttrString("href")
		return ok
	}
	if ce, ok := b.n.AttrString("contentEditable"); ok && ce != "false" {
		return true
	}
	return false
}

// focusTag is the key-focus tag of a focusable box.
func (v *View) focusTag(b *box) event.Tag {
	ns := v.state(b)
	if (b.repl == rInput || b.repl == rTextarea) && ns.ed != nil {
		return ns.ed
	}
	return &ns.tag
}

func (v *View) focusBox(s *scope, b *box) {
	if b.repl == rInput || b.repl == rTextarea {
		v.editor(b)
	}
	s.gtx.Execute(key.FocusCmd{Tag: v.focusTag(b)})
	// DOM order: blur the old element, then focus the new one, before the
	// mouseup/click that follow (Gio reports the focus change a frame later).
	if old := v.byK[v.focusK]; old != nil && old != b && old.ns != nil && old.ns.focused {
		v.setFocused(s, old, false)
	}
	if ns := v.state(b); !ns.focused {
		v.setFocused(s, b, true)
	}
}

// focusEvents reports focus transitions of a focusable box (called while
// painting it, every frame).
func (v *View) focusEvents(s *scope, b *box) {
	ns := v.state(b)
	f := s.gtx.Focused(v.focusTag(b))
	if f == ns.focused {
		return
	}
	v.setFocused(s, b, f)
}

func (v *View) setFocused(s *scope, b *box, f bool) {
	ns := v.state(b)
	ns.focused = f
	v.setState(b, css.StateFocus, f)
	for x := b.parent; x != nil; x = x.parent {
		if x.isElem() {
			v.setState(x, css.StateFocusWithin, f)
		}
	}
	if f {
		v.focusK = b.k()
		if ns.ed != nil {
			ns.focusVal = ns.ed.Text()
		}
		v.dispatchSimple(b, "focus", nil)
		v.dispatchSimple(b, "focusin", nil)
	} else {
		if v.focusK == b.k() {
			v.focusK = 0
		}
		v.dispatchSimple(b, "blur", nil)
		v.dispatchSimple(b, "focusout", nil)
		if (b.repl == rInput || b.repl == rTextarea) && ns.ed != nil && ns.ed.Text() != ns.focusVal {
			v.dispatchValue(b, "change")
		}
	}
	v.invalidate(s)
}

// keyEvents delivers key events of a focused non-editor focusable.
func (v *View) keyEvents(s *scope, b *box) {
	ns := v.state(b)
	if ns.filters == nil {
		ns.filters = []event.Filter{key.FocusFilter{Target: &ns.tag}, key.Filter{Focus: &ns.tag, Name: ""}}
	}
	for {
		e, ok := s.gtx.Event(ns.filters...)
		if !ok {
			break
		}
		ke, ok := e.(key.Event)
		if !ok {
			continue
		}
		typ := "keydown"
		if ke.State == key.Release {
			typ = "keyup"
		}
		v.dispatchKey(b, typ, ke)
		if ke.State == key.Press && (ke.Name == key.NameReturn || ke.Name == key.NameEnter || ke.Name == key.NameSpace) && b.n != nil {
			switch b.n.T {
			case "button", "a", "summary":
				v.click(s, b, &pointer.Event{})
			}
			if b.repl == rCheck || b.repl == rRadio {
				v.click(s, b, &pointer.Event{})
			}
		}
	}
}

// --- event JSON ---

func appendBool(b []byte, name string, v bool) []byte {
	b = append(b, ',', '"')
	b = append(b, name...)
	b = append(b, '"', ':')
	return strconv.AppendBool(b, v)
}

func appendNum(b []byte, name string, v float32) []byte {
	b = append(b, ',', '"')
	b = append(b, name...)
	b = append(b, '"', ':')
	return strconv.AppendFloat(b, float64(v), 'f', -1, 32)
}

func (v *View) beginEvent(typ string) []byte {
	b := make([]byte, 0, 256)
	b = append(b, `{"type":`...)
	b = appendString(b, typ)
	return b
}

// endEvent adds target state and the boxes of the target and its
// ancestors, then dispatches.
func (v *View) endEvent(t *box, b []byte) {
	k := t.k()
	b = append(b, `,"target":{`...)
	val, hasVal := v.targetValue(t)
	first := true
	if hasVal {
		b = append(b, `"value":`...)
		b = appendString(b, val)
		first = false
	}
	if t.repl == rCheck || t.repl == rRadio {
		if !first {
			b = append(b, ',')
		}
		b = append(b, `"checked":`...)
		b = strconv.AppendBool(b, v.checked(t))
	}
	b = append(b, `},"rects":{`...)
	n := 0
	for x := t; x != nil; x = x.parent {
		if !x.isElem() || x.k() == 0 || x.rectFr == 0 {
			continue
		}
		if n > 0 {
			b = append(b, ',')
		}
		n++
		ox, oy := float32(0), float32(0)
		if x.scope != nil {
			ox, oy = x.scope.windowOrigin()
		}
		r := x.rect
		b = append(b, '"')
		b = strconv.AppendInt(b, x.k(), 10)
		b = append(b, `":[`...)
		b = strconv.AppendFloat(b, float64((r.x0+ox)/v.dp), 'f', 1, 32)
		b = append(b, ',')
		b = strconv.AppendFloat(b, float64((r.y0+oy)/v.dp), 'f', 1, 32)
		b = append(b, ',')
		b = strconv.AppendFloat(b, float64((r.x1-r.x0)/v.dp), 'f', 1, 32)
		b = append(b, ',')
		b = strconv.AppendFloat(b, float64((r.y1-r.y0)/v.dp), 'f', 1, 32)
		b = append(b, ']')
	}
	b = append(b, '}', '}')
	if br := bridge(); br != nil {
		br.Dispatch(k, b)
	}
	if testDispatch != nil {
		testDispatch(k, b)
	}
}

// testDispatch observes dispatched events in tests.
var testDispatch func(target int64, ev []byte)

func dispatching() bool { return bridge() != nil || testDispatch != nil }

func appendMods(b []byte, m key.Modifiers) []byte {
	b = appendBool(b, "altKey", m&key.ModAlt != 0)
	b = appendBool(b, "ctrlKey", m&key.ModCtrl != 0)
	b = appendBool(b, "shiftKey", m&key.ModShift != 0)
	b = appendBool(b, "metaKey", m&(key.ModCommand|key.ModSuper) != 0)
	return b
}

func (v *View) dispatchPointer(s *scope, t *box, typ string, pe *pointer.Event, button int) {
	if t == nil || !dispatching() {
		return
	}
	t = t.elem()
	if t == nil || t.k() == 0 {
		return
	}
	b := v.beginEvent(typ)
	cx, cy := pe.Position.X, pe.Position.Y
	if s != nil {
		ox, oy := s.windowOrigin()
		cx, cy = cx+ox, cy+oy
	}
	if f := env().PointerWin; f != nil && pe.Kind != pointer.Kind(0) {
		w := f()
		cx, cy = float32(w.X), float32(w.Y)
	}
	b = appendNum(b, "clientX", cx/v.dp)
	b = appendNum(b, "clientY", cy/v.dp)
	b = append(b, `,"button":`...)
	b = strconv.AppendInt(b, int64(button), 10)
	b = append(b, `,"buttons":`...)
	b = strconv.AppendInt(b, int64(domButtons(pe.Buttons)), 10)
	b = appendMods(b, pe.Modifiers)
	if pe.Kind == pointer.Scroll {
		b = appendNum(b, "deltaX", pe.Scroll.X/v.dp)
		b = appendNum(b, "deltaY", pe.Scroll.Y/v.dp)
	}
	if typ == "click" || typ == "dblclick" {
		b = append(b, `,"detail":`...)
		b = strconv.AppendInt(b, int64(max(v.lastClk.n, 1)), 10)
	}
	v.endEvent(t, b)
}

func domButtons(b pointer.Buttons) int {
	n := 0
	if b&pointer.ButtonPrimary != 0 {
		n |= 1
	}
	if b&pointer.ButtonSecondary != 0 {
		n |= 2
	}
	if b&pointer.ButtonTertiary != 0 {
		n |= 4
	}
	return n
}

func (v *View) dispatchSimple(t *box, typ string, extra func([]byte) []byte) {
	if t == nil || !dispatching() {
		return
	}
	t = t.elem()
	if t == nil || t.k() == 0 {
		return
	}
	b := v.beginEvent(typ)
	if extra != nil {
		b = extra(b)
	}
	v.endEvent(t, b)
}

func (v *View) dispatchValue(t *box, typ string) { v.dispatchSimple(t, typ, nil) }

// domKey maps a Gio key name to DOM key and code values.
func domKey(n key.Name, m key.Modifiers) (string, string) {
	switch n {
	case key.NameReturn, key.NameEnter:
		return "Enter", "Enter"
	case key.NameEscape:
		return "Escape", "Escape"
	case key.NameLeftArrow:
		return "ArrowLeft", "ArrowLeft"
	case key.NameRightArrow:
		return "ArrowRight", "ArrowRight"
	case key.NameUpArrow:
		return "ArrowUp", "ArrowUp"
	case key.NameDownArrow:
		return "ArrowDown", "ArrowDown"
	case key.NameDeleteBackward:
		return "Backspace", "Backspace"
	case key.NameDeleteForward:
		return "Delete", "Delete"
	case key.NameHome:
		return "Home", "Home"
	case key.NameEnd:
		return "End", "End"
	case key.NamePageUp:
		return "PageUp", "PageUp"
	case key.NamePageDown:
		return "PageDown", "PageDown"
	case key.NameTab:
		return "Tab", "Tab"
	case key.NameSpace:
		return " ", "Space"
	case key.NameCtrl:
		return "Control", "ControlLeft"
	case key.NameShift:
		return "Shift", "ShiftLeft"
	case key.NameAlt:
		return "Alt", "AltLeft"
	case key.NameSuper, key.NameCommand:
		return "Meta", "MetaLeft"
	}
	s := string(n)
	if len(s) == 1 {
		c := s[0]
		switch {
		case c >= 'A' && c <= 'Z':
			if m&key.ModShift == 0 {
				return string(c + 32), "Key" + s
			}
			return s, "Key" + s
		case c >= '0' && c <= '9':
			return s, "Digit" + s
		}
	}
	return s, ""
}

func (v *View) dispatchKey(t *box, typ string, ke key.Event) {
	if !dispatching() {
		return
	}
	k, code := domKey(ke.Name, ke.Modifiers)
	v.dispatchSimple(t, typ, func(b []byte) []byte {
		b = append(b, `,"key":`...)
		b = appendString(b, k)
		b = append(b, `,"code":`...)
		b = appendString(b, code)
		b = appendMods(b, ke.Modifiers)
		b = appendBool(b, "repeat", false)
		return b
	})
}

// dispatchChar sends keydown/keyup for a typed character (editors only
// report text changes).
func (v *View) dispatchChar(t *box, ch string) {
	if !dispatching() {
		return
	}
	for _, typ := range [2]string{"keydown", "keyup"} {
		v.dispatchSimple(t, typ, func(b []byte) []byte {
			b = append(b, `,"key":`...)
			b = appendString(b, ch)
			code := ""
			if len(ch) == 1 {
				c := ch[0]
				switch {
				case c >= 'a' && c <= 'z':
					code = "Key" + string(c-32)
				case c >= 'A' && c <= 'Z':
					code = "Key" + ch
				case c >= '0' && c <= '9':
					code = "Digit" + ch
				case c == ' ':
					code = "Space"
				}
			}
			b = append(b, `,"code":`...)
			b = appendString(b, code)
			b = appendMods(b, 0)
			return b
		})
	}
}

// --- tooltips (title attribute) ---

type tipState struct {
	b     *box
	text  string
	since time.Time
}

func (v *View) tipUpdate(s *scope) {
	var tb *box
	var txt string
	for _, b := range v.hover {
		if b.n == nil {
			continue
		}
		if t, ok := b.n.AttrString("title"); ok && t != "" {
			tb, txt = b, t
			break
		}
	}
	if tb == v.tip.b && txt == v.tip.text {
		return
	}
	v.tip.b, v.tip.text = tb, txt
	v.tip.since = time.Now()
	if s != nil && s.gtx.Ops != nil && !s.gtx.Now.IsZero() {
		v.tip.since = s.gtx.Now
	}
}

// drawTip draws the hovered title tooltip after 500 ms, above the box.
func (v *View) drawTip(gtx layout.Context) {
	b := v.tip.b
	if b == nil || v.tip.text == "" || b.scope == nil || b.rectFr != v.frameNo {
		return
	}
	th := env().Theme
	if th == nil {
		return
	}
	now := gtx.Now
	if now.IsZero() {
		now = time.Now()
	}
	at := v.tip.since.Add(500 * time.Millisecond)
	if now.Before(at) {
		gtx.Execute(op.InvalidateCmd{At: at})
		return
	}
	// position in this view's coordinates
	ox, oy := b.scope.windowOrigin()
	mx, my := v.main.windowOrigin()
	r := b.rect.off(ox-mx, oy-my)
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(gtx.Dp(300), gtx.Dp(200))}
	tm := op.Record(gtx.Ops)
	d := th.Text(g, theme.LabelOpts{Font: theme.Font(theme.Bold), Size: unit.Sp(13), Color: th.C.TooltipText, Wrap: text.WrapWords, LineHeight: unit.Sp(18)}, v.tip.text)
	call := tm.Stop()
	padX, padY := float32(gtx.Dp(8)), float32(gtx.Dp(5))
	w, h := float32(d.Size.X)+2*padX, float32(d.Size.Y)+2*padY
	x := (r.x0+r.x1)/2 - w/2
	y := r.y0 - h - float32(gtx.Dp(6))
	if y+my < 0 {
		y = r.y1 + float32(gtx.Dp(6))
	}
	if x+mx < 2 {
		x = 2 - mx
	}
	p := painter{v: v, ops: gtx.Ops, clip: noClip}
	rad := float32(gtx.Dp(6))
	p.fillRR(rectF{x, y, x + w, y + h}, [4]float32{rad, rad, rad, rad}, th.C.Tooltip)
	t := op.Offset(image.Pt(int(x+padX), int(y+padY))).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	op.Defer(gtx.Ops, m.Stop())
}
