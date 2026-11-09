package messages

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/uipref"
)

// One-click reactions follow user_customized_quick_reactions_display_feature
// (1 shows them), the custom slots or frequently used emoji, Slack's
// defaults and the preferred skin tone.
func TestQuickReactionsFollowPrefs(t *testing.T) {
	t.Cleanup(uipref.Reset)
	app, _ := newTestApp(t)
	h := coreHost{app: app, th: theme.New(false)}
	r := &row{}
	var q quickBar
	set := func(p store.Prefs, raw map[string]any) {
		uipref.Set(p)
		m := map[string]json.RawMessage{}
		for k, v := range raw {
			b, _ := json.Marshal(v)
			m[k] = b
		}
		app.S.SetRawPrefs(m)
		q.prepare(h, tGen, r)
	}
	set(store.Prefs{}, nil)
	if len(q.names) != 0 {
		t.Fatalf("shown with the pref off: %v", q.names)
	}
	on := store.Prefs{MorePrefs: store.MorePrefs{QuickReactions: true}, SkinTone: "3", FrequentEmoji: []string{"tada", "fire", "smile", "eyes"}}
	set(on, nil)
	if want := []string{"white_check_mark", "eyes", "raised_hands::skin-tone-3"}; !reflect.DeepEqual(q.names, want) {
		t.Fatalf("defaults %v, want %v", q.names, want)
	}
	if q.tip(0) != "Completed" || q.tip(1) != "Taking a look…" || q.tip(2) != "Nicely done" {
		t.Fatalf("tips %q %q %q", q.tip(0), q.tip(1), q.tip(2))
	}
	set(on, map[string]any{"user_customized_quick_reactions_emoji_2": "heart", "user_customized_quick_reactions_has_customized": true})
	if q.names[1] != "heart" || q.tip(0) != ":white_check_mark:" {
		t.Fatalf("custom %v %q", q.names, q.tip(0))
	}
	set(on, map[string]any{"user_customized_quick_reactions_use_frequently_used_emoji": true})
	if want := []string{"tada", "fire", "smile"}; !reflect.DeepEqual(q.names, want) {
		t.Fatalf("frequent %v", q.names)
	}
}
