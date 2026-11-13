package core

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

func inSection(a *App, typ, id string) bool {
	for _, s := range a.Snapshot().Sections {
		if s.Type == typ && slices.Contains(s.Channels, id) {
			return true
		}
	}
	return false
}

func inSidebar(a *App, id string) bool {
	for _, s := range a.Snapshot().Sections {
		if slices.Contains(s.Channels, id) {
			return true
		}
	}
	return false
}

// checkForOldCodeChannelsToClose's rules, one case each: read, no draft,
// not selected, not starred, only in Code channels sections, quiet for
// over a week since the newer of the latest message and the last reopen;
// the most idle first, at most the limit; nothing without the experiment.
func TestStaleCodeChannelRules(t *testing.T) {
	a, f := testApp(t)
	now := time.Now()
	ago := func(d time.Duration) string { return strconv.FormatInt(now.Add(-d).Unix(), 10) + ".000100" }
	day := 24 * time.Hour
	var counts []slack.CountEntry
	add := func(id string, quiet time.Duration, unread bool) {
		a.S.PutConv(slack.Conversation{ID: id, Name: strings.ToLower(id), IsChannel: true, IsMember: true,
			Created: now.Add(-30 * day).Unix(), Properties: json.RawMessage(liveTestProps)})
		counts = append(counts, slack.CountEntry{ID: id, Latest: ago(quiet), LastRead: ago(quiet), HasUnreads: unread})
	}
	add("CQUIET", 8*day, false)    // stale
	add("COLDER", 10*day, false)   // stale, more idle
	add("CLISTED", 9*day, false)   // stale, listed in a Code channels section
	add("CRECENT", 2*day, false)   // quiet for 2 days: not stale
	add("CUNREAD", 9*day, true)    // "Is unread"
	add("CDRAFT", 9*day, false)    // "Has draft"
	add("CSTAR", 9*day, false)     // "Starred" (and filed in Starred)
	add("CSEL", 9*day, false)      // "Is primary view"
	add("CCUSTOM", 9*day, false)   // in a custom section: "Not in Code Channels section"
	add("CARCHIVED", 9*day, false) // archived
	arch, _ := a.S.Conv("CARCHIVED")
	arch.IsArchived = true
	a.S.PutConv(*arch)
	a.S.SetCounts(&slack.Counts{Channels: counts})
	sec := func(id, typ string, chans ...string) slack.Section {
		s := slack.Section{ID: id, Type: typ}
		s.ChannelIDsPage.ChannelIDs = chans
		return s
	}
	a.S.Sections = []slack.Section{sec("S1", "standard", "CCUSTOM"), sec("S2", "coding_channels", "CLISTED"), sec("L3", "channels")}
	a.mu.Lock()
	a.starred["CSTAR"] = true
	a.mu.Unlock()
	a.putDraft(Draft{ID: "Dr1", Channel: "CDRAFT", Text: "half a thought"})
	a.SetSelected("CSEL")
	close(a.ready)
	a.setExperiments(map[string]string{"code_channels_auto_close": "on", "session_channels": "on"})

	if got := a.staleCodeChannels(10, now); !slices.Equal(got, []string{"COLDER", "CLISTED", "CQUIET"}) {
		t.Errorf("stale %v, want the three quiet read ones, most idle first", got)
	}
	if got := a.staleCodeChannels(2, now); !slices.Equal(got, []string{"COLDER", "CLISTED"}) {
		t.Errorf("limit 2: %v", got)
	}
	// a reopen a day ago counts as activity
	a.setCodeChannelReopenedAt("COLDER", now.Add(-day).Unix())
	if got := a.staleCodeChannels(10, now); !slices.Equal(got, []string{"CLISTED", "CQUIET"}) {
		t.Errorf("after reopening COLDER: %v", got)
	}
	a.clearCodeChannelReopenedAt("COLDER")
	// without session_channels the persisted Code channels section still
	// files them; without any Code channels section nothing qualifies
	a.SetExperimentGroup("session_channels", "off")
	if got := a.staleCodeChannels(10, now); len(got) != 3 {
		t.Errorf("session_channels off, a persisted section: %v", got)
	}
	a.S.Sections = []slack.Section{sec("S1", "standard", "CCUSTOM"), sec("L3", "channels")}
	if got := a.staleCodeChannels(10, now); len(got) != 0 {
		t.Errorf("no Code channels section: %v", got)
	}
	a.ClearExperimentGroup("session_channels")
	a.S.Sections = []slack.Section{sec("S1", "standard", "CCUSTOM"), sec("S2", "coding_channels", "CLISTED"), sec("L3", "channels")}
	for _, g := range []string{"off", ""} {
		a.SetExperimentGroup("code_channels_auto_close", g)
		if got := a.staleCodeChannels(10, now); len(got) != 0 {
			t.Errorf("code_channels_auto_close %q: %v", g, got)
		}
	}
	a.ClearExperimentGroup("code_channels_auto_close")

	// closing: out of the sidebar at once, conversations.close {channel};
	// a failed call puts it back
	f.reply["conversations.close"] = `{"ok":true}`
	if !inSidebar(a, "CQUIET") {
		t.Fatal("CQUIET isn't in the sidebar to begin with")
	}
	if err := a.CloseCodeChannel(context.Background(), "CQUIET"); err != nil {
		t.Fatal(err)
	}
	if v := f.last("conversations.close"); v.Get("channel") != "CQUIET" {
		t.Errorf("close args %v", v)
	}
	if inSidebar(a, "CQUIET") || !a.codeChannelClosed("CQUIET") {
		t.Error("a closed code channel stays in the sidebar")
	}
	if !inSidebar(a, "CLISTED") || !inSidebar(a, "CSTAR") {
		t.Error("closing one moved the others")
	}
	a.SetSelected("CQUIET") // the selected conversation always shows
	if !inSidebar(a, "CQUIET") {
		t.Error("the selected closed code channel isn't shown")
	}
	a.SetSelected("CSEL")
	// under the fake's lock: SetSelected's maybeReopenViewedCodeChannel
	// may still be calling it (ci 43418168216 crashed on the map)
	f.mu.Lock()
	f.reply["conversations.close"] = `{"ok":false,"error":"channel_not_found"}`
	f.mu.Unlock()
	if err := a.CloseCodeChannel(context.Background(), "COLDER"); err == nil {
		t.Error("the failed close reported no error")
	}
	if !inSidebar(a, "COLDER") || a.codeChannelClosed("COLDER") {
		t.Error("a failed close left the channel closed")
	}
}

// Over fakeslack: the closer closes the read code channel quiet for nine
// days (conversations.close), viewing it reopens it (recordChannels.open
// with "viewed-record-channel") and records the reopen time, and
// record_channel_close / record_channel_open move it in and out of the
// sidebar.
func TestCodeChannelsAutoCloseFakeslack(t *testing.T) {
	sz := fakeslack.Small
	sz.CodeChannels = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	dir := t.TempDir()
	a := New(slack.New(fs.Creds()), store.New(dir))
	defer a.Shutdown()
	ctx := context.Background()
	if err := a.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	if a.Experiment("code_channels_auto_close") != "on" {
		t.Fatalf("fakeslack has code_channels_auto_close %q", a.Experiment("code_channels_auto_close"))
	}
	if !inSection(a, "coding_channels", fakeslack.CodeQuietID) {
		t.Fatal("the quiet code channel isn't in Code channels")
	}
	if got := a.staleCodeChannels(10, time.Now()); !slices.Equal(got, []string{fakeslack.CodeQuietID}) {
		t.Fatalf("stale %v, want only the one quiet for nine days", got)
	}
	calls := func(method string) []fakeslack.CodeCall {
		var out []fakeslack.CodeCall
		for _, c := range fs.CodeCalls() {
			if c.Method == method {
				out = append(out, c)
			}
		}
		return out
	}
	a.closeStaleCodeChannels(ctx, codeCloseFirstMax)
	if cs := calls("conversations.close"); len(cs) != 1 || cs[0].Args.Get("channel") != fakeslack.CodeQuietID {
		t.Fatalf("conversations.close calls %+v", cs)
	}
	if inSidebar(a, fakeslack.CodeQuietID) {
		t.Error("the closed code channel is still in the sidebar")
	}

	// viewing it reopens it
	a.SetSelected(fakeslack.CodeQuietID)
	deadline := time.Now().Add(5 * time.Second)
	for len(calls("recordChannels.open")) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if cs := calls("recordChannels.open"); len(cs) != 1 || cs[0].Args.Get("channel") != fakeslack.CodeQuietID || cs[0].Args.Get("reason") != "viewed-record-channel" {
		t.Fatalf("recordChannels.open calls %+v", cs)
	}
	a.SetSelected("")
	for a.codeChannelClosed(fakeslack.CodeQuietID) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !inSection(a, "coding_channels", fakeslack.CodeQuietID) {
		t.Error("the reopened code channel isn't back in Code channels")
	}
	if at := a.codeChannelReopenedAt(fakeslack.CodeQuietID); time.Since(time.Unix(at, 0)) > time.Minute {
		t.Errorf("reopen time %d", at)
	}
	if got := a.staleCodeChannels(10, time.Now()); len(got) != 0 {
		t.Errorf("a code channel reopened just now is stale again: %v", got)
	}
	// the reopen time survives a restart, as Slack persists codeChannelReopen
	b := New(slack.New(fs.Creds()), store.New(dir))
	defer b.Shutdown()
	if at := b.codeChannelReopenedAt(fakeslack.CodeQuietID); at == 0 {
		t.Error("the reopen time wasn't persisted")
	}

	// record_channel_close closes it and drops the reopen time;
	// record_channel_open reopens it
	a.handle(rtm("record_channel_close", map[string]any{"channel": fakeslack.CodeQuietID}))
	if inSidebar(a, fakeslack.CodeQuietID) || a.codeChannelReopenedAt(fakeslack.CodeQuietID) != 0 {
		t.Error("record_channel_close left it open or kept its reopen time")
	}
	a.handle(rtm("record_channel_open", map[string]any{"channel": fakeslack.CodeQuietID}))
	if cs := calls("recordChannels.open"); len(cs) != 2 || cs[1].Args.Get("reason") != "record-channel-opened-rtm" {
		t.Errorf("record_channel_open: recordChannels.open calls %+v", cs)
	}
	if !inSidebar(a, fakeslack.CodeQuietID) {
		t.Error("record_channel_open didn't reopen it")
	}
}
