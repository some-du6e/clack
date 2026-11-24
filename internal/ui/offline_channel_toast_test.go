package ui

// onChannelChange's showOfflineChannelToast (boot-render RpHI
// OfflineChannelToast, build 889ac449a32d): a conversation opened while
// !isClientOnline toasts "No connection." and "Last updated
// {clientLastOnline}." (toTimeAgo of lastClientConnection), or "Go online
// to get the latest messages." when the store holds none of its history;
// top-right, cloud-offline. Online, nothing. Synthetic data on the fake.

import (
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/slack"
)

func TestOfflineChannelToastLikeSlack(t *testing.T) {
	a, ca, fs := fakeApp(t, nil)
	ca.ClientOnline() // lastClientConnection: the start
	online := true
	prev := clientOnlineFor
	clientOnlineFor = func(*core.App) bool { return online }
	t.Cleanup(func() { clientOnlineFor = prev })
	ch, empty := fs.ChannelIDs()[1], fs.ChannelIDs()[2]
	ca.S.SetLatest(ch, []slack.Message{{TS: "1762500000.000100", User: "U0FAKE00001", Text: "synthetic"}}, false)

	a.offlineChannelToast(ch)
	if a.toast.text != "" {
		t.Fatalf("control: online, opening a channel toasted %q", a.toast.text)
	}
	online = false
	a.offlineChannelToast(ch)
	if !a.toast.top || a.toast.icon != "cloud-offline" {
		t.Errorf("the offline toast isn't top-right with cloud-offline (top %v, icon %q)", a.toast.top, a.toast.icon)
	}
	if p := a.toast.parts; len(p) < 3 || p[0].Text != "No connection." || p[0].Tag != "strong" {
		t.Errorf("the toast starts %+v, want the bold \"No connection.\"", p)
	}
	if got := i18n.Plain(a.toast.parts); !strings.Contains(got, "Last updated ") || !strings.HasSuffix(got, "ago.") {
		t.Errorf("a channel with history toasts %q, want \"Last updated … ago.\"", got)
	}
	a.offlineChannelToast(empty)
	if got := i18n.Plain(a.toast.parts); got != "No connection. Go online to get the latest messages." {
		t.Errorf("a channel with no history toasts %q", got)
	}
}
