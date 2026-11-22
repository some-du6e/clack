package logtag

import (
	"regexp"
	"strings"
	"testing"
)

// TestID: an id's tag is its kind and six hex digits (docs/INSPECT.md),
// the same for the same id within a run, different for another, never
// the id itself; "-" for none. Synthetic ids.
func TestID(t *testing.T) {
	tag := regexp.MustCompile(`^(ch|dm|user|bot|team|file|ts|id):[0-9a-f]{6}$`)
	for _, c := range []struct{ id, kind string }{
		{"C0THPUBLIC1", "ch"}, {"G0PRIVATE01", "ch"}, {"D0THDIRECT1", "dm"}, {"U0FAKE00001", "user"},
		{"W0ORGUSER01", "user"}, {"B0DIGEST001", "bot"}, {"T0FAKETEAM", "team"}, {"E0ENTERPRS1", "team"},
		{"F0FILE00001", "file"}, {"1762516060.000100", "ts"}, {"permalink", "id"},
	} {
		got := ID(c.id)
		if !tag.MatchString(got) || !strings.HasPrefix(got, c.kind+":") || strings.Contains(got, c.id) {
			t.Errorf("ID(%s) = %q, want %s:<6 hex>", c.id, got, c.kind)
		}
		if again := ID(c.id); again != got {
			t.Errorf("ID(%s) changed within the run: %q then %q", c.id, got, again)
		}
	}
	if ID("C0THPUBLIC1") == ID("C0THPRIVAT1") {
		t.Errorf("two channels share a tag")
	}
	if got := ID(""); got != "-" {
		t.Errorf("ID(\"\") = %q, want -", got)
	}
	if got, want := IDs([]string{"C0THPUBLIC1", "D0THDIRECT1"}), ID("C0THPUBLIC1")+" "+ID("D0THDIRECT1"); got != want {
		t.Errorf("IDs = %q, want %q", got, want)
	}
}
