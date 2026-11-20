package core

// The reply bar's agent nugget rule (r0Yq ThreadReplies,
// AppSessionOpenThreadReplyNugget, HQze it/ZL/zV, n7Jc): a root whose
// agent_session is processing shows its processing agents (expiry read),
// a thread with an app status shows that status when no agent is
// processing, and nothing shows for an active session, a Slackbot session
// or with the experiments off. Synthetic only.

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

func TestSessionReplyNugget(t *testing.T) {
	a, _ := testApp(t)
	a.S.PutUsers([]slack.User{{ID: "UAG1", Name: "codebot", IsBot: true, Profile: slack.Profile{RealName: "Codebot", APIAppID: "AAG1"}},
		{ID: "UAG2", Name: "planner", IsBot: true, Profile: slack.Profile{RealName: "Planner", APIAppID: "AAG2"}}})
	a.SetExperimentGroup("app_thread_nugget_fe", "on")
	a.SetExperimentGroup("app_nugget_refinement", "on")
	now := time.Unix(1763600000, 0)
	later := strconv.FormatInt(now.Unix()+3600, 10)
	n := 0
	msg := func(session string) *Message {
		t.Helper()
		n++
		ts := "1763599000.00010" + strconv.Itoa(n)
		var m slack.Message
		raw := `{"type":"message","ts":"` + ts + `","user":"USELF","text":"fix it","agent_session":` + session + `}`
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatal(err)
		}
		return a.messagePtr("C1", &m)
	}
	got := func(m *Message) (SessionNugget, bool) { return a.SessionReplyNugget(m, now) }

	working := msg(`{"status":"processing","agent_bot_user_ids":["UAG1"],"agent_statuses":[{"agent_bot_user_id":"UAG1","status":"processing",` +
		`"loading_message":"Reading the redirect code","is_stoppable":true,"date_status_processing_expire":` + later + `}]}`)
	nug, ok := got(working)
	if !ok || len(nug.Agents) != 1 || !nug.OpenThread {
		t.Fatalf("a processing session: %+v %v, want its agent", nug, ok)
	}
	if ag := nug.Agents[0]; ag.BotUserID != "UAG1" || ag.Name != "Codebot" || ag.AppID != "AAG1" || ag.LoadingMessage != "Reading the redirect code" || !ag.Stoppable {
		t.Errorf("the agent: %+v", ag)
	}

	// processing past its expiry reads as active: nothing to draw
	expired := msg(`{"status":"processing","agent_statuses":[{"agent_bot_user_id":"UAG1","status":"processing","date_status_processing_expire":` +
		strconv.FormatInt(now.Unix()-10, 10) + `}]}`)
	if nug, ok := got(expired); ok {
		t.Errorf("an expired processing agent: %+v, want no nugget", nug)
	}
	// an active session (Karim's message): no nugget
	active := msg(`{"status":"active","agent_bot_user_ids":["UAG1"],"agent_statuses":[{"agent_bot_user_id":"UAG1","status":"active","is_stoppable":false}]}`)
	if nug, ok := got(active); ok {
		t.Errorf("an active session: %+v, want no nugget", nug)
	}
	// two agents at work: both (the bar collapses them)
	two := msg(`{"status":"processing","agent_statuses":[{"agent_bot_user_id":"UAG1","status":"processing"},{"agent_bot_user_id":"UAG2","status":"processing"},` +
		`{"agent_bot_user_id":"UAG2","status":"active"}]}`)
	if nug, ok := got(two); !ok || len(nug.Agents) != 2 {
		t.Errorf("two processing agents: %+v %v, want both", nug, ok)
	}
	// no agent_statuses: the first agent_bot_user_ids with the session's
	// status, its loading message the app status's last
	bare := msg(`{"status":"processing","agent_bot_user_ids":["UAG2","UAG1"]}`)
	a.handle(rtm("ai_assistant_status", map[string]any{"channel_id": "C1", "thread_ts": bare.TS, "bot_user_id": "UAG2",
		"status": "is thinking…", "loading_messages": []string{"Planning", "Checking the plan"}}))
	if nug, ok := got(bare); !ok || len(nug.Agents) != 1 || nug.Agents[0].BotUserID != "UAG2" || nug.Agents[0].LoadingMessage != "Checking the plan" {
		t.Errorf("agent_bot_user_ids only: %+v %v, want UAG2 with the status's last loading message", nug, ok)
	}
	// an active session with an app status in the thread: the status
	a.handle(rtm("ai_assistant_status", map[string]any{"channel_id": "C1", "thread_ts": active.TS, "bot_user_id": "UAG1",
		"status": "is thinking…", "loading_messages": []string{"Looking around"}}))
	if nug, ok := got(active); !ok || len(nug.Agents) != 0 || nug.Status.BotUserID != "UAG1" || nug.Status.Name != "Codebot" {
		t.Errorf("an app status, no agent processing: %+v %v, want the status", nug, ok)
	}
	// Slackbot's session, or Slackbot's status: nothing
	sb := msg(`{"status":"processing","agent_statuses":[{"agent_bot_user_id":"USLACKBOT","status":"processing"}]}`)
	if nug, ok := got(sb); ok {
		t.Errorf("a Slackbot session: %+v, want no nugget", nug)
	}
	sbStatus := msg(`{"status":"processing","agent_statuses":[{"agent_bot_user_id":"UAG1","status":"processing"}]}`)
	a.handle(rtm("ai_assistant_status", map[string]any{"channel_id": "C1", "thread_ts": sbStatus.TS, "bot_user_id": "USLACKBOT", "status": "is thinking…"}))
	if nug, ok := got(sbStatus); ok {
		t.Errorf("Slackbot's status in the thread: %+v, want no nugget (isSlackbotAgentThread)", nug)
	}
	// the experiments
	a.SetExperimentGroup("app_nugget_refinement", "off")
	if _, ok := got(working); ok {
		t.Error("app_nugget_refinement off: a nugget, want none (the Stop nugget isn't built)")
	}
	a.SetExperimentGroup("app_nugget_refinement", "on")
	a.SetExperimentGroup("app_thread_nugget_fe", "off")
	if _, ok := got(working); ok {
		t.Error("app_thread_nugget_fe off: a nugget, want none")
	}
}
