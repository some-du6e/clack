package messages

// Karim, 2026-11-22 (α.143 on Windows): "two different pfps?". One
// sender, the same name and status emoji, drawn with two pictures in one
// public channel: a thread broadcast from the night before with the
// picture they had then, a plain message from that afternoon with the one
// they uploaded that day. Slack's sender reads the member from the store
// on every render (getMemberById), so every row draws the member as they
// are now. Clack's rows keep what they resolved until the hub's usersRev
// moves, and a profile change that came while no list drew the member
// moved nothing: the rows drawn before kept the old picture, rows drawn
// first afterwards the new one. Synthetic ids, names and URLs.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const (
	avCh      = "C0AVATARS1"
	avOther   = "C0AVOTHER1"
	avRoot    = "1700100000.000100" // UBOB's thread
	avBcast   = "1700100100.000100" // UALICE's reply, sent to the channel too
	avPlain   = "1700140000.000100" // UALICE, eleven hours later
	avBobOnly = "1700100300.000100"
	avMarker  = "1700150100.000100" // UBOB, after the change
	avNewPic  = "https://avatars.invalid/alice-new-48.png"
	avNewName = "alice.renamed"
	avNewStat = ":palm_tree:"
)

// avatarEnv is the channel with UALICE's broadcast and plain message, and
// another channel only UBOB posts in.
func avatarEnv(t *testing.T) (*ChannelView, func(), *ChannelView, func(), coreHost) {
	app, _ := newTestApp(t)
	s := app.S
	s.PutConv(slack.Conversation{ID: avCh, Name: "avatars", IsChannel: true, IsMember: true, NumMembers: 3})
	s.PutConv(slack.Conversation{ID: avOther, Name: "elsewhere", IsChannel: true, IsMember: true, NumMembers: 2})
	root := slack.Message{Type: "message", TS: avRoot, User: "UBOB", Text: "a synthetic thread", ThreadTS: avRoot, ReplyCount: 1,
		LatestReply: avBcast, ReplyUsers: []string{"UALICE"}}
	rootCopy := root
	s.SetLatest(avCh, []slack.Message{
		root,
		{Type: "message", TS: avBcast, User: "UALICE", Subtype: "thread_broadcast", ThreadTS: avRoot, Root: &rootCopy, Text: "a synthetic reply, also sent to the channel"},
		{Type: "message", TS: avPlain, User: "UALICE", Text: "a synthetic message hours later"},
	}, false)
	s.SetLatest(avOther, []slack.Message{{Type: "message", TS: avBobOnly, User: "UBOB", Text: "nobody else here"}}, false)
	h := coreHost{app: app, th: theme.New(false)}
	v := NewChannelView(h, avCh)
	v.Preview = true
	o := NewChannelView(h, avOther)
	o.Preview = true
	return v, frameFn(v), o, frameFn(o), h
}

// alicesRows is what the last frame drew of UALICE's two rows: their
// picture and name by ts (both are their group's first: each draws its
// sender).
func alicesRows(v *ChannelView) map[string][2]string {
	out := map[string][2]string{}
	for _, sv := range v.Senders() {
		if sv.User == "UALICE" {
			out[sv.TS] = [2]string{sv.Avatar, sv.Name}
		}
	}
	return out
}

func statusOf(v *ChannelView, ts string) string {
	if r := v.s.rows[ts]; r != nil {
		return r.statusURL
	}
	return ""
}

// aliceChanges is the socket's user_change: a new picture, display name
// and status emoji.
func aliceChanges(t *testing.T, h coreHost) {
	t.Helper()
	u, ok := h.app.S.RawUser("UALICE")
	if !ok {
		t.Fatal("no UALICE in the store")
	}
	nu := *u
	nu.Profile.Image24, nu.Profile.Image32, nu.Profile.Image48, nu.Profile.Image72 = avNewPic, avNewPic, avNewPic, avNewPic
	nu.Profile.DisplayName = avNewName
	nu.Profile.StatusEmoji = avNewStat
	nu.Updated = time.Now().Unix()
	raw, _ := json.Marshal(map[string]any{"type": "user_change", "user": nu, "event_ts": "1700150000.000100"})
	h.app.Inject(slack.Event{Type: "user_change", Raw: raw})
}

// A member's new picture, name and status, through user_change while
// their rows are on screen and while no list draws them: both rows, the
// broadcast and the plain message, draw the member as they are now, the
// same picture on both.
func TestSenderFollowsMemberLikeSlack(t *testing.T) {
	for _, offScreen := range []bool{false, true} {
		name := map[bool]string{false: "on screen", true: "while another list is drawn"}[offScreen]
		t.Run(name, func(t *testing.T) {
			v, f, otherList, other, h := avatarEnv(t)
			old, _ := h.app.User("UALICE")
			until(t, 5*time.Second, "UALICE's two rows", f, func() bool {
				rs := alicesRows(v)
				return len(rs) == 2 && rs[avBcast][0] != "" && rs[avPlain][0] != ""
			})
			rs := alicesRows(v)
			if rs[avBcast] != rs[avPlain] || rs[avBcast][0] != old.Avatar {
				t.Fatalf("before the change the rows draw %v; Slack's draws the member's %q on both", rs, old.Avatar)
			}
			if offScreen {
				// the window shows another conversation for a while: no
				// list draws UALICE when the change comes
				for i := 0; i < 6; i++ {
					other()
					time.Sleep(20 * time.Millisecond)
				}
				// the case's precondition: the hub counts UALICE as drawn
				// by no list now
				if v.s.hb.drawsMember(core.Event{Type: "user", UserID: "UALICE"}) {
					t.Fatal("UALICE still counts as drawn: the case tests nothing")
				}
			}
			aliceChanges(t, h)
			if offScreen {
				// a message in the other conversation after the change,
				// and only that list drawn until it shows: the hub (which
				// takes core's events in order, off this goroutine) took
				// the change while no list drew UALICE
				injectEvent(h.app, avOther, "message", "", map[string]any{"user": "UBOB", "ts": avMarker, "text": "after the change"})
				until(t, 5*time.Second, "the marker in the other list", other, func() bool { return otherList.s.rows[avMarker] != nil })
				if v.s.hb.drawsMember(core.Event{Type: "user", UserID: "UALICE"}) {
					t.Fatal("UALICE counted as drawn when the change came: the case tests nothing")
				}
			}
			now, _ := h.app.User("UALICE")
			if now.Avatar == old.Avatar || now.Avatar == "" {
				t.Fatalf("core's member has %q after user_change, the picture before it (%q)", now.Avatar, old.Avatar)
			}
			if offScreen {
				for i := 0; i < 3; i++ {
					other()
				}
			}
			for i := 0; i < 10; i++ {
				f()
				time.Sleep(10 * time.Millisecond)
			}
			rs = alicesRows(v)
			for _, ts := range []string{avBcast, avPlain} {
				what := map[string]string{avBcast: "the thread broadcast", avPlain: "the plain message"}[ts]
				if rs[ts][0] != now.Avatar {
					t.Errorf("%s draws %q; Slack's draws the member's picture now, %q", what, rs[ts][0], now.Avatar)
				}
				if rs[ts][1] != now.Name {
					t.Errorf("%s is named %q; the member is %q now", what, rs[ts][1], now.Name)
				}
				if got := statusOf(v, ts); got != now.StatusEmojiURL {
					t.Errorf("%s's status emoji %q, want the member's %q", what, got, now.StatusEmojiURL)
				}
			}
			if rs[avBcast] != rs[avPlain] {
				t.Errorf("one sender, two pictures: %v", rs)
			}
		})
	}
}
