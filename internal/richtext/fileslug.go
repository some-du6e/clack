package richtext

// A rich_text "file" element (an inline file link), as Slack's web client
// draws it (build 031dbe6a7400): RichTextSectionSUI remaps it to
// rich_text_file (c2gp remapInlineFileTypes), whose renderable is File
// (boot-shared v1Jk): a RichTextSlug with getFileSlugAttributes' (p292)
// label and icon for the file the files store holds (fetched when
// missing):
//
//   - variant getFileStatus (SRWA): no file yet or a deleted one
//     "deleted", one the viewer may not see "hidden", else "normal";
//   - label: "Audio clip" / "Video clip" for a clip (lqEY, 7OiV), else
//     getFileDisplayName (R3SR: its title, else its name), sanitized
//     (0jGF sanitizeOpInsertString);
//   - icon (fileSlugIconResolver): trash (deleted), eye-closed (hidden,
//     locked, missing), else getFileTypeSvgIconName (vITP);
//   - not normal: the slug is disabled;
//   - a skill (is_skill canvas) or an element without file_id: nothing.
//
// The element's own text isn't read.

import (
	"regexp"
	"strings"
)

// fileSlugSpan converts a rich_text file element (ok false: nothing).
func (c *builder) fileSlugSpan(e *bk) (Span, bool) {
	id := e.FileID
	if id == "" {
		return Span{}, false
	}
	var ref CanvasRef
	status := ""
	if f, ok := c.files[id]; ok && f != nil {
		ref = CanvasRef{Title: f.Title, Name: f.Name, Filetype: f.Filetype, Mode: f.Mode, Subtype: f.Subtype, Status: "normal"}
		if f.Mode == "tombstone" {
			ref.Status = "deleted"
		}
		status = ref.Status
	} else if cr, ok := c.r.(CanvasResolver); ok {
		if r, known := cr.CanvasRef(id); known {
			ref, status = r, firstNonEmpty(r.Status, "normal")
		}
	}
	if status == "" { // not fetched yet: getFileStatus(undefined)
		status = "deleted"
	}
	label := ""
	switch {
	case status == "deleted" && ref.Title == "" && ref.Name == "":
	case isAudioClip(ref.Subtype):
		label = "Audio clip"
	case ref.Subtype == "video_story" || ref.Subtype == "slack_video":
		label = "Video clip"
	default:
		label = firstNonEmpty(ref.Title, ref.Name)
	}
	label = strings.ReplaceAll(sanitizeSlugText(label), "\n", " ") // a span holds no newline
	icon := ""
	switch status {
	case "deleted":
		icon = "trash"
	case "hidden", "locked", "missing":
		icon = "eye-closed"
	default:
		icon = FileTypeIcon(ref.Filetype, ref.Mode, ref.Subtype)
	}
	return Span{Kind: Canvas, ID: id, URL: SafeURL(e.URL), Icon: icon, Text: label, Off: status != "normal"}, true
}

func isAudioClip(subtype string) bool {
	switch subtype {
	case "audio_story", "slack_audio", "slack_name_pronunciation":
		return true
	}
	return false
}

var (
	slugAudioExt = regexp.MustCompile(`^(m4a|mp3|wav)$`)
	slugVideoExt = regexp.MustCompile(`^(mp4|mov|webm)$`)
)

// codeFiletypes is the code file types (ZOnG nT).
var codeFiletypes = map[string]bool{}

func init() {
	for _, t := range strings.Fields(`c cfm clojure coffeescript cpp csharp css d dart diff dockerfile erlang fortran fsharp go groovy
		hack handlebars haskell haxe html java javascript json kotlin latex lisp lua markdown matlab mumps mysql objc ocaml pascal
		perl php pig powershell properties puppet python r ruby rust sass scala scheme shell smalltalk sql swift typescript vb
		vbscript velocity verilog xml yaml`) {
		codeFiletypes[t] = true
	}
}

// FileTypeIcon is getFileTypeSvgIconName (boot-shared vITP): a file's
// SvgIcon name by its type.
func FileTypeIcon(filetype, mode, subtype string) string {
	switch {
	case filetype == "":
		return "file"
	case slugAudioExt.MatchString(filetype) || subtype == "audio_story":
		return "music"
	case slugVideoExt.MatchString(filetype):
		return "video"
	case codeFiletypes[filetype]:
		return "code"
	case mode == "huddle_transcript" || filetype == "huddle_transcript":
		return "transcript"
	case mode == "surface" || filetype == "surface":
		return "gauge-filled"
	case filetype == "folder_skill_set":
		return "book"
	case filetype == "folder":
		return "folder-filled"
	}
	switch filetype {
	case "quip":
		return "canvas-content"
	case "gdoc":
		return "document"
	case "list":
		return "lists"
	case "email":
		return "email"
	case "pdf":
		return "pdf-file"
	case "text":
		return "text-snippet"
	case "binary":
		return "binary"
	case "disk":
		return "disc-image"
	case "font":
		return "text"
	case "jpg", "png", "gif", "webp", "tiff", "bmp", "svg", "heic", "psd", "gdraw":
		return "image"
	case "gpres", "ppt", "pptx", "potx", "key", "slides":
		return "presentation"
	case "gsheet", "xls", "xlsx", "xltx", "xlsm", "xlsb", "numbers", "csv":
		return "spreadsheet"
	case "zip", "gzip", "7z", "rar", "tar", "gz", "bz2", "xz":
		return "zip"
	}
	return "file"
}
