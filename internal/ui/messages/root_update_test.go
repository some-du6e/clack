package messages

// Karim's 11-17 report (α.102, macOS): a vote app's Block Kit message, the
// root of a thread open in the thread pane, "gets stripped and pushed
// down" as votes come in. The app updates it with chat.update after each
// vote; his plugins patch the thread root (ThreadRootGeneric), the
// channel's rows (MessageWrapper) and the messages slice (Message Logger's
// copy of every message). A patched component hands its msg back in each
// tree JS delivers, a map parsed afresh, so the row took every new tree's
// msg for a plugin's override and drew it over the unpatched source: the
// plugins' copy of the message (the slice patch) was gone until the row
// read its view again, up to 250 ms later, and an update's first frames
// drew the old tree's msg, the revision before, over it. Slack renders the
// store's patched message in one commit: no frame shows the message
// without its patches, and its height only changes with what it says.
// Synthetic data on internal/fakeslack.

import (
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// rootPatchJS patches the thread root and the channel's rows (a 14px line
// above the native, so a frame drawing the native shows by its height) and
// adds a context block under the vote message in the messages slice, as
// Message Logger keeps its own copy of each message.
var rootPatchJS = pluginJS(`
class PatchedRoot extends TautPlugin {
  static id = 'PatchedRoot'
  static pluginName = 'Patched root (test)'
  static description = 'test: a line above the thread root and the rows, a log line under the vote message'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    const line = (label) => (Original) => (props) => React.createElement('div', null,
      React.createElement('div', { style: { height: '14px' } }, label),
      React.createElement(Original, props))
    this.api.patchComponent('ThreadRootGeneric', line('patched root'))
    this.api.patchComponent('MessageWrapper', line('patched row'))
    const log = (ts, m) => (m && m.bot_id === '` + voteBotID + `' ? { ...m, blocks: [...(m.blocks || []),
      { type: 'context', block_id: 'log', elements: [{ type: 'mrkdwn', text: 'logged by the test' }] }] } : m)
    const { patchSlice, mapEntries } = this.api.redux
    patchSlice('messages', (ch, bucket) => (ch === '` + voteChan + `' && bucket && typeof bucket === 'object' ? mapEntries(bucket, log) : bucket))
  }
}
return PatchedRoot`)

// voteRoot is the vote message at revision n (n upvotes) as the root of
// a thread with two replies.
func voteRoot(ts string, n int, replies []string) slack.Message {
	m := voteMsg(ts, ts, n, n, 0)
	m.ReplyCount, m.ReplyUsers, m.LatestReply = len(replies), []string{"UANA00001", "UBEN00001"}, replies[len(replies)-1]
	return m
}

func TestPatchedRootSurvivesUpdates(t *testing.T) {
	t.Run("free-running", func(t *testing.T) { checkPatchedRoot(t, false) })
	// lockstep: JS answers every render before the next frame
	t.Run("lockstep", func(t *testing.T) { checkPatchedRoot(t, true) })
}

func checkPatchedRoot(t *testing.T, lockstep bool) {
	app, _, _ := shotsApp(t)
	app.S.PutConv(slack.Conversation{ID: voteChan, Name: "meta", IsChannel: true, IsMember: true, NumMembers: 40})
	app.S.PutBot(voteBotID, store.BotInfo{Name: "Vote Bot", Icon: shotAvatar(voteBotID)})
	base := time.Now().Add(-time.Hour).Unix()
	rootTS := fmt.Sprintf("%d.000100", base)
	r1, r2 := fmt.Sprintf("%d.000200", base+60), fmt.Sprintf("%d.000300", base+120)
	replies := []slack.Message{
		{Type: "message", TS: r1, ThreadTS: rootTS, User: "UANA00001", Text: "voted", Blocks: shotRich(shotSection("voted"))},
		{Type: "message", TS: r2, ThreadTS: rootTS, User: "UBEN00001", Text: "me too", Blocks: shotRich(shotSection("me too"))},
	}
	root := voteRoot(rootTS, 1, []string{r1, r2})
	app.S.SetLatest(voteChan, []slack.Message{root}, false)
	app.S.SetThread(voteChan, rootTS, append([]slack.Message{root}, replies...))

	renders, step := patchedEnv(t, app, rootPatchJS, "ThreadRootGeneric", "MessageWrapper")
	if !lockstep {
		step = nil
	}
	until(t, 5*time.Second, "the messages slice patched", func() {}, func() bool { return tautOf().SlicePatched("messages") })
	h := coreHost{app: app, th: theme.New(false)}
	cv := NewChannelView(h, voteChan)
	cv.Preview = true
	tv := NewThreadView(h, voteChan, rootTS)
	now := time.Now()
	panes := []*votePane{
		{name: "thread", s: tv.s, v: tv, size: image.Pt(520, 700), now: &now, step: step},
		{name: "channel", s: cv.s, v: cv, size: image.Pt(1100, 700), now: &now, step: step},
	}
	paneIDs := []string{tv.PaneID(), cv.PaneID()}
	comps := []string{"ThreadRootGeneric", "MessageWrapper"}
	all := func() {
		for _, p := range panes {
			p.frame()
		}
	}
	// what a pane draws of the root: its height, the revision (its
	// upvotes), whether the plugins' copy of it (the log line) is what the
	// row draws, and whether a patched component's native is drawn with no
	// tree from JS (the unpatched component)
	type look struct {
		h, rev     int
		logged     bool
		trees      string
		native     bool
		viewed     bool
		text, body string
	}
	lookAt := func(i int) look {
		p := panes[i]
		l := look{h: itemHeight(p.s, rootTS), trees: slackc.KeyedTreesIn(rootTS, paneIDs[i])}
		l.native = strings.Contains(l.trees, "no tree yet")
		if r := p.s.rows[rootTS]; r != nil {
			l.logged = strings.Contains(string(r.m.Blocks), "logged by the test")
			l.viewed, l.text = r.viewed, r.m.Text
			fmt.Sscanf(r.m.Text, "Upvotes: %d", &l.rev)
			l.body = rowText(r)
		}
		return l
	}
	settled := func(n int) bool {
		for i := range panes {
			l := lookAt(i)
			if l.rev != n || !l.logged || l.native || !strings.Contains(l.trees, comps[i]+": ") || l.h <= 0 {
				return false
			}
		}
		return true
	}
	until(t, 10*time.Second, "the root drawn patched in both panes", all, func() bool { return settled(1) })
	for i := 0; i < 30; i++ {
		all()
		time.Sleep(10 * time.Millisecond)
	}
	if !settled(1) {
		t.Fatalf("the root didn't stay patched: thread %+v, channel %+v", lookAt(0), lookAt(1))
	}
	h0 := []int{lookAt(0).h, lookAt(1).h}

	// every frame from the first update on: never the unpatched root, never
	// shorter than the patched root (each revision says as much), never an
	// older revision than one drawn before
	frames := 0
	bad := map[string][]string{} // what went wrong, by pane, with the frames
	seen := []int{1, 1}
	check := func(when string) {
		frames++
		for i, p := range panes {
			l := lookAt(i)
			var why []string
			if !l.logged {
				why = append(why, fmt.Sprintf("drew %q without the plugins' copy of it (viewed %v)", l.text, l.viewed))
			}
			if l.native {
				why = append(why, "drew a patched component's native: "+l.trees)
			}
			if l.h < h0[i] {
				why = append(why, fmt.Sprintf("%dpx tall, %dpx patched", l.h, h0[i]))
			}
			if l.rev < seen[i] {
				why = append(why, fmt.Sprintf("back at revision %d after %d", l.rev, seen[i]))
			}
			seen[i] = max(seen[i], l.rev)
			if len(why) > 0 && len(bad[p.name]) < 12 {
				bad[p.name] = append(bad[p.name], fmt.Sprintf("frame %d (%s): %s", frames, when, strings.Join(why, "; ")))
			}
		}
	}
	update := func(n int) {
		voteChanged(app, voteRoot(rootTS, n, []string{r1, r2}))
	}
	// votes in quick succession, some while JS is still answering the one
	// before, then one after the views settled
	gaps := []int{3, 1, 6, 2, 12}
	n := 1
	for _, g := range gaps {
		n++
		update(n)
		for k := 0; k < g; k++ {
			all()
			check(fmt.Sprintf("%d frames after revision %d", k+1, n))
			time.Sleep(10 * time.Millisecond)
		}
	}
	// the last revision lands in both panes, patched
	for dl := time.Now().Add(10 * time.Second); !settled(n); {
		if time.Now().After(dl) {
			t.Fatalf("revision %d never settled: thread %+v, channel %+v", n, lookAt(0), lookAt(1))
		}
		all()
		check(fmt.Sprintf("waiting for revision %d", n))
		time.Sleep(10 * time.Millisecond)
	}
	for _, p := range panes {
		for _, b := range bad[p.name] {
			t.Errorf("%s: %s", p.name, b)
		}
	}

	// and stays put: no render sent, the same height, frame after frame
	mark := renders.mark()
	hs := []int{lookAt(0).h, lookAt(1).h}
	for i := 0; i < 40; i++ {
		all()
		for j, p := range panes {
			if l := lookAt(j); l.h != hs[j] || !l.logged || l.rev != n {
				t.Fatalf("%s, settled frame %d: %+v; want revision %d at %dpx, patched", p.name, i, l, n, hs[j])
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, r := range renders.since(mark) {
		if r.ts == rootTS {
			t.Errorf("settled, Go sent JS a render of %s (revision %q)", r.name, r.text)
		}
	}
	for j, p := range panes {
		if hs[j] != h0[j] {
			t.Errorf("%s: the root settled at %dpx, %dpx before the votes; each revision says as much", p.name, hs[j], h0[j])
		}
	}
}
