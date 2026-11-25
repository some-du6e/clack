package composer

import (
	"testing"
	"time"
)

// injectEmojiPlugin patches autocompleteEmoji as HMojis does: it takes the
// original's rows (a promise, as Slack's searcher answers) and puts a row
// of its own first, an emoji only it has, with an image of its own. Its
// row's name says whether it was asked as Slack asks (the typed token, the
// conversation) and got Slack's rows (the cat's item, best name, matches).
func injectEmojiPlugin(channel string) string {
	return `(() => {
const TautPlugin = globalThis.TautPlugin
class InjectEmoji extends TautPlugin {
  static id = 'InjectEmoji'
  static pluginName = 'Inject emoji (test)'
  static description = 'test: adds a row to the emoji autocomplete, as HMojis does'
  static authors = ['test']
  static defaultConfig = { enabled: true }
  start() {
    this.api.redux.patchThunk('autocompleteEmoji', (original) => (...args) => {
      const a = args[0] || {}
      const called = original(...args)
      const finish = (rows) => {
        if (!Array.isArray(rows) || a.query !== ':cat') return rows
        const cat = rows.find((r) => r && r.item && r.item.name === 'cat')
        const asked = a.channelId === '` + channel + `' && a.isThread === false && !a.threadTs && !!cat && cat.id === 'cat' && cat.item.unicode === '1f431' &&
          cat.item.name === 'cat' && cat.emojiBestNameMatchCanonical === 'cat' && JSON.stringify(cat.emojiMatchIndices) === '[0,1,2]'
        const name = asked ? 'hidden_cat' : 'not_as_slack_asks'
        const item = { name, updated: 1, url: 'https://emoji.example/lock.png', id: 'E' + name, keywords: [] }
        // HMojis' buildNativeAutocompleteEntry: the template row, its own item and names
        return [{ ...JSON.parse(JSON.stringify(cat || {})), item, id: item.id, name, emojiBestNameMatch: name,
          emojiBestNameMatchCanonical: name, emojiBestNameMatchLocalized: name, hmojiHidden: true }, ...rows]
      }
      if (called && typeof called.then === 'function') return called.then(finish)
      if (typeof called === 'function') return async (...rest) => finish(await called(...rest))
      return called
    })
  }
  stop() {}
}
return InjectEmoji
})()`
}

// A plugin that patches Slack's autocompleteEmoji thunk (HMojis) changes
// the composer's ":" list: the composer dispatches it with Slack's args,
// its rows in Slack's shape and a promise as Slack's searcher answers, and
// shows what comes back. A row the plugin added draws and inserts its own
// emoji (its name, its image), and the composer's rows follow. It never
// was dispatched, so HMojis' hidden emoji never showed in the list.
func TestTautAutocompleteEmoji(t *testing.T) {
	e := startTautCode(t, []byte(injectEmojiPlugin(tChGen)), nil, nil)
	waitFor(t, 10*time.Second, "autocompleteEmoji patched", func() bool { return currentTaut().ThunkPatched("autocompleteEmoji") })
	ac := newAutocompleteFor(t, e.app)
	names := acSettle(t, ac, ":cat", func(n []string) bool {
		return len(n) > 1 && (n[0] == "hidden_cat" || n[0] == "not_as_slack_asks")
	})
	if names[0] != "hidden_cat" {
		t.Fatalf("the plugin's row is %q (rows %v): it wasn't asked as Slack asks", names[0], names)
	}
	if acIndex(names, "cat") < 1 {
		t.Errorf("the composer's rows after the plugin's: %v", names)
	}
	items, _ := ac.Items()
	if a := items[0].Atom; a == nil || a.Kind != AtomEmoji || a.ID != "hidden_cat" || a.URL != "https://emoji.example/lock.png" || items[0].Avatar != a.URL {
		t.Errorf("the plugin's row draws and inserts %+v (image %q), want :hidden_cat: with its image", a, items[0].Avatar)
	}
}
