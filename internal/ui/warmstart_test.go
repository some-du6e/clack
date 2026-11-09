package ui

import (
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// jadeTheme is an account theme that isn't Slack's default, so a first
// frame that painted the default would differ.
const jadeTheme = `{"primary":{"palette":"jade"},"highlight1":{"palette":"jade"},"highlight2":{"palette":"aubergine"},"important":{"palette":"jade"},"brightness":6,"sidebarInverted":true,"useCustomHex":false}`

// A warm start paints from the boot snapshot before Slack answers anything:
// the account theme, the sidebar's rows with their unread state, and the
// last channel's messages. Every fakeslack call is held until the first
// frames are checked, so none of it can have come from the network. Then
// the answers arrive and nothing on screen changes (Slack's data didn't).
func TestWarmFirstFrameBeforeNetwork(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	fs.SetPrefs(map[string]any{"ia_theme": jadeTheme})
	home := t.TempDir()
	ch := fs.ChannelIDs()[1]

	// session 1: a full boot, open a channel, quit (saves the snapshot)
	e := startEnv(t, fs, home)
	e.waitReady()
	e.openChannel(ch)
	e.until("history", 10*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
	e.settle(300 * time.Millisecond)
	want := struct {
		chrome   any
		acct     acctTheme
		rows     []string
		unread   map[string]bool
		mentions map[string]int
	}{chrome: e.a.th.C.ChromeA, acct: e.a.acct, unread: map[string]bool{}, mentions: map[string]int{}}
	if !want.acct.set {
		t.Fatal("session 1 never applied the account theme")
	}
	if wt, _ := theme.ParseIATheme(jadeTheme); want.acct.t != wt {
		t.Fatalf("session 1 theme %+v, want the account's jade", want.acct.t)
	}
	for _, r := range e.a.sidebar.rows {
		want.rows = append(want.rows, r.key)
	}
	for id, c := range e.a.data.m.Convs {
		want.unread[id], want.mentions[id] = c.Unread, c.Mentions
	}
	<-e.a.data.stop()
	e.shutdown()
	for _, n := range []string{"boot.json", "history.json", "meta.json"} {
		if _, err := os.Stat(filepath.Join(home, "cache", n)); err != nil {
			t.Fatalf("session 1 left no %s: %v", n, err)
		}
	}
	if runtime.GOOS != "windows" {
		_ = filepath.Walk(filepath.Join(home, "cache"), func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			m := info.Mode().Perm()
			if (info.IsDir() && m != 0o700) || (!info.IsDir() && m != 0o600) {
				t.Errorf("%s: mode %o, want 0700 dirs and 0600 files", p, m)
			}
			return nil
		})
	}

	// session 2: every call waits until release
	var (
		calls   atomic.Int64
		release = make(chan struct{})
		once    sync.Once
	)
	open := func() { once.Do(func() { close(release) }) }
	defer open()
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		calls.Add(1)
		<-release
		return nil, false
	})
	defer fs.SetHook(nil)
	e2 := startEnv(t, fs, home)
	e2.frame()
	if !e2.a.data.warm {
		t.Fatal("session 2 didn't start from the boot snapshot")
	}
	if e2.a.acct != want.acct {
		t.Errorf("first frame's account theme %+v, want %+v", e2.a.acct, want.acct)
	}
	if got := any(e2.a.th.C.ChromeA); got != want.chrome {
		t.Errorf("first frame's chrome %v, want the account theme's %v (a default-theme flash)", got, want.chrome)
	}
	var rows []string
	for _, r := range e2.a.sidebar.rows {
		rows = append(rows, r.key)
	}
	if len(rows) == 0 || len(rows) != len(want.rows) {
		t.Fatalf("first frame's sidebar has %d rows, want session 1's %d", len(rows), len(want.rows))
	}
	for i := range rows {
		if rows[i] != want.rows[i] {
			t.Fatalf("first frame's sidebar row %d is %q, want %q", i, rows[i], want.rows[i])
		}
	}
	for id, c := range e2.a.data.m.Convs {
		if c.Unread != want.unread[id] || c.Mentions != want.mentions[id] {
			t.Errorf("first frame: %s unread %v/%d, want the cached %v/%d", id, c.Unread, c.Mentions, want.unread[id], want.mentions[id])
		}
	}
	// the last channel's messages, still with nothing answered
	e2.openChannel(ch)
	shown := func() int {
		if cv := e2.a.convos[ch+"/"]; cv != nil {
			if l, ok := cv.list.(interface{ LayoutSig() (int, bool, uint64) }); ok {
				n, loaded, _ := l.LayoutSig()
				if loaded {
					return n
				}
			}
		}
		return 0
	}
	e2.until("cached messages", 10*time.Second, func() bool { return shown() > 0 })
	if !e2.a.data.booted.IsZero() {
		t.Fatal("core finished booting while every call was held")
	}
	if calls.Load() == 0 {
		t.Fatal("the client sent nothing (the hook never ran)")
	}
	before := e2.a.th.C.ChromeA
	// now Slack answers: the same data, so the same frame
	open()
	e2.waitReady()
	e2.settle(500 * time.Millisecond)
	if e2.a.th.C.ChromeA != before {
		t.Errorf("chrome changed after Slack answered: %v → %v", before, e2.a.th.C.ChromeA)
	}
	var after []string
	for _, r := range e2.a.sidebar.rows {
		after = append(after, r.key)
	}
	if len(after) != len(rows) {
		t.Errorf("sidebar went from %d to %d rows after Slack answered", len(rows), len(after))
	}
	if shown() == 0 {
		t.Error("the messages went away after Slack answered")
	}
}
