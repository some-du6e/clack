package richtext

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// TestFileTypeLikeSlack: FileType is getFileType (client-boot 2grX) over
// vITP's predicates; expected kinds from 2grX and vITP running in node
// (CDN build 35b42de8b870) over these files. Synthetic files.
func TestFileTypeLikeSlack(t *testing.T) {
	for _, c := range []struct{ file, want string }{
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":" Q3 &amp; Q4 &lt;plan&gt; ","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}`, "generic"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"   ","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}`, "generic"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}`, "generic"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted","initial_comment":{"comment":"c"}}`, "generic"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0002","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}`, "generic"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"image/png","filetype":"png","pretty_type":"PNG","mode":"hosted","thumb_360":"https://files.example.invalid/t360.png"}`, "image"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0002","name":"x.bin","title":"","mimetype":"image/png","filetype":"png","pretty_type":"PNG","mode":"hosted","thumb_360":"https://files.example.invalid/t360.png"}`, "image"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"image/png","filetype":"png","pretty_type":"PNG","mode":"hosted"}`, "generic"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"image/png","filetype":"png","pretty_type":"PNG","mode":"hidden_by_limit","thumb_360":"https://files.example.invalid/t360.png"}`, "generic"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"text/x-python","filetype":"python","pretty_type":"Python","mode":"snippet"}`, "snippet"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"text/x-python","filetype":"python","pretty_type":"Python","mode":"snippet","initial_comment":{"comment":"c"}}`, "snippet"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0002","name":"x.bin","title":"","mimetype":"text/x-python","filetype":"python","pretty_type":"Python","mode":"snippet"}`, "snippet"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"text/x-python","filetype":"python","pretty_type":"","mode":"snippet"}`, "snippet"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"text/x-python","filetype":"python","pretty_type":"C&C","mode":"snippet"}`, "snippet"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"email","pretty_type":"Email","mode":"email"}`, "email"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"email","pretty_type":"Email","mode":"email","initial_comment":{"comment":"c"}}`, "email"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"post","pretty_type":"Post","mode":"post"}`, "post"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"docs","pretty_type":"Doc","mode":"docs"}`, "post"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0002","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"space","pretty_type":"Post","mode":"space"}`, "post"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"quip","pretty_type":"Canvas","mode":"quip"}`, "quip"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"quip","pretty_type":"Canvas","mode":"post"}`, "quip"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"audio/mpeg","filetype":"mp3","pretty_type":"MP3","mode":"snippet"}`, "audio"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"video/mp4","filetype":"mp4","pretty_type":"MPEG 4 Video","mode":"hosted"}`, "video"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"list","pretty_type":"List","mode":"list"}`, "list"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"Report","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted"}`, "generic"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"Audio clip","mimetype":"audio/mp4","filetype":"m4a","pretty_type":"Binary","mode":"hosted","subtype":"slack_audio"}`, "audio"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"Video clip","mimetype":"video/mp4","filetype":"mp4","pretty_type":"Binary","mode":"hosted","subtype":"slack_video"}`, "video"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"audio/mp4","filetype":"m4a","pretty_type":"Binary","mode":"hosted","subtype":"slack_audio"}`, "audio"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"video/mp4","filetype":"mp4","pretty_type":"Binary","mode":"hosted","subtype":"slack_video"}`, "video"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted","subtype":"slack_audio"}`, "generic"},
		{`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted","subtype":"slack_video"}`, "video"},
	} {
		var f slack.File
		if err := json.Unmarshal([]byte(c.file), &f); err != nil {
			t.Fatalf("%s: %v", c.file, err)
		}
		if got := fileTypeOf(&f); got != c.want {
			t.Errorf("%s: %q, Slack %q", c.file, got, c.want)
		}
	}
}

// TestNoteFilesOf: a notification reads the first file's title, uploader,
// pretty_type, kind and whether it carries an initial_comment (any value
// JavaScript reads as true), and the clips among all the files (the first
// clip's subtype picks audio); no files, or an empty list, is none.
func TestNoteFilesOf(t *testing.T) {
	files := func(s string) []slack.File {
		var fs []slack.File
		if err := json.Unmarshal([]byte(s), &fs); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		return fs
	}
	if n := noteFilesOf(nil); n != nil {
		t.Errorf("no files: %+v, want nil", n)
	}
	if n := noteFilesOf(files(`[]`)); n != nil {
		t.Errorf("an empty list: %+v, want nil", n)
	}
	n := noteFilesOf(files(`[{"id":"F1","title":"Plan","user":"U2","pretty_type":"Python","mode":"snippet","initial_comment":{"comment":"c"}},` +
		`{"id":"F2","subtype":"slack_video"},{"id":"F3","subtype":"slack_audio"}]`))
	want := NoteFiles{Title: "Plan", User: "U2", PrettyType: "Python", Kind: "snippet", Commented: true, Clips: 2, AudioClip: false}
	if n == nil || *n != want {
		t.Errorf("files: %+v, want %+v", n, want)
	}
	for _, c := range []struct {
		comment string
		want    bool
	}{{`null`, false}, {`false`, false}, {`""`, false}, {`0`, false}, {`{}`, true}, {`"c"`, true}, {`1`, true}} {
		n := noteFilesOf(files(`[{"id":"F1","initial_comment":` + c.comment + `}]`))
		if n == nil || n.Commented != c.want {
			t.Errorf("initial_comment %s: %+v, want commented %v", c.comment, n, c.want)
		}
	}
	if n := noteFilesOf(files(`[{"id":"F1"},{"id":"F2","subtype":"slack_audio"}]`)); n == nil || n.Clips != 1 || !n.AudioClip {
		t.Errorf("a file and an audio clip: %+v, want one audio clip", n)
	}
}

// TestNoteFilesOfStoreCopy: with the file store's copy of the first file
// (Eley's getFileById), the title, uploader, pretty_type, kind and
// comment come from it, the clips from the message's own files (Qre6
// reads t.files).
func TestNoteFilesOfStoreCopy(t *testing.T) {
	var fs []slack.File
	if err := json.Unmarshal([]byte(`[{"id":"F1","file_access":"check_file_info","user":"U1"},{"id":"F2","subtype":"slack_audio"}]`), &fs); err != nil {
		t.Fatal(err)
	}
	var held slack.File
	if err := json.Unmarshal([]byte(`{"id":"F1","title":"Q3 report","user":"U2","pretty_type":"PNG","mimetype":"image/png","filetype":"png","mode":"hosted",`+
		`"url_private":"https://files.example.invalid/f","thumb_360":"https://files.example.invalid/t360.png","initial_comment":{"comment":"c"},"subtype":"slack_video"}`), &held); err != nil {
		t.Fatal(err)
	}
	want := NoteFiles{Title: "Q3 report", User: "U2", PrettyType: "PNG", Kind: "image", Commented: true, Clips: 1, AudioClip: true}
	if n := noteFilesFrom(fs, &held); n == nil || *n != want {
		t.Errorf("the store's copy: %+v, want %+v", n, want)
	}
	if n := noteFilesFrom(fs, nil); n == nil || n.Title != "" || n.User != "U1" || n.Kind != "generic" {
		t.Errorf("the message's own copy: %+v, want its stub's", n)
	}
}

// TestNoteFileTitle: getFileTitle unescapes lodash's five entities and
// trims JavaScript's whitespace (U+FEFF and no-break spaces go, U+0085
// stays: String.prototype.trim in node).
func TestNoteFileTitle(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{" Q3 &amp; Q4 &lt;plan&gt; ", "Q3 & Q4 <plan>"},
		{"&quot;x&quot; &#39;y&#39; &amp;amp;", "\"x\" 'y' &amp;"},
		{"\ufeff a \u0085\u00a0", "a \u0085"},
		{"   ", ""},
	} {
		if got := noteFileTitle(c.in); got != c.want {
			t.Errorf("%q: %q, want %q", c.in, got, c.want)
		}
	}
}
