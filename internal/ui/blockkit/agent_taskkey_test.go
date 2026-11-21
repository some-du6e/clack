package blockkit

// A plan task's state follows its task_id (Slack's PlanTaskList keys its
// items by task_id): a task the viewer closed stays closed when the app's
// update puts another task ahead of it.

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestAgentTaskStateFollowsTaskID(t *testing.T) {
	h := &agentHost{th: theme.New(false)}
	now := time.Now().Add(time.Hour) // see TestAgentTaskShowMore
	tick := func(s *agentShot, n int) {
		for i := 0; i < n; i++ {
			now = now.Add(100 * time.Millisecond)
			s.frame(560, 600, now)
		}
	}
	// the reference: the updated plan with every task open
	ref := newAgentShot(h, agentMsg(t, "plan-follow-2"))
	ref.rows[0].me.agentState().open["plan\x00p10"] = true
	tick(ref, 3)
	allOpen := slotH(t, ref.rows[0])

	m := agentMsg(t, "plan-follow-1")
	s := newAgentShot(h, m)
	st := s.rows[0].me.agentState()
	st.open["plan\x00p10"] = true
	st.open[taskStateKey("p10", "t2", 1)] = false // the viewer closed "Ran the smoke tests"
	tick(s, 3)
	r := s.rows[0]
	r.m = streamTo(t, m, "plan-follow-2") // t0 goes in ahead of it
	r.me = MessageElements(h, &r.m)
	r.v.Set(r.m.Doc, richview.Options{Elements: r.me})
	tick(s, 6)
	if got := slotH(t, r); got >= allOpen {
		t.Errorf("after the update the plan is %dpx, as tall as with every task open (%dpx): the closed task opened again when it moved", got, allOpen)
	}
}
