package ui

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// The workflow mention resolver (wfMentions.slug) called from several
// goroutines at once, as core converts a big page's messages on every
// core (convertEach), for a trigger core already holds: every call
// answers with the trigger. The first call marked the trigger asked and
// read core's cache only after, so a call in between answered "not
// loaded", and its message lost the mention (a share_shortcut card its
// icon) that converting one by one kept. Many rounds of eight callers
// let them meet in the window.
func TestWorkflowMentionSlugConcurrent(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	c := core.New(slack.New(fs.Creds()), store.New(""))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	want, err := c.Interactive().TriggerInfo(ctx, fakeslack.TriggerDeploy)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Interactive().Trigger(fakeslack.TriggerDeploy); !ok {
		t.Fatal("control: core doesn't hold the trigger it looked up")
	}
	const callers = 8
	for round := 0; round < 300; round++ {
		m := &wfMentions{c: c, got: map[string]richtext.WorkflowSlug{}, asked: map[string]bool{}}
		var start, done sync.WaitGroup
		start.Add(1)
		var misses atomic.Int32
		for k := 0; k < callers; k++ {
			done.Add(1)
			go func() {
				defer done.Done()
				start.Wait()
				if s, ok := m.slug(fakeslack.TriggerDeploy); !ok || s.Title != want.WorkflowTitle {
					misses.Add(1)
				}
			}()
		}
		start.Done()
		done.Wait()
		if n := misses.Load(); n > 0 {
			t.Fatalf("round %d: %d of %d calls at once for a trigger core holds answered \"not loaded\"; one by one each knows it", round, n, callers)
		}
	}
}
