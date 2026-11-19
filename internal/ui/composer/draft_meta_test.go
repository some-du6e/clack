package composer

import (
	"bytes"
	"encoding/json"
	"image"
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
	st := chipOf(e.c)
	if !st.loading {
		t.Errorf("before its file came the card is %+v, want it unknown (the loading bars), never the id", st)
	}
	e.until("files.info for the draft's file", func() bool { return asked.Load() > 0 })
	once.Do(func() { close(release) })
	e.until("the file's name and type", func() bool {
		st := chipOf(e.c)
		return !st.loading && st.title == "Q3 report" && st.label == "PDF"
	})
	if st := chipOf(e.c); st.gone || st.name != "q3-report.pdf" {
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
	e.until("the file gone", func() bool { return chipOf(e.c).gone })
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
