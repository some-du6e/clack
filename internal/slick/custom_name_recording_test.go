package slick

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/media/mediatest"
)

// TestCustomNameRecordingLikeSlack: Slick's CustomNameRecording, built
// from Slick's source, in Edit profile's Name recording field: Slack's
// EditAudioButton with the props BaseEditProfileField gives it
// (onChangeFile, onFileUploadStart, onFileUploadEnd). Beside the field's
// own control the plugin renders its outline Upload audio button; clicked,
// it opens a chooser for MP3s (accept "audio/mpeg,.mp3"), reads the file
// with Web Audio's decodeAudioData and uploads it as Slack's recorder
// would name it (audio_name_pronunciation.mp3, subtype
// slack_name_pronunciation), the upload between onFileUploadStart and
// onFileUploadEnd and the new file's id handed to the field
// (onChangeFile). A file that isn't an MP3 gets its "Choose an MP3 file"
// in an error InlineAlert, and nothing uploads.
func TestCustomNameRecordingLikeSlack(t *testing.T) {
	dir := pluginsDir(t)
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	h := newBootedHarness(t, fs)
	h.importFile(filepath.Join(dir, "CustomNameRecording.js"))
	if err := h.s.SetEnabled(h.ctx(), "CustomNameRecording", true); err != nil {
		t.Fatal(err)
	}
	if info := h.plugin("CustomNameRecording"); !info.Running || info.StartError != "" {
		t.Fatalf("CustomNameRecording didn't start: %+v", info)
	}
	const changeFn, startFn, endFn = 525201, 525202, 525203
	props := map[string]any{
		"onChangeFile":      map[string]any{"$gofn": changeFn},
		"onFileUploadStart": map[string]any{"$gofn": startFn},
		"onFileUploadEnd":   map[string]any{"$gofn": endFn},
	}
	h.render(1, "EditAudioButton", props)
	button := func(nodes []any) map[string]any { return withClass(nodes, "slick-cnr__upload") }
	nodes := h.waitTree(t, 1, "the Upload audio button", func(n []any) bool { return button(n) != nil })
	b := button(nodes)
	bp, _ := b["p"].(map[string]any)
	if b["n"] != "Button" || bp["type"] != "outline" || bp["size"] != "medium" || !strings.Contains(treeText([]any{b}), "Upload audio") {
		t.Errorf("the button: %v, want an outline medium Button saying Upload audio", b)
	}
	if findNative(nodes, "EditAudioButton") == nil {
		t.Error("the field's own control (<Original/>) isn't drawn beside it")
	}

	uploads := len(fs.Uploads())
	h.willPick(jsrt.PickedFile{Name: "tones.mp3", Type: "audio/mpeg", Data: mediatest.TonesMP3, LastModified: time.Unix(1700000000, 0)})
	h.callFn(bp["onClick"], "[]")
	eventually(t, 15*time.Second, "the field given the upload's id", func() bool { return len(h.ui.argsOf(changeFn)) > 0 })
	if got := h.choosers(); len(got) != 1 || got[0] != "audio/mpeg,.mp3" {
		t.Errorf("the choosers opened: %q, want one for audio/mpeg,.mp3", got)
	}
	eventually(t, 5*time.Second, "the upload's end", func() bool { return h.ui.called(endFn) > 0 })
	if order := h.ui.callOrder(startFn, changeFn, endFn); !slices.Equal(order, []int64{startFn, changeFn, endFn}) {
		t.Errorf("the field's callbacks in order %v, want onFileUploadStart, onChangeFile, onFileUploadEnd once each", order)
	}
	var id string
	if args := h.ui.argsOf(changeFn); len(args) == 1 {
		var a []any
		if err := json.Unmarshal([]byte(args[0]), &a); err == nil && len(a) == 1 {
			id, _ = a[0].(string)
		}
	}
	all := fs.Uploads()
	if len(all) != uploads+1 || id == "" || all[len(all)-1].ID != id {
		t.Fatalf("onChangeFile got %q; the server has %d new uploads (want the one it names)", h.ui.argsOf(changeFn), len(all)-uploads)
	}
	u := all[len(all)-1]
	if u.Subtype != "slack_name_pronunciation" || u.CompleteSubtype != "slack_name_pronunciation" || u.URLArgs.Get("filename") != "audio_name_pronunciation.mp3" ||
		u.URLArgs.Get("length") != strconv.Itoa(len(mediatest.TonesMP3)) || u.Bytes != len(mediatest.TonesMP3) {
		t.Errorf("the upload: %s, subtype %q/%q, length %s, %d bytes; want the MP3 as audio_name_pronunciation.mp3, slack_name_pronunciation",
			u.URLArgs.Get("filename"), u.Subtype, u.CompleteSubtype, u.URLArgs.Get("length"), u.Bytes)
	}

	// a WAV: the plugin's own check stops it before any upload
	h.willPick(jsrt.PickedFile{Name: "tones.wav", Type: "audio/wav", Data: mediatest.TonesWAV})
	h.callFn(bp["onClick"], "[]")
	nodes = h.waitTree(t, 1, "the error", func(n []any) bool { return withClass(n, "slick-cnr__error") != nil })
	alert := withClass(nodes, "slick-cnr__error")
	ap, _ := alert["p"].(map[string]any)
	if alert["n"] != "InlineAlert" || !strings.Contains(" "+toString(ap["className"])+" ", " c-inline_alert--level_error ") || treeText([]any{alert}) != "Choose an MP3 file" {
		t.Errorf("the error: %v, want an error InlineAlert saying Choose an MP3 file", alert)
	}
	if n := len(fs.Uploads()); n != uploads+1 {
		t.Errorf("%d more uploads after the WAV, want none", n-uploads-1)
	}
}

// withClass is the first node of a host tree whose className has cls.
func withClass(nodes []any, cls string) map[string]any {
	for _, x := range nodes {
		m, ok := x.(map[string]any)
		if !ok {
			continue
		}
		if p, _ := m["p"].(map[string]any); strings.Contains(" "+toString(p["className"])+" ", " "+cls+" ") {
			return m
		}
		if kids, ok := m["c"].([]any); ok {
			if f := withClass(kids, cls); f != nil {
				return f
			}
		}
	}
	return nil
}

func toString(v any) string { s, _ := v.(string); return s }
