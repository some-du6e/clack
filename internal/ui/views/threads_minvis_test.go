package views

// A card's first shown reply on a new refreshId, Slack's
// getDefaultMinimumVisibleTs (iPpN) through getDerivedStateFromProps'
// minimumVisibleTsByThread.

import (
	"net/url"
	"slices"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// Three threads of seven replies, getView's window the three newest: one
// read to its newest shows its two newest and "Show 5 more replies"; one
// read to its second newest shows from that reply on; one never read
// shows every reply the card has until it is marked read, then its two
// newest (iPpN picked again while it was "").
func TestThreadsDefaultFirstReplyLikeSlack(t *testing.T) {
	ch := fakeslack.ViewsChannel
	type th struct{ root, r5, r6, r7 string }
	p := th{"1762620000.000100", "1762620500.000100", "1762620600.000100", "1762620700.000100"}
	q := th{"1762610000.000100", "1762610500.000100", "1762610600.000100", "1762610700.000100"}
	r := th{"1762600000.000100", "1762600500.000100", "1762600600.000100", "1762600700.000100"}
	var marks thrMarkLog
	app, _ := vkFake(t, func(method string, f url.Values) (any, bool) {
		marks.note(method, f)
		if method != "subscriptions.thread.getView" {
			return nil, false
		}
		fix := func(x th, lastRead string) map[string]any {
			return thrFixReplies(ch, x.root, 7, lastRead, thrReplyBy(x.r5, "U0FAKE00002"), thrReplyBy(x.r6, "U0FAKE00001"), thrReplyBy(x.r7, "U0FAKE00002"))
		}
		return thrOrderAnswer(fix(p, p.r7), fix(q, q.r6), fix(r, r.root)), true
	})
	h := newKBHost(app, false)
	h.away.Store(true) // nothing marked read until the last part
	v := NewThreads(h)
	e := thrViewEnv(t, v, 3)
	e.frame()
	e.frame()
	shown := func(x th) []string {
		key := ch + "/" + x.root
		d, _, _ := v.data.get()
		return thrTSs(thrShown(thrByKey(d.items, key), v.cards[key]))
	}
	if got, want := shown(p), []string{p.r6, p.r7}; !slices.Equal(got, want) {
		t.Errorf("a thread read to its newest reply shows %v, want its two newest %v", got, want)
	}
	if c := v.cards[ch+"/"+p.root]; c == nil || c.moreText != "Show 5 more replies" {
		t.Errorf("its row says %q, want Show 5 more replies", c.moreText)
	}
	if got, want := shown(q), []string{q.r6, q.r7}; !slices.Equal(got, want) {
		t.Errorf("a thread read to its second newest shows %v, want from it on %v", got, want)
	}
	if got, want := shown(r), []string{r.r5, r.r6, r.r7}; !slices.Equal(got, want) {
		t.Errorf("a thread never read shows %v, want every reply it has %v", got, want)
	}
	h.away.Store(false) // the attention back: r, on screen and unread, is marked read
	e.until(t, "r marked read", func() bool { return marks.has(r.root + "@" + r.r7 + " read=1") })
	e.frame()
	if got, want := shown(r), []string{r.r6, r.r7}; !slices.Equal(got, want) {
		t.Errorf("once read, the thread shows %v, want its two newest %v", got, want)
	}
	if got, want := shown(q), []string{q.r6, q.r7}; !slices.Equal(got, want) {
		t.Errorf("q's first shown reply moved after its mark: %v, want %v", got, want)
	}
}
