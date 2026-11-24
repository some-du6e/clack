package blockkit

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"slices"
	"strings"
	"testing"
	"time"

	"gioui.org/io/event"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Synthetic fixtures only.

// fiEnv is a modal whose one input block ("files") holds a file_input
// ("f") with el's fields, drawn.
type fiEnv struct {
	*bkEnv
	s *Stack
}

func newFIEnv(t *testing.T, el map[string]any, prefs map[string]json.RawMessage) *fiEnv {
	t.Helper()
	// Slack's processing poll starts at 7.5 s; the order is what counts
	pf, pm := core.UploadPollFirst, core.UploadPollMax
	core.UploadPollFirst, core.UploadPollMax = 10*time.Millisecond, 40*time.Millisecond
	t.Cleanup(func() { core.UploadPollFirst, core.UploadPollMax = pf, pm })
	e := newBKEnv(t, false, false)
	if prefs != nil {
		e.h.a.SetTeamPrefs(prefs)
	}
	pt := func(s string) map[string]any { return map[string]any{"type": "plain_text", "text": s} }
	el["type"], el["action_id"] = "file_input", "f"
	v, err := core.ParseView(mustJSON(map[string]any{"id": "VSURF0FI01", "type": "modal", "callback_id": "fi", "hash": "f1",
		"title": pt("Report"), "submit": pt("Send"), "close": pt("Cancel"), "state": map[string]any{"values": map[string]any{}},
		"blocks": []any{map[string]any{"type": "input", "block_id": "files", "label": pt("Attachments"), "element": el}}}))
	if err != nil {
		t.Fatal(err)
	}
	s := newStack(e.h, v)
	e.h.ShowOverlay(s)
	e.frame()
	e.frame()
	return &fiEnv{e, s}
}

// w is the file_input's widget.
func (e *fiEnv) w() *ew {
	e.t.Helper()
	sf := e.s.top()
	for _, b := range allBlocks(sf.blocks) {
		if b.BlockID == "files" && b.Element != nil {
			if w := sf.ws[surfKey(b, b.Element)]; w != nil {
				return w
			}
		}
	}
	e.t.Fatal("no file_input widget")
	return nil
}

// win is r, element-local, in the window.
func (e *fiEnv) win(r image.Rectangle) image.Rectangle {
	return r.Add(e.s.top().org.Add(e.w().pos))
}

// files is the value's files.
func (e *fiEnv) files() []map[string]any {
	e.t.Helper()
	v := e.s.top().vals.get("files", "f")
	if v == nil {
		return nil
	}
	var out []map[string]any
	for _, raw := range v.Files {
		var o map[string]any
		if err := json.Unmarshal(raw, &o); err != nil {
			e.t.Fatalf("value file %s: %v", raw, err)
		}
		out = append(out, o)
	}
	return out
}

// fiPNG is a synthetic w×h PNG.
func fiPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(40 * x), G: uint8(40 * y), B: 0x80, A: 0xff})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// Slack's FileInputElement (n9p0; build 9f450b8ad3db): the outline Upload
// File button opens the stacked Add file dialog, whose checks keep the
// dialog open with Slack's errors; files that pass close it and upload
// without being shared, each a pending card (the submit waiting) until
// Slack has processed it; the value is each uploaded file's id; removing
// an uploaded card deletes the file.
func TestFileInputElement(t *testing.T) {
	e := newFIEnv(t, map[string]any{"filetypes": []string{"pdf", "png"}, "max_files": 3}, nil)
	w := e.w()
	gtx := e.context()
	if w.fi == nil || w.fi.count() != 0 || len(w.fi.errs) != 0 {
		t.Fatalf("a fresh file_input has files or errors: %+v", w.fi)
	}
	// no files: the button alone, outline medium (36px, its 16px icon 4px
	// before the label, padding 12)
	if h := w.fileHeight(gtx, 464); h != 36 {
		t.Errorf("an empty file_input is %dpx tall, want the button's 36", h)
	}
	bw := measure(gtx, e.h.th, 15, theme.Bold, "Upload File") + 16 + 4 + 24
	if sz := w.fi.upBox.Size(); sz != image.Pt(max(80, bw), 36) {
		t.Errorf("Upload File is %v, want %v", sz, image.Pt(max(80, bw), 36))
	}

	var dlg *fiDialog
	old := fiDialogShown
	fiDialogShown = func(d *fiDialog) { dlg = d }
	t.Cleanup(func() { fiDialogShown = old })
	e.click(center(e.win(w.fi.upBox)))
	if dlg == nil || w.fi.dlg != dlg {
		t.Fatal("a click on Upload File opened no Add file dialog")
	}
	if m, ok := topOverlay[*popups.Modal](e.bkEnv); !ok || m != dlg.m {
		t.Fatal("the Add file dialog isn't stacked over the modal")
	}
	if a := fiAccept(w.e.FileTypes, w.uploadPref()); a != ".pdf,.png" {
		t.Errorf("the chooser's accept is %q, want .pdf,.png", a)
	}

	// FileInputDialog's checks: a type the element doesn't take, too many
	for _, c := range []struct {
		files []core.UploadFile
		want  string
	}{
		{[]core.UploadFile{{Name: "notes.txt", Data: []byte("x")}}, "The file type you attempted to upload is not supported"},
		{[]core.UploadFile{{Name: "a.pdf", Data: []byte("x")}, {Name: "b.pdf", Data: []byte("x")}, {Name: "c.pdf", Data: []byte("x")}, {Name: "d.pdf", Data: []byte("x")}},
			"You have selected too many files. The limit is 3."},
	} {
		dlg.chosen(c.files)
		if dlg.err != c.want || dlg.m.Dismissed() || w.fi.count() != 0 {
			t.Errorf("chose %d file(s): dialog error %q (closed %v, %d files), want %q with the dialog open", len(c.files), dlg.err, dlg.m.Dismissed(), w.fi.count(), c.want)
		}
		e.frame()
		if dlg.promptText() != c.want {
			t.Errorf("the drop target says %q, want the error", dlg.promptText())
		}
		dlg.err = "" // Try Another File
	}

	// an empty file is dropped, an upper-case extension passes
	dlg.chosen([]core.UploadFile{{Name: "empty.pdf"}, {Name: "Report.PDF", Data: []byte("%PDF-1.4 synthetic")}, {Name: "chart.png", Data: fiPNG(t, 8, 6)}})
	if !dlg.m.Dismissed() {
		t.Fatalf("the dialog stayed open (error %q)", dlg.err)
	}
	fs := w.fi
	if len(fs.pending) != 2 || fs.pending[0].name != "Report.PDF" || fs.pending[1].name != "chart.png" {
		t.Fatalf("pending uploads %v, want Report.PDF and chart.png", fs.pending)
	}
	if st := fs.pending[0].state(); st != "processing" || fs.pending[0].image() {
		t.Errorf("the pending PDF is %s (image %v), want a processing compact card", st, fs.pending[0].image())
	}
	if !fs.pending[1].image() {
		t.Error("the pending PNG isn't an image card")
	}
	if !e.s.top().uploading() || slices.Contains(e.s.stops(), event.Tag(&e.s.submit)) {
		t.Error("the modal's submit is on while files upload")
	}
	e.frame()
	if h := w.fileHeight(gtx, 464); h != fs.galleryHeight(gtx)+16+36 {
		t.Errorf("with files the element is %dpx, want the gallery, 16 and the button", h)
	}

	e.until("both uploads", 20*time.Second, func() bool { return len(fs.done) == 2 })
	if e.s.top().uploading() || !slices.Contains(e.s.stops(), event.Tag(&e.s.submit)) {
		t.Error("the submit is still held after the uploads")
	}
	byName := map[string]*fiFile{}
	for _, f := range fs.done {
		byName[f.name] = f
	}
	pdf, img := byName["Report.PDF"], byName["chart.png"]
	if pdf == nil || img == nil {
		t.Fatalf("uploaded %v", fs.done)
	}
	// the value: each file's id and nothing else
	got := map[string]bool{}
	for _, o := range e.files() {
		if len(o) != 1 {
			t.Errorf("a value file %v carries more than its id", o)
		}
		id, _ := o["id"].(string)
		got[id] = true
	}
	if len(got) != 2 || !got[pdf.id] || !got[img.id] {
		t.Errorf("value files %v, want %s and %s", got, pdf.id, img.id)
	}
	if n, c := e.fs.CallCount("files.getUploadURL"), e.fs.CallCount("files.completeUpload"); n != 2 || c != 2 {
		t.Errorf("%d files.getUploadURL and %d files.completeUpload, want 2 each", n, c)
	}
	if n := e.fs.CallCount("files.share"); n != 0 {
		t.Errorf("a file_input upload was shared (%d files.share)", n)
	}
	// the cards come processed: the PDF's pretty_type and pdf tile, the
	// image's thumb
	if pdf.state() != "enabled" || pdf.pretty != "PDF" || pdf.ftype != "pdf" {
		t.Errorf("the PDF card is %s, %q / %q, want enabled with PDF and pdf", pdf.state(), pdf.pretty, pdf.ftype)
	}
	if ic, pal := icons.FileEntityIcon(pdf.entityType()); ic != "pdf-file-filled" || pal != "flamingo" {
		t.Errorf("the PDF's tile is %s on %s, want pdf-file-filled on flamingo", ic, pal)
	}
	if !img.image() || img.thumb == "" {
		t.Errorf("the image card has mime %q and thumb %q", img.mime, img.thumb)
	}

	// hovering the PDF's card shows its remove button 8px out from its top
	// right corner; a click deletes the file and drops it from the value
	e.frame()
	e.hover(center(e.win(pdf.box)))
	e.frame()
	rm := image.Rect(pdf.box.Max.X-12, pdf.box.Min.Y-8, pdf.box.Max.X+8, pdf.box.Min.Y+12)
	e.click(center(e.win(rm)))
	e.until("files.delete", 10*time.Second, func() bool { return e.fs.CallCount("files.delete") == 1 })
	if len(fs.done) != 1 || fs.done[0] != img {
		t.Errorf("after the remove: %v, want the image alone", fs.done)
	}
	if vs := e.files(); len(vs) != 1 || vs[0]["id"] != img.id {
		t.Errorf("after the remove the value is %v, want %s alone", vs, img.id)
	}
}

// An upload that fails is dropped with Yt0k's error first in the errors,
// a boxed error Alert above the button.
func TestFileInputUploadFails(t *testing.T) {
	e := newFIEnv(t, map[string]any{}, nil)
	e.fs.SetFault("files.getUploadURL", fakeslack.Fault{Error: "not_allowed"})
	w := e.w()
	w.uploadFiles([]core.UploadFile{{Name: "plan.txt", Data: []byte("synthetic")}})
	if len(w.fi.pending) != 1 {
		t.Fatalf("pending %v", w.fi.pending)
	}
	e.until("the upload's failure", 10*time.Second, func() bool { return len(w.fi.pending) == 0 })
	want := "There was a problem uploading the file plan.txt. Please try again in a few minutes."
	if len(w.fi.errs) != 1 || w.fi.errs[0].msg != want || len(w.fi.done) != 0 {
		t.Fatalf("errors %v, files %v; want %q and no file", w.fi.errs, w.fi.done, want)
	}
	gtx := e.context()
	// at 900px the message is one line
	if h, a := w.fileHeight(gtx, 900), fiAlertHeight(gtx, e.h.th, 900, want); h != a+16+36 || a != 2+24+22 {
		t.Errorf("the element is %dpx with a %dpx alert, want the alert (one 22px line in its padding and border), 16 and the button", h, a)
	}
	// a new upload clears the errors (another file: the client fails an
	// identical failed request fast for a second, as its guard does)
	e.fs.SetFault("files.getUploadURL", fakeslack.Fault{})
	w.uploadFiles([]core.UploadFile{{Name: "notes.txt", Data: []byte("synthetic notes")}})
	if len(w.fi.errs) != 0 {
		t.Errorf("uploadFiles kept the errors %v", w.fi.errs)
	}
	e.until("the upload", 20*time.Second, func() bool { return len(w.fi.done) == 1 })
}

// The workspace's disable_file_uploads: disable_all disables the button
// (no dialog) and starts the errors with Slack's; disable_all_except_images
// takes images only.
func TestFileInputUploadsOff(t *testing.T) {
	e := newFIEnv(t, map[string]any{}, map[string]json.RawMessage{"disable_file_uploads": json.RawMessage(`"disable_all"`)})
	w := e.w()
	want := "File uploads to this workspace have been disabled at the request of your administrator."
	if len(w.fi.errs) != 1 || w.fi.errs[0].msg != want {
		t.Fatalf("errors %v, want %q", w.fi.errs, want)
	}
	var opened bool
	old := fiDialogShown
	fiDialogShown = func(*fiDialog) { opened = true }
	t.Cleanup(func() { fiDialogShown = old })
	e.click(center(e.win(w.fi.upBox)))
	if opened || w.fi.dlg != nil {
		t.Error("the disabled Upload File opened the dialog")
	}
	if keep, msg := w.fileChecks([]core.UploadFile{{Name: "a.png", Data: []byte("x")}}); keep != nil || msg != "" {
		t.Errorf("checks under disable_all kept %v / %q, want nothing", keep, msg)
	}

	e2 := newFIEnv(t, map[string]any{}, map[string]json.RawMessage{"disable_file_uploads": json.RawMessage(`"disable_all_except_images"`)})
	w2 := e2.w()
	if len(w2.fi.errs) != 0 || fiAccept(nil, w2.uploadPref()) != "image/*" {
		t.Errorf("images only: errors %v, accept %q", w2.fi.errs, fiAccept(nil, w2.uploadPref()))
	}
	keep, msg := w2.fileChecks([]core.UploadFile{{Name: "a.pdf", Data: []byte("x")}, {Name: "b.png", Data: []byte("x")}})
	if msg != "" || len(keep) != 1 || keep[0].Name != "b.png" {
		t.Errorf("images only kept %v (%q), want b.png", keep, msg)
	}
}

// mgGk's messages and R9ib's sizes; iSZO's accept.
func TestFileInputChecks(t *testing.T) {
	f := func(names ...string) []core.UploadFile {
		var out []core.UploadFile
		for _, n := range names {
			out = append(out, core.UploadFile{Name: n, Data: []byte("1234")})
		}
		return out
	}
	// mgGk counts before it weighs: too many files is the error even when
	// they're too large too
	for _, c := range []struct {
		files              []core.UploadFile
		existing, maxFiles int
		maxBytes           int64
		wantID, want       string
	}{
		{f("a.pdf"), 2, 2, 100, "too-many-files", "You have selected too many files. The limit is 2."},
		{f("a.pdf", "b.pdf", "c.pdf"), 0, 2, 3, "too-many-files", "You have selected too many files. The limit is 2."},
		{f("a.pdf"), 0, 2, 3, "files-too-large", "The file a.pdf is too large. Our max file size is 3 B."},
		{f("a.pdf", "b.pdf"), 0, 2, 3, "files-too-large", "The files a.pdf and b.pdf are too large. Our max file size is 3 B."},
		{f("a.pdf", "b.pdf", "c.pdf"), 0, 3, 3, "files-too-large", "The files a.pdf, b.pdf and c.pdf are too large. Our max file size is 3 B."},
		{f("a.pdf"), 1, 2, 100, "", ""},
	} {
		e := fiLimits(c.files, c.existing, c.maxFiles, c.maxBytes)
		switch {
		case c.wantID == "" && e != nil:
			t.Errorf("%d files: %+v, want none", len(c.files), e)
		case c.wantID != "" && (e == nil || e.id != c.wantID || e.msg != c.want):
			t.Errorf("%d files (max %d B): %+v, want %s %q", len(c.files), c.maxBytes, e, c.wantID, c.want)
		}
	}
	for b, want := range map[int64]string{0: "0 bytes", 999: "999 B", 1500: "2 kB", 100_000_000: "100 MB", 2_500_000_000: "3 GB"} {
		if got := fiFileSize(b); got != want {
			t.Errorf("fiFileSize(%d) = %q, want %q", b, got, want)
		}
	}
	for _, c := range []struct {
		types []string
		pref  string
		want  string
	}{{[]string{"pdf", "png"}, "", ".pdf,.png"}, {nil, "", ""}, {[]string{"pdf"}, "disable_all_except_images", "image/*"}, {[]string{"pdf"}, "disable_all", ""}} {
		if got := fiAccept(c.types, c.pref); got != c.want {
			t.Errorf("fiAccept(%v, %q) = %q, want %q", c.types, c.pref, got, c.want)
		}
	}
}

// The gallery as client-boot-styles lays it out: .galleryContainer's
// border and padding around the row, its cards shrinking with the row
// (flex-shrink 1, by their widths) to their min-content, then the
// scroller scrolling the rest (250px a click, to an end under 50px away).
func TestFileInputGallery(t *testing.T) {
	e := newBKEnv(t, false, false)
	gtx := e.context()
	th := e.h.th
	compact := func(n int) *fiState {
		fs := &fiState{}
		for i := range n {
			fs.done = append(fs.done, &fiFile{uploaded: true, name: "Quarterly report " + string(rune('A'+i)) + ".pdf", mime: "application/pdf", pretty: "PDF", ftype: "pdf"})
		}
		return fs
	}
	images := func(n int) *fiState {
		fs := &fiState{}
		for range n {
			fs.done = append(fs.done, &fiFile{uploaded: true, name: "p.png", mime: "image/png"})
		}
		return fs
	}
	const view = 430 // a 520px modal's body (464) less the gallery's border and padding
	if h := compact(1).galleryHeight(gtx); h != 1+16+62+2+16+1 {
		t.Errorf("a compact row's gallery is %dpx, want 98", h)
	}
	if h := images(2).galleryHeight(gtx); h != 1+16+60+2+16+1 {
		t.Errorf("an image row's gallery is %dpx, want 96", h)
	}
	r := compact(1).layRow(gtx, th, view)
	if r.x[0] != 1 || r.w[0] != 220 {
		t.Errorf("one compact card at %d, %dpx; want 1 and 220", r.x[0], r.w[0])
	}
	// two don't fit 430: each shrinks to (430-2-16)/2
	r = compact(2).layRow(gtx, th, view)
	if r.w[0] != 206 || r.w[1] != 206 || r.x[1] != 1+206+16 {
		t.Errorf("two compact cards %v at %v, want 206 each at 1 and 223", r.w, r.x)
	}
	// six stop at their min-content: border, padding, tile and a character
	fs := compact(6)
	r = fs.layRow(gtx, th, view)
	minW := 2 + 36 + 36 + fiWidestChar(gtx, th, fs.done[0])
	for i, w := range r.w {
		if w != minW && w != minW+1 && w != minW-1 {
			t.Errorf("card %d is %dpx, want its min-content %d", i, w, minW)
		}
	}
	// seven images shrink alike (60 each, no min-content)
	r = images(7).layRow(gtx, th, view)
	if want := (view - 2 - 6*16) / 7; r.w[0] < want-1 || r.w[0] > want+1 {
		t.Errorf("seven images are %dpx each, want about %d", r.w[0], want)
	}
	// the scroller: a page is 250px; an end under 50px away is reached
	g := &fiGallery{contentW: 1000, viewW: view}
	g.scrollBy(1, time.Time{})
	g.tick(time.Time{})
	if g.scroll != 250 {
		t.Errorf("a right arrow scrolled to %v, want 250", g.scroll)
	}
	g.scrollBy(1, time.Time{})
	g.tick(time.Time{})
	if g.scroll != 500 {
		t.Errorf("a second one scrolled to %v, want 500", g.scroll)
	}
	g.scrollBy(1, time.Time{})
	g.tick(time.Time{})
	if g.scroll != 570 { // 750 clamps to the end, 570
		t.Errorf("a third one scrolled to %v, want the end (570)", g.scroll)
	}
	g.scroll = 280
	g.scrollBy(-1, time.Time{})
	g.tick(time.Time{})
	if g.scroll != 0 { // 30 is under 50 from the start
		t.Errorf("left from 280 scrolled to %v, want the start", g.scroll)
	}
	if !strings.HasPrefix(fiAccept([]string{"pdf"}, ""), ".") {
		t.Error("accept lost its dot")
	}
}
