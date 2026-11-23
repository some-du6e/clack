// Package stackdump writes every goroutine's stack into clack.log when a
// watchdog finds the socket or the event pump stalled (slack rtm.go, core
// pump_watch.go), so a report of "nothing happened for 45 minutes" comes
// with what each goroutine was doing. A stack holds function names, file
// paths of the build and argument words, never a message's text.
package stackdump

import (
	"bytes"
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"
)

// All is runtime.Stack for every goroutine, the buffer grown up to 16 MiB.
func All() []byte {
	buf := make([]byte, 256<<10)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) || len(buf) >= 16<<20 {
			return buf[:n]
		}
		buf = make([]byte, 2*len(buf))
	}
}

// Of is the goroutines of all whose stacks call fn (a frame's function as
// runtime.Stack prints it, such as "core.(*App).pump("), each as printed.
func Of(all []byte, fn string) []string {
	var out []string
	for _, g := range bytes.Split(all, []byte("\n\n")) {
		if bytes.Contains(g, []byte(fn)) {
			out = append(out, string(g))
		}
	}
	return out
}

// MaxDump is the most one record writes: the stalled goroutines come
// first, so the cap leaves out only the others. A whole dump (up to
// 16 MiB) rotated the 5 MiB clack.log on its own, and filled applog's
// Tail (its last 1 MiB) so the line that said why fell out of a log grab.
const MaxDump = 512 << 10

// Limiter lets a dump through at most once per Every, so a stall that
// comes back, or two at once, doesn't fill the log with copies.
type Limiter struct {
	Every time.Duration
	mu    sync.Mutex
	last  time.Time
}

// Log writes the stacks under what (one record, after the line that says
// why) when l lets it, and reports whether it did. first names stacks to
// print ahead of the rest (Of's), the goroutine the stall is on.
func (l *Limiter) Log(what string, first ...string) bool {
	l.mu.Lock()
	now := time.Now()
	if !l.last.IsZero() && now.Sub(l.last) < l.Every {
		l.mu.Unlock()
		return false
	}
	l.last = now
	l.mu.Unlock()
	all := All()
	var b bytes.Buffer
	b.WriteString(what)
	b.WriteString(":\n")
	for _, f := range first {
		for _, g := range Of(all, f) {
			b.WriteString(g)
			b.WriteString("\n\n")
		}
	}
	b.WriteString("all goroutines:\n")
	b.Write(all)
	if b.Len() > MaxDump {
		more := b.Len() - MaxDump
		b.Truncate(MaxDump)
		fmt.Fprintf(&b, "\n… %d bytes more of goroutines left out (stackdump.MaxDump)", more)
	}
	log.Print(b.String())
	return true
}
