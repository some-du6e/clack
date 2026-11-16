package taut

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// A channel's messages fingerprint is the same every time while nothing
// changed, however many threads are cached: the sweep re-sends the slice
// to JS when it differs, and a fingerprint that walked the threads in map
// order re-rendered every messages-reading plugin's rows every 750 ms.
func TestMessagesFingerprintStable(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 12; i++ {
		root := fmt.Sprintf("17000001%02d.000100", i)
		h.app.S.SetThread(chGen, root, []slack.Message{
			{Type: "message", TS: root, User: "UALICE", Text: "root", ThreadTS: root, ReplyCount: 1},
			{Type: "message", TS: fmt.Sprintf("17000002%02d.000100", i), User: "UBOB", Text: "reply", ThreadTS: root},
		})
	}
	m := h.tt.mirror
	fp := m.messagesFP(chGen)
	for i := 0; i < 50; i++ {
		if got := m.messagesFP(chGen); got != fp {
			t.Fatalf("fingerprint changed with nothing changed (%x then %x)", fp, got)
		}
	}
}

// The fingerprints hash in place: the values hash/fnv gave over the bytes
// the copying code built, with next to no allocation (the sweep runs them
// for every watched channel and member every 750 ms).
func TestFingerprintsInPlace(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 3; i++ {
		root := fmt.Sprintf("17000003%02d.000100", i)
		h.app.S.SetThread(chGen, root, []slack.Message{
			{Type: "message", TS: root, User: "UALICE", Text: "root", ThreadTS: root, ReplyCount: 1,
				Reactions: []slack.Reaction{{Name: "eyes", Count: 2}}, Edited: &slack.Edited{TS: "1700000400.000100"}},
			{Type: "message", TS: fmt.Sprintf("17000004%02d.000100", i), User: "UBOB", Text: "reply", ThreadTS: root, Subtype: "thread_broadcast"},
		})
	}
	old := func() uint64 {
		f := fnv.New64a()
		write := func(sm *slack.Message) {
			f.Write([]byte(sm.TS))
			if sm.Edited != nil {
				f.Write([]byte(sm.Edited.TS))
			}
			f.Write([]byte(strconv.Itoa(sm.ReplyCount)))
			f.Write([]byte(strconv.Itoa(len(sm.Text))))
			for _, r := range sm.Reactions {
				f.Write([]byte(r.Name))
				f.Write([]byte(strconv.Itoa(r.Count)))
			}
			f.Write([]byte(strconv.Itoa(len(sm.Files))))
			f.Write([]byte(sm.Subtype))
		}
		if tl := h.app.S.Timeline(chGen); tl != nil {
			for i := range tl.Msgs {
				write(&tl.Msgs[i])
			}
		}
		threads := h.app.S.ThreadsIn(chGen)
		roots := make([]string, 0, len(threads))
		for r := range threads {
			roots = append(roots, r)
		}
		sort.Strings(roots)
		for _, r := range roots {
			f.Write([]byte(r))
			for i := range threads[r] {
				write(&threads[r][i])
			}
		}
		return f.Sum64()
	}
	m := h.tt.mirror
	if got, want := m.messagesFP(chGen), old(); got != want {
		t.Fatalf("messages fingerprint %x, the copying code's %x", got, want)
	}
	u, _ := h.app.S.RawUser("UALICE")
	f := fnv.New64a()
	f.Write([]byte(strconv.FormatInt(u.Updated, 10)))
	f.Write([]byte(u.Name + "\x00" + u.RealName + "\x00" + u.Profile.DisplayName + "\x00" + u.Profile.StatusText + "\x00" + u.Profile.StatusTextCanonical + "\x00" + u.Profile.StatusEmoji + "\x00" + u.Profile.Image72))
	if got := m.memberFP("UALICE"); got != f.Sum64() {
		t.Fatalf("member fingerprint %x, the concatenating code's %x", got, f.Sum64())
	}
	if raceOn {
		return // the race detector's instrumentation allocates
	}
	if a := testing.AllocsPerRun(50, func() { m.messagesFP(chGen) }); a > 3 {
		t.Errorf("messagesFP: %.0f allocations a call (the thread keys' slice and the closure at most)", a)
	}
	if a := testing.AllocsPerRun(50, func() { m.memberFP("UALICE") }); a > 0 {
		t.Errorf("memberFP: %.0f allocations a call", a)
	}
}
