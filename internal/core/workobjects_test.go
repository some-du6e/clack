package core

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/workobject"
)

// woApp boots core over fakeslack's #work-objects with the socket open.
func woApp(t *testing.T) (*fakeslack.Server, *App, <-chan Event) {
	t.Helper()
	sz := fakeslack.Small
	sz.WorkObjects = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	evs, stop := a.Subscribe()
	t.Cleanup(stop)
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.SetRealtime(true)
	waitEvent(t, evs, 5*time.Second, func(ev Event) bool { return ev.Type == "connection" && ev.Connected })
	return fs, a, evs
}

// waitWO waits for a work_objects event after which ok holds.
func waitWO(t *testing.T, evs <-chan Event, what string, ok func() bool) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for !ok() {
		select {
		case <-evs:
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// The flexpane's fetch (fetchWorkObject): the card's unfurl data at
// once, then workObjects.getEntity's live details from
// entity_details_present.
func TestWorkObjectDetailsFromRTM(t *testing.T) {
	fs, a, evs := woApp(t)
	raw, _ := json.Marshal(fs.WorkObjectTask(1762699700))
	a.FetchWorkObject(WorkObjectRequest{EntityID: fakeslack.WOTaskID, EntityURL: "https://tracker.example.com/TASK-142",
		Channel: fakeslack.WorkObjectsChannelID, TS: fakeslack.WorkObjectTS[0], Unfurl: raw, Reason: "work-objects-open-flexpane"})
	if d := a.WorkObject(fakeslack.WOTaskID); d == nil || d.Entity == nil || !d.FromUnfurl || !d.Loading {
		t.Fatalf("before the event: %+v", d)
	}
	waitWO(t, evs, "entity_details_present", func() bool {
		d := a.WorkObject(fakeslack.WOTaskID)
		return d != nil && !d.Loading && !d.FromUnfurl && d.Entity != nil
	})
	fetched, _, _ := fs.WorkObjectCalls()
	if len(fetched) != 1 || fetched[0].Get("entity_id") != fakeslack.WOTaskID || fetched[0].Get("channel_id") != fakeslack.WorkObjectsChannelID ||
		fetched[0].Get("message_ts") != fakeslack.WorkObjectTS[0] || fetched[0].Get("reason") != "work-objects-open-flexpane" {
		t.Errorf("getEntity params %v", fetched)
	}
}

// Comments: getComments' list from entity_comments_present, a post's
// comment from entity_comment_action_acknowledged; the badge only once
// the comments were seen (hasNewerUnfurlComments).
func TestWorkObjectComments(t *testing.T) {
	fs, a, evs := woApp(t)
	a.FetchWorkObjectComments(fakeslack.WOTaskID, "https://tracker.example.com/TASK-142")
	waitWO(t, evs, "entity_comments_present", func() bool {
		c := a.WorkObjectCommentsOf(fakeslack.WOTaskID)
		return c != nil && len(c.Comments) == 2 && c.CanPostComment
	})
	block := json.RawMessage(`{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"Looks right"}]}]}`)
	a.PostWorkObjectComment(fakeslack.WOTaskID, "https://tracker.example.com/TASK-142", block)
	waitWO(t, evs, "entity_comment_action_acknowledged", func() bool {
		c := a.WorkObjectCommentsOf(fakeslack.WOTaskID)
		return c != nil && len(c.Comments) == 3 && !c.Posting
	})
	_, posted, _ := fs.WorkObjectCalls()
	if len(posted) != 1 || posted[0].Get("reason") != "work-object-comment-post" {
		t.Fatalf("postComment calls %v", posted)
	}
	var cm struct {
		Blocks json.RawMessage `json:"blocks"`
	}
	if json.Unmarshal([]byte(posted[0].Get("comment")), &cm) != nil || len(cm.Blocks) == 0 {
		t.Errorf("comment param %q", posted[0].Get("comment"))
	}
	// never seen: no badge, however new the card's latest comment is
	if _, _, newer := a.WorkObjectCardState("C", "1", fakeslack.WOTaskID, 1e12); newer {
		t.Error("badge before the comments were ever seen")
	}
	a.SeeWorkObjectComments(fakeslack.WOTaskID, 100)
	if _, _, newer := a.WorkObjectCardState("C", "1", fakeslack.WOTaskID, 1); newer {
		t.Error("badge for a comment older than the seen one")
	}
	if _, _, newer := a.WorkObjectCardState("C", "1", fakeslack.WOTaskID, 1e12); !newer {
		t.Error("no badge for a comment newer than the seen one")
	}
}

// Refresh (useRefresh): chat.refreshUnfurl; refreshing until the unfurl
// comes back with a new updated_at.
func TestWorkObjectRefresh(t *testing.T) {
	fs, a, evs := woApp(t)
	ch, ts := fakeslack.WorkObjectsChannelID, fakeslack.WorkObjectTS[0]
	if _, err := a.History(context.Background(), ch, ""); err != nil {
		t.Fatal(err)
	}
	a.RefreshWorkObject(ch, ts, fakeslack.WOTaskID, 1762699700)
	if r, _, _ := a.WorkObjectCardState(ch, ts, fakeslack.WOTaskID, 0); !r {
		t.Fatal("not refreshing after Refresh")
	}
	waitWO(t, evs, "the refreshed unfurl", func() bool {
		r, f, _ := a.WorkObjectCardState(ch, ts, fakeslack.WOTaskID, 0)
		return !r && !f
	})
	_, _, refreshed := fs.WorkObjectCalls()
	if len(refreshed) != 1 || refreshed[0].Get("entity_id") != fakeslack.WOTaskID || refreshed[0].Get("reason") != "work-objects-manual-refresh" {
		t.Errorf("refreshUnfurl calls %v", refreshed)
	}
	m, ok := a.S.Find(ch, ts)
	if !ok || len(m.Attachments) != 1 {
		t.Fatal("refreshed message missing")
	}
	e := workobject.Parse(m.Attachments[0].WorkObjectEntity)
	if e == nil || e.Expanded().UpdatedAtUnix() <= 1762699700 {
		t.Errorf("updated_at after the refresh: %+v", e)
	}
	// a refresh the server refuses fails at once (and says so for 2s)
	a.RefreshWorkObject(ch, "1.000", fakeslack.WOTaskID, 0)
	waitWO(t, evs, "the failed refresh", func() bool {
		_, f, _ := a.WorkObjectCardState(ch, "1.000", fakeslack.WOTaskID, 0)
		return f
	})
}

// Conversations: entities.refs.list.
func TestWorkObjectRefs(t *testing.T) {
	_, a, evs := woApp(t)
	a.FetchWorkObjectRefs(fakeslack.WOTaskID, "")
	waitWO(t, evs, "entities.refs.list", func() bool { return len(a.WorkObjectRefs(fakeslack.WOTaskID)) == 1 })
	if r := a.WorkObjectRefs(fakeslack.WOTaskID)[0]; r.ChannelID != fakeslack.WorkObjectsChannelID || r.TS != fakeslack.WorkObjectTS[0] {
		t.Errorf("ref %+v", r)
	}
}

// The work object experiments read the account's groups, and a forced
// group flips them (the Experiments plugins).
func TestWorkObjectExperiments(t *testing.T) {
	a, _ := testApp(t)
	f := a.WorkObjectFlags()
	if !f.Rearrange || !f.RefreshTimestamp || !f.CommentSync || !f.EmbeddedPreview || !f.Search || !f.VideoPreviews {
		t.Errorf("defaults (the account's groups): %+v", f)
	}
	a.SetExperimentGroup(ExpWORearrangement, "off")
	if a.WorkObjectFlags().Rearrange {
		t.Error("rearrangement still on after forcing off")
	}
}
