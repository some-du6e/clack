package popups

import (
	"errors"
	"image"
	"image/color"
	"strconv"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"

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
		// the toast is posted after the copy's work returns
		for len(h.toastList()) == 0 && time.Now().Before(deadline) {
			Pump()
			time.Sleep(10 * time.Millisecond)
		}
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

// dtRichStyle gives the popups' richviews a style for the test.
func dtRichStyle(t *testing.T) {
	t.Helper()
	prev := RichStyle
	t.Cleanup(func() { RichStyle = prev })
	sh := theme.NewShaper()
	st := richview.NewStyle(sh, nil, richview.LightPalette())
	st.Face = theme.Typeface
	RichStyle = func(Host) *richview.Style { return st }
}

// View as table's table is drawn whole (TableFullscreenModal), not cut
// after 10 rows as a message's table is: twelve series, twelve rows.
func TestChartTableWhole(t *testing.T) {
	h := chartHost(t, true)
	var series []string
	for i := 1; i <= 12; i++ {
		series = append(series, `{"name":"s`+strconv.Itoa(i)+`","data":[{"label":"x","value":`+strconv.Itoa(i)+`}]}`)
	}
	d := richtext.FromMessage(h.a.S, &slack.Message{TS: "1.0", Text: "t", Blocks: []byte(`[{"type":"data_visualization","title":"t","chart":{"type":"bar","series":[` + strings.Join(series, ",") + `],"axis_config":{"categories":["x"]}}}]`)})
	if len(d.Blocks) != 1 || d.Blocks[0].DataViz == nil {
		t.Fatalf("blocks %+v", d.Blocks)
	}
	doc := chartTableDoc(d.Blocks[0].DataViz)
	if len(doc.Blocks) != 1 || doc.Blocks[0].Kind != richtext.Table || !doc.Blocks[0].Whole || len(doc.Blocks[0].Rows) != 13 {
		t.Errorf("View as table: %d blocks, whole %v, %d rows; want one whole table of the header and 12 rows", len(doc.Blocks), len(doc.Blocks) > 0 && doc.Blocks[0].Whole, len(doc.Blocks[0].Rows))
	}
}

// A menu from a richview whose rect isn't in window coordinates (its
// window origin unknown) opens at the pointer, which is on the button;
// a rect in window coordinates is the menu's anchor as it is.
func TestActionAnchor(t *testing.T) {
	prev := theme.WindowPointer
	t.Cleanup(func() { theme.WindowPointer = prev })
	theme.WindowPointer = func() (image.Point, bool) { return image.Pt(640, 410), true }
	btn := image.Rect(10, 20, 38, 48)
	if got := ActionAnchor(btn, false); got != image.Rect(640, 410, 640, 410) {
		t.Errorf("view coordinates: anchor %v, want the pointer (640,410)", got)
	}
	if got := ActionAnchor(btn, true); got != btn {
		t.Errorf("window coordinates: anchor %v, want %v", got, btn)
	}
	theme.WindowPointer = func() (image.Point, bool) { return image.Point{}, false }
	if got := ActionAnchor(btn, false); got != btn {
		t.Errorf("no pointer: anchor %v, want %v", got, btn)
	}
}

// A search card's data blocks reach their menus: the card's handler takes
// a data_table's header buttons and a chart's More actions.
func TestSearchCardDataBlockHandlers(t *testing.T) {
	h := chartHost(t, true)
	var ch any = srCardHandler{srWOHandler: srWOHandler{v: &SearchView{h: h}}}
	btn := image.Rect(600, 10, 628, 38)
	if dh, ok := ch.(richview.DataTableHandler); !ok {
		t.Error("a search card's handler takes no data_table actions")
	} else {
		d := richtext.FromMessage(h.a.S, &slack.Message{TS: "1.0", Text: "t", Blocks: []byte(`[{"type":"data_table","block_id":"b","caption":"Points","rows":[[{"type":"raw_text","text":"Name"}],[{"type":"raw_text","text":"a"}]]}]`)})
		c := richview.NewTableControl(d.Blocks[0].DataTable, nil)
		c.Window = true
		dh.DataTableAction("more", c, btn)
		if n := h.shownCount(); n != 1 {
			t.Errorf("the table's More actions: %d overlays shown, want its menu", n)
		}
	}
	if cah, ok := ch.(richview.ChartActionHandler); !ok {
		t.Error("a search card's handler takes no chart actions")
	} else {
		n := h.shownCount()
		cah.ChartAction(startupDataViz(true), btn, true)
		if h.shownCount() != n+1 {
			t.Errorf("the chart's More actions: no menu shown")
		}
	}
}

// The fullscreen view's filter (DataTableBlockFilterInput): Search table
// gives the field the focus and the field is a Tab stop; with text its
// Clear button shows and is the next stop, and it clears the filter and
// gives the field the focus back; the empty state's Clear filter (the view
// cleared its state) clears the field and focuses it too.
func TestDataTableFullscreenFilter(t *testing.T) {
	h := chartHost(t, true)
	dtRichStyle(t)
	e := newMdEnv(t, h)
	d := richtext.FromMessage(h.a.S, &slack.Message{TS: "1.0", Text: "t", Blocks: []byte(`[{"type":"data_table","block_id":"b","caption":"Points","rows":[[{"type":"raw_text","text":"Name"}],[{"type":"raw_text","text":"alpha"}],[{"type":"raw_text","text":"beta"}]]}]`)})
	c := richview.NewTableControl(d.Blocks[0].DataTable, nil)
	var f *dtFull
	m := e.open(func() { f = openDataTableFull(h, c, true) })
	focused := func(tag any) bool { return e.r.Source().Focused(tag) }
	if !focused(&f.ed) {
		t.Fatal("Search table: the filter field hasn't the focus")
	}
	if n := len(m.o.FocusStops()); n != 1 {
		t.Errorf("no text: %d focus stops, want the field", n)
	}
	e.r.Queue(key.EditEvent{Range: key.Range{Start: 0, End: 0}, Text: "zzz"}, key.SelectionEvent{Start: 3, End: 3})
	e.settle()
	if got := f.full.FilterText(); got != "zzz" {
		t.Fatalf("typed: filter %q", got)
	}
	if n := len(m.o.FocusStops()); n != 2 {
		t.Errorf("with text: %d focus stops, want the field and Clear", n)
	}
	// Tab: Clear, the close button, the field again
	e.press(key.NameTab, 0)
	if !focused(&f.clear) {
		t.Error("Tab from the field: Clear hasn't the focus")
	}
	e.press(key.NameTab, 0)
	e.focus("Close", true)
	e.press(key.NameTab, 0)
	if !focused(&f.ed) {
		t.Error("Tab from Close: the field hasn't the focus back")
	}
	// Clear
	e.press(key.NameTab, 0)
	e.press(key.NameSpace, 0)
	e.settle()
	if f.ed.Text() != "" || f.full.FilterText() != "" {
		t.Errorf("Clear: field %q, filter %q, want both empty", f.ed.Text(), f.full.FilterText())
	}
	if !focused(&f.ed) {
		t.Error("Clear: the field hasn't the focus")
	}
	// the empty state's Clear filter, with the focus on Close
	f.ed.SetText("zzz")
	e.settle()
	e.press(key.NameTab, 0)
	e.press(key.NameTab, 0)
	e.focus("Close", true)
	dtFullHandler{topicHandler{h}, f}.DataTableAction("clearfilter", c, image.Rectangle{})
	e.settle()
	if f.ed.Text() != "" || !focused(&f.ed) {
		t.Errorf("Clear filter: field %q, focused %v; want it empty and focused", f.ed.Text(), focused(&f.ed))
	}
}
