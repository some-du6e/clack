// Command probe is a developer check of internal/core against a real
// account: it boots the service, exercises every read, and (with -writes)
// every write in ONE test channel, printing only ids, counts and timings.
// It never prints or stores message contents.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
)

// The only channel this probe ever writes to (the user's test channel).
const writeChannel = "C0B4DEFKFCJ"

func main() {
	home, _ := os.UserConfigDir()
	credsPath := flag.String("creds", filepath.Join(home, "yoloslack", "creds.json"), "credentials")
	writes := flag.Bool("writes", false, "also exercise writes in "+writeChannel)
	other := flag.String("other", "C0266FRGT", "a second (read-only) channel for cold/warm history timings")
	flag.Parse()
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	b, err := os.ReadFile(*credsPath)
	if err != nil {
		log.Fatal(err)
	}
	var creds slack.Creds
	if err := json.Unmarshal(b, &creds); err != nil {
		log.Fatal(err)
	}
	a := core.New(slack.New(creds), store.New(filepath.Join(home, "yoloslack", "cache")))
	evs, stop := a.Subscribe()
	defer stop()
	ew := newWatcher(evs)

	ctx := context.Background()
	t0 := time.Now()
	if err := a.Boot(ctx); err != nil {
		log.Fatal("boot: ", err)
	}
	snap := a.Snapshot()
	step("boot", t0, "convs=%d sections=%d counts=%d users=%d emoji=%d self=%s", len(snap.Convs), len(snap.Sections), len(snap.Counts), len(snap.Users), snap.EmojiCount, snap.Self.ID)
	for _, s := range snap.Sections {
		fmt.Printf("    section %-16s %-18q %d\n", s.Type, s.Name, len(s.Channels))
	}
	if ev, ok := ew.wait("connection", "", 10*time.Second); ok {
		step("realtime", t0, "connected=%v", ev.Connected)
	} else {
		fail("realtime: no connection event")
	}

	ch := writeChannel
	for _, c := range []string{ch, *other} {
		t := time.Now()
		p, err := a.History(ctx, c, "")
		check("history cold "+c, t, err, "msgs=%d has_more=%v conv=%v", len(p.Messages), p.HasMore, p.Conv != nil)
		t = time.Now()
		p, err = a.History(ctx, c, "")
		check("history warm "+c, t, err, "msgs=%d", len(p.Messages))
		if len(p.Messages) > 0 && c == ch {
			t = time.Now()
			older, err := a.History(ctx, c, p.Messages[0].TS)
			check("history older", t, err, "msgs=%d", len(older.Messages))
		}
	}
	page, _ := a.History(ctx, ch, "")
	var thread, thumb string
	docBlocks := 0
	for _, m := range page.Messages {
		docBlocks += len(m.Doc.Blocks)
		if m.ReplyCount > 0 && thread == "" {
			thread = m.TS
		}
		for _, bl := range m.Doc.Blocks {
			if bl.File != nil && bl.File.Thumb != nil && bl.File.Thumb.URL != "" && thumb == "" {
				thumb = bl.File.Thumb.URL
			}
		}
	}
	fmt.Printf("    doc blocks in %s page: %d\n", ch, docBlocks)
	if tl := a.S.Timeline(ch); thumb == "" && tl != nil { // files.slack.com thumb straight from the store
		for _, m := range tl.Msgs {
			for _, f := range m.Files {
				if f.Thumb360 != "" && thumb == "" {
					thumb = f.Thumb360
				}
			}
		}
	}
	if thread != "" {
		t := time.Now()
		r, err := a.Replies(ctx, ch, thread)
		check("replies", t, err, "msgs=%d", len(r))
	}

	timed("search messages", func() (string, error) {
		r, err := a.SearchMessages(ctx, core.SearchOpts{Query: "hackathon", Count: 5})
		return fmt.Sprintf("hits=%d total=%d convs=%d", len(r.Messages), r.Total, len(r.Convs)), err
	})
	timed("search users", func() (string, error) {
		r, err := a.SearchUsers(ctx, "zach", 10)
		return fmt.Sprintf("users=%d", len(r)), err
	})
	timed("search channels", func() (string, error) {
		r, err := a.SearchChannels(ctx, "lounge", 10)
		return fmt.Sprintf("channels=%d", len(r)), err
	})
	timed("browse channels", func() (string, error) {
		r, err := a.BrowseChannels(ctx, "", 1, "members")
		return fmt.Sprintf("channels=%d total=%d", len(r.Channels), r.Total), err
	})
	timed("usergroups", func() (string, error) {
		r, err := a.Usergroups(ctx, "lead", 5)
		return fmt.Sprintf("groups=%d", len(r)), err
	})
	timed("members", func() (string, error) {
		r, next, err := a.Members(ctx, ch, "", true)
		return fmt.Sprintf("people=%d more=%v", len(r), next != ""), err
	})
	timed("pins", func() (string, error) {
		r, err := a.Pins(ctx, ch)
		return fmt.Sprintf("pins=%d", len(r)), err
	})
	timed("bookmarks", func() (string, error) {
		r, err := a.Bookmarks(ctx, ch)
		return fmt.Sprintf("bookmarks=%d", len(r)), err
	})
	timed("channel info", func() (string, error) {
		r, err := a.ChannelInfo(ctx, ch)
		return fmt.Sprintf("members=%d member=%v", r.Conv.NumMembers, r.IsMember), err
	})
	timed("activity", func() (string, error) {
		r, err := a.Activity(ctx, core.ActivityOpts{View: "all"})
		hyd := 0
		for _, it := range r.Items {
			if it.Msg != nil {
				hyd++
			}
		}
		return fmt.Sprintf("items=%d hydrated=%d", len(r.Items), hyd), err
	})
	timed("threads", func() (string, error) {
		r, err := a.Threads(ctx, "", 10)
		return fmt.Sprintf("threads=%d unread=%v", len(r.Threads), r.State.Unread), err
	})
	timed("saved", func() (string, error) {
		r, err := a.Saved(ctx, "", "")
		return fmt.Sprintf("items=%d", len(r.Items)), err
	})
	timed("drafts", func() (string, error) {
		r, err := a.Drafts(ctx)
		return fmt.Sprintf("drafts=%d", len(r)), err
	})
	timed("dms", func() (string, error) {
		r, err := a.DMs(ctx, "")
		return fmt.Sprintf("dms=%d", len(r.DMs)), err
	})
	timed("commands", func() (string, error) {
		r, err := a.Commands(ctx, "")
		return fmt.Sprintf("commands=%d", len(r)), err
	})
	timed("presence", func() (string, error) {
		r := a.Presence(ctx, []string{snap.Self.ID, "U078JTE15CL"})
		return fmt.Sprintf("known=%d", len(r)), nil
	})
	timed("dnd", func() (string, error) {
		r, err := a.DND(ctx)
		return fmt.Sprintf("bytes=%d", len(r)), err
	})
	timed("prefs", func() (string, error) {
		p, err := a.RefreshPrefs(ctx)
		return fmt.Sprintf("emoji_style=%s muted=%d", p.EmojiStyle, len(p.Muted)), err
	})
	if thumb == "" {
		for _, m := range page.Messages {
			if m.Avatar != "" {
				thumb = m.Avatar
				break
			}
		}
	}
	if thumb == "" {
		thumb = snap.Self.Avatar
	}
	for i := 0; i < 2; i++ {
		timed(fmt.Sprintf("fetch #%d", i+1), func() (string, error) {
			b, mime, err := a.Fetch(ctx, thumb)
			return fmt.Sprintf("bytes=%d mime=%s", len(b), mime), err
		})
	}

	if *writes {
		runWrites(ctx, a, ew)
	}
	fmt.Printf("\nevents seen: %s\n", ew.summary())
	if failures > 0 {
		fmt.Printf("FAILURES: %d\n", failures)
		os.Exit(1)
	}
	fmt.Println("ALL OK")
}

func runWrites(ctx context.Context, a *core.App, ew *watcher) {
	ch := writeChannel
	const tag = "[yoloslack backend core test, please ignore]"
	t := time.Now()
	m, err := a.Send(ctx, core.SendOpts{Channel: ch, Text: tag})
	check("send", t, err, "ts=%s", m.TS)
	if err != nil {
		return
	}
	expect(ew, "message", m.TS, t)
	expectErr(ew, "typing", a.Typing(ch, ""))
	t = time.Now()
	check("react add", t, a.React(ctx, ch, m.TS, "white_check_mark", true), "")
	expect(ew, "update", m.TS, t)
	t = time.Now()
	check("react remove", t, a.React(ctx, ch, m.TS, "white_check_mark", false), "")
	expect(ew, "update", m.TS, t)
	t = time.Now()
	check("edit", t, a.Edit(ctx, ch, m.TS, tag+" (edited)", nil), "")
	expect(ew, "update", m.TS, t)
	t = time.Now()
	check("pin", t, a.Pin(ctx, ch, m.TS), "")
	expect(ew, "update", m.TS, t)
	t = time.Now()
	check("unpin", t, a.Unpin(ctx, ch, m.TS), "")
	expect(ew, "update", m.TS, t)
	t = time.Now()
	check("save", t, a.Save(ctx, ch, m.TS, 0), "")
	sp, err := a.Saved(ctx, "", "")
	found := false
	for _, it := range sp.Items {
		found = found || (it.TS == m.TS && it.Msg != nil)
	}
	check("saved contains it", t, err, "found=%v", found)
	if !found {
		fail("saved item missing")
	}
	t = time.Now()
	check("unsave", t, a.Unsave(ctx, ch, m.TS), "")
	t = time.Now()
	up, err := a.Upload(ctx, core.UploadOpts{Channel: ch, ThreadTS: m.TS, Files: []core.UploadFile{{Name: "yoloslack-core-test.txt", Data: []byte("synthetic upload test\n")}}})
	check("upload", t, err, "files=%d", len(up))
	var fileTS string
	if ev, ok := ew.wait("message", "", 10*time.Second, func(e core.Event) bool { return e.Reply && e.ThreadTS == m.TS }); ok {
		fileTS = ev.TS
		step("  upload message event", t, "ts=%s", fileTS)
		_, upd := ew.wait("update", fileTS, 10*time.Second)
		hasURL := false
		if fm, ok := a.S.Find(ch, fileTS); ok {
			for _, f := range fm.Files {
				hasURL = hasURL || f.URLPrivate != ""
			}
		}
		step("  upload file filled", t, "update_event=%v url_private=%v", upd, hasURL)
	} else {
		fail("upload: no reply message event")
	}
	t = time.Now()
	r, err := a.Replies(ctx, ch, m.TS)
	check("replies of test thread", t, err, "msgs=%d", len(r))
	t = time.Now()
	cr, err := a.Command(ctx, core.CommandOpts{Channel: ch, Command: "/shrug", Text: tag})
	shrugTS := ""
	if cr.Msg != nil {
		shrugTS = cr.Msg.TS
	}
	check("command /shrug", t, err, "ts=%s", shrugTS)
	_, err = a.Command(ctx, core.CommandOpts{Channel: ch, Command: "/yoloslacknonexistent"})
	step("command unknown", t, "err=%v", err)
	t = time.Now()
	cv, err := a.MarkUnread(ctx, ch, m.TS)
	check("mark unread", t, err, "unread=%v last_read=%s", cv.Unread, cv.LastRead)
	expect(ew, "marked", "", t)
	latest := m.TS
	if shrugTS != "" {
		latest = shrugTS
	}
	t = time.Now()
	cv, err = a.Mark(ctx, ch, latest)
	check("mark read", t, err, "unread=%v", cv.Unread)
	t = time.Now()
	dm, err := a.OpenDM(ctx, a.Self().ID)
	check("open self DM", t, err, "id=%s kind=%s", dm.ID, dm.Kind)
	for _, ts := range []string{shrugTS, fileTS, m.TS} {
		if ts == "" {
			continue
		}
		t = time.Now()
		check("delete "+ts, t, a.Delete(ctx, ch, ts), "")
		expect(ew, "delete", ts, t)
	}
}

// ---- output helpers ----

var failures int

func step(name string, t time.Time, f string, args ...any) {
	fmt.Printf("%-24s %8s  %s\n", name, time.Since(t).Round(time.Millisecond), fmt.Sprintf(f, args...))
}

func check(name string, t time.Time, err error, f string, args ...any) {
	if err != nil {
		failures++
		fmt.Printf("%-24s %8s  ERROR %v\n", name, time.Since(t).Round(time.Millisecond), err)
		return
	}
	step(name, t, f, args...)
}

func timed(name string, f func() (string, error)) {
	t := time.Now()
	s, err := f()
	check(name, t, err, "%s", s)
}

func fail(msg string) {
	failures++
	fmt.Println("FAIL", msg)
}

func expect(ew *watcher, typ, ts string, t time.Time) {
	if _, ok := ew.wait(typ, ts, 10*time.Second); ok {
		step("  event "+typ, t, "ts=%s", ts)
	} else {
		fail("no " + typ + " event for " + ts)
	}
}

func expectErr(ew *watcher, name string, err error) {
	if err != nil {
		fail(name + ": " + err.Error())
	} else {
		fmt.Printf("%-24s %8s  sent\n", name, "")
	}
}

// watcher records events (types and ids only) and lets the probe wait.
type watcher struct {
	mu     sync.Mutex
	evs    []core.Event
	counts map[string]int
	cond   *sync.Cond
}

func newWatcher(ch <-chan core.Event) *watcher {
	w := &watcher{counts: map[string]int{}}
	w.cond = sync.NewCond(&w.mu)
	go func() {
		for ev := range ch {
			w.mu.Lock()
			w.evs = append(w.evs, ev)
			w.counts[ev.Type]++
			w.cond.Broadcast()
			w.mu.Unlock()
		}
	}()
	go func() { // wake waiters periodically for timeouts
		for range time.Tick(100 * time.Millisecond) {
			w.cond.Broadcast()
		}
	}()
	return w
}

// wait returns the first not-yet-consumed event of typ (and ts, if set).
func (w *watcher) wait(typ, ts string, d time.Duration, pred ...func(core.Event) bool) (core.Event, bool) {
	deadline := time.Now().Add(d)
	w.mu.Lock()
	defer w.mu.Unlock()
	for {
		for i, ev := range w.evs {
			if ev.Type != typ || (ts != "" && ev.TS != ts) {
				continue
			}
			okp := true
			for _, p := range pred {
				okp = okp && p(ev)
			}
			if okp {
				w.evs = append(w.evs[:i], w.evs[i+1:]...)
				return ev, true
			}
		}
		if time.Now().After(deadline) {
			return core.Event{}, false
		}
		w.cond.Wait()
	}
}

func (w *watcher) summary() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var parts []string
	for k, v := range w.counts {
		parts = append(parts, fmt.Sprintf("%s=%d", k, v))
	}
	return strings.Join(parts, " ")
}
