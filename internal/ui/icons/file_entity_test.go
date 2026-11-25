package icons

import "testing"

// D0WB's map: a file type's SvgIcon and palette, "generic" (file, ocean)
// for a type it doesn't know (V7l1), and every icon it names is drawn as
// Slack's: registered under its own name, or an alias to the glyph
// slackRef holds to it (bulleted-list's list-bullet), never another
// glyph by alias or a stripped suffix (document drew file's page).
func TestFileEntityIcon(t *testing.T) {
	for ft, want := range map[string][2]string{
		"ai": {"brand-adobe-ai-filled", "tangerine"}, "psd": {"brand-adobe-ps-filled", "ocean"},
		"docx": {"brand-microsoft-word-filled", "ocean"}, "key": {"presentation", "ocean"},
		"pdf": {"pdf-file-filled", "flamingo"}, "mp4": {"video-filled", "mojito"}, "zip": {"zip", "ocean"},
		"generic": {"file", "ocean"}, "no-such-type": {"file", "ocean"},
	} {
		if ic, p := FileEntityIcon(ft); ic != want[0] || p != want[1] {
			t.Errorf("%s: %s on %s, want %s on %s", ft, ic, p, want[0], want[1])
		}
	}
	for ft, e := range fileEntityIcons {
		n := e[0]
		if Has(n) {
			continue
		}
		if m, ok := slackIconNames[n]; ok && slackRef[m] == n {
			continue // the alias's glyph is held to Slack's n
		}
		t.Errorf("%s's icon %q isn't the set's own (%q draws instead)", ft, n, Slack(n))
	}
	if FileEntityShade("aubergine") != "60" || FileEntityShade("gray") != "60" || FileEntityShade("ocean") != "50" {
		t.Error("ljGw's shades: aubergine and gray 60, the rest 50")
	}
}
