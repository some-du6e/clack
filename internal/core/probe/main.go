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
	timed("tabs", func() (string, error) {
		r, err := a.Tabs(ctx, ch)
		var kinds []string
		for _, t := range r {
			kinds = append(kinds, t.Type+map[bool]string{true: "(deleted)", false: ""}[t.Deleted])
		}
		return fmt.Sprintf("tabs=%d %s", len(r), strings.Join(kinds, ",")), err
	})
	timed("member counts", func() (string, error) {
		r, err := a.MemberCounts(ctx, ch)
		return fmt.Sprintf("people=%d everyone=%d bots=%d", r.People, r.Everyone, r.Bots), err
	})
	timed("channel info (2)", func() (string, error) {
		r, err := a.ChannelInfo(ctx, ch)
		return fmt.Sprintf("num_members=%d managers=%d", r.Conv.NumMembers, len(r.Managers)), err
	})
	timed("files", func() (string, error) {
		r, err := a.Files(ctx, core.FilesOpts{})
		return fmt.Sprintf("files=%d total=%d", len(r.Files), r.Total), err
	})
	timed("files canvases", func() (string, error) {
		r, err := a.Files(ctx, core.FilesOpts{Kind: "canvases"})
		return fmt.Sprintf("files=%d", len(r.Files)), err
	})
	timed("deleted files", func() (string, error) {
		r, err := a.DeletedFiles(ctx)
		return fmt.Sprintf("files=%d", len(r.Files)), err
	})
	timed("huddle history", func() (string, error) {
		r, _, err := a.HuddleHistory(ctx, "")
		return fmt.Sprintf("huddles=%d", len(r)), err
	})
	timed("sent", func() (string, error) {
		r, err := a.Sent(ctx, "", 1)
		return fmt.Sprintf("msgs=%d total=%d", len(r.Messages), r.Total), err
	})
	timed("browse joined/private", func() (string, error) {
		r, err := a.BrowseChannelsWith(ctx, core.ChannelBrowseOpts{Membership: "joined", Sort: "name"})
		r2, err2 := a.BrowseChannelsWith(ctx, core.ChannelBrowseOpts{Type: "archived", Sort: "member_count", Desc: true})
		if err == nil {
			err = err2
		}
		return fmt.Sprintf("joined=%d archived=%d/%d", len(r.Channels), len(r2.Channels), r2.Total), err
	})
	timed("browse people", func() (string, error) {
		r, err := a.BrowsePeople(ctx, core.PeopleBrowseOpts{Sort: "name"})
		return fmt.Sprintf("users=%d total=%d", len(r.Users), r.Total), err
	})
	timed("usergroups search", func() (string, error) {
		r, err := a.SearchUsergroups(ctx, core.UsergroupOpts{Sort: "member_count", Desc: true, Count: 5})
		return fmt.Sprintf("groups=%d", len(r)), err
	})
	timed("scheduled", func() (string, error) {
		r, err := a.ScheduledMessages(ctx, "")
		return fmt.Sprintf("scheduled=%d", len(r)), err
	})
	timed("notify prefs", func() (string, error) {
		n := a.NotifyPrefs(ch)
		return fmt.Sprintf("desktop=%q muted=%v starred=%v", n.Desktop, n.Muted, a.Starred(ch)), nil
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
	if pe, ok := ew.wait("message", "", 5*time.Second, func(e core.Event) bool { return e.Msg != nil && e.Msg.Pending && e.ClientMsgID == m.ClientMsgID }); ok {
		step("  event message(pending)", t, "local ts=%s", pe.TS)
		if se, ok := ew.wait("sent", m.TS, 10*time.Second); ok && se.ReplacesTS == pe.TS {
			step("  event sent", t, "replaces=%s", se.ReplacesTS)
		} else {
			fail("no sent event replacing the pending row")
		}
	} else {
		fail("no pending message event")
	}
	if _, dup := ew.wait("message", m.TS, 1500*time.Millisecond); dup {
		fail("duplicate message event for own echo")
	}
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
	// to-dos
	t = time.Now()
	check("add to-do", t, a.AddTodo(ctx, ch, m.TS, 0), "")
	expect(ew, "saved", m.TS, t)
	t = time.Now()
	check("complete to-do", t, a.CompleteTodo(ctx, ch, m.TS), "")
	expect(ew, "saved", m.TS, t)
	t = time.Now()
	check("unsave to-do", t, a.Unsave(ctx, ch, m.TS), "")
	expect(ew, "saved", m.TS, t)
	// thread follow
	t = time.Now()
	check("unfollow thread", t, a.SubscribeThread(ctx, ch, m.TS, false), "")
	check("follow thread", t, a.SubscribeThread(ctx, ch, m.TS, true), "")
	if _, ok := ew.wait("thread_subscription", "", 8*time.Second); ok {
		step("  event thread_subscription", t, "")
	} else {
		step("  event thread_subscription", t, "none (not sent for own threads?)")
	}
	// scheduling (far future), then cancel
	t = time.Now()
	sm, err := a.ScheduleMessage(ctx, core.SendOpts{Channel: ch, ThreadTS: m.TS, Text: tag + " (scheduled)"}, time.Now().Add(72*time.Hour).Unix())
	check("schedule", t, err, "id=%s post_at=%d", sm.ID, sm.PostAt)
	if err == nil {
		expect(ew, "draft", "", t)
		sl, err := a.ScheduledMessages(ctx, ch)
		found := false
		for _, s := range sl {
			found = found || s.ID == sm.ID
		}
		check("scheduled listed", t, err, "found=%v", found)
		if !found {
			fail("scheduled message not listed")
		}
		t = time.Now()
		check("delete scheduled", t, a.DeleteScheduled(ctx, ch, sm.ID), "")
	}
	// draft in our own thread, then delete
	t = time.Now()
	dr, err := a.SaveDraft(ctx, core.DraftOpts{Channel: ch, ThreadTS: m.TS, Text: tag + " (draft)"})
	check("save draft", t, err, "id=%s", dr.ID)
	if err == nil {
		dr2, err := a.SaveDraft(ctx, core.DraftOpts{ID: dr.ID, Channel: ch, ThreadTS: m.TS, Text: tag + " (draft 2)"})
		check("update draft", t, err, "id=%s", dr2.ID)
		check("delete draft", t, a.DeleteDraft(ctx, dr.ID), "")
	}
	// star / unstar, mute / unmute (C0B4DEFKFCJ only; restored)
	wasStarred := a.Starred(ch)
	t = time.Now()
	check("star", t, a.Star(ctx, ch, true), "starred=%v", a.Starred(ch))
	if !wasStarred {
		check("unstar", t, a.Star(ctx, ch, false), "starred=%v", a.Starred(ch))
	}
	wasMuted := a.NotifyPrefs(ch).Muted
	t = time.Now()
	check("mute", t, a.Mute(ctx, ch, true), "muted=%v", a.NotifyPrefs(ch).Muted)
	check("restore mute", t, a.Mute(ctx, ch, wasMuted), "muted=%v", a.NotifyPrefs(ch).Muted)
	// activity: mark the newest item unread then read again (restores state)
	if act, err := a.Activity(ctx, core.ActivityOpts{View: "mentions", Limit: 5}); err == nil && len(act.Items) > 0 && !act.Items[0].Unread {
		it := act.Items[0]
		t = time.Now()
		check("activity mark unread", t, a.MarkActivityUnread(ctx, it), "key=%s", it.Key)
		check("activity mark read", t, a.MarkActivityRead(ctx, it), "")
		if _, ok := ew.wait("activity", "", 5*time.Second); ok {
			step("  event activity", t, "")
		}
	}
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
