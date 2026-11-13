package messages

import (
	"fmt"
	"testing"

	"github.com/some-du6e/clack/internal/core"
)

// core trimmed a conversation to Slack's message cache budget
// (core/evict.go, "evicted"): its model drops the same messages and says
// older ones exist, or starts over when the whole history went; a list
// that isn't on screen lets its rows and copy of the messages go, and one
// on screen keeps them (its next sync prunes what went).
func TestEvictedModelAndRows(t *testing.T) {
	m := &model{ch: "C1", typing: map[string]typingEntry{}}
	var page []core.Message
	for i := 0; i < 10; i++ {
		page = append(page, core.Message{TS: fmt.Sprintf("1762500%03d.000100", i), Channel: "C1"})
	}
	m.setAll(page)
	m.loaded = true
	rev := m.rev
	m.evict("1762500004.000100")
	if len(m.msgs) != 6 || m.msgs[0].m.TS != "1762500004.000100" || !m.hasMore || m.rev == rev {
		t.Fatalf("after evicting before 4: %d msgs from %s, hasMore %v", len(m.msgs), m.msgs[0].m.TS, m.hasMore)
	}

	s := &listState{m: m, rows: map[string]*row{"1762500004.000100": {}}, items: make([]item, 3)}
	s.snap.msgs = make([]entry, 6)
	s.snap.rev = 7
	prevC, prevT := lastChannel, lastThread
	defer func() { lastChannel, lastThread = prevC, prevT }()
	lastChannel, lastThread = s, nil
	if s.releaseEvicted() || len(s.rows) != 1 {
		t.Fatal("the list on screen let its rows go")
	}
	lastChannel = nil
	if !s.releaseEvicted() || len(s.rows) != 0 || s.items != nil || s.snap.msgs != nil || s.snap.rev != 0 {
		t.Fatalf("the list off screen kept %d rows", len(s.rows))
	}
	if s.releaseEvicted() {
		t.Fatal("released twice for one eviction")
	}

	m.evict("")
	if len(m.msgs) != 0 || m.loaded || m.hasMore {
		t.Fatalf("a whole eviction left %d msgs (loaded %v)", len(m.msgs), m.loaded)
	}
	if !s.releaseEvicted() {
		t.Fatal("the second eviction wasn't seen")
	}
}
