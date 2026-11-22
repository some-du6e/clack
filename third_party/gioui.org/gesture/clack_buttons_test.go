// SPDX-License-Identifier: Unlicense OR MIT

package gesture

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/internal/clickrule"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/op"
	"gioui.org/op/clip"
)

// Clack patch 48: a press of another button starts a new run of clicks, as
// Chromium counts: on Linux ui::MouseEvent::IsRepeatedClickEvent compares
// the presses' flags (the buttons down with the modifiers), on Windows
// WebMouseEventBuilder counts on only while result.button ==
// s_last_click_button. A right click (or a middle or back one) on the
// spot, then a left click inside the double-click time, is a single left
// click: not a double click that opens the composer +'s file chooser,
// resets a column or selects a word.
func TestClackClickRunButtons(t *testing.T) {
	for _, rule := range []struct {
		name string
		set  func(t *testing.T)
	}{
		{"Linux", func(t *testing.T) { withRule(t, clickrule.Default, nil) }},
		{"Windows", func(t *testing.T) {
			withRule(t, clickrule.Rule{FromFirst: true, MoveResets: true}, func() (time.Duration, image.Point) {
				return 500 * time.Millisecond, image.Pt(2, 2)
			})
		}},
	} {
		t.Run(rule.name, func(t *testing.T) {
			rule.set(t)
			for _, tc := range []struct {
				name    string
				buttons []pointer.Buttons
				want    []int // the left presses' counts
			}{
				{"right, then left", []pointer.Buttons{pointer.ButtonSecondary, pointer.ButtonPrimary}, []int{1}},
				{"middle, then left", []pointer.Buttons{pointer.ButtonTertiary, pointer.ButtonPrimary}, []int{1}},
				{"back, then left", []pointer.Buttons{pointer.ButtonQuaternary, pointer.ButtonPrimary}, []int{1}},
				{"left, right, left", []pointer.Buttons{pointer.ButtonPrimary, pointer.ButtonSecondary, pointer.ButtonPrimary}, []int{1, 1}},
				{"left, left: still a double click", []pointer.Buttons{pointer.ButtonPrimary, pointer.ButtonPrimary}, []int{1, 2}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if got := buttonRun(t, tc.buttons...); !sameInts(got, tc.want) {
						t.Errorf("the left clicks counted %v, want %v", got, tc.want)
					}
				})
			}
		})
	}
}

// buttonRun presses buttons on one spot 100 ms apart, through a router
// into a Click, and returns the counts of the clicks it reports (its
// primary button's).
func buttonRun(t *testing.T, buttons ...pointer.Buttons) []int {
	t.Helper()
	var c Click
	var r input.Router
	at := f32.Pt(50, 50)
	var got []int
	frame := func() {
		for {
			e, ok := c.Update(r.Source())
			if !ok {
				break
			}
			if e.Kind == KindClick {
				got = append(got, e.NumClicks)
			}
		}
		var ops op.Ops
		st := clip.Rect(image.Rect(0, 0, 100, 100)).Push(&ops)
		c.Add(&ops)
		st.Pop()
		r.Frame(&ops)
	}
	frame()
	for i, b := range buttons {
		tm := sec + time.Duration(i)*100*time.Millisecond
		r.Queue(
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: b, Position: at, Time: tm},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: at, Time: tm + 30*time.Millisecond})
		frame()
	}
	frame()
	return got
}
