package core

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/quip"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// Synthetic fixtures only.

// thumbDays are two UTC calendar days a month on (within Slack's 182 days,
// so {date} writes no year), and how Slack reads them in any zone: the
// day's month and ordinal.
func thumbDays() (from, to time.Time, want []string) {
	from = time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, 30)
	to = from.AddDate(0, 0, 2)
	read := func(d time.Time) string {
		n := d.Day()
		suf := "th"
		switch {
		case n%100 >= 11 && n%100 <= 13:
		case n%10 == 1:
			suf = "st"
		case n%10 == 2:
			suf = "nd"
		case n%10 == 3:
			suf = "rd"
		}
		return d.Format("January ") + strconv.Itoa(n) + suf
	}
	return from, to, []string{read(from), read(to)}
}

// thumbCanvas adds a canvas whose Block Kit thumbnail is Quip's shape: its
// title's header, then a paragraph with two date elements as Quip's writer
// emits them (slackDateTextElement: the UTC calendar day at 00:00 UTC,
// timezone "UTC", format "{date}"). It returns the message that shares it
// (its files[] entry is files.info's object) and the dates as Slack reads
// them.
func thumbCanvas(t *testing.T, fs *fakeslack.Server, id string, updated int64) (slack.Message, []string) {
	t.Helper()
	from, to, want := thumbDays()
	date := func(d time.Time) string {
		return `{"type":"date","timestamp":` + itoa64(d.Unix()) + `,"timezone":"UTC","format":"{date}","fallback":"` + d.Format("Jan 2") + `"}`
	}
	thumb := `{"blocks":[` +
		`{"type":"header","block_id":"h","text":{"type":"plain_text","text":"Event FAQ","emoji":true}},` +
		`{"type":"rich_text","block_id":"p1","elements":[{"type":"rich_text_section","elements":[` +
		`{"type":"text","text":"The IRL event is held from "},` + date(from) + `,{"type":"text","text":" to "},` + date(to) +
		`,{"type":"text","text":" at the venue."}]}]}]}`
	fs.AddCanvas(fakeslack.FakeCanvas{ID: id, Title: "Event FAQ", Updated: updated, Thumbnail: thumb})
	return canvasMessage(t, fs, id), want
}

// canvasMessage is a message sharing canvas id.
func canvasMessage(t *testing.T, fs *fakeslack.Server, id string) slack.Message {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"type": "message", "ts": "1795545408.463939", "user": "U0FAKE00001", "text": "see the FAQ",
		"files": []any{fs.CanvasFile(id)}})
	var m slack.Message
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
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

// previewText is a converted card's preview as text.
func previewText(f *richtext.File) string {
	return richtext.PlainText(richtext.Doc{Blocks: f.CanvasPreview})
}

// A canvas card's preview is the canvas's Block Kit thumbnail, read as
// Slack's FileQuip reads it (the Quip thread object, then its
// BLOCK_KIT_THUMBNAIL parcel), only once the card asks for it (it is on
// screen); its dates are date elements, drawn as Slack formats them,
// where the HTML export has empty controls. A newer edit's card keeps the
// preview it had until the new thumbnail comes.
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
	waitFor(t, "the thumbnail", 10*time.Second, func() bool { _, ok := a.S.CanvasPreview(id, f.CanvasUpdated); return ok })
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
	text := previewText(f)
	if want := "The IRL event is held from " + dates[0] + " to " + dates[1] + " at the venue."; !strings.Contains(text, want) {
		t.Errorf("preview %q, want %q", text, want)
	}
	if strings.Contains(text, "Event FAQ") {
		t.Errorf("the title's header stayed in the preview (hideTitle): %q", text)
	}

	// asking again for the same file reads nothing more
	a.S.WantCanvasPreview(id, f.CanvasUpdated)
	time.Sleep(300 * time.Millisecond)
	if n := thumbLoads(fs, id); n != 1 {
		t.Errorf("a held thumbnail was read again (%d parcel loads)", n)
	}
	// a newer edit's card asks again, and keeps drawing the old preview
	// meanwhile: the card doesn't collapse and reopen
	m2, _ := thumbCanvas(t, fs, id, 1795499999)
	f2 := canvasCard(t, a, &m2)
	if !f2.CanvasPreviewWanted || !strings.Contains(previewText(f2), dates[0]) {
		t.Fatalf("a newer edit's card: wanted %v, preview %q; want it asking, with the old preview drawn", f2.CanvasPreviewWanted, previewText(f2))
	}
	a.S.WantCanvasPreview(id, 1795499999)
	waitFor(t, "the newer thumbnail", 10*time.Second, func() bool { _, ok := a.S.CanvasPreview(id, 1795499999); return ok })
	if n := thumbLoads(fs, id); n != 1 { // the edited canvas (AddCanvas replaced it) read once
		t.Errorf("the edited canvas's parcel was loaded %d times, want once", n)
	}
}

// Karim's report on α.165 ("held from  to "), in his zone: a canvas's
// dates are UTC calendar days at 00:00 UTC, so drawn in the viewer's zone
// they read a day early anywhere west of UTC; the date element's own
// timezone, "UTC", keeps them the canvas's. The test runs itself again in
// a child process whose zone is America/Chicago (time.Local is the
// process's, and the App's goroutines read it).
func TestCanvasCardDatesWestOfUTC(t *testing.T) {
	if os.Getenv("CLACK_TEST_CHICAGO") == "" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestCanvasCardDatesWestOfUTC$", "-test.count=1", "-test.v")
		cmd.Env = append(os.Environ(), "CLACK_TEST_CHICAGO=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("in America/Chicago: %v\n%s", err, out)
		}
		return
	}
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	time.Local = loc // before any of the App's goroutines start
	a, fs := canvasApp(t)
	const id = "F0CANVTHMB4"
	m, dates := thumbCanvas(t, fs, id, 1795492383)
	f := canvasCard(t, a, &m)
	a.S.WantCanvasPreview(id, f.CanvasUpdated)
	waitFor(t, "the thumbnail", 10*time.Second, func() bool { _, ok := a.S.CanvasPreview(id, f.CanvasUpdated); return ok })
	text := previewText(canvasCard(t, a, &m))
	if want := "held from " + dates[0] + " to " + dates[1] + " at"; !strings.Contains(text, want) {
		t.Errorf("in America/Chicago the preview reads %q, want %q: the canvas's days, not the days before", text, want)
	}
}

// A canvas a fixture gives no thumbnail has Quip's own, made of its title
// and content (the demo's canvases): its card has a preview.
func TestCanvasCardQuipMadeThumbnail(t *testing.T) {
	a, fs := canvasApp(t)
	const id = "F0CANVTHMB5"
	fs.AddCanvas(fakeslack.FakeCanvas{ID: id, Title: "Launch plan", Updated: 1795492383, Doc: canvas.Doc{Blocks: []canvas.Block{
		{Kind: canvas.Paragraph, Spans: []richtext.Span{{Kind: richtext.Text, Text: "Ship the demo on Friday."}}},
		{Kind: canvas.Bullet, Spans: []richtext.Span{{Kind: richtext.Text, Text: "Record the walkthrough"}}},
	}}})
	m := canvasMessage(t, fs, id)
	f := canvasCard(t, a, &m)
	a.S.WantCanvasPreview(id, f.CanvasUpdated)
	waitFor(t, "the thumbnail", 10*time.Second, func() bool { _, ok := a.S.CanvasPreview(id, f.CanvasUpdated); return ok })
	text := previewText(canvasCard(t, a, &m))
	if !strings.Contains(text, "Ship the demo on Friday.") || !strings.Contains(text, "Record the walkthrough") || strings.Contains(text, "Launch plan") {
		t.Errorf("the Quip-made preview reads %q, want the content without the title", text)
	}
}

// A thumbnail the network fails is asked for again once its backoff
// passes, not before: the retry rebuilds the card's message (a names
// event), and the card, still on screen, asks again. Release stops a retry
// still waiting (its timer held the App). One Quip or Slack won't load
// leaves the card without a preview, and isn't tried again.
func TestCanvasCardThumbnailBackoff(t *testing.T) {
	first, mx := CanvasPreviewRetryFirst, CanvasPreviewRetryMax
	// past the client's guard (slack guard.go: an identical failed request
	// fails fast for a second), as the 30 s default always is
	CanvasPreviewRetryFirst, CanvasPreviewRetryMax = 1200*time.Millisecond, 2*time.Second
	t.Cleanup(func() { CanvasPreviewRetryFirst, CanvasPreviewRetryMax = first, mx })
	a, fs := canvasApp(t)
	const id = "F0CANVTHMB2"
	m, _ := thumbCanvas(t, fs, id, 1795492383)
	f := canvasCard(t, a, &m)
	// the UI: a names event for the canvas rebuilds its card, which asks
	// again while it's on screen
	evs, unsub := a.Subscribe()
	t.Cleanup(unsub)
	go func() {
		for ev := range evs {
			if ev.Type == "names" && strings.Contains(strings.Join(ev.Users, " "), id) {
				a.S.WantCanvasPreview(id, f.CanvasUpdated)
			}
		}
	}()
	fs.SetFault("canvas/load-data", fakeslack.Fault{Status: http.StatusBadGateway, Times: 1})
	a.S.WantCanvasPreview(id, f.CanvasUpdated)
	waitFor(t, "the failed read", 10*time.Second, func() bool { return fs.CallCount("canvas/load-data") >= 1 })
	failed := time.Now()
	if _, ok := a.S.CanvasPreview(id, f.CanvasUpdated); ok {
		t.Fatal("a failed read left a preview")
	}
	waitFor(t, "the read after the backoff", 10*time.Second, func() bool { return fs.CallCount("canvas/load-data") >= 2 })
	if d := time.Since(failed); d < CanvasPreviewRetryFirst-200*time.Millisecond {
		t.Errorf("the read came again %s after the failure, inside the %s backoff", d.Round(time.Millisecond), CanvasPreviewRetryFirst)
	}
	waitFor(t, "the preview", 10*time.Second, func() bool { _, ok := a.S.CanvasPreview(id, f.CanvasUpdated); return ok })

	// Quip refuses the thread: no preview, and no retry
	const gone = "F0CANVTHMB3"
	fs.AddCanvas(fakeslack.FakeCanvas{ID: gone, Title: "Gone", Thumbnail: `{"blocks":[]}`})
	fs.SetFault("canvas/load-data", fakeslack.Fault{Error: "not_found", Times: 1})
	n := fs.CallCount("canvas/load-data")
	a.S.WantCanvasPreview(gone, 0)
	waitFor(t, "the refused read", 10*time.Second, func() bool { _, ok := a.S.CanvasPreview(gone, 0); return ok })
	if thumb, _ := a.S.CanvasPreview(gone, 0); thumb != nil {
		t.Errorf("a refused thread has the thumbnail %s, want none", thumb)
	}
	time.Sleep(CanvasPreviewRetryFirst + 300*time.Millisecond)
	if got := fs.CallCount("canvas/load-data"); got != n+1 {
		t.Errorf("%d load-data calls after the refusal, want its one: a refused thread isn't read again", got-n)
	}

	// a retry still waiting at Release stops there
	const later = "F0CANVTHMB6"
	fs.AddCanvas(fakeslack.FakeCanvas{ID: later, Title: "Later", Updated: 1795492383})
	fs.SetFault("canvas/load-data", fakeslack.Fault{Status: http.StatusBadGateway, Times: 1})
	n = fs.CallCount("canvas/load-data")
	a.S.WantCanvasPreview(later, 1795492383)
	waitFor(t, "the failed read", 10*time.Second, func() bool { return fs.CallCount("canvas/load-data") > n })
	r := a.previewRetries()
	waitFor(t, "the retry's timer", 5*time.Second, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.timers[later] != nil })
	a.Release()
	r.mu.Lock()
	left := len(r.timers)
	r.mu.Unlock()
	if left != 0 {
		t.Errorf("%d retry timer(s) left after Release: each holds the App until it fires", left)
	}
}
