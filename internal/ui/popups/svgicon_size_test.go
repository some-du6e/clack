package popups

import (
	"image"
	"slices"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Synthetic fixtures only.

// popupIconSizes runs draw (twice: a menu places its rows on its first
// frame) and returns the px of each icon its last run drew.
func popupIconSizes(t *testing.T, draw func(gtx layout.Context)) map[string][]int {
	t.Helper()
	var got map[string][]int
	prev := icons.DrawSizeHook
	icons.DrawSizeHook = func(name string, px int) { got[name] = append(got[name], px) }
	defer func() { icons.DrawSizeHook = prev }()
	var ops op.Ops
	for range 2 {
		got = map[string][]int{}
		ops.Reset()
		draw(layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Now: time.Unix(1700000000, 0),
			Constraints: layout.Constraints{Max: image.Pt(1200, 800)}})
	}
	return got
}

// Slack's SvgIcon with no size is 1em of the font it sits in ([data-5jx]
// {width:1em;height:1em;font-size:var(--s,inherit)}). A menu item's sits
// in .c-menu_item__icon, which sets no font-size, so it is 1em of the
// row's 15px (MenuIcon1em), where Slack's menus that pass a size get it
// (Clack's rows draw 18, 20 with a subtitle); the link indicator's
// new-window is 1em of .c-menu_item__link_icon's 15px (Clack drew 16).
// Slack's menus whose items pass no size: DataVisualizationBlock's and
// TextSelectionContextMenu's. In the invite dialog: the recommended
// channels' Tag icons are 1em of the Tag's 15px (Clack drew 20), the
// guest billing warning's info 1em of .p-channel_select--warning's 13px
// (16), the email errors' warning 1em of the line's 15px (16), the
// Customize button's magic-wand 1em of its 15px (20); the share dialog's
// Copy Link icon 1em of its link button's 15px (18).
func TestSvgIconSizesPopups(t *testing.T) {
	h, _, _ := emBooted(t)
	m := NewMenuOpts(h, []MenuItem{
		{Label: "Its own", Icon: "copy", Do: func() {}},
		{Label: "No size", Icon: "download", IconSize: MenuIcon1em, Do: func() {}},
		{Label: "Two lines", Icon: "image", Subtitle: "a subtitle", Do: func() {}},
		{Label: "A link", External: true, Do: func() {}},
	}, MenuOpts{Anchor: image.Rect(100, 100, 132, 132), Popover: "bottom-left"})
	got := popupIconSizes(t, func(gtx layout.Context) { m.Layout(gtx) })
	for name, want := range map[string]int{"copy": 18, "download": 15, "image": 20, "external": 15} {
		if px := got[icons.Slack(name)]; !slices.Equal(px, []int{want}) {
			t.Errorf("the menu drew %s at %v px, want [%d] (all drawn: %v)", name, px, want, got)
		}
	}

	for what, items := range map[string][]MenuItem{
		"text selection": NewTextSelectionMenu(h, image.Pt(10, 10), "some words").items,
		"chart":          NewChartMenu(h, image.Rect(10, 10, 42, 42), &richtext.DataViz{Title: "Sales"}).items,
	} {
		n := 0
		for _, it := range items {
			if it.Icon == "" {
				continue
			}
			n++
			if it.IconSize != MenuIcon1em {
				t.Errorf("the %s menu's %q has its icon at %v, want MenuIcon1em", what, it.Label, it.IconSize)
			}
		}
		if n == 0 {
			t.Errorf("the %s menu has no icons", what)
		}
	}

	th := theme.New(false)
	cl := colorsOf(th)
	bg, fg := menuTagColors(th, "informative")
	got = popupIconSizes(t, func(gtx layout.Context) {
		invRecTag(gtx, th, new(widget.Clickable), core.Conv{ID: "C0SVGTAG001", Name: "general", Kind: "channel"}, bg, fg, 600)
	})
	for _, name := range []string{"plus-filled", "channel"} {
		if px := got[icons.Slack(name)]; !slices.Equal(px, []int{15}) {
			t.Errorf("a recommended channel's Tag drew %s at %v px, want [15] (all drawn: %v)", name, px, got)
		}
	}
	inv := &InviteModal{h: h, chans: []core.Conv{{ID: "C0SVGTAG001", Name: "general"}, {ID: "C0SVGTAG002", Name: "random"}},
		emails: []string{"not an email"}}
	for _, c := range []struct {
		what, icon string
		want       int
		draw       func(gtx layout.Context)
	}{
		{"the guest billing warning", "info", 13, func(gtx layout.Context) { inv.guestWarning(gtx, th, cl, 600) }},
		{"the email error line", "warning", 15, func(gtx layout.Context) { inv.errorLines(gtx, th, cl) }},
		{"Add a message", "magic-wand", 15, func(gtx layout.Context) { inv.customizeButton(gtx, th, cl) }},
		{"Copy Link", "link", 15, func(gtx layout.Context) {
			(&actForward{}).drawCopyLink(gtx, th, new(widget.Clickable), BtnOutline, false, grpSide(0))
		}},
	} {
		got := popupIconSizes(t, c.draw)
		if px := got[icons.Slack(c.icon)]; !slices.Equal(px, []int{c.want}) {
			t.Errorf("%s drew %s at %v px, want [%d] (all drawn: %v)", c.what, c.icon, px, c.want, got)
		}
	}
}
