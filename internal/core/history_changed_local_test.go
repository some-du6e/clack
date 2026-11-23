package core

// What only this client has survives a history change (the
// history-changed review's blocker): a send that failed is the only copy
// of its text (restoreSend refills only an empty composer), and a purge
// or a truncation let it go with the cached history, its row and its
// Retry gone the moment the network came back. Synthetic data only.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// failedSend sends text into ch with chat.postMessage failing, and
// returns the failed copy's ts.
func (e *hcEnv) failedSend(text string) string {
	e.t.Helper()
	e.fs.SetFault("chat.postMessage", fakeslack.Fault{Error: "internal_error"})
	if _, err := e.a.Send(context.Background(), SendOpts{Channel: e.ch, Text: text}); err == nil {
		e.t.Fatal("control: the send didn't fail")
	}
	e.fs.SetFault("chat.postMessage", fakeslack.Fault{})
	ts := e.failedIn(text)
	if ts == "" {
		e.t.Fatal("control: no failed copy in the history")
	}
	return ts
}

// failedIn is the ts of the failed copy with text in ch's newest page
// ("" for none).
func (e *hcEnv) failedIn(text string) string {
	e.t.Helper()
	p, err := e.a.History(context.Background(), e.ch, "")
	if err != nil {
		e.t.Fatal(err)
	}
	for i := range p.Messages {
		if m := &p.Messages[i]; m.Text == text && m.Failed {
			return m.TS
		}
	}
	return ""
}

// client.counts' newer history_invalid purges the conversation: the
// failed send stays, failed (its Retry), and the newest page read again
// comes in around it.
func TestCountsPurgeKeepsFailedSend(t *testing.T) {
	e := newHCEnv(t)
	ctx := context.Background()
	const text = "synthetic: mine, it failed before the purge"
	e.failedSend(text)
	old := fmt.Sprintf("%d.000100", time.Now().Add(-time.Hour).Unix())
	e.fs.SetMutationTimestamps(e.ch, old, old)
	e.a.refreshCounts(ctx)
	e.fs.SetMutationTimestamps(e.ch, fmt.Sprintf("%d.000100", time.Now().Unix()), old)
	e.a.refreshCounts(ctx)
	if e.failedIn(text) == "" {
		t.Fatal("after the counts' purge the failed send is gone")
	}
	newest := e.tss[len(e.tss)-1]
	e.until("the newest page around the failed send", func() bool {
		for _, ts := range e.page() {
			if ts == newest {
				return e.failedIn(text) != ""
			}
		}
		return false
	})
}

// A channel_history_changed past the failed send: it stays, failed.
func TestHistoryChangedKeepsFailedSend(t *testing.T) {
	e := newHCEnv(t)
	const text = "synthetic: mine, it failed before the change"
	e.failedSend(text)
	latest := fmt.Sprintf("%d.000100", time.Now().Add(time.Minute).Unix())
	e.fs.Push(fakeslack.HistoryChangedEvent("channel_history_changed", e.ch, latest, false))
	e.fence()
	if e.failedIn(text) == "" {
		t.Fatal("after channel_history_changed the failed send is gone")
	}
}
