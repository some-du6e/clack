package composer

import (
	"context"
	"encoding/json"
	"image"
	"net/url"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// A file attached in the composer is part of the draft, like Slack's: the
// server draft carries its id (drafts.create/update file_ids, blocks=[]
// with no text), it survives a channel switch and a restart (restored from
// drafts.list into an empty composer, already uploaded), and sending
// shares it and deletes the draft, keeping the file. Removing the last file
// from an empty composer deletes the draft. All against internal/fakeslack,
// with synthetic files.
func TestDraftKeepsAttachedFiles(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	old := draftSyncDelay
	draftSyncDelay = 30 * time.Millisecond
	t.Cleanup(func() { draftSyncDelay = old })
	// A slow drafts.create, as on a loaded runner: the upload finishing
	// (and the chip's thumbnail) change the draft again while the first
	// create is still out. That must update the draft it makes, not create
	// a second one (this test flaked on exactly that: two files-only drafts).
	fs.SetHook(func(method string, _ url.Values) (any, bool) {
		if method == "drafts.create" {
			time.Sleep(200 * time.Millisecond)
		}
		return nil, false
	})
	ch := fs.ChannelIDs()[0]
	key := draftKey(ch, "")
	forget := func() { // what a restart loses: every in-memory draft
		clearDraft(ch, "")
		serverDraftIDs.Delete(key)
		attStore.Lock()
		delete(attStore.m, key)
		attStore.Unlock()
	}
	forget()
	t.Cleanup(forget)

	newApp := func() *core.App {
		a := core.New(slack.New(fs.Creds()), store.New(""))
		a.ServeOffline()
		return a
	}
	app := newApp()
	h := &slickHost{app: app, th: theme.New(false)}
	open := func(h Host) (*Composer, func()) {
		c := New(h, ch, "")
		ops := new(op.Ops)
		return c, func() {
			ops.Reset()
			gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
				Constraints: layout.Constraints{Max: image.Pt(900, 600)}, Now: time.Now()}
			slackc.Frame(gtx)
			c.Layout(gtx)
		}
	}
	until := func(what string, frame func(), cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			frame()
			if cond() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s (drafts %+v)", what, fs.Drafts())
	}
	live := func() []fakeslack.Draft { return fs.LiveDrafts() }

	// attach: uploaded at once, then a files-only draft
	c, frame := open(h)
	const name = "synthetic-diagram.png"
	c.att.Add(name, []byte("\x89PNG\r\n\x1a\nsynthetic, not a real image"))
	var fileID string
	until("a draft carrying the uploaded file", frame, func() bool {
		ds := live()
		if len(ds) != 1 || len(ds[0].FileIDs) != 1 {
			return false
		}
		fileID = ds[0].FileIDs[0]
		return true
	})
	if f, ok := fs.File(fileID); !ok || f.Name != name || f.Bytes == 0 || !f.Completed || f.Shared || f.Cookie {
		t.Fatalf("the draft's file %s: %+v (want uploaded with no cookie, completed, unshared)", fileID, f)
	}
	if b := string(live()[0].Blocks); b != "[]" {
		t.Errorf("files-only draft blocks = %s, want []", b)
	}
	var dest []map[string]any
	_ = json.Unmarshal(live()[0].Destinations, &dest)
	if len(dest) != 1 || dest[0]["channel_id"] != ch {
		t.Errorf("destinations = %s", live()[0].Destinations)
	}
	if !HasDraft(ch, "") {
		t.Error("HasDraft: a draft with only a file has no pencil")
	}
	draftID := live()[0].ID

	// text joins the same draft, with the file still on it
	c.ed.InsertText("a synthetic caption")
	until("the draft updated with text and the file", frame, func() bool {
		ds := live()
		return len(ds) == 1 && ds[0].ID == draftID && strings.Contains(string(ds[0].Blocks), "a synthetic caption") &&
			len(ds[0].FileIDs) == 1 && ds[0].FileIDs[0] == fileID
	})

	// switching away and back: the same files, no new upload or draft
	c.Close()
	c2, frame2 := open(h)
	frame2()
	if c2.att.Len() != 1 || c2.ed.Doc().Text() != "a synthetic caption" {
		t.Fatalf("after a channel switch: %d files, text %q", c2.att.Len(), c2.ed.Doc().Text())
	}
	if n := fs.CallCount("files.getUploadURL"); n != 1 {
		t.Errorf("uploads = %d, want the one at attach", n)
	}
	c2.Close()

	// restart: nothing in memory; the composer restores the server draft
	forget()
	app2 := newApp()
	if _, err := app2.Drafts(context.Background()); err != nil { // what the shell does after boot
		t.Fatal(err)
	}
	d, ok := app2.DraftFor(ch, "")
	if !ok || len(d.FileInfo) != 1 || d.FileInfo[0].ID != fileID || d.FileInfo[0].Name != name || d.FileInfo[0].Thumb == "" {
		t.Fatalf("DraftFor after restart: %+v %v", d, ok)
	}
	h2 := &slickHost{app: app2, th: theme.New(false)}
	c3, frame3 := open(h2)
	until("the restored draft", frame3, func() bool { return c3.att.Len() == 1 && c3.ed.Doc().Text() == "a synthetic caption" })
	c3.att.mu.Lock()
	chip := c3.att.chips[0]
	got := *chip.prepared
	c3.att.mu.Unlock()
	if got.ID != fileID || chip.name != name || chip.data != nil {
		t.Errorf("restored chip: %+v name %q (want the uploaded file, no bytes)", got, chip.name)
	}
	if c3.serverDraftID() != draftID {
		t.Errorf("restored composer's server draft %q, want %q", c3.serverDraftID(), draftID)
	}
	for i := 0; i < 10; i++ {
		frame3()
	}
	if n := len(fs.Drafts()); n != 1 {
		t.Errorf("restoring created another draft: %d drafts", n)
	}

	// send: the file is shared with the caption, the draft deleted, the file kept
	c3.submit()
	until("the file shared and the draft gone", frame3, func() bool { return len(fs.Shared()) == 1 && len(live()) == 0 })
	sh := fs.Shared()[0]
	// files.share with the composer's rich_text blocks, as Slack sends it
	if sh.Channel != ch || len(sh.IDs) != 1 || sh.IDs[0] != fileID || !strings.Contains(string(sh.Blocks), "a synthetic caption") ||
		sh.Comment != "" || sh.Args.Get("resharing_aware") != "true" || len(sh.ClientMsgID) != 36 {
		t.Errorf("shared %+v", sh)
	}
	for _, dr := range fs.Drafts() {
		if dr.ID == draftID && (!dr.IsDeleted || !dr.SkipFileDeletion) {
			t.Errorf("sent draft: deleted %v, skip_file_deletion %v", dr.IsDeleted, dr.SkipFileDeletion)
		}
	}
	until("the composer emptied", frame3, func() bool { return c3.att.Len() == 0 })
	// core forgets the draft when drafts.delete answers, a moment after the
	// fake has marked it deleted
	until("core forgetting the sent draft", frame3, func() bool { _, ok := app2.DraftFor(ch, ""); return !ok })
	if HasDraft(ch, "") {
		t.Error("HasDraft after sending")
	}
	c3.Close()

	// clearing: removing the only file from an empty composer deletes the draft
	c4, frame4 := open(h2)
	c4.att.Add("synthetic-notes.txt", []byte("synthetic notes"))
	until("a second files-only draft", frame4, func() bool { return len(live()) == 1 && len(live()[0].FileIDs) == 1 })
	c4.att.mu.Lock()
	only := c4.att.chips[0]
	c4.att.mu.Unlock()
	c4.att.remove(only)
	until("the draft deleted with its last file", frame4, func() bool { return len(live()) == 0 })
	c4.Close()
}

// Another device's draft (draft_create on the socket) restores into an
// empty composer; one the viewer already typed in keeps its text.
func TestDraftFromAnotherDevice(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	ch, ch2 := fs.ChannelIDs()[1], fs.ChannelIDs()[2]
	for _, k := range []string{ch, ch2} {
		clearDraft(k, "")
		serverDraftIDs.Delete(draftKey(k, ""))
	}
	app := core.New(slack.New(fs.Creds()), store.New(""))
	app.ServeOffline()
	h := &slickHost{app: app, th: theme.New(false)}
	frameOf := func(c *Composer) func() {
		ops := new(op.Ops)
		return func() {
			ops.Reset()
			gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
				Constraints: layout.Constraints{Max: image.Pt(900, 600)}, Now: time.Now()}
			c.Layout(gtx)
		}
	}
	empty, typed := New(h, ch, ""), New(h, ch2, "")
	t.Cleanup(func() { empty.Close(); typed.Close(); clearDraft(ch, ""); clearDraft(ch2, "") })
	fe, ft := frameOf(empty), frameOf(typed)
	fe()
	typed.ed.InsertText("mine")
	ft()
	for _, x := range []struct{ id, ch, text string }{{"Dr0OTHER001", ch, "from my phone"}, {"Dr0OTHER002", ch2, "theirs"}} {
		raw, _ := json.Marshal(map[string]any{"type": "draft_create", "draft": map[string]any{
			"id": x.id, "last_updated_ts": "1700000000000", "file_ids": []string{"F0SYNTH001"},
			"files":        []any{map[string]any{"id": "F0SYNTH001", "name": "synthetic.pdf", "title": "synthetic.pdf", "size": 1234}},
			"destinations": []any{map[string]any{"channel_id": x.ch}},
			"blocks": []any{map[string]any{"type": "rich_text", "elements": []any{map[string]any{"type": "rich_text_section",
				"elements": []any{map[string]any{"type": "text", "text": x.text}}}}}},
		}})
		app.Inject(slack.Event{Type: "draft_create", Raw: raw})
	}
	deadline := time.Now().Add(5 * time.Second)
	for empty.att.Len() == 0 && time.Now().Before(deadline) {
		fe()
		ft()
		time.Sleep(10 * time.Millisecond)
	}
	if empty.ed.Doc().Text() != "from my phone" || empty.att.Len() != 1 {
		t.Errorf("empty composer: text %q, %d files; want the other device's draft", empty.ed.Doc().Text(), empty.att.Len())
	}
	ft()
	if typed.ed.Doc().Text() != "mine" || typed.att.Len() != 0 {
		t.Errorf("typed composer: text %q, %d files; want what the viewer typed", typed.ed.Doc().Text(), typed.att.Len())
	}
}
