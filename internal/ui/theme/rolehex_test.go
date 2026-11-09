package theme

import "testing"

// The Custom theme tab's four hexes for Slack's default Aubergine theme in
// light mode (darker sidebars on), as the live dialog shows them
// (2026-11-08).
func TestRoleHexAubergine(t *testing.T) {
	th := DefaultIATheme
	for role, want := range map[string]string{
		"primary": "#611F69", "highlight1": "#F9EDFF", "highlight2": "#4CC894", "important": "#EABDFB",
	} {
		if got := th.RoleHex(role, false); got != want {
			t.Errorf("%s = %s, want %s", role, got, want)
		}
	}
	th.UseCustomHex = true
	th.Primary.Custom = "#123abc"
	if got := th.RoleHex("primary", false); got != "#123ABC" {
		t.Errorf("custom primary = %s", got)
	}
	for in, want := range map[string]string{"#abc": "#AABBCC", "611f69": "#611F69", " #4CC894 ": "#4CC894"} {
		if got, ok := ParseHexColor(in); !ok || got != want {
			t.Errorf("ParseHexColor(%q) = %q %v, want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "#12", "#GGGGGG", "#1234567"} {
		if _, ok := ParseHexColor(in); ok {
			t.Errorf("ParseHexColor(%q) accepted", in)
		}
	}
}
