package core

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/localimg"
)

// Sending text with an image through every way the upload can go wrong
// (fakeslack's faults: a slow POST, failing getUploadURL / POST /
// completeUpload / share, a share whose reply is lost, duplicated
// realtime events). Each ends in exactly one message carrying both the
// text and the image, or in a visible failure: Upload's error (the
// composer then shows FileUploadErrorDialog and puts the text and file
// back), the placeholder removed, nothing posted (never the text alone).
// The upload log tells which step failed and never names the file.
func TestImageSendFaults(t *testing.T) {
	cases := []struct {
		name    string
		method  string
		fault   fakeslack.Fault
		dup     bool
		wantErr bool
	}{
		{name: "slow upload", method: "upload", fault: fakeslack.Fault{Delay: 700 * time.Millisecond}},
		{name: "getUploadURL fails once (UploadQueue retries)", method: "files.getUploadURL", fault: fakeslack.Fault{Status: 503, Times: 1}},
		{name: "getUploadURL keeps failing", method: "files.getUploadURL", fault: fakeslack.Fault{Status: 500}, wantErr: true},
		{name: "POST dropped once", method: "upload", fault: fakeslack.Fault{HangUp: true, Times: 1}},
		{name: "POST keeps failing", method: "upload", fault: fakeslack.Fault{Status: 500}, wantErr: true},
		{name: "completeUpload API error", method: "files.completeUpload", fault: fakeslack.Fault{Error: "file_upload_failed"}, wantErr: true},
		{name: "share API error", method: "files.share", fault: fakeslack.Fault{Error: "internal_error"}, wantErr: true},
		{name: "share network drop", method: "files.share", fault: fakeslack.Fault{HangUp: true}, wantErr: true},
		{name: "share reply lost", method: "files.share", fault: fakeslack.Fault{HangUp: true, AfterHandle: true}},
		{name: "duplicate events", dup: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, fs, evlog := rtApp(t)
			ch := fs.ChannelIDs()[1]
			if _, err := a.History(context.Background(), ch, ""); err != nil {
				t.Fatal(err)
			}
			fs.DuplicateEvents = c.dup
			if c.method != "" {
				fs.SetFault(c.method, c.fault)
			}
			var logBuf bytes.Buffer
			log.SetOutput(&logBuf)
			t.Cleanup(func() { log.SetOutput(os.Stderr) })

			const name = "secret-plan.png" // the log must never carry it
			data := synthPNG(t, 200, 120)
			pf := NewPendingFile(name, name, data)
			go func() {
				p, err := a.PrepareUpload(context.Background(), UploadFile{Name: name, Title: name, Data: data}, pf.Progress)
				pf.Finish(p, err)
			}()
			blocks := json.RawMessage(`[{"type":"rich_text","block_id":"b1","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"look at this"}]}]}]`)
			_, err := a.Upload(context.Background(), UploadOpts{Channel: ch, Comment: "look at this", Blocks: blocks, Pending: []*PendingFile{pf}})
			if (err != nil) != c.wantErr {
				t.Fatalf("Upload error %v, want error %v\nlog:\n%s", err, c.wantErr, logBuf.String())
			}
			evlog.wait(t, "the placeholder", func(evs []Event) bool {
				for _, ev := range evs {
					if ev.Type == "message" && ev.Msg != nil && ev.Msg.Pending {
						return true
					}
				}
				return false
			})
			var cid, local string
			for _, ev := range evlog.all() {
				if ev.Type == "message" && ev.Msg != nil && ev.Msg.Pending {
					cid, local = ev.ClientMsgID, ev.TS
					// one row: the text and the image together from the start
					f, _, textBefore := docFile(ev.Msg)
					if f == nil || !f.IsImage || !textBefore {
						t.Errorf("placeholder %+v: want the text and the image in one row", ev.Msg.Doc.Blocks)
					}
				}
			}
			if cid == "" {
				t.Fatal("no placeholder")
			}
			if n := fs.CallCount("chat.postMessage"); n != 0 {
				t.Errorf("chat.postMessage called %d times: the text went alone", n)
			}
			if c.wantErr {
				evlog.wait(t, "the placeholder removed", func(evs []Event) bool {
					for _, ev := range evs {
						if ev.Type == "delete" && ev.TS == local {
							return true
						}
					}
					return false
				})
				if len(fs.Shared()) != 0 {
					t.Errorf("shared after a failure: %+v", fs.Shared())
				}
				if !strings.Contains(logBuf.String(), "failed") {
					t.Errorf("the log doesn't say what failed:\n%s", logBuf.String())
				}
			} else {
				evlog.wait(t, "the message confirmed", func(evs []Event) bool {
					for _, ev := range evs {
						if ev.Type == "sent" && ev.ReplacesTS == local {
							return true
						}
					}
					return false
				})
				time.Sleep(200 * time.Millisecond) // late duplicates
				if n := len(fs.Shared()); n != 1 {
					t.Errorf("%d shares, want one", n)
				}
				n := 0
				for _, m := range a.S.Timeline(ch).Msgs {
					if m.ClientMsgID != cid {
						continue
					}
					n++
					v := a.message(ch, &m)
					f, _, textBefore := docFile(&v)
					if v.Pending || f == nil || !f.IsImage || f.Pending || !textBefore {
						t.Errorf("stored message %+v: want the confirmed text and image", v.Doc.Blocks)
					}
				}
				if n != 1 {
					t.Errorf("%d messages with the send's client_msg_id, want one", n)
				}
			}
			if strings.Contains(logBuf.String(), name) || strings.Contains(logBuf.String(), "look at this") {
				t.Errorf("the log names the file or the text:\n%s", logBuf.String())
			}
			deadline := time.Now().Add(2 * time.Second)
			for localimg.Len() != 0 && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if localimg.Len() != 0 {
				t.Errorf("%d preview(s) left", localimg.Len())
			}
		})
	}
}
