package composer

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"log"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// lockedBuf is a log sink for a test.
type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// draftMetaEnv is an empty composer on a channel of its own, whose server
// draft comes from another device (a draft_create event) naming fileID by
// id alone: drafts.list's files has no object for it.
type draftMetaEnv struct {
	t     *testing.T
	c     *Composer
	frame func()
}

func newDraftMetaEnv(t *testing.T, fs *fakeslack.Server, ch, fileID string) *draftMetaEnv {
	t.Helper()
	key := draftKey(ch, "")
	forget := func() {
		clearDraft(ch, "")
		serverDraftIDs.Delete(key)
		attStore.Lock()
		delete(attStore.m, key)
		attStore.Unlock()
	}
	forget()
	t.Cleanup(forget)
	app := core.New(slack.New(fs.Creds()), store.New(""))
	app.ServeOffline()
	h := &slickHost{app: app, th: theme.New(false)}
	c := New(h, ch, "")
	t.Cleanup(c.Close)
	ops := new(op.Ops)
	e := &draftMetaEnv{t: t, c: c, frame: func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Constraints{Max: image.Pt(900, 600)}, Now: time.Now()}
		c.Layout(gtx)
	}}
	e.frame()
	raw, _ := json.Marshal(map[string]any{"type": "draft_create", "draft": map[string]any{
		"id": "Dr0META" + fileID[len(fileID)-4:], "last_updated_ts": "1700000000000", "file_ids": []string{fileID},
		"files": []any{}, "destinations": []any{map[string]any{"channel_id": ch}}, "blocks": []any{}}})
	app.Inject(slack.Event{Type: "draft_create", Raw: raw})
	e.until("the draft's file in the composer", func() bool { return c.att.Len() == 1 })
	return e
}

func (e *draftMetaEnv) until(what string, cond func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		e.frame()
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.t.Fatalf("timed out waiting for %s (chip %+v)", what, chipOf(e.c))
}

// cardDrawn is what a frame of the composer drew: every string, every
// rounded box filled (in the coordinates it was drawn at) and every icon.
type cardDrawn struct {
	texts []string
	boxes []drawnBox
	icons []string
}

type drawnBox struct {
	r   image.Rectangle
	col color.NRGBA
}

// drawn draws a frame and returns what it drew.
func (e *draftMetaEnv) drawn() cardDrawn {
	var d cardDrawn
	prev := theme.DrawHook
	theme.DrawHook = func(s string) { d.texts = append(d.texts, s) }
	stop := recordDraws(func(r image.Rectangle, col color.NRGBA) { d.boxes = append(d.boxes, drawnBox{r, col}) },
		func(n string) { d.icons = append(d.icons, n) })
	e.frame()
	stop()
	theme.DrawHook = prev
	return d
}

// pills counts the boxes w by h.
func (d cardDrawn) pills(w, h int) int {
	n := 0
	for _, b := range d.boxes {
		if b.r.Dx() == w && b.r.Dy() == h {
			n++
		}
	}
	return n
}

// tiles counts the boxes w by h in col.
func (d cardDrawn) tiles(w, h int, col color.NRGBA) int {
	n := 0
	for _, b := range d.boxes {
		if b.r.Dx() == w && b.r.Dy() == h && b.col == col {
			n++
		}
	}
	return n
}

// drew reports whether a string drawn contains s.
func (d cardDrawn) drew(s string) bool {
	for _, t := range d.texts {
		if strings.Contains(t, s) {
			return true
		}
	}
	return false
}

// drewText reports whether s was drawn as a string of its own.
func (d cardDrawn) drewText(s string) bool {
	for _, t := range d.texts {
		if t == s {
			return true
		}
	}
	return false
}

// drewIcon reports whether icon name was drawn.
func (d cardDrawn) drewIcon(name string) bool {
	for _, n := range d.icons {
		if n == name {
			return true
		}
	}
	return false
}

// chipOf is the composer's first attachment as its card draws it.
func chipOf(c *Composer) attChipState {
	c.att.mu.Lock()
	defer c.att.mu.Unlock()
	if len(c.att.chips) == 0 {
		return attChipState{}
	}
	return chipState(c.att.chips[0])
}

// Karim (2026-11-17, #clack 1794887336.751199): a draft restored with its
// file named by id alone drew "F0CJB175J10" over "File". Slack's
// composer asks for the file it doesn't have (getFileById's unknown fetch,
// files.info) and draws it unknown meanwhile, two MissingText bars, never
// the id; once files.info answers, the card has the file's name, its type
// (pretty_type) and its tile.
func TestDraftFileArrivesLate(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	const id = "F0LATE0001"
	var asked atomic.Int64
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method != "files.info" || f.Get("file") != id {
			return nil, false
		}
		asked.Add(1)
		<-release // the file arrives late
		return map[string]any{"ok": true, "comments": []any{}, "file": map[string]any{"id": id, "name": "q3-report.pdf",
			"title": "Q3 report", "filetype": "pdf", "pretty_type": "PDF", "mimetype": "application/pdf", "size": 48213, "mode": "hosted"}}, true
	})
	e := newDraftMetaEnv(t, fs, fs.ChannelIDs()[4], id)
	// held: the card draws MissingText's two 120x8 pills over the empty
	// 40px tile, and no text of its own
	d := e.drawn()
	if d.drew(id) || d.drewText("File") {
		t.Errorf("before its file came the card drew %q; want neither the id nor \"File\"", d.texts)
	}
	if n := d.pills(120, 8); n != 2 {
		t.Errorf("before its file came the card drew %d 120x8 pills, want 2 (MissingText); boxes %v", n, d.boxes)
	}
	if n := d.tiles(40, 40, color.NRGBA{R: 0xdd, G: 0xdd, B: 0xdd, A: 0xff}); n != 1 {
		t.Errorf("before its file came the card drew %d empty exception tiles (40px, sk_foreground_low_solid), want 1; boxes %v", n, d.boxes)
	}
	e.until("files.info for the draft's file", func() bool { return asked.Load() > 0 })
	once.Do(func() { close(release) })
	e.until("the file's name and type", func() bool {
		d := e.drawn()
		return d.drew("Q3 report") && d.drew("PDF")
	})
	d = e.drawn()
	if n := d.pills(120, 8); n != 0 || d.drew(id) {
		t.Errorf("after files.info the card drew %d pills and %q", n, d.texts)
	}
	if st := chipOf(e.c); st.gone || st.loading || st.name != "q3-report.pdf" {
		t.Errorf("after files.info: %+v", st)
	}
}

// files.info refuses the draft's file (file_not_found): fetchWithRetries
// doesn't try again for an answer, the file becomes non-existent ("This
// file was deleted." over the trash tile), and the log says which method
// failed, how, and which file, by its hashed id only.
func TestDraftFileInfoFails(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	const id = "F0GONE0001"
	var asked atomic.Int64
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "files.info" && f.Get("file") == id {
			asked.Add(1)
			return map[string]any{"ok": false, "error": "file_not_found"}, true
		}
		return nil, false
	})
	var buf lockedBuf
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	e := newDraftMetaEnv(t, fs, fs.ChannelIDs()[5], id)
	e.until("the card drawing the file deleted", func() bool { return e.drawn().drew("This file was deleted.") })
	if d := e.drawn(); !d.drewIcon("trash") || d.drew(id) || d.pills(120, 8) != 0 {
		t.Errorf("the deleted file's card drew icons %q, texts %q, %d pills; want the trash glyph, no id, no pills", d.icons, d.texts, d.pills(120, 8))
	}
	time.Sleep(200 * time.Millisecond)
	if n := asked.Load(); n != 1 {
		t.Errorf("files.info asked %d times, want once (an answer isn't retried)", n)
	}
	out := buf.String()
	if !strings.Contains(out, "files.info") || !strings.Contains(out, "file_not_found") || !strings.Contains(out, hashedFileID(id)) {
		t.Errorf("the log doesn't name the method, the error and the hashed id %s:\n%s", hashedFileID(id), out)
	}
	if strings.Contains(out, id) {
		t.Errorf("the log has the file's id:\n%s", out)
	}
}

// A files.info that times out is tried again, three attempts in all
// (fetchWithRetries): two timeouts, then the file.
func TestDraftFileRetriesTimeouts(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	restore := shortMetaTimeout(300 * time.Millisecond)
	t.Cleanup(restore)
	const id = "F0SLOW0001"
	var asked atomic.Int64
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method != "files.info" || f.Get("file") != id {
			return nil, false
		}
		if asked.Add(1) <= 2 {
			time.Sleep(time.Second) // past the attempt's timeout
		}
		return map[string]any{"ok": true, "comments": []any{}, "file": map[string]any{"id": id, "name": "notes.txt",
			"title": "notes.txt", "filetype": "text", "pretty_type": "Plain Text", "mimetype": "text/plain", "size": 12, "mode": "hosted"}}, true
	})
	e := newDraftMetaEnv(t, fs, fs.ChannelIDs()[6], id)
	e.until("the file after two timeouts", func() bool { st := chipOf(e.c); return st.title == "notes.txt" && !st.loading })
	if n := asked.Load(); n != 3 {
		t.Errorf("files.info asked %d times, want 3 (two timeouts, then the answer)", n)
	}
	if st := chipOf(e.c); st.label != "Plain Text" || st.gone {
		t.Errorf("after the retries: %+v", st)
	}
}
