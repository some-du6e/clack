package composer

import (
	"image"
	"slices"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Synthetic fixtures only.

// A hovered file chip's remove button is Slack's RemoveFileButton:
// close-filled, an SvgIcon with no size, so 1em ([data-5jx]{width:1em;
// height:1em;font-size:var(--s,inherit)}) of
// .p-multi_file_upload__file_status_icon--remove's 12px in its 20px
// circle. Clack drew close at 14.
func TestChipRemoveIconSize(t *testing.T) {
	got := map[string][]int{}
	prev := icons.DrawSizeHook
	icons.DrawSizeHook = func(name string, px int) { got[name] = append(got[name], px) }
	defer func() { icons.DrawSizeHook = prev }()
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(20, 20))}
	drawChipStatus(gtx, theme.New(false), 3, 20)
	if px := got["close-filled"]; !slices.Equal(px, []int{12}) || len(got["close"]) > 0 {
		t.Errorf("the remove button drew %v, want close-filled at [12] px", got)
	}
}
