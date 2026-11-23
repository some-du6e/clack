package core

import (
	"context"
	"encoding/json"
	"mime"
	"strconv"
	"testing"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// Each upload goes up as Slack's client posts it: one FormData part named
// and typed as Chromium names and types the File (slack.FileType: Chromium's
// own lists, never the OS's), application/octet-stream where they have no
// type. A snippet goes up as its Blob, which FormData names "blob", of its
// snippet type's type; a voice clip with the recorder's type (audio/mp4, not
// the audio/x-m4a its .m4a name gives); a File with a type of its own (a
// plugin's) with that type. The part's type is what the client controls,
// and Slack's server files the upload by it: an octet-stream part is
// Binary whatever its name. On α.143 Karim's "BlockKitSend (2).js" went up
// octet-stream and came back filetype binary, mimetype
// application/octet-stream, pretty_type Binary (2026-11-22).
func TestUploadTypesLikeSlack(t *testing.T) {
	a, fs, _ := rtApp(t)
	ctx := context.Background()
	files := map[string]*slack.File{}
	for _, c := range []struct {
		f              UploadFile
		partName, part string // the file part as Slack's client posts it
		snippet, sub   string // files.getUploadURL's snippet_type and subtype
	}{
		{f: UploadFile{Name: "BlockKitSend (2).js"}, partName: "BlockKitSend (2).js", part: "text/javascript"},
		{f: UploadFile{Name: "index.ts"}, partName: "index.ts", part: "application/octet-stream"},
		{f: UploadFile{Name: "main.go"}, partName: "main.go", part: "application/octet-stream"},
		{f: UploadFile{Name: "script.py"}, partName: "script.py", part: "application/octet-stream"},
		{f: UploadFile{Name: "data.json"}, partName: "data.json", part: "application/json"},
		{f: UploadFile{Name: "README.md"}, partName: "README.md", part: "text/markdown"},
		{f: UploadFile{Name: "notes.txt"}, partName: "notes.txt", part: "text/plain"},
		{f: UploadFile{Name: "table.csv"}, partName: "table.csv", part: "text/csv"},
		{f: UploadFile{Name: "page.html"}, partName: "page.html", part: "text/html"},
		{f: UploadFile{Name: "icon.svg"}, partName: "icon.svg", part: "image/svg+xml"},
		{f: UploadFile{Name: "bundle.zip"}, partName: "bundle.zip", part: "application/zip"},
		{f: UploadFile{Name: "report.pdf"}, partName: "report.pdf", part: "application/pdf"},
		{f: UploadFile{Name: "shot.png"}, partName: "shot.png", part: "image/png"},
		{f: UploadFile{Name: "clip.mp4"}, partName: "clip.mp4", part: "video/mp4"},
		{f: UploadFile{Name: "SCRIPT.JS"}, partName: "SCRIPT.JS", part: "text/javascript"},
		{f: UploadFile{Name: "README"}, partName: "README", part: "application/octet-stream"},
		{f: UploadFile{Name: "archive.tar.gz"}, partName: "archive.tar.gz", part: "application/gzip"},
		{f: snippetFile("lint.go", "go", "text/x-go", "package main\n"), partName: "blob", part: "text/x-go", snippet: "go"},
		{f: snippetFile("Untitled", "auto", "text/plain", "plain words\n"), partName: "blob", part: "text/plain", snippet: "auto"},
		{f: UploadFile{Name: "audio_message.m4a", Type: "audio/mp4", Subtype: "slack_audio"}, partName: "audio_message.m4a", part: "audio/mp4", sub: "slack_audio"},
		{f: UploadFile{Name: "export.dat", Type: "text/x-custom"}, partName: "export.dat", part: "text/x-custom"},
		{f: UploadFile{Name: "anon.txt", Type: "application/octet-stream"}, partName: "anon.txt", part: "application/octet-stream"},
	} {
		f, name := c.f, c.f.Name
		if f.Data == nil {
			f.Data = []byte("synthetic " + name + "\n")
			if name == "shot.png" {
				f.Data = synthPNG(t, 4, 4)
			}
		}
		f.Title = name
		p, err := a.PrepareUpload(ctx, f, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		up, ok := fs.File(p.ID)
		if !ok {
			t.Fatalf("%s: no upload %s in the fake", name, p.ID)
		}
		if up.PartName != c.partName || up.PartType != c.part {
			t.Errorf("%s: the file part %q went up typed %q; Slack's client posts %q typed %q", name, up.PartName, up.PartType, c.partName, c.part)
		}
		if mt, params, err := mime.ParseMediaType(up.PostType); err != nil || mt != "multipart/form-data" || params["boundary"] == "" {
			t.Errorf("%s: the upload POST's Content-Type %q, want multipart/form-data with a boundary", name, up.PostType)
		}
		if got := up.URLArgs.Get("filename"); got != name {
			t.Errorf("%s: files.getUploadURL filename %q", name, got)
		}
		if got := up.URLArgs.Get("length"); got != strconv.Itoa(len(f.Data)) {
			t.Errorf("%s: files.getUploadURL length %q, want %d", name, got, len(f.Data))
		}
		if got, sub := up.URLArgs.Get("snippet_type"), up.URLArgs.Get("subtype"); got != c.snippet || sub != c.sub {
			t.Errorf("%s: files.getUploadURL snippet_type %q, subtype %q; want %q, %q", name, got, sub, c.snippet, c.sub)
		}
		var refs []struct{ ID, Title, Subtype string }
		if err := json.Unmarshal([]byte(up.CompleteArgs.Get("files")), &refs); err != nil || len(refs) != 1 || refs[0].ID != p.ID || refs[0].Subtype != c.sub {
			t.Errorf("%s: files.completeUpload files %q (%v), want this upload's id and subtype %q", name, up.CompleteArgs.Get("files"), err, c.sub)
		}
		files[name] = p.File
	}
	// and how the fake, filing an upload by its part as Slack's server
	// does, gives it back, and what the message draws for it: a JavaScript
	// file's code icon, not the Binary tile Karim saw
	for _, c := range []struct{ name, filetype, mimetype, pretty, icon string }{
		{"BlockKitSend (2).js", "javascript", "text/javascript", "JavaScript", "code"},
		{"SCRIPT.JS", "javascript", "text/javascript", "JavaScript", "code"},
		{"README.md", "markdown", "text/markdown", "Markdown (raw)", "code"},
		{"notes.txt", "text", "text/plain", "Plain Text", "text-snippet"},
		{"main.go", "binary", "application/octet-stream", "Binary", "binary"},
		{"README", "binary", "application/octet-stream", "Binary", "binary"},
		{"lint.go", "go", "text/x-go", "Go", "code"},
	} {
		f := files[c.name]
		if f == nil {
			t.Errorf("%s: no file object after processing", c.name)
			continue
		}
		if f.Filetype != c.filetype || f.Mimetype != c.mimetype || f.PrettyType != c.pretty {
			t.Errorf("%s: filed as %q, %q, %q; want %q, %q, %q", c.name, f.Filetype, f.Mimetype, f.PrettyType, c.filetype, c.mimetype, c.pretty)
		}
		if got := richtext.FileTypeIcon(f.Filetype, f.Mode, ""); got != c.icon {
			t.Errorf("%s: drawn with the %q icon, want %q", c.name, got, c.icon)
		}
	}
}

// UploadFile.FileType is the File's type as Slack's client holds it: its
// own (a clip's, a plugin File's), a snippet Blob's text/plain, else the
// type Chromium gives its name ("" where Chromium's lists have none).
func TestUploadFileType(t *testing.T) {
	for _, c := range []struct {
		f    UploadFile
		want string
	}{
		{UploadFile{Name: "a.js"}, "text/javascript"},
		{UploadFile{Name: "a.go"}, ""},
		{UploadFile{Name: "audio_message.m4a"}, "audio/x-m4a"},
		{UploadFile{Name: "audio_message.m4a", Type: "audio/mp4", Subtype: "slack_audio"}, "audio/mp4"},
		{UploadFile{Name: "lint.go", SnippetType: "go"}, "text/plain"},
		{UploadFile{Name: "lint.go", SnippetType: "go", Type: "text/x-go"}, "text/x-go"},
	} {
		if got := c.f.FileType(); got != c.want {
			t.Errorf("%+v: FileType() = %q, want %q", c.f, got, c.want)
		}
	}
}
