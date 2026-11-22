package fakeslack

import (
	"mime"
	"strings"
)

// How Slack's server files an upload: the file object's mimetype, filetype
// and pretty_type. A snippet goes by its files.getUploadURL snippet_type;
// any other upload by its part's Content-Type, as the browser's FormData
// typed it. An application/octet-stream part is a Binary file whatever its
// name says: Karim's "BlockKitSend (2).js", uploaded by Clack with Go's
// octet-stream part, came back filetype binary, mimetype
// application/octet-stream, pretty_type Binary (2026-11-22), where Slack's
// client sends text/javascript and the file is JavaScript.

// slackPrettyTypes is Slack's file object docs' "File types" table
// (docs.slack.dev/reference/objects/file-object): each filetype and its
// description, the pretty_type (the docs' own example: gif, "GIF"). "mp3"
// is as the docs print it.
var slackPrettyTypes = map[string]string{
	"auto":         "Auto Detect Type",
	"text":         "Plain Text",
	"ai":           "Illustrator File",
	"apk":          "APK",
	"applescript":  "AppleScript",
	"binary":       "Binary",
	"bmp":          "Bitmap",
	"boxnote":      "BoxNote",
	"c":            "C",
	"csharp":       "C#",
	"cpp":          "C++",
	"css":          "CSS",
	"csv":          "CSV",
	"clojure":      "Clojure",
	"coffeescript": "CoffeeScript",
	"cfm":          "ColdFusion",
	"d":            "D",
	"dart":         "Dart",
	"diff":         "Diff",
	"doc":          "Word Document",
	"docx":         "Word document",
	"dockerfile":   "Docker",
	"dotx":         "Word template",
	"eml":          "Email",
	"eps":          "EPS",
	"epub":         "EPUB",
	"erlang":       "Erlang",
	"fla":          "Flash FLA",
	"flv":          "Flash video",
	"fsharp":       "F#",
	"fortran":      "Fortran",
	"gdoc":         "GDocs Document",
	"gdraw":        "GDocs Drawing",
	"gif":          "GIF",
	"go":           "Go",
	"gpres":        "GDocs Presentation",
	"groovy":       "Groovy",
	"gsheet":       "GDocs Spreadsheet",
	"gzip":         "Gzip",
	"html":         "HTML",
	"handlebars":   "Handlebars",
	"haskell":      "Haskell",
	"haxe":         "Haxe",
	"indd":         "InDesign Document",
	"java":         "Java",
	"javascript":   "JavaScript",
	"jpg":          "JPEG",
	"json":         "JSON",
	"keynote":      "Keynote Document",
	"kotlin":       "Kotlin",
	"latex":        "LaTeX/sTeX",
	"lisp":         "Lisp",
	"lua":          "Lua",
	"m4a":          "MPEG 4 audio",
	"markdown":     "Markdown (raw)",
	"matlab":       "MATLAB",
	"mhtml":        "MHTML",
	"mkv":          "Matroska video",
	"mov":          "QuickTime video",
	"mp3":          "mp4",
	"mp4":          "MPEG 4 video",
	"mpg":          "MPEG video",
	"mumps":        "MUMPS",
	"numbers":      "Numbers Document",
	"nzb":          "NZB",
	"objc":         "Objective-C",
	"ocaml":        "OCaml",
	"odg":          "OpenDocument Drawing",
	"odi":          "OpenDocument Image",
	"odp":          "OpenDocument Presentation",
	"ods":          "OpenDocument Spreadsheet",
	"odt":          "OpenDocument Text",
	"ogg":          "Ogg Vorbis",
	"ogv":          "Ogg video",
	"pages":        "Pages Document",
	"pascal":       "Pascal",
	"pdf":          "PDF",
	"perl":         "Perl",
	"php":          "PHP",
	"pig":          "Pig",
	"png":          "PNG",
	"post":         "Slack Post",
	"powershell":   "PowerShell",
	"ppt":          "PowerPoint presentation",
	"pptx":         "PowerPoint presentation",
	"psd":          "Photoshop Document",
	"puppet":       "Puppet",
	"python":       "Python",
	"qtz":          "Quartz Composer Composition",
	"r":            "R",
	"rtf":          "Rich Text File",
	"ruby":         "Ruby",
	"rust":         "Rust",
	"sql":          "SQL",
	"sass":         "Sass",
	"scala":        "Scala",
	"scheme":       "Scheme",
	"sketch":       "Sketch File",
	"shell":        "Shell",
	"smalltalk":    "Smalltalk",
	"svg":          "SVG",
	"swf":          "Flash SWF",
	"swift":        "Swift",
	"tar":          "Tarball",
	"tiff":         "TIFF",
	"tsv":          "TSV",
	"vb":           "VB.NET",
	"vbscript":     "VBScript",
	"vcard":        "vCard",
	"velocity":     "Velocity",
	"verilog":      "Verilog",
	"wav":          "Waveform audio",
	"webm":         "WebM",
	"wmv":          "Windows Media Video",
	"xls":          "Excel spreadsheet",
	"xlsx":         "Excel spreadsheet",
	"xlsb":         "Excel Spreadsheet (Binary, Macro Enabled)",
	"xlsm":         "Excel Spreadsheet (Macro Enabled)",
	"xltx":         "Excel template",
	"xml":          "XML",
	"yaml":         "YAML",
	"zip":          "Zip",
}

// uploadFiletypes is the filetype Slack gives an upload by its part's
// Content-Type, for each type Chromium's own lists give a File
// (slack.FileType).
var uploadFiletypes = map[string]string{
	"text/javascript":        "javascript",
	"application/javascript": "javascript",
	"application/json":       "json",
	"text/markdown":          "markdown",
	"text/plain":             "text",
	"text/csv":               "csv",
	"text/html":              "html",
	"text/css":               "css",
	"text/xml":               "xml",
	"text/x-sh":              "shell",
	"image/svg+xml":          "svg",
	"image/png":              "png",
	"image/jpeg":             "jpg",
	"image/gif":              "gif",
	"image/bmp":              "bmp",
	"application/zip":        "zip",
	"application/pdf":        "pdf",
	"application/gzip":       "gzip",
	"application/x-gzip":     "gzip",
	"application/x-tar":      "tar",
	"video/mp4":              "mp4",
	"video/webm":             "webm",
}

// types is an upload's mimetype, filetype and pretty_type as Slack's server
// files it: a snippet by its snippet_type ("auto" and "text" as Plain
// Text); an octet-stream or untyped part as Binary; a part of a type
// uploadFiletypes has by that type; any other part as its name's
// extension, the way this server filed every upload before.
func (f *UploadedFile) types() (mimetype, filetype, pretty string) {
	mt, _, err := mime.ParseMediaType(f.PartType)
	if err != nil {
		mt = ""
	}
	if st := f.UploadArgs.Get("snippet_type"); st != "" {
		if st == "auto" {
			st = "text"
		}
		if mt == "" {
			mt = "text/plain"
		}
		return mt, st, prettyType(st)
	}
	if mt == "" || mt == "application/octet-stream" {
		return "application/octet-stream", "binary", prettyType("binary")
	}
	if ft, ok := uploadFiletypes[mt]; ok {
		return mt, ft, prettyType(ft)
	}
	ext := strings.TrimPrefix(extOf(f.Name), ".")
	return mt, strings.ToLower(ext), strings.ToUpper(ext)
}

// prettyType is filetype's pretty_type (its upper case where the docs
// have none).
func prettyType(filetype string) string {
	if p, ok := slackPrettyTypes[filetype]; ok {
		return p
	}
	return strings.ToUpper(filetype)
}
