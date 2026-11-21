package richtext

import (
	"encoding/json"
	"testing"
)

// fileRefRes is the fake resolver with files.info answers.
type fileRefRes struct {
	fakeRes
	refs map[string]CanvasRef
}

func (r fileRefRes) CanvasRef(id string) (CanvasRef, bool) {
	ref, ok := r.refs[id]
	return ref, ok
}

func (fileRefRes) CanvasSection(string, string) (json.RawMessage, bool) { return nil, false }

// An inline rich_text file element is Slack's File slug (boot-shared
// v1Jk via c2gp's rich_text_file remap): getFileSlugAttributes' label and
// icon for the file (the message's own, else files.info), the slug
// disabled unless the file is there to see; the element's text unused.
// The label is getFileDisplayName: the title, the name, the status's
// string ("Deleted file", "Hidden file", "Deleted canvas"…), "Untitled";
// a file not fetched yet is the store's unknown object, a normal
// "Untitled" file slug. A normal file's label is its Name (emoji drawn),
// in the element's style.
func TestRichTextFileSlug(t *testing.T) {
	r := fileRefRes{refs: map[string]CanvasRef{
		"F3":  {Title: "Spec", Status: "normal"},
		"F4":  {Status: "hidden"},
		"F6":  {Status: "deleted"},
		"F10": {Status: "deleted", Kind: "canvas", Filetype: "quip"},
	}}
	for id, ref := range skillRefs() { // F11 a skill canvas, F12 a skill set folder
		r.refs[id] = ref
	}
	el := func(id string) string {
		return `{"type":"file","file_id":"` + id + `","url":"https://x.slack.com/files/U1/` + id + `/f","text":"ignored"}`
	}
	m := msg(t, `{"ts":"1.0","text":"x","files":[
		{"id":"F1","name":"q3.pdf","title":"Q3 report.pdf","filetype":"pdf","mimetype":"application/pdf"},
		{"id":"F2","name":"clip.m4a","title":"","filetype":"m4a","subtype":"slack_audio"},
		{"id":"F7","mode":"tombstone"},
		{"id":"F8","name":"big.mov","title":"","filetype":"mov","mode":"hidden_by_limit"},
		{"id":"F9","name":"launch.png","title":":smile: Launch","filetype":"png","mode":"hosted"}],
		"blocks":[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[
		`+el("F1")+`,{"type":"text","text":" "},`+el("F2")+`,{"type":"text","text":" "},`+el("F3")+`,{"type":"text","text":" "},
		`+el("F4")+`,{"type":"text","text":" "},`+el("F5")+`,{"type":"text","text":" "},`+el("F6")+`,
		{"type":"rich_text_file","file_id":"F3","url":"https://x.slack.com/files/U1/F3/f"},{"type":"file","url":"https://x.test/no-id"},
		`+el("F7")+`,`+el("F8")+`,{"type":"file","file_id":"F9","url":"https://x.slack.com/files/U1/F9/f","style":{"bold":true}},
		`+el("F10")+`,`+el("F11")+`,`+el("F12")+`]}]}]}`)
	d := FromMessage(r, m)
	slug := func(id, icon, text string, off bool) Span {
		s := Span{Kind: Canvas, ID: id, URL: "https://x.slack.com/files/U1/" + id + "/f", Icon: icon, Text: text, Off: off}
		if text != "" {
			s.Parts = &SpanParts{Spans: []Span{tx(text)}}
		}
		return s
	}
	var spans []Span
	for _, b := range d.Blocks {
		if b.Kind == Paragraph {
			spans = b.Spans
		}
	}
	f9 := slug("F9", "image", ":smile: Launch", false)
	f9.Style = Bold
	smile := em("smile")
	smile.Style = Bold
	f9.Parts = &SpanParts{Spans: []Span{smile, {Kind: Text, Text: " Launch", Style: Bold}}}
	eq(t, "file slugs", spans, []Span{
		slug("F1", "pdf-file", "Q3 report.pdf", false), tx(" "),
		slug("F2", "music", "Audio clip", false), tx(" "),
		slug("F3", "file", "Spec", false), tx(" "),
		slug("F4", "eye-closed", "Hidden file", true), tx(" "),
		slug("F5", "file", "Untitled", false), tx(" "), // not fetched yet: the store's unknown object, normal
		slug("F6", "trash", "Deleted file", true),
		slug("F3", "file", "Spec", false),
		slug("F7", "trash", "Deleted file", true), // a tombstone in the message's own files
		slug("F8", "eye-closed", "big.mov", true), // hidden by the storage limit: its name still labels it
		f9,
		slug("F10", "trash", "Deleted canvas", true),
		// F11, a skill canvas, draws nothing
		slug("F12", "book", "skills", false), // a skill set folder
	})
	for _, c := range []struct{ ft, mode, sub, want string }{
		{"mp4", "", "", "video"}, {"go", "", "", "code"}, {"xlsx", "", "", "spreadsheet"}, {"png", "", "", "image"},
		{"zip", "", "", "zip"}, {"quip", "", "", "canvas-content"}, {"", "", "", "file"}, {"mp3", "", "", "music"},
		{"weird", "huddle_transcript", "", "transcript"}, {"folder_skill_set", "", "", "book"}, {"key", "", "", "presentation"},
	} {
		if got := fileTypeIconOf(c.ft, c.mode, c.sub); got != c.want {
			t.Errorf("FileTypeIcon(%q, %q, %q) = %q, want %q", c.ft, c.mode, c.sub, got, c.want)
		}
	}
}
