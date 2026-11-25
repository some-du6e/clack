package icons

// FileEntityIcon is Slack's D0WB map (CDN build 9f450b8ad3db): a file's
// filetype to the SvgIcon on its tile and the tile's palette, as
// FileEntityIcon (ljGw) draws Kit's file entities, Work Object file
// fields (Mxse) and SmallFileEntity's monochrome external-file icons do.
// Its keys are h+m7's file types; V7l1 takes "generic" for any other.
func FileEntityIcon(filetype string) (icon, palette string) {
	e, ok := fileEntityIcons[filetype]
	if !ok {
		e = fileEntityIcons["generic"]
	}
	return e[0], e[1]
}

// FileEntityShade is the tile palette's shade, as ljGw's classes have
// it: aubergine and gray at 60, the rest at 50.
func FileEntityShade(palette string) string {
	if palette == "aubergine" || palette == "gray" {
		return "60"
	}
	return "50"
}

// fileEntityIcons is D0WB's Map: file type -> {SvgIcon, palette}.
var fileEntityIcons = map[string][2]string{
	"ai":         {"brand-adobe-ai-filled", "tangerine"},
	"apk":        {"brand-android-filled", "jade"},
	"avi":        {"video-filled", "mojito"},
	"bmp":        {"image-filled", "ocean"},
	"c":          {"code", "tomato"},
	"cpp":        {"code", "tomato"},
	"csharp":     {"code", "tomato"},
	"csv":        {"spreadsheet", "ocean"},
	"db":         {"database", "tomato"},
	"diff":       {"code", "tomato"},
	"dmg":        {"disc-image", "ocean"},
	"doc":        {"brand-microsoft-word-filled", "ocean"},
	"docx":       {"brand-microsoft-word-filled", "ocean"},
	"eps":        {"image-filled", "ocean"},
	"erb":        {"code", "tomato"},
	"erlang":     {"code", "tomato"},
	"fla":        {"brand-adobe-fl-filled", "tomato"},
	"flv":        {"video-filled", "mojito"},
	"gdraw":      {"bulleted-list", "aubergine"},
	"generic":    {"file", "ocean"},
	"gif":        {"image-filled", "ocean"},
	"gfdorm":     {"bulleted-list", "aubergine"},
	"gfx3d":      {"three-d-graphic", "ocean"},
	"gdoc":       {"document", "ocean"},
	"gpres":      {"presentation", "tangerine"},
	"gsheet":     {"spreadsheet", "mojito"},
	"html":       {"code", "tomato"},
	"image":      {"image-filled", "ocean"},
	"indd":       {"brand-adobe-id-filled", "flamingo"},
	"ipa":        {"brand-apple-filled", "gray"},
	"iso":        {"media-archive", "ocean"},
	"java":       {"code", "tomato"},
	"javascript": {"code", "tomato"},
	"jpg":        {"image-filled", "ocean"},
	"jpeg":       {"image-filled", "ocean"},
	"key":        {"presentation", "ocean"},
	"keynote":    {"presentation", "ocean"},
	"lucidchart": {"brand-lucid-chart-filled", "tangerine"},
	"lucidpress": {"brand-lucid-press-filled", "aquarium"},
	"lucidspark": {"brand-lucid-spark-filled", "tomato"},
	"m4a":        {"sound-filled", "ocean"},
	"markdown":   {"brand-markdown-filled", "gray"},
	"md":         {"brand-markdown-filled", "gray"},
	"mov":        {"video-filled", "mojito"},
	"mp3":        {"sound-filled", "ocean"},
	"mp4":        {"video-filled", "mojito"},
	"numbers":    {"spreadsheet", "ocean"},
	"objc":       {"code", "tomato"},
	"ogg":        {"sound-filled", "ocean"},
	"pages":      {"image-filled", "ocean"},
	"paper":      {"brand-dropbox-paper-filled", "ocean"},
	"papert":     {"brand-dropbox-paper-filled", "ocean"},
	"perl":       {"code", "tomato"},
	"pdf":        {"pdf-file-filled", "flamingo"},
	"png":        {"image-filled", "ocean"},
	"ppt":        {"brand-microsoft-ppt-filled", "tomato"},
	"pptx":       {"brand-microsoft-ppt-filled", "tomato"},
	"psd":        {"brand-adobe-ps-filled", "ocean"},
	"python":     {"code", "tomato"},
	"quip":       {"quip", "tangerine"},
	"rb":         {"code", "tomato"},
	"rtf":        {"text", "ocean"},
	"ruby":       {"code", "tomato"},
	"sql":        {"database", "tomato"},
	"surface":    {"gauge-filled", "ocean"},
	"svg":        {"image-filled", "ocean"},
	"swf":        {"brand-adobe-swf-filled", "tomato"},
	"text":       {"text", "ocean"},
	"tiff":       {"image-filled", "ocean"},
	"tsv":        {"spreadsheet", "ocean"},
	"txt":        {"text", "ocean"},
	"wav":        {"sound-filled", "ocean"},
	"xls":        {"brand-microsoft-excel-filled", "mojito"},
	"xlsm":       {"brand-microsoft-excel-filled", "mojito"},
	"xlsx":       {"brand-microsoft-excel-filled", "mojito"},
	"xltx":       {"brand-microsoft-excel-filled", "mojito"},
	"xml":        {"code", "tomato"},
	"zip":        {"zip", "ocean"},
	"zohoshow":   {"brand-zoho-filled", "tomato"},
}
