package blockkit

// The agent block state's caps evict by last use (re-review NITs): a
// drawing kept on screen keeps its state however many other drawings of
// the same message come and go (8 drawings a message), and a message
// drawn all along keeps its drawings however many other messages come
// after it (400 messages). By creation order, a plan open in a thread pane
// through a long session snapped back to its defaults.

import (
	"fmt"
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestAgentStateEvictsByLastUse(t *testing.T) {
	h := &agentHost{th: theme.New(false)}
	m := agentMsg(t, "plan-long-details")
	me := MessageElements(h, &m)
	stateOf := func(id uint64) *agentState {
		prev := me.inst
		me.inst = id
		defer func() { me.inst = prev }()
		return me.agentState()
	}
	const key = "plan\x00p5"
	kept := NewViewID()
	stateOf(kept).open[key] = true
	for i := 0; i < 9; i++ {
		stateOf(NewViewID()) // another drawing of the message, gone after
		if !stateOf(kept).open[key] {
			t.Fatalf("after %d other drawings the one kept on screen lost its open toggle", i+1)
		}
	}
	for i := 0; i < 401; i++ {
		other := &Message{h: h, m: core.Message{Channel: "C0EVICT", TS: fmt.Sprintf("1700000000.%06d", i)}}
		other.agentState()
		stateOf(kept) // the message drawn all along
	}
	if !stateOf(kept).open[key] {
		t.Error("after 401 other messages' agent states, the message drawn all along lost its drawing's open toggle")
	}
}
