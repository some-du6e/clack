package ui

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/idleprobe"
)

// lookupBehindDelay is how long each pass's lookup takes to answer: past
// the harness's max budget (latMaxBudget), so a message that waits for it
// can't pass.
const lookupBehindDelay = 900 * time.Millisecond

// TestMessageBehindLookupLatency: a message that comes right behind a
// socket event whose lookup is slow draws in Slack's next frame, not once
// the lookup answers (Karim: "messages show late"). The lookups ran on
// core's event pump: users.prefs after a pref_change or a
// search_frecency (Clack's own frecency saves bring one about once a
// minute), the sections reread, conversations.info after a self-join, a
// channel_joined, a group DM's conversion or a message in a conversation
// core didn't know, dnd.info after the viewer's dnd_updated, a saved
// message's messages.list and an assistant status's bot's users/info;
// every message behind one waited for its round trip. Each pass makes one
// lookup slow (lookupBehindDelay) and times the open channel's messages
// right behind its event, with TestMessageLatency's budgets.
func TestMessageBehindLookupLatency(t *testing.T) {
	if os.Getenv("CLACK_LATENCY_TESTS") == "" {
		t.Skip("CLACK_LATENCY_TESTS not set: flake-check's pump-lookups job runs this")
	}
	e := newLatEnv(t, latEnvOpts{rtt: latRTT})
	ids := e.fs.ChannelIDs()
	p := newLatProbe(t, e, ids[0])
	self := fakeslack.SelfID
	now := func() string {
		n := time.Now()
		return fmt.Sprintf("%d.%06d", n.Unix(), n.Nanosecond()/1000)
	}
	passes := []struct {
		name   string
		method string // the slow lookup, as fakeslack's faults name it
		ev     func(i int) map[string]any
	}{
		{"a pref_change", "users.prefs.get", func(i int) map[string]any {
			return map[string]any{"type": "pref_change", "name": "display_real_names", "value": i%2 == 1}
		}},
		{"a search_frecency", "users.prefs.get", func(int) map[string]any {
			return map[string]any{"type": "search_frecency", "value": "updated"}
		}},
		{"a sections change", "users.channelSections.list", func(int) map[string]any {
			return map[string]any{"type": "channel_sections_upserted", "channel_section_id": "S0FAKESECT"}
		}},
		{"the viewer's join", "conversations.info", func(int) map[string]any {
			return map[string]any{"type": "member_joined_channel", "channel": ids[1], "user": self}
		}},
		{"a channel_joined", "conversations.info", func(int) map[string]any {
			return map[string]any{"type": "channel_joined", "channel": map[string]any{"id": ids[2], "name": "joined-again"}}
		}},
		{"a group DM's conversion", "conversations.info", func(i int) map[string]any {
			return map[string]any{"type": "mpim_close", "channel": fmt.Sprintf("G0FAKEGONE%d", i), "converted_to": ids[3]}
		}},
		{"a message of a conversation core doesn't know", "conversations.info", func(i int) map[string]any {
			return map[string]any{"type": "message", "channel": fmt.Sprintf("C0NEWCHAN%03d", i), "user": self, "ts": now(), "text": "somewhere new"}
		}},
		{"the viewer's dnd_updated", "dnd.info", func(i int) map[string]any {
			return map[string]any{"type": "dnd_updated", "user": self, "dnd_status": map[string]any{"dnd_enabled": false, "next_dnd_start_ts": 1762600000 + i, "next_dnd_end_ts": 1762603600 + i}}
		}},
		{"a saved message", "messages.list", func(i int) map[string]any {
			return map[string]any{"type": "saved_added", "saved": map[string]any{"item_id": ids[0], "item_type": "message",
				"ts": fmt.Sprintf("1762700%03d.000100", i), "state": "in_progress", "date_created": 1762700000 + i}}
		}},
		{"an assistant status's new bot", "edge/users/info", func(i int) map[string]any {
			return map[string]any{"type": "ai_assistant_status", "channel_id": ids[0], "thread_ts": "1762700000.000100",
				"bot_user_id": latMember(9000 + i), "status": "is thinking…"}
		}},
	}
	res := map[string]map[string]latDist{}
	for _, ps := range passes {
		asked := e.fs.CallCount(ps.method)
		e.fs.SetFault(ps.method, fakeslack.Fault{Delay: lookupBehindDelay})
		ws := p.behind(3, func(i int) { e.fs.Push(ps.ev(i)) })
		e.fs.SetFault(ps.method, fakeslack.Fault{})
		name := "behind " + ps.name
		res[name] = latReport(t, latPass{name: name}, ws, true)
		if e.fs.CallCount(ps.method) == asked {
			t.Errorf("%s: no %s call: the pass measured no lookup", name, ps.method)
		}
		e.settle(lookupBehindDelay) // the last lookup's answer, before the next pass
	}
	latWrite(t, "TestMessageBehindLookupLatency", res)
}

// behind draws frames as the window does while, n times 700 ms apart, the
// socket brings push's event and right behind it someone's message in the
// open channel; it returns the messages' waits.
func (p *latProbe) behind(n int, push func(i int)) []*latWait {
	e := p.e
	p.mu.Lock()
	p.waits = nil
	p.mu.Unlock()
	senders := p.known(2)
	stop := make(chan struct{})
	var bg sync.WaitGroup
	bg.Add(1)
	go func() {
		defer bg.Done()
		for i := 0; i < n; i++ {
			select {
			case <-stop:
				return
			case <-time.After(700 * time.Millisecond):
			}
			push(i)
			p.post(senders[i%len(senders)], fmt.Sprintf("right behind %d %d", i, time.Now().UnixNano()%1e6))
		}
	}()
	done := func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		k := 0
		for _, w := range p.waits {
			if !w.done {
				return false
			}
			k++
		}
		return k >= n
	}
	askedAt, asked := e.r.WakeupTime()
	last := time.Now()
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline) && !done(); {
		var due <-chan time.Time
		if asked {
			due = time.After(max(time.Until(askedAt), 0))
		}
		select {
		case <-p.wake:
		case <-due:
		case <-time.After(10 * time.Millisecond):
			continue
		}
		if w := time.Until(last.Add(idleprobe.Vsync)); w > 0 { // the next vsync
			time.Sleep(w)
		}
		last = time.Now()
		e.quietFrame(asked, askedAt)
		p.check()
		askedAt, asked = e.r.WakeupTime()
	}
	close(stop)
	bg.Wait()
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*latWait(nil), p.waits...)
}
