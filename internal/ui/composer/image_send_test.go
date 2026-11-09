package composer

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"testing"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/uipref"
)

func init() {
	// Slack's processing poll starts after 7.5 s and the degraded-mode
	// refresh after 5 s; the tests keep the order, not the waits.
	core.UploadPollFirst, core.UploadPollMax, core.UploadDegradedRefresh = 20*time.Millisecond, 80*time.Millisecond, 100*time.Millisecond
}

// imgSynthPNG is a synthetic w×h PNG (a gradient, no real content).
func imgSynthPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			im.SetNRGBA(x, y, color.NRGBA{uint8(40 + x*180/w), uint8(90 + y*120/h), 200, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// Sending an image with no text, against fakeslack (Karim: "sending an
// image by itself feels, wierd"), step by step as Slack's web client does
// it:
//
//  1. attaching uploads at once (getUploadURL → POST → completeUpload) and
//     the tile's corner spinner runs until Slack has processed the file;
//     the gallery is 12px padding around 62px tiles;
//  2. the send button is enabled with only the file, and Enter sends
//     (Mod+Enter with "Enter starts a new line");
//  3. files.share {files, channel, client_msg_id, resharing_aware: true,
//     skip_dlp_user_warning: false, from_share_modal: false} with no
//     blocks and no comment, after the upload is processed;
//  4. the placeholder appears at once (the File's bytes, "Waiting to
//     upload…"/"Uploading…", then "Sending…") and the shared message
//     replaces it;
//  6. the composer is empty right after Enter and its draft is deleted,
//     keeping the file.
func TestImageOnlySendFlow(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	fs.ProcessDelay = 300 * time.Millisecond
	old := draftSyncDelay
	draftSyncDelay = 30 * time.Millisecond
	t.Cleanup(func() { draftSyncDelay = old })
	t.Cleanup(uipref.Reset)
	ch := fs.ChannelIDs()[2]
	key0 := draftKey(ch, "")
	forget := func() {
		clearDraft(ch, "")
		serverDraftIDs.Delete(key0)
		attStore.Lock()
		delete(attStore.m, key0)
		attStore.Unlock()
	}
	forget()
	t.Cleanup(forget)

	app := core.New(slack.New(fs.Creds()), store.New(""))
	app.ServeOffline()
	evs, stop := app.Subscribe()
	t.Cleanup(stop)
	var log []core.Event
	drain := func() {
		for {
			select {
			case ev := <-evs:
				log = append(log, ev)
			default:
				return
			}
		}
	}
	h := &slickHost{app: app, th: theme.New(false)}
	c := New(h, ch, "")
	t.Cleanup(c.Close)
	ops := new(op.Ops)
	var gtx layout.Context
	frame := func() {
		ops.Reset()
		gtx = layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Constraints{Max: image.Pt(900, 600)}, Now: time.Now()}
		slackc.Frame(gtx)
		c.Layout(gtx)
		drain()
	}
	until := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			frame()
			if cond() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s (shared %+v, drafts %+v)", what, fs.Shared(), fs.LiveDrafts())
	}
	frame()

	// 1. attach (paste, the + menu's picker and drag-drop all end in add)
	c.att.Add("image.png", imgSynthPNG(t, 480, 300))
	frame()
	c.att.mu.Lock()
	chip := c.att.chips[0]
	busy := chip.preparing
	c.att.mu.Unlock()
	if !busy {
		t.Error("the tile isn't uploading right after attach")
	}
	if d := c.att.Layout(gtx); d.Size.Y != 12+attChipH+12 {
		t.Errorf("file gallery height %d, want 12+62+12", d.Size.Y)
	}
	// 2. the send button: enabled with only the file
	if st := c.chromeState(gtx); !st.CanSend {
		t.Error("send button disabled with only a file attached")
	}
	// Slack's eager upload: files.getUploadURL at attach, not at send
	until("the upload at attach", func() bool { return fs.CallCount("files.getUploadURL") == 1 })

	// Enter, before Slack finished processing
	edPress(c.ed, key.NameReturn, 0)
	frame()
	// 6. the composer empties at once
	if c.att.Len() != 0 || !c.ed.Empty() {
		t.Fatalf("after Enter: %d files, text %q (Slack empties the composer at once)", c.att.Len(), c.ed.Doc().Text())
	}
	// 4. the placeholder, right away, from the File's bytes
	until("the placeholder", func() bool {
		for _, ev := range log {
			if ev.Type == "message" && ev.Msg != nil && ev.Msg.Pending {
				return true
			}
		}
		return false
	})
	var local string
	for _, ev := range log {
		if ev.Type == "message" && ev.Msg != nil && ev.Msg.Pending {
			local = ev.TS
			blocks := ev.Msg.Doc.Blocks
			if len(blocks) != 2 || blocks[0].Kind != richtext.FileStatus || blocks[1].Kind != richtext.FileBlock ||
				!blocks[1].File.Pending || !blocks[1].File.IsImage {
				t.Errorf("placeholder doc %+v, want the meta line and the pending image only", blocks)
			}
		}
	}
	// 3. the share, once processed
	until("the share", func() bool { return len(fs.Shared()) == 1 })
	sh := fs.Shared()[0]
	if f, _ := fs.File(sh.IDs[0]); !f.Processed {
		t.Error("shared before Slack processed the upload")
	}
	if sh.Channel != ch || len(sh.Blocks) != 0 || sh.Comment != "" || sh.Args.Has("blocks") || sh.Args.Has("comment") ||
		len(sh.ClientMsgID) != 36 || sh.Args.Get("resharing_aware") != "true" || sh.Args.Get("skip_dlp_user_warning") != "false" ||
		sh.Args.Get("from_share_modal") != "false" {
		t.Errorf("files.share %+v", sh.Args)
	}
	if fs.CallCount("files.getUploadURL") != 1 {
		t.Errorf("the file was uploaded again at send (%d uploads)", fs.CallCount("files.getUploadURL"))
	}
	// without a socket the history refresh confirms it
	until("the shared message replacing the placeholder", func() bool {
		for _, ev := range log {
			if ev.Type == "sent" && ev.ReplacesTS == local {
				return true
			}
		}
		return false
	})
	until("the draft gone", func() bool { return len(fs.LiveDrafts()) == 0 })
	for _, d := range fs.Drafts() {
		if d.IsDeleted && !d.SkipFileDeletion {
			t.Errorf("a draft deleted with its files: %+v", d)
		}
	}
	if HasDraft(ch, "") {
		t.Error("HasDraft after sending")
	}

	// 2. "Enter starts a new line": Mod+Enter sends a file on its own
	uipref.Set(store.Prefs{SendOnModEnter: true})
	c.att.Add("second.png", imgSynthPNG(t, 120, 90))
	until("the second upload", func() bool { return fs.CallCount("files.getUploadURL") == 2 })
	edPress(c.ed, key.NameReturn, key.ModShortcut)
	frame()
	if c.att.Len() != 0 {
		t.Fatal("Mod+Enter didn't send the file")
	}
	until("the second share", func() bool { return len(fs.Shared()) == 2 })
	if sh := fs.Shared()[1]; len(sh.Blocks) != 0 || sh.Comment != "" {
		t.Errorf("second share %+v", sh.Args)
	}
}

// A failed share gives the files back to the composer (Slack restores
// them to the draft) and removes the placeholder.
func TestImageSendFailurePutsFilesBack(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	ch := fs.ChannelIDs()[3]
	forget := func() { clearDraft(ch, ""); serverDraftIDs.Delete(draftKey(ch, "")) }
	forget()
	t.Cleanup(forget)
	fs.SetHook(func(method string, _ url.Values) (any, bool) {
		if method == "files.share" {
			return map[string]any{"ok": false, "error": "restricted_action"}, true
		}
		return nil, false
	})
	app := core.New(slack.New(fs.Creds()), store.New(""))
	app.ServeOffline()
	h := &slickHost{app: app, th: theme.New(false)}
	c := New(h, ch, "")
	t.Cleanup(c.Close)
	ops := new(op.Ops)
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Constraints{Max: image.Pt(900, 600)}, Now: time.Now()}
		c.Layout(gtx)
	}
	c.att.Add("image.png", imgSynthPNG(t, 64, 64))
	c.submit()
	if c.att.Len() != 0 {
		t.Fatal("files stayed in the composer while sending")
	}
	deadline := time.Now().Add(10 * time.Second)
	for c.att.Len() == 0 && time.Now().Before(deadline) {
		frame()
		time.Sleep(10 * time.Millisecond)
	}
	if c.att.Len() != 1 {
		t.Fatalf("after the failed share: %d files back", c.att.Len())
	}
}
