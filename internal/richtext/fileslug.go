package richtext

// A rich_text "file" element (an inline file link), as Slack's web client
// draws it (build 031dbe6a7400): RichTextSectionSUI remaps it to
// rich_text_file (c2gp remapInlineFileTypes), whose renderable is File
// (boot-shared v1Jk): a RichTextSlug with getFileSlugAttributes' (p292)
// label and icon for the file the files store holds (fetched when
// missing):
//
//   - variant getFileStatus (SRWA, FileStatusOf): a deleted file
//     "deleted", one the viewer may not see "hidden", else "normal" (a
//     file not fetched yet is the store's unknown object: normal);
//   - label: "Audio clip" / "Video clip" for a clip (lqEY, 7OiV), else
//     getFileDisplayName (R3SR: its title, else its name, else its
//     status's string (Yl4O: "Deleted file", "Hidden file"…), else
//     "Untitled"), sanitized (0jGF sanitizeOpInsertString); a normal
//     file's is its Name (FileSlugRichTextLabel maaT: emoji drawn, a
//     canvas's title_blocks), in the element's style;
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
	status, known := "", false
	if f, ok := c.files[id]; ok && f != nil {
		ref = CanvasRef{Title: f.Title, TitleBlocks: f.TitleBlocks, Name: f.Name, Filetype: f.Filetype, Mode: f.Mode,
			Subtype: f.Subtype, FolderType: f.FolderType, IsSkill: f.IsSkill, Status: FileStatusOf(FileFacts{
				Mode: f.Mode, FileAccess: f.FileAccess, IsDeleted: f.IsDeleted, HiddenByLimit: f.IsHiddenByLimit, Tombstoned: f.IsTombstoned})}
		status, known = ref.Status, true
	} else if cr, ok := c.r.(CanvasResolver); ok {
		if r, have := cr.CanvasRef(id); have {
			ref, status, known = r, firstNonEmpty(r.Status, "normal"), true
		}
	}
	if ref.IsSkill && isCanvasType(ref) { // v1Jk: fileIsSkill draws nothing
		return Span{}, false
	}
	if status == "" { // not fetched yet: the store's unknown object (getFileStatus: normal)
		status = "normal"
	}
	var label string
	switch {
	case isAudioClip(ref.Subtype):
		label = "Audio clip"
	case ref.Subtype == "video_story" || ref.Subtype == "slack_video":
		label = "Video clip"
	default:
		label = fileDisplayName(ref, status)
	}
	label = strings.ReplaceAll(sanitizeSlugText(label), "\n", " ") // a span holds no newline
	icon := ""
	switch {
	case status == "deleted":
		icon = "trash"
	case status == "hidden" || status == "locked" || status == "missing":
		icon = "eye-closed"
	case ref.FolderType == "skill_set": // fileIsSkillSet
		icon = "book"
	default:
		icon = FileTypeIcon(ref.Filetype, ref.Mode, ref.Subtype)
	}
	sp := Span{Kind: Canvas, ID: id, URL: SafeURL(e.URL), Icon: icon, Text: label, Off: status != "normal"}
	if label == "" {
		return sp, true
	}
	// the slug draws its label from its parts (RichTextSlug's children): a
	// normal file the store holds through Name (emoji drawn; a canvas's
	// title_blocks), anything else as plain text
	parts := []Span{{Kind: Text, Text: label}}
	if known && status == "normal" && !isAudioClip(ref.Subtype) && ref.Subtype != "video_story" && ref.Subtype != "slack_video" {
		parts = titleSpans(c.r, label)
		if isCanvasType(ref) {
			if bs := c.titleBlocks(ref.TitleBlocks); len(bs) > 0 {
				if t := firstLine(flatten(bs)); len(t) > 0 {
					parts = t
				}
			}
		}
	}
	if st, _ := e.style(); st != 0 { // the element's style (v1Jk passes style to the slug)
		for i := range parts {
			parts[i].Style |= st
		}
	}
	sp.Parts = &SpanParts{Spans: parts}
	return sp, true
}

// isCanvasType is 2grX's "quip": a canvas.
func isCanvasType(ref CanvasRef) bool {
	return ref.Kind == "canvas" || ref.Filetype == "quip" || ref.Mode == "canvas"
}

// fileDisplayName is getFileDisplayName (R3SR): the file's title, else
// getFileName: its name, else maybeGetFileStatusString (Yl4O) for its
// status and kind, else "Untitled".
func fileDisplayName(ref CanvasRef, status string) string {
	if t := strings.TrimSpace(ref.Title); t != "" {
		return t
	}
	if n := strings.TrimSpace(ref.Name); n != "" {
		return n
	}
	// by 2grX's kind: a canvas, a list, any other file
	byKind := func(canvas, list, file string) string {
		switch {
		case isCanvasType(ref):
			return canvas
		case ref.Kind == "list" || ref.Filetype == "list" || ref.Mode == "list":
			return list
		}
		return file
	}
	switch status {
	case "deleted":
		return byKind("Deleted canvas", "Deleted list", "Deleted file")
	case "hidden":
		return byKind("Private canvas", "Private list", "Hidden file")
	case "locked":
		return "Access denied"
	case "missing":
		return byKind("Private canvas", "Private list", "File not found")
	}
	return "Untitled"
}

// FileFacts is what getFileStatus reads of a file.
type FileFacts struct {
	Mode, FileAccess string
	// IsDeleted is is_deleted, HiddenByLimit is_hidden_by_limit,
	// Tombstoned is_tombstoned (DLP).
	IsDeleted, HiddenByLimit, Tombstoned bool
}

// FileStatusOf is getFileStatus (SRWA) over vITP's predicates: "deleted"
// for a deleted file (is_deleted) or a tombstone; "hidden" for one hidden
// by the storage limit, an access-denied one, a Slack Connect block, one
// not found or not visible, a DLP tombstone (shouldShowFile false); else
// "normal". (Its "locked" and "missing" can't come out: shouldShowFile
// fails for both first. The canvas and list blocks Slack also reads from
// file_access, and DSA restrictions, aren't here.)
func FileStatusOf(f FileFacts) string {
	switch {
	case f.IsDeleted || f.Mode == "tombstone":
		return "deleted"
	case f.Mode == "hidden_by_limit" || f.HiddenByLimit,
		f.Mode == "access_denied" || f.FileAccess == "access_denied",
		f.Mode == "canvas_slack_connect_blocked" || f.Mode == "lists_slack_connect_blocked",
		f.Mode == "not_found" || f.FileAccess == "file_not_found",
		f.Mode == "not_visible" || f.FileAccess == "not_visible",
		f.Tombstoned:
		return "hidden"
	}
	return "normal"
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
