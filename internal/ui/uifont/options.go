// Package uifont is Preferences > Appearance > Font: Slack's list of UI
// fonts, where each one comes from (Slack's CDN web fonts or the
// system's), and the faces Clack's theme draws the whole UI with.
//
// The evidence (Slack web client build a84662d2d135, public CDN
// a.slack-edge.com/bv1-13-br, docs/PREFS-AUDIT.md "Font"):
//
//   - client-boot module "wj/S" holds the options (label, value,
//     fontFamily) and module "ofdg" getAllFontOptions puts them in order
//     (Lato first, then OpenDyslexic, Arial, Roboto Mono, Comic Sans,
//     Georgia, Noto Sans, Verdana, Atkinson Hyperlegible Next, then the
//     system font when the OS has one); prefs-dialog's
//     ThemeFontTypefaceContainer passes them through "ofdg".tw, a sort by
//     label, and renders each option in its own fontFamily.
//   - The system font is labelled "{font} (System)" with San Francisco Pro
//     on macOS, Segoe UI on Windows, and Ubuntu only when isUbuntu()
//     (navigator.userAgent contains "Ubuntu", which the desktop app's
//     Electron user agent never does), so Linux has nine options.
//   - setPersistentCustomTypeface stores the whole option object under
//     the localConfig key "client-typeface" (module "4T1V".C3): per
//     device, never a users.prefs write. Clack keeps it in ui.json.
//   - gantry-v2-async-client-boot-shared's Theme sets
//     :root{--font-family-default: <fontFamily>} and toggles
//     body.use-theme-font when the label isn't Lato's; slack-kit's
//     body{font-family:var(--font-family-default)} makes it the whole UI.
//     Code keeps --font-family-monospace ("Slack-Roboto-Mono", Monaco, …)
//     and emoji are images, so neither changes.
//   - The same Theme rewrites --custom-font-weight-bold to 600 and
//     --custom-font-weight-black to 700 for San Francisco Pro (System),
//     Segoe UI (System), Roboto Mono, Noto Sans and Meiryo (plus letter
//     spacing tracking) and for Atkinson Hyperlegible Next (weights
//     only). Slack's CSS sets bold and black text through those variables
//     (2,591 uses against ~50 literal 700/900s).
//   - slack-kit-gantry-v2-styles' @font-face rules serve Slack-Roboto-Mono,
//     Noto Sans and Atkinson Hyperlegible Next as woff2 (variable
//     weights) and Open Dyslexic as OpenType files, each with
//     src:local(☺) first so an installed copy is never used. Arial, Comic
//     Sans MS, Georgia, Verdana and the system font have no @font-face:
//     they are the computer's.
package uifont

import (
	"runtime"
	"sort"
	"strings"
)

// Option is one font choice, the object Slack's select holds and
// setPersistentCustomTypeface stores ("client-typeface").
type Option struct {
	Default    bool   `json:"default,omitempty"`
	FontFamily string `json:"fontFamily,omitempty"`
	Label      string `json:"label"`
	Value      string `json:"value"`
}

// Option values ("wj/S" and "ofdg").
const (
	Lato         = "lato"
	OpenDyslexic = "opendyslexic"
	Arial        = "arial"
	RobotoMono   = "robotomono"
	ComicSans    = "comicsans"
	Georgia      = "georgia"
	NotoSans     = "notosans"
	Verdana      = "verdana"
	Atkinson     = "atkinsonhyperlegiblenext"
	SystemFont   = "system-font"
)

// SystemStack is the system option's fontFamily ("wj/S".fd).
const SystemStack = `-apple-system, BlinkMacSystemFont, "Segoe UI", "Ubuntu", "Noto Sans", Helvetica, Arial, sans-serif, "Apple Color Emoji", "Segoe UI Emoji"`

// all is getAllFontOptions' order for a non-Japanese locale, before the
// system font.
var all = []Option{
	{Default: true, FontFamily: "var(--font-family-lato)", Label: "Lato (Default)", Value: Lato},
	{Label: "OpenDyslexic", Value: OpenDyslexic, FontFamily: "Open Dyslexic"},
	{Label: "Arial", Value: Arial, FontFamily: "Arial"},
	{Label: "Roboto Mono", Value: RobotoMono, FontFamily: "Slack-Roboto-Mono"},
	{Label: "Comic Sans", Value: ComicSans, FontFamily: `"Comic Sans MS", cursive, sans-serif`},
	{Label: "Georgia", Value: Georgia, FontFamily: "Georgia"},
	{Label: "Noto Sans", Value: NotoSans, FontFamily: "Noto Sans, sans-serif"},
	{Label: "Verdana", Value: Verdana, FontFamily: "Verdana"},
	{Label: "Atkinson Hyperlegible Next", Value: Atkinson, FontFamily: "Atkinson Hyperlegible Next"},
}

// SystemName is the system font Slack names on goos ("wj/S".qL): ""
// where it offers none.
func SystemName(goos string) string {
	switch goos {
	case "darwin":
		return "San Francisco Pro"
	case "windows":
		return "Segoe UI"
	}
	return "" // isUbuntu() is false in the desktop app
}

// Options is the select's list on goos, in Slack's order (sorted by
// label, "ofdg".tw).
func Options(goos string) []Option {
	out := append([]Option(nil), all...)
	if n := SystemName(goos); n != "" {
		out = append(out, Option{Label: n + " (System)", Value: SystemFont, FontFamily: SystemStack})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

// Default is Lato, getDefaultFontOption with the list's label.
func Default() Option { return all[0] }

// Find returns goos's option for value (Slack's ik: by value, so a stored
// "system-font" gets this OS's label). ok is false for values the list
// doesn't have; the caller then uses Default, as Slack's select does.
func Find(goos, value string) (Option, bool) {
	for _, o := range Options(goos) {
		if o.Value == value {
			return o, true
		}
	}
	return Default(), false
}

// Current resolves a stored option on this OS.
func Current(stored *Option) Option {
	if stored == nil {
		return Default()
	}
	o, _ := Find(runtime.GOOS, stored.Value)
	return o
}

// Weights are the CSS weights Slack draws bold and black text with in
// font o: --custom-font-weight-bold and --custom-font-weight-black, 700
// and 900 unless Theme rewrites them (the labels it compares).
func Weights(o Option) (bold, black float32) {
	switch o.Label {
	case "San Francisco Pro (System)", "Segoe UI (System)", "Roboto Mono", "Noto Sans", "Meiryo (System)",
		"Atkinson Hyperlegible Next":
		return 600, 700
	}
	return 700, 900
}

// UsesThemeFont is Slack's "UflI".oo: the label isn't Lato's, so body
// gets use-theme-font (and the option's fontFamily).
func UsesThemeFont(o Option) bool { return !strings.Contains(o.Label, "Lato") }

// families splits a CSS font-family list.
func families(stack string) []string {
	var out []string
	for _, f := range strings.Split(stack, ",") {
		f = strings.Trim(strings.TrimSpace(f), `"'`)
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}
