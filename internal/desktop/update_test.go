package desktop

import "testing"

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		latest, current string
		want            bool
	}{
		{"v0.2.0", "v0.1.0", true},
		{"v0.1.0", "v0.1.0", false},
		{"v0.1.0", "v0.2.0", false},
		{"v1.0.0", "v0.99.99", true},
		{"v0.10.0", "v0.9.0", true},
		{"v0.1.0", "v0.1.0-rc.1", true},
		{"v0.1.0-rc.2", "v0.1.0-rc.1", true},
		{"v0.1.0-rc.10", "v0.1.0-rc.9", true},
		{"v0.1.0-rc.1", "v0.1.0", false},
		{"v0.1.0-beta", "v0.1.0-alpha", true},
		{"v0.1.0-alpha.1", "v0.1.0-alpha", true},
		{"v0.1.0+build.5", "v0.1.0", false},
		{"v0.2.0", "dev", false},
		{"nightly", "v0.1.0", false},
		{"v0.2", "v0.1.0", false},
	} {
		if got := Newer(c.latest, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
	if Released("dev") || !Released("v0.0.0-test.3") {
		t.Fatal("Released")
	}
}
