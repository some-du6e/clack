package messages

// Karim's 11-17 report (α.102, macOS): a vote app's Block Kit message, the
// root of a thread open in the thread pane, "gets stripped and pushed
// down" as votes come in. The app updates it with chat.update after each
// vote; his plugins patch the thread root (ThreadRootGeneric), the
// channel's rows (MessageWrapper), the body (Blocks, MessageText) and the
// messages slice (Message Logger's copy of every message). A patched
// component hands its msg back in each tree JS delivers, a map parsed
// afresh, so the row took every new tree's msg for a plugin's override and
// drew it over the unpatched source: the plugins' copy of the message was
// gone until the row read its view again, up to 250 ms later, and an
// update's first frames drew the old tree's msg, the revision before, over
// it. Slack renders the store's patched message in one commit: no frame
// shows the message without its patches, and its height only changes with
// what it says. What a tree's msg says is now matched against what the row
// sent each level of its components, by content: however many revisions a
// tree is behind (votes, reactions and replies during a JS pause), it
// changes nothing the message says. Synthetic data on internal/fakeslack.

import (
	"encoding/json"
	"fmt"
	"image"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const voteLog = "logged by the test"

// rootCase is one way Karim's vote message is drawn and patched.
type rootCase struct {
	sender   bool // the wrapper patches change the sender, as ShowRealUser does a relay bot's
	reply    bool // the vote message is a reply in the thread (ThreadReplyGeneric patched as Message Logger does), not its root
	textOnly bool // no Block Kit blocks: text only, and the slice patch changes the text
	held     bool // the votes come in a burst with reactions and replies while JS is held
	rewrite  bool // MessageText's patch rewrites what the message says (BetterBlock's filter): "Upvotes:" becomes "Ups:"
	noBody   bool // Blocks and MessageText unpatched: the body draws the row's message itself
}

// rootPatchJS patches the thread root and the channel's rows (a 14px line
// above the native, so a frame drawing the native shows by its height,
// and with sender the person ShowRealUser puts for a relay bot), Blocks
// and MessageText as ShowRealUser and Message Logger do (a new props
// object, the same msg), and the vote message in the messages slice, as
// Message Logger keeps its own copy: a context line under its blocks, or
// its text with the line after it.
func rootPatchJS(c rootCase) []byte {
	msg := "props.msg"
	if c.sender {
		msg = "{ ...props.msg, user: 'UANA00001', bot_id: undefined, username: undefined, subtype: undefined, bot_profile: undefined }"
	}
	logged := `{ ...m, blocks: [...(m.blocks || []), { type: 'context', block_id: 'log', elements: [{ type: 'mrkdwn', text: '` + voteLog + `' }] }] }`
	if c.textOnly {
		logged = `{ ...m, text: m.text + ' (` + voteLog + `)' }`
	}
	// the reply as Message Logger draws it (renderRow: a memo of props.msg)
	reply := ""
	if c.reply {
		reply = `this.api.patchComponent('ThreadReplyGeneric', (Original) => (props) => {
      const msg = React.useMemo(() => props.msg, [props.msg])
      return React.createElement('div', null, React.createElement('div', { style: { height: '14px' } }, 'patched reply'),
        React.createElement(Original, { ...props, msg }))
    })`
	}
	body := `const pass = (Original) => (props) => React.createElement(Original, { ...props, msg: props.msg })
    this.api.patchComponent('Blocks', pass)
    this.api.patchComponent('MessageText', pass)`
	if c.rewrite {
		// BetterBlock's shape: MessageText hands Original a msg it rewrote
		body = `const pass = (Original) => (props) => React.createElement(Original, { ...props, msg: props.msg })
    this.api.patchComponent('Blocks', pass)
    const fix = (t) => (typeof t === 'string' ? t.replace(/Upvotes:/g, 'Ups:') : t)
    this.api.patchComponent('MessageText', (Original) => (props) => {
      const m = props.msg
      const blocks = (m.blocks || []).map((b) => (b.type === 'section' && b.text ? { ...b, text: { ...b.text, text: fix(b.text.text) } } : b))
      return React.createElement(Original, { ...props, msg: { ...m, text: fix(m.text), blocks } })
    })`
	}
	if c.noBody {
		body = ""
	}
	return pluginJS(`
class PatchedRoot extends TautPlugin {
  static id = 'PatchedRoot'
  static pluginName = 'Patched root (test)'
  static description = 'test: a line above the thread root and the rows, the body passed through, a log line on the vote message'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    const line = (label) => (Original) => (props) => React.createElement('div', null,
      React.createElement('div', { style: { height: '14px' } }, label),
      React.createElement(Original, { ...props, msg: ` + msg + ` }))
    this.api.patchComponent('ThreadRootGeneric', line('patched root'))
    this.api.patchComponent('MessageWrapper', line('patched row'))
    ` + reply + `
    ` + body + `
    const log = (ts, m) => (m && m.bot_id === '` + voteBotID + `' ? ` + logged + ` : m)
    const { patchSlice, mapEntries } = this.api.redux
    patchSlice('messages', (ch, bucket) => (ch === '` + voteChan + `' && bucket && typeof bucket === 'object' ? mapEntries(bucket, log) : bucket))
  }
}
return PatchedRoot`)
}

// voteRoot is the vote message at revision n (n upvotes) as the root of
// a thread with replies.
func voteRoot(ts string, n int, replies []string) slack.Message {
	m := voteMsg(ts, ts, n, n, 0)
	m.ReplyCount, m.ReplyUsers, m.LatestReply = len(replies), []string{"UANA00001", "UBEN00001"}, replies[len(replies)-1]
	return m
}

// voteText is the vote message at revision n with no blocks.
func voteText(m slack.Message) slack.Message {
	m.Blocks = nil
	return m
}

// injectEvent sends app a realtime event in ch as the socket brings it
// (its type, subtype, channel and event_ts set here).
func injectEvent(app *core.App, ch, typ, subtype string, body map[string]any) {
	body["type"] = typ
	if subtype != "" {
		body["subtype"] = subtype
	}
	body["channel"] = ch
	body["event_ts"] = fmt.Sprintf("%d.%06d", time.Now().Unix(), time.Now().Nanosecond()/1000)
	raw, _ := json.Marshal(body)
	app.Inject(slack.Event{Type: typ, Subtype: subtype, Channel: json.RawMessage(`"` + ch + `"`), Raw: raw})
}

// voteInject is injectEvent in the vote channel.
func voteInject(app *core.App, typ, subtype string, body map[string]any) {
	injectEvent(app, voteChan, typ, subtype, body)
}

func TestPatchedRootSurvivesUpdates(t *testing.T) {
	cases := []struct {
		name string
		c    rootCase
	}{
		{"root", rootCase{}},
		{"root, the sender a plugin's", rootCase{sender: true}},
		{"root, text only", rootCase{textOnly: true}},
		{"thread reply", rootCase{reply: true}},
		{"root, MessageText rewrites what it says", rootCase{rewrite: true}},
	}
	for _, x := range cases {
		t.Run(x.name+", free-running", func(t *testing.T) { checkPatchedRoot(t, x.c, false) })
		// lockstep: JS answers every render before the next frame
		t.Run(x.name+", lockstep", func(t *testing.T) { checkPatchedRoot(t, x.c, true) })
	}
	// votes, reactions and replies, all while JS is held (a QuickJS
	// collection: up to a second on Karim's plugins), so every tree on
	// screen is many revisions behind when JS lets go
	t.Run("root, a burst while JS is held", func(t *testing.T) { checkPatchedRoot(t, rootCase{held: true}, false) })
	// the same with the body unpatched: what the row's message says is
	// drawn as it is, so a revision going back shows in the text
	t.Run("root, a burst while JS is held, the body unpatched", func(t *testing.T) { checkPatchedRoot(t, rootCase{held: true, noBody: true}, false) })
}

func checkPatchedRoot(t *testing.T, c rootCase, lockstep bool) {
	app, _, _ := shotsApp(t)
	app.S.PutConv(slack.Conversation{ID: voteChan, Name: "meta", IsChannel: true, IsMember: true, NumMembers: 40})
	app.S.PutBot(voteBotID, store.BotInfo{Name: "Vote Bot", Icon: shotAvatar(voteBotID)})
	base := time.Now().Add(-time.Hour).Unix()
	rootTS := fmt.Sprintf("%d.000100", base)
	r1, r2 := fmt.Sprintf("%d.000200", base+60), fmt.Sprintf("%d.000300", base+120)
	replies := []string{r1, r2}
	// the vote message: the root, or the thread's last reply
	voteTS := rootTS
	if c.reply {
		voteTS = r2
	}
	replyCount := len(replies) // the root's, as replies come (the held burst)
	vote := func(n int) slack.Message {
		var m slack.Message
		if c.reply {
			m = voteMsg(r2, rootTS, n, n, 0)
		} else {
			m = voteRoot(rootTS, n, replies)
			m.ReplyCount = replyCount
		}
		if c.textOnly {
			m = voteText(m)
		}
		// every revision edited, the first too: a plugin that makes the
		// bot's message a person's (sender) shows "(edited)" on it, as
		// Slack does, and each revision draws the same
		m.Edited = &slack.Edited{User: voteBotID, TS: fmt.Sprintf("%d.%06d", time.Now().Unix(), n)}
		return m
	}
	root := slack.Message{Type: "message", TS: rootTS, User: "UBEN00001", Text: "Meta vote: move standup?", Blocks: shotRich(shotSection("Meta vote: move standup?"))}
	root.ThreadTS, root.ReplyCount, root.ReplyUsers, root.LatestReply = rootTS, 2, []string{"UANA00001", "UBEN00001"}, r2
	if !c.reply {
		root = vote(1)
	}
	thread := []slack.Message{root,
		{Type: "message", TS: r1, ThreadTS: rootTS, User: "UANA00001", Text: "voted", Blocks: shotRich(shotSection("voted"))}}
	if c.reply {
		thread = append(thread, vote(1))
	} else {
		thread = append(thread, slack.Message{Type: "message", TS: r2, ThreadTS: rootTS, User: "UBEN00001", Text: "me too", Blocks: shotRich(shotSection("me too"))})
	}
	app.S.SetLatest(voteChan, []slack.Message{root}, false)
	app.S.SetThread(voteChan, rootTS, thread)

	patched := []string{"ThreadRootGeneric", "MessageWrapper"}
	if !c.noBody {
		patched = append(patched, "Blocks", "MessageText")
	}
	if c.reply {
		patched = append(patched, "ThreadReplyGeneric")
	}
	renders, step := patchedEnv(t, app, rootPatchJS(c), patched...)
	if !lockstep {
		step = nil
	}
	until(t, 5*time.Second, "the messages slice patched", func() {}, func() bool { return tautOf().SlicePatched("messages") })
	h := coreHost{app: app, th: theme.New(false)}
	tv := NewThreadView(h, voteChan, rootTS)
	now := time.Now()
	panes := []*votePane{{name: "thread", s: tv.s, v: tv, size: image.Pt(520, 700), now: &now, step: step}}
	paneIDs := []string{tv.PaneID()}
	// the trees each pane's row must have from JS: its own component's and
	// the body's
	bodyComps := []string{"Blocks", "MessageText"}
	if c.noBody {
		bodyComps = nil
	}
	comps := [][]string{append([]string{"ThreadRootGeneric"}, bodyComps...)}
	if c.reply {
		comps = [][]string{append([]string{"ThreadReplyGeneric"}, bodyComps...)}
	} else {
		cv := NewChannelView(h, voteChan)
		cv.Preview = true
		panes = append(panes, &votePane{name: "channel", s: cv.s, v: cv, size: image.Pt(1100, 700), now: &now, step: step})
		paneIDs, comps = append(paneIDs, cv.PaneID()), append(comps, append([]string{"MessageWrapper"}, bodyComps...))
	}
	// what the revision reads as, drawn: the vote count after this label
	label := "Upvotes: "
	if c.rewrite {
		label = "Ups: "
	}
	// the sender labels drawn (labelHook), to tell the plugin's person
	// from the bot by what the header draws
	var labMu sync.Mutex
	var labels []string
	labelHook = func(txt string, _ textStyle) { labMu.Lock(); labels = append(labels, txt); labMu.Unlock() }
	t.Cleanup(func() { labelHook = nil })
	// each pane's frame, with the labels it drew (paneLabels)
	paneLabels := make([][]string, len(panes))
	all := func() {
		for i, p := range panes {
			labMu.Lock()
			labels = nil
			labMu.Unlock()
			p.frame()
			labMu.Lock()
			paneLabels[i] = labels
			labels = nil
			labMu.Unlock()
		}
	}
	// what a pane draws of the vote message: its height; the revision and
	// the plugins' copy (the log line), as drawn (the body's text) and as
	// the row's model; the sender; and whether a patched component's
	// native is drawn with no tree from JS (the unpatched component)
	type look struct {
		h, rev, modelRev int
		logged, mlogged  bool
		trees            string
		native           bool
		viewed           bool
		user, body, name string
	}
	lookAt := func(i int) look {
		p := panes[i]
		l := look{h: itemHeight(p.s, voteTS), trees: slackc.KeyedTreesIn(voteTS, paneIDs[i])}
		l.native = strings.Contains(l.trees, "no tree yet")
		if r := p.s.rows[voteTS]; r != nil {
			l.body = rowText(r)
			l.logged = strings.Contains(l.body, voteLog)
			l.mlogged = strings.Contains(string(r.m.Blocks), voteLog) || strings.Contains(r.m.Text, voteLog)
			l.viewed, l.user = r.viewed, r.m.User
			if i := strings.Index(l.body, label); i >= 0 {
				fmt.Sscanf(l.body[i+len(label):], "%d", &l.rev)
			}
			fmt.Sscanf(r.m.Text, "Upvotes: %d", &l.modelRev)
			l.name = r.name
		}
		return l
	}
	wantUser, wantName := "", ""
	if c.sender {
		wantUser = "UANA00001"
		if u, ok := app.User(wantUser); ok {
			wantName = u.Name
		}
	}
	settled := func(n int) bool {
		for i := range panes {
			l := lookAt(i)
			if l.rev != n || l.modelRev != n || !l.logged || !l.mlogged || l.native || l.h <= 0 || wantUser != "" && (l.user != wantUser || l.name != wantName) {
				return false
			}
			for _, comp := range comps[i] {
				if !strings.Contains(l.trees, comp+": ") {
					return false
				}
			}
		}
		return true
	}
	until(t, 10*time.Second, "the vote message drawn patched", all, func() bool { return settled(1) })
	for i := 0; i < 30; i++ {
		all()
		time.Sleep(10 * time.Millisecond)
	}
	if !settled(1) {
		for i, p := range panes {
			t.Logf("%s: %+v", p.name, lookAt(i))
		}
		t.Fatal("the vote message didn't stay patched")
	}
	h0 := make([]int, len(panes))
	for i := range panes {
		h0[i] = lookAt(i).h
	}

	// every frame from the first update on: never the message without the
	// plugins' copy (drawn or in the model), never a patched component's
	// native, the same height (each revision says as much), never an older
	// revision than one drawn before, and the plugin's sender throughout
	frames := 0
	held := false                // JS held (the burst): what's drawn may be the old revision then
	bad := map[string][]string{} // what went wrong, by pane, with the frames
	seen := make([]int, len(panes))
	for i := range seen {
		seen[i] = 1
	}
	check := func(when string) {
		frames++
		for i, p := range panes {
			labMu.Lock()
			drawnLabels := paneLabels[i]
			labMu.Unlock()
			l := lookAt(i)
			var why []string
			if c.rewrite && strings.Contains(l.body, "Upvotes:") {
				why = append(why, fmt.Sprintf("drew %q without MessageText's rewrite", l.body))
			}
			if c.sender && slices.Contains(drawnLabels, "Vote Bot") {
				why = append(why, fmt.Sprintf("drew the bot's name, not the plugin's person (the row's %q, %q; labels %q)", l.name, l.user, drawnLabels))
			}
			if c.held && !held && l.rev != l.modelRev {
				why = append(why, fmt.Sprintf("drew revision %d, the row's message is %d", l.rev, l.modelRev))
			}
			if !l.logged {
				why = append(why, fmt.Sprintf("drew %q without the plugins' copy of it (viewed %v)", l.body, l.viewed))
			}
			if !l.mlogged {
				why = append(why, "the row's message isn't the plugins' copy")
			}
			if l.native {
				why = append(why, "drew a patched component's native: "+l.trees)
			}
			// (the held burst's reactions add their bar when JS lets go)
			if l.h < h0[i] || !c.held && l.h != h0[i] {
				why = append(why, fmt.Sprintf("%dpx tall, %dpx patched", l.h, h0[i]))
			}
			if l.rev < seen[i] {
				why = append(why, fmt.Sprintf("drew revision %d after %d", l.rev, seen[i]))
			}
			if wantUser != "" && (l.user != wantUser || l.name != wantName) {
				why = append(why, fmt.Sprintf("the sender %q (%q), not the plugin's %q (%q)", l.user, l.name, wantUser, wantName))
			}
			seen[i] = max(seen[i], l.rev)
			if len(why) > 0 && len(bad[p.name]) < 12 {
				bad[p.name] = append(bad[p.name], fmt.Sprintf("frame %d (%s): %s", frames, when, strings.Join(why, "; ")))
			}
		}
	}
	frame := func(when string) {
		all()
		check(when)
		time.Sleep(10 * time.Millisecond)
	}
	update := func(n int) { voteChanged(app, vote(n)) }
	react := func(name, user string) {
		voteInject(app, "reaction_added", "", map[string]any{"user": user, "reaction": name,
			"item": map[string]any{"type": "message", "channel": voteChan, "ts": voteTS}})
	}
	n := 1
	if c.held {
		// JS held: votes, each followed by others' reactions (no change to
		// what the message says, a revision each), with frames between
		rt, err := tautOf().JS()
		if err != nil {
			t.Fatal(err)
		}
		release := make(chan struct{})
		rt.Post(func(*jsrt.Ctx) { <-release })
		held = true
		users := []string{"UANA00001", "UBEN00001", "UCAR00001"}
		for v := 0; v < 5; v++ {
			n++
			update(n)
			frame(fmt.Sprintf("JS held, revision %d", n))
			for k, u := range users {
				react([]string{"eyes", "tada", "fire"}[k], u)
				frame(fmt.Sprintf("JS held, revision %d, a reaction", n))
			}
			if !c.reply {
				// and a reply: the root's count moves, what it says doesn't
				replyCount++
				newTS := fmt.Sprintf("%d.%06d", time.Now().Unix(), 500+v)
				voteInject(app, "message", "message_replied", map[string]any{"hidden": true, "ts": newTS, "message": map[string]any{
					"type": "message", "subtype": "bot_message", "bot_id": voteBotID, "ts": rootTS, "thread_ts": rootTS,
					"reply_count": replyCount, "latest_reply": newTS, "reply_users": []string{"UANA00001", "UBEN00001"}}})
				frame(fmt.Sprintf("JS held, revision %d, a reply", n))
			}
		}
		for k := 0; k < 10; k++ {
			frame(fmt.Sprintf("JS held after revision %d", n))
		}
		close(release)
		held = false
	} else {
		// votes in quick succession, some while JS is still answering the
		// one before, then one after the views settled
		for _, g := range []int{3, 1, 6, 2, 12} {
			n++
			update(n)
			for k := 0; k < g; k++ {
				frame(fmt.Sprintf("%d frames after revision %d", k+1, n))
			}
		}
	}
	// the last revision lands in every pane, patched
	for dl := time.Now().Add(10 * time.Second); !settled(n); {
		if time.Now().After(dl) {
			for i, p := range panes {
				t.Logf("%s: %+v", p.name, lookAt(i))
			}
			t.Fatalf("revision %d never settled", n)
		}
		frame(fmt.Sprintf("waiting for revision %d", n))
	}
	for _, p := range panes {
		for _, b := range bad[p.name] {
			t.Errorf("%s: %s", p.name, b)
		}
	}

	// and stays put: no render sent, the same height, frame after frame
	mark := renders.mark()
	hs := make([]int, len(panes))
	for i := range panes {
		hs[i] = lookAt(i).h
	}
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
		if r.ts == voteTS {
			t.Errorf("settled, Go sent JS a render of %s (revision %q)", r.name, r.text)
		}
	}
	for j, p := range panes {
		if !c.held && hs[j] != h0[j] {
			t.Errorf("%s: the vote message settled at %dpx, %dpx before the votes; each revision says as much", p.name, hs[j], h0[j])
		}
	}
}
