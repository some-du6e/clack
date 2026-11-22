package ui

// A reply by someone else in a thread the viewer started toasts and makes
// an Activity item, as Slack's does (Karim, 2026-11-22, Clack for Windows:
// "istg i did not get this notif in windows", a reply in a thread whose
// root was his own). Slack's client toasts such a reply from the server's
// desktop_notification for the thread's follower alone:
// shouldShowNotificationForChannelMessage returns 6 for any reply that
// doesn't mention the viewer (client-boot Fpyc rr, SPMg isReply), and
// shouldShowNotificationForDesktopNotification shows a followed thread's
// reply unless that thread is open in a focused window or read to it
// (Fpyc gA, boot-render /+fI maybeShowNotification). The thread's
// thread_v2 bundle is its Activity item. Everything comes over the
// socket, as the server sends it to a follower (fakeslack
// PushFollowedReplyLive). Synthetic people and text only.

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
)

func TestReplyInOwnThreadToastsAndShowsInActivity(t *testing.T) {
	t.Setenv("CLACK_HOME", t.TempDir()) // no plugin state shared with other tests
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	c := e.a.data.core
	c.SetRealtime(true)
	e.until("the socket", 20*time.Second, func() bool { return c.RTM != nil && c.RTM.Connected() })
	rec := &timedNotes{}
	e.a.notify = rec

	// the Activity items core makes of the server's entries
	evs, stop := c.Subscribe()
	t.Cleanup(stop)
	var ch string // the case's channel (state's and the subscription's)
	var mu sync.Mutex
	var acts []core.ActivityItem
	var seen []string // core's events about ch, in order (ids only)
	go func() {
		for ev := range evs {
			mu.Lock()
			if ev.Type == "activity" && ev.Activity != nil {
				acts = append(acts, *ev.Activity)
				seen = append(seen, fmt.Sprintf("%s activity %s %s/%s", time.Now().Format("15:04:05.000"), ev.Activity.Type, ev.Activity.Channel, ev.Activity.ThreadTS))
			} else if ch != "" && ev.Channel == ch {
				var raw struct {
					Msg      string `json:"msg"`
					ThreadTS string `json:"thread_ts"`
				}
				_ = json.Unmarshal(ev.Raw, &raw)
				seen = append(seen, fmt.Sprintf("%s %s ts=%s msg=%s thread=%s", time.Now().Format("15:04:05.000"), ev.Type, ev.TS, raw.Msg, raw.ThreadTS))
			}
			mu.Unlock()
		}
	}()
	// state reports where a missing notification stopped (ids only)
	state := func(root, ts string) string {
		b := e.a.data
		m, held := c.S.Find(ch, root)
		sub, subKnown := c.S.ThreadSub(ch, root)
		b.notes.mu.Lock()
		waits, posted := len(b.notes.waits), b.notes.posted[ch]
		b.notes.mu.Unlock()
		mu.Lock()
		evs := strings.Join(seen, "; ")
		mu.Unlock()
		tt := e.a.Taut()
		return fmt.Sprintf("shown key %v; root held %v, its last_read %q; subscription %v %+v; noteOrder waits %d, posted[ch] %d; focused %v; taut %v, showNotification patched %v; core's events: %s",
			b.noteWasShown(noteShownKey(ch, ts)), held, m.LastRead, subKnown, sub, waits, posted, e.a.focused, tt != nil, tt.ThunkPatched("showNotification"), evs)
	}
	replier := fs.UserIDs()[3]
	cases := []struct {
		name          string
		activityFirst bool // the Activity entries before the reply's message events
		openThread    bool // the thread's pane open, the window not focused
	}{
		{"the reply's events first, a thread nobody opened", false, false},
		{"the Activity entries first", true, false},
		{"its thread open in a window without the focus", false, true},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// a channel of its own each (one the viewer is in, not the one a
			// launch opens): fakeslack numbers a thread's quiet replies within
			// the second, so two threads' first replies in one channel can share
			// a ts, which Slack's never do, and the second read as shown
			mu.Lock()
			ch, seen = fs.ChannelIDs()[2+i], nil
			mu.Unlock()
			if _, known := c.S.Conv(ch); !known {
				t.Fatalf("core doesn't know %s, a channel the viewer is in", ch)
			}
			// getDisplayNameForChannel: "#name" for a public channel, a private one's bare name
			name := strings.TrimPrefix(c.S.ChannelName(ch), "#")
			if k, _ := c.S.ConvKind(ch); k == "public" {
				name = "#" + name
			}
			title := "New message in " + name
			root, _ := fs.PostQuiet(ch, fakeslack.SelfID, "a synthetic question the viewer asked")
			if tc.openThread {
				e.a.OpenThread(ch, root)
				e.until("the thread's pane", 15*time.Second, func() bool {
					return e.a.secondary.kind == secThread && e.a.secondary.open() && e.threadView(ch, root) != nil
				})
			}
			e.a.focused = false // the viewer is in another app (shouldSuppressNotificationInActiveThread needs a focused window)
			text := "a synthetic answer in the viewer's thread, number " + string(rune('1'+i))
			ts := fs.PushFollowedReplyLive(ch, root, replier, text, tc.activityFirst)

			var got *desktopNote
			if !e.untilSoft(10*time.Second, func() bool {
				rec.mu.Lock()
				defer rec.mu.Unlock()
				for _, n := range rec.notes {
					if strings.Contains(n.Body, text) {
						got = n
						return true
					}
				}
				return false
			}) {
				rec.mu.Lock()
				n := len(rec.notes)
				rec.mu.Unlock()
				t.Errorf("no notification for the reply in the viewer's own thread (%d other notifications); %s", n, state(root, ts))
			} else {
				if got.Title != title {
					t.Errorf("the notification's title is %q, want %q (getNotificationTitle)", got.Title, title)
				}
				if got.Channel != ch || got.Msg != ts || got.ThreadTS != root {
					t.Errorf("the notification opens %s %s in thread %s, want %s %s in thread %s", got.Channel, got.Msg, got.ThreadTS, ch, ts, root)
				}
			}
			if !e.untilSoft(10*time.Second, func() bool {
				mu.Lock()
				defer mu.Unlock()
				for _, it := range acts {
					if it.Type == "thread_v2" && it.Channel == ch && it.ThreadTS == root {
						return true
					}
				}
				return false
			}) {
				t.Errorf("no Activity item for the thread's reply (its thread_v2 bundle); %s", state(root, ts))
			}
		})
	}
}
