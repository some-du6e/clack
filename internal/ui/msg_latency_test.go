package ui

// Socket event to row on screen (msg-latency). Karim on α.102 (Windows,
// Hack Club's Enterprise Grid, a dozen messages-patching plugins): "my
// messages are being delayed, but like in the main message thingy it
// seems like the activity tab is updated", "Like a second and they come
// in clumped", with the realtime socket connected the whole session. Slack
// draws a message in the frame after it arrives: its socket handler puts
// it into the store at once (boot-render messageReceived) and React
// commits it in that task; your own message's pending copy goes in as
// you press Enter (sendMessage's addMsgToEnd).
//
// The client runs whole against fakeslack with its realtime socket on,
// and draws frames the way App.Run's window does (only when something
// asks: an Invalidate, a wake-up a frame asked for, pending input; at
// most one per vsync; quiet frames as quiet.go makes them). Messages go
// out over the socket and own messages through the composer, and each
// one's latency is the time from its event (the socket frame leaving the
// fake, the Enter key) to the end of the first frame whose layout drew
// its row. users/info answers after latRTT, as a busy Grid's does from
// far away, and the senders, typers and mentioned people are often ones
// the client never fetched (fakeslack's synthetic members). The busy
// passes add a big workspace's socket meanwhile: messages from new people
// elsewhere, typing in the open channel, Activity entries, presence,
// profile changes, reactions.
//
// Synthetic data only.

import (
	"encoding/json"
	"fmt"
	"image"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/idleprobe"
	"github.com/some-du6e/clack/internal/ui/messages"
)

// latRTT is users/info's answer time in these tests (a Grid's edge API
// from far away, under load).
const latRTT = 250 * time.Millisecond

// Slack's budget: the row in the frame after the event, which is at
// most a vsync away plus a layout. The CI runners lay a frame out in a few
// ms (no GPU); the budgets leave room for a slow frame or a collection.
const (
	latMedianBudget = 3 * idleprobe.Vsync  // 50 ms
	latP90Budget    = 6 * idleprobe.Vsync  // 100 ms
	latMaxBudget    = 24 * idleprobe.Vsync // 400 ms
)

// latEnvOpts are a latency env's: Hack Club's enterprise id (the
// Library's Hack Club–only plugins run), the RTT.
type latEnvOpts struct {
	hackClub bool
	rtt      time.Duration
}

// newLatEnv is the whole client against its own fake, realtime on and
// connected, the window focused (TestIdleFrames' newIdleEnv), with
// users/info answering after o.rtt.
func newLatEnv(t *testing.T, o latEnvOpts) *perfEnv {
	t.Helper()
	sz := fakeslack.Small
	sz.Unread = 0
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	home := t.TempDir()
	t.Setenv("CLACK_HOME", home)
	e := &perfEnv{tb: t, fs: fs, home: home, size: image.Pt(1440, 900), now: time.Now(), start: time.Now()}
	images := imgcache.New(fs.Fetch, "", nil)
	e.a = New(Options{ConfigDir: home, Started: e.start}, images)
	on := true
	e.a.prefs.Realtime = &on
	e.a.focused = true // App.Run: the first ConfigEvent of a shown window
	cache := filepath.Join(home, "cache")
	creds := fs.Creds()
	if o.hackClub {
		creds.EnterpriseID = "E09V59WQY1E" // taut.HackClubIDs
	}
	svc := core.New(slack.New(creds), store.New(cache))
	e.a.Attach(svc, cache)
	images.SetInvalidate(e.a.Invalidate) // what Run does once the window exists
	e.a.th.Images = images
	if installHooks != nil {
		installHooks(e.a)
	}
	evs, stop := svc.Subscribe()
	e.a.data.start(e.a)
	t.Cleanup(e.shutdown)
	e.waitReady()
	dl := time.After(20 * time.Second)
	for up := false; !up; {
		select {
		case ev := <-evs:
			up = ev.Type == "connection" && ev.Connected
		case <-dl:
			t.Fatal("no realtime connection")
		}
	}
	stop()
	e.until("the socket", 15*time.Second, func() bool { return fs.Sockets() > 0 })
	if o.rtt > 0 {
		fs.SetFault("edge/users/info", fakeslack.Fault{Delay: o.rtt})
	}
	return e
}

// latWait is a row a pass waits for.
type latWait struct {
	kind   string // "others", "own" (the optimistic row), "echo" (the confirmed one)
	ts     string // the row's key (an own send's: found by text)
	text   string
	t0     time.Time // its event
	frame0 int       // frames laid out before it
	done   bool
	lat    time.Duration
	frames int // frames laid out from the event to the one that drew it
}

// latProbe draws the open channel's frames as the window does and times
// the rows it waits for.
type latProbe struct {
	e    *perfEnv
	t    *testing.T
	ch   string
	v    *messages.ChannelView
	wake chan struct{}
	next atomic.Int64 // synthetic members handed out

	mu     sync.Mutex
	waits  []*latWait
	frames int
	echoes map[string]string // own send text → its confirmed ts

	// names counts core's "names" events (a member lookup answered: the
	// sidebar resnapshots, the lists on screen reload, the plugins'
	// members are invalidated)
	names atomic.Int64
}

func newLatProbe(t *testing.T, e *perfEnv, ch string) *latProbe {
	p := &latProbe{e: e, t: t, ch: ch, wake: make(chan struct{}, 1), echoes: map[string]string{}}
	p.next.Store(1000)
	evs, stop := e.a.data.core.Subscribe()
	t.Cleanup(stop)
	go func() {
		for ev := range evs {
			if ev.Type == "names" {
				p.names.Add(1)
			}
		}
	}()
	prev := invalidateHook
	invalidateHook = func(a *App) {
		if prev != nil {
			prev(a)
		}
		if a == e.a {
			select {
			case p.wake <- struct{}{}:
			default:
			}
		}
	}
	t.Cleanup(func() { invalidateHook = prev })
	// the server's echo of an own send, over the socket before its
	// chat.postMessage reply, as Slack's gateway usually brings it
	e.fs.OnPost = func(f url.Values, msg map[string]any) {
		if f.Get("channel") != ch {
			return
		}
		ev := map[string]any{"type": "message", "channel": ch, "event_ts": msg["ts"]}
		for k, v := range msg {
			ev[k] = v
		}
		text, _ := msg["text"].(string)
		ts, _ := msg["ts"].(string)
		p.mu.Lock()
		p.echoes[text] = ts
		p.waits = append(p.waits, &latWait{kind: "echo", ts: ts, text: text, t0: time.Now(), frame0: p.frames})
		p.mu.Unlock()
		e.fs.Push(ev)
	}
	t.Cleanup(func() { e.fs.OnPost = nil })
	e.openChannel(ch)
	e.until("history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
	e.settle(800 * time.Millisecond)
	e.toLatest()
	p.v = e.ownSendList(ch)
	e.focusComposer(ch, "")
	// core's deferred boot loads (a.later 5 s after boot) aren't the
	// passes' traffic
	if d := 6*time.Second - time.Since(e.a.data.booted); d > 0 {
		e.settle(d)
	}
	return p
}

// latMember is the n-th of fakeslack's synthetic members (SynthMemberID:
// users/info answers any of them). Spelled out here so the perf job can
// run this file on a base tree whose fakeslack doesn't know them: there
// users/info answers without them, after the same round trip.
func latMember(n int) string { return fmt.Sprintf("U0SYNTH%06d", n) }

// member hands out a synthetic member the client has never seen.
func (p *latProbe) member() string { return latMember(int(p.next.Add(1))) }

// known hands out n members the client holds: each comes over the socket
// as a user_change, which every core stores without a lookup, so the
// perf job's base tree holds them too (its fakeslack may not answer
// users/info for synthetic members, and a sender it doesn't hold would
// time the base's lookups instead).
func (p *latProbe) known(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = p.member()
		name := strings.ToLower(ids[i])
		p.e.fs.Push(map[string]any{"type": "user_change", "user": map[string]any{"id": ids[i], "team_id": fakeslack.TeamID, "name": name,
			"real_name": "Synthetic " + ids[i], "profile": map[string]any{"display_name": name, "real_name": "Synthetic " + ids[i]}}})
	}
	held := func() bool {
		for _, id := range ids {
			if _, ok := p.e.a.data.core.S.RawUser(id); !ok {
				return false
			}
		}
		return true
	}
	if !p.e.untilSoft(10*time.Second, held) {
		p.t.Fatal("the senders the pass holds aren't held: their user_change never landed")
	}
	return ids
}

// post sends someone's message to the open channel over the socket (in
// the fake's history too, so a reload keeps it) and waits for its row.
func (p *latProbe) post(user, text string) {
	ts, ev := p.e.fs.PostQuiet(p.ch, user, text)
	p.mu.Lock()
	p.waits = append(p.waits, &latWait{kind: "others", ts: ts, text: text, t0: time.Now(), frame0: p.frames})
	p.mu.Unlock()
	p.e.fs.Push(ev)
}

// check notes the rows the frame just laid out drew (UI goroutine).
func (p *latProbe) check() {
	rows := p.v.VisibleRows()
	keys := make(map[string]bool, len(rows))
	for _, r := range rows {
		keys[r.Key] = true
	}
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.frames++
	for _, w := range p.waits {
		if w.done {
			continue
		}
		hit := false
		switch w.kind {
		case "own": // the optimistic row, or the confirmed one if it came first
			for k := range keys {
				if m, ok := p.e.a.data.core.S.Find(p.ch, k); ok && m.Text == w.text {
					hit = true
					break
				}
			}
		default:
			hit = keys[w.ts]
		}
		if hit {
			w.done, w.lat, w.frames = true, now.Sub(w.t0), p.frames-w.frame0
		}
	}
}

// latPass is one measured stretch.
type latPass struct {
	name    string
	others  int  // messages from others in the open channel
	newbies bool // from people the client never fetched (else ones it holds)
	own     int  // own sends
	busy    bool // a big workspace's socket meanwhile
	// before each own send, d of work queued on the plugin runtime's JS
	// thread (busyJS tasks), as when plugins re-render rows
	busyJS func(d time.Duration)
	// drainJS, before each Enter: the plugin runtime's JS thread idle (its
	// re-render of the patched composer after the typing done), so a quiet
	// own send with plugins is timed by its own JS work, not by work left
	// from before it
	drainJS func()
	// pluginSend: plugins check and wrap the send in JS (OwlMode's send
	// check and prepareAndSendMessage patch)
	pluginSend bool
}

// run draws frames as the window does while the pass's messages go out
// (others' every 300 ms from a goroutine, as the socket brings them; own
// sends from the frame loop, as input), and returns the rows' waits.
func (p *latProbe) run(ps latPass) []*latWait {
	e := p.e
	p.mu.Lock()
	p.waits = nil
	p.mu.Unlock()
	senders := []string{}
	if !ps.newbies {
		senders = p.known(4)
	}
	stop := make(chan struct{})
	var bg sync.WaitGroup
	if ps.busy {
		bg.Add(1)
		go func() { defer bg.Done(); p.traffic(stop) }()
	}
	if ps.others > 0 {
		bg.Add(1)
		go func() {
			defer bg.Done()
			for i := 0; i < ps.others; i++ {
				select {
				case <-stop:
					return
				case <-time.After(300*time.Millisecond + time.Duration(i%3)*37*time.Millisecond):
				}
				u := p.member()
				if !ps.newbies {
					u = senders[i%len(senders)]
				}
				p.post(u, fmt.Sprintf("%s message %d", ps.name, i))
			}
		}()
	}
	// own sends from the loop: type, a frame, Enter (t0), every 450 ms
	own, typed := 0, ""
	nextOwn := time.Now().Add(450 * time.Millisecond)
	askedAt, asked := e.r.WakeupTime()
	last := time.Now()
	start := time.Now()
	deadline := start.Add(30 * time.Second)
	// done: every message went out and drew, own sends' echoes included
	done := func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		others, echoes := 0, 0
		for _, w := range p.waits {
			if !w.done {
				return false
			}
			switch w.kind {
			case "others":
				others++
			case "echo":
				echoes++
			}
		}
		return others >= ps.others && echoes >= ps.own
	}
	for time.Now().Before(deadline) {
		if own >= ps.own && typed == "" && done() {
			break
		}
		if own < ps.own || typed != "" {
			if now := time.Now(); !now.Before(nextOwn) {
				if typed == "" {
					if ps.busyJS != nil {
						ps.busyJS(400 * time.Millisecond)
					}
					typed = fmt.Sprintf("%s own %d %d", ps.name, own, now.UnixNano()%1e6)
					e.typeText(typed)
					nextOwn = now // Enter after the frame that takes the text
				} else {
					if ps.drainJS != nil {
						ps.drainJS()
					}
					e.key(key.NameReturn, 0)
					p.mu.Lock()
					p.waits = append(p.waits, &latWait{kind: "own", text: typed, t0: time.Now(), frame0: p.frames})
					p.mu.Unlock()
					typed = ""
					own++
					nextOwn = time.Now().Add(450 * time.Millisecond)
				}
				askedAt, asked = e.r.WakeupTime() // the input is pending: a frame now
			}
		}
		var due <-chan time.Time
		if asked {
			due = time.After(max(time.Until(askedAt), 0))
		}
		select {
		case <-p.wake:
		case <-due:
		case <-time.After(10 * time.Millisecond): // the schedule and the end
			continue
		}
		if w := time.Until(last.Add(idleprobe.Vsync)); w > 0 { // the next vsync
			time.Sleep(w)
		}
		last = time.Now()
		e.quietFrame(asked, askedAt)
		p.check()
		askedAt, asked = e.r.WakeupTime()
		if typed != "" {
			nextOwn = time.Now() // that frame took the text
		}
	}
	close(stop)
	bg.Wait()
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*latWait(nil), p.waits...)
}

// traffic is a big workspace's socket while the client sits in the open
// channel: a message from someone new in another channel every 450 ms, a
// new person typing in the open channel every 700 ms, an Activity entry
// about a message from someone new every 1.2 s, a reaction elsewhere
// every 300 ms, presence every 100 ms and a profile change every 60 ms.
func (p *latProbe) traffic(stop <-chan struct{}) {
	fs := p.e.fs
	var others []string
	for _, id := range fs.ChannelIDs() {
		if id != p.ch && len(others) < 8 {
			others = append(others, id)
		}
	}
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	start := time.Now()
	var lastMsg, lastType, lastAct, lastReact, lastPres, lastProf time.Duration
	var elsewhere []string // ts of the messages elsewhere (reaction targets)
	for i := 0; ; i++ {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		el := time.Since(start)
		if el-lastMsg >= 450*time.Millisecond && len(others) > 0 {
			lastMsg = el
			c := others[i%len(others)]
			ts, ev := fs.PostQuiet(c, p.member(), fmt.Sprintf("elsewhere %d", i))
			elsewhere = append(elsewhere, c+"/"+ts)
			fs.Push(ev)
		}
		if el-lastType >= 700*time.Millisecond {
			lastType = el
			fs.Push(map[string]any{"type": "user_typing", "channel": p.ch, "user": p.member()})
		}
		if el-lastAct >= 1200*time.Millisecond && len(others) > 0 {
			lastAct = el
			now := time.Now()
			ts := fmt.Sprintf("%d.%06d", now.Unix(), now.Nanosecond()/1000)
			k := "at_user-" + ts
			fs.Push(map[string]any{"type": "activity", "subtype": "activity_updated", "key": k,
				"entry": map[string]any{"key": k, "feed_ts": ts, "is_unread": true,
					"item": map[string]any{"type": "at_user", "message": map[string]any{"ts": ts, "channel": others[i%len(others)], "author_user_id": p.member()}}}})
		}
		if el-lastReact >= 300*time.Millisecond && len(elsewhere) > 0 {
			lastReact = el
			c, ts, _ := strings.Cut(elsewhere[i%len(elsewhere)], "/")
			fs.Push(map[string]any{"type": "reaction_added", "user": p.member(), "reaction": "eyes",
				"item": map[string]any{"type": "message", "channel": c, "ts": ts}})
		}
		if el-lastPres >= 100*time.Millisecond {
			lastPres = el
			pr := "active"
			if i%2 == 0 {
				pr = "away"
			}
			fs.Push(map[string]any{"type": "presence_change", "user": latMember(i % 50), "presence": pr})
		}
		if el-lastProf >= 60*time.Millisecond {
			lastProf = el
			u := latMember(i % 50)
			fs.Push(map[string]any{"type": "user_change", "user": map[string]any{"id": u, "name": strings.ToLower(u), "real_name": "Synthetic " + u,
				"profile": map[string]any{"display_name": strings.ToLower(u), "status_emoji": ":wave:", "status_text": fmt.Sprintf("away %d", i)}}})
		}
	}
}

// latDist summarizes waits of one kind: how many, how many never drew,
// the latencies' median, p90 and max (ms), how many frames each took, and
// every latency (ms).
type latDist struct {
	N, Missed        int
	Median, P90, Max float64
	Frames           map[int]int
	All              []float64 `json:"ms"`
}

func latSummary(ws []*latWait, kind string) latDist {
	var ds []time.Duration
	st := latDist{Frames: map[int]int{}}
	for _, w := range ws {
		if w.kind != kind {
			continue
		}
		st.N++
		if !w.done {
			st.Missed++
			continue
		}
		ds = append(ds, w.lat)
		st.Frames[w.frames]++
		st.All = append(st.All, ms(w.lat))
	}
	if len(ds) > 0 {
		st.Median, st.P90, st.Max = pct(ds, 50), pct(ds, 90), pct(ds, 100)
	}
	return st
}

// latReport logs the pass's distributions and checks them against
// Slack's one frame; it returns them for MSG_LATENCY_OUT.
func latReport(t *testing.T, ps latPass, ws []*latWait, enforce bool) map[string]latDist {
	t.Helper()
	out := map[string]latDist{}
	for _, kind := range []string{"others", "own", "echo"} {
		st := latSummary(ws, kind)
		if st.N == 0 {
			continue
		}
		out[kind] = st
		var fr []string
		for n := range st.Frames {
			fr = append(fr, fmt.Sprintf("%d frame(s) ×%d", n, st.Frames[n]))
		}
		sort.Strings(fr)
		t.Logf("latency %-34s %-6s n=%d missed=%d median %.1f ms p90 %.1f ms max %.1f ms (%s); all: %.0f",
			ps.name, kind, st.N, st.Missed, st.Median, st.P90, st.Max, strings.Join(fr, ", "), st.All)
		if !enforce {
			continue
		}
		if st.Missed > 0 {
			t.Errorf("%s, %s: %d of %d rows never drew", ps.name, kind, st.Missed, st.N)
		}
		// an own message's pending copy: the frame after the one that took
		// the Enter key (Slack's sendMessage adds it in the keydown's
		// task), whatever plugins check and patch the send first (OwlMode's
		// check and prepareAndSendMessage patch: their JS runs ahead of the
		// queued work and of the composer's re-render after Enter). With
		// 400 ms of JS queued (busyJS) the checks wait for the 10 ms task
		// running when they come: one frame more there, never the backlog.
		// A frame the socket's traffic asks for in between counts too, so
		// only the quiet passes count frames.
		maxFrames, median := 2, latMedianBudget
		if kind == "own" && ps.pluginSend && ps.busyJS != nil {
			maxFrames, median = 3, latMedianBudget+idleprobe.Vsync
		}
		if kind == "own" && !ps.busy {
			// With plugins and the JS thread drained before each Enter, one
			// send of the pass may come a frame late: a task the plugin
			// runtime starts on its own after the drain (React's scheduler,
			// a timer: 5 to 10 ms mountIn tasks on the runners) holds the
			// send's checks while it runs, as a browser's keydown waits for
			// the task running when it comes. Two late sends are the sends'
			// own work.
			slack := 0
			if ps.pluginSend && ps.drainJS != nil {
				slack = 1
			}
			for n, k := range st.Frames {
				switch {
				case n > maxFrames && n == maxFrames+1 && slack > 0:
					if k > slack {
						t.Errorf("%s, own: %d sends drew their pending copy %d frames after the one that took the Enter key, want at most %d (one send of the pass a frame later)", ps.name, k, n-1, maxFrames-1)
					}
				case n > maxFrames:
					t.Errorf("%s, own: %d sends drew their pending copy %d frames after the one that took the Enter key, want at most %d", ps.name, k, n-1, maxFrames-1)
				}
			}
		}
		if st.Median > ms(median) || st.P90 > ms(latP90Budget) || st.Max > ms(latMaxBudget) {
			t.Errorf("%s, %s: event to row median %.1f ms, p90 %.1f ms, max %.1f ms; Slack draws it the next frame (budget: median %.0f, p90 %.0f, max %.0f ms)",
				ps.name, kind, st.Median, st.P90, st.Max, ms(median), ms(latP90Budget), ms(latMaxBudget))
		}
	}
	return out
}

// latWrite appends a test's passes to MSG_LATENCY_OUT (JSON lines), for
// the perf job's report.
func latWrite(t *testing.T, test string, res map[string]map[string]latDist) {
	path := os.Getenv("MSG_LATENCY_OUT")
	if path == "" {
		return
	}
	b, _ := json.Marshal(map[string]any{"test": test, "passes": res})
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Log(err)
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

// latPasses are the passes every variant runs.
func latPasses() []latPass {
	return []latPass{
		{name: "quiet, people it holds", others: 10},
		{name: "quiet, people it never fetched", others: 10, newbies: true},
		{name: "quiet, own sends", own: 6},
		{name: "busy socket, people it holds", others: 12, busy: true},
		{name: "busy socket, own sends", own: 6, busy: true},
	}
}

// TestMessageLatency: without plugins. Its millisecond budgets want a
// runner to itself, as TestIdleFrames's frame counts do: ci's linux-idle
// job and the flake-check workflow set CLACK_LATENCY_TESTS.
func TestMessageLatency(t *testing.T) {
	if os.Getenv("CLACK_LATENCY_TESTS") == "" {
		t.Skip("CLACK_LATENCY_TESTS not set")
	}
	e := newLatEnv(t, latEnvOpts{rtt: latRTT})
	p := newLatProbe(t, e, e.fs.ChannelIDs()[0])
	res := map[string]map[string]latDist{}
	for _, ps := range latPasses() {
		res[ps.name] = p.measure(t, ps, true)
	}
	latWrite(t, "TestMessageLatency", res)
}

// measure runs ps and reports it (latReport); then every own send is one
// chat.postMessage and one row in core's window, its pending copy
// swapped for the confirmed one.
func (p *latProbe) measure(t *testing.T, ps latPass, enforce bool) map[string]latDist {
	t.Helper()
	posts, asked, names := p.e.fs.CallCount("chat.postMessage"), p.e.fs.CallCount("edge/users/info"), p.names.Load()
	ws := p.run(ps)
	res := latReport(t, ps, ws, enforce)
	// what the pass's messages cost in member lookups: a socket message
	// asks for nobody until a row draws its people (the busy passes'
	// senders elsewhere draw nowhere)
	t.Logf("lookups %-34s users/info calls %d, names events %d", ps.name, p.e.fs.CallCount("edge/users/info")-asked, p.names.Load()-names)
	if got := p.e.fs.CallCount("chat.postMessage") - posts; enforce && got != int64(ps.own) {
		t.Errorf("%s: %d chat.postMessage calls for %d sends", ps.name, got, ps.own)
	}
	if !enforce {
		return res
	}
	for _, w := range ws {
		if w.kind != "own" {
			continue
		}
		var n int
		if tl := p.e.a.data.core.S.Timeline(p.ch); tl != nil {
			for _, m := range tl.Msgs {
				if m.Text == w.text {
					n++
				}
			}
		}
		if n != 1 {
			t.Errorf("%s: %q is %d rows in the window, want 1", ps.name, w.text, n)
		}
	}
	return res
}
