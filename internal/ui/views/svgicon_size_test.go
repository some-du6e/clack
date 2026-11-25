package views

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
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Synthetic fixtures only.

// iconSizeCheck is a drawing and the px it should draw an icon at.
type iconSizeCheck struct {
	what, icon string
	want       int
	draw       func(gtx C)
}

// iconSizes runs draw once and returns the px of each icon it drew, by
// the icon's registry name.
func iconSizes(t *testing.T, draw func(gtx C)) map[string][]int {
	t.Helper()
	got := map[string][]int{}
	prev := icons.DrawSizeHook
	icons.DrawSizeHook = func(name string, px int) { got[name] = append(got[name], px) }
	defer func() { icons.DrawSizeHook = prev }()
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Max: image.Pt(800, 600)}, Now: time.Unix(1700000000, 0)}
	draw(gtx)
	return got
}

// Slack's SvgIcon with no size is 1em of the font it sits in ([data-5jx]
// {width:1em;height:1em;font-size:var(--s,inherit)}), and Clack drew
// these at a size of its own: MediaFileThumbnail's play-filled and
// sound-filled on a Files row are the row's 15px (QuipIcon's glyph keeps
// its size 20); Activity's Mark Complete check and the Huddles page's
// New Huddle plus are their small Button's 13px; the Drafts page's Edit
// pencil is its small Button's 13px.
func TestSvgIconSizesViews(t *testing.T) {
	app, _ := vaFake(t)
	h := &vaHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	files := NewFiles(h)
	files.frame() // the theme and palette Layout sets each frame
	for _, c := range []struct {
		f     core.File
		glyph string
		want  int
	}{
		{core.File{ID: "F0SVGVID001", Name: "demo.mp4", Filetype: "mp4", Mime: "video/mp4", IsVideo: true}, "play-filled", 15},
		{core.File{ID: "F0SVGAUD001", Name: "memo.m4a", Filetype: "m4a", Mime: "audio/mp4"}, "sound-filled", 15},
		{core.File{ID: "F0SVGCNV001", Name: "Roadmap", Filetype: "quip", IsCanvas: true}, "canvas-content-filled", 20},
	} {
		it := &fileItem{c.f}
		got := iconSizes(t, func(gtx C) { files.fileTile(gtx, it, 36) })
		if px := got[icons.Slack(c.glyph)]; !slices.Equal(px, []int{c.want}) {
			t.Errorf("%s's tile drew %s at %v px, want [%d] (all drawn: %v)", c.f.Name, c.glyph, px, c.want, got)
		}
	}

	act, hud, dr := NewActivity(h), NewHuddles(h), NewDrafts(h)
	act.frame()
	hud.frame()
	dr.frame()
	checks := []iconSizeCheck{
		{"Mark Complete", "check", 13, func(gtx C) {
			act.smallButton(gtx, new(widget.Clickable), btnOutline, "check", "Mark Complete")
		}},
		{"New Huddle", "plus", 13, func(gtx C) { hud.newHuddleButton(gtx) }},
		{"Drafts' Edit", "edit", 13, func(gtx C) { dr.editButton(gtx) }},
	}
	for _, c := range checks {
		got := iconSizes(t, c.draw)
		if px := got[icons.Slack(c.icon)]; !slices.Equal(px, []int{c.want}) {
			t.Errorf("%s drew %s at %v px, want [%d] (all drawn: %v)", c.what, c.icon, px, c.want, got)
		}
	}
}
