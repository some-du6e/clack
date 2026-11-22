package messages

// A jump window's page (requestOlder, requestNewer) answers with core's
// whole window as it was when the page landed in core (WindowOlder,
// WindowNewer), and the landing put it over the window's model. What the
// hub took from the socket after that and before the landing went: a
// deleted message came back, an edit or a reaction was undone, and a
// rename's conversion of the window's messages (reconvertIn) was written
// over and never made again, since a newer page didn't count as a load
// out. The page now keeps them, as the window's first load does
// (takeLive, keepLive; the rename recorded and made again by afterLoad).
// Each change lands as the page's answer is in (loadLanded). Synthetic
// history on the fake.

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// TestWindowPageKeepsLiveChanges: a message deleted while the window's
// newer page is out stays out of the window.
func TestWindowPageKeepsLiveChanges(t *testing.T) {
	e := newWinEnv(t)
	s := e.v.s
	w, hb := s.win, s.hb
	w.mu.Lock()
	gone := w.msgs[len(w.msgs)-2].m.TS // in the window, and in core's window the page answers with
	w.mu.Unlock()
	var once sync.Once
	hook := func(lm *model) {
		lm.mu.Lock()
		newer := lm.newer
		lm.mu.Unlock()
		if lm == w && newer {
			once.Do(func() { hb.apply(core.Event{Type: "delete", Channel: winEndChan, TS: gone}) })
		}
	}
	loadLanded.Store(&hook)
	t.Cleanup(func() { loadLanded.Store(nil) })
	w.mu.Lock()
	last := w.msgs[len(w.msgs)-1].m.TS
	w.mu.Unlock()
	if !hb.loadWindowPage(w, false) {
		t.Fatal("the window's newer page wasn't asked for")
	}
	until(t, 10*time.Second, "the newer page", func() {}, func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		return !w.newer && len(w.msgs) > 0 && tsLess(last, w.msgs[len(w.msgs)-1].m.TS)
	})
	w.mu.Lock()
	back := w.find(gone) >= 0
	w.mu.Unlock()
	if back {
		t.Errorf("the window's newer page put back message %d, deleted while the page was out", e.index(gone))
	}
}

// TestRenameDuringWindowPage: a rename that lands while the window's newer
// page is out shows in the window's rows once the page is in (a file's
// owner, Cara, renamed kit.l).
func TestRenameDuringWindowPage(t *testing.T) {
	app, fs, _ := shotsApp(t)
	app.S.PutConv(slack.Conversation{ID: refsChan, Name: "launch", IsChannel: true, IsMember: true, NumMembers: 12})
	for i := 0; i < 60; i++ {
		fs.PostQuiet(refsChan, "UBEN00001", fmt.Sprintf("older %d", i))
	}
	file := slack.File{ID: "F0WINPAGE1", Name: "plan.pdf", Title: "Launch plan", Mimetype: "application/pdf", Filetype: "pdf", PrettyType: "PDF",
		Mode: "hosted", User: "UCARA0001", Size: 2048, URLPrivate: "https://files.invalid/plan.pdf", Permalink: "https://files.invalid/p/plan"}
	fileTS := fs.PostFilesQuiet(refsChan, "UANA00001", "Here's the plan", []slack.File{file})
	for i := 0; i < 120; i++ {
		fs.PostQuiet(refsChan, "UBEN00001", fmt.Sprintf("newer %d", i))
	}
	hb := hubFor(coreHost{app: app, th: theme.New(false)})
	w := hb.newWindow(refsChan)
	t.Cleanup(func() { hb.dropWindow(w) })
	hb.loadWindow(w, fileTS)
	until(t, 10*time.Second, "the window around the file's message", func() {}, func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.loaded && w.around == "" && w.newestMissing && w.find(fileTS) >= 0
	})
	owners := func() []string {
		w.mu.Lock()
		defer w.mu.Unlock()
		if i := w.find(fileTS); i >= 0 {
			return refsOwners(&w.msgs[i].m)
		}
		return nil
	}
	var once sync.Once
	hook := func(lm *model) {
		lm.mu.Lock()
		newer := lm.newer
		lm.mu.Unlock()
		if lm != w || !newer {
			return
		}
		once.Do(func() {
			refsUserChange(app, "kit.l") // as the page's answer is in, before it's in the model
			for dl := time.Now().Add(5 * time.Second); time.Now().Before(dl); time.Sleep(5 * time.Millisecond) {
				if got := owners(); len(got) == 1 && got[0] == "kit.l" {
					return // the window converted it again before the landing
				}
			}
			t.Error("the rename never reached the window's message")
		})
	}
	loadLanded.Store(&hook)
	t.Cleanup(func() { loadLanded.Store(nil) })
	if !hb.loadWindowPage(w, false) {
		t.Fatal("the window's newer page wasn't asked for")
	}
	until(t, 10*time.Second, "the file's owner renamed in the window after its newer page", func() {}, func() bool {
		w.mu.Lock()
		out := w.newer
		w.mu.Unlock()
		got := owners()
		return !out && len(got) == 1 && got[0] == "kit.l"
	})
}

// TestLiveReadsWindow: Live (Activity's rows draw the copy a list draws in
// the same frame) finds a message only a jump's window holds, at the
// window model's revision. It read the conversation's and the threads'
// models only, so Activity drew a copy of its own.
func TestLiveReadsWindow(t *testing.T) {
	e := newWinEnv(t)
	ts := e.tss[40]
	m, rev, ok := Live(e.h.app, winEndChan, ts, "", e.rt.now.Add(-time.Hour), 0)
	if !ok || m.TS != ts {
		t.Fatalf("Live didn't find message 40, which the window draws (found %v)", ok)
	}
	if w := e.v.s.win; rev>>32 != uint64(w.id) {
		t.Errorf("Live's copy of message 40 is model %d's, not the window's (%d)", rev>>32, w.id)
	}
}
