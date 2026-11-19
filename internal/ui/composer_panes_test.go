package ui

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// passComposer patches the composer's pane components and changes nothing.
const passComposer = `(() => {
const TautPlugin = globalThis.TautPlugin
class PassComposer extends TautPlugin {
  static id = 'PassComposer'
  static pluginName = 'Pass composer (test)'
  static description = 'test: patches the composer panes and changes nothing'
  static authors = ['test']
  static defaultConfig = { enabled: true }
  start() {
    const R = globalThis.React
    for (const n of ['MessagePaneInput', 'InputContainer'])
      this.api.patchComponent(n, (Original) => (props) => R.createElement(Original, props))
  }
  stop() {}
}
return PassComposer
})()`

// Each place the shell draws a conversation's composer draws its plugin
// components in that list's slackc pane (layoutConvo through the list's
// PaneID and the composer's LayoutIn, both promoted through mount's
// adapters): a channel open in the main and the split view has a
// MessagePaneInput root in each, and a thread in the side pane and the
// split view an InputContainer root in each. Sharing one, a plugin's
// wrapped prepareAndSendMessage called back the composer drawn last, and
// a message sent from the other was lost (composer's
// TestComposersForOneThreadSendTheirOwn). Synthetic data only.
func TestComposersDrawInTheirListsPanes(t *testing.T) {
	fs, err := fakeslack.Start(benchSize)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	e := benchStart(t, fs)
	e.waitReady()
	e.until("Taut", 60*time.Second, func() bool { return e.a.Taut() != nil })
	tt := e.a.Taut()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	id, err := tt.Import(ctx, []byte(passComposer), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := tt.SetEnabled(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	e.until("the composer panes patched", 20*time.Second, func() bool {
		return slackc.Patched("MessagePaneInput") && slackc.Patched("InputContainer")
	})
	ch := fakeslack.BenchChannelID
	root, _ := fs.PostQuiet(ch, e.c.Authors[0], "a thread for both panes")
	fs.PostReplyQuiet(ch, root, e.c.Authors[1], "its reply")

	panes := func(name, key string, want ...string) {
		t.Helper()
		slices.Sort(want)
		var got []string
		for dl := time.Now().Add(15 * time.Second); time.Now().Before(dl); time.Sleep(10 * time.Millisecond) {
			e.frame()
			if got = slackc.KeyedPanes(name, key); slices.Equal(got, want) {
				return
			}
		}
		t.Errorf("%s %s is drawn in panes %q; want one in each list's: %q", name, key, got, want)
	}
	e.openChannel(ch)
	e.a.OpenSplit("c/" + ch)
	panes("MessagePaneInput", ch+"/", ch, "split:"+ch)

	e.a.OpenThread(ch, root)
	e.a.OpenSplit("c/" + ch + "/t/" + root)
	panes("InputContainer", ch+"/"+root, ch+"/"+root, "split:"+ch+"/"+root)
}
