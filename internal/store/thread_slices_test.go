package store

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// TestThreadHistoryLikeSlack replays page sequences through the thread
// history (thread_slices.go) and holds every state to what Slack's own
// reducer made of them: client-boot 6Yed (addTimestamps, removeTimestamp,
// resetReachedEnd, truncateChannelHistory) over om2Z's slices, and dTxY's
// getPotentiallyOutdatedTimestamps at a dozen start timestamps, run in
// node by tools/oracles/thread_history.cjs on build 35b42de8b870
// (2026-11-19). The cases are a pane opened at the root and paged to the
// end with live replies after it, one opened around a reply and paged
// back to the start with a live reply past its end, two jumps bridged by
// pages, the newest page alone (threads_perf_quality) with a disconnect,
// deletions, and a truncation past 1,050 timestamps.
func TestThreadHistoryLikeSlack(t *testing.T) {
	b, err := os.ReadFile("testdata/thread_history_slack.json")
	if err != nil {
		t.Fatal(err)
	}
	var snap struct {
		Select []string `json:"select"`
		Cases  []struct {
			Name   string              `json:"name"`
			Ops    [][]json.RawMessage `json:"ops"`
			States []struct {
				Slices [][]any `json:"slices"`
				RS     string  `json:"rs"`
				RE     string  `json:"re"`
				Pre    bool    `json:"pre"`
				Sel    [][]any `json:"sel"`
			} `json:"states"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(b, &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Cases) == 0 {
		t.Fatal("no cases")
	}
	ts := func(i int) string { return fmt.Sprintf("%d.000100", 1700000000+i) }
	const ch, root = "C1", "1700000000.000100"
	k := ch + "/" + root
	flag := func(v tbool) string {
		switch v {
		case tTrue:
			return "true"
		case tFalse:
			return "false"
		}
		return "unset" // Slack's undefined; resetReachedEnd's null reads the same to every check
	}
	for _, c := range snap.Cases {
		t.Run(c.Name, func(t *testing.T) {
			s := New(t.TempDir())
			for i, op := range c.Ops {
				var kind string
				if err := json.Unmarshal(op[0], &kind); err != nil {
					t.Fatal(err)
				}
				var idx []int
				var one int
				if len(op) > 1 {
					if json.Unmarshal(op[1], &idx) != nil {
						_ = json.Unmarshal(op[1], &one)
					}
				}
				tss := make([]string, len(idx))
				for j, x := range idx {
					tss[j] = ts(x)
				}
				var bounds struct {
					Oldest  *int            `json:"oldest"`
					Latest  *int            `json:"latest"`
					HasMore json.RawMessage `json:"hasMore"`
				}
				if len(op) > 2 {
					_ = json.Unmarshal(op[2], &bounds)
				}
				pb := ThreadPageBounds{}
				if bounds.Oldest != nil {
					pb.Oldest = ts(*bounds.Oldest)
				}
				if bounds.Latest != nil {
					pb.Latest = ts(*bounds.Latest)
				}
				var ends struct {
					Start *bool `json:"hasMoreStart"`
					End   *bool `json:"hasMoreEnd"`
				}
				if json.Unmarshal(bounds.HasMore, &pb.HasMore) != nil && json.Unmarshal(bounds.HasMore, &ends) == nil && ends.Start != nil {
					pb.Ends, pb.HasMoreStart, pb.HasMoreEnd = true, *ends.Start, ends.End != nil && *ends.End
				}
				s.mu.Lock()
				switch kind {
				case "add":
					s.addTimestampsLocked(k, tss, pb)
				case "live":
					s.addTimestampsLocked(k, tss, ThreadPageBounds{NoBounds: true})
				case "remove":
					s.removeTimestampLocked(k, ts(one))
				case "reset":
				case "truncate":
					// the messages store holds the listed replies, as reads left them
					var th []slack.Message
					for _, sl := range s.threadHist[k].slices {
						for _, x := range sl.tss {
							th = append(th, slack.Message{TS: x, ThreadTS: root})
						}
					}
					s.threads[k] = th
				default:
					t.Fatalf("op %d: unknown %q", i, kind)
				}
				s.mu.Unlock()
				switch kind {
				case "reset":
					s.ResetThreadReachedEnd(ch, root)
				case "truncate":
					s.TruncateThread(ch, root, false)
				}
				want := c.States[i]
				s.mu.RLock()
				a := s.threadHist[k]
				var got [][]any
				rs, re, pre := "unset", "unset", false
				if a != nil {
					for _, sl := range a.slices {
						first, last := "", ""
						if len(sl.tss) > 0 {
							first, last = sl.tss[0], sl.tss[len(sl.tss)-1]
						}
						got = append(got, []any{sl.start, sl.end, float64(len(sl.tss)), first, last})
					}
					rs, re, pre = flag(a.reachedStart), flag(a.reachedEnd), a.prevReachedEnd
				}
				s.mu.RUnlock()
				wantRE := want.RE
				if wantRE == "null" {
					wantRE = "unset"
				}
				if fmt.Sprint(got) != fmt.Sprint(want.Slices) || rs != want.RS || re != wantRE || pre != want.Pre {
					t.Fatalf("after op %d (%s): slices %v reachedStart %s reachedEnd %s prevReachedEnd %v;\nSlack's %v %s %s %v",
						i, kind, got, rs, re, pre, want.Slices, want.RS, want.RE, want.Pre)
				}
				for j, at := range snap.Select {
					sl, _ := s.ThreadSliceAt(ch, root, at)
					var g []any
					if !sl.Empty() || sl.ReachedStart || sl.ReachedEnd {
						first, last := "", ""
						if len(sl.TSs) > 0 {
							first, last = sl.TSs[0], sl.TSs[len(sl.TSs)-1]
						}
						g = []any{sl.Start, sl.End, float64(len(sl.TSs)), first, last, sl.ReachedStart, sl.ReachedEnd, sl.PrevReachedEnd}
					}
					w := want.Sel[j]
					if len(w) == 8 && w[0] == "" && w[1] == "" && w[2] == float64(0) && w[5] == false && w[6] == false && w[7] == false {
						w = nil // {...f, prevReachedEnd: false}: the empty slice still
					}
					if fmt.Sprint(g) != fmt.Sprint(w) {
						t.Errorf("after op %d (%s), the slice at %q: %v; Slack's %v", i, kind, at, g, w)
					}
				}
			}
		})
	}
}

// TestThreadPageKeepsRootOutOfSlice: a page short of the thread's start
// lists its replies but not the root (requestReplies drops it), while the
// messages store keeps the root's copy for the pane (rootMsg); a live
// reply past a slice that stops short of the end makes a slice of its
// own and leaves the pane's slice as it was.
func TestThreadPageKeepsRootOutOfSlice(t *testing.T) {
	s := New(t.TempDir())
	const ch, root = "C1", "1700000000.000100"
	ts := func(i int) string { return fmt.Sprintf("%d.000100", 1700000000+i) }
	msgs := []slack.Message{{TS: root, ThreadTS: root, ReplyCount: 999, Text: "root"}}
	var tss []string
	for i := 500; i <= 527; i++ {
		msgs = append(msgs, slack.Message{TS: ts(i), ThreadTS: root, Text: "r"})
		tss = append(tss, ts(i))
	}
	s.AddThreadPageSince(ch, root, msgs, tss, ThreadPageBounds{Oldest: ts(500), HasMore: true}, s.LiveSeq())
	sl, ok := s.ThreadSliceAt(ch, root, ts(510))
	if !ok || len(sl.TSs) != 28 || sl.TSs[0] != ts(500) || sl.ReachedStart || sl.ReachedEnd {
		t.Fatalf("the slice at reply 510: %d timestamps from %v, start %v end %v", len(sl.TSs), sl.TSs, sl.ReachedStart, sl.ReachedEnd)
	}
	th, _ := s.Thread(ch, root)
	if len(th) != 29 || th[0].TS != root {
		t.Fatalf("the messages store holds %d of the thread, first %q; want the root and 28 replies", len(th), th[0].TS)
	}
	s.AddReply(ch, slack.Message{TS: ts(1000), ThreadTS: root, Text: "live"})
	if got, _ := s.ThreadSliceAt(ch, root, ts(510)); len(got.TSs) != 28 {
		t.Errorf("a live reply past the slice's end changed it: %d timestamps", len(got.TSs))
	}
	if got, _ := s.ThreadSliceAt(ch, root, ts(1000)); len(got.TSs) != 1 || got.TSs[0] != ts(1000) {
		t.Errorf("the live reply's own slice: %v", got.TSs)
	}
	if msgs := s.ThreadSliceMsgs(ch, root, sl); len(msgs) != 28 || msgs[0].TS != ts(500) {
		t.Errorf("the slice's messages: %d, first %q", len(msgs), msgs[0].TS)
	}
	// closing it in a public channel the viewer isn't in drops it whole
	if !s.TruncateThread(ch, root, true) || s.ThreadHeld(ch, root) {
		t.Error("removeAllTimestamps kept the thread's history")
	}
	if _, ok := s.Thread(ch, root); ok {
		t.Error("removeAllTimestamps kept the thread's replies")
	}
}

// TestResetReachedEndCoversThreads: the slow connect's one reset
// (ResetReachedEnd, history-gap's) makes every thread history's end stale
// along with the channels' windows, as Slack's resetReachedEnd({}) covers
// every key: the pane keeps its slice (prevReachedEnd), a reply the socket
// brings after it makes a slice of its own, and the read past the old end
// that reaches the thread's end joins it.
func TestResetReachedEndCoversThreads(t *testing.T) {
	s := New(t.TempDir())
	const ch, root = "C1", "1700000000.000100"
	ts := func(i int) string { return fmt.Sprintf("%d.000100", 1700000000+i) }
	s.SetLatest(ch, []slack.Message{{TS: root, ThreadTS: root, ReplyCount: 3}}, false) // the channel's window
	msgs := []slack.Message{{TS: root, ThreadTS: root, ReplyCount: 3}}
	tss := []string{root}
	for i := 1; i <= 3; i++ {
		msgs = append(msgs, slack.Message{TS: ts(i), ThreadTS: root})
		tss = append(tss, ts(i))
	}
	s.AddThreadPageSince(ch, root, msgs, tss, ThreadPageBounds{Oldest: root, HasMore: false}, s.LiveSeq())
	s.AddThreadPageSince(ch, root, msgs[:1], []string{root}, ThreadPageBounds{Latest: root, HasMore: false}, s.LiveSeq())
	if sl, _ := s.ThreadSliceAt(ch, root, root); !sl.ReachedEnd || !sl.ReachedStart || len(sl.TSs) != 4 {
		t.Fatalf("before the gap: %d timestamps, reachedStart %v reachedEnd %v; want the whole thread", len(sl.TSs), sl.ReachedStart, sl.ReachedEnd)
	}
	if n := s.ResetReachedEnd(nil); n != 2 {
		t.Errorf("the reset covered %d, want the channel's window and the thread's history", n)
	}
	if _, _, stale := s.EndOf(ch); !stale {
		t.Error("the channel's window end isn't stale")
	}
	sl, _ := s.ThreadSliceAt(ch, root, root)
	if sl.ReachedEnd || !sl.PrevReachedEnd || len(sl.TSs) != 4 {
		t.Fatalf("after the reset: %d timestamps, reachedEnd %v prevReachedEnd %v; want the slice kept, its end stale", len(sl.TSs), sl.ReachedEnd, sl.PrevReachedEnd)
	}
	s.AddReply(ch, slack.Message{TS: ts(5), ThreadTS: root}) // the socket's reply after the connect
	if sl, _ := s.ThreadSliceAt(ch, root, root); len(sl.TSs) != 4 {
		t.Errorf("the socket's reply past the stale end joined the pane's slice: %v", sl.TSs)
	}
	// the pane's read past the old end reaches the thread's end
	page := []slack.Message{{TS: root, ThreadTS: root}, {TS: ts(3), ThreadTS: root}, {TS: ts(4), ThreadTS: root}, {TS: ts(5), ThreadTS: root}}
	s.AddThreadPageSince(ch, root, page, []string{ts(3), ts(4), ts(5)}, ThreadPageBounds{Oldest: ts(3), HasMore: false}, s.LiveSeq())
	sl, _ = s.ThreadSliceAt(ch, root, root)
	if !sl.ReachedEnd || sl.PrevReachedEnd || len(sl.TSs) != 6 {
		t.Errorf("after the read: %d timestamps, reachedEnd %v prevReachedEnd %v; want the gap's reply and the socket's joined, the end reached", len(sl.TSs), sl.ReachedEnd, sl.PrevReachedEnd)
	}
	if all, _, _, _, _ := s.ThreadHistory(ch, root); len(all) != 1 {
		t.Errorf("%d slices after the read, want one", len(all))
	}
}
