package core

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// The socket events' lookups run off the event pump (pump_later.go). What
// the pump applies meanwhile must stand as it did when the lookup blocked
// the pump (ui bf448f44): a new group DM's first message counts as a DM's,
// a second sections reread installs its newer list, and a channel left
// while its join's conversations.info was out stays left.

// gateOnce makes the fake's first call of method wait until the returned
// release runs (the later calls answer at once); started reports whether
// that first call came.
func gateOnce(f *fakeSlack, method string) (release func(), started func() bool) {
	gate := make(chan struct{})
	var calls atomic.Int32
	var once sync.Once
	f.mu.Lock()
	f.hooks[method] = func(url.Values) {
		if calls.Add(1) == 1 {
			<-gate
		}
	}
	f.mu.Unlock()
	return func() { once.Do(func() { close(gate) }) }, func() bool { return calls.Load() > 0 }
}

// within runs f as the event pump would, one event after another, and
// reports whether it returned within d.
func within(d time.Duration, f func()) (done <-chan struct{}, ok bool) {
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		f()
	}()
	select {
	case <-ch:
		return ch, true
	case <-time.After(d):
		return ch, false
	}
}

// A group DM's first message badges it (NoteMessage counts every message
// from others in a DM) though its conversations.info is still out: the
// conversation is in the member set as a group DM before the message is
// counted. On ui mpim_joined's lookup ran on the pump first; queued, the
// message came first and counted as a channel's, with no mention, and the
// row was bold with no badge.
func TestNewGroupDMsFirstMessageBadges(t *testing.T) {
	cases := []struct {
		name  string
		setup func(a *App)
		ev    func(ch string) slack.Event // what comes before the message (nil: nothing)
	}{
		{"mpim_joined with its channel", nil, func(ch string) slack.Event {
			return rtm("mpim_joined", map[string]any{"channel": map[string]any{"id": ch, "name": "mpdm-alice--bob--me-1", "is_mpim": true}})
		}},
		{"mpim_open of a group DM the store keeps outside the sidebar", func(a *App) {
			a.S.PutExtConv(slack.Conversation{ID: "C0GRP2", Name: "mpdm-alice--bob--me-2", IsMPIM: true})
		}, func(ch string) slack.Event { return rtm("mpim_open", map[string]any{"channel": ch, "user": "USELF"}) }},
		{"a group DM core never saw", nil, nil},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, f := testApp(t)
			ch := fmt.Sprintf("C0GRP%d", i+1)
			if tc.setup != nil {
				tc.setup(a)
			}
			f.mu.Lock()
			f.reply["conversations.info"] = `{"ok":true,"channel":{"id":"` + ch + `","name":"mpdm-alice--bob--me-` + fmt.Sprint(i+1) + `","is_mpim":true}}`
			f.mu.Unlock()
			release, _ := gateOnce(f, "conversations.info")
			defer release()
			const ts = "1762518900.000100"
			done, ok := within(3*time.Second, func() {
				if tc.ev != nil {
					a.handle(tc.ev(ch))
				}
				a.handle(rtm("message", map[string]any{"channel": ch, "ts": ts, "user": "U2", "text": "hi"}))
			})
			if !ok {
				t.Error("the pump waited for the group DM's conversations.info")
			}
			release()
			<-done
			a.laterDone(t)
			if c := a.Count(ch); !c.Unread || c.Mentions != 1 {
				t.Errorf("the group DM's first message counts %+v, want unread with 1 mention (a DM counts every message)", c)
			}
		})
	}
}

// A sections reread whose event came while an earlier reread was out
// installs its newer list. Each reread compared the journal with where it
// stood when its event came, the earlier reread's install moved the
// journal past that, and the newer list was thrown away: a second move
// made from another client stayed undone in the sidebar.
func TestSectionRereadsTakeTheNewest(t *testing.T) {
	a, f := testApp(t)
	const method = "users.channelSections.list"
	list := func(ids ...string) string {
		var secs []string
		for _, id := range ids {
			secs = append(secs, `{"channel_section_id":"`+id+`","name":"`+id+`","type":"standard","channel_ids_page":{"channel_ids":[]}}`)
		}
		return `{"ok":true,"channel_sections":[` + strings.Join(secs, ",") + `]}`
	}
	f.mu.Lock()
	f.reply[method] = list("S1")
	f.mu.Unlock()
	release, started := gateOnce(f, method)
	defer release()
	a.handle(rtm("channel_sections_channels_upserted", map[string]any{"channel_section_id": "S1"}))
	for dl := time.Now().Add(5 * time.Second); !started(); time.Sleep(time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatal("the first reread never asked")
		}
	}
	// the other client's second move, after the first reread was served
	f.mu.Lock()
	f.reply[method] = list("S1", "S2")
	f.mu.Unlock()
	done, ok := within(3*time.Second, func() {
		a.handle(rtm("channel_sections_channels_upserted", map[string]any{"channel_section_id": "S2"}))
	})
	if !ok {
		t.Error("the pump waited for the sections reread")
	}
	release()
	<-done
	a.laterDone(t)
	a.S.RLock()
	var got []string
	for _, s := range a.S.Sections {
		got = append(got, s.ID)
	}
	a.S.RUnlock()
	if strings.Join(got, " ") != "S1 S2" {
		t.Errorf("the sections are %v after both rereads, want the second's [S1 S2]", got)
	}
}

// A channel left while its join's conversations.info was out stays left:
// the answer, read before the leave, doesn't put it back in the member set.
// On ui the lookup ran on the pump before the leave.
func TestLeftChannelStaysLeft(t *testing.T) {
	cases := []struct {
		name  string
		join  slack.Event
		leave string
	}{
		{"channel_joined, then channel_left", rtm("channel_joined", map[string]any{"channel": map[string]any{"id": "C9", "name": "joined-and-left", "is_channel": true}}), "channel_left"},
		{"the viewer's member_joined_channel, then channel_left", rtm("member_joined_channel", map[string]any{"channel": "C9", "user": "USELF"}), "channel_left"},
		{"channel_joined, then channel_archive", rtm("channel_joined", map[string]any{"channel": map[string]any{"id": "C9", "name": "joined-and-archived", "is_channel": true}}), "channel_archive"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, f := testApp(t)
			f.mu.Lock()
			f.reply["conversations.info"] = `{"ok":true,"channel":{"id":"C9","name":"joined-and-left","is_channel":true,"is_member":true}}`
			f.mu.Unlock()
			release, started := gateOnce(f, "conversations.info")
			defer release()
			done, ok := within(3*time.Second, func() { a.handle(tc.join) })
			if !ok {
				t.Error("the pump waited for the join's conversations.info")
			}
			for dl := time.Now().Add(5 * time.Second); !started(); time.Sleep(time.Millisecond) {
				if time.Now().After(dl) {
					t.Fatal("the join's conversations.info never asked")
				}
			}
			a.handle(rtm(tc.leave, map[string]any{"channel": "C9"}))
			release()
			<-done
			a.laterDone(t)
			if c, member := a.S.Conv("C9"); member {
				t.Errorf("after %s the channel is back in the member set: %+v", tc.leave, c)
			}
		})
	}
}

// An older dnd.info answer doesn't overwrite a newer dnd_updated: the
// viewer snoozed, then resumed from another device, and the read the
// snooze queued came back afterwards, still snoozed. With the resume's own
// read failing, the toasts stayed held until DNDHeld's refetch.
func TestOlderDNDAnswerGivesWay(t *testing.T) {
	a, f := testApp(t)
	future := time.Now().Add(time.Hour).Unix()
	f.mu.Lock()
	f.reply["dnd.info"] = fmt.Sprintf(`{"ok":true,"dnd_enabled":false,"snooze_enabled":true,"snooze_endtime":%d}`, future)
	f.mu.Unlock()
	release, started := gateOnce(f, "dnd.info")
	defer release()
	a.handle(rtm("dnd_updated", map[string]any{"user": "USELF", "dnd_status": map[string]any{"dnd_enabled": false, "snooze_enabled": true, "snooze_endtime": future}}))
	for dl := time.Now().Add(5 * time.Second); !started(); time.Sleep(time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatal("the snooze's dnd.info never asked")
		}
	}
	// the resume, while the snooze's read is out; its own read fails
	f.mu.Lock()
	f.reply["dnd.info"] = `{"ok":false,"error":"fatal_error"}`
	f.mu.Unlock()
	a.handle(rtm("dnd_updated", map[string]any{"user": "USELF", "dnd_status": map[string]any{"dnd_enabled": false, "snooze_enabled": false}}))
	release()
	a.laterDone(t)
	a.dnd.mu.Lock()
	st := a.dnd.status
	a.dnd.mu.Unlock()
	if st.Active(time.Now()) {
		t.Errorf("notifications paused after the viewer resumed: %+v (the snooze's older dnd.info answer won)", st)
	}
}

// A saved event's store writes happen on the pump, as on ui: its saved
// state is there right after the event, while the lookups queued before it
// (a pref_change's users.prefs) and its own message's are still out.
// Queued whole, the save waited behind every earlier lookup.
func TestSavedStateBeforeItsLookup(t *testing.T) {
	a, f := testApp(t)
	f.mu.Lock()
	f.reply["users.prefs.get"] = `{"ok":true,"prefs":{}}`
	f.reply["messages.list"] = `{"ok":true,"messages":{}}`
	f.mu.Unlock()
	releasePrefs, started := gateOnce(f, "users.prefs.get")
	defer releasePrefs()
	a.handle(rtm("pref_change", map[string]any{"name": "display_real_names", "value": true}))
	for dl := time.Now().Add(5 * time.Second); !started(); time.Sleep(time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatal("the pref_change's users.prefs never asked")
		}
	}
	done, ok := within(3*time.Second, func() {
		a.handle(rtm("saved_added", map[string]any{"saved": map[string]any{"item_id": "C1", "item_type": "message", "ts": "1762518950.000100",
			"date_created": 1762518960, "state": "in_progress"}}))
	})
	if !ok {
		t.Error("the pump waited for a lookup")
	}
	if _, saved := a.SavedMeta("C1", "1762518950.000100"); !saved {
		t.Error("the message isn't saved right after saved_added: its store write waited behind the lookups queued before it")
	}
	releasePrefs()
	<-done
	a.laterDone(t)
	if _, saved := a.SavedMeta("C1", "1762518950.000100"); !saved {
		t.Error("the message isn't saved after the lookups")
	}
}
