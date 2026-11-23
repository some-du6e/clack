package slack

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Synthetic: FileType is the type Chromium 156 gives a File by its name,
// without the OS's lists: the text after the last "." in either case, the
// primary list before the secondary one, the first entry of each that has
// the extension.
func TestFileTypeLikeChromium(t *testing.T) {
	for _, c := range []struct{ name, want string }{
		{"BlockKitSend (2).js", "text/javascript"}, // Karim's file (2026-11-22)
		{"worker.mjs", "text/javascript"},
		{"index.ts", ""}, // neither list has it: application/octet-stream
		{"main.go", ""},
		{"script.py", ""},
		{"data.json", "application/json"},
		{"README.md", "text/markdown"},
		{"notes.txt", "text/plain"},
		{"notes.text", "text/plain"},
		{"table.csv", "text/csv"},
		{"page.html", "text/html"},
		{"icon.svg", "image/svg+xml"},
		{"bundle.zip", "application/zip"},
		{"report.pdf", "application/pdf"},
		{"shot.png", "image/png"}, // before image/apng's "png,apng"
		{"clip.mp4", "video/mp4"},
		{"SCRIPT.JS", "text/javascript"},
		{"Shot.PnG", "image/png"},
		{"README", ""},
		{"archive.tar.gz", "application/gzip"}, // the last extension
		{"backup.tgz", "application/gzip"},     // before application/x-gzip
		{"archive.tar", "application/x-tar"},
		{".bashrc", ""},
		{"trailing.", ""},
		{"song.mp3", "audio/mpeg"},   // before audio/mp3
		{"movie.webm", "video/webm"}, // before audio/webm
		{"photo.jpe", "image/jpeg"},
		{"favicon.ico", "image/x-icon"}, // before image/vnd.microsoft.icon
		{"style.css", "text/css"},
		{"run.sh", "text/x-sh"},
		{"setup.exe", "application/octet-stream"},
		{"a.ſvg", ""}, // ASCII case only: no "ſ" for "s"
	} {
		if got := FileType(c.name); got != c.want {
			t.Errorf("FileType(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// Synthetic: an upload is one "file" part, named and typed as a browser's
// FormData names and types a File: UploadBytes types it by its name
// (FileType; application/octet-stream when neither list has it), with the
// name's ", CR and LF percent-encoded; UploadPart posts a snippet's Blob,
// which FormData names "blob". Go's CreateFormFile typed every part
// application/octet-stream, and Slack filed a .js as Binary.
func TestUploadPartLikeFormData(t *testing.T) {
	type part struct{ field, disp, ctype, body string }
	var ctype string
	var parts []part
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctype, parts = r.Header.Get("Content-Type"), nil
		mt, params, err := mime.ParseMediaType(ctype)
		if err != nil || mt != "multipart/form-data" || params["boundary"] == "" {
			t.Errorf("Content-Type %q (%v), want multipart/form-data with a boundary", ctype, err)
			return
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(p)
			parts = append(parts, part{p.FormName(), p.Header.Get("Content-Disposition"), p.Header.Get("Content-Type"), string(b)})
		}
		w.Write([]byte("OK - 1\n"))
	}))
	defer srv.Close()
	c := New(Creds{Token: "xoxc-1", Cookie: "xoxd-a", TeamURL: srv.URL + "/"})
	ctx := context.Background()
	for _, tc := range []struct {
		name, data string
		snippet    *MultipartFile
		disp, typ  string
	}{
		{name: "BlockKitSend (2).js", data: "console.log(1)\n", disp: `form-data; name="file"; filename="BlockKitSend (2).js"`, typ: "text/javascript"},
		{name: "main.go", data: "package main\n", disp: `form-data; name="file"; filename="main.go"`, typ: "application/octet-stream"},
		{name: "SHOT.PNG", data: "PNG", disp: `form-data; name="file"; filename="SHOT.PNG"`, typ: "image/png"},
		{name: "README", data: "hi", disp: `form-data; name="file"; filename="README"`, typ: "application/octet-stream"},
		{name: "a.tar.gz", data: "gz", disp: `form-data; name="file"; filename="a.tar.gz"`, typ: "application/gzip"},
		{name: "me \"x\"\r\n.txt", data: "t", disp: `form-data; name="file"; filename="me %22x%22%0D%0A.txt"`, typ: "text/plain"},
		{snippet: &MultipartFile{Field: "file", Name: "blob", Type: "text/x-go", Data: []byte("package main\n")},
			data: "package main\n", disp: `form-data; name="file"; filename="blob"`, typ: "text/x-go"},
	} {
		var err error
		if tc.snippet != nil {
			err = c.UploadPart(ctx, srv.URL+"/upload/F1", *tc.snippet, nil)
		} else {
			err = c.UploadBytes(ctx, srv.URL+"/upload/F1", tc.name, []byte(tc.data), nil)
		}
		if err != nil {
			t.Fatalf("%q: %v", tc.name, err)
		}
		if len(parts) != 1 {
			t.Fatalf("%q: %d parts (%+v), want the one file part", tc.name, len(parts), parts)
		}
		p := parts[0]
		if p.field != "file" || p.disp != tc.disp || p.ctype != tc.typ || p.body != tc.data {
			t.Errorf("%q: part %+v, want disposition %q, type %q", tc.name, p, tc.disp, tc.typ)
		}
	}
}
