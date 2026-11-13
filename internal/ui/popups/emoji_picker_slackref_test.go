package popups

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Slack's light emoji picker as a page for Chrome, next to Clack's (the
// emoji-picker workflow renders it and runs TestEmojiPickerVsSlack).
//
// The markup is rebuilt from the web client's source (build 2cd9270e786c,
// boot-shared Zu56 EmojiPicker, EmojiListRow, EmojiPickerFooter,
// EmojiPickerInput, the group tabs; the Emoji/GIFs container): the class
// names, nesting and the list's screen rows (getRowHeight: a 1px
// empty-header-row and a 27px heading per group, 33px emoji rows, 43px for
// the list's last; row 0 isn't rendered; data-color-index is
// (9 × row + column) mod 6). The styles are Slack's own: the rules of
// kit.css and client-boot-styles that can match it (testdata/slack-picker/
// slack.css.gz, every rule naming its classes plus the element, :root and
// theme rules, in file order with their @media/@layer wrappers). Lato is
// Clack's copy of the same font. The emoji are the same CDN images Clack
// draws, in the same order. Slack's icon font (the Emoji and GIFs tab
// icons, the category icons, the search glyph) isn't included: those boxes
// are left empty and the comparison masks them.

// epSlackPage writes the page and Slack's CSS into dir.
func epSlackPage(t *testing.T, dir string, p *EmojiPicker, fonts string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gz, err := os.ReadFile(filepath.Join("testdata", "slack-picker", "slack.css.gz"))
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		t.Fatal(err)
	}
	css, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "slack.css"), css, 0o644); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&b, f, a...) }
	w(`<!doctype html><html class="sk-client-theme--light"><head><meta charset="utf-8"><link rel="stylesheet" href="slack.css"><style>`)
	for _, f := range []struct {
		file   string
		weight int
	}{{"Lato-Regular.ttf", 400}, {"Lato-Bold.ttf", 700}, {"Lato-Black.ttf", 900}} {
		w(`@font-face{font-family:"Slack-Lato";font-weight:%d;src:url("%s/%s")}`, f.weight, fonts, f.file)
	}
	// the page around the picker: Clack's shot crops 12px around the card
	w(`html,body{margin:0;background:#fff}#wrap{position:absolute;left:12px;top:12px;width:361px}`)
	w(`</style></head><body class="sk-client-theme--light"><div id="wrap">`)
	w(`<div class="p-emoji_and_gif_pickers_container"><div class="c-tabs">`)
	w(`<div role="tablist" class="c-tabs__tab_menu c-tabs__tab_menu--full_width">`)
	w(`<button class="c-button-unstyled c-tabs__tab c-tabs__tab--active c-tabs__tab--full_width" role="tab"><span class="c-tabs__tab_content"><span class="c-tabs__tab_icon--left" style="width:20px;height:20px;display:inline-block"></span>Emoji</span></button>`)
	w(`<button class="c-button-unstyled c-tabs__tab c-tabs__tab--full_width" role="tab"><span class="c-tabs__tab_content"><span class="c-tabs__tab_icon--left" style="width:20px;height:20px;display:inline-block"></span>GIFs</span></button>`)
	w(`</div><div role="tabpanel" class="c-tabs__tab_panel c-tabs__tab_panel--active">`)
	w(`<div class="p-emoji_picker--with_gif_picker p-emoji_picker c-menu p-emoji_picker--style-v2" data-using-keyboard="false" data-qa="emoji-picker">`)
	w(`<div class="p-emoji_picker__content">`)
	// the category tabs: Search first, then the groups
	w(`<div role="presentation" class="p-emoji_picker__group_tabs p-emoji_picker__group_tabs--slack-kit-tabs"><div class="c-tabs"><div role="tablist" class="c-tabs__tab_menu p-emoji_picker__group_tabs-menu">`)
	for i := 0; i < 10; i++ {
		cls := "c-button-unstyled c-tabs__tab p-emoji_picker__group_tab"
		if i == 0 {
			cls += " p-emoji_picker__group_tab--active p-emoji_picker__group_tab--emoji-a11y c-tabs__tab--active"
		}
		w(`<button class="%s" role="tab"><span class="c-tabs__tab_content"><svg width="20" height="20"></svg></span></button>`, cls)
	}
	w(`</div></div></div>`)
	w(`<div class="p-emoji_picker__input_container"><input aria-label="Emoji name" class="c-input_text p-emoji_picker__input" type="text" placeholder="Search all emoji"></div>`)
	w(`<div class="p-emoji_picker__list_container" role="tabpanel">`)
	w(`<div class="p-emoji_picker__sticky_header"><div class="p-emoji_picker__heading p-emoji_picker__heading--emoji-14-a11y"><span aria-hidden="true">%s</span></div></div>`, html.EscapeString(epGroupNames[epGroupFrequent]))
	w(`<div class="p-emoji_picker__list"><div class="p-emoji_picker__list_scroller" style="position:relative;height:100%%;overflow:hidden;width:100%%">`)
	// screen rows of the first groups, as getAllRowsAndAriaAttributesForHeaders
	type srow struct {
		kind string
		name string
		ids  []int32
	}
	var rows []srow
	for g := 0; g < 3; g++ {
		ids := p.lists[g]
		rows = append(rows, srow{kind: "empty-header-row"}, srow{kind: "group", name: epGroupNames[g]})
		for i := 0; i < len(ids); i += 9 {
			rows = append(rows, srow{kind: "emoji", ids: ids[i:min(i+9, len(ids))]})
		}
	}
	y := 0
	for i, r := range rows {
		h := 33
		switch r.kind {
		case "empty-header-row":
			h = 1
		case "group":
			h = 27
		}
		if i == 0 { // renderRow returns null for index 0
			y += h
			continue
		}
		style := fmt.Sprintf("position:absolute;left:0;top:%dpx;height:%dpx;width:100%%", y, h)
		switch r.kind {
		case "empty-header-row":
			w(`<div class="p-emoji_picker__row" style="%s"><div></div></div>`, style)
		case "group":
			w(`<div class="p-emoji_picker__row" style="%s"><span role="columnheader" class="p-emoji_picker__heading p-emoji_picker__heading--emoji-14-no-bg">%s</span></div>`, style, html.EscapeString(r.name))
		default:
			w(`<div class="p-emoji_picker__row" style="%s">`, style)
			for c, id := range r.ids {
				w(`<button class="c-button-unstyled p-emoji_picker__list_item p-emoji_picker__list_item--a11y-not-selected" data-color-index="%d" role="gridcell"><span class="c-emoji c-emoji__medium"><img src="%s" alt="" width="22" height="22"></span></button>`,
					(9*i+c)%6, html.EscapeString(p.idx.url(id, p.tone)))
			}
			w(`</div>`)
		}
		y += h
	}
	w(`</div></div></div></div>`) // scroller, list, list container, content
	hand := ""
	if id, ok := p.idx.lookup("hand"); ok {
		hand = p.idx.url(id, p.tone)
	}
	w(`<div class="p-emoji_picker__footer" data-qa="emoji_picker__footer"><div class="p-emoji_picker__preview"></div>`)
	w(`<button class="c-button c-button--outline c-button--small p-emoji_picker__add_custom_emoji" type="button">Add Emoji</button>`)
	w(`<div class="p-emoji_picker_skintone"><button class="c-button-unstyled p-emoji_picker_skintone__toggle_btn"><span class="c-emoji c-emoji__medium"><img src="%s" alt="" width="22" height="22"></span><span class="p-emoji_picker_skintone__toggle_btn_label">Skin Tone</span></button></div>`, html.EscapeString(hand))
	w(`</div></div></div></div></div></div></body></html>`)
	page := filepath.Join(dir, "picker-light.html")
	if err := os.WriteFile(page, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return page
}

// TestEmojiPickerSlackPage writes Slack's light picker page (see above)
// into EMOJI_SHOTS_DIR/slack-ref for Chrome.
func TestEmojiPickerSlackPage(t *testing.T) {
	dir := os.Getenv("EMOJI_SHOTS_DIR")
	if dir == "" {
		t.Skip("EMOJI_SHOTS_DIR not set (the emoji-picker workflow)")
	}
	fonts, err := filepath.Abs(filepath.Join("..", "..", "..", "assets", "fonts"))
	if err != nil {
		t.Fatal(err)
	}
	h := epShotEnv(t, false)
	p := NewEmojiPicker(h, nil)
	page := epSlackPage(t, filepath.Join(dir, "slack-ref"), p, "file://"+filepath.ToSlash(fonts))
	t.Logf("wrote %s", page)
}

// TestEmojiPickerVsSlack compares Clack's light picker with Chrome's
// render of Slack's (SLACK_PICKER_PNG, 2x): a side-by-side image and a
// diff, and per band (the Emoji/GIFs tabs, the category tabs, the search
// field, the list, the footer) the share of pixels that differ and the
// mean difference, with the icon boxes masked. It reports; it doesn't fail
// on differences (the page is a rebuild of Slack's markup).
func TestEmojiPickerVsSlack(t *testing.T) {
	ref := os.Getenv("SLACK_PICKER_PNG")
	dir := os.Getenv("EMOJI_SHOTS_DIR")
	if ref == "" || dir == "" {
		t.Skip("SLACK_PICKER_PNG and EMOJI_SHOTS_DIR not set (the emoji-picker workflow)")
	}
	f, err := os.Open(ref)
	if err != nil {
		t.Fatal(err)
	}
	si, err := png.Decode(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	h := epShotEnv(t, false)
	ci := epPickerShot(t, h, false)
	const s = 2
	b := ci.Bounds().Intersect(si.Bounds())
	slack := image.NewRGBA(b)
	draw.Draw(slack, b, si, b.Min, draw.Src)
	diff := image.NewRGBA(b)
	side := image.NewRGBA(image.Rect(0, 0, b.Dx()*3, b.Dy()))
	draw.Draw(side, b, slack, b.Min, draw.Src)
	draw.Draw(side, b.Add(image.Pt(b.Dx(), 0)), ci, b.Min, draw.Src)
	// card-relative bands (CSS px from the card's top, +12px margin)
	bands := []struct {
		name   string
		y0, y1 int
	}{{"Emoji/GIFs tabs", 0, 50}, {"category tabs", 50, 92}, {"search field", 92, 150}, {"list", 150, 458}, {"footer", 458, 518}}
	masks := []image.Rectangle{ // icons from Slack's icon font, absent from the page
		image.Rect(12, 62, 373, 100), // category icons
		image.Rect(26, 108, 56, 140), // search glyph
	}
	masked := func(x, y int) bool {
		p := image.Pt(x/s, y/s)
		for _, m := range masks {
			if p.In(m) {
				return true
			}
		}
		return false
	}
	abs := func(a, b uint8) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	var report strings.Builder
	for _, bd := range bands {
		n, nd, sum := 0, 0, 0
		for y := (12 + bd.y0) * s; y < (12+bd.y1)*s && y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				a, c := slack.RGBAAt(x, y), ci.RGBAAt(x, y)
				d := max(abs(a.R, c.R), abs(a.G, c.G), abs(a.B, c.B))
				if masked(x, y) {
					diff.SetRGBA(x, y, color.RGBA{R: 40, G: 40, B: 40, A: 255})
					continue
				}
				n++
				sum += d
				if d > 24 {
					nd++
					diff.SetRGBA(x, y, color.RGBA{R: 255, A: 255})
				} else {
					diff.SetRGBA(x, y, color.RGBA{R: c.R / 2, G: c.G / 2, B: c.B / 2, A: 255})
				}
			}
		}
		if n > 0 {
			fmt.Fprintf(&report, "%-16s %5.1f%% of pixels differ (>24), mean %4.1f\n", bd.name, 100*float64(nd)/float64(n), float64(sum)/float64(n))
		}
	}
	draw.Draw(side, b.Add(image.Pt(2*b.Dx(), 0)), diff, b.Min, draw.Src)
	epSaveShot(t, dir, "slack-vs-clack-light", side)
	epSaveShot(t, dir, "slack-vs-clack-light-diff", diff)
	_ = os.WriteFile(filepath.Join(dir, "slack-vs-clack-light.txt"), []byte(report.String()), 0o644)
	t.Logf("Slack (left) vs Clack (middle), light, 2x:\n%s", report.String())
}
