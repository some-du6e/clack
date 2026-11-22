package messages

// A plugin's ReactionBar tree from before a reaction went (review of the
// lost-click sweep): slackc resolves an "@" node's native by its slot,
// kept per prop path, so a tree JS hasn't re-rendered (or one a plugin
// keeps) still calls the natives at the paths the new render no longer
// writes: a dropped pill's. Its group index then pointed past the row's
// groups (an index-out-of-range panic in the frame) or at another emoji's.
// A dropped pill's natives now draw nothing. The plugin here keeps the
// first children it sees with three pills, the way a memoized plugin
// would; then two of the three reactions go. Synthetic.

import (
	"strings"
	"testing"
	"time"
)

const freezeBarPlugin = `(() => {
const TautPlugin = globalThis.TautPlugin
class FreezeBar extends TautPlugin {
  static id = 'FreezeBar'
  static pluginName = 'Freeze Bar'
  static description = 'test: keeps the first reaction bar children it sees with three pills'
  static authors = ['test']
  static defaultConfig = { enabled: true }
  start() {
    this.api.patchComponent('ReactionBar', (Original) => (props) => {
      const kept = React.useRef(null)
      if (kept.current === null && Array.isArray(props.children) && props.children.length >= 4) kept.current = props.children
      return React.createElement(Original, Object.assign({}, props, { children: kept.current || props.children }))
    })
  }
}
return FreezeBar
})()`

func TestReactionPillStaleTreeAfterRemoval(t *testing.T) {
	app, _, tt, _, h := pluginEnv(t)
	install(t, tt, []byte(freezeBarPlugin))
	v := NewChannelView(h, tGen)
	v.Preview = true
	f := frameFn(v)
	until(t, 5*time.Second, "the row", f, func() bool { return v.s.rows[tsFair] != nil })
	react := func(typ, user, name string) {
		injectEvent(app, tGen, typ, "", map[string]any{"user": user, "reaction": name,
			"item": map[string]any{"type": "message", "channel": tGen, "ts": tsFair}})
	}
	react("reaction_added", "UANA00001", "eyes")
	react("reaction_added", "UBEN00001", "tada")
	react("reaction_added", "UCAR00001", "rocket")
	until(t, 5*time.Second, "three pills in the plugin's bar", f, func() bool {
		r := v.s.rows[tsFair]
		return r != nil && len(r.groups) == 3 && len(r.pills) == 3
	})
	for i := 0; i < 10; i++ { // the plugin's tree with the three pills, kept
		f()
		time.Sleep(10 * time.Millisecond)
	}
	react("reaction_removed", "UBEN00001", "tada")
	react("reaction_removed", "UCAR00001", "rocket")
	until(t, 5*time.Second, "one group left", f, func() bool {
		r := v.s.rows[tsFair]
		return r != nil && len(r.groups) == 1
	})
	// frames over the kept tree: its third child is the dropped rocket
	// pill's (a path the new render doesn't write)
	for i := 0; i < 30; i++ {
		f()
		time.Sleep(10 * time.Millisecond)
	}
	r := v.s.rows[tsFair]
	if len(r.pills) != 1 || len(r.groups) != 1 || !strings.Contains(r.groups[0].name, "eyes") {
		t.Errorf("after the removals the row has %d pills (%+v), want eyes alone", len(r.pills), r.groups)
	}
}
