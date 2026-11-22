package ui

// Settings' writes against internal/fakeslack (fakeslack/channel_manage.go
// answers them; nothing here reaches a real workspace), and what the
// channel looks like after them, as Slack's handlers leave it:
//
//   - Change to Private: convertChannelToPrivateApi's xulU marks the
//     stored channel is_private, so the sidebar row and the header take
//     the lock while the details stay open.
//   - Archive Channel: the modal's onSubmit closes the details (closeAll);
//     the viewer stays in the channel, which channelArchived marks
//     is_archived, so the composer gives way to the archived footer
//     (MessagePaneInputDisabled).
//
// And the shots of Message history, Export and their modals for
// ch-details-shots.

import (
	"net/url"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// manageChannel is a public channel the viewer is in that isn't
// #general.
func manageChannel(t *testing.T, e *perfEnv, kind string) string {
	t.Helper()
	for _, s := range e.a.model().Sections {
		for _, id := range s.Rows {
			if c, ok := e.a.Core().Conv(id); ok && c.Kind == kind && !c.Preview && !c.IsGeneral && !c.IsArchived {
				return id
			}
		}
	}
	t.Skipf("the fake workspace has no %s channel besides #general", kind)
	return ""
}

// onlyChannelArg: the call's arguments are {channel} (the session token
// rides along in every form).
func onlyChannelArg(f url.Values) bool {
	for k := range f {
		if k != "channel" && k != "token" {
			return false
		}
	}
	return f.Has("channel")
}

func manageHooks(fs *fakeslack.Server, allowed ...string) {
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		switch method {
		case "permissions.info":
			return map[string]any{"ok": true, "allowed": allowed}, true
		case "conversations.canSetProperties":
			return map[string]any{"ok": true, "props": []any{}}, true
		}
		return nil, false
	})
}

func TestChannelDetailsManageState(t *testing.T) {
	e, fs := previewEnv(t)
	ch := manageChannel(t, e, "channel")
	manageHooks(fs, "ARCHIVE_CHANNEL", "CONVERT_TO_PRIVATE")
	t.Cleanup(func() { fs.SetHook(nil) })
	e.openChannel(ch)
	e.settle(200 * time.Millisecond)
	e.a.OpenDetailsTab(ch, "Settings")
	e.settle(800 * time.Millisecond)
	d := topDetails(t, e)
	press := func(row, dlg string) {
		t.Helper()
		e.until("the "+dlg+" modal", 10*time.Second, func() bool {
			if d.Dialog() == "" {
				d.ScrollSettingsTo("managing-this-channel")
				d.Press(row)
			}
			return d.Dialog() == dlg
		})
	}

	press("to-private", "to-private")
	d.Press("ok") // Change to Private
	e.until("conversations.convertToPrivate", 10*time.Second, func() bool { return len(fs.ManageCallsTo("conversations.convertToPrivate")) == 1 })
	if c := fs.ManageCallsTo("conversations.convertToPrivate")[0]; c.Channel != ch || !onlyChannelArg(c.Form) {
		t.Errorf("convertToPrivate form %v, want {channel: %s}", c.Form, ch)
	}
	e.until("the stored channel is private", 10*time.Second, func() bool {
		c, ok := e.a.Core().Conv(ch)
		return ok && c.Kind == "private"
	})
	e.until("the sidebar row's lock", 10*time.Second, func() bool {
		ci := e.a.model().conv(ch)
		return ci != nil && ci.Kind == "private"
	})
	if c, ok := fs.Conv(ch); !ok || !c.IsPrivate {
		t.Errorf("fakeslack's channel after the conversion: %+v", c)
	}
	e.until("the modal closes", 10*time.Second, func() bool { return d.Dialog() == "" })
	if _, open := topOverlay[detailsOverlay](e.a); !open {
		t.Fatal("Change to Private closed the details (Slack's modal closes alone)")
	}

	press("archive", "archive")
	d.Press("ok") // Archive Channel
	e.until("conversations.archive", 10*time.Second, func() bool { return len(fs.ManageCallsTo("conversations.archive")) == 1 })
	if c := fs.ManageCallsTo("conversations.archive")[0]; c.Channel != ch || !onlyChannelArg(c.Form) {
		t.Errorf("archive form %v, want {channel: %s}", c.Form, ch)
	}
	e.until("the details close", 10*time.Second, func() bool { _, open := topOverlay[detailsOverlay](e.a); return !open })
	if got := e.a.router.Current().Channel(); got != ch {
		t.Errorf("after archiving the view is %q, want the channel %s still open", got, ch)
	}
	var cv *convo
	e.until("the archived footer", 20*time.Second, func() bool {
		cv = e.a.convoFor(ch, "")
		return cv != nil && e.a.footerFor(cv) == footArchived
	})
	if c, ok := fs.Conv(ch); !ok || !c.IsArchived {
		t.Errorf("fakeslack's channel after archiving: %+v", c)
	}
	e.settle(200 * time.Millisecond)
	e.shot("ch-details-after-archive")
}

func TestChannelDetailsRetentionShot(t *testing.T) {
	e, fs := previewEnv(t)
	ch := manageChannel(t, e, "private")
	manageHooks(fs)
	t.Cleanup(func() { fs.SetHook(nil) })
	fs.SetChannelRetention(ch, fakeslack.ChannelRetention{Type: 1, Duration: 400})
	e.a.data.core.SetExperimentGroup("channel_exports", "on")
	e.openChannel(ch)
	e.settle(200 * time.Millisecond)
	e.a.OpenDetailsTab(ch, "Settings")
	e.settle(800 * time.Millisecond)
	d := topDetails(t, e)
	both := func(name string) {
		for _, dark := range []bool{false, true} {
			e.setDark(dark)
			e.settle(300 * time.Millisecond)
			e.shot(name + "-" + map[bool]string{false: "light", true: "dark"}[dark])
		}
		e.setDark(false)
		e.settle(200 * time.Millisecond)
	}
	d.ScrollSettingsTo("permissions")
	e.settle(300 * time.Millisecond)
	both("ch-details-message-history")
	d.ScrollSettingsTo("export")
	e.settle(300 * time.Millisecond)
	both("ch-details-export")

	e.until("the retention modal", 10*time.Second, func() bool {
		if d.Dialog() == "" {
			d.ScrollSettingsTo("permissions")
			d.Press("message-history")
		}
		return d.Dialog() == "retention"
	})
	e.settle(500 * time.Millisecond) // getDataRetention
	both("ch-details-retention-modal")
	d.Press("retention-default")
	e.settle(200 * time.Millisecond)
	both("ch-details-retention-default")
	d.Press("cancel")
	e.until("the modal closes", 10*time.Second, func() bool { return d.Dialog() == "" })
	if n := len(fs.ManageCallsTo("conversations.setRetention")); n != 0 {
		t.Errorf("Cancel saved (%d calls)", n)
	}

	e.until("the export modal", 10*time.Second, func() bool {
		if d.Dialog() == "" {
			d.ScrollSettingsTo("export")
			d.Press("export")
		}
		return d.Dialog() == "export"
	})
	e.settle(200 * time.Millisecond)
	both("ch-details-export-modal")
	d.Press("ok")
	e.until("conversations.export", 10*time.Second, func() bool { return len(fs.ManageCallsTo("conversations.export")) == 1 })
	if c := fs.ManageCallsTo("conversations.export")[0]; c.Channel != ch {
		t.Errorf("export call %+v", c)
	}
}
