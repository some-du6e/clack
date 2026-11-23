package composer

import (
	"sync/atomic"
	"testing"
	"time"

	vclip "github.com/some-du6e/clack/internal/clip"
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
)

// A pending file's type (its mimetype, and the File a plugin renaming it
// gets) is its File's as Slack's client holds it: a clip's the recorder's
// audio/mp4, any other the type Chromium gives its name, here on an OS that
// types .py and nothing else. Go's mime table, which it was, gave a .js
// "text/javascript; charset=utf-8".
func TestAttachmentFileTypes(t *testing.T) {
	defer slack.SetPlatformMimeTypes(func(ext string) (string, bool) { return "text/x-python-script", ext == "py" })()
	a := newAttachments(&attFakeHost{})
	want := map[string]string{
		"BlockKitSend (2).js": "text/javascript",
		"notes.txt":           "text/plain",
		"script.py":           "text/x-python-script",
		"main.go":             "",
		"audio_message.m4a":   "audio/mp4",
	}
	for _, n := range []string{"BlockKitSend (2).js", "notes.txt", "script.py", "main.go"} {
		if !a.add(n, []byte("x")) {
			t.Fatalf("%s: not attached", n)
		}
	}
	if !a.addClip(vclip.File{Name: "audio_message.m4a", Mime: "audio/mp4", Subtype: "slack_audio", Data: []byte("clip")}) {
		t.Fatal("the clip wasn't attached")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.chips) != len(want) {
		t.Fatalf("%d chips, want %d", len(a.chips), len(want))
	}
	for _, c := range a.chips {
		if got := c.pendingLocked().Mimetype; got != want[c.name] {
			t.Errorf("%s: pending file mimetype %q, want %q", c.name, got, want[c.name])
		}
		if got := attFileType(c.name, c.mime()); got != want[c.name] {
			t.Errorf("%s: attFileType %q, want %q", c.name, got, want[c.name])
		}
		// a plugin's rename (AnonymiseFileNames) leaves the File's type
		name := c.name
		c.name = "renamed.bin"
		if got := c.pendingLocked().Mimetype; got != want[name] {
			t.Errorf("%s renamed renamed.bin: pending file mimetype %q, want the File's %q", name, got, want[name])
		}
		c.name = name
	}
}

// The OS is asked once per file, as it's attached, never under
// attachments.mu (the registry, UTType or the XDG database can be slow,
// and the UI goroutine takes the lock every frame), and never again for
// what the frames read: the gallery's views and the plugins' pending files.
func TestAttachmentTypeLookedUpOnce(t *testing.T) {
	a := newAttachments(&attFakeHost{})
	var lookups, underLock atomic.Int32
	defer slack.SetPlatformMimeTypes(func(ext string) (string, bool) {
		lookups.Add(1)
		// held by whoever asked, the lock stays taken; another goroutine's
		// (a prepare ending) lets go at once
		for i := 0; ; i++ {
			if a.mu.TryLock() {
				a.mu.Unlock()
				break
			}
			if i == 100 {
				underLock.Add(1)
				break
			}
			time.Sleep(time.Millisecond)
		}
		return "text/x-" + ext, true
	})()
	names := []string{"a.py", "b.go", "c.rs", "d.kt"}
	for _, n := range names {
		if !a.add(n, []byte("x")) {
			t.Fatalf("%s: not attached", n)
		}
	}
	if got := lookups.Load(); got != int32(len(names)) {
		t.Errorf("%d OS lookups attaching %d files, want one each", got, len(names))
	}
	for f := 0; f < 120; f++ { // two seconds of frames
		a.mu.Lock()
		for _, c := range a.chips {
			_ = c.fileTypeLocked()
			_ = c.pendingLocked()
		}
		a.mu.Unlock()
	}
	if got := lookups.Load(); got != int32(len(names)) {
		t.Errorf("%d OS lookups after 120 frames, want the %d the files made", got, len(names))
	}
	if n := underLock.Load(); n != 0 {
		t.Errorf("%d OS lookups ran under attachments.mu", n)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, c := range a.chips {
		if want := "text/x-" + names[i][2:]; c.fileTypeLocked() != want {
			t.Errorf("%s: type %q, want the OS's %q", c.name, c.fileTypeLocked(), want)
		}
	}
}

// A draft's file restored from the server has the server's mimetype, as
// Slack's restored file does; one the draft named by id alone has it once
// files.info answers (it had the type of nothing while it loaded, and
// kept it), else the type its name gives.
func TestRestoredDraftFileTypes(t *testing.T) {
	defer slack.SetPlatformMimeTypes(nil)()
	a := newAttachments(&attFakeHost{})
	a.restoreFiles([]core.DraftFile{
		{ID: "F0FAKEDRAFT1", Name: "deck.key", Mimetype: "application/x-iwork-keynote-sffkey"},
		{ID: "F0FAKEDRAFT2", Name: "notes.txt"},
	})
	loading := &attChip{id: int(attNextID.Add(1)), name: "F0FAKEDRAFT3", loading: true, prepared: &core.PreparedUpload{ID: "F0FAKEDRAFT3"}}
	idOnly := &attChip{id: int(attNextID.Add(1)), name: "F0FAKEDRAFT4", loading: true, prepared: &core.PreparedUpload{ID: "F0FAKEDRAFT4"}}
	a.mu.Lock()
	a.chips = append(a.chips, loading, idOnly)
	if got := loading.pendingLocked().Mimetype; got != "" {
		t.Errorf("a file still loading: mimetype %q, want none yet", got)
	}
	a.mu.Unlock()
	a.metaDone(loading, fileMeta("report.pdf", "application/x-server-pdf"), true)
	a.metaDone(idOnly, fileMeta("table.csv", ""), true)
	want := map[string]string{
		"deck.key":   "application/x-iwork-keynote-sffkey", // the draft's own
		"notes.txt":  "text/plain",                         // none in the draft: its name's
		"report.pdf": "application/x-server-pdf",           // files.info's
		"table.csv":  "text/csv",                           // none in files.info: its name's
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.chips) != len(want) {
		t.Fatalf("%d chips, want %d", len(a.chips), len(want))
	}
	for _, c := range a.chips {
		if got := c.pendingLocked().Mimetype; got != want[c.name] {
			t.Errorf("%s: pending file mimetype %q, want %q", c.name, got, want[c.name])
		}
	}
}
