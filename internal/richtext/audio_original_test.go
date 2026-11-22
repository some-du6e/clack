package richtext

import "testing"

// The review of c38b91b8: an audio file's URL became the aac transcode,
// the player's source, so Download, Open in new tab and the plugins'
// url_private (messages' file objects) fetched the transcode, not the
// upload. URL is the upload's url_private and Download its
// url_private_download, as for every file; the player's source is its
// own field.
func TestAudioFileKeepsItsOriginal(t *testing.T) {
	const (
		orig = "https://files.slack.com/files-pri/T1-F1/standup.mp3"
		dl   = "https://files.slack.com/files-pri/T1-F1/download/standup.mp3"
		aac  = "https://files.slack.com/files-tmb/T1-F1-x/standup_audio.mp4"
	)
	d := doc(t, `{"ts":"1.0","text":"","files":[
		{"id":"F1","name":"standup.mp3","title":"standup.mp3","mimetype":"audio/mpeg","filetype":"mp3","mode":"hosted","size":900000,
		 "url_private":"`+orig+`","url_private_download":"`+dl+`","aac":"`+aac+`","duration_ms":154000}]}`)
	if len(d.Blocks) != 1 || d.Blocks[0].File == nil {
		t.Fatalf("blocks %s", dump(d.Blocks))
	}
	f := d.Blocks[0].File
	if !f.IsAudio {
		t.Fatalf("not an audio file: %+v", f)
	}
	if f.URL != orig || f.Download != dl {
		t.Errorf("URL %q, Download %q: want the upload's %q and %q (the aac %q is only the player's)", f.URL, f.Download, orig, dl, aac)
	}
}
