package core

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/quip"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// Synthetic fixtures only.

// thumbCanvas adds a canvas whose Block Kit thumbnail has two date
// elements in its first paragraph, and returns the message that shares
// it (its files[] entry is files.info's object) and the dates' texts as
// Slack's date element draws them.
func thumbCanvas(t *testing.T, fs *fakeslack.Server, id string, updated int64) (slack.Message, []string) {
	t.Helper()
	from, to := int64(1797768000), int64(1797940800) // two days in December 2026
	thumb := `{"blocks":[` +
		`{"type":"header","block_id":"h","text":{"type":"plain_text","text":"Event FAQ","emoji":true}},` +
		`{"type":"rich_text","block_id":"p1","elements":[{"type":"rich_text_section","elements":[` +
		`{"type":"text","text":"The IRL event is held from "},` +
		`{"type":"date","timestamp":` + itoa64(from) + `,"format":"{date_long}","fallback":"Dec 26"},` +
		`{"type":"text","text":" to "},` +
		`{"type":"date","timestamp":` + itoa64(to) + `,"format":"{date_long}","fallback":"Dec 28"},` +
		`{"type":"text","text":" at the venue."}]}]}]}`
	fs.AddCanvas(fakeslack.FakeCanvas{ID: id, Title: "Event FAQ", Updated: updated, Thumbnail: thumb})
	raw, _ := json.Marshal(map[string]any{"type": "message", "ts": "1795545408.463939", "user": "U0FAKE00001", "text": "see the FAQ",
		"files": []any{fs.CanvasFile(id)}})
	var m slack.Message
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m, []string{richtext.FormatDate(time.Unix(from, 0), "{date_long}"), richtext.FormatDate(time.Unix(to, 0), "{date_long}")}
}

// canvasCard is the message's canvas file as converted now.
func canvasCard(t *testing.T, a *App, m *slack.Message) *richtext.File {
	t.Helper()
	for _, b := range richtext.FromMessage(a.S, m).Blocks {
		if b.File != nil && richtext.IsCanvasFile(b.File) {
			return b.File
		}
	}
	t.Fatal("no canvas card")
	return nil
}

func thumbLoads(fs *fakeslack.Server, id string) int {
	c, _ := fs.Canvas(id)
	return c.ThumbnailLoads
}

// A canvas card's preview is the canvas's Block Kit thumbnail, read as
// Slack's FileQuip reads it (the Quip thread object, then its
// BLOCK_KIT_THUMBNAIL parcel), only once the card asks for it (it is on
// screen); its dates are date elements, drawn as Slack formats them,
// where the HTML export has empty controls.
func TestCanvasCardThumbnail(t *testing.T) {
	a, fs := canvasApp(t)
	const id = "F0CANVTHMB1"
	m, dates := thumbCanvas(t, fs, id, 1795492383)

	// converting the message reads nothing: the card isn't on screen
	f := canvasCard(t, a, &m)
	if !f.CanvasPreviewWanted || f.CanvasUpdated != 1795492383 || len(f.CanvasPreview) != 0 {
		t.Fatalf("a fresh card: wanted %v at %d, preview %v", f.CanvasPreviewWanted, f.CanvasUpdated, f.CanvasPreview)
	}
	time.Sleep(300 * time.Millisecond)
	if n := fs.CallCount("canvas/load-data"); n != 0 || thumbLoads(fs, id) != 0 {
		t.Fatalf("a conversion alone sent %d load-data calls", n)
	}

	// the card on screen asks: two load-data calls, the thread then the parcel
	a.S.WantCanvasPreview(id, f.CanvasUpdated)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, ok := a.S.CanvasPreview(id, f.CanvasUpdated); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no thumbnail after the card asked")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n := fs.CallCount("canvas/load-data"); n != 2 || thumbLoads(fs, id) != 1 {
		t.Errorf("%d load-data calls, %d parcel loads; want the thread and the parcel", n, thumbLoads(fs, id))
	}
	var shapes []string
	for _, r := range fs.QuipRequests() {
		if r.Load != nil {
			shapes = append(shapes, quip.FieldsString(r.Load))
		}
	}
	if strings.Join(shapes, " ") != "1 4" {
		t.Errorf("load-data requests carried fields %v, want [1] (object_ids) then [4] (parcels)", shapes)
	}

	f = canvasCard(t, a, &m)
	if f.CanvasPreviewWanted || len(f.CanvasPreview) == 0 {
		t.Fatalf("after the read: wanted %v, preview %v", f.CanvasPreviewWanted, f.CanvasPreview)
	}
	text := richtext.PlainText(richtext.Doc{Blocks: f.CanvasPreview})
	want := "The IRL event is held from " + dates[0] + " to " + dates[1] + " at the venue."
	if !strings.Contains(text, want) {
		t.Errorf("preview %q, want %q", text, want)
	}
	if strings.Contains(text, "Event FAQ") {
		t.Errorf("the title's header stayed in the preview (hideTitle): %q", text)
	}

	// asking again for the same file reads nothing more; a newer edit does
	a.S.WantCanvasPreview(id, f.CanvasUpdated)
	time.Sleep(300 * time.Millisecond)
	if n := thumbLoads(fs, id); n != 1 {
		t.Errorf("a held thumbnail was read again (%d parcel loads)", n)
	}
	m2, _ := thumbCanvas(t, fs, id, 1795499999)
	if f2 := canvasCard(t, a, &m2); !f2.CanvasPreviewWanted {
		t.Fatal("a newer edit's card doesn't ask for its thumbnail")
	}
	a.S.WantCanvasPreview(id, 1795499999)
	waitFor(t, "the newer thumbnail", 10*time.Second, func() bool { _, ok := a.S.CanvasPreview(id, 1795499999); return ok })
	if n := thumbLoads(fs, id); n != 1 { // the edited canvas (AddCanvas replaced it) read once
		t.Errorf("the edited canvas's parcel was loaded %d times, want once", n)
	}
}

// A thumbnail the network fails isn't asked for again until its backoff
// passes; one Quip won't load leaves the card without a preview.
func TestCanvasCardThumbnailBackoff(t *testing.T) {
	first, mx := CanvasPreviewRetryFirst, CanvasPreviewRetryMax
	CanvasPreviewRetryFirst, CanvasPreviewRetryMax = 400*time.Millisecond, time.Second
	t.Cleanup(func() { CanvasPreviewRetryFirst, CanvasPreviewRetryMax = first, mx })
	a, fs := canvasApp(t)
	const id = "F0CANVTHMB2"
	m, _ := thumbCanvas(t, fs, id, 1795492383)
	f := canvasCard(t, a, &m)
	fs.SetFault("canvas/load-data", fakeslack.Fault{Status: http.StatusBadGateway, Times: 1})
	a.S.WantCanvasPreview(id, f.CanvasUpdated)
	waitFor(t, "the failed read", 10*time.Second, func() bool { return fs.CallCount("canvas/load-data") >= 1 })
	time.Sleep(150 * time.Millisecond)
	a.S.WantCanvasPreview(id, f.CanvasUpdated) // the card still on screen, inside the backoff
	time.Sleep(150 * time.Millisecond)
	if n := fs.CallCount("canvas/load-data"); n != 1 {
		t.Fatalf("%d load-data calls inside the backoff, want the failed one", n)
	}
	if _, ok := a.S.CanvasPreview(id, f.CanvasUpdated); ok {
		t.Fatal("a failed read left a preview")
	}
	// after it the card's next ask reads it
	time.Sleep(CanvasPreviewRetryFirst)
	a.S.WantCanvasPreview(id, f.CanvasUpdated)
	waitFor(t, "the read after the backoff", 10*time.Second, func() bool { _, ok := a.S.CanvasPreview(id, f.CanvasUpdated); return ok })

	// Quip refuses the thread: no preview, and no retry
	const gone = "F0CANVTHMB3"
	fs.AddCanvas(fakeslack.FakeCanvas{ID: gone, Title: "Gone", Thumbnail: `{"blocks":[]}`})
	fs.SetFault("canvas/load-data", fakeslack.Fault{Error: "not_found", Times: 1})
	a.S.WantCanvasPreview(gone, 0)
	waitFor(t, "the refused read", 10*time.Second, func() bool { _, ok := a.S.CanvasPreview(gone, 0); return ok })
	if thumb, _ := a.S.CanvasPreview(gone, 0); thumb != nil {
		t.Errorf("a refused thread has the thumbnail %s, want none", thumb)
	}
}
