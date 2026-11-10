package demo

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// The bots: a scripted, local engine (no LLM, no network). Ambient timers
// play scenes from data/scenes*.json, rotate statuses and presence, post
// Pulse polls, ring the viewer now and then and move Inkwell's PR along;
// the viewer's own messages get answers in character (bots_reply.go).
// Every delay is divided by Options.Speed; Options.Quiet turns the
// ambient timers off.

type engine struct {
	w *World
	s *Scripts

	mu       sync.Mutex
	plays    map[string]int  // scene id -> times played
	used     map[string]bool // dialogue lines said already
	statusAt map[string]int  // handle -> next Statuses index
	progress int             // next Inkwell progress update
	pollNext int
	prompts  map[string]string // "ch/ts" of an Inkwell reply -> its prompt (Regenerate)
	// answered is the DM of the last ring the viewer answered, and when
	answered   string
	answeredAt time.Time
}

func newEngine(w *World) *engine {
	return &engine{w: w, s: w.scripts, plays: map[string]int{}, used: map[string]bool{}, statusAt: map[string]int{},
		prompts: map[string]string{}}
}

func (e *engine) start(ctx context.Context) {
	if e.w.opts.Quiet {
		return
	}
	w := e.w
	w.goLoop(e.ambient)
	w.goLoop(e.statuses)
	w.goLoop(e.pollLoop)
	w.goLoop(e.huddleLoop)
	w.goLoop(e.agentLoop)
}

// ambient plays a scene within 10–20 s, then one every 45–120 s.
func (e *engine) ambient() {
	w := e.w
	if !w.sleep(w.randDur(10*time.Second, 20*time.Second)) {
		return
	}
	for {
		if sc := e.pickScene(); sc != nil {
			w.busy.Add(1)
			e.playScene(sc)
			w.busy.Add(-1)
		}
		if !w.sleep(w.randDur(45*time.Second, 120*time.Second)) {
			return
		}
	}
}

// pickScene picks among the least-played scenes, by weight; scenes marked
// Once play once.
func (e *engine) pickScene() *Scene {
	e.mu.Lock()
	least := -1
	for i := range e.s.Scenes {
		sc := &e.s.Scenes[i]
		n := e.plays[sc.ID]
		if sc.Once && n > 0 {
			continue
		}
		if least < 0 || n < least {
			least = n
		}
	}
	var cands []*Scene
	total := 0
	for i := range e.s.Scenes {
		sc := &e.s.Scenes[i]
		if n := e.plays[sc.ID]; n != least || (sc.Once && n > 0) {
			continue
		}
		cands = append(cands, sc)
		total += max(sc.Weight, 1)
	}
	e.mu.Unlock()
	if total == 0 {
		return nil
	}
	r := e.w.pick(total)
	for _, sc := range cands {
		if r -= max(sc.Weight, 1); r < 0 {
			return sc
		}
	}
	return cands[len(cands)-1]
}

// PlayScene plays a scene by id now (tests, the smoke test); false when
// there is no such scene.
func (w *World) PlayScene(id string) bool {
	for i := range w.scripts.Scenes {
		if sc := &w.scripts.Scenes[i]; sc.ID == id {
			w.goBusy(func() { w.bots.playScene(sc) })
			return true
		}
	}
	return false
}

type posted struct{ ch, ts string }

func (e *engine) playScene(sc *Scene) {
	w := e.w
	e.mu.Lock()
	e.plays[sc.ID]++
	e.mu.Unlock()
	w.logf("demo: scene %s", sc.ID)
	refs := make([]posted, len(sc.Steps))
	roots := map[string]string{}
	lastMsg := -1
	for i, st := range sc.Steps {
		who, ok := w.person(st.Who)
		if !ok {
			continue
		}
		pause := time.Duration(st.Pause * float64(time.Second))
		if pause == 0 {
			if i == 0 {
				pause = w.randDur(0, 2*time.Second)
			} else {
				pause = w.randDur(4*time.Second, 14*time.Second)
			}
		}
		if st.React != "" {
			to := lastMsg
			if st.To != nil {
				to = *st.To
			}
			if !w.sleep(min(pause, w.randDur(2*time.Second, 5*time.Second))) {
				return
			}
			if to >= 0 && to < len(refs) && refs[to].ts != "" && w.validEmoji(st.React) {
				w.react(who.ID, refs[to].ch, refs[to].ts, st.React, true)
			}
			continue
		}
		ch := e.channelFor(orStr(st.Channel, sc.Channel), who.ID)
		if ch == "" {
			continue
		}
		thread := ""
		switch {
		case st.In != nil && *st.In >= 0 && *st.In < len(refs) && refs[*st.In].ts != "":
			ch, thread = refs[*st.In].ch, refs[*st.In].ts
		case st.Thread:
			thread = roots[ch]
		}
		text := e.expand(st.Text, "")
		if !e.typeThenWait(who.ID, ch, thread, text, pause) {
			return
		}
		m := slack.Message{User: who.ID, Text: text, ThreadTS: thread}
		if st.Broadcast && thread != "" {
			m.Subtype = "thread_broadcast"
		}
		ts, _ := w.post(ch, m, false)
		refs[i] = posted{ch, ts}
		lastMsg = i
		if thread == "" && roots[ch] == "" {
			roots[ch] = ts
		}
	}
}

// typeThenWait waits out pause, the last part of it with user typing in
// ch (Slack's "X is typing…"), 1.5–6 s depending on the text's length.
func (e *engine) typeThenWait(user, ch, thread, text string, pause time.Duration) bool {
	w := e.w
	t := 1500*time.Millisecond + time.Duration(len(text))*45*time.Millisecond
	t = min(t+w.randDur(0, time.Second), 6*time.Second)
	if t > pause {
		pause = t
	}
	if !w.sleep(pause - t) {
		return false
	}
	// Slack's client drops a typing line after a few seconds without a
	// new user_typing: repeat it while "typing"
	for t > 0 {
		w.typing(user, ch, thread)
		step := min(t, 3*time.Second)
		if !w.sleep(step) {
			return false
		}
		t -= step
	}
	return true
}

// channelFor resolves a script channel key ("dm": the speaker's DM with
// the viewer, opened when new).
func (e *engine) channelFor(key, speaker string) string {
	switch key {
	case "general":
		return GeneralID
	case "random":
		return RandomID
	case "sales":
		return SalesID
	case "party":
		return PartyID
	case "it":
		return ITHelpID
	case "warehouse":
		return WareID
	case "lunch":
		return LunchID
	case "kudos":
		return KudosID
	case "santa":
		return SantaID
	case "code":
		return CodeID
	case "group":
		return GroupDMID
	case "dm":
		return e.w.ensureDM(speaker)
	}
	return ""
}

// channelKey is channelFor's inverse for the fixed channels ("dm" for a
// DM, "" otherwise).
func (e *engine) channelKey(ch string) string {
	for _, k := range []string{"general", "random", "sales", "party", "it", "warehouse", "lunch", "kudos", "santa", "code", "group"} {
		if e.channelFor(k, "") == ch {
			return k
		}
	}
	if strings.HasPrefix(ch, "D") {
		return "dm"
	}
	return ""
}

var placeholderRe = regexp.MustCompile(`\{([@#]?[a-z_-]+)\}`)

// expand fills in a script line's placeholders; name is {name}'s first
// name ("" uses the viewer's).
func (e *engine) expand(text, name string) string {
	w := e.w
	if name == "" {
		name = w.firstName(SelfID)
	}
	return placeholderRe.ReplaceAllStringFunc(text, func(m string) string {
		k := m[1 : len(m)-1]
		switch {
		case k == "self":
			return "<@" + SelfID + ">"
		case k == "you":
			return w.firstName(SelfID)
		case k == "name":
			return name
		case k == "here":
			return "<!here>"
		case k == "channel":
			return "<!channel>"
		case strings.HasPrefix(k, "@"):
			if p, ok := w.person(k[1:]); ok {
				return "<@" + p.ID + ">"
			}
			if k == "@inkwell" {
				return "<@" + InkwellID + ">"
			}
			if k == "@pulse" {
				return "<@" + PulseID + ">"
			}
		case strings.HasPrefix(k, "#"):
			if id := e.channelFor(k[1:], ""); id != "" && k != "#dm" {
				w.mu.Lock()
				c, ok := w.convLocked(id)
				name := ""
				if ok {
					name = c.Name
				}
				w.mu.Unlock()
				return "<#" + id + "|" + name + ">"
			}
		}
		return m
	})
}

// line picks one of lines, preferring ones not said yet.
func (e *engine) line(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	e.mu.Lock()
	var fresh []string
	for _, l := range lines {
		if !e.used[l] {
			fresh = append(fresh, l)
		}
	}
	if len(fresh) == 0 {
		for _, l := range lines {
			delete(e.used, l)
		}
		fresh = lines
	}
	e.mu.Unlock()
	l := fresh[e.w.pick(len(fresh))]
	e.mu.Lock()
	e.used[l] = true
	e.mu.Unlock()
	return l
}

// statuses rotates someone's custom status every 1–3 minutes, and now
// and then someone steps away or comes back.
func (e *engine) statuses() {
	w := e.w
	if !w.sleep(w.randDur(30*time.Second, 60*time.Second)) {
		return
	}
	for {
		var cands []Person
		for _, p := range w.ws.People {
			if len(p.Statuses) > 0 && !p.Self && !p.Bot {
				cands = append(cands, p)
			}
		}
		if len(cands) > 0 {
			p := cands[w.pick(len(cands))]
			e.mu.Lock()
			i := e.statusAt[p.Handle]
			e.statusAt[p.Handle] = i + 1
			e.mu.Unlock()
			w.SetStatus(p.ID, p.Statuses[i%len(p.Statuses)])
		}
		if w.chance(0.35) {
			var people []Person
			for _, p := range w.ws.People {
				if !p.Self && !p.Bot {
					people = append(people, p)
				}
			}
			if len(people) > 0 {
				p := people[w.pick(len(people))]
				w.mu.Lock()
				u := w.users[p.ID]
				pres := ""
				if u != nil {
					pres = map[string]string{"active": "away"}[u.Presence]
					if pres == "" {
						pres = "active"
					}
				}
				w.mu.Unlock()
				w.setPresence(p.ID, pres)
			}
		}
		if !w.sleep(w.randDur(60*time.Second, 180*time.Second)) {
			return
		}
	}
}

// SetStatus sets a user's custom status and pushes user_change with the
// whole user, as Slack does.
func (w *World) SetStatus(id string, s Status) {
	w.mu.Lock()
	u, ok := w.users[id]
	if !ok {
		w.mu.Unlock()
		return
	}
	u.Profile.StatusText, u.Profile.StatusEmoji = s.Text, s.Emoji
	u.Profile.StatusExpiration = 0
	u.Profile.StatusEmojiDisplay = nil
	u.Updated = w.now().Unix()
	cp := *u
	w.mu.Unlock()
	w.fs.Push(map[string]any{"type": "user_change", "user": cp, "cache_ts": cp.Updated, "event_ts": microTS(w.now().UnixMicro())})
	w.fs.Push(map[string]any{"type": "user_status_changed", "user": cp, "cache_ts": cp.Updated})
}
