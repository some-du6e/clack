package core

import (
	"bytes"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/logtag"
	"github.com/some-du6e/clack/internal/slack"
)

// The socket's and the event pump's watchdogs (slack rtm.go,
// pump_watch.go) over fakeslack, after Karim's α.136 logged nothing at
// all for 45 minutes while its socket answered pings (2026-11-22): a
// socket that keeps answering pings but brings nothing else is closed and
// connected again, a handler that blocks is named with its goroutine's
// stack, and a healthy idle session logs none of it.

// stallLogs captures the log as the watchdogs write it.
type stallLogs struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *stallLogs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *stallLogs) has(s string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Contains(l.b.String(), s)
}

func captureStallLogs(t *testing.T) *stallLogs {
	l := &stallLogs{}
	prev, flags := log.Writer(), log.Flags()
	log.SetOutput(l)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(prev); log.SetFlags(flags) })
	return l
}

// waitStall polls ok (which mustn't take the store's lock) for d.
func waitStall(t *testing.T, d time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Slack's gateway stops sending a socket anything but its pongs (no
// event, no reconnect_url) and doesn't close it. The liveness check is
// happy, so the watchdog is what closes it, with every goroutine's stack
// in the log, once nothing of Slack's own came for its timeout, and the
// client connects again and gets the events after it. Clack kept the
// socket: every event was lost.
func TestSocketThatSendsNothingConnectsAgain(t *testing.T) {
	t.Cleanup(slack.SetPingTimers(50*time.Millisecond, 5*time.Second))
	stallTimers(t, 800*time.Millisecond, 5*time.Second, 50*time.Millisecond)
	logs := captureStallLogs(t)
	a, fs, el := rtApp(t)
	fs.SendReconnectURLs(100 * time.Millisecond) // a healthy gateway's frames
	t.Cleanup(func() { fs.SendReconnectURLs(0) })
	time.Sleep(300 * time.Millisecond)
	dials := fs.CallCount("ws")
	fs.MuteSockets()
	waitStall(t, 15*time.Second, "the client connecting again", func() bool { return fs.CallCount("ws") > dials && a.RTM.Connected() })
	if !logs.has("rtm: nothing from Slack for") {
		t.Error(`no "nothing from Slack" line for the quiet socket`)
	}
	if !logs.has("all goroutines:") {
		t.Error("no goroutine dump for the quiet socket")
	}
	ch := fs.ChannelIDs()[2]
	ts, ev := fs.PostQuiet(ch, fs.UserIDs()[3], "a synthetic line after the reconnect")
	fs.Push(ev)
	waitStall(t, 10*time.Second, "the message after the reconnect", func() bool {
		for _, e := range el.all() {
			if e.Type == "message" && e.TS == ts {
				return true
			}
		}
		return false
	})
}

// A handler blocks on a lock another goroutine holds (here the store's,
// held by the test): the watchdog names the event the pump is on, with
// its conversation's tag, and logs the pump's goroutine first, then every
// goroutine's, once; when the handler goes on, it says how long it took.
// Clack logged nothing.
func TestStalledHandlerIsNamed(t *testing.T) {
	pumpTimers(t, 300*time.Millisecond, 50*time.Millisecond)
	logs := captureStallLogs(t)
	a, fs, _ := rtApp(t)
	ch := fs.ChannelIDs()[2]
	a.S.Lock()
	locked := true
	defer func() {
		if locked {
			a.S.Unlock()
		}
	}()
	fs.Push(map[string]any{"type": "user_typing", "channel": ch, "user": fs.UserIDs()[3]})
	what := "a user_typing event of " + logtag.ID(ch)
	waitStall(t, 10*time.Second, "the stall's line", func() bool { return logs.has("core: the event pump has been on " + what + " for") })
	// the dump is its own log call, after the stall's line
	waitStall(t, 10*time.Second, "the stalled pump's stack", func() bool { return logs.has("core.(*App).pump(") && logs.has("all goroutines:") })
	a.S.Unlock()
	locked = false
	waitStall(t, 10*time.Second, "the line once it went on", func() bool { return logs.has("core: the event pump took ") && logs.has(" on "+what) })
}

// A healthy idle session: the gateway sends its reconnect_url now and
// then and answers the pings, and no event comes. Neither watchdog fires,
// and the socket stays.
func TestIdleSessionLogsNoStall(t *testing.T) {
	t.Cleanup(slack.SetPingTimers(50*time.Millisecond, 5*time.Second))
	stallTimers(t, time.Second, time.Second, 50*time.Millisecond)
	pumpTimers(t, time.Second, 50*time.Millisecond)
	logs := captureStallLogs(t)
	_, fs, _ := rtApp(t)
	fs.SendReconnectURLs(100 * time.Millisecond)
	t.Cleanup(func() { fs.SendReconnectURLs(0) })
	dials := fs.CallCount("ws")
	time.Sleep(3 * time.Second)
	for _, bad := range []string{"nothing from Slack", "pong check hasn't run", "has been on", "event buffer full", "all goroutines:"} {
		if logs.has(bad) {
			t.Errorf("an idle session logged %q", bad)
		}
	}
	if n := fs.CallCount("ws"); n != dials {
		t.Errorf("an idle session's socket connected %d more times", n-dials)
	}
}

// A handler is on an event when the machine sleeps an hour (the clock
// jumps, the wall and the monotonic one both, as on Windows, which counts
// a sleep in either): on wake the watchdog ages it from the wake, so the
// time asleep isn't a stall and no goroutine dump comes, and its "took"
// leaves the sleep out. Clack logged it as on its event for an hour, with
// the dump, the moment the machine woke.
func TestPumpSleepIsNotAStall(t *testing.T) {
	pumpTimers(t, 2*time.Second, 50*time.Millisecond)
	forgetWake(t) // the fake clock's wake isn't the next test's
	logs := captureStallLogs(t)
	start := time.Now()
	var jump atomic.Int64
	t.Cleanup(slack.SetClock(func() slack.ClockReading {
		now, d := time.Now(), time.Duration(jump.Load())
		return slack.ClockReading{Wall: now.Add(d).Round(0), Mono: now.Sub(start) + d}
	}))
	a, fs, _ := rtApp(t)
	ch := fs.ChannelIDs()[2]
	a.S.Lock()
	locked := true
	defer func() {
		if locked {
			a.S.Unlock()
		}
	}()
	fs.Push(map[string]any{"type": "user_typing", "channel": ch, "user": fs.UserIDs()[3]})
	time.Sleep(300 * time.Millisecond) // the pump is on it
	jump.Store(int64(time.Hour))       // the machine slept
	time.Sleep(800 * time.Millisecond) // the watchdog looks a dozen times after the wake
	for _, bad := range []string{"has been on", "all goroutines:"} {
		if logs.has(bad) {
			t.Errorf("a sleep made a stall: %q in the log", bad)
		}
	}
	a.S.Unlock()
	locked = false
	time.Sleep(200 * time.Millisecond)
	if logs.has("took 1h") {
		t.Error("the handler's \"took\" counted the hour asleep")
	}
}
