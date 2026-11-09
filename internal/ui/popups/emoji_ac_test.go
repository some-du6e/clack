package popups

import (
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/acrank"
	"github.com/some-du6e/yoloslackgoclient/internal/frecency"
)

// The ":" list in Slack's order (scoreEmojiNameAgainstQuery, the model)
// at the skin tone in use; a frecent emoji climbs to the top.
func TestAutocompleteEmoji(t *testing.T) {
	epToneMu.Lock()
	was := epToneSet
	epToneSet = 3
	epToneMu.Unlock()
	defer func() {
		epToneMu.Lock()
		epToneSet = was
		epToneMu.Unlock()
	}()
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	o := acrank.Options{Now: now, EmojiExactBoost: true}
	ms := AutocompleteEmoji(nil, "wave", o)
	if len(ms) == 0 || ms[0].Name != "wave" || ms[0].Toned != "wave::skin-tone-3" || !strings.HasSuffix(ms[0].URL, "/1f44b-1f3fc.png") {
		t.Fatalf("wave at tone 3 first: %+v", ms)
	}
	ms = AutocompleteEmoji(nil, "smi", o)
	names := make([]string, len(ms))
	for i, m := range ms {
		names[i] = m.Name
	}
	if len(names) < 3 || names[0] != "smile" {
		t.Errorf("tightest prefix first: %v", names)
	}
	f := frecency.Parse(nil)
	for i := 0; i < 5; i++ {
		f.Record("E1f605", ":smi", now.Add(-time.Duration(i)*time.Minute)) // sweat_smile
	}
	o.Frecency = f
	if ms := AutocompleteEmoji(nil, "smi", o); len(ms) == 0 || ms[0].Name != "sweat_smile" {
		t.Errorf("frecent first: %+v", ms[:min(3, len(ms))])
	}
	if len(AutocompleteEmoji(nil, "smi", o)) > 25 {
		t.Error("25 rows at most")
	}
}
