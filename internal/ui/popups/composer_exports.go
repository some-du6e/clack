package popups

import (
	"image"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// Pieces of Slack's form controls the message composer's dialogs
// (internal/ui/composer: the schedule dialog's DateTimePicker) draw with.

// SelectField draws Slack's select box w px wide (the date and time
// pickers' field: 36px / 15px, a leading icon, a trailing chevron); open
// draws the focus ring. Clicks land on btn.
func SelectField(gtx layout.Context, th *theme.Theme, btn *widget.Clickable, w int, iconName, label string, open, disabled bool) layout.Dimensions {
	return dtField(gtx, th, btn, w, dtLarge, iconName, label, true, open, disabled)
}

// SelectFieldHeight is SelectField's height.
func SelectFieldHeight(gtx layout.Context) int { return dtHeight(gtx, dtLarge) }

// NewTimePickerRange is NewTimePicker listing only the times from from to
// to minutes after midnight (TimePicker's min and max).
func NewTimePickerRange(h Host, anchor image.Rectangle, value, from, to, step int, onPick func(mins int)) Overlay {
	t := dtNewTimeListStep(h, anchor, value, from, step, onPick)
	n := len(t.slots)
	for n > 1 && t.slots[n-1] > to {
		n--
	}
	t.slots, t.labels, t.clicks = t.slots[:n], t.labels[:n], t.clicks[:n]
	if t.sel >= n {
		t.sel = -1
	}
	if t.list.Position.First >= n {
		t.list.Position.First = n - 1
	}
	return t
}

// ModalBodyOrigin is the window position of m's body origin, for
// anchoring a body field's popover (the date and time lists).
func ModalBodyOrigin(gtx layout.Context, m *Modal) image.Point { return dtModalOrigin(gtx, m) }

// CalendarDate is toCalendarDate's default form, "November 12th, 2026".
func CalendarDate(day time.Time) string { return dtDateLabel(day, day, dtLong) }

// Rect is the modal card's window rectangle at its last layout.
func (m *Modal) Rect() image.Rectangle { return m.rect }

// ColorsFor is popups' palette for th (input borders, focus rings), for
// dialogs drawn outside popups.
func ColorsFor(th *theme.Theme) *Colors { return colorsOf(th) }
