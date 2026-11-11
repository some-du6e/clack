package ui

import (
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// unarchive_channel_banner_polish (boot-render bgYM): a viewer with
// UNARCHIVE_CHANNEL gets the archived footer's Unarchive link
// (conversations.unarchive); with the experiment off the link isn't there.
func TestArchivedFooterUnarchive(t *testing.T) {
	e, fs := previewEnv(t)
	ch := fakeslack.ArchivedChannelID
	fs.Permissions = map[string][]string{ch: {"UNARCHIVE_CHANNEL"}}
	var calls atomic.Int32
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "conversations.unarchive" && f.Get("channel") == ch {
			calls.Add(1)
			return map[string]any{"ok": true}, true
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetHook(nil) })
	svc := e.a.data.core
	svc.SetExperimentGroup("unarchive_channel_banner_polish", "off")
	e.openChannel(ch)
	var c *convo
	e.until("archived bar", 20*time.Second, func() bool {
		c = e.a.convoFor(ch, "")
		return c != nil && e.a.footerFor(c) == footArchived && c.jp != nil
	})
	e.until("the permission", 10*time.Second, func() bool { return svc.CanUnarchive(ch) })
	c.jp.unarchive.Click()
	e.settle(300 * time.Millisecond)
	if n := calls.Load(); n != 0 {
		t.Fatalf("off: the footer unarchived (%d calls)", n)
	}
	svc.SetExperimentGroup("unarchive_channel_banner_polish", "on")
	e.settle(300 * time.Millisecond)
	e.shot("channel-archived-unarchive")
	c.jp.unarchive.Click()
	e.until("conversations.unarchive", 10*time.Second, func() bool { return calls.Load() == 1 })
}
