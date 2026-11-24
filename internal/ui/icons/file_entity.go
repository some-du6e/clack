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
	"generic": {"file", "ocean"},
}
