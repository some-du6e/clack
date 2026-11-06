package composer

import (
	"sort"
	"sync"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
)

// The typing hub listens to core's realtime typing events once per
// core.App and answers "who is typing here" for the hint bar under the
// composer ("Karim is typing…").

const typingTTL = 6 * time.Second

type typer struct {
	name  string
	until time.Time
}

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
	case "typing", "message":
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
		s.timer = time.AfterFunc(typingTTL+100*time.Millisecond, func() {
			s.mu.Lock()
			s.timer = nil
			inv := s.invalidators()
			s.mu.Unlock()
			for _, f := range inv {
				f()
			}
		})
	}
	s.mu.Unlock()
	for _, f := range inv {
		f()
	}
}

// names returns who is typing in ch/ts (excluding self), sorted.
func (s *typingState) names(ch, ts, self string) []string {
	if s == nil {
		return nil
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	us := s.m[draftKey(ch, ts)]
	var out []string
	for id, t := range us {
		if t.until.Before(now) {
			delete(us, id)
			continue
		}
		if id != self {
			out = append(out, t.name)
		}
	}
	sort.Strings(out)
	return out
}

// typingLine formats Slack's typing indicator text.
func typingLine(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0] + " is typing…"
	case 2:
		return names[0] + " and " + names[1] + " are typing…"
	default:
		return "Several people are typing…"
	}
}

func typingText(a *core.App, ch, ts, self string) string {
	return typingLine(typingHub(a).names(ch, ts, self))
}
