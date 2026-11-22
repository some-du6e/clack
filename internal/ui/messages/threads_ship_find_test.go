package messages

import (
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Find in thread over a thread too long to page through (threads-ship
// finding 8): a result the pane's slice doesn't hold loads the slice at
// it, as a jump to a reply does (two pages of 28 around it), and lands
// on screen, flashed. Slack's loadAndScrollToTs pages older from the
// slice's start until the result shows or the thread's start is reached
// (boot-shared l6n6 loadMoreForPendingScroll, checkPendingScrollToTs),
// and so did Clack: a result newer than the slice never landed, an old
// one in a long thread was thousands of pages away, and closing the bar
// left the paging going. Synthetic data only: the fake's huge thread
// makes every reply up on request (huge_thread.go).

// findHuge opens a 200,000-reply thread in its pane, with find_in_thread
// on, and its search answering needle with reply far and haystack with
// reply old.
func findHuge(t *testing.T, far, old int, at func(*ThreadView, func(int) string)) (*ThreadView, *fakeslack.Server, *findHarness, func(int) string) {
	t.Helper()
	app, fs := headerEnv(t)
	th := fs.AddHugeThread("find-in-thread-fixture", 200000, 40)
	fs.SetHugeThreadFind("needle", far)
	fs.SetHugeThreadFind("haystack", old)
	h := coreHost{app: app, th: theme.New(false)}
	tv := NewThreadView(h, th.Channel, th.Root)
	reply := fs.HugeThreadReplyTS
	if at != nil {
		at(tv, reply)
	}
	fh := newFindHarness(tv, h.th)
	fh.until(t, "the thread's first page", func() bool { return len(tv.s.snap.msgs) > 20 && !tv.s.snap.loading })
	app.SetExperimentGroup("find_in_thread", "on")
	fh.frame()
	if !tv.OpenFind() {
		t.Fatal("no find bar with find_in_thread on")
	}
	fh.frame()
	return tv, fs, fh, reply
}

// findFor types q in the bar and waits for its one result.
func (fh *findHarness) findFor(t *testing.T, q string) {
	t.Helper()
	fh.tv.SetFindQuery(q)
	fh.frame()
	fh.until(t, "the answer for "+q, func() bool {
		_, res, _, loading := fh.tv.FindState()
		return !loading && len(res) == 1
	})
}

func onScreen(tv *ThreadView, ts string) bool {
	for _, r := range tv.RowRects() {
		if r.TS == ts {
			return true
		}
	}
	return false
}

// TestFindInThreadNewerResultLands: the pane open at the root of the
// thread, its slice the root's page; the result is reply 150,009: it
// lands on screen and flashes, with the two reads around it.
func TestFindInThreadNewerResultLands(t *testing.T) {
	const far = 150009
	tv, fs, fh, reply := findHuge(t, far, 30, nil)
	before := len(fs.HugeThreadRequests())
	fh.findFor(t, "needle")
	fh.until(t, "the result on screen", func() bool { return onScreen(tv, reply(far)) })
	if n := len(fs.HugeThreadRequests()) - before; n > 3 {
		t.Errorf("the result took %d replies reads to land, want the two around it", n)
	}
	if tv.s.findFlash != reply(far) {
		t.Errorf("flashing %q, want the result %s", tv.s.findFlash, reply(far))
	}
}

// TestFindInThreadClosedStopsReading: the pane open at reply 150,000 (a
// jump); the result is reply 30, far older than the slice, and the bar
// closes as soon as it answers: the result's slice loads (the two reads
// around it), and nothing more is read for the find.
func TestFindInThreadClosedStopsReading(t *testing.T) {
	const old = 30
	tv, fs, fh, reply := findHuge(t, 150009, old, func(tv *ThreadView, reply func(int) string) { tv.JumpTo(reply(150000), false) })
	if onScreen(tv, reply(old)) {
		t.Fatal("the result is on screen before the find")
	}
	fh.findFor(t, "haystack")
	fh.press(key.NameEscape, 0)
	if tv.find.active {
		t.Fatal("Escape left the bar open")
	}
	settle := func(d time.Duration) {
		for dl := time.Now().Add(d); time.Now().Before(dl); time.Sleep(20 * time.Millisecond) {
			fh.frame()
		}
	}
	settle(time.Second)
	at := len(fs.HugeThreadRequests())
	settle(2 * time.Second)
	if n := len(fs.HugeThreadRequests()) - at; n > 0 {
		t.Errorf("%d replies reads after the bar closed: %v", n, fs.HugeThreadRequests()[at:])
	}
}
