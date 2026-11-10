package applog

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Debug lines and the startup timeline.
//
// Debugf lines are on for the first DebugWindow after the process starts
// (StartTimeline sets the start) and always with CLACK_DEBUG=1, so every
// clack.log shows how the last start went without logging chatter all day.
//
// The startup timeline is a list of milestones, each logged once with its
// time since the process started:
//
//	debug: startup +412ms sidebar (467 rows)
//
// Clack's packages Mark what they reach (the window, the first frame, the
// account theme, the sidebar from the disk cache and from Slack, the open
// channel's messages, images, plugins). Relayout counts the frames where
// something already on screen moved (the sidebar's rows, the message
// list's content); once every expected milestone is reached and nothing
// moved for a while, the UI calls Settle, which logs one summary line:
//
//	startup: timeline settled=1830ms window=31ms first-frame=118ms … relayouts=2 (sidebar=1 messages=1)
//
// The perf workflow reads that line (.github/perf/process.sh).

// DebugWindow is how long after start Debugf logs without CLACK_DEBUG.
const DebugWindow = 60 * time.Second

var (
	dbgMu     sync.Mutex
	dbgUntil  time.Time
	dbgAlways = os.Getenv("CLACK_DEBUG") != "" || os.Getenv("CLACK_TIMING") != ""
)

// DebugOn reports whether Debugf lines are written now.
func DebugOn() bool {
	if dbgAlways {
		return true
	}
	dbgMu.Lock()
	defer dbgMu.Unlock()
	return time.Now().Before(dbgUntil)
}

// Debugf logs a "debug: " line while DebugOn. Like every log call, it must
// not carry message text.
func Debugf(format string, args ...any) {
	if DebugOn() {
		_ = log.Output(2, "debug: "+fmt.Sprintf(format, args...))
	}
}

// Milestone is one reached point of the startup timeline.
type Milestone struct {
	Name   string
	At     time.Duration // since the process started
	Detail string
}

type timeline struct {
	mu        sync.Mutex
	start     time.Time
	marks     []Milestone
	seen      map[string]bool
	relayouts map[string]int
	lastMove  time.Duration
	settled   bool
}

var tl = &timeline{seen: map[string]bool{}, relayouts: map[string]int{}}

// StartTimeline sets the process start time (cmd/clack's first line) and
// opens the debug window. Later calls only move the start (tests).
func StartTimeline(start time.Time) {
	tl.mu.Lock()
	tl.start = start
	tl.marks, tl.seen, tl.relayouts, tl.lastMove, tl.settled = nil, map[string]bool{}, map[string]int{}, 0, false
	tl.mu.Unlock()
	dbgMu.Lock()
	dbgUntil = start.Add(DebugWindow)
	dbgMu.Unlock()
}

func (t *timeline) since(at time.Time) time.Duration {
	if t.start.IsZero() {
		t.start = at
	}
	return at.Sub(t.start)
}

// Mark records milestone name (once; later calls are ignored) at now.
// detail is a short note (counts, a source), never message text.
func Mark(name, detail string) { MarkAt(name, time.Now(), detail) }

// MarkAt is Mark with the time the milestone was reached.
func MarkAt(name string, at time.Time, detail string) {
	tl.mu.Lock()
	if tl.seen[name] || tl.settled {
		tl.mu.Unlock()
		return
	}
	tl.seen[name] = true
	d := tl.since(at)
	tl.marks = append(tl.marks, Milestone{Name: name, At: d, Detail: detail})
	tl.mu.Unlock()
	if detail != "" {
		Debugf("startup +%dms %s (%s)", d.Milliseconds(), name, detail)
	} else {
		Debugf("startup +%dms %s", d.Milliseconds(), name)
	}
}

// Marked reports whether name was reached.
func Marked(name string) bool {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	return tl.seen[name]
}

// Relayout counts one visible re-layout of what (a frame where something
// already painted moved) until the timeline settles.
func Relayout(what string) {
	tl.mu.Lock()
	if tl.settled {
		tl.mu.Unlock()
		return
	}
	tl.relayouts[what]++
	d := tl.since(time.Now())
	tl.lastMove = d
	n := tl.relayouts[what]
	tl.mu.Unlock()
	Debugf("startup +%dms relayout %s (#%d)", d.Milliseconds(), what, n)
}

// LastMove is when the last counted re-layout happened (0: none).
func LastMove() time.Duration {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	return tl.lastMove
}

// Settled reports whether Settle ran.
func Settled() bool {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	return tl.settled
}

// Settle ends the timeline at at (the last moment anything changed) and
// logs the summary line. reason says why it ended ("quiet", "timeout").
func Settle(at time.Time, reason string) string {
	tl.mu.Lock()
	if tl.settled {
		tl.mu.Unlock()
		return ""
	}
	tl.settled = true
	d := tl.since(at)
	marks := append([]Milestone(nil), tl.marks...)
	rl := make(map[string]int, len(tl.relayouts))
	total := 0
	for k, v := range tl.relayouts {
		rl[k] = v
		total += v
	}
	tl.mu.Unlock()
	line := summary(d, reason, marks, rl, total)
	log.Print(line)
	return line
}

func summary(settled time.Duration, reason string, marks []Milestone, rl map[string]int, total int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "startup: timeline settled=%dms", settled.Milliseconds())
	sort.SliceStable(marks, func(i, j int) bool { return marks[i].At < marks[j].At })
	for _, m := range marks {
		fmt.Fprintf(&b, " %s=%dms", m.Name, m.At.Milliseconds())
	}
	fmt.Fprintf(&b, " relayouts=%d", total)
	if total > 0 {
		keys := make([]string, 0, len(rl))
		for k := range rl {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = fmt.Sprintf("%s=%d", k, rl[k])
		}
		b.WriteString(" (" + strings.Join(parts, " ") + ")")
	}
	if reason != "" && reason != "quiet" {
		b.WriteString(" [" + reason + "]")
	}
	return b.String()
}

// Timeline returns the milestones reached so far, in order.
func Timeline() []Milestone {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	out := append([]Milestone(nil), tl.marks...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out
}

// Relayouts returns the re-layout counts so far.
func Relayouts() map[string]int {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	out := make(map[string]int, len(tl.relayouts))
	for k, v := range tl.relayouts {
		out[k] = v
	}
	return out
}
