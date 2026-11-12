package shortcut

import (
	"slices"
	"testing"

	"gioui.org/io/key"
)

// setGOOS formats and binds as goos for the rest of the test.
func setGOOS(t *testing.T, goos string) {
	prev := GOOS
	GOOS = goos
	t.Cleanup(func() { GOOS = prev })
}

// Format matches the labels Slack's web client writes per platform:
// "⌘C" / "Ctrl+C", "⇧⌘S" / "Ctrl+Shift+S", "⌘F" / "Ctrl+F", the
// shortcuts dialog's Option→Alt and Cmd→Ctrl swap, and ^1 for the More
// menu's tab numbers on macOS.
func TestFormatPerOS(t *testing.T) {
	cases := []struct {
		combo, mac, win, linux string
	}{
		{"Mod+C", "⌘C", "Ctrl+C", "Ctrl+C"},
		{"Mod+Shift+S", "⇧⌘S", "Ctrl+Shift+S", "Ctrl+Shift+S"},
		{"Mod+F", "⌘F", "Ctrl+F", "Ctrl+F"},
		{"Mod+Shift+K", "⇧⌘K", "Ctrl+Shift+K", "Ctrl+Shift+K"},
		{"Mod+,", "⌘,", "Ctrl+,", "Ctrl+,"},
		{"Mod+.", "⌘.", "Ctrl+.", "Ctrl+."},
		{"Mod++", "⌘+", "Ctrl++", "Ctrl++"},
		{"Mod+Shift+Enter", "⇧⌘Enter", "Ctrl+Shift+Enter", "Ctrl+Shift+Enter"},
		{"Mod+Alt+Shift+C", "⌥⇧⌘C", "Ctrl+Alt+Shift+C", "Ctrl+Alt+Shift+C"},
		{"Mod+Shift+7", "⇧⌘7", "Ctrl+Shift+7", "Ctrl+Shift+7"},
		{"Ctrl+1", "⌃1", "Ctrl+1", "Ctrl+1"},
		{"Ctrl+Mod+F", "⌃⌘F", "Ctrl+F", "Ctrl+F"},
		{"Alt+Up", "⌥↑", "Alt+↑", "Alt+↑"},
		{"Alt+Shift+Down", "⌥⇧↓", "Alt+Shift+↓", "Alt+Shift+↓"},
		{"Shift+Esc", "⇧Esc", "Shift+Esc", "Shift+Esc"},
		{"Cmd+k", "⌘K", "Ctrl+K", "Ctrl+K"}, // Cmd is Mod, as in Slack's key_Cmd
		{"Esc", "Esc", "Esc", "Esc"},
		{"L", "L", "L", "L"},
		{"delete", "delete", "delete", "delete"},                         // Slack's Delete message… label
		{"Ctrl Shift 1", "Ctrl Shift 1", "Ctrl Shift 1", "Ctrl Shift 1"}, // not a combo: as given
		{"", "", "", ""},
	}
	for _, c := range cases {
		for _, os := range []struct{ goos, want string }{{"darwin", c.mac}, {"windows", c.win}, {"linux", c.linux}} {
			if got := FormatFor(os.goos, c.combo); got != os.want {
				t.Errorf("FormatFor(%s, %q) = %q, want %q", os.goos, c.combo, got, os.want)
			}
			setGOOS(t, os.goos)
			if got := Format(c.combo); got != os.want {
				t.Errorf("Format(%q) on %s = %q, want %q", c.combo, os.goos, got, os.want)
			}
		}
	}
}

// FormatAsWritten keeps the written modifier order on macOS, for
// Slack's literal "⌘⇧Enter" (the + menu's Text snippet); elsewhere it is
// Format.
func TestFormatAsWritten(t *testing.T) {
	cases := []struct{ combo, mac, win string }{
		{"Mod+Shift+Enter", "⌘⇧Enter", "Ctrl+Shift+Enter"},
		{"Shift+Mod+Enter", "⇧⌘Enter", "Ctrl+Shift+Enter"},
		{"Mod+O", "⌘O", "Ctrl+O"},
		{"Mod+Alt+K", "⌘⌥K", "Ctrl+Alt+K"},
		{"Mod++", "⌘+", "Ctrl++"},
		{"Esc", "Esc", "Esc"},
		{"Ctrl Shift 1", "Ctrl Shift 1", "Ctrl Shift 1"},
	}
	for _, c := range cases {
		if got := FormatAsWrittenFor("darwin", c.combo); got != c.mac {
			t.Errorf("FormatAsWrittenFor(darwin, %q) = %q, want %q", c.combo, got, c.mac)
		}
		if got := FormatAsWrittenFor("windows", c.combo); got != c.win {
			t.Errorf("FormatAsWrittenFor(windows, %q) = %q, want %q", c.combo, got, c.win)
		}
		setGOOS(t, "darwin")
		if got := FormatAsWritten(c.combo); got != c.mac {
			t.Errorf("FormatAsWritten(%q) on darwin = %q, want %q", c.combo, got, c.mac)
		}
	}
}

// Chips follow Slack's KeyboardKeysTooltip: ⌘, Option, Shift, key on
// macOS; Ctrl, Alt, Shift, key elsewhere.
func TestChipsPerOS(t *testing.T) {
	cases := []struct {
		combo      string
		mac, other []string
	}{
		{"Mod+B", []string{"⌘", "B"}, []string{"Ctrl", "B"}},
		{"Mod+Shift+X", []string{"⌘", "Shift", "X"}, []string{"Ctrl", "Shift", "X"}},
		{"Mod+Alt+Shift+C", []string{"⌘", "Option", "Shift", "C"}, []string{"Ctrl", "Alt", "Shift", "C"}},
		{"Esc", []string{"Esc"}, []string{"Esc"}},
		{"-", []string{"-"}, []string{"-"}},
		{"+", []string{"+"}, []string{"+"}},
		{"", nil, nil},
	}
	for _, c := range cases {
		if got := ChipsFor("darwin", c.combo); !slices.Equal(got, c.mac) {
			t.Errorf("ChipsFor(darwin, %q) = %q, want %q", c.combo, got, c.mac)
		}
		for _, goos := range []string{"windows", "linux"} {
			if got := ChipsFor(goos, c.combo); !slices.Equal(got, c.other) {
				t.Errorf("ChipsFor(%s, %q) = %q, want %q", goos, c.combo, got, c.other)
			}
		}
	}
}

// Parse binds Mod to ⌘ on macOS and Ctrl elsewhere; Ctrl is always the
// Control key.
func TestParsePerOS(t *testing.T) {
	cases := []struct {
		goos, combo string
		name        key.Name
		mods        key.Modifiers
	}{
		{"darwin", "Mod+Shift+A", "A", key.ModCommand | key.ModShift},
		{"linux", "Mod+Shift+A", "A", key.ModCtrl | key.ModShift},
		{"windows", "Mod+K", "K", key.ModCtrl},
		{"darwin", "Ctrl+1", "1", key.ModCtrl},
		{"darwin", "Alt+Up", key.NameUpArrow, key.ModAlt},
		{"linux", "Mod+[", "[", key.ModCtrl},
		{"linux", "Mod++", "+", key.ModCtrl},
		{"linux", "Shift+Esc", key.NameEscape, key.ModShift},
		{"linux", "Mod+Enter", key.NameReturn, key.ModCtrl},
		{"linux", "Mod+,", ",", key.ModCtrl},
	}
	for _, c := range cases {
		setGOOS(t, c.goos)
		if n, m := Parse(c.combo); n != c.name || m != c.mods {
			t.Errorf("Parse(%q) on %s = %q %v, want %q %v", c.combo, c.goos, n, m, c.name, c.mods)
		}
	}
}

// ID and EventID are Slack's kE2I: a registered key and a press agree on
// the same id, Mod is ⌘ on macOS (Control matches nothing there) and
// Ctrl elsewhere, and Shift only counts for letters and named keys.
func TestMenuKeyIDs(t *testing.T) {
	for k, want := range map[string]string{
		"l": "l", "L": "l", "mod+c": "mod+c", "mod+shift+s": "mod+shift+s",
		"Delete": "delete", "del": "del", "Backspace": "backspace", "ctrl+c": "", "mod+": "",
	} {
		if got := ID(k); got != want {
			t.Errorf("ID(%q) = %q, want %q", k, got, want)
		}
	}
	cases := []struct {
		goos string
		name key.Name
		mods key.Modifiers
		want string
	}{
		{"darwin", "L", 0, "l"},
		{"darwin", "L", key.ModShift, "shift+l"},
		{"darwin", "C", key.ModCommand, "mod+c"},
		{"darwin", "C", key.ModCtrl, ""},
		{"darwin", "S", key.ModCommand | key.ModShift, "mod+shift+s"},
		{"darwin", key.NameDeleteBackward, 0, "backspace"},
		{"darwin", key.NameDeleteForward, 0, "delete"},
		{"linux", "C", key.ModCtrl, "mod+c"},
		{"linux", "C", key.ModSuper, ""},
		{"windows", "L", key.ModAlt, "alt+l"},
		{"linux", "/", key.ModShift, "/"},
	}
	for _, c := range cases {
		setGOOS(t, c.goos)
		if got := EventID(c.name, c.mods); got != c.want {
			t.Errorf("EventID(%q, %v) on %s = %q, want %q", c.name, c.mods, c.goos, got, c.want)
		}
		if c.want == "" || c.mods == key.ModShift && c.want == string(c.name) {
			continue // a shifted symbol: its id carries no shift
		}
		// the filter for the id hears this very press
		n, m, ok := Filter(c.want)
		if !ok || n != c.name || m != c.mods {
			t.Errorf("Filter(%q) on %s = %q %v, want %q %v", c.want, c.goos, n, m, c.name, c.mods)
		}
	}
}
