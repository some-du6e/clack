package views

// Activity's rows keep their text across loads and are described again
// only when what they were described from changed (activity_settle.go):
// a load's answer settles before the next load starts, a profile change
// reaches the rows that read the member, and a refresh under the VIP sort
// keeps the later pages it merges after. Synthetic content only.

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
)

// actEditStoredMessage edits, in core's store, the message of a built row
// whose item hydrates from there, and returns the row's key and the new
// text (the next feed answer carries it).
func actEditStoredMessage(t *testing.T, v *Activity) (key, text string) {
	t.Helper()
	s := v.h.Core().S
	page := actPage(v)
	for i := range page.Items {
		it := &page.Items[i]
		r := v.rows[actKey(it)]
		if it.Msg == nil || it.Msg.TS != it.TS || r == nil || !r.built {
			continue
		}
		tl := s.Timeline(it.Channel)
		if tl == nil {
			continue
		}
		msgs := append([]slack.Message(nil), tl.Msgs...)
		for j := range msgs {
			if msgs[j].TS == it.TS {
				text = "synthetic edit seen after the reload"
				msgs[j].Text = text
				s.SetLatest(it.Channel, msgs, tl.HasMore)
				return actKey(it), text
			}
		}
	}
	t.Fatal("no built row whose message core's store holds")
	return "", ""
}

// actTSN is a synthetic feed_ts, n seconds into the fixture's day.
func actTSN(n int) string { return strconv.Itoa(1762700000+n) + ".000100" }

// actIdle waits, without a frame, for the feed's load to answer.
func actIdle(t *testing.T, v *Activity) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); v.data.busy(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the feed's load didn't answer")
		}
	}
}

// A load that starts before any frame settled the previous answer (the
// peek opened again off screen; an activity_invalidated reload, which
// notes.drain runs before settle in the same frame) still describes again
// the rows that answer changed: their text is the new one.
func TestActivitySettlesAnswerBeforeNextLoad(t *testing.T) {
	for _, order := range []string{"peek again", "invalidated in a frame"} {
		t.Run(order, func(t *testing.T) {
			v, _, e, _ := vaFakeActivity(t, false)
			e.frame()
			key, text := actEditStoredMessage(t, v)
			if strings.Contains(v.rows[key].d.prev, text) {
				t.Fatal("the row has the edit before any reload")
			}
			v.PeekOpened(func() {}, "activity") // load A: its answer brings the edit
			actIdle(t, v)                       // landed; no frame settled it
			switch order {
			case "peek again":
				v.PeekOpened(func() {}, "activity") // load B, still no frame
				actIdle(t, v)
				v.settle()
			case "invalidated in a frame":
				queued := func() int { v.notes.mu.Lock(); defer v.notes.mu.Unlock(); return len(v.notes.fns) }
				n0 := queued()
				raw, _ := json.Marshal(map[string]any{"type": "activity", "subtype": "activity_invalidated"})
				v.h.Core().Inject(slack.Event{Type: "activity", Subtype: "activity_invalidated", Raw: raw})
				for deadline := time.Now().Add(5 * time.Second); queued() <= n0; time.Sleep(5 * time.Millisecond) {
					if time.Now().After(deadline) {
						t.Fatal("activity_invalidated queued no reload")
					}
				}
				time.Sleep(100 * time.Millisecond) // the watcher handles events in order: ours is in by now
				e.frame()                          // drains the reload (load B) before it settles
				actIdle(t, v)
			}
			e.frame()
			e.frame()
			if r := v.rows[key]; r == nil || !strings.Contains(r.d.prev, text) {
				t.Errorf("row %s still reads %q; the first reload's answer changed it to %q", key, r.d.prev, text)
			}
		})
	}
}

// A member's new name and picture reach the rows that read them (their
// sender, a DM's title) on the profile change, with no reload: the rows
// keep their text across loads now, and core announces the change with a
// "user" event (user_change).
func TestActivityRowsFollowProfileChanges(t *testing.T) {
	v, _, e, _ := vaFakeActivity(t, false)
	e.frame()
	const id = "U0FAKE00001"
	var keys []string
	for k, r := range v.rows {
		if r.built && strings.Contains(r.d.name, "Ada") {
			keys = append(keys, k)
			found := false
			for _, x := range r.d.reads {
				found = found || x == id
			}
			if !found {
				t.Fatalf("row %s names Ada without recording that it read %s (reads %v)", k, id, r.d.reads)
			}
		}
	}
	if len(keys) == 0 {
		t.Fatal("no built row names Ada")
	}
	raw, _ := json.Marshal(map[string]any{"type": "user_change", "user": map[string]any{
		"id": id, "name": "ada", "real_name": "Ada Renamed",
		"profile": map[string]any{"real_name": "Ada Renamed", "display_name": "Ada Renamed", "image_48": "https://avatars.invalid/ada-new-48.png", "image_72": "https://avatars.invalid/ada-new-72.png"}}})
	v.h.Core().Inject(slack.Event{Type: "user_change", Raw: raw})
	e.until(t, "the rows' new name", func() bool {
		for _, k := range keys {
			if r := v.rows[k]; r == nil || !strings.Contains(r.d.name, "Ada Renamed") {
				return false
			}
		}
		return true
	})
	if u, ok := v.h.Core().User(id); !ok || u.Name != "Ada Renamed" {
		t.Fatalf("core has %+v", u)
	}
}

// Under the VIP sort the feed isn't in time order (VIP unreads first,
// then the rest), and a refresh's first page keeps the later pages loaded
// after it: where fresh ends is found by key. When the old list doesn't
// hold fresh's last row, the cursor is fresh's, so nothing is skipped.
func TestActivityMergeKeepsLaterPagesOutOfTimeOrder(t *testing.T) {
	item := func(key, ts string) core.ActivityItem {
		return core.ActivityItem{Key: key, Type: "at_user", FeedTS: ts}
	}
	var page1, old []core.ActivityItem
	for i := 0; i < 20; i++ { // page 1: VIP unreads, ts 100 … 81
		page1 = append(page1, item("vip-"+string(rune('a'+i)), actTSN(100-i)))
	}
	old = append(old, page1...)
	old = append(old, item("vip-last", actTSN(5))) // page 2: the last VIP unread, then the rest by time
	for i := 0; i < 19; i++ {
		old = append(old, item("rest-"+string(rune('a'+i)), actTSN(99-i)))
	}
	got := actMerge(core.ActivityPage{Items: page1, Next: "c2"}, core.ActivityPage{Items: old, Next: "c3"}, false)
	if len(got.Items) != len(old) || got.Next != "c3" {
		t.Fatalf("merged %d rows (next %q), want the %d loaded with next c3", len(got.Items), got.Next, len(old))
	}
	for i := range old {
		if got.Items[i].Key != old[i].Key {
			t.Fatalf("row %d is %s, want %s", i, got.Items[i].Key, old[i].Key)
		}
	}
	// a fresh page that ends on a row the old list doesn't hold
	fresh := append([]core.ActivityItem{item("new", actTSN(101))}, page1[:19]...)
	fresh[len(fresh)-1] = item("unseen", actTSN(82))
	got = actMerge(core.ActivityPage{Items: fresh, Next: "c2"}, core.ActivityPage{Items: old, Next: "c3"}, false)
	if len(got.Items) != len(fresh) || got.Next != "c2" {
		t.Errorf("merged %d rows (next %q), want fresh's %d with its cursor c2", len(got.Items), got.Next, len(fresh))
	}
	// the viewer read vip-b: it left the VIP unreads for the rest, a page
	// further on, so the refresh pages again from fresh instead of keeping
	// the old cursor past where vip-b now sits
	read := append(append([]core.ActivityItem{page1[0]}, page1[2:]...), old[20]) // vip-a, vip-c … vip-t, vip-last
	got = actMerge(core.ActivityPage{Items: read, Next: "c2"}, core.ActivityPage{Items: old, Next: "c3"}, false)
	if len(got.Items) != len(read) || got.Next != "c2" {
		t.Errorf("after reading vip-b: merged %d rows (next %q), want fresh's %d with its cursor c2", len(got.Items), got.Next, len(read))
	}
	// in time order a first-page row fresh doesn't hold is gone (deleted,
	// cleared): it stays out and the later pages stay
	var chrono []core.ActivityItem
	for i := 0; i < 40; i++ {
		chrono = append(chrono, item("t-"+strconv.Itoa(i), actTSN(200-i)))
	}
	gone := append(append([]core.ActivityItem{}, chrono[:3]...), chrono[4:21]...) // t-3 deleted: t-20 joins page 1
	got = actMerge(core.ActivityPage{Items: gone, Next: "c2"}, core.ActivityPage{Items: chrono, Next: "c3"}, true)
	if len(got.Items) != len(chrono)-1 || got.Next != "c3" {
		t.Fatalf("time order: merged %d rows (next %q), want %d with next c3", len(got.Items), got.Next, len(chrono)-1)
	}
	for _, it := range got.Items {
		if it.Key == "t-3" {
			t.Error("time order: the deleted row came back")
		}
	}
}
