package popups

import (
	"errors"
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/chart"
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Synthetic fixtures only.

func chartHost(t *testing.T, copyImage bool) *tzHost {
	t.Helper()
	a := core.New(slack.New(slack.Creds{}), store.New(""))
	// on by default (the account's group, defaultExperimentGroups)
	group := "off"
	if copyImage {
		group = "on"
	}
	a.SetExperimentGroup("data_visualization_copy_image", group)
	th := theme.New(false)
	sh := theme.NewShaper()
	th.SetShapers(sh, sh)
	return &tzHost{a: a, th: th}
}

func menuLabels(m *Menu) []string {
	var out []string
	for _, it := range m.items {
		switch {
		case it.Separator:
			out = append(out, "-")
		case it.Header != "":
			out = append(out, "#"+it.Header)
		default:
			s := it.Label
			if it.Disabled {
				s += " (disabled)"
			}
			if it.Checked {
				s += " (checked)"
			}
			out = append(out, s)
		}
	}
	return out
}

func startupDataViz(previews bool) *richtext.DataViz {
	dv := &richtext.DataViz{BlockID: "viz-bar-single", Title: "startup loading time",
		Chart: chart.ParseChart([]byte(`{"type":"bar","series":[{"name":"time to window (ms)","data":[{"label":"Slack","value":1059},{"label":"Slick","value":1816}]}],"axis_config":{"categories":["Slack","Slick"]}}`))}
	if previews {
		dv.PreviewLight, dv.PreviewDark = "https://files.slack.com/files-pri/T1-F1/light.png", "https://files.slack.com/files-pri/T1-F1/dark.png"
	}
	return dv
}

// The chart's More actions: View as table and Download chart data, then
// with data_visualization_copy_image a separator and Copy as image, off
// without a preview image.
func TestChartMenu(t *testing.T) {
	btn := image.Rect(600, 20, 632, 52)
	if got := strings.Join(menuLabels(NewChartMenu(chartHost(t, false), btn, startupDataViz(true))), "|"); got != "View as table|Download chart data" {
		t.Errorf("without the experiment: %q", got)
	}
	if got := strings.Join(menuLabels(NewChartMenu(chartHost(t, true), btn, startupDataViz(true))), "|"); got != "View as table|Download chart data|-|Copy as image" {
		t.Errorf("with previews: %q", got)
	}
	if got := strings.Join(menuLabels(NewChartMenu(chartHost(t, true), btn, startupDataViz(false))), "|"); got != "View as table|Download chart data|-|Copy as image (disabled)" {
		t.Errorf("without previews: %q", got)
	}
	dv := startupDataViz(true)
	if ChartPreview(dv, false) != dv.PreviewLight || ChartPreview(dv, true) != dv.PreviewDark {
		t.Errorf("the theme's preview")
	}
	dv.PreviewDark = ""
	if ChartPreview(dv, true) != dv.PreviewLight {
		t.Errorf("dark falls back to the light preview")
	}
}

// Copy as image: the preview under the title strip (64 + 72 + 48px, the
// theme's background), "Image copied to clipboard"; a failed fetch is
// "Failed to copy image".
func TestCopyChartImage(t *testing.T) {
	for _, dark := range []bool{false, true} {
		h := chartHost(t, true)
		dv := startupDataViz(true)
		var asked []string
		got := copyCatch(t, func(u string) ([]byte, error) {
			asked = append(asked, u)
			return pngOf(t, 300, 180), nil
		})
		CopyChartImage(h, dv, dark)
		deadline := time.Now().Add(10 * time.Second)
		for got() == nil && len(h.toastList()) == 0 && time.Now().Before(deadline) {
			Pump()
			time.Sleep(10 * time.Millisecond)
		}
		img := got()
		if img == nil {
			t.Fatalf("dark %v: nothing copied (toasts %q)", dark, h.toastList())
		}
		if want := ChartPreview(dv, dark); len(asked) != 1 || asked[0] != want {
			t.Errorf("dark %v: fetched %q, want %q", dark, asked, want)
		}
		if b := img.Bounds(); b.Dx() != 300 || b.Dy() != 180+64+72+48 {
			t.Errorf("dark %v: composite %v, want 300×%d", dark, b, 180+184)
		}
		bg := color.NRGBAModel.Convert(img.At(2, 2)).(color.NRGBA)
		want := color.NRGBA{0xff, 0xff, 0xff, 0xff}
		if dark {
			want = color.NRGBA{0x1a, 0x1d, 0x21, 0xff}
		}
		if bg != want {
			t.Errorf("dark %v: strip background %v, want %v", dark, bg, want)
		}
		// the title's ink in the strip
		ink := false
		for x := 48; x < 290 && !ink; x++ {
			for y := 64; y < 64+72; y++ {
				if c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA); c != want {
					ink = true
					break
				}
			}
		}
		if !ink {
			t.Errorf("dark %v: no title in the strip", dark)
		}
		Pump()
		if ts := h.toastList(); len(ts) != 1 || ts[0] != "Image copied to clipboard" {
			t.Errorf("dark %v: toasts %q", dark, ts)
		}
	}
	// a fetch that fails
	h := chartHost(t, true)
	copyCatch(t, func(string) ([]byte, error) { return nil, errors.New("offline") })
	CopyChartImage(h, startupDataViz(true), false)
	deadline := time.Now().Add(10 * time.Second)
	for len(h.toastList()) == 0 && time.Now().Before(deadline) {
		Pump()
		time.Sleep(10 * time.Millisecond)
	}
	if ts := h.toastList(); len(ts) != 1 || ts[0] != "Failed to copy image" {
		t.Errorf("failed fetch: toasts %q", ts)
	}
}

// The data_table's More actions and a column's Sort menu.
func TestDataTableMenus(t *testing.T) {
	h := chartHost(t, true)
	d := richtext.FromMessage(h.a.S, &slack.Message{TS: "1.0", Text: "t", Blocks: []byte(`[{"type":"data_table","block_id":"b","caption":"Points","rows":[[{"type":"raw_text","text":"Name"},{"type":"raw_text","text":"Points"}],[{"type":"raw_text","text":"a"},{"type":"raw_number","value":3,"text":"3"}]]}]`)})
	dt := d.Blocks[0].DataTable
	c := richview.NewTableControl(dt, nil)
	btn := image.Rect(600, 10, 628, 38)
	if got := strings.Join(menuLabels(NewDataTableMenu(h, btn, c)), "|"); got != "Copy table|Download table" {
		t.Errorf("More actions: %q", got)
	}
	c.Column = 1
	if got := strings.Join(menuLabels(NewDataTableSortMenu(h, btn, c)), "|"); got != "#Sort|Ascending|Descending" {
		t.Errorf("unsorted column: %q", got)
	}
	c.Sort(1, true)
	if got := strings.Join(menuLabels(NewDataTableSortMenu(h, btn, c)), "|"); got != "#Sort|Ascending|Descending (checked)|-|Clear Sort" {
		t.Errorf("sorted column: %q", got)
	}
	m := NewDataTableSortMenu(h, btn, c)
	m.items[len(m.items)-1].Do() // Clear Sort
	if _, _, ok := c.SortState(); ok {
		t.Errorf("Clear Sort left the sort")
	}
	if got, want := dt.TSV(), "Name\tPoints\na\t3"; got != want {
		t.Errorf("Copy table's TSV %q, want %q", got, want)
	}
}

// stubElements is an element host the fullscreen view must draw with.
type stubElements struct{ n *int }

func (s stubElements) ElementsSize(bs []richtext.Button, maxW int, _ bool, _ unit.Metric) (image.Point, bool) {
	return image.Pt(min(80, maxW), 28), false
}
func (s stubElements) LayoutElements(gtx layout.Context, _ []richtext.Button, _ bool) layout.Dimensions {
	*s.n++
	return layout.Dimensions{Size: gtx.Constraints.Max}
}
func (stubElements) ElementsVersion() uint32 { return 0 }

// View {caption}'s table draws its action cells through the inline
// table's element host, so they send block_actions.
func TestDataTableFullscreenElements(t *testing.T) {
	h := chartHost(t, true)
	d := richtext.FromMessage(h.a.S, &slack.Message{TS: "1.0", Text: "t", Blocks: []byte(`[{"type":"data_table","block_id":"dtact","caption":"Tasks","rows":[[{"type":"raw_text","text":"Task"},{"type":"raw_text","text":"Action"}],[{"type":"raw_text","text":"a"},{"type":"action_cell","element":{"type":"button","action_id":"done_a","text":{"type":"plain_text","text":"Mark done"},"value":"a"}}]]}]`)})
	if len(d.Blocks) != 1 || d.Blocks[0].DataTable == nil {
		t.Fatalf("not a data_table: %+v", d.Blocks)
	}
	var drawn int
	c := richview.NewTableControl(d.Blocks[0].DataTable, nil)
	c.Elements = stubElements{&drawn}
	m, v := newDataTableFull(h, c, false)
	if m == nil || v == nil {
		t.Fatal("no fullscreen view")
	}
	if _, ok := v.Options().Elements.(stubElements); !ok {
		t.Fatalf("the fullscreen table's element host is %T, want the inline table's", v.Options().Elements)
	}
}
