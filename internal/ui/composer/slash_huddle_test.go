package composer

import (
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// huddleHost is a host with the ui root's /huddle.
type huddleHost struct {
	slickHost
	started []string
}

func (h *huddleHost) SlashHuddle(ch string) { h.started = append(h.started, ch) }

// /huddle starts or joins the huddle in the conversation (Slack's
// startHuddle, through the ui root) instead of going to chat.command.
func TestSlashHuddle(t *testing.T) {
	app, fs := syntheticApp(t, func(*store.Store) {})
	stubTyping(t)
	h := &huddleHost{slickHost: slickHost{app: app, th: theme.New(false)}}
	c := New(h, tChGen, "")
	t.Cleanup(c.Close)
	d := NewDoc()
	d.Insert(0, "/huddle", Attr{})
	c.ed.SetDoc(d)
	c.submit()
	time.Sleep(100 * time.Millisecond)
	if len(h.started) != 1 || h.started[0] != tChGen {
		t.Fatalf("SlashHuddle calls %q", h.started)
	}
	if n := len(fs.called("chat.command")); n != 0 {
		t.Fatalf("%d chat.command calls for /huddle", n)
	}
	if !c.ed.Doc().Empty() {
		t.Fatal("the composer kept /huddle")
	}
}
