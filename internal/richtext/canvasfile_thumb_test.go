package richtext

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// Synthetic fixtures only.

// thumbRes holds canvas thumbnails by file id (what store.Store's
// CanvasPreview answers).
type thumbRes struct {
	fakeRes
	thumbs map[string]string
	asked  []int64
}

func (r *thumbRes) CanvasPreview(fileID string, updated int64) (json.RawMessage, bool) {
	r.asked = append(r.asked, updated)
	t, ok := r.thumbs[fileID]
	if !ok {
		return nil, false
	}
	if t == "" {
		return nil, true
	}
	return json.RawMessage(t), true
}

func canvasFileOf(t *testing.T, r Resolver, id string) *File {
	t.Helper()
	m := slack.Message{Files: []slack.File{{ID: id, Mode: "quip", Filetype: "quip", Title: "Plan", Updated: 1795492383}}}
	for _, b := range FromMessage(r, &m).Blocks {
		if b.File != nil {
			return b.File
		}
	}
	t.Fatal("no file block")
	return nil
}

// useQuipBlocks for a message card (FileQuip with hideTitle; boot-shared
// 6WLC, build d4845cd3e2ea): the thumbnail's first 11 blocks without the
// first header, drawn as Block Kit; nothing for an errored thumbnail or
// one without blocks; a card with no thumbnail held asks for one.
func TestCanvasCardQuipBlocks(t *testing.T) {
	hdr := func(s string) string { return `{"type":"header","text":{"type":"plain_text","text":"` + s + `"}}` }
	para := func(s string) string {
		return `{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"` + s + `"}]}]}`
	}
	var bs []string
	bs = append(bs, hdr("Title"), para("one"), hdr("Second header"))
	for i := 2; i <= 12; i++ {
		bs = append(bs, para("p"+string(rune('a'+i))))
	}
	r := &thumbRes{thumbs: map[string]string{
		"F1": `{"blocks":[` + strings.Join(bs, ",") + `]}`,
		"F2": `{"blocks":[],"error":true}`,
		"F3": `{"blocks":[]}`,
		"F4": `{"error":false}`,
		"F5": "",
	}}
	f := canvasFileOf(t, r, "F1")
	if f.CanvasPreviewWanted {
		t.Error("a held thumbnail's card asks for one")
	}
	var texts []string
	for _, b := range f.CanvasPreview {
		texts = append(texts, PlainText(Doc{Blocks: []Block{b}}))
	}
	// 11 blocks: Title (dropped), one, Second header, pc … pj
	want := []string{"one", "Second header", "pc", "pd", "pe", "pf", "pg", "ph", "pi", "pj"}
	if strings.Join(texts, "|") != strings.Join(want, "|") {
		t.Errorf("preview blocks %q, want %q (11 blocks, the first header dropped)", texts, want)
	}
	for _, id := range []string{"F2", "F3", "F4", "F5"} {
		if f := canvasFileOf(t, r, id); len(f.CanvasPreview) != 0 || f.CanvasPreviewWanted {
			t.Errorf("%s: preview %v (wanted %v), want none", id, f.CanvasPreview, f.CanvasPreviewWanted)
		}
	}
	f = canvasFileOf(t, r, "F9")
	if !f.CanvasPreviewWanted || f.CanvasUpdated != 1795492383 || r.asked[len(r.asked)-1] != 1795492383 {
		t.Errorf("a card with no thumbnail: wanted %v at %d (asked with %v)", f.CanvasPreviewWanted, f.CanvasUpdated, r.asked)
	}
}
