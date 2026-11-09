package popups

import (
	"strings"
	"testing"
)

// The composer's ":" autocomplete (SearchEmoji) inserts and shows the skin
// tone in use, like Slack's: toneable emoji come back as
// "name::skin-tone-N" with that tone's image, others unchanged.
func TestSearchEmojiSkinTone(t *testing.T) {
	epFrecMu.Lock()
	epFrecLoadLocked()
	was := epFrec.SkinTone
	epFrec.SkinTone = 3
	epFrecMu.Unlock()
	defer func() {
		epFrecMu.Lock()
		epFrec.SkinTone = was
		epFrecMu.Unlock()
	}()
	epSharedMu.Lock()
	epShared.setFrecency(nil)
	epSharedMu.Unlock()

	ms := SearchEmoji(nil, "wave", 5)
	if len(ms) == 0 || ms[0].Name != "wave" {
		t.Fatalf("wave: %+v", ms)
	}
	if ms[0].Toned != "wave::skin-tone-3" || !strings.HasSuffix(ms[0].URL, "/1f44b-1f3fc.png") {
		t.Errorf("wave at tone 3: %+v", ms[0])
	}
	ms = SearchEmoji(nil, "rocket", 1)
	if len(ms) != 1 || ms[0].Toned != "rocket" || !strings.HasSuffix(ms[0].URL, "/1f680.png") {
		t.Errorf("rocket has no tones: %+v", ms)
	}
}
