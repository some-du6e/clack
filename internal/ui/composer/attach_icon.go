package composer

import (
	"strings"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// A chip's svgicon: Slack's multi-file upload draws a file that isn't an
// image as a MediumFileEntity (the gallery's renderFile, PendingFile's
// p-pending_file__medium_file_entity), whose icon is FileEntityThumbnailIcon
// at medium, 36px, of the File being uploaded (CDN build bv1-13). Its type
// is getSVGIconType's (LqOh) for a File: audio for a recorded clip of an
// audio subtype (lqEY), video for a video clip's (7OiV), else the type its
// MIME type names (getFileTypeFromMimeType, vITP J4) where Slack has an
// svgicon for it (G40A), else generic: an .exe's application/x-msdownload
// is "application", so Slack's generic blue page, as a .docx's long
// subtype is until the server names its filetype.
const attIconPx = 36

// attIconArt is v's svgicon, as richview.FileIconArt names it: a restored
// file's by its own filetype (getSVGIconType of the file object), else the
// File's.
func attIconArt(v attView) string {
	if v.server != nil {
		return richview.FileIconArt(v.server)
	}
	return richview.FileIconArt(&richtext.File{Type: attSVGType(v.c.subtype(), v.typ)})
}

// attSVGType is getSVGIconType for a File of subtype sub and MIME type
// mime.
func attSVGType(sub, mime string) string {
	switch sub {
	case "audio_story", "slack_audio", "slack_name_pronunciation": // lqEY
		return "audio"
	case "video_story", "slack_video": // 7OiV
		return "video"
	}
	if t := attMimeFileType(mime); t != "" && richview.SVGIconAvailable(t) {
		return t
	}
	return "generic"
}

// attMimeFileType is getFileTypeFromMimeType: application/vnd.ms-excel is
// csv; else the subtype when Slack knows it as a file type
// (attKnownFileTypes), else the top-level type. "" without a type.
func attMimeFileType(mime string) string {
	if mime == "" {
		return ""
	}
	if mime == "application/vnd.ms-excel" {
		return "csv"
	}
	top, sub, _ := strings.Cut(mime, "/")
	if attKnownFileTypes[sub] {
		return sub
	}
	return top
}

// attKnownFileTypes is ZOnG's $D: the file types Slack's client knows.
var attKnownFileTypes = func() map[string]bool {
	m := map[string]bool{}
	for _, t := range []string{
		"auto", "text", "ai", "apk", "applescript", "binary", "bmp", "boxnote", "c", "csharp", "cpp",
		"css", "csv", "clojure", "coffeescript", "cfm", "d", "dart", "diff", "doc", "docx", "dockerfile",
		"dotx", "email", "eps", "epub", "erlang", "fla", "flv", "fsharp", "fortran", "gdoc", "gdraw",
		"gif", "go", "gpres", "groovy", "gsheet", "gzip", "html", "handlebars", "haskell", "haxe",
		"indd", "java", "javascript", "jpg", "keynote", "kotlin", "latex", "lisp", "lua", "lucidchart",
		"lucidpress", "lucidspark", "m4a", "markdown", "matlab", "mhtml", "mkv", "mov", "mp3", "mp4",
		"mpg", "mumps", "numbers", "nzb", "objc", "ocaml", "odg", "odi", "odp", "odd", "odt", "ogg",
		"ogv", "pages", "paper", "papert", "pascal", "pdf", "zohoshow", "perl", "php", "pig", "png",
		"post", "powershell", "ppt", "pptx", "psd", "puppet", "python", "qtz", "r", "rtf", "ruby",
		"rust", "sql", "sass", "scala", "scheme", "sketch", "shell", "smalltalk", "svg", "swf", "swift",
		"tar", "tiff", "tsv", "vb", "vbscript", "vcard", "velocity", "verilog", "wav", "webm", "wmv",
		"xls", "xlsx", "xlsb", "xlsm", "xltx", "xml", "yaml", "zip",
	} {
		m[t] = true
	}
	return m
}()
