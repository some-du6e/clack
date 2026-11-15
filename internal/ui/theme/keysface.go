package theme

import (
	_ "embed"

	"gioui.org/font"
)

// Mac key symbols in shortcut hints.
//
// On macOS shortcuts are written with AppKit's glyphs: "⌃⌥⇧⌘D" (the
// inspect tool), "⇧⌘K", "⌘⇧Enter", Slack's key chips. Lato has none of
// them, and Gio's system-font fallback found ⇧ and ⌘ on macOS but not ⌃
// or ⌥, so the inspect toast read "□□⇧⌘D" (α.85). Chrome draws them from
// a system font; Clack bundles them instead: KeysFamily is a 3 KB subset
// of Inter (OFL, keysface-OFL.txt; keysface_gen.py builds it), whose key
// symbols follow Apple's, and Typeface lists it after the UI font's
// families. A UI font that has its own (San Francisco, Segoe UI) still
// wins; any other resolves these runes here, on every platform, before a
// system font is tried.

// KeysFamily is the key-symbol face's family (last in Typeface).
const KeysFamily = "Clack Keys"

var (
	//go:embed keysface-regular.ttf
	keysRegularTTF []byte
	//go:embed keysface-bold.ttf
	keysBoldTTF []byte
)

// keysFaces are KeysFamily's faces: Regular and Bold. Gio picks the
// nearest for other weights and styles (Black takes Bold; italics use
// the upright symbols).
func keysFaces() []font.FontFace {
	return []font.FontFace{
		{Font: font.Font{Typeface: KeysFamily, Weight: font.Normal}, Face: parseFace(keysRegularTTF)},
		{Font: font.Font{Typeface: KeysFamily, Weight: font.Bold}, Face: parseFace(keysBoldTTF)},
	}
}
