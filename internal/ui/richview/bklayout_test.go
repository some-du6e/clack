package richview

// Block Kit layout geometry against Slack's renderer (client CSS and
// components, 2026-11; see blocks.go margins, section, kitFields,
// contextRow, kitImage, table). Synthetic blocks only.

import (
	"encoding/json"
	"image"
	"strings"
	"testing"

	"gioui.org/font"

	rt "github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// kitDoc builds the document of a message carrying blocks (JSON).
func kitDoc(t *testing.T, blocks string) rt.Doc {
	t.Helper()
	m := slack.Message{TS: "1.0", BotID: "B1", Text: "fallback", Blocks: json.RawMessage(blocks)}
	if !json.Valid(m.Blocks) {
		t.Fatalf("bad fixture JSON: %s", blocks)
	}
	return rt.FromMessage(codeRes{}, &m)
}

func imgsWithURL(d *display, url string) []imgPrim {
	var out []imgPrim
	for _, im := range d.imgs {
		if im.url == url {
			out = append(out, im)
		}
	}
	return out
}

func TestKitBlockMargins(t *testing.T) {
	st := newTestStyle()
	d := kitDoc(t, `[
		{"type":"section","text":{"type":"mrkdwn","text":"Alpha"}},
		{"type":"section","text":{"type":"mrkdwn","text":"Bravo"}},
		{"type":"divider"},
		{"type":"context","elements":[{"type":"mrkdwn","text":"Charlie"}]},
		{"type":"header","text":{"type":"plain_text","text":"Delta"}},
		{"type":"image","image_url":"https://img.test/e.png","alt_text":"e","image_width":300,"image_height":200}
	]`)
	v := layoutAt(st, d, Options{}, 800)
	lh := 22
	a, b := findFrag(t, &v.d, "Alpha"), findFrag(t, &v.d, "Bravo")
	if a.top != 0 {
		t.Errorf("first section top %d, want 0 (.p-block_kit_renderer__block_wrapper--first)", a.top)
	}
	// section 4px bottom + section 8px top
	if want := lh + 12; b.top != want {
		t.Errorf("second section top %d, want %d", b.top, want)
	}
	div := boxesOf(&v.d, bFill)
	var dv *box
	for i := range div {
		if div[i].fill == st.Palette.Divider && div[i].r.Dy() == 1 {
			dv = &div[i]
		}
	}
	divY := b.top + lh + 4 + 8
	if dv == nil || dv.r.Min.Y != divY || dv.r.Dx() != 800 {
		t.Fatalf("divider %+v, want a 1px rule at y=%d", dv, divY)
	}
	c := findFrag(t, &v.d, "Charlie")
	if want := divY + 1 + 8 + 4; c.top != want {
		t.Errorf("context top %d, want %d", c.top, want)
	}
	h := findFrag(t, &v.d, "Delta")
	if want := c.top + lh + 4 + 15; h.top != want {
		t.Errorf("header top %d, want %d (context 4px + header 1em)", h.top, want)
	}
	im := imgsWithURL(&v.d, "https://img.test/e.png")
	if len(im) != 1 {
		t.Fatalf("image block imgs %d", len(im))
	}
	// header 4px + image 8px, then the 22px title row and 5px
	if want := image.Rect(0, h.top+lh+4+8+lh+5, 300, h.top+lh+4+8+lh+5+200); im[0].r != want {
		t.Errorf("image %v, want %v", im[0].r, want)
	}
	if want := im[0].r.Max.Y + 8; v.d.size.Y != want {
		t.Errorf("doc height %d, want %d (the last block's 8px bottom margin)", v.d.size.Y, want)
	}
}

func TestKitHeaderFirst(t *testing.T) {
	st := newTestStyle()
	v := layoutAt(st, kitDoc(t, `[{"type":"header","text":{"type":"plain_text","text":"Title"}}]`), Options{}, 400)
	f := findFrag(t, &v.d, "Title")
	if f.top != 4 || v.d.size.Y != 4+22+4 {
		t.Errorf("first header top %d height %d, want 4 and 30", f.top, v.d.size.Y)
	}
	if f.weight != font.Black {
		t.Errorf("header weight %d, want black (900)", f.weight)
	}
}

func TestKitFieldsGrid(t *testing.T) {
	st := newTestStyle()
	d := kitDoc(t, `[{"type":"section","text":{"type":"mrkdwn","text":"Summary"},"fields":[
		{"type":"mrkdwn","text":"one"},{"type":"mrkdwn","text":"two"},{"type":"plain_text","text":"three"},{"type":"mrkdwn","text":"four"},{"type":"mrkdwn","text":"five"}]}]`)
	v := layoutAt(st, d, Options{}, 1000) // fields are at most 600 wide
	f1, f2, f3, f4, f5 := findFrag(t, &v.d, "one"), findFrag(t, &v.d, "two"), findFrag(t, &v.d, "three"), findFrag(t, &v.d, "four"), findFrag(t, &v.d, "five")
	// calc(50% - 16px) columns, the second 16px from the first
	if f1.x != 0 || f3.x != 0 || f5.x != 0 || f2.x != 300 || f4.x != 300 {
		t.Errorf("columns x: %.0f %.0f / %.0f %.0f / %.0f, want 0 300", f1.x, f2.x, f3.x, f4.x, f5.x)
	}
	if f1.top != 22+12 || f2.top != f1.top || f3.top != f1.top+22 || f5.top != f3.top+22 {
		t.Errorf("rows: %d %d %d %d, want 34 34 56 78 (12px under the text, no row gap)", f1.top, f2.top, f3.top, f5.top)
	}
	// fields only: no top margin
	v = layoutAt(st, kitDoc(t, `[{"type":"section","fields":[{"type":"mrkdwn","text":"solo"}]}]`), Options{}, 500)
	if f := findFrag(t, &v.d, "solo"); f.top != 0 {
		t.Errorf("fields-only top %d, want 0", f.top)
	}
	// a narrow pane: columns are half the pane less 16
	v = layoutAt(st, d, Options{}, 400)
	if f := findFrag(t, &v.d, "two"); f.x != 200 {
		t.Errorf("narrow second column x %.0f, want 200", f.x)
	}
}

func TestKitSectionAccessory(t *testing.T) {
	st := newTestStyle()
	d := kitDoc(t, `[{"type":"section","text":{"type":"mrkdwn","text":"Wide pane text"},
		"accessory":{"type":"image","image_url":"https://img.test/acc.png","alt_text":"acc"}}]`)
	v := layoutAt(st, d, Options{}, 1200)
	im := imgsWithURL(&v.d, "https://img.test/acc.png")
	// .c-message__message_blocks .p-section_block--has_accessory: max 600
	if len(im) != 1 || im[0].r != image.Rect(600-88, 0, 600, 88) {
		t.Fatalf("accessory %+v, want 88x88 at the 600px block's right edge", im)
	}
	if v.d.size.Y != 88+4+4 {
		t.Errorf("height %d, want 96 (accessory 4px margin + section 4px)", v.d.size.Y)
	}
	// button fallback (no Elements host): 8px from the text
	d = kitDoc(t, `[{"type":"section","text":{"type":"mrkdwn","text":"word word word word word word word word word word word word word word"},
		"accessory":{"type":"button","text":{"type":"plain_text","text":"Go"},"action_id":"go"}}]`)
	v = layoutAt(st, d, Options{}, 300)
	var btn *box
	for _, bb := range buttonBoxes(&v.d) {
		bb := bb
		btn = &bb
	}
	if btn == nil || btn.r.Max.X != 300 {
		t.Fatalf("button %+v", btn)
	}
	for _, f := range textFrags(&v.d) {
		if f.sh.str != "Go" && f.x+f.w > float32(btn.r.Min.X-8)+0.5 {
			t.Errorf("text %q ends at %.1f, past the accessory's 8px margin (%d)", f.sh.str, f.x+f.w, btn.r.Min.X-8)
		}
	}
}

func TestKitContextFlex(t *testing.T) {
	st := newTestStyle()
	d := kitDoc(t, `[{"type":"context","elements":[
		{"type":"image","image_url":"https://img.test/av.png","alt_text":"av"},
		{"type":"mrkdwn","text":"by *bot*"},
		{"type":"plain_text","text":"later"}]}]`)
	v := layoutAt(st, d, Options{}, 600)
	im := imgsWithURL(&v.d, "https://img.test/av.png")
	if len(im) != 1 || im[0].r != image.Rect(0, 4, 20, 24) {
		t.Fatalf("context image %+v, want 20x20 at (0,4)", im)
	}
	by, bot, later := findFrag(t, &v.d, "by"), findFrag(t, &v.d, "bot"), findFrag(t, &v.d, "later")
	if by.x != 24 { // 20 + the image's 4px spacer
		t.Errorf("text after image x %.1f, want 24", by.x)
	}
	if by.size != 13 || by.col != st.Palette.Text2 {
		t.Errorf("context text %dpx %v", by.size, by.col)
	}
	if want := bot.x + bot.w + 16; later.x < want-0.5 || later.x > want+1.5 {
		t.Errorf("second text x %.1f, want ~%.1f (16px spacer)", later.x, want)
	}
	if by.top != 4 || by.bot-by.top != 22 {
		t.Errorf("context text line %d..%d, want a 22px line box at 4", by.top, by.bot)
	}
	if v.d.size.Y != 4+25+4 {
		t.Errorf("context height %d, want 33 (image row 25 + 4px margins)", v.d.size.Y)
	}
	// too narrow for the second text: it wraps to its own flex line
	w := int(bot.x+bot.w) + 16 + 10
	v = layoutAt(st, d, Options{}, w)
	later = findFrag(t, &v.d, "later")
	if later.x != 0 || later.top != 4+25 {
		t.Errorf("wrapped element at (%.1f,%d), want (0,29)", later.x, later.top)
	}
	if string(v.d.plain) != "by bot later" {
		t.Errorf("plain %q", v.d.plain)
	}
}

func TestKitImageBlock(t *testing.T) {
	st := newTestStyle()
	d := kitDoc(t, `[{"type":"image","image_url":"https://img.test/wide.png","alt_text":"wide","title":{"type":"plain_text","text":"Wide one"},
		"image_width":1200,"image_height":600,"image_bytes":24576},
		{"type":"image","image_url":"https://img.test/tall.png","alt_text":"tall","image_width":300,"image_height":900}]`)
	v := layoutAt(st, d, Options{}, 1000)
	wide, tall := imgsWithURL(&v.d, "https://img.test/wide.png"), imgsWithURL(&v.d, "https://img.test/tall.png")
	if len(wide) != 1 || wide[0].r.Size() != image.Pt(600, 300) || wide[0].r.Min.Y != 22+5 {
		t.Fatalf("wide %+v, want 600x300 under the 22px title row and 5px", wide)
	}
	if len(tall) != 1 || tall[0].r.Size() != image.Pt(133, 400) { // floor(300*4/9)
		t.Errorf("tall %+v, want 133x400", tall)
	}
	if wide[0].radii != [4]int{8, 8, 8, 8} {
		t.Errorf("radius %v, want 8", wide[0].radii)
	}
	ti := findFrag(t, &v.d, "Wide")
	if ti.col != st.Palette.Text2 || ti.size != 15 {
		t.Errorf("title %dpx %v", ti.size, ti.col)
	}
	if !hasFrag(&v.d, "(24") {
		t.Errorf("no (24 KB) size after the title: %q", fragStrings(&v.d))
	}
	// a narrow pane scales the image down
	v2 := layoutAt(st, d, Options{}, 300)
	if r := imgsWithURL(&v2.d, "https://img.test/wide.png")[0].r; r.Size() != image.Pt(300, 150) {
		t.Errorf("narrow wide image %v, want 300x150", r.Size())
	}
	// the toggle hides the image
	var tg int16 = -1
	for i, x := range v.d.targets {
		if x.kind == tCollapse && x.img == d.Blocks[0].Image {
			tg = int16(i)
		}
	}
	if tg < 0 || len(boxesOf(&v.d, bCaret)) != 2 {
		t.Fatalf("toggle target %d, carets %d", tg, len(boxesOf(&v.d, bCaret)))
	}
	h0 := v.d.size.Y
	v.activate(tg, image.Point{}, nil)
	v.relayout(st, 1000, 1, 1)
	if len(imgsWithURL(&v.d, "https://img.test/wide.png")) != 0 || len(boxesOf(&v.d, bCaretRight)) != 1 {
		t.Errorf("collapsed image still drawn")
	}
	if v.d.size.Y != h0-305 {
		t.Errorf("collapsed height %d, want %d", v.d.size.Y, h0-305)
	}
}

func TestKitTable(t *testing.T) {
	st := newTestStyle()
	rows := `[[{"type":"raw_text","text":"Head"},{"type":"raw_text","text":"Col"}]`
	for i := 0; i < 11; i++ {
		rows += `,[{"type":"raw_text","text":"r` + string(rune('a'+i)) + `"},{"type":"raw_text","text":"v"}]`
	}
	rows += "]"
	v := layoutAt(st, kitDoc(t, `[{"type":"table","rows":`+rows+`}]`), Options{}, 600)
	head, ra, rb := findFrag(t, &v.d, "Head"), findFrag(t, &v.d, "ra"), findFrag(t, &v.d, "rb")
	if ra.top-head.top != 28 || rb.top-ra.top != 28 {
		t.Errorf("row pitch %d %d, want 28", ra.top-head.top, rb.top-ra.top)
	}
	if head.top != 4+1+2 {
		t.Errorf("first cell text top %d, want 7 (4px table margin, border, 2px padding)", head.top)
	}
	if head.weight != font.Bold {
		t.Errorf("header row weight %d", head.weight)
	}
	// 12 rows: the first 10 show, under a fade
	if hasFrag(&v.d, "rj") || !hasFrag(&v.d, "ri") {
		t.Errorf("preview rows: %q", fragStrings(&v.d))
	}
	fades := 0
	for _, b := range v.d.boxes {
		if b.layer == 1 && b.fill.R == st.Palette.Panel.R && b.fill.A < 255 {
			fades++
		}
	}
	if fades == 0 {
		t.Error("no fade over the previewed table")
	}
	var frame *box
	for i := range v.d.boxes {
		if v.d.boxes[i].kind == bBorder {
			frame = &v.d.boxes[i]
			break
		}
	}
	if frame == nil || frame.radius != 4 || frame.r.Min.Y != 4 || frame.r.Dy() != 1+10*28 {
		t.Errorf("frame %+v, want radius 4, 10 rows of 28 + 1px", frame)
	}
}

func TestKitUnsupported(t *testing.T) {
	st := newTestStyle()
	d := kitDoc(t, `[{"type":"section","text":{"type":"mrkdwn","text":"before"}},{"type":"section","fields":"oops"},{"type":"image","alt_text":"no url"}]`)
	if len(d.Blocks) != 3 || d.Blocks[1].Kind != rt.Unsupported || d.Blocks[2].Kind != rt.Unsupported {
		t.Fatalf("blocks %+v", d.Blocks)
	}
	v := layoutAt(st, d, Options{}, 500)
	f := findFrag(t, &v.d, "This")
	if f.size != 13 || f.col == st.Palette.Text {
		t.Errorf("error text %dpx %v", f.size, f.col)
	}
	if b := findFrag(t, &v.d, "before"); f.top != b.top+22+4 {
		t.Errorf("error top %d, want %d", f.top, b.top+26)
	}
}

func TestBorderedQuoteAndPre(t *testing.T) {
	st := newTestStyle()
	d := kitDoc(t, `[{"type":"rich_text","elements":[
		{"type":"rich_text_quote","border":1,"elements":[{"type":"text","text":"quoted"}]},
		{"type":"rich_text_preformatted","border":1,"elements":[{"type":"text","text":"code"}]},
		{"type":"rich_text_list","style":"bullet","border":1,"elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"item"}]}]}]}]`)
	v := layoutAt(st, d, Options{}, 500)
	if q := findFrag(t, &v.d, "quoted"); q.x != 31 {
		t.Errorf("bordered quote text x %.0f, want 31", q.x)
	}
	var pre *box
	for i := range v.d.boxes {
		if v.d.boxes[i].kind == bBorder && v.d.boxes[i].fill == st.Palette.PreBg {
			pre = &v.d.boxes[i]
		}
	}
	if pre == nil || pre.r.Min.X != 13 {
		t.Errorf("bordered pre box %+v, want x 13", pre)
	}
	if c := findFrag(t, &v.d, "code"); c.x != 26 {
		t.Errorf("bordered pre text x %.0f, want 26", c.x)
	}
	if it := findFrag(t, &v.d, "item"); it.x != 28+11 {
		t.Errorf("bordered list text x %.0f, want 39", it.x)
	}
}

// TestKitSectionShowMore: in a thread (Options.Truncate) Slack's
// WithTextTruncation cuts section text at 300 characters or 5 line breaks,
// ends it with "…" and an inline " Show more"; expanded (kept for the
// session per message and block_id) it reads "Show less" on its own line.
// Channel messages and "expand": true sections never truncate.
func TestKitSectionShowMore(t *testing.T) {
	st := newTestStyle()
	long := strings.Repeat("word ", 80) // 400 characters
	blocks := `[{"type":"section","block_id":"long","text":{"type":"mrkdwn","text":"` + long + `"},
		"fields":[{"type":"mrkdwn","text":"` + long + `"}]}]`
	d := kitDoc(t, blocks)
	o := Options{Truncate: true, Channel: "C1", TS: "9.0"}
	v := layoutAt(st, d, o, 600)
	plain := string(v.d.plain)
	if strings.Count(plain, "Show more") != 2 || !strings.Contains(plain, "…") {
		t.Fatalf("truncated plain %q", plain)
	}
	if n := strings.Count(plain[:strings.Index(plain, "…")], "word"); n != 60 {
		t.Errorf("text cut after %d words, want 60 (300 characters)", n)
	}
	var tg int16 = -1
	for i, x := range v.d.targets {
		if x.kind == tExpand && x.id != "" {
			tg = int16(i)
		}
	}
	if tg < 0 {
		t.Fatal("no Show more target")
	}
	h0 := v.d.size.Y
	key := v.d.targets[tg].id
	if key != "blockText/C1/9.0/long" {
		t.Errorf("expand key %q", key)
	}
	v.activate(tg, image.Point{}, nil)
	t.Cleanup(func() { setKitExpanded(key, false) })
	// a new View (the row re-created) keeps the expansion
	v = layoutAt(st, d, o, 600)
	plain = string(v.d.plain)
	if strings.Contains(plain, "Show more") || strings.Count(plain, "Show less") != 2 || v.d.size.Y <= h0 {
		t.Errorf("expanded plain %q height %d (was %d)", plain, v.d.size.Y, h0)
	}
	if less := findFrag(t, &v.d, "Show"); less.x != 0 {
		t.Errorf("Show less x %.0f, want its own line", less.x)
	}
	// 5 line breaks
	v = layoutAt(st, kitDoc(t, `[{"type":"section","block_id":"lines","text":{"type":"mrkdwn","text":"a\nb\nc\nd\ne\nf\ng"}}]`), o, 600)
	if p := string(v.d.plain); strings.Contains(p, "f") || !strings.Contains(p, "e Show more") {
		t.Errorf("newline cut %q", p)
	}
	// a channel, and expand: true
	for _, c := range []struct {
		blocks string
		o      Options
	}{{blocks, Options{Channel: "C1", TS: "9.0"}},
		{`[{"type":"section","expand":true,"block_id":"x","text":{"type":"mrkdwn","text":"` + long + `"}}]`, o}} {
		v = layoutAt(st, kitDoc(t, c.blocks), c.o, 600)
		if p := string(v.d.plain); strings.Contains(p, "Show") || strings.Contains(p, "…") {
			t.Errorf("truncated where Slack doesn't: %q", p)
		}
	}
}
