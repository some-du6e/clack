package core

import (
	"context"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
)

// A pasted screenshot's message event often beats Slack's processing of
// the upload: its file has a name and url_private but no mimetype,
// filetype or thumbs (media_display_type "unknown"), which draws as a
// generic "FILE image.png" card. Core completes such files with
// files.info, refetches them on file_change (Slack's filesChanged) and
// doesn't let a message_changed copy put the stub back.

const stubFile = "F0STUBIMG01"

// unprocessed is the file object of an upload Slack hasn't processed.
func unprocessed() map[string]any {
	return map[string]any{"id": stubFile, "created": 1794242958, "timestamp": 1794242958, "name": "image.png", "title": "image.png",
		"mimetype": "", "filetype": "", "pretty_type": "", "user": "U0FAKE00001", "editable": false, "size": 0, "mode": "hosted",
		"is_external": false, "external_type": "", "is_public": true, "public_url_shared": false, "display_as_bot": false,
		"url_private":          "https://files.slack.com/files-pri/T0FAKE-" + stubFile + "/image.png",
		"url_private_download": "https://files.slack.com/files-pri/T0FAKE-" + stubFile + "/download/image.png",
		"media_display_type":   "unknown", "permalink": "https://fake.slack.com/files/U0FAKE00001/" + stubFile + "/image.png",
		"has_rich_preview": false, "file_access": "visible"}
}

// processedImage is the same file once Slack is done: a 223px paste with
// thumbs up to thumb_360 only.
func processedImage() map[string]any {
	f := unprocessed()
	th := func(n string) string {
		return "https://files.slack.com/files-tmb/T0FAKE-" + stubFile + "-x/image_" + n + ".png"
	}
	for k, v := range map[string]any{"mimetype": "image/png", "filetype": "png", "pretty_type": "PNG", "size": 21403,
		"media_display_type": "unknown", "is_modified_by_ai": false, "original_w": 223, "original_h": 150,
		"thumb_64": th("64"), "thumb_80": th("80"), "thumb_160": th("160"), "thumb_360": th("360"), "thumb_360_w": 223, "thumb_360_h": 150,
		"thumb_tiny": "AwAeADDS"} {
		f[k] = v
	}
	return f
}

// stubApp is rtApp with files.info answering the stub until processed.
func stubApp(t *testing.T) (*App, func(ev map[string]any), *atomic.Bool, *atomic.Int32, *eventLog, string) {
	t.Helper()
	a, fs, log := rtApp(t)
	ch := fs.ChannelIDs()[1]
	if _, err := a.History(context.Background(), ch, ""); err != nil {
		t.Fatal(err)
	}
	var done atomic.Bool
	var infos atomic.Int32
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method != "files.info" || f.Get("file") != stubFile {
			return nil, false
		}
		infos.Add(1)
		file := unprocessed()
		if done.Load() {
			file = processedImage()
		}
		return map[string]any{"ok": true, "file": file}, true
	})
	t.Cleanup(func() { fs.SetHook(nil) })
	push := func(ev map[string]any) { fs.Push(ev) }
	return a, push, &done, &infos, log, ch
}

func stubMessage(ch, ts string) map[string]any {
	return map[string]any{"type": "message", "subtype": "file_share", "channel": ch, "ts": ts, "event_ts": ts, "user": "U0FAKE00001",
		"text": "this too", "upload": true, "files": []any{unprocessed()}}
}

// fileIn is the file block of ch/ts as the views get it.
func fileIn(a *App, ch, ts string) *richtext.File {
	m, ok := a.S.Find(ch, ts)
	if !ok {
		return nil
	}
	v := a.MessageFromSlack(ch, &m)
	for _, b := range v.Doc.Blocks {
		if b.Kind == richtext.FileBlock {
			return b.File
		}
	}
	return nil
}

func waitImage(t *testing.T, a *App, log *eventLog, ch, ts, what string) {
	t.Helper()
	log.wait(t, what, func(evs []Event) bool {
		for _, ev := range evs {
			if ev.Type == "update" && ev.TS == ts && ev.Msg != nil {
				for _, b := range ev.Msg.Doc.Blocks {
					if b.Kind == richtext.FileBlock && b.File != nil && b.File.IsImage && b.File.Thumb != nil {
						return true
					}
				}
			}
		}
		return false
	})
	if f := fileIn(a, ch, ts); f == nil || !f.IsImage || f.Thumb == nil || f.Type != "png" {
		t.Fatalf("stored file %+v isn't the processed image", f)
	}
}

var fillDelaysMu sync.Mutex

func shortFill(t *testing.T, d ...time.Duration) {
	fillDelaysMu.Lock()
	prev := fileFillDelays
	fileFillDelays = d
	t.Cleanup(func() { fileFillDelays = prev; fillDelaysMu.Unlock() })
}

func TestUnprocessedUploadIsFilled(t *testing.T) {
	shortFill(t, 50*time.Millisecond, 100*time.Millisecond, 200*time.Millisecond, 400*time.Millisecond)
	a, push, done, infos, log, ch := stubApp(t)
	const ts = "1794242958.307289"
	push(stubMessage(ch, ts))
	log.wait(t, "the message", func(evs []Event) bool {
		for _, ev := range evs {
			if ev.Type == "message" && ev.TS == ts {
				return true
			}
		}
		return false
	})
	if f := fileIn(a, ch, ts); f == nil || f.IsImage {
		t.Fatalf("the unprocessed file %+v should start as a file card", f)
	}
	// Slack is still processing for the first files.info
	dl := time.Now().Add(5 * time.Second)
	for infos.Load() == 0 && time.Now().Before(dl) {
		time.Sleep(5 * time.Millisecond)
	}
	if infos.Load() == 0 {
		t.Fatal("no files.info for a file with no mimetype or filetype (url_private set)")
	}
	done.Store(true)
	waitImage(t, a, log, ch, ts, "the processed image")
}

func TestFileChangeRefetchesFile(t *testing.T) {
	shortFill(t, 20*time.Millisecond)
	a, push, done, infos, log, ch := stubApp(t)
	const ts = "1794242990.000100"
	push(stubMessage(ch, ts))
	dl := time.Now().Add(5 * time.Second)
	for infos.Load() == 0 && time.Now().Before(dl) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond) // fillFiles gave up: still processing
	if f := fileIn(a, ch, ts); f == nil || f.IsImage {
		t.Fatalf("file %+v", f)
	}
	done.Store(true)
	push(map[string]any{"type": "file_change", "file_id": stubFile, "user_id": "U0FAKE00001", "file": map[string]any{"id": stubFile},
		"event_ts": "1794242995.000200"})
	waitImage(t, a, log, ch, ts, "the file_change refetch")

	// a message_changed (an edit) carrying the stub again keeps the image
	n := infos.Load()
	done.Store(false)
	edited := stubMessage(ch, ts)
	edited["text"] = "this too (edited)"
	edited["edited"] = map[string]any{"user": "U0FAKE00001", "ts": "1794243000.000000"}
	delete(edited, "channel")
	push(map[string]any{"type": "message", "subtype": "message_changed", "hidden": true, "channel": ch, "event_ts": "1794243000.000100",
		"message": edited})
	log.wait(t, "the edit", func(evs []Event) bool {
		m, ok := a.S.Find(ch, ts)
		return ok && m.Text == "this too (edited)"
	})
	if f := fileIn(a, ch, ts); f == nil || !f.IsImage || f.Thumb == nil {
		t.Errorf("message_changed put the stub back: %+v", f)
	}
	time.Sleep(100 * time.Millisecond)
	if infos.Load() != n {
		t.Errorf("files.info refetched a file the store already had (%d → %d calls)", n, infos.Load())
	}
}
