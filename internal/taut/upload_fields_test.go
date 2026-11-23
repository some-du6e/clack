package taut

import (
	"context"
	"slices"
	"testing"
	"time"
)

// TestUploadFileFieldsLikeSlack: a File's own upload fields, as Slack's
// recorder sets them on the File and CustomNameRecording does from
// decodeAudioData (subtype, duration, audio_wave_samples), cross the
// bridge with it both ways: a File Go hands JS comes out a File carrying
// them, and addAndUploadPendingFile's File reaches Go with them. Slack's
// UploadQueue (boot-shared HnCu) sends the subtype with the upload: a name
// recording files.upload uploads names slack_name_pronunciation in
// files.getUploadURL and in completeUpload's files entry.
func TestUploadFileFieldsLikeSlack(t *testing.T) {
	e := newAPICallEnv(t, 5*time.Second)
	type upload struct {
		File File `json:"file"`
	}
	in := File{Name: "audio_name_pronunciation.mp3", Type: "audio/mpeg", Data: []byte("ID3 synthetic")}
	in = withUploadFields(in, "slack_name_pronunciation", 2.5, []float64{12, 40.5, 99})
	var got File
	native := func(ctx context.Context, a upload) (any, error) { got = a.File; return map[string]any{}, nil }
	if _, err := Dispatch(e.ctx(), e.tt, "addAndUploadPendingFile", upload{in}, native); err != nil {
		t.Fatal(err)
	}
	sub, dur, wave := uploadFields(got)
	if got.Name != in.Name || got.Type != in.Type || string(got.Data) != string(in.Data) || sub != "slack_name_pronunciation" ||
		dur != 2.5 || !slices.Equal(wave, []float64{12, 40.5, 99}) {
		t.Errorf("the File across the bridge: %q %q %q, subtype %q, duration %v, audio_wave_samples %v; want its own fields kept",
			got.Name, got.Type, got.Data, sub, dur, wave)
	}

	var id string
	e.jsInto(`const f = Object.assign(new File([new Uint8Array([0x49, 0x44, 0x33, 4, 0, 0, 0, 0, 0, 0])], 'audio_name_pronunciation.mp3', { type: 'audio/mpeg' }),
			{ subtype: 'slack_name_pronunciation', duration: 2.5, audio_wave_samples: [12, 40, 99] })
		return await T.files.upload(f)`, &id)
	if id == "" {
		t.Fatal("files.upload gave no file id")
	}
	for _, u := range e.fs.Uploads() {
		if u.ID != id {
			continue
		}
		if u.Subtype != "slack_name_pronunciation" || u.CompleteSubtype != "slack_name_pronunciation" || u.URLArgs.Get("filename") != "audio_name_pronunciation.mp3" {
			t.Errorf("the upload: subtype %q, completeUpload's %q, filename %q; want the name recording's slack_name_pronunciation in both",
				u.Subtype, u.CompleteSubtype, u.URLArgs.Get("filename"))
		}
		return
	}
	t.Errorf("no upload %s at the server", id)
}
