package core

import (
	"context"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

func troubleIs(on bool) func([]Event) bool {
	return func(evs []Event) bool {
		for i := len(evs) - 1; i >= 0; i-- {
			if evs[i].Type == "connection_trouble" {
				return evTrouble(evs[i]) == on
			}
		}
		return false
	}
}

// The socket's trouble connecting reaches the UI: a gateway that refuses
// every upgrade after the connection dropped turns ConnectionTrouble on
// and emits connection_trouble; once it lets the socket back, the hello
// turns it off.
func TestConnectionTroubleLikeSlack(t *testing.T) {
	a, fs, l := rtApp(t)
	t.Cleanup(setTroubleThresholds(0, 1)) // the first failure after a connection
	if connectionTrouble(a) {
		t.Fatal("control: trouble while connected")
	}
	fs.SetFault("ws", fakeslack.Fault{Status: 503})
	fs.DropSockets()
	l.wait(t, "connection_trouble on", troubleIs(true))
	if !connectionTrouble(a) {
		t.Error("ConnectionTrouble is false after connection_trouble on")
	}
	fs.SetFault("ws", fakeslack.Fault{})
	connectNow(a) // the backoff's wait cut short (the banner's click)
	l.wait(t, "connection_trouble off at the hello", troubleIs(false))
}

// DegradedLoad is the banner's loadNewMessages: the open conversation's
// newest history (not while the socket is up), client.counts at most once
// a minute, the history at most once a second.
func TestDegradedLoadLikeSlack(t *testing.T) {
	a, fs, l := rtApp(t)
	ch := fs.ChannelIDs()[1]
	ctx := context.Background()
	counts := fs.CallCount("client.counts")
	degradedLoad(a, ctx, ch, "", "")
	read := degradedHistRead(a, ch)
	if read { // the boot's prefetch may read it meanwhile: the banner's own read is the throttle's
		t.Error("with the socket up the banner read the history again: hasSyncedConnection")
	}
	if fs.CallCount("client.counts") <= counts { // the boot's own refreshes may come too
		t.Fatal("the counts weren't asked for")
	}
	t.Cleanup(setTroubleThresholds(0, 1))
	fs.SetFault("ws", fakeslack.Fault{Status: 503})
	fs.DropSockets()
	l.wait(t, "connection_trouble on", troubleIs(true))
	hist := fs.CallCount("conversations.history")
	at := degradedCountsAt(a)
	degradedLoad(a, ctx, ch, "", "")
	if n := fs.CallCount("conversations.history"); n != hist+1 {
		t.Errorf("the open conversation's history was read %d times, want once", n-hist)
	}
	again := !degradedCountsAt(a).Equal(at)
	if again {
		t.Error("the banner's client.counts went again within the minute")
	}
	degradedLoad(a, ctx, ch, "", "")
	if n := fs.CallCount("conversations.history"); n != hist+1 {
		t.Errorf("the history went again within the second (%d reads)", n-hist)
	}
	time.Sleep(degradedHistoryWait() + 50*time.Millisecond)
	degradedLoad(a, ctx, ch, "", "")
	if n := fs.CallCount("conversations.history"); n != hist+2 {
		t.Errorf("a second later the history went %d times in all, want twice", n-hist)
	}
}
