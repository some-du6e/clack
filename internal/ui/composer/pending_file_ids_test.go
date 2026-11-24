package composer

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// A file not uploaded yet when the message goes is in
// prepareAndSendMessage's pendingFileIds, by the id plugins know it by
// (state.pendingFileUploads), as Slack's sendDraftMessage passes the
// draft's unsent pending files beside its file_ids. Only uploaded files'
// ids were there, so a plugin that leaves sends with files to Slack
// (Karim's Block Kit Send) took this one over, and the file was never
// shared.
func TestSendArgsCarryPendingFileIDs(t *testing.T) {
	c, fs, _ := composerEnv(t, nil)
	// the file's upload never finishes: Slack refuses its upload URL
	fs.mu.Lock()
	fs.reply = map[string]any{"files.getUploadURL": map[string]any{"ok": false, "error": "fatal_error"}}
	fs.mu.Unlock()
	tt := pluginTaut(t, c.api.a, `start() {
    globalThis.__sendFiles = () => globalThis.__seenFiles ?? null
    this.off = this.api.redux.patchThunk('prepareAndSendMessage', (orig) => (args, ...rest) => {
      globalThis.__seenFiles = { fileIds: args.fileIds ?? [], pendingFileIds: args.pendingFileIds ?? [] }
      return () => undefined // takes the send over: nothing goes out
    })
  }
  stop() { this.off?.() }`)
	waitFor(t, 10*time.Second, "the plugin's prepareAndSendMessage patch", func() bool { return tt.ThunkPatched("prepareAndSendMessage") })
	rt, err := tt.JS()
	if err != nil || rt == nil {
		t.Fatalf("no JS runtime: %v", err)
	}

	c.att.Add("notes.txt", []byte("not uploaded yet"))
	c.att.mu.Lock()
	if len(c.att.chips) != 1 {
		c.att.mu.Unlock()
		t.Fatalf("%d chips attached, want 1", len(c.att.chips))
	}
	want, uploaded := attPendingID(c.att.chips[0].id), c.att.chips[0].prepared != nil
	c.att.mu.Unlock()
	if uploaded {
		t.Fatal("the file's upload finished; the test needs one that hasn't")
	}
	d := NewDoc()
	d.Insert(0, "with a file", Attr{})
	c.ed.SetDoc(d)
	c.submit()

	var seen struct {
		FileIDs        []string `json:"fileIds"`
		PendingFileIDs []string `json:"pendingFileIds"`
	}
	waitFor(t, 10*time.Second, "the patched prepareAndSendMessage", func() bool {
		raw, err := rt.Call(context.Background(), "__sendFiles")
		if err != nil || string(raw) == "null" {
			return false
		}
		return json.Unmarshal(raw, &seen) == nil
	})
	if len(seen.PendingFileIDs) != 1 || seen.PendingFileIDs[0] != want {
		t.Errorf("the send's pendingFileIds are %q, want the file still uploading, %q: a plugin taking the send over can't tell it has a file", seen.PendingFileIDs, want)
	}
	if len(seen.FileIDs) != 0 {
		t.Errorf("the send's fileIds are %q; nothing is uploaded yet", seen.FileIDs)
	}
}
