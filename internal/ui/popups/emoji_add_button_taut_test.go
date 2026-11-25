package popups

import (
	"image"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// addHiddenEmoji patches the picker's Add Emoji as HMojis does: a row of
// Slack's button (Original, with its props) and a button of its own. The
// row's data attributes say which props it was given.
const addHiddenEmoji = `(() => {
const TautPlugin = globalThis.TautPlugin
class AddHidden extends TautPlugin {
  static id = 'AddHidden'
  static pluginName = 'Add hidden emoji (test)'
  static description = 'test: a button beside the emoji picker Add Emoji, as HMojis adds one'
  static authors = ['test']
  static defaultConfig = { enabled: true }
  start() {
    const R = globalThis.React
    this.api.patchComponent('AddCustomEmojiButton', (Original) => (props) => {
      const { Button } = this.api.elements
      return R.createElement('span', { className: 'p-emoji_picker__add_custom_emoji hmojis-add-row',
          'data-huddles': String(props.isHuddlesEmojiPicker), 'data-onclick': typeof props.onClick },
        R.createElement(Original, { ...props }),
        R.createElement(Button, { type: 'outline', size: 'small', 'data-qa': 'hmojis_add_hidden_emoji', onClick: () => {} }, 'Add hidden emoji'))
    })
  }
  stop() {}
}
return AddHidden
})()`

// The emoji picker's Add Emoji is Slack's AddCustomEmojiButton boundary
// (EmojiPickerFooter's, where no emoji is previewed, given the picker's
// onClick and isHuddlesEmojiPicker), so a plugin's patch draws there:
// HMojis' "Add hidden emoji" beside Slack's button. The picker drew its
// button natively whatever was patched.
func TestTautAddCustomEmojiButton(t *testing.T) {
	e := slNewEnvIn(t, "")
	id, err := e.tt.Import(e.ctx(t), []byte(addHiddenEmoji), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.tt.SetEnabled(e.ctx(t), id, true); err != nil {
		t.Fatal(err)
	}
	slUntil(t, 10*time.Second, "AddCustomEmojiButton patched", nil, func() bool { return slackc.Patched("AddCustomEmojiButton") })

	p := NewEmojiPicker(e.h, nil)
	p.SetAnchorRect(image.Rect(20, 548, 60, 556))
	// The plugin's own button is host-tree text, which slackc shapes
	// itself (theme.DrawHook sees only theme.Text): its tree says it's
	// there, and Slack's button, the native in it, draws through theme.Text.
	var mu sync.Mutex
	var drawn []string
	frame := slFrame(p)
	slUntil(t, 10*time.Second, "the plugin's row in the picker's footer", func() {
		mu.Lock()
		drawn = drawn[:0]
		mu.Unlock()
		theme.DrawHook = func(s string) { mu.Lock(); drawn = append(drawn, s); mu.Unlock() }
		frame()
		theme.DrawHook = nil
	}, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return strings.Contains(e.rec.raw("AddCustomEmojiButton"), "hmojis_add_hidden_emoji") && slices.Contains(drawn, "Add Emoji")
	})
	raw := e.rec.raw("AddCustomEmojiButton")
	for _, want := range []string{`Add hidden emoji`, `"data-huddles":"false"`, `"data-onclick":"function"`} {
		if !strings.Contains(raw, want) {
			t.Errorf("the plugin's tree lacks %s (its props): %s", want, raw)
		}
	}
}
