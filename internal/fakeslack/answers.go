package fakeslack

import (
	"encoding/json"
	"sort"
	"sync"
	"time"
)

// What the fake couldn't answer, for tests that must not hit a gap
// (internal/ui's TestDemoSurfaces): every request no hook and no handler
// knew (the client got a bare {"ok":true} with none of the fields it
// reads) and every {"ok":false} answer, with the error.

// unanswered is handle's answer when nothing in it knew the method.
type unanswered struct{}

// AnswerKind is how the fake answered a request it couldn't serve.
type AnswerKind string

const (
	answerNone  AnswerKind = "unanswered" // no hook or handler knew the method
	answerError AnswerKind = "error"      // {"ok":false,...}
)

// Gap is one request the fake couldn't serve.
type Gap struct {
	Method string
	Kind   AnswerKind
	Error  string // the answer's "error" (Kind error)
	// Form is the request's form, values cut to 80 bytes (no message
	// content is written anywhere: tests log it).
	Form map[string]string
	At   time.Time
}

type gapLog struct {
	mu   sync.Mutex
	gaps []Gap
}

func (s *Server) gapLog() *gapLog { return &s.gaps }

func (s *Server) noteAnswer(method string, form map[string][]string, kind AnswerKind, e string) {
	fm := map[string]string{}
	for k, vs := range form {
		if len(vs) == 0 || k == "token" {
			continue
		}
		v := vs[0]
		if len(v) > 80 {
			v = v[:80] + "…"
		}
		fm[k] = v
	}
	g := s.gapLog()
	g.mu.Lock()
	g.gaps = append(g.gaps, Gap{Method: method, Kind: kind, Error: e, Form: fm, At: time.Now()})
	g.mu.Unlock()
}

// Gaps is every request the fake couldn't serve so far, in order.
func (s *Server) Gaps() []Gap {
	g := s.gapLog()
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]Gap(nil), g.gaps...)
}

// GapSummary is Gaps grouped: "method kind[:error]" -> count, sorted keys.
func (s *Server) GapSummary() ([]string, map[string]int) {
	n := map[string]int{}
	for _, g := range s.Gaps() {
		k := g.Method + " " + string(g.Kind)
		if g.Error != "" {
			k += ":" + g.Error
		}
		n[k]++
	}
	keys := make([]string, 0, len(n))
	for k := range n {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, n
}

// failedAnswer reports an answer of {"ok":false} and its error.
func failedAnswer(b []byte) (string, bool) {
	var v struct {
		OK    *bool  `json:"ok"`
		Error string `json:"error"`
	}
	if json.Unmarshal(b, &v) != nil || v.OK == nil || *v.OK {
		return "", false
	}
	return v.Error, true
}
