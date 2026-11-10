package composer

import (
	"sort"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/core"
)

// The typing hub listens to core's realtime typing events once per
// core.App and answers "who is typing here" for the hint bar under the
// composer ("Karim is typing").

// typingTTL is removeOldTypingRecords' 6e3 ms for other people.
const typingTTL = 6 * time.Second

type typer struct {
	name   string
	until  time.Time
	status string // an app's assistant status ("is thinking…"), assistant.threads.setStatus
}

// assistantTTL is how long an app status lasts without a message
// (assistant.threads.setStatus: two minutes).
const assistantTTL = 2 * time.Minute

type typingState struct {
	mu    sync.Mutex
	m     map[string]map[string]typer // draftKey(ch, ts) -> user id -> typer
	hosts map[Host]bool
	timer *time.Timer
}

var typingHubs struct {
	sync.Mutex
	m map[*core.App]*typingState
}

func typingHub(a *core.App) *typingState {
	if a == nil {
		return nil
	}
	typingHubs.Lock()
	defer typingHubs.Unlock()
	if s := typingHubs.m[a]; s != nil {
		return s
	}
	if typingHubs.m == nil {
		typingHubs.m = map[*core.App]*typingState{}
	}
	s := &typingState{m: map[string]map[string]typer{}}
	typingHubs.m[a] = s
	evs, _ := a.Subscribe()
	go func() {
		for ev := range evs {
			s.apply(ev)
		}
	}()
	return s
}

// addHost makes typing changes invalidate h's window.
func (s *typingState) addHost(h Host) {
	if s == nil || h == nil {
		return
	}
	s.mu.Lock()
	if s.hosts == nil {
		s.hosts = map[Host]bool{}
	}
	s.hosts[h] = true
	s.mu.Unlock()
}

func (s *typingState) invalidators() []func() {
	out := make([]func(), 0, len(s.hosts))
	for h := range s.hosts {
		out = append(out, h.Invalidate)
	}
	return out
}

func (s *typingState) apply(ev core.Event) {
	switch ev.Type {
	case "typing", "message", "assistant_status":
	default:
		return
	}
	k := draftKey(ev.Channel, ev.ThreadTS)
	s.mu.Lock()
	switch ev.Type {
	case "typing":
		if ev.UserID == "" {
			s.mu.Unlock()
			return
		}
		us := s.m[k]
		if us == nil {
			us = map[string]typer{}
			s.m[k] = us
		}
		name := ev.Name
		if name == "" {
			name = "Someone"
		}
		us[ev.UserID] = typer{name: name, until: time.Now().Add(typingTTL)}
	case "assistant_status":
		st, _ := core.AssistantStatusOf(ev)
		us := s.m[k]
		if st.Status == "" || (st.StatusType != "" && st.StatusType != "typing") {
			if us != nil {
				delete(us, ev.UserID)
			}
			break
		}
		if us == nil {
			us = map[string]typer{}
			s.m[k] = us
		}
		us[ev.UserID] = typer{name: firstNonEmptyName(ev.Name), until: time.Now().Add(assistantTTL), status: st.Status}
	case "message":
		uid := ev.UserID
		if uid == "" && ev.Msg != nil {
			uid = ev.Msg.User
		}
		if us := s.m[k]; us != nil {
			delete(us, uid)
		}
	}
	inv := s.invalidators()
	if s.timer == nil {
		s.timer = time.AfterFunc(typingTick, s.tick)
	}
	s.mu.Unlock()
	for _, f := range inv {
		f()
	}
}

// typingTick is Slack's maybeStartTimer interval: checkTypingMap runs
// every second while anyone is typing, and removeOldTypingRecords drops
// records 6 s after they started (5 s for the viewer's own).
const typingTick = time.Second

// tick is checkTypingMap: expire the old records, repaint when one went,
// and stop once nobody is typing (clearTypingCleanupInterval).
func (s *typingState) tick() {
	now := time.Now()
	s.mu.Lock()
	gone, live := false, false
	for k, us := range s.m {
		for id, t := range us {
			if !t.until.After(now) {
				delete(us, id)
				gone = true
			} else {
				live = true
			}
		}
		if len(us) == 0 {
			delete(s.m, k)
		}
	}
	if live {
		s.timer = time.AfterFunc(typingTick, s.tick)
	} else {
		s.timer = nil
	}
	inv := s.invalidators()
	s.mu.Unlock()
	if gone {
		for _, f := range inv {
			f()
		}
	}
}

func firstNonEmptyName(n string) string {
	if n == "" {
		return "Someone"
	}
	return n
}

// assistantLine is Slack's AiAppStatusIndicator: when the only typer is
// an app with a status, "<name> <status>" replaces the typing line.
func (s *typingState) assistantLine(ch, ts, self string) (name, status string) {
	if s == nil {
		return "", ""
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	us := s.m[draftKey(ch, ts)]
	n := 0
	for id, t := range us {
		if id == self || t.until.Before(now) {
			continue
		}
		n++
		name, status = t.name, t.status
	}
	if n != 1 || status == "" {
		return "", ""
	}
	return name, status
}

// names returns who is typing in ch/ts (excluding self), sorted.
func (s *typingState) names(ch, ts, self string) []string {
	_, names := s.typers(ch, ts, self)
	return names
}

// typers returns the user ids and names of who is typing in ch/ts
// (excluding self), sorted by name (ids in the same order).
func (s *typingState) typers(ch, ts, self string) (ids, names []string) {
	if s == nil {
		return nil, nil
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	us := s.m[draftKey(ch, ts)]
	for id, t := range us {
		if t.until.Before(now) {
			delete(us, id)
			continue
		}
		if id != self {
			ids = append(ids, id)
			names = append(names, t.name)
		}
	}
	if len(names) > 1 {
		sort.Sort(byName{ids, names})
	}
	return ids, names
}

type byName struct{ ids, names []string }

func (b byName) Len() int { return len(b.names) }
func (b byName) Less(i, j int) bool {
	if b.names[i] != b.names[j] {
		return b.names[i] < b.names[j]
	}
	return b.ids[i] < b.ids[j]
}
func (b byName) Swap(i, j int) {
	b.ids[i], b.ids[j] = b.ids[j], b.ids[i]
	b.names[i], b.names[j] = b.names[j], b.names[i]
}

// typingLine is Slack's TypingNames text (notification_bar):
// "<b>{name}</b> is typing", "<b>{name}</b> and <b>{other_name}</b> are
// typing", "Several people are typing", with no trailing ellipsis.
func typingLine(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0] + " is typing"
	case 2:
		return names[0] + " and " + names[1] + " are typing"
	default:
		return "Several people are typing"
	}
}

func typingText(a *core.App, ch, ts, self string) string {
	return typingLine(typingHub(a).names(ch, ts, self))
}
