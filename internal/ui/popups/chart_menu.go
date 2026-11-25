package popups

// A data_visualization block's More actions (canvas-data-visualization-
// block v4Jz, CDN build 9f450b8ad3db: DataVisualizationBlock's renderMenu,
// useDataVisualizationActions), a Menu at "right-bottom" of the button:
//
//	View as table           (table)    the chart's table in a modal
//	Download chart data     (download) <title slug>.tsv
//	-                                  (when Copy as image shows)
//	Copy as image           (image)    data_visualization_copy_image; off
//	                                   without a preview image
//
// Left out, as their experiments are off on the accounts Clack follows:
// Open in new window (bk_refresh_data_visualization_expanded_view, with
// the size variants), Copy chart and Share chart (share_and_copy_blocks);
// and the block_file_download IP check before a download or a copy (an
// Enterprise Grid policy). View as table opens Clack's modal, not Slack
// Kit's fullscreen one (TableFullscreenModal).

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"

	"gioui.org/layout"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/some-du6e/clack/assets/fonts"
	"github.com/some-du6e/clack/internal/chart"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
)

var nsBlockKit = i18n.NS("block_kit")

// NewChartMenu is the menu for dv's More actions button btn (window
// coordinates).
func NewChartMenu(h Host, btn image.Rectangle, dv *richtext.DataViz) *Menu {
	if dv == nil {
		return nil
	}
	items := []MenuItem{
		MenuItem{Label: "View as table", Icon: "table", Do: func() { OpenChartTable(h, dv) }}.In(nsBlockKit),
		MenuItem{Label: "Download chart data", Icon: "download", Do: func() { DownloadChartData(h, dv) }}.In(nsBlockKit),
	}
	if c := h.Core(); c != nil && c.Experiment("data_visualization_copy_image") == "on" {
		dark := false
		if RichStyle != nil {
			if st := RichStyle(h); st != nil {
				dark = st.Palette.Dark
			}
		}
		src := ChartPreview(dv, dark)
		items = append(items, Sep(), MenuItem{Label: "Copy as image", Icon: "image", Disabled: src == "",
			Do: func() { CopyChartImage(h, dv, dark) }}.In(nsBlockKit))
	}
	return NewMenuOpts(h, items, MenuOpts{Anchor: btn, Popover: "right-bottom", TryOpposite: true})
}

// ChartPreview is the preview image Copy as image copies: the theme's,
// else the other one ("" none).
func ChartPreview(dv *richtext.DataViz, dark bool) string {
	if dark {
		if dv.PreviewDark != "" {
			return dv.PreviewDark
		}
		return dv.PreviewLight
	}
	if dv.PreviewLight != "" {
		return dv.PreviewLight
	}
	return dv.PreviewDark
}

// OpenChartTable is View as table: convertDataVisualizationToTableBlock's
// table (the header row, then each series or segment) under the chart's
// title.
func OpenChartTable(h Host, dv *richtext.DataViz) {
	v := richview.New(chartTableDoc(dv), richview.Options{NoHoverCards: true})
	title := dv.Title
	if title == "" {
		title = nsBlockKit.T("Table")
	}
	m := NewModal(h, ModalOpts{Title: title, Width: 720, Body: func(gtx layout.Context) layout.Dimensions {
		if RichStyle == nil {
			return layout.Dimensions{}
		}
		st := RichStyle(h)
		if st == nil {
			return layout.Dimensions{}
		}
		return v.Layout(gtx, st, topicHandler{h})
	}})
	h.ShowOverlay(m)
}

// chartTableDoc is View as table's table: every row, as Slack's
// TableFullscreenModal draws it (no TablePreview cut after 10 rows).
func chartTableDoc(dv *richtext.DataViz) richtext.Doc {
	headers, rows := dv.Chart.Table()
	tb := richtext.Block{Kind: richtext.Table, Kit: true, Whole: true}
	text := func(s string) [][]richtext.Span {
		return [][]richtext.Span{{{Kind: richtext.Text, Text: s}}}
	}
	var hr [][]richtext.Span
	for _, hd := range headers {
		hr = append(hr, text(hd)...)
	}
	tb.Rows = append(tb.Rows, hr)
	for _, r := range rows {
		var cells [][]richtext.Span
		for _, c := range r {
			if c.Null {
				cells = append(cells, nil)
				continue
			}
			cells = append(cells, text(c.Text)...)
		}
		tb.Rows = append(tb.Rows, cells)
	}
	return richtext.Doc{Blocks: []richtext.Block{tb}}
}

// DownloadChartData is Download chart data: the chart's TSV as
// <filenameSlugFromLabel(title, "chart-data")>.tsv in the downloads.
func DownloadChartData(h Host, dv *richtext.DataViz) {
	tsv := dv.Chart.TSV()
	name := chart.FilenameSlug(dv.Title, "chart-data") + ".tsv"
	run(h, func(ctx context.Context) (string, error) {
		return lbSave(name, "", []byte(tsv))
	}, func(p string, err error) {
		if errors.Is(err, ErrDownloadCanceled) {
			return
		}
		if err != nil {
			h.Toast("Couldn't download: " + errText(err))
			return
		}
		h.Toast("Saved to " + p)
	})
}

// The title strip copyDataVisualizationAsImage puts above the preview
// (compositeWithTitle): bold 72px Lato from (48, 64), the image under its
// 64 + 72 + 48px, on the theme's background.
var (
	chartTitleLight = chartTitleStyle{fg: color.NRGBA{0x1d, 0x1c, 0x1d, 0xff}, bg: color.NRGBA{0xff, 0xff, 0xff, 0xff}}
	chartTitleDark  = chartTitleStyle{fg: color.NRGBA{0xf8, 0xf8, 0xf8, 0xff}, bg: color.NRGBA{0x1a, 0x1d, 0x21, 0xff}}
)

type chartTitleStyle struct{ fg, bg color.NRGBA }

const (
	chartTitleTop    = 64
	chartTitleBottom = 48
	chartTitleX      = 48
	chartTitleLine   = 72
)

// CopyChartImage is Copy as image: the preview image (fetched with the
// session's credentials) under the title strip, as a PNG on the
// clipboard, the bitmap alone (Slack's canvas-data-visualization-block,
// build e277bbacc0bc: navigator.clipboard.write with an image/png
// ClipboardItem, so no markup, no bookmark); "Image copied to clipboard"
// (check), else "Failed to copy image" (warning).
func CopyChartImage(h Host, dv *richtext.DataViz, dark bool) {
	src := ChartPreview(dv, dark)
	if src == "" {
		return
	}
	style := chartTitleLight
	if dark {
		style = chartTitleDark
	}
	title := dv.Title
	run(h, func(ctx context.Context) (struct{}, error) {
		data, err := copyFetch(ctx, h, src, true)
		if err != nil {
			return struct{}{}, err
		}
		return struct{}{}, imgcache.WithBudget(ctx, func(b *imgcache.Budget) error {
			img, err := imgcache.DecodeBudget(data, imgcache.MaxPixels, imgcache.MaxSide, b)
			if err != nil {
				return err
			}
			out, err := compositeChartTitle(img, title, style)
			if err != nil {
				return err
			}
			return copyWrite(ctx, out, nil, imgcache.ChargeFunc(b))
		})
	}, func(_ struct{}, err error) {
		if err != nil {
			toastIcon(h, nsBlockKit.T("Failed to copy image"), "warning")
			return
		}
		toastIcon(h, nsBlockKit.T("Image copied to clipboard"), "check")
	})
}

// compositeChartTitle is compositeWithTitle: a canvas the image's width
// and its height plus the strip, the background, the title (fillText at
// maxWidth width - 96: squeezed sideways when wider), the image under.
func compositeChartTitle(img image.Image, title string, s chartTitleStyle) (*image.NRGBA, error) {
	b := img.Bounds()
	strip := chartTitleTop + chartTitleLine + chartTitleBottom
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()+strip))
	draw.Draw(out, out.Bounds(), image.NewUniform(s.bg), image.Point{}, draw.Src)
	if title != "" {
		f, err := opentype.Parse(fonts.Bold)
		if err != nil {
			return nil, err
		}
		face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: 72, DPI: 72, Hinting: font.HintingNone})
		if err != nil {
			return nil, err
		}
		defer face.Close()
		adv := font.MeasureString(face, title).Ceil()
		m := face.Metrics()
		// textBaseline "top": the em box's top at y 64
		h := (m.Ascent + m.Descent).Ceil()
		line := image.NewNRGBA(image.Rect(0, 0, max(adv, 1), max(h, 1)))
		d := font.Drawer{Dst: line, Src: image.NewUniform(s.fg), Face: face, Dot: fixed.Point26_6{X: 0, Y: m.Ascent}}
		d.DrawString(title)
		maxW := b.Dx() - 2*chartTitleX
		dst := image.Rect(chartTitleX, chartTitleTop, chartTitleX+adv, chartTitleTop+h)
		if adv > maxW && maxW > 0 {
			dst.Max.X = chartTitleX + maxW
			xdraw.BiLinear.Scale(out, dst, line, line.Bounds(), draw.Over, nil)
		} else {
			draw.Draw(out, dst, line, image.Point{}, draw.Over)
		}
	}
	draw.Draw(out, image.Rect(0, strip, b.Dx(), strip+b.Dy()), img, b.Min, draw.Src)
	return out, nil
}
