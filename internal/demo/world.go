package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// World serves a Workspace through fakeslack's hooks and runs the bots.
// Its state starts as a copy of the Workspace and changes as the viewer
// and the bots post, react, pin and read; everything lives in memory.
//
// Locking: mu guards every field below it. Realtime events are pushed
// after mu is released (fs.Push writes to sockets and may take a while),
// so state changes return the events they caused and the caller pushes
// them.
type World struct {
	fs   *fakeslack.Server
	ws   *Workspace
	opts Options
	t0   time.Time // wall clock at newWorld (now() runs opts.Now forward from it)

	scripts *Scripts
	bots    *engine

	ctx    context.Context // ends at Stop (or when Start's ctx ends)
	cancel context.CancelFunc
	wg     sync.WaitGroup
	busy   atomic.Int64 // bot work in flight (WaitIdle)
	start  sync.Once
	stop   sync.Once
	// runMu orders goroutine starts against Stop: a request that comes
	// in while Stop waits must not Add to the WaitGroup
	runMu   sync.Mutex
	stopped bool

	mu        sync.Mutex
	rnd       *rand.Rand
	lastMicro int64
	convs     []slack.Conversation
	users     map[string]*slack.User
	hist      map[string][]slack.Message            // newest first
	replies   map[string]map[string][]slack.Message // channel -> parent ts -> oldest first
	pins      map[string][]livePin
	lastRead  map[string]string // conversations.mark (or the seed)
	seedRead  map[string]string // the seed's last_read: seeded mentions count until it moves
	seedMent  map[string]int
	startTS   string            // messages after this are live
	threadRd  map[string]string // "ch/thread" -> ts the viewer read the thread to
	activity  []*actEntry       // newest first
	actSeq    int
	polls     map[string]*pollState // "ch/ts"
	sections  []*liveSection
	secSeq    int
	huddle    *ringState      // the pending huddle invite, if any
	saved     map[string]bool // saved.add: "ch/ts"
	savedAt   map[string]int64
	bookmarks map[string][]map[string]any
}

type livePin struct {
	TS, By string
	At     int64
}

func newWorld(fs *fakeslack.Server, ws *Workspace, opts Options) *World {
	if opts.Speed <= 0 {
		opts.Speed = 1
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if opts.Seed == 0 {
		opts.Seed = opts.Now.UnixNano()
	}
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	w := &World{fs: fs, ws: ws, opts: opts, t0: time.Now(), rnd: rand.New(rand.NewSource(opts.Seed)),
		users: map[string]*slack.User{}, hist: map[string][]slack.Message{}, replies: map[string]map[string][]slack.Message{},
		pins: map[string][]livePin{}, lastRead: map[string]string{}, seedRead: map[string]string{}, seedMent: map[string]int{},
		threadRd: map[string]string{}, polls: map[string]*pollState{}, saved: map[string]bool{}, savedAt: map[string]int64{},
		bookmarks: map[string][]map[string]any{}}
	s, err := loadScripts()
	if err != nil {
		opts.Logf("demo: scripts: %v", err)
		s = &Scripts{}
	}
	w.scripts = s
	w.ctx, w.cancel = context.WithCancel(context.Background())
	w.seed()
	w.bots = newEngine(w)
	return w
}

// seed copies the Workspace into the world's live state.
func (w *World) seed() {
	ws := w.ws
	w.convs = append([]slack.Conversation(nil), ws.Convs...)
	for i := range ws.Users {
		u := ws.Users[i]
		if u.Presence == "" {
			u.Presence = "active"
		}
		w.users[u.ID] = &u
	}
	if _, ok := w.users[SelfID]; !ok && ws.Self.ID != "" {
		u := ws.Self
		w.users[u.ID] = &u
	}
	newest := int64(0)
	note := func(ts string) {
		if us := tsMicros(ts); us > newest {
			newest = us
		}
	}
	for ch, ms := range ws.History {
		out := make([]slack.Message, len(ms))
		for i := range ms {
			out[i] = copyMsg(ms[i])
			note(ms[i].TS)
		}
		sort.SliceStable(out, func(i, j int) bool { return tsBefore(out[j].TS, out[i].TS) })
		w.hist[ch] = out
	}
	for ch, th := range ws.Replies {
		m := map[string][]slack.Message{}
		for parent, rs := range th {
			out := make([]slack.Message, len(rs))
			for i := range rs {
				out[i] = copyMsg(rs[i])
				note(rs[i].TS)
			}
			m[parent] = out
		}
		w.replies[ch] = m
	}
	for ch, tss := range ws.Pins {
		for _, ts := range tss {
			by := SelfID
			if m, ok := w.findLocked(ch, ts); ok && m.User != "" {
				by = m.User
			}
			w.pins[ch] = append(w.pins[ch], livePin{TS: ts, By: by, At: tsMicros(ts) / 1e6})
		}
	}
	for _, c := range w.convs {
		latest := w.latestLocked(c.ID)
		lr := latest
		if u, ok := ws.Unread[c.ID]; ok && u.LastRead != "" {
			lr = u.LastRead
			w.seedMent[c.ID] = u.Mentions
		}
		if lr == "" {
			lr = "0000000000.000000"
		}
		w.lastRead[c.ID], w.seedRead[c.ID] = lr, lr
	}
	now := w.now().UnixMicro()
	if newest >= now {
		now = newest + 1
	}
	w.lastMicro = now
	w.startTS = microTS(now)
	w.seedSections()
	w.seedActivity()
}

// Start installs the hooks, puts the workspace's canvases, lists and
// scheduled messages into the fake, and starts the bots until ctx ends
// or Stop.
func (w *World) Start(ctx context.Context) {
	w.start.Do(func() {
		go func() {
			select {
			case <-ctx.Done():
				w.cancel()
			case <-w.ctx.Done():
			}
		}()
		for _, c := range w.ws.Canvases {
			w.fs.AddCanvas(c)
		}
		for _, l := range w.ws.Lists {
			w.fs.AddList(l)
		}
		for _, s := range w.ws.Scheduled {
			dest := map[string]any{"channel_id": s.Channel}
			if s.ThreadTS != "" {
				dest["thread_ts"] = s.ThreadTS
			}
			db, _ := json.Marshal([]any{dest})
			created := s.Created
			if created.IsZero() {
				created = w.now().Add(-time.Hour)
			}
			w.fs.AddDraft(fakeslack.Draft{Blocks: s.Blocks, Destinations: db, DateCreated: created.Unix(), DateScheduled: s.At.Unix()})
		}
		w.fs.SetHook(w.api)
		w.fs.SetEdgeHook(w.edge)
		w.bots.start(w.ctx)
	})
}

// Stop stops the bots and waits for them. It is safe to call twice, and
// without Start. The hooks stay: the fake keeps answering from the
// world's state until it closes.
func (w *World) Stop() {
	w.stop.Do(func() {
		w.runMu.Lock()
		w.stopped = true
		w.runMu.Unlock()
		w.cancel()
		w.wg.Wait()
	})
}

// now is the demo's clock: opts.Now, running.
func (w *World) now() time.Time { return w.opts.Now.Add(time.Since(w.t0)) }

// nextTSLocked is a fresh message ts, later than every other.
func (w *World) nextTSLocked() string { return microTS(w.nextMicroLocked()) }

func (w *World) nextMicroLocked() int64 {
	us := w.now().UnixMicro()
	if us <= w.lastMicro {
		us = w.lastMicro + 1
	}
	w.lastMicro = us
	return us
}

func microTS(us int64) string { return fmt.Sprintf("%d.%06d", us/1e6, us%1e6) }

// tsMicros parses a Slack ts into microseconds.
func tsMicros(ts string) int64 {
	sec, frac, _ := strings.Cut(ts, ".")
	s, _ := strconv.ParseInt(sec, 10, 64)
	frac = (frac + "000000")[:6]
	f, _ := strconv.ParseInt(frac, 10, 64)
	return s*1e6 + f
}

func tsBefore(a, b string) bool { return tsMicros(a) < tsMicros(b) }

// copyMsg copies the slices a message's state changes touch.
func copyMsg(m slack.Message) slack.Message {
	if m.Reactions != nil {
		rs := make([]slack.Reaction, len(m.Reactions))
		for i, r := range m.Reactions {
			r.Users = append([]string(nil), r.Users...)
			rs[i] = r
		}
		m.Reactions = rs
	}
	m.ReplyUsers = append([]string(nil), m.ReplyUsers...)
	if len(m.ReplyUsers) == 0 {
		m.ReplyUsers = nil
	}
	m.PinnedTo = append([]string(nil), m.PinnedTo...)
	if len(m.PinnedTo) == 0 {
		m.PinnedTo = nil
	}
	if m.Edited != nil {
		e := *m.Edited
		m.Edited = &e
	}
	return m
}

// ---- lookups (mu held) ----

func (w *World) convLocked(id string) (*slack.Conversation, bool) {
	for i := range w.convs {
		if w.convs[i].ID == id {
			return &w.convs[i], true
		}
	}
	return nil, false
}

// findLocked finds a message, top level or reply.
func (w *World) findLocked(ch, ts string) (slack.Message, bool) {
	if p := w.msgPtrLocked(ch, ts); p != nil {
		return *p, true
	}
	return slack.Message{}, false
}

func (w *World) msgPtrLocked(ch, ts string) *slack.Message {
	h := w.hist[ch]
	for i := range h {
		if h[i].TS == ts {
			return &h[i]
		}
	}
	for _, rs := range w.replies[ch] {
		for i := range rs {
			if rs[i].TS == ts {
				return &rs[i]
			}
		}
	}
	return nil
}

// latestLocked is a conversation's newest top-level ts ("" when empty).
func (w *World) latestLocked(ch string) string {
	if h := w.hist[ch]; len(h) > 0 {
		return h[0].TS
	}
	return ""
}

func (w *World) userLocked(id string) (slack.User, bool) {
	if u, ok := w.users[id]; ok {
		return *u, true
	}
	return slack.User{}, false
}

// person finds a cast member by handle or id.
func (w *World) person(handleOrID string) (Person, bool) {
	for _, p := range w.ws.People {
		if p.Handle == handleOrID || p.ID == handleOrID {
			return p, true
		}
	}
	return Person{}, false
}

// firstName is a user's first name, for dialogue.
func (w *World) firstName(id string) string {
	w.mu.Lock()
	u, ok := w.userLocked(id)
	w.mu.Unlock()
	if !ok {
		return "there"
	}
	if u.Profile.FirstName != "" {
		return u.Profile.FirstName
	}
	n := orStr(u.RealName, u.Profile.RealName, u.Name)
	f, _, _ := strings.Cut(n, " ")
	return f
}

// dmWith is the viewer's DM with user ("" when there is none).
func (w *World) dmWithLocked(user string) string {
	for _, c := range w.convs {
		if c.IsIM && c.User == user {
			return c.ID
		}
	}
	return ""
}

// ensureDM is the viewer's DM with user, opened (and announced with
// im_created) when there was none.
func (w *World) ensureDM(user string) string {
	if user == "" || user == SelfID {
		return ""
	}
	w.mu.Lock()
	if id := w.dmWithLocked(user); id != "" {
		w.mu.Unlock()
		return id
	}
	c := w.newDMLocked(user)
	w.mu.Unlock()
	w.fs.Push(map[string]any{"type": "im_created", "user": SelfID, "channel": c})
	return c.ID
}

func (w *World) newDMLocked(user string) slack.Conversation {
	id := "D" + strings.TrimPrefix(user, "U")
	if len(id) > 11 {
		id = id[:11]
	}
	if _, taken := w.convLocked(id); taken {
		id = fmt.Sprintf("D0OPEN%05d", len(w.convs))
	}
	c := slack.Conversation{ID: id, IsIM: true, User: user, IsOpen: true, Created: w.now().Unix(), ContextTeamID: TeamID}
	w.convs = append(w.convs, c)
	w.lastRead[id] = w.nextTSLocked()
	w.seedRead[id] = w.lastRead[id]
	return c
}

func orStr(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// ---- exported API (tests and the headless smoke test) ----

// Post posts text as a cast member (or the viewer, SelfID) through the
// same path as chat.postMessage: stored, pushed, and, from the viewer,
// answered by the bots. threadTS "" posts top level. It returns the ts.
func (w *World) Post(as, channel, threadTS, text string) string {
	m := slack.Message{User: as, Text: text, ThreadTS: threadTS}
	if as == SelfID {
		m.ClientMsgID = w.clientMsgID()
	}
	ts, _ := w.post(channel, m, false)
	return ts
}

// History is a conversation's top-level messages, newest first (a copy).
func (w *World) History(channel string) []slack.Message {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]slack.Message, len(w.hist[channel]))
	for i, m := range w.hist[channel] {
		out[i] = copyMsg(m)
	}
	return out
}

// Thread is a thread's replies, oldest first, without the parent.
func (w *World) Thread(channel, ts string) []slack.Message {
	w.mu.Lock()
	defer w.mu.Unlock()
	rs := w.replies[channel][ts]
	out := make([]slack.Message, len(rs))
	for i, m := range rs {
		out[i] = copyMsg(m)
	}
	return out
}

// Message finds one message (top level or reply).
func (w *World) Message(channel, ts string) (slack.Message, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	m, ok := w.findLocked(channel, ts)
	return copyMsg(m), ok
}

// ActivityCount is how many items the Activity feed has.
func (w *World) ActivityCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.activity)
}

// WaitIdle waits until no bot reply, scene, poll vote or agent stream is
// in flight (ambient timers waiting for their next turn don't count). It
// reports false on timeout.
func (w *World) WaitIdle(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	// let a reply that is about to be scheduled get scheduled
	time.Sleep(5 * time.Millisecond)
	for {
		if w.busy.Load() == 0 {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// goBusy runs f on a tracked goroutine counted by WaitIdle.
func (w *World) goBusy(f func()) {
	if !w.track() {
		return
	}
	w.busy.Add(1)
	go func() {
		defer w.wg.Done()
		defer w.busy.Add(-1)
		f()
	}()
}

// goLoop runs f on a tracked goroutine WaitIdle doesn't wait for (the
// ambient timers).
func (w *World) goLoop(f func()) {
	if !w.track() {
		return
	}
	go func() {
		defer w.wg.Done()
		f()
	}()
}

// track adds a goroutine to the WaitGroup; false once stopped.
func (w *World) track() bool {
	w.runMu.Lock()
	defer w.runMu.Unlock()
	if w.stopped {
		return false
	}
	w.wg.Add(1)
	return true
}

// sleep waits d scaled by Speed; false when the world stopped.
func (w *World) sleep(d time.Duration) bool {
	d = time.Duration(float64(d) / w.opts.Speed)
	if d <= 0 {
		return w.ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-w.ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// randDur is a random duration in [lo, hi) (unscaled).
func (w *World) randDur(lo, hi time.Duration) time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	if hi <= lo {
		return lo
	}
	return lo + time.Duration(w.rnd.Int63n(int64(hi-lo)))
}

// chance is true with probability p.
func (w *World) chance(p float64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.rnd.Float64() < p
}

// pick is a random index below n.
func (w *World) pick(n int) int {
	if n <= 1 {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.rnd.Intn(n)
}

func (w *World) clientMsgID() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return fmt.Sprintf("%08x-%04x-4%03x-a%03x-%012x", w.rnd.Uint32(), w.rnd.Intn(1<<16), w.rnd.Intn(1<<12), w.rnd.Intn(1<<12), w.rnd.Int63n(1<<48))
}

func (w *World) logf(format string, args ...any) { w.opts.Logf(format, args...) }
