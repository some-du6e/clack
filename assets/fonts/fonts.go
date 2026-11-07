// Package fonts embeds the Lato typeface (SIL Open Font License 1.1, see
// OFL.txt) used by the native UI.
package fonts

import _ "embed"

var (
	//go:embed Lato-Light.ttf
	Light []byte
	//go:embed Lato-LightItalic.ttf
	LightItalic []byte
	//go:embed Lato-Regular.ttf
	Regular []byte
	//go:embed Lato-Italic.ttf
	Italic []byte
	//go:embed Lato-Bold.ttf
	Bold []byte
	//go:embed Lato-BoldItalic.ttf
	BoldItalic []byte
	//go:embed Lato-Black.ttf
	Black []byte
	//go:embed Lato-BlackItalic.ttf
	BlackItalic []byte
)
