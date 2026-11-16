package core

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// warmSession boots a client on dir against fs and closes it the way the
// UI does at quit (StopTraffic, then Close: the warm boot's files and
// meta.json are written).
func warmSession(t *testing.T, fs *fakeslack.Server, dir string, creds slack.Creds, during func(a *App)) Snapshot {
	t.Helper()
	a := New(slack.New(creds), store.New(dir))
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if during != nil {
		during(a)
	}
	snap := a.Snapshot()
	a.StopTraffic()
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	a.Shutdown()
	return snap
}

// holdMethods makes fs hold the given methods' answers until the returned
// release is called (once is enough; the test's cleanup calls it too).
func holdMethods(t *testing.T, fs *fakeslack.Server, methods ...string) (release func()) {
	t.Helper()
	ch := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(ch) }) }
	fs.SetHook(func(m string, _ url.Values) (any, bool) {
		if slices.Contains(methods, m) {
			<-ch
		}
		return nil, false
	})
	t.Cleanup(func() { release(); fs.SetHook(nil) })
	return release
}

func convIDs(s Snapshot) []string {
	var ids []string
	for _, c := range s.Convs {
		ids = append(ids, c.ID)
	}
	slices.Sort(ids)
	return ids
}

func sectionRows(s Snapshot) []string {
	var out []string
	for _, sec := range s.Sections {
		out = append(out, sec.ID+":"+strings.Join(sec.Channels, ","))
	}
	return out
}

// A warm boot renders from the boot data the session before saved, as
// Slack renders from its persisted store with every boot API nonblocking:
// with users.conversations and users.prefs held, DataReady closes and
// WarmSnapshot is the last session's sidebar (conversations, sections,
// unread counts) while Boot is still waiting. When Slack answers, its
// list replaces the cached one: a channel left meanwhile leaves.
func TestWarmBootFromBootData(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	dir := t.TempDir()
	first := warmSession(t, fs, dir, fs.Creds(), nil)
	if len(first.Convs) == 0 || len(first.Sections) == 0 {
		t.Fatalf("session 1 has %d convs, %d sections", len(first.Convs), len(first.Sections))
	}
	for _, n := range []string{store.BootDataFile, "meta.json"} {
		fi, err := os.Stat(filepath.Join(dir, n))
		if err != nil {
			t.Fatalf("session 1 left no %s: %v", n, err)
		}
		if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode %o, want 0600", n, fi.Mode().Perm())
		}
	}

	// Slack's state moves on: one channel is left from elsewhere
	gone := fs.ChannelIDs()[3]
	if err := slack.New(fs.Creds()).Call(context.Background(), "conversations.leave", url.Values{"channel": {gone}}, nil); err != nil {
		t.Fatal(err)
	}
	release := holdMethods(t, fs, "users.conversations", "users.prefs.get")
	a := New(slack.New(fs.Creds()), store.New(dir))
	t.Cleanup(a.Shutdown)
	booted := make(chan error, 1)
	go func() { booted <- a.Boot(context.Background()) }()
	select {
	case <-a.DataReady():
	case <-time.After(15 * time.Second):
		t.Fatal("DataReady didn't close from the boot data")
	}
	select {
	case <-a.Ready():
		t.Fatal("Boot finished while users.conversations was held")
	default:
	}
	ws, ok := a.WarmSnapshot()
	if !ok || !a.Warm() {
		t.Fatal("no warm snapshot from the boot data")
	}
	if !slices.Contains(convIDs(ws), gone) {
		t.Fatalf("the boot data lacks %s, which session 1 had", gone)
	}
	if got, want := convIDs(ws), convIDs(first); !slices.Equal(got, want) {
		t.Errorf("warm conversations differ from session 1's:\n got %v\nwant %v", got, want)
	}
	if got, want := sectionRows(ws), sectionRows(first); !slices.Equal(got, want) {
		t.Errorf("warm sections differ from session 1's:\n got %v\nwant %v", got, want)
	}
	for id, c := range first.Counts {
		if w := ws.Counts[id]; w.Unread != c.Unread || w.Mentions != c.Mentions {
			t.Errorf("%s: warm unread %v/%d, want session 1's %v/%d", id, w.Unread, w.Mentions, c.Unread, c.Mentions)
		}
	}
	// readers that never wait see it too
	if n := len(a.RecentSnapshot().Convs); n != len(ws.Convs) {
		t.Errorf("RecentSnapshot has %d convs before Boot, want the boot data's %d", n, len(ws.Convs))
	}
	// the restored channel's header read (the UI's opened) refreshes the
	// cached copy of the channel left elsewhere before Boot answers
	if _, err := a.ChannelInfo(context.Background(), gone); err != nil {
		t.Logf("conversations.info %s: %v", gone, err)
	}
	release()
	if err := <-booted; err != nil {
		t.Fatal(err)
	}
	live := a.Snapshot()
	if slices.Contains(convIDs(live), gone) {
		t.Errorf("%s was left on the server but stays after Boot answered", gone)
	}
	want := slices.DeleteFunc(convIDs(first), func(id string) bool { return id == gone })
	if got := convIDs(live); !slices.Equal(got, want) {
		t.Errorf("after Boot: conversations\n got %v\nwant %v", got, want)
	}
	if _, ok := a.WarmSnapshot(); ok || !a.Warm() {
		t.Error("after Boot the warm snapshot should be let go, and Warm still true")
	}
}

// Boot data names its account: another user (or workspace) on the same
// cache directory starts cold, a sign-out deletes the warm boot's files,
// and nothing writes them again afterwards. (Each account also has its
// own directory: cmd/clack's store.AccountDir.)
func TestBootDataIsPerAccount(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	dir := t.TempDir()
	warmSession(t, fs, dir, fs.Creds(), func(a *App) {
		a.SetCachedHeader(fs.ChannelIDs()[0], ChannelHeader{Tabs: []Tab{{ID: "files", Type: "files", Title: "Files"}}, Num: 7})
	})
	creds := fs.Creds()
	team := firstNonEmpty(creds.Team, creds.EnterpriseID)
	st := store.New(dir)
	if st.ReadBootData(team, creds.UserID) == nil {
		t.Fatal("no boot data for the account that wrote it")
	}
	if st.ReadBootData(team, "U0OTHER001") != nil || st.ReadBootData("T0OTHER001", creds.UserID) != nil {
		t.Fatal("boot data read for another account")
	}
	for _, other := range []slack.Creds{
		func() slack.Creds { c := fs.Creds(); c.UserID = "U0OTHER001"; return c }(),
		func() slack.Creds { c := fs.Creds(); c.Team = "T0OTHER001"; return c }(),
	} {
		a := New(slack.New(other), store.New(dir))
		a.S.LoadCache() // the directory's metadata cache is there: only the account check keeps it cold
		a.loadWarm(closedChan())
		if a.Warm() {
			t.Errorf("user %s team %s: warm from another account's boot data", other.UserID, other.Team)
		}
		if _, ok := a.CachedHeader(fs.ChannelIDs()[0]); ok {
			t.Errorf("user %s team %s: another account's header cache", other.UserID, other.Team)
		}
	}
	// the account itself: warm, with its header
	a := New(slack.New(fs.Creds()), store.New(dir))
	t.Cleanup(a.Shutdown)
	a.S.LoadCache()
	a.loadWarm(closedChan())
	if !a.Warm() {
		t.Fatal("the account's own boot data didn't load")
	}
	if h, ok := a.CachedHeader(fs.ChannelIDs()[0]); !ok || h.Num != 7 || len(h.Tabs) != 1 {
		t.Fatalf("header cache: %+v %v", h, ok)
	}
	// sign-out
	<-a.DropCache()
	for _, n := range []string{store.BootDataFile, store.HeadersFile} {
		if _, err := os.Stat(filepath.Join(dir, n)); !os.IsNotExist(err) {
			t.Errorf("%s still there after sign-out: %v", n, err)
		}
	}
	a.setBootOK()
	a.SetCachedHeader("C1", ChannelHeader{Num: 1})
	a.SaveWarm()
	for _, n := range []string{store.BootDataFile, store.HeadersFile} {
		if _, err := os.Stat(filepath.Join(dir, n)); !os.IsNotExist(err) {
			t.Errorf("%s written after sign-out", n)
		}
	}
}

func closedChan() chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}

// No message reaches the boot data: a conversation's latest (a message,
// as conversations.info returns it) is dropped when it's written.
func TestBootDataHasNoMessages(t *testing.T) {
	dir := t.TempDir()
	c := slack.New(slack.Creds{Token: "xoxc-test", UserID: "USELF", Team: "T1", TeamURL: "http://127.0.0.1:1/"})
	s := store.New(dir)
	s.PutConv(slack.Conversation{ID: "C1", Name: "general", IsChannel: true,
		Latest: json.RawMessage(`{"type":"message","ts":"1.0","text":"a secret message body"}`)})
	a := New(c, s)
	a.setBootOK()
	a.SaveWarm()
	b, err := os.ReadFile(filepath.Join(dir, store.BootDataFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "secret message body") || strings.Contains(string(b), `"latest":{`) {
		t.Fatalf("boot_data.json holds a message: %s", b)
	}
	if !strings.Contains(string(b), `"general"`) {
		t.Fatalf("boot_data.json lacks the conversation: %s", b)
	}
	if c, ok := s.Conv("C1"); !ok || len(c.Latest) == 0 {
		t.Error("saving dropped the store's own copy of latest")
	}
}

// Snapshot and BootErr wait for Boot, so a call before it finished is
// reported with its caller (the UI turns the report into a uiguard
// violation, and core logs a wait over 50 ms); the readers the UI
// goroutine uses never wait and report nothing.
func TestBootWaitIsReported(t *testing.T) {
	a, _ := testApp(t)
	var mu sync.Mutex
	var got []string
	SetBootWaitHook(func(method, caller string) {
		mu.Lock()
		got = append(got, method+" "+caller)
		mu.Unlock()
	})
	t.Cleanup(func() { SetBootWaitHook(nil) })
	a.SnapshotNow()
	a.RecentSnapshot()
	if _, ok := a.CachedHeader("C1"); ok {
		t.Error("a header before any read")
	}
	mu.Lock()
	if len(got) != 0 {
		t.Errorf("non-waiting readers reported a wait: %v", got)
	}
	mu.Unlock()
	done := make(chan Snapshot)
	go func() { done <- a.Snapshot() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("a Snapshot before Boot reported no wait")
		}
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case <-done:
		t.Fatal("Snapshot returned before Boot finished")
	default:
	}
	a.ServeOffline()
	if s := <-done; len(s.Convs) == 0 {
		t.Error("Snapshot after ServeOffline is empty")
	}
	select {
	case <-a.DataReady():
	default:
		t.Error("ServeOffline left DataReady open")
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.HasPrefix(got[0], "Snapshot internal/core/warmboot_test.go:") {
		t.Errorf("reported %q, want Snapshot with this file as the caller", got[0])
	}
}

// The header cache keeps the headerKeep most recently read conversations
// on disk, and a session's own read wins over what the disk had.
func TestHeaderCacheKeepsRecent(t *testing.T) {
	dir := t.TempDir()
	creds := slack.Creds{Token: "xoxc-test", UserID: "USELF", Team: "T1", TeamURL: "http://127.0.0.1:1/"}
	id := func(i int) string { return "C000" + string(rune('A'+i%26)) + string(rune('A'+i/26)) }
	total := headerKeep + 20
	a := New(slack.New(creds), store.New(dir))
	a.loadHeaders("T1", "USELF")
	now := time.Now().UnixMilli()
	for i := 0; i < total; i++ {
		a.SetCachedHeader(id(i), ChannelHeader{Num: i, At: now - int64(total-i)})
	}
	a.saveHeaders()
	b := New(slack.New(creds), store.New(dir))
	newest := id(total - 1)
	b.SetCachedHeader(newest, ChannelHeader{Num: 999}) // read before the disk loaded
	b.loadHeaders("T1", "USELF")
	b.warm.headers.mu.Lock()
	n := len(b.warm.headers.m)
	b.warm.headers.mu.Unlock()
	if n != headerKeep {
		t.Errorf("%d headers after reload, want %d", n, headerKeep)
	}
	if h, _ := b.CachedHeader(newest); h.Num != 999 {
		t.Errorf("the session's own read lost to the disk's: %+v", h)
	}
	if h, ok := b.CachedHeader(id(total - 2)); !ok || h.Num != total-2 {
		t.Errorf("a recent header didn't survive: %+v %v", h, ok)
	}
	if _, ok := b.CachedHeader(id(0)); ok {
		t.Error("the oldest header outlived the keep limit")
	}
}
