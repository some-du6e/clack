package richview

import (
	"image"
	"testing"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// Karim's screenshot of Slack: a message with text and an image has
// "image.png ▾" between them (FilesMetaTextWrapper, which MessageFiles
// draws in channels and threads): 4px under the text a 22px line of 13px
// text in foreground_max .7 and a caret in the text color, then 4px, then
// the image. The caret hides the image (caret-right-filled) and shows it
// again; several files read "N files", one PDF its pretty_type.
func TestFileMetaLine(t *testing.T) {
	st := newTestStyle()
	img := func() *rt.File {
		return &rt.File{ID: "F1", Name: "image.png", IsImage: true, Thumb: &rt.Image{URL: "https://files.test/i.png", W: 800, H: 400}}
	}
	text := rt.Block{Kind: rt.Paragraph, Spans: []rt.Span{{Kind: rt.Text, Text: "3. im pretty sure the colors are off"}}}
	o := Options{Channel: "C1", TS: "1794262915.960579"}
	v := layoutAt(st, doc(text, fileBlock(img())), o, 600)
	title := findFrag(t, &v.d, "image.png")
	findFrag(t, &v.d, "3.")
	if title.size != 13<<6 && title.col != st.Palette.Text2 {
		t.Errorf("title frag %+v, want 13px Text2", title)
	}
	// text line 22, 4px gap, meta line 22 + 4px, then the image
	lineBottom := 22 // the one text line, from 0
	if len(v.d.imgs) != 1 {
		t.Fatalf("%d images", len(v.d.imgs))
	}
	if top := v.d.imgs[0].r.Min.Y; top != lineBottom+4+22+4 {
		t.Errorf("image top %d, want %d (text, 4, meta line 22, 4)", top, lineBottom+4+22+4)
	}
	carets := boxesOf(&v.d, bCaret)
	if len(carets) != 1 || carets[0].fill != st.Palette.Text || carets[0].r.Min.Y < lineBottom+4 || carets[0].r.Max.Y > lineBottom+4+22 {
		t.Fatalf("caret %+v", carets)
	}
	tg := v.targetAt(rectCenter(carets[0].r))
	if tg < 0 || v.d.targets[tg].kind != tExpand {
		t.Fatalf("the caret isn't a toggle (target %d)", tg)
	}
	// collapse: only the line is left, caret-right-filled
	v.activate(tg, image.Point{}, nil)
	v.relayout(st, 600, 1, 1)
	if len(v.d.imgs) != 0 || len(boxesOf(&v.d, bCaretRight)) != 1 {
		t.Errorf("collapsed: %d images, right carets %d", len(v.d.imgs), len(boxesOf(&v.d, bCaretRight)))
	}
	// the same message elsewhere in the session stays collapsed
	if w := layoutAt(st, doc(text, fileBlock(img())), o, 600); len(w.d.imgs) != 0 {
		t.Error("the collapsed state didn't stick for the session")
	}
	setKitExpanded(fileKeyFor(o, "F1"), false)

	// image only: the line comes first
	v = layoutAt(st, doc(fileBlock(img())), Options{Channel: "C1", TS: "2.0"}, 600)
	if top := v.d.imgs[0].r.Min.Y; top != 22+4 {
		t.Errorf("image-only message: image at %d, want 26", top)
	}
	// several files, a PDF
	two := doc(fileBlock(img()), fileBlock(&rt.File{ID: "F2", Name: "b.png", IsImage: true, Thumb: &rt.Image{URL: "https://files.test/b.png", W: 400, H: 400}}))
	if pl := string(layoutAt(st, two, Options{TS: "3.0"}, 600).d.plain); pl[:7] != "2 files" {
		t.Errorf("two files: plain %q", pl)
	}
	pdf := doc(fileBlock(&rt.File{ID: "F3", Name: "report.pdf", PrettyType: "PDF"}))
	findFrag(t, &layoutAt(st, pdf, Options{TS: "4.0"}, 600).d, "PDF")

	// expand_internal_inline_imgs off: images start collapsed
	t.Cleanup(uipref.Reset)
	uipref.Set(store.Prefs{NoInternalImgs: true})
	if v := layoutAt(st, doc(fileBlock(img())), Options{TS: "5.0"}, 600); len(v.d.imgs) != 0 || len(boxesOf(&v.d, bCaretRight)) != 1 {
		t.Errorf("pref off: %d images", len(v.d.imgs))
	}
	uipref.Reset()

	// the placeholder's "Uploading…" takes the same 22px line: no jump
	// when the title replaces it
	pend := img()
	pend.Pending = true
	status := rt.Block{Kind: rt.FileStatus, Spans: []rt.Span{{Kind: rt.Text, Text: "Uploading…"}}}
	p := layoutAt(st, doc(status, fileBlock(pend)), Options{TS: "6.0"}, 600)
	f := layoutAt(st, doc(fileBlock(img())), Options{TS: "6.1"}, 600)
	if p.d.imgs[0].r != f.d.imgs[0].r || len(boxesOf(&p.d, bCaret)) != 0 {
		t.Errorf("placeholder image %v, final %v (want the same box); placeholder carets %d", p.d.imgs[0].r, f.d.imgs[0].r, len(boxesOf(&p.d, bCaret)))
	}
	if p.d.imgs[0].alpha != 0.5 || len(p.d.spinners) != 1 || p.d.spinners[0].r.Dx() != 52 {
		t.Errorf("pending tile alpha %v, spinners %+v (want .5 and a 52px spinner)", p.d.imgs[0].alpha, p.d.spinners)
	}
}

func fileKeyFor(o Options, ids string) string { return "files/" + o.Channel + "/" + o.TS + "/" + ids }
