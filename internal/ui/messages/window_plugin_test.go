package messages

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// TestWindowRowsDrawPluginViews: a jump window's rows draw the view a
// plugin patching the messages slice makes of them, as the newest rows
// do: Slack's messages store holds every channelHistory slice's messages.
// The plugin mirror held the timeline and the threads only, so a window's
// rows drew their source: here a word the plugin masks showed in clear.
// Synthetic history on the fake; the plugin masks "job".
func TestWindowRowsDrawPluginViews(t *testing.T) {
	app, fs, _ := shotsApp(t)
	const ch = "C0WINPLUG1"
	app.S.PutConv(slack.Conversation{ID: ch, Name: "careers", IsChannel: true, IsMember: true, NumMembers: 12})
	base := time.Now().Add(-7 * time.Hour).Unix()
	var tss []string
	for i := 0; i < 240; i++ {
		ts := fmt.Sprintf("%d.000100", base+int64(i)*90)
		fs.PostQuietAt(ch, "UBEN00001", fmt.Sprintf("the job, message %d", i), ts)
		tss = append(tss, ts)
	}
	patchedEnv(t, app, maskJS(ch, false))
	until(t, 5*time.Second, "the messages slice patched", func() {}, func() bool { return tautOf().SlicePatched("messages") })
	h := &ctxHost{coreHost: coreHost{app: app, th: theme.New(false)}}
	v := NewChannelView(h, ch)
	rt := newRouted(v, 900, 700)
	masked := func(ts string) bool { return strings.Contains(v.RowText(ts), "***") }
	until(t, 15*time.Second, "the newest message masked", rt.frame, func() bool { return masked(tss[239]) })
	v.Highlight(tss[40])
	until(t, 15*time.Second, "the window's message 40 masked", rt.frame, func() bool { return v.InWindow() && masked(tss[40]) })
}
