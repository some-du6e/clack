package ui

import (
	"encoding/json"
	"fmt"
	"image"
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
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/uiguard"
)

// sidebarSig is the sidebar's rows as "kind:key:label".
func sidebarSig(a *App) []string {
	out := make([]string, 0, len(a.sidebar.rows))
	for _, r := range a.sidebar.rows {
		out = append(out, fmt.Sprintf("%d:%s:%s", r.kind, r.key, r.label))
	}
	return out
}

// unreadSig is each sidebar conversation's unread state.
func unreadSig(a *App) map[string]string {
	out := map[string]string{}
	for id, c := range a.model().Convs {
		out[id] = fmt.Sprintf("%v/%d", c.Unread, c.Mentions)
	}
	return out
}

// headerSig is ch's tab bar and facepile as drawn.
func headerSig(a *App, ch string) string {
	var tabs []string
	for _, t := range a.data.tabCache[ch] {
		tabs = append(tabs, t.key+":"+t.label+":"+t.icon)
	}
	sd := a.data.side[ch]
	if sd == nil {
		sd = &chSide{}
	}
	return fmt.Sprintf("tabs=%s members=%v num=%d", strings.Join(tabs, ","), sd.members, sd.num)
}

// setUIRoute makes the next start in home open on route (ui.json, as a
// returning user's), keeping the rest of ui.json.
func setUIRoute(t *testing.T, home, route string) {
	t.Helper()
	p := filepath.Join(home, "ui.json")
	m := map[string]any{}
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	m["route"] = route
	if ch, ok := strings.CutPrefix(route, "c/"); ok {
		m["last_channel"] = ch
	}
	m["realtime"] = false
	b, _ := json.Marshal(m)
	writeFile(t, p, b)
}

// holdCalls holds fs's answers to methods until release (also run at
// cleanup); held counts the calls waiting.
func holdCalls(t *testing.T, fs *fakeslack.Server, methods ...string) (release func(), held func() int) {
	t.Helper()
	ch := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	n := 0
	release = func() { once.Do(func() { close(ch) }) }
	fs.SetHook(func(m string, _ url.Values) (any, bool) {
		if slices.Contains(methods, m) {
			select {
			case <-ch:
			default:
				mu.Lock()
				n++
				mu.Unlock()
				<-ch
			}
		}
		return nil, false
	})
	t.Cleanup(func() { release(); fs.SetHook(nil) })
	return release, func() int { mu.Lock(); defer mu.Unlock(); return n }
}

// A warm start renders from disk, the way Slack renders from its
// persisted store on a warm boot: with users.conversations, users.prefs
// and the restored channel's header reads (conversations.info, files.info,
// bookmarks.list) all held, the boot placeholder's data gates open from
// the boot data and the header cache, the sidebar is the last session's
// (same rows, same unread state) and so is the channel's tab bar. When
// Slack then answers with the same data, nothing on screen changes.
func TestWarmBootRevealsBeforeNetwork(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	home := freezeHome(t)    // background writers may land a file just after the test
	ch := fs.ChannelIDs()[0] // read, with a canvas tab and a Files tab

	// session 1: a full boot, the channel's header read, quit
	e := startEnv(t, fs, home)
	e.waitReady()
	e.openChannel(ch)
	e.until("the channel's header", 10*time.Second, func() bool { return e.a.data.tabsDone[ch] && len(e.a.data.tabCache[ch]) > 0 })
	e.settle(400 * time.Millisecond)
	wantRows, wantUnread, wantHeader := sidebarSig(e.a), unreadSig(e.a), headerSig(e.a, ch)
	if len(wantRows) < 10 {
		t.Fatalf("session 1's sidebar has %d rows", len(wantRows))
	}
	<-e.a.data.stop()
	e.shutdown()
	for _, n := range []string{store.BootDataFile, store.HeadersFile, "meta.json"} {
		fi, err := os.Stat(filepath.Join(home, "cache", n))
		if err != nil {
			t.Fatalf("session 1 left no %s: %v", n, err)
		}
		if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode %o, want 0600", n, fi.Mode().Perm())
		}
	}
	setUIRoute(t, home, "c/"+ch)

	// session 2: Slack's boot data and the header's reads are held
	release, held := holdCalls(t, fs, "users.conversations", "users.prefs.get", "conversations.info", "files.info", "bookmarks.list")
	t0 := time.Now()
	e2 := startEnv(t, fs, home)
	a := e2.a
	gate := func() string { return a.revealBlocker(time.Now()) }
	e2.until("the boot placeholder's data gates", 10*time.Second, func() bool {
		b := gate()
		return b != "boot data" && b != "channel header" && b != "fonts"
	})
	opened := time.Since(t0)
	t.Logf("warm start: the placeholder's data gates opened %v after start with the network held (blocker now %q)", opened.Round(time.Millisecond), gate())
	if !a.data.booted.IsZero() {
		t.Fatal("core finished booting while users.conversations was held")
	}
	if a.data.warmAt.IsZero() || !a.data.core.Warm() {
		t.Fatal("session 2 didn't render from the boot data")
	}
	if held() == 0 {
		t.Fatal("no call was held (the hook never ran)")
	}
	e2.settle(200 * time.Millisecond)
	if got := sidebarSig(a); !slices.Equal(got, wantRows) {
		t.Errorf("the warm sidebar isn't session 1's: %s", rowsDiff(got, wantRows))
	}
	for id, w := range wantUnread {
		if g := unreadSig(a)[id]; g != w {
			t.Errorf("%s: warm unread %s, want session 1's %s", id, g, w)
		}
	}
	if got := headerSig(a, ch); got != wantHeader {
		t.Errorf("the restored channel's header from the cache:\n got %s\nwant %s", got, wantHeader)
	}

	// Slack answers with the same data: the same frame
	rows, unread, header := sidebarSig(a), unreadSig(a), headerSig(a, ch)
	cached, _ := a.data.core.CachedHeader(ch)
	release()
	e2.waitReady()
	e2.until("the header revalidated", 10*time.Second, func() bool {
		h, _ := a.data.core.CachedHeader(ch)
		return h.At > cached.At
	})
	e2.settle(600 * time.Millisecond)
	if got := sidebarSig(a); !slices.Equal(got, rows) {
		t.Errorf("the sidebar changed when Slack answered with the same data: %s", rowsDiff(got, rows))
	}
	for id, w := range unread {
		if g := unreadSig(a)[id]; g != w {
			t.Errorf("%s: unread %s after Slack answered, was %s", id, g, w)
		}
	}
	if got := headerSig(a, ch); got != header {
		t.Errorf("the header changed when its reads answered the same:\n got %s\nwant %s", got, header)
	}
}

// rowsDiff says how got differs from want: rows only in one of them, else
// the first position where the order differs.
func rowsDiff(got, want []string) string {
	in := func(s []string) map[string]bool {
		m := map[string]bool{}
		for _, x := range s {
			m[x] = true
		}
		return m
	}
	g, w := in(got), in(want)
	var extra, missing []string
	for _, x := range got {
		if !w[x] {
			extra = append(extra, x)
		}
	}
	for _, x := range want {
		if !g[x] {
			missing = append(missing, x)
		}
	}
	if len(extra)+len(missing) > 0 {
		return fmt.Sprintf("%d rows, want %d; extra %v; missing %v", len(got), len(want), extra, missing)
	}
	for i := range got {
		if got[i] != want[i] {
			return fmt.Sprintf("the same rows, reordered from %d: got %q, want %q", i, got[i], want[i])
		}
	}
	return "equal"
}

// The UI goroutine never waits for core's Boot: with users.conversations
// held (Boot can't finish), a start restored to each page lays out its
// frames, and on a channel the composer's autocomplete runs. A core call
// that would wait is recorded by uiguard (backend.go's boot-wait hook);
// a frame that waits anyway is caught by the watchdog below, which then
// lets Boot finish so the test can end. α.89 painted nothing for 2.3 s
// on a launch restored to Activity: its first frame waited in Snapshot.
func TestUINeverWaitsForBoot(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	ch := fs.ChannelIDs()[1]
	for _, route := range []string{"activity", "unreads", "threads", "later", "c/" + ch} {
		for _, warm := range []bool{false, true} {
			t.Run(strings.ReplaceAll(route, "/", "_")+map[bool]string{false: "-cold", true: "-warm"}[warm], func(t *testing.T) {
				home := freezeHome(t)
				if warm { // a session before this one wrote the caches
					e := startEnv(t, fs, home)
					e.waitReady()
					e.settle(200 * time.Millisecond)
					<-e.a.data.stop()
					e.shutdown()
				}
				setUIRoute(t, home, route)
				release, _ := holdCalls(t, fs, "users.conversations")
				uiguard.Reset()
				uiguard.MarkUI()
				defer uiguard.Unmark()
				e := startEnv(t, fs, home)
				frame := func(what string) {
					t.Helper()
					stuck := time.AfterFunc(5*time.Second, func() {
						buf := make([]byte, 1<<20)
						n := runtime.Stack(buf, true)
						t.Errorf("%s: a frame waited 5 s with Boot held:\n%s", what, buf[:n])
						release() // let it finish
					})
					e.frame()
					stuck.Stop()
				}
				for i := 0; i < 5; i++ {
					frame("first frames")
					time.Sleep(10 * time.Millisecond)
				}
				if warm {
					for dl := time.Now().Add(5 * time.Second); e.a.data.warmAt.IsZero() && time.Now().Before(dl); {
						frame("the warm sidebar")
						time.Sleep(5 * time.Millisecond)
					}
					if e.a.data.warmAt.IsZero() {
						t.Error("the warm start's boot data never rendered")
					}
				}
				for i := 0; i < 20; i++ {
					frame("frames before boot")
					time.Sleep(5 * time.Millisecond)
				}
				if route == "c/"+ch {
					e.click(composerPoint(e))
					frame("composer focused")
					e.typeText("@a")
					for i := 0; i < 5; i++ {
						frame("composer autocomplete")
					}
				}
				if !e.a.data.booted.IsZero() {
					t.Fatal("core booted while users.conversations was held")
				}
				if v := uiguard.Violations(); len(v) > 0 {
					t.Errorf("waited for boot on the UI goroutine: %v", v)
				}
				release()
				e.waitReady()
			})
		}
	}
}

// composerPoint is inside the open channel's message box (bottom of the
// window, right of the sidebar).
func composerPoint(e *perfEnv) image.Point {
	return image.Pt(int(float32(e.size.X)*0.6), e.size.Y-60)
}
