package core

import (
	"context"
	"encoding/json"
	"mime"
	"strconv"
	"testing"

	"github.com/some-du6e/clack/internal/richtext"
)

// Each upload goes up as Slack's client posts it, and the fake files it as
// Slack's server does (fakeslack's types). The part is typed as Chromium
// types a File of its name (slack.FileType: Chromium's own lists, never the
// OS's), application/octet-stream where they have no type. The server files
// an upload by that type, and an octet-stream part is Binary whatever its
// name. On α.143 Karim's "BlockKitSend (2).js" went up octet-stream and
// came back filetype binary, mimetype application/octet-stream, pretty_type
// Binary (2026-11-22). A snippet goes up as its Blob, which FormData names
// "blob", typed by its snippet type, and files.getUploadURL carries the
// name and snippet_type.
func TestUploadTypesLikeSlack(t *testing.T) {
	a, fs, _ := rtApp(t)
	ctx := context.Background()
	check := func(name string, data []byte, f UploadFile, partName, partType, snippetType, filetype, mimetype, pretty, icon string) {
		t.Helper()
		p, err := a.PrepareUpload(ctx, f, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		up, ok := fs.File(p.ID)
		if !ok {
			t.Fatalf("%s: no upload %s in the fake", name, p.ID)
		}
		if mt, params, err := mime.ParseMediaType(up.PostType); err != nil || mt != "multipart/form-data" || params["boundary"] == "" {
			t.Errorf("%s: the upload POST's Content-Type %q, want multipart/form-data with a boundary", name, up.PostType)
		}
		if up.PartName != partName || up.PartType != partType {
			t.Errorf("%s: the file part %q typed %q, want %q typed %q", name, up.PartName, up.PartType, partName, partType)
		}
		if got := up.UploadArgs.Get("filename"); got != name {
			t.Errorf("%s: files.getUploadURL filename %q", name, got)
		}
		if got := up.UploadArgs.Get("length"); got != strconv.Itoa(len(data)) {
			t.Errorf("%s: files.getUploadURL length %q, want %d", name, got, len(data))
		}
		if got := up.UploadArgs.Get("snippet_type"); got != snippetType {
			t.Errorf("%s: files.getUploadURL snippet_type %q, want %q", name, got, snippetType)
		}
		var refs []struct{ ID, Title string }
		if err := json.Unmarshal([]byte(up.CompleteArgs.Get("files")), &refs); err != nil || len(refs) != 1 || refs[0].ID != p.ID {
			t.Errorf("%s: files.completeUpload files %q (%v), want this upload's id", name, up.CompleteArgs.Get("files"), err)
		}
		if p.File == nil {
			t.Fatalf("%s: no file object after processing", name)
		}
		if p.File.Filetype != filetype || p.File.Mimetype != mimetype || p.File.PrettyType != pretty {
			t.Errorf("%s: filed as %q, %q, %q; Slack files it %q, %q, %q", name, p.File.Filetype, p.File.Mimetype, p.File.PrettyType, filetype, mimetype, pretty)
		}
		// what the message draws: a code file's icon, not the Binary tile
		if got := richtext.FileTypeIcon(p.File.Filetype, p.File.Mode, ""); icon != "" && got != icon {
			t.Errorf("%s: drawn with the %q icon, want %q", name, got, icon)
		}
	}
	for _, c := range []struct{ name, part, filetype, mimetype, pretty, icon string }{
		{"BlockKitSend (2).js", "text/javascript", "javascript", "text/javascript", "JavaScript", "code"},
		{"index.ts", "application/octet-stream", "binary", "application/octet-stream", "Binary", "binary"},
		{"main.go", "application/octet-stream", "binary", "application/octet-stream", "Binary", "binary"},
		{"script.py", "application/octet-stream", "binary", "application/octet-stream", "Binary", "binary"},
		{"data.json", "application/json", "json", "application/json", "JSON", "code"},
		{"README.md", "text/markdown", "markdown", "text/markdown", "Markdown (raw)", "code"},
		{"notes.txt", "text/plain", "text", "text/plain", "Plain Text", "text-snippet"},
		{"table.csv", "text/csv", "csv", "text/csv", "CSV", ""},
		{"page.html", "text/html", "html", "text/html", "HTML", "code"},
		{"icon.svg", "image/svg+xml", "svg", "image/svg+xml", "SVG", ""},
		{"bundle.zip", "application/zip", "zip", "application/zip", "Zip", ""},
		{"report.pdf", "application/pdf", "pdf", "application/pdf", "PDF", "pdf-file"},
		{"shot.png", "image/png", "png", "image/png", "PNG", ""},
		{"clip.mp4", "video/mp4", "mp4", "video/mp4", "MPEG 4 video", ""},
		{"SCRIPT.JS", "text/javascript", "javascript", "text/javascript", "JavaScript", "code"},
		{"README", "application/octet-stream", "binary", "application/octet-stream", "Binary", "binary"},
		{"archive.tar.gz", "application/gzip", "gzip", "application/gzip", "Gzip", ""},
	} {
		data := []byte("synthetic " + c.name + "\n")
		if c.name == "shot.png" {
			data = synthPNG(t, 4, 4)
		}
		check(c.name, data, UploadFile{Name: c.name, Title: c.name, Data: data}, c.name, c.part, "", c.filetype, c.mimetype, c.pretty, c.icon)
	}
	src := []byte("package main\n")
	check("lint.go", src, snippetFile("lint.go", "go", "text/x-go", string(src)), "blob", "text/x-go", "go", "go", "text/x-go", "Go", "code")
	note := []byte("plain words\n")
	check("Untitled", note, snippetFile("Untitled", "auto", "text/plain", string(note)), "blob", "text/plain", "auto", "text", "text/plain", "Plain Text", "")
}
