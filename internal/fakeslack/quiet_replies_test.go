package fakeslack

import "testing"

// Replies to two threads of one channel, posted in the same second, each
// get a ts no other message of the channel has, as Slack's do. The fake
// numbered a reply from its own thread's count alone, so the second
// thread's first reply took the first thread's ts, and the UI's
// shown-toast key (channel:ts) read its toast as shown
// (TestReplyUnreadMarkInTheReplysFrame, once a second passed between its
// second root and the replies).
func TestQuietRepliesTakeTheirOwnTS(t *testing.T) {
	s, err := Start(Small)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ch := s.ChannelIDs()[2]
	root1, _ := s.PostQuiet(ch, SelfID, "a synthetic question, the first")
	root2, _ := s.PostQuiet(ch, SelfID, "a synthetic question, the second")
	seen := map[string]string{root1: "the first root", root2: "the second root"}
	for i := 0; i < 3; i++ {
		for _, root := range []string{root1, root2} {
			ts := s.PostReplyQuiet(ch, root, s.UserIDs()[3], "a synthetic answer")
			if was, ok := seen[ts]; ok {
				t.Fatalf("reply %d under %s has ts %s, %s's", i, root, ts, was)
			}
			seen[ts] = "a reply under " + root
		}
	}
	for _, root := range []string{root1, root2} {
		rs, ok := s.quietThread(ch, root)
		if !ok || len(rs) != 4 || rs[0].TS != root {
			t.Fatalf("the thread under %s: %d messages (%v), want the root and 3 replies", root, len(rs), ok)
		}
		for i := 2; i < len(rs); i++ {
			if !tsAfterEq(rs[i].TS, rs[i-1].TS) || rs[i].TS == rs[i-1].TS {
				t.Errorf("the thread under %s: reply %s after %s", root, rs[i].TS, rs[i-1].TS)
			}
		}
	}
}
