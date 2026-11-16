//go:build !go1.25

package tracerec

import (
	"bytes"
	"io"
	"strings"
	"testing"

	xtrace "golang.org/x/exp/trace"
)

// parseWindow reads a window as go tool trace would (x/exp/trace's
// reader, which reads Go 1.22-1.24 traces) and counts its events, its
// stop-the-world ranges and its GC mark phases.
func parseWindow(t *testing.T, b []byte) (events, stw, gc int, ok bool) {
	t.Helper()
	rd, err := xtrace.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("the window isn't a trace: %v", err)
	}
	for {
		ev, err := rd.ReadEvent()
		if err == io.EOF {
			return events, stw, gc, true
		}
		if err != nil {
			t.Fatalf("event %d: %v", events, err)
		}
		events++
		if k := ev.Kind(); k == xtrace.EventRangeBegin || k == xtrace.EventRangeActive {
			switch name := ev.Range().Name; {
			case strings.HasPrefix(name, "stop-the-world"):
				stw++
			case name == "GC concurrent mark phase":
				gc++
			}
		}
	}
}
