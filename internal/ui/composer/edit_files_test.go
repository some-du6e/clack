package composer

import (
	"encoding/json"
	"image"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Karim posted "test", edited it and attached an image; Save showed
// "test (edited)" and dropped the image (karim-1113c). Slack's edit
// composer (cjbg) takes files: onFilesSelected → messageEditUploadNewFiles
// uploads them at once (attachUploadToMessage), and Save
// (prepareAndSaveEditMessage → saveMessageEditWithFiles) waits for the
// uploads, then commitMessageEdit sends chat.update with file_ids: the
// files the message had, then the new ones (getFormData joins them with
// commas). An edit whose files didn't change sends no file_ids.
func TestEditAttachesFiles(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	var mu sync.Mutex
	var updates []url.Values
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		if method != "chat.update" {
			return nil, false
		}
		mu.Lock()
		updates = append(updates, form)
		mu.Unlock()
		return map[string]any{"ok": true, "channel": form.Get("channel"), "ts": form.Get("ts")}, true
	})
	t.Cleanup(func() { fs.SetHook(nil) })
	app := core.New(slack.New(fs.Creds()), store.New(""))
	app.ServeOffline()
	stubTyping(t)
	h := &slickHost{app: app, th: theme.New(false)}
	ch := fs.ChannelIDs()[2]

	ops := new(op.Ops)
	frame := func(c *Composer) {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Constraints{Max: image.Pt(900, 600)}, Now: time.Now()}
		slackc.Frame(gtx)
		c.Layout(gtx)
	}
	update := func(n int) url.Values {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			if len(updates) >= n {
				u := updates[n-1]
				mu.Unlock()
				return u
			}
			mu.Unlock()
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("no chat.update #%d (uploads %d)", n, fs.CallCount("files.getUploadURL"))
		return nil
	}
	msg := func(ts string, files ...string) core.Message {
		m := core.Message{Channel: ch, TS: ts, User: app.Self().ID, Text: "test"}
		m.Doc.Blocks = []richtext.Block{{Kind: richtext.Paragraph, Spans: []richtext.Span{{Kind: richtext.Text, Text: "test"}}}}
		for _, id := range files {
			m.Doc.Blocks = append(m.Doc.Blocks, richtext.Block{Kind: richtext.FileBlock, File: &richtext.File{ID: id, Name: id + ".png"}})
		}
		return m
	}

	// "test" with no file: attach an image in the edit, Save
	c := NewEdit(h, msg("1762700000.000100"), func() {})
	t.Cleanup(c.Close)
	frame(c)
	if !c.TakesEditFiles() {
		t.Fatal("the edit composer refuses files (disable_file_attach_after_send is unset)")
	}
	c.att.Add("image.png", imgSynthPNG(t, 120, 80))
	frame(c)
	c.submit()
	if !c.Done() {
		t.Error("the edit composer stayed open after Save")
	}
	u := update(1)
	if fs.CallCount("files.getUploadURL") != 1 {
		t.Errorf("%d uploads, want the one eager upload", fs.CallCount("files.getUploadURL"))
	}
	ids := u.Get("file_ids")
	if ids == "" || strings.Contains(ids, ",") || !strings.HasPrefix(ids, "F") {
		t.Errorf("file_ids %q, want the new file's id", ids)
	}
	if u.Get("skip_dlp_user_warning") != "false" || u.Get("channel") != ch || u.Get("ts") != "1762700000.000100" {
		t.Errorf("chat.update %v", u)
	}
	var blocks []map[string]any
	if err := json.Unmarshal([]byte(u.Get("blocks")), &blocks); err != nil || len(blocks) == 0 || !strings.Contains(u.Get("blocks"), `"test"`) {
		t.Errorf("blocks %s (%v), want the message's rich_text", u.Get("blocks"), err)
	}

	// a message that already has a file keeps it, first
	c2 := NewEdit(h, msg("1762700000.000200", "F0KEEP"), func() {})
	t.Cleanup(c2.Close)
	frame(c2)
	c2.att.Add("second.png", imgSynthPNG(t, 64, 64))
	frame(c2)
	c2.submit()
	u = update(2)
	if got := strings.Split(u.Get("file_ids"), ","); len(got) != 2 || got[0] != "F0KEEP" || got[1] == "" {
		t.Errorf("file_ids %q, want F0KEEP then the new file", u.Get("file_ids"))
	}

	// a text-only edit: saveMessageEdit, no file_ids
	c3 := NewEdit(h, msg("1762700000.000300", "F0KEEP"), func() {})
	t.Cleanup(c3.Close)
	frame(c3)
	c3.submit()
	u = update(3)
	if _, has := u["file_ids"]; has {
		t.Errorf("a text-only edit sent file_ids %q", u.Get("file_ids"))
	}

	// disable_file_attach_after_send: no + in the edit composer, and drops
	// go to the draft instead
	app.SetTeamPrefs(map[string]json.RawMessage{"disable_file_attach_after_send": json.RawMessage("true")})
	c4 := NewEdit(h, msg("1762700000.000400"), func() {})
	t.Cleanup(c4.Close)
	frame(c4)
	if c4.TakesEditFiles() {
		t.Error("the edit takes files with disable_file_attach_after_send")
	}
	if st := c4.cs; !st.NoEditFiles || c4.ui.itemVisible(crAttach) {
		t.Errorf("NoEditFiles %v, + visible %v; want no + while editing", st.NoEditFiles, c4.ui.itemVisible(crAttach))
	}
	if items := c.att.plusMenuItems(c.plusMenu()); len(items) == 0 || items[len(items)-1].Icon != "laptop" {
		t.Errorf("edit + menu %+v, want Upload from your computer", items)
	} else {
		for _, it := range items {
			if it.Icon == "text-snippet" {
				t.Error("the edit + menu has Text snippet (Slack's has none while editing)")
			}
		}
	}
}
