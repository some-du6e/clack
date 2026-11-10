package uifont

import (
	"log"
	"os"
	"runtime"
	"strings"
	"sync"

	"github.com/go-text/typesetting/fontscan"
)

var defaultGOOS = runtime.GOOS

// File is an installed font file (Index: the member of a collection).
type File struct {
	Path  string
	Index int
}

// SystemFonts lists installed fonts by family.
type SystemFonts interface {
	// Faces lists a family's files; family names compare
	// case-insensitively, ignoring spaces.
	Faces(family string) []File
}

// osFonts is the system font index: go-text's fontscan over the OS's
// font directories (fontscan.DefaultFontDirectories: macOS
// /System/Library/Fonts, /Library/Fonts and ~/Library/Fonts; Windows
// %WINDIR%\Fonts and %LOCALAPPDATA%\Microsoft\Windows\Fonts; Linux the
// XDG and fontconfig directories). Gio's shaper builds the same index for
// its fallback fonts, and fontscan shares it within the process and
// caches it on disk.
var osFonts = sync.OnceValue(func() SystemFonts {
	fm := fontscan.NewFontMap(log.New(discard{}, "", 0))
	dir, _ := os.UserCacheDir()
	if err := fm.UseSystemFonts(dir); err != nil {
		log.Printf("uifont: system fonts: %v", err)
	}
	return scanFonts{fm}
})

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

type scanFonts struct{ fm *fontscan.FontMap }

var scanMu sync.Mutex // FontMap isn't safe for concurrent use

func (s scanFonts) Faces(family string) []File {
	scanMu.Lock()
	locs := s.fm.FindSystemFonts(family)
	scanMu.Unlock()
	out := make([]File, 0, len(locs))
	for _, l := range locs {
		out = append(out, File{Path: l.File, Index: int(l.Index)})
	}
	return out
}

// appleSystemFont is what -apple-system / BlinkMacSystemFont are on
// macOS: the system UI font, San Francisco (SFNS, a variable font with
// wght and opsz axes; the older split Text/Display files before macOS 11).
var appleSystemFont = []string{
	"/System/Library/Fonts/SFNS.ttf",
	"/System/Library/Fonts/SFNSItalic.ttf",
}

// genericFamily maps a CSS generic family to Chromium's default for the
// OS (chrome/app/resources/locale_settings_{mac,win,linux}.grd, which
// Electron's shell/browser/font_defaults.cc copies): sans-serif is
// Helvetica on macOS and Arial elsewhere, serif and the standard font
// Times / Times New Roman, cursive Apple Chancery / Comic Sans MS.
func genericFamily(goos, key string) (string, bool) {
	mac := goos == "darwin"
	switch key {
	case "sans-serif":
		if mac {
			return "Helvetica", true
		}
		return "Arial", true
	case "serif":
		if mac {
			return "Times", true
		}
		return "Times New Roman", true
	case "cursive":
		if mac {
			return "Apple Chancery", true
		}
		return "Comic Sans MS", true
	case "monospace":
		switch goos {
		case "darwin":
			return "Courier", true
		case "windows":
			return "Consolas", true
		}
		return "Monospace", true
	}
	return "", false
}

// metricCompatible are the fonts Chromium's fontconfig lookup accepts in
// place of a missing one on Linux (Skia's SkFontConfigInterface_direct
// kFontEquivMap): Arimo and Liberation Sans for Arial, Tinos and
// Liberation Serif for Times New Roman, Cousine and Liberation Mono for
// Courier New. Any other substitute fontconfig offers is refused, so the
// next family in the CSS list is tried.
func metricCompatible(family string) []string {
	classes := [][]string{
		{"Arial", "Arimo", "Liberation Sans"},
		{"Times New Roman", "Tinos", "Liberation Serif"},
		{"Courier New", "Cousine", "Liberation Mono"},
		{"Cambria", "Caladea"},
		{"Calibri", "Carlito"},
	}
	for _, c := range classes {
		for _, n := range c {
			if strings.EqualFold(n, family) {
				var out []string
				for _, m := range c {
					if !strings.EqualFold(m, family) {
						out = append(out, m)
					}
				}
				return out
			}
		}
	}
	return nil
}

// lastResort is what Chromium draws with when no family of the list is
// there (FontFallbackList::GetFontData): the standard font, then
// FontCache's last-resort font (on Linux the fontconfig match for
// "Sans", any font: fontconfig's 60-latin.conf sans-serif preferences).
func lastResort(goos string) []string {
	switch goos {
	case "darwin":
		return []string{"Times", "Lucida Grande"}
	case "windows":
		return []string{"Times New Roman", "Arial"}
	}
	return []string{"Times New Roman", "Noto Sans", "DejaVu Sans", "Verdana", "Arial", "Nimbus Sans L", "Nimbus Sans", "Helvetica"}
}
