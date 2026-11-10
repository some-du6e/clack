package core

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/localimg"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

func init() {
	// Slack's waits are long (7.5 s first poll, 5 s degraded refresh):
	// the tests keep their order, not their length.
	UploadPollFirst, UploadPollMax, UploadDegradedRefresh = 20*time.Millisecond, 80*time.Millisecond, 100*time.Millisecond
}

// synthPNG is a synthetic w×h PNG (a gradient; no real content).
func synthPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			im.SetNRGBA(x, y, color.NRGBA{uint8(x * 255 / w), uint8(y * 255 / h), 160, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// docFile is the first file of a message's document, and whether a
// FileStatus line comes before it (and what it says).
func docFile(m *Message) (f *richtext.File, status string, textBefore bool) {
	for _, b := range m.Doc.Blocks {
		switch b.Kind {
		case richtext.FileStatus:
			if len(b.Spans) > 0 {
				status = b.Spans[0].Text
			}
		case richtext.FileBlock:
			return b.File, status, textBefore
		default:
			textBefore = true
		}
	}
	return nil, status, textBefore
}

// A screenshot pasted and sent at once, with no text (Karim's "sending an
// image by itself feels weird"), against fakeslack with the realtime
// socket up. Slack's sequence: the placeholder appears right away with
// the File's own bytes at .5 under "Waiting to upload…" / "Uploading…"
// (appendPlaceholderMessage, PendingFilesAtom); the upload resolves on
// file_created (UploadQueue PROCESSING → files.info); "Sending…" with the
// real file; only then files.share {files, channel, client_msg_id,
// resharing_aware, …} with no blocks and no comment (convertDeltaToBlocks
// gives nothing for an empty delta); the echo replaces the placeholder.
// The row never draws a FILE card and never has a text line above the
// image.
func TestImageOnlySendPlaceholder(t *testing.T) {
	a, fs, log := rtApp(t)
	fs.ProcessDelay = 400 * time.Millisecond
	ch := fs.ChannelIDs()[1]
	if _, err := a.History(context.Background(), ch, ""); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var share url.Values
	processedAtShare := false
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "files.share" {
			id := strings.Split(f.Get("files"), ",")[0]
			up, _ := fs.File(id)
			mu.Lock()
			share, processedAtShare = f, up.Processed
			mu.Unlock()
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetHook(nil) })

	data := synthPNG(t, 640, 400)
	pf := NewPendingFile("image.png", "image.png", data)
	go func() {
		p, err := a.PrepareUpload(context.Background(), UploadFile{Name: "image.png", Title: "image.png", Data: data}, pf.Progress)
		pf.Finish(p, err)
	}()
	done := make(chan error, 1)
	go func() {
		_, err := a.Upload(context.Background(), UploadOpts{Channel: ch, Pending: []*PendingFile{pf}})
		done <- err
	}()

	var cid, local string
	log.wait(t, "the placeholder", func(evs []Event) bool {
		for _, ev := range evs {
			if ev.Type == "message" && ev.Channel == ch && ev.Msg != nil && ev.Msg.Pending {
				cid, local = ev.ClientMsgID, ev.TS
				return true
			}
		}
		return false
	})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	log.wait(t, "the echo replacing the placeholder", func(evs []Event) bool {
		for _, ev := range evs {
			if ev.Type == "sent" && ev.ReplacesTS == local {
				return true
			}
		}
		return false
	})

	var statuses []string
	sawLocal, sawReal := false, false
	var pendBox, realBox [2]int
	for _, ev := range log.all() {
		if ev.Msg == nil || ev.ClientMsgID != cid && ev.TS != local {
			continue
		}
		f, status, textBefore := docFile(ev.Msg)
		if f == nil {
			t.Fatalf("%s event without the file: %+v", ev.Type, ev.Msg.Doc.Blocks)
		}
		if !f.IsImage {
			t.Errorf("%s event drew a FILE card (%+v)", ev.Type, f)
		}
		if textBefore {
			t.Errorf("%s event has a line above the image: %+v", ev.Type, ev.Msg.Doc.Blocks)
		}
		if f.Pending {
			sawLocal = sawLocal || (f.Thumb != nil && localimg.Is(f.Thumb.URL))
			pendBox = [2]int{f.Thumb.W, f.Thumb.H}
		} else if f.Thumb != nil && strings.HasPrefix(f.Thumb.URL, fs.URL) {
			sawReal = true
			realBox = [2]int{f.Thumb.W, f.Thumb.H}
		}
		if ev.Type == "sent" && (ev.Msg.Pending || status != "") {
			t.Errorf("confirmed message still pending: %+v status %q", ev.Msg.Pending, status)
		}
		if status != "" && (len(statuses) == 0 || statuses[len(statuses)-1] != status) {
			statuses = append(statuses, status)
		}
	}
	if !sawLocal || !sawReal {
		t.Errorf("placeholder from the File's bytes %v, then the processed file %v", sawLocal, sawReal)
	}
	if pendBox != realBox || pendBox != [2]int{640, 400} {
		t.Errorf("image size %v while pending, %v once shared: the gallery box would jump", pendBox, realBox)
	}
	if len(statuses) < 2 || (statuses[0] != "Waiting to upload…" && statuses[0] != "Uploading…") ||
		statuses[len(statuses)-1] != "Sending…" {
		t.Errorf("meta line: %q, want Waiting to upload…/Uploading… then Sending…", statuses)
	}

	mu.Lock()
	defer mu.Unlock()
	if !processedAtShare {
		t.Error("files.share went out before Slack processed the upload (the echo would carry a stub)")
	}
	if share.Get("channel") != ch || share.Get("client_msg_id") != cid || share.Has("blocks") || share.Has("comment") ||
		share.Has("thread_ts") || share.Get("resharing_aware") != "true" || share.Get("skip_dlp_user_warning") != "false" ||
		share.Get("from_share_modal") != "false" {
		t.Errorf("files.share args: %v", share)
	}
	if n := fs.CallCount("files.getUploadURL"); n != 1 {
		t.Errorf("uploads = %d, want the one at attach", n)
	}
	if got, ok := a.S.Find(ch, local); ok {
		t.Errorf("placeholder still stored: %+v", got)
	}
	deadline := time.Now().Add(2 * time.Second)
	for localimg.Len() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := localimg.Len(); n != 0 {
		t.Errorf("%d preview(s) still held after the echo", n)
	}
}

// Every upload already processed at send: the placeholder carries the
// files themselves (appendPlaceholderMessage's files, no pendingFiles), so
// there's no meta line, and the echo changes nothing on screen.
func TestImageSendAllUploaded(t *testing.T) {
	a, fs, log := rtApp(t)
	ch := fs.ChannelIDs()[1]
	data := synthPNG(t, 300, 200)
	p, err := a.PrepareUpload(context.Background(), UploadFile{Name: "shot.png", Title: "shot.png", Data: data}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.File == nil || fileStub(p.File) || p.File.OriginalW != 300 {
		t.Fatalf("PrepareUpload didn't wait for processing: %+v", p.File)
	}
	if _, err := a.Upload(context.Background(), UploadOpts{Channel: ch, Prepared: []PreparedUpload{p}}); err != nil {
		t.Fatal(err)
	}
	var local string
	log.wait(t, "placeholder and echo", func(evs []Event) bool {
		for _, ev := range evs {
			if ev.Type == "message" && ev.Msg != nil && ev.Msg.Pending {
				local = ev.TS
			}
			if ev.Type == "sent" && local != "" && ev.ReplacesTS == local {
				return true
			}
		}
		return false
	})
	var thumbs []string
	for _, ev := range log.all() {
		if ev.Msg == nil || (ev.TS != local && ev.ReplacesTS != local) {
			continue
		}
		f, status, _ := docFile(ev.Msg)
		if f == nil || !f.IsImage || f.Pending || status != "" {
			t.Fatalf("%s: file %+v status %q", ev.Type, f, status)
		}
		thumbs = append(thumbs, f.Thumb.URL)
	}
	if len(thumbs) < 2 || thumbs[0] != thumbs[len(thumbs)-1] {
		t.Errorf("thumbs across the swap: %q (want the same image)", thumbs)
	}
}

// A failed share removes the placeholder (cleanUpPlaceholder) and drops
// its previews; Resend isn't offered for it.
func TestFileSendFailureRemovesPlaceholder(t *testing.T) {
	a, fs, log := rtApp(t)
	ch := fs.ChannelIDs()[1]
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "files.share" {
			return map[string]any{"ok": false, "error": "not_in_channel"}, true
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetHook(nil) })
	_, err := a.Upload(context.Background(), UploadOpts{Channel: ch, Files: []UploadFile{{Name: "x.png", Data: synthPNG(t, 40, 30)}}})
	if err == nil {
		t.Fatal("share error swallowed")
	}
	var local, cid string
	for _, ev := range log.all() {
		if ev.Type == "message" && ev.Msg != nil && ev.Msg.Pending {
			local, cid = ev.TS, ev.ClientMsgID
		}
	}
	if local == "" {
		t.Fatal("no placeholder")
	}
	log.wait(t, "the placeholder removed", func(evs []Event) bool {
		for _, ev := range evs {
			if ev.Type == "delete" && ev.TS == local {
				return true
			}
		}
		return false
	})
	if _, ok := a.S.Find(ch, local); ok {
		t.Error("placeholder kept after a failed share")
	}
	if _, err := a.Resend(context.Background(), ch, cid); err == nil {
		t.Error("Resend offered for a file placeholder")
	}
	if n := localimg.Len(); n != 0 {
		t.Errorf("%d preview(s) held after the failure", n)
	}
}

// Without a realtime connection the upload is polled for
// (pollForFileProcessing) until files.info has the processed file.
func TestPrepareUploadPollsOffline(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	fs.ProcessDelay = 150 * time.Millisecond
	a := New(slack.New(fs.Creds()), store.New(""))
	a.ServeOffline()
	t0 := time.Now()
	p, err := a.PrepareUpload(context.Background(), UploadFile{Name: "p.png", Title: "p.png", Data: synthPNG(t, 50, 20)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(t0) < 150*time.Millisecond || p.File == nil || p.File.Mimetype != "image/png" || p.File.OriginalW != 50 {
		t.Errorf("after %s: %+v", time.Since(t0), p.File)
	}
	if n := fs.CallCount("files.info"); n < 2 {
		t.Errorf("files.info polls = %d", n)
	}
}

// The placeholder's meta line follows PendingFilesMetaTextWrapper.
func TestPlaceholderStatus(t *testing.T) {
	slot := func(pct int, done bool) *phSlot {
		return &phSlot{file: slack.File{UploadPercent: pct}, done: done}
	}
	for _, c := range []struct {
		started bool
		slots   []*phSlot
		want    string
	}{
		{false, []*phSlot{slot(0, true)}, ""},
		{true, []*phSlot{slot(0, false)}, "Waiting to upload…"},
		{true, []*phSlot{slot(40, false)}, "Uploading…"},
		{true, []*phSlot{slot(100, false)}, "Uploading…"},
		{true, []*phSlot{slot(0, true), slot(0, false)}, "Uploading…"},
		{true, []*phSlot{slot(0, true), slot(0, true)}, "Sending…"},
	} {
		ph := &filePlaceholder{started: c.started, slots: c.slots}
		if got := ph.statusLocked(); got != c.want {
			t.Errorf("%+v: %q, want %q", c, got, c.want)
		}
	}
}
