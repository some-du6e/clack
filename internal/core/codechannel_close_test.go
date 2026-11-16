package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

// The code channels Clack closed stay closed across a restart
// (code_channel_closed.json next to the reopen times). Slack reads is_open
// from client.channels at boot, which Clack doesn't call, so before, every
// launch listed the closed ones again and the closer closed the same ones
// two minutes later (Karim's log: "closing 4 stale code channels" once per
// launch, 21 times since 11-10). A record_channel_open reopens it for the
// next launch too.
func TestClosedCodeChannelsSurviveRestart(t *testing.T) {
	sz := fakeslack.Small
	sz.CodeChannels = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	dir := t.TempDir()
	ctx := context.Background()
	launch := func() *App {
		t.Helper()
		a := New(slack.New(fs.Creds()), store.New(dir))
		t.Cleanup(a.Shutdown)
		if err := a.Boot(ctx); err != nil {
			t.Fatal(err)
		}
		return a
	}
	closes := func() int {
		n := 0
		for _, c := range fs.CodeCalls() {
			if c.Method == "conversations.close" {
				n++
			}
		}
		return n
	}
	a := launch()
	a.closeStaleCodeChannels(ctx, codeCloseFirstMax)
	if closes() != 1 || inSidebar(a, fakeslack.CodeQuietID) {
		t.Fatalf("the first launch's closer: %d conversations.close, in the sidebar %v", closes(), inSidebar(a, fakeslack.CodeQuietID))
	}
	a.Shutdown()

	b := launch()
	if inSidebar(b, fakeslack.CodeQuietID) {
		t.Error("after a restart the closed code channel is in the sidebar again")
	}
	if got := b.staleCodeChannels(10, time.Now()); len(got) != 0 {
		t.Errorf("after a restart the closer would close %v again", got)
	}
	b.closeStaleCodeChannels(ctx, codeCloseFirstMax)
	if n := closes(); n != 1 {
		t.Errorf("after a restart conversations.close went out again (%d calls)", n)
	}
	// reopened (record_channel_open): open for the next launch as well
	b.handle(rtm("record_channel_open", map[string]any{"channel": fakeslack.CodeQuietID}))
	if !inSidebar(b, fakeslack.CodeQuietID) {
		t.Fatal("record_channel_open didn't reopen it")
	}
	b.Shutdown()

	c := launch()
	if !inSidebar(c, fakeslack.CodeQuietID) {
		t.Error("a reopened code channel is closed again after a restart")
	}
	// closed again as the closer closes one (its reopen time dropped first)
	c.clearCodeChannelReopenedAt(fakeslack.CodeQuietID)
	if err := c.CloseCodeChannel(ctx, fakeslack.CodeQuietID); err != nil || closes() != 2 || inSidebar(c, fakeslack.CodeQuietID) {
		t.Fatalf("closing it on the third launch: %v, %d conversations.close, in the sidebar %v", err, closes(), inSidebar(c, fakeslack.CodeQuietID))
	}
	c.Shutdown()

	// control: without the saved set (Clack before: only memory) the next
	// launch lists it again and its closer would close it again, so this
	// test reproduces the bug
	if err := os.Remove(filepath.Join(dir, codeClosedFile)); err != nil {
		t.Fatal(err)
	}
	d := launch()
	if !inSidebar(d, fakeslack.CodeQuietID) || len(d.staleCodeChannels(10, time.Now())) == 0 {
		t.Error("control: without code_channel_closed.json the closed code channel stayed out of the sidebar; the test no longer reproduces the bug")
	}
}

// Sign-out deletes the saved closed set with the rest of the account's
// cache (store.DropDisk's cacheFiles), so signing back in to the same
// account doesn't hide a code channel that may have been reopened
// meanwhile.
func TestSignOutForgetsClosedCodeChannels(t *testing.T) {
	sz := fakeslack.Small
	sz.CodeChannels = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	dir := t.TempDir()
	ctx := context.Background()
	a := New(slack.New(fs.Creds()), store.New(dir))
	t.Cleanup(a.Shutdown)
	if err := a.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	a.closeStaleCodeChannels(ctx, codeCloseFirstMax)
	closed := filepath.Join(dir, codeClosedFile)
	if _, err := os.Stat(closed); err != nil || inSidebar(a, fakeslack.CodeQuietID) {
		t.Fatalf("closing it: saved %v, in the sidebar %v", err, inSidebar(a, fakeslack.CodeQuietID))
	}
	a.Shutdown()
	<-a.DropCache() // sign-out
	for _, n := range []string{codeClosedFile, codeClosedFile + ".tmp", codeReopenFile, codeReopenFile + ".tmp"} {
		if _, err := os.Stat(filepath.Join(dir, n)); !os.IsNotExist(err) {
			t.Errorf("sign-out left %s (%v)", n, err)
		}
	}
	if es, err := os.ReadDir(dir); err == nil {
		var left []string
		for _, e := range es {
			left = append(left, e.Name())
		}
		t.Logf("the account directory after sign-out: %v", left)
	}

	b := New(slack.New(fs.Creds()), store.New(dir)) // signed in again
	t.Cleanup(b.Shutdown)
	if err := b.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	if !inSidebar(b, fakeslack.CodeQuietID) || b.codeChannelClosed(fakeslack.CodeQuietID) {
		t.Error("after signing out and in again the code channel closed before is still hidden")
	}
}

// The closer's close is saved only once conversations.close answered ok:
// quitting while it runs (the call never sent) leaves nothing on disk, so
// the next launch doesn't hide a channel the server still has open.
func TestCodeChannelCloseSavedOnlyOnceConfirmed(t *testing.T) {
	sz := fakeslack.Small
	sz.CodeChannels = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	dir := t.TempDir()
	a := New(slack.New(fs.Creds()), store.New(dir))
	t.Cleanup(a.Shutdown)
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	quit, cancel := context.WithCancel(context.Background())
	cancel() // the app is quitting: core's background context is done
	if err := a.CloseCodeChannel(quit, fakeslack.CodeQuietID); err == nil {
		t.Fatal("conversations.close went out on a cancelled context")
	}
	for _, c := range fs.CodeCalls() {
		if c.Method == "conversations.close" {
			t.Fatalf("the fake got conversations.close %v", c.Args)
		}
	}
	if !a.codeChannelClosed(fakeslack.CodeQuietID) {
		t.Error("the optimistic close isn't in memory")
	}
	if m := a.readCodeState(codeClosedFile); len(m) != 0 {
		t.Errorf("a close the server never got was saved: %v", m)
	}
	a.Shutdown()

	b := New(slack.New(fs.Creds()), store.New(dir))
	t.Cleanup(b.Shutdown)
	if err := b.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !inSidebar(b, fakeslack.CodeQuietID) || len(b.staleCodeChannels(10, time.Now())) == 0 {
		t.Error("after quitting mid-close the next launch hides the channel (or won't close it)")
	}
	// confirmed this time: saved
	if err := b.CloseCodeChannel(context.Background(), fakeslack.CodeQuietID); err != nil {
		t.Fatal(err)
	}
	if m := b.readCodeState(codeClosedFile); m[fakeslack.CodeQuietID] == 0 {
		t.Errorf("a close conversations.close answered isn't saved: %v", m)
	}
}

// codeClosedApp is a core over the fake API with a cache directory, the
// viewer's code channels ids and plain channel plain, each channel's
// latest message at latest's time.
func codeClosedApp(t *testing.T, dir string, ids []string, plain string, latest map[string]time.Time) (*App, *fakeSlack) {
	t.Helper()
	f := newFake(t)
	c := slack.New(slack.Creds{Token: "xoxc-test", UserID: "USELF", Team: "T1", TeamURL: f.srv.URL + "/"})
	c.EdgeURL = f.srv.URL + "/edge/"
	s := store.New(dir)
	s.PutUsers([]slack.User{{ID: "USELF", Name: "me"}})
	a := New(c, s)
	t.Cleanup(a.Shutdown)
	a.S.Self = *mustUser(s, "USELF")
	created := time.Now().Add(-60 * 24 * time.Hour).Unix()
	var counts []slack.CountEntry
	for _, id := range ids {
		a.S.PutConv(slack.Conversation{ID: id, Name: strings.ToLower(id), IsChannel: true, IsMember: true, Created: created,
			Properties: json.RawMessage(liveTestProps)})
	}
	a.S.PutConv(slack.Conversation{ID: plain, Name: strings.ToLower(plain), IsChannel: true, IsMember: true, Created: created})
	for id, at := range latest {
		ts := strconv.FormatInt(at.Unix(), 10) + ".000100"
		counts = append(counts, slack.CountEntry{ID: id, Latest: ts, LastRead: ts})
	}
	a.S.SetCounts(&slack.Counts{Channels: counts})
	return a, f
}

// countsReply is client.counts answering every channel's latest message
// at latest's time, read.
func countsReply(latest map[string]time.Time) string {
	var chans []map[string]any
	for id, at := range latest {
		ts := strconv.FormatInt(at.Unix(), 10) + ".000100"
		chans = append(chans, map[string]any{"id": id, "latest": ts, "last_read": ts})
	}
	b, _ := json.Marshal(map[string]any{"ok": true, "channels": chans, "ims": []any{}, "mpims": []any{}})
	return string(b)
}

// A saved close doesn't last for good. A join is a new membership, and
// leaving drops the entry. Boot drops entries for channels that aren't the
// viewer's code channels any more, and those with a message newer than the
// newest Clack heard on the socket (the record_channel_open it may have
// missed). A message heard live, with no reopen, keeps the channel closed,
// as Slack's client does: quitting saves it as the entry's watermark.
func TestClosedCodeChannelEntriesExpire(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	day := 24 * time.Hour
	quiet, recent := now.Add(-10*day), now.Add(-day)
	w := now.Add(-5 * day).Unix() // when they were closed
	saved := map[string]int64{"CJOIN": w, "CLEFT": w, "CSEEN": w, "CMISSED": w, "CGONE": w, "CPLAIN": w, "CQUIET": w}
	b, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, codeClosedFile), b, 0o600); err != nil {
		t.Fatal(err)
	}
	codes := []string{"CJOIN", "CLEFT", "CSEEN", "CMISSED", "CGONE", "CQUIET"}
	all := func(at map[string]time.Time) map[string]time.Time {
		out := map[string]time.Time{}
		for _, id := range append(codes, "CPLAIN") {
			out[id] = quiet
		}
		for id, t := range at {
			out[id] = t
		}
		return out
	}

	// the first launch: boot's check drops the plain channel's entry only
	a, _ := codeClosedApp(t, dir, codes, "CPLAIN", all(nil))
	a.loadCodeChannelState()
	a.reconcileClosedCodeChannels(a.S.Seq())
	for _, id := range codes {
		if !a.codeChannelClosed(id) {
			t.Errorf("first launch: %s isn't closed", id)
		}
	}
	if a.codeChannelClosed("CPLAIN") {
		t.Error("first launch: an entry for a channel that isn't a code channel stayed")
	}
	// while it runs: rejoined, left, and a message in CSEEN heard on the
	// socket with no reopen
	a.handle(rtm("channel_joined", map[string]any{"channel": map[string]any{"id": "CJOIN", "name": "cjoin"}}))
	a.handle(rtm("channel_left", map[string]any{"channel": "CLEFT"}))
	ts := strconv.FormatInt(recent.Unix(), 10) + ".000100"
	var counts []slack.CountEntry
	for id, at := range all(map[string]time.Time{"CSEEN": recent}) {
		l := strconv.FormatInt(at.Unix(), 10) + ".000100"
		counts = append(counts, slack.CountEntry{ID: id, Latest: l, LastRead: l})
	}
	a.S.SetCounts(&slack.Counts{Channels: counts})
	a.noteCodeChannelMessage("CSEEN", ts, true)
	if a.codeChannelClosed("CJOIN") || a.codeChannelClosed("CLEFT") {
		t.Errorf("joined %v, left %v: still closed", a.codeChannelClosed("CJOIN"), a.codeChannelClosed("CLEFT"))
	}
	if !a.codeChannelClosed("CSEEN") {
		t.Error("a message with no record_channel_open reopened CSEEN")
	}
	_ = a.Close() // quit: the watermarks
	a.Shutdown()

	// the next launch: CMISSED had a message while Clack wasn't running,
	// CGONE isn't the viewer's any more
	next := all(map[string]time.Time{"CSEEN": recent, "CMISSED": recent})
	delete(next, "CGONE")
	b2, _ := codeClosedApp(t, dir, []string{"CJOIN", "CLEFT", "CSEEN", "CMISSED", "CQUIET"}, "CPLAIN", next)
	b2.loadCodeChannelState()
	// Boot's calls go out (since); CQUIET is closed again here meanwhile
	// (journaled), so Boot's view of it, older, doesn't touch it even where
	// it would drop it (a message newer than its watermark)
	since := b2.S.Seq()
	st := b2.codeOpen()
	st.mu.Lock()
	st.wrote["CQUIET"] = b2.S.NextSeq()
	st.closed["CQUIET"] = quiet.Unix() - 1
	st.mu.Unlock()
	b2.reconcileClosedCodeChannels(since)
	if !b2.codeChannelClosed("CQUIET") {
		t.Error("a close journaled after Boot's calls went out was undone by Boot's check")
	}
	b2.reconcileClosedCodeChannels(b2.S.Seq()) // a later boot: its own view counts
	want := map[string]bool{"CJOIN": false, "CLEFT": false, "CSEEN": true, "CMISSED": false, "CGONE": false, "CPLAIN": false, "CQUIET": false}
	for id, closed := range want {
		if got := b2.codeChannelClosed(id); got != closed {
			t.Errorf("next launch: %s closed %v, want %v", id, got, closed)
		}
	}
	if m := b2.readCodeState(codeClosedFile); m["CSEEN"] != recent.Unix() {
		t.Errorf("CSEEN's watermark %d, want the message it saw (%d); ts %s", m["CSEEN"], recent.Unix(), ts)
	}
}

// A closed code channel's watermark moves only with messages heard on the
// socket. Activity seen in the counts after a socket gap (a reconnect after
// sleep) or a poll (realtime off) is activity Clack wasn't hearing, where
// a record_channel_open may have gone by: the gap's counts refresh checks
// the closed set again and shows the channel, and quitting never saves that
// activity as seen. Quitting saves nothing before Boot's check ran (a boot
// that failed, a quit mid-boot).
func TestClosedCodeChannelWatermarkHeardLive(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	day := 24 * time.Hour
	quiet, recent := now.Add(-10*day), now.Add(-day)
	w := now.Add(-5 * day).Unix()
	b, err := json.Marshal(map[string]int64{"CHEARD": w, "CGAP": w})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, codeClosedFile), b, 0o600); err != nil {
		t.Fatal(err)
	}
	codes := []string{"CHEARD", "CGAP"}
	at := func(m map[string]time.Time) map[string]time.Time {
		out := map[string]time.Time{"CHEARD": quiet, "CGAP": quiet, "CPLAIN": quiet}
		for id, t := range m {
			out[id] = t
		}
		return out
	}

	// a boot that never got to its check (users.conversations failed):
	// its counts already show CGAP's message, and quitting saves nothing
	early, _ := codeClosedApp(t, dir, codes, "CPLAIN", at(map[string]time.Time{"CGAP": recent}))
	early.loadCodeChannelState()
	early.noteCodeChannelMessage("CHEARD", strconv.FormatInt(recent.Unix(), 10)+".000100", true)
	_ = early.Close()
	early.Shutdown()
	if m := early.readCodeState(codeClosedFile); m["CHEARD"] != w || m["CGAP"] != w {
		t.Fatalf("a quit before Boot's check saved watermarks: %v", m)
	}

	a, f := codeClosedApp(t, dir, codes, "CPLAIN", at(nil))
	a.loadCodeChannelState()
	a.reconcileClosedCodeChannels(a.S.Seq()) // Boot's check
	evs, stop := a.Subscribe()
	defer stop()
	// CHEARD's message comes over the socket; CGAP's while the socket was
	// down (the laptop asleep): only the counts after the reconnect show it
	a.noteCodeChannelMessage("CHEARD", strconv.FormatInt(recent.Unix(), 10)+".000100", true)
	a.noteCodeChannelMessage("CGAP", strconv.FormatInt(recent.Unix(), 10)+".000100", false) // not heard: no watermark
	f.reply["client.counts"] = countsReply(at(map[string]time.Time{"CHEARD": recent, "CGAP": recent}))
	a.refreshCountsAfterGap(context.Background())
	if !a.codeChannelClosed("CHEARD") {
		t.Error("a message heard on the socket, with no reopen, reopened CHEARD")
	}
	if a.codeChannelClosed("CGAP") {
		t.Error("activity the socket missed (a reopen may have gone by) left CGAP hidden")
	}
	next(t, evs, "sidebar_changed")
	_ = a.Close() // quit: CHEARD's watermark is what it heard
	a.Shutdown()

	b2, _ := codeClosedApp(t, dir, codes, "CPLAIN", at(map[string]time.Time{"CHEARD": recent, "CGAP": recent}))
	b2.loadCodeChannelState()
	b2.reconcileClosedCodeChannels(b2.S.Seq())
	if !b2.codeChannelClosed("CHEARD") || b2.codeChannelClosed("CGAP") {
		t.Errorf("next launch: CHEARD closed %v (want true), CGAP closed %v (want false)", b2.codeChannelClosed("CHEARD"), b2.codeChannelClosed("CGAP"))
	}
	if m := b2.readCodeState(codeClosedFile); m["CHEARD"] != recent.Unix() {
		t.Errorf("CHEARD's watermark %d, want the message heard (%d)", m["CHEARD"], recent.Unix())
	}
}
