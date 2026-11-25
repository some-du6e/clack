package composer

import (
	"image"
	"image/color"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Synthetic fixtures only.

// galleryDrawn lays a's gallery out once (900px wide, 1px a dp) and
// returns what it drew.
func galleryDrawn(t *testing.T, a *attachments) cardDrawn {
	t.Helper()
	var d cardDrawn
	prev := theme.DrawHook
	theme.DrawHook = func(s string) { d.texts = append(d.texts, s) }
	stop := recordDraws(func(r image.Rectangle, col color.NRGBA) { d.boxes = append(d.boxes, drawnBox{r, col}) },
		func(n string) { d.icons = append(d.icons, n) })
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Max: image.Pt(900, 600)}, Now: time.Now()}
	a.Layout(gtx)
	stop()
	theme.DrawHook = prev
	return d
}

// A file restored with a draft (drafts.list's file objects, an edited
// message's files) is the store's file in Slack (draftComposerFiles), so
// maybeRenderImageFile draws it by the file object (CDN build bv1-13,
// boot-shared tTyz): a .docx's card has Word's svgicon, not the generic
// page its long MIME subtype gives; a canvas's card has QuipIcon's tile; a
// playable video is DraftVideoFile, a 62px tile under the 20% black
// overlay and play-filled; a playable audio file is PendingFile's
// FileAudio, the 184px mini player in its pillow, no card.
func TestRestoredChipsDrawTheirFile(t *testing.T) {
	defer slack.SetPlatformMimeTypes(nil)()
	docx := "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	a := newAttachments(&slickHost{th: theme.New(false)})
	a.restoreFiles([]core.DraftFile{{ID: "F0FAKEDOCX1", Name: "plan.docx", Mimetype: docx, PrettyType: "Word Document", Size: 1200,
		File: &richtext.File{ID: "F0FAKEDOCX1", Name: "plan.docx", Mime: docx, Type: "docx", Mode: "hosted", PrettyType: "Word Document"}}})
	if d := galleryDrawn(t, a); !d.drewIcon("fileicon-file-word") || d.drewIcon("fileicon-generic") || !d.drew("plan.docx") {
		t.Errorf("a restored .docx drew icons %q and %q, want its card with Word's svgicon", d.icons, d.texts)
	}

	a = newAttachments(&slickHost{th: theme.New(false)})
	a.restoreFiles([]core.DraftFile{{ID: "F0FAKECANV1", Name: "Roadmap", Title: "Roadmap", PrettyType: "Canvas",
		File: &richtext.File{ID: "F0FAKECANV1", Name: "Roadmap", Type: "quip", Mode: "quip", PrettyType: "Canvas"}}})
	d := galleryDrawn(t, a)
	if d.tiles(attIconPx, attIconPx, rgb(0x1cb6eb)) != 1 || !d.drewIcon("canvas-content-filled") || d.drewIcon("fileicon-file-text-quip") {
		t.Errorf("a restored canvas drew icons %q, boxes %v: want QuipIcon's lagoon-40 tile and canvas-content-filled", d.icons, d.boxes)
	}

	a = newAttachments(&slickHost{th: theme.New(false)})
	a.restoreFiles([]core.DraftFile{{ID: "F0FAKEVID01", Name: "demo.mp4", Mimetype: "video/mp4", PrettyType: "MPEG 4 Video",
		File: &richtext.File{ID: "F0FAKEVID01", Name: "demo.mp4", Mime: "video/mp4", Type: "mp4", Mode: "hosted", IsVideo: true,
			Thumb: &richtext.Image{URL: "https://files.example.test/demo_thumb_video.jpg", W: 1280, H: 720}}}})
	d = galleryDrawn(t, a)
	if d.tiles(attChipH, attChipH, color.NRGBA{A: 0x33}) != 1 || !d.drewIcon(icons.Slack("play-filled")) || d.drew("demo.mp4") || d.drewIcon("fileicon-file-video") {
		t.Errorf("a restored video drew icons %q, texts %q, boxes %v: want DraftVideoFile's 62px tile, its overlay and play-filled, no card", d.icons, d.texts, d.boxes)
	}
	a.mu.Lock()
	poster := a.chips[0].thumbURL
	a.mu.Unlock()
	if poster != "https://files.example.test/demo_thumb_video.jpg" {
		t.Errorf("the restored video's thumb is %q, want its poster (thumb_video)", poster)
	}

	a = newAttachments(&slickHost{th: theme.New(false)})
	a.restoreFiles([]core.DraftFile{{ID: "F0FAKEAUD01", Name: "memo.mp3", Mimetype: "audio/mpeg", PrettyType: "MP3",
		File: &richtext.File{ID: "F0FAKEAUD01", Name: "memo.mp3", Mime: "audio/mpeg", Type: "mp3", Mode: "hosted", IsAudio: true,
			DurationMS: 65_000, Waveform: []int{10, 40, 80, 40, 10}}}})
	d = galleryDrawn(t, a)
	if d.pills(attClipW, attChipH) != 1 || !d.drew("1:05") || d.drew("memo.mp3") || d.drewIcon("fileicon-file-audio") {
		t.Errorf("a restored mp3 drew icons %q, texts %q, boxes %v: want the mini player's %dpx pillow and its 1:05, no card", d.icons, d.texts, d.boxes, attClipW)
	}
}

// A video uploaded this session is PendingVideoFile (isPendingVideoFile:
// Chromium's File.type starts with video/), whatever its type's svgicon:
// a 62px tile, its frame or (before one) the placeholder's black, under
// play-filled; a webm is no longer a card with the generic page.
func TestPendingVideoChip(t *testing.T) {
	defer slack.SetPlatformMimeTypes(nil)()
	a := newAttachments(&slickHost{th: theme.New(false)})
	if !a.add("screen recording.webm", []byte("not really a webm")) {
		t.Fatal("not attached")
	}
	d := galleryDrawn(t, a)
	if d.tiles(attChipH, attChipH, color.NRGBA{A: 0xff}) != 1 || !d.drewIcon(icons.Slack("play-filled")) || d.drew("screen recording.webm") || d.drewIcon("fileicon-generic") {
		t.Errorf("a webm drew icons %q, texts %q, boxes %v: want PendingVideoFile's black 62px tile and play-filled, no card", d.icons, d.texts, d.boxes)
	}
	// an mp3 uploaded this session is the File, not a playable file
	// object: a card with its type's svgicon
	a = newAttachments(&slickHost{th: theme.New(false)})
	if !a.add("memo.mp3", []byte("not really an mp3")) {
		t.Fatal("not attached")
	}
	if d := galleryDrawn(t, a); !d.drewIcon("fileicon-file-audio") || !d.drew("memo.mp3") || d.pills(attCardW, attChipH) != 1 {
		t.Errorf("an mp3 drew icons %q, texts %q: want its card with file-audio", d.icons, d.texts)
	}
}

// A file card's icon sits 13px in (c-pillow_file's 1px border and 12px
// padding) and its text 12px after it: the tile a non-existent file's
// card draws is at x 13.
func TestFileCardInset(t *testing.T) {
	a := newAttachments(&slickHost{th: theme.New(false)})
	a.mu.Lock()
	a.chips = append(a.chips, &attChip{id: int(attNextID.Add(1)), name: "F0FAKEGONE1", gone: true, prepared: &core.PreparedUpload{ID: "F0FAKEGONE1"}})
	a.mu.Unlock()
	d := galleryDrawn(t, a)
	want := image.Rect(13, 13, 13+attIconPx, 13+attIconPx)
	found := false
	for _, b := range d.boxes {
		if b.r == want && b.col == rgb(0xdddddd) {
			found = true
		}
	}
	if !found {
		t.Errorf("the card's exception tile isn't at %v: boxes %v", want, d.boxes)
	}
}
