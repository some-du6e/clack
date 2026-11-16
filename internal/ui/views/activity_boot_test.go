package views

import (
	"image"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// A launch restored to Activity lays out before core's Boot finished:
// α.89 painted nothing for 2.3 s on one, its first frame waiting in a
// core snapshot that waited for Boot. With Boot never run (Ready stays
// open), the page lays out with an In filter (a channel, a section), the
// In popover's options, the "+" menu's suggestions, the live filter and a
// person's In id all answer, and no core call waits for Boot.
func TestActivityNeverWaitsForBoot(t *testing.T) {
	sz := fakeslack.Small
	sz.Views = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	s := store.New("")
	s.Lock()
	s.TeamID, s.TeamName = fakeslack.TeamID, "Synthetic Club"
	s.Unlock()
	s.PutUsers([]slack.User{{ID: fakeslack.SelfID, Name: "self"}, {ID: "U0FAKE00001", Name: "ada"}})
	s.PutConv(slack.Conversation{ID: fakeslack.ViewsChannel, Name: "views-fixtures", IsChannel: true, IsMember: true, NumMembers: 3})
	s.PutConv(slack.Conversation{ID: "D0FAKE00000", IsIM: true, User: "U0FAKE00001", IsMember: true})
	app := core.New(slack.New(fs.Creds()), s)
	offline := sync.OnceFunc(app.ServeOffline) // what lets a stuck frame finish
	var mu sync.Mutex
	var waits []string
	core.SetBootWaitHook(func(method, caller string) {
		mu.Lock()
		waits = append(waits, method+" from "+caller)
		mu.Unlock()
	})
	t.Cleanup(func() {
		core.SetBootWaitHook(nil)
		offline()
		app.Shutdown()
	})
	h := &vaHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	v := NewActivity(h)
	e := &vaEnv{size: image.Pt(1200, 1000)}
	e.draw = func(gtx layout.Context) {
		v.Layout(gtx)
		for _, o := range h.open() {
			o.Layout(gtx)
		}
	}
	// guard runs f off the test goroutine and fails when it takes 5 s
	// (it waits for Boot): then Boot's stand-in lets it finish
	guard := func(what string, f func()) {
		t.Helper()
		done := make(chan struct{})
		go func() { defer close(done); f() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			offline()
			<-done
			t.Fatalf("%s waited for core's Boot", what)
		}
	}
	for i := 0; i < 5; i++ {
		guard("the first frames", e.frame)
	}
	v.channels = []string{fakeslack.ViewsChannel}
	guard("In: one channel", func() {
		if txt, _, _ := v.inPillText(); txt == "" {
			t.Error("no In pill text")
		}
		e.frame()
		e.frame()
	})
	v.channels, v.sections = nil, []string{"S0FAKE0001"}
	guard("In: one section", func() { v.inPillText(); e.frame() })
	v.channels = []string{fakeslack.ViewsChannel, "D0FAKE00000"}
	guard("the live filter", func() {
		v.liveAccepts(&core.ActivityItem{Type: "at_user", Channel: fakeslack.ViewsChannel})
	})
	guard("the In popover", func() {
		v.in.init()
		v.inOptions()
		if id := v.inFilterID("U0FAKE00001"); id != "D0FAKE00000" {
			t.Errorf("a person's In id is %q, want their DM D0FAKE00000", id)
		}
	})
	guard("the + menu", func() {
		v.suggestions()
		v.plus.Click()
		e.frame()
		e.frame()
	})
	select {
	case <-app.Ready():
		t.Fatal("Ready closed: the test's Boot stand-in ran")
	default:
	}
	mu.Lock()
	defer mu.Unlock()
	if len(waits) > 0 {
		t.Errorf("core calls waited for Boot: %v", waits)
	}
}
