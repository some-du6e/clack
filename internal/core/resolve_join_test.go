package core

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// resolveJoinApp is a core over the fake with nothing booted: every
// conversation ResolveConv asks for is unknown to the store.
func resolveJoinApp(t *testing.T, prep func(fs *fakeslack.Server)) (*App, *fakeslack.Server) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	if prep != nil {
		prep(fs)
	}
	a := New(slack.New(fs.Creds()), store.New(""))
	t.Cleanup(a.Shutdown)
	return a, fs
}

// A ResolveConv that joins another caller's conversations.info gets the
// conversation that lookup brought. The joiner reads it from the store,
// and the owner stored it only after waking the joiners (for a DM, after
// fetching its partner too): one that woke in between found nothing and
// answered "unknown". The UI's lookup is such a joiner when History's side
// read for a channel opened from a link asked first; it then gave up on
// the channel's membership and drew the composer in a channel the viewer
// isn't in (TestPreviewFooterMembershipUnknownAtOpen, failing in α.135's
// release test job). Here the owner's partner fetch holds that gap open.
func TestResolveConvJoinerGetsTheAnswer(t *testing.T) {
	const dm, partner = "D0JOINER0001", "U0FAKE00002"
	a, fs := resolveJoinApp(t, func(fs *fakeslack.Server) { fs.AddClosedDM(dm, partner) })
	fs.SetFault("conversations.info", fakeslack.Fault{Delay: 300 * time.Millisecond})
	fs.SetFault("edge/users/info", fakeslack.Fault{Delay: 1500 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	owner := make(chan bool, 1)
	go func() {
		_, ok := a.ResolveConv(ctx, dm)
		owner <- ok
	}()
	for deadline := time.Now().Add(5 * time.Second); !a.inFlight("ci:" + dm); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the owner's conversations.info never went out")
		}
	}
	c, ok := a.ResolveConv(ctx, dm) // joins the owner's lookup
	if !ok || c.ID != dm || c.Kind != "im" || c.User != partner {
		t.Fatalf("the joiner got %+v %v, want the DM with %s", c, ok, partner)
	}
	if _, ok := a.User(partner); !ok {
		t.Errorf("the joiner returned before the DM's partner %s was fetched", partner)
	}
	if !<-owner {
		t.Error("the owner's lookup failed")
	}
}

// Many lookups of a channel the viewer isn't in at once, as a link opens
// one (the pane's lookup, History's side read, the header's): every
// joiner gets the channel, as a preview. The gap is a few instructions
// wide here, so this one stresses it (run it with -count).
func TestResolveConvJoinersOfAChannel(t *testing.T) {
	const n, callers = 12, 16
	ids := make([]string, n)
	a, fs := resolveJoinApp(t, func(fs *fakeslack.Server) {
		for i := range ids {
			ids[i] = fmt.Sprintf("C0JOINER%04d", i)
			fs.AddOtherChannel(slack.Conversation{ID: ids[i], Name: fmt.Sprintf("joiners-%d", i), Created: time.Now().Unix(),
				Creator: "U0FAKE00002", NumMembers: 2})
		}
	})
	fs.SetFault("conversations.info", fakeslack.Fault{Delay: 50 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, id := range ids {
		var wg sync.WaitGroup
		var mu sync.Mutex
		var bad []string
		start := make(chan struct{})
		for i := 0; i < callers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				c, ok := a.ResolveConv(ctx, id)
				if !ok || c.ID != id || !c.Preview {
					mu.Lock()
					bad = append(bad, fmt.Sprintf("%+v %v", c, ok))
					mu.Unlock()
				}
			}()
		}
		close(start)
		wg.Wait()
		if len(bad) > 0 {
			t.Errorf("%s: %d of %d lookups didn't get the preview channel: %s", id, len(bad), callers, bad[0])
		}
	}
}
