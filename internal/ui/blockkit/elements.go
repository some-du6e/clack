package blockkit

import (
	"encoding/json"
	"image"
	"strings"
	"time"

	"gioui.org/gesture"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/composer"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// owner is what element widgets live in: a message body or a surface
// (modal, App Home).
type owner interface {
	host() Host
	values() Values
	// fire sends the element's block action; the widget shows the
	// loading state until fire calls done (on the UI goroutine).
	fire(w *ew, action json.RawMessage, done func(err error))
	// base is the service ids and container (external suggestions).
	base() core.BlockAction
	channel() string
	modal() bool
	// changed: a value or a size changed (relayout and repaint).
	changed()
	// loading: an action of block bid is in flight. Slack's
	// useBlockKitInteractiveState disables the block's elements and shows
	// its spinner until the request settles.
	loading(bid string) bool
}

// tap is a click target that remembers where it is in the window.
type tap struct {
	g   gesture.Click
	org image.Point // window position of the target's top-left at the last click
	sz  image.Point
}

func (t *tap) clicked(gtx C, h Host) bool {
	hit := false
	for {
		e, ok := t.g.Update(gtx.Source)
		if !ok {
			break
		}
		if e.Kind == gesture.KindClick {
			hit = true
			t.org = pointerAt(h, e.Position).Sub(e.Position)
		}
	}
	return hit
}

func (t *tap) add(gtx C, sz image.Point) {
	t.sz = sz
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	t.g.Add(gtx.Ops)
	pointer.CursorPointer.Add(gtx.Ops)
}

func (t *tap) rect() image.Rectangle { return image.Rectangle{Min: t.org, Max: t.org.Add(t.sz)} }

// ew is one element widget.
type ew struct {
	o     owner
	bid   string
	b     *Block
	e     *Element
	input bool // inside an input block: changes update the state only

	main  tap
	rows  []tap // checkbox / radio options, multi-select chip removers
	more  tap
	ed    *widget.Editor
	edSet bool
	red   *composer.Editor // rich_text_input (richinput.go)
	files []*upFile        // file_input (richinput.go)

	busy  bool
	errAt time.Time
	views map[string]*textView // mrkdwn option labels
	open  bool                 // a dropdown is open for it

	// datetimepicker's second field
	time2 tap

	// slot is where the element was last drawn inside its message
	// elements slot (actions run or accessory); moreAt, its run's "+ N more".
	slot, moreAt image.Point

	// pressed: when a button was last clicked. Slack's ButtonElement
	// sets isProcessingClick for one second (the button is disabled, no
	// spinner) so double clicks don't send twice.
	pressed time.Time
	tip     popups.TipState // icon_button's tooltip

	// multi-selects outside modals are an outline button that opens
	// Slack's BaseMultiSelectElementDialog (btn); inDialog while it is
	// open (edits stay local until Go), dlgPrev to restore on Cancel.
	btn      tap
	inDialog bool
	dlgPrev  *Value

	// on_character_entered dispatch is debounced 500ms, leading and
	// trailing (useBlockKitInteractiveState's debounce config).
	charLast    time.Time
	charPending bool
}

func newEW(o owner, b *Block, e *Element, input bool) *ew {
	w := &ew{o: o, bid: b.BlockID, b: b, e: e, input: input}
	if isTextInput(e.Type) && e.Type != "rich_text_input" {
		// Enter dispatches in a dispatch_action block even when multiline
		// (PlainTextInputElement.handleKeyDown prevents the newline).
		w.ed = &widget.Editor{SingleLine: !e.Multiline, Submit: !e.Multiline || b.DispatchAction}
		if e.Type == "number_input" {
			w.ed.Filter = "0123456789-"
			if e.IsDecimalAllowed {
				w.ed.Filter += ".,"
			}
		}
		if e.MaxLength > 0 {
			w.ed.MaxLen = e.MaxLength
		}
	}
	return w
}

func (w *ew) value() *Value {
	vs := w.o.values()
	v := vs.get(w.bid, w.e.ActionID)
	if v == nil && stateful(w.e.Type) {
		v = initialValue(w.e)
		vs.set(w.bid, w.e.ActionID, v)
	}
	if v != nil && w.e.DefaultToCurrent && !v.defaulted {
		// default_to_current_conversation: the conversation the surface
		// belongs to, unless an initial value was given
		v.defaulted = true
		if ch := w.o.channel(); ch != "" {
			switch w.e.Type {
			case "conversations_select":
				if v.SelectedConv == "" {
					v.SelectedConv = ch
				}
			case "multi_conversations_select":
				if len(v.SelectedConvs) == 0 {
					v.SelectedConvs = []string{ch}
				}
			}
		}
	}
	return v
}

// disabled: Slack disables an element while its block's action is in
// flight, and a button for a second after each click.
func (w *ew) disabled(gtx C) bool {
	if w.o.loading(w.bid) {
		return true
	}
	if (w.e.Type == "button" || w.e.Type == "workflow_button") && !w.pressed.IsZero() {
		until := w.pressed.Add(time.Second)
		if gtx.Now.Before(until) {
			gtx.Execute(op.InvalidateCmd{At: until})
			return true
		}
	}
	return false
}

// dialogMulti: a multi-select drawn as Slack's "Pick your options"
// button (messages and App Home: shouldRenderMultiSelectsInline is only
// on in modals).
// The row keeps its button while the dialog is open; the dialog body
// draws the inline field itself (layoutMulti).
func (w *ew) dialogMulti() bool { return isMulti(w.e.Type) && !w.o.modal() }

func (w *ew) th() *theme.Theme { return w.o.host().Theme() }

// medium: modal-sized controls (36px) for inputs and datetimepickers.
func (w *ew) medium() bool {
	return w.input || w.e.Type == "datetimepicker" || (w.o.modal() && (isTextInput(w.e.Type) || isMulti(w.e.Type)))
}

// stacks reports whether a section accessory goes under the text.
func stacks(e *Element) bool {
	return e.Type == "checkboxes" || e.Type == "radio_buttons" || isMulti(e.Type)
}

// ---- sizes ----

// size is the element's size for an available width (px).
func (w *ew) size(gtx C, maxW int, accessory bool) image.Point {
	th := w.th()
	h28, h36 := dp(gtx, 28), dp(gtx, 36)
	full := func(h int) image.Point { return image.Pt(maxW, h) }
	fixedW := func(v float32, h int) image.Point {
		x := dp(gtx, v)
		if w.medium() {
			return full(h)
		}
		return image.Pt(min(x, maxW), h)
	}
	switch w.e.Type {
	case "button", "workflow_button":
		tw := measure(gtx, th, 13, theme.Bold, textOf(w.e.Text))
		bw := tw + 2*dp(gtx, 8)
		if accessory && w.o.modal() {
			bw = max(bw, dp(gtx, 120))
		}
		return image.Pt(min(bw, maxW), h28)
	case "overflow":
		return image.Pt(dp(gtx, 30), h28)
	case "icon_button":
		return image.Pt(h28, h28)
	case "static_select", "external_select", "users_select", "conversations_select", "channels_select":
		if w.medium() {
			return full(h36)
		}
		return fixedW(190, h28)
	case "multi_static_select", "multi_external_select", "multi_users_select", "multi_conversations_select", "multi_channels_select":
		if w.dialogMulti() {
			tw := measure(gtx, th, 13, theme.Bold, w.multiButtonText())
			return image.Pt(min(tw+2*dp(gtx, 12), maxW), h28)
		}
		return image.Pt(maxW, w.multiHeight(gtx, maxW)+w.maxAlertHeight(gtx))
	case "datepicker":
		if w.medium() {
			return full(h36)
		}
		return fixedW(220, h28)
	case "timepicker":
		if w.medium() {
			return full(h36)
		}
		return fixedW(190, h28)
	case "datetimepicker":
		fw := min(dp(gtx, 208), (maxW-dp(gtx, 8))/2)
		if w.input {
			fw = (maxW - dp(gtx, 8)) / 2
		}
		return image.Pt(2*fw+dp(gtx, 8), h36+dp(gtx, 4)+dp(gtx, 16))
	case "checkboxes", "radio_buttons":
		return image.Pt(maxW, w.optionsHeight(gtx, maxW))
	case "plain_text_input", "email_text_input", "url_text_input", "number_input":
		if w.e.Multiline {
			return full(max(dp(gtx, 104), w.edHeight(gtx, maxW)))
		}
		return full(h36)
	case "rich_text_input":
		return full(w.richHeight(gtx))
	case "file_input":
		return full(w.fileHeight(gtx))
	}
	return image.Point{}
}

func (w *ew) edHeight(gtx C, maxW int) int {
	if w.ed == nil {
		return 0
	}
	n := strings.Count(w.ed.Text(), "\n") + 1
	return n*dp(gtx, 22) + dp(gtx, 16)
}

// optionsHeight: rows of 22 (+22 with a description), 8 apart.
func (w *ew) optionsHeight(gtx C, maxW int) int {
	h := 0
	for i, o := range w.e.Options {
		if i > 0 {
			h += dp(gtx, 8)
		}
		h += w.optionTextHeight(gtx, &o, maxW-dp(gtx, 22))
	}
	return h
}

func (w *ew) optionTextHeight(gtx C, o *Option, tw int) int {
	h := w.textView(o.Text).height(gtx, tw)
	if o.Description != nil {
		h += w.textView(*o.Description).height(gtx, tw)
	}
	return max(h, dp(gtx, 22))
}

// multiHeight: one 36px field; chips wrap onto more rows.
func (w *ew) multiHeight(gtx C, maxW int) int {
	chips := w.chips()
	if len(chips) == 0 {
		if w.medium() {
			return dp(gtx, 36)
		}
		return dp(gtx, 28)
	}
	x, rows := dp(gtx, 6), 1
	for _, c := range chips {
		cw := w.chipWidth(gtx, c.label)
		if x+cw > maxW-dp(gtx, 6) && x > dp(gtx, 6) {
			rows++
			x = dp(gtx, 6)
		}
		x += cw + dp(gtx, 4)
	}
	return max(dp(gtx, 36), rows*dp(gtx, 26)+dp(gtx, 10))
}

// ---- layout ----

// layout draws the element in sz and handles its input.
func (w *ew) layout(gtx C, sz image.Point) D {
	switch w.e.Type {
	case "button", "workflow_button":
		w.layoutButton(gtx, sz)
	case "overflow":
		w.layoutOverflow(gtx, sz)
	case "icon_button":
		w.layoutIconButton(gtx, sz)
	case "static_select", "external_select", "users_select", "conversations_select", "channels_select",
		"datepicker", "timepicker":
		w.layoutSelect(gtx, sz)
	case "multi_static_select", "multi_external_select", "multi_users_select", "multi_conversations_select", "multi_channels_select":
		if w.dialogMulti() {
			w.layoutMultiButton(gtx, sz)
		} else {
			w.layoutMulti(gtx, sz)
		}
	case "datetimepicker":
		w.layoutDateTime(gtx, sz)
	case "checkboxes", "radio_buttons":
		w.layoutOptions(gtx, sz)
	case "plain_text_input", "email_text_input", "url_text_input", "number_input":
		w.layoutText(gtx, sz)
	case "rich_text_input":
		w.layoutRich(gtx, sz)
	case "file_input":
		w.layoutFile(gtx, sz)
	}
	return D{Size: sz}
}

// ---- buttons ----

func (w *ew) layoutButton(gtx C, sz image.Point) {
	h := w.o.host()
	off := w.disabled(gtx)
	if w.main.clicked(gtx, h) && !off {
		w.pressed = gtx.Now
		w.press()
		off = w.disabled(gtx)
	}
	drawButton(gtx, w.th(), sz, w.e.Style, textOf(w.e.Text), 4, 13, w.main.g.Hovered(), false, off)
	w.main.add(gtx, sz)
}

// layoutIconButton is Slack's icon_button: a borderless 28px
// c-icon_button (size smedium) with the text as its tooltip; it sends
// {type, action_id, block_id, icon, text, value}.
func (w *ew) layoutIconButton(gtx C, sz image.Point) {
	h := w.o.host()
	th := w.th()
	cl := colorsOf(th)
	off := w.disabled(gtx)
	if w.main.clicked(gtx, h) && !off {
		act := func() { w.send(actionPayload(w.bid, w.e, nil)) }
		if w.e.Confirm != nil {
			showConfirm(h, w.e.Confirm, act, nil)
		} else {
			act()
		}
	}
	hov := w.main.g.Hovered()
	if hov && !off {
		rrect(gtx, image.Rectangle{Max: sz}, dp(gtx, 4), cl.Hover)
	}
	name := blockKitIconNames[w.e.Icon] // agenticons.go
	fg := cl.Text2
	if hov {
		fg = cl.Text
	}
	if off {
		fg = cl.DisText
	}
	is := dp(gtx, 18)
	at(gtx, image.Pt((sz.X-is)/2, (sz.Y-is)/2), func(gtx C) D { return agentIcon(gtx, name, 18, fg) })
	w.main.add(gtx, sz)
	popups.Tooltip(gtx, th, &w.tip, hov, sz, popups.Above, textOf(w.e.Text), "")
}

// press runs a button: url buttons open the link (and still notify the
// app, like Slack), workflow buttons start their workflow, and confirm
// asks first.
func (w *ew) press() {
	if w.e.Type == "workflow_button" {
		// WorkflowButtonElement only trips the trigger: it never sends
		// blocks.actions.
		if link, inputs := workflowTrigger(w.e); link != "" {
			act := func() { tripWorkflow(w.o.host(), link, w.o.channel(), w.messageTS(), inputs) }
			if w.e.Confirm != nil {
				showConfirm(w.o.host(), w.e.Confirm, act, nil)
				return
			}
			act()
		}
		return
	}
	act := func() {
		// handleClickOrConfirmDialogGo: the action, then the url
		w.send(actionPayload(w.bid, w.e, nil))
		if w.e.URL != "" {
			openURL(w.e.URL)
		}
	}
	if w.e.Confirm != nil {
		showConfirm(w.o.host(), w.e.Confirm, act, nil)
		return
	}
	act()
}

// messageTS is the ts of the message the element is in ("" in views).
func (w *ew) messageTS() string {
	if m, ok := w.o.(*Message); ok {
		return m.m.TS
	}
	return ""
}

// send fires the action with the loading state.
func (w *ew) send(payload json.RawMessage) {
	w.busy = true
	w.o.changed()
	w.o.fire(w, payload, func(err error) {
		w.busy = false
		w.o.changed()
	})
}

// commit records a new value: in an input block that's all (unless the
// block dispatches actions); elsewhere the app gets the action.
func (w *ew) commit(prev *Value) {
	w.o.changed()
	if w.inDialog || (w.input && !w.b.DispatchAction) {
		return
	}
	payload := actionPayload(w.bid, w.e, w.value())
	if w.e.Confirm != nil {
		cur := *w.value()
		showConfirm(w.o.host(), w.e.Confirm, func() { w.send(payload) }, func() {
			// denied: back to the previous value
			if prev != nil {
				w.o.values().set(w.bid, w.e.ActionID, prev)
			}
			_ = cur
			w.o.changed()
		})
		return
	}
	w.send(payload)
}

func (w *ew) snapshot() *Value {
	if v := w.value(); v != nil {
		c := *v
		c.SelectedOptions = append([]Option(nil), v.SelectedOptions...)
		c.SelectedUsers = append([]string(nil), v.SelectedUsers...)
		c.SelectedConvs = append([]string(nil), v.SelectedConvs...)
		c.SelectedChans = append([]string(nil), v.SelectedChans...)
		return &c
	}
	return nil
}

// ---- overflow ----

func (w *ew) layoutOverflow(gtx C, sz image.Point) {
	h := w.o.host()
	off := w.disabled(gtx)
	if w.main.clicked(gtx, h) && !off {
		openOverflow(w)
	}
	th := w.th()
	cl := colorsOf(th)
	hov := (w.main.g.Hovered() || w.open) && !off
	bg, fg, border := buttonColors(cl, "", hov)
	if off {
		bg, fg = cl.Disabled, cl.DisText
	}
	outline(gtx, image.Rectangle{Max: sz}, dp(gtx, 4), max(1, dp(gtx, 1)), border, bg)
	_ = fg
	is := dp(gtx, 16)
	ic := cl.InputText
	if off {
		ic = cl.DisText
	}
	at(gtx, image.Pt((sz.X-is)/2, (sz.Y-is)/2), func(gtx C) D { return icon(gtx, "more", 16, ic) })
	w.main.add(gtx, sz)
}

// ---- single selects and pickers ----

// fieldText is what a select-like field shows, and whether it's the
// placeholder.
func (w *ew) fieldText() (string, bool) {
	v := w.value()
	ph := textOf(w.e.Placeholder)
	switch w.e.Type {
	case "static_select", "external_select":
		if v != nil && v.SelectedOption != nil {
			return textOf(&v.SelectedOption.Text), false
		}
		if ph == "" {
			ph = "Pick an option"
		}
	case "users_select":
		if v != nil && v.SelectedUser != "" {
			return w.userName(v.SelectedUser), false
		}
		if ph == "" {
			ph = "Select a user"
		}
	case "conversations_select":
		if v != nil && v.SelectedConv != "" {
			return w.convName(v.SelectedConv), false
		}
		if ph == "" {
			ph = "Select a conversation"
		}
	case "channels_select":
		if v != nil && v.SelectedChan != "" {
			return w.convName(v.SelectedChan), false
		}
		if ph == "" {
			ph = "Select a channel"
		}
	case "datepicker":
		if v != nil && v.SelectedDate != "" {
			if !w.input {
				// DatepickerElement outside input blocks: moment "L",
				// and "Today" for today
				return shortDateLabel(v.SelectedDate, time.Now()), false
			}
			return dateLabel(v.SelectedDate), false
		}
		if ph == "" {
			ph = "Select a date"
		}
	case "timepicker":
		if v != nil && v.SelectedTime != "" {
			return timeLabel(v.SelectedTime), false
		}
		if ph == "" {
			ph = "Select time"
		}
	}
	return ph, true
}

func (w *ew) userName(id string) string {
	if u, ok := w.o.host().Core().User(id); ok {
		if u.Name != "" {
			return u.Name
		}
		return u.RealName
	}
	return id
}

func (w *ew) convName(id string) string {
	c := w.o.host().Core()
	if cv, ok := c.Conv(id); ok {
		switch cv.Kind {
		case "im":
			return w.userName(cv.User)
		case "mpim":
			return cv.Name
		}
		return "#" + cv.Name
	}
	ref := c.ConvRef(id)
	if ref.Name != "" {
		return "#" + strings.TrimPrefix(ref.Name, "#")
	}
	return id
}

func (w *ew) lead() string {
	switch w.e.Type {
	case "datepicker":
		return "calendar"
	case "timepicker":
		return "clock"
	}
	return ""
}

func (w *ew) layoutSelect(gtx C, sz image.Point) {
	h := w.o.host()
	if w.main.clicked(gtx, h) && !w.disabled(gtx) {
		w.openPicker(w.main.rect())
	}
	txt, ph := w.fieldText()
	fs := float32(13)
	if sz.Y > dp(gtx, 32) {
		fs = 15
	}
	drawField(gtx, w.th(), sz, w.lead(), txt, ph, w.open, true, fs, w.errored())
	w.main.add(gtx, sz)
}

func (w *ew) errored() bool {
	if s, ok := w.o.(*Surface); ok {
		return s.errs[w.bid] != ""
	}
	return false
}

// openPicker opens the dropdown / calendar / time list for a field.
func (w *ew) openPicker(anchor image.Rectangle) {
	switch w.e.Type {
	case "datepicker":
		openDate(w, anchor, func(day time.Time) {
			prev := w.snapshot()
			w.value().SelectedDate = formatDate(day)
			w.commit(prev)
		})
	case "timepicker":
		openTime(w, anchor, w.value().SelectedTime, func(hm string) {
			prev := w.snapshot()
			w.value().SelectedTime = hm
			w.commit(prev)
		})
	default:
		openSelect(w, anchor)
	}
}

// ---- datetimepicker ----

func (w *ew) layoutDateTime(gtx C, sz image.Point) {
	h := w.o.host()
	th := w.th()
	cl := colorsOf(th)
	gap := dp(gtx, 8)
	fw := (sz.X - gap) / 2
	fh := dp(gtx, 36)
	v := w.value()
	var day time.Time
	var mins int
	hasDay, hasTime := false, false
	if v.SelectedDateTime != 0 {
		t := time.Unix(v.SelectedDateTime, 0)
		day = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
		mins = t.Hour()*60 + t.Minute()
		hasDay, hasTime = true, true
	}
	set := func(d time.Time, m int) {
		prev := w.snapshot()
		w.value().SelectedDateTime = d.Add(time.Duration(m) * time.Minute).Unix()
		w.commit(prev)
	}
	off := w.disabled(gtx)
	if w.main.clicked(gtx, h) && !off {
		r := w.main.rect()
		openDateAt(w, r, day, func(d time.Time) { set(d, mins) })
	}
	if w.time2.clicked(gtx, h) && !off {
		cur := ""
		if hasTime {
			cur = formatHM(mins)
		}
		openTime(w, w.time2.rect(), cur, func(hm string) {
			m, _ := parseHM(hm)
			d := day
			if !hasDay {
				n := time.Now()
				d = time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.Local)
			}
			set(d, m)
		})
	}
	dtxt, dph := "Select a date", !hasDay
	if hasDay {
		dtxt = day.Format("Jan 2, 2006")
	}
	ttxt, tph := "Time", !hasTime
	if hasTime {
		ttxt = timeLabel(formatHM(mins))
	}
	at(gtx, image.Point{}, func(gtx C) D {
		drawField(gtx, th, image.Pt(fw, fh), "calendar", dtxt, dph, false, true, 15, w.errored())
		w.main.add(gtx, image.Pt(fw, fh))
		return D{}
	})
	at(gtx, image.Pt(fw+gap, 0), func(gtx C) D {
		drawField(gtx, th, image.Pt(fw, fh), "clock", ttxt, tph, false, true, 15, w.errored())
		w.time2.add(gtx, image.Pt(fw, fh))
		return D{}
	})
	at(gtx, image.Pt(fw+gap, fh+dp(gtx, 4)), func(gtx C) D {
		gtx.Constraints = layout.Constraints{Max: image.Pt(fw, dp(gtx, 16))}
		return label(gtx, th, 13, theme.Regular, cl.Text2, "Time zone: "+tzLabel(h))
	})
}

// tzLabel is the viewer's time zone as Slack names it.
func tzLabel(h Host) string {
	if c := h.Core(); c != nil {
		if u := c.Self(); u.TZLabel != "" {
			return u.TZLabel
		}
	}
	name, _ := time.Now().Zone()
	return name
}

// ---- checkboxes / radio buttons ----

func (w *ew) layoutOptions(gtx C, sz image.Point) {
	h := w.o.host()
	th := w.th()
	cl := colorsOf(th)
	if len(w.rows) != len(w.e.Options) {
		w.rows = make([]tap, len(w.e.Options))
	}
	v := w.value()
	radio := w.e.Type == "radio_buttons"
	tw := sz.X - dp(gtx, 22)
	if radio {
		tw = sz.X - dp(gtx, 24)
	}
	y := 0
	for i := range w.e.Options {
		o := &w.e.Options[i]
		rh := w.optionTextHeight(gtx, o, tw)
		on := false
		if radio {
			on = v.SelectedOption != nil && v.SelectedOption.Value == o.Value
		} else {
			for _, s := range v.SelectedOptions {
				if s.Value == o.Value {
					on = true
				}
			}
		}
		row := &w.rows[i]
		if row.clicked(gtx, h) && !w.disabled(gtx) {
			prev := w.snapshot()
			if radio {
				oc := *o
				v.SelectedOption = &oc
			} else if on {
				out := v.SelectedOptions[:0:0]
				for _, s := range v.SelectedOptions {
					if s.Value != o.Value {
						out = append(out, s)
					}
				}
				v.SelectedOptions = out
			} else {
				v.SelectedOptions = append(append([]Option(nil), v.SelectedOptions...), *o)
			}
			w.commit(prev)
		}
		at(gtx, image.Pt(0, y), func(gtx C) D {
			hov := row.g.Hovered()
			at(gtx, image.Pt(0, dp(gtx, 4)), func(gtx C) D {
				if radio {
					drawRadio(gtx, th, on, hov)
				} else {
					drawCheckbox(gtx, th, on, hov)
				}
				return D{}
			})
			x := dp(gtx, 22)
			if radio {
				x = dp(gtx, 24)
			}
			at(gtx, image.Pt(x, 0), func(gtx C) D {
				th1 := w.textView(o.Text).layout(gtx, tw, cl.Text)
				if o.Description != nil {
					at(gtx, image.Pt(0, th1), func(gtx C) D {
						dv := w.textView(*o.Description)
						dv.setMuted(true)
						dv.layout(gtx, tw, cl.Text2)
						return D{}
					})
				}
				return D{}
			})
			row.add(gtx, image.Pt(sz.X, rh))
			return D{}
		})
		y += rh + dp(gtx, 8)
	}
}

// ---- text inputs ----

func (w *ew) layoutText(gtx C, sz image.Point) {
	th := w.th()
	cl := colorsOf(th)
	v := w.value()
	if !w.edSet {
		w.edSet = true
		w.ed.SetText(v.Text)
		if w.e.Type == "rich_text_input" && len(v.Rich) > 0 {
			w.ed.SetText(richPlain(v.Rich))
		}
		if w.e.FocusOnLoad {
			gtx.Execute(key.FocusCmd{Tag: w.ed})
		}
	}
	for {
		e, ok := w.ed.Update(gtx)
		if !ok {
			break
		}
		switch e.(type) {
		case widget.ChangeEvent:
			before := textValue(w.e.Type, v.Text)
			v.Text = w.ed.Text()
			if w.e.Type == "rich_text_input" {
				v.Rich = nil
			}
			if s, ok := w.o.(*Surface); ok {
				s.clearErr(w.bid)
			}
			// only a change of the trimmed value dispatches
			if w.dispatchOn("on_character_entered") && textValue(w.e.Type, v.Text) != before {
				w.charInput(gtx)
			}
			if w.e.Multiline || w.e.Type == "rich_text_input" {
				w.o.changed()
			}
		case widget.SubmitEvent:
			if w.dispatchOn("on_enter_pressed") {
				w.charPending = false
				w.send(actionPayload(w.bid, w.e, v))
			}
		}
	}
	if w.charPending {
		if at := w.charLast.Add(charDebounce); !gtx.Now.Before(at) {
			w.charPending = false
			w.charLast = gtx.Now
			w.send(actionPayload(w.bid, w.e, v))
		} else {
			gtx.Execute(op.InvalidateCmd{At: at})
		}
	}
	focused := gtx.Focused(w.ed)
	r := image.Rectangle{Max: sz}
	rad := dp(gtx, 8)
	bw := max(1, dp(gtx, 1))
	switch {
	case focused:
		ring := dp(gtx, 4)
		rrect(gtx, r.Inset(-ring), rad+ring, cl.FocusRing)
		outline(gtx, r, rad, bw, cl.Focus, cl.Bg)
	case w.errored():
		outline(gtx, r, rad, bw, cl.ErrorText, cl.Bg)
	default:
		outline(gtx, r, rad, bw, cl.SelectBorder, cl.Bg)
	}
	// clicks anywhere in the box focus the editor (the editor, added
	// after, takes the clicks on its own text)
	if w.main.clicked(gtx, w.o.host()) {
		gtx.Execute(key.FocusCmd{Tag: w.ed})
	}
	func() {
		defer clip.Rect(r).Push(gtx.Ops).Pop()
		w.main.g.Add(gtx.Ops)
		pointer.CursorText.Add(gtx.Ops)
	}()
	padX, padY := dp(gtx, 12), dp(gtx, 7)
	if w.e.Multiline || w.e.Type == "rich_text_input" {
		padY = dp(gtx, 8)
	}
	inner := image.Pt(sz.X-2*padX, sz.Y-2*padY)
	at(gtx, image.Pt(padX, padY), func(gtx C) D {
		gtx.Constraints = layout.Constraints{Min: image.Pt(inner.X, 0), Max: inner}
		if w.ed.Len() == 0 {
			if ph := w.textPlaceholder(); ph != "" {
				label(gtx, th, 15, theme.Regular, cl.Placeholder, ph)
			}
		}
		return th.Editor(gtx, w.ed, 15, cl.InputText, cl.FocusRing)
	})
}

// charDebounce is the on_character_entered debounce (500ms, leading and
// trailing).
const charDebounce = 500 * time.Millisecond

// charInput dispatches a typed character: at once when nothing went out
// in the last 500ms (leading), else once the typing pauses (trailing).
func (w *ew) charInput(gtx C) {
	if gtx.Now.Sub(w.charLast) >= charDebounce && !w.charPending {
		w.charLast = gtx.Now
		w.send(actionPayload(w.bid, w.e, w.value()))
		return
	}
	w.charPending = true
	w.charLast = gtx.Now
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(charDebounce)})
}

// dispatchOn reports whether a dispatch_action input sends on trigger,
// as Slack's text inputs decide: plain_text_input sends on Enter
// whenever its block dispatches (handleKeyDown ignores the config) and
// on change when the config lists on_character_entered; the number,
// email and url inputs follow dispatch_action_config, which defaults to
// on_enter_pressed.
func (w *ew) dispatchOn(trigger string) bool {
	if !w.input {
		// not valid Block Kit (text inputs live in input blocks); Enter
		// sends, like a dispatching block
		return trigger == "on_enter_pressed"
	}
	if !w.b.DispatchAction {
		return false
	}
	on := []string{"on_enter_pressed"}
	if w.e.Dispatch != nil && len(w.e.Dispatch.TriggerActionsOn) > 0 {
		on = w.e.Dispatch.TriggerActionsOn
	}
	if trigger == "on_enter_pressed" && w.e.Type == "plain_text_input" {
		return true
	}
	for _, t := range on {
		if t == trigger {
			return true
		}
	}
	return false
}

// enterHint: the input block shows "⏎ Press 'enter' to submit" (only
// for a dispatching plain_text_input whose dispatch_action_config lists
// on_enter_pressed and not on_character_entered).
func enterHint(b *Block) bool {
	e := b.Element
	if e == nil || !b.DispatchAction || e.Type != "plain_text_input" || e.Dispatch == nil {
		return false
	}
	enter, char := false, false
	for _, t := range e.Dispatch.TriggerActionsOn {
		enter = enter || t == "on_enter_pressed"
		char = char || t == "on_character_entered"
	}
	return enter && !char
}

// textPlaceholder is a text input's placeholder, with Slack's defaults
// for the typed inputs (plain_text_input has none).
func (w *ew) textPlaceholder() string {
	if ph := textOf(w.e.Placeholder); ph != "" {
		return ph
	}
	switch w.e.Type {
	case "email_text_input":
		return "Enter an email address"
	case "url_text_input":
		return "Enter a URL"
	case "number_input":
		return "Enter a number"
	}
	return ""
}

// richPlain flattens a rich_text value to text for editing.
func richPlain(raw json.RawMessage) string {
	var rt struct {
		Elements []struct {
			Elements []struct {
				Type string `json:"type"`
				Text string `json:"text"`
				URL  string `json:"url"`
			} `json:"elements"`
		} `json:"elements"`
	}
	if json.Unmarshal(raw, &rt) != nil {
		return ""
	}
	var b strings.Builder
	for i, s := range rt.Elements {
		if i > 0 {
			b.WriteByte('\n')
		}
		for _, e := range s.Elements {
			if e.Text != "" {
				b.WriteString(e.Text)
			} else {
				b.WriteString(e.URL)
			}
		}
	}
	return b.String()
}

// ---- multi-selects ----

type chip struct{ id, label string }

func (w *ew) chips() []chip {
	v := w.value()
	if v == nil {
		return nil
	}
	var out []chip
	switch w.e.Type {
	case "multi_static_select", "multi_external_select":
		for _, o := range v.SelectedOptions {
			out = append(out, chip{o.Value, textOf(&o.Text)})
		}
	case "multi_users_select":
		for _, id := range v.SelectedUsers {
			out = append(out, chip{id, w.userName(id)})
		}
	case "multi_conversations_select":
		for _, id := range v.SelectedConvs {
			out = append(out, chip{id, w.convName(id)})
		}
	case "multi_channels_select":
		for _, id := range v.SelectedChans {
			out = append(out, chip{id, w.convName(id)})
		}
	}
	return out
}

func (w *ew) chipWidth(gtx C, s string) int {
	return measure(gtx, w.th(), 15, theme.Regular, s) + dp(gtx, 8+22)
}

// remove drops a chip.
func (w *ew) remove(id string) {
	prev := w.snapshot()
	v := w.value()
	drop := func(s []string) []string {
		out := s[:0:0]
		for _, x := range s {
			if x != id {
				out = append(out, x)
			}
		}
		return out
	}
	switch w.e.Type {
	case "multi_static_select", "multi_external_select":
		out := v.SelectedOptions[:0:0]
		for _, o := range v.SelectedOptions {
			if o.Value != id {
				out = append(out, o)
			}
		}
		v.SelectedOptions = out
	case "multi_users_select":
		v.SelectedUsers = drop(v.SelectedUsers)
	case "multi_conversations_select":
		v.SelectedConvs = drop(v.SelectedConvs)
	case "multi_channels_select":
		v.SelectedChans = drop(v.SelectedChans)
	}
	w.commit(prev)
}

// multiPlaceholder is a multi-select's placeholder, or Slack's fallback.
func (w *ew) multiPlaceholder() string {
	if ph := textOf(w.e.Placeholder); ph != "" {
		return ph
	}
	switch w.e.Type {
	case "multi_users_select":
		return "Select users"
	case "multi_channels_select":
		return "Select channels"
	}
	return "Pick your options"
}

// multiButtonText is the dialog button's label: "{N} selected", or the
// placeholder (Slack's MultiSelectInput, maxCharacters 30).
func (w *ew) multiButtonText() string {
	if n := len(w.chips()); n > 0 {
		return itoa(n) + " selected"
	}
	s := []rune(w.multiPlaceholder())
	if len(s) > 30 {
		s = append(s[:30], '…')
	}
	return string(s)
}

// overMax reports whether more than max_selected_items are selected
// (Slack lets you pick them, then flags it and won't send).
func (w *ew) overMax() bool {
	return w.e.MaxSelectedItems > 0 && len(w.chips()) > w.e.MaxSelectedItems
}

// maxAlert is MaxSelectedItemsAlert's text.
func (w *ew) maxAlert() string {
	n := itoa(w.e.MaxSelectedItems)
	switch w.e.Type {
	case "multi_conversations_select":
		return "You can select up to " + n + " conversations."
	case "multi_users_select":
		return "You can select up to " + n + " people."
	case "multi_channels_select":
		return "You can select up to " + n + " channels."
	}
	return "You can select up to " + n + " items."
}

func (w *ew) maxAlertHeight(gtx C) int {
	if w.overMax() {
		return dp(gtx, 4) + dp(gtx, 20)
	}
	return 0
}

// layoutMultiButton is the outline button that opens the dialog.
func (w *ew) layoutMultiButton(gtx C, sz image.Point) {
	h := w.o.host()
	off := w.disabled(gtx)
	if w.btn.clicked(gtx, h) && !off {
		w.openMultiDialog()
	}
	drawButton(gtx, w.th(), sz, "", w.multiButtonText(), 4, 13, w.btn.g.Hovered(), false, off)
	w.btn.add(gtx, sz)
}

// openMultiDialog is Slack's BaseMultiSelectElementDialog: a dialog
// titled with the placeholder, the inline multi-select, Cancel and Go
// ("Confirm"; disabled while over max_selected_items). Go sends the
// action (after the element's confirm, as a speed bump); Cancel puts
// the old selection back.
func (w *ew) openMultiDialog() {
	h := w.o.host()
	w.dlgPrev = w.snapshot()
	w.inDialog = true
	w.o.changed()
	gone := false
	finish := func(keep bool) {
		if gone {
			return
		}
		gone = true
		w.inDialog = false
		if !keep && w.dlgPrev != nil {
			w.o.values().set(w.bid, w.e.ActionID, w.dlgPrev)
		}
		w.o.changed()
	}
	submit := func() {
		finish(true)
		w.send(actionPayload(w.bid, w.e, w.value()))
	}
	btns := []*popups.ModalButton{
		{Label: "Cancel", Kind: popups.BtnOutline, Do: func() bool { finish(false); return true }},
		{Label: "Confirm", Kind: popups.BtnPrimary, Enabled: func() bool { return !w.overMax() }, Do: func() bool {
			if c := w.e.Confirm; c != nil {
				showConfirm(h, c, submit, nil)
				return true
			}
			submit()
			return true
		}},
	}
	m := popups.NewModal(h, popups.ModalOpts{
		Title: w.multiPlaceholder(),
		Width: 520,
		Body: func(gtx C) D {
			bw := gtx.Constraints.Max.X
			fh := w.multiHeight(gtx, bw)
			w.layoutMulti(fixed(gtx, image.Pt(bw, fh)), image.Pt(bw, fh))
			hh := fh
			if w.overMax() {
				at(gtx, image.Pt(0, fh+dp(gtx, 4)), func(gtx C) D {
					return errorLine(gtx, h.Theme(), colorsOf(h.Theme()), bw, w.maxAlert())
				})
				hh += w.maxAlertHeight(gtx)
			}
			return D{Size: image.Pt(bw, hh)}
		},
		Buttons: btns,
		OnClose: func() {
			if !gone && w.inDialog {
				finish(false)
			}
		},
	})
	if modalShown != nil {
		modalShown(m, btns)
	}
	h.ShowOverlay(m)
}

func (w *ew) layoutMulti(gtx C, sz image.Point) {
	h := w.o.host()
	th := w.th()
	cl := colorsOf(th)
	if a := w.maxAlertHeight(gtx); a > 0 && !w.inDialog {
		// the alert under an inline (modal) multi-select
		sz.Y -= a
		at(gtx, image.Pt(0, sz.Y+dp(gtx, 4)), func(gtx C) D { return errorLine(gtx, th, cl, sz.X, w.maxAlert()) })
	}
	chips := w.chips()
	if len(w.rows) < len(chips) {
		w.rows = append(w.rows, make([]tap, len(chips)-len(w.rows))...)
	}
	for i := range chips {
		if w.rows[i].clicked(gtx, h) {
			w.remove(chips[i].id)
			return
		}
	}
	if w.main.clicked(gtx, h) && !w.disabled(gtx) {
		openSelect(w, w.main.rect())
	}
	if len(chips) == 0 {
		ph := w.multiPlaceholder()
		fs := float32(15)
		if sz.Y < dp(gtx, 32) {
			fs = 13
		}
		drawField(gtx, th, sz, "", ph, true, w.open, false, fs, w.errored())
		w.main.add(gtx, sz)
		return
	}
	r := image.Rectangle{Max: sz}
	bw := max(1, dp(gtx, 1))
	if w.open {
		ring := dp(gtx, 4)
		rrect(gtx, r.Inset(-ring), dp(gtx, 8)+ring, cl.FocusRing)
		outline(gtx, r, dp(gtx, 8), bw, cl.Focus, cl.Bg)
	} else {
		outline(gtx, r, dp(gtx, 8), bw, cl.SelectBorder, cl.Bg)
	}
	w.main.add(gtx, sz)
	x, y := dp(gtx, 6), dp(gtx, 5)
	ch := dp(gtx, 26)
	for i, c := range chips {
		cw := w.chipWidth(gtx, c.label)
		if x+cw > sz.X-dp(gtx, 6) && x > dp(gtx, 6) {
			x = dp(gtx, 6)
			y += ch
		}
		row := &w.rows[i]
		at(gtx, image.Pt(x, y), func(gtx C) D {
			cr := image.Rect(0, 0, cw, dp(gtx, 22))
			rrect(gtx, cr, dp(gtx, 4), cl.BgAlt)
			at(gtx, image.Pt(dp(gtx, 8), 0), func(gtx C) D {
				return vcenter(gtx, cr.Dy(), func(gtx C) D { return label(gtx, th, 15, theme.Regular, cl.InputText, c.label) })
			})
			at(gtx, image.Pt(cw-dp(gtx, 20), dp(gtx, 3)), func(gtx C) D {
				icon(gtx, "close-small", 16, cl.Text2)
				row.add(gtx, image.Pt(dp(gtx, 16), dp(gtx, 16)))
				return D{}
			})
			return D{}
		})
		x += cw + dp(gtx, 4)
	}
}
