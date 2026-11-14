package richview

// FileActions on message files (file_actions.go). Synthetic documents
// only: which buttons each kind of file gets, in Slack's order; their
// geometry; that a click on one reaches the handler and not the card.

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

type actHandler struct {
	recHandler
	acts  []FileAction
	files []*rt.File
	btns  []image.Rectangle
}

func (h *actHandler) FileAction(a FileAction, f *rt.File, btn image.Rectangle) {
	h.acts = append(h.acts, a)
	h.files = append(h.files, f)
	h.btns = append(h.btns, btn)
}

func actNames(bs []barBtn) string {
	names := map[FileAction]string{FileCollapse: "Collapse", FileDownload: "Download", FileExpand: "Expand", FileEdit: "Edit",
		FileEditWindow: "EditWindow", FileExternal: "External", FileShare: "Share", FileStar: "Star", FileSection: "Section",
		FileDetails: "Details", FileMore: "More"}
	var out []string
	for _, b := range bs {
		out = append(out, names[b.act])
	}
	return strings.Join(out, " ")
}

// TestFileActionsPerFile: renderX's rules for each kind of message file.
func TestFileActionsPerFile(t *testing.T) {
	defer func(f func() string) { SelfID = f }(SelfID)
	SelfID = func() string { return "U0SELF" }
	snippet := func(user string) *rt.File {
		return &rt.File{ID: "F1", Name: "fib.py", Type: "python", Mode: "snippet", User: user, IsSnippet: true, Preview: "x", Permalink: "https://x.test/F1"}
	}
	own := snippet("U0SELF")
	expanded := snippet("U0SELF")
	expanded.Expanded = true
	thumb := &rt.Image{URL: "https://img.test/a", W: 480, H: 270}
	for _, c := range []struct {
		name    string
		f       *rt.File
		gallery bool
		noAnim  bool
		want    string
	}{
		{"someone's snippet", snippet("U0OTHER"), false, false, "Download External Share Details More"},
		{"own snippet", own, false, false, "Download Edit Share Details More"},
		{"own snippet, expanded", expanded, false, false, "Collapse Download Edit Share Details More"},
		{"own post (space)", &rt.File{ID: "F2", Type: "space", Mode: "space", User: "U0SELF"}, false, false, "EditWindow Share Details More"},
		{"pdf", &rt.File{ID: "F3", Type: "pdf", Mode: "hosted"}, false, false, "Download Share Details More"},
		{"someone's CSV snippet", &rt.File{ID: "F4", Type: "csv", Mode: "snippet", IsSnippet: true, User: "U0OTHER"}, false, false, "Download Share Details More"},
		{"google drive doc", &rt.File{ID: "F5", Type: "gdoc", Mode: "external", IsExternal: true, ExternalType: "gdrive"}, false, false, "Share Details More"},
		{"dropbox file", &rt.File{ID: "F6", Type: "pdf", Mode: "external", IsExternal: true, ExternalType: "dropbox"}, false, false, "External Share Details More"},
		{"canvas", &rt.File{ID: "F7", Type: "quip", Mode: "quip"}, false, false, "Share Star Details More"},
		{"gallery image", &rt.File{ID: "F8", Type: "png", Mode: "hosted", IsImage: true, Thumb: thumb}, true, false, "Download More"},
		{"gallery gif, animations off", &rt.File{ID: "F9", Type: "gif", Mode: "hosted", IsImage: true, Thumb: thumb, Deanimate: true}, true, true, "Download Expand More"},
		{"gallery gif, animations on", &rt.File{ID: "F9", Type: "gif", Mode: "hosted", IsImage: true, Thumb: thumb, Deanimate: true}, true, false, "Download More"},
	} {
		if got := actNames(fileActions(c.f, c.gallery, c.noAnim)); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	// the labels
	tips := map[FileAction]string{}
	for _, b := range fileActions(snippet("U0OTHER"), false, false) {
		tips[b.act] = b.tip
	}
	if tips[FileDownload] != "Download" || tips[FileExternal] != "Open in new window" || tips[FileShare] != "Share file…" ||
		tips[FileDetails] != "View file details" || tips[FileMore] != "More actions" {
		t.Errorf("snippet tips %q", tips)
	}
	if bs := fileActions(&rt.File{ID: "F6", Mode: "external", IsExternal: true, ExternalType: "dropbox"}, false, false); bs[0].tip != "Open file on Dropbox" {
		t.Errorf("dropbox: %q", bs[0].tip)
	}
	cv := fileActions(&rt.File{ID: "F7", Type: "quip", Mode: "quip"}, false, false)
	if cv[0].tip != "Share canvas" || cv[1].tip != "Add to starred" || cv[2].tip != "View canvas details" {
		t.Errorf("canvas tips %q %q %q", cv[0].tip, cv[1].tip, cv[2].tip)
	}
	defer func(f func(string) bool) { FileStarred = f }(FileStarred)
	FileStarred = func(string) bool { return true }
	if cv = fileActions(&rt.File{ID: "F7", Type: "quip", Mode: "quip"}, false, false); cv[1].tip != "Unstar canvas" || cv[1].icon != "star-filled" || !cv[1].starred {
		t.Errorf("a starred canvas: %+v", cv[1])
	}
	// sizes: smedium 32px, the menu small (28 wide); in a gallery the
	// gallery_v3 buttons are 28px with 2px after Download
	pill := fileActions(&rt.File{ID: "F3", Mode: "hosted"}, false, false)
	if pill[0].w != 32 || pill[0].h != 32 || pill[len(pill)-1].w != 28 || pill[len(pill)-1].h != 32 || !pill[len(pill)-1].right {
		t.Errorf("pillow sizes %+v", pill)
	}
	gal := fileActions(&rt.File{ID: "F9", Type: "gif", Mode: "hosted", IsImage: true, Deanimate: true}, true, true)
	if !gal[0].gal || gal[0].w != 28 || gal[0].h != 28 || gal[0].mr != 2 || gal[1].gal || gal[1].w != 32 || !gal[2].gal || gal[2].h != 28 {
		t.Errorf("gallery sizes %+v", gal)
	}
}

// TestFileActionsBar: a PDF pillow's group at top 12, right 12 shows
// while the pillow is hovered; its buttons reach the handler, never the
// card (which opens the file); a voice clip, a video, an upload and a
// hover card's file have none.
func TestFileActionsBar(t *testing.T) {
	uipref.Reset()
	pdf := &rt.File{ID: "F3", Name: "q3.pdf", Type: "pdf", PrettyType: "PDF", Mode: "hosted", Permalink: "https://x.test/F3"}
	h := &actHandler{}
	hs := newHarness(doc(fileBlock(pdf)), h)
	hs.w = 600
	hs.v.Origin = image.Pt(100, 200)
	hs.frame()
	if len(hs.v.d.bars) != 1 {
		t.Fatalf("%d bars", len(hs.v.d.bars))
	}
	if len(hs.v.FileActionRects()) != 0 {
		t.Error("the group shows with the pointer away")
	}
	card := hs.v.d.bars[0].card
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(card.Min.X+40), float32(card.Min.Y+30))})
	rs := hs.v.FileActionRects()
	if len(rs) != 4 {
		t.Fatalf("%d buttons shown, want 4", len(rs))
	}
	group, _ := hs.v.barRects(0)
	if group.Max.X != card.Max.X-12 || group.Min.Y != card.Min.Y+12 || group.Dy() != 32+8 || group.Dx() != 3*32+28+8 {
		t.Errorf("group %v in card %v", group, card)
	}
	for k, r := range rs {
		if r.R.Min.Y != group.Min.Y+4 || k > 0 && r.R.Min.X != rs[k-1].R.Max.X {
			t.Errorf("button %d at %v", k, r.R)
		}
	}
	// Download, then More actions: the handler, with the button's window box
	for i, k := range []int{0, 3} {
		p := rectCenter(rs[k].R)
		hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p})
		at := time.Duration(i+1) * time.Second
		hs.send(press(p, at), release(p, at+20*time.Millisecond))
		if len(h.acts) != i+1 || h.acts[i] != rs[k].Action || h.files[i] != pdf || h.btns[i] != rs[k].R.Add(hs.v.Origin) {
			t.Fatalf("click %d: actions %v boxes %v, want %v at %v", i, h.acts, h.btns, rs[k].Action, rs[k].R.Add(hs.v.Origin))
		}
	}
	if len(h.recHandler.files) != 0 {
		t.Errorf("a button opened the file")
	}
	// between buttons in the group: nothing; on the card: the file
	gp := f32.Pt(float32(group.Min.X+2), float32(group.Min.Y+2))
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: gp})
	hs.send(press(gp, 5*time.Second), release(gp, 5*time.Second+20*time.Millisecond))
	cp := f32.Pt(float32(card.Min.X+40), float32(card.Min.Y+30))
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: cp})
	hs.send(press(cp, 6*time.Second), release(cp, 6*time.Second+20*time.Millisecond))
	if len(h.acts) != 2 || len(h.recHandler.files) != 1 {
		t.Errorf("after the group and the card: %v actions, %d files opened", h.acts, len(h.recHandler.files))
	}

	for _, c := range []struct {
		name string
		f    *rt.File
		o    Options
	}{
		{"voice clip", &rt.File{ID: "F4", Mode: "hosted", IsAudio: true, Subtype: "slack_audio"}, Options{}},
		{"video", &rt.File{ID: "F5", Mode: "hosted", IsVideo: true}, Options{}},
		{"upload", &rt.File{ID: "F6", Mode: "hosted", Type: "zip", Pending: true}, Options{}},
		{"hover card", pdf, Options{NoHoverCards: true}},
	} {
		if v := layoutDoc(t, doc(fileBlock(c.f)), 600, c.o); len(v.d.bars) != 0 {
			t.Errorf("%s: %d bars", c.name, len(v.d.bars))
		}
	}
	// an audio file has one
	if v := layoutDoc(t, doc(fileBlock(&rt.File{ID: "F7", Mode: "hosted", IsAudio: true, Name: "a.mp3"})), 600, Options{}); len(v.d.bars) != 1 {
		t.Errorf("audio: %d bars", len(v.d.bars))
	}
}

// TestGalleryExpandStillOpens: the GIF still's Expand (animations off) is
// in its gallery FileActions and still opens the viewer (Handler.OpenFile);
// Download and More actions go to the FileActionHandler.
func TestGalleryExpandStillOpens(t *testing.T) {
	uipref.Set(store.Prefs{NoAnimations: true})
	defer uipref.Reset()
	d, f := gifDoc()
	h := &actHandler{}
	hs := newHarness(d, h)
	hs.w = 700
	hs.st.Images = newGIFImages(gifThumb)
	hs.frame()
	tile := hs.v.d.bars[0].card
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: rectCenter(tile)})
	var exp, dl image.Rectangle
	for _, r := range hs.v.FileActionRects() {
		switch r.Action {
		case FileExpand:
			exp = r.R
		case FileDownload:
			dl = r.R
		}
	}
	if exp.Empty() || dl.Empty() {
		t.Fatalf("buttons %+v", hs.v.FileActionRects())
	}
	p := rectCenter(exp)
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p})
	hs.send(press(p, time.Second), release(p, time.Second+20*time.Millisecond))
	if len(h.recHandler.files) != 1 || h.recHandler.files[0] != f || len(h.acts) != 0 || hs.v.playing[gifThumb] {
		t.Fatalf("Expand: %d files opened, actions %v, playing %v", len(h.recHandler.files), h.acts, hs.v.playing[gifThumb])
	}
	p = rectCenter(dl)
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p})
	hs.send(press(p, 2*time.Second), release(p, 2*time.Second+20*time.Millisecond))
	if len(h.acts) != 1 || h.acts[0] != FileDownload || hs.v.playing[gifThumb] {
		t.Errorf("Download: actions %v, playing %v", h.acts, hs.v.playing[gifThumb])
	}
}
