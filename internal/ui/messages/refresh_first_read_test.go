package messages

// A plugin-wide refresh (api.redux.refresh: Message Logger after every
// edit or delete it logs in any channel, ShowRealUser after each sender
// lookup) changes no message, so the views cached before it are still
// JS's copies of their messages' revisions. A row's first read (a list
// built again: a channel shown in a second pane, listCache's eviction past
// 80 lists) waited on it all the same, and drew its source, the masked
// word in clear and the relay bot where the person was, until JS answered
// again. Synthetic data on internal/fakeslack.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// refreshingMaskJS is maskJS's mask with the plugin's refresh reachable
// from the test (globalThis.__testRefresh), as Message Logger calls it.
func refreshingMaskJS(ch string) []byte {
	return pluginJS(`
class MaskAndRefresh extends TautPlugin {
  static id = 'MaskAndRefresh'
  static pluginName = 'Mask and refresh (test)'
  static description = 'test: masks a word in the messages slice and refreshes on demand'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    const fix = (t) => (typeof t === 'string' ? t.replace(/job/g, '***') : t)
    const els = (list) => (Array.isArray(list) ? list.map((e) => (e && e.type === 'text' ? { ...e, text: fix(e.text) } : e && e.elements ? { ...e, elements: els(e.elements) } : e)) : list)
    const mask = (ts, m) => (m && typeof m.text === 'string' && m.text.includes('job') ? { ...m, text: fix(m.text), blocks: els(m.blocks) } : m)
    const { patchSlice, mapEntries } = this.api.redux
    patchSlice('messages', (ch, bucket) => (ch === '` + ch + `' && bucket && typeof bucket === 'object' ? mapEntries(bucket, mask) : bucket))
    globalThis.__testRefresh = () => this.api.redux.refresh()
  }
}
return MaskAndRefresh`)
}

func TestFirstReadAfterARefreshDrawsTheView(t *testing.T) {
	app, _, _ := shotsApp(t)
	const ch = "C0REFRESH1"
	app.S.PutConv(slack.Conversation{ID: ch, Name: "fair", IsChannel: true, IsMember: true, NumMembers: 12})
	a := fmt.Sprintf("%d.000100", time.Now().Add(-time.Hour).Unix())
	app.S.SetLatest(ch, []slack.Message{{Type: "message", TS: a, User: "UANA00001", Text: "the job fair is today",
		Blocks: shotRich(shotSection("the job fair is today"))}}, false)
	patchedEnv(t, app, refreshingMaskJS(ch))
	until(t, 5*time.Second, "the messages slice patched", func() {}, func() bool { return tautOf().SlicePatched("messages") })
	h := coreHost{app: app, th: theme.New(false)}
	first := NewChannelView(h, ch)
	frame := frameFn(first)
	until(t, 10*time.Second, "the masked row", frame, func() bool {
		r := first.s.rows[a]
		return r != nil && r.viewed && strings.Contains(rowText(r), "the *** fair is today")
	})
	for i := 0; i < 20; i++ {
		frame()
		time.Sleep(5 * time.Millisecond)
	}
	rt, err := tautOf().JS()
	if err != nil {
		t.Fatal(err)
	}
	// the plugin refreshes (an edit it logged elsewhere), and JS is busy
	// right after, so the views it asks for again don't come back yet
	if err := rt.Do(func(js *jsrt.Ctx) error {
		_, err := js.Global().Get("__testRefresh").Call()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	hold := make(chan struct{})
	rt.Post(func(*jsrt.Ctx) { <-hold })
	defer close(hold)
	time.Sleep(30 * time.Millisecond)
	// the channel shows in a second pane: its rows are new, over the view
	// cached before the refresh
	split := NewChannelViewIn(h, ch, "split")
	frameFn(split)()
	r := split.s.rows[a]
	if r == nil || itemHeight(split.s, a) <= 0 {
		t.Fatal("the second pane's first frame drew no row")
	}
	if got := rowText(r); !strings.Contains(got, "the *** fair is today") {
		t.Errorf("the second pane's first frame draws %q; want the plugins' view cached before a refresh that changed no message", got)
	}
}
