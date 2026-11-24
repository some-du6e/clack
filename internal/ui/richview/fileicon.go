package richview

// FileIconArt picks a file's svgicon the way Slack's file thumbnails do
// (CDN build bv1-13, boot-shared): FileThumbnail's IconThumbnail (PQXV)
// draws Icon (xjlK) with file "svgicon" and getSVGIconType's type (LqOh);
// Icon turns the type into a class, the type with "_" made "-" looked up
// in the filetype alias table (ABUP) or "file-" before it, and the class
// c-icon--{class}-svgicon's content is one of the SVG files
// client-boot-styles names; .c-icon--filetype-svgicon, on every svgicon,
// is generic-b556cfe.svg for a class with no rule of its own. The files
// are art in internal/ui/icons (file_icons.go): "fileicon-" and the
// file's name without its hash.

import (
	"image/color"
	"strings"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func setOf(xs ...string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// G40A's isSVGIconAvailable set; LqOh's code filetypes; ABUP's aliases;
// client-boot-styles' c-icon--{class}-svgicon files.
var svgIconAvailable = setOf("ai", "archive", "audio", "avi", "binary", "bmp", "cpp", "code", "css", "csv", "doc", "docs", "docx", "email", "eml", "eps", "flv", "folder", "gdoc", "generic", "gpres", "gsheet", "html", "indd", "java", "lucidchart", "lucidpress", "lucidspark", "m4a", "mov", "mp3", "mp4", "ogg", "post", "png", "jpeg", "jpg", "gif", "pdf", "php", "ppt", "pptx", "psd", "quip", "sketch", "snippet", "space", "spreadsheet", "text", "tiff", "video", "wav", "xls", "xlsm", "xlsx", "xltx", "zip", "zohoshow")
var codeFileTypes = setOf("apl", "applescript", "asciiarmor", "asn.1", "aspx", "asterisk", "brainfuck", "c", "cfm", "clojure", "cmake", "cobol", "coffeescript", "cpp", "crystal", "csharp", "css", "csv", "cypher", "d", "dart", "diff", "django", "dockerfile", "dtd", "dylan", "ebnf", "ecl", "eiffel", "ejs", "elixir", "elm", "erb", "erlang", "factor", "fcl", "forth", "fortran", "fsharp", "gas", "gherkin", "go", "graphql", "groovy", "gss", "haml", "handlebars", "haskell", "haskell_literate", "haxe", "html", "http", "idl", "java", "javascript", "jinja2", "json", "json_ld", "jsp", "jsx", "julia", "kotlin", "latex", "less", "lisp", "livescript", "lua", "markdown", "mathematica", "matlab", "mbox", "mirc", "modelica", "mscgen", "mumps", "mysql", "nginx", "nsis", "ntriples", "objc", "ocaml", "oz", "pascal", "pegjs", "perl", "php", "pig", "powershell", "properties", "protobuf", "pug", "puppet", "python", "q", "r", "rpm", "rst", "ruby", "rust", "sas", "sass", "scala", "scheme", "scss", "shell", "sieve", "slim", "smalltalk", "smarty", "solr", "soy", "sparql", "spreadsheet", "sql", "stylus", "swift", "tcl", "textile", "tiddlywiki", "tiki", "toml", "tornado", "troff", "tsv", "tsx", "ttcn", "ttcn_cfg", "turtle", "twig", "typescript", "vb", "vbscript", "velocity", "verilog", "vhdl", "vue", "webidl", "xml", "xquery", "yacas", "yaml", "z80")
var iconTypeAlias = map[string]string{"mp3": "file-audio", "wav": "file-audio", "m4a": "file-audio", "ogg": "file-audio", "txt": "file-text", "rtf": "file-text", "png": "file-image", "jpg": "file-image", "jpeg": "file-image", "gif": "file-image", "bmp": "file-image", "eps": "file-image", "image": "file-image", "pages": "file-image", "tiff": "file-image", "svg": "file-image", "flv": "file-video", "mov": "file-video", "avi": "file-video", "mp4": "file-video", "key": "file-presentation", "keynote": "file-presentation", "csv": "file-spreadsheet", "tsv": "file-spreadsheet", "numbers": "file-spreadsheet", "iso": "file-media-archive", "zip": "file-archive", "dmg": "file-disk-image", "exe": "file-executable", "gfx3d": "file-3d-graphic", "db": "file-database", "sql": "file-database", "md": "file-markdown", "c": "file-code", "cpp": "file-code", "csharp": "file-code", "diff": "file-code", "erlang": "file-code", "java": "file-code", "javascript": "file-code", "objc": "file-code", "perl": "file-code", "python": "file-code", "ruby": "file-code", "erb": "file-code", "rb": "file-code", "xml": "file-code", "space": "file-text-post", "docs": "file-text-post", "quip": "file-text-quip", "gpres": "file-google-presentation", "gsheet": "file-google-spreadsheet", "gdoc": "file-google-document", "gdraw": "file-google-form", "gform": "file-google-form", "zohoshow": "file-zoho-show", "paper": "file-dropbox-paper", "papert": "file-dropbox-paper", "lucidchart": "file-lucidchart", "lucidpress": "file-lucidpress", "lucidspark": "file-lucidspark", "miro": "brand-miro", "doc": "file-word", "docx": "file-word", "xls": "file-excel", "xlsx": "file-excel", "xlsm": "file-excel", "xltx": "file-excel", "ppt": "file-powerpoint", "pptx": "file-powerpoint", "ai": "file-illustrator", "psd": "file-photoshop", "indd": "file-indesign", "fla": "file-adobe-flash", "swf": "file-adobe-swf", "apk": "file-android-app", "ipa": "file-iphone-app"}
var svgIconFiles = map[string]string{"file-archive": "archive", "file-audio": "file-audio", "file-binary": "binary", "file-code": "code", "file-css": "css", "file-email": "email", "file-excel": "file-excel", "file-google-document": "file-google-document", "file-google-presentation": "file-google-presentation", "file-google-spreadsheet": "file-google-spreadsheet", "file-html": "html", "file-illustrator": "file-illustrator", "file-image": "file-image", "file-indesign": "file-indesign", "file-lucidchart": "file-lucidchart", "file-lucidpress": "file-lucidpress", "file-lucidspark": "file-lucidspark", "file-pdf": "file-pdf", "file-photoshop": "file-photoshop", "file-php": "php", "file-powerpoint": "file-powerpoint", "file-sketch": "file-sketch", "file-snippet": "file-snippet", "file-spreadsheet": "file-excel", "file-text": "file-snippet", "file-text-post": "file-text-post", "file-text-quip": "file-text-quip", "file-video": "file-video", "file-word": "file-word", "file-zoho-show": "file-zoho-show"}

// SVGIconAvailable reports whether Slack has an svgicon of its own for
// filetype t (G40A's isSVGIconAvailable): getSVGIconType keeps only such
// a type for a File being uploaded (the composer's chips).
func SVGIconAvailable(t string) bool { return svgIconAvailable[t] }

// FileIconArt is f's svgicon as an icons.Art name.
func FileIconArt(f *richtext.File) string {
	t := strings.ReplaceAll(svgIconType(f), "_", "-")
	class := iconTypeAlias[t]
	if class == "" {
		class = "file-" + t
	}
	if file, ok := svgIconFiles[class]; ok {
		return "fileicon-" + file
	}
	return "fileicon-generic"
}

// svgIconType is getSVGIconType for a file object: the filetype when
// Slack has an icon for it, "code" for a code filetype, else the file's
// kind (getFileType, 2grX), a snippet in anything but text or markdown
// being code.
func svgIconType(f *richtext.File) string {
	t := strings.ToLower(f.Type)
	if t != "" && svgIconAvailable[t] {
		return t
	}
	if t != "" && codeFileTypes[t] && t != "text" && t != "markdown" {
		return "code"
	}
	a := fileKind(f)
	if a == "snippet" && t != "text" && t != "markdown" {
		return "code"
	}
	return a
}

// fileKind is getFileType (2grX): image, audio, video, post, list, quip,
// huddle_transcript, snippet, email, docs or generic.
func fileKind(f *richtext.File) string {
	switch {
	case f.IsImage:
		return "image"
	case f.IsAudio:
		return "audio"
	case f.IsVideo:
		return "video"
	case f.Type != "quip" && (f.Mode == "post" || f.Mode == "space" || f.Mode == "docs"):
		return "post"
	case isListFile(f):
		return "list"
	case richtext.IsCanvasFile(f):
		return "quip"
	case f.Mode == "huddle_transcript" || f.Type == "huddle_transcript":
		return "huddle_transcript"
	case f.Mode == "snippet":
		return "snippet"
	case f.Mode == "email":
		return "email"
	case f.Mode == "docs":
		return "docs"
	}
	return "generic"
}

// IconLook is what IconThumbnail (PQXV) draws in a file's 36px tile: a
// tile of its own (Bg, radius 8) under a 20px Glyph in Fg, or the file's
// svgicon (Art, its own rounded tile).
type IconLook struct {
	Art    string
	Bg, Fg color.NRGBA
	Glyph  string
}

// FileIconLook is IconThumbnail for f (CDN build bv1-13, boot-shared
// PQXV), the tile FileEntityThumbnailIcon draws for a file that isn't an
// image or playable media: a canvas's QuipIcon (useIa4Icon), a list's
// BaseListIcon, a folder's p-file_thumbnail__folder_icon, else the file's
// svgicon. A skill canvas's ai-skills, a surface's SurfaceIcon
// (gauge-filled) and Miro's MiroIcon (brand-miro-filled) have no glyph
// here yet: those files get the svgicon.
func FileIconLook(f *richtext.File, dark bool) IconLook {
	switch {
	case richtext.IsCanvasFile(f):
		return QuipIconLook(dark)
	case f.Mode == "list" || isListFile(f):
		return ListIconLook(dark)
	case f.Type == "folder" || f.Type == "folder_skill_set": // fileIsFolder
		if f.Type == "folder_skill_set" { // fileIsSkillSet: --skill-set
			return IconLook{Bg: theme.PltColor("indigo", "5", dark), Fg: theme.PltColor("indigo", "60", dark), Glyph: "book"}
		}
		return IconLook{Bg: theme.PltColor("gray", "50", dark), Fg: color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, Glyph: "folder-filled"}
	}
	return IconLook{Art: FileIconArt(f)}
}

// QuipIconLook is QuipIcon with useIa4Icon: canvas-content-filled,
// lagoon-0 on lagoon-40 (p-quip_icon_container__ia4_icon); lagoon-100 on
// lagoon-60 in the dark theme (p-file_thumbnail__container--dark-theme).
func QuipIconLook(dark bool) IconLook {
	if dark {
		return IconLook{Bg: theme.PltColor("lagoon", "60", true), Fg: theme.PltColor("lagoon", "100", true), Glyph: "canvas-content-filled"}
	}
	return IconLook{Bg: theme.PltColor("lagoon", "40", false), Fg: theme.PltColor("lagoon", "0", false), Glyph: "canvas-content-filled"}
}

// ListIconLook is BaseListIcon (isFilled): lists-filled, Clack's
// list-tab-filled, honeycomb-0 on honeycomb-40 (p-list_icon--light);
// honeycomb-100 on honeycomb-50 dark.
func ListIconLook(dark bool) IconLook {
	if dark {
		return IconLook{Bg: theme.PltColor("honeycomb", "50", true), Fg: theme.PltColor("honeycomb", "100", true), Glyph: "list-tab-filled"}
	}
	return IconLook{Bg: theme.PltColor("honeycomb", "40", false), Fg: theme.PltColor("honeycomb", "0", false), Glyph: "list-tab-filled"}
}
