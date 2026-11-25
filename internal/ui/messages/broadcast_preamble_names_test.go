package messages

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// preamblePlugin patches the channel message header under each of names,
// each patch adding a .preamble-mark span to its children, with no guard
// of its own against a second one (Slick's plugins have one).
func preamblePlugin(id string, names ...string) []byte {
	list, _ := json.Marshal(names)
	return pluginJS(`
class ` + id + ` extends TautPlugin {
  static id = '` + id + `'
  static pluginName = '` + id + `'
  static description = 'test: patches the channel message header by its names'
  static authors = ['test']
  static defaultConfig = { enabled: true }
  start() {
    const R = React
    for (const name of ` + string(list) + `) {
      this.api.patchComponent(name, (O) => (p) => {
        const kids = R.Children.toArray(p.children)
        kids.push(R.createElement('span', { key: 'mark-' + name, className: 'preamble-mark' }, name))
        return R.createElement(O, p, kids)
      })
    }
  }
}
return ` + id)
}

// Slack build 889ac449a32d renamed the channel message header
// BaseBroadcastPreamble to BroadcastPreamble, and the plugins followed:
// Taut's User Pronouns 3.7.6 patches the new name (3.7.5 the old one), and
// Slick's User Pronouns and Local Time patch both. Clack's header is
// BroadcastPreamble and answers to the old name too, so each draws in it,
// once: a plugin's patch of the old name gives way to its patch of the new
// one, as Slack draws one of the two. The header answered to the old name
// alone, so Taut 3.7.6's pronouns were gone from channel rows.
func TestBroadcastPreambleAnswersBothNames(t *testing.T) {
	for _, c := range []struct {
		name  string
		names []string
	}{
		{"the new name (Taut's User Pronouns 3.7.6)", []string{"BroadcastPreamble"}},
		{"the old name (3.7.5)", []string{"BaseBroadcastPreamble"}},
		{"both names (Slick's User Pronouns and Local Time)", []string{"BroadcastPreamble", "BaseBroadcastPreamble"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, _, tt, u, h := pluginEnv(t)
			install(t, tt, preamblePlugin("PreambleNames", c.names...))
			v := NewChannelView(h, tGen)
			v.Preview = true
			f := frameFn(v)
			until(t, 8*time.Second, "the plugin's mark in a channel row's header", f, func() bool { return u.anyTree("preamble-mark") })
			for i := 0; i < 5; i++ { // every visible header drawn and delivered again
				f()
			}
			time.Sleep(100 * time.Millisecond)
			f()
			u.mu.Lock()
			defer u.mu.Unlock()
			headers := 0
			for root, tr := range u.trees {
				n := strings.Count(string(tr), "preamble-mark")
				if n == 0 {
					continue
				}
				headers++
				if n != 1 {
					t.Errorf("root %d: %d marks in one header, want 1: %s", root, n, tr)
				}
			}
			if headers == 0 {
				t.Error("no header drew the plugin's mark")
			}
		})
	}
}
