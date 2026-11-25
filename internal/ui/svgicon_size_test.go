package ui

import (
	"slices"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/ui/icons"
)

// Synthetic fixtures only.

// Slack's SvgIcon with no size is 1em of the font it sits in ([data-5jx]
// {width:1em;height:1em;font-size:var(--s,inherit)}): the top bar's
// search icon is 1em of BaseSearch's 15px button, its text 12 + 15 + 8px
// in (Clack drew a 16px icon, the text at 34); the sidebar filter's clear
// icon is 1em of .c-filter_input__reset_button's 18px (Clack drew 16).
func TestSvgIconSizesTopBarAndFilter(t *testing.T) {
	e, _ := filterEnv(t, nil)
	a := e.a
	var got map[string][]int
	prev := icons.DrawSizeHook
	icons.DrawSizeHook = func(name string, px int) { got[name] = append(got[name], px) }
	t.Cleanup(func() { icons.DrawSizeHook = prev })

	got = map[string][]int{}
	e.frame()
	if px := got["search"]; !slices.Contains(px, 15) || slices.Contains(px, 16) {
		t.Errorf("the window drew search at %v px, want the top bar's at 15 (none at 16)", px)
	}
	if a.searchBoxIcon() {
		gtx := e.frameContext()
		if x := a.searchBoxTextX(gtx); x != gtx.Dp(35) {
			t.Errorf("the search text starts at %d, want 35 (12 + 15 + 8)", x)
		}
	}

	e.typeFilter(t, someChannelFragment(t, a))
	e.until("the filter's text", 3*time.Second, func() bool { return a.sidebar.filter.text != "" })
	got = map[string][]int{}
	e.frame()
	if px := got["close-circle-filled"]; !slices.Equal(px, []int{18}) {
		t.Errorf("the filter drew its clear icon at %v px, want [18]", px)
	}
}
