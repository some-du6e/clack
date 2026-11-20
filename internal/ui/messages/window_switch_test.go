package messages

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// TestWindowSwitchCountsFromItsModel: a list switches models when a jump
// shows the window around a message (openWindow) and when it goes back to
// the conversation's newest messages (the hand-over, leaveWindow), as it
// does when the hub made a new model for the conversation (adopt), and
// revisions and evictions count per model. The window switches kept the
// counts of the model before. A list that had let its rows go for one of
// core's evictions (releaseEvicted) read the window, never evicted, as
// evicted, and the first sweep that found it off screen (another
// conversation shown) let the window's rows, its place and its jump go:
// back in the conversation, the list wasn't where the reader left it. And
// a row kept the number the window gave its message's copy: the
// conversation's copy, given the same number (each model numbers from its
// own first load), never drew, nor its reaction. The hub's loads are off:
// the test plays the models.
func TestWindowSwitchCountsFromItsModel(t *testing.T) {
	hb := &hub{}
	hb.authFailed.Store(true)
	ts := func(i int) string { return fmt.Sprintf("1762500%03d.000100", i) }
	page := func(from, to int, text string) []core.Message {
		var out []core.Message
		for i := from; i < to; i++ {
			out = append(out, core.Message{TS: ts(i), Channel: "C1", User: "UANA00001", Text: text})
		}
		return out
	}
	chm := &model{ch: "C1", typing: map[string]typingEntry{}}
	chm.setAll(page(900, 910, "newest"))
	chm.loaded, chm.hasMore = true, true
	s := &listState{hb: hb, ch: "C1", chm: chm, m: chm, rows: map[string]*row{}}
	prevC, prevT := lastChannel, lastThread
	t.Cleanup(func() { lastChannel, lastThread = prevC, prevT })

	// off screen, the list lets its rows go for core's trim of the
	// conversation (Slack's message cache budget)
	lastChannel, lastThread = nil, nil
	chm.evict(ts(902))
	if !s.releaseEvicted() {
		t.Fatal("the list off screen kept its rows through the eviction")
	}

	// shown again, a jump to an old message: the window around it
	lastChannel = s
	s.openWindow(ts(100))
	win := s.win
	if win == nil || s.m != win {
		t.Fatal("the jump to an old message didn't show its window")
	}
	win.mu.Lock()
	win.setAll(page(86, 114, "window"))
	win.loaded, win.hasMore, win.newestMissing = true, true, true
	jumpRev := win.msgs[win.find(ts(100))].rev
	win.mu.Unlock()
	s.rows[ts(100)] = &row{rev: jumpRev}
	s.place = jumpPlace{key: ts(100)}
	s.list.Position = layout.Position{BeforeEnd: true, First: 14, Offset: -40}
	s.positioned, s.laidOut = true, true

	// another conversation shows: the sweep finds the list off screen
	lastChannel = nil
	if s.releaseEvicted() {
		t.Errorf("the window, never evicted, let the list's rows go: %d rows, place %q, first item %d, positioned %v",
			len(s.rows), s.place.key, s.list.Position.First, s.positioned)
	} else if len(s.rows) != 1 || s.place.key != ts(100) || s.list.Position.First != 14 || !s.positioned {
		t.Errorf("off screen, the list lost its place in the window: %d rows, place %q, first item %d", len(s.rows), s.place.key, s.list.Position.First)
	}

	// back in the conversation, the window meets the newest messages and
	// goes over to the conversation's model, whose copy of one message has
	// the reaction the window's read missed, under the number the window
	// gave its own copy
	lastChannel = s
	shared := ts(905)
	win.mu.Lock()
	win.setAll(append(page(86, 114, "window"), page(900, 910, "newest")...))
	win.merged, win.newestMissing = true, false
	win.mu.Unlock()
	chm.mu.Lock()
	i := chm.find(shared)
	msg := chm.msgs[i].m
	msg.Reactions = []core.Reaction{{Name: "tada", Count: 1, Users: []string{"UBEN00001"}}}
	chm.msgs[i] = entry{m: msg, rev: chm.bump(), seq: 1}
	sharedRev := chm.msgs[i].rev
	chm.readGen, chm.readOK = 3, 4 // a read begun after the join has landed
	chm.mu.Unlock()
	s.rows[shared] = &row{rev: sharedRev, viewed: true, viewReady: true} // drawing a plugin's view of the window's copy
	s.place = jumpPlace{}
	if !s.handOver() || s.win != nil || s.m != chm {
		t.Fatal("the window that met the newest messages didn't go over to the conversation's model")
	}
	chm.mu.Lock()
	e := chm.msgs[chm.find(shared)]
	chm.mu.Unlock()
	if r := s.rows[shared]; r.rev == e.rev {
		t.Errorf("the row kept the number %d the window gave its copy, the number of the conversation's copy: it never draws that copy's reaction", r.rev)
	}
	if r := s.rows[shared]; r.viewReady {
		// applyView's first ready read after the switch looks at JS's view
		// only when it turned ready: a view that changed meanwhile stayed
		t.Error("the viewed row kept its view's ready mark through the switch: JS's view of the new copy is never compared")
	}

	// the switch counts from the conversation's own evictions: none since
	// it, then core's next
	lastChannel = nil
	if s.releaseEvicted() {
		t.Fatal("off screen after the hand-over, the list let its rows go for an eviction from before it")
	}
	chm.mu.Lock()
	chm.evict(ts(906))
	chm.mu.Unlock()
	if !s.releaseEvicted() {
		t.Fatal("core's next eviction of the conversation wasn't seen after the hand-over")
	}
}

// TestWindowLoadTakesUpdateWhileOut: an update of a message a jump's
// window is loading, landing once the answer is in and before it's in
// the model (core's merge of what changed during the calls has run),
// changes the answer's copy, as it does the conversation's model's
// (notePlace, keepLive). The window holds nothing while its load is out,
// and it took only changes to messages it held: it drew the copy read
// before the update until something else changed the message. Through
// the hub's real window load (Slack's two calls around the message, on
// the fake), the update landing as the answer is in (loadLanded).
func TestWindowLoadTakesUpdateWhileOut(t *testing.T) {
	app, fs, _ := shotsApp(t)
	const ch = "C0WINLIVE01"
	app.S.PutConv(slack.Conversation{ID: ch, Name: "archive-walk", IsChannel: true, IsMember: true, NumMembers: 12})
	var tss []string
	for i := 0; i < 80; i++ {
		ts, _ := fs.PostQuiet(ch, "UBEN00001", fmt.Sprintf("message %d", i))
		tss = append(tss, ts)
	}
	hb := hubFor(coreHost{app: app, th: theme.New(false)})
	w := hb.newWindow(ch)
	t.Cleanup(func() { hb.dropWindow(w) })
	target, edited := tss[20], tss[21]
	const text = "edited while the window loaded"
	const seq = uint64(1) << 40
	var once sync.Once
	hook := func(lm *model) {
		if lm != w {
			return
		}
		once.Do(func() {
			m := core.Message{TS: edited, Channel: ch, User: "UBEN00001", Text: text, Edited: true, EditedTS: tss[79]}
			hb.apply(core.Event{Type: "update", Channel: ch, Msg: &m, EmitSeq: seq})
		})
	}
	loadLanded.Store(&hook)
	t.Cleanup(func() { loadLanded.Store(nil) })
	hb.loadWindow(w, target)
	until(t, 10*time.Second, "the window around the message", func() {}, func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.loaded && !w.loading && w.around == ""
	})
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil || w.find(target) < 0 {
		t.Fatalf("the window around %s didn't load (%v)", target, w.err)
	}
	i := w.find(edited)
	if i < 0 {
		t.Fatalf("the window around %s doesn't hold the message after it, %s", target, edited)
	}
	if e := w.msgs[i]; e.m.Text != text || e.seq != seq {
		t.Fatalf("the window holds %q (seq %d): the copy read before the update", e.m.Text, e.seq)
	}
}
