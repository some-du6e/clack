package popups

import (
	"image"
	"image/color"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// The experiments the menus, selects and buttons read (docs/EXPERIMENTS.md,
// "Menus, selects and buttons"), build 4d0c78085232:
//
//   - slack_kit_v2_button: getGroupForUser "on" (gantry-v2-async-message-
//     share-dialog jdUv isSlackKitV2ButtonEnabled): the share (forward)
//     dialog's footer uses Slack Kit's v2 Button (theme/button.go).
//   - a11y_no_auto_highlight_search_multi_select and
//     basic_select_placeholder_option: session toggles (client-boot MrD9 Hk
//     and tS, read with NEu9 isToggleEnabledForSession by the legacy
//     SelectOptionsList Fd8o, BasicSelect zDm3 and SearchSelect ivsZ).
//     A session toggle is enabled when its group is one of its
//     treatmentGroups (V1z/: "treatment" and "on" for every MrD9 toggle);
//     any other group, or none, is off. Slack latches the group in
//     localStorage for the next session (setToggleGroupsForNextSession);
//     Clack, whose overrides apply live, reads the current one.
const (
	ExpSlackKitV2Button                 = "slack_kit_v2_button"
	ExpNoAutoHighlightSearchMultiSelect = "a11y_no_auto_highlight_search_multi_select"
	ExpBasicSelectPlaceholderOption     = "basic_select_placeholder_option"
)

// expOn is `"on" === getGroupForUser(state, name)`.
func expOn(h Host, name string) bool {
	if h == nil {
		return false
	}
	a := h.Core()
	return a != nil && a.Experiment(name) == "on"
}

// sessionToggle is isToggleEnabledForSession for a toggle with the default
// treatment groups.
func sessionToggle(h Host, name string) bool {
	if h == nil {
		return false
	}
	a := h.Core()
	if a == nil {
		return false
	}
	switch a.Experiment(name) {
	case "treatment", "on":
		return true
	}
	return false
}

// noAutoHighlight is a11y_no_auto_highlight_search_multi_select: options
// lists open without an active row; the first selectable row is
// pseudo-active instead (getPseudoActiveRowIndex), and Enter picks it.
func noAutoHighlight(h Host) bool { return sessionToggle(h, ExpNoAutoHighlightSearchMultiSelect) }

// placeholderOption is basic_select_placeholder_option: a basic select
// without a preselected option lists its placeholder first
// ("--Select an option--", formatPlaceholderText nb8N), and its closed
// button shows that text in the button's own colour.
func placeholderOption(h Host) bool { return sessionToggle(h, ExpBasicSelectPlaceholderOption) }

// formatPlaceholderText is nb8N: "--{placeholder}--", "--Select an
// option--" without one.
func formatPlaceholderText(s string) string {
	if s == "" {
		s = "Select an option"
	}
	return "--" + s + "--"
}

// selectPseudoActive is .c-select_options_list__option--pseudo-active's
// background, var(--dt_color-surf-pry), over the list's (both option
// lists here sit on the popups card background).
func selectPseudoActive(th *theme.Theme) color.NRGBA {
	return overMenu(th, chipTok{"var(--dt_color-surf-pry)", rgba8(0x1d1c1d, 0x0f), rgba8(0xf8f8f8, 0x0f)}.of(th))
}

// enterKeyChip draws the pseudo-active row's KeyboardKey "Enter" (HgMH,
// slim and flush: 20px tall, padding-inline 4, 13px, base.tertiary with a
// 1px tertiary bottom border, radius 4) with its right edge at right,
// centred in a row h tall; it returns its left edge.
func enterKeyChip(gtx layout.Context, th *theme.Theme, right, h int) int {
	bg := chipTok{"var(--dt_color-base-ter)", rgb(0xeaeaea), rgb(0x212428)}.of(th)
	// the border paints over the key's own background (border-box)
	edge := theme.Over(chipTok{"var(--dt_color-otl-ter)", rgba8(0x5e5d60, 0x21), rgba8(0x797c81, 0x4d)}.of(th), bg)
	fg := tokContentPry.of(th)
	const label = "Enter" // Slack's option list passes the literal (label: "Enter")
	ch := dp(gtx, 20)
	w := max(ch, textW(gtx, th, 13, theme.Regular, label)+2*dp(gtx, 4))
	r := image.Rect(right-w, (h-ch)/2, right, (h-ch)/2+ch)
	rad := dp(gtx, 4)
	rrect(gtx, r, rad, edge)                                                               // the bottom border shows under the face
	rrect(gtx, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Max.Y-max(1, dp(gtx, 1))), rad, bg) // the face
	at(gtx, image.Pt(r.Min.X+(w-textW(gtx, th, 13, theme.Regular, label))/2, r.Min.Y), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, ch, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 13, theme.Regular, fg, label)
		})
	})
	return r.Min.X
}
