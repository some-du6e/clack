package blockkit

// split_button: Slack's SplitButtonElementBase (boot-shared wAPv; CDN
// build 9f450b8ad3db), which jHw9 registers with toCamelCaseProps.
//
//   - With the bk_refresh_split_button experiment off (the accounts Clack
//     follows: their experiments.getByUser doesn't list it) it is the
//     primary action's Button alone. On, a ButtonGroup of that Button and
//     a caret-down-filled IconButton, whose Menu (bottom-right) lists the
//     secondary actions, and whose tooltip (top) is "More options for
//     {primary_text}".
//   - A click sends {type: split_button, action_id, block_id, text, value}
//     for the action (asking its confirm first), then opens its url. The
//     primary's clicks within a second of the one that sent send nothing.
//   - Its type is getButtonType's (szY5): primary, danger, ghost, else
//     outline; its size getButtonSize's (Nds2): medium, else small.
//     disabled is aria-disabled (drawn disabled, no click); is_loading
//     draws the group's box as one LoadingButton.
//   - client-boot-styles: c-button 28px tall (medium 36px), min-width 56px
//     (80px), padding 0 12px 1px, 13px (15px) bold, radius 4px (8px);
//     c-button--grouped rounds the group's outer corners only, keeps the
//     first's right border between the two (a grouped primary's 1px
//     jade-60, dark jade-30; danger's flamingo-40, dark flamingo-50), and
//     c-icon_button--grouped_small has radius 4px.
//
// Left out: the primary label's tooltip when it's cut (TruncatedText
// withTooltip) and the menu labels' emoji (they're the text).

import (
	"encoding/json"
	"image"
	"image/color"
	"time"

	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// splitDebounce is how long the primary's clicks after one that sent
// send nothing (SplitButtonElementBase's isClickDisabled timeout).
const splitDebounce = time.Second

// splitOn reports whether the split_button draws its chevron and menu.
func (w *ew) splitOn() bool {
	c := w.o.host().Core()
	return c != nil && c.Experiment("bk_refresh_split_button") == "on"
}

// splitPrimary is the primary action (an empty one when it has none).
func (w *ew) splitPrimary() *SplitAction {
	if w.e.PrimaryAction != nil {
		return w.e.PrimaryAction
	}
	return &SplitAction{}
}

// splitStyle is getButtonType's type as a Block Kit style ("" outline).
func (w *ew) splitStyle() string {
	switch w.e.Style {
	case "primary", "danger", "ghost":
		return w.e.Style
	}
	return ""
}

// splitMetrics are the Button's height, font size and radius (dp) for
// the element's size.
func (w *ew) splitMetrics(gtx C) (h int, fs, rad float32) {
	if w.e.Size == "medium" {
		return dp(gtx, 36), 15, 8
	}
	return dp(gtx, 28), 13, 4
}

// splitSize is the group's box: the primary's Button and, with the
// experiment, the chevron's square beside it.
func (w *ew) splitSize(gtx C, maxW int) image.Point {
	h, fs, _ := w.splitMetrics(gtx)
	minW := dp(gtx, 56)
	if w.e.Size == "medium" {
		minW = dp(gtx, 80)
	}
	tw := runsWidth(gtx, w.th(), fs, buttonRuns(w.o.host(), w.splitPrimary().Text))
	bw := max(tw+2*dp(gtx, 12), minW)
	if w.splitOn() {
		bw += h
	}
	return image.Pt(min(bw, maxW), h)
}

// splitColors are a Button's fill, text and border for style (ghost has
// neither fill nor border).
func splitColors(cl colors, style string, hovered bool) (bg, fg, border color.NRGBA) {
	if style == "ghost" {
		if hovered {
			return cl.Hover, cl.Text, color.NRGBA{}
		}
		return color.NRGBA{}, cl.Text, color.NRGBA{}
	}
	return buttonColors(cl, style, hovered)
}

// layoutSplit draws the split_button and takes its clicks.
func (w *ew) layoutSplit(gtx C, sz image.Point) {
	h := w.o.host()
	th := w.th()
	cl := colorsOf(th)
	pa := w.splitPrimary()
	on := w.splitOn()
	_, fs, radDp := w.splitMetrics(gtx)
	rad := dp(gtx, radDp)
	style := w.splitStyle()
	off := w.disabled(gtx) || w.e.Disabled
	if w.e.IsLoading {
		// the LoadingButton over the group's box: no click while it loads
		drawButton(gtx, th, sz, style, "", radDp, fs, false, true, off)
		w.main.add(gtx, sz)
		return
	}
	cw := 0
	if on {
		cw = sz.Y
	}
	pw := sz.X - cw
	if w.main.clicked(gtx, h) && !off && (w.pressed.IsZero() || gtx.Now.Sub(w.pressed) >= splitDebounce) {
		w.pressed = gtx.Now
		w.splitAct(pa)
	}
	if on && w.more.clicked(gtx, h) && !off {
		openSplitMenu(w)
	}
	hovP, hovC := w.main.g.Hovered() && !off, on && (w.more.g.Hovered() || w.open) && !off
	bg, fg, border := splitColors(cl, style, false)
	if off {
		bg, fg, border = cl.Disabled, cl.DisText, color.NRGBA{}
	}
	box := image.Rectangle{Max: sz}
	// the group's shape: outer corners round, inner ones square
	if border.A > 0 {
		outline(gtx, box, rad, max(1, dp(gtx, 1)), border, bg)
	} else if bg.A > 0 {
		rrect(gtx, box, rad, bg)
	}
	hover := func(r image.Rectangle) {
		hb, _, _ := splitColors(cl, style, true)
		if border.A > 0 {
			r = r.Inset(max(1, dp(gtx, 1)))
		}
		defer clip.UniformRRect(box.Inset(boolPx(border.A > 0, max(1, dp(gtx, 1)))), max(rad-1, 0)).Push(gtx.Ops).Pop()
		paint.FillShape(gtx.Ops, hb, clip.Rect(r).Op())
	}
	if hovP {
		hover(image.Rect(0, 0, pw, sz.Y))
	}
	if hovC {
		hover(image.Rect(pw, 0, sz.X, sz.Y))
	}
	if on {
		// the border between the two: the first's own (border-inline-end)
		div := border
		switch {
		case off:
			div = cl.DisText
		case style == "primary": // light-dark(jade-60, jade-30)
			div = theme.PltColor("jade", "60", false)
			if th.Dark {
				div = theme.PltColor("jade", "30", true)
			}
		case style == "danger": // light-dark(flamingo-40, flamingo-50)
			div = theme.PltColor("flamingo", "40", false)
			if th.Dark {
				div = theme.PltColor("flamingo", "50", true)
			}
		}
		if div.A > 0 {
			bw := max(1, dp(gtx, 1))
			paint.FillShape(gtx.Ops, div, clip.Rect(image.Rect(pw-bw, 0, pw, sz.Y)).Op())
		}
	}
	// the label centred, cut at the padding (padding: 0 12px 1px)
	runs := buttonRuns(h, pa.Text)
	pad := dp(gtx, 12)
	tw := runsWidth(gtx, th, fs, runs)
	drawRuns(gtx, th, h, fs, fg, runs, (pw-min(tw, pw-2*pad))/2, sz.Y-dp(gtx, 1), pw-pad)
	w.main.add(gtx, image.Pt(pw, sz.Y))
	if !on {
		return
	}
	at(gtx, image.Pt(pw, 0), func(gtx C) D {
		is := dp(gtx, 16)
		at(gtx, image.Pt((cw-is)/2, (sz.Y-is)/2), func(gtx C) D { return icon(gtx, "caret-down-filled", 16, fg) })
		w.more.add(gtx, image.Pt(cw, sz.Y))
		if !off {
			popups.Tooltip(gtx, th, &w.tip, w.more.g.Hovered() && !w.open, image.Pt(cw, sz.Y), popups.Above, splitMoreLabel(textOf(pa.Text)), "")
		}
		return D{Size: image.Pt(cw, sz.Y)}
	})
}

// splitMoreLabel is the chevron's label and tooltip.
func splitMoreLabel(primary string) string { return "More options for " + primary }

// splitAct sends action a (its confirm first), then opens its url.
func (w *ew) splitAct(a *SplitAction) {
	act := func() {
		w.o.fire(w, splitPayload(w.bid, w.e, a), func(error) {})
		if a.URL != "" {
			openURL(a.URL)
		}
	}
	if a.Confirm != nil {
		w.confirm(a.Confirm, act, nil) // in a Block Kit modal its in-card speed bump
		return
	}
	act()
}

// splitPayload is onAction's {type, actionId, blockId, text, value} for
// action a of split_button e.
func splitPayload(blockID string, e *Element, a *SplitAction) json.RawMessage {
	m := map[string]any{"type": "split_button", "block_id": blockID, "action_id": e.ActionID}
	if a.Text != nil {
		m["text"] = a.Text
	}
	if a.Value != "" {
		m["value"] = a.Value
	}
	b, _ := json.Marshal(m)
	return b
}

// openSplitMenu opens the chevron's Menu of the secondary actions, at its
// bottom right.
func openSplitMenu(w *ew) {
	h := w.o.host()
	items := make([]popups.MenuItem, 0, len(w.e.SecondaryActions))
	for i := range w.e.SecondaryActions {
		a := &w.e.SecondaryActions[i]
		items = append(items, popups.MenuItem{Label: textOf(a.Text), External: a.URL != "", Do: func() { w.splitAct(a) }})
	}
	w.open = true
	m := popups.NewMenuOpts(h, items, popups.MenuOpts{Anchor: w.more.rect(), Popover: "bottom-right", OnClose: func() { w.open = false; w.o.changed() }})
	h.ShowOverlay(m)
}

// boolPx is px when b, else 0.
func boolPx(b bool, px int) int {
	if b {
		return px
	}
	return 0
}
